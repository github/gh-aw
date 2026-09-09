//go:build !integration

package workflow

import (
	"bytes"
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWarnUnknownConfiguredModels(t *testing.T) {
	// This test redirects the process-wide stderr stream and cannot run in parallel.
	compiler := NewCompiler()
	compiler.SetConfiguredModelValidator(func(data *WorkflowData) []string {
		assert.Equal(t, "test", data.WorkflowID)
		return []string{"Model missing was not found in the active model inventory"}
	})

	oldStderr := os.Stderr
	reader, writer, err := os.Pipe()
	require.NoError(t, err)
	os.Stderr = writer
	defer func() {
		os.Stderr = oldStderr
	}()

	compiler.warnUnknownConfiguredModels(&WorkflowData{WorkflowID: "test"}, "test.md")
	require.NoError(t, writer.Close())
	output, err := io.ReadAll(reader)
	require.NoError(t, err)

	assert.Equal(t, 1, compiler.GetWarningCount())
	assert.Contains(t, string(bytes.TrimSpace(output)), "test.md: warning: Model missing")
}

func TestWarnUnknownConfiguredModelsWithoutInventory(t *testing.T) {
	t.Parallel()

	compiler := NewCompiler()
	compiler.warnUnknownConfiguredModels(&WorkflowData{}, "test.md")
	assert.Zero(t, compiler.GetWarningCount())
}

func TestWarnCodexCopilotModelCompatibility(t *testing.T) {
	tests := []struct {
		name        string
		data        *WorkflowData
		wantWarning bool
	}{
		{
			name: "general-purpose Copilot model",
			data: &WorkflowData{
				Model:        "copilot/mai-code-1-flash-picker",
				EngineConfig: &EngineConfig{ID: "codex"},
			},
			wantWarning: true,
		},
		{
			name: "Copilot auto model",
			data: &WorkflowData{
				Model:        "copilot/auto",
				EngineConfig: &EngineConfig{ID: "codex"},
			},
			wantWarning: true,
		},
		{
			name: "explicit GitHub provider",
			data: &WorkflowData{
				Model: "gpt-5.4",
				EngineConfig: &EngineConfig{
					ID:          "codex",
					LLMProvider: LLMProviderGitHub,
				},
			},
			wantWarning: true,
		},
		{
			name: "Codex Copilot model",
			data: &WorkflowData{
				Model:        "copilot/gpt-5.3-codex?effort=high",
				EngineConfig: &EngineConfig{ID: "codex"},
			},
		},
		{
			name: "runtime expression",
			data: &WorkflowData{
				Model:        "copilot/${{ inputs.model }}",
				EngineConfig: &EngineConfig{ID: "codex"},
			},
		},
		{
			name: "OpenAI model",
			data: &WorkflowData{
				Model:        "openai/gpt-5.4",
				EngineConfig: &EngineConfig{ID: "codex"},
			},
		},
		{
			name: "Copilot engine",
			data: &WorkflowData{
				Model:        "copilot/mai-code-1-flash-picker",
				EngineConfig: &EngineConfig{ID: "copilot"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()
			oldStderr := os.Stderr
			reader, writer, err := os.Pipe()
			require.NoError(t, err)
			os.Stderr = writer
			defer func() {
				os.Stderr = oldStderr
			}()

			compiler.warnCodexCopilotModelCompatibility(tt.data, "test.md")
			require.NoError(t, writer.Close())
			output, err := io.ReadAll(reader)
			require.NoError(t, err)

			if tt.wantWarning {
				assert.Equal(t, 1, compiler.GetWarningCount())
				assert.Contains(t, string(output), "Select a Codex model such as copilot/gpt-5.3-codex")
			} else {
				assert.Zero(t, compiler.GetWarningCount())
				assert.Empty(t, output)
			}
		})
	}
}
