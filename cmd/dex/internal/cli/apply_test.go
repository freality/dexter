package cli

import (
	"bytes"
	"errors"
	"strings"
	"testing"

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
	if err == nil || !strings.Contains(err.Error(), "no provider registered for plugin") {
		t.Fatalf("Execute() error = %v", err)
	}
}
