package identity

import (
	"context"
	"strings"

	"github.com/ravenmk2/octodcp/internal/modules/audit"
	"github.com/ravenmk2/octodcp/internal/platform/httpx"
)

func (s *Service) Events(ctx context.Context, raw string, f audit.Filter) ([]Event, string, *Auth, error) {
	f.Query = strings.TrimSpace(f.Query)
	if err := f.Validate(); err != nil {
		return nil, "", nil, err
	}
	var events []Event
	a, err := s.execute(ctx, raw, policy{admin: true, renew: true}, 0, func(tx Tx, _ *Auth, _ *User) error { var e error; events, e = tx.AuditEvents(f); return e })
	if err != nil {
		return nil, "", nil, err
	}
	next := ""
	if len(events) > f.Limit {
		events = events[:f.Limit]
		last := events[len(events)-1]
		next = audit.EncodeCursor(last.OccurredAt, last.ID)
	}
	return events, next, a, nil
}
func (s *Service) Event(ctx context.Context, raw string, id uint64) (*Event, *Auth, error) {
	var event *Event
	a, err := s.execute(ctx, raw, policy{admin: true, renew: true}, 0, func(tx Tx, _ *Auth, _ *User) error {
		var e error
		event, e = tx.AuditEvent(id)
		if e == nil && event == nil {
			return httpx.E(404, "NOT_FOUND", "审计记录不存在")
		}
		return e
	})
	return event, a, err
}
