package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// FileStore persists state to <projectRoot>/.dex/state.json.
type FileStore struct {
	path string
	now  func() time.Time
}

// NewFileStore returns a Store rooted at projectRoot. now is injected so
// tests can assert UpdatedAt exactly.
func NewFileStore(projectRoot string, now func() time.Time) *FileStore {
	return &FileStore{
		path: filepath.Join(projectRoot, ".dex", "state.json"),
		now:  now,
	}
}

// Load reads state.json, or returns a fresh State if it does not exist yet.
func (f *FileStore) Load() (*State, error) {
	data, err := os.ReadFile(f.path)
	if errors.Is(err, fs.ErrNotExist) {
		return New(), nil
	}
	if err != nil {
		return nil, fmt.Errorf("read state %s: %w", f.path, err)
	}
	var st State
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, fmt.Errorf("parse state %s: %w", f.path, err)
	}
	if st.Resources == nil {
		st.Resources = map[string]Resource{}
	}
	return &st, nil
}

// Save bumps Serial, stamps UpdatedAt, and atomically rewrites state.json.
func (f *FileStore) Save(st *State) error {
	dir := filepath.Dir(f.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create state dir %s: %w", dir, err)
	}
	st.Serial++
	st.UpdatedAt = f.now().UTC()

	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state %s: %w", f.path, err)
	}
	data = append(data, '\n')

	tmp := f.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write state %s: %w", f.path, err)
	}
	if err := os.Rename(tmp, f.path); err != nil {
		return fmt.Errorf("rename state %s: %w", f.path, err)
	}
	return nil
}
