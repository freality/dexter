package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/freality/dexter/pkg/jsenv"
)

// fakeRunner lets run tests avoid booting esbuild/goja.
type fakeRunner struct {
	listF func() ([]jsenv.CommandInfo, error)
	runF  func(ctx context.Context, name string, args []string) (int, error)
}

func (f *fakeRunner) List(context.Context) ([]jsenv.CommandInfo, error) { return f.listF() }
func (f *fakeRunner) Run(ctx context.Context, name string, args []string) (int, error) {
	return f.runF(ctx, name, args)
}

func runDeps(fr runner) deps {
	return deps{
		newRunner: func(string) (runner, error) { return fr, nil },
	}
}

func writeRunProject(t *testing.T, dir string, commands map[string]string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "dex.config.ts"), []byte("export {};\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for rel, content := range commands {
		path := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// TestRunExitCode: `dex run` exits with the command's result, not a generic
// 1, and prints no Error line.
func TestRunExitCode(t *testing.T) {
	fr := &fakeRunner{runF: func(context.Context, string, []string) (int, error) { return 3, nil }}
	var out, errOut bytes.Buffer
	code := mainWith(runDeps(fr), []string{"run", "x"}, strings.NewReader(""), &out, &errOut)
	if code != 3 {
		t.Fatalf("Main() = %d, want 3", code)
	}
	if errOut.String() != "" {
		t.Fatalf("errOut = %q, want empty (a result is not an error)", errOut.String())
	}
}

func TestRunZeroExitCode(t *testing.T) {
	fr := &fakeRunner{runF: func(context.Context, string, []string) (int, error) { return 0, nil }}
	var out, errOut bytes.Buffer
	code := mainWith(runDeps(fr), []string{"run", "ok"}, strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("Main() = %d, want 0", code)
	}
}

func TestRunUnknownCommandError(t *testing.T) {
	fr := &fakeRunner{runF: func(context.Context, string, []string) (int, error) {
		return 0, errors.New(`unknown command "nope" (run ` + "`dex run`" + ` to list commands)`)
	}}
	var out, errOut bytes.Buffer
	code := mainWith(runDeps(fr), []string{"run", "nope"}, strings.NewReader(""), &out, &errOut)
	if code != 1 {
		t.Fatalf("Main() = %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), `unknown command "nope"`) {
		t.Fatalf("errOut = %q, want unknown-command error", errOut.String())
	}
}

func TestRunListsCommands(t *testing.T) {
	fr := &fakeRunner{listF: func() ([]jsenv.CommandInfo, error) {
		return []jsenv.CommandInfo{
			{Name: "hello", Description: "Say hello"},
			{Name: "bare"},
		}, nil
	}}
	var out, errOut bytes.Buffer
	code := mainWith(runDeps(fr), []string{"run"}, strings.NewReader(""), &out, &errOut)
	if code != 0 {
		t.Fatalf("Main() = %d, want 0", code)
	}
	if out.String() != "hello: Say hello\nbare\n" {
		t.Fatalf("out = %q, want the command list", out.String())
	}
	if errOut.String() != "" {
		t.Fatalf("errOut = %q, want empty", errOut.String())
	}
}

func TestRunListError(t *testing.T) {
	fr := &fakeRunner{listF: func() ([]jsenv.CommandInfo, error) {
		return nil, errors.New("list boom")
	}}
	var out, errOut bytes.Buffer
	code := mainWith(runDeps(fr), []string{"run"}, strings.NewReader(""), &out, &errOut)
	if code != 1 || !strings.Contains(errOut.String(), "list boom") {
		t.Fatalf("Main() = %d, errOut = %q", code, errOut.String())
	}
}

func TestRunNewRunnerError(t *testing.T) {
	d := deps{newRunner: func(string) (runner, error) { return nil, errors.New("runner boom") }}
	root := newRootCmd(d)
	root.SetArgs([]string{"run", "x"})
	var out, errOut bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errOut)
	if err := root.Execute(); err == nil || !strings.Contains(err.Error(), "runner boom") {
		t.Fatalf("Execute() error = %v, want runner boom", err)
	}
}

func TestExitCodeErrorText(t *testing.T) {
	if got := (&exitCodeError{code: 4}).Error(); got != "exit status 4" {
		t.Fatalf("exitCodeError.Error() = %q, want \"exit status 4\"", got)
	}
}

// TestRunSIGINTCancellation sends a real SIGINT to the test process while a
// command is running: the signal must cancel the run's ctx instead of
// killing dex, and Main must return non-zero.
func TestRunSIGINTCancellation(t *testing.T) {
	started := make(chan struct{})
	fr := &fakeRunner{runF: func(ctx context.Context, name string, args []string) (int, error) {
		close(started) // the NotifyContext handler is registered by now
		<-ctx.Done()
		return 0, ctx.Err()
	}}
	d := runDeps(fr)
	root := newRootCmd(d)
	root.SetArgs([]string{"run", "sleeper"})
	var out, errOut bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errOut)

	codeCh := make(chan int, 1)
	go func() {
		if err := root.Execute(); err != nil {
			codeCh <- 1
			return
		}
		codeCh <- 0
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("command did not start")
	}
	if err := syscall.Kill(os.Getpid(), syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	select {
	case code := <-codeCh:
		if code != 1 {
			t.Fatalf("Execute() after SIGINT = %d, want 1", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Execute() did not return after SIGINT")
	}
}

const helloCommand = `import { defineCommand } from "@dex/iac";
defineCommand("hello", {
	description: "Say hello",
	run: (ctx) => {
		if (ctx.args.length !== 1 || ctx.args[0] !== "you") return 9;
		return ctx.exec(["echo", "hi"]);
	},
});`

// TestRunEndToEnd drives the real production runner (esbuild + goja + the
// embedded SDK) over a temp project with a command in dex/commands/.
func TestRunEndToEnd(t *testing.T) {
	dir := t.TempDir()
	writeRunProject(t, dir, map[string]string{"dex/commands/hello.ts": helloCommand})

	var out, errOut bytes.Buffer
	code := Main([]string{"run", "--dir", dir}, strings.NewReader(""), &out, &errOut)
	if code != 0 || out.String() != "hello: Say hello\n" {
		t.Fatalf("run list = %d, out = %q, errOut = %q", code, out.String(), errOut.String())
	}

	out.Reset()
	errOut.Reset()
	code = Main([]string{"run", "--dir", dir, "hello", "you"}, strings.NewReader(""), &out, &errOut)
	if code != 0 || errOut.String() != "" {
		t.Fatalf("run hello = %d, errOut = %q", code, errOut.String())
	}

	out.Reset()
	errOut.Reset()
	code = Main([]string{"run", "--dir", dir, "nope"}, strings.NewReader(""), &out, &errOut)
	if code != 1 || !strings.Contains(errOut.String(), `unknown command "nope"`) {
		t.Fatalf("run nope = %d, errOut = %q", code, errOut.String())
	}
}

func TestRunEndToEndMalformedDotEnv(t *testing.T) {
	dir := t.TempDir()
	writeRunProject(t, dir, map[string]string{"dex/commands/hello.ts": helloCommand})
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("NOEQUALS\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	code := Main([]string{"run", "--dir", dir, "hello", "you"}, strings.NewReader(""), &out, &errOut)
	if code != 1 || !strings.Contains(errOut.String(), ".env:1") {
		t.Fatalf("Main() = %d, errOut = %q, want .env parse error", code, errOut.String())
	}
}

func TestCommandRunnerWithFakeBundler(t *testing.T) {
	dir := t.TempDir()
	writeRunProject(t, dir, nil)
	script := `globalThis.__dex_commands = () => [{ name: "x", description: "D", run: (ctx) => ctx.args.length }];`
	r := &commandRunner{
		bundler:   &fakeBundler{script: script},
		evaluator: jsenv.NewGojaEvaluatorWithTimeout(0),
		dir:       dir,
		env:       map[string]string{},
		resources: func() (map[string]any, error) { return map[string]any{}, nil },
		exec:      func(context.Context, []string) (int, error) { return 0, nil },
	}

	infos, err := r.List(context.Background())
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(infos) != 1 || infos[0].Name != "x" || infos[0].Description != "D" {
		t.Fatalf("List() = %+v", infos)
	}

	code, err := r.Run(context.Background(), "x", []string{"a"})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if code != 1 {
		t.Fatalf("Run() code = %d, want 1 (ctx.args.length)", code)
	}

	if _, err := r.Run(context.Background(), "nope", nil); err == nil || !strings.Contains(err.Error(), `unknown command "nope"`) {
		t.Fatalf("Run(nope) error = %v, want unknown-command error", err)
	}
}

func TestCommandRunnerErrors(t *testing.T) {
	dir := t.TempDir()
	writeRunProject(t, dir, nil)

	r := &commandRunner{
		bundler:   &fakeBundler{err: errors.New("bundle boom")},
		evaluator: jsenv.NewGojaEvaluatorWithTimeout(0),
		dir:       dir,
		env:       map[string]string{},
		resources: func() (map[string]any, error) { return nil, nil },
		exec:      func(context.Context, []string) (int, error) { return 0, nil },
	}
	if _, err := r.List(context.Background()); err == nil || !strings.Contains(err.Error(), "bundle boom") {
		t.Fatalf("List() error = %v, want bundle error", err)
	}
	if _, err := r.Run(context.Background(), "x", nil); err == nil || !strings.Contains(err.Error(), "bundle boom") {
		t.Fatalf("Run() error = %v, want bundle error", err)
	}

	r.bundler = &fakeBundler{script: "ok"}
	r.resources = func() (map[string]any, error) { return nil, errors.New("state boom") }
	if _, err := r.Run(context.Background(), "x", nil); err == nil || !strings.Contains(err.Error(), "load state: state boom") {
		t.Fatalf("Run() error = %v, want load state error", err)
	}

	// Discover error (no dex.config.ts) surfaces from both List and Run.
	r.dir = t.TempDir()
	if _, err := r.List(context.Background()); err == nil || !strings.Contains(err.Error(), "no dex.config.ts in") {
		t.Fatalf("List() error = %v, want no-dex.config.ts error", err)
	}
}

func TestHostExec(t *testing.T) {
	if code, err := hostExec(context.Background(), []string{"sh", "-c", "true"}); err != nil || code != 0 {
		t.Fatalf("hostExec(true) = %d, %v", code, err)
	}
	if code, err := hostExec(context.Background(), []string{"sh", "-c", "exit 3"}); err != nil || code != 3 {
		t.Fatalf("hostExec(exit 3) = %d, %v", code, err)
	}
	if _, err := hostExec(context.Background(), []string{"/nonexistent/dex-xyz"}); err == nil {
		t.Fatal("hostExec(missing binary) error = nil, want start error")
	}
	if _, err := hostExec(context.Background(), nil); err == nil || !strings.Contains(err.Error(), "empty command line") {
		t.Fatalf("hostExec(empty) error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := hostExec(ctx, []string{"sleep", "5"}); err == nil {
		t.Fatal("hostExec(pre-cancelled ctx) error = nil, want error")
	}

	// A child killed mid-run by ctx cancellation (the SIGINT path) must be
	// an error, never a silent exit code.
	ctx, cancel = context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := hostExec(ctx, []string{"sleep", "5"})
		done <- err
	}()
	time.Sleep(100 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "interrupted by signal") {
			t.Fatalf("hostExec(killed) error = %v, want interrupted by signal", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("hostExec(killed) did not return")
	}
}

func TestLoadStateResources(t *testing.T) {
	dir := t.TempDir()
	res, err := loadStateResources(dir)
	if err != nil || len(res) != 0 {
		t.Fatalf("loadStateResources(no state) = %v, %v", res, err)
	}

	// state.json exists but carries no resources: empty map, not nil.
	stateDir := filepath.Join(dir, ".dex")
	if mkErr := os.MkdirAll(stateDir, 0o755); mkErr != nil {
		t.Fatal(mkErr)
	}
	if wErr := os.WriteFile(filepath.Join(stateDir, "state.json"), []byte(`{"project":"p"}`), 0o600); wErr != nil {
		t.Fatal(wErr)
	}
	res, err = loadStateResources(dir)
	if err != nil || len(res) != 0 {
		t.Fatalf("loadStateResources(empty state) = %v, %v", res, err)
	}

	stateJSON := `{"project":"p","serial":1,"resources":{"db":{"type":"container","plugin":"docker","properties":{"pass":{"token":"tok","sha256":"abc123"}},"depends_on":[],"outputs":{"ip":"10.0.0.5"}}}}`
	if wErr := os.WriteFile(filepath.Join(stateDir, "state.json"), []byte(stateJSON), 0o600); wErr != nil {
		t.Fatal(wErr)
	}
	res, err = loadStateResources(dir)
	if err != nil {
		t.Fatalf("loadStateResources() error = %v", err)
	}
	db, ok := res["db"].(map[string]any)
	if !ok {
		t.Fatalf("res[db] = %T, want map", res["db"])
	}
	props := db["properties"].(map[string]any)
	pass := props["pass"].(map[string]any)
	if pass["sha256"] != "abc123" {
		t.Fatalf("pass = %v, want hashed secret preserved", pass)
	}
	if res["db"].(map[string]any)["outputs"].(map[string]any)["ip"] != "10.0.0.5" {
		t.Fatalf("outputs = %v, want preserved", res["db"])
	}

	if wErr := os.WriteFile(filepath.Join(stateDir, "state.json"), []byte("{not json"), 0o600); wErr != nil {
		t.Fatal(wErr)
	}
	if _, sErr := loadStateResources(dir); sErr == nil {
		t.Fatal("loadStateResources(corrupt) error = nil, want parse error")
	}

	// Unreadable state path (a directory) surfaces the read error.
	if rmErr := os.Remove(filepath.Join(stateDir, "state.json")); rmErr != nil {
		t.Fatal(rmErr)
	}
	if mkErr := os.Mkdir(filepath.Join(stateDir, "state.json"), 0o700); mkErr != nil {
		t.Fatal(mkErr)
	}
	if _, sErr := loadStateResources(dir); sErr == nil {
		t.Fatal("loadStateResources(directory) error = nil, want read error")
	}
}
