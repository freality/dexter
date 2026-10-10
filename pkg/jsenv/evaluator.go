package jsenv

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/dop251/goja"
)

// Evaluator runs a bundled script and returns the JSON IR produced by
// calling __dex_synth().
type Evaluator interface {
	Evaluate(ctx context.Context, script string, env map[string]string) ([]byte, error)
}

const defaultTimeout = 10 * time.Second

// GojaEvaluator runs scripts in a goja ECMAScript VM.
type GojaEvaluator struct {
	timeout time.Duration
}

// NewGojaEvaluator returns an Evaluator with a 10s run budget.
func NewGojaEvaluator() *GojaEvaluator {
	return NewGojaEvaluatorWithTimeout(defaultTimeout)
}

// NewGojaEvaluatorWithTimeout returns an Evaluator with a custom run budget.
func NewGojaEvaluatorWithTimeout(d time.Duration) *GojaEvaluator {
	return &GojaEvaluator{timeout: d}
}

// Evaluate runs script with env exposed as globalThis.__dex_env and returns
// the JSON string produced by calling globalThis.__dex_synth().
func (g *GojaEvaluator) Evaluate(ctx context.Context, script string, env map[string]string) ([]byte, error) {
	vm := goja.New()
	// vm.Set on a plain map[string]string never fails; error is unreachable.
	_ = vm.Set("__dex_env", env)

	timer := time.AfterFunc(g.timeout, func() { vm.Interrupt("timeout") })
	defer timer.Stop()
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			vm.Interrupt(ctx.Err())
		case <-done:
		}
	}()
	defer vm.ClearInterrupt()

	if _, err := vm.RunString(script); err != nil {
		return nil, fmt.Errorf("evaluate config: %w", jsError(err))
	}

	synth, ok := goja.AssertFunction(vm.Get("__dex_synth"))
	if !ok {
		return nil, errors.New("evaluate config: __dex_synth is not defined (is @dex/iac imported?)")
	}

	result, err := synth(goja.Undefined())
	if err != nil {
		return nil, fmt.Errorf("synth: %w", jsError(err))
	}

	s, ok := result.Export().(string)
	if !ok {
		return nil, fmt.Errorf("synth: expected a JSON string, got %T", result.Export())
	}
	return []byte(s), nil
}

// jsError unwraps a thrown DexError (SPEC §5.1) into "[CODE] message" so the
// SDK's stable error codes surface in CLI output, wrapping the original
// *goja.Exception with %w so its JS stack stays reachable via errors.Unwrap;
// any other error passes through unchanged.
func jsError(err error) error {
	var ex *goja.Exception
	if !errors.As(err, &ex) {
		return err
	}
	obj, ok := ex.Value().(*goja.Object)
	if !ok {
		return err
	}
	name := obj.Get("name")
	code := obj.Get("code")
	if name == nil || code == nil || name.String() != "DexError" || code.String() == "" {
		return err
	}
	message := ""
	if msg := obj.Get("message"); msg != nil {
		message = msg.String()
	}
	return fmt.Errorf("[%s] %s: %w", code.String(), message, err)
}
