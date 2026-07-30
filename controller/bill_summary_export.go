package controller

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
	"github.com/xuri/excelize/v2"
)

type billExportParams struct {
	startTimestamp int64
	endTimestamp   int64
	username       string
	channel        int
	tokenName      string
	modelName      string
	group          string
	withDetail     bool
	detailSplit    bool
	external       bool
	granularity    string
	exchangeRate   float64
}

func parseBillExportParams(c *gin.Context) billExportParams {
	start, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	end, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	channel, _ := strconv.Atoi(c.Query("channel"))
	rate := operation_setting.USDExchangeRate
	if raw := strings.TrimSpace(c.Query("exchange_rate")); raw != "" {
		if v, err := strconv.ParseFloat(raw, 64); err == nil && v > 0 {
			rate = v
		}
	}
	return billExportParams{
		startTimestamp: start,
		endTimestamp:   end,
		username:       c.Query("username"),
		channel:        channel,
		tokenName:      c.Query("token_name"),
		modelName:      c.Query("model_name"),
		group:          c.Query("group"),
		withDetail:     c.Query("with_detail") == "1",
		detailSplit:    c.Query("detail_split_model") == "1",
		external:       c.Query("bill_mode") == "external",
		granularity:    normalizeBillGranularity(c.Query("granularity")),
		exchangeRate:   rate,
	}
}

// runBillExport 执行一次流式扫描并输出 xlsx。streamFn 由调用方绑定 admin/self 版本。
func runBillExport(c *gin.Context, p billExportParams,
	streamFn func(maxRows int, consume func([]*model.Log) error) (bool, error)) {

	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	agg := newBillSummaryAgg()
	agg.external = p.external
	agg.granularity = p.granularity
	var detail *billDetailWriter
	if p.withDetail {
		var err error
		detail, err = newBillDetailWriter(f, p.detailSplit)
		if err != nil {
			common.ApiError(c, err)
			return
		}
	}

	maxRows := model.LogExportMaxRows("xlsx")
	truncated, err := streamFn(maxRows, func(batch []*model.Log) error {
		agg.addBatch(batch)
		if detail != nil {
			return detail.addBatch(batch)
		}
		return nil
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if detail != nil {
		if err := detail.finish(); err != nil {
			common.ApiError(c, err)
			return
		}
	}
	if err := writeBillSummarySheets(f, agg, p.exchangeRate); err != nil {
		common.ApiError(c, err)
		return
	}

	finalizeBillWorkbook(f)

	if truncated {
		c.Writer.Header().Set("X-Export-Truncated", "1")
		c.Writer.Header().Set("X-Export-Max-Rows", strconv.Itoa(maxRows))
	}
	filename := "bill-summary-" + time.Now().Format("20060102-150405") + ".xlsx"
	c.Writer.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	c.Writer.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.WriteHeader(http.StatusOK)
	if err := f.Write(c.Writer); err != nil {
		common.SysError(fmt.Sprintf("bill summary xlsx write failed uid=%d: %v", c.GetInt("id"), err))
	}
}

// ExportBillSummaryAll 管理员：可按 username / channel 查任意用户。
func ExportBillSummaryAll(c *gin.Context) {
	p := parseBillExportParams(c)
	runBillExport(c, p, func(maxRows int, consume func([]*model.Log) error) (bool, error) {
		return model.GetAllLogsForExport(model.LogTypeUnknown, p.startTimestamp, p.endTimestamp,
			p.modelName, p.username, p.tokenName, p.channel, p.group, "", maxRows, consume)
	})
}

// ExportBillSummarySelf 普通用户：忽略 username/channel，强制锁定本人。
func ExportBillSummarySelf(c *gin.Context) {
	if !common.LogExportEnabled {
		c.JSON(http.StatusForbidden, gin.H{"success": false, "message": "导出功能已关闭"})
		return
	}
	userId := c.GetInt("id")
	p := parseBillExportParams(c)
	p.username = ""
	p.channel = 0
	runBillExport(c, p, func(maxRows int, consume func([]*model.Log) error) (bool, error) {
		return model.GetUserLogsForExport(userId, model.LogTypeUnknown, p.startTimestamp, p.endTimestamp,
			p.modelName, p.tokenName, p.group, "", maxRows, consume)
	})
}

// finalizeBillWorkbook removes the default Sheet1, moves the summary sheets
// (总对账单*, 明细对账单*) in front of the daily detail sheets — they are created
// after the streamed detail sheets, but must appear first in the tab order —
// and activates the grand summary.
func finalizeBillWorkbook(f *excelize.File) {
	if err := f.DeleteSheet("Sheet1"); err != nil {
		common.SysLog("bill export: delete default sheet: " + err.Error())
	}
	var summaries []string
	firstOther := ""
	for _, name := range f.GetSheetList() {
		if strings.HasPrefix(name, billGrandSheetPrefix) || strings.HasPrefix(name, billDailySheetPrefix) {
			summaries = append(summaries, name)
		} else if firstOther == "" {
			firstOther = name
		}
	}
	if firstOther != "" {
		// Moving each summary before the first detail sheet keeps their
		// creation order: 总对账单, 总对账单 (2)…, 明细对账单, 明细对账单 (2)…
		for _, name := range summaries {
			if err := f.MoveSheet(name, firstOther); err != nil {
				common.SysLog("bill export: move sheet " + name + ": " + err.Error())
			}
		}
	}
	if idx, err := f.GetSheetIndex(billGrandSheetPrefix); err == nil && idx >= 0 {
		f.SetActiveSheet(idx)
	}
}
