package controller

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateTaskPluginChannel(t *testing.T) {
	source := `
export const meta = {apiVersion: 1, key: "channel-validation", name: "Validation", version: "1.0.0", author: {name: "Test"}, models: ["doc"], fetchMode: "per_task"};
export function buildSubmitRequest() { return {}; }
export function parseSubmitResponse() { return {}; }
export function buildQueryRequest() { return {}; }
export function parseTaskResult() { return {}; }
`
	_, err := jsplugin.DefaultRegistry.Register(source, jsplugin.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { jsplugin.DefaultRegistry.Unregister("channel-validation") })
	baseURL := "https://example.com"

	channel := &model.Channel{Type: constant.ChannelTypeTaskPlugin, BaseURL: &baseURL}
	require.ErrorContains(t, validateChannel(channel, false), "task plugin key is required")

	missing := `{"task_plugin_key":"missing"}`
	channel.Setting = &missing
	require.ErrorContains(t, validateChannel(channel, false), "is not registered")

	longKey := `{"task_plugin_key":"` + strings.Repeat("x", 31) + `"}`
	channel.Setting = &longKey
	require.ErrorContains(t, validateChannel(channel, false), "must not exceed 30")

	valid := `{"task_plugin_key":"channel-validation"}`
	channel.Setting = &valid
	channel.BaseURL = nil
	require.ErrorContains(t, validateChannel(channel, false), "base URL is required")
}

func TestValidateTaskPluginChannelFillsPluginDefaultBaseURL(t *testing.T) {
	source := `
export const meta = {apiVersion: 1, key: "channel-default-url", name: "Default URL", version: "1.0.0", author: {name: "Test"}, baseUrl: "http://127.0.0.1:8000/", models: ["doc"], fetchMode: "per_task"};
export function buildSubmitRequest() { return {}; }
export function parseSubmitResponse() { return {}; }
export function buildQueryRequest() { return {}; }
export function parseTaskResult() { return {}; }
`
	_, err := jsplugin.DefaultRegistry.Register(source, jsplugin.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { jsplugin.DefaultRegistry.Unregister("channel-default-url") })
	bound := `{"task_plugin_key":"channel-default-url"}`

	empty := "  "
	for _, baseURL := range []*string{nil, &empty} {
		channel := &model.Channel{Type: constant.ChannelTypeTaskPlugin, Key: "sk", Setting: &bound, BaseURL: baseURL}
		require.NoError(t, validateChannel(channel, true))
		require.NotNil(t, channel.BaseURL)
		assert.Equal(t, "http://127.0.0.1:8000", *channel.BaseURL, "normalized plugin default is persisted onto the channel")
	}

	explicit := "https://override.example.com"
	channel := &model.Channel{Type: constant.ChannelTypeTaskPlugin, Setting: &bound, BaseURL: &explicit}
	require.NoError(t, validateChannel(channel, false))
	assert.Equal(t, explicit, *channel.BaseURL, "an administrator value is never replaced by the plugin default")
}

func TestValidateNewAPIChannelTaskPluginExtensions(t *testing.T) {
	// gateway-ext-vendor never declares new_api, so it can only run against
	// its vendor and must be refused on a New API channel.
	for key, upstreams := range map[string]string{"gateway-ext-a": `upstreams: ["vendor", "new_api"],`, "gateway-ext-b": `upstreams: ["new_api"],`, "gateway-ext-vendor": ""} {
		source := `
export const meta = {apiVersion: 1, key: "` + key + `", name: "Gateway", version: "1.0.0", author: {name: "Test"}, models: ["doc-` + key + `"], fetchMode: "per_task", ` + upstreams + `};
export function buildSubmitRequest() { return {}; }
export function parseSubmitResponse() { return {}; }
export function buildQueryRequest() { return {}; }
export function parseTaskResult() { return {}; }
`
		_, err := jsplugin.DefaultRegistry.Register(source, jsplugin.Options{})
		require.NoError(t, err)
		t.Cleanup(func() { jsplugin.DefaultRegistry.Unregister(key) })
	}
	baseURL := "https://gateway.example"
	cases := []struct {
		name, setting, wantErr string
	}{
		{"single and extension keys", `{"task_plugin_key":"gateway-ext-a","task_extend_plugin_keys":["gateway-ext-b"]}`, ""},
		{"extension keys only", `{"task_extend_plugin_keys":["gateway-ext-a","gateway-ext-b"]}`, ""},
		{"unregistered", `{"task_extend_plugin_keys":["gateway-ext-a","missing"]}`, `task plugin "missing" is not registered`},
		{"vendor-only plugin as extension", `{"task_extend_plugin_keys":["gateway-ext-a","gateway-ext-vendor"]}`, `task plugin "gateway-ext-vendor" does not support a New API upstream`},
		{"vendor-only plugin as single key", `{"task_plugin_key":"gateway-ext-vendor"}`, `task plugin "gateway-ext-vendor" does not support a New API upstream`},
		{"duplicate", `{"task_extend_plugin_keys":["gateway-ext-a","gateway-ext-a"]}`, "bound more than once"},
		{"duplicate of single key", `{"task_plugin_key":"gateway-ext-a","task_extend_plugin_keys":["gateway-ext-a"]}`, "bound more than once"},
		{"padded", `{"task_extend_plugin_keys":[" gateway-ext-a"]}`, "is invalid"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setting := tc.setting
			channel := &model.Channel{Type: constant.ChannelTypeNewAPI, BaseURL: &baseURL, Setting: &setting}
			err := validateChannel(channel, false)
			if tc.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tc.wantErr)
		})
	}

	setting := `{"task_extend_plugin_keys":["gateway-ext-a"]}`
	channel := &model.Channel{Type: constant.ChannelTypeOpenAI, Setting: &setting}
	require.ErrorContains(t, validateChannel(channel, false), "only supported on New API channels")
}

func TestValidateChannelTaskPluginConfig(t *testing.T) {
	register := func(key, hooks string) {
		source := `
export const meta = {apiVersion: 1, key: "` + key + `", name: "Config", version: "1.0.0", author: {name: "Test"}, models: ["doc-` + key + `"], fetchMode: "per_task"};
export function buildSubmitRequest() { return {}; }
export function parseSubmitResponse() { return {}; }
export function buildQueryRequest() { return {}; }
export function parseTaskResult() { return {}; }
` + hooks
		_, err := jsplugin.DefaultRegistry.Register(source, jsplugin.Options{})
		require.NoError(t, err)
		t.Cleanup(func() { jsplugin.DefaultRegistry.Unregister(key) })
	}
	register("config-hook", `export function validateChannelConfig(config) { if (!config.relay) throw new Error("relay is required"); }`)
	register("config-no-hook", "")
	register("config-not-callable", "export const validateChannelConfig = 1;")
	baseURL := "https://relay.example"

	cases := []struct {
		name        string
		channelType int
		setting     string
		wantErr     string
		wantKey     string
	}{
		{"no configuration", constant.ChannelTypeTaskPlugin, `{"task_plugin_key":"config-hook"}`, "", ""},
		{"null configuration", constant.ChannelTypeTaskPlugin, `{"task_plugin_key":"config-hook","task_plugin_config":null}`, "", ""},
		{"accepted by the plugin", constant.ChannelTypeTaskPlugin, `{"task_plugin_key":"config-hook","task_plugin_config":{"relay":{}}}`, "", ""},
		{"rejected by the plugin", constant.ChannelTypeTaskPlugin, `{"task_plugin_key":"config-hook","task_plugin_config":{"other":1}}`, "Invalid task plugin configuration: relay is required", "Invalid task plugin configuration: {{error}}"},
		{"not an object", constant.ChannelTypeTaskPlugin, `{"task_plugin_key":"config-hook","task_plugin_config":[1]}`, "must be a JSON object", "Task plugin configuration must be a JSON object"},
		{"plugin without the hook", constant.ChannelTypeTaskPlugin, `{"task_plugin_key":"config-no-hook","task_plugin_config":{"relay":{}}}`, "does not accept a channel configuration", "Task plugin {{key}} does not accept a channel configuration"},
		// A hook that cannot run is not a verdict on the configuration.
		{"hook that is not callable", constant.ChannelTypeTaskPlugin, `{"task_plugin_key":"config-not-callable","task_plugin_config":{"relay":{}}}`, "could not validate the configuration", "Task plugin {{key}} could not validate the configuration, please try again later"},
		{"New API channel", constant.ChannelTypeNewAPI, `{"task_plugin_config":{"relay":{}}}`, "not supported on New API channels", "Task plugin configuration is not supported on New API channels"},
		{"channel type without a plugin", constant.ChannelTypeAnthropic, `{"task_plugin_config":{"relay":{}}}`, "does not run a task plugin", "Channel type {{type}} does not run a task plugin, so a task plugin configuration is not supported"},
		{"oversized", constant.ChannelTypeTaskPlugin, `{"task_plugin_key":"config-hook","task_plugin_config":{"relay":{},"pad":"` + strings.Repeat("x", 33<<10) + `"}}`, "must not exceed 32 KiB", "Task plugin configuration must not exceed {{size}} KiB"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setting := tc.setting
			channel := &model.Channel{Type: tc.channelType, BaseURL: &baseURL, Setting: &setting}
			err := validateChannel(channel, false)
			if tc.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tc.wantErr)
			var message *common.Message
			require.ErrorAs(t, err, &message)
			assert.Equal(t, tc.wantKey, message.Key)
		})
	}

	// validateChannel rejects an unregistered binding before this check, so
	// the check is called directly.
	_, _, err := validateChannelTaskPluginConfig(context.Background(), constant.ChannelTypeTaskPlugin, dto.ChannelSettings{TaskPluginKey: "missing-plugin", TaskPluginConfig: json.RawMessage(`{"relay":{}}`)})
	var message *common.Message
	require.ErrorAs(t, err, &message)
	assert.Equal(t, "Task plugin {{key}} is not registered", message.Key)
	assert.Equal(t, map[string]any{"key": "missing-plugin"}, message.Params)
}

func TestValidateChannelAcceptsAlibabaRelayConfig(t *testing.T) {
	config := `{"relay":{"submit":{"path":"/v1/aigc/video/generate","body":{"model":"$.model","prompt":"$.input.prompt"},"taskId":"data.id"},` +
		`"query":{"path":"/v1/aigc/tasks/{task_id}","status":"data.task_status","statusMap":{"COMPLETED":"SUCCEEDED","FAILED":"FAILED"},"videoUrl":"data.result.output"}}}`
	baseURL := "https://api.relay.example"
	for _, channelType := range []int{constant.ChannelTypeTaskPlugin, constant.ChannelTypeAli} {
		setting := `{"task_plugin_key":"alibaba","task_plugin_config":` + config + `}`
		if channelType == constant.ChannelTypeAli {
			setting = `{"task_plugin_config":` + config + `}`
		}
		channel := &model.Channel{Type: channelType, BaseURL: &baseURL, Setting: &setting}
		require.NoError(t, validateChannel(channel, false), "channel type %d", channelType)
	}

	broken := `{"task_plugin_key":"alibaba","task_plugin_config":{"relay":{"submit":{"path":"/x"}}}}`
	channel := &model.Channel{Type: constant.ChannelTypeTaskPlugin, BaseURL: &baseURL, Setting: &broken}
	require.ErrorContains(t, validateChannel(channel, false), "Invalid task plugin configuration: relay.submit.body must be an object")
}

const previewRelayConfig = `{"relay":{"submit":{"path":"/v1/aigc/video/generate","body":{"model":"$.model","prompt":"$.input.prompt","resolution":"$.parameters.resolution","ratio":"$.parameters.ratio","duration":"$.parameters.duration"},"ok":{"path":"code","equals":200},"taskId":"data.id","error":"message"},` +
	`"query":{"path":"/v1/aigc/tasks/{task_id}","ok":{"path":"code","equals":200},"status":"data.task_status","statusMap":{"RUNNING":"RUNNING","COMPLETED":"SUCCEEDED","FAILED":"FAILED"},"videoUrl":"data.result.output","usage":{"seconds":"data.usage.duration","resolution":"data.usage.resolution"}}}}`

func runTaskPluginConfigPreview(t *testing.T, payload string) (bool, string, string, map[string]any) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/channel/task_plugin_config/preview", strings.NewReader(payload))
	c.Request.Header.Set("Content-Type", "application/json")
	PreviewChannelTaskPluginConfig(c)
	var response struct {
		Success    bool           `json:"success"`
		Message    string         `json:"message"`
		MessageKey string         `json:"message_key"`
		Data       map[string]any `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	return response.Success, response.Message, response.MessageKey, response.Data
}

func TestPreviewChannelTaskPluginConfigRunsRelayHooksOffline(t *testing.T) {
	success, message, _, data := runTaskPluginConfigPreview(t, `{
	  "type": 61, "task_plugin_key": "alibaba", "base_url": "https://relay.example/", "config": `+previewRelayConfig+`,
	  "model": "wan3.0-video", "entry": "openai_video",
	  "request": {"model": "wan3.0-video", "prompt": "a cat", "seconds": 5, "resolution": "720P"},
	  "submit_response": {"code": 200, "data": {"id": "relay-task-1"}},
	  "query_response": {"code": 200, "data": {"task_status": "COMPLETED", "result": {"output": "https://cdn.example/v.mp4"}, "usage": {"duration": 5, "resolution": "720P"}}}
	}`)
	require.True(t, success, message)

	submit := data["submit"].(map[string]any)
	assert.Equal(t, "https://relay.example/v1/aigc/video/generate", submit["url"])
	assert.Equal(t, "Bearer sk-****", submit["headers"].(map[string]any)["Authorization"], "the channel key never appears in a preview")
	assert.Equal(t, map[string]any{"model": "wan3.0-video", "prompt": "a cat", "resolution": "720P", "ratio": "adaptive", "duration": float64(5)}, submit["body"])
	assert.Equal(t, map[string]any{"seconds": float64(5), "resolution": "720P"}, data["reservation"])
	assert.Equal(t, "relay-task-1", data["submit_result"].(map[string]any)["task_id"])
	assert.Equal(t, "https://relay.example/v1/aigc/tasks/relay-task-1", data["query"].(map[string]any)["url"])
	assert.Equal(t, "Bearer sk-****", data["query"].(map[string]any)["headers"].(map[string]any)["Authorization"], "the channel key never appears in a preview")
	queryResult := data["query_result"].(map[string]any)
	assert.Equal(t, "SUCCESS", queryResult["status"])
	assert.Equal(t, "https://cdn.example/v.mp4", queryResult["url"])
	assert.Equal(t, "SUCCEEDED", queryResult["snapshot"].(map[string]any)["output"].(map[string]any)["task_status"])
	assert.Equal(t, map[string]any{"seconds": float64(5), "resolution": "720P"}, data["settlement"])
	assert.Empty(t, data["errors"])
}

func TestPreviewChannelTaskPluginConfigNativeEntryAndStepErrors(t *testing.T) {
	success, message, _, data := runTaskPluginConfigPreview(t, `{
	  "type": 17, "base_url": "https://relay.example", "config": `+previewRelayConfig+`,
	  "model": "wan3.0-video", "entry": "/ali/api/v1/services/aigc/video-generation/video-synthesis",
	  "request": {"model": "wan3.0-video", "input": {"prompt": "a dog"}, "parameters": {"resolution": "720P", "duration": 5}},
	  "submit_response": {"code": 10108, "message": "bad duration"}
	}`)
	require.True(t, success, message)
	assert.Equal(t, "a dog", data["submit"].(map[string]any)["body"].(map[string]any)["prompt"])
	assert.Contains(t, data["errors"].(map[string]any)["submit_result"], "bad duration")
	assert.NotContains(t, data, "query_result")

	success, message, _, _ = runTaskPluginConfigPreview(t, `{"type": 61, "task_plugin_key": "alibaba", "config": {"relay": {}}, "model": "wan3.0-video", "entry": "openai_video"}`)
	assert.False(t, success)
	assert.Contains(t, message, "Invalid task plugin configuration")
}

func TestPreviewChannelTaskPluginConfigStepsAreIndependent(t *testing.T) {
	const channel = `"type": 61, "task_plugin_key": "alibaba", "base_url": "https://relay.example", "model": "wan3.0-video", "config": ` + previewRelayConfig
	const completed = `{"code": 200, "data": {"task_status": "COMPLETED", "result": {"output": "https://cdn.example/v.mp4"}, "usage": {"duration": 5, "resolution": "720P"}}}`

	// An entry the plugin does not serve fails only the submit step.
	success, message, _, data := runTaskPluginConfigPreview(t, `{`+channel+`, "entry": "/not/a/route", "request": {"model": "wan3.0-video"},
	  "submit_response": {"code": 200, "data": {"id": "relay-task-1"}}}`)
	require.True(t, success, message)
	assert.NotContains(t, data, "submit")
	assert.Contains(t, data["errors"].(map[string]any)["submit"], "neither openai_video nor a native submit route")
	assert.Equal(t, "relay-task-1", data["submit_result"].(map[string]any)["task_id"])

	// Without a submit response the query step asks for preview-task-id.
	success, message, _, data = runTaskPluginConfigPreview(t, `{`+channel+`, "query_response": `+completed+`}`)
	require.True(t, success, message)
	assert.Equal(t, "https://relay.example/v1/aigc/tasks/preview-task-id", data["query"].(map[string]any)["url"])

	// A null request is absent, and a submit response that yields no task id
	// leaves the query step on preview-task-id.
	success, message, _, data = runTaskPluginConfigPreview(t, `{`+channel+`, "entry": "openai_video", "request": null,
	  "submit_response": {"code": 10108, "message": "bad duration"}, "query_response": `+completed+`}`)
	require.True(t, success, message)
	failures := data["errors"].(map[string]any)
	assert.NotContains(t, data, "submit")
	assert.NotContains(t, failures, "submit")
	assert.Contains(t, failures["submit_result"], "bad duration")
	assert.Equal(t, "https://relay.example/v1/aigc/tasks/preview-task-id", data["query"].(map[string]any)["url"])
	assert.Equal(t, "SUCCESS", data["query_result"].(map[string]any)["status"])
}

func TestPreviewChannelTaskPluginConfigReportsEveryHookFailure(t *testing.T) {
	source := `
export const meta = {apiVersion: 1, key: "preview-hooks", name: "Preview", version: "1.0.0", author: {name: "Test"}, models: ["preview-hooks-model"], fetchMode: "per_task", protocols: ["openai_video"]};
export const protocols = {openai_video: {
  decodeRequest: function(ctx) { return {kind: "submit", model: ctx.model, requestBody: ctx.body.value}; },
  render: function() { return {}; }
}};
export function validateChannelConfig() {}
export function buildSubmitRequest(ctx) { return {url: (ctx.channelConfig.submitBase || ctx.baseUrl) + "/submit", method: "POST", body: ctx.requestBody}; }
export function extractUsage() { throw new Error("no reservation facts"); }
export function parseSubmitResponse(ctx, response) { return response.body.result; }
export function buildQueryRequest(ctx) { return {url: (ctx.channelConfig.queryBase || ctx.baseUrl) + "/tasks/" + ctx.taskId, method: "GET"}; }
export function parseTaskResult(ctx, body) { return {status: "SUCCESS", url: body.url, data: body}; }
export function extractUsageOnComplete() { throw new Error("no completion facts"); }
export function listArtifacts() { return []; }
export function buildContentRequest() { return {}; }
`
	_, err := jsplugin.DefaultRegistry.Register(source, jsplugin.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { jsplugin.DefaultRegistry.Unregister("preview-hooks") })
	const channel = `"type": 61, "task_plugin_key": "preview-hooks", "base_url": "https://relay.example", "model": "preview-hooks-model", "entry": "openai_video"`
	const queryResponse = `"query_response": {"url": "https://cdn.example/v.mp4"}`

	// A failing usage hook keeps what its step built.
	success, message, _, data := runTaskPluginConfigPreview(t, `{`+channel+`, "config": {}, "request": {"prompt": "a cat"}, "submit_response": {"result": {}}, `+queryResponse+`}`)
	require.True(t, success, message)
	assert.Equal(t, map[string]any{"url": "https://relay.example/submit", "method": "POST", "body": map[string]any{"prompt": "a cat"}}, data["submit"])
	assert.NotContains(t, data, "reservation")
	assert.NotContains(t, data, "submit_result")
	assert.Equal(t, map[string]any{"url": "https://relay.example/tasks/preview-task-id", "method": "GET"}, data["query"])
	assert.Equal(t, map[string]any{"status": "SUCCESS", "url": "https://cdn.example/v.mp4", "reason": nil, "snapshot": map[string]any{"url": "https://cdn.example/v.mp4"}}, data["query_result"])
	assert.NotContains(t, data, "settlement")
	assert.Equal(t, map[string]any{
		"submit":        "extractUsage failed: no reservation facts",
		"submit_result": "parseSubmitResponse returned no task id",
		"query_result":  "extractUsageOnComplete failed: no completion facts",
	}, data["errors"])

	// The runtime decodes taskId into a string and refuses a blank one, so a
	// non-object result, a blank id and a numeric id all yield no task id.
	for _, parsed := range []string{`"task-1"`, `{"taskId": " "}`, `{"taskId": 7}`, `null`} {
		success, message, _, data = runTaskPluginConfigPreview(t, `{`+channel+`, "config": {}, "submit_response": {"result": `+parsed+`}}`)
		require.True(t, success, message)
		assert.NotContains(t, data, "submit_result", parsed)
		assert.Equal(t, map[string]any{"submit_result": "parseSubmitResponse returned no task id"}, data["errors"], parsed)
	}

	// A request to a host the runtime would refuse is shown and reported, and
	// the query response is then not parsed.
	success, message, _, data = runTaskPluginConfigPreview(t, `{`+channel+`, "config": {"submitBase": "https://elsewhere.example", "queryBase": "https://elsewhere.example"},
	  "request": {"prompt": "a cat"}, `+queryResponse+`}`)
	require.True(t, success, message)
	assert.Equal(t, map[string]any{"url": "https://elsewhere.example/submit", "method": "POST", "body": map[string]any{"prompt": "a cat"}}, data["submit"])
	assert.Equal(t, map[string]any{"url": "https://elsewhere.example/tasks/preview-task-id", "method": "GET"}, data["query"])
	assert.NotContains(t, data, "query_result")
	assert.Equal(t, map[string]any{
		"submit":       `plugin request host "elsewhere.example" is not allowed`,
		"query_result": `plugin request host "elsewhere.example" is not allowed`,
	}, data["errors"])
}

func TestPreviewChannelTaskPluginConfigRejectsUnusableInput(t *testing.T) {
	cases := []struct {
		name    string
		payload string
		wantKey string
	}{
		{"malformed request", `{"type": 61,`, "Invalid preview request"},
		{"part over 1 MiB", `{"type": 61, "request": {"prompt": "` + strings.Repeat("x", 1<<20) + `"}}`, "Preview input must not exceed 1 MiB"},
		// No part is oversized here; only the bound on the whole body stops it.
		{"body over the bound", `{"type": 61, "padding": "` + strings.Repeat("x", 4<<20) + `"}`, "Preview input must not exceed 1 MiB"},
		{"no configuration", `{"type": 61, "task_plugin_key": "alibaba", "config": null, "model": "wan3.0-video"}`, "Task plugin configuration is empty"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			success, _, messageKey, _ := runTaskPluginConfigPreview(t, tc.payload)
			assert.False(t, success)
			assert.Equal(t, tc.wantKey, messageKey)
		})
	}
}
