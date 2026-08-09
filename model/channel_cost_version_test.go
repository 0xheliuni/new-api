package model

import (
	"testing"

	"github.com/QuantumNous/new-api/setting/operation_setting"
)

// seedLoadExchangeRate 必须从 options 表读汇率，而不是读包级变量。
//
// 为什么这条测试存在：seedChannelCostVersions 由 migrateDB() 调用，而 migrateDB()
// 跑在 InitOptionMap() **之前**（main.go: InitDB 早于 InitOptionMap）。此时
// operation_setting.USDExchangeRate 还是包级默认 7.3，管理员配置的值只存在于
// options 表里。若这里退回读包级变量，所有 discount 模式渠道的初始版本都会被
// 冻结成错误汇率 —— 而 ExchangeRate 这一列的存在意义正是防止这件事。
func TestSeedLoadExchangeRate_ReadsOptionsTableNotPackageDefault(t *testing.T) {
	DB.Exec("DELETE FROM options")
	t.Cleanup(func() { DB.Exec("DELETE FROM options") })

	// 模拟启动时序：包级变量仍是默认值，管理员配置只在 options 表里。
	oldRate := operation_setting.USDExchangeRate
	operation_setting.USDExchangeRate = 7.3
	t.Cleanup(func() { operation_setting.USDExchangeRate = oldRate })

	if err := DB.Create(&Option{Key: "USDExchangeRate", Value: "6.5"}).Error; err != nil {
		t.Fatalf("seed option: %v", err)
	}

	if got := seedLoadExchangeRate(); got != 6.5 {
		t.Fatalf("rate = %v, want 6.5 (must read options table, not the 7.3 package default)", got)
	}
}

// options 表无该键时回退到包级变量，而不是硬编码常量。
func TestSeedLoadExchangeRate_FallsBackToPackageVarWhenAbsent(t *testing.T) {
	DB.Exec("DELETE FROM options")
	t.Cleanup(func() { DB.Exec("DELETE FROM options") })

	oldRate := operation_setting.USDExchangeRate
	operation_setting.USDExchangeRate = 7.1
	t.Cleanup(func() { operation_setting.USDExchangeRate = oldRate })

	if got := seedLoadExchangeRate(); got != 7.1 {
		t.Fatalf("rate = %v, want 7.1 (package fallback)", got)
	}
}

// 值不可解析或非正时同样回退，不能把 0 冻进版本行（0 倍率 = 成本记 0）。
func TestSeedLoadExchangeRate_IgnoresInvalidValue(t *testing.T) {
	DB.Exec("DELETE FROM options")
	t.Cleanup(func() { DB.Exec("DELETE FROM options") })

	oldRate := operation_setting.USDExchangeRate
	operation_setting.USDExchangeRate = 7.2
	t.Cleanup(func() { operation_setting.USDExchangeRate = oldRate })

	for _, bad := range []string{"", "abc", "0", "-1"} {
		DB.Exec("DELETE FROM options")
		if err := DB.Create(&Option{Key: "USDExchangeRate", Value: bad}).Error; err != nil {
			t.Fatalf("seed option %q: %v", bad, err)
		}
		if got := seedLoadExchangeRate(); got != 7.2 {
			t.Fatalf("value %q: rate = %v, want 7.2 (fallback)", bad, got)
		}
	}
}

// 单个渠道插入失败不能中断整批回填：其余渠道必须照常获得初始版本。
// 用重复主键制造失败——先手工插一条 channel 1 的版本，再让 seed 跑全表。
func TestSeedChannelCostVersions_SkipsSeededChannelsIdempotently(t *testing.T) {
	DB.Exec("DELETE FROM channel_cost_versions")
	DB.Exec("DELETE FROM channels")
	t.Cleanup(func() {
		DB.Exec("DELETE FROM channel_cost_versions")
		DB.Exec("DELETE FROM channels")
	})

	ratioSetting := `{"cost_mode":"ratio","cost_ratio":2.5}`
	discSetting := `{"cost_mode":"discount","cost_discount":0.8}`
	noCostSetting := `{"force_format":true}`
	for _, ch := range []Channel{
		{Id: 1, Name: "a", Setting: &ratioSetting},
		{Id: 2, Name: "b", Setting: &discSetting},
		{Id: 3, Name: "c", Setting: &noCostSetting},
	} {
		if err := DB.Create(&ch).Error; err != nil {
			t.Fatalf("create channel %d: %v", ch.Id, err)
		}
	}

	if err := seedChannelCostVersions(); err != nil {
		t.Fatalf("first seed: %v", err)
	}
	var afterFirst int64
	DB.Model(&ChannelCostVersion{}).Count(&afterFirst)
	if afterFirst != 2 { // 渠道 3 无成本配置，不建版本
		t.Fatalf("after first seed: %d versions, want 2", afterFirst)
	}

	// 幂等：重复调用不新增行
	if err := seedChannelCostVersions(); err != nil {
		t.Fatalf("second seed: %v", err)
	}
	var afterSecond int64
	DB.Model(&ChannelCostVersion{}).Count(&afterSecond)
	if afterSecond != afterFirst {
		t.Fatalf("second seed added rows: %d -> %d, want idempotent", afterFirst, afterSecond)
	}

	// discount 模式渠道必须带上非零冻结汇率，否则 EffectiveRatio 返回 false（成本记 0）
	var discVer ChannelCostVersion
	if err := DB.Where("channel_id = ?", 2).First(&discVer).Error; err != nil {
		t.Fatalf("load discount version: %v", err)
	}
	if discVer.ExchangeRate <= 0 {
		t.Fatalf("discount version exchange rate = %v, want > 0", discVer.ExchangeRate)
	}
	if _, ok := discVer.EffectiveRatio(); !ok {
		t.Fatal("seeded discount version must yield a usable ratio")
	}
}
