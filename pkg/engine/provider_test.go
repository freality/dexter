package engine

import (
	"context"
	"testing"

	"github.com/freality/dexter/pkg/engine/ir"
	"github.com/freality/dexter/pkg/engine/state"
)

type stubProvider struct{}

func (stubProvider) Create(context.Context, string, ir.Resource) (map[string]any, error) {
	return nil, nil
}
func (stubProvider) Update(context.Context, string, ir.Resource, state.Resource) (map[string]any, error) {
	return nil, nil
}
func (stubProvider) Delete(context.Context, string, state.Resource) error { return nil }

func TestRegistryGetMiss(t *testing.T) {
	r := NewRegistry()
	_, err := r.Get("docker")
	if err == nil || err.Error() != `no provider registered for plugin "docker"` {
		t.Fatalf("Get() error = %v", err)
	}
}

func TestRegistryGetHit(t *testing.T) {
	r := NewRegistry()
	p := stubProvider{}
	r.Register("docker", p)

	got, err := r.Get("docker")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got != p {
		t.Fatalf("Get() = %v, want %v", got, p)
	}
}
