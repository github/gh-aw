package console

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"github.com/charmbracelet/lipgloss/tree"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/styles"
	"github.com/github/gh-aw/pkg/tty"
)

var consoleLog = logger.New("console:console")

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

// isTTY checks if stdout is a terminal
func isTTY() bool {
	return tty.IsStdoutTerminal()
}

// applyStyle conditionally applies styling based on TTY status
func applyStyle(style lipgloss.Style, text string) string {
	if isTTY() {
		return style.Render(text)
	}
	return text
}

// ToRelativePath converts an absolute path to a relative path from the current working directory
// If the relative path contains "..", returns the absolute path instead for clarity
func ToRelativePath(path string) string {
	if !filepath.IsAbs(path) {
		return path
	}

	wd, err := os.Getwd()
	if err != nil {
		// If we can't get the working directory, return the original path
		return path
	}

	relPath, err := filepath.Rel(wd, path)
	if err != nil {
		// If we can't get a relative path, return the original path
		return path
	}

	// If the relative path contains "..", use absolute path instead for clarity
	if strings.Contains(relPath, "..") {
		return path
	}

	return relPath
}

// FormatError formats a CompilerError with Rust-like rendering
func FormatError(err CompilerError) string {
	consoleLog.Printf("Formatting error: type=%s, file=%s, line=%d", err.Type, err.Position.File, err.Position.Line)
	var output strings.Builder

	// Get style based on error type
	var typeStyle lipgloss.Style
	var prefix string
	switch err.Type {
	case "warning":
		typeStyle = styles.Warning
		prefix = "warning"
	case "info":
		typeStyle = styles.Info
		prefix = "info"
	default:
		typeStyle = styles.Error
		prefix = "error"
	}

	// IDE-parseable format: file:line:column: type: message
	if err.Position.File != "" {
		relativePath := ToRelativePath(err.Position.File)
		location := fmt.Sprintf("%s:%d:%d:",
			relativePath,
			err.Position.Line,
			err.Position.Column)
		output.WriteString(applyStyle(styles.FilePath, location))
		output.WriteString(" ")
	}

	// Error type and message
	output.WriteString(applyStyle(typeStyle, prefix+":"))
	output.WriteString(" ")
	output.WriteString(err.Message)
	output.WriteString("\n")

	// Context lines (Rust-like error rendering)
	if len(err.Context) > 0 && err.Position.Line > 0 {
		output.WriteString(renderContext(err))
	}

	// Remove hints as per requirements - hints are no longer displayed

	return output.String()
}

// findWordEnd finds the end of a word starting at the given position
// A word ends at whitespace, punctuation, or end of line
func findWordEnd(line string, start int) int {
	if start >= len(line) {
		return len(line)
	}

	end := start
	for end < len(line) {
		char := line[end]
		// Stop at whitespace or common punctuation that would end a YAML key/value
		if char == ' ' || char == '\t' || char == ':' || char == '\n' || char == '\r' {
			break
		}
		end++
	}

	return end
}

// renderContext renders source code context with line numbers and highlighting
func renderContext(err CompilerError) string {
	var output strings.Builder

	// Calculate line number width for padding
	maxLineNum := err.Position.Line + len(err.Context)/2
	lineNumWidth := len(fmt.Sprintf("%d", maxLineNum))

	for i, line := range err.Context {
		// Calculate actual line number (context usually centers around error line)
		lineNum := err.Position.Line - len(err.Context)/2 + i
		if lineNum < 1 {
			continue
		}

		// Format line number with proper padding
		lineNumStr := fmt.Sprintf("%*d", lineNumWidth, lineNum)
		output.WriteString(applyStyle(styles.LineNumber, lineNumStr))
		output.WriteString(" | ")

		// Highlight the error line
		if lineNum == err.Position.Line {
			// For JSON validation errors, highlight from column to end of word
			if err.Position.Column > 0 && err.Position.Column <= len(line) {
				before := line[:err.Position.Column-1]

				// Find the end of the word starting at the column position
				wordEnd := findWordEnd(line, err.Position.Column-1)
				highlightedPart := line[err.Position.Column-1 : wordEnd]
				after := ""
				if wordEnd < len(line) {
					after = line[wordEnd:]
				}

				output.WriteString(applyStyle(styles.ContextLine, before))
				output.WriteString(applyStyle(styles.Highlight, highlightedPart))
				output.WriteString(applyStyle(styles.ContextLine, after))
			} else {
				// Highlight entire line if no specific column or invalid column
				output.WriteString(applyStyle(styles.Highlight, line))
			}
		} else {
			output.WriteString(applyStyle(styles.ContextLine, line))
		}
		output.WriteString("\n")

		// Add pointer to error position (only when highlighting specific column)
		if lineNum == err.Position.Line && err.Position.Column > 0 && err.Position.Column <= len(line) {
			// Create pointer line that spans the highlighted word
			wordEnd := findWordEnd(line, err.Position.Column-1)
			wordLength := wordEnd - (err.Position.Column - 1)

			padding := strings.Repeat(" ", lineNumWidth+3+err.Position.Column-1)
			pointer := applyStyle(styles.Error, strings.Repeat("^", wordLength))
			output.WriteString(padding)
			output.WriteString(pointer)
			output.WriteString("\n")
		}
	}

	return output.String()
}

// FormatSuccessMessage formats a success message with styling
func FormatSuccessMessage(message string) string {
	return applyStyle(styles.Success, "✓ ") + message
}

// FormatInfoMessage formats an informational message
func FormatInfoMessage(message string) string {
	return applyStyle(styles.Info, "ℹ ") + message
}

// FormatWarningMessage formats a warning message
func FormatWarningMessage(message string) string {
	return applyStyle(styles.Warning, "⚠ ") + message
}

// TableConfig represents configuration for table rendering
type TableConfig struct {
	Headers   []string
	Rows      [][]string
	Title     string
	ShowTotal bool
	TotalRow  []string
}

// RenderTable renders a formatted table using lipgloss/table package
func RenderTable(config TableConfig) string {
	if len(config.Headers) == 0 {
		consoleLog.Print("No headers provided for table rendering")
		return ""
	}

	consoleLog.Printf("Rendering table: title=%s, columns=%d, rows=%d", config.Title, len(config.Headers), len(config.Rows))
	var output strings.Builder

	// Title
	if config.Title != "" {
		output.WriteString(applyStyle(styles.TableTitle, config.Title))
		output.WriteString("\n")
	}

	// Build rows including total row if specified
	allRows := config.Rows
	if config.ShowTotal && len(config.TotalRow) > 0 {
		allRows = append(allRows, config.TotalRow)
	}

	// Determine row count for styling purposes
	dataRowCount := len(config.Rows)

	// Create style function that applies different styles based on row type
	styleFunc := func(row, col int) lipgloss.Style {
		if !isTTY() {
			return lipgloss.NewStyle()
		}
		if row == table.HeaderRow {
			// Add horizontal padding to header cells for better spacing
			headerStyle := styles.TableHeader
			return headerStyle.PaddingLeft(1).PaddingRight(1)
		}
		// If we have a total row and this is the last row
		if config.ShowTotal && len(config.TotalRow) > 0 && row == dataRowCount {
			// Add horizontal padding to total row cells
			totalStyle := styles.TableTotal
			return totalStyle.PaddingLeft(1).PaddingRight(1)
		}
		// Zebra striping: alternate row colors
		if row%2 == 0 {
			// Add horizontal padding to even row cells
			cellStyle := styles.TableCell
			return cellStyle.PaddingLeft(1).PaddingRight(1)
		}
		// Odd rows with subtle background and horizontal padding
		return lipgloss.NewStyle().
			Foreground(styles.ColorForeground).
			Background(styles.ColorTableAltRow).
			PaddingLeft(1).
			PaddingRight(1)
	}

	// Create table with lipgloss/table package
	t := table.New().
		Headers(config.Headers...).
		Rows(allRows...).
		Border(styles.RoundedBorder).
		BorderStyle(styles.TableBorder).
		StyleFunc(styleFunc)

	output.WriteString(t.String())
	output.WriteString("\n")

	return output.String()
}

// FormatLocationMessage formats a file/directory location message
func FormatLocationMessage(message string) string {
	return applyStyle(styles.Location, "📁 ") + message
}

// FormatCommandMessage formats a command execution message
func FormatCommandMessage(command string) string {
	return applyStyle(styles.Command, "⚡ ") + command
}

// FormatProgressMessage formats a progress/activity message
func FormatProgressMessage(message string) string {
	return applyStyle(styles.Progress, "🔨 ") + message
}

// FormatPromptMessage formats a user prompt message
func FormatPromptMessage(message string) string {
	return applyStyle(styles.Prompt, "❓ ") + message
}

// FormatCountMessage formats a count/numeric status message
func FormatCountMessage(message string) string {
	return applyStyle(styles.Count, "📊 ") + message
}

// FormatVerboseMessage formats verbose debugging output
func FormatVerboseMessage(message string) string {
	return applyStyle(styles.Verbose, "🔍 ") + message
}

// FormatListHeader formats a section header for lists
func FormatListHeader(header string) string {
	return applyStyle(styles.ListHeader, header)
}

// FormatListItem formats an item in a list
func FormatListItem(item string) string {
	return applyStyle(styles.ListItem, "  • "+item)
}

// FormatErrorMessage formats a simple error message (for stderr output)
func FormatErrorMessage(message string) string {
	return applyStyle(styles.Error, "✗ ") + message
}

// FormatSectionHeader formats a section header with proper styling
// This is used for major sections in CLI output (e.g., "Overview", "Metrics")
func FormatSectionHeader(header string) string {
	if isTTY() {
		// TTY mode: Use styled header with underline
		return applyStyle(styles.Header, header)
	}
	// Non-TTY mode: Simple header text
	return header
}

// FormatErrorWithSuggestions formats an error message with actionable suggestions
func FormatErrorWithSuggestions(message string, suggestions []string) string {
	var output strings.Builder
	output.WriteString(FormatErrorMessage(message))

	if len(suggestions) > 0 {
		output.WriteString("\n\nSuggestions:\n")
		for _, suggestion := range suggestions {
			output.WriteString("  • " + suggestion + "\n")
		}
	}

	return output.String()
}

// RenderTitleBox renders a title with a double border box in TTY mode,
// or plain text with separator lines in non-TTY mode.
// The box will be centered and styled with the Info color scheme.
// Returns a slice of strings ready to be added to sections.
func RenderTitleBox(title string, width int) []string {
	if tty.IsStderrTerminal() {
		// TTY mode: Use Lipgloss styled box - returns as single string
		box := lipgloss.NewStyle().
			Bold(true).
			Foreground(styles.ColorInfo).
			Border(lipgloss.DoubleBorder(), true, false).
			Padding(0, 2).
			Width(width).
			Align(lipgloss.Center).
			Render(title)
		return []string{box}
	}

	// Non-TTY mode: Plain text with separators - returns as separate lines
	separator := strings.Repeat("━", width)
	return []string{separator, "  " + title, separator}
}

// RenderErrorBox renders an error/warning message with a rounded border box in TTY mode,
// or plain text in non-TTY mode.
// The box will be styled with the Error color scheme for critical messages.
// Returns a slice of strings ready to be added to sections or printed directly.
func RenderErrorBox(title string) []string {
	if tty.IsStderrTerminal() {
		// TTY mode: Use Lipgloss styled box with rounded border
		box := lipgloss.NewStyle().
			Border(styles.RoundedBorder).
			BorderForeground(styles.ColorError).
			Padding(1, 2).
			Bold(true).
			Render(title)
		return []string{box}
	}

	// Non-TTY mode: Plain text with error formatting
	return []string{
		FormatErrorMessage(title),
	}
}

// RenderInfoSection renders an info section with left border emphasis in TTY mode,
// or plain text with manual indentation in non-TTY mode.
// Returns a slice of strings ready to be added to sections.
func RenderInfoSection(content string) []string {
	if tty.IsStderrTerminal() {
		// TTY mode: Use Lipgloss styled section with left border
		section := lipgloss.NewStyle().
			Border(lipgloss.NormalBorder(), false, false, false, true).
			BorderForeground(styles.ColorInfo).
			PaddingLeft(2).
			Render(content)
		return []string{section}
	}

	// Non-TTY mode: Add manual indentation
	lines := strings.Split(content, "\n")
	result := make([]string, len(lines))
	for i, line := range lines {
		result[i] = "  " + line
	}
	return result
}

// RenderComposedSections composes and outputs a slice of sections to stderr.
// In TTY mode, uses lipgloss.JoinVertical for proper composition.
// In non-TTY mode, outputs each section as a separate line.
// Adds blank lines before and after the output.
func RenderComposedSections(sections []string) {
	if tty.IsStderrTerminal() {
		// TTY mode: Use Lipgloss to compose sections vertically
		plan := lipgloss.JoinVertical(lipgloss.Left, sections...)
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, plan)
		fmt.Fprintln(os.Stderr, "")
	} else {
		// Non-TTY mode: Output sections directly
		fmt.Fprintln(os.Stderr, "")
		for _, section := range sections {
			fmt.Fprintln(os.Stderr, section)
		}
		fmt.Fprintln(os.Stderr, "")
	}
}

// RenderTableAsJSON renders a table configuration as JSON
// This converts the table structure to a JSON array of objects
func RenderTableAsJSON(config TableConfig) (string, error) {
	if len(config.Headers) == 0 {
		return "[]", nil
	}

	// Create array of objects, where each object has header names as keys
	var result []map[string]string
	for _, row := range config.Rows {
		obj := make(map[string]string)
		for i, cell := range row {
			if i < len(config.Headers) {
				// Convert header to lowercase with underscores for JSON keys
				key := strings.ToLower(strings.ReplaceAll(config.Headers[i], " ", "_"))
				obj[key] = cell
			}
		}
		result = append(result, obj)
	}

	// Marshal to JSON with indentation
	jsonBytes, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("failed to marshal table to JSON: %w", err)
	}

	return string(jsonBytes), nil
}

// TreeNode represents a node in a hierarchical tree structure
type TreeNode struct {
	Value    string
	Children []TreeNode
}

// RenderTree renders a hierarchical tree structure using lipgloss/tree package
// Returns styled tree output for TTY, or simple indented text for non-TTY
func RenderTree(root TreeNode) string {
	if !isTTY() {
		// For non-TTY, render simple indented text
		return renderTreeSimple(root, "", true)
	}

	// For TTY, use lipgloss/tree for styled output
	lipglossTree := buildLipglossTree(root)
	return lipglossTree.String()
}

// renderTreeSimple renders a simple text-based tree without styling
func renderTreeSimple(node TreeNode, prefix string, isLast bool) string {
	var output strings.Builder

	// Current node
	connector := "├── "
	if isLast {
		connector = "└── "
	}
	if prefix == "" {
		// Root node has no connector
		output.WriteString(node.Value + "\n")
	} else {
		output.WriteString(prefix + connector + node.Value + "\n")
	}

	// Children
	for i, child := range node.Children {
		childIsLast := i == len(node.Children)-1
		var childPrefix string
		if prefix == "" {
			childPrefix = ""
		} else {
			if isLast {
				childPrefix = prefix + "    "
			} else {
				childPrefix = prefix + "│   "
			}
		}
		output.WriteString(renderTreeSimple(child, childPrefix, childIsLast))
	}

	return output.String()
}

// buildLipglossTree converts our TreeNode structure to lipgloss/tree format
func buildLipglossTree(node TreeNode) *tree.Tree {
	// Create root tree
	t := tree.Root(node.Value).
		EnumeratorStyle(styles.TreeEnumerator).
		ItemStyle(styles.TreeNode)

	// Add children recursively
	if len(node.Children) > 0 {
		children := make([]any, len(node.Children))
		for i, child := range node.Children {
			if len(child.Children) > 0 {
				// If child has children, create a subtree
				children[i] = buildLipglossTree(child)
			} else {
				// If child is a leaf, add as string
				children[i] = child.Value
			}
		}
		t.Child(children...)
	}

	return t
}
