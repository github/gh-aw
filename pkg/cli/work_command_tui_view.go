package cli

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

var workQueueBorder = lipgloss.Border{Top: "-", Bottom: "-", Left: "|", Right: "|", TopLeft: "+", TopRight: "+", BottomLeft: "+", BottomRight: "+"}

func (m *workQueueTUI) sizeDetails() {
	width, height := max(1, m.width-4), max(1, m.height-11)
	if m.width >= 100 && m.mode != "help" {
		width = max(1, m.width-m.width/2-2)
	}
	m.details.SetWidth(width)
	m.details.SetHeight(height)
	m.search.SetWidth(max(1, m.width-8))
}

func (m workQueueTUI) listView(width, height int) string {
	lines := []string{}
	top := max(0, min(m.cursor-height/2, len(m.rows)-height))
	for offset, row := range m.rows[top:] {
		if len(lines) >= height {
			break
		}
		index := top + offset
		marker := "   "
		if _, ok := m.selected[row.key()]; ok {
			marker = "[x]"
		}
		cursor := " "
		if index == m.cursor {
			cursor = ">"
		}
		line := lipgloss.NewStyle().MaxWidth(width).Render(cursor + " " + marker + " " + m.compactRow(row))
		if index == m.cursor {
			line = lipgloss.NewStyle().Reverse(true).Render(line)
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		lines = append(lines, "No matching Work/Claims. / search; r refresh.")
	}
	return strings.Join(lines, "\n")
}

func (m workQueueTUI) compactRow(row workQueueRow) string {
	switch row.Kind {
	case "work":
		work := m.state.Works[row.WorkID]
		key := []rune(work.NodeKey)
		if len(key) > 18 {
			key = append(key[:18], '.', '.', '.')
		}
		return fmt.Sprintf("+-- W %s [%s P%d] delivery=%s", workQueueText(string(key)), work.State, work.SchedulingPriority(), work.Barrier)
	case "claim":
		id := []rune(row.ID)
		if len(id) > 12 {
			id = append(id[:12], '.', '.', '.')
		}
		owner := "history"
		if row.Current {
			owner = "current"
		}
		return fmt.Sprintf("|   \\-- C %s [%s %s]", workQueueText(string(id)), row.State, owner)
	default:
		return row.Text
	}
}

func (m workQueueTUI) actionPreview() string {
	if m.mode == "help" {
		return "KEYBOARD HELP\n\nUp/Down, j/k    Navigate Work and Claim attempts\nLeft/Right     Collapse/expand Claim history\nSpace          Toggle selection (including across search)\n/              Search IDs and metadata; Enter applies\nTab or Enter   Focus details; scroll with arrows/PgUp/PgDn\nr              Refresh; automatic reads every 10 seconds\nc              Review terminal cancellation of selection/cursor\np              Review priority change for available Work\nEsc            Close dialog; otherwise clear search/selection\nq              Quit (Ctrl+C may leave acknowledgment uncertain)\n\nForest branches show graph membership and Claim attempts.\nDependency edges are explicit cross-references in details.\nNo direct Claim grants, worker finish or native force-release.\n\nEsc closes help."
	}
	title := "CANCEL WORK (terminal, not retry)"
	explanation := "Selected current Claims lose authority; Work becomes cancelled.\nNative workers are NOT stopped or force-released. Debt is NOT refunded."
	if m.mode == "priority" {
		title = "REPRIORITIZE AVAILABLE WORK"
		explanation = "Choose 1..5 (1 highest; weighted fairness, not strict priority).\nOnly future grants change; original metadata, age and debt remain."
		if m.priority != 0 {
			explanation += fmt.Sprintf("\nNew priority: %d", m.priority)
		}
	}
	lines := []string{title, "Authority: " + workQueueText(m.state.Repository) + " branch=" + workQueueText(m.branch), "", explanation, "", fmt.Sprintf("Exact targets: %d Work (including hidden selections)", len(m.targets))}
	for _, target := range m.targets {
		line := "Work " + workQueueText(target.WorkID)
		if target.ClaimID != "" {
			line += " via Claim " + workQueueText(target.ClaimID)
		}
		lines = append(lines, line)
	}
	lines = append(lines, "", "y publish checked atomic action | n/Esc return without changes")
	// Never hide targets behind an unscrollable destructive confirmation.
	return strings.Join(lines, "\n")
}

func (m workQueueTUI) previewFits() bool {
	return m.width >= 45 && m.height >= 16 &&
		lipgloss.Height(lipgloss.NewStyle().Width(max(1, m.width-4)).Render(m.actionPreview())) <= m.height-10
}

func (m workQueueTUI) modalView() string {
	if m.mode == "help" {
		return m.details.View()
	}
	if m.mode != "help" && !m.previewFits() {
		return fmt.Sprintf("ACTION DISABLED\n\n%d targets exceed this confirmation viewport.\nClear the selection and use smaller batches or the explicit CLI.\n\nn/Esc returns without changes.", len(m.targets))
	}
	return m.actionPreview()
}

func (m workQueueTUI) View() tea.View {
	m.sizeDetails()
	if m.width < 45 || m.height < 16 {
		view := tea.NewView(lipgloss.NewStyle().MaxWidth(max(1, m.width)).MaxHeight(max(1, m.height)).Render("Work queue: terminal too small (minimum 45x16).\nResize, or q to quit."))
		view.AltScreen = true
		return view
	}
	width, height := m.width, m.height-10
	s := m.state.Stats
	header := fmt.Sprintf("WORK QUEUE %s branch=%s\nWork %d  available %d  claimed %d  completed %d  cancelled %d | Claims %d | native %d\n",
		workQueueText(m.state.Repository), workQueueText(m.branch), s.Work, s.Available, s.Claimed, s.Completed, s.Cancelled, s.Claims, s.Dispatches)
	header += fmt.Sprintf("admission paused=%t | grants paused=%t | read %s | selected %d\n", m.state.AdmissionPaused, m.state.GrantsPaused, m.readAt.Format("15:04:05"), len(m.selected))
	if m.editing {
		header += m.search.View() + "\n"
	} else {
		header += "Search " + workQueueText(m.filter.Query) + " | / edit | Esc clear\n"
	}
	frame := lipgloss.NewStyle().Border(workQueueBorder)
	body := ""
	if m.mode != "" {
		body = frame.Width(width - 2).Height(height).Render(m.modalView())
	} else if width >= 100 {
		listWidth, detailWidth := width/2-2, width-width/2-2
		listTitle, detailTitle := "FOREST [focused]", "DETAILS [Tab to focus]"
		if m.detailFocus {
			listTitle, detailTitle = "FOREST [Tab to focus]", "DETAILS [focused]"
		}
		left := frame.Width(listWidth).Height(height).Render(listTitle + "\n" + m.listView(listWidth, height-1))
		right := frame.Width(detailWidth).Height(height).Render(detailTitle + "\n" + m.details.View())
		body = lipgloss.JoinHorizontal(lipgloss.Top, left, right)
	} else if m.detailFocus {
		body = frame.Width(width - 2).Height(height).Render("DETAILS [Tab returns to forest]\n" + m.details.View())
	} else {
		body = frame.Width(width - 2).Height(height).Render("FOREST [Tab shows cursor details]\n" + m.listView(width-2, height-1))
	}
	status := m.status
	if m.busy {
		status = "BUSY | " + status
	}
	footer := status + "\n"
	if m.lastRequest != "" {
		footer += "Last action request=" + m.lastRequest + "\n"
	}
	footer += "j/k move | Space select | Tab details | / search | c cancel | p priority | r refresh | ? help | q quit"
	content := lipgloss.NewStyle().MaxWidth(width).MaxHeight(m.height).Render(header + body + "\n" + footer)
	view := tea.NewView(content)
	view.AltScreen = true
	return view
}
