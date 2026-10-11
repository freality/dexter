// Package docker provides the first-party Docker adapter. Plugin wraps
// *Adapter as a plugin.Adapter so it can run as a separate binary.
package docker

import (
	"context"

	"github.com/freality/dexter/pkg/plugin"
)

// Plugin wraps *Adapter to implement plugin.Adapter, bridging the in-process
// docker.Adapter interface to the gRPC-facing plugin.Adapter interface.
type Plugin struct {
	a *Adapter
}

// NewPlugin returns a Plugin wrapping a.
func NewPlugin(a *Adapter) *Plugin {
	return &Plugin{a: a}
}

// Apply delegates to Create or Update based on whether req.Prior is nil.
// Empty Prior means create; non-nil means update.
func (p *Plugin) Apply(ctx context.Context, req plugin.Request, r plugin.Reporter) (map[string]any, error) {
	if req.Prior == nil {
		return p.a.Create(ctx, req.Name, req.Type, req.Properties)
	}
	return p.a.Update(ctx, req.Name, req.Properties, req.Prior)
}

// Destroy delegates to Delete.
func (p *Plugin) Destroy(ctx context.Context, req plugin.Request, r plugin.Reporter) error {
	return p.a.Delete(ctx, req.Name)
}
