package controller

import (
	"errors"
	"strconv"
	"time"
	"unicode/utf8"

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
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	versions, err := model.GetChannelCostVersions(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, versions)
}

// costVersionNoteMaxRunes 与 ChannelCostVersion.Note 的 varchar(255) 对齐。
const costVersionNoteMaxRunes = 255

// 系统自动写入的版本备注。前端把它们当作英文原文键翻译后展示。
const (
	costVersionNoteChannelUpdate = "auto from channel update"
	costVersionNoteCostPage      = "set on the cost page"
)

// costVersionRequest 新增、修改价格记录与设置当前价共用的请求体；设置当前价不读
// EffectiveFrom 和 Note。
type costVersionRequest struct {
	// EffectiveFrom 刻意不加 binding:"required"：validator 对非指针 int64 的 required
	// 会把显式传入的 0（初始价）一并拦在 ShouldBindJSON 里。
	EffectiveFrom int64   `json:"effective_from"`
	CostMode      string  `json:"cost_mode"`
	CostRatio     float64 `json:"cost_ratio"`
	CostDiscount  float64 `json:"cost_discount"`
	ExchangeRate  float64 `json:"exchange_rate"`
	Note          string  `json:"note"`
}

// validateCostPrice 校验计价字段，返回控制台提示（英文原文作键，由前端翻译）。
func validateCostPrice(req *costVersionRequest) *common.Message {
	switch req.CostMode {
	// ratio（含空值）模式：CostRatio<=0 时 EffectiveRatio 返回 false，版本无法定价。
	case "", "ratio":
		if req.CostRatio <= 0 {
			return common.NewMessage("Cost ratio must be greater than 0.")
		}
	// discount 模式：CostDiscount<=0 时乘以汇率仍为 0，版本同样无法定价。
	case "discount":
		if req.CostDiscount <= 0 {
			return common.NewMessage("Cost discount must be greater than 0.")
		}
	// 未知值写入后 EffectiveRatio 返回 false，该版本下的日志永久无法定价。
	default:
		return common.NewMessage("Unknown pricing mode. Use ratio or discount.")
	}
	// note 列是 varchar(255)。不校验的话同一个超长请求在三库上表现不同：PG 报
	// 22001、MySQL 严格模式报 1406，SQLite 不校验声明长度于是静默存下。按字符数而非
	// 字节数算——varchar(255) 在三库上都是 255 个字符。
	if utf8.RuneCountInString(req.Note) > costVersionNoteMaxRunes {
		return common.NewMessage("Note is too long (max 255 characters).")
	}
	return nil
}

// describeCostVersion 把价格写成审计日志里的短串：ratio 3.1 / discount 0.31 × 6.8。
func describeCostVersion(v model.ChannelCostVersion) string {
	if v.CostMode == "discount" {
		return "discount " + strconv.FormatFloat(v.CostDiscount, 'f', -1, 64) +
			" × " + strconv.FormatFloat(v.ExchangeRate, 'f', -1, 64)
	}
	return "ratio " + strconv.FormatFloat(v.CostRatio, 'f', -1, 64)
}

// afterCostVersionChange 在价格记录写入之后收尾：作废成本缓存，再把渠道设置里的
// 成本价同步成此刻生效的价，渠道编辑页显示的才是实际的价。同步失败只记日志：版本
// 已经写入，是成本的唯一依据，下一次改动还会再同步。
func afterCostVersionChange(channelId int) {
	costCubeCacheClear()
	if err := model.SyncChannelCostSetting(channelId, time.Now().Unix()); err != nil {
		common.SysError(common.LogText("sync channel cost setting failed: channel_id=%d, error=%v", channelId, err))
		return
	}
	model.InitChannelCache()
}

// CreateChannelCostVersion POST /api/cost/channels/:id/versions
// 新增一条价格记录。effective_from=0 是初始价，适用于第一条带日期记录之前的全部
// 日志，渠道已有初始价时拒绝。discount 模式下 ExchangeRate 随版本一并冻结，之后不再
// 随汇率配置变动而漂移；取值优先客户端传参，缺省退回服务端当前汇率，见下方注释。
func CreateChannelCostVersion(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	var req costVersionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	if req.EffectiveFrom < 0 {
		common.ApiErrorT(c, "The effective time cannot be negative.")
		return
	}
	if msg := validateCostPrice(&req); msg != nil {
		common.ApiError(c, msg)
		return
	}
	// 渠道必须存在：孤儿版本行虽然当下无日志引用、不会误算成本，但会被
	// GetAllChannelCostVersions 悄悄载入 VersionMap；早点报错比静默成功易排查。
	if _, err := model.GetChannelById(id, false); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			common.ApiErrorT(c, "Channel does not exist")
			return
		}
		common.ApiError(c, err)
		return
	}
	// 同渠道同 effective_from 已存在则拒绝：同一时间点两条价会造成歧义。
	// 该检查与下方写入非原子，但这是仅超管可达的低频路径，冲突窗口可接受。
	exists, err := model.VersionExists(id, req.EffectiveFrom)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if exists && req.EffectiveFrom == 0 {
		common.ApiErrorT(c, "This channel already has an initial price. Edit it instead.")
		return
	}
	if exists {
		common.ApiErrorT(c, "A price record with this effective time already exists for this channel.")
		return
	}
	// discount 模式的结算汇率：客户端传了就照原样冻结，没传才退回服务端当前汇率
	// （operation_setting.USDExchangeRate，启动时由 InitOptionMap 从 options 表同步，
	// 运行期直接读包级变量即可，无需再查库）。
	//
	// 优先客户端传值而不是一律用服务端汇率，是因为本接口的主要用途就是回填历史价：
	// 六月的那一版该按六月的汇率结算，而服务端只知道今天的汇率。不传 exchange_rate 时
	// 语义是「按当下的价记一版」，服务端汇率正是那一刻的正确答案，继续作为默认值。
	//
	// 汇率非正时拒绝写入，而不是像 seed 那样兜底成 7.3：seed 跑在 InitOptionMap 之前，
	// 读到默认值是时序使然，只能兜底；而运行期两边都拿不到正数意味着汇率确实没配好，
	// 此时把 0 冻进版本行会让该版本下所有日志无法定价（成本记 0，显示成 100% 毛利）。
	exRate := req.ExchangeRate
	if req.CostMode == "discount" {
		if exRate <= 0 {
			exRate = operation_setting.USDExchangeRate
		}
		if exRate <= 0 {
			common.ApiErrorT(c, "Discount pricing needs a settlement exchange rate. Enter one, or set the USD exchange rate in system settings first.")
			return
		}
	}
	// CreatedBy 取中间件已验证的登录用户 id，不信任客户端传值。
	v := &model.ChannelCostVersion{
		ChannelId:     id,
		EffectiveFrom: req.EffectiveFrom,
		CostMode:      req.CostMode,
		CostRatio:     req.CostRatio,
		CostDiscount:  req.CostDiscount,
		ExchangeRate:  exRate,
		Note:          req.Note,
		CreatedBy:     c.GetInt("id"),
	}
	if err := model.CreateChannelCostVersion(v); err != nil {
		common.ApiError(c, err)
		return
	}
	afterCostVersionChange(id)
	recordManageAudit(c, "cost.version_create", map[string]any{
		"channel_id": id, "version_id": v.Id, "effective_from": v.EffectiveFrom,
		"after": describeCostVersion(*v),
	})
	common.ApiSuccess(c, v)
}

// UpdateChannelCostVersion PUT /api/cost/versions/:vid
// 修改一条价格记录（含初始价）。初始价的生效时间固定为 0；带日期的记录可以改时间。
// 折扣模式必须带结算汇率：不能像新增那样退回站点汇率，否则会悄悄改掉这条历史记录
// 冻结的汇率。
func UpdateChannelCostVersion(c *gin.Context) {
	vid, err := strconv.Atoi(c.Param("vid"))
	if err != nil || vid <= 0 {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	var req costVersionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	if req.EffectiveFrom < 0 {
		common.ApiErrorT(c, "The effective time cannot be negative.")
		return
	}
	if msg := validateCostPrice(&req); msg != nil {
		common.ApiError(c, msg)
		return
	}
	if req.CostMode == "discount" && req.ExchangeRate <= 0 {
		common.ApiErrorT(c, "Settlement exchange rate must be greater than 0.")
		return
	}
	v := &model.ChannelCostVersion{
		Id:            vid,
		EffectiveFrom: req.EffectiveFrom,
		CostMode:      req.CostMode,
		CostRatio:     req.CostRatio,
		CostDiscount:  req.CostDiscount,
		ExchangeRate:  req.ExchangeRate,
		Note:          req.Note,
	}
	before, err := model.UpdateChannelCostVersion(v)
	switch {
	case errors.Is(err, model.ErrVersionNotFound):
		common.ApiErrorT(c, "Price record not found.")
		return
	case errors.Is(err, model.ErrInitialVersionTime):
		common.ApiErrorT(c, "The initial price and dated records cannot be converted into each other.")
		return
	case errors.Is(err, model.ErrVersionTimeTaken):
		common.ApiErrorT(c, "A price record with this effective time already exists for this channel.")
		return
	case err != nil:
		common.ApiError(c, err)
		return
	}
	afterCostVersionChange(v.ChannelId)
	recordManageAudit(c, "cost.version_update", map[string]any{
		"channel_id": v.ChannelId, "version_id": v.Id, "effective_from": v.EffectiveFrom,
		"before": describeCostVersion(before), "after": describeCostVersion(*v),
	})
	common.ApiSuccess(c, v)
}

// DeleteChannelCostVersion DELETE /api/cost/versions/:vid
// 删除指定版本（幂等：版本已不存在时返回成功）。
//
// 唯一的拒绝条件是该渠道只剩这一条版本：删光后渠道没有任何价格可用，全部日志成本
// 记 0。初始版本（effective_from=0）可以删，删掉后由后面的记录往前覆盖（见
// model.VersionMap.VersionAt）。管理员如需换掉唯一的一条，应先补录新版本再删旧版本。
func DeleteChannelCostVersion(c *gin.Context) {
	vid, err := strconv.Atoi(c.Param("vid"))
	if err != nil || vid <= 0 {
		common.ApiErrorT(c, "Invalid parameters")
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
	// 校验与删除的原子性由 model 层负责（事务 + 行锁），见
	// DeleteChannelCostVersionIfNotLast。拒绝是预期内的业务结果而非内部故障，
	// 所以给一句可操作的提示，不走 ApiError 那条通道。
	if err := model.DeleteChannelCostVersionIfNotLast(v.ChannelId, vid); err != nil {
		if errors.Is(err, model.ErrLastVersion) {
			common.ApiErrorT(c, "Cannot delete the last price record of a channel. Add another record first.")
			return
		}
		common.ApiError(c, err)
		return
	}
	afterCostVersionChange(v.ChannelId)
	recordManageAudit(c, "cost.version_delete", map[string]any{
		"channel_id": v.ChannelId, "version_id": v.Id, "effective_from": v.EffectiveFrom,
		"before": describeCostVersion(v),
	})
	common.ApiSuccess(c, nil)
}

// costVersionFloatEpsilon 计价字段的浮点相等容差。不能用 ==：同一个价经过
// 前端 → JSON → float64 往返后可能差出几个 ULP，逐字节比较会把「没改价」判成
// 改价，于是每保存一次渠道就多一条内容完全相同的版本。
const costVersionFloatEpsilon = 1e-9

// versionInEffect 从降序版本列表中取出 ts 时刻生效的那条：即 effective_from <= ts
// 中 effective_from 最大的一条。列表为降序，第一条满足条件的就是答案。
// 全部版本都在 ts 之后（该渠道当时还没有任何定价）时返回 ok=false。
//
// 与 model.VersionAt 的取舍：这里复用调用方已经读到的 versions 切片，避免为同一次
// 保存再查一遍库；语义（闭区间起点、取最后一个不晚于 ts 的版本）与之保持一致。
func versionInEffect(versions []model.ChannelCostVersion, ts int64) (model.ChannelCostVersion, bool) {
	for _, v := range versions {
		if v.EffectiveFrom <= ts {
			return v, true
		}
	}
	return model.ChannelCostVersion{}, false
}

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
	if normMode(latest.CostMode) != normMode(s.CostMode) {
		return true
	}
	// 只比当前模式用到的字段：渠道设置会留着另一模式的旧值（切回去时还能看到），
	// 折扣模式下残留的旧倍率不算改价，否则每次保存都会多追一条价格相同的版本。
	if normMode(s.CostMode) == "ratio" {
		return !sameFloat(latest.CostRatio, s.CostRatio)
	}
	// 折扣模式还要比结算汇率：同一个折扣换个汇率结算就是另一个价。设置里没带汇率
	// （只提交折扣的旧客户端）时不参与比对，否则每次保存都会多追一条版本。
	rateChanged := s.CostExchangeRate > 0 && !sameFloat(latest.ExchangeRate, s.CostExchangeRate)
	return !sameFloat(latest.CostDiscount, s.CostDiscount) || rateChanged
}

// appendCostVersionIfChanged 追加一条 effective_from=now 的计价版本，仅当计价字段相对
// 此刻生效的版本真的变了才追加。价格未配置（对应模式的值为 0）时不追加。note 写进
// 新版本的备注。
//
// 必须先比对：否则每次保存渠道（改 key、改模型列表…）都会插一条内容重复的版本，
// 价格历史很快会被噪声淹没，"这个价从哪天开始的"就再也读不出来了。
//
// 渠道保存的钩子只记日志、不向上传播错误：渠道已经存进库了，再让接口报错既救不回
// 这条版本，又会让管理员以为渠道没保存成功而重试。设置当前价的接口把错误返回给前端。
func appendCostVersionIfChanged(c *gin.Context, channelId int, s *dto.ChannelSettings, note string) error {
	hasCost := (s.CostMode == "discount" && s.CostDiscount > 0) ||
		(s.CostMode != "discount" && s.CostRatio > 0)
	if !hasCost {
		return nil
	}
	// 与 CreateChannelCostVersion 的白名单保持一致：渠道保存路径本身并不校验这个字段，
	// 未知 cost_mode 落进版本行后该版本下的日志无法定价。
	if s.CostMode != "" && s.CostMode != "ratio" && s.CostMode != "discount" {
		return errors.New("unknown cost_mode " + s.CostMode)
	}
	versions, err := model.GetChannelCostVersions(channelId) // 按 effective_from 降序
	if err != nil {
		return err
	}
	// 比对基准取「此刻生效中」的版本，而不是 versions[0]。
	//
	// versions[0] 是 effective_from 最大的一条，可能尚未生效（未来时间点是允许的）。
	// 拿未来那条比对会把真实改价误判成没变：基线 2.5 生效中、排期一条明天的 3.0、再把
	// 渠道当前价改成 3.0 —— 与未来那条相等，于是不追版本，今天的流量继续按 2.5 计成本。
	if inEffect, ok := versionInEffect(versions, time.Now().Unix()); ok && !costVersionChanged(inEffect, s) {
		return nil
	}
	// discount 模式冻结渠道设置里填的结算汇率；没填时退回服务端当前汇率
	// （operation_setting.USDExchangeRate，启动时由 InitOptionMap 从 options 表同步）。
	// 优先取设置值：USDExchangeRate 是全站展示用的设置，可以被配成 1，拿它冻结会把
	// 折扣渠道的成本算小好几倍。
	//
	// 汇率非正时放弃追加，而不是兜底一个默认值：把 0 冻进版本行，该版本覆盖的日志会
	// 算不出成本（显示成 100% 毛利）；兜底成 7.3 更糟——数字看着正常，实际是编的。
	exRate := s.CostExchangeRate
	if exRate <= 0 {
		exRate = operation_setting.USDExchangeRate
	}
	if s.CostMode == "discount" && exRate <= 0 {
		return errors.New("discount mode requires a positive exchange rate")
	}
	// 首个版本用 effective_from=0（自古以来），而不是 now：它就是版本历史里的「初始」
	// 价，覆盖渠道创建到首次编辑之间的全部日志。取 0 与 seedChannelCostVersions 的
	// 语义完全一致（它同样把当前配置按 effective_from=0 落库）。
	effectiveFrom := int64(0)
	if len(versions) > 0 {
		effectiveFrom = time.Now().Unix()
		if exists, _ := model.VersionExists(channelId, effectiveFrom); exists {
			return nil // 同一秒内重复保存，跳过（同一时间点两条价会造成歧义）
		}
	}
	v := &model.ChannelCostVersion{
		ChannelId:     channelId,
		EffectiveFrom: effectiveFrom,
		CostMode:      s.CostMode,
		CostRatio:     s.CostRatio,
		CostDiscount:  s.CostDiscount,
		ExchangeRate:  exRate,
		Note:          note,
		CreatedBy:     c.GetInt("id"),
	}
	if err := model.CreateChannelCostVersion(v); err != nil {
		return err
	}
	// 新价已生效，缓存里那份按旧版本算出的成本必须作废——否则改完渠道价立刻看报表
	// 仍是旧数字。放在这里而非各个调用点，所有路径就都覆盖到了。
	costCubeCacheClear()
	return nil
}

// SetChannelCostPrice PUT /api/cost/channels/:id/price
// 成本核算页弹窗上半部分：设置从现在起的价。与此刻生效的价不同才追加一条从现在起
// 生效的记录（渠道还没有记录时作为初始价），然后同步渠道设置。
//
// 不借道 PUT /api/channel：那条路径只在成本字段相对原渠道设置改过时才追加记录，而
// 这里要和此刻生效的价比较——渠道设置可能和它不一致（比如有一条到点生效的记录），
// 恰好填回设置里的旧值时就会提示保存成功、价却没变。
func SetChannelCostPrice(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	var req costVersionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	req.Note = ""
	if msg := validateCostPrice(&req); msg != nil {
		common.ApiError(c, msg)
		return
	}
	if req.CostMode == "discount" && req.ExchangeRate <= 0 {
		common.ApiErrorT(c, "Settlement exchange rate must be greater than 0.")
		return
	}
	if _, err := model.GetChannelById(id, false); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			common.ApiErrorT(c, "Channel does not exist")
			return
		}
		common.ApiError(c, err)
		return
	}
	versions, err := model.GetChannelCostVersions(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	before, _ := versionInEffect(versions, time.Now().Unix())
	price := &dto.ChannelSettings{
		CostMode:         req.CostMode,
		CostRatio:        req.CostRatio,
		CostDiscount:     req.CostDiscount,
		CostExchangeRate: req.ExchangeRate,
	}
	if err := appendCostVersionIfChanged(c, id, price, costVersionNoteCostPage); err != nil {
		common.ApiError(c, err)
		return
	}
	afterCostVersionChange(id)
	recordManageAudit(c, "cost.price_set", map[string]any{
		"channel_id": id,
		"before":     describeCostVersion(before),
		"after": describeCostVersion(model.ChannelCostVersion{CostMode: req.CostMode, CostRatio: req.CostRatio,
			CostDiscount: req.CostDiscount, ExchangeRate: req.ExchangeRate}),
	})
	common.ApiSuccess(c, nil)
}

// appendNewChannelCostVersions 为一批新建的渠道补计价版本。
//
// 为什么新建也要挂钩子：没有它，"新建带价渠道 → 流量立刻进来"这条最普通的路径下渠道
// 全程无版本，VersionAt 解析不到，成本报表把它算成 0 成本、100% 毛利。自愈只发生在
// 下次编辑或进程重启，所以长期不重启的实例上错误数字能挂满整个运行周期，且毫无征兆。
//
// 逐个复用 appendCostVersionIfChanged，而不另写一套：hasCost 门槛、汇率非正时放弃、
// 首版本写 effective_from=0 这三条判断都得跟编辑路径完全一致，复制一遍就是复制三个
// 未来会走偏的分支。新渠道零版本，所以那里的比对必然判定为"变了"。
//
// 依赖 BatchInsertChannels 把自增 Id 回填进传入的 channels（由
// model.TestBatchInsertChannels_BackfillsIds 钉住）：Id 为 0 时会写出 channel_id=0
// 的孤儿版本行，比没有版本更难查，所以下面仍显式挡一道。
func appendNewChannelCostVersions(c *gin.Context, channels []model.Channel) {
	for i := range channels {
		ch := &channels[i]
		if ch.Id <= 0 {
			common.SysError("skip auto cost version: channel id not backfilled after insert, name=" + ch.Name)
			continue
		}
		if ch.Setting == nil || *ch.Setting == "" {
			continue
		}
		var s dto.ChannelSettings
		if err := common.UnmarshalJsonStr(*ch.Setting, &s); err != nil {
			common.SysError("parse channel setting for cost version failed: " + err.Error())
			continue
		}
		if err := appendCostVersionIfChanged(c, ch.Id, &s, costVersionNoteChannelUpdate); err != nil {
			common.SysError(common.LogText("append cost version failed: channel_id=%d, error=%v", ch.Id, err))
		}
	}
}
