package controller

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
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

// TestClampCostRange 覆盖 spec §5.2(4) 的时间跨度护栏：370 天上限，
// start/end 缺省（<=0）时的兜底行为。
func TestClampCostRange(t *testing.T) {
	const day = int64(24 * 3600)
	now := int64(2000000000) // 固定基准时间，避免测试依赖真实 time.Now()

	cases := []struct {
		name       string
		start, end int64
		wantStart  int64
		wantEnd    int64
	}{
		{
			name:  "zero end falls back to now",
			start: now - 10*day, end: 0,
			wantStart: now - 10*day, wantEnd: now,
		},
		{
			name:  "zero start clamps to end-370d",
			start: 0, end: now,
			wantStart: now - costCubeMaxRangeSeconds, wantEnd: now,
		},
		{
			name:  "negative start clamps to end-370d",
			start: -1, end: now,
			wantStart: now - costCubeMaxRangeSeconds, wantEnd: now,
		},
		{
			name:  "span over 370d clamps start",
			start: now - 400*day, end: now,
			wantStart: now - costCubeMaxRangeSeconds, wantEnd: now,
		},
		{
			name:  "normal span within 370d is untouched",
			start: now - 30*day, end: now,
			wantStart: now - 30*day, wantEnd: now,
		},
		{
			name:  "both zero falls back to now and 370d window",
			start: 0, end: 0,
			wantStart: now - costCubeMaxRangeSeconds, wantEnd: now,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotStart, gotEnd := clampCostRange(tc.start, tc.end, now)
			if gotStart != tc.wantStart || gotEnd != tc.wantEnd {
				t.Fatalf("clampCostRange(%d, %d, %d) = (%d, %d), want (%d, %d)",
					tc.start, tc.end, now, gotStart, gotEnd, tc.wantStart, tc.wantEnd)
			}
		})
	}
}

// TestCostCubeCacheKey_ChannelDiffers 验证 channel 过滤参数参与缓存键生成：
// 不同 channel 值必须落在不同的缓存条目上，否则切换渠道筛选会误命中旧缓存。
func TestCostCubeCacheKey_ChannelDiffers(t *testing.T) {
	base := costCubeCacheKey(100, 200, "gpt-4o", "alice", 7.0, 0)
	withCh3 := costCubeCacheKey(100, 200, "gpt-4o", "alice", 7.0, 3)
	withCh9 := costCubeCacheKey(100, 200, "gpt-4o", "alice", 7.0, 9)
	if base == withCh3 {
		t.Fatal("expected different cache keys for channel=0 vs channel=3")
	}
	if withCh3 == withCh9 {
		t.Fatal("expected different cache keys for channel=3 vs channel=9")
	}
	if base == withCh9 {
		t.Fatal("expected different cache keys for channel=0 vs channel=9")
	}
	// 其余参数不变时同一 channel 值必须产生同一个键（缓存才能命中）。
	if costCubeCacheKey(100, 200, "gpt-4o", "alice", 7.0, 3) != withCh3 {
		t.Fatal("expected identical cache key for identical params")
	}
}

// TestCostCubeCacheGetPut 验证缓存命中/未命中/过期逻辑。
// 通过手工构造 entry.at（而非 time.Sleep）注入"过去"的时间戳来模拟过期，
// 保持测试快速且确定。
func TestCostCubeCacheGetPut(t *testing.T) {
	key := "test-key-" + t.Name()
	defer costCubeCache.Delete(key)

	if _, ok := costCubeCacheGet(key); ok {
		t.Fatal("expected miss before any put")
	}

	fresh := &costCubeCacheEntry{cube: newCostCube(), channels: map[int]*model.ChannelCostInfo{}, rate: 7.0, at: time.Now()}
	costCubeCachePut(key, fresh)
	got, ok := costCubeCacheGet(key)
	if !ok {
		t.Fatal("expected hit right after put")
	}
	if got.rate != 7.0 {
		t.Fatalf("rate = %v, want 7.0", got.rate)
	}

	// 模拟过期：直接把时间戳写到 61 秒前（超过 60 秒 TTL）
	expired := &costCubeCacheEntry{cube: newCostCube(), channels: map[int]*model.ChannelCostInfo{}, rate: 7.0, at: time.Now().Add(-61 * time.Second)}
	costCubeCache.Store(key, expired)
	if _, ok := costCubeCacheGet(key); ok {
		t.Fatal("expected miss for expired entry")
	}
	// costCubeCacheGet 命中过期项后应主动清理
	if _, ok := costCubeCache.Load(key); ok {
		t.Fatal("expired entry should have been evicted from the map")
	}
}

// TestCostCubeCachePutEvictsOldestWhenFull 验证条目数超过上限时会淘汰最旧的一条，
// 防止无界增长。
func TestCostCubeCachePutEvictsOldestWhenFull(t *testing.T) {
	prefix := "evict-test-" + t.Name() + "-"
	defer func() {
		for i := 0; i < costCubeCacheMaxEntries+2; i++ {
			costCubeCache.Delete(prefix + string(rune('a'+i)))
		}
	}()

	base := time.Now()
	var oldestKey string
	for i := 0; i < costCubeCacheMaxEntries; i++ {
		k := prefix + string(rune('a'+i))
		if i == 0 {
			oldestKey = k
		}
		entry := &costCubeCacheEntry{
			cube: newCostCube(), channels: map[int]*model.ChannelCostInfo{}, rate: 1.0,
			// 递增时间戳，确保第一个是最旧的
			at: base.Add(time.Duration(i) * time.Millisecond),
		}
		costCubeCachePut(k, entry)
	}

	// 再插入一条，触发对已满缓存的淘汰
	newKey := prefix + string(rune('a'+costCubeCacheMaxEntries))
	costCubeCachePut(newKey, &costCubeCacheEntry{
		cube: newCostCube(), channels: map[int]*model.ChannelCostInfo{}, rate: 1.0,
		at: base.Add(time.Duration(costCubeCacheMaxEntries) * time.Millisecond),
	})

	if _, ok := costCubeCache.Load(oldestKey); ok {
		t.Fatal("oldest entry should have been evicted once cache was full")
	}
	if _, ok := costCubeCache.Load(newKey); !ok {
		t.Fatal("newly inserted entry should be present")
	}
}
