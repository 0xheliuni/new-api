package controller

import (
	"sort"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

type costTrendPoint struct {
	Date       string  `json:"date"`
	RevenueCny float64 `json:"revenue_cny"`
	CostCny    float64 `json:"cost_cny"`
	ProfitCny  float64 `json:"profit_cny"`
}

type costStackPoint struct {
	Date        string  `json:"date"`
	ChannelId   int     `json:"channel_id"`
	ChannelName string  `json:"channel_name"`
	CostCny     float64 `json:"cost_cny"`
}

type costOverviewDTO struct {
	Totals               costMoney        `json:"totals"`
	UnpricedChannelCount int              `json:"unpriced_channel_count"`
	ExchangeRate         float64          `json:"exchange_rate"`
	Trend                []costTrendPoint `json:"trend"`
	CostStack            []costStackPoint `json:"cost_stack"`
}

type costPageDTO struct {
	Items    []costDimensionRow `json:"items"`
	Total    int                `json:"total"`
	Page     int                `json:"page"`
	PageSize int                `json:"page_size"`
	Summary  costMoney          `json:"summary"`
}

// buildCostCube 流式扫描日志构建立方体，同时载入渠道倍率映射与汇率。
func buildCostCube(c *gin.Context) (*costCube, map[int]*model.ChannelCostInfo, float64, error) {
	start, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	end, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	cube := newCostCube()
	maxRows := model.LogExportMaxRows("xlsx")
	_, err := model.GetAllLogsForExport(model.LogTypeUnknown, start, end,
		c.Query("model_name"), c.Query("username"), "", 0, "", "", maxRows,
		func(batch []*model.Log) error {
			cube.addBatch(batch)
			return nil
		})
	if err != nil {
		return nil, nil, 0, err
	}
	channels, err := model.GetAllChannelCostInfos()
	if err != nil {
		return nil, nil, 0, err
	}
	return cube, channels, billSummaryRate(c), nil
}

func buildCostOverview(cube *costCube, channels map[int]*model.ChannelCostInfo, rate float64) costOverviewDTO {
	ov := costOverviewDTO{ExchangeRate: rate}
	trend := make(map[string]*costTrendPoint)
	stack := make(map[string]*costStackPoint) // key: day|channelId
	unpriced := make(map[int]bool)
	for k, r := range cube.rows {
		ratio, chName := channelRatio(channels, k.ChannelId)
		m := costMoneyFromParts(r.Quota, r.ListQuota, r.RefundQuota, r.PromptTokens, r.CompletionTokens, r.RequestCount, ratio, rate)
		ov.Totals.add(m)
		if ratio <= 0 {
			unpriced[k.ChannelId] = true
		}
		tp := trend[k.Day]
		if tp == nil {
			tp = &costTrendPoint{Date: k.Day}
			trend[k.Day] = tp
		}
		tp.RevenueCny = roundTo6(tp.RevenueCny + m.RevenueCny)
		tp.CostCny = roundTo6(tp.CostCny + m.CostCny)
		tp.ProfitCny = roundTo6(tp.ProfitCny + m.ProfitCny)

		sk := k.Day + "|" + strconv.Itoa(k.ChannelId)
		sp := stack[sk]
		if sp == nil {
			sp = &costStackPoint{Date: k.Day, ChannelId: k.ChannelId, ChannelName: chName}
			stack[sk] = sp
		}
		sp.CostCny = roundTo6(sp.CostCny + m.CostCny)
	}
	ov.UnpricedChannelCount = len(unpriced)
	for _, tp := range trend {
		ov.Trend = append(ov.Trend, *tp)
	}
	sort.Slice(ov.Trend, func(i, j int) bool { return ov.Trend[i].Date < ov.Trend[j].Date })
	for _, sp := range stack {
		ov.CostStack = append(ov.CostStack, *sp)
	}
	sort.Slice(ov.CostStack, func(i, j int) bool {
		if ov.CostStack[i].Date != ov.CostStack[j].Date {
			return ov.CostStack[i].Date < ov.CostStack[j].Date
		}
		return ov.CostStack[i].ChannelId < ov.CostStack[j].ChannelId
	})
	return ov
}

func paginateCostRows(rows []costDimensionRow, page, pageSize int) costPageDTO {
	var summary costMoney
	for i := range rows {
		summary.add(rows[i].costMoney)
	}
	start := (page - 1) * pageSize
	if start < 0 {
		start = 0
	}
	end := start + pageSize
	items := []costDimensionRow{}
	if start < len(rows) {
		if end > len(rows) {
			end = len(rows)
		}
		items = rows[start:end]
	}
	return costPageDTO{Items: items, Total: len(rows), Page: page, PageSize: pageSize, Summary: summary}
}

// GetCostOverview GET /api/cost/overview — Root only.
func GetCostOverview(c *gin.Context) {
	cube, channels, rate, err := buildCostCube(c)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, buildCostOverview(cube, channels, rate))
}

func getCostByDimension(c *gin.Context, dim string) {
	cube, channels, rate, err := buildCostCube(c)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	page, pageSize := parseBillSummaryPaging(c)
	rows := foldCostCube(cube, dim, channels, rate)
	common.ApiSuccess(c, paginateCostRows(rows, page, pageSize))
}

// GetCostByUsers GET /api/cost/users — Root only.
func GetCostByUsers(c *gin.Context) { getCostByDimension(c, costDimUser) }

// GetCostByModels GET /api/cost/models — Root only.
func GetCostByModels(c *gin.Context) { getCostByDimension(c, costDimModel) }

// GetCostByChannels GET /api/cost/channels — Root only.
func GetCostByChannels(c *gin.Context) { getCostByDimension(c, costDimChannel) }
