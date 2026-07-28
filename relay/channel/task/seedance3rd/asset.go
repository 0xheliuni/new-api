package seedance3rd

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"

	"github.com/pkg/errors"
)

const (
	assetPollInterval = 2 * time.Second
	assetPollTimeout  = 60 * time.Second
)

// assetClient:上传单个媒体 URL → 轮询到就绪 → 返回 assetId。
type assetClient interface {
	CreateAndWait(ctx context.Context, mediaURL, assetType string) (string, error)
}

// ---- sdAssetClient: /v1/sd/assets(hc 变体) ----

type sdAssetClient struct {
	baseURL      string
	apiKey       string
	httpClient   *http.Client
	pollInterval time.Duration
	pollTimeout  time.Duration
}

func (cl *sdAssetClient) interval() time.Duration {
	if cl.pollInterval > 0 {
		return cl.pollInterval
	}
	return assetPollInterval
}

func (cl *sdAssetClient) timeout() time.Duration {
	if cl.pollTimeout > 0 {
		return cl.pollTimeout
	}
	return assetPollTimeout
}

type sdCreateResp struct {
	Data struct {
		Id     string `json:"Id"`
		Status string `json:"Status"`
	} `json:"data"`
}

func (cl *sdAssetClient) CreateAndWait(ctx context.Context, mediaURL, assetType string) (string, error) {
	body, _ := common.Marshal(map[string]string{"URL": mediaURL, "Name": "newapi", "AssetType": assetType})
	var cr sdCreateResp
	if err := cl.do(ctx, http.MethodPost, "/v1/sd/assets", body, &cr); err != nil {
		return "", errors.Wrap(err, "sd create asset failed")
	}
	if cr.Data.Id == "" {
		return "", errors.New("sd create asset returned empty id")
	}
	deadline := time.Now().Add(cl.timeout())
	for {
		var gr sdCreateResp
		if err := cl.do(ctx, http.MethodGet, "/v1/sd/assets/"+cr.Data.Id, nil, &gr); err != nil {
			return "", errors.Wrap(err, "sd get asset failed")
		}
		switch gr.Data.Status {
		case "Active":
			return cr.Data.Id, nil
		case "Failed":
			return "", errors.Errorf("sd asset %s failed", cr.Data.Id)
		}
		if time.Now().After(deadline) {
			return "", errors.Errorf("sd asset %s not active within %s (last: %s)", cr.Data.Id, cl.timeout(), gr.Data.Status)
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(cl.interval()):
		}
	}
}

// do 发起一次 Bearer 鉴权的 JSON 请求并解析到 out。
func (cl *sdAssetClient) do(ctx context.Context, method, path string, body []byte, out any) error {
	return doJSON(ctx, cl.httpClient, method, cl.baseURL+path, cl.apiKey, body, out)
}

// doJSON 是包内共用的 Bearer JSON 请求辅助。
func doJSON(ctx context.Context, client *http.Client, method, url, apiKey string, body []byte, out any) error {
	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, r)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return errors.Errorf("upstream status %d: %s", resp.StatusCode, truncate(respBody, 512))
	}
	if out != nil {
		if err := common.Unmarshal(respBody, out); err != nil {
			return errors.Wrapf(err, "unmarshal failed (body: %s)", truncate(respBody, 512))
		}
	}
	return nil
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "..."
}

// pickMedia 返回 content item 的媒体引用与 AssetType(本包独立实现,保隔离)。
func pickMedia(item *ContentItem) (*MediaURL, string) {
	switch {
	case item.ImageURL != nil:
		return item.ImageURL, "Image"
	case item.VideoURL != nil:
		return item.VideoURL, "Video"
	case item.AudioURL != nil:
		return item.AudioURL, "Audio"
	default:
		return nil, ""
	}
}

var _ = fmt.Sprintf // 占位:后续 group client 使用 fmt
var _ = strings.HasSuffix
