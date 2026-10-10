// Package engine orchestrates synth, diff, and apply: the dumb coordinator
// that drives Adapter implementations, in-process for now (SPEC §2).
package engine

import (
	"context"
	"errors"
	"fmt"
	"io"

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
	Out      io.Writer // progress lines during Apply; nil => io.Discard
}

// Engine drives Plan and Apply.
type Engine struct {
	synth    *jsenv.Synthesizer
	store    state.Store
	lock     Locker
	registry *Registry
	env      map[string]string
	out      io.Writer
}

// New builds an Engine from Options.
func New(o Options) *Engine {
	out := o.Out
	if out == nil {
		out = io.Discard
	}
	return &Engine{
		synth:    o.Synth,
		store:    o.Store,
		lock:     o.Lock,
		registry: o.Registry,
		env:      o.Env,
		out:      out,
	}
}

// Plan synthesizes entrypoint and diffs it against stored state. Read-only:
// no lock is taken.
func (e *Engine) Plan(ctx context.Context, entrypoint string) (*diff.Plan, error) {
	data, err := e.synth.Synth(ctx, entrypoint, e.env)
	if err != nil {
		return nil, fmt.Errorf("synthesize: %w", err)
	}
	// ir.Parse already wraps its own JSON-decode failures and returns a
	// self-describing *ValidationError otherwise; nothing to add here.
	desired, err := ir.Parse(data)
	if err != nil {
		return nil, err
	}
	current, err := e.store.Load()
	if err != nil {
		return nil, fmt.Errorf("load state: %w", err)
	}
	return diff.Compute(desired, current)
}

// Apply executes a Plan previously returned by Plan, persisting state after
// every change attempt (a failed create/update saves the resource tainted)
// so a mid-run failure leaves truthful state. It
// never re-synthesizes or re-diffs: the plan a caller confirmed is the plan
// that runs. If state moved since Plan computed it (another dex process, a
// manual edit), Apply fails rather than silently applying stale changes
// against resources it never diffed.
func (e *Engine) Apply(ctx context.Context, plan *diff.Plan) (_ *diff.Plan, err error) {
	if err := e.lock.Acquire(); err != nil {
		return nil, fmt.Errorf("acquire lock: %w", err)
	}
	defer func() {
		if relErr := e.lock.Release(); relErr != nil {
			err = errors.Join(err, relErr)
		}
	}()

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
		// Save after every attempt, failed ones too: a failure may have
		// tainted the resource, and state must say so.
		applyErr := e.applyChange(ctx, st, change)
		if applyErr == nil {
			fmt.Fprintf(e.out, "%s: done\n", change.Name)
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
// A failed create or update records the resource as tainted: the adapter may
// have changed the target part-way, so the next plan replaces it.
func (e *Engine) applyChange(ctx context.Context, st *state.State, change diff.Change) error {
	adapter, err := e.registry.Get(change.Plugin)
	if err != nil {
		return fmt.Errorf("%s %s: %w", change.Action, change.Name, err)
	}

	var outputs map[string]any
	switch change.Action {
	case diff.ActionCreate:
		outputs, err = adapter.Create(ctx, change.Name, *change.Desired)
	case diff.ActionUpdate:
		outputs, err = adapter.Update(ctx, change.Name, *change.Desired, *change.Current)
	case diff.ActionDelete:
		if err := adapter.Delete(ctx, change.Name, *change.Current); err != nil {
			return fmt.Errorf("%s %s: %w", change.Action, change.Name, err)
		}
		delete(st.Resources, change.Name)
		return nil
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
