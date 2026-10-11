package jsenv

import (
	"strings"
	"testing"
)

func TestEsbuildBundlerOK(t *testing.T) {
	b := NewEsbuildBundler()
	script, err := b.Bundle("testdata", []string{"ok.ts"})
	if err != nil {
		t.Fatalf("Bundle() error = %v", err)
	}
	if !strings.Contains(script, "__dex_synth") {
		t.Fatalf("bundled script missing __dex_synth: %s", script)
	}
	if !strings.Contains(script, "whoami") {
		t.Fatalf("bundled script missing user code: %s", script)
	}
}

func TestEsbuildBundlerSyntaxError(t *testing.T) {
	b := NewEsbuildBundler()
	_, err := b.Bundle("testdata", []string{"syntax-error.ts"})
	if err == nil || !strings.Contains(err.Error(), "bundle:") {
		t.Fatalf("Bundle() error = %v, want bundle error", err)
	}
	if err == nil || !strings.Contains(err.Error(), "syntax-error.ts") {
		t.Fatalf("Bundle() error = %v, want the failing file named", err)
	}
}

func TestEsbuildBundlerMissingFile(t *testing.T) {
	b := NewEsbuildBundler()
	_, err := b.Bundle("testdata", []string{"does-not-exist.ts"})
	if err == nil || !strings.Contains(err.Error(), "does-not-exist.ts") {
		t.Fatalf("Bundle() error = %v, want missing-file error", err)
	}
}

func TestEsbuildBundlerUnknownImport(t *testing.T) {
	b := NewEsbuildBundler()
	_, err := b.Bundle("testdata", []string{"unknown-import.ts"})
	if err == nil || !strings.Contains(err.Error(), "unknown-import.ts") {
		t.Fatalf("Bundle() error = %v, want unknown-import error", err)
	}
}
