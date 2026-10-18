package identity

import (
	"context"
	"encoding/json"
	"time"

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
type CreateRequest struct {
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Role        string `json:"role"`
	Password    string `json:"password"`
}
type ProfilePatch struct {
	Username    *string `json:"username,omitempty"`
	DisplayName *string `json:"display_name,omitempty"`
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
