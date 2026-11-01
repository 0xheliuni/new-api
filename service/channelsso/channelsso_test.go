package channelsso

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/glebarez/sqlite"
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
