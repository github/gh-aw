//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/require"
)

func TestValidateMaxToolCallsSupport(t *testing.T) {
	t.Parallel()

	compiler := NewCompiler()
	registry := GetGlobalEngineRegistry()

	copilotEngine, err := registry.GetEngine("copilot")
	require.NoError(t, err)

	claudeEngine, err := registry.GetEngine("claude")
	require.NoError(t, err)

	tests := []struct {
		name        string
		frontmatter map[string]any
		engine      CodingAgentEngine
		expectError string
	}{
		{
			name: "no max-tool-calls",
			frontmatter: map[string]any{
				"engine": "claude",
			},
			engine: claudeEngine,
		},
		{
			name: "copilot sdk with max-tool-calls",
			frontmatter: map[string]any{
				"engine": map[string]any{
					"id":          "copilot",
					"copilot-sdk": true,
				},
				"max-tool-calls": 120,
			},
			engine: copilotEngine,
		},
		{
			name: "copilot sdk with max-tool-calls expression",
			frontmatter: map[string]any{
				"engine": map[string]any{
					"id":          "copilot",
					"copilot-sdk": true,
				},
				"max-tool-calls": "${{ inputs.max-tool-calls }}",
			},
			engine: copilotEngine,
		},
		{
			name: "copilot without sdk rejects max-tool-calls",
			frontmatter: map[string]any{
				"engine": map[string]any{
					"id": "copilot",
				},
				"max-tool-calls": 120,
			},
			engine:      copilotEngine,
			expectError: "requires Copilot SDK mode",
		},
		{
			name: "non-copilot rejects max-tool-calls",
			frontmatter: map[string]any{
				"engine":         "claude",
				"max-tool-calls": 120,
			},
			engine:      claudeEngine,
			expectError: "does not support max-tool-calls",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := compiler.validateMaxToolCallsSupport(tt.frontmatter, tt.engine)
			if tt.expectError == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tt.expectError)
		})
	}
}

func TestExtractEngineConfigMaxToolCalls(t *testing.T) {
	t.Parallel()

	compiler := NewCompiler()

	t.Run("top-level value without engine", func(t *testing.T) {
		t.Parallel()
		_, config, _ := compiler.ExtractEngineConfig(map[string]any{"max-tool-calls": 42})
		require.NotNil(t, config)
		require.Equal(t, "42", config.MaxToolCalls)
	})

	t.Run("top-level value with string engine", func(t *testing.T) {
		t.Parallel()
		_, config, _ := compiler.ExtractEngineConfig(map[string]any{
			"engine":         "copilot",
			"max-tool-calls": 42,
		})
		require.NotNil(t, config)
		require.Equal(t, "42", config.MaxToolCalls)
	})

	t.Run("top-level value with engine object", func(t *testing.T) {
		t.Parallel()
		_, config, _ := compiler.ExtractEngineConfig(map[string]any{
			"engine": map[string]any{
				"id":          "copilot",
				"copilot-sdk": true,
			},
			"max-tool-calls": "${{ inputs.budget }}",
		})
		require.NotNil(t, config)
		require.Equal(t, "${{ inputs.budget }}", config.MaxToolCalls)
	})

	t.Run("invalid values are ignored", func(t *testing.T) {
		t.Parallel()
		for _, raw := range []any{0, -3, "not-a-number"} {
			_, config, _ := compiler.ExtractEngineConfig(map[string]any{
				"engine":         "copilot",
				"max-tool-calls": raw,
			})
			if config != nil {
				require.Empty(t, config.MaxToolCalls)
			}
		}
	})
}

func TestCopilotSDKMaxToolCallsEnv(t *testing.T) {
	t.Parallel()

	engine := NewCopilotEngine()

	t.Run("emits budget when configured", func(t *testing.T) {
		t.Parallel()
		env := map[string]string{}
		engine.addCopilotWorkflowStepEnv(env, &WorkflowData{
			EngineConfig: &EngineConfig{ID: "copilot", CopilotSDK: true, MaxToolCalls: "150"},
		}, false)
		require.Equal(t, "150", env[constants.EnvVarMaxToolCalls])
	})

	t.Run("omits budget when unset", func(t *testing.T) {
		t.Parallel()
		env := map[string]string{}
		engine.addCopilotWorkflowStepEnv(env, &WorkflowData{
			EngineConfig: &EngineConfig{ID: "copilot", CopilotSDK: true},
		}, false)
		_, ok := env[constants.EnvVarMaxToolCalls]
		require.False(t, ok, "max-tool-calls budget must be unlimited when unset")
	})

	t.Run("omits budget outside SDK mode", func(t *testing.T) {
		t.Parallel()
		env := map[string]string{}
		engine.addCopilotWorkflowStepEnv(env, &WorkflowData{
			EngineConfig: &EngineConfig{ID: "copilot", MaxToolCalls: "150"},
		}, false)
		_, ok := env[constants.EnvVarMaxToolCalls]
		require.False(t, ok)
	})
}

func TestCopilotEngineExecutionStepsExportMaxToolCalls(t *testing.T) {
	t.Parallel()

	engine := NewCopilotEngine()
	steps := engine.GetExecutionSteps(&WorkflowData{
		Name:         "test-workflow",
		EngineConfig: &EngineConfig{CopilotSDK: true, MaxToolCalls: "${{ inputs.max-tool-calls }}"},
	}, "/tmp/gh-aw/test.log")
	require.Len(t, steps, 1)

	stepContent := strings.Join([]string(steps[0]), "\n")
	require.True(t,
		strings.Contains(stepContent, constants.EnvVarMaxToolCalls+": ${{ inputs.max-tool-calls }}") ||
			strings.Contains(stepContent, constants.EnvVarMaxToolCalls+`: "${{ inputs.max-tool-calls }}"`),
		"expected %s in execution step, got:\n%s", constants.EnvVarMaxToolCalls, stepContent)
}

func TestSetupEngineAndImports_ImportedTopLevelMaxToolCalls(t *testing.T) {
	tmpDir := testutil.TempDir(t, "engine-imported-max-tool-calls")

	sharedContent := `---
engine:
  id: copilot
  copilot-sdk: true
max-tool-calls: 90
---

# Shared Workflow
`
	sharedDir := filepath.Join(tmpDir, "shared")
	require.NoError(t, os.MkdirAll(sharedDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(sharedDir, "common.md"), []byte(sharedContent), 0644))

	testContent := `---
on: push
imports:
  - shared/common.md
---

# Test Workflow
`
	testFile := filepath.Join(tmpDir, "test.md")
	require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644))

	compiler := NewCompiler()
	content := []byte(testContent)
	frontmatterResult, err := parser.ExtractFrontmatterFromContent(string(content))
	require.NoError(t, err)

	result, err := compiler.setupEngineAndImports(frontmatterResult, testFile, content, tmpDir)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.engineConfig)
	require.Equal(t, "90", result.engineConfig.MaxToolCalls)
}
