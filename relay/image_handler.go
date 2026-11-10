package relay

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-gonic/gin"
)

func ImageHelper(c *gin.Context, info *relaycommon.RelayInfo) (newAPIError *types.NewAPIError) {
	info.InitChannelMeta(c)
	info.BillingImageCount = nil
	info.ImageRequestCount = 0
	info.SvgUsage = nil

	imageReq, ok := info.Request.(*dto.ImageRequest)
	if !ok {
		return types.NewErrorWithStatusCode(fmt.Errorf("invalid request type, expected dto.ImageRequest, got %T", info.Request), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}

	request, err := common.DeepCopy(imageReq)
	if err != nil {
		return types.NewError(fmt.Errorf("failed to copy request to ImageRequest: %w", err), types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
	}

	err = helper.ModelMappedHelper(c, info, request)
	if err != nil {
		return types.NewError(err, types.ErrorCodeChannelModelMappedError, types.ErrOptionWithSkipRetry())
	}

	adaptor := GetAdaptor(info.ApiType)
	if adaptor == nil {
		return types.NewError(fmt.Errorf("invalid api type: %d", info.ApiType), types.ErrorCodeInvalidApiType, types.ErrOptionWithSkipRetry())
	}
	adaptor.Init(info)
	imageCount, err := request.ImageCount(false)
	if err != nil {
		return types.NewErrorWithStatusCode(err, types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}

	var requestBody io.Reader
	var jsonData []byte

	if model_setting.GetGlobalSettings().PassThroughRequestEnabled || info.ChannelSetting.PassThroughBodyEnabled {
		storage, err := common.GetBodyStorage(c)
		if err != nil {
			return types.NewErrorWithStatusCode(err, types.ErrorCodeReadRequestBodyFailed, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
		}
		if strings.Contains(c.Request.Header.Get("Content-Type"), "multipart/form-data") {
			requestBody = common.NewReplayableBodyReader(storage)
		} else {
			jsonData, err = storage.Bytes()
			if err != nil {
				return types.NewErrorWithStatusCode(err, types.ErrorCodeReadRequestBodyFailed, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
			}
		}
	} else {
		convertedRequest, err := adaptor.ConvertImageRequest(c, info, *request)
		if err != nil {
			// An adaptor that already classified its rejection (status code
			// and retry policy) keeps that classification instead of being
			// downgraded to a retryable conversion failure.
			var apiErr *types.NewAPIError
			if errors.As(err, &apiErr) {
				return apiErr
			}
			return types.NewError(err, types.ErrorCodeConvertRequestFailed)
		}
		relaycommon.AppendRequestConversionFromRequest(info, convertedRequest)

		switch convertedRequest.(type) {
		case *bytes.Buffer:
			requestBody = convertedRequest.(io.Reader)
		default:
			jsonData, err = common.Marshal(convertedRequest)
			if err != nil {
				return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
			}

			// apply param override
			if len(info.ParamOverride) > 0 {
				jsonData, err = relaycommon.ApplyParamOverrideWithRelayInfo(jsonData, info)
				if err != nil {
					return newAPIErrorFromParamOverride(err)
				}
			}

		}
	}
	if jsonData != nil {
		// This is a different trust boundary from ingress: channel overrides
		// and pass-through bodies can change the quantity actually submitted.
		var outbound struct {
			N *uint `json:"n"`
		}
		if err := common.Unmarshal(jsonData, &outbound); err != nil {
			return types.NewErrorWithStatusCode(fmt.Errorf("invalid image billing parameters: %w", err), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
		}
		quantityRequest := dto.ImageRequest{N: outbound.N}
		if quantityRequest.N == nil {
			quantityRequest.N = common.GetPointer(uint(imageCount))
		}
		imageCount, err = quantityRequest.ImageCount(false)
		if err != nil {
			return types.NewErrorWithStatusCode(err, types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
		}
		logger.LogDebug(c, "image request body: %s", jsonData)
		body, closer, err := relaycommon.NewOutboundJSONBody(jsonData)
		if err != nil {
			return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
		}
		defer closer.Close()
		requestBody = body
	}
	if billingErr := service.PrepareImageBillingForRequest(c, info, imageCount); billingErr != nil {
		return billingErr
	}

	statusCodeMappingStr := c.GetString("status_code_mapping")

	resp, err := adaptor.DoRequest(c, info, requestBody)
	if err != nil {
		return types.NewOpenAIError(err, types.ErrorCodeDoRequestFailed, http.StatusInternalServerError)
	}
	var httpResp *http.Response
	if resp != nil {
		httpResp = resp.(*http.Response)
		info.IsStream = info.IsStream || strings.HasPrefix(httpResp.Header.Get("Content-Type"), "text/event-stream")
		if httpResp.StatusCode != http.StatusOK {
			if httpResp.StatusCode == http.StatusCreated && info.ApiType == constant.APITypeReplicate {
				// replicate channel returns 201 Created when using Prefer: wait, treat it as success.
				httpResp.StatusCode = http.StatusOK
			} else {
				newAPIError = service.RelayErrorHandler(c.Request.Context(), httpResp, false)
				// reset status code 重置状态码
				service.ResetStatusCode(newAPIError, statusCodeMappingStr)
				return newAPIError
			}
		}
	}

	usage, newAPIError := adaptor.DoResponse(c, httpResp, info)
	if newAPIError != nil {
		// reset status code 重置状态码
		service.ResetStatusCode(newAPIError, statusCodeMappingStr)
		return newAPIError
	}

	// The log content shows the settled count: the count the handler derived
	// from the upstream response when it did, otherwise the reserved quantity.
	imageN := info.RequestedImageCount()
	if info.BillingImageCount != nil {
		imageN = *info.BillingImageCount
	} else if count, ok := info.PriceData.OtherRatios()["n"]; ok && info.PriceData.UsePrice && count >= 1 && count <= dto.MaxImageN {
		imageN = common.QuotaRound(count)
	}

	if usage.(*dto.Usage).TotalTokens == 0 {
		usage.(*dto.Usage).TotalTokens = 1
	}
	if usage.(*dto.Usage).PromptTokens == 0 {
		usage.(*dto.Usage).PromptTokens = 1
	}

	quality := request.Quality
	if quality == "" {
		quality = "standard"
	}

	var logContent []*common.Message

	if len(request.Size) > 0 {
		logContent = append(logContent, common.NewMessage("Size {{size}}", map[string]any{"size": request.Size}))
	}
	if len(quality) > 0 {
		logContent = append(logContent, common.NewMessage("Quality {{quality}}", map[string]any{"quality": quality}))
	}
	if imageN > 0 {
		logContent = append(logContent, common.NewMessage("Image count {{count}}", map[string]any{"count": imageN}))
	}

	service.PostTextConsumeQuota(c, info, usage.(*dto.Usage), logContent)

	var syncTask *model.Task
	if common.RecordSyncImageTaskEnabled {
		syncTask = insertSyncImageTask(c, info, request, quality, uint(imageN))
	}

	// Global detection: L1/L2 always run; L3 is gated by sampling rate.
	// Uses gopool.Go so the goroutine has panic recovery from the pool.
	// context.Background() ensures the pipeline outlives the HTTP request.
	if s := operation_setting.GetImageDetectionSetting(); s.GlobalEnabled {
		channelID := info.GetChannelID()
		if s.IsChannelInScope(channelID, nil) && rand.Float64() < s.GlobalSampleRate {
			taskID := ""
			if syncTask != nil {
				taskID = syncTask.TaskID
			}
			// Prefer decoded bytes (captured by sampleImagePayload in the
			// openai image handler); fall back to fetching the URL when only
			// a URL was returned (URL-only responses, expires in ~1h).
			imgBytes := info.SampledImageBytes
			if len(imgBytes) == 0 && info.SampledImageURL != "" {
				if fetched, err := fetchSampledImageURL(info.SampledImageURL); err == nil {
					imgBytes = fetched
				}
			}
			job := service.ImageDetectionJob{
				TaskID:      taskID,
				RequestID:   info.RequestId,
				ChannelID:   channelID,
				Model:       request.Model,
				RequestSize: request.Size,
				LatencyMs:   info.FirstResponseTime.Sub(info.StartTime).Milliseconds(),
				ImageBytes:  imgBytes,
				Trigger:     service.ImageDetectionTriggerGlobal,
			}
			gopool.Go(func() { service.RunImageDetectionPipeline(job) })
		}
	}

	return nil
}

// insertSyncImageTask 将同步生图调用合成一条 task 记录写入 tasks 表，
// 让其在「绘图日志」中可见。任何错误仅记录日志，不影响响应。返回写入的
// task，供全局图片检测复用其 task_id；插入失败或 panic 时返回 nil。
func insertSyncImageTask(c *gin.Context, info *relaycommon.RelayInfo, request *dto.ImageRequest, quality string, imageN uint) (task *model.Task) {
	defer func() {
		if r := recover(); r != nil {
			common.SysError(fmt.Sprintf("insertSyncImageTask panic: %v", r))
			task = nil
		}
	}()

	task = model.InitTask(constant.TaskPlatformOpenAIImage, info)
	now := time.Now().Unix()
	task.Status = model.TaskStatusSuccess
	task.Progress = "100%"
	task.StartTime = now
	task.FinishTime = now
	task.Quota = info.PriceData.Quota

	if info.RelayMode == relayconstant.RelayModeImagesEdits {
		task.Action = constant.TaskActionImageToVideo // edits mapped to closest video action
	} else {
		task.Action = constant.TaskActionTextToVideo // generate mapped to text-to-video
	}

	task.PrivateData.BillingSource = info.BillingSource
	task.PrivateData.SubscriptionId = info.SubscriptionId
	task.PrivateData.TokenId = info.TokenId
	if len(info.ImageResultURLs) > 0 {
		task.PrivateData.ResultURL = info.ImageResultURLs[0]
	}

	prompt := request.Prompt
	if len(prompt) > 300 {
		prompt = prompt[:300]
	}
	taskData := map[string]any{
		"prompt":          prompt,
		"size":            request.Size,
		"quality":         quality,
		"n":               imageN,
		"response_format": request.ResponseFormat,
		"model":           request.Model,
		"urls":            info.ImageResultURLs,
		"b64_count":       info.ImageResultB64Count,
	}
	task.SetData(taskData)

	if err := task.Insert(); err != nil {
		common.SysError("insert sync image task error: " + err.Error())
		return nil
	}
	return task
}

// fetchSampledImageURL downloads the image at imageURL for passive detection.
// Uses the project's SSRF-protected HTTP client so private-network URLs from
// pool channels cannot reach internal infrastructure.
// Returns nil bytes on any error; the detection pipeline handles nil gracefully.
func fetchSampledImageURL(imageURL string) ([]byte, error) {
	const maxBytes = 10 * 1024 * 1024
	client := service.GetSSRFProtectedHTTPClient()
	req, err := http.NewRequestWithContext(context.TODO(), http.MethodGet, imageURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d fetching sampled image", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes))
	if err != nil {
		return nil, err
	}
	return data, nil
}
