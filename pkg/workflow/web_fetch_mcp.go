package workflow

const webFetchMCPServer = "web-fetch"

// The converter installs the fallback in the agent's MCP config, never in the
// gateway's backend config. Its stdio child therefore runs inside AWF.
func usesWebFetchMCP(data *WorkflowData) bool {
	return data != nil && usesWebFetchMCPForEngine(data.AI, data)
}

func usesWebFetchMCPForEngine(engineID string, data *WorkflowData) bool {
	if !isCopilotToolValueEnabled(data.Tools, "web-fetch") {
		return false
	}
	switch engineID {
	case "copilot", "pi", "goose", "opencode":
		return true
	case "claude":
		return claudeUsesCopilotInference(data)
	default:
		return false
	}
}
