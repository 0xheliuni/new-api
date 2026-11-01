package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/channelsso"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const (
	ssoTestRefreshCookie = "refresh-cookie-value-9f2c"
	ssoTestAccessToken   = "upstream-access-token-71ab"
)

// ssoFakeUpstream mimics the modern upstream new-api endpoints Channel SSO
// talks to. Its only valid password is the one passed to newSSOFakeUpstream.
type ssoFakeUpstream struct {
	URL         string
	mu          sync.Mutex
	loginCalls  int
	logoutCalls int
	// logoutCookies holds the refresh cookie value each logout carried.
	logoutCookies []string
}

func (f *ssoFakeUpstream) counts() (login, logout int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.loginCalls, f.logoutCalls
}

func newSSOFakeUpstream(t *testing.T, password string) *ssoFakeUpstream {
	t.Helper()
	fake := &ssoFakeUpstream{}
	writeJSON := func(w http.ResponseWriter, status int, v any) {
		data, err := common.Marshal(v)
		require.NoError(t, err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write(data)
	}
	user := map[string]any{"id": 3, "username": "upstream-admin", "role": 100}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": map[string]any{"system_name": "Fake Upstream"}})
	})
	mux.HandleFunc("/api/user/login/encryption-key", http.NotFound)
	mux.HandleFunc("/api/user/login", func(w http.ResponseWriter, r *http.Request) {
		fake.mu.Lock()
		fake.loginCalls++
		fake.mu.Unlock()
		var body struct {
			Password string `json:"password"`
		}
		require.NoError(t, common.DecodeJson(r.Body, &body))
		if body.Password != password {
			writeJSON(w, http.StatusOK, map[string]any{"success": false, "message": "wrong password"})
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "new_api_refresh", Value: ssoTestRefreshCookie, Path: "/api/user/auth", HttpOnly: true})
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": map[string]any{"access_token": ssoTestAccessToken, "user": user}})
	})
	mux.HandleFunc("/api/user/auth/refresh", func(w http.ResponseWriter, r *http.Request) {
		if cookie, err := r.Cookie("new_api_refresh"); err != nil || cookie.Value != ssoTestRefreshCookie {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"success": false})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": map[string]any{"access_token": ssoTestAccessToken}})
	})
	mux.HandleFunc("/api/user/self", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+ssoTestAccessToken {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"success": false})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": user})
	})
	mux.HandleFunc("/api/user/auth/logout", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			fake.mu.Lock()
			fake.logoutCalls++
			if cookie, err := r.Cookie("new_api_refresh"); err == nil {
				fake.logoutCookies = append(fake.logoutCookies, cookie.Value)
			}
			fake.mu.Unlock()
		}
		writeJSON(w, http.StatusOK, map[string]any{"success": true})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	fake.URL = srv.URL
	return fake
}

type ssoTestUser struct {
	user     *model.User
	token    string
	identity service.AuthIdentity
}

type ssoTestEnv struct {
	engine    *gin.Engine
	channelID int
	upstream  *ssoFakeUpstream
	rootA     ssoTestUser
	rootB     ssoTestUser
	admin     ssoTestUser
}

// setupChannelSSOTest wires the Channel SSO routes with the same middleware
// chain router/channel-router.go registers, against a SQLite database, the
// in-memory SSO store, and a fake modern upstream reachable over http.
func setupChannelSSOTest(t *testing.T) *ssoTestEnv {
	t.Helper()
	setupSecurityEnrollmentTest(t)
	// The shared fixture leaves the log database open, which keeps its
	// SQLite file locked on Windows and fails TempDir cleanup.
	logDB, err := model.LOG_DB.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = logDB.Close() })
	require.NoError(t, model.DB.AutoMigrate(&model.Channel{}, &model.Ability{}, &model.ChannelSSOCredential{}, &model.ChannelSSOUpstreamSession{}))

	// Restore the process config after t.Setenv has restored the environment.
	t.Cleanup(func() { channelsso.LoadConfig() })
	t.Setenv("SSO_PROXY_DOMAIN", "modex-ai.cloud")
	t.Setenv("SSO_PROXY_SCHEME", "http")
	t.Setenv("SSO_SESSION_TTL", "")
	t.Setenv("CRYPTO_SECRET", "channel-sso-controller-test")
	channelsso.LoadConfig()

	previousFetch := *system_setting.GetFetchSetting()
	previousCritical := common.CriticalRateLimitEnable
	previousSecureCookie := common.SessionCookieSecure
	system_setting.GetFetchSetting().EnableSSRFProtection = true
	system_setting.GetFetchSetting().AllowPrivateIp = true
	system_setting.GetFetchSetting().AllowedPorts = []string{"1-65535"}
	common.CriticalRateLimitEnable = false
	t.Cleanup(func() {
		*system_setting.GetFetchSetting() = previousFetch
		common.CriticalRateLimitEnable = previousCritical
		common.SessionCookieSecure = previousSecureCookie
	})

	channel := &model.Channel{Name: "sso-channel", Status: common.ChannelStatusEnabled, Models: "gpt-4", Group: "default"}
	require.NoError(t, channel.Insert())

	engine := gin.New()
	channelRoute := engine.Group("/api/channel")
	channelRoute.Use(middleware.AdminAuth())
	channelRoute.GET("/sso/status", middleware.RootAuth(), middleware.DisableCache(), GetChannelSSOStatus)
	channelRoute.GET("/:id/sso", middleware.RootAuth(), middleware.DisableCache(), GetChannelSSOConfig)
	channelRoute.PUT("/:id/sso", middleware.RootAuth(), middleware.DisableCache(), SaveChannelSSOConfig)
	channelRoute.POST("/:id/sso/check", middleware.RootAuth(), middleware.CriticalRateLimit(), middleware.DisableCache(), CheckChannelSSO)
	channelRoute.POST("/sso/:sid/validate", middleware.RootAuth(), middleware.DisableCache(), ValidateChannelSSO)
	channelRoute.POST("/sso/:sid/open", middleware.RootAuth(), middleware.DisableCache(), OpenChannelSSO)
	// Registered here (without the Casbin permission middleware the real
	// router applies) only so TestAddChannelReturnsCreatedChannelIds can
	// assert on AddChannel's response shape; it is not otherwise exercised by
	// the SSO flow tests in this file.
	channelRoute.POST("/", AddChannel)
	channelRoute.DELETE("/:id", DeleteChannel)
	channelRoute.POST("/batch", DeleteChannelBatch)

	return &ssoTestEnv{
		engine:    engine,
		channelID: channel.Id,
		upstream:  newSSOFakeUpstream(t, "secret"),
		rootA:     newSSOTestUser(t, "sso-root-a", common.RoleRootUser),
		rootB:     newSSOTestUser(t, "sso-root-b", common.RoleRootUser),
		admin:     newSSOTestUser(t, "sso-admin", common.RoleAdminUser),
	}
}

func newSSOTestUser(t *testing.T, username string, role int) ssoTestUser {
	t.Helper()
	user := &model.User{Username: username, Password: "unused", Role: role, Status: common.UserStatusEnabled, Group: "default", AuthVersion: 1, AffCode: username}
	require.NoError(t, model.DB.Create(user).Error)
	require.NoError(t, model.DB.Create(&model.TwoFA{UserId: user.Id, Secret: "JBSWY3DPEHPK3PXP", IsEnabled: true}).Error)
	require.NoError(t, model.PublishUserAuthCache(user.Id))
	bundle, err := service.CreateLoginSession(user.Id, "password", "127.0.0.1", username)
	require.NoError(t, err)
	identity, err := service.ParseAccessToken(bundle.AccessToken)
	require.NoError(t, err)
	return ssoTestUser{user: user, token: bundle.AccessToken, identity: identity}
}

type ssoResponse struct {
	Success    bool   `json:"success"`
	Message    string `json:"message"`
	MessageKey string `json:"message_key"`
	Code       string `json:"code"`
	Data       struct {
		Configured  bool   `json:"configured"`
		BaseURL     string `json:"base_url"`
		Username    string `json:"username"`
		EntryPath   string `json:"entry_path"`
		HasPassword bool   `json:"has_password"`
		SessionID   string `json:"session_id"`
		Enabled     bool   `json:"enabled"`
		SiteName    string `json:"site_name"`
		Role        int    `json:"role"`
		Reused      bool   `json:"reused"`
		EnterURL    string `json:"enter_url"`
		IDs         []int  `json:"ids"`
	} `json:"data"`
}

func (env *ssoTestEnv) request(t *testing.T, as ssoTestUser, method, path, body, proof string) (*httptest.ResponseRecorder, ssoResponse) {
	t.Helper()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+as.token)
	if proof != "" {
		request.Header.Set("X-Security-Proof", proof)
	}
	env.engine.ServeHTTP(recorder, request)
	var decoded ssoResponse
	_ = common.Unmarshal(recorder.Body.Bytes(), &decoded)
	return recorder, decoded
}

func (env *ssoTestEnv) configPath() string {
	return fmt.Sprintf("/api/channel/%d/sso", env.channelID)
}

func (env *ssoTestEnv) saveConfig(t *testing.T, password string) {
	t.Helper()
	body := fmt.Sprintf(`{"base_url":%q,"username":"upstream-admin","password":%q,"entry_path":""}`, env.upstream.URL, password)
	recorder, response := env.request(t, env.rootA, http.MethodPut, env.configPath(), body, "")
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.True(t, response.Success, recorder.Body.String())
}

func (env *ssoTestEnv) check(t *testing.T, as ssoTestUser) ssoResponse {
	t.Helper()
	recorder, response := env.request(t, as, http.MethodPost, env.configPath()+"/check", "", "")
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.True(t, response.Success, recorder.Body.String())
	return response
}

func TestSSORoutesRequireRoot(t *testing.T) {
	env := setupChannelSSOTest(t)
	for _, route := range []struct{ method, path string }{
		{http.MethodGet, "/api/channel/sso/status"},
		{http.MethodGet, env.configPath()},
		{http.MethodPut, env.configPath()},
		{http.MethodPost, env.configPath() + "/check"},
		{http.MethodPost, "/api/channel/sso/abc/validate"},
		{http.MethodPost, "/api/channel/sso/abc/open"},
	} {
		recorder, _ := env.request(t, env.admin, route.method, route.path, `{}`, "")
		assert.Equal(t, http.StatusForbidden, recorder.Code, "%s %s", route.method, route.path)
	}
}

func TestSSODisabledReturnsNotEnabled(t *testing.T) {
	env := setupChannelSSOTest(t)
	_, status := env.request(t, env.rootA, http.MethodGet, "/api/channel/sso/status", "", "")
	require.True(t, status.Success)
	assert.True(t, status.Data.Enabled)

	t.Setenv("SSO_PROXY_DOMAIN", "")
	channelsso.LoadConfig()
	_, response := env.request(t, env.rootA, http.MethodGet, env.configPath(), "", "")
	assert.False(t, response.Success)
	assert.Equal(t, channelsso.ErrDisabled.Error(), response.MessageKey)
	_, status = env.request(t, env.rootA, http.MethodGet, "/api/channel/sso/status", "", "")
	require.True(t, status.Success, "the status probe itself must not fail when the feature is off")
	assert.False(t, status.Data.Enabled)
}

func TestGetConfigNeverReturnsPassword(t *testing.T) {
	env := setupChannelSSOTest(t)
	env.saveConfig(t, "secret")
	stored, err := model.GetChannelSSOCredential(env.channelID)
	require.NoError(t, err)

	recorder, response := env.request(t, env.rootA, http.MethodGet, env.configPath(), "", "")
	require.True(t, response.Success, recorder.Body.String())
	assert.NotContains(t, recorder.Body.String(), "secret")
	assert.NotContains(t, recorder.Body.String(), stored.PasswordCipher)
	assert.True(t, response.Data.Configured)
	assert.True(t, response.Data.HasPassword)
	assert.Equal(t, env.upstream.URL, response.Data.BaseURL)
	assert.Equal(t, "upstream-admin", response.Data.Username)
	assert.Equal(t, "/dashboard", response.Data.EntryPath)
}

func TestSavePreservesPasswordWhenBlank(t *testing.T) {
	env := setupChannelSSOTest(t)
	env.saveConfig(t, "secret")
	before, err := model.GetChannelSSOCredential(env.channelID)
	require.NoError(t, err)
	env.check(t, env.rootA)
	_, err = model.GetChannelSSOUpstreamSession(env.channelID)
	require.NoError(t, err, "check stores the upstream login state")

	env.saveConfig(t, "")

	after, err := model.GetChannelSSOCredential(env.channelID)
	require.NoError(t, err)
	assert.Equal(t, before.PasswordCipher, after.PasswordCipher)
	_, err = model.GetChannelSSOUpstreamSession(env.channelID)
	assert.Error(t, err, "saving the configuration discards the stored upstream login state")
	_, logoutCalls := env.upstream.counts()
	assert.Equal(t, 1, logoutCalls)

	for _, test := range []struct {
		name, body string
		want       error
	}{
		{"protocol-relative entry path", fmt.Sprintf(`{"base_url":%q,"username":"u","entry_path":"//evil.example"}`, env.upstream.URL), channelsso.ErrInvalidEntryPath},
		{"backslash entry path", fmt.Sprintf(`{"base_url":%q,"username":"u","entry_path":"/\\\\evil.example"}`, env.upstream.URL), channelsso.ErrInvalidEntryPath},
		{"relative entry path", fmt.Sprintf(`{"base_url":%q,"username":"u","entry_path":"console"}`, env.upstream.URL), channelsso.ErrInvalidEntryPath},
		{"tab in entry path", fmt.Sprintf(`{"base_url":%q,"username":"u","entry_path":"/\t/evil.example"}`, env.upstream.URL), channelsso.ErrInvalidEntryPath},
		{"newline in entry path", fmt.Sprintf(`{"base_url":%q,"username":"u","entry_path":"/\r\n/evil.example"}`, env.upstream.URL), channelsso.ErrInvalidEntryPath},
		{"DEL in entry path", fmt.Sprintf(`{"base_url":%q,"username":"u","entry_path":"/\u007f/evil.example"}`, env.upstream.URL), channelsso.ErrInvalidEntryPath},
		{"non-http base url", `{"base_url":"ftp://example.com","username":"u"}`, channelsso.ErrInvalidBaseURL},
		{"missing username", fmt.Sprintf(`{"base_url":%q,"username":" "}`, env.upstream.URL), channelsso.ErrConfigIncomplete},
	} {
		_, response := env.request(t, env.rootA, http.MethodPut, env.configPath(), test.body, "")
		assert.False(t, response.Success, test.name)
		assert.Equal(t, test.want.Error(), response.MessageKey, test.name)
	}
	after, err = model.GetChannelSSOCredential(env.channelID)
	require.NoError(t, err)
	assert.Equal(t, "/dashboard", after.EntryPath, "rejected input leaves the stored configuration unchanged")
}

func TestSaveClearRemovesConfiguration(t *testing.T) {
	env := setupChannelSSOTest(t)
	env.saveConfig(t, "secret")
	sid := env.check(t, env.rootA).Data.SessionID

	_, response := env.request(t, env.rootA, http.MethodPut, env.configPath(), `{"clear":true}`, "")
	require.True(t, response.Success, response.Message)

	_, err := model.GetChannelSSOCredential(env.channelID)
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)
	_, err = model.GetChannelSSOUpstreamSession(env.channelID)
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)
	_, logoutCalls := env.upstream.counts()
	assert.Equal(t, 1, logoutCalls)
	_, response = env.request(t, env.rootA, http.MethodGet, env.configPath(), "", "")
	assert.False(t, response.Data.Configured)
	_, response = env.request(t, env.rootA, http.MethodPost, "/api/channel/sso/"+sid+"/open", "", "")
	assert.Equal(t, "SSO session expired; please start again", response.MessageKey)
}

// TestCheckNeedsRootButNoStepUp pins the access rule for starting single
// sign-on: a super admin starts it without any extra verification, while a
// non-root admin is refused before the upstream is contacted.
func TestCheckNeedsRootButNoStepUp(t *testing.T) {
	env := setupChannelSSOTest(t)
	env.saveConfig(t, "secret")

	recorder, _ := env.request(t, env.admin, http.MethodPost, env.configPath()+"/check", "", "")
	assert.Equal(t, http.StatusForbidden, recorder.Code)
	loginCalls, _ := env.upstream.counts()
	assert.Equal(t, 0, loginCalls)

	response := env.check(t, env.rootA)
	assert.NotEmpty(t, response.Data.SessionID)
	loginCalls, _ = env.upstream.counts()
	assert.Equal(t, 1, loginCalls)
}

// TestCheckLogsOutRejectedStoredSession pins that a stored upstream login
// the upstream no longer accepts is logged out before it is replaced, so
// upstream sessions do not accumulate.
func TestCheckLogsOutRejectedStoredSession(t *testing.T) {
	env := setupChannelSSOTest(t)
	env.saveConfig(t, "secret")
	env.check(t, env.rootA)

	stored, err := model.GetChannelSSOUpstreamSession(env.channelID)
	require.NoError(t, err)
	staleJar := &channelsso.Jar{Cookies: []channelsso.StoredCookie{{Name: "new_api_refresh", Value: "stale-refresh-value", Path: "/api/user/auth", HttpOnly: true}}}
	plaintext, err := staleJar.Marshal()
	require.NoError(t, err)
	stored.CookieJarCipher, err = channelsso.Seal(plaintext)
	require.NoError(t, err)
	require.NoError(t, model.SaveChannelSSOUpstreamSession(stored))

	result := env.check(t, env.rootA)
	assert.False(t, result.Data.Reused)
	loginCalls, _ := env.upstream.counts()
	assert.Equal(t, 2, loginCalls)
	env.upstream.mu.Lock()
	assert.Equal(t, []string{"stale-refresh-value"}, env.upstream.logoutCookies)
	env.upstream.mu.Unlock()
}

func TestCheckReusesStoredSession(t *testing.T) {
	env := setupChannelSSOTest(t)
	env.saveConfig(t, "secret")

	first := env.check(t, env.rootA)
	assert.False(t, first.Data.Reused)
	assert.Equal(t, "Fake Upstream", first.Data.SiteName)
	assert.Equal(t, "upstream-admin", first.Data.Username)
	assert.Equal(t, 100, first.Data.Role)
	assert.NotEmpty(t, first.Data.SessionID)

	second := env.check(t, env.rootA)
	assert.True(t, second.Data.Reused)
	loginCalls, _ := env.upstream.counts()
	assert.Equal(t, 1, loginCalls)
}

func TestSessionBoundToAdmin(t *testing.T) {
	env := setupChannelSSOTest(t)
	env.saveConfig(t, "secret")
	sid := env.check(t, env.rootA).Data.SessionID

	for _, step := range []string{"validate", "open"} {
		_, response := env.request(t, env.rootB, http.MethodPost, "/api/channel/sso/"+sid+"/"+step, "", "")
		assert.False(t, response.Success, step)
		assert.Equal(t, channelsso.ErrSessionForbidden.Error(), response.MessageKey, step)
	}
	_, response := env.request(t, env.rootA, http.MethodPost, "/api/channel/sso/"+sid+"/validate", "", "")
	assert.True(t, response.Success, "the owner can still use the session")
}

func TestOpenRejectsStaleCredential(t *testing.T) {
	env := setupChannelSSOTest(t)
	env.saveConfig(t, "secret")

	// A credential that changes underneath a session (for example while a
	// check was in flight) is caught by the credential version binding.
	sid := env.check(t, env.rootA).Data.SessionID
	stored, err := model.GetChannelSSOCredential(env.channelID)
	require.NoError(t, err)
	require.NoError(t, model.DB.Model(&model.ChannelSSOCredential{}).Where("channel_id = ?", env.channelID).Update("updated_at", stored.UpdatedAt+1).Error)
	_, response := env.request(t, env.rootA, http.MethodPost, "/api/channel/sso/"+sid+"/open", "", "")
	assert.False(t, response.Success)
	assert.Equal(t, channelsso.ErrCredentialChanged.Error(), response.MessageKey)

	// Saving new settings through the API revokes pending sessions outright.
	sid = env.check(t, env.rootA).Data.SessionID
	env.saveConfig(t, "")
	_, response = env.request(t, env.rootA, http.MethodPost, "/api/channel/sso/"+sid+"/open", "", "")
	assert.False(t, response.Success)
	assert.Equal(t, "SSO session expired; please start again", response.MessageKey)
}

func TestOpenWritesAuditWithoutSecrets(t *testing.T) {
	env := setupChannelSSOTest(t)
	password := "Upstr3am-Pa55word"
	env.upstream = newSSOFakeUpstream(t, password)
	env.saveConfig(t, password)
	sid := env.check(t, env.rootA).Data.SessionID
	_, validated := env.request(t, env.rootA, http.MethodPost, "/api/channel/sso/"+sid+"/validate", "", "")
	require.True(t, validated.Success, validated.Message)
	assert.Equal(t, "upstream-admin", validated.Data.Username)
	assert.Equal(t, 100, validated.Data.Role)

	// Opening never contacts the upstream, so switch the proxy origin to the
	// production https scheme here (the fake upstream itself is plain http).
	t.Setenv("SSO_PROXY_SCHEME", "https")
	common.SessionCookieSecure = true
	channelsso.LoadConfig()

	recorder, opened := env.request(t, env.rootA, http.MethodPost, "/api/channel/sso/"+sid+"/open", "", "")
	require.True(t, opened.Success, recorder.Body.String())
	assert.Regexp(t, regexp.MustCompile(`^https://[a-z0-9]{8}\.modex-ai\.cloud/__sso/enter\?ticket=`), opened.Data.EnterURL)
	_, ticket, found := strings.Cut(opened.Data.EnterURL, "ticket=")
	require.True(t, found)
	require.NotEmpty(t, ticket)

	var audit model.AuditLog
	require.NoError(t, model.LOG_DB.Where("action = ?", "channel.sso_open").First(&audit).Error)
	assert.Equal(t, env.rootA.user.Id, audit.UserId)
	assert.Equal(t, "Opened upstream console via channel SSO", audit.Content)
	require.NotNil(t, audit.Other.Op)
	for _, key := range []string{"channel_id", "upstream_base_url", "upstream_username", "upstream_role"} {
		assert.Contains(t, audit.Other.Op.Params, key)
	}

	var all []model.AuditLog
	require.NoError(t, model.LOG_DB.Find(&all).Error)
	serialized, err := common.Marshal(all)
	require.NoError(t, err)
	for _, secret := range []string{password, ticket, ssoTestRefreshCookie, ssoTestAccessToken} {
		assert.NotContains(t, string(serialized), secret)
	}
	assert.NotContains(t, recorder.Body.String(), ssoTestRefreshCookie)

	_, reopened := env.request(t, env.rootA, http.MethodPost, "/api/channel/sso/"+sid+"/open", "", "")
	assert.False(t, reopened.Success, "a session opens at most once")
	_, foreign := env.request(t, env.rootB, http.MethodPost, "/api/channel/sso/"+sid+"/validate", "", "")
	assert.False(t, foreign.Success)

	// The session ID is in the request path; no audit row may carry it.
	require.NoError(t, model.LOG_DB.Find(&all).Error)
	serialized, err = common.Marshal(all)
	require.NoError(t, err)
	assert.NotContains(t, string(serialized), sid)
	var validateAudits []model.AuditLog
	require.NoError(t, model.LOG_DB.Where("action = ?", "channel.sso_validate").Find(&validateAudits).Error)
	assert.Len(t, validateAudits, 2, "validate is audited explicitly, on success and on failure")
}

// TestDeleteChannelSSOCleanupOrdering pins when channel deletion contacts the
// upstream: never while Channel SSO is disabled, never when the deletion
// itself fails, and once (with the stored login state) after it succeeds.
func TestDeleteChannelSSOCleanupOrdering(t *testing.T) {
	env := setupChannelSSOTest(t)
	env.saveConfig(t, "secret")
	env.check(t, env.rootA)
	deletePath := fmt.Sprintf("/api/channel/%d", env.channelID)

	const failDelete = "test:fail_channel_delete"
	require.NoError(t, model.DB.Callback().Delete().Before("gorm:delete").Register(failDelete, func(db *gorm.DB) {
		if db.Statement.Schema != nil && db.Statement.Schema.Table == "channels" {
			_ = db.AddError(fmt.Errorf("forced channel delete failure"))
		}
	}))
	_, response := env.request(t, env.rootA, http.MethodDelete, deletePath, "", "")
	assert.False(t, response.Success)
	_, response = env.request(t, env.rootA, http.MethodPost, "/api/channel/batch", fmt.Sprintf(`{"ids":[%d]}`, env.channelID), "")
	assert.False(t, response.Success)
	require.NoError(t, model.DB.Callback().Delete().Remove(failDelete))
	_, logoutCalls := env.upstream.counts()
	assert.Zero(t, logoutCalls, "a failed deletion must not log the upstream out")
	_, err := model.GetChannelSSOUpstreamSession(env.channelID)
	require.NoError(t, err, "a failed deletion keeps the stored login state")

	_, response = env.request(t, env.rootA, http.MethodDelete, deletePath, "", "")
	require.True(t, response.Success, response.Message)
	_, logoutCalls = env.upstream.counts()
	assert.Equal(t, 1, logoutCalls)
	env.upstream.mu.Lock()
	assert.Equal(t, []string{ssoTestRefreshCookie}, env.upstream.logoutCookies)
	env.upstream.mu.Unlock()

	// With the feature disabled, deletion never contacts the upstream; the
	// model still removes the channel's SSO rows.
	channel := &model.Channel{Name: "sso-channel-2", Status: common.ChannelStatusEnabled, Models: "gpt-4", Group: "default"}
	require.NoError(t, channel.Insert())
	env.channelID = channel.Id
	env.saveConfig(t, "secret")
	env.check(t, env.rootA)
	t.Setenv("SSO_PROXY_DOMAIN", "")
	channelsso.LoadConfig()
	_, response = env.request(t, env.rootA, http.MethodDelete, fmt.Sprintf("/api/channel/%d", channel.Id), "", "")
	require.True(t, response.Success, response.Message)
	_, logoutCalls = env.upstream.counts()
	assert.Equal(t, 1, logoutCalls, "no upstream logout while Channel SSO is disabled")
	_, err = model.GetChannelSSOUpstreamSession(channel.Id)
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)
	_, err = model.GetChannelSSOCredential(channel.Id)
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)
}

// TestAddChannelReturnsCreatedChannelIds pins AddChannel's response shape: the
// frontend channel editor needs the newly created channel's id back (added
// additively in "data") so it can save a Channel SSO configuration entered
// while creating the channel, which GORM only backfills onto the in-memory
// Channel struct, never into the JSON response, unless the handler puts it
// there itself.
func TestAddChannelReturnsCreatedChannelIds(t *testing.T) {
	env := setupChannelSSOTest(t)

	body := `{"mode":"single","channel":{"type":1,"name":"sso-created-channel","key":"sk-test-key","models":"gpt-4","group":"default"}}`
	recorder, response := env.request(t, env.rootA, http.MethodPost, "/api/channel/", body, "")
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.True(t, response.Success, recorder.Body.String())
	require.Len(t, response.Data.IDs, 1, recorder.Body.String())
	assert.Greater(t, response.Data.IDs[0], 0)

	var created model.Channel
	require.NoError(t, model.DB.Where("id = ?", response.Data.IDs[0]).First(&created).Error)
	assert.Equal(t, "sso-created-channel", created.Name)
}
