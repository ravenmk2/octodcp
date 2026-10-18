package mysql

import (
	"context"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	driver "github.com/go-sql-driver/mysql"
	"github.com/ravenmk2/octodcp/internal/modules/install"
	"github.com/ravenmk2/octodcp/internal/platform/config"
	"github.com/ravenmk2/octodcp/internal/platform/cryptox"
	"github.com/ravenmk2/octodcp/internal/platform/password"
)

// This test intentionally leaves its dedicated schema intact for inspection.
// It never drops an existing schema or accepts the application's live database.
func TestMySQLInstallation(t *testing.T) {
	dsn := os.Getenv("OCTODCP_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("set OCTODCP_TEST_MYSQL_DSN to a dedicated empty octodcp_test database")
	}
	parsed, err := driver.ParseDSN(dsn)
	if err != nil {
		t.Fatal("invalid test DSN")
	}
	if parsed.DBName != "octodcp_test" && !strings.HasPrefix(parsed.DBName, "octodcp_test_") {
		t.Fatal("integration tests require a dedicated octodcp_test database")
	}
	// Use the driver-parsed address; all queries still go through the real MySQL adapter.
	d := config.Database{Host: "127.0.0.1", Port: 3306, Name: parsed.DBName, Username: parsed.User, Password: parsed.Passwd, TLSMode: "disabled"}
	if parsed.Net != "tcp" {
		t.Fatal("test database must use TCP")
	}
	host, port, err := splitTestAddress(parsed.Addr)
	if err != nil {
		t.Fatal(err)
	}
	d.Host = host
	d.Port = port
	b, err := Open(d, true)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = b.Close() }()
	s := b.(*Store)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	unlock, err := b.Lock(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if err = b.Check(ctx, true); err != nil {
		t.Fatal("test database must be empty and compatible", err)
	}
	if err = b.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err = b.SchemaReady(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.ExecContext(ctx, "ALTER TABLE `user` AUTO_INCREMENT=42"); err != nil {
		t.Fatal(err)
	}
	hash, err := password.Hash("installation-test-password")
	if err != nil {
		t.Fatal(err)
	}
	seed := install.Seed{InstallationID: cryptox.ID(), Fingerprint: cryptox.Fingerprint(cryptox.Random(32)), Site: install.Site{Name: "协作系统", Timezone: "Asia/Shanghai"}, Username: "founder", DisplayName: "创始人", PasswordHash: hash, Now: time.Now().UTC()}
	if err = b.Seed(ctx, seed); err != nil {
		t.Fatal(err)
	}
	i, err := b.Instance(ctx)
	if err != nil || i == nil || i.FounderID != 42 || i.InstallationID != seed.InstallationID {
		t.Fatal("incorrect founder reference", err)
	}
	var name string
	if err = s.db.GetContext(ctx, &name, "SELECT JSON_UNQUOTE(value) FROM system_setting WHERE name='site.name'"); err != nil || name != "协作系统" {
		t.Fatal("JSON/Unicode roundtrip failed", err)
	}
	if err = b.Check(ctx, true); err == nil {
		t.Fatal("installed database treated as empty")
	}
	if err = b.Seed(ctx, seed); err == nil {
		t.Fatal("second initialization accepted")
	}
}

func splitTestAddress(addr string) (string, int, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", 0, err
	}
	n, err := strconv.Atoi(port)
	return host, n, err
}
