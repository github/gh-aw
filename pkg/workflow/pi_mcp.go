package workflow

import (
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

var piMCPLog = logger.New("workflow:pi_mcp")

// RenderMCPConfig renders the MCP configuration for Pi engine.
func (e *PiEngine) RenderMCPConfig(yaml *strings.Builder, tools map[string]any, mcpTools []string, workflowData *WorkflowData) error {
	piMCPLog.Printf("Rendering MCP config for Pi: tool_count=%d, mcp_tool_count=%d", len(tools), len(mcpTools))

	// The gateway converter emits scoped HTTP clients, which pi_runtime.cjs
	// installs as mcp.json in the same agent directory as models and packages.
	return renderDefaultJSONMCPConfig(yaml, tools, mcpTools, workflowData, constants.ShellMcpServersJsonPath)
}
