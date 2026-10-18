package mysql

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"encoding/hex"
	"errors"
	"net"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	driver "github.com/go-sql-driver/mysql"
	"github.com/jmoiron/sqlx"
	"github.com/pressly/goose/v3"
	"github.com/ravenmk2/octodcp/internal/modules/install"
	"github.com/ravenmk2/octodcp/internal/modules/settings"
	"github.com/ravenmk2/octodcp/internal/platform/config"
	"github.com/ravenmk2/octodcp/internal/platform/httpx"
	"github.com/ravenmk2/octodcp/internal/store/migrations"
)

type Store struct {
	db   *sqlx.DB
	name string
}
type silentLogger struct{}

func (silentLogger) Print(...any) {}

func Open(d config.Database, migration bool) (install.Backend, error) {
	if err := d.Validate(); err != nil {
		return nil, err
	}
	cfg := driver.NewConfig()
	cfg.User = d.Username
	cfg.Passwd = d.Password
	cfg.Net = "tcp"
	cfg.Addr = net.JoinHostPort(d.Host, strconv.Itoa(d.Port))
	cfg.DBName = d.Name
	cfg.ParseTime = true
	cfg.Loc = time.UTC
	cfg.Timeout = 5 * time.Second
	cfg.ReadTimeout = 15 * time.Second
	cfg.WriteTimeout = 15 * time.Second
	cfg.Params = map[string]string{"time_zone": "'+00:00'"}
	cfg.Collation = "utf8mb4_0900_ai_ci"
	cfg.MultiStatements = migration
	cfg.Logger = silentLogger{}
	if d.TLSMode == "verify_identity" {
		cfg.TLS = &tls.Config{MinVersion: tls.VersionTLS12, ServerName: d.Host}
		if d.CAFile != "" {
			pem, err := os.ReadFile(d.CAFile)
			if err != nil {
				return nil, errors.New("cannot read database CA")
			}
			pool, err := x509.SystemCertPool()
			if err != nil {
				pool = x509.NewCertPool()
			}
			if !pool.AppendCertsFromPEM(pem) {
				return nil, errors.New("invalid database CA")
			}
			cfg.TLS.RootCAs = pool
		}
	}
	connector, err := driver.NewConnector(cfg)
	if err != nil {
		return nil, errors.New("invalid database connection configuration")
	}
	db := sqlx.NewDb(sql.OpenDB(connector), "mysql")
	db.SetMaxOpenConns(12)
	db.SetMaxIdleConns(4)
	db.SetConnMaxLifetime(3 * time.Minute)
	return &Store{db: db, name: d.Name}, nil
}
func (s *Store) Close() error { return s.db.Close() }

var versionPattern = regexp.MustCompile(`^(\d+)\.(\d+)\.(\d+)`)

func CompatibleVersion(version string) bool {
	if strings.Contains(strings.ToLower(version), "mariadb") {
		return false
	}
	m := versionPattern.FindStringSubmatch(version)
	if len(m) != 4 {
		return false
	}
	major, _ := strconv.Atoi(m[1])
	minor, _ := strconv.Atoi(m[2])
	patch, _ := strconv.Atoi(m[3])
	return major == 8 && (minor > 0 || patch >= 16)
}
func (s *Store) Check(ctx context.Context, empty bool) error {
	if err := s.db.PingContext(ctx); err != nil {
		return httpx.E(503, "DATABASE_UNAVAILABLE", "无法连接数据库，请检查部署配置")
	}
	var version string
	if err := s.db.GetContext(ctx, &version, "SELECT VERSION()"); err != nil {
		return err
	}
	if !CompatibleVersion(version) {
		return httpx.E(422, "DATABASE_INCOMPATIBLE", "需要 MySQL 8.0.16 及以上的 8.x 版本")
	}
	var charset string
	if err := s.db.GetContext(ctx, &charset, "SELECT DEFAULT_CHARACTER_SET_NAME FROM information_schema.SCHEMATA WHERE SCHEMA_NAME = DATABASE()"); err != nil {
		return err
	}
	if charset != "utf8mb4" {
		return httpx.E(422, "DATABASE_INCOMPATIBLE", "数据库字符集必须为 utf8mb4")
	}
	if empty {
		var count int
		if err := s.db.GetContext(ctx, &count, "SELECT COUNT(*) FROM information_schema.TABLES WHERE TABLE_SCHEMA=DATABASE()"); err != nil {
			return err
		}
		if count != 0 {
			return httpx.E(409, "DATABASE_NOT_EMPTY", "安装需要专用的空数据库")
		}
	}
	return nil
}

func (s *Store) Lock(ctx context.Context) (func(), error) {
	c, err := s.db.Connx(ctx)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256([]byte(s.name))
	name := "octodcp:install:" + hex.EncodeToString(digest[:16])
	var locked sql.NullInt64
	err = c.GetContext(ctx, &locked, "SELECT GET_LOCK(?, 0)", name)
	if err != nil || !locked.Valid || locked.Int64 != 1 {
		_ = c.Close()
		return nil, httpx.E(409, "INSTALL_IN_PROGRESS", "数据库正在初始化")
	}
	return func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_, _ = c.ExecContext(ctx, "SELECT RELEASE_LOCK(?)", name)
		_ = c.Close()
	}, nil
}

func (s *Store) Migrate(ctx context.Context) error {
	p, err := goose.NewProvider(goose.DialectMySQL, s.db.DB, migrations.FS, goose.WithDisableGlobalRegistry(true), goose.WithLogger(discardGooseLogger{}))
	if err != nil {
		return err
	}
	current, target, err := p.GetVersions(ctx)
	if err != nil {
		return err
	}
	if current > target {
		return errors.New("database is newer than this executable")
	}
	_, err = p.Up(ctx)
	return err
}

type discardGooseLogger struct{}

func (discardGooseLogger) Printf(string, ...any) {}
func (discardGooseLogger) Fatalf(string, ...any) {}

func (s *Store) SchemaReady(ctx context.Context) error {
	var v int64
	err := s.db.GetContext(ctx, &v, "SELECT version_id FROM goose_db_version WHERE is_applied = TRUE ORDER BY id DESC LIMIT 1")
	var databaseError *driver.MySQLError
	if errors.Is(err, sql.ErrNoRows) || errors.As(err, &databaseError) && databaseError.Number == 1146 || err == nil && v == 0 {
		return install.ErrIncompleteSchema
	}
	if err != nil {
		return err
	}
	if v != 1 {
		return errors.New("database migration version does not match this executable")
	}
	var tables int
	if err = s.db.GetContext(ctx, &tables, "SELECT COUNT(*) FROM information_schema.TABLES WHERE TABLE_SCHEMA=DATABASE() AND TABLE_TYPE='BASE TABLE' AND TABLE_NAME IN ('user','user_password','avatar_file','session','system_setting','system_instance','audit_event')"); err != nil {
		return err
	}
	if tables != 7 {
		return install.ErrIncompleteSchema
	}
	instance, err := s.Instance(ctx)
	if err != nil {
		if errors.As(err, &databaseError) && databaseError.Number == 1146 {
			return install.ErrIncompleteSchema
		}
		return err
	}
	if instance != nil {
		var rows []struct {
			Name    string `db:"name"`
			Value   []byte `db:"value"`
			Version uint64 `db:"row_version"`
		}
		if err = s.db.SelectContext(ctx, &rows, "SELECT name,value,row_version FROM system_setting WHERE name IN ('site.name','site.timezone','auth.registration_enabled','auth.session_ttl_days') ORDER BY name"); err != nil {
			return err
		}
		entries := make([]settings.Entry, 0, len(rows))
		for _, row := range rows {
			entries = append(entries, settings.Entry{Name: row.Name, Value: row.Value, Version: row.Version})
		}
		if _, err = settings.Parse(entries); err != nil {
			return errors.New("required system settings are invalid")
		}
	}
	return nil
}
func (s *Store) Instance(ctx context.Context) (*install.Instance, error) {
	var i install.Instance
	err := s.db.GetContext(ctx, &i, "SELECT installation_id, key_fingerprint, founder_user_id FROM system_instance WHERE id=1")
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &i, nil
}

func (s *Store) Seed(ctx context.Context, seed install.Seed) error {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var count int
	if err = tx.GetContext(ctx, &count, "SELECT COUNT(*) FROM `user`"); err != nil {
		return err
	}
	if count != 0 {
		return errors.New("database already contains users; initialization refused")
	}
	r, err := tx.ExecContext(ctx, "INSERT INTO `user` (username,display_name,role,status,created_at,updated_at) VALUES (?,?,'admin','active',?,?)", seed.Username, seed.DisplayName, seed.Now, seed.Now)
	if err != nil {
		return err
	}
	id, err := r.LastInsertId()
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO user_password (user_id,password_hash,require_password_change,password_changed_at) VALUES (?,?,FALSE,?)", id, seed.PasswordHash, seed.Now); err != nil {
		return err
	}
	for _, entry := range settings.Defaults(seed.Site.Name, seed.Site.Timezone) {
		if _, err = tx.ExecContext(ctx, "INSERT INTO system_setting (name,value,created_at,updated_at) VALUES (?,CAST(? AS JSON),?,?)", entry.Name, string(entry.Value), seed.Now, seed.Now); err != nil {
			return err
		}
	}

	if _, err = tx.ExecContext(ctx, "INSERT INTO system_instance (id,installation_id,founder_user_id,key_fingerprint,installed_at) VALUES (1,?,?,?,?)", seed.InstallationID, id, seed.Fingerprint, seed.Now); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO audit_event (actor_user_id,actor_type,actor_name,actor_username,action,target_type,target_id,target_label,occurred_at,detail) VALUES (?,'user',?,?,'installation.completed','system',?,?,?,CAST(? AS JSON))", id, seed.DisplayName, seed.Username, seed.InstallationID, seed.Site.Name, seed.Now, `{"master_key_initialized":true}`); err != nil {
		return err
	}
	return tx.Commit()
}
