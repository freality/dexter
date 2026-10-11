package jsenv

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type fakeBundler struct {
	script string
	err    error
}

func (f *fakeBundler) Bundle(dir string, files []string) (string, error) {
	return f.script, f.err
}

type fakeEvaluator struct {
	data []byte
	err  error
}

func (f *fakeEvaluator) Evaluate(ctx context.Context, script string, env map[string]string) ([]byte, error) {
	return f.data, f.err
}

// synthDir returns a TempDir containing an (empty) dex.config.ts so the real
// Discover inside Synth succeeds; bundle/evaluate stay faked.
func synthDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "dex.config.ts"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestSynthBundleError(t *testing.T) {
	boom := errors.New("bundle boom")
	s := NewSynthesizer(&fakeBundler{err: boom}, &fakeEvaluator{})

	_, err := s.Synth(context.Background(), synthDir(t), nil)
	if !errors.Is(err, boom) {
		t.Fatalf("Synth() error = %v, want %v", err, boom)
	}
}

func TestSynthEvaluateError(t *testing.T) {
	boom := errors.New("evaluate boom")
	s := NewSynthesizer(&fakeBundler{script: "ok"}, &fakeEvaluator{err: boom})

	_, err := s.Synth(context.Background(), synthDir(t), nil)
	if !errors.Is(err, boom) {
		t.Fatalf("Synth() error = %v, want %v", err, boom)
	}
}

func TestSynthSuccess(t *testing.T) {
	data := []byte(`{"project":"p","resources":{}}`)
	s := NewSynthesizer(&fakeBundler{script: "ok"}, &fakeEvaluator{data: data})

	got, err := s.Synth(context.Background(), synthDir(t), map[string]string{"X": "1"})
	if err != nil {
		t.Fatalf("Synth() error = %v", err)
	}
	if string(got) != string(data) {
		t.Fatalf("Synth() = %s, want %s", got, data)
	}
}

func TestSynthMissingConfig(t *testing.T) {
	s := NewSynthesizer(&fakeBundler{}, &fakeEvaluator{})
	dir := t.TempDir()
	_, err := s.Synth(context.Background(), dir, nil)
	if err == nil || err.Error() != "no dex.config.ts in "+dir+" (run `dex init`)" {
		t.Fatalf("Synth() error = %v, want missing-config error", err)
	}
}
