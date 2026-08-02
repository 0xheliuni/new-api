package controller

import (
	"testing"
)

func TestBuildCostOverview_TrendAndStackAndWarning(t *testing.T) {
	cube := seedCube() // 2 users, 2 channels(3 priced, 7 unpriced), same day
	ov := buildCostOverview(cube, testChannels(), 7.0)
	if len(ov.Trend) != 1 || ov.Trend[0].Date != "2026-06-01" {
		t.Fatalf("trend: %+v", ov.Trend)
	}
	// stack: 每渠道一条（同一天）
	if len(ov.CostStack) != 2 {
		t.Fatalf("stack: %+v", ov.CostStack)
	}
	if ov.UnpricedChannelCount != 1 {
		t.Fatalf("unpriced = %d, want 1", ov.UnpricedChannelCount)
	}
	// 总收入 = (800+500)/5e5*7
	if ov.Totals.RevenueCny != 0.0182 {
		t.Fatalf("revenue_cny = %v", ov.Totals.RevenueCny)
	}
	// 成本 = 1000/5e5*2.5 = 0.005（ch7 为 0）
	if ov.Totals.CostCny != 0.005 {
		t.Fatalf("cost_cny = %v", ov.Totals.CostCny)
	}
}

func TestPaginateCostRows(t *testing.T) {
	rows := foldCostCube(seedCube(), costDimUser, testChannels(), 7.0)
	page := paginateCostRows(rows, 1, 1)
	if page.Total != 2 || len(page.Items) != 1 {
		t.Fatalf("page: total=%d items=%d", page.Total, len(page.Items))
	}
	// 排序：收入降序 → alice(0.0112) 先于 bob(0.007)
	if page.Items[0].Username != "alice" {
		t.Fatalf("first = %q", page.Items[0].Username)
	}
	page2 := paginateCostRows(rows, 2, 1)
	if len(page2.Items) != 1 || page2.Items[0].Username != "bob" {
		t.Fatalf("page2: %+v", page2.Items)
	}
	// 总计行独立于分页
	if page.Summary.RevenueCny != page2.Summary.RevenueCny {
		t.Fatal("summary must be page-independent")
	}
}
