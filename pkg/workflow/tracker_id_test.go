//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

func TestExtractTrackerID(t *testing.T) {
	tests := []struct {
		name        string
		frontmatter map[string]any
		expected    string
		shouldError bool
		errorMsg    string
	}{
		{
			name:        "Valid tracker-id with alphanumeric and hyphens",
			frontmatter: map[string]any{"tracker-id": "test-fp-12345"},
			expected:    "test-fp-12345",
			shouldError: false,
		},
		{
			name:        "Valid tracker-id with underscores",
			frontmatter: map[string]any{"tracker-id": "test_fp_12345"},
			expected:    "test_fp_12345",
			shouldError: false,
		},
		{
			name:        "Valid tracker-id exactly 8 characters",
			frontmatter: map[string]any{"tracker-id": "12345678"},
			expected:    "12345678",
			shouldError: false,
		},
		{
			name:        "Valid tracker-id with mixed case",
			frontmatter: map[string]any{"tracker-id": "TestFP_123"},
			expected:    "TestFP_123",
			shouldError: false,
		},
		{
			name:        "Missing tracker-id returns empty string",
			frontmatter: map[string]any{},
			expected:    "",
			shouldError: false,
		},
		{
			name:        "Tracker-id with leading/trailing spaces trimmed",
			frontmatter: map[string]any{"tracker-id": "  test-fp-12345  "},
			expected:    "test-fp-12345",
			shouldError: false,
		},
		{
			name:        "Tracker-id too short (7 chars)",
			frontmatter: map[string]any{"tracker-id": "1234567"},
			expected:    "",
			shouldError: true,
			errorMsg:    "tracker-id must be at least 8 characters long",
		},
		{
			name:        "Tracker-id with invalid character (@)",
			frontmatter: map[string]any{"tracker-id": "test@fp123"},
			expected:    "",
			shouldError: true,
			errorMsg:    "tracker-id contains invalid character",
		},
		{
			name:        "Tracker-id with invalid character (space)",
			frontmatter: map[string]any{"tracker-id": "test fp 123"},
			expected:    "",
			shouldError: true,
			errorMsg:    "tracker-id contains invalid character",
		},
		{
			name:        "Tracker-id with invalid character (.)",
			frontmatter: map[string]any{"tracker-id": "test.fp.123"},
			expected:    "",
			shouldError: true,
			errorMsg:    "tracker-id contains invalid character",
		},
		{
			name:        "Tracker-id not a string",
			frontmatter: map[string]any{"tracker-id": 12345678},
			expected:    "",
			shouldError: true,
			errorMsg:    "tracker-id must be a string",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := &Compiler{}
			result, err := compiler.extractTrackerID(tt.frontmatter)

			if tt.shouldError {
				if err == nil {
					t.Errorf("Expected error containing '%s', got nil", tt.errorMsg)
				} else if tt.errorMsg != "" && !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("Expected error containing '%s', got '%s'", tt.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
				if result != tt.expected {
					t.Errorf("Expected '%s', got '%s'", tt.expected, result)
				}
			}
		})
	}
}
