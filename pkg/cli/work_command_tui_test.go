package cli

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/github/gh-aw/pkg/workqueue"
)

func tuiKey(t *testing.T, model workQueueTUI, key string) (workQueueTUI, tea.Cmd) {
	t.Helper()
	code := rune(0)
	text := ""
	switch key {
	case "up":
		code = tea.KeyUp
	case "down":
		code = tea.KeyDown
	case "tab":
		code = tea.KeyTab
	case "enter":
		code = tea.KeyEnter
	case "esc":
		code = tea.KeyEscape
	default:
		text = key
		if len([]rune(key)) == 1 {
			code = []rune(key)[0]
		}
	}
	next, command := model.Update(tea.KeyPressMsg{Code: code, Text: text})
	updated, ok := next.(workQueueTUI)
	if !ok {
		t.Fatal("TUI changed model type")
	}
	return updated, command
}

func loadedTUI(t *testing.T, granted bool) workQueueTUI {
	t.Helper()
	state, _, _ := workOperatorFixture(t, granted)
	model := newWorkQueueTUI(workQueueTUIService{}, "work-queue")
	next, _ := model.Update(workQueueLoaded{state: &state})
	loaded, ok := next.(workQueueTUI)
	if !ok {
		t.Fatal("TUI changed model type")
	}
	loaded.width, loaded.height = 180, 60
	loaded.sizeDetails()
	return loaded
}

func TestWorkTUICursorDetailsSearchCollapseAndResize(t *testing.T) {
	m := loadedTUI(t, true)
	first := m.rows[m.cursor]
	if first.Kind != "work" || !strings.Contains(m.details.GetContent(), first.ID) {
		t.Fatal("cursor Work has no details")
	}
	m, _ = tuiKey(t, m, "down")
	claim := m.rows[m.cursor]
	if claim.Kind != "claim" || !strings.Contains(m.details.GetContent(), claim.ID) {
		t.Fatal("cursor Claim did not update details")
	}
	m, _ = tuiKey(t, m, "tab")
	if !m.detailFocus {
		t.Fatal("details cannot receive keyboard focus")
	}
	m, _ = tuiKey(t, m, "tab")
	m, _ = tuiKey(t, m, "h")
	if m.rows[m.cursor].Kind != "work" || !m.collapsed[first.WorkID] {
		t.Fatal("collapse did not retain owning Work")
	}
	m, _ = tuiKey(t, m, "l")
	m, _ = tuiKey(t, m, "/")
	m.search.SetValue(claim.ID)
	m, _ = tuiKey(t, m, "enter")
	if len(m.rows) != 3 || m.rows[2].ID != claim.ID {
		t.Fatal("search lost Claim context")
	}
	m, _ = tuiKey(t, m, "esc")
	if m.filter.Query != "" || len(m.rows) != 7 {
		t.Fatal("Esc did not clear filter")
	}
	for _, size := range [][2]int{{180, 60}, {100, 30}, {80, 24}, {45, 16}, {30, 10}} {
		next, _ := m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		var ok bool
		m, ok = next.(workQueueTUI)
		if !ok {
			t.Fatal("window update lost model")
		}
		view := m.View()
		if !view.AltScreen || lipgloss.Width(view.Content) > size[0] || lipgloss.Height(view.Content) > size[1] {
			t.Fatalf("responsive frame exceeds %dx%d: %dx%d", size[0], size[1], lipgloss.Width(view.Content), lipgloss.Height(view.Content))
		}
		if strings.Contains(view.Content, "PAYLOAD-MUST") || strings.Contains(view.Content, "SECRET-MUST") {
			t.Fatal("TUI leaked payloads")
		}
	}
}

func TestWorkTUIConfirmCancelIsScopedAndAtomic(t *testing.T) {
	m := loadedTUI(t, true)
	m, _ = tuiKey(t, m, "down") // Current Claim, not its Work.
	claim := m.rows[m.cursor]
	writes := 0
	m.service.publish = func(id, kind string, params workqueue.OperationsParameters) (workqueue.Publication, error) {
		writes++
		if id == "" || kind != "cancel_work" || len(params.Operations) != 1 || !strings.Contains(string(params.Operations[0]), claim.ID) {
			t.Fatal("confirmation did not preserve exact Claim fence")
		}
		return workqueue.Publication{Changed: true}, nil
	}
	m.service.read = func() (workqueue.Projection, error) { return m.state, nil }
	m, command := tuiKey(t, m, "c")
	if command != nil || writes != 0 || m.mode != "cancel" || len(m.targets) != 1 {
		t.Fatal("cancel mutated before review")
	}
	m, command = tuiKey(t, m, "n")
	if command != nil || m.mode != "" || writes != 0 {
		t.Fatal("cancel dismissal mutated state")
	}
	m, _ = tuiKey(t, m, "c")
	m, command = tuiKey(t, m, "y")
	if command == nil || !m.busy || writes != 0 {
		t.Fatal("confirmation bypassed async action state")
	}
	result := command()
	if writes != 1 {
		t.Fatal("operator action was duplicated or never executed")
	}
	next, _ := m.Update(result)
	updated, ok := next.(workQueueTUI)
	if !ok || updated.busy || updated.lastRequest == "" || !strings.Contains(updated.status, "Committed") {
		t.Fatal("committed action has no stable status")
	}
}

func TestWorkTUIBulkPreviewCannotHideTargetsOrMutate(t *testing.T) {
	m := loadedTUI(t, true)
	for _, row := range m.rows {
		if row.Kind == "work" {
			m.selected[row.key()] = row
		}
	}
	m.width, m.height = 45, 16
	m, _ = tuiKey(t, m, "c")
	m, command := tuiKey(t, m, "y")
	if command != nil || m.busy || !strings.Contains(m.modalView(), "ACTION DISABLED") {
		t.Fatal("oversize/hidden confirmation published")
	}
	m.width, m.height = 180, 60
	if !m.previewFits() || len(m.targets) != 3 {
		t.Fatal("full-size batch review is unavailable")
	}
	m, _ = tuiKey(t, m, "esc")
	m, _ = tuiKey(t, m, "p")
	if m.mode != "" || !strings.Contains(m.status, "ineligible") {
		t.Fatal("active Claims were reprioritized")
	}
}

func TestWorkTUIPriorityRefreshAndErrors(t *testing.T) {
	m := loadedTUI(t, false)
	m, _ = tuiKey(t, m, "p")
	m, command := tuiKey(t, m, "y")
	if command != nil {
		t.Fatal("priority action accepted without an explicit class")
	}
	m, _ = tuiKey(t, m, "1")
	if m.priority != 1 || m.pendingKind != "control" || !strings.Contains(string(m.pending.Operations[0]), `"expected_priority":3`) {
		t.Fatal("priority review lacks prospective CAS intent")
	}
	m, _ = tuiKey(t, m, "esc")
	key := m.rows[m.cursor].key()
	state := m.state
	state.Works[m.rows[m.cursor].WorkID].EffectivePriority = 2
	next, _ := m.Update(workQueueLoaded{state: &state})
	var ok bool
	m, ok = next.(workQueueTUI)
	if !ok || m.rows[m.cursor].key() != key || !strings.Contains(m.details.GetContent(), "Priority: 2") {
		t.Fatal("refresh lost cursor or current details")
	}
	next, _ = m.Update(workQueueLoaded{err: errors.New("permission denied\x1b[31m"), requestID: "stable-uncertain"})
	m, ok = next.(workQueueTUI)
	if !ok || !strings.Contains(m.status, "uncertain") || strings.Contains(m.status, "\x1b") || m.lastRequest != "stable-uncertain" || len(m.rows) == 0 {
		t.Fatal("failed publication disappeared or injected terminal controls")
	}
	m.mode = "cancel"
	_, command = m.Update(workQueueTick(time.Now()))
	if command == nil || m.busy {
		t.Fatal("auto-refresh mutated a frozen review")
	}
}
