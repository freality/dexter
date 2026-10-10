package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/freality/dexter/adapters/docker"
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
		cmd := newRootCmd(engineDeps(doc, st, registryFor(docker.NewWithAPI(daemon))))
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

func TestMainReportsBadDockerHost(t *testing.T) {
	dir := t.TempDir()
	mustRunInit(t, dir)
	t.Setenv("DOCKER_HOST", "::not-a-host")
	var out, errOut bytes.Buffer
	if code := Main([]string{"plan", "--dir", dir}, strings.NewReader(""), &out, &errOut); code != 1 || !strings.Contains(errOut.String(), "docker client: ") {
		t.Fatalf("code = %d, stderr = %q", code, errOut.String())
	}
}
