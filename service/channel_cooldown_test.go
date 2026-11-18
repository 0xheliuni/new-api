package service

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const cooldownTestModel = "cooldown-test-model"

// cooldownTestClock is the fake time the cooldown records read in a test.
type cooldownTestClock struct{ now time.Time }

// setupChannelCooldownTest gives a test an in-memory database holding the
// channels, the memory channel cache, the request policy options and a fake
// clock, and restores every global afterwards.
func setupChannelCooldownTest(t *testing.T, options map[string]string, channels ...model.Channel) *cooldownTestClock {
	t.Helper()
	gin.SetMode(gin.TestMode)
	previousDB, previousType := model.DB, common.MainDatabaseType()
	previousCache, previousRedis := common.MemoryCacheEnabled, common.RedisEnabled
	previousAutoDisable, previousNow := common.AutomaticDisableChannelEnabled, cooldownNow
	previousPolicy := model.CurrentRequestPolicy().Options
	previousAffinity := *operation_setting.GetChannelAffinitySetting()
	common.OptionMapRWMutex.Lock()
	previousOptions := common.OptionMap
	common.OptionMap = maps.Clone(previousPolicy)
	common.OptionMapRWMutex.Unlock()

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Ability{}, &model.Option{}, &model.User{}))
	model.DB = db
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	common.MemoryCacheEnabled, common.RedisEnabled = true, false
	common.AutomaticDisableChannelEnabled = true
	// DisableChannel notifies the root user.
	require.NoError(t, db.Create(&model.User{Username: "cooldown-root", Role: common.RoleRootUser, Status: common.UserStatusEnabled}).Error)
	for i := range channels {
		require.NoError(t, channels[i].Insert())
	}
	model.InitChannelCache()
	require.NoError(t, model.UpdateRequestPolicyOptions(options))
	// Option writes merge a rule JSON into the default rule objects, so publish
	// the committed policy snapshot as the live affinity setting instead, as the
	// options API does on reload.
	*operation_setting.GetChannelAffinitySetting() = model.CurrentRequestPolicy().Affinity
	clock := &cooldownTestClock{now: time.Unix(1_800_000_000, 0)}
	cooldownNow = func() time.Time { return clock.now }
	channelCooldowns.Lock()
	channelCooldowns.entries = make(map[int]*channelCooldown)
	channelCooldowns.Unlock()

	t.Cleanup(func() {
		restore := make(map[string]string, len(options))
		for key := range options {
			restore[key] = previousPolicy[key]
		}
		require.NoError(t, model.UpdateRequestPolicyOptions(restore))
		*operation_setting.GetChannelAffinitySetting() = previousAffinity
		channelCooldowns.Lock()
		channelCooldowns.entries = make(map[int]*channelCooldown)
		channelCooldowns.Unlock()
		cooldownNow = previousNow
		common.AutomaticDisableChannelEnabled = previousAutoDisable
		common.MemoryCacheEnabled, common.RedisEnabled = previousCache, previousRedis
		model.DB = previousDB
		common.SetMainDatabaseType(previousType)
		common.OptionMapRWMutex.Lock()
		common.OptionMap = previousOptions
		common.OptionMapRWMutex.Unlock()
		if previousCache && previousDB != nil &&
			previousDB.Migrator().HasTable(&model.Channel{}) && previousDB.Migrator().HasTable(&model.Ability{}) {
			model.InitChannelCache()
		}
		if sqlDB, err := db.DB(); err == nil {
			require.NoError(t, sqlDB.Close())
		}
	})
	return clock
}

func cooldownTestChannel(id int, priority int64, weight uint) model.Channel {
	autoBan := 1
	return model.Channel{
		Id:       id,
		Type:     constant.ChannelTypeOpenAI,
		Key:      fmt.Sprintf("key-%d", id),
		Status:   common.ChannelStatusEnabled,
		Name:     fmt.Sprintf("channel-%d", id),
		Weight:   &weight,
		Priority: &priority,
		AutoBan:  &autoBan,
		Models:   cooldownTestModel,
		Group:    "default",
	}
}

// cooldownAttempt starts an attempt on channelID in a fresh request context.
func cooldownAttempt(channelID int) *gin.Context {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Set("original_model", cooldownTestModel)
	c.Set("channel_id", channelID)
	RequestPolicy(c).BeginAttempt(&model.Channel{Id: channelID}, "default")
	return c
}

func lastPolicyEvent(c *gin.Context) PolicyEvent {
	events := RequestPolicy(c).Events()
	return events[len(events)-1]
}

func TestClassifyChannelFailure(t *testing.T) {
	setupChannelCooldownTest(t, map[string]string{
		"channel_scheduling_setting.disable_error_codes": "insufficient_quota",
	})
	upstream := func(status int, code string) *types.NewAPIError {
		return types.WithOpenAIError(types.OpenAIError{Message: "upstream failed", Code: code}, status)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	policy := &model.CurrentRequestPolicy().Scheduling
	for _, tc := range []struct {
		name     string
		err      *types.NewAPIError
		canceled bool
		want     channelFailureClass
	}{
		{name: "rate limited", err: upstream(http.StatusTooManyRequests, "rate_limit_exceeded"), want: channelFailureTransient},
		{name: "upstream server error", err: upstream(http.StatusBadGateway, "bad_gateway"), want: channelFailureTransient},
		{name: "gateway timeout is not a cooldown status", err: upstream(http.StatusGatewayTimeout, "timeout"), want: channelFailureIgnored},
		{name: "connection failure", err: types.NewError(errors.New("dial tcp: connection refused"), types.ErrorCodeDoRequestFailed), want: channelFailureTransient},
		{name: "disable status code", err: upstream(http.StatusUnauthorized, "invalid_api_key"), want: channelFailurePermanent},
		{name: "failure error code ignores case", err: upstream(http.StatusTooManyRequests, "INSUFFICIENT_QUOTA"), want: channelFailurePermanent},
		{name: "channel error", err: types.NewError(errors.New("no key"), types.ErrorCodeChannelNoAvailableKey), want: channelFailurePermanent},
		{name: "local error", err: types.NewErrorWithStatusCode(errors.New("bad request body"), types.ErrorCodeInvalidRequest, http.StatusInternalServerError), want: channelFailureIgnored},
		{name: "skip retry error", err: types.WithOpenAIError(types.OpenAIError{Message: "limited", Code: "rate_limit_exceeded"}, http.StatusTooManyRequests, types.ErrOptionWithSkipRetry()), want: channelFailureIgnored},
		{name: "invalid request", err: upstream(http.StatusBadRequest, "invalid_request_error"), want: channelFailureIgnored},
		{name: "client hung up", err: upstream(http.StatusTooManyRequests, "rate_limit_exceeded"), canceled: true, want: channelFailureIgnored},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			if tc.canceled {
				c.Request = c.Request.WithContext(canceled)
			}
			assert.Equal(t, tc.want, classifyChannelFailure(c, tc.err, policy))
		})
	}
}

func TestChannelCooldownBacksOffAndIgnoresLateResults(t *testing.T) {
	clock := setupChannelCooldownTest(t, map[string]string{
		"channel_scheduling_setting.cooldown_enabled":        "true",
		"channel_scheduling_setting.disable_after_cooldowns": "0",
	}, cooldownTestChannel(301, 0, 10))
	channel := types.ChannelError{ChannelId: 301, ChannelName: "channel-301", AutoBan: true}
	limited := types.WithOpenAIError(types.OpenAIError{Message: "slow down", Code: "rate_limit_exceeded"}, http.StatusTooManyRequests)

	inFlight := cooldownAttempt(301)
	var seconds, counts []int
	for range 7 {
		c := cooldownAttempt(301)
		RecordChannelCooldownFailure(c, channel, limited)
		event := lastPolicyEvent(c)
		require.Equal(t, "cooldown_started", event.Decision.Reason)
		seconds = append(seconds, event.CooldownSeconds)
		counts = append(counts, event.CooldownCount)
		clock.now = clock.now.Add(time.Duration(event.CooldownSeconds)*time.Second + time.Second)
	}
	assert.Equal(t, []int{30, 60, 120, 240, 480, 600, 600}, seconds)
	assert.Equal(t, []int{1, 2, 3, 4, 5, 6, 7}, counts)

	RecordChannelCooldownFailure(inFlight, channel, limited)
	assert.Equal(t, "already_cooling", lastPolicyEvent(inFlight).Decision.Reason, "an attempt that began before the latest cooldown is not counted")

	fresh := cooldownAttempt(301)
	RecordChannelCooldownFailure(fresh, channel, limited)
	assert.Equal(t, 8, lastPolicyEvent(fresh).CooldownCount)
	concurrent := cooldownAttempt(301)
	RecordChannelCooldownFailure(concurrent, channel, limited)
	assert.Equal(t, "already_cooling", lastPolicyEvent(concurrent).Decision.Reason, "failures during a cooldown are counted once")

	RecordChannelCooldownSuccess(inFlight, 301)
	assert.Equal(t, []ChannelCooldownStatus{{
		ChannelID: 301, State: "cooling", CooldownUntil: clock.now.Add(600 * time.Second).Unix(), CooldownCount: 8,
		WeightFactor: 0.25, LastStatusCode: http.StatusTooManyRequests, LastErrorCode: "rate_limit_exceeded",
	}}, ChannelCooldownStatuses(), "a late success does not end the cooldown")
	assert.True(t, IsChannelCoolingDown(301))

	clock.now = clock.now.Add(601 * time.Second)
	assert.False(t, IsChannelCoolingDown(301))
	RecordChannelCooldownSuccess(cooldownAttempt(301), 301)
	statuses := ChannelCooldownStatuses()
	require.Len(t, statuses, 1)
	assert.Equal(t, "recovering", statuses[0].State)
	assert.Equal(t, 0.5, statuses[0].WeightFactor)
	RecordChannelCooldownSuccess(cooldownAttempt(301), 301)
	assert.Empty(t, ChannelCooldownStatuses(), "the record goes away once the weight is back to full")

	again := cooldownAttempt(301)
	RecordChannelCooldownFailure(again, channel, limited)
	assert.Equal(t, 30, lastPolicyEvent(again).CooldownSeconds, "a success resets the backoff")
	ClearChannelCooldowns(301)
	assert.Empty(t, ChannelCooldownStatuses())
}

func TestChannelCooldownLimit(t *testing.T) {
	limited := types.WithOpenAIError(types.OpenAIError{Message: "slow down", Code: "rate_limit_exceeded"}, http.StatusTooManyRequests)
	unauthorized := types.WithOpenAIError(types.OpenAIError{Message: "invalid key", Code: "invalid_api_key"}, http.StatusUnauthorized)
	options := map[string]string{
		"channel_scheduling_setting.cooldown_enabled":        "true",
		"channel_scheduling_setting.disable_after_cooldowns": "2",
	}

	t.Run("reaching the limit disables the channel", func(t *testing.T) {
		clock := setupChannelCooldownTest(t, options, cooldownTestChannel(401, 0, 10))
		// DisableChannel runs in the background and notifies the root user as
		// its last step; the notice limit counter shows that it finished.
		root := model.GetRootUser()
		notifyType := formatNotifyType(401, common.ChannelStatusAutoDisabled)
		noticeKeys := func() []string {
			return []string{
				fmt.Sprintf("%d:%s:%s", root.Id, notifyType, time.Now().Format("2006010215")),
				fmt.Sprintf("%d:%s:%s", root.Id, notifyType, time.Now().Add(-time.Hour).Format("2006010215")),
			}
		}
		t.Cleanup(func() {
			for _, key := range noticeKeys() {
				notifyLimitStore.Delete(key)
			}
		})
		channel := types.ChannelError{ChannelId: 401, ChannelName: "channel-401", AutoBan: true}
		RecordChannelCooldownFailure(cooldownAttempt(401), channel, limited)
		clock.now = clock.now.Add(31 * time.Second)
		second := cooldownAttempt(401)
		RecordChannelCooldownFailure(second, channel, limited)
		event := lastPolicyEvent(second)
		assert.Equal(t, "cooldown_disable_requested", event.Decision.Reason)
		assert.Equal(t, 2, event.CooldownCount)
		require.Eventually(t, func() bool {
			for _, key := range noticeKeys() {
				if _, ok := notifyLimitStore.Load(key); ok {
					return true
				}
			}
			return false
		}, 5*time.Second, 20*time.Millisecond)
		require.Eventually(t, func() bool {
			stored, err := model.GetChannelById(401, false)
			return err == nil && stored.Status == common.ChannelStatusAutoDisabled
		}, 5*time.Second, 20*time.Millisecond)
		stored, err := model.GetChannelById(401, false)
		require.NoError(t, err)
		assert.Equal(t, model.ChannelStatusReasonRepeatedCooldowns, stored.GetOtherInfo()["status_reason"])
		assert.Equal(t, []ChannelCooldownStatus{{
			ChannelID: 401, State: "recovering", WeightFactor: 0.25,
			LastStatusCode: http.StatusTooManyRequests, LastErrorCode: "rate_limit_exceeded",
		}}, ChannelCooldownStatuses(), "a channel enabled again starts at a quarter of its weight")
	})

	t.Run("most of the priority level cooling keeps the channel enabled", func(t *testing.T) {
		clock := setupChannelCooldownTest(t, options,
			cooldownTestChannel(411, 0, 10), cooldownTestChannel(412, 0, 10),
			cooldownTestChannel(413, 0, 10), cooldownTestChannel(414, 0, 10))
		channel := types.ChannelError{ChannelId: 411, ChannelName: "channel-411", AutoBan: true}
		RecordChannelCooldownFailure(cooldownAttempt(411), channel, limited)
		clock.now = clock.now.Add(31 * time.Second)
		for _, id := range []int{412, 413} {
			RecordChannelCooldownFailure(cooldownAttempt(id), types.ChannelError{ChannelId: id, ChannelName: fmt.Sprintf("channel-%d", id), AutoBan: true}, limited)
		}
		second := cooldownAttempt(411)
		RecordChannelCooldownFailure(second, channel, limited)
		event := lastPolicyEvent(second)
		assert.Equal(t, "cooldown_upstream_wide", event.Decision.Reason)
		assert.Equal(t, 60, event.CooldownSeconds)
		stored, err := model.GetChannelById(411, false)
		require.NoError(t, err)
		assert.Equal(t, common.ChannelStatusEnabled, stored.Status)
	})

	t.Run("without automatic disable the channel keeps cooling down", func(t *testing.T) {
		clock := setupChannelCooldownTest(t, options, cooldownTestChannel(421, 0, 10))
		common.AutomaticDisableChannelEnabled = false
		channel := types.ChannelError{ChannelId: 421, ChannelName: "channel-421", AutoBan: true}
		RecordChannelCooldownFailure(cooldownAttempt(421), channel, limited)
		clock.now = clock.now.Add(31 * time.Second)
		second := cooldownAttempt(421)
		RecordChannelCooldownFailure(second, channel, unauthorized)
		event := lastPolicyEvent(second)
		assert.Equal(t, "cooldown_started", event.Decision.Reason, "a dead key that cannot be disabled cools down instead")
		assert.Equal(t, 2, event.CooldownCount)
	})

	t.Run("a dead key that can be disabled is left to the automatic disable", func(t *testing.T) {
		setupChannelCooldownTest(t, options, cooldownTestChannel(431, 0, 10))
		c := cooldownAttempt(431)
		before := len(RequestPolicy(c).Events())
		RecordChannelCooldownFailure(c, types.ChannelError{ChannelId: 431, ChannelName: "channel-431", AutoBan: true}, unauthorized)
		assert.Len(t, RequestPolicy(c).Events(), before)
		assert.Empty(t, ChannelCooldownStatuses())
	})
}

func TestPickScheduledChannel(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	var candidates []*model.Channel
	for _, spec := range []struct {
		id       int
		priority int64
	}{{101, 100}, {102, 100}, {103, 100}, {201, 10}} {
		channel := cooldownTestChannel(spec.id, spec.priority, 10)
		candidates = append(candidates, &channel)
	}
	cooling := channelCooldown{until: now.Add(time.Minute), weightFactor: cooldownRampStart}
	coolingLonger := channelCooldown{until: now.Add(2 * time.Minute), weightFactor: cooldownRampStart}
	for _, tc := range []struct {
		name       string
		tried      []int
		health     map[int]channelCooldown
		tierLimit  int
		wantID     int
		wantReason string
	}{
		{name: "an untried channel at the top priority", tierLimit: 1, wantID: 101},
		{name: "one attempt per priority moves down after a try", tried: []int{101}, tierLimit: 1, wantID: 201, wantReason: "priority_limit_reached"},
		{name: "three attempts per priority stay at the top", tried: []int{101}, tierLimit: 3, wantID: 102},
		{name: "a fully tried priority moves down", tried: []int{101, 102, 103}, tierLimit: 3, wantID: 201, wantReason: "priority_limit_reached"},
		{name: "cooling channels are skipped", health: map[int]channelCooldown{101: cooling, 102: cooling}, tierLimit: 3, wantID: 103},
		{name: "a cooling priority falls through without the limit", health: map[int]channelCooldown{101: cooling, 102: cooling, 103: cooling}, tierLimit: 3, wantID: 201},
		{name: "a used up lower priority goes back above the limit", tried: []int{101, 201}, tierLimit: 1, wantID: 102},
		{name: "every untried channel cooling picks the earliest end", tried: []int{201}, health: map[int]channelCooldown{101: coolingLonger, 102: cooling, 103: coolingLonger}, tierLimit: 3, wantID: 102, wantReason: "cooldown_fallback"},
		{name: "every channel tried reuses the top priority", tried: []int{101, 102, 103, 201}, tierLimit: 3, wantID: 101, wantReason: "tried_channel_reused"},
		{name: "every channel tried and cooling reuses the earliest end", tried: []int{101, 102, 103, 201}, health: map[int]channelCooldown{101: coolingLonger, 102: coolingLonger, 103: coolingLonger, 201: cooling}, tierLimit: 3, wantID: 201, wantReason: "tried_channel_reused"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tried := make(map[int]bool)
			for _, id := range tc.tried {
				tried[id] = true
			}
			channel, reason := pickScheduledChannel(candidates, tried, tc.health, now, tc.tierLimit, func() float64 { return 0 })
			require.NotNil(t, channel)
			assert.Equal(t, tc.wantID, channel.Id)
			assert.Equal(t, tc.wantReason, reason)
		})
	}
}

func TestPickWeightedChannelFollowsTheCooldownRamp(t *testing.T) {
	ramping, full, idle := cooldownTestChannel(1, 0, 10), cooldownTestChannel(2, 0, 10), cooldownTestChannel(3, 0, 0)
	health := map[int]channelCooldown{1: {weightFactor: cooldownRampStart}}
	// The ramping channel weighs 2.5 against 10.
	assert.Equal(t, 1, pickWeightedChannel([]*model.Channel{&ramping, &full}, health, func() float64 { return 0.1 }).Id)
	assert.Equal(t, 2, pickWeightedChannel([]*model.Channel{&ramping, &full}, health, func() float64 { return 0.3 }).Id)
	assert.Equal(t, 1, pickWeightedChannel([]*model.Channel{&idle, &ramping}, nil, func() float64 { return 0 }).Id, "a channel of weight 0 only runs when every weight is 0")
	assert.Equal(t, 3, pickWeightedChannel([]*model.Channel{&idle}, nil, func() float64 { return 0.9 }).Id)
}

func TestScheduledSelectionFollowsTheTiers(t *testing.T) {
	setupChannelCooldownTest(t, map[string]string{
		"channel_scheduling_setting.cooldown_enabled":   "true",
		"channel_scheduling_setting.tier_attempt_limit": "3",
	}, cooldownTestChannel(501, 100, 10), cooldownTestChannel(502, 100, 10), cooldownTestChannel(503, 100, 10), cooldownTestChannel(601, 10, 10))
	previousRand := schedulerRandFloat
	schedulerRandFloat = func() float64 { return 0 }
	t.Cleanup(func() { schedulerRandFloat = previousRand })
	limited := types.WithOpenAIError(types.OpenAIError{Message: "slow down", Code: "rate_limit_exceeded"}, http.StatusTooManyRequests)
	RecordChannelCooldownFailure(cooldownAttempt(502), types.ChannelError{ChannelId: 502, ChannelName: "channel-502", AutoBan: true}, limited)

	c := cooldownAttempt(501)
	AppendUsedChannel(c, 501)
	retry := 1
	param := &RetryParam{Ctx: c, TokenGroup: "default", ModelName: cooldownTestModel, Retry: &retry}
	channel, group, err := CacheGetRandomSatisfiedChannel(param)
	require.NoError(t, err)
	assert.Equal(t, "default", group)
	assert.Equal(t, 503, channel.Id, "the retry skips the tried channel and the cooling one at the same priority")

	AppendUsedChannel(c, 503)
	retry = 2
	channel, _, err = CacheGetRandomSatisfiedChannel(param)
	require.NoError(t, err)
	assert.Equal(t, 601, channel.Id, "no usable channel left at the top moves the retry down")

	full := cooldownAttempt(501)
	for _, id := range []int{501, 502, 503} {
		AppendUsedChannel(full, id)
	}
	fullRetry := 3
	channel, _, err = CacheGetRandomSatisfiedChannel(&RetryParam{Ctx: full, TokenGroup: "default", ModelName: cooldownTestModel, Retry: &fullRetry})
	require.NoError(t, err)
	require.Equal(t, 601, channel.Id)
	RequestPolicy(full).BeginAttempt(channel, "default")
	events := RequestPolicy(full).Events()
	route := events[len(events)-2]
	assert.Equal(t, PolicyDecision{Action: "route", Reason: "priority_limit_reached", Source: "routing"}, route.Decision)
	assert.Equal(t, 2, route.Attempt, "the selection event carries the number of the attempt it starts")
	assert.Equal(t, "channel_selected", events[len(events)-1].Decision.Reason)
}

func TestSessionAffinityWithACoolingChannel(t *testing.T) {
	limited := types.WithOpenAIError(types.OpenAIError{Message: "slow down", Code: "rate_limit_exceeded"}, http.StatusTooManyRequests)
	for _, tc := range []struct {
		mode   string
		wantID int
	}{
		{mode: "prefer", wantID: 702},
		{mode: "strict", wantID: 701},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			setupChannelCooldownTest(t, map[string]string{
				"channel_scheduling_setting.cooldown_enabled": "true",
				"channel_affinity_setting.enabled":            "true",
				"channel_affinity_setting.session_mode":       tc.mode,
				"channel_affinity_setting.rules":              `[{"name":"session","model_regex":[".*"],"key_sources":[{"type":"request_header","key":"X-Session"}],"session_mode":"inherit"}]`,
			}, cooldownTestChannel(701, 0, 10), cooldownTestChannel(702, 0, 10))
			newSession := func() *gin.Context {
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
				c.Request.Header.Set("X-Session", "session-1")
				return c
			}
			seed := newSession()
			_, found := GetPreferredChannelByAffinity(seed, cooldownTestModel, "default")
			require.False(t, found)
			seed.Set("channel_id", 701)
			RecordChannelAffinity(seed, 701)
			t.Cleanup(func() { ClearCurrentChannelAffinityCache(seed) })
			RecordChannelCooldownFailure(cooldownAttempt(701), types.ChannelError{ChannelId: 701, ChannelName: "channel-701", AutoBan: true}, limited)

			c := newSession()
			retry := 0
			channel, _, selectErr := SelectChannelForRequest(c, cooldownTestModel, &RetryParam{Ctx: c, TokenGroup: "default", ModelName: cooldownTestModel, Retry: &retry})
			require.Nil(t, selectErr)
			assert.Equal(t, tc.wantID, channel.Id)
			c.Set("channel_id", channel.Id)
			RecordChannelAffinity(c, channel.Id)
			bound, found := GetPreferredChannelByAffinity(newSession(), cooldownTestModel, "default")
			require.True(t, found)
			assert.Equal(t, 701, bound, "skipping a cooling channel keeps the session binding")
			if tc.mode == "prefer" {
				RequestPolicy(c).BeginAttempt(channel, "default")
				events := RequestPolicy(c).Events()
				assert.Equal(t, PolicyDecision{Action: "route", Reason: "session_channel_cooling", Source: "session_rule"}, events[len(events)-2].Decision)
			}
		})
	}
}
