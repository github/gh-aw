package workflow

import "strings"

// engineSupportsMCPToolCalls reports whether the workflow engine can call MCP tools
// directly. Unknown or unset engines are treated as MCP-capable, matching the default
// engine behaviour.
func engineSupportsMCPToolCalls(catalog *EngineCatalog, data *WorkflowData) bool {
	if data == nil || data.EngineConfig == nil || data.EngineConfig.ID == "" {
		return true
	}

	engineID := strings.ToLower(data.EngineConfig.ID)
	if catalog != nil {
		resolved, err := catalog.Resolve(engineID, data.EngineConfig)
		if err == nil && resolved != nil && resolved.Runtime != nil {
			return resolved.Runtime.GetCapabilities().MCP
		}
	}

	engine, err := GetGlobalEngineRegistry().GetEngine(engineID)
	if err != nil || engine == nil {
		return true
	}
	return engine.GetCapabilities().MCP
}
