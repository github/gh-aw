// This file contains RPC message parsing functions for MCP gateway log analysis.
// It handles rpc-messages.jsonl (canonical fallback when gateway.jsonl is absent).

package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/setutil"
	"github.com/github/gh-aw/pkg/timeutil"
)

// parseRPCMessages parses a rpc-messages.jsonl file and extracts GatewayMetrics.
// This is the canonical fallback when gateway.jsonl is not available.
func parseRPCMessages(logPath string, verbose bool) (*GatewayMetrics, error) {
	gatewayLogsLog.Printf("Parsing rpc-messages.jsonl from: %s", logPath)

	file, err := os.Open(logPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open rpc-messages.jsonl: %w", err)
	}
	defer file.Close()

	metrics := &GatewayMetrics{
		Servers: make(map[string]*GatewayServerMetrics),
	}

	// Track pending requests by (serverID, id) for duration calculation.
	// Key format: "<serverID>/<id>"
	pendingRequests := make(map[string]*rpcPendingRequest)

	scanner := bufio.NewScanner(file)
	// Increase scanner buffer for large payloads
	buf := make([]byte, maxScannerBufferSize)
	scanner.Buffer(buf, maxScannerBufferSize)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var entry RPCMessageEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			gatewayLogsLog.Printf("Failed to parse rpc-messages.jsonl line %d: %v", lineNum, err)
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(
					fmt.Sprintf("Failed to parse rpc-messages.jsonl line %d: %v", lineNum, err)))
			}
			continue
		}

		// Update time range
		if entry.Timestamp != "" {
			if t, err := time.Parse(time.RFC3339Nano, entry.Timestamp); err == nil {
				if metrics.StartTime.IsZero() || t.Before(metrics.StartTime) {
					metrics.StartTime = t
				}
				if metrics.EndTime.IsZero() || t.After(metrics.EndTime) {
					metrics.EndTime = t
				}
			}
		}

		if entry.ServerID == "" {
			continue
		}

		switch {
		case entry.EffectiveType() == "DIFC_FILTERED":
			// DIFC integrity/secrecy filter event — not a REQUEST or RESPONSE
			metrics.TotalFiltered++
			server := getOrCreateServer(metrics, entry.ServerID)
			server.FilteredCount++
			metrics.FilteredEvents = append(metrics.FilteredEvents, DifcFilteredEvent{
				Timestamp:         entry.Timestamp,
				ServerID:          entry.ServerID,
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

		case entry.Direction == "OUT" && entry.EffectiveType() == "REQUEST":
			// Outgoing request from AI engine to MCP server
			var req rpcRequestPayload
			if err := json.Unmarshal(entry.Payload, &req); err != nil {
				continue
			}
			if req.Method != "tools/call" {
				continue
			}

			// Extract tool name
			var params rpcToolCallParams
			if err := json.Unmarshal(req.Params, &params); err != nil || params.Name == "" {
				continue
			}

			metrics.TotalRequests++
			server := getOrCreateServer(metrics, entry.ServerID)
			server.RequestCount++
			metrics.TotalToolCalls++
			server.ToolCallCount++

			tool := getOrCreateTool(server, params.Name)
			tool.CallCount++

			// Store pending request for duration calculation
			if req.ID != nil && entry.Timestamp != "" {
				if t, err := time.Parse(time.RFC3339Nano, entry.Timestamp); err == nil {
					key := fmt.Sprintf("%s/%v", entry.ServerID, req.ID)
					pendingRequests[key] = &rpcPendingRequest{
						ServerID:  entry.ServerID,
						ToolName:  params.Name,
						Timestamp: t,
					}
				}
			}

		case entry.Direction == "IN" && entry.EffectiveType() == "RESPONSE":
			// Incoming response from MCP server to AI engine
			var resp rpcResponsePayload
			if err := json.Unmarshal(entry.Payload, &resp); err != nil {
				continue
			}

			// Track errors and detect guard policy blocks
			if resp.Error != nil {
				metrics.TotalErrors++
				server := getOrCreateServer(metrics, entry.ServerID)
				server.ErrorCount++

				// Detect guard policy enforcement errors
				if isGuardPolicyErrorCode(resp.Error.Code) {
					metrics.TotalGuardBlocked++
					server.GuardPolicyBlocked++

					// Determine tool name from pending request if available
					toolName := ""
					if resp.ID != nil {
						key := fmt.Sprintf("%s/%v", entry.ServerID, resp.ID)
						if pending, ok := pendingRequests[key]; ok {
							toolName = pending.ToolName
						}
					}

					reason := guardPolicyReasonFromCode(resp.Error.Code)
					if resp.Error.Data != nil && resp.Error.Data.Reason != "" {
						reason = resp.Error.Data.Reason
					}

					evt := GuardPolicyEvent{
						Timestamp: entry.Timestamp,
						ServerID:  entry.ServerID,
						ToolName:  toolName,
						ErrorCode: resp.Error.Code,
						Reason:    reason,
						Message:   resp.Error.Message,
					}
					if resp.Error.Data != nil {
						evt.Details = resp.Error.Data.Details
						evt.Repository = resp.Error.Data.Repository
					}
					metrics.GuardPolicyEvents = append(metrics.GuardPolicyEvents, evt)
				}
			}

			// Calculate duration by matching with pending request
			if resp.ID != nil && entry.Timestamp != "" {
				key := fmt.Sprintf("%s/%v", entry.ServerID, resp.ID)
				if pending, ok := pendingRequests[key]; ok {
					delete(pendingRequests, key)
					if t, err := time.Parse(time.RFC3339Nano, entry.Timestamp); err == nil {
						durationMs := float64(t.Sub(pending.Timestamp).Milliseconds())
						if durationMs >= 0 {
							server := getOrCreateServer(metrics, entry.ServerID)
							server.TotalDuration += durationMs
							metrics.TotalDuration += durationMs

							tool := getOrCreateTool(server, pending.ToolName)
							tool.TotalDuration += durationMs
							if tool.MaxDuration == 0 || durationMs > tool.MaxDuration {
								tool.MaxDuration = durationMs
							}
							if tool.MinDuration == 0 || durationMs < tool.MinDuration {
								tool.MinDuration = durationMs
							}

							if resp.Error != nil {
								tool.ErrorCount++
							}
						}
					}
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading rpc-messages.jsonl: %w", err)
	}

	calculateGatewayAggregates(metrics)

	gatewayLogsLog.Printf("Successfully parsed rpc-messages.jsonl: %d servers, %d total requests",
		len(metrics.Servers), metrics.TotalRequests)

	return metrics, nil
}

// findRPCMessagesPath returns the path to rpc-messages.jsonl if it exists, or "" if not found.
func findRPCMessagesPath(logDir string) string {
	// Check mcp-logs subdirectory (standard location)
	mcpLogsPath := filepath.Join(logDir, "mcp-logs", "rpc-messages.jsonl")
	if _, err := os.Stat(mcpLogsPath); err == nil {
		return mcpLogsPath
	}
	// Check root directory as fallback
	rootPath := filepath.Join(logDir, "rpc-messages.jsonl")
	if _, err := os.Stat(rootPath); err == nil {
		return rootPath
	}
	return ""
}

// buildToolCallsFromRPCMessages reads rpc-messages.jsonl and builds MCPToolCall records.
// Duration is computed by pairing outgoing requests with incoming responses.
// Input/output sizes are not available in rpc-messages.jsonl and will be 0.
func buildToolCallsFromRPCMessages(logPath string) ([]MCPToolCall, error) {
	file, err := os.Open(logPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open rpc-messages.jsonl: %w", err)
	}
	defer file.Close()

	type pendingCall struct {
		serverID  string
		toolName  string
		timestamp time.Time
	}
	pending := make(map[string]*pendingCall) // key: "<serverID>/<id>"

	// Collect requests first to pair with responses
	type rawEntry struct {
		entry RPCMessageEntry
		req   rpcRequestPayload
		resp  rpcResponsePayload
		valid bool
	}
	var entries []rawEntry

	scanner := bufio.NewScanner(file)
	buf := make([]byte, maxScannerBufferSize)
	scanner.Buffer(buf, maxScannerBufferSize)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var e RPCMessageEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue
		}
		entries = append(entries, rawEntry{entry: e, valid: true})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading rpc-messages.jsonl: %w", err)
	}

	// Second pass: build MCPToolCall records.
	// Declared before first pass so requests without IDs can be appended immediately.
	var toolCalls []MCPToolCall
	processedKeys := make(map[string]struct {
	})

	// First pass: index outgoing tool-call requests by (serverID, id)
	for i := range entries {
		e := &entries[i]
		if e.entry.Direction != "OUT" || e.entry.EffectiveType() != "REQUEST" {
			continue
		}
		if err := json.Unmarshal(e.entry.Payload, &e.req); err != nil || e.req.Method != "tools/call" {
			continue
		}
		var params rpcToolCallParams
		if err := json.Unmarshal(e.req.Params, &params); err != nil || params.Name == "" {
			continue
		}
		if e.req.ID == nil {
			// Requests without an ID cannot be matched to responses.
			// Emit the tool call immediately with "unknown" status so it appears
			// in the tool_calls list (same as parseRPCMessages counts it in the summary).
			toolCalls = append(toolCalls, MCPToolCall{
				Timestamp:  e.entry.Timestamp,
				ServerName: e.entry.ServerID,
				ToolName:   params.Name,
				Status:     "unknown",
			})
			continue
		}
		t, err := time.Parse(time.RFC3339Nano, e.entry.Timestamp)
		if err != nil {
			continue
		}
		key := fmt.Sprintf("%s/%v", e.entry.ServerID, e.req.ID)
		pending[key] = &pendingCall{
			serverID:  e.entry.ServerID,
			toolName:  params.Name,
			timestamp: t,
		}
	}

	// Second pass: pair responses with pending requests to compute durations
	for i := range entries {
		e := &entries[i]
		switch {
		case e.entry.Direction == "OUT" && e.entry.EffectiveType() == "REQUEST":
			// Outgoing tool-call request – we'll emit the record when we see the response
			// (or after if no response found)
		case e.entry.Direction == "IN" && e.entry.EffectiveType() == "RESPONSE":
			if err := json.Unmarshal(e.entry.Payload, &e.resp); err != nil {
				continue
			}
			if e.resp.ID == nil {
				continue
			}
			key := fmt.Sprintf("%s/%v", e.entry.ServerID, e.resp.ID)
			p, ok := pending[key]
			if !ok {
				continue
			}
			processedKeys[key] = struct {
			}{}

			call := MCPToolCall{
				Timestamp:  p.timestamp.Format(time.RFC3339Nano),
				ServerName: p.serverID,
				ToolName:   p.toolName,
				Status:     "success",
			}
			if e.resp.Error != nil {
				call.Status = "error"
				call.Error = e.resp.Error.Message
			}
			if t, err := time.Parse(time.RFC3339Nano, e.entry.Timestamp); err == nil {
				d := t.Sub(p.timestamp)
				if d >= 0 {
					call.Duration = timeutil.FormatDuration(d)
				}
			}
			toolCalls = append(toolCalls, call)
		}
	}

	// Emit any requests that never received a response
	for key, p := range pending {
		if !setutil.Contains(processedKeys, key) {
			toolCalls = append(toolCalls, MCPToolCall{
				Timestamp:  p.timestamp.Format(time.RFC3339Nano),
				ServerName: p.serverID,
				ToolName:   p.toolName,
				Status:     "unknown",
			})
		}
	}

	return toolCalls, nil
}
