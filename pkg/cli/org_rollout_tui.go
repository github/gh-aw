package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/github/gh-aw/pkg/stringutil"
)

type orgRolloutTUI struct {
	ctx            context.Context
	cancel         context.CancelFunc
	rows           []orgRolloutRow
	opts           orgRolloutOptions
	events         <-chan orgRolloutEvent
	results        []orgRolloutResult
	cursor, height int
	width          int
	filter         string
	searching      bool
	running, done  bool
	canceled       bool
}

type orgRolloutCanceled struct{}

func rolloutText(text string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, stringutil.StripANSI(text))
}

func (m orgRolloutTUI) visible() []int {
	var indices []int
	for index, row := range m.rows {
		if strings.Contains(strings.ToLower(row.repo), strings.ToLower(m.filter)) {
			indices = append(indices, index)
		}
	}
	return indices
}

func (m orgRolloutTUI) Init() tea.Cmd {
	return func() tea.Msg {
		<-m.ctx.Done()
		return orgRolloutCanceled{}
	}
}

func (m orgRolloutTUI) next() tea.Cmd {
	return func() tea.Msg {
		event, ok := <-m.events
		if !ok {
			return orgRolloutEvent{done: true}
		}
		return event
	}
}

func (m orgRolloutTUI) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := message.(type) {
	case orgRolloutCanceled:
		m.canceled = true
		m.cancel()
		if !m.running {
			return m, tea.Quit
		}
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case orgRolloutEvent:
		if msg.done {
			m.running, m.done = false, true
			for index, row := range m.rows {
				if row.selected && (row.status == "ready" || row.status == "replace existing PR" ||
					row.status == "replacement allowed; space to select") {
					row.status = "not run"
					m.rows[index] = row
				}
			}
			if m.canceled {
				return m, tea.Quit
			}
			return m, nil
		}
		if msg.index >= 0 && msg.index < len(m.rows) {
			m.rows[msg.index].status = msg.result.Status
			if msg.result.URL != "" {
				m.rows[msg.index].status += " " + msg.result.URL
			}
			if msg.result.Preview != nil && m.opts.command == "upgrade" {
				m.rows[msg.index].status += " | compilers: " + formatUpgradeCompilerVersions(*msg.result.Preview)
			}
		}
		if msg.result.Status != "running" {
			m.results = append(m.results, msg.result)
		}
		return m, m.next()
	case tea.KeyPressMsg:
		return m.key(msg)
	}
	return m, nil
}

func (m orgRolloutTUI) key(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+c" || (!m.searching && (msg.String() == "q" || msg.String() == "esc")) {
		if !m.done {
			m.canceled = true
		}
		m.cancel()
		if !m.running {
			return m, tea.Quit
		}
		return m, nil
	}
	if m.searching {
		switch msg.String() {
		case "enter", "esc":
			m.searching = false
		case "backspace":
			runes := []rune(m.filter)
			if len(runes) > 0 {
				m.filter = string(runes[:len(runes)-1])
			}
		default:
			m.filter += msg.Text
		}
		m.cursor = 0
		return m, nil
	}
	switch msg.String() {
	case "up", "k":
		m.cursor = max(0, m.cursor-1)
	case "down", "j":
		m.cursor = min(max(0, len(m.visible())-1), m.cursor+1)
	case "/":
		m.searching = true
	case "enter":
		if m.done {
			return m, tea.Quit
		}
		if !m.running {
			m.running = true
			m.events = startOrgRolloutJobs(m.ctx, m.rows, m.opts, time.Second, runOrgRolloutWorker)
			return m, m.next()
		}
	default:
		m.selectionKey(msg.String())
	}
	return m, nil
}

func (m *orgRolloutTUI) selectionKey(key string) {
	if m.running || m.done {
		return
	}
	visible := m.visible()
	switch key {
	case "space":
		if m.cursor >= 0 && m.cursor < len(visible) {
			index := visible[m.cursor]
			if index < 0 || index >= len(m.rows) {
				return
			}
			row := &m.rows[index]
			if len(row.prs) == 0 || m.opts.replace {
				row.selected = !row.selected
			}
		}
	case "a":
		all := true
		for _, index := range visible {
			if index < 0 || index >= len(m.rows) {
				continue
			}
			row := m.rows[index]
			if (len(row.prs) == 0 || m.opts.replace) && !row.selected {
				all = false
			}
		}
		for _, index := range visible {
			if index < 0 || index >= len(m.rows) {
				continue
			}
			row := &m.rows[index]
			row.selected = !all && (len(row.prs) == 0 || m.opts.replace)
		}
	case "o":
		m.opts.replace = !m.opts.replace
		for index := range m.rows {
			row := &m.rows[index]
			if len(row.prs) > 0 {
				row.selected = false
				row.status = "existing PR"
				if m.opts.replace {
					row.status = "replacement allowed; space to select"
				}
			}
		}
	}
}

func (m orgRolloutTUI) View() tea.View {
	var text strings.Builder
	mode := "Create pull requests"
	if m.opts.dryRun {
		mode = "Dry-run preview (no remote writes)"
	}
	header := fmt.Sprintf("%s | workers: %d | replace existing: %t\n", mode, m.opts.concurrency, m.opts.replace)
	text.WriteString(header)
	selected := 0
	for _, row := range m.rows {
		if row.selected {
			selected++
		}
	}
	summary := fmt.Sprintf("%d selected | %d completed | filter: %s\n\n", selected, len(m.results), rolloutText(m.filter))
	text.WriteString(summary)
	visible := m.visible()
	height := max(1, m.height-9)
	top := max(0, min(m.cursor-height/2, len(visible)-height))
	for offset, index := range visible[top:] {
		if offset >= height {
			break
		}
		if index < 0 || index >= len(m.rows) {
			continue
		}
		row := m.rows[index]
		cursor, checked := " ", " "
		if top+offset == m.cursor {
			cursor = ">"
		}
		if row.selected {
			checked = "x"
		}
		status := row.status
		if len(row.prs) > 0 {
			status += fmt.Sprintf(" (#%d)", row.prs[0].Number)
		}
		line := fmt.Sprintf("%s [%s] %s  %s", cursor, checked, row.repo, status)
		text.WriteString(ansi.Truncate(rolloutText(line), max(1, m.width), "...") + "\n")
	}
	if m.running {
		text.WriteString("\nRunning. q / ctrl+c: cancel pending and active workers; waiting for cleanup.\n")
	} else if m.done {
		text.WriteString("\nFinished. enter / q: close and print results.\n")
	} else {
		text.WriteString("\nspace: select  a: toggle filtered  /: filter  o: allow replacement\nenter: run selected  q: cancel\n")
	}
	view := tea.NewView(text.String())
	view.AltScreen = true
	return view
}

func runOrgRolloutTUI(ctx context.Context, rows []orgRolloutRow, opts orgRolloutOptions) ([]orgRolloutResult, error) {
	// Do not give Bubble Tea the worker context: it must drain results after
	// cancellation before quitting, so no worker can block on an abandoned channel.
	workCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	model := orgRolloutTUI{ctx: workCtx, cancel: cancel, rows: rows, opts: opts, width: 100, height: 30}
	final, err := tea.NewProgram(model, tea.WithInput(os.Stdin), tea.WithOutput(os.Stderr)).Run()
	if err != nil {
		cancel()
		// If rendering fails after execution started, drain all worker events.
		if state, ok := final.(orgRolloutTUI); ok && state.events != nil {
			for range state.events {
			}
		}
		return nil, fmt.Errorf("rollout TUI: %w", err)
	}
	state, ok := final.(orgRolloutTUI)
	if !ok {
		return nil, errors.New("rollout TUI returned an unexpected model")
	}
	if state.canceled {
		return state.results, context.Canceled
	}
	return state.results, nil
}
