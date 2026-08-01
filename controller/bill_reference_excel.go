package controller

import (
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/xuri/excelize/v2"
)

const (
	billByDaySheetPrefix   = "按日汇总"
	billByTokenSheetPrefix = "按令牌汇总"
	billByModelSheetPrefix = "按模型汇总"
	billTsFormat           = "2006-01-02 15:04:05"
)

func billRefTs(ts int64) string {
	if ts == 0 {
		return ""
	}
	return time.Unix(ts, 0).Format(billTsFormat)
}

// writeBillReferenceSheets 输出 按日/按令牌/按模型 三个汇总 sheet（参考账单
// 版式）。withUser=true（internal 模式）时 按令牌/按模型 增加用户名首列。
func writeBillReferenceSheets(f *excelize.File, styles billExcelStyles, ref *billRefAgg, withUser bool, exchangeRate float64) error {
	money := func(v float64) excelize.Cell {
		return excelize.Cell{Value: roundTo6(v), StyleID: styles.money}
	}
	numRow := func(r *billRefRow) []any {
		return []any{
			r.BillingRecords, r.RequestCount,
			r.PromptTokens, r.CompletionTokens, r.CacheReadTokens, r.CacheCreationTokens,
			r.Quota, money(r.ListQuota / common.QuotaPerUnit), money(float64(r.Quota) / common.QuotaPerUnit),
		}
	}

	// 按日汇总（恒为自然日，不受 granularity 参数影响）
	days, byDay := ref.byDay()
	dayRows := make([][]any, 0, len(days))
	for _, d := range days {
		dayRows = append(dayRows, append([]any{d}, numRow(byDay[d])...))
	}
	dayLayout := billSheetLayout{
		prefix: billByDaySheetPrefix,
		headers: []string{"日期", "计费记录", "请求数", "输入tokens", "输出tokens",
			"缓存读取tokens", "缓存创建tokens", "Quota Units", "刊例价金额(美元)", "汇总金额(美元)"},
		widths: []float64{14, 10, 10, 12, 12, 16, 16, 14, 16, 16},
	}
	if err := writeBillRowsSheet(f, dayLayout, styles, dayRows); err != nil {
		return err
	}

	// 按令牌汇总 / 按模型汇总（同构）
	dimLayout := func(prefix, nameHeader string) billSheetLayout {
		headers := []string{nameHeader, "计费记录", "请求数", "输入tokens", "输出tokens",
			"缓存读取tokens", "缓存创建tokens", "Quota Units", "刊例价金额(美元)", "汇总金额(美元)",
			"首笔计费时间", "末笔计费时间"}
		widths := []float64{22, 10, 10, 12, 12, 16, 16, 14, 16, 16, 20, 20}
		if withUser {
			headers = append([]string{"用户名"}, headers...)
			widths = append([]float64{16}, widths...)
		}
		return billSheetLayout{prefix: prefix, headers: headers, widths: widths}
	}
	writeDim := func(prefix, nameHeader string, keys []billRefDimKey, m map[billRefDimKey]*billRefRow) error {
		rows := make([][]any, 0, len(keys))
		for _, k := range keys {
			r := m[k]
			row := make([]any, 0, 14)
			if withUser {
				row = append(row, k.Username)
			}
			row = append(row, k.Name)
			row = append(row, numRow(r)...)
			row = append(row, billRefTs(r.FirstTs), billRefTs(r.LastTs))
			rows = append(rows, row)
		}
		return writeBillRowsSheet(f, dimLayout(prefix, nameHeader), styles, rows)
	}
	tokKeys, tokMap := ref.byToken(withUser)
	if err := writeDim(billByTokenSheetPrefix, "令牌名称", tokKeys, tokMap); err != nil {
		return err
	}
	modKeys, modMap := ref.byModel(withUser)
	return writeDim(billByModelSheetPrefix, "模型名称", modKeys, modMap)
}
