# Seedance 限时折扣计费层 + 新模型接入 Skill 设计

日期:2026-09-11
范围:`relay/channel/task/seedance` 计费矩阵(三个渠道类型共用:`ChannelTypeVolcEngine` 45 / `ChannelTypeDoubaoVideo` 54 / `ChannelTypeSeedance3rd` 59)

---

## Context(为什么做这件事)

两个诉求:

1. **Seedance 2.5 需要 1080p 档位与限时折扣**。矩阵里 `doubao-seedance-2-5-260628`(`relay/channel/task/seedance/pricing.go:55`)与 `dreamina-seedance-2-5-260628`(`:36`)已存在,base 档单价(国内 70/42、海外 10.7/6.4)与官方一致,但两者都只有 base 档,且代码注释明确写着「2.5 仅支持输出 480p/720p(无 1080p/4k 档)」(`pricing.go:35`、`:53`)。新官方定价给出了 1080p 档,并对该档位施加限时折扣(国内 72折 / 海外 28% off,系数均为 0.72),与现有注释冲突。

2. **每次出新模型都要改代码,希望固化成 Skill**。当前接入一个新 seedance 模型需要人工判断改哪几处,容易漏。目标是让后续接入退化为「给模型名 + 价格」的表格编辑。

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

**折扣天然属于「单元格单价」这一层**,落在矩阵内即自动贯穿预扣与结算,计费管线一行不动。

最终费用公式不变:
`USD/元 = completion_tokens × modelRatio × groupRatio × video_pricing ÷ QuotaPerUnit`

---

## 一、数据模型

矩阵从裸 map 升级为带结构的类型:

```go
type modelPricing struct {
    tiers map[string]map[bool]float64 // 原价:档位 → 是否含视频 → 单价(每百万 token)
    promo *promo                      // 可选,限时折扣层
}

type promo struct {
    tiers   []string // 作用档位,如 ["1080p"] 或 ["base"]
    factor  float64  // 0.72 = 72折 / 28% off
    expires string   // "2026-12-31";空串 = 不设到期
}
```

**关键约束:`tiers` 里存原价,折扣永不预乘进数字。** 这样「原价 / 折扣 / 实收」三者始终可分离,账单能拆,下次调价也只改一处。

`promo` 不限模型、不限档位——2.0 / fast / mini / 2.5 及后续新型号均可挂,作用档位由 `promo.tiers` 指定。

### 到期判定

按模型名前缀分流时区:

| 前缀 | 时区 | 含义 |
|---|---|---|
| `doubao-*` | `Asia/Shanghai` | 国内火山方舟 |
| `dreamina-*` | `UTC` | 海外 BytePlus |

`expires` 当天 23:59:59 前仍享折扣,过期自动回原价。同写 `"2026-12-31"` 时两者相差 8 小时,这是刻意的:各自贴合所在市场的官方口径。

时钟需可注入,便于测试不依赖真实时间。

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

国内 72折 与海外 28% off 是同一系数 0.72。矩阵填**原价**,折扣走 `promo{tiers: ["1080p"], factor: 0.72}`。

`pricing.go:35`、`:53` 的「无 1080p 档」注释随之更正。

### fast / mini 的 base 档折扣

`doubao-seedance-2-0-fast-260128`、`doubao-seedance-2-0-mini-260615` 及对应 `dreamina-*` 挂 base 档折扣(`promo{tiers: ["base"], ...}`)。

**待补数据(4 组,官方文档中没有,需运营提供)**:每个模型的 `factor` 与 `expires`。

**同时需确认**:矩阵现有的 37/22(fast)、23/14(mini)是原价还是折后价。若为折后价,须先还原为原价再挂 promo,否则重复打折。若为原价,直接加 promo 层即可。此项确认前不动这四个模型的数字。

两地官方文档均写明 fast/mini 不支持 1080p 输出(`接口文档/海外byteplus-seedance文档/模型计费定价.md:117-118`、`接口文档/seedance2视频文档/Seedance 2.0 计费规则说明.md:34,39`),因此**不为其编造 1080p 价格**,仅保留 base 档。

---

## 三、单价求值

`CellUnit` 两项行为变更:

### 1. 显式返回实际计价档位

新增返回值 `tierHit string`。调用方据此把日志的 `video_resolution_tier` 改为记录**真实计价档位**而非请求档位。1080p 请求落到 base 计价时,账单如实显示 base,不再自欺。

`CellUnit` 与 `PricingRatio` 当前均为三返回值(`unit, base, ok` / `ratio, base, ok`),加 `tierHit` 后变四返回值,调用方需同步:`ResolveVideoBilling`(`billing.go:37`)是唯一生产调用点,其余为测试(`pricing_test.go:72`、`:119`)。

### 2. 折扣在返回单价前施加

仅当当前档位命中 `promo.tiers` 且未过期时施加。

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
| `PromoExpires` | 到期时间;空串表示不设 |

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

## 五、测试

现有测试编码了「2.5 无 1080p」的旧假设,必须随本次变更更新:

| 位置 | 现断言 | 变更后 |
|---|---|---|
| `pricing_test.go:68` | `dreamina` 2.5 `1080p` 含视频 → 6.4(回退 base) | 7.0 原价 / 5.04 折后 |
| `pricing_test.go:114` | `doubao` 2.5 `1080p` 不含视频 → 倍率 1.0 | 77/70 原价 → 折后 0.72×77/70 |
| `pricing_test.go:115` | `doubao` 2.5 `1080p` 含视频 → 42/70 | 46/70 原价 → 折后 0.72×46/70 |

`:65`、`:69`、`:111`、`:116` 的 `4k` 断言与注释同样声明「2.5 不支持 1080p/4k」,4k 行为不变(仍回退 base)但注释需更正。fast/mini 挂 base 折扣后,`:61-64`、`:105-110` 的期望值随之改变——具体数值取决于待补的 factor,以及 37/22、23/14 是原价还是折后价的确认结果。

新增覆盖:

- 1080p 原价与折后价(国内、海外各一组)
- 折扣到期前后各一组,两个时区分别验证(`doubao-*` 东八区、`dreamina-*` UTC)
- **折扣挂 base 档时倍率不被分母抵消** —— 对应第三节的分母陷阱
- fast/mini 未配 1080p 时 `tierHit` 如实返回 `base`
- `expires` 为空串时折扣长期有效
- 恒等式 `PricingRatio == ListPricingRatio × PromoFactor` 在打折与未打折两种情形下均成立
- 未打折模型 `PromoFactor == 1` 且 `ListPricingRatio == PricingRatio`(保证既有模型行为零变化)

---

## 六、新模型接入 Skill

上述结构落地后,接入新 seedance 模型退化为三步表格编辑:

1. `relay/channel/task/seedance/pricing.go` 矩阵加模型条目(原价 + 可选 promo)
2. `relay/channel/task/{doubao,seedance3rd}/constants.go` 模型列表加名字
3. `dreamina-*` 另加默认倍率(`setting/ratio_setting/model_ratio.go:283`,值 = USD单价 ÷ 2);`doubao-*` 刻意不设默认值,由管理员按基准单价配置为**倍率而非固定价**(否则 `PerCallBilling` 会跳过 token 阶梯结算,见 `model_ratio.go:287-291`)

Skill 把这三步固化为检查单,并附带易踩空的约束:

- 折扣填原价 + promo 层,**不填折后价**
- fast/mini 类精简型号通常无 1080p/4k 档,不要凭空补价
- 改矩阵必须同步更新 `pricing_test.go`
- 新增档位要确认 `ClassifyResTier`(`pricing.go:68`)能识别该分辨率字符串
- 折扣挂 base 档时检查分母未被打折

**实施顺序:先完成本次改造,再写 Skill。** Skill 应描述一个已验证过的流程,而非设想的流程。

---

## 不做(YAGNI)

- 不把视频计费迁到 `pkg/billingexpr` 表达式系统。两套系统当前互不相通(任务路径的 `ModelPriceHelperPerCall` 无 `GetBillingMode` 分支;`BuildTieredTokenParams` 只读 `dto.Usage`,视频任务不产生 Usage),迁移需新增 resolution/duration 变量、改编译原型与结算入参,远超本次范围。
- 不引入后台可视化折扣编辑器。折扣由官方定价驱动,随代码发布即可。
- 不改 duration/fps/watermark 的计费参与方式——现状是通过 token 数隐式生效,本次不动。
