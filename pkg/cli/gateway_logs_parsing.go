// This file contains gateway.jsonl parsing functions for MCP gateway log analysis.

package cli

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/console"
)

// parseGatewayLogs parses a gateway.jsonl file and extracts metrics.
// Falls back to rpc-messages.jsonl (canonical fallback) when gateway.jsonl is not present.
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
			// Fall back to rpc-messages.jsonl (canonical fallback when gateway.jsonl is missing)
			rpcPath := findRPCMessagesPath(logDir)
			if rpcPath != "" {
				gatewayLogsLog.Printf("gateway.jsonl not found; falling back to rpc-messages.jsonl: %s", rpcPath)
				return parseRPCMessages(rpcPath, verbose)
			}
			gatewayLogsLog.Printf("gateway.jsonl not found at: %s or %s", gatewayLogPath, mcpLogsPath)
			return nil, errors.New("gateway.jsonl not found")
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
	buf := make([]byte, maxScannerBufferSize)
	scanner.Buffer(buf, maxScannerBufferSize)
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
	// Parse timestamp for time range (supports both RFC3339 and RFC3339Nano)
	if entry.Timestamp != "" {
		t, err := time.Parse(time.RFC3339Nano, entry.Timestamp)
		if err != nil {
			t, err = time.Parse(time.RFC3339, entry.Timestamp)
		}
		if err == nil {
			if metrics.StartTime.IsZero() || t.Before(metrics.StartTime) {
				metrics.StartTime = t
			}
			if metrics.EndTime.IsZero() || t.After(metrics.EndTime) {
				metrics.EndTime = t
			}
		}
	}

	// Handle DIFC_FILTERED events
	if entry.Type == "DIFC_FILTERED" {
		metrics.TotalFiltered++
		// DIFC_FILTERED events use server_id; fall back to server_name for compatibility
		serverKey := entry.ServerID
		if serverKey == "" {
			serverKey = entry.ServerName
		}
		if serverKey != "" {
			server := getOrCreateServer(metrics, serverKey)
			server.FilteredCount++
		}
		metrics.FilteredEvents = append(metrics.FilteredEvents, DifcFilteredEvent{
			Timestamp:         entry.Timestamp,
			ServerID:          serverKey,
			ToolName:          entry.ToolName,
			Description:       entry.Description,
			Reason:            entry.Reason,
			SecrecyTags:       entry.SecrecyTags,
			IntegrityTags:     entry.IntegrityTags,
			AuthorAssociation: entry.AuthorAssociation,
			AuthorLogin:       entry.AuthorLogin,
			HTMLURL:           entry.HTMLURL,
			Number:            entry.Number,
		})
		return
	}

	// Handle GUARD_POLICY_BLOCKED events from gateway.jsonl
	if entry.Type == "GUARD_POLICY_BLOCKED" {
		metrics.TotalGuardBlocked++
		serverKey := entry.ServerID
		if serverKey == "" {
			serverKey = entry.ServerName
		}
		if serverKey != "" {
			server := getOrCreateServer(metrics, serverKey)
			server.GuardPolicyBlocked++
		}
		metrics.GuardPolicyEvents = append(metrics.GuardPolicyEvents, GuardPolicyEvent{
			Timestamp: entry.Timestamp,
			ServerID:  serverKey,
			ToolName:  entry.ToolName,
			Reason:    entry.Reason,
			Message:   entry.Message,
			Details:   entry.Description,
		})
		return
	}

	// Track errors. Include entries where level is "error" to handle gateway log formats
	// that omit the "status" field post-OTel-collector migration.
	if entry.Status == "error" || entry.Error != "" || entry.Level == "error" {
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

			// Track only actual tool invocations, not protocol requests such as tools/list.
			if entry.Event == "tool_call" || entry.Method == "tools/call" {
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
