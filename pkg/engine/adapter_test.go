package engine

import (
	"context"
	"fmt"
	"testing"

	"github.com/freality/dexter/pkg/engine/ir"
	"github.com/freality/dexter/pkg/engine/state"
)

type stubAdapter struct{}

func (stubAdapter) Create(context.Context, string, ir.Resource) (map[string]any, error) {
	return nil, nil
}
func (stubAdapter) Update(context.Context, string, ir.Resource, state.Resource) (map[string]any, error) {
	return nil, nil
}
func (stubAdapter) Delete(context.Context, string, state.Resource) error { return nil }

func resolveErr(plugin string) (Adapter, error) {
	return nil, fmt.Errorf("no adapter registered for plugin %q", plugin)
}

func resolveOne(plugin string, a Adapter) func(string) (Adapter, error) {
	return func(p string) (Adapter, error) {
		if p != plugin {
			return nil, fmt.Errorf("no adapter registered for plugin %q", p)
		}
		return a, nil
	}
}

func TestRegistryGetMiss(t *testing.T) {
	r := NewRegistry(resolveErr)
	_, err := r.Get("docker")
	if err == nil || err.Error() != `no adapter registered for plugin "docker"` {
		t.Fatalf("Get() error = %v", err)
	}
}

func TestRegistryGetHit(t *testing.T) {
	p := stubAdapter{}
	r := NewRegistry(resolveOne("docker", p))

	got, err := r.Get("docker")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got != p {
		t.Fatalf("Get() = %v, want %v", got, p)
	}
}

func TestRegistryCacheHit(t *testing.T) {
	callCount := 0
	p := stubAdapter{}
	r := NewRegistry(func(plugin string) (Adapter, error) {
		callCount++
		if plugin != "docker" {
			return nil, fmt.Errorf("no adapter registered for plugin %q", plugin)
		}
		return p, nil
	})

	got1, err1 := r.Get("docker")
	got2, err2 := r.Get("docker")
	if err1 != nil || err2 != nil {
		t.Fatalf("Get() error = %v, %v", err1, err2)
	}
	if got1 != p || got2 != p {
		t.Fatalf("Get() = %v, %v, want %v", got1, got2, p)
	}
	if callCount != 1 {
		t.Fatalf("resolve called %d times, want 1 (cached)", callCount)
	}
}

func TestRegistryCachesPerPlugin(t *testing.T) {
	callCount := 0
	p := stubAdapter{}
	q := stubAdapter{}
	r := NewRegistry(func(plugin string) (Adapter, error) {
		callCount++
		switch plugin {
		case "docker":
			return p, nil
		case "other":
			return q, nil
		}
		return nil, fmt.Errorf("no adapter registered for plugin %q", plugin)
	})

	got1, _ := r.Get("docker")
	got2, _ := r.Get("other")
	got3, _ := r.Get("docker")
	if got1 != p || got2 != q || got3 != p {
		t.Fatalf("wrong adapters: got1=%v got2=%v got3=%v", got1, got2, got3)
	}
	if callCount != 2 {
		t.Fatalf("resolve called %d times, want 2 (once per plugin name)", callCount)
	}
}

func TestRegistryCacheError(t *testing.T) {
	callCount := 0
	r := NewRegistry(func(plugin string) (Adapter, error) {
		callCount++
		return nil, fmt.Errorf("no adapter registered for plugin %q", plugin)
	})

	_, err1 := r.Get("missing")
	_, err2 := r.Get("missing")
	if err1 == nil || err2 == nil {
		t.Fatalf("Get() error = %v, %v, want error", err1, err2)
	}
	if callCount != 1 {
		t.Fatalf("resolve called %d times, want 1 (error cached)", callCount)
	}
}
