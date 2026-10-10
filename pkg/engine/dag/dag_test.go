package dag

import (
	"reflect"
	"strings"
	"testing"
)

func mustAddEdge(t *testing.T, g *Graph, node, dep string) {
	t.Helper()
	if err := g.AddEdge(node, dep); err != nil {
		t.Fatalf("AddEdge(%q, %q) error = %v", node, dep, err)
	}
}

func TestSortEmptyGraph(t *testing.T) {
	g := New()
	got, err := g.Sort()
	if err != nil {
		t.Fatalf("Sort() error = %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("Sort() = %v, want empty", got)
	}
}

func TestSortChain(t *testing.T) {
	g := New()
	g.AddNode("a")
	g.AddNode("b")
	g.AddNode("c")
	mustAddEdge(t, g, "b", "a")
	mustAddEdge(t, g, "c", "b")

	got, err := g.Sort()
	if err != nil {
		t.Fatalf("Sort() error = %v", err)
	}
	want := []string{"a", "b", "c"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Sort() = %v, want %v", got, want)
	}
}

func TestSortDiamond(t *testing.T) {
	g := New()
	g.AddNode("a")
	g.AddNode("b")
	g.AddNode("c")
	g.AddNode("d")
	mustAddEdge(t, g, "b", "a")
	mustAddEdge(t, g, "c", "a")
	mustAddEdge(t, g, "d", "b")
	mustAddEdge(t, g, "d", "c")

	got, err := g.Sort()
	if err != nil {
		t.Fatalf("Sort() error = %v", err)
	}
	want := []string{"a", "b", "c", "d"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Sort() = %v, want %v", got, want)
	}
}

func TestSortIndependentRootsDeterministic(t *testing.T) {
	build := func() *Graph {
		g := New()
		g.AddNode("z")
		g.AddNode("a")
		g.AddNode("m")
		return g
	}
	want := []string{"z", "a", "m"}
	for i := range 5 {
		got, err := build().Sort()
		if err != nil {
			t.Fatalf("Sort() error = %v", err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("run %d: Sort() = %v, want %v (insertion order)", i, got, want)
		}
	}
}

func TestSortTwoNodeCycle(t *testing.T) {
	g := New()
	g.AddNode("a")
	g.AddNode("b")
	mustAddEdge(t, g, "a", "b")
	mustAddEdge(t, g, "b", "a")

	_, err := g.Sort()
	if err == nil {
		t.Fatal("Sort() error = nil, want cycle error")
	}
	var ce *CycleError
	if !asCycleError(err, &ce) {
		t.Fatalf("Sort() error = %v (%T), want *CycleError", err, err)
	}
	want := []string{"a", "b"}
	if !reflect.DeepEqual(ce.Nodes, want) {
		t.Fatalf("CycleError.Nodes = %v, want %v", ce.Nodes, want)
	}
	if ce.Error() != "dependency cycle: a -> b -> a" {
		t.Fatalf("Error() = %q", ce.Error())
	}
}

func TestSortThreeNodeCycle(t *testing.T) {
	g := New()
	g.AddNode("a")
	g.AddNode("b")
	g.AddNode("c")
	mustAddEdge(t, g, "a", "b")
	mustAddEdge(t, g, "b", "c")
	mustAddEdge(t, g, "c", "a")

	_, err := g.Sort()
	var ce *CycleError
	if !asCycleError(err, &ce) {
		t.Fatalf("Sort() error = %v (%T), want *CycleError", err, err)
	}
	want := []string{"a", "b", "c"}
	if !reflect.DeepEqual(ce.Nodes, want) {
		t.Fatalf("CycleError.Nodes = %v, want %v", ce.Nodes, want)
	}
	if !strings.HasPrefix(ce.Error(), "dependency cycle: ") {
		t.Fatalf("Error() = %q", ce.Error())
	}
}

func TestAddEdgeUnknownNode(t *testing.T) {
	g := New()
	g.AddNode("a")

	if err := g.AddEdge("a", "ghost"); err == nil || err.Error() != `unknown node "ghost"` {
		t.Fatalf("AddEdge(known, unknown) error = %v", err)
	}
	if err := g.AddEdge("ghost", "a"); err == nil || err.Error() != `unknown node "ghost"` {
		t.Fatalf("AddEdge(unknown, known) error = %v", err)
	}
}

func TestAddNodeIdempotent(t *testing.T) {
	g := New()
	g.AddNode("a")
	g.AddNode("a")
	got, err := g.Sort()
	if err != nil {
		t.Fatalf("Sort() error = %v", err)
	}
	if !reflect.DeepEqual(got, []string{"a"}) {
		t.Fatalf("Sort() = %v, want [a]", got)
	}
}

func asCycleError(err error, target **CycleError) bool {
	ce, ok := err.(*CycleError)
	if !ok {
		return false
	}
	*target = ce
	return true
}
