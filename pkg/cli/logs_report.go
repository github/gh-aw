package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/constants"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/sliceutil"
	"github.com/github/gh-aw/pkg/timeutil"
	"github.com/github/gh-aw/pkg/workflow"
)

var reportLog = logger.New("cli:logs_report")

// LogsData represents the complete structured data for logs output
type LogsData struct {
	Summary             LogsSummary                 `json:"summary" console:"title:Workflow Logs Summary"`
	Runs                []RunData                   `json:"runs" console:"title:Workflow Logs Overview"`
	Episodes            []EpisodeData               `json:"episodes" console:"-"`
	Edges               []EpisodeEdge               `json:"edges" console:"-"`
	ToolUsage           []ToolUsageSummary          `json:"tool_usage,omitempty" console:"title:🛠️  Tool Usage Summary,omitempty"`
	MCPToolUsage        *MCPToolUsageSummary        `json:"mcp_tool_usage,omitempty" console:"title:🔧 MCP Tool Usage,omitempty"`
	Observability       []ObservabilityInsight      `json:"observability_insights,omitempty" console:"-"`
	ErrorsAndWarnings   []ErrorSummary              `json:"errors_and_warnings,omitempty" console:"title:Errors and Warnings,omitempty"`
	MissingTools        []MissingToolSummary        `json:"missing_tools,omitempty" console:"title:🛠️  Missing Tools Summary,omitempty"`
	MissingData         []MissingDataSummary        `json:"missing_data,omitempty" console:"title:📊 Missing Data Summary,omitempty"`
	MCPFailures         []MCPFailureSummary         `json:"mcp_failures,omitempty" console:"-"`
	AccessLog           *AccessLogSummary           `json:"access_log,omitempty" console:"title:Access Log Analysis,omitempty"`
	FirewallLog         *FirewallLogSummary         `json:"firewall_log,omitempty" console:"title:🔥 Firewall Log Analysis,omitempty"`
	RedactedDomains     *RedactedDomainsLogSummary  `json:"redacted_domains,omitempty" console:"title:🔒 Redacted URL Domains,omitempty"`
	Continuation        *ContinuationData           `json:"continuation,omitempty" console:"-"`
	Continuations       []WorkflowContinuation      `json:"continuations,omitempty" console:"-"`
	LogsLocation        string                      `json:"logs_location" console:"-"`
	Message             string                      `json:"message,omitempty" console:"-"`
	StaleWarning        string                      `json:"stale_warning,omitempty" console:"-"`
	GitHubAPIRateLimit  *GitHubAPIRateLimitReport   `json:"github_api_rate_limit,omitempty" console:"-"`
	GitHubAPIRateLimits []*GitHubAPIRateLimitReport `json:"github_api_rate_limits,omitempty" console:"-"`
}

// ContinuationData provides parameters to continue an incomplete logs query.
type ContinuationData struct {
	Message               string `json:"message"`
	WorkflowName          string `json:"workflow_name,omitempty"`
	Count                 int    `json:"count,omitempty"`
	StartDate             string `json:"start_date,omitempty"`
	EndDate               string `json:"end_date,omitempty"`
	Engine                string `json:"engine,omitempty"`
	Branch                string `json:"branch,omitempty"`
	AfterRunID            int64  `json:"after_run_id,omitempty"`
	BeforeRunID           int64  `json:"before_run_id,omitempty"`
	Timeout               int    `json:"timeout,omitempty"`
	MaxGitHubAPIRateLimit int    `json:"max_github_api_rate_limit,omitempty"`
	MaxStorageMB          int    `json:"max_storage,omitempty"`
	PruneOlderRuns        bool   `json:"prune_older_runs,omitempty"`
}

// WorkflowContinuation identifies a per-target cursor in a combined
// multi-workflow report.
type WorkflowContinuation struct {
	Repository string `json:"repository,omitempty"`
	ContinuationData
}

// LogsSummary contains aggregate metrics across all runs
type LogsSummary struct {
	TotalRuns           int     `json:"total_runs" console:"header:Total Runs"`
	TotalDuration       string  `json:"total_duration" console:"header:Total Duration"`
	TotalAIC            float64 `json:"total_aic,omitempty"`
	TotalTokens         int     `json:"total_tokens,omitempty" console:"header:Total Tokens,format:number,omitempty"`
	TotalActionMinutes  float64 `json:"total_action_minutes" console:"header:Total Action Minutes"`
	TotalTurns          int     `json:"total_turns" console:"header:Total Turns"`
	TotalSteeringEvents int     `json:"total_steering_events,omitempty" console:"header:Total Steering Events,format:number,omitempty"`
	TotalErrors         int     `json:"total_errors" console:"header:Total Errors"`
	TotalWarnings       int     `json:"total_warnings" console:"header:Total Warnings"`
	TotalMissingTools   int     `json:"total_missing_tools" console:"header:Total Missing Tools"`
	TotalMissingData    int     `json:"total_missing_data" console:"header:Total Missing Data"`
	TotalSafeItems      int     `json:"total_safe_items" console:"header:Total Safe Items"`
	// TotalDriverExitFailures counts failed runs with zero agent turns — the CLI wrapper
	// or a pre/post-agent infrastructure step exited non-zero before the agent ran.
	// These are infra-flakiness signals, not agent-logic regressions.
	TotalDriverExitFailures int `json:"total_driver_exit_failures" console:"header:Driver-Exit Failures"`
	// TotalAgentLogicFailures counts failed runs caused after agent execution started:
	// either one or more agent turns were observed, or job metadata confirms
	// agent=success followed by a failed safe_outputs job.
	TotalAgentLogicFailures       int `json:"total_agent_logic_failures" console:"header:Agent-Logic Failures"`
	RunsWithTemporaryIDChains     int `json:"runs_with_temporary_id_chains,omitempty" console:"-"`
	RunsWithDelegatedTempTargets  int `json:"runs_with_delegated_temp_targets,omitempty" console:"-"`
	RunsWithMissingTemporaryIDMap int `json:"runs_with_missing_temporary_id_map,omitempty" console:"-"`
	RunsWithInvalidTemporaryIDMap int `json:"runs_with_invalid_temporary_id_map,omitempty" console:"-"`
	TotalTemporaryIDMappings      int `json:"total_temporary_id_mappings,omitempty" console:"-"`
	TotalChainedTargets           int `json:"total_chained_targets,omitempty" console:"-"`
	TotalChainedFollowupActions   int `json:"total_chained_followup_actions,omitempty" console:"-"`
	TotalClosedTempTargets        int `json:"total_closed_temp_targets,omitempty" console:"-"`
	TotalEpisodes                 int `json:"total_episodes" console:"header:Total Episodes"`
	HighConfidenceEpisodes        int `json:"high_confidence_episodes" console:"header:High Confidence Episodes"`
	TotalGitHubAPICalls           int `json:"total_github_api_calls,omitempty" console:"header:Total GitHub API Calls,format:number,omitempty"`
	// EngineCounts maps engine_id (from aw_info.json) to the number of runs using that engine.
	// Use this field to accurately classify engine types — do NOT infer engines by scanning
	// lock files, which contain the word "copilot" in allowed-domains and workflow-source paths
	// regardless of which engine the workflow actually uses.
	EngineCounts map[string]int `json:"engine_counts,omitempty" console:"-"`

	// IntentionalFailureRuns is the count of runs belonging to workflows tagged with
	// intentional-failure: true. These runs are intentionally expected to fail (e.g.
	// credit-guardrail stress tests) and should be excluded from prod-main / fleet-health
	// success-rate rollups. Agents should subtract this count from TotalRuns before computing
	// fleet-level success rates so that deliberate failures do not depress the baseline.
	IntentionalFailureRuns int `json:"intentional_failure_runs,omitempty" console:"-"`

	// Outcome metrics (populated when outcome evaluation is enabled)
	OutcomeAccepted       int     `json:"outcome_accepted,omitempty" console:"-"`
	OutcomeRejected       int     `json:"outcome_rejected,omitempty" console:"-"`
	OutcomeIgnored        int     `json:"outcome_ignored,omitempty" console:"-"`
	OutcomePending        int     `json:"outcome_pending,omitempty" console:"-"`
	OutcomeAcceptanceRate float64 `json:"outcome_acceptance_rate,omitempty" console:"-"`
	OutcomeWasteRate      float64 `json:"outcome_waste_rate,omitempty" console:"-"`
	OutcomeZeroTouchRate  float64 `json:"outcome_zero_touch_rate,omitempty" console:"-"`
}

// RunData contains information about a single workflow run
type RunData struct {
	RunID        int64  `json:"run_id" console:"header:Run ID"`
	Number       int    `json:"number" console:"-"`
	WorkflowName string `json:"workflow_name" console:"header:Workflow"`
	WorkflowPath string `json:"workflow_path" console:"-"`
	// IntentionalFailure is true when the workflow is tagged with intentional-failure: true
	// in its frontmatter (e.g. credit-guardrail stress tests that are expected to fail).
	// Agents and dashboards MUST exclude these runs from prod-main and fleet-health
	// success-rate rollups to avoid depressing the real-regression baseline.
	IntentionalFailure bool   `json:"intentional_failure,omitempty" console:"-"`
	Agent              string `json:"agent,omitempty" console:"header:Agent,omitempty"`
	Engine             string `json:"engine,omitempty" console:"-"`
	EngineID           string `json:"engine_id,omitempty" console:"-"`
	Status             string `json:"status" console:"header:Status"`
	Conclusion         string `json:"conclusion,omitempty" console:"-"`
	Classification     string `json:"classification" console:"-"`
	// FailureKind classifies the cause of a failed run.
	// "driver_exit"   – zero agent turns; the CLI wrapper or an infra step exited before the agent ran.
	// "agent_logic"   – the run failed after agent execution started: one or more agent
	//                   turns were observed, or job metadata shows agent=success followed
	//                   by a failed safe_outputs job.
	// ""              – the run did not fail (success), or turn data was unavailable for classification.
	FailureKind                string                 `json:"failure_kind,omitempty" console:"-"`
	Duration                   string                 `json:"duration,omitempty" console:"header:Duration,omitempty"`
	ActionMinutes              float64                `json:"action_minutes,omitempty" console:"header:Action Minutes,omitempty"`
	TokenUsage                 int                    `json:"token_usage,omitempty" console:"header:Tokens,format:number,omitempty"`
	AIC                        float64                `json:"aic,omitempty"`
	AmbientContext             *AmbientContextMetrics `json:"ambient_context,omitempty" console:"-"`
	WorkingSet                 *WorkingSetMetrics     `json:"working_set,omitempty" console:"-"`
	WSRF                       string                 `json:"-" console:"header:WSRF,omitempty"` // Working-Set Rebuild Factor, pre-formatted for table display
	Turns                      int                    `json:"turns,omitempty" console:"header:Turns,omitempty"`
	ErrorCount                 int                    `json:"error_count,omitempty" console:"header:Errors"`
	WarningCount               int                    `json:"warning_count,omitempty" console:"header:Warnings"`
	MissingToolCount           int                    `json:"missing_tool_count,omitempty" console:"header:Missing Tools"`
	MissingDataCount           int                    `json:"missing_data_count,omitempty" console:"header:Missing Data"`
	SafeItemsCount             int                    `json:"safe_items_count,omitempty" console:"header:Safe Items,omitempty"`
	ManifestEntryCount         int                    `json:"manifest_entry_count,omitempty" console:"-"`
	TemporaryIDMapStatus       string                 `json:"temporary_id_map_status,omitempty" console:"-"`
	TemporaryIDMappings        int                    `json:"temporary_id_mappings,omitempty" console:"-"`
	ChainedTargetCount         int                    `json:"chained_target_count,omitempty" console:"-"`
	ChainedFollowupActionCount int                    `json:"chained_followup_action_count,omitempty" console:"-"`
	DelegatedTempTargetCount   int                    `json:"delegated_temp_target_count,omitempty" console:"-"`
	ClosedTempTargetCount      int                    `json:"closed_temp_target_count,omitempty" console:"-"`
	CreatedAt                  time.Time              `json:"created_at" console:"header:Created"`
	StartedAt                  time.Time              `json:"started_at,omitzero" console:"-"`
	UpdatedAt                  time.Time              `json:"updated_at,omitzero" console:"-"`
	URL                        string                 `json:"url" console:"-"`
	LogsPath                   string                 `json:"logs_path" console:"header:Logs Path"`
	AuditPath                  string                 `json:"audit_path,omitempty" console:"-"`
	Event                      string                 `json:"event" console:"-"`
	Branch                     string                 `json:"branch" console:"-"`
	HeadSHA                    string                 `json:"head_sha,omitempty" console:"-"`
	DisplayTitle               string                 `json:"display_title,omitempty" console:"-"`
	Repository                 string                 `json:"repository,omitempty" console:"-"`
	Organization               string                 `json:"organization,omitempty" console:"-"`
	Ref                        string                 `json:"ref,omitempty" console:"-"`
	SHA                        string                 `json:"sha,omitempty" console:"-"`
	Actor                      string                 `json:"actor,omitempty" console:"-"`
	RunAttempt                 string                 `json:"run_attempt,omitempty" console:"-"`
	TargetRepo                 string                 `json:"target_repo,omitempty" console:"-"`
	EventName                  string                 `json:"event_name,omitempty" console:"-"`
	Comparison                 *AuditComparisonData   `json:"comparison,omitempty" console:"-"`
	TaskDomain                 *TaskDomainInfo        `json:"task_domain,omitempty" console:"-"`
	BehaviorFingerprint        *BehaviorFingerprint   `json:"behavior_fingerprint,omitempty" console:"-"`
	AgenticAssessments         []AgenticAssessment    `json:"agentic_assessments,omitempty" console:"-"`
	AwContext                  *AwContext             `json:"context,omitempty" console:"-"`                                                        // aw_context data from aw_info.json
	TokenUsageSummary          *TokenUsageSummary     `json:"token_usage_summary,omitempty" console:"-"`                                            // Token usage from firewall proxy
	GitHubAPICalls             int                    `json:"github_api_calls,omitempty" console:"header:GitHub API Calls,format:number,omitempty"` // GitHub API calls made during the run
	AvgTimeBetweenTurns        string                 `json:"avg_time_between_turns,omitempty" console:"-"`                                         // Average time between consecutive LLM API calls (TBT)
	Experiments                *ExperimentData        `json:"experiments,omitempty" console:"-"`                                                    // A/B experiment assignments for this run
	Graders                    *GradersData           `json:"graders,omitempty" console:"-"`                                                        // Deterministic grader results for this run
}

// logsAggregate accumulates cross-run totals while runs are converted to RunData.
type logsAggregate struct {
	totalDuration                 time.Duration
	totalAIC                      float64
	totalTokens                   int
	totalActionMinutes            float64
	totalTurns                    int
	totalSteeringEvents           int
	totalErrors                   int
	totalWarnings                 int
	totalMissingTools             int
	totalMissingData              int
	totalSafeItems                int
	totalDriverExitFailures       int
	totalAgentLogicFailures       int
	runsWithTemporaryIDChains     int
	runsWithDelegatedTempTargets  int
	runsWithMissingTemporaryIDMap int
	runsWithInvalidTemporaryIDMap int
	totalTemporaryIDMappings      int
	totalChainedTargets           int
	totalChainedFollowupActions   int
	totalClosedTempTargets        int
	totalGitHubAPICalls           int
	intentionalFailureRuns        int
	// engineCounts tracks the number of runs per engine_id, sourced from aw_info.json.
	// This is the authoritative engine classification — do not infer engine type from
	// lock file contents, which contain "copilot" in allowed-domains and source paths
	// regardless of which engine the workflow uses.
	engineCounts map[string]int
}

// accumulateRunTotals adds the per-run metrics of a processed run to the aggregate.
func (agg *logsAggregate) accumulateRunTotals(pr ProcessedRun) {
	run := pr.Run
	if run.Duration > 0 {
		agg.totalDuration += run.Duration
	}
	if pr.TokenUsage != nil {
		agg.totalAIC += pr.TokenUsage.TotalAIC
		agg.totalSteeringEvents += pr.TokenUsage.TotalSteeringEvents
	}
	agg.totalTokens += run.TokenUsage
	agg.totalActionMinutes += run.ActionMinutes
	agg.totalTurns += run.Turns
	agg.totalErrors += run.ErrorCount
	agg.totalWarnings += run.WarningCount
	agg.totalMissingTools += run.MissingToolCount
	agg.totalMissingData += run.MissingDataCount
	agg.totalSafeItems += run.SafeItemsCount
}

// accumulateChainMetrics adds safe-output chain metrics of a run to the aggregate.
func (agg *logsAggregate) accumulateChainMetrics(chainMetrics SafeOutputChainMetrics) {
	agg.totalTemporaryIDMappings += chainMetrics.TemporaryIDMappings
	agg.totalChainedTargets += chainMetrics.ChainedTargetCount
	agg.totalChainedFollowupActions += chainMetrics.ChainedFollowupActionCount
	agg.totalClosedTempTargets += chainMetrics.ClosedTempTargetCount
	if chainMetrics.ChainedTargetCount > 0 {
		agg.runsWithTemporaryIDChains++
	}
	if chainMetrics.DelegatedTempTargetCount > 0 {
		agg.runsWithDelegatedTempTargets++
	}
	switch chainMetrics.TemporaryIDMapStatus {
	case temporaryIDMapStatusMissing:
		agg.runsWithMissingTemporaryIDMap++
	case temporaryIDMapStatusInvalid:
		agg.runsWithInvalidTemporaryIDMap++
	}
}

// classifyRunFailure determines the failure kind for a run and updates failure rollups.
// Returns "" when the run did not fail or turn data was unavailable for classification.
//
// isDriverExitFailure requires TurnsAvailable so that runs without artifact data
// (ErrNoArtifacts) are not wrongly labelled driver_exit.
// Agent-logic requires a failed run and either:
//  1. reliable turn data (TurnsAvailable) / confirmed non-zero turns, or
//  2. job metadata showing agent=success followed by a failed safe_outputs job.
func (agg *logsAggregate) classifyRunFailure(pr ProcessedRun) string {
	run := pr.Run
	if isFailureConclusion(run.Conclusion) && isSafeOutputsFailureAfterSuccessfulAgent(pr.JobDetails) {
		agg.totalAgentLogicFailures++
		return "agent_logic"
	}
	if isDriverExitFailure(run) {
		agg.totalDriverExitFailures++
		return "driver_exit"
	}
	if isFailureConclusion(run.Conclusion) && (run.TurnsAvailable || run.Turns > 0) {
		agg.totalAgentLogicFailures++
		return "agent_logic"
	}
	return ""
}

// summary converts the accumulated totals into a LogsSummary.
func (agg *logsAggregate) summary(totalRuns int) LogsSummary {
	summary := LogsSummary{
		TotalRuns:                     totalRuns,
		TotalDuration:                 timeutil.FormatDuration(agg.totalDuration),
		TotalAIC:                      agg.totalAIC,
		TotalTokens:                   agg.totalTokens,
		TotalActionMinutes:            agg.totalActionMinutes,
		TotalTurns:                    agg.totalTurns,
		TotalSteeringEvents:           agg.totalSteeringEvents,
		TotalErrors:                   agg.totalErrors,
		TotalWarnings:                 agg.totalWarnings,
		TotalMissingTools:             agg.totalMissingTools,
		TotalMissingData:              agg.totalMissingData,
		TotalSafeItems:                agg.totalSafeItems,
		TotalDriverExitFailures:       agg.totalDriverExitFailures,
		TotalAgentLogicFailures:       agg.totalAgentLogicFailures,
		RunsWithTemporaryIDChains:     agg.runsWithTemporaryIDChains,
		RunsWithDelegatedTempTargets:  agg.runsWithDelegatedTempTargets,
		RunsWithMissingTemporaryIDMap: agg.runsWithMissingTemporaryIDMap,
		RunsWithInvalidTemporaryIDMap: agg.runsWithInvalidTemporaryIDMap,
		TotalTemporaryIDMappings:      agg.totalTemporaryIDMappings,
		TotalChainedTargets:           agg.totalChainedTargets,
		TotalChainedFollowupActions:   agg.totalChainedFollowupActions,
		TotalClosedTempTargets:        agg.totalClosedTempTargets,
		TotalGitHubAPICalls:           agg.totalGitHubAPICalls,
	}
	if len(agg.engineCounts) > 0 {
		summary.EngineCounts = agg.engineCounts
	}
	if agg.intentionalFailureRuns > 0 {
		summary.IntentionalFailureRuns = agg.intentionalFailureRuns
	}
	return summary
}

// runEngineInfo holds the engine and context data extracted from aw_info.json for a run.
type runEngineInfo struct {
	engineID   string
	engineName string
	awContext  *AwContext
	awInfo     *AwInfo
}

// extractRunEngineInfo reads aw_info.json for a run and resolves the engine identity and
// aw_context, falling back to the processed run's context when unavailable.
func extractRunEngineInfo(pr ProcessedRun) runEngineInfo {
	var info runEngineInfo
	awInfoPath := filepath.Join(pr.Run.LogsPath, "aw_info.json")
	if parsed, err := parseAwInfo(awInfoPath, false); err == nil && parsed != nil {
		info.awInfo = parsed
		info.engineID = parsed.EngineID
		info.engineName = parsed.EngineName
		info.awContext = parsed.Context
	}
	if info.engineName == "" {
		info.engineName = info.engineID
	}
	if info.awContext == nil {
		info.awContext = pr.AwContext
	}
	return info
}

// applyAwInfoToRunData copies repository/ref metadata from aw_info.json onto the run data.
func applyAwInfoToRunData(runData *RunData, awInfo *AwInfo) {
	if awInfo.Repository != "" {
		runData.Repository = awInfo.Repository
		if parts := strings.SplitN(awInfo.Repository, "/", 2); len(parts) == 2 {
			runData.Organization = parts[0]
		}
	}
	if awInfo.Ref != "" {
		runData.Ref = awInfo.Ref
	}
	if awInfo.SHA != "" {
		runData.SHA = awInfo.SHA
	}
	if awInfo.Actor != "" {
		runData.Actor = awInfo.Actor
	}
	if awInfo.RunAttempt != "" {
		runData.RunAttempt = awInfo.RunAttempt
	}
	if awInfo.TargetRepo != "" {
		runData.TargetRepo = awInfo.TargetRepo
	}
	if awInfo.EventName != "" {
		runData.EventName = awInfo.EventName
	}
	// Fall back to inferring the workflow path from the display name when the
	// GitHub API returned an empty path (e.g. for scheduled agentic runs).
	// This handles both fresh runs and old cached RunSummary entries whose
	// run.WorkflowPath was persisted as empty before the fix in
	// logs_run_processor.go was applied.
	if runData.WorkflowPath == "" && awInfo.WorkflowName != "" {
		runData.WorkflowPath = inferWorkflowPathFromDisplayName(awInfo.WorkflowName)
	}
}

func applyGitHubMetadataToRunData(runData *RunData, run WorkflowRun) {
	if run.Repository != "" {
		runData.Repository = run.Repository
	}
	if run.HeadSha != "" {
		runData.SHA = run.HeadSha
	}
	if run.Actor != "" {
		runData.Actor = run.Actor
	}
	if run.Event != "" {
		runData.EventName = run.Event
	}
	if run.Attempt > 0 {
		runData.RunAttempt = strconv.Itoa(run.Attempt)
	}
	if parts := strings.SplitN(run.Repository, "/", 2); len(parts) == 2 {
		runData.Organization = parts[0]
	}
}

// buildRunData converts a processed run into RunData while accumulating rollup totals.
// localRepo guards against cross-repo misclassification of intentional-failure workflows.
func buildRunData(pr ProcessedRun, processedRuns []ProcessedRun, localRepo string, agg *logsAggregate) RunData {
	run := pr.Run

	agg.accumulateRunTotals(pr)
	failureKind := agg.classifyRunFailure(pr)

	// Accumulate GitHub API call counts
	var gitHubAPICalls int
	if pr.GitHubRateLimitUsage != nil {
		gitHubAPICalls = pr.GitHubRateLimitUsage.TotalRequestsMade
	}
	agg.totalGitHubAPICalls += gitHubAPICalls

	chainMetrics := buildSafeOutputChainMetrics(run.LogsPath)
	agg.accumulateChainMetrics(chainMetrics)

	// Extract engine ID and aw_context from aw_info.json.
	engineInfo := extractRunEngineInfo(pr)
	// Accumulate engine counts from aw_info.json data (authoritative source).
	if engineInfo.engineID != "" {
		agg.engineCounts[engineInfo.engineID]++
	}

	comparison := buildAuditComparisonForProcessedRuns(pr, processedRuns)

	runData := newRunData(pr, engineInfo, chainMetrics, comparison, failureKind, gitHubAPICalls)
	if engineInfo.awInfo != nil {
		applyAwInfoToRunData(&runData, engineInfo.awInfo)
	}
	// Mark runs from workflows tagged intentional-failure: true so that
	// agents and dashboards can exclude them from fleet-health success-rate rollups.
	// Only classify when the run comes from the same repository as the local checkout
	// (or when either side is unknown), to avoid cross-repo misclassification.
	if localRepo == "" || runData.Repository == "" || strings.EqualFold(localRepo, runData.Repository) {
		runData.IntentionalFailure = workflow.IsIntentionalFailure(runData.WorkflowPath)
	}
	if runData.IntentionalFailure {
		agg.intentionalFailureRuns++
	}
	if run.Duration > 0 {
		runData.Duration = timeutil.FormatDuration(run.Duration)
	}
	if pr.TokenUsage != nil && pr.TokenUsage.TotalAIC > 0 {
		runData.AIC = pr.TokenUsage.TotalAIC
	}
	// Compute average TBT from metrics when available; fall back to wall-time / (turns - 1).
	if run.AvgTimeBetweenTurns > 0 {
		runData.AvgTimeBetweenTurns = timeutil.FormatDuration(run.AvgTimeBetweenTurns)
	} else if run.Turns > 1 && run.Duration > 0 {
		runData.AvgTimeBetweenTurns = timeutil.FormatDuration(run.Duration/time.Duration(run.Turns-1)) + " (estimated)"
	}
	return runData
}

// newRunData assembles the base RunData fields for a processed run.
func newRunData(pr ProcessedRun, engineInfo runEngineInfo, chainMetrics SafeOutputChainMetrics, comparison *AuditComparisonData, failureKind string, gitHubAPICalls int) RunData {
	run := pr.Run

	var ambientContext *AmbientContextMetrics
	if pr.TokenUsage != nil {
		ambientContext = pr.TokenUsage.AmbientContext
	}

	runData := RunData{
		RunID:                      run.DatabaseID,
		Number:                     run.Number,
		WorkflowName:               run.WorkflowName,
		WorkflowPath:               run.WorkflowPath,
		Agent:                      engineInfo.engineID,
		Engine:                     engineInfo.engineName,
		EngineID:                   engineInfo.engineID,
		Status:                     run.Status,
		Conclusion:                 run.Conclusion,
		Classification:             deriveRunClassification(comparison),
		FailureKind:                failureKind,
		TokenUsage:                 run.TokenUsage,
		AIC:                        0,
		AmbientContext:             ambientContext,
		WorkingSet:                 pr.WorkingSet,
		WSRF:                       wsrfDisplayValue(pr.WorkingSet),
		ActionMinutes:              run.ActionMinutes,
		Turns:                      run.Turns,
		ErrorCount:                 run.ErrorCount,
		WarningCount:               run.WarningCount,
		MissingToolCount:           run.MissingToolCount,
		MissingDataCount:           run.MissingDataCount,
		SafeItemsCount:             run.SafeItemsCount,
		ManifestEntryCount:         chainMetrics.ManifestEntryCount,
		TemporaryIDMapStatus:       chainMetrics.TemporaryIDMapStatus,
		TemporaryIDMappings:        chainMetrics.TemporaryIDMappings,
		ChainedTargetCount:         chainMetrics.ChainedTargetCount,
		ChainedFollowupActionCount: chainMetrics.ChainedFollowupActionCount,
		DelegatedTempTargetCount:   chainMetrics.DelegatedTempTargetCount,
		ClosedTempTargetCount:      chainMetrics.ClosedTempTargetCount,
		CreatedAt:                  run.CreatedAt,
		StartedAt:                  run.StartedAt,
		UpdatedAt:                  run.UpdatedAt,
		URL:                        run.URL,
		LogsPath:                   run.LogsPath,
		AuditPath:                  existingAuditPath(run.LogsPath),
		Event:                      run.Event,
		Branch:                     run.HeadBranch,
		HeadSHA:                    run.HeadSha,
		DisplayTitle:               run.DisplayTitle,
		Comparison:                 comparison,
		TaskDomain:                 pr.TaskDomain,
		BehaviorFingerprint:        pr.BehaviorFingerprint,
		AgenticAssessments:         pr.AgenticAssessments,
		AwContext:                  engineInfo.awContext,
		TokenUsageSummary:          pr.TokenUsage,
		GitHubAPICalls:             gitHubAPICalls,
		Experiments:                extractExperimentData(run.LogsPath),
		Graders:                    extractGradersData(run.LogsPath),
	}
	applyGitHubMetadataToRunData(&runData, run)
	return runData
}

// buildLogsData creates structured logs data from processed runs
func buildLogsData(processedRuns []ProcessedRun, outputDir string, continuation *ContinuationData) LogsData {
	reportLog.Printf("Building logs data from %d processed runs", len(processedRuns))

	agg := &logsAggregate{engineCounts: make(map[string]int)}

	// Get the local repository slug once to guard against cross-repo misclassification.
	// IsIntentionalFailure reads from the local filesystem; when a run belongs to a
	// different repository the local file may not exist (fail-open) or, in edge cases,
	// may match an unrelated local file.  We skip detection when the run's repository
	// is known and does not match the local checkout.  Fails open (empty string) when
	// the slug cannot be determined.
	localRepo, _ := GetCurrentRepoSlug()

	// Build runs data
	// Initialize as empty slice to ensure JSON marshals to [] instead of null
	runs := make([]RunData, 0, len(processedRuns))
	for _, pr := range processedRuns {
		runs = append(runs, buildRunData(pr, processedRuns, localRepo, agg))
	}

	summary := agg.summary(len(processedRuns))

	episodes, edges := buildEpisodeData(runs, processedRuns)
	for _, episode := range episodes {
		summary.TotalEpisodes++
		if episode.Confidence == "high" {
			summary.HighConfidenceEpisodes++
		}
	}

	data := buildLogsSections(processedRuns)
	data.Summary = summary
	data.Runs = runs
	data.Episodes = episodes
	data.Edges = edges
	data.Continuation = continuation
	data.LogsLocation, _ = filepath.Abs(outputDir)
	return data
}

// buildLogsSections builds the cross-run analysis sections of the logs report.
func buildLogsSections(processedRuns []ProcessedRun) LogsData {
	// Build tool usage summary
	toolUsage := buildToolUsageSummary(processedRuns)

	observability := buildLogsObservabilityInsights(processedRuns, toolUsage)
	observability = append(observability, buildDrain3InsightsMultiRun(processedRuns)...)

	return LogsData{
		ToolUsage: toolUsage,
		// Build MCP tool usage summary
		MCPToolUsage:  buildMCPToolUsageSummary(processedRuns),
		Observability: observability,
		// Build combined error and warning summary
		ErrorsAndWarnings: buildCombinedErrorsSummary(processedRuns),
		// Build missing tools summary
		MissingTools: buildMissingToolsSummary(processedRuns),
		// Build missing data summary
		MissingData: buildMissingDataSummary(processedRuns),
		// Build MCP failures summary
		MCPFailures: buildMCPFailuresSummary(processedRuns),
		// Build access log summary
		AccessLog: buildAccessLogSummary(processedRuns),
		// Build firewall log summary
		FirewallLog: buildFirewallLogSummary(processedRuns),
		// Build redacted domains summary
		RedactedDomains: buildRedactedDomainsSummary(processedRuns),
	}
}

func isSafeOutputsFailureAfterSuccessfulAgent(jobDetails []JobInfoWithDuration) bool {
	agentSucceeded := false
	safeOutputsFailed := false

	for _, job := range jobDetails {
		normalizedName := normalizeJobName(job.Name)
		if normalizedName == "agent" && strings.EqualFold(job.Conclusion, "success") {
			agentSucceeded = true
		}
		if normalizedName == "safe_outputs" &&
			isFailureConclusion(job.Conclusion) &&
			!strings.EqualFold(job.Conclusion, "cancelled") {
			safeOutputsFailed = true
		}
		if agentSucceeded && safeOutputsFailed {
			return true
		}
	}

	return false
}

func normalizeJobName(name string) string {
	normalized := strings.ToLower(strings.TrimSpace(name))
	normalized = strings.ReplaceAll(normalized, " ", "_")
	return strings.ReplaceAll(normalized, "-", "_")
}

// deriveRunClassification maps a run's AuditComparisonData to one of four
// human-readable classification labels:
//
//   - "risky"       – comparison detected a risk signal (e.g. posture change, new MCP failure).
//   - "normal"      – comparison found no risk signals (stable or minor changes).
//   - "baseline"    – no prior successful run was available to compare against;
//     this run acts as its own baseline.
//   - "unclassified" – comparison data is absent or incomplete.
func deriveRunClassification(comparison *AuditComparisonData) string {
	if comparison == nil {
		return "unclassified"
	}
	if !comparison.BaselineFound {
		return "baseline"
	}
	if comparison.Classification == nil {
		return "unclassified"
	}
	if comparison.Classification.Label == "risky" {
		return "risky"
	}
	return "normal"
}

// renderLogsJSONToWriter outputs the logs data as JSON to w.
// When verbose is false, audit-heavy fields are stripped for compact agentic consumption.
func renderLogsJSONToWriter(w io.Writer, data LogsData, verbose bool) error {
	reportLog.Printf("Rendering logs data as JSON: %d runs, verbose=%v", data.Summary.TotalRuns, verbose)

	if !verbose {
		data = compactLogsData(data)
	}

	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(data)
}

// renderLogsJSON outputs the logs data as JSON to os.Stdout.
// When verbose is false, audit-heavy fields are stripped for compact agentic consumption.
func renderLogsJSON(data LogsData, verbose bool) error {
	return renderLogsJSONToWriter(os.Stdout, data, verbose)
}

// compactLogsData strips audit-heavy fields from LogsData for token-efficient agentic output.
// Removes: comparison, behavior_fingerprint, task_domain, agentic_assessments,
// token_usage_summary, experiments, ambient_context from each run.
// Omits episodes when all are standalone (single-run episodes add no information).
func compactLogsData(data LogsData) LogsData {
	// Strip audit-heavy fields from runs
	for i := range data.Runs {
		data.Runs[i].Comparison = nil
		data.Runs[i].BehaviorFingerprint = nil
		data.Runs[i].TaskDomain = nil
		data.Runs[i].AgenticAssessments = nil
		data.Runs[i].TokenUsageSummary = nil
		data.Runs[i].Experiments = nil
		data.Runs[i].AmbientContext = nil
		data.Runs[i].AwContext = nil
	}

	// Omit episodes when all are standalone (no multi-run episodes)
	allStandalone := true
	for _, ep := range data.Episodes {
		if ep.TotalRuns > 1 {
			allStandalone = false
			break
		}
	}
	if allStandalone {
		// Use empty slices (not nil) so JSON marshaling produces [] instead of null.
		// A null value breaks agent-side Python code that calls len(d.get('episodes', []))
		// because d.get returns None (the existing key's value) rather than the default [].
		data.Episodes = []EpisodeData{}
		data.Edges = []EpisodeEdge{}
	}

	return data
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
	if err := os.MkdirAll(dir, constants.DirPermPublic); err != nil {
		return fmt.Errorf("could not create directory %q for summary file, expected a writable parent path: %w", dir, err)
	}

	// Marshal to JSON with indentation for readability
	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("could not marshal logs data to JSON, expected all summary fields to be serializable: %w", err)
	}

	// Write to file
	if err := os.WriteFile(path, jsonData, constants.FilePermPublic); err != nil {
		return fmt.Errorf("could not write summary file %q, expected a writable path with sufficient disk space: %w", path, err)
	}

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Wrote summary to "+path))
	}

	reportLog.Printf("Successfully wrote summary file: %s", path)
	return nil
}

// renderLogsConsoleToWriter outputs the logs data as formatted console output to w.
func renderLogsConsoleToWriter(w io.Writer, data LogsData) {
	reportLog.Printf("Rendering logs data to console: %d runs, %d errors, %d warnings",
		data.Summary.TotalRuns, data.Summary.TotalErrors, data.Summary.TotalWarnings)

	// Use unified console rendering for the entire logs data structure.
	mcpFailures := data.MCPFailures
	consoleData := data
	consoleData.MCPFailures = nil
	fmt.Fprint(w, console.RenderStruct(consoleData))
	fmt.Fprint(w, console.RenderStruct(struct {
		MCPFailures []mcpFailureSummaryDisplay `console:"title:⚠️  MCP Server Failures,omitempty"`
	}{MCPFailures: mcpFailureSummaryDisplays(mcpFailures)}))

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

	if len(data.Observability) > 0 {
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, console.FormatSectionHeader("Observability Insights"))
		fmt.Fprintln(os.Stderr)
		renderObservabilityInsights(data.Observability)
	}
}

type mcpFailureSummaryDisplay struct {
	ServerName       string `console:"header:Server"`
	Count            int    `console:"header:Failures"`
	WorkflowsDisplay string `console:"header:Workflows,maxlen:60"`
}

func mcpFailureSummaryDisplays(summaries []MCPFailureSummary) []mcpFailureSummaryDisplay {
	return sliceutil.Map(summaries, func(summary MCPFailureSummary) mcpFailureSummaryDisplay {
		return mcpFailureSummaryDisplay{
			ServerName:       summary.ServerName,
			Count:            summary.Count,
			WorkflowsDisplay: summary.WorkflowsDisplay,
		}
	})
}

// renderLogsConsole outputs the logs data as formatted console output to os.Stdout.
func renderLogsConsole(data LogsData) {
	renderLogsConsoleToWriter(os.Stdout, data)
}
