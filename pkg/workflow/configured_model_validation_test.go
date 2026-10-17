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

func TestWarnCopilotIntegrationModelAvailability(t *testing.T) {
	for _, tt := range []struct {
		name        string
		model       string
		engine      string
		env         map[string]string
		wantWarning bool
	}{
		{name: "Gemini", model: "gemini-3.8-flash", engine: "copilot", wantWarning: true},
		{name: "provider qualified with effort", model: "copilot/gemini-3.8-flash?effort=high", engine: "copilot", wantWarning: true},
		{name: "github-copilot prefix", model: "github-copilot/gemini-3.7-flash", engine: "copilot", wantWarning: true},
		{name: "engine-name meta-alias", model: "gemini", engine: "copilot"},
		{name: "engine-name meta-alias with effort", model: "gemini?effort=high", engine: "copilot"},
		{name: "runtime model", model: "${{ inputs.model }}", engine: "copilot"},
		{name: "other engine", model: "gemini-3.8-flash", engine: "pi"},
		{name: "Google provider", model: "google/gemini-3.8-flash", engine: "copilot"},
		{name: "explicit BYOK", model: "gemini-3.8-flash", engine: "copilot", env: map[string]string{"COPILOT_PROVIDER_BASE_URL": "https://example.com/v1"}},
		{name: "other model", model: "claude-sonnet-5", engine: "copilot"},
		{name: "no model", engine: "copilot"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()
			oldStderr := os.Stderr
			reader, writer, err := os.Pipe()
			require.NoError(t, err)
			os.Stderr = writer
			defer func() { os.Stderr = oldStderr }()
			compiler.warnCopilotIntegrationModelAvailability(&WorkflowData{
				Model:        tt.model,
				EngineConfig: &EngineConfig{ID: tt.engine, Env: tt.env},
			}, "test.md")
			require.NoError(t, writer.Close())
			output, err := io.ReadAll(reader)
			require.NoError(t, err)
			if tt.wantWarning {
				assert.Equal(t, 1, compiler.GetWarningCount())
				assert.Contains(t, string(output), "agentic-workflows integration")
				assert.Contains(t, string(output), "does not guarantee integration access")
			} else {
				assert.Zero(t, compiler.GetWarningCount())
				assert.Empty(t, output)
			}
		})
	}
}

func TestWarnCodexCopilotModelCompatibility(t *testing.T) {
	type testCase struct {
		name        string
		data        *WorkflowData
		wantWarning bool
	}
	tests := []testCase{
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
		},
		{
			name: "bare auto with GitHub provider",
			data: &WorkflowData{
				Model:        "auto",
				EngineConfig: &EngineConfig{ID: "codex", LLMProvider: LLMProviderGitHub},
			},
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
			name: "unknown GPT Copilot model",
			data: &WorkflowData{
				Model:        "copilot/gpt-6-unknown",
				EngineConfig: &EngineConfig{ID: "codex"},
			},
			wantWarning: true,
		},
		{
			name: "explicit GitHub provider with a modern Codex model",
			data: &WorkflowData{
				Model: "gpt-6-sol",
				EngineConfig: &EngineConfig{
					ID:          "codex",
					LLMProvider: LLMProviderGitHub,
				},
			},
		},
		{
			name: "modern Codex model with normalized prefix and effort",
			data: &WorkflowData{
				Model:        " Copilot/GPT-6.1-SOL?effort=high ",
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

	for _, model := range []string{
		"gpt-6.1-sol", "gpt-6-sol", "gpt-6-luna", "gpt-6-astra",
		"gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.6-luna",
	} {
		tests = append(tests, testCase{
			name: "modern Codex Copilot model " + model,
			data: &WorkflowData{
				Model:        "copilot/" + model,
				EngineConfig: &EngineConfig{ID: "codex"},
			},
		})
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
				assert.Contains(t, string(output), "Select a supported Codex model such as copilot/gpt-6.1-sol")
			} else {
				assert.Zero(t, compiler.GetWarningCount())
				assert.Empty(t, output)
			}
		})
	}
}

func TestWarnsWhenEngineCannotResolveAlias(t *testing.T) {
	builtins := BuiltinModelAliases()
	for _, tt := range []struct {
		engine      string
		model       string
		wantWarning bool
	}{
		{engine: "gemini", model: "small", wantWarning: true},
		{engine: "gemini", model: "google/small?effort=high", wantWarning: true},
		{engine: "aider", model: "opus", wantWarning: true},
		{engine: "gemini", model: "gemini-3.8-flash-exact"},
		{engine: "gemini", model: "${{ inputs.model }}"},
		{engine: "copilot", model: "opus"},
		{engine: "claude", model: "copilot/opus"},
		{engine: "codex", model: "copilot/gpt-5.6"},
		{engine: "pi", model: "copilot/opus"},
		{engine: "agy", model: "small"},
	} {
		t.Run(tt.engine+"/"+tt.model, func(t *testing.T) {
			compiler := NewCompiler()
			oldStderr := os.Stderr
			reader, writer, err := os.Pipe()
			require.NoError(t, err)
			os.Stderr = writer
			defer func() { os.Stderr = oldStderr }()
			compiler.warnEngineCannotResolveModelAlias(&WorkflowData{
				Model:         tt.model,
				EngineConfig:  &EngineConfig{ID: tt.engine},
				ModelMappings: builtins,
			}, "test.md")
			require.NoError(t, writer.Close())
			output, err := io.ReadAll(reader)
			require.NoError(t, err)
			if tt.wantWarning {
				assert.Equal(t, 1, compiler.GetWarningCount())
				assert.Contains(t, string(output), `engine "`+tt.engine+`" does not resolve gh-aw model aliases`)
				assert.Contains(t, string(output), `uses alias "`)
			} else {
				assert.Zero(t, compiler.GetWarningCount())
				assert.Empty(t, output)
			}
		})
	}
}
