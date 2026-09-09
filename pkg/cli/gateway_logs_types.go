// This file contains type definitions and constants for MCP gateway log parsing.

package cli

import (
	"encoding/json"
	"time"

	"github.com/github/gh-aw/pkg/logger"
)

var gatewayLogsLog = logger.New("cli:gateway_logs")

// maxScannerBufferSize is the maximum scanner buffer for large JSONL payloads (1 MB).
const maxScannerBufferSize = 1024 * 1024

// GatewayLogEntry represents a single log entry from gateway.jsonl
type GatewayLogEntry struct {
	Timestamp         string   `json:"timestamp"`
	Level             string   `json:"level"`
	Type              string   `json:"type"`
	Event             string   `json:"event"`
	ServerName        string   `json:"server_name,omitempty"`
	ServerID          string   `json:"server_id,omitempty"` // used by DIFC_FILTERED events
	ToolName          string   `json:"tool_name,omitempty"`
	Method            string   `json:"method,omitempty"`
	Duration          float64  `json:"duration,omitempty"` // in milliseconds
	InputSize         int      `json:"input_size,omitempty"`
	OutputSize        int      `json:"output_size,omitempty"`
	Status            string   `json:"status,omitempty"`
	Error             string   `json:"error,omitempty"`
	Message           string   `json:"message,omitempty"`
	Description       string   `json:"description,omitempty"`
	Reason            string   `json:"reason,omitempty"`
	SecrecyTags       []string `json:"secrecy_tags,omitempty"`
	IntegrityTags     []string `json:"integrity_tags,omitempty"`
	AuthorAssociation string   `json:"author_association,omitempty"`
	AuthorLogin       string   `json:"author_login,omitempty"`
	HTMLURL           string   `json:"html_url,omitempty"`
	Number            string   `json:"number,omitempty"`
}

// DifcFilteredEvent represents a DIFC_FILTERED log entry from gateway.jsonl.
// These events occur when a tool call is blocked by DIFC integrity or secrecy checks.
type DifcFilteredEvent struct {
	Timestamp         string   `json:"timestamp"`
	ServerID          string   `json:"server_id"`
	ToolName          string   `json:"tool_name"`
	Description       string   `json:"description,omitempty"`
	Reason            string   `json:"reason"`
	SecrecyTags       []string `json:"secrecy_tags,omitempty"`
	IntegrityTags     []string `json:"integrity_tags,omitempty"`
	AuthorAssociation string   `json:"author_association,omitempty"`
	AuthorLogin       string   `json:"author_login,omitempty"`
	HTMLURL           string   `json:"html_url,omitempty"`
	Number            string   `json:"number,omitempty"`
}

// Guard policy error codes from MCP Gateway.
// These JSON-RPC error codes indicate guard policy enforcement decisions.
const (
	guardPolicyErrorCodeAccessDenied      = -32001 // General access denied
	guardPolicyErrorCodeRepoNotAllowed    = -32002 // Repository not in allowlist (repos)
	guardPolicyErrorCodeInsufficientPerms = -32003 // Insufficient permissions (roles)
	guardPolicyErrorCodePrivateRepoDenied = -32004 // Private repository access denied
	guardPolicyErrorCodeBlockedUser       = -32005 // Content from blocked user
	guardPolicyErrorCodeIntegrityBelowMin = -32006 // Content integrity below minimum threshold (min-integrity)
)

// GuardPolicyEvent represents a guard policy enforcement decision from the MCP Gateway.
// These events are extracted from JSON-RPC error responses with specific error codes
// (-32001 to -32006) in rpc-messages.jsonl.
type GuardPolicyEvent struct {
	Timestamp  string `json:"timestamp"`
	ServerID   string `json:"server_id"`
	ToolName   string `json:"tool_name"`
	ErrorCode  int    `json:"error_code"`
	Reason     string `json:"reason"`               // e.g., "repository_not_allowed", "min_integrity"
	Message    string `json:"message"`              // Error message from JSON-RPC response
	Details    string `json:"details,omitempty"`    // Additional details from error data
	Repository string `json:"repository,omitempty"` // Repository involved (for repo scope blocks)
}

// GatewayServerMetrics represents usage metrics for a single MCP server
type GatewayServerMetrics struct {
	ServerName         string
	RequestCount       int
	ToolCallCount      int
	TotalDuration      float64 // in milliseconds
	ErrorCount         int
	FilteredCount      int // number of DIFC_FILTERED events for this server
	GuardPolicyBlocked int // number of tool calls blocked by guard policies for this server
	Tools              map[string]*GatewayToolMetrics
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
	TotalRequests     int
	TotalToolCalls    int
	TotalErrors       int
	TotalFiltered     int // number of DIFC_FILTERED events
	TotalGuardBlocked int // number of tool calls blocked by guard policies
	Servers           map[string]*GatewayServerMetrics
	FilteredEvents    []DifcFilteredEvent
	GuardPolicyEvents []GuardPolicyEvent
	StartTime         time.Time
	EndTime           time.Time
	TotalDuration     float64 // in milliseconds
}

// RPCMessageEntry represents a single entry from rpc-messages.jsonl.
// This file is written by the Copilot CLI and contains raw JSON-RPC protocol messages
// exchanged between the AI engine and MCP servers, as well as DIFC_FILTERED events.
type RPCMessageEntry struct {
	Timestamp string          `json:"timestamp"`
	Direction string          `json:"direction"`         // "IN" = received from server, "OUT" = sent to server; empty for DIFC_FILTERED
	Type      string          `json:"type"`              // legacy field: "REQUEST", "RESPONSE", or "DIFC_FILTERED"
	Event     string          `json:"event,omitempty"`   // schema "rpc-message/v2" field: "rpc_request", "rpc_response", or "difc_filtered"
	Schema    string          `json:"_schema,omitempty"` // e.g. "rpc-message/v2"
	ServerID  string          `json:"server_id"`
	Payload   json.RawMessage `json:"payload"`
	// Fields populated only for DIFC_FILTERED entries
	ToolName          string   `json:"tool_name,omitempty"`
	Description       string   `json:"description,omitempty"`
	Reason            string   `json:"reason,omitempty"`
	SecrecyTags       []string `json:"secrecy_tags,omitempty"`
	IntegrityTags     []string `json:"integrity_tags,omitempty"`
	AuthorAssociation string   `json:"author_association,omitempty"`
	AuthorLogin       string   `json:"author_login,omitempty"`
	HTMLURL           string   `json:"html_url,omitempty"`
	Number            string   `json:"number,omitempty"`
}

// rpcEventToType maps the "event" field values used by the schema "rpc-message/v2"
// format (written by real Copilot CLI MCP gateway telemetry) to the legacy "type"
// values ("REQUEST", "RESPONSE", "DIFC_FILTERED") used throughout this codebase.
// Real-world rpc-messages.jsonl files do not populate a top-level "type" field at
// all; they use "event" instead (e.g. "rpc_request"/"rpc_response").
var rpcEventToType = map[string]string{
	"rpc_request":   "REQUEST",
	"rpc_response":  "RESPONSE",
	"difc_filtered": "DIFC_FILTERED",
}

// EffectiveType returns the logical message type for this entry ("REQUEST",
// "RESPONSE", or "DIFC_FILTERED"), normalizing across rpc-messages.jsonl schema
// versions. Older/synthetic files populate the top-level "type" field directly;
// real production files (schema "rpc-message/v2") use "event" instead. The legacy
// "type" field takes precedence when both are present, since no known schema
// version populates both fields with conflicting values; this keeps the contract
// simple while remaining forward-compatible if a future schema starts setting
// "type" explicitly again.
func (e RPCMessageEntry) EffectiveType() string {
	if e.Type != "" {
		return e.Type
	}
	if mapped, ok := rpcEventToType[e.Event]; ok {
		return mapped
	}
	return e.Event
}

// rpcRequestPayload represents the JSON-RPC request payload fields we care about.
type rpcRequestPayload struct {
	Method string          `json:"method"`
	ID     any             `json:"id"`
	Params json.RawMessage `json:"params"`
}

// rpcToolCallParams represents the params for a tools/call request.
type rpcToolCallParams struct {
	Name string `json:"name"`
}

// rpcResponsePayload represents the JSON-RPC response payload fields we care about.
type rpcResponsePayload struct {
	ID    any       `json:"id"`
	Error *rpcError `json:"error,omitempty"`
}

func (p *rpcResponsePayload) UnmarshalJSON(data []byte) error {
	type responseAlias rpcResponsePayload
	var decoded struct {
		responseAlias
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*p = rpcResponsePayload(decoded.responseAlias)
	var resultMetadata struct {
		IsError bool `json:"isError"`
	}
	if p.Error == nil && len(decoded.Result) > 0 && json.Unmarshal(decoded.Result, &resultMetadata) == nil && resultMetadata.IsError {
		p.Error = &rpcError{Message: "MCP tool returned an error result"}
	}
	return nil
}

// rpcError represents a JSON-RPC error object.
type rpcError struct {
	Code    int           `json:"code"`
	Message string        `json:"message"`
	Data    *rpcErrorData `json:"data,omitempty"`
}

// rpcErrorData represents the optional data field in a JSON-RPC error, used by
// guard policy enforcement to communicate the reason and context for a denial.
type rpcErrorData struct {
	Reason     string `json:"reason,omitempty"`
	Repository string `json:"repository,omitempty"`
	Details    string `json:"details,omitempty"`
}

// rpcPendingRequest tracks an in-flight tool call for duration calculation.
type rpcPendingRequest struct {
	ServerID  string
	ToolName  string
	Timestamp time.Time
}

// isGuardPolicyErrorCode returns true if the JSON-RPC error code indicates a
// guard policy enforcement decision.
func isGuardPolicyErrorCode(code int) bool {
	return code >= guardPolicyErrorCodeIntegrityBelowMin && code <= guardPolicyErrorCodeAccessDenied
}

// guardPolicyReasonFromCode returns a human-readable reason string for a guard policy error code.
func guardPolicyReasonFromCode(code int) string {
	switch code {
	case guardPolicyErrorCodeAccessDenied:
		return "access_denied"
	case guardPolicyErrorCodeRepoNotAllowed:
		return "repo_not_allowed"
	case guardPolicyErrorCodeInsufficientPerms:
		return "insufficient_permissions"
	case guardPolicyErrorCodePrivateRepoDenied:
		return "private_repo_denied"
	case guardPolicyErrorCodeBlockedUser:
		return "blocked_user"
	case guardPolicyErrorCodeIntegrityBelowMin:
		return "integrity_below_minimum"
	default:
		return "unknown"
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
