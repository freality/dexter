package jsenv

import (
	"strings"
	"testing"
)

func TestEsbuildBundlerOK(t *testing.T) {
	b := NewEsbuildBundler()
	script, err := b.Bundle("testdata/ok.ts")
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
	_, err := b.Bundle("testdata/syntax-error.ts")
	if err == nil || !strings.Contains(err.Error(), "bundle testdata/syntax-error.ts:") {
		t.Fatalf("Bundle() error = %v, want bundle error", err)
	}
}

func TestEsbuildBundlerMissingFile(t *testing.T) {
	b := NewEsbuildBundler()
	_, err := b.Bundle("testdata/does-not-exist.ts")
	if err == nil || !strings.Contains(err.Error(), "bundle testdata/does-not-exist.ts:") {
		t.Fatalf("Bundle() error = %v, want bundle error", err)
	}
}

func TestEsbuildBundlerUnknownImport(t *testing.T) {
	b := NewEsbuildBundler()
	_, err := b.Bundle("testdata/unknown-import.ts")
	if err == nil || !strings.Contains(err.Error(), "bundle testdata/unknown-import.ts:") {
		t.Fatalf("Bundle() error = %v, want bundle error", err)
	}
}
