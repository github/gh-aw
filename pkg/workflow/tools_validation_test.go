//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateBashToolConfig(t *testing.T) {
	tests := []struct {
		name        string
		toolsMap    map[string]any
		shouldError bool
		errorMsg    string
	}{
		{
			name:        "nil tools config is valid",
			toolsMap:    nil,
			shouldError: false,
		},
		{
			name:        "no bash tool is valid",
			toolsMap:    map[string]any{"github": nil},
			shouldError: false,
		},
		{
			name:        "bash: true is valid",
			toolsMap:    map[string]any{"bash": true},
			shouldError: false,
		},
		{
			name:        "bash: false is valid",
			toolsMap:    map[string]any{"bash": false},
			shouldError: false,
		},
		{
			name:        "bash with array is valid",
			toolsMap:    map[string]any{"bash": []any{"echo", "ls"}},
			shouldError: false,
		},
		{
			name:        "bash with wildcard is valid",
			toolsMap:    map[string]any{"bash": []any{"*"}},
			shouldError: false,
		},
		{
			name:        "anonymous bash (nil) is invalid",
			toolsMap:    map[string]any{"bash": nil},
			shouldError: true,
			errorMsg:    "anonymous syntax 'bash:' is not supported",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tools := NewTools(tt.toolsMap)
			err := validateBashToolConfig(tools, "test-workflow")

			if tt.shouldError {
				require.Error(t, err, "Expected error for %s", tt.name)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg, "Error message should contain expected text")
				}
			} else {
				assert.NoError(t, err, "Expected no error for %s", tt.name)
			}
		})
	}
}

func TestParseBashToolWithBoolean(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		expected *BashToolConfig
	}{
		{
			name:     "bash: true enables all commands",
			input:    true,
			expected: &BashToolConfig{AllowedCommands: nil},
		},
		{
			name:     "bash: false explicitly disables",
			input:    false,
			expected: &BashToolConfig{AllowedCommands: []string{}},
		},
		{
			name:     "bash: nil is invalid",
			input:    nil,
			expected: nil,
		},
		{
			name:  "bash with array",
			input: []any{"echo", "ls"},
			expected: &BashToolConfig{
				AllowedCommands: []string{"echo", "ls"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseBashTool(tt.input)

			if tt.expected == nil {
				assert.Nil(t, result, "Expected nil result")
			} else {
				require.NotNil(t, result, "Expected non-nil result")
				if tt.expected.AllowedCommands == nil {
					assert.Nil(t, result.AllowedCommands, "Expected nil AllowedCommands (all allowed)")
				} else {
					assert.Equal(t, tt.expected.AllowedCommands, result.AllowedCommands, "AllowedCommands should match")
				}
			}
		})
	}
}

func TestNewToolsWithInvalidBash(t *testing.T) {
	t.Run("detects invalid bash configuration", func(t *testing.T) {
		toolsMap := map[string]any{
			"bash": nil, // Anonymous syntax
		}

		tools := NewTools(toolsMap)

		// The parser should set Bash to nil for invalid config
		assert.Nil(t, tools.Bash, "Bash should be nil for invalid config")

		// Validation should catch this
		err := validateBashToolConfig(tools, "test-workflow")
		require.Error(t, err, "Expected validation error")
		assert.Contains(t, err.Error(), "anonymous syntax", "Error should mention anonymous syntax")
	})

	t.Run("accepts valid bash configurations", func(t *testing.T) {
		validConfigs := []map[string]any{
			{"bash": true},
			{"bash": false},
			{"bash": []any{"echo"}},
			{"bash": []any{"*"}},
		}

		for _, toolsMap := range validConfigs {
			tools := NewTools(toolsMap)
			assert.NotNil(t, tools.Bash, "Bash should not be nil for valid config")

			err := validateBashToolConfig(tools, "test-workflow")
			assert.NoError(t, err, "Expected no validation error for valid config")
		}
	})
}

func TestIsGitToolAllowed(t *testing.T) {
	tests := []struct {
		name        string
		toolsMap    map[string]any
		expectGitOK bool
		description string
	}{
		{
			name:        "nil tools - git allowed (defaults will apply)",
			toolsMap:    nil,
			expectGitOK: true,
			description: "No tools configuration means defaults will apply which include git for PR operations",
		},
		{
			name:        "no bash tool - git allowed (defaults will apply)",
			toolsMap:    map[string]any{},
			expectGitOK: true,
			description: "No bash tool configured means defaults will apply which include git for PR operations",
		},
		{
			name: "bash: true - git allowed",
			toolsMap: map[string]any{
				"bash": true,
			},
			expectGitOK: true,
			description: "bash: true allows all commands including git",
		},
		{
			name: "bash: false - git not allowed",
			toolsMap: map[string]any{
				"bash": false,
			},
			expectGitOK: false,
			description: "bash: false explicitly disables bash",
		},
		{
			name: "bash with empty list - git not allowed",
			toolsMap: map[string]any{
				"bash": []any{},
			},
			expectGitOK: false,
			description: "Empty command list means no commands allowed",
		},
		{
			name: "bash with git command - git allowed",
			toolsMap: map[string]any{
				"bash": []any{"git"},
			},
			expectGitOK: true,
			description: "git explicitly in allowed commands",
		},
		{
			name: "bash with git and other commands - git allowed",
			toolsMap: map[string]any{
				"bash": []any{"echo", "git", "ls"},
			},
			expectGitOK: true,
			description: "git in allowed commands list",
		},
		{
			name: "bash with wildcard - git allowed",
			toolsMap: map[string]any{
				"bash": []any{"*"},
			},
			expectGitOK: true,
			description: "Wildcard allows all commands including git",
		},
		{
			name: "bash with wildcard and other commands - git allowed",
			toolsMap: map[string]any{
				"bash": []any{"echo", "*"},
			},
			expectGitOK: true,
			description: "Wildcard in list allows all commands",
		},
		{
			name: "bash with git and space wildcard - git allowed",
			toolsMap: map[string]any{
				"bash": []any{"git *"},
			},
			expectGitOK: true,
			description: "git * pattern allows all git commands",
		},
		{
			name: "bash with git colon wildcard - git allowed",
			toolsMap: map[string]any{
				"bash": []any{"git:*"},
			},
			expectGitOK: true,
			description: "git:* pattern allows all git commands",
		},
		{
			name: "bash with git subcommand wildcard - git allowed",
			toolsMap: map[string]any{
				"bash": []any{"git checkout:*", "git status"},
			},
			expectGitOK: true,
			description: "git <subcommand>:* pattern allows git commands",
		},
		{
			name: "bash with other commands only - git not allowed",
			toolsMap: map[string]any{
				"bash": []any{"echo", "ls", "cat"},
			},
			expectGitOK: false,
			description: "git not in allowed commands list",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tools := NewTools(tt.toolsMap)
			result := isGitToolAllowed(tools)
			assert.Equal(t, tt.expectGitOK, result, tt.description)
		})
	}
}

func TestValidateGitToolForSafeOutputs(t *testing.T) {
	tests := []struct {
		name          string
		toolsMap      map[string]any
		safeOutputs   *SafeOutputsConfig
		expectError   bool
		errorContains string
	}{
		{
			name:        "nil safe-outputs - no validation needed",
			toolsMap:    nil,
			safeOutputs: nil,
			expectError: false,
		},
		{
			name:     "safe-outputs without create-pull-request or push-to-pull-request-branch - no validation",
			toolsMap: nil,
			safeOutputs: &SafeOutputsConfig{
				CreateIssues: &CreateIssuesConfig{},
			},
			expectError: false,
		},
		{
			name:     "create-pull-request without bash - OK (defaults will apply)",
			toolsMap: map[string]any{},
			safeOutputs: &SafeOutputsConfig{
				CreatePullRequests: &CreatePullRequestsConfig{},
			},
			expectError: false,
		},
		{
			name: "create-pull-request with bash: false - error",
			toolsMap: map[string]any{
				"bash": false,
			},
			safeOutputs: &SafeOutputsConfig{
				CreatePullRequests: &CreatePullRequestsConfig{},
			},
			expectError:   true,
			errorContains: "create-pull-request but git tool is not allowed",
		},
		{
			name: "create-pull-request with bash: [] - error",
			toolsMap: map[string]any{
				"bash": []any{},
			},
			safeOutputs: &SafeOutputsConfig{
				CreatePullRequests: &CreatePullRequestsConfig{},
			},
			expectError:   true,
			errorContains: "create-pull-request but git tool is not allowed",
		},
		{
			name: "create-pull-request with bash: [echo] - error",
			toolsMap: map[string]any{
				"bash": []any{"echo", "ls"},
			},
			safeOutputs: &SafeOutputsConfig{
				CreatePullRequests: &CreatePullRequestsConfig{},
			},
			expectError:   true,
			errorContains: "create-pull-request but git tool is not allowed",
		},
		{
			name: "create-pull-request with bash: true - valid",
			toolsMap: map[string]any{
				"bash": true,
			},
			safeOutputs: &SafeOutputsConfig{
				CreatePullRequests: &CreatePullRequestsConfig{},
			},
			expectError: false,
		},
		{
			name: "create-pull-request with bash: [git] - valid",
			toolsMap: map[string]any{
				"bash": []any{"git"},
			},
			safeOutputs: &SafeOutputsConfig{
				CreatePullRequests: &CreatePullRequestsConfig{},
			},
			expectError: false,
		},
		{
			name: "create-pull-request with bash: [*] - valid",
			toolsMap: map[string]any{
				"bash": []any{"*"},
			},
			safeOutputs: &SafeOutputsConfig{
				CreatePullRequests: &CreatePullRequestsConfig{},
			},
			expectError: false,
		},
		{
			name: "create-pull-request with bash: [git, echo] - valid",
			toolsMap: map[string]any{
				"bash": []any{"git", "echo", "ls"},
			},
			safeOutputs: &SafeOutputsConfig{
				CreatePullRequests: &CreatePullRequestsConfig{},
			},
			expectError: false,
		},
		{
			name:     "push-to-pull-request-branch without bash - OK (defaults will apply)",
			toolsMap: map[string]any{},
			safeOutputs: &SafeOutputsConfig{
				PushToPullRequestBranch: &PushToPullRequestBranchConfig{},
			},
			expectError: false,
		},
		{
			name: "push-to-pull-request-branch with bash: true - valid",
			toolsMap: map[string]any{
				"bash": true,
			},
			safeOutputs: &SafeOutputsConfig{
				PushToPullRequestBranch: &PushToPullRequestBranchConfig{},
			},
			expectError: false,
		},
		{
			name: "push-to-pull-request-branch with bash: [git] - valid",
			toolsMap: map[string]any{
				"bash": []any{"git"},
			},
			safeOutputs: &SafeOutputsConfig{
				PushToPullRequestBranch: &PushToPullRequestBranchConfig{},
			},
			expectError: false,
		},
		{
			name:     "both create-pull-request and push-to-pull-request-branch without bash - OK (defaults will apply)",
			toolsMap: map[string]any{},
			safeOutputs: &SafeOutputsConfig{
				CreatePullRequests:      &CreatePullRequestsConfig{},
				PushToPullRequestBranch: &PushToPullRequestBranchConfig{},
			},
			expectError: false,
		},
		{
			name: "both features with bash: false - error mentions both",
			toolsMap: map[string]any{
				"bash": false,
			},
			safeOutputs: &SafeOutputsConfig{
				CreatePullRequests:      &CreatePullRequestsConfig{},
				PushToPullRequestBranch: &PushToPullRequestBranchConfig{},
			},
			expectError:   true,
			errorContains: "create-pull-request and push-to-pull-request-branch",
		},
		{
			name: "both features with bash: [echo] - error mentions both",
			toolsMap: map[string]any{
				"bash": []any{"echo", "ls"},
			},
			safeOutputs: &SafeOutputsConfig{
				CreatePullRequests:      &CreatePullRequestsConfig{},
				PushToPullRequestBranch: &PushToPullRequestBranchConfig{},
			},
			expectError:   true,
			errorContains: "create-pull-request and push-to-pull-request-branch",
		},
		{
			name: "both create-pull-request and push-to-pull-request-branch with bash: true - valid",
			toolsMap: map[string]any{
				"bash": true,
			},
			safeOutputs: &SafeOutputsConfig{
				CreatePullRequests:      &CreatePullRequestsConfig{},
				PushToPullRequestBranch: &PushToPullRequestBranchConfig{},
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tools := NewTools(tt.toolsMap)
			err := validateGitToolForSafeOutputs(tools, tt.safeOutputs, "test-workflow")

			if tt.expectError {
				require.Error(t, err, "Expected validation error")
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains, "Error should contain expected message")
				}
				// Verify error message includes helpful suggestions
				assert.True(t,
					strings.Contains(err.Error(), "bash: true") ||
						strings.Contains(err.Error(), "bash: [\"git\"]") ||
						strings.Contains(err.Error(), "bash: [\"*\"]"),
					"Error message should include helpful suggestions")
			} else {
				assert.NoError(t, err, "Expected no validation error")
			}
		})
	}
}
