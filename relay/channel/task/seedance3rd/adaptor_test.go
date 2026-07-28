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

func TestConvertToRequestPayload_TextOnly(t *testing.T) {
	a := &TaskAdaptor{}
	req := &relaycommon.TaskSubmitReq{Model: "dreamina-seedance-2-0-260128", Prompt: "a girl dancing"}
	got, err := a.convertToRequestPayload(req)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got.Model != "dreamina-seedance-2-0-260128" {
		t.Fatalf("model = %q", got.Model)
	}
	if len(got.Content) != 1 || got.Content[0].Type != "text" || got.Content[0].Text != "a girl dancing" {
		t.Fatalf("content = %+v, want single text item", got.Content)
	}
}

func TestConvertToRequestPayload_ImagesAndDuration(t *testing.T) {
	a := &TaskAdaptor{}
	req := &relaycommon.TaskSubmitReq{
		Model:    "dreamina-seedance-2-0-260128",
		Prompt:   "blink",
		Images:   []string{"https://x/a.png"},
		Duration: 5,
	}
	got, err := a.convertToRequestPayload(req)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	// 期望:1 个 image_url + 1 个末尾 text
	if len(got.Content) != 2 {
		t.Fatalf("content len = %d, want 2 (%+v)", len(got.Content), got.Content)
	}
	if got.Content[0].Type != "image_url" || got.Content[0].ImageURL == nil || got.Content[0].ImageURL.URL != "https://x/a.png" {
		t.Fatalf("first item not image_url: %+v", got.Content[0])
	}
	if got.Content[1].Type != "text" {
		t.Fatalf("last item not text: %+v", got.Content[1])
	}
	if got.Duration == nil || int(*got.Duration) != 5 {
		t.Fatalf("duration = %v, want 5", got.Duration)
	}
}
