package console

import (
	"fmt"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/styles"
	"github.com/github/gh-aw/pkg/tty"
)

var terminalLog = logger.New("console:terminal")

// ANSI escape sequences for terminal control
//
// These two sequences are a deliberate exception to the Lipgloss-based styling used
// elsewhere in this package: Lipgloss has no equivalent for cursor/screen control
// operations (clear-screen, clear-to-end-of-line), so raw ANSI codes are required here.
const (
	// ansiClearScreen clears the screen and moves cursor to home position
	ansiClearScreen = "\033[H\033[2J"

	// ansiClearLine clears from cursor to end of line
	ansiClearLine = "\033[K"

	// ansiCarriageReturn moves cursor to start of current line
	ansiCarriageReturn = "\r"
)

// ClearScreen clears the terminal screen if stderr is a TTY
// Uses ANSI escape codes for cross-platform compatibility
func ClearScreen() {
	if tty.IsStderrTerminal() {
		if _, err := fmt.Fprint(stderrWriter(), ansiClearScreen); err != nil {
			terminalLog.Printf("Failed to clear screen: %v", err)
		}
	}
}

// ClearLine clears the current line in the terminal if stderr is a TTY
// Uses ANSI escape codes: \r moves cursor to start, \033[K clears to end of line
func ClearLine() {
	if tty.IsStderrTerminal() {
		if _, err := fmt.Fprintf(stderrWriter(), "%s%s", ansiCarriageReturn, ansiClearLine); err != nil {
			terminalLog.Printf("Failed to clear line: %v", err)
		}
	}
}

// ShowWelcomeBanner clears the screen and displays the welcome banner for interactive commands.
// Use this at the start of interactive commands (add, trial, init) for a consistent experience.
func ShowWelcomeBanner(description string) {
	ClearScreen()
	header := "→ Welcome to GitHub Agentic Workflows!"
	if tty.IsStderrTerminal() {
		header = styles.Header.Render(header)
	}
	out := stderrWriter()
	if _, err := fmt.Fprintln(out, ""); err != nil {
		terminalLog.Printf("Failed to write welcome banner spacing: %v", err)
	}
	if _, err := fmt.Fprintln(out, header); err != nil {
		terminalLog.Printf("Failed to write welcome banner header: %v", err)
	}
	if _, err := fmt.Fprintln(out, ""); err != nil {
		terminalLog.Printf("Failed to write welcome banner spacing: %v", err)
	}
	if _, err := fmt.Fprintln(out, description); err != nil {
		terminalLog.Printf("Failed to write welcome banner description: %v", err)
	}
	if _, err := fmt.Fprintln(out, ""); err != nil {
		terminalLog.Printf("Failed to write welcome banner spacing: %v", err)
	}
}
