package workflow

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiagnosticsToolParsing(t *testing.T) {
	for _, value := range []any{"go", []any{"go", "typescript", "python"}, []string{"python"}} {
		tools, err := ParseToolsConfig(map[string]any{"diagnostics": value})
		require.NoError(t, err)
		require.True(t, tools.HasTool("diagnostics"))
		assert.NotContains(t, tools.Custom, "diagnostics")
		assert.Contains(t, tools.GetToolNames(), "diagnostics")
		assert.Equal(t, value, tools.ToMap()["diagnostics"])
	}
	for _, value := range []any{nil, false, "", "rust", []any{}, []any{"go", "go"}, []any{"go", 1}} {
		_, err := ParseToolsConfig(map[string]any{"diagnostics": value})
		require.Error(t, err, "value: %#v", value)
		require.Error(t, NewTools(map[string]any{"diagnostics": value}).ParseError())
	}
}

func TestDiagnosticsCompilation(t *testing.T) {
	for _, engine := range []string{"pi", "claude", "codex", "{id: copilot, copilot-sdk: true}"} {
		for _, selection := range []string{"go", "[go, typescript, python]"} {
			t.Run(engine+selection, func(t *testing.T) {
				content := fmt.Sprintf("---\non: workflow_dispatch\npermissions:\n  contents: read\nengine: %s\ntools:\n  diagnostics: %s\n  bash: false\n  cli-proxy: false\n  github: false\n---\nDiagnose command output.\n", engine, selection)
				data, err := NewCompiler(WithSkipValidation(true)).ParseWorkflowString(content, "diagnostics.md")
				require.NoError(t, err)
				assert.NotContains(t, data.ParsedTools.Custom, "diagnostics")
				switch engine {
				case "pi":
					steps := NewPiEngine().GetExecutionSteps(data, "/tmp/agent.log")
					text := strings.Join(steps[0], "\n")
					assert.Contains(t, text, "GH_AW_DIAGNOSTICS")
					assert.Contains(t, text, "pi_diagnostics_extension.cjs")
				case "claude":
					text := strings.Join(NewClaudeEngine().GetExecutionSteps(data, "/tmp/agent.log")[0], "\n")
					assert.Contains(t, text, "GH_AW_DIAGNOSTICS")
					assert.Contains(t, text, "claude_diagnostics.cjs")
					assert.NotContains(t, text, "permissionDecision")
				case "codex":
					config, err := NewCodexEngine().buildNativeConfig(data, nil)
					require.NoError(t, err)
					assert.Equal(t, data.ParsedTools.Diagnostics, config.Diagnostics)
					assert.NotContains(t, config.Defaults["mcp_servers"], "diagnostics")
				default:
					config := buildCopilotSDKToolConfig(data, []string{"--allow-tool", "read"})
					assert.Equal(t, data.ParsedTools.Diagnostics, config.Diagnostics)
					assert.False(t, config.Capabilities.MCP)
					assert.False(t, config.Capabilities.Bash)
					assert.Equal(t, []string{"read"}, config.Permissions.AllowedTools)
				}
			})
		}
	}
}

func TestDiagnosticsSchemaAndUnsupportedEngines(t *testing.T) {
	for _, selection := range []string{"rust", "false", "null", "[]", "[go, go]", "[python, 42]"} {
		content := fmt.Sprintf("---\non: workflow_dispatch\nengine: pi\ntools:\n  diagnostics: %s\n---\nTest.\n", selection)
		_, err := NewCompiler(WithSkipValidation(true)).ParseWorkflowString(content, "invalid-diagnostics.md")
		require.Error(t, err, "selection: %s", selection)
	}
	data := &WorkflowData{Tools: map[string]any{"diagnostics": "go"}, EngineConfig: &EngineConfig{ID: "gemini"}}
	require.ErrorContains(t, validateDiagnosticsEngine(data), "Copilot SDK")
	data.Tools = nil
	require.NoError(t, validateDiagnosticsEngine(data))
}

func TestDiagnosticsOmittedLeavesPiCommandUnchanged(t *testing.T) {
	content := "---\non: workflow_dispatch\nengine: pi\ntools:\n  bash: false\n  github: false\n  cli-proxy: false\n---\nTest.\n"
	data, err := NewCompiler(WithSkipValidation(true)).ParseWorkflowString(content, "no-diagnostics.md")
	require.NoError(t, err)
	text := strings.Join(NewPiEngine().GetExecutionSteps(data, "/tmp/agent.log")[0], "\n")
	assert.NotContains(t, text, "GH_AW_DIAGNOSTICS")
	assert.NotContains(t, text, "pi_diagnostics_extension.cjs")
}

func TestDiagnosticsCodexHookRequirements(t *testing.T) {
	data := &WorkflowData{Tools: map[string]any{"diagnostics": "go"}, EngineConfig: &EngineConfig{ID: "codex", Version: "0.118.0"}}
	require.ErrorContains(t, validateDiagnosticsEngine(data), "0.159.3")
	data.EngineConfig.Version = "0.159.3"
	require.NoError(t, validateDiagnosticsEngine(data))
	data.EngineConfig.Config = "[features]\nhooks = false\n"
	require.ErrorContains(t, validateDiagnosticsEngine(data), "requires Codex hooks")
	data.EngineConfig.Config = ""
	for _, args := range [][]string{{"--disable", "hooks"}, {"--disable=hooks"}, {"-c", "features.hooks=false"}} {
		data.EngineConfig.Args = args
		require.ErrorContains(t, validateDiagnosticsEngine(data), "engine.args")
	}
}

func TestDiagnosticsDriverConfiguration(t *testing.T) {
	data := &WorkflowData{Tools: map[string]any{"diagnostics": []any{"typescript", "python"}}, EngineConfig: &EngineConfig{ID: "pi"}}
	for _, driver := range []string{"", "pi_agent_core_driver.cjs", "pi_rpc_driver.cjs"} {
		data.EngineConfig.Driver = driver
		env := map[string]string{}
		applyPiToolPolicyEnv(env, data)
		var languages []string
		require.NoError(t, json.Unmarshal([]byte(env["GH_AW_DIAGNOSTICS"]), &languages))
		assert.Equal(t, []string{"typescript", "python"}, languages)
	}
	env := map[string]string{}
	applyPiToolPolicyEnv(env, &WorkflowData{})
	assert.NotContains(t, env, "GH_AW_DIAGNOSTICS")
}
