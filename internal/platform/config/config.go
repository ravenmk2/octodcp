package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/ravenmk2/octodcp/internal/platform/filestore"
	"gopkg.in/yaml.v3"
)

type Server struct {
	Listen         string   `yaml:"listen"`
	PublicURL      string   `yaml:"public_url"`
	TrustedProxies []string `yaml:"trusted_proxies"`
}
type Database struct {
	Host     string `yaml:"host" json:"host"`
	Port     int    `yaml:"port" json:"port"`
	Name     string `yaml:"name" json:"name"`
	Username string `yaml:"username" json:"username"`
	Password string `yaml:"password" json:"password"`
	TLSMode  string `yaml:"tls_mode" json:"tls_mode"`
	CAFile   string `yaml:"ca_file" json:"-"`
}
type File struct {
	SchemaVersion  int      `yaml:"schema_version"`
	InstallationID string   `yaml:"installation_id"`
	Server         Server   `yaml:"server"`
	Database       Database `yaml:"database"`
}
type Runtime struct {
	DataDir    string
	Server     Server
	DatabaseCA string
}

var ErrEmptyFile = errors.New("configuration file is empty")

type Overrides struct{ DataDir, Listen, PublicURL string }

type FileError struct{ Err error }

func (e *FileError) Error() string { return "configuration requires maintenance" }
func (e *FileError) Unwrap() error { return e.Err }

func ResolveDataDir(value string) (string, error) {
	if value == "" {
		value = os.Getenv("OCTODCP_DATA_DIR")
	}
	if value == "" {
		value = "data"
	}
	path, err := filepath.Abs(value)
	if err != nil {
		return "", err
	}
	if filepath.Dir(path) == path {
		return "", errors.New("data directory cannot be a filesystem root")
	}
	return filepath.Clean(path), nil
}

func Load(o Overrides) (Runtime, *File, error) {
	dir, err := ResolveDataDir(o.DataDir)
	if err != nil {
		return Runtime{}, nil, err
	}
	r := Runtime{DataDir: dir, Server: Server{Listen: "0.0.0.0:8080", PublicURL: "http://localhost:8080", TrustedProxies: []string{}}, DatabaseCA: os.Getenv("OCTODCP_DATABASE_CA_FILE")}
	f, fileErr := Read(filepath.Join(dir, "config.yaml"))
	if errors.Is(fileErr, os.ErrNotExist) || errors.Is(fileErr, ErrEmptyFile) {
		fileErr = nil
	}
	if f != nil {
		r.Server = f.Server
		if _, overridden := os.LookupEnv("OCTODCP_DATABASE_CA_FILE"); !overridden {
			r.DatabaseCA = f.Database.CAFile
		}
	}
	if v, ok := os.LookupEnv("OCTODCP_LISTEN"); ok {
		r.Server.Listen = v
	}
	if v, ok := os.LookupEnv("OCTODCP_PUBLIC_URL"); ok {
		r.Server.PublicURL = v
	}
	if v, ok := os.LookupEnv("OCTODCP_TRUSTED_PROXIES"); ok {
		r.Server.TrustedProxies = nil
		if v != "" {
			for _, p := range strings.Split(v, ",") {
				r.Server.TrustedProxies = append(r.Server.TrustedProxies, strings.TrimSpace(p))
			}
		}
	}
	if o.Listen != "" {
		r.Server.Listen = o.Listen
	}
	if o.PublicURL != "" {
		r.Server.PublicURL = o.PublicURL
	}
	if err := ValidateServer(r.Server); err != nil {
		return r, f, err
	}
	if fileErr != nil {
		return r, f, &FileError{fileErr}
	}
	return r, f, nil
}

func Read(path string) (*File, error) {
	b, err := filestore.ReadPrivate(path, 65536)
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(b)) == 0 {
		return nil, ErrEmptyFile
	}
	d := yaml.NewDecoder(bytes.NewReader(b))
	d.KnownFields(true)
	var f File
	if err := d.Decode(&f); err != nil {
		return nil, errors.New("invalid configuration file")
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return nil, errors.New("configuration must contain one document")
	}
	if f.SchemaVersion != 1 || len(f.InstallationID) != 32 {
		return nil, errors.New("unsupported configuration format")
	}
	if err := ValidateServer(f.Server); err != nil {
		return nil, err
	}
	return &f, nil
}
func Write(path string, f File) error {
	b, err := yaml.Marshal(f)
	if err != nil {
		return err
	}
	return filestore.AtomicWrite(path, b)
}

func ValidateServer(s Server) error {
	if _, _, err := net.SplitHostPort(s.Listen); err != nil {
		return errors.New("invalid listen address")
	}
	u, err := url.Parse(s.PublicURL)
	if err != nil || u.Host == "" || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" || (u.Path != "" && u.Path != "/") {
		return errors.New("public URL must be an absolute HTTP(S) origin")
	}
	for _, p := range s.TrustedProxies {
		if _, err := netip.ParsePrefix(p); err != nil {
			if _, err := netip.ParseAddr(p); err != nil {
				return errors.New("invalid trusted proxy address")
			}
		}
	}
	return nil
}
func (r Runtime) SecureCookies() bool { return strings.HasPrefix(r.Server.PublicURL, "https://") }
func (r Runtime) Origin() string      { return strings.TrimSuffix(r.Server.PublicURL, "/") }
func (d Database) Validate() error {
	if d.Host == "" || len(d.Host) > 253 || d.Port < 1 || d.Port > 65535 || d.Name == "" || len(d.Name) > 64 || d.Username == "" || len(d.Username) > 64 || len(d.Password) > 4096 {
		return errors.New("invalid database connection fields")
	}
	if strings.ContainsAny(d.Host, "/\\\x00") || strings.ContainsRune(d.Name, 0) {
		return errors.New("invalid database host or name")
	}
	if d.TLSMode != "disabled" && d.TLSMode != "verify_identity" {
		return fmt.Errorf("unsupported database TLS mode")
	}
	return nil
}
