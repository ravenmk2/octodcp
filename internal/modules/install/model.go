package install

import (
	"context"
	"errors"
	"time"

	"github.com/ravenmk2/octodcp/internal/platform/config"
)

var ErrIncompleteSchema = errors.New("database schema is incomplete")

type State string

const (
	SetupRequired    State = "setup_required"
	Initializing     State = "initializing"
	Ready            State = "ready"
	RecoveryRequired State = "recovery_required"
	Unavailable      State = "unavailable"
)

type Status struct {
	State     State  `json:"state"`
	PublicURL string `json:"public_url"`
}
type Site struct {
	Name     string `json:"site_name"`
	Timezone string `json:"timezone"`
}
type Founder struct {
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Password    string `json:"password"`
}
type Request struct {
	Database config.Database `json:"database"`
	Site     Site            `json:"site"`
	Founder  Founder         `json:"founder"`
}
type Seed struct {
	InstallationID                      string
	Fingerprint                         []byte
	Site                                Site
	Username, DisplayName, PasswordHash string
	Now                                 time.Time
}
type Instance struct {
	InstallationID string `db:"installation_id"`
	KeyFingerprint []byte `db:"key_fingerprint"`
	FounderID      uint64 `db:"founder_user_id"`
}

// Backend has installation semantics; it does not expose SQL or driver handles.
type Backend interface {
	Check(context.Context, bool) error
	Lock(context.Context) (func(), error)
	Migrate(context.Context) error
	SchemaReady(context.Context) error
	Seed(context.Context, Seed) error
	Instance(context.Context) (*Instance, error)
	Close() error
}
type Connector func(config.Database, bool) (Backend, error)
