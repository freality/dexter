package cli

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/freality/dexter/pkg/engine"
	"github.com/freality/dexter/pkg/engine/diff"
)

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// EventMsg wraps an engine.Event for delivery to the TUI via Program.Send.
type EventMsg struct {
	Event engine.Event
}

// TickMsg drives spinner animation.
type TickMsg time.Time

type rowState int

const (
	rowPending rowState = iota
	rowActive
	rowDone
	rowFailed
)

type applyRow struct {
	name    string
	action  diff.Action
	state   rowState
	message string
	percent int
}

// ApplyModel is a bubbletea model rendering one row per resource as the
// engine applies a plan.
type ApplyModel struct {
	order      []string
	rows       map[string]*applyRow
	spinnerIdx int
}

// NewApplyModel returns an empty ApplyModel ready to receive EventMsg values.
func NewApplyModel() *ApplyModel {
	return &ApplyModel{rows: make(map[string]*applyRow)}
}

func (m *ApplyModel) Init() tea.Cmd {
	return tick()
}

func tick() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg {
		return TickMsg(t)
	})
}

func (m *ApplyModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case EventMsg:
		m.handleEvent(msg.Event)
		return m, nil
	case TickMsg:
		m.spinnerIdx = (m.spinnerIdx + 1) % len(spinnerFrames)
		return m, tick()
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m *ApplyModel) handleEvent(ev engine.Event) {
	r := m.UpsertRow(ev.Name, ev.Action)
	switch ev.Kind {
	case engine.EventStarted:
		r.state = rowActive
	case engine.EventProgress:
		r.percent = ev.Percent
	case engine.EventLog:
		r.message = ev.Message
	case engine.EventDone:
		r.state = rowDone
	case engine.EventFailed:
		r.state = rowFailed
		r.message = ev.Message
	}
}

// UpsertRow returns the row for name, creating it with the given action if
// it doesn't exist yet. action is ignored for an already-existing row.
func (m *ApplyModel) UpsertRow(name string, action diff.Action) *applyRow {
	if r, ok := m.rows[name]; ok {
		return r
	}
	r := &applyRow{name: name, action: action, state: rowPending}
	m.rows[name] = r
	m.order = append(m.order, name)
	return r
}

func (m *ApplyModel) View() string {
	var b strings.Builder
	for _, name := range m.order {
		b.WriteString(m.renderRow(m.rows[name]))
		b.WriteString("\n")
	}
	return b.String()
}

func (m *ApplyModel) renderRow(r *applyRow) string {
	var marker string
	switch r.state {
	case rowPending:
		marker = "○"
	case rowActive:
		marker = spinnerFrames[m.spinnerIdx]
	case rowDone:
		marker = "✓"
	case rowFailed:
		marker = "✗"
	}
	line := fmt.Sprintf("%s %s", marker, r.name)
	if r.percent > 0 {
		line += " " + ProgressBar(r.percent)
	}
	if r.message != "" {
		line += " " + r.message
	}
	return line
}

// ProgressBar renders a 20-cell bar plus a trailing percentage label, e.g.
// "[██████████░░░░░░░░░░] 50%".
func ProgressBar(percent int) string {
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}
	const width = 20
	filled := percent * width / 100
	return "[" + strings.Repeat("█", filled) + strings.Repeat("░", width-filled) + fmt.Sprintf("] %d%%", percent)
}

// Summary returns a one-line tally of resource outcomes, e.g.
// "2 added, 1 failed".
func (m *ApplyModel) Summary() string {
	var done, failed int
	for _, name := range m.order {
		switch m.rows[name].state {
		case rowDone:
			done++
		case rowFailed:
			failed++
		}
	}
	var parts []string
	if done > 0 {
		parts = append(parts, fmt.Sprintf("%d added", done))
	}
	if failed > 0 {
		parts = append(parts, fmt.Sprintf("%d failed", failed))
	}
	return strings.Join(parts, ", ")
}
