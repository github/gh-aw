package cli

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"time"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/sliceutil"
	"github.com/github/gh-aw/pkg/timeutil"
)

var logsUsageActivityLog = logger.New("cli:logs_usage_activity")

const usageActivitySummarySchema = "usage-activity-summary/v1"

type usageActivitySummary struct {
	Schema      string                    `json:"schema,omitempty"`
	Firewall    *usageActivityFirewall    `json:"firewall,omitempty"`
	Session     *usageActivitySession     `json:"session,omitempty"`
	Gateway     *usageActivityGateway     `json:"gateway,omitempty"`
	Integrity   *IntegrityFilterSummary   `json:"integrity,omitempty"`
	SafeOutputs *usageActivitySafeOutputs `json:"safe_outputs,omitempty"`
	Experiments *usageActivityExperiments `json:"experiments,omitempty"`
	WorkingSet  *WorkingSetMetrics        `json:"working_set,omitempty"`
}

// WorkingSetMetrics describes cumulative model-input traffic relative to the
// largest invocation context observed during the agent phase.
type WorkingSetMetrics struct {
	MeasurementState      string   `json:"measurement_state"`
	RebuildFactor         *float64 `json:"rebuild_factor,omitempty"`
	CumulativeInputTokens int64    `json:"cumulative_input_tokens"`
	PeakInputTokens       int64    `json:"peak_input_tokens"`
	RebuildExcessTokens   int64    `json:"rebuild_excess_tokens"`
	Invocations           int      `json:"invocations"`
}

// wsrfDisplayValue formats the Working-Set Rebuild Factor for compact table
// display, returning "" when the metric was not measured for the run so table
// renderers can fall back to their standard "-" placeholder.
func wsrfDisplayValue(ws *WorkingSetMetrics) string {
	if ws == nil || ws.RebuildFactor == nil {
		return ""
	}
	return fmt.Sprintf("%.2f", *ws.RebuildFactor)
}

type usageActivityFirewall struct {
	TotalRequests    int                           `json:"total_requests"`
	AllowedRequests  int                           `json:"allowed_requests"`
	BlockedRequests  int                           `json:"blocked_requests"`
	AllowedDomains   []string                      `json:"allowed_domains,omitempty"`
	BlockedDomains   []string                      `json:"blocked_domains,omitempty"`
	RequestsByDomain map[string]DomainRequestStats `json:"requests_by_domain,omitempty"`
}

type usageActivitySession struct {
	TotalEvents            int `json:"total_events"`
	SessionStarts          int `json:"session_starts"`
	SessionShutdowns       int `json:"session_shutdowns"`
	Turns                  int `json:"turns"`
	AssistantMessages      int `json:"assistant_messages"`
	ReasoningEvents        int `json:"reasoning_events"`
	ToolExecutionStarts    int `json:"tool_execution_starts"`
	ToolExecutionCompletes int `json:"tool_execution_completes"`
	FailedToolExecutions   int `json:"failed_tool_executions"`
}

type usageActivityGateway struct {
	TotalCalls      int                          `json:"total_calls"`
	FailedCalls     int                          `json:"failed_calls"`
	TotalInputSize  int                          `json:"total_input_size"`
	TotalOutputSize int                          `json:"total_output_size"`
	MaxInputSize    int                          `json:"max_input_size"`
	MaxOutputSize   int                          `json:"max_output_size"`
	Servers         []usageActivityGatewayServer `json:"servers,omitempty"`
	Tools           []usageActivityGatewayTool   `json:"tools,omitempty"`
	ToolCalls       []usageActivityGatewayCall   `json:"tool_calls,omitempty"`
}

type usageActivityGatewayCall struct {
	ToolCallID   string  `json:"tool_call_id"`
	Timestamp    string  `json:"timestamp"`
	ServerName   string  `json:"server_name"`
	ToolName     string  `json:"tool_name"`
	RequestSize  int     `json:"request_size"`
	ResponseSize int     `json:"response_size"`
	DurationMS   float64 `json:"duration_ms"`
	Outcome      string  `json:"outcome"`
}

type usageActivityGatewayServer struct {
	ServerName      string  `json:"server_name"`
	RequestCount    int     `json:"request_count"`
	ToolCallCount   int     `json:"tool_call_count"`
	FailedCalls     int     `json:"failed_calls"`
	TotalInputSize  int     `json:"total_input_size"`
	TotalOutputSize int     `json:"total_output_size"`
	AvgDurationMS   float64 `json:"avg_duration_ms"`
}

type usageActivityGatewayTool struct {
	ServerName      string  `json:"server_name"`
	ToolName        string  `json:"tool_name"`
	CallCount       int     `json:"call_count"`
	FailedCalls     int     `json:"failed_calls"`
	TotalInputSize  int     `json:"total_input_size"`
	TotalOutputSize int     `json:"total_output_size"`
	MaxInputSize    int     `json:"max_input_size"`
	MaxOutputSize   int     `json:"max_output_size"`
	AvgDurationMS   float64 `json:"avg_duration_ms"`
	MaxDurationMS   float64 `json:"max_duration_ms"`
}

type usageActivitySafeOutputs struct {
	TotalItems  int            `json:"total_items"`
	ItemsByType map[string]int `json:"items_by_type,omitempty"`
}

type usageActivityExperiments struct {
	// Assignments maps each experiment name to the variant selected for this run.
	Assignments map[string]string `json:"assignments,omitempty"`
}

func loadUsageActivitySummary(runDir string) (*usageActivitySummary, error) {
	candidates := []string{
		filepath.Join(runDir, "usage", "activity", "summary.json"),
		filepath.Join(runDir, "activity", "summary.json"),
	}
	var lastErr error
	for _, candidate := range candidates {
		cleanPath := filepath.Clean(candidate)
		raw, err := os.ReadFile(cleanPath)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("read usage activity summary %s: %w", cleanPath, err)
		}
		var summary usageActivitySummary
		if err := json.Unmarshal(raw, &summary); err != nil {
			logsUsageActivityLog.Printf("loadUsageActivitySummary: failed to parse %s, trying next candidate", cleanPath)
			lastErr = fmt.Errorf("parse usage activity summary %s: %w", cleanPath, err)
			continue
		}
		if summary.Schema != usageActivitySummarySchema {
			logsUsageActivityLog.Printf("loadUsageActivitySummary: unsupported schema %q in %s (expected %q)", summary.Schema, cleanPath, usageActivitySummarySchema)
			lastErr = fmt.Errorf("unsupported usage activity summary schema %q in %s (expected %q)", summary.Schema, cleanPath, usageActivitySummarySchema)
			continue
		}
		logsUsageActivityLog.Printf("loadUsageActivitySummary: loaded summary from %s", cleanPath)
		return &summary, nil
	}
	return nil, lastErr
}

func applyUsageActivitySummaryToResult(summary *usageActivitySummary, result *DownloadResult, allowTurnBackfill bool) {
	if summary == nil || result == nil {
		return
	}

	if summary.WorkingSet != nil {
		result.WorkingSet = summary.WorkingSet
	}

	// Preserve previously parsed turn counts (from full session artifacts/events.jsonl)
	// and only backfill when they are missing.
	if allowTurnBackfill && summary.Session != nil && result.Run.Turns == 0 && summary.Session.Turns > 0 {
		result.Run.Turns = summary.Session.Turns
	}

	applyUsageActivityFirewallSummary(summary.Firewall, result)
	applyUsageActivityMCPSummary(summary.Gateway, summary.Integrity, result)

	// Backfill safe output item count from usage summary when the safe-outputs-items
	// artifact was not downloaded separately. The count is 0-safe: only backfill when
	// the summary reports at least one item to avoid masking genuine zero-item runs.
	if summary.SafeOutputs != nil && result.Run.SafeItemsCount == 0 && summary.SafeOutputs.TotalItems > 0 {
		logsUsageActivityLog.Printf("applyUsageActivitySummaryToResult: backfilling safe output item count from usage summary (total=%d)", summary.SafeOutputs.TotalItems)
		result.Run.SafeItemsCount = summary.SafeOutputs.TotalItems
	}
}

func applyUsageActivityFirewallSummary(firewall *usageActivityFirewall, result *DownloadResult) {
	if firewall == nil || result.FirewallAnalysis != nil {
		return
	}
	logsUsageActivityLog.Printf("applyUsageActivitySummaryToResult: backfilling firewall analysis (total=%d allowed=%d blocked=%d)", firewall.TotalRequests, firewall.AllowedRequests, firewall.BlockedRequests)
	requestsByDomain := maps.Clone(firewall.RequestsByDomain)
	if requestsByDomain == nil {
		requestsByDomain = map[string]DomainRequestStats{}
	}
	allowedSet := map[string]struct{}{}
	blockedSet := map[string]struct{}{}
	for _, domain := range firewall.AllowedDomains {
		allowedSet[domain] = struct{}{}
	}
	for _, domain := range firewall.BlockedDomains {
		blockedSet[domain] = struct{}{}
	}
	for domain, stats := range requestsByDomain {
		if stats.Allowed > 0 {
			allowedSet[domain] = struct{}{}
		}
		if stats.Blocked > 0 {
			blockedSet[domain] = struct{}{}
		}
	}
	result.FirewallAnalysis = &FirewallAnalysis{
		AnalysisBase: AnalysisBase{
			DomainBuckets: DomainBuckets{
				AllowedDomains: sliceutil.SortedKeys(allowedSet),
				BlockedDomains: sliceutil.SortedKeys(blockedSet),
			},
			TotalRequests:   firewall.TotalRequests,
			AllowedRequests: firewall.AllowedRequests,
			BlockedRequests: firewall.BlockedRequests,
		},
		RequestsByDomain: requestsByDomain,
	}
}

func applyUsageActivityMCPSummary(gateway *usageActivityGateway, integritySummary *IntegrityFilterSummary, result *DownloadResult) {
	if gateway == nil && integritySummary == nil {
		return
	}
	if result.MCPToolUsage != nil {
		backfillUsageActivityMCPMetrics(gateway, integritySummary, result.MCPToolUsage)
		return
	}
	serverCount := 0
	if gateway != nil {
		serverCount = len(gateway.Servers)
	}
	logsUsageActivityLog.Printf("applyUsageActivitySummaryToResult: backfilling MCP tool usage from %d gateway server(s)", serverCount)

	var tools []MCPToolSummary
	var servers []MCPServerStats
	if gateway != nil {
		tools = buildUsageActivityTools(gateway.Tools)
		servers = buildUsageActivityServers(gateway.Servers)
	}

	var integrity *IntegrityFilterSummary
	if integritySummary != nil {
		integrity = cloneIntegrityFilterSummary(integritySummary)
	}
	result.MCPToolUsage = &MCPToolUsageData{
		Summary:   tools,
		ToolCalls: buildUsageActivityToolCalls(gateway),
		Servers:   servers,
		Integrity: integrity,
	}
}

func backfillUsageActivityMCPMetrics(gateway *usageActivityGateway, integritySummary *IntegrityFilterSummary, usage *MCPToolUsageData) {
	if gateway != nil {
		if len(usage.ToolCalls) == 0 {
			usage.ToolCalls = buildUsageActivityToolCalls(gateway)
		}
		activityTools := make(map[string]usageActivityGatewayTool, len(gateway.Tools))
		for _, tool := range gateway.Tools {
			activityTools[tool.ServerName+":"+tool.ToolName] = tool
		}
		for index := range usage.Summary {
			tool := &usage.Summary[index]
			tool.syncFieldsFromBase()
			if activity, ok := activityTools[tool.ServerName+":"+tool.ToolName]; ok {
				backfillUsageActivityToolMetrics(tool, activity)
			}
		}

		activityServers := make(map[string]usageActivityGatewayServer, len(gateway.Servers))
		for _, server := range gateway.Servers {
			activityServers[server.ServerName] = server
		}
		for index := range usage.Servers {
			server := &usage.Servers[index]
			if activity, ok := activityServers[server.ServerName]; ok {
				if server.TotalInputSize == 0 {
					server.TotalInputSize = activity.TotalInputSize
				}
				if server.TotalOutputSize == 0 {
					server.TotalOutputSize = activity.TotalOutputSize
				}
				if server.AvgDuration == "" {
					server.AvgDuration = formatActivityDuration(activity.AvgDurationMS)
				}
			}
		}
	}
	if usage.Integrity == nil && len(usage.FilteredEvents) == 0 && integritySummary != nil {
		usage.Integrity = cloneIntegrityFilterSummary(integritySummary)
	}
}

func buildUsageActivityToolCalls(gateway *usageActivityGateway) []MCPToolCall {
	if gateway == nil {
		return nil
	}
	calls := make([]MCPToolCall, 0, len(gateway.ToolCalls))
	for _, call := range gateway.ToolCalls {
		calls = append(calls, MCPToolCall{
			ToolCallID: call.ToolCallID,
			Timestamp:  call.Timestamp,
			ServerName: call.ServerName,
			ToolName:   call.ToolName,
			InputSize:  call.RequestSize,
			OutputSize: call.ResponseSize,
			Duration:   formatActivityDuration(call.DurationMS),
			Status:     call.Outcome,
		})
	}
	return calls
}

func backfillUsageActivityToolMetrics(tool *MCPToolSummary, activity usageActivityGatewayTool) {
	if tool.TotalInputSize == 0 {
		tool.TotalInputSize = activity.TotalInputSize
	}
	if tool.TotalOutputSize == 0 {
		tool.TotalOutputSize = activity.TotalOutputSize
	}
	if tool.MaxInputSize == 0 {
		tool.MaxInputSize = activity.MaxInputSize
	}
	if tool.MaxOutputSize == 0 {
		tool.MaxOutputSize = activity.MaxOutputSize
	}
	if tool.AvgDuration == "" {
		tool.AvgDuration = formatActivityDuration(activity.AvgDurationMS)
	}
	if tool.MaxDuration == "" {
		tool.MaxDuration = formatActivityDuration(activity.MaxDurationMS)
	}
	tool.syncBaseFromFields()
}

func cloneIntegrityFilterSummary(summary *IntegrityFilterSummary) *IntegrityFilterSummary {
	if summary == nil {
		return nil
	}
	return &IntegrityFilterSummary{
		TotalFiltered:        summary.TotalFiltered,
		FilteredServerCounts: maps.Clone(summary.FilteredServerCounts),
		FilteredToolCounts:   maps.Clone(summary.FilteredToolCounts),
		FilteredReasonCounts: maps.Clone(summary.FilteredReasonCounts),
	}
}

func buildUsageActivityTools(activityTools []usageActivityGatewayTool) []MCPToolSummary {
	tools := make([]MCPToolSummary, 0, len(activityTools))
	for _, tool := range activityTools {
		toolSummary := MCPToolSummary{
			ServerName:      tool.ServerName,
			ToolName:        tool.ToolName,
			CallCount:       tool.CallCount,
			TotalInputSize:  tool.TotalInputSize,
			TotalOutputSize: tool.TotalOutputSize,
			MaxInputSize:    tool.MaxInputSize,
			MaxOutputSize:   tool.MaxOutputSize,
			AvgDuration:     formatActivityDuration(tool.AvgDurationMS),
			MaxDuration:     formatActivityDuration(tool.MaxDurationMS),
			ErrorCount:      tool.FailedCalls,
		}
		toolSummary.syncBaseFromFields()
		tools = append(tools, toolSummary)
	}
	return tools
}

func buildUsageActivityServers(activityServers []usageActivityGatewayServer) []MCPServerStats {
	servers := make([]MCPServerStats, 0, len(activityServers))
	for _, server := range activityServers {
		requestCount := server.RequestCount
		if requestCount == 0 {
			requestCount = server.ToolCallCount
		}
		servers = append(servers, MCPServerStats{
			MCPServerStatsBase: MCPServerStatsBase{
				ServerName:    server.ServerName,
				ToolCallCount: server.ToolCallCount,
				ErrorCount:    server.FailedCalls,
			},
			RequestCount:    requestCount,
			TotalInputSize:  server.TotalInputSize,
			TotalOutputSize: server.TotalOutputSize,
			AvgDuration:     formatActivityDuration(server.AvgDurationMS),
		})
	}
	return servers
}

func formatActivityDuration(milliseconds float64) string {
	if milliseconds <= 0 {
		return ""
	}
	return timeutil.FormatDuration(time.Duration(milliseconds * float64(time.Millisecond)))
}
