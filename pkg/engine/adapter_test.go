package engine

import (
	"context"
	"testing"

	"github.com/freality/dexter/pkg/engine/ir"
	"github.com/freality/dexter/pkg/engine/state"
)

type stubAdapter struct{}

func (stubAdapter) Create(context.Context, string, ir.Resource, func(Event)) (map[string]any, error) {
	return nil, nil
}
func (stubAdapter) Update(context.Context, string, ir.Resource, state.Resource, func(Event)) (map[string]any, error) {
	return nil, nil
}
func (stubAdapter) Delete(context.Context, string, state.Resource, func(Event)) error { return nil }

func TestRegistryGetMiss(t *testing.T) {
	r := NewRegistry()
	_, err := r.Get("docker")
	if err == nil || err.Error() != `no adapter registered for plugin "docker"` {
		t.Fatalf("Get() error = %v", err)
	}
}

func TestRegistryGetHit(t *testing.T) {
	r := NewRegistry()
	p := stubAdapter{}
	r.Register("docker", p)

	got, err := r.Get("docker")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got != p {
		t.Fatalf("Get() = %v, want %v", got, p)
	}
}
