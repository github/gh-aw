package workflow

const webFetchMCPServer = "web-fetch"

// The converter installs the fallback in the agent's MCP config, never in the
// gateway's backend config. Its stdio child therefore runs inside AWF.
func usesWebFetchMCP(data *WorkflowData) bool {
	return data != nil && data.webFetchFallback && isCopilotToolValueEnabled(data.Tools, "web-fetch")
}

func usesWebFetchMCPForEngine(engine CapabilityProvider, data *WorkflowData) bool {
	return data != nil && isCopilotToolValueEnabled(data.Tools, "web-fetch") && engine.UsesWebFetchFallback(data)
}
