package doubao

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func newCloudwiseTestClient(endpoint string) *cloudwiseAssetClient {
	return &cloudwiseAssetClient{
		baseURL:      endpoint,
		apiKey:       "sk-test",
		httpClient:   http.DefaultClient,
		pollInterval: 5 * time.Millisecond,
		pollTimeout:  2 * time.Second,
	}
}

// TestCloudwise_CreateAndWait_RoundTrip verifies a full upload→poll→return cycle.
// Confirms Authorization header carries the channel API key (not AK/SK).
// Uses channel id 9001 / URL https://example.com/cw-rt.jpg to avoid cache collisions
// with existing tests.
func TestCloudwise_CreateAndWait_RoundTrip(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/assets/create":
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
	// Auth error mentioning an unrelated word must not trip the fallback.
	authErr := &assetAPIError{Code: "AuthFailure", Message: "authorization failed: quota not available"}
	if cl.IsGroupExhausted(authErr) {
		t.Error("auth failure without 'group' indicator must not be classified as group-exhausted")
	}
}
