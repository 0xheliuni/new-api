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

func testChannels() map[int]*model.ChannelCostInfo {
	return map[int]*model.ChannelCostInfo{
		3: {Id: 3, Name: "openai-a", CostRatio: 2.5},
		7: {Id: 7, Name: "nopriced", CostRatio: 0},
	}
}

func seedCube() *costCube {
	c := newCostCube()
	c.addBatch([]*model.Log{
		// alice, gpt-4o, ch3: 实付800 刊例1000（8折）
		{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-01", 10), UserId: 1, Username: "alice",
			ChannelId: 3, ModelName: "gpt-4o", Quota: 800, PromptTokens: 10, CompletionTokens: 5,
			Other: `{"model_ratio":2,"group_ratio":0.8}`},
		// bob, gpt-4o, ch7: 实付=刊例 500
		{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-01", 11), UserId: 2, Username: "bob",
			ChannelId: 7, ModelName: "gpt-4o", Quota: 500, PromptTokens: 3, CompletionTokens: 2,
			Other: `{"model_ratio":2,"group_ratio":1}`},
	})
	return c
}

// 金额换算：QuotaPerUnit=500000, 汇率 7.0, ch3 倍率 2.5
// alice: revenue_usd=800/5e5=0.0016, revenue_cny=0.0112, list_usd=0.002, cost=0.005, profit=0.0062
func TestFoldCostCube_UserDimensionMoney(t *testing.T) {
	rows := foldCostCube(seedCube(), costDimUser, testChannels(), 7.0)
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	var alice *costDimensionRow
	for i := range rows {
		if rows[i].Username == "alice" {
			alice = &rows[i]
		}
	}
	if alice == nil {
		t.Fatal("alice missing")
	}
	if alice.RevenueUsd != 0.0016 || alice.RevenueCny != 0.0112 {
		t.Fatalf("revenue: %+v", alice.costMoney)
	}
	if alice.ListUsd != 0.002 || alice.CostCny != 0.005 {
		t.Fatalf("cost: %+v", alice.costMoney)
	}
	if alice.ProfitCny != 0.0062 {
		t.Fatalf("profit = %v, want 0.0062", alice.ProfitCny)
	}
	if len(alice.Breakdown) != 1 || alice.Breakdown[0].ChannelName != "openai-a" || alice.Breakdown[0].ModelName != "gpt-4o" {
		t.Fatalf("breakdown: %+v", alice.Breakdown)
	}
}

// 未填倍率渠道：成本 0、Priced=false；渠道维度带 user_count。
func TestFoldCostCube_ChannelDimensionUnpriced(t *testing.T) {
	rows := foldCostCube(seedCube(), costDimChannel, testChannels(), 7.0)
	var ch7 *costDimensionRow
	for i := range rows {
		if rows[i].ChannelId == 7 {
			ch7 = &rows[i]
		}
	}
	if ch7 == nil {
		t.Fatal("channel 7 missing")
	}
	if ch7.Priced || ch7.CostCny != 0 {
		t.Fatalf("unpriced channel must cost 0: %+v", ch7)
	}
	if ch7.UserCount != 1 {
		t.Fatalf("user_count = %d, want 1", ch7.UserCount)
	}
	if ch7.ChannelName != "nopriced" {
		t.Fatalf("name = %q", ch7.ChannelName)
	}
}

// 三维度总计一致：Σ用户 == Σ模型 == Σ渠道（收入与成本都相等）。
func TestFoldCostCube_DimensionTotalsAgree(t *testing.T) {
	cube := seedCube()
	chs := testChannels()
	sum := func(dim string) (rev, cost float64) {
		for _, r := range foldCostCube(cube, dim, chs, 7.0) {
			rev += r.RevenueCny
			cost += r.CostCny
		}
		return
	}
	r1, c1 := sum(costDimUser)
	r2, c2 := sum(costDimModel)
	r3, c3 := sum(costDimChannel)
	if r1 != r2 || r2 != r3 || c1 != c2 || c2 != c3 {
		t.Fatalf("totals disagree: rev(%v,%v,%v) cost(%v,%v,%v)", r1, r2, r3, c1, c2, c3)
	}
}

// 收入为 0 时利润率必须为 0（不得 NaN/Inf）。
func TestCostMoney_ZeroRevenueRate(t *testing.T) {
	m := costMoneyFromParts(0, 0, 0, 0, 0, 0, 2.5, 7.0)
	if m.ProfitRate != 0 {
		t.Fatalf("rate = %v, want 0", m.ProfitRate)
	}
}
