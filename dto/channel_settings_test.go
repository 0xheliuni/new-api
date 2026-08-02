package dto

import (
	"strings"
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

func TestChannelSettingsCostRatioRoundTrip(t *testing.T) {
	s := ChannelSettings{CostRatio: 2.5}
	data, err := common.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	var out ChannelSettings
	if err := common.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if out.CostRatio != 2.5 {
		t.Fatalf("cost_ratio = %v, want 2.5", out.CostRatio)
	}
	// zero value must be omitted (0 = "not set")
	data, _ = common.Marshal(ChannelSettings{})
	if strings.Contains(string(data), "cost_ratio") {
		t.Fatalf("zero cost_ratio must be omitted, got %s", data)
	}
}
