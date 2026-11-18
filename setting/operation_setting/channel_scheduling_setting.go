package operation_setting

import "github.com/QuantumNous/new-api/setting/config"

// ChannelSchedulingSetting holds the cooldown, tiered attempt and disable
// error code options of the request policy. Request paths read the parsed
// copy in the request policy snapshot, not this struct.
type ChannelSchedulingSetting struct {
	CooldownEnabled       bool   `json:"cooldown_enabled"`
	CooldownStatusCodes   string `json:"cooldown_status_codes"`
	CooldownBaseSeconds   int    `json:"cooldown_base_seconds"`
	CooldownMaxSeconds    int    `json:"cooldown_max_seconds"`
	DisableAfterCooldowns int    `json:"disable_after_cooldowns"`
	DisableErrorCodes     string `json:"disable_error_codes"`
	TierAttemptLimit      int    `json:"tier_attempt_limit"`
}

var channelSchedulingSetting = ChannelSchedulingSetting{
	CooldownEnabled:       false,
	CooldownStatusCodes:   "429,500-503,505-523,525-599",
	CooldownBaseSeconds:   30,
	CooldownMaxSeconds:    600,
	DisableAfterCooldowns: 5,
	DisableErrorCodes:     "",
	TierAttemptLimit:      1,
}

func init() {
	config.GlobalConfig.Register("channel_scheduling_setting", &channelSchedulingSetting)
}

func GetChannelSchedulingSetting() *ChannelSchedulingSetting {
	return &channelSchedulingSetting
}
