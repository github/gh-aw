package cli

import (
	"time"

	"github.com/github/gh-aw/pkg/scanfindings"
)

// AuditData represents the complete structured audit data for a workflow run
type AuditData struct {
	SchemaVersion              int                      `json:"schema_version"`
	CacheSource                auditCacheSource         `json:"cache_source,omitempty"`
	Overview                   OverviewData             `json:"overview"`
	Comparison                 *AuditComparisonData     `json:"comparison,omitempty"`
	TaskDomain                 *TaskDomainInfo          `json:"task_domain,omitempty"`
	BehaviorFingerprint        *BehaviorFingerprint     `json:"behavior_fingerprint,omitempty"`
	AgenticAssessments         []AgenticAssessment      `json:"agentic_assessments,omitempty"`
	Metrics                    MetricsData              `json:"metrics"`
	KeyFindings                []AuditFinding           `json:"key_findings,omitempty"`
	Recommendations            []Recommendation         `json:"recommendations,omitempty"`
	ObservabilityInsights      []ObservabilityInsight   `json:"observability_insights,omitempty"`
	PerformanceMetrics         *PerformanceMetrics      `json:"performance_metrics,omitempty"`
	EngineConfig               *AuditEngineConfig       `json:"engine_config,omitempty"`
	PromptAnalysis             *PromptAnalysis          `json:"prompt_analysis,omitempty"`
	SessionAnalysis            *SessionAnalysis         `json:"session_analysis,omitempty"`
	SafeOutputSummary          *SafeOutputSummary       `json:"safe_output_summary,omitempty"`
	MCPServerHealth            *MCPServerHealth         `json:"mcp_server_health,omitempty"`
	Jobs                       []JobData                `json:"jobs,omitempty"`
	DownloadedFiles            []FileInfo               `json:"downloaded_files"`
	MissingTools               []MissingToolReport      `json:"missing_tools,omitempty"`
	MissingData                []MissingDataReport      `json:"missing_data,omitempty"`
	Noops                      []NoopReport             `json:"noops,omitempty"`
	MCPFailures                []MCPFailureReport       `json:"mcp_failures,omitempty"`
	SkillActivations           []SkillActivation        `json:"skill_activations,omitempty"`
	GatewaySteeringEvents      []GatewaySteeringEvent   `json:"gateway_steering_events,omitempty"`
	SteeringNotices            []SteeringNotice         `json:"steering_notices,omitempty"`
	SteeringDeliveries         []SteeringDelivery       `json:"steering_deliveries,omitempty"`
	SteeringNoticesUnavailable bool                     `json:"steering_notices_unavailable,omitempty"`
	FirewallTokenUsage         *TokenUsageSummary       `json:"firewall_token_usage,omitempty"`
	DetectionModelUsage        []DetectionModelUsage    `json:"detection_model_usage,omitempty"`
	ModelRouting               *ModelRoutingSummary     `json:"model_routing,omitempty"`
	ModelEndpointMismatches    []ModelEndpointMismatch  `json:"model_endpoint_mismatches,omitempty"`
	GitHubRateLimitUsage       *GitHubRateLimitUsage    `json:"github_rate_limit_usage,omitempty"`
	FirewallAnalysis           *FirewallAnalysis        `json:"firewall_analysis,omitempty"`
	PolicyAnalysis             *PolicyAnalysis          `json:"policy_analysis,omitempty"`
	RedactedDomainsAnalysis    *RedactedDomainsAnalysis `json:"redacted_domains_analysis,omitempty"`
	Errors                     []ValidationIssue        `json:"errors,omitempty"`
	Warnings                   []ValidationIssue        `json:"warnings,omitempty"`
	ToolUsage                  []ToolUsageInfo          `json:"tool_usage,omitempty"`
	MCPToolUsage               *MCPToolUsageData        `json:"mcp_tool_usage,omitempty"`
	CreatedItems               []CreatedItemReport      `json:"created_items,omitempty"`
	Outcomes                   []OutcomeReport          `json:"outcomes,omitempty"`
	OutcomeSummary             *OutcomeSummary          `json:"outcome_summary,omitempty"`
	Experiments                *ExperimentData          `json:"experiments,omitempty"`
	Graders                    *GradersData             `json:"graders,omitempty"`
	Evals                      *EvalsSummary            `json:"evals,omitempty"`
	Ledger                     *LedgerActivity          `json:"ledger,omitempty"`
	WorkQueue                  *WorkQueueReport         `json:"work_queue,omitempty"`
	Friction                   *FrictionCostSummary     `json:"friction,omitempty"`
	EvidenceSearch             *AuditEvidenceSearch     `json:"evidence_search,omitempty"`
	ModelCatalog               *AuditModelCatalog       `json:"model_catalog,omitempty"`
	Retries                    *AuditRetrySummary       `json:"retries,omitempty"`
	DurableSession             *DurableSessionReport    `json:"durable_session,omitempty"`
}

// AuditFinding represents a key insight discovered during audit
type AuditFinding struct {
	Code        AuditFindingCode           `json:"code"`             // Stable machine-readable identifier
	Category    string                     `json:"category"`         // e.g., "error", "performance", "cost", "tooling"
	Severity    scanfindings.SeverityLevel `json:"severity"`         // shared severity vocabulary
	Title       string                     `json:"title"`            // Brief title
	Description string                     `json:"description"`      // Detailed description
	Impact      string                     `json:"impact,omitempty"` // What impact this has
	PlaybookURL string                     `json:"playbook_url,omitempty"`
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
	TokenUsage           int                    `json:"token_usage,omitempty" console:"header:Token Usage,format:number,omitempty"`
	AIC                  float64                `json:"aic,omitempty"`
	AmbientContext       *AmbientContextMetrics `json:"ambient_context,omitempty" console:"title:Ambient Context,omitempty"`
	WorkingSet           *WorkingSetMetrics     `json:"working_set,omitempty" console:"-"`
	Friction             *FrictionCostSummary   `json:"-" console:"-"`
	ActionMinutes        float64                `json:"action_minutes,omitempty" console:"header:Action Minutes,omitempty"`
	Turns                int                    `json:"turns,omitempty" console:"header:Turns,omitempty"`
	ErrorCount           int                    `json:"error_count" console:"header:Errors"`
	SubagentFailureCount int                    `json:"subagent_failure_count,omitempty" console:"header:Sub-agent Failures,omitempty"`
	WarningCount         int                    `json:"warning_count" console:"header:Warnings"`
}

// JobData is the audit report rendering of a job.
// It is derived from, and intentionally much smaller than, the raw API mirror JobInfo.
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
	Type            string         `json:"type" console:"header:Type"`
	URL             string         `json:"url,omitempty" console:"header:URL,omitempty"`
	Number          int            `json:"number,omitempty" console:"header:Number,omitempty"`
	Repo            string         `json:"repo,omitempty" console:"header:Repo,omitempty"`
	Provider        string         `json:"provider,omitempty" console:"-"`
	ID              any            `json:"id,omitempty" console:"-"`
	Identifier      string         `json:"identifier,omitempty" console:"-"`
	Target          map[string]any `json:"target,omitempty" console:"-"`
	Labels          []LabelReport  `json:"labels,omitempty" console:"-"`
	LabelsAdded     []string       `json:"labelsAdded,omitempty" console:"-"`
	LabelsSuggested []string       `json:"labelsSuggested,omitempty" console:"-"`
	LabelsBefore    []string       `json:"labelsBefore,omitempty" console:"-"`
	TemporaryID     string         `json:"temporaryId,omitempty" console:"header:Temp ID,omitempty"`
	Metadata        map[string]any `json:"metadata,omitempty" console:"-"`
	BeforeState     map[string]any `json:"before_state,omitempty" console:"-"`
	AfterState      map[string]any `json:"after_state,omitempty" console:"-"`
	Timestamp       string         `json:"timestamp" console:"header:Timestamp"`
}

type LabelReport struct {
	Name       string `json:"name"`
	DatabaseID int64  `json:"database_id,omitempty"`
	NodeID     string `json:"node_id,omitempty"`
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
	AvgInputSize       int    `json:"avg_input_size,omitempty" console:"header:Avg Input,format:number"`
	AvgOutputSize      int    `json:"avg_output_size,omitempty" console:"header:Avg Output,format:number"`
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
	ToolCallID string `json:"tool_call_id,omitempty"`
	Timestamp  string `json:"timestamp"`
	ServerName string `json:"server_name"`
	ToolName   string `json:"tool_name"`
	Method     string `json:"method,omitempty"`
	InputSize  int    `json:"input_size"`
	OutputSize int    `json:"output_size"`
	Duration   string `json:"duration,omitempty"`
	Status     string `json:"status"`
	Error      string `json:"error,omitempty"`
}

// MCPServerStats contains server-level statistics
type MCPServerStats struct {
	MCPServerStatsBase
	// RequestCount is kept for backward-compatible report schemas that label per-server
	// request volume; in MCP usage summaries this currently mirrors ToolCallCount.
	RequestCount    int    `json:"request_count" console:"header:Requests"`
	TotalInputSize  int    `json:"total_input_size" console:"header:Total Input,format:number"`
	TotalOutputSize int    `json:"total_output_size" console:"header:Total Output,format:number"`
	AvgInputSize    int    `json:"avg_input_size,omitempty" console:"header:Avg Input,format:number"`
	AvgOutputSize   int    `json:"avg_output_size,omitempty" console:"header:Avg Output,format:number"`
	MaxInputSize    int    `json:"max_input_size,omitempty" console:"header:Max Input,format:number"`
	MaxOutputSize   int    `json:"max_output_size,omitempty" console:"header:Max Output,format:number"`
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
