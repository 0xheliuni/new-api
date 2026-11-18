package service

import (
	"cmp"
	"slices"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-gonic/gin"
)

const (
	// A channel leaves a cooldown at a quarter of its weight; every success
	// doubles the share until it is back to the configured weight.
	cooldownRampStart = 0.25
	// Reaching the cooldown limit does not disable a channel while more than
	// half of a priority level with at least this many channels is cooling
	// down: that points at the upstream rather than at one key.
	cooldownGuardMinTier = 4

	ginKeyChannelAffinityCooldownSkip = "channel_affinity_cooldown_skip"
)

type channelFailureClass int

const (
	channelFailureIgnored channelFailureClass = iota
	channelFailurePermanent
	channelFailureTransient
)

// channelCooldown is the in-memory health record of one single-key channel.
type channelCooldown struct {
	// consecutive counts cooldowns without a success in between.
	consecutive int
	// startedAt is the start of the latest cooldown. Results of attempts that
	// began earlier are not counted.
	startedAt time.Time
	// The channel is cooling down while the current time is before until.
	until         time.Time
	weightFactor  float64
	lastStatus    int
	lastErrorCode string
}

// ChannelCooldownStatus is one item of GET /api/channel/cooldowns.
type ChannelCooldownStatus struct {
	ChannelID      int     `json:"channel_id"`
	State          string  `json:"state"`
	CooldownUntil  int64   `json:"cooldown_until"`
	CooldownCount  int     `json:"cooldown_count"`
	WeightFactor   float64 `json:"weight_factor"`
	LastStatusCode int     `json:"last_status_code"`
	LastErrorCode  string  `json:"last_error_code"`
}

var channelCooldowns = struct {
	sync.Mutex
	entries map[int]*channelCooldown
}{entries: make(map[int]*channelCooldown)}

// cooldownNow is the clock of the cooldown records; tests replace it.
var cooldownNow = time.Now

// channelCooldownActive reports whether cooldowns take part in channel
// selection. They need the in-memory channel cache as well as the switch.
func channelCooldownActive(policy *model.ChannelSchedulingPolicy) bool {
	return common.MemoryCacheEnabled && policy.CooldownEnabled
}

// classifyChannelFailure sorts a failed attempt into a dead key, a temporary
// upstream failure, or a failure that says nothing about the channel.
func classifyChannelFailure(c *gin.Context, err *types.NewAPIError, policy *model.ChannelSchedulingPolicy) channelFailureClass {
	if err == nil {
		return channelFailureIgnored
	}
	// A client that hung up says nothing about the channel.
	if c != nil && c.Request != nil && c.Request.Context().Err() != nil {
		return channelFailureIgnored
	}
	if !types.IsChannelError(err) {
		if types.IsSkipRetryError(err) {
			return channelFailureIgnored
		}
		if err.GetErrorType() == types.ErrorTypeNewAPIError && err.GetErrorCode() != types.ErrorCodeDoRequestFailed {
			return channelFailureIgnored
		}
	}
	if IsChannelDisableError(err) {
		return channelFailurePermanent
	}
	if err.GetErrorCode() == types.ErrorCodeDoRequestFailed {
		return channelFailureTransient
	}
	if slices.ContainsFunc(policy.CooldownCodes, func(codes operation_setting.StatusCodeRange) bool {
		return err.StatusCode >= codes.Start && err.StatusCode <= codes.End
	}) {
		return channelFailureTransient
	}
	return channelFailureIgnored
}

// RecordChannelCooldownFailure counts a failed attempt against the cooldown
// record of a single-key channel. ProcessChannelError calls it before the
// persisted error log is recorded, so the log row's request_policy events
// include the cooldown decision.
func RecordChannelCooldownFailure(c *gin.Context, channelError types.ChannelError, err *types.NewAPIError) {
	policy := &model.CurrentRequestPolicy().Scheduling
	if !channelCooldownActive(policy) || channelError.IsMultiKey || channelError.ChannelId <= 0 {
		return
	}
	class := classifyChannelFailure(c, err, policy)
	if class == channelFailureIgnored {
		return
	}
	disableAllowed := common.AutomaticDisableChannelEnabled && channelError.AutoBan
	if class == channelFailurePermanent && disableAllowed {
		// The automatic disable in ProcessChannelError handles a dead key.
		return
	}
	state := RequestPolicy(c)
	attemptStart := state.attemptStartedAt()
	limitEnabled := disableAllowed && policy.DisableAfterCooldowns > 0
	// The priority level of the failed channel decides whether the upstream
	// as a whole is failing.
	var tier []int
	if limitEnabled && c != nil {
		group := common.GetContextKeyString(c, constant.ContextKeyAutoGroup)
		if group == "" {
			group = state.SelectedGroup
		}
		candidates, _ := model.ListSatisfiedChannels(group, c.GetString("original_model"), GetChannelConstraints(c).Filters)
		index := slices.IndexFunc(candidates, func(candidate *model.Channel) bool {
			return candidate.Id == channelError.ChannelId
		})
		for _, candidate := range candidates {
			if index >= 0 && candidate.GetPriority() == candidates[index].GetPriority() {
				tier = append(tier, candidate.Id)
			}
		}
	}

	now := cooldownNow()
	channelCooldowns.Lock()
	entry := channelCooldowns.entries[channelError.ChannelId]
	if entry == nil {
		entry = &channelCooldown{weightFactor: 1}
		channelCooldowns.entries[channelError.ChannelId] = entry
	}
	if now.Before(entry.until) || attemptStart.Before(entry.startedAt) {
		channelCooldowns.Unlock()
		state.AddEvent(PolicyEvent{ChannelID: channelError.ChannelId, Decision: PolicyDecision{Action: "health", Reason: "already_cooling", Source: "cooldown"}})
		return
	}
	entry.consecutive++
	entry.lastStatus, entry.lastErrorCode = err.StatusCode, string(err.GetErrorCode())
	count := entry.consecutive
	limitReached := limitEnabled && count >= policy.DisableAfterCooldowns
	upstreamWide := false
	if limitReached && len(tier) >= cooldownGuardMinTier {
		cooling := 0
		for _, id := range tier {
			other := channelCooldowns.entries[id]
			if id == channelError.ChannelId || other != nil && now.Before(other.until) {
				cooling++
			}
		}
		upstreamWide = cooling*2 > len(tier)
	}
	if limitReached && !upstreamWide {
		// A channel enabled again by the recovery test starts at a quarter of
		// its weight; results of attempts from before the disable are ignored.
		entry.consecutive, entry.until = 0, time.Time{}
		entry.startedAt, entry.weightFactor = now, cooldownRampStart
		channelCooldowns.Unlock()
		state.AddEvent(PolicyEvent{ChannelID: channelError.ChannelId, Decision: PolicyDecision{Action: "health", Reason: "cooldown_disable_requested", Source: "cooldown"}, CooldownCount: count})
		common.SysLog(common.LogText("channel %q (#%d) reached %d consecutive cooldowns, disabling it", channelError.ChannelName, channelError.ChannelId, count))
		gopool.Go(func() {
			DisableChannel(channelError, model.ChannelStatusReasonRepeatedCooldowns)
		})
		return
	}
	// The loader keeps the maximum at or above the base; the guard keeps the
	// state machine self-contained, so a zero maximum cannot zero the cooldown.
	maxCooldown := max(policy.CooldownMax, policy.CooldownBase)
	duration := policy.CooldownBase
	for range count - 1 {
		if duration >= maxCooldown {
			break
		}
		duration *= 2
	}
	duration = min(duration, maxCooldown)
	entry.startedAt, entry.until, entry.weightFactor = now, now.Add(duration), cooldownRampStart
	channelCooldowns.Unlock()

	seconds := int(duration / time.Second)
	reason := "cooldown_started"
	if limitReached {
		reason = "cooldown_upstream_wide"
		common.SysLog(common.LogText("channel %q (#%d) reached %d consecutive cooldowns, but most channels at its priority are cooling down; keeping it enabled", channelError.ChannelName, channelError.ChannelId, count))
	}
	common.SysLog(common.LogText("channel %q (#%d) cooling down for %ds after %d consecutive temporary failures, last status code %d", channelError.ChannelName, channelError.ChannelId, seconds, count, err.StatusCode))
	state.AddEvent(PolicyEvent{ChannelID: channelError.ChannelId, Decision: PolicyDecision{Action: "health", Reason: reason, Source: "cooldown"}, CooldownCount: count, CooldownSeconds: seconds})
}

// RecordChannelCooldownSuccess lets a channel that succeeded take more traffic
// again: it clears the backoff, ends a cooldown early and doubles the weight
// share of a recovering channel.
func RecordChannelCooldownSuccess(c *gin.Context, channelID int) {
	if channelID <= 0 || !channelCooldownActive(&model.CurrentRequestPolicy().Scheduling) {
		return
	}
	attemptStart := RequestPolicy(c).attemptStartedAt()
	now := cooldownNow()
	channelCooldowns.Lock()
	defer channelCooldowns.Unlock()
	entry := channelCooldowns.entries[channelID]
	if entry == nil || attemptStart.Before(entry.startedAt) {
		return
	}
	entry.consecutive = 0
	if now.Before(entry.until) {
		entry.until = now
	} else {
		entry.weightFactor = min(1, entry.weightFactor*2)
	}
	if entry.weightFactor >= 1 {
		delete(channelCooldowns.entries, channelID)
	}
}

// ClearChannelCooldowns drops the cooldown records of channels an
// administrator edited or switched on or off by hand.
func ClearChannelCooldowns(channelIDs ...int) {
	channelCooldowns.Lock()
	defer channelCooldowns.Unlock()
	for _, id := range channelIDs {
		delete(channelCooldowns.entries, id)
	}
}

// IsChannelCoolingDown reports whether channelID is in an active cooldown.
func IsChannelCoolingDown(channelID int) bool {
	if !channelCooldownActive(&model.CurrentRequestPolicy().Scheduling) {
		return false
	}
	now := cooldownNow()
	channelCooldowns.Lock()
	defer channelCooldowns.Unlock()
	entry := channelCooldowns.entries[channelID]
	return entry != nil && now.Before(entry.until)
}

// ChannelCooldownStatuses lists the cooldown records for the channel list. It
// drops the records of channels that no longer exist or are no longer enabled:
// a disabled channel is not selected, so its cooldown is moot.
func ChannelCooldownStatuses() []ChannelCooldownStatus {
	// The records only exist while the in-memory cache does; the payload's
	// memory_cache_enabled flag tells the frontend why the list is empty.
	if !common.MemoryCacheEnabled {
		return []ChannelCooldownStatus{}
	}
	now := cooldownNow()
	channelCooldowns.Lock()
	defer channelCooldowns.Unlock()
	statuses := make([]ChannelCooldownStatus, 0, len(channelCooldowns.entries))
	for id, entry := range channelCooldowns.entries {
		channel, err := model.CacheGetChannel(id)
		if err != nil || channel.Status != common.ChannelStatusEnabled {
			delete(channelCooldowns.entries, id)
			continue
		}
		status := ChannelCooldownStatus{
			ChannelID:      id,
			State:          "recovering",
			CooldownCount:  entry.consecutive,
			WeightFactor:   entry.weightFactor,
			LastStatusCode: entry.lastStatus,
			LastErrorCode:  entry.lastErrorCode,
		}
		if now.Before(entry.until) {
			status.State, status.CooldownUntil = "cooling", entry.until.Unix()
		}
		statuses = append(statuses, status)
	}
	slices.SortFunc(statuses, func(a, b ChannelCooldownStatus) int {
		return cmp.Compare(a.ChannelID, b.ChannelID)
	})
	return statuses
}

// channelCooldownSnapshot copies the cooldown records of channels for one
// selection.
func channelCooldownSnapshot(channels []*model.Channel) map[int]channelCooldown {
	channelCooldowns.Lock()
	defer channelCooldowns.Unlock()
	snapshot := make(map[int]channelCooldown)
	for _, channel := range channels {
		if entry := channelCooldowns.entries[channel.Id]; entry != nil {
			snapshot[channel.Id] = *entry
		}
	}
	return snapshot
}
