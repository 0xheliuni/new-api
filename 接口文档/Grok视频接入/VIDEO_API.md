# 视频 API 完整文档

grok2api 所有视频相关对外接口。涵盖请求体/表单字段、header、返回值、全部错误形态。

**URL host** 取自 `app.app_url`(当前 `https://grok.xitongsp.top`),未配置则回退 `request.base_url`。

**代码入口:**
- 路由: `app/products/openai/router.py`
- 核心逻辑: `app/products/openai/video.py`
- 错误类型: `app/platform/errors.py`
- Chat/Responses 格式化: `app/products/openai/_format.py`
- 请求 Pydantic schema: `app/products/openai/schemas.py`

---

## 目录

- [0. 通用约定](#0-通用约定)
- [1. POST /v1/videos](#1-post-v1videos)
- [2. POST /v1/videos (Prefer: wait 同步模式)](#2-post-v1videos-prefer-wait-同步模式)
- [3. POST /v1/videos (上游转发模式)](#3-post-v1videos-上游转发模式)
- [4. GET /v1/videos/{video_id}](#4-get-v1videosvideo_id)
- [5. GET /v1/videos/{video_id}/content](#5-get-v1videosvideo_idcontent)
- [6. GET /v1/files/video](#6-get-v1filesvideo)
- [7. POST /v1/chat/completions (chat 风格视频)](#7-post-v1chatcompletions-chat-风格视频)
- [8. 参数取值表](#8-参数取值表)
- [9. 错误对象形态汇总](#9-错误对象形态汇总)
- [10. 错误文案本地化映射表](#10-错误文案本地化映射表)
- [11. 典型时序](#11-典型时序)
- [附录: 配置项](#附录-配置项)

---

## 0. 通用约定

### 0.1 认证

所有 `/v1/videos*` 端点(除 `/v1/files/video`)都依赖 `verify_api_key`(`app/platform/auth/middleware.py`)。

**Header:** `Authorization: Bearer <API_KEY>` 或 `X-API-Key: <API_KEY>`

**鉴权失败 → HTTP 401:**
```json
{"error": {"message": "Invalid or missing API key", "type": "authentication_error", "code": "invalid_api_key"}}
```

### 0.2 计费

视频统一走"**预扣费 + 失败退款**"模式(`router.py:1031`):

| 时机 | 动作 |
|---|---|
| 创建 job 前 | 按 `seconds` 计算成本,立即从 `api_keys.remaining` 和 `users.quota_used` 扣除 |
| job `status=completed` | 不退,把 `video_url` 写回 usage_log |
| job `status=failed` | 全额退款,删除 usage_log 行 |
| 超过 `max_wait` 未完成 | 全额退款(`max_wait = max(360, seconds*30)` 秒) |
| 上游转发失败 | 全额退款 |

成本由 `billing.costs.video_by_seconds` 表决定(没配置则走 `video_per_second * seconds`,再不行走 `video` 单价默认 50)。

### 0.3 并发/限流

- 本地并发受 `video.max_concurrency` 控制
- 超过 `routing.local_queue_threshold`(默认 150)会自动转发到 `routing_upstreams.json` 里的其他 grok2api 实例
- 单请求最多 **7 张参考图**(`_MAX_VIDEO_REFERENCE_IMAGES`)
- Job 在内存里保留 `_VIDEO_JOB_TTL_S = 3600` 秒(1 小时),TTL 过期后 `GET /v1/videos/{id}` 会返回 404

---

## 1. POST `/v1/videos`

OpenAI 兼容的视频生成端点。**默认异步**,立即返回 queued job,客户端轮询 `GET /v1/videos/{id}`。

**代码:** `router.py:930-1154`

### 1.1 Request Headers

| Header | 必填 | 说明 |
|---|---|---|
| `Authorization: Bearer <key>` | 是 | API key |
| `Content-Type: application/json` | 二选一 | JSON 模式 |
| `Content-Type: multipart/form-data` | 二选一 | 表单模式(支持二进制文件上传作为参考图) |
| `Prefer: wait` | 否 | 启用同步阻塞模式(见 §2) |

### 1.2 Request Body — JSON 模式

```jsonc
{
  "model":           "grok-imagine-video",   // string, 可选, 默认 "grok-video"
  "prompt":          "a red fox in snow",    // string, 必填, 非空
  "seconds":         6,                      // int, 默认 6, 取值见 §8
  "size":            "720x1280",             // string, 默认 "720x1280", 取值见 §8
  "resolution_name": "720p",                 // "480p" | "720p", 默认由 size 推导
  "preset":          "custom",               // "fun" | "normal" | "spicy" | "custom", 默认 "custom"

  // 单张参考图 (legacy, 二选一):
  "image_reference":  { "image_url": "https://... 或 data:image/png;base64,..." },

  // 多张参考图 (最多 7 张, 与 image_reference 二选一):
  "image_references": [
    { "image_url": "https://..." },
    { "image_url": "data:image/png;base64,..." },
    { "file_id":   "abcd1234..." }          // 或已上传的 file_id
  ],

  "wait": false                              // bool, 等同于 "Prefer: wait" header
}
```

**字段校验规则:**

| 字段 | 规则 | 违反时错误 |
|---|---|---|
| body 本身 | 必须是合法 JSON | `400 invalid_request_error invalid_value param=body "Invalid JSON body"` |
| `prompt` | 非空字符串(trim 后) | `400 "prompt is required" param=prompt` |
| `seconds` | 必须 ∈ `{6, 10, 12, 16, 20, 30}` | `400 "seconds must be one of [6, 10, 12, 16, 20, 30]" param=seconds` |
| `seconds` 非整数 | `_coerce_seconds` 失败 | `400 "seconds must be an integer string" param=seconds` |
| `size` | 必须 ∈ 白名单 5 种 | `400 "size must be one of [...]" param=size` |
| `resolution_name` | 必须 ∈ `{"480p", "720p"}` | `400 "resolution_name must be one of [480p, 720p]" param=resolution_name` |
| `preset` | 必须 ∈ `{"fun","normal","spicy","custom"}` | `400 "preset must be one of [...]" param=preset` |
| `model` | 必须是已注册的 video 模型 | `400 "Model '...' is not a video model" param=model` |

> `image_references` 里的每一项必须是 dict;非 dict 会被静默过滤。如果两个字段都给了,`image_references` 优先。

### 1.3 Request Body — multipart/form-data 模式

用于直接上传二进制图片文件作为参考图。

| 字段名 | 类型 | 说明 |
|---|---|---|
| `model` | string | 同 JSON |
| `prompt` | string | 同 JSON |
| `seconds` | string(int) | 同 JSON |
| `size` | string | 同 JSON |
| `resolution_name` | string | 同 JSON |
| `preset` | string | 同 JSON |
| `wait` | string | `"true"`/`"1"`/`"on"`/`"yes"` 启用同步模式 |
| `input_reference` | file | 单个参考图文件 |
| `input_references` | file(多个) | 多个参考图文件(最多 7 个) |

上传的文件必须是 `image/*` MIME,否则:
```
400 "Uploaded file must be an image" param=input_references
```

### 1.4 Response (异步默认)

**立即 200 JSON:**

```json
{
  "id": "video_dd72654a6b094df6adb00570621af473",
  "object": "video",
  "created_at": 1745380717,
  "status": "queued",
  "model": "grok-imagine-video",
  "progress": 0,
  "prompt": "a red fox walking through snow",
  "seconds": "6",
  "size": "720x1280",
  "quality": "standard"
}
```

**字段表(完整可能字段):**

| 字段 | 类型 | 何时出现 | 说明 |
|---|---|---|---|
| `id` | string | 总是 | `video_` + uuid hex(32 字符) |
| `object` | string | 总是 | 固定 `"video"` |
| `created_at` | int | 总是 | Unix 秒 |
| `status` | string | 总是 | `queued` / `in_progress` / `completed` / `failed` |
| `model` | string | 总是 | 请求中的模型名 |
| `progress` | int | 总是 | 0–100 |
| `prompt` | string | 总是 | 规范化后的 prompt(trim) |
| `seconds` | string | 总是 | 字符串形式时长 |
| `size` | string | 总是 | 如 `720x1280` |
| `quality` | string | 总是 | 固定 `"standard"` |
| `completed_at` | int | 仅 `completed` | Unix 秒 |
| `error` | object | 仅 `failed` | `{code, message}`,见 §9.2 |
| `remixed_from_video_id` | string | 可选 | 由原视频扩展而来时 |

### 1.5 Errors 一览

| HTTP | 错误 | 触发条件 |
|---|---|---|
| 400 | `invalid_request_error` / `invalid_value` | 任一参数违反 §1.2 / §1.3 的校验规则 |
| 401 | `authentication_error` / `invalid_api_key` | 缺失或错误 API key |
| 402 | `payment_required` | 账号余额不足以覆盖预扣费用 |
| 429 | `rate_limit_exceeded` | 账号池为空(所有 grok 账号都不可用) |
| 500 | `server_error` | 内部异常 |

---

## 2. POST `/v1/videos` (Prefer: wait 同步模式)

**阻塞直到 job 终态**,`Content-Type: application/json`,但实际是 `StreamingResponse`:先吐空白心跳(防止中间网络掐连接),最后一条才是完整 JSON。客户端按普通 JSON 解析即可(JSON 允许前导空白)。

**触发方式(任一即可):**
- Header `Prefer: wait`
- Query `?wait=true`、`?wait=1`
- JSON body `"wait": true`
- Form 字段 `wait=true`

判定逻辑在 `router.py:902-927` `_wants_video_wait`。

### 2.1 成功 (`router.py:1176-1183`)

完整 job dict,**顶层加 `url` 字段**:

```json
{
  "id": "video_xxx",
  "object": "video",
  "created_at": 1745380717,
  "completed_at": 1745380999,
  "status": "completed",
  "model": "grok-imagine-video",
  "progress": 100,
  "prompt": "...",
  "seconds": "6",
  "size": "720x1280",
  "quality": "standard",
  "url": "https://grok.xitongsp.top/v1/files/video?id=video_xxx"
}
```

### 2.2 失败 (`router.py:1184-1188`)

```json
{"error": {"message": "<本地化错误消息>", "type": "server_error"}}
```

### 2.3 超时 (`router.py:1189-1190`,费用已退)

```json
{"error": {"message": "Video generation timed out, cost refunded", "type": "timeout"}}
```

> **注意:** 这个模式下的错误 body 用的是 **自定义外壳**,和 `AppError.to_dict()` 的标准错误不同(没有 `code`/`param`)。客户端要按 `code` 是否存在分支。

---

## 3. POST `/v1/videos` (上游转发模式)

当本地排队超过阈值(`routing.local_queue_threshold`,默认 150)且能挑到可用 upstream 时触发(`router.py:1045-1087`)。

返回是对 upstream 响应的**透明流转**(`StreamingResponse`,`media_type=application/json`)。

- upstream 返回 body 含 `"url"` 字段 → 视为成功,不退款
- 转发失败时本端吐:
  ```json
  {"error": {"message": "Upstream forwarding failed: <...>", "type": "server_error"}}
  ```
  并退回预扣费用

上游请求的 header:
```
Authorization: Bearer <upstream.api_key>
Content-Type: application/json
Prefer: wait          (如果客户端传了 Prefer: wait)
```
URL 为 `<upstream.url>/v1/videos`,timeout 1200s。

---

## 4. GET `/v1/videos/{video_id}`

标准轮询端点。基于 `retrieve()` (`video.py:1124`)。

**代码:** `router.py:1207-1216`

### 4.1 Request

| 部分 | 值 |
|---|---|
| Path | `/v1/videos/video_<32hex>` |
| Header | `Authorization: Bearer <key>` |
| Body | 无 |

### 4.2 Response — queued / in_progress

```json
{
  "id": "video_xxx",
  "object": "video",
  "created_at": 1745380717,
  "status": "in_progress",
  "model": "grok-imagine-video",
  "progress": 42,
  "prompt": "...",
  "seconds": "6",
  "size": "720x1280",
  "quality": "standard"
}
```

### 4.3 Response — completed

在 `to_dict()` 的基础上 **额外注入 `url` 字段**(绝对 URL,走 `_public_base_url` → https):

```json
{
  "id": "video_xxx",
  "object": "video",
  "created_at": 1745380717,
  "completed_at": 1745380999,
  "status": "completed",
  "model": "grok-imagine-video",
  "progress": 100,
  "prompt": "...",
  "seconds": "6",
  "size": "720x1280",
  "quality": "standard",
  "url": "https://grok.xitongsp.top/v1/files/video?id=video_xxx"
}
```

### 4.4 Response — failed

```json
{
  ...
  "status": "failed",
  "error": {
    "code": "video_generation_failed",
    "message": "参考图片上传失败,请检查图片链接是否有效后重试"
  }
}
```

### 4.5 Errors

| HTTP | 内容 | 条件 |
|---|---|---|
| 400 | `{"error":{"message":"Video 'video_xxx' not found","type":"invalid_request_error","code":"invalid_value","param":"video_id"}}` | `video_id` 不存在(已过期或从未创建) |
| 401 | 标准鉴权错误 | key 无效 |

---

## 5. GET `/v1/videos/{video_id}/content`

OpenAI 标准下载端点。基于 `content_path()` (`video.py:1131`)。

**代码:** `router.py:1219-1223`

### 5.1 Request

| 部分 | 值 |
|---|---|
| Path | `/v1/videos/{video_id}/content` |
| Header | `Authorization: Bearer <key>` |
| Body | 无 |

### 5.2 Response

| HTTP | 内容 | 条件 |
|---|---|---|
| 200 | `FileResponse`,`Content-Type: video/mp4`,`filename=<video_id>.mp4` | job 已完成且磁盘文件存在 |
| 400 | `ValidationError "Video 'xxx' not found" param=video_id` | job 不存在(TTL 过期或从未创建) |
| 409 | `{"error":{"message":"Video content is not ready yet","type":"server_error","code":"video_not_ready"}}` | job 存在但 `status` 不是 `completed`,或 `content_path` 为空 |
| 400 | `ValidationError "Video content for 'xxx' not found" param=video_id` | job 标记为 completed 但磁盘 mp4 被清掉了 |

---

## 6. GET `/v1/files/video`

本地缓存直出端点,**无需鉴权**(无 `verify_api_key` 依赖)。用来把 `job.id` 或 32 位 file_id 映射到磁盘 mp4。

**代码:** `router.py:1279-1291`

这是 `POST /v1/videos` / `GET /v1/videos/{id}` 返回的 `url` 字段最终指向的端点。只要本地 `data/files/videos/` 里的 mp4 还没被 LRU 驱逐,就能直接 GET。

### 6.1 Request

| 部分 | 值 |
|---|---|
| Query | `id=<video_<hex>>` 或 `id=<hex>`(至少 16 位、最多 36 位) |
| ID 正则 | `^(?:video_)?[0-9a-f\-]{16,36}$` |
| Header | 无 |
| Body | 无 |

### 6.2 Response

| HTTP | 内容 | 条件 |
|---|---|---|
| 200 | `FileResponse`,`media_type=video/mp4` | 磁盘存在 |
| 400 | `ValidationError "Invalid file ID" param=id` | ID 不匹配正则 |
| 400 | `ValidationError "Video 'xxx' not found" param=id` | 磁盘不存在 |

---

## 7. POST `/v1/chat/completions` (chat 风格视频)

当 `model` 是 video 类型模型(如 `grok-imagine-video`)时,走 `video.completions()` (`video.py:1195`)。

用 chat 语义包装视频生成,视频地址塞进 assistant `content`;具体形态受配置 `features.video_format` 控制(`video.py:576`):

| 配置值 | `content` 里放什么 |
|---|---|
| `grok_url` (默认) | Grok 上游 CDN 的原始 mp4 URL |
| `local_url` | `https://<app_url>/v1/files/video?id=<hash>` |
| `grok_html` | `<video controls src="<grok_cdn>"></video>` |
| `local_html` | `<video controls src="https://.../v1/files/video?...">` |

### 7.1 Request Body

完整 `ChatCompletionRequest` schema(`schemas.py:29`):

```jsonc
{
  "model": "grok-imagine-video",             // 必填
  "messages": [                              // 必填,至少一条 user 消息包含文本
    {
      "role": "user",
      "content": [
        { "type": "text", "text": "a red fox in snow" },
        { "type": "image_url", "image_url": { "url": "https://..." } }  // 可选参考图
      ]
    }
  ],
  "stream": true,                            // bool, 默认按 features.stream
  "video_config": {                          // 可选
    "seconds":          6,                   // 同 §1 规则
    "size":             "720x1280",          // Literal 白名单, 同 §1
    "resolution_name":  "720p",              // "480p" | "720p"
    "preset":           "custom"             // "fun" | "normal" | "spicy" | "custom"
  },

  // 下面这些对视频模型无效但接受:
  "thinking": null,
  "temperature": 0.8,
  "top_p": 0.95,
  "tools": null,
  "tool_choice": null,
  "max_tokens": null
}
```

**Prompt 提取规则** (`_extract_video_prompt_and_reference`,`video.py:1148`):
- 从 `messages` **倒序**找第一个含非空文本的消息
- 如果 `content` 是字符串,直接当 prompt
- 如果 `content` 是数组,拼接所有 `type=text` 的 `text`,用空格连接
- `type=image_url` 的 URL 自动作为参考图(只取第一个非空的)
- 如果最终 prompt 为空 → `400 "Video prompt cannot be empty" param=messages`

**参数校验错误:**

| 条件 | 错误 |
|---|---|
| `video_config.seconds` 非法 | `400 "seconds must be one of [6, 10, 12, 16, 20, 30]"` |
| `video_config.size` 非法 | pydantic Literal 校验拒绝,`422 Unprocessable Entity` |
| `video_config.resolution_name` 非法 | pydantic Literal 校验拒绝,`422` |
| `video_config.preset` 非法 | pydantic Literal 校验拒绝,`422` |
| 空 prompt | `400 "Video prompt cannot be empty" param=messages` |

### 7.2 Response — 非流式 (`stream=false`)

`make_chat_response` (`_format.py:93`):

```json
{
  "id": "chatcmpl-1745380717xxxx",
  "object": "chat.completion",
  "created": 1745380717,
  "model": "grok-imagine-video",
  "choices": [{
    "index": 0,
    "message": {
      "role": "assistant",
      "content": "https://grok.xitongsp.top/v1/files/video?id=abcd1234...",
      "reasoning_content": "Uploading references\nGenerating video\nFinalizing..."
    },
    "finish_reason": "stop"
  }],
  "usage": {
    "prompt_tokens": 12,
    "completion_tokens": 35,
    "total_tokens": 47,
    "prompt_tokens_details": { "...": "..." },
    "completion_tokens_details": { "...": "..." }
  }
}
```

`reasoning_content` 是进度摘要汇总(去重后的阶段描述);没进度时省略。

### 7.3 Response — 流式 (`stream=true`)

SSE 序列 (`video.py:1190-1213`):

**1. 多帧 thinking chunk**(每次进度百分比提升推一次)
```
data: {"id":"chatcmpl-...","object":"chat.completion.chunk","created":...,"model":"grok-imagine-video","choices":[{"index":0,"delta":{"role":"assistant","reasoning_content":"Generating video (42%)"}}]}
```

**2. 1 帧内容 chunk**(最终 URL 或 HTML 写入 `delta.content`)
```
data: {"id":"chatcmpl-...","choices":[{"index":0,"delta":{"role":"assistant","content":"https://.../v1/files/video?id=..."}}]}
```

**3. 1 帧收尾 chunk**(`finish_reason=stop`,`content=""`)
```
data: {"id":"chatcmpl-...","choices":[{"index":0,"delta":{"role":"assistant","content":""},"finish_reason":"stop"}]}
```

**4. 终止**
```
data: [DONE]
```

### 7.4 Errors(流式)

流中途失败会发 error 事件,之后仍以 `[DONE]` 收尾:
```
event: error
data: {"error":{"message":"...","type":"server_error"}}

data: [DONE]
```

此时对应的预扣费**已退**(`router.py:505-517`)。

---

## 8. 参数取值表

### 8.1 `seconds`(`_SUPPORTED_VIDEO_LENGTHS`,`video.py:56`)

| 值 | 分段方案 (`_build_segment_lengths`) | 典型总耗时 |
|---|---|---|
| 6 | `[6]` | 单段 |
| 10 | `[10]` | 单段 |
| 12 | `[6, 6]` | 两段拼接 |
| 16 | `[10, 6]` | 两段拼接 |
| 20 | `[10, 10]` | 两段拼接 |
| 30 | `[10, 10, 10]` | 三段拼接 |

### 8.2 `size`(`_VIDEO_SIZE_MAP`,`video.py:61`)

| size | 上游 aspect_ratio | 默认 resolution_name |
|---|---|---|
| `720x1280` | `9:16` | `720p` |
| `1280x720` | `16:9` | `720p` |
| `1024x1024` | `1:1` | `720p` |
| `1024x1792` | `9:16` | `720p` |
| `1792x1024` | `16:9` | `720p` |

### 8.3 `resolution_name`

| 值 | 说明 |
|---|---|
| `720p` | 默认 |
| `480p` | 低分辨率 |

### 8.4 `preset`(`_PRESET_FLAGS`,`video.py:68`)

| 值 | 传给上游的 flag |
|---|---|
| `fun` | `--mode=extremely-crazy` |
| `normal` | `--mode=normal` |
| `spicy` | `--mode=extremely-spicy-or-crazy` |
| `custom` | `--mode=custom`(默认) |

### 8.5 参考图(`image_references`)

- 最多 **7 张**
- 每项格式:
  - `{"image_url": "https://..."}` — http(s) URL
  - `{"image_url": "data:image/xxx;base64,..."}` — data URI
  - `{"file_id": "<upload-file-id>"}` — 预上传的 file id
- MIME 必须是图片(`image/jpeg` / `image/png` / `image/webp`)
- multipart 模式下还支持直接上传二进制

---

## 9. 错误对象形态汇总

### 9.1 标准 `AppError` 外壳(绝大多数 HTTP 错误)

FastAPI 异常处理器从 `AppError.to_dict()` 统一渲染:

```json
{
  "error": {
    "message": "...",
    "type":    "invalid_request_error" | "authentication_error" | "rate_limit_exceeded" | "upstream_error" | "server_error",
    "code":    "...",
    "param":   "..."    // 仅 ValidationError 等携带 param 时出现
  }
}
```

**视频相关的完整错误码表:**

| 场景 | HTTP | `type` | `code` | 触发位置 |
|---|---|---|---|---|
| 参数校验失败 | 400 | `invalid_request_error` | `invalid_value` | `ValidationError` 各处 |
| `video_id` 非法/不存在 | 400 | `invalid_request_error` | `invalid_value` | `video.py:1127`, `1134`, `router.py:1285`, `1291` |
| body JSON 解析失败 | 400 | `invalid_request_error` | `invalid_value` | `router.py:944` |
| 模型不是视频模型 | 400 | `invalid_request_error` | `invalid_value` | `video.py:1086` |
| content 尚未就绪 | 409 | `server_error` | `video_not_ready` | `video.py:1136-1140` |
| pydantic Literal 违反 | 422 | - | - | FastAPI 自动 |
| 鉴权失败 | 401 | `authentication_error` | `invalid_api_key` | `AuthError` |
| 余额不足 | 402 | - | `payment_required` | `router.py:75-78` |
| 无可用账号 | 429 | `rate_limit_exceeded` | `rate_limit_exceeded` | `RateLimitError` |
| 上游 4xx/5xx | 502(默认) | `upstream_error` | `upstream_error` | `UpstreamError` |
| 流 idle 超时 | 504 | `upstream_error` | `stream_idle_timeout` | `StreamIdleTimeout` |
| 其他未捕获 | 500 | `server_error` | `internal_error` | `AppError` 默认 |

### 9.2 Job 内嵌 `error` 字段(`failed` 状态)

`_job_error_payload()` (`video.py:893`):

```json
{
  "code": "video_generation_failed",
  "message": "<本地化后的中文消息>"
}
```

**仅 `code` + `message` 两个字段**,没有 `type`、`param`。

### 9.3 `Prefer: wait` / upstream 转发失败的简化外壳

```json
{"error": {"message": "...", "type": "server_error" | "timeout"}}
```

**只有 `message` 和 `type`**,**没有 `code`/`param`**。客户端要兼容这两种外壳时,按 `code` 是否存在分支。

---

## 10. 错误文案本地化映射表

`_localize_video_error()` (`video.py:897`) 按关键字(小写包含)匹配上游/内部的英文错误串,转成中文。规则按下表**顺序短路**,第一条命中即返回。用于 job `error.message` 与 `Prefer: wait` 模式的失败返回。

| 分类 | 匹配关键字 | 本地化消息 |
|---|---|---|
| 内容审核 | `content is moderated` / `content-moderated` | 参考图片含有违规内容,已被平台审核拦截,请更换图片后重试 |
| 参考图格式 | `unsupported mime type` | 参考图片格式无法识别,请确保图片链接直接指向 JPEG / PNG / WEBP 文件 |
| 参考图格式 | `video files are not supported` | 不支持将视频文件作为参考图,请提供静态图片(JPEG / PNG / WEBP) |
| 参考图链接失效 | `reference upload failed` + (`400` 或 `403`) | 参考图片上传失败,请检查图片链接是否有效后重试 |
| 参考图上传 | `reference upload failed` / `asset upload failed` / `could not resolve uploaded asset` | 参考图片上传失败,请稍后重试 |
| 上游返回不完整 | `no final video url` / `no artifact` / `create-post returned no` / `without a resolvable url` | 视频生成失败,平台未返回有效结果,请稍后重试 |
| 重试耗尽 | `segment failed after retries` | 视频生成超时或多次失败,请稍后重试 |
| 下载失败 | `video download failed` | 视频下载失败,请稍后重试 |
| 账号池 | `no available accounts` | 当前账号资源不足,请稍后重试 |
| 启动中 | `account directory not initialised` / `account directory not initialized` | 服务正在初始化,请稍后重试 |
| 上游 HTTP 状态 | `upstream returned` | 上游服务返回异常,请稍后重试 |
| 网络/代理 | `socks` / `proxy error` / `proxyerror` / `host unreachable` / `network is unreachable` / `ttl expired` / `transport error` / `connect` / `timeout` / `timed out` | 网络异常,请稍后重试 |
| 空消息 / 仅类名 | `raw=""` 或 仅含 `XxxError`(无空格) | 视频生成失败,请稍后重试 |
| fallback | 以上全不匹配 | 原始英文串透传(保留调试价值) |

对 `UpstreamError`,优先尝试解析 `details.body` 里的 Grok 原始 JSON,取 `message` 字段再做本地化;没 body 或解析失败则本地化 `real.message` 本身。

> `connect` 这条会命中 `connection reset/aborted/closed`、`cannot connect to host`、`server disconnected` 等多种形态,不需要分别写。

---

## 11. 典型时序

### 11.1 OpenAI 风格(SDK 默认)

```
POST /v1/videos                → 200 JSON {status:"queued", id:"video_..."}
GET  /v1/videos/{id}            → 200 JSON {status:"in_progress", progress:40}   (多次轮询)
GET  /v1/videos/{id}            → 200 JSON {status:"completed", url:"https://..."}
GET  url                        → 200 video/mp4
```

### 11.2 同步阻塞(`Prefer: wait`)

```
POST /v1/videos (Prefer: wait)
  → (长时间心跳空白)...
  → 200 JSON {status:"completed", url:"https://..."}          或
  → 200 JSON {"error":{"message":"...","type":"server_error"}}
```

### 11.3 chat 风格(插 markdown / 前端展示)

```
POST /v1/chat/completions  (model=grok-imagine-video)
  → (stream)  多帧 reasoning → 1 帧 content(URL) → 收尾 → [DONE]
  或
  → 200 JSON chat.completion,content=URL,reasoning_content=阶段描述
```

### 11.4 本地过载转发

```
POST /v1/videos  (本地 in_flight > routing.local_queue_threshold)
  → 选一个 upstream
  → 透传 upstream 响应 body
  → 成功则不退款,失败则退款 + 返回 {"error":{"message":"Upstream forwarding failed: ...","type":"server_error"}}
```

---

## 附录: 配置项

| 配置 key | 默认 | 说明 |
|---|---|---|
| `app.app_url` | `""` | 对外 base URL,影响所有视频 `url` 字段 |
| `features.stream` | `true` | chat_completions 的默认 stream 值 |
| `features.video_format` | `grok_url` | chat 模式下 content 放什么:`grok_url` / `local_url` / `grok_html` / `local_html` |
| `features.thinking` | `true` | 是否吐 `reasoning_content` 进度 |
| `routing.local_queue_threshold` | 150 | 本地排队上限,超过走 upstream 转发 |
| `video.max_concurrency` | 由 `_get_video_sem` 决定 | 本地并发 semaphore |
| `video.cache_max_bytes` | - | `_video_cache_max_bytes`,LRU 磁盘上限 |
| `billing.costs.video_by_seconds` | `{}` | 按时长查成本的映射表 |
| `billing.costs.video_per_second` | 0 | 按秒单价,`video_by_seconds` 不匹配时 fallback |
| `billing.costs.video` | 50 | 最终 fallback 单价 |

> **REST `/v1/videos` 的 `url` 字段始终指向本地 `/v1/files/video`,`features.video_format` 只影响 chat 风格接口。**
