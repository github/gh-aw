//go:build !integration

package workflow

import (
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
