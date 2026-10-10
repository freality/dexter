package cli

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/freality/dexter/pkg/engine/ir"
	"github.com/freality/dexter/pkg/engine/state"
)

func TestPlanCmdWithChanges(t *testing.T) {
	dir := t.TempDir()
	mustRunInit(t, dir)
	doc := &ir.Document{Project: "p", Resources: map[string]ir.Resource{
		"a": {Type: "container", Plugin: "docker", Properties: map[string]any{}, DependsOn: []string{}},
	}}
	cmd := newRootCmd(fakeEngineDeps(doc, state.New()))
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"plan", "--dir", dir})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(out.String(), "  + a") {
		t.Fatalf("out = %q", out.String())
	}
	if !strings.Contains(out.String(), "Plan: 1 to add, 0 to change, 0 to destroy.") {
		t.Fatalf("out = %q", out.String())
	}
}

func TestPlanCmdNoChanges(t *testing.T) {
	dir := t.TempDir()
	mustRunInit(t, dir)
	doc := &ir.Document{Project: "p", Resources: map[string]ir.Resource{}}
	cmd := newRootCmd(fakeEngineDeps(doc, state.New()))
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"plan", "--dir", dir})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(out.String(), "No changes. Infrastructure matches configuration.") {
		t.Fatalf("out = %q", out.String())
	}
}

func TestPlanCmdMissingConfig(t *testing.T) {
	dir := t.TempDir()
	cmd := newRootCmd(fakeEngineDeps(nil, state.New()))
	cmd.SetArgs([]string{"plan", "--dir", dir})

	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "run `dex init`") {
		t.Fatalf("Execute() error = %v", err)
	}
}

func TestPlanCmdEngineConstructionError(t *testing.T) {
	dir := t.TempDir()
	mustRunInit(t, dir)
	boom := errors.New("engine boom")
	cmd := newRootCmd(failingEngineDeps(boom))
	cmd.SetArgs([]string{"plan", "--dir", dir})

	err := cmd.Execute()
	if !errors.Is(err, boom) {
		t.Fatalf("Execute() error = %v, want %v", err, boom)
	}
}

func TestPlanCmdSynthError(t *testing.T) {
	dir := t.TempDir()
	mustRunInit(t, dir)
	// A nil doc means the fake evaluator returns no data, which fails IR parsing.
	cmd := newRootCmd(fakeEngineDeps(nil, state.New()))
	cmd.SetArgs([]string{"plan", "--dir", dir})

	if err := cmd.Execute(); err == nil {
		t.Fatal("Execute() error = nil, want synth/parse error")
	}
}

func mustRunInit(t *testing.T, dir string) {
	t.Helper()
	if err := runInit(&bytes.Buffer{}, dir); err != nil {
		t.Fatalf("runInit() error = %v", err)
	}
}
