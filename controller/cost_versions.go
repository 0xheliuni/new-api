package controller

import (
	"errors"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// GetChannelCostVersions GET /api/cost/channels/:id/versions
// 返回指定渠道的全部计价版本，按 effective_from 降序（最新版本在前），供管理员查看历史定价轨迹。
func GetChannelCostVersions(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiErrorMsg(c, "invalid channel id")
		return
	}
	versions, err := model.GetChannelCostVersions(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, versions)
}

type createVersionRequest struct {
	// EffectiveFrom 刻意不加 binding:"required"：validator 对非指针 int64 的 required
	// 会把显式传入的 0 一并拦在 ShouldBindJSON 里，令下方「0 保留给迁移版本」这条
	// 可操作的提示永远不可达，客户端只能看到一句泛化的绑定错误。改为显式检查。
	EffectiveFrom int64   `json:"effective_from"`
	CostMode      string  `json:"cost_mode"`
	CostRatio     float64 `json:"cost_ratio"`
	CostDiscount  float64 `json:"cost_discount"`
	ExchangeRate  float64 `json:"exchange_rate"`
	Note          string  `json:"note"`
}

// CreateChannelCostVersion POST /api/cost/channels/:id/versions
// 追加新计价版本；版本行一经写入不可变，改价只追加不更新。
// discount 模式下 ExchangeRate 由服务端冻结为当前汇率（operation_setting.USDExchangeRate），
// 忽略客户端传值——确保历史成本不因日后汇率变动而漂移（设计不变量 2）。
func CreateChannelCostVersion(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiErrorMsg(c, "invalid channel id")
		return
	}
	var req createVersionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	// effective_from=0 保留给迁移回填的"自古以来"初始版本，API 追加禁止复用该值。
	if req.EffectiveFrom == 0 {
		common.ApiErrorMsg(c, "effective_from cannot be 0 (reserved for migration)")
		return
	}
	// 校验 cost_mode：只接受空（等同 ratio）、ratio、discount；
	// 未知值写入后 EffectiveRatio 会返回 ok=false，让日志永久无法定价。
	mode := req.CostMode
	if mode != "" && mode != "ratio" && mode != "discount" {
		common.ApiErrorMsg(c, `unknown cost_mode: must be "ratio" or "discount"`)
		return
	}
	// ratio（含空值）模式：CostRatio<=0 时 EffectiveRatio 同样返回 false，版本无意义。
	if mode != "discount" && req.CostRatio <= 0 {
		common.ApiErrorMsg(c, "cost_ratio must be > 0 for ratio mode")
		return
	}
	// discount 模式：CostDiscount<=0 时乘以汇率仍为 0，版本同样无法定价。
	if mode == "discount" && req.CostDiscount <= 0 {
		common.ApiErrorMsg(c, "cost_discount must be > 0 for discount mode")
		return
	}
	// 渠道必须存在：孤儿版本行虽然当下无日志引用、不会误算成本，但会被
	// GetAllChannelCostVersions 悄悄载入 VersionMap；早点报错比静默成功易排查。
	if _, err := model.GetChannelById(id, false); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			common.ApiErrorMsg(c, "channel not found")
			return
		}
		common.ApiError(c, err)
		return
	}
	// 同渠道同 effective_from 已存在则拒绝：版本不可变，重复写入会造成时间点歧义。
	// 该检查与下方写入非原子，但这是仅超管可达的低频路径，冲突窗口可接受。
	exists, err := model.VersionExists(id, req.EffectiveFrom)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if exists {
		common.ApiErrorMsg(c, "a version with this effective_from already exists for this channel")
		return
	}
	// discount 模式冻结服务端当前汇率，与迁移回填（seedLoadExchangeRate）同一来源。
	// InitOptionMap 在启动时已把 options 表的值同步进 operation_setting.USDExchangeRate，
	// 运行期直接读包级变量即可，无需再查库。
	//
	// 汇率非正时拒绝写入，而不是像 seed 那样兜底成 7.3：seed 跑在 InitOptionMap 之前，
	// 读到默认值是时序使然，只能兜底；而运行期读到 0 意味着汇率确实没配好，此时把 0
	// 冻进不可变的版本行会让该版本下所有日志永久无法定价（成本记 0，显示成 100% 毛利），
	// 且因为版本不可更新，只能靠再追加一版来绕过。让管理员改完配置重试，代价小得多。
	exRate := req.ExchangeRate
	if mode == "discount" {
		if operation_setting.USDExchangeRate <= 0 {
			common.ApiErrorMsg(c, "discount mode requires a positive USDExchangeRate; configure it in system settings first")
			return
		}
		exRate = operation_setting.USDExchangeRate
	}
	// CreatedBy 取中间件已验证的登录用户 id，不信任客户端传值。
	userId := c.GetInt("id")
	v := &model.ChannelCostVersion{
		ChannelId:     id,
		EffectiveFrom: req.EffectiveFrom,
		CostMode:      req.CostMode,
		CostRatio:     req.CostRatio,
		CostDiscount:  req.CostDiscount,
		ExchangeRate:  exRate,
		Note:          req.Note,
		CreatedBy:     userId,
	}
	if err := model.CreateChannelCostVersion(v); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, v)
}

// DeleteChannelCostVersion DELETE /api/cost/versions/:vid
// 删除指定版本（幂等：版本已不存在时返回成功）。
//
// 设计不变量 4 处理策略：拒绝删除渠道的最后一个版本。
// 理由：版本行不可变且无更新接口，删除最后一行后该渠道所有历史日志将永久标记为
// 未定价（EffectiveRatio 返回 ok=false），且无法通过"再插一行"来覆盖历史——
// effective_from=0 的种子行若丢失，1970 年之后、下一版本之前的全部日志都将失去
// 成本基准，损失不可逆。保留最后一条是最低安全护栏。
// 管理员如需彻底移除某渠道定价，应先追加一个新版本再删旧版本，确保始终有版本可查。
func DeleteChannelCostVersion(c *gin.Context) {
	vid, err := strconv.Atoi(c.Param("vid"))
	if err != nil || vid <= 0 {
		common.ApiErrorMsg(c, "invalid version id")
		return
	}
	// 先读出版本以获取 channel_id，供最后版本保护检查定位渠道。
	var v model.ChannelCostVersion
	if err := model.DB.Where("id = ?", vid).First(&v).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// 版本已不存在，幂等返回成功。
			common.ApiSuccess(c, nil)
			return
		}
		common.ApiError(c, err)
		return
	}
	// 计数与删除必须在同一事务内：分成两步时两个并发 DELETE 会同时读到 count=2、
	// 同时通过校验，把渠道删空——正是这道校验要挡的状态。
	if err := model.DeleteChannelCostVersionIfNotLast(v.ChannelId, vid); err != nil {
		if errors.Is(err, model.ErrLastVersion) {
			common.ApiErrorMsg(c, "cannot delete the last version of a channel; add a replacement version first")
			return
		}
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}
