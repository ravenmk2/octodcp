package identity

import (
	"bytes"
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/ravenmk2/octodcp/internal/modules/settings"
	"github.com/ravenmk2/octodcp/internal/platform/httpx"
	"github.com/ravenmk2/octodcp/internal/platform/password"
)

func (s *Service) Users(ctx context.Context, raw string, f Filter) ([]User, int, *Auth, error) {
	if f.Page < 1 || f.Page > 1000000 || f.PageSize < 1 || f.PageSize > 100 || utf8.RuneCountInString(f.Query) > 64 {
		return nil, 0, nil, httpx.Invalid("pagination", "invalid_value")
	}
	if f.Status != "" && f.Status != "active" && f.Status != "disabled" {
		return nil, 0, nil, httpx.Invalid("status", "invalid_value")
	}
	if f.Identity != "" && f.Identity != "founder" && f.Identity != "admin" && f.Identity != "user" {
		return nil, 0, nil, httpx.Invalid("identity", "invalid_value")
	}
	var users []User
	var total int
	a, err := s.execute(ctx, raw, policy{admin: true, renew: true}, 0, func(tx Tx, _ *Auth, _ *User) error { var e error; users, total, e = tx.ListUsers(f); return e })
	return users, total, a, err
}
func (s *Service) User(ctx context.Context, raw string, id uint64) (*User, *Auth, error) {
	var user *User
	a, err := s.execute(ctx, raw, policy{admin: true, renew: true}, id, func(_ Tx, _ *Auth, u *User) error { user = u; return nil })
	return user, a, err
}
func (s *Service) CreateUser(ctx context.Context, raw string, r CreateRequest, meta Metadata) (*User, *Auth, error) {
	if _, err := s.execute(ctx, raw, policy{admin: true}, 0, nil); err != nil {
		return nil, nil, err
	}
	if err := validateCreate(&r); err != nil {
		return nil, nil, err
	}
	hash, err := s.hash(ctx, r.Password)
	if err != nil {
		return nil, nil, err
	}
	var user *User
	a, err := s.execute(ctx, raw, policy{admin: true, renew: true}, 0, func(tx Tx, a *Auth, _ *User) error {
		var e error
		user, e = s.create(tx, r, hash, true)
		if e != nil {
			return e
		}
		return s.audit(tx, a.User, "user.created", "user", strconv.FormatUint(user.ID, 10), user.DisplayName, meta, map[string]any{"role": r.Role})
	})
	return user, a, err
}
func (s *Service) UpdateUser(ctx context.Context, raw string, id uint64, etag string, patch ProfilePatch, meta Metadata) (*User, *Auth, error) {
	var result *User
	a, err := s.execute(ctx, raw, policy{admin: true, renew: true}, id, func(tx Tx, a *Auth, u *User) error {
		if err := protect(a, u, "profile"); err != nil {
			return err
		}
		if err := checkVersion(etag, u); err != nil {
			return err
		}
		if patch.Username == nil && patch.DisplayName == nil {
			return httpx.Invalid("body", "empty_patch")
		}
		changes := map[string]any{}
		if patch.Username != nil {
			if !usernamePattern.MatchString(*patch.Username) {
				return httpx.Invalid("username", "invalid_format")
			}
			if u.Username != *patch.Username {
				changes["username"] = map[string]string{"before": u.Username, "after": *patch.Username}
				u.Username = *patch.Username
			}
		}
		if patch.DisplayName != nil {
			name := strings.TrimSpace(*patch.DisplayName)
			if err := validateName(name, "display_name", 30); err != nil {
				return err
			}
			if u.DisplayName != name {
				changes["display_name"] = map[string]string{"before": u.DisplayName, "after": name}
				u.DisplayName = name
			}
		}
		result = u
		if len(changes) == 0 {
			return nil
		}
		if err := s.bump(tx, u, false); err != nil {
			return err
		}
		return s.audit(tx, a.User, "user.profile_updated", "user", strconv.FormatUint(u.ID, 10), u.DisplayName, meta, map[string]any{"changes": changes})
	})
	return result, a, err
}
func (s *Service) UpdateAccount(ctx context.Context, raw, etag, name string, meta Metadata) (*User, *Auth, error) {
	var result *User
	a, err := s.execute(ctx, raw, policy{renew: true}, 0, func(tx Tx, a *Auth, _ *User) error {
		u := a.User
		if err := checkVersion(etag, u); err != nil {
			return err
		}
		name = strings.TrimSpace(name)
		if err := validateName(name, "display_name", 30); err != nil {
			return err
		}
		result = u
		if u.DisplayName == name {
			return nil
		}
		before := u.DisplayName
		u.DisplayName = name
		if err := s.bump(tx, u, false); err != nil {
			return err
		}
		return s.audit(tx, u, "user.profile_updated", "user", strconv.FormatUint(u.ID, 10), u.DisplayName, meta, map[string]any{"changes": map[string]any{"display_name": map[string]string{"before": before, "after": name}}})
	})
	return result, a, err
}
func (s *Service) SetRole(ctx context.Context, raw string, id uint64, etag, role string, meta Metadata) (*User, *Auth, error) {
	return s.securityChange(ctx, raw, id, etag, "role", role, meta)
}
func (s *Service) SetStatus(ctx context.Context, raw string, id uint64, etag, status string, meta Metadata) (*User, *Auth, error) {
	return s.securityChange(ctx, raw, id, etag, "status", status, meta)
}
func (s *Service) securityChange(ctx context.Context, raw string, id uint64, etag, kind, value string, meta Metadata) (*User, *Auth, error) {
	var result *User
	a, err := s.execute(ctx, raw, policy{admin: true, renew: true}, id, func(tx Tx, a *Auth, u *User) error {
		if err := protect(a, u, kind); err != nil {
			return err
		}
		if err := checkVersion(etag, u); err != nil {
			return err
		}
		before := u.Role
		action := "user.role_changed"
		if kind == "role" {
			if value != "admin" && value != "user" {
				return httpx.Invalid("role", "invalid_value")
			}
		} else {
			before = u.Status
			if value != "active" && value != "disabled" {
				return httpx.Invalid("status", "invalid_value")
			}
			action = "user.disabled"
			if value == "active" {
				action = "user.enabled"
			}
		}
		result = u
		if before == value {
			return nil
		}
		if kind == "role" {
			u.Role = value
		} else {
			u.Status = value
		}
		if err := s.bump(tx, u, true); err != nil {
			return err
		}
		return s.audit(tx, a.User, action, "user", strconv.FormatUint(u.ID, 10), u.DisplayName, meta, map[string]any{"changes": map[string]any{kind: map[string]string{"before": before, "after": value}}})
	})
	return result, a, err
}
func (s *Service) ResetPassword(ctx context.Context, raw string, id uint64, etag, secret string, meta Metadata) (*Auth, error) {
	// Check actor/target protection before doing expensive password work.
	if _, err := s.execute(ctx, raw, policy{admin: true}, id, func(_ Tx, a *Auth, u *User) error {
		if e := protect(a, u, "password"); e != nil {
			return e
		}
		return checkVersion(etag, u)
	}); err != nil {
		return nil, err
	}
	if err := password.Validate(secret); err != nil {
		return nil, httpx.Invalid("new_password", err.Error())
	}
	hash, err := s.hash(ctx, secret)
	if err != nil {
		return nil, err
	}
	return s.execute(ctx, raw, policy{admin: true, renew: true}, id, func(tx Tx, a *Auth, u *User) error {
		if e := protect(a, u, "password"); e != nil {
			return e
		}
		if e := checkVersion(etag, u); e != nil {
			return e
		}
		if e := tx.WritePassword(u.ID, Password{hash, true, s.now()}, false); e != nil {
			return e
		}
		if e := s.bump(tx, u, true); e != nil {
			return e
		}
		return s.audit(tx, a.User, "user.password_reset", "user", strconv.FormatUint(u.ID, 10), u.DisplayName, meta, map[string]bool{"require_password_change": true})
	})
}
func (s *Service) ChangePassword(ctx context.Context, raw, current, next string, meta Metadata) error {
	if _, err := s.execute(ctx, raw, policy{allowChange: true}, 0, nil); err != nil {
		return err
	}
	if err := password.Validate(next); err != nil {
		return httpx.Invalid("new_password", err.Error())
	}
	hash, err := s.hash(ctx, next)
	if err != nil {
		return err
	}
	_, err = s.execute(ctx, raw, policy{allowChange: true}, 0, func(tx Tx, a *Auth, _ *User) error {
		p, e := tx.Password(a.User.ID)
		if e != nil {
			return e
		}
		if p == nil {
			return unauthorized()
		}
		verified := false
		unchanged := false
		if e = s.hashing(ctx, func() error {
			verified = password.Verify(p.Hash, current)
			if verified {
				unchanged = password.Verify(p.Hash, next)
			}
			return nil
		}); e != nil {
			return e
		}
		if !verified {
			e := httpx.E(422, "CURRENT_PASSWORD_INVALID", "当前密码不正确")
			e.Details = append(e.Details, httpx.Detail{Field: "current_password", Code: "invalid_password"})
			return e
		}
		if unchanged {
			e := httpx.E(422, "PASSWORD_UNCHANGED", "新密码不能与当前密码相同")
			e.Details = append(e.Details, httpx.Detail{Field: "new_password", Code: "password_unchanged"})
			return e
		}
		if e = tx.WritePassword(a.User.ID, Password{hash, false, s.now()}, false); e != nil {
			return e
		}
		if e = s.bump(tx, a.User, true); e != nil {
			return e
		}
		return s.audit(tx, a.User, "user.password_changed", "user", strconv.FormatUint(a.User.ID, 10), a.User.DisplayName, meta, nil)
	})
	return err
}
func (s *Service) DeleteUser(ctx context.Context, raw string, id uint64, etag, confirmation string, meta Metadata) (*Auth, error) {
	return s.execute(ctx, raw, policy{admin: true, renew: true}, id, func(tx Tx, a *Auth, u *User) error {
		if e := protect(a, u, "delete"); e != nil {
			return e
		}
		if e := checkVersion(etag, u); e != nil {
			return e
		}
		if confirmation != u.Username {
			return httpx.E(422, "CONFIRMATION_MISMATCH", "请输入正确的用户名确认删除")
		}
		now := s.now()
		u.DeletedAt = &now
		u.Status = "disabled"
		avatar := u.AvatarID
		u.AvatarID = nil
		if e := s.bump(tx, u, true); e != nil {
			return e
		}
		if e := tx.DeletePassword(u.ID); e != nil {
			return e
		}
		if avatar != nil {
			if e := tx.OrphanAvatar(*avatar, now); e != nil {
				return e
			}
		}
		return s.audit(tx, a.User, "user.deleted", "user", strconv.FormatUint(u.ID, 10), u.DisplayName, meta, nil)
	})
}

func (s *Service) Settings(ctx context.Context, raw string) (settings.Snapshot, *Auth, error) {
	var snapshot settings.Snapshot
	a, err := s.execute(ctx, raw, policy{admin: true, renew: true}, 0, func(tx Tx, _ *Auth, _ *User) error { var e error; snapshot, e = settingSnapshot(tx, false); return e })
	return snapshot, a, err
}
func (s *Service) UpdateSettings(ctx context.Context, raw, etag string, p SettingsPatch, meta Metadata) (settings.Snapshot, *Auth, error) {
	var result settings.Snapshot
	a, err := s.execute(ctx, raw, policy{admin: true, renew: true}, 0, func(tx Tx, a *Auth, _ *User) error {
		entries, e := tx.Settings(true)
		if e != nil {
			return e
		}
		before, e := settings.Parse(entries)
		if e != nil {
			return e
		}
		if etag == "" {
			return httpx.E(428, "PRECONDITION_REQUIRED", "请先加载系统设置")
		}
		if !settingsTagPattern.MatchString(etag) {
			return httpx.Invalid("If-Match", "invalid_format")
		}
		if etag != `"settings-`+before.Version+`"` {
			return httpx.E(412, "VERSION_CONFLICT", "设置已被更新，请重新加载")
		}
		values := map[string]any{}
		if p.Name != nil {
			values["site.name"] = strings.TrimSpace(*p.Name)
		}
		if p.Timezone != nil {
			values["site.timezone"] = *p.Timezone
		}
		if p.Registration != nil {
			values["auth.registration_enabled"] = *p.Registration
		}
		if p.Days != nil {
			values["auth.session_ttl_days"] = *p.Days
		}
		if len(values) == 0 {
			return httpx.Invalid("body", "empty_patch")
		}
		changes := map[string]any{}
		for i := range entries {
			entry := &entries[i]
			v, ok := values[entry.Name]
			if !ok {
				continue
			}
			data, _ := json.Marshal(v)
			var old any
			_ = json.Unmarshal(entry.Value, &old)
			canonicalOld, _ := json.Marshal(old)
			if bytes.Equal(data, canonicalOld) {
				continue
			}
			changes[entry.Name] = map[string]any{"before": old, "after": v}
			entry.Value = data
			entry.Version++
		}
		after, e := settings.Parse(entries)
		if e != nil {
			return httpx.Invalid("settings", "invalid_value")
		}
		result = after
		if len(changes) == 0 {
			return nil
		}
		for _, entry := range entries {
			if _, changed := changes[entry.Name]; changed {
				if e = tx.SaveSetting(entry.Name, entry.Value, s.now()); e != nil {
					return e
				}
			}
		}
		return s.audit(tx, a.User, "settings.updated", "settings", "system", "系统设置", meta, map[string]any{"changes": changes})
	})
	return result, a, err
}
func (s *Service) Overview(ctx context.Context, raw string) (Counts, settings.Values, []Event, *Auth, error) {
	var counts Counts
	var values settings.Values
	var events []Event
	a, err := s.execute(ctx, raw, policy{admin: true, renew: true}, 0, func(tx Tx, _ *Auth, _ *User) error {
		var e error
		counts, e = tx.Counts()
		if e != nil {
			return e
		}
		snapshot, e := settingSnapshot(tx, false)
		if e != nil {
			return e
		}
		values = snapshot.Values
		events, e = tx.RecentEvents(5)
		return e
	})
	return counts, values, events, a, err
}
