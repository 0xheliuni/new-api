package controller

import (
	"fmt"
	"math"
	"sort"

	"github.com/QuantumNous/new-api/common"
	"github.com/xuri/excelize/v2"
)

const (
	billGrandSheetPrefix = "总对账单"
	billDailySheetPrefix = "明细对账单"
)

// billMoneyNumFmt keeps the six-decimal display of v1 while the cell stays a
// real number (so Excel can sum the column directly).
const billMoneyNumFmt = "0.000000"

func roundTo6(v float64) float64 {
	return math.Round(v*1e6) / 1e6
}

// billSheetLayout is one summary sheet's shape; headers/widths depend on the
// export mode (external drops the channel/token columns).
type billSheetLayout struct {
	prefix  string
	headers []string
	widths  []float64
}

func billGrandLayout(external bool) billSheetLayout {
	if external {
		return billSheetLayout{
			prefix: billGrandSheetPrefix,
			headers: []string{
				"开始日期", "结束日期", "用户名", "模型名称",
				"计费记录", "请求数", "刊例价金额(美元)",
				"汇总金额(美元)", "汇率", "汇总金额(人民币)",
				"输入tokens", "输出tokens", "缓存读取tokens", "缓存创建tokens",
			},
			widths: []float64{12, 12, 16, 22, 10, 10, 16, 16, 8, 18, 12, 12, 16, 16},
		}
	}
	return billSheetLayout{
		prefix: billGrandSheetPrefix,
		headers: []string{
			"开始日期", "结束日期", "用户名", "渠道ID", "模型名称",
			"计费记录", "请求数", "刊例价金额(美元)",
			"汇总金额(美元)", "汇率", "汇总金额(人民币)",
			"输入tokens", "输出tokens", "缓存读取tokens", "缓存创建tokens",
		},
		widths: []float64{12, 12, 16, 10, 22, 10, 10, 16, 16, 8, 18, 12, 12, 16, 16},
	}
}

func billDailyLayout(external bool) billSheetLayout {
	if external {
		return billSheetLayout{
			prefix: billDailySheetPrefix,
			headers: []string{
				"日期", "用户名", "模型名称", "刊例价", "专属倍率",
				"计费记录", "请求数", "刊例价金额(美元)",
				"汇总金额(美元)", "汇率", "汇总金额(人民币)",
				"输入tokens", "输出tokens", "缓存读取tokens", "缓存创建tokens",
			},
			widths: []float64{14, 16, 22, 10, 10, 10, 10, 16, 16, 8, 18, 12, 12, 16, 16},
		}
	}
	return billSheetLayout{
		prefix: billDailySheetPrefix,
		headers: []string{
			"日期", "用户名", "渠道ID", "令牌名称", "模型名称", "刊例价", "专属倍率",
			"计费记录", "请求数", "刊例价金额(美元)",
			"汇总金额(美元)", "汇率", "汇总金额(人民币)",
			"输入tokens", "输出tokens", "缓存读取tokens", "缓存创建tokens",
		},
		widths: []float64{14, 16, 10, 16, 22, 10, 10, 10, 10, 16, 16, 8, 18, 12, 12, 16, 16},
	}
}

type billExcelStyles struct {
	header int
	money  int
}

func newBillExcelStyles(f *excelize.File) (billExcelStyles, error) {
	header, err := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}})
	if err != nil {
		return billExcelStyles{}, err
	}
	numFmt := billMoneyNumFmt
	money, err := f.NewStyle(&excelize.Style{CustomNumFmt: &numFmt})
	if err != nil {
		return billExcelStyles{}, err
	}
	return billExcelStyles{header: header, money: money}, nil
}

// writeBillRowsSheet streams pre-built rows into sheets named by layout.prefix,
// rolling to "<prefix> (2)" ... past excelSingleSheetSoftCap rows.
func writeBillRowsSheet(f *excelize.File, layout billSheetLayout, styles billExcelStyles, rows [][]any) error {
	var (
		sw       *excelize.StreamWriter
		sheetIdx int
		rowInSt  int
	)

	newSheet := func() error {
		if sw != nil {
			if err := sw.Flush(); err != nil {
				return err
			}
		}
		name := layout.prefix
		if sheetIdx > 0 {
			name = fmt.Sprintf("%s (%d)", layout.prefix, sheetIdx+1)
		}
		if _, err := f.NewSheet(name); err != nil {
			return err
		}
		s, err := f.NewStreamWriter(name)
		if err != nil {
			return err
		}
		for i, w := range layout.widths {
			if err := s.SetColWidth(i+1, i+1, w); err != nil {
				return err
			}
		}
		header := make([]any, len(layout.headers))
		for i, h := range layout.headers {
			header[i] = excelize.Cell{Value: h, StyleID: styles.header}
		}
		if err := s.SetRow("A1", header); err != nil {
			return err
		}
		sw = s
		rowInSt = 1
		sheetIdx++
		return nil
	}

	if err := newSheet(); err != nil {
		return err
	}
	for _, row := range rows {
		if rowInSt >= excelSingleSheetSoftCap {
			if err := newSheet(); err != nil {
				return err
			}
		}
		cell, err := excelize.CoordinatesToCellName(1, rowInSt+1)
		if err != nil {
			return err
		}
		if err := sw.SetRow(cell, row); err != nil {
			return err
		}
		rowInSt++
	}
	return sw.Flush()
}

// billGrandKey collapses the day/token dimensions: the grand sheet is one row
// per model (per channel too in internal mode) over the whole export range.
type billGrandKey struct {
	Username  string
	ChannelId int
	ModelName string
}

// writeBillSummarySheets derives the grand (whole-range) rows from the daily
// aggregation and writes 总对账单 then 明细对账单. Amount cells are written as
// float64 with a 0.000000 number format; no totals row is appended.
func writeBillSummarySheets(f *excelize.File, agg *billSummaryAgg, exchangeRate float64) error {
	styles, err := newBillExcelStyles(f)
	if err != nil {
		return err
	}
	keys := agg.sortedKeys()

	// Grand aggregation over raw quota (converted once at the end, so the
	// range total doesn't accumulate per-day rounding). The range columns use
	// the real calendar days tracked by addBatch; tests that fill agg.rows
	// directly fall back to the bucket keys.
	grand := make(map[billGrandKey]*billSummaryRow)
	minDay, maxDay := agg.minDay, agg.maxDay
	for _, k := range keys {
		r := agg.rows[k]
		gk := billGrandKey{Username: k.Username, ChannelId: k.ChannelId, ModelName: k.ModelName}
		g := grand[gk]
		if g == nil {
			g = &billSummaryRow{}
			grand[gk] = g
		}
		g.Quota += r.Quota
		g.PromptTokens += r.PromptTokens
		g.CompletionTokens += r.CompletionTokens
		g.CacheReadTokens += r.CacheReadTokens
		g.CacheCreationTokens += r.CacheCreationTokens
		g.BillingRecords += r.BillingRecords
		g.RequestCount += r.RequestCount
		g.ListQuota += r.ListQuota
		if agg.minDay == "" {
			if minDay == "" || k.Day < minDay {
				minDay = k.Day
			}
			if maxDay == "" || k.Day > maxDay {
				maxDay = k.Day
			}
		}
	}
	grandKeys := make([]billGrandKey, 0, len(grand))
	for gk := range grand {
		grandKeys = append(grandKeys, gk)
	}
	sortBillGrandKeys(grandKeys)

	money := func(v float64) excelize.Cell {
		return excelize.Cell{Value: roundTo6(v), StyleID: styles.money}
	}

	grandRows := make([][]any, 0, len(grandKeys))
	for _, gk := range grandKeys {
		g := grand[gk]
		usd := float64(g.Quota) / common.QuotaPerUnit
		row := []any{minDay, maxDay, gk.Username}
		if !agg.external {
			row = append(row, gk.ChannelId)
		}
		row = append(row, gk.ModelName,
			g.BillingRecords, g.RequestCount, money(g.ListQuota/common.QuotaPerUnit),
			money(usd), exchangeRate, money(usd*exchangeRate),
			g.PromptTokens, g.CompletionTokens, g.CacheReadTokens, g.CacheCreationTokens,
		)
		grandRows = append(grandRows, row)
	}
	if err := writeBillRowsSheet(f, billGrandLayout(agg.external), styles, grandRows); err != nil {
		return err
	}

	dailyRows := make([][]any, 0, len(keys))
	for _, k := range keys {
		r := agg.rows[k]
		usd := float64(r.Quota) / common.QuotaPerUnit
		row := []any{billPeriodLabel(k.Day, agg.granularity), k.Username}
		if !agg.external {
			row = append(row, k.ChannelId, k.TokenName)
		}
		// 刊例价/专属倍率：旧日志无定价键时留空，不写 0（避免误读为免费）。
		var listPrice, ratio any
		if r.hasPrice {
			listPrice = r.ListPriceUSD
		}
		if r.hasRatio {
			ratio = r.EffectiveRatio
		}
		row = append(row, k.ModelName, listPrice, ratio,
			r.BillingRecords, r.RequestCount, money(r.ListQuota/common.QuotaPerUnit),
			money(usd), exchangeRate, money(usd*exchangeRate),
			r.PromptTokens, r.CompletionTokens, r.CacheReadTokens, r.CacheCreationTokens,
		)
		dailyRows = append(dailyRows, row)
	}
	return writeBillRowsSheet(f, billDailyLayout(agg.external), styles, dailyRows)
}

func sortBillGrandKeys(keys []billGrandKey) {
	sort.Slice(keys, func(i, j int) bool {
		x, y := keys[i], keys[j]
		if x.Username != y.Username {
			return x.Username < y.Username
		}
		if x.ChannelId != y.ChannelId {
			return x.ChannelId < y.ChannelId
		}
		return x.ModelName < y.ModelName
	})
}
