// Package workflow provides built-in MCP server configuration rendering.
//
// # Built-in MCP Servers
//
// This file implements rendering functions for gh-aw's built-in MCP servers:
// safe-outputs, agentic-workflows, and their variations. These servers provide
// core functionality for AI agent workflows including controlled output storage,
// workflow execution, and memory management.
//
// Key responsibilities:
//   - Rendering safe-outputs MCP server configuration (HTTP transport)
//   - Rendering agentic-workflows MCP server configuration (stdio transport)
//   - Engine-specific format handling (JSON vs TOML)
//   - Managing HTTP server endpoints and authentication
//   - Configuring Docker containers for stdio servers
//   - Handling environment variable passthrough
//
// Built-in MCP servers:
//
// 1. Safe-outputs MCP server:
//   - Transport: HTTP (runs on host, accessed via HTTP)
//   - Port: 3001 (configurable via GH_AW_SAFE_OUTPUTS_PORT)
//   - Authentication: API key in Authorization header
//   - Purpose: Provides controlled storage for AI agent outputs
//   - Tools: add_issue_comment, create_issue, update_issue, upload_asset, etc.
//
// 2. Agentic-workflows MCP server:
//   - Transport: stdio (runs in Docker container)
//   - Container: Alpine Linux with gh-aw binary mounted (or localhost/gh-aw:dev in dev mode)
//   - Entrypoint: /opt/gh-aw/gh-aw mcp-server (release mode) or container default (dev mode)
//   - Network: Enabled via --network host for GitHub API access (api.github.com)
//   - Purpose: Enables workflow compilation, validation, and execution via gh aw CLI
//   - Tools: compile, validate, list, status, audit, logs, add, update, fix
//
// HTTP vs stdio transport:
// - HTTP: Server runs on host, accessible via HTTP URL with authentication
// - stdio: Server runs in Docker container, communicates via stdin/stdout
//
// Engine compatibility:
// The renderer supports multiple output formats:
//   - JSON (Copilot, Claude, Custom): JSON-like MCP configuration
//   - TOML (Codex): TOML-like MCP configuration
//
// Copilot-specific features:
// When IncludeCopilotFields is true, the renderer adds:
//   - "type" field: Specifies transport type (http or stdio)
//   - Backslash-escaped variables: \${VAR} for MCP passthrough
//
// Safe-outputs configuration:
// Safe-outputs runs as an HTTP server and requires:
//   - Port and API key from step outputs
//   - Config files: config.json, tools.json, validation.json
//   - Environment variables for feature configuration
//
// The HTTP URL uses either:
//   - host.docker.internal: When agent runs in firewall container
//   - localhost: When agent firewall is disabled (sandbox.agent.disabled)
//
// Agentic-workflows configuration:
// Agentic-workflows runs in a stdio container and requires:
//   - Mounted gh-aw binary from /opt/gh-aw (release mode) or baked into image (dev mode)
//   - Mounted gh CLI binary for GitHub API access (release mode) or baked into image (dev mode)
//   - Mounted workspace for workflow files
//   - Mounted temp directory for logs
//   - GITHUB_TOKEN for GitHub API access
//   - Network access enabled via --network host for api.github.com
//
// Related files:
//   - mcp_renderer.go: Main renderer that calls these functions
//   - mcp_setup_generator.go: Generates setup steps for these servers
//   - safe_outputs.go: Safe-outputs configuration and validation
//   - safe_inputs.go: Safe-inputs configuration (similar pattern)
//
// Example safe-outputs config:
//
//	{
//	  "safe_outputs": {
//	    "type": "http",
//	    "url": "http://host.docker.internal:$GH_AW_SAFE_OUTPUTS_PORT",
//	    "headers": {
//	      "Authorization": "$GH_AW_SAFE_OUTPUTS_API_KEY"
//	    }
//	  }
//	}
//
// Example agentic-workflows config:
//
//	{
//	  "agenticworkflows": {
//	    "type": "stdio",
//	    "container": "alpine:3.20",
//	    "entrypoint": "/opt/gh-aw/gh-aw",
//	    "entrypointArgs": ["mcp-server"],
//	    "mounts": ["/opt/gh-aw:/opt/gh-aw:ro", ...],
//	    "env": {
//	      "GITHUB_TOKEN": "$GITHUB_TOKEN"
//	    }
//	  }
//	}
package workflow

import (
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

var mcpBuiltinLog = logger.New("workflow:mcp-config-builtin")

// renderSafeOutputsMCPConfig generates the Safe Outputs MCP server configuration
// This is a shared function used by both Claude and Custom engines
func renderSafeOutputsMCPConfig(yaml *strings.Builder, isLast bool, workflowData *WorkflowData) {
	mcpBuiltinLog.Print("Rendering Safe Outputs MCP configuration")
	renderSafeOutputsMCPConfigWithOptions(yaml, isLast, false, workflowData)
}

// renderSafeOutputsMCPConfigWithOptions generates the Safe Outputs MCP server configuration with engine-specific options
// Now uses HTTP transport instead of stdio, similar to safe-inputs
// The server is started in a separate step before the agent job
func renderSafeOutputsMCPConfigWithOptions(yaml *strings.Builder, isLast bool, includeCopilotFields bool, workflowData *WorkflowData) {
	yaml.WriteString("              \"" + constants.SafeOutputsMCPServerID + "\": {\n")

	// HTTP transport configuration - server started in separate step
	// Add type field for HTTP (required by MCP specification for HTTP transport)
	yaml.WriteString("                \"type\": \"http\",\n")

	// Determine host based on whether agent is disabled
	host := "host.docker.internal"
	if workflowData != nil && workflowData.SandboxConfig != nil && workflowData.SandboxConfig.Agent != nil && workflowData.SandboxConfig.Agent.Disabled {
		// When agent is disabled (no firewall), use localhost instead of host.docker.internal
		host = "localhost"
	}

	// HTTP URL using environment variable - NOT escaped so shell expands it before awmg validation
	// Use host.docker.internal to allow access from firewall container (or localhost if agent disabled)
	// Note: awmg validates URL format before variable resolution, so we must expand the port variable
	yaml.WriteString("                \"url\": \"http://" + host + ":$GH_AW_SAFE_OUTPUTS_PORT\",\n")

	// Add Authorization header with API key
	yaml.WriteString("                \"headers\": {\n")
	if includeCopilotFields {
		// Copilot format: backslash-escaped shell variable reference
		yaml.WriteString("                  \"Authorization\": \"\\${GH_AW_SAFE_OUTPUTS_API_KEY}\"\n")
	} else {
		// Claude/Custom format: direct shell variable reference
		yaml.WriteString("                  \"Authorization\": \"$GH_AW_SAFE_OUTPUTS_API_KEY\"\n")
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

// renderAgenticWorkflowsMCPConfigWithOptions generates the Agentic Workflows MCP server configuration with engine-specific options
// Per MCP Gateway Specification v1.0.0 section 3.2.1, stdio-based MCP servers MUST be containerized.
// Uses MCP Gateway spec format: container, entrypoint, entrypointArgs, and mounts fields.
func renderAgenticWorkflowsMCPConfigWithOptions(yaml *strings.Builder, isLast bool, includeCopilotFields bool, actionMode ActionMode) {
	// Environment variables: map of env var name to value (literal) or source variable (reference)
	envVars := []struct {
		name      string
		value     string
		isLiteral bool
	}{
		{"DEBUG", "*", true},                    // Literal value "*"
		{"GITHUB_TOKEN", "GITHUB_TOKEN", false}, // Variable reference (gh CLI auto-sets GH_TOKEN from GITHUB_TOKEN if needed)
	}

	// Use MCP Gateway spec format with container, entrypoint, entrypointArgs, and mounts
	yaml.WriteString("              \"" + constants.AgenticWorkflowsMCPServerID + "\": {\n")

	// Add type field for Copilot (per MCP Gateway Specification v1.0.0, use "stdio" for containerized servers)
	if includeCopilotFields {
		yaml.WriteString("                \"type\": \"stdio\",\n")
	}

	// MCP Gateway spec fields for containerized stdio servers
	containerImage := constants.DefaultAlpineImage
	var entrypoint string
	var entrypointArgs []string
	var mounts []string

	if actionMode.IsDev() {
		// Dev mode: Use locally built Docker image which includes gh-aw binary and gh CLI
		// The Dockerfile sets ENTRYPOINT ["gh-aw"] and CMD ["mcp-server"]
		// Binary path is automatically detected via os.Executable()
		// So we don't need to specify entrypoint or entrypointArgs
		containerImage = constants.DevModeGhAwImage
		entrypoint = ""      // Use container's default entrypoint
		entrypointArgs = nil // Use container's default CMD
		// Only mount workspace and temp directory - binary and gh CLI are in the image
		mounts = []string{constants.DefaultWorkspaceMount, constants.DefaultTmpGhAwMount}
	} else {
		// Release mode: Use minimal Alpine image with mounted binaries
		// The gh-aw binary is mounted from /opt/gh-aw and executed directly
		entrypoint = "/opt/gh-aw/gh-aw"
		entrypointArgs = []string{"mcp-server"}
		// Mount gh-aw binary, gh CLI binary, workspace, and temp directory
		mounts = []string{constants.DefaultGhAwMount, constants.DefaultGhBinaryMount, constants.DefaultWorkspaceMount, constants.DefaultTmpGhAwMount}
	}

	yaml.WriteString("                \"container\": \"" + containerImage + "\",\n")

	// Only write entrypoint if it's specified (release mode)
	// In dev mode, use the container's default ENTRYPOINT
	if entrypoint != "" {
		yaml.WriteString("                \"entrypoint\": \"" + entrypoint + "\",\n")
	}

	// Only write entrypointArgs if specified (release mode)
	// In dev mode, use the container's default CMD
	if entrypointArgs != nil {
		yaml.WriteString("                \"entrypointArgs\": [\"mcp-server\"],\n")
	}

	// Write mounts
	yaml.WriteString("                \"mounts\": [")
	for i, mount := range mounts {
		if i > 0 {
			yaml.WriteString(", ")
		}
		yaml.WriteString("\"" + mount + "\"")
	}
	yaml.WriteString("],\n")

	// Add Docker runtime args:
	// - --network host: Enables network access for GitHub API calls (gh CLI needs api.github.com)
	// - -w: Sets working directory to workspace for .github/workflows folder resolution
	yaml.WriteString("                \"args\": [\"--network\", \"host\", \"-w\", \"${{ github.workspace }}\"],\n")

	// Note: tools field is NOT included here - the converter script adds it back
	// for Copilot. This keeps the gateway config compatible with the schema.

	// Write environment variables
	yaml.WriteString("                \"env\": {\n")
	for i, envVar := range envVars {
		isLastEnvVar := i == len(envVars)-1
		comma := ""
		if !isLastEnvVar {
			comma = ","
		}

		var valueStr string
		if envVar.isLiteral {
			// Literal value (e.g., DEBUG = "*")
			valueStr = envVar.value
		} else {
			// Variable reference
			if includeCopilotFields {
				// Copilot format: backslash-escaped shell variable reference
				valueStr = "\\${" + envVar.value + "}"
			} else {
				// Claude/Custom format: direct shell variable reference
				valueStr = "$" + envVar.value
			}
		}

		yaml.WriteString("                  \"" + envVar.name + "\": \"" + valueStr + "\"" + comma + "\n")
	}
	yaml.WriteString("                }\n")

	if isLast {
		yaml.WriteString("              }\n")
	} else {
		yaml.WriteString("              },\n")
	}
}

// renderSafeOutputsMCPConfigTOML generates the Safe Outputs MCP server configuration in TOML format for Codex
// Per MCP Gateway Specification v1.0.0 section 3.2.1, stdio-based MCP servers MUST be containerized.
// Uses MCP Gateway spec format: container, entrypoint, entrypointArgs, and mounts fields.
func renderSafeOutputsMCPConfigTOML(yaml *strings.Builder) {
	yaml.WriteString("          \n")
	yaml.WriteString("          [mcp_servers." + constants.SafeOutputsMCPServerID + "]\n")
	yaml.WriteString("          type = \"http\"\n")
	yaml.WriteString("          url = \"http://host.docker.internal:$GH_AW_SAFE_OUTPUTS_PORT\"\n")
	yaml.WriteString("          \n")
	yaml.WriteString("          [mcp_servers." + constants.SafeOutputsMCPServerID + ".headers]\n")
	yaml.WriteString("          Authorization = \"$GH_AW_SAFE_OUTPUTS_API_KEY\"\n")
}

// renderAgenticWorkflowsMCPConfigTOML generates the Agentic Workflows MCP server configuration in TOML format for Codex
// Per MCP Gateway Specification v1.0.0 section 3.2.1, stdio-based MCP servers MUST be containerized.
// Uses MCP Gateway spec format: container, entrypoint, entrypointArgs, and mounts fields.
func renderAgenticWorkflowsMCPConfigTOML(yaml *strings.Builder, actionMode ActionMode) {
	yaml.WriteString("          \n")
	yaml.WriteString("          [mcp_servers." + constants.AgenticWorkflowsMCPServerID + "]\n")

	containerImage := constants.DefaultAlpineImage
	var entrypoint string
	var entrypointArgs []string
	var mounts []string

	if actionMode.IsDev() {
		// Dev mode: Use locally built Docker image which includes gh-aw binary and gh CLI
		// The Dockerfile sets ENTRYPOINT ["gh-aw"] and CMD ["mcp-server", "--cmd", "gh-aw"]
		// So we don't need to specify entrypoint or entrypointArgs
		containerImage = constants.DevModeGhAwImage
		entrypoint = ""      // Use container's default ENTRYPOINT
		entrypointArgs = nil // Use container's default CMD
		// Only mount workspace and temp directory - binary and gh CLI are in the image
		mounts = []string{constants.DefaultWorkspaceMount, constants.DefaultTmpGhAwMount}
	} else {
		// Release mode: Use minimal Alpine image with mounted binaries
		entrypoint = "/opt/gh-aw/gh-aw"
		entrypointArgs = []string{"mcp-server"}
		// Mount gh-aw binary, gh CLI binary, workspace, and temp directory
		mounts = []string{constants.DefaultGhAwMount, constants.DefaultGhBinaryMount, constants.DefaultWorkspaceMount, constants.DefaultTmpGhAwMount}
	}

	yaml.WriteString("          container = \"" + containerImage + "\"\n")

	// Only write entrypoint if it's specified (release mode)
	// In dev mode, use the container's default ENTRYPOINT
	if entrypoint != "" {
		yaml.WriteString("          entrypoint = \"" + entrypoint + "\"\n")
	}

	// Only write entrypointArgs if specified (release mode)
	// In dev mode, use the container's default CMD
	if entrypointArgs != nil {
		yaml.WriteString("          entrypointArgs = [\"mcp-server\"]\n")
	}

	// Write mounts
	yaml.WriteString("          mounts = [")
	for i, mount := range mounts {
		if i > 0 {
			yaml.WriteString(", ")
		}
		yaml.WriteString("\"" + mount + "\"")
	}
	yaml.WriteString("]\n")

	// Add Docker runtime args:
	// - --network host: Enables network access for GitHub API calls (gh CLI needs api.github.com)
	// - -w: Sets working directory to workspace for .github/workflows folder resolution
	yaml.WriteString("          args = [\"--network\", \"host\", \"-w\", \"${{ github.workspace }}\"]\n")

	// Use env_vars array to reference environment variables instead of embedding secrets
	yaml.WriteString("          env_vars = [\"DEBUG\", \"GITHUB_TOKEN\"]\n")
}
