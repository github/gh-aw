// This file contains the extractMCPToolUsageData function for MCP gateway log analysis.
// It orchestrates gateway/rpc-messages log parsing to produce MCPToolUsageData.

package cli

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/errorutil"
	"github.com/github/gh-aw/pkg/timeutil"
)

// extractMCPToolUsageData creates detailed MCP tool usage data from gateway metrics
func extractMCPToolUsageData(logDir string, verbose bool) (*MCPToolUsageData, error) {
	gatewayLogsLog.Printf("Extracting MCP tool usage data from: %s", logDir)

	// Parse gateway logs (falls back to rpc-messages.jsonl automatically)
	gatewayMetrics, err := parseGatewayLogs(logDir, verbose)
	if err != nil {
		// Return nil if no log file exists (not an error for workflows without MCP)
		if errorutil.IsNotFoundError(err) {
			gatewayLogsLog.Print("No gateway log file found, skipping MCP tool usage extraction")
			return nil, nil
		}
		return nil, fmt.Errorf("could not parse gateway logs; ensure the gateway log files are valid JSONL and not corrupted, then retry: %w", err)
	}

	if gatewayMetrics == nil || len(gatewayMetrics.Servers) == 0 {
		gatewayLogsLog.Print("No gateway metrics or servers found")
		return nil, nil
	}
	gatewayLogsLog.Printf("Found gateway metrics: %d servers", len(gatewayMetrics.Servers))

	mcpData := &MCPToolUsageData{
		Summary:        []MCPToolSummary{},
		ToolCalls:      []MCPToolCall{},
		Servers:        []MCPServerStats{},
		FilteredEvents: gatewayMetrics.FilteredEvents,
	}

	// Build guard policy summary if there are guard policy events
	if len(gatewayMetrics.GuardPolicyEvents) > 0 {
		mcpData.GuardPolicySummary = buildGuardPolicySummary(gatewayMetrics)
	}

	// Read the log file again to get individual tool call records.
	// Prefer gateway.jsonl; fall back to rpc-messages.jsonl when not available.
	gatewayLogPath := filepath.Join(logDir, "gateway.jsonl")
	usingRPCMessages := false

	if _, err := os.Stat(gatewayLogPath); os.IsNotExist(err) {
		mcpLogsPath := filepath.Join(logDir, "mcp-logs", "gateway.jsonl")
		if _, err := os.Stat(mcpLogsPath); os.IsNotExist(err) {
			// Fall back to rpc-messages.jsonl
			rpcPath := findRPCMessagesPath(logDir)
			if rpcPath == "" {
				return nil, errors.New("gateway.jsonl not found")
			}
			gatewayLogPath = rpcPath
			usingRPCMessages = true
		} else {
			gatewayLogPath = mcpLogsPath
		}
	}

	if usingRPCMessages {
		gatewayLogsLog.Printf("Reading tool calls from rpc-messages.jsonl: %s", gatewayLogPath)
		// Build tool call records from rpc-messages.jsonl
		toolCalls, err := buildToolCallsFromRPCMessages(gatewayLogPath)
		if err != nil {
			return nil, fmt.Errorf("could not read rpc-messages.jsonl; ensure the file exists and contains valid JSONL records, then retry: %w", err)
		}
		mcpData.ToolCalls = toolCalls
		gatewayLogsLog.Printf("Loaded %d tool calls from rpc-messages.jsonl", len(toolCalls))
	} else {
		gatewayLogsLog.Printf("Reading tool calls from gateway.jsonl: %s", gatewayLogPath)
		if err := extractToolCallsFromGatewayLog(gatewayLogPath, mcpData); err != nil {
			return nil, err
		}
		gatewayLogsLog.Printf("Loaded %d tool calls from gateway.jsonl", len(mcpData.ToolCalls))
	}

	// Build summary statistics from aggregated metrics
	buildMCPSummaryStats(gatewayMetrics, mcpData)
	gatewayLogsLog.Printf("Built MCP summary: %d tool summaries, %d server stats", len(mcpData.Summary), len(mcpData.Servers))

	return mcpData, nil
}

// extractToolCallsFromGatewayLog reads gateway.jsonl and appends tool call records to mcpData.
func extractToolCallsFromGatewayLog(gatewayLogPath string, mcpData *MCPToolUsageData) error {
	file, err := os.Open(gatewayLogPath)
	if err != nil {
		return fmt.Errorf("could not open gateway.jsonl; ensure required prerequisites are configured, then retry: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	buf := make([]byte, maxScannerBufferSize)
	scanner.Buffer(buf, maxScannerBufferSize)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var entry GatewayLogEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue // Skip malformed lines
		}

		// Only process actual tool invocations, not protocol requests such as tools/list.
		if entry.Event == "tool_call" || entry.Method == "tools/call" {
			toolName := entry.ToolName
			if toolName == "" {
				toolName = entry.Method
			}

			// Skip entries without tool information
			if entry.ServerName == "" || toolName == "" {
				continue
			}

			// Derive status from available fields when not explicitly set.
			// Post-OTel-collector migrations may omit the "status" string field,
			// relying instead on "error" or "level" to signal failures.
			status := entry.Status
			if status == "" {
				if entry.Error != "" || entry.Level == "error" {
					status = "error"
				} else {
					status = "success"
				}
			}

			// Create individual tool call record
			toolCall := MCPToolCall{
				Timestamp:  entry.Timestamp,
				ServerName: entry.ServerName,
				ToolName:   toolName,
				Method:     entry.Method,
				InputSize:  entry.InputSize,
				OutputSize: entry.OutputSize,
				Status:     status,
				Error:      entry.Error,
			}

			if entry.Duration > 0 {
				toolCall.Duration = timeutil.FormatDuration(time.Duration(entry.Duration * float64(time.Millisecond)))
			}

			mcpData.ToolCalls = append(mcpData.ToolCalls, toolCall)
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading gateway.jsonl: %w", err)
	}
	return nil
}

// buildMCPSummaryStats populates mcpData.Summary and mcpData.Servers from aggregated gateway metrics.
func buildMCPSummaryStats(gatewayMetrics *GatewayMetrics, mcpData *MCPToolUsageData) {
	for serverName, serverMetrics := range gatewayMetrics.Servers {
		// Server-level stats
		serverStats := MCPServerStats{
			MCPServerStatsBase: MCPServerStatsBase{
				ServerName:    serverName,
				ToolCallCount: serverMetrics.ToolCallCount,
				ErrorCount:    serverMetrics.ErrorCount,
			},
			RequestCount:    serverMetrics.RequestCount,
			TotalInputSize:  0,
			TotalOutputSize: 0,
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
			summary.syncBaseFromFields()

			mcpData.Summary = append(mcpData.Summary, summary)

			// Update server totals
			serverStats.TotalInputSize += toolMetrics.TotalInputSize
			serverStats.TotalOutputSize += toolMetrics.TotalOutputSize
		}

		mcpData.Servers = append(mcpData.Servers, serverStats)
	}

	// Sort summaries by server name, then tool name
	slices.SortFunc(mcpData.Summary, func(a, b MCPToolSummary) int {
		if a.ServerName != b.ServerName {
			if a.ServerName < b.ServerName {
				return -1
			}
			return 1
		}
		switch {
		case a.ToolName < b.ToolName:
			return -1
		case a.ToolName > b.ToolName:
			return 1
		default:
			return 0
		}
	})

	// Sort servers by name
	slices.SortFunc(mcpData.Servers, func(a, b MCPServerStats) int {
		switch {
		case a.ServerName < b.ServerName:
			return -1
		case a.ServerName > b.ServerName:
			return 1
		default:
			return 0
		}
	})
}
