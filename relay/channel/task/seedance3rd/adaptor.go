package seedance3rd

import (
	"fmt"
	"net/http"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
)

// ============================
// Request / Response structures (第三方 model.service-inference.ai)
// ============================

type ContentItem struct {
	Type     string    `json:"type,omitempty"`
	Text     string    `json:"text,omitempty"`
	ImageURL *MediaURL `json:"image_url,omitempty"`
	VideoURL *MediaURL `json:"video_url,omitempty"`
	AudioURL *MediaURL `json:"audio_url,omitempty"`
	Role     string    `json:"role,omitempty"`
}

type MediaURL struct {
	URL string `json:"url,omitempty"`
}

// requestPayload 对齐第三方 POST /v1/video/generate 顶层参数。
type requestPayload struct {
	Model           string         `json:"model"`
	Content         []ContentItem  `json:"content,omitempty"`
	Duration        *dto.IntValue  `json:"duration,omitempty"`
	Resolution      string         `json:"resolution,omitempty"`
	Ratio           string         `json:"ratio,omitempty"`
	GenerateAudio   *dto.BoolValue `json:"generate_audio,omitempty"`
	Watermark       *dto.BoolValue `json:"watermark,omitempty"`
	ReturnLastFrame *dto.BoolValue `json:"return_last_frame,omitempty"`
}

// submitResponse: { "task": { "id": ... } }
type submitResponse struct {
	Task struct {
		ID string `json:"id"`
	} `json:"task"`
}

// fetchResponse: { "task": {...} }
type fetchResponse struct {
	Task struct {
		ID           string   `json:"id"`
		Status       string   `json:"status"`
		Model        string   `json:"model"`
		Outputs      []string `json:"outputs"`
		LastFrameURL string   `json:"last_frame_url"`
		Error        *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
		Usage struct {
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
		DurationSeconds int `json:"duration_seconds"`
	} `json:"task"`
}

// ============================
// Adaptor
// ============================

type TaskAdaptor struct {
	taskcommon.BaseBilling
	ChannelType   int
	apiKey        string
	baseURL       string
	channelId     int
	proxy         string
	otherSettings dto.ChannelOtherSettings
	// endpointOverride 仅测试用,指向 httptest.Server;生产为空。
	endpointOverride string
}

func (a *TaskAdaptor) Init(info *relaycommon.RelayInfo) {
	a.ChannelType = info.ChannelType
	a.baseURL = info.ChannelBaseUrl
	a.apiKey = info.ApiKey
	a.channelId = info.ChannelId
	a.proxy = info.ChannelSetting.Proxy
	a.otherSettings = info.ChannelOtherSettings
}

func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	return relaycommon.ValidateBasicTaskRequest(c, info, constant.TaskActionGenerate)
}

func (a *TaskAdaptor) BuildRequestURL(_ *relaycommon.RelayInfo) (string, error) {
	return fmt.Sprintf("%s/v1/video/generate", a.baseURL), nil
}

func (a *TaskAdaptor) BuildRequestHeader(_ *gin.Context, req *http.Request, _ *relaycommon.RelayInfo) error {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	return nil
}

func (a *TaskAdaptor) GetModelList() []string { return ModelList }
func (a *TaskAdaptor) GetChannelName() string { return ChannelName }
