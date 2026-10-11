package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestMainUnknownCommand(t *testing.T) {
	var out, errOut bytes.Buffer
	code := Main([]string{"bogus"}, strings.NewReader(""), &out, &errOut)
	if code != 1 {
		t.Fatalf("Main() = %d, want 1", code)
	}
	if !strings.HasPrefix(errOut.String(), "Error: ") {
		t.Fatalf("errOut = %q, want Error: prefix", errOut.String())
	}
}

func TestMainNoArgsShowsHelp(t *testing.T) {
	var out, errOut bytes.Buffer
	code := Main(nil, strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("Main() = %d, want 0 (help)", code)
	}
	if errOut.String() != "" {
		t.Fatalf("errOut = %q, want empty", errOut.String())
	}
}

func TestMainInitSuccess(t *testing.T) {
	dir := t.TempDir()
	var out, errOut bytes.Buffer
	code := Main([]string{"init", "--dir", dir}, strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("Main() = %d, errOut = %q", code, errOut.String())
	}
	if !strings.Contains(out.String(), "created dex.config.ts") {
		t.Fatalf("out = %q", out.String())
	}
}

func TestMainPlanMissingConfig(t *testing.T) {
	dir := t.TempDir()
	var out, errOut bytes.Buffer
	code := Main([]string{"plan", "--dir", dir}, strings.NewReader(""), &out, &errOut)
	if code != 1 {
		t.Fatalf("Main() = %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "run `dex init`") {
		t.Fatalf("errOut = %q", errOut.String())
	}
}

func TestEnvMap(t *testing.T) {
	t.Setenv("DEX_TEST_ENV_VAR", "value")
	m := envMap()
	if m["DEX_TEST_ENV_VAR"] != "value" {
		t.Fatalf("envMap()[DEX_TEST_ENV_VAR] = %q, want %q", m["DEX_TEST_ENV_VAR"], "value")
	}
}

// TestMainInitThenPlanEndToEnd drives the real production engine factory
// (esbuild + goja + the embedded SDK), not a fake, so newProductionEngine
// is exercised end to end.
func TestMainInitThenPlanEndToEnd(t *testing.T) {
	dir := t.TempDir()
	var out, errOut bytes.Buffer
	if code := Main([]string{"init", "--dir", dir}, strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatalf("init Main() = %d, errOut = %q", code, errOut.String())
	}

	out.Reset()
	errOut.Reset()
	code := Main([]string{"plan", "--dir", dir}, strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("plan Main() = %d, errOut = %q", code, errOut.String())
	}
	if !strings.Contains(out.String(), "+ whoami") {
		t.Fatalf("plan out = %q, want + whoami", out.String())
	}
	if !strings.Contains(out.String(), "Plan: 1 to add, 0 to change, 0 to destroy.") {
		t.Fatalf("plan out = %q", out.String())
	}
}

// TestNewProductionEngineMalformedDotEnv covers the loadEnv error path in
// newProductionEngine: a .env with a malformed line (no '=') must fail.
func TestNewProductionEngineMalformedDotEnv(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(dir+"/.env", []byte("GOOD=1\nBADLINE\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := newProductionEngine(dir); err == nil {
		t.Fatal("newProductionEngine() = nil error, want error from malformed .env")
	}
}
