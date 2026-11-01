// Package channelsso: this file serves the per-session proxy subdomains
// (design §4.6). A request whose Host is <label>.<SSO_PROXY_DOMAIN> never
// reaches the main site's routes: /__sso/enter exchanges a one-time ticket
// for the proxy session cookie, and every other path is reverse-proxied to
// the session's upstream new-api site using the channel's server-side
// cookie jar. Third-party upstream JavaScript runs on this origin, so the
// browser never receives an upstream session cookie and the upstream never
// receives a browser cookie.
package channelsso

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"html"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	enterPath = "/__sso/enter"

	// hintCookieName is the modern upstream SPA's non-HttpOnly "a session
	// may exist" marker. It carries no secret and the SPA only tries to
	// refresh its access token when it sees it, so it is the one upstream
	// cookie that reaches the browser.
	hintCookieName = "new_api_has_session"

	// maxProxyBodyBytes bounds every proxied request body (constraints.md).
	maxProxyBodyBytes = 32 << 20
)

// Fixed user-visible messages of the proxy subdomain.
const (
	msgEnterInvalid   = "SSO link is invalid or expired"
	msgSessionExpired = "SSO session expired. Return to the admin console and start single sign-on again."
	msgUpgrade        = "WebSocket is not supported through the SSO proxy"
	msgBadGateway     = "Upstream site is unreachable"
	msgBodyTooLarge   = "Request body is too large"
)

// HostDispatch routes requests for proxy subdomains to the SSO proxy and
// aborts the chain; every other request (and every request while Channel
// SSO is disabled) continues to the main site untouched. Register it before
// any other middleware that is not panic recovery.
func HostDispatch() gin.HandlerFunc {
	return func(c *gin.Context) {
		if Enabled() != nil {
			c.Next()
			return
		}
		label, ok := proxyHostLabel(c.Request.Host, Current().ProxyDomain)
		if !ok {
			c.Next()
			return
		}
		c.Abort()
		if c.Request.URL.Path == enterPath {
			serveEnter(c.Writer, c.Request, label)
			return
		}
		serveProxy(c.Writer, c.Request, label)
	}
}

// proxyHostLabel reports whether host (port stripped, case-folded) is
// exactly <label>.<domain> with an 8-character [a-z0-9] label.
func proxyHostLabel(host, domain string) (string, bool) {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.ToLower(host)
	label, rest, ok := strings.Cut(host, ".")
	if !ok || rest != strings.ToLower(domain) || len(label) != hostLabelLength {
		return "", false
	}
	for _, ch := range label {
		if !strings.ContainsRune(hostLabelAlphabet, ch) {
			return "", false
		}
	}
	return label, true
}

// sessionCookieName is the proxy session cookie's name. The __Host- prefix
// (which browsers only accept on Secure cookies) pins it to this exact
// host; the plain "sso" name is used only for http local development.
func sessionCookieName() string {
	if Current().Scheme == "http" {
		return "sso"
	}
	return "__Host-sso"
}

// serveEnter consumes the one-time ticket, sets the proxy session cookie,
// and sends the browser to the session's entry path. The response drops
// the ticket from the URL and is sent with no-referrer so the ticket never
// leaks to the upstream or any third party.
func serveEnter(w http.ResponseWriter, r *http.Request, label string) {
	h := w.Header()
	h.Set("Cache-Control", "no-store")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("X-Content-Type-Options", "nosniff")

	s, err := ConsumeTicket(r.URL.Query().Get("ticket"), label)
	if err != nil {
		if !errors.Is(err, ErrTicketInvalid) && !errors.Is(err, ErrSessionNotFound) {
			common.SysError(common.LogText("channel sso: consume ticket for proxy label %s failed: %v", label, err))
		}
		http.Error(w, msgEnterInvalid, http.StatusBadRequest)
		return
	}
	maxAge := int(s.ExpiresAt - nowFunc().Unix())
	if maxAge <= 0 {
		http.Error(w, msgEnterInvalid, http.StatusBadRequest)
		return
	}
	secure := Current().Scheme == "https"
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName(),
		Value:    s.ID,
		Path:     "/",
		MaxAge:   maxAge,
		Secure:   secure,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	if s.UpstreamKind != "legacy" {
		http.SetCookie(w, &http.Cookie{Name: hintCookieName, Value: "1", Path: "/", MaxAge: maxAge, Secure: secure, SameSite: http.SameSiteLaxMode})
		http.Redirect(w, r, s.EntryPath, http.StatusFound)
		return
	}

	// The legacy SPA decides it is logged in from localStorage.user. Both
	// values are JSON string literals with every "<" escaped, so neither
	// can close the <script> element or open an HTML comment in it.
	userLiteral, err := common.Marshal(string(s.User.Raw))
	if err != nil {
		http.Error(w, msgEnterInvalid, http.StatusBadRequest)
		return
	}
	entryLiteral, err := common.Marshal(s.EntryPath)
	if err != nil {
		http.Error(w, msgEnterInvalid, http.StatusBadRequest)
		return
	}
	nonceBytes := make([]byte, 16)
	if _, err := rand.Read(nonceBytes); err != nil {
		http.Error(w, msgEnterInvalid, http.StatusInternalServerError)
		return
	}
	nonce := base64.StdEncoding.EncodeToString(nonceBytes)
	userScript := strings.ReplaceAll(string(userLiteral), "<", `\u003c`)
	entryScript := strings.ReplaceAll(string(entryLiteral), "<", `\u003c`)

	h.Set("Content-Security-Policy", "default-src 'none'; script-src 'nonce-"+nonce+"'")
	h.Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`<!doctype html><html><head><meta charset="utf-8"><meta name="referrer" content="no-referrer"><title>SSO</title></head><body><script nonce="` +
		html.EscapeString(nonce) + `">try{localStorage.setItem('user',` + userScript + `)}catch(e){}location.replace(` + entryScript + `);</script></body></html>`))
}

// serveProxy authenticates the proxy session cookie against the Host label
// and reverse-proxies the request to the session's upstream.
func serveProxy(w http.ResponseWriter, r *http.Request, label string) {
	s, ok := proxySession(r, label)
	if !ok {
		writeSessionExpired(w)
		return
	}
	if r.Header.Get("Upgrade") != "" {
		http.Error(w, msgUpgrade, http.StatusNotImplemented)
		return
	}

	target, err := url.Parse(s.BaseURL)
	if err != nil || target.Host == "" {
		http.Error(w, msgBadGateway, http.StatusBadGateway)
		return
	}
	upstreamOrigin := target.Scheme + "://" + target.Host
	proxyOrigin := Current().Scheme + "://" + strings.ToLower(r.Host)

	// The jar is loaded fresh for every request so concurrent tabs of the
	// same channel always send the newest upstream cookies.
	stored, err := model.GetChannelSSOUpstreamSession(s.ChannelID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		writeSessionExpired(w)
		return
	}
	if err != nil {
		common.SysError(common.LogText("channel sso: load upstream login state for channel %d failed: %v", s.ChannelID, err))
		http.Error(w, msgBadGateway, http.StatusBadGateway)
		return
	}
	jar, err := openStoredJar(stored)
	if err != nil {
		common.SysError(common.LogText("channel sso: open upstream login state for channel %d failed: %v", s.ChannelID, err))
		http.Error(w, msgBadGateway, http.StatusBadGateway)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxProxyBodyBytes)
	proxy := &httputil.ReverseProxy{
		// The SSRF-protected transport re-validates the upstream address at
		// dial time. ReverseProxy never follows redirects, and with Rewrite
		// it adds no X-Forwarded-* header of its own.
		Transport:     upstreamHTTPClient().Transport,
		FlushInterval: -1,
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target)
			pr.Out.Host = target.Host
			h := pr.Out.Header
			for name := range h {
				if strings.HasPrefix(name, "X-Forwarded-") {
					h.Del(name)
				}
			}
			h.Del("X-Real-Ip")
			h.Del("Forwarded")
			h.Del("Cookie")
			if cookieHeader := jar.Header(pr.Out.URL.Path); cookieHeader != "" {
				h.Set("Cookie", cookieHeader)
			}
			if h.Get("Origin") == proxyOrigin {
				h.Set("Origin", upstreamOrigin)
			}
			if rest, ok := cutOriginPrefix(h.Get("Referer"), proxyOrigin); ok {
				h.Set("Referer", upstreamOrigin+rest)
			}
		},
		ModifyResponse: func(resp *http.Response) error {
			cookies := resp.Cookies()
			resp.Header.Del("Set-Cookie")
			var captured []*http.Cookie
			for _, c := range cookies {
				if c.Name != hintCookieName {
					captured = append(captured, c)
					continue
				}
				c.Domain = ""
				if v := c.String(); v != "" {
					resp.Header.Add("Set-Cookie", v)
				}
			}
			if len(captured) > 0 {
				persistProxyJar(s, stored, jar, captured)
			}
			if rest, ok := cutOriginPrefix(resp.Header.Get("Location"), upstreamOrigin); ok {
				resp.Header.Set("Location", proxyOrigin+rest)
			}
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				http.Error(w, msgBodyTooLarge, http.StatusRequestEntityTooLarge)
				return
			}
			common.SysError(common.LogText("channel sso: proxy request for channel %d failed: %v", s.ChannelID, err))
			http.Error(w, msgBadGateway, http.StatusBadGateway)
		},
	}
	proxy.ServeHTTP(w, r)
}

// proxySession returns the activated, unexpired session named by the proxy
// session cookie, provided it is bound to this Host's label.
func proxySession(r *http.Request, label string) (*Session, bool) {
	cookie, err := r.Cookie(sessionCookieName())
	if err != nil || cookie.Value == "" {
		return nil, false
	}
	s, err := GetSession(cookie.Value)
	if err != nil {
		if !errors.Is(err, ErrSessionNotFound) {
			common.SysError(common.LogText("channel sso: load proxy session for label %s failed: %v", label, err))
		}
		return nil, false
	}
	if !s.Active || s.HostLabel != label {
		return nil, false
	}
	return s, true
}

func writeSessionExpired(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Cache-Control", "no-store")
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Content-Security-Policy", "default-src 'none'")
	h.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte(`<!doctype html><html><head><meta charset="utf-8"><title>SSO</title></head><body><p>` + html.EscapeString(msgSessionExpired) + `</p></body></html>`))
}

// cutOriginPrefix reports whether value is origin itself or a URL on it
// (origin followed by "/", "?" or "#"), returning what follows the origin.
// A bare prefix match is not enough: https://a.example must not match
// https://a.example.evil.com.
func cutOriginPrefix(value, origin string) (string, bool) {
	rest, ok := strings.CutPrefix(value, origin)
	if !ok {
		return "", false
	}
	if rest == "" || rest[0] == '/' || rest[0] == '?' || rest[0] == '#' {
		return rest, true
	}
	return "", false
}

// persistProxyJar applies the upstream's Set-Cookie values to the request's
// jar and, when that changes it, writes it back as the channel's login
// state (last write wins across concurrent requests). It never fails the
// response: errors are logged without any cookie value. A write is skipped
// once the channel's SSO configuration has changed since the session was
// created, so a request in flight cannot resurrect a discarded login state.
func persistProxyJar(s *Session, stored *model.ChannelSSOUpstreamSession, jar *Jar, setCookies []*http.Cookie) {
	before, err := jar.Marshal()
	if err != nil {
		common.SysError(common.LogText("channel sso: encode upstream login state for channel %d failed: %v", s.ChannelID, err))
		return
	}
	jar.Apply(setCookies)
	after, err := jar.Marshal()
	if err != nil {
		common.SysError(common.LogText("channel sso: encode upstream login state for channel %d failed: %v", s.ChannelID, err))
		return
	}
	if bytes.Equal(before, after) {
		return
	}
	cred, err := model.GetChannelSSOCredential(s.ChannelID)
	if err != nil || cred.UpdatedAt != s.CredentialUpdatedAt {
		return
	}
	user := &UpstreamUser{Raw: json.RawMessage(stored.UserSnapshot)}
	if err := saveUpstreamSession(s.ChannelID, jar, stored.UpstreamKind, user); err != nil {
		common.SysError(common.LogText("channel sso: save upstream login state for channel %d failed: %v", s.ChannelID, err))
	}
}
