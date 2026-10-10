package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/freality/dexter/pkg/engine/ir"
	"github.com/freality/dexter/pkg/engine/state"
	"github.com/freality/dexter/pkg/jsenv"
)

// --- fakes -------------------------------------------------------------

type fakeBundler struct {
	script string
	err    error
}

func (f *fakeBundler) Bundle(string) (string, error) { return f.script, f.err }

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

type fakeProvider struct {
	createOut map[string]any
	createErr error
	updateOut map[string]any
	updateErr error
	deleteErr error

	created []string
	updated []string
	deleted []string
}

func (p *fakeProvider) Create(_ context.Context, name string, _ ir.Resource) (map[string]any, error) {
	p.created = append(p.created, name)
	return p.createOut, p.createErr
}

func (p *fakeProvider) Update(_ context.Context, name string, _ ir.Resource, _ state.Resource) (map[string]any, error) {
	p.updated = append(p.updated, name)
	return p.updateOut, p.updateErr
}

func (p *fakeProvider) Delete(_ context.Context, name string, _ state.Resource) error {
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

	plan, err := e.Plan(context.Background(), "entry.ts")
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

	_, err := e.Plan(context.Background(), "entry.ts")
	if !errors.Is(err, boom) {
		t.Fatalf("Plan() error = %v, want %v", err, boom)
	}
}

func TestEnginePlanStoreLoadError(t *testing.T) {
	boom := errors.New("load boom")
	doc := &ir.Document{Project: "p", Resources: map[string]ir.Resource{}}
	e := New(Options{Synth: fakeSynth(t, doc), Store: &fakeStore{loadErr: boom}})

	_, err := e.Plan(context.Background(), "entry.ts")
	if !errors.Is(err, boom) {
		t.Fatalf("Plan() error = %v, want %v", err, boom)
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

	provider := &fakeProvider{
		createOut: map[string]any{"id": "new-id"},
		updateOut: map[string]any{"id": "mid-id"},
	}
	registry := NewRegistry()
	registry.Register("docker", provider)

	store := &fakeStore{st: current}
	locker := &fakeLocker{}
	var out bytes.Buffer

	e := New(Options{
		Synth: fakeSynth(t, desired), Store: store, Lock: locker, Registry: registry, Out: &out,
	})

	plan, err := e.Apply(context.Background(), "entry.ts")
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
	if provider.created[0] != "new" || provider.updated[0] != "mid" || provider.deleted[0] != "old" {
		t.Fatalf("provider calls: created=%v updated=%v deleted=%v", provider.created, provider.updated, provider.deleted)
	}
	if !strings.Contains(out.String(), "mid: done\n") || !strings.Contains(out.String(), "new: done\n") || !strings.Contains(out.String(), "old: done\n") {
		t.Fatalf("Out = %q, want done lines for every change", out.String())
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
	e := New(Options{Synth: fakeSynth(t, desired), Store: store, Lock: locker, Registry: NewRegistry()})

	plan, err := e.Apply(context.Background(), "entry.ts")
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
	e := New(Options{Synth: fakeSynth(t, &ir.Document{Project: "p"}), Store: &fakeStore{st: state.New()}, Lock: locker, Registry: NewRegistry()})

	_, err := e.Apply(context.Background(), "entry.ts")
	if !errors.Is(err, boom) {
		t.Fatalf("Apply() error = %v, want %v", err, boom)
	}
	if locker.released {
		t.Fatal("Release() called despite failed Acquire()")
	}
}

func TestEngineApplySynthError(t *testing.T) {
	boom := errors.New("synth boom")
	locker := &fakeLocker{}
	e := New(Options{Synth: failingSynth(boom), Store: &fakeStore{st: state.New()}, Lock: locker, Registry: NewRegistry()})

	_, err := e.Apply(context.Background(), "entry.ts")
	if !errors.Is(err, boom) {
		t.Fatalf("Apply() error = %v, want %v", err, boom)
	}
	if !locker.released {
		t.Fatal("Release() not called after synth failure")
	}
}

func TestEngineApplyStoreLoadError(t *testing.T) {
	boom := errors.New("load boom")
	locker := &fakeLocker{}
	e := New(Options{Synth: fakeSynth(t, &ir.Document{Project: "p"}), Store: &fakeStore{loadErr: boom}, Lock: locker, Registry: NewRegistry()})

	_, err := e.Apply(context.Background(), "entry.ts")
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
	registry := NewRegistry()
	registry.Register("docker", &fakeProvider{updateErr: boom})
	locker := &fakeLocker{}
	e := New(Options{Synth: fakeSynth(t, desired), Store: &fakeStore{st: current}, Lock: locker, Registry: registry})

	_, err := e.Apply(context.Background(), "entry.ts")
	if err == nil || !errors.Is(err, boom) || !strings.HasPrefix(err.Error(), "update a: ") {
		t.Fatalf("Apply() error = %v, want wrapped update error", err)
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
	registry := NewRegistry()
	registry.Register("docker", &fakeProvider{deleteErr: boom})
	locker := &fakeLocker{}
	e := New(Options{Synth: fakeSynth(t, desired), Store: &fakeStore{st: current}, Lock: locker, Registry: registry})

	_, err := e.Apply(context.Background(), "entry.ts")
	if err == nil || !errors.Is(err, boom) || !strings.HasPrefix(err.Error(), "delete a: ") {
		t.Fatalf("Apply() error = %v, want wrapped delete error", err)
	}
	if !locker.released {
		t.Fatal("Release() not called after delete failure")
	}
}

func TestEngineApplyDiffCycleError(t *testing.T) {
	desired := &ir.Document{Project: "p", Resources: map[string]ir.Resource{
		"a": {Type: "t", Plugin: "p", Properties: map[string]any{}, DependsOn: []string{"b"}},
		"b": {Type: "t", Plugin: "p", Properties: map[string]any{}, DependsOn: []string{"a"}},
	}}
	locker := &fakeLocker{}
	store := &fakeStore{st: state.New()}
	e := New(Options{Synth: fakeSynth(t, desired), Store: store, Lock: locker, Registry: NewRegistry()})

	_, err := e.Apply(context.Background(), "entry.ts")
	if err == nil || !strings.Contains(err.Error(), "dependency cycle") {
		t.Fatalf("Apply() error = %v, want cycle error", err)
	}
	if !locker.released {
		t.Fatal("Release() not called after diff failure")
	}
	if len(store.saves) != 0 {
		t.Fatalf("saves = %d, want 0", len(store.saves))
	}
}

func TestEngineApplyRegistryMiss(t *testing.T) {
	desired := &ir.Document{Project: "p", Resources: map[string]ir.Resource{
		"a": {Type: "container", Plugin: "missing", Properties: map[string]any{}, DependsOn: []string{}},
	}}
	locker := &fakeLocker{}
	e := New(Options{Synth: fakeSynth(t, desired), Store: &fakeStore{st: state.New()}, Lock: locker, Registry: NewRegistry()})

	_, err := e.Apply(context.Background(), "entry.ts")
	want := `create a: no provider registered for plugin "missing"`
	if err == nil || err.Error() != want {
		t.Fatalf("Apply() error = %q, want %q", err, want)
	}
	if !locker.released {
		t.Fatal("Release() not called after registry miss")
	}
}

func TestEngineApplyProviderErrorPersistsEarlierChange(t *testing.T) {
	desired := &ir.Document{Project: "p", Resources: map[string]ir.Resource{
		"a": {Type: "container", Plugin: "docker", Properties: map[string]any{}, DependsOn: []string{}},
		"b": {Type: "container", Plugin: "docker", Properties: map[string]any{}, DependsOn: []string{"a"}},
	}}
	boom := errors.New("create boom")
	provider := &fakeProvider{}
	registry := NewRegistry()
	registry.Register("docker", provider)
	store := &fakeStore{st: state.New()}
	locker := &fakeLocker{}

	e := New(Options{Synth: fakeSynth(t, desired), Store: store, Lock: locker, Registry: registry})

	// First change ("a") succeeds; fail the second ("b") by making the
	// second Create call error.
	calls := 0
	registry.providers["docker"] = providerFunc{
		create: func(ctx context.Context, name string, r ir.Resource) (map[string]any, error) {
			calls++
			if calls == 2 {
				return nil, boom
			}
			return map[string]any{"n": calls}, nil
		},
	}

	_, err := e.Apply(context.Background(), "entry.ts")
	if err == nil || !errors.Is(err, boom) {
		t.Fatalf("Apply() error = %v, want wrapped %v", err, boom)
	}
	if !strings.HasPrefix(err.Error(), "create b: ") {
		t.Fatalf("Apply() error = %v, want prefix 'create b: '", err)
	}
	if len(store.saves) != 1 {
		t.Fatalf("saves = %d, want 1 (only the earlier successful change persisted)", len(store.saves))
	}
	if _, ok := store.saves[0].Resources["a"]; !ok {
		t.Fatal("earlier successful change 'a' not persisted")
	}
	if !locker.released {
		t.Fatal("Release() not called after provider failure")
	}
}

func TestEngineApplyStoreSaveError(t *testing.T) {
	desired := &ir.Document{Project: "p", Resources: map[string]ir.Resource{
		"a": {Type: "container", Plugin: "docker", Properties: map[string]any{}, DependsOn: []string{}},
	}}
	boom := errors.New("save boom")
	registry := NewRegistry()
	registry.Register("docker", &fakeProvider{})
	locker := &fakeLocker{}
	e := New(Options{Synth: fakeSynth(t, desired), Store: &fakeStore{st: state.New(), saveErr: boom}, Lock: locker, Registry: registry})

	_, err := e.Apply(context.Background(), "entry.ts")
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
	e := New(Options{Synth: fakeSynth(t, desired), Store: &fakeStore{st: state.New()}, Lock: locker, Registry: NewRegistry()})

	_, err := e.Apply(context.Background(), "entry.ts")
	if !errors.Is(err, releaseErr) {
		t.Fatalf("Apply() error = %v, want release error joined in", err)
	}
}

// providerFunc lets a single test override just Create for call-counting.
type providerFunc struct {
	create func(context.Context, string, ir.Resource) (map[string]any, error)
}

func (p providerFunc) Create(ctx context.Context, name string, r ir.Resource) (map[string]any, error) {
	return p.create(ctx, name, r)
}
func (providerFunc) Update(context.Context, string, ir.Resource, state.Resource) (map[string]any, error) {
	return nil, nil
}
func (providerFunc) Delete(context.Context, string, state.Resource) error { return nil }
