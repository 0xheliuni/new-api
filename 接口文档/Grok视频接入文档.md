# Grok 视频接入文档

> 本文档面向客户端开发者，描述通过 new-api 网关接入 Grok 视频生成能力的完整接口规范与使用方法。

---

## 目录

- [1. 概述](#1-概述)
- [2. 认证](#2-认证)
- [3. 接口列表](#3-接口列表)
  - [3.1 提交视频生成 — POST /v1/videos](#31-提交视频生成--post-v1videos)
  - [3.2 查询任务状态 — GET /v1/videos/{task_id}](#32-查询任务状态--get-v1videostask_id)
  - [3.3 下载视频 — GET /v1/videos/{task_id}/content](#33-下载视频--get-v1videostask_idcontent)
  - [3.4 Remix 视频 — POST /v1/videos/{video_id}/remix](#34-remix-视频--post-v1videosvideo_idremix)
- [4. 参数取值表](#4-参数取值表)
- [5. 状态流转与轮询策略](#5-状态流转与轮询策略)
- [6. 计费规则](#6-计费规则)
- [7. 错误处理](#7-错误处理)
- [8. 客户端接入示例](#8-客户端接入示例)
  - [8.1 cURL](#81-curl)
  - [8.2 Python](#82-python)
  - [8.3 Node.js](#83-nodejs)
- [9. 注意事项](#9-注意事项)

---

## 1. 概述

| 项目 | 值 |
|:--|:--|
| Base URL | `https://<your-host>` |
| 认证方式 | `Authorization: Bearer <api_key>` |
| Content-Type | `application/json`（同时支持 `multipart/form-data`） |
| 视频模型 | `grok-imagine-video` |
| 任务模式 | 异步：提交 → 轮询 → 下载 |
| 任务 ID 格式 | `task_` + 32 位随机字符（如 `task_a1b2c3d4...`） |

new-api 作为 API 网关，将客户端请求代理到 Grok 上游，并提供统一的鉴权、计费、任务管理能力。服务端每 15 秒自动轮询上游更新任务状态。

---

## 2. 认证

所有接口均需在 HTTP Header 中携带 API Key：

```
Authorization: Bearer <api_key>
```

`/v1/videos/{task_id}/content` 端点额外支持 Session 认证（后台面板场景）。

---

## 3. 接口列表

### 3.1 提交视频生成 — POST /v1/videos

异步提交视频生成任务，立即返回任务 ID 和初始状态。

> 兼容路径：`POST /v1/video/generations`（旧版，行为一致）

#### 请求字段

| 字段 | 类型 | 必填 | 说明 |
|:--|:--|:--|:--|
| `model` | string | 是 | 固定 `grok-imagine-video` |
| `prompt` | string | 是 | 视频描述文本 |
| `seconds` | int / string | 否 | 视频时长（秒），取值 `6` / `10` / `12` / `16` / `20`，默认 `6` |
| `size` | string | 否 | 视频尺寸，默认 `720x1280`，取值见 [参数取值表](#4-参数取值表) |
| `duration` | int | 否 | 等同 `seconds`（扩展字段，二选一） |
| `resolution_name` | string | 否 | 分辨率名称：`480p` / `720p` |
| `preset` | string | 否 | 风格预设：`fun` / `normal` / `spicy` / `custom` |
| `image_reference` | object | 否 | 单图参考（JSON 格式），格式 `{"image_url": "<URL 或 data URI>"}` |
| `image_references` | array | 否 | 多图参考（JSON 格式），格式 `[{"image_url": "..."}, ...]` |
| `input_reference` | file | 否 | 单图参考（Multipart 格式） |
| `input_references` | file[] | 否 | 多图参考（Multipart 格式，重复字段名） |
| `metadata` | object | 否 | 扩展元数据，可在其中传递 `seconds` / `duration` 等 |

> 单图字段（`image_reference` / `input_reference`）与多图字段（`image_references` / `input_references`）不可混用。

#### JSON 请求示例

**文生视频：**

```json
{
  "model": "grok-imagine-video",
  "prompt": "霓虹雨夜街头，电影感慢镜头追拍",
  "seconds": 10,
  "size": "1792x1024",
  "resolution_name": "720p",
  "preset": "normal"
}
```

**单图图生视频：**

```json
{
  "model": "grok-imagine-video",
  "prompt": "让这张照片里的人物转身微笑",
  "seconds": 6,
  "image_reference": {
    "image_url": "https://example.com/ref.jpg"
  }
}
```

**多图图生视频：**

```json
{
  "model": "grok-imagine-video",
  "prompt": "从第一张镜头平滑过渡到第二张",
  "seconds": 10,
  "image_references": [
    { "image_url": "https://example.com/a.jpg" },
    { "image_url": "data:image/png;base64,iVBORw0K..." }
  ]
}
```

#### Multipart 请求示例

```bash
# 单图图生视频
curl https://<your-host>/v1/videos \
  -H "Authorization: Bearer $API_KEY" \
  -F "model=grok-imagine-video" \
  -F "prompt=参考这张照片生成视频" \
  -F "seconds=6" \
  -F "input_reference=@/path/to/ref.jpg"

# 多图图生视频
curl https://<your-host>/v1/videos \
  -H "Authorization: Bearer $API_KEY" \
  -F "model=grok-imagine-video" \
  -F "prompt=从 A 过渡到 B" \
  -F "seconds=10" \
  -F "input_references=@/path/a.jpg" \
  -F "input_references=@/path/b.jpg"
```

#### 响应字段

| 字段 | 类型 | 说明 |
|:--|:--|:--|
| `id` | string | 任务 ID（格式 `task_xxx`） |
| `object` | string | 固定 `"video"` |
| `model` | string | 使用的模型名 |
| `status` | string | 任务状态：`queued` |
| `progress` | int | 进度百分比（0） |
| `created_at` | int64 | 创建时间（Unix 时间戳） |
| `prompt` | string | 输入的视频描述 |
| `seconds` | string | 视频时长 |
| `size` | string | 视频尺寸 |
| `quality` | string | 分辨率（如 `720p`） |

#### 响应示例

```json
{
  "id": "task_a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6",
  "object": "video",
  "created_at": 1714780800,
  "status": "queued",
  "model": "grok-imagine-video",
  "progress": 0,
  "prompt": "霓虹雨夜街头，电影感慢镜头追拍",
  "seconds": "10",
  "size": "1792x1024",
  "quality": "720p"
}
```

---

### 3.2 查询任务状态 — GET /v1/videos/{task_id}

轮询视频生成任务的当前状态。

> 兼容路径：`GET /v1/video/generations/{task_id}`（旧版，行为一致）

#### 请求

```
GET /v1/videos/{task_id}
Authorization: Bearer <api_key>
```

`task_id`：提交任务时返回的 `id` 字段值。

#### 响应字段

| 字段 | 类型 | 说明 |
|:--|:--|:--|
| `id` | string | 任务 ID |
| `object` | string | `"video"` |
| `model` | string | 模型名 |
| `status` | string | 当前状态：`queued` / `in_progress` / `completed` / `failed` |
| `progress` | int | 进度百分比（0-100） |
| `created_at` | int64 | 创建时间 |
| `completed_at` | int64 | 完成时间（仅 `completed` 状态） |
| `metadata.url` | string | 视频访问地址（仅 `completed` 状态） |
| `metadata.storage_status` | string | 媒体转存状态（如启用 CloudPaste） |
| `metadata.preview_url` | string | 预览地址（如启用 CloudPaste） |
| `error` | object | 错误信息（仅 `failed` 状态） |
| `error.code` | string | 错误代码 |
| `error.message` | string | 错误描述 |

#### 各状态响应示例

**queued（已入队）：**

```json
{
  "id": "task_a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6",
  "object": "video",
  "status": "queued",
  "progress": 0,
  "created_at": 1714780800,
  "model": "grok-imagine-video"
}
```

**in_progress（生成中）：**

```json
{
  "id": "task_a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6",
  "object": "video",
  "status": "in_progress",
  "progress": 45,
  "created_at": 1714780800,
  "model": "grok-imagine-video"
}
```

**completed（已完成）：**

```json
{
  "id": "task_a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6",
  "object": "video",
  "status": "completed",
  "progress": 100,
  "created_at": 1714780800,
  "completed_at": 1714781400,
  "model": "grok-imagine-video",
  "metadata": {
    "url": "https://<upstream-or-storage>/path/to/video.mp4",
    "storage_status": "success",
    "preview_url": "https://<storage>/preview/video.mp4"
  }
}
```

**failed（已失败）：**

```json
{
  "id": "task_a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6",
  "object": "video",
  "status": "failed",
  "progress": 0,
  "created_at": 1714780800,
  "model": "grok-imagine-video",
  "error": {
    "code": "upstream_failure",
    "message": "Video generation failed due to content policy violation"
  }
}
```

---

### 3.3 下载视频 — GET /v1/videos/{task_id}/content

代理下载已完成的视频文件。仅在任务状态为 `completed` 时可用。

#### 请求

```
GET /v1/videos/{task_id}/content
Authorization: Bearer <api_key>
```

支持 Bearer Token 认证和 Session 认证（后台面板）。

#### 响应

- 如果已启用 CloudPaste 转存且转存成功：返回 `302` 重定向到转存 URL
- 否则：代理转发上游视频流，`Content-Type: video/mp4`

#### 示例

```bash
curl -L https://<your-host>/v1/videos/task_a1b2c3d4.../content \
  -H "Authorization: Bearer $API_KEY" \
  -o result.mp4
```

> 建议使用 `-L` 跟随重定向。

---

### 3.4 Remix 视频 — POST /v1/videos/{video_id}/remix

基于已有视频生成新的变体视频。会自动锁定到原始任务的渠道，并继承原始任务的计费参数（时长、分辨率）。

#### 请求字段

| 字段 | 类型 | 必填 | 说明 |
|:--|:--|:--|:--|
| `prompt` | string | 是 | 新的视频描述 |

#### 请求示例

```bash
curl https://<your-host>/v1/videos/task_a1b2c3d4.../remix \
  -H "Authorization: Bearer $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"prompt": "将视频风格改为赛博朋克风"}'
```

#### 响应

返回格式与 `POST /v1/videos` 相同，包含新任务的 `id` 和初始状态。

---

## 4. 参数取值表

### seconds（视频时长）

| 值 | 说明 |
|:--|:--|
| `6` | 默认值 |
| `10` | — |
| `12` | — |
| `16` | — |
| `20` | — |

### size（视频尺寸）

| 值 | 比例 |
|:--|:--|
| `720x1280` | 9:16（竖屏，默认） |
| `1280x720` | 16:9（横屏） |
| `1024x1024` | 1:1（方形） |
| `1024x1792` | 9:16（竖屏高清） |
| `1792x1024` | 16:9（横屏高清） |

### resolution_name（分辨率）

| 值 | 说明 |
|:--|:--|
| `480p` | 低分辨率 |
| `720p` | 默认 |

### preset（风格预设）

| 值 | 说明 |
|:--|:--|
| `fun` | 趣味风格 |
| `normal` | 标准风格 |
| `spicy` | 大胆风格 |
| `custom` | 自定义（默认） |

### image_url 格式

参考图的 `image_url` 字段支持两种格式：
- HTTP/HTTPS 链接：`https://example.com/image.jpg`
- Base64 data URI：`data:image/jpeg;base64,/9j/4AAQ...`

同一请求中可混合使用。URL 方式对服务端更友好，base64 适合图片不对外暴露的场景。

---

## 5. 状态流转与轮询策略

### 状态流转

```
queued → in_progress → completed
                    ↘ failed
```

| 状态 | 含义 | 说明 |
|:--|:--|:--|
| `queued` | 已入队 | 任务已创建，等待上游处理 |
| `in_progress` | 生成中 | 视频正在生成，`progress` 反映进度 |
| `completed` | 已完成 | 视频生成成功，`metadata.url` 可用 |
| `failed` | 已失败 | 生成失败，`error` 包含原因，额度自动退还 |

### 轮询策略

- 服务端每 15 秒自动向上游轮询一次任务状态
- 客户端建议轮询间隔：**5-10 秒**（过快无意义）
- 建议设置最大等待时间（如 5-10 分钟），超时后放弃
- 超时未完成的任务会被服务端自动标记为失败并退还额度

---

## 6. 计费规则

计费采用**预扣费 + 失败退款**模式：提交时预扣额度，任务失败或超时自动全额退还。

### 文生视频 / 单图图生视频

按时长分档计费，单图图生视频与文生视频计费完全一致，不加价。

计费公式：`模型单价 × 分组倍率 × 时长秒数`

Grok 视频（Bearer 模式）仅按 `call × seconds` 计费，不叠加分辨率倍率。

### 多图图生视频（≥2 张参考图）

仅上传 ≥2 张参考图时触发额外倍率，具体倍率由上游定价决定。

### 计费时机

| 时机 | 动作 |
|:--|:--|
| 提交任务 | 按 seconds × 模型价格预扣额度 |
| 任务成功 | 保留预扣额度（可能差额结算） |
| 任务失败 | 全额退还预扣额度 |
| 任务超时 | 全额退还预扣额度 |

---

## 7. 错误处理

### HTTP 状态码

| 状态码 | 含义 |
|:--|:--|
| `200` | 成功 |
| `400` | 参数错误 / 任务不存在 |
| `401` | 认证失败 |
| `402` | 余额不足 |
| `429` | 上游负载饱和 |
| `500` | 服务内部错误 |
| `502` | 上游服务异常 |

### 错误响应格式

```json
{
  "error": {
    "message": "描述信息",
    "type": "invalid_request_error",
    "code": "error_code"
  }
}
```

### 常见错误

| 场景 | HTTP | 错误信息 |
|:--|:--|:--|
| prompt 为空 | 400 | `field prompt is required` |
| 任务不存在 | 400 | `task_not_exist` |
| 视频未完成就下载 | 400 | `Task is not completed yet, current status: XXX` |
| 认证失败 | 401 | `authentication_error` |
| 余额不足 | 402 | `payment_required` |
| 上游限流 | 429 | `当前分组上游负载已饱和，请稍后再试` |

---

## 8. 客户端接入示例

### 8.1 cURL

```bash
# 1. 提交任务
curl -X POST https://<your-host>/v1/videos \
  -H "Authorization: Bearer $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "grok-imagine-video",
    "prompt": "a red fox walking through snow",
    "seconds": 6,
    "size": "720x1280"
  }'

# 2. 轮询状态（替换 task_id）
curl https://<your-host>/v1/videos/task_xxx \
  -H "Authorization: Bearer $API_KEY"

# 3. 下载视频
curl -L -o video.mp4 \
  https://<your-host>/v1/videos/task_xxx/content \
  -H "Authorization: Bearer $API_KEY"
```

### 8.2 Python

```python
import requests
import time

BASE_URL = "https://<your-host>"
API_KEY = "<your-api-key>"
HEADERS = {
    "Authorization": f"Bearer {API_KEY}",
    "Content-Type": "application/json",
}

# 1. 提交
resp = requests.post(f"{BASE_URL}/v1/videos", headers=HEADERS, json={
    "model": "grok-imagine-video",
    "prompt": "a red fox walking through snow",
    "seconds": "6",
    "size": "720x1280",
})
task = resp.json()
task_id = task["id"]
print(f"Task created: {task_id}, status: {task['status']}")

# 2. 轮询
while True:
    time.sleep(8)
    resp = requests.get(f"{BASE_URL}/v1/videos/{task_id}", headers=HEADERS)
    data = resp.json()
    status = data.get("status", "unknown")
    progress = data.get("progress", 0)
    print(f"Status: {status}, Progress: {progress}%")

    if status == "completed":
        video_url = (data.get("metadata") or {}).get("url", "")
        print(f"Video URL: {video_url}")
        break
    elif status == "failed":
        print(f"Failed: {data.get('error', {}).get('message', 'unknown')}")
        exit(1)

# 3. 下载（通过代理端点）
video_resp = requests.get(
    f"{BASE_URL}/v1/videos/{task_id}/content",
    headers={"Authorization": f"Bearer {API_KEY}"},
    stream=True, allow_redirects=True,
)
with open("output.mp4", "wb") as f:
    for chunk in video_resp.iter_content(chunk_size=8192):
        f.write(chunk)
print("Video saved to output.mp4")
```

### 8.3 Node.js

```javascript
const fs = require("fs");

const BASE_URL = "https://<your-host>";
const API_KEY = "<your-api-key>";

async function generateVideo() {
  // 1. 提交
  const submitResp = await fetch(`${BASE_URL}/v1/videos`, {
    method: "POST",
    headers: {
      Authorization: `Bearer ${API_KEY}`,
      "Content-Type": "application/json",
    },
    body: JSON.stringify({
      model: "grok-imagine-video",
      prompt: "a red fox walking through snow",
      seconds: "6",
      size: "720x1280",
    }),
  });
  const task = await submitResp.json();
  console.log(`Task created: ${task.id}`);

  // 2. 轮询
  while (true) {
    await new Promise((r) => setTimeout(r, 8000));
    const pollResp = await fetch(`${BASE_URL}/v1/videos/${task.id}`, {
      headers: { Authorization: `Bearer ${API_KEY}` },
    });
    const data = await pollResp.json();
    console.log(`Status: ${data.status}, Progress: ${data.progress}%`);

    if (data.status === "completed") {
      const url = data.metadata?.url || "";
      console.log(`Video URL: ${url}`);
      break;
    }
    if (data.status === "failed") {
      console.error("Failed:", data.error?.message);
      process.exit(1);
    }
  }

  // 3. 下载
  const videoResp = await fetch(
    `${BASE_URL}/v1/videos/${task.id}/content`,
    { headers: { Authorization: `Bearer ${API_KEY}` }, redirect: "follow" }
  );
  const buffer = Buffer.from(await videoResp.arrayBuffer());
  fs.writeFileSync("output.mp4", buffer);
  console.log("Video saved to output.mp4");
}

generateVideo();
```

---

## 9. 注意事项

1. **任务 ID 格式**：new-api 返回的 ID 格式为 `task_xxx`（非上游的 `video_xxx`），客户端不应硬编码 ID 前缀。

2. **视频 URL 位置**：完成后的视频地址在 `metadata.url` 字段中，而非顶层 `url`。

3. **下载需鉴权**：`/v1/videos/{task_id}/content` 端点需要携带 Authorization header。

4. **轮询间隔**：服务端每 15 秒向上游轮询一次，客户端轮询间隔建议 ≥ 5 秒。

5. **单图与多图字段不可混用**：
   - JSON：单图用 `image_reference`，多图用 `image_references`
   - Multipart：单图用 `input_reference`，多图用 `input_references`

6. **image_url 支持 HTTP URL 和 data URI**，同一请求中可混合使用。

7. **超时处理**：建议客户端设置 5-10 分钟最大等待时间。服务端也有超时机制，超时任务自动标记失败并退还额度。

8. **Remix 限制**：Remix 操作会锁定到原始任务的渠道，如果原始渠道被禁用则无法 Remix。

9. **不支持的上游功能**：
   - 同步阻塞模式（`Prefer: wait`）
   - Chat Completions 风格视频生成（`/v1/chat/completions` + 视频模型）
   - 公开无鉴权下载端点（`/v1/files/video`）
