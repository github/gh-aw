package workflow

import (
	"sort"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

var safeInputsRendererLog = logger.New("workflow:safe_inputs_renderer")

// getSafeInputsEnvVars returns the list of environment variables needed for safe-inputs
func getSafeInputsEnvVars(safeInputs *SafeInputsConfig) []string {
	envVars := []string{}
	seen := make(map[string]bool)

	if safeInputs == nil {
		safeInputsRendererLog.Print("No safe-inputs configuration provided")
		return envVars
	}

	safeInputsRendererLog.Printf("Collecting environment variables from %d safe-inputs tools", len(safeInputs.Tools))

	for _, toolConfig := range safeInputs.Tools {
		for envName := range toolConfig.Env {
			if !seen[envName] {
				envVars = append(envVars, envName)
				seen[envName] = true
			}
		}
	}

	sort.Strings(envVars)
	safeInputsRendererLog.Printf("Collected %d unique environment variables", len(envVars))
	return envVars
}

// collectSafeInputsSecrets collects all secrets from safe-inputs configuration
func collectSafeInputsSecrets(safeInputs *SafeInputsConfig) map[string]string {
	secrets := make(map[string]string)

	if safeInputs == nil {
		safeInputsRendererLog.Print("No safe-inputs configuration provided for secret collection")
		return secrets
	}

	safeInputsRendererLog.Printf("Collecting secrets from %d safe-inputs tools", len(safeInputs.Tools))

	// Sort tool names for consistent behavior when same env var appears in multiple tools
	toolNames := make([]string, 0, len(safeInputs.Tools))
	for toolName := range safeInputs.Tools {
		toolNames = append(toolNames, toolName)
	}
	sort.Strings(toolNames)

	for _, toolName := range toolNames {
		toolConfig := safeInputs.Tools[toolName]
		// Sort env var names for consistent order within each tool
		envNames := make([]string, 0, len(toolConfig.Env))
		for envName := range toolConfig.Env {
			envNames = append(envNames, envName)
		}
		sort.Strings(envNames)

		for _, envName := range envNames {
			secrets[envName] = toolConfig.Env[envName]
		}
	}

	safeInputsRendererLog.Printf("Collected %d secrets from safe-inputs configuration", len(secrets))
	return secrets
}

// renderSafeInputsMCPConfigWithOptions generates the Safe Inputs MCP server configuration with engine-specific options
// Only supports HTTP transport mode
func renderSafeInputsMCPConfigWithOptions(yaml *strings.Builder, safeInputs *SafeInputsConfig, isLast bool, includeCopilotFields bool, workflowData *WorkflowData) {
	safeInputsRendererLog.Printf("Rendering Safe Inputs MCP config: includeCopilotFields=%t, isLast=%t",
		includeCopilotFields, isLast)

	yaml.WriteString("              \"" + constants.SafeInputsMCPServerID + "\": {\n")

	// HTTP transport configuration - server started in separate step
	// Add type field for HTTP (required by MCP specification for HTTP transport)
	yaml.WriteString("                \"type\": \"http\",\n")

	// Determine host based on whether agent is disabled
	host := "host.docker.internal"
	if workflowData != nil && workflowData.SandboxConfig != nil && workflowData.SandboxConfig.Agent != nil && workflowData.SandboxConfig.Agent.Disabled {
		// When agent is disabled (no firewall), use localhost instead of host.docker.internal
		host = "localhost"
		safeInputsRendererLog.Print("Agent disabled, using localhost for Safe Inputs MCP server")
	}

	// HTTP URL using environment variable - NOT escaped so shell expands it before awmg validation
	// Use host.docker.internal to allow access from firewall container (or localhost if agent disabled)
	// Note: awmg validates URL format before variable resolution, so we must expand the port variable
	yaml.WriteString("                \"url\": \"http://" + host + ":$GH_AW_SAFE_INPUTS_PORT\",\n")

	// Add Authorization header with API key
	yaml.WriteString("                \"headers\": {\n")
	if includeCopilotFields {
		// Copilot format: backslash-escaped shell variable reference
		yaml.WriteString("                  \"Authorization\": \"\\${GH_AW_SAFE_INPUTS_API_KEY}\"\n")
	} else {
		// Claude/Custom format: direct shell variable reference
		yaml.WriteString("                  \"Authorization\": \"$GH_AW_SAFE_INPUTS_API_KEY\"\n")
	}
	// Close headers - no trailing comma since this is the last field
	// Note: env block is NOT included for HTTP servers because the old MCP Gateway schema
	// doesn't allow env in httpServerConfig. The variables are resolved via URL templates.
	yaml.WriteString("                }\n")

	if isLast {
		yaml.WriteString("              }\n")
	} else {
		yaml.WriteString("              },\n")
	}
}
