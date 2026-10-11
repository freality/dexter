package docker

import (
	"context"
	"testing"

	"github.com/freality/dexter/pkg/plugin"
	"github.com/moby/moby/api/types/container"
)

// TestNewPlugin verifies NewPlugin returns a Plugin wrapping the adapter.
func TestNewPlugin(t *testing.T) {
	f := NewFake()
	a := NewWithAPI(f)
	p := NewPlugin(a)
	if p.a != a {
		t.Fatalf("NewPlugin().a = %v, want %v", p.a, a)
	}
}

// TestPluginApplyCreate checks that Plugin.Apply with nil Prior calls Create
// and returns the outputs.
func TestPluginApplyCreate(t *testing.T) {
	f := NewFake()
	p := Plugin{a: NewWithAPI(f)}

	out, err := p.Apply(context.Background(), plugin.Request{
		Name: "whoami", Type: "container",
		Properties: map[string]any{"image": "traefik/whoami:latest"},
	}, &noopReporter{})
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if f.Containers["whoami"] == nil {
		t.Fatal("container not created")
	}
	if _, ok := out["id"]; !ok {
		t.Fatalf("outputs = %v, want an id", out)
	}
}

// TestPluginApplyUpdate checks that Plugin.Apply with non-nil Prior calls Update.
func TestPluginApplyUpdate(t *testing.T) {
	f := NewFake()
	p := Plugin{a: NewWithAPI(f)}

	// Create first.
	_, err := p.Apply(context.Background(), plugin.Request{
		Name: "whoami", Type: "container",
		Properties: map[string]any{"image": "traefik/whoami:latest"},
	}, &noopReporter{})
	if err != nil {
		t.Fatalf("create Apply() error = %v", err)
	}
	if !f.Containers["whoami"].Running {
		t.Fatal("container not created or not running")
	}

	// Update with Prior.
	out, err := p.Apply(context.Background(), plugin.Request{
		Name: "whoami", Type: "container",
		Properties: map[string]any{"image": "traefik/whoami:latest", "restart": "always"},
		Prior:      map[string]any{"image": "traefik/whoami:latest"},
	}, &noopReporter{})
	if err != nil {
		t.Fatalf("update Apply() error = %v", err)
	}
	if _, ok := out["id"]; !ok {
		t.Fatalf("update outputs = %v, want an id", out)
	}
}

// TestPluginDestroy checks that Plugin.Destroy calls Delete.
func TestPluginDestroy(t *testing.T) {
	f := NewFake()
	p := Plugin{a: NewWithAPI(f)}

	// Create first.
	_, err := p.Apply(context.Background(), plugin.Request{
		Name: "whoami", Type: "container",
		Properties: map[string]any{"image": "traefik/whoami:latest"},
	}, &noopReporter{})
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if f.Containers["whoami"] == nil {
		t.Fatal("container not created")
	}

	err = p.Destroy(context.Background(), plugin.Request{
		Name: "whoami", Type: "container",
		Properties: map[string]any{},
		Outputs:    map[string]any{},
	}, &noopReporter{})
	if err != nil {
		t.Fatalf("Destroy() error = %v", err)
	}
	if f.Containers["whoami"] != nil {
		t.Fatal("container not deleted")
	}
}

// TestPluginDestroyObeysForeign checks that a foreign container is left alone.
func TestPluginDestroyObeysForeign(t *testing.T) {
	f := NewFake()
	p := Plugin{a: NewWithAPI(f)}

	// Create a container not owned by dex.
	f.Containers["foreign"] = &FakeContainer{
		Config:  &container.Config{Image: "img"},
		Running: true,
	}
	f.Calls = []string{}

	err := p.Destroy(context.Background(), plugin.Request{
		Name: "foreign", Type: "container",
		Properties: map[string]any{},
		Outputs:    map[string]any{},
	}, &noopReporter{})
	if err != nil {
		t.Fatalf("Destroy() error = %v", err)
	}
	if f.Containers["foreign"] == nil {
		t.Fatal("foreign container was deleted")
	}
}

// --- helpers ----------------------------------------------------------------

type noopReporter struct{}

func (noopReporter) Log(msg string)   {}
func (noopReporter) Progress(pct int) {}
