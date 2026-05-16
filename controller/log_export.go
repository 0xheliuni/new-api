package controller

import (
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
	"github.com/xuri/excelize/v2"
)

// logExportColumn describes one exportable bill column. Frontend "列设置" keys
// are the source of truth, but the bill export filters the user-selected set
// through a strict allow-list defined here. Channel-related and privacy-
// sensitive keys (channel/retry/ip/details) are intentionally absent.
type logExportColumn struct {
	key    string
	header string
	width  float64
}

// logExportAllowedColumns is the closed set of bill columns. Order matters and
// matches what we want the xlsx to look like by default. The frontend may pass
// a `columns` query param to reorder/subset; unknown keys are silently dropped.
var logExportAllowedColumns = []logExportColumn{
	{key: "time", header: "时间", width: 20},
	{key: "username", header: "账户", width: 16},
	{key: "token", header: "令牌", width: 16},
	{key: "group", header: "分组", width: 12},
	{key: "type", header: "类型", width: 10},
	{key: "model", header: "模型", width: 22},
	{key: "use_time", header: "用时(ms)", width: 10},
	{key: "prompt", header: "输入 tokens", width: 12},
	{key: "completion", header: "输出 tokens", width: 12},
	{key: "cost", header: "费用", width: 14},
}

// logExportColumnMap is the indexed view of logExportAllowedColumns.
var logExportColumnMap = func() map[string]logExportColumn {
	m := make(map[string]logExportColumn, len(logExportAllowedColumns))
	for _, c := range logExportAllowedColumns {
		m[c.key] = c
	}
	return m
}()

// logExportDefaultColumnKeys is the fallback when no `columns` param is given.
var logExportDefaultColumnKeys = []string{"time", "username", "token", "group", "type", "model", "prompt", "completion", "cost"}

// Forced columns appended to every export regardless of the user's selection.
var cacheReadColumn = logExportColumn{key: "cache_read", header: "缓存读取 tokens", width: 16}
var cacheCreationColumn = logExportColumn{key: "cache_creation", header: "缓存创建 tokens", width: 16}
var billingColumn = logExportColumn{key: "billing", header: "计费过程", width: 70}
var requestIdColumn = logExportColumn{key: "request_id", header: "请求 ID", width: 36}

// logPricingInfo mirrors the subset of Log.Other we need to rebuild the
// billing breakdown. Field names match the JS source in
// web/src/helpers/render.jsx so the two stay easy to keep in sync.
type logPricingInfo struct {
	ModelRatio            float64 `json:"model_ratio"`
	ModelPrice            float64 `json:"model_price"`
	CompletionRatio       float64 `json:"completion_ratio"`
	GroupRatio            float64 `json:"group_ratio"`
	UserGroupRatio        float64 `json:"user_group_ratio"`
	CacheTokens           int     `json:"cache_tokens"`
	CacheRatio            float64 `json:"cache_ratio"`
	CacheCreationTokens   int     `json:"cache_creation_tokens"`
	CacheCreationRatio    float64 `json:"cache_creation_ratio"`
	CacheCreationTokens5m int     `json:"cache_creation_tokens_5m"`
	CacheCreationRatio5m  float64 `json:"cache_creation_ratio_5m"`
	CacheCreationTokens1h int     `json:"cache_creation_tokens_1h"`
	CacheCreationRatio1h  float64 `json:"cache_creation_ratio_1h"`
}

const billingDisclaimer = "仅供参考，以实际扣费为准"
const billingMissingPlaceholder = "（无计费明细）" + "\n" + billingDisclaimer

// buildBillingText reconstructs the same three-line "price display" billing
// breakdown the frontend shows on the cost column tooltip, plus a disclaimer
// line. The actual total is read straight from Log.Quota to stay aligned with
// what was charged — we only rebuild the per-token math.
func buildBillingText(log *model.Log) string {
	if log == nil {
		return billingMissingPlaceholder
	}
	if strings.TrimSpace(log.Other) == "" {
		return billingMissingPlaceholder
	}
	var info logPricingInfo
	if err := common.UnmarshalJsonStr(log.Other, &info); err != nil {
		return billingMissingPlaceholder
	}

	totalUSD := float64(log.Quota) / common.QuotaPerUnit
	ratioLabel := "分组倍率"
	effectiveRatio := info.GroupRatio
	if isValidGroupRatio(info.UserGroupRatio) {
		ratioLabel = "专属倍率"
		effectiveRatio = info.UserGroupRatio
	}

	// Per-call pricing branch: model_price > 0 means flat-rate per request,
	// not a ratio-based bill. Mirror render.jsx's short-form output.
	if info.ModelPrice > 0 {
		line := fmt.Sprintf("按次计费：$%s * %s %s = $%s",
			formatPrice(info.ModelPrice),
			ratioLabel,
			formatRatio(effectiveRatio),
			formatPrice(totalUSD),
		)
		return line + "\n" + billingDisclaimer
	}

	// Ratio pricing branch needs at least a model_ratio to be meaningful. If
	// it's missing, fall back to the placeholder so we don't emit a bogus
	// "$0.000000 / 1M tokens" line.
	if info.ModelRatio == 0 {
		return billingMissingPlaceholder
	}

	inputUnitPrice := info.ModelRatio * 2.0
	completionUnitPrice := info.ModelRatio * 2.0 * info.CompletionRatio
	cacheUnitPrice := info.ModelRatio * 2.0 * info.CacheRatio
	cacheCreationUnitPrice := info.ModelRatio * 2.0 * info.CacheCreationRatio
	cacheCreationUnitPrice5m := info.ModelRatio * 2.0 * info.CacheCreationRatio5m
	cacheCreationUnitPrice1h := info.ModelRatio * 2.0 * info.CacheCreationRatio1h

	hasSplitCacheCreation := info.CacheCreationTokens5m > 0 || info.CacheCreationTokens1h > 0
	showLegacyCacheCreation := !hasSplitCacheCreation && info.CacheCreationTokens > 0

	segments := []string{
		fmt.Sprintf("提示 %d tokens / 1M tokens * $%s", log.PromptTokens, formatPrice(inputUnitPrice)),
	}
	if info.CacheTokens > 0 {
		segments = append(segments,
			fmt.Sprintf("缓存 %d tokens / 1M tokens * $%s", info.CacheTokens, formatPrice(cacheUnitPrice)),
		)
	}
	if showLegacyCacheCreation {
		segments = append(segments,
			fmt.Sprintf("缓存创建 %d tokens / 1M tokens * $%s", info.CacheCreationTokens, formatPrice(cacheCreationUnitPrice)),
		)
	}
	if hasSplitCacheCreation && info.CacheCreationTokens5m > 0 {
		segments = append(segments,
			fmt.Sprintf("5m缓存创建 %d tokens / 1M tokens * $%s", info.CacheCreationTokens5m, formatPrice(cacheCreationUnitPrice5m)),
		)
	}
	if hasSplitCacheCreation && info.CacheCreationTokens1h > 0 {
		segments = append(segments,
			fmt.Sprintf("1h缓存创建 %d tokens / 1M tokens * $%s", info.CacheCreationTokens1h, formatPrice(cacheCreationUnitPrice1h)),
		)
	}
	segments = append(segments,
		fmt.Sprintf("补全 %d tokens / 1M tokens * $%s", log.CompletionTokens, formatPrice(completionUnitPrice)),
	)

	breakdown := strings.Join(segments, " + ")
	line1 := fmt.Sprintf("输入价格：$%s / 1M tokens", formatPrice(inputUnitPrice))
	line2 := fmt.Sprintf("输出价格：$%s / 1M tokens", formatPrice(completionUnitPrice))
	line3 := fmt.Sprintf("%s * %s %s = $%s", breakdown, ratioLabel, formatRatio(effectiveRatio), formatPrice(totalUSD))

	return line1 + "\n" + line2 + "\n" + line3 + "\n" + billingDisclaimer
}

// isValidGroupRatio mirrors the JS counterpart: a ratio is valid only if it's
// finite and not the sentinel -1.
func isValidGroupRatio(r float64) bool {
	if math.IsNaN(r) || math.IsInf(r, 0) {
		return false
	}
	return r != -1
}

// formatPrice renders a USD amount the way the frontend does — six fractional
// digits — so the export and the page match character-for-character.
func formatPrice(v float64) string {
	return strconv.FormatFloat(v, 'f', 6, 64)
}

// formatRatio drops trailing zeros so common values like 4.27 stay short
// instead of becoming 4.270000.
func formatRatio(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// logTypeLabel maps the integer Log.Type enum to the short Chinese labels
// that match the dropdown on the log page filter.
func logTypeLabel(t int) string {
	switch t {
	case model.LogTypeTopup:
		return "充值"
	case model.LogTypeConsume:
		return "消费"
	case model.LogTypeManage:
		return "管理"
	case model.LogTypeSystem:
		return "系统"
	case model.LogTypeError:
		return "错误"
	case model.LogTypeRefund:
		return "退款"
	default:
		return "未知"
	}
}

// resolveExportColumns picks the column set for one export. It accepts the
// raw `columns` query param (comma-separated frontend keys), drops anything
// not on the allow-list, dedupes, and always appends billing + request_id at
// the end. An empty/all-filtered input falls back to logExportDefaultColumnKeys.
func resolveExportColumns(raw string) []logExportColumn {
	keys := strings.Split(raw, ",")
	seen := make(map[string]bool, len(keys))
	result := make([]logExportColumn, 0, len(keys)+2)

	for _, k := range keys {
		k = strings.TrimSpace(k)
		if k == "" || seen[k] {
			continue
		}
		col, ok := logExportColumnMap[k]
		if !ok {
			continue
		}
		seen[k] = true
		result = append(result, col)
	}

	if len(result) == 0 {
		for _, k := range logExportDefaultColumnKeys {
			if !seen[k] {
				seen[k] = true
				result = append(result, logExportColumnMap[k])
			}
		}
	}

	// Insert cache columns after "completion" and before "cost".
	// Find the insertion point: right after "completion", or right before "cost",
	// or at the end of user-selected columns if neither is present.
	insertIdx := -1
	for i, col := range result {
		if col.key == "completion" {
			insertIdx = i + 1
			break
		}
	}
	if insertIdx == -1 {
		for i, col := range result {
			if col.key == "cost" {
				insertIdx = i
				break
			}
		}
	}
	if insertIdx == -1 {
		insertIdx = len(result)
	}
	// Insert cacheReadColumn and cacheCreationColumn at insertIdx
	forced := []logExportColumn{cacheReadColumn, cacheCreationColumn}
	result = append(result[:insertIdx], append(forced, result[insertIdx:]...)...)

	// Append billing and request_id at the very end
	result = append(result, billingColumn, requestIdColumn)
	return result
}

// cellValue returns the value to write for a given column on a given log row.
// Numeric tokens are returned as int (so Excel treats them as numbers); the
// rest are strings. The billing column is the only one that contains \n —
// rendering callers must apply a WrapText style to it.
// getCacheTokensFromOther extracts a single integer field from Log.Other JSON.
func getCacheTokensFromOther(log *model.Log, field string) int {
	if log == nil || strings.TrimSpace(log.Other) == "" {
		return 0
	}
	var m map[string]any
	if err := common.UnmarshalJsonStr(log.Other, &m); err != nil {
		return 0
	}
	v, ok := m[field]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return int(n)
	default:
		return 0
	}
}

// getCacheCreationTokensFromOther sums all cache creation token variants
// (legacy + 5m + 1h) to give a single "total cache creation tokens" number.
func getCacheCreationTokensFromOther(log *model.Log) int {
	if log == nil || strings.TrimSpace(log.Other) == "" {
		return 0
	}
	var m map[string]any
	if err := common.UnmarshalJsonStr(log.Other, &m); err != nil {
		return 0
	}
	total := 0
	for _, key := range []string{"cache_creation_tokens", "cache_creation_tokens_5m", "cache_creation_tokens_1h"} {
		if v, ok := m[key]; ok {
			if n, ok2 := v.(float64); ok2 {
				total += int(n)
			}
		}
	}
	return total
}

func cellValue(col logExportColumn, log *model.Log) any {
	switch col.key {
	case "time":
		return time.Unix(log.CreatedAt, 0).Format("2006-01-02 15:04:05")
	case "username":
		return log.Username
	case "token":
		return log.TokenName
	case "group":
		return log.Group
	case "type":
		return logTypeLabel(log.Type)
	case "model":
		return log.ModelName
	case "use_time":
		return log.UseTime
	case "prompt":
		return log.PromptTokens
	case "completion":
		return log.CompletionTokens
	case "cache_read":
		return getCacheTokensFromOther(log, "cache_tokens")
	case "cache_creation":
		return getCacheCreationTokensFromOther(log)
	case "cost":
		return "$" + formatPrice(float64(log.Quota)/common.QuotaPerUnit)
	case "billing":
		return buildBillingText(log)
	case "request_id":
		return log.RequestId
	default:
		return ""
	}
}

// writeBillExcel streams the rows into an xlsx and writes the result to the
// HTTP response. truncated == true triggers an X-Export-Truncated header so
// the frontend can warn the user that filters need to be narrowed.
func writeBillExcel(c *gin.Context, logs []*model.Log, columns []logExportColumn, truncated bool) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	sheet := "账单"
	idx, err := f.NewSheet(sheet)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	f.SetActiveSheet(idx)
	_ = f.DeleteSheet("Sheet1")

	headerStyle, err := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true},
		Alignment: &excelize.Alignment{Vertical: "center", Horizontal: "left", WrapText: true},
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	wrapStyle, err := f.NewStyle(&excelize.Style{
		Alignment: &excelize.Alignment{Vertical: "top", Horizontal: "left", WrapText: true},
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}

	sw, err := f.NewStreamWriter(sheet)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	// Column widths and header row.
	billingColIdx := -1
	headerRow := make([]any, len(columns))
	for i, col := range columns {
		if err = sw.SetColWidth(i+1, i+1, col.width); err != nil {
			common.ApiError(c, err)
			return
		}
		headerRow[i] = excelize.Cell{Value: col.header, StyleID: headerStyle}
		if col.key == "billing" {
			billingColIdx = i
		}
	}
	if err = sw.SetRow("A1", headerRow); err != nil {
		common.ApiError(c, err)
		return
	}

	for rowIdx, log := range logs {
		row := make([]any, len(columns))
		for i, col := range columns {
			val := cellValue(col, log)
			if i == billingColIdx {
				row[i] = excelize.Cell{Value: val, StyleID: wrapStyle}
			} else {
				row[i] = val
			}
		}
		cell, _ := excelize.CoordinatesToCellName(1, rowIdx+2)
		if err = sw.SetRow(cell, row); err != nil {
			common.ApiError(c, err)
			return
		}
	}

	if err = sw.Flush(); err != nil {
		common.ApiError(c, err)
		return
	}

	if truncated {
		c.Writer.Header().Set("X-Export-Truncated", "1")
		c.Writer.Header().Set("X-Export-Max-Rows", strconv.Itoa(model.LogExportMaxRows()))
	}
	filename := "bill-" + time.Now().Format("20060102-150405") + ".xlsx"
	c.Writer.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	c.Writer.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.WriteHeader(http.StatusOK)
	if err = f.Write(c.Writer); err != nil {
		// Headers already sent — best effort log; we can't switch to JSON now.
		common.SysError("failed to write export xlsx: " + err.Error())
	}
}

// ExportAllLogs handles the admin bill export. Filter query params match
// /api/log/ exactly so the frontend can reuse its existing form-values
// serializer without translation.
func ExportAllLogs(c *gin.Context) {
	logType, _ := strconv.Atoi(c.Query("type"))
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	username := c.Query("username")
	tokenName := c.Query("token_name")
	modelName := c.Query("model_name")
	channel, _ := strconv.Atoi(c.Query("channel"))
	group := c.Query("group")
	requestId := c.Query("request_id")

	logs, truncated, err := model.GetAllLogsForExport(logType, startTimestamp, endTimestamp, modelName, username, tokenName, channel, group, requestId)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	columns := resolveExportColumns(c.Query("columns"))
	writeBillExcel(c, logs, columns, truncated)
}

// ExportUserLogs handles the self-service bill export. It is gated by the
// admin-controlled LogExportEnabled toggle even when the user is authenticated.
func ExportUserLogs(c *gin.Context) {
	if !common.LogExportEnabled {
		c.JSON(http.StatusForbidden, gin.H{"success": false, "message": "导出功能已关闭"})
		return
	}
	userId := c.GetInt("id")
	logType, _ := strconv.Atoi(c.Query("type"))
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	tokenName := c.Query("token_name")
	modelName := c.Query("model_name")
	group := c.Query("group")
	requestId := c.Query("request_id")

	logs, truncated, err := model.GetUserLogsForExport(userId, logType, startTimestamp, endTimestamp, modelName, tokenName, group, requestId)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	columns := resolveExportColumns(c.Query("columns"))
	writeBillExcel(c, logs, columns, truncated)
}
