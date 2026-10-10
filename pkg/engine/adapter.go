package engine

import (
	"context"
	"fmt"

	"github.com/freality/dexter/pkg/engine/ir"
	"github.com/freality/dexter/pkg/engine/state"
)

// Adapter applies changes for one plugin. The in-process Docker adapter is
// the first implementation; the gRPC client in Phase 2 is another.
type Adapter interface {
	Create(ctx context.Context, name string, r ir.Resource) (map[string]any, error)
	Update(ctx context.Context, name string, r ir.Resource, cur state.Resource) (map[string]any, error)
	Delete(ctx context.Context, name string, cur state.Resource) error
}

// Registry looks up an Adapter by plugin name. Keyed by plugin, not
// resource type: SPEC §5.2 adapters are per-plugin and one plugin serves
// many types.
type Registry struct {
	adapters map[string]Adapter
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry {
	return &Registry{adapters: map[string]Adapter{}}
}

// Register associates a plugin name with an Adapter.
func (r *Registry) Register(plugin string, a Adapter) {
	r.adapters[plugin] = a
}

// Get returns the Adapter registered for plugin, or an error naming it.
func (r *Registry) Get(plugin string) (Adapter, error) {
	a, ok := r.adapters[plugin]
	if !ok {
		return nil, fmt.Errorf("no adapter registered for plugin %q", plugin)
	}
	return a, nil
}
