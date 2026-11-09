# 新前端补齐 fork 视频渠道配置（渠道抽屉）

2026-11-09

## 背景

10 月合并上游（26a3623c2）时，新前端的渠道抽屉换成了上游重写的版本。合并前 `web/default`
抽屉里 fork 自己的视频渠道配置因此全部丢失，包括：素材库、Seedance 第三方开关、
透传上游任务 ID、火山透传控制面签名，以及 45 和火山透传的地址输入。随后的 3664298f1
又把 fork 渠道的类型号从 58–61 挪到了 64–67。

现状：

- 后端对这些配置都有支持。
- 新前端 `channel-form.ts` 还保留着这些字段的读取和写回，类型号已修正为 64–67，但抽屉里没有任何界面。
- 经典版有完整界面，类型号也已修正。
- 线上 4 个 Seedance 第三方渠道（153、164、167、169）依赖其中的素材预上传和透传上游任务 ID。

## 目标与范围

目标：

1. 新前端渠道抽屉的配置能力恢复到合并前的水平，与经典版一致。
2. 以后合并上游时，fork 代码不再被整块覆盖。fork 逻辑集中放在独立文件里，上游文件只保留少量调用。

涉及的渠道类型：

| 类型 | 名称 | 本次补齐 |
|---|---|---|
| 45 / 54 | 火山方舟 / 豆包视频 | 素材库预上传（官方 BytePlus / 第三方 Cloudwise）、透传上游任务 ID；45 的地址改为下拉加手填 |
| 64 | OpenAI Video | 密钥格式提示、默认模型列表 |
| 65 | Seedance 第三方 | 素材预上传开关、透传上游任务 ID、默认模型列表 |
| 66 | 字节火山透传 | 地址下拉加手填、控制面签名、素材库条数上限、新建时的默认地址 |

不做：

- 不改后端，也不改 settings 的键名和语义。
- 不改经典版。
- 不新增合并前没有的校验规则。
- 模型定价里的视频限时折扣编辑器是第二个子项目，单独设计。

## 一、代码结构

### fork 模块：`web/src/features/channels/lib/video-channel-extensions.ts`

这是 fork 视频渠道的唯一入口，职责如下：

- 字段定义：第二节表中 13 个 settings 字段的 zod 定义，由 `channelFormSchema` 展开引入。
- 默认值：由 `CHANNEL_FORM_DEFAULT_VALUES` 展开引入。
- 读取：从已解析的 settings 对象生成表单值，规则见第二节。
- 写回：把表单值写进 settings，渠道类型不对应时删除这些键，规则见第二节。
- 校验：由 `channelFormSchema` 的 `superRefine` 调用，规则见第四节。
- 类型信息：
  - 各类型显示哪些配置块；
  - 地址由 fork 组件渲染的类型（45、66），以及它们的地址预设；
  - 66 的默认地址；
  - 64、65 的默认模型列表；
  - 64 的密钥格式提示。

以上读取、写回、校验逻辑从 `channel-form.ts` 原样搬过来，行为不变，`channel-form.ts` 里的旧代码同时删除。

### 界面组件：`web/src/features/channels/components/drawers/video-channel/`

| 文件 | 内容 | 适用类型 |
|---|---|---|
| `video-channel-settings.tsx` | 入口，按类型组合下面各块；其他类型返回 `null` | 全部 |
| `video-channel-block.tsx`、`video-channel-fields.tsx` | 配置块外框，以及开关行、文本输入行；密钥输入复用共享的 `PasswordInput` | 各块共用 |
| `video-channel-base-url-field.tsx` | 地址下拉加手填 | 45、66 |
| `asset-library-fields.tsx` | 素材库预上传 | 45、54 |
| `seedance-third-party-fields.tsx` | 素材预上传开关 | 65 |
| `upstream-task-id-field.tsx` | 透传上游任务 ID 开关 | 45、54、65 |
| `volc-passthrough-fields.tsx` | 控制面签名、素材库条数上限 | 66 |

- 组件接收四个参数：`form`、当前类型、是否禁用（没有编辑权限时为真）、地址占位文字（抽屉现有的 `baseUrlPlaceholder`，即服务端下发的默认地址）。
- 控件复用项目已有的 Form、Input、Switch、Select、`ComboboxInput`。开关的样式与抽屉里现有的开关保持一致。

### 上游文件的接入点

只加调用，不改原有逻辑：

| 文件 | 改动 |
|---|---|
| `lib/channel-form.ts` | 加 5 处调用：schema 展开 fork 字段，默认值展开 fork 默认值，读取 settings 时展开 fork 读取结果，`buildSettingsJSON` 调用 fork 写回，`superRefine` 调用 fork 校验。被搬走的旧代码删除 |
| `components/drawers/channel-mutate-drawer.tsx` | 1. 在"连接与模型"分区的通用地址输入之后渲染入口组件，与合并前的位置一致；<br>2. 上游 45 的两个地址块（锁定下拉、解锁后输入）和通用地址块的条件各加一个"地址不由 fork 组件渲染"；<br>3. 切换类型时设默认值的 effect 里调用一次 fork 默认值；<br>4. "填充相关模型"的模型来源在 64、65 时先取 fork 默认模型 |
| `lib/channel-utils.ts` | 密钥提示先查上游的提示表，查不到再查 fork 模块 |
| `src/i18n/static-keys.ts` | 登记 64 的密钥提示。它经变量传给 `t()`，不登记的话提取工具识别不到 |

上游 45 的"连点标签解锁"代码保留，不删；45 只是不再走那两个分支。

## 二、settings 兼容

键名、默认值、读写规则与现有 `channel-form.ts`、经典版、后端全部一致。已有渠道打开后再保存，内容不变。

| 键 | 写入的类型 | 默认值 | 读取 | 写回 |
|---|---|---|---|---|
| `byteplus_asset_enabled` | 45、54 | `false` | `=== true` | `=== true` |
| `asset_provider` | 45、54 | `byteplus` | 空值取 `byteplus` | 空值取 `byteplus` |
| `byteplus_access_key` | 45、54、66 | 空 | 原值 | 去首尾空格 |
| `byteplus_secret_key` | 45、54、66 | 空 | 原值 | 去首尾空格 |
| `byteplus_project_name` | 45、54、66 | `default` | 空值取 `default` | 去空格后为空则取 `default` |
| `byteplus_asset_group_id` | 45、54 | 空 | 原值 | 去首尾空格 |
| `byteplus_region` | 45、54 | `cn-beijing` | 空值取 `cn-beijing` | 去空格后为空则取 `cn-beijing` |
| `byteplus_moderation_skip` | 45、54 | `true` | `!== false` | `!== false` |
| `seedance3rd_asset_enabled` | 65 | `false` | `=== true` | `=== true` |
| `expose_upstream_task_id` | 45、54、65 | `false` | `=== true` | `=== true` |
| `volc_sign_region` | 66 | 空 | 原值 | 去首尾空格 |
| `volc_openapi_endpoint` | 66 | 空 | 原值 | 去首尾空格 |
| `volc_asset_quota_limit` | 66 | `0` | 转数字，非法为 `0` | 转数字后取整，最小为 `0` |

清理规则，与现在一致：

- 不是 45、54 时，删除 `byteplus_*` 和 `asset_provider`。例外是 66：它的 AK、SK、项目名由火山透传分支写入，素材库相关的旧键也不删。
- 不是 66 时，删除 `volc_*`。
- 不是 65 时，删除 `seedance3rd_asset_enabled`。
- 不是 45、54、65 时，删除 `expose_upstream_task_id`。

## 三、界面

全部放在"连接与模型"分区、通用地址输入之后，字段和文案与合并前一致。

- 45、66 的地址：下拉选择或手动填写，用 `ComboboxInput` 并允许自定义值。
  - 两者的预设都是 `https://ark.cn-beijing.volces.com`（国内）和 `https://ark.ap-southeast.bytepluses.com`（海外），选项直接显示地址，不另加地域文案。
  - 标签为"API Base URL"。45 标为必填，因为已有必填校验；66 不标必填，因为地址为空时后端会用默认地址。占位文字沿用抽屉现有逻辑，即服务端下发的默认地址；清空后保存，不会把占位地址写进去。
  - 45 保留上游对 `doubao-coding-plan` 的处理：该值显示为国内地址。
  - 新建渠道时选中 66 且地址为空，默认填入国内地址。
- 45、54 素材库预上传：一个开关，打开后选择素材库来源。
  - 官方（BytePlus）：Access Key、Secret Key、素材组 ID、项目名、区域、跳过内容预审。
  - 第三方（Cloudwise）：提示"复用渠道地址和密钥"，只显示素材组 ID 和跳过内容预审。
- 65：素材预上传开关。
- 45、54、65：透传上游任务 ID 开关，说明里注明会暴露上游供应商。
- 66 控制面签名：Access Key、Secret Key、项目名、签名区域、控制面地址、素材库条数上限。签名区域和控制面地址留空时，后端按渠道地址推导。
- 64：密钥输入框显示格式提示，英文原文为 `Format: SubAppId|SecretId|SecretKey[|Region], or an API key such as sk-xxx`。
- 64、65：点"填充相关模型"时，填入与经典版相同的固定模型列表，来源是经典版 `EditChannelModal.jsx` 里 `case 64`、`case 65` 的列表。65 的列表与后端 `seedance3rd.ModelList` 一致。
- 没有编辑权限时整块禁用，与抽屉里其他敏感字段一致。
- 文案沿用合并前的翻译键，七种语言里都还在。新增四个键，七个语言文件都要加：
  - 64 的密钥提示；
  - 素材库条数上限的占位文字 `e.g., 1000`；
  - 素材库条数上限的说明；
  - 66 地址的说明。

  后两个合并前就没有翻译过，界面一直显示英文。

## 四、校验与错误

校验规则与现在一致，只是搬进 fork 模块：

- 45、54 开启素材库并选择官方：Access Key、Secret Key 必填。
- 45、54 开启素材库并选择 Cloudwise：渠道地址必填。
- 66 不新增必填规则。合并前也没有这条规则；地址为空时，后端使用 `ChannelBaseURLs` 里的默认地址。

fork 字段没有登记在 `lib/channel-configuration.ts` 的配置块里，出错时会按抽屉现有的错误定位跳到"连接与模型"分区并聚焦该字段，所以上游的错误定位不用改。

## 五、测试

测试放在各模块的 `__tests__/` 目录下。

fork 模块：扩展现有的 `lib/__tests__/fork-channel-types.test.ts`。

- 45、54、65、66 的 settings 读出再写回后内容不变，含 Cloudwise 和官方两种素材库来源。
- 换成其他类型后保存，对应的键按第二节的规则清理。
- 三条校验规则；关闭素材库时不做校验。
- 64、65 的默认模型，64 的密钥提示，66 的默认地址。

界面组件：新建 `components/drawers/video-channel/__tests__/video-channel-settings.test.tsx`。

- 每种类型显示哪些配置块；OpenAI 等上游类型一块都不显示。
- 切换素材库来源时，各字段正确显示或隐藏。
- 没有编辑权限时整块禁用。
- 每个控件都有可访问名称。

抽屉集成：改动现有的 `components/__tests__/channel-configuration.test.tsx`，防止以后合并上游时接入点被冲掉。

- 新增一条：编辑 66 渠道时能看到签名配置；改其中一项后保存，其余 AK、SK 不丢。
- 新增一条：新建渠道时选火山透传，地址自动填国内地址。
- 新增一条：新建渠道时选 Seedance 第三方，点"填充相关模型"后填入 fork 的模型列表。
- 调整一条：现有用例"editing type 45 keeps the URL placeholder out of the saved address"依赖连点解锁，并按 textbox 角色查找输入框。改为直接操作新的地址下拉（`ComboboxInput` 的输入框是 combobox 角色），断言不变：占位是服务端默认地址，清空并手填后保存的是手填地址。这条同时覆盖"45 可以直接手填地址"。

这四条分别对应抽屉里的四处接入点。

收尾检查：`bun run typecheck`、改动文件的 lint 和格式检查、全量测试，结果与修改前的基线对比。

## 六、上线与维护

- 只改前端，随下个版本发布。settings 键名不变，不需要数据迁移。
- 前提：4 个 Seedance 第三方渠道已用 SQL 从 59 改为 65。
- 以后合并上游后，先确认 `channel-form.ts` 的 5 处调用和抽屉里的 4 处接入点都还在，再跑 fork 相关测试。
- 45 的上游地址块被 fork 组件整个接管，上游以后在那里加的地区或 Coding Plan 处理不会自动出现。合并上游后，对比上游 45 地址块的选项与 `VIDEO_CHANNEL_BASE_URL_PRESETS`，有新增就同步到 fork 模块。
