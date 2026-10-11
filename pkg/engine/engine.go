// Package engine orchestrates synth, diff, and apply: the dumb coordinator
// that drives Adapter implementations, in-process for now (SPEC §2).
package engine

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/freality/dexter/pkg/engine/diff"
	"github.com/freality/dexter/pkg/engine/ir"
	"github.com/freality/dexter/pkg/engine/state"
	"github.com/freality/dexter/pkg/jsenv"
)

// maxParallel bounds how many changes apply at once (W4b).
// ponytail: fixed worker limit; expose a --parallelism flag when real
// workloads outgrow it.
const maxParallel = 4

// Locker guards concurrent applies. An interface rather than *state.Lock so
// the acquire-failure path is testable.
type Locker interface {
	Acquire() error
	Release() error
}

// Options configures an Engine.
type Options struct {
	Synth    *jsenv.Synthesizer
	Store    state.Store
	Lock     Locker
	Registry *Registry
	Env      map[string]string
	// Events receives progress events during Apply; nil discards them.
	// It replaces the old Out writer (D11): a plain-line sink lives in the
	// CLI, and the TUI feeds on the same stream (W4c).
	Events func(Event)
}

// Engine drives Plan and Apply.
type Engine struct {
	synth    *jsenv.Synthesizer
	store    state.Store
	lock     Locker
	registry *Registry
	env      map[string]string
	events   func(Event)

	// stateMu serializes st mutation and store.Save across the concurrent
	// change workers (W4b): maps are racy to write in parallel even on
	// disjoint keys, and Save must observe a consistent snapshot.
	stateMu sync.Mutex
	// eventsMu serializes emit calls so Options.Events (the CLI sink) is
	// never invoked concurrently once Apply runs changes in parallel.
	eventsMu sync.Mutex
}

// New builds an Engine from Options.
func New(o Options) *Engine {
	return &Engine{
		synth:    o.Synth,
		store:    o.Store,
		lock:     o.Lock,
		registry: o.Registry,
		env:      o.Env,
		events:   o.Events,
	}
}

// SetEvents replaces the events callback. Used by the TUI to swap in its
// own sink before Apply. Safe to call concurrently with emit.
func (e *Engine) SetEvents(fn func(Event)) {
	e.eventsMu.Lock()
	e.events = fn
	e.eventsMu.Unlock()
}

// emit forwards one event to the Options.Events callback, if set. It is
// serialized: concurrent change workers (W4b) report through the same
// callback, and no sink is required to be goroutine-safe.
func (e *Engine) emit(ev Event) {
	if e.events == nil {
		return
	}
	e.eventsMu.Lock()
	defer e.eventsMu.Unlock()
	e.events(ev)
}

// Plan synthesizes dir's config (dex.config.ts plus dex/services/**) and
// diffs it against stored state. Read-only: no lock is taken.
func (e *Engine) Plan(ctx context.Context, dir string) (*diff.Plan, error) {
	data, err := e.synth.Synth(ctx, dir, e.env)
	if err != nil {
		return nil, fmt.Errorf("synthesize: %w", err)
	}
	desired, err := ir.Parse(data)
	if err != nil {
		return nil, err
	}

	key, err := loadOrCreateKey(dir)
	if err != nil {
		return nil, fmt.Errorf("secret key: %w", err)
	}
	if err = hashSecrets(desired, e.env, key); err != nil {
		return nil, err
	}

	current, err := e.store.Load()
	if err != nil {
		return nil, fmt.Errorf("load state: %w", err)
	}
	plan, err := diff.Compute(desired, current)
	if err != nil {
		return nil, err
	}
	plan.Dir = dir
	return plan, nil
}

// Apply executes a Plan previously returned by Plan. Changes run as a
// dependency graph (W4b): independent creates/updates overlap up to
// maxParallel, dependents wait on their DependsOn, replace creates wait on
// their own delete, and pure deletes wait for every create/update and for
// deletes of resources that depend on them. State is persisted after every
// change attempt (a failed create/update saves the resource tainted) so a
// mid-run failure leaves truthful state. On the first failure nothing new
// is launched; in-flight changes run to completion and all failures are
// joined into the returned error.
func (e *Engine) Apply(ctx context.Context, plan *diff.Plan) (_ *diff.Plan, err error) {
	if err = e.lock.Acquire(); err != nil {
		return nil, fmt.Errorf("acquire lock: %w", err)
	}
	defer func() {
		if relErr := e.lock.Release(); relErr != nil {
			err = errors.Join(err, relErr)
		}
	}()

	key, err := loadOrCreateKey(plan.Dir)
	if err != nil {
		return nil, fmt.Errorf("secret key: %w", err)
	}

	st, err := e.store.Load()
	if err != nil {
		return nil, fmt.Errorf("load state: %w", err)
	}
	if st.Serial != plan.BaseSerial {
		return nil, fmt.Errorf("state changed since plan was computed (serial %d, now %d); re-run dex plan", plan.BaseSerial, st.Serial)
	}
	if plan.Empty() {
		return plan, nil
	}
	st.Project = plan.Project

	if err := e.applyConcurrent(ctx, st, plan, key); err != nil {
		return nil, err
	}
	return plan, nil
}

// changeResult is one finished change: its plan index and error, if any.
type changeResult struct {
	idx int
	err error
}

// applyConcurrent runs plan.Changes as a dependency graph with at most
// maxParallel changes in flight, in plan order for determinism. Once a
// change fails, nothing new is launched; in-flight changes run to
// completion and every failure (including theirs, and a save error) is
// joined into the returned error.
func (e *Engine) applyConcurrent(ctx context.Context, st *state.State, plan *diff.Plan, key []byte) error {
	n := len(plan.Changes)
	deps := changeDeps(plan)

	done := make([]bool, n)
	launched := make([]bool, n)
	remaining := n
	running := 0
	stopped := false
	var failures []error

	results := make(chan changeResult, n)
	sem := make(chan struct{}, maxParallel)
	var wg sync.WaitGroup

	for remaining > 0 && !stopped {
		// Launch every ready change that fits in the pool, plan order.
		launchedAny := false
		for i := range n {
			if launched[i] || !depsReady(deps[i], done) {
				continue
			}
			select {
			case sem <- struct{}{}:
				launched[i] = true
				launchedAny = true
				running++
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					results <- changeResult{idx: i, err: e.runChange(ctx, st, plan.Changes[i], key)}
				}(i)
			default:
			}
		}
		if !launchedAny && running == 0 {
			// Nothing can ever finish (nothing is running and no dependency
			// is satisfiable): changeDeps is cyclic. diff.Compute rejects
			// cycles in the resource DAG, so this needs an inconsistent
			// hand-built plan or a corrupted state DependsOn to trigger.
			return fmt.Errorf("apply: dependency cycle among changes")
		}
		if !launchedAny {
			// Nothing ready or the pool is full: wait for a result. Each
			// consume here releases the change's pool slot, so a failure
			// observed now halts launches before any of its freed slots can
			// be reused (the worker never releases its own slot).
			res := <-results
			<-sem
			done[res.idx] = true
			remaining--
			running--
			if res.err != nil {
				failures = append(failures, res.err)
				stopped = true
			}
		}
	}

	// In-flight changes run to completion after a failure; collect theirs too.
	wg.Wait()
	close(results)
	for res := range results {
		<-sem
		if res.err != nil {
			failures = append(failures, res.err)
		}
	}
	if len(failures) > 0 {
		return errors.Join(failures...)
	}
	return nil
}

// runChange executes one change: emits Started/Done/Failed, applies it, and
// persists state under the state mutex — every attempt is saved, success
// and failure alike (a failed create/update is stored tainted).
func (e *Engine) runChange(ctx context.Context, st *state.State, change diff.Change, key []byte) error {
	e.emit(Event{Name: change.Name, Action: change.Action, Kind: EventStarted})
	next, applyErr := e.applyChange(ctx, change, key)
	if applyErr == nil {
		e.emit(Event{Name: change.Name, Action: change.Action, Kind: EventDone})
	} else {
		e.emit(Event{Name: change.Name, Action: change.Action, Kind: EventFailed, Message: applyErr.Error()})
	}

	e.stateMu.Lock()
	if next == nil && applyErr == nil {
		delete(st.Resources, change.Name) // successful delete
	} else if next != nil {
		st.Resources[change.Name] = *next
	}
	saveErr := e.store.Save(st)
	e.stateMu.Unlock()

	if saveErr != nil {
		return errors.Join(applyErr, fmt.Errorf("save state: %w", saveErr))
	}
	return applyErr
}

// changeDeps maps each change to the plan indexes it must wait for (W4b):
//   - a create/update of X waits on every change of each X.DependsOn;
//   - a replace pair's create waits on its own delete;
//   - pure deletes wait on every create and update (deletes last, as
//     today), and on delete changes of resources that depend on them
//     (dependent first, matching diff's reverse delete order).
func changeDeps(plan *diff.Plan) [][]int {
	n := len(plan.Changes)
	deps := make([][]int, n)

	byName := map[string][]int{}
	for i, c := range plan.Changes {
		byName[c.Name] = append(byName[c.Name], i)
	}

	for i, c := range plan.Changes {
		switch c.Action {
		case diff.ActionCreate, diff.ActionUpdate:
			for _, dep := range c.Desired.DependsOn {
				deps[i] = append(deps[i], byName[dep]...)
			}
		case diff.ActionDelete:
			if len(byName[c.Name]) == 1 { // pure delete, not a replace pair
				for j, d := range plan.Changes {
					if d.Action == diff.ActionCreate || d.Action == diff.ActionUpdate {
						deps[i] = append(deps[i], j)
					}
				}
			}
			for j, d := range plan.Changes {
				if j != i && d.Action == diff.ActionDelete && dependsOn(d, c.Name) {
					deps[i] = append(deps[i], j)
				}
			}
		}
	}

	// The create half of a replace pair waits on its own delete.
	for _, idxs := range byName {
		if len(idxs) != 2 {
			continue
		}
		var del, create int
		for _, j := range idxs {
			if plan.Changes[j].Action == diff.ActionDelete {
				del = j
			} else {
				create = j
			}
		}
		deps[create] = append(deps[create], del)
	}
	return deps
}

// depsReady reports whether every change i waits for has finished.
func depsReady(deps []int, done []bool) bool {
	for _, j := range deps {
		if !done[j] {
			return false
		}
	}
	return true
}

// dependsOn reports whether delete change d's resource depends on name.
// Delete changes carry the resource's persisted DependsOn (state mirrors IR
// deps at apply time), which is what rule 4 needs.
func dependsOn(d diff.Change, name string) bool {
	return slices.Contains(d.Current.DependsOn, name)
}

// applyChange runs one change against its adapter and returns the resource
// to persist — nil for a successful delete, which instead removes the name
// from state. It never touches st; the caller commits the result under the
// state mutex (W4b).
func (e *Engine) applyChange(ctx context.Context, change diff.Change, key []byte) (*state.Resource, error) {
	adapter, err := e.registry.Get(change.Plugin)
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", change.Action, change.Name, err)
	}

	// report lets the adapter stream Log/Progress events; the engine fills
	// in Name and Action so adapters never need to know them.
	report := func(ev Event) {
		ev.Name = change.Name
		ev.Action = change.Action
		e.emit(ev)
	}

	var outputs map[string]any
	var resolvedDesired map[string]any
	var resolvedCurrent map[string]any

	switch change.Action {
	case diff.ActionCreate:
		resolvedDesired, err = resolveSecrets(change.Desired.Properties, e.env, key)
		if err != nil {
			return nil, fmt.Errorf("%s %s: %w", change.Action, change.Name, err)
		}
		desiredCopy := *change.Desired
		desiredCopy.Properties = resolvedDesired
		outputs, err = adapter.Create(ctx, change.Name, desiredCopy, report)
	case diff.ActionUpdate:
		resolvedDesired, err = resolveSecrets(change.Desired.Properties, e.env, key)
		if err != nil {
			return nil, fmt.Errorf("%s %s: %w", change.Action, change.Name, err)
		}
		resolvedCurrent, _ = resolveSecretsWithCurrent(change.Current.Properties, e.env, key)
		desiredCopy := *change.Desired
		desiredCopy.Properties = resolvedDesired
		currentCopy := *change.Current
		currentCopy.Properties = resolvedCurrent
		outputs, err = adapter.Update(ctx, change.Name, desiredCopy, currentCopy, report)
	case diff.ActionDelete:
		if err = adapter.Delete(ctx, change.Name, *change.Current, report); err != nil {
			return nil, fmt.Errorf("%s %s: %w", change.Action, change.Name, err)
		}
		return nil, nil
	}

	// Leak guard: check that adapter outputs don't contain any resolved secret values.
	// Runs even when the adapter call itself already failed: a partial
	// outputs map returned alongside an error must not carry a leaked
	// secret into the tainted state entry below.
	if outputs != nil {
		resolvedAll := collectSecrets(change.Desired.Properties, e.env)
		if le := checkLeak(outputs, resolvedAll); le != nil {
			if err == nil {
				return nil, fmt.Errorf("%s %s: %w", change.Action, change.Name, le)
			}
			// Already failing: drop the leaked outputs rather than persist
			// them, and fold the leak into the reported error.
			outputs = nil
			err = errors.Join(err, le)
		}
	}

	res := &state.Resource{
		Type: change.Desired.Type, Plugin: change.Desired.Plugin,
		Properties: change.Desired.Properties, DependsOn: change.Desired.DependsOn,
		Outputs: outputs, Tainted: err != nil,
	}
	if err != nil {
		return res, fmt.Errorf("%s %s: %w", change.Action, change.Name, err)
	}
	return res, nil
}

// collectSecrets walks props for secret tokens and returns a map of env key
// to plaintext for every token found. Used by the leak guard.
func collectSecrets(props map[string]any, env map[string]string) map[string]string {
	out := map[string]string{}
	collectSecretsWalk(props, env, out)
	return out
}

func collectSecretsWalk(props map[string]any, env map[string]string, out map[string]string) {
	// Check if props itself is a secret token.
	if t, _ := props["$dex_type"].(string); t == "secret" {
		if key, _ := props["key"].(string); key != "" {
			if pt, inEnv := env[key]; inEnv {
				out[key] = pt
			}
		}
		return
	}

	for _, v := range props {
		switch val := v.(type) {
		case map[string]any:
			collectSecretsWalk(val, env, out)
		case []any:
			for _, item := range val {
				if m, ok := item.(map[string]any); ok {
					collectSecretsWalk(m, env, out)
				}
			}
		}
	}
}
