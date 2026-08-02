package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
)

// 分组折扣 0.8：实付 800，刊例应还原为 1000。
func TestCostCube_ListVsActualWithGroupDiscount(t *testing.T) {
	c := newCostCube()
	c.addBatch([]*model.Log{
		{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-01", 10), UserId: 1, Username: "alice",
			ChannelId: 3, ModelName: "gpt-4o", Quota: 800, PromptTokens: 10, CompletionTokens: 5,
			Other: `{"model_ratio":2,"group_ratio":0.8}`},
	})
	if len(c.rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(c.rows))
	}
	for _, r := range c.rows {
		if r.Quota != 800 {
			t.Fatalf("actual = %v, want 800", r.Quota)
		}
		if r.ListQuota != 1000 {
			t.Fatalf("list = %v, want 1000", r.ListQuota)
		}
		if r.RequestCount != 1 {
			t.Fatalf("requests = %d, want 1", r.RequestCount)
		}
	}
}

// seedance 三场景：全额退款净 0；结算补扣净 120；结算部分退净 70。
// settle 行不计请求数；退款行冲减实付与刊例并累计 RefundQuota。
func TestCostCube_SeedanceRefundScenarios(t *testing.T) {
	mk := func(typ int, quota int, stage string, day string, hour int) *model.Log {
		return &model.Log{Type: typ, CreatedAt: tsOn(day, hour), UserId: 1, Username: "alice",
			ChannelId: 3, ModelName: "seedance-1-5-pro",
			Quota: quota, Other: `{"group_ratio":1,"billing_stage":"` + stage + `","is_task":true}`}
	}
	c := newCostCube()
	// 场景1: 预扣100 全退 → 净0
	c.addBatch([]*model.Log{
		mk(model.LogTypeConsume, 100, "pre_consume", "2026-06-01", 9),
		mk(model.LogTypeRefund, 100, "refund", "2026-06-01", 10),
	})
	// 场景2: 预扣100 补扣20 → 净120（settle 不计请求）
	c.addBatch([]*model.Log{
		mk(model.LogTypeConsume, 100, "pre_consume", "2026-06-02", 9),
		mk(model.LogTypeConsume, 20, "settle", "2026-06-02", 10),
	})
	// 场景3: 预扣100 退30 → 净70
	c.addBatch([]*model.Log{
		mk(model.LogTypeConsume, 100, "pre_consume", "2026-06-03", 9),
		mk(model.LogTypeRefund, 30, "refund", "2026-06-03", 10),
	})
	var totalActual, totalList, totalRefund float64
	var totalReq int
	for _, r := range c.rows {
		totalActual += r.Quota
		totalList += r.ListQuota
		totalRefund += r.RefundQuota
		totalReq += r.RequestCount
	}
	if totalActual != 190 { // 0 + 120 + 70
		t.Fatalf("net actual = %v, want 190", totalActual)
	}
	if totalList != 190 { // ratio 1 → list == actual
		t.Fatalf("net list = %v, want 190", totalList)
	}
	if totalRefund != 130 { // 100 + 30
		t.Fatalf("refund = %v, want 130", totalRefund)
	}
	if totalReq != 3 { // settle 行不计
		t.Fatalf("requests = %d, want 3", totalReq)
	}
}

// 非消费/退款类型忽略；无 other 的旧日志刊例=实付兜底。
func TestCostCube_IgnoresOtherTypesAndLegacyLogs(t *testing.T) {
	c := newCostCube()
	c.addBatch([]*model.Log{
		{Type: model.LogTypeTopup, CreatedAt: tsOn("2026-06-01", 9), UserId: 1, Username: "a", Quota: 999},
		{Type: model.LogTypeError, CreatedAt: tsOn("2026-06-01", 9), UserId: 1, Username: "a", Quota: 5},
		{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-01", 9), UserId: 1, Username: "a",
			ChannelId: 7, ModelName: "m", Quota: 50, Other: ``},
	})
	if len(c.rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(c.rows))
	}
	for _, r := range c.rows {
		if r.Quota != 50 || r.ListQuota != 50 {
			t.Fatalf("legacy fallback broken: %+v", r)
		}
	}
}
