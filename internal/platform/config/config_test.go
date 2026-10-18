package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestPrecedenceAndPrivateConfig(t *testing.T) {
	dir := t.TempDir()
	f := File{SchemaVersion: 1, InstallationID: "0123456789abcdef0123456789abcdef", Server: Server{Listen: "127.0.0.1:9000", PublicURL: "https://file.example.com", TrustedProxies: []string{}}, Database: Database{Password: "enc:v1:placeholder", CAFile: "/file/mysql-ca.pem"}}
	if err := Write(filepath.Join(dir, "config.yaml"), f); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OCTODCP_DATA_DIR", filepath.Join(dir, "other"))
	t.Setenv("OCTODCP_LISTEN", "127.0.0.1:9001")
	t.Setenv("OCTODCP_PUBLIC_URL", "https://env.example.com")
	t.Setenv("OCTODCP_DATABASE_CA_FILE", "/environment/mysql-ca.pem")
	r, loaded, err := Load(Overrides{DataDir: dir, Listen: "127.0.0.1:9002"})
	if err != nil {
		t.Fatal(err)
	}
	if r.DataDir != dir || r.Server.Listen != "127.0.0.1:9002" || r.Server.PublicURL != "https://env.example.com" || loaded.Database.Password != "enc:v1:placeholder" || r.DatabaseCA != "/environment/mysql-ca.pem" {
		t.Fatal("precedence changed")
	}
	t.Setenv("OCTODCP_DATABASE_CA_FILE", "")
	r, _, err = Load(Overrides{DataDir: dir})
	if err != nil || r.DatabaseCA != "" {
		t.Fatal("explicit empty CA override was ignored", err)
	}
}

func TestRejectUnsafePublicURL(t *testing.T) {
	for _, url := range []string{"http://user:pass@example.com", "https://example.com/subpath", "https://example.com?secret=x", "//example.com", "ftp://example.com", "https://example.com/#fragment"} {
		if ValidateServer(Server{Listen: "127.0.0.1:0", PublicURL: url}) == nil {
			t.Errorf("accepted %q", url)
		}
	}
}

func TestCorruptConfigIsMaintenanceNotFreshInstall(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("not: a valid config"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OCTODCP_PUBLIC_URL", "https://maintenance.example.com")
	r, _, err := Load(Overrides{DataDir: dir})
	var fileErr *FileError
	if !errors.As(err, &fileErr) || r.Server.PublicURL != "https://maintenance.example.com" {
		t.Fatal("corrupt configuration lost deployment overrides", err)
	}
}

func TestBlankConfigurationUsesBootstrapDefaults(t *testing.T) {
	for _, content := range []string{"", " \t\n"} {
		dir := t.TempDir()
		path := filepath.Join(dir, "config.yaml")
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := Read(path); !errors.Is(err, ErrEmptyFile) {
			t.Fatal("blank file was not recognized", err)
		}
		if _, loaded, err := Load(Overrides{DataDir: dir}); err != nil || loaded != nil {
			t.Fatal("blank file did not allow bootstrap defaults", err)
		}
	}
}
