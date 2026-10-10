package diff

import (
	"errors"
	"strings"
	"testing"

	"github.com/freality/dexter/pkg/engine/dag"
	"github.com/freality/dexter/pkg/engine/ir"
	"github.com/freality/dexter/pkg/engine/state"
)

func doc(resources map[string]ir.Resource) *ir.Document {
	return &ir.Document{Project: "p", Resources: resources}
}

func st(resources map[string]state.Resource) *state.State {
	return &state.State{Project: "p", Resources: resources}
}

func TestComputeCreate(t *testing.T) {
	d := doc(map[string]ir.Resource{
		"a": {Type: "container", Plugin: "docker", Properties: map[string]any{}, DependsOn: []string{}},
	})
	plan, err := Compute(d, st(map[string]state.Resource{}))
	if err != nil {
		t.Fatalf("Compute() error = %v", err)
	}
	if len(plan.Changes) != 1 || plan.Changes[0].Action != ActionCreate || plan.Changes[0].Name != "a" {
		t.Fatalf("Changes = %+v", plan.Changes)
	}
	if plan.Changes[0].Desired == nil || plan.Changes[0].Current != nil {
		t.Fatalf("Changes[0] = %+v", plan.Changes[0])
	}
	add, change, destroy := plan.Counts()
	if add != 1 || change != 0 || destroy != 0 {
		t.Fatalf("Counts() = %d,%d,%d", add, change, destroy)
	}
	if plan.Empty() {
		t.Fatal("Empty() = true, want false")
	}
}

func TestComputePureDelete(t *testing.T) {
	plan, err := Compute(doc(nil), st(map[string]state.Resource{
		"old": {Type: "container", Plugin: "docker"},
	}))
	if err != nil {
		t.Fatalf("Compute() error = %v", err)
	}
	if len(plan.Changes) != 1 || plan.Changes[0].Action != ActionDelete || plan.Changes[0].Name != "old" {
		t.Fatalf("Changes = %+v", plan.Changes)
	}
	if plan.Changes[0].Current == nil || plan.Changes[0].Desired != nil {
		t.Fatalf("Changes[0] = %+v", plan.Changes[0])
	}
}

func TestComputeNoOp(t *testing.T) {
	d := doc(map[string]ir.Resource{
		"a": {Type: "container", Plugin: "docker", Properties: map[string]any{"count": 1}, DependsOn: []string{}},
	})
	s := st(map[string]state.Resource{
		"a": {Type: "container", Plugin: "docker", Properties: map[string]any{"count": 1.0}}, // int vs float64
	})
	plan, err := Compute(d, s)
	if err != nil {
		t.Fatalf("Compute() error = %v", err)
	}
	if !plan.Empty() {
		t.Fatalf("Changes = %+v, want no-op", plan.Changes)
	}
}

func TestComputePropAddRemoveChange(t *testing.T) {
	d := doc(map[string]ir.Resource{
		"a": {
			Type: "container", Plugin: "docker", DependsOn: []string{},
			Properties: map[string]any{"image": "v2", "added": "x"},
		},
	})
	s := st(map[string]state.Resource{
		"a": {
			Type: "container", Plugin: "docker",
			Properties: map[string]any{"image": "v1", "removed": "y"},
		},
	})
	plan, err := Compute(d, s)
	if err != nil {
		t.Fatalf("Compute() error = %v", err)
	}
	if len(plan.Changes) != 1 || plan.Changes[0].Action != ActionUpdate {
		t.Fatalf("Changes = %+v", plan.Changes)
	}
	props := plan.Changes[0].Props
	if len(props) != 3 {
		t.Fatalf("Props = %+v, want 3 entries", props)
	}
	// sorted by key: added, image, removed
	if props[0].Key != "added" || props[0].Old != nil || props[0].New != "x" {
		t.Fatalf("Props[0] = %+v", props[0])
	}
	if props[1].Key != "image" || props[1].Old != "v1" || props[1].New != "v2" {
		t.Fatalf("Props[1] = %+v", props[1])
	}
	if props[2].Key != "removed" || props[2].Old != "y" || props[2].New != nil {
		t.Fatalf("Props[2] = %+v", props[2])
	}

	add, change, destroy := plan.Counts()
	if add != 0 || change != 1 || destroy != 0 {
		t.Fatalf("Counts() = %d,%d,%d", add, change, destroy)
	}
}

func TestComputeTypeChangeReplaces(t *testing.T) {
	d := doc(map[string]ir.Resource{
		"a": {Type: "volume", Plugin: "docker", DependsOn: []string{}, Properties: map[string]any{}},
	})
	s := st(map[string]state.Resource{
		"a": {Type: "container", Plugin: "docker", Properties: map[string]any{}},
	})
	plan, err := Compute(d, s)
	if err != nil {
		t.Fatalf("Compute() error = %v", err)
	}
	if len(plan.Changes) != 2 {
		t.Fatalf("Changes = %+v, want delete+create pair", plan.Changes)
	}
	if plan.Changes[0].Action != ActionDelete || plan.Changes[0].Type != "container" {
		t.Fatalf("Changes[0] = %+v", plan.Changes[0])
	}
	if plan.Changes[1].Action != ActionCreate || plan.Changes[1].Type != "volume" {
		t.Fatalf("Changes[1] = %+v", plan.Changes[1])
	}
}

func TestComputePluginChangeReplaces(t *testing.T) {
	d := doc(map[string]ir.Resource{
		"a": {Type: "container", Plugin: "proxmox", DependsOn: []string{}, Properties: map[string]any{}},
	})
	s := st(map[string]state.Resource{
		"a": {Type: "container", Plugin: "docker", Properties: map[string]any{}},
	})
	plan, err := Compute(d, s)
	if err != nil {
		t.Fatalf("Compute() error = %v", err)
	}
	if len(plan.Changes) != 2 || plan.Changes[0].Action != ActionDelete || plan.Changes[1].Action != ActionCreate {
		t.Fatalf("Changes = %+v", plan.Changes)
	}
	add, change, destroy := plan.Counts()
	if add != 1 || change != 0 || destroy != 1 {
		t.Fatalf("Counts() = %d,%d,%d", add, change, destroy)
	}
}

func TestComputeDependencyOrdering(t *testing.T) {
	d := doc(map[string]ir.Resource{
		"c": {Type: "t", Plugin: "p", Properties: map[string]any{}, DependsOn: []string{"b"}},
		"a": {Type: "t", Plugin: "p", Properties: map[string]any{}, DependsOn: []string{}},
		"b": {Type: "t", Plugin: "p", Properties: map[string]any{}, DependsOn: []string{"a"}},
	})
	plan, err := Compute(d, st(map[string]state.Resource{}))
	if err != nil {
		t.Fatalf("Compute() error = %v", err)
	}
	if len(plan.Changes) != 3 {
		t.Fatalf("Changes = %+v", plan.Changes)
	}
	order := []string{plan.Changes[0].Name, plan.Changes[1].Name, plan.Changes[2].Name}
	want := []string{"a", "b", "c"}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order = %v, want %v", order, want)
		}
	}
}

func TestComputeDeletesInReverseOrder(t *testing.T) {
	// "alpha" depends on "zeta": alphabetical order would delete the
	// dependency (zeta) first, which breaks a real adapter (e.g. a
	// container deleted after the network it still references). Deletes
	// must go dependent-first, the reverse of create order, regardless of
	// name.
	plan, err := Compute(doc(nil), st(map[string]state.Resource{
		"alpha": {Type: "t", Plugin: "p", DependsOn: []string{"zeta"}},
		"zeta":  {Type: "t", Plugin: "p"},
	}))
	if err != nil {
		t.Fatalf("Compute() error = %v", err)
	}
	if len(plan.Changes) != 2 {
		t.Fatalf("Changes = %+v", plan.Changes)
	}
	order := []string{plan.Changes[0].Name, plan.Changes[1].Name}
	want := []string{"alpha", "zeta"}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order = %v, want %v", order, want)
		}
	}
}

// TestComputeDeleteCyclePassthrough exercises the delete-ordering DAG's
// cycle path. Only reachable with a corrupted state.json: a successful
// apply always persists an already-acyclic dependency subgraph.
func TestComputeDeleteCyclePassthrough(t *testing.T) {
	_, err := Compute(doc(nil), st(map[string]state.Resource{
		"a": {Type: "t", Plugin: "p", DependsOn: []string{"b"}},
		"b": {Type: "t", Plugin: "p", DependsOn: []string{"a"}},
	}))
	var ce *dag.CycleError
	if err == nil {
		t.Fatal("Compute() error = nil, want cycle error")
	}
	if !errors.As(err, &ce) {
		t.Fatalf("Compute() error = %v (%T), want *dag.CycleError", err, err)
	}
}

func TestComputeCyclePassthrough(t *testing.T) {
	d := doc(map[string]ir.Resource{
		"a": {Type: "t", Plugin: "p", Properties: map[string]any{}, DependsOn: []string{"b"}},
		"b": {Type: "t", Plugin: "p", Properties: map[string]any{}, DependsOn: []string{"a"}},
	})
	_, err := Compute(d, st(map[string]state.Resource{}))
	var ce *dag.CycleError
	if err == nil {
		t.Fatal("Compute() error = nil, want cycle error")
	}
	if ce2, ok := err.(*dag.CycleError); !ok {
		t.Fatalf("Compute() error = %v (%T), want *dag.CycleError", err, err)
	} else {
		ce = ce2
	}
	if ce.Error() != "dependency cycle: a -> b -> a" {
		t.Fatalf("Error() = %q", ce.Error())
	}
}

func TestComputeAddEdgeErrorPropagated(t *testing.T) {
	// Bypasses ir.Validate to simulate a validation regression: a
	// depends_on target that is not itself a key in Resources.
	d := &ir.Document{Project: "p", Resources: map[string]ir.Resource{
		"a": {Type: "t", Plugin: "p", Properties: map[string]any{}, DependsOn: []string{"ghost"}},
	}}
	_, err := Compute(d, st(map[string]state.Resource{}))
	if err == nil || !strings.Contains(err.Error(), "build dependency graph") {
		t.Fatalf("Compute() error = %v, want wrapped AddEdge error", err)
	}
}

func TestPlanEmptyTrue(t *testing.T) {
	p := &Plan{}
	if !p.Empty() {
		t.Fatal("Empty() = false, want true")
	}
	add, change, destroy := p.Counts()
	if add != 0 || change != 0 || destroy != 0 {
		t.Fatalf("Counts() = %d,%d,%d, want zeros", add, change, destroy)
	}
}

func TestComputeReplacesTaintedResource(t *testing.T) {
	d := doc(map[string]ir.Resource{
		"a": {Type: "container", Plugin: "docker", Properties: map[string]any{"image": "v1"}, DependsOn: []string{}},
	})
	plan, err := Compute(d, st(map[string]state.Resource{
		"a": {Type: "container", Plugin: "docker", Properties: map[string]any{"image": "v1"}, Tainted: true},
	}))
	if err != nil {
		t.Fatalf("Compute() error = %v", err)
	}
	if len(plan.Changes) != 2 || plan.Changes[0].Action != ActionDelete || plan.Changes[1].Action != ActionCreate {
		t.Fatalf("Changes = %+v, want delete+create replacing tainted a despite equal properties", plan.Changes)
	}
}
