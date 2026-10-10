package state

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestLockAcquireRelease(t *testing.T) {
	dir := t.TempDir()
	l := NewLock(dir)

	if err := l.Acquire(); err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	path := filepath.Join(dir, ".dex", "state.json.lock")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read lock file: %v", err)
	}
	if _, err := strconv.Atoi(string(data)); err != nil {
		t.Fatalf("lock file contents = %q, want a pid", data)
	}

	if err := l.Release(); err != nil {
		t.Fatalf("Release() error = %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("lock file still present after Release()")
	}
}

func TestLockDoubleAcquire(t *testing.T) {
	dir := t.TempDir()
	l := NewLock(dir)

	if err := l.Acquire(); err != nil {
		t.Fatalf("first Acquire() error = %v", err)
	}
	err := l.Acquire()
	if err == nil {
		t.Fatal("second Acquire() error = nil, want contention error")
	}
	if !strings.Contains(err.Error(), "state is locked by pid "+strconv.Itoa(os.Getpid())) {
		t.Fatalf("Acquire() error = %v, want pid in message", err)
	}
}

func TestLockReleaseWhenAbsent(t *testing.T) {
	dir := t.TempDir()
	l := NewLock(dir)

	if err := l.Release(); err != nil {
		t.Fatalf("Release() error = %v, want nil for absent lock", err)
	}
}

func TestLockUnreadablePidBody(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".dex"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, ".dex", "state.json.lock")
	if err := os.WriteFile(path, []byte("not-a-pid"), 0o600); err != nil {
		t.Fatal(err)
	}
	l := NewLock(dir)

	err := l.Acquire()
	if err == nil || !strings.Contains(err.Error(), "pid unknown") {
		t.Fatalf("Acquire() error = %v, want pid unknown", err)
	}
}

func TestLockEmptyPidBody(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".dex"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, ".dex", "state.json.lock")
	if err := os.WriteFile(path, []byte(""), 0o600); err != nil {
		t.Fatal(err)
	}
	l := NewLock(dir)

	err := l.Acquire()
	if err == nil || !strings.Contains(err.Error(), "pid unknown") {
		t.Fatalf("Acquire() error = %v, want pid unknown", err)
	}
}

func TestLockUnreadablePidFile(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can read anything; skipping permission test")
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".dex"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, ".dex", "state.json.lock")
	if err := os.WriteFile(path, []byte("123"), 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(path, 0o600) })
	l := NewLock(dir)

	err := l.Acquire()
	if err == nil || !strings.Contains(err.Error(), "pid unknown") {
		t.Fatalf("Acquire() error = %v, want pid unknown", err)
	}
}

func TestLockReleaseFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions; skipping")
	}
	dir := t.TempDir()
	l := NewLock(dir)
	if err := l.Acquire(); err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	dexDir := filepath.Join(dir, ".dex")
	if err := os.Chmod(dexDir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dexDir, 0o755) })

	err := l.Release()
	if err == nil || !strings.Contains(err.Error(), "release lock") {
		t.Fatalf("Release() error = %v, want release error", err)
	}
}

func TestLockAcquireMkdirAllFailure(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".dex"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	l := NewLock(dir)

	err := l.Acquire()
	if err == nil || !strings.Contains(err.Error(), "create lock dir") {
		t.Fatalf("Acquire() error = %v, want mkdir error", err)
	}
}

func TestLockAcquireOpenFailureNotExist(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions; skipping")
	}
	dir := t.TempDir()
	// A read-only .dex dir makes OpenFile fail with permission denied, not
	// IsExist.
	dexDir := filepath.Join(dir, ".dex")
	if err := os.MkdirAll(dexDir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dexDir, 0o755) })
	l := NewLock(dir)

	err := l.Acquire()
	if err == nil || !strings.Contains(err.Error(), "acquire lock") {
		t.Fatalf("Acquire() error = %v, want acquire lock error", err)
	}
}
