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
}

type costCube struct {
	rows map[costCubeKey]*costCubeRow
}

func newCostCube() *costCube {
	return &costCube{rows: make(map[costCubeKey]*costCubeRow)}
}

func (c *costCube) addBatch(logs []*model.Log) {
	for _, log := range logs {
		if log.Type != model.LogTypeConsume && log.Type != model.LogTypeRefund {
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

// costMoneyFromParts 统一金额换算：
// revenue_usd = 实付/QuotaPerUnit；cost_cny = 刊例USD × 渠道倍率；
// profit = revenue_cny − cost_cny；收入为 0 时利润率置 0。
func costMoneyFromParts(actualQuota, listQuota, refundQuota float64, pt, ct, rc int, ratio, exchangeRate float64) costMoney {
	m := costMoney{
		RevenueUsd:       roundTo6(actualQuota / common.QuotaPerUnit),
		ListUsd:          roundTo6(listQuota / common.QuotaPerUnit),
		RefundUsd:        roundTo6(refundQuota / common.QuotaPerUnit),
		PromptTokens:     pt,
		CompletionTokens: ct,
		RequestCount:     rc,
	}
	m.RevenueCny = roundTo6(m.RevenueUsd * exchangeRate)
	m.CostCny = roundTo6(m.ListUsd * ratio)
	m.ProfitCny = roundTo6(m.RevenueCny - m.CostCny)
	if m.RevenueCny != 0 {
		m.ProfitRate = roundTo6(m.ProfitCny / m.RevenueCny)
	}
	return m
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
	if m.RevenueCny != 0 {
		m.ProfitRate = roundTo6(m.ProfitCny / m.RevenueCny)
	} else {
		m.ProfitRate = 0
	}
}

// channelRatio 渠道倍率；未知渠道/未填 → 0（成本按 0 计，Priced=false）。
func channelRatio(channels map[int]*model.ChannelCostInfo, id int) (ratio float64, name string) {
	if ci := channels[id]; ci != nil {
		return ci.CostRatio, ci.Name
	}
	return 0, ""
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
		ratio, chName := channelRatio(channels, k.ChannelId)
		m := costMoneyFromParts(r.Quota, r.ListQuota, r.RefundQuota, r.PromptTokens, r.CompletionTokens, r.RequestCount, ratio, exchangeRate)

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
			_, chName := channelRatio(channels, b.key.ChannelId)
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
