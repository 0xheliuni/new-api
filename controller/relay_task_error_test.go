package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func runRespondTaskError(t *testing.T, path string, taskErr *dto.TaskError) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req, err := http.NewRequest(http.MethodPost, path, nil)
	if err != nil {
		t.Fatal(err)
	}
	c.Request = req
	respondTaskError(c, taskErr)
	return w
}

func TestRespondTaskError_OpenAIVideoEndpointUsesOpenAIFormat(t *testing.T) {
	w := runRespondTaskError(t, "/v1/videos", &dto.TaskError{
		Code:       "content_policy_violation",
		Message:    "The prompt violates content policy",
		StatusCode: http.StatusBadRequest,
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	body := w.Body.String()
	for _, want := range []string{`"error"`, `"message":"The prompt violates content policy"`, `"type":"invalid_request_error"`, `"code":"content_policy_violation"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %s: %s", want, body)
		}
	}
	if strings.Contains(body, `"data"`) {
		t.Fatalf("OpenAI-format body must not contain TaskError data field: %s", body)
	}
}

func TestRespondTaskError_OpenAIVideoFetchPathAlsoOpenAIFormat(t *testing.T) {
	w := runRespondTaskError(t, "/v1/videos/task_abc123", &dto.TaskError{
		Code:       "upstream_error",
		Message:    "boom",
		StatusCode: http.StatusBadGateway,
	})
	body := w.Body.String()
	if !strings.Contains(body, `"type":"upstream_error"`) {
		t.Fatalf("5xx must map to upstream_error type: %s", body)
	}
}

func TestRespondTaskError_LegacyEndpointKeepsTaskErrorShape(t *testing.T) {
	w := runRespondTaskError(t, "/v1/video/generations", &dto.TaskError{
		Code:       "upstream_error",
		Message:    "boom",
		StatusCode: http.StatusBadRequest,
	})
	body := w.Body.String()
	if !strings.Contains(body, `"code":"upstream_error"`) || !strings.Contains(body, `"data"`) {
		t.Fatalf("legacy endpoint should keep TaskError shape: %s", body)
	}
	if strings.Contains(body, `invalid_request_error`) {
		t.Fatalf("legacy endpoint must not use OpenAI type mapping: %s", body)
	}
}

func TestRespondTaskError_429RewritesMessageBothShapes(t *testing.T) {
	w := runRespondTaskError(t, "/v1/videos", &dto.TaskError{
		Code: "rate_limited", Message: "upstream raw", StatusCode: http.StatusTooManyRequests,
	})
	if !strings.Contains(w.Body.String(), "当前分组上游负载已饱和") {
		t.Fatalf("429 message not rewritten: %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"type":"rate_limit_error"`) {
		t.Fatalf("429 type wrong: %s", w.Body.String())
	}
}

// 端到端形状回归：上游火山引擎内容审核 400 → 客户在 /v1/videos 收到的最终响应。
func TestRespondTaskError_VolcengineSensitiveContentEndToEnd(t *testing.T) {
	upstreamBody := []byte(`{"error":{"code":"InputTextSensitiveContentDetected","message":"The request failed because the input text 'content[6]' may contain sensitive information. Request id: 021785493621261e63fcd4b145954693"}}`)
	taskErr := service.TaskErrorFromUpstreamBody(http.StatusBadRequest, upstreamBody)
	w := runRespondTaskError(t, "/v1/videos", taskErr)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	body := w.Body.String()
	for _, want := range []string{
		`"code":"InputTextSensitiveContentDetected"`,
		`may contain sensitive information`,
		`Request id: 021785493621261e63fcd4b145954693`,
		`"type":"invalid_request_error"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("client body missing %s:\n%s", want, body)
		}
	}
}
