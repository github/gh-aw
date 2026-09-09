//go:build !integration

package workflow

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeMCPGatewayStepEnvForTest(yaml *strings.Builder, mcpEnvVars map[string]string, safeOutputsInputEnvVars map[string]string, gatewayEnvVars map[string]string) {
	writeMCPGatewayStepEnvWithCustomGatewayEnvNames(yaml, mcpEnvVars, safeOutputsInputEnvVars, gatewayEnvVars, sanitizedGatewayEnvNames(gatewayEnvVars), "")
}

func appendMCPGatewayCustomAndHTTPEnvFlagsForTest(containerCmd *strings.Builder, workflowData *WorkflowData, gatewayConfig *MCPGatewayRuntimeConfig, mcpEnvVars map[string]string, hasGitHub bool, githubTool map[string]any, tools map[string]any, engine CodingAgentEngine) {
	appendMCPGatewayCustomAndHTTPEnvFlagsWithCustomGatewayEnvNames(containerCmd, workflowData, sanitizedGatewayEnvNames(gatewayConfig.Env), mcpEnvVars, hasGitHub, githubTool, tools, engine)
}

func TestMCPGatewayCustomEnvValuesStayOutOfRunScript(t *testing.T) {
	gatewayEnv := map[string]string{
		"AAA_INJECT":   "legit; echo PWNED_$(id) > /tmp/pwned #",
		"BBB_NEWLINE":  "ok\n          echo PWNED_NEWLINE",
		"CCC_BACKTICK": "`touch /tmp/PWNED_BACKTICK`",
	}

	var stepEnv strings.Builder
	writeMCPGatewayStepEnvForTest(&stepEnv, nil, nil, gatewayEnv)

	assert.Contains(t, stepEnv.String(), `GH_AW_MCP_GATEWAY_ENV_0: "legit; echo PWNED_$(id) > /tmp/pwned #"`)
	assert.Contains(t, stepEnv.String(), `GH_AW_MCP_GATEWAY_ENV_1: "ok\n          echo PWNED_NEWLINE"`)
	assert.Contains(t, stepEnv.String(), "GH_AW_MCP_GATEWAY_ENV_2: \"`touch /tmp/PWNED_BACKTICK`\"")
	assert.Contains(t, stepEnv.String(), `GH_AW_MCP_GATEWAY_CUSTOM_ENV_NAMES: "[\"AAA_INJECT\",\"BBB_NEWLINE\",\"CCC_BACKTICK\"]"`)
	assert.NotContains(t, stepEnv.String(), "AAA_INJECT:")
	assert.NotContains(t, stepEnv.String(), "BBB_NEWLINE:")
	assert.NotContains(t, stepEnv.String(), "CCC_BACKTICK:")

	var runScript strings.Builder
	writeMCPGatewayExports(&runScript, writeMCPGatewayExportsOptions{
		engine:        NewCopilotEngine(),
		workflowData:  &WorkflowData{},
		gatewayConfig: &MCPGatewayRuntimeConfig{Env: gatewayEnv},
		port:          8080,
		domain:        "localhost",
		payloadDir:    "/tmp/payloads",
	})

	assert.NotContains(t, runScript.String(), "AAA_INJECT")
	assert.NotContains(t, runScript.String(), "BBB_NEWLINE")
	assert.NotContains(t, runScript.String(), "CCC_BACKTICK")

	var containerCommand strings.Builder
	appendMCPGatewayCustomAndHTTPEnvFlagsForTest(
		&containerCommand,
		&WorkflowData{},
		&MCPGatewayRuntimeConfig{Env: gatewayEnv},
		nil,
		false,
		nil,
		nil,
		NewCopilotEngine(),
	)
	assert.Equal(t, " "+mcpGatewayCustomEnvMarker, containerCommand.String())
}

func TestMCPGatewayCustomEnvOverridesGeneratedStepEnv(t *testing.T) {
	var yaml strings.Builder
	writeMCPGatewayStepEnvForTest(
		&yaml,
		map[string]string{
			"API_TOKEN":               "${{ secrets.DEFAULT_TOKEN }}",
			"GH_AW_MCP_GATEWAY_ENV_0": "reserved-value",
		},
		map[string]string{"TARGET_REPO": "${{ inputs.target_repo }}"},
		map[string]string{
			"API_TOKEN":   "custom-token",
			"TARGET_REPO": "custom-repo",
		},
	)

	output := yaml.String()
	require.Zero(t, strings.Count(output, "API_TOKEN:"))
	require.Zero(t, strings.Count(output, "TARGET_REPO:"))
	assert.Contains(t, output, `GH_AW_MCP_GATEWAY_ENV_0: "custom-token"`)
	assert.Contains(t, output, `GH_AW_MCP_GATEWAY_ENV_1: "custom-repo"`)
	assert.NotContains(t, output, "API_TOKEN:")
	assert.NotContains(t, output, "TARGET_REPO:")
	assert.NotContains(t, output, "reserved-value")
}

func TestMCPGatewayCustomEnvDoesNotSetBashEnvOnHost(t *testing.T) {
	var yaml strings.Builder
	writeMCPGatewayStepEnvForTest(&yaml, nil, nil, map[string]string{
		"BASH_ENV": "$(touch /tmp/pwned)",
	})

	assert.Contains(t, yaml.String(), `GH_AW_MCP_GATEWAY_ENV_0: "$(touch /tmp/pwned)"`)
	assert.NotContains(t, yaml.String(), "BASH_ENV:")
}

func TestMCPGatewayCustomEnvPreservesGitHubExpressionAsData(t *testing.T) {
	var yaml strings.Builder
	writeMCPGatewayStepEnvForTest(&yaml, nil, nil, map[string]string{
		"API_TOKEN": "${{ inputs.api_token }}",
	})

	assert.Contains(t, yaml.String(), `GH_AW_MCP_GATEWAY_ENV_0: "${{ inputs.api_token }}"`)
	assert.NotContains(t, yaml.String(), "API_TOKEN:")
}

func TestMCPGatewayCustomEnvReservesTransportMetadataName(t *testing.T) {
	var yaml strings.Builder
	writeMCPGatewayStepEnvForTest(&yaml, map[string]string{
		mcpGatewayCustomEnvNamesVar: "${{ secrets.COLLISION }}",
	}, nil, map[string]string{
		"API_TOKEN": "custom-token",
	})

	output := yaml.String()
	assert.Equal(t, 1, strings.Count(output, mcpGatewayCustomEnvNamesVar+":"))
	assert.Contains(t, output, `GH_AW_MCP_GATEWAY_CUSTOM_ENV_NAMES: "[\"API_TOKEN\"]"`)
	assert.NotContains(t, output, "${{ secrets.COLLISION }}")
}

func TestMCPGatewayCustomEnvReservesTransportPrefix(t *testing.T) {
	var yaml strings.Builder
	writeMCPGatewayStepEnvForTest(&yaml, map[string]string{
		"GH_AW_MCP_GATEWAY_ENV_5": "${{ secrets.COLLISION }}",
	}, nil, map[string]string{
		"API_TOKEN": "custom-token",
	})

	output := yaml.String()
	assert.Contains(t, output, `GH_AW_MCP_GATEWAY_CUSTOM_ENV_NAMES: "[\"API_TOKEN\"]"`)
	assert.Contains(t, output, `GH_AW_MCP_GATEWAY_ENV_0: "custom-token"`)
	assert.NotContains(t, output, "GH_AW_MCP_GATEWAY_ENV_5")
	assert.NotContains(t, output, "${{ secrets.COLLISION }}")
}

func TestMCPGatewayCustomEnvCommandContract(t *testing.T) {
	gatewayConfig := &MCPGatewayRuntimeConfig{
		Container: "ghcr.io/github/gh-aw-mcpg",
		Env:       map[string]string{"API_TOKEN": "value"},
	}
	workflowData := &WorkflowData{
		SandboxConfig: &SandboxConfig{MCP: gatewayConfig},
	}

	command := buildMCPGatewayContainerCommand(buildMCPGatewayContainerCommandOptions{
		engine:                NewCopilotEngine(),
		workflowData:          workflowData,
		gatewayConfig:         gatewayConfig,
		customGatewayEnvNames: sanitizedGatewayEnvNames(gatewayConfig.Env),
	})

	markerIndex := strings.Index(command, mcpGatewayCustomEnvMarker)
	imageIndex := strings.Index(command, "ghcr.io/github/gh-aw-mcpg:")
	require.GreaterOrEqual(t, markerIndex, 0, "Docker command should contain the custom environment marker")
	require.Greater(t, imageIndex, markerIndex, "Custom environment marker should appear before the container image")
	assert.NotContains(t, command, "GH_AW_MCP_GATEWAY_ENV_0")
	assert.NotContains(t, command, "API_TOKEN=value")

	launcher, err := os.ReadFile("../../actions/setup/js/start_mcp_gateway.cjs")
	require.NoError(t, err)
	assert.Contains(t, string(launcher), `const customGatewayEnvMarker = "`+mcpGatewayCustomEnvMarker+`"`)
	assert.Contains(t, string(launcher), mcpGatewayCustomEnvNamesVar)
	assert.Contains(t, string(launcher), `const customGatewayEnvTransportPrefix = "`+mcpGatewayCustomEnvTransportPrefix+`"`)
	assert.Contains(t, string(launcher), "${customGatewayEnvTransportPrefix}${index}")
}

func TestMCPGatewayCustomEnvNamesAreFilteredAtEmissionBoundary(t *testing.T) {
	gatewayEnv := map[string]string{
		"API_TOKEN":                 "custom-token",
		"BAD-NAME":                  "shell-unsafe",
		"lowercase":                 "shell-unsafe",
		"NAME; touch /tmp/pwned":    "shell-unsafe",
		"$(touch /tmp/pwned)":       "shell-unsafe",
		mcpGatewayCustomEnvNamesVar: "reserved",
		"GH_AW_MCP_GATEWAY_ENV_3":   "reserved",
		"GH_AW_MCP_GATEWAY_FOO":     "reserved",
	}

	assert.Equal(t, []string{"API_TOKEN"}, sanitizedGatewayEnvNames(gatewayEnv))

	var yaml strings.Builder
	writeMCPGatewayStepEnvForTest(&yaml, nil, nil, gatewayEnv)

	output := yaml.String()
	assert.Contains(t, output, `GH_AW_MCP_GATEWAY_CUSTOM_ENV_NAMES: "[\"API_TOKEN\"]"`)
	assert.Contains(t, output, `GH_AW_MCP_GATEWAY_ENV_0: "custom-token"`)
	assert.NotContains(t, output, "shell-unsafe")
	assert.NotContains(t, output, "reserved")
}

func TestMCPGatewayFilteredCustomEnvIsOmittedFromStepEnv(t *testing.T) {
	var yaml strings.Builder
	writeMCPGatewayStepEnvForTest(&yaml, nil, nil, map[string]string{"BAD-NAME": "shell-unsafe"})

	assert.Empty(t, yaml.String())
}

func TestMCPGatewayCustomEnvMarkerOmittedWhenAllNamesFiltered(t *testing.T) {
	gatewayEnv := map[string]string{"BAD-NAME": "shell-unsafe"}

	var containerCommand strings.Builder
	appendMCPGatewayCustomAndHTTPEnvFlagsForTest(
		&containerCommand,
		&WorkflowData{},
		&MCPGatewayRuntimeConfig{Env: gatewayEnv},
		map[string]string{"HTTP_MCP_TOKEN": "forwarded"},
		false,
		nil,
		nil,
		NewCopilotEngine(),
	)
	assert.NotContains(t, containerCommand.String(), mcpGatewayCustomEnvMarker)
}

func TestMCPGatewayFilteredCustomEnvDoesNotSuppressHTTPMCPEnvForwarding(t *testing.T) {
	var containerCommand strings.Builder
	appendMCPGatewayCustomAndHTTPEnvFlagsForTest(
		&containerCommand,
		&WorkflowData{},
		&MCPGatewayRuntimeConfig{Env: map[string]string{"GH_AW_MCP_GATEWAY_ENV_0": "filtered"}},
		map[string]string{"GH_AW_MCP_GATEWAY_ENV_0": "forwarded"},
		false,
		nil,
		nil,
		NewCopilotEngine(),
	)

	assert.Equal(t, " -e GH_AW_MCP_GATEWAY_ENV_0", containerCommand.String())
}

func TestMCPGatewayConfiguredAgentIDPassedViaStepEnv(t *testing.T) {
	configuredAgentID := `my-agent-id"; touch /tmp/pwned; #`

	var stepEnv strings.Builder
	writeMCPGatewayStepEnvWithCustomGatewayEnvNames(&stepEnv, nil, nil, nil, nil, configuredAgentID)

	assert.Contains(t, stepEnv.String(), `GH_AW_MCP_GATEWAY_CONFIGURED_AGENT_ID: "my-agent-id\"; touch /tmp/pwned; #"`)

	var runScript strings.Builder
	writeMCPGatewayExports(&runScript, writeMCPGatewayExportsOptions{
		engine:        NewCopilotEngine(),
		workflowData:  &WorkflowData{},
		gatewayConfig: &MCPGatewayRuntimeConfig{AgentID: configuredAgentID},
		port:          8080,
		domain:        "localhost",
		payloadDir:    "/tmp/payloads",
	})

	assert.Contains(t, runScript.String(), `export MCP_GATEWAY_AGENT_ID="${GH_AW_MCP_GATEWAY_CONFIGURED_AGENT_ID}"`)
	assert.NotContains(t, runScript.String(), `export MCP_GATEWAY_AGENT_ID="my-agent-id`)
}
