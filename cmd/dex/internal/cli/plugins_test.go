package cli

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/freality/dexter/pkg/engine"
	"github.com/freality/dexter/pkg/engine/ir"
	"github.com/freality/dexter/pkg/engine/state"
	"github.com/freality/dexter/pkg/plugin"
)

// TestCleanupClients verifies that tracking and cleaning kill funcs works.
func TestCleanupClients(t *testing.T) {
	cleaned := []string{}
	trackKill(func() { cleaned = append(cleaned, "a") })
	trackKill(func() { cleaned = append(cleaned, "b") })
	CleanupClients()
	if len(cleaned) != 2 || cleaned[0] != "a" || cleaned[1] != "b" {
		t.Fatalf("cleaned = %v, want [a, b]", cleaned)
	}
	// Second call is a no-op.
	CleanupClients()
	if len(cleaned) != 2 {
		t.Fatalf("cleaned = %v, want still 2", cleaned)
	}
}

// TestEventReporterLog verifies Log emits an EventLog.
func TestEventReporterLog(t *testing.T) {
	var got engine.Event
	r := &eventReporter{report: func(ev engine.Event) { got = ev }}
	r.Log("hello")
	if got.Kind != engine.EventLog || got.Message != "hello" {
		t.Fatalf("Event = %+v, want EventLog + hello", got)
	}
}

// TestEventReporterProgress verifies Progress emits an EventProgress.
func TestEventReporterProgress(t *testing.T) {
	var got engine.Event
	r := &eventReporter{report: func(ev engine.Event) { got = ev }}
	r.Progress(42)
	if got.Kind != engine.EventProgress || got.Percent != 42 {
		t.Fatalf("Event = %+v, want EventProgress + 42", got)
	}
}

// TestPluginBridgeCreate verifies Create bridges to plugin.Adapter.Apply
// with nil Prior (create signal) and returns the result.
func TestPluginBridgeCreate(t *testing.T) {
	var captured plugin.Request
	inner := &captureAdapter{apply: func(req plugin.Request) (map[string]any, error) {
		captured = req
		return map[string]any{"id": "abc"}, nil
	}}
	bridge := pluginBridge{inner: inner}

	out, err := bridge.Create(context.Background(), "s1", ir.Resource{
		Type: "container", Plugin: "docker",
		Properties: map[string]any{"image": "img"},
	}, nil)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if captured.Name != "s1" || captured.Type != "container" || captured.Prior != nil {
		t.Fatalf("Request = %+v, want s1/container/nil Prior", captured)
	}
	if out["id"] != "abc" {
		t.Fatalf("outputs = %v, want {id: abc}", out)
	}
}

// TestPluginBridgeUpdate verifies Update bridges to plugin.Adapter.Apply
// with non-nil Prior.
func TestPluginBridgeUpdate(t *testing.T) {
	var captured plugin.Request
	inner := &captureAdapter{apply: func(req plugin.Request) (map[string]any, error) {
		captured = req
		return nil, nil
	}}
	bridge := pluginBridge{inner: inner}

	_, err := bridge.Update(context.Background(), "s1", ir.Resource{
		Type: "container", Plugin: "docker",
		Properties: map[string]any{"image": "v2"},
	}, state.Resource{
		Type: "container", Plugin: "docker",
		Properties: map[string]any{"image": "v1"},
	}, nil)
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if captured.Name != "s1" || captured.Prior["image"] != "v1" {
		t.Fatalf("Request = %+v, want s1 with Prior image=v1", captured)
	}
}

// TestPluginBridgeDelete verifies Delete bridges to plugin.Adapter.Destroy
// with Properties and Outputs from cur.
func TestPluginBridgeDelete(t *testing.T) {
	var captured plugin.Request
	inner := &captureAdapter{destroy: func(req plugin.Request) error {
		captured = req
		return nil
	}}
	bridge := pluginBridge{inner: inner}

	err := bridge.Delete(context.Background(), "s1", state.Resource{
		Type: "container", Plugin: "docker",
		Properties: map[string]any{"image": "v1"},
		Outputs:    map[string]any{"id": "abc"},
	}, nil)
	if err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if captured.Name != "s1" || captured.Properties["image"] != "v1" || captured.Outputs["id"] != "abc" {
		t.Fatalf("Request = %+v, want s1 with props+outputs", captured)
	}
}

// --- captureAdapter implements plugin.Adapter by capturing calls. -----------

type captureAdapter struct {
	apply   func(plugin.Request) (map[string]any, error)
	destroy func(plugin.Request) error
}

func (a *captureAdapter) Apply(_ context.Context, req plugin.Request, _ plugin.Reporter) (map[string]any, error) {
	return a.apply(req)
}

func (a *captureAdapter) Destroy(_ context.Context, req plugin.Request, _ plugin.Reporter) error {
	if a.destroy != nil {
		return a.destroy(req)
	}
	return nil
}

// --- resolvePlugin / openAdapter --------------------------------------------

// writeExecutable creates an executable regular file at path (any content;
// it is never actually run, only looked up and "opened" via the pluginOpen
// seam).
func writeExecutable(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

// withPluginOpen swaps the pluginOpen seam for the test's duration.
func withPluginOpen(t *testing.T, fn func(path string, logOut io.Writer) (plugin.Adapter, func(), error)) {
	t.Helper()
	original := pluginOpen
	pluginOpen = fn
	t.Cleanup(func() { pluginOpen = original })
}

// TestResolvePluginNotInstalled verifies the "not installed" error when the
// binary is in neither the project-local dir nor PATH.
func TestResolvePluginNotInstalled(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	// With no declared specs, the message suggests adding a repo.
	_, err := resolvePlugin(t.TempDir(), []string{})("docker")
	if err == nil || err.Error() != `plugin "docker" not installed (run: dex plugin add <repo>)` {
		t.Fatalf("err = %v", err)
	}
}

// TestResolvePluginNotInstalledDeclared verifies that when specs are
// declared (non-empty IR.Plugins), the message suggests install.
func TestResolvePluginNotInstalledDeclared(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	_, err := resolvePlugin(t.TempDir(), []string{"github.com/freality/dexter@v1.0.0"})("docker")
	if err == nil || err.Error() != `plugin "docker" not installed (run: dex plugin install)` {
		t.Fatalf("err = %v", err)
	}
}

// TestResolvePluginLocal verifies the project-local .dex/plugins lookup
// (D7's first path) opens via openAdapter.
func TestResolvePluginLocal(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("exec bit not meaningful on windows")
	}
	dir := t.TempDir()
	writeExecutable(t, filepath.Join(dir, ".dex", "plugins", "dex-adapter-docker"))
	t.Setenv("PATH", t.TempDir()) // PATH has nothing, so only local resolves

	var gotPath string
	withPluginOpen(t, func(path string, _ io.Writer) (plugin.Adapter, func(), error) {
		gotPath = path
		return &captureAdapter{}, func() {}, nil
	})

	adapter, err := resolvePlugin(dir, []string{})("docker")
	if err != nil {
		t.Fatalf("resolvePlugin: %v", err)
	}
	if adapter == nil {
		t.Fatal("adapter is nil")
	}
	want := filepath.Join(dir, ".dex", "plugins", "dex-adapter-docker")
	if gotPath != want {
		t.Fatalf("opened path = %q, want %q", gotPath, want)
	}
}

// TestResolvePluginPATH verifies the PATH fallback (D7's second path) when
// no project-local binary exists.
func TestResolvePluginPATH(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("exec bit not meaningful on windows")
	}
	pathDir := t.TempDir()
	writeExecutable(t, filepath.Join(pathDir, "dex-adapter-docker"))
	t.Setenv("PATH", pathDir)

	withPluginOpen(t, func(path string, _ io.Writer) (plugin.Adapter, func(), error) {
		return &captureAdapter{}, func() {}, nil
	})

	_, err := resolvePlugin(t.TempDir(), []string{})("docker") // empty project dir: no local binary
	if err != nil {
		t.Fatalf("resolvePlugin: %v", err)
	}
}

// TestOpenAdapterError verifies a pluginOpen failure propagates and does not
// track a kill func.
func TestOpenAdapterError(t *testing.T) {
	withPluginOpen(t, func(_ string, _ io.Writer) (plugin.Adapter, func(), error) {
		return nil, nil, errors.New("boom")
	})
	before := len(killFuncs)
	_, err := openAdapter("/does/not/matter", "docker")
	if err == nil || err.Error() != "boom" {
		t.Fatalf("err = %v", err)
	}
	if len(killFuncs) != before {
		t.Fatalf("killFuncs grew on error: %d -> %d", before, len(killFuncs))
	}
}

// TestOpenAdapterSuccess verifies a successful pluginOpen tracks its kill
// func and wraps the adapter in a pluginBridge.
func TestOpenAdapterSuccess(t *testing.T) {
	killed := false
	withPluginOpen(t, func(_ string, _ io.Writer) (plugin.Adapter, func(), error) {
		return &captureAdapter{}, func() { killed = true }, nil
	})
	before := len(killFuncs)
	adapter, err := openAdapter("/does/not/matter", "docker")
	if err != nil {
		t.Fatalf("openAdapter: %v", err)
	}
	if _, ok := adapter.(*pluginBridge); !ok {
		t.Fatalf("adapter = %T, want *pluginBridge", adapter)
	}
	if len(killFuncs) != before+1 {
		t.Fatalf("killFuncs did not grow: %d -> %d", before, len(killFuncs))
	}
	CleanupClients()
	if !killed {
		t.Fatal("kill func was not called by CleanupClients")
	}
}