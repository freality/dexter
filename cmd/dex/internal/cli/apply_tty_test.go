package cli

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/freality/dexter/pkg/engine"
	"github.com/freality/dexter/pkg/engine/ir"
	"github.com/freality/dexter/pkg/engine/state"

	tea "github.com/charmbracelet/bubbletea"
)

// fakeTTY implements term.File (io.ReadWriteCloser + Fd()) over a
// bytes.Buffer, so isTerminal's type assertion succeeds in tests without a
// real terminal. Its Fd() value is never passed to a real syscall because
// termIsTerminal is stubbed alongside it.
type fakeTTY struct {
	bytes.Buffer
}

func (fakeTTY) Fd() uintptr  { return 0 }
func (fakeTTY) Close() error { return nil }

// TestIsTerminalTrue verifies isTerminal reports true for a term.File whose
// underlying fd the (stubbed) terminal check reports as a tty.
func TestIsTerminalTrue(t *testing.T) {
	orig := termIsTerminal
	termIsTerminal = func(uintptr) bool { return true }
	defer func() { termIsTerminal = orig }()

	if !isTerminal(&fakeTTY{}) {
		t.Fatal("isTerminal() = false, want true")
	}
}

// TestIsTerminalFalseNotAFile verifies isTerminal reports false for a
// plain io.Writer that isn't a term.File at all (today's bytes.Buffer
// behavior for piped/redirected output).
func TestIsTerminalFalseNotAFile(t *testing.T) {
	if isTerminal(&bytes.Buffer{}) {
		t.Fatal("isTerminal() = true, want false for a plain io.Writer")
	}
}

// TestIsTerminalFalseStubbedNotATTY verifies isTerminal reports false when
// the underlying check says the fd isn't a terminal.
func TestIsTerminalFalseStubbedNotATTY(t *testing.T) {
	orig := termIsTerminal
	termIsTerminal = func(uintptr) bool { return false }
	defer func() { termIsTerminal = orig }()

	if isTerminal(&fakeTTY{}) {
		t.Fatal("isTerminal() = true, want false")
	}
}

// TestApplyCmdTUIPath drives the real cobra apply command with a fake
// term.File as its output, forcing runApply's TTY branch (runApplyTUI) end
// to end: the TUI renders rows for the plan and prints the final summary.
func TestApplyCmdTUIPath(t *testing.T) {
	origIsTerminal := termIsTerminal
	termIsTerminal = func(uintptr) bool { return true }
	defer func() { termIsTerminal = origIsTerminal }()

	origInput := ttyInput
	ttyInput = strings.NewReader("") // EOF immediately; no real stdin needed
	defer func() { ttyInput = origInput }()

	dir := t.TempDir()
	mustRunInit(t, dir)
	doc := &ir.Document{Project: "p", Resources: map[string]ir.Resource{
		"a": {Type: "container", Plugin: "docker", Properties: map[string]any{}, DependsOn: []string{}},
	}}
	cmd := newRootCmd(fakeEngineDeps(doc, state.New()))
	out := &fakeTTY{}
	cmd.SetOut(out)
	cmd.SetArgs([]string{"apply", "--dir", dir, "--yes"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(out.String(), "a") {
		t.Fatalf("out = %q, want a row for resource %q", out.String(), "a")
	}
	if !strings.Contains(out.String(), "1 added") {
		t.Fatalf("out = %q, want the TUI summary", out.String())
	}
}

// TestApplyCmdTUIPathFailure verifies a failed apply under the TUI path
// still returns the adapter's error from the command.
func TestApplyCmdTUIPathFailure(t *testing.T) {
	origIsTerminal := termIsTerminal
	termIsTerminal = func(uintptr) bool { return true }
	defer func() { termIsTerminal = origIsTerminal }()

	origInput := ttyInput
	ttyInput = strings.NewReader("")
	defer func() { ttyInput = origInput }()

	dir := t.TempDir()
	mustRunInit(t, dir)
	doc := &ir.Document{Project: "p", Resources: map[string]ir.Resource{
		"a": {Type: "container", Plugin: "docker", Properties: map[string]any{}, DependsOn: []string{}},
	}}
	registry := testRegistry(map[string]engine.Adapter{"docker": nameFailAdapter{failCreate: "a"}})
	cmd := newRootCmd(engineDeps(doc, state.New(), registry))
	out := &fakeTTY{}
	cmd.SetOut(out)
	cmd.SetArgs([]string{"apply", "--dir", dir, "--yes"})

	if err := cmd.Execute(); err == nil {
		t.Fatal("Execute() error = nil, want adapter failure")
	}
}

// TestRunApplyTUIProgramRunError verifies a non-kill/non-interrupt error
// from the bubbletea Program's Run surfaces as runApplyTUI's error, via the
// runProgram seam (a real internal bubbletea error isn't reproducible from
// outside the library).
func TestRunApplyTUIProgramRunError(t *testing.T) {
	origRun := runProgram
	boom := errors.New("program boom")
	runProgram = func(*tea.Program) (tea.Model, error) { return nil, boom }
	defer func() { runProgram = origRun }()

	origInput := ttyInput
	ttyInput = strings.NewReader("")
	defer func() { ttyInput = origInput }()

	dir := t.TempDir()
	mustRunInit(t, dir)
	doc := &ir.Document{Project: "p", Resources: map[string]ir.Resource{
		"a": {Type: "container", Plugin: "docker", Properties: map[string]any{}, DependsOn: []string{}},
	}}
	cmd := newRootCmd(fakeEngineDeps(doc, state.New()))
	out := &fakeTTY{}
	cmd.SetOut(out)
	cmd.SetArgs([]string{"apply", "--dir", dir, "--yes"})

	// Force the TTY branch without depending on termIsTerminal's own stub
	// lifetime from another test.
	origIsTerminal := termIsTerminal
	termIsTerminal = func(uintptr) bool { return true }
	defer func() { termIsTerminal = origIsTerminal }()

	err := cmd.Execute()
	if !errors.Is(err, boom) {
		t.Fatalf("Execute() error = %v, want %v", err, boom)
	}
}
