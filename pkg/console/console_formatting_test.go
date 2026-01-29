//go:build !integration

package console

import (
	"strings"
	"testing"
)

func TestFormatCommandMessage(t *testing.T) {
	tests := []struct {
		name     string
		command  string
		expected string
	}{
		{
			name:     "simple command",
			command:  "git status",
			expected: "git status",
		},
		{
			name:     "complex command with flags",
			command:  "gh aw compile --verbose",
			expected: "gh aw compile --verbose",
		},
		{
			name:     "empty command",
			command:  "",
			expected: "",
		},
		{
			name:     "command with special characters",
			command:  "make test && echo 'done'",
			expected: "make test && echo 'done'",
		},
		{
			name:     "multiline command",
			command:  "echo 'line1'\necho 'line2'",
			expected: "echo 'line1'\necho 'line2'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatCommandMessage(tt.command)

			// Should contain the thunderbolt emoji prefix
			if !strings.Contains(result, "⚡") {
				t.Errorf("FormatCommandMessage() should contain ⚡ prefix")
			}

			// Should contain the command text
			if !strings.Contains(result, tt.expected) {
				t.Errorf("FormatCommandMessage() = %v, should contain %v", result, tt.expected)
			}

			// Result should not be empty unless input was empty
			if tt.command != "" && result == "" {
				t.Errorf("FormatCommandMessage() returned empty string for non-empty input")
			}
		})
	}
}

func TestFormatProgressMessage(t *testing.T) {
	tests := []struct {
		name     string
		message  string
		expected string
	}{
		{
			name:     "simple progress message",
			message:  "Compiling workflow files...",
			expected: "Compiling workflow files...",
		},
		{
			name:     "build progress message",
			message:  "Building application",
			expected: "Building application",
		},
		{
			name:     "empty message",
			message:  "",
			expected: "",
		},
		{
			name:     "message with numbers",
			message:  "Processing 5 of 10 files",
			expected: "Processing 5 of 10 files",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatProgressMessage(tt.message)

			// Should contain the hammer emoji prefix
			if !strings.Contains(result, "🔨") {
				t.Errorf("FormatProgressMessage() should contain 🔨 prefix")
			}

			// Should contain the message text
			if !strings.Contains(result, tt.expected) {
				t.Errorf("FormatProgressMessage() = %v, should contain %v", result, tt.expected)
			}
		})
	}
}

func TestFormatPromptMessage(t *testing.T) {
	tests := []struct {
		name     string
		message  string
		expected string
	}{
		{
			name:     "confirmation prompt",
			message:  "Are you sure you want to continue? [y/N]: ",
			expected: "Are you sure you want to continue? [y/N]: ",
		},
		{
			name:     "input prompt",
			message:  "Enter workflow name: ",
			expected: "Enter workflow name: ",
		},
		{
			name:     "empty prompt",
			message:  "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatPromptMessage(tt.message)

			// Should contain the question mark emoji prefix
			if !strings.Contains(result, "❓") {
				t.Errorf("FormatPromptMessage() should contain ❓ prefix")
			}

			// Should contain the message text
			if !strings.Contains(result, tt.expected) {
				t.Errorf("FormatPromptMessage() = %v, should contain %v", result, tt.expected)
			}
		})
	}
}

func TestFormatCountMessage(t *testing.T) {
	tests := []struct {
		name     string
		message  string
		expected string
	}{
		{
			name:     "file count",
			message:  "Found 15 workflows to compile",
			expected: "Found 15 workflows to compile",
		},
		{
			name:     "zero count",
			message:  "Found 0 issues",
			expected: "Found 0 issues",
		},
		{
			name:     "percentage",
			message:  "Coverage: 85.5%",
			expected: "Coverage: 85.5%",
		},
		{
			name:     "empty count message",
			message:  "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatCountMessage(tt.message)

			// Should contain the chart emoji prefix
			if !strings.Contains(result, "📊") {
				t.Errorf("FormatCountMessage() should contain 📊 prefix")
			}

			// Should contain the message text
			if !strings.Contains(result, tt.expected) {
				t.Errorf("FormatCountMessage() = %v, should contain %v", result, tt.expected)
			}
		})
	}
}

func TestFormatVerboseMessage(t *testing.T) {
	tests := []struct {
		name     string
		message  string
		expected string
	}{
		{
			name:     "debug message",
			message:  "Debug: Parsing frontmatter section",
			expected: "Debug: Parsing frontmatter section",
		},
		{
			name:     "detailed trace",
			message:  "Trace: Function called with args: [arg1, arg2]",
			expected: "Trace: Function called with args: [arg1, arg2]",
		},
		{
			name:     "empty verbose message",
			message:  "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatVerboseMessage(tt.message)

			// Should contain the magnifying glass emoji prefix
			if !strings.Contains(result, "🔍") {
				t.Errorf("FormatVerboseMessage() should contain 🔍 prefix")
			}

			// Should contain the message text
			if !strings.Contains(result, tt.expected) {
				t.Errorf("FormatVerboseMessage() = %v, should contain %v", result, tt.expected)
			}
		})
	}
}

func TestFormatListHeader(t *testing.T) {
	tests := []struct {
		name     string
		header   string
		expected string
	}{
		{
			name:     "simple header",
			header:   "Available Workflows",
			expected: "Available Workflows",
		},
		{
			name:     "header with underscores",
			header:   "==================",
			expected: "==================",
		},
		{
			name:     "empty header",
			header:   "",
			expected: "",
		},
		{
			name:     "header with numbers",
			header:   "Section 1: Configuration",
			expected: "Section 1: Configuration",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatListHeader(tt.header)

			// Should contain the header text
			if !strings.Contains(result, tt.expected) {
				t.Errorf("FormatListHeader() = %v, should contain %v", result, tt.expected)
			}

			// Result should not be empty unless input was empty
			if tt.header != "" && result == "" {
				t.Errorf("FormatListHeader() returned empty string for non-empty input")
			}
		})
	}
}

func TestFormatListItem(t *testing.T) {
	tests := []struct {
		name     string
		item     string
		expected string
	}{
		{
			name:     "simple item",
			item:     "weekly-research.md",
			expected: "weekly-research.md",
		},
		{
			name:     "item with path",
			item:     "src/workflow/daily-plan.md",
			expected: "src/workflow/daily-plan.md",
		},
		{
			name:     "empty item",
			item:     "",
			expected: "",
		},
		{
			name:     "item with spaces",
			item:     "Complex Workflow Name",
			expected: "Complex Workflow Name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatListItem(tt.item)

			// Should contain the bullet point prefix
			if !strings.Contains(result, "•") {
				t.Errorf("FormatListItem() should contain • prefix")
			}

			// Should contain the item text
			if !strings.Contains(result, tt.expected) {
				t.Errorf("FormatListItem() = %v, should contain %v", result, tt.expected)
			}

			// Should contain proper indentation
			if !strings.Contains(result, "  •") {
				t.Errorf("FormatListItem() should contain proper indentation '  •'")
			}
		})
	}
}

func TestFormatErrorMessage(t *testing.T) {
	tests := []struct {
		name     string
		message  string
		expected string
	}{
		{
			name:     "simple error",
			message:  "File not found",
			expected: "File not found",
		},
		{
			name:     "detailed error",
			message:  "failed to compile workflow: invalid syntax at line 15",
			expected: "failed to compile workflow: invalid syntax at line 15",
		},
		{
			name:     "empty error message",
			message:  "",
			expected: "",
		},
		{
			name:     "error with code",
			message:  "exit code 1: command failed",
			expected: "exit code 1: command failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatErrorMessage(tt.message)

			// Should contain the X emoji prefix
			if !strings.Contains(result, "✗") {
				t.Errorf("FormatErrorMessage() should contain ✗ prefix")
			}

			// Should contain the error message text
			if !strings.Contains(result, tt.expected) {
				t.Errorf("FormatErrorMessage() = %v, should contain %v", result, tt.expected)
			}
		})
	}
}

func TestFormatSectionHeader(t *testing.T) {
	tests := []struct {
		name     string
		header   string
		expected string
	}{
		{
			name:     "simple header",
			header:   "Overview",
			expected: "Overview",
		},
		{
			name:     "header with spaces",
			header:   "Key Findings",
			expected: "Key Findings",
		},
		{
			name:     "empty header",
			header:   "",
			expected: "",
		},
		{
			name:     "header with numbers",
			header:   "Section 1: Configuration",
			expected: "Section 1: Configuration",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatSectionHeader(tt.header)

			// Should contain the header text
			if !strings.Contains(result, tt.expected) {
				t.Errorf("FormatSectionHeader() = %v, should contain %v", result, tt.expected)
			}

			// Result should not be empty unless input was empty
			if tt.header != "" && result == "" {
				t.Errorf("FormatSectionHeader() returned empty string for non-empty input")
			}
		})
	}
}

// Edge case tests for all formatting functions
func TestFormattingFunctionsWithSpecialCharacters(t *testing.T) {
	specialChars := "!@#$%^&*()[]{}|\\:;\"'<>,.?/`~"

	// Test that all functions handle special characters without panicking
	t.Run("special characters don't cause panic", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("Formatting function panicked with special characters: %v", r)
			}
		}()

		FormatCommandMessage(specialChars)
		FormatProgressMessage(specialChars)
		FormatPromptMessage(specialChars)
		FormatCountMessage(specialChars)
		FormatVerboseMessage(specialChars)
		FormatListHeader(specialChars)
		FormatListItem(specialChars)
		FormatErrorMessage(specialChars)
	})
}

func TestFormattingFunctionsWithUnicodeCharacters(t *testing.T) {
	unicodeText := "Test with unicode: 你好 🌍 café naïve résumé"

	// Test that all functions handle unicode characters properly
	t.Run("unicode characters handled properly", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("Formatting function panicked with unicode characters: %v", r)
			}
		}()

		result1 := FormatCommandMessage(unicodeText)
		if !strings.Contains(result1, unicodeText) {
			t.Errorf("FormatCommandMessage did not preserve unicode text")
		}

		result2 := FormatProgressMessage(unicodeText)
		if !strings.Contains(result2, unicodeText) {
			t.Errorf("FormatProgressMessage did not preserve unicode text")
		}

		result3 := FormatErrorMessage(unicodeText)
		if !strings.Contains(result3, unicodeText) {
			t.Errorf("FormatErrorMessage did not preserve unicode text")
		}
	})
}
