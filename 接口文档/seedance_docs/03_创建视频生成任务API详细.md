# 创建视频生成任务 API

> **API Endpoint**
>
> `POST https://ark.cn-beijing.volces.com/api/v3/contents/generations/tasks`

模型会依据传入的图片及文本信息生成视频，待生成完成后，您可以按条件查询任务并获取生成的视频。

> **说明**：请确保您的账户余额大于等于 200 元（[前往充值](https://www.volcengine.com)），或已购买资源包，否则无法开通 seedance 2.0 及 seedance 2.0 fast 模型。

---

## 模型能力

### seedance 2.0 & 2.0 fast（有声视频/无声视频）

- **多模态参考生视频**：输入参考图片（0\~9）+ 参考视频（0\~3）+ 参考音频（0\~3）+ 文本提示词（可选）生成 1 个目标视频。注意不可单独输入音频，应至少包含 1 个参考视频或图片。支持生成全新视频、编辑视频、延长视频。
- **图生视频-首尾帧**：输入首帧图片 + 尾帧图片 + 文本提示词（可选）生成 1 个目标视频。
- **图生视频-首帧**：输入首帧图片 + 文本提示词（可选）生成 1 个目标视频。
- **文生视频**：输入文本提示词生成 1 个目标视频。

### seedance 1.5 pro（有声视频/无声视频）

- 图生视频-首尾帧、图生视频-首帧、文生视频

### seedance 1.0 pro

- 图生视频-首尾帧、图生视频-首帧、文生视频

### seedance 1.0 pro fast

- 图生视频-首帧、文生视频

### seedance 1.0 lite

- `doubao-seedance-1-0-lite-t2v`：文生视频
- `doubao-seedance-1-0-lite-i2v`：参考图生视频（1-4 张）+ 文本提示词（可选）、图生视频-首尾帧、图生视频-首帧

---

## 鉴权说明

本接口仅支持 **API Key 鉴权**，请在获取 API Key 页面，获取长效 API Key。

---

## 请求参数

### `model`（string，必选）

您需要调用的模型的 ID（Model ID），开通模型服务，并查询 Model ID。也可通过 Endpoint ID 来调用模型，获得限流、计费类型（前付费/后付费）、运行状态查询、监控、安全等高级能力。

### `content`（object[]，必选）

输入给模型，生成视频的信息，支持文本、图片、音频、视频、样片任务 ID。支持以下组合：

- 文本
- 文本（可选）+ 图片
- 文本（可选）+ 视频
- 文本（可选）+ 图片 + 音频
- 文本（可选）+ 图片 + 视频
- 文本（可选）+ 视频 + 音频
- 文本（可选）+ 图片 + 视频 + 音频
- 样片任务 ID

> **注意**：seedance 2.0 系列模型不支持直接上传含有真人人脸的参考图/视频。

---

#### 文本信息（`content.type = "text"`）

| 字段 | 类型 | 说明 |
|------|------|------|
| `content.type` | string | 输入内容的类型，此处应为 `text` |
| `content.text` | string | 输入给模型的文本提示词，描述期望生成的视频 |

**提示词说明**：

- **语言支持**：所有模型均支持中英文；seedance 2.0 及 2.0 fast 额外支持日语、印尼语、西班牙语、葡萄牙语
- **字数建议**：中文不超过 500 字，英文不超过 1000 词

---

#### 图片信息（`content.type = "image_url"`）

| 字段 | 类型 | 说明 |
|------|------|------|
| `content.type` | string | 输入内容的类型，此处应为 `image_url` |
| `content.image_url.url` | string | 图片 URL、Base64 编码、素材 ID |
| `content.role` | string | 图片的位置或用途（条件必填） |

**图片要求**：

- **格式**：jpeg、png、webp、bmp、tiff、gif（seedance 1.5 pro 额外支持 heic/heif）
- **宽高比**（宽/高）：(0.4, 2.5)
- **宽高长度**（px）：(300, 6000)
- **大小**：单张 < 30 MB，请求体 < 64 MB

**`role` 取值**：

| 值 | 说明 |
|----|------|
| `first_frame` | 首帧图片 |
| `last_frame` | 尾帧图片 |
| `reference_image` | 参考图 |

**图片数量**：

| 场景 | 数量 |
|------|------|
| 图生视频-首帧 | 1 张 |
| 图生视频-首尾帧 | 2 张 |
| seedance 2.0 & 2.0 fast 多模态参考 | 1\~9 张 |
| seedance 1.0 lite 参考图 | 1\~4 张 |

---

#### 视频信息（`content.type = "video_url"`）

> 仅 seedance 2.0 & 2.0 fast 支持输入视频。

| 字段 | 类型 | 说明 |
|------|------|------|
| `content.type` | string | 此处应为 `video_url` |
| `content.video_url.url` | string | 视频 URL、素材 ID |
| `content.role` | string | 当前仅支持 `reference_video` |

**视频要求**：

- **格式**：mp4、mov
- **分辨率**：480p、720p、1080p
- **时长**：单个 [2, 15]s，最多 3 个，总时长 ≤ 15s
- **宽高比**（宽/高）：[0.4, 2.5]
- **宽高像素**：[300, 6000]
- **总像素数**：[409600, 2086876]
- **大小**：单个 ≤ 50 MB
- **帧率**：[24, 60] FPS

---

#### 音频信息（`content.type = "audio_url"`）

> 仅 seedance 2.0 & 2.0 fast 支持输入音频。不可单独输入音频。

| 字段 | 类型 | 说明 |
|------|------|------|
| `content.type` | string | 此处应为 `audio_url` |
| `content.audio_url.url` | string | 音频 URL、Base64 编码、素材 ID |
| `content.role` | string | 当前仅支持 `reference_audio` |

**音频要求**：

- **格式**：wav、mp3
- **时长**：单个 [2, 15]s，最多 3 段，总时长 ≤ 15s
- **大小**：单个 ≤ 15 MB，请求体 ≤ 64 MB

---

#### 样片信息（`content.type = "draft_task"`）

| 字段 | 类型 | 说明 |
|------|------|------|
| `content.type` | string | 此处应为 `draft_task` |
| `content.draft_task.id` | string | 样片任务 ID |

---

### `callback_url`（string，可选）

填写本次生成任务结果的回调通知地址。回调状态：`queued`、`running`、`succeeded`、`failed`、`expired`。

### `return_last_frame`（boolean，默认 `false`）

设为 `true` 时返回生成视频的尾帧图像（png 格式）。可实现连续视频生成：以上一个视频的尾帧作为下一个视频的首帧。

### `service_tier`（string，默认 `"default"`）

| 值 | 说明 |
|----|------|
| `default` | 在线推理模式 |
| `flex` | 离线推理模式，价格为在线推理的 50% |

> seedance 2.0 & 2.0 fast 不支持离线推理。

### `execution_expires_after`（integer，默认 `172800`）

任务超时阈值（秒），范围 `[3600, 259200]`，默认 48 小时。

### `generate_audio`（boolean，默认 `true`）

仅 seedance 2.0 & 2.0 fast、seedance 1.5 pro 支持。

- `true`：输出包含同步音频的视频
- `false`：输出无声视频
- 生成的有声视频均为单声道

### `draft`（boolean，默认 `false`）

仅 seedance 1.5 pro 支持。开启样片模式，消耗 token 更少。开启后使用 480p 分辨率。

### `tools`（object[]，可选）

仅 seedance 2.0 & 2.0 fast 支持。

- `web_search`：联网搜索工具

### `safety_identifier`（string，可选）

终端用户的唯一标识符，长度不超过 64 个字符。

### `resolution`（string）

生成视频的分辨率。

| 模型 | 默认值 |
|------|--------|
| seedance 2.0 & 2.0 fast、seedance 1.5 pro、seedance 1.0 lite | 720p |
| seedance 1.0 pro & pro-fast | 1080p |

枚举值：`480p`、`720p`、`1080p`

### `ratio`（string）

生成视频的宽高比例。

枚举值：`16:9`、`4:3`、`1:1`、`3:4`、`9:16`、`21:9`、`adaptive`

**不同宽高比对应的宽高像素值表**：

| 分辨率 | 宽高比 | seedance 1.0 系列 | seedance 1.5 pro / 2.0 系列 |
|--------|--------|-------------------|----------------------------|
| 480p | 16:9 | 864x480 | 864x496 |
| 480p | 4:3 | 736x544 | 752x560 |
| 480p | 1:1 | 640x640 | 640x640 |
| 480p | 3:4 | 544x736 | 560x752 |
| 480p | 9:16 | 480x864 | 496x864 |
| 480p | 21:9 | 960x416 | 992x432 |
| 720p | 16:9 | 1248x704 | 1280x720 |
| 720p | 4:3 | 1120x832 | 1112x834 |
| 720p | 1:1 | 960x960 | 960x960 |
| 720p | 3:4 | 832x1120 | 834x1112 |
| 720p | 9:16 | 704x1248 | 720x1280 |
| 720p | 21:9 | 1504x640 | 1470x630 |
| 1080p | 16:9 | 1920x1088 | 1920x1080 |
| 1080p | 4:3 | 1664x1248 | 1664x1248 |
| 1080p | 1:1 | 1440x1440 | 1440x1440 |
| 1080p | 3:4 | 1248x1664 | 1248x1664 |
| 1080p | 9:16 | 1088x1920 | 1080x1920 |
| 1080p | 21:9 | 2176x928 | 2206x946 |

### `duration`（integer，默认 `5`）

生成视频时长（秒）：

| 模型 | 取值范围 |
|------|----------|
| seedance 1.0 系列 | [2, 12]s |
| seedance 1.5 pro | [4, 12] 或 -1 |
| seedance 2.0 & 2.0 fast | [4, 15] 或 -1 |

> 设置为 `-1` 表示由模型自主选择合适的时长。

### `frames`（integer，可选）

> seedance 2.0 & 2.0 fast、seedance 1.5 pro 暂不支持。

取值范围：`[29, 289]` 区间内满足 `25 + 4n` 格式的整数值。

### `seed`（integer，默认 `-1`）

种子整数，取值 `[-1, 2^32-1]`。

### `camera_fixed`（boolean，默认 `false`）

是否固定摄像头。参考图场景不支持，seedance 2.0 & 2.0 fast 暂不支持。

### `watermark`（boolean，默认 `false`）

生成视频是否包含水印。

---

## 响应参数

### `id`（string）

视频生成任务 ID。仅保存 7 天。创建后需通过**查询视频生成任务 API** 查询状态。

---

## 代码示例（Python）

```python
import os
import time
from volcenginesdkarkruntime import Ark

client = Ark(
    base_url='https://ark.cn-beijing.volces.com/api/v3',
    api_key=os.environ.get("ARK_API_KEY"),
)

# 创建任务
create_result = client.content_generation.tasks.create(
    model="doubao-seedance-2-0-260128",
    content=[
        {
            "type": "text",
            "text": "你的提示词内容",
        },
        {
            "type": "image_url",
            "image_url": {"url": "https://example.com/image.jpg"},
            "role": "reference_image",
        },
        {
            "type": "video_url",
            "video_url": {"url": "https://example.com/video.mp4"},
            "role": "reference_video",
        },
        {
            "type": "audio_url",
            "audio_url": {"url": "https://example.com/audio.mp3"},
            "role": "reference_audio",
        },
    ],
    generate_audio=True,
    ratio="16:9",
    duration=11,
    watermark=True,
)
task_id = create_result.id

# 轮询查询
while True:
    get_result = client.content_generation.tasks.get(task_id=task_id)
    status = get_result.status
    if status == "succeeded":
        print(get_result)
        break
    elif status == "failed":
        print(f"Error: {get_result.error}")
        break
    else:
        print(f"Current status: {status}")
        time.sleep(30)
```

---

## 响应示例

```json
{
  "id": "cgt-20260414114820-*****",
  "model": "doubao-seedance-2-0-260128",
  "status": "succeeded",
  "content": {
    "video_url": "https://...",
    "last_frame_url": null,
    "file_url": null
  },
  "usage": {
    "completion_tokens": 411300,
    "total_tokens": 411300
  },
  "framespersecond": 24,
  "seed": 33608,
  "service_tier": "default",
  "execution_expires_after": 172800,
  "generate_audio": true,
  "duration": 11,
  "ratio": "16:9",
  "resolution": "720p",
  "draft": false
}
```
