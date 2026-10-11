package cli

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/freality/dexter/adapters/docker"
	"github.com/freality/dexter/pkg/engine"
	"github.com/freality/dexter/pkg/engine/ir"
	"github.com/freality/dexter/pkg/engine/state"
)

// TestApplyDrivesDockerAdapter runs create, restart-only update, and delete
// through `dex apply` with the real Docker adapter over an in-memory daemon.
func TestApplyDrivesDockerAdapter(t *testing.T) {
	dir := t.TempDir()
	mustRunInit(t, dir)
	daemon := docker.NewFake()
	st := state.New()

	apply := func(props map[string]any) string {
		t.Helper()
		doc := &ir.Document{Project: "p", Resources: map[string]ir.Resource{}}
		if props != nil {
			doc.Resources["whoami"] = ir.Resource{Type: "container", Plugin: "docker", Properties: props, DependsOn: []string{}}
		}
		reg := testRegistry(map[string]engine.Adapter{"docker": docker.NewWithAPI(daemon)})
		cmd := newRootCmd(engineDeps(doc, st, reg))
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetArgs([]string{"apply", "--dir", dir, "--yes"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("apply error = %v", err)
		}
		return out.String()
	}

	out := apply(map[string]any{"image": "traefik/whoami:latest", "ports": []any{"8080:80"}, "restart": "no"})
	c := daemon.Containers["whoami"]
	if !strings.Contains(out, "Apply complete: 1 added, 0 changed, 0 destroyed.") || c == nil || !c.Running {
		t.Fatalf("create: out = %q, container = %+v", out, c)
	}
	if got := st.Resources["whoami"].Outputs["id"]; got != c.ID {
		t.Fatalf("create: state id = %v, want %s", got, c.ID)
	}

	out = apply(map[string]any{"image": "traefik/whoami:latest", "ports": []any{"8080:80"}, "restart": "always"})
	if !strings.Contains(out, "Apply complete: 0 added, 1 changed, 0 destroyed.") {
		t.Fatalf("update: out = %q", out)
	}
	if daemon.Containers["whoami"].ID != c.ID || st.Resources["whoami"].Outputs["id"] != c.ID {
		t.Fatalf("update: container %s, state id %v; want both kept as %s",
			daemon.Containers["whoami"].ID, st.Resources["whoami"].Outputs["id"], c.ID)
	}

	out = apply(nil)
	if !strings.Contains(out, "Apply complete: 0 added, 0 changed, 1 destroyed.") || len(daemon.Containers) != 0 || len(st.Resources) != 0 {
		t.Fatalf("delete: out = %q, containers = %v, state = %v", out, daemon.Containers, st.Resources)
	}
}

// TestPlanNeverCallsResolve verifies that `dex plan` does not invoke the
// registry's resolve function, even when the adapter binary is missing.
// The resolve function panics if called, proving Plan doesn't touch it.
func TestPlanNeverCallsResolve(t *testing.T) {
	dir := t.TempDir()
	mustRunInit(t, dir)
	st := state.New()

	doc := &ir.Document{Project: "p", Resources: map[string]ir.Resource{
		"whoami": {Type: "container", Plugin: "docker", Properties: map[string]any{}, DependsOn: []string{}},
	}}
	registry := engine.NewRegistry(func(plugin string) (engine.Adapter, error) {
		t.Fatal("resolve called during Plan (should not happen)")
		return nil, fmt.Errorf("unreachable")
	})

	cmd := newRootCmd(engineDeps(doc, st, registry))
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"plan", "--dir", dir})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("plan error = %v", err)
	}
	if !strings.Contains(out.String(), "whoami") {
		t.Fatalf("plan output = %q, want whoami listed", out.String())
	}
}

// TestUnknownPluginError checks that apply fails with a clear "not installed"
// error when the adapter binary is not found.
func TestUnknownPluginError(t *testing.T) {
	dir := t.TempDir()
	mustRunInit(t, dir)
	st := state.New()

	doc := &ir.Document{Project: "p", Resources: map[string]ir.Resource{
		"svc": {Type: "container", Plugin: "missing-plugin", Properties: map[string]any{}, DependsOn: []string{}},
	}}
	registry := engine.NewRegistry(func(plugin string) (engine.Adapter, error) {
		return nil, fmt.Errorf(`plugin %q not installed (run: dex plugin install)`, plugin)
	})

	cmd := newRootCmd(engineDeps(doc, st, registry))
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs([]string{"apply", "--dir", dir, "--yes"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("apply should have failed")
	}
	if !strings.Contains(errOut.String(), "not installed") {
		t.Fatalf("stderr = %q, want 'not installed'", errOut.String())
	}
}
