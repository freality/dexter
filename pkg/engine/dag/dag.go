// Package dag provides a minimal directed-acyclic-graph with a deterministic
// topological sort. It knows nothing about the IR (plain string node IDs) so
// it stays trivially testable.
package dag

import (
	"fmt"
	"sort"
	"strings"
)

// Graph is a set of nodes and dependency edges between them.
type Graph struct {
	nodes []string            // insertion order
	index map[string]struct{} // existence check
	deps  map[string][]string // node -> dependencies, in AddEdge call order
}

// New returns an empty graph.
func New() *Graph {
	return &Graph{
		index: map[string]struct{}{},
		deps:  map[string][]string{},
	}
}

// AddNode registers a node. Re-adding an existing node is a no-op.
func (g *Graph) AddNode(id string) {
	if _, ok := g.index[id]; ok {
		return
	}
	g.index[id] = struct{}{}
	g.nodes = append(g.nodes, id)
}

// AddEdge records that node depends on dependency. Both endpoints must
// already exist via AddNode.
func (g *Graph) AddEdge(node, dependency string) error {
	if _, ok := g.index[node]; !ok {
		return fmt.Errorf("unknown node %q", node)
	}
	if _, ok := g.index[dependency]; !ok {
		return fmt.Errorf("unknown node %q", dependency)
	}
	g.deps[node] = append(g.deps[node], dependency)
	return nil
}

// CycleError reports a dependency cycle. Nodes is the sorted set of nodes
// that still had unmet dependencies when the sort got stuck.
type CycleError struct {
	Nodes []string
}

func (e *CycleError) Error() string {
	path := append(append([]string{}, e.Nodes...), e.Nodes[0])
	return "dependency cycle: " + strings.Join(path, " -> ")
}

// Sort returns nodes dependency-first; ties broken by insertion order.
//
// ponytail: O(n^2) selection scan rather than queue-based Kahn's; fine for
// homelab-scale resource counts. Upgrade to an indegree queue if graphs grow
// into the thousands of nodes.
func (g *Graph) Sort() ([]string, error) {
	done := make(map[string]bool, len(g.nodes))
	result := make([]string, 0, len(g.nodes))

	for len(result) < len(g.nodes) {
		progressed := false
		for _, n := range g.nodes {
			if done[n] {
				continue
			}
			if g.ready(n, done) {
				done[n] = true
				result = append(result, n)
				progressed = true
				break
			}
		}
		if !progressed {
			remaining := make([]string, 0, len(g.nodes)-len(result))
			for _, n := range g.nodes {
				if !done[n] {
					remaining = append(remaining, n)
				}
			}
			sort.Strings(remaining)
			return nil, &CycleError{Nodes: remaining}
		}
	}
	return result, nil
}

func (g *Graph) ready(node string, done map[string]bool) bool {
	for _, d := range g.deps[node] {
		if !done[d] {
			return false
		}
	}
	return true
}
