// Package state manages the engine's on-disk record of what has been
// applied: .dex/state.json plus its advisory lock file.
package state

import (
	"time"

	"github.com/freality/dexter/pkg/engine/internal/maputil"
)

// Resource is one resource as last materialized by an adapter.
type Resource struct {
	Type       string         `json:"type"`
	Plugin     string         `json:"plugin"`
	Properties map[string]any `json:"properties"`
	// DependsOn mirrors the resource's IR dependencies at apply time, so a
	// later pure delete can be ordered without re-synthesizing the config.
	DependsOn []string `json:"depends_on,omitempty"`
	// Outputs are adapter-materialized values (ids, IPs) carried across applies.
	Outputs map[string]any `json:"outputs,omitempty"`
	// Tainted marks a resource whose last create/update failed, so the target
	// may not match Properties; the next plan replaces it.
	Tainted bool `json:"tainted,omitempty"`
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
	return maputil.SortedKeys(s.Resources)
}

// Store is the engine's only view of persistence.
type Store interface {
	Load() (*State, error)
	Save(*State) error
}
