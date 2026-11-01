package channelsso

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
