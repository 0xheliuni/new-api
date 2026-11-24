# 中转站模式（通用第三方平台）实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让阿里插件按渠道上的 `task_plugin_config.relay` 对接任意中转站：客户端入口、参数校验和计费方式不变，同时提供保存时校验和离线预览。

**Architecture:** 宿主（Go）新增渠道字段 `task_plugin_config`，以 `ctx.channelConfig` 传给插件钩子（包括后台轮询）；`parseTaskResult` 可以返回 `data`，宿主把它存为任务数据，并用它来结算；保存渠道时调用插件的可选钩子 `validateChannelConfig`；新增离线预览接口。阿里插件（JS）有 `relay` 时按模板转换请求、按路径解析返回，并把查询结果整理成 DashScope 格式的快照。新前端在渠道编辑页加配置编辑区块和预览弹窗。

**Tech Stack:** Go 1.26（gin、testify）、moejs 插件运行时（JS）、React 19 + TypeScript + react-hook-form + zod + vitest、Bun。

**Spec:** `接口文档/中转站wan视频接入设计.md`

## Global Constraints

- root 模块里的 JSON 编解码一律用 `common.Marshal` / `common.Unmarshal` / `common.UnmarshalJsonStr` / `common.DecodeJson`；`json.RawMessage` 这类类型可以引用。
- `relaykit/` 必须能独立编译：`cd relaykit && GOWORK=off go build ./...`。
- 新增或修改的 Go 测试使用 testify 的 `require` / `assert`；改过的 Go 文件跑 `gofmt -w`。
- 插件 JS 不用可选链 `?.`（`??`、箭头函数、展开语法可以用）；钩子参数只读，要修改先 `utils.json.clone`。插件抛出的错误信息写英文句子。
- 给控制台的新后端提示用 `common.NewMessage`，提示键要补进 7 个前端语言文件（Task 10 统一补）。
- 不在 `docs/` 下新建文件，只改 `docs/plugin-api/` 里已有的三个文件。
- 前端复用 `JsonCodeEditor`、`@/components/dialog`、`Combobox`、`Button`、`@/components/ui/form`；界面文字一律 `t()`；改完跑 `bun run typecheck`，对改动的文件跑 lint，并跑受影响的测试。
- 不改额度换算、预扣、结算代码；结算结果只随插件返回的用量事实变化（spec 第 6 节，「多退少补」）。
- 在本地分支 `feat/relay-task-plugin-config` 上每完成一个任务提交一次，不推送。

## File Map

| 文件 | 改动 | 职责 |
|---|---|---|
| `relaykit/dto/channel_settings.go` | 改 | `TaskPluginConfig` 字段 |
| `relay/channel/task/jsplugin/adaptor.go` | 改 | `ctx.channelConfig`；`parseTaskResult` 的 `data` |
| `relay/common/relay_info.go` | 改 | `TaskInfo.Data` |
| `service/task_polling.go` | 改 | 轮询带上渠道设置；保存插件给的 `data` |
| `controller/task_video.go` | 改 | 保存插件给的 `data` |
| `common/log_text.zh-CN.json` | 改 | 新日志行的中文 |
| `controller/channel_task_plugin_config.go` | 新建 | 配置校验、预览接口 |
| `controller/channel.go` | 改 | `validateChannel` 调用配置校验 |
| `router/channel-router.go` | 改 | 预览路由 |
| `plugins/tasks/alibaba/plugin.js` | 改 | 中转站模式、`validateChannelConfig`、版本号 |
| `plugins/alibaba_relay_test.go` | 新建 | 插件中转站模式测试 |
| `relay/channel/task/jsplugin/adaptor_test.go` | 改 | 宿主上下文与数据测试 |
| `service/task_polling_test.go` | 改 | 轮询测试 |
| `controller/channel_task_plugin_validation_test.go` | 改 | 保存校验、预览测试 |
| `router/video_router_test.go` | 改 | 端到端测试 |
| `docs/plugin-api/v1.md`、`v1.d.ts`、`v1.schema.json` | 改 | 插件接口文档 |
| `web/src/features/channels/constants.ts` | 改 | 常量 |
| `web/src/features/channels/lib/channel-plugin-extensions.ts` | 改 | `supportsTaskPluginConfig` |
| `web/src/features/channels/lib/channel-form.ts` | 改 | 表单字段 |
| `web/src/features/channels/types.ts` | 改 | 设置类型 |
| `web/src/features/channels/api.ts` | 改 | 预览接口调用 |
| `web/src/features/channels/components/drawers/sections/channel-relay-config-section.tsx` | 新建 | 配置编辑区块 |
| `web/src/features/channels/components/drawers/sections/index.ts` | 改 | 导出 |
| `web/src/features/channels/components/dialogs/relay-config-preview-dialog.tsx` | 新建 | 预览弹窗 |
| `web/src/features/channels/components/drawers/channel-mutate-drawer.tsx` | 改 | 接入区块 |
| `web/src/i18n/locales/{en,zh,zh-TW,fr,ru,ja,vi}.json` | 改 | 文案 |
| `web/src/features/channels/lib/__tests__/task-plugin-config.test.ts` | 新建 | 表单逻辑测试 |
| `web/src/features/channels/components/__tests__/relay-config-section.test.tsx` | 新建 | 界面测试 |

---

### Task 0: 准备分支

- [ ] **Step 1: 建分支**

```bash
cd /d/Project/new-api
git status --short        # 只应有未跟踪的 接口文档/ 内容
git switch -c feat/relay-task-plugin-config
```

---

### Task 1: 渠道字段 `task_plugin_config` 与 `ctx.channelConfig`

**Files:**
- Modify: `relaykit/dto/channel_settings.go`（`ChannelSettings` 结构体）
- Modify: `relay/channel/task/jsplugin/adaptor.go`（`submitContext`、`queryContext`、`batchQueryContext`、`BuildContentRequest`，新增 `channelConfigValue`）
- Modify: `service/task_polling.go`（`BuildVideoPollingRelayInfo`、批量轮询的 `ChannelMeta`）
- Test: `relay/channel/task/jsplugin/adaptor_test.go`、`service/task_polling_test.go`

**Interfaces:**
- Produces: `dto.ChannelSettings.TaskPluginConfig json.RawMessage`（JSON 键 `task_plugin_config`）；钩子上下文键 `channelConfig`（解码后的 JSON 值，未配置时为 `nil`）；`func channelConfigValue(info *relaycommon.RelayInfo) any`。

- [ ] **Step 1: 写失败测试（adaptor）**

在 `relay/channel/task/jsplugin/adaptor_test.go` 末尾追加：

```go
const channelConfigPlugin = `
export const meta = {apiVersion:1,key:"channel-config",name:"Channel Config",version:"1.0.0",author:{name:"Test"},models:["m"],fetchMode:"per_task"};
export function buildSubmitRequest(ctx){return {url:ctx.baseUrl+"/submit"}}
export function parseSubmitResponse(){return {taskId:"1"}}
export function buildQueryRequest(ctx){return {url:ctx.baseUrl+"/query"}}
export function parseTaskResult(){return {status:"IN_PROGRESS"}}
export function listArtifacts(){return [{key:"video",type:"video"}]}
export function buildContentRequest(ctx){return {url:"https://cdn.example/"+(ctx.channelConfig ? ctx.channelConfig.marker : "none")+".mp4",credentialless:true}}
`

func TestTaskAdaptorExposesChannelConfigToDriverHooks(t *testing.T) {
	plugin, err := pluginruntime.NewRegistry().Register(channelConfigPlugin, pluginruntime.Options{})
	require.NoError(t, err)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)

	configured := New(plugin)
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{
		ChannelBaseUrl: "https://provider.example",
		ApiKey:         "secret",
		ChannelSetting: dto.ChannelSettings{TaskPluginConfig: json.RawMessage(`{"marker":"relay-a"}`)},
	}}
	configured.Init(info)
	want := map[string]any{"marker": "relay-a"}

	assert.Equal(t, want, configured.submitContext(c, info)["channelConfig"])
	query, err := configured.queryContext(&model.Task{}, "secret", "https://provider.example", "")
	require.NoError(t, err)
	assert.Equal(t, want, query["channelConfig"])
	batch, err := configured.batchQueryContext("secret", "https://provider.example", "", nil)
	require.NoError(t, err)
	assert.Equal(t, want, batch["channelConfig"])
	content, err := configured.BuildContentRequest(&model.Task{Status: model.TaskStatusSuccess}, "video", channel.TaskArtifactClientRequest{Method: http.MethodGet})
	require.NoError(t, err)
	assert.Equal(t, "https://cdn.example/relay-a.mp4", content.URL)

	bare := New(plugin)
	bareInfo := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: "https://provider.example", ApiKey: "secret"}}
	bare.Init(bareInfo)
	submit := bare.submitContext(c, bareInfo)
	require.Contains(t, submit, "channelConfig")
	assert.Nil(t, submit["channelConfig"])
}
```

- [ ] **Step 2: 写失败测试（轮询带上渠道设置）**

在 `service/task_polling_test.go` 的 `TestPollingPassesExecutingChannelTypeToAdaptor` 里：

把 `gateway.SetOtherSettings(dto.ChannelOtherSettings{DisableTaskPollingSleep: true})` 这一行下面加一行：

```go
	gateway.SetSetting(dto.ChannelSettings{TaskPluginConfig: json.RawMessage(`{"marker":"poll"}`)})
```

在第一个 `assert.Equal(t, baseURL, meta.ChannelBaseUrl)`（per-task 部分）后面加：

```go
	assert.JSONEq(t, `{"marker":"poll"}`, string(meta.ChannelSetting.TaskPluginConfig), "per-task polling carries the channel setting")
```

在第二个 `assert.Equal(t, baseURL, meta.ChannelBaseUrl)`（batch 部分）后面加：

```go
	assert.JSONEq(t, `{"marker":"poll"}`, string(meta.ChannelSetting.TaskPluginConfig), "batch polling carries the channel setting")
```

如果文件还没有导入 `encoding/json`，在 import 块里加上 `"encoding/json"`（`dto` 已经导入）。

- [ ] **Step 3: 运行，确认失败**

```bash
go test ./relay/channel/task/jsplugin/ -run TestTaskAdaptorExposesChannelConfigToDriverHooks -count=1
go test ./service/ -run TestPollingPassesExecutingChannelTypeToAdaptor -count=1
```

Expected: 两个都编译失败，报 `unknown field TaskPluginConfig in struct literal`。

- [ ] **Step 4: 加字段**

`relaykit/dto/channel_settings.go`：import 块加 `"encoding/json"`；在 `TaskExtendPluginKeys []string ...` 那一行后面加：

```go
	// TaskPluginConfig is the executing task plugin's per-channel
	// configuration, a JSON object. The host validates it on save through the
	// plugin's validateChannelConfig hook and passes it to driver hooks as
	// ctx.channelConfig.
	TaskPluginConfig json.RawMessage `json:"task_plugin_config,omitempty"`
```

- [ ] **Step 5: 把配置放进钩子上下文**

`relay/channel/task/jsplugin/adaptor.go`：

在 `func (a *TaskAdaptor) queryCredentials()` 函数结束的 `}` 后面加：

```go
// channelConfigValue decodes the executing channel's task_plugin_config for
// ctx.channelConfig. A channel without one, or without channel metadata,
// yields null.
func channelConfigValue(info *relaycommon.RelayInfo) any {
	if info == nil || !info.HasChannelMeta() || len(info.ChannelSetting.TaskPluginConfig) == 0 {
		return nil
	}
	var config any
	if err := common.Unmarshal(info.ChannelSetting.TaskPluginConfig, &config); err != nil {
		return nil
	}
	return config
}
```

`submitContext` 里，把

```go
	ctx["userSetting"] = info.UserSetting
```

改成

```go
	ctx["userSetting"] = info.UserSetting
	ctx["channelConfig"] = channelConfigValue(info)
```

`queryContext` 的 map 字面量里，在 `"state":         nil,` 后面加一行 `"channelConfig": channelConfigValue(a.info),`。

`batchQueryContext` 里，把

```go
	ctx := map[string]any{"baseUrl": baseURL, "tasks": tasks}
```

改成

```go
	ctx := map[string]any{"baseUrl": baseURL, "tasks": tasks, "channelConfig": channelConfigValue(a.info)}
```

`BuildContentRequest` 里，把

```go
	ctx["baseUrl"] = a.info.ChannelBaseUrl
	ctx["clientRequest"] = jsonValue(clientRequest)
```

改成

```go
	ctx["baseUrl"] = a.info.ChannelBaseUrl
	ctx["channelConfig"] = channelConfigValue(a.info)
	ctx["clientRequest"] = jsonValue(clientRequest)
```

- [ ] **Step 6: 轮询带上渠道设置**

`service/task_polling.go`：

`BuildVideoPollingRelayInfo` 的 `ChannelMeta` 字面量加一行：

```go
		ChannelSetting:       ch.GetSetting(),
```

批量轮询那处

```go
	info.ChannelMeta = &relaycommon.ChannelMeta{ChannelType: ch.Type, ChannelId: ch.Id, ChannelBaseUrl: baseURL}
```

改成

```go
	info.ChannelMeta = &relaycommon.ChannelMeta{ChannelType: ch.Type, ChannelId: ch.Id, ChannelBaseUrl: baseURL, ChannelSetting: ch.GetSetting()}
```

- [ ] **Step 7: 运行，确认通过**

```bash
gofmt -w relaykit/dto/channel_settings.go relay/channel/task/jsplugin/adaptor.go service/task_polling.go relay/channel/task/jsplugin/adaptor_test.go service/task_polling_test.go
go test ./relay/channel/task/jsplugin/ ./service/ -count=1
cd relaykit && GOWORK=off go build ./... && cd ..
```

Expected: 全部 PASS，relaykit 编译成功。

- [ ] **Step 8: 提交**

```bash
git add relaykit/dto/channel_settings.go relay/channel/task/jsplugin/adaptor.go relay/channel/task/jsplugin/adaptor_test.go service/task_polling.go service/task_polling_test.go
git commit -m "feat(task-plugin): pass channel task_plugin_config to plugin hooks"
```

---

### Task 2: `parseTaskResult` 返回的 `data` 用于保存和结算

**Files:**
- Modify: `relay/common/relay_info.go`（`TaskInfo`）
- Modify: `relay/channel/task/jsplugin/adaptor.go`（`taskResult`、`ParseTaskResult`）
- Modify: `service/task_polling.go`（`updateVideoSingleTask` 里的 `task.Data = redactVideoResponseBody(responseBody)`）
- Modify: `controller/task_video.go`（同名逻辑）
- Modify: `common/log_text.zh-CN.json`
- Test: `relay/channel/task/jsplugin/adaptor_test.go`、`service/task_polling_test.go`

**Interfaces:**
- Consumes: Task 1 无依赖。
- Produces: `relaycommon.TaskInfo.Data json.RawMessage`（`json:"-"`）；插件 `parseTaskResult` 结果里的可选字段 `data`。

- [ ] **Step 1: 写失败测试（adaptor）**

在 `relay/channel/task/jsplugin/adaptor_test.go` 末尾追加：

```go
func TestTaskAdaptorPersistsAndSettlesFromReturnedTaskData(t *testing.T) {
	source := `
export const meta = {apiVersion:1,key:"task-data",name:"Task Data",version:"1.0.0",author:{name:"Test"},models:["m"],fetchMode:"per_task",usageSchema:{seconds:{type:"number",unit:"second"}}};
export function buildSubmitRequest(ctx){return {url:ctx.baseUrl+"/submit"}}
export function parseSubmitResponse(){return {taskId:"1"}}
export function buildQueryRequest(ctx){return {url:ctx.baseUrl+"/query"}}
export function parseTaskResult(ctx, body){
  if (body.raw) return {status:"SUCCESS", url:"https://cdn.example/v.mp4", data:{output:{task_status:"SUCCEEDED"}, usage:{seconds: body.seconds}}};
  return {status:"IN_PROGRESS"};
}
export function extractUsageOnComplete(task, result, data){return {seconds: data.usage ? data.usage.seconds : 0}}
`
	plugin, err := pluginruntime.NewRegistry().Register(source, pluginruntime.Options{})
	require.NoError(t, err)
	adaptor := New(plugin)
	adaptor.Init(&relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: "https://provider.example", ApiKey: "secret"}})
	response := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header)}

	result, err := adaptor.ParseTaskResult(&model.Task{}, response, []byte(`{"raw":true,"seconds":7,"envelope":"x"}`))
	require.NoError(t, err)
	assert.JSONEq(t, `{"output":{"task_status":"SUCCEEDED"},"usage":{"seconds":7}}`, string(result.Data))
	assert.EqualValues(t, 7, result.UsageFacts["seconds"], "settlement reads the returned data, not the upstream body")

	result, err = adaptor.ParseTaskResult(&model.Task{}, response, []byte(`{"pending":true}`))
	require.NoError(t, err)
	assert.Empty(t, result.Data, "without data the host keeps the upstream body")
}
```

- [ ] **Step 2: 写失败测试（轮询保存）**

在 `service/task_polling_test.go` 的 `TestUpdateVideoSingleTaskPollClassification` 里：

1. 测试用例结构体里，在 `statusCode    int` 后面加 `body          []byte`，在 `wantUnchanged bool` 后面加 `wantData      string`。
2. 在 `testCases` 列表末尾（`other 4xx non-terminal is unrecognized` 用例之后）加两个用例：

```go
		{
			name:       "plugin data replaces the upstream body",
			statusCode: http.StatusOK,
			body:       []byte(`{"relay":"envelope"}`),
			parse:      &relaycommon.TaskInfo{Status: model.TaskStatusInProgress, Data: []byte(`{"output":{"task_status":"RUNNING"}}`)},
			wantStatus: model.TaskStatusInProgress,
			wantData:   `{"output":{"task_status":"RUNNING"}}`,
		},
		{
			name:       "without plugin data the upstream body is kept",
			statusCode: http.StatusOK,
			body:       []byte(`{"relay":"envelope"}`),
			parse:      &relaycommon.TaskInfo{Status: model.TaskStatusInProgress},
			wantStatus: model.TaskStatusInProgress,
			wantData:   `{"relay":"envelope"}`,
		},
```

3. 把创建适配器那行改成把 `body` 也传进去：

```go
			adaptor := &scriptedPollingAdaptor{statusCode: testCase.statusCode, body: testCase.body, fetchErr: testCase.fetchErr, parse: testCase.parse, parseErr: testCase.parseErr}
```

4. 在 `if testCase.wantState != "" {` 那个判断之前加：

```go
			if testCase.wantData != "" {
				assert.JSONEq(t, testCase.wantData, string(persisted.Data))
			}
```

- [ ] **Step 3: 运行，确认失败**

```bash
go test ./relay/channel/task/jsplugin/ -run TestTaskAdaptorPersistsAndSettlesFromReturnedTaskData -count=1
go test ./service/ -run TestUpdateVideoSingleTaskPollClassification -count=1
```

Expected: 编译失败，`TaskInfo` 没有 `Data` 字段。

- [ ] **Step 4: 给 `TaskInfo` 加字段**

`relay/common/relay_info.go` 的 `TaskInfo` 结构体，在 `PluginState      json.RawMessage ...` 后面加：

```go
	// Data, when a task plugin's parseTaskResult returns it, replaces the
	// upstream response body as the persisted task data.
	Data json.RawMessage `json:"-"`
```

- [ ] **Step 5: 适配器接收 `data`**

`relay/channel/task/jsplugin/adaptor.go`：

`taskResult` 结构体在 `State            any     \`json:"state"\`` 后面加一行 `Data             any     \`json:"data"\``（gofmt 会对齐）。

`ParseTaskResult` 里，把

```go
	// The raw polling response only exists at this boundary. Capture upstream
	// units here so the host settlement path can consume them from TaskInfo.
	if a.hasHook(context.Background(), "extractUsageOnComplete") {
		facts, hookErr := a.plugin.Engine.Call(context.Background(), "extractUsageOnComplete", ctx, jsonValue(result), input)
```

改成

```go
	usageBody := input
	if parsed.Data != nil {
		data, marshalErr := common.Marshal(parsed.Data)
		if marshalErr != nil || len(data) > maxTaskPluginPersistedJSONBytes {
			logger.LogWarn(context.Background(), common.LogText("task plugin %s rejected invalid or oversized poll data", a.plugin.Meta.Key))
		} else {
			// A plugin that normalizes the upstream snapshot persists that
			// snapshot and settles from the same value.
			result.Data = data
			usageBody = parsed.Data
		}
	}
	// The raw polling response only exists at this boundary. Capture upstream
	// units here so the host settlement path can consume them from TaskInfo.
	if a.hasHook(context.Background(), "extractUsageOnComplete") {
		facts, hookErr := a.plugin.Engine.Call(context.Background(), "extractUsageOnComplete", ctx, jsonValue(result), usageBody)
```

- [ ] **Step 6: 两处轮询保存 `data`**

`service/task_polling.go`（`updateVideoSingleTask` 里）把

```go
	task.Data = redactVideoResponseBody(responseBody)
```

改成

```go
	if len(taskResult.Data) > 0 {
		task.Data = taskResult.Data
	} else {
		task.Data = redactVideoResponseBody(responseBody)
	}
```

`controller/task_video.go` 把

```go
	} else if taskResult, err = adaptor.ParseTaskResult(task, resp, responseBody); err != nil {
		return fmt.Errorf("parseTaskResult failed for task %s: %w", taskId, err)
	} else {
		task.Data = redactVideoResponseBody(responseBody)
	}
```

改成

```go
	} else if taskResult, err = adaptor.ParseTaskResult(task, resp, responseBody); err != nil {
		return fmt.Errorf("parseTaskResult failed for task %s: %w", taskId, err)
	} else if len(taskResult.Data) > 0 {
		task.Data = taskResult.Data
	} else {
		task.Data = redactVideoResponseBody(responseBody)
	}
```

`common/log_text.zh-CN.json`：在 `"task plugin %s rejected invalid or oversized poll state": ...` 这一行后面加一行：

```json
  "task plugin %s rejected invalid or oversized poll data": "任务插件 %s 返回的轮询数据无效或过大，未保存",
```

- [ ] **Step 7: 运行，确认通过**

```bash
gofmt -w relay/common/relay_info.go relay/channel/task/jsplugin/adaptor.go service/task_polling.go controller/task_video.go relay/channel/task/jsplugin/adaptor_test.go service/task_polling_test.go
go test ./relay/channel/task/jsplugin/ ./service/ ./common/ -count=1
go build ./...
```

Expected: 全部 PASS，编译通过。

- [ ] **Step 8: 提交**

```bash
git add relay/common/relay_info.go relay/channel/task/jsplugin/adaptor.go relay/channel/task/jsplugin/adaptor_test.go service/task_polling.go service/task_polling_test.go controller/task_video.go common/log_text.zh-CN.json
git commit -m "feat(task-plugin): persist and settle from data returned by parseTaskResult"
```

---

### Task 3: 保存渠道时校验 `task_plugin_config`

**Files:**
- Create: `controller/channel_task_plugin_config.go`
- Modify: `controller/channel.go`（`validateChannel`）
- Test: `controller/channel_task_plugin_validation_test.go`

**Interfaces:**
- Consumes: Task 1 的 `dto.ChannelSettings.TaskPluginConfig`。
- Produces:
  - `func validateChannelTaskPluginConfig(ctx context.Context, channelType int, setting dto.ChannelSettings) (map[string]any, *jsplugin.LoadedPlugin, error)`：没有配置时返回 `nil, nil, nil`。
  - `func taskPluginHookMessage(err error) string`
  - 常量 `maxChannelTaskPluginConfigBytes = 32 << 10`
  - 后端提示键 `Invalid task plugin configuration: {{error}}`（Task 10 补进语言文件）

- [ ] **Step 1: 写失败测试**

在 `controller/channel_task_plugin_validation_test.go` 末尾追加：

```go
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
	baseURL := "https://relay.example"

	cases := []struct {
		name        string
		channelType int
		setting     string
		wantErr     string
	}{
		{"no configuration", constant.ChannelTypeTaskPlugin, `{"task_plugin_key":"config-hook"}`, ""},
		{"accepted by the plugin", constant.ChannelTypeTaskPlugin, `{"task_plugin_key":"config-hook","task_plugin_config":{"relay":{}}}`, ""},
		{"rejected by the plugin", constant.ChannelTypeTaskPlugin, `{"task_plugin_key":"config-hook","task_plugin_config":{"other":1}}`, "Invalid task plugin configuration: relay is required"},
		{"not an object", constant.ChannelTypeTaskPlugin, `{"task_plugin_key":"config-hook","task_plugin_config":[1]}`, "must be a JSON object"},
		{"plugin without the hook", constant.ChannelTypeTaskPlugin, `{"task_plugin_key":"config-no-hook","task_plugin_config":{"relay":{}}}`, "does not accept a channel configuration"},
		{"New API channel", constant.ChannelTypeNewAPI, `{"task_plugin_config":{"relay":{}}}`, "not supported on New API channels"},
		{"channel type without a plugin", constant.ChannelTypeAnthropic, `{"task_plugin_config":{"relay":{}}}`, "does not run a task plugin"},
		{"oversized", constant.ChannelTypeTaskPlugin, `{"task_plugin_key":"config-hook","task_plugin_config":{"relay":{},"pad":"` + strings.Repeat("x", 33<<10) + `"}}`, "must not exceed 32 KiB"},
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
		})
	}
}
```

- [ ] **Step 2: 运行，确认失败**

```bash
go test ./controller/ -run TestValidateChannelTaskPluginConfig -count=1
```

Expected: FAIL，「rejected by the plugin」「not an object」等用例没有返回错误。

- [ ] **Step 3: 新建 `controller/channel_task_plugin_config.go`**

```go
package controller

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/relaykit/dto"
)

const maxChannelTaskPluginConfigBytes = 32 << 10

// validateChannelTaskPluginConfig checks a channel's task_plugin_config and
// has the executing plugin validate it: the bound plugin of a Task Plugin
// channel, or the plugin registered for a legacy channel type. It returns the
// decoded configuration and that plugin, or nil values when the channel
// stores no configuration.
func validateChannelTaskPluginConfig(ctx context.Context, channelType int, setting dto.ChannelSettings) (map[string]any, *jsplugin.LoadedPlugin, error) {
	raw := bytes.TrimSpace(setting.TaskPluginConfig)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil, nil, nil
	}
	if len(raw) > maxChannelTaskPluginConfigBytes {
		return nil, nil, fmt.Errorf("task_plugin_config must not exceed 32 KiB")
	}
	var config map[string]any
	if err := common.Unmarshal(raw, &config); err != nil || config == nil {
		return nil, nil, fmt.Errorf("task_plugin_config must be a JSON object")
	}

	var plugin *jsplugin.LoadedPlugin
	var found bool
	switch channelType {
	case constant.ChannelTypeTaskPlugin:
		plugin, found = jsplugin.DefaultRegistry.Get(strings.TrimSpace(setting.TaskPluginKey))
	case constant.ChannelTypeNewAPI:
		return nil, nil, fmt.Errorf("task_plugin_config is not supported on New API channels")
	default:
		plugin, found = jsplugin.DefaultRegistry.GetByChannelType(channelType)
	}
	if !found {
		return nil, nil, fmt.Errorf("channel type %d does not run a task plugin, so task_plugin_config is not supported", channelType)
	}
	if !plugin.Engine.HasExport("validateChannelConfig") {
		return nil, nil, fmt.Errorf("task plugin %q does not accept a channel configuration", plugin.Meta.Key)
	}
	if _, err := plugin.Engine.Call(ctx, "validateChannelConfig", config); err != nil {
		return nil, nil, common.NewMessage("Invalid task plugin configuration: {{error}}", map[string]any{"error": taskPluginHookMessage(err)})
	}
	return config, plugin, nil
}

// taskPluginHookMessage is the sanitized message a plugin hook threw, safe to
// show to administrators.
func taskPluginHookMessage(err error) string {
	var hookErr *jsplugin.HookError
	if errors.As(err, &hookErr) && hookErr.Message != "" {
		return hookErr.Message
	}
	return "plugin hook failed"
}
```

说明：Task Plugin 渠道绑定的插件不存在时，`validateChannel` 前面的逻辑已经报「is not registered」，走不到这里。

- [ ] **Step 4: `validateChannel` 调用校验**

`controller/channel.go` 的 `validateChannel` 里，在

```go
	if channel.Type == constant.ChannelTypeSGLang && strings.TrimSpace(channel.GetBaseURL()) == "" {
		return fmt.Errorf("SGLang channel base URL cannot be empty")
	}
```

后面加：

```go
	if _, _, err := validateChannelTaskPluginConfig(context.Background(), channel.Type, setting); err != nil {
		return err
	}
```

（`setting` 就是函数前面 `setting := channel.GetSetting()` 那个变量；如果文件还没导入 `context`，在 import 块加上。）

- [ ] **Step 5: 运行，确认通过**

```bash
gofmt -w controller/channel_task_plugin_config.go controller/channel.go controller/channel_task_plugin_validation_test.go
go test ./controller/ -run 'TestValidateChannelTaskPluginConfig|TestValidateTaskPluginChannel|TestValidateNewAPIChannelTaskPluginExtensions' -count=1
```

Expected: PASS。

- [ ] **Step 6: 提交**

```bash
git add controller/channel_task_plugin_config.go controller/channel.go controller/channel_task_plugin_validation_test.go
git commit -m "feat(channel): validate task_plugin_config through the executing plugin on save"
```

---

### Task 4: 阿里插件中转站模式（提交、查询、快照）

**Files:**
- Modify: `plugins/tasks/alibaba/plugin.js`
- Create: `plugins/alibaba_relay_test.go`

**Interfaces:**
- Consumes: 钩子上下文里的 `ctx.channelConfig`（Task 1）；`parseTaskResult` 返回 `data`（Task 2）。
- Produces: 插件版本 `1.5.0`；`ctx.channelConfig.relay` 存在时，`buildSubmitRequest` / `parseSubmitResponse` / `buildQueryRequest` / `parseTaskResult` 按 spec 第 3 节工作；快照格式见 spec 3.6。测试常量 `tokenPonyRelayConfig`（Task 5 复用）。

- [ ] **Step 1: 写失败测试**

新建 `plugins/alibaba_relay_test.go`（复用同包里 `alibaba_responses_test.go` 的 `newAlibabaPlugin`、`alibabaObject`）：

```go
package plugins_test

import (
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

	t.Run("relay mode rejects image models", func(t *testing.T) {
		_, err := plugin.Engine.Call(t.Context(), "buildSubmitRequest", submitCtx("wan2.6-t2i", map[string]any{"model": "wan2.6-t2i", "prompt": "a cat"}))
		require.ErrorContains(t, err, "relay mode only supports video models")
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
```

- [ ] **Step 2: 运行，确认失败**

```bash
go test ./plugins/ -run TestAlibabaRelayMode -count=1
```

Expected: FAIL，提交请求的 url 还是 DashScope 地址，查询解析返回 DashScope 的结果。

- [ ] **Step 3: 版本号**

`plugins/tasks/alibaba/plugin.js`：把 `  version: "1.4.1",` 改成 `  version: "1.5.0",`。

- [ ] **Step 4: 中转站辅助函数**

在 `export function buildSubmitRequest(ctx) {` 这一行前面插入：

```js
// ============================================================================
// Relay mode
//
// A channel whose task_plugin_config carries `relay` submits to and polls a
// third-party relay instead of DashScope. Client entries, request validation,
// reservation and the DashScope-shaped task data stay the same; the channel
// configuration only declares the relay's wire format (validateChannelConfig
// documents the accepted shape).

const RELAY_STATUSES = ["PENDING", "RUNNING", "SUCCEEDED", "FAILED", "CANCELED"];
// Fields of the normalized DashScope request that "$." references may read.
const RELAY_REQUEST_FIELDS = [
  "model",
  "input.prompt",
  "input.negative_prompt",
  "input.template",
  "input.media",
  "parameters.resolution",
  "parameters.ratio",
  "parameters.size",
  "parameters.duration",
  "parameters.audio",
  "parameters.watermark",
  "parameters.seed",
  "parameters.prompt_extend",
  "parameters.shot_type",
];
const RELAY_QUERY_FIELDS = ["task_id"];
const RELAY_FORBIDDEN_HEADERS = ["host", "content-length", "transfer-encoding", "connection"];
const RELAY_TEMPLATE_MAX_DEPTH = 16;
const RELAY_RESPONSE_PATH = /^[A-Za-z0-9_-]+(\.[A-Za-z0-9_-]+)*$/;
const RELAY_VALUES_PATH = /^[A-Za-z0-9_-]+(\[\])?(\.[A-Za-z0-9_-]+(\[\])?)*$/;
const RELAY_HEADER_NAME = /^[A-Za-z0-9!#$%&'*+.^_`|~-]+$/;
// Marks an unresolved template reference so the enclosing member is omitted.
const RELAY_MISSING = {};

function relayConfig(ctx) {
  const config = ctx && ctx.channelConfig;
  return config && typeof config === "object" && config.relay && typeof config.relay === "object" ? config.relay : null;
}

// relayGet reads a dot-separated path; numeric segments index arrays. Only own
// properties are followed, so prototype members never resolve.
function relayGet(value, path) {
  let current = value;
  for (const segment of String(path).split(".")) {
    if (current === null || typeof current !== "object" || !Object.prototype.hasOwnProperty.call(current, segment)) return undefined;
    current = current[segment];
  }
  return current;
}

function relayText(value) {
  if (value === undefined || value === null) return "";
  return typeof value === "string" ? value.trim() : JSON.stringify(value);
}

// relayValueKey is the string form used to match ok.equals, statusMap keys and
// values mappings, so 200 matches "200" and -1 matches "-1".
function relayValueKey(value) {
  return typeof value === "string" ? value : JSON.stringify(value);
}

function relayNumber(value) {
  if (typeof value === "string" && value.trim() !== "" && Number.isFinite(Number(value))) return Number(value);
  return value;
}

function relayOK(spec, body) {
  if (!spec.ok) return true;
  const actual = relayGet(body, spec.ok.path);
  return actual !== undefined && relayValueKey(actual) === relayValueKey(spec.ok.equals);
}

function relayURL(ctx, path) {
  return String(ctx.baseUrl || "").replace(/\/+$/, "") + path;
}

function relayHeaders(spec, apiKey, hasBody) {
  const headers = { Authorization: "Bearer " + apiKey };
  if (hasBody) headers["Content-Type"] = "application/json";
  const overrides = spec.headers || {};
  for (const name of Object.keys(overrides)) {
    for (const existing of Object.keys(headers)) {
      if (existing.toLowerCase() === name.toLowerCase()) delete headers[existing];
    }
    if (overrides[name] !== null) headers[name] = String(overrides[name]).split("{key}").join(apiKey);
  }
  return headers;
}

// renderRelayTemplate resolves "$." references against scope and "@."
// references against the current $each item; other values are constants.
function renderRelayTemplate(template, scope, item, depth) {
  if (depth > RELAY_TEMPLATE_MAX_DEPTH) throw new Error("relay template is nested too deeply");
  if (typeof template === "string") {
    if (template.startsWith("$.")) {
      const value = relayGet(scope, template.slice(2));
      return value === undefined ? RELAY_MISSING : value;
    }
    if (template.startsWith("@.")) {
      const value = item === undefined ? undefined : relayGet(item, template.slice(2));
      return value === undefined ? RELAY_MISSING : value;
    }
    return template;
  }
  if (Array.isArray(template)) {
    const rendered = [];
    for (const element of template) {
      const value = renderRelayTemplate(element, scope, item, depth + 1);
      if (value !== RELAY_MISSING) rendered.push(value);
    }
    return rendered;
  }
  if (template === null || typeof template !== "object") return template;
  if (Object.prototype.hasOwnProperty.call(template, "$each")) {
    const source = renderRelayTemplate(template.$each, scope, item, depth + 1);
    if (!Array.isArray(source)) return RELAY_MISSING;
    return source.map((entry) => renderRelayObject(template, scope, entry, depth + 1));
  }
  return renderRelayObject(template, scope, item, depth);
}

function renderRelayObject(template, scope, item, depth) {
  const rendered = {};
  for (const key of Object.keys(template)) {
    if (key === "$each") continue;
    const value = renderRelayTemplate(template[key], scope, item, depth + 1);
    if (value !== RELAY_MISSING) rendered[key] = value;
  }
  return rendered;
}

// applyRelayValue rewrites one leaf of the rendered body in place. A segment
// ending in "[]" visits every array element; a mapping to null deletes.
function applyRelayValue(container, segments, mapping) {
  if (container === null || typeof container !== "object") return;
  const each = segments[0].endsWith("[]");
  const key = each ? segments[0].slice(0, -2) : segments[0];
  if (!Object.prototype.hasOwnProperty.call(container, key)) return;
  if (each) {
    if (!Array.isArray(container[key])) return;
    for (const element of container[key]) applyRelayValue(element, segments.slice(1), mapping);
    return;
  }
  if (segments.length > 1) {
    applyRelayValue(container[key], segments.slice(1), mapping);
    return;
  }
  const valueKey = relayValueKey(container[key]);
  if (!Object.prototype.hasOwnProperty.call(mapping, valueKey)) return;
  if (mapping[valueKey] === null) delete container[key];
  else container[key] = mapping[valueKey];
}

// relaySnapshot converts a relay query response into the DashScope task shape
// that presenters, artifacts and extractUsageOnComplete already read.
function relaySnapshot(ctx, spec, body, status) {
  const output = { task_id: String(ctx.taskId || ""), task_status: status };
  const snapshot = { request_id: "", output: output };
  if (status === "SUCCEEDED") {
    for (const path of Array.isArray(spec.videoUrl) ? spec.videoUrl : [spec.videoUrl]) {
      const url = relayGet(body, path);
      if (typeof url === "string" && url.trim()) {
        output.video_url = url.trim();
        break;
      }
    }
  }
  if (status === "FAILED" || status === "CANCELED") {
    output.code = "UpstreamTaskFailed";
    const reason = spec.reason ? relayText(relayGet(body, spec.reason)) : "";
    if (reason) output.message = reason;
  }
  const usage = spec.usage || {};
  const seconds = usage.seconds ? relayNumber(relayGet(body, usage.seconds)) : undefined;
  if (seconds !== undefined && seconds !== null) {
    const inputSeconds = usage.inputSeconds ? relayNumber(relayGet(body, usage.inputSeconds)) : undefined;
    snapshot.usage = {
      duration: seconds,
      output_video_duration: seconds,
      input_video_duration: inputSeconds === undefined || inputSeconds === null ? 0 : inputSeconds,
    };
    const resolution = usage.resolution ? relayGet(body, usage.resolution) : undefined;
    if (resolution !== undefined && resolution !== null) snapshot.usage.SR = resolution;
  }
  return snapshot;
}

```

- [ ] **Step 5: 提交请求分支**

把

```js
export function buildSubmitRequest(ctx) {
  if (imageModel(ctx)) {
    const converted = convertImage(ctx);
```

改成

```js
export function buildSubmitRequest(ctx) {
  const relay = relayConfig(ctx);
  if (imageModel(ctx)) {
    if (relay) throw new Error("relay mode only supports video models");
    const converted = convertImage(ctx);
```

再把

```js
  const body = convert(ctx);
  const kind = modelProfile(body.model).kind;
  const service = kind === "frames" || kind === "speech" ? "image2video" : "video-generation";
```

改成

```js
  const body = convert(ctx);
  if (relay) {
    const requestBody = utils.json.clone(renderRelayObject(relay.submit.body, body, undefined, 0));
    const values = relay.submit.values || {};
    for (const path of Object.keys(values)) applyRelayValue(requestBody, path.split("."), values[path]);
    return {
      url: relayURL(ctx, relay.submit.path),
      method: "POST",
      headers: relayHeaders(relay.submit, ctx.apiKey, true),
      body: requestBody,
      action: videoAction(body.input),
    };
  }
  const kind = modelProfile(body.model).kind;
  const service = kind === "frames" || kind === "speech" ? "image2video" : "video-generation";
```

- [ ] **Step 6: 提交返回分支**

把

```js
export function parseSubmitResponse(ctx, resp) {
  const body = resp.body || {};
  if (body.code) throw new Error(body.code + ": " + (body.message || ""));
```

改成

```js
export function parseSubmitResponse(ctx, resp) {
  const body = resp.body || {};
  const relay = relayConfig(ctx);
  if (relay) {
    const spec = relay.submit;
    if (!relayOK(spec, body)) throw new Error((spec.error ? relayText(relayGet(body, spec.error)) : "") || "relay returned a failure");
    const rawTaskId = relayGet(body, spec.taskId);
    const taskId = typeof rawTaskId === "string" || typeof rawTaskId === "number" ? String(rawTaskId).trim() : "";
    if (!taskId) throw new Error("relay response has no task id at " + spec.taskId);
    return { taskId: taskId, taskData: { request_id: "", output: { task_id: taskId, task_status: "PENDING" } } };
  }
  if (body.code) throw new Error(body.code + ": " + (body.message || ""));
```

- [ ] **Step 7: 查询请求分支**

把

```js
export function buildQueryRequest(ctx) {
  return { url: apiRoot(ctx) + "/api/v1/tasks/" + ctx.taskId, method: "GET", headers: { Authorization: "Bearer " + ctx.apiKey } };
}
```

改成

```js
export function buildQueryRequest(ctx) {
  const relay = relayConfig(ctx);
  if (relay) {
    const spec = relay.query;
    const post = spec.method === "POST";
    const descriptor = {
      url: relayURL(ctx, spec.path.split("{task_id}").join(encodeURIComponent(ctx.taskId))),
      method: post ? "POST" : "GET",
      headers: relayHeaders(spec, ctx.apiKey, post),
    };
    if (post) descriptor.body = utils.json.clone(renderRelayObject(spec.body || {}, { task_id: ctx.taskId }, undefined, 0));
    return descriptor;
  }
  return { url: apiRoot(ctx) + "/api/v1/tasks/" + ctx.taskId, method: "GET", headers: { Authorization: "Bearer " + ctx.apiKey } };
}
```

- [ ] **Step 8: 查询返回分支**

把

```js
export function parseTaskResult(ctx, body) {
  const output = body.output || {};
```

改成

```js
export function parseTaskResult(ctx, body) {
  const relay = relayConfig(ctx);
  if (relay) {
    const spec = relay.query;
    if (!relayOK(spec, body)) return { status: "UNKNOWN", reason: "relay query was not successful" };
    const rawStatus = relayGet(body, spec.status);
    const statusKey = rawStatus === undefined || rawStatus === null ? "" : relayValueKey(rawStatus);
    const status = Object.prototype.hasOwnProperty.call(spec.statusMap, statusKey) ? spec.statusMap[statusKey] : undefined;
    if (!RELAY_STATUSES.includes(status)) return { status: "UNKNOWN", reason: "unrecognized relay status: " + relayText(rawStatus) };
    const snapshot = relaySnapshot(ctx, spec, body, status);
    if (status === "PENDING") return { status: "QUEUED", data: snapshot };
    if (status === "RUNNING") return { status: "IN_PROGRESS", data: snapshot };
    if (status === "SUCCEEDED") {
      if (!snapshot.output.video_url) return { status: "UNKNOWN", reason: "relay reported success without a video URL" };
      return { status: "SUCCESS", url: snapshot.output.video_url, data: snapshot };
    }
    return { status: "FAILURE", reason: snapshot.output.message || "task failed", data: snapshot };
  }
  const output = body.output || {};
```

- [ ] **Step 9: 运行，确认通过**

```bash
go test ./plugins/ -count=1
```

Expected: `TestAlibabaRelayMode` 和现有阿里插件测试全部 PASS（非中转站路径不受影响）。

- [ ] **Step 10: 提交**

```bash
git add plugins/tasks/alibaba/plugin.js plugins/alibaba_relay_test.go
git commit -m "feat(plugins/alibaba): relay mode driven by channel task_plugin_config"
```

---

### Task 5: 阿里插件 `validateChannelConfig`

**Files:**
- Modify: `plugins/tasks/alibaba/plugin.js`（文件末尾追加）
- Test: `plugins/alibaba_relay_test.go`、`controller/channel_task_plugin_validation_test.go`

**Interfaces:**
- Consumes: Task 4 的 `RELAY_*` 常量、`tokenPonyRelayConfig`；Task 3 的 `validateChannel` 接入。
- Produces: 导出函数 `validateChannelConfig(config)`，不合法时抛出以字段路径开头的英文错误。

- [ ] **Step 1: 写失败测试（插件）**

在 `plugins/alibaba_relay_test.go` 末尾追加：

```go
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
		{"unknown mapped status", func(_, _, query map[string]any) { query["statusMap"] = map[string]any{"DONE": "SUCCESS"} }, "statusMap values must be one of"},
		{"no failure status", func(_, _, query map[string]any) { query["statusMap"] = map[string]any{"DONE": "SUCCEEDED"} }, "must map a success status and a failure status"},
		{"missing video URL path", func(_, _, query map[string]any) { delete(query, "videoUrl") }, "relay.query.videoUrl must be a dot-separated response path"},
		{"query body with GET", func(_, _, query map[string]any) { query["body"] = map[string]any{"id": "$.task_id"} }, "relay.query.body requires method POST"},
		{"unknown usage field", func(_, _, query map[string]any) { query["usage"] = map[string]any{"cost": "data.cost"} }, "relay.query.usage has unknown field cost"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			config := relayConfigValue(t, tokenPonyRelayConfig)
			relay := config["relay"].(map[string]any)
			tc.mutate(config, relay["submit"].(map[string]any), relay["query"].(map[string]any))
			require.ErrorContains(t, validate(config), tc.wantErr)
		})
	}
}
```

- [ ] **Step 2: 写失败测试（保存渠道时用真实阿里插件）**

在 `controller/channel_task_plugin_validation_test.go` 末尾追加：

```go
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
```

- [ ] **Step 3: 运行，确认失败**

```bash
go test ./plugins/ -run TestAlibabaValidateChannelConfig -count=1
go test ./controller/ -run TestValidateChannelAcceptsAlibabaRelayConfig -count=1
```

Expected: 插件测试报 `validateChannelConfig` 不存在；controller 测试报 `does not accept a channel configuration`。

- [ ] **Step 4: 实现**

`plugins/tasks/alibaba/plugin.js` 文件末尾（`protocols` 对象结束的 `};` 之后）追加：

```js

function requireRelayObject(value, where) {
  if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error(where + " must be an object");
}

function requireRelayKeys(value, where, allowed) {
  for (const key of Object.keys(value)) {
    if (!allowed.includes(key)) throw new Error(where + " has unknown field " + key);
  }
}

function validateRelayPath(path, where) {
  if (typeof path !== "string" || !path.startsWith("/")) throw new Error(where + " must start with /");
  if (path.length > 512 || path.includes("..") || path.includes("//") || path.includes("#") || /[\s\u0000-\u001f\u007f]/.test(path)) {
    throw new Error(where + " must not contain .., //, #, spaces or control characters");
  }
}

function validateRelayResponsePath(path, where, required) {
  if (path === undefined && !required) return;
  if (typeof path !== "string" || !RELAY_RESPONSE_PATH.test(path)) throw new Error(where + " must be a dot-separated response path");
}

function validateRelayHeaders(headers, where) {
  if (headers === undefined) return;
  requireRelayObject(headers, where);
  for (const name of Object.keys(headers)) {
    if (!RELAY_HEADER_NAME.test(name) || RELAY_FORBIDDEN_HEADERS.includes(name.toLowerCase())) throw new Error(where + " cannot set header " + name);
    const value = headers[name];
    if (value !== null && (typeof value !== "string" || /[\r\n]/.test(value))) throw new Error(where + "." + name + " must be a single-line string or null");
  }
}

function validateRelayOK(ok, where) {
  if (ok === undefined) return;
  requireRelayObject(ok, where);
  requireRelayKeys(ok, where, ["path", "equals"]);
  validateRelayResponsePath(ok.path, where + ".path", true);
  const kind = typeof ok.equals;
  if (kind !== "string" && kind !== "number" && kind !== "boolean") throw new Error(where + ".equals must be a string, number or boolean");
}

function validateRelayTemplate(value, where, fields, inEach, depth) {
  if (depth > RELAY_TEMPLATE_MAX_DEPTH) throw new Error(where + " is nested too deeply");
  if (typeof value === "string") {
    if (value.startsWith("$.") && !fields.includes(value.slice(2))) throw new Error(where + " references unknown field " + value);
    if (value.startsWith("@.")) {
      if (!inEach) throw new Error(where + " uses " + value + " outside $each");
      if (!RELAY_RESPONSE_PATH.test(value.slice(2))) throw new Error(where + " has an invalid item path " + value);
    }
    return;
  }
  if (Array.isArray(value)) {
    value.forEach((element, index) => validateRelayTemplate(element, where + "[" + index + "]", fields, inEach, depth + 1));
    return;
  }
  if (value === null || typeof value !== "object") return;
  if (Object.prototype.hasOwnProperty.call(value, "$each")) {
    if (typeof value.$each !== "string" || !value.$each.startsWith("$.") || !fields.includes(value.$each.slice(2))) {
      throw new Error(where + ".$each must reference a request field");
    }
    if (Object.keys(value).length < 2) throw new Error(where + ".$each needs at least one mapped field");
    for (const key of Object.keys(value)) {
      if (key !== "$each") validateRelayTemplate(value[key], where + "." + key, fields, true, depth + 1);
    }
    return;
  }
  for (const key of Object.keys(value)) validateRelayTemplate(value[key], where + "." + key, fields, inEach, depth + 1);
}

// validateChannelConfig accepts the channel task_plugin_config described in
// the relay mode section. The host calls it whenever a channel that stores a
// configuration is saved.
export function validateChannelConfig(config) {
  requireRelayObject(config, "configuration");
  for (const key of Object.keys(config)) {
    if (key !== "relay") throw new Error("unknown configuration field: " + key);
  }
  const relay = config.relay;
  requireRelayObject(relay, "relay");
  requireRelayKeys(relay, "relay", ["submit", "query"]);

  const submit = relay.submit;
  requireRelayObject(submit, "relay.submit");
  requireRelayKeys(submit, "relay.submit", ["path", "method", "headers", "body", "values", "ok", "taskId", "error"]);
  validateRelayPath(submit.path, "relay.submit.path");
  if (submit.method !== undefined && submit.method !== "POST") throw new Error("relay.submit.method must be POST");
  validateRelayHeaders(submit.headers, "relay.submit.headers");
  requireRelayObject(submit.body, "relay.submit.body");
  validateRelayTemplate(submit.body, "relay.submit.body", RELAY_REQUEST_FIELDS, false, 0);
  if (submit.values !== undefined) {
    requireRelayObject(submit.values, "relay.submit.values");
    for (const path of Object.keys(submit.values)) {
      if (!RELAY_VALUES_PATH.test(path) || path.endsWith("[]")) throw new Error("relay.submit.values has an invalid path " + path);
      requireRelayObject(submit.values[path], "relay.submit.values." + path);
    }
  }
  validateRelayOK(submit.ok, "relay.submit.ok");
  validateRelayResponsePath(submit.taskId, "relay.submit.taskId", true);
  validateRelayResponsePath(submit.error, "relay.submit.error", false);

  const query = relay.query;
  requireRelayObject(query, "relay.query");
  requireRelayKeys(query, "relay.query", ["path", "method", "headers", "body", "ok", "status", "statusMap", "videoUrl", "reason", "usage"]);
  validateRelayPath(query.path, "relay.query.path");
  if (query.method !== undefined && query.method !== "GET" && query.method !== "POST") throw new Error("relay.query.method must be GET or POST");
  validateRelayHeaders(query.headers, "relay.query.headers");
  if (query.body !== undefined) {
    if (query.method !== "POST") throw new Error("relay.query.body requires method POST");
    requireRelayObject(query.body, "relay.query.body");
    validateRelayTemplate(query.body, "relay.query.body", RELAY_QUERY_FIELDS, false, 0);
  }
  if (!query.path.includes("{task_id}") && !JSON.stringify(query.body || {}).includes('"$.task_id"')) {
    throw new Error("relay.query must reference the task id with {task_id} in path or $.task_id in body");
  }
  validateRelayOK(query.ok, "relay.query.ok");
  validateRelayResponsePath(query.status, "relay.query.status", true);
  requireRelayObject(query.statusMap, "relay.query.statusMap");
  const mapped = Object.keys(query.statusMap).map((key) => query.statusMap[key]);
  for (const value of mapped) {
    if (!RELAY_STATUSES.includes(value)) throw new Error("relay.query.statusMap values must be one of " + RELAY_STATUSES.join(", "));
  }
  if (!mapped.includes("SUCCEEDED") || !(mapped.includes("FAILED") || mapped.includes("CANCELED"))) {
    throw new Error("relay.query.statusMap must map a success status and a failure status");
  }
  for (const path of Array.isArray(query.videoUrl) && query.videoUrl.length ? query.videoUrl : [query.videoUrl]) {
    validateRelayResponsePath(path, "relay.query.videoUrl", true);
  }
  validateRelayResponsePath(query.reason, "relay.query.reason", false);
  if (query.usage !== undefined) {
    requireRelayObject(query.usage, "relay.query.usage");
    requireRelayKeys(query.usage, "relay.query.usage", ["seconds", "inputSeconds", "resolution"]);
    for (const key of Object.keys(query.usage)) validateRelayResponsePath(query.usage[key], "relay.query.usage." + key, true);
  }
}
```

- [ ] **Step 5: 运行，确认通过**

```bash
go test ./plugins/ ./controller/ -count=1
go run . plugin lint plugins/tasks/alibaba/plugin.js
```

Expected: 测试全部 PASS；lint 无错误输出、退出码 0。

- [ ] **Step 6: 提交**

```bash
git add plugins/tasks/alibaba/plugin.js plugins/alibaba_relay_test.go controller/channel_task_plugin_validation_test.go
git commit -m "feat(plugins/alibaba): validate relay channel configuration"
```

---

### Task 6: 配置预览接口

**Files:**
- Modify: `controller/channel_task_plugin_config.go`（追加预览处理函数）
- Modify: `router/channel-router.go`（`channelPermissionRoutes`）
- Test: `controller/channel_task_plugin_validation_test.go`

**Interfaces:**
- Consumes: Task 3 的 `validateChannelTaskPluginConfig`、`taskPluginHookMessage`；Task 4、5 的阿里插件钩子。
- Produces: `func PreviewChannelTaskPluginConfig(c *gin.Context)`，路由 `POST /api/channel/task_plugin_config/preview`（权限 `authz.ChannelWrite`）。请求和返回字段见 spec 第 9 节；返回的 `data` 里有 `submit`、`reservation`、`submit_result.task_id`、`query`、`query_result{status,url,reason,snapshot}`、`settlement`、`errors{submit,submit_result,query_result}`。

- [ ] **Step 1: 写失败测试**

在 `controller/channel_task_plugin_validation_test.go` 末尾追加（import 块需要有 `net/http`、`net/http/httptest`、`github.com/QuantumNous/new-api/common`、`github.com/gin-gonic/gin`，缺的补上）：

```go
const previewRelayConfig = `{"relay":{"submit":{"path":"/v1/aigc/video/generate","body":{"model":"$.model","prompt":"$.input.prompt","resolution":"$.parameters.resolution","ratio":"$.parameters.ratio","duration":"$.parameters.duration"},"ok":{"path":"code","equals":200},"taskId":"data.id","error":"message"},` +
	`"query":{"path":"/v1/aigc/tasks/{task_id}","ok":{"path":"code","equals":200},"status":"data.task_status","statusMap":{"RUNNING":"RUNNING","COMPLETED":"SUCCEEDED","FAILED":"FAILED"},"videoUrl":"data.result.output","usage":{"seconds":"data.usage.duration","resolution":"data.usage.resolution"}}}}`

func runTaskPluginConfigPreview(t *testing.T, payload string) (bool, string, map[string]any) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/channel/task_plugin_config/preview", strings.NewReader(payload))
	c.Request.Header.Set("Content-Type", "application/json")
	PreviewChannelTaskPluginConfig(c)
	var response struct {
		Success bool           `json:"success"`
		Message string         `json:"message"`
		Data    map[string]any `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	return response.Success, response.Message, response.Data
}

func TestPreviewChannelTaskPluginConfigRunsRelayHooksOffline(t *testing.T) {
	success, message, data := runTaskPluginConfigPreview(t, `{
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
	queryResult := data["query_result"].(map[string]any)
	assert.Equal(t, "SUCCESS", queryResult["status"])
	assert.Equal(t, "https://cdn.example/v.mp4", queryResult["url"])
	assert.Equal(t, "SUCCEEDED", queryResult["snapshot"].(map[string]any)["output"].(map[string]any)["task_status"])
	assert.Equal(t, map[string]any{"seconds": float64(5), "resolution": "720P"}, data["settlement"])
	assert.Empty(t, data["errors"])
}

func TestPreviewChannelTaskPluginConfigNativeEntryAndStepErrors(t *testing.T) {
	success, message, data := runTaskPluginConfigPreview(t, `{
	  "type": 17, "base_url": "https://relay.example", "config": `+previewRelayConfig+`,
	  "model": "wan3.0-video", "entry": "/ali/api/v1/services/aigc/video-generation/video-synthesis",
	  "request": {"model": "wan3.0-video", "input": {"prompt": "a dog"}, "parameters": {"resolution": "720P", "duration": 5}},
	  "submit_response": {"code": 10108, "message": "bad duration"}
	}`)
	require.True(t, success, message)
	assert.Equal(t, "a dog", data["submit"].(map[string]any)["body"].(map[string]any)["prompt"])
	assert.Contains(t, data["errors"].(map[string]any)["submit_result"], "bad duration")
	assert.NotContains(t, data, "query_result")

	success, message, _ = runTaskPluginConfigPreview(t, `{"type": 61, "task_plugin_key": "alibaba", "config": {"relay": {}}, "model": "wan3.0-video", "entry": "openai_video"}`)
	assert.False(t, success)
	assert.Contains(t, message, "Invalid task plugin configuration")
}
```

- [ ] **Step 2: 运行，确认失败**

```bash
go test ./controller/ -run 'TestPreviewChannelTaskPluginConfig' -count=1
```

Expected: 编译失败，`PreviewChannelTaskPluginConfig` 未定义。

- [ ] **Step 3: 实现预览处理函数**

在 `controller/channel_task_plugin_config.go` 的 import 块补上 `"encoding/json"`、`"maps"`、`"net/http"`、`"github.com/gin-gonic/gin"`，文件末尾追加：

```go
const (
	maxTaskPluginConfigPreviewInputBytes = 1 << 20
	taskPluginConfigPreviewAPIKey        = "sk-****"
	taskPluginConfigPreviewTaskID        = "preview-task-id"
)

type taskPluginConfigPreviewRequest struct {
	Type           int             `json:"type"`
	TaskPluginKey  string          `json:"task_plugin_key"`
	BaseURL        string          `json:"base_url"`
	Config         json.RawMessage `json:"config"`
	Model          string          `json:"model"`
	Entry          string          `json:"entry"`
	Request        json.RawMessage `json:"request"`
	SubmitResponse json.RawMessage `json:"submit_response"`
	QueryResponse  json.RawMessage `json:"query_response"`
}

// PreviewChannelTaskPluginConfig runs an unsaved task_plugin_config through
// the executing plugin's decoder and driver hooks against sample requests and
// responses. It sends no network request, writes nothing and bills nothing;
// each step that fails is reported under errors without stopping the others.
func PreviewChannelTaskPluginConfig(c *gin.Context) {
	var request taskPluginConfigPreviewRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		common.ApiErrorMsg(c, "invalid preview request")
		return
	}
	for _, input := range []json.RawMessage{request.Request, request.SubmitResponse, request.QueryResponse} {
		if len(input) > maxTaskPluginConfigPreviewInputBytes {
			common.ApiErrorMsg(c, "preview input must not exceed 1 MiB")
			return
		}
	}
	ctx := c.Request.Context()
	config, plugin, err := validateChannelTaskPluginConfig(ctx, request.Type, dto.ChannelSettings{TaskPluginKey: request.TaskPluginKey, TaskPluginConfig: request.Config})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if config == nil {
		common.ApiErrorMsg(c, "task_plugin_config is empty")
		return
	}

	baseURL := strings.TrimRight(strings.TrimSpace(request.BaseURL), "/")
	authHeader := "Bearer " + taskPluginConfigPreviewAPIKey
	driver := map[string]any{
		"model": request.Model, "upstreamModel": request.Model, "baseUrl": baseURL,
		"apiKey": taskPluginConfigPreviewAPIKey, "authHeader": authHeader, "auth": map[string]any{"authHeader": authHeader},
		"upstream": map[string]any{"kind": jsplugin.UpstreamKindVendor}, "requestHeaders": map[string]any{}, "files": []any{},
		"publicTaskId": taskPluginConfigPreviewTaskID, "action": "", "channelConfig": config,
	}
	result := gin.H{}
	failures := gin.H{}

	if len(request.Request) > 0 {
		descriptor, reservation, submitErr := previewTaskPluginSubmit(ctx, plugin, request, driver)
		if submitErr != nil {
			failures["submit"] = submitErr.Error()
		} else {
			result["submit"] = descriptor
			result["reservation"] = reservation
		}
	}

	taskID := taskPluginConfigPreviewTaskID
	var taskData any
	if len(request.SubmitResponse) > 0 {
		var body any
		if err := common.Unmarshal(request.SubmitResponse, &body); err != nil {
			failures["submit_result"] = "submit_response must be valid JSON"
		} else if value, err := plugin.Engine.Call(ctx, "parseSubmitResponse", driver, map[string]any{"statusCode": http.StatusOK, "headers": map[string]any{}, "body": body}); err != nil {
			failures["submit_result"] = taskPluginHookMessage(err)
		} else if parsed, _ := value.(map[string]any); parsed != nil {
			taskID = fmt.Sprint(parsed["taskId"])
			taskData = parsed["taskData"]
			result["submit_result"] = gin.H{"task_id": taskID}
		}
	}

	if len(request.QueryResponse) > 0 {
		query := map[string]any{
			"taskId": taskID, "publicTaskId": taskPluginConfigPreviewTaskID, "action": driver["action"],
			"model": driver["model"], "upstreamModel": driver["upstreamModel"], "baseUrl": baseURL,
			"apiKey": taskPluginConfigPreviewAPIKey, "authHeader": authHeader, "auth": driver["auth"], "upstream": driver["upstream"],
			"data": taskData, "state": nil, "channelConfig": config,
		}
		var body any
		if err := common.Unmarshal(request.QueryResponse, &body); err != nil {
			failures["query_result"] = "query_response must be valid JSON"
		} else if descriptor, err := plugin.Engine.Call(ctx, "buildQueryRequest", query); err != nil {
			failures["query_result"] = taskPluginHookMessage(err)
		} else if value, err := plugin.Engine.Call(ctx, "parseTaskResult", query, body, map[string]any{"status": http.StatusOK, "headers": map[string]any{}}); err != nil {
			result["query"] = descriptor
			failures["query_result"] = taskPluginHookMessage(err)
		} else {
			parsed, _ := value.(map[string]any)
			result["query"] = descriptor
			result["query_result"] = gin.H{"status": parsed["status"], "url": parsed["url"], "reason": parsed["reason"], "snapshot": parsed["data"]}
			usageBody := parsed["data"]
			if usageBody == nil {
				usageBody = body
			}
			if plugin.Engine.HasExport("extractUsageOnComplete") {
				if facts, err := plugin.Engine.Call(ctx, "extractUsageOnComplete", query, parsed, usageBody); err == nil {
					result["settlement"] = facts
				}
			}
		}
	}

	result["errors"] = failures
	common.ApiSuccess(c, result)
}

// previewTaskPluginSubmit decodes the sample client request through the entry
// it was written for (openai_video, or a native submit route of the plugin),
// builds the submit request against the preview credentials and reports the
// reservation facts. The decoded body and action are written into driver for
// the later preview steps.
func previewTaskPluginSubmit(ctx context.Context, plugin *jsplugin.LoadedPlugin, request taskPluginConfigPreviewRequest, driver map[string]any) (any, any, error) {
	var body any
	if err := common.Unmarshal(request.Request, &body); err != nil {
		return nil, nil, fmt.Errorf("request must be valid JSON")
	}
	jsonBody := map[string]any{"kind": "json", "value": body}
	var decoded any
	var err error
	if request.Entry == "openai_video" {
		decoded, err = plugin.Engine.CallPath(ctx, "protocols", []string{"openai_video", "decodeRequest"}, map[string]any{"model": request.Model, "body": jsonBody})
	} else {
		decode := ""
		for _, route := range plugin.Meta.Routes {
			if route.Type == jsplugin.RouteTypeSubmit && strings.EqualFold(route.Method, http.MethodPost) && route.Path == request.Entry {
				decode = route.Decode
				break
			}
		}
		if decode == "" {
			return nil, nil, fmt.Errorf("entry %q is neither openai_video nor a native submit route of the plugin", request.Entry)
		}
		decoded, err = plugin.Engine.CallPath(ctx, "native", []string{decode}, map[string]any{"body": jsonBody, "path": request.Entry, "method": http.MethodPost})
	}
	if err != nil {
		return nil, nil, errors.New(taskPluginHookMessage(err))
	}
	intent, _ := decoded.(map[string]any)
	driver["requestBody"] = intent["requestBody"]
	if action, ok := intent["action"].(string); ok {
		driver["action"] = action
	}
	if model, ok := intent["model"].(string); ok && model != "" {
		driver["model"], driver["upstreamModel"] = model, model
	}

	descriptor, err := plugin.Engine.Call(ctx, "buildSubmitRequest", driver)
	if err != nil {
		return nil, nil, errors.New(taskPluginHookMessage(err))
	}
	described, _ := descriptor.(map[string]any)
	url, _ := described["url"].(string)
	if err := jsplugin.ValidateRequestURL(url, fmt.Sprint(driver["baseUrl"]), plugin.Meta.AllowedHosts); err != nil {
		return nil, nil, err
	}
	var reservation any
	if plugin.Engine.HasExport("extractUsage") {
		usageCtx := maps.Clone(driver)
		usageCtx["usagePurpose"] = "facts"
		if facts, err := plugin.Engine.Call(ctx, "extractUsage", usageCtx); err == nil {
			reservation = facts
		}
	}
	return descriptor, reservation, nil
}
```

说明：`previewTaskPluginSubmit` 只有一个调用方，单独拆出是因为它是「按入口解码再构造提交请求」这一个完整步骤，放回主函数会让三段预览互相缠在一起。

- [ ] **Step 4: 注册路由**

`router/channel-router.go` 的 `channelPermissionRoutes` 列表里，在 `/fetch_models` 那一项后面加：

```go
	{method: http.MethodPost, path: "/task_plugin_config/preview", permission: authz.ChannelWrite, handler: controller.PreviewChannelTaskPluginConfig},
```

- [ ] **Step 5: 运行，确认通过**

```bash
gofmt -w controller/channel_task_plugin_config.go router/channel-router.go controller/channel_task_plugin_validation_test.go
go test ./controller/ -run 'TestPreviewChannelTaskPluginConfig|TestValidateChannel' -count=1
go test ./router/ -run 'TestAccessToken' -count=1
```

Expected: 全部 PASS（访问令牌权限测试会自动覆盖新路由）。

- [ ] **Step 6: 提交**

```bash
git add controller/channel_task_plugin_config.go router/channel-router.go controller/channel_task_plugin_validation_test.go
git commit -m "feat(channel): offline preview for task_plugin_config"
```

---

### Task 7: 端到端测试（模拟中转站）

**Files:**
- Test: `router/video_router_test.go`（扩充现有文件）

**Interfaces:**
- Consumes: Task 1 到 Task 5 的全部改动。

- [ ] **Step 1: 写测试**

在 `router/video_router_test.go` 的 import 块补上（已有的不重复）：`"context"`、`"fmt"`、`"io"`、`"strings"`、`"sync"`、`"time"`、`"bytes"`、`"github.com/QuantumNous/new-api/i18n"`、`"github.com/QuantumNous/new-api/relay"`、`"github.com/QuantumNous/new-api/service"`、`"github.com/QuantumNous/new-api/setting/operation_setting"`、`"github.com/tidwall/gjson"`。文件末尾追加：

```go
const relayE2EConfig = `{"relay":{"submit":{"path":"/v1/aigc/video/generate","body":{"model":"$.model","prompt":"$.input.prompt","media":"$.input.media","resolution":"$.parameters.resolution","ratio":"$.parameters.ratio","duration":"$.parameters.duration"},"ok":{"path":"code","equals":200},"taskId":"data.id","error":"message"},` +
	`"query":{"path":"/v1/aigc/tasks/{task_id}","ok":{"path":"code","equals":200},"status":"data.task_status","statusMap":{"PENDING":"PENDING","RUNNING":"RUNNING","COMPLETED":"SUCCEEDED","FAILED":"FAILED"},"videoUrl":"data.result.output","usage":{"seconds":"data.usage.duration","resolution":"data.usage.resolution"}}}}`

func TestRelayChannelServesOpenAIAndNativeEntriesThroughPolling(t *testing.T) {
	gin.SetMode(gin.TestMode)
	require.NoError(t, i18n.Init())

	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousDatabaseType, previousLogDatabaseType := common.MainDatabaseType(), common.LogDatabaseType()
	previousSQLitePath, previousMasterNode, previousRedisEnabled := common.SQLitePath, common.IsMasterNode, common.RedisEnabled
	previousSelfUse, previousFactory := operation_setting.SelfUseModeEnabled, service.GetTaskAdaptorFunc
	common.SQLitePath = t.TempDir() + "/relay-e2e.db"
	common.IsMasterNode = false
	common.RedisEnabled = false
	// Pricing is covered elsewhere; self-use mode lets the unpriced model submit.
	operation_setting.SelfUseModeEnabled = true
	t.Setenv("SQL_DSN", "")
	require.NoError(t, model.InitDB())
	database := model.DB
	model.LOG_DB = database
	require.NoError(t, database.AutoMigrate(&model.User{}, &model.Token{}, &model.Channel{}, &model.Ability{}, &model.Task{}, &model.Log{},
		&model.UserSubscription{}, &model.SubscriptionPlan{}, &model.SubscriptionPreConsumeRecord{}))
	service.GetTaskAdaptorFunc = func(platform constant.TaskPlatform) service.TaskPollingAdaptor {
		adaptor := relay.GetTaskAdaptor(platform)
		if adaptor == nil {
			return nil
		}
		return adaptor
	}
	t.Cleanup(func() {
		sqlDB, closeErr := database.DB()
		require.NoError(t, closeErr)
		require.NoError(t, sqlDB.Close())
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.SetDatabaseTypes(previousDatabaseType, previousLogDatabaseType)
		common.SQLitePath, common.IsMasterNode, common.RedisEnabled = previousSQLitePath, previousMasterNode, previousRedisEnabled
		operation_setting.SelfUseModeEnabled, service.GetTaskAdaptorFunc = previousSelfUse, previousFactory
	})

	var mu sync.Mutex
	var submitted []map[string]any
	relayServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		assert.Equal(t, "Bearer relay-key", r.Header.Get("Authorization"))
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/aigc/video/generate":
			var body map[string]any
			assert.NoError(t, common.DecodeJson(r.Body, &body))
			mu.Lock()
			submitted = append(submitted, body)
			id := fmt.Sprintf("relay-%d", len(submitted))
			mu.Unlock()
			_, _ = w.Write([]byte(`{"code":200,"message":"success","data":{"id":"` + id + `"}}`))
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/aigc/tasks/"):
			_, _ = w.Write([]byte(`{"code":200,"data":{"task_status":"COMPLETED","result":{"output":"https://cdn.example/relay.mp4"},"usage":{"duration":5,"resolution":"720P"}}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer relayServer.Close()

	require.NoError(t, database.Create(&model.User{Id: 1, Username: "root", Role: common.RoleRootUser, Status: common.UserStatusEnabled, Quota: 1_000_000_000_000, Group: "default", AuthVersion: 1}).Error)
	require.NoError(t, database.Create(&model.Token{Id: 1, UserId: 1, Key: "relaye2e", Status: common.TokenStatusEnabled, Name: "relay", ExpiredTime: -1, UnlimitedQuota: true}).Error)
	baseURL := relayServer.URL
	setting := `{"task_plugin_key":"alibaba","task_plugin_config":` + relayE2EConfig + `}`
	relayChannel := &model.Channel{Id: 12, Type: constant.ChannelTypeTaskPlugin, Key: "relay-key", Status: common.ChannelStatusEnabled, Name: "relay",
		Models: "wan3.0-video", Group: "default", BaseURL: &baseURL, Setting: &setting, OtherSettings: `{"disable_task_polling_sleep":true}`}
	require.NoError(t, relayChannel.Insert())

	engine := gin.New()
	SetRelayRouter(engine)
	SetTaskPluginProtocolRouter(engine)
	SetVideoRouter(engine)
	SetTaskRouter(engine)
	engine.NoRoute(SetPluginRouter(engine))
	call := func(method, path, body string) (int, string) {
		var reader io.Reader
		if body != "" {
			reader = bytes.NewBufferString(body)
		}
		request := httptest.NewRequest(method, path, reader)
		request.Header.Set("Authorization", "Bearer sk-relaye2e")
		if body != "" {
			request.Header.Set("Content-Type", "application/json")
		}
		recorder := httptest.NewRecorder()
		engine.ServeHTTP(recorder, request)
		return recorder.Code, recorder.Body.String()
	}

	code, body := call(http.MethodPost, "/v1/videos", `{"model":"wan3.0-video","prompt":"a cat","seconds":5,"resolution":"720P"}`)
	require.Equal(t, http.StatusOK, code, body)
	openAIID := gjson.Get(body, "id").String()
	code, body = call(http.MethodPost, "/ali/api/v1/services/aigc/video-generation/video-synthesis", `{"model":"wan3.0-video","input":{"prompt":"a dog"},"parameters":{"resolution":"720P","duration":5}}`)
	require.Equal(t, http.StatusOK, code, body)
	nativeID := gjson.Get(body, "output.task_id").String()
	require.NotEmpty(t, openAIID)
	require.NotEmpty(t, nativeID)

	mu.Lock()
	require.Len(t, submitted, 2, "both client entries reach the relay")
	assert.Equal(t, map[string]any{"model": "wan3.0-video", "prompt": "a cat", "resolution": "720P", "ratio": "adaptive", "duration": float64(5)}, submitted[0])
	assert.Equal(t, "a dog", submitted[1]["prompt"])
	mu.Unlock()

	// Tasks submitted in this instant are still eligible; backdating only
	// guards against a minimum-age filter in the poller.
	require.NoError(t, database.Model(&model.Task{}).Where("1 = 1").Update("submit_time", time.Now().Add(-time.Minute).Unix()).Error)
	service.RunTaskPollingOnce(context.Background(), nil)

	code, body = call(http.MethodGet, "/ali/api/v1/tasks/"+nativeID, "")
	require.Equal(t, http.StatusOK, code, body)
	assert.Equal(t, nativeID, gjson.Get(body, "output.task_id").String())
	assert.Equal(t, "SUCCEEDED", gjson.Get(body, "output.task_status").String())
	assert.Equal(t, "https://cdn.example/relay.mp4", gjson.Get(body, "output.video_url").String())
	assert.False(t, gjson.Get(body, "data").Exists(), "the relay envelope is not persisted")
	assert.False(t, gjson.Get(body, "code").Exists())

	code, body = call(http.MethodGet, "/v1/videos/"+openAIID, "")
	require.Equal(t, http.StatusOK, code, body)
	assert.Equal(t, "completed", gjson.Get(body, "status").String())
}
```

- [ ] **Step 2: 运行**

```bash
gofmt -w router/video_router_test.go
go test ./router/ -run TestRelayChannelServesOpenAIAndNativeEntriesThroughPolling -count=1 -v
```

Expected: PASS。如果日志里出现 `no such table: xxx`，把对应的 `&model.Xxx{}` 加进 `AutoMigrate` 后重跑；如果轮询没有处理这两个任务（查询仍是 PENDING），检查 `RunTaskPollingOnce` 的筛选条件，并按它的要求准备任务字段，不要改生产代码。

- [ ] **Step 3: 提交**

```bash
git add router/video_router_test.go
git commit -m "test(router): relay channel end to end through both entries and polling"
```

---

### Task 8: 插件接口文档

**Files:**
- Modify: `docs/plugin-api/v1.md`、`docs/plugin-api/v1.d.ts`、`docs/plugin-api/v1.schema.json`

- [ ] **Step 1: v1.md**

在 `## Polling contract` 这一行前面插入：

```markdown
## Channel configuration

A channel may store a JSON object for its executing plugin in `setting.task_plugin_config` (at most 32 KiB). The executing plugin is the bound plugin of a type-61 channel or the plugin indexed by a legacy channel's type; type-60 New API channels reject the field. A plugin opts in by exporting `validateChannelConfig(config)`: the host calls it whenever an administrator saves a channel that stores a configuration and rejects the save with the hook's error message. A configuration is rejected for plugins that do not export the hook.

The host passes the configuration read-only as `ctx.channelConfig` (null when unset) to `DriverContext`, `TaskQueryContext`, `BatchQueryContext`, and the `buildContentRequest` context, for submission and background polling alike. Decoders and presenters do not receive it.

`parseTaskResult` may return `data`. The host then persists that value as `task.data` instead of the upstream response body and passes it as the third argument to `extractUsageOnComplete`; an unencodable value or one larger than 1 MiB is ignored with a warning. A plugin that normalizes a third-party upstream into its vendor shape uses this so presenters, artifacts, and settlement read one format. `parseBatchResult` items already carry `data` with the same meaning.

`POST /api/channel/task_plugin_config/preview` runs a configuration through the executing plugin's decoder and driver hooks against sample requests and responses, without network requests, persistence, or billing.

These are additive apiVersion 1 extensions: plugins that ignore them are unaffected, and older hosts never call `validateChannelConfig` or set `ctx.channelConfig`.

```

- [ ] **Step 2: v1.d.ts**

依次做这几处替换：

- `authHeader: string; upstream: UpstreamContext; files: readonly FileReference[];` → `authHeader: string; upstream: UpstreamContext; channelConfig: unknown; files: readonly FileReference[];`
- `upstream: UpstreamContext; data: unknown; state: unknown}` → `upstream: UpstreamContext; channelConfig: unknown; data: unknown; state: unknown}`
- `upstream: UpstreamContext; tasks: readonly TaskQueryContext[]}` → `upstream: UpstreamContext; channelConfig: unknown; tasks: readonly TaskQueryContext[]}`
- `export declare function parseTaskResult(ctx: TaskQueryContext, body: unknown, response: HookHTTPResponse): NormalizedTaskResult;` → 
  ```ts
  export declare function parseTaskResult(ctx: TaskQueryContext, body: unknown, response: HookHTTPResponse): NormalizedTaskResult & {data?: unknown; state?: unknown};
  export declare function validateChannelConfig(config: Readonly<Record<string, unknown>>): void;
  ```

- [ ] **Step 3: v1.schema.json**

- `taskQueryContext.properties` 里把 `"state": true` 改成 `"state": true,` 并在下一行加 `"channelConfig": true`。
- `batchQueryContext.properties` 里在 `"tasks": {...}` 前一行加 `"channelConfig": true,`。

- [ ] **Step 4: 检查**

```bash
python -c "import json; json.load(open('docs/plugin-api/v1.schema.json', encoding='utf-8')); print('schema ok')"
grep -c "channelConfig" docs/plugin-api/v1.d.ts docs/plugin-api/v1.schema.json docs/plugin-api/v1.md
```

Expected: `schema ok`；d.ts 至少 3 处，schema 2 处，md 至少 1 处。

- [ ] **Step 5: 提交**

```bash
git add docs/plugin-api/v1.md docs/plugin-api/v1.d.ts docs/plugin-api/v1.schema.json
git commit -m "docs(plugin-api): channel configuration, ctx.channelConfig and parseTaskResult data"
```

---

### Task 9: 前端表单字段 `task_plugin_config`

**Files:**
- Modify: `web/src/features/channels/constants.ts`
- Modify: `web/src/features/channels/lib/channel-plugin-extensions.ts`
- Modify: `web/src/features/channels/types.ts`
- Modify: `web/src/features/channels/lib/channel-form.ts`
- Create: `web/src/features/channels/lib/__tests__/task-plugin-config.test.ts`

**Interfaces:**
- Produces: `CHANNEL_TYPE_ALI = 17`、`ALIBABA_TASK_PLUGIN_KEY = 'alibaba'`；`supportsTaskPluginConfig(channelType: number, taskPluginKey?: string): boolean`；表单字段 `task_plugin_config: string`（JSON 文本，空字符串表示不配置）。

- [ ] **Step 1: 写失败测试**

新建 `web/src/features/channels/lib/__tests__/task-plugin-config.test.ts`（文件头复制同目录其他测试文件的版权注释）：

```ts
import { describe, expect, test } from 'vitest'

import { CHANNEL_TYPE_ALI, CHANNEL_TYPE_TASK_PLUGIN } from '../../constants'
import { channelSchema } from '../../types'
import {
  CHANNEL_FORM_DEFAULT_VALUES,
  channelFormSchema,
  transformChannelToFormDefaults,
  transformFormDataToCreatePayload,
} from '../channel-form'
import { supportsTaskPluginConfig } from '../channel-plugin-extensions'

const relayConfig = { relay: { submit: { path: '/v1/aigc/video/generate' } } }

function relayForm(overrides: Record<string, unknown> = {}) {
  return channelFormSchema.parse({
    ...CHANNEL_FORM_DEFAULT_VALUES,
    name: 'Relay',
    type: CHANNEL_TYPE_TASK_PLUGIN,
    task_plugin_key: 'alibaba',
    base_url: 'https://relay.example',
    key: 'relay-key',
    models: 'wan3.0-video',
    task_plugin_config: JSON.stringify(relayConfig),
    ...overrides,
  })
}

function settingOf(form: ReturnType<typeof relayForm>) {
  return JSON.parse(transformFormDataToCreatePayload(form).channel.setting ?? '{}')
}

describe('task plugin configuration', () => {
  test('is offered for Alibaba task plugin channels and Ali channels only', () => {
    expect(supportsTaskPluginConfig(CHANNEL_TYPE_TASK_PLUGIN, 'alibaba')).toBe(true)
    expect(supportsTaskPluginConfig(CHANNEL_TYPE_ALI)).toBe(true)
    expect(supportsTaskPluginConfig(CHANNEL_TYPE_TASK_PLUGIN, 'kling')).toBe(false)
    expect(supportsTaskPluginConfig(1)).toBe(false)
  })

  test('rejects a configuration that is not a JSON object', () => {
    const base = { ...CHANNEL_FORM_DEFAULT_VALUES, name: 'Relay', type: CHANNEL_TYPE_ALI, key: 'k', models: 'wan3.0-video' }
    expect(channelFormSchema.safeParse({ ...base, task_plugin_config: '[1]' }).success).toBe(false)
    expect(channelFormSchema.safeParse({ ...base, task_plugin_config: '{"relay":' }).success).toBe(false)
    expect(channelFormSchema.safeParse({ ...base, task_plugin_config: '' }).success).toBe(true)
  })

  test('serializes the configuration into the setting of an eligible channel', () => {
    expect(settingOf(relayForm()).task_plugin_config).toEqual(relayConfig)
  })

  test('omits the configuration when it is empty or the channel cannot use it', () => {
    expect(settingOf(relayForm({ task_plugin_config: '' }))).not.toHaveProperty('task_plugin_config')
    expect(settingOf(relayForm({ task_plugin_key: 'kling' }))).not.toHaveProperty('task_plugin_config')
  })

  test('loads a stored configuration as formatted JSON', () => {
    const channel = channelSchema.parse({
      id: 12,
      name: 'Relay',
      type: CHANNEL_TYPE_TASK_PLUGIN,
      key: '',
      status: 1,
      created_time: 1,
      test_time: 0,
      response_time: 0,
      balance_updated_time: 0,
      models: 'wan3.0-video',
      group: 'default',
      base_url: 'https://relay.example',
      setting: JSON.stringify({ task_plugin_key: 'alibaba', task_plugin_config: relayConfig }),
    })
    expect(transformChannelToFormDefaults(channel).task_plugin_config).toBe(JSON.stringify(relayConfig, null, 2))
  })
})
```

- [ ] **Step 2: 运行，确认失败**

```bash
cd web && bun run test -- src/features/channels/lib/__tests__/task-plugin-config.test.ts
```

Expected: FAIL，`CHANNEL_TYPE_ALI` / `supportsTaskPluginConfig` 不存在。

- [ ] **Step 3: 常量与判断函数**

`web/src/features/channels/constants.ts`：在 `export const CHANNEL_TYPE_TASK_PLUGIN = 61` 后面加：

```ts

export const CHANNEL_TYPE_ALI = 17

export const ALIBABA_TASK_PLUGIN_KEY = 'alibaba'
```

`web/src/features/channels/lib/channel-plugin-extensions.ts`：import 里加上 `ALIBABA_TASK_PLUGIN_KEY`、`CHANNEL_TYPE_ALI`；在 `supportsChannelPluginExtensions` 函数后面加：

```ts
// The Alibaba Bailian plugin accepts a task_plugin_config (relay mode). It
// executes Task Plugin channels bound to it and Ali channels by channel type.
export function supportsTaskPluginConfig(
  channelType: number,
  taskPluginKey?: string
): boolean {
  if (channelType === CHANNEL_TYPE_ALI) return true
  return (
    channelType === CHANNEL_TYPE_TASK_PLUGIN &&
    taskPluginKey?.trim() === ALIBABA_TASK_PLUGIN_KEY
  )
}
```

`web/src/features/channels/types.ts` 的 `ChannelSettings` 接口里，在 `task_extend_plugin_keys?: string[]` 后面加 `task_plugin_config?: Record<string, unknown>`。

- [ ] **Step 4: 表单字段**

`web/src/features/channels/lib/channel-form.ts`：

1. 把 `import { readTaskExtendPluginKeys } from './channel-plugin-extensions'` 改成

```ts
import {
  readTaskExtendPluginKeys,
  supportsTaskPluginConfig,
} from './channel-plugin-extensions'
```

2. `channelFormSchema` 里，在 `task_extend_plugin_keys: z.array(z.string()).optional(),` 后面加：

```ts
    task_plugin_config: z
      .string()
      .optional()
      .refine(isOptionalJsonObject, ERROR_MESSAGES.INVALID_JSON),
```

3. `CHANNEL_FORM_DEFAULT_VALUES` 里，在 `task_extend_plugin_keys: [],` 后面加 `task_plugin_config: '',`。

4. `transformChannelToFormDefaults` 的 `extraSettings` 初始值里，在 `task_extend_plugin_keys: [] as string[],` 后面加 `task_plugin_config: '',`；解析分支里，在 `task_extend_plugin_keys: readTaskExtendPluginKeys(channel.type, parsed),` 后面加：

```ts
        task_plugin_config: isJsonObjectValue(parsed.task_plugin_config)
          ? JSON.stringify(parsed.task_plugin_config, null, 2)
          : '',
```

5. `buildSettingJSON` 的 `settingObj` 里，在 `task_extend_plugin_keys: ...` 这一项后面加：

```ts
    task_plugin_config:
      supportsTaskPluginConfig(formData.type, formData.task_plugin_key) &&
      formData.task_plugin_config?.trim()
        ? parseOptionalJson(formData.task_plugin_config)
        : undefined,
```

- [ ] **Step 5: 运行，确认通过**

```bash
cd web
bun run test -- src/features/channels/lib/__tests__/task-plugin-config.test.ts src/features/channels/lib/__tests__/new-api-channel.test.ts
bun run typecheck
npx oxlint -c .oxlintrc.json src/features/channels/constants.ts src/features/channels/lib/channel-plugin-extensions.ts src/features/channels/lib/channel-form.ts src/features/channels/types.ts src/features/channels/lib/__tests__/task-plugin-config.test.ts
```

Expected: 测试 PASS，typecheck 和 lint 没有错误。（`bun x oxlint ...` 也可以，按本机可用的方式运行。）

- [ ] **Step 6: 提交**

```bash
cd ..
git add web/src/features/channels/constants.ts web/src/features/channels/lib/channel-plugin-extensions.ts web/src/features/channels/types.ts web/src/features/channels/lib/channel-form.ts web/src/features/channels/lib/__tests__/task-plugin-config.test.ts
git commit -m "feat(web/channels): task_plugin_config form field"
```

---

### Task 10: 前端中转站配置区块、预览弹窗与文案

**Files:**
- Modify: `web/src/features/channels/api.ts`
- Create: `web/src/features/channels/components/drawers/sections/channel-relay-config-section.tsx`
- Modify: `web/src/features/channels/components/drawers/sections/index.ts`
- Create: `web/src/features/channels/components/dialogs/relay-config-preview-dialog.tsx`
- Modify: `web/src/features/channels/components/drawers/channel-mutate-drawer.tsx`
- Modify: `web/src/i18n/locales/{en,zh,zh-TW,fr,ru,ja,vi}.json`
- Create: `web/src/features/channels/components/__tests__/relay-config-section.test.tsx`

**Interfaces:**
- Consumes: Task 6 的预览接口；Task 9 的 `supportsTaskPluginConfig`、表单字段 `task_plugin_config`。
- Produces: `previewTaskPluginConfig(payload: TaskPluginConfigPreviewRequest): Promise<TaskPluginConfigPreview>`；`ChannelRelayConfigSection`；`RELAY_CONFIG_EXAMPLE`；`RelayConfigPreviewDialog`。

- [ ] **Step 1: 接口调用**

`web/src/features/channels/api.ts` 在 `getTaskPluginOptions` 函数后面加：

```ts
export type TaskPluginConfigPreviewRequest = {
  type: number
  task_plugin_key?: string
  base_url?: string
  config: Record<string, unknown>
  model: string
  entry: string
  request?: unknown
  submit_response?: unknown
  query_response?: unknown
}

export type TaskPluginConfigPreview = {
  submit?: Record<string, unknown>
  reservation?: Record<string, unknown> | null
  submit_result?: { task_id: string }
  query?: Record<string, unknown>
  query_result?: {
    status?: string
    url?: string
    reason?: string
    snapshot?: unknown
  }
  settlement?: Record<string, unknown> | null
  errors?: Partial<Record<'submit' | 'submit_result' | 'query_result', string>>
}

/**
 * Run a task_plugin_config through the executing plugin offline.
 */
export async function previewTaskPluginConfig(
  payload: TaskPluginConfigPreviewRequest
): Promise<TaskPluginConfigPreview> {
  const response = await api.post<{
    success: boolean
    message?: string
    data: TaskPluginConfigPreview
  }>(
    '/api/channel/task_plugin_config/preview',
    payload,
    channelActionConfig()
  )
  return requireServerSuccess(response.data).data
}
```

- [ ] **Step 2: 配置编辑区块**

新建 `web/src/features/channels/components/drawers/sections/channel-relay-config-section.tsx`（文件头复制同目录 `channel-sso-section.tsx` 的版权注释）：

```tsx
import { Eye, FileCode } from 'lucide-react'
import { useState } from 'react'
import type { ControllerRenderProps } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { JsonCodeEditor } from '@/components/json-code-editor'
import { Button } from '@/components/ui/button'
import {
  FormControl,
  FormDescription,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'

import type { ChannelFormValues } from '../../../lib/channel-form'
import { RelayConfigPreviewDialog } from '../../dialogs/relay-config-preview-dialog'

// The spec's TokenPony configuration, a starting point for any relay.
export const RELAY_CONFIG_EXAMPLE = {
  relay: {
    submit: {
      path: '/v1/aigc/video/generate',
      body: {
        model: '$.model',
        prompt: '$.input.prompt',
        media: '$.input.media',
        resolution: '$.parameters.resolution',
        ratio: '$.parameters.ratio',
        duration: '$.parameters.duration',
        generate_audio: '$.parameters.audio',
        watermark: '$.parameters.watermark',
        seed: '$.parameters.seed',
      },
      values: {
        'media[].type': { first_frame: 'first_image', last_frame: 'last_image' },
        seed: { '-1': null },
      },
      ok: { path: 'code', equals: 200 },
      taskId: 'data.id',
      error: 'message',
    },
    query: {
      path: '/v1/aigc/tasks/{task_id}',
      ok: { path: 'code', equals: 200 },
      status: 'data.task_status',
      statusMap: {
        PENDING: 'PENDING',
        RUNNING: 'RUNNING',
        COMPLETED: 'SUCCEEDED',
        FAILED: 'FAILED',
        CANCELED: 'CANCELED',
      },
      videoUrl: ['data.result.output', 'data.result.result'],
      reason: 'data.task_status_msg',
      usage: { seconds: 'data.usage.duration', resolution: 'data.usage.resolution' },
    },
  },
}

type ChannelRelayConfigSectionProps = {
  field: ControllerRenderProps<ChannelFormValues, 'task_plugin_config'>
  disabled?: boolean
  channelType: number
  taskPluginKey?: string
  baseUrl?: string
  model: string
}

export function ChannelRelayConfigSection(props: ChannelRelayConfigSectionProps) {
  const { t } = useTranslation()
  const [previewOpen, setPreviewOpen] = useState(false)
  const value = props.field.value || ''

  return (
    <FormItem className='space-y-3 border-t pt-4'>
      <div className='flex flex-col gap-2 sm:flex-row sm:items-start sm:justify-between'>
        <div className='space-y-1'>
          <FormLabel>{t('Relay configuration')}</FormLabel>
          <FormDescription>
            {t(
              'Describe how this channel talks to a third-party relay. Leave empty to connect to Alibaba Cloud Bailian directly.'
            )}
          </FormDescription>
        </div>
        <div className='flex flex-wrap gap-2'>
          <Button
            type='button'
            variant='outline'
            size='sm'
            disabled={props.disabled}
            onClick={() =>
              props.field.onChange(JSON.stringify(RELAY_CONFIG_EXAMPLE, null, 2))
            }
          >
            <FileCode className='mr-2 h-4 w-4' />
            {t('Insert example')}
          </Button>
          <Button
            type='button'
            variant='outline'
            size='sm'
            disabled={!value.trim()}
            onClick={() => setPreviewOpen(true)}
          >
            <Eye className='mr-2 h-4 w-4' />
            {t('Preview')}
          </Button>
        </div>
      </div>
      <FormControl>
        <JsonCodeEditor
          value={value}
          onChange={props.field.onChange}
          name={props.field.name}
          onBlur={props.field.onBlur}
          textareaRef={props.field.ref}
          disabled={props.disabled}
          ariaLabel={t('Relay configuration')}
          heightClassName='h-72 min-h-72 max-h-72'
        />
      </FormControl>
      <FormMessage />
      <RelayConfigPreviewDialog
        open={previewOpen}
        onOpenChange={setPreviewOpen}
        config={value}
        channelType={props.channelType}
        taskPluginKey={props.taskPluginKey}
        baseUrl={props.baseUrl}
        model={props.model}
      />
    </FormItem>
  )
}
```

`web/src/features/channels/components/drawers/sections/index.ts` 加一行：`export * from './channel-relay-config-section'`（按字母序放在 `channel-models-section` 之后）。

- [ ] **Step 3: 预览弹窗**

新建 `web/src/features/channels/components/dialogs/relay-config-preview-dialog.tsx`（文件头复制同目录 `channel-test-dialog.tsx` 的版权注释）：

```tsx
import { useMutation } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Dialog } from '@/components/dialog'
import { JsonCodeEditor } from '@/components/json-code-editor'
import { Button } from '@/components/ui/button'
import { Combobox } from '@/components/ui/combobox'
import { Label } from '@/components/ui/label'
import { getServerErrorMessage } from '@/lib/server-error-message'

import {
  previewTaskPluginConfig,
  type TaskPluginConfigPreviewRequest,
} from '../../api'

const OPENAI_VIDEO_ENTRY = 'openai_video'
const ALIBABA_NATIVE_ENTRY =
  '/ali/api/v1/services/aigc/video-generation/video-synthesis'

const SAMPLE_REQUESTS: Record<string, unknown> = {
  [OPENAI_VIDEO_ENTRY]: {
    model: 'wan3.0-video',
    prompt: 'A cat running on the grass',
    seconds: 5,
    resolution: '720P',
  },
  [ALIBABA_NATIVE_ENTRY]: {
    model: 'wan3.0-video',
    input: { prompt: 'A cat running on the grass' },
    parameters: { resolution: '720P', duration: 5 },
  },
}

const SAMPLE_SUBMIT_RESPONSE = {
  code: 200,
  message: 'success',
  data: { id: 'relay-task-1', status: 'PENDING' },
}

const SAMPLE_QUERY_RESPONSE = {
  code: 200,
  data: {
    task_id: 'relay-task-1',
    task_status: 'COMPLETED',
    result: { output: 'https://cdn.example/video.mp4' },
    usage: { duration: 5, resolution: '720P' },
  },
}

function formatJson(value: unknown): string {
  return JSON.stringify(value, null, 2)
}

type PreviewJsonFieldProps = {
  label: string
  value: string
  onChange?: (value: string) => void
}

function PreviewJsonField(props: PreviewJsonFieldProps) {
  return (
    <div className='grid gap-2'>
      <Label>{props.label}</Label>
      <JsonCodeEditor
        value={props.value}
        onChange={props.onChange ?? (() => undefined)}
        disabled={!props.onChange}
        ariaLabel={props.label}
        heightClassName='h-40 min-h-40 max-h-40'
      />
    </div>
  )
}

type RelayConfigPreviewDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  config: string
  channelType: number
  taskPluginKey?: string
  baseUrl?: string
  model: string
}

export function RelayConfigPreviewDialog(props: RelayConfigPreviewDialogProps) {
  const { t } = useTranslation()
  const [entry, setEntry] = useState(OPENAI_VIDEO_ENTRY)
  const [requestText, setRequestText] = useState(() =>
    formatJson(SAMPLE_REQUESTS[OPENAI_VIDEO_ENTRY])
  )
  const [submitResponseText, setSubmitResponseText] = useState(() =>
    formatJson(SAMPLE_SUBMIT_RESPONSE)
  )
  const [queryResponseText, setQueryResponseText] = useState(() =>
    formatJson(SAMPLE_QUERY_RESPONSE)
  )
  const [inputError, setInputError] = useState('')
  const preview = useMutation({
    mutationFn: previewTaskPluginConfig,
    meta: { errorToast: false },
  })

  const entryOptions = [
    { value: OPENAI_VIDEO_ENTRY, label: t('OpenAI video (/v1/videos)') },
    { value: ALIBABA_NATIVE_ENTRY, label: t('Alibaba native (/ali/api/v1/...)') },
  ]
  const errorLabels: Record<string, string> = {
    submit: t('Submit request'),
    submit_result: t('Task ID'),
    query_result: t('Query result'),
  }

  const handleEntryChange = (value: string | null) => {
    if (!value) return
    setEntry(value)
    setRequestText(formatJson(SAMPLE_REQUESTS[value]))
  }

  const parseField = (text: string, field: string): unknown => {
    if (!text.trim()) return undefined
    try {
      return JSON.parse(text)
    } catch {
      throw new Error(t('Invalid JSON: {{field}}', { field }))
    }
  }

  const handleRun = () => {
    setInputError('')
    let payload: TaskPluginConfigPreviewRequest
    try {
      payload = {
        type: props.channelType,
        task_plugin_key: props.taskPluginKey,
        base_url: props.baseUrl,
        config: parseField(props.config, t('Relay configuration')) as Record<
          string,
          unknown
        >,
        model: props.model,
        entry,
        request: parseField(requestText, t('Sample client request')),
        submit_response: parseField(submitResponseText, t('Relay submit response')),
        query_response: parseField(queryResponseText, t('Relay query response')),
      }
    } catch (error: unknown) {
      setInputError(error instanceof Error ? error.message : t('Preview failed'))
      return
    }
    preview.mutate(payload)
  }

  const result = preview.data
  const runError =
    inputError ||
    (preview.error ? getServerErrorMessage(preview.error, t('Preview failed')) : '')
  const stepErrors = Object.entries(result?.errors ?? {}).filter(
    ([, message]) => message
  )

  return (
    <Dialog
      open={props.open}
      onOpenChange={props.onOpenChange}
      title={t('Relay configuration preview')}
      description={t('No network request is sent and nothing is billed.')}
      contentClassName='sm:max-w-4xl'
      footer={
        <>
          <Button variant='outline' onClick={() => props.onOpenChange(false)}>
            {t('Close')}
          </Button>
          <Button onClick={handleRun} disabled={preview.isPending}>
            {t('Run preview')}
          </Button>
        </>
      }
    >
      <div className='max-h-[70vh] space-y-4 overflow-y-auto pr-1'>
        <div className='grid gap-2'>
          <Label htmlFor='relay-preview-entry'>{t('Client entry')}</Label>
          <Combobox
            id='relay-preview-entry'
            options={entryOptions}
            value={entry}
            onValueChange={handleEntryChange}
            className='w-full min-w-0'
          />
        </div>
        <PreviewJsonField label={t('Sample client request')} value={requestText} onChange={setRequestText} />
        <PreviewJsonField label={t('Relay submit response')} value={submitResponseText} onChange={setSubmitResponseText} />
        <PreviewJsonField label={t('Relay query response')} value={queryResponseText} onChange={setQueryResponseText} />
        {runError && (
          <p role='alert' className='text-destructive text-sm'>
            {runError}
          </p>
        )}
        {result && (
          <div className='space-y-4 border-t pt-4'>
            {stepErrors.map(([step, message]) => (
              <p key={step} role='alert' className='text-destructive text-sm'>
                {`${errorLabels[step] ?? step}: ${message}`}
              </p>
            ))}
            {result.submit && <PreviewJsonField label={t('Submit request')} value={formatJson(result.submit)} />}
            {result.reservation && <PreviewJsonField label={t('Reserved usage')} value={formatJson(result.reservation)} />}
            {result.submit_result && <PreviewJsonField label={t('Task ID')} value={formatJson(result.submit_result)} />}
            {result.query && <PreviewJsonField label={t('Query request')} value={formatJson(result.query)} />}
            {result.query_result && <PreviewJsonField label={t('Query result')} value={formatJson(result.query_result)} />}
            {result.settlement && <PreviewJsonField label={t('Settled usage')} value={formatJson(result.settlement)} />}
          </div>
        )}
      </div>
    </Dialog>
  )
}
```

- [ ] **Step 4: 接入渠道编辑抽屉**

`web/src/features/channels/components/drawers/channel-mutate-drawer.tsx`：

1. 从 `'./sections'` 的导入里加上 `ChannelRelayConfigSection`；在文件的 lib 导入处加上 `import { supportsTaskPluginConfig } from '../../lib/channel-plugin-extensions'`（如果该文件已从这个模块导入别的函数，就合并进去）。
2. 在 `const overrideFields = (` 这一行前面加：

```tsx
  const relayConfigModel =
    formValues.models
      ?.split(',')
      .map((model) => model.trim())
      .find(Boolean) ?? ''
  const relayConfigFields = supportsTaskPluginConfig(
    currentType,
    currentTaskPluginKey
  ) && (
    <FormField
      control={form.control}
      name='task_plugin_config'
      render={({ field }) => (
        <ChannelRelayConfigSection
          field={field}
          disabled={sensitiveLocked || isSubmitting}
          channelType={currentType}
          taskPluginKey={currentTaskPluginKey}
          baseUrl={currentBaseUrl}
          model={relayConfigModel}
        />
      )}
    />
  )
```

3. 在 `request={` 里，把

```tsx
          <>
            {overrideFields}
```

改成

```tsx
          <>
            {overrideFields}
            {relayConfigFields}
```

- [ ] **Step 5: 文案（7 个语言文件）**

`Preview`、`Task ID`、`Close`、`Submit request` 已经存在，直接复用。其余新键用下面的脚本一次性追加到 7 个文件末尾（已存在的键会跳过），再跑同步脚本统一顺序：

```bash
cd /d/Project/new-api/web
cat > /tmp/add-relay-i18n.py <<'EOF'
import json, os, sys
locales = sys.argv[1]
T = {
  "Invalid task plugin configuration: {{error}}": ["任务插件配置无效：{{error}}", "任務外掛設定無效：{{error}}", "Configuration du plugin de tâche invalide : {{error}}", "Недопустимая конфигурация плагина задач: {{error}}", "タスクプラグインの設定が無効です: {{error}}", "Cấu hình plugin tác vụ không hợp lệ: {{error}}"],
  "Relay configuration": ["中转站配置", "中轉站設定", "Configuration du relais", "Конфигурация ретранслятора", "中継設定", "Cấu hình trạm trung chuyển"],
  "Describe how this channel talks to a third-party relay. Leave empty to connect to Alibaba Cloud Bailian directly.": ["描述这个渠道如何与第三方中转站通信。留空表示直连阿里云百炼。", "描述這個渠道如何與第三方中轉站通訊。留空表示直連阿里雲百煉。", "Décrit comment ce canal communique avec un relais tiers. Laissez vide pour vous connecter directement à Alibaba Cloud Bailian.", "Описывает, как этот канал взаимодействует со сторонним ретранслятором. Оставьте пустым для прямого подключения к Alibaba Cloud Bailian.", "このチャネルがサードパーティの中継サービスとどう通信するかを記述します。空欄のままにすると Alibaba Cloud Bailian に直接接続します。", "Mô tả cách kênh này giao tiếp với trạm trung chuyển bên thứ ba. Để trống để kết nối trực tiếp tới Alibaba Cloud Bailian."],
  "Insert example": ["插入示例配置", "插入範例設定", "Insérer un exemple", "Вставить пример", "例を挿入", "Chèn ví dụ"],
  "Relay configuration preview": ["中转站配置预览", "中轉站設定預覽", "Aperçu de la configuration du relais", "Предпросмотр конфигурации ретранслятора", "中継設定のプレビュー", "Xem trước cấu hình trạm trung chuyển"],
  "No network request is sent and nothing is billed.": ["不会发送任何网络请求，也不会扣费。", "不會發送任何網路請求，也不會扣費。", "Aucune requête réseau n'est envoyée et rien n'est facturé.", "Сетевые запросы не отправляются, и ничего не списывается.", "ネットワークリクエストは送信されず、課金も発生しません。", "Không gửi yêu cầu mạng nào và không tính phí."],
  "Run preview": ["运行预览", "執行預覽", "Lancer l'aperçu", "Запустить предпросмотр", "プレビューを実行", "Chạy xem trước"],
  "Client entry": ["客户端入口", "用戶端入口", "Point d'entrée client", "Точка входа клиента", "クライアントのエントリ", "Điểm vào của máy khách"],
  "OpenAI video (/v1/videos)": ["OpenAI 视频格式（/v1/videos）", "OpenAI 影片格式（/v1/videos）", "Vidéo OpenAI (/v1/videos)", "Видео OpenAI (/v1/videos)", "OpenAI 動画 (/v1/videos)", "Video OpenAI (/v1/videos)"],
  "Alibaba native (/ali/api/v1/...)": ["阿里原生格式（/ali/api/v1/...）", "阿里原生格式（/ali/api/v1/...）", "Format natif Alibaba (/ali/api/v1/...)", "Нативный формат Alibaba (/ali/api/v1/...)", "Alibaba ネイティブ (/ali/api/v1/...)", "Định dạng gốc Alibaba (/ali/api/v1/...)"],
  "Sample client request": ["示例客户端请求", "範例用戶端請求", "Exemple de requête client", "Пример клиентского запроса", "クライアントリクエストの例", "Yêu cầu mẫu của máy khách"],
  "Relay submit response": ["中转站提交返回", "中轉站提交回應", "Réponse de soumission du relais", "Ответ ретранслятора на отправку", "中継サービスの送信レスポンス", "Phản hồi gửi của trạm trung chuyển"],
  "Relay query response": ["中转站查询返回", "中轉站查詢回應", "Réponse de suivi du relais", "Ответ ретранслятора на запрос статуса", "中継サービスの照会レスポンス", "Phản hồi truy vấn của trạm trung chuyển"],
  "Reserved usage": ["预扣用量", "預扣用量", "Utilisation réservée", "Зарезервированное использование", "予約使用量", "Mức dùng tạm giữ"],
  "Query request": ["查询请求", "查詢請求", "Requête de suivi", "Запрос статуса", "照会リクエスト", "Yêu cầu truy vấn"],
  "Query result": ["查询结果", "查詢結果", "Résultat du suivi", "Результат запроса статуса", "照会結果", "Kết quả truy vấn"],
  "Settled usage": ["结算用量", "結算用量", "Utilisation facturée", "Итоговое использование", "精算使用量", "Mức dùng quyết toán"],
  "Invalid JSON: {{field}}": ["JSON 格式错误：{{field}}", "JSON 格式錯誤：{{field}}", "JSON invalide : {{field}}", "Некорректный JSON: {{field}}", "JSON が不正です: {{field}}", "JSON không hợp lệ: {{field}}"],
  "Preview failed": ["预览失败", "預覽失敗", "Échec de l'aperçu", "Не удалось выполнить предпросмотр", "プレビューに失敗しました", "Xem trước thất bại"],
}
order = ["zh", "zh-TW", "fr", "ru", "ja", "vi"]
for lang in ["en"] + order:
    path = os.path.join(locales, lang + ".json")
    text = open(path, encoding="utf-8").read()
    existing = json.loads(text)["translation"]
    lines = []
    for key, values in T.items():
        if key in existing:
            continue
        value = key if lang == "en" else values[order.index(lang)]
        lines.append("    " + json.dumps(key, ensure_ascii=False) + ": " + json.dumps(value, ensure_ascii=False))
    if not lines:
        continue
    # Insert textually before the closing "  }\n}" so the rest of the file,
    # including escaped keys, stays byte-for-byte unchanged.
    closing = text.rstrip().rfind("\n  }")
    assert closing > 0, path
    text = text[:closing] + ",\n" + ",\n".join(lines) + text[closing:]
    with open(path, "w", encoding="utf-8", newline="\n") as f:
        f.write(text)
    json.loads(text)
EOF
python /tmp/add-relay-i18n.py "$(cygpath -w src/i18n/locales)"
bun run i18n:sync
git diff --stat src/i18n/locales
```

Expected: 7 个文件各新增 19 行，其余内容不变。`bun run i18n:sync` 只会按 en.json 统一键的顺序；如果它报告缺失的键，回到脚本补齐对应语言。

- [ ] **Step 6: 写界面测试**

新建 `web/src/features/channels/components/__tests__/relay-config-section.test.tsx`（文件头复制同目录其他测试的版权注释）：

```tsx
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useForm } from 'react-hook-form'
import { afterEach, describe, expect, test, vi } from 'vitest'

import { Form, FormField } from '@/components/ui/form'

import { previewTaskPluginConfig } from '../../api'
import type { ChannelFormValues } from '../../lib/channel-form'
import {
  ChannelRelayConfigSection,
  RELAY_CONFIG_EXAMPLE,
} from '../drawers/sections/channel-relay-config-section'

vi.mock('../../api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../api')>()),
  previewTaskPluginConfig: vi.fn(),
}))

const exampleText = JSON.stringify(RELAY_CONFIG_EXAMPLE, null, 2)

function Harness(props: { initial?: string }) {
  const form = useForm<ChannelFormValues>({
    defaultValues: { task_plugin_config: props.initial ?? '' } as ChannelFormValues,
  })
  return (
    <QueryClientProvider client={new QueryClient()}>
      <Form {...form}>
        <FormField
          control={form.control}
          name='task_plugin_config'
          render={({ field }) => (
            <ChannelRelayConfigSection
              field={field}
              channelType={61}
              taskPluginKey='alibaba'
              baseUrl='https://relay.example'
              model='wan3.0-video'
            />
          )}
        />
      </Form>
    </QueryClientProvider>
  )
}

afterEach(() => {
  vi.mocked(previewTaskPluginConfig).mockReset()
})

describe('ChannelRelayConfigSection', () => {
  test('insert example fills the editor with the relay example', async () => {
    render(<Harness />)
    await userEvent.click(screen.getByRole('button', { name: 'Insert example' }))
    expect(screen.getByRole('textbox', { name: 'Relay configuration' })).toHaveValue(exampleText)
  })

  test('preview stays disabled while the configuration is empty', () => {
    render(<Harness />)
    expect(screen.getByRole('button', { name: 'Preview' })).toBeDisabled()
  })

  test('running the preview sends the parsed configuration and shows the translated request', async () => {
    vi.mocked(previewTaskPluginConfig).mockResolvedValue({
      submit: { url: 'https://relay.example/v1/aigc/video/generate', method: 'POST' },
      submit_result: { task_id: 'relay-task-1' },
      errors: {},
    })
    render(<Harness initial={exampleText} />)
    await userEvent.click(screen.getByRole('button', { name: 'Preview' }))
    await userEvent.click(await screen.findByRole('button', { name: 'Run preview' }))

    await waitFor(() => expect(previewTaskPluginConfig).toHaveBeenCalled())
    expect(vi.mocked(previewTaskPluginConfig).mock.calls[0][0]).toMatchObject({
      type: 61,
      task_plugin_key: 'alibaba',
      base_url: 'https://relay.example',
      model: 'wan3.0-video',
      entry: 'openai_video',
      config: RELAY_CONFIG_EXAMPLE,
    })
    expect(await screen.findByDisplayValue(/relay\.example\/v1\/aigc\/video\/generate/)).toBeInTheDocument()
  })

  test('step errors returned by the preview are shown', async () => {
    vi.mocked(previewTaskPluginConfig).mockResolvedValue({
      errors: { submit_result: 'relay returned a failure' },
    })
    render(<Harness initial={exampleText} />)
    await userEvent.click(screen.getByRole('button', { name: 'Preview' }))
    await userEvent.click(await screen.findByRole('button', { name: 'Run preview' }))
    expect(await screen.findByText('Task ID: relay returned a failure')).toBeInTheDocument()
  })

  test('invalid sample JSON is reported without calling the server', async () => {
    render(<Harness initial={exampleText} />)
    await userEvent.click(screen.getByRole('button', { name: 'Preview' }))
    fireEvent.change(await screen.findByRole('textbox', { name: 'Sample client request' }), {
      target: { value: '{' },
    })
    await userEvent.click(screen.getByRole('button', { name: 'Run preview' }))
    expect(await screen.findByText('Invalid JSON: Sample client request')).toBeInTheDocument()
    expect(previewTaskPluginConfig).not.toHaveBeenCalled()
  })
})
```

- [ ] **Step 7: 运行，确认通过**

```bash
cd /d/Project/new-api/web
bun run test -- src/features/channels/components/__tests__/relay-config-section.test.tsx src/features/channels/lib/__tests__/task-plugin-config.test.ts
bun run typecheck
npx oxlint -c .oxlintrc.json src/features/channels/api.ts src/features/channels/components/drawers/sections/channel-relay-config-section.tsx src/features/channels/components/drawers/sections/index.ts src/features/channels/components/dialogs/relay-config-preview-dialog.tsx src/features/channels/components/drawers/channel-mutate-drawer.tsx src/features/channels/components/__tests__/relay-config-section.test.tsx
```

Expected: 测试 PASS；typecheck、lint 没有错误。如果测试里按钮名称取不到，先确认测试环境的 i18n 返回英文键原文（同目录其他测试用的就是英文名称）。

- [ ] **Step 8: 提交**

```bash
cd /d/Project/new-api
git add web/src/features/channels web/src/i18n/locales
git commit -m "feat(web/channels): relay configuration editor and offline preview"
```

---

### Task 11: 全量验证

- [ ] **Step 1: 后端**

```bash
cd /d/Project/new-api
gofmt -l relaykit relay controller service router plugins common   # 应无输出
go vet ./relay/... ./controller/... ./service/... ./router/... ./plugins/...
go test ./relay/... ./controller/... ./service/... ./router/... ./plugins/... ./pkg/jsplugin/... ./common/... -count=1
cd relaykit && GOWORK=off go build ./... && cd ..
go run . plugin lint plugins/tasks/alibaba/plugin.js
```

Expected: 全部通过。本功能不改数据库表结构（配置存在渠道已有的 setting 文本列里），不需要三库验证。

- [ ] **Step 2: 前端**

```bash
cd web
bun run test -- src/features/channels
bun run typecheck
bun run lint
bun run build
```

Expected: 全部通过；`bun run build` 成功，新的 `web/dist` 包含 `task_plugin_config/preview`（`grep -rl "task_plugin_config/preview" dist | head -1` 有输出）。

- [ ] **Step 3: 镜像构建（可选，需 Docker Desktop）**

按之前的做法，用 `git archive HEAD` 导出干净代码构建镜像，确认 CI 能编译；国内网络需要临时把基础镜像换成镜像站、Go 代理换成 goproxy.cn，只在临时目录里改，不改仓库里的 Dockerfile。

- [ ] **Step 4: 真实请求（需用户同意，会产生少量费用）**

在本地新建 TokenPony 渠道（Task Plugin + 阿里百炼，Base URL `https://api.tokenpony.cn`，模型 `wan3.0-video`，插入示例配置并预览），用 `POST /v1/videos` 发一个 480P、2 秒的请求，确认任务完成、视频能通过 `/v1/videos/{id}/content` 下载、消费日志按实际用量结算。

