package cli

// normalizeMCPPayloadStats derives payload averages and server maxima from the
// additive totals already present in historical and current MCP usage records.
func normalizeMCPPayloadStats(usage *MCPToolUsageData) *MCPToolUsageData {
	if usage == nil {
		return nil
	}

	serverMaxInput := make(map[string]int)
	serverMaxOutput := make(map[string]int)
	summaries := make([]MCPToolSummary, 0, len(usage.Summary))
	for _, summary := range usage.Summary {
		if summary.CallCount > 0 {
			summary.AvgInputSize = summary.TotalInputSize / summary.CallCount
			summary.AvgOutputSize = summary.TotalOutputSize / summary.CallCount
		}
		serverMaxInput[summary.ServerName] = max(serverMaxInput[summary.ServerName], summary.MaxInputSize)
		serverMaxOutput[summary.ServerName] = max(serverMaxOutput[summary.ServerName], summary.MaxOutputSize)
		summaries = append(summaries, summary)
	}
	usage.Summary = summaries

	servers := make([]MCPServerStats, 0, len(usage.Servers))
	for _, server := range usage.Servers {
		if server.ToolCallCount > 0 {
			server.AvgInputSize = server.TotalInputSize / server.ToolCallCount
			server.AvgOutputSize = server.TotalOutputSize / server.ToolCallCount
		}
		server.MaxInputSize = max(server.MaxInputSize, serverMaxInput[server.ServerName])
		server.MaxOutputSize = max(server.MaxOutputSize, serverMaxOutput[server.ServerName])
		servers = append(servers, server)
	}
	usage.Servers = servers
	return usage
}
