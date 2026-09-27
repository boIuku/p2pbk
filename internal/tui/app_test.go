package tui

import (
	"testing"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"p2pbk/internal/app"
)

func TestTUIProcessesBackgroundEventsWhileEditing(t *testing.T) {
	m := model{
		form: passwordForm(),
		op:   &operationState{events: make(chan app.OperationEvent, 1)},
	}
	next, _ := m.Update(opMsg(app.OperationEvent{ID: "backup-1", Kind: "backup", Phase: "backing up", Message: "working"}))
	got := next.(model)
	if got.form == nil {
		t.Fatal("background operation event closed the active form")
	}
	if got.op == nil || got.op.event.ID != "backup-1" {
		t.Fatalf("operation event was not consumed: %+v", got.op)
	}

	next, _ = got.Update(opMsg(app.OperationEvent{ID: "stale", Kind: "backup", Done: true, Message: "stale"}))
	got = next.(model)
	if got.op == nil || got.op.event.ID != "backup-1" {
		t.Fatal("stale operation completion replaced the active operation")
	}
}

func TestTUICancelledUnlockClearsPendingAction(t *testing.T) {
	m := model{form: passwordForm(), pending: &pendingAction{kind: "restore", snapshot: "snapshot-id", target: "target"}}
	msg := tea.KeyMsg{Type: tea.KeyEsc}
	next, _ := m.Update(msg)
	got := next.(model)
	if got.form != nil {
		t.Fatal("password form remained open after cancellation")
	}
	if got.pending != nil {
		t.Fatal("cancelled password prompt retained pending restore")
	}
}

func TestTUIKeyBindingUsesStandardEscape(t *testing.T) {
	if !key.Matches(tea.KeyMsg{Type: tea.KeyEsc}, key.NewBinding(key.WithKeys("esc"))) {
		t.Fatal("escape key binding was not recognized")
	}
}
