package cli

import (
	"errors"
	"time"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/workflow"
)

var logsModelsLog = logger.New("cli:logs_models")

const (
	// defaultAgentStdioLogPath is the default log file path for agent stdout/stderr
	defaultAgentStdioLogPath = "/tmp/gh-aw/agent-stdio.log"
	// runSummaryFileName is the name of the summary file created in each run folder
	runSummaryFileName = "run_summary.json"
	// defaultLogsOutputDir is the default directory for downloaded workflow logs
	defaultLogsOutputDir = ".github/aw/logs"
)

// Constants for the iterative algorithm
const (
	// MaxIterations limits how many batches we fetch to prevent infinite loops
	MaxIterations = 20
	// BatchSize is the number of runs to fetch in each iteration
	BatchSize = 100
	// BatchSizeForAllWorkflows is the larger batch size when searching for agentic workflows
	// There can be a really large number of workflow runs in a repository, so
	// we are generous in the batch size when used without qualification.
	BatchSizeForAllWorkflows = 250
	// MaxConcurrentDownloads limits the number of parallel artifact downloads
	MaxConcurrentDownloads = 10
)

// WorkflowRun represents a GitHub Actions workflow run with metrics
type WorkflowRun struct {
	DatabaseID       int64     `json:"databaseId"`
	Number           int       `json:"number"`
	URL              string    `json:"url"`
	Status           string    `json:"status"`
	Conclusion       string    `json:"conclusion"`
	WorkflowName     string    `json:"workflowName"`
	WorkflowPath     string    `json:"workflowPath"` // Workflow file path (e.g., .github/workflows/copilot-swe-agent.yml)
	CreatedAt        time.Time `json:"createdAt"`
	StartedAt        time.Time `json:"startedAt"`
	UpdatedAt        time.Time `json:"updatedAt"`
	Event            string    `json:"event"`
	HeadBranch       string    `json:"headBranch"`
	HeadSha          string    `json:"headSha"`
	DisplayTitle     string    `json:"displayTitle"`
	Duration         time.Duration
	TokenUsage       int
	EstimatedCost    float64
	Turns            int
	ErrorCount       int
	WarningCount     int
	MissingToolCount int
	MissingDataCount int
	NoopCount        int
	LogsPath         string
}

// LogMetrics represents extracted metrics from log files
// This is now an alias to the shared type in workflow package
type LogMetrics = workflow.LogMetrics

// ProcessedRun represents a workflow run with its associated analysis
type ProcessedRun struct {
	Run                     WorkflowRun
	AccessAnalysis          *DomainAnalysis
	FirewallAnalysis        *FirewallAnalysis
	RedactedDomainsAnalysis *RedactedDomainsAnalysis
	MissingTools            []MissingToolReport
	MissingData             []MissingDataReport
	Noops                   []NoopReport
	MCPFailures             []MCPFailureReport
	MCPToolUsage            *MCPToolUsageData
	JobDetails              []JobInfoWithDuration
}

// MissingToolReport represents a missing tool reported by an agentic workflow
type MissingToolReport struct {
	Tool         string `json:"tool"`
	Reason       string `json:"reason"`
	Alternatives string `json:"alternatives,omitempty"`
	Timestamp    string `json:"timestamp"`
	WorkflowName string `json:"workflow_name,omitempty"` // Added for tracking which workflow reported this
	RunID        int64  `json:"run_id,omitempty"`        // Added for tracking which run reported this
}

// NoopReport represents a noop message reported by an agentic workflow
type NoopReport struct {
	Message      string `json:"message"`
	Timestamp    string `json:"timestamp,omitempty"`
	WorkflowName string `json:"workflow_name,omitempty"` // Added for tracking which workflow reported this
	RunID        int64  `json:"run_id,omitempty"`        // Added for tracking which run reported this
}

// MissingDataReport represents missing data reported by an agentic workflow
type MissingDataReport struct {
	DataType     string `json:"data_type"`
	Reason       string `json:"reason"`
	Context      string `json:"context,omitempty"`
	Alternatives string `json:"alternatives,omitempty"`
	Timestamp    string `json:"timestamp"`
	WorkflowName string `json:"workflow_name,omitempty"` // Added for tracking which workflow reported this
	RunID        int64  `json:"run_id,omitempty"`        // Added for tracking which run reported this
}

// MCPFailureReport represents an MCP server failure detected in a workflow run
type MCPFailureReport struct {
	ServerName   string `json:"server_name"`
	Status       string `json:"status"`
	Timestamp    string `json:"timestamp,omitempty"`
	WorkflowName string `json:"workflow_name,omitempty"`
	RunID        int64  `json:"run_id,omitempty"`
}

// MissingToolSummary aggregates missing tool reports across runs
type MissingToolSummary struct {
	Tool               string   `json:"tool" console:"header:Tool"`
	Count              int      `json:"count" console:"header:Occurrences"`
	Workflows          []string `json:"workflows" console:"-"`                     // List of workflow names that reported this tool
	WorkflowsDisplay   string   `json:"-" console:"header:Workflows,maxlen:40"`    // Formatted display of workflows
	FirstReason        string   `json:"first_reason" console:"-"`                  // Reason from the first occurrence
	FirstReasonDisplay string   `json:"-" console:"header:First Reason,maxlen:50"` // Formatted display of first reason
	RunIDs             []int64  `json:"run_ids" console:"-"`                       // List of run IDs where this tool was reported
}

// MCPFailureSummary aggregates MCP server failure reports across runs
type MCPFailureSummary struct {
	ServerName       string   `json:"server_name" console:"header:Server"`
	Count            int      `json:"count" console:"header:Failures"`
	Workflows        []string `json:"workflows" console:"-"`                  // List of workflow names that had this server fail
	WorkflowsDisplay string   `json:"-" console:"header:Workflows,maxlen:60"` // Formatted display of workflows
	RunIDs           []int64  `json:"run_ids" console:"-"`                    // List of run IDs where this server failed
}

// MissingDataSummary aggregates missing data reports across runs
type MissingDataSummary struct {
	DataType           string   `json:"data_type" console:"header:Data Type"`
	Count              int      `json:"count" console:"header:Occurrences"`
	Workflows          []string `json:"workflows" console:"-"`                     // List of workflow names that reported this data
	WorkflowsDisplay   string   `json:"-" console:"header:Workflows,maxlen:40"`    // Formatted display of workflows
	FirstReason        string   `json:"first_reason" console:"-"`                  // Reason from the first occurrence
	FirstReasonDisplay string   `json:"-" console:"header:First Reason,maxlen:50"` // Formatted display of first reason
	RunIDs             []int64  `json:"run_ids" console:"-"`                       // List of run IDs where this data was reported
}

// MCPToolUsageSummary aggregates MCP tool usage across all runs
type MCPToolUsageSummary struct {
	Summary   []MCPToolSummary `json:"summary" console:"title:Tool Statistics"`             // Aggregated statistics per tool
	Servers   []MCPServerStats `json:"servers,omitempty" console:"title:Server Statistics"` // Server-level statistics
	ToolCalls []MCPToolCall    `json:"tool_calls" console:"-"`                              // Individual tool call records (excluded from console)
}

// ErrNoArtifacts indicates that a workflow run has no artifacts
var ErrNoArtifacts = errors.New("no artifacts found for this run")

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
	CLIVersion              string                   `json:"cli_version"`               // CLI version used to process this run
	RunID                   int64                    `json:"run_id"`                    // Workflow run database ID
	ProcessedAt             time.Time                `json:"processed_at"`              // When this summary was created
	Run                     WorkflowRun              `json:"run"`                       // Full workflow run metadata
	Metrics                 LogMetrics               `json:"metrics"`                   // Extracted log metrics
	AccessAnalysis          *DomainAnalysis          `json:"access_analysis"`           // Network access analysis
	FirewallAnalysis        *FirewallAnalysis        `json:"firewall_analysis"`         // Firewall log analysis
	RedactedDomainsAnalysis *RedactedDomainsAnalysis `json:"redacted_domains_analysis"` // Redacted URL domains analysis
	MissingTools            []MissingToolReport      `json:"missing_tools"`             // Missing tool reports
	MissingData             []MissingDataReport      `json:"missing_data"`              // Missing data reports
	Noops                   []NoopReport             `json:"noops"`                     // Noop messages
	MCPFailures             []MCPFailureReport       `json:"mcp_failures"`              // MCP server failures
	MCPToolUsage            *MCPToolUsageData        `json:"mcp_tool_usage,omitempty"`  // MCP tool usage data
	ArtifactsList           []string                 `json:"artifacts_list"`            // List of downloaded artifact files
	JobDetails              []JobInfoWithDuration    `json:"job_details"`               // Job execution details
}

// DownloadResult represents the result of downloading and processing a workflow run
type DownloadResult struct {
	Run                     WorkflowRun
	Metrics                 LogMetrics
	AccessAnalysis          *DomainAnalysis
	FirewallAnalysis        *FirewallAnalysis
	RedactedDomainsAnalysis *RedactedDomainsAnalysis
	MissingTools            []MissingToolReport
	MissingData             []MissingDataReport
	Noops                   []NoopReport
	MCPFailures             []MCPFailureReport
	MCPToolUsage            *MCPToolUsageData
	JobDetails              []JobInfoWithDuration
	Error                   error
	Skipped                 bool
	Cached                  bool // True if loaded from cached summary
	LogsPath                string
}

// JobInfo represents basic information about a workflow job
type JobInfo struct {
	Name        string    `json:"name"`
	Status      string    `json:"status"`
	Conclusion  string    `json:"conclusion"`
	StartedAt   time.Time `json:"started_at,omitempty"`
	CompletedAt time.Time `json:"completed_at,omitempty"`
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

// AwInfo represents the structure of aw_info.json files
type AwInfo struct {
	EngineID        string      `json:"engine_id"`
	EngineName      string      `json:"engine_name"`
	Model           string      `json:"model"`
	Version         string      `json:"version"`
	CLIVersion      string      `json:"cli_version,omitempty"` // gh-aw CLI version
	WorkflowName    string      `json:"workflow_name"`
	Staged          bool        `json:"staged"`
	AwfVersion      string      `json:"awf_version,omitempty"`      // AWF firewall version (new name)
	FirewallVersion string      `json:"firewall_version,omitempty"` // AWF firewall version (old name, for backward compatibility)
	Steps           AwInfoSteps `json:"steps,omitempty"`            // Steps metadata
	CreatedAt       string      `json:"created_at"`
	// Additional fields that might be present
	RunID      any    `json:"run_id,omitempty"`
	RunNumber  any    `json:"run_number,omitempty"`
	Repository string `json:"repository,omitempty"`
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
