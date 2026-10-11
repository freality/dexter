package engine

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/freality/dexter/pkg/engine/diff"
	"github.com/freality/dexter/pkg/engine/ir"
	"github.com/freality/dexter/pkg/engine/state"
)

// gateAdapter blocks every Create/Update/Delete on release (closed once)
// and records each call's resource name on entered, in the order the
// dispatcher launched them. errs names fail immediately, before blocking,
// so a test can race a fast failure against blocked in-flight work.
type gateAdapter struct {
	entered chan string
	release chan struct{}
	errs    map[string]error
}

func newGateAdapter(errs map[string]error) *gateAdapter {
	return &gateAdapter{
		entered: make(chan string, 16),
		release: make(chan struct{}),
		errs:    errs,
	}
}

func (g *gateAdapter) Create(_ context.Context, name string, _ ir.Resource, _ func(Event)) (map[string]any, error) {
	return nil, g.call(name)
}

func (g *gateAdapter) Update(_ context.Context, name string, _ ir.Resource, _ state.Resource, _ func(Event)) (map[string]any, error) {
	return nil, g.call(name)
}

func (g *gateAdapter) Delete(_ context.Context, name string, _ state.Resource, _ func(Event)) error {
	return g.call(name)
}

func (g *gateAdapter) call(name string) error {
	g.entered <- name
	if err := g.errs[name]; err != nil {
		return err
	}
	<-g.release
	return nil
}

// receiveN reads a single name from entered, failing the test on timeout.
func receiveN(t *testing.T, g *gateAdapter, want []string) {
	t.Helper()
	for _, w := range want {
		select {
		case got := <-g.entered:
			if got != w {
				t.Fatalf("adapter entered with %q, want %q", got, w)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("timed out waiting for %q to enter its adapter call", w)
		}
	}
}

// receiveSet reads names from entered until every name in want has been
// seen, in any order. Use it when several changes are launched in the same
// round: the launch is deterministic but their goroutines race to report.
func receiveSet(t *testing.T, g *gateAdapter, want ...string) {
	t.Helper()
	left := map[string]bool{}
	for _, w := range want {
		left[w] = true
	}
	for len(left) > 0 {
		select {
		case got := <-g.entered:
			if !left[got] {
				t.Fatalf("adapter entered with %q, want one of %v", got, want)
			}
			delete(left, got)
		case <-time.After(5 * time.Second):
			t.Fatalf("timed out waiting for %v to enter their adapter calls", want)
		}
	}
}

// assertNotEntered fails if any name reaches the adapter within the window.
// Used only to prove a change that must wait does not start: the dispatcher
// cannot launch a waiting change at all until its dependency finishes, so
// the window never elapses when the scheduler is correct.
func assertNotEntered(t *testing.T, g *gateAdapter) {
	t.Helper()
	select {
	case got := <-g.entered:
		t.Fatalf("%q entered its adapter call, want it to stay waiting", got)
	case <-time.After(200 * time.Millisecond):
	}
}

// gateEngine builds an engine (with the fake synth) around a gateAdapter.
func gateEngine(t *testing.T, desired *ir.Document, current *state.State, gate *gateAdapter) (*Engine, *fakeStore) {
	t.Helper()
	if current == nil {
		current = state.New()
	}
	registry := NewRegistry(resolveOne("docker", gate))
	store := &fakeStore{st: current}
	e := New(Options{Synth: fakeSynth(t, desired), Store: store, Lock: &fakeLocker{}, Registry: registry})
	return e, store
}

// applyAsync runs e.Apply in a goroutine and returns its error channel.
func applyAsync(e *Engine, plan *diff.Plan) <-chan error {
	done := make(chan error, 1)
	go func() {
		_, err := e.Apply(context.Background(), plan)
		done <- err
	}()
	return done
}

// TestEngineApplyIndependentCreatesOverlap proves two resources with no
// dependency between them are inside their adapter calls at the same time
// (acceptance 1). The dispatcher launches both before either finishes.
func TestEngineApplyIndependentCreatesOverlap(t *testing.T) {
	desired := &ir.Document{Project: "p", Resources: map[string]ir.Resource{
		"a": {Type: "container", Plugin: "docker", Properties: map[string]any{}, DependsOn: []string{}},
		"b": {Type: "container", Plugin: "docker", Properties: map[string]any{}, DependsOn: []string{}},
	}}
	gate := newGateAdapter(nil)
	e, _ := gateEngine(t, desired, state.New(), gate)
	plan, err := e.Plan(context.Background(), entryDir)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	done := applyAsync(e, plan)

	// Both creates must be running simultaneously: each blocks inside the
	// adapter until release, so receiving both names proves overlap.
	receiveSet(t, gate, "a", "b")
	close(gate.release)
	if err := <-done; err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
}

// TestEngineApplyDependentCreateWaits proves a create whose DependsOn is
// still in flight does not start until its dependency finishes (acceptance
// 2). Blocking b while watching a proves the wait is real.
func TestEngineApplyDependentCreateWaits(t *testing.T) {
	desired := &ir.Document{Project: "p", Resources: map[string]ir.Resource{
		"a": {Type: "container", Plugin: "docker", Properties: map[string]any{}, DependsOn: []string{"b"}},
		"b": {Type: "container", Plugin: "docker", Properties: map[string]any{}, DependsOn: []string{}},
	}}
	gate := newGateAdapter(nil)
	e, _ := gateEngine(t, desired, state.New(), gate)
	plan, err := e.Plan(context.Background(), entryDir)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	done := applyAsync(e, plan)

	receiveN(t, gate, []string{"b"}) // b starts immediately
	assertNotEntered(t, gate)        // a must not start while b is blocked
	close(gate.release)              // b finishes
	receiveN(t, gate, []string{"a"}) // only now may a start
	if err := <-done; err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
}

// TestEngineApplyFailureStopsNewLaunches proves the failure semantics
// (acceptance 3): with the pool full, a fast failure of a stops the launch
// of the deferred change e; in-flight b/d finish and are saved, in-flight c
// fails and is saved tainted; both failures are joined into the error.
func TestEngineApplyFailureStopsNewLaunches(t *testing.T) {
	boomA, boomC := errors.New("boomA"), errors.New("boomC")
	desired := &ir.Document{Project: "p", Resources: map[string]ir.Resource{
		"a": {Type: "container", Plugin: "docker", Properties: map[string]any{}, DependsOn: []string{}},
		"b": {Type: "container", Plugin: "docker", Properties: map[string]any{}, DependsOn: []string{}},
		"c": {Type: "container", Plugin: "docker", Properties: map[string]any{}, DependsOn: []string{}},
		"d": {Type: "container", Plugin: "docker", Properties: map[string]any{}, DependsOn: []string{}},
		"e": {Type: "container", Plugin: "docker", Properties: map[string]any{}, DependsOn: []string{}},
	}}
	// maxParallel is 4: a,b,c,d fill the pool; e waits for a slot.
	gate := newGateAdapter(map[string]error{"a": boomA, "c": boomC})
	e, store := gateEngine(t, desired, state.New(), gate)
	plan, err := e.Plan(context.Background(), entryDir)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	done := applyAsync(e, plan)

	receiveSet(t, gate, "a", "b", "c", "d")
	assertNotEntered(t, gate) // e deferred: pool full, then a's failure stops launches
	close(gate.release)       // in-flight b, c, d may now finish
	err = <-done
	if err == nil || !errors.Is(err, boomA) || !errors.Is(err, boomC) {
		t.Fatalf("Apply() error = %v, want both %v and %v joined", err, boomA, boomC)
	}
	// e must never have started, even after its slot would have freed.
	select {
	case got := <-gate.entered:
		t.Fatalf("%q applied after the first failure", got)
	default:
	}

	if len(store.saves) != 4 {
		t.Fatalf("saves = %d, want 4 (every attempt, including failures)", len(store.saves))
	}
	final := store.saves[len(store.saves)-1]
	for _, name := range []string{"a", "b", "c", "d"} {
		if _, ok := final.Resources[name]; !ok {
			t.Fatalf("final state missing %q: %+v", name, final.Resources)
		}
	}
	if _, ok := final.Resources["e"]; ok {
		t.Fatal("e recorded in state although it never applied")
	}
	if !final.Resources["a"].Tainted || !final.Resources["c"].Tainted {
		t.Fatalf("a/c = %+v/%+v, want both tainted", final.Resources["a"], final.Resources["c"])
	}
	if final.Resources["b"].Tainted || final.Resources["d"].Tainted {
		t.Fatalf("b/d = %+v/%+v, want not tainted", final.Resources["b"], final.Resources["d"])
	}
}

// TestEngineApplyDeletesWaitForCreates proves pure deletes still run after
// every create/update (acceptance 4): old's delete must not start while
// independent creates a and b are still in flight.
func TestEngineApplyDeletesWaitForCreates(t *testing.T) {
	current := state.New()
	current.Project = "p"
	current.Resources["old"] = state.Resource{Type: "container", Plugin: "docker", Properties: map[string]any{}}
	desired := &ir.Document{Project: "p", Resources: map[string]ir.Resource{
		"a": {Type: "container", Plugin: "docker", Properties: map[string]any{}, DependsOn: []string{}},
		"b": {Type: "container", Plugin: "docker", Properties: map[string]any{}, DependsOn: []string{}},
	}}
	gate := newGateAdapter(nil)
	e, store := gateEngine(t, desired, current, gate)
	plan, err := e.Plan(context.Background(), entryDir)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	done := applyAsync(e, plan)

	receiveSet(t, gate, "a", "b")
	assertNotEntered(t, gate) // the delete must wait for both creates
	close(gate.release)
	receiveN(t, gate, []string{"old"})
	if err := <-done; err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if _, ok := store.saves[len(store.saves)-1].Resources["old"]; ok {
		t.Fatal("old still present after its delete")
	}
}

// replaceGate is the gateAdapter for a replace pair, where the delete and
// the create share one resource name, so their entries need distinguishing.
type replaceGate struct {
	deleteEntered chan struct{}
	createEntered chan struct{}
	release       chan struct{}
}

func (g *replaceGate) Delete(_ context.Context, _ string, _ state.Resource, _ func(Event)) error {
	g.deleteEntered <- struct{}{}
	<-g.release
	return nil
}

func (g *replaceGate) Create(_ context.Context, _ string, _ ir.Resource, _ func(Event)) (map[string]any, error) {
	g.createEntered <- struct{}{}
	return nil, nil
}

func (g *replaceGate) Update(_ context.Context, _ string, _ ir.Resource, _ state.Resource, _ func(Event)) (map[string]any, error) {
	return nil, nil
}

// TestEngineApplyReplaceCreateWaitsOnDelete proves the create half of a
// replace pair (a type change) does not start until its delete finishes.
func TestEngineApplyReplaceCreateWaitsOnDelete(t *testing.T) {
	current := state.New()
	current.Project = "p"
	current.Resources["x"] = state.Resource{Type: "volume", Plugin: "docker", Properties: map[string]any{}}
	desired := &ir.Document{Project: "p", Resources: map[string]ir.Resource{
		"x": {Type: "container", Plugin: "docker", Properties: map[string]any{}, DependsOn: []string{}},
	}}
	gate := &replaceGate{
		deleteEntered: make(chan struct{}, 1),
		createEntered: make(chan struct{}, 1),
		release:       make(chan struct{}),
	}
	registry := NewRegistry(resolveOne("docker", gate))
	e := New(Options{
		Synth:    fakeSynth(t, desired),
		Store:    &fakeStore{st: current},
		Lock:     &fakeLocker{},
		Registry: registry,
	})
	plan, err := e.Plan(context.Background(), entryDir)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if len(plan.Changes) != 2 || plan.Changes[0].Action != diff.ActionDelete || plan.Changes[1].Action != diff.ActionCreate {
		t.Fatalf("plan changes = %+v, want a delete+create replace pair", plan.Changes)
	}
	done := applyAsync(e, plan)

	select {
	case <-gate.deleteEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the replace delete to start")
	}
	select {
	case <-gate.createEntered:
		t.Fatal("replace create started before its delete finished")
	case <-time.After(200 * time.Millisecond):
	}
	close(gate.release)
	select {
	case <-gate.createEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the replace create after its delete")
	}
	if err := <-done; err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
}

// TestEngineApplyDeletesReverseDependency proves a pure delete waits on the
// delete of a resource that depends on it: svc must go before the net it
// depended on, matching today's reverse delete order.
func TestEngineApplyDeletesReverseDependency(t *testing.T) {
	current := state.New()
	current.Project = "p"
	current.Resources["net"] = state.Resource{Type: "network", Plugin: "docker", Properties: map[string]any{}, DependsOn: []string{}}
	current.Resources["svc"] = state.Resource{
		Type: "container", Plugin: "docker", Properties: map[string]any{}, DependsOn: []string{"net"},
	}
	desired := &ir.Document{Project: "p", Resources: map[string]ir.Resource{}}
	gate := newGateAdapter(nil)
	e, _ := gateEngine(t, desired, current, gate)
	plan, err := e.Plan(context.Background(), entryDir)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	done := applyAsync(e, plan)

	receiveN(t, gate, []string{"svc"}) // the dependent deletes first
	assertNotEntered(t, gate)          // net must wait for svc
	close(gate.release)
	receiveN(t, gate, []string{"net"})
	if err := <-done; err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
}

// TestEngineApplyPlanChangeCycle proves a dependency cycle among the
// scheduled changes (only constructible by hand — diff.Compute rejects
// cycles in the resource DAG) fails instead of deadlocking the dispatcher.
func TestEngineApplyPlanChangeCycle(t *testing.T) {
	plan := &diff.Plan{
		Project: "p", BaseSerial: 0, Dir: entryDir,
		Changes: []diff.Change{
			{Action: diff.ActionDelete, Name: "a", Type: "t", Plugin: "docker", Current: &state.Resource{DependsOn: []string{"b"}}},
			{Action: diff.ActionDelete, Name: "b", Type: "t", Plugin: "docker", Current: &state.Resource{DependsOn: []string{"a"}}},
		},
	}
	e := New(Options{Store: &fakeStore{st: state.New()}, Lock: &fakeLocker{}, Registry: NewRegistry(resolveNone())})
	_, err := e.Apply(context.Background(), plan)
	if err == nil || !strings.Contains(err.Error(), "dependency cycle") {
		t.Fatalf("Apply() error = %v, want a dependency-cycle error", err)
	}
}
