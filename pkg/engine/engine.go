// Package engine orchestrates synth, diff, and apply: the dumb coordinator
// that drives Adapter implementations, in-process for now (SPEC §2).
package engine

import (
	"context"
	"errors"
	"fmt"

	"github.com/freality/dexter/pkg/engine/diff"
	"github.com/freality/dexter/pkg/engine/ir"
	"github.com/freality/dexter/pkg/engine/state"
	"github.com/freality/dexter/pkg/jsenv"
)

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

// emit forwards one event to the Options.Events callback, if set.
func (e *Engine) emit(ev Event) {
	if e.events != nil {
		e.events(ev)
	}
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

// Apply executes a Plan previously returned by Plan, persisting state after
// every change attempt (a failed create/update saves the resource tainted)
// so a mid-run failure leaves truthful state.
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

	for _, change := range plan.Changes {
		e.emit(Event{Name: change.Name, Action: change.Action, Kind: EventStarted})
		applyErr := e.applyChange(ctx, st, change, key)
		if applyErr == nil {
			e.emit(Event{Name: change.Name, Action: change.Action, Kind: EventDone})
		} else {
			e.emit(Event{Name: change.Name, Action: change.Action, Kind: EventFailed, Message: applyErr.Error()})
		}
		if saveErr := e.store.Save(st); saveErr != nil {
			return nil, errors.Join(applyErr, fmt.Errorf("save state: %w", saveErr))
		}
		if applyErr != nil {
			return nil, applyErr
		}
	}
	return plan, nil
}

// applyChange runs one change against its adapter and updates st in place.
func (e *Engine) applyChange(ctx context.Context, st *state.State, change diff.Change, key []byte) error {
	adapter, err := e.registry.Get(change.Plugin)
	if err != nil {
		return fmt.Errorf("%s %s: %w", change.Action, change.Name, err)
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
			return fmt.Errorf("%s %s: %w", change.Action, change.Name, err)
		}
		desiredCopy := *change.Desired
		desiredCopy.Properties = resolvedDesired
		outputs, err = adapter.Create(ctx, change.Name, desiredCopy, report)
	case diff.ActionUpdate:
		resolvedDesired, err = resolveSecrets(change.Desired.Properties, e.env, key)
		if err != nil {
			return fmt.Errorf("%s %s: %w", change.Action, change.Name, err)
		}
		resolvedCurrent, _ = resolveSecretsWithCurrent(change.Current.Properties, e.env, key)
		desiredCopy := *change.Desired
		desiredCopy.Properties = resolvedDesired
		currentCopy := *change.Current
		currentCopy.Properties = resolvedCurrent
		outputs, err = adapter.Update(ctx, change.Name, desiredCopy, currentCopy, report)
	case diff.ActionDelete:
		if err = adapter.Delete(ctx, change.Name, *change.Current, report); err != nil {
			return fmt.Errorf("%s %s: %w", change.Action, change.Name, err)
		}
		delete(st.Resources, change.Name)
		return nil
	}

	// Leak guard: check that adapter outputs don't contain any resolved secret values.
	if err == nil && outputs != nil {
		resolvedAll := collectSecrets(change.Desired.Properties, e.env)
		if le := checkLeak(outputs, resolvedAll); le != nil {
			return fmt.Errorf("%s %s: %w", change.Action, change.Name, le)
		}
	}

	st.Resources[change.Name] = state.Resource{
		Type: change.Desired.Type, Plugin: change.Desired.Plugin,
		Properties: change.Desired.Properties, DependsOn: change.Desired.DependsOn,
		Outputs: outputs, Tainted: err != nil,
	}
	if err != nil {
		return fmt.Errorf("%s %s: %w", change.Action, change.Name, err)
	}
	return nil
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
