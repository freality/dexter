package cli

import (
	tea "github.com/charmbracelet/bubbletea"
	"strings"
	"testing"
	"time"

	"github.com/freality/dexter/pkg/engine"
)

func TestEventStartedCreatesActiveRow(t *testing.T) {
	m := NewApplyModel()
	m.Update(EventMsg{Event: engine.Event{Name: "web", Kind: engine.EventStarted}})
	view := m.View()
	if !strings.Contains(view, "web") {
		t.Fatalf("view missing resource name: %q", view)
	}
	if strings.Contains(view, "○") {
		t.Fatalf("view still shows pending marker: %q", view)
	}
}

func TestEventProgressUpdatesPercent(t *testing.T) {
	m := NewApplyModel()
	m.Update(EventMsg{Event: engine.Event{Name: "web", Kind: engine.EventStarted}})
	m.Update(EventMsg{Event: engine.Event{Name: "web", Kind: engine.EventProgress, Percent: 45}})
	view := m.View()
	if !strings.Contains(view, "45%") {
		t.Fatalf("view missing progress percent: %q", view)
	}
}

func TestEventDoneMarksRowDone(t *testing.T) {
	m := NewApplyModel()
	m.Update(EventMsg{Event: engine.Event{Name: "web", Kind: engine.EventStarted}})
	m.Update(EventMsg{Event: engine.Event{Name: "web", Kind: engine.EventDone}})
	view := m.View()
	if !strings.Contains(view, "✓") {
		t.Fatalf("view missing checkmark: %q", view)
	}
}

func TestEventFailedShowsError(t *testing.T) {
	m := NewApplyModel()
	m.Update(EventMsg{Event: engine.Event{Name: "web", Kind: engine.EventStarted}})
	m.Update(EventMsg{Event: engine.Event{Name: "web", Kind: engine.EventFailed, Message: "connection refused"}})
	view := m.View()
	if !strings.Contains(view, "✗") {
		t.Fatalf("view missing failure marker: %q", view)
	}
	if !strings.Contains(view, "connection refused") {
		t.Fatalf("view missing error message: %q", view)
	}
}

func TestMultipleResourcesOrdered(t *testing.T) {
	m := NewApplyModel()
	m.Update(EventMsg{Event: engine.Event{Name: "a", Kind: engine.EventStarted}})
	m.Update(EventMsg{Event: engine.Event{Name: "b", Kind: engine.EventStarted}})
	m.Update(EventMsg{Event: engine.Event{Name: "c", Kind: engine.EventStarted}})
	view := m.View()
	ia := strings.Index(view, "a")
	ib := strings.Index(view, "b")
	ic := strings.Index(view, "c")
	if !(ia < ib && ib < ic) {
		t.Fatalf("resources not in order: %q", view)
	}
}

func TestFinalSummary(t *testing.T) {
	m := NewApplyModel()
	m.Update(EventMsg{Event: engine.Event{Name: "a", Kind: engine.EventStarted}})
	m.Update(EventMsg{Event: engine.Event{Name: "a", Kind: engine.EventDone}})
	m.Update(EventMsg{Event: engine.Event{Name: "b", Kind: engine.EventStarted}})
	m.Update(EventMsg{Event: engine.Event{Name: "b", Kind: engine.EventDone}})
	m.Update(EventMsg{Event: engine.Event{Name: "c", Kind: engine.EventStarted}})
	m.Update(EventMsg{Event: engine.Event{Name: "c", Kind: engine.EventFailed, Message: "boom"}})
	summary := m.Summary()
	if !strings.Contains(summary, "2 added") || !strings.Contains(summary, "1 failed") {
		t.Fatalf("summary = %q, want '2 added' and '1 failed'", summary)
	}
}

func TestTickMsgAdvancesSpinner(t *testing.T) {
	m := NewApplyModel()
	m.Update(EventMsg{Event: engine.Event{Name: "web", Kind: engine.EventStarted}})
	before := m.View()
	m.Update(TickMsg(time.Time{}))
	after := m.View()
	if before == after {
		t.Fatalf("spinner did not change: %q", after)
	}
}

func TestProgressBarRendering(t *testing.T) {
	fifty := ProgressBar(50)
	if !strings.Contains(fifty, "50%") {
		t.Fatalf("50%% bar = %q", fifty)
	}
	zero := ProgressBar(0)
	if !strings.Contains(zero, "0%") || !strings.Contains(zero, "░") {
		t.Fatalf("0%% bar = %q", zero)
	}
	hundred := ProgressBar(100)
	if !strings.Contains(hundred, "100%") || !strings.Contains(hundred, "█") {
		t.Fatalf("100%% bar = %q", hundred)
	}
}

func TestProgressBarClamps(t *testing.T) {
	neg := ProgressBar(-5)
	if !strings.Contains(neg, "0%") {
		t.Fatalf("negative -> 0%%: %q", neg)
	}
	over := ProgressBar(150)
	if !strings.Contains(over, "100%") {
		t.Fatalf("overflow -> 100%%: %q", over)
	}
}

func TestInitReturnsCmd(t *testing.T) {
	m := NewApplyModel()
	cmd := m.Init()
	if cmd == nil {
		t.Fatalf("Init() returned nil")
	}
}

func TestUpsertRowCreatesWhenMissing(t *testing.T) {
	m := NewApplyModel()
	r := m.UpsertRow("svc", "create")
	if r.name != "svc" {
		t.Fatalf("row.name = %q, want svc", r.name)
	}
	if r.state != rowPending {
		t.Fatalf("state = %v, want pending", r.state)
	}
}

func TestUpsertRowReturnsExisting(t *testing.T) {
	m := NewApplyModel()
	r1 := m.UpsertRow("svc", "create")
	r2 := m.UpsertRow("svc", "delete")
	if r1 != r2 {
		t.Fatalf("different rows for same name")
	}
	if r2.action != "create" {
		t.Fatalf("action overwritten: %q, want create", r2.action)
	}
}

func TestEventLogMessage(t *testing.T) {
	m := NewApplyModel()
	m.Update(EventMsg{Event: engine.Event{Name: "db", Kind: engine.EventStarted}})
	m.Update(EventMsg{Event: engine.Event{Name: "db", Kind: engine.EventLog, Message: "pulling image..."}})
	view := m.View()
	if !strings.Contains(view, "pulling image...") {
		t.Fatalf("view missing log message: %q", view)
	}
}

func TestRenderRowPending(t *testing.T) {
	m := NewApplyModel()
	r := m.UpsertRow("svc", "create")
	r.state = rowPending
	view := m.View()
	if !strings.Contains(view, "○") {
		t.Fatalf("pending row should show ○: %q", view)
	}
}

func TestTickCmdReturnsTickMsg(t *testing.T) {
	cmd := tick()
	msg := cmd()
	if _, ok := msg.(TickMsg); !ok {
		t.Fatalf("tick() returned %v, want TickMsg", msg)
	}
}

func TestKeyMsgCtrlCQuits(t *testing.T) {
	m := NewApplyModel()
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatalf("Update(ctrl+c) returned nil cmd, want tea.Quit")
	}
}

func TestKeyMsgOther(t *testing.T) {
	m := NewApplyModel()
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatalf("Update(enter) returned cmd %v, want nil", cmd)
	}
}

func TestUpdateUnknownMsg(t *testing.T) {
	m := NewApplyModel()
	m2, cmd := m.Update(nil)
	if m2 != m {
		t.Fatalf("Update(nil) returned different model")
	}
	if cmd != nil {
		t.Fatalf("Update(nil) returned cmd %v, want nil", cmd)
	}
}
