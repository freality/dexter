package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadOrCreateKeyCreatesNewKey(t *testing.T) {
	dir := t.TempDir()
	key, err := loadOrCreateKey(dir)
	if err != nil {
		t.Fatalf("loadOrCreateKey() error = %v", err)
	}
	if len(key) != 32 {
		t.Fatalf("key length = %d, want 32", len(key))
	}
	// Verify it was written to disk
	path := filepath.Join(dir, ".dex", "secret.key")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", path, err)
	}
	if len(data) != 32 {
		t.Fatalf("on-disk key length = %d, want 32", len(data))
	}
}

func TestLoadOrCreateKeyReadsExisting(t *testing.T) {
	dir := t.TempDir()
	key1, err := loadOrCreateKey(dir)
	if err != nil {
		t.Fatalf("first loadOrCreateKey() error = %v", err)
	}
	key2, err := loadOrCreateKey(dir)
	if err != nil {
		t.Fatalf("second loadOrCreateKey() error = %v", err)
	}
	if len(key1) != 32 || len(key2) != 32 {
		t.Fatal("keys not 32 bytes")
	}
	for i := range key1 {
		if key1[i] != key2[i] {
			t.Fatal("second call returned a different key (regenerated instead of reading)")
		}
	}
}

func TestLoadOrCreateKeyFilePermissions(t *testing.T) {
	dir := t.TempDir()
	_, err := loadOrCreateKey(dir)
	if err != nil {
		t.Fatalf("loadOrCreateKey() error = %v", err)
	}
	path := filepath.Join(dir, ".dex", "secret.key")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat(%s) error = %v", path, err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("secret.key permissions = %o, want 0600", perm)
	}
}

func TestLoadOrCreateKeyCreatesDexDir(t *testing.T) {
	dir := t.TempDir()
	dexDir := filepath.Join(dir, ".dex")
	if _, err := os.Stat(dexDir); !os.IsNotExist(err) {
		t.Fatal(".dex/ already exists")
	}
	_, err := loadOrCreateKey(dir)
	if err != nil {
		t.Fatalf("loadOrCreateKey() error = %v", err)
	}
	if _, err := os.Stat(dexDir); err != nil {
		t.Fatalf(".dex/ dir not created: %v", err)
	}
}

func TestLoadOrCreateKeyRejectsWrongSize(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".dex", "secret.key")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, make([]byte, 16), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := loadOrCreateKey(dir)
	if err == nil || !strings.Contains(err.Error(), "must be 32 bytes") {
		t.Fatalf("loadOrCreateKey() error = %v, want size mismatch", err)
	}
}
func TestLoadOrCreateKeyMkdirAllFailure(t *testing.T) {
	dir := t.TempDir()
	// Make dir read-only so MkdirAll inside loadOrCreateKey fails.
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	_, err := loadOrCreateKey(dir)
	if err == nil || !strings.Contains(err.Error(), "create secret key dir") {
		t.Fatalf("loadOrCreateKey() error = %v, want create secret key dir error", err)
	}
}

func TestLoadOrCreateKeyWriteFileFailure(t *testing.T) {
	dir := t.TempDir()
	dexDir := filepath.Join(dir, ".dex")
	if err := os.MkdirAll(dexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Make .dex dir read-only so WriteFile inside loadOrCreateKey fails
	// even though MkdirAll (existing dir) succeeds.
	if err := os.Chmod(dexDir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dexDir, 0o755) })

	_, err := loadOrCreateKey(dir)
	if err == nil || !strings.Contains(err.Error(), "write secret key") {
		t.Fatalf("loadOrCreateKey() error = %v, want write secret key error", err)
	}
}

func TestLoadOrCreateKeyRandReadFailure(t *testing.T) {
	// crypto/rand.Read never fails on real systems; inject a failing reader
	// through the package-level variable for coverage.
	defer func(old func([]byte) (int, error)) { randReader = old }(randReader)
	randReader = func(b []byte) (int, error) { return 0, fmt.Errorf("injected entropy failure") }

	dir := t.TempDir()
	_, err := loadOrCreateKey(dir)
	if err == nil || !strings.Contains(err.Error(), "generate secret key") {
		t.Fatalf("loadOrCreateKey() error = %v, want generate secret key error", err)
	}
}
