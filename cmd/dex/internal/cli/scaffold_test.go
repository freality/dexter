package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunInitHappyPath(t *testing.T) {
	dir := t.TempDir()
	var out bytes.Buffer

	if err := runInit(&out, dir); err != nil {
		t.Fatalf("runInit() error = %v", err)
	}

	wantLines := []string{
		"created dex.config.ts",
		"created .env",
		"created dex/services/.gitkeep",
		"add .dex/ to your .gitignore",
	}
	gotLines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(gotLines) != len(wantLines) {
		t.Fatalf("out lines = %v, want %v", gotLines, wantLines)
	}
	for i, want := range wantLines {
		if gotLines[i] != want {
			t.Fatalf("out line %d = %q, want %q", i, gotLines[i], want)
		}
	}

	config, err := os.ReadFile(filepath.Join(dir, "dex.config.ts"))
	if err != nil || string(config) != configTemplate {
		t.Fatalf("dex.config.ts = %q, err = %v", config, err)
	}
	env, err := os.ReadFile(filepath.Join(dir, ".env"))
	if err != nil || string(env) != envTemplate {
		t.Fatalf(".env = %q, err = %v", env, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "dex", "services", ".gitkeep")); err != nil {
		t.Fatalf("gitkeep missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".gitignore")); !os.IsNotExist(err) {
		t.Fatalf("runInit() wrote .gitignore, want it untouched (not a dex init exception per AGENTS.md)")
	}
}

func TestRunInitRefusesExisting(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "dex.config.ts"), []byte("existing"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := runInit(&bytes.Buffer{}, dir)
	if err == nil || err.Error() != "dex.config.ts already exists; refusing to overwrite" {
		t.Fatalf("runInit() error = %v", err)
	}
}

func TestRunInitStatPermissionError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions; skipping")
	}
	parent := t.TempDir()
	dir := filepath.Join(parent, "proj")
	if err := os.Mkdir(dir, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o755) })

	err := runInit(&bytes.Buffer{}, dir)
	if err == nil || !strings.Contains(err.Error(), "check") || !strings.Contains(err.Error(), "dex.config.ts") {
		t.Fatalf("runInit() error = %v, want wrapped stat error", err)
	}
}

func TestRunInitWriteConfigFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions; skipping")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o755) })

	err := runInit(&bytes.Buffer{}, dir)
	if err == nil || !strings.Contains(err.Error(), "write") || !strings.Contains(err.Error(), "dex.config.ts") {
		t.Fatalf("runInit() error = %v, want wrapped write error", err)
	}
}

func TestRunInitWriteEnvFailure(t *testing.T) {
	dir := t.TempDir()
	// .env exists as a directory, so writing the file fails.
	if err := os.Mkdir(filepath.Join(dir, ".env"), 0o755); err != nil {
		t.Fatal(err)
	}
	err := runInit(&bytes.Buffer{}, dir)
	if err == nil || !strings.Contains(err.Error(), "write") || !strings.Contains(err.Error(), ".env") {
		t.Fatalf("runInit() error = %v, want wrapped write error", err)
	}
}

func TestRunInitGitkeepMkdirFailure(t *testing.T) {
	dir := t.TempDir()
	// "dex" exists as a regular file, so MkdirAll("dex/services") fails.
	if err := os.WriteFile(filepath.Join(dir, "dex"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := runInit(&bytes.Buffer{}, dir)
	if err == nil || !strings.Contains(err.Error(), "create") {
		t.Fatalf("runInit() error = %v, want wrapped mkdir error", err)
	}
}

func TestRunInitGitkeepWriteFailure(t *testing.T) {
	dir := t.TempDir()
	// .gitkeep exists as a directory, so writing the file fails.
	if err := os.MkdirAll(filepath.Join(dir, "dex", "services", ".gitkeep"), 0o755); err != nil {
		t.Fatal(err)
	}
	err := runInit(&bytes.Buffer{}, dir)
	if err == nil || !strings.Contains(err.Error(), "write") || !strings.Contains(err.Error(), ".gitkeep") {
		t.Fatalf("runInit() error = %v, want wrapped write error", err)
	}
}
