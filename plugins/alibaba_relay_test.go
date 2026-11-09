package plugins_test

import (
	"maps"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// tokenPonyRelayConfig is the spec's TokenPony configuration plus an
// inputSeconds mapping so settlement of input video seconds is covered.
const tokenPonyRelayConfig = `{"relay":{
  "submit":{"path":"/v1/aigc/video/generate",
    "body":{"model":"$.model","prompt":"$.input.prompt","media":"$.input.media","resolution":"$.parameters.resolution","ratio":"$.parameters.ratio","duration":"$.parameters.duration","generate_audio":"$.parameters.audio","watermark":"$.parameters.watermark","seed":"$.parameters.seed"},
    "values":{"media[].type":{"first_frame":"first_image","last_frame":"last_image"},"seed":{"-1":null}},
    "ok":{"path":"code","equals":200},"taskId":"data.id","error":"message"},
  "query":{"path":"/v1/aigc/tasks/{task_id}","ok":{"path":"code","equals":200},"status":"data.task_status",
    "statusMap":{"PENDING":"PENDING","RUNNING":"RUNNING","COMPLETED":"SUCCEEDED","FAILED":"FAILED","CANCELED":"CANCELED"},
    "videoUrl":["data.result.output","data.result.result"],"reason":"data.task_status_msg",
    "usage":{"seconds":"data.usage.duration","inputSeconds":"data.usage.input_duration","resolution":"data.usage.resolution"}}
}}`

func relayConfigValue(t *testing.T, text string) map[string]any {
	t.Helper()
	var config map[string]any
	require.NoError(t, common.UnmarshalJsonStr(text, &config))
	return config
}

func TestAlibabaRelayMode(t *testing.T) {
	plugin := newAlibabaPlugin(t)
	config := relayConfigValue(t, tokenPonyRelayConfig)
	submitCtx := func(model string, body map[string]any) map[string]any {
		return map[string]any{"model": model, "upstreamModel": model, "baseUrl": "https://relay.example/", "apiKey": "rk", "requestBody": body, "channelConfig": config}
	}
	queryCtx := map[string]any{"taskId": "relay/task 1", "model": "wan3.0-video", "upstreamModel": "wan3.0-video", "baseUrl": "https://relay.example", "apiKey": "rk", "channelConfig": config}

	t.Run("submit translates the normalized request into the relay format", func(t *testing.T) {
		value, err := plugin.Engine.Call(t.Context(), "buildSubmitRequest", submitCtx("wan3.0-video", map[string]any{
			"model": "wan3.0-video", "prompt": "a cat", "seconds": 5, "resolution": "720P", "seed": -1, "audio": false,
			"images": []any{"https://cdn.example/first.png", "https://cdn.example/last.png"},
		}))
		require.NoError(t, err)
		descriptor := alibabaObject(t, value)
		assert.Equal(t, "https://relay.example/v1/aigc/video/generate", descriptor["url"])
		assert.Equal(t, "POST", descriptor["method"])
		assert.Equal(t, map[string]any{"Authorization": "Bearer rk", "Content-Type": "application/json"}, descriptor["headers"])
		assert.Equal(t, map[string]any{
			"model": "wan3.0-video", "prompt": "a cat", "resolution": "720P", "ratio": "adaptive", "duration": float64(5), "generate_audio": false,
			"media": []any{
				map[string]any{"type": "first_image", "url": "https://cdn.example/first.png"},
				map[string]any{"type": "last_image", "url": "https://cdn.example/last.png"},
			},
		}, descriptor["body"], "unresolved fields, prompt_extend and the -1 seed are not sent")
	})

	t.Run("submit forwards an inline first-frame image larger than the JSON clone limit", func(t *testing.T) {
		image := "data:image/png;base64," + strings.Repeat("A", 2<<20)
		value, err := plugin.Engine.Call(t.Context(), "buildSubmitRequest", submitCtx("wan3.0-video", map[string]any{
			"model": "wan3.0-video", "prompt": "a cat", "images": []any{image},
		}))
		require.NoError(t, err)
		media := alibabaObject(t, value)["body"].(map[string]any)["media"].([]any)
		require.Len(t, media, 1)
		entry := media[0].(map[string]any)
		assert.Equal(t, "first_image", entry["type"])
		assert.True(t, entry["url"] == image, "the data URL is forwarded unchanged")
	})

	t.Run("values rewrite one rendered reference and leave another reference to the same source", func(t *testing.T) {
		shared := relayConfigValue(t, tokenPonyRelayConfig)
		submit := shared["relay"].(map[string]any)["submit"].(map[string]any)
		submit["body"] = map[string]any{"media": "$.input.media", "media_copy": "$.input.media"}
		submit["values"] = map[string]any{"media[].type": map[string]any{"first_frame": "first_image"}}
		_, err := plugin.Engine.Call(t.Context(), "validateChannelConfig", shared)
		require.NoError(t, err)

		ctx := submitCtx("wan3.0-video", map[string]any{"model": "wan3.0-video", "prompt": "a cat", "images": []any{"https://cdn.example/first.png"}})
		ctx["channelConfig"] = shared
		value, err := plugin.Engine.Call(t.Context(), "buildSubmitRequest", ctx)
		require.NoError(t, err)
		assert.Equal(t, map[string]any{
			"media":      []any{map[string]any{"type": "first_image", "url": "https://cdn.example/first.png"}},
			"media_copy": []any{map[string]any{"type": "first_frame", "url": "https://cdn.example/first.png"}},
		}, alibabaObject(t, value)["body"])
	})

	t.Run("prototype member names stay plain data in values rules", func(t *testing.T) {
		guarded := relayConfigValue(t, tokenPonyRelayConfig)
		submit := guarded["relay"].(map[string]any)["submit"].(map[string]any)
		submit["body"] = map[string]any{"prompt": "$.input.prompt", "media": "$.input.media"}
		submit["values"] = map[string]any{"prompt": map[string]any{"x": "y"}, "media[].type": map[string]any{"first_frame": "first_image"}}
		for _, prompt := range []string{"constructor", "__proto__"} {
			ctx := submitCtx("wan3.0-video", map[string]any{"model": "wan3.0-video", "prompt": prompt, "media": []any{
				map[string]any{"type": "first_frame", "url": "https://cdn.example/first.png", "__proto__": map[string]any{"type": "last_frame"}},
			}})
			ctx["channelConfig"] = guarded
			value, err := plugin.Engine.Call(t.Context(), "buildSubmitRequest", ctx)
			require.NoError(t, err)
			assert.Equal(t, map[string]any{"prompt": prompt, "media": []any{
				map[string]any{"type": "first_image", "url": "https://cdn.example/first.png", "__proto__": map[string]any{"type": "last_frame"}},
			}}, alibabaObject(t, value)["body"], "a mapping without the key leaves %q, and a rewritten element keeps its own __proto__ member", prompt)
		}
	})

	t.Run("relay mode rejects image models", func(t *testing.T) {
		_, err := plugin.Engine.Call(t.Context(), "buildSubmitRequest", submitCtx("wan2.6-t2i", map[string]any{"model": "wan2.6-t2i", "prompt": "a cat"}))
		require.ErrorContains(t, err, "relay mode only supports video models")
	})

	t.Run("$each renders one entry per source element and omits a missing source", func(t *testing.T) {
		each := relayConfigValue(t, tokenPonyRelayConfig)
		submit := each["relay"].(map[string]any)["submit"].(map[string]any)
		submit["body"] = map[string]any{"model": "$.model", "media": map[string]any{"$each": "$.input.media", "kind": "@.type", "image_url": "@.url"}}
		delete(submit, "values")

		ctx := submitCtx("wan3.0-video", map[string]any{"model": "wan3.0-video", "prompt": "a cat", "images": []any{"https://cdn.example/first.png", "https://cdn.example/last.png"}})
		ctx["channelConfig"] = each
		value, err := plugin.Engine.Call(t.Context(), "buildSubmitRequest", ctx)
		require.NoError(t, err)
		assert.Equal(t, map[string]any{"model": "wan3.0-video", "media": []any{
			map[string]any{"kind": "first_frame", "image_url": "https://cdn.example/first.png"},
			map[string]any{"kind": "last_frame", "image_url": "https://cdn.example/last.png"},
		}}, alibabaObject(t, value)["body"])

		ctx = submitCtx("wan3.0-video", map[string]any{"model": "wan3.0-video", "prompt": "a cat"})
		ctx["channelConfig"] = each
		value, err = plugin.Engine.Call(t.Context(), "buildSubmitRequest", ctx)
		require.NoError(t, err)
		assert.Equal(t, map[string]any{"model": "wan3.0-video"}, alibabaObject(t, value)["body"], "a text-only request has no media to iterate")
	})

	t.Run("header overrides replace defaults case-insensitively and null removes one", func(t *testing.T) {
		custom := relayConfigValue(t, tokenPonyRelayConfig)
		custom["relay"].(map[string]any)["submit"].(map[string]any)["headers"] = map[string]any{"authorization": "Token {key}", "Content-Type": nil, "X-Trace": "t-1"}
		ctx := submitCtx("wan3.0-video", map[string]any{"model": "wan3.0-video", "prompt": "a cat"})
		ctx["channelConfig"] = custom
		value, err := plugin.Engine.Call(t.Context(), "buildSubmitRequest", ctx)
		require.NoError(t, err)
		assert.Equal(t, map[string]any{"authorization": "Token rk", "X-Trace": "t-1"}, alibabaObject(t, value)["headers"])
	})

	t.Run("submit response yields the relay task id and a DashScope snapshot", func(t *testing.T) {
		value, err := plugin.Engine.Call(t.Context(), "parseSubmitResponse", submitCtx("wan3.0-video", nil), map[string]any{
			"statusCode": 200, "headers": map[string]any{}, "body": map[string]any{"code": 200, "message": "success", "data": map[string]any{"id": "relay-task-1"}},
		})
		require.NoError(t, err)
		assert.Equal(t, map[string]any{
			"taskId":   "relay-task-1",
			"taskData": map[string]any{"request_id": "", "output": map[string]any{"task_id": "relay-task-1", "task_status": "PENDING"}},
		}, alibabaObject(t, value))

		_, err = plugin.Engine.Call(t.Context(), "parseSubmitResponse", submitCtx("wan3.0-video", nil), map[string]any{
			"statusCode": 200, "headers": map[string]any{}, "body": map[string]any{"code": 10108, "message": "duration out of range"},
		})
		require.ErrorContains(t, err, "duration out of range")

		_, err = plugin.Engine.Call(t.Context(), "parseSubmitResponse", submitCtx("wan3.0-video", nil), map[string]any{
			"statusCode": 200, "headers": map[string]any{}, "body": map[string]any{"code": 200, "data": map[string]any{}},
		})
		require.ErrorContains(t, err, "no task id at data.id")

		value, err = plugin.Engine.Call(t.Context(), "parseSubmitResponse", submitCtx("wan3.0-video", nil), map[string]any{
			"statusCode": 200, "headers": map[string]any{}, "body": map[string]any{"code": 200, "data": map[string]any{"id": 12345}},
		})
		require.NoError(t, err)
		assert.Equal(t, map[string]any{
			"taskId":   "12345",
			"taskData": map[string]any{"request_id": "", "output": map[string]any{"task_id": "12345", "task_status": "PENDING"}},
		}, alibabaObject(t, value), "a numeric task id is kept as a string")

		stringOK := relayConfigValue(t, tokenPonyRelayConfig)
		stringOK["relay"].(map[string]any)["submit"].(map[string]any)["ok"] = map[string]any{"path": "code", "equals": "200"}
		ctx := submitCtx("wan3.0-video", nil)
		ctx["channelConfig"] = stringOK
		value, err = plugin.Engine.Call(t.Context(), "parseSubmitResponse", ctx, map[string]any{
			"statusCode": 200, "headers": map[string]any{}, "body": map[string]any{"code": 200, "data": map[string]any{"id": "relay-task-2"}},
		})
		require.NoError(t, err, "a string ok.equals matches a numeric code")
		assert.Equal(t, "relay-task-2", alibabaObject(t, value)["taskId"])

		_, err = plugin.Engine.Call(t.Context(), "parseSubmitResponse", submitCtx("wan3.0-video", nil), map[string]any{
			"statusCode": 200, "headers": map[string]any{}, "body": map[string]any{"code": 500},
		})
		require.ErrorContains(t, err, "relay returned a failure")
	})

	t.Run("query URL encodes the relay task id", func(t *testing.T) {
		value, err := plugin.Engine.Call(t.Context(), "buildQueryRequest", queryCtx)
		require.NoError(t, err)
		assert.Equal(t, map[string]any{
			"url": "https://relay.example/v1/aigc/tasks/relay%2Ftask%201", "method": "GET", "headers": map[string]any{"Authorization": "Bearer rk"},
		}, alibabaObject(t, value))

		post := relayConfigValue(t, tokenPonyRelayConfig)
		query := post["relay"].(map[string]any)["query"].(map[string]any)
		query["path"] = "/v1/tasks/query"
		query["method"] = "POST"
		query["body"] = map[string]any{"id": "$.task_id", "detail": true}
		postCtx := map[string]any{"taskId": "relay-1", "baseUrl": "https://relay.example", "apiKey": "rk", "channelConfig": post}
		value, err = plugin.Engine.Call(t.Context(), "buildQueryRequest", postCtx)
		require.NoError(t, err)
		descriptor := alibabaObject(t, value)
		assert.Equal(t, "https://relay.example/v1/tasks/query", descriptor["url"])
		assert.Equal(t, "POST", descriptor["method"])
		assert.Equal(t, map[string]any{"id": "relay-1", "detail": true}, descriptor["body"])
		assert.Equal(t, "application/json", descriptor["headers"].(map[string]any)["Content-Type"])
	})

	t.Run("query results map to task states with a DashScope snapshot", func(t *testing.T) {
		cases := []struct {
			name, body string
			want       map[string]any
		}{
			{"running", `{"code":200,"data":{"task_status":"RUNNING","result":{},"usage":null}}`, map[string]any{
				"status": "IN_PROGRESS",
				"data":   map[string]any{"request_id": "", "output": map[string]any{"task_id": "relay/task 1", "task_status": "RUNNING"}},
			}},
			{"completed through the fallback URL path", `{"code":200,"data":{"task_status":"COMPLETED","result":{"result":"https://cdn.example/v.mp4"},"usage":{"duration":5,"input_duration":3,"resolution":"720P"}}}`, map[string]any{
				"status": "SUCCESS", "url": "https://cdn.example/v.mp4",
				"data": map[string]any{
					"request_id": "",
					"output":     map[string]any{"task_id": "relay/task 1", "task_status": "SUCCEEDED", "video_url": "https://cdn.example/v.mp4"},
					"usage":      map[string]any{"duration": float64(5), "output_video_duration": float64(5), "input_video_duration": float64(3), "SR": "720P"},
				},
			}},
			{"completed without a video URL", `{"code":200,"data":{"task_status":"COMPLETED","result":{}}}`, map[string]any{
				"status": "UNKNOWN", "reason": "relay reported success without a video URL",
			}},
			{"failed", `{"code":200,"data":{"task_status":"FAILED","task_status_msg":"InvalidParameter: bad image"}}`, map[string]any{
				"status": "FAILURE", "reason": "InvalidParameter: bad image",
				"data": map[string]any{"request_id": "", "output": map[string]any{
					"task_id": "relay/task 1", "task_status": "FAILED", "code": "UpstreamTaskFailed", "message": "InvalidParameter: bad image",
				}},
			}},
			{"unrecognized status", `{"code":200,"data":{"task_status":"WEIRD"}}`, map[string]any{
				"status": "UNKNOWN", "reason": "unrecognized relay status: WEIRD",
			}},
			{"query not successful", `{"code":10107,"message":"task not found"}`, map[string]any{
				"status": "UNKNOWN", "reason": "relay query was not successful",
			}},
			{"pending", `{"code":200,"data":{"task_status":"PENDING"}}`, map[string]any{
				"status": "QUEUED",
				"data":   map[string]any{"request_id": "", "output": map[string]any{"task_id": "relay/task 1", "task_status": "PENDING"}},
			}},
			{"canceled with a reason", `{"code":200,"data":{"task_status":"CANCELED","task_status_msg":"canceled by user"}}`, map[string]any{
				"status": "FAILURE", "reason": "canceled by user",
				"data": map[string]any{"request_id": "", "output": map[string]any{
					"task_id": "relay/task 1", "task_status": "CANCELED", "code": "UpstreamTaskFailed", "message": "canceled by user",
				}},
			}},
			{"failed without a reason", `{"code":200,"data":{"task_status":"FAILED"}}`, map[string]any{
				"status": "FAILURE", "reason": "task failed",
				"data": map[string]any{"request_id": "", "output": map[string]any{"task_id": "relay/task 1", "task_status": "FAILED", "code": "UpstreamTaskFailed"}},
			}},
			{"prototype member name is not a mapped status", `{"code":200,"data":{"task_status":"constructor"}}`, map[string]any{
				"status": "UNKNOWN", "reason": "unrecognized relay status: constructor",
			}},
			{"numeric string duration becomes a number", `{"code":200,"data":{"task_status":"COMPLETED","result":{"output":"https://cdn.example/v.mp4"},"usage":{"duration":"5","resolution":"720P"}}}`, map[string]any{
				"status": "SUCCESS", "url": "https://cdn.example/v.mp4",
				"data": map[string]any{
					"request_id": "",
					"output":     map[string]any{"task_id": "relay/task 1", "task_status": "SUCCEEDED", "video_url": "https://cdn.example/v.mp4"},
					"usage":      map[string]any{"duration": float64(5), "output_video_duration": float64(5), "input_video_duration": float64(0), "SR": "720P"},
				},
			}},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				value, err := plugin.Engine.Call(t.Context(), "parseTaskResult", queryCtx, relayConfigValue(t, tc.body), map[string]any{"status": 200, "headers": map[string]any{}})
				require.NoError(t, err)
				assert.Equal(t, tc.want, alibabaObject(t, value))
			})
		}
	})

	t.Run("snapshot usage settles output plus input seconds", func(t *testing.T) {
		value, err := plugin.Engine.Call(t.Context(), "extractUsageOnComplete", queryCtx, map[string]any{"status": "SUCCESS"}, map[string]any{
			"output": map[string]any{"task_status": "SUCCEEDED", "video_url": "https://cdn.example/v.mp4"},
			"usage":  map[string]any{"duration": 5, "output_video_duration": 5, "input_video_duration": 3, "SR": "720P"},
		})
		require.NoError(t, err)
		assert.Equal(t, map[string]any{"seconds": float64(8), "resolution": "720P"}, alibabaObject(t, value))
	})

	t.Run("without an inputSeconds mapping only output seconds settle", func(t *testing.T) {
		outputOnly := relayConfigValue(t, tokenPonyRelayConfig)
		outputOnly["relay"].(map[string]any)["query"].(map[string]any)["usage"] = map[string]any{"seconds": "data.usage.duration", "resolution": "data.usage.resolution"}
		ctx := maps.Clone(queryCtx)
		ctx["channelConfig"] = outputOnly
		body := relayConfigValue(t, `{"code":200,"data":{"task_status":"COMPLETED","result":{"output":"https://cdn.example/v.mp4"},"usage":{"duration":5,"resolution":"720P"}}}`)
		value, err := plugin.Engine.Call(t.Context(), "parseTaskResult", ctx, body, map[string]any{"status": 200, "headers": map[string]any{}})
		require.NoError(t, err)
		result := alibabaObject(t, value)
		require.Equal(t, "SUCCESS", result["status"])
		snapshot := result["data"].(map[string]any)
		assert.Equal(t, map[string]any{"duration": float64(5), "output_video_duration": float64(5), "input_video_duration": float64(0), "SR": "720P"}, snapshot["usage"])

		value, err = plugin.Engine.Call(t.Context(), "extractUsageOnComplete", ctx, map[string]any{"status": "SUCCESS"}, snapshot)
		require.NoError(t, err)
		assert.Equal(t, map[string]any{"seconds": float64(5), "resolution": "720P"}, alibabaObject(t, value))
	})

	t.Run("native task status presents the snapshot in DashScope form", func(t *testing.T) {
		value, err := plugin.Engine.CallPath(t.Context(), "native", []string{"taskStatus"}, map[string]any{}, map[string]any{
			"task_id": "task_public",
			"data":    map[string]any{"request_id": "", "output": map[string]any{"task_id": "relay-task-1", "task_status": "SUCCEEDED", "video_url": "https://cdn.example/v.mp4"}},
		})
		require.NoError(t, err)
		assert.Equal(t, map[string]any{"request_id": "", "output": map[string]any{
			"task_id": "task_public", "task_status": "SUCCEEDED", "video_url": "https://cdn.example/v.mp4",
		}}, alibabaObject(t, value))
	})
}

func TestAlibabaValidateChannelConfig(t *testing.T) {
	plugin := newAlibabaPlugin(t)
	validate := func(config map[string]any) error {
		_, err := plugin.Engine.Call(t.Context(), "validateChannelConfig", config)
		return err
	}
	require.NoError(t, validate(relayConfigValue(t, tokenPonyRelayConfig)))

	cases := []struct {
		name    string
		mutate  func(config, submit, query map[string]any)
		wantErr string
	}{
		{"missing relay", func(config, _, _ map[string]any) { delete(config, "relay") }, "relay must be an object"},
		{"unknown top-level field", func(config, _, _ map[string]any) { config["extra"] = 1 }, "unknown configuration field: extra"},
		{"relative submit path", func(_, submit, _ map[string]any) { submit["path"] = "v1/generate" }, "relay.submit.path must start with /"},
		{"dot segments in path", func(_, submit, _ map[string]any) { submit["path"] = "/v1/../generate" }, "must not contain"},
		{"GET submit", func(_, submit, _ map[string]any) { submit["method"] = "GET" }, "relay.submit.method must be POST"},
		{"forbidden header", func(_, submit, _ map[string]any) { submit["headers"] = map[string]any{"Host": "evil.example"} }, "cannot set header Host"},
		{"multi-line header", func(_, submit, _ map[string]any) { submit["headers"] = map[string]any{"X-Trace": "a\nb"} }, "must be a single-line string"},
		{"unknown request field", func(_, submit, _ map[string]any) {
			submit["body"] = map[string]any{"duration": "$.parameters.durtion"}
		}, "references unknown field $.parameters.durtion"},
		{"empty $each", func(_, submit, _ map[string]any) {
			submit["body"] = map[string]any{"media": map[string]any{"$each": "$.input.media"}}
		}, "needs at least one mapped field"},
		{"item reference outside $each", func(_, submit, _ map[string]any) { submit["body"] = map[string]any{"kind": "@.type"} }, "outside $each"},
		{"array leaf in values", func(_, submit, _ map[string]any) {
			submit["values"] = map[string]any{"media[]": map[string]any{"a": "b"}}
		}, "invalid path media[]"},
		{"missing task id path", func(_, submit, _ map[string]any) { delete(submit, "taskId") }, "relay.submit.taskId must be a dot-separated response path"},
		{"query without task id", func(_, _, query map[string]any) { query["path"] = "/v1/aigc/tasks" }, "must reference the task id"},
		{"unknown mapped status", func(_, _, query map[string]any) { query["statusMap"] = map[string]any{"DONE": "SUCCESS"} }, "relay.query.statusMap values must be one of PENDING, RUNNING, SUCCEEDED, FAILED, CANCELED (got DONE: SUCCESS)"},
		{"no failure status", func(_, _, query map[string]any) { query["statusMap"] = map[string]any{"DONE": "SUCCEEDED"} }, "must map a success status and a failure status"},
		{"missing video URL path", func(_, _, query map[string]any) { delete(query, "videoUrl") }, "relay.query.videoUrl must be a dot-separated response path"},
		{"query body with GET", func(_, _, query map[string]any) { query["body"] = map[string]any{"id": "$.task_id"} }, "relay.query.body requires method POST"},
		{"unknown usage field", func(_, _, query map[string]any) { query["usage"] = map[string]any{"cost": "data.cost"} }, "relay.query.usage has unknown field cost"},
		{"double slash in path", func(_, submit, _ map[string]any) { submit["path"] = "/v1//generate" }, "relay.submit.path must not contain .., //, #, spaces or control characters"},
		{"fragment in path", func(_, submit, _ map[string]any) { submit["path"] = "/v1/generate#top" }, "relay.submit.path must not contain .., //, #, spaces or control characters"},
		{"space in path", func(_, submit, _ map[string]any) { submit["path"] = "/v1/video generate" }, "relay.submit.path must not contain .., //, #, spaces or control characters"},
		{"control character in path", func(_, submit, _ map[string]any) { submit["path"] = "/v1/video\x7fgenerate" }, "relay.submit.path must not contain .., //, #, spaces or control characters"},
		{"overlong path", func(_, submit, _ map[string]any) { submit["path"] = "/" + strings.Repeat("a", 512) }, "relay.submit.path must not exceed 512 characters"},
		{"lowercase forbidden header", func(_, submit, _ map[string]any) { submit["headers"] = map[string]any{"content-length": "1"} }, "relay.submit.headers cannot set header content-length"},
		{"invalid header name", func(_, submit, _ map[string]any) { submit["headers"] = map[string]any{"Bad Header": "x"} }, "relay.submit.headers has an invalid header name Bad Header"},
		{"control character in header", func(_, submit, _ map[string]any) { submit["headers"] = map[string]any{"X-Trace": "a\x00b"} }, "relay.submit.headers.X-Trace must be a single-line string or null without control characters"},
		{"unsupported header placeholder", func(_, submit, _ map[string]any) {
			submit["headers"] = map[string]any{"Authorization": "Bearer {task_id}"}
		}, "relay.submit.headers.Authorization only supports the {key} placeholder"},
		{"nested $each", func(_, submit, _ map[string]any) {
			submit["body"] = map[string]any{"media": map[string]any{"$each": "$.input.media", "items": map[string]any{"$each": "$.input.media", "kind": "@.type"}}}
		}, "relay.submit.body.media.items.$each cannot be nested inside another $each"},
		{"$each as the whole body", func(_, submit, _ map[string]any) {
			submit["body"] = map[string]any{"$each": "$.input.media", "kind": "@.type"}
		}, "relay.submit.body cannot be a $each object"},
		{"$each over a scalar field", func(_, submit, _ map[string]any) {
			submit["body"] = map[string]any{"media": map[string]any{"$each": "$.model", "kind": "@.type"}}
		}, "relay.submit.body.media.$each must reference an array field"},
		{"task id only as a body key", func(_, _, query map[string]any) {
			query["path"], query["method"], query["body"] = "/v1/tasks/query", "POST", map[string]any{"$.task_id": 1}
		}, "relay.query must reference the task id with {task_id} in path or $.task_id in body"},
		{"object ok.equals", func(_, submit, _ map[string]any) {
			submit["ok"] = map[string]any{"path": "code", "equals": map[string]any{"value": 200}}
		}, "relay.submit.ok.equals must be a string, number or boolean"},
		{"bad video URL array element", func(_, _, query map[string]any) {
			query["videoUrl"] = []any{"data.result.output", "data/result"}
		}, "relay.query.videoUrl[1] must be a dot-separated response path"},
		{"POST query with the task id in the body", func(_, _, query map[string]any) {
			query["path"], query["method"], query["body"] = "/v1/tasks/query", "POST", map[string]any{"id": "$.task_id"}
		}, ""},
		{"{key} placeholder and removed header", func(_, submit, _ map[string]any) {
			submit["headers"] = map[string]any{"Authorization": "Token {key}", "Content-Type": nil}
		}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			config := relayConfigValue(t, tokenPonyRelayConfig)
			relay := config["relay"].(map[string]any)
			tc.mutate(config, relay["submit"].(map[string]any), relay["query"].(map[string]any))
			if tc.wantErr == "" {
				require.NoError(t, validate(config))
				return
			}
			require.ErrorContains(t, validate(config), tc.wantErr)
		})
	}

	// Members of a $each object sit one level below it for the validator and
	// the renderer alike, so every template the validator accepts renders.
	t.Run("validator and renderer share the template depth limit", func(t *testing.T) {
		nest := func(value any, levels int) any {
			for range levels {
				value = map[string]any{"n": value}
			}
			return value
		}
		eachAt := func(depth int) (config, ctx map[string]any) {
			config = relayConfigValue(t, tokenPonyRelayConfig)
			submit := config["relay"].(map[string]any)["submit"].(map[string]any)
			submit["body"] = nest(map[string]any{"$each": "$.input.media", "kind": "@.type"}, depth)
			delete(submit, "values")
			ctx = map[string]any{
				"model": "wan3.0-video", "upstreamModel": "wan3.0-video", "baseUrl": "https://relay.example", "apiKey": "rk", "channelConfig": config,
				"requestBody": map[string]any{"model": "wan3.0-video", "prompt": "a cat", "images": []any{"https://cdn.example/first.png", "https://cdn.example/last.png"}},
			}
			return config, ctx
		}

		config, ctx := eachAt(15)
		require.NoError(t, validate(config))
		value, err := plugin.Engine.Call(t.Context(), "buildSubmitRequest", ctx)
		require.NoError(t, err)
		assert.Equal(t, nest([]any{map[string]any{"kind": "first_frame"}, map[string]any{"kind": "last_frame"}}, 15), alibabaObject(t, value)["body"])

		config, ctx = eachAt(16)
		require.ErrorContains(t, validate(config), "relay.submit.body"+strings.Repeat(".n", 16)+".kind is nested too deeply")
		_, err = plugin.Engine.Call(t.Context(), "buildSubmitRequest", ctx)
		require.ErrorContains(t, err, "relay template is nested too deeply")
	})
}
