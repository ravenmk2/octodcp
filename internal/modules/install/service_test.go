package install

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	_ "time/tzdata"

	"github.com/ravenmk2/octodcp/internal/platform/config"
	"github.com/ravenmk2/octodcp/internal/platform/password"
)

type fakeDB struct {
	mu                             sync.Mutex
	instance                       *Instance
	seed                           *Seed
	schema, tables, emptyOnFailure bool
	failMigrate, failSeed, offline bool
	migrations, seeds, opens       int
	lastMigrationFlag              bool
	wait                           chan struct{}
	entered                        chan struct{}
	afterSeed                      func()
}
type fakeHandle struct {
	db        *fakeDB
	migration bool
}

func (f *fakeDB) connect(_ config.Database, migration bool) (Backend, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.opens++
	f.lastMigrationFlag = migration
	return &fakeHandle{f, migration}, nil
}
func (f *fakeHandle) Check(_ context.Context, empty bool) error {
	f.db.mu.Lock()
	defer f.db.mu.Unlock()
	if f.db.offline {
		return errors.New("offline")
	}
	if empty && (f.db.schema || f.db.tables) {
		return errors.New("not empty")
	}
	return nil
}
func (f *fakeHandle) Lock(context.Context) (func(), error) { return func() {}, nil }
func (f *fakeHandle) Migrate(context.Context) error {
	if f.db.entered != nil {
		close(f.db.entered)
		<-f.db.wait
	}
	f.db.mu.Lock()
	defer f.db.mu.Unlock()
	f.db.migrations++
	if f.db.failMigrate {
		f.db.tables = !f.db.emptyOnFailure
		return errors.New("partial DDL")
	}
	f.db.schema = true
	f.db.tables = true
	return nil
}
func (f *fakeHandle) SchemaReady(context.Context) error {
	f.db.mu.Lock()
	defer f.db.mu.Unlock()
	if !f.db.schema {
		return ErrIncompleteSchema
	}
	return nil
}
func (f *fakeHandle) Seed(_ context.Context, s Seed) error {
	f.db.mu.Lock()
	defer f.db.mu.Unlock()
	if f.db.failSeed {
		return errors.New("seed failed")
	}
	f.db.seeds++
	f.db.seed = &s
	f.db.instance = &Instance{s.InstallationID, append([]byte(nil), s.Fingerprint...), 42}
	if f.db.afterSeed != nil {
		f.db.afterSeed()
	}
	return nil
}
func (f *fakeHandle) Instance(context.Context) (*Instance, error) {
	f.db.mu.Lock()
	defer f.db.mu.Unlock()
	if f.db.instance == nil {
		return nil, nil
	}
	v := *f.db.instance
	return &v, nil
}
func (f *fakeHandle) Close() error { return nil }

func runtime(t *testing.T) config.Runtime {
	t.Helper()
	return config.Runtime{DataDir: t.TempDir(), Server: config.Server{Listen: "127.0.0.1:0", PublicURL: "http://localhost:8080", TrustedProxies: []string{}}}
}
func newService(t *testing.T, r config.Runtime, db *fakeDB) *Service {
	t.Helper()
	s, err := New(context.Background(), r, db.connect)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}
func validRequest() Request {
	return Request{Database: config.Database{Host: "mysql", Port: 3306, Name: "octodcp", Username: "octodcp", Password: "database-secret", TLSMode: "disabled"}, Site: Site{"开发协作", "Asia/Shanghai"}, Founder: Founder{"founder", "创始人", "founder-password"}}
}

func TestFreshInstallNeedsNoTokenAndOwnsDataDirectory(t *testing.T) {
	r := runtime(t)
	db := &fakeDB{}
	s := newService(t, r, db)
	if s.Status(context.Background()).State != SetupRequired || db.opens != 0 {
		t.Fatal("fresh boot must not connect to an unknown database")
	}
	if exists(filepath.Join(r.DataDir, "setup.token")) || exists(filepath.Join(r.DataDir, "install.lock")) {
		t.Fatal("fresh boot must not create a token or installation lock")
	}
	if _, err := New(context.Background(), r, db.connect); err == nil {
		t.Fatal("second process acquired same data directory")
	}
}

func TestInstallationSeedsOnceAndUsesRuntimeConnection(t *testing.T) {
	r := runtime(t)
	db := &fakeDB{}
	s := newService(t, r, db)
	id, err := s.Install(context.Background(), validRequest())
	if err != nil {
		t.Fatal(err)
	}
	if s.Status(context.Background()).State != Ready || db.seeds != 1 || db.lastMigrationFlag {
		t.Fatal("installation did not transition to a non-migration connection")
	}
	if !password.Verify(db.seed.PasswordHash, "founder-password") {
		t.Fatal("founder password was not hashed")
	}
	b, err := os.ReadFile(filepath.Join(r.DataDir, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "database-secret") || !strings.Contains(string(b), "enc:v1:") {
		t.Fatal("database password was saved in plaintext")
	}
	if db.instance.InstallationID != id || db.instance.FounderID != 42 {
		t.Fatal("incorrect installation identity")
	}
	if exists(filepath.Join(r.DataDir, "setup.token")) {
		t.Fatal("obsolete owner token exists")
	}
	lock, err := os.ReadFile(filepath.Join(r.DataDir, "install.lock"))
	if err != nil || strings.TrimSpace(string(lock)) != id {
		t.Fatal("completed installation was not locked", err)
	}
	if _, err = s.Install(context.Background(), validRequest()); err == nil || db.seeds != 1 {
		t.Fatal("installation repeated")
	}
}

func TestRestartDoesNotMigrateOrRecreateFounder(t *testing.T) {
	r := runtime(t)
	db := &fakeDB{}
	s := newService(t, r, db)
	if _, err := s.Install(context.Background(), validRequest()); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s2 := newService(t, r, db)
	if s2.Status(context.Background()).State != Ready || db.migrations != 1 || db.seeds != 1 {
		t.Fatal("restart modified installed database")
	}
}

func TestPartialFilesNeverBecomeSetupRequired(t *testing.T) {
	for _, name := range []string{"install.lock", "master.key", "config.yaml", "installation.json", "unknown-file"} {
		t.Run(name, func(t *testing.T) {
			r := runtime(t)
			if err := os.WriteFile(filepath.Join(r.DataDir, name), []byte("partial"), 0600); err != nil {
				t.Fatal(err)
			}
			s := newService(t, r, &fakeDB{})
			if s.Status(context.Background()).State != RecoveryRequired || exists(filepath.Join(r.DataDir, "setup.token")) {
				t.Fatal("partial installation enabled fresh setup")
			}
		})
	}
}

func TestEmptyConfigurationAllowsFirstInstallation(t *testing.T) {
	for _, content := range []string{"", " \n\t"} {
		t.Run(content, func(t *testing.T) {
			r := runtime(t)
			if err := os.WriteFile(filepath.Join(r.DataDir, "config.yaml"), []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
			db := &fakeDB{}
			s := newService(t, r, db)
			if s.Status(context.Background()).State != SetupRequired {
				t.Fatal("blank configuration blocked first installation")
			}
			if _, err := s.Install(context.Background(), validRequest()); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestEmptyInstallationLockStillBlocksWizard(t *testing.T) {
	r := runtime(t)
	if err := os.WriteFile(filepath.Join(r.DataDir, "install.lock"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	db := &fakeDB{}
	s := newService(t, r, db)
	if s.Status(context.Background()).State != RecoveryRequired || db.opens != 0 {
		t.Fatal("existing lock without configuration must skip setup")
	}
	if _, err := s.Install(context.Background(), validRequest()); err == nil || db.opens != 0 {
		t.Fatal("existing lock allowed initialization")
	}
}

func TestObsoletePreinstallTokenIsRemoved(t *testing.T) {
	r := runtime(t)
	if err := os.WriteFile(filepath.Join(r.DataDir, "setup.token"), []byte("obsolete"), 0600); err != nil {
		t.Fatal(err)
	}
	s := newService(t, r, &fakeDB{})
	if s.Status(context.Background()).State != SetupRequired || exists(filepath.Join(r.DataDir, "setup.token")) {
		t.Fatal("obsolete token blocked upgrade to tokenless setup")
	}
}

func TestNonemptyDatabaseCannotInitializeLocalFiles(t *testing.T) {
	r := runtime(t)
	db := &fakeDB{tables: true}
	s := newService(t, r, db)
	if err := s.CheckDatabase(context.Background(), validRequest().Database); err == nil {
		t.Fatal("nonempty database check passed")
	}
	if _, err := s.Install(context.Background(), validRequest()); err == nil {
		t.Fatal("nonempty database was initialized")
	}
	if db.migrations != 0 || db.seeds != 0 || s.Status(context.Background()).State != SetupRequired {
		t.Fatal("failed database check modified installation")
	}
	for _, name := range []string{"config.yaml", "master.key", "installation.json", "install.lock"} {
		if exists(filepath.Join(r.DataDir, name)) {
			t.Fatalf("failed database check created %s", name)
		}
	}
}

func TestNewLocalInstallationArtifactsBlockSetupImmediately(t *testing.T) {
	for _, name := range []string{"config.yaml", "install.lock"} {
		t.Run(name, func(t *testing.T) {
			r := runtime(t)
			db := &fakeDB{}
			s := newService(t, r, db)
			if err := os.WriteFile(filepath.Join(r.DataDir, name), []byte("existing"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := s.CheckDatabase(context.Background(), validRequest().Database); err == nil {
				t.Fatal("local state change did not block database check")
			}
			if _, err := s.Install(context.Background(), validRequest()); err == nil || db.opens != 0 {
				t.Fatal("local state change allowed installation")
			}
			if s.Status(context.Background()).State != RecoveryRequired {
				t.Fatal("existing installation artifacts reopened the wizard")
			}
		})
	}
}

func TestCompletedInstallationBackfillsOnlyMissingLock(t *testing.T) {
	r := runtime(t)
	db := &fakeDB{}
	s := newService(t, r, db)
	id, err := s.Install(context.Background(), validRequest())
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	lockPath := filepath.Join(r.DataDir, "install.lock")
	if err := os.Remove(lockPath); err != nil {
		t.Fatal(err)
	}
	s2 := newService(t, r, db)
	lock, err := os.ReadFile(lockPath)
	if err != nil || strings.TrimSpace(string(lock)) != id || s2.Status(context.Background()).State != Ready {
		t.Fatal("healthy installation did not regain its lock", err)
	}
	_ = s2.Close()
	if err := os.WriteFile(lockPath, []byte("foreign-installation\n"), 0600); err != nil {
		t.Fatal(err)
	}
	s3 := newService(t, r, db)
	if s3.Status(context.Background()).State != RecoveryRequired || db.seeds != 1 || db.migrations != 1 {
		t.Fatal("mismatched lock was ignored or caused reinstallation")
	}
	after, _ := os.ReadFile(lockPath)
	if string(after) != "foreign-installation\n" {
		t.Fatal("existing lock was overwritten")
	}
}

func TestLockWriteFailureRetainsCommittedInstallationForRecovery(t *testing.T) {
	r := runtime(t)
	db := &fakeDB{}
	s := newService(t, r, db)
	db.afterSeed = func() {
		if err := os.Mkdir(filepath.Join(r.DataDir, "install.lock"), 0700); err != nil {
			t.Error(err)
		}
	}
	if _, err := s.Install(context.Background(), validRequest()); err == nil {
		t.Fatal("expected installation lock failure")
	}
	if s.Status(context.Background()).State != RecoveryRequired || db.seeds != 1 || !exists(filepath.Join(r.DataDir, "config.yaml")) || !exists(filepath.Join(r.DataDir, "installation.json")) {
		t.Fatal("lock was written before installation data or failure reopened wizard")
	}
	if err := os.Remove(filepath.Join(r.DataDir, "install.lock")); err != nil {
		t.Fatal(err)
	}
	if err := s.Recover(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	if s.Status(context.Background()).State != Ready || !exists(filepath.Join(r.DataDir, "install.lock")) || db.seeds != 1 {
		t.Fatal("recovery failed to seal installation without reseeding")
	}
}

func TestFailedDDLRetainsOriginalKeyAndRefusesBlindRecovery(t *testing.T) {
	r := runtime(t)
	db := &fakeDB{failMigrate: true}
	s := newService(t, r, db)
	if _, err := s.Install(context.Background(), validRequest()); err == nil {
		t.Fatal("expected DDL failure")
	}
	key, err := os.ReadFile(filepath.Join(r.DataDir, "master.key"))
	if err != nil {
		t.Fatal(err)
	}
	if s.Status(context.Background()).State != RecoveryRequired {
		t.Fatal("failure returned to fresh setup")
	}
	if err = s.Recover(context.Background(), "new-password"); err == nil {
		t.Fatal("partial DDL was replayed")
	}
	after, _ := os.ReadFile(filepath.Join(r.DataDir, "master.key"))
	if string(key) != string(after) || db.migrations != 1 {
		t.Fatal("key replaced or DDL retried")
	}
}

func TestRecoverMigratedButUnseeded(t *testing.T) {
	r := runtime(t)
	db := &fakeDB{failSeed: true}
	s := newService(t, r, db)
	if _, err := s.Install(context.Background(), validRequest()); err == nil {
		t.Fatal("expected seed failure")
	}
	key, _ := os.ReadFile(filepath.Join(r.DataDir, "master.key"))
	db.failSeed = false
	if err := s.Recover(context.Background(), ""); !errors.Is(err, ErrFounderPasswordRequired) {
		t.Fatal("recovery should require a new password", err)
	}
	if err := s.Recover(context.Background(), "replacement-password"); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(filepath.Join(r.DataDir, "master.key"))
	if string(key) != string(after) || db.migrations != 1 || db.seeds != 1 || !password.Verify(db.seed.PasswordHash, "replacement-password") {
		t.Fatal("recovery changed identity or failed to resume")
	}
}

func TestRecoveryMayMigrateOnlyWhenDatabaseIsStillEmpty(t *testing.T) {
	r := runtime(t)
	db := &fakeDB{failMigrate: true, emptyOnFailure: true}
	s := newService(t, r, db)
	if _, err := s.Install(context.Background(), validRequest()); err == nil {
		t.Fatal("expected failure before DDL")
	}
	key, _ := os.ReadFile(filepath.Join(r.DataDir, "master.key"))
	db.failMigrate = false
	if err := s.Recover(context.Background(), "replacement-password"); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(filepath.Join(r.DataDir, "master.key"))
	if string(key) != string(after) || db.migrations != 2 || db.seeds != 1 || s.Status(context.Background()).State != Ready {
		t.Fatal("empty-schema recovery failed or changed the master key")
	}
}

func TestInstalledFilesWithMissingSchemaRequireRecovery(t *testing.T) {
	r := runtime(t)
	db := &fakeDB{}
	s := newService(t, r, db)
	if _, err := s.Install(context.Background(), validRequest()); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	db.schema = false
	s2 := newService(t, r, db)
	if s2.Status(context.Background()).State != RecoveryRequired || db.migrations != 1 {
		t.Fatal("missing schema was treated as a fresh installation or replayed")
	}
}

func TestRecoverCommittedSeedWithoutDuplicatingFounder(t *testing.T) {
	r := runtime(t)
	db := &fakeDB{}
	s := newService(t, r, db)
	db.afterSeed = func() {
		if err := os.Mkdir(filepath.Join(r.DataDir, "config.yaml"), 0700); err != nil {
			t.Error(err)
		}
	}
	if _, err := s.Install(context.Background(), validRequest()); err == nil {
		t.Fatal("expected final configuration write failure")
	}
	if db.seeds != 1 {
		t.Fatal("seed was not committed before injected failure")
	}
	if err := os.Remove(filepath.Join(r.DataDir, "config.yaml")); err != nil {
		t.Fatal(err)
	}
	if err := s.Recover(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	if db.seeds != 1 || s.Status(context.Background()).State != Ready {
		t.Fatal("recovery duplicated the founder")
	}
}

func TestDatabaseOutageAndIdentityMismatchDoNotReinstall(t *testing.T) {
	r := runtime(t)
	db := &fakeDB{}
	s := newService(t, r, db)
	if _, err := s.Install(context.Background(), validRequest()); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	db.offline = true
	s2 := newService(t, r, db)
	if s2.Status(context.Background()).State != Unavailable {
		t.Fatal("outage did not remain installed")
	}
	_ = s2.Close()
	db.offline = false
	db.instance.InstallationID = "different-installation"
	s3 := newService(t, r, db)
	if s3.Status(context.Background()).State != RecoveryRequired {
		t.Fatal("foreign database accepted")
	}
}

func TestConcurrentInstallRefused(t *testing.T) {
	r := runtime(t)
	db := &fakeDB{entered: make(chan struct{}), wait: make(chan struct{})}
	s := newService(t, r, db)
	finished := make(chan error, 1)
	go func() { _, err := s.Install(context.Background(), validRequest()); finished <- err }()
	select {
	case <-db.entered:
	case <-time.After(10 * time.Second):
		t.Fatal("installation never reached migration")
	}
	if _, err := s.Install(context.Background(), validRequest()); err == nil {
		t.Fatal("concurrent installation accepted")
	}
	close(db.wait)
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
}
