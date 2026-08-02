# 视频生成 API 参考

> 来源：https://www.volcengine.com/docs/82379/1520758?lang=zh

## 1. 创建视频生成任务 API

`POST https://ark.cn-beijing.volces.com/api/v3/contents/generations/tasks`

本接口用于创建视频生成任务。模型会依据传入的图片及文本信息生成视频。

> 请确保您的账户余额>=200元，或已购买资源包，否则无法开通 seedance 2.0 及 2.0 fast。

### 鉴权

本接口仅支持 API Key 鉴权。

### 模型能力

**seedance 2.0 & 2.0 fast**（有声/无声视频）：

- 多模态参考生视频：参考图片(0~9)+参考视频(0~3)+参考音频(0~3)+文本提示词(可选)
- 图生视频-首尾帧/首帧
- 文生视频

**seedance 1.5 pro**（有声/无声视频）：图生视频-首尾帧/首帧、文生视频

**seedance 1.0 pro**：图生视频-首尾帧/首帧、文生视频

**seedance 1.0 pro fast**：图生视频-首帧、文生视频

**seedance 1.0 lite**：

- t2v：文生视频
- i2v：参考图生视频(1-4张)、图生视频-首尾帧/首帧

### 请求参数

#### model (string, 必选)

模型 ID 或 Endpoint ID。

#### content (object[], 必选)

输入给模型的信息，支持文本、图片、音频、视频、样片任务ID。

**文本信息** (type="text")：

- text: 文本提示词。中文<=500字，英文<=1000词。

**图片信息** (type="image_url")：

- image_url.url: 图片URL / Base64编码 / 素材ID(`asset://<ASSET_ID>`)
- role: `first_frame`(首帧) / `last_frame`(尾帧) / `reference_image`(参考图)
- 格式：jpeg/png/webp/bmp/tiff/gif，宽高比(0.4, 2.5)，宽高(300, 6000)px，<30MB

**视频信息** (type="video_url")，仅2.0系列：

- video_url.url: 视频URL / 素材ID
- role: `reference_video`
- 格式：mp4/mov，分辨率480p/720p/1080p，单个[2, 15]s，最多3个，总时长<=15s，<50MB

**音频信息** (type="audio_url")，仅2.0系列：

- audio_url.url: 音频URL / Base64 / 素材ID
- role: `reference_audio`
- 格式：wav/mp3，单个[2, 15]s，最多3段，总时长<=15s，<15MB

**样片信息** (type="draft_task")，仅1.5 pro：

- draft_task.id: 样片任务ID

#### 其他参数

| 参数 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| callback_url | string | - | 回调通知地址（状态变化时推送POST） |
| return_last_frame | bool | false | 是否返回视频尾帧图像(png) |
| service_tier | string | "default" | `default`在线推理 / `flex`离线推理（50%价格，2.0不支持） |
| execution_expires_after | int | 172800 | 任务超时（秒），[3600, 259200] |
| generate_audio | bool | true | 是否含同步声音（仅2.0/1.5pro） |
| draft | bool | false | 是否开启样片模式（仅1.5pro） |
| tools | object[] | - | 工具配置，如`web_search`（仅2.0） |
| safety_identifier | string | - | 终端用户标识（<=64字符） |
| resolution | string | 720p/1080p | 视频分辨率: 480p/720p/1080p |
| ratio | string | adaptive/16:9 | 宽高比: 16:9/4:3/1:1/3:4/9:16/21:9/adaptive |
| duration | int | 5 | 视频时长（秒）: 1.0系列[2, 12]，1.5pro[4, 12]/-1，2.0[4, 15]/-1 |
| frames | int | - | 帧数（2.0/1.5pro不支持），[29, 289]内满足25+4n |
| seed | int | -1 | 种子[-1, 2^32-1] |
| camera_fixed | bool | false | 固定摄像头（参考图/2.0不支持） |
| watermark | bool | false | 是否含水印 |

### 响应参数

- id (string): 视频生成任务ID，保存7天

### Python 代码示例

```python
import os, time
from volcenginesdkarkruntime import Ark

client = Ark(
    base_url='https://ark.cn-beijing.volces.com/api/v3',
    api_key=os.environ.get("ARK_API_KEY"),
)

create_result = client.content_generation.tasks.create(
    model="doubao-seedance-2-0-260128",
    content=[
        {"type": "text", "text": "提示词内容"},
        {"type": "image_url", "image_url": {"url": "https://example.com/img.jpg"}, "role": "reference_image"},
    ],
    generate_audio=True,
    ratio="16:9",
    duration=11,
)

task_id = create_result.id
while True:
    result = client.content_generation.tasks.get(task_id=task_id)
    if result.status == "succeeded":
        print(result)
        break
    elif result.status == "failed":
        print(f"Error: {result.error}")
        break
    time.sleep(30)
```

---

## 2. 查询视频生成任务 API

`GET https://ark.cn-beijing.volces.com/api/v3/contents/generations/tasks/{id}`

查询视频生成任务的状态。仅支持查询最近7天历史数据。

### 请求参数

- id (string, 必选): 任务ID（URL路径参数）

### 响应参数

| 参数 | 类型 | 说明 |
|------|------|------|
| id | string | 任务ID |
| model | string | 模型名称-版本 |
| status | string | queued/running/cancelled/succeeded/failed/expired |
| error | object/null | 错误信息(code+message) |
| created_at | int | 创建时间Unix时间戳（秒） |
| updated_at | int | 更新时间Unix时间戳（秒） |
| content.video_url | string | 视频URL（mp4，24h后清理） |
| content.last_frame_url | string | 尾帧图像URL（24h有效） |
| seed | int | 种子值 |
| resolution | string | 分辨率 |
| ratio | string | 宽高比 |
| duration | int | 时长（秒） |
| frames | int | 帧数（与duration二选一返回） |
| framespersecond | int | 帧率 |
| generate_audio | bool | 是否含音频（仅2.0/1.5pro） |
| tools | object[] | 实际使用的工具 |
| safety_identifier | string | 终端用户标识 |
| draft | bool | 是否Draft视频（仅1.5pro） |
| draft_task_id | string | Draft任务ID |
| service_tier | string | 服务等级 |
| execution_expires_after | int | 超时阈值（秒） |
| usage.completion_tokens | int | 输出token数 |
| usage.total_tokens | int | 总token数（=completion_tokens） |
| usage.tool_usage.web_search | int | 联网搜索次数 |

### curl 示例

```bash
curl -X GET https://ark.cn-beijing.volces.com/api/v3/contents/generations/tasks/cgt-2025**** \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $ARK_API_KEY"
```

### 响应示例

```json
{
  "id": "cgt-2025******-****",
  "model": "doubao-seedance-1-5-pro-251215",
  "status": "succeeded",
  "content": {"video_url": "https://..."},
  "usage": {"completion_tokens": 108900, "total_tokens": 108900},
  "created_at": 1743414619,
  "updated_at": 1743414673,
  "seed": 10,
  "resolution": "720p",
  "ratio": "16:9",
  "duration": 5,
  "framespersecond": 24,
  "service_tier": "default",
  "execution_expires_after": 172800,
  "generate_audio": true,
  "draft": false
}
```

---

## 3. 查询视频生成任务列表

`GET https://ark.cn-beijing.volces.com/api/v3/contents/generations/tasks?page_num={}&page_size={}&filter.status={}&filter.task_ids={}&filter.model={}`

批量查询符合条件的任务。仅支持查询最近7天历史数据。

### 请求参数（Query String）

| 参数 | 类型 | 说明 |
|------|------|------|
| page_num | int/null | 页码[1, 500] |
| page_size | int/null | 每页数量[1, 500] |
| filter.status | string/null | 任务状态筛选 |
| filter.task_ids | string[]/null | 任务ID精确搜索（多个用&连接） |
| filter.model | string/null | 推理接入点ID精确搜索 |
| filter.service_tier | string/null | 服务等级（default/flex） |

### 响应参数

- items (object[]): 任务列表（每项结构同查询单个任务）
- total (int): 符合条件的任务总数

### curl 示例

```bash
curl -X GET "https://ark.cn-beijing.volces.com/api/v3/contents/generations/tasks?page_size=3&filter.status=succeeded" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $ARK_API_KEY"
```

---

## 4. 取消或删除视频生成任务

`DELETE https://ark.cn-beijing.volces.com/api/v3/contents/generations/tasks/{id}`

取消排队中的任务，或删除已完成/失败/超时的任务记录。

### 请求参数

- id (string, 必选): 任务ID（URL路径参数）

### 操作规则

| 当前状态 | 支持DELETE | 操作含义 | 操作后状态 |
|---------|-----------|---------|-----------|
| queued | 是 | 取消排队 | cancelled |
| running | 否 | - | - |
| succeeded | 是 | 删除记录 | - |
| failed | 是 | 删除记录 | - |
| cancelled | 否 | - | - |
| expired | 是 | 删除记录 | - |

### curl 示例

```bash
curl -X DELETE https://ark.cn-beijing.volces.com/api/v3/contents/generations/tasks/$ID \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $ARK_API_KEY"
```

响应: `{}`
