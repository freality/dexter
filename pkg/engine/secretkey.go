package engine

import (
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
)

// loadOrCreateKey loads the 32-byte secret key from
// <dir>/.dex/secret.key, creating it with 32 random bytes if absent.
func loadOrCreateKey(dir string) ([]byte, error) {
	path := filepath.Join(dir, ".dex", "secret.key")
	data, err := os.ReadFile(path)
	if err == nil {
		if len(data) != 32 {
			return nil, fmt.Errorf("secret key %s: must be 32 bytes, got %d", path, len(data))
		}
		return data, nil
	}
	if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read secret key %s: %w", path, err)
	}
	key := make([]byte, 32)
	if _, err := randReader(key); err != nil {
		return nil, fmt.Errorf("generate secret key: %w", err)
	}
	keyDir := filepath.Dir(path)
	if err := os.MkdirAll(keyDir, 0o755); err != nil {
		return nil, fmt.Errorf("create secret key dir %s: %w", keyDir, err)
	}
	if err := os.WriteFile(path, key, 0o600); err != nil {
		return nil, fmt.Errorf("write secret key %s: %w", path, err)
	}
	return key, nil
}

// randReader allows tests to inject a failure. crypto/rand.Read on every
// real OS always succeeds; the test override exists only for coverage.
var randReader = rand.Read
