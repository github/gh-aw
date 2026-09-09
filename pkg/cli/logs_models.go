package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/types"
	"github.com/github/gh-aw/pkg/workflow"
)

var logsModelsLog = logger.New("cli:logs_models")

const (
	// defaultAgentStdioLogPath is the default log file path for agent stdout/stderr
	defaultAgentStdioLogPath = "/tmp/gh-aw/agent-stdio.log"
	// runSummaryFileName is the name of the summary file created in each run folder
	runSummaryFileName = "run_summary.json"
	// jobsAPIResponseFileName is the raw GitHub Actions jobs API response cached for each run
	jobsAPIResponseFileName = "jobs.json"
	// runAPIResponseFileName is the raw GitHub Actions run API response cached for each run
	runAPIResponseFileName = "run.json"
	// auditFileName is the structured audit report cached for each run
	auditFileName = "audit.json"
	// defaultLogsOutputDir is the default directory for downloaded workflow logs
	defaultLogsOutputDir = ".github/aw/logs"
)

// Constants for the iterative algorithm
const (
	// MaxIterations limits how many batches we fetch to prevent infinite loops
	MaxIterations = 20
	// BatchSize is the number of runs to fetch in each iteration
	BatchSize = 100
	// BatchSizeForAllWorkflows is the batch size when searching across all agentic workflows.
	// We cap this at 100 (the GitHub API max per_page) so each batch requires only a single
	// API call.  Using 250 previously caused three API round-trips per batch, making the
	// unfiltered list slow enough to exceed the MCP gateway's default 60-second tool timeout.
	BatchSizeForAllWorkflows = 100
	// MaxConcurrentDownloads limits the number of parallel artifact downloads
	MaxConcurrentDownloads = 10
	// APICallCooldown is the minimum pause between successive batch-fetch iterations to
	// avoid hitting the GitHub API rate limit when no explicit usage ceiling is configured.
	APICallCooldown = 500 * time.Millisecond
	// RateLimitThreshold is the minimum number of GitHub API core requests that must
	// remain before the rate-limit helper considers the budget healthy.  When the
	// remaining count falls at or below this value the helper sleeps until the reset
	// window so subsequent iterations are not rejected with a 403/429.
	RateLimitThreshold = 10
	// RateLimitWarningThresholdPercent is the core quota percentage at or below
	// which non-JSON logs output warns that the API limit is approaching.
	RateLimitWarningThresholdPercent = 20
	// rateLimitResetBuffer is the extra duration added on top of the computed wait time
	// after a rate-limit reset to avoid resuming right on the boundary.

	// GitHubActionsRetentionDays is GitHub's default log-retention window for
	// GitHub Actions workflow runs.  Runs older than this threshold are unlikely
	// to have artifacts available, so the logs tool uses it to produce a helpful
	// "beyond retention period" message when no results are found.
	GitHubActionsRetentionDays = 90
	rateLimitResetBuffer       = 2 * time.Second
)

// WorkflowRun represents a GitHub Actions workflow run with metrics
type WorkflowRun struct {
	DatabaseID          int64     `json:"databaseId"`
	Number              int       `json:"number"`
	URL                 string    `json:"url"`
	Status              string    `json:"status"`
	Conclusion          string    `json:"conclusion"`
	WorkflowName        string    `json:"workflowName"`
	WorkflowPath        string    `json:"workflowPath"` // Workflow file path (e.g., .github/workflows/copilot-swe-agent.yml)
	CreatedAt           time.Time `json:"createdAt"`
	StartedAt           time.Time `json:"startedAt"`
	UpdatedAt           time.Time `json:"updatedAt"`
	Event               string    `json:"event"`
	HeadBranch          string    `json:"headBranch"`
	HeadSha             string    `json:"headSha"`
	DisplayTitle        string    `json:"displayTitle"`
	Attempt             int       `json:"attempt,omitempty"`
	Repository          string    `json:"repository,omitempty"`
	Actor               string    `json:"actor,omitempty"`
	Duration            time.Duration
	ActionMinutes       float64 // Billable Actions minutes estimated from wall-clock time
	TokenUsage          int
	Turns               int
	TurnsAvailable      bool // True when turn count was successfully read from artifact logs
	ErrorCount          int
	WarningCount        int
	MissingToolCount    int
	MissingDataCount    int
	NoopCount           int
	SafeItemsCount      int           `json:"safe_items_count,omitempty"` // Count of safe-output items actually written to GitHub
	EffectiveTokens     int           // Cost-normalized token count computed from per-model multipliers
	AvgTimeBetweenTurns time.Duration // Average time between consecutive LLM API calls (from per-turn timestamps when available)
	LogsPath            string
}

// LogMetrics represents extracted metrics from log files
// This is now an alias to the shared type in workflow package
type LogMetrics = workflow.LogMetrics

// ToolCallInfo represents statistics for a single tool invocation type
// This is an alias to the shared type in workflow package
type ToolCallInfo = workflow.ToolCallInfo

// ProcessedRun represents a workflow run with its associated analysis
type ProcessedRun struct {
	Run                     WorkflowRun
	AwContext               *AwContext
	TaskDomain              *TaskDomainInfo
	BehaviorFingerprint     *BehaviorFingerprint
	AgenticAssessments      []AgenticAssessment
	AccessAnalysis          *DomainAnalysis
	FirewallAnalysis        *FirewallAnalysis
	PolicyAnalysis          *PolicyAnalysis
	RedactedDomainsAnalysis *RedactedDomainsAnalysis
	MissingTools            []MissingToolReport
	MissingData             []MissingDataReport
	Noops                   []NoopReport
	MCPFailures             []MCPFailureReport
	SkillActivations        []SkillActivation
	MCPToolUsage            *MCPToolUsageData
	TokenUsage              *TokenUsageSummary
	WorkingSet              *WorkingSetMetrics
	GitHubRateLimitUsage    *GitHubRateLimitUsage
	JobDetails              []JobInfoWithDuration
}

// ReportProvenance holds the shared provenance fields common to all report record types.
type ReportProvenance struct {
	Timestamp      string `json:"timestamp"`
	WorkflowName   string `json:"workflow_name,omitempty"`   // Tracks which workflow reported this
	RunID          int64  `json:"run_id,omitempty"`          // Tracks which run reported this
	ExperimentName string `json:"experiment_name,omitempty"` // Assigned experiment name for this run (if present)
	Variant        string `json:"variant,omitempty"`         // Assigned variant value for ExperimentName (if present)
}

// MissingToolReport represents a missing tool reported by an agentic workflow
type MissingToolReport struct {
	Tool         string `json:"tool"`
	Reason       string `json:"reason"`
	Alternatives string `json:"alternatives,omitempty"`
	ReportProvenance
}

// NoopReport represents a noop message reported by an agentic workflow
type NoopReport struct {
	Message string `json:"message"`
	ReportProvenance
}

// MissingDataReport represents missing data reported by an agentic workflow
type MissingDataReport struct {
	DataType     string `json:"data_type"`
	Reason       string `json:"reason"`
	Context      string `json:"context,omitempty"`
	Alternatives string `json:"alternatives,omitempty"`
	ReportProvenance
}

// MCPFailureReport represents an MCP server failure detected in a workflow run
type MCPFailureReport struct {
	ServerName string `json:"server_name"`
	Status     string `json:"status"`
	ReportProvenance
}

// SkillActivation records a detected skill invocation from agent logs.
// Source indicates where the invocation was detected: "agent_output" for
// items emitted by the workflow via safe-output, or "log_parse" for
// patterns extracted from raw agent log files.
type SkillActivation struct {
	Name   string `json:"name"`
	Status string `json:"status"`           // "invoked"
	Source string `json:"source,omitempty"` // "agent_output" or "log_parse"
	ReportProvenance
}

// AggregatedSummaryBase holds the shared tail fields that appear byte-for-byte identically
// in MissingToolSummary and MissingDataSummary (and as a subset in MCPFailureSummary).
// Embedding this struct removes copy-paste drift risk across the aggregated-report types.
type AggregatedSummaryBase struct {
	Count              int      `json:"count" console:"header:Occurrences"`
	Workflows          []string `json:"workflows" console:"-"`                     // List of workflow names
	WorkflowsDisplay   string   `json:"-" console:"header:Workflows,maxlen:40"`    // Formatted display of workflows
	FirstReason        string   `json:"first_reason" console:"-"`                  // Reason from the first occurrence
	FirstReasonDisplay string   `json:"-" console:"header:First Reason,maxlen:50"` // Formatted display of first reason
	RunIDs             []int64  `json:"run_ids" console:"-"`                       // List of run IDs
}

// MCPServerStatsBase holds the per-server identity and volume fields shared by the MCP
// server health/stats report types (MCPServerStats, MCPServerHealthDetail and
// MCPServerCrossRunHealth). Those types previously spelled the same concepts four
// different ways (TotalCalls/ToolCalls/ToolCallCount and TotalErrors/ErrorCount);
// embedding this struct standardizes the Go field names and removes copy-paste drift
// risk, following the same approach as AggregatedSummaryBase. Types whose serialized
// schema differs from these tags keep it via a MarshalJSON override.
type MCPServerStatsBase struct {
	ServerName    string `json:"server_name" console:"header:Server"`
	ToolCallCount int    `json:"tool_call_count" console:"header:Tool Calls"`
	// ErrorCount keeps the omitempty tags of MCPServerStats, the only embedder that
	// serializes/renders these tags directly; the other embedders override MarshalJSON.
	ErrorCount int `json:"error_count,omitempty" console:"header:Errors,omitempty"`
}

// MissingToolSummary aggregates missing tool reports across runs
type MissingToolSummary struct {
	Tool string `json:"tool" console:"header:Tool"`
	AggregatedSummaryBase
}

// MCPFailureSummary aggregates MCP server failure reports across runs
type MCPFailureSummary struct {
	ServerName            string `json:"server_name" console:"header:Server"`
	AggregatedSummaryBase `console:"-"`
}

// MarshalJSON preserves the MCP failure JSON schema while sharing aggregation state with
// the other summary types.
func (s MCPFailureSummary) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		ServerName string   `json:"server_name"`
		Count      int      `json:"count"`
		Workflows  []string `json:"workflows"`
		RunIDs     []int64  `json:"run_ids"`
	}{
		ServerName: s.ServerName,
		Count:      s.Count,
		Workflows:  s.Workflows,
		RunIDs:     s.RunIDs,
	})
}

// MissingDataSummary aggregates missing data reports across runs
type MissingDataSummary struct {
	DataType string `json:"data_type" console:"header:Data Type"`
	AggregatedSummaryBase
}

// MCPToolUsageSummary aggregates MCP tool usage across all runs
type MCPToolUsageSummary struct {
	Summary        []MCPToolSummary        `json:"summary" console:"title:Tool Statistics"`             // Aggregated statistics per tool
	Servers        []MCPServerStats        `json:"servers,omitempty" console:"title:Server Statistics"` // Server-level statistics
	ToolCalls      []MCPToolCall           `json:"tool_calls" console:"-"`                              // Individual tool call records (excluded from console)
	FilteredEvents []DifcFilteredEvent     `json:"filtered_events,omitempty" console:"-"`               // DIFC filtered events (excluded from console display)
	Integrity      *IntegrityFilterSummary `json:"integrity,omitempty" console:"-"`                     // Aggregate DIFC integrity-filter activity
}

// ErrNoArtifacts indicates that a workflow run has no artifacts
var ErrNoArtifacts = errors.New("no artifacts found for this run")

// RunAnalysis holds the run metadata, metrics and analysis reports extracted from a
// workflow run's logs and artifacts. It is embedded by both RunSummary and DownloadResult
// so both carriers share a single definition of the analysis surface.
type RunAnalysis struct {
	Run                     WorkflowRun              `json:"run"`                               // Full workflow run metadata
	Metrics                 LogMetrics               `json:"metrics"`                           // Extracted log metrics
	AwContext               *AwContext               `json:"context,omitempty"`                 // aw_context data from aw_info.json
	TaskDomain              *TaskDomainInfo          `json:"task_domain,omitempty"`             // Inferred workflow task domain
	BehaviorFingerprint     *BehaviorFingerprint     `json:"behavior_fingerprint,omitempty"`    // Compact execution profile
	AgenticAssessments      []AgenticAssessment      `json:"agentic_assessments,omitempty"`     // Derived agentic judgments
	AccessAnalysis          *DomainAnalysis          `json:"access_analysis"`                   // Network access analysis
	FirewallAnalysis        *FirewallAnalysis        `json:"firewall_analysis"`                 // Firewall log analysis
	RedactedDomainsAnalysis *RedactedDomainsAnalysis `json:"redacted_domains_analysis"`         // Redacted URL domains analysis
	MissingTools            []MissingToolReport      `json:"missing_tools"`                     // Missing tool reports
	MissingData             []MissingDataReport      `json:"missing_data"`                      // Missing data reports
	Noops                   []NoopReport             `json:"noops"`                             // Noop messages
	MCPFailures             []MCPFailureReport       `json:"mcp_failures"`                      // MCP server failures
	SkillActivations        []SkillActivation        `json:"skill_activations,omitempty"`       // Detected skill invocations
	MCPToolUsage            *MCPToolUsageData        `json:"mcp_tool_usage,omitempty"`          // MCP tool usage data
	TokenUsage              *TokenUsageSummary       `json:"token_usage_summary,omitempty"`     // Token usage from firewall proxy
	WorkingSet              *WorkingSetMetrics       `json:"working_set,omitempty"`             // Working-set rebuild metric from usage summary
	GitHubRateLimitUsage    *GitHubRateLimitUsage    `json:"github_rate_limit_usage,omitempty"` // GitHub API quota consumption
	JobDetails              []JobInfoWithDuration    `json:"job_details"`                       // Job execution details
}

// RunSummary represents a complete summary of a workflow run's artifacts and metrics.
// This file is written to each run folder as "run_summary.json" to cache processing results
// and avoid re-downloading and re-processing already analyzed runs.
//
// Key features:
// - Acts as a marker that a run has been fully processed
// - Stores all extracted metrics and analysis results
// - Includes CLI version for cache invalidation when the tool is updated
// - Enables fast reloading of run data without re-parsing logs
//
// Cache invalidation:
// - If the CLI version in the summary doesn't match the current version, the run is reprocessed
// - This ensures that bug fixes and improvements in log parsing are automatically applied
type RunSummary struct {
	CLIVersion  string    `json:"cli_version"`  // CLI version used to process this run
	RunID       int64     `json:"run_id"`       // Workflow run database ID
	ProcessedAt time.Time `json:"processed_at"` // When this summary was created
	RunAnalysis
	PolicyAnalysis *PolicyAnalysis `json:"policy_analysis,omitempty"` // Firewall policy rule attribution
	ArtifactsList  []string        `json:"artifacts_list"`            // List of downloaded artifact files
}

// DownloadResult represents the result of downloading and processing a workflow run
type DownloadResult struct {
	RunAnalysis
	Error           error
	Skipped         bool
	Cached          bool // True if loaded from cached summary
	LogsPath        string
	storageReserved bool
}

// JobInfo represents basic information about a workflow job
type JobInfo struct {
	ID              int64     `json:"id,omitempty"`
	RunID           int64     `json:"run_id,omitempty"`
	RunURL          string    `json:"run_url,omitempty"`
	RunAttempt      int       `json:"run_attempt,omitempty"`
	NodeID          string    `json:"node_id,omitempty"`
	HeadSha         string    `json:"head_sha,omitempty"`
	URL             string    `json:"url,omitempty"`
	HTMLURL         string    `json:"html_url,omitempty"`
	Status          string    `json:"status"`
	Conclusion      string    `json:"conclusion"`
	CreatedAt       time.Time `json:"created_at,omitzero"`
	StartedAt       time.Time `json:"started_at,omitzero"`
	CompletedAt     time.Time `json:"completed_at,omitzero"`
	Name            string    `json:"name"`
	Steps           []JobStep `json:"steps,omitempty"`
	CheckRunURL     string    `json:"check_run_url,omitempty"`
	Labels          []string  `json:"labels,omitempty"`
	RunnerID        int64     `json:"runner_id,omitempty"`
	RunnerName      string    `json:"runner_name,omitempty"`
	RunnerGroupID   int64     `json:"runner_group_id,omitempty"`
	RunnerGroupName string    `json:"runner_group_name,omitempty"`
	WorkflowName    string    `json:"workflow_name,omitempty"`
	HeadBranch      string    `json:"head_branch,omitempty"`
}

// JobStep represents basic information about an individual workflow job step.
type JobStep struct {
	Name        string    `json:"name"`
	Status      string    `json:"status,omitempty"`
	Conclusion  string    `json:"conclusion,omitempty"`
	Number      int       `json:"number,omitempty"`
	StartedAt   time.Time `json:"started_at,omitzero"`
	CompletedAt time.Time `json:"completed_at,omitzero"`
}

// JobInfoWithDuration extends JobInfo with calculated duration
type JobInfoWithDuration struct {
	JobInfo
	Duration time.Duration
}

// AwInfoSteps represents the steps information in aw_info.json files
type AwInfoSteps struct {
	Firewall string `json:"firewall,omitempty"` // Firewall type (e.g., "squid") or empty if no firewall
}

// NumericID is an integer identifier that accepts either a JSON number or its string representation.
type NumericID int64

// UnmarshalJSON normalizes numeric and string JSON representations to an integer.
// JSON null and empty strings are treated as unset (zero) so that historical
// aw_info.json files, which stored these fields loosely typed, keep loading.
func (n *NumericID) UnmarshalJSON(data []byte) error {
	switch string(bytes.TrimSpace(data)) {
	case "null", `""`:
		*n = 0
		return nil
	}

	// json.Number accepts both a JSON number (123) and its string form ("123").
	var number json.Number
	if err := json.Unmarshal(data, &number); err != nil {
		return fmt.Errorf("numeric ID should be a JSON number or numeric string, for example 123 or \"123\": %w", err)
	}

	value, err := number.Int64()
	if err != nil {
		return fmt.Errorf("numeric ID %q should be a whole number within the int64 range (up to 9223372036854775807): %w", number, err)
	}

	*n = NumericID(value)
	return nil
}

// AwContext represents the caller-workflow identity injected by dispatch_workflow.cjs
// into the aw_context input, and stored under the "context" key in aw_info.json.
// All values are strings (no nested objects) as validated by generate_aw_info.cjs.
type AwContext struct {
	Repo           string `json:"repo"`                       // "owner/repo" of the calling workflow
	RunID          string `json:"run_id"`                     // GitHub Actions run ID of the calling workflow
	WorkflowID     string `json:"workflow_id"`                // Full workflow ref, e.g. "owner/repo/.github/workflows/foo.yml@refs/heads/main"
	WorkflowCallID string `json:"workflow_call_id,omitempty"` // Unique call attempt ID (run_id + run_attempt)
	Time           string `json:"time,omitempty"`             // ISO 8601 timestamp of the dispatch
	Actor          string `json:"actor,omitempty"`            // GitHub actor that triggered the calling workflow
	EventType      string `json:"event_type,omitempty"`       // GitHub event name of the calling workflow
	ItemType       string `json:"item_type,omitempty"`        // Kind of triggering item: "issue", "pull_request", "discussion", "check_run", "check_suite", or ""
	ItemNumber     string `json:"item_number,omitempty"`      // Number (issue/PR/discussion) or database id (check_run/check_suite) of the triggering item
	CommentID      string `json:"comment_id,omitempty"`       // ID of the triggering comment or review; empty when not a comment/review event
}

// AwInfo represents the structure of aw_info.json files
type AwInfo struct {
	EngineID        string              `json:"engine_id"`
	EngineName      string              `json:"engine_name"`
	Model           string              `json:"model"`
	Version         string              `json:"version"`
	CLIVersion      string              `json:"cli_version,omitempty"` // gh-aw CLI version
	WorkflowName    string              `json:"workflow_name"`
	Staged          bool                `json:"staged"`
	AwfVersion      string              `json:"awf_version,omitempty"`      // AWF firewall version (new name)
	FirewallVersion string              `json:"firewall_version,omitempty"` // AWF firewall version (old name, for backward compatibility)
	AgentRuntime    string              `json:"agent_runtime,omitempty"`    // sandbox.agent.runtime value (e.g., "gvisor", "docker-sbx", "cloud-hypervisor"); empty when unset
	CacheMemory     bool                `json:"cache_memory"`               // true when the workflow declares tools.cache-memory
	Steps           AwInfoSteps         `json:"steps,omitzero"`             // Steps metadata
	CreatedAt       string              `json:"created_at"`
	Context         *AwContext          `json:"context,omitempty"`       // aw_context data passed via workflow_dispatch inputs
	TokenWeights    *types.TokenWeights `json:"token_weights,omitempty"` // Historical/custom model cost data stored in aw_info.json
	// Additional fields that might be present
	RunID      NumericID `json:"run_id,omitempty"`
	RunNumber  NumericID `json:"run_number,omitempty"`
	RunAttempt string    `json:"run_attempt,omitempty"`
	Repository string    `json:"repository,omitempty"`
	Ref        string    `json:"ref,omitempty"`
	SHA        string    `json:"sha,omitempty"`
	Actor      string    `json:"actor,omitempty"`
	EventName  string    `json:"event_name,omitempty"`
	TargetRepo string    `json:"target_repo,omitempty"`
}

// GetFirewallVersion returns the AWF firewall version, preferring the new field name
// (awf_version) but falling back to the old field name (firewall_version) for
// backward compatibility with older aw_info.json files.
func (a *AwInfo) GetFirewallVersion() string {
	if a.AwfVersion != "" {
		return a.AwfVersion
	}
	return a.FirewallVersion
}

// isFailureConclusion returns true if the conclusion represents a failure state
// (timed_out, failure, or cancelled) that should be counted as an error
func isFailureConclusion(conclusion string) bool {
	isFailure := conclusion == "timed_out" || conclusion == "failure" || conclusion == "cancelled"
	if logsModelsLog.Enabled() {
		logsModelsLog.Printf("Checking failure conclusion: conclusion=%s, is_failure=%t", conclusion, isFailure)
	}
	return isFailure
}

// isNonDispatchedConclusion returns true when a workflow run conclusion means the
// run never dispatched any agentic job, so it carries no agentic data and must not
// be counted as an executed run in success-rate, duration or token metrics.
//
// "skipped" runs had their activation condition evaluate to false (for example a
// command workflow triggered by a comment that does not contain the command).
// "action_required" runs were created by GitHub but held for manual approval and
// never started any job (for example comment events authored by a bot actor whose
// workflow runs require approval).
func isNonDispatchedConclusion(conclusion string) bool {
	return conclusion == "skipped" || conclusion == "action_required"
}

// isDriverExitFailure returns true when a failed run shows no agent turns, which
// indicates the CLI wrapper or a pre/post-agent infrastructure step exited non-zero
// before the agent had a chance to run.  Runs with Turns > 0 are classified as
// agent-logic failures instead because the agent did execute.
// TurnsAvailable must be true to confirm the zero is real rather than missing data.
func isDriverExitFailure(run WorkflowRun) bool {
	return isFailureConclusion(run.Conclusion) && run.TurnsAvailable && run.Turns == 0
}
