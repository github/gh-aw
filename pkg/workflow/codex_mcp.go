package workflow

import (
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var codexMCPLog = logger.New("workflow:codex_mcp")

// RenderMCPConfig generates MCP server configuration for Codex
func (e *CodexEngine) RenderMCPConfig(yaml *strings.Builder, tools map[string]any, mcpTools []string, workflowData *WorkflowData) {
	if codexMCPLog.Enabled() {
		codexMCPLog.Printf("Rendering MCP config for Codex: mcp_tools=%v, tool_count=%d", mcpTools, len(tools))
	}

	// Create unified renderer with Codex-specific options
	// Codex uses TOML format without Copilot-specific fields and multi-line args
	createRenderer := func(isLast bool) *MCPConfigRendererUnified {
		return NewMCPConfigRenderer(MCPRendererOptions{
			IncludeCopilotFields: false, // Codex doesn't use "type" and "tools" fields
			InlineArgs:           false, // Codex uses multi-line args format
			Format:               "toml",
			IsLast:               isLast,
			ActionMode:           GetActionModeFromWorkflowData(workflowData),
		})
	}

	yaml.WriteString("          cat > /tmp/gh-aw/mcp-config/config.toml << EOF\n")

	// Add history configuration to disable persistence
	yaml.WriteString("          [history]\n")
	yaml.WriteString("          persistence = \"none\"\n")

	// Add shell environment policy to control which environment variables are passed through
	// This is a security feature to prevent accidental exposure of secrets
	e.renderShellEnvironmentPolicy(yaml, tools, mcpTools, workflowData)

	// Expand neutral tools (like playwright: null) to include the copilot agent tools
	expandedTools := e.expandNeutralToolsToCodexToolsFromMap(tools)

	// Generate [mcp_servers] section
	for _, toolName := range mcpTools {
		renderer := createRenderer(false) // isLast is always false in TOML format
		switch toolName {
		case "github":
			githubTool := expandedTools["github"]
			renderer.RenderGitHubMCP(yaml, githubTool, workflowData)
		case "playwright":
			playwrightTool := expandedTools["playwright"]
			renderer.RenderPlaywrightMCP(yaml, playwrightTool)
		case "serena":
			serenaTool := expandedTools["serena"]
			renderer.RenderSerenaMCP(yaml, serenaTool)
		case "agentic-workflows":
			renderer.RenderAgenticWorkflowsMCP(yaml)
		case "safe-outputs":
			// Add safe-outputs MCP server if safe-outputs are configured
			hasSafeOutputs := workflowData != nil && workflowData.SafeOutputs != nil && HasSafeOutputsEnabled(workflowData.SafeOutputs)
			if hasSafeOutputs {
				renderer.RenderSafeOutputsMCP(yaml, workflowData)
			}
		case "safe-inputs":
			// Add safe-inputs MCP server if safe-inputs are configured and feature flag is enabled
			hasSafeInputs := workflowData != nil && IsSafeInputsEnabled(workflowData.SafeInputs, workflowData)
			if hasSafeInputs {
				renderer.RenderSafeInputsMCP(yaml, workflowData.SafeInputs, workflowData)
			}
		case "web-fetch":
			renderMCPFetchServerConfig(yaml, "toml", "          ", false, false)
		default:
			// Handle custom MCP tools using shared helper (with adapter for isLast parameter)
			HandleCustomMCPToolInSwitch(yaml, toolName, expandedTools, false, func(yaml *strings.Builder, toolName string, toolConfig map[string]any, isLast bool) error {
				return e.renderCodexMCPConfigWithContext(yaml, toolName, toolConfig, workflowData)
			})
		}
	}

	// Append custom config if provided
	if workflowData.EngineConfig != nil && workflowData.EngineConfig.Config != "" {
		yaml.WriteString("          \n")
		yaml.WriteString("          # Custom configuration\n")
		// Write the custom config line by line with proper indentation
		configLines := strings.Split(workflowData.EngineConfig.Config, "\n")
		for _, line := range configLines {
			if strings.TrimSpace(line) != "" {
				yaml.WriteString("          " + line + "\n")
			} else {
				yaml.WriteString("          \n")
			}
		}
	}

	yaml.WriteString("          EOF\n")

	// Also generate JSON config for MCP gateway
	// Per MCP Gateway Specification v1.0.0 section 4.1, the gateway requires JSON input
	// This JSON config is used by the gateway, while the TOML config above is used by Codex
	yaml.WriteString("          \n")
	yaml.WriteString("          # Generate JSON config for MCP gateway\n")

	// Build gateway configuration
	gatewayConfig := buildMCPGatewayConfig(workflowData)

	// Use shared JSON renderer for gateway input
	createJSONRenderer := func(isLast bool) *MCPConfigRendererUnified {
		actionMode := ActionModeDev // Default to dev mode
		if workflowData != nil {
			actionMode = workflowData.ActionMode
		}
		return NewMCPConfigRenderer(MCPRendererOptions{
			IncludeCopilotFields: false, // Gateway doesn't need Copilot fields
			InlineArgs:           false, // Use standard multi-line format
			Format:               "json",
			IsLast:               isLast,
			ActionMode:           actionMode,
		})
	}

	_ = RenderJSONMCPConfig(yaml, tools, mcpTools, workflowData, JSONMCPConfigOptions{
		ConfigPath:    "/tmp/gh-aw/mcp-config/mcp-servers.json",
		GatewayConfig: gatewayConfig,
		Renderers: MCPToolRenderers{
			RenderGitHub: func(yaml *strings.Builder, githubTool any, isLast bool, workflowData *WorkflowData) {
				renderer := createJSONRenderer(isLast)
				renderer.RenderGitHubMCP(yaml, githubTool, workflowData)
			},
			RenderPlaywright: func(yaml *strings.Builder, playwrightTool any, isLast bool) {
				renderer := createJSONRenderer(isLast)
				renderer.RenderPlaywrightMCP(yaml, playwrightTool)
			},
			RenderSerena: func(yaml *strings.Builder, serenaTool any, isLast bool) {
				renderer := createJSONRenderer(isLast)
				renderer.RenderSerenaMCP(yaml, serenaTool)
			},
			RenderCacheMemory: func(yaml *strings.Builder, isLast bool, workflowData *WorkflowData) {
				// Cache-memory is not used as MCP server
			},
			RenderAgenticWorkflows: func(yaml *strings.Builder, isLast bool) {
				renderer := createJSONRenderer(isLast)
				renderer.RenderAgenticWorkflowsMCP(yaml)
			},
			RenderSafeOutputs: func(yaml *strings.Builder, isLast bool, workflowData *WorkflowData) {
				renderer := createJSONRenderer(isLast)
				renderer.RenderSafeOutputsMCP(yaml, workflowData)
			},
			RenderSafeInputs: func(yaml *strings.Builder, safeInputs *SafeInputsConfig, isLast bool) {
				renderer := createJSONRenderer(isLast)
				renderer.RenderSafeInputsMCP(yaml, safeInputs, workflowData)
			},
			RenderWebFetch: func(yaml *strings.Builder, isLast bool) {
				renderMCPFetchServerConfig(yaml, "json", "              ", isLast, false)
			},
			RenderCustomMCPConfig: func(yaml *strings.Builder, toolName string, toolConfig map[string]any, isLast bool) error {
				return e.renderCodexJSONMCPConfigWithContext(yaml, toolName, toolConfig, isLast, workflowData)
			},
		},
	})
}

// renderCodexMCPConfigWithContext generates custom MCP server configuration for a single tool in codex workflow config.toml
// This version includes workflowData to determine if localhost URLs should be rewritten
func (e *CodexEngine) renderCodexMCPConfigWithContext(yaml *strings.Builder, toolName string, toolConfig map[string]any, workflowData *WorkflowData) error {
	yaml.WriteString("          \n")
	fmt.Fprintf(yaml, "          [mcp_servers.%s]\n", toolName)

	// Determine if localhost URLs should be rewritten to host.docker.internal
	// This is needed when firewall is enabled (agent is not disabled)
	rewriteLocalhost := workflowData != nil && (workflowData.SandboxConfig == nil ||
		workflowData.SandboxConfig.Agent == nil ||
		!workflowData.SandboxConfig.Agent.Disabled)

	// Use the shared MCP config renderer with TOML format
	renderer := MCPConfigRenderer{
		IndentLevel:              "          ",
		Format:                   "toml",
		RewriteLocalhostToDocker: rewriteLocalhost,
	}

	err := renderSharedMCPConfig(yaml, toolName, toolConfig, renderer)
	if err != nil {
		return err
	}

	return nil
}

// renderCodexJSONMCPConfigWithContext generates custom MCP server configuration in JSON format for gateway
// This is used to generate the JSON config file that the MCP gateway reads
func (e *CodexEngine) renderCodexJSONMCPConfigWithContext(yaml *strings.Builder, toolName string, toolConfig map[string]any, isLast bool, workflowData *WorkflowData) error {
	// Determine if localhost URLs should be rewritten to host.docker.internal
	rewriteLocalhost := workflowData != nil && (workflowData.SandboxConfig == nil ||
		workflowData.SandboxConfig.Agent == nil ||
		!workflowData.SandboxConfig.Agent.Disabled)

	// Use the shared renderer with JSON format for gateway
	renderer := MCPConfigRenderer{
		Format:                   "json",
		IndentLevel:              "              ",
		RewriteLocalhostToDocker: rewriteLocalhost,
	}

	yaml.WriteString("              \"" + toolName + "\": {\n")

	err := renderSharedMCPConfig(yaml, toolName, toolConfig, renderer)
	if err != nil {
		return err
	}

	if isLast {
		yaml.WriteString("              }\n")
	} else {
		yaml.WriteString("              },\n")
	}

	return nil
}
