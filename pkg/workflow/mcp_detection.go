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

	mcpDetectionLog.Print("Checking for MCP servers in workflow configuration")
	// Check for standard MCP tools
	for toolName, toolValue := range workflowData.Tools {
		// Skip if the tool is explicitly disabled (set to false)
		if toolValue == false {
			continue
		}
		if toolName == "github" || toolName == "playwright" || toolName == "cache-memory" || toolName == "agentic-workflows" {
			return true
		}
		// Check for custom MCP tools
		if mcpConfig, ok := toolValue.(map[string]any); ok {
			if hasMcp, _ := hasMCPConfig(mcpConfig); hasMcp {
				return true
			}
		}
	}

	// Check if safe-outputs is enabled (adds safe-outputs MCP server)
	if HasSafeOutputsEnabled(workflowData.SafeOutputs) {
		return true
	}

	// Check if safe-inputs is configured and feature flag is enabled (adds safe-inputs MCP server)
	if IsSafeInputsEnabled(workflowData.SafeInputs, workflowData) {
		return true
	}

	return false
}
