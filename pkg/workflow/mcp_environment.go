// Package workflow provides environment variable management for MCP server execution.
//
// # MCP Environment Variables
//
// This file is responsible for collecting and managing all environment variables
// required by MCP servers during workflow execution. Environment variables are
// used to pass configuration, authentication tokens, and runtime settings to
// MCP servers running in the gateway.
//
// Key responsibilities:
//   - Collecting MCP-related environment variables from workflow configuration
//   - Managing GitHub MCP server tokens (custom, default, and GitHub App tokens)
//   - Handling safe-outputs and mcp-scripts environment variables
//   - Processing Playwright domain secrets
//   - Extracting secrets from HTTP MCP server headers
//   - Managing agentic-workflows GITHUB_TOKEN
//
// Environment variable categories:
//   - GitHub MCP: GITHUB_MCP_SERVER_TOKEN, GITHUB_MCP_GUARD_MIN_INTEGRITY, GITHUB_MCP_GUARD_REPOS
//   - Safe Outputs: GH_AW_SAFE_OUTPUTS_*, GH_AW_ASSETS_*
//   - MCP Scripts: GH_AW_MCP_SCRIPTS_PORT, GH_AW_MCP_SCRIPTS_API_KEY
//   - Serena: removed (use shared/mcp/serena.md instead)
//   - Playwright: Secrets from custom args expressions
//   - HTTP MCP: Custom secrets from headers and env sections
//
// Token precedence for GitHub MCP:
//  1. GitHub App token (if app configuration exists)
//  2. Custom github-token from tool configuration
//  3. Top-level github-token from frontmatter
//  4. Default GITHUB_TOKEN secret
//
// The environment variables collected here are passed to both the
// "Start MCP gateway" step and the "MCP Gateway" step to ensure
// MCP servers have access to necessary configuration and secrets.
//
// Related files:
//   - mcp_setup_generator.go: Uses collected env vars in gateway setup
//   - mcp_github_config.go: GitHub-specific token and configuration
//   - safe_outputs.go: Safe outputs configuration
//   - mcp_scripts.go: MCP Scripts configuration
//
// Example usage:
//
//	envVars := collectMCPEnvironmentVariables(tools, mcpTools, workflowData, hasAgenticWorkflows)
//	// Returns map[string]string with all required environment variables
package workflow

import (
	"fmt"
	"maps"

	"slices"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/workflow/compilerenv"
)

var mcpEnvironmentLog = logger.New("workflow:mcp_environment")

// collectMCPEnvironmentVariables collects all MCP-related environment variables
// from the workflow configuration to be passed to both Start MCP gateway and MCP Gateway steps
func collectMCPEnvironmentVariables(tools map[string]any, mcpTools []string, workflowData *WorkflowData, hasAgenticWorkflows bool) map[string]string { //nolint:largefunc // Existing MCP environment collection remains centralized.
	envVars := make(map[string]string)

	// Check for GitHub MCP server token
	hasGitHub := slices.Contains(mcpTools, "github")
	rawGitHubTool, hasGitHubInTools := tools["github"]
	githubToolEnabledInTools := hasGitHubInTools && rawGitHubTool != false
	if hasGitHub {
		toolConfig, _ := rawGitHubTool.(map[string]any)

		// Check if GitHub App is configured for token minting
		appConfigured := hasGitHubApp(toolConfig)

		// If GitHub App is configured, use the app token minted directly in the agent job.
		// The token cannot be passed via job outputs from the activation job because
		// actions/create-github-app-token calls ::add-mask:: on the token, and the
		// GitHub Actions runner silently drops masked values in job outputs (runner v2.308+).
		if appConfigured {
			mcpEnvironmentLog.Print("Using GitHub App token from agent job step for GitHub MCP server (overrides custom and default tokens)")
			tokenExpression := "${{ steps.github-mcp-app-token.outputs.token }}"
			if appMap, ok := toolConfig["github-app"].(map[string]any); ok {
				if appConfig := parseAppConfig(appMap); appConfig.shouldIgnoreMissingKey() {
					customGitHubToken := getGitHubToken(toolConfig)
					tokenExpression = combineTokenExpressions(tokenExpression, getEffectiveGitHubToken(customGitHubToken))
				}
			}
			envVars["GITHUB_MCP_SERVER_TOKEN"] = tokenExpression
		} else {
			// Otherwise, use custom token or default fallback
			customGitHubToken := getGitHubToken(toolConfig)
			effectiveToken := getEffectiveGitHubToken(customGitHubToken)
			envVars["GITHUB_MCP_SERVER_TOKEN"] = effectiveToken
		}

		// Add guard policy env vars if the determine-automatic-lockdown step will be generated
		// and its outputs are actually used to render the primary GitHub MCP server's guard
		// policy. Skip when guard policy is already explicitly set, or when the GitHub MCP
		// server exists solely to serve an enclave identity — enclave-only backends always
		// derive their guard policy from the enclave declaration, never from the step outputs
		// (see staticEnclaveGitHubGuardPolicies / dynamicEnclaveGitHubGuardPolicies), even
		// though the step may still be generated to supply GH_AW_SINK_VISIBILITY.
		// Security: Pass step outputs through environment variables to prevent template injection.
		guardPoliciesExplicit := len(getGitHubGuardPolicies(toolConfig)) > 0
		if githubToolEnabledInTools && !guardPoliciesExplicit && !githubBackendIsEnclaveOnly(workflowData) && githubLockdownDetectionStepEnabled(workflowData) {
			envVars["GITHUB_MCP_GUARD_MIN_INTEGRITY"] = "${{ steps.determine-automatic-lockdown.outputs.min_integrity }}"
			envVars["GITHUB_MCP_GUARD_REPOS"] = "${{ steps.determine-automatic-lockdown.outputs.repos }}"
		}
	}

	// Emit GH_AW_SINK_VISIBILITY for all workflows where the determine-automatic-lockdown step
	// runs (i.e., any workflow with a GitHub tool, or an enclave-only GitHub backend — static
	// or dynamic — whose write-sink policy needs the destination visibility). This avoids
	// embedding a ${{ }} expression directly in the run: heredoc, which zizmor flags as
	// template injection. The value is the raw step output (no toJSON), and the surrounding
	// JSON double-quotes in the heredoc produce a valid JSON string at runtime:
	//   "sink-visibility": "${GH_AW_SINK_VISIBILITY}"  →  "sink-visibility": "public"
	// githubBackendIsEnclaveOnly is the same shared helper used to decide whether the primary
	// agent's automatic guard-policy env vars apply, keeping both decisions in sync.
	// Gating on githubLockdownDetectionStepEnabled ensures this never references a step that
	// isn't actually generated (a dangling steps.<id>.outputs.* reference).
	if (githubToolEnabledInTools || githubBackendIsEnclaveOnly(workflowData)) && githubLockdownDetectionStepEnabled(workflowData) {
		envVars[sinkVisibilityEnvVar] = "${{ steps.determine-automatic-lockdown.outputs.visibility }}"
	}
	if enclaveDynamicRepositoryPolicyEnabled(workflowData) {
		envVars["GH_AW_TIMEOUT_MINUTES"] = resolveStepTimeoutValue(workflowData)
	}

	// Check for safe-outputs env vars
	hasSafeOutputs := slices.Contains(mcpTools, "safe-outputs")
	if hasSafeOutputs {
		envVars["GH_AW_SAFE_OUTPUTS"] = "${{ steps.set-runtime-paths.outputs.GH_AW_SAFE_OUTPUTS }}"
		// GH_AW_SAFE_OUTPUTS_CONFIG_PATH and GH_AW_SAFE_OUTPUTS_TOOLS_PATH are referenced as
		// ${VAR} placeholders in the safeoutputs MCP container env block and must be resolvable
		// by the gateway from process.env at startup time.
		envVars["GH_AW_SAFE_OUTPUTS_CONFIG_PATH"] = "${{ steps.set-runtime-paths.outputs.GH_AW_SAFE_OUTPUTS_CONFIG_PATH }}"
		envVars["GH_AW_SAFE_OUTPUTS_TOOLS_PATH"] = "${{ steps.set-runtime-paths.outputs.GH_AW_SAFE_OUTPUTS_TOOLS_PATH }}"
		// PolicyAllowCreatePullRequest is sourced from vars.* (GitHub repository/environment
		// configuration variables, not runner env) so that org admins can disable PR creation
		// without modifying compiled workflow YAML. The || 'true' fallback preserves
		// backward-compatible default behaviour when the variable is unset.
		envVars[compilerenv.PolicyAllowCreatePullRequest] = fmt.Sprintf("${{ vars.%s || 'true' }}", compilerenv.PolicyAllowCreatePullRequest)
		// GITHUB_TOKEN is needed by the safeoutputs container for GitHub API access.
		// Only set if not already provided (e.g. by hasAgenticWorkflows below).
		if _, ok := envVars["GITHUB_TOKEN"]; !ok {
			envVars["GITHUB_TOKEN"] = "${{ secrets.GITHUB_TOKEN }}"
		}
		// Only add upload-assets env vars if upload-assets is configured
		if workflowData.SafeOutputs.UploadAssets != nil {
			envVars["GH_AW_ASSETS_BRANCH"] = "${{ env.GH_AW_ASSETS_BRANCH }}"
			envVars["GH_AW_ASSETS_MAX_SIZE_KB"] = "${{ env.GH_AW_ASSETS_MAX_SIZE_KB }}"
			envVars["GH_AW_ASSETS_ALLOWED_EXTS"] = "${{ env.GH_AW_ASSETS_ALLOWED_EXTS }}"
		}
	}

	// Check for mcp-scripts env vars
	// Only add env vars if mcp-scripts is actually enabled (has tools configured)
	// This prevents referencing step outputs that don't exist when mcp-scripts isn't used
	if IsMCPScriptsEnabled(workflowData.MCPScripts) {
		// Add server configuration env vars from step outputs
		envVars["GH_AW_MCP_SCRIPTS_PORT"] = "${{ steps.mcp-scripts-start.outputs.port }}"
		envVars["GH_AW_MCP_SCRIPTS_API_KEY"] = "${{ steps.mcp-scripts-start.outputs.api_key }}"

		// Add tool-specific env vars (secrets passthrough)
		mcpScriptsSecrets := collectMCPScriptsSecrets(workflowData.MCPScripts)
		maps.Copy(envVars, mcpScriptsSecrets)
	}

	// Check for agentic-workflows GITHUB_TOKEN
	if hasAgenticWorkflows {
		envVars["GITHUB_TOKEN"] = "${{ secrets.GITHUB_TOKEN }}"
	}

	// Check for HTTP MCP servers with secrets in headers (e.g., Tavily)
	// These need to be available as environment variables when the MCP gateway starts
	for toolName, toolValue := range tools {
		// Skip standard tools that are handled above
		if toolName == "github" ||
			toolName == "cache-memory" || toolName == "agentic-workflows" ||
			toolName == "safe-outputs" || toolName == "mcp-scripts" {
			continue
		}

		// Check if this is an MCP tool
		if toolConfig, ok := toolValue.(map[string]any); ok {
			if hasMcp, _ := hasMCPConfig(toolConfig); !hasMcp {
				continue
			}

			// Get MCP config and check if it's an HTTP type
			mcpConfig, err := getMCPConfig(toolConfig, toolName)
			if err != nil {
				mcpEnvironmentLog.Printf("Failed to parse MCP config for tool %s: %v", toolName, err)
				continue
			}

			// Extract secrets from headers for HTTP MCP servers
			if mcpConfig.Type == "http" && len(mcpConfig.Headers) > 0 {
				headerSecrets := ExtractSecretsFromMap(mcpConfig.Headers)
				mcpEnvironmentLog.Printf("Extracted %d secrets from HTTP MCP server '%s'", len(headerSecrets), toolName)
				maps.Copy(envVars, headerSecrets)
			}

			// Also extract secrets and env expressions from env section if present
			if len(mcpConfig.Env) > 0 {
				envSecrets := ExtractSecretsFromMap(mcpConfig.Env)
				mcpEnvironmentLog.Printf("Extracted %d secrets from env section of MCP server '%s'", len(envSecrets), toolName)
				maps.Copy(envVars, envSecrets)

				// Also extract env var expressions in addition to secrets
				// (e.g., ${{ env.SENTRY_HOST || 'https://sentry.io' }}) so the gateway container can resolve them
				envExprs := ExtractEnvExpressionsFromMap(mcpConfig.Env)
				mcpEnvironmentLog.Printf("Extracted %d env expressions from env section of MCP server '%s'", len(envExprs), toolName)
				maps.Copy(envVars, envExprs)
			}
		}
	}

	// Codex engine needs CODEX_HOME available in the gateway setup step so that
	// the converted MCP config can be copied into the writable Codex home directory.
	// This matches the value set on the agent step in codex_engine.go.
	if workflowData != nil && workflowData.AI == string(constants.CodexEngine) {
		envVars["CODEX_HOME"] = constants.TmpMcpConfigDir
	}

	return envVars
}

// hasGitHubOIDCAuthInTools checks if any HTTP MCP server in the tools configuration
// uses auth.type: "github-oidc". This is used to determine whether the OIDC env vars
// (ACTIONS_ID_TOKEN_REQUEST_URL, ACTIONS_ID_TOKEN_REQUEST_TOKEN) need to be forwarded
// to the MCP gateway container.
func hasGitHubOIDCAuthInTools(tools map[string]any) bool {
	for toolName, toolValue := range tools {
		// Skip standard tools that don't support auth config
		if toolName == "github" || toolName == "playwright" ||
			toolName == "cache-memory" || toolName == "agentic-workflows" ||
			toolName == "safe-outputs" || toolName == "mcp-scripts" {
			continue
		}

		toolConfig, ok := toolValue.(map[string]any)
		if !ok {
			continue
		}

		hasMcp, _ := hasMCPConfig(toolConfig)
		if !hasMcp {
			continue
		}

		mcpConfig, err := getMCPConfig(toolConfig, toolName)
		if err != nil {
			continue
		}

		if mcpConfig.Type == "http" && mcpConfig.Auth != nil && mcpConfig.Auth.Type == "github-oidc" {
			mcpEnvironmentLog.Printf("Found github-oidc auth on HTTP MCP server '%s'", toolName)
			return true
		}
	}
	return false
}
