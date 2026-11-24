package middleware

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func newTokenAutoGroupsContext() *gin.Context {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	return ctx
}

func TestSetupContextForTokenPreservesCustomAutoGroupsOrder(t *testing.T) {
	ctx := newTokenAutoGroupsContext()
	token := &model.Token{Id: 1, UserId: 2, AutoGroups: `["vip","default"]`}

	require.NoError(t, SetupContextForToken(ctx, token))
	value, ok := common.GetContextKey(ctx, constant.ContextKeyTokenAutoGroups)
	require.True(t, ok)
	assert.Equal(t, []string{"vip", "default"}, value)
}

func TestSetupContextForTokenTreatsStoredEmptyArrayAsInheritance(t *testing.T) {
	ctx := newTokenAutoGroupsContext()
	token := &model.Token{Id: 1, UserId: 2, AutoGroups: `[]`}

	require.NoError(t, SetupContextForToken(ctx, token))
	_, ok := common.GetContextKey(ctx, constant.ContextKeyTokenAutoGroups)
	assert.False(t, ok)
}

func TestSetupContextForTokenMalformedAutoGroupsFailsClosed(t *testing.T) {
	ctx := newTokenAutoGroupsContext()
	token := &model.Token{Id: 1, UserId: 2, AutoGroups: `not-json`}

	require.NoError(t, SetupContextForToken(ctx, token))
	value, ok := common.GetContextKey(ctx, constant.ContextKeyTokenAutoGroups)
	require.True(t, ok)
	assert.Equal(t, []string{}, value)
}

type tokenAuthGroupResult struct {
	status          int
	body            string
	usingGroup      string
	tokenGroup      string
	autoGroups      any
	crossGroupRetry bool
}

func runTokenAuthWithGroups(t *testing.T, usableGroups string, token model.Token) tokenAuthGroupResult {
	t.Helper()
	require.NoError(t, i18n.Init())
	previousDB, previousLogDB, previousRedis, previousMaster := model.DB, model.LOG_DB, common.RedisEnabled, common.IsMasterNode
	previousMainType, previousLogType := common.MainDatabaseType(), common.LogDatabaseType()
	originalUsable := setting.UserUsableGroups2JSONString()
	originalRatios := ratio_setting.GroupRatio2JSONString()
	originalMax := setting.GetMaxTokenAutoGroups()
	db, err := gorm.Open(sqlite.Open("file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Token{}))
	model.DB, common.RedisEnabled, common.IsMasterNode = db, false, false
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	// InitLogDB shares the main database and sets the SQLite column quoting
	// that token lookups by key need.
	t.Setenv("LOG_SQL_DSN", "")
	require.NoError(t, model.InitLogDB())
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(usableGroups))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"vip":2}`))
	require.NoError(t, setting.UpdateMaxTokenAutoGroups("5"))
	t.Cleanup(func() {
		model.DB, model.LOG_DB, common.RedisEnabled, common.IsMasterNode = previousDB, previousLogDB, previousRedis, previousMaster
		common.SetMainDatabaseType(previousMainType)
		common.SetLogDatabaseType(previousLogType)
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(originalUsable))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalRatios))
		require.NoError(t, setting.UpdateMaxTokenAutoGroups(strconv.Itoa(originalMax)))
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	user := model.User{Username: "token-groups-user", Password: "password-placeholder", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Group: "default", AffCode: "token-groups-user"}
	require.NoError(t, db.Create(&user).Error)
	token.UserId, token.Key, token.Name = user.Id, "tokengroupskey", "token-groups"
	token.Status, token.ExpiredTime, token.UnlimitedQuota = common.TokenStatusEnabled, -1, true
	require.NoError(t, db.Create(&token).Error)

	var result tokenAuthGroupResult
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/v1/chat/completions", TokenAuth(), func(c *gin.Context) {
		result.usingGroup = common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
		result.tokenGroup = common.GetContextKeyString(c, constant.ContextKeyTokenGroup)
		result.autoGroups, _ = common.GetContextKey(c, constant.ContextKeyTokenAutoGroups)
		result.crossGroupRetry = common.GetContextKeyBool(c, constant.ContextKeyTokenCrossGroupRetry)
		c.Status(http.StatusNoContent)
	})
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	request.Header.Set("Authorization", "Bearer sk-tokengroupskey")
	request.Header.Set("Accept-Language", "en")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	result.status, result.body = recorder.Code, recorder.Body.String()
	return result
}

func TestTokenAuthRoutesLegacyGroupListAsOrderedAutoGroups(t *testing.T) {
	result := runTokenAuthWithGroups(t, `{"default":"Default","vip":"VIP"}`, model.Token{Group: "vip,default"})

	require.Equal(t, http.StatusNoContent, result.status, result.body)
	assert.Equal(t, "auto", result.usingGroup)
	assert.Equal(t, "auto", result.tokenGroup)
	assert.Equal(t, []string{"vip", "default"}, result.autoGroups)
	assert.True(t, result.crossGroupRetry)
}

func TestTokenAuthOrderedGroupListDoesNotRequireAutoPermission(t *testing.T) {
	result := runTokenAuthWithGroups(t, `{"default":"Default","vip":"VIP"}`, model.Token{Group: "auto", AutoGroups: `["vip","default"]`})

	require.Equal(t, http.StatusNoContent, result.status, result.body)
	assert.Equal(t, "auto", result.usingGroup)
	assert.Equal(t, []string{"vip", "default"}, result.autoGroups)
}

func TestTokenAuthRefusesOrderedGroupListWithoutUsableGroups(t *testing.T) {
	result := runTokenAuthWithGroups(t, `{"default":"Default"}`, model.Token{Group: "auto", AutoGroups: `["vip","revoked"]`})

	assert.Equal(t, http.StatusForbidden, result.status)
	assert.Contains(t, result.body, "None of the groups bound to this token are available")
}

func TestTokenAuthInheritedAutoStillRequiresAutoPermission(t *testing.T) {
	result := runTokenAuthWithGroups(t, `{"default":"Default","vip":"VIP"}`, model.Token{Group: "auto"})

	assert.Equal(t, http.StatusForbidden, result.status)
	assert.Contains(t, result.body, "No permission to access group auto")
}
