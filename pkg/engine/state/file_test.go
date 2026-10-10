package state

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fixedNow(t time.Time) func() time.Time {
	return func() time.Time { return t }
}

func TestStateNamesSorted(t *testing.T) {
	st := New()
	st.Resources["zeta"] = Resource{Type: "t", Plugin: "p"}
	st.Resources["alpha"] = Resource{Type: "t", Plugin: "p"}
	got := st.Names()
	want := []string{"alpha", "zeta"}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("Names() = %v, want %v", got, want)
	}
}

func TestFileStoreLoadMissing(t *testing.T) {
	dir := t.TempDir()
	fs := NewFileStore(dir, fixedNow(time.Now()))

	st, err := fs.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if st.Resources == nil || len(st.Resources) != 0 {
		t.Fatalf("Load() = %+v, want fresh empty state", st)
	}
}

func TestFileStoreSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	clock := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	fs := NewFileStore(dir, fixedNow(clock))

	st := New()
	st.Project = "homelab"
	st.Resources["a"] = Resource{Type: "container", Plugin: "docker"}

	if err := fs.Save(st); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if st.Serial != 1 {
		t.Fatalf("Serial after save = %d, want 1", st.Serial)
	}
	if !st.UpdatedAt.Equal(clock) {
		t.Fatalf("UpdatedAt = %v, want %v", st.UpdatedAt, clock)
	}

	loaded, err := fs.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.Project != "homelab" || loaded.Serial != 1 {
		t.Fatalf("Load() = %+v", loaded)
	}
	if loaded.Resources["a"].Type != "container" {
		t.Fatalf("loaded resource = %+v", loaded.Resources["a"])
	}

	if err := fs.Save(st); err != nil {
		t.Fatalf("second Save() error = %v", err)
	}
	if st.Serial != 2 {
		t.Fatalf("Serial after second save = %d, want 2", st.Serial)
	}
}

func TestFileStoreLoadMalformedJSON(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".dex"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".dex", "state.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	fs := NewFileStore(dir, fixedNow(time.Now()))

	_, err := fs.Load()
	if err == nil || !strings.Contains(err.Error(), "parse state") {
		t.Fatalf("Load() error = %v, want parse error", err)
	}
}

func TestFileStoreLoadNilResourcesNormalized(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".dex"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".dex", "state.json"), []byte(`{"project":"p"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	fs := NewFileStore(dir, fixedNow(time.Now()))

	st, err := fs.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if st.Resources == nil {
		t.Fatal("Resources not normalized to empty map")
	}
}

func TestFileStoreLoadUnreadable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can read anything; skipping permission test")
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".dex"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, ".dex", "state.json")
	if err := os.WriteFile(path, []byte("{}"), 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(path, 0o644) })
	fs := NewFileStore(dir, fixedNow(time.Now()))

	_, err := fs.Load()
	if err == nil || !strings.Contains(err.Error(), "read state") {
		t.Fatalf("Load() error = %v, want read error", err)
	}
}

func TestFileStoreSaveMkdirAllFailure(t *testing.T) {
	dir := t.TempDir()
	// .dex exists as a regular file, so MkdirAll cannot create it as a dir.
	if err := os.WriteFile(filepath.Join(dir, ".dex"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	fs := NewFileStore(dir, fixedNow(time.Now()))

	err := fs.Save(New())
	if err == nil || !strings.Contains(err.Error(), "create state dir") {
		t.Fatalf("Save() error = %v, want mkdir error", err)
	}
}

func TestFileStoreSaveMarshalFailure(t *testing.T) {
	dir := t.TempDir()
	fs := NewFileStore(dir, fixedNow(time.Now()))

	st := New()
	st.Resources["a"] = Resource{
		Type:   "container",
		Plugin: "docker",
		Properties: map[string]any{
			"bad": func() {}, // funcs are not JSON-marshalable
		},
	}

	err := fs.Save(st)
	if err == nil || !strings.Contains(err.Error(), "marshal state") {
		t.Fatalf("Save() error = %v, want marshal error", err)
	}
}

func TestFileStoreSaveWriteFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions; skipping")
	}
	dir := t.TempDir()
	dexDir := filepath.Join(dir, ".dex")
	if err := os.MkdirAll(dexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dexDir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dexDir, 0o755) })
	fs := NewFileStore(dir, fixedNow(time.Now()))

	err := fs.Save(New())
	if err == nil || !strings.Contains(err.Error(), "write state") {
		t.Fatalf("Save() error = %v, want write error", err)
	}
}

func TestFileStoreSaveRenameFailure(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".dex"), 0o755); err != nil {
		t.Fatal(err)
	}
	// state.json exists as a non-empty directory, so renaming the tmp file
	// over it fails.
	statePath := filepath.Join(dir, ".dex", "state.json")
	if err := os.MkdirAll(statePath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(statePath, "occupied"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	fs := NewFileStore(dir, fixedNow(time.Now()))

	err := fs.Save(New())
	if err == nil || !strings.Contains(err.Error(), "rename state") {
		t.Fatalf("Save() error = %v, want rename error", err)
	}
}
