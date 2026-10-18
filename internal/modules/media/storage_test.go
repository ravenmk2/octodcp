package media

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func samplePNG(t *testing.T) []byte {
	t.Helper()
	m := image.NewNRGBA(image.Rect(0, 0, 600, 300))
	for y := range 300 {
		for x := range 600 {
			c := color.NRGBA{R: 255, A: 255}
			if x >= 150 && x < 450 {
				c = color.NRGBA{G: 200, A: 200}
			}
			m.SetNRGBA(x, y, c)
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, m); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func TestNormalizationAndLimits(t *testing.T) {
	source := samplePNG(t)
	data, err := Normalize(source)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil || img.Bounds().Dx() != 256 || img.Bounds().Dy() != 256 {
		t.Fatal("not normalized", err)
	}
	r, g, _, a := img.At(128, 128).RGBA()
	if r > 100 || g < 1000 || a == 65535 {
		t.Fatal("center crop/alpha lost")
	}
	var jpegData bytes.Buffer
	if err = jpeg.Encode(&jpegData, img, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = Normalize(jpegData.Bytes()); err != nil {
		t.Fatal(err)
	}
	webp, err := base64.StdEncoding.DecodeString("UklGRiIAAABXRUJQVlA4IBYAAAAwAQCdASoBAAEADsD+JaQAA3AAAAAA")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = Normalize(webp); err != nil {
		t.Fatal("WebP decoder", err)
	}
	oversizeDimensions := append([]byte(nil), source...)
	binary.BigEndian.PutUint32(oversizeDimensions[16:20], 50000)
	binary.BigEndian.PutUint32(oversizeDimensions[29:33], crc32.ChecksumIEEE(oversizeDimensions[12:29]))
	for _, bad := range [][]byte{[]byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`), []byte("GIF89a"), source[:20], oversizeDimensions, make([]byte, MaxInput+1)} {
		if _, err = Normalize(bad); err == nil {
			t.Fatal("accepted invalid image")
		}
	}
}
func TestConfinedStorageAndGarbageCandidates(t *testing.T) {
	dir := t.TempDir()
	store := New(dir)
	if _, err := os.Stat(filepath.Join(dir, "media")); !os.IsNotExist(err) {
		t.Fatal("created files before installation")
	}
	file, err := store.Write(context.Background(), samplePNG(t))
	if err != nil {
		t.Fatal(err)
	}
	data, err := store.Read(file.Key)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	if !bytes.Equal(file.Hash, digest[:]) || file.Size != len(data) {
		t.Fatal("metadata mismatch")
	}
	if err = store.Remove(file.Key); err == nil {
		t.Fatal("deleted in-flight upload")
	}
	path := filepath.Join(dir, "media", filepath.FromSlash(file.Key))
	old := time.Now().Add(-48 * time.Hour)
	if err = os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	if candidates, err := store.Candidates(context.Background(), time.Now().Add(-24*time.Hour)); err != nil || len(candidates) != 0 {
		t.Fatal(candidates, err)
	}
	store.Release(file.Key)
	if candidates, err := store.Candidates(context.Background(), time.Now().Add(-24*time.Hour)); err != nil || len(candidates) != 1 {
		t.Fatal(candidates, err)
	}
	crashed := filepath.Join(filepath.Dir(path), ".tmp-"+filepath.Base(file.Key)[:32])
	if err = os.WriteFile(crashed, []byte("incomplete"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Chtimes(crashed, old, old); err != nil {
		t.Fatal(err)
	}
	if candidates, err := store.Candidates(context.Background(), time.Now().Add(-24*time.Hour)); err != nil || len(candidates) != 2 {
		t.Fatal(candidates, err)
	}
	for _, invalid := range []string{"../secret", "/etc/passwd", "avatars/aa/../../secret", `avatars\aa\secret.png`} {
		if _, e := store.Read(invalid); e == nil {
			t.Fatal("read escaped root")
		}
		if e := store.Remove(invalid); e == nil {
			t.Fatal("removed escaped root")
		}
	}
	// A symlink with a syntactically valid key still cannot escape os.Root.
	outside := filepath.Join(t.TempDir(), "private.png")
	if err = os.WriteFile(outside, []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	linkKey := "avatars/aa/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.png"
	if err = os.MkdirAll(filepath.Join(dir, "media", "avatars", "aa"), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(outside, filepath.Join(dir, "media", filepath.FromSlash(linkKey))); err == nil {
		if _, err = store.Read(linkKey); err == nil {
			t.Fatal("read external symlink")
		}
	}
	if err = store.Remove(file.Key); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Read(file.Key); !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if err = store.Remove(file.Key); err != nil {
		t.Fatal(err)
	}
}
