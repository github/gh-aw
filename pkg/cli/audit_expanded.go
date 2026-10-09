package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/fileutil"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/sliceutil"
	"github.com/github/gh-aw/pkg/timeutil"
	"github.com/github/gh-aw/pkg/workflow"
)

var auditExpandedLog = logger.New("cli:audit_expanded")

// AuditEngineConfig represents the engine configuration extracted from aw_info.json
type AuditEngineConfig struct {
	EngineID           string                      `json:"engine_id" console:"header:Engine ID"`
	EngineName         string                      `json:"engine_name,omitempty" console:"header:Engine Name,omitempty"`
	Model              string                      `json:"model,omitempty" console:"header:Model,omitempty"`
	RequestedModel     string                      `json:"requested_model,omitempty" console:"header:Requested Model,omitempty"`
	ModelEffort        string                      `json:"model_effort,omitempty" console:"header:Model Effort,omitempty"`
	ModelRoutingStatus string                      `json:"model_routing_status,omitempty" console:"header:Model Routing,omitempty"`
	HarnessOutcome     *AuditHarnessRoutingOutcome `json:"harness_outcome,omitempty"`
	RoutingWarning     string                      `json:"routing_warning,omitempty"`
	Version            string                      `json:"version,omitempty" console:"header:Version,omitempty"`
	CLIVersion         string                      `json:"cli_version,omitempty" console:"header:CLI Version,omitempty"`
	FirewallVersion    string                      `json:"firewall_version,omitempty" console:"header:Firewall Version,omitempty"`
	MCPServers         []string                    `json:"mcp_servers,omitempty"`
	TriggerEvent       string                      `json:"trigger_event,omitempty" console:"header:Trigger Event,omitempty"`
	Repository         string                      `json:"repository,omitempty" console:"header:Repository,omitempty"`
	DryRun             bool                        `json:"dry_run" console:"header:Dry Run"`
}

type AuditHarnessRoutingOutcome struct {
	Status            string `json:"status,omitempty"`
	WireModel         string `json:"wire_model,omitempty"`
	EffectiveEndpoint string `json:"effective_endpoint,omitempty"`
	SelectedEndpoint  string `json:"selected_endpoint,omitempty"`
	Effort            string `json:"effort,omitempty"`
	AppliedEffort     string `json:"applied_effort,omitempty"`
	FailureCode       string `json:"failure_code,omitempty"`
}

// PromptAnalysis represents analysis of the input prompt
type PromptAnalysis struct {
	PromptSize int    `json:"prompt_size" console:"header:Prompt Size (chars)"`
	PromptFile string `json:"prompt_file,omitempty" console:"header:Prompt File,omitempty"`
}

// SessionAnalysis represents session and agent performance metrics
type SessionAnalysis struct {
	WallTime            string  `json:"wall_time,omitempty" console:"header:Wall Time,omitempty"`
	TurnCount           int     `json:"turn_count,omitempty" console:"header:Turn Count,omitempty"`
	AvgTurnDuration     string  `json:"avg_turn_duration,omitempty" console:"header:Avg Turn Duration,omitempty"`
	AvgTimeBetweenTurns string  `json:"avg_time_between_turns,omitempty" console:"header:Avg Time Between Turns,omitempty"`
	MaxTimeBetweenTurns string  `json:"max_time_between_turns,omitempty" console:"header:Max Time Between Turns,omitempty"`
	TokensPerMinute     float64 `json:"tokens_per_minute,omitempty"`
	TimeoutDetected     bool    `json:"timeout_detected"`
	NoopCount           int     `json:"noop_count,omitempty" console:"header:Noop Count,omitempty"`
	AgentActiveRatio    float64 `json:"agent_active_ratio,omitempty"` // 0.0 - 1.0
	CacheWarning        string  `json:"cache_warning,omitempty" console:"header:Cache Warning,omitempty"`
}

// SafeOutputSummary provides a summary of safe output items by type
type SafeOutputSummary struct {
	TotalItems                 int                    `json:"total_items" console:"header:Total Items"`
	ItemsByType                map[string]int         `json:"items_by_type"`
	Summary                    string                 `json:"summary" console:"header:Summary"`
	TemporaryIDMapStatus       string                 `json:"temporary_id_map_status,omitempty"`
	TemporaryIDMappings        int                    `json:"temporary_id_mappings,omitempty"`
	ChainedTargetCount         int                    `json:"chained_target_count,omitempty"`
	ChainedFollowupActionCount int                    `json:"chained_followup_action_count,omitempty"`
	DelegatedTempTargetCount   int                    `json:"delegated_temp_target_count,omitempty"`
	ClosedTempTargetCount      int                    `json:"closed_temp_target_count,omitempty"`
	TypeDetails                []SafeOutputTypeDetail `json:"type_details,omitempty"`
}

// SafeOutputTypeDetail contains counts for a specific safe output type
type SafeOutputTypeDetail struct {
	Type  string `json:"type" console:"header:Type"`
	Count int    `json:"count" console:"header:Count"`
}

// MCPServerHealth provides a health summary of MCP servers from gateway metrics
type MCPServerHealth struct {
	TotalServers  int                     `json:"total_servers"`
	HealthySvrs   int                     `json:"healthy_servers"`
	DegradedSvrs  int                     `json:"degraded_servers"`
	FailedSvrs    int                     `json:"failed_servers"`
	Summary       string                  `json:"summary" console:"header:Summary"`
	TotalRequests int                     `json:"total_requests" console:"header:Total Requests"`
	TotalErrors   int                     `json:"total_errors" console:"header:Total Errors"`
	ErrorRate     float64                 `json:"error_rate"`
	Servers       []MCPServerHealthDetail `json:"servers,omitempty"`
	SlowestCalls  []MCPSlowestToolCall    `json:"slowest_calls,omitempty"`
}

// MCPServerHealthDetail represents health details for a single MCP server
type MCPServerHealthDetail struct {
	MCPServerStatsBase
	RequestCount int     `json:"request_count" console:"header:Requests"`
	ErrorRate    float64 `json:"error_rate"` // Percentage (0–100)
	ErrorRateStr string  `json:"error_rate_str" console:"header:Error Rate"`
	AvgLatency   string  `json:"avg_latency" console:"header:Avg Latency"`
	Status       string  `json:"status" console:"header:Status"`
}

// MarshalJSON preserves the MCP server health detail JSON schema while sharing the
// per-server stat fields with the other MCP server report types.
func (d MCPServerHealthDetail) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		ServerName   string  `json:"server_name"`
		RequestCount int     `json:"request_count"`
		ToolCalls    int     `json:"tool_calls"`
		ErrorCount   int     `json:"error_count"`
		ErrorRate    float64 `json:"error_rate"`
		ErrorRateStr string  `json:"error_rate_str"`
		AvgLatency   string  `json:"avg_latency"`
		Status       string  `json:"status"`
	}{
		ServerName:   d.ServerName,
		RequestCount: d.RequestCount,
		ToolCalls:    d.ToolCallCount,
		ErrorCount:   d.ErrorCount,
		ErrorRate:    d.ErrorRate,
		ErrorRateStr: d.ErrorRateStr,
		AvgLatency:   d.AvgLatency,
		Status:       d.Status,
	})
}

// UnmarshalJSON is the counterpart to MarshalJSON, mapping the legacy
// "tool_calls" wire key back into the embedded MCPServerStatsBase.
func (d *MCPServerHealthDetail) UnmarshalJSON(data []byte) error {
	var aux struct {
		ServerName   string  `json:"server_name"`
		RequestCount int     `json:"request_count"`
		ToolCalls    int     `json:"tool_calls"`
		ErrorCount   int     `json:"error_count"`
		ErrorRate    float64 `json:"error_rate"`
		ErrorRateStr string  `json:"error_rate_str"`
		AvgLatency   string  `json:"avg_latency"`
		Status       string  `json:"status"`
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	d.MCPServerStatsBase = MCPServerStatsBase{
		ServerName:    aux.ServerName,
		ToolCallCount: aux.ToolCalls,
		ErrorCount:    aux.ErrorCount,
	}
	d.RequestCount = aux.RequestCount
	d.ErrorRate = aux.ErrorRate
	d.ErrorRateStr = aux.ErrorRateStr
	d.AvgLatency = aux.AvgLatency
	d.Status = aux.Status
	return nil
}

// MCPSlowestToolCall represents a slow tool call for surfacing in the audit
type MCPSlowestToolCall struct {
	ServerName string `json:"server_name" console:"header:Server"`
	ToolName   string `json:"tool_name" console:"header:Tool"`
	Duration   string `json:"duration" console:"header:Duration"`
}

// findAwInfoPath returns the first existing aw_info.json path from known locations.
// The activation artifact may or may not have been flattened to the root directory.
func findAwInfoPath(logsPath string) string {
	candidates := []string{
		filepath.Join(logsPath, "agent", "aw_info.json"),
		filepath.Join(logsPath, "aw_info.json"),
		filepath.Join(logsPath, "activation", "aw_info.json"),
	}
	for _, p := range candidates {
		if fileutil.FileExists(p) {
			return p
		}
	}
	return ""
}

func extractEngineConfigWithInferredEngine(logsPath, inferredEngineID string) *AuditEngineConfig {
	if logsPath == "" {
		return nil
	}

	awInfo := loadAwInfoForEngineConfig(logsPath)
	if awInfo == nil {
		auditExpandedLog.Printf("aw_info.json not found in %s", logsPath)
		if inferredEngineID != "" {
			registry := workflow.GetGlobalEngineRegistry()
			if engine, err := registry.GetEngine(inferredEngineID); err == nil {
				auditExpandedLog.Printf("Inferred engine config without aw_info.json: engine=%s", inferredEngineID)
				return &AuditEngineConfig{
					EngineID:   inferredEngineID,
					EngineName: engine.GetDisplayName(),
				}
			}
		}
		return nil
	}

	routing := analyzeModelRouting(logsPath)
	attribution := resolveEffectiveModelAttribution(logsPath, awInfo, routing, nil)
	config := &AuditEngineConfig{
		EngineID:           awInfo.EngineID,
		EngineName:         awInfo.EngineName,
		Model:              attribution.Model,
		RequestedModel:     attribution.RequestedModel,
		ModelEffort:        attribution.Effort,
		ModelRoutingStatus: attribution.RoutingStatus,
		Version:            awInfo.Version,
		CLIVersion:         awInfo.CLIVersion,
		FirewallVersion:    awInfo.GetFirewallVersion(),
		TriggerEvent:       awInfo.EventName,
		Repository:         awInfo.Repository,
		DryRun:             awInfo.DryRun,
	}
	addHarnessRoutingOutcome(config, awInfo, logsPath)

	// Extract MCP server names from aw_info.json steps metadata
	if mcpNames, ok := extractMCPServerNamesFromAwInfo(logsPath); ok {
		config.MCPServers = mcpNames
	}

	auditExpandedLog.Printf("Extracted engine config: engine=%s, model=%s, mcp_servers=%d",
		config.EngineID, config.Model, len(config.MCPServers))
	return config
}

func addHarnessRoutingOutcome(config *AuditEngineConfig, awInfo *AwInfo, logsPath string) {
	sessionRouting, _, err := readSessionModelRouting(logsPath)
	if err != nil || sessionRouting == nil || sessionRouting.Outcome == nil {
		return
	}
	outcome := sessionRouting.Outcome
	config.HarnessOutcome = &AuditHarnessRoutingOutcome{
		Status:            outcome.Status,
		WireModel:         outcome.WireModel,
		EffectiveEndpoint: outcome.EffectiveEndpoint,
		SelectedEndpoint:  outcome.SelectedEndpoint,
		Effort:            outcome.Effort,
		AppliedEffort:     outcome.AppliedEffort,
		FailureCode:       outcome.FailureCode,
	}
	routing := awInfo.ModelRouting
	if workflowInfo := sessionRouting.WorkflowInfo; workflowInfo != nil && workflowInfo.ModelRouting != nil {
		data := workflowInfo.ModelRouting
		routing = &AwInfoModelRouting{
			Status: data.Status, WireModel: data.WireModel, Endpoint: data.EffectiveEndpoint,
			SelectedEndpoint: data.SelectedEndpoint, Effort: data.Effort,
			AppliedEffort: data.AppliedEffort, FailureCode: data.FailureCode,
		}
	}
	if sessionRouting.outcomeDisagreesWith(routing) {
		config.RoutingWarning = "Harness routing outcome disagrees with runner-written model routing metadata."
	}
}

func loadAwInfoForEngineConfig(logsPath string) *AwInfo {
	if awInfoPath := findAwInfoPath(logsPath); awInfoPath != "" {
		awInfo, err := parseAwInfo(awInfoPath, false)
		if err != nil || awInfo == nil {
			auditExpandedLog.Printf("Failed to parse aw_info.json for engine config: %v", err)
			return nil
		}
		return awInfo
	}
	sessionRouting, found, err := readSessionModelRouting(logsPath)
	if err != nil {
		auditExpandedLog.Printf("Failed to read unified session workflow metadata: %v", err)
	}
	if found {
		return sessionRouting.awInfo()
	}
	return nil
}

func inferFallbackLogMetrics(logsPath string) (LogMetrics, string) {
	if logsPath == "" {
		return LogMetrics{}, ""
	}

	if eventsJSONLPath := findEventsJSONLFile(logsPath); eventsJSONLPath != "" {
		if metrics, err := parseEventsJSONLMetrics(eventsJSONLPath, false); err == nil && hasUsefulFallbackMetrics(metrics) {
			return metrics, "copilot"
		}
	}

	agentLogPath := findAgentStdioLogPath(logsPath)
	if agentLogPath == "" {
		return LogMetrics{}, ""
	}
	content, err := os.ReadFile(agentLogPath)
	if err != nil {
		return LogMetrics{}, ""
	}
	return inferBestEngineMetricsFromContent(string(content))
}

func findAgentStdioLogPath(logsPath string) string {
	root := filepath.Join(logsPath, "agent-stdio.log")
	if fileutil.FileExists(root) {
		return root
	}

	var found string
	walkErr := filepath.Walk(logsPath, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		if info.Name() == "agent-stdio.log" {
			found = path
			return filepath.SkipAll
		}
		return nil
	})
	if walkErr != nil && !errors.Is(walkErr, filepath.SkipAll) {
		auditExpandedLog.Printf("Failed while searching for agent-stdio.log in %s: %v", logsPath, walkErr)
	}
	return found
}

func hasUsefulFallbackMetrics(metrics LogMetrics) bool {
	return metrics.TokenUsage > 0 || metrics.Turns > 0 || len(metrics.ToolCalls) > 0
}

func inferBestEngineMetricsFromContent(logContent string) (LogMetrics, string) {
	registry := workflow.GetGlobalEngineRegistry()
	engineIDs := registry.GetSupportedEngines()
	const (
		// Prioritize selecting parsers that recover turn count first (primary signal for audit quality),
		// then token usage, then tool call shape.
		fallbackTurnsWeight     = 100000
		fallbackToolCallsWeight = 1000
	)

	var bestMetrics LogMetrics
	var bestEngineID string
	bestScore := -1

	for _, engineID := range engineIDs {
		engine, err := registry.GetEngine(engineID)
		if err != nil {
			continue
		}
		metrics := engine.ParseLogMetrics(logContent, false)
		score := metrics.TokenUsage + (metrics.Turns * fallbackTurnsWeight) + (len(metrics.ToolCalls) * fallbackToolCallsWeight)
		if score > bestScore {
			bestScore = score
			bestMetrics = metrics
			bestEngineID = engineID
		}
	}

	if !hasUsefulFallbackMetrics(bestMetrics) {
		return LogMetrics{}, ""
	}
	return bestMetrics, bestEngineID
}

// extractPromptAnalysis reads prompt.txt and returns analysis metrics
func extractPromptAnalysis(logsPath string) *PromptAnalysis {
	if logsPath == "" {
		return nil
	}

	// Try multiple possible locations for prompt.txt.
	// The activation artifact may or may not have been flattened to the root.
	promptPaths := []string{
		filepath.Join(logsPath, "prompt.txt"),
		filepath.Join(logsPath, "aw-prompts", "prompt.txt"),
		filepath.Join(logsPath, "activation", "aw-prompts", "prompt.txt"),
		filepath.Join(logsPath, "agent", "aw-prompts", "prompt.txt"),
	}

	for _, promptPath := range promptPaths {
		data, err := os.ReadFile(promptPath)
		if err != nil {
			continue
		}

		// Store a stable relative path instead of machine-specific absolute path
		relPromptPath, relErr := filepath.Rel(logsPath, promptPath)
		if relErr != nil {
			relPromptPath = filepath.Base(promptPath)
		}

		analysis := &PromptAnalysis{
			PromptSize: len(data),
			PromptFile: relPromptPath,
		}

		auditExpandedLog.Printf("Extracted prompt analysis: size=%d chars from %s", analysis.PromptSize, relPromptPath)
		return analysis
	}

	auditExpandedLog.Printf("No prompt.txt found in %s", logsPath)
	return nil
}

// buildSessionAnalysis creates session performance metrics from available data
func buildSessionAnalysis(processedRun ProcessedRun, metrics LogMetrics) *SessionAnalysis {
	run := processedRun.Run

	session := &SessionAnalysis{
		TurnCount: metrics.Turns,
		NoopCount: run.NoopCount,
	}

	// Wall time from run duration
	if run.Duration > 0 {
		session.WallTime = timeutil.FormatDuration(run.Duration)
	}

	// Average turn duration
	if metrics.Turns > 0 && run.Duration > 0 {
		avgTurnDuration := run.Duration / (time.Duration(metrics.Turns))
		session.AvgTurnDuration = timeutil.FormatDuration(avgTurnDuration)
	}

	setSessionTurnTiming(session, run.Duration, metrics)

	// Tokens per minute
	if metrics.TokenUsage > 0 && run.Duration > 0 {
		minutes := run.Duration.Minutes()
		if minutes > 0 {
			session.TokensPerMinute = float64(metrics.TokenUsage) / minutes
		}
	}

	// Timeout detection: check if the run was cancelled (typically indicates timeout)
	if run.Conclusion == "cancelled" || run.Conclusion == "timed_out" {
		session.TimeoutDetected = true
	}

	// Check for timeout patterns in job conclusions
	for _, job := range processedRun.JobDetails {
		if job.Conclusion == "cancelled" || job.Conclusion == "timed_out" {
			session.TimeoutDetected = true
			break
		}
	}

	auditExpandedLog.Printf("Built session analysis: turns=%d, wall_time=%s, avg_tbt=%s, max_tbt=%s, timeout=%v",
		session.TurnCount, session.WallTime, session.AvgTimeBetweenTurns, session.MaxTimeBetweenTurns, session.TimeoutDetected)
	return session
}

func setSessionTurnTiming(session *SessionAnalysis, runDuration time.Duration, metrics LogMetrics) {
	const anthropicCacheTTL = 5 * time.Minute
	if metrics.AvgTimeBetweenTurns > 0 {
		session.AvgTimeBetweenTurns = timeutil.FormatDuration(metrics.AvgTimeBetweenTurns)
		if metrics.MaxTimeBetweenTurns > 0 {
			session.MaxTimeBetweenTurns = timeutil.FormatDuration(metrics.MaxTimeBetweenTurns)
		}
		if metrics.MaxTimeBetweenTurns > anthropicCacheTTL {
			session.CacheWarning = fmt.Sprintf(
				"Max TBT (%s) exceeds Anthropic 5-min cache TTL — prompt cache will expire between turns, increasing cost",
				timeutil.FormatDuration(metrics.MaxTimeBetweenTurns),
			)
		} else if metrics.AvgTimeBetweenTurns > anthropicCacheTTL {
			session.CacheWarning = fmt.Sprintf(
				"Avg TBT (%s) exceeds Anthropic 5-min cache TTL — prompt cache likely expiring between turns",
				timeutil.FormatDuration(metrics.AvgTimeBetweenTurns),
			)
		}
	} else if metrics.Turns > 1 && runDuration > 0 {
		avgTBT := runDuration / time.Duration(metrics.Turns-1)
		session.AvgTimeBetweenTurns = timeutil.FormatDuration(avgTBT) + " (estimated)"
		if avgTBT > anthropicCacheTTL {
			session.CacheWarning = fmt.Sprintf(
				"Estimated avg TBT (%s) exceeds Anthropic 5-min cache TTL — prompt cache likely expiring between turns",
				timeutil.FormatDuration(avgTBT),
			)
		}
	}
}

// buildSafeOutputSummary creates a summary of safe output items by type
func buildSafeOutputSummary(items []CreatedItemReport, chainMetrics SafeOutputChainMetrics) *SafeOutputSummary {
	if len(items) == 0 && chainMetrics.TemporaryIDMapStatus == "" {
		return nil
	}

	summary := &SafeOutputSummary{
		TotalItems:                 len(items),
		ItemsByType:                make(map[string]int),
		TemporaryIDMapStatus:       chainMetrics.TemporaryIDMapStatus,
		TemporaryIDMappings:        chainMetrics.TemporaryIDMappings,
		ChainedTargetCount:         chainMetrics.ChainedTargetCount,
		ChainedFollowupActionCount: chainMetrics.ChainedFollowupActionCount,
		DelegatedTempTargetCount:   chainMetrics.DelegatedTempTargetCount,
		ClosedTempTargetCount:      chainMetrics.ClosedTempTargetCount,
	}

	// Count items by type
	for _, item := range items {
		itemType := item.Type
		if itemType == "" {
			itemType = "unknown"
		}
		summary.ItemsByType[itemType]++
	}

	// Build type details sorted by count (desc), then type name (asc) for determinism
	for itemType, count := range summary.ItemsByType {
		summary.TypeDetails = append(summary.TypeDetails, SafeOutputTypeDetail{
			Type:  itemType,
			Count: count,
		})
	}
	slices.SortFunc(summary.TypeDetails, func(a, b SafeOutputTypeDetail) int {
		if a.Count == b.Count {
			switch {
			case a.Type < b.Type:
				return -1
			case a.Type > b.Type:
				return 1
			default:
				return 0
			}
		}
		if a.Count > b.Count {
			return -1
		}
		return 1
	})

	// Build human-readable summary string
	summary.Summary = buildSafeOutputSummaryString(summary.TypeDetails)

	auditExpandedLog.Printf("Built safe output summary: %d items across %d types (temp_map_status=%s)",
		summary.TotalItems, len(summary.ItemsByType), summary.TemporaryIDMapStatus)
	return summary
}

// buildSafeOutputSummaryString creates a human-readable summary like "2 PRs, 1 comment, 1 review"
func buildSafeOutputSummaryString(details []SafeOutputTypeDetail) string {
	if len(details) == 0 {
		return "No items"
	}

	parts := make([]string, 0, len(details))
	for _, detail := range details {
		displayType := prettifySafeOutputType(detail.Type)
		parts = append(parts, fmt.Sprintf("%d %s", detail.Count, displayType))
	}
	return strings.Join(parts, ", ")
}

// prettifySafeOutputType converts safe output types to human-readable names
func prettifySafeOutputType(itemType string) string {
	typeMap := map[string]string{
		"create_pull_request":   "PR(s)",
		"create_issue":          "issue(s)",
		"add_comment":           "comment(s)",
		"add_issue_comment":     "issue comment(s)",
		"create_review":         "review(s)",
		"add_labels":            "label operation(s)",
		"close_issue":           "issue close(s)",
		"create_discussion":     "discussion(s)",
		"create_release":        "release(s)",
		"update_pull_request":   "PR update(s)",
		"merge_pull_request":    "PR merge(s)",
		"create_or_update_file": "file operation(s)",
	}
	if display, ok := typeMap[itemType]; ok {
		return display
	}
	return itemType
}

// buildMCPServerHealth creates MCP server health summary from gateway metrics and MCP failures
func buildMCPServerHealth(mcpToolUsage *MCPToolUsageData, mcpFailures []MCPFailureReport) *MCPServerHealth {
	if mcpToolUsage == nil && len(mcpFailures) == 0 {
		return nil
	}

	health := &MCPServerHealth{}

	// Track failed servers from MCPFailures
	failedServers := make(map[string]struct {
	})
	for _, failure := range mcpFailures {
		failedServers[failure.ServerName] = struct {
		}{}
	}
	health.FailedSvrs = len(failedServers)

	// Process server statistics from mcpToolUsage
	if mcpToolUsage != nil {
		appendMCPServerDetails(health, mcpToolUsage, failedServers)

		// Build slowest tool calls from individual call records (top 5)
		health.SlowestCalls = buildSlowestToolCalls(mcpToolUsage.ToolCalls, 5)
	}

	// Add failed servers that don't appear in stats
	appendMissingFailedServers(health, failedServers)

	finalizeMCPServerHealth(health)

	auditExpandedLog.Printf("Built MCP server health: %s, total_requests=%d, error_rate=%.1f%%",
		health.Summary, health.TotalRequests, health.ErrorRate)
	return health
}

// appendMCPServerDetails adds per-server health details from gateway metrics and
// accumulates request/error totals.
func appendMCPServerDetails(health *MCPServerHealth, mcpToolUsage *MCPToolUsageData, failedServers map[string]struct{}) {
	for _, server := range mcpToolUsage.Servers {
		health.TotalRequests += server.RequestCount
		health.TotalErrors += server.ErrorCount

		errorRate := safePercent(server.ErrorCount, server.RequestCount)

		status := "✅ healthy"
		if _, isFailed := failedServers[server.ServerName]; isFailed {
			status = "❌ failed"
		} else if errorRate > 10 {
			status = "⚠️ degraded"
		}

		health.Servers = append(health.Servers, MCPServerHealthDetail{
			MCPServerStatsBase: MCPServerStatsBase{
				ServerName:    server.ServerName,
				ToolCallCount: server.ToolCallCount,
				ErrorCount:    server.ErrorCount,
			},
			RequestCount: server.RequestCount,
			ErrorRate:    errorRate,
			ErrorRateStr: fmt.Sprintf("%.1f%%", errorRate),
			AvgLatency:   server.AvgDuration,
			Status:       status,
		})
	}
}

// appendMissingFailedServers adds failed servers that have no gateway statistics.
func appendMissingFailedServers(health *MCPServerHealth, failedServers map[string]struct{}) {
	for serverName := range failedServers {
		found := false
		for _, s := range health.Servers {
			if s.ServerName == serverName {
				found = true
				break
			}
		}
		if !found {
			health.Servers = append(health.Servers, MCPServerHealthDetail{
				MCPServerStatsBase: MCPServerStatsBase{ServerName: serverName},
				Status:             "❌ failed",
			})
		}
	}
}

// finalizeMCPServerHealth computes the health rollups, sorts servers and builds the summary.
func finalizeMCPServerHealth(health *MCPServerHealth) {
	health.TotalServers = len(health.Servers)

	// Count servers by status for accurate summary
	degradedCount := 0
	for _, s := range health.Servers {
		if strings.Contains(s.Status, "degraded") {
			degradedCount++
		}
	}
	health.DegradedSvrs = degradedCount
	health.HealthySvrs = health.TotalServers - health.FailedSvrs - health.DegradedSvrs

	// Calculate overall error rate
	health.ErrorRate = safePercent(health.TotalErrors, health.TotalRequests)

	// Sort servers by request count (highest first)
	slices.SortFunc(health.Servers, func(a, b MCPServerHealthDetail) int {
		if a.RequestCount > b.RequestCount {
			return -1
		}
		if a.RequestCount < b.RequestCount {
			return 1
		}
		return 0
	})

	// Build summary string
	health.Summary = fmt.Sprintf("%d server(s), %d healthy, %d degraded, %d failed",
		health.TotalServers, health.HealthySvrs, health.DegradedSvrs, health.FailedSvrs)
}

// buildSlowestToolCalls extracts the N slowest tool calls from the call records
func buildSlowestToolCalls(calls []MCPToolCall, topN int) []MCPSlowestToolCall {
	if len(calls) == 0 {
		return nil
	}

	// Filter calls that have duration information
	type callWithDuration struct {
		call     MCPToolCall
		duration time.Duration
	}

	var withDuration []callWithDuration
	for _, call := range calls {
		if call.Duration == "" {
			continue
		}
		d, err := time.ParseDuration(call.Duration)
		if err != nil {
			// Try parsing as bare number (milliseconds) only if no unit suffix present
			if !strings.ContainsAny(call.Duration, "smhμnuµ") {
				d, err = time.ParseDuration(call.Duration + "ms")
			}
			if err != nil {
				continue
			}
		}
		withDuration = append(withDuration, callWithDuration{call: call, duration: d})
	}

	// Sort by duration descending
	slices.SortFunc(withDuration, func(a, b callWithDuration) int {
		if a.duration > b.duration {
			return -1
		}
		if a.duration < b.duration {
			return 1
		}
		return 0
	})

	// Take top N
	if len(withDuration) > topN {
		withDuration = withDuration[:topN]
	}

	result := make([]MCPSlowestToolCall, 0, len(withDuration))
	for _, wd := range withDuration {
		result = append(result, MCPSlowestToolCall{
			ServerName: wd.call.ServerName,
			ToolName:   wd.call.ToolName,
			Duration:   timeutil.FormatDuration(wd.duration),
		})
	}

	return result
}

// extractMCPServerNamesFromAwInfo extracts MCP server names from aw_info.json steps metadata
// and returns them along with a boolean indicating whether any servers were found.
// We need to inspect the raw JSON since AwInfoSteps.MCPServers may not be
// deserialized as a map for all formats.
func extractMCPServerNamesFromAwInfo(logsPath string) ([]string, bool) {
	awInfoPath := findAwInfoPath(logsPath)
	if awInfoPath == "" {
		return nil, false
	}
	data, err := os.ReadFile(awInfoPath)
	if err != nil {
		return nil, false
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, false
	}

	stepsRaw, ok := raw["steps"]
	if !ok {
		return nil, false
	}

	var steps map[string]json.RawMessage
	if err := json.Unmarshal(stepsRaw, &steps); err != nil {
		return nil, false
	}

	mcpRaw, ok := steps["mcp_servers"]
	if !ok {
		return nil, false
	}

	var servers map[string]json.RawMessage
	if err := json.Unmarshal(mcpRaw, &servers); err != nil {
		return nil, false
	}

	names := sliceutil.SortedKeys(servers)
	return names, len(names) > 0
}
