# 中转站 wan 视频接入设计（通用中转站模式）

日期：2026-11-04
状态：设计已逐段确认，待审阅规格

## 1. 背景与目标

阿里百炼的 wan 视频模型现在由内置的 `alibaba` 任务插件处理。客户端有三个入口：

| 入口 | 提交 | 查询 |
|---|---|---|
| OpenAI 视频格式 | `POST /v1/videos` | `GET /v1/videos/{id}`，下载 `GET /v1/videos/{id}/content` |
| 阿里原生格式 | `POST /ali/api/v1/services/aigc/video-generation/video-synthesis` | `GET /ali/api/v1/tasks/{id}` |
| 旧版格式（仅 Ali 类型渠道） | `POST /v1/video/generations` | `GET /v1/video/generations/{id}` |

目标：

1. 接入第三方中转站（第一家是 TokenPony），客户端入口和请求参数完全不变。
2. 同一个模型名（如 `wan3.0-video`）在阿里直连和中转站之间按渠道优先级、权重分流，一家失败时可以换另一家重试。
3. 以后接新的中转站不改代码：新建渠道，填 Base URL、密钥、模型，再填一份「中转站配置」JSON 即可。

范围：

- 只支持视频模型，首批是 `wan3.0-video`。图片模型不支持中转站模式。
- 编辑界面只做新前端，经典版不做。
- 中转站的请求和返回都必须是 JSON；不支持 SSE、回调和文件上传（素材必须是 URL 或 Base64 data URL）。

不做：按供应商分别定价、图片模型、经典版界面。

## 2. 总体方案

中转站渠道和阿里直连渠道绑定的是同一个 `alibaba` 插件，所以三个入口都能分到中转站：

- 渠道类型选「Task Plugin」并绑定阿里百炼；或者选「Ali（阿里）」类型，这样还能支持旧版入口。
- 渠道设置里新增 `task_plugin_config` 字段。其中有 `relay` 时，阿里插件进入「中转站模式」；没有时行为和现在完全一样。

中转站模式的处理流程：

1. 客户端请求先经过阿里插件现有的解码、校验和默认值补全，整理成一份阿里官方格式的请求（下文称「标准请求」）。
2. 按 `relay.submit` 把标准请求转换成中转站格式，发到「渠道 Base URL + submit.path」。
3. 按配置解析提交返回，拿到中转站的任务号。
4. 后台轮询时按 `relay.query` 查询，把返回转换成阿里格式的快照保存，中转站原始返回不保存。
5. 展示、下载、结算都读这份阿里格式的快照，现有代码不用改。

## 3. 中转站配置格式

配置位置：渠道设置 `task_plugin_config.relay`，整份配置不超过 32 KiB。

### 3.1 标准请求字段（`$.` 可以引用的范围）

| 字段 | 说明 |
|---|---|
| `model` | 上游模型名（已按渠道模型映射替换） |
| `input.prompt`、`input.negative_prompt`、`input.template` | 提示词、反向提示词、特效模板 |
| `input.media` | 素材数组，每项 `{type, url}`。type 取值：`first_frame`、`last_frame`、`reference_image`、`reference_video`、`reference_audio`、`file`、`link` |
| `parameters.resolution` | `480P` / `720P` / `1080P`，未传时为模型默认值（wan3.0 是 1080P） |
| `parameters.ratio` | 宽高比，wan3.0 未传时为 `adaptive` |
| `parameters.size` | 少数旧版文生视频模型使用的像素尺寸 |
| `parameters.duration` | 秒数，`-1` 表示智能时长 |
| `parameters.audio`、`watermark`、`seed`、`prompt_extend`、`shot_type` | 客户端传了才有，`prompt_extend` 默认是 true |
| `task_id` | 只在 POST 查询的 body 模板里可用，是中转站的任务号 |

引用了不在这张表里的字段时，保存会报错。

### 3.2 提交 `relay.submit`

| 字段 | 必填 | 说明 |
|---|---|---|
| `path` | 是 | 拼在渠道 Base URL 后面，如 `/v1/aigc/video/generate` |
| `method` | 否 | 只支持 `POST`（默认） |
| `headers` | 否 | 默认 `Authorization: Bearer {key}`，有 body 时再加 `Content-Type: application/json`。可以按名字覆盖（不区分大小写），`{key}` 会替换成渠道密钥，值写 `null` 表示去掉这个请求头 |
| `body` | 是 | 中转站请求体模板，规则见 3.4 |
| `values` | 否 | 按中转站请求体的路径替换取值，规则见 3.5 |
| `ok` | 否 | 判断提交是否成功，如 `{"path": "code", "equals": 200}`。不写就只看 HTTP 状态码是否为 2xx |
| `taskId` | 是 | 中转站任务号的路径，如 `data.id`。取到的值必须是非空字符串或数字 |
| `error` | 否 | 失败时错误信息的路径 |

### 3.3 查询 `relay.query`

| 字段 | 必填 | 说明 |
|---|---|---|
| `path` | 是 | 用 `{task_id}` 占位，如 `/v1/aigc/tasks/{task_id}`，任务号编码后填入 |
| `method` | 否 | `GET`（默认）或 `POST`；POST 时可以写 `body` 模板，用 `$.task_id` 取任务号 |
| `headers` | 否 | 规则同提交 |
| `body` | 否 | 只在 POST 时使用 |
| `ok` | 否 | 不成功的一次记为查询失败 |
| `status` | 是 | 状态字段的路径 |
| `statusMap` | 是 | 把中转站的状态（统一按字符串比较）对应到 `PENDING`、`RUNNING`、`SUCCEEDED`、`FAILED`、`CANCELED`。至少要有一个对应到 `SUCCEEDED`，至少一个对应到 `FAILED` 或 `CANCELED`。不在表里的状态记为查询失败 |
| `videoUrl` | 是 | 视频地址路径，可以写一个或一组，取第一个非空字符串 |
| `reason` | 否 | 失败原因路径 |
| `usage` | 否 | 实际用量：`seconds`（输出时长）、`inputSeconds`（输入视频时长，可选）、`resolution`（分辨率档位） |

### 3.4 body 模板规则

| 写法 | 含义 |
|---|---|
| `"$.parameters.duration"` | 整个字符串是一个引用：取标准请求里的值，保持原类型。取不到就不传这个字段 |
| `{"$each": "$.input.media", "kind": "@.type", "image_url": "@.url"}` | 逐项改写数组，`@.` 表示当前这一项。字段名完全一样时不用写，直接 `"media": "$.input.media"` |
| 其他任何值 | 原样作为固定值发送，如 `"watermark": false` |

模板只能是 JSON 数据，不支持表达式和代码。嵌套不超过 16 层。

### 3.5 路径与取值规则

- **返回里取值**（`taskId`、`error`、`status`、`videoUrl`、`reason`、`usage.*`、`ok.path`）：用点号分隔，数字段表示数组下标，如 `data.results.0.url`。
- **`values` 的键**是中转站请求体里的路径，`[]` 表示数组里的每一项，如 `media[].type`、`seed`。
  - 映射表的键是取值的字符串形式，所以 `"-1"` 同时匹配数字 -1 和字符串 "-1"；
  - 映射成 `null` 表示删掉这个字段；
  - 不在映射表里的值保持不变。
- **`ok.equals`**：数字和它的字符串形式视为相等，`200` 和 `"200"` 都算。

### 3.6 保存的阿里格式快照

提交成功后保存：

```json
{"request_id": "", "output": {"task_id": "<中转站任务号>", "task_status": "PENDING"}}
```

每次查询成功后整体覆盖为：

```json
{
  "request_id": "",
  "output": {
    "task_id": "<中转站任务号>",
    "task_status": "PENDING | RUNNING | SUCCEEDED | FAILED | CANCELED",
    "video_url": "<仅成功时>",
    "code": "<仅失败或取消时，固定为 UpstreamTaskFailed>",
    "message": "<失败原因>"
  },
  "usage": {
    "duration": "<seconds>",
    "output_video_duration": "<seconds>",
    "input_video_duration": "<inputSeconds，没配或没取到时为 0>",
    "SR": "<resolution>"
  }
}
```

`usage` 只在配置了 `usage.seconds` 并且这次查询取到了值时才写入。

现有的这些代码直接读这份快照，不用改：原生查询的返回（`output.task_id` 会被替换成网关任务号）、`/v1/videos` 查询、视频下载、按实际用量结算、旧版查询里的 `data.data.output.video_url`。

### 3.7 TokenPony 配置（也是「插入示例配置」按钮填入的内容）

```json
{
  "relay": {
    "submit": {
      "path": "/v1/aigc/video/generate",
      "body": {
        "model": "$.model", "prompt": "$.input.prompt", "media": "$.input.media",
        "resolution": "$.parameters.resolution", "ratio": "$.parameters.ratio",
        "duration": "$.parameters.duration", "generate_audio": "$.parameters.audio",
        "watermark": "$.parameters.watermark", "seed": "$.parameters.seed"
      },
      "values": {
        "media[].type": { "first_frame": "first_image", "last_frame": "last_image" },
        "seed": { "-1": null }
      },
      "ok": { "path": "code", "equals": 200 },
      "taskId": "data.id",
      "error": "message"
    },
    "query": {
      "path": "/v1/aigc/tasks/{task_id}",
      "ok": { "path": "code", "equals": 200 },
      "status": "data.task_status",
      "statusMap": { "PENDING": "PENDING", "RUNNING": "RUNNING", "COMPLETED": "SUCCEEDED", "FAILED": "FAILED", "CANCELED": "CANCELED" },
      "videoUrl": ["data.result.output", "data.result.result"],
      "reason": "data.task_status_msg",
      "usage": { "seconds": "data.usage.duration", "resolution": "data.usage.resolution" }
    }
  }
}
```

说明：

- TokenPony 不支持 seed 为 -1，所以用 `values` 删掉；
- `prompt_extend` 没写进模板，就不会发给中转站；
- 任务不存在时 TokenPony 返回 HTTP 200、code 10107，按 `ok` 规则记为一次查询失败。

## 4. 后端（Go）改动

1. **渠道设置字段**：`relaykit/dto/channel_settings.go` 的 `ChannelSettings` 加 `TaskPluginConfig`（JSON 键 `task_plugin_config`，内容是 JSON 对象）。它存在已有的 setting 文本列里，不改表结构，不做迁移。改完要在 `relaykit` 目录下用 `GOWORK=off go build ./...` 验证能独立编译。
2. **保存渠道时校验**（`controller/channel.go` 的渠道校验）。配置非空时：
   - 必须是 JSON 对象，不超过 32 KiB；
   - 渠道类型必须是「Task Plugin」（按 `task_plugin_key` 找插件），或者能按渠道类型找到插件的类型（如 Ali）。New API 类型和找不到插件的类型都拒绝；
   - 插件必须导出 `validateChannelConfig`，后端调用它，出错就拒绝保存。返回信息为 `Invalid task plugin configuration: {{error}}`，这个键要补进新前端的 7 个语言文件。
3. **插件上下文**：提交、提交返回解析、预扣用量、查询、查询返回解析、批量查询、下载这些钩子的上下文里加 `ctx.channelConfig`（只读，没有配置时为 null）。后台轮询本来就带渠道信息，也加上。
4. **查询结果可带 `data`**：`parseTaskResult` 返回 `data` 时，后端把它保存为任务数据，代替原始返回，大小上限和现有的任务数据一样是 1 MiB；同时把它作为第三个参数传给 `extractUsageOnComplete`。涉及 `relay/channel/task/jsplugin/adaptor.go`、`relaycommon.TaskInfo` 和 `service/task_polling.go`。批量查询现在已经支持 `data`，行为对齐。
5. **可选钩子 `validateChannelConfig(config)`**：校验不通过就抛出 Error，报错信息会显示在渠道表单上。和其他可选钩子一样，插件加载时就确定有没有导出。
6. **预览接口**：`POST /api/channel/task_plugin_config/preview`，权限和编辑渠道相同（`authz.ChannelWrite`），详见第 9 节。
7. **接口文档**：`docs/plugin-api/v1.md`、`v1.d.ts`、`v1.schema.json` 同步写上第 3 到第 5 点，都是 apiVersion 1 的追加扩展。

## 5. 阿里插件改动（`plugins/tasks/alibaba/plugin.js`）

- **版本号**：1.4.1 升到 1.5.0。
- **新增导出 `validateChannelConfig`**：实现第 3 节的全部校验规则，以及第 8 节里的路径和请求头限制。
- **模式判断**：`ctx.channelConfig?.relay` 存在时进入中转站模式。中转站模式下请求图片模型时报错，提示中转站模式只支持视频模型。
- **中转站模式下各钩子的行为**：
  - `buildSubmitRequest`：先调用现有的 `convert(ctx)` 得到标准请求，校验也在这一步完成；再套模板、做 `values` 替换。请求地址是 Base URL 去掉末尾 `/` 后接上 `path`，请求头按 3.2 生成。
  - `parseSubmitResponse`：先按 `ok` 判断，不通过就抛出 `error` 路径上的信息（取不到就用「中转站返回失败」）；取不到任务号也抛错；成功时返回任务号和 3.6 的初始快照。
  - `buildQueryRequest`：按 3.3 生成查询请求。
  - `parseTaskResult`：生成 3.6 的快照，然后按快照状态返回：
    - PENDING 返回 QUEUED，RUNNING 返回 IN_PROGRESS；
    - SUCCEEDED 返回 SUCCESS，并带上视频地址；如果取不到视频地址，返回 UNKNOWN；
    - FAILED 或 CANCELED 返回 FAILURE，原因取不到时用 `task failed`；
    - `ok` 不通过或者状态不在表里，也返回 UNKNOWN。
    
    返回结果里带上 `data: 快照`。
- **不改的部分**：预扣用量（`extractUsage`）、`extractUsageOnComplete`、`listArtifacts`、`buildContentRequest`、原生入口的返回和 `/v1/videos` 的返回都不改，它们读到的就是阿里格式。
- **非中转站模式**：所有逻辑和现在完全一样。

## 6. 计费

- **价格**：两家共用 `wan3.0-video` 的同一套模型价格，按秒计费、分辨率分档。上线前要先在「模型定价」里配好。两家进价的差异用渠道上已有的成本设置记录。
- **预扣**：和阿里直连完全一致，所以同一个请求，不管分到哪家，预扣金额都相同。
  - 按标准请求里的时长和分辨率算，不传分辨率按 1080P；
  - 智能时长（-1）或带参考视频时，按上限 30 秒预扣。
- **结算：多退少补**（已确认）。
  - 按快照里的用量结算。wan3.0 系列的秒数等于输出时长加输入时长，输入时长没配或没取到就按 0 算；其他模型只算输出时长。分辨率取中转站返回的档位，没取到就沿用请求时的档位。
  - 实际用量比预扣多时补扣，少时退差价，和阿里直连的做法一致。
  - 配置没写 `usage.seconds`，或者查询时没取到用量，就按预扣金额收。
- **保护**：中转站报回的用量要过后端现有的校验（时长不超过 3600 秒，分辨率必须在 schema 允许的三档里），不合法时按预扣金额收，并记一条警告。额度换算和结算链路沿用现有逻辑，不改。

## 7. 出错处理

**提交**

| 情况 | 处理 |
|---|---|
| 请求参数不合法 | 返回 400，不发请求，不扣费 |
| 中转站返回非 2xx | 提交失败，退还预扣。5xx 和 429 可以换渠道重试，4xx 不重试 |
| 2xx 但 `ok` 不通过 | 返回 502，报错信息取 `error` 路径，退还预扣，可以换渠道重试 |
| 2xx、`ok` 通过但取不到任务号 | 同上。此时中转站可能已经建了任务，重试会重复下单，所以新配置要先用预览和少量真实请求验证 |

「换渠道重试」需要把「失败重试次数」设为至少 1。默认是 0，也就是不重试。

**查询**

| 情况 | 处理 |
|---|---|
| HTTP 404 或 410 | 立即判定失败，退款 |
| HTTP 401 或 403 | 状态不变，记一条警告，不自动禁用渠道 |
| 429、5xx、网络错误、`ok` 不通过、状态不在表里、显示成功却没有视频地址 | 记一次查询失败，继续轮询；连续 20 次后判定失败并退款 |
| 中转站显示失败或取消 | 任务失败并退款，原因取 `reason` 路径 |
| 超过 24 小时未完成 | 现有的超时清理会把任务判失败并退款 |

## 8. 安全

- **目标地址固定**：插件发出的每个请求，后端都会检查必须发往渠道 Base URL 的域名（`pluginruntime.ValidateRequestURL`），配置里的 `path` 改不了目标域名。
- **`path` 的限制**：必须以 `/` 开头，不能含 `..`、`//`、`#` 和控制字符。
- **请求头的限制**：
  - 不能设置 `Host`、`Content-Length`、`Transfer-Encoding`、`Connection`；
  - 头名必须是合法字符，值里不能有换行；
  - 只支持 `{key}` 一个占位符。
- **视频下载**：中转站给的视频地址，下载时走现有的防 SSRF 客户端，指向内网的地址会被拒绝。
- **密钥**：渠道密钥只在发请求时替换进请求头，不会存进任务数据，也不会返回给客户端。
- **权限**：配置是纯数据映射，不能执行代码。只有有渠道编辑权限的管理员能修改配置，也只有他们能调用预览。

## 9. 配置预览

**用途**：保存渠道之前，验证配置能否正确地转换请求、解析返回。整个过程不发任何网络请求、不写数据库、不扣费。

**接口**：`POST /api/channel/task_plugin_config/preview`

请求（各部分可选，填了哪部分就预览哪部分）：

```json
{
  "type": 61,
  "task_plugin_key": "alibaba",
  "base_url": "https://api.tokenpony.cn",
  "config": { "relay": { } },
  "model": "wan3.0-video",
  "entry": "openai_video",
  "request": { "model": "wan3.0-video", "prompt": "...", "seconds": 5, "resolution": "720P" },
  "submit_response": { "code": 200, "data": { "id": "..." } },
  "query_response": { "code": 200, "data": { "task_status": "COMPLETED" } }
}
```

`entry` 取 `openai_video`，或者插件的某个原生提交路径（如 `/ali/api/v1/services/aigc/video-generation/video-synthesis`），用来指定 `request` 是按哪个入口的格式写的。后端按 `entry` 找到对应的解码器，不写死任何插件。

处理过程：

1. 用和保存时相同的规则，先校验渠道类型和配置。
2. 有 `request` 时：调用对应入口的解码器，用表单里的 Base URL、打码后的密钥 `sk-****` 和 `config` 调 `buildSubmitRequest`，得到提交请求；再调 `extractUsage` 得到预扣用量。
3. 有 `submit_response` 时：调 `parseSubmitResponse`（HTTP 状态按 200 处理），得到任务号。
4. 有 `query_response` 时：用第 3 步的任务号（没有就用 `preview-task-id`）调 `buildQueryRequest`、`parseTaskResult`，再用快照调 `extractUsageOnComplete`，得到结算用量。

返回：

```json
{
  "submit": { "url": "...", "method": "POST", "headers": { "Authorization": "Bearer sk-****" }, "body": { } },
  "reservation": { "seconds": 5, "resolution": "720P" },
  "submit_result": { "task_id": "..." },
  "query": { "url": ".../v1/aigc/tasks/<task_id>", "method": "GET" },
  "query_result": { "status": "SUCCESS", "url": "...", "reason": "", "snapshot": { } },
  "settlement": { "seconds": 5, "resolution": "720P" },
  "errors": { "submit": "", "submit_result": "", "query_result": "" }
}
```

每一步出错只把错误写进 `errors` 里对应的项，不影响其他步骤。输入的各个 JSON 每份不超过 1 MiB，插件钩子沿用现有的执行超时。

## 10. 前端（新前端 `web/src`）

**中转站配置区块**

- 放在渠道编辑抽屉里，只在两种渠道上显示：「Task Plugin」类型且插件是阿里百炼，或「Ali（阿里）」类型。
- 编辑框复用 `JsonCodeEditor`（`@/components/json-code-editor`），留空表示直连阿里。
- 「插入示例配置」按钮：填入 3.7 的配置。
- 「预览」按钮：打开预览弹窗。
- 保存时后端返回的校验错误显示在表单上。

**预览弹窗**

- 复用 `@/components/dialog` 和 `JsonCodeEditor`。
- 输入三块，都预填示例：示例请求（带入口选择）、提交返回、查询返回。
- 点「运行预览」后，在下方用只读方式展示：提交请求、预扣用量、任务号、查询请求、查询结果和快照、结算用量，以及各步的错误。

**其他**

- 新增的界面文字和后端提示键，补齐 7 种语言（`bun run i18n:sync`）。
- 经典版不做界面。经典版保存渠道时会保留它不认识的设置项，不会冲掉配置。

## 11. 测试与验证

- **插件**：新增一个测试文件 `plugins/alibaba_relay_test.go`，用 3.7 的配置覆盖：
  - 请求转换：素材类型改名、`audio` 转成 `generate_audio`、seed 为 -1 时删除、取不到的字段不传、`$each` 改写数组；
  - 提交返回：`ok` 判断、取任务号、取报错信息；
  - 查询：地址编码、POST 查询的 body；
  - 查询返回：状态对应、多个视频地址取第一个非空、用量（包括 `inputSeconds`）、未知状态和「成功但没有视频地址」都返回 UNKNOWN；
  - `validateChannelConfig` 能拦下各种错误写法；
  - 非中转站模式的现有测试全部照常通过。
- **后端**：扩充现有测试文件，不新建：
  - `controller/channel_task_plugin_validation_test.go`：保存校验（合法、非法、类型不对、超过上限）和预览接口；
  - `relay/channel/task/jsplugin/adaptor_test.go`：`ctx.channelConfig` 会传给插件，`parseTaskResult` 的 `data` 会被保存并传给结算；
  - 轮询保存任务数据的部分扩充 `service/task_polling_test.go`。
  
  新测试统一用 testify 的 require 和 assert。
- **端到端**：用模拟中转站跑通 `/v1/videos` 和 `/ali/...` 两个入口提交、后台轮询、`/ali/api/v1/tasks/{id}` 查询、视频下载，确认保存的和返回的都是阿里格式，结算按实际用量多退少补。
- **前端**：在 channels 模块的 `__tests__` 里写组件测试，覆盖：区块只在两种渠道上显示、保存时带上配置、显示后端校验错误、预览弹窗能展示结果和错误。另外跑 `bun run typecheck`，并对改动的文件跑 lint。
- **其他检查**：
  - `relaykit` 用 `GOWORK=off go build ./...` 单独编译；
  - 根模块编译并跑相关测试，Go 文件用 gofmt 格式化；
  - 插件接口文档的三个文件保持一致。
- **真实验证**：用你的 TokenPony Key 发一个最便宜的请求（480P、2 秒）。会产生少量费用，发之前先征得你同意。

## 12. 上线步骤

1. 修好 GitHub Actions，打出包含插件系统和本功能的新镜像，然后更新服务器。
2. 在「模型定价」里给 `wan3.0-video` 配置价格。
3. 把「失败重试次数」设为至少 1。
4. 新建中转站渠道：「Task Plugin」类型选阿里百炼（需要旧版入口就选「Ali（阿里）」类型），填 Base URL `https://api.tokenpony.cn`、密钥、模型 `wan3.0-video`、分组 default。然后插入示例配置，用预览验证无误后保存。
5. 发一个小额真实请求确认整个流程。
6. 按需要调整两家渠道的优先级和权重。

## 13. 已知限制

- 不能按供应商分别定价，两家共用模型价格。
- 中转站的报错原文和失败原因会截断后返回给客户端，和现在直连阿里时一样，客户端可能从措辞看出是哪家中转站。
- 原生入口 `/ali/api/v1/services/aigc/image2video/video-synthesis` 在中转站模式下同样发往 `relay.submit.path`。中转站渠道只填它支持的模型即可。
- 预览只验证配置的转换和解析，不验证网络连通性和真实鉴权。
