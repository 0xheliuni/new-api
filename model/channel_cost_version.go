package model

import (
	"sort"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

// ChannelCostVersion 渠道成本计价历史版本。
// EffectiveFrom 秒级 Unix 时间戳（闭区间起点）；0 = 初始版本（自古以来）。
// 版本区间 = [EffectiveFrom, 下一版本.EffectiveFrom)。
// 版本行不可变：改价追加新行，误填 DELETE 后重加。
type ChannelCostVersion struct {
	Id            int   `json:"id"             gorm:"primaryKey"`
	ChannelId     int   `json:"channel_id"     gorm:"index:idx_channel_effective,priority:1;not null"`
	EffectiveFrom int64 `json:"effective_from" gorm:"index:idx_channel_effective,priority:2;not null"`
	// CostMode: "ratio"（CNY:USD 倍率）| "discount"（刊例折扣）。空值等同 ratio。
	CostMode     string  `json:"cost_mode"     gorm:"type:varchar(16)"`
	CostRatio    float64 `json:"cost_ratio"`
	CostDiscount float64 `json:"cost_discount"`
	// ExchangeRate discount 模式冻结结算汇率，不随查询汇率浮动。
	ExchangeRate float64 `json:"exchange_rate"`
	Note         string  `json:"note"       gorm:"type:varchar(255)"`
	CreatedAt    int64   `json:"created_at"`
	CreatedBy    int     `json:"created_by"`
}

// VersionMap channelId → 按 EffectiveFrom 升序排列的版本切片。
type VersionMap map[int][]ChannelCostVersion

// VersionAt 返回 channelId 在 ts 时刻生效的版本本体，供调用方识别"跨了哪几个版本"。
// ok=false 时第一个返回值无意义。
func (v VersionMap) VersionAt(channelId int, ts int64) (ChannelCostVersion, bool) {
	versions := v[channelId]
	if len(versions) == 0 {
		return ChannelCostVersion{}, false
	}
	// versions 升序；找最后一个 EffectiveFrom <= ts 的版本。
	idx := sort.Search(len(versions), func(i int) bool {
		return versions[i].EffectiveFrom > ts
	}) - 1
	if idx < 0 {
		return ChannelCostVersion{}, false
	}
	return versions[idx], true
}

// EffectiveRatio 返回该版本换算后的 CNY:USD 倍率（discount 模式乘冻结汇率）。
// 值为 0 或缺省 → 0, false（无法定价，不代表上游免费）。
func (v ChannelCostVersion) EffectiveRatio() (float64, bool) {
	if v.CostMode == "discount" {
		r := v.CostDiscount * v.ExchangeRate
		if r <= 0 {
			return 0, false
		}
		return r, true
	}
	if v.CostRatio <= 0 {
		return 0, false
	}
	return v.CostRatio, true
}

// RatioAt 返回 channelId 在时间戳 ts 时刻的有效 CNY:USD 倍率。
// 与 VersionAt 共用同一套查找逻辑，避免两处漂移。
func (v VersionMap) RatioAt(channelId int, ts int64) (float64, bool) {
	ver, ok := v.VersionAt(channelId, ts)
	if !ok {
		return 0, false
	}
	return ver.EffectiveRatio()
}

// GetAllChannelCostVersions 一次性载入全部版本（主库），升序，供 buildCostCube 缓存。
func GetAllChannelCostVersions() (VersionMap, error) {
	var rows []ChannelCostVersion
	if err := DB.Order("effective_from asc").Find(&rows).Error; err != nil {
		return nil, err
	}
	m := make(VersionMap, len(rows))
	for _, r := range rows {
		m[r.ChannelId] = append(m[r.ChannelId], r)
	}
	return m, nil
}

// GetChannelCostVersions 查单渠道版本，降序（最新在前），供 UI 展示。
func GetChannelCostVersions(channelId int) ([]ChannelCostVersion, error) {
	var rows []ChannelCostVersion
	err := DB.Where("channel_id = ?", channelId).
		Order("effective_from desc").Find(&rows).Error
	return rows, err
}

// CreateChannelCostVersion 追加新版本（CreatedAt 由函数填充）。
func CreateChannelCostVersion(v *ChannelCostVersion) error {
	v.CreatedAt = common.GetTimestamp()
	return DB.Create(v).Error
}

// DeleteChannelCostVersion 删除指定版本（幂等）。
func DeleteChannelCostVersion(id int) error {
	return DB.Where("id = ?", id).Delete(&ChannelCostVersion{}).Error
}

// VersionExists 检查同渠道同 effective_from 是否已有版本，用于 409 冲突检测。
func VersionExists(channelId int, effectiveFrom int64) (bool, error) {
	var count int64
	err := DB.Model(&ChannelCostVersion{}).
		Where("channel_id = ? AND effective_from = ?", channelId, effectiveFrom).
		Count(&count).Error
	return count > 0, err
}

// seedChannelCostVersions 迁移回填：对已配置成本计价但版本表无记录的渠道，
// 插入 effective_from=0 的初始版本。幂等，重复调用跳过已有渠道。
func seedChannelCostVersions() error {
	type cid struct{ ChannelId int }
	var existing []cid
	if err := DB.Model(&ChannelCostVersion{}).Select("channel_id").
		Group("channel_id").Find(&existing).Error; err != nil {
		return err
	}
	seeded := make(map[int]bool, len(existing))
	for _, r := range existing {
		seeded[r.ChannelId] = true
	}
	var channels []Channel
	if err := DB.Select("id", "setting").Find(&channels).Error; err != nil {
		return err
	}
	er := operation_setting.USDExchangeRate
	if er <= 0 {
		er = 7.3
	}
	for _, ch := range channels {
		if seeded[ch.Id] || ch.Setting == nil || *ch.Setting == "" {
			continue
		}
		var s dto.ChannelSettings
		if err := common.UnmarshalJsonStr(*ch.Setting, &s); err != nil {
			continue
		}
		hasCost := (s.CostMode == "discount" && s.CostDiscount > 0) ||
			(s.CostMode != "discount" && s.CostRatio > 0)
		if !hasCost {
			continue
		}
		v := &ChannelCostVersion{
			ChannelId: ch.Id, EffectiveFrom: 0,
			CostMode: s.CostMode, CostRatio: s.CostRatio,
			CostDiscount: s.CostDiscount, ExchangeRate: er,
			Note: "migrated from channel settings",
		}
		if err := CreateChannelCostVersion(v); err != nil {
			return err
		}
	}
	return nil
}
