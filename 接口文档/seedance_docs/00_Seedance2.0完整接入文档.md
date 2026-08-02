# Seedance 2.0 完整接入文档

> 火山方舟 - Seedance 视频生成平台 API 接入指南
>
> 本文档汇总了 Seedance 2.0 的使用教程、API 接口、素材库管理等全部接入信息，帮助开发者快速完成集成。

---

## 目录

- [一、概述与准备工作](#一概述与准备工作)
- [二、模型选型](#二模型选型)
- [三、快速开始：5 分钟跑通第一个视频](#三快速开始5-分钟跑通第一个视频)
- [四、视频生成 API 接口](#四视频生成-api-接口)
- [五、提示词指南](#五提示词指南)
- [六、私域虚拟人像素材库](#六私域虚拟人像素材库)
- [七、私域真人人像素材库](#七私域真人人像素材库)
- [八、素材库 API 接口参考](#八素材库-api-接口参考)
- [九、计费说明](#九计费说明)
- [十、常见问题与注意事项](#十常见问题与注意事项)

---

## 一、概述与准备工作

### 1.1 产品简介

Seedance 2.0 系列模型支持图像、视频、音频、文本等多种模态内容输入，具备以下核心能力：

- **视频生成**：文生视频、图生视频（首帧/首尾帧）、多模态参考生视频
- **视频编辑**：元素增删改、视频延长、轨道补齐
- **有声视频**：原生支持音频与视频联合生成
- **联网搜索**：基于提示词自动搜索互联网内容提升时效性

### 1.2 接入准备

| 步骤 | 说明 |
|------|------|
| 1. 注册账号 | 注册火山引擎账号并登录 |
| 2. 账户充值 | 确保账户余额 >= 200 元，否则无法开通 Seedance 2.0 |
| 3. 获取 API Key | 访问 [API Key 管理页面](https://console.volcengine.com/ark/region:ark+cn-beijing/apikey) 创建并保存 API Key |
| 4. 开通模型 | 在控制台开通所需的 Seedance 模型 |
| 5. 安装 SDK | `pip install 'volcengine-python-sdk[ark]'` |

### 1.3 基础信息

| 项目 | 值 |
|------|-----|
| Base URL | `https://ark.cn-beijing.volces.com/api/v3` |
| 鉴权方式 | API Key（视频生成）/ AK/SK（素材库） |
| SDK 包名 | `volcenginesdkarkruntime`（Python） |
| 视频生成接口 | 异步接口，需轮询查询结果 |
| 任务数据保留 | 7 天 |
| 生成视频保留 | 24 小时（请及时转存） |

---

## 二、模型选型

### 2.1 模型能力矩阵

| 能力 | 2.0 | 2.0 fast | 1.5 pro | 1.0 pro | 1.0 pro fast | 1.0 lite |
|------|-----|----------|---------|---------|--------------|----------|
| 文生视频 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| 图生视频-首帧 | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| 图生视频-首尾帧 | ✅ | ✅ | ✅ | ✅ | ❌ | ✅(i2v) |
| 多模态参考(图+视频+音频) | ✅(9图3视频3音频) | ✅ | ❌ | ❌ | ❌ | ❌ |
| 参考图生视频 | ✅ | ✅ | ❌ | ❌ | ❌ | ✅(1-4张) |
| 有声视频 | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ |
| 视频编辑/延长 | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ |
| 联网搜索 | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ |
| Draft 样片 | ❌ | ❌ | ✅ | ❌ | ❌ | ❌ |
| 离线推理(flex) | ❌ | ❌ | ✅ | ✅ | ✅ | ✅ |

### 2.2 Model ID

| 模型 | Model ID |
|------|----------|
| Seedance 2.0 | `doubao-seedance-2-0-260128` |
| Seedance 2.0 fast | `doubao-seedance-2-0-fast-250901` |
| Seedance 1.5 pro | `doubao-seedance-1-5-pro-251215` |
| Seedance 1.0 pro | `doubao-seedance-1-0-pro-250528` |
| Seedance 1.0 pro fast | `doubao-seedance-1-0-pro-fast-250528` |
| Seedance 1.0 lite t2v | `doubao-seedance-1-0-lite-t2v` |
| Seedance 1.0 lite i2v | `doubao-seedance-1-0-lite-i2v` |

### 2.3 选型建议

- **追求最高品质** → Seedance 2.0
- **平衡成本与速度** → Seedance 2.0 fast
- **需要 Draft 预览降低成本** → Seedance 1.5 pro
- **需要离线推理(半价)** → Seedance 1.5 pro / 1.0 系列
- **最低成本** → Seedance 1.0 pro fast

### 2.4 输出视频规格

| 参数 | Seedance 2.0 / 2.0 fast | Seedance 1.5 pro | Seedance 1.0 系列 |
|------|-------------------------|------------------|------------------|
| 分辨率 | 480p/720p (默认720p) | 480p/720p/1080p (默认720p) | 480p/720p/1080p (默认1080p) |
| 宽高比 | 16:9/4:3/1:1/3:4/9:16/21:9/adaptive | 同左 | 同左 |
| 时长 | 4~15秒 或 -1(自动) | 4~12秒 或 -1 | 2~12秒 |
| 帧率 | 24 FPS | 24 FPS | 24 FPS |

> **注意**：Seedance 2.0 / 2.0 fast **不支持 1080p 输出**。

---

## 三、快速开始：5 分钟跑通第一个视频

### 3.1 环境准备

```bash
# 安装 SDK
pip install 'volcengine-python-sdk[ark]'

# 设置 API Key 环境变量
export ARK_API_KEY="your-api-key-here"
```

### 3.2 文生视频 - 最简示例

```python
import os, time
from volcenginesdkarkruntime import Ark

client = Ark(
    base_url='https://ark.cn-beijing.volces.com/api/v3',
    api_key=os.environ.get("ARK_API_KEY"),
)

# Step 1: 创建视频生成任务
task = client.content_generation.tasks.create(
    model="doubao-seedance-2-0-260128",
    content=[
        {"type": "text", "text": "一只金色柴犬在樱花树下奔跑，花瓣随风飘落，电影质感，暖色调"},
    ],
    generate_audio=True,   # 生成有声视频
    ratio="16:9",          # 宽高比
    duration=5,            # 时长 5 秒
    resolution="720p",     # 分辨率
)
print(f"任务已创建: {task.id}")

# Step 2: 轮询查询结果
while True:
    result = client.content_generation.tasks.get(task_id=task.id)
    if result.status == "succeeded":
        print(f"生成成功! 视频URL: {result.content.video_url}")
        print(f"消耗 tokens: {result.usage.total_tokens}")
        break
    elif result.status == "failed":
        print(f"生成失败: {result.error}")
        break
    else:
        print(f"状态: {result.status}，等待 30 秒...")
        time.sleep(30)
```

### 3.3 图生视频 - 首帧驱动

```python
task = client.content_generation.tasks.create(
    model="doubao-seedance-2-0-260128",
    content=[
        {"type": "text", "text": "女孩微笑着转头看向镜头，头发随风飘动"},
        {
            "type": "image_url",
            "image_url": {"url": "https://your-domain.com/girl.jpg"},
            "role": "first_frame",  # 指定为首帧
        },
    ],
    generate_audio=True,
    ratio="adaptive",  # 自动匹配图片比例
    duration=5,
)
```

### 3.4 多模态参考生视频 (Seedance 2.0 专属)

```python
task = client.content_generation.tasks.create(
    model="doubao-seedance-2-0-260128",
    content=[
        {
            "type": "text",
            "text": "参考视频1的运镜和动作，图片1中的女孩穿着图片2的服装在咖啡店里微笑，配合音频1的背景音乐",
        },
        # 参考图片 (最多9张)
        {"type": "image_url", "image_url": {"url": "https://example.com/girl.jpg"}, "role": "reference_image"},
        {"type": "image_url", "image_url": {"url": "https://example.com/dress.jpg"}, "role": "reference_image"},
        # 参考视频 (最多3个，总时长<=15s)
        {"type": "video_url", "video_url": {"url": "https://example.com/motion.mp4"}, "role": "reference_video"},
        # 参考音频 (最多3段，总时长<=15s)
        {"type": "audio_url", "audio_url": {"url": "https://example.com/bgm.mp3"}, "role": "reference_audio"},
    ],
    generate_audio=True,
    ratio="16:9",
    duration=11,
)
```

### 3.5 使用回调替代轮询

```python
task = client.content_generation.tasks.create(
    model="doubao-seedance-2-0-260128",
    content=[{"type": "text", "text": "日落时分的海滩，海浪拍打沙滩"}],
    callback_url="https://your-server.com/webhook/seedance",  # 回调地址
    duration=5,
)
# 不需要轮询，任务完成后平台会向 callback_url 推送 POST 请求
# 回调请求体结构与查询任务API的返回体一致
```

### 3.6 连续视频生成（尾帧接力）

```python
# 第一段视频：返回尾帧
task1 = client.content_generation.tasks.create(
    model="doubao-seedance-2-0-260128",
    content=[{"type": "text", "text": "女孩走进咖啡店"}],
    return_last_frame=True,  # 返回尾帧
    duration=5,
)
# ... 等待 task1 完成 ...
result1 = client.content_generation.tasks.get(task_id=task1.id)
last_frame_url = result1.content.last_frame_url

# 第二段视频：用上一段的尾帧作为首帧
task2 = client.content_generation.tasks.create(
    model="doubao-seedance-2-0-260128",
    content=[
        {"type": "text", "text": "女孩坐下来点了一杯咖啡"},
        {"type": "image_url", "image_url": {"url": last_frame_url}, "role": "first_frame"},
    ],
    duration=5,
)
```

---

## 四、视频生成 API 接口

### 4.1 创建视频生成任务

```
POST https://ark.cn-beijing.volces.com/api/v3/contents/generations/tasks
```

**鉴权**：API Key（Header: `Authorization: Bearer <API_KEY>`）

#### 请求体参数

| 参数 | 类型 | 必选 | 默认值 | 说明 |
|------|------|------|--------|------|
| model | string | 是 | - | Model ID 或 Endpoint ID |
| content | object[] | 是 | - | 输入内容数组（文本/图片/视频/音频/样片） |
| callback_url | string | 否 | - | 结果回调地址 |
| return_last_frame | bool | 否 | false | 是否返回尾帧图像 |
| service_tier | string | 否 | "default" | `default`(在线) / `flex`(离线,半价) |
| execution_expires_after | int | 否 | 172800 | 任务超时(秒) [3600, 259200] |
| generate_audio | bool | 否 | true | 是否生成有声视频(仅2.0/1.5pro) |
| draft | bool | 否 | false | 样片模式(仅1.5pro) |
| tools | object[] | 否 | - | 工具配置 `[{"type":"web_search"}]`(仅2.0) |
| resolution | string | 否 | "720p" | 分辨率: 480p/720p/1080p |
| ratio | string | 否 | "adaptive" | 宽高比: 16:9/4:3/1:1/3:4/9:16/21:9/adaptive |
| duration | int | 否 | 5 | 时长(秒)，-1 表示自动 |
| seed | int | 否 | -1 | 随机种子 [-1, 2^32-1] |
| watermark | bool | 否 | false | 是否含水印 |
| safety_identifier | string | 否 | - | 终端用户标识(<=64字符) |

#### content 数组元素类型

**文本** (`type: "text"`)：

```json
{"type": "text", "text": "提示词内容"}
```

**图片** (`type: "image_url"`)：

```json
{
  "type": "image_url",
  "image_url": {"url": "图片URL / data:image/png;base64,xxx / asset://asset_id"},
  "role": "first_frame | last_frame | reference_image"
}
```

| 限制项 | 要求 |
|--------|------|
| 格式 | jpeg/png/webp/bmp/tiff/gif (1.5pro额外支持heic/heif) |
| 宽高比(宽/高) | (0.4, 2.5) |
| 宽高(px) | (300, 6000) |
| 大小 | 单张 < 30MB，请求体 < 64MB |

**视频** (`type: "video_url"`)，仅 2.0 系列：

```json
{
  "type": "video_url",
  "video_url": {"url": "视频URL / asset://asset_id"},
  "role": "reference_video"
}
```

| 限制项 | 要求 |
|--------|------|
| 格式 | mp4/mov |
| 时长 | 单个[2,15]s，最多3个，总时长<=15s |
| 分辨率 | 480p/720p/1080p |
| 大小 | 单个 <= 50MB |
| 帧率 | [24, 60] FPS |

**音频** (`type: "audio_url"`)，仅 2.0 系列：

```json
{
  "type": "audio_url",
  "audio_url": {"url": "音频URL / data:audio/wav;base64,xxx / asset://asset_id"},
  "role": "reference_audio"
}
```

| 限制项 | 要求 |
|--------|------|
| 格式 | wav/mp3 |
| 时长 | 单个[2,15]s，最多3段，总时长<=15s |
| 大小 | 单个 <= 15MB |

> **重要**：不可单独输入音频，必须同时包含至少 1 个参考视频或图片。

#### 响应

```json
{"id": "cgt-20260414114820-xxxxx"}
```

返回任务 ID，需通过查询接口获取结果。

---

### 4.2 查询视频生成任务

```
GET https://ark.cn-beijing.volces.com/api/v3/contents/generations/tasks/{id}
```

仅支持查询最近 **7 天**历史数据。

#### 响应参数

| 参数 | 类型 | 说明 |
|------|------|------|
| id | string | 任务 ID |
| model | string | 模型名称-版本 |
| status | string | `queued` / `running` / `succeeded` / `failed` / `cancelled` / `expired` |
| error | object/null | 失败时返回 `{code, message}` |
| content.video_url | string | 视频 URL (mp4，**24小时后清理**) |
| content.last_frame_url | string | 尾帧图像 URL (24小时有效) |
| usage.completion_tokens | int | 输出 token 数（计费依据） |
| usage.total_tokens | int | 总 token 数（= completion_tokens） |
| seed | int | 实际使用的种子值 |
| resolution | string | 实际分辨率 |
| ratio | string | 实际宽高比 |
| duration | int | 实际时长(秒) |
| framespersecond | int | 帧率 |
| generate_audio | bool | 是否含音频 |
| service_tier | string | 服务等级 |

#### 响应示例

```json
{
  "id": "cgt-20260414114820-xxxxx",
  "model": "doubao-seedance-2-0-260128",
  "status": "succeeded",
  "content": {
    "video_url": "https://ark-content-generation-cn-beijing.tos-cn-beijing.volces.com/xxx",
    "last_frame_url": null
  },
  "usage": {"completion_tokens": 411300, "total_tokens": 411300},
  "seed": 33608,
  "resolution": "720p",
  "ratio": "16:9",
  "duration": 11,
  "framespersecond": 24,
  "generate_audio": true,
  "service_tier": "default"
}
```

---

### 4.3 查询视频生成任务列表

```
GET https://ark.cn-beijing.volces.com/api/v3/contents/generations/tasks?page_num=1&page_size=10&filter.status=succeeded
```

| Query 参数 | 说明 |
|------------|------|
| page_num | 页码 [1, 500] |
| page_size | 每页数量 [1, 500] |
| filter.status | 按状态筛选 |
| filter.task_ids | 按任务ID精确搜索（多个用 & 连接） |
| filter.model | 按接入点ID精确搜索 |
| filter.service_tier | 按服务等级筛选 |

响应包含 `items`(任务数组) 和 `total`(总数)。

---

### 4.4 取消或删除视频生成任务

```
DELETE https://ark.cn-beijing.volces.com/api/v3/contents/generations/tasks/{id}
```

| 当前状态 | 可否 DELETE | 效果 |
|---------|------------|------|
| queued | 是 | 取消排队 → cancelled |
| running | 否 | - |
| succeeded | 是 | 删除记录 |
| failed | 是 | 删除记录 |
| cancelled | 否 | - |
| expired | 是 | 删除记录 |

---

## 五、提示词指南

### 5.1 基础公式

```
「主体 + 动作」 + 「环境 + 风格」 + 「进阶指令（镜头/声效）」
```

### 5.2 多模态素材指代规则

在提示词中使用 **"图片N"、"视频N"、"音频N"** 指代，N 为该类素材在请求体中的排序序号。

```
# 正确写法
"图片1中的女孩穿着图片2的服装，参考视频1的动作"

# 错误写法（不要使用 Asset ID）
"asset://asset-xxx 中的女孩..."
```

### 5.3 文字生成模板

**广告语**：`「文字内容」+「出现时机」+「出现位置」+「出现方式」，「文字特征」`

**字幕**：`画面底部出现字幕，字幕内容为"..."，字幕需与音频节奏完全同步。`

**气泡台词**：`「角色」说："..."，角色说话时周围出现气泡，气泡里写着台词。`

### 5.4 参考模板

| 场景 | 提示词模板 |
|------|-----------|
| 主体参考 | `参考图片N中的「主体」，生成「画面描述」，保持「主体」特征一致。` |
| Logo参考 | `画面出现图片N的Logo，「描述」` |
| 动作参考 | `参考视频N的「动作描述」，生成「画面描述」，保持动作细节一致。` |
| 运镜参考 | `参考视频N的「运镜描述」，生成「画面描述」，保持运镜一致。` |
| 特效参考 | `参考视频N的「特效描述」，生成「画面描述」，保持特效一致。` |

### 5.5 视频编辑模板 (2.0 专属)

| 操作 | 提示词模板 |
|------|-----------|
| 增加元素 | `在视频1的「位置」增加「元素描述」` |
| 删除元素 | `删除视频1中的「元素」，视频其他内容保持不变` |
| 修改元素 | `将视频1中的「原元素」替换为「新元素」，动作和运镜不变` |
| 向后延长 | `生成视频1之后的内容，「后续描述」` |
| 向前延长 | `向前延长视频1，「前序描述」` |
| 轨道补齐 | `视频1，「过渡描述」，接视频2`（最多3段，总时长<=15s） |

### 5.6 提示词注意事项

- 中文提示词 <= 500 字，英文 <= 1000 词
- 2.0 / 2.0 fast 支持：中文、英文、日语、印尼语、西班牙语、葡萄牙语
- 对话部分置于双引号内，优化音频生成：`男人说："你好"`
- 优先使用常用字，避免生僻字与特殊符号

---

## 六、私域虚拟人像素材库

> 仅限邀测用户。用于管理 AI 生成的虚拟人像素材。

### 6.1 概述

Seedance 2.0 系列模型**不支持直接上传含真人人脸**的参考图/视频。需通过私域素材库入库后使用。

### 6.2 使用流程

```
创建素材组(CreateAssetGroup)
    ↓
上传素材(CreateAsset) → 系统处理(Processing)
    ↓
轮询查询状态(GetAsset) → Active(可用) / Failed(失败)
    ↓
使用 asset://<asset_ID> 生成视频
```

### 6.3 操作步骤

#### Step 1: 创建素材组

```python
# 需使用 AK/SK 鉴权（非 API Key）
from volcengine.volcengine_go_sdk import universal

resp = call_ark_api(
    action="CreateAssetGroup",
    params={
        "Name": "my_avatar_group",
        "Description": "虚拟人像素材组",
        "GroupType": "AIGC",           # 虚拟人像固定值
        "ProjectName": "default",      # 项目名称
    }
)
# 返回: {"Id": "group-20260318033332-xxxxx"}
```

#### Step 2: 上传素材

```python
resp = call_ark_api(
    action="CreateAsset",
    params={
        "GroupId": "group-20260318033332-xxxxx",
        "URL": "https://your-domain.com/avatar.jpg",
        "AssetType": "Image",    # Image / Video / Audio
        "ProjectName": "default",
    }
)
# 返回: {"Id": "asset-20260318071009-xxxxx"}
```

> CreateAsset 是**异步接口**，需轮询 GetAsset 查询状态直到 `Active`。

#### Step 3: 查询素材状态

```python
resp = call_ark_api(
    action="GetAsset",
    params={
        "Id": "asset-20260318071009-xxxxx",
        "ProjectName": "default",
    }
)
# Status: "Processing" → "Active"(可用) / "Failed"(失败)
# Active 后的 URL 有效期 12 小时
```

#### Step 4: 使用素材生成视频

```python
task = client.content_generation.tasks.create(
    model="doubao-seedance-2-0-260128",
    content=[
        {"type": "text", "text": "图片1中的虚拟人像在直播间介绍产品"},
        {
            "type": "image_url",
            "image_url": {"url": "asset://asset-20260318071009-xxxxx"},  # 素材URI
            "role": "reference_image",
        },
    ],
    generate_audio=True,
    duration=11,
)
```

### 6.4 人像素材最佳实践

| 类型 | 要求 |
|------|------|
| 全身参考图 | 竖版图片，人物全身正面 |
| 人脸特写图 | 竖版图片，正面无表情特写，肩部以上，面部占画面 2/3 |

---

## 七、私域真人人像素材库

> 仅限邀测用户。需通过 H5 真人认证锁定肖像权。

### 7.1 与虚拟人像库的区别

| 维度 | 虚拟人像 (AIGC) | 真人人像 (LivenessFace) |
|------|----------------|----------------------|
| GroupType | `AIGC` | `LivenessFace` |
| 创建 Group 方式 | `CreateAssetGroup` | `CreateVisualValidateSession` + H5真人认证 |
| 人脸验证 | 无 | 自动面部一致性比对 |
| 肖像权 | 需自行确保 | H5 真人认证锁定 |
| 多人脸素材 | 支持 | 不支持 |
| 鉴权要求 | AK/SK | AK/SK + ArkFullAccess 权限 |

### 7.2 使用流程

```
拉起H5认证页(CreateVisualValidateSession)
    ↓
终端用户完成真人认证 → 跳转 CallbackURL
    ↓
解析回调参数(resultCode=10000为成功)
    ↓
获取Group ID(GetVisualValidateResult)
    ↓
上传素材(CreateAsset) → 系统面部比对
    ↓
Active 后使用 asset://<ID> 生成视频
```

### 7.3 操作步骤

#### Step 1: 拉起 H5 真人认证页

```python
resp = call_ark_api(
    action="CreateVisualValidateSession",
    params={
        "CallbackURL": "https://your-app.com/callback",
        "ProjectName": "default",
    }
)
# 返回:
# {
#   "BytedToken": "202603311449168C23BA26xxxxx",
#   "H5Link": "https://h5-v2.kych5.com?...",     # 有效期 120 秒
#   "CallbackURL": "https://your-app.com/callback"
# }
```

> H5Link 末尾 `lng` 参数可指定语言：`zh`(简中) / `en`(英文) / `zh-Hant`(繁中)

#### Step 2: 解析回调结果

用户完成认证后，浏览器跳转到：

```
https://your-app.com/callback?bytedToken=xxx&resultCode=10000&algorithmBaseRespCode=0&verify_type=real_time
```

- `resultCode=10000`：认证成功
- `bytedToken`：用于下一步获取 Group ID

#### Step 3: 获取 Asset Group ID

```python
resp = call_ark_api(
    action="GetVisualValidateResult",
    params={
        "BytedToken": "202603311449168C23BA26xxxxx",  # 有效期 120 秒
        "ProjectName": "default",
    }
)
# 返回: {"GroupId": "group-20260331145705-xxxxx"}
```

#### Step 4: 上传素材

与虚拟人像库的 `CreateAsset` 相同，但系统会额外进行**面部一致性比对**：
- 非同一人物 → 入库失败
- 多人脸素材 → 入库失败

#### Step 5: 生成视频

与虚拟人像库用法一致，使用 `asset://<asset_ID>` 格式。

---

## 八、素材库 API 接口参考

> 所有素材库接口均使用 **AK/SK 鉴权**（非 API Key），服务名: `ark`，版本: `2024-01-01`

### 8.1 接口总览

| 接口 | 方法 | 说明 | 适用范围 |
|------|------|------|---------|
| CreateAssetGroup | POST | 创建素材组 | 虚拟人像 |
| CreateVisualValidateSession | POST | 拉起H5真人认证 | 真人人像 |
| GetVisualValidateResult | POST | 获取认证结果及GroupID | 真人人像 |
| CreateAsset | POST | 上传素材(异步) | 通用 |
| GetAsset | POST | 查询单个素材 | 通用 |
| GetAssetGroup | POST | 查询单个素材组 | 通用 |
| ListAssets | POST | 查询素材列表 | 通用 |
| ListAssetGroups | POST | 查询素材组列表 | 通用 |
| UpdateAsset | POST | 更新素材信息 | 通用 |
| UpdateAssetGroup | POST | 更新素材组信息 | 通用 |
| DeleteAsset | POST | 删除素材 | 通用 |
| DeleteAssetGroup | POST | 删除素材组 | 通用 |

### 8.2 限流要求

| 接口 | 限流 |
|------|------|
| CreateVisualValidateSession / GetVisualValidateResult | 3 QPS |
| CreateAssetGroup | 10 QPS |
| CreateAsset | 300 QPM |
| GetAsset | 100 QPS |
| GetAssetGroup / ListAssetGroups / ListAssets | 10 QPS |
| UpdateAsset / UpdateAssetGroup | 10 QPS |
| DeleteAsset | 10 QPS |
| DeleteAssetGroup | 5 QPS |

### 8.3 Go SDK 调用模板

```go
package main

import (
    "fmt"
    "github.com/bytedance/sonic"
    "github.com/volcengine/volcengine-go-sdk/volcengine"
    "github.com/volcengine/volcengine-go-sdk/volcengine/credentials"
    "github.com/volcengine/volcengine-go-sdk/volcengine/session"
    "github.com/volcengine/volcengine-go-sdk/volcengine/universal"
)

func main() {
    config := volcengine.NewConfig().
        WithCredentials(credentials.NewStaticCredentials("<AK>", "<SK>", "")).
        WithRegion("cn-beijing")
    sess, _ := session.NewSession(config)

    resp, err := universal.New(sess).DoCall(
        universal.RequestUniversal{
            ServiceName: "ark",
            Action:      "CreateAssetGroup",  // 替换为目标接口
            Version:     "2024-01-01",
            HttpMethod:  universal.POST,
            ContentType: universal.ApplicationJSON,
        },
        &map[string]any{
            "Name":        "my_group",
            "Description": "描述",
            "GroupType":   "AIGC",            // AIGC 或 LivenessFace
            "ProjectName": "default",
        },
    )
    if err != nil {
        fmt.Printf("error: %v\n", err)
        return
    }
    respData, _ := sonic.Marshal(resp)
    fmt.Println(string(respData))
}
```

### 8.4 项目隔离说明

- 素材库按 **Project** 隔离
- Asset 和 Asset Group 的 `ProjectName` 必须一致
- 素材的 `ProjectName` 需与视频生成 API 的 API Key 所属项目一致
- 默认 `ProjectName` 为 `default`（大小写敏感）

---

## 九、计费说明

### 9.1 Seedance 视频生成定价

| 模型 | 在线推理 (元/百万token) | 离线推理 (元/百万token) |
|------|----------------------|----------------------|
| **seedance-2.0** (480p/720p，无视频输入) | 46.00 | 不支持 |
| **seedance-2.0** (480p/720p，有视频输入) | 28.00 | 不支持 |
| **seedance-2.0** (1080p，无视频输入) | 51.00 | 不支持 |
| **seedance-2.0** (1080p，有视频输入) | 31.00 | 不支持 |
| **seedance-2.0-fast** (无视频输入) | 37.00 | 不支持 |
| **seedance-2.0-fast** (有视频输入) | 22.00 | 不支持 |
| **seedance-1.5-pro** (有声) | 16.00 | 8.00 |
| **seedance-1.5-pro** (无声) | 8.00 | 4.00 |
| **seedance-1.0-pro** | 15.00 | 7.50 |
| **seedance-1.0-pro-fast** | 4.20 | 2.10 |
| **seedance-1.0-lite** | 10.00 | 5.00 |

### 9.2 价格速算（5 秒视频，16:9，720p）

| 模型 | 无视频输入 (元/个) | 有视频输入 (元/个) |
|------|------------------|------------------|
| seedance-2.0 | **4.97** | 5.44~12.10 |
| seedance-2.0-fast | **4.00** | 4.28~9.50 |
| seedance-1.5-pro (有声) | **1.73** | - |
| seedance-1.5-pro (无声) | **0.86** | - |

### 9.3 计费公式

```
视频费用 = token 单价 × token 用量
token 用量 = (输入视频时长 + 输出视频时长) × 宽 × 高 × 帧率 / 1024
```

- 仅对成功生成的视频计费，失败不收费
- 准确 token 用量以 API 返回的 `usage` 字段为准
- 2.0 系列存在**最低 token 用量限制**（有视频输入时）

---

## 十、常见问题与注意事项

### 10.1 视频生成

**Q: 生成的视频 URL 多久失效？**
A: **24 小时**后自动清理。建议配置 TOS 数据订阅自动转存。

**Q: 任务数据保留多久？**
A: **7 天**。超过后自动清除，无法查询。

**Q: 2.0 系列为什么不能直接上传真人照片？**
A: 为防范 Deepfake 风险，需通过私域素材库入库后使用（虚拟人像或真人认证）。

**Q: duration 设为 -1 有什么效果？**
A: 模型自动选择合适时长（仅 2.0 和 1.5 pro 支持）。实际时长可从查询结果的 `duration` 字段获取。

### 10.2 素材库

**Q: 素材上传后无法生成视频？**
A: 检查 `ProjectName` 是否一致。素材库按项目隔离，CreateAsset、GetAsset 和视频生成 API 必须使用同一项目。

**Q: 素材 URL 有效期？**
A: GetAsset 返回的 URL 有效期 **12 小时**。

**Q: 如何在提示词中引用素材？**
A: 使用"图片1"、"视频1"序号指代，**不要**直接使用 Asset ID。

### 10.3 关键限制汇总

| 限制项 | 值 |
|--------|-----|
| 提示词长度 | 中文 <= 500 字，英文 <= 1000 词 |
| 参考图片数量 (2.0) | 1~9 张 |
| 参考视频数量 (2.0) | 1~3 个，总时长 <= 15s |
| 参考音频数量 (2.0) | 1~3 段，总时长 <= 15s |
| 单张图片大小 | < 30 MB |
| 单个视频大小 | <= 50 MB |
| 单个音频大小 | <= 15 MB |
| 请求体大小 | <= 64 MB |
| 视频时长 (2.0) | 4~15 秒 |
| 视频时长 (1.5 pro) | 4~12 秒 |
| 视频时长 (1.0 系列) | 2~12 秒 |
| 任务超时 | 默认 48h，范围 [1h, 72h] |
| 账户余额要求 (2.0) | >= 200 元 |

---

> 本文档基于火山方舟官方文档整理，最后更新: 2026-04-17
>
> 官方文档链接:
> - [计费说明](https://www.volcengine.com/docs/82379/1544106?lang=zh)
> - [视频生成 API](https://www.volcengine.com/docs/82379/1520758?lang=zh)
> - [Seedance 2.0 教程](https://www.volcengine.com/docs/82379/2291680?lang=zh)
> - [提示词指南](https://www.volcengine.com/docs/82379/2222480?lang=zh)
> - [私域虚拟人像库](https://www.volcengine.com/docs/82379/2333565?lang=zh)
> - [私域真人人像库](https://www.volcengine.com/docs/82379/2333589?lang=zh)
