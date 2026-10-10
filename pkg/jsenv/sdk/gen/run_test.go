package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFakeRepo(t *testing.T, indexJS string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.work"), []byte("go 1.27.2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if indexJS != "" {
		distDir := filepath.Join(root, "packages", "sdk", "dist")
		if err := os.MkdirAll(distDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(distDir, "index.js"), []byte(indexJS), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	sdkDir := filepath.Join(root, "pkg", "jsenv", "sdk")
	if err := os.MkdirAll(sdkDir, 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestRunSuccess(t *testing.T) {
	root := writeFakeRepo(t, "export const x = 1;\nglobalThis.__dex_synth = () => '{}';\n")
	t.Chdir(root)

	if err := Run(); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, "pkg", "jsenv", "sdk", "iac.js"))
	if err != nil {
		t.Fatalf("read generated bundle: %v", err)
	}
	if !strings.Contains(string(data), "__dex_synth") {
		t.Fatalf("generated bundle missing __dex_synth: %s", data)
	}
}

func TestRunMissingInput(t *testing.T) {
	root := writeFakeRepo(t, "")
	t.Chdir(root)

	err := Run()
	if err == nil || !strings.Contains(err.Error(), "generate SDK bundle") {
		t.Fatalf("Run() error = %v, want missing-input error", err)
	}
}

func TestRunMissingRepoRoot(t *testing.T) {
	t.Chdir(t.TempDir())

	err := Run()
	if err == nil || !strings.Contains(err.Error(), "generate SDK bundle") {
		t.Fatalf("Run() error = %v, want repo-root error", err)
	}
}

func TestRunBuildError(t *testing.T) {
	root := writeFakeRepo(t, "this is not valid javascript {{{")
	t.Chdir(root)

	err := Run()
	if err == nil || !strings.Contains(err.Error(), "generate SDK bundle") {
		t.Fatalf("Run() error = %v, want build error", err)
	}
}

func TestRunWriteFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions; skipping")
	}
	root := writeFakeRepo(t, "export const x = 1;\n")
	sdkDir := filepath.Join(root, "pkg", "jsenv", "sdk")
	if err := os.Chmod(sdkDir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(sdkDir, 0o755) })
	t.Chdir(root)

	err := Run()
	if err == nil || !strings.Contains(err.Error(), "generate SDK bundle") {
		t.Fatalf("Run() error = %v, want write error", err)
	}
}

func TestMainSuccessAndFailure(t *testing.T) {
	root := writeFakeRepo(t, "export const x = 1;\n")
	t.Chdir(root)
	if code := Main(); code != 0 {
		t.Fatalf("Main() = %d, want 0", code)
	}

	t.Chdir(t.TempDir())
	if code := Main(); code != 1 {
		t.Fatalf("Main() = %d, want 1", code)
	}
}
