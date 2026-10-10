// Package engine orchestrates synth, diff, and apply: the dumb coordinator
// that drives adapters through gRPC-free Provider implementations (SPEC §2).
package engine

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/freality/dexter/pkg/engine/diff"
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
	desired, err := e.synth.Synth(ctx, entrypoint, e.env)
	if err != nil {
		return nil, err
	}
	current, err := e.store.Load()
	if err != nil {
		return nil, err
	}
	return diff.Compute(desired, current)
}

// Apply plans entrypoint and executes the resulting changes in order,
// persisting state after each successful change so a mid-run failure
// leaves truthful state.
func (e *Engine) Apply(ctx context.Context, entrypoint string) (plan *diff.Plan, err error) {
	if err := e.lock.Acquire(); err != nil {
		return nil, err
	}
	defer func() {
		if relErr := e.lock.Release(); relErr != nil {
			err = errors.Join(err, relErr)
		}
	}()

	desired, err := e.synth.Synth(ctx, entrypoint, e.env)
	if err != nil {
		return nil, err
	}
	st, err := e.store.Load()
	if err != nil {
		return nil, err
	}
	plan, err = diff.Compute(desired, st)
	if err != nil {
		return nil, err
	}
	if plan.Empty() {
		return plan, nil
	}
	st.Project = desired.Project

	for _, change := range plan.Changes {
		if err = e.applyChange(ctx, st, change); err != nil {
			return nil, err
		}
		fmt.Fprintf(e.out, "%s: done\n", change.Name)
		if err = e.store.Save(st); err != nil {
			return nil, fmt.Errorf("save state: %w", err)
		}
	}
	return plan, nil
}

// applyChange runs one change against its provider and updates st in place.
func (e *Engine) applyChange(ctx context.Context, st *state.State, change diff.Change) error {
	provider, err := e.registry.Get(change.Plugin)
	if err != nil {
		return fmt.Errorf("%s %s: %w", change.Action, change.Name, err)
	}

	switch change.Action {
	case diff.ActionCreate:
		outputs, err := provider.Create(ctx, change.Name, *change.Desired)
		if err != nil {
			return fmt.Errorf("%s %s: %w", change.Action, change.Name, err)
		}
		st.Resources[change.Name] = state.Resource{
			Type: change.Desired.Type, Plugin: change.Desired.Plugin,
			Properties: change.Desired.Properties, Outputs: outputs,
		}
	case diff.ActionUpdate:
		outputs, err := provider.Update(ctx, change.Name, *change.Desired, *change.Current)
		if err != nil {
			return fmt.Errorf("%s %s: %w", change.Action, change.Name, err)
		}
		st.Resources[change.Name] = state.Resource{
			Type: change.Desired.Type, Plugin: change.Desired.Plugin,
			Properties: change.Desired.Properties, Outputs: outputs,
		}
	case diff.ActionDelete:
		if err := provider.Delete(ctx, change.Name, *change.Current); err != nil {
			return fmt.Errorf("%s %s: %w", change.Action, change.Name, err)
		}
		delete(st.Resources, change.Name)
	}
	return nil
}
