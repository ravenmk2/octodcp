package identity

import (
	"bytes"
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/ravenmk2/octodcp/internal/platform/httpx"

	"github.com/ravenmk2/octodcp/internal/modules/audit"
	"github.com/ravenmk2/octodcp/internal/modules/settings"
)

type User struct {
	ID          uint64     `db:"id"`
	Username    string     `db:"username"`
	DisplayName string     `db:"display_name"`
	Role        string     `db:"role"`
	Status      string     `db:"status"`
	AvatarID    *uint64    `db:"avatar_file_id"`
	AuthVersion uint64     `db:"auth_version"`
	Version     uint64     `db:"row_version"`
	LastLoginAt *time.Time `db:"last_login_at"`
	CreatedAt   time.Time  `db:"created_at"`
	UpdatedAt   time.Time  `db:"updated_at"`
	DeletedAt   *time.Time `db:"deleted_at"`
}
type Password struct {
	Hash          string    `db:"password_hash"`
	RequireChange bool      `db:"require_password_change"`
	ChangedAt     time.Time `db:"password_changed_at"`
}
type Credential struct {
	UserID uint64 `db:"user_id"`
	Hash   string `db:"password_hash"`
}
type Session struct {
	ID          uint64     `db:"id"`
	UserID      uint64     `db:"user_id"`
	Hash        []byte     `db:"token_hash"`
	AuthVersion uint64     `db:"auth_version"`
	Days        int        `db:"ttl_days"`
	CreatedAt   time.Time  `db:"created_at"`
	LastSeenAt  time.Time  `db:"last_seen_at"`
	ExpiresAt   time.Time  `db:"expires_at"`
	RevokedAt   *time.Time `db:"revoked_at"`
	IP          string     `db:"ip_address"`
	UserAgent   string     `db:"user_agent"`
}
type Metadata struct{ RequestID, IP, UserAgent string }
type Event struct {
	ID            uint64          `db:"id"`
	ActorID       *uint64         `db:"actor_user_id"`
	ActorType     string          `db:"actor_type"`
	ActorName     string          `db:"actor_name"`
	ActorUsername *string         `db:"actor_username"`
	Action        string          `db:"action"`
	TargetType    string          `db:"target_type"`
	TargetID      *string         `db:"target_id"`
	TargetLabel   string          `db:"target_label"`
	OccurredAt    time.Time       `db:"occurred_at"`
	RequestID     *string         `db:"request_id"`
	IP            *string         `db:"ip_address"`
	Detail        json.RawMessage `db:"detail"`
}
type Filter struct {
	Page, PageSize          int
	Query, Status, Identity string
}
type Counts struct {
	Total    int `db:"total"`
	Active   int `db:"active"`
	Disabled int `db:"disabled"`
	Admins   int `db:"admins"`
}
type Auth struct {
	User          *User
	Session       *Session
	FounderID     uint64
	RequireChange bool
}
type LoginResult struct {
	Auth  *Auth
	Token string
}
type AvatarPatch struct {
	Present bool
	ID      *uint64
}

func (p AvatarPatch) IsZero() bool { return !p.Present }
func (p AvatarPatch) MarshalJSON() ([]byte, error) {
	if p.ID == nil {
		return []byte("null"), nil
	}
	return json.Marshal(strconv.FormatUint(*p.ID, 10))
}
func (p *AvatarPatch) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	p.Present = true
	p.ID = nil
	if string(data) == "null" {
		return nil
	}
	var raw string
	if err := json.Unmarshal(data, &raw); err != nil {
		return httpx.Invalid("avatar_upload_id", "invalid_format")
	}
	id, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || id == 0 || strconv.FormatUint(id, 10) != raw {
		return httpx.Invalid("avatar_upload_id", "invalid_format")
	}
	p.ID = &id
	return nil
}

type Avatar struct {
	ID         uint64     `db:"id"`
	Key        string     `db:"storage_key"`
	Hash       []byte     `db:"sha256"`
	Size       int        `db:"size_bytes"`
	UploaderID uint64     `db:"uploaded_by_user_id"`
	State      string     `db:"state"`
	ExpiresAt  *time.Time `db:"expires_at"`
	OrphanedAt *time.Time `db:"orphaned_at"`
	CreatedAt  time.Time  `db:"created_at"`
}
type CreateRequest struct {
	Avatar      AvatarPatch `json:"avatar_upload_id,omitzero"`
	Username    string      `json:"username"`
	DisplayName string      `json:"display_name"`
	Role        string      `json:"role"`
	Password    string      `json:"password"`
}
type ProfilePatch struct {
	Avatar      AvatarPatch `json:"avatar_upload_id,omitzero"`
	Username    *string     `json:"username,omitempty"`
	DisplayName *string     `json:"display_name,omitempty"`
}
type SettingsPatch struct {
	Name         *string `json:"site_name,omitempty"`
	Timezone     *string `json:"timezone,omitempty"`
	Registration *bool   `json:"registration_enabled,omitempty"`
	Days         *int    `json:"session_ttl_days,omitempty"`
}

// Transactions expose domain operations, never SQL/driver handles. Implementations
// must lock users in ascending ID order, before locking their sessions/settings.
type Tx interface {
	FounderID() (uint64, error)
	LockUsers([]uint64) (map[uint64]*User, error)
	Password(uint64) (*Password, error)
	Session([]byte, bool) (*Session, error)
	Settings(bool) ([]settings.Entry, error)
	InsertUser(*User) error
	SaveUser(*User) error
	WritePassword(uint64, Password, bool) error
	DeletePassword(uint64) error
	InsertSession(*Session) error
	RenewSession(uint64, time.Time, time.Time) error
	RevokeSessions(uint64, time.Time) error
	RevokeSession(uint64, time.Time) error
	UpdateLogin(uint64, time.Time) error
	SaveSetting(string, json.RawMessage, time.Time) error
	OrphanAvatar(uint64, time.Time) error
	Avatars([]uint64, bool) (map[uint64]*Avatar, error)
	InsertAvatar(*Avatar) error
	AttachAvatar(uint64) error
	DeleteAvatar(uint64) error
	AvatarOwner(uint64) (uint64, error)
	ExpiredAvatars(time.Time, int) ([]Avatar, error)
	AvatarKeyExists(string) (bool, error)
	AuditEvents(audit.Filter) ([]Event, error)
	AuditEvent(uint64) (*Event, error)
	Audit(Event) error
	ListUsers(Filter) ([]User, int, error)
	Counts() (Counts, error)
	RecentEvents(int) ([]Event, error)
}
type Store interface {
	Credential(context.Context, string) (*Credential, error)
	Transact(context.Context, func(Tx) error) error
}
type Provider func() (Store, error)
