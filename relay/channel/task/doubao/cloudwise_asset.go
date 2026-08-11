package doubao

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"

	"github.com/pkg/errors"
)

// cloudwiseAssetClient implements assetClient for the third-party cloudwise asset proxy.
//
// Protocol (verified from cloudwise-api-docs.md, 2026-08-10 export):
//   - Auth: Authorization: Bearer {channel api key} — no AK/SK required.
//   - Success envelope for all three operations is PascalCase:
//     {ResponseMetadata:{RequestId, ...}, Result:{...}}
//   - Create-asset body mixes cases: assetType/groupId/url and lowercase
//     name/description, with a nested PascalCase Moderation.Strategy.
//   - An asset is usable at Status=="Active"; "Processing" keeps polling and
//     "Failed" is terminal.
//
// ERROR SHAPE — NOT VERIFIED. The document has no error-response example for any
// asset operation. Two shapes are therefore both accepted, and neither is
// confirmed for asset endpoints:
//   - ResponseMetadata.Error{Code, Message}: inferred from the sibling BytePlus
//     provider (see byteplus_asset.go), on the assumption that cloudwise proxies
//     the same underlying Volcengine service.
//   - lowercase {code, message} (also nested under "error"): the shape this
//     gateway demonstrably uses for its task endpoints.
//
// When neither parses on a non-2xx, the HTTP status becomes the error code so
// that classification still has something to work with. Revisit once a real
// asset-operation error is observed in production logs.
//
// DISCREPANCY vs task-4-brief.md (document wins per task instructions):
//   - Brief claimed "upload uses {code,message,data} (snake_case)" — WRONG.
//     Document shows /api/v1/assets/create returns {ResponseMetadata, Result:{Id}}.
//   - Brief claimed create-group request is PascalCase — WRONG.
//     Document shows {"name":"","description":"..."} (lowercase).
//   - Brief claimed upload request is snake_case — WRONG.
//     Document shows mixed: assetType/groupId/url (camelCase), Moderation (PascalCase).
//   - Brief claimed ready status is "completed" — WRONG.
//     Document shows Status:"Active" (same as BytePlus).
type cloudwiseAssetClient struct {
	baseURL        string
	apiKey         string
	skipModeration bool
	httpClient     *http.Client
	// pollInterval/pollTimeout zero values fall back to package-level defaults.
	pollInterval time.Duration
	pollTimeout  time.Duration
}

const (
	cloudwisePollInterval = 2 * time.Second
	cloudwisePollTimeout  = 60 * time.Second
)

func (cl *cloudwiseAssetClient) intervalOrDefault() time.Duration {
	if cl.pollInterval > 0 {
		return cl.pollInterval
	}
	return cloudwisePollInterval
}

func (cl *cloudwiseAssetClient) timeoutOrDefault() time.Duration {
	if cl.pollTimeout > 0 {
		return cl.pollTimeout
	}
	return cloudwisePollTimeout
}

// cloudwiseEnvelope is the unified response envelope for all cloudwise asset operations.
// Same shape as bytePlusEnvelope — cloudwise acts as a Bearer-auth proxy to the same
// underlying Volcengine service.
type cloudwiseEnvelope struct {
	ResponseMetadata struct {
		RequestId string `json:"RequestId"`
		Error     *struct {
			Code    string `json:"Code"`
			Message string `json:"Message"`
		} `json:"Error,omitempty"`
	} `json:"ResponseMetadata"`
	Result struct {
		Id     string `json:"Id"`
		Status string `json:"Status"`
	} `json:"Result"`
}

// cloudwiseCreateGroupReq: document shows lowercase field names.
type cloudwiseCreateGroupReq struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// cloudwiseCreateAssetReq: document shows camelCase for data fields, PascalCase for Moderation.
type cloudwiseCreateAssetReq struct {
	AssetType  string                    `json:"assetType"`
	GroupId    string                    `json:"groupId"`
	URL        string                    `json:"url"`
	Name       string                    `json:"name,omitempty"`
	Moderation *cloudwiseAssetModeration `json:"Moderation,omitempty"`
}

type cloudwiseAssetModeration struct {
	Strategy string `json:"Strategy"`
}

// cloudwiseGetAssetReq: document shows PascalCase Id field.
type cloudwiseGetAssetReq struct {
	Id string `json:"Id"`
}

// CreateGroup creates a new cloudwise asset group and returns its id.
// POST {base}/api/v1/assets/groups/create
func (cl *cloudwiseAssetClient) CreateGroup(ctx context.Context, name string) (string, error) {
	env, _, err := cl.post(ctx, "/api/v1/assets/groups/create", cloudwiseCreateGroupReq{Name: name})
	if err != nil {
		return "", errors.Wrap(err, "cloudwise CreateGroup failed")
	}
	if env.Result.Id == "" {
		return "", errors.New("cloudwise CreateGroup returned empty group id")
	}
	return env.Result.Id, nil
}

// CreateAndWait uploads a single asset and polls until Active, returning assetId.
func (cl *cloudwiseAssetClient) CreateAndWait(ctx context.Context, groupID, mediaURL, assetType string) (string, error) {
	req := cloudwiseCreateAssetReq{
		AssetType: assetType,
		GroupId:   groupID,
		URL:       mediaURL,
	}
	if cl.skipModeration {
		req.Moderation = &cloudwiseAssetModeration{Strategy: "Skip"}
	}
	env, _, err := cl.post(ctx, "/api/v1/assets/create", req)
	if err != nil {
		return "", errors.Wrap(err, "cloudwise CreateAsset failed")
	}
	id := env.Result.Id
	if id == "" {
		return "", errors.New("cloudwise CreateAsset returned empty asset id")
	}

	deadline := time.Now().Add(cl.timeoutOrDefault())
	for {
		statusEnv, raw, pollErr := cl.post(ctx, "/api/v1/assets/get", cloudwiseGetAssetReq{Id: id})
		if pollErr != nil {
			return "", errors.Wrap(pollErr, "cloudwise GetAsset failed")
		}
		switch statusEnv.Result.Status {
		case "Active":
			return id, nil
		case "Failed":
			// raw is already masked by post; only truncate so a large rejection
			// body cannot be embedded whole into the relay response and logs.
			return "", errors.Errorf("cloudwise asset %s processing failed: %s", id, truncate([]byte(raw), 512))
		}
		if time.Now().After(deadline) {
			return "", errors.Errorf("cloudwise asset %s not ready within %s (last status: %s)",
				id, cl.timeoutOrDefault(), statusEnv.Result.Status)
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(cl.intervalOrDefault()):
		}
	}
}

// cloudwiseGroupExhaustedCodes are string error codes that indicate the asset group is
// full, invalid, or otherwise unusable for new uploads.
//
// PROVENANCE: The cloudwise API documentation contains no error-code table.
// These code names are guesses based on the underlying Volcengine/BytePlus platform's
// naming conventions (cloudwise appears to be a Bearer-auth proxy to the same service).
// They are NOT confirmed from any documentation.
//
// When a real group-full or group-not-found error is observed in production logs,
// verify the actual Code value and update this list accordingly.
//
// Design intent: same as groupExhaustedCodes in byteplus_asset.go.
//   - Do NOT add bare quota or rate-limit codes — they would cause runaway group creation
//     under API throttling.
//   - Unknown codes return false; the operator sees the real error.
var cloudwiseGroupExhaustedCodes = map[string]bool{
	"GroupFull":                   true,
	"InvalidGroupId":              true,
	"GroupNotFound":               true,
	"AssetGroupCapacityExceeded":  true,
	"InvalidParameter.GroupId":    true,
	"ResourceNotFound.GroupId":    true,
	"LimitExceeded.GroupCapacity": true,
}

// IsGroupExhausted returns true only when err is an *assetAPIError whose Code
// positively matches a known group-unusable error code.
// Keyword fallback requires BOTH a group indicator AND a capacity/existence
// indicator so that auth or moderation failures cannot trip it.
func (cl *cloudwiseAssetClient) IsGroupExhausted(err error) bool {
	var apiErr *assetAPIError
	if !errors.As(err, &apiErr) {
		return false
	}
	if cloudwiseGroupExhaustedCodes[apiErr.Code] {
		return true
	}
	// Message-text fallback for undocumented codes observed in production.
	// Requires BOTH a group-scoped word AND an unambiguous capacity word.
	//
	// Deliberately NOT matched here:
	//   - "exceed": it also matches "exceeded", which nearly every quota and
	//     throttle message contains ("asset group request rate exceeded, retry
	//     later"). Under throttling that would mint one junk group per concurrent
	//     request, the exact runaway that keeping QuotaExceeded out of the code map
	//     prevents.
	//   - "not found": a missing group id is an operator misconfiguration far more
	//     often than an exhausted group, and rotating hides it.
	msg := strings.ToLower(apiErr.Message)
	return strings.Contains(msg, "group") &&
		(strings.Contains(msg, "full") || strings.Contains(msg, "capacity"))
}

// post sends a POST to {baseURL}{path} with Bearer auth and returns the parsed envelope.
// The masked raw response is returned alongside for error reporting.
func (cl *cloudwiseAssetClient) post(ctx context.Context, path string, payload any) (*cloudwiseEnvelope, string, error) {
	body, err := common.Marshal(payload)
	if err != nil {
		return nil, "", err
	}

	url := strings.TrimRight(cl.baseURL, "/") + path
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cl.apiKey)

	client := cl.httpClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}

	maskedRaw := common.MaskSensitiveInfo(string(respBody))

	var env cloudwiseEnvelope
	envErr := common.Unmarshal(respBody, &env)

	if envErr == nil && env.ResponseMetadata.Error != nil && env.ResponseMetadata.Error.Code != "" {
		return nil, maskedRaw, &assetAPIError{
			Code:    env.ResponseMetadata.Error.Code,
			Message: env.ResponseMetadata.Error.Message,
			Raw:     maskedRaw,
		}
	}
	// Every error response must surface as *assetAPIError: IsGroupExhausted starts
	// with an errors.As check, so a plain error here would make group rotation
	// unreachable on this provider. The lowercase {code,message} shape and a
	// status-code fallback cover the undocumented asset-error format.
	if resp.StatusCode >= http.StatusBadRequest {
		code, message := parseCloudwiseLowercaseError(respBody)
		if code == "" {
			code = "HTTP" + strconv.Itoa(resp.StatusCode)
		}
		if message == "" {
			// Keep the body as the message so the group-capacity text fallback can
			// still match a non-JSON or unrecognised error payload.
			message = truncate([]byte(maskedRaw), 512)
		}
		return nil, maskedRaw, &assetAPIError{Code: code, Message: message, Raw: maskedRaw}
	}
	if envErr != nil {
		return nil, maskedRaw, errors.Wrapf(envErr,
			"cloudwise: unmarshal response failed (status %d, body: %s)", resp.StatusCode, truncate(respBody, 512))
	}
	return &env, maskedRaw, nil
}

// parseCloudwiseLowercaseError extracts the lowercase {code,message} error shape,
// accepting it both at the top level and nested under "error" — the two forms this
// gateway uses for its task endpoints. code is read as a raw scalar because the
// document shows it quoted ("10000") while a numeric code would otherwise break
// the whole parse. Returns empty strings when the body does not carry this shape.
func parseCloudwiseLowercaseError(body []byte) (code, message string) {
	var payload struct {
		Code    json.RawMessage `json:"code"`
		Message string          `json:"message"`
		Error   *struct {
			Code    json.RawMessage `json:"code"`
			Message string          `json:"message"`
		} `json:"error"`
	}
	if err := common.Unmarshal(body, &payload); err != nil {
		return "", ""
	}
	if payload.Error != nil {
		return jsonScalarString(payload.Error.Code), payload.Error.Message
	}
	return jsonScalarString(payload.Code), payload.Message
}

// jsonScalarString renders a raw JSON scalar as a plain string, stripping the
// quotes from a JSON string and leaving a number as written.
func jsonScalarString(raw json.RawMessage) string {
	s := strings.TrimSpace(string(raw))
	if s == "" || s == "null" {
		return ""
	}
	return strings.Trim(s, `"`)
}
