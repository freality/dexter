package cli

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/freality/dexter/pkg/engine"
	"github.com/freality/dexter/pkg/engine/ir"
	"github.com/freality/dexter/pkg/engine/state"
)

func TestApplyCmdYesFlag(t *testing.T) {
	dir := t.TempDir()
	mustRunInit(t, dir)
	doc := &ir.Document{Project: "p", Resources: map[string]ir.Resource{
		"a": {Type: "container", Plugin: "docker", Properties: map[string]any{}, DependsOn: []string{}},
	}}
	cmd := newRootCmd(fakeEngineDeps(doc, state.New()))
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"apply", "--dir", dir, "--yes"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(out.String(), "Apply complete: 1 added, 0 changed, 0 destroyed.") {
		t.Fatalf("out = %q", out.String())
	}
	if strings.Contains(out.String(), "[y/N]") {
		t.Fatalf("out = %q, want no prompt with --yes", out.String())
	}
}

func TestApplyCmdConfirmYes(t *testing.T) {
	dir := t.TempDir()
	mustRunInit(t, dir)
	doc := &ir.Document{Project: "p", Resources: map[string]ir.Resource{
		"a": {Type: "container", Plugin: "docker", Properties: map[string]any{}, DependsOn: []string{}},
	}}
	cmd := newRootCmd(fakeEngineDeps(doc, state.New()))
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetIn(strings.NewReader("y\n"))
	cmd.SetArgs([]string{"apply", "--dir", dir})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(out.String(), "Apply these changes? [y/N]: ") {
		t.Fatalf("out = %q, want prompt", out.String())
	}
	if !strings.Contains(out.String(), "Apply complete: 1 added, 0 changed, 0 destroyed.") {
		t.Fatalf("out = %q", out.String())
	}
}

func TestApplyCmdConfirmNo(t *testing.T) {
	dir := t.TempDir()
	mustRunInit(t, dir)
	doc := &ir.Document{Project: "p", Resources: map[string]ir.Resource{
		"a": {Type: "container", Plugin: "docker", Properties: map[string]any{}, DependsOn: []string{}},
	}}
	cmd := newRootCmd(fakeEngineDeps(doc, state.New()))
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetIn(strings.NewReader("n\n"))
	cmd.SetArgs([]string{"apply", "--dir", dir})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(out.String(), "Apply cancelled.") {
		t.Fatalf("out = %q, want cancellation", out.String())
	}
	if strings.Contains(out.String(), "Apply complete") {
		t.Fatalf("out = %q, want no apply after declining", out.String())
	}
}

func TestApplyCmdNoChangesSkipsPromptAndLock(t *testing.T) {
	dir := t.TempDir()
	mustRunInit(t, dir)
	doc := &ir.Document{Project: "p", Resources: map[string]ir.Resource{}}
	cmd := newRootCmd(fakeEngineDeps(doc, state.New()))
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetIn(strings.NewReader("")) // would fail to read if a prompt were attempted
	cmd.SetArgs([]string{"apply", "--dir", dir})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(out.String(), "No changes. Infrastructure matches configuration.") {
		t.Fatalf("out = %q", out.String())
	}
	if strings.Contains(out.String(), "[y/N]") {
		t.Fatalf("out = %q, want no prompt for empty plan", out.String())
	}
}

func TestApplyCmdMissingConfig(t *testing.T) {
	dir := t.TempDir()
	cmd := newRootCmd(fakeEngineDeps(nil, state.New()))
	cmd.SetArgs([]string{"apply", "--dir", dir, "--yes"})

	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "run `dex init`") {
		t.Fatalf("Execute() error = %v", err)
	}
}

func TestApplyCmdEngineConstructionError(t *testing.T) {
	dir := t.TempDir()
	mustRunInit(t, dir)
	boom := errors.New("engine boom")
	cmd := newRootCmd(failingEngineDeps(boom))
	cmd.SetArgs([]string{"apply", "--dir", dir, "--yes"})

	err := cmd.Execute()
	if !errors.Is(err, boom) {
		t.Fatalf("Execute() error = %v, want %v", err, boom)
	}
}

func TestApplyCmdPlanError(t *testing.T) {
	dir := t.TempDir()
	mustRunInit(t, dir)
	cmd := newRootCmd(fakeEngineDeps(nil, state.New()))
	cmd.SetArgs([]string{"apply", "--dir", dir, "--yes"})

	if err := cmd.Execute(); err == nil {
		t.Fatal("Execute() error = nil, want plan/synth error")
	}
}

func TestApplyCmdApplyError(t *testing.T) {
	dir := t.TempDir()
	mustRunInit(t, dir)
	doc := &ir.Document{Project: "p", Resources: map[string]ir.Resource{
		"a": {Type: "container", Plugin: "missing-plugin", Properties: map[string]any{}, DependsOn: []string{}},
	}}
	cmd := newRootCmd(fakeEngineDeps(doc, state.New()))
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"apply", "--dir", dir, "--yes"})

	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "no adapter registered for plugin") {
		t.Fatalf("Execute() error = %v", err)
	}
}

// TestApplyOutputByteIdenticalSuccess proves `dex apply --yes` prints exactly
// today's bytes: the plan, the "<name>: done" progress lines, and the summary
// (W4a acceptance: a successful run's output is unchanged).
func TestApplyOutputByteIdenticalSuccess(t *testing.T) {
	dir := t.TempDir()
	mustRunInit(t, dir)
	doc := &ir.Document{Project: "p", Resources: map[string]ir.Resource{
		"a": {Type: "container", Plugin: "docker", Properties: map[string]any{}, DependsOn: []string{}},
	}}
	registry := engine.NewRegistry()
	registry.Register("docker", okAdapter{})
	var out bytes.Buffer
	cmd := newRootCmd(eventsEngineDeps(doc, state.New(), registry, applySink(&out)))
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"apply", "--dir", dir, "--yes"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	want := "\n  + a\n\n" +
		"Plan: 1 to add, 0 to change, 0 to destroy.\n" +
		"a: done\n" +
		"Apply complete: 1 added, 0 changed, 0 destroyed.\n"
	if out.String() != want {
		t.Fatalf("out = %q, want %q", out.String(), want)
	}
}

// TestApplyOutputByteIdenticalFailedCreate proves a failed create keeps
// today's output: the done line only for what succeeded, the error returned
// (Main prints it as "Error: ..."), and no summary line.
func TestApplyOutputByteIdenticalFailedCreate(t *testing.T) {
	dir := t.TempDir()
	mustRunInit(t, dir)
	doc := &ir.Document{Project: "p", Resources: map[string]ir.Resource{
		"a": {Type: "container", Plugin: "docker", Properties: map[string]any{}, DependsOn: []string{}},
		"b": {Type: "container", Plugin: "docker", Properties: map[string]any{}, DependsOn: []string{}},
	}}
	registry := engine.NewRegistry()
	registry.Register("docker", nameFailAdapter{failCreate: "b"})
	var out bytes.Buffer
	cmd := newRootCmd(eventsEngineDeps(doc, state.New(), registry, applySink(&out)))
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"apply", "--dir", dir, "--yes"})

	err := cmd.Execute()
	if err == nil || err.Error() != "create b: boom" {
		t.Fatalf("Execute() error = %v, want 'create b: boom'", err)
	}
	want := "\n  + a\n  + b\n\n" +
		"Plan: 2 to add, 0 to change, 0 to destroy.\n" +
		"a: done\n"
	if out.String() != want {
		t.Fatalf("out = %q, want %q", out.String(), want)
	}
	if strings.Contains(out.String(), "Apply complete") {
		t.Fatalf("out = %q, want no summary after failure", out.String())
	}
}

// TestApplyOutputByteIdenticalFailedUpdate proves a failed update returns
// the wrapped error and prints no done line (today's behavior).
func TestApplyOutputByteIdenticalFailedUpdate(t *testing.T) {
	dir := t.TempDir()
	mustRunInit(t, dir)
	current := state.New()
	current.Project = "p"
	current.Resources["a"] = state.Resource{Type: "container", Plugin: "docker", Properties: map[string]any{"image": "v1"}}
	doc := &ir.Document{Project: "p", Resources: map[string]ir.Resource{
		"a": {Type: "container", Plugin: "docker", Properties: map[string]any{"image": "v2"}, DependsOn: []string{}},
	}}
	registry := engine.NewRegistry()
	registry.Register("docker", nameFailAdapter{failUpdate: "a"})
	var out bytes.Buffer
	cmd := newRootCmd(eventsEngineDeps(doc, current, registry, applySink(&out)))
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"apply", "--dir", dir, "--yes"})

	err := cmd.Execute()
	if err == nil || err.Error() != "update a: boom" {
		t.Fatalf("Execute() error = %v, want 'update a: boom'", err)
	}
	want := "\n  ~ a (image)\n\n" +
		"Plan: 0 to add, 1 to change, 0 to destroy.\n"
	if out.String() != want {
		t.Fatalf("out = %q, want %q", out.String(), want)
	}
	if strings.Contains(out.String(), "done") {
		t.Fatalf("out = %q, want no done line after failure", out.String())
	}
}
