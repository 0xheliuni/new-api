# 成本核算（Cost Accounting）功能设计方案

- 日期：2026-08-02
- 状态：待评审
- 权限级别：仅超级管理员（Root，role >= 100）
- 范围：后端（Go）+ 新前端（web/default）+ 老前端（web/classic）

---

## 1. 目标

在管理员控制台新增「成本核算」页面，让超级管理员能够按时间范围看清：

1. **用户维度**：每个用户消耗了多少额度（= 我们的收入），这些额度分别经由哪些供应商提供，对应的供应商成本是多少，从而看清单个用户的利润。
2. **供应商维度**：每个供应商被哪些用户使用、用户在其上消耗的额度（收入）、供应商侧的成本，从而算出利差。
3. **供应商倍率**：每个供应商可填写一个「人民币:美元」成本倍率（如 2.5 表示 2.5 CNY 兑 1 USD 的成本价），成本 = 上游消耗 USD × 倍率。
4. **seedance 等异步视频任务的退款必须计入**（预扣费 → 结算/退款差额）。
5. **聚合系统对接（二期）**：若供应商背后是自建聚合系统（如另一套 new-api），通过其 API 拉取真实的上游消耗、成本、可用性、缓存、输入/输出 token 等数据。接口由用户后续提供，本期只预留适配器接口与数据结构。

## 2. 现状盘点（探索结论）

| 事实 | 位置 |
|---|---|
| 日志表 `logs` 已含 `user_id / channel_id / model_name / quota / prompt_tokens / completion_tokens / type / created_at`，索引含 `idx_created_at_type` | `model/log.go:35-77` |
| 额度换算：`USD = quota / common.QuotaPerUnit`（默认 500000）；CNY = USD × `operation_setting.USDExchangeRate` | `common/constants.go:62`、`controller/bill_summary_query.go:60,125-133` |
| seedance 退款已是一等公民：失败退款与结算差额都会写 `LogTypeRefund(6)` 日志行（`Quota` 存正数），`other.billing_stage ∈ {pre_consume, settle, refund}`；净消耗 = `CASE WHEN type=6 THEN -quota ELSE quota END` | `service/task_billing.go:180-295`、`model/log.go:548-552` |
| 展示层会隐藏 settle/refund 行（合并进 pre_consume 行），但**原始聚合/导出流不隐藏**，聚合口径正确 | `model/log.go:400-403` |
| 已有最接近的先例：`billSummaryAgg` 按（时间桶 × 用户 × channel × 模型）聚合并对退款做净额处理 | `controller/bill_summary.go:46-148` |
| 渠道（channel）目前**没有**任何成本/倍率字段；`Vendor` 表只是模型展示元数据，与渠道无成本关联 | `model/channel.go`、`model/vendor_meta.go` |
| logs 可能位于独立 `LOG_DB`，**不能与 channels 做 SQL JOIN**，需在应用层映射 channel → 供应商 | `model/main.go`、`model/log.go:843-879` |
| Root 权限中间件 `middleware.RootAuth()` 已有成熟用法（`/api/option` 组） | `middleware/auth.go:192`、`router/api-router.go:186-201` |
| 新前端：TanStack Router 文件路由 + TanStack Table + Base UI + Tailwind v4 + VChart；`ROLE.SUPER_ADMIN = 100` | `web/default/src/lib/roles.ts` 等 |
| 老前端：React 19 + rsbuild + Semi Design + VChart；`isRoot()` = role >= 100，但只有 `AdminRoute`（>=10），无 RootRoute | `web/classic/src/helpers/auth.jsx` |

## 3. 核心计费模型

所有金额计算基于 logs 表（净额口径，退款自动冲减）：

```
净消耗额度 net_quota = Σ CASE WHEN type = 6 (Refund) THEN -quota ELSE quota END
                       WHERE type IN (2 Consume, 6 Refund) AND created_at ∈ [start, end]

消耗美元  usage_usd  = net_quota / QuotaPerUnit
收入人民币 revenue_cny = usage_usd × USDExchangeRate          （站点售卖汇率，现有全局设置）
成本人民币 cost_cny    = usage_usd × supplier.cost_ratio       （供应商倍率，CNY:USD）
利润      profit_cny  = revenue_cny − cost_cny
利润率    profit_rate = profit_cny / revenue_cny
```

- **seedance 退款**：预扣 100 → 全额退款 → 净额 0；预扣 100 → 结算补扣 20 → 净额 120；预扣 100 → 结算退回 30 → 净额 70。以上均由 `type=6` 净额公式天然覆盖，收入与成本同步冲减。
- **一期近似**：上游消耗 USD 以「我们向用户计费的 USD」近似（假设我方模型倍率与上游标价一致）。聚合系统接入后（二期），供应商维度可用上游真实账单数据校正。
- **未绑定供应商的渠道**：归入虚拟分组「未分配供应商」，成本按 0 计并在前端明显警示（黄色提示条 + 行标记），避免利润虚高被误读。

## 4. 备选方案对比

### 4.1 「供应商」如何建模

| 方案 | 说明 | 结论 |
|---|---|---|
| **A. 新建 `suppliers` 表 + channel 加 `supplier_id` 外键（推荐）** | 供应商是独立实体：名称、倍率、备注、聚合 API 配置；一个供应商可对应多个渠道（同一上游开多个渠道很常见）；聚合系统配置天然挂在供应商上 | ✅ 采用 |
| B. 在渠道 `ChannelSettings` JSON 里加 `cost_ratio` | 免建表免迁移，但倍率要逐渠道重复填写，且"哪些用户用了这家供应商"无法跨渠道汇总，聚合 API 配置无处安放 | ❌ |
| C. 复用渠道 Tag 当供应商 | 零改动，但 Tag 已有分组语义，无法挂倍率/API 配置字段 | ❌ |

### 4.2 聚合数据从哪来

| 方案 | 说明 | 结论 |
|---|---|---|
| **A. 直接对 logs 做 SQL GROUP BY（推荐）** | `GROUP BY user_id, channel_id`，走 `idx_created_at_type` 索引；天然实时（用户要求"实时显示"）；三库兼容（CASE WHEN / IN 均安全）；应用层完成 channel→supplier 映射与汇率换算 | ✅ 一期采用 |
| B. 新建小时级汇总表（类 QuotaData + channel 维度） | 查询快，但有 5 分钟延迟、需要双写与回补逻辑，先不做 | 🔜 数据量大后的二期优化，API 层已预留（响应结构不变，只换数据源） |
| C. 扩展现有 QuotaData 加 channel 维度 | 侵入现有看板链路，风险大收益小 | ❌ |

## 5. 后端方案

### 5.1 数据模型

**新表 `suppliers`**（注册进 `model/main.go` 的 `AutoMigrate` 列表，主库 DB）：

```go
// model/supplier.go
type Supplier struct {
    Id          int     `json:"id"`
    Name        string  `json:"name" gorm:"size:64;uniqueIndex"`
    Remark      string  `json:"remark" gorm:"type:text"`
    CostRatio   float64 `json:"cost_ratio" gorm:"default:0"` // 成本倍率 CNY:USD，如 2.5
    Status      int     `json:"status" gorm:"default:1"`     // 1 启用 2 停用
    // ---- 聚合系统对接（二期使用，一期仅存储）----
    ApiType     string  `json:"api_type" gorm:"size:32;default:''"` // "" 无 / "newapi" 聚合系统
    BaseUrl     string  `json:"base_url" gorm:"type:text"`
    ApiKey      string  `json:"api_key" gorm:"type:text"`           // 仅 Root 可读写，列表响应脱敏
    CreatedTime int64   `json:"created_time" gorm:"bigint"`
    UpdatedTime int64   `json:"updated_time" gorm:"bigint"`
}
```

**channels 表加列**（AutoMigrate ADD COLUMN，SQLite/MySQL/PG 均安全）：

```go
// model/channel.go Channel struct 追加
SupplierId int `json:"supplier_id" gorm:"default:0;index"` // 0 = 未绑定
```

**二期预留表 `supplier_usage_snapshots`**（本期只定义结构不实现拉取）：聚合系统回传的上游真实数据快照 —— `supplier_id / start_ts / end_ts / usage_usd / cost_usd / availability / cache_tokens / prompt_tokens / completion_tokens / raw(json) / synced_at`。

### 5.2 聚合查询（model/cost_stat.go，查 LOG_DB）

一条跨库安全的 SQL 出全量矩阵，应用层再做映射与分页：

```sql
SELECT user_id, username, channel_id,
       SUM(CASE WHEN type = 6 THEN -quota ELSE quota END)            AS quota,
       SUM(CASE WHEN type = 2 THEN prompt_tokens ELSE 0 END)         AS prompt_tokens,
       SUM(CASE WHEN type = 2 THEN completion_tokens ELSE 0 END)     AS completion_tokens,
       SUM(CASE WHEN type = 2 THEN 1 ELSE 0 END)                     AS request_count,
       SUM(CASE WHEN type = 6 THEN quota ELSE 0 END)                 AS refund_quota
FROM logs
WHERE type IN (2, 6) AND created_at >= ? AND created_at <= ?
GROUP BY user_id, username, channel_id
```

- 趋势图另有一条按天分桶的 SQL（应用层按 `created_at - created_at % 86400` 分桶亦可，规避三库日期函数差异 —— 参照 `billSummaryAgg` 的做法在流式扫描中分桶，或直接复用其导出流引擎）。
- channel → supplier 映射：一次性 `SELECT id, name, supplier_id FROM channels` 从主库载入内存 map（对齐 `populateLogChannelNames` 模式）。
- 可选性能护栏：时间跨度上限（如 370 天）+ 60 秒 Redis/内存结果缓存（key = 参数哈希），保证"实时"又防止重复重查。

### 5.3 API 设计（全部挂 `middleware.RootAuth()`）

`router/api-router.go` 新增两个路由组：

```
/api/supplier                     — 供应商管理
  GET    /                        列表（api_key 脱敏为掩码）
  POST   /                        新建 {name, remark, cost_ratio, status, api_type?, base_url?, api_key?}
  PUT    /                        更新（同上，含 id）
  DELETE /:id                     删除（校验：仍有渠道绑定时拒绝或提示解绑）
  POST   /:id/channels            批量绑定/解绑渠道 {channel_ids: []int}
                                  （渠道编辑接口 UpdateChannel 也接受 supplier_id 字段单个改绑）

/api/cost                         — 成本核算报表
  GET /overview?start_timestamp&end_timestamp
      → { revenue_cny, cost_cny, profit_cny, profit_rate, usage_usd,
          refund_quota, refund_cny, request_count, prompt_tokens, completion_tokens,
          trend: [ {date, revenue_cny, cost_cny, profit_cny} ],           // 按天
          supplier_stack: [ {date, supplier_name, cost_cny} ] }           // 成本构成堆叠
  GET /users?start_timestamp&end_timestamp&username&sort&p&page_size
      → { total, items: [ { user_id, username, quota, usage_usd,
            revenue_cny, cost_cny, profit_cny, profit_rate,
            prompt_tokens, completion_tokens, request_count, refund_quota, refund_cny,
            suppliers: [ { supplier_id, supplier_name, quota, usage_usd,
                           cost_cny, revenue_cny, profit_cny,
                           prompt_tokens, completion_tokens, request_count } ] } ] }
  GET /suppliers?start_timestamp&end_timestamp
      → { items: [ { supplier_id, supplier_name, cost_ratio, channel_count,
            quota, usage_usd, revenue_cny, cost_cny, profit_cny, profit_rate,
            user_count, prompt_tokens, completion_tokens, request_count, refund_cny,
            unassigned: bool } ] }        // 含「未分配供应商」虚拟行
  GET /suppliers/:id/users?start_timestamp&end_timestamp&p&page_size
      → 该供应商下的用户明细（字段同 /users 的顶层行，去掉 suppliers 嵌套）
  POST /suppliers/:id/sync             // 二期：触发聚合系统拉取，一期返回 501/未配置
```

- 分页策略：`/users` 先按 `ORDER BY SUM(net_quota) DESC` 的用户分组 SQL 取当前页用户，再用 `user_id IN (...)` 出该页用户的供应商拆分，两条查询避免全量物化。
- 响应统一 `{ success, message, data }`，金额字段为 float（保留 6 位，沿用 `roundTo6`）。

### 5.4 聚合系统适配器（二期接口，本期定义）

```go
// service/supplier_adapter.go
type SupplierUsage struct {
    UsageUsd, CostUsd            float64
    Availability                 float64 // 0-1
    CacheTokens                  int64
    PromptTokens, CompletionTokens int64
    Raw                          map[string]any
}
type SupplierAdapter interface {
    FetchUsage(ctx context.Context, s *model.Supplier, start, end int64) (*SupplierUsage, error)
}
// 注册表按 s.ApiType 分发；用户提供聚合系统接口文档后实现 "newapi" 适配器，
// 结果写入 supplier_usage_snapshots，供应商维度报表叠加「上游真实成本」对照列。
```

### 5.5 规则遵从

- JSON 一律 `common.Marshal/Unmarshal`（Rule 1）。
- SQL 仅用 CASE WHEN / IN / SUM / GROUP BY，三库兼容；不 JOIN 跨库表；沿用 `logGroupCol` 等既有模式（Rule 2）。
- 金额换算复用 `common.QuotaPerUnit` 与 `operation_setting.USDExchangeRate`，不另造设置项。

### 5.6 后端测试

- `model/cost_stat_test.go`（SQLite 内存库）：退款净额（三种 seedance 场景）、跨供应商拆分、未绑定渠道归组、时间边界。
- `controller` 层：倍率换算、利润率除零（收入为 0 时 profit_rate 置 0）、分页正确性。
- 权限：非 Root 访问 `/api/cost/*`、`/api/supplier/*` 返回 403。

## 6. 新前端方案（web/default）

### 6.1 路由与入口

- 路由：`src/routes/_authenticated/cost/index.tsx`，`beforeLoad` 校验 `role >= ROLE.SUPER_ADMIN`（100），否则 redirect `/403`；zod search schema 承载 `start/end/tab/p` 等 URL 状态。
- 侧边栏：`use-sidebar-data.ts` 的 `admin` 组新增 `{ title: t('Cost Accounting'), url: '/cost', icon: Calculator }`；在 `use-sidebar-view.ts` 为该项增加 root-only 过滤（admin 组现有逻辑只挡 <10，需按 item 级 `rootOnly` 标记再过滤一次）。
- 功能目录：`src/features/cost/`（`index.tsx`、`api.ts`、`components/*`），模板对齐 `features/users` + `features/dashboard`。

### 6.2 页面结构（交互蓝图）

```
┌ SectionPageLayout ────────────────────────────────────────────────┐
│ 标题「成本核算」        [时间范围 ▾ 近7天] [供应商筛选 ▾] [刷新 ⟳] │
│                                                                    │
│ ┌KPI 行（StatCard ×4，含迷你趋势 sparkline）────────────────────┐ │
│ │ 收入(¥)       成本(¥)       利润(¥)        利润率              │ │
│ │ ¥12,345.67    ¥4,938.27     ¥7,407.40      60.0%               │ │
│ │ $1,691.19     — 明细含各倍率  绿涨红亏      环比小字            │ │
│ │ 副行：含 seedance 退款冲减 ¥xxx                                 │ │
│ └────────────────────────────────────────────────────────────────┘ │
│ ┌趋势图（VChart 折线，单轴 CNY）─────┐ ┌成本构成（堆叠柱/天）──┐ │
│ │ 收入 / 成本 / 利润 三条线            │ │ 按供应商着色，固定色序 │ │
│ └──────────────────────────────────────┘ └────────────────────────┘ │
│                                                                    │
│ Tabs: [ 用户维度 ] [ 供应商维度 ]                                   │
│                                                                    │
│ 用户维度 → DataTablePage（服务端分页，URL 同步）                   │
│   列：用户 | 消耗($) | 收入(¥) | 成本(¥) | 利润(¥) | 利润率 |      │
│        输入/输出tokens | 请求数 | 退款(¥)                          │
│   行可展开 ▸ 该用户的供应商拆分子表 + 水平堆叠占比条                │
│                                                                    │
│ 供应商维度 → 表格（供应商数量少，StaticDataTable 即可）             │
│   列：供应商 | 倍率(CNY:USD) | 渠道数 | 用户数 | 消耗($) |          │
│        收入(¥) | 成本(¥) | 利差(¥) | 利润率 | [管理]               │
│   「未分配供应商」行黄色警示；行展开 ▸ Top 用户列表（可跳转）        │
│   右上「管理供应商」按钮 → CRUD Dialog                              │
└────────────────────────────────────────────────────────────────────┘
```

- **时间筛选**：复制 `models-filter-dialog.tsx` 模式 —— 预设快捷键（今天/昨天/近 7 天/近 30 天/本月）+ 两个 `DateTimePicker` 自定义区间；写入 URL search params，刷新/分享不丢状态。
- **供应商管理 Dialog**：名称、备注、成本倍率（数字输入，后缀说明「¥ : $1」+ 实时示例文案「倍率 2.5 → 上游每消耗 $1 记成本 ¥2.50」）、状态开关、绑定渠道（`multi-select`，展示渠道 id/名称）、聚合系统区块（类型/BaseUrl/ApiKey，折叠区，标注「接口待接入」）。
- **明细跳转**：用户行、供应商行提供「查看日志」快捷链接，带上时间与 channel/user 过滤参数跳转现有日志页。

### 6.3 样式方案（遵循 dataviz 规范）

- **一律语义 token**：卡片 `bg-card text-card-foreground`，正利润 `text-success`、负利润 `text-destructive`、次要信息 `text-muted-foreground`；不写裸色值，天然适配暗色模式（`.dark` 类策略 + VChart ThemeManager 跟随，现成 `use-chart-theme.ts`）。
- **图表规范**：单轴（收入/成本/利润同为 CNY，共轴合法）；折线 2px、hover crosshair + tooltip；堆叠柱段间 2px 表面缝隙；供应商分类色按固定顺序分配、筛选后不重排；≥2 系列必有图例；金额文字用文本 token，不用系列色；实现时用 `scripts/validate_palette.js` 对最终分类色跑通过再上线；每张图表配套表格视图（本页表格本身即是）。
- **数字排版**：金额右对齐 + `tabular-nums`；USD 与 CNY 并排展示时 CNY 为主字号、USD 为 `text-muted-foreground` 小字。
- **加载/空态**：DataTablePage 自带 skeleton 与空态插画；聚合中显示 KPI 骨架屏。
- **i18n**：英文平铺键，`en.json` + `zh.json`（及其余语言）同步，如 `"Cost Accounting"`, `"Supplier"`, `"Cost Ratio (CNY per USD)"`, `"Profit"`, `"Unassigned Supplier"`。

## 7. 老前端方案（web/classic）

- **路由**：`App.jsx` 新增 `/console/cost`；新增 `RootRoute` 守卫组件（`helpers/auth.jsx`，`role >= 100` 否则 `/forbidden`），因为现有 `AdminRoute` 只要求 >=10。
- **侧边栏**：`SiderBar.jsx` 的 `routerMap` 加 `cost: '/console/cost'`；`adminItems` 加 `{ text: t('成本核算'), itemKey: 'cost', to: '/console/cost', className: isRoot() ? '' : 'tableHiddle' }`。
- **页面**：`pages/Cost/index.jsx` + `components/cost/`，沿用 usage-logs 容器模式 —— 一个 `useCostData()` hook 管全部状态，`CardPro` 组合：
  - `statsArea`：Semi Card KPI 行（收入/成本/利润/利润率，复用 `StatsCards` 视觉）。
  - `searchArea`：Semi `Form.DatePicker`（dateTime）×2 + 预设 `RadioGroup` + 供应商 `Select`。
  - 主体：Semi `Tabs`（用户维度 / 供应商维度），各自 `CardTable`（controlled 分页，`expandedRowRender` 做供应商拆分/Top 用户），上方 `VChart` 趋势卡片（复用 `helpers/dashboard.js` 的 spec 构造方式，同样单轴、固定色序）。
  - 供应商管理：Semi `Modal` + `Form`（字段与新前端一致）。
- **i18n**：中文串即键，写入 `zh-CN.json` 等各 locale。

## 8. 实施里程碑

1. **M1 后端**：`suppliers` 表 + channel `supplier_id` 列 + Supplier CRUD + `/api/cost/*` 三个报表接口（含测试，TDD）。
2. **M2 新前端**：路由/侧边栏/KPI/双图表/双维度表格/供应商管理 Dialog + i18n。
3. **M3 老前端**：RootRoute + 页面 + i18n。
4. **M4 聚合系统适配器**：等用户提供聚合系统接口文档后实现 `newapi` 适配器、快照表落库与「上游真实成本」对照列（可用性、缓存、输入/输出 token 同步展示）。

## 9. 已定默认值与待确认项

| # | 决策点 | 本方案默认 | 备注 |
|---|---|---|---|
| 1 | 收入折算汇率 | 全局 `USDExchangeRate` 设置 | 若不同用户有不同售价（充值折扣），一期不体现，报表口径为"标准售价收入" |
| 2 | 供应商建模 | 新 `suppliers` 表，渠道 N:1 绑定 | 见 4.1 对比 |
| 3 | 上游成本口径（一期） | 以我方计费 USD 近似上游消耗 USD | 聚合系统接入后校正 |
| 4 | 未绑定渠道 | 计入「未分配供应商」，成本按 0 记并警示 | 避免静默丢数据 |
| 5 | 报表权限 | 严格 Root（role 100） | 用户已明确"只有超级管理员" |
| 6 | 缓存 token 展示 | 一期不做（缓存量在 `other` JSON 中无法 SQL 聚合） | 二期由聚合系统 API 提供 |
