package storage

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
)

type CloudPasteClient struct {
	BaseURL         string
	APIKey          string
	StorageConfigID string // optional
	HTTPClient      *http.Client
}

type UploadResult struct {
	Slug        string `json:"slug"`
	URL         string `json:"url"`
	PreviewURL  string `json:"previewUrl"`
	DownloadURL string `json:"downloadUrl"`
	LinkType    string `json:"linkType"`
}

func NewCloudPasteClient(baseURL, apiKey, storageConfigID string) *CloudPasteClient {
	return &CloudPasteClient{
		BaseURL:         baseURL,
		APIKey:          apiKey,
		StorageConfigID: storageConfigID,
		HTTPClient: &http.Client{
			Timeout: 5 * time.Minute,
		},
	}
}

func (c *CloudPasteClient) StreamUpload(ctx context.Context, filename, contentType string, body io.Reader, fileSize int64) (*UploadResult, error) {
	uploadURL := fmt.Sprintf("%s/api/share/upload", c.BaseURL)

	// Build X-Share-Options JSON
	options := map[string]any{
		"use_proxy":         true,
		"original_filename": true,
	}
	if c.StorageConfigID != "" {
		options["storage_config_id"] = c.StorageConfigID
	}
	optionsJSON, err := common.Marshal(options)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal share options: %w", err)
	}
	optionsBase64 := base64.StdEncoding.EncodeToString(optionsJSON)

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, uploadURL, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create upload request: %w", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("ApiKey %s", c.APIKey))
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("X-Share-Filename", filename)
	req.Header.Set("X-Share-Options", optionsBase64)
	if fileSize > 0 {
		req.Header.Set("Content-Length", strconv.FormatInt(fileSize, 10))
		req.ContentLength = fileSize
	}

	logger.LogInfo(ctx, fmt.Sprintf("CloudPaste uploading file: %s, size: %d", filename, fileSize))

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cloudpaste upload request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		errMsg := fmt.Sprintf("cloudpaste upload failed with status %d: %s", resp.StatusCode, string(respBody))
		logger.LogError(ctx, errMsg)
		return nil, fmt.Errorf(errMsg)
	}

	var result UploadResult
	if err := common.DecodeJson(resp.Body, &result); err != nil {
		return nil, fmt.Errorf("failed to decode cloudpaste upload response: %w", err)
	}

	logger.LogInfo(ctx, fmt.Sprintf("CloudPaste upload success: slug=%s, url=%s", result.Slug, result.URL))
	return &result, nil
}

func GenerateSlug(platform, taskID string) string {
	return fmt.Sprintf("%s_%s_%d", platform, taskID, time.Now().Unix())
}
