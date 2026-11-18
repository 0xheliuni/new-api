package service

import (
	"cmp"
	"math/rand/v2"
	"slices"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
)

// schedulerRandFloat draws the weighted channel pick; tests replace it.
var schedulerRandFloat = rand.Float64

// selectScheduledChannel picks the channel of group for one attempt. Without
// the in-memory cache, or with cooldowns off and one attempt per priority
// level, it keeps the priority-per-retry pick of GetRandomSatisfiedChannel.
func selectScheduledChannel(param *RetryParam, group string, priorityRetry int, filters []dto.ChannelFilter) (*model.Channel, error) {
	policy := &model.CurrentRequestPolicy().Scheduling
	if !common.MemoryCacheEnabled || (!policy.CooldownEnabled && policy.TierAttemptLimit <= 1) {
		return model.GetRandomSatisfiedChannel(group, param.ModelName, priorityRetry, filters)
	}
	candidates, err := model.ListSatisfiedChannels(group, param.ModelName, filters)
	if err != nil || len(candidates) == 0 {
		return nil, err
	}
	tried := make(map[int]bool)
	if param.Ctx != nil {
		for _, value := range param.Ctx.GetStringSlice("use_channel") {
			if id, convErr := strconv.Atoi(value); convErr == nil {
				tried[id] = true
			}
		}
	}
	var health map[int]channelCooldown
	if policy.CooldownEnabled {
		health = channelCooldownSnapshot(candidates)
	}
	channel, reason := pickScheduledChannel(candidates, tried, health, cooldownNow(), policy.TierAttemptLimit, schedulerRandFloat)
	if reason != "" && param.Ctx != nil {
		RequestPolicy(param.Ctx).QueueEvent(PolicyEvent{ChannelID: channel.Id, Decision: PolicyDecision{Action: "route", Reason: reason, Source: "routing"}})
	}
	return channel, nil
}

// pickScheduledChannel applies the tiered rounds of the scheduling policy to
// the candidates of one group. tried holds the channels this request already
// attempted and health the cooldown records of the candidates. The reason is
// empty for an ordinary pick and names the fallback otherwise.
func pickScheduledChannel(candidates []*model.Channel, tried map[int]bool, health map[int]channelCooldown, now time.Time, tierLimit int, randFloat func() float64) (*model.Channel, string) {
	sorted := slices.Clone(candidates)
	slices.SortStableFunc(sorted, func(a, b *model.Channel) int {
		return cmp.Compare(b.GetPriority(), a.GetPriority())
	})
	var tiers [][]*model.Channel
	for index, channel := range sorted {
		if index == 0 || channel.GetPriority() != sorted[index-1].GetPriority() {
			tiers = append(tiers, nil)
		}
		tiers[len(tiers)-1] = append(tiers[len(tiers)-1], channel)
	}
	coolingUntil := func(channel *model.Channel) (time.Time, bool) {
		entry, ok := health[channel.Id]
		return entry.until, ok && now.Before(entry.until)
	}

	// Rounds 1 and 2 take untried channels that are not cooling down, highest
	// priority first. Round 1 also skips a priority level other than the
	// lowest once this request tried tierLimit of its channels.
	for _, honorLimit := range []bool{true, false} {
		limitSkipped := false
		for index, tier := range tiers {
			if honorLimit && index < len(tiers)-1 {
				attempted := 0
				for _, channel := range tier {
					if tried[channel.Id] {
						attempted++
					}
				}
				if attempted >= tierLimit {
					limitSkipped = true
					continue
				}
			}
			pool := slices.DeleteFunc(slices.Clone(tier), func(channel *model.Channel) bool {
				_, cooling := coolingUntil(channel)
				return tried[channel.Id] || cooling
			})
			if len(pool) == 0 {
				continue
			}
			reason := ""
			if limitSkipped {
				reason = "priority_limit_reached"
			}
			return pickWeightedChannel(pool, health, randFloat), reason
		}
	}

	// Round 3 takes the untried channel whose cooldown ends first.
	var earliest *model.Channel
	var earliestUntil time.Time
	for _, channel := range sorted {
		until, cooling := coolingUntil(channel)
		if !tried[channel.Id] && cooling && (earliest == nil || until.Before(earliestUntil)) {
			earliest, earliestUntil = channel, until
		}
	}
	if earliest != nil {
		return earliest, "cooldown_fallback"
	}

	// Round 4 reuses a tried channel: the highest priority level with a
	// channel that is not cooling down, else the cooldown that ends first.
	for _, tier := range tiers {
		pool := slices.DeleteFunc(slices.Clone(tier), func(channel *model.Channel) bool {
			_, cooling := coolingUntil(channel)
			return cooling
		})
		if len(pool) > 0 {
			return pickWeightedChannel(pool, health, randFloat), "tried_channel_reused"
		}
	}
	for _, channel := range sorted {
		until, _ := coolingUntil(channel)
		if earliest == nil || until.Before(earliestUntil) {
			earliest, earliestUntil = channel, until
		}
	}
	return earliest, "tried_channel_reused"
}

// pickWeightedChannel draws a channel by its configured weight times its
// cooldown ramp share. As in GetRandomSatisfiedChannel, a channel of weight 0
// only runs when every weight in the pool is 0.
func pickWeightedChannel(pool []*model.Channel, health map[int]channelCooldown, randFloat func() float64) *model.Channel {
	weights := make([]float64, len(pool))
	total := 0.0
	for index, channel := range pool {
		weight := float64(channel.GetWeight())
		if entry, ok := health[channel.Id]; ok {
			weight *= entry.weightFactor
		}
		weights[index] = weight
		total += weight
	}
	if total <= 0 {
		return pool[min(int(randFloat()*float64(len(pool))), len(pool)-1)]
	}
	target := randFloat() * total
	for index, weight := range weights {
		if target < weight {
			return pool[index]
		}
		target -= weight
	}
	// Rounding can leave the target at the very end of the range.
	for index := len(pool) - 1; index >= 0; index-- {
		if weights[index] > 0 {
			return pool[index]
		}
	}
	return pool[len(pool)-1]
}
