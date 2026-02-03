package cli

import (
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
	"github.com/github/gh-aw/pkg/workflow"
)

var reportLog = logger.New("cli:logs_report")

// LogsData represents the complete structured data for logs output
type LogsData struct {
	Summary           LogsSummary                `json:"summary" console:"title:Workflow Logs Summary"`
	Runs              []RunData                  `json:"runs" console:"title:Workflow Logs Overview"`
	ToolUsage         []ToolUsageSummary         `json:"tool_usage,omitempty" console:"title:🛠️  Tool Usage Summary,omitempty"`
	MCPToolUsage      *MCPToolUsageSummary       `json:"mcp_tool_usage,omitempty" console:"title:🔧 MCP Tool Usage,omitempty"`
	ErrorsAndWarnings []ErrorSummary             `json:"errors_and_warnings,omitempty" console:"title:Errors and Warnings,omitempty"`
	MissingTools      []MissingToolSummary       `json:"missing_tools,omitempty" console:"title:🛠️  Missing Tools Summary,omitempty"`
	MissingData       []MissingDataSummary       `json:"missing_data,omitempty" console:"title:📊 Missing Data Summary,omitempty"`
	MCPFailures       []MCPFailureSummary        `json:"mcp_failures,omitempty" console:"title:⚠️  MCP Server Failures,omitempty"`
	AccessLog         *AccessLogSummary          `json:"access_log,omitempty" console:"title:Access Log Analysis,omitempty"`
	FirewallLog       *FirewallLogSummary        `json:"firewall_log,omitempty" console:"title:🔥 Firewall Log Analysis,omitempty"`
	RedactedDomains   *RedactedDomainsLogSummary `json:"redacted_domains,omitempty" console:"title:🔒 Redacted URL Domains,omitempty"`
	Continuation      *ContinuationData          `json:"continuation,omitempty" console:"-"`
	LogsLocation      string                     `json:"logs_location" console:"-"`
}

// ContinuationData provides parameters to continue querying when timeout is reached
type ContinuationData struct {
	Message      string `json:"message"`
	WorkflowName string `json:"workflow_name,omitempty"`
	Count        int    `json:"count,omitempty"`
	StartDate    string `json:"start_date,omitempty"`
	EndDate      string `json:"end_date,omitempty"`
	Engine       string `json:"engine,omitempty"`
	Branch       string `json:"branch,omitempty"`
	AfterRunID   int64  `json:"after_run_id,omitempty"`
	BeforeRunID  int64  `json:"before_run_id,omitempty"`
	Timeout      int    `json:"timeout,omitempty"`
}

// LogsSummary contains aggregate metrics across all runs
type LogsSummary struct {
	TotalRuns         int     `json:"total_runs" console:"header:Total Runs"`
	TotalDuration     string  `json:"total_duration" console:"header:Total Duration"`
	TotalTokens       int     `json:"total_tokens" console:"header:Total Tokens,format:number"`
	TotalCost         float64 `json:"total_cost" console:"header:Total Cost,format:cost"`
	TotalTurns        int     `json:"total_turns" console:"header:Total Turns"`
	TotalErrors       int     `json:"total_errors" console:"header:Total Errors"`
	TotalWarnings     int     `json:"total_warnings" console:"header:Total Warnings"`
	TotalMissingTools int     `json:"total_missing_tools" console:"header:Total Missing Tools"`
	TotalMissingData  int     `json:"total_missing_data" console:"header:Total Missing Data"`
}

// RunData contains information about a single workflow run
type RunData struct {
	DatabaseID       int64     `json:"database_id" console:"header:Run ID"`
	Number           int       `json:"number" console:"-"`
	WorkflowName     string    `json:"workflow_name" console:"header:Workflow"`
	WorkflowPath     string    `json:"workflow_path" console:"-"`
	Agent            string    `json:"agent,omitempty" console:"header:Agent,omitempty"`
	Status           string    `json:"status" console:"header:Status"`
	Conclusion       string    `json:"conclusion,omitempty" console:"-"`
	Duration         string    `json:"duration,omitempty" console:"header:Duration,omitempty"`
	TokenUsage       int       `json:"token_usage,omitempty" console:"header:Tokens,format:number,omitempty"`
	EstimatedCost    float64   `json:"estimated_cost,omitempty" console:"header:Cost ($),format:cost,omitempty"`
	Turns            int       `json:"turns,omitempty" console:"header:Turns,omitempty"`
	ErrorCount       int       `json:"error_count" console:"header:Errors"`
	WarningCount     int       `json:"warning_count" console:"header:Warnings"`
	MissingToolCount int       `json:"missing_tool_count" console:"header:Missing Tools"`
	MissingDataCount int       `json:"missing_data_count" console:"header:Missing Data"`
	CreatedAt        time.Time `json:"created_at" console:"header:Created"`
	StartedAt        time.Time `json:"started_at,omitempty" console:"-"`
	UpdatedAt        time.Time `json:"updated_at,omitempty" console:"-"`
	URL              string    `json:"url" console:"-"`
	LogsPath         string    `json:"logs_path" console:"header:Logs Path"`
	Event            string    `json:"event" console:"-"`
	Branch           string    `json:"branch" console:"-"`
}

// ToolUsageSummary contains aggregated tool usage statistics
type ToolUsageSummary struct {
	Name          string `json:"name" console:"header:Tool"`
	TotalCalls    int    `json:"total_calls" console:"header:Total Calls,format:number"`
	Runs          int    `json:"runs" console:"header:Runs"` // Number of runs that used this tool
	MaxOutputSize int    `json:"max_output_size,omitempty" console:"header:Max Output,format:filesize,default:N/A,omitempty"`
	MaxDuration   string `json:"max_duration,omitempty" console:"header:Max Duration,default:N/A,omitempty"`
}

// ErrorSummary contains aggregated error/warning statistics
type ErrorSummary struct {
	Type         string `json:"type" console:"header:Type"`
	Message      string `json:"message" console:"header:Message,maxlen:80"`
	Count        int    `json:"count" console:"header:Occurrences"`
	Engine       string `json:"engine,omitempty" console:"header:Engine,omitempty"`
	RunID        int64  `json:"run_id" console:"header:Sample Run"`
	RunURL       string `json:"run_url" console:"-"`
	WorkflowName string `json:"workflow_name,omitempty" console:"-"`
	PatternID    string `json:"pattern_id,omitempty" console:"-"`
}

// AccessLogSummary contains aggregated access log analysis
type AccessLogSummary struct {
	TotalRequests  int                        `json:"total_requests" console:"header:Total Requests"`
	AllowedCount   int                        `json:"allowed_count" console:"header:Allowed"`
	BlockedCount   int                        `json:"blocked_count" console:"header:Blocked"`
	AllowedDomains []string                   `json:"allowed_domains" console:"-"`
	BlockedDomains []string                   `json:"blocked_domains" console:"-"`
	ByWorkflow     map[string]*DomainAnalysis `json:"by_workflow,omitempty" console:"-"`
}

// FirewallLogSummary contains aggregated firewall log data
type FirewallLogSummary struct {
	TotalRequests    int                           `json:"total_requests" console:"header:Total Requests"`
	AllowedRequests  int                           `json:"allowed_requests" console:"header:Allowed"`
	BlockedRequests  int                           `json:"blocked_requests" console:"header:Blocked"`
	AllowedDomains   []string                      `json:"allowed_domains" console:"-"`
	BlockedDomains   []string                      `json:"blocked_domains" console:"-"`
	RequestsByDomain map[string]DomainRequestStats `json:"requests_by_domain,omitempty" console:"-"`
	ByWorkflow       map[string]*FirewallAnalysis  `json:"by_workflow,omitempty" console:"-"`
}

// buildLogsData creates structured logs data from processed runs
func buildLogsData(processedRuns []ProcessedRun, outputDir string, continuation *ContinuationData) LogsData {
	reportLog.Printf("Building logs data from %d processed runs", len(processedRuns))

	// Build summary
	var totalDuration time.Duration
	var totalTokens int
	var totalCost float64
	var totalTurns int
	var totalErrors int
	var totalWarnings int
	var totalMissingTools int
	var totalMissingData int

	// Build runs data
	// Initialize as empty slice to ensure JSON marshals to [] instead of null
	runs := make([]RunData, 0, len(processedRuns))
	for _, pr := range processedRuns {
		run := pr.Run

		if run.Duration > 0 {
			totalDuration += run.Duration
		}
		totalTokens += run.TokenUsage
		totalCost += run.EstimatedCost
		totalTurns += run.Turns
		totalErrors += run.ErrorCount
		totalWarnings += run.WarningCount
		totalMissingTools += run.MissingToolCount
		totalMissingData += run.MissingDataCount

		// Extract agent/engine ID from aw_info.json
		agentID := ""
		awInfoPath := filepath.Join(run.LogsPath, "aw_info.json")
		if info, err := parseAwInfo(awInfoPath, false); err == nil && info != nil {
			agentID = info.EngineID
		}

		runData := RunData{
			DatabaseID:       run.DatabaseID,
			Number:           run.Number,
			WorkflowName:     run.WorkflowName,
			WorkflowPath:     run.WorkflowPath,
			Agent:            agentID,
			Status:           run.Status,
			Conclusion:       run.Conclusion,
			TokenUsage:       run.TokenUsage,
			EstimatedCost:    run.EstimatedCost,
			Turns:            run.Turns,
			ErrorCount:       run.ErrorCount,
			WarningCount:     run.WarningCount,
			MissingToolCount: run.MissingToolCount,
			MissingDataCount: run.MissingDataCount,
			CreatedAt:        run.CreatedAt,
			StartedAt:        run.StartedAt,
			UpdatedAt:        run.UpdatedAt,
			URL:              run.URL,
			LogsPath:         run.LogsPath,
			Event:            run.Event,
			Branch:           run.HeadBranch,
		}
		if run.Duration > 0 {
			runData.Duration = timeutil.FormatDuration(run.Duration)
		}
		runs = append(runs, runData)
	}

	summary := LogsSummary{
		TotalRuns:         len(processedRuns),
		TotalDuration:     timeutil.FormatDuration(totalDuration),
		TotalTokens:       totalTokens,
		TotalCost:         totalCost,
		TotalTurns:        totalTurns,
		TotalErrors:       totalErrors,
		TotalWarnings:     totalWarnings,
		TotalMissingTools: totalMissingTools,
		TotalMissingData:  totalMissingData,
	}

	// Build tool usage summary
	toolUsage := buildToolUsageSummary(processedRuns)

	// Build combined error and warning summary
	errorsAndWarnings := buildCombinedErrorsSummary(processedRuns)

	// Build missing tools summary
	missingTools := buildMissingToolsSummary(processedRuns)

	// Build missing data summary
	missingData := buildMissingDataSummary(processedRuns)

	// Build MCP failures summary
	mcpFailures := buildMCPFailuresSummary(processedRuns)

	// Build MCP tool usage summary
	mcpToolUsage := buildMCPToolUsageSummary(processedRuns)

	// Build access log summary
	accessLog := buildAccessLogSummary(processedRuns)

	// Build firewall log summary
	firewallLog := buildFirewallLogSummary(processedRuns)

	// Build redacted domains summary
	redactedDomains := buildRedactedDomainsSummary(processedRuns)

	absOutputDir, _ := filepath.Abs(outputDir)

	return LogsData{
		Summary:           summary,
		Runs:              runs,
		ToolUsage:         toolUsage,
		MCPToolUsage:      mcpToolUsage,
		ErrorsAndWarnings: errorsAndWarnings,
		MissingTools:      missingTools,
		MissingData:       missingData,
		MCPFailures:       mcpFailures,
		AccessLog:         accessLog,
		FirewallLog:       firewallLog,
		RedactedDomains:   redactedDomains,
		Continuation:      continuation,
		LogsLocation:      absOutputDir,
	}
}

// isValidToolName checks if a tool name appears to be valid
// Filters out single words, common words, and other garbage that shouldn't be tools
func isValidToolName(toolName string) bool {
	name := strings.TrimSpace(toolName)

	// Filter out empty names
	if name == "" || name == "-" {
		return false
	}

	// Filter out single character names
	if len(name) == 1 {
		return false
	}

	// Filter out common English words that are likely from error messages
	commonWords := map[string]bool{
		"calls": true, "to": true, "for": true, "the": true, "a": true, "an": true,
		"is": true, "are": true, "was": true, "were": true, "be": true, "been": true,
		"have": true, "has": true, "had": true, "do": true, "does": true, "did": true,
		"will": true, "would": true, "could": true, "should": true, "may": true, "might": true,
		"Testing": true, "multiple": true, "launches": true, "command": true, "invocation": true,
		"with": true, "from": true, "by": true, "at": true, "in": true, "on": true,
	}

	if commonWords[name] {
		return false
	}

	// Tool names should typically contain underscores, hyphens, or be camelCase
	// or be all lowercase. Single words without these patterns are suspect.
	hasUnderscore := strings.Contains(name, "_")
	hasHyphen := strings.Contains(name, "-")
	hasCapital := strings.ToLower(name) != name

	// If it's a single word with no underscores/hyphens and is lowercase and short,
	// it's likely a fragment
	words := strings.Fields(name)
	if len(words) == 1 && !hasUnderscore && !hasHyphen && len(name) < 10 && !hasCapital {
		// Could be a fragment - be conservative and reject if it's a common word
		return false
	}

	return true
}

// buildToolUsageSummary aggregates tool usage across all runs
// Filters out invalid tool names that appear to be fragments or garbage
func buildToolUsageSummary(processedRuns []ProcessedRun) []ToolUsageSummary {
	toolStats := make(map[string]*ToolUsageSummary)

	for _, pr := range processedRuns {
		// Extract metrics from run's logs
		metrics := ExtractLogMetricsFromRun(pr)

		// Track which runs use each tool
		toolRunTracker := make(map[string]bool)

		for _, toolCall := range metrics.ToolCalls {
			displayKey := workflow.PrettifyToolName(toolCall.Name)

			// Filter out invalid tool names
			if !isValidToolName(displayKey) {
				continue
			}

			toolRunTracker[displayKey] = true

			if existing, exists := toolStats[displayKey]; exists {
				existing.TotalCalls += toolCall.CallCount
				if toolCall.MaxOutputSize > existing.MaxOutputSize {
					existing.MaxOutputSize = toolCall.MaxOutputSize
				}
				if toolCall.MaxDuration > 0 {
					maxDur := timeutil.FormatDuration(toolCall.MaxDuration)
					if existing.MaxDuration == "" || toolCall.MaxDuration > parseDurationString(existing.MaxDuration) {
						existing.MaxDuration = maxDur
					}
				}
			} else {
				info := &ToolUsageSummary{
					Name:          displayKey,
					TotalCalls:    toolCall.CallCount,
					MaxOutputSize: toolCall.MaxOutputSize,
					Runs:          0, // Will be incremented below
				}
				if toolCall.MaxDuration > 0 {
					info.MaxDuration = timeutil.FormatDuration(toolCall.MaxDuration)
				}
				toolStats[displayKey] = info
			}
		}

		// Increment run count for tools used in this run
		for toolName := range toolRunTracker {
			if stat, exists := toolStats[toolName]; exists {
				stat.Runs++
			}
		}
	}

	var result []ToolUsageSummary
	for _, info := range toolStats {
		result = append(result, *info)
	}

	// Sort by total calls descending
	sort.Slice(result, func(i, j int) bool {
		return result[i].TotalCalls > result[j].TotalCalls
	})

	return result
}

// addUniqueWorkflow adds a workflow to the list if it's not already present
func addUniqueWorkflow(workflows []string, workflow string) []string {
	for _, wf := range workflows {
		if wf == workflow {
			return workflows
		}
	}
	return append(workflows, workflow)
}

// aggregateSummaryItems is a generic helper that aggregates items from processed runs into summaries
// It handles the common pattern of grouping by key, counting occurrences, tracking unique workflows, and collecting run IDs
func aggregateSummaryItems[TItem any, TSummary any](
	processedRuns []ProcessedRun,
	getItems func(ProcessedRun) []TItem,
	getKey func(TItem) string,
	createSummary func(TItem) *TSummary,
	updateSummary func(*TSummary, TItem),
	finalizeSummary func(*TSummary),
) []TSummary {
	summaryMap := make(map[string]*TSummary)

	// Aggregate items from all runs
	for _, pr := range processedRuns {
		for _, item := range getItems(pr) {
			key := getKey(item)
			if summary, exists := summaryMap[key]; exists {
				updateSummary(summary, item)
			} else {
				summaryMap[key] = createSummary(item)
			}
		}
	}

	// Convert map to slice and finalize each summary
	var result []TSummary
	for _, summary := range summaryMap {
		finalizeSummary(summary)
		result = append(result, *summary)
	}

	return result
}

// buildMissingToolsSummary aggregates missing tools across all runs
func buildMissingToolsSummary(processedRuns []ProcessedRun) []MissingToolSummary {
	result := aggregateSummaryItems(
		processedRuns,
		// getItems: extract missing tools from each run
		func(pr ProcessedRun) []MissingToolReport {
			return pr.MissingTools
		},
		// getKey: use tool name as the aggregation key
		func(tool MissingToolReport) string {
			return tool.Tool
		},
		// createSummary: create new summary for first occurrence
		func(tool MissingToolReport) *MissingToolSummary {
			return &MissingToolSummary{
				Tool:        tool.Tool,
				Count:       1,
				Workflows:   []string{tool.WorkflowName},
				FirstReason: tool.Reason,
				RunIDs:      []int64{tool.RunID},
			}
		},
		// updateSummary: update existing summary with new occurrence
		func(summary *MissingToolSummary, tool MissingToolReport) {
			summary.Count++
			summary.Workflows = addUniqueWorkflow(summary.Workflows, tool.WorkflowName)
			summary.RunIDs = append(summary.RunIDs, tool.RunID)
		},
		// finalizeSummary: populate display fields for console rendering
		func(summary *MissingToolSummary) {
			summary.WorkflowsDisplay = strings.Join(summary.Workflows, ", ")
			summary.FirstReasonDisplay = summary.FirstReason
		},
	)

	// Sort by count descending
	sort.Slice(result, func(i, j int) bool {
		return result[i].Count > result[j].Count
	})

	return result
}

// buildMissingDataSummary aggregates missing data across all runs
func buildMissingDataSummary(processedRuns []ProcessedRun) []MissingDataSummary {
	result := aggregateSummaryItems(
		processedRuns,
		// getItems: extract missing data from each run
		func(pr ProcessedRun) []MissingDataReport {
			return pr.MissingData
		},
		// getKey: use data type as the aggregation key
		func(data MissingDataReport) string {
			return data.DataType
		},
		// createSummary: create new summary for first occurrence
		func(data MissingDataReport) *MissingDataSummary {
			return &MissingDataSummary{
				DataType:    data.DataType,
				Count:       1,
				Workflows:   []string{data.WorkflowName},
				FirstReason: data.Reason,
				RunIDs:      []int64{data.RunID},
			}
		},
		// updateSummary: update existing summary with new occurrence
		func(summary *MissingDataSummary, data MissingDataReport) {
			summary.Count++
			summary.Workflows = addUniqueWorkflow(summary.Workflows, data.WorkflowName)
			summary.RunIDs = append(summary.RunIDs, data.RunID)
		},
		// finalizeSummary: populate display fields for console rendering
		func(summary *MissingDataSummary) {
			summary.WorkflowsDisplay = strings.Join(summary.Workflows, ", ")
			summary.FirstReasonDisplay = summary.FirstReason
		},
	)

	// Sort by count descending
	sort.Slice(result, func(i, j int) bool {
		return result[i].Count > result[j].Count
	})

	return result
}

// buildMCPFailuresSummary aggregates MCP failures across all runs
func buildMCPFailuresSummary(processedRuns []ProcessedRun) []MCPFailureSummary {
	result := aggregateSummaryItems(
		processedRuns,
		// getItems: extract MCP failures from each run
		func(pr ProcessedRun) []MCPFailureReport {
			return pr.MCPFailures
		},
		// getKey: use server name as the aggregation key
		func(failure MCPFailureReport) string {
			return failure.ServerName
		},
		// createSummary: create new summary for first occurrence
		func(failure MCPFailureReport) *MCPFailureSummary {
			return &MCPFailureSummary{
				ServerName: failure.ServerName,
				Count:      1,
				Workflows:  []string{failure.WorkflowName},
				RunIDs:     []int64{failure.RunID},
			}
		},
		// updateSummary: update existing summary with new occurrence
		func(summary *MCPFailureSummary, failure MCPFailureReport) {
			summary.Count++
			summary.Workflows = addUniqueWorkflow(summary.Workflows, failure.WorkflowName)
			summary.RunIDs = append(summary.RunIDs, failure.RunID)
		},
		// finalizeSummary: populate display fields for console rendering
		func(summary *MCPFailureSummary) {
			summary.WorkflowsDisplay = strings.Join(summary.Workflows, ", ")
		},
	)

	// Sort by count descending
	sort.Slice(result, func(i, j int) bool {
		return result[i].Count > result[j].Count
	})

	return result
}

// domainAggregation holds the result of aggregating domain statistics
type domainAggregation struct {
	allAllowedDomains map[string]bool
	allBlockedDomains map[string]bool
	totalRequests     int
	allowedCount      int
	blockedCount      int
}

// aggregateDomainStats aggregates domain statistics across runs
// This is a shared helper for both access log and firewall log summaries
func aggregateDomainStats(processedRuns []ProcessedRun, getAnalysis func(*ProcessedRun) (allowedDomains, blockedDomains []string, totalRequests, allowedCount, blockedCount int, exists bool)) *domainAggregation {
	agg := &domainAggregation{
		allAllowedDomains: make(map[string]bool),
		allBlockedDomains: make(map[string]bool),
	}

	for _, pr := range processedRuns {
		allowedDomains, blockedDomains, totalRequests, allowedCount, blockedCount, exists := getAnalysis(&pr)
		if !exists {
			continue
		}

		agg.totalRequests += totalRequests
		agg.allowedCount += allowedCount
		agg.blockedCount += blockedCount

		for _, domain := range allowedDomains {
			agg.allAllowedDomains[domain] = true
		}
		for _, domain := range blockedDomains {
			agg.allBlockedDomains[domain] = true
		}
	}

	return agg
}

// convertDomainsToSortedSlices converts domain maps to sorted slices
func convertDomainsToSortedSlices(allowedMap, blockedMap map[string]bool) (allowed, blocked []string) {
	for domain := range allowedMap {
		allowed = append(allowed, domain)
	}
	sort.Strings(allowed)

	for domain := range blockedMap {
		blocked = append(blocked, domain)
	}
	sort.Strings(blocked)

	return allowed, blocked
}

// buildAccessLogSummary aggregates access log data across all runs
func buildAccessLogSummary(processedRuns []ProcessedRun) *AccessLogSummary {
	byWorkflow := make(map[string]*DomainAnalysis)

	// Use shared aggregation helper
	agg := aggregateDomainStats(processedRuns, func(pr *ProcessedRun) ([]string, []string, int, int, int, bool) {
		if pr.AccessAnalysis == nil {
			return nil, nil, 0, 0, 0, false
		}
		byWorkflow[pr.Run.WorkflowName] = pr.AccessAnalysis
		return pr.AccessAnalysis.AllowedDomains,
			pr.AccessAnalysis.BlockedDomains,
			pr.AccessAnalysis.TotalRequests,
			pr.AccessAnalysis.AllowedCount,
			pr.AccessAnalysis.BlockedCount,
			true
	})

	if agg.totalRequests == 0 {
		return nil
	}

	allowedDomains, blockedDomains := convertDomainsToSortedSlices(agg.allAllowedDomains, agg.allBlockedDomains)

	return &AccessLogSummary{
		TotalRequests:  agg.totalRequests,
		AllowedCount:   agg.allowedCount,
		BlockedCount:   agg.blockedCount,
		AllowedDomains: allowedDomains,
		BlockedDomains: blockedDomains,
		ByWorkflow:     byWorkflow,
	}
}

// buildFirewallLogSummary aggregates firewall log data across all runs
func buildFirewallLogSummary(processedRuns []ProcessedRun) *FirewallLogSummary {
	allRequestsByDomain := make(map[string]DomainRequestStats)
	byWorkflow := make(map[string]*FirewallAnalysis)

	// Use shared aggregation helper
	agg := aggregateDomainStats(processedRuns, func(pr *ProcessedRun) ([]string, []string, int, int, int, bool) {
		if pr.FirewallAnalysis == nil {
			return nil, nil, 0, 0, 0, false
		}
		byWorkflow[pr.Run.WorkflowName] = pr.FirewallAnalysis

		// Aggregate request stats by domain (firewall-specific)
		for domain, stats := range pr.FirewallAnalysis.RequestsByDomain {
			existing := allRequestsByDomain[domain]
			existing.Allowed += stats.Allowed
			existing.Blocked += stats.Blocked
			allRequestsByDomain[domain] = existing
		}

		return pr.FirewallAnalysis.AllowedDomains,
			pr.FirewallAnalysis.BlockedDomains,
			pr.FirewallAnalysis.TotalRequests,
			pr.FirewallAnalysis.AllowedRequests,
			pr.FirewallAnalysis.BlockedRequests,
			true
	})

	if agg.totalRequests == 0 {
		return nil
	}

	allowedDomains, blockedDomains := convertDomainsToSortedSlices(agg.allAllowedDomains, agg.allBlockedDomains)

	return &FirewallLogSummary{
		TotalRequests:    agg.totalRequests,
		AllowedRequests:  agg.allowedCount,
		BlockedRequests:  agg.blockedCount,
		AllowedDomains:   allowedDomains,
		BlockedDomains:   blockedDomains,
		RequestsByDomain: allRequestsByDomain,
		ByWorkflow:       byWorkflow,
	}
}

// buildRedactedDomainsSummary aggregates redacted domains data across all runs
func buildRedactedDomainsSummary(processedRuns []ProcessedRun) *RedactedDomainsLogSummary {
	allDomainsSet := make(map[string]bool)
	byWorkflow := make(map[string]*RedactedDomainsAnalysis)
	hasData := false

	for _, pr := range processedRuns {
		if pr.RedactedDomainsAnalysis == nil {
			continue
		}
		hasData = true
		byWorkflow[pr.Run.WorkflowName] = pr.RedactedDomainsAnalysis

		// Collect all unique domains
		for _, domain := range pr.RedactedDomainsAnalysis.Domains {
			allDomainsSet[domain] = true
		}
	}

	if !hasData {
		return nil
	}

	// Convert set to sorted slice
	var allDomains []string
	for domain := range allDomainsSet {
		allDomains = append(allDomains, domain)
	}
	sort.Strings(allDomains)

	return &RedactedDomainsLogSummary{
		TotalDomains: len(allDomains),
		Domains:      allDomains,
		ByWorkflow:   byWorkflow,
	}
}

// buildMCPToolUsageSummary aggregates MCP tool usage data across all runs
func buildMCPToolUsageSummary(processedRuns []ProcessedRun) *MCPToolUsageSummary {
	reportLog.Printf("Building MCP tool usage summary from %d processed runs", len(processedRuns))

	// Maps for aggregating data
	toolSummaryMap := make(map[string]*MCPToolSummary) // Key: serverName:toolName
	serverStatsMap := make(map[string]*MCPServerStats) // Key: serverName
	var allToolCalls []MCPToolCall

	// Aggregate data from all runs
	for _, pr := range processedRuns {
		if pr.MCPToolUsage == nil {
			continue
		}

		// Aggregate tool calls
		allToolCalls = append(allToolCalls, pr.MCPToolUsage.ToolCalls...)

		// Aggregate tool summaries
		for _, summary := range pr.MCPToolUsage.Summary {
			key := summary.ServerName + ":" + summary.ToolName

			if existing, exists := toolSummaryMap[key]; exists {
				// Store previous count before updating
				prevCallCount := existing.CallCount

				// Merge with existing summary
				existing.CallCount += summary.CallCount
				existing.TotalInputSize += summary.TotalInputSize
				existing.TotalOutputSize += summary.TotalOutputSize

				// Update max sizes
				if summary.MaxInputSize > existing.MaxInputSize {
					existing.MaxInputSize = summary.MaxInputSize
				}
				if summary.MaxOutputSize > existing.MaxOutputSize {
					existing.MaxOutputSize = summary.MaxOutputSize
				}

				// Update error count
				existing.ErrorCount += summary.ErrorCount

				// Recalculate average duration (weighted)
				if summary.AvgDuration != "" && existing.CallCount > 0 {
					existingDur := parseDurationString(existing.AvgDuration)
					newDur := parseDurationString(summary.AvgDuration)
					// Weight by call counts using previous count
					weightedDur := (existingDur*time.Duration(prevCallCount) + newDur*time.Duration(summary.CallCount)) / time.Duration(existing.CallCount)
					existing.AvgDuration = timeutil.FormatDuration(weightedDur)
				}

				// Update max duration
				if summary.MaxDuration != "" {
					maxDur := parseDurationString(summary.MaxDuration)
					existingMaxDur := parseDurationString(existing.MaxDuration)
					if maxDur > existingMaxDur {
						existing.MaxDuration = summary.MaxDuration
					}
				}
			} else {
				// Create new summary entry (copy to avoid mutation)
				newSummary := summary
				toolSummaryMap[key] = &newSummary
			}
		}

		// Aggregate server stats
		for _, serverStats := range pr.MCPToolUsage.Servers {
			if existing, exists := serverStatsMap[serverStats.ServerName]; exists {
				// Store previous count before updating
				prevRequestCount := existing.RequestCount

				// Merge with existing stats
				existing.RequestCount += serverStats.RequestCount
				existing.ToolCallCount += serverStats.ToolCallCount
				existing.TotalInputSize += serverStats.TotalInputSize
				existing.TotalOutputSize += serverStats.TotalOutputSize
				existing.ErrorCount += serverStats.ErrorCount

				// Recalculate average duration (weighted)
				if serverStats.AvgDuration != "" && existing.RequestCount > 0 {
					existingDur := parseDurationString(existing.AvgDuration)
					newDur := parseDurationString(serverStats.AvgDuration)
					// Weight by request counts using previous count
					weightedDur := (existingDur*time.Duration(prevRequestCount) + newDur*time.Duration(serverStats.RequestCount)) / time.Duration(existing.RequestCount)
					existing.AvgDuration = timeutil.FormatDuration(weightedDur)
				}
			} else {
				// Create new server stats entry (copy to avoid mutation)
				newStats := serverStats
				serverStatsMap[serverStats.ServerName] = &newStats
			}
		}
	}

	// Return nil if no MCP tool usage data was found
	if len(toolSummaryMap) == 0 && len(serverStatsMap) == 0 {
		return nil
	}

	// Convert maps to slices
	var summaries []MCPToolSummary
	for _, summary := range toolSummaryMap {
		summaries = append(summaries, *summary)
	}

	var servers []MCPServerStats
	for _, stats := range serverStatsMap {
		servers = append(servers, *stats)
	}

	// Sort summaries by server name, then tool name
	sort.Slice(summaries, func(i, j int) bool {
		if summaries[i].ServerName != summaries[j].ServerName {
			return summaries[i].ServerName < summaries[j].ServerName
		}
		return summaries[i].ToolName < summaries[j].ToolName
	})

	// Sort servers by name
	sort.Slice(servers, func(i, j int) bool {
		return servers[i].ServerName < servers[j].ServerName
	})

	reportLog.Printf("Built MCP tool usage summary: %d tool summaries, %d servers, %d total tool calls",
		len(summaries), len(servers), len(allToolCalls))

	return &MCPToolUsageSummary{
		Summary:   summaries,
		Servers:   servers,
		ToolCalls: allToolCalls,
	}
}

// logErrorAggregator and related functions have been removed since error patterns are no longer supported

// buildCombinedErrorsSummary has been removed since error patterns are no longer supported
func buildCombinedErrorsSummary(processedRuns []ProcessedRun) []ErrorSummary {
	// Return empty slice since error patterns have been removed
	return []ErrorSummary{}
}

// renderLogsJSON outputs the logs data as JSON
func renderLogsJSON(data LogsData) error {
	reportLog.Printf("Rendering logs data as JSON: %d runs", data.Summary.TotalRuns)
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(data)
}

// writeSummaryFile writes the logs data to a JSON file
// This file contains complete metrics and run data for all downloaded workflow runs.
// It's primarily designed for campaign orchestrators to access workflow execution data
// in subsequent steps without needing GitHub CLI access.
//
// The summary file includes:
//   - Aggregate metrics (total runs, tokens, costs, errors, warnings)
//   - Individual run details with metrics and metadata
//   - Tool usage statistics
//   - Error and warning summaries
//   - Network access logs (if available)
//   - Firewall logs (if available)
func writeSummaryFile(path string, data LogsData, verbose bool) error {
	reportLog.Printf("Writing summary file: path=%s, runs=%d", path, data.Summary.TotalRuns)

	// Create parent directory if it doesn't exist
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory for summary file: %w", err)
	}

	// Marshal to JSON with indentation for readability
	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal logs data to JSON: %w", err)
	}

	// Write to file
	if err := os.WriteFile(path, jsonData, 0644); err != nil {
		return fmt.Errorf("failed to write summary file: %w", err)
	}

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("Wrote summary to %s", path)))
	}

	reportLog.Printf("Successfully wrote summary file: %s", path)
	return nil
}

// renderLogsConsole outputs the logs data as formatted console output
func renderLogsConsole(data LogsData) {
	reportLog.Printf("Rendering logs data to console: %d runs, %d errors, %d warnings",
		data.Summary.TotalRuns, data.Summary.TotalErrors, data.Summary.TotalWarnings)

	// Use unified console rendering for the entire logs data structure
	fmt.Print(console.RenderStruct(data))

	// Display concise summary at the end
	fmt.Fprintln(os.Stderr, "") // Blank line for spacing
	fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("✓ Downloaded %d workflow logs to %s", data.Summary.TotalRuns, data.LogsLocation)))

	// Show key metrics in a concise format
	if data.Summary.TotalErrors > 0 || data.Summary.TotalWarnings > 0 {
		fmt.Fprintf(os.Stderr, "  %s %d errors, %d warnings across %d runs\n",
			console.FormatInfoMessage("•"),
			data.Summary.TotalErrors,
			data.Summary.TotalWarnings,
			data.Summary.TotalRuns)
	}

	if len(data.ToolUsage) > 0 {
		fmt.Fprintf(os.Stderr, "  %s %d unique tools used\n",
			console.FormatInfoMessage("•"),
			len(data.ToolUsage))
	}
}
