package controller

import (
	"errors"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
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
// discount 模式下 ExchangeRate 随版本一并冻结，之后不再随汇率配置变动而漂移
// （设计不变量 2）；取值优先客户端传参，缺省退回服务端当前汇率，见下方注释。
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
	// discount 模式的结算汇率：客户端传了就照原样冻结，没传才退回服务端当前汇率
	// （operation_setting.USDExchangeRate，启动时由 InitOptionMap 从 options 表同步，
	// 运行期直接读包级变量即可，无需再查库）。
	//
	// 优先客户端传值而不是一律用服务端汇率，是因为本接口的主要用途就是回填历史价：
	// 六月的那一版该按六月的汇率结算，而服务端只知道今天的汇率。用今天的汇率冻结一条
	// 六月的版本行，等于把「冻结汇率」这条不变量反过来用——历史成本恰恰因此漂移，且
	// 版本不可更新，事后无从修正。不传 exchange_rate 时语义是「按当下的价记一版」，
	// 服务端汇率正是那一刻的正确答案，继续作为默认值。
	//
	// 汇率非正时拒绝写入，而不是像 seed 那样兜底成 7.3：seed 跑在 InitOptionMap 之前，
	// 读到默认值是时序使然，只能兜底；而运行期两边都拿不到正数意味着汇率确实没配好，
	// 此时把 0 冻进不可变的版本行会让该版本下所有日志永久无法定价（成本记 0，显示成
	// 100% 毛利），只能靠再追加一版来绕过。让管理员补上汇率再重试，代价小得多。
	exRate := req.ExchangeRate
	if mode == "discount" {
		if exRate <= 0 {
			exRate = operation_setting.USDExchangeRate
		}
		if exRate <= 0 {
			common.ApiErrorMsg(c, "discount mode requires a positive exchange rate; supply exchange_rate or configure USDExchangeRate in system settings first")
			return
		}
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

// costVersionFloatEpsilon 计价字段的浮点相等容差。不能用 ==：同一个价经过
// 前端 → JSON → float64 往返后可能差出几个 ULP，逐字节比较会把「没改价」判成
// 改价，于是每保存一次渠道就多一条内容完全相同的版本。
const costVersionFloatEpsilon = 1e-9

// costVersionChanged 比对渠道当前最新版本与新设置的计价字段是否真的不同。
// 抽成纯函数是为了能被单测直接钉住——「只改 API key 不该产生版本」这条回归
// 全靠它，而它是本路径上唯一有分支的逻辑。
func costVersionChanged(latest model.ChannelCostVersion, s *dto.ChannelSettings) bool {
	// 归一化：空 CostMode 等同 "ratio"（见 ChannelCostVersion.CostMode 注释），
	// 否则一次 "" → "ratio" 的纯写法变更会被误判成改价。
	normMode := func(m string) string {
		if m == "" {
			return "ratio"
		}
		return m
	}
	sameFloat := func(a, b float64) bool {
		d := a - b
		return d < costVersionFloatEpsilon && d > -costVersionFloatEpsilon
	}
	// 三个字段都要比：版本行原样存下这三个值，任何一个不同都是一条内容不同的版本。
	return normMode(latest.CostMode) != normMode(s.CostMode) ||
		!sameFloat(latest.CostRatio, s.CostRatio) ||
		!sameFloat(latest.CostDiscount, s.CostDiscount)
}

// appendCostVersionIfChanged 在渠道保存成功后追加一条 effective_from=now 的计价版本，
// 仅当计价字段相对当前最新版本真的变了才追加。价格未配置（对应模式的值为 0）时不追加。
//
// 必须先比对：否则每次保存渠道（改 key、改模型列表…）都会插一条内容重复的版本，
// 价格历史很快会被噪声淹没，"这个价从哪天开始的"就再也读不出来了。
//
// 全部失败路径都只记日志、不向上传播：版本记录是成本核算的辅助事实，渠道已经存
// 进库了，再让接口报错既救不回这条版本，又会让管理员以为渠道没保存成功而重试。
func appendCostVersionIfChanged(c *gin.Context, channelId int, s *dto.ChannelSettings) {
	hasCost := (s.CostMode == "discount" && s.CostDiscount > 0) ||
		(s.CostMode != "discount" && s.CostRatio > 0)
	if !hasCost {
		return
	}
	// 与 CreateChannelCostVersion 的白名单保持一致：未知 cost_mode 落进不可变的版本行
	// 后无法修正，而渠道保存路径本身并不校验这个字段。
	if s.CostMode != "" && s.CostMode != "ratio" && s.CostMode != "discount" {
		common.SysError("skip auto cost version for channel " + strconv.Itoa(channelId) +
			": unknown cost_mode " + s.CostMode)
		return
	}
	versions, err := model.GetChannelCostVersions(channelId) // 降序，[0] 为最新
	if err != nil {
		common.SysError("load cost versions failed: " + err.Error())
		return
	}
	if len(versions) > 0 && !costVersionChanged(versions[0], s) {
		return
	}
	// discount 模式冻结服务端当前汇率，与 CreateChannelCostVersion 同一来源
	// （operation_setting.USDExchangeRate，启动时由 InitOptionMap 从 options 表同步）。
	//
	// 汇率非正时放弃追加，而不是兜底一个默认值：把 0 冻进不可变的版本行，该版本
	// 覆盖的日志会永久算不出成本（显示成 100% 毛利）；兜底成 7.3 更糟——数字看着
	// 正常，实际是编的。这里又不能像 API 那样退回 400：渠道已经存好了。
	// 放弃是可自愈的：汇率配好后再保存一次渠道，与最新版本的比对依然为「变了」，
	// 版本会在那时补上，只是 effective_from 顺延到那一刻。
	exRate := operation_setting.USDExchangeRate
	if s.CostMode == "discount" && exRate <= 0 {
		common.SysError("skip auto cost version for channel " + strconv.Itoa(channelId) +
			": discount mode requires a positive USDExchangeRate")
		return
	}
	// 首个版本用 effective_from=0（自古以来），而不是 now。
	//
	// 用 now 会同时挖两个坑：渠道创建到首次编辑之间的日志永远落在版本区间之前，
	// VersionAt 返回 false，这段成本永久记 0（显示成 100% 毛利）；而且一旦写入，
	// seedChannelCostVersions 的 seeded 集合就认为该渠道"已回填"，重启补种也不会
	// 再管它。版本行不可变、创建接口又硬拒 effective_from=0，事后无从补救。
	//
	// 取 0 与 seedChannelCostVersions 的语义完全一致（它同样把当前配置按
	// effective_from=0 落库），把这条历史缺口从源头堵上。
	effectiveFrom := int64(0)
	if len(versions) > 0 {
		effectiveFrom = time.Now().Unix()
		if exists, _ := model.VersionExists(channelId, effectiveFrom); exists {
			return // 同一秒内重复保存，跳过（版本不可变，同一时间点重复写入会造成歧义）
		}
	}
	v := &model.ChannelCostVersion{
		ChannelId:     channelId,
		EffectiveFrom: effectiveFrom,
		CostMode:      s.CostMode,
		CostRatio:     s.CostRatio,
		CostDiscount:  s.CostDiscount,
		ExchangeRate:  exRate,
		Note:          "auto from channel update",
		CreatedBy:     c.GetInt("id"),
	}
	if err := model.CreateChannelCostVersion(v); err != nil {
		common.SysError("auto append cost version failed: " + err.Error())
	}
}
