package cli

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"github.com/github/gh-aw/pkg/tty"
	"github.com/github/gh-aw/pkg/workqueue"
	"github.com/spf13/cobra"
)

type workQueueTUIService struct {
	read    func() (workqueue.Projection, error)
	publish func(string, string, workqueue.OperationsParameters) (workqueue.Publication, error)
}
type workQueueLoaded struct {
	state     *workqueue.Projection
	err       error
	requestID string
	committed bool
}
type workQueueTick time.Time
type workQueueTUI struct {
	service                                workQueueTUIService
	state                                  workqueue.Projection
	branch                                 string
	filter                                 workQueueFilter
	rows                                   []workQueueRow
	cursor                                 int
	selected                               map[string]workQueueRow
	collapsed                              map[string]bool
	details                                viewport.Model
	search                                 textinput.Model
	editing, detailFocus, busy, publishing bool
	width, height                          int
	mode                                   string
	targets                                []workOperatorTarget
	pending                                workqueue.OperationsParameters
	pendingKind                            string
	priority                               int
	status, lastRequest                    string
	readAt                                 time.Time
}

func newWorkQueueTUI(service workQueueTUIService, branch string) workQueueTUI {
	search := textinput.New()
	search.CharLimit, search.Prompt = 256, "/ "
	details := viewport.New()
	details.SoftWrap = true
	return workQueueTUI{service: service, branch: branch, selected: map[string]workQueueRow{}, collapsed: map[string]bool{},
		details: details, search: search, busy: true, status: "Loading committed queue...", width: 100, height: 30}
}
func workQueueNextTick() tea.Cmd {
	return tea.Tick(10*time.Second, func(at time.Time) tea.Msg { return workQueueTick(at) })
}
func (m workQueueTUI) read() tea.Cmd {
	return func() tea.Msg {
		state, err := m.service.read()
		if err != nil {
			return workQueueLoaded{err: err}
		}
		return workQueueLoaded{state: &state}
	}
}
func (m workQueueTUI) Init() tea.Cmd { return tea.Batch(m.read(), workQueueNextTick()) }

func (m workQueueTUI) current() (workQueueRow, bool) {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return workQueueRow{}, false
	}
	return m.rows[m.cursor], true
}
func (m *workQueueTUI) rebuild(preferred string) {
	m.rows = workQueueRows(m.state, m.filter, m.collapsed)
	m.cursor = min(m.cursor, max(0, len(m.rows)-1))
	for index, row := range m.rows {
		if row.key() == preferred {
			m.cursor = index
			break
		}
	}
	if row, ok := m.current(); ok && row.Kind == "graph" && m.cursor+1 < len(m.rows) {
		m.cursor++
	}
	m.updateDetails()
}
func (m *workQueueTUI) updateDetails() {
	text := "No matching Work or Claims.\nPress / to search, r to refresh, ? for help."
	if row, ok := m.current(); ok {
		var err error
		text, err = workQueueDetails(m.state, row, m.readAt.UnixMilli())
		if err != nil {
			text = "Details unavailable: " + workQueueText(err.Error())
		}
	}
	m.details.SetContent(text)
	m.details.GotoTop()
}
func (m *workQueueTUI) move(delta int) {
	if len(m.rows) == 0 {
		return
	}
	m.cursor = max(0, min(len(m.rows)-1, m.cursor+delta))
	if row, ok := m.current(); ok && row.Kind == "graph" {
		if delta < 0 && m.cursor > 0 {
			m.cursor--
		} else if m.cursor+1 < len(m.rows) {
			m.cursor++
		}
	}
	m.updateDetails()
}

func (m *workQueueTUI) actionTargets() ([]workOperatorTarget, error) {
	chosen := []workQueueRow{}
	for _, row := range m.selected {
		chosen = append(chosen, row)
	}
	if row, ok := m.current(); ok && len(chosen) == 0 {
		chosen = append(chosen, row)
	}
	if len(chosen) == 0 {
		return nil, errors.New("no Work or Claim selected")
	}
	byWork := map[string]workOperatorTarget{}
	for _, row := range chosen {
		work := m.state.Works[row.WorkID]
		if work == nil {
			return nil, errors.New("selected Work disappeared; clear selection and refresh")
		}
		target := workOperatorTarget{WorkID: work.WorkID}
		if row.Kind == "claim" {
			claim := m.state.Claims[row.ID]
			if claim == nil || claim.State != "open" || work.State != "claimed" || work.ClaimID != claim.ClaimID {
				return nil, errors.New("selected Claim is historical or terminal; select current ownership")
			}
			target.ClaimID = claim.ClaimID
		}
		if previous, ok := byWork[work.WorkID]; ok && previous.ClaimID != "" {
			target = previous
		}
		byWork[work.WorkID] = target
	}
	if len(byWork) > 256 {
		return nil, errors.New("select at most 256 distinct Work per action")
	}
	targets := make([]workOperatorTarget, 0, len(byWork))
	for _, target := range byWork {
		targets = append(targets, target)
	}
	slices.SortFunc(targets, func(a, b workOperatorTarget) int { return strings.Compare(a.WorkID, b.WorkID) })
	return targets, nil
}
func (m *workQueueTUI) openAction(kind string) {
	targets, err := m.actionTargets()
	if err != nil {
		m.status = "Error: " + workQueueText(err.Error())
		return
	}
	for _, target := range targets {
		work := m.state.Works[target.WorkID]
		if kind == "priority" && work.State != "available" || kind == "cancel" && (work.State == "completed" || work.State == "cancelled") {
			m.status = "Error: selection contains ineligible Work; inspect or refresh before changing it"
			return
		}
	}
	m.targets, m.mode, m.priority = targets, kind, 0
	if kind == "cancel" {
		m.prepareAction()
	}
}
func (m *workQueueTUI) prepareAction() {
	reason := "operator_cancelled"
	if m.mode == "priority" {
		reason = "operator_reprioritized"
	}
	params, kind, err := workOperatorIntent(m.state, slices.Clone(m.targets), reason, m.priority, "")
	if err != nil {
		m.status, m.mode = "Error: "+workQueueText(err.Error()), ""
		return
	}
	m.pending, m.pendingKind = params, kind
}
func (m *workQueueTUI) confirm() tea.Cmd {
	var token [16]byte
	if _, err := rand.Read(token[:]); err != nil {
		m.status = "Error: cannot create stable request ID: " + workQueueText(err.Error())
		return nil
	}
	id := hex.EncodeToString(token[:])
	m.lastRequest, m.busy, m.publishing, m.mode = id, true, true, ""
	m.status = "Publishing checked action; request=" + id
	service, params, kind := m.service, m.pending, m.pendingKind
	return func() tea.Msg {
		_, err := service.publish(id, kind, params)
		if err != nil {
			return workQueueLoaded{err: err, requestID: id}
		}
		state, err := service.read()
		if err != nil {
			return workQueueLoaded{err: err, requestID: id, committed: true}
		}
		return workQueueLoaded{state: &state, requestID: id, committed: true}
	}
}

func (m *workQueueTUI) loaded(msg workQueueLoaded) {
	m.busy, m.publishing = false, false
	if msg.state != nil {
		preferred := ""
		if row, ok := m.current(); ok {
			preferred = row.key()
		}
		m.state, m.readAt = *msg.state, time.Now()
		m.rebuild(preferred)
		for key, row := range m.selected {
			work := m.state.Works[row.WorkID]
			if work == nil || work.State == "cancelled" || work.State == "completed" {
				delete(m.selected, key)
			}
		}
	}
	m.status = "Read-only committed snapshot; actions are revalidated through CAS"
	if msg.requestID != "" {
		m.lastRequest = msg.requestID
	}
	if msg.committed {
		m.status = "Committed; native reservations/debt retained. request=" + msg.requestID
	}
	if msg.err != nil {
		m.status = "Error (view may be stale): " + workQueueText(msg.err.Error())
		if msg.requestID != "" && !msg.committed {
			m.status += "; acknowledgment may be uncertain: inspect request=" + msg.requestID + " before retrying"
		}
		if msg.committed {
			m.status = "Committed request=" + msg.requestID + "; refresh failed: " + workQueueText(msg.err.Error())
		}
	}
}
func (m workQueueTUI) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	var command tea.Cmd
	switch msg := message.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.sizeDetails()
	case workQueueLoaded:
		m.loaded(msg)
	case workQueueTick:
		if !m.busy && m.mode == "" && !m.editing {
			m.busy = true
			command = tea.Batch(m.read(), workQueueNextTick())
		} else {
			command = workQueueNextTick()
		}
	case tea.KeyPressMsg:
		command = m.key(msg)
	default:
		if m.editing {
			m.search, command = m.search.Update(message)
		}
	}
	return m, command
}

func workQueueService(ctx context.Context, branch workqueue.Branch) workQueueTUIService {
	return workQueueTUIService{
		read: func() (workqueue.Projection, error) {
			ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			commits, err := branch.Read(ctx)
			if err != nil {
				return workqueue.Projection{}, err
			}
			return workqueue.Replay(commits)
		},
		publish: func(id, kind string, params workqueue.OperationsParameters) (workqueue.Publication, error) {
			ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			actor, err := branch.Authenticate(ctx, "administrator")
			if err != nil {
				return workqueue.Publication{}, err
			}
			request, err := workqueue.NewRequest(id, kind, actor, params)
			if err != nil {
				return workqueue.Publication{}, err
			}
			return branch.Publish(ctx, actor, request)
		},
	}
}
func workTUICommand() *cobra.Command {
	cmd := &cobra.Command{Use: "tui", Aliases: []string{"interactive"}, Short: "Keyboard-driven Work/Claim browser with live details and checked operator actions", Args: cobra.NoArgs}
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		jsonOutput, _ := cmd.Flags().GetBool("json")
		id, _ := cmd.Flags().GetString("request-id")
		if jsonOutput || id != "" {
			return errors.New("tui does not accept --json or --request-id; each action creates its own stable request ID")
		}
		if !tty.IsStdinTerminal() || !tty.IsStdoutTerminal() {
			return errors.New("tui requires interactive stdin/stdout terminals; use state --json or inspect for automation")
		}
		branch := workBranch(cmd)
		ctx, stop := context.WithCancel(cmd.Context())
		defer stop()
		model := newWorkQueueTUI(workQueueService(ctx, branch), branch.Name)
		final, err := tea.NewProgram(model, tea.WithContext(ctx), tea.WithInput(cmd.InOrStdin()), tea.WithOutput(cmd.OutOrStdout())).Run()
		if finished, ok := final.(workQueueTUI); ok && finished.lastRequest != "" {
			if _, writeErr := fmt.Fprintf(cmd.OutOrStdout(), "Last operator request=%s; use explain --request-id to inspect its acknowledgment.\n", finished.lastRequest); writeErr != nil {
				return writeErr
			}
		}
		if err != nil {
			return fmt.Errorf("work-queue TUI: %w", err)
		}
		return nil
	}
	return cmd
}
