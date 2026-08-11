package doubao

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/pkg/cachex"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	"github.com/samber/hot"
)

// assetClient uploads one media URL to an asset library and waits until it is
// ready.  groupID is a per-call parameter so the factory does not need to know
// which group to use at construction time.
type assetClient interface {
	CreateAndWait(ctx context.Context, groupID, mediaURL, assetType string) (string, error)
}

// assetAPIError is returned by doAction when the upstream API responds with a
// structured error in ResponseMetadata.Error.  Callers can type-assert to
// inspect Code without string-matching the error message.
type assetAPIError struct {
	Code    string
	Message string
	Raw     string // masked copy of the full response body
}

func (e *assetAPIError) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// newAssetClient constructs a BytePlus asset client from the adaptor's channel
// settings.  Returns an error if the access key or secret key is missing.
// Do not add a provider switch here — that belongs to a later task.
func (a *TaskAdaptor) newAssetClient() (assetClient, error) {
	s := a.otherSettings
	if s.BytePlusAccessKey == "" || s.BytePlusSecretKey == "" {
		return nil, errors.New("byteplus asset upload enabled but access_key/secret_key is missing in channel settings")
	}
	region, project, skipMod := s.ResolveBytePlusAsset()
	httpClient, err := service.GetHttpClientWithProxy(a.proxy)
	if err != nil {
		return nil, errors.Wrap(err, "create http client for byteplus asset upload failed")
	}
	return &bytePlusAssetClient{
		ak:             s.BytePlusAccessKey,
		sk:             s.BytePlusSecretKey,
		region:         region,
		projectName:    project,
		skipModeration: skipMod,
		httpClient:     httpClient,
		endpoint:       a.endpointOverride,
	}, nil
}

// assetCacheTTL is the URL→assetId mapping TTL.  Short enough to cover a
// typical batch request, conservative enough to avoid stale mappings after
// an asset expires upstream.
const assetCacheTTL = 6 * time.Hour

// preuploadAssets replaces public media URLs in payload.Content with
// asset://<id> references by uploading them to the BytePlus asset library.
// When the feature flag is disabled it is a no-op.
func (a *TaskAdaptor) preuploadAssets(c *gin.Context, payload *requestPayload) error {
	s := a.otherSettings
	if !s.BytePlusAssetEnabled {
		return nil
	}
	if s.BytePlusAssetGroupId == "" {
		return errors.New("byteplus asset upload enabled but group_id is missing in channel settings")
	}

	cl, err := a.newAssetClient()
	if err != nil {
		return err
	}

	_, project, _ := s.ResolveBytePlusAsset()

	ctx := c.Request.Context()
	for i := range payload.Content {
		item := &payload.Content[i]
		media, assetType := pickMedia(item)
		if media == nil {
			continue // text and other non-media items
		}
		url := strings.TrimSpace(media.URL)
		if url == "" {
			continue
		}
		// Already an asset reference — idempotent skip.
		if strings.HasPrefix(url, "asset://") {
			continue
		}
		// CreateAsset only accepts public HTTP(S) URLs; base64/data URIs are unsupported.
		if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
			return errors.Errorf("byteplus asset upload requires a public http(s) URL, got unsupported input for %s (base64/data URIs are not supported)", item.Type)
		}

		cacheKey := assetCacheKey(a.channelId, s.BytePlusAssetGroupId, project, url)
		if assetID, ok := getCachedAssetID(cacheKey); ok {
			media.URL = "asset://" + assetID
			continue
		}

		assetID, err := cl.CreateAndWait(ctx, s.BytePlusAssetGroupId, url, assetType)
		if err != nil {
			return errors.Wrapf(err, "preupload %s to byteplus asset library failed", item.Type)
		}
		setCachedAssetID(cacheKey, assetID)
		media.URL = "asset://" + assetID
	}
	return nil
}

// pickMedia returns the media reference and its AssetType string for a content item.
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

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "..."
}

func assetCacheKey(channelId int, groupId, project, url string) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%d|%s|%s|%s", channelId, groupId, project, url)))
	return hex.EncodeToString(h[:])
}

var (
	assetIDCache     *cachex.HybridCache[string]
	assetIDCacheOnce sync.Once
)

func getAssetIDCache() *cachex.HybridCache[string] {
	assetIDCacheOnce.Do(func() {
		assetIDCache = cachex.NewHybridCache[string](cachex.HybridCacheConfig[string]{
			Namespace: cachex.Namespace("byteplus_asset"),
			Redis:     common.RDB,
			RedisEnabled: func() bool {
				return common.RedisEnabled && common.RDB != nil
			},
			RedisCodec: cachex.StringCodec{},
			Memory: func() *hot.HotCache[string, string] {
				return hot.NewHotCache[string, string](hot.LRU, 10_000).
					WithTTL(assetCacheTTL).
					WithJanitor().
					Build()
			},
		})
	})
	return assetIDCache
}

func getCachedAssetID(key string) (string, bool) {
	v, found, err := getAssetIDCache().Get(key)
	if err != nil || !found {
		return "", false
	}
	return v, true
}

func setCachedAssetID(key, assetID string) {
	if err := getAssetIDCache().SetWithTTL(key, assetID, assetCacheTTL); err != nil {
		logger.LogWarn(context.Background(), fmt.Sprintf("cache byteplus asset id failed: %s", err.Error()))
	}
}
