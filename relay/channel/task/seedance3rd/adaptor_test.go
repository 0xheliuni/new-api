package seedance3rd

import (
	"net/http"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

func TestBuildRequestURL(t *testing.T) {
	a := &TaskAdaptor{}
	a.Init(&relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: "https://model.service-inference.ai", ApiKey: "sk-x"}})
	got, err := a.BuildRequestURL(nil)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if want := "https://model.service-inference.ai/v1/video/generate"; got != want {
		t.Fatalf("url = %q, want %q", got, want)
	}
}

func TestBuildRequestHeader(t *testing.T) {
	a := &TaskAdaptor{}
	a.Init(&relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: "https://x", ApiKey: "sk-abc"}})
	req, _ := http.NewRequest(http.MethodPost, "https://x", nil)
	if err := a.BuildRequestHeader(nil, req, nil); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer sk-abc" {
		t.Fatalf("auth = %q, want %q", got, "Bearer sk-abc")
	}
}
