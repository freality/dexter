package engine

import (
	"context"
	"fmt"

	"github.com/freality/dexter/pkg/engine/ir"
	"github.com/freality/dexter/pkg/engine/state"
)

// Provider applies changes for one plugin. dex-adapter-docker (next roadmap
// item) is the first implementation; the gRPC client in Phase 2 is another.
type Provider interface {
	Create(ctx context.Context, name string, r ir.Resource) (map[string]any, error)
	Update(ctx context.Context, name string, r ir.Resource, cur state.Resource) (map[string]any, error)
	Delete(ctx context.Context, name string, cur state.Resource) error
}

// Registry looks up a Provider by plugin name. Keyed by plugin, not
// resource type: SPEC §5.2 adapters are per-plugin and one plugin serves
// many types.
type Registry struct {
	providers map[string]Provider
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry {
	return &Registry{providers: map[string]Provider{}}
}

// Register associates a plugin name with a Provider.
func (r *Registry) Register(plugin string, p Provider) {
	r.providers[plugin] = p
}

// Get returns the Provider registered for plugin, or an error naming it.
func (r *Registry) Get(plugin string) (Provider, error) {
	p, ok := r.providers[plugin]
	if !ok {
		return nil, fmt.Errorf("no provider registered for plugin %q", plugin)
	}
	return p, nil
}
