// Package state manages the engine's on-disk record of what has been
// applied: .dex/state.json plus its advisory lock file.
package state

import (
	"sort"
	"time"
)

// Resource is one resource as last materialized by an adapter.
type Resource struct {
	Type       string         `json:"type"`
	Plugin     string         `json:"plugin"`
	Properties map[string]any `json:"properties"`
	// Outputs are adapter-materialized values (ids, IPs) carried across applies.
	Outputs map[string]any `json:"outputs,omitempty"`
}

// State is the full persisted record for a project.
type State struct {
	Project   string              `json:"project"`
	Serial    int                 `json:"serial"`
	UpdatedAt time.Time           `json:"updated_at"`
	Resources map[string]Resource `json:"resources"`
}

// New returns an empty State with a non-nil Resources map.
func New() *State {
	return &State{Resources: map[string]Resource{}}
}

// Names returns resource names in sorted order.
func (s *State) Names() []string {
	names := make([]string, 0, len(s.Resources))
	for name := range s.Resources {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Store is the engine's only view of persistence.
type Store interface {
	Load() (*State, error)
	Save(*State) error
}
