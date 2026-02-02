package console

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/styles"
	"github.com/github/gh-aw/pkg/tty"
)

var listLog = logger.New("console:list")

// ListItem represents an item in an interactive list
type ListItem struct {
	title       string
	description string
	value       string
}

// NewListItem creates a new list item with title, description, and value
func NewListItem(title, description, value string) ListItem {
	return ListItem{
		title:       title,
		description: description,
		value:       value,
	}
}

// Title returns the item's title
func (i ListItem) Title() string { return i.title }

// Description returns the item's description
func (i ListItem) Description() string { return i.description }

// FilterValue returns the value used for filtering
func (i ListItem) FilterValue() string { return i.title }

// listModel is the Bubble Tea model for the interactive list
type listModel struct {
	list     list.Model
	choice   string
	quitting bool
}

// Init initializes the list model
func (m listModel) Init() tea.Cmd {
	return nil
}

// Update handles messages and updates the model
func (m listModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q", "esc":
			m.quitting = true
			return m, tea.Quit
		case "enter":
			if i, ok := m.list.SelectedItem().(ListItem); ok {
				m.choice = i.value
			}
			return m, tea.Quit
		}
	case tea.WindowSizeMsg:
		m.list.SetWidth(msg.Width)
		return m, nil
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

// View renders the list
func (m listModel) View() string {
	if m.quitting {
		return ""
	}
	return m.list.View()
}

// itemDelegate is a custom delegate for list items
type itemDelegate struct{}

// Height returns the height of a list item
func (d itemDelegate) Height() int { return 1 }

// Spacing returns the spacing between items
func (d itemDelegate) Spacing() int { return 0 }

// Update handles item-specific updates
func (d itemDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }

// Render renders a list item with custom styling
func (d itemDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	i, ok := listItem.(ListItem)
	if !ok {
		return
	}

	// Style for selected item
	selectedStyle := lipgloss.NewStyle().
		Foreground(styles.ColorSuccess).
		Bold(true)

	// Style for normal item title
	titleStyle := lipgloss.NewStyle().
		Foreground(styles.ColorForeground)

	// Style for description
	descStyle := lipgloss.NewStyle().
		Foreground(styles.ColorComment).
		Italic(true)

	// Check if this item is selected
	isSelected := index == m.Index()

	var str strings.Builder

	// Render cursor or spacer
	if isSelected {
		str.WriteString(selectedStyle.Render("> "))
	} else {
		str.WriteString("  ")
	}

	// Render title
	if isSelected {
		str.WriteString(selectedStyle.Render(i.title))
	} else {
		str.WriteString(titleStyle.Render(i.title))
	}
	str.WriteString("\n")

	// Render description if present
	if i.description != "" {
		if isSelected {
			str.WriteString("  " + selectedStyle.Render(i.description))
		} else {
			str.WriteString("  " + descStyle.Render(i.description))
		}
	}

	fmt.Fprint(w, str.String())
}

// ShowInteractiveList displays an interactive list with arrow key navigation
// Returns the selected item's value, or an error if cancelled or failed
func ShowInteractiveList(title string, items []ListItem) (string, error) {
	listLog.Printf("Showing interactive list: title=%s, items=%d", title, len(items))

	if len(items) == 0 {
		return "", fmt.Errorf("no items to display")
	}

	// Check if we're in a TTY environment
	if !tty.IsStderrTerminal() {
		listLog.Print("Non-TTY detected, falling back to text list")
		return showTextList(title, items)
	}

	// Convert ListItem to list.Item interface
	listItems := make([]list.Item, len(items))
	for i, item := range items {
		listItems[i] = item
	}

	// Create the list with custom delegate
	delegate := itemDelegate{}
	l := list.New(listItems, delegate, 80, 20)
	l.Title = title
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(true)
	l.SetShowHelp(true)

	// Customize list styles
	l.Styles.Title = lipgloss.NewStyle().
		Foreground(styles.ColorInfo).
		Bold(true).
		Padding(0, 0, 1, 0)

	l.Styles.FilterPrompt = lipgloss.NewStyle().
		Foreground(styles.ColorInfo)

	l.Styles.FilterCursor = lipgloss.NewStyle().
		Foreground(styles.ColorSuccess)

	// Create the model and run the program
	m := listModel{list: l}
	p := tea.NewProgram(m, tea.WithAltScreen())

	finalModel, err := p.Run()
	if err != nil {
		listLog.Printf("Error running list program: %v", err)
		return "", fmt.Errorf("failed to run interactive list: %w", err)
	}

	// Check if user cancelled
	result := finalModel.(listModel)
	if result.quitting && result.choice == "" {
		return "", fmt.Errorf("selection cancelled")
	}

	listLog.Printf("Selected item: %s", result.choice)
	return result.choice, nil
}

// showTextList displays a non-interactive numbered list for non-TTY environments
func showTextList(title string, items []ListItem) (string, error) {
	listLog.Printf("Showing text list: title=%s, items=%d", title, len(items))

	fmt.Fprintf(os.Stderr, "\n%s\n\n", title)
	for i, item := range items {
		fmt.Fprintf(os.Stderr, "  %d) %s\n", i+1, item.title)
		if item.description != "" {
			fmt.Fprintf(os.Stderr, "     %s\n", item.description)
		}
	}
	fmt.Fprintf(os.Stderr, "\nSelect (1-%d): ", len(items))

	var choice int
	_, err := fmt.Scanf("%d", &choice)
	if err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}

	if choice < 1 || choice > len(items) {
		return "", fmt.Errorf("selection out of range (must be 1-%d)", len(items))
	}

	selectedItem := items[choice-1]
	listLog.Printf("Selected item from text list: %s", selectedItem.value)
	return selectedItem.value, nil
}
