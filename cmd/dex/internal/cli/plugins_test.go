package cli

import (
	"context"
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
