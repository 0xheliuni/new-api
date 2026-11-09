package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
)

const (
	maxChannelTaskPluginConfigBytes = 32 << 10
	// taskPluginConfigValidateTimeout bounds the validateChannelConfig call,
	// including the wait for a free JavaScript slot of a busy plugin.
	taskPluginConfigValidateTimeout = 10 * time.Second
)

// validateChannelTaskPluginConfig checks a channel's task_plugin_config and
// has the executing plugin validate it: the bound plugin of a Task Plugin
// channel, or the plugin registered for a legacy channel type. It returns the
// decoded configuration and that plugin, or nil values when the channel
// stores no configuration.
func validateChannelTaskPluginConfig(ctx context.Context, channelType int, setting dto.ChannelSettings) (map[string]any, *jsplugin.LoadedPlugin, error) {
	raw := bytes.TrimSpace(setting.TaskPluginConfig)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil, nil, nil
	}
	if len(raw) > maxChannelTaskPluginConfigBytes {
		return nil, nil, common.NewMessage("Task plugin configuration must not exceed {{size}} KiB", map[string]any{"size": maxChannelTaskPluginConfigBytes >> 10})
	}
	var config map[string]any
	if err := common.Unmarshal(raw, &config); err != nil || config == nil {
		return nil, nil, common.NewMessage("Task plugin configuration must be a JSON object")
	}

	var plugin *jsplugin.LoadedPlugin
	var found bool
	switch channelType {
	case constant.ChannelTypeTaskPlugin:
		key := strings.TrimSpace(setting.TaskPluginKey)
		if key == "" {
			return nil, nil, common.NewMessage("Task plugin key is required")
		}
		if plugin, found = jsplugin.DefaultRegistry.Get(key); !found {
			return nil, nil, common.NewMessage("Task plugin {{key}} is not registered", map[string]any{"key": key})
		}
	case constant.ChannelTypeNewAPI:
		return nil, nil, common.NewMessage("Task plugin configuration is not supported on New API channels")
	default:
		if plugin, found = jsplugin.DefaultRegistry.GetByChannelType(channelType); !found {
			return nil, nil, common.NewMessage("Channel type {{type}} does not run a task plugin, so a task plugin configuration is not supported", map[string]any{"type": channelType})
		}
	}
	if !plugin.Engine.HasExport("validateChannelConfig") {
		return nil, nil, common.NewMessage("Task plugin {{key}} does not accept a channel configuration", map[string]any{"key": plugin.Meta.Key})
	}

	ctx, cancel := context.WithTimeout(ctx, taskPluginConfigValidateTimeout)
	defer cancel()
	_, err := plugin.Engine.Call(ctx, "validateChannelConfig", config)
	if err == nil {
		return config, plugin, nil
	}
	// Only an error the hook threw is the plugin's verdict on the
	// configuration. A timeout, a non-callable export or an engine failure is
	// not, so it is logged and reported without blaming the configuration.
	var hookErr *jsplugin.HookError
	if errors.As(err, &hookErr) {
		return nil, nil, common.NewMessage("Invalid task plugin configuration: {{error}}", map[string]any{"error": taskPluginHookMessage(err)})
	}
	common.SysError(common.LogText("task plugin %s failed to validate a channel configuration: %v", plugin.Meta.Key, err))
	return nil, nil, common.NewMessage("Task plugin {{key}} could not validate the configuration, please try again later", map[string]any{"key": plugin.Meta.Key})
}

// taskPluginHookMessage is the sanitized message a plugin hook threw, safe to
// show to administrators.
func taskPluginHookMessage(err error) string {
	var hookErr *jsplugin.HookError
	if errors.As(err, &hookErr) && hookErr.Message != "" {
		return hookErr.Message
	}
	return "plugin hook failed"
}

const (
	maxTaskPluginConfigPreviewInputBytes = 1 << 20
	// maxTaskPluginConfigPreviewBodyBytes bounds the whole preview request:
	// the three sample parts, the configuration and room for the other fields.
	maxTaskPluginConfigPreviewBodyBytes = 3*maxTaskPluginConfigPreviewInputBytes + maxChannelTaskPluginConfigBytes + 64<<10
	taskPluginConfigPreviewAPIKey       = "sk-****"
	taskPluginConfigPreviewTaskID       = "preview-task-id"
)

type taskPluginConfigPreviewRequest struct {
	Type           int             `json:"type"`
	TaskPluginKey  string          `json:"task_plugin_key"`
	BaseURL        string          `json:"base_url"`
	Config         json.RawMessage `json:"config"`
	Model          string          `json:"model"`
	Entry          string          `json:"entry"`
	Request        json.RawMessage `json:"request"`
	SubmitResponse json.RawMessage `json:"submit_response"`
	QueryResponse  json.RawMessage `json:"query_response"`
}

// PreviewChannelTaskPluginConfig runs an unsaved task_plugin_config through
// the executing plugin's decoder and driver hooks against sample requests and
// responses. It sends no network request, writes nothing and bills nothing;
// each step that fails is reported under errors without stopping the others.
func PreviewChannelTaskPluginConfig(c *gin.Context) {
	var request taskPluginConfigPreviewRequest
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxTaskPluginConfigPreviewBodyBytes)
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			common.ApiErrorT(c, "Preview input must not exceed 1 MiB")
			return
		}
		common.ApiErrorT(c, "Invalid preview request")
		return
	}
	for _, part := range []*json.RawMessage{&request.Request, &request.SubmitResponse, &request.QueryResponse} {
		if len(*part) > maxTaskPluginConfigPreviewInputBytes {
			common.ApiErrorT(c, "Preview input must not exceed 1 MiB")
			return
		}
		// A null part is absent, like a missing one.
		if bytes.Equal(bytes.TrimSpace(*part), []byte("null")) {
			*part = nil
		}
	}
	ctx := c.Request.Context()
	config, plugin, err := validateChannelTaskPluginConfig(ctx, request.Type, dto.ChannelSettings{TaskPluginKey: request.TaskPluginKey, TaskPluginConfig: request.Config})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if config == nil {
		common.ApiErrorT(c, "Task plugin configuration is empty")
		return
	}

	baseURL := strings.TrimRight(strings.TrimSpace(request.BaseURL), "/")
	authHeader := "Bearer " + taskPluginConfigPreviewAPIKey
	driver := map[string]any{
		"model": request.Model, "upstreamModel": request.Model, "baseUrl": baseURL,
		"apiKey": taskPluginConfigPreviewAPIKey, "authHeader": authHeader, "auth": map[string]any{"authHeader": authHeader},
		"upstream": map[string]any{"kind": jsplugin.UpstreamKindVendor}, "requestHeaders": map[string]any{}, "files": []any{},
		"publicTaskId": taskPluginConfigPreviewTaskID, "action": "", "channelConfig": config,
	}
	result := gin.H{}
	failures := gin.H{}

	if len(request.Request) > 0 {
		if err := previewTaskPluginSubmit(ctx, plugin, request, driver, result); err != nil {
			failures["submit"] = err.Error()
		}
	}

	taskID := taskPluginConfigPreviewTaskID
	var taskData any
	if len(request.SubmitResponse) > 0 {
		var body any
		if err := common.Unmarshal(request.SubmitResponse, &body); err != nil {
			failures["submit_result"] = "submit_response must be valid JSON"
		} else if value, err := plugin.Engine.Call(ctx, "parseSubmitResponse", driver, map[string]any{"statusCode": http.StatusOK, "headers": map[string]any{}, "body": body}); err != nil {
			failures["submit_result"] = taskPluginHookMessage(err)
		} else {
			// The runtime decodes taskId into a string and refuses a blank
			// one; without a task id the query step keeps preview-task-id.
			parsed, _ := value.(map[string]any)
			if id, _ := parsed["taskId"].(string); strings.TrimSpace(id) != "" {
				taskID = id
				taskData = parsed["taskData"]
				result["submit_result"] = gin.H{"task_id": taskID}
			} else {
				failures["submit_result"] = "parseSubmitResponse returned no task id"
			}
		}
	}

	if len(request.QueryResponse) > 0 {
		query := map[string]any{
			"taskId": taskID, "publicTaskId": taskPluginConfigPreviewTaskID, "action": driver["action"],
			"model": driver["model"], "upstreamModel": driver["upstreamModel"], "baseUrl": baseURL,
			"apiKey": taskPluginConfigPreviewAPIKey, "authHeader": authHeader, "auth": driver["auth"], "upstream": driver["upstream"],
			"data": taskData, "state": nil, "channelConfig": config,
		}
		if err := previewTaskPluginQuery(ctx, plugin, query, request.QueryResponse, result); err != nil {
			failures["query_result"] = err.Error()
		}
	}

	result["errors"] = failures
	common.ApiSuccess(c, result)
}

// previewTaskPluginSubmit decodes the sample client request through the entry
// it was written for (openai_video, or a native submit route of the plugin),
// builds the submit request against the preview credentials and extracts the
// reservation facts. Each part is recorded in result ("submit",
// "reservation") as soon as it exists, so a later failure keeps it. The
// decoded body and action are written into driver for the later preview
// steps.
func previewTaskPluginSubmit(ctx context.Context, plugin *jsplugin.LoadedPlugin, request taskPluginConfigPreviewRequest, driver map[string]any, result gin.H) error {
	var body any
	if err := common.Unmarshal(request.Request, &body); err != nil {
		return errors.New("request must be valid JSON")
	}
	jsonBody := map[string]any{"kind": "json", "value": body}
	var decoded any
	var err error
	if request.Entry == "openai_video" {
		decoded, err = plugin.Engine.CallPath(ctx, "protocols", []string{"openai_video", "decodeRequest"}, map[string]any{"model": request.Model, "body": jsonBody})
	} else {
		decode := ""
		for _, route := range plugin.Meta.Routes {
			if route.Type == jsplugin.RouteTypeSubmit && strings.EqualFold(route.Method, http.MethodPost) && route.Path == request.Entry {
				decode = route.Decode
				break
			}
		}
		if decode == "" {
			return fmt.Errorf("entry %q is neither openai_video nor a native submit route of the plugin", request.Entry)
		}
		decoded, err = plugin.Engine.CallPath(ctx, "native", []string{decode}, map[string]any{"body": jsonBody, "path": request.Entry, "method": http.MethodPost})
	}
	if err != nil {
		return errors.New(taskPluginHookMessage(err))
	}
	intent, _ := decoded.(map[string]any)
	driver["requestBody"] = intent["requestBody"]
	if action, ok := intent["action"].(string); ok {
		driver["action"] = action
	}
	if model, ok := intent["model"].(string); ok && model != "" {
		driver["model"], driver["upstreamModel"] = model, model
	}

	descriptor, err := plugin.Engine.Call(ctx, "buildSubmitRequest", driver)
	if err != nil {
		return errors.New(taskPluginHookMessage(err))
	}
	result["submit"] = descriptor
	described, _ := descriptor.(map[string]any)
	url, _ := described["url"].(string)
	if err := jsplugin.ValidateRequestURL(url, fmt.Sprint(driver["baseUrl"]), plugin.Meta.AllowedHosts); err != nil {
		return err
	}
	if !plugin.Engine.HasExport("extractUsage") {
		return nil
	}
	usageCtx := maps.Clone(driver)
	usageCtx["usagePurpose"] = "facts"
	facts, err := plugin.Engine.Call(ctx, "extractUsage", usageCtx)
	if err != nil {
		return errors.New("extractUsage failed: " + taskPluginHookMessage(err))
	}
	result["reservation"] = facts
	return nil
}

// previewTaskPluginQuery builds the query request for the task in query,
// refuses a request host the runtime would refuse, parses the sample query
// response and extracts the settlement facts. Each part is recorded in result
// ("query", "query_result", "settlement") as soon as it exists, so a later
// failure keeps it.
func previewTaskPluginQuery(ctx context.Context, plugin *jsplugin.LoadedPlugin, query map[string]any, response json.RawMessage, result gin.H) error {
	var body any
	if err := common.Unmarshal(response, &body); err != nil {
		return errors.New("query_response must be valid JSON")
	}
	descriptor, err := plugin.Engine.Call(ctx, "buildQueryRequest", query)
	if err != nil {
		return errors.New(taskPluginHookMessage(err))
	}
	result["query"] = descriptor
	described, _ := descriptor.(map[string]any)
	url, _ := described["url"].(string)
	if err := jsplugin.ValidateRequestURL(url, fmt.Sprint(query["baseUrl"]), plugin.Meta.AllowedHosts); err != nil {
		return err
	}
	value, err := plugin.Engine.Call(ctx, "parseTaskResult", query, body, map[string]any{"status": http.StatusOK, "headers": map[string]any{}})
	if err != nil {
		return errors.New(taskPluginHookMessage(err))
	}
	parsed, _ := value.(map[string]any)
	result["query_result"] = gin.H{"status": parsed["status"], "url": parsed["url"], "reason": parsed["reason"], "snapshot": parsed["data"]}
	if !plugin.Engine.HasExport("extractUsageOnComplete") {
		return nil
	}
	usageBody := parsed["data"]
	if usageBody == nil {
		usageBody = body
	}
	facts, err := plugin.Engine.Call(ctx, "extractUsageOnComplete", query, parsed, usageBody)
	if err != nil {
		return errors.New("extractUsageOnComplete failed: " + taskPluginHookMessage(err))
	}
	result["settlement"] = facts
	return nil
}
