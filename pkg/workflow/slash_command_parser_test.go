//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

func TestParseSlashCommandShorthand(t *testing.T) {
	tests := []struct {
		name            string
		input           string
		expectedCommand string
		expectedIsSlash bool
		expectedError   bool
		errorSubstring  string
	}{
		{
			name:            "valid simple command",
			input:           "/help",
			expectedCommand: "help",
			expectedIsSlash: true,
			expectedError:   false,
		},
		{
			name:            "valid command with hyphens",
			input:           "/my-bot",
			expectedCommand: "my-bot",
			expectedIsSlash: true,
			expectedError:   false,
		},
		{
			name:            "valid command with underscores",
			input:           "/code_review",
			expectedCommand: "code_review",
			expectedIsSlash: true,
			expectedError:   false,
		},
		{
			name:            "valid command with numbers",
			input:           "/bot123",
			expectedCommand: "bot123",
			expectedIsSlash: true,
			expectedError:   false,
		},
		{
			name:            "valid long command",
			input:           "/very-long-command-name-with-many-parts",
			expectedCommand: "very-long-command-name-with-many-parts",
			expectedIsSlash: true,
			expectedError:   false,
		},
		{
			name:            "empty command after slash",
			input:           "/",
			expectedCommand: "",
			expectedIsSlash: true,
			expectedError:   true,
			errorSubstring:  "slash command shorthand cannot be empty after '/'",
		},
		{
			name:            "not a slash command - regular event",
			input:           "push",
			expectedCommand: "",
			expectedIsSlash: false,
			expectedError:   false,
		},
		{
			name:            "not a slash command - schedule",
			input:           "daily",
			expectedCommand: "",
			expectedIsSlash: false,
			expectedError:   false,
		},
		{
			name:            "not a slash command - cron",
			input:           "0 9 * * 1",
			expectedCommand: "",
			expectedIsSlash: false,
			expectedError:   false,
		},
		{
			name:            "not a slash command - empty string",
			input:           "",
			expectedCommand: "",
			expectedIsSlash: false,
			expectedError:   false,
		},
		{
			name:            "command with mixed case",
			input:           "/MyBot",
			expectedCommand: "MyBot",
			expectedIsSlash: true,
			expectedError:   false,
		},
		{
			name:            "command with dots",
			input:           "/my.bot",
			expectedCommand: "my.bot",
			expectedIsSlash: true,
			expectedError:   false,
		},
		{
			name:            "command starting with number",
			input:           "/123bot",
			expectedCommand: "123bot",
			expectedIsSlash: true,
			expectedError:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			commandName, isSlashCommand, err := parseSlashCommandShorthand(tt.input)

			// Check error
			if tt.expectedError {
				if err == nil {
					t.Errorf("expected error containing '%s', got nil", tt.errorSubstring)
					return
				}
				if !strings.Contains(err.Error(), tt.errorSubstring) {
					t.Errorf("expected error containing '%s', got '%s'", tt.errorSubstring, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
					return
				}
			}

			// Check isSlashCommand flag
			if isSlashCommand != tt.expectedIsSlash {
				t.Errorf("expected isSlashCommand=%v, got %v", tt.expectedIsSlash, isSlashCommand)
			}

			// Check command name (only if not an error)
			if !tt.expectedError && commandName != tt.expectedCommand {
				t.Errorf("expected command name '%s', got '%s'", tt.expectedCommand, commandName)
			}
		})
	}
}

func TestExpandSlashCommandShorthand(t *testing.T) {
	tests := []struct {
		name        string
		commandName string
	}{
		{
			name:        "simple command",
			commandName: "help",
		},
		{
			name:        "command with hyphens",
			commandName: "my-bot",
		},
		{
			name:        "command with underscores",
			commandName: "code_review",
		},
		{
			name:        "command with numbers",
			commandName: "bot123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := expandSlashCommandShorthand(tt.commandName)

			// Check that result is a map
			if result == nil {
				t.Error("expected non-nil result")
				return
			}

			// Check slash_command field
			slashCommandValue, hasSlashCommand := result["slash_command"]
			if !hasSlashCommand {
				t.Error("expected 'slash_command' field in result")
				return
			}

			slashCommandStr, ok := slashCommandValue.(string)
			if !ok {
				t.Errorf("expected slash_command to be string, got %T", slashCommandValue)
				return
			}

			if slashCommandStr != tt.commandName {
				t.Errorf("expected slash_command '%s', got '%s'", tt.commandName, slashCommandStr)
			}

			// Check workflow_dispatch field
			if _, hasWorkflowDispatch := result["workflow_dispatch"]; !hasWorkflowDispatch {
				t.Error("expected 'workflow_dispatch' field in result")
				return
			}

			// Check that there are exactly 2 fields
			if len(result) != 2 {
				t.Errorf("expected exactly 2 fields in result, got %d: %v", len(result), result)
			}
		})
	}
}

func TestParseSlashCommandShorthandEdgeCases(t *testing.T) {
	tests := []struct {
		name            string
		input           string
		expectedCommand string
		expectedIsSlash bool
		expectedError   bool
	}{
		{
			name:            "single character command",
			input:           "/a",
			expectedCommand: "a",
			expectedIsSlash: true,
			expectedError:   false,
		},
		{
			name:            "command with spaces (preserved)",
			input:           "/my bot",
			expectedCommand: "my bot",
			expectedIsSlash: true,
			expectedError:   false,
		},
		{
			name:            "command with special characters",
			input:           "/bot@v1",
			expectedCommand: "bot@v1",
			expectedIsSlash: true,
			expectedError:   false,
		},
		{
			name:            "command with unicode",
			input:           "/bot🤖",
			expectedCommand: "bot🤖",
			expectedIsSlash: true,
			expectedError:   false,
		},
		{
			name:            "multiple slashes",
			input:           "//command",
			expectedCommand: "/command",
			expectedIsSlash: true,
			expectedError:   false,
		},
		{
			name:            "slash in middle - not a slash command",
			input:           "issues/opened",
			expectedCommand: "",
			expectedIsSlash: false,
			expectedError:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			commandName, isSlashCommand, err := parseSlashCommandShorthand(tt.input)

			if tt.expectedError {
				if err == nil {
					t.Error("expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}

			if isSlashCommand != tt.expectedIsSlash {
				t.Errorf("expected isSlashCommand=%v, got %v", tt.expectedIsSlash, isSlashCommand)
			}

			if !tt.expectedError && commandName != tt.expectedCommand {
				t.Errorf("expected command name '%s', got '%s'", tt.expectedCommand, commandName)
			}
		})
	}
}

func TestParseSlashCommandShorthandWithWhitespace(t *testing.T) {
	tests := []struct {
		name            string
		input           string
		expectedCommand string
		expectedIsSlash bool
	}{
		{
			name:            "leading whitespace preserved",
			input:           "/ bot",
			expectedCommand: " bot",
			expectedIsSlash: true,
		},
		{
			name:            "trailing whitespace preserved",
			input:           "/bot ",
			expectedCommand: "bot ",
			expectedIsSlash: true,
		},
		{
			name:            "tab character",
			input:           "/bot\t",
			expectedCommand: "bot\t",
			expectedIsSlash: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			commandName, isSlashCommand, err := parseSlashCommandShorthand(tt.input)

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if isSlashCommand != tt.expectedIsSlash {
				t.Errorf("expected isSlashCommand=%v, got %v", tt.expectedIsSlash, isSlashCommand)
			}

			if commandName != tt.expectedCommand {
				t.Errorf("expected command name '%s', got '%s'", tt.expectedCommand, commandName)
			}
		})
	}
}
