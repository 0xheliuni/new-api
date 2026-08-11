package doubao

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"

	"github.com/pkg/errors"
)

func newCloudwiseTestClient(endpoint string) *cloudwiseAssetClient {
	return &cloudwiseAssetClient{
		baseURL:        endpoint,
		apiKey:         "sk-test",
		skipModeration: true,
		httpClient:     http.DefaultClient,
		pollInterval:   5 * time.Millisecond,
		pollTimeout:    2 * time.Second,
	}
}

// TestCloudwise_CreateAndWait_RoundTrip verifies a full upload→poll→return cycle.
// Confirms Authorization header carries the channel API key (not AK/SK).
// Uses channel id 9001 / URL https://example.com/cw-rt.jpg to avoid cache collisions
// with existing tests.
func TestCloudwise_CreateAndWait_RoundTrip(t *testing.T) {
	var gotAuth string
	var createBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/assets/create":
			raw, readErr := io.ReadAll(r.Body)
			if readErr != nil {
				t.Errorf("read create-asset body: %v", readErr)
			}
			if err := common.Unmarshal(raw, &createBody); err != nil {
				t.Errorf("decode create-asset body %q: %v", raw, err)
			}
			// Document: POST /api/v1/assets/create → {ResponseMetadata, Result:{Id}}
			w.Write([]byte(`{"ResponseMetadata":{"RequestId":"r1"},"Result":{"Id":"cw-asset-1"}}`))
		case "/api/v1/assets/get":
			// Document: GET /api/v1/assets/get → {ResponseMetadata, Result:{Id,Status}}
			// Ready status from document is "Active".
			w.Write([]byte(`{"ResponseMetadata":{"RequestId":"r2"},"Result":{"Id":"cw-asset-1","Status":"Active"}}`))
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
		}
	}))
	defer srv.Close()

	cl := newCloudwiseTestClient(srv.URL)
	id, err := cl.CreateAndWait(context.Background(), "group-1", "https://example.com/cw-rt.jpg", "Image")
	if err != nil {
		t.Fatalf("CreateAndWait: %v", err)
	}
	if id != "cw-asset-1" {
		t.Errorf("id = %q, want cw-asset-1", id)
	}
	if gotAuth != "Bearer sk-test" {
		t.Errorf("Authorization = %q, want Bearer sk-test", gotAuth)
	}

	// Pin the exact wire field names. The document mixes camelCase data fields with
	// a nested PascalCase Moderation object; a "consistency" rename to snake_case
	// would break every production upload while leaving the rest of this suite green.
	for _, want := range []struct{ key, value string }{
		{"assetType", "Image"},
		{"groupId", "group-1"},
		{"url", "https://example.com/cw-rt.jpg"},
	} {
		if got := createBody[want.key]; got != want.value {
			t.Errorf("create-asset body[%q] = %v, want %q (body: %v)", want.key, got, want.value, createBody)
		}
	}
	mod, ok := createBody["Moderation"].(map[string]any)
	if !ok {
		t.Fatalf(`create-asset body missing PascalCase "Moderation" object (body: %v)`, createBody)
	}
	if mod["Strategy"] != "Skip" {
		t.Errorf(`Moderation.Strategy = %v, want "Skip"`, mod["Strategy"])
	}
}

// TestCloudwise_CreateAsset_ModerationOmittedWhenDisabled verifies that Moderation
// is absent from the request when skipModeration is false, rather than sent as an
// empty object the upstream would have to interpret.
func TestCloudwise_CreateAsset_ModerationOmittedWhenDisabled(t *testing.T) {
	var createBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/assets/create":
			raw, _ := io.ReadAll(r.Body)
			if err := common.Unmarshal(raw, &createBody); err != nil {
				t.Errorf("decode create-asset body %q: %v", raw, err)
			}
			w.Write([]byte(`{"ResponseMetadata":{"RequestId":"r1"},"Result":{"Id":"cw-asset-2"}}`))
		case "/api/v1/assets/get":
			w.Write([]byte(`{"ResponseMetadata":{"RequestId":"r2"},"Result":{"Id":"cw-asset-2","Status":"Active"}}`))
		}
	}))
	defer srv.Close()

	cl := newCloudwiseTestClient(srv.URL)
	cl.skipModeration = false
	if _, err := cl.CreateAndWait(context.Background(), "group-1", "https://example.com/cw-nomod.jpg", "Image"); err != nil {
		t.Fatalf("CreateAndWait: %v", err)
	}
	if _, present := createBody["Moderation"]; present {
		t.Errorf("Moderation must be omitted when skipModeration is false (body: %v)", createBody)
	}
}

// TestCloudwise_CreateAsset_ErrorCode verifies that a structured upstream error
// propagates as an assetAPIError carrying the message, and that
// IsGroupExhausted returns false for a generic quota/rate-limit code.
// This is the regression guard: a bare quota code must NOT trigger group rotation.
// Uses URL https://example.com/cw-err.jpg to avoid cache collisions.
func TestCloudwise_CreateAsset_ErrorCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Simulates a generic quota/rate-limit error that is NOT group exhaustion.
		// Code "QuotaExceeded" must not be treated as group-full (regression guard).
		w.Write([]byte(`{"ResponseMetadata":{"RequestId":"r1","Error":{"Code":"QuotaExceeded","Message":"API rate limit exceeded, please retry later"}},"Result":{}}`))
	}))
	defer srv.Close()

	cl := newCloudwiseTestClient(srv.URL)
	_, err := cl.CreateAndWait(context.Background(), "group-1", "https://example.com/cw-err.jpg", "Image")
	if err == nil {
		t.Fatal("expected error for upstream ErrorCode response")
	}
	if !strings.Contains(err.Error(), "QuotaExceeded") {
		t.Errorf("error should carry upstream code, got %v", err)
	}
	// Key regression guard: a generic quota/rate-limit code must NOT be treated as
	// group exhaustion — that would trigger runaway group creation under throttling.
	if cl.IsGroupExhausted(err) {
		t.Error("QuotaExceeded must not be classified as group-exhausted (regression guard)")
	}
}

// TestCloudwise_CreateGroup verifies that CreateGroup hits the correct path and
// returns the group id from the document-defined envelope.
// Uses channel id 9001 to avoid cache collisions.
func TestCloudwise_CreateGroup(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/assets/groups/create" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		// Document: {ResponseMetadata, Result:{Id}} — PascalCase envelope.
		w.Write([]byte(`{"ResponseMetadata":{"RequestId":"r1"},"Result":{"Id":"cw-group-9"}}`))
	}))
	defer srv.Close()

	cl := newCloudwiseTestClient(srv.URL)
	id, err := cl.CreateGroup(context.Background(), "newapi-ch1")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	if id != "cw-group-9" {
		t.Errorf("group id = %q, want cw-group-9", id)
	}
}

// TestCloudwise_IsGroupExhausted_GroupFullCode verifies that a code whose name
// contains an unambiguous group-capacity indicator IS classified as exhausted.
func TestCloudwise_IsGroupExhausted_GroupFullCode(t *testing.T) {
	cl := &cloudwiseAssetClient{}
	err := &assetAPIError{Code: "GroupFull", Message: "the group is full"}
	if !cl.IsGroupExhausted(err) {
		t.Error("GroupFull should be classified as group-exhausted")
	}
}

// TestCloudwise_IsGroupExhausted_FallbackKeyword verifies the message-text fallback:
// requires BOTH a group indicator AND a capacity word.
func TestCloudwise_IsGroupExhausted_FallbackKeyword(t *testing.T) {
	cl := &cloudwiseAssetClient{}
	groupFullMsg := &assetAPIError{Code: "UnknownCode", Message: "the asset group has exceeded its capacity limit"}
	if !cl.IsGroupExhausted(groupFullMsg) {
		t.Error("message containing 'group' and 'capacity' should be classified as group-exhausted")
	}
	// Discriminating negative: carries an exceed-word, so it only stays false while
	// the group conjunction is intact.
	authErr := &assetAPIError{Code: "AuthFailure", Message: "quota exceeded"}
	if cl.IsGroupExhausted(authErr) {
		t.Error("auth failure without 'group' indicator must not be classified as group-exhausted")
	}
	// A throttle message is group-scoped AND contains "exceeded". Matching bare
	// exceed-words here would mint one junk asset group per concurrent throttled
	// request, each with a channel-row write.
	throttled := &assetAPIError{Code: "Throttling", Message: "asset group request rate exceeded, retry later"}
	if cl.IsGroupExhausted(throttled) {
		t.Error("group-scoped throttling must not be classified as group-exhausted (runaway-creation guard)")
	}
	// A missing group id is an operator misconfiguration; rotating would hide it.
	notFound := &assetAPIError{Code: "UnknownCode", Message: "asset group not found"}
	if cl.IsGroupExhausted(notFound) {
		t.Error("'group not found' text must not be classified as group-exhausted")
	}
}

// cloudwisePollServer serves one create-asset response and then replies to every
// poll with the given status body, counting the polls.
func cloudwisePollServer(t *testing.T, assetID, statusBody string, polls *int32) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/assets/create":
			w.Write([]byte(`{"ResponseMetadata":{"RequestId":"r1"},"Result":{"Id":"` + assetID + `"}}`))
		case "/api/v1/assets/get":
			atomic.AddInt32(polls, 1)
			w.Write([]byte(statusBody))
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
		}
	}))
}

// TestCloudwise_CreateAndWait_Failed covers the terminal Failed branch: polling
// stops immediately and the masked response body is reported for triage
// (moderation rejections arrive this way).
func TestCloudwise_CreateAndWait_Failed(t *testing.T) {
	var polls int32
	srv := cloudwisePollServer(t, "cw-asset-failed",
		`{"ResponseMetadata":{"RequestId":"r2"},"Result":{"Id":"cw-asset-failed","Status":"Failed"}}`, &polls)
	defer srv.Close()

	cl := newCloudwiseTestClient(srv.URL)
	_, err := cl.CreateAndWait(context.Background(), "group-1", "https://example.com/cw-failed.jpg", "Image")
	if err == nil {
		t.Fatal("expected error for Failed asset status")
	}
	if !strings.Contains(err.Error(), "cw-asset-failed") || !strings.Contains(err.Error(), "processing failed") {
		t.Errorf("error should name the asset and the failure, got %v", err)
	}
	if n := atomic.LoadInt32(&polls); n != 1 {
		t.Errorf("Failed is terminal, want exactly 1 poll, got %d", n)
	}
}

// TestCloudwise_CreateAndWait_Timeout covers the deadline branch: Processing never
// resolves, so the call gives up and reports the last status.
func TestCloudwise_CreateAndWait_Timeout(t *testing.T) {
	var polls int32
	srv := cloudwisePollServer(t, "cw-asset-slow",
		`{"ResponseMetadata":{"RequestId":"r2"},"Result":{"Id":"cw-asset-slow","Status":"Processing"}}`, &polls)
	defer srv.Close()

	cl := newCloudwiseTestClient(srv.URL)
	cl.pollInterval = time.Millisecond
	cl.pollTimeout = 20 * time.Millisecond

	_, err := cl.CreateAndWait(context.Background(), "group-1", "https://example.com/cw-slow.jpg", "Image")
	if err == nil {
		t.Fatal("expected timeout error when status never leaves Processing")
	}
	if !strings.Contains(err.Error(), "not ready within") || !strings.Contains(err.Error(), "Processing") {
		t.Errorf("error should report the timeout and last status, got %v", err)
	}
	if n := atomic.LoadInt32(&polls); n < 1 {
		t.Error("expected at least one poll before the deadline")
	}
}

// TestCloudwise_CreateAndWait_ContextCanceled covers the ctx.Done() branch: a
// cancelled client request aborts polling instead of running to the poll deadline.
func TestCloudwise_CreateAndWait_ContextCanceled(t *testing.T) {
	polled := make(chan struct{}, 8)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/assets/create":
			w.Write([]byte(`{"ResponseMetadata":{"RequestId":"r1"},"Result":{"Id":"cw-asset-cancel"}}`))
		case "/api/v1/assets/get":
			w.Write([]byte(`{"ResponseMetadata":{"RequestId":"r2"},"Result":{"Id":"cw-asset-cancel","Status":"Processing"}}`))
			select {
			case polled <- struct{}{}:
			default:
			}
		}
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		<-polled
		cancel()
	}()

	cl := newCloudwiseTestClient(srv.URL)
	// Interval far longer than the poll round-trip, so the call is parked in the
	// select when the cancel lands and cannot pass the deadline branch instead.
	cl.pollInterval = 30 * time.Second
	cl.pollTimeout = 5 * time.Minute

	_, err := cl.CreateAndWait(ctx, "group-1", "https://example.com/cw-cancel.jpg", "Image")
	if err == nil {
		t.Fatal("expected error when the request context is cancelled")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error should unwrap to context.Canceled, got %v", err)
	}
}

// TestCloudwise_HTTPStatusError_Classifiable guards the non-2xx path: the asset
// error format is undocumented, so a plain HTTP error must still arrive as an
// *assetAPIError. Otherwise IsGroupExhausted stops at its errors.As check and
// group rotation is silently dead on this provider.
func TestCloudwise_HTTPStatusError_Classifiable(t *testing.T) {
	cases := []struct {
		name          string
		status        int
		body          string
		wantCode      string
		wantExhausted bool
	}{
		{
			// The lowercase shape this gateway uses for its task endpoints.
			name:          "lowercase code/message, group full",
			status:        http.StatusBadRequest,
			body:          `{"code":"1026","message":"asset group is full"}`,
			wantCode:      "1026",
			wantExhausted: true,
		},
		{
			name:          "lowercase nested error object",
			status:        http.StatusBadRequest,
			body:          `{"error":{"code":1026,"message":"asset group capacity reached"}}`,
			wantCode:      "1026",
			wantExhausted: true,
		},
		{
			// Neither shape parses: fall back to the status so the operator still
			// sees something classifiable, and do not rotate.
			name:          "unparseable body falls back to status",
			status:        http.StatusBadGateway,
			body:          `<html>502 Bad Gateway</html>`,
			wantCode:      "HTTP502",
			wantExhausted: false,
		},
		{
			name:          "lowercase throttling is not exhaustion",
			status:        http.StatusTooManyRequests,
			body:          `{"code":"Throttling","message":"asset group request rate exceeded"}`,
			wantCode:      "Throttling",
			wantExhausted: false,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				w.Write([]byte(tc.body))
			}))
			defer srv.Close()

			cl := newCloudwiseTestClient(srv.URL)
			_, err := cl.CreateAndWait(context.Background(), "group-1", "https://example.com/cw-http.jpg", "Image")
			if err == nil {
				t.Fatal("expected error for non-2xx response")
			}
			var apiErr *assetAPIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("non-2xx must produce *assetAPIError so it can be classified, got %T: %v", err, err)
			}
			if apiErr.Code != tc.wantCode {
				t.Errorf("Code = %q, want %q", apiErr.Code, tc.wantCode)
			}
			if got := cl.IsGroupExhausted(err); got != tc.wantExhausted {
				t.Errorf("IsGroupExhausted = %v, want %v (code %q, message %q)",
					got, tc.wantExhausted, apiErr.Code, apiErr.Message)
			}
		})
	}
}
