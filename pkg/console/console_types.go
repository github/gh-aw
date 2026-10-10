package console

// ErrorPosition represents a position in a source file
type ErrorPosition struct {
	File   string
	Line   int
	Column int
}

// CompilerError represents a structured compiler error with position information
type CompilerError struct {
	Position ErrorPosition
	Type     string // "error", "warning", "info"
	Message  string
	Context  []string // Source code lines for context
	Hint     string   // Optional hint for fixing the error
}

// TableConfig represents configuration for table rendering
type TableConfig struct {
	Headers   []string
	Rows      [][]string
	Title     string
	ShowTotal bool
	TotalRow  []string
	// MaxWidth bounds native table output in display columns. Zero keeps the
	// existing unbounded layout; positive values below two use two columns.
	MaxWidth int
	// TTYFunc overrides the renderer's default destination TTY check.
	TTYFunc func() bool
}

// RenderOptions configures the destination and width of reflected console output.
type RenderOptions struct {
	Stderr   bool
	MaxWidth int
}

// TreeNode represents a node in a hierarchical tree structure
type TreeNode struct {
	Value    string
	Children []TreeNode
}

// SelectOption represents a selectable option with a label and value
type SelectOption struct {
	Label string
	Value string
}

// FormField represents a generic form field configuration
type FormField struct {
	Type        string // "input", "password", "confirm", "select"
	Title       string
	Description string
	Placeholder string
	Value       any                // Pointer to the value to store the result
	Options     []SelectOption     // For select fields
	Validate    func(string) error // For input/password fields
}

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
