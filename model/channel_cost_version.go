package model

import (
	"errors"
	"sort"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"gorm.io/gorm"
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

// ErrLastVersion 目标是该渠道仅存版本时返回，供调用方与数据库错误区分处理
// （返回可读提示而非 500）。
var ErrLastVersion = errors.New("cannot delete the last version of a channel")

// DeleteChannelCostVersionIfNotLast 在同一事务内完成「计数 + 删除」：计数 <=1 时
// 返回 ErrLastVersion 且不删除。
//
// 必须放在事务里：计数与删除若是两次独立请求，两个并发 DELETE 可以同时读到 count=2、
// 同时通过校验、同时删除，最终把渠道清空——正是这道校验要挡的状态。
// 用事务而非 SELECT ... FOR UPDATE：后者 SQLite 不支持，而三库兼容是硬约束。
func DeleteChannelCostVersionIfNotLast(channelId, versionId int) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		var count int64
		if err := tx.Model(&ChannelCostVersion{}).
			Where("channel_id = ?", channelId).
			Count(&count).Error; err != nil {
			return err
		}
		if count <= 1 {
			return ErrLastVersion
		}
		return tx.Where("id = ?", versionId).Delete(&ChannelCostVersion{}).Error
	})
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
// 每个渠道独立处理：单渠道插入失败只记日志并继续，下次启动会重试该渠道。
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
	// 从 options 表直接读汇率快照：seedChannelCostVersions 在 InitOptionMap() 之前
	// 被 migrateDB() 调用，此时 operation_setting.USDExchangeRate 尚未从 DB 加载，
	// 仍是包级默认值 7.3。直接查 options 表才能拿到管理员配置的值。
	er := seedLoadExchangeRate()
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
			// 非致命：记日志并继续，让其余渠道正常回填；
			// 失败渠道在下次启动时会被再次尝试（seeded map 不含它）。
			common.SysError("seedChannelCostVersions: channel " +
				strconv.Itoa(ch.Id) + ": " + err.Error())
		}
	}
	return nil
}

// seedLoadExchangeRate 直接从 options 表查 USDExchangeRate，返回解析后的 float64。
// 查询失败或值不合法时回退到 operation_setting.USDExchangeRate（包级默认）。
// 此函数仅在 migrateDB() 内调用，此时 InitOptionMap() 尚未运行。
// key 是三库保留字，必须用 commonKeyCol 引用（PG 用双引号，MySQL/SQLite 用反引号）。
func seedLoadExchangeRate() float64 {
	// 用 Find 而非 First：First 在无记录时返回 ErrRecordNotFound，GORM 会把它
	// 记成错误日志——但"选项没配过"是完全正常的启动状态，不该制造日志噪声。
	var opts []Option
	err := DB.Where(commonKeyCol+" = ?", "USDExchangeRate").Limit(1).Find(&opts).Error
	if err == nil && len(opts) > 0 {
		if r, parseErr := strconv.ParseFloat(opts[0].Value, 64); parseErr == nil && r > 0 {
			return r
		}
	}
	// 回退到包级变量（默认 7.3，或已被其他路径提前设置的值）
	if operation_setting.USDExchangeRate > 0 {
		return operation_setting.USDExchangeRate
	}
	return 7.3
}
