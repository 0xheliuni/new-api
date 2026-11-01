// Package channelsso: this file holds the SSO session, one-time ticket,
// and subdomain-label store used by the admin SSO flow (Task 5) and the
// subdomain reverse proxy (Task 6). See docs/superpowers/specs/
// 2026-11-01-channel-sso-design.md §4.5.
//
// Two storage backends share the exact same semantics: Redis (required for
// multi-instance deployments, used when common.RedisEnabled) and an
// in-process map guarded by a mutex (single-instance fallback). Every
// expiry decision is made by comparing a record's own ExpiresAt field
// against nowFunc(), never by trusting the backing store's own TTL alone;
// the store TTL (Redis key expiry, or the periodic memory sweep) is only a
// best-effort cleanup mechanism so stale records do not accumulate
// forever. This lets tests drive expiry deterministically through nowFunc
// without real sleeps.
package channelsso

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strconv"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	goredis "github.com/go-redis/redis/v8"
)

// nowFunc returns the current time for every expiry and TTL computation in
// this file. Tests replace it to advance the clock without sleeping.
var nowFunc = time.Now

const (
	redisKeyPrefix = "channel_sso:"

	// pendingSessionTTL is how long a session created by
	// CreatePendingSession stays valid before ActivateSession is called
	// (design §4.5: 待定态 TTL 5 分钟).
	pendingSessionTTL = 5 * time.Minute

	// ticketTTL is how long an issued ticket remains consumable
	// (design §4.5: 票据 TTL 60 秒).
	ticketTTL = 60 * time.Second

	// ticketRandomBytes is the size of the raw random ticket value
	// before encoding (design §4.5: 32 字节随机值).
	ticketRandomBytes = 32

	// sessionIDRandomBytes is the size of the raw random Session.ID
	// value before hex-encoding (task-4 brief: 16 字节随机 hex).
	sessionIDRandomBytes = 16

	// hostLabelLength and hostLabelAlphabet produce the 8-character
	// lowercase-alphanumeric subdomain label (design §4.5).
	hostLabelLength   = 8
	hostLabelAlphabet = "abcdefghijklmnopqrstuvwxyz0123456789"

	// maxHostLabelAttempts bounds retries when a freshly generated label
	// collides with an existing active session's label.
	maxHostLabelAttempts = 5

	// channelIndexTTL bounds how long the per-channel session-index set
	// survives in Redis if it is never revoked or refreshed. It only
	// needs to outlive any realistic session lifetime; actual session
	// and ticket expiry is enforced via their own ExpiresAt fields, not
	// this backstop.
	channelIndexTTL = 24 * time.Hour

	// memCleanupInterval is how often the in-process memory store sweeps
	// expired sessions and tickets in the background.
	memCleanupInterval = time.Minute
)

// ErrSessionNotFound is returned by GetSession, GetSessionByLabel, and
// ConsumeTicket when the requested session does not exist, has expired, or
// (for GetSessionByLabel) is not an activated session.
var ErrSessionNotFound = errors.New("channel sso session not found")

// ErrTicketInvalid is returned by ConsumeTicket when the ticket does not
// exist, has already been consumed, has expired, or is not bound to the
// given label.
var ErrTicketInvalid = errors.New("channel sso ticket is invalid")

// Session is a Channel SSO proxy session. CreatePendingSession creates it
// in its pending form (design §4.5 step ①); ActivateSession turns it into
// an activated, proxy-routable session (step ③). The cookie jar holding
// upstream credentials is NOT part of Session; it is looked up separately
// by ChannelID (model.ChannelSSOUpstreamSession, Task 3).
type Session struct {
	ID                  string
	ChannelID           int
	AdminUserID         int
	BaseURL             string
	EntryPath           string
	CredentialUpdatedAt int64
	UpstreamKind        string
	User                UpstreamUser
	HostLabel           string
	Active              bool
	CreatedAt           int64
	ExpiresAt           int64
}

// ticketRecord is what IssueTicket stores, keyed by sha256(ticket). The
// raw ticket value itself is never persisted.
type ticketRecord struct {
	SessionID string
	Label     string
	ExpiresAt int64
}

func sessionKey(id string) string  { return redisKeyPrefix + "session:" + id }
func labelKey(label string) string { return redisKeyPrefix + "label:" + label }
func ticketKey(hash string) string { return redisKeyPrefix + "ticket:" + hash }
func channelKey(channelID int) string {
	return redisKeyPrefix + "channel:" + strconv.Itoa(channelID)
}

// memoryStore is the in-process fallback used when common.RedisEnabled is
// false. mem is the single package-level instance; tests reset its maps
// directly (same package) between cases.
type memoryStore struct {
	mu       sync.Mutex
	sessions map[string]*Session
	labels   map[string]string // host label -> session id
	tickets  map[string]ticketRecord
	channels map[int]map[string]struct{} // channel id -> session ids
}

var mem = &memoryStore{
	sessions: map[string]*Session{},
	labels:   map[string]string{},
	tickets:  map[string]ticketRecord{},
	channels: map[int]map[string]struct{}{},
}

// memCleanupOnce ensures the background sweep goroutine is started at most
// once for the lifetime of the process, regardless of how many times the
// memory store is used (including across tests), so it never leaks one
// goroutine per call.
var memCleanupOnce sync.Once

func startMemCleanup() {
	memCleanupOnce.Do(func() {
		go func() {
			ticker := time.NewTicker(memCleanupInterval)
			defer ticker.Stop()
			for range ticker.C {
				cleanupExpiredMemory()
			}
		}()
	})
}

// cleanupExpiredMemory removes sessions and tickets whose ExpiresAt has
// passed according to nowFunc. It is a best-effort backstop; correctness
// of reads never depends on it having run (see GetSession, ConsumeTicket).
func cleanupExpiredMemory() {
	now := nowFunc().Unix()
	mem.mu.Lock()
	defer mem.mu.Unlock()
	for id, s := range mem.sessions {
		if s.ExpiresAt > now {
			continue
		}
		removeSessionLocked(id, s)
	}
	for hash, rec := range mem.tickets {
		if rec.ExpiresAt <= now {
			delete(mem.tickets, hash)
		}
	}
}

// removeSessionLocked deletes a session and its label/channel-index
// entries. Callers must hold mem.mu.
func removeSessionLocked(id string, s *Session) {
	delete(mem.sessions, id)
	if s.HostLabel != "" {
		delete(mem.labels, s.HostLabel)
	}
	if set, ok := mem.channels[s.ChannelID]; ok {
		delete(set, id)
		if len(set) == 0 {
			delete(mem.channels, s.ChannelID)
		}
	}
}

// CreatePendingSession generates a new Session.ID (16 random bytes, hex
// encoded), sets it up as a pending session with a 5-minute TTL, and
// persists it. s.ChannelID, s.AdminUserID, s.BaseURL, s.EntryPath,
// s.CredentialUpdatedAt, s.UpstreamKind, and s.User must already be set by
// the caller; CreatePendingSession overwrites ID, Active, HostLabel,
// CreatedAt, and ExpiresAt.
func CreatePendingSession(s *Session) error {
	idBytes := make([]byte, sessionIDRandomBytes)
	if _, err := rand.Read(idBytes); err != nil {
		return err
	}
	s.ID = hex.EncodeToString(idBytes)
	s.Active = false
	s.HostLabel = ""
	now := nowFunc()
	s.CreatedAt = now.Unix()
	s.ExpiresAt = now.Add(pendingSessionTTL).Unix()

	if common.RedisEnabled {
		return createPendingSessionRedis(s)
	}
	createPendingSessionMemory(s)
	return nil
}

func createPendingSessionRedis(s *Session) error {
	data, err := common.Marshal(s)
	if err != nil {
		return err
	}
	ctx := context.Background()
	if err := common.RDB.Set(ctx, sessionKey(s.ID), data, pendingSessionTTL).Err(); err != nil {
		return err
	}
	return addToChannelIndexRedis(ctx, s.ChannelID, s.ID)
}

func addToChannelIndexRedis(ctx context.Context, channelID int, sessionID string) error {
	pipe := common.RDB.TxPipeline()
	pipe.SAdd(ctx, channelKey(channelID), sessionID)
	pipe.Expire(ctx, channelKey(channelID), channelIndexTTL)
	_, err := pipe.Exec(ctx)
	return err
}

func createPendingSessionMemory(s *Session) {
	startMemCleanup()
	mem.mu.Lock()
	defer mem.mu.Unlock()
	storeSessionLocked(s)
}

// storeSessionLocked stores a copy of s and (re)indexes it by channel.
// Callers must hold mem.mu.
func storeSessionLocked(s *Session) {
	cp := *s
	mem.sessions[s.ID] = &cp
	if mem.channels[s.ChannelID] == nil {
		mem.channels[s.ChannelID] = map[string]struct{}{}
	}
	mem.channels[s.ChannelID][s.ID] = struct{}{}
}

// GetSession returns the session with the given ID, or ErrSessionNotFound
// if it does not exist or has expired (per nowFunc, regardless of whether
// the backing store has physically removed it yet).
func GetSession(id string) (*Session, error) {
	if common.RedisEnabled {
		return getSessionRedis(id)
	}
	return getSessionMemory(id)
}

func getSessionRedis(id string) (*Session, error) {
	ctx := context.Background()
	data, err := common.RDB.Get(ctx, sessionKey(id)).Bytes()
	if errors.Is(err, goredis.Nil) {
		return nil, ErrSessionNotFound
	}
	if err != nil {
		return nil, err
	}
	var s Session
	if err := common.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	if s.ExpiresAt <= nowFunc().Unix() {
		_ = common.RDB.Del(ctx, sessionKey(id)).Err()
		return nil, ErrSessionNotFound
	}
	return &s, nil
}

func getSessionMemory(id string) (*Session, error) {
	mem.mu.Lock()
	defer mem.mu.Unlock()
	s, ok := mem.sessions[id]
	if !ok {
		return nil, ErrSessionNotFound
	}
	if s.ExpiresAt <= nowFunc().Unix() {
		removeSessionLocked(id, s)
		return nil, ErrSessionNotFound
	}
	cp := *s
	return &cp, nil
}

// ActivateSession turns a pending (or previously activated) session into
// an activated one: it assigns a unique HostLabel, sets Active=true, and
// resets ExpiresAt to now + Config.SessionTTL (fixed, does not renew after
// that). It builds the label->id index entry so GetSessionByLabel can find
// it.
func ActivateSession(id string) (*Session, error) {
	s, err := GetSession(id)
	if err != nil {
		return nil, err
	}
	label, err := assignUniqueHostLabel()
	if err != nil {
		return nil, err
	}
	now := nowFunc()
	s.HostLabel = label
	s.Active = true
	s.ExpiresAt = now.Add(Current().SessionTTL).Unix()

	if common.RedisEnabled {
		if err := activateSessionRedis(s); err != nil {
			return nil, err
		}
	} else {
		activateSessionMemory(s)
	}
	cp := *s
	return &cp, nil
}

// assignUniqueHostLabel generates an 8-character lowercase-alphanumeric
// label that does not collide with any existing active session's label,
// retrying up to maxHostLabelAttempts times.
func assignUniqueHostLabel() (string, error) {
	for range maxHostLabelAttempts {
		label, err := randomHostLabel()
		if err != nil {
			return "", err
		}
		if _, err := GetSessionByLabel(label); errors.Is(err, ErrSessionNotFound) {
			return label, nil
		}
	}
	return "", errors.New("channelsso: could not assign a unique host label after retries")
}

func randomHostLabel() (string, error) {
	buf := make([]byte, hostLabelLength)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	out := make([]byte, hostLabelLength)
	for i, b := range buf {
		out[i] = hostLabelAlphabet[int(b)%len(hostLabelAlphabet)]
	}
	return string(out), nil
}

func activateSessionRedis(s *Session) error {
	data, err := common.Marshal(s)
	if err != nil {
		return err
	}
	ttl := time.Until(time.Unix(s.ExpiresAt, 0))
	if ttl <= 0 {
		ttl = time.Second
	}
	ctx := context.Background()
	pipe := common.RDB.TxPipeline()
	pipe.Set(ctx, sessionKey(s.ID), data, ttl)
	pipe.Set(ctx, labelKey(s.HostLabel), s.ID, ttl)
	if _, err := pipe.Exec(ctx); err != nil {
		return err
	}
	return addToChannelIndexRedis(ctx, s.ChannelID, s.ID)
}

func activateSessionMemory(s *Session) {
	startMemCleanup()
	mem.mu.Lock()
	defer mem.mu.Unlock()
	storeSessionLocked(s)
	mem.labels[s.HostLabel] = s.ID
}

// GetSessionByLabel returns the activated, unexpired session for the given
// subdomain label, or ErrSessionNotFound if no such session exists, it has
// expired, or it is still pending.
func GetSessionByLabel(label string) (*Session, error) {
	id, err := lookupLabel(label)
	if err != nil {
		return nil, err
	}
	s, err := GetSession(id)
	if err != nil {
		return nil, ErrSessionNotFound
	}
	if !s.Active || s.HostLabel != label {
		return nil, ErrSessionNotFound
	}
	return s, nil
}

func lookupLabel(label string) (string, error) {
	if common.RedisEnabled {
		ctx := context.Background()
		id, err := common.RDB.Get(ctx, labelKey(label)).Result()
		if errors.Is(err, goredis.Nil) {
			return "", ErrSessionNotFound
		}
		if err != nil {
			return "", err
		}
		return id, nil
	}
	mem.mu.Lock()
	defer mem.mu.Unlock()
	id, ok := mem.labels[label]
	if !ok {
		return "", ErrSessionNotFound
	}
	return id, nil
}

// IssueTicket mints a new one-time ticket for the given session and label,
// returning the raw ticket value (32 random bytes, base64url encoded). It
// stores only sha256(ticket), bound to the session and label, with a
// 60-second TTL.
func IssueTicket(sessionID, label string) (string, error) {
	buf := make([]byte, ticketRandomBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	ticket := base64.RawURLEncoding.EncodeToString(buf)
	hash := ticketHash(ticket)
	rec := ticketRecord{
		SessionID: sessionID,
		Label:     label,
		ExpiresAt: nowFunc().Add(ticketTTL).Unix(),
	}

	if common.RedisEnabled {
		if err := issueTicketRedis(hash, rec); err != nil {
			return "", err
		}
	} else {
		issueTicketMemory(hash, rec)
	}
	return ticket, nil
}

func ticketHash(ticket string) string {
	sum := sha256.Sum256([]byte(ticket))
	return hex.EncodeToString(sum[:])
}

func issueTicketRedis(hash string, rec ticketRecord) error {
	data, err := common.Marshal(rec)
	if err != nil {
		return err
	}
	return common.RDB.Set(context.Background(), ticketKey(hash), data, ticketTTL).Err()
}

func issueTicketMemory(hash string, rec ticketRecord) {
	startMemCleanup()
	mem.mu.Lock()
	defer mem.mu.Unlock()
	mem.tickets[hash] = rec
}

// ConsumeTicket atomically retrieves and deletes the ticket (Redis GETDEL,
// or a mutex-guarded delete in memory) so every ticket is usable at most
// once, even when validation below fails. It returns ErrTicketInvalid if
// the ticket does not exist, already was consumed, has expired, or is not
// bound to label; otherwise it returns the referenced session (which may
// itself be ErrSessionNotFound if it expired first).
func ConsumeTicket(ticket, label string) (*Session, error) {
	hash := ticketHash(ticket)
	var rec ticketRecord
	var found bool
	if common.RedisEnabled {
		r, ok, err := consumeTicketRecordRedis(hash)
		if err != nil {
			return nil, err
		}
		rec, found = r, ok
	} else {
		rec, found = consumeTicketRecordMemory(hash)
	}
	if !found || rec.ExpiresAt <= nowFunc().Unix() || rec.Label != label {
		return nil, ErrTicketInvalid
	}
	return GetSession(rec.SessionID)
}

func consumeTicketRecordRedis(hash string) (ticketRecord, bool, error) {
	ctx := context.Background()
	data, err := common.RDB.GetDel(ctx, ticketKey(hash)).Bytes()
	if errors.Is(err, goredis.Nil) {
		return ticketRecord{}, false, nil
	}
	if err != nil {
		return ticketRecord{}, false, err
	}
	var rec ticketRecord
	if err := common.Unmarshal(data, &rec); err != nil {
		return ticketRecord{}, false, err
	}
	return rec, true, nil
}

func consumeTicketRecordMemory(hash string) (ticketRecord, bool) {
	mem.mu.Lock()
	defer mem.mu.Unlock()
	rec, ok := mem.tickets[hash]
	if ok {
		delete(mem.tickets, hash)
	}
	return rec, ok
}

// RevokeChannelSessions invalidates every SSO session (pending or
// activated) belonging to channelID, so none of them remain reachable by
// ID or by subdomain label. Callers use this when the channel's SSO
// credential changes or the channel is deleted.
func RevokeChannelSessions(channelID int) error {
	if common.RedisEnabled {
		return revokeChannelSessionsRedis(channelID)
	}
	revokeChannelSessionsMemory(channelID)
	return nil
}

func revokeChannelSessionsRedis(channelID int) error {
	ctx := context.Background()
	ids, err := common.RDB.SMembers(ctx, channelKey(channelID)).Result()
	if err != nil {
		return err
	}
	for _, id := range ids {
		if data, getErr := common.RDB.Get(ctx, sessionKey(id)).Bytes(); getErr == nil {
			var s Session
			if unmarshalErr := common.Unmarshal(data, &s); unmarshalErr == nil && s.HostLabel != "" {
				_ = common.RDB.Del(ctx, labelKey(s.HostLabel)).Err()
			}
		}
		_ = common.RDB.Del(ctx, sessionKey(id)).Err()
	}
	return common.RDB.Del(ctx, channelKey(channelID)).Err()
}

func revokeChannelSessionsMemory(channelID int) {
	mem.mu.Lock()
	defer mem.mu.Unlock()
	ids := mem.channels[channelID]
	for id := range ids {
		if s, ok := mem.sessions[id]; ok {
			if s.HostLabel != "" {
				delete(mem.labels, s.HostLabel)
			}
			delete(mem.sessions, id)
		}
	}
	delete(mem.channels, channelID)
}
