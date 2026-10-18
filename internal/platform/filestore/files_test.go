package filestore

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAtomicReplacementAndExclusiveCreation(t *testing.T) {
	dir := t.TempDir()
	if err := EnsurePrivateDir(dir); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "master.key")
	if err := CreatePrivate(path, []byte("first")); err != nil {
		t.Fatal(err)
	}
	if err := CreatePrivate(path, []byte("replacement")); err == nil {
		t.Fatal("exclusive file was replaced")
	}
	if err := AtomicWrite(path, []byte("second")); err != nil {
		t.Fatal(err)
	}
	value, err := ReadPrivate(path, 64)
	if err != nil || string(value) != "second" {
		t.Fatal("atomic replacement failed", err)
	}
	if _, err = ReadPrivate(path, 1); err == nil {
		t.Fatal("oversized private file accepted")
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatal("temporary files were left behind")
	}
}
