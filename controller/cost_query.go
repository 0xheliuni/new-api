package controller

import (
	"fmt"
	"sort"
	"strconv"
	"sync"
	"time"

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

// costCubeMaxRangeSeconds 时间跨度上限：370 天。超出的查询会把 start 钳制到
// end - 370 天，避免一次请求把全部历史日志流式扫描一遍（spec §5.2(4)）。
const costCubeMaxRangeSeconds = int64(370 * 24 * 3600)

// clampCostRange 归一化查询时间范围：
//   - end<=0（未传）时取当前时间；
//   - start<=0（未传）或跨度超过 370 天时，把 start 钳制为 end-370天。
//
// 提取成纯函数便于单元测试，不依赖 gin.Context / time.Now。
func clampCostRange(start, end, now int64) (int64, int64) {
	if end <= 0 {
		end = now
	}
	if start <= 0 || end-start > costCubeMaxRangeSeconds {
		start = end - costCubeMaxRangeSeconds
	}
	return start, end
}

// costCubeCacheEntry 缓存一次 buildCostCube 的完整产出（立方体 + 渠道倍率映射 + 汇率）。
type costCubeCacheEntry struct {
	cube     *costCube
	channels map[int]*model.ChannelCostInfo
	rate     float64
	at       time.Time
}

// costCubeCache 一期护栏：60 秒内相同查询参数直接复用结果，避免同一页面（总览 +
// 三个维度表）多次点击重复流式扫描整段日志。一期仅内存缓存（不接 Redis），
// 使用 sync.Map 做简单的进程内存储；写入时顺带清理过期项并在条目数超过上限时
// 淘汰最旧的一条，避免无界增长。
var costCubeCache sync.Map

const (
	costCubeCacheTTL        = 60 * time.Second
	costCubeCacheMaxEntries = 32
)

// costCubeCacheKey 由归一化后的查询参数拼接生成缓存键。
func costCubeCacheKey(start, end int64, modelName, username string, rate float64) string {
	return fmt.Sprintf("%d|%d|%s|%s|%.6f", start, end, modelName, username, rate)
}

func costCubeCacheGet(key string) (*costCubeCacheEntry, bool) {
	v, ok := costCubeCache.Load(key)
	if !ok {
		return nil, false
	}
	entry := v.(*costCubeCacheEntry)
	if time.Since(entry.at) > costCubeCacheTTL {
		costCubeCache.Delete(key)
		return nil, false
	}
	return entry, true
}

func costCubeCachePut(key string, entry *costCubeCacheEntry) {
	// 清理过期项，同时统计存活条目数，超过上限时淘汰其中最旧的一条
	// （简单策略，非严格 LRU，够用即可）。
	var oldestKey any
	var oldestAt time.Time
	count := 0
	costCubeCache.Range(func(k, v any) bool {
		e, _ := v.(*costCubeCacheEntry)
		if e == nil || time.Since(e.at) > costCubeCacheTTL {
			costCubeCache.Delete(k)
			return true
		}
		count++
		if oldestKey == nil || e.at.Before(oldestAt) {
			oldestKey = k
			oldestAt = e.at
		}
		return true
	})
	if count >= costCubeCacheMaxEntries && oldestKey != nil {
		costCubeCache.Delete(oldestKey)
	}
	costCubeCache.Store(key, entry)
}

// buildCostCube 流式扫描日志构建立方体，同时载入渠道倍率映射与汇率。
// 命中 60 秒内相同参数的缓存时直接复用，否则重新扫描并写入缓存。
func buildCostCube(c *gin.Context) (*costCube, map[int]*model.ChannelCostInfo, float64, error) {
	start, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	end, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	start, end = clampCostRange(start, end, time.Now().Unix())

	modelName := c.Query("model_name")
	username := c.Query("username")
	rate := billSummaryRate(c)

	cacheKey := costCubeCacheKey(start, end, modelName, username, rate)
	if entry, ok := costCubeCacheGet(cacheKey); ok {
		return entry.cube, entry.channels, entry.rate, nil
	}

	cube := newCostCube()
	maxRows := model.LogExportMaxRows("xlsx")
	_, err := model.GetAllLogsForExport(model.LogTypeUnknown, start, end,
		modelName, username, "", 0, "", "", maxRows,
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
	costCubeCachePut(cacheKey, &costCubeCacheEntry{cube: cube, channels: channels, rate: rate, at: time.Now()})
	return cube, channels, rate, nil
}

func buildCostOverview(cube *costCube, channels map[int]*model.ChannelCostInfo, rate float64) costOverviewDTO {
	ov := costOverviewDTO{ExchangeRate: rate}
	trend := make(map[string]*costTrendPoint)
	stack := make(map[string]*costStackPoint) // key: day|channelId
	unpriced := make(map[int]bool)
	for k, r := range cube.rows {
		ratio, chName := effectiveChannelRatio(channels, k.ChannelId, rate)
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
