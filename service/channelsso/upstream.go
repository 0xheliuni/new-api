// Package channelsso: this file holds the HTTP client that logs in to an
// upstream new-api site and keeps its cookies in a Jar (see jar.go). It is
// the only place in this feature that makes outbound network requests, so
// every request goes through SSRF validation both when the Upstream is
// constructed and again at dial time, to resist DNS rebinding between the
// two checks.
package channelsso

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/system_setting"
)

// upstreamUserAgent identifies our outbound requests to the upstream site;
// it never carries any credential.
const upstreamUserAgent = "new-api-channel-sso"

// upstreamRequestTimeout bounds every non-streaming request this client
// makes (constraints.md: 上游请求超时 10s).
const upstreamRequestTimeout = 10 * time.Second

// Errors returned by Upstream methods. They double as frontend i18n keys,
// so their text MUST stay in English and MUST NOT be changed casually.
var (
	ErrSiteUnreachable   = errors.New("Upstream site is unreachable")
	ErrNotNewAPI         = errors.New("Upstream site is not a new-api site")
	ErrChallengeRequired = errors.New("Upstream requires two-step or human verification; automatic login is not possible")
	ErrSessionInvalid    = errors.New("Upstream session is invalid")
)

// UpstreamError carries an upstream {success:false, message:...} response
// message verbatim (for example a wrong-password message in the upstream's
// own language). It is distinct from the fixed Err* sentinels above because
// its text comes from the upstream, not from us, and must never be treated
// as a stable i18n key.
type UpstreamError struct {
	Message string
}

func (e *UpstreamError) Error() string {
	return e.Message
}

// UpstreamUser is the account Channel SSO logged in as. Raw MUST hold the
// upstream's raw user JSON object exactly as received (the "user" object
// for a modern login, or the whole "data" object for a legacy one) because
// a later step replays it into the legacy upstream SPA's
// localStorage.user.
type UpstreamUser struct {
	Id       int
	Username string
	Role     int
	Raw      json.RawMessage
}

// LoginResult reports which upstream generation answered the login and the
// credentials it returned.
type LoginResult struct {
	// Kind is "modern" (access token + refresh cookie) or "legacy" (session
	// cookie only).
	Kind        string
	AccessToken string
	User        UpstreamUser
}

// Upstream is an HTTP client bound to one upstream new-api site's origin.
type Upstream struct {
	BaseURL string
	Client  *http.Client
}

// NewUpstream normalizes baseURL to scheme://host[:port] and returns a
// client bound to it. It rejects a plain-http upstream when the Channel SSO
// Config scheme is "https" (production must not downgrade to cleartext),
// and validates the origin against the SSRF protection built from the
// current fetch_setting. The returned Client never follows redirects
// (CheckRedirect returns http.ErrUseLastResponse) and re-validates every
// resolved IP immediately before dialing it.
func NewUpstream(baseURL string) (*Upstream, error) {
	normalized, err := normalizeUpstreamBaseURL(baseURL)
	if err != nil {
		return nil, err
	}
	u, _ := url.Parse(normalized)
	if Current().Scheme == "https" && u.Scheme != "https" {
		return nil, errors.New("Upstream URL must use https")
	}
	protection, enabled, err := currentUpstreamSSRFProtection()
	if err != nil {
		return nil, err
	}
	if enabled {
		if err := protection.ValidateURL(normalized); err != nil {
			return nil, err
		}
	}
	return &Upstream{BaseURL: normalized, Client: newUpstreamHTTPClient()}, nil
}

// normalizeUpstreamBaseURL validates raw and strips it down to
// scheme://host[:port], discarding any path, query, or fragment.
func normalizeUpstreamBaseURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", fmt.Errorf("invalid upstream URL: %s", raw)
	}
	return u.Scheme + "://" + u.Host, nil
}

// currentUpstreamSSRFProtection builds a common.SSRFProtection from the
// currently persisted fetch_setting, mirroring
// service.currentFetchProtection. It is reimplemented locally (rather than
// imported) because service/channelsso must not import the service
// package, which this feature's later tasks are expected to depend on.
func currentUpstreamSSRFProtection() (*common.SSRFProtection, bool, error) {
	fetchSetting := system_setting.GetFetchSetting()
	if !fetchSetting.EnableSSRFProtection {
		return nil, false, nil
	}
	protection, err := common.NewSSRFProtectionFromFetchSetting(
		fetchSetting.AllowPrivateIp,
		fetchSetting.DomainFilterMode,
		fetchSetting.IpFilterMode,
		fetchSetting.DomainList,
		fetchSetting.IpList,
		fetchSetting.AllowedPorts,
		fetchSetting.ApplyIPFilterForDomain,
	)
	if err != nil {
		return nil, true, err
	}
	return protection, true, nil
}

func newUpstreamHTTPClient() *http.Client {
	dialer := &net.Dialer{Timeout: upstreamRequestTimeout}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dialUpstreamAddr(ctx, dialer, network, addr)
		},
	}
	return &http.Client{
		Transport: transport,
		Timeout:   upstreamRequestTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// dialUpstreamAddr re-validates the SSRF protection immediately before
// dialing, using the IP actually about to be connected to rather than the
// one checked when the Upstream was constructed, so a domain that resolves
// differently between those two moments (DNS rebinding) cannot bypass the
// check.
func dialUpstreamAddr(ctx context.Context, dialer *net.Dialer, network, addr string) (net.Conn, error) {
	protection, enabled, err := currentUpstreamSSRFProtection()
	if err != nil {
		return nil, err
	}
	host, portText, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("invalid dial address %s: %w", addr, err)
	}
	if enabled {
		port, err := strconv.Atoi(portText)
		if err != nil {
			return nil, fmt.Errorf("invalid port: %s", portText)
		}
		if err := protection.ValidateNetworkTarget(host, port); err != nil {
			return nil, err
		}
	}
	if ip := net.ParseIP(host); ip != nil {
		if enabled {
			if err := protection.ValidateResolvedIP(host, ip); err != nil {
				return nil, err
			}
		}
		return dialer.DialContext(ctx, network, addr)
	}

	resolved, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("DNS resolution failed for %s: %w", host, err)
	}
	var lastErr error
	for _, ipAddr := range resolved {
		if enabled {
			if err := protection.ValidateResolvedIP(host, ipAddr.IP); err != nil {
				lastErr = err
				continue
			}
		}
		conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ipAddr.IP.String(), portText))
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("DNS resolution for %s returned no usable IP addresses", host)
	}
	return nil, lastErr
}

// newRequest builds a request with the Origin/Referer/User-Agent headers
// every outbound call MUST carry, so the upstream's own origin checks (and
// its request logs) see our server acting as itself, not as the browser.
func (u *Upstream) newRequest(ctx context.Context, method, path string, body []byte) (*http.Request, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, u.BaseURL+path, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Origin", u.BaseURL)
	req.Header.Set("Referer", u.BaseURL+"/")
	req.Header.Set("User-Agent", upstreamUserAgent)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

// send issues one request through the jar: the jar's cookies for path are
// attached before the call, and every Set-Cookie the response carries is
// applied back into it afterward, even on a non-2xx response. jar may be
// nil for a request that carries no session state (none of this client's
// callers currently do that, but Status has no jar to apply).
func (u *Upstream) send(ctx context.Context, jar *Jar, method, path string, payload any, extraHeaders map[string]string) (*http.Response, error) {
	var body []byte
	if payload != nil {
		var err error
		body, err = common.Marshal(payload)
		if err != nil {
			return nil, err
		}
	}
	req, err := u.newRequest(ctx, method, path, body)
	if err != nil {
		return nil, err
	}
	if jar != nil {
		if cookieHeader := jar.Header(path); cookieHeader != "" {
			req.Header.Set("Cookie", cookieHeader)
		}
	}
	for name, value := range extraHeaders {
		req.Header.Set(name, value)
	}
	resp, err := u.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrSiteUnreachable, err)
	}
	if jar != nil {
		jar.Apply(resp.Cookies())
	}
	return resp, nil
}

// Status checks that the upstream is reachable and looks like a new-api
// site, and returns its configured site name.
func (u *Upstream) Status(ctx context.Context) (string, error) {
	resp, err := u.send(ctx, nil, http.MethodGet, "/api/status", nil, nil)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", ErrSiteUnreachable
	}
	var body struct {
		Success bool `json:"success"`
		Data    struct {
			SystemName string `json:"system_name"`
		} `json:"data"`
	}
	if err := common.DecodeJson(resp.Body, &body); err != nil || !body.Success || body.Data.SystemName == "" {
		return "", ErrNotNewAPI
	}
	return body.Data.SystemName, nil
}

// encryptionKeyResponse is the GET /api/user/login/encryption-key shape
// (controller.GetPasswordEncryptionKey).
type encryptionKeyResponse struct {
	Success bool `json:"success"`
	Data    struct {
		Enabled   bool   `json:"enabled"`
		Kid       string `json:"kid"`
		PublicKey string `json:"public_key"`
	} `json:"data"`
}

// passwordLoginPayload fetches the upstream's password-encryption key and
// returns the request-body fields Login should send: either a plaintext
// "password", or an encrypted "password_encrypted"/"encryption_key_id" pair
// when the upstream has encryption enabled. A 404 or any non-2xx/malformed
// response is treated the same as "disabled": upstream releases before
// password-login encryption existed don't have this endpoint at all.
func (u *Upstream) passwordLoginPayload(ctx context.Context, password string) (map[string]any, error) {
	resp, err := u.send(ctx, nil, http.MethodGet, "/api/user/login/encryption-key", nil, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return map[string]any{"password": password}, nil
	}
	var body encryptionKeyResponse
	if err := common.DecodeJson(resp.Body, &body); err != nil || !body.Success || !body.Data.Enabled {
		return map[string]any{"password": password}, nil
	}
	encrypted, err := encryptUpstreamPasswordV2(body.Data.PublicKey, body.Data.Kid, password)
	if err != nil {
		return nil, err
	}
	return map[string]any{"password_encrypted": encrypted, "encryption_key_id": body.Data.Kid}, nil
}

// encryptUpstreamPasswordV2 builds the v2 password envelope that
// common.DecryptPassword decrypts: a fresh AES-256 key wrapped with
// RSA-OAEP/SHA-256 (label "password-v2"), and the password encrypted with
// that key under AES-256-GCM (additional data "password-v2:"+kid),
// formatted as "v2.<wrapped-key>.<nonce>.<ciphertext>" with each part
// standard-base64 encoded.
func encryptUpstreamPasswordV2(publicKeyPEM, kid, password string) (string, error) {
	block, _ := pem.Decode([]byte(publicKeyPEM))
	if block == nil {
		return "", fmt.Errorf("channelsso: upstream encryption public key is not valid PEM")
	}
	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("channelsso: parse upstream encryption public key: %w", err)
	}
	publicKey, ok := parsed.(*rsa.PublicKey)
	if !ok {
		return "", fmt.Errorf("channelsso: upstream encryption public key is not RSA")
	}

	aesKey := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, aesKey); err != nil {
		return "", err
	}
	wrappedKey, err := rsa.EncryptOAEP(sha256.New(), rand.Reader, publicKey, aesKey, []byte("password-v2"))
	if err != nil {
		return "", err
	}

	block2, err := aes.NewCipher(aesKey)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block2)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ciphertext := gcm.Seal(nil, nonce, []byte(password), []byte("password-v2:"+kid))

	return strings.Join([]string{
		"v2",
		base64.StdEncoding.EncodeToString(wrappedKey),
		base64.StdEncoding.EncodeToString(nonce),
		base64.StdEncoding.EncodeToString(ciphertext),
	}, "."), nil
}

// Login authenticates against the upstream and classifies the response as
// modern, legacy, a verification challenge, or an upstream-reported
// failure. On success it has already applied any Set-Cookie the upstream
// sent into jar.
func (u *Upstream) Login(ctx context.Context, jar *Jar, username, password string) (*LoginResult, error) {
	payload, err := u.passwordLoginPayload(ctx, password)
	if err != nil {
		return nil, err
	}
	payload["username"] = username
	resp, err := u.send(ctx, jar, http.MethodPost, "/api/user/login", payload, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return parseLoginResponse(resp.Body)
}

// loginResponseProbe captures the handful of discriminating fields present
// across every shape /api/user/login can answer with: a verification
// challenge (service.LoginChallenge), a modern success (access_token +
// user), or a legacy success (the user fields directly in data).
type loginResponseProbe struct {
	RequireVerification bool            `json:"require_verification"`
	AccessToken         string          `json:"access_token"`
	Id                  *int            `json:"id"`
	User                json.RawMessage `json:"user"`
}

func parseLoginResponse(r io.Reader) (*LoginResult, error) {
	var envelope struct {
		Success bool            `json:"success"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if err := common.DecodeJson(r, &envelope); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNotNewAPI, err)
	}
	if strings.Contains(strings.ToLower(envelope.Message), "turnstile") {
		return nil, ErrChallengeRequired
	}
	var probe loginResponseProbe
	if len(envelope.Data) > 0 {
		// A malformed data payload just leaves probe at its zero value,
		// which falls through to ErrNotNewAPI below.
		_ = common.Unmarshal(envelope.Data, &probe)
	}
	if !envelope.Success {
		if envelope.Message == "" {
			return nil, ErrNotNewAPI
		}
		return nil, &UpstreamError{Message: envelope.Message}
	}
	if probe.RequireVerification {
		return nil, ErrChallengeRequired
	}
	if probe.AccessToken != "" {
		user, err := parseUpstreamUser(probe.User)
		if err != nil {
			return nil, err
		}
		return &LoginResult{Kind: "modern", AccessToken: probe.AccessToken, User: user}, nil
	}
	if probe.Id != nil {
		user, err := parseUpstreamUser(envelope.Data)
		if err != nil {
			return nil, err
		}
		return &LoginResult{Kind: "legacy", User: user}, nil
	}
	return nil, ErrNotNewAPI
}

func parseUpstreamUser(raw json.RawMessage) (UpstreamUser, error) {
	if len(raw) == 0 {
		return UpstreamUser{}, ErrNotNewAPI
	}
	var fields struct {
		Id       int    `json:"id"`
		Username string `json:"username"`
		Role     int    `json:"role"`
	}
	if err := common.Unmarshal(raw, &fields); err != nil {
		return UpstreamUser{}, fmt.Errorf("%w: %v", ErrNotNewAPI, err)
	}
	rawCopy := make(json.RawMessage, len(raw))
	copy(rawCopy, raw)
	return UpstreamUser{Id: fields.Id, Username: fields.Username, Role: fields.Role, Raw: rawCopy}, nil
}

// Refresh exchanges the jar's refresh cookie for a new access token on a
// modern upstream, without touching /api/user/login. The jar's refresh
// cookie is updated in place from the response, matching how a browser
// session rotates it.
func (u *Upstream) Refresh(ctx context.Context, jar *Jar) (string, error) {
	resp, err := u.send(ctx, jar, http.MethodPost, "/api/user/auth/refresh", nil, nil)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", ErrSessionInvalid
	}
	var body struct {
		Success bool `json:"success"`
		Data    struct {
			AccessToken string `json:"access_token"`
		} `json:"data"`
	}
	if err := common.DecodeJson(resp.Body, &body); err != nil || !body.Success || body.Data.AccessToken == "" {
		return "", ErrSessionInvalid
	}
	return body.Data.AccessToken, nil
}

// Self validates the login state by calling /api/user/self: a modern
// upstream is authorized with the Bearer access token, a legacy one with
// the jar's session cookie plus the New-Api-User header the legacy
// middleware requires.
func (u *Upstream) Self(ctx context.Context, jar *Jar, kind, accessToken string, userId int) (*UpstreamUser, error) {
	headers := map[string]string{}
	if kind == "modern" {
		headers["Authorization"] = "Bearer " + accessToken
	} else {
		headers["New-Api-User"] = strconv.Itoa(userId)
	}
	resp, err := u.send(ctx, jar, http.MethodGet, "/api/user/self", nil, headers)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, ErrSessionInvalid
	}
	var envelope struct {
		Success bool            `json:"success"`
		Data    json.RawMessage `json:"data"`
	}
	if err := common.DecodeJson(resp.Body, &envelope); err != nil || !envelope.Success {
		return nil, ErrSessionInvalid
	}
	user, err := parseUpstreamUser(envelope.Data)
	if err != nil {
		return nil, ErrSessionInvalid
	}
	return &user, nil
}

// Logout tells the upstream to end the session the jar holds
// (POST /api/user/auth/logout), best-effort: callers that cannot afford a
// failure (configuration change, channel deletion) should log and continue
// rather than block on its result.
func (u *Upstream) Logout(ctx context.Context, jar *Jar) error {
	resp, err := u.send(ctx, jar, http.MethodPost, "/api/user/auth/logout", nil, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ErrSessionInvalid
	}
	return nil
}
