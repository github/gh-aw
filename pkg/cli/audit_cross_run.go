package cli

import (
	"encoding/json"
	"time"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/sliceutil"
	"github.com/github/gh-aw/pkg/stats"
)

var auditCrossRunLog = logger.New("cli:audit_cross_run")

// mcpErrorRateThreshold is the error-rate above which an MCP server is flagged as unreliable.
const mcpErrorRateThreshold = 0.10

// mcpConnectionRateThreshold is the minimum fraction of runs a server must appear in
// before it is flagged as unreliable due to low connectivity.
const mcpConnectionRateThreshold = 0.75

// spikeDetectionMultiplier is the ratio above which a run's token usage is
// flagged as a spike relative to the cross-run average (e.g., 2.0 → >2x avg).
const spikeDetectionMultiplier = 2.0

// CrossRunAuditReport represents aggregated audit data across multiple workflow runs.
// It includes firewall analysis, metrics trends, MCP server health, and error trends.
type CrossRunAuditReport struct {
	RunsAnalyzed        int                         `json:"runs_analyzed"`
	RunsWithData        int                         `json:"runs_with_data"`
	RunsWithoutData     int                         `json:"runs_without_data"`
	Summary             CrossRunSummary             `json:"summary"`
	MetricsTrend        MetricsTrendData            `json:"metrics_trend"`
	MCPHealth           []MCPServerCrossRunHealth   `json:"mcp_health,omitempty"`
	ErrorTrend          ErrorTrendData              `json:"error_trend"`
	DomainInventory     []DomainInventoryEntry      `json:"domain_inventory"`
	PerRunBreakdown     []PerRunFirewallBreakdown   `json:"per_run_breakdown"`
	Drain3Insights      []ObservabilityInsight      `json:"drain3_insights,omitempty"`
	ClusterAnalysis     *ClusterAnalysis            `json:"cluster_analysis,omitempty"`
	GitHubAPIRateLimit  *GitHubAPIRateLimitReport   `json:"github_api_rate_limit,omitempty"`
	GitHubAPIRateLimits []*GitHubAPIRateLimitReport `json:"github_api_rate_limits,omitempty"`
}

// CrossRunSummary provides top-level statistics across all analyzed runs.
type CrossRunSummary struct {
	TotalRequests   int     `json:"total_requests"`
	TotalAllowed    int     `json:"total_allowed"`
	TotalBlocked    int     `json:"total_blocked"`
	OverallDenyRate float64 `json:"overall_deny_rate"` // 0.0–1.0
	UniqueDomains   int     `json:"unique_domains"`
}

// MetricsTrendData contains aggregated token, turn, and duration statistics
// across multiple runs, with spike detection for anomalous runs.
//
// Token counts (MinTokens, MaxTokens, AvgTokens) are stored as int to preserve
// integer semantics consistent with the source data; MedianTokens and StdDevTokens
// use float64 because statistical measures of integer quantities can be fractional.
//
// Duration fields only aggregate runs where timing data was recorded (duration > 0),
// so the duration statistics may cover fewer runs than the token/turn statistics.
// All stddev fields use the sample standard deviation (Bessel's correction).
type MetricsTrendData struct {
	TotalTokens  int     `json:"total_tokens"`
	AvgTokens    int     `json:"avg_tokens"`
	MedianTokens float64 `json:"median_tokens"` // float64: median of integer counts can be fractional
	StdDevTokens float64 `json:"stddev_tokens"` // float64: stddev is always fractional
	MinTokens    int     `json:"min_tokens"`
	MaxTokens    int     `json:"max_tokens"`
	TotalTurns   int     `json:"total_turns"`
	AvgTurns     float64 `json:"avg_turns"`
	MedianTurns  float64 `json:"median_turns"`
	StdDevTurns  float64 `json:"stddev_turns"`
	MaxTurns     int     `json:"max_turns"`
	// Duration statistics (stored as nanoseconds for JSON portability).
	// Only runs with duration > 0 contribute; runs without timing data are excluded.
	AvgDurationNs    int64   `json:"avg_duration_ns"`
	MedianDurationNs int64   `json:"median_duration_ns"`
	StdDevDurationNs int64   `json:"stddev_duration_ns"`
	MinDurationNs    int64   `json:"min_duration_ns"`
	MaxDurationNs    int64   `json:"max_duration_ns"`
	TokenSpikes      []int64 `json:"token_spikes,omitempty"` // Run IDs with tokens > 2x avg
}

// MCPServerCrossRunHealth describes the health of a single MCP server across runs.
type MCPServerCrossRunHealth struct {
	MCPServerStatsBase
	RunsConnected int     `json:"runs_connected"` // Runs where server was used (appeared in tool usage)
	TotalRuns     int     `json:"total_runs"`
	ErrorRate     float64 `json:"error_rate"` // 0.0–1.0
	Unreliable    bool    `json:"unreliable"` // True if error_rate > 0.10 or connected < 75% of runs
}

// MarshalJSON preserves the cross-run MCP health JSON schema while sharing the
// per-server stat fields with the other MCP server report types.
func (h MCPServerCrossRunHealth) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		ServerName    string  `json:"server_name"`
		RunsConnected int     `json:"runs_connected"`
		TotalRuns     int     `json:"total_runs"`
		TotalCalls    int     `json:"total_calls"`
		TotalErrors   int     `json:"total_errors"`
		ErrorRate     float64 `json:"error_rate"`
		Unreliable    bool    `json:"unreliable"`
	}{
		ServerName:    h.ServerName,
		RunsConnected: h.RunsConnected,
		TotalRuns:     h.TotalRuns,
		TotalCalls:    h.ToolCallCount,
		TotalErrors:   h.ErrorCount,
		ErrorRate:     h.ErrorRate,
		Unreliable:    h.Unreliable,
	})
}

// UnmarshalJSON is the counterpart to MarshalJSON, mapping the legacy
// "total_calls"/"total_errors" wire keys back into the embedded MCPServerStatsBase.
func (h *MCPServerCrossRunHealth) UnmarshalJSON(data []byte) error {
	var aux struct {
		ServerName    string  `json:"server_name"`
		RunsConnected int     `json:"runs_connected"`
		TotalRuns     int     `json:"total_runs"`
		TotalCalls    int     `json:"total_calls"`
		TotalErrors   int     `json:"total_errors"`
		ErrorRate     float64 `json:"error_rate"`
		Unreliable    bool    `json:"unreliable"`
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	h.MCPServerStatsBase = MCPServerStatsBase{
		ServerName:    aux.ServerName,
		ToolCallCount: aux.TotalCalls,
		ErrorCount:    aux.TotalErrors,
	}
	h.RunsConnected = aux.RunsConnected
	h.TotalRuns = aux.TotalRuns
	h.ErrorRate = aux.ErrorRate
	h.Unreliable = aux.Unreliable
	return nil
}

// ErrorTrendData summarizes error and warning patterns across runs.
type ErrorTrendData struct {
	RunsWithErrors   int     `json:"runs_with_errors"`
	TotalErrors      int     `json:"total_errors"`
	AvgErrorsPerRun  float64 `json:"avg_errors_per_run"`
	RunsWithWarnings int     `json:"runs_with_warnings"`
	TotalWarnings    int     `json:"total_warnings"`
}

// DomainInventoryEntry describes a single domain seen across multiple runs.
type DomainInventoryEntry struct {
	Domain        string            `json:"domain"`
	SeenInRuns    int               `json:"seen_in_runs"`
	TotalAllowed  int               `json:"total_allowed"`
	TotalBlocked  int               `json:"total_blocked"`
	OverallStatus string            `json:"overall_status"` // "allowed", "denied", "mixed"
	PerRunStatus  []DomainRunStatus `json:"per_run_status"`
}

// DomainRunStatus records the status of a domain in a single run.
type DomainRunStatus struct {
	RunID   int64  `json:"run_id"`
	Status  string `json:"status"` // "allowed", "denied", "mixed", "absent"
	Allowed int    `json:"allowed"`
	Blocked int    `json:"blocked"`
}

// PerRunFirewallBreakdown is a summary row for a single run within the cross-run report.
// It extends the firewall view with token, turn, and MCP error information.
type PerRunFirewallBreakdown struct {
	RunID         int64         `json:"run_id"`
	WorkflowName  string        `json:"workflow_name"`
	Conclusion    string        `json:"conclusion"`
	Duration      time.Duration `json:"duration_ns"` // Wall-clock duration of the run
	TotalRequests int           `json:"total_requests"`
	Allowed       int           `json:"allowed"`
	Blocked       int           `json:"blocked"`
	DenyRate      float64       `json:"deny_rate"` // 0.0–1.0
	UniqueDomains int           `json:"unique_domains"`
	Tokens        int           `json:"tokens"`
	Turns         int           `json:"turns"`
	MCPErrors     int           `json:"mcp_errors"`
	ErrorCount    int           `json:"error_count"`
	HasData       bool          `json:"has_data"`
	TokenSpike    bool          `json:"token_spike,omitempty"` // True if tokens > 2x avg
}

// crossRunInput bundles per-run data needed for aggregation.
type crossRunInput struct {
	RunID               int64
	WorkflowName        string
	Conclusion          string
	Duration            time.Duration
	FirewallAnalysis    *FirewallAnalysis
	Metrics             LogMetrics
	MCPToolUsage        *MCPToolUsageData
	MCPFailures         []MCPFailureReport
	ErrorCount          int
	TaskDomain          *TaskDomainInfo
	BehaviorFingerprint *BehaviorFingerprint
	GradersCluster      string
	EvalsCluster        string
}

// buildCrossRunAuditReport aggregates data from multiple runs into a CrossRunAuditReport.
func buildCrossRunAuditReport(inputs []crossRunInput) *CrossRunAuditReport {
	auditCrossRunLog.Printf("Building cross-run audit report: %d inputs", len(inputs))

	report := &CrossRunAuditReport{RunsAnalyzed: len(inputs)}
	domainMap := make(map[string]*crossRunDomainAgg)
	runIDs := collectCrossRunRunIDs(inputs)
	metricsRows, mcpServerMap := collectCrossRunInputs(report, inputs, domainMap)

	finalizeCrossRunSummary(report)
	applyCrossRunMetricsTrend(report, metricsRows)
	report.MCPHealth = buildCrossRunMCPHealth(mcpServerMap, len(inputs))
	finalizeCrossRunErrorTrend(report, len(inputs))
	buildCrossRunDomainInventory(report, domainMap, runIDs)

	auditCrossRunLog.Printf("Cross-run audit report built: runs=%d, with_data=%d, unique_domains=%d, mcp_servers=%d",
		report.RunsAnalyzed, report.RunsWithData, report.Summary.UniqueDomains, len(report.MCPHealth))

	report.Drain3Insights = buildDrain3InsightsFromCrossRunInputs(inputs)
	report.ClusterAnalysis = buildClusterAnalysis(inputs)
	return report
}

type crossRunDomainAgg struct {
	totalAllowed int
	totalBlocked int
	perRun       []DomainRunStatus
}

type crossRunMCPServerAgg struct {
	totalCalls  int
	totalErrors int
	runsSeen    map[int64]bool
}

func collectCrossRunRunIDs(inputs []crossRunInput) []int64 {
	runIDs := make([]int64, 0, len(inputs))
	for _, in := range inputs {
		runIDs = append(runIDs, in.RunID)
	}
	return runIDs
}

func collectCrossRunInputs(
	report *CrossRunAuditReport,
	inputs []crossRunInput,
	domainMap map[string]*crossRunDomainAgg,
) ([]metricsRawRow, map[string]*crossRunMCPServerAgg) {
	metricsRows := make([]metricsRawRow, 0, len(inputs))
	mcpServerMap := make(map[string]*crossRunMCPServerAgg)

	for _, in := range inputs {
		breakdown := newPerRunFirewallBreakdown(in)
		aggregateCrossRunMCPData(&breakdown, in, mcpServerMap)
		applyCrossRunFirewallData(report, &breakdown, in, domainMap)
		metricsRows = append(metricsRows, metricsRawRow{
			runID:    in.RunID,
			tokens:   in.Metrics.TokenUsage,
			turns:    in.Metrics.Turns,
			duration: in.Duration,
		})
		updateCrossRunErrorTrend(&report.ErrorTrend, in.ErrorCount)
		report.PerRunBreakdown = append(report.PerRunBreakdown, breakdown)
	}

	return metricsRows, mcpServerMap
}

func newPerRunFirewallBreakdown(in crossRunInput) PerRunFirewallBreakdown {
	return PerRunFirewallBreakdown{
		RunID:        in.RunID,
		WorkflowName: in.WorkflowName,
		Conclusion:   in.Conclusion,
		Duration:     in.Duration,
		Tokens:       in.Metrics.TokenUsage,
		Turns:        in.Metrics.Turns,
		ErrorCount:   in.ErrorCount,
	}
}

func aggregateCrossRunMCPData(
	breakdown *PerRunFirewallBreakdown,
	in crossRunInput,
	mcpServerMap map[string]*crossRunMCPServerAgg,
) {
	breakdown.MCPErrors = len(in.MCPFailures)
	if in.MCPToolUsage == nil {
		return
	}

	for _, srv := range in.MCPToolUsage.Servers {
		breakdown.MCPErrors += srv.ErrorCount
		agg, ok := mcpServerMap[srv.ServerName]
		if !ok {
			agg = &crossRunMCPServerAgg{runsSeen: make(map[int64]bool)}
			mcpServerMap[srv.ServerName] = agg
		}
		agg.totalCalls += srv.ToolCallCount
		agg.totalErrors += srv.ErrorCount
		agg.runsSeen[in.RunID] = true
	}
}

func applyCrossRunFirewallData(
	report *CrossRunAuditReport,
	breakdown *PerRunFirewallBreakdown,
	in crossRunInput,
	domainMap map[string]*crossRunDomainAgg,
) {
	if in.FirewallAnalysis == nil {
		report.RunsWithoutData++
		return
	}

	report.RunsWithData++
	breakdown.HasData = true
	breakdown.TotalRequests = in.FirewallAnalysis.TotalRequests
	breakdown.Allowed = in.FirewallAnalysis.AllowedRequests
	breakdown.Blocked = in.FirewallAnalysis.BlockedRequests
	if breakdown.TotalRequests > 0 {
		breakdown.DenyRate = float64(breakdown.Blocked) / float64(breakdown.TotalRequests)
	}
	breakdown.UniqueDomains = len(in.FirewallAnalysis.RequestsByDomain)

	report.Summary.TotalRequests += breakdown.TotalRequests
	report.Summary.TotalAllowed += breakdown.Allowed
	report.Summary.TotalBlocked += breakdown.Blocked

	for domain, stats := range in.FirewallAnalysis.RequestsByDomain {
		agg, exists := domainMap[domain]
		if !exists {
			agg = &crossRunDomainAgg{}
			domainMap[domain] = agg
		}
		agg.totalAllowed += stats.Allowed
		agg.totalBlocked += stats.Blocked
		agg.perRun = append(agg.perRun, DomainRunStatus{
			RunID:   in.RunID,
			Status:  classifyFirewallDomainStatus(stats),
			Allowed: stats.Allowed,
			Blocked: stats.Blocked,
		})
	}
}

func updateCrossRunErrorTrend(trend *ErrorTrendData, errorCount int) {
	if errorCount <= 0 {
		return
	}
	trend.RunsWithErrors++
	trend.TotalErrors += errorCount
}

func finalizeCrossRunSummary(report *CrossRunAuditReport) {
	if report.Summary.TotalRequests > 0 {
		report.Summary.OverallDenyRate = float64(report.Summary.TotalBlocked) / float64(report.Summary.TotalRequests)
	}
}

func applyCrossRunMetricsTrend(report *CrossRunAuditReport, metricsRows []metricsRawRow) {
	report.MetricsTrend = buildMetricsTrend(metricsRows)

	tokenSpikes := make(map[int64]bool, len(report.MetricsTrend.TokenSpikes))
	for _, rid := range report.MetricsTrend.TokenSpikes {
		tokenSpikes[rid] = true
	}

	for i := range report.PerRunBreakdown {
		run := &report.PerRunBreakdown[i]
		run.TokenSpike = tokenSpikes[run.RunID]
	}
}

func buildCrossRunMCPHealth(mcpServerMap map[string]*crossRunMCPServerAgg, totalRuns int) []MCPServerCrossRunHealth {
	if len(mcpServerMap) == 0 {
		return nil
	}

	auditCrossRunLog.Printf("Building MCP health for %d server(s) across %d run(s)", len(mcpServerMap), totalRuns)

	sortedServers := sliceutil.SortedKeys(mcpServerMap)

	health := make([]MCPServerCrossRunHealth, 0, len(sortedServers))
	for _, name := range sortedServers {
		agg := mcpServerMap[name]
		connected := len(agg.runsSeen)
		errorRate := 0.0
		if agg.totalCalls > 0 {
			errorRate = float64(agg.totalErrors) / float64(agg.totalCalls)
		}
		unreliable := errorRate > mcpErrorRateThreshold
		if totalRuns > 0 && float64(connected)/float64(totalRuns) < mcpConnectionRateThreshold {
			unreliable = true
		}
		health = append(health, MCPServerCrossRunHealth{
			MCPServerStatsBase: MCPServerStatsBase{
				ServerName:    name,
				ToolCallCount: agg.totalCalls,
				ErrorCount:    agg.totalErrors,
			},
			RunsConnected: connected,
			TotalRuns:     totalRuns,
			ErrorRate:     errorRate,
			Unreliable:    unreliable,
		})
	}

	return health
}

func finalizeCrossRunErrorTrend(report *CrossRunAuditReport, totalRuns int) {
	if totalRuns > 0 {
		report.ErrorTrend.AvgErrorsPerRun = float64(report.ErrorTrend.TotalErrors) / float64(totalRuns)
	}
}

func buildCrossRunDomainInventory(
	report *CrossRunAuditReport,
	domainMap map[string]*crossRunDomainAgg,
	runIDs []int64,
) {
	sortedDomains := sliceutil.SortedKeys(domainMap)

	auditCrossRunLog.Printf("Building domain inventory: %d unique domain(s) across %d run(s)", len(sortedDomains), len(runIDs))

	report.Summary.UniqueDomains = len(sortedDomains)
	for _, domain := range sortedDomains {
		report.DomainInventory = append(report.DomainInventory, buildCrossRunDomainEntry(domain, domainMap[domain], runIDs))
	}
}

func buildCrossRunDomainEntry(domain string, agg *crossRunDomainAgg, runIDs []int64) DomainInventoryEntry {
	presentRuns := make(map[int64]DomainRunStatus, len(agg.perRun))
	for _, status := range agg.perRun {
		presentRuns[status.RunID] = status
	}

	fullPerRun := make([]DomainRunStatus, 0, len(runIDs))
	for _, rid := range runIDs {
		status, ok := presentRuns[rid]
		if !ok {
			status = DomainRunStatus{RunID: rid, Status: "absent"}
		}
		fullPerRun = append(fullPerRun, status)
	}

	return DomainInventoryEntry{
		Domain:        domain,
		SeenInRuns:    len(agg.perRun),
		TotalAllowed:  agg.totalAllowed,
		TotalBlocked:  agg.totalBlocked,
		OverallStatus: classifyFirewallDomainStatus(DomainRequestStats{Allowed: agg.totalAllowed, Blocked: agg.totalBlocked}),
		PerRunStatus:  fullPerRun,
	}
}

// metricsRawRow holds per-run raw metric values for aggregation.
type metricsRawRow struct {
	runID    int64
	tokens   int
	turns    int
	duration time.Duration
}

// buildMetricsTrend computes aggregate metrics (min/max/avg/median/stddev/total, spike
// detection) from a slice of per-run raw metric rows. Mean and variance are computed
// using Welford's online algorithm via StatVar for numerical stability.
func buildMetricsTrend(rows []metricsRawRow) MetricsTrendData {
	auditCrossRunLog.Printf("Building metrics trend from %d rows", len(rows))
	if len(rows) == 0 {
		return MetricsTrendData{}
	}

	var tokenStats, turnStats, durationStats stats.StatVar
	trend := MetricsTrendData{}
	for _, row := range rows {
		accumulateMetricsTrendRow(&trend, &tokenStats, &turnStats, &durationStats, row)
	}

	applyMetricsTrendStats(&trend, tokenStats, turnStats, durationStats)
	applyMetricsTrendSpikes(&trend, rows)
	auditCrossRunLog.Printf("Metrics trend computed: avg_tokens=%d, avg_turns=%.1f, token_spikes=%d",
		trend.AvgTokens, trend.AvgTurns, len(trend.TokenSpikes))
	return trend
}

func accumulateMetricsTrendRow(
	trend *MetricsTrendData,
	tokenStats *stats.StatVar,
	turnStats *stats.StatVar,
	durationStats *stats.StatVar,
	row metricsRawRow,
) {
	trend.TotalTokens += row.tokens
	trend.TotalTurns += row.turns
	if row.turns > trend.MaxTurns {
		trend.MaxTurns = row.turns
	}

	tokenStats.Add(float64(row.tokens))
	turnStats.Add(float64(row.turns))
	if row.duration > 0 {
		durationStats.Add(float64(row.duration))
	}
}

func applyMetricsTrendStats(
	trend *MetricsTrendData,
	tokenStats stats.StatVar,
	turnStats stats.StatVar,
	durationStats stats.StatVar,
) {
	if tokenStats.Count() > 0 {
		trend.AvgTokens = int(tokenStats.Mean())
		trend.MedianTokens = tokenStats.Median()
		trend.StdDevTokens = tokenStats.SampleStdDev()
		trend.MinTokens = int(tokenStats.Min())
		trend.MaxTokens = int(tokenStats.Max())
	}
	if turnStats.Count() > 0 {
		trend.AvgTurns = turnStats.Mean()
		trend.MedianTurns = turnStats.Median()
		trend.StdDevTurns = turnStats.SampleStdDev()
	}
	if durationStats.Count() > 0 {
		trend.AvgDurationNs = int64(durationStats.Mean())
		trend.MedianDurationNs = int64(durationStats.Median())
		trend.StdDevDurationNs = int64(durationStats.SampleStdDev())
		trend.MinDurationNs = int64(durationStats.Min())
		trend.MaxDurationNs = int64(durationStats.Max())
	}
}

func applyMetricsTrendSpikes(trend *MetricsTrendData, rows []metricsRawRow) {
	if trend.AvgTokens > 0 {
		for _, row := range rows {
			if row.tokens > int(spikeDetectionMultiplier*float64(trend.AvgTokens)) {
				trend.TokenSpikes = append(trend.TokenSpikes, row.runID)
			}
		}
	}
}

// buildDrain3InsightsFromCrossRunInputs converts cross-run inputs to ProcessedRuns and
// delegates to the shared multi-run drain3 analysis function.
// Returns nil if inputs is empty or if no events could be extracted.
func buildDrain3InsightsFromCrossRunInputs(inputs []crossRunInput) []ObservabilityInsight {
	if len(inputs) == 0 {
		return nil
	}
	auditCrossRunLog.Printf("Building drain3 insights from %d cross-run input(s)", len(inputs))
	runs := make([]ProcessedRun, 0, len(inputs))
	for _, in := range inputs {
		pr := ProcessedRun{
			Run: WorkflowRun{
				DatabaseID:   in.RunID,
				WorkflowName: in.WorkflowName,
				Conclusion:   in.Conclusion,
				Duration:     in.Duration,
				Turns:        in.Metrics.Turns,
				TokenUsage:   in.Metrics.TokenUsage,
				ErrorCount:   in.ErrorCount,
			},
			MCPFailures: in.MCPFailures,
		}
		runs = append(runs, pr)
	}
	return buildDrain3InsightsMultiRun(runs)
}
