package app

import (
	"context"
	"log/slog"
	"time"

	"github.com/ravenmk2/octodcp/internal/modules/identity"
	"github.com/ravenmk2/octodcp/internal/modules/install"
	"github.com/ravenmk2/octodcp/internal/modules/media"
)

func maintainAvatars(ctx context.Context, installer *install.Service, accounts *identity.Service, files media.Store, logger *slog.Logger) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		work, cancel := context.WithTimeout(ctx, 30*time.Second)
		if installer.Status(work).State == install.Ready {
			if err := cleanAvatars(work, accounts, files, time.Now()); err != nil && ctx.Err() == nil {
				logger.Error("avatar cleanup failed", "error", err)
			}
		}
		cancel()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func cleanAvatars(ctx context.Context, accounts *identity.Service, files media.Store, now time.Time) error {
	rows, err := accounts.PruneAvatars(ctx)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err = files.Remove(row.Key); err != nil {
			return err
		}
	}
	candidates, err := files.Candidates(ctx, now.Add(-24*time.Hour))
	if err != nil {
		return err
	}
	for _, key := range candidates {
		exists, e := accounts.AvatarKeyExists(ctx, key)
		if e != nil {
			return e
		}
		if !exists {
			if e = files.Remove(key); e != nil {
				return e
			}
		}
	}
	return nil
}
