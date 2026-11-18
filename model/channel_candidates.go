package model

import (
	"fmt"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

// ListSatisfiedChannels returns the enabled channels of group that serve
// modelName and pass filters. It looks up the exact model name first and the
// normalized name second, like GetRandomSatisfiedChannel. It reads the
// in-memory channel cache only, so callers check common.MemoryCacheEnabled.
func ListSatisfiedChannels(group string, modelName string, filters []dto.ChannelFilter) ([]*Channel, error) {
	channelSyncLock.RLock()
	defer channelSyncLock.RUnlock()

	ids, _ := filterCandidateIDs(group2model2channels[group][modelName], modelName, filters)
	if len(ids) == 0 {
		ids, _ = filterCandidateIDs(group2model2channels[group][ratio_setting.RoutingMatchModelName(modelName)], modelName, filters)
	}
	channels := make([]*Channel, 0, len(ids))
	for _, id := range ids {
		channel, ok := channelsIDM[id]
		if !ok {
			return nil, fmt.Errorf("database consistency error: channel #%d does not exist, please contact the administrator", id)
		}
		channels = append(channels, channel)
	}
	return channels, nil
}
