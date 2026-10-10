//go:build !js && !wasm

package console

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"

	lipgloss "charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"
	"github.com/github/gh-aw/pkg/colorwriter"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/styles"
	"github.com/github/gh-aw/pkg/tty"
)

var consoleLog = logger.New("console:console")

// stdoutEnviron caches the process environment on first use so stdout styling
// helpers do not repeatedly copy and re-parse it while rendering output.
var stdoutEnviron = sync.OnceValue(os.Environ)
var stderrEnviron = sync.OnceValue(os.Environ)

// isTTY checks if stdout is a terminal
func isTTY() bool {
	return tty.IsStdoutTerminal()
}

// isStderrTTY checks if stderr is a terminal
func isStderrTTY() bool {
	return tty.IsStderrTerminal()
}

// applyStyle conditionally applies styling based on TTY status and color profile.
// When stdout is a TTY the rendered ANSI is downgraded through the colorprofile
// writer so that NO_COLOR, COLORTERM, and TERM are honored.
func applyStyle(style lipgloss.Style, text string) string {
	return applyStdoutStyleWithTTY(style, text, isTTY, stdoutEnviron())
}

func applyStyleWithTTYAndEnviron(style lipgloss.Style, text string, ttyCheck func() bool, environ []string) string {
	if !ttyCheck() {
		return text
	}
	return colorwriter.Degrade(style.Render(text), environ)
}

func applyStdoutStyleWithTTY(style lipgloss.Style, text string, ttyCheck func() bool, environ []string) string {
	return applyStyleWithTTYAndEnviron(style, text, ttyCheck, environ)
}

func applyStderrStyle(style lipgloss.Style, text string) string {
	return applyStderrStyleWithTTY(style, text, isStderrTTY, stderrEnviron())
}

func applyStderrStyleWithTTY(style lipgloss.Style, text string, ttyCheck func() bool, environ []string) string {
	return applyStyleWithTTYAndEnviron(style, text, ttyCheck, environ)
}

// applyStyleWithTTY conditionally renders raw ANSI based on a provided TTY check.
// Use this only for strings that will later be written through a color-profile-
// aware writer (for example stderrWriter); stdout-facing helpers should use
// applyStdoutStyleWithTTY so environment-based degradation happens here.
func applyStyleWithTTY(style lipgloss.Style, text string, ttyCheck func() bool) string {
	if ttyCheck() {
		return style.Render(text)
	}
	return text
}

// FormatErrorStdout formats a CompilerError with Rust-like rendering
func FormatErrorStdout(err CompilerError) string {
	return formatErrorWithStyle(err, applyStyle)
}

// FormatError formats a CompilerError for stderr with stderr TTY detection.
func FormatError(err CompilerError) string {
	return formatErrorStderrWithTTY(err, isStderrTTY, stderrEnviron())
}

func formatErrorStderrWithTTY(err CompilerError, ttyCheck func() bool, environ []string) string {
	return formatErrorWithStyle(err, func(style lipgloss.Style, text string) string {
		return applyStderrStyleWithTTY(style, text, ttyCheck, environ)
	})
}

func formatErrorWithStyle(err CompilerError, styleText func(lipgloss.Style, string) string) string {
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
	// Only include line:column when a meaningful position is known (line > 0)
	if err.Position.File != "" {
		relativePath := ToRelativePath(err.Position.File)
		var location string
		if err.Position.Line > 0 {
			location = fmt.Sprintf("%s:%d:%d:",
				relativePath,
				err.Position.Line,
				err.Position.Column)
		} else {
			location = relativePath + ":"
		}
		output.WriteString(styleText(styles.FilePath, location))
		output.WriteString(" ")
	}

	// Error type and message
	output.WriteString(styleText(typeStyle, prefix+":"))
	output.WriteString(" ")
	output.WriteString(err.Message)
	output.WriteString("\n")

	// Context lines (Rust-like error rendering)
	if len(err.Context) > 0 && err.Position.Line > 0 {
		output.WriteString(renderContext(err, styleText))
	}

	// Hint for fixing the error
	// Note: we intentionally use styles.Info (cyan) for hints since there is no
	// dedicated Hint style; Info is visually distinct and non-alarming, which is
	// appropriate for actionable guidance.
	if err.Hint != "" {
		output.WriteString(styleText(styles.Info, "hint: "))
		output.WriteString(err.Hint)
		output.WriteString("\n")
	}

	return output.String()
}

// renderContext renders source code context with line numbers and highlighting
func renderContext(err CompilerError, styleText func(lipgloss.Style, string) string) string {
	var output strings.Builder

	maxLineNum := err.Position.Line + len(err.Context)/2
	lineNumWidth := len(strconv.Itoa(maxLineNum))

	for i, line := range err.Context {
		lineNum := err.Position.Line - len(err.Context)/2 + i
		if lineNum < 1 {
			continue
		}

		lineNumStr := fmt.Sprintf("%*d", lineNumWidth, lineNum)
		output.WriteString(styleText(styles.LineNumber, lineNumStr))
		output.WriteString(" | ")

		if lineNum == err.Position.Line {
			if err.Position.Column > 0 && err.Position.Column <= len(line) {
				before := line[:err.Position.Column-1]
				wordEnd := findWordEnd(line, err.Position.Column-1)
				highlightedPart := line[err.Position.Column-1 : wordEnd]
				after := ""
				if wordEnd < len(line) {
					after = line[wordEnd:]
				}
				output.WriteString(styleText(styles.ContextLine, before))
				output.WriteString(styleText(styles.Highlight, highlightedPart))
				output.WriteString(styleText(styles.ContextLine, after))
			} else {
				output.WriteString(styleText(styles.Highlight, line))
			}
		} else {
			output.WriteString(styleText(styles.ContextLine, line))
		}
		output.WriteString("\n")

		if lineNum == err.Position.Line && err.Position.Column > 0 && err.Position.Column <= len(line) {
			wordEnd := findWordEnd(line, err.Position.Column-1)
			wordLength := wordEnd - (err.Position.Column - 1)
			padding := strings.Repeat(" ", lineNumWidth+3+err.Position.Column-1)
			pointer := styleText(styles.Error, strings.Repeat("^", wordLength))
			output.WriteString(padding)
			output.WriteString(pointer)
			output.WriteString("\n")
		}
	}

	return output.String()
}

// FormatSuccessMessageStdout formats a success message with styling
func FormatSuccessMessageStdout(message string) string {
	return formatSuccessMessageWithTTY(message, isTTY, stdoutEnviron())
}

// FormatSuccessMessage formats a success message for stderr output.
func FormatSuccessMessage(message string) string {
	return applyStderrStyle(styles.Success, "✓ ") + message
}

func formatSuccessMessageWithTTY(message string, ttyCheck func() bool, environ []string) string {
	return applyStdoutStyleWithTTY(styles.Success, "✓ ", ttyCheck, environ) + message
}

// FormatInfoMessageStdout formats an informational message
func FormatInfoMessageStdout(message string) string {
	return formatInfoMessageWithTTY(message, isTTY, stdoutEnviron())
}

// FormatInfoMessage formats an informational message for stderr output.
func FormatInfoMessage(message string) string {
	return applyStderrStyle(styles.Info, "i ") + message
}

func formatInfoMessageWithTTY(message string, ttyCheck func() bool, environ []string) string {
	return applyStdoutStyleWithTTY(styles.Info, "i ", ttyCheck, environ) + message
}

// FormatTableHeaderStderr formats table header text for stderr output.
func FormatTableHeaderStderr(text string) string {
	return applyStderrStyle(styles.TableHeader, text)
}

// FormatWarningMessageStdout formats a warning message
func FormatWarningMessageStdout(message string) string {
	return applyStyle(styles.Warning, "⚠ ") + message
}

// FormatWarningMessage formats a warning message for stderr output.
func FormatWarningMessage(message string) string {
	return applyStderrStyle(styles.Warning, "⚠ ") + message
}

// RenderTableStdout renders a formatted table using lipgloss/table package
func RenderTableStdout(config TableConfig) string {
	if len(config.Headers) == 0 {
		consoleLog.Print("No headers provided for table rendering")
		return ""
	}

	consoleLog.Printf("Rendering table: title=%s, columns=%d, rows=%d", config.Title, len(config.Headers), len(config.Rows))

	// Use caller-supplied TTY detector when provided (e.g. tty.IsStderrTerminal
	// for tables written to stderr), otherwise fall back to stdout detection.
	ttyCheck := isTTY
	stdoutOutput := true
	if config.TTYFunc != nil {
		ttyCheck = config.TTYFunc
		stdoutOutput = false
	}
	return renderTableWithTTY(config, ttyCheck, stdoutEnviron(), stdoutOutput)
}

// RenderTable renders a table using stderr terminal and color detection.
func RenderTable(config TableConfig) string {
	if len(config.Headers) == 0 {
		return ""
	}
	ttyCheck := isStderrTTY
	if config.TTYFunc != nil {
		ttyCheck = config.TTYFunc
	}
	return renderTableWithTTY(config, ttyCheck, stderrEnviron(), true)
}

// buildTableStyleFunc returns the lipgloss style function used by RenderTableStdout.
// config supplies the ShowTotal/TotalRow flags; ttyCheck detects terminal output;
// dataRowCount is the number of data rows (excluding any total row).
func buildTableStyleFunc(config TableConfig, ttyCheck func() bool, dataRowCount int) func(int, int) lipgloss.Style {
	return func(row, col int) lipgloss.Style {
		if !ttyCheck() {
			return lipgloss.NewStyle()
		}
		if row == table.HeaderRow {
			return styles.TableHeader.PaddingLeft(1).PaddingRight(1)
		}
		if config.ShowTotal && len(config.TotalRow) > 0 && row == dataRowCount {
			return styles.TableTotal.PaddingLeft(1).PaddingRight(1)
		}
		if row%2 == 0 {
			return styles.TableCell.PaddingLeft(1).PaddingRight(1)
		}
		return lipgloss.NewStyle().
			Foreground(styles.ColorForeground).
			Background(styles.ColorTableAltRow).
			PaddingLeft(1).
			PaddingRight(1)
	}
}

func renderTableWithTTY(config TableConfig, ttyCheck func() bool, environ []string, degradeStdout bool) string {
	var output strings.Builder

	titleStyle := applyStyleWithTTY
	if degradeStdout {
		titleStyle = func(style lipgloss.Style, text string, ttyCheck func() bool) string {
			return applyStdoutStyleWithTTY(style, text, ttyCheck, environ)
		}
	}

	if config.Title != "" {
		output.WriteString(titleStyle(styles.TableTitle, config.Title, ttyCheck))
		output.WriteString("\n")
	}

	allRows := config.Rows
	if config.ShowTotal && len(config.TotalRow) > 0 {
		allRows = append(allRows, config.TotalRow)
	}

	dataRowCount := len(config.Rows)
	styleFunc := buildTableStyleFunc(config, ttyCheck, dataRowCount)

	borderStyle := lipgloss.NewStyle()
	if ttyCheck() {
		borderStyle = styles.TableBorder
	}

	t := table.New().
		Headers(config.Headers...).
		Rows(allRows...).
		Border(styles.RoundedBorder).
		BorderStyle(borderStyle).
		StyleFunc(styleFunc)

	output.WriteString(renderBoundedTable(t, config, allRows, ttyCheck()))
	output.WriteString("\n")
	result := wrapConsoleText(output.String(), config.MaxWidth)

	if degradeStdout && ttyCheck() {
		return colorwriter.Degrade(result, environ)
	}

	return result
}

// FormatCommandMessageStdout formats a command execution message
func FormatCommandMessageStdout(command string) string {
	return applyStyle(styles.Command, "$ ") + command
}

// FormatCommandMessage formats a command execution message for stderr output.
func FormatCommandMessage(command string) string {
	return applyStderrStyle(styles.Command, "$ ") + command
}

// FormatProgressMessageStdout formats a progress/activity message
func FormatProgressMessageStdout(message string) string {
	return applyStyle(styles.Progress, "▸ ") + message
}

// FormatProgressMessage formats a progress message for stderr output.
func FormatProgressMessage(message string) string {
	return applyStderrStyle(styles.Progress, "▸ ") + message
}

// FormatPromptMessageStdout formats a user prompt message
func FormatPromptMessageStdout(message string) string {
	return applyStyle(styles.Prompt, "? ") + message
}

// FormatPromptMessage formats a prompt for stderr output.
func FormatPromptMessage(message string) string {
	return applyStderrStyle(styles.Prompt, "? ") + message
}

// FormatVerboseMessageStdout formats verbose debugging output
func FormatVerboseMessageStdout(message string) string {
	return applyStyle(styles.Verbose, "» ") + message
}

// FormatVerboseMessage formats verbose output for stderr.
func FormatVerboseMessage(message string) string {
	return applyStderrStyle(styles.Verbose, "» ") + message
}

// FormatListItemStdout formats an item in a list
func FormatListItemStdout(item string) string {
	return formatListItemWithTTY(item, isTTY, stdoutEnviron())
}

// FormatListItem formats a list item for stderr output.
func FormatListItem(item string) string {
	return applyStderrStyle(styles.ListItem, "  • "+item)
}

func formatListItemWithTTY(item string, ttyCheck func() bool, environ []string) string {
	return applyStdoutStyleWithTTY(styles.ListItem, "  • "+item, ttyCheck, environ)
}

// FormatErrorMessage formats a simple error message (for stderr output)
func FormatErrorMessage(message string) string {
	return applyStderrStyle(styles.Error, "✗ ") + message
}

// FormatErrorTextStderr formats plain error-styled text for stderr output.
func FormatErrorTextStderr(text string) string {
	return applyStderrStyle(styles.Error, text)
}

// FormatErrorChain formats an error and its full unwrapped chain in a reading-friendly way.
// For wrapped errors (fmt.Errorf with %w), each level of the chain is shown on a new
// indented line. For errors whose message contains newlines (e.g. errors.Join), each
// line is indented after the first.
func FormatErrorChain(err error) string {
	if err == nil {
		return ""
	}

	chain := unwrapErrorChain(err)
	if len(chain) <= 1 {
		return formatMultilineError(err.Error())
	}

	var sb strings.Builder
	sb.WriteString(applyStderrStyle(styles.Error, "✗ "))
	sb.WriteString(chain[0])
	for _, msg := range chain[1:] {
		// Each message in the chain may itself contain newlines (e.g. from errors.Join
		// nested inside a wrapping error); expand them all with consistent indentation.
		for line := range strings.SplitSeq(msg, "\n") {
			if line != "" {
				sb.WriteString("\n  ")
				sb.WriteString(line)
			}
		}
	}
	return sb.String()
}

// unwrapErrorChain walks the error chain via errors.Unwrap and returns a slice of
// individual message contributions, from outermost to innermost. Each entry contains
// only the message added at that level (i.e. the inner error's message is stripped).
func unwrapErrorChain(err error) []string {
	var chain []string
	current := err
	for current != nil {
		next := errors.Unwrap(current)
		if next == nil {
			chain = append(chain, current.Error())
			break
		}
		outerMsg := current.Error()
		innerMsg := next.Error()
		// Strip the inner error's message from the current error's message
		// to isolate this level's own contribution. This assumes the standard
		// fmt.Errorf("prefix: %w", inner) pattern (colon-space separator).
		// If the pattern does not match, the full message is used as a fallback
		// so no information is lost.
		suffix := ": " + innerMsg
		if strings.HasSuffix(outerMsg, suffix) {
			chain = append(chain, outerMsg[:len(outerMsg)-len(suffix)])
		} else {
			// Format does not follow the standard ": %w" pattern; keep the full message.
			chain = append(chain, outerMsg)
		}
		current = next
	}
	return chain
}

// formatMultilineError formats a plain error message, indenting any newlines so that
// continuation lines are visually subordinate to the leading "✗" prefix.
func formatMultilineError(msg string) string {
	if !strings.Contains(msg, "\n") {
		return FormatErrorMessage(msg)
	}
	lines := strings.Split(msg, "\n")
	var sb strings.Builder
	sb.WriteString(applyStderrStyle(styles.Error, "✗ "))
	sb.WriteString(lines[0])
	for _, line := range lines[1:] {
		if line != "" {
			sb.WriteString("\n  ")
			sb.WriteString(line)
		}
	}
	return sb.String()
}

// FormatSectionHeaderStdout formats a section header with proper styling
func FormatSectionHeaderStdout(header string) string {
	return formatSectionHeaderWithTTY(header, isTTY, stdoutEnviron())
}

// FormatSectionHeader formats a section header for stderr output.
func FormatSectionHeader(header string) string {
	return applyStderrStyle(styles.Header, header)
}

func formatSectionHeaderWithTTY(header string, ttyCheck func() bool, environ []string) string {
	return applyStdoutStyleWithTTY(styles.Header, header, ttyCheck, environ)
}

// RenderTitleBox renders a title with a double border box in TTY mode
func RenderTitleBox(title string, width int) []string {
	if tty.IsStderrTerminal() {
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

	separator := strings.Repeat("━", width)
	return []string{separator, "  " + title, separator}
}

// RenderErrorBox renders an error/warning message with a rounded border box
func RenderErrorBox(title string) []string {
	if tty.IsStderrTerminal() {
		box := lipgloss.NewStyle().
			Border(styles.RoundedBorder).
			BorderForeground(styles.ColorError).
			Padding(1, 2).
			Bold(true).
			Render(title)
		return []string{box}
	}

	return []string{
		FormatErrorMessage(title),
	}
}

// RenderInfoSection renders an info section with left border emphasis
func RenderInfoSection(content string) []string {
	if tty.IsStderrTerminal() {
		section := lipgloss.NewStyle().
			Border(lipgloss.NormalBorder(), false, false, false, true).
			BorderForeground(styles.ColorInfo).
			PaddingLeft(2).
			Render(content)
		return []string{section}
	}

	lines := strings.Split(content, "\n")
	result := make([]string, len(lines))
	for i, line := range lines {
		result[i] = "  " + line
	}
	return result
}

// RenderComposedSections composes and outputs a slice of sections to stderr
func RenderComposedSections(sections []string) {
	out := stderrWriter()
	if tty.IsStderrTerminal() {
		plan := lipgloss.JoinVertical(lipgloss.Left, sections...)
		fmt.Fprintln(out, "")
		fmt.Fprintln(out, plan)
		fmt.Fprintln(out, "")
	} else {
		fmt.Fprintln(out, "")
		for _, section := range sections {
			fmt.Fprintln(out, section)
		}
		fmt.Fprintln(out, "")
	}
}
