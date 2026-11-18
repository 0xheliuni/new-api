package controller

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

// GetChannelCooldowns lists the channels that are cooling down or ramping
// their weight back up after a cooldown. GET /api/channel/cooldowns
func GetChannelCooldowns(c *gin.Context) {
	common.ApiSuccess(c, gin.H{
		"enabled":              model.CurrentRequestPolicy().Scheduling.CooldownEnabled,
		"memory_cache_enabled": common.MemoryCacheEnabled,
		"items":                service.ChannelCooldownStatuses(),
	})
}
