package controller

import (
	"sort"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

// costCubeKey 成本立方体分桶键：用户 × 模型 × 渠道 × 日。
// 三个维度报表与总览趋势都从同一立方体折叠得出，保证口径一致。
type costCubeKey struct {
	UserId    int
	Username  string
	ModelName string
	ChannelId int
	Day       string // "2006-01-02"（服务器本地时区，与账单汇总一致）
}

type costCubeRow struct {
	// Quota 净实付（含分组折扣，退款已冲减）；ListQuota 净刊例价金额（上游计费基数）。
	// 均为 quota 单位，除以 QuotaPerUnit 得 USD。
	Quota            float64
	ListQuota        float64
	RefundQuota      float64 // 退款行 quota 正数累计（仅展示用，净额已含在 Quota/ListQuota）
	PromptTokens     int
	CompletionTokens int
	RequestCount     int // 消费且非 settle 补扣行（任务多行只按 pre_consume 计 1 次）

	ErrorCount          int     // LogTypeError 行计数（同一用户/模型/渠道/日桶）
	CacheReadTokens     int     // 累计缓存读取 tokens（消费非退款行）
	CacheCreationTokens int     // 累计缓存创建 tokens（legacy + 5m + 1h 合计）
	FrtSumMs            float64 // 首字延迟毫秒累加（仅 info.Frt > 0 的行）
	FrtCount            int     // 参与 FrtSumMs 累加的行数
}

type costCube struct {
	rows map[costCubeKey]*costCubeRow
}

func newCostCube() *costCube {
	return &costCube{rows: make(map[costCubeKey]*costCubeRow)}
}

func (c *costCube) addBatch(logs []*model.Log) {
	for _, log := range logs {
		if log.Type != model.LogTypeConsume && log.Type != model.LogTypeRefund && log.Type != model.LogTypeError {
			continue
		}
		key := costCubeKey{
			UserId:    log.UserId,
			Username:  log.Username,
			ModelName: log.ModelName,
			ChannelId: log.ChannelId,
			Day:       time.Unix(log.CreatedAt, 0).Format("2006-01-02"),
		}
		row := c.rows[key]
		if row == nil {
			row = &costCubeRow{}
			c.rows[key] = row
		}
		if log.Type == model.LogTypeError {
			row.ErrorCount++
			continue
		}
		info := parseLogPricingInfo(log)
		listQ := logListQuota(log, info)
		if log.Type == model.LogTypeRefund {
			row.Quota -= float64(log.Quota)
			row.ListQuota -= listQ
			row.RefundQuota += float64(log.Quota)
			continue
		}
		if !isSettleStageLog(info) {
			row.RequestCount++
		}
		row.Quota += float64(log.Quota)
		row.ListQuota += listQ
		row.PromptTokens += log.PromptTokens
		row.CompletionTokens += log.CompletionTokens
		row.CacheReadTokens += getCacheTokensFromOther(log, "cache_tokens")
		row.CacheCreationTokens += getCacheCreationTokensFromOther(log)
		if info != nil && info.Frt > 0 {
			row.FrtSumMs += info.Frt
			row.FrtCount++
		}
	}
}

const (
	costDimUser      = "user"
	costDimModel     = "model"
	costDimChannel   = "channel"
	costBreakdownCap = 100
)

// costMoney 金额与用量的汇总单元：USD 原始金额 + 按汇率/渠道倍率换算后的 CNY 金额。
type costMoney struct {
	RevenueUsd float64 `json:"revenue_usd"`
	RevenueCny float64 `json:"revenue_cny"`
	ListUsd    float64 `json:"list_usd"`
	CostCny    float64 `json:"cost_cny"`
	ProfitCny  float64 `json:"profit_cny"`
	ProfitRate float64 `json:"profit_rate"`
	RefundUsd  float64 `json:"refund_usd"`

	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	RequestCount     int `json:"request_count"`

	CacheReadTokens     int     `json:"cache_read_tokens"`
	CacheCreationTokens int     `json:"cache_creation_tokens"`
	TotalTokens         int     `json:"total_tokens"`
	ErrorCount          int     `json:"error_count"`
	FrtSumMs            float64 `json:"frt_sum_ms"`
	FrtCount            int     `json:"frt_count"`
	SuccessRate         float64 `json:"success_rate"`
	CacheRate           float64 `json:"cache_rate"`
	AvgTtftMs           float64 `json:"avg_ttft_ms"`
}

// costBreakdownRow 维度折叠行下的子明细（折叠掉本维度与 Day，保留其余两个维度）。
type costBreakdownRow struct {
	Username    string `json:"username,omitempty"`
	ModelName   string `json:"model_name,omitempty"`
	ChannelId   int    `json:"channel_id,omitempty"`
	ChannelName string `json:"channel_name,omitempty"`
	costMoney
}

// costDimensionRow 单个维度（user/model/channel）折叠后的一行；同一时刻只有对应
// 维度的身份字段被填充。
type costDimensionRow struct {
	UserId      int     `json:"user_id,omitempty"`
	Username    string  `json:"username,omitempty"`
	ModelName   string  `json:"model_name,omitempty"`
	ChannelId   int     `json:"channel_id,omitempty"`
	ChannelName string  `json:"channel_name,omitempty"`
	CostRatio   float64 `json:"cost_ratio,omitempty"`
	Priced      bool    `json:"priced"`
	UserCount   int     `json:"user_count,omitempty"`
	costMoney
	Breakdown          []costBreakdownRow `json:"breakdown,omitempty"`
	BreakdownTruncated int                `json:"breakdown_truncated,omitempty"`
}

// costMoneyFromRow 统一金额换算：
// revenue_usd = 实付/QuotaPerUnit；cost_cny = 刊例USD × 渠道倍率；
// profit = revenue_cny − cost_cny；收入为 0 时利润率置 0。
// 同时派生 v2 指标：total_tokens、success_rate、cache_rate、avg_ttft_ms
// （零分母兜底规则见 deriveCostMoneyRates）。
func costMoneyFromRow(r *costCubeRow, ratio, exchangeRate float64) costMoney {
	m := costMoney{
		RevenueUsd:          roundTo6(r.Quota / common.QuotaPerUnit),
		ListUsd:             roundTo6(r.ListQuota / common.QuotaPerUnit),
		RefundUsd:           roundTo6(r.RefundQuota / common.QuotaPerUnit),
		PromptTokens:        r.PromptTokens,
		CompletionTokens:    r.CompletionTokens,
		RequestCount:        r.RequestCount,
		CacheReadTokens:     r.CacheReadTokens,
		CacheCreationTokens: r.CacheCreationTokens,
		ErrorCount:          r.ErrorCount,
		FrtSumMs:            r.FrtSumMs,
		FrtCount:            r.FrtCount,
	}
	m.RevenueCny = roundTo6(m.RevenueUsd * exchangeRate)
	m.CostCny = roundTo6(m.ListUsd * ratio)
	m.ProfitCny = roundTo6(m.RevenueCny - m.CostCny)
	if m.RevenueCny != 0 {
		m.ProfitRate = roundTo6(m.ProfitCny / m.RevenueCny)
	}
	m.deriveRates()
	return m
}

// deriveRates 重新计算 v2 派生指标：total_tokens 恒为 prompt+completion；
// success_rate 在请求+错误数为 0 时兜底为 1；cache_rate 在 prompt tokens 为
// 0 时兜底为 0；avg_ttft_ms 在无 frt 采样时兜底为 0。add() 汇总原始字段后
// 必须重新调用本方法，不能直接对派生字段做加法。
func (m *costMoney) deriveRates() {
	m.TotalTokens = m.PromptTokens + m.CompletionTokens
	if m.RequestCount+m.ErrorCount == 0 {
		m.SuccessRate = 1
	} else {
		m.SuccessRate = roundTo6(float64(m.RequestCount) / float64(m.RequestCount+m.ErrorCount))
	}
	if m.PromptTokens == 0 {
		m.CacheRate = 0
	} else {
		m.CacheRate = roundTo6(float64(m.CacheReadTokens) / float64(m.PromptTokens))
	}
	if m.FrtCount == 0 {
		m.AvgTtftMs = 0
	} else {
		m.AvgTtftMs = roundTo6(m.FrtSumMs / float64(m.FrtCount))
	}
}

func (m *costMoney) add(o costMoney) {
	m.RevenueUsd = roundTo6(m.RevenueUsd + o.RevenueUsd)
	m.RevenueCny = roundTo6(m.RevenueCny + o.RevenueCny)
	m.ListUsd = roundTo6(m.ListUsd + o.ListUsd)
	m.CostCny = roundTo6(m.CostCny + o.CostCny)
	m.ProfitCny = roundTo6(m.ProfitCny + o.ProfitCny)
	m.RefundUsd = roundTo6(m.RefundUsd + o.RefundUsd)
	m.PromptTokens += o.PromptTokens
	m.CompletionTokens += o.CompletionTokens
	m.RequestCount += o.RequestCount
	m.CacheReadTokens += o.CacheReadTokens
	m.CacheCreationTokens += o.CacheCreationTokens
	m.ErrorCount += o.ErrorCount
	m.FrtSumMs = roundTo6(m.FrtSumMs + o.FrtSumMs)
	m.FrtCount += o.FrtCount
	if m.RevenueCny != 0 {
		m.ProfitRate = roundTo6(m.ProfitCny / m.RevenueCny)
	} else {
		m.ProfitRate = 0
	}
	m.deriveRates()
}

// effectiveChannelRatio 依据计价方式换算有效倍率（CNY:USD）：
// ratio 模式取 CostRatio；discount 模式取 CostDiscount×exchangeRate；未知渠道/未填 → 0。
func effectiveChannelRatio(channels map[int]*model.ChannelCostInfo, id int, exchangeRate float64) (ratio float64, name string) {
	ci := channels[id]
	if ci == nil {
		return 0, ""
	}
	if ci.CostMode == "discount" {
		return ci.CostDiscount * exchangeRate, ci.Name
	}
	return ci.CostRatio, ci.Name
}

// foldCostCube 沿 dim 折叠立方体。每行金额先在 (渠道) 粒度换算再累加，
// 保证不同倍率渠道混在同一用户/模型行时成本正确。
func foldCostCube(cube *costCube, dim string, channels map[int]*model.ChannelCostInfo, exchangeRate float64) []costDimensionRow {
	type groupKey struct {
		UserId    int
		Username  string
		ModelName string
		ChannelId int
	}
	groups := make(map[groupKey]*costDimensionRow)
	breakdowns := make(map[groupKey]map[costCubeKey]*costMoney) // sub-agg per group
	userSets := make(map[groupKey]map[int]bool)

	for k, r := range cube.rows {
		ratio, chName := effectiveChannelRatio(channels, k.ChannelId, exchangeRate)
		m := costMoneyFromRow(r, ratio, exchangeRate)

		var gk groupKey
		switch dim {
		case costDimUser:
			gk = groupKey{UserId: k.UserId, Username: k.Username}
		case costDimModel:
			gk = groupKey{ModelName: k.ModelName}
		default:
			gk = groupKey{ChannelId: k.ChannelId}
		}
		row := groups[gk]
		if row == nil {
			row = &costDimensionRow{UserId: gk.UserId, Username: gk.Username, ModelName: gk.ModelName, ChannelId: gk.ChannelId}
			if dim == costDimChannel {
				row.ChannelName = chName
				row.CostRatio = ratio
				row.Priced = ratio > 0
				userSets[gk] = make(map[int]bool)
			}
			groups[gk] = row
		}
		row.costMoney.add(m)
		if dim == costDimChannel {
			userSets[gk][k.UserId] = true
		}

		// breakdown 子键：折叠掉本维度与 Day，保留其余两个维度
		bk := k
		bk.Day = ""
		switch dim {
		case costDimUser:
			bk.UserId, bk.Username = 0, ""
		case costDimModel:
			bk.ModelName = ""
		default:
			bk.ChannelId = 0
		}
		if breakdowns[gk] == nil {
			breakdowns[gk] = make(map[costCubeKey]*costMoney)
		}
		sub := breakdowns[gk][bk]
		if sub == nil {
			sub = &costMoney{}
			breakdowns[gk][bk] = sub
		}
		sub.add(m)
	}

	rows := make([]costDimensionRow, 0, len(groups))
	for gk, row := range groups {
		if dim == costDimChannel {
			row.UserCount = len(userSets[gk])
		}
		// breakdown 排序取前 costBreakdownCap
		type bd struct {
			key costCubeKey
			m   *costMoney
		}
		bds := make([]bd, 0, len(breakdowns[gk]))
		for bk, m := range breakdowns[gk] {
			bds = append(bds, bd{bk, m})
		}
		sort.Slice(bds, func(i, j int) bool {
			a, b := bds[i], bds[j]
			if a.m.RevenueCny != b.m.RevenueCny {
				return a.m.RevenueCny > b.m.RevenueCny
			}
			if a.key.Username != b.key.Username {
				return a.key.Username < b.key.Username
			}
			if a.key.ModelName != b.key.ModelName {
				return a.key.ModelName < b.key.ModelName
			}
			return a.key.ChannelId < b.key.ChannelId
		})
		if len(bds) > costBreakdownCap {
			row.BreakdownTruncated = len(bds) - costBreakdownCap
			bds = bds[:costBreakdownCap]
		}
		for _, b := range bds {
			_, chName := effectiveChannelRatio(channels, b.key.ChannelId, exchangeRate)
			row.Breakdown = append(row.Breakdown, costBreakdownRow{
				Username: b.key.Username, ModelName: b.key.ModelName,
				ChannelId: b.key.ChannelId, ChannelName: chName,
				costMoney: *b.m,
			})
		}
		rows = append(rows, *row)
	}
	sort.Slice(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if a.RevenueCny != b.RevenueCny {
			return a.RevenueCny > b.RevenueCny
		}
		if a.Username != b.Username {
			return a.Username < b.Username
		}
		if a.ModelName != b.ModelName {
			return a.ModelName < b.ModelName
		}
		return a.ChannelId < b.ChannelId
	})
	return rows
}
