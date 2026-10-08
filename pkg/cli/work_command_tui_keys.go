package cli

import (
	"strconv"

	tea "charm.land/bubbletea/v2"
)

func (m *workQueueTUI) searchKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "enter":
		m.filter.Query, m.editing = m.search.Value(), false
		m.search.Blur()
		m.rebuild("")
	case "esc":
		m.editing = false
		m.search.Blur()
	default:
		var command tea.Cmd
		m.search, command = m.search.Update(msg)
		return command
	}
	return nil
}

func (m *workQueueTUI) modalKey(msg tea.KeyPressMsg) tea.Cmd {
	key := msg.String()
	if key == "esc" || key == "n" {
		m.mode = ""
		m.sizeDetails()
		m.updateDetails()
		return nil
	}
	if m.mode == "help" {
		var command tea.Cmd
		m.details, command = m.details.Update(msg)
		return command
	}
	if value, err := strconv.Atoi(key); m.mode == "priority" && err == nil && value >= 1 && value <= 5 {
		m.priority = value
		m.prepareAction()
	}
	if key == "y" && (m.mode == "cancel" || m.mode == "priority" && m.priority != 0) {
		if !m.previewFits() {
			m.status = "Action disabled: resize or select a smaller batch so every target is visible"
			return nil
		}
		return m.confirm()
	}
	return nil
}

func (m *workQueueTUI) key(msg tea.KeyPressMsg) tea.Cmd {
	key := msg.String()
	if key == "ctrl+c" {
		return tea.Quit
	}
	if m.editing {
		return m.searchKey(msg)
	}
	if m.mode != "" {
		return m.modalKey(msg)
	}
	switch key {
	case "q":
		if !m.publishing {
			return tea.Quit
		}
		m.status = "Request in flight; wait, or Ctrl+C to quit with potentially uncertain acknowledgment"
	case "?":
		m.mode = "help"
		m.sizeDetails()
		m.details.SetContent(m.actionPreview())
		m.details.GotoTop()
	case "/":
		m.editing = true
		m.search.SetValue(m.filter.Query)
		return m.search.Focus()
	case "esc":
		m.filter.Query = ""
		clear(m.selected)
		m.rebuild("")
	case "tab", "enter":
		m.detailFocus = !m.detailFocus
	case "r":
		if !m.busy {
			m.busy = true
			m.status = "Refreshing committed state..."
			return m.read()
		}
	case "c", "p":
		if !m.busy {
			if key == "c" {
				m.openAction("cancel")
			} else {
				m.openAction("priority")
			}
		}
	default:
		if m.detailFocus {
			var command tea.Cmd
			m.details, command = m.details.Update(msg)
			return command
		}
		m.navigate(key)
	}
	return nil
}

func (m *workQueueTUI) navigate(key string) {
	switch key {
	case "up", "k":
		m.move(-1)
	case "down", "j":
		m.move(1)
	case "pgup":
		m.move(-max(1, m.height-10))
	case "pgdown":
		m.move(max(1, m.height-10))
	case "home", "g":
		m.cursor = 0
		m.move(0)
	case "end", "G":
		m.cursor = max(0, len(m.rows)-1)
		m.move(0)
	case "left", "h", "right", "l":
		if row, ok := m.current(); ok {
			m.collapsed[row.WorkID] = key == "left" || key == "h"
			m.rebuild("work:" + row.WorkID)
		}
	case "space", " ":
		if row, ok := m.current(); ok && row.Kind != "graph" {
			if _, selected := m.selected[row.key()]; selected {
				delete(m.selected, row.key())
			} else {
				m.selected[row.key()] = row
			}
		}
	}
}
