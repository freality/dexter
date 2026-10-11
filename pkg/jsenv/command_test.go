package jsenv

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// resourcesWithSecret is state exactly as persisted: the secret property is
// {token, sha256}, never plaintext (D13).
func resourcesWithSecret() map[string]any {
	return map[string]any{
		"db": map[string]any{
			"type": "container",
			"properties": map[string]any{
				"pass": map[string]any{"token": "tok", "sha256": "abc123"},
			},
			"outputs": map[string]any{"ip": "10.0.0.5"},
		},
	}
}

// TestRunCommandCtx is the D13 acceptance test: ctx.resources never carries a
// resolved secret value, secret() reads the plaintext deliberately, env()
// reads the same map, args pass through, and exec runs a host process and
// returns its exit code.
func TestRunCommandCtx(t *testing.T) {
	script := `globalThis.__dex_commands = () => [{
		name: "peek", description: "Look",
		run: (ctx) => {
			if (ctx.args.length !== 2 || ctx.args[1] !== "b") return 10;
			if (ctx.env("A") !== "1") return 11;
			if (ctx.env("NOPE") !== undefined) return 12;
			if (ctx.resources.db.properties.pass.token !== "tok") return 13;
			if (JSON.stringify(ctx.resources).includes("hunter2")) return 14;
			if (ctx.secret("DB_PASSWORD") !== "hunter2") return 15;
			return ctx.exec(["echo", "hi", "x"]);
		},
	}];`

	var gotArgs []string
	code, err := NewGojaEvaluatorWithTimeout(0).RunCommand(
		context.Background(), script,
		map[string]string{"A": "1", "DB_PASSWORD": "hunter2"},
		"peek", []string{"a", "b"}, resourcesWithSecret(),
		func(ctx context.Context, argv []string) (int, error) {
			gotArgs = argv
			return 3, nil
		},
	)
	if err != nil {
		t.Fatalf("RunCommand() error = %v", err)
	}
	if code != 3 {
		t.Fatalf("RunCommand() code = %d, want 3 (the exec result)", code)
	}
	want := []string{"echo", "hi", "x"}
	if len(gotArgs) != len(want) || gotArgs[0] != want[0] || gotArgs[1] != want[1] || gotArgs[2] != want[2] {
		t.Fatalf("exec argv = %v, want %v", gotArgs, want)
	}
}

func TestRunCommandListAndInvoke(t *testing.T) {
	script := `globalThis.__dex_commands = () => [
		{ name: "b", description: "Bee", run: () => 2 },
		{ name: "a", description: "", run: () => 1 },
		{ name: "none", run: () => {} },
		{ name: "frac", run: () => 2.5 },
		{ name: "bad", run: () => "x" },
	];`

	infos, err := NewGojaEvaluatorWithTimeout(0).ListCommands(context.Background(), script, nil)
	if err != nil {
		t.Fatalf("ListCommands() error = %v", err)
	}
	if len(infos) != 5 || infos[0].Name != "b" || infos[0].Description != "Bee" || infos[1].Description != "" {
		t.Fatalf("ListCommands() = %+v", infos)
	}

	// List and run share one evaluation: a second script run by RunCommand
	// with the same registrations.
	run := func(name string, wantCode int, wantErr string) {
		t.Helper()
		e := NewGojaEvaluatorWithTimeout(0)
		code, err := e.RunCommand(context.Background(), script, nil, name, nil, map[string]any{}, nil)
		if wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), wantErr) {
				t.Fatalf("RunCommand(%s) error = %v, want containing %q", name, err, wantErr)
			}
			return
		}
		if err != nil {
			t.Fatalf("RunCommand(%s) error = %v", name, err)
		}
		if code != wantCode {
			t.Fatalf("RunCommand(%s) code = %d, want %d", name, code, wantCode)
		}
	}
	run("b", 2, "")
	run("a", 1, "")
	run("none", 0, "") // undefined result means 0
	run("frac", 2, "") // truncated to an integer
	run("bad", 0, "want a number")
	run("nope", 0, `unknown command "nope"`)
}

func TestRunCommandErrors(t *testing.T) {
	tests := []struct {
		name    string
		script  string
		wantErr string
	}{
		{
			name:    "registry missing",
			script:  "const x = 1;",
			wantErr: "__dex_commands is not defined",
		},
		{
			name:    "entries throws",
			script:  `globalThis.__dex_commands = () => { throw new Error("boom"); };`,
			wantErr: "boom",
		},
		{
			name:    "entries not an array",
			script:  `globalThis.__dex_commands = () => 42;`,
			wantErr: "did not return an array",
		},
		{
			name:    "run not a function",
			script:  `globalThis.__dex_commands = () => [{ run: 42 }];`,
			wantErr: `command "": run is not a function`,
		},
		{
			name:    "run throws plain error",
			script:  `globalThis.__dex_commands = () => [{ name: "x", description: "", run: () => { throw new Error("kaboom"); } }];`,
			wantErr: `kaboom`,
		},
		{
			name:    "run throws DexError",
			script:  `globalThis.__dex_commands = () => [{ name: "x", description: "", run: () => { throw { name: "DexError", code: "X", message: "msg" }; } }];`,
			wantErr: `command "x": [X] msg`,
		},
		{
			name:    "script syntax error",
			script:  "this is not valid javascript {{{",
			wantErr: "evaluate config",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewGojaEvaluatorWithTimeout(0).RunCommand(
				context.Background(), tt.script, nil, "x", nil, map[string]any{}, nil)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("RunCommand() error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestRunCommandExecErrors(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		execErr error
		wantErr string
	}{
		{
			name:    "non-array argv",
			body:    `ctx.exec("echo");`,
			wantErr: "expected an array of strings",
		},
		{
			name:    "empty argv",
			body:    `ctx.exec([]);`,
			wantErr: "expected at least one argument",
		},
		{
			name:    "process start failure",
			body:    `ctx.exec(["missing-bin"]);`,
			execErr: context.DeadlineExceeded,
			wantErr: `exec "missing-bin"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			script := `globalThis.__dex_commands = () => [{ name: "x", description: "", run: (ctx) => { ` + tt.body + ` } }];`
			_, err := NewGojaEvaluatorWithTimeout(0).RunCommand(
				context.Background(), script, nil, "x", nil, map[string]any{},
				func(ctx context.Context, argv []string) (int, error) { return 0, tt.execErr })
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("RunCommand() error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestRunCommandSecretMissing(t *testing.T) {
	script := `globalThis.__dex_commands = () => [{ name: "x", description: "", run: (ctx) => ctx.secret("NOPE") }];`
	_, err := NewGojaEvaluatorWithTimeout(0).RunCommand(
		context.Background(), script, map[string]string{}, "x", nil, map[string]any{}, nil)
	if err == nil || !strings.Contains(err.Error(), `secret "NOPE" is not set`) {
		t.Fatalf("RunCommand() error = %v, want missing-secret error", err)
	}
}

func TestRunCommandTimeout(t *testing.T) {
	e := NewGojaEvaluatorWithTimeout(50 * time.Millisecond)
	script := `globalThis.__dex_commands = () => [{ name: "x", description: "", run: () => { while (true) {} } }];`
	start := time.Now()
	_, err := e.RunCommand(context.Background(), script, nil, "x", nil, map[string]any{}, nil)
	if err == nil {
		t.Fatal("RunCommand() error = nil, want timeout error")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("RunCommand() took %v, want bounded by timeout", elapsed)
	}
}

// TestRunCommandNoBudgetCtxCancel proves a zero timeout means *no* budget:
// only ctx cancellation interrupts the script (D13).
func TestRunCommandNoBudgetCtxCancel(t *testing.T) {
	e := NewGojaEvaluatorWithTimeout(0)
	script := `globalThis.__dex_commands = () => [{ name: "x", description: "", run: () => { while (true) {} } }];`
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := e.RunCommand(ctx, script, nil, "x", nil, map[string]any{}, nil)
		done <- err
	}()
	time.Sleep(100 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("RunCommand() error = nil, want ctx cancellation")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("RunCommand() did not return after ctx cancellation")
	}
}

// TestRunCommandExecCancelledByCtx is the SIGINT path: exec is blocked in a
// host process, ctx cancels, the child is killed (execFn returns), and the
// pending VM interrupt surfaces as a command error.
func TestRunCommandExecCancelledByCtx(t *testing.T) {
	script := `globalThis.__dex_commands = () => [{ name: "x", description: "", run: (ctx) => ctx.exec(["sleep", "60"]) }];`
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := NewGojaEvaluatorWithTimeout(0).RunCommand(
			ctx, script, nil, "x", nil, map[string]any{},
			func(ctx context.Context, argv []string) (int, error) {
				// Wait for ctx cancellation, simulating a blocking exec
				// that gets killed. Return an error so the exec call panics
				// and the command always errors regardless of goroutine race.
				<-ctx.Done()
				// The command must not complete normally: a killed exec is a
				// thrown JS error, never a plain exit code.
				return 0, errors.New("killed by signal")
			})
		done <- err
	}()
	time.Sleep(100 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("RunCommand() error = nil, want either error or interruption after cancellation")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("RunCommand() did not return after cancellation")
	}
}

// TestGojaEvaluateNoBudgetWhenTimeoutZero locks in the timeout-0 guard on
// the synthesis path too: no timer, ctx cancellation still interrupts.
func TestGojaEvaluateNoBudgetWhenTimeoutZero(t *testing.T) {
	e := NewGojaEvaluatorWithTimeout(0)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := e.Evaluate(ctx, `while (true) {}`, nil)
		done <- err
	}()
	time.Sleep(100 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Evaluate() error = nil, want ctx cancellation")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Evaluate() did not return after ctx cancellation")
	}
}

func TestListCommandsErrors(t *testing.T) {
	if _, err := NewGojaEvaluatorWithTimeout(0).ListCommands(context.Background(), "const x = 1;", nil); err == nil || !strings.Contains(err.Error(), "__dex_commands is not defined") {
		t.Fatalf("ListCommands() error = %v, want missing registry error", err)
	}
	script := `globalThis.__dex_commands = () => { throw new Error("boom"); };`
	if _, err := NewGojaEvaluatorWithTimeout(0).ListCommands(context.Background(), script, nil); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("ListCommands() error = %v, want thrown error", err)
	}
	if _, err := NewGojaEvaluatorWithTimeout(0).ListCommands(context.Background(), "this is not valid javascript {{{", nil); err == nil || !strings.Contains(err.Error(), "evaluate config") {
		t.Fatalf("ListCommands() error = %v, want evaluate config error", err)
	}
}
