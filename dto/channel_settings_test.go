package dto

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
)

func TestSeedance3rdAssetEnabledRoundTrip(t *testing.T) {
	s := ChannelOtherSettings{Seedance3rdAssetEnabled: true}
	b, err := common.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back ChannelOtherSettings
	if err := common.Unmarshal(b, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !back.Seedance3rdAssetEnabled {
		t.Fatalf("Seedance3rdAssetEnabled lost in round-trip: %s", string(b))
	}
}
