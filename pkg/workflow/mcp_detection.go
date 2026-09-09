package workflow

import (
	"github.com/github/gh-aw/pkg/logger"
)

var mcpDetectionLog = logger.New("workflow:mcp_detection")

// HasMCPServers checks if the workflow has any MCP servers configured
func HasMCPServers(workflowData *WorkflowData) bool {
	if workflowData == nil {
		return false
	}

	if mcpDetectionLog.Enabled() {
		mcpDetectionLog.Printf("Checking for MCP servers in workflow '%s': tools=%d", workflowData.Name, len(workflowData.Tools))
	}
	// Check for standard MCP tools
	for toolName, toolValue := range workflowData.Tools {
		// Skip if the tool is explicitly disabled (set to false)
		if toolValue == false {
			continue
		}
		if toolName == "github" || toolName == "cache-memory" || toolName == "agentic-workflows" {
			if mcpDetectionLog.Enabled() {
				mcpDetectionLog.Printf("MCP server detected via built-in tool: %s", toolName)
			}
			return true
		}
		// Check for custom MCP tools
		if mcpConfig, ok := toolValue.(map[string]any); ok {
			if hasMcp, _ := hasMCPConfig(mcpConfig); hasMcp {
				if mcpDetectionLog.Enabled() {
					mcpDetectionLog.Printf("MCP server detected via custom tool config: %s", toolName)
				}
				return true
			}
		}
	}

	// Check if safe-outputs is enabled (adds safe-outputs MCP server)
	if HasSafeOutputsEnabled(workflowData.SafeOutputs) {
		mcpDetectionLog.Print("MCP server detected via safe-outputs configuration")
		return true
	}

	// Check if mcp-scripts is configured and feature flag is enabled (adds mcp-scripts MCP server)
	if IsMCPScriptsEnabled(workflowData.MCPScripts) {
		mcpDetectionLog.Print("MCP server detected via mcp-scripts configuration")
		return true
	}

	mcpDetectionLog.Print("No MCP servers detected in workflow configuration")
	return false
}
