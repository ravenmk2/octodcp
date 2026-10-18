package identity

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ravenmk2/octodcp/internal/modules/settings"
	"github.com/ravenmk2/octodcp/internal/platform/cryptox"
	"github.com/ravenmk2/octodcp/internal/platform/httpx"
	"github.com/ravenmk2/octodcp/internal/platform/password"
)

var usernamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{2,29}$`)
var userTagPattern = regexp.MustCompile(`^"u-[1-9][0-9]*-v[1-9][0-9]*"$`)
var settingsTagPattern = regexp.MustCompile(`^"settings-[0-9a-f]{64}"$`)

type Service struct {
	provider Provider
	slots    chan struct{}
	dummy    string
	now      func() time.Time
}

func New(provider Provider) *Service {
	hash, err := password.Hash(cryptox.Token())
	if err != nil {
		panic(err)
	}
	return &Service{provider: provider, slots: make(chan struct{}, 4), dummy: hash, now: func() time.Time { return time.Now().UTC().Truncate(time.Millisecond) }}
}
func (s *Service) store() (Store, error) { return s.provider() }
func storageError(err error) error {
	if err == nil {
		return nil
	}
	var e *httpx.Error
	if errors.As(err, &e) {
		return err
	}
	return httpx.E(503, "DATABASE_UNAVAILABLE", "数据库暂时不可用，请稍后重试")
}
func unauthorized() error { return httpx.E(401, "UNAUTHENTICATED", "请先登录") }
func invalidCredentials() error {
	return httpx.E(401, "AUTH_INVALID_CREDENTIALS", "用户名或密码错误，或账号不可用")
}
func (s *Service) hashing(ctx context.Context, fn func() error) error {
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
		return fn()
	case <-ctx.Done():
		return ctx.Err()
	default:
		return httpx.E(429, "RATE_LIMITED", "操作过于频繁，请稍后重试")
	}
}
func (s *Service) hash(ctx context.Context, value string) (string, error) {
	if err := password.Validate(value); err != nil {
		return "", httpx.Invalid("password", err.Error())
	}
	var hash string
	err := s.hashing(ctx, func() error { var e error; hash, e = password.Hash(value); return e })
	return hash, err
}
func validateName(value string, field string, max int) error {
	if n := utf8.RuneCountInString(value); !utf8.ValidString(value) || n < 1 || n > max {
		return httpx.Invalid(field, "invalid_length")
	}
	return nil
}
func validateCreate(r *CreateRequest) error {
	r.DisplayName = strings.TrimSpace(r.DisplayName)
	if !usernamePattern.MatchString(r.Username) {
		return httpx.Invalid("username", "invalid_format")
	}
	if err := validateName(r.DisplayName, "display_name", 30); err != nil {
		return err
	}
	if r.Role != "user" && r.Role != "admin" {
		return httpx.Invalid("role", "invalid_value")
	}
	return nil
}
func settingSnapshot(tx Tx, lock bool) (settings.Snapshot, error) {
	entries, err := tx.Settings(lock)
	if err != nil {
		return settings.Snapshot{}, err
	}
	return settings.Parse(entries)
}
func (s *Service) PublicSettings(ctx context.Context) (settings.Values, error) {
	store, err := s.store()
	if err != nil {
		return settings.Values{}, err
	}
	var snapshot settings.Snapshot
	err = store.Transact(ctx, func(tx Tx) error { var e error; snapshot, e = settingSnapshot(tx, false); return e })
	return snapshot.Values, storageError(err)
}

func (s *Service) Login(ctx context.Context, username, secret, oldToken string, meta Metadata) (*LoginResult, error) {
	return s.login(ctx, username, secret, oldToken, meta, false)
}

// Called only by the successful installation request, never by setup/status.
func (s *Service) FounderLogin(ctx context.Context, username, secret, oldToken string, meta Metadata) (*LoginResult, error) {
	return s.login(ctx, username, secret, oldToken, meta, true)
}
func (s *Service) login(ctx context.Context, username, secret, oldToken string, meta Metadata, founderOnly bool) (*LoginResult, error) {
	store, err := s.store()
	if err != nil {
		return nil, err
	}
	credential, err := store.Credential(ctx, username)
	if err != nil {
		return nil, storageError(err)
	}
	hash := s.dummy
	if credential != nil {
		hash = credential.Hash
	}
	verified := false
	if err = s.hashing(ctx, func() error { verified = password.Verify(hash, secret); return nil }); err != nil {
		return nil, err
	}
	if !verified || credential == nil {
		return nil, invalidCredentials()
	}
	result := &LoginResult{Token: cryptox.Token()}
	err = store.Transact(ctx, func(tx Tx) error {
		ids := []uint64{credential.UserID}
		var previous *Session
		if len(oldToken) == 43 {
			var e error
			previous, e = tx.Session(cryptox.Digest(oldToken), false)
			if e != nil {
				return e
			}
			if previous != nil {
				ids = append(ids, previous.UserID)
			}
		}
		users, e := tx.LockUsers(ids)
		if e != nil {
			return e
		}
		u := users[credential.UserID]
		if u == nil || u.Status != "active" || u.DeletedAt != nil {
			return invalidCredentials()
		}
		p, e := tx.Password(u.ID)
		if e != nil {
			return e
		}
		if p == nil || p.Hash != credential.Hash {
			return invalidCredentials()
		}
		founder, e := tx.FounderID()
		if e != nil {
			return e
		}
		if founderOnly && founder != u.ID {
			return invalidCredentials()
		}
		if previous != nil {
			previous, e = tx.Session(cryptox.Digest(oldToken), true)
			if e != nil {
				return e
			}
			if previous != nil {
				if e = tx.RevokeSession(previous.ID, s.now()); e != nil {
					return e
				}
			}
		}
		a, e := s.openSession(tx, u, result.Token, founder, p.RequireChange, meta)
		if e != nil {
			return e
		}
		result.Auth = a
		return s.audit(tx, u, "auth.login_succeeded", "user", strconv.FormatUint(u.ID, 10), u.DisplayName, meta, nil)
	})
	if err != nil {
		return nil, storageError(err)
	}
	return result, nil
}
func (s *Service) openSession(tx Tx, u *User, raw string, founder uint64, requireChange bool, meta Metadata) (*Auth, error) {
	snapshot, err := settingSnapshot(tx, false)
	if err != nil {
		return nil, err
	}
	now := s.now()
	ua := []rune(meta.UserAgent)
	if len(ua) > 512 {
		ua = ua[:512]
	}
	ip := meta.IP
	if len(ip) > 45 {
		ip = ""
	}
	ss := &Session{UserID: u.ID, Hash: cryptox.Digest(raw), AuthVersion: u.AuthVersion, Days: snapshot.Values.SessionDays, CreatedAt: now, LastSeenAt: now, ExpiresAt: now.Add(time.Duration(snapshot.Values.SessionDays) * 24 * time.Hour), IP: ip, UserAgent: string(ua)}
	if err = tx.InsertSession(ss); err != nil {
		return nil, err
	}
	if err = tx.UpdateLogin(u.ID, now); err != nil {
		return nil, err
	}
	u.LastLoginAt = &now
	return &Auth{u, ss, founder, requireChange}, nil
}
func (s *Service) Register(ctx context.Context, r CreateRequest, meta Metadata) (*LoginResult, error) {
	r.Role = "user"
	if err := validateCreate(&r); err != nil {
		return nil, err
	}
	public, err := s.PublicSettings(ctx)
	if err != nil {
		return nil, err
	}
	if !public.Registration {
		return nil, httpx.E(409, "REGISTRATION_CLOSED", "系统暂未开放自主注册")
	}
	hash, err := s.hash(ctx, r.Password)
	if err != nil {
		return nil, err
	}
	store, err := s.store()
	if err != nil {
		return nil, err
	}
	result := &LoginResult{Token: cryptox.Token()}
	err = store.Transact(ctx, func(tx Tx) error {
		snapshot, e := settingSnapshot(tx, true)
		if e != nil {
			return e
		}
		if !snapshot.Values.Registration {
			return httpx.E(409, "REGISTRATION_CLOSED", "系统暂未开放自主注册")
		}
		u, e := s.create(tx, r, hash, false)
		if e != nil {
			return e
		}
		founder, e := tx.FounderID()
		if e != nil {
			return e
		}
		result.Auth, e = s.openSession(tx, u, result.Token, founder, false, meta)
		if e != nil {
			return e
		}
		return s.audit(tx, u, "user.registered", "user", strconv.FormatUint(u.ID, 10), u.DisplayName, meta, nil)
	})
	if err != nil {
		return nil, storageError(err)
	}
	return result, nil
}
func (s *Service) create(tx Tx, r CreateRequest, hash string, requireChange bool) (*User, error) {
	now := s.now()
	u := &User{Username: r.Username, DisplayName: r.DisplayName, Role: r.Role, CreatedAt: now, UpdatedAt: now}
	if err := tx.InsertUser(u); err != nil {
		return nil, err
	}
	if err := tx.WritePassword(u.ID, Password{hash, requireChange, now}, true); err != nil {
		return nil, err
	}
	return u, nil
}

type policy struct{ admin, allowChange, renew bool }

// Authentication, authorization, mutation and sliding renewal share a transaction.
func (s *Service) execute(ctx context.Context, raw string, p policy, targetID uint64, fn func(Tx, *Auth, *User) error) (*Auth, error) {
	if len(raw) != 43 {
		return nil, unauthorized()
	}
	store, err := s.store()
	if err != nil {
		return nil, err
	}
	var result *Auth
	err = store.Transact(ctx, func(tx Tx) error {
		ss, e := tx.Session(cryptox.Digest(raw), false)
		if e != nil {
			return e
		}
		if ss == nil {
			return unauthorized()
		}
		users, e := tx.LockUsers([]uint64{ss.UserID, targetID})
		if e != nil {
			return e
		}
		u := users[ss.UserID]
		ss, e = tx.Session(cryptox.Digest(raw), true)
		if e != nil {
			return e
		}
		now := s.now()
		if ss == nil || u == nil || u.Status != "active" || u.DeletedAt != nil || ss.AuthVersion != u.AuthVersion || ss.RevokedAt != nil || !now.Before(ss.ExpiresAt) {
			return unauthorized()
		}
		pwd, e := tx.Password(u.ID)
		if e != nil {
			return e
		}
		if pwd == nil {
			return unauthorized()
		}
		if pwd.RequireChange && !p.allowChange {
			return httpx.E(403, "PASSWORD_CHANGE_REQUIRED", "请先修改初始密码")
		}
		if p.admin && u.Role != "admin" {
			return httpx.E(403, "FORBIDDEN", "需要管理员权限")
		}
		founder, e := tx.FounderID()
		if e != nil {
			return e
		}
		a := &Auth{u, ss, founder, pwd.RequireChange}
		target := users[targetID]
		if targetID != 0 && (target == nil || target.DeletedAt != nil) {
			return httpx.E(404, "NOT_FOUND", "用户不存在")
		}
		if fn != nil {
			if e = fn(tx, a, target); e != nil {
				return e
			}
		}
		if p.renew {
			seen := now
			if seen.Before(ss.LastSeenAt) {
				seen = ss.LastSeenAt
			}
			expires := seen.Add(time.Duration(ss.Days) * 24 * time.Hour)
			if expires.Before(ss.ExpiresAt) {
				expires = ss.ExpiresAt
			}
			if e = tx.RenewSession(ss.ID, seen, expires); e != nil {
				return e
			}
			ss.LastSeenAt = seen
			ss.ExpiresAt = expires
		}
		result = a
		return nil
	})
	if err != nil {
		return nil, storageError(err)
	}
	return result, nil
}
func (s *Service) Current(ctx context.Context, raw string) (*Auth, error) {
	return s.execute(ctx, raw, policy{allowChange: true, renew: true}, 0, nil)
}
func (s *Service) AdminAccess(ctx context.Context, raw string, fn func() error) (*Auth, error) {
	return s.execute(ctx, raw, policy{admin: true, renew: true}, 0, func(Tx, *Auth, *User) error {
		if fn != nil {
			return fn()
		}
		return nil
	})
}
func (s *Service) Logout(ctx context.Context, raw string, meta Metadata) error {
	_, err := s.execute(ctx, raw, policy{allowChange: true}, 0, func(tx Tx, a *Auth, _ *User) error {
		if err := tx.RevokeSession(a.Session.ID, s.now()); err != nil {
			return err
		}
		return s.audit(tx, a.User, "auth.logged_out", "user", strconv.FormatUint(a.User.ID, 10), a.User.DisplayName, meta, nil)
	})
	var e *httpx.Error
	if errors.As(err, &e) && e.Code == "UNAUTHENTICATED" {
		return nil
	}
	return err
}
func (s *Service) audit(tx Tx, actor *User, action, targetType, targetID, label string, meta Metadata, detail any) error {
	var data []byte
	var err error
	if detail != nil {
		data, err = json.Marshal(detail)
		if err != nil {
			return err
		}
	}
	id := actor.ID
	name := actor.Username
	requestID := meta.RequestID
	ip := meta.IP
	if len(ip) > 45 {
		ip = ""
	}
	return tx.Audit(Event{ActorID: &id, ActorType: "user", ActorName: actor.DisplayName, ActorUsername: &name, Action: action, TargetType: targetType, TargetID: &targetID, TargetLabel: label, OccurredAt: s.now(), RequestID: &requestID, IP: &ip, Detail: data})
}

func UserTag(u *User) string {
	return `"u-` + strconv.FormatUint(u.ID, 10) + `-v` + strconv.FormatUint(u.Version, 10) + `"`
}
func checkVersion(value string, u *User) error {
	if value == "" {
		return httpx.E(428, "PRECONDITION_REQUIRED", "请先加载用户资料")
	}
	if !userTagPattern.MatchString(value) {
		return httpx.Invalid("If-Match", "invalid_format")
	}
	if value != UserTag(u) {
		return httpx.E(412, "VERSION_CONFLICT", "资料已被更新，请重新加载")
	}
	return nil
}
func protect(a *Auth, target *User, operation string) error {
	self := a.User.ID == target.ID
	if self && operation == "password" {
		return httpx.E(403, "SELF_PASSWORD_CHANGE_REQUIRED", "请使用个人账号中的修改密码功能")
	}
	if target.ID == a.FounderID && (!self || operation == "role" || operation == "status" || operation == "delete" || operation == "password") {
		return httpx.E(403, "FOUNDER_PROTECTED", "创始人身份受保护，资料与密码仅能本人修改")
	}
	if self && (operation == "role" || operation == "status" || operation == "delete") {
		return httpx.E(403, "SELF_PROTECTED", "不能禁用、删除或更改自己的角色")
	}
	return nil
}
func (s *Service) bump(tx Tx, u *User, security bool) error {
	u.Version++
	u.UpdatedAt = s.now()
	if security {
		u.AuthVersion++
		if err := tx.RevokeSessions(u.ID, s.now()); err != nil {
			return err
		}
	}
	return tx.SaveUser(u)
}
