// Package channelsso: this file holds the server-side cookie jar used to
// hold an upstream new-api site's login cookies between requests. The
// browser never sees these cookies; see upstream.go for the HTTP client that
// applies and sends them.
package channelsso

import (
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
)

// StoredCookie is one cookie held in a Jar. Expires is a Unix second
// timestamp, or zero for a session cookie that never expires on its own
// (it lasts until the upstream overwrites or deletes it).
type StoredCookie struct {
	Name     string
	Value    string
	Path     string
	Expires  int64
	HttpOnly bool
}

// Jar holds the cookies a Channel SSO upstream session has set for us. It is
// sealed (see crypto.go) before being persisted, and MUST never be logged or
// returned in any API response: its cookies are upstream session secrets.
type Jar struct {
	Cookies []StoredCookie
}

// Apply merges a response's Set-Cookie values into the jar: a cookie with
// the same Name and Path is replaced in place, a new one is appended, and a
// cookie that has been told to expire (MaxAge < 0, or an Expires time that
// has already passed) is removed instead. Call it with every response the
// Upstream client receives, including a non-2xx one — upstream may clear a
// cookie on a failed request too.
func (j *Jar) Apply(setCookies []*http.Cookie) {
	now := time.Now()
	for _, sc := range setCookies {
		if sc == nil || sc.Name == "" {
			continue
		}
		path := sc.Path
		if path == "" {
			path = "/"
		}
		idx := j.indexOf(sc.Name, path)
		if sc.MaxAge < 0 || (sc.MaxAge == 0 && !sc.Expires.IsZero() && sc.Expires.Before(now)) {
			if idx >= 0 {
				j.Cookies = slices.Delete(j.Cookies, idx, idx+1)
			}
			continue
		}
		var expiresAt int64
		switch {
		case sc.MaxAge > 0:
			expiresAt = now.Add(time.Duration(sc.MaxAge) * time.Second).Unix()
		case !sc.Expires.IsZero():
			expiresAt = sc.Expires.Unix()
		}
		stored := StoredCookie{Name: sc.Name, Value: sc.Value, Path: path, Expires: expiresAt, HttpOnly: sc.HttpOnly}
		if idx >= 0 {
			j.Cookies[idx] = stored
		} else {
			j.Cookies = append(j.Cookies, stored)
		}
	}
}

// Header builds the Cookie request header to send for a request to path:
// every stored cookie whose Path is a prefix match for path (the same
// algorithm browsers use, RFC 6265 §5.1.4), excluding any cookie that has
// expired. It returns "" when nothing matches, never a trailing separator.
func (j *Jar) Header(path string) string {
	now := time.Now().Unix()
	var parts []string
	for _, c := range j.Cookies {
		if c.Expires != 0 && c.Expires <= now {
			continue
		}
		if !cookiePathMatches(c.Path, path) {
			continue
		}
		parts = append(parts, c.Name+"="+c.Value)
	}
	return strings.Join(parts, "; ")
}

// Marshal serializes the jar for sealed storage (see crypto.go Seal). The
// result is plaintext JSON and MUST be sealed before it is persisted or ever
// leaves process memory.
func (j *Jar) Marshal() ([]byte, error) {
	return common.Marshal(j)
}

// UnmarshalJar reverses Marshal.
func UnmarshalJar(data []byte) (*Jar, error) {
	var j Jar
	if err := common.Unmarshal(data, &j); err != nil {
		return nil, err
	}
	return &j, nil
}

func (j *Jar) indexOf(name, path string) int {
	for i, c := range j.Cookies {
		if c.Name == name && c.Path == path {
			return i
		}
	}
	return -1
}

// cookiePathMatches implements the RFC 6265 §5.1.4 path-match algorithm:
// cookiePath matches requestPath when they are equal, or requestPath has
// cookiePath as a prefix that either ends with "/" or is immediately
// followed by "/" in requestPath.
func cookiePathMatches(cookiePath, requestPath string) bool {
	if cookiePath == "" {
		cookiePath = "/"
	}
	if requestPath == cookiePath {
		return true
	}
	if !strings.HasPrefix(requestPath, cookiePath) {
		return false
	}
	if strings.HasSuffix(cookiePath, "/") {
		return true
	}
	return requestPath[len(cookiePath)] == '/'
}
