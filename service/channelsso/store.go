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
	"fmt"
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

	// channelIndexTTL is the floor for how long the per-channel
	// session-index set survives in Redis: see channelIndexExpiry, which
	// also accounts for the configured SessionTTL. It only needs to
	// outlive any realistic session lifetime; actual session and ticket
	// expiry is enforced via their own ExpiresAt fields, not this
	// backstop.
	channelIndexTTL = 24 * time.Hour

	// channelIndexMargin is added on top of the configured SessionTTL
	// (see channelIndexExpiry) so the index cannot expire while a
	// session using that TTL is still logically alive.
	channelIndexMargin = time.Hour

	// memCleanupInterval is how often the in-process memory store sweeps
	// expired sessions and tickets in the background.
	memCleanupInterval = time.Minute
)

// ErrSessionNotFound is returned by GetSession, GetSessionByLabel, and
// ConsumeTicket when the requested session does not exist, has expired, or
// (for GetSessionByLabel) is not an activated session.
var ErrSessionNotFound = errors.New("channel sso session not found")

// ErrSessionAlreadyActive is returned by ActivateSession when the session
// has already been activated. A session's ExpiresAt and HostLabel are
// fixed at activation and never slide forward on a later call, so callers
// must not call ActivateSession a second time for the same id and must
// treat this error as a caller bug, not a reason to retry.
var ErrSessionAlreadyActive = errors.New("channel sso session is already active")

// errHostLabelTaken is an internal retry signal, never returned to
// callers. ActivateSession generates a fresh random label per attempt and
// relies entirely on the atomic backend check (a Redis EVAL, or the
// memory store's single critical section in activateSessionMemory) to
// decide whether that label is actually free; on a collision it retries
// with a new random label, up to maxHostLabelAttempts times.
var errHostLabelTaken = errors.New("channelsso: host label is taken")

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

// activeMarkerKey is a small sentinel key (not the main session JSON blob)
// set only once activateSessionScript successfully activates a session.
// It exists so that script can reject a second activation attempt with a
// plain EXISTS check instead of having to parse the session JSON in Lua.
func activeMarkerKey(id string) string { return redisKeyPrefix + "active:" + id }

// channelIndexExpiry returns how long the per-channel session-index Redis
// set should live: at least channelIndexTTL, but never less than the
// currently configured SessionTTL plus channelIndexMargin. Without this
// floor, an operator raising SSO_SESSION_TTL above channelIndexTTL would
// make the index key expire while an activated session using that TTL is
// still logically alive, so RevokeChannelSessions would find nothing to
// revoke for it.
func channelIndexExpiry() time.Duration {
	if floor := Current().SessionTTL + channelIndexMargin; floor > channelIndexTTL {
		return floor
	}
	return channelIndexTTL
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
	pipe.Expire(ctx, channelKey(channelID), channelIndexExpiry())
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

// ActivateSession turns a pending session into an activated one: it
// assigns a unique HostLabel, sets Active=true, and resets ExpiresAt to
// now + Config.SessionTTL (fixed, does not renew after that). It builds
// the label->id index entry so GetSessionByLabel can find it.
//
// The whole check-and-mutate step is atomic per backend (a single Redis
// EVAL, or one critical section under the memory store's mutex), so it
// returns ErrSessionNotFound if the session was deleted or revoked after
// the caller last observed it, and ErrSessionAlreadyActive if it was
// already activated (by this call or a concurrent one) — in both cases
// without mutating anything. ActivateSession must not be called a second
// time for the same id; doing so is rejected rather than re-extending the
// session's lifetime or assigning it a second label.
func ActivateSession(id string) (*Session, error) {
	for range maxHostLabelAttempts {
		label, err := randomHostLabel()
		if err != nil {
			return nil, err
		}
		now := nowFunc()
		expiresAt := now.Add(Current().SessionTTL).Unix()

		var s *Session
		if common.RedisEnabled {
			s, err = activateSessionRedis(id, label, expiresAt)
		} else {
			s, err = activateSessionMemory(id, label, expiresAt)
		}
		if err == nil {
			return s, nil
		}
		if !errors.Is(err, errHostLabelTaken) {
			return nil, err
		}
	}
	return nil, errors.New("channelsso: could not assign a unique host label after retries")
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

// activateSessionScript atomically transitions a pending session into an
// activated one, or fails without any side effect if the session no
// longer exists (deleted, expired, or revoked between the caller's last
// read and this call), is already active, or the chosen label is taken.
// EVAL already guarantees the whole script runs without any other Redis
// command interleaving, so the EXISTS-then-SET checks below are exact —
// no separate WATCH/MULTI round trip or optimistic retry is needed. The
// already-active check reads a dedicated marker key (KEYS[2]) rather than
// the session JSON blob itself, so the script never needs to parse JSON.
//
// KEYS: 1=session key, 2=active-marker key, 3=label key, 4=channel-index key
// ARGV: 1=new session JSON, 2=TTL seconds for session/marker/label,
//
//	3=session id, 4=channel-index TTL seconds
//
// Returns: 1 ok, -1 session not found, -2 already active, -3 label taken.
const activateSessionScript = `
if redis.call('EXISTS', KEYS[1]) == 0 then
  return -1
end
if redis.call('EXISTS', KEYS[2]) == 1 then
  return -2
end
if redis.call('EXISTS', KEYS[3]) == 1 then
  return -3
end
redis.call('SET', KEYS[1], ARGV[1], 'EX', ARGV[2])
redis.call('SET', KEYS[2], '1', 'EX', ARGV[2])
redis.call('SET', KEYS[3], ARGV[3], 'EX', ARGV[2])
redis.call('SADD', KEYS[4], ARGV[3])
redis.call('EXPIRE', KEYS[4], ARGV[4])
return 1`

func activateSessionRedis(id, label string, expiresAt int64) (*Session, error) {
	current, err := getSessionRedis(id)
	if err != nil {
		return nil, err
	}
	candidate := *current
	candidate.HostLabel = label
	candidate.Active = true
	candidate.ExpiresAt = expiresAt
	data, err := common.Marshal(&candidate)
	if err != nil {
		return nil, err
	}
	ttlSeconds := int64(time.Until(time.Unix(expiresAt, 0)).Seconds())
	if ttlSeconds <= 0 {
		ttlSeconds = 1
	}

	result, err := common.RDB.Eval(context.Background(), activateSessionScript,
		[]string{sessionKey(id), activeMarkerKey(id), labelKey(label), channelKey(candidate.ChannelID)},
		data, ttlSeconds, id, int64(channelIndexExpiry().Seconds()),
	).Int64()
	if err != nil {
		return nil, err
	}
	switch result {
	case 1:
		return &candidate, nil
	case -1:
		return nil, ErrSessionNotFound
	case -2:
		return nil, ErrSessionAlreadyActive
	case -3:
		return nil, errHostLabelTaken
	default:
		return nil, fmt.Errorf("channelsso: unexpected activateSessionScript result %d", result)
	}
}

// activateSessionMemory performs the same check-and-mutate step as
// activateSessionRedis, but as one critical section under mem.mu: the
// existence/expiry/active checks and the mutation happen without
// releasing the lock in between, so a concurrent revokeChannelSessionsMemory
// or another activateSessionMemory call can never interleave with it.
func activateSessionMemory(id, label string, expiresAt int64) (*Session, error) {
	startMemCleanup()
	mem.mu.Lock()
	defer mem.mu.Unlock()

	s, ok := mem.sessions[id]
	if ok && s.ExpiresAt <= nowFunc().Unix() {
		removeSessionLocked(id, s)
		ok = false
	}
	if !ok {
		return nil, ErrSessionNotFound
	}
	if s.Active {
		return nil, ErrSessionAlreadyActive
	}
	if _, taken := mem.labels[label]; taken {
		return nil, errHostLabelTaken
	}

	s.HostLabel = label
	s.Active = true
	s.ExpiresAt = expiresAt
	mem.labels[label] = id
	if mem.channels[s.ChannelID] == nil {
		mem.channels[s.ChannelID] = map[string]struct{}{}
	}
	mem.channels[s.ChannelID][id] = struct{}{}

	cp := *s
	return &cp, nil
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
// the ticket does not exist, already was consumed, has expired, is not
// bound to label, or no longer resolves to an activated session bound to
// that same label (for example because the session was revoked or
// somehow still pending); otherwise it returns the referenced session.
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
	s, err := GetSession(rec.SessionID)
	if err != nil {
		return nil, err
	}
	if !s.Active || s.HostLabel != label {
		return nil, ErrTicketInvalid
	}
	return s, nil
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

// revokeChannelSessionsRedis deletes every session, its active marker, and
// (when known) its label index entry, for every id found in the
// channel's index set, then removes the index itself. It fails closed:
// any real error reading a session, decoding it, or executing the delete
// pipeline is collected and returned (via errors.Join) rather than
// swallowed, because this is a security control (it must make every
// session for the channel unreachable) and a caller that ignores a
// returned error must not be able to mistake partial success for
// complete revocation.
func revokeChannelSessionsRedis(channelID int) error {
	ctx := context.Background()
	ids, err := common.RDB.SMembers(ctx, channelKey(channelID)).Result()
	if err != nil {
		return err
	}

	var errs []error
	pipe := common.RDB.TxPipeline()
	for _, id := range ids {
		data, getErr := common.RDB.Get(ctx, sessionKey(id)).Bytes()
		switch {
		case errors.Is(getErr, goredis.Nil):
			// Already gone; still delete the id's keys below so a
			// leftover active-marker or label key cannot survive it.
		case getErr != nil:
			errs = append(errs, fmt.Errorf("channelsso: revoke: read session %s: %w", id, getErr))
		default:
			var s Session
			if unmarshalErr := common.Unmarshal(data, &s); unmarshalErr != nil {
				errs = append(errs, fmt.Errorf("channelsso: revoke: decode session %s: %w", id, unmarshalErr))
			} else if s.HostLabel != "" {
				pipe.Del(ctx, labelKey(s.HostLabel))
			}
		}
		pipe.Del(ctx, sessionKey(id))
		pipe.Del(ctx, activeMarkerKey(id))
	}
	pipe.Del(ctx, channelKey(channelID))
	if _, err := pipe.Exec(ctx); err != nil {
		errs = append(errs, fmt.Errorf("channelsso: revoke: delete pipeline: %w", err))
	}
	return errors.Join(errs...)
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
