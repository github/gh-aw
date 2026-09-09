package workflow

import (
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/sliceutil"
)

var mcpRendererBuiltinLog = logger.New("workflow:mcp_renderer_builtin")

// RenderSafeOutputsMCP generates the Safe Outputs MCP server configuration
func (r *MCPConfigRendererUnified) RenderSafeOutputsMCP(yaml *strings.Builder, workflowData *WorkflowData) {
	mcpRendererLog.Printf("Rendering Safe Outputs MCP: format=%s", r.options.Format)

	if r.options.Format == "toml" {
		r.renderSafeOutputsTOML(yaml, workflowData)
		return
	}

	// JSON format
	renderSafeOutputsMCPConfigWithOptions(yaml, r.options.IsLast, r.options.IncludeCopilotFields, workflowData)
}

// renderSafeOutputsTOML generates Safe Outputs MCP configuration in TOML format
// Uses containerized stdio transport in the gh-aw-node image, overriding the container's
// default entrypoint to run the stdio MCP server script.
func (r *MCPConfigRendererUnified) renderSafeOutputsTOML(yaml *strings.Builder, workflowData *WorkflowData) {
	containerImage := resolveMCPGatewayContainerImage(constants.DefaultGhAwNodeImage, workflowData)
	yaml.WriteString("          \n")
	yaml.WriteString("          [mcp_servers." + constants.SafeOutputsMCPServerID.String() + "]\n")
	yaml.WriteString("          container = \"" + containerImage + "\"\n")
	yaml.WriteString("          mounts = [\"" + constants.DefaultWorkspaceMount + "\", \"" + constants.DefaultSafeOutputsMount + "\", \"" + constants.DefaultTmpGhAwMount + "\"]\n")
	yaml.WriteString("          args = [\"-w\", \"$GITHUB_WORKSPACE\"]\n")
	yaml.WriteString("          entrypoint = \"sh\"\n")
	yaml.WriteString("          entrypointArgs = [\"-c\", \"sh ${RUNNER_TEMP}/gh-aw/safeoutputs/start_safe_outputs_mcp.sh\"]\n")

	// Build the env_vars list: fixed vars + any GH_AW_INPUT_* vars referenced by the
	// safe-outputs config so the nested container can resolve ${GH_AW_INPUT_…} placeholders.
	safeOutputsEnvVars := []string{
		"DEBUG", "DEFAULT_BRANCH",
		"GH_AW_ASSETS_ALLOWED_EXTS", "GH_AW_ASSETS_BRANCH", "GH_AW_ASSETS_MAX_SIZE_KB",
		"GH_AW_MCP_LOG_DIR", "GH_AW_SAFE_OUTPUTS", "GH_AW_SAFE_OUTPUTS_CONFIG_PATH",
		"GH_AW_SAFE_OUTPUTS_TOOLS_PATH", "GH_AW_POLICY_ALLOW_CREATE_PULL_REQUEST",
		"GH_AW_PR_HEAD_BASE_BRANCH", "GH_AW_PR_HEAD_BASE_SHA", "GH_AW_PR_HEAD_BASE_REPO",
		"GH_AW_PR_HEAD_BASE_PR_NUMBER", "GH_AW_PR_HEAD_BASE_REF", "GH_AW_PR_HEAD_REPO",
		"GITHUB_EVENT_NAME", "GITHUB_EVENT_PATH", "GITHUB_REPOSITORY", "GITHUB_SHA",
		"GITHUB_TOKEN", "GITHUB_WORKSPACE", "RUNNER_TEMP",
	}
	if workflowData != nil {
		safeOutputsEnvVars = append(safeOutputsEnvVars, sliceutil.SortedKeys(workflowData.SafeOutputsInputEnvVars)...)
	}
	quoted := make([]string, len(safeOutputsEnvVars))
	for i, v := range safeOutputsEnvVars {
		quoted[i] = "\"" + v + "\""
	}
	yaml.WriteString("          env_vars = [" + strings.Join(quoted, ", ") + "]\n")

	// Check if GitHub tool has guard-policies configured (or auto-lockdown will run)
	// If so, generate a linked write-sink guard-policy for safeoutputs
	guardPolicies := deriveWriteSinkGuardPolicyFromWorkflow(workflowData)
	if len(guardPolicies) > 0 {
		mcpRendererLog.Print("Adding guard-policies to safeoutputs TOML (derived from GitHub guard-policy or auto-lockdown detection)")
		// Render guard-policies in TOML format
		renderGuardPoliciesToml(yaml, guardPolicies, constants.SafeOutputsMCPServerID.String())
	}
}

// RenderMCPScriptsMCP generates the MCP Scripts server configuration
func (r *MCPConfigRendererUnified) RenderMCPScriptsMCP(yaml *strings.Builder, mcpScripts *MCPScriptsConfig, workflowData *WorkflowData) {
	mcpRendererLog.Printf("Rendering MCP Scripts: format=%s", r.options.Format)

	if r.options.Format == "toml" {
		r.renderMCPScriptsTOML(yaml, mcpScripts, workflowData)
		// Add guard policies for TOML format as a separate section
		if len(r.options.WriteSinkGuardPolicies) > 0 {
			mcpRendererLog.Print("Adding guard-policies to mcp-scripts TOML (derived from GitHub guard-policy)")
			renderGuardPoliciesToml(yaml, r.options.WriteSinkGuardPolicies, constants.MCPScriptsMCPServerID.String())
		}
		return
	}

	// JSON format
	renderMCPScriptsMCPConfigWithOptions(yaml, mcpScripts, r.options.IsLast, r.options.IncludeCopilotFields, workflowData, r.options.WriteSinkGuardPolicies)
}

// renderMCPScriptsTOML generates MCP Scripts configuration in TOML format
// Uses HTTP transport exclusively
func (r *MCPConfigRendererUnified) renderMCPScriptsTOML(yaml *strings.Builder, mcpScripts *MCPScriptsConfig, workflowData *WorkflowData) {
	yaml.WriteString("          \n")
	yaml.WriteString("          [mcp_servers." + constants.MCPScriptsMCPServerID.String() + "]\n")
	yaml.WriteString("          type = \"http\"\n")

	// Determine host based on whether agent is disabled
	host := "host.docker.internal"
	if workflowData != nil && workflowData.SandboxConfig != nil && workflowData.SandboxConfig.Agent != nil && workflowData.SandboxConfig.Agent.Disabled {
		// When agent is disabled (no firewall), use localhost instead of host.docker.internal
		host = "localhost"
		mcpRendererLog.Print("Using localhost for mcp-scripts (agent disabled)")
	} else {
		mcpRendererLog.Print("Using host.docker.internal for mcp-scripts (agent enabled)")
	}

	yaml.WriteString("          url = \"http://" + host + ":$GH_AW_MCP_SCRIPTS_PORT\"\n")
	yaml.WriteString("          headers = { Authorization = \"$GH_AW_MCP_SCRIPTS_API_KEY\" }\n")
	// Note: env_vars is not supported for HTTP transport in MCP configuration
	// Environment variables are passed via the workflow job's env: section instead
}

// RenderAgenticWorkflowsMCP generates the Agentic Workflows MCP server configuration
func (r *MCPConfigRendererUnified) RenderAgenticWorkflowsMCP(yaml *strings.Builder) {
	mcpRendererLog.Printf("Rendering Agentic Workflows MCP: format=%s, action_mode=%s", r.options.Format, r.options.ActionMode)

	if r.options.Format == "toml" {
		r.renderAgenticWorkflowsTOML(yaml)
		// Add guard policies for TOML format as a separate section
		if len(r.options.WriteSinkGuardPolicies) > 0 {
			mcpRendererLog.Print("Adding guard-policies to agentic-workflows TOML (derived from GitHub guard-policy)")
			renderGuardPoliciesToml(yaml, r.options.WriteSinkGuardPolicies, constants.AgenticWorkflowsMCPServerID.String())
		}
		return
	}

	// JSON format
	renderAgenticWorkflowsMCPConfigWithOptions(yaml, r.options.IsLast, r.options.IncludeCopilotFields, r.options.ActionMode, r.options.WriteSinkGuardPolicies, r.options.ContainerPinMappings)
}

// renderAgenticWorkflowsTOML generates Agentic Workflows MCP configuration in TOML format
// Per MCP Gateway Specification v1.0.0 section 3.2.1, stdio-based MCP servers MUST be containerized.
func (r *MCPConfigRendererUnified) renderAgenticWorkflowsTOML(yaml *strings.Builder) { //nolint:largefunc // Existing TOML emission preserves field ordering.
	mcpRendererBuiltinLog.Printf("Rendering Agentic Workflows MCP in TOML format: action_mode=%s", r.options.ActionMode)
	yaml.WriteString("          \n")
	yaml.WriteString("          [mcp_servers." + constants.AgenticWorkflowsMCPServerID.String() + "]\n")

	containerImage := constants.DefaultAlpineImage
	var entrypoint string
	var entrypointArgs []string
	var mounts []string

	if r.options.ActionMode.IsDev() {
		// Dev mode: Use locally built Docker image which includes gh-aw binary and gh CLI
		// The Dockerfile sets ENTRYPOINT ["gh-aw"] and CMD ["mcp-server", "--validate-actor"]
		// So we don't need to specify entrypoint or entrypointArgs
		containerImage = constants.DevModeGhAwImage
		entrypoint = ""      // Use container's default ENTRYPOINT
		entrypointArgs = nil // Use container's default CMD
		// Only mount workspace and temp directory - binary and gh CLI are in the image
		mounts = []string{constants.DefaultWorkspaceMount, constants.DefaultTmpGhAwMount}
	} else {
		// Release mode: Use minimal Alpine image with mounted binaries
		entrypoint = GhAwBinaryPath
		entrypointArgs = []string{"mcp-server", "--validate-actor"}
		// Mount gh-aw binary, gh CLI binary, workspace, and temp directory
		mounts = []string{constants.DefaultGhAwMount, constants.DefaultGhBinaryMount, constants.DefaultWorkspaceMount, constants.DefaultTmpGhAwMount}
	}

	// Apply container_pins mapping so private-cloud runners use the configured mirror.
	containerImage = resolveGatewayContainerFromMappings(containerImage, r.options.ContainerPinMappings)

	yaml.WriteString("          container = \"" + containerImage + "\"\n")

	// Only write entrypoint if it's specified (release mode)
	// In dev mode, use the container's default ENTRYPOINT
	if entrypoint != "" {
		yaml.WriteString("          entrypoint = \"" + entrypoint + "\"\n")
	}

	// Only write entrypointArgs if specified (release mode)
	// In dev mode, use the container's default CMD
	if entrypointArgs != nil {
		yaml.WriteString("          entrypointArgs = [")
		for i, arg := range entrypointArgs {
			if i > 0 {
				yaml.WriteString(", ")
			}
			yaml.WriteString("\"" + arg + "\"")
		}
		yaml.WriteString("]\n")
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

	yaml.WriteString("          env_vars = [\"DEBUG\", \"GH_TOKEN\", \"GITHUB_TOKEN\", \"GITHUB_ACTOR\", \"GITHUB_REPOSITORY\"]\n")
}

// renderSafeOutputsMCPConfigWithOptions generates the Safe Outputs MCP server configuration with engine-specific options.
// The server runs as a containerized stdio MCP server in the published gh-aw node image.
func renderSafeOutputsMCPConfigWithOptions(yaml *strings.Builder, isLast bool, includeCopilotFields bool, workflowData *WorkflowData) { //nolint:largefunc // Existing config emission preserves field ordering.
	mcpRendererBuiltinLog.Printf("Rendering Safe Outputs MCP config with options: isLast=%v, includeCopilotFields=%v", isLast, includeCopilotFields)
	containerImage := resolveMCPGatewayContainerImage(constants.DefaultGhAwNodeImage, workflowData)
	yaml.WriteString("              \"" + constants.SafeOutputsMCPServerID.String() + "\": {\n")

	if includeCopilotFields {
		yaml.WriteString("                \"type\": \"stdio\",\n")
	}
	yaml.WriteString("                \"container\": \"" + containerImage + "\",\n")
	yaml.WriteString("                \"mounts\": [\"" + constants.DefaultWorkspaceMount + "\", \"" + constants.DefaultSafeOutputsMount + "\", \"" + constants.DefaultTmpGhAwMount + "\"],\n")
	yaml.WriteString("                \"args\": [\"-w\", \"\\${GITHUB_WORKSPACE}\"],\n")
	yaml.WriteString("                \"entrypoint\": \"sh\",\n")
	yaml.WriteString("                \"entrypointArgs\": [\"-c\", \"sh ${RUNNER_TEMP}/gh-aw/safeoutputs/start_safe_outputs_mcp.sh\"],\n")
	yaml.WriteString("                \"env\": {\n")

	envVars := []struct {
		name      string
		value     string
		isLiteral bool
	}{
		{"DEBUG", "*", true},
		{"DEFAULT_BRANCH", "DEFAULT_BRANCH", false},
		{"GH_AW_ASSETS_ALLOWED_EXTS", "GH_AW_ASSETS_ALLOWED_EXTS", false},
		{"GH_AW_ASSETS_BRANCH", "GH_AW_ASSETS_BRANCH", false},
		{"GH_AW_ASSETS_MAX_SIZE_KB", "GH_AW_ASSETS_MAX_SIZE_KB", false},
		{"GH_AW_MCP_LOG_DIR", "GH_AW_MCP_LOG_DIR", false},
		{"GH_AW_SAFE_OUTPUTS", "GH_AW_SAFE_OUTPUTS", false},
		{"GH_AW_SAFE_OUTPUTS_CONFIG_PATH", "GH_AW_SAFE_OUTPUTS_CONFIG_PATH", false},
		{"GH_AW_SAFE_OUTPUTS_TOOLS_PATH", "GH_AW_SAFE_OUTPUTS_TOOLS_PATH", false},
		{"GH_AW_POLICY_ALLOW_CREATE_PULL_REQUEST", "GH_AW_POLICY_ALLOW_CREATE_PULL_REQUEST", false},
		{"GH_AW_PR_HEAD_BASE_BRANCH", "GH_AW_PR_HEAD_BASE_BRANCH", false},
		{"GH_AW_PR_HEAD_BASE_SHA", "GH_AW_PR_HEAD_BASE_SHA", false},
		{"GH_AW_PR_HEAD_BASE_REPO", "GH_AW_PR_HEAD_BASE_REPO", false},
		{"GH_AW_PR_HEAD_BASE_PR_NUMBER", "GH_AW_PR_HEAD_BASE_PR_NUMBER", false},
		{"GH_AW_PR_HEAD_BASE_REF", "GH_AW_PR_HEAD_BASE_REF", false},
		{"GH_AW_PR_HEAD_REPO", "GH_AW_PR_HEAD_REPO", false},
		{"GITHUB_EVENT_NAME", "GITHUB_EVENT_NAME", false},
		{"GITHUB_EVENT_PATH", "GITHUB_EVENT_PATH", false},
		{"GITHUB_REPOSITORY", "GITHUB_REPOSITORY", false},
		{"GITHUB_SHA", "GITHUB_SHA", false},
		{"GITHUB_TOKEN", "GITHUB_TOKEN", false},
		{"GITHUB_WORKSPACE", "GITHUB_WORKSPACE", false},
		{"RUNNER_TEMP", "RUNNER_TEMP", false},
	}

	// Append GH_AW_INPUT_* vars referenced by the safe-outputs config so the nested
	// container can resolve ${GH_AW_INPUT_…} shell-style placeholders at runtime.
	if workflowData != nil {
		for _, name := range sliceutil.SortedKeys(workflowData.SafeOutputsInputEnvVars) {
			envVars = append(envVars, struct {
				name      string
				value     string
				isLiteral bool
			}{name, name, false})
		}
	}

	for i, envVar := range envVars {
		comma := ","
		if i == len(envVars)-1 {
			comma = ""
		}
		var valueStr string
		if envVar.isLiteral {
			valueStr = envVar.value
		} else {
			// Always use backslash-escaped shell variable references in JSON MCP config heredocs.
			// The heredoc delimiter is unquoted so bash would expand $VAR before the gateway
			// script runs; escaping ensures the literal ${VAR} string is passed to the gateway,
			// which resolves it from its own environment without leaking secret values in logs.
			valueStr = "\\${" + envVar.value + "}"
		}
		yaml.WriteString("                  \"" + envVar.name + "\": \"" + valueStr + "\"" + comma + "\n")
	}
	yaml.WriteString("                }")

	// Check if GitHub tool has guard-policies configured (or auto-lockdown will run)
	// If so, generate a linked write-sink guard-policy for safeoutputs
	guardPolicies := deriveWriteSinkGuardPolicyFromWorkflow(workflowData)

	// Add guard-policies if configured
	if len(guardPolicies) > 0 {
		mcpRendererBuiltinLog.Print("Adding guard-policies to safeoutputs (derived from GitHub guard-policy or auto-lockdown detection)")
		yaml.WriteString(",\n")
		renderGuardPoliciesJSON(yaml, guardPolicies, "                ")
	} else {
		yaml.WriteString("\n")
	}

	if isLast {
		yaml.WriteString("              }\n")
	} else {
		yaml.WriteString("              },\n")
	}
}

// renderAgenticWorkflowsMCPConfigWithOptions generates the Agentic Workflows MCP server configuration with engine-specific options
// Per MCP Gateway Specification v1.0.0 section 3.2.1, stdio-based MCP servers MUST be containerized.
// Uses MCP Gateway spec format: container, entrypoint, entrypointArgs, and mounts fields.
func renderAgenticWorkflowsMCPConfigWithOptions(yaml *strings.Builder, isLast bool, includeCopilotFields bool, actionMode ActionMode, guardPolicies map[string]any, containerPinMappings map[string]string) { //nolint:largefunc // Existing config emission preserves field ordering.
	mcpRendererBuiltinLog.Printf("Rendering Agentic Workflows MCP config: isLast=%v, includeCopilotFields=%v, actionMode=%v", isLast, includeCopilotFields, actionMode)

	// Environment variables: map of env var name to value (literal) or source variable (reference)
	envVars := []struct {
		name      string
		value     string
		isLiteral bool
	}{
		{"DEBUG", "*", true},                              // Literal value "*"
		{"GITHUB_TOKEN", "GITHUB_TOKEN", false},           // Variable reference (gh CLI auto-sets GH_TOKEN from GITHUB_TOKEN if needed)
		{"GITHUB_ACTOR", "GITHUB_ACTOR", false},           // Variable reference for actor-based access control
		{"GITHUB_REPOSITORY", "GITHUB_REPOSITORY", false}, // Variable reference for repository context
	}

	// Use MCP Gateway spec format with container, entrypoint, entrypointArgs, and mounts
	yaml.WriteString("              \"" + constants.AgenticWorkflowsMCPServerID.String() + "\": {\n")

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
		mcpRendererBuiltinLog.Print("Using dev mode configuration with locally built Docker image")
		// Dev mode: Use locally built Docker image which includes gh-aw binary and gh CLI
		// The Dockerfile sets ENTRYPOINT ["gh-aw"] and CMD ["mcp-server", "--validate-actor"]
		// Binary path is automatically detected via os.Executable()
		// So we don't need to specify entrypoint or entrypointArgs
		containerImage = constants.DevModeGhAwImage
		entrypoint = ""      // Use container's default entrypoint
		entrypointArgs = nil // Use container's default CMD
		// Only mount workspace and temp directory - binary and gh CLI are in the image
		mounts = []string{constants.DefaultWorkspaceMount, constants.DefaultTmpGhAwMount}
	} else {
		// Release mode: Use minimal Alpine image with mounted binaries
		// The gh-aw binary is mounted from ${RUNNER_TEMP}/gh-aw and executed directly
		// Pass --validate-actor flag to enable role-based access control
		entrypoint = GhAwBinaryPath
		entrypointArgs = []string{"mcp-server", "--validate-actor"}
		// Mount gh-aw binary, gh CLI binary, workspace, and temp directory
		mounts = []string{constants.DefaultGhAwMount, constants.DefaultGhBinaryMount, constants.DefaultWorkspaceMount, constants.DefaultTmpGhAwMount}
	}

	// Apply container_pins mapping so private-cloud runners use the configured mirror.
	containerImage = resolveGatewayContainerFromMappings(containerImage, containerPinMappings)

	yaml.WriteString("                \"container\": \"" + containerImage + "\",\n")

	// Only write entrypoint if it's specified (release mode)
	// In dev mode, use the container's default ENTRYPOINT
	if entrypoint != "" {
		yaml.WriteString("                \"entrypoint\": \"" + entrypoint + "\",\n")
	}

	// Only write entrypointArgs if specified (release mode)
	// In dev mode, use the container's default CMD
	if entrypointArgs != nil {
		yaml.WriteString("                \"entrypointArgs\": [")
		for i, arg := range entrypointArgs {
			if i > 0 {
				yaml.WriteString(", ")
			}
			yaml.WriteString("\"" + arg + "\"")
		}
		yaml.WriteString("],\n")
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
	// Security: Use GITHUB_WORKSPACE environment variable instead of template expansion to prevent template injection
	yaml.WriteString("                \"args\": [\"--network\", \"host\", \"-w\", \"\\${GITHUB_WORKSPACE}\"],\n")

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
			// Always use backslash-escaped shell variable references in JSON MCP config heredocs.
			// The heredoc delimiter is unquoted so bash would expand $VAR before the gateway
			// script runs; escaping ensures the literal ${VAR} string is passed to the gateway,
			// which resolves it from its own environment without leaking secret values in logs.
			valueStr = "\\${" + envVar.value + "}"
		}

		yaml.WriteString("                  \"" + envVar.name + "\": \"" + valueStr + "\"" + comma + "\n")
	}
	// Close env section - with or without trailing comma depending on whether guard policies follow
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
