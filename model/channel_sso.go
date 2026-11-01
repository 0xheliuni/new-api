package model

// Channel SSO data model: this file persists the two tables that back the
// Channel SSO feature (service/channelsso) — the upstream back-office login
// credential per channel, and the (optional) upstream cookie-jar session
// reused across activations so the gateway does not have to re-authenticate
// upstream every time an admin opens the proxy. Secrets stored here
// (PasswordCipher, CookieJarCipher) are always sealed ciphertext produced by
// channelsso.Seal and MUST never be exposed through the JSON tag, logs, or
// audit records.

import (
	"time"

	"github.com/samber/lo"
	"gorm.io/gorm/clause"
)

// ChannelSSOCredential holds the upstream back-office login credential used
// to establish a Channel SSO session for one channel. There is at most one
// row per channel (ChannelId is uniquely indexed).
type ChannelSSOCredential struct {
	Id        int    `json:"id"`
	ChannelId int    `json:"channel_id" gorm:"uniqueIndex"`
	BaseURL   string `json:"base_url" gorm:"type:varchar(512)"`
	Username  string `json:"username" gorm:"type:varchar(255)"`
	// PasswordCipher is ciphertext sealed with channelsso.Seal; it must
	// never be serialized back to API clients.
	PasswordCipher string `json:"-" gorm:"type:text"`
	EntryPath      string `json:"entry_path" gorm:"type:varchar(255)"`
	UpdatedAt      int64  `json:"updated_at" gorm:"bigint"`
}

// ChannelSSOUpstreamSession holds the sealed upstream cookie jar and a
// snapshot of the authenticated upstream user for one channel, so an
// already-active upstream login can be reused without re-authenticating.
// There is at most one row per channel (ChannelId is uniquely indexed).
type ChannelSSOUpstreamSession struct {
	Id        int `json:"id"`
	ChannelId int `json:"channel_id" gorm:"uniqueIndex"`
	// CookieJarCipher is ciphertext sealed with channelsso.Seal; it must
	// never be serialized back to API clients.
	CookieJarCipher string `json:"-" gorm:"type:text"`
	UserSnapshot    string `json:"user_snapshot" gorm:"type:text"`
	// UpstreamKind is "modern" or "legacy".
	UpstreamKind string `json:"upstream_kind" gorm:"type:varchar(16)"`
	UpdatedAt    int64  `json:"updated_at" gorm:"bigint"`
}

// GetChannelSSOCredential returns the stored credential for channelId, or
// gorm.ErrRecordNotFound if none exists.
func GetChannelSSOCredential(channelId int) (*ChannelSSOCredential, error) {
	var credential ChannelSSOCredential
	if err := DB.Where("channel_id = ?", channelId).First(&credential).Error; err != nil {
		return nil, err
	}
	return &credential, nil
}

// SaveChannelSSOCredential upserts c by its ChannelId, setting UpdatedAt to
// now. Safe to call concurrently across the three supported databases.
func SaveChannelSSOCredential(c *ChannelSSOCredential) error {
	c.UpdatedAt = time.Now().UnixMilli()
	return DB.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "channel_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"base_url", "username", "password_cipher", "entry_path", "updated_at",
		}),
	}).Create(c).Error
}

// DeleteChannelSSOCredential removes the stored credential for channelId, if
// any.
func DeleteChannelSSOCredential(channelId int) error {
	return DB.Where("channel_id = ?", channelId).Delete(&ChannelSSOCredential{}).Error
}

// GetChannelSSOUpstreamSession returns the stored upstream session for
// channelId, or gorm.ErrRecordNotFound if none exists.
func GetChannelSSOUpstreamSession(channelId int) (*ChannelSSOUpstreamSession, error) {
	var session ChannelSSOUpstreamSession
	if err := DB.Where("channel_id = ?", channelId).First(&session).Error; err != nil {
		return nil, err
	}
	return &session, nil
}

// SaveChannelSSOUpstreamSession upserts s by its ChannelId, setting
// UpdatedAt to now. Safe to call concurrently across the three supported
// databases.
func SaveChannelSSOUpstreamSession(s *ChannelSSOUpstreamSession) error {
	s.UpdatedAt = time.Now().UnixMilli()
	return DB.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "channel_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"cookie_jar_cipher", "user_snapshot", "upstream_kind", "updated_at",
		}),
	}).Create(s).Error
}

// DeleteChannelSSOUpstreamSession removes the stored upstream session for
// channelId, if any.
func DeleteChannelSSOUpstreamSession(channelId int) error {
	return DB.Where("channel_id = ?", channelId).Delete(&ChannelSSOUpstreamSession{}).Error
}

// GetChannelSSOStates returns the stored Channel SSO credentials and
// upstream sessions of the given channels, querying each table in chunks
// that stay within every supported database's bound-parameter limit.
func GetChannelSSOStates(channelIds []int) ([]ChannelSSOCredential, []ChannelSSOUpstreamSession, error) {
	var credentials []ChannelSSOCredential
	var sessions []ChannelSSOUpstreamSession
	for _, chunk := range lo.Chunk(channelIds, 500) {
		var chunkCredentials []ChannelSSOCredential
		if err := DB.Where("channel_id IN ?", chunk).Find(&chunkCredentials).Error; err != nil {
			return nil, nil, err
		}
		credentials = append(credentials, chunkCredentials...)
		var chunkSessions []ChannelSSOUpstreamSession
		if err := DB.Where("channel_id IN ?", chunk).Find(&chunkSessions).Error; err != nil {
			return nil, nil, err
		}
		sessions = append(sessions, chunkSessions...)
	}
	return credentials, sessions, nil
}
