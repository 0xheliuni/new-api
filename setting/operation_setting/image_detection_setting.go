package operation_setting

import (
	"strings"

	"github.com/QuantumNous/new-api/setting/config"
)

// ImageDetectionScope values select which channels feed the detection pipeline.
const (
	ImageDetectionScopeAll      = "all"      // 全部渠道
	ImageDetectionScopeSelected = "selected" // 多选渠道
)

// ImageDetectionSetting configures the OpenAI image-series detection pipeline.
// The pipeline has three layers that run in order and short-circuit: L1
// resolution check, L2 C2PA metadata, L3 SynthID API. Three trigger modes feed
// it: per-request global detection, cron-scheduled sampling, and targeted
// channel check (manual). SynthID calls are free but rate-limited upstream, so
// the per-minute and per-day caps plus content-hash caching keep the call
// volume bounded.
type ImageDetectionSetting struct {
	// 全局检测：每次 ImageHelper 成功返回后按采样率触发。
	GlobalEnabled    bool    `json:"global_enabled"`
	GlobalSampleRate float64 `json:"global_sample_rate"` // 0.0–1.0；1.0 = 每个请求都检测

	// 定时抽样：按 cron 表达式触发，从历史 task 表批量捞未检测记录。
	ScheduledEnabled    bool   `json:"scheduled_enabled"`
	ScheduledCron       string `json:"scheduled_cron"`        // 标准 5 段 cron，如 "0 */2 * * *"
	ScheduledBatchLimit int    `json:"scheduled_batch_limit"` // 每次最多处理几条历史记录

	// 渠道范围：全局检测 + 定时抽样共用。
	ChannelScope       string `json:"channel_scope"`        // "all" | "selected"
	SelectedChannelIDs []int  `json:"selected_channel_ids"` // scope=selected 时生效

	// 检测项开关：执行时按序判断，未开启的层级直接跳过。
	CheckResolution bool `json:"check_resolution"` // L1: 分辨率校验
	CheckC2PA       bool `json:"check_c2pa"`       // L2: C2PA 元数据
	CheckSynthID    bool `json:"check_synth_id"`   // L3: SynthID API
	// SynthIDForce 为 true 时，即使 L2 C2PA 已检测到信号也继续执行 L3，
	// 用 SynthID 官方内容检测复核元数据（元数据可被伪造或转写）。
	SynthIDForce bool `json:"synth_id_force"`
	// ProvenanceAPIKey 是调用 OpenAI 内容溯源接口的密钥，优先于环境变量
	// OPENAI_API_KEY_DETECTION / OPENAI_API_KEY。选项键以 api_key 结尾，
	// GetOptions 不会把它回传给前端。
	ProvenanceAPIKey string `json:"provenance_api_key"`

	// SynthID 限速：内容哈希缓存去重 + 滑动窗口令牌桶。
	SynthIDRatePerMinute int    `json:"synth_id_rate_per_minute"` // 默认 50
	SynthIDRatePerDay    int    `json:"synth_id_rate_per_day"`    // 默认 500
	C2PAToolPath         string `json:"c2pa_tool_path"`           // c2patool 可执行路径
	C2PATimeoutSeconds   int    `json:"c2pa_timeout_seconds"`     // 默认 3
}

const (
	DefaultImageDetectionSampleRate         = 1.0
	DefaultImageDetectionCron               = "0 */2 * * *"
	DefaultImageDetectionBatchLimit         = 200
	DefaultImageDetectionSynthIDPerMinute   = 50
	DefaultImageDetectionSynthIDPerDay      = 500
	DefaultImageDetectionC2PATimeoutSeconds = 3
	MaxImageDetectionSampleRate             = 1.0
	MaxImageDetectionBatchLimit             = 1000
	MaxImageDetectionSynthIDRate            = 10000
	MaxImageDetectionC2PATimeoutSeconds     = 30
)

var imageDetectionSetting = ImageDetectionSetting{
	GlobalEnabled:        false,
	GlobalSampleRate:     DefaultImageDetectionSampleRate,
	ScheduledEnabled:     false,
	ScheduledCron:        DefaultImageDetectionCron,
	ScheduledBatchLimit:  DefaultImageDetectionBatchLimit,
	ChannelScope:         ImageDetectionScopeAll,
	SelectedChannelIDs:   nil,
	CheckResolution:      true,
	CheckC2PA:            true,
	CheckSynthID:         true,
	SynthIDRatePerMinute: DefaultImageDetectionSynthIDPerMinute,
	SynthIDRatePerDay:    DefaultImageDetectionSynthIDPerDay,
	C2PAToolPath:         "c2patool",
	C2PATimeoutSeconds:   DefaultImageDetectionC2PATimeoutSeconds,
}

func init() {
	config.GlobalConfig.Register("image_detection_setting", &imageDetectionSetting)
}

// GetImageDetectionSetting returns the shared image-detection config instance.
func GetImageDetectionSetting() *ImageDetectionSetting {
	s := &imageDetectionSetting
	s.ChannelScope = normalizeImageDetectionScope(s.ChannelScope)
	s.GlobalSampleRate = normalizeImageDetectionSampleRate(s.GlobalSampleRate)
	s.ScheduledBatchLimit = normalizeImageDetectionBatchLimit(s.ScheduledBatchLimit)
	s.SynthIDRatePerMinute = normalizeImageDetectionRate(s.SynthIDRatePerMinute, DefaultImageDetectionSynthIDPerMinute)
	s.SynthIDRatePerDay = normalizeImageDetectionRate(s.SynthIDRatePerDay, DefaultImageDetectionSynthIDPerDay)
	s.C2PATimeoutSeconds = normalizeImageDetectionC2PATimeout(s.C2PATimeoutSeconds)
	return s
}

func normalizeImageDetectionScope(scope string) string {
	switch strings.TrimSpace(scope) {
	case ImageDetectionScopeSelected:
		return ImageDetectionScopeSelected
	default:
		return ImageDetectionScopeAll
	}
}

func normalizeImageDetectionSampleRate(rate float64) float64 {
	if rate <= 0 {
		return 0
	}
	if rate > MaxImageDetectionSampleRate {
		return MaxImageDetectionSampleRate
	}
	return rate
}

func normalizeImageDetectionBatchLimit(limit int) int {
	if limit <= 0 {
		return DefaultImageDetectionBatchLimit
	}
	if limit > MaxImageDetectionBatchLimit {
		return MaxImageDetectionBatchLimit
	}
	return limit
}

func normalizeImageDetectionRate(rate, fallback int) int {
	if rate <= 0 {
		return fallback
	}
	if rate > MaxImageDetectionSynthIDRate {
		return MaxImageDetectionSynthIDRate
	}
	return rate
}

func normalizeImageDetectionC2PATimeout(seconds int) int {
	if seconds <= 0 {
		return DefaultImageDetectionC2PATimeoutSeconds
	}
	if seconds > MaxImageDetectionC2PATimeoutSeconds {
		return MaxImageDetectionC2PATimeoutSeconds
	}
	return seconds
}

// ChannelIDsForDetection returns the channel IDs that should participate in
// detection. When scope is selected and the configured list is non-empty, only
// those IDs are returned; otherwise allChannelIDs is returned unchanged so the
// caller can still run against every channel.
func (s *ImageDetectionSetting) ChannelIDsForDetection(allChannelIDs []int) []int {
	if s.ChannelScope == ImageDetectionScopeSelected && len(s.SelectedChannelIDs) > 0 {
		return s.SelectedChannelIDs
	}
	return allChannelIDs
}

// IsChannelInScope reports whether channelID should be detected under the
// current scope setting. Used by the per-request global trigger.
func (s *ImageDetectionSetting) IsChannelInScope(channelID int, allChannelIDs []int) bool {
	allowed := s.ChannelIDsForDetection(allChannelIDs)
	if s.ChannelScope != ImageDetectionScopeSelected {
		return true
	}
	for _, id := range allowed {
		if id == channelID {
			return true
		}
	}
	return false
}
