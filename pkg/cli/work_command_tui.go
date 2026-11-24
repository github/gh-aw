package cli

import (
	"context"
	"errors"
	"fmt"
	"slices"
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
	publish func(kind string, parameters any, operationsFor workqueue.OperationsFor) (workqueue.Publication, error)
	// maxAttempts resolves a pool's compiled max_attempts for release.
	maxAttempts func(string) int
}
type workQueueLoaded struct {
	state     *workqueue.Projection
	err       error
	requests  []string
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
	targets                                []string
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
		inspection, err := workQueueInspect(m.state, m.branch, row)
		text = inspection.Details
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

func (m *workQueueTUI) chosenRows() []workQueueRow {
	chosen := []workQueueRow{}
	for _, row := range m.selected {
		chosen = append(chosen, row)
	}
	if row, ok := m.current(); ok && len(chosen) == 0 && row.Kind != "graph" {
		chosen = append(chosen, row)
	}
	return chosen
}

// actionTargets resolves the selection to available Work IDs (cancel) or
// non-closed Dispatch IDs (release) of the selected Claims or claimed Work.
func (m *workQueueTUI) actionTargets(kind string) ([]string, error) {
	chosen := m.chosenRows()
	if len(chosen) == 0 {
		return nil, errors.New("no Work or Claim selected")
	}
	targets := []string{}
	for _, row := range chosen {
		work := m.state.Works[row.WorkID]
		if work == nil {
			return nil, errors.New("selected Work disappeared; clear selection and refresh")
		}
		if kind == "cancel" {
			if work.State != "available" {
				return nil, errors.New("cancel applies to available Work only; release claimed Work's dispatch first")
			}
			targets = append(targets, work.WorkID)
			continue
		}
		claimID := work.ClaimID
		if row.Kind == "claim" {
			claimID = row.ID
		}
		claim := m.state.Claims[claimID]
		if claim == nil || claim.State != "open" {
			return nil, errors.New("release applies to open Claims; select claimed Work or its current Claim")
		}
		if dispatch := m.state.Dispatches[claim.DispatchID]; dispatch != nil && dispatch.State != "closed" {
			targets = append(targets, dispatch.DispatchID)
		}
	}
	targets = slices.Compact(slices.Sorted(slices.Values(targets)))
	if len(targets) > workqueue.OperationsPerCommit {
		return nil, errors.New("select at most 256 targets per action")
	}
	return targets, nil
}
func (m *workQueueTUI) openAction(kind string) {
	targets, err := m.actionTargets(kind)
	if err != nil {
		m.status = "Error: " + workQueueText(err.Error())
		return
	}
	m.targets, m.mode = targets, kind
}
func (m *workQueueTUI) confirm() tea.Cmd {
	m.busy, m.publishing = true, true
	m.status = "Publishing checked action..."
	service, kind, targets := m.service, m.mode, slices.Clone(m.targets)
	m.mode = ""
	return func() tea.Msg {
		requests := []string{}
		publish := func(kind string, parameters any, operationsFor workqueue.OperationsFor) error {
			published, err := service.publish(kind, parameters, operationsFor)
			if err == nil && published.Committed {
				requests = append(requests, published.Request.ID)
			}
			return err
		}
		var err error
		if kind == "cancel" {
			err = publish("cancel_work", workqueue.CancelWorkParameters{WorkIDs: targets, Reason: "operator"},
				func(state workqueue.Projection, _ workqueue.Request) ([]workqueue.Operation, error) {
					return workCancelOperations(state, targets, "operator")
				})
		} else {
			for _, id := range targets {
				if err = publish("release", workqueue.ReleaseParameters{DispatchID: id, Reason: "operator"},
					func(state workqueue.Projection, _ workqueue.Request) ([]workqueue.Operation, error) {
						return workReleaseOperations(state, id, "operator", service.maxAttempts)
					}); err != nil {
					break
				}
			}
		}
		if err != nil {
			return workQueueLoaded{err: err, requests: requests, committed: len(requests) > 0}
		}
		state, err := service.read()
		if err != nil {
			return workQueueLoaded{err: err, requests: requests, committed: true}
		}
		return workQueueLoaded{state: &state, requests: requests, committed: true}
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
	// Requests are in publication order; the last one is reported on exit.
	for _, request := range msg.requests {
		m.lastRequest = request
	}
	if msg.committed {
		m.status = fmt.Sprintf("Committed %d requests; running workflows are not stopped", len(msg.requests))
	}
	if msg.err != nil {
		m.status = "Error (view may be stale): " + workQueueText(msg.err.Error())
		if msg.committed {
			m.status = fmt.Sprintf("Committed %d requests, then: %s", len(msg.requests), workQueueText(msg.err.Error()))
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

func workQueueService(ctx context.Context, branch workqueue.Branch, actor workqueue.Actor) workQueueTUIService {
	return workQueueTUIService{
		read: func() (workqueue.Projection, error) {
			ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			snapshot, err := branch.Read(ctx)
			if err != nil {
				return workqueue.Projection{}, err
			}
			if snapshot.State.Repository == "" {
				snapshot.State.Repository = branch.Remote
			}
			return snapshot.State, nil
		},
		publish: func(kind string, parameters any, operationsFor workqueue.OperationsFor) (workqueue.Publication, error) {
			ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			return branch.Publish(ctx, actor, kind, parameters, operationsFor)
		},
		maxAttempts: workMaxAttempts(),
	}
}
func workTUICommand() *cobra.Command {
	cmd := &cobra.Command{Use: "tui", Aliases: []string{"interactive"}, Short: "Keyboard-driven Work/Claim browser with live details and checked operator actions", Args: cobra.NoArgs}
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		if jsonOutput, _ := cmd.Flags().GetBool("json"); jsonOutput {
			return errors.New("tui does not accept --json; use state --json or inspect for automation")
		}
		if !tty.IsStdinTerminal() || !tty.IsStdoutTerminal() {
			return errors.New("tui requires interactive stdin/stdout terminals; use state --json or inspect for automation")
		}
		branch := workBranch(cmd)
		ctx, stop := context.WithCancel(cmd.Context())
		defer stop()
		model := newWorkQueueTUI(workQueueService(ctx, branch, workActor(cmd)), branch.Name)
		final, err := tea.NewProgram(model, tea.WithContext(ctx), tea.WithInput(cmd.InOrStdin()), tea.WithOutput(cmd.OutOrStdout())).Run()
		if finished, ok := final.(workQueueTUI); ok && finished.lastRequest != "" {
			if _, writeErr := fmt.Fprintf(cmd.OutOrStdout(), "Last operator request=%s; use inspect to review its effect.\n", finished.lastRequest); writeErr != nil {
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
