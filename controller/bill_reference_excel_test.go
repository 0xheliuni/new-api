package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/xuri/excelize/v2"
)

func TestWriteBillReferenceSheets(t *testing.T) {
	ref := newBillRefAgg()
	ref.addBatch([]*model.Log{
		{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-02", 12), Username: "u1", TokenName: "tk1", ModelName: "m1",
			Quota: 1000, PromptTokens: 10, CompletionTokens: 5, Other: `{"model_ratio":10,"group_ratio":0.5}`},
		{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-01", 10), Username: "u1", TokenName: "tk2", ModelName: "m2",
			Quota: 500, PromptTokens: 2, CompletionTokens: 1, Other: `{"group_ratio":1}`},
	})

	f := excelize.NewFile()
	defer f.Close()
	styles, err := newBillExcelStyles(f)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeBillReferenceSheets(f, styles, ref, false, 7.3); err != nil {
		t.Fatal(err)
	}

	get := func(sheet, cell string) string {
		v, _ := f.GetCellValue(sheet, cell)
		return v
	}
	// 按日汇总：day DESC
	if get(billByDaySheetPrefix, "A1") != "日期" || get(billByDaySheetPrefix, "A2") != "2026-06-02" || get(billByDaySheetPrefix, "A3") != "2026-06-01" {
		t.Fatalf("byDay rows: %q/%q/%q", get(billByDaySheetPrefix, "A1"), get(billByDaySheetPrefix, "A2"), get(billByDaySheetPrefix, "A3"))
	}
	if get(billByDaySheetPrefix, "B2") != "1" || get(billByDaySheetPrefix, "C2") != "1" {
		t.Fatalf("byDay counts = %q/%q", get(billByDaySheetPrefix, "B2"), get(billByDaySheetPrefix, "C2"))
	}
	if get(billByDaySheetPrefix, "H2") != "1000" { // Quota Units 原生整数
		t.Fatalf("byDay quota units = %q, want 1000", get(billByDaySheetPrefix, "H2"))
	}
	if get(billByDaySheetPrefix, "I2") != "0.004000" { // list 2000/500000
		t.Fatalf("byDay list amount = %q, want 0.004000", get(billByDaySheetPrefix, "I2"))
	}
	if get(billByDaySheetPrefix, "J2") != "0.002000" {
		t.Fatalf("byDay amount = %q, want 0.002000", get(billByDaySheetPrefix, "J2"))
	}
	assertCellNotText(t, f, billByDaySheetPrefix, "I2")

	// 按令牌汇总：withUser=false 无用户名列；Quota DESC → tk1 在前；首/末笔时间格式
	if get(billByTokenSheetPrefix, "A1") != "令牌名称" || get(billByTokenSheetPrefix, "A2") != "tk1" || get(billByTokenSheetPrefix, "A3") != "tk2" {
		t.Fatalf("byToken rows: %q/%q/%q", get(billByTokenSheetPrefix, "A1"), get(billByTokenSheetPrefix, "A2"), get(billByTokenSheetPrefix, "A3"))
	}
	if get(billByTokenSheetPrefix, "K1") != "首笔计费时间" || get(billByTokenSheetPrefix, "L1") != "末笔计费时间" {
		t.Fatalf("byToken ts headers: %q/%q", get(billByTokenSheetPrefix, "K1"), get(billByTokenSheetPrefix, "L1"))
	}
	if get(billByTokenSheetPrefix, "K2") != "2026-06-02 12:00:00" {
		t.Fatalf("byToken first ts = %q", get(billByTokenSheetPrefix, "K2"))
	}

	// 按模型汇总
	if get(billByModelSheetPrefix, "A1") != "模型名称" || get(billByModelSheetPrefix, "A2") != "m1" {
		t.Fatalf("byModel: %q/%q", get(billByModelSheetPrefix, "A1"), get(billByModelSheetPrefix, "A2"))
	}
}

func TestWriteBillReferenceSheets_WithUserColumn(t *testing.T) {
	ref := newBillRefAgg()
	ref.addBatch([]*model.Log{
		{Type: model.LogTypeConsume, CreatedAt: tsOn("2026-06-01", 10), Username: "u1", TokenName: "tk1", ModelName: "m1",
			Quota: 100, Other: `{"group_ratio":1}`},
	})
	f := excelize.NewFile()
	defer f.Close()
	styles, _ := newBillExcelStyles(f)
	if err := writeBillReferenceSheets(f, styles, ref, true, 7.3); err != nil {
		t.Fatal(err)
	}
	u, _ := f.GetCellValue(billByTokenSheetPrefix, "A1")
	n, _ := f.GetCellValue(billByTokenSheetPrefix, "B1")
	if u != "用户名" || n != "令牌名称" {
		t.Fatalf("byToken withUser headers = %q/%q", u, n)
	}
}
