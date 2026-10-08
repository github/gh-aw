//go:build !integration

package workflow

import (
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/require"
)

func TestBehaviorDefinedEngineCLIOnlyMCPInfrastructure(t *testing.T) {
	for _, tt := range []struct {
		name        string
		declaration string
		enabled     bool
	}{
		{name: "default", declaration: "{}"},
		{name: "disabled", declaration: "cli-only-mcp-infrastructure: false"},
		{name: "enabled", declaration: "cli-only-mcp-infrastructure: true", enabled: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			def := newHarnessEngineDefinition()
			require.NoError(t, yaml.Unmarshal([]byte(tt.declaration), &def.Behaviors.Capabilities))
			engine, err := NewBehaviorDefinedEngine(def)
			require.NoError(t, err)
			capabilities := engine.GetCapabilities()
			require.Equal(t, tt.enabled, capabilities.CLIOnlyMCPInfrastructure)

			data := &WorkflowData{
				EngineConfig: &EngineConfig{ID: def.ID},
				SafeOutputs:  &SafeOutputsConfig{NoOp: &NoOpConfig{}},
			}
			if tt.enabled {
				require.Equal(t, []string{"safeoutputs"}, getMCPCLIExcludeFromAgentConfig(data, capabilities))
			} else {
				require.Empty(t, getMCPCLIExcludeFromAgentConfig(data, capabilities))
			}
		})
	}
}

func TestPromptToolCLIOnlyMCPInfrastructure(t *testing.T) {
	for _, tt := range []struct {
		name       string
		capability bool
		cliProxy   bool
		server     string
		tool       string
		bare       bool
		excluded   bool
		reason     string
		canDisable bool
	}{
		{name: "default native infrastructure", server: "safeoutputs", tool: "noop"},
		{name: "CLI-only safeoutputs", capability: true, server: "safeoutputs", tool: "noop", excluded: true, reason: "engine's CLI-only MCP infrastructure capability"},
		{name: "CLI-only mcpscripts", capability: true, server: "mcpscripts", tool: "hello", excluded: true, reason: "engine's CLI-only MCP infrastructure capability"},
		{name: "bare infrastructure instruction remains available", capability: true, server: "safeoutputs", tool: "noop", bare: true},
		{name: "CLI-only infrastructure preserves custom native MCP", capability: true, server: "custom", tool: "lookup"},
		{name: "explicit proxy excludes infrastructure", cliProxy: true, server: "safeoutputs", tool: "noop", excluded: true, reason: "tools.cli-proxy", canDisable: true},
		{name: "capability still excludes infrastructure after disabling proxy", capability: true, cliProxy: true, server: "safeoutputs", tool: "noop", excluded: true, reason: "engine's CLI-only MCP infrastructure capability"},
		{name: "explicit proxy excludes custom MCP", capability: true, cliProxy: true, server: "custom", tool: "lookup", excluded: true, reason: "tools.cli-proxy", canDisable: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			data := &WorkflowData{
				EngineConfig: &EngineConfig{ID: "custom-capability-engine"},
				ParsedTools:  &Tools{CLIProxy: tt.cliProxy},
				SafeOutputs:  &SafeOutputsConfig{NoOp: &NoOpConfig{}},
				MCPScripts: &MCPScriptsConfig{Tools: map[string]*MCPScriptToolConfig{
					"hello": {Name: "hello", Script: "return 'ok';"},
				}},
				Tools: map[string]any{"custom": map[string]any{"type": "http", "url": "http://localhost:8080"}},
			}
			capabilities := EngineCapabilities{MCP: true, CLIOnlyMCPInfrastructure: tt.capability}
			requirement := promptToolRequirement{server: tt.server, tool: tt.tool, nativeMCP: !tt.bare}
			require.Equal(t, tt.excluded, promptToolTransportUnavailable(data, requirement, capabilities))
			require.Equal(t, !tt.excluded, promptToolAvailable(data, requirement, capabilities))
			if tt.excluded {
				warning := promptToolWarning(data, requirement, capabilities)
				require.Contains(t, warning, tt.reason)
				require.Contains(t, warning, "use the "+tt.server+" CLI instead")
				if tt.canDisable {
					require.Contains(t, warning, "disable tools.cli-proxy")
				} else {
					require.NotContains(t, warning, "disable tools.cli-proxy")
				}
			}
		})
	}
}
