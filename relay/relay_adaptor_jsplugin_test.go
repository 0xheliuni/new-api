package relay

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	pluginruntime "github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/relay/channel"
	taskAigc "github.com/QuantumNous/new-api/relay/channel/task/aigc"
	taskdoubao "github.com/QuantumNous/new-api/relay/channel/task/doubao"
	jspluginadaptor "github.com/QuantumNous/new-api/relay/channel/task/jsplugin"
	taskminimaximage "github.com/QuantumNous/new-api/relay/channel/task/minimaximage"
	taskSeedance3rd "github.com/QuantumNous/new-api/relay/channel/task/seedance3rd"
	tasksora "github.com/QuantumNous/new-api/relay/channel/task/sora"
	taskvertex "github.com/QuantumNous/new-api/relay/channel/task/vertex"
	"github.com/QuantumNous/new-api/relay/channel/volcpassthrough"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Submission and background polling must resolve the same adaptor for a
// legacy channel-type platform: plugin-era platforms run their factory plugin
// and the fork's native channel types run their Go adaptors. An unpinned
// submission pins the resolved plugin for its retries; a native adaptor pins
// nothing, so billing never picks up a plugin key for it.
func TestLegacyTaskPlatformsResolveTheSameAdaptorForSubmitAndPolling(t *testing.T) {
	platformOf := func(channelType int) constant.TaskPlatform {
		return constant.TaskPlatform(strconv.Itoa(channelType))
	}
	cases := []struct {
		platform  constant.TaskPlatform
		pluginKey string
		native    channel.TaskAdaptor
	}{
		{platform: constant.TaskPlatformSuno, pluginKey: "sunoapi"},
		{platform: platformOf(constant.ChannelTypeAli), pluginKey: "alibaba"},
		{platform: platformOf(constant.ChannelTypeGemini), pluginKey: "google"},
		{platform: platformOf(constant.ChannelTypeMiniMax), pluginKey: "hailuo"},
		{platform: platformOf(constant.ChannelTypeJimeng), pluginKey: "jimeng"},
		{platform: platformOf(constant.ChannelTypeKling), pluginKey: "kling"},
		{platform: platformOf(constant.ChannelTypeVidu), pluginKey: "vidu"},
		{platform: platformOf(constant.ChannelTypeVertexAi), native: &taskvertex.TaskAdaptor{}},
		{platform: platformOf(constant.ChannelTypeDoubaoVideo), native: &taskdoubao.TaskAdaptor{}},
		{platform: platformOf(constant.ChannelTypeVolcEngine), native: &taskdoubao.TaskAdaptor{}},
		{platform: platformOf(constant.ChannelTypeSora), native: &tasksora.TaskAdaptor{}},
		{platform: platformOf(constant.ChannelTypeOpenAI), native: &tasksora.TaskAdaptor{}},
		{platform: platformOf(constant.ChannelTypeAIGCVideo), native: &taskAigc.TaskAdaptor{}},
		{platform: platformOf(constant.ChannelTypeSeedance3rd), native: &taskSeedance3rd.TaskAdaptor{}},
		{platform: platformOf(constant.ChannelTypeVolcPassthrough), native: &volcpassthrough.TaskAdaptor{}},
		{platform: platformOf(constant.ChannelTypeMinimaxImage), native: &taskminimaximage.TaskAdaptor{}},
	}
	for _, tc := range cases {
		t.Run(string(tc.platform), func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/video/generations", nil)

			polled := GetTaskAdaptor(tc.platform)
			platform, submitted := getTaskAdaptorForRequest(c, tc.platform)

			require.NotNil(t, polled)
			require.NotNil(t, submitted)
			assert.Equal(t, tc.platform, platform)
			pinnedValue, pinned := c.Get(pluginruntime.ContextKeyPinnedPlugin)
			if tc.native != nil {
				assert.IsType(t, tc.native, polled)
				assert.IsType(t, tc.native, submitted)
				assert.False(t, pinned, "a native adaptor pins no plugin")
				return
			}
			assert.IsType(t, &jspluginadaptor.TaskAdaptor{}, polled)
			assert.IsType(t, &jspluginadaptor.TaskAdaptor{}, submitted)
			require.True(t, pinned)
			pin, ok := pinnedValue.(pluginruntime.PinnedPlugin)
			require.True(t, ok)
			require.NotNil(t, pin.Plugin)
			assert.Equal(t, tc.pluginKey, pin.Plugin.Meta.Key)
			assert.Same(t, pluginruntime.DefaultRegistry.Generation(), pin.Generation)
		})
	}
}

func TestGetTaskAdaptorUsesPlatformAsThirdPartyPluginKey(t *testing.T) {
	t.Cleanup(func() {
		pluginruntime.DefaultRegistry.Unregister("registry-fallback")
	})
	source := `
export const meta = {apiVersion: 1, key: "registry-fallback", name: "Registry Fallback", version: "1.0.0", author: {name: "Test"}, channelTypes: [1999], models: ["fallback-v1"], fetchMode: "per_task"};
export function buildSubmitRequest(ctx) { return {url: ctx.baseUrl + "/submit"}; }
export function parseSubmitResponse(ctx, resp) { return {taskId: "id", taskData: resp.body}; }
export function buildQueryRequest(ctx) { return {url: ctx.baseUrl + "/tasks/" + ctx.taskId}; }
export function parseTaskResult(ctx, body) { return {taskId: body.id, status: "SUCCESS"}; }
`
	_, err := pluginruntime.DefaultRegistry.Register(source, pluginruntime.Options{})
	require.NoError(t, err)

	adaptor := GetTaskAdaptor(constant.TaskPlatform("registry-fallback"))
	require.NotNil(t, adaptor)
	assert.Equal(t, "Registry Fallback", adaptor.GetChannelName())
}

func TestGetTaskAdaptorReturnsNilForUnknownPlatform(t *testing.T) {
	assert.Nil(t, GetTaskAdaptor(constant.TaskPlatform("missing-task-platform")))
}

func TestGetTaskAdaptorForRequestUsesExactPinnedPlugin(t *testing.T) {
	source := `
export const meta = {apiVersion: 1, key: "pinned-request", name: "Pinned Generation", version: "1.0.0", author: {name: "Test"}, models: ["pinned-v1"], fetchMode: "per_task"};
export function buildSubmitRequest(ctx) { return {url: ctx.baseUrl + "/submit"}; }
export function parseSubmitResponse(ctx) { return {taskId: "one"}; }
export function buildQueryRequest(ctx) { return {url: ctx.baseUrl + "/query"}; }
export function parseTaskResult() { return {status: "SUCCESS"}; }
`
	pinned, err := pluginruntime.NewRegistry().Register(source, pluginruntime.Options{})
	require.NoError(t, err)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/vendor/submit", nil)
	c.Set(pluginruntime.ContextKeyPinnedPlugin, pluginruntime.PinnedPlugin{Plugin: pinned})

	platform, adaptor := getTaskAdaptorForRequest(c, constant.TaskPlatform("missing-task-platform"))
	require.NotNil(t, adaptor)
	assert.Equal(t, constant.TaskPlatform("pinned-request"), platform)
	assert.Equal(t, "Pinned Generation", adaptor.GetChannelName())
}
