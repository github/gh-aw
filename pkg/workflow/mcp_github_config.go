// Package workflow provides GitHub MCP server configuration and toolset management.
//
// # GitHub MCP Server Configuration
//
// This file manages the configuration of the GitHub MCP server, which provides
// AI agents with access to GitHub's API through the Model Context Protocol (MCP).
// It handles both local (Docker-based) and remote (hosted) deployment modes.
//
// Key responsibilities:
//   - Extracting GitHub tool configuration from workflow frontmatter
//   - Managing GitHub MCP server modes (local Docker vs remote hosted)
//   - Handling GitHub authentication tokens (custom, default, GitHub App)
//   - Managing read-only and lockdown security modes
//   - Expanding and managing GitHub toolsets (repos, issues, pull_requests, etc.)
//   - Handling allowed tool lists for fine-grained access control
//   - Determining Docker image versions for local mode
//   - Generating automatic lockdown detection steps
//   - Managing GitHub App token minting and invalidation
//
// GitHub MCP modes:
//   - Local (default): Runs GitHub MCP server in Docker container
//   - Remote: Uses hosted GitHub MCP service
//
// Security features:
//   - Read-only mode: Prevents write operations (default: true)
//   - Lockdown mode: Restricts access to current repository only
//   - Automatic lockdown: Enables lockdown for private repositories
//   - Allowed tools: Restricts available GitHub API operations
//
// GitHub toolsets:
//   - default/action-friendly: Standard toolsets safe for GitHub Actions
//   - repos, issues, pull_requests, discussions, search, code_scanning
//   - secret_scanning, labels, releases, milestones, projects, gists
//   - teams, actions, packages (requires specific permissions)
//   - users (excluded from action-friendly due to token limitations)
//
// Token precedence:
//  1. GitHub App token (minted from app configuration)
//  2. Custom github-token from tool configuration
//  3. Top-level github-token from frontmatter
//  4. Default GITHUB_TOKEN secret
//
// Automatic lockdown detection:
// When lockdown is not explicitly set, a step is generated to automatically
// enable lockdown for private repositories while keeping it disabled for
// public repositories. This provides security by default without hindering
// open source workflows.
//
// Related files:
//   - mcp_renderer.go: Renders GitHub MCP configuration to YAML
//   - mcp_environment.go: Manages GitHub MCP environment variables
//   - mcp_setup_generator.go: Generates GitHub MCP setup steps
//   - safe_outputs_app.go: GitHub App token minting helpers
//
// Example configuration:
//
//	tools:
//	  github:
//	    mode: remote                    # or "local" for Docker
//	    github-token: ${{ secrets.PAT }}
//	    read-only: true
//	    lockdown: true                  # or omit for automatic detection
//	    toolsets: [repos, issues, pull_requests]
//	    allowed: [get_repo, list_issues, get_pull_request]
package workflow

import (
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

var githubConfigLog = logger.New("workflow:mcp_github_config")

// hasGitHubTool checks if the GitHub tool is configured (using ParsedTools)
func hasGitHubTool(parsedTools *Tools) bool {
	if parsedTools == nil {
		return false
	}
	return parsedTools.GitHub != nil
}

// getGitHubType extracts the mode from GitHub tool configuration (local or remote)
func getGitHubType(githubTool any) string {
	if toolConfig, ok := githubTool.(map[string]any); ok {
		if modeSetting, exists := toolConfig["mode"]; exists {
			if stringValue, ok := modeSetting.(string); ok {
				return stringValue
			}
		}
	}
	return "local" // default to local (Docker)
}

// getGitHubToken extracts the custom github-token from GitHub tool configuration
func getGitHubToken(githubTool any) string {
	if toolConfig, ok := githubTool.(map[string]any); ok {
		if tokenSetting, exists := toolConfig["github-token"]; exists {
			if stringValue, ok := tokenSetting.(string); ok {
				return stringValue
			}
		}
	}
	return ""
}

// getGitHubReadOnly checks if read-only mode is enabled for GitHub tool
// Defaults to true for security
func getGitHubReadOnly(githubTool any) bool {
	if toolConfig, ok := githubTool.(map[string]any); ok {
		if readOnlySetting, exists := toolConfig["read-only"]; exists {
			if boolValue, ok := readOnlySetting.(bool); ok {
				return boolValue
			}
		}
	}
	return true // default to read-only for security
}

// getGitHubLockdown checks if lockdown mode is enabled for GitHub tool
// Defaults to false (lockdown disabled)
func getGitHubLockdown(githubTool any) bool {
	if toolConfig, ok := githubTool.(map[string]any); ok {
		if lockdownSetting, exists := toolConfig["lockdown"]; exists {
			if boolValue, ok := lockdownSetting.(bool); ok {
				return boolValue
			}
		}
	}
	return false // default to lockdown disabled
}

// hasGitHubLockdownExplicitlySet checks if lockdown field is explicitly set in GitHub tool config
func hasGitHubLockdownExplicitlySet(githubTool any) bool {
	if toolConfig, ok := githubTool.(map[string]any); ok {
		_, exists := toolConfig["lockdown"]
		return exists
	}
	return false
}

// getGitHubToolsets extracts the toolsets configuration from GitHub tool
// Expands "default" to individual toolsets for action-friendly compatibility
func getGitHubToolsets(githubTool any) string {
	if toolConfig, ok := githubTool.(map[string]any); ok {
		if toolsetsSetting, exists := toolConfig["toolsets"]; exists {
			// Handle array format only
			switch v := toolsetsSetting.(type) {
			case []any:
				// Convert array to comma-separated string
				toolsets := make([]string, 0, len(v))
				for _, item := range v {
					if str, ok := item.(string); ok {
						toolsets = append(toolsets, str)
					}
				}
				toolsetsStr := strings.Join(toolsets, ",")
				// Expand "default" to individual toolsets for action-friendly compatibility
				return expandDefaultToolset(toolsetsStr)
			case []string:
				toolsetsStr := strings.Join(v, ",")
				// Expand "default" to individual toolsets for action-friendly compatibility
				return expandDefaultToolset(toolsetsStr)
			}
		}
	}
	// default to action-friendly toolsets (excludes "users" which GitHub Actions tokens don't support)
	return strings.Join(ActionFriendlyGitHubToolsets, ",")
}

// expandDefaultToolset expands "default" and "action-friendly" keywords to individual toolsets.
// This ensures that "default" and "action-friendly" in the source expand to action-friendly toolsets
// (excluding "users" which GitHub Actions tokens don't support).
func expandDefaultToolset(toolsetsStr string) string {
	if toolsetsStr == "" {
		return strings.Join(ActionFriendlyGitHubToolsets, ",")
	}

	// Split by comma and check if "default" or "action-friendly" is present
	toolsets := strings.Split(toolsetsStr, ",")
	var result []string
	seenToolsets := make(map[string]bool)

	for _, toolset := range toolsets {
		toolset = strings.TrimSpace(toolset)
		if toolset == "" {
			continue
		}

		if toolset == "default" || toolset == "action-friendly" {
			// Expand "default" or "action-friendly" to action-friendly toolsets (excludes "users")
			for _, dt := range ActionFriendlyGitHubToolsets {
				if !seenToolsets[dt] {
					result = append(result, dt)
					seenToolsets[dt] = true
				}
			}
		} else {
			// Keep other toolsets as-is (including "all", individual toolsets, etc.)
			if !seenToolsets[toolset] {
				result = append(result, toolset)
				seenToolsets[toolset] = true
			}
		}
	}

	return strings.Join(result, ",")
}

// getGitHubAllowedTools extracts the allowed tools list from GitHub tool configuration
// Returns the list of allowed tools, or nil if no allowed list is specified (which means all tools are allowed)
func getGitHubAllowedTools(githubTool any) []string {
	if toolConfig, ok := githubTool.(map[string]any); ok {
		if allowedSetting, exists := toolConfig["allowed"]; exists {
			// Handle array format
			switch v := allowedSetting.(type) {
			case []any:
				// Convert array to string slice
				tools := make([]string, 0, len(v))
				for _, item := range v {
					if str, ok := item.(string); ok {
						tools = append(tools, str)
					}
				}
				return tools
			case []string:
				return v
			}
		}
	}
	return nil
}

func getGitHubDockerImageVersion(githubTool any) string {
	githubDockerImageVersion := string(constants.DefaultGitHubMCPServerVersion) // Default Docker image version
	// Extract version setting from tool properties
	if toolConfig, ok := githubTool.(map[string]any); ok {
		if versionSetting, exists := toolConfig["version"]; exists {
			// Handle different version types
			switch v := versionSetting.(type) {
			case string:
				githubDockerImageVersion = v
			case int:
				githubDockerImageVersion = fmt.Sprintf("%d", v)
			case int64:
				githubDockerImageVersion = fmt.Sprintf("%d", v)
			case uint64:
				githubDockerImageVersion = fmt.Sprintf("%d", v)
			case float64:
				// Use %g to avoid trailing zeros and scientific notation for simple numbers
				githubDockerImageVersion = fmt.Sprintf("%g", v)
			}
		}
	}
	return githubDockerImageVersion
}

// generateGitHubMCPLockdownDetectionStep generates a step to determine automatic lockdown mode
// for GitHub MCP server based on repository visibility. This step is added when:
// - GitHub tool is enabled AND
// - lockdown field is not explicitly specified in the workflow configuration
// The step includes a runtime condition that only executes if GH_AW_GITHUB_MCP_SERVER_TOKEN is defined
func (c *Compiler) generateGitHubMCPLockdownDetectionStep(yaml *strings.Builder, data *WorkflowData) {
	// Check if GitHub tool is present
	githubTool, hasGitHub := data.Tools["github"]
	if !hasGitHub || githubTool == false {
		return
	}

	// Check if lockdown is already explicitly set
	if hasGitHubLockdownExplicitlySet(githubTool) {
		githubConfigLog.Print("Lockdown explicitly set in workflow, skipping automatic lockdown determination")
		return
	}

	githubConfigLog.Print("Generating automatic lockdown determination step for GitHub MCP server")

	// Resolve the latest version of actions/github-script
	actionRepo := "actions/github-script"
	actionVersion := string(constants.DefaultGitHubScriptVersion)
	pinnedAction, err := GetActionPinWithData(actionRepo, actionVersion, data)
	if err != nil {
		githubConfigLog.Printf("Failed to resolve %s@%s: %v", actionRepo, actionVersion, err)
		// In strict mode, this error would have been returned by GetActionPinWithData
		// In normal mode, we fall back to using the version tag without pinning
		pinnedAction = fmt.Sprintf("%s@%s", actionRepo, actionVersion)
	}

	// Generate the step using the determine_automatic_lockdown.cjs action
	// The step only runs if GH_AW_GITHUB_MCP_SERVER_TOKEN secret is defined
	yaml.WriteString("      - name: Determine automatic lockdown mode for GitHub MCP server\n")
	yaml.WriteString("        id: determine-automatic-lockdown\n")
	yaml.WriteString("        env:\n")
	yaml.WriteString("          TOKEN_CHECK: ${{ secrets.GH_AW_GITHUB_MCP_SERVER_TOKEN }}\n")
	yaml.WriteString("        if: env.TOKEN_CHECK != ''\n")
	fmt.Fprintf(yaml, "        uses: %s\n", pinnedAction)
	yaml.WriteString("        with:\n")
	yaml.WriteString("          script: |\n")
	yaml.WriteString("            const determineAutomaticLockdown = require('/opt/gh-aw/actions/determine_automatic_lockdown.cjs');\n")
	yaml.WriteString("            await determineAutomaticLockdown(github, context, core);\n")
}

// generateGitHubMCPAppTokenMintingStep generates a step to mint a GitHub App token for GitHub MCP server
// This step is added when:
// - GitHub tool is enabled with app configuration
// The step mints an installation access token with permissions matching the agent job permissions
func (c *Compiler) generateGitHubMCPAppTokenMintingStep(yaml *strings.Builder, data *WorkflowData) {
	// Check if GitHub tool has app configuration
	if data.ParsedTools == nil || data.ParsedTools.GitHub == nil || data.ParsedTools.GitHub.App == nil {
		return
	}

	app := data.ParsedTools.GitHub.App
	githubConfigLog.Printf("Generating GitHub App token minting step for GitHub MCP server: app-id=%s", app.AppID)

	// Get permissions from the agent job - parse from YAML string
	var permissions *Permissions
	if data.Permissions != "" {
		parser := NewPermissionsParser(data.Permissions)
		permissions = parser.ToPermissions()
	} else {
		githubConfigLog.Print("No permissions specified, using empty permissions")
		permissions = NewPermissions()
	}

	// Generate the token minting step using the existing helper from safe_outputs_app.go
	steps := c.buildGitHubAppTokenMintStep(app, permissions)

	// Modify the step ID to differentiate from safe-outputs app token
	// Replace "safe-outputs-app-token" with "github-mcp-app-token"
	for _, step := range steps {
		modifiedStep := strings.ReplaceAll(step, "id: safe-outputs-app-token", "id: github-mcp-app-token")
		yaml.WriteString(modifiedStep)
	}
}

// generateGitHubMCPAppTokenInvalidationStep generates a step to invalidate the GitHub App token for GitHub MCP server
// This step always runs (even on failure) to ensure tokens are properly cleaned up
func (c *Compiler) generateGitHubMCPAppTokenInvalidationStep(yaml *strings.Builder, data *WorkflowData) {
	// Check if GitHub tool has app configuration
	if data.ParsedTools == nil || data.ParsedTools.GitHub == nil || data.ParsedTools.GitHub.App == nil {
		return
	}

	githubConfigLog.Print("Generating GitHub App token invalidation step for GitHub MCP server")

	// Generate the token invalidation step using the existing helper from safe_outputs_app.go
	steps := c.buildGitHubAppTokenInvalidationStep()

	// Modify the step references to use github-mcp-app-token instead of safe-outputs-app-token
	for _, step := range steps {
		modifiedStep := strings.ReplaceAll(step, "steps.safe-outputs-app-token.outputs.token", "steps.github-mcp-app-token.outputs.token")
		yaml.WriteString(modifiedStep)
	}
}
