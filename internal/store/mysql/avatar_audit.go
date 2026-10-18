package mysql

import (
	"database/sql"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/ravenmk2/octodcp/internal/modules/audit"
	"github.com/ravenmk2/octodcp/internal/modules/identity"
)

const avatarColumns = "id,storage_key,sha256,size_bytes,uploaded_by_user_id,state,expires_at,orphaned_at,created_at"
const eventColumns = "id,actor_user_id,actor_type,actor_name,actor_username,action,target_type,target_id,target_label,occurred_at,request_id,ip_address,detail"

func (t *identityTx) Avatars(ids []uint64, lock bool) (map[uint64]*identity.Avatar, error) {
	unique := map[uint64]bool{}
	for _, id := range ids {
		if id > 0 {
			unique[id] = true
		}
	}
	ordered := []uint64{}
	for id := range unique {
		ordered = append(ordered, id)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i] < ordered[j] })
	rows := map[uint64]*identity.Avatar{}
	for _, id := range ordered {
		v := &identity.Avatar{}
		q := "SELECT " + avatarColumns + " FROM avatar_file WHERE id=?"
		if lock {
			q += " FOR UPDATE"
		}
		err := t.tx.GetContext(t.ctx, v, q, id)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		rows[id] = v
	}
	return rows, nil
}
func (t *identityTx) InsertAvatar(v *identity.Avatar) error {
	r, err := t.tx.ExecContext(t.ctx, "INSERT INTO avatar_file (storage_key,content_type,width,height,size_bytes,sha256,uploaded_by_user_id,state,expires_at,created_at) VALUES (?,'image/png',256,256,?,?,?,'staged',?,?)", v.Key, v.Size, v.Hash, v.UploaderID, v.ExpiresAt, v.CreatedAt)
	if err != nil {
		return err
	}
	id, err := r.LastInsertId()
	v.ID = uint64(id)
	return err
}
func (t *identityTx) AttachAvatar(id uint64) error {
	_, err := t.tx.ExecContext(t.ctx, "UPDATE avatar_file SET state='attached',expires_at=NULL,orphaned_at=NULL WHERE id=?", id)
	return err
}
func (t *identityTx) DeleteAvatar(id uint64) error {
	_, err := t.tx.ExecContext(t.ctx, "DELETE FROM avatar_file WHERE id=?", id)
	return err
}
func (t *identityTx) AvatarOwner(id uint64) (uint64, error) {
	var owner uint64
	err := t.tx.GetContext(t.ctx, &owner, "SELECT id FROM `user` WHERE avatar_file_id=? AND deleted_at IS NULL", id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return owner, err
}
func (t *identityTx) ExpiredAvatars(now time.Time, limit int) ([]identity.Avatar, error) {
	rows := []identity.Avatar{}
	err := t.tx.SelectContext(t.ctx, &rows, "SELECT "+avatarColumns+" FROM avatar_file WHERE ((state='staged' AND expires_at<=?) OR (state='orphaned' AND orphaned_at<=?)) AND NOT EXISTS (SELECT 1 FROM `user` WHERE avatar_file_id=avatar_file.id) ORDER BY id LIMIT ? FOR UPDATE SKIP LOCKED", now, now.Add(-24*time.Hour), limit)
	return rows, err
}
func (t *identityTx) AvatarKeyExists(key string) (bool, error) {
	var count int
	err := t.tx.GetContext(t.ctx, &count, "SELECT COUNT(*) FROM avatar_file WHERE storage_key=?", key)
	return count > 0, err
}
func (t *identityTx) AuditEvents(f audit.Filter) ([]identity.Event, error) {
	where := []string{"1=1"}
	args := []any{}
	if f.Query != "" {
		escaped := strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(f.Query)
		pattern := "%" + escaped + "%"
		search := "(actor_name LIKE ? ESCAPE '!' OR CONVERT(actor_username USING utf8mb4) LIKE ? ESCAPE '!' OR target_label LIKE ? ESCAPE '!' OR CONVERT(action USING utf8mb4) LIKE ? ESCAPE '!'"
		args = append(args, pattern, pattern, pattern, pattern)
		for _, code := range audit.SearchActions(f.Query) {
			search += " OR action=?"
			args = append(args, code)
		}
		where = append(where, search+")")
	}
	if f.ActionPrefix != "" {
		where = append(where, "action LIKE ? ESCAPE '!'")
		args = append(args, strings.ReplaceAll(f.ActionPrefix, "_", "!_")+"%")
	}
	if f.ActorUserID > 0 {
		where = append(where, "actor_user_id=?")
		args = append(args, f.ActorUserID)
	}
	if f.TargetType != "" {
		where = append(where, "target_type=?")
		args = append(args, f.TargetType)
	}
	if f.TargetID != "" {
		// The column is ASCII. Avoid a charset-conversion error for non-ASCII
		// input while preserving indexed equality for actual stored IDs.
		if strings.IndexFunc(f.TargetID, func(r rune) bool { return r > 127 }) >= 0 {
			where = append(where, "1=0")
		} else {
			where = append(where, "target_id=?")
			args = append(args, f.TargetID)
		}
	}
	if f.From != nil {
		where = append(where, "occurred_at>=?")
		args = append(args, f.From.UTC())
	}
	if f.To != nil {
		where = append(where, "occurred_at<?")
		args = append(args, f.To.UTC())
	}
	if f.Before != nil {
		where = append(where, "(occurred_at<? OR (occurred_at=? AND id<?))")
		args = append(args, f.Before.Time, f.Before.Time, f.Before.ID)
	}
	args = append(args, f.Limit+1)
	events := []identity.Event{}
	err := t.tx.SelectContext(t.ctx, &events, "SELECT "+eventColumns+" FROM audit_event WHERE "+strings.Join(where, " AND ")+" ORDER BY occurred_at DESC,id DESC LIMIT ?", args...)
	return events, err
}
func (t *identityTx) AuditEvent(id uint64) (*identity.Event, error) {
	v := &identity.Event{}
	err := t.tx.GetContext(t.ctx, v, "SELECT "+eventColumns+" FROM audit_event WHERE id=?", id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return v, err
}
