package jsenv

import (
	"context"
	"errors"
	"testing"
)

type fakeBundler struct {
	script string
	err    error
}

func (f *fakeBundler) Bundle(entrypoint string) (string, error) {
	return f.script, f.err
}

type fakeEvaluator struct {
	data []byte
	err  error
}

func (f *fakeEvaluator) Evaluate(ctx context.Context, script string, env map[string]string) ([]byte, error) {
	return f.data, f.err
}

func TestSynthBundleError(t *testing.T) {
	boom := errors.New("bundle boom")
	s := NewSynthesizer(&fakeBundler{err: boom}, &fakeEvaluator{})

	_, err := s.Synth(context.Background(), "entry.ts", nil)
	if !errors.Is(err, boom) {
		t.Fatalf("Synth() error = %v, want %v", err, boom)
	}
}

func TestSynthEvaluateError(t *testing.T) {
	boom := errors.New("evaluate boom")
	s := NewSynthesizer(&fakeBundler{script: "ok"}, &fakeEvaluator{err: boom})

	_, err := s.Synth(context.Background(), "entry.ts", nil)
	if !errors.Is(err, boom) {
		t.Fatalf("Synth() error = %v, want %v", err, boom)
	}
}

func TestSynthSuccess(t *testing.T) {
	data := []byte(`{"project":"p","resources":{}}`)
	s := NewSynthesizer(&fakeBundler{script: "ok"}, &fakeEvaluator{data: data})

	got, err := s.Synth(context.Background(), "entry.ts", map[string]string{"X": "1"})
	if err != nil {
		t.Fatalf("Synth() error = %v", err)
	}
	if string(got) != string(data) {
		t.Fatalf("Synth() = %s, want %s", got, data)
	}
}
