# 项目 OpenAI Video 端点逻辑分析

本文档分析 new-api 项目中 OpenAI Video 兼容端点的完整请求和轮询逻辑。

---

## 1. 路由定义

文件：`router/video-router.go`

```
POST /v1/videos              → controller.RelayTask       (提交视频生成)
GET  /v1/videos/:task_id     → controller.RelayTaskFetch   (轮询任务状态)
GET  /v1/videos/:task_id/content → controller.VideoProxy   (下载视频内容)
```

认证中间件：
- 提交和轮询：`TokenAuth()` + `Distribute()`（API Key 鉴权 + 负载分发）
- 下载代理：`TokenOrUserAuth()`（支持 API Key 或 Session 登录）

---

## 2. 提交流程（POST /v1/videos）

### 2.1 入口：`controller/relay.go:479` — `RelayTask()`

```
RelayTask()
  ├─ GenRelayInfo()           — 构建 RelayInfo（渠道、用户、模型等上下文）
  ├─ ResolveOriginTask()      — 处理 remix 场景（锁定原始渠道、提取参数）
  ├─ 重试循环 (最多 RetryTimes 次)
  │   ├─ getChannel()         — 选择可用渠道（或使用锁定渠道）
  │   └─ RelayTaskSubmit()    — 核心提交逻辑
  ├─ 成功 → SettleBilling + LogTaskConsumption + 插入 Task 记录
  └─ 失败 → Refund 预扣费 + 返回错误
```

### 2.2 核心提交：`relay/relay_task.go:144` — `RelayTaskSubmit()`

```
RelayTaskSubmit()
  ├─ GetTaskPlatform()                — 根据渠道类型确定平台（openai-video）
  ├─ GetTaskAdaptor(platform)         — 获取 AIGC 适配器
  ├─ adaptor.Init(info)               — 初始化认证模式（Bearer / HMAC）
  ├─ adaptor.ValidateRequestAndSetAction() — 校验请求体
  ├─ ModelMappedHelper()              — 应用渠道模型映射
  ├─ ModelPriceHelperPerCall()        — 计算基础模型价格
  ├─ adaptor.EstimateBilling()        — 提取 seconds/size 计费倍率
  ├─ PreConsumeBilling()              — 预扣费（仅首次）
  ├─ adaptor.BuildRequestBody()       — 构建上游请求体（替换 model 为上游名称）
  ├─ adaptor.DoRequest()              — 发送 POST /v1/videos 到上游
  └─ adaptor.DoResponse()             — 解析响应，替换 ID 为公开 task_id，返回给客户端
```

### 2.3 AIGC 适配器：`relay/channel/task/aigc/adaptor.go`

认证模式自动检测：
- API Key 含 `|` 且 ≥3 段 → HMAC-SHA256 签名模式
- 否则 → Bearer Token 模式

请求构建：
- URL: `{baseURL}/v1/videos`（提交）或 `{baseURL}/v1/videos/{id}/remix`（remix）
- Body: 原始请求体，`model` 字段替换为上游模型名称

响应处理：
- 解析上游返回的 `responseTask`（id, object, model, status, progress 等）
- 将上游 `id` 替换为项目内部生成的公开 `task_id`（格式：`task_` + 32位随机字符）
- 上游真实 ID 存入 `PrivateData.UpstreamTaskID`

### 2.4 请求 DTO

文件：`relay/common/relay_info.go:674`

```go
type TaskSubmitReq struct {
    Prompt          string                 `json:"prompt"`
    Model           string                 `json:"model,omitempty"`
    Mode            string                 `json:"mode,omitempty"`
    Image           string                 `json:"image,omitempty"`
    Images          []string               `json:"images,omitempty"`
    Size            string                 `json:"size,omitempty"`
    Duration        int                    `json:"duration,omitempty"`
    Seconds         string                 `json:"seconds,omitempty"`
    InputReference  string                 `json:"input_reference,omitempty"`
    ImageReference  *ImageURLRef           `json:"image_reference,omitempty"`
    ImageReferences []ImageURLRef          `json:"image_references,omitempty"`
    Metadata        map[string]interface{} `json:"metadata,omitempty"`
}
```

### 2.5 响应 DTO

文件：`dto/openai_video.go`

```go
type OpenAIVideo struct {
    ID                 string            `json:"id"`
    Object             string            `json:"object"`           // 固定 "video"
    Model              string            `json:"model"`
    Status             string            `json:"status"`           // queued/in_progress/completed/failed
    Progress           int               `json:"progress"`         // 0-100
    CreatedAt          int64             `json:"created_at"`
    CompletedAt        int64             `json:"completed_at,omitempty"`
    ExpiresAt          int64             `json:"expires_at,omitempty"`
    Seconds            string            `json:"seconds,omitempty"`
    Size               string            `json:"size,omitempty"`
    Quality            string            `json:"quality,omitempty"`
    Prompt             string            `json:"prompt,omitempty"`
    Error              *OpenAIVideoError `json:"error,omitempty"`
    Metadata           map[string]any    `json:"metadata,omitempty"`
}
```

### 2.6 提交成功后的数据持久化

`controller/relay.go:572-591`：

```go
task := model.InitTask(result.Platform, relayInfo)
task.PrivateData.UpstreamTaskID = result.UpstreamTaskID    // 上游真实 ID
task.PrivateData.BillingSource = relayInfo.BillingSource
task.PrivateData.BillingContext = &TaskBillingContext{...}  // 计费快照
task.Quota = result.Quota
task.Data = result.TaskData                                 // 上游原始响应
task.Insert()
```

---

## 3. 轮询流程（GET /v1/videos/:task_id）

### 3.1 客户端主动轮询

入口：`controller/relay.go:464` — `RelayTaskFetch()`

```
RelayTaskFetch()
  └─ relay.RelayTaskFetch(c, relayMode)
       └─ videoFetchByIDRespBodyBuilder(c)
            ├─ model.GetByTaskId(userId, taskId)     — 查询本地 Task 记录
            ├─ tryRealtimeFetch()                     — Gemini/Vertex 实时拉取（其他渠道跳过）
            ├─ GetTaskAdaptor(platform)               — 获取适配器
            └─ adaptor.ConvertToOpenAIVideo(task)     — 转换为 OpenAI Video 格式
```

`relay/relay_task.go:362-416` — `videoFetchByIDRespBodyBuilder()`：

关键逻辑：
1. 通过 `task_id` + `user_id` 查询本地 Task 表
2. 判断是否为 OpenAI Video API 格式（URL 以 `/v1/videos/` 开头）
3. 对 Gemini/Vertex 渠道：调用 `tryRealtimeFetch()` 实时从上游拉取最新状态
4. 对其他渠道（包括 openai-video）：直接从本地 Task 记录构建响应
5. 调用 `adaptor.ConvertToOpenAIVideo(task)` 将 Task.Data（上游原始响应）转为 OpenAI Video 格式

AIGC 适配器的 `ConvertToOpenAIVideo()`（`aigc/adaptor.go:353`）：
- 上游已返回 OpenAI 兼容格式，只需将 `id` 替换为公开 `task_id`

### 3.2 服务端后台轮询

入口：`service/task_polling.go:91` — `TaskPollingLoop()`

```
TaskPollingLoop()  (每 15 秒执行一次)
  ├─ sweepTimedOutTasks()              — 清理超时任务（标记失败 + 退款）
  ├─ GetAllUnFinishSyncTasks(limit)    — 查询所有未完成任务
  ├─ 按 platform 分组
  └─ DispatchPlatformUpdate()
       └─ UpdateVideoTasks()           — 按渠道逐个更新
            └─ updateVideoSingleTask() — 单任务更新
```

`service/task_polling.go:344` — `updateVideoSingleTask()`：

```
updateVideoSingleTask()
  ├─ adaptor.FetchTask(baseURL, key, {task_id, action}, proxy)
  │   → GET {baseURL}/v1/videos/{upstream_task_id}
  ├─ 解析响应（先尝试 New API 格式，再尝试 adaptor.ParseTaskResult）
  ├─ 状态映射：
  │   ├─ SUBMITTED  → progress: 10%
  │   ├─ QUEUED     → progress: 20%
  │   ├─ IN_PROGRESS → progress: 30%
  │   ├─ SUCCESS    → progress: 100%, 设置 ResultURL
  │   └─ FAILURE    → progress: 100%, 触发退款
  ├─ CAS 更新（UpdateWithStatus 防止并发覆盖）
  ├─ 成功 → settleTaskBillingOnComplete()（差额结算）
  └─ 失败 → RefundTaskQuota()（全额退款）
```

AIGC 适配器的 `FetchTask()`（`aigc/adaptor.go:261`）：
- 请求：`GET {baseURL}/v1/videos/{upstream_task_id}`
- 认证：根据 Key 格式自动选择 HMAC 或 Bearer

AIGC 适配器的 `ParseTaskResult()`（`aigc/adaptor.go:315`）：
- 状态映射：`queued/pending` → QUEUED, `processing/in_progress` → IN_PROGRESS, `completed` → SUCCESS, `failed/cancelled` → FAILURE
- 成功时提取 `video_url` 字段

### 3.3 超时处理

`service/task_polling.go:41` — `sweepTimedOutTasks()`：
- 超时阈值：`constant.TaskTimeoutMinutes`
- 超时任务标记为 FAILURE + 全额退款
- 使用 CAS（UpdateWithStatus）防止覆盖已被正常轮询推进的任务

---

## 4. 下载流程（GET /v1/videos/:task_id/content）

入口：`controller/video_proxy.go:33` — `VideoProxy()`

```
VideoProxy()
  ├─ model.GetByTaskId(userId, taskId)     — 查询任务
  ├─ 检查 task.Status == SUCCESS
  ├─ model.CacheGetChannel(task.ChannelId) — 获取渠道信息
  ├─ 根据渠道类型构建视频 URL：
  │   ├─ ChannelTypeOpenAI/Sora → {baseURL}/v1/videos/{upstream_id}/content + Bearer auth
  │   ├─ ChannelTypeGemini      → getGeminiVideoURL() + x-goog-api-key
  │   ├─ ChannelTypeVertexAi    → getVertexVideoURL()
  │   └─ 其他                   → task.GetResultURL()（PrivateData.ResultURL）
  ├─ URL 安全校验（SSRF 防护）
  ├─ data: URI → 直接 base64 解码返回
  └─ HTTP URL → 代理请求，流式转发 video/mp4
```

---

## 5. 计费模型

### 5.1 预扣费

提交时根据 `seconds × size倍率 × 模型单价` 预扣费用。

`EstimateBilling()`（`aigc/adaptor.go:119`）：
- `seconds`：从请求体 `seconds` / `duration` / `metadata.seconds` 提取，默认 5
- `size`：默认 1.0 倍率，`1792x1024` 或 `1024x1792` 或 `1080P` 为 1.666667 倍

### 5.2 终态结算

- 成功：`settleTaskBillingOnComplete()` — 按实际参数差额结算
- 失败：`RefundTaskQuota()` — 全额退款
- 超时：`sweepTimedOutTasks()` — 全额退款

---

## 6. 数据模型

### 6.1 Task 表（`model/task.go`）

| 字段 | 说明 |
|---|---|
| `TaskID` | 对外公开 ID（`task_` + 32位随机字符） |
| `Platform` | 平台标识（如 `openai-video`） |
| `Status` | `NOT_START / SUBMITTED / QUEUED / IN_PROGRESS / SUCCESS / FAILURE` |
| `Progress` | 百分比字符串（`0%` ~ `100%`） |
| `Data` | 上游原始响应 JSON（脱敏后） |
| `PrivateData.UpstreamTaskID` | 上游真实 task ID |
| `PrivateData.ResultURL` | 视频下载地址 |
| `PrivateData.BillingContext` | 计费参数快照 |

### 6.2 状态映射

| 内部状态 | OpenAI Video 状态 | 进度 |
|---|---|---|
| `NOT_START` | `unknown` | 0% |
| `SUBMITTED` | `queued` | 10% |
| `QUEUED` | `queued` | 20% |
| `IN_PROGRESS` | `in_progress` | 30% |
| `SUCCESS` | `completed` | 100% |
| `FAILURE` | `failed` | 100% |

---

## 7. 关键文件索引

| 文件 | 职责 |
|---|---|
| `router/video-router.go` | 视频路由定义 |
| `controller/relay.go:464-597` | RelayTask / RelayTaskFetch 控制器 |
| `controller/video_proxy.go` | 视频下载代理 |
| `relay/relay_task.go` | 任务提交 + 轮询响应构建 |
| `relay/channel/task/aigc/adaptor.go` | AIGC（openai-video）适配器 |
| `relay/channel/task/aigc/constants.go` | 支持的模型列表 |
| `relay/common/relay_info.go:674-780` | TaskSubmitReq / TaskInfo DTO |
| `dto/openai_video.go` | OpenAIVideo 响应 DTO |
| `dto/video.go` | 通用 VideoRequest/VideoTaskResponse DTO |
| `model/task.go` | Task 数据模型 + DB 操作 |
| `service/task_polling.go` | 后台轮询循环 + 状态更新 |
| `relay/channel/task/taskcommon/helpers.go` | BuildProxyURL 等工具函数 |
