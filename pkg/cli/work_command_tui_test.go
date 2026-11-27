package cli

import (
	"errors"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/github/gh-aw/pkg/workqueue"
	"github.com/stretchr/testify/require"
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
	require.True(t, ok, "TUI changed model type")
	return updated, command
}

func tuiLoad(t *testing.T, model workQueueTUI, message workQueueLoaded) workQueueTUI {
	t.Helper()
	next, _ := model.Update(message)
	updated, ok := next.(workQueueTUI)
	require.True(t, ok, "TUI changed model type")
	return updated
}

func loadedTUI(t *testing.T, log *workQueueTestLog) workQueueTUI {
	t.Helper()
	state := log.state()
	loaded := tuiLoad(t, newWorkQueueTUI(workQueueTUIService{}, "work-queue"), workQueueLoaded{state: &state})
	loaded.width, loaded.height = 180, 60
	loaded.sizeDetails()
	return loaded
}

func TestWorkTUICursorDetailsSearchCollapseAndResize(t *testing.T) {
	log, _ := workOperatorFixture(t, true)
	m := loadedTUI(t, log)
	first := m.rows[m.cursor]
	require.Equal(t, "work", first.Kind, "the cursor skips the graph header")
	require.Contains(t, m.details.GetContent(), first.ID)
	m, _ = tuiKey(t, m, "down")
	claim := m.rows[m.cursor]
	require.Equal(t, "claim", claim.Kind)
	require.Contains(t, m.details.GetContent(), claim.ID)
	m, _ = tuiKey(t, m, "tab")
	require.True(t, m.detailFocus, "details cannot receive keyboard focus")
	m, _ = tuiKey(t, m, "tab")
	m, _ = tuiKey(t, m, "h")
	require.Equal(t, "work", m.rows[m.cursor].Kind, "collapse must keep the owning Work under the cursor")
	require.True(t, m.collapsed[first.WorkID])
	require.Len(t, m.rows, 6)
	m, _ = tuiKey(t, m, "l")
	require.Len(t, m.rows, 7)
	m, _ = tuiKey(t, m, "/")
	require.True(t, m.editing)
	m.search.SetValue(claim.ID)
	m, _ = tuiKey(t, m, "enter")
	require.Len(t, m.rows, 3, "search lost Claim context")
	require.Equal(t, claim.ID, m.rows[2].ID)
	m, _ = tuiKey(t, m, "esc")
	require.Empty(t, m.filter.Query)
	require.Len(t, m.rows, 7)
	m, _ = tuiKey(t, m, "G")
	require.Equal(t, len(m.rows)-1, m.cursor)
	m, _ = tuiKey(t, m, "g")
	require.Equal(t, "work", m.rows[m.cursor].Kind, "home lands on the first Work, not the graph header")
	for _, size := range [][2]int{{180, 60}, {100, 30}, {80, 24}, {45, 16}, {30, 10}} {
		next, _ := m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		var ok bool
		m, ok = next.(workQueueTUI)
		require.True(t, ok, "window update lost model")
		view := m.View()
		require.True(t, view.AltScreen)
		require.LessOrEqual(t, lipgloss.Width(view.Content), size[0], "responsive frame exceeds %dx%d", size[0], size[1])
		require.LessOrEqual(t, lipgloss.Height(view.Content), size[1], "responsive frame exceeds %dx%d", size[0], size[1])
		require.NotContains(t, view.Content, "PAYLOAD-MUST")
		require.NotContains(t, view.Content, "SECRET-MUST")
	}
}

func TestWorkTUICancelIsScopedToAvailableWorkAndConfirmed(t *testing.T) {
	log, ids := workOperatorFixture(t, false)
	m := loadedTUI(t, log)
	work := m.rows[m.cursor]
	require.Equal(t, ids[0], work.ID)
	writes := 0
	m.service.publish = func(kind string, parameters any, operationsFor workqueue.OperationsFor) (workqueue.Publication, error) {
		writes++
		require.Equal(t, "cancel_work", kind)
		require.Equal(t, workqueue.CancelWorkParameters{WorkIDs: []string{work.ID}, Reason: "operator"}, parameters)
		state := log.state()
		operations, err := operationsFor(state, workqueue.Request{})
		require.NoError(t, err)
		require.Len(t, operations, 1)
		require.JSONEq(t, `{"kind":"WorkCancellation","work_id":"`+work.ID+`","reason":"operator"}`, string(operations[0]))
		log.cancel(work.ID)
		return workqueue.Publication{Committed: true, Request: workqueue.Request{ID: "request:cancel"}}, nil
	}
	m.service.read = func() (workqueue.Projection, error) { return log.state(), nil }
	m, _ = tuiKey(t, m, " ")
	require.Len(t, m.selected, 1)
	m, command := tuiKey(t, m, "c")
	require.Nil(t, command)
	require.Zero(t, writes, "cancel mutated before review")
	require.Equal(t, "cancel", m.mode)
	require.Equal(t, []string{work.ID}, m.targets)
	require.Contains(t, m.modalView(), "CANCEL AVAILABLE WORK")
	require.Contains(t, m.modalView(), workQueueText(work.ID))
	m, command = tuiKey(t, m, "n")
	require.Nil(t, command)
	require.Empty(t, m.mode)
	require.Zero(t, writes, "cancel dismissal mutated state")
	m, _ = tuiKey(t, m, "c")
	m, command = tuiKey(t, m, "y")
	require.NotNil(t, command)
	require.True(t, m.busy)
	require.Zero(t, writes, "confirmation bypassed async action state")
	m, _ = tuiKey(t, m, "q")
	require.Contains(t, m.status, "Request in flight", "quitting mid-publication must warn")
	m = tuiLoad(t, m, command().(workQueueLoaded))
	require.Equal(t, 1, writes, "operator action was duplicated or never executed")
	require.False(t, m.busy)
	require.Equal(t, "request:cancel", m.lastRequest)
	require.Contains(t, m.status, "Committed 1 requests")
	require.Empty(t, m.selected, "cancelled Work must leave the selection")
	require.Equal(t, "cancelled", m.state.Works[work.ID].State)
}

func TestWorkTUIReleaseTargetsTheSelectedClaimsDispatch(t *testing.T) {
	log, ids := workOperatorFixture(t, true)
	m := loadedTUI(t, log)
	m, _ = tuiKey(t, m, "c")
	require.Empty(t, m.mode, "claimed Work cannot be cancelled directly")
	require.Contains(t, m.status, "cancel applies to available Work only")
	m, _ = tuiKey(t, m, "down")
	claim := m.rows[m.cursor]
	require.Equal(t, "claim", claim.Kind)
	m.service.maxAttempts = func(pool string) int {
		require.Equal(t, "default", pool)
		return 1
	}
	var released []string
	m.service.publish = func(kind string, parameters any, operationsFor workqueue.OperationsFor) (workqueue.Publication, error) {
		require.Equal(t, "release", kind)
		release, ok := parameters.(workqueue.ReleaseParameters)
		require.True(t, ok)
		require.Equal(t, "operator", release.Reason)
		operations, err := operationsFor(log.state(), workqueue.Request{})
		require.NoError(t, err)
		require.Len(t, operations, 2, "Release plus attempts_exhausted at max_attempts 1")
		require.JSONEq(t, `{"kind":"Release","dispatch_id":"`+release.DispatchID+`","reason":"operator"}`, string(operations[0]))
		require.Contains(t, string(operations[1]), `"attempts_exhausted"`)
		values := make([]any, 0, len(operations))
		for _, operation := range operations {
			values = append(values, operation)
		}
		log.append(workQueueTestAdmin, "release", release, values...)
		released = append(released, release.DispatchID)
		return workqueue.Publication{Committed: true, Request: workqueue.Request{ID: "request:" + release.DispatchID}}, nil
	}
	m.service.read = func() (workqueue.Projection, error) { return log.state(), nil }
	m, _ = tuiKey(t, m, "x")
	require.Equal(t, "release", m.mode)
	require.Equal(t, []string{claim.DispatchID}, m.targets)
	require.Contains(t, m.modalView(), "Running workflows are NOT stopped")
	m, command := tuiKey(t, m, "y")
	require.NotNil(t, command)
	m = tuiLoad(t, m, command().(workQueueLoaded))
	require.Equal(t, []string{claim.DispatchID}, released)
	require.Equal(t, "request:"+claim.DispatchID, m.lastRequest)
	require.Equal(t, "cancelled", m.state.Works[ids[0]].State)
	require.Equal(t, "attempts_exhausted", m.state.Works[ids[0]].CancellationReason)
	require.Equal(t, "claimed", m.state.Works[ids[1]].State, "release must touch only the selected dispatch")

	m, _ = tuiKey(t, m, "x")
	require.Empty(t, m.mode, "a closed Claim cannot be released again")
	require.Contains(t, m.status, "release applies to open Claims")
}

func TestWorkTUIBulkPreviewCannotHideTargetsOrMutate(t *testing.T) {
	log, ids := workOperatorFixture(t, false)
	m := loadedTUI(t, log)
	m.service.publish = func(string, any, workqueue.OperationsFor) (workqueue.Publication, error) {
		t.Fatal("hidden confirmation published")
		return workqueue.Publication{}, nil
	}
	for _, row := range m.rows {
		if row.Kind == "work" {
			m.selected[row.key()] = row
		}
	}
	m.width, m.height = 45, 16
	m, _ = tuiKey(t, m, "c")
	require.Equal(t, "cancel", m.mode)
	m, command := tuiKey(t, m, "y")
	require.Nil(t, command)
	require.False(t, m.busy)
	require.Contains(t, m.modalView(), "ACTION DISABLED")
	require.Contains(t, m.status, "Action disabled")
	m.width, m.height = 180, 60
	require.True(t, m.previewFits(), "full-size batch review is unavailable")
	require.Len(t, m.targets, len(ids))
	for _, id := range ids {
		require.Contains(t, m.modalView(), workQueueText(id))
	}
	m, _ = tuiKey(t, m, "esc")
	require.Empty(t, m.mode)
	m, _ = tuiKey(t, m, "x")
	require.Empty(t, m.mode, "available Work has no dispatch to release")
	require.Contains(t, m.status, "release applies to open Claims")
}

func TestWorkTUIRefreshKeepsCursorAndReportsErrors(t *testing.T) {
	log, ids := workOperatorFixture(t, false)
	m := loadedTUI(t, log)
	m, _ = tuiKey(t, m, "down")
	require.Equal(t, ids[1], m.rows[m.cursor].ID)
	key := m.rows[m.cursor].key()
	require.Len(t, log.dispatch(1), 1)
	state := log.state()
	m = tuiLoad(t, m, workQueueLoaded{state: &state})
	require.Len(t, m.rows, 5, "the refreshed forest shows the new Claim")
	require.Equal(t, key, m.rows[m.cursor].key(), "refresh lost the cursor")
	require.Contains(t, m.details.GetContent(), workQueueText(ids[1]))
	require.Contains(t, m.details.GetContent(), "State: available")

	m = tuiLoad(t, m, workQueueLoaded{err: errors.New("permission denied\x1b[31m")})
	require.Contains(t, m.status, "Error (view may be stale)")
	require.NotContains(t, m.status, "\x1b")
	require.Len(t, m.rows, 5, "a failed read must keep the last committed view")
	m = tuiLoad(t, m, workQueueLoaded{err: errors.New("read failed\x1b[31m"), requests: []string{"request:stable"}, committed: true})
	require.Contains(t, m.status, "Committed 1 requests, then:")
	require.NotContains(t, m.status, "\x1b")
	require.Equal(t, "request:stable", m.lastRequest)

	m.mode = "cancel"
	next, command := m.Update(workQueueTick(time.Now()))
	require.NotNil(t, command, "ticks must keep scheduling")
	frozen, ok := next.(workQueueTUI)
	require.True(t, ok)
	require.False(t, frozen.busy, "auto-refresh must not reload under a frozen review")
	frozen.mode = ""
	next, _ = frozen.Update(workQueueTick(time.Now()))
	refreshing, ok := next.(workQueueTUI)
	require.True(t, ok)
	require.True(t, refreshing.busy, "idle ticks refresh the committed state")
}
