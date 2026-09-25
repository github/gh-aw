//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateUniversalLLMConsumerModel(t *testing.T) {
	compiler := NewCompiler()
	opencodeEngine, err := NewBehaviorDefinedEngine(&EngineDefinition{
		ID:          "opencode",
		DisplayName: "OpenCode",
		Behaviors: &EngineBehaviorDefinition{
			SecretStrategy: behaviorSecretStrategyUniversalLLMConsumer,
		},
	})
	require.NoError(t, err)

	t.Run("non universal engine skips validation", func(t *testing.T) {
		err := compiler.validateUniversalLLMConsumerModel(
			map[string]any{
				"engine": map[string]any{
					"id": "copilot",
				},
			},
			NewCopilotEngine(),
		)
		assert.NoError(t, err, "Non-universal engines should skip model validation")
	})

	t.Run("opencode requires model", func(t *testing.T) {
		err := compiler.validateUniversalLLMConsumerModel(
			map[string]any{
				"engine": map[string]any{
					"id": "opencode",
				},
			},
			opencodeEngine,
		)
		require.Error(t, err, "Missing model should fail for opencode")
		require.ErrorContains(t, err, "engine.model is required for engine 'opencode'")
	})

	t.Run("opencode requires provider/model format", func(t *testing.T) {
		err := compiler.validateUniversalLLMConsumerModel(
			map[string]any{
				"engine": map[string]any{
					"id":    "opencode",
					"model": "gpt-4.1",
				},
			},
			opencodeEngine,
		)
		require.Error(t, err, "Unqualified model should fail for opencode")
		require.ErrorContains(t, err, "provider/model format")
	})

	t.Run("unsupported provider fails", func(t *testing.T) {
		err := compiler.validateUniversalLLMConsumerModel(
			map[string]any{
				"engine": map[string]any{
					"id":    "opencode",
					"model": "groq/llama-4",
				},
			},
			opencodeEngine,
		)
		require.Error(t, err, "Unsupported provider should fail")
		require.ErrorContains(t, err, "unsupported provider")
	})

	t.Run("supported provider passes", func(t *testing.T) {
		err := compiler.validateUniversalLLMConsumerModel(
			map[string]any{
				"engine": map[string]any{
					"id":    "opencode",
					"model": "anthropic/claude-sonnet-4",
				},
			},
			opencodeEngine,
		)
		assert.NoError(t, err, "Supported provider/model should pass")
	})
}

func TestValidatePiEngineRequirements(t *testing.T) {
	compiler := NewCompiler()

	t.Run("non pi engine skips validation", func(t *testing.T) {
		err := compiler.validatePiEngineRequirements(NewTools(map[string]any{}), NewCopilotEngine())
		assert.NoError(t, err)
	})

	t.Run("pi requires github gh-proxy mode", func(t *testing.T) {
		err := compiler.validatePiEngineRequirements(NewTools(map[string]any{
			"github": true,
		}), NewPiEngine())
		require.Error(t, err)
		require.ErrorContains(t, err, "tools.github.mode: gh-proxy")
	})

	t.Run("pi requires cli-proxy", func(t *testing.T) {
		err := compiler.validatePiEngineRequirements(NewTools(map[string]any{
			"github": map[string]any{"mode": "gh-proxy"},
		}), NewPiEngine())
		require.Error(t, err)
		require.ErrorContains(t, err, "tools.cli-proxy: true")
	})

	t.Run("valid pi tool config passes", func(t *testing.T) {
		err := compiler.validatePiEngineRequirements(NewTools(map[string]any{
			"github":    map[string]any{"mode": "gh-proxy"},
			"cli-proxy": true,
		}), NewPiEngine())
		assert.NoError(t, err)
	})
}

func TestValidateContextWindowSupport(t *testing.T) {
	t.Run("missing context-window does not warn", func(t *testing.T) {
		compiler := NewCompiler()
		compiler.validateContextWindowSupport(&EngineConfig{}, NewCodexEngine())
		assert.Zero(t, compiler.GetWarningCount())
	})

	t.Run("nil engine config does not warn", func(t *testing.T) {
		compiler := NewCompiler()
		compiler.validateContextWindowSupport(nil, NewCodexEngine())
		assert.Zero(t, compiler.GetWarningCount())
	})

	engineConfig := &EngineConfig{ContextWindow: 1000000}

	t.Run("Pi supports context-window", func(t *testing.T) {
		compiler := NewCompiler()
		compiler.validateContextWindowSupport(engineConfig, NewPiEngine())
		assert.Zero(t, compiler.GetWarningCount())
	})

	for _, engine := range []CodingAgentEngine{
		NewClaudeEngine(),
		NewCodexEngine(),
		NewCopilotEngine(),
		NewGeminiEngine(),
	} {
		t.Run(engine.GetID()+" warns when context-window is unsupported", func(t *testing.T) {
			compiler := NewCompiler()
			compiler.validateContextWindowSupport(engineConfig, engine)
			assert.Equal(t, 1, compiler.GetWarningCount())
		})
	}
}

func TestCompileWorkflowWarnsForImportedUnsupportedContextWindow(t *testing.T) {
	compileWithSharedEngine := func(t *testing.T, sharedEngine string) int {
		t.Helper()
		tmpDir := testutil.TempDir(t, "imported-context-window")
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "shared.md"), []byte("---\nengine:\n"+sharedEngine+"---\n\nShared engine configuration.\n"), 0o644))
		workflowPath := filepath.Join(tmpDir, "workflow.md")
		require.NoError(t, os.WriteFile(workflowPath, []byte(`---
on: workflow_dispatch
imports:
  - shared.md
---

Run the workflow.
`), 0o644))

		compiler := NewCompiler(WithVersion("dev"))
		require.NoError(t, compiler.CompileWorkflow(workflowPath))
		return compiler.GetWarningCount()
	}

	baseline := compileWithSharedEngine(t, "  id: codex\n")
	withContextWindow := compileWithSharedEngine(t, "  id: codex\n  context-window: 1000000\n")
	assert.Equal(t, baseline+1, withContextWindow, "Imported context-window on an unsupported engine should warn")
}
