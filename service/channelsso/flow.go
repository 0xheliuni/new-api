// Package channelsso: this file orchestrates the admin-facing three-step SSO
// flow (check → validate → open, design §4.3–4.5) and the channel SSO
// configuration lifecycle on top of the credential tables (model), the
// upstream client (upstream.go), and the session/ticket store (store.go).
package channelsso

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
)

// Errors returned by the flow. They double as frontend i18n keys, so their
// text MUST stay in English and MUST NOT be changed casually.
var (
	ErrSessionForbidden  = errors.New("This SSO session does not belong to you")
	ErrCredentialChanged = errors.New("Channel SSO settings changed; please start again")
	ErrNotConfigured     = errors.New("Channel SSO is not configured")
	ErrChannelNotFound   = errors.New("Channel does not exist")
	ErrInvalidBaseURL    = errors.New("Upstream URL is invalid or not allowed")
	ErrInvalidEntryPath  = errors.New("Entry path must start with / and must not contain // or \\")
	ErrConfigIncomplete  = errors.New("Upstream URL, username and password are required")
)

const (
	defaultEntryPath = "/dashboard"

	// forgetChannelTimeout bounds the best-effort upstream logout done when
	// a channel is deleted, so channel deletion is never held up by an
	// unresponsive upstream.
	forgetChannelTimeout = 10 * time.Second

	maxBaseURLLength   = 512
	maxUsernameLength  = 255
	maxEntryPathLength = 255
)

// ConfigView is the channel SSO configuration as shown to the admin. It
// never carries the password or its ciphertext.
type ConfigView struct {
	Configured  bool   `json:"configured"`
	BaseURL     string `json:"base_url"`
	Username    string `json:"username"`
	EntryPath   string `json:"entry_path"`
	HasPassword bool   `json:"has_password"`
}

// SaveConfigInput is the admin's configuration change. An empty Password
// keeps the stored one; Clear removes the whole configuration.
type SaveConfigInput struct {
	BaseURL   string `json:"base_url"`
	Username  string `json:"username"`
	Password  string `json:"password"`
	EntryPath string `json:"entry_path"`
	Clear     bool   `json:"clear"`
}

// CheckResult is the outcome of step ①.
type CheckResult struct {
	SessionID string `json:"session_id"`
	SiteName  string `json:"site_name"`
	Username  string `json:"username"`
	Role      int    `json:"role"`
	Reused    bool   `json:"reused"`
}

// GetConfig returns the channel's SSO configuration view.
func GetConfig(channelID int) (*ConfigView, error) {
	cred, err := model.GetChannelSSOCredential(channelID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return &ConfigView{}, nil
	}
	if err != nil {
		return nil, err
	}
	return &ConfigView{
		Configured:  true,
		BaseURL:     cred.BaseURL,
		Username:    cred.Username,
		EntryPath:   cred.EntryPath,
		HasPassword: cred.PasswordCipher != "",
	}, nil
}

// SaveConfig stores or clears the channel's SSO configuration. Any stored
// upstream login state is logged out (best-effort) and discarded first, and
// every SSO session of the channel is revoked afterwards, so nothing made
// with the previous configuration outlives the change.
func SaveConfig(ctx context.Context, channelID int, in SaveConfigInput) error {
	if _, err := model.GetChannelById(channelID, false); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrChannelNotFound
		}
		return err
	}

	var cred *model.ChannelSSOCredential
	if !in.Clear {
		username := strings.TrimSpace(in.Username)
		if username == "" || len(username) > maxUsernameLength {
			return ErrConfigIncomplete
		}
		up, err := newChannelUpstream(strings.TrimSpace(in.BaseURL))
		if err != nil {
			return err
		}
		if len(up.BaseURL) > maxBaseURLLength {
			return ErrInvalidBaseURL
		}
		entryPath := strings.TrimSpace(in.EntryPath)
		if entryPath == "" {
			entryPath = defaultEntryPath
		}
		if !strings.HasPrefix(entryPath, "/") || strings.Contains(entryPath, "//") || strings.Contains(entryPath, `\`) || len(entryPath) > maxEntryPathLength {
			return ErrInvalidEntryPath
		}
		existing, err := model.GetChannelSSOCredential(channelID)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		passwordCipher := ""
		if existing != nil {
			passwordCipher = existing.PasswordCipher
		}
		if in.Password != "" {
			passwordCipher, err = Seal([]byte(in.Password))
			if err != nil {
				return err
			}
		}
		if passwordCipher == "" {
			return ErrConfigIncomplete
		}
		cred = &model.ChannelSSOCredential{
			ChannelId:      channelID,
			BaseURL:        up.BaseURL,
			Username:       username,
			PasswordCipher: passwordCipher,
			EntryPath:      entryPath,
		}
	}

	// The stored login state belongs to the previous account; it must not
	// survive into the new configuration, so a failed delete aborts.
	if err := logoutUpstreamSession(ctx, channelID); err != nil {
		common.SysError(common.LogText("channel sso: upstream logout for channel %d failed: %v", channelID, err))
	}
	if err := model.DeleteChannelSSOUpstreamSession(channelID); err != nil {
		return err
	}
	if in.Clear {
		if err := model.DeleteChannelSSOCredential(channelID); err != nil {
			return err
		}
	} else if err := model.SaveChannelSSOCredential(cred); err != nil {
		return err
	}
	return RevokeChannelSessions(channelID)
}

// ForgetChannel discards everything Channel SSO holds for a channel that is
// about to be deleted: it logs the stored upstream login state out, deletes
// it, and revokes the channel's SSO sessions. It is best-effort and bounded
// by forgetChannelTimeout; failures are logged and never block deletion.
func ForgetChannel(ctx context.Context, channelID int) {
	ctx, cancel := context.WithTimeout(ctx, forgetChannelTimeout)
	defer cancel()
	if err := logoutUpstreamSession(ctx, channelID); err != nil {
		common.SysError(common.LogText("channel sso: upstream logout for channel %d failed: %v", channelID, err))
	}
	if err := model.DeleteChannelSSOUpstreamSession(channelID); err != nil {
		common.SysError(common.LogText("channel sso: delete upstream login state for channel %d failed: %v", channelID, err))
	}
	if err := RevokeChannelSessions(channelID); err != nil {
		common.SysError(common.LogText("channel sso: revoke sessions for channel %d failed: %v", channelID, err))
	}
}

// Check runs step ①: it confirms the upstream is a reachable new-api site,
// reuses the channel's stored upstream login state when it is still valid
// (or logs in with the stored credential otherwise and stores the new
// state), and creates a pending SSO session bound to adminUserID and to the
// credential version it used.
func Check(ctx context.Context, channelID, adminUserID int) (*CheckResult, error) {
	cred, err := loadCredential(channelID)
	if err != nil {
		return nil, err
	}
	up, err := newChannelUpstream(cred.BaseURL)
	if err != nil {
		return nil, err
	}
	siteName, err := up.Status(ctx)
	if err != nil {
		return nil, err
	}

	reused := false
	var jar *Jar
	var kind string
	var user *UpstreamUser
	if stored, err := model.GetChannelSSOUpstreamSession(channelID); err == nil {
		jar, user, err = resumeUpstreamSession(ctx, up, stored)
		reused = err == nil
		kind = stored.UpstreamKind
	}
	if !reused {
		password, err := Open(cred.PasswordCipher)
		if err != nil {
			return nil, err
		}
		jar = &Jar{}
		result, err := up.Login(ctx, jar, cred.Username, string(password))
		if err != nil {
			return nil, err
		}
		kind, user = result.Kind, &result.User
	}
	if err := saveUpstreamSession(channelID, jar, kind, user); err != nil {
		return nil, err
	}
	// A configuration change that landed while we were logging in must not
	// leave this login state behind for the new configuration to reuse.
	if latest, err := loadCredential(channelID); err != nil || latest.UpdatedAt != cred.UpdatedAt {
		ForgetChannel(ctx, channelID)
		return nil, ErrCredentialChanged
	}

	s := &Session{
		ChannelID:           channelID,
		AdminUserID:         adminUserID,
		BaseURL:             up.BaseURL,
		EntryPath:           cred.EntryPath,
		CredentialUpdatedAt: cred.UpdatedAt,
		UpstreamKind:        kind,
		User:                *user,
	}
	if err := CreatePendingSession(s); err != nil {
		return nil, err
	}
	return &CheckResult{SessionID: s.ID, SiteName: siteName, Username: user.Username, Role: user.Role, Reused: reused}, nil
}

// Validate runs step ②: it confirms the session belongs to adminUserID and
// was made with the current credential, then proves the stored upstream
// login state still works by calling the upstream /api/user/self.
func Validate(ctx context.Context, sessionID string, adminUserID int) (*UpstreamUser, error) {
	s, err := authorizeSession(sessionID, adminUserID)
	if err != nil {
		return nil, err
	}
	up, err := newChannelUpstream(s.BaseURL)
	if err != nil {
		return nil, err
	}
	stored, err := model.GetChannelSSOUpstreamSession(s.ChannelID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrSessionInvalid
	}
	if err != nil {
		return nil, err
	}
	jar, user, err := resumeUpstreamSession(ctx, up, stored)
	if err != nil {
		return nil, err
	}
	if user.Id != s.User.Id {
		return nil, ErrSessionInvalid
	}
	if err := saveUpstreamSession(s.ChannelID, jar, stored.UpstreamKind, user); err != nil {
		return nil, err
	}
	return user, nil
}

// OpenSession runs step ③: after the same ownership and credential checks
// as Validate, it activates the session (assigning its random subdomain)
// and issues a one-time ticket, returning the URL that enters the proxy.
func OpenSession(ctx context.Context, sessionID string, adminUserID int) (string, *Session, error) {
	if _, err := authorizeSession(sessionID, adminUserID); err != nil {
		return "", nil, err
	}
	active, err := ActivateSession(sessionID)
	if err != nil {
		return "", nil, err
	}
	ticket, err := IssueTicket(active.ID, active.HostLabel)
	if err != nil {
		return "", nil, err
	}
	return ProxyOrigin(active.HostLabel) + "/__sso/enter?ticket=" + ticket, active, nil
}

// authorizeSession loads a session and enforces that it belongs to
// adminUserID and was created with the channel's current credential.
func authorizeSession(sessionID string, adminUserID int) (*Session, error) {
	s, err := GetSession(sessionID)
	if err != nil {
		return nil, err
	}
	if s.AdminUserID != adminUserID {
		return nil, ErrSessionForbidden
	}
	cred, err := model.GetChannelSSOCredential(s.ChannelID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrCredentialChanged
	}
	if err != nil {
		return nil, err
	}
	if cred.UpdatedAt != s.CredentialUpdatedAt {
		return nil, ErrCredentialChanged
	}
	return s, nil
}

func loadCredential(channelID int) (*model.ChannelSSOCredential, error) {
	cred, err := model.GetChannelSSOCredential(channelID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotConfigured
	}
	return cred, err
}

// newChannelUpstream builds the upstream client for a configured base URL,
// folding every rejection other than the https requirement into
// ErrInvalidBaseURL so callers get a stable message.
func newChannelUpstream(baseURL string) (*Upstream, error) {
	up, err := NewUpstream(baseURL)
	if err == nil || errors.Is(err, ErrUpstreamNotHTTPS) {
		return up, err
	}
	return nil, fmt.Errorf("%w: %v", ErrInvalidBaseURL, err)
}

// resumeUpstreamSession proves a stored upstream login state still works: a
// modern upstream first exchanges the refresh cookie for a fresh access
// token (access tokens are never stored), then /api/user/self is called.
// The returned jar carries any cookies the upstream rotated meanwhile.
func resumeUpstreamSession(ctx context.Context, up *Upstream, stored *model.ChannelSSOUpstreamSession) (*Jar, *UpstreamUser, error) {
	jar, err := openStoredJar(stored)
	if err != nil {
		return nil, nil, err
	}
	snapshot, err := parseUpstreamUser([]byte(stored.UserSnapshot))
	if err != nil {
		return nil, nil, ErrSessionInvalid
	}
	accessToken := ""
	if stored.UpstreamKind == "modern" {
		accessToken, err = up.Refresh(ctx, jar)
		if err != nil {
			return nil, nil, err
		}
	}
	user, err := up.Self(ctx, jar, stored.UpstreamKind, accessToken, snapshot.Id)
	if err != nil {
		return nil, nil, err
	}
	return jar, user, nil
}

func openStoredJar(stored *model.ChannelSSOUpstreamSession) (*Jar, error) {
	plaintext, err := Open(stored.CookieJarCipher)
	if err != nil {
		return nil, err
	}
	return UnmarshalJar(plaintext)
}

func saveUpstreamSession(channelID int, jar *Jar, kind string, user *UpstreamUser) error {
	plaintext, err := jar.Marshal()
	if err != nil {
		return err
	}
	sealed, err := Seal(plaintext)
	if err != nil {
		return err
	}
	return model.SaveChannelSSOUpstreamSession(&model.ChannelSSOUpstreamSession{
		ChannelId:       channelID,
		CookieJarCipher: sealed,
		UserSnapshot:    string(user.Raw),
		UpstreamKind:    kind,
	})
}

// logoutUpstreamSession ends the channel's stored upstream login state at
// the upstream. It does not delete the stored row; callers do that, and
// treat a logout failure as best-effort (log and continue).
func logoutUpstreamSession(ctx context.Context, channelID int) error {
	stored, err := model.GetChannelSSOUpstreamSession(channelID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	cred, err := model.GetChannelSSOCredential(channelID)
	if err != nil {
		return err
	}
	up, err := newChannelUpstream(cred.BaseURL)
	if err != nil {
		return err
	}
	jar, err := openStoredJar(stored)
	if err != nil {
		return err
	}
	return up.Logout(ctx, jar, stored.UpstreamKind)
}
