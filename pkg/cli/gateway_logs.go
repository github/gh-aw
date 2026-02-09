// This file provides command-line interface functionality for gh-aw.
// This file (gateway_logs.go) contains functions for parsing and analyzing
// MCP gateway logs from gateway.jsonl files.
//
// Key responsibilities:
//   - Parsing gateway.jsonl JSONL format logs
//   - Extracting server and tool usage metrics
//   - Aggregating gateway statistics
//   - Rendering gateway metrics tables

package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/timeutil"
)

var gatewayLogsLog = logger.New("cli:gateway_logs")

// GatewayLogEntry represents a single log entry from gateway.jsonl
type GatewayLogEntry struct {
	Timestamp  string  `json:"timestamp"`
	Level      string  `json:"level"`
	Type       string  `json:"type"`
	Event      string  `json:"event"`
	ServerName string  `json:"server_name,omitempty"`
	ToolName   string  `json:"tool_name,omitempty"`
	Method     string  `json:"method,omitempty"`
	Duration   float64 `json:"duration,omitempty"` // in milliseconds
	InputSize  int     `json:"input_size,omitempty"`
	OutputSize int     `json:"output_size,omitempty"`
	Status     string  `json:"status,omitempty"`
	Error      string  `json:"error,omitempty"`
	Message    string  `json:"message,omitempty"`
}

// GatewayServerMetrics represents usage metrics for a single MCP server
type GatewayServerMetrics struct {
	ServerName    string
	RequestCount  int
	ToolCallCount int
	TotalDuration float64 // in milliseconds
	ErrorCount    int
	Tools         map[string]*GatewayToolMetrics
}

// GatewayToolMetrics represents usage metrics for a specific tool
type GatewayToolMetrics struct {
	ToolName        string
	CallCount       int
	TotalDuration   float64 // in milliseconds
	AvgDuration     float64 // in milliseconds
	MaxDuration     float64 // in milliseconds
	MinDuration     float64 // in milliseconds
	ErrorCount      int
	TotalInputSize  int
	TotalOutputSize int
}

// GatewayMetrics represents aggregated metrics from gateway logs
type GatewayMetrics struct {
	TotalRequests  int
	TotalToolCalls int
	TotalErrors    int
	Servers        map[string]*GatewayServerMetrics
	StartTime      time.Time
	EndTime        time.Time
	TotalDuration  float64 // in milliseconds
}

// parseGatewayLogs parses a gateway.jsonl file and extracts metrics
func parseGatewayLogs(logDir string, verbose bool) (*GatewayMetrics, error) {
	// Try root directory first (for older logs where gateway.jsonl was in the root)
	gatewayLogPath := filepath.Join(logDir, "gateway.jsonl")

	// Check if gateway.jsonl exists in root
	if _, err := os.Stat(gatewayLogPath); os.IsNotExist(err) {
		// Try mcp-logs subdirectory (new path after artifact download)
		// Gateway logs are uploaded from /tmp/gh-aw/mcp-logs/gateway.jsonl and the common parent
		// /tmp/gh-aw/ is stripped during artifact upload, resulting in mcp-logs/gateway.jsonl after download
		mcpLogsPath := filepath.Join(logDir, "mcp-logs", "gateway.jsonl")
		if _, err := os.Stat(mcpLogsPath); os.IsNotExist(err) {
			gatewayLogsLog.Printf("gateway.jsonl not found at: %s or %s", gatewayLogPath, mcpLogsPath)
			return nil, fmt.Errorf("gateway.jsonl not found")
		}
		gatewayLogPath = mcpLogsPath
		gatewayLogsLog.Printf("Found gateway.jsonl in mcp-logs subdirectory")
	}

	gatewayLogsLog.Printf("Parsing gateway.jsonl from: %s", gatewayLogPath)

	file, err := os.Open(gatewayLogPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open gateway.jsonl: %w", err)
	}
	defer file.Close()

	metrics := &GatewayMetrics{
		Servers: make(map[string]*GatewayServerMetrics),
	}

	scanner := bufio.NewScanner(file)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines
		if line == "" {
			continue
		}

		var entry GatewayLogEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			gatewayLogsLog.Printf("Failed to parse line %d: %v", lineNum, err)
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to parse gateway.jsonl line %d: %v", lineNum, err)))
			}
			continue
		}

		// Process the entry based on its type/event
		processGatewayLogEntry(&entry, metrics, verbose)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading gateway.jsonl: %w", err)
	}

	// Calculate aggregate statistics
	calculateGatewayAggregates(metrics)

	gatewayLogsLog.Printf("Successfully parsed gateway.jsonl: %d servers, %d total requests",
		len(metrics.Servers), metrics.TotalRequests)

	return metrics, nil
}

// processGatewayLogEntry processes a single log entry and updates metrics
func processGatewayLogEntry(entry *GatewayLogEntry, metrics *GatewayMetrics, verbose bool) {
	// Parse timestamp for time range
	if entry.Timestamp != "" {
		if t, err := time.Parse(time.RFC3339, entry.Timestamp); err == nil {
			if metrics.StartTime.IsZero() || t.Before(metrics.StartTime) {
				metrics.StartTime = t
			}
			if metrics.EndTime.IsZero() || t.After(metrics.EndTime) {
				metrics.EndTime = t
			}
		}
	}

	// Track errors
	if entry.Status == "error" || entry.Error != "" {
		metrics.TotalErrors++
		if entry.ServerName != "" {
			server := getOrCreateServer(metrics, entry.ServerName)
			server.ErrorCount++

			if entry.ToolName != "" {
				tool := getOrCreateTool(server, entry.ToolName)
				tool.ErrorCount++
			}
		}
	}

	// Process based on event type
	switch entry.Event {
	case "request", "tool_call", "rpc_call":
		metrics.TotalRequests++

		if entry.ServerName != "" {
			server := getOrCreateServer(metrics, entry.ServerName)
			server.RequestCount++

			if entry.Duration > 0 {
				server.TotalDuration += entry.Duration
				metrics.TotalDuration += entry.Duration
			}

			// Track tool calls
			if entry.ToolName != "" || entry.Method != "" {
				toolName := entry.ToolName
				if toolName == "" {
					toolName = entry.Method
				}

				metrics.TotalToolCalls++
				server.ToolCallCount++

				tool := getOrCreateTool(server, toolName)
				tool.CallCount++

				if entry.Duration > 0 {
					tool.TotalDuration += entry.Duration
					if tool.MaxDuration == 0 || entry.Duration > tool.MaxDuration {
						tool.MaxDuration = entry.Duration
					}
					if tool.MinDuration == 0 || entry.Duration < tool.MinDuration {
						tool.MinDuration = entry.Duration
					}
				}

				if entry.InputSize > 0 {
					tool.TotalInputSize += entry.InputSize
				}
				if entry.OutputSize > 0 {
					tool.TotalOutputSize += entry.OutputSize
				}
			}
		}
	}
}

// getOrCreateServer gets or creates a server metrics entry
func getOrCreateServer(metrics *GatewayMetrics, serverName string) *GatewayServerMetrics {
	if server, exists := metrics.Servers[serverName]; exists {
		return server
	}

	server := &GatewayServerMetrics{
		ServerName: serverName,
		Tools:      make(map[string]*GatewayToolMetrics),
	}
	metrics.Servers[serverName] = server
	return server
}

// getOrCreateTool gets or creates a tool metrics entry
func getOrCreateTool(server *GatewayServerMetrics, toolName string) *GatewayToolMetrics {
	if tool, exists := server.Tools[toolName]; exists {
		return tool
	}

	tool := &GatewayToolMetrics{
		ToolName: toolName,
	}
	server.Tools[toolName] = tool
	return tool
}

// calculateGatewayAggregates calculates aggregate statistics
func calculateGatewayAggregates(metrics *GatewayMetrics) {
	for _, server := range metrics.Servers {
		for _, tool := range server.Tools {
			if tool.CallCount > 0 {
				tool.AvgDuration = tool.TotalDuration / float64(tool.CallCount)
			}
		}
	}
}

// renderGatewayMetricsTable renders gateway metrics as a console table
func renderGatewayMetricsTable(metrics *GatewayMetrics, verbose bool) string {
	if metrics == nil || len(metrics.Servers) == 0 {
		return ""
	}

	var output strings.Builder

	output.WriteString("\n")
	output.WriteString(console.FormatInfoMessage("MCP Gateway Metrics"))
	output.WriteString("\n\n")

	// Summary statistics
	fmt.Fprintf(&output, "Total Requests: %d\n", metrics.TotalRequests)
	fmt.Fprintf(&output, "Total Tool Calls: %d\n", metrics.TotalToolCalls)
	fmt.Fprintf(&output, "Total Errors: %d\n", metrics.TotalErrors)
	fmt.Fprintf(&output, "Servers: %d\n", len(metrics.Servers))

	if !metrics.StartTime.IsZero() && !metrics.EndTime.IsZero() {
		duration := metrics.EndTime.Sub(metrics.StartTime)
		fmt.Fprintf(&output, "Time Range: %s\n", duration.Round(time.Second))
	}

	output.WriteString("\n")

	// Server metrics table
	if len(metrics.Servers) > 0 {
		output.WriteString("Server Usage:\n")
		output.WriteString("┌────────────────────────────┬──────────┬────────────┬───────────┬────────┐\n")
		output.WriteString("│ Server                     │ Requests │ Tool Calls │ Avg Time  │ Errors │\n")
		output.WriteString("├────────────────────────────┼──────────┼────────────┼───────────┼────────┤\n")

		// Sort servers by request count
		var serverNames []string
		for name := range metrics.Servers {
			serverNames = append(serverNames, name)
		}
		sort.Slice(serverNames, func(i, j int) bool {
			return metrics.Servers[serverNames[i]].RequestCount > metrics.Servers[serverNames[j]].RequestCount
		})

		for _, serverName := range serverNames {
			server := metrics.Servers[serverName]
			avgTime := 0.0
			if server.RequestCount > 0 {
				avgTime = server.TotalDuration / float64(server.RequestCount)
			}

			fmt.Fprintf(&output, "│ %-26s │ %8d │ %10d │ %7.0fms │ %6d │\n",
				truncateString(serverName, 26),
				server.RequestCount,
				server.ToolCallCount,
				avgTime,
				server.ErrorCount)
		}

		output.WriteString("└────────────────────────────┴──────────┴────────────┴───────────┴────────┘\n")
	}

	// Tool metrics table (if verbose)
	if verbose {
		output.WriteString("\n")
		output.WriteString("Tool Usage Details:\n")

		for _, serverName := range getSortedServerNames(metrics) {
			server := metrics.Servers[serverName]
			if len(server.Tools) == 0 {
				continue
			}

			fmt.Fprintf(&output, "\n%s:\n", serverName)
			output.WriteString("┌──────────────────────────┬───────┬──────────┬──────────┬──────────┐\n")
			output.WriteString("│ Tool                     │ Calls │ Avg Time │ Max Time │ Errors   │\n")
			output.WriteString("├──────────────────────────┼───────┼──────────┼──────────┼──────────┤\n")

			// Sort tools by call count
			var toolNames []string
			for name := range server.Tools {
				toolNames = append(toolNames, name)
			}
			sort.Slice(toolNames, func(i, j int) bool {
				return server.Tools[toolNames[i]].CallCount > server.Tools[toolNames[j]].CallCount
			})

			for _, toolName := range toolNames {
				tool := server.Tools[toolName]
				fmt.Fprintf(&output, "│ %-24s │ %5d │ %6.0fms │ %6.0fms │ %8d │\n",
					truncateString(toolName, 24),
					tool.CallCount,
					tool.AvgDuration,
					tool.MaxDuration,
					tool.ErrorCount)
			}

			output.WriteString("└──────────────────────────┴───────┴──────────┴──────────┴──────────┘\n")
		}
	}

	return output.String()
}

// getSortedServerNames returns server names sorted by request count
func getSortedServerNames(metrics *GatewayMetrics) []string {
	var names []string
	for name := range metrics.Servers {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		return metrics.Servers[names[i]].RequestCount > metrics.Servers[names[j]].RequestCount
	})
	return names
}

// truncateString truncates a string to a maximum length
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

// extractMCPToolUsageData creates detailed MCP tool usage data from gateway metrics
func extractMCPToolUsageData(logDir string, verbose bool) (*MCPToolUsageData, error) {
	// Parse gateway logs
	gatewayMetrics, err := parseGatewayLogs(logDir, verbose)
	if err != nil {
		// Return nil if gateway.jsonl doesn't exist (not an error for workflows without MCP)
		if strings.Contains(err.Error(), "not found") {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to parse gateway logs: %w", err)
	}

	if gatewayMetrics == nil || len(gatewayMetrics.Servers) == 0 {
		return nil, nil
	}

	mcpData := &MCPToolUsageData{
		Summary:   []MCPToolSummary{},
		ToolCalls: []MCPToolCall{},
		Servers:   []MCPServerStats{},
	}

	// Read gateway.jsonl again to get individual tool call records
	// Try root directory first (for older logs where gateway.jsonl was in the root)
	gatewayLogPath := filepath.Join(logDir, "gateway.jsonl")

	// Check if gateway.jsonl exists in root
	if _, err := os.Stat(gatewayLogPath); os.IsNotExist(err) {
		// Try mcp-logs subdirectory (new path after artifact download)
		mcpLogsPath := filepath.Join(logDir, "mcp-logs", "gateway.jsonl")
		if _, err := os.Stat(mcpLogsPath); os.IsNotExist(err) {
			return nil, fmt.Errorf("gateway.jsonl not found")
		}
		gatewayLogPath = mcpLogsPath
	}

	file, err := os.Open(gatewayLogPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open gateway.jsonl: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var entry GatewayLogEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue // Skip malformed lines
		}

		// Only process tool call events
		if entry.Event == "tool_call" || entry.Event == "rpc_call" || entry.Event == "request" {
			toolName := entry.ToolName
			if toolName == "" {
				toolName = entry.Method
			}

			// Skip entries without tool information
			if entry.ServerName == "" || toolName == "" {
				continue
			}

			// Create individual tool call record
			toolCall := MCPToolCall{
				Timestamp:  entry.Timestamp,
				ServerName: entry.ServerName,
				ToolName:   toolName,
				Method:     entry.Method,
				InputSize:  entry.InputSize,
				OutputSize: entry.OutputSize,
				Status:     entry.Status,
				Error:      entry.Error,
			}

			if entry.Duration > 0 {
				toolCall.Duration = timeutil.FormatDuration(time.Duration(entry.Duration * float64(time.Millisecond)))
			}

			mcpData.ToolCalls = append(mcpData.ToolCalls, toolCall)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading gateway.jsonl: %w", err)
	}

	// Build summary statistics from aggregated metrics
	for serverName, serverMetrics := range gatewayMetrics.Servers {
		// Server-level stats
		serverStats := MCPServerStats{
			ServerName:      serverName,
			RequestCount:    serverMetrics.RequestCount,
			ToolCallCount:   serverMetrics.ToolCallCount,
			TotalInputSize:  0,
			TotalOutputSize: 0,
			ErrorCount:      serverMetrics.ErrorCount,
		}

		if serverMetrics.RequestCount > 0 {
			avgDur := serverMetrics.TotalDuration / float64(serverMetrics.RequestCount)
			serverStats.AvgDuration = timeutil.FormatDuration(time.Duration(avgDur * float64(time.Millisecond)))
		}

		// Tool-level stats
		for toolName, toolMetrics := range serverMetrics.Tools {
			summary := MCPToolSummary{
				ServerName:      serverName,
				ToolName:        toolName,
				CallCount:       toolMetrics.CallCount,
				TotalInputSize:  toolMetrics.TotalInputSize,
				TotalOutputSize: toolMetrics.TotalOutputSize,
				MaxInputSize:    0, // Will be calculated below
				MaxOutputSize:   0, // Will be calculated below
				ErrorCount:      toolMetrics.ErrorCount,
			}

			if toolMetrics.AvgDuration > 0 {
				summary.AvgDuration = timeutil.FormatDuration(time.Duration(toolMetrics.AvgDuration * float64(time.Millisecond)))
			}
			if toolMetrics.MaxDuration > 0 {
				summary.MaxDuration = timeutil.FormatDuration(time.Duration(toolMetrics.MaxDuration * float64(time.Millisecond)))
			}

			// Calculate max input/output sizes from individual tool calls
			for _, tc := range mcpData.ToolCalls {
				if tc.ServerName == serverName && tc.ToolName == toolName {
					if tc.InputSize > summary.MaxInputSize {
						summary.MaxInputSize = tc.InputSize
					}
					if tc.OutputSize > summary.MaxOutputSize {
						summary.MaxOutputSize = tc.OutputSize
					}
				}
			}

			mcpData.Summary = append(mcpData.Summary, summary)

			// Update server totals
			serverStats.TotalInputSize += toolMetrics.TotalInputSize
			serverStats.TotalOutputSize += toolMetrics.TotalOutputSize
		}

		mcpData.Servers = append(mcpData.Servers, serverStats)
	}

	// Sort summaries by server name, then tool name
	sort.Slice(mcpData.Summary, func(i, j int) bool {
		if mcpData.Summary[i].ServerName != mcpData.Summary[j].ServerName {
			return mcpData.Summary[i].ServerName < mcpData.Summary[j].ServerName
		}
		return mcpData.Summary[i].ToolName < mcpData.Summary[j].ToolName
	})

	// Sort servers by name
	sort.Slice(mcpData.Servers, func(i, j int) bool {
		return mcpData.Servers[i].ServerName < mcpData.Servers[j].ServerName
	})

	return mcpData, nil
}

// displayAggregatedGatewayMetrics aggregates and displays gateway metrics across all processed runs
func displayAggregatedGatewayMetrics(processedRuns []ProcessedRun, outputDir string, verbose bool) {
	// Aggregate gateway metrics from all runs
	aggregated := &GatewayMetrics{
		Servers: make(map[string]*GatewayServerMetrics),
	}

	runCount := 0
	for _, pr := range processedRuns {
		runDir := pr.Run.LogsPath
		if runDir == "" {
			continue
		}

		// Try to parse gateway.jsonl from this run
		runMetrics, err := parseGatewayLogs(runDir, false)
		if err != nil {
			// Skip runs without gateway.jsonl (this is normal for runs without MCP gateway)
			continue
		}

		runCount++

		// Merge metrics from this run into aggregated metrics
		aggregated.TotalRequests += runMetrics.TotalRequests
		aggregated.TotalToolCalls += runMetrics.TotalToolCalls
		aggregated.TotalErrors += runMetrics.TotalErrors
		aggregated.TotalDuration += runMetrics.TotalDuration

		// Merge server metrics
		for serverName, serverMetrics := range runMetrics.Servers {
			aggServer := getOrCreateServer(aggregated, serverName)
			aggServer.RequestCount += serverMetrics.RequestCount
			aggServer.ToolCallCount += serverMetrics.ToolCallCount
			aggServer.TotalDuration += serverMetrics.TotalDuration
			aggServer.ErrorCount += serverMetrics.ErrorCount

			// Merge tool metrics
			for toolName, toolMetrics := range serverMetrics.Tools {
				aggTool := getOrCreateTool(aggServer, toolName)
				aggTool.CallCount += toolMetrics.CallCount
				aggTool.TotalDuration += toolMetrics.TotalDuration
				aggTool.ErrorCount += toolMetrics.ErrorCount
				aggTool.TotalInputSize += toolMetrics.TotalInputSize
				aggTool.TotalOutputSize += toolMetrics.TotalOutputSize

				// Update max/min durations
				if toolMetrics.MaxDuration > aggTool.MaxDuration {
					aggTool.MaxDuration = toolMetrics.MaxDuration
				}
				if aggTool.MinDuration == 0 || (toolMetrics.MinDuration > 0 && toolMetrics.MinDuration < aggTool.MinDuration) {
					aggTool.MinDuration = toolMetrics.MinDuration
				}
			}
		}

		// Update time range
		if aggregated.StartTime.IsZero() || (!runMetrics.StartTime.IsZero() && runMetrics.StartTime.Before(aggregated.StartTime)) {
			aggregated.StartTime = runMetrics.StartTime
		}
		if aggregated.EndTime.IsZero() || (!runMetrics.EndTime.IsZero() && runMetrics.EndTime.After(aggregated.EndTime)) {
			aggregated.EndTime = runMetrics.EndTime
		}
	}

	// Only display if we found gateway metrics
	if runCount == 0 || len(aggregated.Servers) == 0 {
		return
	}

	// Recalculate averages for aggregated data
	calculateGatewayAggregates(aggregated)

	// Display the aggregated metrics
	if metricsOutput := renderGatewayMetricsTable(aggregated, verbose); metricsOutput != "" {
		fmt.Fprint(os.Stderr, metricsOutput)
		if runCount > 1 {
			fmt.Fprintf(os.Stderr, "\n%s\n",
				console.FormatInfoMessage(fmt.Sprintf("Gateway metrics aggregated from %d runs", runCount)))
		}
	}
}
