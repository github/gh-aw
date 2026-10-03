//go:build !integration

package workflow

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateBashCommandAllowlistSupport(t *testing.T) {
	tests := []struct {
		name        string
		engineID    string
		tools       map[string]any
		shouldError bool
		errorMsg    string
	}{
		// Codex engine - restricted allowlist should error
		{
			name:        "codex with restricted bash allowlist should error",
			engineID:    "codex",
			tools:       map[string]any{"bash": []any{"git", "npm"}},
			shouldError: true,
			errorMsg:    "does not support bash command allow-listing",
		},
		{
			name:        "codex with single command allowlist should error",
			engineID:    "codex",
			tools:       map[string]any{"bash": []any{"git"}},
			shouldError: true,
			errorMsg:    "does not support bash command allow-listing",
		},
		// Codex engine - full refusal (bash: false, bash: []) is supported via BashDisable
		// (features.shell_tool=false), even though per-command allowlisting is not.
		{
			name:        "codex with bash: false should succeed",
			engineID:    "codex",
			tools:       map[string]any{"bash": false},
			shouldError: false,
		},
		{
			name:        "codex with empty bash list should succeed",
			engineID:    "codex",
			tools:       map[string]any{"bash": []any{}},
			shouldError: false,
		},
		// Codex engine - wildcard or absent should succeed
		{
			name:        "codex with wildcard bash should succeed",
			engineID:    "codex",
			tools:       map[string]any{"bash": []any{"*"}},
			shouldError: false,
		},
		{
			name:        "codex with namespace wildcard bash should succeed",
			engineID:    "codex",
			tools:       map[string]any{"bash": []any{":*"}},
			shouldError: false,
		},
		{
			name:        "codex with bash: true should succeed",
			engineID:    "codex",
			tools:       map[string]any{"bash": true},
			shouldError: false,
		},
		{
			name:        "codex with no bash should succeed",
			engineID:    "codex",
			tools:       map[string]any{"github": nil},
			shouldError: false,
		},
		{
			name:        "codex with nil tools should succeed",
			engineID:    "codex",
			tools:       nil,
			shouldError: false,
		},
		// Engines that support bash allowlists - restricted should succeed
		{
			name:        "claude with restricted bash allowlist should succeed",
			engineID:    "claude",
			tools:       map[string]any{"bash": []any{"git", "npm"}},
			shouldError: false,
		},
		{
			name:        "copilot with restricted bash allowlist should succeed",
			engineID:    "copilot",
			tools:       map[string]any{"bash": []any{"git"}},
			shouldError: false,
		},
		{
			name:        "gemini with restricted bash allowlist should succeed",
			engineID:    "gemini",
			tools:       map[string]any{"bash": []any{"make", "go"}},
			shouldError: false,
		},
		// Engines that support bash allowlists - deny configs should succeed (engine enforces them)
		{
			name:        "claude with bash: false should succeed",
			engineID:    "claude",
			tools:       map[string]any{"bash": false},
			shouldError: false,
		},
		{
			name:        "copilot with empty bash list should succeed",
			engineID:    "copilot",
			tools:       map[string]any{"bash": []any{}},
			shouldError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := GetGlobalEngineRegistry()
			engine, err := registry.GetEngine(tt.engineID)
			require.NoError(t, err, "failed to get engine %q", tt.engineID)

			compiler := NewCompiler()
			err = compiler.validateBashCommandAllowlistSupport(tt.tools, engine)

			if tt.shouldError {
				require.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestEngineBashCommandAllowlistCapability(t *testing.T) {
	tests := []struct {
		engineID  string
		supported bool
	}{
		{"claude", true},
		{"copilot", true},
		{"gemini", true},
		{"codex", false},
	}

	for _, tt := range tests {
		t.Run(tt.engineID, func(t *testing.T) {
			registry := GetGlobalEngineRegistry()
			engine, err := registry.GetEngine(tt.engineID)
			require.NoError(t, err)

			got := engine.GetCapabilities().BashCommandAllowlist
			assert.Equal(t, tt.supported, got,
				"engine %q BashCommandAllowlist capability mismatch", tt.engineID)
		})
	}
}

func TestEngineBashDisableCapability(t *testing.T) {
	tests := []struct {
		engineID  string
		supported bool
	}{
		{"claude", true},
		{"copilot", false},
		{"gemini", false},
		{"codex", true},
	}

	for _, tt := range tests {
		t.Run(tt.engineID, func(t *testing.T) {
			registry := GetGlobalEngineRegistry()
			engine, err := registry.GetEngine(tt.engineID)
			require.NoError(t, err)

			got := engine.GetCapabilities().BashDisable
			assert.Equal(t, tt.supported, got,
				"engine %q BashDisable capability mismatch", tt.engineID)
		})
	}
}
