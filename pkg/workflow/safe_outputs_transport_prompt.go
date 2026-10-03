package workflow

import (
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var mcpToolCallsLog = logger.New("workflow:safe_outputs_transport_prompt")

// engineSupportsMCPToolCalls reports whether the workflow engine can call MCP tools
// directly. Unknown or unset engines are treated as MCP-capable, matching the default
// engine behaviour.
func engineSupportsMCPToolCalls(catalog *EngineCatalog, data *WorkflowData) bool {
	if data != nil && data.EngineConfig != nil && data.EngineConfig.ID == "pi" && data.ParsedTools != nil && data.ParsedTools.CLIProxy {
		mcpToolCallsLog.Print("Pi engine in CLI-proxy mode: MCP tool calls unsupported")
		return false
	}
	if data == nil || data.EngineConfig == nil || data.EngineConfig.ID == "" {
		return true
	}

	engineID := strings.ToLower(data.EngineConfig.ID)
	if catalog != nil {
		resolved, err := catalog.Resolve(engineID, data.EngineConfig)
		if err == nil && resolved != nil && resolved.Runtime != nil {
			mcpSupported := resolved.Runtime.GetCapabilities().MCP
			mcpToolCallsLog.Printf("Resolved MCP capability for engine %q via catalog: %t", engineID, mcpSupported)
			return mcpSupported
		}
	}

	engine, err := GetGlobalEngineRegistry().GetEngine(engineID)
	if err != nil || engine == nil {
		mcpToolCallsLog.Printf("Engine %q not found in catalog or registry; defaulting to MCP-capable", engineID)
		return true
	}
	return engine.GetCapabilities().MCP
}
