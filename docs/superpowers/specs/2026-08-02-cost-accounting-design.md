# 成本核算（Cost Accounting）功能设计方案

- 日期：2026-08-02（v2，按用户口径修订）
- 状态：待评审
- 权限级别：仅超级管理员（Root，role >= 100）
- 范围：后端（Go）+ 新前端（web/default）+ 老前端（web/classic）

---

## 1. 目标与口径（用户已确认）

在管理员控制台新增「成本核算」页面，超级管理员按时间范围查看**用户 / 模型 / 供应商（=渠道）三个维度**的收入、成本与利润：

1. **收入 = 折后美元**：用户实际被扣的额度（已含分组专属倍率折扣，如 0.8 = 刊例价 8 折）折算成 USD。
2. **成本 = 模型刊例价 × 渠道倍率**：上游与我方标价一致（同一模型刊例价），但上游按「CNY:USD 倍率」收款（如 2.5 → 每消耗刊例 $1 记成本 ¥2.50）。**倍率在每个渠道上单独填写**。
3. **利润口径（已确认）**：收入折成 CNY 再相减 —— `利润¥ = 收入$ × 售卖汇率 − 刊例$ × 渠道倍率`；报表同时展示收入 $ 原值。
4. **供应商 = 渠道（已确认）**：不建独立供应商表；「供应商维度」即按渠道聚合。聚合系统类渠道（自建 new-api 等）在二期通过其 API 下钻背后各家供应商（各自倍率/成本/可用性/缓存/tokens）。
5. **seedance 等异步视频任务的退款必须计入**（预扣 → 结算/退款差额，收入与成本同步冲减）。

## 2. 现状盘点（探索结论）

| 事实 | 位置 |
|---|---|
| 日志表 `logs` 含 `user_id / channel_id / model_name / quota / prompt_tokens / completion_tokens / type / created_at`，索引含 `idx_created_at_type`、keyset 导出索引 | `model/log.go:35-77` |
| 额度换算：`USD = quota / common.QuotaPerUnit`（默认 500000）；CNY = USD × `operation_setting.USDExchangeRate` | `common/constants.go:62`、`controller/bill_summary_query.go:60,125-133` |
| **日志 `other` JSON 携带完整定价信息**：`model_ratio / model_price / group_ratio / user_group_ratio / completion_ratio / cache_* / billing_mode / expr_b64` 等，可据此重算刊例价 | `controller/log_export.go:66-101` |
| **`billSummaryAgg` 已实现本功能所需引擎的 90%**：流式扫描日志（keyset 分页）、按（时间桶 × 用户 × channel × 模型）分桶、从 `other` 重算**刊例价 vs 实付价**、`LogTypeRefund` 净额冲减、USD/CNY 换算 | `controller/bill_summary.go:46-148` |
| seedance 退款是一等公民：失败退款与结算差额写 `LogTypeRefund(6)` 行（`Quota` 正数），`other.billing_stage ∈ {pre_consume, settle, refund}`；导出/聚合流**不**隐藏这些行 | `service/task_billing.go:180-295`、`model/log.go:400-403,548-552` |
| 渠道已有类型化 JSON 设置 `dto.ChannelSettings`（经 `GetSetting/SetSetting` 与 `UpdateChannel` 持久化），新增字段零迁移 | `dto/channel_settings.go`、`model/channel.go:940-971` |
| logs 可能位于独立 `LOG_DB`，不能与 channels JOIN，需应用层映射（已有 `populateLogChannelNames` 先例） | `model/log.go:843-879` |
| Root 权限中间件 `middleware.RootAuth()` 已有成熟用法（`/api/option` 组） | `middleware/auth.go:192`、`router/api-router.go:186-201` |
| 新前端：TanStack Router/Table + Base UI + Tailwind v4 + VChart；`ROLE.SUPER_ADMIN = 100` | `web/default/src/lib/roles.ts` 等 |
| 老前端：Semi Design + VChart；`isRoot()` = role>=100，但只有 `AdminRoute`（>=10），需新增 RootRoute | `web/classic/src/helpers/auth.jsx` |

## 3. 核心计费模型

对每条日志（`type IN (2 Consume, 6 Refund)`，Refund 取负号净额）：

```
实付额度  actual_quota  = 日志 quota（已含分组折扣，即折后价）
刊例额度  list_quota    = 由 other JSON 定价字段重算（去除 group_ratio / user_group_ratio 折扣）
                          —— 复用 billSummaryAgg 现成的 list-vs-actual 重算逻辑

按（user_id × model_name × channel_id × 日）分桶累加后：

收入$   revenue_usd = Σ net_actual_quota / QuotaPerUnit        （折后实收美元）
刊例$   list_usd    = Σ net_list_quota   / QuotaPerUnit        （上游计费基数）
收入¥   revenue_cny = revenue_usd × USDExchangeRate            （现有全局售卖汇率）
成本¥   cost_cny    = list_usd × channel.cost_ratio            （渠道倍率 CNY:USD）
利润¥   profit_cny  = revenue_cny − cost_cny
利润率  profit_rate = profit_cny / revenue_cny（收入为 0 时置 0）
```

- **seedance 退款**：预扣 100 → 全额退款 → 净额 0；预扣 100 → 结算补扣 20 → 净额 120；预扣 100 → 结算退回 30 → 净额 70。`type=6` 净额公式同时作用于实付与刊例两条线，收入与成本同步冲减。
- **未填倍率的渠道**：`cost_ratio` 缺省 0 → 成本按 0 计，报表行黄色警示「未填倍率」，页面顶部汇总提示条列出未填倍率渠道数，避免利润虚高被误读。
- **缓存计费**：`other` 中的 cache ratio 字段参与刊例价重算（billSummary 引擎已处理），口径与上游一致。

## 4. 备选方案对比

### 4.1 渠道倍率存哪里

| 方案 | 说明 | 结论 |
|---|---|---|
| **A. `dto.ChannelSettings` 新增 `CostRatio float64`（推荐）** | 走现有 `Setting` JSON 列与 `GetSetting/SetSetting/UpdateChannel` 链路，零表迁移；渠道编辑页天然有落点；成本页可内联快捷编辑 | ✅ 采用 |
| B. channels 表加 `cost_ratio` 物理列 | 也可行，但收益仅是 SQL 可查——而映射本来就在应用层做（跨库无法 JOIN），无必要 | ❌ |
| C. 独立 root-only 倍率表 | 仅当要求倍率对普通管理员保密时才需要（见 §9 待确认项 3） | 备选 |

### 4.2 聚合引擎

| 方案 | 说明 | 结论 |
|---|---|---|
| **A. 复用/抽取 `billSummaryAgg` 流式引擎（推荐）** | 刊例价必须从 `other` JSON 逐行重算，纯 SQL 做不到（跨三库无 JSON 函数可用）；该引擎已具备 keyset 流扫、四维分桶、退款净额、刊例/实付双线 —— 抽出公共聚合核，成本报表只是换一组输出列 | ✅ 采用 |
| B. 纯 SQL GROUP BY | 快，但只能得到实付口径，拿不到刊例价 → 成本算不出 | ❌ |
| C. 新建小时级汇总表（含刊例价列） | 查询最快，但需双写/回补；一期先不做 | 🔜 数据量大后的优化，API 响应结构不变仅换数据源 |

## 5. 后端方案

### 5.1 数据模型

**渠道倍率**（零迁移）：

```go
// dto/channel_settings.go 追加
CostRatio float64 `json:"cost_ratio,omitempty"` // 成本倍率 CNY:USD，0/缺省=未填写
```

- 写入路径：现有 `PUT /api/channel`（渠道编辑）即可；成本页提供快捷编辑入口调同一接口。
- 聚合系统类渠道同样在渠道上填倍率（作为该渠道整体的兜底口径）；其背后各家供应商的独立倍率与真实成本属于二期，由聚合系统 API 返回（见 §5.4）。

**二期预留表 `channel_upstream_snapshots`**（本期只定义不实现）：聚合系统回传的下钻数据 —— `channel_id / sub_supplier_name / start_ts / end_ts / usage_usd / cost_cny / cost_ratio / availability / cache_tokens / prompt_tokens / completion_tokens / raw(json) / synced_at`。

### 5.2 聚合引擎（service/cost_stat.go）

1. 从 `controller/bill_summary.go` 抽出可复用的流式聚合核（扫描 `GetAllLogsForExport` 流、解析 `other` 定价、算刊例/实付净额），成本报表与账单汇总共用，避免两份定价重算逻辑漂移。
2. 分桶键：`(user_id, username, model_name, channel_id, day)`，day = `created_at - created_at % 86400`（应用层分桶，规避三库日期函数差异）。
3. 内存立方体（cube）聚合完成后，应用层完成：channel → 渠道名/倍率映射（主库一次性载入，对齐 `populateLogChannelNames`）、三个维度的收敛（对 cube 分别按 user / model / channel 折叠）、排序与分页切片。
4. 性能护栏：时间跨度上限（默认 370 天）、行数上限沿用 `LogExportMaxRows` 模式、结果 60 秒缓存（key = 参数哈希，Redis 优先、退化内存），保证"实时"又防重复重查。

### 5.3 API 设计（全部挂 `middleware.RootAuth()`）

`router/api-router.go` 新增路由组 `/api/cost`：

```
GET /api/cost/overview?start_timestamp&end_timestamp
    → { revenue_usd, revenue_cny, list_usd, cost_cny, profit_cny, profit_rate,
        refund_quota, refund_usd, request_count, prompt_tokens, completion_tokens,
        unpriced_channel_count,                       // 未填倍率渠道数（警示条）
        trend:  [ {date, revenue_cny, cost_cny, profit_cny} ],   // 按天，单轴 CNY
        cost_stack: [ {date, channel_id, channel_name, cost_cny} ] }

GET /api/cost/users?start_timestamp&end_timestamp&username&sort&p&page_size
    → { total, items: [ { user_id, username,
          revenue_usd, revenue_cny, cost_cny, profit_cny, profit_rate,
          prompt_tokens, completion_tokens, request_count, refund_usd,
          breakdown: [ { channel_id, channel_name, model_name,
                         revenue_usd, list_usd, cost_cny, profit_cny,
                         prompt_tokens, completion_tokens, request_count } ] } ] }

GET /api/cost/models?start_timestamp&end_timestamp&model_name&sort&p&page_size
    → 行结构同上（主键换成 model_name；breakdown 为 用户 × 渠道）

GET /api/cost/channels?start_timestamp&end_timestamp&sort&p&page_size
    → { total, items: [ { channel_id, channel_name, cost_ratio, priced: bool,
          revenue_usd, list_usd, revenue_cny, cost_cny, profit_cny, profit_rate,
          user_count, prompt_tokens, completion_tokens, request_count, refund_usd,
          breakdown: [ { username, model_name, ... } ] } ] }   // 用户 × 模型

POST /api/cost/channels/:id/sync    // 二期：触发聚合系统下钻拉取；一期返回未配置
```

- 三个维度接口共用同一 cube 构建函数，仅折叠维度不同；`breakdown` 只对当前页行返回（限制条数，如每行 top 100，按收入排序，超出折叠为「其他」并给计数）。
- 响应统一 `{ success, message, data }`；金额 float 保留 6 位（沿用 `roundTo6`）。

### 5.4 聚合系统适配器（二期接口，本期定义）

```go
// service/upstream_adapter.go
type UpstreamSupplierUsage struct {
    SupplierName                   string
    UsageUsd                       float64 // 该家供应商刊例消耗
    CostRatio                      float64 // 该家供应商自己的 CNY:USD 倍率
    CostCny                        float64
    Availability                   float64 // 0-1
    CacheTokens                    int64
    PromptTokens, CompletionTokens int64
    Raw                            map[string]any
}
type UpstreamAdapter interface {
    FetchUsage(ctx context.Context, ch *model.Channel, start, end int64) ([]UpstreamSupplierUsage, error)
}
```

- 按渠道设置里的适配类型分发（`ChannelSettings` 二期再加 `upstream_api` 配置块）；用户提供聚合系统接口文档后实现适配器，结果落 `channel_upstream_snapshots`。
- 前端表现：聚合系统渠道行展开时多一个「上游供应商」子表（各家倍率/成本/可用性/缓存/tokens），并给出「渠道兜底口径 vs 上游真实成本」对照。

### 5.5 规则遵从

- JSON 一律 `common.Marshal/Unmarshal`（Rule 1）；`ChannelSettings` 新增字段用 `omitempty` 且语义为「0 = 未填写」，不涉及 Rule 6 的上游转发路径。
- 聚合不写新 SQL（走既有导出流），天然三库兼容（Rule 2）。
- 金额换算复用 `common.QuotaPerUnit` 与 `operation_setting.USDExchangeRate`，不另造设置项。

### 5.6 后端测试

- 聚合核单测（SQLite 内存库 + 构造日志行）：三种 seedance 退款场景的净额；分组折扣 0.8 时 刊例$/收入$ 差异正确；固定价模型（model_price）与倍率模型（model_ratio）两条定价路径；缓存 ratio 参与刊例重算；未填倍率渠道成本为 0 且计入警示数。
- 三维度折叠一致性：三个接口的总计行相互相等（Σ用户 = Σ模型 = Σ渠道）。
- 权限：非 Root 访问 `/api/cost/*` 返回 403。

## 6. 新前端方案（web/default）

### 6.1 路由与入口

- 路由：`src/routes/_authenticated/cost/index.tsx`，`beforeLoad` 校验 `role >= ROLE.SUPER_ADMIN`，否则 redirect `/403`；zod search schema 承载 `start / end / tab / p / sort`。
- 侧边栏：`use-sidebar-data.ts` 的 `admin` 组新增 `{ title: t('Cost Accounting'), url: '/cost', icon: Calculator }`；`use-sidebar-view.ts` 为该项加 root-only 过滤（item 级 `rootOnly` 标记）。
- 功能目录：`src/features/cost/`（`index.tsx`、`api.ts`、`components/*`），模板对齐 `features/users` + `features/dashboard`。

### 6.2 页面结构（交互蓝图）

```
┌ SectionPageLayout ────────────────────────────────────────────────┐
│ 标题「成本核算」        [时间范围 ▾ 近7天] [刷新 ⟳]                 │
│ ⚠ 提示条（仅当存在未填倍率渠道）：3 个渠道未填成本倍率，成本按 0 计 │
│                                                                    │
│ ┌KPI 行（StatCard ×4）──────────────────────────────────────────┐ │
│ │ 收入            成本(¥)        利润(¥)        利润率            │ │
│ │ $1,691.19       ¥4,938.27      ¥7,407.40      60.0%            │ │
│ │ 副行 ¥12,345.67  副行 刊例$X×倍率 绿涨红亏      副行 环比        │ │
│ │ 副行：seedance 退款冲减 $xx                                     │ │
│ └────────────────────────────────────────────────────────────────┘ │
│ ┌趋势（VChart 折线，单轴 CNY）────────┐ ┌成本构成（按渠道堆叠柱）─┐ │
│ │ 收入¥ / 成本¥ / 利润¥ 三条线          │ │ 固定分类色序，hover 提示 │ │
│ └──────────────────────────────────────┘ └──────────────────────────┘ │
│                                                                    │
│ Tabs: [ 用户维度 ] [ 模型维度 ] [ 供应商维度 ]                      │
│                                                                    │
│ 用户维度（DataTablePage，服务端分页，URL 同步）                     │
│   列：用户 | 收入($) | 收入(¥) | 成本(¥) | 利润(¥) | 利润率 |       │
│        输入/输出 tokens | 请求数 | 退款($)                          │
│   行展开 ▸ 渠道 × 模型 明细子表 + 成本占比条                        │
│                                                                    │
│ 模型维度：列同上（主键=模型名）；行展开 ▸ 用户 × 渠道 明细           │
│                                                                    │
│ 供应商维度（=渠道）：                                               │
│   列：渠道 | 倍率(CNY:USD)✎ | 收入($) | 刊例($) | 成本(¥) |         │
│        利差(¥) | 利润率 | 用户数 | 请求数                           │
│   倍率列内联编辑（✎ → 小弹窗，实时示例「2.5 → $1 记 ¥2.50」）        │
│   未填倍率行黄色标记；行展开 ▸ 用户 × 模型 明细；                    │
│   聚合系统渠道（二期）额外显示「上游供应商」子表                     │
└────────────────────────────────────────────────────────────────────┘
```

- **时间筛选**：复制 `models-filter-dialog.tsx` 模式 —— 预设（今天/昨天/近 7 天/近 30 天/本月）+ 两个 `DateTimePicker` 自定义；写入 URL，可分享。
- **明细跳转**：各维度行提供「查看日志」链接，带时间与 user/channel/model 过滤参数跳转现有日志页。
- **渠道编辑页**同步新增「成本倍率 CNY:USD」输入框（`ChannelSettings` 表单区，`isRoot()` 时才渲染）。

### 6.3 样式方案（遵循 dataviz 规范）

- **语义 token 全覆盖**：卡片 `bg-card`，正利润 `text-success`、负利润 `text-destructive`、次要信息 `text-muted-foreground`；不写裸色值，`.dark` 自动适配；VChart 走现成 `use-chart-theme.ts` 主题跟随。
- **图表规范**：单轴（收入/成本/利润同为 CNY 共轴合法，收入$只进表格与 KPI 不上图混轴）；折线 2px + crosshair tooltip；堆叠柱段间 2px 表面缝隙；渠道分类色固定顺序、筛选后不重排、超过 8 个渠道折叠为「其他」；≥2 系列必有图例；数值文字用文本 token 不用系列色；实现时对最终分类色跑 `validate_palette.js`（light/dark 双模式）通过后再上线。
- **数字排版**：金额右对齐 + `tabular-nums`；$ 与 ¥ 并排时主口径大字、另一币种 `text-muted-foreground` 小字（KPI 收入卡：$ 主、¥ 副；成本/利润卡：¥ 主）。
- **加载/空态**：DataTablePage 自带 skeleton 与空态；KPI 骨架屏。
- **i18n**：英文平铺键，`en.json` + `zh.json`（及其余语言）同步，如 `"Cost Accounting"`、`"Cost Ratio (CNY per USD)"`、`"List Price"`、`"Profit"`、`"No cost ratio set"`。

## 7. 老前端方案（web/classic）

- **路由**：`App.jsx` 新增 `/console/cost`；`helpers/auth.jsx` 新增 `RootRoute`（`role >= 100` 否则 `/forbidden`）。
- **侧边栏**：`SiderBar.jsx` 的 `routerMap` 加 `cost: '/console/cost'`；`adminItems` 加 `{ text: t('成本核算'), itemKey: 'cost', to: '/console/cost', className: isRoot() ? '' : 'tableHiddle' }`。
- **页面**：`pages/Cost/index.jsx` + `components/cost/`，usage-logs 容器模式 —— `useCostData()` hook 管全部状态，`CardPro` 组合：
  - `statsArea`：Semi Card KPI 行（收入 $/¥、成本 ¥、利润 ¥、利润率，复用 `StatsCards` 视觉）。
  - `searchArea`：Semi `Form.DatePicker`（dateTime）×2 + 预设 `RadioGroup`。
  - 主体：Semi `Tabs` 三个维度，各自 `CardTable`（controlled 分页，`expandedRowRender` 做明细子表），上方 VChart 趋势/堆叠卡片（复用 `helpers/dashboard.js` spec 构造方式，同样单轴、固定色序）。
  - 渠道倍率内联编辑：Semi `Modal` + `InputNumber`，调 `PUT /api/channel`。
- **i18n**：中文串即键，写入 `zh-CN.json` 等各 locale。

## 8. 实施里程碑

1. **M1 后端**：`ChannelSettings.CostRatio` + 抽取公共聚合核 + `/api/cost/*` 四个接口（含测试，TDD）。
2. **M2 新前端**：路由/侧边栏/KPI/双图表/三维度表格/倍率内联编辑 + 渠道编辑页倍率输入 + i18n。
3. **M3 老前端**：RootRoute + 页面 + i18n。
4. **M4 聚合系统适配器**：等用户提供接口文档后实现，落快照表，渠道行展开显示上游供应商明细与真实成本对照。

## 9. 已定默认值与待确认项

| # | 决策点 | 结论 |
|---|---|---|
| 1 | 利润口径 | ✅ 已确认：收入$ × 售卖汇率(USDExchangeRate) − 刊例$ × 渠道倍率，全 CNY 相减 |
| 2 | 供应商建模 | ✅ 已确认：供应商 = 渠道，倍率逐渠道填写；聚合系统背后供应商二期经 API 下钻 |
| 3 | 倍率可见性 | ✅ 已确认：倍率存 `ChannelSettings`；成本核算页面仅 Root 可见（路由守卫 + 侧边栏隐藏），渠道编辑页的「成本倍率」输入框前端加 `isRoot()` 判断对管理员隐藏。注：管理员经渠道 API 响应仍可能看到 setting JSON 内的数字，用户已接受此程度 |
| 4 | 未填倍率渠道 | 成本按 0 计 + 黄色警示 + 顶部提示条 |
| 5 | 售卖汇率 | 复用全局 `USDExchangeRate`；不同客户折扣已体现在实付额度（分组倍率）中，无需另设 |
| 6 | 缓存 token 明细展示 | 一期不单列（缓存折扣已在刊例/实付重算中计价）；缓存命中量等运营指标二期由聚合系统 API 提供 |
