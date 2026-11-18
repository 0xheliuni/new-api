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
		stored, err := model.GetChannelById(401, false)
		require.NoError(t, err)
		assert.Equal(t, common.ChannelStatusAutoDisabled, stored.Status)
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
