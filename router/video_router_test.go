package router

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/relay"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestGetOpenAIVideoRouteRendersJimengTask(t *testing.T) {
	gin.SetMode(gin.TestMode)

	previousDB := model.DB
	previousDatabaseType := common.MainDatabaseType()
	previousLogDatabaseType := common.LogDatabaseType()
	previousSQLitePath := common.SQLitePath
	previousMasterNode := common.IsMasterNode
	previousRedisEnabled := common.RedisEnabled
	common.SQLitePath = t.TempDir() + "/router-video.db"
	common.IsMasterNode = false
	common.RedisEnabled = false
	t.Setenv("SQL_DSN", "")
	require.NoError(t, model.InitDB())
	database := model.DB
	require.NoError(t, database.AutoMigrate(&model.User{}, &model.Token{}, &model.Channel{}, &model.Task{}))
	t.Cleanup(func() {
		sqlDB, closeErr := database.DB()
		require.NoError(t, closeErr)
		require.NoError(t, sqlDB.Close())
		model.DB = previousDB
		common.SetDatabaseTypes(previousDatabaseType, previousLogDatabaseType)
		common.SQLitePath = previousSQLitePath
		common.IsMasterNode = previousMasterNode
		common.RedisEnabled = previousRedisEnabled
	})

	require.NoError(t, database.Create(&model.User{
		Id:          91,
		Username:    "jimeng-fetch-user",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Quota:       100,
		Group:       "default",
		AuthVersion: 1,
	}).Error)
	require.NoError(t, database.Create(&model.Token{
		Id:             1,
		UserId:         91,
		Key:            "jimengfetch",
		Status:         common.TokenStatusEnabled,
		Name:           "jimeng fetch",
		ExpiredTime:    -1,
		UnlimitedQuota: true,
	}).Error)
	require.NoError(t, database.Create(&model.Channel{
		Id:     17,
		Type:   constant.ChannelTypeJimeng,
		Key:    "unused",
		Status: common.ChannelStatusEnabled,
		Name:   "jimeng fetch",
		Models: "jimeng_vgfm_t2v_l20",
		Group:  "default",
	}).Error)

	task := &model.Task{
		CreatedAt: 1710000000,
		UpdatedAt: 1710000060,
		TaskID:    "task_jimeng_public",
		Platform:  constant.TaskPlatform("jimeng"),
		UserId:    91,
		Group:     "default",
		ChannelId: 17,
		Status:    model.TaskStatusSuccess,
		Progress:  "100%",
		PrivateData: model.TaskPrivateData{
			ResultURL: "data:video/mp4;base64,ZGF0YQ==",
		},
	}
	task.SetData(map[string]any{
		"code": 10000,
		"data": map[string]any{
			"status":    "done",
			"task_id":   "jimeng-private-1",
			"video_url": "https://cdn.example/video.mp4",
		},
		"message": "success",
	})
	require.NoError(t, database.Create(task).Error)

	engine := gin.New()
	SetVideoRouter(engine)
	SetTaskPluginProtocolRouter(engine)
	request := httptest.NewRequest(http.MethodGet, "/v1/videos/task_jimeng_public", nil)
	request.Header.Set("Authorization", "Bearer sk-jimengfetch")
	recorder := httptest.NewRecorder()

	engine.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var response struct {
		ID          string `json:"id"`
		Object      string `json:"object"`
		Status      string `json:"status"`
		Progress    int    `json:"progress"`
		CreatedAt   int64  `json:"created_at"`
		CompletedAt int64  `json:"completed_at"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.Equal(t, "task_jimeng_public", response.ID)
	assert.Equal(t, "video", response.Object)
	assert.Equal(t, "completed", response.Status)
	assert.Equal(t, 100, response.Progress)
	assert.Equal(t, int64(1710000000), response.CreatedAt)
	assert.Equal(t, int64(1710000060), response.CompletedAt)
	assert.NotContains(t, recorder.Body.String(), "cdn.example")
	assert.NotContains(t, recorder.Body.String(), "jimeng-private-1")

	for _, testCase := range []struct {
		name          string
		authorization string
		query         string
		wantStatus    int
	}{
		{name: "missing credential rejected", wantStatus: http.StatusUnauthorized},
		{name: "access rejected", query: "?access=not-a-video-credential", wantStatus: http.StatusUnauthorized},
		{name: "bearer accepted", authorization: "Bearer sk-jimengfetch", wantStatus: http.StatusOK},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			request := httptest.NewRequest(
				http.MethodGet,
				"/v1/videos/task_jimeng_public/content"+testCase.query,
				nil,
			)
			if testCase.authorization != "" {
				request.Header.Set("Authorization", testCase.authorization)
			}
			recorder := httptest.NewRecorder()
			engine.ServeHTTP(recorder, request)
			assert.Equal(t, testCase.wantStatus, recorder.Code, recorder.Body.String())
			if testCase.wantStatus == http.StatusOK {
				assert.Equal(t, "data", recorder.Body.String())
			}
		})
	}
}

const relayE2EConfig = `{"relay":{"submit":{"path":"/v1/aigc/video/generate","body":{"model":"$.model","prompt":"$.input.prompt","media":"$.input.media","resolution":"$.parameters.resolution","ratio":"$.parameters.ratio","duration":"$.parameters.duration"},"ok":{"path":"code","equals":200},"taskId":"data.id","error":"message"},` +
	`"query":{"path":"/v1/aigc/tasks/{task_id}","ok":{"path":"code","equals":200},"status":"data.task_status","statusMap":{"PENDING":"PENDING","RUNNING":"RUNNING","COMPLETED":"SUCCEEDED","FAILED":"FAILED"},"videoUrl":"data.result.output","usage":{"seconds":"data.usage.duration","resolution":"data.usage.resolution"}}}}`

func TestRelayChannelServesOpenAIAndNativeEntriesThroughPolling(t *testing.T) {
	gin.SetMode(gin.TestMode)
	require.NoError(t, i18n.Init())
	service.InitHttpClient()

	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousDatabaseType, previousLogDatabaseType := common.MainDatabaseType(), common.LogDatabaseType()
	previousSQLitePath, previousMasterNode, previousRedisEnabled := common.SQLitePath, common.IsMasterNode, common.RedisEnabled
	previousFactory := service.GetTaskAdaptorFunc
	previousTaskQueryLimit := constant.TaskQueryLimit
	fetchSetting := system_setting.GetFetchSetting()
	previousFetchSetting := *fetchSetting
	savedBilling := map[string]string{}
	require.NoError(t, config.GlobalConfig.SaveToDB(func(key, value string) error {
		if strings.HasPrefix(key, "billing_setting.") {
			savedBilling[key] = value
		}
		return nil
	}))
	common.SQLitePath = t.TempDir() + "/relay-e2e.db"
	common.IsMasterNode = false
	common.RedisEnabled = false
	// The env loader sets the production default; without it the poller reads
	// LIMIT 0 unfinished tasks.
	constant.TaskQueryLimit = 1000
	// The mock relay serves the finished video on loopback. SSRF protection
	// stays on; the operator setting admits private addresses on any port.
	fetchSetting.AllowPrivateIp = true
	fetchSetting.AllowedPorts = nil
	t.Setenv("SQL_DSN", "")
	require.NoError(t, model.InitDB())
	database := model.DB
	model.LOG_DB = database
	require.NoError(t, database.AutoMigrate(&model.User{}, &model.Token{}, &model.Channel{}, &model.Ability{}, &model.Task{}, &model.Log{},
		&model.UserSubscription{}, &model.SubscriptionPlan{}, &model.SubscriptionPreConsumeRecord{}))
	service.GetTaskAdaptorFunc = func(platform constant.TaskPlatform) service.TaskPollingAdaptor {
		return relay.GetTaskAdaptor(platform)
	}
	t.Cleanup(func() {
		sqlDB, closeErr := database.DB()
		require.NoError(t, closeErr)
		require.NoError(t, sqlDB.Close())
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.SetDatabaseTypes(previousDatabaseType, previousLogDatabaseType)
		common.SQLitePath, common.IsMasterNode, common.RedisEnabled = previousSQLitePath, previousMasterNode, previousRedisEnabled
		service.GetTaskAdaptorFunc = previousFactory
		constant.TaskQueryLimit = previousTaskQueryLimit
		*fetchSetting = previousFetchSetting
		require.NoError(t, config.GlobalConfig.LoadFromDB(savedBilling))
		// SetPluginRouter below installs a route preparer on the shared registry.
		require.NoError(t, jsplugin.DefaultRegistry.SetGenerationPreparer(nil))
	})
	// $0.1 per second with group ratio 1: 5 requested seconds reserve 250000
	// quota per task and 3 reported seconds settle at 150000.
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		"billing_setting.billing_mode": `{"wan3.0-video":"tiered_expr"}`,
		"billing_setting.billing_expr": `{"wan3.0-video":"tier(\"base\", u(\"seconds\") * 0.1)"}`,
	}))

	var mu sync.Mutex
	var submitted []map[string]any
	var queried []string
	relayServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/video.mp4" {
			// Content requests are credentialless.
			assert.Empty(t, r.Header.Get("Authorization"))
			w.Header().Set("Content-Type", "video/mp4")
			_, _ = w.Write([]byte("fake-mp4"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		assert.Equal(t, "Bearer relay-key", r.Header.Get("Authorization"))
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/aigc/video/generate":
			var body map[string]any
			assert.NoError(t, common.DecodeJson(r.Body, &body))
			mu.Lock()
			submitted = append(submitted, body)
			id := fmt.Sprintf("relay-%d", len(submitted))
			mu.Unlock()
			_, _ = w.Write([]byte(`{"code":200,"message":"success","data":{"id":"` + id + `"}}`))
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/aigc/tasks/"):
			mu.Lock()
			queried = append(queried, strings.TrimPrefix(r.URL.Path, "/v1/aigc/tasks/"))
			mu.Unlock()
			_, _ = w.Write([]byte(`{"code":200,"data":{"task_status":"COMPLETED","result":{"output":"http://` + r.Host + `/video.mp4"},"usage":{"duration":3,"resolution":"720P"}}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer relayServer.Close()
	videoURL := relayServer.URL + "/video.mp4"

	const initialQuota = 10_000_000
	require.NoError(t, database.Create(&model.User{Id: 1, Username: "root", Role: common.RoleRootUser, Status: common.UserStatusEnabled, Quota: initialQuota, Group: "default", AuthVersion: 1}).Error)
	require.NoError(t, database.Create(&model.Token{Id: 1, UserId: 1, Key: "relaye2e", Status: common.TokenStatusEnabled, Name: "relay", ExpiredTime: -1, UnlimitedQuota: true}).Error)
	baseURL := relayServer.URL
	setting := `{"task_plugin_key":"alibaba","task_plugin_config":` + relayE2EConfig + `}`
	relayChannel := &model.Channel{Id: 12, Type: constant.ChannelTypeTaskPlugin, Key: "relay-key", Status: common.ChannelStatusEnabled, Name: "relay",
		Models: "wan3.0-video", Group: "default", BaseURL: &baseURL, Setting: &setting, OtherSettings: `{"disable_task_polling_sleep":true}`}
	require.NoError(t, relayChannel.Insert())

	engine := gin.New()
	SetRelayRouter(engine)
	SetTaskPluginProtocolRouter(engine)
	SetVideoRouter(engine)
	SetTaskRouter(engine)
	engine.NoRoute(SetPluginRouter(engine))
	call := func(method, path, body string) (int, string) {
		var reader io.Reader
		if body != "" {
			reader = bytes.NewBufferString(body)
		}
		request := httptest.NewRequest(method, path, reader)
		request.Header.Set("Authorization", "Bearer sk-relaye2e")
		if body != "" {
			request.Header.Set("Content-Type", "application/json")
		}
		recorder := httptest.NewRecorder()
		engine.ServeHTTP(recorder, request)
		return recorder.Code, recorder.Body.String()
	}

	code, body := call(http.MethodPost, "/v1/videos", `{"model":"wan3.0-video","prompt":"a cat","seconds":5,"resolution":"720P"}`)
	require.Equal(t, http.StatusOK, code, body)
	openAIID := gjson.Get(body, "id").String()
	code, body = call(http.MethodPost, "/ali/api/v1/services/aigc/video-generation/video-synthesis", `{"model":"wan3.0-video","input":{"prompt":"a dog"},"parameters":{"resolution":"720P","duration":5}}`)
	require.Equal(t, http.StatusOK, code, body)
	nativeID := gjson.Get(body, "output.task_id").String()
	require.NotEmpty(t, openAIID)
	require.NotEmpty(t, nativeID)

	mu.Lock()
	require.Len(t, submitted, 2, "both client entries reach the relay")
	assert.Equal(t, map[string]any{"model": "wan3.0-video", "prompt": "a cat", "resolution": "720P", "ratio": "adaptive", "duration": float64(5)}, submitted[0])
	assert.Equal(t, "a dog", submitted[1]["prompt"])
	mu.Unlock()
	quota, err := model.GetUserQuota(1, true)
	require.NoError(t, err)
	assert.Equal(t, initialQuota-2*250_000, quota, "each submit reserves 5 seconds")

	service.RunTaskPollingOnce(t.Context(), nil)

	mu.Lock()
	assert.ElementsMatch(t, []string{"relay-1", "relay-2"}, queried, "polling queries the relay task ids")
	mu.Unlock()

	code, body = call(http.MethodGet, "/ali/api/v1/tasks/"+nativeID, "")
	require.Equal(t, http.StatusOK, code, body)
	assert.Equal(t, nativeID, gjson.Get(body, "output.task_id").String())
	assert.Equal(t, "SUCCEEDED", gjson.Get(body, "output.task_status").String())
	assert.Equal(t, videoURL, gjson.Get(body, "output.video_url").String())
	assert.Equal(t, int64(3), gjson.Get(body, "usage.duration").Int())
	assert.Equal(t, int64(3), gjson.Get(body, "usage.output_video_duration").Int())
	assert.True(t, gjson.Get(body, "usage.input_video_duration").Exists(), "input_video_duration must exist")
	assert.Equal(t, int64(0), gjson.Get(body, "usage.input_video_duration").Int())
	assert.Equal(t, "720P", gjson.Get(body, "usage.SR").String())
	assert.False(t, gjson.Get(body, "data").Exists(), "the relay envelope is not persisted")
	assert.False(t, gjson.Get(body, "code").Exists())

	code, body = call(http.MethodGet, "/v1/videos/"+openAIID, "")
	require.Equal(t, http.StatusOK, code, body)
	assert.Equal(t, "completed", gjson.Get(body, "status").String())

	code, body = call(http.MethodGet, "/v1/videos/"+openAIID+"/content", "")
	require.Equal(t, http.StatusOK, code, body)
	assert.Equal(t, "fake-mp4", body)

	// Settlement replaces the requested 5 seconds with the 3 the relay reported
	// and refunds the difference to the wallet. The refund logs carry the
	// settled facts; the task row keeps its submit-time billing snapshot.
	var settled model.Task
	require.NoError(t, database.Where("task_id = ?", openAIID).First(&settled).Error)
	assert.Equal(t, 150_000, settled.Quota)
	var refunds []model.Log
	require.NoError(t, database.Where("type = ?", model.LogTypeRefund).Order("id").Find(&refunds).Error)
	require.Len(t, refunds, 2, "one refund per entry")
	for _, refund := range refunds {
		assert.Equal(t, 100_000, refund.Quota)
		var facts map[string]any
		require.NoError(t, common.UnmarshalJsonStr(gjson.Get(refund.Other, "usage_facts").Raw, &facts))
		assert.Equal(t, map[string]any{"seconds": float64(3), "resolution": "720P"}, facts)
	}
	quota, err = model.GetUserQuota(1, true)
	require.NoError(t, err)
	assert.Equal(t, initialQuota-2*150_000, quota, "each task settles at 3 seconds")
}
