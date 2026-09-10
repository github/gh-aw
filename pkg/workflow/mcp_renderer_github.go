package workflow

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/sliceutil"
)

// RenderGitHubMCP generates the GitHub MCP server configuration
// Supports both local (Docker) and remote (hosted) modes
func (r *MCPConfigRendererUnified) RenderGitHubMCP(yaml *strings.Builder, githubTool map[string]any, workflowData *WorkflowData) { //nolint:largefunc // Existing renderer preserves emitted YAML field ordering.
	githubType := getGitHubType(githubTool)
	readOnly := getGitHubReadOnly()

	// Get explicit lockdown value (only used when lockdown is explicitly configured)
	lockdown := getGitHubLockdown(githubTool)

	// Guard policies from step: automatically applied for public repositories when no explicit
	// guard policy is configured. GitHub App token scope is authentication, not a substitute
	// for the DIFC source labels enforced by the MCP gateway.
	// The determine-automatic-lockdown step outputs min_integrity and repos for public repos.
	explicitGuardPolicies := getGitHubGuardPolicies(githubTool)
	if len(explicitGuardPolicies) == 0 && githubBackendIsDynamicDelegationOnly(workflowData) {
		explicitGuardPolicies = dynamicEnclaveGitHubGuardPolicies(workflowData)
	}
	if githubBackendIsStaticEnclaveDelegationOnly(workflowData) {
		explicitGuardPolicies = staticEnclaveGitHubGuardPolicies(workflowData)
	}
	// Integrity reaction fields are only supported in proxy mode (DIFC/CLI proxy),
	// not in gateway mode. The MCP gateway cannot identify reaction authors because
	// the GitHub MCP server protocol does not expose that information. Warn if the
	// user configured reactions with the gateway path.
	if isFeatureEnabled(constants.IntegrityReactionsFeatureFlag, workflowData) {
		if hasReactionFieldsInToolConfig(githubTool) {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(
				"integrity-reactions: endorsement/disapproval reactions are ignored in MCP gateway mode because "+
					"reaction authors cannot be identified from the GitHub MCP server. Reactions are only enforced "+
					"in proxy mode (DIFC proxy / CLI proxy)."))
		}
	}
	shouldUseStepOutputForGuardPolicy := githubGuardPoliciesFromStep(workflowData, explicitGuardPolicies)
	emitRequiredFalse := githubBackendIsDynamicDelegationOnly(workflowData)

	toolsets := getGitHubToolsets(githubTool)
	features := getGitHubFeatures(githubTool)

	mcpRendererLog.Printf("Rendering GitHub MCP: type=%s, read_only=%t, lockdown=%t (explicit=%t), guard_from_step=%t, toolsets=%v, features=%s, format=%s",
		githubType, readOnly, lockdown, hasGitHubLockdownExplicitlySet(githubTool), shouldUseStepOutputForGuardPolicy, toolsets, features, r.options.Format)

	if r.options.Format == "toml" {
		mcpRendererLog.Print("GitHub MCP format=toml, dispatching to renderGitHubTOML")
		r.renderGitHubTOML(yaml, githubTool, workflowData)
		return
	}

	yaml.WriteString("              \"github\": {\n")

	// Check if remote mode is enabled (type: remote)
	if githubType == GitHubMCPModeRemote {
		mcpRendererLog.Printf("GitHub MCP remote mode selected: copilot_fields=%t", r.options.IncludeCopilotFields)
		// Determine authorization value based on engine requirements
		// Copilot uses MCP passthrough syntax: "Bearer \${GITHUB_PERSONAL_ACCESS_TOKEN}"
		// Other engines use shell variable: "Bearer $GITHUB_MCP_SERVER_TOKEN"
		authValue := "Bearer $GITHUB_MCP_SERVER_TOKEN"
		if r.options.IncludeCopilotFields {
			authValue = "Bearer \\${GITHUB_PERSONAL_ACCESS_TOKEN}"
		}

		RenderGitHubMCPRemoteConfig(yaml, GitHubMCPRemoteOptions{
			ReadOnly:              readOnly,
			Lockdown:              lockdown,
			LockdownFromStep:      false,
			GuardPoliciesFromStep: shouldUseStepOutputForGuardPolicy,
			Toolsets:              toolsets,
			Features:              features,
			AuthorizationValue:    authValue,
			IncludeToolsField:     r.options.IncludeCopilotFields,
			AllowedTools:          getGitHubAllowedTools(githubTool),
			IncludeEnvSection:     r.options.IncludeCopilotFields,
			GuardPolicies:         explicitGuardPolicies,
			EmitRequiredFalse:     emitRequiredFalse,
		})
	} else {
		// Local mode - use Docker-based GitHub MCP server (default)
		githubDockerImageVersion := getGitHubDockerImageVersion(githubTool)
		customArgs := getGitHubCustomArgs(githubTool)

		mcpRendererLog.Printf("GitHub MCP local docker mode: image_version=%s, custom_args=%d", githubDockerImageVersion, len(customArgs))

		RenderGitHubMCPDockerConfig(yaml, GitHubMCPDockerOptions{
			ReadOnly:              readOnly,
			Lockdown:              lockdown,
			LockdownFromStep:      false,
			GuardPoliciesFromStep: shouldUseStepOutputForGuardPolicy,
			Toolsets:              toolsets,
			Features:              features,
			DockerImageVersion:    githubDockerImageVersion,
			CustomArgs:            customArgs,
			IncludeTypeField:      r.options.IncludeCopilotFields,
			AllowedTools:          getGitHubAllowedTools(githubTool),
			EffectiveToken:        "", // Token passed via env
			GuardPolicies:         explicitGuardPolicies,
			EmitRequiredFalse:     emitRequiredFalse,
			ContainerPinMappings:  r.options.ContainerPinMappings,
		})
	}

	if r.options.IsLast {
		yaml.WriteString("              }\n")
	} else {
		yaml.WriteString("              },\n")
	}
}

// renderGitHubTOML generates GitHub MCP configuration in TOML format (for Codex engine)
func (r *MCPConfigRendererUnified) renderGitHubTOML(yaml *strings.Builder, githubTool map[string]any, workflowData *WorkflowData) { //nolint:largefunc // Existing renderer preserves emitted TOML field ordering.
	githubType := getGitHubType(githubTool)
	readOnly := getGitHubReadOnly()
	lockdown := getGitHubLockdown(githubTool)
	toolsets := getGitHubToolsets(githubTool)
	features := getGitHubFeatures(githubTool)

	mcpRendererLog.Printf("Rendering GitHub MCP TOML: type=%s, read_only=%t, lockdown=%t, toolsets=%s, features=%s", githubType, readOnly, lockdown, toolsets, features)

	yaml.WriteString("          \n")
	yaml.WriteString("          [mcp_servers.github]\n")

	// Add user_agent field defaulting to workflow identifier
	userAgent := "github-agentic-workflow"
	if workflowData != nil {
		// Check if user_agent is configured in engine config first
		if workflowData.EngineConfig != nil && workflowData.EngineConfig.UserAgent != "" {
			userAgent = workflowData.EngineConfig.UserAgent
		} else if workflowData.Name != "" {
			// Fall back to sanitizing the workflow name as an artifact/user-agent identifier
			userAgent = SanitizeArtifactIdentifier(workflowData.Name)
		}
	}
	yaml.WriteString("          user_agent = \"" + userAgent + "\"\n")

	// Use tools.startup-timeout if specified, otherwise default to DefaultMCPStartupTimeout
	// For GitHub Actions expressions, fall back to default (TOML format doesn't support expressions)
	startupTimeout := int(constants.DefaultMCPStartupTimeout / time.Second)
	if workflowData != nil && workflowData.ToolsStartupTimeout != "" {
		if n := templatableIntValue(&workflowData.ToolsStartupTimeout); n > 0 {
			startupTimeout = n
		}
	}
	fmt.Fprintf(yaml, "          startup_timeout_sec = %d\n", startupTimeout)

	// Use tools.timeout if specified, otherwise default to DefaultToolTimeout
	// For GitHub Actions expressions, fall back to default (TOML format doesn't support expressions)
	toolTimeout := int(constants.DefaultToolTimeout / time.Second)
	if workflowData != nil && workflowData.ToolsTimeout != "" {
		if n := templatableIntValue(&workflowData.ToolsTimeout); n > 0 {
			toolTimeout = n
		}
	}
	fmt.Fprintf(yaml, "          tool_timeout_sec = %d\n", toolTimeout)

	// Check if remote mode is enabled
	if githubType == GitHubMCPModeRemote {
		mcpRendererLog.Printf("GitHub MCP TOML remote mode: readonly_endpoint=%t", readOnly)
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

		// MCP Gateway spec fields for containerized stdio servers.
		// Apply container_pins mapping so private-cloud runners use the configured mirror.
		githubMCPImage := resolveGatewayContainerFromMappings("ghcr.io/github/github-mcp-server:"+githubDockerImageVersion, workflowData.getContainerPinMappings())
		yaml.WriteString("          container = \"" + githubMCPImage + "\"\n")

		// Append custom args if present (these are Docker runtime args, go before container image)
		if len(customArgs) > 0 {
			yaml.WriteString("          args = [\n")
			for _, arg := range customArgs {
				yaml.WriteString("            " + strconv.Quote(arg) + ",\n")
			}
			yaml.WriteString("          ]\n")
		}

		// Build environment variables
		envVars := buildGitHubMCPEnvVars(
			"$GH_AW_GITHUB_TOKEN",
			"$GITHUB_SERVER_URL",
			readOnly,
			lockdown,
			toolsets,
			features,
		)

		// Write environment variables in sorted order for deterministic output
		envKeys := sliceutil.SortedKeys(envVars)

		writeTOMLInlineStringMapSection(yaml, "          ", "env", envVars)

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

// RenderGitHubMCPDockerConfig renders the GitHub MCP server configuration for Docker (local mode).
// Per MCP Gateway Specification v1.0.0 section 3.2.1, stdio-based MCP servers MUST be containerized.
// Uses MCP Gateway spec format: container, entrypointArgs, and env fields.
//
// Parameters:
//   - yaml: The string builder for YAML output
//   - options: GitHub MCP Docker rendering options
func RenderGitHubMCPDockerConfig(yaml *strings.Builder, options GitHubMCPDockerOptions) {
	mcpRendererLog.Printf("Rendering GitHub MCP Docker config: image=%s, read_only=%t, lockdown=%t", options.DockerImageVersion, options.ReadOnly, options.Lockdown)

	// Add type field if needed (Copilot requires this, Claude doesn't)
	// Per MCP Gateway Specification v1.0.0 section 4.1.2, use "stdio" for containerized servers
	if options.IncludeTypeField {
		yaml.WriteString("                \"type\": \"stdio\",\n")
	}

	// MCP Gateway spec fields for containerized stdio servers.
	// Apply container_pins mapping so private-cloud runners use the configured mirror.
	githubMCPImage := resolveGatewayContainerFromMappings("ghcr.io/github/github-mcp-server:"+options.DockerImageVersion, options.ContainerPinMappings)
	yaml.WriteString("                \"container\": \"" + githubMCPImage + "\",\n")

	// Append custom args if present (these are Docker runtime args, go before container image)
	if len(options.CustomArgs) > 0 {
		yaml.WriteString("                \"args\": [\n")
		for _, arg := range options.CustomArgs {
			quotedArg, _ := json.Marshal(arg) //nolint:jsonmarshalignoredeerror // marshaling a string cannot fail
			yaml.WriteString("                  " + string(quotedArg) + ",\n")
		}
		yaml.WriteString("                ],\n")
	}

	// Note: tools field is NOT included here - the converter script adds it back
	// for Copilot (see convert_gateway_config_copilot.cjs). This keeps the gateway
	// config compatible with the schema which doesn't have the tools field.

	// GitHub token (always required)
	tokenValue := "$GITHUB_MCP_SERVER_TOKEN"
	hostValue := "$GITHUB_SERVER_URL"
	if options.IncludeTypeField {
		// Copilot engine: keep shell expansion so gateway input remains valid JSON.
		tokenValue = "${GITHUB_MCP_SERVER_TOKEN}"
		// GitHub host for enterprise deployments (format: https://hostname, e.g. https://myorg.ghe.com).
		// GITHUB_SERVER_URL is set by GitHub Actions as a full URL (https://hostname, no trailing slash),
		// which matches the format expected by github-mcp-server for GITHUB_HOST.
		hostValue = "${GITHUB_SERVER_URL}"
	}

	envVars := buildGitHubMCPEnvVars(tokenValue, hostValue, options.ReadOnly, options.Lockdown, options.Toolsets, options.Features)
	hasGuardPolicies := hasGitHubMCPGuardPolicies(options.GuardPolicies, options.GuardPoliciesFromStep)
	writeJSONStringMapSection(yaml, "                ", "env", envVars, hasGuardPolicies || options.EmitRequiredFalse)
	renderGitHubMCPGuardPolicies(yaml, options.GuardPolicies, options.GuardPoliciesFromStep, "                ")
	if options.EmitRequiredFalse {
		appendRequiredFalseField(yaml, hasGuardPolicies, "                ")
	}
}

// RenderGitHubMCPRemoteConfig renders the GitHub MCP server configuration for remote (hosted) mode.
// This shared function extracts the duplicate pattern from Claude and Copilot engines.
//
// Parameters:
//   - yaml: The string builder for YAML output
//   - options: GitHub MCP remote rendering options
func RenderGitHubMCPRemoteConfig(yaml *strings.Builder, options GitHubMCPRemoteOptions) {
	mcpRendererLog.Printf("Rendering GitHub MCP remote config: read_only=%t, lockdown=%t, toolsets=%s", options.ReadOnly, options.Lockdown, options.Toolsets)

	// Remote mode - use hosted GitHub MCP server
	yaml.WriteString("                \"type\": \"http\",\n")
	yaml.WriteString("                \"url\": \"https://api.githubcopilot.com/mcp/\",\n")
	hasGuardPolicies := hasGitHubMCPGuardPolicies(options.GuardPolicies, options.GuardPoliciesFromStep)
	// Use writeJSONStringMapSectionRaw so that pre-escaped shell placeholders such as
	// \${GITHUB_PERSONAL_ACCESS_TOKEN} (Copilot passthrough syntax) are written with a
	// single backslash in the generated lock file.  The compiled config is embedded in an
	// unquoted bash heredoc; bash collapses \$ → $, delivering the literal ${VAR} string
	// that the MCP gateway then expands from its own environment at runtime.
	// Using writeJSONStringMapSection instead would double-escape the backslash to \\${VAR},
	// which bash expands to \<secret-value> — an invalid JSON escape character that causes
	// JSON.parse to fail on every run.
	writeJSONStringMapSectionRaw(
		yaml,
		"                ",
		"headers",
		buildGitHubMCPRemoteHeaders(options.AuthorizationValue, options.ReadOnly, options.Lockdown, options.Toolsets, options.Features),
		(options.IncludeToolsField && len(options.AllowedTools) > 0) || options.IncludeEnvSection || hasGuardPolicies || options.EmitRequiredFalse,
	)

	// Add tools field if requested (Copilot needs it, Claude doesn't)
	// Note: This is added here when IncludeToolsField is true, but in some cases
	// the converter script also adds it back (see convert_gateway_config_copilot.cjs).
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
		if options.IncludeEnvSection || hasGuardPolicies || options.EmitRequiredFalse {
			yaml.WriteString("                ],\n")
		} else {
			yaml.WriteString("                ]\n")
		}
	}

	// Add env section if needed (Copilot uses this, Claude doesn't)
	if options.IncludeEnvSection {
		writeJSONStringMapSection(
			yaml,
			"                ",
			"env",
			buildGitHubMCPEnvVars("${GITHUB_MCP_SERVER_TOKEN}", "${GITHUB_SERVER_URL}", false, false, "", ""),
			hasGuardPolicies || options.EmitRequiredFalse,
		)
	}

	// Add guard-policies if configured or from step
	renderGitHubMCPGuardPolicies(yaml, options.GuardPolicies, options.GuardPoliciesFromStep, "                ")
	if options.EmitRequiredFalse {
		appendRequiredFalseField(yaml, hasGuardPolicies, "                ")
	}
}
