// Package workflow provides GitHub Actions setup step generation for MCP servers.
//
// # MCP Setup Generator
//
// This file generates the complete setup sequence for MCP servers in GitHub Actions
// workflows. It orchestrates the initialization of all MCP tools including built-in
// servers (GitHub, safe-outputs, mcp-scripts) and custom HTTP/stdio
// MCP servers.
//
// Key responsibilities:
//   - Identifying and collecting MCP tools from workflow configuration
//   - Generating Docker image download steps
//   - Installing gh-aw extension for agentic-workflows tool
//   - Setting up safe-outputs MCP server runtime files and container config
//   - Setting up mcp-scripts MCP server (config, tool files, HTTP server)
//   - Starting the MCP gateway with proper environment variables
//   - Rendering MCP configuration for the selected AI engine
//
// Setup sequence:
//  1. Download required Docker images
//  2. Install gh-aw extension (if agentic-workflows enabled)
//  3. Write safe-outputs config.json (may contain template expressions; kept small)
//  4. Write safe-outputs tools.json and validation.json (large, no template expressions)
//  5. Prepare safe-outputs runtime files for containerized MCP execution
//  6. Setup mcp-scripts config and tool files (JavaScript, Python, Shell, Go)
//  7. Generate and start mcp-scripts HTTP server
//  8. Start MCP Gateway with all environment variables

// 10. Render engine-specific MCP configuration
//
// MCP tools supported:
//   - github: GitHub API access via MCP (local Docker or remote hosted)
//   - playwright: Browser automation with Playwright
//   - safe-outputs: Controlled output storage for AI agents
//   - mcp-scripts: Custom tool execution with secret passthrough
//   - cache-memory: Memory/knowledge base management
//   - agentic-workflows: Workflow execution via gh-aw
//   - Custom HTTP/stdio MCP servers
//
// Gateway modes:
//   - Enabled (default): MCP servers run through gateway proxy
//   - Disabled (sandbox: false): Direct MCP server communication
//
// Related files:
//   - mcp_setup_awf_install.go: Agentic-workflows gh-aw install step generation
//   - mcp_setup_safe_outputs.go: All safe-outputs YAML generation
//   - mcp_setup_scripts.go: mcp-scripts config and tool file generation
//   - mcp_setup_gateway.go: MCP gateway container command construction
//   - mcp_gateway_config.go: Gateway configuration management
//   - mcp_environment.go: Environment variable collection
//   - mcp_renderer.go: MCP configuration YAML rendering
//   - safe_outputs.go: Safe outputs server configuration
//   - mcp_scripts.go: MCP Scripts server configuration
//
// Example workflow setup:
//   - Download Docker images
//   - Write safe-outputs config to ${RUNNER_TEMP}/gh-aw/safeoutputs/
//   - Mount safe-outputs runtime files into the gh-aw node MCP container
//   - Write mcp-scripts config to ${RUNNER_TEMP}/gh-aw/mcp-scripts/
//   - Start mcp-scripts HTTP server on port 3000
//   - Start MCP Gateway (default port 8080)
//   - Render MCP config based on engine (copilot/claude/codex/custom)
package workflow

import (
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var mcpSetupGeneratorLog = logger.New("workflow:mcp_setup_generator")

// generateMCPSetup generates the MCP server configuration setup
func (c *Compiler) generateMCPSetup(yaml *strings.Builder, tools map[string]any, engine CodingAgentEngine, workflowData *WorkflowData) error { //nolint:largefunc // Existing setup orchestration preserves step ordering.
	mcpSetupGeneratorLog.Print("Generating MCP server configuration setup")
	if workflowData == nil {
		return nil
	}

	mcpTools := collectMCPTools(workflowData)
	tools = toolsWithEnclaveGitHubIssues(tools, workflowData)

	// Populate dispatch-workflow file mappings before generating config
	// This ensures workflow_files is available in the config.json
	populateDispatchWorkflowFiles(workflowData, c.markdownPath)

	// Populate call-workflow file mappings before generating config
	// This ensures workflow_files is available in the config.json
	populateCallWorkflowFiles(workflowData, c.markdownPath)

	safeOutputConfig, err := generateSafeOutputsConfigIfEnabled(workflowData)
	if err != nil {
		return fmt.Errorf("safe outputs setup preparation failed: %w", err)
	}

	// Sort tools to ensure stable code generation
	sort.Strings(mcpTools)

	if mcpSetupGeneratorLog.Enabled() {
		mcpSetupGeneratorLog.Printf("Collected %d MCP tools: %v", len(mcpTools), mcpTools)
	}

	// Ensure MCP gateway config has defaults set before collecting Docker images
	ensureDefaultMCPGatewayConfig(workflowData)

	// Collect all Docker images that will be used and generate download step
	dockerImages := collectDockerImages(tools, workflowData, c.actionMode)
	generateDownloadDockerImagesStep(yaml, dockerImages)

	// If no MCP tools, skip setup unless the engine still needs MCP gateway/config bootstrap.
	// Codex with AWF firewall enabled requires MCP config generation to set its OpenAI proxy
	// provider, even when no MCP tools are configured (e.g. threat-detection jobs).
	needsSetupWithoutMCPTools := len(mcpTools) == 0 && engine.GetID() == "codex" && isFirewallEnabled(workflowData)
	if len(mcpTools) == 0 && !needsSetupWithoutMCPTools {
		mcpSetupGeneratorLog.Print("No MCP tools configured, skipping MCP setup")
		return nil
	}

	hasAgenticWorkflows := slices.Contains(mcpTools, "agentic-workflows")
	generateAgenticWorkflowsInstallStep(c, yaml, hasAgenticWorkflows, workflowData)

	generateSafeOutputsSetup(c, yaml, safeOutputConfig, workflowData)
	if err := generateMCPScriptsSetup(yaml, workflowData); err != nil {
		return fmt.Errorf("failed to generate mcp-scripts setup YAML: %w", err)
	}
	// Extract GH_AW_INPUT_* env vars from the safe-outputs config so the MCP
	// gateway container receives them in its -e allowlist and the nested
	// safe-outputs container inherits them via its env_vars/env allowlist.
	// Without this, any safe-outputs field that references ${{ inputs.* }} is
	// written to config.json as a ${GH_AW_INPUT_…} placeholder that the
	// containerised MCP server cannot resolve, causing failures such as
	// "No remote refs available for merge-base calculation" when using a
	// dynamic base-branch.
	workflowData.SafeOutputsInputEnvVars = extractSafeOutputsInputEnvVars(safeOutputConfig)
	return generateMCPGatewaySetup(yaml, tools, mcpTools, engine, workflowData, hasAgenticWorkflows, workflowData.SafeOutputsInputEnvVars)
}

func toolsWithEnclaveGitHubIssues(tools map[string]any, workflowData *WorkflowData) map[string]any {
	if !enclaveGitHubDelegationEnabled(workflowData) {
		return tools
	}
	updated := make(map[string]any, len(tools)+1)
	maps.Copy(updated, tools)
	if githubToolRaw, hasGitHub := tools["github"]; hasGitHub && githubToolRaw == false {
		return updated
	}
	githubTool, _ := tools["github"].(map[string]any)
	githubConfig := make(map[string]any, len(githubTool))
	maps.Copy(githubConfig, githubTool)
	if allowed, ok := githubConfig["allowed"].([]any); ok {
		for _, tool := range []string{"list_issues", "issue_read"} {
			if !slices.Contains(allowed, any(tool)) {
				githubConfig["allowed"] = append(allowed, tool)
			}
		}
	} else if allowed, ok := githubConfig["allowed"].([]string); ok {
		for _, tool := range []string{"list_issues", "issue_read"} {
			if !slices.Contains(allowed, tool) {
				githubConfig["allowed"] = append(allowed, tool)
			}
		}
	} else if toolsets, ok := githubConfig["toolsets"].([]any); ok {
		if !slices.Contains(toolsets, "issues") {
			githubConfig["toolsets"] = append(toolsets, "issues")
		}
	} else if toolsets, ok := githubConfig["toolsets"].([]string); ok {
		if !slices.Contains(toolsets, "issues") {
			githubConfig["toolsets"] = append(toolsets, "issues")
		}
	} else if toolsets, ok := githubConfig["toolsets"].(string); ok {
		if !slices.Contains(strings.Split(toolsets, ","), "issues") {
			githubConfig["toolsets"] = toolsets + ",issues"
		}
	} else {
		githubConfig["toolsets"] = []any{"issues"}
	}
	updated["github"] = githubConfig
	return updated
}

func collectMCPTools(workflowData *WorkflowData) []string {
	var mcpTools []string
	for toolName, toolValue := range workflowData.Tools {
		if toolValue == false {
			continue
		}
		if toolName == "github" && isGitHubCLIModeEnabled(workflowData) {
			mcpSetupGeneratorLog.Print("Skipping GitHub MCP server registration: tools.github.mode is gh-proxy")
			continue
		}
		if toolName == "github" || toolName == "cache-memory" || toolName == "agentic-workflows" {
			mcpTools = append(mcpTools, toolName)
			continue
		}
		if mcpConfig, ok := toolValue.(map[string]any); ok {
			if hasMcp, _ := hasMCPConfig(mcpConfig); hasMcp {
				mcpTools = append(mcpTools, toolName)
			}
		}
	}
	if HasSafeOutputsEnabled(workflowData.SafeOutputs) {
		mcpTools = append(mcpTools, "safe-outputs")
	}
	if IsMCPScriptsEnabled(workflowData.MCPScripts) {
		mcpTools = append(mcpTools, "mcp-scripts")
	}
	if enclavesEnabled(workflowData) {
		mcpTools = append(mcpTools, enclaveMCPServerName)
	}
	if enclaveGitHubDelegationEnabled(workflowData) && !slices.Contains(mcpTools, "github") {
		mcpTools = append(mcpTools, "github")
	}
	return mcpTools
}

func generateSafeOutputsConfigIfEnabled(workflowData *WorkflowData) (string, error) {
	if !HasSafeOutputsEnabled(workflowData.SafeOutputs) {
		return "", nil
	}
	safeOutputConfig, err := generateSafeOutputsConfig(workflowData)
	if err != nil {
		return "", fmt.Errorf("failed to generate safe outputs config: %w", err)
	}
	return safeOutputConfig, nil
}

// extractSafeOutputsInputEnvVars returns a map of GH_AW_INPUT_* environment variable names
// to their GitHub Actions expressions for all ${{ inputs.* }} references in the safe-outputs
// config. The map is used to populate the MCP gateway step env block AND the docker run -e
// allowlist, so the containerised safe-outputs MCP server can resolve the ${GH_AW_INPUT_…}
// shell-style placeholders written into config.json at compile time.
func extractSafeOutputsInputEnvVars(safeOutputConfig string) map[string]string {
	if safeOutputConfig == "" {
		return nil
	}
	envKeys, envValues := buildSafeOutputsConfigRuntimeEnvVars(safeOutputConfig)
	result := make(map[string]string)
	for _, key := range envKeys {
		if strings.HasPrefix(key, "GH_AW_INPUT_") {
			result[key] = envValues[key]
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}
