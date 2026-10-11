package engine

import "github.com/freality/dexter/pkg/engine/diff"

// EventKind classifies one Event.
type EventKind string

const (
	// EventStarted fires when a change begins applying.
	EventStarted EventKind = "started"
	// EventLog carries one log line reported by an adapter.
	EventLog EventKind = "log"
	// EventProgress carries an aggregate percent reported by an adapter.
	EventProgress EventKind = "progress"
	// EventDone fires after a change applied successfully.
	EventDone EventKind = "done"
	// EventFailed fires when a change errored; Message holds the error.
	EventFailed EventKind = "failed"
)

// Event is one progress observation during Apply (D11: a callback, not a
// writer or a real bus). The engine emits Started/Done/Failed itself;
// adapters emit Log/Progress through their report parameter, which only
// fills Kind, Message and Percent — the engine sets Name and Action. A
// fan-out bus waits for Phase 3, when the daemon has multiple subscribers.
type Event struct {
	Name    string
	Action  diff.Action
	Kind    EventKind
	Message string
	Percent int
}
