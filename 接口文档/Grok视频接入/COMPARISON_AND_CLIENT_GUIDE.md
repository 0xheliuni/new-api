# 对比汇总与客户端接口分析方案

本文档对比 Grok 上游 API（VIDEO_API.md）与 new-api 项目实际实现的差异，并提供客户端接入方案。

---

## 一、架构对比

| 维度 | Grok 上游（grok2api） | new-api 项目 |
|---|---|---|
| 角色 | 直接视频生成服务 | API 网关/代理层 |
| 任务 ID 格式 | `video_` + 32位 hex | `task_` + 32位随机字符 |
| 轮询方式 | 客户端主动轮询 | 客户端主动轮询 + 服务端后台 15s 自动轮询 |
| 同步模式 | 支持（`Prefer: wait`） | 不支持（网关层无此逻辑） |
| Chat 风格 | 支持（`/v1/chat/completions`） | 不支持（视频模型走 Task 通道） |
| 计费 | 预扣费 + 失败退款 | 预扣费 + 失败退款 + 差额结算 |
| 视频缓存 | 本地磁盘 LRU（1小时 TTL） | 不缓存，代理转发上游 URL |

---

## 二、端点对比

### 2.1 提交端点

| | Grok 上游 | new-api |
|---|---|---|
| URL | `POST /v1/videos` | `POST /v1/videos` |
| 认证 | `Authorization: Bearer <key>` | `Authorization: Bearer <key>` |
| Content-Type | JSON 或 multipart/form-data | JSON（multipart 由中间件处理） |
| 请求体差异 | 见下表 |  |

请求体字段对比：

| 字段 | Grok 上游 | new-api | 说明 |
|---|---|---|---|
| `prompt` | 必填 | 必填 | 一致 |
| `model` | 可选，默认 `grok-video` | 可选 | new-api 通过渠道模型映射处理 |
| `seconds` | int, 枚举 `{6,10,12,16,20,30}` | string 或 int | new-api 透传，不做枚举校验 |
| `size` | string, 5种白名单 | string | new-api 透传，不做白名单校验 |
| `resolution_name` | `480p` / `720p` | 不支持 | new-api 不处理此字段 |
| `preset` | `fun/normal/spicy/custom` | 不支持 | new-api 不处理此字段 |
| `image_reference` | 单张参考图 | `image_reference` | 一致 |
| `image_references` | 多张参考图（最多7张） | `image_references` | 一致 |
| `wait` | bool, 同步模式 | 不支持 | new-api 无同步阻塞模式 |
| `duration` | 不支持 | int | new-api 扩展字段，等同 seconds |
| `metadata` | 不支持 | map | new-api 扩展字段，可传递额外参数 |

响应对比：

| 字段 | Grok 上游 | new-api | 说明 |
|---|---|---|---|
| `id` | `video_xxx` | `task_xxx` | ID 格式不同 |
| `object` | `"video"` | `"video"` | 一致 |
| `status` | `queued` | `queued` | 一致 |
| `progress` | 0-100 | 0-100 | 一致 |
| `created_at` | Unix 秒 | Unix 秒 | 一致 |
| `seconds` | string | string | 一致 |
| `size` | string | string | 一致 |
| `quality` | `"standard"` | string | 一致 |

### 2.2 轮询端点

| | Grok 上游 | new-api |
|---|---|---|
| URL | `GET /v1/videos/{video_id}` | `GET /v1/videos/{task_id}` |
| 认证 | `Authorization: Bearer <key>` | `Authorization: Bearer <key>` |
| 实时性 | 实时查询上游状态 | 依赖后台 15s 轮询缓存（Gemini/Vertex 除外） |

状态映射：

| Grok 上游状态 | new-api 内部状态 | new-api 返回状态 | 进度 |
|---|---|---|---|
| `queued` | `QUEUED` | `queued` | 20% |
| `in_progress` | `IN_PROGRESS` | `in_progress` | 30% |
| `completed` | `SUCCESS` | `completed` | 100% |
| `failed` | `FAILURE` | `failed` | 100% |

completed 响应差异：

| 字段 | Grok 上游 | new-api |
|---|---|---|
| `url` | 顶层 `url` 字段 | `metadata.url` 字段 |
| URL 格式 | `/v1/files/video?id=xxx`（无需鉴权） | `/v1/videos/{task_id}/content`（需鉴权） |

### 2.3 下载端点

| | Grok 上游 | new-api |
|---|---|---|
| 公开端点 | `GET /v1/files/video?id=xxx`（无需鉴权） | 无 |
| 鉴权端点 | `GET /v1/videos/{id}/content` | `GET /v1/videos/{task_id}/content` |
| 认证 | Bearer Token | Bearer Token 或 Session |
| 返回 | 直接返回 mp4 | 代理转发上游 mp4（流式） |

### 2.4 不支持的端点

以下 Grok 上游端点在 new-api 中无对应实现：

| 端点 | 说明 |
|---|---|
| `POST /v1/videos` + `Prefer: wait` | 同步阻塞模式 |
| `GET /v1/files/video` | 公开无鉴权下载 |
| `POST /v1/chat/completions`（视频模型） | Chat 风格视频生成 |

---

## 三、客户端接入方案

### 3.1 方案 A：直接对接 new-api（推荐）

适用于通过 new-api 网关统一管理 API Key 和计费的场景。

#### 第一步：提交视频生成

```bash
curl -X POST https://<new-api-host>/v1/videos \
  -H "Authorization: Bearer <NEW_API_KEY>" \
  -H "Content-Type: application/json" \
  -d '{
    "prompt": "a red fox walking through snow",
    "model": "grok-imagine-video",
    "seconds": "6",
    "size": "720x1280"
  }'
```

响应：
```json
{
  "id": "task_a1b2c3d4e5f6...",
  "object": "video",
  "status": "queued",
  "progress": 0,
  "model": "grok-imagine-video",
  "created_at": 1745380717,
  "seconds": "6",
  "size": "720x1280"
}
```

#### 第二步：轮询状态

```bash
curl https://<new-api-host>/v1/videos/task_a1b2c3d4e5f6... \
  -H "Authorization: Bearer <NEW_API_KEY>"
```

轮询策略：
- 建议间隔：5-10 秒（服务端后台 15 秒轮询一次，客户端过快无意义）
- 检查 `status` 字段：`queued` / `in_progress` → 继续轮询
- `completed` → 提取视频 URL
- `failed` → 读取 `error` 信息

completed 响应：
```json
{
  "id": "task_a1b2c3d4e5f6...",
  "object": "video",
  "status": "completed",
  "progress": 100,
  "completed_at": 1745380999,
  "metadata": {
    "url": "https://<upstream-url>/path/to/video.mp4"
  }
}
```

#### 第三步：下载视频

```bash
# 通过 new-api 代理下载（推荐，统一鉴权）
curl -o video.mp4 \
  https://<new-api-host>/v1/videos/task_a1b2c3d4e5f6.../content \
  -H "Authorization: Bearer <NEW_API_KEY>"
```

#### 完整 Python 示例

```python
import requests
import time

BASE_URL = "https://<new-api-host>"
API_KEY = "<NEW_API_KEY>"
HEADERS = {
    "Authorization": f"Bearer {API_KEY}",
    "Content-Type": "application/json",
}

# 1. 提交
resp = requests.post(f"{BASE_URL}/v1/videos", headers=HEADERS, json={
    "prompt": "a red fox walking through snow",
    "model": "grok-imagine-video",
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
    status_data = resp.json()
    status = status_data.get("status", "unknown")
    progress = status_data.get("progress", 0)
    print(f"Status: {status}, Progress: {progress}%")

    if status == "completed":
        break
    elif status == "failed":
        error = status_data.get("error", {})
        print(f"Failed: {error.get('message', 'unknown error')}")
        exit(1)

# 3. 下载
video_resp = requests.get(
    f"{BASE_URL}/v1/videos/{task_id}/content",
    headers={"Authorization": f"Bearer {API_KEY}"},
    stream=True,
)
with open("output.mp4", "wb") as f:
    for chunk in video_resp.iter_content(chunk_size=8192):
        f.write(chunk)
print("Video saved to output.mp4")
```

#### 完整 Node.js 示例

```javascript
const BASE_URL = "https://<new-api-host>";
const API_KEY = "<NEW_API_KEY>";
const fs = require("fs");

async function generateVideo() {
  // 1. 提交
  const submitResp = await fetch(`${BASE_URL}/v1/videos`, {
    method: "POST",
    headers: {
      Authorization: `Bearer ${API_KEY}`,
      "Content-Type": "application/json",
    },
    body: JSON.stringify({
      prompt: "a red fox walking through snow",
      model: "grok-imagine-video",
      seconds: "6",
      size: "720x1280",
    }),
  });
  const task = await submitResp.json();
  const taskId = task.id;
  console.log(`Task created: ${taskId}`);

  // 2. 轮询
  while (true) {
    await new Promise((r) => setTimeout(r, 8000));
    const pollResp = await fetch(`${BASE_URL}/v1/videos/${taskId}`, {
      headers: { Authorization: `Bearer ${API_KEY}` },
    });
    const data = await pollResp.json();
    console.log(`Status: ${data.status}, Progress: ${data.progress}%`);

    if (data.status === "completed") break;
    if (data.status === "failed") {
      console.error("Failed:", data.error?.message);
      process.exit(1);
    }
  }

  // 3. 下载
  const videoResp = await fetch(`${BASE_URL}/v1/videos/${taskId}/content`, {
    headers: { Authorization: `Bearer ${API_KEY}` },
  });
  const buffer = Buffer.from(await videoResp.arrayBuffer());
  fs.writeFileSync("output.mp4", buffer);
  console.log("Video saved to output.mp4");
}

generateVideo();
```

### 3.2 方案 B：直接对接 Grok 上游

适用于绕过 new-api 网关、直接调用 Grok 上游的场景。

参考 `VIDEO_REQUEST_GUIDE.md` 中的三种方式，此处不再重复。

额外支持的能力：
- 同步阻塞模式（`Prefer: wait`）
- Chat Completions 风格
- 公开无鉴权下载（`/v1/files/video`）

---

## 四、关键差异总结

### 4.1 客户端必须适配的差异

| 差异点 | 影响 | 适配方式 |
|---|---|---|
| ID 格式 | `task_xxx` vs `video_xxx` | 客户端不应硬编码 ID 前缀 |
| 视频 URL 位置 | `metadata.url` vs 顶层 `url` | 优先检查 `metadata.url`，回退到 `url` |
| 下载需鉴权 | new-api 无公开下载端点 | 下载时必须带 Authorization header |
| 轮询延迟 | 后台 15s 轮询，非实时 | 客户端轮询间隔建议 ≥ 8s |
| 无同步模式 | 不支持 `Prefer: wait` | 必须实现异步轮询逻辑 |

### 4.2 兼容性建议

编写同时兼容 Grok 上游和 new-api 的客户端：

```python
def get_video_url(status_data: dict) -> str | None:
    """兼容两种 URL 位置"""
    # new-api: metadata.url
    if "metadata" in status_data and status_data["metadata"]:
        url = status_data["metadata"].get("url")
        if url:
            return url
    # Grok 上游: 顶层 url
    return status_data.get("url")

def get_task_id(submit_response: dict) -> str:
    """兼容两种 ID 格式"""
    return submit_response.get("id", "")

def is_completed(status_data: dict) -> bool:
    """统一状态判断"""
    return status_data.get("status") == "completed"

def is_failed(status_data: dict) -> bool:
    return status_data.get("status") == "failed"
```

### 4.3 错误处理对比

| 场景 | Grok 上游 | new-api |
|---|---|---|
| 参数错误 | `400 {error: {message, type, code, param}}` | `{code, message, statusCode}` |
| 鉴权失败 | `401 {error: {type: "authentication_error"}}` | `401 {error: {type: "authentication_error"}}` |
| 余额不足 | `402 payment_required` | `402 payment_required` |
| 限流 | `429 rate_limit_exceeded` | `429 "当前分组上游负载已饱和，请稍后再试"` |
| 任务不存在 | `400 {error: {param: "video_id"}}` | `{code: "task_not_exist", message: "task_not_exist"}` |
| 视频未就绪 | `409 video_not_ready` | `400 "Task is not completed yet"` |

---

## 五、推荐接入策略

| 场景 | 推荐方案 | 理由 |
|---|---|---|
| 生产环境、多用户 | 方案 A（new-api） | 统一计费、鉴权、负载均衡、自动重试 |
| 快速测试、单用户 | 方案 B（Grok 直连） | 支持同步模式，延迟更低 |
| 已有 OpenAI SDK 集成 | 方案 B + Chat 风格 | 最小改动，复用现有代码 |
| 需要多供应商切换 | 方案 A（new-api） | 网关层统一接口，切换供应商无需改客户端 |
