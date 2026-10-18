package install

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/gofrs/flock"
	"github.com/ravenmk2/octodcp/internal/platform/config"
	"github.com/ravenmk2/octodcp/internal/platform/cryptox"
	"github.com/ravenmk2/octodcp/internal/platform/filestore"
	"github.com/ravenmk2/octodcp/internal/platform/httpx"
	"github.com/ravenmk2/octodcp/internal/platform/password"
)

var usernamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{2,29}$`)
var ErrFounderPasswordRequired = errors.New("a new founder password is required to complete initialization")

type marker struct {
	Version        int       `json:"version"`
	InstallationID string    `json:"installation_id"`
	CompletedAt    time.Time `json:"completed_at"`
}
type checkpoint struct {
	Version        int    `json:"version"`
	InstallationID string `json:"installation_id"`
	Stage          string `json:"stage"`
	Site           Site   `json:"site"`
	Username       string `json:"username"`
	DisplayName    string `json:"display_name"`
}
type Service struct {
	mu           sync.RWMutex
	runtime      config.Runtime
	state        State
	backend      Backend
	connect      Connector
	key          []byte
	csrfKey      []byte
	instanceLock *flock.Flock
	lastProbe    time.Time
}

func New(ctx context.Context, r config.Runtime, connect Connector) (*Service, error) {
	if err := filestore.EnsurePrivateDir(r.DataDir); err != nil {
		return nil, err
	}
	if err := filestore.EnsurePrivateDir(filepath.Join(r.DataDir, "locks")); err != nil {
		return nil, err
	}
	l := flock.New(filepath.Join(r.DataDir, "locks", "instance.lock"))
	ok, err := l.TryLock()
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New("another process owns the data directory")
	}
	s := &Service{runtime: r, connect: connect, state: RecoveryRequired, csrfKey: cryptox.Random(32), instanceLock: l}
	if err := s.load(ctx); err != nil {
		_ = l.Close()
		return nil, err
	}
	return s, nil
}

func (s *Service) path(p string) string {
	return filepath.Join(s.runtime.DataDir, filepath.FromSlash(p))
}
func exists(path string) bool { _, err := os.Lstat(path); return err == nil }

func (s *Service) load(ctx context.Context) error {
	full := exists(s.path("config.yaml")) && exists(s.path("master.key")) && exists(s.path("installation.json"))
	if exists(s.path("install.lock")) && !full {
		s.state = RecoveryRequired
		return nil
	}
	if full {
		f, err := config.Read(s.path("config.yaml"))
		if err != nil {
			s.state = RecoveryRequired
			return nil
		}
		key, err := filestore.ReadPrivate(s.path("master.key"), 32)
		if err != nil || len(key) != 32 {
			s.state = RecoveryRequired
			return nil
		}
		var m marker
		if err := readJSON(s.path("installation.json"), &m); err != nil || m.Version != 1 || m.InstallationID != f.InstallationID {
			s.state = RecoveryRequired
			return nil
		}
		s.key = key
		s.csrfKey = cryptox.Derive(key, "csrf")
		plain, err := cryptox.Decrypt(key, f.InstallationID, "database.password", f.Database.Password)
		if err != nil {
			s.state = RecoveryRequired
			return nil
		}
		db := f.Database
		db.Password = plain
		db.CAFile = s.runtime.DatabaseCA
		b, err := s.connect(db, false)
		if err != nil {
			s.state = Unavailable
			return nil
		}
		if err = b.Check(ctx, false); err != nil {
			_ = b.Close()
			s.state = Unavailable
			return nil
		}
		if err = b.SchemaReady(ctx); err != nil {
			_ = b.Close()
			s.state = Unavailable
			if errors.Is(err, ErrIncompleteSchema) {
				s.state = RecoveryRequired
			}
			return nil
		}
		i, err := b.Instance(ctx)
		if err != nil || i == nil || i.InstallationID != f.InstallationID || subtle.ConstantTimeCompare(i.KeyFingerprint, cryptox.Fingerprint(key)) != 1 {
			_ = b.Close()
			s.state = RecoveryRequired
			return nil
		}
		if err = s.sealInstallation(f.InstallationID); err != nil {
			_ = b.Close()
			s.state = RecoveryRequired
			return nil
		}
		if s.backend != nil {
			_ = s.backend.Close()
		}
		s.backend = b
		s.state = Ready
		return nil
	}
	clean, err := s.freshData()
	if err != nil {
		return err
	}
	if !clean {
		s.state = RecoveryRequired
		return nil
	}
	// Discard the obsolete pre-install token from earlier development versions.
	_ = os.Remove(s.path("setup.token"))
	s.state = SetupRequired
	return nil
}

func (s *Service) freshData() (bool, error) {
	for _, name := range []string{"install.lock", "master.key", "installation.json"} {
		if exists(s.path(name)) {
			return false, nil
		}
	}
	clean := true
	err := filepath.WalkDir(s.runtime.DataDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == s.runtime.DataDir {
			return nil
		}
		rel, _ := filepath.Rel(s.runtime.DataDir, path)
		if rel == "locks" && d.IsDir() {
			return filepath.SkipDir
		}
		if rel == "setup.token" && d.Type().IsRegular() {
			return nil
		}
		if rel == "config.yaml" {
			_, configErr := config.Read(path)
			if errors.Is(configErr, config.ErrEmptyFile) {
				return nil
			}
			clean = false
			return fs.SkipAll
		}
		if !d.IsDir() {
			clean = false
			return fs.SkipAll
		}
		return nil
	})
	return clean, err
}

func (s *Service) sealInstallation(id string) error {
	data, err := filestore.ReadPrivate(s.path("install.lock"), 128)
	if errors.Is(err, os.ErrNotExist) {
		return filestore.CreatePrivate(s.path("install.lock"), []byte(id+"\n"))
	}
	if err != nil || strings.TrimSpace(string(data)) != id {
		return errors.New("installation lock does not match configuration")
	}
	return nil
}

// Called with mu held. Local state is checked again before each setup operation.
func (s *Service) checkSetup() error {
	if s.state != SetupRequired {
		code := "ALREADY_INSTALLED"
		if s.state == Initializing {
			code = "INSTALL_IN_PROGRESS"
		}
		return httpx.E(409, code, "当前状态不允许安装")
	}
	clean, err := s.freshData()
	if err != nil || !clean {
		s.state = RecoveryRequired
		return httpx.E(409, "INSTALLATION_STATE_CHANGED", "检测到已有安装配置，请刷新页面并检查系统状态")
	}
	return nil
}

func (s *Service) Status(ctx context.Context) Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state == SetupRequired {
		_ = s.checkSetup()
	}
	if (s.state == Unavailable || s.state == Ready) && time.Since(s.lastProbe) > 5*time.Second {
		s.lastProbe = time.Now()
		if s.state == Ready {
			if err := s.backend.Check(ctx, false); err != nil {
				s.state = Unavailable
			}
		} else {
			_ = s.load(ctx)
		}
	}
	return Status{s.state, s.runtime.Origin()}
}
func (s *Service) Backend() Backend { s.mu.RLock(); defer s.mu.RUnlock(); return s.backend }
func (s *Service) CSRFKey() []byte {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]byte(nil), s.csrfKey...)
}
func (s *Service) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.backend != nil {
		_ = s.backend.Close()
	}
	return s.instanceLock.Close()
}

func (s *Service) CheckDatabase(ctx context.Context, d config.Database) error {
	s.mu.Lock()
	stateErr := s.checkSetup()
	s.mu.Unlock()
	if stateErr != nil {
		return stateErr
	}
	if err := d.Validate(); err != nil {
		return httpx.Invalid("database", "invalid_connection")
	}
	d.CAFile = s.runtime.DatabaseCA
	b, err := s.connect(d, false)
	if err != nil {
		return httpx.E(422, "DATABASE_CHECK_FAILED", "数据库连接配置无效")
	}
	defer func() { _ = b.Close() }()
	if err := b.Check(ctx, true); err != nil {
		return err
	}
	return nil
}

func Validate(r *Request) error {
	if err := r.Database.Validate(); err != nil {
		return httpx.Invalid("database", "invalid_connection")
	}
	r.Site.Name = strings.TrimSpace(r.Site.Name)
	r.Founder.DisplayName = strings.TrimSpace(r.Founder.DisplayName)
	if n := utf8.RuneCountInString(r.Site.Name); n < 1 || n > 40 {
		return httpx.Invalid("site.site_name", "invalid_length")
	}
	if _, err := time.LoadLocation(r.Site.Timezone); err != nil || r.Site.Timezone == "" {
		return httpx.Invalid("site.timezone", "invalid_timezone")
	}
	if !usernamePattern.MatchString(r.Founder.Username) {
		return httpx.Invalid("founder.username", "invalid_format")
	}
	if n := utf8.RuneCountInString(r.Founder.DisplayName); n < 1 || n > 30 {
		return httpx.Invalid("founder.display_name", "invalid_length")
	}
	if err := password.Validate(r.Founder.Password); err != nil {
		return httpx.Invalid("founder.password", err.Error())
	}
	return nil
}

func (s *Service) Install(ctx context.Context, r Request) (string, error) {
	if err := Validate(&r); err != nil {
		return "", err
	}
	s.mu.Lock()
	if stateErr := s.checkSetup(); stateErr != nil {
		s.mu.Unlock()
		return "", stateErr
	}
	s.state = Initializing
	s.mu.Unlock()
	durable := false
	defer func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.state == Initializing {
			if durable {
				s.state = RecoveryRequired
			} else {
				s.state = SetupRequired
			}
		}
	}()
	r.Database.CAFile = s.runtime.DatabaseCA
	b, err := s.connect(r.Database, true)
	if err != nil {
		return "", httpx.E(422, "DATABASE_CHECK_FAILED", "数据库配置无效")
	}
	defer func() { _ = b.Close() }()
	unlock, err := b.Lock(ctx)
	if err != nil {
		return "", err
	}
	defer unlock()
	if err = b.Check(ctx, true); err != nil {
		return "", err
	}
	if clean, checkErr := s.freshData(); checkErr != nil || !clean {
		s.mu.Lock()
		s.state = RecoveryRequired
		s.mu.Unlock()
		return "", httpx.E(409, "INSTALLATION_STATE_CHANGED", "检测到已有安装配置，请检查系统状态")
	}
	hash, err := password.Hash(r.Founder.Password)
	if err != nil {
		return "", err
	}
	id := cryptox.ID()
	key := cryptox.Random(32)
	if err = filestore.EnsurePrivateDir(s.path(".install")); err != nil {
		return "", err
	}
	j := checkpoint{1, id, "prepared", r.Site, r.Founder.Username, r.Founder.DisplayName}
	durable = true
	if err = writeJSON(s.path(".install/staging.json"), j); err != nil {
		return "", err
	}
	if err = filestore.CreatePrivate(s.path("master.key"), key); err != nil {
		return "", err
	}
	encrypted, err := cryptox.Encrypt(key, id, "database.password", r.Database.Password)
	if err != nil {
		return "", err
	}
	f := config.File{SchemaVersion: 1, InstallationID: id, Server: s.runtime.Server, Database: r.Database}
	f.Database.Password = encrypted
	if err = config.Write(s.path(".install/config.pending.yaml"), f); err != nil {
		return "", err
	}
	if err = b.Migrate(ctx); err != nil {
		return "", httpx.E(500, "INSTALLATION_INTERRUPTED", "初始化中断，请由部署维护者检查恢复")
	}
	j.Stage = "migrated"
	if err = writeJSON(s.path(".install/staging.json"), j); err != nil {
		return "", err
	}
	if err = b.Seed(ctx, Seed{id, cryptox.Fingerprint(key), r.Site, r.Founder.Username, r.Founder.DisplayName, hash, time.Now().UTC()}); err != nil {
		return "", err
	}
	if err = s.finish(ctx, f, key); err != nil {
		return "", err
	}
	return id, nil
}

func (s *Service) finish(ctx context.Context, f config.File, key []byte) error {
	d := f.Database
	plain, err := cryptox.Decrypt(key, f.InstallationID, "database.password", d.Password)
	if err != nil {
		return err
	}
	d.Password = plain
	d.CAFile = s.runtime.DatabaseCA
	b, err := s.connect(d, false)
	if err != nil {
		return err
	}
	keep := false
	defer func() {
		if !keep {
			_ = b.Close()
		}
	}()
	if err = b.Check(ctx, false); err != nil {
		return err
	}
	if err = b.SchemaReady(ctx); err != nil {
		return err
	}
	instance, err := b.Instance(ctx)
	if err != nil || instance == nil || instance.InstallationID != f.InstallationID || subtle.ConstantTimeCompare(instance.KeyFingerprint, cryptox.Fingerprint(key)) != 1 {
		return errors.New("runtime database identity does not match installation")
	}

	if current, err := config.Read(s.path("config.yaml")); err == nil {
		if current.InstallationID != f.InstallationID {
			return errors.New("configuration belongs to a different installation")
		}
	} else if !errors.Is(err, os.ErrNotExist) && !errors.Is(err, config.ErrEmptyFile) {
		return err
	}
	if err := config.Write(s.path("config.yaml"), f); err != nil {
		return err
	}
	if err := writeJSON(s.path("installation.json"), marker{1, f.InstallationID, time.Now().UTC()}); err != nil {
		return err
	}
	if err := s.sealInstallation(f.InstallationID); err != nil {
		return err
	}
	s.mu.Lock()
	keep = true
	s.backend = b
	s.key = key
	s.csrfKey = cryptox.Derive(key, "csrf")
	s.state = Ready
	s.mu.Unlock()
	_ = os.Remove(s.path("setup.token"))
	_ = os.Remove(s.path(".install/staging.json"))
	_ = os.Remove(s.path(".install/config.pending.yaml"))
	return nil
}

// Recover resumes only an empty schema or a fully migrated installation owned by
// the same key/id. Partial DDL is never replayed speculatively.
func (s *Service) Recover(ctx context.Context, newPassword string) error {
	s.mu.RLock()
	state := s.state
	s.mu.RUnlock()
	if state != RecoveryRequired {
		return errors.New("instance does not require recovery")
	}
	var j checkpoint
	if err := readJSON(s.path(".install/staging.json"), &j); err != nil || j.Version != 1 {
		return errors.New("no valid installation checkpoint")
	}
	f, err := config.Read(s.path(".install/config.pending.yaml"))
	if errors.Is(err, os.ErrNotExist) {
		f, err = config.Read(s.path("config.yaml"))
	}
	if err != nil || f.InstallationID != j.InstallationID {
		return errors.New("configuration does not match checkpoint")
	}
	key, err := filestore.ReadPrivate(s.path("master.key"), 32)
	if err != nil || len(key) != 32 {
		return errors.New("original master key is required")
	}
	d := f.Database
	d.Password, err = cryptox.Decrypt(key, f.InstallationID, "database.password", d.Password)
	if err != nil {
		return err
	}
	d.CAFile = s.runtime.DatabaseCA
	b, err := s.connect(d, true)
	if err != nil {
		return err
	}
	defer func() { _ = b.Close() }()
	unlock, err := b.Lock(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	if err = b.Check(ctx, false); err != nil {
		return err
	}
	if err = b.SchemaReady(ctx); err != nil {
		if !errors.Is(err, ErrIncompleteSchema) || b.Check(ctx, true) != nil {
			return errors.New("migration is incomplete; inspect the schema before recovery")
		}
		if err = b.Migrate(ctx); err != nil {
			return errors.New("migration interrupted; inspect the schema before recovery")
		}
		if err = b.SchemaReady(ctx); err != nil {
			return err
		}
		j.Stage = "migrated"
		if err = writeJSON(s.path(".install/staging.json"), j); err != nil {
			return err
		}
	}
	i, err := b.Instance(ctx)
	if err != nil {
		return err
	}
	if i == nil {
		hash, err := password.Hash(newPassword)
		if err != nil {
			return ErrFounderPasswordRequired
		}
		if err = b.Seed(ctx, Seed{j.InstallationID, cryptox.Fingerprint(key), j.Site, j.Username, j.DisplayName, hash, time.Now().UTC()}); err != nil {
			return err
		}
	} else if i.InstallationID != j.InstallationID || subtle.ConstantTimeCompare(i.KeyFingerprint, cryptox.Fingerprint(key)) != 1 {
		return errors.New("database belongs to a different installation")
	}
	if err = s.finish(ctx, *f, key); err != nil {
		return err
	}
	return nil
}
func readJSON(path string, v any) error {
	b, err := filestore.ReadPrivate(path, 65536)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}
func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return filestore.AtomicWrite(path, append(b, '\n'))
}

func (s *Service) Migrate(ctx context.Context, username, secret string) error {
	f, err := config.Read(s.path("config.yaml"))
	if err != nil {
		return errors.New("an installed configuration is required")
	}
	if !exists(s.path("installation.json")) {
		return errors.New("incomplete installations must use setup recover")
	}
	var m marker
	if err := readJSON(s.path("installation.json"), &m); err != nil || m.Version != 1 || m.InstallationID != f.InstallationID {
		return errors.New("local installation identity does not match configuration")
	}
	key, err := filestore.ReadPrivate(s.path("master.key"), 32)
	if err != nil {
		return err
	}
	d := f.Database
	d.Password, err = cryptox.Decrypt(key, f.InstallationID, "database.password", d.Password)
	if err != nil {
		return err
	}
	if username != "" || secret != "" {
		if username == "" || secret == "" {
			return errors.New("both migration username and password are required")
		}
		d.Username = username
		d.Password = secret
	}
	d.CAFile = s.runtime.DatabaseCA
	b, err := s.connect(d, true)
	if err != nil {
		return err
	}
	defer func() { _ = b.Close() }()
	unlock, err := b.Lock(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	i, err := b.Instance(ctx)
	if err != nil || i == nil || i.InstallationID != f.InstallationID || subtle.ConstantTimeCompare(i.KeyFingerprint, cryptox.Fingerprint(key)) != 1 {
		return errors.New("database identity does not match installation")
	}
	if err = b.Migrate(ctx); err != nil {
		return err
	}
	return b.SchemaReady(ctx)
}
