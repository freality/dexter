package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadEnvNormal(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".env"), []byte("FOO=bar\nBAZ=qux\n"), 0644)

	m, err := loadEnv(dir)
	if err != nil {
		t.Fatal(err)
	}
	if m["FOO"] != "bar" {
		t.Fatalf("FOO = %q, want %q", m["FOO"], "bar")
	}
	if m["BAZ"] != "qux" {
		t.Fatalf("BAZ = %q, want %q", m["BAZ"], "qux")
	}
	if len(m) != 2 {
		t.Fatalf("len(m) = %d, want 2", len(m))
	}
}

func TestLoadEnvCommentsAndBlanks(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".env"), []byte("# comment\n\nKEY=val\n# another\n\n"), 0644)

	m, err := loadEnv(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(m) != 1 {
		t.Fatalf("len(m) = %d, want 1", len(m))
	}
	if m["KEY"] != "val" {
		t.Fatalf("KEY = %q, want %q", m["KEY"], "val")
	}
}

func TestLoadEnvExportPrefix(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".env"), []byte("export FOO=bar\nexport BAZ=qux\n"), 0644)

	m, err := loadEnv(dir)
	if err != nil {
		t.Fatal(err)
	}
	if m["FOO"] != "bar" {
		t.Fatalf("FOO = %q, want %q", m["FOO"], "bar")
	}
	if m["BAZ"] != "qux" {
		t.Fatalf("BAZ = %q, want %q", m["BAZ"], "qux")
	}
}

func TestLoadEnvQuotedValues(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".env"), []byte("FOO=\"bar\"\nBAZ='qux'\n"), 0644)

	m, err := loadEnv(dir)
	if err != nil {
		t.Fatal(err)
	}
	if m["FOO"] != "bar" {
		t.Fatalf("FOO = %q, want %q", m["FOO"], "bar")
	}
	if m["BAZ"] != "qux" {
		t.Fatalf("BAZ = %q, want %q", m["BAZ"], "qux")
	}
}

func TestLoadEnvMissingFile(t *testing.T) {
	dir := t.TempDir()

	m, err := loadEnv(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(m) != 0 {
		t.Fatalf("len(m) = %d, want 0", len(m))
	}
}

func TestLoadEnvMalformedLine(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".env"), []byte("FOO=bar\nBADLINE\n"), 0644)

	_, err := loadEnv(dir)
	if err == nil {
		t.Fatal("expected error for malformed line")
	}
	if err.Error() != ".env:2: malformed line (missing '=')" {
		t.Fatalf("err = %q, want %q", err.Error(), ".env:2: malformed line (missing '=')")
	}
}

func TestLoadEnvEmptyFile(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".env"), []byte(""), 0644)

	m, err := loadEnv(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(m) != 0 {
		t.Fatalf("len(m) = %d, want 0", len(m))
	}
}

func TestLoadEnvMalformedLineOnFirstLine(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".env"), []byte("NOPE\n"), 0644)

	_, err := loadEnv(dir)
	if err == nil {
		t.Fatal("expected error for malformed line")
	}
	if err.Error() != ".env:1: malformed line (missing '=')" {
		t.Fatalf("err = %q, want %q", err.Error(), ".env:1: malformed line (missing '=')")
	}
}

func TestLoadEnvQuotedValueWithExport(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".env"), []byte("export FOO=\"bar\"\n"), 0644)

	m, err := loadEnv(dir)
	if err != nil {
		t.Fatal(err)
	}
	if m["FOO"] != "bar" {
		t.Fatalf("FOO = %q, want %q", m["FOO"], "bar")
	}
}

func TestLoadEnvReadError(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	os.WriteFile(envPath, []byte("FOO=bar\n"), 0644)
	os.Chmod(envPath, 0000) // remove all permissions

	_, err := loadEnv(dir)
	if err == nil {
		t.Fatal("expected error for unreadable .env file")
	}
}
