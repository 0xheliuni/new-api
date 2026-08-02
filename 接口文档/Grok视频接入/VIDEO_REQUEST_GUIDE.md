# Grok 视频 API 请求指南

## 通用信息

- 认证：`Authorization: Bearer <API_KEY>` 或 `X-API-Key: <API_KEY>`
- `seconds` 取值：`6, 10, 12, 16, 20, 30`
- `size` 取值：`720x1280, 1280x720, 1024x1024, 1024x1792, 1792x1024`
- 视频在服务端保留 1 小时，过期后无法下载

---

## 方式一：异步轮询（推荐）

最通用、最稳定的方式，分三步完成。

### 第一步：提交生成任务

```http
POST /v1/videos
Content-Type: application/json
Authorization: Bearer <API_KEY>

{
  "prompt": "a red fox in snow",
  "model": "grok-imagine-video",
  "seconds": 6,
  "size": "720x1280"
}
```

返回示例：

```json
{
  "id": "video_dd72654a6b094df6adb00570621af473",
  "object": "video",
  "created_at": 1745380717,
  "status": "queued",
  "model": "grok-imagine-video",
  "progress": 0,
  "prompt": "a red fox in snow",
  "seconds": "6",
  "size": "720x1280",
  "quality": "standard"
}
```

### 第二步：轮询状态（建议每 3-5 秒一次）

```http
GET /v1/videos/{video_id}
Authorization: Bearer <API_KEY>
```

- `status: "queued"` / `"in_progress"` → 继续轮询，关注 `progress` 字段（0-100）
- `status: "completed"` → 拿到 `url` 字段，进入第三步
- `status: "failed"` → 生成失败，查看 `error.message`

返回示例（completed）：

```json
{
  "id": "video_dd72654a6b094df6adb00570621af473",
  "status": "completed",
  "progress": 100,
  "completed_at": 1745380999,
  "url": "https://<host>/v1/files/video?id=video_dd72654a6b094df6adb00570621af473"
}
```

### 第三步：下载视频

两个端点可选：

**A. 公开端点（无需鉴权）**

```http
GET /v1/files/video?id={video_id}
```

直接返回 `video/mp4` 二进制流。

**B. 标准端点（需鉴权）**

```http
GET /v1/videos/{video_id}/content
Authorization: Bearer <API_KEY>
```

返回 `video/mp4`，`filename=<video_id>.mp4`。

### cURL 完整示例

```bash
# 1. 提交任务
curl -X POST https://<host>/v1/videos \
  -H "Authorization: Bearer <API_KEY>" \
  -H "Content-Type: application/json" \
  -d '{"prompt":"a red fox in snow","seconds":6,"size":"720x1280"}'

# 2. 轮询（替换 video_id）
curl https://<host>/v1/videos/video_xxx \
  -H "Authorization: Bearer <API_KEY>"

# 3a. 下载（公开端点）
curl -o video.mp4 "https://<host>/v1/files/video?id=video_xxx"

# 3b. 下载（鉴权端点）
curl -o video.mp4 https://<host>/v1/videos/video_xxx/content \
  -H "Authorization: Bearer <API_KEY>"
```

---

## 方式二：同步阻塞（Prefer: wait）

一个请求完成全部流程，不需要轮询。请求会阻塞直到视频生成完毕或超时。

### 请求

```http
POST /v1/videos
Content-Type: application/json
Authorization: Bearer <API_KEY>
Prefer: wait

{
  "prompt": "a red fox in snow",
  "model": "grok-imagine-video",
  "seconds": 6,
  "size": "720x1280"
}
```

也可以通过以下方式触发同步模式（任选其一）：
- Header: `Prefer: wait`
- Query: `?wait=true` 或 `?wait=1`
- Body: `"wait": true`

### 成功返回

```json
{
  "id": "video_xxx",
  "object": "video",
  "status": "completed",
  "progress": 100,
  "completed_at": 1745380999,
  "url": "https://<host>/v1/files/video?id=video_xxx"
}
```

拿到 `url` 后直接下载即可。

### 失败返回

```json
{"error": {"message": "...", "type": "server_error"}}
```

### 超时返回（费用自动退还）

```json
{"error": {"message": "Video generation timed out, cost refunded", "type": "timeout"}}
```

### cURL 完整示例

```bash
# 同步生成
curl -X POST https://<host>/v1/videos \
  -H "Authorization: Bearer <API_KEY>" \
  -H "Content-Type: application/json" \
  -H "Prefer: wait" \
  -d '{"prompt":"a red fox in snow","seconds":6,"size":"720x1280"}'

# 从返回的 url 字段下载
curl -o video.mp4 "https://<host>/v1/files/video?id=video_xxx"
```

> 注意：同步模式的错误格式只有 `message` + `type`，没有 `code` / `param`，和标准错误格式不同，客户端需兼容。

---

## 方式三：Chat Completions 风格

适合已集成 OpenAI SDK 的场景，用聊天接口触发视频生成，视频 URL 在 `content` 字段返回。

### 非流式请求

```http
POST /v1/chat/completions
Content-Type: application/json
Authorization: Bearer <API_KEY>

{
  "model": "grok-imagine-video",
  "messages": [
    {
      "role": "user",
      "content": "a red fox in snow"
    }
  ],
  "stream": false,
  "video_config": {
    "seconds": 6,
    "size": "720x1280",
    "resolution_name": "720p",
    "preset": "custom"
  }
}
```

返回示例：

```json
{
  "id": "chatcmpl-1745380717xxxx",
  "object": "chat.completion",
  "model": "grok-imagine-video",
  "choices": [{
    "index": 0,
    "message": {
      "role": "assistant",
      "content": "https://<host>/v1/files/video?id=abcd1234..."
    },
    "finish_reason": "stop"
  }]
}
```

`content` 就是视频下载地址，直接 GET 即可。

### 流式请求

```http
POST /v1/chat/completions
Content-Type: application/json
Authorization: Bearer <API_KEY>

{
  "model": "grok-imagine-video",
  "messages": [{"role": "user", "content": "a red fox in snow"}],
  "stream": true,
  "video_config": {"seconds": 6}
}
```

SSE 事件序列：
1. 多帧 `reasoning_content`（进度百分比）
2. 1 帧 `content`（最终视频 URL）
3. 1 帧收尾（`finish_reason: "stop"`）
4. `data: [DONE]`

### 带参考图的请求

```json
{
  "model": "grok-imagine-video",
  "messages": [
    {
      "role": "user",
      "content": [
        {"type": "text", "text": "make this image come alive"},
        {"type": "image_url", "image_url": {"url": "https://example.com/photo.jpg"}}
      ]
    }
  ],
  "stream": false,
  "video_config": {"seconds": 6}
}
```

### cURL 完整示例

```bash
# 非流式
curl -X POST https://<host>/v1/chat/completions \
  -H "Authorization: Bearer <API_KEY>" \
  -H "Content-Type: application/json" \
  -d '{
    "model":"grok-imagine-video",
    "messages":[{"role":"user","content":"a red fox in snow"}],
    "stream":false,
    "video_config":{"seconds":6,"size":"720x1280"}
  }'

# 从 choices[0].message.content 拿到 URL 后下载
curl -o video.mp4 "<返回的URL>"
```

---

## 三种方式对比

| | 异步轮询 | 同步阻塞 | Chat 风格 |
|---|---|---|---|
| 复杂度 | 需要轮询逻辑 | 最简单 | 中等 |
| 连接时间 | 短（每次轮询秒级） | 长（可能数分钟） | 长 |
| 适用场景 | 后端服务、批量任务 | 简单脚本、快速测试 | 已有 OpenAI SDK 集成 |
| 超时风险 | 低（可控轮询） | 高（中间件可能断连） | 中等 |
| 进度感知 | 有（progress 字段） | 无 | 流式有（reasoning_content） |
