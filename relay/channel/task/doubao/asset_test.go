package doubao

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/dto"

	"github.com/gin-gonic/gin"
)

// newAdaptorWithServer returns a TaskAdaptor configured to talk to the test
// server, with BytePlus asset upload enabled. The asset client endpoint is
// injected via a hook so we can point it at httptest.
func enabledSettings() dto.ChannelOtherSettings {
	return dto.ChannelOtherSettings{
		BytePlusAssetEnabled: true,
		BytePlusAccessKey:    "ak",
		BytePlusSecretKey:    "sk",
		BytePlusAssetGroupId: "group-test",
		BytePlusProjectName:  "default",
		BytePlusRegion:       "ap-southeast-1",
	}
}

// ginCtx returns a minimal *gin.Context wrapping a background request.
func ginCtx() *gin.Context {
	req, _ := http.NewRequest(http.MethodPost, "/v1/video/generations", nil)
	c := &gin.Context{Request: req}
	return c
}

func TestPreuploadAssets_Disabled_Passthrough(t *testing.T) {
	a := &TaskAdaptor{} // otherSettings zero => disabled
	payload := &requestPayload{Content: []ContentItem{
		{Type: "image_url", ImageURL: &MediaURL{URL: "https://example.com/i.jpg"}},
	}}
	if err := a.preuploadAssets(ginCtx(), payload); err != nil {
		t.Fatalf("preuploadAssets: %v", err)
	}
	if got := payload.Content[0].ImageURL.URL; got != "https://example.com/i.jpg" {
		t.Errorf("url mutated while disabled: %q", got)
	}
}

func TestPreuploadAssets_Base64Rejected(t *testing.T) {
	a := &TaskAdaptor{otherSettings: enabledSettings(), endpointOverride: "http://127.0.0.1:0"}
	payload := &requestPayload{Content: []ContentItem{
		{Type: "image_url", ImageURL: &MediaURL{URL: "data:image/png;base64,AAAA"}},
	}}
	err := a.preuploadAssets(ginCtx(), payload)
	if err == nil || !strings.Contains(err.Error(), "public http(s) URL") {
		t.Fatalf("expected base64 rejection, got %v", err)
	}
}

func TestPreuploadAssets_ReplacesAndIsIdempotent(t *testing.T) {
	var createCalls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("Action") {
		case "CreateAsset":
			n := atomic.AddInt32(&createCalls, 1)
			w.Write([]byte(`{"Result":{"Id":"asset-` + string(rune('0'+n)) + `"}}`))
		case "GetAsset":
			w.Write([]byte(`{"Result":{"Status":"Active"}}`))
		}
	}))
	defer srv.Close()

	a := &TaskAdaptor{otherSettings: enabledSettings(), endpointOverride: srv.URL, channelId: 4242}
	payload := &requestPayload{Content: []ContentItem{
		{Type: "image_url", ImageURL: &MediaURL{URL: "https://example.com/uniq-1.jpg"}},
		{Type: "video_url", VideoURL: &MediaURL{URL: "asset://asset-preexisting"}}, // idempotent skip
		{Type: "text", Text: "hello"},
	}}
	if err := a.preuploadAssets(ginCtx(), payload); err != nil {
		t.Fatalf("preuploadAssets: %v", err)
	}
	if got := payload.Content[0].ImageURL.URL; !strings.HasPrefix(got, "asset://asset-") {
		t.Errorf("image url not replaced: %q", got)
	}
	if got := payload.Content[1].VideoURL.URL; got != "asset://asset-preexisting" {
		t.Errorf("preexisting asset:// url should be untouched, got %q", got)
	}
	if got := payload.Content[2].Text; got != "hello" {
		t.Errorf("text item mutated: %q", got)
	}

	// Second call with same URL must hit the cache (no new CreateAsset).
	before := atomic.LoadInt32(&createCalls)
	payload2 := &requestPayload{Content: []ContentItem{
		{Type: "image_url", ImageURL: &MediaURL{URL: "https://example.com/uniq-1.jpg"}},
	}}
	if err := a.preuploadAssets(ginCtx(), payload2); err != nil {
		t.Fatalf("preuploadAssets (2): %v", err)
	}
	if atomic.LoadInt32(&createCalls) != before {
		t.Errorf("expected cache hit (no new CreateAsset), calls went %d -> %d", before, atomic.LoadInt32(&createCalls))
	}
}

func TestPreuploadAssets_MissingCreds(t *testing.T) {
	a := &TaskAdaptor{otherSettings: dto.ChannelOtherSettings{BytePlusAssetEnabled: true}}
	payload := &requestPayload{Content: []ContentItem{
		{Type: "image_url", ImageURL: &MediaURL{URL: "https://example.com/i.jpg"}},
	}}
	if err := a.preuploadAssets(ginCtx(), payload); err == nil {
		t.Fatal("expected error for missing credentials")
	}
}

// TestPreuploadAssets_GroupExhausted_RetrySucceeds verifies that when a group
// is full, exactly one new group is created and the upload retries successfully.
func TestPreuploadAssets_GroupExhausted_RetrySucceeds(t *testing.T) {
	const chanID = 8001
	var createGroupCalls int32
	var createAssetAttempts int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("Action") {
		case "CreateAsset":
			n := atomic.AddInt32(&createAssetAttempts, 1)
			if n == 1 {
				// First attempt: group full.
				w.Write([]byte(`{"ResponseMetadata":{"Error":{"Code":"GroupFull","Message":"group is full"}}}`))
				return
			}
			// Retry into new group: succeed.
			w.Write([]byte(`{"Result":{"Id":"asset-new-group"}}`))
		case "GetAsset":
			w.Write([]byte(`{"Result":{"Status":"Active"}}`))
		case "CreateAssetGroup":
			atomic.AddInt32(&createGroupCalls, 1)
			w.Write([]byte(`{"Result":{"Id":"group-new-1"}}`))
		}
	}))
	defer srv.Close()

	settings := enabledSettings()
	settings.BytePlusAssetGroupId = "group-exhausted"
	a := &TaskAdaptor{
		otherSettings:    settings,
		endpointOverride: srv.URL,
		channelId:        chanID,
	}
	payload := &requestPayload{Content: []ContentItem{
		{Type: "image_url", ImageURL: &MediaURL{URL: "https://example.com/rotate-test-ch8001.jpg"}},
	}}

	if err := a.preuploadAssets(ginCtx(), payload); err != nil {
		t.Fatalf("preuploadAssets: %v", err)
	}
	if got := payload.Content[0].ImageURL.URL; got != "asset://asset-new-group" {
		t.Errorf("expected asset://asset-new-group, got %q", got)
	}
	if n := atomic.LoadInt32(&createGroupCalls); n != 1 {
		t.Errorf("expected exactly 1 CreateAssetGroup call, got %d", n)
	}
	if n := atomic.LoadInt32(&createAssetAttempts); n != 2 {
		t.Errorf("expected 2 CreateAsset attempts (initial + retry), got %d", n)
	}
	// New group id must be reflected in adaptor state.
	if a.otherSettings.BytePlusAssetGroupId != "group-new-1" {
		t.Errorf("expected group id updated to group-new-1, got %q", a.otherSettings.BytePlusAssetGroupId)
	}
}

// TestPreuploadAssets_GroupExhausted_DoubleExhaustion verifies that when the
// retry into the fresh group also fails with exhaustion, the error surfaces and
// still exactly one group was created (no second creation attempt).
func TestPreuploadAssets_GroupExhausted_DoubleExhaustion(t *testing.T) {
	const chanID = 8002
	var createGroupCalls int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("Action") {
		case "CreateAsset":
			// Always fail with GroupFull — both initial and retry attempts.
			w.Write([]byte(`{"ResponseMetadata":{"Error":{"Code":"GroupFull","Message":"group is full"}}}`))
		case "CreateAssetGroup":
			atomic.AddInt32(&createGroupCalls, 1)
			w.Write([]byte(`{"Result":{"Id":"group-double-1"}}`))
		}
	}))
	defer srv.Close()

	settings := enabledSettings()
	settings.BytePlusAssetGroupId = "group-will-exhaust"
	a := &TaskAdaptor{
		otherSettings:    settings,
		endpointOverride: srv.URL,
		channelId:        chanID,
	}
	payload := &requestPayload{Content: []ContentItem{
		{Type: "image_url", ImageURL: &MediaURL{URL: "https://example.com/double-exhaust-ch8002.jpg"}},
	}}

	err := a.preuploadAssets(ginCtx(), payload)
	if err == nil {
		t.Fatal("expected error for double exhaustion, got nil")
	}
	if n := atomic.LoadInt32(&createGroupCalls); n != 1 {
		t.Errorf("expected exactly 1 CreateAssetGroup call (no runaway), got %d", n)
	}
}

// TestPreuploadAssets_NonGroupErrors_NoGroupCreated verifies that errors
// unrelated to group exhaustion (auth, moderation, bad URL) propagate and zero
// groups are created.
func TestPreuploadAssets_NonGroupErrors_NoGroupCreated(t *testing.T) {
	cases := []struct {
		name     string
		code     string
		chanID   int
		mediaURL string
	}{
		{"auth failure", "AuthFailure", 8101, "https://example.com/auth-fail-ch8101.jpg"},
		{"moderation rejection", "ContentRiskFailed", 8102, "https://example.com/moderation-ch8102.jpg"},
		{"invalid media URL", "InvalidParameter.URL", 8103, "https://example.com/bad-url-ch8103.jpg"},
		{"unknown error code", "InternalError", 8104, "https://example.com/unknown-err-ch8104.jpg"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			var createGroupCalls int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Query().Get("Action") {
				case "CreateAsset":
					w.Write([]byte(`{"ResponseMetadata":{"Error":{"Code":"` + tc.code + `","Message":"test error"}}}`))
				case "CreateAssetGroup":
					atomic.AddInt32(&createGroupCalls, 1)
					w.Write([]byte(`{"Result":{"Id":"should-never-be-created"}}`))
				}
			}))
			defer srv.Close()

			settings := enabledSettings()
			settings.BytePlusAssetGroupId = "group-non-exhausted"
			a := &TaskAdaptor{
				otherSettings:    settings,
				endpointOverride: srv.URL,
				channelId:        tc.chanID,
			}
			payload := &requestPayload{Content: []ContentItem{
				{Type: "image_url", ImageURL: &MediaURL{URL: tc.mediaURL}},
			}}

			err := a.preuploadAssets(ginCtx(), payload)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if n := atomic.LoadInt32(&createGroupCalls); n != 0 {
				t.Errorf("expected 0 CreateAssetGroup calls for non-group error %q, got %d", tc.code, n)
			}
		})
	}
}
