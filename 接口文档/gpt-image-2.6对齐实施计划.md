# gpt-image-2.6 对齐实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 不开透传时也能原样转发 gpt-image-2.6 的新参数（svg、complexity、GIF 参数、markup、safety_identifier），并把响应里的 `svg_usage` 按描图模型的价格计费、记日志、在新版前端展示。

**Architecture:** 请求侧先用 `git am` 打上游 PR #8208 的补丁，再补 `safety_identifier`；`relaykit` 增加 `ImageSvgUsage` 和 `ImageRequest.SvgAuthorModel()`。OpenAI 生图响应处理把 `svg_usage` 存到 `RelayInfo.SvgUsage`；`ModelPriceHelper` 在发上游前检查客户端指定的描图模型有没有价格；结算时 `service` 按描图模型自己的定价（表达式 → 按次价 → 倍率）算出描图费用，加到三条计价路径上，并写进消费日志。新版前端在费用列加标记、在日志详情里展示描图明细。

**Tech Stack:** Go 1.26（gin、testify、shopspring/decimal、gjson）、relaykit 独立模块（Go 1.25.1）、React 19 + TypeScript + vitest + Testing Library、Bun。

**Spec:** `接口文档/gpt-image-2.6对齐设计.md`

## Global Constraints

- 开始 Task 3、4、5、6、7 之前，必须用读文件工具完整读一遍 `.agents/rules/billing.md`（AGENTS.md 的计费强制阅读规则），并遵守其中所有规则。
- root 模块里的 JSON 编解码一律用 `common.Marshal` / `common.Unmarshal` / `common.UnmarshalJsonStr` / `common.DecodeJson`；`relaykit/` 里用 `kitutil.*`。`json.RawMessage` 这类类型可以直接引用。
- `relaykit/` 必须能独立编译：`cd relaykit && GOWORK=off go build ./...`。
- 新增或修改的 Go 测试使用 testify 的 `require` / `assert`，优先扩展现有测试文件；改过的 Go 文件跑 `gofmt -w`。
- 额度换算只用 `common.QuotaFromDecimalChecked` / `common.QuotaFromDecimal` 等现有函数，不写裸 `int(...)` 转换；封顶事件用 `noteQuotaClamp` 记录。
- 写给控制台的日志内容用 `common.NewMessage`，新键追加到 7 个前端语言文件末尾（Task 8 统一加）；不要在真实语言文件上跑 `bun run i18n:sync`，它会改写受保护的署名键。
- 服务端日志行用英文经 `common.LogText` 输出，中文写进 `common/log_text.zh-CN.json`（该文件按键的字节序排序）。
- 前端改动前读 `web/AGENTS.md`；界面文字一律 `t()`；数字格式化用 `@/lib/format` 并经 `toIntlLocale`；新代码不解构 props；改完跑 `bun run typecheck`、改动文件的 lint 和受影响的 vitest。
- 不在 `docs/` 下新建文件；`接口文档/` 下的设计和计划不提交。
- 在本地分支 `feat/gpt-image-2.6-svg` 上每完成一个任务提交一次，不推送。
- 不向真实上游发请求，除非用户明确同意（会产生费用）。

## File Map

| 文件 | 改动 | 职责 |
|---|---|---|
| `relaykit/dto/openai_image.go` | 改 | 新请求字段（补丁 + `SafetyIdentifier`）、`SvgAuthorModel()`、`ImageSvgUsage` |
| `relaykit/dto/openai_request_zero_value_test.go` | 改 | 请求字段往返、`SvgAuthorModel()` 测试 |
| `relay/channel/openai/adaptor.go` | 改（补丁） | 表单版 edits 转发 `markup` 文件 |
| `relay/channel/openai/image_edit_test.go` | 改（补丁） | 表单版 edits 测试 |
| `relay/convert_request_error_test.go` | 改 | `ImageHelper` 原样转发 2.6 字段的端到端测试 |
| `relay/common/relay_info.go` | 改 | `RelayInfo.SvgUsage` |
| `relay/image_handler.go` | 改 | 每次尝试开头清空 `SvgUsage` |
| `relay/channel/openai/relay_image.go` | 改 | 读取 `svg_usage`；SVG、GIF 输出不采样检测 |
| `relay/channel/openai/image_stream_test.go` | 改 | 上面两项的测试 |
| `relay/helper/price.go` | 改 | 描图模型价格预检 |
| `relay/helper/price_test.go` | 改 | 预检测试 |
| `service/svg_author_billing.go` | 新建 | 描图费用计算 |
| `service/text_quota.go` | 改 | 描图费用加进总价、写日志 |
| `service/text_quota_test.go` | 改 | 计费和日志测试 |
| `common/log_text.zh-CN.json` | 改 | 告警日志的中文 |
| `setting/billing_setting/builtin_billing.go` | 改 | 2.6 内置价格 |
| `setting/billing_setting/builtin_billing_test.go` | 改 | 内置价格测试 |
| `web/src/features/usage-logs/types.ts` | 改 | `svg_author` 类型 |
| `web/src/features/usage-logs/lib/format.ts` | 改 | `hasSvgAuthorCharge` |
| `web/src/features/usage-logs/components/log-cost-display.tsx` | 改 | 费用标记组件化，新增描图标记 |
| `web/src/features/usage-logs/components/dialogs/details-dialog.tsx` | 改 | 计费详情三行 |
| `web/src/features/usage-logs/lib/__tests__/svg-author.test.ts` | 新建 | `hasSvgAuthorCharge` 测试 |
| `web/src/features/usage-logs/components/__tests__/cost-display.test.tsx` | 改 | 标记测试 |
| `web/src/features/usage-logs/components/__tests__/usage-facts.test.tsx` | 改 | 详情三行测试 |
| `web/src/i18n/locales/{en,zh,zh-TW,fr,ru,ja,vi}.json` | 改 | 5 个新键 |

---

### Task 0: 准备分支

- [ ] **Step 1: 建分支**

```bash
cd /d/Project/new-api
git status --short        # 只应有未跟踪的 接口文档/ 内容
git switch -c feat/gpt-image-2.6-svg
```

Expected: `Switched to a new branch 'feat/gpt-image-2.6-svg'`

---

### Task 1: 打上游 PR #8208 的补丁

上游提交 3374f251e 给 `ImageRequest` 加了 `complexity`、`markup`、`svg`、`frame_count`、`frame_duration_ms`，并让表单版 edits 转发 `markup` 文件，附带两个测试。用 `git am` 保留原作者和提交信息，以后合并上游时同一处改动不会冲突。

**Files:**
- Modify（补丁）: `relaykit/dto/openai_image.go`、`relaykit/dto/openai_request_zero_value_test.go`、`relay/channel/openai/adaptor.go`、`relay/channel/openai/image_edit_test.go`

**Interfaces:**
- Produces: `dto.ImageRequest` 的字段 `Complexity`、`Markup`、`Svg`、`FrameCount`、`FrameDurationMs`（都是 `json.RawMessage`，`omitempty`）；测试 `TestImageRequestPreserveGPTImage26Fields`。

- [ ] **Step 1: 下载补丁（GitHub 需要走本机代理）**

```bash
curl -sSL -x http://127.0.0.1:10808 -o /tmp/8208.patch \
  https://github.com/QuantumNous/new-api/commit/3374f251e3a84eebfd7f37d4746d7bb017e4f4b1.patch
head -5 /tmp/8208.patch
```

Expected: 第一行是 `From 3374f251e3a84eebfd7f37d4746d7bb017e4f4b1 ...`，第二行 `From: feitianbubu <feitianbubu@qq.com>`。

- [ ] **Step 2: 应用补丁**

```bash
git am -3 /tmp/8208.patch
git log -1 --format='%an | %s'
```

Expected: `feitianbubu | feat: forward image complexity and markup for gpt-image-2.6 (#8208)`。如果 `git am` 冲突，先 `git am --abort`，停下来报告，不要手工改。

- [ ] **Step 3: 跑补丁自带的测试和 relaykit 独立编译**

```bash
(cd relaykit && GOWORK=off go build ./... && GOWORK=off go test ./dto/ -run TestImageRequestPreserveGPTImage26Fields -v)
go test ./relay/channel/openai/ -run TestConvertImageEditRequestMultipart -v
```

Expected: 两个测试都 `PASS`，编译没有输出。

补丁本身就是一次提交，这个任务不需要另外提交。

---

### Task 2: `safety_identifier`、`SvgAuthorModel()`、`ImageSvgUsage`

**Files:**
- Modify: `relaykit/dto/openai_image.go`
- Test: `relaykit/dto/openai_request_zero_value_test.go`、`relay/convert_request_error_test.go`

**Interfaces:**
- Consumes: Task 1 的 `ImageRequest.Svg` 等字段。
- Produces:
  - `ImageRequest.SafetyIdentifier json.RawMessage`（`json:"safety_identifier,omitempty"`）
  - `func (i *ImageRequest) SvgAuthorModel() string`：返回去掉首尾空白的 `svg.model`；没有 `svg`、`svg` 为 null、`model` 不是字符串时返回 `""`。
  - `type ImageSvgUsage struct { Model string; InputTokens, OutputTokens, TotalTokens, ReasoningTokens int }`，JSON 键 `model`、`input_tokens`、`output_tokens`、`total_tokens`、`reasoning_tokens`。

- [ ] **Step 1: 写失败的测试**

`relaykit/dto/openai_request_zero_value_test.go`：把 Task 1 带来的 `TestImageRequestPreserveGPTImage26Fields` 里的 `raw` 换成下面这行（末尾加了 `safety_identifier`）：

```go
	raw := []byte(`{"model":"gpt-image-2.6-flare","prompt":"p","complexity":"low","markup":{"image_url":"https://x/o.png"},"svg":{"model":"m"},"frame_count":2,"frame_duration_ms":0,"safety_identifier":"user-hash"}`)
```

同一文件末尾追加：

```go
// The request names the SVG author model in svg.model; anything else means
// the client did not choose one.
func TestImageRequestSvgAuthorModel(t *testing.T) {
	for _, tc := range []struct {
		name, raw, want string
	}{
		{"named author", `{"model":"gpt-image-2.6-flare","prompt":"p","svg":{"model":" gpt-6-astra ","reasoning":{"effort":"low"}}}`, "gpt-6-astra"},
		{"no svg object", `{"model":"gpt-image-2.6-flare","prompt":"p"}`, ""},
		{"null svg object", `{"model":"gpt-image-2.6-flare","prompt":"p","svg":null}`, ""},
		{"model is not a string", `{"model":"gpt-image-2.6-flare","prompt":"p","svg":{"model":1}}`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var req ImageRequest
			require.NoError(t, kitutil.Unmarshal([]byte(tc.raw), &req))
			assert.Equal(t, tc.want, req.SvgAuthorModel())
		})
	}
}
```

`relay/convert_request_error_test.go` 末尾追加（复用文件里已有的 `imageReservation`，imports 已经齐全）：

```go
// The image relay re-encodes the request when the channel does not pass the
// body through. Every gpt-image-2.6 field must survive that, including an
// explicit zero.
func TestImageRequestForwardsGPTImage26Fields(t *testing.T) {
	service.InitHttpClient()
	received := make(chan []byte, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err == nil {
			received <- body
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(w, `{"error":{"message":"fixture upstream failure","type":"upstream_error"}}`)
	}))
	t.Cleanup(upstream.Close)

	const body = `{"model":"gpt-image-2.6-flare","prompt":"bakery logo","n":1,"output_format":"svg",` +
		`"svg":{"model":"gpt-6-astra","reasoning":{"effort":"low"}},"complexity":"low",` +
		`"frame_count":0,"frame_duration_ms":200,"markup":{"image_url":"https://example.com/m.png"},` +
		`"safety_identifier":"user-hash"}`
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeOpenAI)
	common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, upstream.URL)
	common.SetContextKey(c, constant.ContextKeyChannelSetting, dto.ChannelSettings{})
	request, err := helper.GetAndValidOpenAIImageRequest(c, relayconstant.RelayModeImagesGenerations)
	require.NoError(t, err)
	info := &relaycommon.RelayInfo{Request: request, OriginModelName: "gpt-image-2.6-flare", RelayMode: relayconstant.RelayModeImagesGenerations,
		RequestURLPath: c.Request.URL.Path, Billing: &imageReservation{held: 20000, limit: 500000},
		PriceData: hosttypes.PriceData{UsePrice: true, ModelPrice: 0.04, GroupRatioInfo: hosttypes.GroupRatioInfo{GroupRatio: 1}},
	}

	apiErr := ImageHelper(c, info)

	require.NotNil(t, apiErr)
	require.Len(t, received, 1)
	forwarded := <-received
	for path, want := range map[string]string{
		"output_format":     `"svg"`,
		"svg":               `{"model":"gpt-6-astra","reasoning":{"effort":"low"}}`,
		"complexity":        `"low"`,
		"frame_count":       `0`,
		"frame_duration_ms": `200`,
		"markup":            `{"image_url":"https://example.com/m.png"}`,
		"safety_identifier": `"user-hash"`,
	} {
		assert.Equal(t, want, gjson.GetBytes(forwarded, path).Raw, path)
	}
}
```

- [ ] **Step 2: 确认测试失败**

```bash
(cd relaykit && GOWORK=off go test ./dto/ -run 'TestImageRequestPreserveGPTImage26Fields|TestImageRequestSvgAuthorModel' -v)
go test ./relay/ -run TestImageRequestForwardsGPTImage26Fields -v
```

Expected:
- relaykit 编译失败，提示 `req.SvgAuthorModel undefined`。
- relay 测试 FAIL，只有 `safety_identifier` 一项不相等（expected `"user-hash"`，actual 空串）；其余字段已由 Task 1 修好。

- [ ] **Step 3: 实现**

`relaykit/dto/openai_image.go`：

1. import 加上 `"strings"`。
2. 结构体里在 `FrameDurationMs` 下面加一行：

```go
	FrameDurationMs   json.RawMessage `json:"frame_duration_ms,omitempty"`
	SafetyIdentifier  json.RawMessage `json:"safety_identifier,omitempty"`
```

3. 在 `SetModelName` 方法后面加：

```go
// SvgAuthorModel returns svg.model, the model the client chose to author an
// SVG output, or "" when the request names none.
func (i *ImageRequest) SvgAuthorModel() string {
	var svg struct {
		Model string `json:"model"`
	}
	if len(i.Svg) == 0 || kitutil.Unmarshal(i.Svg, &svg) != nil {
		return ""
	}
	return strings.TrimSpace(svg.Model)
}
```

4. 在文件末尾 `ImageData` 后面加：

```go
// ImageSvgUsage is svg_usage, the token usage of the model that authored an
// SVG output. OpenAI reports it beside the image model's usage and bills it at
// the author model's rates.
type ImageSvgUsage struct {
	Model           string `json:"model"`
	InputTokens     int    `json:"input_tokens"`
	OutputTokens    int    `json:"output_tokens"`
	TotalTokens     int    `json:"total_tokens"`
	ReasoningTokens int    `json:"reasoning_tokens"`
}
```

- [ ] **Step 4: 确认测试通过**

```bash
gofmt -w relaykit/dto/openai_image.go relaykit/dto/openai_request_zero_value_test.go relay/convert_request_error_test.go
(cd relaykit && GOWORK=off go build ./... && GOWORK=off go test ./dto/ -v -run 'TestImageRequest')
go test ./relay/ -run 'TestImageRequestForwardsGPTImage26Fields|TestImageRequestReservesFinalQuantityBeforeUpstream' -v
```

Expected: 全部 PASS。

- [ ] **Step 5: 提交**

```bash
git add relaykit/dto/openai_image.go relaykit/dto/openai_request_zero_value_test.go relay/convert_request_error_test.go
git commit -m "feat(image): forward safety_identifier and read the SVG author model"
```

---

### Task 3: 读取 `svg_usage`；SVG、GIF 输出不采样

**Files:**
- Modify: `relay/common/relay_info.go`（`RelayInfo` 结构体）
- Modify: `relay/image_handler.go`（`ImageHelper` 开头）
- Modify: `relay/channel/openai/relay_image.go`
- Modify: `common/log_text.zh-CN.json`
- Test: `relay/channel/openai/image_stream_test.go`、`relay/convert_request_error_test.go`

**Interfaces:**
- Consumes: `dto.ImageSvgUsage`（Task 2）。
- Produces: `RelayInfo.SvgUsage *dto.ImageSvgUsage`。`OpenaiImageHandler` 和 `openaiImageJSONAsStreamHandler` 在响应里有合法 `svg_usage` 时写入；`ImageHelper` 每次尝试开头置 `nil`。

- [ ] **Step 1: 写失败的测试**

`relay/channel/openai/image_stream_test.go`：import 里加 `"encoding/json"`，文件末尾追加：

```go
func TestOpenaiImageHandlersCaptureSvgUsage(t *testing.T) {
	const svgUsage = `{"model":"gpt-6-astra","input_tokens":1200,"output_tokens":800,"total_tokens":2000,"reasoning_tokens":300}`
	want := &dto.ImageSvgUsage{Model: "gpt-6-astra", InputTokens: 1200, OutputTokens: 800, TotalTokens: 2000, ReasoningTokens: 300}
	for _, tc := range []struct {
		name, body string
		stream     bool
		want       *dto.ImageSvgUsage
	}{
		{"JSON response", `{"data":[{"b64_json":"PHN2Zz4="}],"output_format":"svg","svg_usage":` + svgUsage + `}`, false, want},
		{"JSON wrapped as SSE", `{"data":[{"b64_json":"PHN2Zz4="}],"output_format":"svg","svg_usage":` + svgUsage + `}`, true, want},
		{"response without svg_usage", `{"data":[{"b64_json":"aW1n"}]}`, false, nil},
		{"null svg_usage", `{"data":[{"b64_json":"aW1n"}],"svg_usage":null}`, false, nil},
		{"malformed svg_usage", `{"data":[{"b64_json":"aW1n"}],"svg_usage":{"input_tokens":"many"}}`, false, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _, resp, info := newImageTestContext(t, tc.body, "application/json", tc.stream)
			if tc.stream {
				_, err := OpenaiImageStreamHandler(c, info, resp)
				require.Nil(t, err)
			} else {
				_, err := OpenaiImageHandler(c, info, resp)
				require.Nil(t, err)
			}
			assert.Equal(t, tc.want, info.SvgUsage)
		})
	}
}

// SynthID reads only raster PNG, JPEG and WebP images, so a sampled SVG or GIF
// would only ever record a detection error.
func TestSampleImagePayloadSkipsVectorAndAnimatedOutput(t *testing.T) {
	for _, tc := range []struct {
		name, body, requestFormat string
		wantSampled               bool
	}{
		{"svg in response", `{"output_format":"svg","data":[{"b64_json":"PHN2Zz4="}]}`, "", false},
		{"gif in response", `{"output_format":"gif","data":[{"b64_json":"R0lGODlh"}]}`, "", false},
		{"svg only in request", `{"data":[{"b64_json":"PHN2Zz4="}]}`, `"svg"`, false},
		{"png in response", `{"output_format":"png","data":[{"b64_json":"iVBORw=="}]}`, "", true},
		{"format stated nowhere", `{"data":[{"b64_json":"iVBORw=="}]}`, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			info := &relaycommon.RelayInfo{Request: &dto.ImageRequest{OutputFormat: json.RawMessage(tc.requestFormat)}}

			sampleImagePayload(info, []byte(tc.body))

			assert.Equal(t, tc.wantSampled, len(info.SampledImageBytes) > 0)
		})
	}
}
```

`relay/convert_request_error_test.go`：扩展 `TestImageRequestReservesFinalQuantityBeforeUpstream` 的重试用例。

把

```go
				info.BillingImageCount = common.GetPointer(3)
			}
```

改成

```go
				info.BillingImageCount = common.GetPointer(3)
				info.SvgUsage = &dto.ImageSvgUsage{Model: "previous-attempt", InputTokens: 1}
			}
```

把

```go
			assert.Nil(t, info.BillingImageCount)
```

改成

```go
			assert.Nil(t, info.BillingImageCount)
			assert.Nil(t, info.SvgUsage)
```

- [ ] **Step 2: 确认测试失败**

```bash
go test ./relay/channel/openai/ -run 'TestOpenaiImageHandlersCaptureSvgUsage|TestSampleImagePayloadSkipsVectorAndAnimatedOutput' 2>&1 | head -20
```

Expected: 编译失败，提示 `info.SvgUsage undefined`。

- [ ] **Step 3: 实现**

`relay/common/relay_info.go`，在 `ImageQuotaBeforeGroup float64` 后面加：

```go
	ImageRequestCount     int
	ImageQuotaBeforeGroup float64
	// SvgUsage is the upstream's svg_usage, the token usage of the model that
	// authored an SVG output. Settlement bills it at that model's own rates.
	// Reset before each channel attempt.
	SvgUsage *dto.ImageSvgUsage
```

`relay/image_handler.go`，`ImageHelper` 开头：

```go
	info.BillingImageCount = nil
	info.ImageRequestCount = 0
	info.SvgUsage = nil
```

`relay/channel/openai/relay_image.go`：

1. `OpenaiImageHandler` 里，在 `info.UpdateImageCount(openaiImageResponseCount(responseBody))` 下面加一行：

```go
	info.UpdateImageCount(openaiImageResponseCount(responseBody))
	captureImageSvgUsage(c, info, responseBody)
	sampleImagePayload(info, responseBody) // passive detection capture (first image only)
```

2. `openaiImageJSONAsStreamHandler` 里同样加一行：

```go
	info.UpdateImageCount(openaiImageResponseCount(responseBody))
	captureImageSvgUsage(c, info, responseBody)

	helper.SetEventStreamHeaders(c)
```

3. 在 `openaiImageDataHasField` 函数后面加：

```go
// captureImageSvgUsage keeps svg_usage, the SVG author model's token usage, for
// settlement. A malformed object is reported because its tokens cannot be billed.
func captureImageSvgUsage(c *gin.Context, info *relaycommon.RelayInfo, responseBody []byte) {
	if info == nil {
		return
	}
	raw := gjson.GetBytes(responseBody, "svg_usage")
	if !raw.IsObject() {
		return
	}
	var usage dto.ImageSvgUsage
	if err := common.UnmarshalJsonStr(raw.Raw, &usage); err != nil {
		logger.LogWarn(c, common.LogText("invalid svg_usage in image response: %v", err))
		return
	}
	info.SvgUsage = &usage
}
```

4. `sampleImagePayload` 开头，在 `if info == nil { return }` 之后、`const maxBytes` 之前加：

```go
	// SynthID reads only raster PNG, JPEG and WebP images, so sampling an SVG
	// or GIF would only record a detection error.
	format := gjson.GetBytes(responseBody, "output_format").String()
	if request, ok := info.Request.(*dto.ImageRequest); ok && format == "" && len(request.OutputFormat) > 0 {
		_ = common.Unmarshal(request.OutputFormat, &format)
	}
	if format == "svg" || format == "gif" {
		return
	}
```

`common/log_text.zh-CN.json`，在 `"invalid response or response body": "上游响应为空或没有响应体",` 这一行下面加（文件按键的字节序排序）：

```json
  "invalid svg_usage in image response: %v": "图片响应里的 svg_usage 无效: %v",
```

- [ ] **Step 4: 确认测试通过**

```bash
gofmt -w relay/common/relay_info.go relay/image_handler.go relay/channel/openai/relay_image.go relay/channel/openai/image_stream_test.go relay/convert_request_error_test.go
go test ./relay/channel/openai/ ./relay/ ./i18n/
```

Expected: 全部 `ok`。

- [ ] **Step 5: 提交**

```bash
git add relay/common/relay_info.go relay/image_handler.go relay/channel/openai/relay_image.go relay/channel/openai/image_stream_test.go relay/convert_request_error_test.go common/log_text.zh-CN.json
git commit -m "feat(image): capture svg_usage and skip detection for SVG and GIF output"
```

---

### Task 4: 客户端指定的描图模型没配价时，发上游前拒绝

**Files:**
- Modify: `relay/helper/price.go`（`ModelPriceHelper` 开头）
- Test: `relay/helper/price_test.go`

**Interfaces:**
- Consumes: `(*dto.ImageRequest).SvgAuthorModel() string`（Task 2）；现有的 `HasModelBillingConfig(modelName string) bool`（自用模式下未配价的模型也返回 true）、`modelPriceNotConfiguredError(c, modelName, userId) error`。
- Produces: 无新符号。`ModelPriceHelper` 对未配价的描图模型返回错误；`relay/request_billing.go` 把它转成 400。

- [ ] **Step 1: 写失败的测试**

`relay/helper/price_test.go`：import 里加 `"encoding/json"` 和 `"github.com/QuantumNous/new-api/i18n"`，文件末尾追加：

```go
func TestModelPriceHelperRefusesUnpricedSvgAuthor(t *testing.T) {
	gin.SetMode(gin.TestMode)
	require.NoError(t, i18n.Init())
	savedPrices, savedRatios := ratio_setting.ModelPrice2JSONString(), ratio_setting.ModelRatio2JSONString()
	oldSelfUse := operation_setting.SelfUseModeEnabled
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(savedPrices))
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(savedRatios))
		operation_setting.SelfUseModeEnabled = oldSelfUse
	})
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"svg-image-model":0.04}`))
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"priced-author":2}`))

	for _, tc := range []struct {
		name, svg            string
		selfUse, acceptUnset bool
		wantRefused          bool
	}{
		{name: "unpriced author is refused", svg: `{"model":"unpriced-author"}`, wantRefused: true},
		{name: "priced author proceeds", svg: `{"model":"priced-author"}`},
		{name: "self-use mode proceeds", svg: `{"model":"unpriced-author"}`, selfUse: true},
		{name: "user accepting unpriced models proceeds", svg: `{"model":"unpriced-author"}`, acceptUnset: true},
		{name: "request naming no author proceeds", svg: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			operation_setting.SelfUseModeEnabled = tc.selfUse
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
			ctx.Set("group", "default")
			info := &relaycommon.RelayInfo{
				OriginModelName: "svg-image-model",
				UserGroup:       "default",
				UsingGroup:      "default",
				UserSetting:     dto.UserSetting{AcceptUnsetRatioModel: tc.acceptUnset},
				Request:         &dto.ImageRequest{Model: "svg-image-model", Prompt: "logo", Svg: json.RawMessage(tc.svg)},
			}

			_, err := ModelPriceHelper(ctx, info, 0, &types.TokenCountMeta{})

			if tc.wantRefused {
				require.ErrorContains(t, err, "unpriced-author")
				return
			}
			require.NoError(t, err)
		})
	}
}
```

- [ ] **Step 2: 确认测试失败**

```bash
go test ./relay/helper/ -run TestModelPriceHelperRefusesUnpricedSvgAuthor 2>&1 | tail -15
```

Expected: 子测试 `unpriced_author_is_refused` FAIL（`An error is expected but got nil`），其余 PASS。

- [ ] **Step 3: 实现**

`relay/helper/price.go`，`ModelPriceHelper` 里，在 `billingModelName := info.GetBillingModelName()` 这一行上面加：

```go
	// An SVG output is also billed at its author model's rates, so an author
	// named by the client without a price is refused before the upstream runs.
	if request, image := info.Request.(*dto.ImageRequest); image && !info.UserSetting.AcceptUnsetRatioModel {
		if author := request.SvgAuthorModel(); author != "" && !HasModelBillingConfig(author) {
			return hosttypes.PriceData{}, modelPriceNotConfiguredError(c, author, info.UserId)
		}
	}
```

- [ ] **Step 4: 确认测试通过**

```bash
gofmt -w relay/helper/price.go relay/helper/price_test.go
go test ./relay/helper/
```

Expected: `ok`。

- [ ] **Step 5: 提交**

```bash
git add relay/helper/price.go relay/helper/price_test.go
git commit -m "feat(image): refuse an unpriced SVG author model before the upstream call"
```

---

### Task 5: 按描图模型的价格算描图费用，加进总价

**Files:**
- Create: `service/svg_author_billing.go`
- Modify: `service/text_quota.go`（`textQuotaSummary`、`hasBillableUsage`、`composeTieredTextQuota`、`calculateTextQuotaSummary`）
- Test: `service/text_quota_test.go`

**Interfaces:**
- Consumes: `RelayInfo.SvgUsage`（Task 3）、`(*dto.ImageRequest).SvgAuthorModel()`（Task 2）。
- Produces（Task 6 使用）:
  - `type svgAuthorCharge struct { Model, PricedAs string; InputTokens, OutputTokens, ReasoningTokens int; Quota decimal.Decimal }`。`PricedAs` 为空表示没有价格可用。
  - `func calculateSvgAuthorCharge(relayInfo *relaycommon.RelayInfo) *svgAuthorCharge`：没有描图用量时返回 `nil`。
  - `textQuotaSummary.SvgAuthor *svgAuthorCharge`
  - `func (s *textQuotaSummary) additionalQuota() decimal.Decimal`：工具附加费 + 描图费用。
  - 测试夹具 `svgAuthorPricingFixture(t)`。

- [ ] **Step 1: 读计费规则**

用读文件工具完整读 `.agents/rules/billing.md`。

- [ ] **Step 2: 写失败的测试**

`service/text_quota_test.go`：import 里加 `"encoding/json"`、`"github.com/QuantumNous/new-api/setting/config"`、`"github.com/QuantumNous/new-api/setting/ratio_setting"`，文件末尾追加：

```go
// svgAuthorPricingFixture installs one author model per pricing mode and
// restores the global price tables afterwards.
func svgAuthorPricingFixture(t *testing.T) {
	t.Helper()
	saved := map[string]string{}
	require.NoError(t, config.GlobalConfig.SaveToDB(func(key, value string) error {
		saved[key] = value
		return nil
	}))
	savedPrices, savedRatios, savedCompletions := ratio_setting.ModelPrice2JSONString(), ratio_setting.ModelRatio2JSONString(), ratio_setting.CompletionRatio2JSONString()
	oldSelfUse := operation_setting.SelfUseModeEnabled
	t.Cleanup(func() {
		require.NoError(t, config.GlobalConfig.LoadFromDB(saved))
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(savedPrices))
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(savedRatios))
		require.NoError(t, ratio_setting.UpdateCompletionRatioByJSONString(savedCompletions))
		operation_setting.SelfUseModeEnabled = oldSelfUse
	})
	operation_setting.SelfUseModeEnabled = false
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		"billing_setting.billing_mode": `{"expr-author":"tiered_expr","fixed-author":"tiered_expr"}`,
		"billing_setting.billing_expr": `{"expr-author":"tier(\"base\", p * 2 + c * 10)","fixed-author":"tier(\"request\", fixed(0.01))"}`,
	}))
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"price-author":0.02,"negative-author":-0.02}`))
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"ratio-author":2}`))
	require.NoError(t, ratio_setting.UpdateCompletionRatioByJSONString(`{"ratio-author":3,"mystery-author":1}`))
}

func TestCalculateSvgAuthorChargeUsesTheAuthorModelsOwnPrice(t *testing.T) {
	svgAuthorPricingFixture(t)
	for _, tc := range []struct {
		name, reported, requested, wantPricedAs string
		selfUse, acceptUnset                    bool
		wantQuota                               int
	}{
		// 1000 input, 200 output, group ratio 1.5.
		{name: "token expression", reported: "expr-author", wantPricedAs: "expr-author", wantQuota: 3000},
		{name: "fixed expression charges once", reported: "fixed-author", wantPricedAs: "fixed-author", wantQuota: 7500},
		{name: "per-call price", reported: "price-author", wantPricedAs: "price-author", wantQuota: 15000},
		{name: "ratio", reported: "ratio-author", wantPricedAs: "ratio-author", wantQuota: 4800},
		{name: "reported name wins over the request", reported: "ratio-author", requested: "price-author", wantPricedAs: "ratio-author", wantQuota: 4800},
		{name: "unpriced snapshot name falls back to the request", reported: "ratio-author-2026-11-01", requested: "ratio-author", wantPricedAs: "ratio-author", wantQuota: 4800},
		{name: "nothing priced charges zero", reported: "mystery-author", requested: "other-author"},
		{name: "negative price is not usable", reported: "negative-author"},
		{name: "self-use mode pays the fallback ratio", reported: "mystery-author", selfUse: true, wantPricedAs: "mystery-author", wantQuota: 67500},
		{name: "accepted unpriced model pays the fallback ratio", reported: "mystery-author", acceptUnset: true, wantPricedAs: "mystery-author", wantQuota: 67500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			operation_setting.SelfUseModeEnabled = tc.selfUse
			info := &relaycommon.RelayInfo{
				SvgUsage:    &dto.ImageSvgUsage{Model: tc.reported, InputTokens: 1000, OutputTokens: 200, TotalTokens: 1200, ReasoningTokens: 50},
				UserSetting: dto.UserSetting{AcceptUnsetRatioModel: tc.acceptUnset},
				PriceData:   hosttypes.PriceData{GroupRatioInfo: hosttypes.GroupRatioInfo{GroupRatio: 1.5}},
			}
			if tc.requested != "" {
				info.Request = &dto.ImageRequest{Svg: json.RawMessage(`{"model":"` + tc.requested + `"}`)}
			}

			charge := calculateSvgAuthorCharge(info)

			require.NotNil(t, charge)
			assert.Equal(t, tc.reported, charge.Model)
			assert.Equal(t, tc.wantPricedAs, charge.PricedAs)
			assert.Equal(t, tc.wantQuota, common.QuotaFromDecimal(charge.Quota))
			assert.Equal(t, 50, charge.ReasoningTokens)
		})
	}
}

func TestCalculateSvgAuthorChargeBillableTokens(t *testing.T) {
	svgAuthorPricingFixture(t)
	for _, tc := range []struct {
		name                             string
		usage                            dto.ImageSvgUsage
		wantInput, wantOutput, wantQuota int
	}{
		// ratio-author: (input + output * 3) * 2, group ratio 1.
		{"output already includes reasoning", dto.ImageSvgUsage{InputTokens: 1000, OutputTokens: 500, TotalTokens: 1500, ReasoningTokens: 300}, 1000, 500, 5000},
		{"reasoning reported beside output", dto.ImageSvgUsage{InputTokens: 1000, OutputTokens: 200, TotalTokens: 1500, ReasoningTokens: 300}, 1000, 500, 5000},
		{"total below input keeps output", dto.ImageSvgUsage{InputTokens: 1000, OutputTokens: 200, TotalTokens: 10}, 1000, 200, 3200},
		{"negative counts become zero", dto.ImageSvgUsage{InputTokens: -5, OutputTokens: 10, TotalTokens: math.MinInt}, 0, 10, 60},
	} {
		t.Run(tc.name, func(t *testing.T) {
			usage := tc.usage
			usage.Model = "ratio-author"
			info := &relaycommon.RelayInfo{SvgUsage: &usage, PriceData: hosttypes.PriceData{GroupRatioInfo: hosttypes.GroupRatioInfo{GroupRatio: 1}}}

			charge := calculateSvgAuthorCharge(info)

			require.NotNil(t, charge)
			assert.Equal(t, tc.wantInput, charge.InputTokens)
			assert.Equal(t, tc.wantOutput, charge.OutputTokens)
			assert.Equal(t, tc.wantQuota, common.QuotaFromDecimal(charge.Quota))
		})
	}

	for _, usage := range []*dto.ImageSvgUsage{nil, {Model: "ratio-author"}, {Model: "ratio-author", InputTokens: -1, OutputTokens: -2, TotalTokens: -3}} {
		assert.Nil(t, calculateSvgAuthorCharge(&relaycommon.RelayInfo{SvgUsage: usage}))
	}

	negativeGroup := &relaycommon.RelayInfo{
		SvgUsage:  &dto.ImageSvgUsage{Model: "ratio-author", InputTokens: 1000},
		PriceData: hosttypes.PriceData{GroupRatioInfo: hosttypes.GroupRatioInfo{GroupRatio: -1}},
	}
	assert.True(t, calculateSvgAuthorCharge(negativeGroup).Quota.IsZero(), "a negative group ratio must not credit the user")
}

func TestCalculateTextQuotaSummaryAddsSvgAuthorOutsideImageMultipliers(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	svgAuthorPricingFixture(t)
	// ratio-author at group ratio 1: (1000 + 200 * 3) * 2 = 3200.
	svgUsage := &dto.ImageSvgUsage{Model: "ratio-author", InputTokens: 1000, OutputTokens: 200, TotalTokens: 1200}
	usage := &dto.Usage{PromptTokens: 100, CompletionTokens: 50, TotalTokens: 150}
	newInfo := func(priceData hosttypes.PriceData) *relaycommon.RelayInfo {
		return &relaycommon.RelayInfo{OriginModelName: "gpt-image-2.6-flare", StartTime: time.Now(), SvgUsage: svgUsage, PriceData: priceData}
	}

	t.Run("ratio pricing", func(t *testing.T) {
		info := newInfo(hosttypes.PriceData{ModelRatio: 1, CompletionRatio: 1, GroupRatioInfo: hosttypes.GroupRatioInfo{GroupRatio: 1}})
		assert.Equal(t, 3350, calculateTextQuotaSummary(ctx, info, usage).Quota)
	})

	t.Run("per-call pricing does not multiply the author by n", func(t *testing.T) {
		priceData := hosttypes.PriceData{ModelPrice: 0.04, UsePrice: true, GroupRatioInfo: hosttypes.GroupRatioInfo{GroupRatio: 1}}
		priceData.AddOtherRatio("n", 3)
		assert.Equal(t, 63200, calculateTextQuotaSummary(ctx, newInfo(priceData), usage).Quota)
	})

	t.Run("expression pricing", func(t *testing.T) {
		info := newInfo(hosttypes.PriceData{ModelRatio: 1, CompletionRatio: 1, GroupRatioInfo: hosttypes.GroupRatioInfo{GroupRatio: 1}})
		info.TieredBillingSnapshot = &billingexpr.BillingSnapshot{BillingMode: "tiered_expr", GroupRatio: 1}
		summary := calculateTextQuotaSummary(ctx, info, usage)
		assert.Equal(t, 4200, composeTieredTextQuota(info, summary, 1000, &billingexpr.TieredResult{ActualQuotaBeforeGroup: 1000, ActualQuotaAfterGroup: 1000}))
		assert.Equal(t, 5200, composeTieredTextQuota(info, summary, 2000, nil))
	})

	t.Run("author tokens alone are billable", func(t *testing.T) {
		info := newInfo(hosttypes.PriceData{GroupRatioInfo: hosttypes.GroupRatioInfo{GroupRatio: 1}})
		assert.Equal(t, 3200, calculateTextQuotaSummary(ctx, info, &dto.Usage{}).Quota)
	})

	t.Run("oversized usage saturates and is audited", func(t *testing.T) {
		info := newInfo(hosttypes.PriceData{ModelRatio: 1, CompletionRatio: 1, GroupRatioInfo: hosttypes.GroupRatioInfo{GroupRatio: 1}})
		info.SvgUsage = &dto.ImageSvgUsage{Model: "ratio-author", InputTokens: math.MaxInt, OutputTokens: math.MaxInt}
		assert.Equal(t, common.MaxQuota, calculateTextQuotaSummary(ctx, info, usage).Quota)
		require.NotNil(t, info.QuotaClamp)
		assert.Equal(t, common.QuotaClampOverflow, info.QuotaClamp.Kind)
	})
}
```

- [ ] **Step 3: 确认测试失败**

```bash
go test ./service/ -run 'SvgAuthor' 2>&1 | head -20
```

Expected: 编译失败，`undefined: calculateSvgAuthorCharge`。

- [ ] **Step 4: 新建 `service/svg_author_billing.go`**

```go
package service

import (
	"math"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/shopspring/decimal"
)

// svgAuthorCharge is what the model that authored an SVG output costs on top of
// the image model. OpenAI bills those tokens at the author model's own rates.
type svgAuthorCharge struct {
	Model string
	// PricedAs names the model whose price was applied, and is empty when no
	// price was available and the tokens went unbilled.
	PricedAs        string
	InputTokens     int
	OutputTokens    int
	ReasoningTokens int
	Quota           decimal.Decimal
}

// calculateSvgAuthorCharge prices the upstream's svg_usage at the group ratio of
// the image request. It returns nil when the response reported no author tokens.
func calculateSvgAuthorCharge(relayInfo *relaycommon.RelayInfo) *svgAuthorCharge {
	usage := relayInfo.SvgUsage
	if usage == nil {
		return nil
	}
	input := max(usage.InputTokens, 0)
	// OpenAI does not say whether output_tokens includes reasoning_tokens. If it
	// does, total minus input equals output; if not, total minus input is larger.
	output := max(usage.OutputTokens, max(usage.TotalTokens, 0)-input, 0)
	if input == 0 && output == 0 {
		return nil
	}

	requested := ""
	if request, ok := relayInfo.Request.(*dto.ImageRequest); ok {
		requested = request.SvgAuthorModel()
	}
	reported := strings.TrimSpace(usage.Model)
	charge := &svgAuthorCharge{
		Model:           common.GetStringIfEmpty(reported, requested),
		InputTokens:     input,
		OutputTokens:    output,
		ReasoningTokens: max(usage.ReasoningTokens, 0),
	}
	groupRatio := decimal.NewFromFloat(relayInfo.PriceData.GroupRatioInfo.GroupRatio)
	for _, name := range []string{reported, requested} {
		if quota, ok := svgAuthorQuotaBeforeGroup(name, input, output); ok {
			charge.PricedAs = name
			charge.Quota = quota.Mul(groupRatio)
			break
		}
	}
	// Mirror a direct call to an unpriced model: a self-use site or a user who
	// accepted unpriced models pays the fallback ratio.
	if charge.PricedAs == "" && charge.Model != "" && (operation_setting.SelfUseModeEnabled || relayInfo.UserSetting.AcceptUnsetRatioModel) {
		fallbackRatio, _, _ := ratio_setting.GetModelRatio(charge.Model)
		charge.PricedAs = charge.Model
		charge.Quota = svgAuthorRatioQuota(charge.Model, fallbackRatio, input, output).Mul(groupRatio)
	}
	// A negative group ratio must not turn the author's tokens into a credit.
	if charge.Quota.IsNegative() {
		charge.Quota = decimal.Zero
	}
	return charge
}

// svgAuthorQuotaBeforeGroup prices the tokens the way a direct call to the model
// is priced: expression, then per-call price, then ratio. The second result is
// false when the name has no usable price; a negative price is not usable.
func svgAuthorQuotaBeforeGroup(name string, input, output int) (decimal.Decimal, bool) {
	if name == "" {
		return decimal.Zero, false
	}
	if billing_setting.GetBillingMode(name) == billing_setting.BillingModeTieredExpr {
		if expression, ok := billing_setting.GetBillingExpr(name); ok {
			// The author call is not a client request, so request-dependent rules
			// evaluate against an empty context.
			result, err := billingexpr.ComputeTieredQuota(&billingexpr.BillingSnapshot{
				ExprString:   expression,
				ExprHash:     billingexpr.ExprHashString(expression),
				GroupRatio:   1,
				QuotaPerUnit: common.QuotaPerUnit,
				ExprVersion:  billingexpr.ExprVersion(expression),
			}, billingexpr.TokenParams{P: float64(input), C: float64(output), Len: float64(input)})
			quota := result.ActualQuotaBeforeGroup
			if err != nil || math.IsNaN(quota) || math.IsInf(quota, 0) || quota < 0 {
				return decimal.Zero, false
			}
			return decimal.NewFromFloat(quota), true
		}
	}
	var quota decimal.Decimal
	if price, ok := ratio_setting.GetModelPrice(name, false); ok {
		quota = decimal.NewFromFloat(price).Mul(decimal.NewFromFloat(common.QuotaPerUnit))
	} else if ratio_setting.HasConfiguredModelRatio(name) {
		modelRatio, _, _ := ratio_setting.GetModelRatio(name)
		quota = svgAuthorRatioQuota(name, modelRatio, input, output)
	} else {
		return decimal.Zero, false
	}
	return quota, !quota.IsNegative()
}

func svgAuthorRatioQuota(name string, modelRatio float64, input, output int) decimal.Decimal {
	return decimal.NewFromInt(int64(input)).
		Add(decimal.NewFromInt(int64(output)).Mul(decimal.NewFromFloat(ratio_setting.GetCompletionRatio(name)))).
		Mul(decimal.NewFromFloat(modelRatio))
}
```

- [ ] **Step 5: 改 `service/text_quota.go`**

1. `textQuotaSummary` 结构体，在 `ToolCallSurchargeQuota decimal.Decimal` 下面加一行：

```go
	ToolCallSurchargeQuota decimal.Decimal
	SvgAuthor              *svgAuthorCharge
```

2. 把 `hasBillableUsage` 换成下面两个方法：

```go
// hasBillableUsage reports whether this request should incur any charge.
// A request can carry zero tokens yet still be billable via a tool-call
// surcharge (e.g. /v1/alpha/search returns no usage but bills one web_search
// call), so token count alone is not sufficient to decide.
func (s *textQuotaSummary) hasBillableUsage() bool {
	return s.FixedPriceBilling || s.TotalTokens > 0 || !s.additionalQuota().IsZero()
}

// additionalQuota is charged on top of the model's own price and outside its
// request multipliers: tool-call surcharges and an SVG author's tokens.
func (s *textQuotaSummary) additionalQuota() decimal.Decimal {
	if s.SvgAuthor == nil {
		return s.ToolCallSurchargeQuota
	}
	return s.ToolCallSurchargeQuota.Add(s.SvgAuthor.Quota)
}
```

3. 把 `composeTieredTextQuota` 换成：

```go
func composeTieredTextQuota(relayInfo *relaycommon.RelayInfo, summary textQuotaSummary, tieredQuota int, tieredResult *billingexpr.TieredResult) int {
	additionalQuota := summary.additionalQuota()
	if additionalQuota.IsZero() {
		return tieredQuota
	}

	if tieredResult != nil {
		if snap := relayInfo.TieredBillingSnapshot; snap != nil {
			quota, clamp := common.QuotaFromDecimalChecked(decimal.NewFromFloat(tieredResult.ActualQuotaBeforeGroup).
				Mul(decimal.NewFromFloat(snap.GroupRatio)).
				Add(additionalQuota))
			noteQuotaClamp(relayInfo, clamp)
			return quota
		}
	}

	// Saturate the final sum, not just the surcharge: tieredQuota can be near
	// MaxQuota and adding the surcharge could push the total past the
	// single-request quota policy bound.
	total, clamp := common.QuotaFromDecimalChecked(
		decimal.NewFromInt(int64(tieredQuota)).Add(additionalQuota),
	)
	noteQuotaClamp(relayInfo, clamp)
	return total
}
```

4. `calculateTextQuotaSummary` 里，在 `summary.ToolCallSurchargeQuota = calculateTextToolCallSurcharge(ctx, relayInfo, &summary)` 下面加一行：

```go
	summary.ToolCallSurchargeQuota = calculateTextToolCallSurcharge(ctx, relayInfo, &summary)
	summary.SvgAuthor = calculateSvgAuthorCharge(relayInfo)
```

5. 同一个函数里有两处（倍率分支、按次价分支）：

```go
		quotaCalculateDecimal = quotaCalculateDecimal.Add(summary.ToolCallSurchargeQuota)
```

都改成：

```go
		quotaCalculateDecimal = quotaCalculateDecimal.Add(summary.additionalQuota())
```

- [ ] **Step 6: 确认测试通过**

```bash
gofmt -w service/svg_author_billing.go service/text_quota.go service/text_quota_test.go
go test ./service/ -run 'SvgAuthor|ComposeTieredTextQuota|CalculateTextQuotaSummary|CalculateTextToolCallSurcharge' -v 2>&1 | tail -30
go test ./service/
```

Expected: 全部 PASS，最后一行 `ok`。

- [ ] **Step 7: 提交**

```bash
git add service/svg_author_billing.go service/text_quota.go service/text_quota_test.go
git commit -m "feat(billing): charge SVG author tokens at the author model's price"
```

---

### Task 6: 消费日志记录描图费用

**Files:**
- Modify: `service/svg_author_billing.go`、`service/text_quota.go`（`PostTextConsumeQuota`）、`common/log_text.zh-CN.json`
- Test: `service/text_quota_test.go`

**Interfaces:**
- Consumes: `svgAuthorCharge`、`textQuotaSummary.SvgAuthor`、`svgAuthorPricingFixture`（Task 5）。
- Produces: 消费日志字段（Task 8 的前端使用）：
  - 日志内容消息键 `SVG author {{model}}: {{input}} input tokens, {{output}} output tokens, cost {{quota}}`
  - `other.svg_author`：`{model, priced_as, input_tokens, output_tokens, reasoning_tokens, quota}`（`quota` 是四舍五入后的整数额度）
  - `other.admin_info.svg_author_unpriced`：未配价的模型名

- [ ] **Step 1: 读计费规则**

用读文件工具完整读 `.agents/rules/billing.md`。

- [ ] **Step 2: 写失败的测试**

`service/text_quota_test.go` 末尾追加：

```go
func TestPostTextConsumeQuotaRecordsSvgAuthorCharge(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svgAuthorPricingFixture(t)
	for index, tc := range []struct {
		name, author, wantPricedAs string
		wantQuota                  int
		wantAuthorQuota            float64
		wantUnpriced               any
	}{
		{name: "priced author", author: "ratio-author", wantPricedAs: "ratio-author", wantQuota: 3350, wantAuthorQuota: 3200},
		{name: "unpriced author is reported to administrators", author: "mystery-author", wantQuota: 150, wantUnpriced: "mystery-author"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			truncate(t)
			userID, channelID := 63+index, 63+index
			seedUser(t, userID, 1_000_000)
			seedChannel(t, channelID)
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest("POST", "/v1/images/generations", nil)
			info := &relaycommon.RelayInfo{
				UserId:          userID,
				UserQuota:       1_000_000,
				IsPlayground:    true,
				OriginModelName: "gpt-image-2.6-flare",
				UsingGroup:      "default",
				StartTime:       time.Now(),
				ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: channelID},
				PriceData:       hosttypes.PriceData{ModelRatio: 1, CompletionRatio: 1, GroupRatioInfo: hosttypes.GroupRatioInfo{GroupRatio: 1}},
				SvgUsage:        &dto.ImageSvgUsage{Model: tc.author, InputTokens: 1000, OutputTokens: 200, TotalTokens: 1200, ReasoningTokens: 50},
			}

			PostTextConsumeQuota(ctx, info, &dto.Usage{PromptTokens: 100, CompletionTokens: 50, TotalTokens: 150}, nil)

			log := getLastLog(t)
			require.NotNil(t, log)
			assert.Equal(t, tc.wantQuota, log.Quota)
			assert.Contains(t, log.Content, "SVG author "+tc.author+": 1000 input tokens, 200 output tokens, cost ")
			var other map[string]any
			require.NoError(t, common.UnmarshalJsonStr(log.Other, &other))
			assert.Equal(t, map[string]any{
				"model":            tc.author,
				"priced_as":        tc.wantPricedAs,
				"input_tokens":     float64(1000),
				"output_tokens":    float64(200),
				"reasoning_tokens": float64(50),
				"quota":            tc.wantAuthorQuota,
			}, other["svg_author"])
			adminInfo, ok := other["admin_info"].(map[string]any)
			require.True(t, ok)
			assert.Equal(t, tc.wantUnpriced, adminInfo["svg_author_unpriced"])
		})
	}
}
```

- [ ] **Step 3: 确认测试失败**

```bash
go test ./service/ -run TestPostTextConsumeQuotaRecordsSvgAuthorCharge 2>&1 | tail -20
```

Expected: 两个子测试都 FAIL，`log.Content` 不含 `SVG author`（费用断言已经通过）。

- [ ] **Step 4: 实现**

`service/svg_author_billing.go`：import 加 `"github.com/QuantumNous/new-api/logger"`、`"github.com/QuantumNous/new-api/model"`、`"github.com/gin-gonic/gin"`，文件末尾追加：

```go
// appendSvgAuthorLogInfo records the author's tokens and cost on the consume
// log. An author without a price is also reported to administrators, because
// its tokens went unbilled.
func appendSvgAuthorLogInfo(ctx *gin.Context, other *model.LogOther, author *svgAuthorCharge) {
	if author == nil {
		return
	}
	other.SetPublic("svg_author", map[string]any{
		"model":            author.Model,
		"priced_as":        author.PricedAs,
		"input_tokens":     author.InputTokens,
		"output_tokens":    author.OutputTokens,
		"reasoning_tokens": author.ReasoningTokens,
		"quota":            common.QuotaFromDecimal(author.Quota),
	})
	if author.PricedAs == "" {
		other.SetAdmin("svg_author_unpriced", author.Model)
		logger.LogWarn(ctx, common.LogText("SVG author model %s could not be priced; its tokens were not charged", author.Model))
	}
}
```

`service/text_quota.go`，`PostTextConsumeQuota` 里：

1. 在 `for _, item := range summary.ToolSurchargeItems { ... }` 循环结束之后加：

```go
	if author := summary.SvgAuthor; author != nil {
		extraContent = append(extraContent, common.NewMessage("SVG author {{model}}: {{input}} input tokens, {{output}} output tokens, cost {{quota}}", map[string]any{
			"model":  author.Model,
			"input":  author.InputTokens,
			"output": author.OutputTokens,
			"quota":  logger.FormatQuota(common.QuotaFromDecimal(author.Quota)),
		}))
	}
```

2. 在 `appendToolSurchargeLogInfo(other, summary.ToolSurchargeItems)` 下面加一行：

```go
	appendToolSurchargeLogInfo(other, summary.ToolSurchargeItems)
	appendSvgAuthorLogInfo(ctx, other, summary.SvgAuthor)
```

`common/log_text.zh-CN.json`，在 `"SSE ping ticker stopped": "SSE ping ticker 已停止",` 这一行下面加：

```json
  "SVG author model %s could not be priced; its tokens were not charged": "SVG 描图模型 %s 没有配置价格，描图 token 未计费",
```

- [ ] **Step 5: 确认测试通过**

```bash
gofmt -w service/svg_author_billing.go service/text_quota.go service/text_quota_test.go
go test ./service/ ./i18n/
```

Expected: 全部 `ok`。

- [ ] **Step 6: 提交**

```bash
git add service/svg_author_billing.go service/text_quota.go service/text_quota_test.go common/log_text.zh-CN.json
git commit -m "feat(billing): record the SVG author's tokens and cost on the consume log"
```

---

### Task 7: 2.6 内置价格和 OpenAI 渠道默认模型

**Files:**
- Modify: `setting/billing_setting/builtin_billing.go`、`relay/channel/openai/constant.go`
- Test: `setting/billing_setting/builtin_billing_test.go`

**Interfaces:**
- Produces: `billing_setting.GetBillingExpr` 对 `gpt-image-2.6-sunburst`、`gpt-image-2.6-flare` 返回内置表达式；管理员已配置的倍率或价格仍然优先（`GetBillingMode` 的现有逻辑）。

- [ ] **Step 1: 读计费规则**

用读文件工具完整读 `.agents/rules/billing.md`。

- [ ] **Step 2: 写失败的测试**

`setting/billing_setting/builtin_billing_test.go`，`TestImageModelBuiltinPricesAndOverrides` 的模型列表改成：

```go
	for _, name := range []string{"gpt-image-2", "gpt-image-2.5-sunburst", "gpt-image-2.5-flare", "gpt-image-2.6-sunburst", "gpt-image-2.6-flare"} {
```

- [ ] **Step 3: 确认测试失败**

```bash
go test ./setting/billing_setting/ -run TestImageModelBuiltinPricesAndOverrides 2>&1 | tail -12
```

Expected: 子测试 `gpt-image-2.6-sunburst`、`gpt-image-2.6-flare` FAIL（`require.True` 失败），其余 PASS。

- [ ] **Step 4: 实现**

`setting/billing_setting/builtin_billing.go`，把图片模型这段改成：

```go
	// https://developers.openai.com/api/docs/pricing (Standard, 2026-09-09; 2.6 models 2026-11-10).
	// The Images API reports image output in output_tokens, normalized to c.
	"gpt-image-2":            `tier("standard", p * 5 + cr * 1.25 + img * 8 + img_cr * 2 + c * 30)`,
	"gpt-image-2.5-sunburst": `tier("standard", p * 5 + cr * 1.25 + img * 8 + img_cr * 2 + c * 30)`,
	"gpt-image-2.5-flare":    `tier("standard", p * 5 + cr * 1.25 + img * 8 + img_cr * 2 + c * 30)`,
	"gpt-image-2.6-sunburst": `tier("standard", p * 5 + cr * 1.25 + img * 8 + img_cr * 2 + c * 30)`,
	"gpt-image-2.6-flare":    `tier("standard", p * 5 + cr * 1.25 + img * 8 + img_cr * 2 + c * 30)`,
```

`relay/channel/openai/constant.go`，把

```go
	"gpt-image-1", "gpt-image-1-mini", "gpt-image-1.5",
	"chatgpt-image-latest",
```

改成

```go
	"gpt-image-1", "gpt-image-1-mini", "gpt-image-1.5",
	"gpt-image-2", "gpt-image-2.5-sunburst", "gpt-image-2.5-flare",
	"gpt-image-2.6-sunburst", "gpt-image-2.6-flare",
	"chatgpt-image-latest",
```

- [ ] **Step 5: 确认测试通过**

```bash
gofmt -w setting/billing_setting/builtin_billing.go setting/billing_setting/builtin_billing_test.go relay/channel/openai/constant.go
go test ./setting/billing_setting/ ./relay/channel/openai/
```

Expected: 全部 `ok`。

- [ ] **Step 6: 提交**

```bash
git add setting/billing_setting/builtin_billing.go setting/billing_setting/builtin_billing_test.go relay/channel/openai/constant.go
git commit -m "feat(billing): add built-in prices for the gpt-image-2.6 models"
```

---

### Task 8: 新版前端展示描图费用和翻译

**Files:**
- Modify: `web/src/features/usage-logs/types.ts`、`web/src/features/usage-logs/lib/format.ts`
- Modify: `web/src/features/usage-logs/components/log-cost-display.tsx`
- Modify: `web/src/features/usage-logs/components/dialogs/details-dialog.tsx`
- Modify: `web/src/i18n/locales/{en,zh,zh-TW,fr,ru,ja,vi}.json`
- Create: `web/src/features/usage-logs/lib/__tests__/svg-author.test.ts`
- Test: `web/src/features/usage-logs/components/__tests__/cost-display.test.tsx`、`web/src/features/usage-logs/components/__tests__/usage-facts.test.tsx`

**Interfaces:**
- Consumes: Task 6 写入的 `other.svg_author` 和日志内容消息键。
- Produces: `SvgAuthorCharge` 类型、`hasSvgAuthorCharge(other: LogOtherData | null): boolean`。

- [ ] **Step 1: 读前端规范**

用读文件工具读 `web/AGENTS.md` 和 `.agents/skills/shadcn-ui/SKILL.md`。这个任务只扩展现有的 `Badge`、`Tooltip`、`LogCostDisplay` 标记和 `DetailRow` 行，不新增通用 UI。

- [ ] **Step 2: 写失败的测试**

新建 `web/src/features/usage-logs/lib/__tests__/svg-author.test.ts`：

```ts
/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { describe, expect, test } from 'vitest'

import type { LogOtherData } from '../../types'
import { hasSvgAuthorCharge } from '../format'

function withAuthorQuota(quota: number): LogOtherData {
  return {
    svg_author: {
      model: 'gpt-6-astra',
      priced_as: 'gpt-6-astra',
      input_tokens: 1000,
      output_tokens: 200,
      reasoning_tokens: 50,
      quota,
    },
  }
}

describe('SVG author charge detection', () => {
  test('is true when the author tokens were charged', () => {
    expect(hasSvgAuthorCharge(withAuthorQuota(3200))).toBe(true)
  })

  test.each([
    { name: 'a missing log', other: null },
    { name: 'a log without an author', other: {} },
    { name: 'an author that charged nothing', other: withAuthorQuota(0) },
  ])('is false for $name', ({ other }) => {
    expect(hasSvgAuthorCharge(other)).toBe(false)
  })
})
```

`web/src/features/usage-logs/components/__tests__/cost-display.test.tsx`：在 `describe` 内 `'shows subscription cost and source alongside the legacy surcharge marker'` 这个测试后面追加：

```tsx
  const svgAuthor = {
    model: 'gpt-6-astra',
    priced_as: 'gpt-6-astra',
    input_tokens: 1000,
    output_tokens: 200,
    reasoning_tokens: 50,
  }

  test('adds an accessible SVG authoring marker when author tokens were charged', () => {
    renderCost({
      quota: 12500,
      other: { svg_author: { ...svgAuthor, quota: 3200 } },
    })

    expect(screen.getByText('$0.025')).toBeVisible()
    const marker = screen.getByRole('img', {
      name: 'Includes SVG authoring cost',
    })
    expect(marker).toHaveAttribute('data-svg-author-indicator', 'true')
    expect(marker).toHaveAttribute('tabindex', '0')
    expect(
      screen.queryByRole('img', { name: 'Includes tool-call surcharge' })
    ).not.toBeInTheDocument()
  })

  test('hides the SVG authoring marker when the author charged nothing', () => {
    renderCost({
      quota: 12500,
      other: { svg_author: { ...svgAuthor, priced_as: '', quota: 0 } },
    })

    expect(
      screen.queryByRole('img', { name: 'Includes SVG authoring cost' })
    ).not.toBeInTheDocument()
  })

  test('shows the SVG authoring and tool surcharge markers together', () => {
    renderCost({
      quota: 12500,
      other: {
        svg_author: { ...svgAuthor, quota: 3200 },
        tool_surcharges: [{ name: 'lookup_customer', count: 1, price: 5 }],
      },
    })

    expect(
      screen.getByRole('img', { name: 'Includes SVG authoring cost' })
    ).toBeVisible()
    expect(
      screen.getByRole('img', { name: 'Includes tool-call surcharge' })
    ).toHaveAttribute('data-tool-surcharge-indicator', 'true')
  })
```

`web/src/features/usage-logs/components/__tests__/usage-facts.test.tsx`：在 `'does not render usage parameter rows when usage_facts is absent'` 这个测试前面追加：

```tsx
  test('shows the SVG author model, tokens and cost in the billing details', () => {
    queryClients.push(
      renderDetails({
        group_ratio: 1,
        svg_author: {
          model: 'gpt-6-astra',
          priced_as: 'gpt-6-astra',
          input_tokens: 1000,
          output_tokens: 200,
          reasoning_tokens: 50,
          quota: 3200,
        },
      })
    )

    expect(rowValue('SVG author model')).toBe('gpt-6-astra')
    expect(rowValue('SVG author tokens (input / output)')).toBe('1,000 / 200')
    expect(rowValue('SVG author cost')).toContain('0.0064')
  })

  test('does not render SVG author rows when the log has no author', () => {
    queryClients.push(renderDetails({ group_ratio: 1 }))

    expect(screen.queryByText('SVG author model')).toBeNull()
    expect(screen.queryByText('SVG author cost')).toBeNull()
  })
```

- [ ] **Step 3: 确认测试失败**

```bash
cd /d/Project/new-api/web
bunx vitest run src/features/usage-logs/lib/__tests__/svg-author.test.ts src/features/usage-logs/components/__tests__/cost-display.test.tsx src/features/usage-logs/components/__tests__/usage-facts.test.tsx 2>&1 | tail -25
```

Expected: `hasSvgAuthorCharge is not a function`；新增的显示类用例 FAIL；"hides"、"does not render" 这两个用例本来就通过。

- [ ] **Step 4: 类型和判断函数**

`web/src/features/usage-logs/types.ts`：

在 `ToolSurchargeItem` 接口后面加：

```ts
export interface SvgAuthorCharge {
  model: string
  // Model whose price was applied; empty when the tokens went unbilled.
  priced_as: string
  input_tokens: number
  output_tokens: number
  reasoning_tokens: number
  quota: number
}
```

`LogOtherData` 里，`tool_surcharges?: ToolSurchargeItem[]` 下面加一行：

```ts
  tool_surcharges?: ToolSurchargeItem[]
  svg_author?: SvgAuthorCharge
```

`LogOtherData.admin_info` 里，`reject_reason?: string` 下面加：

```ts
    reject_reason?: string
    // SVG author model that had no price, so its tokens went unbilled
    svg_author_unpriced?: string
```

`web/src/features/usage-logs/lib/format.ts`，在 `hasToolSurcharge` 函数后面加：

```ts
/**
 * Check whether a consume log includes a charged SVG authoring cost.
 */
export function hasSvgAuthorCharge(other: LogOtherData | null): boolean {
  return isPositiveFiniteNumber(other?.svg_author?.quota)
}
```

- [ ] **Step 5: 费用标记**

`web/src/features/usage-logs/components/log-cost-display.tsx`：

把图标和类型的 import 改成：

```tsx
import {
  CrownIcon,
  SvgIcon,
  Wallet01Icon,
  Wrench01Icon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon, type IconSvgElement } from '@hugeicons/react'
```

把 `import { hasToolSurcharge } from '../lib/format'` 改成：

```tsx
import { hasSvgAuthorCharge, hasToolSurcharge } from '../lib/format'
```

把整个 `ToolSurchargeMarker` 函数换成：

```tsx
function AdditionalChargeMarker(props: {
  kind: 'tool-surcharge' | 'svg-author'
  label: string
  icon: IconSvgElement
}) {
  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <Badge
            variant='warning'
            className='h-5 min-w-5 cursor-help gap-0 rounded-full px-1'
            role='img'
            aria-label={props.label}
            tabIndex={0}
            data-tool-surcharge-indicator={
              props.kind === 'tool-surcharge' ? 'true' : undefined
            }
            data-svg-author-indicator={
              props.kind === 'svg-author' ? 'true' : undefined
            }
          >
            <HugeiconsIcon
              icon={props.icon}
              strokeWidth={2}
              aria-hidden='true'
            />
            <span
              className='text-[9px] leading-none font-bold'
              aria-hidden='true'
            >
              +
            </span>
          </Badge>
        }
      />
      <TooltipContent>{props.label}</TooltipContent>
    </Tooltip>
  )
}
```

`LogCostDisplay` 里，`const showToolSurcharge = hasToolSurcharge(props.other)` 下面加：

```tsx
  const showSvgAuthorCharge = hasSvgAuthorCharge(props.other)
```

把 `{showToolSurcharge ? <ToolSurchargeMarker /> : null}` 换成：

```tsx
        {showToolSurcharge ? (
          <AdditionalChargeMarker
            kind='tool-surcharge'
            label={t('Includes tool-call surcharge')}
            icon={Wrench01Icon}
          />
        ) : null}
        {showSvgAuthorCharge ? (
          <AdditionalChargeMarker
            kind='svg-author'
            label={t('Includes SVG authoring cost')}
            icon={SvgIcon}
          />
        ) : null}
```

- [ ] **Step 6: 日志详情**

`web/src/features/usage-logs/components/dialogs/details-dialog.tsx`：

import 改动：

```tsx
import { useCopyToClipboard } from '@/hooks/use-copy-to-clipboard'
import { toIntlLocale } from '@/i18n/languages'
import { formatBillingCurrencyFromUSD } from '@/lib/currency'
import {
  formatLogQuota,
  formatNumber,
  formatTokens,
  formatUseTime,
} from '@/lib/format'
```

`BillingBreakdown` 开头：

```tsx
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const { log, other, isAdmin } = props
```

在 `if (other.image_generation_call && other.image_generation_call_price) { ... }` 这段后面加：

```tsx
  if (other.svg_author) {
    rows.push({ label: t('SVG author model'), value: other.svg_author.model })
    rows.push({
      label: t('SVG author tokens (input / output)'),
      value: `${formatNumber(other.svg_author.input_tokens, locale)} / ${formatNumber(other.svg_author.output_tokens, locale)}`,
    })
    rows.push({
      label: t('SVG author cost'),
      value: formatLogQuota(other.svg_author.quota),
    })
  }
```

- [ ] **Step 7: 确认测试通过**

```bash
cd /d/Project/new-api/web
bunx vitest run src/features/usage-logs
bun run typecheck
bunx oxlint -c .oxlintrc.json src/features/usage-logs/types.ts src/features/usage-logs/lib/format.ts src/features/usage-logs/components/log-cost-display.tsx src/features/usage-logs/components/dialogs/details-dialog.tsx src/features/usage-logs/lib/__tests__/svg-author.test.ts src/features/usage-logs/components/__tests__/cost-display.test.tsx src/features/usage-logs/components/__tests__/usage-facts.test.tsx
```

Expected: vitest 全部通过；typecheck 没有错误；oxlint 没有 error。

- [ ] **Step 8: 加翻译**

把 5 个键追加到每个语言文件末尾。不要跑 `bun run i18n:sync`：它会改写受保护的署名键。

```bash
cd /d/Project/new-api
cat > /tmp/add-svg-i18n.mjs <<'EOF'
import { readFileSync, writeFileSync } from 'node:fs'

const keys = [
  'SVG author {{model}}: {{input}} input tokens, {{output}} output tokens, cost {{quota}}',
  'SVG author model',
  'SVG author tokens (input / output)',
  'SVG author cost',
  'Includes SVG authoring cost',
]
const translations = {
  en: keys,
  zh: [
    'SVG 描图模型 {{model}}：输入 {{input}} Token，输出 {{output}} Token，花费 {{quota}}',
    'SVG 描图模型',
    'SVG 描图 Token（输入 / 输出）',
    'SVG 描图费用',
    '包含 SVG 描图费用',
  ],
  'zh-TW': [
    'SVG 描圖模型 {{model}}：輸入 {{input}} Token，輸出 {{output}} Token，花費 {{quota}}',
    'SVG 描圖模型',
    'SVG 描圖 Token（輸入 / 輸出）',
    'SVG 描圖費用',
    '包含 SVG 描圖費用',
  ],
  fr: [
    "Modèle de création SVG {{model}} : {{input}} tokens d'entrée, {{output}} tokens de sortie, coût {{quota}}",
    'Modèle de création SVG',
    'Tokens de création SVG (entrée / sortie)',
    'Coût de création SVG',
    'Inclut le coût de création SVG',
  ],
  ru: [
    'Модель создания SVG {{model}}: входных токенов {{input}}, выходных токенов {{output}}, стоимость {{quota}}',
    'Модель создания SVG',
    'Токены создания SVG (вход / выход)',
    'Стоимость создания SVG',
    'Включает стоимость создания SVG',
  ],
  ja: [
    'SVG 作成モデル {{model}}：入力トークン {{input}}、出力トークン {{output}}、費用 {{quota}}',
    'SVG 作成モデル',
    'SVG 作成トークン（入力 / 出力）',
    'SVG 作成費用',
    'SVG 作成費用を含む',
  ],
  vi: [
    'Mô hình tạo SVG {{model}}: {{input}} token đầu vào, {{output}} token đầu ra, chi phí {{quota}}',
    'Mô hình tạo SVG',
    'Token tạo SVG (đầu vào / đầu ra)',
    'Chi phí tạo SVG',
    'Bao gồm chi phí tạo SVG',
  ],
}
const ending = '"\n  }\n}\n'
for (const [locale, values] of Object.entries(translations)) {
  const path = `web/src/i18n/locales/${locale}.json`
  const text = readFileSync(path, 'utf8')
  if (!text.endsWith(ending)) throw new Error(`${path}: unexpected ending`)
  const lines = keys.map((key, index) => `    ${JSON.stringify(key)}: ${JSON.stringify(values[index])}`)
  const updated = text.slice(0, -ending.length) + '",\n' + lines.join(',\n') + '\n  }\n}\n'
  JSON.parse(updated)
  writeFileSync(path, updated)
}
EOF
node "$(cygpath -w /tmp/add-svg-i18n.mjs)"
git diff --stat -- web/src/i18n/locales
```

Expected: 7 个文件，每个 `7 ++++++-`（6 行新增、1 行删除：原来的最后一行多了逗号）。

- [ ] **Step 9: 提交**

```bash
git add web/src/features/usage-logs web/src/i18n/locales
git commit -m "feat(usage-logs): show the SVG author's tokens and cost"
```

---

### Task 9: 全量验证

**Files:** 无代码改动。发现问题就回到对应任务修复并单独提交。

- [ ] **Step 1: 后端**

```bash
cd /d/Project/new-api
gofmt -l relay service setting relaykit common
go build ./...
(cd relaykit && GOWORK=off go build ./... && GOWORK=off go test ./...)
go test ./...
```

Expected: `gofmt -l` 没有输出；编译没有错误；测试全部 `ok`。如果有失败，先在 `prod` 分支上跑同一个测试，确认是不是本来就失败，并如实记录。

这次没有改数据库结构和查询（描图信息写进现有的 `other` JSON），不需要跑三库矩阵。

- [ ] **Step 2: 前端**

```bash
cd /d/Project/new-api/web
bun run typecheck
bunx vitest run src/features/usage-logs
bun run build
```

Expected: 全部成功。`web/dist` 被嵌入 Go 程序，不重新构建看不到前端改动。

- [ ] **Step 3: 用假上游走一遍真实链路**

调用 `verify` 技能，用临时 SQLite 和本地假上游（不碰真实上游，也不用项目根的 `one-api.db`）。假上游对 `POST /v1/images/generations` 记录收到的请求体，并返回：

```json
{"created":1,"output_format":"svg","data":[{"b64_json":"PHN2Zz48L3N2Zz4="}],"usage":{"input_tokens":100,"output_tokens":50,"total_tokens":150,"input_tokens_details":{"text_tokens":100,"image_tokens":0}},"svg_usage":{"model":"gpt-6-astra","input_tokens":1000,"output_tokens":200,"total_tokens":1200,"reasoning_tokens":50}}
```

用没开透传的 OpenAI 类型渠道，发 `{"model":"gpt-image-2.6-flare","prompt":"logo","n":1,"output_format":"svg","svg":{"model":"gpt-6-astra","reasoning":{"effort":"low"}},"complexity":"low"}`。

确认：

1. 假上游收到的请求体里有完整的 `svg` 和 `complexity`。
2. 客户端拿到的响应里有 `svg_usage`。
3. 消费日志的额度 = 生图费用 + 描图费用：`gpt-6-astra` 的内置价格是输入 $10、输出 $50，描图费用 = (1000×10 + 200×50) / 1000000 × 500000 × 分组倍率 = 10000 × 分组倍率。
4. 日志详情里能看到三行描图信息，费用列有 SVG 标记。
5. 把 `svg.model` 换成没配价的模型时返回 400，假上游没有收到请求。

- [ ] **Step 4: 汇报**

汇报实际运行的命令和结果。不要推送、不要打 tag。提醒用户：上线前需要用一条真实 SVG 请求核对 `reasoning_tokens` 口径（会产生费用，要先征得同意）。
