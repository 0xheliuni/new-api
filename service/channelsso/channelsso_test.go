package channelsso

import (
	"context"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	goredis "github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// setupChannelSSOModelTest installs an in-memory SQLite model.DB migrated
// with the tables the data-layer tests below exercise (channels, abilities,
// and the two Channel SSO tables), restoring the previous model.DB on
// cleanup.
func setupChannelSSOModelTest(t *testing.T) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(
		&model.Channel{},
		&model.Ability{},
		&model.ChannelSSOCredential{},
		&model.ChannelSSOUpstreamSession{},
	))

	prevDB := model.DB
	model.DB = db
	t.Cleanup(func() {
		model.DB = prevDB
	})
}

// setTestConfig installs a package-level config and explicit-secret flag for
// the duration of the test, restoring the previous state on cleanup.
func setTestConfig(t *testing.T, cfg Config, explicit bool, secureCookie bool) {
	t.Helper()

	mu.Lock()
	prevConfig := current
	prevExplicit := explicitSecret
	mu.Unlock()
	prevSecure := common.SessionCookieSecure

	mu.Lock()
	current = cfg
	explicitSecret = explicit
	mu.Unlock()
	common.SessionCookieSecure = secureCookie

	t.Cleanup(func() {
		mu.Lock()
		current = prevConfig
		explicitSecret = prevExplicit
		mu.Unlock()
		common.SessionCookieSecure = prevSecure
	})
}

func TestSealOpenRoundTrip(t *testing.T) {
	sealed, err := Seal([]byte("p@ss"))
	require.NoError(t, err)
	assert.NotContains(t, sealed, "p@ss")
	got, err := Open(sealed)
	require.NoError(t, err)
	assert.Equal(t, "p@ss", string(got))
	again, _ := Seal([]byte("p@ss"))
	assert.NotEqual(t, sealed, again) // random nonce
}

func TestOpenRejectsTampered(t *testing.T) {
	sealed, _ := Seal([]byte("x"))
	_, err := Open(sealed[:len(sealed)-2] + "AA")
	assert.Error(t, err)
}

func TestEnabledRequiresSecureCookieForHTTPS(t *testing.T) {
	setTestConfig(t, Config{ProxyDomain: "modex-ai.cloud", Scheme: "https", SessionTTL: 2 * time.Hour}, true /*explicit secret*/, false /*secure cookie*/)
	assert.ErrorIs(t, Enabled(), ErrDisabled)

	setTestConfig(t, Config{ProxyDomain: "modex-ai.cloud", Scheme: "https", SessionTTL: 2 * time.Hour}, true, true)
	assert.NoError(t, Enabled())

	setTestConfig(t, Config{ProxyDomain: "", Scheme: "https"}, true, true)
	assert.ErrorIs(t, Enabled(), ErrDisabled)

	setTestConfig(t, Config{ProxyDomain: "localhost", Scheme: "http"}, false /*no explicit secret*/, false)
	assert.ErrorIs(t, Enabled(), ErrDisabled)
}

func TestProxyOrigin(t *testing.T) {
	setTestConfig(t, Config{ProxyDomain: "modex-ai.cloud", Scheme: "https", SessionTTL: 2 * time.Hour}, true, true)
	assert.Equal(t, "https://ow1ff033.modex-ai.cloud", ProxyOrigin("ow1ff033"))
}

func TestLoadConfigDefaults(t *testing.T) {
	t.Setenv("SSO_PROXY_DOMAIN", "")
	t.Setenv("SSO_PROXY_SCHEME", "")
	t.Setenv("SSO_SESSION_TTL", "")
	t.Setenv("CRYPTO_SECRET", "")
	t.Setenv("SESSION_SECRET", "")

	cfg := LoadConfig()
	assert.Equal(t, "", cfg.ProxyDomain)
	assert.Equal(t, "https", cfg.Scheme)
	assert.Equal(t, 2*time.Hour, cfg.SessionTTL)
	assert.ErrorIs(t, Enabled(), ErrDisabled)
}

func TestLoadConfigInvalidTTLFallsBackToDefault(t *testing.T) {
	t.Setenv("SSO_PROXY_DOMAIN", "modex-ai.cloud")
	t.Setenv("SSO_PROXY_SCHEME", "https")
	t.Setenv("SSO_SESSION_TTL", "not-a-duration")
	t.Setenv("CRYPTO_SECRET", "s3cret")

	cfg := LoadConfig()
	assert.Equal(t, 2*time.Hour, cfg.SessionTTL)

	t.Setenv("SSO_SESSION_TTL", "-5m")
	cfg = LoadConfig()
	assert.Equal(t, 2*time.Hour, cfg.SessionTTL)

	t.Setenv("SSO_SESSION_TTL", "30m")
	cfg = LoadConfig()
	assert.Equal(t, 30*time.Minute, cfg.SessionTTL)
}

// TestCredentialUpsertAndCascadeDelete pins two contracts the data layer
// must hold for every supported database: saving the same channel's
// credential twice upserts in place (one row, UpdatedAt advances) rather
// than accumulating rows, and deleting the owning channel cascades to both
// Channel SSO tables so no orphaned secret survives channel deletion.
func TestCredentialUpsertAndCascadeDelete(t *testing.T) {
	setupChannelSSOModelTest(t)

	channel := &model.Channel{Name: "sso-test", Status: 1, Models: "gpt-4", Group: "default"}
	require.NoError(t, channel.Insert())

	cred := &model.ChannelSSOCredential{
		ChannelId:      channel.Id,
		BaseURL:        "https://upstream.example.com",
		Username:       "admin",
		PasswordCipher: "sealed-v1",
		EntryPath:      "/login",
	}
	require.NoError(t, model.SaveChannelSSOCredential(cred))
	firstUpdatedAt := cred.UpdatedAt

	time.Sleep(2 * time.Millisecond)

	cred2 := &model.ChannelSSOCredential{
		ChannelId:      channel.Id,
		BaseURL:        "https://upstream.example.com",
		Username:       "admin",
		PasswordCipher: "sealed-v2",
		EntryPath:      "/login",
	}
	require.NoError(t, model.SaveChannelSSOCredential(cred2))

	var count int64
	require.NoError(t, model.DB.Model(&model.ChannelSSOCredential{}).Where("channel_id = ?", channel.Id).Count(&count).Error)
	assert.Equal(t, int64(1), count, "upsert must not accumulate rows for the same channel_id")

	stored, err := model.GetChannelSSOCredential(channel.Id)
	require.NoError(t, err)
	assert.Equal(t, "sealed-v2", stored.PasswordCipher)
	assert.Greater(t, stored.UpdatedAt, firstUpdatedAt)

	session := &model.ChannelSSOUpstreamSession{
		ChannelId:       channel.Id,
		CookieJarCipher: "sealed-cookies",
		UserSnapshot:    `{"name":"admin"}`,
		UpstreamKind:    "modern",
	}
	require.NoError(t, model.SaveChannelSSOUpstreamSession(session))

	require.NoError(t, channel.Delete())

	_, err = model.GetChannelSSOCredential(channel.Id)
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)

	_, err = model.GetChannelSSOUpstreamSession(channel.Id)
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)
}

// allowPrivateUpstreamAccess enables SSRF access to private addresses and
// any port for the duration of the test, restoring the previous
// fetch_setting on cleanup. The fake upstream used by the tests below runs
// on 127.0.0.1 at a random port, which the default fetch_setting (SSRF
// protection on, private IPs disallowed) would otherwise reject.
func allowPrivateUpstreamAccess(t *testing.T) {
	t.Helper()
	original := *system_setting.GetFetchSetting()
	system_setting.GetFetchSetting().EnableSSRFProtection = true
	system_setting.GetFetchSetting().AllowPrivateIp = true
	system_setting.GetFetchSetting().AllowedPorts = []string{"1-65535"}
	t.Cleanup(func() { *system_setting.GetFetchSetting() = original })
}

// fakeCapture records what the fake upstream observed while handling a
// request, for assertions that need to inspect the request rather than
// just the response (the decrypted password, the Origin header, and how
// many times /api/user/login was called).
type fakeCapture struct {
	mu                sync.Mutex
	decryptedPassword string
	origin            string
	loginCalls        int
	logoutRequests    []string
}

func (c *fakeCapture) get() (password, origin string, loginCalls int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.decryptedPassword, c.origin, c.loginCalls
}

// fakeOpts configures newFakeUpstream's behavior.
type fakeOpts struct {
	// Encryption makes /api/user/login/encryption-key report a real key
	// (generated fresh for this server) instead of 404.
	Encryption bool
	// Challenge makes /api/user/login answer with a
	// service.StartLoginVerification-shaped two-step challenge.
	Challenge bool
	// WrongPassword makes /api/user/login answer {success:false} with a
	// wrong-password message, regardless of what password was sent.
	WrongPassword bool
	// EncryptionKeyFailStatus, when non-zero, makes
	// /api/user/login/encryption-key answer with this HTTP status and an
	// empty body instead of its normal response, to simulate the endpoint
	// itself failing (as opposed to reporting encryption disabled).
	EncryptionKeyFailStatus int
	Capture                 *fakeCapture
}

// newFakeUpstream starts an httptest server that mimics the real upstream
// new-api endpoints Upstream talks to (controller/user.go for the modern
// shapes, v1.0.0-rc.21:controller/user.go for the legacy one), for kind
// "modern" or "legacy". The only valid credentials it accepts are
// username/password "secret" (any username).
func newFakeUpstream(t *testing.T, kind string, opts fakeOpts) *httptest.Server {
	t.Helper()
	if opts.Encryption {
		pemKey, err := common.GeneratePasswordEncryptionPrivateKey()
		require.NoError(t, err)
		require.NoError(t, common.LoadPasswordEncryptionPrivateKey(pemKey))
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		writeFakeJSON(w, map[string]any{"success": true, "data": map[string]any{"system_name": "Fake Upstream"}})
	})
	mux.HandleFunc("/api/user/login/encryption-key", func(w http.ResponseWriter, r *http.Request) {
		if opts.EncryptionKeyFailStatus != 0 {
			w.WriteHeader(opts.EncryptionKeyFailStatus)
			return
		}
		if !opts.Encryption {
			http.NotFound(w, r)
			return
		}
		kid, publicKey := common.PasswordEncryptionPublicKey()
		writeFakeJSON(w, map[string]any{"success": true, "data": map[string]any{
			"enabled": true, "kid": kid, "public_key": publicKey,
		}})
	})
	mux.HandleFunc("/api/user/login", func(w http.ResponseWriter, r *http.Request) {
		if opts.Capture != nil {
			opts.Capture.mu.Lock()
			opts.Capture.origin = r.Header.Get("Origin")
			opts.Capture.loginCalls++
			opts.Capture.mu.Unlock()
		}
		var body struct {
			Username          string `json:"username"`
			Password          string `json:"password"`
			PasswordEncrypted string `json:"password_encrypted"`
			EncryptionKeyID   string `json:"encryption_key_id"`
		}
		require.NoError(t, common.DecodeJson(r.Body, &body))

		if opts.Challenge {
			writeFakeJSON(w, map[string]any{"success": true, "data": map[string]any{
				"require_verification": true,
				"flow_token":           "flow-token-value",
				"expires_at":           time.Now().Add(5 * time.Minute).Unix(),
				"methods":              []any{},
			}})
			return
		}

		plainPassword := body.Password
		if opts.Encryption {
			decrypted, err := common.DecryptPassword(body.PasswordEncrypted, body.EncryptionKeyID)
			if err != nil {
				writeFakeJSON(w, map[string]any{"success": false, "message": "invalid encrypted password"})
				return
			}
			plainPassword = decrypted
		}
		if opts.Capture != nil {
			opts.Capture.mu.Lock()
			opts.Capture.decryptedPassword = plainPassword
			opts.Capture.mu.Unlock()
		}

		if opts.WrongPassword || plainPassword != "secret" {
			writeFakeJSON(w, map[string]any{"success": false, "message": "用户名或密码错误"})
			return
		}

		if kind == "modern" {
			http.SetCookie(w, &http.Cookie{Name: "new_api_refresh", Value: "refresh-token-value", Path: "/api/user/auth", HttpOnly: true})
			http.SetCookie(w, &http.Cookie{Name: "new_api_has_session", Value: "1", Path: "/"})
			writeFakeJSON(w, map[string]any{"success": true, "data": map[string]any{
				"access_token": "access-token-value",
				"user":         map[string]any{"id": 1, "username": body.Username, "role": 100},
			}})
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "session", Value: "legacy-session-value", Path: "/"})
		writeFakeJSON(w, map[string]any{"success": true, "data": map[string]any{
			"id": 1, "username": body.Username, "role": 100,
		}})
	})
	mux.HandleFunc("/api/user/auth/refresh", func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("new_api_refresh")
		if err != nil || cookie.Value == "" {
			w.WriteHeader(http.StatusUnauthorized)
			writeFakeJSON(w, map[string]any{"success": false, "message": "unauthorized"})
			return
		}
		writeFakeJSON(w, map[string]any{"success": true, "data": map[string]any{"access_token": "refreshed-access-token"}})
	})
	mux.HandleFunc("/api/user/self", func(w http.ResponseWriter, r *http.Request) {
		if kind == "modern" {
			if r.Header.Get("Authorization") == "" {
				w.WriteHeader(http.StatusUnauthorized)
				writeFakeJSON(w, map[string]any{"success": false, "message": "unauthorized"})
				return
			}
		} else {
			cookie, cookieErr := r.Cookie("session")
			if r.Header.Get("New-Api-User") == "" || cookieErr != nil || cookie.Value == "" {
				w.WriteHeader(http.StatusUnauthorized)
				writeFakeJSON(w, map[string]any{"success": false, "message": "unauthorized"})
				return
			}
		}
		writeFakeJSON(w, map[string]any{"success": true, "data": map[string]any{"id": 1, "username": "u", "role": 100}})
	})

	recordLogout := func(w http.ResponseWriter, r *http.Request) {
		if opts.Capture != nil {
			opts.Capture.mu.Lock()
			opts.Capture.logoutRequests = append(opts.Capture.logoutRequests, r.Method+" "+r.URL.Path)
			opts.Capture.mu.Unlock()
		}
		writeFakeJSON(w, map[string]any{"success": true})
	}
	mux.HandleFunc("/api/user/auth/logout", recordLogout)
	mux.HandleFunc("/api/user/logout", recordLogout)

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func writeFakeJSON(w http.ResponseWriter, v any) {
	data, err := common.Marshal(v)
	if err != nil {
		panic(err)
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(data)
}

func jarHasCookie(jar *Jar, name, value string) bool {
	for _, c := range jar.Cookies {
		if c.Name == name && c.Value == value {
			return true
		}
	}
	return false
}

func TestJarAppliesCookieDeletion(t *testing.T) {
	j := &Jar{}
	j.Apply([]*http.Cookie{{Name: "new_api_refresh", Value: "r1", Path: "/api/user/auth"}})
	assert.Equal(t, "new_api_refresh=r1", j.Header("/api/user/auth/refresh"))
	assert.Equal(t, "", j.Header("/api/status"))
	j.Apply([]*http.Cookie{{Name: "new_api_refresh", Value: "", Path: "/api/user/auth", MaxAge: -1}})
	assert.Equal(t, "", j.Header("/api/user/auth/refresh"))
}

func TestLoginModernWithEncryption(t *testing.T) {
	allowPrivateUpstreamAccess(t)
	setTestConfig(t, Config{Scheme: "http"}, true, true)

	capture := &fakeCapture{}
	srv := newFakeUpstream(t, "modern", fakeOpts{Encryption: true, Capture: capture})

	up, err := NewUpstream(srv.URL)
	require.NoError(t, err)

	jar := &Jar{}
	res, err := up.Login(context.Background(), jar, "u", "secret")
	require.NoError(t, err)

	decrypted, origin, _ := capture.get()
	assert.Equal(t, "secret", decrypted)
	assert.Equal(t, srv.URL, origin)

	assert.Equal(t, "modern", res.Kind)
	assert.NotEmpty(t, res.AccessToken)
	assert.Equal(t, 100, res.User.Role)
	assert.True(t, jarHasCookie(jar, "new_api_refresh", "refresh-token-value"), "jar must contain new_api_refresh")
}

func TestLoginLegacyPlainPassword(t *testing.T) {
	allowPrivateUpstreamAccess(t)
	setTestConfig(t, Config{Scheme: "http"}, true, true)

	capture := &fakeCapture{}
	srv := newFakeUpstream(t, "legacy", fakeOpts{Capture: capture})

	up, err := NewUpstream(srv.URL)
	require.NoError(t, err)

	jar := &Jar{}
	res, err := up.Login(context.Background(), jar, "u", "secret")
	require.NoError(t, err)

	plainPassword, _, _ := capture.get()
	assert.Equal(t, "secret", plainPassword)

	assert.Equal(t, "legacy", res.Kind)
	assert.Empty(t, res.AccessToken)
	assert.True(t, jarHasCookie(jar, "session", "legacy-session-value"), "jar must contain session")

	user, err := up.Self(context.Background(), jar, "legacy", "", res.User.Id)
	require.NoError(t, err)
	assert.Equal(t, res.User.Id, user.Id)
}

func TestLoginChallengeIsReported(t *testing.T) {
	allowPrivateUpstreamAccess(t)
	setTestConfig(t, Config{Scheme: "http"}, true, true)

	srv := newFakeUpstream(t, "modern", fakeOpts{Challenge: true})
	up, err := NewUpstream(srv.URL)
	require.NoError(t, err)

	_, err = up.Login(context.Background(), &Jar{}, "u", "secret")
	assert.ErrorIs(t, err, ErrChallengeRequired)
}

func TestLoginWrongPasswordPassesMessage(t *testing.T) {
	allowPrivateUpstreamAccess(t)
	setTestConfig(t, Config{Scheme: "http"}, true, true)

	srv := newFakeUpstream(t, "modern", fakeOpts{WrongPassword: true})
	up, err := NewUpstream(srv.URL)
	require.NoError(t, err)

	_, err = up.Login(context.Background(), &Jar{}, "u", "secret")
	require.Error(t, err)
	var upstreamErr *UpstreamError
	require.ErrorAs(t, err, &upstreamErr)
	assert.Equal(t, "用户名或密码错误", upstreamErr.Message)
}

// TestLoginEncryptionKeyServerErrorAbortsLogin pins that a failure of the
// encryption-key check itself (as opposed to the key being absent/disabled)
// must never fall back to sending the password in the clear: Login must
// return an error and never reach /api/user/login at all.
func TestLoginEncryptionKeyServerErrorAbortsLogin(t *testing.T) {
	allowPrivateUpstreamAccess(t)
	setTestConfig(t, Config{Scheme: "http"}, true, true)

	capture := &fakeCapture{}
	srv := newFakeUpstream(t, "modern", fakeOpts{EncryptionKeyFailStatus: http.StatusInternalServerError, Capture: capture})
	up, err := NewUpstream(srv.URL)
	require.NoError(t, err)

	_, err = up.Login(context.Background(), &Jar{}, "u", "secret")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrSiteUnreachable)

	_, _, loginCalls := capture.get()
	assert.Equal(t, 0, loginCalls, "Login must not call /api/user/login when the encryption-key check fails")
}

func TestRefreshReusesJar(t *testing.T) {
	allowPrivateUpstreamAccess(t)
	setTestConfig(t, Config{Scheme: "http"}, true, true)

	capture := &fakeCapture{}
	srv := newFakeUpstream(t, "modern", fakeOpts{Capture: capture})
	up, err := NewUpstream(srv.URL)
	require.NoError(t, err)

	jar := &Jar{}
	jar.Apply([]*http.Cookie{{Name: "new_api_refresh", Value: "existing-refresh", Path: "/api/user/auth", HttpOnly: true}})

	token, err := up.Refresh(context.Background(), jar)
	require.NoError(t, err)
	assert.NotEmpty(t, token)

	_, _, loginCalls := capture.get()
	assert.Equal(t, 0, loginCalls, "Refresh must not call /api/user/login")
}

// TestLogoutUsesKindSpecificEndpoint pins the logout endpoint per upstream
// generation: a legacy upstream only has GET /api/user/logout.
func TestLogoutUsesKindSpecificEndpoint(t *testing.T) {
	allowPrivateUpstreamAccess(t)
	setTestConfig(t, Config{Scheme: "http"}, true, true)

	for kind, want := range map[string]string{
		"modern": "POST /api/user/auth/logout",
		"legacy": "GET /api/user/logout",
	} {
		capture := &fakeCapture{}
		srv := newFakeUpstream(t, kind, fakeOpts{Capture: capture})
		up, err := NewUpstream(srv.URL)
		require.NoError(t, err)
		require.NoError(t, up.Logout(context.Background(), &Jar{}, kind), kind)
		capture.mu.Lock()
		assert.Equal(t, []string{want}, capture.logoutRequests, kind)
		capture.mu.Unlock()
	}
}

func TestUpstreamRejectsPrivateAddress(t *testing.T) {
	setTestConfig(t, Config{Scheme: "http"}, true, true)
	original := *system_setting.GetFetchSetting()
	system_setting.GetFetchSetting().EnableSSRFProtection = true
	system_setting.GetFetchSetting().AllowPrivateIp = false
	// Use an allowed port so the only possible rejection reason is the
	// private-IP check itself; port 1 (outside the default allowlist) would
	// make this test pass even if the private-IP check were broken.
	system_setting.GetFetchSetting().AllowedPorts = []string{"1-65535"}
	t.Cleanup(func() { *system_setting.GetFetchSetting() = original })

	_, err := NewUpstream("http://127.0.0.1:80")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "private IP address not allowed")
}

// --- Task 4: session, ticket, and host-label store ---

// resetChannelSSOStore points the package at either the in-process memory
// store or a fresh miniredis instance, resets nowFunc to the real clock,
// and clears the shared memory-store maps, restoring everything on
// cleanup. Using the same package-level mem/nowFunc across tests is safe
// because every test resets them before running. It returns the
// miniredis server when useRedis is true (nil otherwise) so a test that
// needs to simulate real-time Redis key expiry (as opposed to the
// logical, nowFunc-driven expiry channelsso itself enforces) can call its
// FastForward.
func resetChannelSSOStore(t *testing.T, useRedis bool) *miniredis.Miniredis {
	t.Helper()
	prevRedisEnabled := common.RedisEnabled
	prevRDB := common.RDB
	prevNow := nowFunc

	common.RedisEnabled = useRedis
	var srv *miniredis.Miniredis
	if useRedis {
		srv = miniredis.RunT(t)
		client := goredis.NewClient(&goredis.Options{Addr: srv.Addr()})
		require.NoError(t, client.Ping(context.Background()).Err())
		common.RDB = client
		t.Cleanup(func() { _ = client.Close() })
	}
	nowFunc = time.Now

	mem.mu.Lock()
	mem.sessions = map[string]*Session{}
	mem.labels = map[string]string{}
	mem.tickets = map[string]ticketRecord{}
	mem.channels = map[int]map[string]struct{}{}
	mem.mu.Unlock()

	t.Cleanup(func() {
		common.RedisEnabled = prevRedisEnabled
		common.RDB = prevRDB
		nowFunc = prevNow
	})
	return srv
}

// advanceClock moves nowFunc forward by d relative to its current value,
// so tests can drive expiry deterministically without sleeping.
func advanceClock(d time.Duration) {
	cur := nowFunc()
	nowFunc = func() time.Time { return cur.Add(d) }
}

// withEachBackend runs run once against the in-process memory store and
// once against a real (miniredis-backed) Redis store, per task-4-brief.md:
// "内存实现与 Redis 实现跑同一组用例". The Redis subtest still honors the
// ambient common.RedisEnabled gate (skipping if some earlier state left
// Redis unavailable) even though miniredis itself needs no external
// service.
func withEachBackend(t *testing.T, run func(t *testing.T)) {
	t.Helper()
	ambientRedisEnabled := common.RedisEnabled

	t.Run("memory", func(t *testing.T) {
		resetChannelSSOStore(t, false)
		run(t)
	})
	t.Run("redis", func(t *testing.T) {
		if !ambientRedisEnabled {
			t.Skip("redis disabled in this environment")
		}
		resetChannelSSOStore(t, true)
		run(t)
	})
}

func newPendingTestSession(t *testing.T, channelID int) *Session {
	t.Helper()
	s := &Session{
		ChannelID:           channelID,
		AdminUserID:         7,
		BaseURL:             "https://upstream.example.com",
		EntryPath:           "/console",
		CredentialUpdatedAt: 1234,
		UpstreamKind:        "modern",
		User:                UpstreamUser{Id: 1, Username: "admin", Role: 100, Raw: []byte(`{"id":1,"username":"admin","role":100}`)},
	}
	require.NoError(t, CreatePendingSession(s))
	return s
}

func TestTicketSingleUseAndBoundToLabel(t *testing.T) {
	withEachBackend(t, func(t *testing.T) {
		setTestConfig(t, Config{ProxyDomain: "modex-ai.cloud", Scheme: "https", SessionTTL: 2 * time.Hour}, true, true)

		pending := newPendingTestSession(t, 1)
		activated, err := ActivateSession(pending.ID)
		require.NoError(t, err)

		ticket, err := IssueTicket(activated.ID, activated.HostLabel)
		require.NoError(t, err)
		assert.NotEmpty(t, ticket)

		got, err := ConsumeTicket(ticket, activated.HostLabel)
		require.NoError(t, err)
		assert.Equal(t, activated.ID, got.ID)
		assert.Equal(t, activated.User, got.User, "UpstreamUser, including Raw, must round-trip intact")

		_, err = ConsumeTicket(ticket, activated.HostLabel)
		assert.ErrorIs(t, err, ErrTicketInvalid, "a ticket must be usable at most once")

		ticket2, err := IssueTicket(activated.ID, activated.HostLabel)
		require.NoError(t, err)
		_, err = ConsumeTicket(ticket2, "wrong-label-000")
		assert.ErrorIs(t, err, ErrTicketInvalid, "a ticket must be bound to the label it was issued for")

		// Consuming with the wrong label still burns the ticket.
		_, err = ConsumeTicket(ticket2, activated.HostLabel)
		assert.ErrorIs(t, err, ErrTicketInvalid, "a ticket is single-use even when a prior consume used the wrong label")
	})
}

func TestTicketExpires(t *testing.T) {
	withEachBackend(t, func(t *testing.T) {
		setTestConfig(t, Config{ProxyDomain: "modex-ai.cloud", Scheme: "https", SessionTTL: 2 * time.Hour}, true, true)

		pending := newPendingTestSession(t, 1)
		activated, err := ActivateSession(pending.ID)
		require.NoError(t, err)

		ticket, err := IssueTicket(activated.ID, activated.HostLabel)
		require.NoError(t, err)

		advanceClock(61 * time.Second)

		_, err = ConsumeTicket(ticket, activated.HostLabel)
		assert.ErrorIs(t, err, ErrTicketInvalid)
	})
}

func TestActivateAssignsUniqueLabel(t *testing.T) {
	withEachBackend(t, func(t *testing.T) {
		setTestConfig(t, Config{ProxyDomain: "modex-ai.cloud", Scheme: "https", SessionTTL: 2 * time.Hour}, true, true)

		s1 := newPendingTestSession(t, 1)
		a1, err := ActivateSession(s1.ID)
		require.NoError(t, err)
		assert.Regexp(t, `^[a-z0-9]{8}$`, a1.HostLabel)
		assert.True(t, a1.Active)

		s2 := newPendingTestSession(t, 2)
		a2, err := ActivateSession(s2.ID)
		require.NoError(t, err)
		assert.Regexp(t, `^[a-z0-9]{8}$`, a2.HostLabel)
		assert.NotEqual(t, a1.HostLabel, a2.HostLabel, "two sessions must not share a host label")

		got, err := GetSessionByLabel(a1.HostLabel)
		require.NoError(t, err)
		assert.Equal(t, a1.ID, got.ID)

		advanceClock(2*time.Hour + time.Second)
		_, err = GetSessionByLabel(a1.HostLabel)
		assert.ErrorIs(t, err, ErrSessionNotFound, "an activated session must expire after its SessionTTL")
	})
}

func TestRevokeChannelSessions(t *testing.T) {
	withEachBackend(t, func(t *testing.T) {
		setTestConfig(t, Config{ProxyDomain: "modex-ai.cloud", Scheme: "https", SessionTTL: 2 * time.Hour}, true, true)

		s1 := newPendingTestSession(t, 42)
		a1, err := ActivateSession(s1.ID)
		require.NoError(t, err)

		s2 := newPendingTestSession(t, 42)
		a2, err := ActivateSession(s2.ID)
		require.NoError(t, err)

		other := newPendingTestSession(t, 99)
		otherActivated, err := ActivateSession(other.ID)
		require.NoError(t, err)

		require.NoError(t, RevokeChannelSessions(42))

		_, err = GetSession(a1.ID)
		assert.ErrorIs(t, err, ErrSessionNotFound)
		_, err = GetSession(a2.ID)
		assert.ErrorIs(t, err, ErrSessionNotFound)
		_, err = GetSessionByLabel(a1.HostLabel)
		assert.ErrorIs(t, err, ErrSessionNotFound, "revoked sessions must not remain reachable by label")
		_, err = GetSessionByLabel(a2.HostLabel)
		assert.ErrorIs(t, err, ErrSessionNotFound)

		gotOther, err := GetSession(otherActivated.ID)
		require.NoError(t, err)
		assert.Equal(t, otherActivated.ID, gotOther.ID, "revoking one channel must not affect another channel's sessions")
	})
}

// --- Task 4 review fix round 1 ---

func TestActivateSessionRejectsSecondActivation(t *testing.T) {
	withEachBackend(t, func(t *testing.T) {
		setTestConfig(t, Config{ProxyDomain: "modex-ai.cloud", Scheme: "https", SessionTTL: 2 * time.Hour}, true, true)

		pending := newPendingTestSession(t, 5)
		first, err := ActivateSession(pending.ID)
		require.NoError(t, err)

		_, err = ActivateSession(pending.ID)
		assert.ErrorIs(t, err, ErrSessionAlreadyActive, "a session must not be re-activated once active")

		again, err := GetSession(pending.ID)
		require.NoError(t, err)
		assert.Equal(t, first.HostLabel, again.HostLabel, "a rejected re-activation must not assign a new label")
		assert.Equal(t, first.ExpiresAt, again.ExpiresAt, "a rejected re-activation must not slide the fixed TTL forward")

		got, err := GetSessionByLabel(first.HostLabel)
		require.NoError(t, err)
		assert.Equal(t, pending.ID, got.ID, "the original label must still resolve to the session")
	})
}

func TestActivateSessionFailsAfterRevoke(t *testing.T) {
	withEachBackend(t, func(t *testing.T) {
		setTestConfig(t, Config{ProxyDomain: "modex-ai.cloud", Scheme: "https", SessionTTL: 2 * time.Hour}, true, true)

		pending := newPendingTestSession(t, 6)
		require.NoError(t, RevokeChannelSessions(6))

		_, err := ActivateSession(pending.ID)
		assert.ErrorIs(t, err, ErrSessionNotFound, "activating a revoked session must fail, not resurrect it")
	})
}

// TestChannelIndexSurvivesLongSessionTTL pins the fix for review finding 2:
// the per-channel Redis index key used to carry a flat 24h TTL regardless
// of the configured SSO_SESSION_TTL, so a deployment with a longer
// SessionTTL could have the index key evicted by Redis while the session
// it points to was still logically alive, making RevokeChannelSessions
// silently find nothing. channelIndexExpiry now floors the index TTL at
// SessionTTL+channelIndexMargin, so with a 48h SessionTTL the index must
// still resolve the session after fast-forwarding Redis's own clock well
// past the old flat 24h TTL.
func TestChannelIndexSurvivesLongSessionTTL(t *testing.T) {
	srv := resetChannelSSOStore(t, true)
	setTestConfig(t, Config{ProxyDomain: "modex-ai.cloud", Scheme: "https", SessionTTL: 48 * time.Hour}, true, true)

	pending := newPendingTestSession(t, 77)
	activated, err := ActivateSession(pending.ID)
	require.NoError(t, err)

	// Advance both clocks by more than the old flat 24h channel-index
	// TTL: nowFunc (so the session's own ExpiresAt, checked logically on
	// every read, is still well within its 48h SessionTTL) and
	// miniredis's real-time clock (so a channel-index key that had only
	// the old flat 24h TTL would have been physically evicted by Redis by
	// this point, even though the session it indexes is still logically
	// alive).
	advanceClock(25 * time.Hour)
	srv.FastForward(25 * time.Hour)

	require.NoError(t, RevokeChannelSessions(activated.ChannelID))

	_, err = GetSession(activated.ID)
	assert.ErrorIs(t, err, ErrSessionNotFound, "revoke must still find and remove the session once the channel index TTL correctly covers a 48h SessionTTL")
}

// TestRevokeChannelSessionsFailsClosedOnDecodeError pins the fix for review
// finding 1: revokeChannelSessionsRedis used to swallow every per-session
// Get/decode/Del error (`_ = ...Err()`) and unconditionally return nil, so
// a failure encountered while revoking a security-sensitive control could
// be reported as success. A corrupted session record (decode failure) must
// now make RevokeChannelSessions itself return a non-nil error.
func TestRevokeChannelSessionsFailsClosedOnDecodeError(t *testing.T) {
	resetChannelSSOStore(t, true)
	setTestConfig(t, Config{ProxyDomain: "modex-ai.cloud", Scheme: "https", SessionTTL: 2 * time.Hour}, true, true)

	pending := newPendingTestSession(t, 902)
	activated, err := ActivateSession(pending.ID)
	require.NoError(t, err)

	// Simulate a corrupted session record (e.g. a bug or an external
	// write) so decoding it during revoke fails for this one id, even
	// though every Redis command involved still succeeds.
	require.NoError(t, common.RDB.Set(context.Background(), sessionKey(activated.ID), "{not-json", 0).Err())

	err = RevokeChannelSessions(902)
	assert.Error(t, err, "a decode failure encountered while revoking must be reported, not swallowed as success")
}

// proxyUpstreamCapture records what the proxy test upstream received, so
// tests can assert exactly what crossed the proxy.
type proxyUpstreamCapture struct {
	mu       sync.Mutex
	requests int
	header   http.Header
}

func (c *proxyUpstreamCapture) record(r *http.Request) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.requests++
	c.header = r.Header.Clone()
}

func (c *proxyUpstreamCapture) snapshot() (int, http.Header) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.requests, c.header
}

// startProxyTestSession prepares everything the subdomain proxy needs: a
// channel with an SSO credential, an upstream login state whose jar holds
// new_api_refresh=r1, and an activated SSO session pointing at an
// httptest upstream served by handler (wrapped to record each request).
func startProxyTestSession(t *testing.T, kind string, rawUser string, handler http.HandlerFunc) (*Session, *httptest.Server, *proxyUpstreamCapture) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	setupChannelSSOModelTest(t)
	allowPrivateUpstreamAccess(t)
	setTestConfig(t, Config{ProxyDomain: "modex-ai.cloud", Scheme: "https", SessionTTL: 2 * time.Hour}, true, true)
	resetChannelSSOStore(t, false)

	capture := &proxyUpstreamCapture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capture.record(r)
		handler(w, r)
	}))
	t.Cleanup(srv.Close)

	channel := &model.Channel{Name: "sso-proxy", Status: 1, Models: "gpt-4", Group: "default"}
	require.NoError(t, channel.Insert())
	cred := &model.ChannelSSOCredential{ChannelId: channel.Id, BaseURL: srv.URL, Username: "admin", PasswordCipher: "sealed", EntryPath: "/dashboard"}
	require.NoError(t, model.SaveChannelSSOCredential(cred))

	user := UpstreamUser{Id: 1, Username: "admin", Role: 100, Raw: []byte(rawUser)}
	jar := &Jar{Cookies: []StoredCookie{{Name: "new_api_refresh", Value: "r1", Path: "/", HttpOnly: true}}}
	require.NoError(t, saveUpstreamSession(channel.Id, jar, kind, &user))

	pending := &Session{
		ChannelID:           channel.Id,
		AdminUserID:         7,
		BaseURL:             srv.URL,
		EntryPath:           "/dashboard",
		CredentialUpdatedAt: cred.UpdatedAt,
		UpstreamKind:        kind,
		User:                user,
	}
	require.NoError(t, CreatePendingSession(pending))
	active, err := ActivateSession(pending.ID)
	require.NoError(t, err)
	return active, srv, capture
}

func newProxyTestEngine() http.Handler {
	engine := gin.New()
	engine.NoRoute(func(c *gin.Context) { c.String(http.StatusOK, "main-site") })
	return WrapHandler(engine)
}

func proxyRequest(engine http.Handler, s *Session, method, target string, header http.Header) *httptest.ResponseRecorder {
	// A cancelable context mirrors a real server request; ReverseProxy
	// otherwise falls back to CloseNotify, which a recorder lacks.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := httptest.NewRequestWithContext(ctx, method, target, nil)
	req.Host = s.HostLabel + ".modex-ai.cloud"
	maps.Copy(req.Header, header)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

func sessionCookieHeader(s *Session) http.Header {
	return http.Header{"Cookie": {"__Host-sso=" + s.ID + "; main=1"}}
}

func findCookie(cookies []*http.Cookie, name string) *http.Cookie {
	for _, c := range cookies {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func TestEnterConsumesTicketAndSetsCookie(t *testing.T) {
	s, _, capture := startProxyTestSession(t, "modern", `{"id":1,"username":"admin","role":100}`, func(w http.ResponseWriter, r *http.Request) {})
	engine := newProxyTestEngine()
	ticket, err := IssueTicket(s.ID, s.HostLabel)
	require.NoError(t, err)

	rec := proxyRequest(engine, s, http.MethodGet, "/__sso/enter?ticket="+ticket, nil)
	require.Equal(t, http.StatusFound, rec.Code)
	assert.Equal(t, "/dashboard", rec.Header().Get("Location"))
	assert.Equal(t, "no-referrer", rec.Header().Get("Referrer-Policy"))

	cookies := rec.Result().Cookies()
	ssoCookie := findCookie(cookies, "__Host-sso")
	require.NotNil(t, ssoCookie)
	assert.Equal(t, s.ID, ssoCookie.Value)
	assert.True(t, ssoCookie.HttpOnly)
	assert.True(t, ssoCookie.Secure)
	assert.Equal(t, http.SameSiteLaxMode, ssoCookie.SameSite)
	assert.Equal(t, "/", ssoCookie.Path)
	assert.Empty(t, ssoCookie.Domain)
	assert.InDelta(t, (2 * time.Hour).Seconds(), float64(ssoCookie.MaxAge), 5)
	hint := findCookie(cookies, "new_api_has_session")
	require.NotNil(t, hint)
	assert.False(t, hint.HttpOnly)
	assert.Equal(t, "/", hint.Path)

	again := proxyRequest(engine, s, http.MethodGet, "/__sso/enter?ticket="+ticket, nil)
	assert.Equal(t, http.StatusBadRequest, again.Code)
	assert.Contains(t, again.Body.String(), "SSO link is invalid or expired")
	assert.Empty(t, again.Result().Cookies())

	requests, _ := capture.snapshot()
	assert.Zero(t, requests, "entering must not touch the upstream")
}

func TestEnterLegacySeedsLocalStorageSafely(t *testing.T) {
	raw := `{"id":1,"username":"</script><script>alert(1)</script>","role":100}`
	s, _, _ := startProxyTestSession(t, "legacy", raw, func(w http.ResponseWriter, r *http.Request) {})
	engine := newProxyTestEngine()
	ticket, err := IssueTicket(s.ID, s.HostLabel)
	require.NoError(t, err)

	rec := proxyRequest(engine, s, http.MethodGet, "/__sso/enter?ticket="+ticket, nil)
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.NotContains(t, body, "</script><script>alert")
	assert.Contains(t, body, "localStorage.setItem('user',")
	assert.Contains(t, body, `location.replace("/dashboard")`)
	assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	assert.Equal(t, "no-referrer", rec.Header().Get("Referrer-Policy"))
	assert.Contains(t, rec.Header().Get("Content-Security-Policy"), "default-src 'none'; script-src 'nonce-")
	assert.NotNil(t, findCookie(rec.Result().Cookies(), "__Host-sso"))
	assert.Nil(t, findCookie(rec.Result().Cookies(), "new_api_has_session"), "the hint cookie is for modern upstreams only")
}

func TestProxyStripsBrowserCookiesAndUsesJar(t *testing.T) {
	s, srv, capture := startProxyTestSession(t, "modern", `{"id":1}`, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	engine := newProxyTestEngine()
	header := sessionCookieHeader(s)
	proxyOrigin := "https://" + s.HostLabel + ".modex-ai.cloud"
	header.Set("Origin", proxyOrigin)
	header.Set("Referer", proxyOrigin+"/console?x=1")
	header.Set("Authorization", "Bearer upstream-access")
	header.Set("X-Forwarded-For", "1.2.3.4")
	header.Set("X-Real-IP", "1.2.3.4")
	header.Set("Forwarded", "for=1.2.3.4")

	rec := proxyRequest(engine, s, http.MethodGet, "/api/user/self", header)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "ok", rec.Body.String())

	requests, got := capture.snapshot()
	require.Equal(t, 1, requests)
	assert.Equal(t, "new_api_refresh=r1", got.Get("Cookie"))
	assert.Equal(t, srv.URL, got.Get("Origin"))
	assert.Equal(t, srv.URL+"/console?x=1", got.Get("Referer"))
	assert.Equal(t, "Bearer upstream-access", got.Get("Authorization"))
	assert.Empty(t, got.Values("X-Forwarded-For"))
	assert.Empty(t, got.Values("X-Real-IP"))
	assert.Empty(t, got.Values("Forwarded"))
}

func TestProxyCapturesSetCookie(t *testing.T) {
	s, _, _ := startProxyTestSession(t, "modern", `{"id":1}`, func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "new_api_refresh", Value: "r2", Path: "/", HttpOnly: true})
		http.SetCookie(w, &http.Cookie{Name: "new_api_has_session", Value: "1", Path: "/", Domain: "upstream.example.com"})
		_, _ = w.Write([]byte("ok"))
	})
	engine := newProxyTestEngine()

	rec := proxyRequest(engine, s, http.MethodPost, "/api/user/auth/refresh", sessionCookieHeader(s))
	require.Equal(t, http.StatusOK, rec.Code)
	cookies := rec.Result().Cookies()
	assert.Nil(t, findCookie(cookies, "new_api_refresh"), "upstream session cookies must never reach the browser")
	hint := findCookie(cookies, "new_api_has_session")
	require.NotNil(t, hint)
	assert.Empty(t, hint.Domain)

	stored, err := model.GetChannelSSOUpstreamSession(s.ChannelID)
	require.NoError(t, err)
	jar, err := openStoredJar(stored)
	require.NoError(t, err)
	assert.True(t, jarHasCookie(jar, "new_api_refresh", "r2"))
	assert.False(t, jarHasCookie(jar, "new_api_has_session", "1"))
}

func TestProxyRewritesOnlyUpstreamLocation(t *testing.T) {
	var location string
	s, srv, _ := startProxyTestSession(t, "modern", `{"id":1}`, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", location)
		w.WriteHeader(http.StatusFound)
	})
	engine := newProxyTestEngine()

	cases := map[string]string{
		srv.URL + "/login":               "https://" + s.HostLabel + ".modex-ai.cloud/login",
		"https://github.com/login/oauth": "https://github.com/login/oauth",
		"/console":                       "/console",
	}
	for upstreamLocation, want := range cases {
		location = upstreamLocation
		rec := proxyRequest(engine, s, http.MethodGet, "/", sessionCookieHeader(s))
		require.Equal(t, http.StatusFound, rec.Code)
		assert.Equal(t, want, rec.Header().Get("Location"), upstreamLocation)
	}
}

func TestProxyExpiredSessionReturns401(t *testing.T) {
	s, _, capture := startProxyTestSession(t, "modern", `{"id":1}`, func(w http.ResponseWriter, r *http.Request) {})
	engine := newProxyTestEngine()

	advanceClock(2*time.Hour + time.Second)
	rec := proxyRequest(engine, s, http.MethodGet, "/dashboard", sessionCookieHeader(s))
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Body.String(), "SSO session expired. Return to the admin console and start single sign-on again.")
	nowFunc = time.Now

	// A missing cookie, and a session presented on another label's host,
	// fail the same way.
	rec = proxyRequest(engine, s, http.MethodGet, "/dashboard", nil)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	req.Host = "zzzz9999.modex-ai.cloud"
	req.Header.Set("Cookie", "__Host-sso="+s.ID)
	other := httptest.NewRecorder()
	engine.ServeHTTP(other, req)
	assert.Equal(t, http.StatusUnauthorized, other.Code)

	requests, _ := capture.snapshot()
	assert.Zero(t, requests, "the upstream must not see unauthenticated requests")
}

func TestProxyRejectsWebSocketUpgrade(t *testing.T) {
	s, _, capture := startProxyTestSession(t, "modern", `{"id":1}`, func(w http.ResponseWriter, r *http.Request) {})
	engine := newProxyTestEngine()
	header := sessionCookieHeader(s)
	header.Set("Connection", "Upgrade")
	header.Set("Upgrade", "websocket")

	rec := proxyRequest(engine, s, http.MethodGet, "/ws", header)
	assert.Equal(t, http.StatusNotImplemented, rec.Code)
	requests, _ := capture.snapshot()
	assert.Zero(t, requests)
}

func TestWrapHandlerIgnoresOtherHosts(t *testing.T) {
	s, _, capture := startProxyTestSession(t, "modern", `{"id":1}`, func(w http.ResponseWriter, r *http.Request) {})
	engine := newProxyTestEngine()

	for _, host := range []string{"modex-ai.cloud", "api.modex-ai.cloud", s.HostLabel + ".evil.com", "x" + s.HostLabel + ".modex-ai.cloud"} {
		req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
		req.Host = host
		req.Header.Set("Cookie", "__Host-sso="+s.ID)
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code, host)
		assert.Equal(t, "main-site", rec.Body.String(), host)
	}

	// With the feature disabled, even a matching host reaches the main site.
	setTestConfig(t, Config{ProxyDomain: "modex-ai.cloud", Scheme: "https", SessionTTL: 2 * time.Hour}, true, false)
	rec := proxyRequest(engine, s, http.MethodGet, "/dashboard", sessionCookieHeader(s))
	assert.Equal(t, "main-site", rec.Body.String())

	requests, _ := capture.snapshot()
	assert.Zero(t, requests)
}

func TestProxyStreamsSSE(t *testing.T) {
	release := make(chan struct{})
	s, _, _ := startProxyTestSession(t, "modern", `{"id":1}`, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: a\n\n"))
		w.(http.Flusher).Flush()
		<-release
		_, _ = w.Write([]byte("data: b\n\n"))
	})
	proxy := httptest.NewServer(newProxyTestEngine())
	t.Cleanup(proxy.Close)
	defer close(release)

	req, err := http.NewRequest(http.MethodGet, proxy.URL+"/api/stream", nil)
	require.NoError(t, err)
	req.Host = s.HostLabel + ".modex-ai.cloud"
	req.Header.Set("Cookie", "__Host-sso="+s.ID)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	buf := make([]byte, len("data: a\n\n"))
	_, err = io.ReadFull(resp.Body, buf)
	require.NoError(t, err, "the first event must arrive before the upstream sends the second")
	assert.Equal(t, "data: a\n\n", string(buf))
}

// TestProxyAbortsTruncatedStream pins that an upstream stream cut off
// mid-body reaches the browser as a broken connection, not as a cleanly
// finished response, and that the main site's gin panic recovery (which
// would append its own error body) never handles proxy requests.
func TestProxyAbortsTruncatedStream(t *testing.T) {
	s, _, _ := startProxyTestSession(t, "modern", `{"id":1}`, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: a\n\n"))
		w.(http.Flusher).Flush()
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			_ = conn.Close()
		}
	})
	engine := gin.New()
	engine.Use(gin.CustomRecovery(func(c *gin.Context, err any) {
		c.String(http.StatusInternalServerError, "Panic detected")
	}))
	engine.NoRoute(func(c *gin.Context) { c.String(http.StatusOK, "main-site") })
	proxy := httptest.NewServer(WrapHandler(engine))
	t.Cleanup(proxy.Close)

	req, err := http.NewRequest(http.MethodGet, proxy.URL+"/api/stream", nil)
	require.NoError(t, err)
	req.Host = s.HostLabel + ".modex-ai.cloud"
	req.Header.Set("Cookie", "__Host-sso="+s.ID)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	assert.ErrorIs(t, err, io.ErrUnexpectedEOF, "a truncated upstream stream must not look complete")
	assert.Equal(t, "data: a\n\n", string(body))
	assert.NotContains(t, string(body), "Panic detected")
}

// TestProxyWriteBackKeepsConcurrentRotation pins that the jar written back
// after a response is the currently stored one plus this response's
// cookies: a refresh cookie another request rotated while this one was in
// flight must not be reverted to the value this request started with.
func TestProxyWriteBackKeepsConcurrentRotation(t *testing.T) {
	var channelID int
	s, _, _ := startProxyTestSession(t, "modern", `{"id":1}`, func(w http.ResponseWriter, r *http.Request) {
		// Request B finished first and rotated the refresh cookie.
		rotated := &Jar{Cookies: []StoredCookie{{Name: "new_api_refresh", Value: "r2", Path: "/", HttpOnly: true}}}
		user := UpstreamUser{Id: 1, Raw: []byte(`{"id":1}`)}
		if err := saveUpstreamSession(channelID, rotated, "modern", &user); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "x", Value: "1", Path: "/"})
		_, _ = w.Write([]byte("ok"))
	})
	channelID = s.ChannelID
	engine := newProxyTestEngine()

	rec := proxyRequest(engine, s, http.MethodGet, "/api/user/self", sessionCookieHeader(s))
	require.Equal(t, http.StatusOK, rec.Code)

	stored, err := model.GetChannelSSOUpstreamSession(s.ChannelID)
	require.NoError(t, err)
	jar, err := openStoredJar(stored)
	require.NoError(t, err)
	assert.True(t, jarHasCookie(jar, "new_api_refresh", "r2"), "the concurrently rotated refresh cookie must survive")
	assert.False(t, jarHasCookie(jar, "new_api_refresh", "r1"))
	assert.True(t, jarHasCookie(jar, "x", "1"))
}
