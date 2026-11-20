//go:build !integration

package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompatWebFetchRouting(t *testing.T) {
	for _, tt := range []struct {
		name     string
		engine   string
		model    string
		sdk      bool
		disabled bool
		runtime  AgentRuntime
		want     bool
		err      string
	}{
		{name: "CLI", engine: "copilot", want: true},
		{name: "SDK retains its fetch", engine: "copilot", sdk: true},
		{name: "Claude Copilot", engine: "claude", model: "copilot/claude-sonnet-4.6", want: true},
		{name: "Claude native", engine: "claude"},
		{name: "Codex native", engine: "codex"},
		{name: "Gemini native", engine: "gemini"},
		{name: "OpenCode native", engine: "opencode"},
		{name: "no firewall", engine: "copilot", disabled: true, err: "requires the agent firewall"},
		{name: "Pi consumer", engine: "pi", want: true},
		{name: "NVX consumer", engine: "copilot", runtime: AgentRuntimeNVX, err: "unavailable with sandbox.agent.runtime: nvx"},
		{name: "custom consumer", engine: "custom", err: "MCP consumer does not support"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			data := &WorkflowData{
				EngineConfig:  &EngineConfig{ID: tt.engine, CopilotSDK: tt.sdk},
				Model:         tt.model,
				Tools:         map[string]any{"web-fetch": nil},
				SandboxConfig: &SandboxConfig{Agent: &AgentSandboxConfig{ID: "awf", Disabled: tt.disabled, Runtime: tt.runtime}},
			}
			assert.Equal(t, tt.want, compatWebFetchEnabled(data))
			err := validateCompatWebFetch(data)
			if tt.err != "" {
				require.ErrorContains(t, err, tt.err)
			} else {
				require.NoError(t, err)
			}
			if tt.want {
				assert.True(t, HasMCPServers(data))
				assert.Empty(t, collectMCPTools(data), "compat must not be a gateway backend")
				data.Tools["compat"] = map[string]any{"command": "custom"}
				require.ErrorContains(t, validateCompatWebFetch(data), "reserved")
			}
			data.Tools = map[string]any{"web-fetch": false}
			assert.False(t, compatWebFetchEnabled(data))
			require.NoError(t, validateCompatWebFetch(data))
		})
	}
}

func TestCompatWebFetchOnlyCompilation(t *testing.T) {
	for _, engine := range []string{"copilot", "claude", "pi"} {
		t.Run(engine, func(t *testing.T) {
			model := ""
			if engine == "claude" {
				model = "model: copilot/claude-haiku-4.5\n"
			}
			source := fmt.Sprintf("---\non: workflow_dispatch\nengine: %s\n%spermissions:\n  contents: read\n  copilot-requests: write\ncheckout: false\ntools:\n  bash: false\n  cli-proxy: false\n  edit: false\n  github: false\n  web-fetch:\nsafe-outputs:\n  create-issue: false\n---\nFetch an allowed URL.\n", engine, model)
			compiler := NewCompiler(WithSkipValidation(true))
			workflowPath := filepath.Join(t.TempDir(), "compat.md")
			require.NoError(t, os.WriteFile(workflowPath, []byte(source), 0600))
			require.NoError(t, compiler.CompileWorkflow(workflowPath))
			lock, err := os.ReadFile(stringutil.MarkdownToLockFile(workflowPath))
			require.NoError(t, err)
			agent := extractJobSection(string(lock), "agent")
			assert.Contains(t, agent, "Install compat WebFetch dependencies")
			assert.Contains(t, agent, "undici@6.29.1")
			assert.Contains(t, agent, "export GH_AW_COMPAT_WEB_FETCH=1")
			assert.Contains(t, agent, "Start MCP Gateway")
			assert.NotContains(t, agent, `"compat": {`, "compat must not be rendered as a gateway backend")
			switch engine {
			case "copilot":
				assert.Contains(t, agent, "/compat_mcp_config.cjs")
				assert.Contains(t, agent, "--allow-tool compat")
			case "claude":
				assert.Contains(t, agent, "/compat_mcp_config.cjs")
				assert.Contains(t, agent, "mcp__compat__web_fetch")
				assert.Contains(t, agent, `--mcp-config "`+claudeCompatMCPConfigPath(nil)+`"`)
			default:
				assert.Contains(t, agent, "/pi_runtime.cjs")
			}
		})
	}
}

func TestCompatWebFetchBootstrapWithoutGatewayTools(t *testing.T) {
	for _, engine := range []CodingAgentEngine{NewCopilotEngine(), NewClaudeEngine(), NewPiEngine()} {
		t.Run(engine.GetID(), func(t *testing.T) {
			data := &WorkflowData{
				EngineConfig:  &EngineConfig{ID: engine.GetID()},
				Tools:         map[string]any{"web-fetch": nil},
				SandboxConfig: &SandboxConfig{Agent: &AgentSandboxConfig{ID: "awf"}},
			}
			if engine.GetID() == "claude" {
				data.Model = "copilot/claude-haiku-4.5"
			}
			require.Empty(t, collectMCPTools(data))
			var yaml strings.Builder
			require.NoError(t, NewCompiler().generateMCPSetup(&yaml, data.Tools, engine, data))
			assert.Contains(t, yaml.String(), "Start MCP Gateway")
			assert.Contains(t, yaml.String(), "export GH_AW_COMPAT_WEB_FETCH=1")
			assert.NotContains(t, yaml.String(), `"compat": {`)
		})
	}
}

func TestCompatWebFetchRuntimePathsAndOrder(t *testing.T) {
	for _, tt := range []struct {
		name     string
		runtime  AgentRuntime
		topology RunnerTopology
		path     string
	}{
		{name: "Docker", runtime: AgentRuntimeDocker, path: "/tmp/gh-aw/agent/compat-mcp-config.json"},
		{name: "Cloud Hypervisor", runtime: AgentRuntimeCloudHypervisor, path: "/tmp/gh-aw/agent/compat-mcp-config.json"},
		{name: "ARC DinD", topology: RunnerTopologyArcDind, path: "${RUNNER_TEMP}/gh-aw/sandbox/agent/compat-mcp-config.json"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			data := &WorkflowData{
				EngineConfig:  &EngineConfig{ID: "claude"},
				Model:         "copilot/claude-haiku-4.5",
				Tools:         map[string]any{"web-fetch": nil},
				RunnerConfig:  &RunnerConfig{Topology: tt.topology},
				SandboxConfig: &SandboxConfig{Agent: &AgentSandboxConfig{ID: "awf", Runtime: tt.runtime}},
			}
			assert.Equal(t, tt.path, claudeCompatMCPConfigPath(data))
			command := NewClaudeEngine().buildClaudeFullCommand(data, "claude", "agent.log")
			if tt.topology == RunnerTopologyArcDind {
				assert.Contains(t, command, `--mount "${RUNNER_TEMP}/gh-aw/sandbox/agent:${RUNNER_TEMP}/gh-aw/sandbox/agent:rw"`)
			} else {
				assert.NotContains(t, tt.path, "${RUNNER_TEMP}/gh-aw", "default gh-aw runtime tree is read-only")
			}
			assertRuntimePATHBeforeCompat(t, command)
			data.EngineConfig.ID = "copilot"
			assertRuntimePATHBeforeCompat(t, NewCopilotEngine().buildCopilotFirewallCommand(data, "copilot", "", true, "agent.log"))
		})
	}
}

func assertRuntimePATHBeforeCompat(t *testing.T, command string) {
	t.Helper()
	pathPosition := strings.Index(command, "GH_AW_TOOL_BINS=")
	bindPosition := strings.Index(command, "compat_mcp_config.cjs")
	require.NotEqual(t, -1, pathPosition)
	require.Greater(t, bindPosition, pathPosition, "repair hostedtoolcache PATH before resolving the compat Node runtime")
}
