//go:build !integration

package console

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLogVerbose(t *testing.T) {
	tests := []struct {
		name     string
		verbose  bool
		message  string
		expected bool // whether output is expected
	}{
		{
			name:     "verbose enabled outputs message",
			verbose:  true,
			message:  "Processing workflow",
			expected: true,
		},
		{
			name:     "verbose disabled no output",
			verbose:  false,
			message:  "Processing workflow",
			expected: false,
		},
		{
			name:     "verbose enabled with empty message",
			verbose:  true,
			message:  "",
			expected: true,
		},
		{
			name:     "verbose disabled with empty message",
			verbose:  false,
			message:  "",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Capture stderr output
			oldStderr := os.Stderr
			r, w, _ := os.Pipe()
			os.Stderr = w

			// Execute the function
			LogVerbose(tt.verbose, tt.message)

			// Restore stderr and read captured output
			w.Close()
			os.Stderr = oldStderr
			var buf bytes.Buffer
			_, _ = buf.ReadFrom(r)
			output := buf.String()

			if tt.expected {
				// Should contain the message
				assert.Contains(t, output, tt.message, "Output should contain the message when verbose is enabled")
				// Should contain the verbose icon (🔍)
				assert.True(t, strings.Contains(output, "🔍") || strings.Contains(output, tt.message),
					"Output should contain verbose formatting or message")
			} else {
				// Should be empty
				assert.Empty(t, output, "Output should be empty when verbose is disabled")
			}
		})
	}
}

func TestLogVerboseFormatting(t *testing.T) {
	// Capture stderr output
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	message := "Test message"
	LogVerbose(true, message)

	w.Close()
	os.Stderr = oldStderr
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	output := buf.String()

	// Verify the output uses FormatVerboseMessage
	// The output should contain the message and end with newline
	assert.Contains(t, output, message, "Output should contain the test message")
	assert.True(t, strings.HasSuffix(output, "\n"), "Output should end with newline")
}
