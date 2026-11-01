package channelsso

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
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
