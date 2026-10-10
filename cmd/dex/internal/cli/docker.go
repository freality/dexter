package cli

import (
	"context"

	"github.com/freality/dexter/adapters/docker"
	"github.com/freality/dexter/pkg/engine"
	"github.com/freality/dexter/pkg/engine/ir"
	"github.com/freality/dexter/pkg/engine/state"
)

// registryFor registers d as the adapter for plugin "docker".
func registryFor(d *docker.Adapter) *engine.Registry {
	r := engine.NewRegistry()
	r.Register("docker", dockerAdapter{d})
	return r
}

// dockerAdapter fits docker.Adapter, which knows nothing of engine types,
// to engine.Adapter.
type dockerAdapter struct{ d *docker.Adapter }

func (a dockerAdapter) Create(ctx context.Context, name string, r ir.Resource) (map[string]any, error) {
	return a.d.Create(ctx, name, r.Type, r.Properties)
}

func (a dockerAdapter) Update(ctx context.Context, name string, r ir.Resource, cur state.Resource) (map[string]any, error) {
	return a.d.Update(ctx, name, r.Properties, cur.Properties)
}

func (a dockerAdapter) Delete(ctx context.Context, name string, _ state.Resource) error {
	return a.d.Delete(ctx, name)
}
