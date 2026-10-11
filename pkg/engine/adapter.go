package engine

import (
	"context"
	"fmt"
	"sync"

	"github.com/freality/dexter/pkg/engine/ir"
	"github.com/freality/dexter/pkg/engine/state"
)

// Adapter applies changes for one plugin. The in-process Docker adapter is
// the first implementation; the gRPC client in Phase 2 is another.
type Adapter interface {
	// report streams Log/Progress events: only Kind, Message and Percent
	// are used; the engine fills in Name and Action (W4a).
	Create(ctx context.Context, name string, r ir.Resource, report func(Event)) (map[string]any, error)
	Update(ctx context.Context, name string, r ir.Resource, cur state.Resource, report func(Event)) (map[string]any, error)
	Delete(ctx context.Context, name string, cur state.Resource, report func(Event)) error
}

// Registry looks up an Adapter by plugin name. Keyed by plugin, not
// resource type: SPEC §5.2 adapters are per-plugin and one plugin serves
// many types.
type Registry struct {
	resolve func(plugin string) (Adapter, error)
	cache   map[string]Adapter
	cacheOk map[string]bool
	// mu guards cache/cacheOk: W4b calls Get concurrently from the change
	// workers, but each plugin still resolves at most once.
	mu sync.Mutex
}

// NewRegistry returns a Registry backed by resolve, which is called at most
// once per plugin name (lazy, cached).
func NewRegistry(resolve func(plugin string) (Adapter, error)) *Registry {
	return &Registry{resolve: resolve, cache: map[string]Adapter{}, cacheOk: map[string]bool{}}
}

// Get returns the Adapter for plugin, resolving lazily and caching the result.
// An earlier resolve error for the same name is replayed without re-calling resolve.
func (r *Registry) Get(plugin string) (Adapter, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if ok, seen := r.cacheOk[plugin]; seen {
		if !ok {
			return nil, fmt.Errorf("no adapter registered for plugin %q", plugin)
		}
		return r.cache[plugin], nil
	}
	a, err := r.resolve(plugin)
	if err != nil {
		r.cacheOk[plugin] = false
		return nil, err
	}
	r.cache[plugin] = a
	r.cacheOk[plugin] = true
	return a, nil
}
