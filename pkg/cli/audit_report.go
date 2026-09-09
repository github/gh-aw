package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"math"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/fileutil"
	"github.com/github/gh-aw/pkg/github"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/scanfindings"
	"github.com/github/gh-aw/pkg/sliceutil"
	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/github/gh-aw/pkg/timeutil"
)

var auditReportLog = logger.New("cli:audit_report")

// AuditData represents the complete structured audit data for a workflow run
type AuditData struct {
	CacheSource             auditCacheSource         `json:"cache_source,omitempty"`
	Overview                OverviewData             `json:"overview"`
	Comparison              *AuditComparisonData     `json:"comparison,omitempty"`
	TaskDomain              *TaskDomainInfo          `json:"task_domain,omitempty"`
	BehaviorFingerprint     *BehaviorFingerprint     `json:"behavior_fingerprint,omitempty"`
	AgenticAssessments      []AgenticAssessment      `json:"agentic_assessments,omitempty"`
	Metrics                 MetricsData              `json:"metrics"`
	KeyFindings             []AuditFinding           `json:"key_findings,omitempty"`
	Recommendations         []Recommendation         `json:"recommendations,omitempty"`
	ObservabilityInsights   []ObservabilityInsight   `json:"observability_insights,omitempty"`
	PerformanceMetrics      *PerformanceMetrics      `json:"performance_metrics,omitempty"`
	EngineConfig            *AuditEngineConfig       `json:"engine_config,omitempty"`
	PromptAnalysis          *PromptAnalysis          `json:"prompt_analysis,omitempty"`
	SessionAnalysis         *SessionAnalysis         `json:"session_analysis,omitempty"`
	SafeOutputSummary       *SafeOutputSummary       `json:"safe_output_summary,omitempty"`
	MCPServerHealth         *MCPServerHealth         `json:"mcp_server_health,omitempty"`
	Jobs                    []JobData                `json:"jobs,omitempty"`
	DownloadedFiles         []FileInfo               `json:"downloaded_files"`
	MissingTools            []MissingToolReport      `json:"missing_tools,omitempty"`
	MissingData             []MissingDataReport      `json:"missing_data,omitempty"`
	Noops                   []NoopReport             `json:"noops,omitempty"`
	MCPFailures             []MCPFailureReport       `json:"mcp_failures,omitempty"`
	SkillActivations        []SkillActivation        `json:"skill_activations,omitempty"`
	FirewallTokenUsage      *TokenUsageSummary       `json:"firewall_token_usage,omitempty"`
	GitHubRateLimitUsage    *GitHubRateLimitUsage    `json:"github_rate_limit_usage,omitempty"`
	FirewallAnalysis        *FirewallAnalysis        `json:"firewall_analysis,omitempty"`
	PolicyAnalysis          *PolicyAnalysis          `json:"policy_analysis,omitempty"`
	RedactedDomainsAnalysis *RedactedDomainsAnalysis `json:"redacted_domains_analysis,omitempty"`
	Errors                  []ValidationIssue        `json:"errors,omitempty"`
	Warnings                []ValidationIssue        `json:"warnings,omitempty"`
	ToolUsage               []ToolUsageInfo          `json:"tool_usage,omitempty"`
	MCPToolUsage            *MCPToolUsageData        `json:"mcp_tool_usage,omitempty"`
	CreatedItems            []CreatedItemReport      `json:"created_items,omitempty"`
	Outcomes                []OutcomeReport          `json:"outcomes,omitempty"`
	OutcomeSummary          *OutcomeSummary          `json:"outcome_summary,omitempty"`
	Experiments             *ExperimentData          `json:"experiments,omitempty"`
	Graders                 *GradersData             `json:"graders,omitempty"`
}

// AuditFinding represents a key insight discovered during audit
type AuditFinding struct {
	Category    string                     `json:"category"`         // e.g., "error", "performance", "cost", "tooling"
	Severity    scanfindings.SeverityLevel `json:"severity"`         // shared severity vocabulary
	Title       string                     `json:"title"`            // Brief title
	Description string                     `json:"description"`      // Detailed description
	Impact      string                     `json:"impact,omitempty"` // What impact this has
}

// Recommendation represents an actionable suggestion
type Recommendation struct {
	Priority string `json:"priority"`          // "high", "medium", "low"
	Action   string `json:"action"`            // What to do
	Reason   string `json:"reason"`            // Why to do it
	Example  string `json:"example,omitempty"` // Example of how to implement
}

// PerformanceMetrics provides aggregated performance statistics
type PerformanceMetrics struct {
	TokensPerMinute float64 `json:"tokens_per_minute,omitempty"`
	AvgToolDuration string  `json:"avg_tool_duration,omitempty"`
	MostUsedTool    string  `json:"most_used_tool,omitempty"`
	NetworkRequests int     `json:"network_requests,omitempty"`
}

// OverviewData contains basic information about the workflow run
type OverviewData struct {
	RunID        int64      `json:"run_id" console:"header:Run ID"`
	WorkflowName string     `json:"workflow_name" console:"header:Workflow"`
	Status       string     `json:"status" console:"header:Status"`
	Conclusion   string     `json:"conclusion,omitempty" console:"header:Conclusion,omitempty"`
	CreatedAt    time.Time  `json:"created_at" console:"header:Created At"`
	StartedAt    time.Time  `json:"started_at,omitzero" console:"header:Started At,omitempty"`
	UpdatedAt    time.Time  `json:"updated_at,omitzero" console:"header:Updated At,omitempty"`
	Duration     string     `json:"duration,omitempty" console:"header:Duration,omitempty"`
	Event        string     `json:"event" console:"header:Event"`
	Branch       string     `json:"branch" console:"header:Branch"`
	URL          string     `json:"url" console:"header:URL"`
	LogsPath     string     `json:"logs_path,omitempty" console:"header:Files,omitempty"`
	Experiment   string     `json:"experiment,omitempty" console:"header:Experiment,omitempty"` // compact A/B experiment label, e.g. "style=concise"
	AwContext    *AwContext `json:"context,omitempty" console:"-"`                              // aw_context data from aw_info.json
}

// MetricsData contains execution metrics
type MetricsData struct {
	TokenUsage     int                    `json:"token_usage,omitempty" console:"header:Token Usage,format:number,omitempty"`
	AIC            float64                `json:"aic,omitempty"`
	AmbientContext *AmbientContextMetrics `json:"ambient_context,omitempty" console:"title:Ambient Context,omitempty"`
	WorkingSet     *WorkingSetMetrics     `json:"working_set,omitempty" console:"-"`
	ActionMinutes  float64                `json:"action_minutes,omitempty" console:"header:Action Minutes,omitempty"`
	Turns          int                    `json:"turns,omitempty" console:"header:Turns,omitempty"`
	ErrorCount     int                    `json:"error_count" console:"header:Errors"`
	WarningCount   int                    `json:"warning_count" console:"header:Warnings"`
}

// JobData contains information about individual jobs
type JobData struct {
	Name       string        `json:"name" console:"header:Name"`
	Status     string        `json:"status" console:"header:Status"`
	Conclusion string        `json:"conclusion,omitempty" console:"header:Conclusion,omitempty"`
	Duration   string        `json:"duration,omitempty" console:"header:Duration,omitempty"`
	Steps      []JobStepData `json:"steps,omitempty"`
}

// JobStepData is an alias for JobStep, kept to avoid renaming the existing
// "Data" suffixed usages of this type within this package.
type JobStepData = JobStep

// FileInfo contains information about downloaded artifact files
type FileInfo struct {
	Path        string `json:"path"`
	Size        int64  `json:"size"`
	Description string `json:"description"`
}

// CreatedItemReport represents a single item executed in GitHub by a safe output handler.
// URL is present for creation types (e.g. create_issue, add_comment) but may be empty
// for modification types (e.g. add_labels, close_issue) that do not return a URL.
type CreatedItemReport struct {
	Type        string         `json:"type" console:"header:Type"`
	URL         string         `json:"url,omitempty" console:"header:URL,omitempty"`
	Number      int            `json:"number,omitempty" console:"header:Number,omitempty"`
	Repo        string         `json:"repo,omitempty" console:"header:Repo,omitempty"`
	TemporaryID string         `json:"temporaryId,omitempty" console:"header:Temp ID,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty" console:"-"`
	BeforeState map[string]any `json:"before_state,omitempty" console:"-"`
	AfterState  map[string]any `json:"after_state,omitempty" console:"-"`
	Timestamp   string         `json:"timestamp" console:"header:Timestamp"`
}

// ToolUsageInfo contains aggregated tool usage statistics
type ToolUsageInfo struct {
	Name          string `json:"name" console:"header:Tool"`
	CallCount     int    `json:"call_count" console:"header:Calls"`
	MaxInputSize  int    `json:"max_input_size,omitempty" console:"header:Max Input,format:number,omitempty"`
	MaxOutputSize int    `json:"max_output_size,omitempty" console:"header:Max Output,format:number,omitempty"`
	MaxDuration   string `json:"max_duration,omitempty" console:"header:Max Duration,omitempty"`
	OutputSample  string `json:"output_sample,omitempty" console:"header:Response Preview,omitempty"`
}

// IntegrityFilterSummary contains aggregate DIFC integrity-filter activity.
type IntegrityFilterSummary struct {
	TotalFiltered          int            `json:"total_filtered"`
	RunsWithFilteredEvents int            `json:"runs_with_filtered_events,omitempty"`
	FilteredServerCounts   map[string]int `json:"filtered_server_counts,omitempty"`
	FilteredToolCounts     map[string]int `json:"filtered_tool_counts,omitempty"`
	FilteredReasonCounts   map[string]int `json:"filtered_reason_counts,omitempty"`
}

// MCPToolUsageData contains detailed MCP tool usage statistics and individual call records
type MCPToolUsageData struct {
	Summary            []MCPToolSummary        `json:"summary"`                        // Aggregated statistics per tool
	ToolCalls          []MCPToolCall           `json:"tool_calls"`                     // Individual tool call records
	Servers            []MCPServerStats        `json:"servers,omitempty"`              // Server-level statistics
	FilteredEvents     []DifcFilteredEvent     `json:"filtered_events,omitempty"`      // DIFC filtered events
	Integrity          *IntegrityFilterSummary `json:"integrity,omitempty"`            // Aggregate DIFC integrity-filter activity
	GuardPolicySummary *GuardPolicySummary     `json:"guard_policy_summary,omitempty"` // Guard policy enforcement summary
}

// MCPToolSummary contains aggregated statistics for a single MCP tool
type MCPToolSummary struct {
	ServerName         string `json:"server_name" console:"header:Server"`
	ToolUsageStatsBase `json:"-" console:"-"`
	ToolName           string `json:"tool_name" console:"header:Tool"`
	CallCount          int    `json:"call_count" console:"header:Calls"`
	TotalInputSize     int    `json:"total_input_size" console:"header:Total Input,format:number"`
	TotalOutputSize    int    `json:"total_output_size" console:"header:Total Output,format:number"`
	MaxInputSize       int    `json:"max_input_size" console:"header:Max Input,format:number"`
	MaxOutputSize      int    `json:"max_output_size" console:"header:Max Output,format:number"`
	AvgDuration        string `json:"avg_duration,omitempty" console:"header:Avg Duration,omitempty"`
	MaxDuration        string `json:"max_duration,omitempty" console:"header:Max Duration,omitempty"`
	ErrorCount         int    `json:"error_count,omitempty" console:"header:Errors,omitempty"`
}

func (s *MCPToolSummary) syncFieldsFromBase() {
	s.syncFields(&s.ToolName, &s.CallCount, &s.MaxOutputSize, &s.MaxDuration)
}

func (s *MCPToolSummary) syncBaseFromFields() {
	s.syncFromFields(s.ToolName, s.CallCount, s.MaxOutputSize, s.MaxDuration)
}

// MCPToolCall represents a single MCP tool call with full details
type MCPToolCall struct {
	ToolCallID          string `json:"tool_call_id,omitempty"`
	Timestamp           string `json:"timestamp"`
	ServerName          string `json:"server_name"`
	ToolName            string `json:"tool_name"`
	Method              string `json:"method,omitempty"`
	InputSize           int    `json:"input_size"`
	OutputSize          int    `json:"output_size"`
	Duration            string `json:"duration,omitempty"`
	Status              string `json:"status"`
	Error               string `json:"error,omitempty"`
	EffectiveTokenDelta int    `json:"effective_token_delta,omitempty"` // Change in effective tokens caused by this tool call result
}

// MCPServerStats contains server-level statistics
type MCPServerStats struct {
	MCPServerStatsBase
	// RequestCount is kept for backward-compatible report schemas that label per-server
	// request volume; in MCP usage summaries this currently mirrors ToolCallCount.
	RequestCount    int    `json:"request_count" console:"header:Requests"`
	TotalInputSize  int    `json:"total_input_size" console:"header:Total Input,format:number"`
	TotalOutputSize int    `json:"total_output_size" console:"header:Total Output,format:number"`
	AvgDuration     string `json:"avg_duration,omitempty" console:"header:Avg Duration,omitempty"`
}

// GuardPolicySummary contains summary statistics for guard policy enforcement.
// Guard policies control which tool calls the MCP Gateway allows based on
// repository scope (repos) and content integrity level (min-integrity).
type GuardPolicySummary struct {
	TotalBlocked        int                `json:"total_blocked"`
	IntegrityBlocked    int                `json:"integrity_blocked"`             // Blocked by min-integrity (-32006)
	RepoScopeBlocked    int                `json:"repo_scope_blocked"`            // Blocked by repos scope (-32002)
	AccessDenied        int                `json:"access_denied"`                 // General access denied (-32001)
	BlockedUserDenied   int                `json:"blocked_user_denied,omitempty"` // Content from blocked user (-32005)
	PermissionDenied    int                `json:"permission_denied,omitempty"`   // Insufficient permissions (-32003)
	PrivateRepoDenied   int                `json:"private_repo_denied,omitempty"` // Private repository denied (-32004)
	Events              []GuardPolicyEvent `json:"events"`
	BlockedToolCounts   map[string]int     `json:"blocked_tool_counts,omitempty"`   // tool name -> blocked count
	BlockedServerCounts map[string]int     `json:"blocked_server_counts,omitempty"` // server ID -> blocked count
}

// PolicySummaryDisplay is a display-optimized version of PolicyAnalysis for console rendering
type PolicySummaryDisplay struct {
	Policy        string `console:"header:Policy"`
	TotalRequests int    `console:"header:Total Requests"`
	Allowed       int    `console:"header:Allowed"`
	Denied        int    `console:"header:Denied"`
	UniqueDomains int    `console:"header:Unique Domains"`
}

// OverviewDisplay is a display-optimized version of OverviewData for console rendering
type OverviewDisplay struct {
	RunID      int64  `console:"header:Run ID"`
	Workflow   string `console:"header:Workflow"`
	Status     string `console:"header:Status"`
	Duration   string `console:"header:Duration,omitempty"`
	Event      string `console:"header:Event"`
	Branch     string `console:"header:Branch"`
	URL        string `console:"header:URL"`
	Files      string `console:"header:Files,omitempty"`
	Experiment string `console:"header:Experiment,omitempty"`
}

// buildAuditData creates structured audit data from workflow run information
func buildAuditData(ctx context.Context, processedRun ProcessedRun, metrics LogMetrics, mcpToolUsage *MCPToolUsageData) AuditData {
	auditData, createdItems := buildLocalAuditData(processedRun, metrics, mcpToolUsage)
	addAuditOutcomeSummary(ctx, &auditData, createdItems)
	return auditData
}

// buildLocalAuditData creates an audit exclusively from downloaded run data.
// Callers such as logs --audit use this path to avoid additional GitHub API calls.
func buildLocalAuditData(processedRun ProcessedRun, metrics LogMetrics, mcpToolUsage *MCPToolUsageData) (AuditData, []CreatedItemReport) {
	run := processedRun.Run
	auditReportLog.Printf("Building audit data for run ID %d", run.DatabaseID)
	expData := extractExperimentData(run.LogsPath)
	overview := buildAuditOverview(run, expData)
	metricsData, inferredEngineID := buildAuditMetrics(processedRun, metrics)
	jobs := buildAuditJobs(processedRun.JobDetails)
	errors := extractAuditErrors(run)
	downloadedFiles := extractDownloadedFiles(run.LogsPath)
	toolUsage := buildAuditToolUsage(metrics, mcpToolUsage)
	createdItems := extractCreatedItemsFromManifest(run.LogsPath)
	taskDomain, behaviorFingerprint, agenticAssessments := buildAuditAssessments(processedRun, metricsData, toolUsage, createdItems, overview.AwContext)
	findings, recommendations, observabilityInsights := buildAuditNarrative(processedRun, metricsData, errors, toolUsage, createdItems, agenticAssessments)
	auditData := assembleAuditData(auditDataInputs{
		processedRun:          processedRun,
		metrics:               metrics,
		mcpToolUsage:          mcpToolUsage,
		expData:               expData,
		inferredEngineID:      inferredEngineID,
		overview:              overview,
		metricsData:           metricsData,
		jobs:                  jobs,
		downloadedFiles:       downloadedFiles,
		errors:                errors,
		toolUsage:             toolUsage,
		createdItems:          createdItems,
		taskDomain:            taskDomain,
		behaviorFingerprint:   behaviorFingerprint,
		agenticAssessments:    agenticAssessments,
		findings:              findings,
		recommendations:       recommendations,
		observabilityInsights: observabilityInsights,
	})
	return auditData, createdItems
}

func buildAuditOverview(run WorkflowRun, expData *ExperimentData) OverviewData {
	overview := OverviewData{
		RunID:        run.DatabaseID,
		WorkflowName: run.WorkflowName,
		Status:       run.Status,
		Conclusion:   run.Conclusion,
		CreatedAt:    run.CreatedAt,
		StartedAt:    run.StartedAt,
		UpdatedAt:    run.UpdatedAt,
		Event:        run.Event,
		Branch:       run.HeadBranch,
		URL:          run.URL,
		Experiment:   formatExperimentLabel(expData),
	}
	if run.LogsPath != "" {
		overview.LogsPath = run.LogsPath
	}
	if run.Duration > 0 {
		overview.Duration = timeutil.FormatDuration(run.Duration)
	}
	if run.LogsPath == "" {
		return overview
	}
	awInfoPath := filepath.Join(run.LogsPath, "aw_info.json")
	if awInfo, err := parseAwInfo(awInfoPath, false); err == nil && awInfo != nil {
		overview.AwContext = awInfo.Context
	}
	return overview
}

func buildAuditMetrics(processedRun ProcessedRun, metrics LogMetrics) (MetricsData, string) {
	run := processedRun.Run
	metricsData := MetricsData{
		TokenUsage:   run.TokenUsage,
		Turns:        run.Turns,
		ErrorCount:   run.ErrorCount,
		WarningCount: run.WarningCount,
	}
	if run.Conclusion == "failure" && metricsData.ErrorCount == 0 {
		metricsData.ErrorCount = 1
	}

	fallbackMetrics, inferredEngineID := lookupFallbackMetrics(run.LogsPath, metricsData)
	applyFallbackMetrics(&metricsData, processedRun, metrics, fallbackMetrics)
	populateAuditMetricContext(&metricsData, processedRun.TokenUsage)
	metricsData.WorkingSet = processedRun.WorkingSet
	return metricsData, inferredEngineID
}

func lookupFallbackMetrics(logsPath string, metricsData MetricsData) (LogMetrics, string) {
	if logsPath == "" {
		return LogMetrics{}, ""
	}
	needsFallbackMetrics := metricsData.TokenUsage == 0 || metricsData.Turns == 0
	needsFallbackEngineConfig := findAwInfoPath(logsPath) == ""
	if !needsFallbackMetrics && !needsFallbackEngineConfig {
		return LogMetrics{}, ""
	}
	return inferFallbackLogMetrics(logsPath)
}

func applyFallbackMetrics(metricsData *MetricsData, processedRun ProcessedRun, metrics LogMetrics, fallbackMetrics LogMetrics) {
	if metricsData.TokenUsage == 0 && processedRun.TokenUsage != nil {
		metricsData.TokenUsage = processedRun.TokenUsage.TotalInputTokens + processedRun.TokenUsage.TotalOutputTokens
	}
	if metricsData.TokenUsage == 0 && metrics.TokenUsage > 0 {
		metricsData.TokenUsage = metrics.TokenUsage
	}
	if metricsData.Turns == 0 && metrics.Turns > 0 {
		metricsData.Turns = metrics.Turns
	}
	if metricsData.TokenUsage == 0 && fallbackMetrics.TokenUsage > 0 {
		metricsData.TokenUsage = fallbackMetrics.TokenUsage
	}
	if metricsData.Turns == 0 && fallbackMetrics.Turns > 0 {
		metricsData.Turns = fallbackMetrics.Turns
	}
}

func populateAuditMetricContext(metricsData *MetricsData, tokenUsage *TokenUsageSummary) {
	if tokenUsage != nil && tokenUsage.TotalAIC > 0 {
		metricsData.AIC = tokenUsage.TotalAIC
	}
	if tokenUsage != nil && tokenUsage.AmbientContext != nil {
		metricsData.AmbientContext = tokenUsage.AmbientContext
	}
}

func buildAuditJobs(jobDetails []JobInfoWithDuration) []JobData {
	return sliceutil.Map(jobDetails, func(jobDetail JobInfoWithDuration) JobData {
		job := JobData{
			Name:       jobDetail.Name,
			Status:     jobDetail.Status,
			Conclusion: jobDetail.Conclusion,
			Steps:      jobDetail.Steps,
		}
		if jobDetail.Duration > 0 {
			job.Duration = timeutil.FormatDuration(jobDetail.Duration)
		}
		return job
	})
}

func extractAuditErrors(run WorkflowRun) []ValidationIssue {
	if run.Conclusion != "failure" || run.LogsPath == "" {
		return nil
	}
	if stepErrors := extractPreAgentStepErrors(run.LogsPath); len(stepErrors) > 0 {
		return stepErrors
	}
	return nil
}

func buildAuditToolUsage(metrics LogMetrics, mcpToolUsage *MCPToolUsageData) []ToolUsageInfo {
	return mergeMCPToolUsageInfo(buildToolUsageInfo(metrics), mcpToolUsage)
}

func buildAuditAssessments(processedRun ProcessedRun, metricsData MetricsData, toolUsage []ToolUsageInfo, createdItems []CreatedItemReport, awContext *AwContext) (*TaskDomainInfo, *BehaviorFingerprint, []AgenticAssessment) {
	taskDomain := detectTaskDomain(processedRun, createdItems, toolUsage, awContext)
	behaviorFingerprint := buildBehaviorFingerprint(processedRun, metricsData, toolUsage, createdItems, awContext)
	agenticAssessments := buildAgenticAssessments(processedRun, metricsData, toolUsage, createdItems, taskDomain, behaviorFingerprint, awContext)
	return taskDomain, behaviorFingerprint, agenticAssessments
}

func buildAuditNarrative(processedRun ProcessedRun, metricsData MetricsData, errors []ValidationIssue, toolUsage []ToolUsageInfo, createdItems []CreatedItemReport, agenticAssessments []AgenticAssessment) ([]AuditFinding, []Recommendation, []ObservabilityInsight) {
	findings := generateFindings(processedRun, metricsData, errors)
	findings = append(findings, generateAgenticAssessmentFindings(agenticAssessments)...)

	recommendations := generateRecommendations(processedRun, metricsData, findings)
	recommendations = append(recommendations, generateAgenticAssessmentRecommendations(agenticAssessments)...)

	observabilityInsights := buildAuditObservabilityInsights(processedRun, metricsData, toolUsage, createdItems)
	observabilityInsights = append(observabilityInsights, buildDrain3Insights(processedRun, metricsData, toolUsage)...)
	return findings, recommendations, observabilityInsights
}

type auditDataInputs struct {
	processedRun          ProcessedRun
	metrics               LogMetrics
	mcpToolUsage          *MCPToolUsageData
	expData               *ExperimentData
	inferredEngineID      string
	overview              OverviewData
	metricsData           MetricsData
	jobs                  []JobData
	downloadedFiles       []FileInfo
	errors                []ValidationIssue
	toolUsage             []ToolUsageInfo
	createdItems          []CreatedItemReport
	taskDomain            *TaskDomainInfo
	behaviorFingerprint   *BehaviorFingerprint
	agenticAssessments    []AgenticAssessment
	findings              []AuditFinding
	recommendations       []Recommendation
	observabilityInsights []ObservabilityInsight
}

func assembleAuditData(inputs auditDataInputs) AuditData {
	run := inputs.processedRun.Run
	metricsData := inputs.metricsData
	if run.ActionMinutes > 0 {
		metricsData.ActionMinutes = run.ActionMinutes
	} else if run.Duration > 0 {
		metricsData.ActionMinutes = math.Ceil(run.Duration.Minutes())
	}

	performanceMetrics := generatePerformanceMetrics(inputs.processedRun, metricsData, inputs.toolUsage)
	chainMetrics := buildSafeOutputChainMetrics(run.LogsPath)
	engineConfig := extractEngineConfigWithInferredEngine(run.LogsPath, inputs.inferredEngineID)
	promptAnalysis := extractPromptAnalysis(run.LogsPath)
	sessionAnalysis := buildSessionAnalysis(inputs.processedRun, inputs.metrics)
	safeOutputSummary := buildSafeOutputSummary(inputs.createdItems, chainMetrics)
	mcpServerHealth := buildMCPServerHealth(inputs.mcpToolUsage, inputs.processedRun.MCPFailures)

	if auditReportLog.Enabled() {
		auditReportLog.Printf("Built audit data: %d jobs, %d errors, %d tool types, %d findings, %d recommendations",
			len(inputs.jobs), len(inputs.errors), len(inputs.toolUsage), len(inputs.findings), len(inputs.recommendations))
	}

	return AuditData{
		Overview:                inputs.overview,
		TaskDomain:              inputs.taskDomain,
		BehaviorFingerprint:     inputs.behaviorFingerprint,
		AgenticAssessments:      inputs.agenticAssessments,
		Metrics:                 metricsData,
		KeyFindings:             inputs.findings,
		Recommendations:         inputs.recommendations,
		ObservabilityInsights:   inputs.observabilityInsights,
		PerformanceMetrics:      performanceMetrics,
		EngineConfig:            engineConfig,
		PromptAnalysis:          promptAnalysis,
		SessionAnalysis:         sessionAnalysis,
		SafeOutputSummary:       safeOutputSummary,
		MCPServerHealth:         mcpServerHealth,
		Jobs:                    inputs.jobs,
		DownloadedFiles:         inputs.downloadedFiles,
		MissingTools:            inputs.processedRun.MissingTools,
		MissingData:             inputs.processedRun.MissingData,
		Noops:                   inputs.processedRun.Noops,
		MCPFailures:             inputs.processedRun.MCPFailures,
		SkillActivations:        inputs.processedRun.SkillActivations,
		FirewallTokenUsage:      inputs.processedRun.TokenUsage,
		GitHubRateLimitUsage:    inputs.processedRun.GitHubRateLimitUsage,
		FirewallAnalysis:        inputs.processedRun.FirewallAnalysis,
		PolicyAnalysis:          inputs.processedRun.PolicyAnalysis,
		RedactedDomainsAnalysis: inputs.processedRun.RedactedDomainsAnalysis,
		Errors:                  inputs.errors,
		ToolUsage:               inputs.toolUsage,
		MCPToolUsage:            inputs.mcpToolUsage,
		CreatedItems:            inputs.createdItems,
		Experiments:             inputs.expData,
		Graders:                 extractGradersData(run.LogsPath),
	}
}

func addAuditOutcomeSummary(ctx context.Context, auditData *AuditData, createdItems []CreatedItemReport) {
	if len(createdItems) == 0 {
		return
	}
	mapping := github.LoadObjectiveMapping()
	outcomeReports := EvaluateOutcomes(ctx, createdItems, "", mapping)
	auditData.Outcomes = outcomeReports
	outcomeSummary := ComputeOutcomeSummary(outcomeReports, mapping)
	auditData.OutcomeSummary = &outcomeSummary
}

// extractDownloadedFiles scans the logs directory recursively and returns file information.
// It walks subdirectories (aw-prompts/, base/, etc.) so the JSON output enumerates every
// file available for inspection. Baseline directories are excluded to keep output focused.
func extractDownloadedFiles(logsPath string) []FileInfo {
	auditReportLog.Printf("Extracting downloaded files from: %s", logsPath)
	var files []FileInfo

	absLogsPath, err := filepath.Abs(logsPath)
	if err != nil {
		auditReportLog.Printf("Failed to resolve absolute logs path: %v", err)
		absLogsPath = logsPath
	}

	err = filepath.WalkDir(absLogsPath, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil // skip unreadable entries
		}

		// Skip baseline directories — they belong to comparison runs, not the audited run
		if d.IsDir() && strings.HasPrefix(d.Name(), "baseline-") {
			return filepath.SkipDir
		}

		// Skip the base/ directory — it's the full cloned repo, not a log artifact
		if d.IsDir() && d.Name() == "base" && path == filepath.Join(absLogsPath, "base") {
			return filepath.SkipDir
		}

		// Skip directories themselves (we only list files)
		if d.IsDir() {
			return nil
		}

		fileInfo := FileInfo{
			Path:        path,
			Description: describeFile(d.Name()),
		}

		if info, statErr := os.Stat(path); statErr == nil {
			fileInfo.Size = info.Size()
		}

		files = append(files, fileInfo)
		return nil
	})
	if err != nil {
		auditReportLog.Printf("Failed to walk logs directory: %v", err)
	}

	auditReportLog.Printf("Extracted %d files from logs directory", len(files))
	return files
}

// safeOutputItemsManifestFilename is the name of the manifest artifact file containing
// all items created in GitHub by safe output handlers.
const safeOutputItemsManifestFilename = "safe-output-items.jsonl"

// extractCreatedItemsFromManifest reads the safe output items manifest from the run
// output directory and returns the list of created items. Returns nil if the file
// does not exist or cannot be parsed.
func extractCreatedItemsFromManifest(logsPath string) []CreatedItemReport {
	if logsPath == "" {
		return nil
	}

	manifestPath := filepath.Join(logsPath, safeOutputItemsManifestFilename)
	f, err := os.Open(manifestPath)
	if err != nil {
		// File not present is expected for runs without safe outputs
		return nil
	}
	defer f.Close()

	var items []CreatedItemReport
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var item CreatedItemReport
		if err := json.Unmarshal([]byte(line), &item); err != nil {
			auditReportLog.Printf("Skipping invalid manifest line: %v", err)
			continue
		}
		if item.Type == "" {
			continue
		}
		items = append(items, item)
	}

	if err := scanner.Err(); err != nil {
		auditReportLog.Printf("Error reading manifest file: %v", err)
	}

	auditReportLog.Printf("Extracted %d created item(s) from manifest", len(items))
	return items
}

// describeFile provides a short description for known artifact files
func describeFile(filename string) string {
	descriptions := map[string]string{
		"aw_info.json":                            "Engine configuration and workflow metadata",
		"safe_output.jsonl":                       "Safe outputs from workflow execution",
		safeOutputItemsManifestFilename:           "Created items manifest (audit trail)",
		constants.SafeOutputErrorsFilename:        "Safe outputs failure diagnostics (error code, message, failing types)",
		constants.AgentOutputFilename.String():    "Validated safe outputs",
		"aw.patch":                                "Git patch of changes made during execution",
		"agent-stdio.log":                         "Agent standard output/error logs",
		"log.md":                                  "Human-readable agent session summary",
		"firewall.md":                             "Firewall log analysis report",
		"run_summary.json":                        "Cached summary of workflow run analysis",
		forecastAICCacheFileName:                  "Cached AI Credits (AIC) value for forecasting",
		"prompt.txt":                              "Input prompt for AI agent",
		constants.GraderResultsFilename.String():  "Deterministic grader results for the run",
		constants.GraderManifestFilename.String(): "Grader manifest (configured graders and thresholds)",
	}

	if desc, ok := descriptions[filename]; ok {
		return desc
	}

	// Handle directories
	if strings.HasSuffix(filename, "/") {
		return "Directory"
	}

	// Common directory names
	if filename == "agent_output" || filename == "firewall-logs" || filename == "squid-logs" {
		return "Directory containing log files"
	}
	if filename == "aw-prompts" {
		return "Directory containing AI prompts"
	}

	// Handle file patterns by extension
	if strings.HasSuffix(filename, ".log") {
		return "Log file"
	}
	if strings.HasSuffix(filename, ".md") {
		return "Markdown documentation"
	}
	if strings.HasSuffix(filename, ".json") {
		return "JSON data file"
	}
	if strings.HasSuffix(filename, ".jsonl") {
		return "JSON Lines data file"
	}
	if strings.HasSuffix(filename, ".patch") {
		return "Git patch file"
	}
	if strings.HasSuffix(filename, ".txt") {
		return "Text file"
	}

	return ""
}

// parseDurationString parses a duration string back to time.Duration (best effort)
func parseDurationString(s string) time.Duration {
	d, _ := time.ParseDuration(s)
	return d
}

// extractPreAgentStepErrors scans workflow step log files for actionable failure content.
// It always prefers GitHub Actions ##[error] annotations from step logs. When no
// annotations are found and the agent did not execute, it falls back to the final
// step log content. When the agent did execute, it falls back to a short excerpt from
// agent-stdio.log so failed runs still surface concrete diagnostics.
//
// Step log files are stored in workflow-logs/{job}/{step_num}_{step_name}.txt after
// downloading via downloadWorkflowRunLogs. The function first scans all step logs for
// ##[error] annotations (GitHub Actions error annotations), which are the most precise
// failure indicators. If none are found, it falls back to the content of the last step
// (highest step number) as a general failure indicator.
func extractPreAgentStepErrors(logsPath string) []ValidationIssue {
	agentStdioPath := filepath.Join(logsPath, "agent-stdio.log")
	agentRan := fileutil.FileExists(agentStdioPath)

	const maxMessageLen = 1500

	workflowLogsDir := filepath.Join(logsPath, "workflow-logs")
	if _, err := os.Stat(workflowLogsDir); err != nil {
		auditReportLog.Printf("workflow-logs directory not found, skipping step log extraction")
		if agentError := extractAgentFailureError(agentRan, agentStdioPath, maxMessageLen); len(agentError) > 0 {
			return agentError
		}
		return nil
	}

	errorAnnotations, lastStep, err := scanWorkflowStepLogs(workflowLogsDir, maxMessageLen)
	if err != nil {
		return nil
	}
	if len(errorAnnotations) > 0 {
		return errorAnnotations
	}

	if agentError := extractAgentFailureError(agentRan, agentStdioPath, maxMessageLen); len(agentError) > 0 {
		return agentError
	}

	return extractLastStepFallbackError(lastStep, workflowLogsDir, maxMessageLen)
}

type stepLog struct {
	path    string
	num     int
	stepKey string
}

func scanWorkflowStepLogs(workflowLogsDir string, maxMessageLen int) ([]ValidationIssue, *stepLog, error) {
	jobDirs, err := os.ReadDir(workflowLogsDir)
	if err != nil {
		return nil, nil, err
	}

	var lastStep *stepLog
	var errorAnnotations []ValidationIssue

	for _, jobEntry := range jobDirs {
		if !jobEntry.IsDir() {
			lastStep, errorAnnotations = scanFlatStepLog(
				workflowLogsDir,
				jobEntry.Name(),
				lastStep,
				errorAnnotations,
				maxMessageLen,
			)
			continue
		}
		lastStep, errorAnnotations = scanNestedStepLogs(
			workflowLogsDir,
			jobEntry.Name(),
			lastStep,
			errorAnnotations,
			maxMessageLen,
		)
	}

	return errorAnnotations, lastStep, nil
}

func scanFlatStepLog(
	workflowLogsDir, filename string,
	lastStep *stepLog,
	errorAnnotations []ValidationIssue,
	maxMessageLen int,
) (*stepLog, []ValidationIssue) {
	if !strings.HasSuffix(filename, ".txt") {
		return lastStep, errorAnnotations
	}
	num, jobName := parseStepFilename(filename)
	if num <= 0 {
		return lastStep, errorAnnotations
	}

	flatFilePath := filepath.Join(workflowLogsDir, filename)
	lastStep = updateLastStep(lastStep, flatFilePath, num, jobName)
	errorAnnotations = appendErrorAnnotation(errorAnnotations, flatFilePath, jobName, num, maxMessageLen, "flat job log")
	return lastStep, errorAnnotations
}

func scanNestedStepLogs(
	workflowLogsDir, jobName string,
	lastStep *stepLog,
	errorAnnotations []ValidationIssue,
	maxMessageLen int,
) (*stepLog, []ValidationIssue) {
	jobDir := filepath.Join(workflowLogsDir, jobName)
	stepFiles, err := os.ReadDir(jobDir)
	if err != nil {
		return lastStep, errorAnnotations
	}

	for _, stepFile := range stepFiles {
		if stepFile.IsDir() || !strings.HasSuffix(stepFile.Name(), ".txt") {
			continue
		}
		num, stepName := parseStepFilename(stepFile.Name())
		if num <= 0 {
			continue
		}

		stepFilePath := filepath.Join(jobDir, stepFile.Name())
		stepKey := path.Join(jobName, stepName)
		lastStep = updateLastStep(lastStep, stepFilePath, num, stepKey)
		errorAnnotations = appendErrorAnnotation(errorAnnotations, stepFilePath, stepKey, num, maxMessageLen, "step")
	}

	return lastStep, errorAnnotations
}

func updateLastStep(lastStep *stepLog, path string, num int, stepKey string) *stepLog {
	if lastStep == nil || num > lastStep.num {
		return &stepLog{
			path:    path,
			num:     num,
			stepKey: stepKey,
		}
	}
	return lastStep
}

func appendErrorAnnotation(
	errorAnnotations []ValidationIssue,
	filePath, stepKey string,
	num int,
	maxMessageLen int,
	logLabel string,
) []ValidationIssue {
	errorLines := extractGHErrorLines(filePath)
	if len(errorLines) == 0 {
		return errorAnnotations
	}

	message := stringutil.Truncate(strings.Join(errorLines, "\n"), maxMessageLen)
	auditReportLog.Printf("Extracted ##[error] annotations from %s %s (%d)", logLabel, stepKey, num)

	return append(errorAnnotations, ValidationIssue{
		Type:    "step_failure",
		File:    stepKey,
		Message: message,
	})
}

func extractGHErrorLines(filePath string) []string {
	content, err := os.ReadFile(filePath)
	if err != nil {
		auditReportLog.Printf("Failed to read step log %s: %v", filePath, err)
		return nil
	}

	var errorLines []string
	for line := range strings.SplitSeq(string(content), "\n") {
		if strings.Contains(line, "##[error]") {
			stripped := stripGHALogTimestamps(line)
			if stripped != "" && !isAgentToolResultAnnotation(stripped) {
				errorLines = append(errorLines, stripped)
			}
		}
	}

	return errorLines
}

func isAgentToolResultAnnotation(line string) bool {
	_, payload, found := strings.Cut(line, "##[error]")
	if !found {
		return false
	}

	var event struct {
		Type    string `json:"type"`
		Message struct {
			Content []struct {
				Type string `json:"type"`
			} `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(payload)), &event); err != nil || event.Type != "user" {
		return false
	}
	hasToolResult := false
	for _, content := range event.Message.Content {
		if content.Type != "tool_result" {
			return false
		}
		hasToolResult = true
	}
	return hasToolResult
}

func extractAgentFailureError(agentRan bool, agentStdioPath string, maxMessageLen int) []ValidationIssue {
	if !agentRan {
		return nil
	}
	if agentExcerpt := extractAgentStdioFailureExcerpt(agentStdioPath, maxMessageLen); agentExcerpt != "" {
		return []ValidationIssue{{
			Type:    "agent_failure",
			File:    "agent-stdio.log",
			Message: agentExcerpt,
		}}
	}
	return nil
}

func extractLastStepFallbackError(lastStep *stepLog, workflowLogsDir string, maxMessageLen int) []ValidationIssue {
	if lastStep == nil {
		auditReportLog.Printf("No step log files found in %s", workflowLogsDir)
		return nil
	}

	content, err := os.ReadFile(lastStep.path)
	if err != nil {
		auditReportLog.Printf("Failed to read step log %s: %v", lastStep.path, err)
		return nil
	}

	message := stripGHALogTimestamps(strings.TrimSpace(string(content)))
	if message == "" {
		return nil
	}

	message = stringutil.Truncate(message, maxMessageLen)

	auditReportLog.Printf("Extracted pre-agent step error from %s (step %d) as fallback", lastStep.stepKey, lastStep.num)
	return []ValidationIssue{{
		Type:    "step_failure",
		File:    lastStep.stepKey,
		Message: message,
	}}
}

func extractAgentStdioFailureExcerpt(agentStdioPath string, maxMessageLen int) string {
	f, err := os.Open(agentStdioPath)
	if err != nil {
		auditReportLog.Printf("Failed to read %s: %v", agentStdioPath, err)
		return ""
	}
	defer f.Close()

	// Read only the tail of the file to bound memory use for large logs.
	const maxReadBytes int64 = 64 * 1024 // 64 KB tail window

	fi, err := f.Stat()
	if err != nil {
		auditReportLog.Printf("Failed to stat %s: %v", agentStdioPath, err)
		return ""
	}
	if fi.Size() > maxReadBytes {
		if _, err := f.Seek(-maxReadBytes, io.SeekEnd); err != nil {
			auditReportLog.Printf("Failed to seek in %s: %v", agentStdioPath, err)
			return ""
		}
	}

	tail, err := io.ReadAll(f)
	if err != nil {
		auditReportLog.Printf("Failed to read %s: %v", agentStdioPath, err)
		return ""
	}

	lines := strings.Split(stripGHALogTimestamps(string(tail)), "\n")
	nonEmpty, errorLike := classifyAgentStdioLines(lines)

	if len(errorLike) > 0 {
		const maxErrorLines = 5
		start := max(0, len(errorLike)-maxErrorLines)
		return stringutil.Truncate(strings.Join(errorLike[start:], "\n"), maxMessageLen)
	}

	if len(nonEmpty) == 0 {
		return ""
	}

	const maxTailLines = 10
	start := max(0, len(nonEmpty)-maxTailLines)
	return stringutil.Truncate(strings.Join(nonEmpty[start:], "\n"), maxMessageLen)
}

func classifyAgentStdioLines(lines []string) ([]string, []string) {
	nonEmpty := make([]string, 0, len(lines))
	errorLike := make([]string, 0, len(lines))

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		nonEmpty = append(nonEmpty, trimmed)

		lower := strings.ToLower(trimmed)
		if strings.Contains(lower, "##[error]") ||
			strings.HasPrefix(lower, "error:") ||
			strings.HasPrefix(lower, "fatal:") ||
			strings.HasPrefix(lower, "panic:") {
			errorLike = append(errorLike, trimmed)
		}
	}

	return nonEmpty, errorLike
}

// parseStepFilename extracts the step number and name from a GitHub Actions step log
// filename in the format "{step_num}_{step_name}.txt" (e.g. "12_Validate lockdown mode.txt").
// Returns (0, filename) if the filename does not match the expected format.
func parseStepFilename(filename string) (int, string) {
	base := strings.TrimSuffix(filename, ".txt")
	idx := strings.IndexByte(base, '_')
	if idx <= 0 {
		return 0, base
	}
	num, err := strconv.Atoi(base[:idx])
	if err != nil {
		return 0, base
	}
	return num, base[idx+1:]
}

// stripGHALogTimestamps removes GitHub Actions timestamp prefixes from each line of a log.
// GitHub Actions step log files prefix each line with an RFC3339 timestamp followed by a space,
// e.g. "2024-01-01T10:00:00.1234567Z message here". This function strips those prefixes so the
// returned string contains only the actual log content.
func stripGHALogTimestamps(content string) string {
	lines := strings.Split(content, "\n")
	stripped := make([]string, 0, len(lines))
	for _, line := range lines {
		// GHA timestamp format: YYYY-MM-DDTHH:MM:SS[.sss...]Z<space>
		// The 'T' separator is always at position 10. Search for the terminating 'Z' after 'T'
		// in a generous window (positions 11-35) to handle any fractional seconds length.
		if len(line) > 19 && line[4] == '-' && line[7] == '-' && line[10] == 'T' {
			// Find the Z that ends the timestamp within a reasonable range
			searchBound := min(35, len(line))
			if zIdx := strings.IndexByte(line[11:searchBound], 'Z'); zIdx >= 0 {
				zPos := 11 + zIdx
				if zPos+1 <= len(line) {
					line = line[zPos+1:]
					// Skip leading space after the timestamp
					if line != "" && line[0] == ' ' {
						line = line[1:]
					}
				}
			}
		}
		stripped = append(stripped, line)
	}
	return strings.Join(stripped, "\n")
}
