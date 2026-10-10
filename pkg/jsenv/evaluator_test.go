package jsenv

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestGojaEvaluateSuccess(t *testing.T) {
	e := NewGojaEvaluator()
	script := `globalThis.__dex_synth = () => JSON.stringify({project:"p", resources:{}});`

	data, err := e.Evaluate(context.Background(), script, map[string]string{"FOO": "bar"})
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if string(data) != `{"project":"p","resources":{}}` {
		t.Fatalf("Evaluate() = %s", data)
	}
}

func TestGojaEvaluateEnvVisible(t *testing.T) {
	e := NewGojaEvaluator()
	script := `globalThis.__dex_synth = () => JSON.stringify({value: globalThis.__dex_env.FOO});`

	data, err := e.Evaluate(context.Background(), script, map[string]string{"FOO": "bar"})
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if string(data) != `{"value":"bar"}` {
		t.Fatalf("Evaluate() = %s, want __dex_env visible", data)
	}
}

func TestGojaEvaluateThrownDexError(t *testing.T) {
	e := NewGojaEvaluator()
	script := `
		throw Object.assign(new Error("no config"), { name: "DexError", code: "MISSING_CONFIG" });
	`
	_, err := e.Evaluate(context.Background(), script, nil)
	if err == nil || !strings.Contains(err.Error(), "[MISSING_CONFIG] no config") {
		t.Fatalf("Evaluate() error = %v, want DexError formatting", err)
	}
}

func TestGojaEvaluateThrownPlainError(t *testing.T) {
	e := NewGojaEvaluator()
	script := `throw new Error("plain failure");`

	_, err := e.Evaluate(context.Background(), script, nil)
	if err == nil || !strings.Contains(err.Error(), "plain failure") {
		t.Fatalf("Evaluate() error = %v, want plain error message", err)
	}
	if strings.Contains(err.Error(), "[") {
		t.Fatalf("Evaluate() error = %v, want no DexError-style code prefix", err)
	}
}

func TestGojaEvaluateThrownPrimitive(t *testing.T) {
	e := NewGojaEvaluator()
	_, err := e.Evaluate(context.Background(), `throw "boom";`, nil)
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("Evaluate() error = %v, want thrown primitive surfaced", err)
	}
}

func TestGojaEvaluateThrownDexErrorNoMessage(t *testing.T) {
	e := NewGojaEvaluator()
	script := `throw { name: "DexError", code: "X" };`
	_, err := e.Evaluate(context.Background(), script, nil)
	if err == nil || !strings.Contains(err.Error(), "[X] ") {
		t.Fatalf("Evaluate() error = %v, want [X] with empty message", err)
	}
}

func TestGojaEvaluateMissingSynth(t *testing.T) {
	e := NewGojaEvaluator()
	_, err := e.Evaluate(context.Background(), `const x = 1;`, nil)
	if err == nil || !strings.Contains(err.Error(), "__dex_synth is not defined") {
		t.Fatalf("Evaluate() error = %v, want missing __dex_synth error", err)
	}
}

func TestGojaEvaluateSynthNonString(t *testing.T) {
	e := NewGojaEvaluator()
	_, err := e.Evaluate(context.Background(), `globalThis.__dex_synth = () => 42;`, nil)
	if err == nil || !strings.Contains(err.Error(), "expected a JSON string") {
		t.Fatalf("Evaluate() error = %v, want non-string error", err)
	}
}

func TestGojaEvaluateSynthThrows(t *testing.T) {
	e := NewGojaEvaluator()
	script := `globalThis.__dex_synth = () => { throw new Error("synth failed"); };`
	_, err := e.Evaluate(context.Background(), script, nil)
	if err == nil || !strings.Contains(err.Error(), "synth failed") {
		t.Fatalf("Evaluate() error = %v, want synth error", err)
	}
}

func TestGojaEvaluateRunStringError(t *testing.T) {
	e := NewGojaEvaluator()
	_, err := e.Evaluate(context.Background(), `this is not valid javascript {{{`, nil)
	if err == nil || !strings.Contains(err.Error(), "evaluate config:") {
		t.Fatalf("Evaluate() error = %v, want evaluate config error", err)
	}
}

func TestGojaEvaluateTimeout(t *testing.T) {
	e := NewGojaEvaluatorWithTimeout(50 * time.Millisecond)
	start := time.Now()
	_, err := e.Evaluate(context.Background(), `while (true) {}`, nil)
	if err == nil {
		t.Fatal("Evaluate() error = nil, want timeout error")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("Evaluate() took %v, want bounded by timeout", elapsed)
	}
}

func TestGojaEvaluateContextCancelled(t *testing.T) {
	e := NewGojaEvaluator()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan error, 1)
	go func() {
		_, err := e.Evaluate(ctx, `while (true) {}`, nil)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Evaluate() error = nil, want context-cancellation error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Evaluate() did not return after context cancellation")
	}
}
