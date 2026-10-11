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

// NewGojaEvaluatorWithTimeout returns an Evaluator with a custom run budget;
// a zero duration means no budget, so only ctx cancellation interrupts.
func NewGojaEvaluatorWithTimeout(d time.Duration) *GojaEvaluator {
	return &GojaEvaluator{timeout: d}
}

// Evaluate runs script with env exposed as globalThis.__dex_env and returns
// the JSON string produced by calling globalThis.__dex_synth().
func (g *GojaEvaluator) Evaluate(ctx context.Context, script string, env map[string]string) ([]byte, error) {
	vm := goja.New()
	// vm.Set on a plain map[string]string never fails; error is unreachable.
	_ = vm.Set("__dex_env", env)

	stop := g.interruptOn(ctx, vm)
	defer stop()

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

// interruptOn arranges for the VM to be interrupted when the run budget
// elapses (a zero timeout means no budget) or when ctx is cancelled, and
// returns a stop func that clears any pending interrupt and releases both
// watchers. Call it exactly once, via defer.
func (g *GojaEvaluator) interruptOn(ctx context.Context, vm *goja.Runtime) func() {
	var timer *time.Timer
	if g.timeout > 0 {
		timer = time.AfterFunc(g.timeout, func() { vm.Interrupt("timeout") })
	}
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			vm.Interrupt(ctx.Err())
		case <-done:
		}
	}()
	return func() {
		vm.ClearInterrupt()
		close(done)
		if timer != nil {
			timer.Stop()
		}
	}
}

// ExecFunc runs argv as a host process with stdio attached and returns its
// exit code. It must return an error only when the process could not be
// started; a process that ran and exited non-zero is a code, not an error.
type ExecFunc func(ctx context.Context, argv []string) (int, error)

// CommandInfo describes one registered command for `dex run` listing.
type CommandInfo struct {
	Name        string
	Description string
}

// commandEntry is one entry of the SDK's __dex_commands registry.
type commandEntry struct {
	name        string
	description string
	run         goja.Callable
}

// registeredCommands evaluates the __dex_commands registry entries. It is
// shared by ListCommands and RunCommand; entries are validated eagerly, so a
// malformed spec fails regardless of which command was requested.
func registeredCommands(vm *goja.Runtime) ([]commandEntry, error) {
	entriesFn, ok := goja.AssertFunction(vm.Get("__dex_commands"))
	if !ok {
		return nil, errors.New("__dex_commands is not defined (is @dex/iac imported?)")
	}
	result, err := entriesFn(goja.Undefined())
	if err != nil {
		return nil, fmt.Errorf("list commands: %w", jsError(err))
	}
	arr, ok := result.(*goja.Object)
	if !ok {
		return nil, errors.New("__dex_commands did not return an array of command entries")
	}
	// Keys() yields the array's element indexes (length is non-enumerable),
	// so holes can't occur. Get returns nil for a missing property, which is
	// why name/description are read defensively.
	out := make([]commandEntry, 0, len(arr.Keys()))
	for _, key := range arr.Keys() {
		obj := arr.Get(key).ToObject(vm)
		name := ""
		if n := obj.Get("name"); n != nil {
			name = n.String()
		}
		runFn, ok := goja.AssertFunction(obj.Get("run"))
		if !ok {
			return nil, fmt.Errorf("command %q: run is not a function", name)
		}
		description := ""
		if d := obj.Get("description"); d != nil {
			description = d.String()
		}
		out = append(out, commandEntry{name: name, description: description, run: runFn})
	}
	return out, nil
}

// ListCommands evaluates script (env exposed as __dex_env) and returns the
// commands registered via the SDK's defineCommand, in registry order.
func (g *GojaEvaluator) ListCommands(ctx context.Context, script string, env map[string]string) ([]CommandInfo, error) {
	vm := goja.New()
	_ = vm.Set("__dex_env", env)
	stop := g.interruptOn(ctx, vm)
	defer stop()

	if _, err := vm.RunString(script); err != nil {
		return nil, fmt.Errorf("evaluate config: %w", jsError(err))
	}
	entries, err := registeredCommands(vm)
	if err != nil {
		return nil, err
	}
	infos := make([]CommandInfo, 0, len(entries))
	for _, e := range entries {
		infos = append(infos, CommandInfo{Name: e.name, Description: e.description})
	}
	return infos, nil
}

// RunCommand evaluates script and calls the run() of the command named name
// with a ctx exposing args, env(key), resources (state exactly as
// persisted, secrets still hashed), secret(key) (plaintext from the same
// env map), and exec(argv) (a host process, via execFn), returning the
// command's numeric result as an exit code (0 when run returns undefined).
func (g *GojaEvaluator) RunCommand(ctx context.Context, script string, env map[string]string, name string, args []string, resources map[string]any, execFn ExecFunc) (int, error) {
	vm := goja.New()
	_ = vm.Set("__dex_env", env)
	stop := g.interruptOn(ctx, vm)
	defer stop()

	if _, err := vm.RunString(script); err != nil {
		return 0, fmt.Errorf("evaluate config: %w", jsError(err))
	}
	entries, err := registeredCommands(vm)
	if err != nil {
		return 0, err
	}
	for _, entry := range entries {
		if entry.name != name {
			continue
		}
		result, err := entry.run(goja.Undefined(), commandContext(vm, ctx, args, env, resources, execFn))
		if err != nil {
			return 0, fmt.Errorf("command %q: %w", name, jsError(err))
		}
		code, err := exitCode(result)
		if err != nil {
			return 0, fmt.Errorf("command %q: %w", name, err)
		}
		return code, nil
	}
	return 0, fmt.Errorf("unknown command %q (run `dex run` to list commands)", name)
}

// commandContext builds the JS ctx object handed to a command's run().
func commandContext(vm *goja.Runtime, ctx context.Context, args []string, env map[string]string, resources map[string]any, execFn ExecFunc) *goja.Object {
	obj := vm.NewObject()
	// Set on a fresh object never fails; errors are unreachable.
	_ = obj.Set("args", args)
	_ = obj.Set("resources", vm.ToValue(resources))
	_ = obj.Set("env", func(call goja.FunctionCall) goja.Value {
		value, ok := env[call.Argument(0).String()]
		if !ok {
			return goja.Undefined()
		}
		return vm.ToValue(value)
	})
	_ = obj.Set("secret", func(call goja.FunctionCall) goja.Value {
		key := call.Argument(0).String()
		value, ok := env[key]
		if !ok {
			panic(vm.NewGoError(fmt.Errorf("secret %q is not set (.env or environment)", key)))
		}
		return vm.ToValue(value)
	})
	_ = obj.Set("exec", func(call goja.FunctionCall) goja.Value {
		var argv []string
		if err := vm.ExportTo(call.Argument(0), &argv); err != nil {
			panic(vm.NewGoError(fmt.Errorf("exec: expected an array of strings: %v", err)))
		}
		if len(argv) == 0 {
			panic(vm.NewGoError(errors.New("exec: expected at least one argument")))
		}
		code, err := execFn(ctx, argv)
		if err != nil {
			panic(vm.NewGoError(fmt.Errorf("exec %q: %v", argv[0], err)))
		}
		return vm.ToValue(code)
	})
	return obj
}

// exitCode converts a run() result to a process exit code: undefined or
// null means 0, a number is truncated to an integer, anything else is an
// error.
func exitCode(result goja.Value) (int, error) {
	if result == nil || goja.IsUndefined(result) || goja.IsNull(result) {
		return 0, nil
	}
	switch v := result.Export().(type) {
	case int64:
		return int(v), nil
	case float64:
		return int(v), nil
	default:
		return 0, fmt.Errorf("command run() returned %s, want a number or undefined", result.String())
	}
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
