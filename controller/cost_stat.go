package controller

import (
	"time"

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
