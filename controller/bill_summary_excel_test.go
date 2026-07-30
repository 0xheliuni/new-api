package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/xuri/excelize/v2"
)

func assertCellNotText(t *testing.T, f *excelize.File, sheet, cell string) {
	t.Helper()
	typ, err := f.GetCellType(sheet, cell)
	if err != nil {
		t.Fatalf("GetCellType(%s!%s): %v", sheet, cell, err)
	}
	if typ == excelize.CellTypeInlineString || typ == excelize.CellTypeSharedString {
		t.Fatalf("%s!%s written as text, want numeric", sheet, cell)
	}
}

func TestWriteBillSummarySheets_InternalValues(t *testing.T) {
	agg := newBillSummaryAgg()
	agg.rows[billSummaryKey{Day: "2026-06-01", Username: "alice", ChannelId: 3, TokenName: "tk", ModelName: "gpt-4o"}] =
		&billSummaryRow{Quota: 1500, PromptTokens: 12, CompletionTokens: 6, CacheReadTokens: 5, CacheCreationTokens: 3}
	agg.rows[billSummaryKey{Day: "2026-06-02", Username: "alice", ChannelId: 3, TokenName: "tk", ModelName: "gpt-4o"}] =
		&billSummaryRow{Quota: 500, PromptTokens: 2, CompletionTokens: 1, CacheReadTokens: 1, CacheCreationTokens: 0}

	f := excelize.NewFile()
	defer f.Close()
	if err := writeBillSummarySheets(f, agg, 7.3); err != nil {
		t.Fatal(err)
	}

	// ---- 明细对账单 (daily) ----
	daily := billDailySheetPrefix
	h, _ := f.GetCellValue(daily, "A1")
	if h != "日期" {
		t.Fatalf("daily A1 = %q, want 日期", h)
	}
	ch, _ := f.GetCellValue(daily, "C1")
	if ch != "渠道ID" {
		t.Fatalf("daily C1 = %q, want 渠道ID", ch)
	}
	// Day DESC: row2 = 06-02, row3 = 06-01
	d2, _ := f.GetCellValue(daily, "A2")
	if d2 != "2026-06-02" {
		t.Fatalf("daily A2 = %q, want 2026-06-02", d2)
	}
	// new pricing columns sit between 模型名称 and 汇总金额(美元)
	lp, _ := f.GetCellValue(daily, "F1")
	er, _ := f.GetCellValue(daily, "G1")
	if lp != "刊例价" || er != "专属倍率" {
		t.Fatalf("daily F1/G1 = %q/%q, want 刊例价/专属倍率", lp, er)
	}
	// rows filled directly (no Other) → pricing cells stay empty, not 0
	lpv, _ := f.GetCellValue(daily, "F3")
	if lpv != "" {
		t.Fatalf("daily F3 (list price, no Other) = %q, want empty", lpv)
	}
	// quota 1500 => $0.003, six-decimal number format
	usd, _ := f.GetCellValue(daily, "H3")
	if usd != "0.003000" {
		t.Fatalf("daily H3 (USD) = %q, want 0.003000", usd)
	}
	assertCellNotText(t, f, daily, "H3")
	rate, _ := f.GetCellValue(daily, "I3")
	if rate != "7.3" {
		t.Fatalf("daily I3 (rate) = %q, want 7.3", rate)
	}
	assertCellNotText(t, f, daily, "I3")
	cny, _ := f.GetCellValue(daily, "J3")
	if cny != "0.021900" {
		t.Fatalf("daily J3 (CNY) = %q, want 0.021900", cny)
	}
	prompt, _ := f.GetCellValue(daily, "K3")
	if prompt != "12" {
		t.Fatalf("daily K3 (prompt) = %q, want 12", prompt)
	}
	assertCellNotText(t, f, daily, "K3")
	// no totals row after the last data row
	after, _ := f.GetCellValue(daily, "A4")
	if after != "" {
		t.Fatalf("daily A4 = %q, want empty (totals row removed)", after)
	}

	// ---- 总对账单 (grand) ----
	grand := billGrandSheetPrefix
	gs, _ := f.GetCellValue(grand, "A1")
	if gs != "开始日期" {
		t.Fatalf("grand A1 = %q, want 开始日期", gs)
	}
	from, _ := f.GetCellValue(grand, "A2")
	to, _ := f.GetCellValue(grand, "B2")
	if from != "2026-06-01" || to != "2026-06-02" {
		t.Fatalf("grand range = %q..%q, want 2026-06-01..2026-06-02", from, to)
	}
	gm, _ := f.GetCellValue(grand, "E2")
	if gm != "gpt-4o" {
		t.Fatalf("grand E2 (model) = %q, want gpt-4o", gm)
	}
	// whole-range quota 2000 => $0.004
	gu, _ := f.GetCellValue(grand, "F2")
	if gu != "0.004000" {
		t.Fatalf("grand F2 (USD) = %q, want 0.004000", gu)
	}
	assertCellNotText(t, f, grand, "F2")
	gp, _ := f.GetCellValue(grand, "I2")
	if gp != "14" {
		t.Fatalf("grand I2 (prompt) = %q, want 14", gp)
	}
	// single grand row, no totals row
	g3, _ := f.GetCellValue(grand, "A3")
	if g3 != "" {
		t.Fatalf("grand A3 = %q, want empty", g3)
	}
}

func TestWriteBillSummarySheets_ExternalMergesChannelsAndTokens(t *testing.T) {
	agg := newBillSummaryAgg()
	agg.external = true
	agg.addBatch([]*model.Log{
		{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-01", 10), Username: "alice", ChannelId: 3, TokenName: "tk1", ModelName: "gpt-4o",
			Quota: 1000, PromptTokens: 10, CompletionTokens: 5},
		{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-01", 11), Username: "alice", ChannelId: 7, TokenName: "tk2", ModelName: "gpt-4o",
			Quota: 500, PromptTokens: 2, CompletionTokens: 1},
	})
	if got := len(agg.sortedKeys()); got != 1 {
		t.Fatalf("external groups = %d, want 1 (channel/token merged)", got)
	}

	f := excelize.NewFile()
	defer f.Close()
	if err := writeBillSummarySheets(f, agg, 7.3); err != nil {
		t.Fatal(err)
	}

	daily := billDailySheetPrefix
	// external layout: no 渠道ID / 令牌名称 columns
	c1, _ := f.GetCellValue(daily, "C1")
	if c1 != "模型名称" {
		t.Fatalf("external daily C1 = %q, want 模型名称", c1)
	}
	model2, _ := f.GetCellValue(daily, "C2")
	if model2 != "gpt-4o" {
		t.Fatalf("external daily C2 = %q, want gpt-4o", model2)
	}
	// merged quota 1500 => $0.003 (USD moved to F with the two pricing columns)
	usd, _ := f.GetCellValue(daily, "F2")
	if usd != "0.003000" {
		t.Fatalf("external daily F2 (USD) = %q, want 0.003000", usd)
	}
	// only one merged data row
	a3, _ := f.GetCellValue(daily, "A3")
	if a3 != "" {
		t.Fatalf("external daily A3 = %q, want empty (rows merged)", a3)
	}

	grand := billGrandSheetPrefix
	gd, _ := f.GetCellValue(grand, "D1")
	if gd != "模型名称" {
		t.Fatalf("external grand D1 = %q, want 模型名称", gd)
	}
	gu, _ := f.GetCellValue(grand, "E2")
	if gu != "0.003000" {
		t.Fatalf("external grand E2 (USD) = %q, want 0.003000", gu)
	}
}

func TestBillDetailWriter_CostIsNumeric(t *testing.T) {
	f := excelize.NewFile()
	defer f.Close()
	w, err := newBillDetailWriter(f, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.addBatch([]*model.Log{
		{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-01", 10), Username: "alice", TokenName: "tk", ModelName: "gpt-4o",
			Quota: 1500, PromptTokens: 10, CompletionTokens: 5},
	}); err != nil {
		t.Fatal(err)
	}
	if err := w.finish(); err != nil {
		t.Fatal(err)
	}
	// billDetailColumns: cost is the 11th column (K)
	cost, _ := f.GetCellValue("2026-06-01", "K2")
	if cost != "$0.003000" {
		t.Fatalf("detail K2 (cost) = %q, want $0.003000", cost)
	}
	assertCellNotText(t, f, "2026-06-01", "K2")
}

func TestWriteBillSummarySheets_MonthGranularity(t *testing.T) {
	agg := newBillSummaryAgg()
	agg.granularity = "month"
	agg.addBatch([]*model.Log{
		{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-03", 10), Username: "alice", ChannelId: 3, TokenName: "tk", ModelName: "gpt-4o",
			Quota: 1000, PromptTokens: 10, CompletionTokens: 5},
		{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-07", 11), Username: "alice", ChannelId: 3, TokenName: "tk", ModelName: "gpt-4o",
			Quota: 500, PromptTokens: 2, CompletionTokens: 1},
	})

	f := excelize.NewFile()
	defer f.Close()
	if err := writeBillSummarySheets(f, agg, 7.3); err != nil {
		t.Fatal(err)
	}

	// daily sheet buckets by month
	day, _ := f.GetCellValue(billDailySheetPrefix, "A2")
	if day != "2026-06" {
		t.Fatalf("month-bucket daily A2 = %q, want 2026-06", day)
	}
	usd, _ := f.GetCellValue(billDailySheetPrefix, "H2")
	if usd != "0.003000" {
		t.Fatalf("month-bucket daily H2 = %q, want 0.003000", usd)
	}
	// grand range stays in real calendar days regardless of granularity
	from, _ := f.GetCellValue(billGrandSheetPrefix, "A2")
	to, _ := f.GetCellValue(billGrandSheetPrefix, "B2")
	if from != "2026-06-03" || to != "2026-06-07" {
		t.Fatalf("grand range = %q..%q, want 2026-06-03..2026-06-07", from, to)
	}
}

func TestWriteBillSummarySheets_WeekLabel(t *testing.T) {
	agg := newBillSummaryAgg()
	agg.granularity = "week"
	agg.addBatch([]*model.Log{
		{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-03", 10), Username: "alice", ChannelId: 3, TokenName: "tk", ModelName: "gpt-4o",
			Quota: 1000, PromptTokens: 10, CompletionTokens: 5},
	})

	f := excelize.NewFile()
	defer f.Close()
	if err := writeBillSummarySheets(f, agg, 7.3); err != nil {
		t.Fatal(err)
	}
	day, _ := f.GetCellValue(billDailySheetPrefix, "A2")
	if day != "2026-06-01 ~ 2026-06-07" {
		t.Fatalf("week label A2 = %q, want 2026-06-01 ~ 2026-06-07", day)
	}
}

// TestWriteBillSummarySheets_ListPriceAndRatioColumns covers the 刊例价/专属倍率
// columns: token-ratio pricing, per-call pricing, video unit price, the
// user_group_ratio-over-group_ratio priority, and the -1 sentinel fallback.
func TestWriteBillSummarySheets_ListPriceAndRatioColumns(t *testing.T) {
	agg := newBillSummaryAgg()
	agg.addBatch([]*model.Log{
		// token 模型：刊例价 = model_ratio×2 = 20；user_group_ratio=-1 → 回退 group_ratio 0.8
		{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-01", 10), Username: "alice", ChannelId: 3, TokenName: "tk", ModelName: "a-model",
			Quota: 1000, PromptTokens: 10,
			Other: `{"model_ratio":10,"group_ratio":0.8,"user_group_ratio":-1}`},
		// 专属倍率优先：user_group_ratio=0.5 覆盖 group_ratio=1
		{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-01", 10), Username: "alice", ChannelId: 3, TokenName: "tk", ModelName: "b-model",
			Quota: 1000, PromptTokens: 10,
			Other: `{"model_ratio":5,"group_ratio":1,"user_group_ratio":0.5}`},
		// 视频任务：刊例价 = video_unit_price
		{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-01", 10), Username: "alice", ChannelId: 3, TokenName: "tk", ModelName: "c-video",
			Quota: 1000,
			Other: `{"model_ratio":10,"group_ratio":1,"video_unit_price":40}`},
		// 按次计费：刊例价 = model_price
		{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-01", 10), Username: "alice", ChannelId: 3, TokenName: "tk", ModelName: "d-percall",
			Quota: 1000,
			Other: `{"model_price":0.1,"group_ratio":1}`},
		// 旧日志无 Other：两列留空
		{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-01", 10), Username: "alice", ChannelId: 3, TokenName: "tk", ModelName: "e-legacy",
			Quota: 1000, Other: ``},
	})

	f := excelize.NewFile()
	defer f.Close()
	if err := writeBillSummarySheets(f, agg, 7.3); err != nil {
		t.Fatal(err)
	}

	daily := billDailySheetPrefix
	// rows sorted by model name ASC within the day: a,b,c,d,e → rows 2..6
	get := func(cell string) string {
		v, _ := f.GetCellValue(daily, cell)
		return v
	}
	if get("F2") != "20" || get("G2") != "0.8" {
		t.Fatalf("a-model price/ratio = %q/%q, want 20/0.8 (sentinel -1 falls back)", get("F2"), get("G2"))
	}
	if get("F3") != "10" || get("G3") != "0.5" {
		t.Fatalf("b-model price/ratio = %q/%q, want 10/0.5 (user_group_ratio wins)", get("F3"), get("G3"))
	}
	if get("F4") != "40" {
		t.Fatalf("c-video list price = %q, want 40 (video_unit_price)", get("F4"))
	}
	if get("F5") != "0.1" {
		t.Fatalf("d-percall list price = %q, want 0.1 (model_price)", get("F5"))
	}
	if get("F6") != "" || get("G6") != "" {
		t.Fatalf("e-legacy price/ratio = %q/%q, want empty (no Other)", get("F6"), get("G6"))
	}
	assertCellNotText(t, f, daily, "F2")
	assertCellNotText(t, f, daily, "G2")
}

// TestBillSummaryAgg_PricingFirstSeenWins: 流式 DESC 顺序下组内最新一条日志的
// 定价胜出，旧值不覆盖。
func TestBillSummaryAgg_PricingFirstSeenWins(t *testing.T) {
	agg := newBillSummaryAgg()
	agg.addBatch([]*model.Log{
		// 最新一条（DESC 首见）：ratio 0.5
		{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-01", 12), Username: "alice", ChannelId: 3, TokenName: "tk", ModelName: "m",
			Quota: 100, Other: `{"model_ratio":10,"group_ratio":0.5}`},
		// 更早一条：ratio 1（不应覆盖）
		{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-01", 10), Username: "alice", ChannelId: 3, TokenName: "tk", ModelName: "m",
			Quota: 100, Other: `{"model_ratio":8,"group_ratio":1}`},
	})
	keys := agg.sortedKeys()
	if len(keys) != 1 {
		t.Fatalf("groups = %d, want 1", len(keys))
	}
	r := agg.rows[keys[0]]
	if !r.hasPrice || r.ListPriceUSD != 20 {
		t.Fatalf("list price = %v (has=%v), want 20 from newest log", r.ListPriceUSD, r.hasPrice)
	}
	if !r.hasRatio || r.EffectiveRatio != 0.5 {
		t.Fatalf("ratio = %v (has=%v), want 0.5 from newest log", r.EffectiveRatio, r.hasRatio)
	}
}
