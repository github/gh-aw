// This file implements unified timeline merging for MCP Gateway, AWF firewall, and agent
// JSONL logs.
//
// All three systems emit JSONL logs during an agentic workflow run:
//   - MCP Gateway:  gateway.jsonl (or rpc-messages.jsonl as fallback)
//   - AWF Firewall: audit.jsonl
//   - Agent:        events.jsonl  (Copilot CLI session events)
//
// All JSONL files are collected from a run directory, each line is converted to a
// [UnifiedTimelineEvent], and the resulting stream is sorted by wall-clock time so
// that a caller can render a single, chronologically ordered timeline that spans all
// system boundaries.

package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/fileutil"
)

// TimelineEventSource identifies which system produced a timeline event.
type TimelineEventSource string

const (
	// TimelineSourceGateway indicates the event came from the MCP Gateway (gateway.jsonl
	// or rpc-messages.jsonl).
	TimelineSourceGateway TimelineEventSource = "gateway"
	// TimelineSourceFirewall indicates the event came from the AWF firewall (audit.jsonl).
	TimelineSourceFirewall TimelineEventSource = "firewall"
	// TimelineSourceAgent indicates the event came from the agent session (events.jsonl).
	TimelineSourceAgent TimelineEventSource = "agent"
)

// TimelineEventKind classifies the type of a unified timeline event.
type TimelineEventKind string

const (
	// TimelineKindToolCall is a successful or failed MCP tool invocation.
	TimelineKindToolCall TimelineEventKind = "tool_call"
	// TimelineKindDIFCFiltered is an MCP tool call blocked by DIFC integrity/secrecy checks.
	TimelineKindDIFCFiltered TimelineEventKind = "difc_filtered"
	// TimelineKindGuardPolicyBlocked is an MCP tool call blocked by a guard policy rule.
	TimelineKindGuardPolicyBlocked TimelineEventKind = "guard_blocked"
	// TimelineKindNetworkAllowed is a network request that the AWF firewall permitted.
	TimelineKindNetworkAllowed TimelineEventKind = "net_allowed"
	// TimelineKindNetworkBlocked is a network request that the AWF firewall denied.
	TimelineKindNetworkBlocked TimelineEventKind = "net_blocked"
	// TimelineKindAgentTurn marks the start of a new conversation turn (user.message event).
	TimelineKindAgentTurn TimelineEventKind = "agent_turn"
	// TimelineKindAgentToolStart marks the beginning of an agent-initiated tool execution
	// (tool.execution_start event).
	TimelineKindAgentToolStart TimelineEventKind = "agent_tool_start"
	// TimelineKindAgentToolDone marks the completion of an agent-initiated tool execution
	// (tool.execution_complete event).
	TimelineKindAgentToolDone TimelineEventKind = "agent_tool_done"
	// TimelineKindAssistantMessage is an assistant response message (assistant.message event).
	TimelineKindAssistantMessage TimelineEventKind = "assistant_message"
	// TimelineKindReasoning is a model reasoning/thinking trace (reasoning or assistant.reasoning event).
	TimelineKindReasoning TimelineEventKind = "reasoning"
	// TimelineKindSteering is a budget or time pressure steering message injected by the AWF
	// API proxy (token_steering or timeout_steering event from api-proxy-logs/events.jsonl).
	TimelineKindSteering TimelineEventKind = "steering"
)

// UnifiedTimelineEvent represents a single event from the MCP Gateway, the AWF
// firewall, or the agent session, normalised to a common structure for merged timeline
// rendering.
//
// Gateway events populate the Server/Tool/Method/Status/Error/Duration fields.
// Firewall events populate the Host/HTTPMethod/HTTPStatus/Decision fields.
// Agent events populate the ToolName/ServerName (for tool events) or TurnIndex field.
// A subset of fields (Reason, AuthorLogin) may be set by either source.
type UnifiedTimelineEvent struct {
	Time   time.Time           // Normalised wall-clock time used for sorting
	Source TimelineEventSource // Which system produced this event
	Kind   TimelineEventKind   // Event classification

	// Gateway-specific fields (tool_call, difc_filtered, guard_blocked)
	ServerName  string  // MCP server name or server ID
	ToolName    string  // Tool name invoked
	Method      string  // JSON-RPC method (may duplicate ToolName)
	Status      string  // "success" or "error"
	Error       string  // Non-empty when Status == "error"
	Duration    float64 // Round-trip time in milliseconds (0 when unknown)
	AuthorLogin string  // GitHub login of the content author (DIFC events)

	// Firewall-specific fields (net_allowed, net_blocked)
	Host       string // Target host (domain:port)
	HTTPMethod string // HTTP method (GET, CONNECT, …)
	HTTPStatus int    // HTTP response status code
	Decision   string // Proxy decision string (e.g. TCP_TUNNEL:HIER_DIRECT)

	// Agent-specific fields (agent_turn, agent_tool_start, agent_tool_done)
	TurnIndex  int    // 1-based conversation turn number (agent_turn events)
	ToolCallID string // Opaque call ID that pairs start/done events
	Success    bool   // True when tool execution succeeded (agent_tool_done events)

	// Message content fields (agent_turn, assistant_message, reasoning)
	// MessageContent holds the first portion of the message text for display.
	MessageContent string

	// Shared fields
	Reason string // Human-readable reason or description
}

// gatewayTimestampToTime parses a gateway RFC3339/RFC3339Nano timestamp string.
// Returns (zero, false) when the string is empty or unparseable.
func gatewayTimestampToTime(ts string) (time.Time, bool) {
	if ts == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		t, err = time.Parse(time.RFC3339, ts)
	}
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// firewallTimestampToTime converts a Unix float64 timestamp (as used in audit.jsonl) to
// a time.Time. Returns the zero value when ts is non-positive.
func firewallTimestampToTime(ts float64) time.Time {
	if ts <= 0 {
		return time.Time{}
	}
	sec := int64(math.Floor(ts))
	nsec := int64((ts - float64(sec)) * 1e9)
	return time.Unix(sec, nsec).UTC()
}

// gatewayEntryToTimelineEvent converts a GatewayLogEntry to a UnifiedTimelineEvent.
// Returns (zero, false) when the entry cannot be mapped (unknown event/type combination
// or unparseable timestamp).
func gatewayEntryToTimelineEvent(entry GatewayLogEntry) (UnifiedTimelineEvent, bool) {
	t, ok := gatewayTimestampToTime(entry.Timestamp)
	if !ok {
		return UnifiedTimelineEvent{}, false
	}

	evt := UnifiedTimelineEvent{
		Time:   t,
		Source: TimelineSourceGateway,
	}

	switch entry.Type {
	case "DIFC_FILTERED":
		evt.Kind = TimelineKindDIFCFiltered
		evt.ServerName = entry.ServerID
		if evt.ServerName == "" {
			evt.ServerName = entry.ServerName
		}
		evt.ToolName = entry.ToolName
		evt.Reason = entry.Reason
		evt.AuthorLogin = entry.AuthorLogin

	case "GUARD_POLICY_BLOCKED":
		evt.Kind = TimelineKindGuardPolicyBlocked
		evt.ServerName = entry.ServerID
		if evt.ServerName == "" {
			evt.ServerName = entry.ServerName
		}
		evt.ToolName = entry.ToolName
		evt.Reason = entry.Reason
		evt.Error = entry.Message

	default:
		switch entry.Event {
		case "tool_call", "rpc_call", "request":
			evt.Kind = TimelineKindToolCall
			evt.ServerName = entry.ServerName
			evt.ToolName = entry.ToolName
			evt.Method = entry.Method
			evt.Duration = entry.Duration
			evt.Status = entry.Status
			if evt.Status == "" {
				if entry.Error != "" || entry.Level == "error" {
					evt.Status = "error"
				} else {
					evt.Status = "success"
				}
			}
			evt.Error = entry.Error
		default:
			return UnifiedTimelineEvent{}, false
		}
	}

	return evt, true
}

// rpcEntryToTimelineEvent converts an RPCMessageEntry (from rpc-messages.jsonl) to a
// UnifiedTimelineEvent. Only REQUEST (OUT) and DIFC_FILTERED entries are converted;
// all other entries return (zero, false).
func rpcEntryToTimelineEvent(entry RPCMessageEntry) (UnifiedTimelineEvent, bool) {
	t, ok := gatewayTimestampToTime(entry.Timestamp)
	if !ok {
		return UnifiedTimelineEvent{}, false
	}

	evt := UnifiedTimelineEvent{
		Time:       t,
		Source:     TimelineSourceGateway,
		ServerName: entry.ServerID,
	}

	switch entry.EffectiveType() {
	case "DIFC_FILTERED":
		evt.Kind = TimelineKindDIFCFiltered
		evt.ToolName = entry.ToolName
		evt.Reason = entry.Reason
		evt.AuthorLogin = entry.AuthorLogin

	case "REQUEST":
		if entry.Direction != "OUT" {
			return UnifiedTimelineEvent{}, false
		}
		evt.Kind = TimelineKindToolCall
		evt.Status = "initiated"
		// Extract method and tool name from the payload when possible.
		var req rpcRequestPayload
		if entry.Payload != nil {
			if err := json.Unmarshal(entry.Payload, &req); err == nil {
				evt.Method = req.Method
				if req.Method == "tools/call" {
					var params rpcToolCallParams
					if err := json.Unmarshal(req.Params, &params); err == nil {
						evt.ToolName = params.Name
					}
				}
			}
		}
		if evt.Method == "" && evt.ToolName == "" {
			return UnifiedTimelineEvent{}, false
		}

	default:
		return UnifiedTimelineEvent{}, false
	}

	return evt, true
}

// auditEntryToTimelineEvent converts an AuditLogEntry (from audit.jsonl) to a
// UnifiedTimelineEvent. Returns (zero, false) for benign Squid operational entries or
// entries with a zero timestamp.
func auditEntryToTimelineEvent(entry AuditLogEntry) (UnifiedTimelineEvent, bool) {
	t := firewallTimestampToTime(entry.Timestamp)
	if t.IsZero() {
		return UnifiedTimelineEvent{}, false
	}
	// Skip benign Squid operational entries (mirrors enrichWithPolicyRules filter).
	if entry.URL == "error:transaction-end-before-headers" {
		return UnifiedTimelineEvent{}, false
	}
	// Skip entries with no host information.
	if entry.Host == "" || entry.Host == "-" {
		return UnifiedTimelineEvent{}, false
	}

	kind := TimelineKindNetworkBlocked
	if isEntryAllowed(entry) {
		kind = TimelineKindNetworkAllowed
	}

	return UnifiedTimelineEvent{
		Time:       t,
		Source:     TimelineSourceFirewall,
		Kind:       kind,
		Host:       entry.Host,
		HTTPMethod: entry.Method,
		HTTPStatus: entry.Status,
		Decision:   entry.Decision,
	}, true
}

// findGatewayJSONLPath returns the path to the primary gateway JSONL file in logDir.
// It checks gateway.jsonl in the root, then mcp-logs/gateway.jsonl, and finally falls
// back to rpc-messages.jsonl (canonical fallback when gateway.jsonl is absent).
// Returns an empty string when no file is found.
func findGatewayJSONLPath(logDir string) string {
	// Root-level gateway.jsonl (pre-mcp-logs-subdirectory artifact layout)
	if p := filepath.Join(logDir, "gateway.jsonl"); fileutil.FileExists(p) {
		return p
	}
	// mcp-logs/ subdirectory (standard layout after artifact download)
	if p := filepath.Join(logDir, "mcp-logs", "gateway.jsonl"); fileutil.FileExists(p) {
		return p
	}
	// Fallback: rpc-messages.jsonl
	return findRPCMessagesPath(logDir)
}

// collectGatewayTimelineEvents reads the gateway JSONL file in logDir and returns a slice
// of timeline events, one per parseable, recognised log entry. The file may be
// gateway.jsonl or rpc-messages.jsonl (the latter is used as a fallback).
// Returns nil (not an error) when no file is found.
func collectGatewayTimelineEvents(logDir string, verbose bool) ([]UnifiedTimelineEvent, error) {
	path := findGatewayJSONLPath(logDir)
	if path == "" {
		gatewayLogsLog.Printf("No gateway JSONL found in %s; skipping gateway timeline collection", logDir)
		return nil, nil
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	isRPCMessages := strings.HasSuffix(filepath.Base(path), "rpc-messages.jsonl")
	gatewayLogsLog.Printf("Collecting gateway timeline events from: %s (rpc_messages=%v)", path, isRPCMessages)

	var events []UnifiedTimelineEvent
	scanner := bufio.NewScanner(file)
	buf := make([]byte, maxScannerBufferSize)
	scanner.Buffer(buf, maxScannerBufferSize)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if isRPCMessages {
			var entry RPCMessageEntry
			if err := json.Unmarshal([]byte(line), &entry); err != nil {
				gatewayLogsLog.Printf("Skipping malformed rpc-messages line: %v", err)
				continue
			}
			if evt, ok := rpcEntryToTimelineEvent(entry); ok {
				events = append(events, evt)
			}
		} else {
			var entry GatewayLogEntry
			if err := json.Unmarshal([]byte(line), &entry); err != nil {
				gatewayLogsLog.Printf("Skipping malformed gateway.jsonl line: %v", err)
				continue
			}
			if evt, ok := gatewayEntryToTimelineEvent(entry); ok {
				events = append(events, evt)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	gatewayLogsLog.Printf("Collected %d gateway timeline events from %s", len(events), filepath.Base(path))
	return events, nil
}

// collectFirewallTimelineEvents reads audit.jsonl from logDir (using the same discovery
// logic as detectFirewallAuditArtifacts) and returns a slice of timeline events.
// Returns nil (not an error) when no file is found.
func collectFirewallTimelineEvents(logDir string, verbose bool) ([]UnifiedTimelineEvent, error) {
	_, auditJSONLPath, err := detectFirewallAuditArtifacts(logDir)
	if err != nil {
		return nil, err
	}
	if auditJSONLPath == "" {
		gatewayLogsLog.Printf("No audit.jsonl found in %s; skipping firewall timeline collection", logDir)
		return nil, nil
	}

	gatewayLogsLog.Printf("Collecting firewall timeline events from: %s", auditJSONLPath)

	entries, err := parseAuditJSONL(auditJSONLPath)
	if err != nil {
		return nil, err
	}

	var events []UnifiedTimelineEvent
	for _, entry := range entries {
		if evt, ok := auditEntryToTimelineEvent(entry); ok {
			events = append(events, evt)
		}
	}

	gatewayLogsLog.Printf("Collected %d firewall timeline events from %s", len(events), filepath.Base(auditJSONLPath))
	return events, nil
}

// parseEventsJSONL reads a Copilot events.jsonl file and returns the raw entries in the
// order they appear in the file.  Malformed lines are silently skipped.
func parseEventsJSONL(path string) ([]copilotEventsJSONLEntry, error) {
	cleanPath := filepath.Clean(path)
	f, err := os.Open(cleanPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open events.jsonl: %w", err)
	}
	defer f.Close()

	var entries []copilotEventsJSONLEntry
	scanner := bufio.NewScanner(f)
	buf := make([]byte, maxScannerBufferSize)
	scanner.Buffer(buf, maxScannerBufferSize)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || !strings.HasPrefix(line, "{") {
			continue
		}
		var entry copilotEventsJSONLEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			gatewayLogsLog.Printf("Skipping malformed events.jsonl line: %v", err)
			continue
		}
		entries = append(entries, entry)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scanner error reading events.jsonl: %w", err)
	}
	return entries, nil
}

// agentEntryToTimelineEvent converts a single agent copilotEventsJSONLEntry into a
// UnifiedTimelineEvent.  Only event types that are meaningful at the timeline level
// (user.message, assistant.message, reasoning, assistant.reasoning,
// tool.execution_start, tool.execution_complete) are converted; all other types are
// silently skipped (ok == false).
func agentEntryToTimelineEvent(entry copilotEventsJSONLEntry, turnIndex int) (UnifiedTimelineEvent, bool) {
	t, ok := gatewayTimestampToTime(entry.Timestamp)
	if !ok {
		return UnifiedTimelineEvent{}, false
	}

	switch entry.Type {
	case "user.message":
		return UnifiedTimelineEvent{
			Time:           t,
			Source:         TimelineSourceAgent,
			Kind:           TimelineKindAgentTurn,
			TurnIndex:      turnIndex,
			MessageContent: entry.Data.Content,
		}, true

	case "assistant.message":
		return UnifiedTimelineEvent{
			Time:           t,
			Source:         TimelineSourceAgent,
			Kind:           TimelineKindAssistantMessage,
			MessageContent: entry.Data.Content,
		}, true

	case "reasoning", "assistant.reasoning":
		return UnifiedTimelineEvent{
			Time:           t,
			Source:         TimelineSourceAgent,
			Kind:           TimelineKindReasoning,
			MessageContent: entry.Data.Content,
		}, true

	case "tool.execution_start":
		return UnifiedTimelineEvent{
			Time:       t,
			Source:     TimelineSourceAgent,
			Kind:       TimelineKindAgentToolStart,
			ToolName:   entry.Data.ToolName,
			ServerName: entry.Data.MCPServerName,
			ToolCallID: entry.Data.ToolCallID,
		}, true

	case "tool.execution_complete":
		status := "success"
		if !entry.Data.Success {
			status = "error"
		}
		return UnifiedTimelineEvent{
			Time:       t,
			Source:     TimelineSourceAgent,
			Kind:       TimelineKindAgentToolDone,
			ToolName:   entry.Data.ToolName,
			ServerName: entry.Data.MCPServerName,
			ToolCallID: entry.Data.ToolCallID,
			Success:    entry.Data.Success,
			Status:     status,
		}, true

	default:
		return UnifiedTimelineEvent{}, false
	}
}

// collectAgentTimelineEvents reads events.jsonl from the agent session directory inside
// logDir and returns a slice of timeline events.  Returns nil (not an error) when no
// file is found.
func collectAgentTimelineEvents(logDir string, verbose bool) ([]UnifiedTimelineEvent, error) {
	eventsPath := findEventsJSONLFile(logDir)
	if eventsPath == "" {
		gatewayLogsLog.Printf("No events.jsonl found in %s; skipping agent timeline collection", logDir)
		return nil, nil
	}

	gatewayLogsLog.Printf("Collecting agent timeline events from: %s", eventsPath)

	entries, err := parseEventsJSONL(eventsPath)
	if err != nil {
		return nil, err
	}

	var events []UnifiedTimelineEvent
	turnIndex := 0
	for _, entry := range entries {
		if entry.Type == "user.message" {
			turnIndex++
		}
		if evt, ok := agentEntryToTimelineEvent(entry, turnIndex); ok {
			events = append(events, evt)
		}
	}

	gatewayLogsLog.Printf("Collected %d agent timeline events from %s", len(events), filepath.Base(eventsPath))
	return events, nil
}

// steeringEntryToTimelineEvent converts a proxyEventsEntry into a
// UnifiedTimelineEvent with Kind == TimelineKindSteering.
// Returns (zero, false) when the entry is not a recognised steering event.
//
// The Status field is set to "token" for token_steering events and "time" for
// timeout_steering events so that the renderer can apply appropriate icons.
// The Reason field carries the full message text.
// The Time field is set from the Timestamp field when present; zero otherwise.
func steeringEntryToTimelineEvent(entry proxyEventsEntry) (UnifiedTimelineEvent, bool) {
	name := entry.eventName()
	msg := strings.TrimSpace(entry.Message)
	if !isSteeringEvent(name, msg) {
		return UnifiedTimelineEvent{}, false
	}

	var status string
	switch name {
	case tokenSteeringEventName:
		status = "token"
	case timeoutSteeringEventName:
		status = "time"
	}

	var t time.Time
	if entry.Timestamp != "" {
		if parsed, ok := gatewayTimestampToTime(entry.Timestamp); ok {
			t = parsed
		}
	}

	return UnifiedTimelineEvent{
		Time:   t,
		Source: TimelineSourceFirewall,
		Kind:   TimelineKindSteering,
		Status: status,
		Reason: msg,
	}, true
}

// collectSteeringTimelineEvents reads the api-proxy events.jsonl from logDir and
// returns a slice of TimelineKindSteering timeline events, one per recognised steering
// record.  Returns nil (not an error) when no proxy events file is found.
func collectSteeringTimelineEvents(logDir string, verbose bool) ([]UnifiedTimelineEvent, error) {
	log, err := findAPIProxyEventsLog(logDir)
	if err != nil {
		return nil, fmt.Errorf("failed to parse proxy events in %s: %w", logDir, err)
	}
	if log == nil {
		gatewayLogsLog.Printf("No api-proxy events.jsonl found in %s; skipping steering timeline collection", logDir)
		return nil, nil
	}

	gatewayLogsLog.Printf("Collecting steering timeline events from: %s", log.path)

	events := make([]UnifiedTimelineEvent, 0, len(log.entries))
	for _, entry := range log.entries {
		if evt, ok := steeringEntryToTimelineEvent(entry); ok {
			events = append(events, evt)
		}
	}

	gatewayLogsLog.Printf("Collected %d steering timeline events from %s", len(events), filepath.Base(log.path))
	return events, nil
}

// BuildUnifiedTimeline collects all JSONL events from the MCP Gateway, the AWF
// firewall, the agent session, and the AWF API proxy in logDir, merges them into a
// single slice, and sorts the slice in ascending wall-clock order (oldest first).
//
// If a source is unavailable (no matching file), it is silently skipped; collection
// errors are logged but do not prevent events from the other sources from being returned.
func BuildUnifiedTimeline(logDir string, verbose bool) ([]UnifiedTimelineEvent, error) {
	gatewayEvents, gwErr := collectGatewayTimelineEvents(logDir, verbose)
	if gwErr != nil {
		gatewayLogsLog.Printf("collectGatewayTimelineEvents error: %v", gwErr)
	}

	firewallEvents, fwErr := collectFirewallTimelineEvents(logDir, verbose)
	if fwErr != nil {
		gatewayLogsLog.Printf("collectFirewallTimelineEvents error: %v", fwErr)
	}

	agentEvents, agErr := collectAgentTimelineEvents(logDir, verbose)
	if agErr != nil {
		gatewayLogsLog.Printf("collectAgentTimelineEvents error: %v", agErr)
	}

	steeringEvents, stErr := collectSteeringTimelineEvents(logDir, verbose)
	if stErr != nil {
		gatewayLogsLog.Printf("collectSteeringTimelineEvents error: %v", stErr)
	}

	events := make([]UnifiedTimelineEvent, 0,
		len(gatewayEvents)+len(firewallEvents)+len(agentEvents)+len(steeringEvents))
	events = append(events, gatewayEvents...)
	events = append(events, firewallEvents...)
	events = append(events, agentEvents...)
	events = append(events, steeringEvents...)

	slices.SortFunc(events, func(a, b UnifiedTimelineEvent) int {
		switch {
		case a.Time.Before(b.Time):
			return -1
		case b.Time.Before(a.Time):
			return 1
		default:
			return 0
		}
	})

	gatewayLogsLog.Printf("Built unified timeline: %d events (gateway=%d, firewall=%d, agent=%d, steering=%d)",
		len(events), len(gatewayEvents), len(firewallEvents), len(agentEvents), len(steeringEvents))

	return events, nil
}
