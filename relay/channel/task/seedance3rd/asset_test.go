package seedance3rd

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSDAssetClient_CreateAndWait(t *testing.T) {
	var createHits, getHits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/sd/assets":
			createHits++
			w.Write([]byte(`{"success":true,"data":{"Id":"asset-abc","base_resp":{"status_code":0}}}`))
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/sd/assets/"):
			getHits++
			// 第一次 Processing,第二次 Active,验证轮询逻辑
			if getHits == 1 {
				w.Write([]byte(`{"success":true,"data":{"Id":"asset-abc","Status":"Processing"}}`))
			} else {
				w.Write([]byte(`{"success":true,"data":{"Id":"asset-abc","Status":"Active"}}`))
			}
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()

	cl := &sdAssetClient{
		baseURL:      srv.URL,
		apiKey:       "sk-x",
		httpClient:   srv.Client(),
		pollInterval: time.Millisecond,
		pollTimeout:  time.Second,
	}
	id, err := cl.CreateAndWait(context.Background(), "https://x/a.png", "Image")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if id != "asset-abc" {
		t.Fatalf("id = %q, want asset-abc", id)
	}
	if createHits != 1 || getHits != 2 {
		t.Fatalf("createHits=%d getHits=%d, want 1/2", createHits, getHits)
	}
}
