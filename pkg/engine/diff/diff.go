// Package diff computes the set of changes needed to move state.State to
// match an ir.Document.
package diff

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"

	"github.com/freality/dexter/pkg/engine/dag"
	"github.com/freality/dexter/pkg/engine/ir"
	"github.com/freality/dexter/pkg/engine/state"
)

// Action is the kind of change applied to one resource.
type Action string

const (
	ActionCreate Action = "create"
	ActionUpdate Action = "update"
	ActionDelete Action = "delete"
)

// PropChange is one changed property within an ActionUpdate.
type PropChange struct {
	Key      string
	Old, New any
}

// Change is one step in a Plan.
type Change struct {
	Action  Action
	Name    string
	Type    string
	Plugin  string
	Desired *ir.Resource    // nil for delete
	Current *state.Resource // nil for create
	Props   []PropChange    // update only, sorted by Key
}

// Plan is an ordered list of changes.
type Plan struct {
	Changes []Change
	// Project and BaseSerial identify the desired/current pair this Plan was
	// computed from, so Engine.Apply can detect state that moved underneath
	// a confirmed plan instead of silently applying it against new state.
	Project    string
	BaseSerial int
}

// Counts returns the number of create/update/delete changes.
func (p *Plan) Counts() (add, change, destroy int) {
	for _, c := range p.Changes {
		switch c.Action {
		case ActionCreate:
			add++
		case ActionUpdate:
			change++
		case ActionDelete:
			destroy++
		}
	}
	return add, change, destroy
}

// Empty reports whether the plan has no changes.
func (p *Plan) Empty() bool {
	return len(p.Changes) == 0
}

// Compute diffs desired against current: creates/updates (and replacement
// delete+create pairs) in dependency order, then pure deletes last in
// reverse dependency order.
func Compute(desired *ir.Document, current *state.State) (*Plan, error) {
	g := dag.New()
	for _, name := range desired.Names() {
		g.AddNode(name)
	}
	for _, name := range desired.Names() {
		for _, dep := range desired.Resources[name].DependsOn {
			// IR validation already guarantees known targets; a failure here
			// means that guarantee regressed.
			if err := g.AddEdge(name, dep); err != nil {
				return nil, fmt.Errorf("build dependency graph: %w", err)
			}
		}
	}
	order, err := g.Sort()
	if err != nil {
		return nil, err
	}

	plan := &Plan{Project: desired.Project, BaseSerial: current.Serial}
	for _, name := range order {
		des := desired.Resources[name]
		cur, inCurrent := current.Resources[name]
		if !inCurrent {
			plan.Changes = append(plan.Changes, Change{
				Action: ActionCreate, Name: name, Type: des.Type, Plugin: des.Plugin,
				Desired: &des,
			})
			continue
		}
		// A tainted resource's target may not match its saved properties, so
		// it is replaced even when the properties are unchanged.
		if des.Type != cur.Type || des.Plugin != cur.Plugin || cur.Tainted {
			plan.Changes = append(plan.Changes,
				Change{Action: ActionDelete, Name: name, Type: cur.Type, Plugin: cur.Plugin, Current: &cur},
				Change{Action: ActionCreate, Name: name, Type: des.Type, Plugin: des.Plugin, Desired: &des},
			)
			continue
		}
		props := propChanges(cur.Properties, des.Properties)
		if len(props) > 0 {
			plan.Changes = append(plan.Changes, Change{
				Action: ActionUpdate, Name: name, Type: des.Type, Plugin: des.Plugin,
				Desired: &des, Current: &cur, Props: props,
			})
		}
	}

	names := current.Names()
	toDelete := map[string]state.Resource{}
	for _, name := range names {
		if _, inDesired := desired.Resources[name]; !inDesired {
			toDelete[name] = current.Resources[name]
		}
	}

	// Order deletes by dependency, not by name: a dependent (e.g. a
	// container) must go before what it depends on (e.g. its network), the
	// reverse of create order. dg mirrors only edges between two resources
	// that are both being deleted; a dependency left in desired imposes no
	// delete-order constraint.
	dg := dag.New()
	for _, name := range names {
		if _, ok := toDelete[name]; ok {
			dg.AddNode(name)
		}
	}
	for _, name := range names {
		res, ok := toDelete[name]
		if !ok {
			continue
		}
		for _, dep := range res.DependsOn {
			if _, ok := toDelete[dep]; ok {
				// name and dep were both just added as nodes above (both
				// keys of toDelete), so AddEdge's unknown-node error cannot
				// occur; nothing to wrap or guard.
				_ = dg.AddEdge(name, dep)
			}
		}
	}
	deleteOrder, err := dg.Sort()
	if err != nil {
		return nil, err
	}
	for i := len(deleteOrder) - 1; i >= 0; i-- {
		name := deleteOrder[i]
		cur := toDelete[name]
		plan.Changes = append(plan.Changes, Change{
			Action: ActionDelete, Name: name, Type: cur.Type, Plugin: cur.Plugin, Current: &cur,
		})
	}

	return plan, nil
}

// propChanges compares normalized properties and returns differing keys
// sorted by name. A key missing from one side reports a nil Old or New.
func propChanges(oldProps, newProps map[string]any) []PropChange {
	oldN := normalize(oldProps).(map[string]any)
	newN := normalize(newProps).(map[string]any)

	keys := make(map[string]struct{}, len(oldN)+len(newN))
	for k := range oldN {
		keys[k] = struct{}{}
	}
	for k := range newN {
		keys[k] = struct{}{}
	}

	var changes []PropChange
	for k := range keys {
		ov, oOK := oldN[k]
		nv, nOK := newN[k]
		if oOK && nOK && reflect.DeepEqual(ov, nv) {
			continue
		}
		changes = append(changes, PropChange{Key: k, Old: ov, New: nv})
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].Key < changes[j].Key })
	return changes
}

// normalize round-trips v through JSON so Go-literal values (e.g. int 1)
// compare equal to their JSON-decoded equivalents (float64 1.0).
//
// Inputs always originate from parsed IR or persisted state, both already
// JSON-safe, so a marshal/unmarshal failure here cannot occur; errors are
// ignored rather than guarded with an unreachable branch.
func normalize(v any) any {
	data, _ := json.Marshal(v)
	var out any
	_ = json.Unmarshal(data, &out)
	return out
}
