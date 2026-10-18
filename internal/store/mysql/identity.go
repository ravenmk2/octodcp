package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	driver "github.com/go-sql-driver/mysql"
	"github.com/jmoiron/sqlx"
	"github.com/ravenmk2/octodcp/internal/modules/identity"
	"github.com/ravenmk2/octodcp/internal/modules/settings"
	"github.com/ravenmk2/octodcp/internal/platform/httpx"
)

func (s *Store) Credential(ctx context.Context, username string) (*identity.Credential, error) {
	var value identity.Credential
	err := s.db.GetContext(ctx, &value, "SELECT p.user_id,p.password_hash FROM user_password p JOIN `user` u ON u.id=p.user_id WHERE u.username=? AND u.deleted_at IS NULL AND u.status='active'", username)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &value, err
}
func (s *Store) Transact(ctx context.Context, fn func(identity.Tx) error) error {
	for attempt := 0; ; attempt++ {
		tx, err := s.db.BeginTxx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
		if err != nil {
			return err
		}
		err = fn(&identityTx{ctx, tx})
		if err == nil {
			err = tx.Commit()
		} else {
			_ = tx.Rollback()
		}
		var dbErr *driver.MySQLError
		if errors.As(err, &dbErr) && dbErr.Number == 1062 {
			return httpx.E(409, "USERNAME_TAKEN", "用户名已被使用")
		}
		if !errors.As(err, &dbErr) || (dbErr.Number != 1213 && dbErr.Number != 1205) || attempt >= 2 {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(attempt+1) * 15 * time.Millisecond):
		}
	}
}

type identityTx struct {
	ctx context.Context
	tx  *sqlx.Tx
}

const userColumns = "id,username,display_name,role,status,avatar_file_id,auth_version,row_version,last_login_at,created_at,updated_at,deleted_at"

func (t *identityTx) FounderID() (uint64, error) {
	var id uint64
	err := t.tx.GetContext(t.ctx, &id, "SELECT founder_user_id FROM system_instance WHERE id=1")
	return id, err
}
func (t *identityTx) LockUsers(ids []uint64) (map[uint64]*identity.User, error) {
	unique := map[uint64]bool{}
	for _, id := range ids {
		if id != 0 {
			unique[id] = true
		}
	}
	ordered := make([]uint64, 0, len(unique))
	for id := range unique {
		ordered = append(ordered, id)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i] < ordered[j] })
	result := map[uint64]*identity.User{}
	// One indexed point lock per user makes the acquisition order explicit.
	for _, id := range ordered {
		var u identity.User
		err := t.tx.GetContext(t.ctx, &u, "SELECT "+userColumns+" FROM `user` WHERE id=? FOR UPDATE", id)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		result[id] = &u
	}
	return result, nil
}
func (t *identityTx) Password(id uint64) (*identity.Password, error) {
	var p identity.Password
	err := t.tx.GetContext(t.ctx, &p, "SELECT password_hash,require_password_change,password_changed_at FROM user_password WHERE user_id=?", id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &p, err
}
func (t *identityTx) Session(hash []byte, lock bool) (*identity.Session, error) {
	var s identity.Session
	q := "SELECT id,user_id,token_hash,auth_version,ttl_days,created_at,last_seen_at,expires_at,revoked_at,COALESCE(ip_address,'') AS ip_address,COALESCE(user_agent,'') AS user_agent FROM `session` WHERE token_hash=?"
	if lock {
		q += " FOR UPDATE"
	}
	err := t.tx.GetContext(t.ctx, &s, q, hash)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &s, err
}
func (t *identityTx) Settings(lock bool) ([]settings.Entry, error) {
	var rows []struct {
		Name    string `db:"name"`
		Value   []byte `db:"value"`
		Version uint64 `db:"row_version"`
	}
	q := "SELECT name,value,row_version FROM system_setting WHERE name IN ('auth.registration_enabled','auth.session_ttl_days','site.name','site.timezone') ORDER BY name"
	if lock {
		q += " FOR UPDATE"
	}
	if err := t.tx.SelectContext(t.ctx, &rows, q); err != nil {
		return nil, err
	}
	entries := make([]settings.Entry, 0, len(rows))
	for _, r := range rows {
		entries = append(entries, settings.Entry{Name: r.Name, Value: r.Value, Version: r.Version})
	}
	return entries, nil
}
func (t *identityTx) InsertUser(u *identity.User) error {
	r, err := t.tx.ExecContext(t.ctx, "INSERT INTO `user` (username,display_name,role,status,created_at,updated_at) VALUES (?,?,?,'active',?,?)", u.Username, u.DisplayName, u.Role, u.CreatedAt, u.UpdatedAt)
	if err != nil {
		return err
	}
	id, err := r.LastInsertId()
	u.ID = uint64(id)
	u.AuthVersion = 1
	u.Version = 1
	u.Status = "active"
	return err
}
func (t *identityTx) SaveUser(u *identity.User) error {
	_, err := t.tx.ExecContext(t.ctx, "UPDATE `user` SET username=?,display_name=?,role=?,status=?,avatar_file_id=?,auth_version=?,row_version=?,updated_at=?,deleted_at=? WHERE id=?", u.Username, u.DisplayName, u.Role, u.Status, u.AvatarID, u.AuthVersion, u.Version, u.UpdatedAt, u.DeletedAt, u.ID)
	return err
}
func (t *identityTx) WritePassword(id uint64, p identity.Password, create bool) error {
	q := "UPDATE user_password SET password_hash=?,require_password_change=?,password_changed_at=? WHERE user_id=?"
	args := []any{p.Hash, p.RequireChange, p.ChangedAt, id}
	if create {
		q = "INSERT INTO user_password (password_hash,require_password_change,password_changed_at,user_id) VALUES (?,?,?,?)"
	}
	_, err := t.tx.ExecContext(t.ctx, q, args...)
	return err
}
func (t *identityTx) DeletePassword(id uint64) error {
	_, err := t.tx.ExecContext(t.ctx, "DELETE FROM user_password WHERE user_id=?", id)
	return err
}
func (t *identityTx) InsertSession(s *identity.Session) error {
	r, err := t.tx.ExecContext(t.ctx, "INSERT INTO `session` (user_id,token_hash,auth_version,ttl_days,created_at,last_seen_at,expires_at,ip_address,user_agent) VALUES (?,?,?,?,?,?,?,?,?)", s.UserID, s.Hash, s.AuthVersion, s.Days, s.CreatedAt, s.LastSeenAt, s.ExpiresAt, s.IP, s.UserAgent)
	if err != nil {
		return err
	}
	id, err := r.LastInsertId()
	s.ID = uint64(id)
	return err
}
func (t *identityTx) RenewSession(id uint64, seen, expires time.Time) error {
	_, err := t.tx.ExecContext(t.ctx, "UPDATE `session` SET last_seen_at=?,expires_at=? WHERE id=? AND revoked_at IS NULL", seen, expires, id)
	return err
}
func (t *identityTx) RevokeSessions(id uint64, now time.Time) error {
	_, err := t.tx.ExecContext(t.ctx, "UPDATE `session` SET revoked_at=? WHERE user_id=? AND revoked_at IS NULL", now, id)
	return err
}
func (t *identityTx) RevokeSession(id uint64, now time.Time) error {
	_, err := t.tx.ExecContext(t.ctx, "UPDATE `session` SET revoked_at=? WHERE id=? AND revoked_at IS NULL", now, id)
	return err
}
func (t *identityTx) UpdateLogin(id uint64, now time.Time) error {
	_, err := t.tx.ExecContext(t.ctx, "UPDATE `user` SET last_login_at=? WHERE id=?", now, id)
	return err
}
func (t *identityTx) SaveSetting(name string, value json.RawMessage, now time.Time) error {
	_, err := t.tx.ExecContext(t.ctx, "UPDATE system_setting SET value=CAST(? AS JSON),row_version=row_version+1,updated_at=? WHERE name=?", string(value), now, name)
	return err
}
func (t *identityTx) OrphanAvatar(id uint64, now time.Time) error {
	_, err := t.tx.ExecContext(t.ctx, "UPDATE avatar_file SET state='orphaned',expires_at=NULL,orphaned_at=? WHERE id=? AND state='attached'", now, id)
	return err
}
func (t *identityTx) Audit(e identity.Event) error {
	detail := e.Detail
	if len(detail) == 0 {
		detail = json.RawMessage(`{}`)
	}
	_, err := t.tx.ExecContext(t.ctx, "INSERT INTO audit_event (actor_user_id,actor_type,actor_name,actor_username,action,target_type,target_id,target_label,occurred_at,request_id,ip_address,detail) VALUES (?,?,?,?,?,?,?,?,?,?,?,CAST(? AS JSON))", e.ActorID, e.ActorType, e.ActorName, e.ActorUsername, e.Action, e.TargetType, e.TargetID, e.TargetLabel, e.OccurredAt, e.RequestID, e.IP, string(detail))
	return err
}
func (t *identityTx) ListUsers(f identity.Filter) ([]identity.User, int, error) {
	where := "deleted_at IS NULL"
	args := []any{}
	if f.Query != "" {
		q := strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(f.Query)
		where += " AND (username LIKE ? ESCAPE '!' OR display_name LIKE ? ESCAPE '!')"
		args = append(args, "%"+q+"%", "%"+q+"%")
	}
	if f.Status != "" {
		where += " AND status=?"
		args = append(args, f.Status)
	}
	if f.Identity != "" {
		founder, err := t.FounderID()
		if err != nil {
			return nil, 0, err
		}
		switch f.Identity {
		case "founder":
			where += " AND id=?"
			args = append(args, founder)
		case "admin":
			where += " AND role='admin' AND id<>?"
			args = append(args, founder)
		case "user":
			where += " AND role='user'"
		}
	}
	var total int
	if err := t.tx.GetContext(t.ctx, &total, "SELECT COUNT(*) FROM `user` WHERE "+where, args...); err != nil {
		return nil, 0, err
	}
	users := []identity.User{}
	queryArgs := append(append([]any{}, args...), f.PageSize, (f.Page-1)*f.PageSize)
	err := t.tx.SelectContext(t.ctx, &users, "SELECT "+userColumns+" FROM `user` WHERE "+where+" ORDER BY created_at DESC,id DESC LIMIT ? OFFSET ?", queryArgs...)
	return users, total, err
}
func (t *identityTx) Counts() (identity.Counts, error) {
	var c identity.Counts
	err := t.tx.GetContext(t.ctx, &c, "SELECT COUNT(*) AS total,COALESCE(SUM(status='active'),0) AS active,COALESCE(SUM(status='disabled'),0) AS disabled,COALESCE(SUM(role='admin'),0) AS admins FROM `user` WHERE deleted_at IS NULL")
	return c, err
}
func (t *identityTx) RecentEvents(limit int) ([]identity.Event, error) {
	events := []identity.Event{}
	err := t.tx.SelectContext(t.ctx, &events, "SELECT id,actor_user_id,actor_type,actor_name,actor_username,action,target_type,target_id,target_label,occurred_at,request_id,ip_address,detail FROM audit_event ORDER BY occurred_at DESC,id DESC LIMIT ?", limit)
	return events, err
}

var _ identity.Store = (*Store)(nil)
