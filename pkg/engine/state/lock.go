package state

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

// Lock is an advisory, pid-stamped lock file used only by apply.
type Lock struct {
	path string
}

// NewLock returns a Lock at <projectRoot>/.dex/state.json.lock.
func NewLock(projectRoot string) *Lock {
	return &Lock{path: filepath.Join(projectRoot, ".dex", "state.json.lock")}
}

// Acquire creates the lock file, failing if one already exists.
//
// ponytail: no stale-lock reaping; add a pid-liveness check if users hit it.
func (l *Lock) Acquire() error {
	if err := os.MkdirAll(filepath.Dir(l.path), 0o755); err != nil {
		return fmt.Errorf("create lock dir: %w", err)
	}
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		if os.IsExist(err) {
			return fmt.Errorf("state is locked by pid %s (%s); remove it if the process is gone", l.heldBy(), l.path)
		}
		return fmt.Errorf("acquire lock %s: %w", l.path, err)
	}
	defer f.Close()
	// A write failure here is an unreachable disk-full/kernel-fault case;
	// not worth a contrived test.
	fmt.Fprintf(f, "%d", os.Getpid())
	return nil
}

// Release removes the lock file. Already-gone is not an error.
func (l *Lock) Release() error {
	if err := os.Remove(l.path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("release lock %s: %w", l.path, err)
	}
	return nil
}

// heldBy returns the pid recorded in the lock file, or "unknown" when the
// file is empty or unreadable.
func (l *Lock) heldBy() string {
	data, err := os.ReadFile(l.path)
	if err != nil || len(data) == 0 {
		return "unknown"
	}
	if _, err := strconv.Atoi(string(data)); err != nil {
		return "unknown"
	}
	return string(data)
}
