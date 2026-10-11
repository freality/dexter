// Package cli implements the dex Cobra commands. plugins.go provides the
// plugin adapter bridge (engine.Adapter backed by a plugin.Adapter) and the
// production resolve function that looks up adapter binaries on the
// filesystem or PATH.
package cli

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"sync"

	"github.com/freality/dexter/pkg/engine"
	"github.com/freality/dexter/pkg/engine/ir"
	"github.com/freality/dexter/pkg/engine/state"
	"github.com/freality/dexter/pkg/plugin"
)

// --- kill-func tracking ---------------------------------------------------
//
// Every plugin.Open call returns a kill func. Track them so Main can clean
// up on exit or signal.

var (
	killFuncs []func()
	killMu    sync.Mutex
)

// CleanupClients calls every tracked kill func and clears the list.
func CleanupClients() {
	killMu.Lock()
	for _, k := range killFuncs {
		k()
	}
	killFuncs = []func(){}
	killMu.Unlock()
}

func trackKill(kill func()) {
	killMu.Lock()
	killFuncs = append(killFuncs, kill)
	killMu.Unlock()
}

// --- resolvePlugin --------------------------------------------------------

// resolvePlugin returns a resolve func for engine.NewRegistry that looks up
// adapter binaries. Lookup order per D7:
//
//	<dir>/.dex/plugins/dex-adapter-<name>  then  exec.LookPath("dex-adapter-<name>")
//
// A missing binary produces a "not installed" error. Plan never calls
// resolve — only Apply does — so these filesystem checks never block dex plan.
func resolvePlugin(dir string) func(plugin string) (engine.Adapter, error) {
	return func(plugin string) (engine.Adapter, error) {
		// Try the project-local plugins directory first.
		localPath := filepath.Join(dir, ".dex", "plugins", "dex-adapter-"+plugin)
		if exec.Access(localPath, exec.X_OK) == nil {
			return openAdapter(localPath, plugin)
		}

		// Fall back to PATH.
		bin := "dex-adapter-" + plugin
		path, err := exec.LookPath(bin)
		if err == nil {
			return openAdapter(path, plugin)
		}

		return nil, fmt.Errorf("plugin %q not installed (run: dex plugin install)", plugin)
	}
}

func openAdapter(path, pluginName string) (engine.Adapter, error) {
	inner, kill, err := plugin.Open(path, io.Discard)
	if err != nil {
		return nil, err
	}
	trackKill(kill)
	return &pluginBridge{inner: inner}, nil
}

// --- pluginBridge -----------------------------------------------------------
//
// pluginBridge wraps a plugin.Adapter to implement engine.Adapter.

type pluginBridge struct {
	inner plugin.Adapter
}

func (a *pluginBridge) Create(ctx context.Context, name string, r ir.Resource) (map[string]any, error) {
	return a.inner.Apply(ctx, plugin.Request{
		Name: name, Type: r.Type, Properties: r.Properties,
	}, nilReporter)
}

func (a *pluginBridge) Update(ctx context.Context, name string, r ir.Resource, cur state.Resource) (map[string]any, error) {
	return a.inner.Apply(ctx, plugin.Request{
		Name: name, Type: r.Type, Properties: r.Properties,
		Prior: cur.Properties,
	}, nilReporter)
}

func (a *pluginBridge) Delete(ctx context.Context, name string, cur state.Resource) error {
	return a.inner.Destroy(ctx, plugin.Request{
		Name: name, Type: cur.Type, Properties: cur.Properties, Outputs: cur.Outputs,
	}, nilReporter)
}

// --- nilReporter ------------------------------------------------------------

type nilReporterType struct{}

func (nilReporterType) Log(msg string)   {}
func (nilReporterType) Progress(pct int) {}

var nilReporter = nilReporterType{}
