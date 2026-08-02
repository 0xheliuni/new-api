# Grok2API 中文 API 文档

> 基于 FastAPI 的 Grok 网关，提供 OpenAI / Anthropic 兼容接口。
> 本文覆盖所有 `/v1/*` 端点，重点展开视频接口的单图 / 多图用法。

- [概述](#概述)
- [鉴权](#鉴权)
- [错误返回](#错误返回)
- [计费](#计费)
- [模型](#模型)
- [API 端点](#api-端点)
  - [GET /v1/models](#get-v1models)
  - [POST /v1/chat/completions](#post-v1chatcompletions)
  - [POST /v1/responses](#post-v1responses)
  - [POST /v1/messages](#post-v1messages)
  - [POST /v1/images/generations](#post-v1imagesgenerations)
  - [POST /v1/images/edits](#post-v1imagesedits)
  - [POST /v1/videos](#post-v1videos)
  - [GET /v1/videos/{video_id}](#get-v1videosvideo_id)
  - [GET /v1/videos/{video_id}/content](#get-v1videosvideo_idcontent)
  - [GET /v1/files/{image,video}](#get-v1files)

---

## 概述

| 项目 | 值 |
| :-- | :-- |
| Base URL（示例） | `http://localhost:8000` |
| Content-Type | `application/json`（视频、图像编辑额外支持 `multipart/form-data`） |
| 鉴权方式 | HTTP Header `Authorization: Bearer <api_key>` |
| 主要协议 | OpenAI 兼容；Anthropic Messages 兼容子集 |

所有 `/v1/*` 接口均可用 OpenAI 官方 SDK 直接调用，只需把 `base_url` 指向本服务。

---

## 鉴权

- `/v1/*`：使用 `app.api_key`。若为空则不强制校验。
- 动态用户 Key：后台可为每个用户签发独立 Key，按次扣除积分额度。
- `/admin/*`：使用 `app.app_key`（默认 `grok2api`）。
- `/webui/*`：由 `app.webui_enabled` + `app.webui_key` 控制。

---

## 错误返回

统一使用 OpenAI 风格错误体：

```json
{
  "error": {
    "message": "seconds must be one of [6, 10, 12, 16, 20]",
    "type": "invalid_request_error",
    "param": "seconds",
    "code": null
  }
}
```

常见 HTTP 状态：

| 状态码 | 含义 |
| :-- | :-- |
| `400` | 参数错误（`ValidationError`） |
| `401` | 未鉴权 / Key 无效 |
| `402` | 余额不足（启用计费后） |
| `404` | 资源不存在（video_id 等） |
| `429` | 上游节流（一般经过队列已被消化） |
| `5xx` | 上游或服务内部错误 |

---

## 计费

仅当 `billing.enabled = true` 时生效。`app.api_key` 静态 Key 不扣费，只有动态用户 Key 才计入。积分在请求入口处**预扣**，失败自动退还（含流式中途失败）。

### 对话

| 类型 | 积分 |
| :-- | :-- |
| `chat`（默认） | 30 |
| `chat_fast` · fast mode | 30 |
| `chat_expert` · expert mode | 90 |
| `chat_heavy` · heavy mode | 150 |

### 图片

**按生成张数线性计费**：`实际 = 60 × n`。`n` 取值由模型决定：`lite` 1–4、标准 1–10、`edit` 1–2。

| n | 积分 |
| :-- | :-- |
| 1 | 60 |
| 2 | 120 |
| 4 | 240 |
| 10 | 600 |

### 视频

**单图 / 文生视频**按时长分档；多图（`/v1/videos` 的 `image_references` 数组或 `input_references` 多文件上传）**整体费用 × 4/3**（在原价基础上加 1/3）。

| 时长 | 单图 / 文生 | 多图（≥2 张，整数向下取整） |
| :-- | :-- | :-- |
| 6s | 60 | 80 |
| 10s | 100 | 133 |
| 12s | 140 | 186 |
| 16s | 240 | 320 |
| 20s | 500 | 666 |

说明：
- 上传**一张**参考图（图生视频）的计费逻辑与文生视频完全一致，**不加价**。
- 只有上传 **≥2 张**参考图时才触发 4/3 倍率。
- `/v1/chat/completions` 走视频模型同样遵守这一规则，多模态消息里出现 ≥2 个 `image_url` 块自动按多图计费。

---

## 模型

完整列表以 `GET /v1/models` 为准。以下为 2026-04 当前清单。

**Chat**

| 模型名 | mode | tier |
| :-- | :-- | :-- |
| `grok-4.20-0309-non-reasoning` | fast | basic |
| `grok-4.20-0309` | auto | basic |
| `grok-4.20-0309-reasoning` | expert | basic |
| `grok-4.20-0309-non-reasoning-super` | fast | super |
| `grok-4.20-0309-super` | auto | super |
| `grok-4.20-0309-reasoning-super` | expert | super |
| `grok-4.20-0309-non-reasoning-heavy` | fast | heavy |
| `grok-4.20-0309-heavy` | auto | heavy |
| `grok-4.20-0309-reasoning-heavy` | expert | heavy |
| `grok-4.20-multi-agent-0309` | heavy | heavy |

**Image**

| 模型名 | 说明 |
| :-- | :-- |
| `grok-imagine-image-lite` | 低配文生图 |
| `grok-imagine-image` | 标准文生图 |
| `grok-imagine-image-pro` | 高质量文生图 |
| `grok-imagine-image-edit` | 图像编辑（需上传参考图） |

**Video**

| 模型名 | 说明 |
| :-- | :-- |
| `grok-imagine-video` | 文生视频 / 图生视频（支持单图、多图） |

---

## API 端点

### GET /v1/models

列出当前启用的模型。

```bash
curl http://localhost:8000/v1/models \
  -H "Authorization: Bearer $API_KEY"
```

---

### POST /v1/chat/completions

OpenAI Chat Completions 兼容接口，同时支撑对话 / 图像 / 视频。返回格式根据 `model` 自动适配。

**通用字段**

| 字段 | 类型 | 说明 |
| :-- | :-- | :-- |
| `model` | string | 必填 |
| `messages` | array | 支持文本与多模态内容块（`text` / `image_url`） |
| `stream` | bool | 是否流式 |
| `thinking` | bool | 是否显式输出思考过程 |
| `reasoning_effort` | string | `none` / `minimal` / `low` / `medium` / `high` / `xhigh` |
| `tools` | array | OpenAI function tools 结构 |
| `image_config` | object | 图像模型配置（见下） |
| `video_config` | object | 视频模型配置（见下） |

**`image_config`**

| 字段 | 说明 |
| :-- | :-- |
| `n` | `lite`: 1-4；其他生成模型: 1-10；编辑模型: 1-2 |
| `size` | `1280x720` / `720x1280` / `1792x1024` / `1024x1792` / `1024x1024` |
| `response_format` | `url` / `b64_json` |

**`video_config`**

| 字段 | 取值 |
| :-- | :-- |
| `seconds` | `6` / `10` / `12` / `16` / `20` |
| `size` | `720x1280` / `1280x720` / `1024x1024` / `1024x1792` / `1792x1024` |
| `resolution_name` | `480p` / `720p` |
| `preset` | `fun` / `normal` / `spicy` / `custom` |

#### 示例 · 对话

```bash
curl http://localhost:8000/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $API_KEY" \
  -d '{
    "model": "grok-4.20-0309",
    "stream": true,
    "messages": [
      {"role": "user", "content": "你好"}
    ]
  }'
```

#### 示例 · 图像生成

```bash
curl http://localhost:8000/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $API_KEY" \
  -d '{
    "model": "grok-imagine-image",
    "messages": [
      {"role": "user", "content": "一只在太空漂浮的猫"}
    ],
    "image_config": {
      "n": 2,
      "size": "1024x1024",
      "response_format": "url"
    }
  }'
```

> 费用随 `image_config.n` 线性：上例 `n=2` 扣 120 积分。

#### 示例 · 文生视频

```bash
curl http://localhost:8000/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $API_KEY" \
  -d '{
    "model": "grok-imagine-video",
    "stream": true,
    "messages": [
      {"role": "user", "content": "霓虹雨夜街头，电影感慢镜头追拍"}
    ],
    "video_config": {
      "seconds": 10,
      "size": "1792x1024",
      "resolution_name": "720p",
      "preset": "normal"
    }
  }'
```

#### 示例 · 图生视频（单图）

把参考图作为一条 `image_url` 内容块放到 `messages` 里。`url` 可以是 URL，也可以是 `data:` URI（base64）。

```bash
curl http://localhost:8000/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $API_KEY" \
  -d '{
    "model": "grok-imagine-video",
    "messages": [
      {
        "role": "user",
        "content": [
          {"type": "text", "text": "让这张图里的人物转身微笑"},
          {"type": "image_url", "image_url": {"url": "https://example.com/a.jpg"}}
        ]
      }
    ],
    "video_config": { "seconds": 6, "resolution_name": "720p" }
  }'
```

#### 示例 · 图生视频（多图）

放两条或更多 `image_url` 块即可。**≥2 张自动按 4/3 计费**（即原价 + 1/3），单图不加价。

```bash
curl http://localhost:8000/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $API_KEY" \
  -d '{
    "model": "grok-imagine-video",
    "messages": [
      {
        "role": "user",
        "content": [
          {"type": "text", "text": "从第一张镜头平滑过渡到第二张"},
          {"type": "image_url", "image_url": {"url": "https://example.com/a.jpg"}},
          {"type": "image_url", "image_url": {"url": "data:image/png;base64,iVBORw0K..."}}
        ]
      }
    ],
    "video_config": { "seconds": 10, "resolution_name": "720p" }
  }'
```

---

### POST /v1/responses

OpenAI Responses API 的兼容子集。

```bash
curl http://localhost:8000/v1/responses \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $API_KEY" \
  -d '{
    "model": "grok-4.20-0309",
    "input": "解释一下量子隧穿",
    "stream": true
  }'
```

---

### POST /v1/messages

Anthropic Messages API 兼容接口。

```bash
curl http://localhost:8000/v1/messages \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $API_KEY" \
  -d '{
    "model": "grok-4.20-0309",
    "stream": true,
    "messages": [
      {"role": "user", "content": "用三句话解释量子隧穿"}
    ]
  }'
```

---

### POST /v1/images/generations

独立图像生成端点。计费：**60 × n**（`n` 由请求指定，`lite` 1–4，其他模型 1–10）。

```bash
curl http://localhost:8000/v1/images/generations \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $API_KEY" \
  -d '{
    "model": "grok-imagine-image",
    "prompt": "一只在太空漂浮的猫",
    "n": 1,
    "size": "1024x1024",
    "response_format": "url"
  }'
```

---

### POST /v1/images/edits

独立图像编辑端点，仅接受 `multipart/form-data`。计费：**60 × n**（`n` 限 1–2）。

```bash
curl http://localhost:8000/v1/images/edits \
  -H "Authorization: Bearer $API_KEY" \
  -F "model=grok-imagine-image-edit" \
  -F "prompt=把这张图变清晰一些" \
  -F "image[]=@/path/to/image.png" \
  -F "n=1" \
  -F "size=1024x1024" \
  -F "response_format=url"
```

---

### POST /v1/videos

异步视频任务。**同时支持 `application/json` 与 `multipart/form-data`**；两种格式的参考图字段名不同，需按格式分别处理。

#### 请求字段

| 字段 | 类型 | 说明 |
| :-- | :-- | :-- |
| `model` | string | 固定 `grok-imagine-video` |
| `prompt` | string | 视频描述（必填） |
| `seconds` | int | `6` / `10` / `12` / `16` / `20` |
| `size` | string | `720x1280` / `1280x720` / `1024x1024` / `1024x1792` / `1792x1024` |
| `resolution_name` | string | `480p` / `720p` |
| `preset` | string | `fun` / `normal` / `spicy` / `custom` |

#### 参考图字段（图生视频）

**单图与多图字段名不同，不能混用**。JSON 与 Multipart 各自独立。

| 场景 | JSON | Multipart |
| :-- | :-- | :-- |
| 单图 | `image_reference: { "image_url": "<URL 或 data URI>" }` | `input_reference=@file` |
| 多图 | `image_references: [ { "image_url": "..." }, ... ]` | 重复 `input_references=@file` |

`image_url` 可以是：
- `http://` / `https://` 链接
- `data:image/<type>;base64,<...>` URI

#### 计费规则

- **文生 / 单图**：按时长分档，参见 [计费 · 视频](#视频)。
- **多图（≥2 张）**：在单图价基础上 **× 4/3**（即加 1/3），向下取整。
- 单图的图生视频与文生视频计费一致，**不加价**。

#### 返回示例（任务已入队）

```json
{
  "id": "vg_abc123...",
  "object": "video.generation",
  "created_at": 1714780800,
  "status": "queued",
  "model": "grok-imagine-video",
  "progress": 0,
  "prompt": "霓虹雨夜街头...",
  "seconds": "10",
  "size": "1792x1024",
  "quality": "720p"
}
```

`status` 取值：`queued` / `processing` / `completed` / `failed`。

#### 示例 1 · 文生视频（JSON）

```bash
curl http://localhost:8000/v1/videos \
  -H "Authorization: Bearer $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "grok-imagine-video",
    "prompt": "霓虹雨夜街头，电影感慢镜头追拍",
    "seconds": 10,
    "size": "1792x1024",
    "resolution_name": "720p",
    "preset": "normal"
  }'
```

#### 示例 2 · 图生视频 · 单图（JSON）

```bash
curl http://localhost:8000/v1/videos \
  -H "Authorization: Bearer $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "grok-imagine-video",
    "prompt": "让这张照片里的人物转身微笑",
    "seconds": 6,
    "image_reference": {
      "image_url": "https://example.com/ref.jpg"
    }
  }'
```

#### 示例 3 · 图生视频 · 多图（JSON，URL + base64 混用）

```bash
curl http://localhost:8000/v1/videos \
  -H "Authorization: Bearer $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "grok-imagine-video",
    "prompt": "从第一张镜头平滑过渡到第二张",
    "seconds": 10,
    "image_references": [
      { "image_url": "https://example.com/a.jpg" },
      { "image_url": "data:image/png;base64,iVBORw0K..." }
    ]
  }'
```

#### 示例 4 · 文生视频（Multipart）

```bash
curl http://localhost:8000/v1/videos \
  -H "Authorization: Bearer $API_KEY" \
  -F "model=grok-imagine-video" \
  -F "prompt=霓虹雨夜街头，电影感慢镜头追拍" \
  -F "seconds=10" \
  -F "size=1792x1024" \
  -F "resolution_name=720p" \
  -F "preset=normal"
```

#### 示例 5 · 图生视频 · 单图（Multipart）

```bash
curl http://localhost:8000/v1/videos \
  -H "Authorization: Bearer $API_KEY" \
  -F "model=grok-imagine-video" \
  -F "prompt=参考这张照片生成视频" \
  -F "seconds=6" \
  -F "input_reference=@/path/to/ref.jpg"
```

#### 示例 6 · 图生视频 · 多图（Multipart）

```bash
curl http://localhost:8000/v1/videos \
  -H "Authorization: Bearer $API_KEY" \
  -F "model=grok-imagine-video" \
  -F "prompt=从 A 过渡到 B" \
  -F "seconds=10" \
  -F "input_references=@/path/a.jpg" \
  -F "input_references=@/path/b.jpg"
```

---

### GET /v1/videos/{video_id}

轮询任务状态。建议的轮询间隔：2–5 秒；`completed` / `failed` 后停止。

```bash
curl http://localhost:8000/v1/videos/<video_id> \
  -H "Authorization: Bearer $API_KEY"
```

完成后返回体会多出：

```json
{
  "status": "completed",
  "progress": 100,
  "completed_at": 1714781400,
  "video_url": "https://.../cdn/..."
}
```

失败时：

```json
{
  "status": "failed",
  "error": { "code": "upstream_failure", "message": "..." }
}
```

---

### GET /v1/videos/{video_id}/content

下载最终 MP4 内容（须在 `status = completed` 之后调用）。

```bash
curl -L http://localhost:8000/v1/videos/<video_id>/content \
  -H "Authorization: Bearer $API_KEY" \
  -o result.mp4
```

---

### GET /v1/files/{image,video}

本地缓存的文件代理。返回的图片 / 视频链接如果配置为 `local_url` / `local_html` 格式，会指向这两个端点。无需鉴权。

```bash
curl "http://localhost:8000/v1/files/video?id=<job_id>" -o result.mp4
curl "http://localhost:8000/v1/files/image?id=<file_id>" -o result.png
```

---

## 客户端最佳实践

- **视频轮询**：优先用流式 `/v1/chat/completions`（内部会发心跳空白），客户端无需显式轮询；如果用 `/v1/videos`，建议 2–5s 间隔轮询，5 分钟还没完成再放弃。
- **多图参考**：`/v1/videos` 的多图通过 `image_references` / `input_references`，`/v1/chat/completions` 的多图直接叠加 `image_url` 块；两边都按 4/3 计费（原价 + 1/3），单图不加价。
- **base64 vs URL**：两种都支持；URL 对服务更友好（不占内存），base64 适合内网或图片不对外的场景。
- **SDK 接入**：OpenAI 官方 SDK 设置 `base_url` 即可；视频单独的 `/v1/videos` 需要自己写 HTTP 调用（SDK 中无对应方法）。
