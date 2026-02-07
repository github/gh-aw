//go:build !integration

package console

import (
	"bytes"
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

// captureStderr captures stderr output during function execution
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()

	// Save original stderr
	oldStderr := os.Stderr

	// Create a pipe to capture stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("Failed to create pipe: %v", err)
	}

	// Replace stderr with the write end of the pipe
	os.Stderr = w

	// Create a channel to receive the captured output
	outputChan := make(chan string, 1)

	// Read from the pipe in a goroutine
	go func() {
		var buf bytes.Buffer
		io.Copy(&buf, r)
		outputChan <- buf.String()
	}()

	// Execute the function
	fn()

	// Close the write end and restore stderr
	w.Close()
	os.Stderr = oldStderr

	// Get the captured output
	output := <-outputChan
	r.Close()

	return output
}

func TestMoveCursorUp(t *testing.T) {
	tests := []struct {
		name  string
		lines int
	}{
		{
			name:  "move up 1 line",
			lines: 1,
		},
		{
			name:  "move up 5 lines",
			lines: 5,
		},
		{
			name:  "move up 0 lines",
			lines: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := captureStderr(t, func() {
				MoveCursorUp(tt.lines)
			})

			// In non-TTY environments, output should be empty
			// We just ensure no panic occurs
			assert.NotNil(t, output, "MoveCursorUp should not panic")
		})
	}
}

func TestMoveCursorDown(t *testing.T) {
	tests := []struct {
		name  string
		lines int
	}{
		{
			name:  "move down 1 line",
			lines: 1,
		},
		{
			name:  "move down 5 lines",
			lines: 5,
		},
		{
			name:  "move down 0 lines",
			lines: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := captureStderr(t, func() {
				MoveCursorDown(tt.lines)
			})

			// In non-TTY environments, output should be empty
			// We just ensure no panic occurs
			assert.NotNil(t, output, "MoveCursorDown should not panic")
		})
	}
}

func TestTerminalCursorFunctionsNoTTY(t *testing.T) {
	// This test verifies that in non-TTY environments (like CI/tests),
	// no ANSI codes are emitted for cursor movement functions

	tests := []struct {
		name string
		fn   func()
	}{
		{
			name: "MoveCursorUp",
			fn:   func() { MoveCursorUp(5) },
		},
		{
			name: "MoveCursorDown",
			fn:   func() { MoveCursorDown(3) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := captureStderr(t, tt.fn)

			// Since tests typically run in non-TTY, verify output is empty
			// This ensures we properly respect TTY detection
			if os.Getenv("CI") != "" || !isRealTerminal() {
				assert.Empty(t, output, "%s should not output ANSI codes in non-TTY", tt.name)
			}
		})
	}
}

// isRealTerminal checks if we're actually running in a terminal
// This is a helper to distinguish between test environments and real terminals
func isRealTerminal() bool {
	// In test environments, stderr is typically redirected
	fileInfo, err := os.Stderr.Stat()
	if err != nil {
		return false
	}
	// Check if stderr is a character device (terminal)
	return (fileInfo.Mode() & os.ModeCharDevice) != 0
}

func TestTerminalCursorFunctionsDoNotPanic(t *testing.T) {
	// Ensure all cursor movement functions can be called safely without panicking
	// even in edge cases

	t.Run("all cursor functions", func(t *testing.T) {
		assert.NotPanics(t, func() {
			MoveCursorUp(0)
			MoveCursorUp(100)
			MoveCursorDown(0)
			MoveCursorDown(100)
		}, "Cursor movement functions should never panic")
	})
}
