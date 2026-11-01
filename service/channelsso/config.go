// Package channelsso implements the Channel SSO feature: proxying admin
// access to upstream channel back-offices through short-lived, randomly
// labeled subdomains. This file holds configuration loading and the
// enabled/disabled gate; see crypto.go for credential sealing.
package channelsso

import (
	"errors"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
)

// defaultSessionTTL is used whenever SSO_SESSION_TTL is unset, unparsable,
// or non-positive.
const defaultSessionTTL = 2 * time.Hour

// ErrDisabled is returned by Enabled when Channel SSO is not available on
// this server, and callers MUST surface it (or an equivalent message) rather
// than proceeding with any SSO flow.
var ErrDisabled = errors.New("Channel SSO is not enabled on this server")

// Config holds the Channel SSO settings loaded from the environment once at
// process startup.
type Config struct {
	// ProxyDomain is the parent domain under which per-session subdomains
	// are minted (SSO_PROXY_DOMAIN). An empty value disables the feature.
	ProxyDomain string
	// Scheme is the scheme used for proxy subdomain origins
	// (SSO_PROXY_SCHEME). Defaults to "https"; "http" is for local
	// development only.
	Scheme string
	// SessionTTL is how long an activated SSO session stays valid
	// (SSO_SESSION_TTL). It does not renew.
	SessionTTL time.Duration
}

var (
	mu sync.RWMutex

	// current is the last config installed by LoadConfig.
	current Config
	// explicitSecret records whether CRYPTO_SECRET or SESSION_SECRET was
	// explicitly configured, as opposed to common.CryptoSecret falling
	// back to a random value generated for this process only. Without an
	// explicit secret, credentials sealed before a restart cannot be
	// opened afterward, so the feature stays disabled.
	explicitSecret bool
)

// LoadConfig reads the Channel SSO environment variables, installs them as
// the current package-level config, and returns the loaded Config. Call it
// once at process startup, after common.InitEnv. It does not panic, exit,
// or log; the caller warns at startup when a configured proxy domain is
// left disabled, and callers must still go through Enabled() before using
// any SSO capability.
func LoadConfig() Config {
	cfg := Config{
		ProxyDomain: strings.TrimSpace(os.Getenv("SSO_PROXY_DOMAIN")),
		Scheme:      strings.TrimSpace(os.Getenv("SSO_PROXY_SCHEME")),
		SessionTTL:  parseSessionTTL(os.Getenv("SSO_SESSION_TTL")),
	}
	if cfg.Scheme == "" {
		cfg.Scheme = "https"
	}
	hasExplicitSecret := os.Getenv("CRYPTO_SECRET") != "" || os.Getenv("SESSION_SECRET") != ""

	mu.Lock()
	current = cfg
	explicitSecret = hasExplicitSecret
	mu.Unlock()
	return cfg
}

// Current returns the currently installed Config.
func Current() Config {
	mu.RLock()
	defer mu.RUnlock()
	return current
}

// parseSessionTTL parses SSO_SESSION_TTL, falling back to defaultSessionTTL
// when raw is empty, unparsable, or non-positive.
func parseSessionTTL(raw string) time.Duration {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return defaultSessionTTL
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return defaultSessionTTL
	}
	return d
}

// Enabled reports whether Channel SSO is available on this server. It
// returns nil when enabled, or ErrDisabled when any required condition is
// unmet: a configured proxy domain, an explicitly configured encryption
// secret (CRYPTO_SECRET or SESSION_SECRET), and — when Scheme is "https" —
// a secure session cookie configuration (common.SessionCookieSecure).
// Every handler and background task that performs an SSO action MUST call
// Enabled() and stop on error before doing anything else.
func Enabled() error {
	mu.RLock()
	cfg := current
	hasSecret := explicitSecret
	mu.RUnlock()

	if cfg.ProxyDomain == "" {
		return ErrDisabled
	}
	if !hasSecret {
		return ErrDisabled
	}
	if cfg.Scheme == "https" && !common.SessionCookieSecure {
		return ErrDisabled
	}
	return nil
}

// ProxyOrigin builds the full origin for a proxy subdomain label, using the
// currently installed Scheme and ProxyDomain (scheme://label.domain).
// Callers MUST check Enabled() before relying on the result.
func ProxyOrigin(label string) string {
	mu.RLock()
	cfg := current
	mu.RUnlock()
	return cfg.Scheme + "://" + label + "." + cfg.ProxyDomain
}
