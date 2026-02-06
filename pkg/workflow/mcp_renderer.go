// Package workflow provides YAML rendering for MCP server configurations.
//
// # MCP Configuration Renderer
//
// This file implements the unified MCP configuration renderer that generates
// YAML configuration for various MCP servers across different AI engines
// (Copilot, Claude, Codex, Custom). It provides a consistent interface for
// rendering MCP configurations while handling engine-specific format requirements.
//
// Key responsibilities:
//   - Unified rendering interface for all MCP server types
//   - Engine-specific format handling (JSON-like vs TOML-like)
//   - GitHub MCP server configuration (local Docker and remote hosted)
//   - Playwright MCP server configuration
//   - Safe-outputs and safe-inputs MCP server configuration
//   - Agentic-workflows MCP server configuration
//   - Cache-memory MCP server configuration
//   - Serena MCP server configuration
//   - Custom HTTP and stdio MCP server configuration
//
// Renderer architecture:
// The renderer uses the MCPConfigRendererUnified struct with MCPRendererOptions
// to configure engine-specific behaviors:
//   - IncludeCopilotFields: Add "type" and "tools" fields for Copilot
//   - InlineArgs: Render args inline (Copilot) vs multi-line (Claude/Custom)
//   - Format: "json" for JSON-like or "toml" for TOML-like output
//   - IsLast: Control trailing commas in rendered configuration
//
// Supported MCP server types:
//   - GitHub: Local (Docker) or remote (hosted) GitHub API access
//   - Playwright: Browser automation with domain restrictions
//   - Safe-outputs: Controlled output storage for AI agents
//   - Safe-inputs: Custom tool execution with secret passthrough
//   - Cache-memory: Memory/knowledge base management
//   - Agentic-workflows: Workflow execution via gh-aw extension
//   - Serena: Local search functionality
//   - Custom HTTP: User-defined HTTP-based MCP servers
//   - Custom stdio: User-defined stdio-based MCP servers
//
// Engine-specific rendering:
//   - Copilot: JSON format with "type" and "tools" fields, inline args
//   - Claude: JSON format without Copilot fields, multi-line args
//   - Codex: TOML format for MCP configuration
//   - Custom: Same as Claude (JSON, multi-line args)
//
// Configuration structure:
// All MCP servers follow the MCP Gateway Specification v1.0.0:
//   - HTTP servers: type, url, headers
//   - Stdio servers: type, container, entrypoint, entrypointArgs, mounts, env
//
// Variable resolution:
// The renderer handles two types of variable syntax:
//   - Shell variables: $VAR or ${VAR} - resolved by shell before gateway
//   - Gateway variables: ${VAR} in JSON - resolved by gateway at runtime
//
// Copilot-specific features:
// Copilot uses backslash-escaped variables (\${VAR}) for proper MCP passthrough
// and includes additional fields required by the Copilot MCP specification.
//
// Related files:
//   - mcp_github_config.go: GitHub MCP server configuration
//   - mcp_config_playwright_renderer.go: Playwright-specific rendering
//   - mcp_config_builtin.go: Built-in MCP server rendering
//   - mcp_config_custom.go: Custom MCP server rendering
//   - mcp_setup_generator.go: Calls renderer for configuration generation
//
// Example usage:
//
//	renderer := NewMCPConfigRenderer(MCPRendererOptions{
//	    IncludeCopilotFields: true,
//	    InlineArgs: true,
//	    Format: "json",
//	    IsLast: false,
//	})
//	renderer.RenderGitHubMCP(yaml, githubTool, workflowData)
package workflow

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

var mcpRendererLog = logger.New("workflow:mcp_renderer")

// MCPRendererOptions contains configuration options for the unified MCP renderer
type MCPRendererOptions struct {
	// IncludeCopilotFields indicates if the engine requires "type" and "tools" fields (true for copilot engine)
	IncludeCopilotFields bool
	// InlineArgs indicates if args should be rendered inline (true for copilot) or multi-line (false for claude/custom)
	InlineArgs bool
	// Format specifies the output format ("json" for JSON-like, "toml" for TOML-like)
	Format string
	// IsLast indicates if this is the last server in the configuration (affects trailing comma)
	IsLast bool
	// ActionMode indicates the action mode for workflow compilation (dev, release, script)
	ActionMode ActionMode
}

// MCPConfigRendererUnified provides unified rendering methods for MCP configurations
// across different engines (Claude, Copilot, Codex, Custom)
type MCPConfigRendererUnified struct {
	options MCPRendererOptions
}

// NewMCPConfigRenderer creates a new unified MCP config renderer with the specified options
func NewMCPConfigRenderer(opts MCPRendererOptions) *MCPConfigRendererUnified {
	mcpRendererLog.Printf("Creating MCP renderer: format=%s, copilot_fields=%t, inline_args=%t, is_last=%t",
		opts.Format, opts.IncludeCopilotFields, opts.InlineArgs, opts.IsLast)
	return &MCPConfigRendererUnified{
		options: opts,
	}
}

// RenderGitHubMCP generates the GitHub MCP server configuration
// Supports both local (Docker) and remote (hosted) modes
func (r *MCPConfigRendererUnified) RenderGitHubMCP(yaml *strings.Builder, githubTool any, workflowData *WorkflowData) {
	githubType := getGitHubType(githubTool)
	readOnly := getGitHubReadOnly(githubTool)

	// Get lockdown value - use detected value if lockdown wasn't explicitly set
	lockdown := getGitHubLockdown(githubTool)

	// Check if automatic lockdown determination step will be generated
	// The step is always generated when lockdown is not explicitly set
	shouldUseStepOutput := !hasGitHubLockdownExplicitlySet(githubTool)

	if shouldUseStepOutput {
		// Use the detected lockdown value from the step output
		// This will be evaluated at runtime based on repository visibility
		lockdown = true // This is a placeholder - actual value comes from step output
	}

	toolsets := getGitHubToolsets(githubTool)

	mcpRendererLog.Printf("Rendering GitHub MCP: type=%s, read_only=%t, lockdown=%t (explicit=%t, use_step=%t), toolsets=%v, format=%s",
		githubType, readOnly, lockdown, hasGitHubLockdownExplicitlySet(githubTool), shouldUseStepOutput, toolsets, r.options.Format)

	if r.options.Format == "toml" {
		r.renderGitHubTOML(yaml, githubTool, workflowData)
		return
	}

	yaml.WriteString("              \"github\": {\n")

	// Check if remote mode is enabled (type: remote)
	if githubType == "remote" {
		// Determine authorization value based on engine requirements
		// Copilot uses MCP passthrough syntax: "Bearer \${GITHUB_PERSONAL_ACCESS_TOKEN}"
		// Other engines use shell variable: "Bearer $GITHUB_MCP_SERVER_TOKEN"
		authValue := "Bearer $GITHUB_MCP_SERVER_TOKEN"
		if r.options.IncludeCopilotFields {
			authValue = "Bearer \\${GITHUB_PERSONAL_ACCESS_TOKEN}"
		}

		RenderGitHubMCPRemoteConfig(yaml, GitHubMCPRemoteOptions{
			ReadOnly:           readOnly,
			Lockdown:           lockdown,
			LockdownFromStep:   shouldUseStepOutput,
			Toolsets:           toolsets,
			AuthorizationValue: authValue,
			IncludeToolsField:  r.options.IncludeCopilotFields,
			AllowedTools:       getGitHubAllowedTools(githubTool),
			IncludeEnvSection:  r.options.IncludeCopilotFields,
		})
	} else {
		// Local mode - use Docker-based GitHub MCP server (default)
		githubDockerImageVersion := getGitHubDockerImageVersion(githubTool)
		customArgs := getGitHubCustomArgs(githubTool)
		mounts := getGitHubMounts(githubTool)

		RenderGitHubMCPDockerConfig(yaml, GitHubMCPDockerOptions{
			ReadOnly:           readOnly,
			Lockdown:           lockdown,
			LockdownFromStep:   shouldUseStepOutput,
			Toolsets:           toolsets,
			DockerImageVersion: githubDockerImageVersion,
			CustomArgs:         customArgs,
			Mounts:             mounts,
			IncludeTypeField:   r.options.IncludeCopilotFields,
			AllowedTools:       getGitHubAllowedTools(githubTool),
			EffectiveToken:     "", // Token passed via env
		})
	}

	if r.options.IsLast {
		yaml.WriteString("              }\n")
	} else {
		yaml.WriteString("              },\n")
	}
}

// RenderPlaywrightMCP generates the Playwright MCP server configuration
func (r *MCPConfigRendererUnified) RenderPlaywrightMCP(yaml *strings.Builder, playwrightTool any) {
	mcpRendererLog.Printf("Rendering Playwright MCP: format=%s, inline_args=%t", r.options.Format, r.options.InlineArgs)

	// Parse playwright tool configuration to strongly-typed struct
	playwrightConfig := parsePlaywrightTool(playwrightTool)

	if r.options.Format == "toml" {
		r.renderPlaywrightTOML(yaml, playwrightConfig)
		return
	}

	// JSON format
	renderPlaywrightMCPConfigWithOptions(yaml, playwrightConfig, r.options.IsLast, r.options.IncludeCopilotFields, r.options.InlineArgs)
}

// renderPlaywrightTOML generates Playwright MCP configuration in TOML format
// Per MCP Gateway Specification v1.0.0 section 3.2.1, stdio-based MCP servers MUST be containerized.
// Uses MCP Gateway spec format: container, entrypointArgs, mounts, and args fields.
func (r *MCPConfigRendererUnified) renderPlaywrightTOML(yaml *strings.Builder, playwrightConfig *PlaywrightToolConfig) {
	args := generatePlaywrightDockerArgs(playwrightConfig)
	customArgs := getPlaywrightCustomArgs(playwrightConfig)

	// Use official Playwright MCP Docker image (no version tag - only one image)
	playwrightImage := "mcr.microsoft.com/playwright/mcp"

	yaml.WriteString("          \n")
	yaml.WriteString("          [mcp_servers.playwright]\n")
	yaml.WriteString("          container = \"" + playwrightImage + "\"\n")

	// Docker runtime args (goes before container image in docker run command)
	yaml.WriteString("          args = [\n")
	yaml.WriteString("            \"--init\",\n")
	yaml.WriteString("            \"--network\",\n")
	yaml.WriteString("            \"host\",\n")
	yaml.WriteString("          ]\n")

	// Entrypoint args for Playwright MCP server (goes after container image)
	yaml.WriteString("          entrypointArgs = [\n")
	yaml.WriteString("            \"--output-dir\",\n")
	yaml.WriteString("            \"/tmp/gh-aw/mcp-logs/playwright\"")
	if len(args.AllowedDomains) > 0 {
		domainsStr := strings.Join(args.AllowedDomains, ";")
		yaml.WriteString(",\n")
		yaml.WriteString("            \"--allowed-hosts\",\n")
		yaml.WriteString("            \"" + domainsStr + "\",\n")
		yaml.WriteString("            \"--allowed-origins\",\n")
		yaml.WriteString("            \"" + domainsStr + "\"")
	}

	// Append custom args if present
	writeArgsToYAML(yaml, customArgs, "            ")

	yaml.WriteString("\n")
	yaml.WriteString("          ]\n")

	// Add volume mounts
	yaml.WriteString("          mounts = [\"/tmp/gh-aw/mcp-logs:/tmp/gh-aw/mcp-logs:rw\"]\n")
}

// RenderSerenaMCP generates Serena MCP server configuration
func (r *MCPConfigRendererUnified) RenderSerenaMCP(yaml *strings.Builder, serenaTool any) {
	mcpRendererLog.Printf("Rendering Serena MCP: format=%s, inline_args=%t", r.options.Format, r.options.InlineArgs)

	if r.options.Format == "toml" {
		r.renderSerenaTOML(yaml, serenaTool)
		return
	}

	// JSON format
	renderSerenaMCPConfigWithOptions(yaml, serenaTool, r.options.IsLast, r.options.IncludeCopilotFields, r.options.InlineArgs)
}

// renderSerenaTOML generates Serena MCP configuration in TOML format
// Supports two modes:
// - "docker" (default): Uses Docker container with stdio transport
// - "local": Uses local uvx with HTTP transport
func (r *MCPConfigRendererUnified) renderSerenaTOML(yaml *strings.Builder, serenaTool any) {
	customArgs := getSerenaCustomArgs(serenaTool)

	// Determine the mode
	mode := "docker" // default
	if toolMap, ok := serenaTool.(map[string]any); ok {
		if modeStr, ok := toolMap["mode"].(string); ok {
			mode = modeStr
		}
	}

	yaml.WriteString("          \n")
	yaml.WriteString("          [mcp_servers.serena]\n")

	if mode == "local" {
		// Local mode: use HTTP transport
		yaml.WriteString("          type = \"http\"\n")
		yaml.WriteString("          url = \"http://localhost:$GH_AW_SERENA_PORT\"\n")
	} else {
		// Docker mode: use stdio transport (default)
		// Select the appropriate Serena container based on requested languages
		containerImage := selectSerenaContainer(serenaTool)
		yaml.WriteString("          container = \"" + containerImage + ":latest\"\n")

		// Docker runtime args (--network host for network access)
		yaml.WriteString("          args = [\n")
		yaml.WriteString("            \"--network\",\n")
		yaml.WriteString("            \"host\",\n")
		yaml.WriteString("          ]\n")

		// Serena entrypoint
		yaml.WriteString("          entrypoint = \"serena\"\n")

		// Entrypoint args for Serena MCP server
		yaml.WriteString("          entrypointArgs = [\n")
		yaml.WriteString("            \"start-mcp-server\",\n")
		yaml.WriteString("            \"--context\",\n")
		yaml.WriteString("            \"codex\",\n")
		yaml.WriteString("            \"--project\",\n")
		yaml.WriteString("            \"${{ github.workspace }}\"")

		// Append custom args if present
		for _, arg := range customArgs {
			yaml.WriteString(",\n")
			fmt.Fprintf(yaml, "            \"%s\"", arg)
		}

		yaml.WriteString("\n")
		yaml.WriteString("          ]\n")

		// Add volume mount for workspace access
		yaml.WriteString("          mounts = [\"${{ github.workspace }}:${{ github.workspace }}:rw\"]\n")
	}
}

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
// Now uses HTTP transport instead of stdio, similar to safe-inputs
func (r *MCPConfigRendererUnified) renderSafeOutputsTOML(yaml *strings.Builder, workflowData *WorkflowData) {
	// Determine host based on whether agent is disabled
	host := "host.docker.internal"
	if workflowData != nil && workflowData.SandboxConfig != nil && workflowData.SandboxConfig.Agent != nil && workflowData.SandboxConfig.Agent.Disabled {
		// When agent is disabled (no firewall), use localhost instead of host.docker.internal
		host = "localhost"
	}

	yaml.WriteString("          \n")
	yaml.WriteString("          [mcp_servers." + constants.SafeOutputsMCPServerID + "]\n")
	yaml.WriteString("          type = \"http\"\n")
	yaml.WriteString("          url = \"http://" + host + ":$GH_AW_SAFE_OUTPUTS_PORT\"\n")
	yaml.WriteString("          \n")
	yaml.WriteString("          [mcp_servers." + constants.SafeOutputsMCPServerID + ".headers]\n")
	yaml.WriteString("          Authorization = \"$GH_AW_SAFE_OUTPUTS_API_KEY\"\n")
}

// RenderSafeInputsMCP generates the Safe Inputs MCP server configuration
func (r *MCPConfigRendererUnified) RenderSafeInputsMCP(yaml *strings.Builder, safeInputs *SafeInputsConfig, workflowData *WorkflowData) {
	mcpRendererLog.Printf("Rendering Safe Inputs MCP: format=%s", r.options.Format)

	if r.options.Format == "toml" {
		r.renderSafeInputsTOML(yaml, safeInputs, workflowData)
		return
	}

	// JSON format
	renderSafeInputsMCPConfigWithOptions(yaml, safeInputs, r.options.IsLast, r.options.IncludeCopilotFields, workflowData)
}

// renderSafeInputsTOML generates Safe Inputs MCP configuration in TOML format
// Uses HTTP transport exclusively
func (r *MCPConfigRendererUnified) renderSafeInputsTOML(yaml *strings.Builder, safeInputs *SafeInputsConfig, workflowData *WorkflowData) {
	yaml.WriteString("          \n")
	yaml.WriteString("          [mcp_servers." + constants.SafeInputsMCPServerID + "]\n")
	yaml.WriteString("          type = \"http\"\n")

	// Determine host based on whether agent is disabled
	host := "host.docker.internal"
	if workflowData != nil && workflowData.SandboxConfig != nil && workflowData.SandboxConfig.Agent != nil && workflowData.SandboxConfig.Agent.Disabled {
		// When agent is disabled (no firewall), use localhost instead of host.docker.internal
		host = "localhost"
		mcpRendererLog.Print("Using localhost for safe-inputs (agent disabled)")
	} else {
		mcpRendererLog.Print("Using host.docker.internal for safe-inputs (agent enabled)")
	}

	yaml.WriteString("          url = \"http://" + host + ":$GH_AW_SAFE_INPUTS_PORT\"\n")
	yaml.WriteString("          headers = { Authorization = \"$GH_AW_SAFE_INPUTS_API_KEY\" }\n")
	// Note: env_vars is not supported for HTTP transport in MCP configuration
	// Environment variables are passed via the workflow job's env: section instead
}

// RenderAgenticWorkflowsMCP generates the Agentic Workflows MCP server configuration
func (r *MCPConfigRendererUnified) RenderAgenticWorkflowsMCP(yaml *strings.Builder) {
	mcpRendererLog.Printf("Rendering Agentic Workflows MCP: format=%s, action_mode=%s", r.options.Format, r.options.ActionMode)

	if r.options.Format == "toml" {
		r.renderAgenticWorkflowsTOML(yaml)
		return
	}

	// JSON format
	renderAgenticWorkflowsMCPConfigWithOptions(yaml, r.options.IsLast, r.options.IncludeCopilotFields, r.options.ActionMode)
}

// renderAgenticWorkflowsTOML generates Agentic Workflows MCP configuration in TOML format
// Per MCP Gateway Specification v1.0.0 section 3.2.1, stdio-based MCP servers MUST be containerized.
func (r *MCPConfigRendererUnified) renderAgenticWorkflowsTOML(yaml *strings.Builder) {
	yaml.WriteString("          \n")
	yaml.WriteString("          [mcp_servers." + constants.AgenticWorkflowsMCPServerID + "]\n")

	containerImage := constants.DefaultAlpineImage
	var entrypoint string
	var entrypointArgs []string
	var mounts []string

	if r.options.ActionMode.IsDev() {
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

	yaml.WriteString("          env_vars = [\"DEBUG\", \"GH_TOKEN\", \"GITHUB_TOKEN\"]\n")
}

// renderGitHubTOML generates GitHub MCP configuration in TOML format (for Codex engine)
func (r *MCPConfigRendererUnified) renderGitHubTOML(yaml *strings.Builder, githubTool any, workflowData *WorkflowData) {
	githubType := getGitHubType(githubTool)
	readOnly := getGitHubReadOnly(githubTool)
	lockdown := getGitHubLockdown(githubTool)
	toolsets := getGitHubToolsets(githubTool)

	yaml.WriteString("          \n")
	yaml.WriteString("          [mcp_servers.github]\n")

	// Add user_agent field defaulting to workflow identifier
	userAgent := "github-agentic-workflow"
	if workflowData != nil {
		// Check if user_agent is configured in engine config first
		if workflowData.EngineConfig != nil && workflowData.EngineConfig.UserAgent != "" {
			userAgent = workflowData.EngineConfig.UserAgent
		} else if workflowData.Name != "" {
			// Fall back to sanitizing workflow name to identifier
			userAgent = SanitizeIdentifier(workflowData.Name)
		}
	}
	yaml.WriteString("          user_agent = \"" + userAgent + "\"\n")

	// Use tools.startup-timeout if specified, otherwise default to DefaultMCPStartupTimeout
	startupTimeout := int(constants.DefaultMCPStartupTimeout / time.Second)
	if workflowData != nil && workflowData.ToolsStartupTimeout > 0 {
		startupTimeout = workflowData.ToolsStartupTimeout
	}
	fmt.Fprintf(yaml, "          startup_timeout_sec = %d\n", startupTimeout)

	// Use tools.timeout if specified, otherwise default to DefaultToolTimeout
	toolTimeout := int(constants.DefaultToolTimeout / time.Second)
	if workflowData != nil && workflowData.ToolsTimeout > 0 {
		toolTimeout = workflowData.ToolsTimeout
	}
	fmt.Fprintf(yaml, "          tool_timeout_sec = %d\n", toolTimeout)

	// Check if remote mode is enabled
	if githubType == "remote" {
		// Remote mode - use hosted GitHub MCP server with streamable HTTP
		// Use readonly endpoint if read-only mode is enabled
		if readOnly {
			yaml.WriteString("          url = \"https://api.githubcopilot.com/mcp-readonly/\"\n")
		} else {
			yaml.WriteString("          url = \"https://api.githubcopilot.com/mcp/\"\n")
		}

		// Use bearer_token_env_var for authentication
		yaml.WriteString("          bearer_token_env_var = \"GH_AW_GITHUB_TOKEN\"\n")
	} else {
		// Local mode - use Docker-based GitHub MCP server with MCP Gateway spec format
		githubDockerImageVersion := getGitHubDockerImageVersion(githubTool)
		customArgs := getGitHubCustomArgs(githubTool)
		mounts := getGitHubMounts(githubTool)

		// MCP Gateway spec fields for containerized stdio servers
		yaml.WriteString("          container = \"ghcr.io/github/github-mcp-server:" + githubDockerImageVersion + "\"\n")

		// Append custom args if present (these are Docker runtime args, go before container image)
		if len(customArgs) > 0 {
			yaml.WriteString("          args = [\n")
			for _, arg := range customArgs {
				yaml.WriteString("            \"" + arg + "\",\n")
			}
			yaml.WriteString("          ]\n")
		}

		// Add volume mounts if present
		if len(mounts) > 0 {
			yaml.WriteString("          mounts = [")
			for i, mount := range mounts {
				if i > 0 {
					yaml.WriteString(", ")
				}
				yaml.WriteString("\"" + mount + "\"")
			}
			yaml.WriteString("]\n")
		}

		// Build environment variables
		envVars := make(map[string]string)
		envVars["GITHUB_PERSONAL_ACCESS_TOKEN"] = "$GH_AW_GITHUB_TOKEN"

		if readOnly {
			envVars["GITHUB_READ_ONLY"] = "1"
		}

		if lockdown {
			envVars["GITHUB_LOCKDOWN_MODE"] = "1"
		}

		envVars["GITHUB_TOOLSETS"] = toolsets

		// Write environment variables in sorted order for deterministic output
		envKeys := make([]string, 0, len(envVars))
		for key := range envVars {
			envKeys = append(envKeys, key)
		}
		sort.Strings(envKeys)

		yaml.WriteString("          env = { ")
		for i, key := range envKeys {
			if i > 0 {
				yaml.WriteString(", ")
			}
			fmt.Fprintf(yaml, "\"%s\" = \"%s\"", key, envVars[key])
		}
		yaml.WriteString(" }\n")

		// Use env_vars array to reference environment variables
		yaml.WriteString("          env_vars = [")
		for i, key := range envKeys {
			if i > 0 {
				yaml.WriteString(", ")
			}
			fmt.Fprintf(yaml, "\"%s\"", key)
		}
		yaml.WriteString("]\n")
	}
}

// RenderCustomMCPToolConfigHandler is a function type for rendering custom MCP tool configurations
type RenderCustomMCPToolConfigHandler func(yaml *strings.Builder, toolName string, toolConfig map[string]any, isLast bool) error

// HandleCustomMCPToolInSwitch processes custom MCP tools in the default case of a switch statement.
// This shared function extracts the common pattern used across all workflow engines.
//
// Parameters:
//   - yaml: The string builder for YAML output
//   - toolName: The name of the tool being processed
//   - tools: The tools map containing tool configurations (supports both expanded and non-expanded tools)
//   - isLast: Whether this is the last tool in the list
//   - renderFunc: Engine-specific function to render the MCP configuration
//
// Returns:
//   - bool: true if a custom MCP tool was handled, false otherwise
func HandleCustomMCPToolInSwitch(
	yaml *strings.Builder,
	toolName string,
	tools map[string]any,
	isLast bool,
	renderFunc RenderCustomMCPToolConfigHandler,
) bool {
	// Handle custom MCP tools (those with MCP-compatible type)
	if toolConfig, ok := tools[toolName].(map[string]any); ok {
		if hasMcp, _ := hasMCPConfig(toolConfig); hasMcp {
			if err := renderFunc(yaml, toolName, toolConfig, isLast); err != nil {
				fmt.Fprintf(os.Stderr, "Error generating custom MCP configuration for %s: %v\n", toolName, err)
			}
			return true
		}
	}
	return false
}

// MCPToolRenderers holds engine-specific rendering functions for each MCP tool type
type MCPToolRenderers struct {
	RenderGitHub           func(yaml *strings.Builder, githubTool any, isLast bool, workflowData *WorkflowData)
	RenderPlaywright       func(yaml *strings.Builder, playwrightTool any, isLast bool)
	RenderSerena           func(yaml *strings.Builder, serenaTool any, isLast bool)
	RenderCacheMemory      func(yaml *strings.Builder, isLast bool, workflowData *WorkflowData)
	RenderAgenticWorkflows func(yaml *strings.Builder, isLast bool)
	RenderSafeOutputs      func(yaml *strings.Builder, isLast bool, workflowData *WorkflowData)
	RenderSafeInputs       func(yaml *strings.Builder, safeInputs *SafeInputsConfig, isLast bool)
	RenderWebFetch         func(yaml *strings.Builder, isLast bool)
	RenderCustomMCPConfig  RenderCustomMCPToolConfigHandler
}

// JSONMCPConfigOptions defines configuration for JSON-based MCP config rendering
type JSONMCPConfigOptions struct {
	// ConfigPath is the file path for the MCP config (e.g., "/tmp/gh-aw/mcp-config/mcp-servers.json")
	ConfigPath string
	// Renderers contains engine-specific rendering functions for each tool
	Renderers MCPToolRenderers
	// FilterTool is an optional function to filter out tools before processing
	// Returns true if the tool should be included, false to skip it
	FilterTool func(toolName string) bool
	// PostEOFCommands is an optional function to add commands after the EOF (e.g., debug output)
	PostEOFCommands func(yaml *strings.Builder)
	// GatewayConfig is an optional gateway configuration to include in the MCP config
	// When set, adds a "gateway" section with port and apiKey for awmg to use
	GatewayConfig *MCPGatewayRuntimeConfig
}

// GitHubMCPDockerOptions defines configuration for GitHub MCP Docker rendering
type GitHubMCPDockerOptions struct {
	// ReadOnly enables read-only mode for GitHub API operations
	ReadOnly bool
	// Lockdown enables lockdown mode for GitHub MCP server (limits content from public repos)
	Lockdown bool
	// LockdownFromStep indicates if lockdown value should be read from step output
	LockdownFromStep bool
	// Toolsets specifies the GitHub toolsets to enable
	Toolsets string
	// DockerImageVersion specifies the GitHub MCP server Docker image version
	DockerImageVersion string
	// CustomArgs are additional arguments to append to the Docker command
	CustomArgs []string
	// IncludeTypeField indicates whether to include the "type": "stdio" field (Copilot needs it, Claude doesn't)
	IncludeTypeField bool
	// AllowedTools specifies the list of allowed tools (Copilot uses this, Claude doesn't)
	AllowedTools []string
	// EffectiveToken is the GitHub token to use (Claude uses this, Copilot uses env passthrough)
	EffectiveToken string
	// Mounts specifies volume mounts for the GitHub MCP server container (format: "host:container:mode")
	Mounts []string
}

// RenderGitHubMCPDockerConfig renders the GitHub MCP server configuration for Docker (local mode).
// Per MCP Gateway Specification v1.0.0 section 3.2.1, stdio-based MCP servers MUST be containerized.
// Uses MCP Gateway spec format: container, entrypointArgs, and env fields.
//
// Parameters:
//   - yaml: The string builder for YAML output
//   - options: GitHub MCP Docker rendering options
func RenderGitHubMCPDockerConfig(yaml *strings.Builder, options GitHubMCPDockerOptions) {
	// Add type field if needed (Copilot requires this, Claude doesn't)
	// Per MCP Gateway Specification v1.0.0 section 4.1.2, use "stdio" for containerized servers
	if options.IncludeTypeField {
		yaml.WriteString("                \"type\": \"stdio\",\n")
	}

	// MCP Gateway spec fields for containerized stdio servers
	yaml.WriteString("                \"container\": \"ghcr.io/github/github-mcp-server:" + options.DockerImageVersion + "\",\n")

	// Append custom args if present (these are Docker runtime args, go before container image)
	if len(options.CustomArgs) > 0 {
		yaml.WriteString("                \"args\": [\n")
		for _, arg := range options.CustomArgs {
			yaml.WriteString("                  \"" + arg + "\",\n")
		}
		yaml.WriteString("                ],\n")
	}

	// Add volume mounts if present
	if len(options.Mounts) > 0 {
		yaml.WriteString("                \"mounts\": [\n")
		for i, mount := range options.Mounts {
			yaml.WriteString("                  \"" + mount + "\"")
			if i < len(options.Mounts)-1 {
				yaml.WriteString(",")
			}
			yaml.WriteString("\n")
		}
		yaml.WriteString("                ],\n")
	}

	// Note: tools field is NOT included here - the converter script adds it back
	// for Copilot (see convert_gateway_config_copilot.sh). This keeps the gateway
	// config compatible with the schema which doesn't have the tools field.

	// Add env section for GitHub MCP server environment variables
	yaml.WriteString("                \"env\": {\n")

	// Build environment variables map
	envVars := make(map[string]string)

	// GitHub token (always required)
	if options.IncludeTypeField {
		// Copilot engine: use escaped variable for Copilot CLI to interpolate
		envVars["GITHUB_PERSONAL_ACCESS_TOKEN"] = "\\${GITHUB_MCP_SERVER_TOKEN}"
	} else {
		// Non-Copilot engines (Claude/Custom): use plain shell variable
		envVars["GITHUB_PERSONAL_ACCESS_TOKEN"] = "$GITHUB_MCP_SERVER_TOKEN"
	}

	// Read-only mode
	if options.ReadOnly {
		envVars["GITHUB_READ_ONLY"] = "1"
	}

	// Lockdown mode
	if options.LockdownFromStep {
		// Security: Use environment variable instead of template expression to prevent template injection
		// The GITHUB_MCP_LOCKDOWN env var is set in Start MCP gateway step from step output
		// Value is already converted to "1" or "0" in the environment variable
		envVars["GITHUB_LOCKDOWN_MODE"] = "$GITHUB_MCP_LOCKDOWN"
	} else if options.Lockdown {
		// Use explicit lockdown value from configuration
		envVars["GITHUB_LOCKDOWN_MODE"] = "1"
	}

	// Toolsets (always configured, defaults to "default")
	envVars["GITHUB_TOOLSETS"] = options.Toolsets

	// Write environment variables in sorted order for deterministic output
	envKeys := make([]string, 0, len(envVars))
	for key := range envVars {
		envKeys = append(envKeys, key)
	}
	sort.Strings(envKeys)

	for i, key := range envKeys {
		isLast := i == len(envKeys)-1
		comma := ""
		if !isLast {
			comma = ","
		}
		fmt.Fprintf(yaml, "                  \"%s\": \"%s\"%s\n", key, envVars[key], comma)
	}

	yaml.WriteString("                }\n")
}

// GitHubMCPRemoteOptions defines configuration for GitHub MCP remote mode rendering
type GitHubMCPRemoteOptions struct {
	// ReadOnly enables read-only mode for GitHub API operations
	ReadOnly bool
	// Lockdown enables lockdown mode for GitHub MCP server (limits content from public repos)
	Lockdown bool
	// LockdownFromStep indicates if lockdown value should be read from step output
	LockdownFromStep bool
	// Toolsets specifies the GitHub toolsets to enable
	Toolsets string
	// AuthorizationValue is the value for the Authorization header
	// For Claude: "Bearer {effectiveToken}"
	// For Copilot: "Bearer \\${GITHUB_PERSONAL_ACCESS_TOKEN}"
	AuthorizationValue string
	// IncludeToolsField indicates whether to include the "tools" field (Copilot needs it, Claude doesn't)
	IncludeToolsField bool
	// AllowedTools specifies the list of allowed tools (Copilot uses this, Claude doesn't)
	AllowedTools []string
	// IncludeEnvSection indicates whether to include the env section (Copilot needs it, Claude doesn't)
	IncludeEnvSection bool
}

// RenderGitHubMCPRemoteConfig renders the GitHub MCP server configuration for remote (hosted) mode.
// This shared function extracts the duplicate pattern from Claude and Copilot engines.
//
// Parameters:
//   - yaml: The string builder for YAML output
//   - options: GitHub MCP remote rendering options
func RenderGitHubMCPRemoteConfig(yaml *strings.Builder, options GitHubMCPRemoteOptions) {
	// Remote mode - use hosted GitHub MCP server
	yaml.WriteString("                \"type\": \"http\",\n")
	yaml.WriteString("                \"url\": \"https://api.githubcopilot.com/mcp/\",\n")
	yaml.WriteString("                \"headers\": {\n")

	// Collect headers in a map
	headers := make(map[string]string)
	headers["Authorization"] = options.AuthorizationValue

	// Add X-MCP-Readonly header if read-only mode is enabled
	if options.ReadOnly {
		headers["X-MCP-Readonly"] = "true"
	}

	// Add X-MCP-Lockdown header if lockdown mode is enabled
	if options.LockdownFromStep {
		// Security: Use environment variable instead of template expression to prevent template injection
		// The GITHUB_MCP_LOCKDOWN env var contains "1" or "0", convert to "true" or "false" for header
		headers["X-MCP-Lockdown"] = "$([ \"$GITHUB_MCP_LOCKDOWN\" = \"1\" ] && echo true || echo false)"
	} else if options.Lockdown {
		// Use explicit lockdown value from configuration
		headers["X-MCP-Lockdown"] = "true"
	}

	// Add X-MCP-Toolsets header if toolsets are configured
	if options.Toolsets != "" {
		headers["X-MCP-Toolsets"] = options.Toolsets
	}

	// Write headers using helper
	writeHeadersToYAML(yaml, headers, "                  ")

	// Close headers section
	if options.IncludeToolsField || options.IncludeEnvSection {
		yaml.WriteString("                },\n")
	} else {
		yaml.WriteString("                }\n")
	}

	// Add tools field if requested (Copilot needs it, Claude doesn't)
	// Note: This is added here when IncludeToolsField is true, but in some cases
	// the converter script also adds it back (see convert_gateway_config_copilot.sh).
	if options.IncludeToolsField && len(options.AllowedTools) > 0 {
		yaml.WriteString("                \"tools\": [\n")
		for i, tool := range options.AllowedTools {
			yaml.WriteString("                  \"")
			yaml.WriteString(tool)
			yaml.WriteString("\"")
			if i < len(options.AllowedTools)-1 {
				yaml.WriteString(",")
			}
			yaml.WriteString("\n")
		}
		if options.IncludeEnvSection {
			yaml.WriteString("                ],\n")
		} else {
			yaml.WriteString("                ]\n")
		}
	}

	// Add env section if needed (Copilot uses this, Claude doesn't)
	if options.IncludeEnvSection {
		yaml.WriteString("                \"env\": {\n")
		yaml.WriteString("                  \"GITHUB_PERSONAL_ACCESS_TOKEN\": \"\\${GITHUB_MCP_SERVER_TOKEN}\"\n")
		yaml.WriteString("                }\n")
	}
}

// RenderJSONMCPConfig renders MCP configuration in JSON format with the common mcpServers structure.
// This shared function extracts the duplicate pattern from Claude, Copilot, and Custom engines.
//
// Parameters:
//   - yaml: The string builder for YAML output
//   - tools: Map of tool configurations
//   - mcpTools: Ordered list of MCP tool names to render
//   - workflowData: Workflow configuration data
//   - options: JSON MCP config rendering options
func RenderJSONMCPConfig(
	yaml *strings.Builder,
	tools map[string]any,
	mcpTools []string,
	workflowData *WorkflowData,
	options JSONMCPConfigOptions,
) error {
	mcpRendererLog.Printf("Rendering JSON MCP config: %d tools", len(mcpTools))

	// Build the JSON configuration in a separate builder for validation
	var configBuilder strings.Builder
	configBuilder.WriteString("          {\n")
	configBuilder.WriteString("            \"mcpServers\": {\n")

	// Filter tools if needed (e.g., Copilot filters out cache-memory)
	var filteredTools []string
	for _, toolName := range mcpTools {
		if options.FilterTool != nil && !options.FilterTool(toolName) {
			mcpRendererLog.Printf("Filtering out MCP tool: %s", toolName)
			continue
		}
		filteredTools = append(filteredTools, toolName)
	}

	mcpRendererLog.Printf("Rendering %d MCP tools after filtering", len(filteredTools))

	// Process each MCP tool
	totalServers := len(filteredTools)
	serverCount := 0

	for _, toolName := range filteredTools {
		serverCount++
		isLast := serverCount == totalServers

		switch toolName {
		case "github":
			githubTool := tools["github"]
			options.Renderers.RenderGitHub(&configBuilder, githubTool, isLast, workflowData)
		case "playwright":
			playwrightTool := tools["playwright"]
			options.Renderers.RenderPlaywright(&configBuilder, playwrightTool, isLast)
		case "serena":
			serenaTool := tools["serena"]
			options.Renderers.RenderSerena(&configBuilder, serenaTool, isLast)
		case "cache-memory":
			options.Renderers.RenderCacheMemory(&configBuilder, isLast, workflowData)
		case "agentic-workflows":
			options.Renderers.RenderAgenticWorkflows(&configBuilder, isLast)
		case "safe-outputs":
			options.Renderers.RenderSafeOutputs(&configBuilder, isLast, workflowData)
		case "safe-inputs":
			if options.Renderers.RenderSafeInputs != nil {
				options.Renderers.RenderSafeInputs(&configBuilder, workflowData.SafeInputs, isLast)
			}
		case "web-fetch":
			options.Renderers.RenderWebFetch(&configBuilder, isLast)
		default:
			// Handle custom MCP tools using shared helper
			HandleCustomMCPToolInSwitch(&configBuilder, toolName, tools, isLast, options.Renderers.RenderCustomMCPConfig)
		}
	}

	// Write config file footer - but don't add newline yet if we need to add gateway
	if options.GatewayConfig != nil {
		configBuilder.WriteString("            },\n")
		// Add gateway section (needed for gateway to process)
		// Per MCP Gateway Specification v1.0.0 section 4.2, use "${VARIABLE_NAME}" syntax for variable expressions
		configBuilder.WriteString("            \"gateway\": {\n")
		// Port as unquoted variable - shell expands to integer (e.g., 8080) for valid JSON
		fmt.Fprintf(&configBuilder, "              \"port\": $MCP_GATEWAY_PORT,\n")
		fmt.Fprintf(&configBuilder, "              \"domain\": \"%s\",\n", options.GatewayConfig.Domain)
		fmt.Fprintf(&configBuilder, "              \"apiKey\": \"%s\"", options.GatewayConfig.APIKey)
		// Add payloadDir if specified
		if options.GatewayConfig.PayloadDir != "" {
			fmt.Fprintf(&configBuilder, ",\n              \"payloadDir\": \"%s\"\n", options.GatewayConfig.PayloadDir)
		} else {
			configBuilder.WriteString("\n")
		}
		configBuilder.WriteString("            }\n")
	} else {
		configBuilder.WriteString("            }\n")
	}

	configBuilder.WriteString("          }\n")

	// Get the generated configuration
	generatedConfig := configBuilder.String()

	// Write the configuration to the YAML output
	yaml.WriteString("          cat << MCPCONFIG_EOF | bash /opt/gh-aw/actions/start_mcp_gateway.sh\n")
	yaml.WriteString(generatedConfig)
	yaml.WriteString("          MCPCONFIG_EOF\n")

	// Note: Post-EOF commands are no longer needed since we pipe directly to the gateway script
	return nil
}
