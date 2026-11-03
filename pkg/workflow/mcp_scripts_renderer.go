package workflow

import (
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/sliceutil"
)

var mcpScriptsRendererLog = logger.New("workflow:mcp_scripts_renderer")

// collectMCPScriptsSecrets collects all secrets from mcp-scripts configuration
func collectMCPScriptsSecrets(mcpScripts *MCPScriptsConfig) map[string]string {
	secrets := make(map[string]string)

	if mcpScripts == nil {
		mcpScriptsRendererLog.Print("No mcp-scripts configuration provided for secret collection")
		return secrets
	}

	mcpScriptsRendererLog.Printf("Collecting secrets from %d mcp-scripts tools", len(mcpScripts.Tools))

	// Keep each tool's binding distinct even when variable names overlap.
	toolNames := sliceutil.SortedKeys(mcpScripts.Tools)

	for _, toolName := range toolNames {
		toolConfig := mcpScripts.Tools[toolName]
		// Sort env var names for consistent order within each tool
		envNames := sliceutil.SortedKeys(toolConfig.Env)

		for _, envName := range envNames {
			secrets[mcpScriptEnvBindingName(toolName, envName)] = toolConfig.Env[envName]
		}
	}

	mcpScriptsRendererLog.Printf("Collected %d secrets from mcp-scripts configuration", len(secrets))
	return secrets
}

// renderMCPScriptsMCPConfigWithOptions generates the MCP Scripts server configuration with engine-specific options
// Always uses HTTP transport mode
func renderMCPScriptsMCPConfigWithOptions(yaml *strings.Builder, mcpScripts *MCPScriptsConfig, isLast bool, includeCopilotFields bool, workflowData *WorkflowData, guardPolicies map[string]any) {
	mcpScriptsRendererLog.Printf("Rendering MCP Scripts config: includeCopilotFields=%t, isLast=%t",
		includeCopilotFields, isLast)

	yaml.WriteString("              \"" + constants.MCPScriptsMCPServerID.String() + "\": {\n")

	// HTTP transport configuration - server started in separate step
	// Add type field for HTTP (required by MCP specification for HTTP transport)
	yaml.WriteString("                \"type\": \"http\",\n")

	// Determine host based on whether agent is disabled
	host := "host.docker.internal"
	if workflowData != nil && workflowData.SandboxConfig != nil && workflowData.SandboxConfig.Agent != nil && workflowData.SandboxConfig.Agent.Disabled {
		// When agent is disabled (no firewall), use localhost instead of host.docker.internal
		host = "localhost"
		mcpScriptsRendererLog.Print("Agent disabled, using localhost for MCP Scripts server")
	}

	// HTTP URL using environment variable - NOT escaped so shell expands it before awmg validation
	// Use host.docker.internal to allow access from firewall container (or localhost if agent disabled)
	// Note: awmg validates URL format before variable resolution, so we must expand the port variable
	yaml.WriteString("                \"url\": \"http://" + host + ":$GH_AW_MCP_SCRIPTS_PORT\",\n")

	// Add Authorization header with API key
	yaml.WriteString("                \"headers\": {\n")
	// Always use backslash-escaped shell variable references in JSON MCP config heredocs.
	// The heredoc delimiter is unquoted so bash would expand $VAR before the gateway
	// script runs; escaping ensures the literal ${VAR} string is passed to the gateway,
	// which resolves it from its own environment without leaking secret values in logs.
	yaml.WriteString("                  \"Authorization\": \"\\${GH_AW_MCP_SCRIPTS_API_KEY}\"\n")
	// Close headers - with or without trailing comma depending on whether guard policies follow
	// Note: env block is NOT included for HTTP servers because the old MCP Gateway schema
	// doesn't allow env in httpServerConfig. The variables are resolved via URL templates.
	if len(guardPolicies) > 0 {
		yaml.WriteString("                },\n")
		renderGuardPoliciesJSON(yaml, guardPolicies, "                ")
	} else {
		yaml.WriteString("                }\n")
	}

	if isLast {
		yaml.WriteString("              }\n")
	} else {
		yaml.WriteString("              },\n")
	}
}
