# gpt-image-2.6 对齐设计（新参数转发与 svg_usage 计费）

日期：2026-11-10
状态：设计已确认（用户要求直接定稿并进入实施计划）

## 1. 背景

OpenAI 在 2026-11-04 发布 `gpt-image-2.6-sunburst` 和 `gpt-image-2.6-flare`，新增复杂度控制（`complexity`）、GIF 输出、SVG 输出和 markup 引导编辑。我们的 4 个 New API 实例都用不了 SVG。

### 1.1 官方约定

来源（2026-11-10 核对）：openai-python 3.46.0 由 OpenAPI 生成的类型（提交 #4247、#4210）、官方 Image generation 指南、官方价格页。

- 请求：`output_format: "svg"`，再加 `svg: {"model": "<描图模型 ID>", "reasoning": {"effort": "none|minimal|low|medium|high|xhigh|max"}}`。
  - `svg.model` 是字符串；effort 在 `svg.reasoning` 下面。
  - 上游账号开了 SVG 模型选择时必须带 `svg.model`；没开时不能带 `svg` 对象，由 OpenAI 选默认描图模型。
- 生成方式：先用生图模型画一张参考位图（`quality`、`complexity` 控制的是这张图），再由描图模型把它写成 SVG。
- 限制：只能 `n=1`；不支持 `stream`、`partial_images`、`output_compression`、`prompt_rewriter`；edits 接口既不输出 SVG，也不接受 SVG 作为输入。
- 返回：`data[0].b64_json` 是 base64 编码的 UTF-8 SVG 文本。响应里除了 `usage`，还有单独的 `svg_usage`：`{model, input_tokens, output_tokens, total_tokens, reasoning_tokens}`，五个字段都必有。
- 计费：参考图按生图模型的价格收；描图 token 按描图模型的价格收。
- GIF：generations 和 edits 都支持 `output_format: "gif"`，参数是 `frame_count`（2–16，默认 9）和 `frame_duration_ms`（20–2000，默认 200）。
- markup：edits 的表单上传版是 `markup` 文件；JSON 版是 `markup` 图片引用（file ID、HTTPS URL 或 data URL）。不能和 mask 同时用。
- `safety_identifier`：generations 和 edits 都有，用来取代已废弃的 `user`。
- 价格（Standard，每百万 token）：2.6 两个模型和 2.5 相同，文本输入 $5、文本缓存 $1.25、图片输入 $8、图片缓存 $2、输出 $30。

### 1.2 现状问题

当前 prod 的图片请求代码等同于上游 v1.0.0-rc.47。

1. `ImageRequest.MarshalJSON` 不把 `Extra` 写回去（`relaykit/dto/openai_image.go`）。渠道没开透传时请求体会被重新序列化，所以 `svg`、`complexity`、`frame_count`、`frame_duration_ms`、`safety_identifier`，以及 JSON 版 edits 的 `markup` 都被丢掉。表单版 edits 重新组包时只带 image 和 mask 文件，`markup` 文件也被丢掉。OpenAI、Azure、自定义、NewAPI 等走 OpenAI 适配器的渠道都受影响。
2. `svg_usage` 完全没读，描图 token 没有计费。即使开透传让 SVG 能用，这部分也是白送。
3. 被动图片检测会把 SVG、GIF 结果记成检测错误，因为 SynthID 只接受 PNG、JPEG、WebP。
4. `gpt-image-2.6-*` 没有内置价格。
5. 日志里的 `***.model`：上游报错原文是 `svg.model`，被 `MaskSensitiveInfo` 的域名规则当成域名脱敏了。

### 1.3 上游进展

QuantumNous/new-api 的 PR #8208（提交 3374f251e，2026-11-05，v1.0.0-rc.49 起包含）给 `ImageRequest` 加了 `complexity`、`markup`、`svg`、`frame_count`、`frame_duration_ms` 五个字段，并让表单版 edits 转发 `markup` 文件。它没有加 `safety_identifier`，也没有处理 `svg_usage`。补丁已用 `git apply --check` 验证可以直接打到 prod 上。

## 2. 目标与范围

目标：

1. 2.6 的所有新请求参数在不开透传时也能原样转发。
2. `svg_usage` 按描图模型的价格计费，加到这次生图请求的费用里，日志里能看到明细。
3. SVG、GIF 结果不再触发被动检测错误；2.6 有内置价格。

不做：

- Responses API 的 image_generation 工具也支持了 `svg` 和 `svg_usage`，那条链路按调用次数收工具费，另行处理。
- `***.model` 脱敏误伤要改 relaykit 的通用脱敏规则，另行处理。
- 不在本地检查 SVG 的组合限制（n、stream 等），由上游返回 400。
- 经典版前端不做结构化展示，只显示日志内容那一行文字。
- `svg.model` 不走渠道模型映射，也不做分组可用性限制。

## 3. 请求转发

1. 用 `git am` 打上游 #8208 的补丁，保留原作者和提交信息。之后合并上游时，同一处改动不会冲突。
2. 在 `ImageRequest` 里再加 `SafetyIdentifier json.RawMessage \`json:"safety_identifier,omitempty"\``，写法和其他 OpenAI 字段一致：客户端没传就不发，传了什么（包括 0、空串、null）就原样发。
3. 不恢复 `Extra` 合并。上游 7b5d406 是有意关掉的（注释"不能合并ExtraFields"）。合并回去会把客户端夹带的其他厂商参数发给 OpenAI，OpenAI 对不认识的参数报错；`parameters.n` 这类数量字段也会绕过校验被发到上游。以后 OpenAI 再加参数，同样按显式字段补。
4. 透传开关不变。
5. `relaykit` 是独立模块，改完要在 `relaykit/` 下跑 `GOWORK=off go build ./...`。

## 4. svg_usage 计费

### 4.1 读取

- `relaykit/dto/openai_image.go` 新增 `ImageSvgUsage{Model, InputTokens, OutputTokens, TotalTokens, ReasoningTokens}`，对应响应里的 `svg_usage`。
- `ImageRequest` 新增方法 `SvgAuthorModel()`：返回请求里 `svg.model` 去掉首尾空白后的值；没有 `svg`、`svg` 不是对象或 `model` 不是字符串时返回空串。
- `RelayInfo` 新增计费字段 `SvgUsage *dto.ImageSvgUsage`。`ImageHelper` 每次尝试开头把它置空（和 `BillingImageCount` 一样），避免重试带上前一次的值。
- `OpenaiImageHandler` 和 `openaiImageJSONAsStreamHandler`（客户端要流式、上游回普通 JSON 的分支）用 gjson 读 `svg_usage`，是对象且能解析才写入 `info.SvgUsage`。SVG 不支持流式，真正的 SSE 分支不处理。

### 4.2 计费 token

- 输入 = `max(input_tokens, 0)`。
- 输出 = `max(output_tokens, max(total_tokens, 0) − 输入, 0)`。官方没说 `reasoning_tokens` 是否已含在 `output_tokens` 里：含在里面时 `total − input = output`；不含时 `total − input = output + reasoning`。取较大值，两种口径都不会少收。上线前用一条真实请求核对口径。
- 输入和输出都是 0 时视为没有描图用量，不产生描图费用，也不写日志字段。
- 金额全程用 decimal 计算，最后和主费用一起用 `common.QuotaFromDecimalChecked` 换算，超出单次上限时封顶并记录封顶事件（复用现有 `noteQuotaClamp`）。

### 4.3 描图模型怎么定价

规则和用户直接调用这个模型时一样（`ModelPriceHelper` 的优先级）：

1. 计费模式是表达式（`tiered_expr`）且有表达式：用 `billingexpr.ComputeTieredQuota` 跑，参数 `P=输入、C=输出、Len=输入`，不带请求上下文（描图不是客户端直接发起的请求，不套用请求规则里的 `param()`、`header()` 条件）。取 `ActualQuotaBeforeGroup × 分组倍率`。表达式里的 `fixed()` 按一次请求计价。表达式执行失败，或结果是 NaN、无穷大、负数时，这个名字算"未配价"。
2. 配了按次价：`价格 × QuotaPerUnit × 分组倍率`，按一次计。
3. 配了倍率（含系统默认倍率表里的模型）：`(输入 + 输出 × 补全倍率) × 模型倍率 × 分组倍率`。
4. 都没有：这个名字算"未配价"。自用模式下的兜底倍率在这一步不算配价。

分组倍率用这次生图请求的分组倍率（`PriceData.GroupRatioInfo.GroupRatio`），和工具附加费一致，因此成本核算按"费用 ÷ 分组倍率"推出的标价也会包含描图部分。

配置的价格或倍率算出负数时，这个名字算"未配价"；分组倍率为负时描图费用按 0 计。描图费用永远不会让总费用变少。

### 4.4 模型名查找顺序与兜底

1. 先按 `svg_usage.model` 查价格（4.3 的 1–3 步）。
2. 查不到，再按请求里的 `svg.model` 查。这样上游报回带日期的快照名时，也能用客户端请求的模型价格。
3. 两个都查不到：
   - 站点开了自用模式，或该用户勾选了"接受未定价模型"：按系统兜底倍率给 `svg_usage.model` 计价（和直接调用未定价模型一样）。
   - 否则描图部分记 0，参考图照常收费。日志的管理员信息里记 `svg_author_unpriced = <模型名>`，服务端写一条告警。

### 4.5 发上游前预检

`ModelPriceHelper` 开头：请求是 `ImageRequest`，`SvgAuthorModel()` 非空，`HasModelBillingConfig(该模型)` 为假（这个判断已包含自用模式），并且用户没勾选"接受未定价模型"时，返回现有的"模型价格未配置"错误（400，错误里是描图模型的名字），不发上游。透传渠道也生效，因为 `info.Request` 依然是解析后的 `ImageRequest`。

### 4.6 加进总价

- `textQuotaSummary` 新增 `SvgAuthor *svgAuthorCharge`（模型名、实际用来计价的名字、输入、输出、推理 token、金额）。
- `calculateTextQuotaSummary` 在算工具附加费的地方一起算描图费用，三条计价路径都加上：
  - 倍率：加在 `ApplyOtherRatiosToDecimal` 之后，和工具附加费并列。
  - 按次价：同样加在 `ApplyOtherRatiosToDecimal` 之后，所以不会乘图片张数 `n`。
  - 表达式：`composeTieredTextQuota` 把"工具附加费 + 描图费用"作为额外金额加到表达式结果上。
- `hasBillableUsage` 把描图费用算作可计费用量。
- 预扣费不加描图估算：描图用多少 token 要等响应回来才知道，和文本模型的输出 token 一样在结算时补扣。

## 5. 日志与展示

### 5.1 后端

`PostTextConsumeQuota` 在有描图用量时：

- 日志内容追加一行 `common.NewMessage("SVG author {{model}}: {{input}} input tokens, {{output}} output tokens, cost {{quota}}", ...)`，`output` 是计费输出 token，`quota` 用 `logger.FormatQuota` 格式化。
- `other.svg_author`（公开）：`{model, priced_as, input_tokens, output_tokens, reasoning_tokens, quota}`。`priced_as` 是实际用来计价的模型名，未配价时为空串；`quota` 是描图部分四舍五入后的额度，只用于展示。
- 未配价时：`other.admin_info.svg_author_unpriced = <模型名>`，并写服务端告警 `common.LogText("SVG author model %s could not be priced; its tokens were not charged", ...)`，在 `common/log_text.zh-CN.json` 补中文。

### 5.2 新版前端（web/src）

- `LogOtherData` 增加 `svg_author` 类型和 `admin_info.svg_author_unpriced`。
- `lib/format.ts` 新增 `hasSvgAuthorCharge(other)`：`svg_author.quota` 是正数时为真。
- 费用列 `LogCostDisplay`：现有的工具附加费标记改成可传文案、图标和数据属性的本地组件，工具附加费的行为和 `data-tool-surcharge-indicator` 不变；有描图费用时再显示一个"含 SVG 描图费用"标记（图标 `SvgIcon`，`data-svg-author-indicator="true"`）。
- 日志详情弹窗「计费详情」：在图片生成那几行后面加三行：SVG 描图模型、SVG 描图 Token（输入 / 输出，用 `formatNumber` 和 `toIntlLocale` 格式化）、SVG 描图费用（`formatLogQuota`）。
- 经典版不改，只显示日志内容那一行文字。

### 5.3 文案

5 个新键，逐个追加到 7 个语言文件末尾（不跑 `i18n:sync`，它会改写受保护的署名键）：

| 键 | zh |
|---|---|
| `SVG author {{model}}: {{input}} input tokens, {{output}} output tokens, cost {{quota}}` | `SVG 描图模型 {{model}}：输入 {{input}} Token，输出 {{output}} Token，花费 {{quota}}` |
| `SVG author model` | `SVG 描图模型` |
| `SVG author tokens (input / output)` | `SVG 描图 Token（输入 / 输出）` |
| `SVG author cost` | `SVG 描图费用` |
| `Includes SVG authoring cost` | `包含 SVG 描图费用` |

其他语言的译文见实施计划。

## 6. 其他改动

- 内置价格：`setting/billing_setting/builtin_billing.go` 增加 `gpt-image-2.6-sunburst`、`gpt-image-2.6-flare`，表达式和 2.5 相同：`tier("standard", p * 5 + cr * 1.25 + img * 8 + img_cr * 2 + c * 30)`。管理员已配置的倍率或价格仍然优先。
- 被动检测：`sampleImagePayload` 在输出格式是 `svg` 或 `gif` 时不采样。先看响应里的 `output_format`，没有就看请求里的 `output_format`。全局检测抽中时，这类请求会记成"未检测"，不再记成检测错误。
- OpenAI 渠道默认模型列表（`relay/channel/openai/constant.go`）补上 `gpt-image-2`、`gpt-image-2.5-sunburst`、`gpt-image-2.5-flare`、`gpt-image-2.6-sunburst`、`gpt-image-2.6-flare`。

## 7. 测试

后端（testify，表格测试，扩展现有文件）：

| 文件 | 覆盖 |
|---|---|
| `relaykit/dto/openai_request_zero_value_test.go` | 上游的往返测试加入 `safety_identifier`；`SvgAuthorModel()` 的有值、缺省、null、类型不对 |
| `relay/channel/openai/image_edit_test.go` | 上游补丁自带：表单版 edits 转发 `complexity`、mask、markup |
| `relay/convert_request_error_test.go` | 不开透传时 `ImageHelper` 发给上游的请求体带齐 2.6 的字段（含显式 0）；重试时清掉上一次的 `SvgUsage` |
| `relay/channel/openai/image_stream_test.go` | 非流式和"JSON 转流式"两个分支都读出 `svg_usage`；没有 `svg_usage` 时为 nil；SVG、GIF 输出不采样，PNG 照常采样 |
| `relay/helper/price_test.go` | 预检：显式 `svg.model` 未配价返回错误；已配价、自用模式、用户接受未定价时放行；不带 `svg` 时不检查 |
| `service/text_quota_test.go` | 倍率、按次价、表达式三条路径加上描图费用；按次价时不乘 n；描图模型分别用表达式、按次价、倍率计价；先查 `svg_usage.model` 再查请求 `svg.model`；都未配价时记 0；自用模式按兜底倍率；`total − input` 大于 `output` 时按大值计；负数和超大值不产生负费用；消费日志里的 `svg_author`、管理员标记和日志内容行 |
| `setting/billing_setting/builtin_billing_test.go` | 2.6 两个模型的内置表达式和管理员覆盖 |

前端（vitest）：

- `lib/__tests__/tool-surcharge.test.ts` 同目录新增 `svg-author.test.ts`：`hasSvgAuthorCharge` 的正数、0、缺省。
- `components/__tests__/cost-display.test.tsx`：有描图费用时显示带可访问名称的标记；没有时不显示；和工具附加费标记同时出现。
- `components/__tests__/usage-facts.test.tsx`：计费详情里出现三行描图信息，没有 `svg_author` 时不出现。

整体验证：`go build ./...`、受影响包的 `go test`、`relaykit` 独立编译和测试、`bun run typecheck`、改动文件的 lint、受影响的 vitest、`bun run build`（GoLand 运行的程序嵌入 `web/dist`，不重新构建看不到前端改动）。

## 8. 上线

1. 合并后打新 tag，4 个实例依次升级。
2. 管理员给允许使用的描图模型配好价格。没配价的描图模型：客户端显式指定时会被拒绝；由 OpenAI 默认选择时描图部分记 0，并在日志管理员信息里留下模型名。上线后先查一遍这类日志。
3. 用一条真实的 SVG 请求核对 `reasoning_tokens` 口径和日志展示。这会产生少量费用，执行前要征得同意。
4. 2.6 的定价建议用内置表达式（按 token）。按次价收不到 GIF 帧数和 SVG 描图带来的额外 token 成本（描图费用本身仍会单独计）。
5. 发版前的临时办法：可以给只服务图片模型的渠道单独开透传，让 SVG 先能用，但描图 token 不会计费，其他参数也不再经过模型映射和参数覆盖。不建议在混用渠道上开。

## 9. 风险与待验证

- `reasoning_tokens` 口径未经真实响应验证，按 4.2 的较大值规则兜底。
- 描图模型未配价且未被预检拦住时（客户端没带 `svg.model`）会少收描图费用，靠管理员告警发现。
- 上游以后如果也加 `safety_identifier`，合并时会有一行冲突，保留一份即可。
