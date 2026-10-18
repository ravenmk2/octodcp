package identity

import (
	"context"
	"strconv"
	"time"

	"github.com/ravenmk2/octodcp/internal/modules/media"
	"github.com/ravenmk2/octodcp/internal/platform/httpx"
)

func avatarUnavailable() error {
	return httpx.E(409, "AVATAR_UPLOAD_UNAVAILABLE", "头像草稿不可用，请重新上传")
}
func (s *Service) UploadAccess(ctx context.Context, raw string) error {
	_, err := s.execute(ctx, raw, policy{}, 0, nil)
	return err
}
func (s *Service) StageAvatar(ctx context.Context, raw string, file media.File) (*Avatar, *Auth, error) {
	if !media.ValidKey(file.Key) || file.Size < 1 || file.Size > media.MaxOutput || len(file.Hash) != 32 {
		return nil, nil, httpx.Invalid("file", "invalid_image")
	}
	var avatar *Avatar
	a, err := s.execute(ctx, raw, policy{renew: true}, 0, func(tx Tx, a *Auth, _ *User) error {
		now := s.now()
		expires := now.Add(24 * time.Hour)
		avatar = &Avatar{Key: file.Key, Hash: file.Hash, Size: file.Size, UploaderID: a.User.ID, State: "staged", ExpiresAt: &expires, CreatedAt: now}
		return tx.InsertAvatar(avatar)
	})
	return avatar, a, err
}
func (s *Service) DeleteAvatarDraft(ctx context.Context, raw string, id uint64) (string, *Auth, error) {
	key := ""
	a, err := s.execute(ctx, raw, policy{renew: true}, 0, func(tx Tx, a *Auth, _ *User) error {
		rows, e := tx.Avatars([]uint64{id}, true)
		if e != nil {
			return e
		}
		v := rows[id]
		if v == nil || v.UploaderID != a.User.ID {
			return httpx.E(404, "NOT_FOUND", "头像草稿不存在")
		}
		if v.State != "staged" {
			return avatarUnavailable()
		}
		key = v.Key
		return tx.DeleteAvatar(id)
	})
	return key, a, err
}
func (s *Service) Avatar(ctx context.Context, raw string, id uint64) (*Avatar, *Auth, error) {
	var avatar *Avatar
	a, err := s.execute(ctx, raw, policy{allowChange: true, renew: true}, 0, func(tx Tx, a *Auth, _ *User) error {
		missing := httpx.E(404, "NOT_FOUND", "头像不存在")
		rows, e := tx.Avatars([]uint64{id}, false)
		if e != nil {
			return e
		}
		v := rows[id]
		if v == nil {
			return missing
		}
		switch v.State {
		case "staged":
			if a.RequireChange || v.UploaderID != a.User.ID || v.ExpiresAt == nil || !s.now().Before(*v.ExpiresAt) {
				return missing
			}
		case "attached":
			owner, e := tx.AvatarOwner(id)
			if e != nil {
				return e
			}
			if owner == 0 || (a.RequireChange && owner != a.User.ID) {
				return missing
			}
		default:
			return missing
		}
		avatar = v
		return nil
	})
	return avatar, a, err
}
func (s *Service) bindAvatar(tx Tx, actor, target *User, p AvatarPatch) (bool, error) {
	if !p.Present {
		return false, nil
	}
	if p.ID == nil && target.AvatarID == nil {
		return false, nil
	}
	ids := []uint64{}
	if p.ID != nil {
		ids = append(ids, *p.ID)
	}
	if target.AvatarID != nil {
		ids = append(ids, *target.AvatarID)
	}
	rows, err := tx.Avatars(ids, true)
	if err != nil {
		return false, err
	}
	if p.ID != nil {
		v := rows[*p.ID]
		if v == nil || v.State != "staged" || v.UploaderID != actor.ID || v.ExpiresAt == nil || !s.now().Before(*v.ExpiresAt) {
			return false, avatarUnavailable()
		}
		if err = tx.AttachAvatar(v.ID); err != nil {
			return false, err
		}
	}
	if target.AvatarID != nil {
		if err = tx.OrphanAvatar(*target.AvatarID, s.now()); err != nil {
			return false, err
		}
	}
	target.AvatarID = p.ID
	return true, nil
}
func (s *Service) avatarAudit(tx Tx, actor, target *User, meta Metadata) error {
	op := "updated"
	if target.AvatarID == nil {
		op = "removed"
	}
	return s.audit(tx, actor, "user.avatar_changed", "user", strconv.FormatUint(target.ID, 10), target.DisplayName, meta, map[string]any{"operation": op})
}

// Prune metadata transactionally; callers remove files only after commit.
func (s *Service) PruneAvatars(ctx context.Context) ([]Avatar, error) {
	store, err := s.store()
	if err != nil {
		return nil, err
	}
	var removed []Avatar
	err = store.Transact(ctx, func(tx Tx) error {
		rows, e := tx.ExpiredAvatars(s.now(), 100)
		if e != nil {
			return e
		}
		removed = rows
		for _, v := range rows {
			if e = tx.DeleteAvatar(v.ID); e != nil {
				return e
			}
		}
		return nil
	})
	return removed, storageError(err)
}
func (s *Service) AvatarKeyExists(ctx context.Context, key string) (bool, error) {
	store, err := s.store()
	if err != nil {
		return false, err
	}
	var exists bool
	err = store.Transact(ctx, func(tx Tx) error { var e error; exists, e = tx.AvatarKeyExists(key); return e })
	return exists, storageError(err)
}
