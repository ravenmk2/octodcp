// Package media normalizes avatars and confines file access to the media root.
package media

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"image"
	_ "image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/ravenmk2/octodcp/internal/platform/cryptox"
	"github.com/ravenmk2/octodcp/internal/platform/filestore"
	"github.com/ravenmk2/octodcp/internal/platform/httpx"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

const MaxInput = 2 << 20
const MaxOutput = 512 << 10

var keyPattern = regexp.MustCompile(`^avatars/[0-9a-f]{2}/[0-9a-f]{32}\.png$`)
var temporaryPattern = regexp.MustCompile(`^avatars/[0-9a-f]{2}/\.tmp-[0-9a-f]{32}$`)

type File struct {
	Key  string
	Hash []byte
	Size int
}

// Store keeps HTTP and maintenance independent of the local filesystem adapter.
// Write leases the new key until Release; cleanup must skip leased keys.
type Store interface {
	Write(context.Context, []byte) (File, error)
	Release(string)
	Read(string) ([]byte, error)
	Remove(string) error
	Candidates(context.Context, time.Time) ([]string, error)
}
type Storage struct {
	dir    string
	slots  chan struct{}
	mu     sync.Mutex
	active map[string]bool
}

func New(dataDir string) *Storage {
	return &Storage{dir: filepath.Join(dataDir, "media"), slots: make(chan struct{}, 2), active: map[string]bool{}}
}
func Normalize(input []byte) ([]byte, error) {
	if len(input) > MaxInput {
		return nil, httpx.E(413, "PAYLOAD_TOO_LARGE", "头像图片不能超过 2 MB")
	}
	// Sniff the bytes, rather than trusting the filename or multipart MIME header.
	supported := bytes.HasPrefix(input, []byte{137, 80, 78, 71, 13, 10, 26, 10}) || bytes.HasPrefix(input, []byte{255, 216, 255}) || (len(input) >= 12 && string(input[:4]) == "RIFF" && string(input[8:12]) == "WEBP")
	if !supported {
		return nil, httpx.E(415, "UNSUPPORTED_MEDIA_TYPE", "请选择 JPG、PNG 或 WebP 图片")
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(input))
	if err != nil {
		return nil, httpx.Invalid("file", "invalid_image")
	}
	if format != "jpeg" && format != "png" && format != "webp" {
		return nil, httpx.E(415, "UNSUPPORTED_MEDIA_TYPE", "图片格式不受支持")
	}
	if cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 4096 || cfg.Height > 4096 || int64(cfg.Width)*int64(cfg.Height) > 16777216 {
		return nil, httpx.Invalid("file", "invalid_dimensions")
	}
	source, _, err := image.Decode(bytes.NewReader(input))
	if err != nil {
		return nil, httpx.Invalid("file", "invalid_image")
	}
	bounds := source.Bounds()
	side := min(bounds.Dx(), bounds.Dy())
	x := bounds.Min.X + (bounds.Dx()-side)/2
	y := bounds.Min.Y + (bounds.Dy()-side)/2
	dst := image.NewNRGBA(image.Rect(0, 0, 256, 256))
	draw.CatmullRom.Scale(dst, dst.Bounds(), source, image.Rect(x, y, x+side, y+side), draw.Src, nil)
	var output bytes.Buffer
	if err = png.Encode(&output, dst); err != nil {
		return nil, err
	}
	if output.Len() > MaxOutput {
		return nil, httpx.Invalid("file", "image_too_large")
	}
	return output.Bytes(), nil
}
func (s *Storage) Write(ctx context.Context, input []byte) (File, error) {
	if err := ctx.Err(); err != nil {
		return File{}, err
	}
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	case <-ctx.Done():
		return File{}, ctx.Err()
	default:
		return File{}, httpx.E(429, "RATE_LIMITED", "图片处理繁忙，请稍后重试")
	}
	data, err := Normalize(input)
	if err != nil {
		return File{}, err
	}
	if err = filestore.EnsurePrivateDir(s.dir); err != nil {
		return File{}, err
	}
	root, err := os.OpenRoot(s.dir)
	if err != nil {
		return File{}, err
	}
	defer func() { _ = root.Close() }()
	token := cryptox.ID()
	key := "avatars/" + token[:2] + "/" + token + ".png"
	s.mu.Lock()
	s.active[key] = true
	s.mu.Unlock()
	success := false
	defer func() {
		if !success {
			s.Release(key)
		}
	}()
	if err = root.MkdirAll(filepath.Dir(key), 0700); err != nil {
		return File{}, err
	}
	tmp := filepath.Dir(key) + "/.tmp-" + token
	f, err := root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return File{}, err
	}
	defer func() { _ = f.Close(); _ = root.Remove(tmp) }()
	if _, err = f.Write(data); err != nil {
		return File{}, err
	}
	if err = f.Sync(); err != nil {
		return File{}, err
	}
	if err = f.Close(); err != nil {
		return File{}, err
	}
	if err = root.Rename(tmp, key); err != nil {
		return File{}, err
	}
	// Persist the directory entry as well as the image bytes on Unix.
	for _, dir := range []string{filepath.Dir(s.dir), s.dir, filepath.Join(s.dir, "avatars"), filepath.Join(s.dir, filepath.Dir(key))} {
		if err = filestore.SyncDir(dir); err != nil {
			return File{}, err
		}
	}
	success = true
	digest := sha256.Sum256(data)
	return File{key, digest[:], len(data)}, nil
}
func (s *Storage) Read(key string) ([]byte, error) {
	if !keyPattern.MatchString(key) {
		return nil, os.ErrNotExist
	}
	root, err := os.OpenRoot(s.dir)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	f, err := root.Open(key)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() || st.Size() > MaxOutput {
		return nil, errors.New("invalid media file")
	}
	return io.ReadAll(io.LimitReader(f, MaxOutput+1))
}
func (s *Storage) Release(key string) { s.mu.Lock(); delete(s.active, key); s.mu.Unlock() }
func (s *Storage) Remove(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	leaseKey := key
	if temporaryPattern.MatchString(key) {
		leaseKey = strings.Replace(key, "/.tmp-", "/", 1) + ".png"
	}
	if s.active[leaseKey] {
		return errors.New("media upload is still in progress")
	}
	if !keyPattern.MatchString(key) && !temporaryPattern.MatchString(key) {
		return errors.New("invalid media key")
	}
	root, err := os.OpenRoot(s.dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	err = root.Remove(key)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// Candidates only scans our own random filenames after a grace period. The
// caller must recheck the metadata before removing a file without a DB row.
func (s *Storage) Candidates(ctx context.Context, before time.Time) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(s.dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	candidates := []string{}
	err = filepath.WalkDir(s.dir, func(path string, d os.DirEntry, e error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if e != nil {
			return e
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		rel, e := filepath.Rel(s.dir, path)
		if e != nil {
			return e
		}
		key := filepath.ToSlash(rel)
		if !keyPattern.MatchString(key) && !temporaryPattern.MatchString(key) {
			return nil
		}
		s.mu.Lock()
		leaseKey := key
		if temporaryPattern.MatchString(key) {
			leaseKey = strings.Replace(key, "/.tmp-", "/", 1) + ".png"
		}
		active := s.active[leaseKey]
		s.mu.Unlock()
		if active {
			return nil
		}
		st, e := root.Lstat(rel)
		if e != nil {
			return e
		}
		if st.Mode().IsRegular() && st.ModTime().Before(before) {
			candidates = append(candidates, key)
		}
		if len(candidates) >= 500 {
			return io.EOF
		}
		return nil
	})
	if errors.Is(err, io.EOF) {
		err = nil
	}
	return candidates, err
}
func ValidKey(key string) bool { return keyPattern.MatchString(key) && !strings.Contains(key, "..") }
