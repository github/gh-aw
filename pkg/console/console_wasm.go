//go:build js || wasm

package console

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var consoleWasmLog = logger.New("console:console_wasm")

func isTTY() bool {
	return false
}

func FormatError(err CompilerError) string {
	consoleWasmLog.Printf("FormatError: type=%s, file=%s, line=%d", err.Type, err.Position.File, err.Position.Line)

	var output strings.Builder

	var prefix string
	switch err.Type {
	case "warning":
		prefix = "warning"
	case "info":
		prefix = "info"
	default:
		prefix = "error"
	}

	if err.Position.File != "" {
		relativePath := ToRelativePath(err.Position.File)
		location := fmt.Sprintf("%s:%d:%d:", relativePath, err.Position.Line, err.Position.Column)
		output.WriteString(location)
		output.WriteString(" ")
	}

	output.WriteString(prefix + ": ")
	output.WriteString(err.Message)
	output.WriteString("\n")

	if len(err.Context) > 0 && err.Position.Line > 0 {
		consoleWasmLog.Printf("FormatError: rendering %d lines of source context", len(err.Context))
		maxLineNum := err.Position.Line + len(err.Context)/2
		lineNumWidth := len(strconv.Itoa(maxLineNum))
		for i, line := range err.Context {
			lineNum := err.Position.Line - len(err.Context)/2 + i
			if lineNum < 1 {
				continue
			}
			lineNumStr := fmt.Sprintf("%*d", lineNumWidth, lineNum)
			output.WriteString(lineNumStr)
			output.WriteString(" | ")
			output.WriteString(line)
			output.WriteString("\n")
			if lineNum == err.Position.Line && err.Position.Column > 0 && err.Position.Column <= len(line) {
				wordEnd := findWordEnd(line, err.Position.Column-1)
				wordLength := wordEnd - (err.Position.Column - 1)
				padding := strings.Repeat(" ", lineNumWidth+3+err.Position.Column-1)
				pointer := strings.Repeat("^", wordLength)
				output.WriteString(padding)
				output.WriteString(pointer)
				output.WriteString("\n")
			}
		}
	}

	return output.String()
}

func FormatSuccessMessage(message string) string  { return "✓ " + message }
func FormatInfoMessage(message string) string     { return "i " + message }
func FormatTableHeaderStderr(text string) string  { return text }
func FormatWarningMessage(message string) string  { return "⚠ " + message }
func FormatErrorMessage(message string) string    { return "✗ " + message }
func FormatErrorTextStderr(text string) string    { return text }
func FormatLocationMessage(message string) string { return "~ " + message }
func FormatCommandMessage(command string) string  { return "$ " + command }
func FormatProgressMessage(message string) string { return "▸ " + message }
func FormatPromptMessage(message string) string   { return "? " + message }
func FormatCountMessage(message string) string    { return "# " + message }
func FormatVerboseMessage(message string) string  { return "» " + message }
func FormatListHeader(header string) string       { return header }
func FormatListItem(item string) string           { return "  • " + item }
func FormatSectionHeader(header string) string    { return header }

// FormatErrorChain formats an error and its full unwrapped chain.
// In the WASM build there is no rich terminal styling; the top-level error
// message is sufficient for the JSON-based diagnostics that WASM consumers
// produce. Full chain formatting is provided by the non-WASM implementation.
func FormatErrorChain(err error) string {
	if err == nil {
		return ""
	}
	return "✗ " + err.Error()
}

func RenderTable(config TableConfig) string {
	if len(config.Headers) == 0 {
		return ""
	}
	var output strings.Builder
	if config.Title != "" {
		output.WriteString(config.Title)
		output.WriteString("\n")
	}
	output.WriteString(strings.Join(config.Headers, "\t"))
	output.WriteString("\n")
	allRows := config.Rows
	if config.ShowTotal && len(config.TotalRow) > 0 {
		allRows = append(allRows, config.TotalRow)
	}
	for _, row := range allRows {
		output.WriteString(strings.Join(row, "\t"))
		output.WriteString("\n")
	}
	return output.String()
}

// RenderTableStderr preserves the WASM tab-separated table representation.

func RenderTitleBox(title string, width int) []string {
	separator := strings.Repeat("=", width)
	return []string{separator, "  " + title, separator}
}

func RenderErrorBox(title string) []string {
	return []string{FormatErrorMessage(title)}
}

func RenderInfoSection(content string) []string {
	lines := strings.Split(content, "\n")
	result := make([]string, len(lines))
	for i, line := range lines {
		result[i] = "  " + line
	}
	return result
}

func RenderComposedSections(sections []string) {
	fmt.Fprintln(os.Stderr, "")
	for _, section := range sections {
		fmt.Fprintln(os.Stderr, section)
	}
	fmt.Fprintln(os.Stderr, "")
}

func RenderTree(root TreeNode) string {
	consoleWasmLog.Printf("RenderTree: rendering tree rooted at %q with %d children", root.Value, len(root.Children))
	var render func(node TreeNode, prefix string, isLast bool) string
	render = func(node TreeNode, prefix string, isLast bool) string {
		var output strings.Builder
		if prefix == "" {
			output.WriteString(node.Value + "\n")
		} else {
			connector := "├── "
			if isLast {
				connector = "└── "
			}
			output.WriteString(prefix + connector + node.Value + "\n")
		}
		for i, child := range node.Children {
			childIsLast := i == len(node.Children)-1
			childPrefix := prefix + "│   "
			if isLast {
				childPrefix = prefix + "    "
			}
			output.WriteString(render(child, childPrefix, childIsLast))
		}
		return output.String()
	}
	return render(root, "", true)
}
