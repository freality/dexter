package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/freality/dexter/pkg/engine/diff"
	"github.com/freality/dexter/pkg/engine/ir"
	"github.com/freality/dexter/pkg/engine/state"
	"github.com/freality/dexter/pkg/jsenv"
)

// --- helpers ----------------------------------------------------------------

// resolveOne returns a resolve func that serves a single named adapter.
func resolveOne(plugin string, a Adapter) func(string) (Adapter, error) {
	return func(p string) (Adapter, error) {
		if p != plugin {
			return nil, fmt.Errorf("no adapter registered for plugin %q", p)
		}
		return a, nil
	}
}

// resolveNone returns a resolve func that always fails.
func resolveNone() func(string) (Adapter, error) {
	return func(plugin string) (Adapter, error) {
		return nil, fmt.Errorf("no adapter registered for plugin %q", plugin)
	}
}

// disableResolver returns a resolve that blocks all adapters with an error
// mentioning pluginName, matching TestEngineApplyRegistryMiss's expected error.
func resolveMissing() func(string) (Adapter, error) {
	return func(plugin string) (Adapter, error) {
		return nil, fmt.Errorf("no adapter registered for plugin %q", plugin)
	}
}

// --- fakes -------------------------------------------------------------

type fakeBundler struct {
	script string
	err    error
}

func (f *fakeBundler) Bundle(string, []string) (string, error) { return f.script, f.err }

// entryDir is a project dir containing dex.config.ts so the real Discover
// inside Synth passes; the bundler and evaluator underneath stay faked.
// Discover only stats the file, so one shared dir serves every test.
var entryDir = func() string {
	dir, err := os.MkdirTemp("", "dex-engine-test")
	if err != nil {
		panic(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "dex.config.ts"), nil, 0o644); err != nil {
		panic(err)
	}
	return dir
}()

type fakeEvaluator struct {
	data []byte
	err  error
}

func (f *fakeEvaluator) Evaluate(context.Context, string, map[string]string) ([]byte, error) {
	return f.data, f.err
}

func fakeSynth(t *testing.T, doc *ir.Document) *jsenv.Synthesizer {
	t.Helper()
	var data []byte
	if doc != nil {
		b, err := json.Marshal(doc)
		if err != nil {
			t.Fatal(err)
		}
		data = b
	}
	return jsenv.NewSynthesizer(&fakeBundler{script: "ok"}, &fakeEvaluator{data: data})
}

func failingSynth(err error) *jsenv.Synthesizer {
	return jsenv.NewSynthesizer(&fakeBundler{err: err}, &fakeEvaluator{})
}

type fakeStore struct {
	st      *state.State
	loadErr error
	saveErr error
	saves   []state.State // deep snapshots, in order
}

func (f *fakeStore) Load() (*state.State, error) {
	if f.loadErr != nil {
		return nil, f.loadErr
	}
	return f.st, nil
}

func (f *fakeStore) Save(s *state.State) error {
	if f.saveErr != nil {
		return f.saveErr
	}
	cp := *s
	cp.Resources = make(map[string]state.Resource, len(s.Resources))
	for k, v := range s.Resources {
		cp.Resources[k] = v
	}
	f.saves = append(f.saves, cp)
	return nil
}

type fakeLocker struct {
	acquireErr error
	releaseErr error
	acquired   bool
	released   bool
}

func (f *fakeLocker) Acquire() error { f.acquired = true; return f.acquireErr }
func (f *fakeLocker) Release() error { f.released = true; return f.releaseErr }

type fakeAdapter struct {
	createOut map[string]any
	createErr error
	updateOut map[string]any
	updateErr error
	deleteErr error

	created []string
	updated []string
	deleted []string
}

func (p *fakeAdapter) Create(_ context.Context, name string, _ ir.Resource, _ func(Event)) (map[string]any, error) {
	p.created = append(p.created, name)
	return p.createOut, p.createErr
}

func (p *fakeAdapter) Update(_ context.Context, name string, _ ir.Resource, _ state.Resource, _ func(Event)) (map[string]any, error) {
	p.updated = append(p.updated, name)
	return p.updateOut, p.updateErr
}

func (p *fakeAdapter) Delete(_ context.Context, name string, _ state.Resource, _ func(Event)) error {
	p.deleted = append(p.deleted, name)
	return p.deleteErr
}

// --- Plan ----------------------------------------------------------------

func TestEnginePlanSuccess(t *testing.T) {
	doc := &ir.Document{Project: "p", Resources: map[string]ir.Resource{
		"a": {Type: "container", Plugin: "docker", Properties: map[string]any{}, DependsOn: []string{}},
	}}
	e := New(Options{
		Synth: fakeSynth(t, doc),
		Store: &fakeStore{st: state.New()},
	})

	plan, err := e.Plan(context.Background(), entryDir)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if len(plan.Changes) != 1 || plan.Changes[0].Name != "a" {
		t.Fatalf("Plan() = %+v", plan.Changes)
	}
}

func TestEnginePlanSynthError(t *testing.T) {
	boom := errors.New("synth boom")
	e := New(Options{Synth: failingSynth(boom), Store: &fakeStore{st: state.New()}})

	_, err := e.Plan(context.Background(), entryDir)
	if !errors.Is(err, boom) {
		t.Fatalf("Plan() error = %v, want %v", err, boom)
	}
}

func TestEnginePlanStoreLoadError(t *testing.T) {
	boom := errors.New("load boom")
	doc := &ir.Document{Project: "p", Resources: map[string]ir.Resource{}}
	e := New(Options{Synth: fakeSynth(t, doc), Store: &fakeStore{loadErr: boom}})

	_, err := e.Plan(context.Background(), entryDir)
	if !errors.Is(err, boom) {
		t.Fatalf("Plan() error = %v, want %v", err, boom)
	}
}

// TestEnginePlanParseError covers Plan's ir.Parse call: moved here from
// jsenv, which now returns raw bytes and leaves IR parsing to the engine.
func TestEnginePlanParseError(t *testing.T) {
	synth := jsenv.NewSynthesizer(&fakeBundler{script: "ok"}, &fakeEvaluator{data: []byte("not json")})
	e := New(Options{Synth: synth, Store: &fakeStore{st: state.New()}})

	_, err := e.Plan(context.Background(), entryDir)
	if err == nil || !strings.Contains(err.Error(), "parse IR:") {
		t.Fatalf("Plan() error = %v, want wrapped parse error", err)
	}
}

// --- Apply -----------------------------------------------------------------

func TestEngineApplyFullLifecycle(t *testing.T) {
	current := state.New()
	current.Project = "p"
	current.Resources["mid"] = state.Resource{Type: "container", Plugin: "docker", Properties: map[string]any{"image": "v1"}}
	current.Resources["old"] = state.Resource{Type: "container", Plugin: "docker", Properties: map[string]any{}}

	desired := &ir.Document{Project: "p", Resources: map[string]ir.Resource{
		"mid": {Type: "container", Plugin: "docker", Properties: map[string]any{"image": "v2"}, DependsOn: []string{}},
		"new": {Type: "container", Plugin: "docker", Properties: map[string]any{}, DependsOn: []string{"mid"}},
	}}

	adapter := &fakeAdapter{
		createOut: map[string]any{"id": "new-id"},
		updateOut: map[string]any{"id": "mid-id"},
	}
	registry := NewRegistry(resolveOne("docker", adapter))

	store := &fakeStore{st: current}
	locker := &fakeLocker{}
	var events []Event

	e := New(Options{
		Synth: fakeSynth(t, desired), Store: store, Lock: locker, Registry: registry,
		Events: func(ev Event) { events = append(events, ev) },
	})

	plan, err := e.Plan(context.Background(), entryDir)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	plan, err = e.Apply(context.Background(), plan)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if len(plan.Changes) != 3 {
		t.Fatalf("Changes = %+v", plan.Changes)
	}
	wantOrder := []string{"mid", "new", "old"}
	for i, c := range plan.Changes {
		if c.Name != wantOrder[i] {
			t.Fatalf("Changes[%d].Name = %q, want %q", i, c.Name, wantOrder[i])
		}
	}
	if !locker.acquired || !locker.released {
		t.Fatalf("locker = %+v, want acquired+released", locker)
	}
	if len(store.saves) != 3 {
		t.Fatalf("saves = %d, want 3 (one per change)", len(store.saves))
	}
	final := store.saves[len(store.saves)-1]
	if final.Project != "p" {
		t.Fatalf("final.Project = %q", final.Project)
	}
	if _, ok := final.Resources["old"]; ok {
		t.Fatal("old resource still present after delete")
	}
	if final.Resources["mid"].Outputs["id"] != "mid-id" {
		t.Fatalf("mid outputs = %+v", final.Resources["mid"])
	}
	if final.Resources["new"].Outputs["id"] != "new-id" {
		t.Fatalf("new outputs = %+v", final.Resources["new"])
	}
	if adapter.created[0] != "new" || adapter.updated[0] != "mid" || adapter.deleted[0] != "old" {
		t.Fatalf("adapter calls: created=%v updated=%v deleted=%v", adapter.created, adapter.updated, adapter.deleted)
	}
	wantEvents := []Event{
		{Name: "mid", Action: diff.ActionUpdate, Kind: EventStarted},
		{Name: "mid", Action: diff.ActionUpdate, Kind: EventDone},
		{Name: "new", Action: diff.ActionCreate, Kind: EventStarted},
		{Name: "new", Action: diff.ActionCreate, Kind: EventDone},
		{Name: "old", Action: diff.ActionDelete, Kind: EventStarted},
		{Name: "old", Action: diff.ActionDelete, Kind: EventDone},
	}
	if !reflect.DeepEqual(events, wantEvents) {
		t.Fatalf("events = %+v, want %+v", events, wantEvents)
	}
}

// --- Events (W4a) -----------------------------------------------------------

// TestEngineApplyCreateEventSequence asserts a successful create emits
// exactly Started then Done, with Name and Action filled in.
func TestEngineApplyCreateEventSequence(t *testing.T) {
	desired := &ir.Document{Project: "p", Resources: map[string]ir.Resource{
		"a": {Type: "container", Plugin: "docker", Properties: map[string]any{}, DependsOn: []string{}},
	}}
	registry := NewRegistry()
	registry.Register("docker", &fakeAdapter{})
	locker := &fakeLocker{}
	store := &fakeStore{st: state.New()}
	var events []Event
	e := New(Options{
		Synth: fakeSynth(t, desired), Store: store, Lock: locker, Registry: registry,
		Events: func(ev Event) { events = append(events, ev) },
	})

	plan, err := e.Plan(context.Background(), entryDir)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if _, err = e.Apply(context.Background(), plan); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	want := []Event{
		{Name: "a", Action: diff.ActionCreate, Kind: EventStarted},
		{Name: "a", Action: diff.ActionCreate, Kind: EventDone},
	}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %+v, want %+v", events, want)
	}
}

// TestEngineApplyUpdateFailureEmitsFailed asserts a failed update emits
// Started then Failed, with the wrapped adapter error in Message.
func TestEngineApplyUpdateFailureEmitsFailed(t *testing.T) {
	current := state.New()
	current.Project = "p"
	current.Resources["a"] = state.Resource{Type: "container", Plugin: "docker", Properties: map[string]any{"image": "v1"}}
	desired := &ir.Document{Project: "p", Resources: map[string]ir.Resource{
		"a": {Type: "container", Plugin: "docker", Properties: map[string]any{"image": "v2"}, DependsOn: []string{}},
	}}
	boom := errors.New("update boom")
	registry := NewRegistry()
	registry.Register("docker", &fakeAdapter{updateErr: boom})
	locker := &fakeLocker{}
	var events []Event
	e := New(Options{
		Synth: fakeSynth(t, desired), Store: &fakeStore{st: current}, Lock: locker, Registry: registry,
		Events: func(ev Event) { events = append(events, ev) },
	})

	plan, err := e.Plan(context.Background(), entryDir)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if _, err = e.Apply(context.Background(), plan); err == nil {
		t.Fatal("Apply() error = nil, want update failure")
	}
	want := []Event{
		{Name: "a", Action: diff.ActionUpdate, Kind: EventStarted},
		{Name: "a", Action: diff.ActionUpdate, Kind: EventFailed, Message: "update a: update boom"},
	}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %+v, want %+v", events, want)
	}
}

// TestEngineApplyAdapterLogPassthrough asserts adapter Log/Progress calls
// through the report parameter reach the Events sink with Name and Action
// filled in (only Kind/Message/Percent come from the adapter, per W4a).
func TestEngineApplyAdapterLogPassthrough(t *testing.T) {
	desired := &ir.Document{Project: "p", Resources: map[string]ir.Resource{
		"a": {Type: "container", Plugin: "docker", Properties: map[string]any{}, DependsOn: []string{}},
	}}
	registry := NewRegistry()
	registry.Register("docker", adapterFunc{
		create: func(_ context.Context, _ string, _ ir.Resource, report func(Event)) (map[string]any, error) {
			report(Event{Kind: EventLog, Message: "pulling image"})
			report(Event{Kind: EventProgress, Percent: 42})
			return nil, nil
		},
	})
	locker := &fakeLocker{}
	var events []Event
	e := New(Options{
		Synth: fakeSynth(t, desired), Store: &fakeStore{st: state.New()}, Lock: locker, Registry: registry,
		Events: func(ev Event) { events = append(events, ev) },
	})

	plan, err := e.Plan(context.Background(), entryDir)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if _, err = e.Apply(context.Background(), plan); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	want := []Event{
		{Name: "a", Action: diff.ActionCreate, Kind: EventStarted},
		{Name: "a", Action: diff.ActionCreate, Kind: EventLog, Message: "pulling image"},
		{Name: "a", Action: diff.ActionCreate, Kind: EventProgress, Percent: 42},
		{Name: "a", Action: diff.ActionCreate, Kind: EventDone},
	}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %+v, want %+v", events, want)
	}
}

func TestEngineApplyEmptyPlanDoesNotSave(t *testing.T) {
	current := state.New()
	current.Project = "p"
	current.Resources["a"] = state.Resource{Type: "container", Plugin: "docker", Properties: map[string]any{}}
	desired := &ir.Document{Project: "p", Resources: map[string]ir.Resource{
		"a": {Type: "container", Plugin: "docker", Properties: map[string]any{}, DependsOn: []string{}},
	}}
	store := &fakeStore{st: current}
	locker := &fakeLocker{}
	e := New(Options{Synth: fakeSynth(t, desired), Store: store, Lock: locker, Registry: NewRegistry(resolveNone())})

	plan, err := e.Plan(context.Background(), entryDir)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	plan, err = e.Apply(context.Background(), plan)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if !plan.Empty() {
		t.Fatalf("plan = %+v, want empty", plan.Changes)
	}
	if len(store.saves) != 0 {
		t.Fatalf("saves = %d, want 0 for empty plan", len(store.saves))
	}
	if !locker.acquired || !locker.released {
		t.Fatalf("locker = %+v, want acquired+released even for empty plan", locker)
	}
}

func TestEngineApplyLockAcquireFailure(t *testing.T) {
	boom := errors.New("locked")
	locker := &fakeLocker{acquireErr: boom}
	e := New(Options{Synth: fakeSynth(t, &ir.Document{Project: "p"}), Store: &fakeStore{st: state.New()}, Lock: locker, Registry: NewRegistry(resolveNone())})

	plan, err := e.Plan(context.Background(), entryDir)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	_, err = e.Apply(context.Background(), plan)
	if !errors.Is(err, boom) {
		t.Fatalf("Apply() error = %v, want %v", err, boom)
	}
	if locker.released {
		t.Fatal("Release() called despite failed Acquire()")
	}
}

// TestEngineApplyStaleStateRejected proves Apply executes exactly the plan
// the caller confirmed: if state.json moved underneath it (another dex
// process, a manual edit) between Plan and Apply, it must fail instead of
// silently applying stale changes against resources it never diffed.
func TestEngineApplyStaleStateRejected(t *testing.T) {
	desired := &ir.Document{Project: "p", Resources: map[string]ir.Resource{
		"a": {Type: "container", Plugin: "docker", Properties: map[string]any{}, DependsOn: []string{}},
	}}
	store := &fakeStore{st: state.New()}
	locker := &fakeLocker{}
	e := New(Options{Synth: fakeSynth(t, desired), Store: store, Lock: locker, Registry: NewRegistry(resolveNone())})

	plan, err := e.Plan(context.Background(), entryDir)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	store.st.Serial++ // simulate a concurrent write between Plan and Apply

	_, err = e.Apply(context.Background(), plan)
	if err == nil || !strings.Contains(err.Error(), "state changed since plan was computed") {
		t.Fatalf("Apply() error = %v, want stale-state rejection", err)
	}
	if !locker.released {
		t.Fatal("Release() not called after stale-state rejection")
	}
}

func TestEngineApplyStoreLoadError(t *testing.T) {
	boom := errors.New("load boom")
	locker := &fakeLocker{}
	e := New(Options{Store: &fakeStore{loadErr: boom}, Lock: locker, Registry: NewRegistry(resolveNone())})

	_, err := e.Apply(context.Background(), &diff.Plan{Project: "p"})
	if !errors.Is(err, boom) {
		t.Fatalf("Apply() error = %v, want %v", err, boom)
	}
	if !locker.released {
		t.Fatal("Release() not called after load failure")
	}
}

func TestEngineApplyUpdateErrorAborts(t *testing.T) {
	current := state.New()
	current.Project = "p"
	current.Resources["a"] = state.Resource{Type: "container", Plugin: "docker", Properties: map[string]any{"image": "v1"}}
	desired := &ir.Document{Project: "p", Resources: map[string]ir.Resource{
		"a": {Type: "container", Plugin: "docker", Properties: map[string]any{"image": "v2"}, DependsOn: []string{}},
	}}
	boom := errors.New("update boom")
	registry := NewRegistry(resolveOne("docker", &fakeAdapter{updateErr: boom}))
	locker := &fakeLocker{}
	store := &fakeStore{st: current}
	e := New(Options{Synth: fakeSynth(t, desired), Store: store, Lock: locker, Registry: registry})

	plan, err := e.Plan(context.Background(), entryDir)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	_, err = e.Apply(context.Background(), plan)
	if err == nil || !errors.Is(err, boom) || !strings.HasPrefix(err.Error(), "update a: ") {
		t.Fatalf("Apply() error = %v, want wrapped update error", err)
	}
	// The target may be half-changed, so a is saved tainted for replacement.
	if len(store.saves) != 1 || !store.saves[0].Resources["a"].Tainted || store.saves[0].Resources["a"].Properties["image"] != "v2" {
		t.Fatalf("saves = %+v, want a saved tainted with desired properties", store.saves)
	}
	if !locker.released {
		t.Fatal("Release() not called after update failure")
	}
}

func TestEngineApplyDeleteErrorAborts(t *testing.T) {
	current := state.New()
	current.Project = "p"
	current.Resources["a"] = state.Resource{Type: "container", Plugin: "docker", Properties: map[string]any{}}
	desired := &ir.Document{Project: "p", Resources: map[string]ir.Resource{}}
	boom := errors.New("delete boom")
	registry := NewRegistry(resolveOne("docker", &fakeAdapter{deleteErr: boom}))
	locker := &fakeLocker{}
	e := New(Options{Synth: fakeSynth(t, desired), Store: &fakeStore{st: current}, Lock: locker, Registry: registry})

	plan, err := e.Plan(context.Background(), entryDir)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	_, err = e.Apply(context.Background(), plan)
	if err == nil || !errors.Is(err, boom) || !strings.HasPrefix(err.Error(), "delete a: ") {
		t.Fatalf("Apply() error = %v, want wrapped delete error", err)
	}
	if !locker.released {
		t.Fatal("Release() not called after delete failure")
	}
}

func TestEngineApplyRegistryMiss(t *testing.T) {
	desired := &ir.Document{Project: "p", Resources: map[string]ir.Resource{
		"a": {Type: "container", Plugin: "missing", Properties: map[string]any{}, DependsOn: []string{}},
	}}
	locker := &fakeLocker{}
	store := &fakeStore{st: state.New()}
	e := New(Options{Synth: fakeSynth(t, desired), Store: store, Lock: locker, Registry: NewRegistry(resolveMissing())})

	plan, err := e.Plan(context.Background(), entryDir)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	_, err = e.Apply(context.Background(), plan)
	want := `create a: no adapter registered for plugin "missing"`
	if err == nil || err.Error() != want {
		t.Fatalf("Apply() error = %q, want %q", err, want)
	}
	if _, ok := store.st.Resources["a"]; ok {
		t.Fatal("a recorded in state although no adapter ever ran")
	}
	if !locker.released {
		t.Fatal("Release() not called after registry miss")
	}
}

func TestEngineApplyAdapterErrorPersistsEarlierChange(t *testing.T) {
	desired := &ir.Document{Project: "p", Resources: map[string]ir.Resource{
		"a": {Type: "container", Plugin: "docker", Properties: map[string]any{}, DependsOn: []string{}},
		"b": {Type: "container", Plugin: "docker", Properties: map[string]any{}, DependsOn: []string{"a"}},
	}}
	boom := errors.New("create boom")

	calls := 0
	adapter := adapterFunc{
		create: func(ctx context.Context, name string, r ir.Resource, _ func(Event)) (map[string]any, error) {
			calls++
			if calls == 2 {
				return nil, boom
			}
			return map[string]any{"n": calls}, nil
		},
	}
	registry := NewRegistry(resolveOne("docker", adapter))
	store := &fakeStore{st: state.New()}
	locker := &fakeLocker{}

	e := New(Options{Synth: fakeSynth(t, desired), Store: store, Lock: locker, Registry: registry})

	plan, err := e.Plan(context.Background(), entryDir)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	_, err = e.Apply(context.Background(), plan)
	if err == nil || !errors.Is(err, boom) {
		t.Fatalf("Apply() error = %v, want wrapped %v", err, boom)
	}
	if !strings.HasPrefix(err.Error(), "create b: ") {
		t.Fatalf("Apply() error = %v, want prefix 'create b: '", err)
	}
	if len(store.saves) != 2 {
		t.Fatalf("saves = %d, want 2 (one per change attempt)", len(store.saves))
	}
	if a, ok := store.saves[0].Resources["a"]; !ok || a.Tainted {
		t.Fatal("earlier successful change 'a' not persisted")
	}
	if b := store.saves[1].Resources["b"]; !b.Tainted || len(b.DependsOn) != 1 {
		t.Fatalf("b = %+v, want failed create saved tainted with its dependencies", b)
	}
	if !locker.released {
		t.Fatal("Release() not called after adapter failure")
	}
}

func TestEngineApplyJoinsSaveErrorWithAdapterError(t *testing.T) {
	desired := &ir.Document{Project: "p", Resources: map[string]ir.Resource{
		"a": {Type: "container", Plugin: "docker", Properties: map[string]any{}, DependsOn: []string{}},
	}}
	createErr, saveErr := errors.New("create boom"), errors.New("save boom")
	registry := NewRegistry(resolveOne("docker", &fakeAdapter{createErr: createErr}))
	e := New(Options{Synth: fakeSynth(t, desired), Store: &fakeStore{st: state.New(), saveErr: saveErr}, Lock: &fakeLocker{}, Registry: registry})

	plan, err := e.Plan(context.Background(), entryDir)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	_, err = e.Apply(context.Background(), plan)
	if !errors.Is(err, createErr) || !errors.Is(err, saveErr) {
		t.Fatalf("Apply() error = %v, want both the create and save errors", err)
	}
}

func TestEngineApplyStoreSaveError(t *testing.T) {
	desired := &ir.Document{Project: "p", Resources: map[string]ir.Resource{
		"a": {Type: "container", Plugin: "docker", Properties: map[string]any{}, DependsOn: []string{}},
	}}
	boom := errors.New("save boom")
	registry := NewRegistry(resolveOne("docker", &fakeAdapter{}))
	locker := &fakeLocker{}
	e := New(Options{Synth: fakeSynth(t, desired), Store: &fakeStore{st: state.New(), saveErr: boom}, Lock: locker, Registry: registry})

	plan, err := e.Plan(context.Background(), entryDir)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	_, err = e.Apply(context.Background(), plan)
	if err == nil || !errors.Is(err, boom) || !strings.Contains(err.Error(), "save state:") {
		t.Fatalf("Apply() error = %v, want wrapped save error", err)
	}
	if !locker.released {
		t.Fatal("Release() not called after save failure")
	}
}

func TestEngineApplyLockReleaseErrorJoined(t *testing.T) {
	desired := &ir.Document{Project: "p", Resources: map[string]ir.Resource{}}
	releaseErr := errors.New("release boom")
	locker := &fakeLocker{releaseErr: releaseErr}
	e := New(Options{Synth: fakeSynth(t, desired), Store: &fakeStore{st: state.New()}, Lock: locker, Registry: NewRegistry(resolveNone())})

	plan, err := e.Plan(context.Background(), entryDir)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	_, err = e.Apply(context.Background(), plan)
	if !errors.Is(err, releaseErr) {
		t.Fatalf("Apply() error = %v, want release error joined in", err)
	}
}

// adapterFunc lets a single test override just Create for call-counting.
type adapterFunc struct {
	create func(context.Context, string, ir.Resource, func(Event)) (map[string]any, error)
}

func (p adapterFunc) Create(ctx context.Context, name string, r ir.Resource, report func(Event)) (map[string]any, error) {
	return p.create(ctx, name, r, report)
}
func (adapterFunc) Update(context.Context, string, ir.Resource, state.Resource, func(Event)) (map[string]any, error) {
	return nil, nil
}
func (adapterFunc) Delete(context.Context, string, state.Resource, func(Event)) error { return nil }

// --- Plan key/hash errors ---------------------------------------------------

// TestEnginePlanKeyError covers Plan's loadOrCreateKey failure: a read-only dir.
func TestEnginePlanKeyError(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "dex.config.ts"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	// Make the dir read-only so MkdirAll inside loadOrCreateKey fails.
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) }) // so TempDir cleanup works

	doc := &ir.Document{Project: "p", Resources: map[string]ir.Resource{}}
	e := New(Options{
		Synth: fakeSynth(t, doc),
		Store: &fakeStore{st: state.New()},
	})

	_, err := e.Plan(context.Background(), dir)
	if err == nil || !strings.HasPrefix(err.Error(), "secret key:") {
		t.Fatalf("Plan() error = %v, want 'secret key:' prefix", err)
	}
}

func TestEnginePlanHashError(t *testing.T) {
	doc := &ir.Document{Project: "p", Resources: map[string]ir.Resource{
		"a": {Type: "container", Plugin: "docker", Properties: map[string]any{
			"env": map[string]any{"PASSWORD": map[string]any{
				"$dex_type": "secret", "provider": "env", "key": "DB_PWD",
			}},
		}, DependsOn: []string{}},
	}}
	e := New(Options{
		Synth: fakeSynth(t, doc),
		Store: &fakeStore{st: state.New()},
		// Env deliberately missing DB_PWD so hashSecrets in Plan fails.
		Env: map[string]string{"OTHER": "x"},
	})

	_, err := e.Plan(context.Background(), entryDir)
	if err == nil {
		t.Fatal("Plan() error = nil, want hash error")
	}
	if !strings.Contains(err.Error(), `resource "a"`) || !strings.Contains(err.Error(), `secret "DB_PWD" is not set`) {
		t.Fatalf("Plan() error = %v, want resource+key naming", err)
	}
}

// --- Apply key error ---------------------------------------------------------

// TestEngineApplyKeyError covers loadOrCreateKey failure in Apply.
func TestEngineApplyKeyError(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "dex.config.ts"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	// Plan succeeds: no secrets needed.
	doc := &ir.Document{Project: "p", Resources: map[string]ir.Resource{
		"a": {Type: "container", Plugin: "docker", Properties: map[string]any{}, DependsOn: []string{}},
	}}
	registry := NewRegistry(resolveOne("docker", &fakeAdapter{}))
	store := &fakeStore{st: state.New()}
	locker := &fakeLocker{}
	e := New(Options{Synth: fakeSynth(t, doc), Store: store, Lock: locker, Registry: registry})

	plan, err := e.Plan(context.Background(), dir)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}

	// Delete the key so Apply must recreate it; make .dex read-only so WriteFile fails.
	keyPath := filepath.Join(dir, ".dex", "secret.key")
	if err = os.Remove(keyPath); err != nil {
		t.Fatal(err)
	}
	dexDir := filepath.Join(dir, ".dex")
	if err = os.Chmod(dexDir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dexDir, 0o755) })

	// A fresh engine for Apply; can't write to .dex/ so key creation fails.
	e2 := New(Options{
		Synth:    failingSynth(errors.New("unused")),
		Store:    store,
		Lock:     &fakeLocker{},
		Registry: registry,
	})

	_, err = e2.Apply(context.Background(), plan)
	if err == nil || !strings.HasPrefix(err.Error(), "secret key:") {
		t.Fatalf("Apply() error = %v, want 'secret key:' prefix", err)
	}
}

// --- applyChange resolve errors ---------------------------------------------

// TestEngineApplyCreateResolveError: Create change with a secret token whose env
// key is missing. Plan succeeds (env has the key during Plan).
func TestEngineApplyCreateResolveError(t *testing.T) {
	doc := &ir.Document{Project: "p", Resources: map[string]ir.Resource{
		"a": {Type: "container", Plugin: "docker", Properties: map[string]any{
			"env": map[string]any{"PASSWORD": map[string]any{
				"$dex_type": "secret", "provider": "env", "key": "DB_PWD",
			}},
		}, DependsOn: []string{}},
	}}
	registry := NewRegistry(resolveOne("docker", &fakeAdapter{}))
	store := &fakeStore{st: state.New()}
	locker := &fakeLocker{}

	// Plan with env containing the key so hashSecrets succeeds.
	engine1 := New(Options{
		Synth:    fakeSynth(t, doc),
		Store:    store,
		Lock:     locker,
		Registry: registry,
		Env:      map[string]string{"DB_PWD": "secret"},
	})
	plan, err := engine1.Plan(context.Background(), entryDir)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}

	// Apply with env missing the key.
	engine2 := New(Options{
		Synth:    failingSynth(errors.New("unused")),
		Store:    store,
		Lock:     &fakeLocker{},
		Registry: registry,
		Env:      map[string]string{},
	})
	_, err = engine2.Apply(context.Background(), plan)
	if err == nil || !strings.HasPrefix(err.Error(), "create a:") {
		t.Fatalf("Apply() error = %v, want 'create a:' prefix", err)
	}
	if !strings.Contains(err.Error(), `secret "DB_PWD" is not set`) {
		t.Fatalf("Apply() error = %v, want missing key error", err)
	}
}

// TestEngineApplyUpdateResolveError: Update change with a secret token whose env
// key is missing in desired side. Plan has the key; env loses it for Apply.
func TestEngineApplyUpdateResolveError(t *testing.T) {
	current := state.New()
	current.Project = "p"
	current.Resources["a"] = state.Resource{Type: "container", Plugin: "docker", Properties: map[string]any{"image": "v1"}}

	doc := &ir.Document{Project: "p", Resources: map[string]ir.Resource{
		"a": {Type: "container", Plugin: "docker", Properties: map[string]any{
			"image": "v2",
			"env": map[string]any{"PASSWORD": map[string]any{
				"$dex_type": "secret", "provider": "env", "key": "DB_PWD",
			}},
		}, DependsOn: []string{}},
	}}
	registry := NewRegistry(resolveOne("docker", &fakeAdapter{}))
	store := &fakeStore{st: current}
	locker := &fakeLocker{}

	// Plan with env containing the key so hashSecrets succeeds.
	engine1 := New(Options{
		Synth:    fakeSynth(t, doc),
		Store:    store,
		Lock:     locker,
		Registry: registry,
		Env:      map[string]string{"DB_PWD": "secret"},
	})
	plan, err := engine1.Plan(context.Background(), entryDir)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}

	// Apply with env missing the key — resolveSecrets fails on desired side.
	engine2 := New(Options{
		Synth:    failingSynth(errors.New("unused")),
		Store:    store,
		Lock:     &fakeLocker{},
		Registry: registry,
		Env:      map[string]string{},
	})
	_, err = engine2.Apply(context.Background(), plan)
	if err == nil || !strings.HasPrefix(err.Error(), "update a:") {
		t.Fatalf("Apply() error = %v, want 'update a:' prefix", err)
	}
	if !strings.Contains(err.Error(), `secret "DB_PWD" is not set`) {
		t.Fatalf("Apply() error = %v, want missing key error", err)
	}
}

// TestEnginePlanCycleError verifies that a cycle in the dependency graph fails
// Plan with a diff.Compute error, covering the `return nil, err` after diff.Compute.
func TestEnginePlanCycleError(t *testing.T) {
	// IR validation does not catch cycles (only self-dependencies), so a
	// cycle A→B→A passes Parse but fails diff.Compute's DAG sort.
	doc := &ir.Document{Project: "p", Resources: map[string]ir.Resource{
		"a": {Type: "container", Plugin: "docker", Properties: map[string]any{}, DependsOn: []string{"b"}},
		"b": {Type: "container", Plugin: "docker", Properties: map[string]any{}, DependsOn: []string{"a"}},
	}}
	e := New(Options{
		Synth: fakeSynth(t, doc),
		Store: &fakeStore{st: state.New()},
	})
	_, err := e.Plan(context.Background(), entryDir)
	if err == nil || (!strings.Contains(err.Error(), "dependency") && !strings.Contains(err.Error(), "cycle")) {
		t.Fatalf("Plan() error = %v, want cycle/dependency graph error", err)
	}
}
