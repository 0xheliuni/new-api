package model

import (
	"errors"
	"sort"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"gorm.io/gorm"
)

// ChannelCostVersion 渠道成本计价历史版本。
// EffectiveFrom 秒级 Unix 时间戳（闭区间起点）；0 = 初始版本（自古以来）。
// 版本区间 = [EffectiveFrom, 下一版本.EffectiveFrom)。
// 改价追加新行；填错的价可以修改（UpdateChannelCostVersion），改动留在审计日志里。
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
// 早于最早一条版本的时刻按最早那条计：初始价被删掉后由后面的记录往前覆盖，不留下
// 查不到价的空档。ok=false 仅表示该渠道没有任何版本，此时第一个返回值无意义。
func (v VersionMap) VersionAt(channelId int, ts int64) (ChannelCostVersion, bool) {
	versions := v[channelId]
	if len(versions) == 0 {
		return ChannelCostVersion{}, false
	}
	// versions 升序；找最后一个 EffectiveFrom <= ts 的版本。
	idx := sort.Search(len(versions), func(i int) bool {
		return versions[i].EffectiveFrom > ts
	}) - 1
	return versions[max(idx, 0)], true
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

// DeleteChannelCostVersionIfNotLast 删除指定版本，但不允许删掉该渠道仅剩的一条
// （ErrLastVersion）：删光后渠道没有任何价格可用，全部日志成本记 0。
//
// 初始版本（effective_from=0）可以删：早于最早一条记录的日志按最早那条计价（见
// VersionAt），删掉它就由后面的记录往前覆盖，不会留下成本记 0 的空档；更早的价
// 也能用「补录历史价格」重新录入。
//
// 关于并发：事务只给原子性，不给互斥——普通 COUNT(*) 在三库上都是不加锁的读，两个
// 并发 DELETE 会同时读到 count=2、同时通过计数校验，把渠道删空。所以这里对
// channel_id 的全部版本行加行锁，把「数」和「删」真正串起来：第二个事务阻塞到第一个
// 提交后重读，看到 count=1 并正确拒绝。
//
// SQLite 不支持 FOR UPDATE，lockForUpdate 在那里不加锁：它的写事务本身互斥，第二个
// 删除要么排在第一个之后（重读到 count=1，同样被拒），要么直接拿到 SQLITE_BUSY——
// 失败得很响，不会静默删空。
//
// 加锁的这次 SELECT 同时充当目标行查找与计数。
func DeleteChannelCostVersionIfNotLast(channelId, versionId int) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		q := lockForUpdate(tx.Model(&ChannelCostVersion{}).Where("channel_id = ?", channelId))
		var rows []ChannelCostVersion
		if err := q.Select("id", "effective_from").Find(&rows).Error; err != nil {
			return err
		}
		// 在事务内重新定位目标行：调用方读取与本次删除之间该行可能已被删掉，
		// 那种情况下期望的终态已经达成，按幂等处理返回成功。
		idx := -1
		for i := range rows {
			if rows[i].Id == versionId {
				idx = i
				break
			}
		}
		if idx < 0 {
			return nil
		}
		if len(rows) <= 1 {
			return ErrLastVersion
		}
		return tx.Where("id = ?", versionId).Delete(&ChannelCostVersion{}).Error
	})
}

// ErrVersionNotFound 要修改的版本不存在（可能已被删除）。
var ErrVersionNotFound = errors.New("price version not found")

// ErrInitialVersionTime 初始版本与带日期的版本不能互相转换：初始版本的生效时间恒为
// 0，带日期的版本也不能改成 0（重新设置初始价走新增）。
var ErrInitialVersionTime = errors.New("initial and dated price versions cannot be converted into each other")

// ErrVersionTimeTaken 同一渠道已有相同生效时间的版本。
var ErrVersionTimeTaken = errors.New("a price version with this effective time already exists")

// UpdateChannelCostVersion 按 v.Id 修改一条版本的生效时间与计价字段，返回修改前的
// 版本；成功后 v 补齐 ChannelId、CreatedAt、CreatedBy。
//
// 版本原本不可修改，填错的价只能删了重建，初始价删掉后还补不回来。现在允许修改，
// 改动记录交给审计日志。
//
// 与删除相同，事务内锁住该渠道的全部版本行，把「检查时间是否已被占用」和「写入」
// 串起来，避免两个并发修改把同一渠道的两条版本改到同一时刻。
func UpdateChannelCostVersion(v *ChannelCostVersion) (ChannelCostVersion, error) {
	var before ChannelCostVersion
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("id = ?", v.Id).Limit(1).Find(&before).Error; err != nil {
			return err
		}
		if before.Id == 0 {
			return ErrVersionNotFound
		}
		var rows []ChannelCostVersion
		if err := lockForUpdate(tx.Model(&ChannelCostVersion{}).Where("channel_id = ?", before.ChannelId)).
			Select("id", "effective_from").Find(&rows).Error; err != nil {
			return err
		}
		// 加锁前读到的行可能刚被删掉，或刚被别的修改换了时间，以锁内读到的为准。
		found := false
		for _, row := range rows {
			if row.Id == v.Id {
				found = true
				before.EffectiveFrom = row.EffectiveFrom
			}
		}
		if !found {
			return ErrVersionNotFound
		}
		if (before.EffectiveFrom == 0) != (v.EffectiveFrom == 0) {
			return ErrInitialVersionTime
		}
		for _, row := range rows {
			if row.Id != v.Id && row.EffectiveFrom == v.EffectiveFrom {
				return ErrVersionTimeTaken
			}
		}
		// Select 指定列：结构体 Updates 默认跳过零值，改成折扣模式后 cost_ratio=0、
		// 清空备注这类修改会被悄悄丢掉。
		return tx.Model(&ChannelCostVersion{Id: v.Id}).
			Select("effective_from", "cost_mode", "cost_ratio", "cost_discount", "exchange_rate", "note").
			Updates(v).Error
	})
	if err != nil {
		return ChannelCostVersion{}, err
	}
	v.ChannelId = before.ChannelId
	v.CreatedAt = before.CreatedAt
	v.CreatedBy = before.CreatedBy
	return before, nil
}

// SyncChannelCostSetting 把渠道在 now 时刻生效的版本写回 channels.setting 的
// cost_mode/cost_ratio/cost_discount/cost_exchange_rate，其它键原样保留。
//
// 渠道编辑页显示的成本价就是 setting 里的这一份。版本增删改之后不同步的话，编辑页
// 显示的就不是实际生效的价。渠道没有任何版本时不动。
//
// 只写当前模式用到的字段：另一模式的旧值留着，管理员切回去时还能看到上次的数；
// 倍率模式不用结算汇率，删掉它。
func SyncChannelCostSetting(channelId int, now int64) error {
	var versions []ChannelCostVersion
	if err := DB.Where("channel_id = ?", channelId).Order("effective_from asc").Find(&versions).Error; err != nil {
		return err
	}
	inEffect, ok := VersionMap{channelId: versions}.VersionAt(channelId, now)
	if !ok {
		return nil
	}
	var channel Channel
	if err := DB.Select("id", "setting").Where("id = ?", channelId).Limit(1).Find(&channel).Error; err != nil {
		return err
	}
	if channel.Id == 0 {
		return nil
	}
	setting := map[string]any{}
	if channel.Setting != nil && strings.TrimSpace(*channel.Setting) != "" {
		if err := common.UnmarshalJsonStr(*channel.Setting, &setting); err != nil {
			return err
		}
	}
	if inEffect.CostMode == "discount" {
		setting["cost_mode"] = "discount"
		setting["cost_discount"] = inEffect.CostDiscount
		setting["cost_exchange_rate"] = inEffect.ExchangeRate
	} else {
		setting["cost_mode"] = "ratio"
		setting["cost_ratio"] = inEffect.CostRatio
		delete(setting, "cost_exchange_rate")
	}
	data, err := common.Marshal(setting)
	if err != nil {
		return err
	}
	return DB.Model(&Channel{}).Where("id = ?", channelId).Update("setting", string(data)).Error
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
		// 渠道设置里填了结算汇率就用它，与渠道保存时追版本同一口径。
		rate := er
		if s.CostExchangeRate > 0 {
			rate = s.CostExchangeRate
		}
		v := &ChannelCostVersion{
			ChannelId: ch.Id, EffectiveFrom: 0,
			CostMode: s.CostMode, CostRatio: s.CostRatio,
			CostDiscount: s.CostDiscount, ExchangeRate: rate,
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
