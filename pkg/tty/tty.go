//go:build !js && !wasm

// Package tty provides utilities for TTY (terminal) detection.
// This package uses golang.org/x/term for TTY detection, which aligns with
// modern Go best practices and the spinner library v1.23.1+ implementation.
package tty

import (
	"os"

	"golang.org/x/term"
)

// IsStdinTerminal returns true if stdin is connected to a terminal.
func IsStdinTerminal() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}

// IsStdoutTerminal returns true if stdout is connected to a terminal.
func IsStdoutTerminal() bool {
	return term.IsTerminal(int(os.Stdout.Fd()))
}

// IsStderrTerminal returns true if stderr is connected to a terminal.
func IsStderrTerminal() bool {
	return term.IsTerminal(int(os.Stderr.Fd()))
}
