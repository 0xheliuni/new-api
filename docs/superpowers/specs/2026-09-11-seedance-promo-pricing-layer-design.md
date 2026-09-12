# Seedance 限时折扣计费层 + 新模型接入 Skill 设计

日期:2026-09-11
范围:`relay/channel/task/seedance` 计费矩阵(三个渠道类型共用:`ChannelTypeVolcEngine` 45 / `ChannelTypeDoubaoVideo` 54 / `ChannelTypeSeedance3rd` 59)

---

## Context(为什么做这件事)

两个诉求:

1. **Seedance 2.5 需要 1080p 档位与限时折扣**。矩阵里 `doubao-seedance-2-5-260628`(`relay/channel/task/seedance/pricing.go:55`)与 `dreamina-seedance-2-5-260628`(`:36`)已存在,base 档单价(国内 70/42、海外 10.7/6.4)与官方一致,但两者都只有 base 档,且代码注释明确写着「2.5 仅支持输出 480p/720p(无 1080p/4k 档)」(`pricing.go:35`、`:53`)。新官方定价给出了 1080p 档,并对该档位施加限时折扣(国内 72折 / 海外 28% off,系数均为 0.72),与现有注释冲突。

2. **每次出新模型都要改代码,希望固化成 Skill**。当前接入一个新 seedance 模型需要人工判断改哪几处,容易漏。目标是让后续接入退化为「给模型名 + 价格」的表格编辑。

3. **折扣与到期时间必须可自由配置**。限时折扣是商务决策,变更节奏远快于发版:改一个系数或延一次期限不应该需要开发介入。因此折扣不写死在代码里,而是后台可编辑的配置。

### 一个现存缺陷,正好是折扣可审计性的障碍

模型没有某档位时,单价静默回退到 base(`pricing.go:87-90`):

```go
cell, has := tiers[tier]
if !has {
    cell = tiers["base"]
}
```

但日志照旧记录请求档位(`service/task_billing.go:52` `other["video_resolution_tier"] = vb.ResolutionTier`)。结果是**账单显示 1080p、实际按 480p/720p 价扣费**。折扣一旦引入,这种偏差会让「原价 × 折扣 = 实收」在账单上算不平,因此本次一并修正。

---

## 计费链路(沿用现有,不改)

矩阵是单一事实来源,三个渠道类型共用,因此「修改所有 seedance 渠道类型」只需改这一处:

- `relay/channel/task/seedance/pricing.go:96` `PricingRatio` 算出 `video_pricing = 单元格单价 ÷ 基准单价`
- `relay/channel/task/seedance/billing.go:15` `EstimateBilling` 注入 `OtherRatios{"video_pricing": ratio}`
- `relay/relay_task.go:197` 预扣阶段乘进额度
- `service/task_billing.go:364` 结算阶段用同一快照系数重算

**折扣天然属于「单元格单价」这一层**,落在该层即自动贯穿预扣与结算,计费管线一行不动。

### 异步任务的折扣一致性(配置化带来的新要求)

视频任务是异步的:提交时预扣,轮询到终态才结算。折扣一旦可随时修改,就必须回答「任务执行期间运营改了折扣,按哪个价结算」。

答案是**按提交时的折扣结算**,且现有机制已经保证这一点:`video_pricing` 在提交时写入 `TaskBillingContext.OtherRatios`(`model/task.go:142`),随 `PrivateData` 持久化到 `tasks.private_data` 列(`model/task.go:85`);结算阶段只读这份快照的乘积(`task_billing.go:354-361`),不重新查配置。

因此本次实现有一条硬约束:**结算路径不得重新读取折扣配置**。用户按下单时看到的价格付费,中途调价不影响在途任务——这既是正确的商业语义,也避免了对账争议。需为此补一条回归测试。

最终费用公式不变:
`USD/元 = completion_tokens × modelRatio × groupRatio × video_pricing ÷ QuotaPerUnit`

---

## 一、数据模型:原价在代码,折扣在配置

两类数据的变更节奏完全不同,因此分置:

| 数据 | 位置 | 谁改 | 为何 |
|---|---|---|---|
| **原价矩阵**(档位 × 含视频) | 代码 `pricing.go` | 开发,随上游公告 | 官方定价,新档位往往同时需要 `ClassifyResTier` 支持,天然伴随发版 |
| **限时折扣**(系数 + 到期) | 后台配置 | 运营,随时 | 商务决策,需要自由调整,不应为改一个数字发版 |

### 原价矩阵

保留代码内定义,仅去掉折扣相关的假设:

```go
// 原价:档位 → 是否含视频 → 单价(每百万 token)
var unitPrice = map[string]map[string]map[bool]float64{ ... }
```

**关键约束:矩阵里存原价,折后价永不写入。** 折扣一律来自配置层,「原价 / 折扣 / 实收」因此始终可分离。

### 折扣配置

新建 `setting/billing_setting/video_promo.go`,沿用本仓库唯一的配置注册模式(对照 `tiered_billing.go:31`):

```go
// DB key: billing_setting.video_promo
type VideoPromo struct {
    Tiers     []string `json:"tiers"`      // 作用档位,如 ["1080p"] 或 ["base"]
    Factor    float64  `json:"factor"`     // 0.72 = 72折 / 28% off
    ExpiresAt int64    `json:"expires_at"` // Unix 秒;0 = 不设到期
}

type BillingSetting struct {
    // ...existing fields...
    VideoPromo map[string]VideoPromo `json:"video_promo"` // 模型名 → 折扣
}
```

挂在既有 `billing_setting` 之下而非新建 package:该配置已在 `model/option.go:622-634` 的写后钩子里绑定了 `InvalidatePricingCache()` 与 `ratio_setting.InvalidateExposedDataCache()`,复用即自动获得缓存失效。

`setting/config/config.go:146-152`(写)与 `:255-268`(读)对任意嵌套结构走 `json.Marshal`/`Unmarshal`,故整张嵌套 map 存于单个 DB key,无需额外序列化代码。`reflect.Map` 字段更新时会重建新 map(`config.go:255-263`),删除的模型键能被正确清除。

读访问器返回 `lo.Assign` 副本,与 `GetBillingModeCopy`(`tiered_billing.go:50`)一致,热路径不共享 map。

### 到期判定

`ExpiresAt` 存 **Unix 秒**,与 `common.GetTimestamp()` 比较。语义为闭区间起点的镜像:`ExpiresAt == 0` 表示长期有效,否则 `now <= ExpiresAt` 时折扣生效。

沿用 `model/channel_cost_version.go:16-22` 的既有先例(`EffectiveFrom int64`,管理员可设的生效时刻,过点即改变计费行为)。选时间戳而非日期字符串的理由:**时间戳不带时区**,管理员在日期选择器里挑的就是一个确定时刻,不存在「国内按东八区、海外按 UTC」的规则需要向客服解释,也不会出现同一日期字符串在两个模型上相差 8 小时。前端负责把本地时间转成时间戳提交。

时钟通过参数注入,测试不依赖真实时间。

---

## 二、2.5 的 1080p 档位与折扣

| 模型 | 档位 | 不含视频 | 含视频 |
|---|---|---|---|
| `doubao-seedance-2-5-260628` | 480p/720p (base) | 70.00 | 42.00 |
| | 1080p 原价 | 77.00 | 46.00 |
| | 1080p 折后(×0.72) | 55.44 | 33.12 |
| `dreamina-seedance-2-5-260628` | 480p/720p (base) | 10.70 | 6.40 |
| | 1080p 原价 | 11.70 | 7.00 |
| | 1080p 折后(×0.72) | 8.424 | 5.04 |

国内 72折 与海外 28% off 是同一系数 0.72。**矩阵只填原价那两行**,折后价由配置产生,不写进代码。

`pricing.go:35`、`:53` 的「无 1080p 档」注释随之更正。

### 0.72 是随迁移写入的初始配置,不是硬编码

两个 2.5 模型的 `{tiers: ["1080p"], factor: 0.72, expires_at: <你指定>}` 由一次性迁移写入配置表,此后完全由你在后台调整。代码里不留 0.72 这个数字。

`expires_at` 需要你给一个初始值,或留 0 表示暂不设到期、后续在后台补。

### fast / mini

矩阵只保留 base 档:两地官方文档均写明 fast/mini 不支持 1080p 输出(`接口文档/海外byteplus-seedance文档/模型计费定价.md:117-118`、`接口文档/seedance2视频文档/Seedance 2.0 计费规则说明.md:34,39`),不为其编造 1080p 价格。

折扣不再需要我预先填数——你在后台给这四个模型配 `{tiers: ["base"], factor, expires_at}` 即可,原先待补的 4 组数据因此不再阻塞实施。

**但有一项仍需你确认**:矩阵现有的 37/22(fast)、23/14(mini)究竟是原价还是折后价。这是数据卫生问题,配置化无法回避——若它们已是折后价,你在后台再配折扣就会重复打折。若为折后价需先还原为原价。确认前不动这四个数字。

---

## 三、单价求值

`CellUnit` 两项行为变更:

### 1. 显式返回实际计价档位

新增返回值 `tierHit string`。调用方据此把日志的 `video_resolution_tier` 改为记录**真实计价档位**而非请求档位。1080p 请求落到 base 计价时,账单如实显示 base,不再自欺。

`CellUnit` 与 `PricingRatio` 当前均为三返回值(`unit, base, ok` / `ratio, base, ok`),加 `tierHit` 后变四返回值,调用方需同步:`ResolveVideoBilling`(`billing.go:37`)是唯一生产调用点,其余为测试(`pricing_test.go:72`、`:119`)。

### 2. 折扣在返回单价前施加

从配置读取该模型的折扣,仅当**实际计价档位**(即 `tierHit`,而非请求档位)命中 `Tiers` 且未过期时施加。

用 `tierHit` 而非请求档位是必要的:1080p 请求在未配 1080p 的模型上回退到 base 计价,此时若按请求档位判断,会把 1080p 的折扣错误地施加到 base 单价上。

### 配置健壮性

配置由人手填,坏值必须安全降级而非放大扣费或崩溃:

| 情形 | 处理 |
|---|---|
| `Factor <= 0` 或 `> 1` | 忽略折扣,按原价计;记 warning |
| `Tiers` 为空 | 不作用于任何档位(等同无折扣) |
| 模型名未出现在配置中 | 无折扣,原价 |
| 已过期 | 无折扣,原价 |

保存前做校验(参照 `controller/option.go:306-341` 的 `case` 分支模式,这是本仓库唯一的存前校验机制):`Factor` 落在 `(0, 1]`,`Tiers` 元素为已知档位字符串,`ExpiresAt >= 0`。后台直接拒绝坏值,运行时的降级只是兜底。

### 分母陷阱(必须避开)

`PricingRatio` 的相对倍率语义不变(单元格 ÷ 基准),币种依旧对扣费无影响,折扣自动包含在分子里。

但**基准单价 `tiers["base"][false]` 作分母时不施加折扣**。若折扣挂在 base 档而分子分母同时打折,倍率不变、折扣完全丢失。分母必须固定取原价基准,折扣才能真正体现在倍率上——这是 fast/mini 挂 base 档折扣时的正确性前提。

---

## 四、账单与日志

`types.VideoBillingDisplay`(`types/price_data.go:31`)新增三字段:

| 字段 | 含义 |
|---|---|
| `ListPricingRatio` | 未打折的相对基准倍率 |
| `PromoFactor` | 折扣系数;未打折为 1 |
| `PromoExpiresAt` | 到期 Unix 秒;0 表示不设 |

**为何存倍率而非单价**:现有 `video_unit_price` 由 `ModelRatio × 2 × PricingRatio` 导出(`task_billing.go:55`),即实际单价随管理员配置的 `ModelRatio` 缩放。`PricingRatio` 打折后该式自动得到**实收价**,无需改动。但原价必须同样经 `ModelRatio` 缩放才可与实收价并列比较,故存未打折倍率 `ListPricingRatio`,原价按 `ModelRatio × 2 × ListPricingRatio` 导出。若直接存矩阵里的绝对数字(如 77.00),管理员改价后原价与实收价会不同步,账单上「原价 × 折扣 = 实收」算不平。

恒等式:`PricingRatio == ListPricingRatio × PromoFactor`。可在测试中作为断言。

`service/task_billing.go` 两处写日志处(`:51-59` 预扣、`:147-160` 结算)同步追加:

```
video_resolution_tier  = "1080p"   // 真实计价档位
video_list_unit_price  = 77.00     // 原价
video_promo_factor     = 0.72      // 限时72折
video_unit_price       = 55.44     // 实收
```

前端详情弹窗(`web/default/src/features/usage-logs/components/dialogs/details-dialog.tsx:406-433`)已在展示 `video_unit_price` 与档位,按同样模式补原价与折扣两行;类型定义在 `types.ts:201-204`、`data/schema.ts:36-37`。

未打折时(`PromoFactor == 1`)不渲染折扣行,避免给全价请求增加噪音。

---

## 五、后台折扣编辑界面

复用既有模型定价表,不新建页面:折扣是模型的属性,和倍率、计费模式同属一处心智模型。

- 每模型一行的表格在 `web/default/src/features/system-settings/models/model-ratio-visual-editor.tsx`,列定义在 `model-ratio-table-columns.tsx`
- 逐模型编辑在 `model-pricing-sheet.tsx`;其中 `tiered-pricing-editor.tsx`(`model-pricing-sheet.tsx:640`)已证明嵌套的档位列表能在该抽屉内编辑,折扣编辑器沿用同一位置与形态
- 保存需在 `ratio-settings-card.tsx:315-318` 的 `apiKeyMap` 中登记 `billing_setting.video_promo`;该文件已有按字段脏检查(`:320-334`),未改动的键不会被写入

编辑内容三项:作用档位(多选)、折扣系数、到期时刻(日期时间选择器,提交为 Unix 秒;`components/ui/calendar.tsx` 已存在但当前未在系统设置中使用)。

界面上直接显示折后价预览,避免运营心算。

本仓库没有通用嵌套表单渲染器,每个嵌套配置都是手写编辑器,且均配 JSON 模式作为兜底(最佳参照:`features/system-settings/general/channel-affinity/index.tsx`,visual/json 双模式在 `:146`、`:541`)。折扣编辑器同样提供 JSON 模式,以便批量配置多个模型。

---

## 六、测试

现有测试编码了「2.5 无 1080p」的旧假设,必须随本次变更更新:

| 位置 | 现断言 | 变更后 |
|---|---|---|
| `pricing_test.go:68` | `dreamina` 2.5 `1080p` 含视频 → 6.4(回退 base) | 7.0 原价 / 5.04 折后 |
| `pricing_test.go:114` | `doubao` 2.5 `1080p` 不含视频 → 倍率 1.0 | 77/70 原价 → 折后 0.72×77/70 |
| `pricing_test.go:115` | `doubao` 2.5 `1080p` 含视频 → 42/70 | 46/70 原价 → 折后 0.72×46/70 |

`:65`、`:69`、`:111`、`:116` 的 `4k` 断言与注释同样声明「2.5 不支持 1080p/4k」,4k 行为不变(仍回退 base)但注释需更正。fast/mini 挂 base 折扣后,`:61-64`、`:105-110` 的期望值随之改变——具体数值取决于待补的 factor,以及 37/22、23/14 是原价还是折后价的确认结果。

`relay/channel/task/sora/seedance2_test.go` 是第二个受影响的测试文件(此前遗漏)。`:85-95` 的 `TestEstimateSeedance2Ratios_Mini1080pNoResolutionRatio` 断言 mini 请求 1080p 时倍率仍为 14/23,该行为本身不变(mini 确实无 1080p 档),但注释与断言需随 `tierHit` 变更复核;`:64`、`:73`、`:91` 的 14/23 期望值若 fast/mini 的原价确认结果为「需还原」,则同样要改。

新增覆盖:

- 1080p 原价与折后价(国内、海外各一组)
- 折扣到期前后各一组(注入固定时钟,不依赖真实时间)
- **在途任务不受调价影响**:提交后修改配置,结算仍按快照折扣计费 —— 对应第二节的异步一致性约束
- 坏配置降级:`Factor` 为 0 / 负数 / 大于 1 / `Tiers` 为空,均按原价计且不 panic
- 折扣按 `tierHit` 而非请求档位判断:1080p 请求落到 base 计价时,1080p 折扣不生效
- **折扣挂 base 档时倍率不被分母抵消** —— 对应第三节的分母陷阱
- fast/mini 未配 1080p 时 `tierHit` 如实返回 `base`
- `expires` 为空串时折扣长期有效
- 恒等式 `PricingRatio == ListPricingRatio × PromoFactor` 在打折与未打折两种情形下均成立
- 未打折模型 `PromoFactor == 1` 且 `ListPricingRatio == PricingRatio`(保证既有模型行为零变化)

---

## 七、新模型接入 Skill

折扣配置化后,Skill 的范围缩小了——**折扣不再需要改代码**,新模型接入只剩原价与模型名注册:

1. `relay/channel/task/seedance/pricing.go` 原价矩阵加模型条目
2. `relay/channel/task/{doubao,seedance3rd}/constants.go` 模型列表加名字
3. `dreamina-*` 另加默认倍率(`setting/ratio_setting/model_ratio.go:283`,值 = USD单价 ÷ 2);`doubao-*` 刻意不设默认值,由管理员按基准单价配置为**倍率而非固定价**(否则 `PerCallBilling` 会跳过 token 阶梯结算,见 `model_ratio.go:287-291`)
4. 有限时折扣则在后台配,不动代码

于是「给模型名 + 价格就能接入」基本成立:代码改动是三处机械编辑,商务参数走配置。

Skill 固化这几步为检查单,并附带易踩空的约束:

- 矩阵只填**原价**,折后价一律走后台配置
- fast/mini 类精简型号通常无 1080p/4k 档,不要凭空补价——查官方文档确认
- 改矩阵必须同步更新 `pricing_test.go` **与** `sora/seedance2_test.go`(两个文件都断言倍率)
- 新增档位要确认 `ClassifyResTier`(`pricing.go:68`)能识别该分辨率字符串
- 折扣作用于 base 档时检查分母未被打折(第三节的分母陷阱)
- 新模型名需同时确认走的是 `doubao-*` 还是 `dreamina-*` 前缀,两者默认倍率处理方式不同

**实施顺序:先完成本次改造,再写 Skill。** Skill 应描述一个已验证过的流程,而非设想的流程。

---

## 不做(YAGNI)

- 不把视频计费迁到 `pkg/billingexpr` 表达式系统。两套系统当前互不相通(任务路径的 `ModelPriceHelperPerCall` 无 `GetBillingMode` 分支;`BuildTieredTokenParams` 只读 `dto.Usage`,视频任务不产生 Usage),迁移需新增 resolution/duration 变量、改编译原型与结算入参,远超本次范围。
- 不把**原价矩阵**也搬进后台。原价随上游公告变动,且新档位往往需要 `ClassifyResTier` 同步支持,本就伴随发版;搬进配置只会让代码与配置对同一事实各存一份。可配置的边界划在折扣。
- 不做折扣的生效起始时间(只做到期)。当前诉求是「限时优惠何时结束」;需要预约未来生效时再加,`channel_cost_version.go` 已有 `EffectiveFrom` 先例可循。
- 不做按分组/令牌差异化折扣。分组倍率已是独立乘子,叠加会让账单更难解释。
- 不改 duration/fps/watermark 的计费参与方式——现状是通过 token 数隐式生效,本次不动。
