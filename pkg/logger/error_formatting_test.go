//go:build !integration

package logger

import (
	"strings"
	"testing"
)

func TestExtractErrorMessage(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "ISO 8601 timestamp with T separator and Z",
			input:    "2024-01-01T12:00:00.123Z Error: connection failed",
			expected: "connection failed",
		},
		{
			name:     "ISO 8601 timestamp with T separator and timezone offset",
			input:    "2024-01-01T12:00:00.123+00:00 Error: connection failed",
			expected: "connection failed",
		},
		{
			name:     "Date-time with space separator",
			input:    "2024-01-01 12:00:00 Error: connection failed",
			expected: "connection failed",
		},
		{
			name:     "Date-time with space separator and milliseconds",
			input:    "2024-01-01 12:00:00.456 Error: connection failed",
			expected: "connection failed",
		},
		{
			name:     "Bracketed date-time",
			input:    "[2024-01-01 12:00:00] Error: connection failed",
			expected: "connection failed",
		},
		{
			name:     "Bracketed time only",
			input:    "[12:00:00] Error: connection failed",
			expected: "connection failed",
		},
		{
			name:     "Time only with milliseconds",
			input:    "12:00:00.123 Error: connection failed",
			expected: "connection failed",
		},
		{
			name:     "Time only without milliseconds",
			input:    "12:00:00 Error: connection failed",
			expected: "connection failed",
		},
		{
			name:     "ERROR prefix with colon",
			input:    "ERROR: connection failed",
			expected: "connection failed",
		},
		{
			name:     "ERROR prefix without colon",
			input:    "ERROR connection failed",
			expected: "connection failed",
		},
		{
			name:     "Bracketed ERROR prefix",
			input:    "[ERROR] connection failed",
			expected: "connection failed",
		},
		{
			name:     "Bracketed ERROR prefix with colon",
			input:    "[ERROR]: connection failed",
			expected: "connection failed",
		},
		{
			name:     "WARNING prefix",
			input:    "WARNING: disk space low",
			expected: "disk space low",
		},
		{
			name:     "WARN prefix",
			input:    "WARN: deprecated API used",
			expected: "deprecated API used",
		},
		{
			name:     "INFO prefix",
			input:    "INFO: service started",
			expected: "service started",
		},
		{
			name:     "DEBUG prefix",
			input:    "DEBUG: processing request",
			expected: "processing request",
		},
		{
			name:     "Case insensitive log level",
			input:    "error: connection failed",
			expected: "connection failed",
		},
		{
			name:     "Combined timestamp and log level",
			input:    "2024-01-01 12:00:00 ERROR: connection failed",
			expected: "connection failed",
		},
		{
			name:     "Combined ISO timestamp with Z and log level",
			input:    "2024-01-01T12:00:00Z ERROR: connection failed",
			expected: "connection failed",
		},
		{
			name:     "Multiple timestamps - only first is removed",
			input:    "[12:00:00] 2024-01-01 12:00:00 ERROR: connection failed",
			expected: "2024-01-01 12:00:00 ERROR: connection failed",
		},
		{
			name:     "No timestamp or log level",
			input:    "connection failed",
			expected: "connection failed",
		},
		{
			name:     "Empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "Only whitespace",
			input:    "   ",
			expected: "",
		},
		{
			name:     "Truncation at 200 chars",
			input:    "ERROR: " + strings.Repeat("a", 250),
			expected: strings.Repeat("a", 197) + "...",
		},
		{
			name:     "Exactly 200 chars - no truncation",
			input:    "ERROR: " + strings.Repeat("a", 193),
			expected: strings.Repeat("a", 193),
		},
		{
			name:     "Real world example from metrics.go",
			input:    "2024-01-15 14:30:22 ERROR: Failed to connect to database",
			expected: "Failed to connect to database",
		},
		{
			name:     "Real world example from copilot_agent.go",
			input:    "2024-01-15T14:30:22.123Z ERROR: API request failed",
			expected: "API request failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExtractErrorMessage(tt.input)
			if result != tt.expected {
				t.Errorf("ExtractErrorMessage(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func BenchmarkExtractErrorMessage(b *testing.B) {
	testLine := "2024-01-01T12:00:00.123Z ERROR: connection failed to remote server"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ExtractErrorMessage(testLine)
	}
}

func BenchmarkExtractErrorMessageLong(b *testing.B) {
	testLine := "2024-01-01T12:00:00.123Z ERROR: " + strings.Repeat("very long error message ", 20)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ExtractErrorMessage(testLine)
	}
}
