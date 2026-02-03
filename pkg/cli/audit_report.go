package cli

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/timeutil"
	"github.com/github/gh-aw/pkg/workflow"
)

var auditReportLog = logger.New("cli:audit_report")

// AuditData represents the complete structured audit data for a workflow run
type AuditData struct {
	Overview                OverviewData             `json:"overview"`
	Metrics                 MetricsData              `json:"metrics"`
	KeyFindings             []Finding                `json:"key_findings,omitempty"`
	Recommendations         []Recommendation         `json:"recommendations,omitempty"`
	FailureAnalysis         *FailureAnalysis         `json:"failure_analysis,omitempty"`
	PerformanceMetrics      *PerformanceMetrics      `json:"performance_metrics,omitempty"`
	Jobs                    []JobData                `json:"jobs,omitempty"`
	DownloadedFiles         []FileInfo               `json:"downloaded_files"`
	MissingTools            []MissingToolReport      `json:"missing_tools,omitempty"`
	MissingData             []MissingDataReport      `json:"missing_data,omitempty"`
	Noops                   []NoopReport             `json:"noops,omitempty"`
	MCPFailures             []MCPFailureReport       `json:"mcp_failures,omitempty"`
	FirewallAnalysis        *FirewallAnalysis        `json:"firewall_analysis,omitempty"`
	RedactedDomainsAnalysis *RedactedDomainsAnalysis `json:"redacted_domains_analysis,omitempty"`
	Errors                  []ErrorInfo              `json:"errors,omitempty"`
	Warnings                []ErrorInfo              `json:"warnings,omitempty"`
	ToolUsage               []ToolUsageInfo          `json:"tool_usage,omitempty"`
	MCPToolUsage            *MCPToolUsageData        `json:"mcp_tool_usage,omitempty"`
}

// Finding represents a key insight discovered during audit
type Finding struct {
	Category    string `json:"category"`         // e.g., "error", "performance", "cost", "tooling"
	Severity    string `json:"severity"`         // "critical", "high", "medium", "low", "info"
	Title       string `json:"title"`            // Brief title
	Description string `json:"description"`      // Detailed description
	Impact      string `json:"impact,omitempty"` // What impact this has
}

// Recommendation represents an actionable suggestion
type Recommendation struct {
	Priority string `json:"priority"`          // "high", "medium", "low"
	Action   string `json:"action"`            // What to do
	Reason   string `json:"reason"`            // Why to do it
	Example  string `json:"example,omitempty"` // Example of how to implement
}

// FailureAnalysis provides structured analysis for failed workflows
type FailureAnalysis struct {
	PrimaryFailure string   `json:"primary_failure"`      // Main reason for failure
	FailedJobs     []string `json:"failed_jobs"`          // List of failed job names
	ErrorSummary   string   `json:"error_summary"`        // Summary of errors
	RootCause      string   `json:"root_cause,omitempty"` // Identified root cause if determinable
}

// PerformanceMetrics provides aggregated performance statistics
type PerformanceMetrics struct {
	TokensPerMinute float64 `json:"tokens_per_minute,omitempty"`
	CostEfficiency  string  `json:"cost_efficiency,omitempty"` // e.g., "good", "poor"
	AvgToolDuration string  `json:"avg_tool_duration,omitempty"`
	MostUsedTool    string  `json:"most_used_tool,omitempty"`
	NetworkRequests int     `json:"network_requests,omitempty"`
}

// OverviewData contains basic information about the workflow run
type OverviewData struct {
	RunID        int64     `json:"run_id" console:"header:Run ID"`
	WorkflowName string    `json:"workflow_name" console:"header:Workflow"`
	Status       string    `json:"status" console:"header:Status"`
	Conclusion   string    `json:"conclusion,omitempty" console:"header:Conclusion,omitempty"`
	CreatedAt    time.Time `json:"created_at" console:"header:Created At"`
	StartedAt    time.Time `json:"started_at,omitempty" console:"header:Started At,omitempty"`
	UpdatedAt    time.Time `json:"updated_at,omitempty" console:"header:Updated At,omitempty"`
	Duration     string    `json:"duration,omitempty" console:"header:Duration,omitempty"`
	Event        string    `json:"event" console:"header:Event"`
	Branch       string    `json:"branch" console:"header:Branch"`
	URL          string    `json:"url" console:"header:URL"`
	LogsPath     string    `json:"logs_path,omitempty" console:"header:Files,omitempty"`
}

// MetricsData contains execution metrics
type MetricsData struct {
	TokenUsage    int     `json:"token_usage,omitempty" console:"header:Token Usage,format:number,omitempty"`
	EstimatedCost float64 `json:"estimated_cost,omitempty" console:"header:Estimated Cost,format:cost,omitempty"`
	Turns         int     `json:"turns,omitempty" console:"header:Turns,omitempty"`
	ErrorCount    int     `json:"error_count" console:"header:Errors"`
	WarningCount  int     `json:"warning_count" console:"header:Warnings"`
}

// JobData contains information about individual jobs
type JobData struct {
	Name       string `json:"name" console:"header:Name"`
	Status     string `json:"status" console:"header:Status"`
	Conclusion string `json:"conclusion,omitempty" console:"header:Conclusion,omitempty"`
	Duration   string `json:"duration,omitempty" console:"header:Duration,omitempty"`
}

// FileInfo contains information about downloaded artifact files
type FileInfo struct {
	Path        string `json:"path"`
	Size        int64  `json:"size"`
	Description string `json:"description"`
}

// ErrorInfo contains detailed error information
type ErrorInfo struct {
	File    string `json:"file,omitempty"`
	Line    int    `json:"line,omitempty"`
	Type    string `json:"type"`
	Message string `json:"message"`
}

// ToolUsageInfo contains aggregated tool usage statistics
type ToolUsageInfo struct {
	Name          string `json:"name" console:"header:Tool"`
	CallCount     int    `json:"call_count" console:"header:Calls"`
	MaxInputSize  int    `json:"max_input_size,omitempty" console:"header:Max Input,format:number,omitempty"`
	MaxOutputSize int    `json:"max_output_size,omitempty" console:"header:Max Output,format:number,omitempty"`
	MaxDuration   string `json:"max_duration,omitempty" console:"header:Max Duration,omitempty"`
}

// MCPToolUsageData contains detailed MCP tool usage statistics and individual call records
type MCPToolUsageData struct {
	Summary   []MCPToolSummary `json:"summary"`           // Aggregated statistics per tool
	ToolCalls []MCPToolCall    `json:"tool_calls"`        // Individual tool call records
	Servers   []MCPServerStats `json:"servers,omitempty"` // Server-level statistics
}

// MCPToolSummary contains aggregated statistics for a single MCP tool
type MCPToolSummary struct {
	ServerName      string `json:"server_name" console:"header:Server"`
	ToolName        string `json:"tool_name" console:"header:Tool"`
	CallCount       int    `json:"call_count" console:"header:Calls"`
	TotalInputSize  int    `json:"total_input_size" console:"header:Total Input,format:number"`
	TotalOutputSize int    `json:"total_output_size" console:"header:Total Output,format:number"`
	MaxInputSize    int    `json:"max_input_size" console:"header:Max Input,format:number"`
	MaxOutputSize   int    `json:"max_output_size" console:"header:Max Output,format:number"`
	AvgDuration     string `json:"avg_duration,omitempty" console:"header:Avg Duration,omitempty"`
	MaxDuration     string `json:"max_duration,omitempty" console:"header:Max Duration,omitempty"`
	ErrorCount      int    `json:"error_count,omitempty" console:"header:Errors,omitempty"`
}

// MCPToolCall represents a single MCP tool call with full details
type MCPToolCall struct {
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
	ServerName      string `json:"server_name" console:"header:Server"`
	RequestCount    int    `json:"request_count" console:"header:Requests"`
	ToolCallCount   int    `json:"tool_call_count" console:"header:Tool Calls"`
	TotalInputSize  int    `json:"total_input_size" console:"header:Total Input,format:number"`
	TotalOutputSize int    `json:"total_output_size" console:"header:Total Output,format:number"`
	AvgDuration     string `json:"avg_duration,omitempty" console:"header:Avg Duration,omitempty"`
	ErrorCount      int    `json:"error_count,omitempty" console:"header:Errors,omitempty"`
}

// OverviewDisplay is a display-optimized version of OverviewData for console rendering
type OverviewDisplay struct {
	RunID    int64  `console:"header:Run ID"`
	Workflow string `console:"header:Workflow"`
	Status   string `console:"header:Status"`
	Duration string `console:"header:Duration,omitempty"`
	Event    string `console:"header:Event"`
	Branch   string `console:"header:Branch"`
	URL      string `console:"header:URL"`
	Files    string `console:"header:Files,omitempty"`
}

// buildAuditData creates structured audit data from workflow run information
func buildAuditData(processedRun ProcessedRun, metrics LogMetrics, mcpToolUsage *MCPToolUsageData) AuditData {
	run := processedRun.Run
	auditReportLog.Printf("Building audit data for run ID %d", run.DatabaseID)

	// Build overview
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
	}

	// Convert LogsPath to relative path from workspace root
	if run.LogsPath != "" {
		logsPathDisplay := run.LogsPath
		if cwd, err := os.Getwd(); err == nil {
			if relPath, err := filepath.Rel(cwd, run.LogsPath); err == nil {
				logsPathDisplay = relPath
			}
		}
		overview.LogsPath = logsPathDisplay
	}

	if run.Duration > 0 {
		overview.Duration = timeutil.FormatDuration(run.Duration)
	}

	// Build metrics
	metricsData := MetricsData{
		TokenUsage:    run.TokenUsage,
		EstimatedCost: run.EstimatedCost,
		Turns:         run.Turns,
		ErrorCount:    run.ErrorCount,
		WarningCount:  run.WarningCount,
	}

	// Build job data
	var jobs []JobData
	for _, jobDetail := range processedRun.JobDetails {
		job := JobData{
			Name:       jobDetail.Name,
			Status:     jobDetail.Status,
			Conclusion: jobDetail.Conclusion,
		}
		if jobDetail.Duration > 0 {
			job.Duration = timeutil.FormatDuration(jobDetail.Duration)
		}
		jobs = append(jobs, job)
	}

	// Build downloaded files list
	downloadedFiles := extractDownloadedFiles(run.LogsPath)

	// No error/warning extraction since error patterns have been removed
	var errors []ErrorInfo
	var warnings []ErrorInfo

	// Build tool usage
	var toolUsage []ToolUsageInfo
	toolStats := make(map[string]*ToolUsageInfo)
	for _, toolCall := range metrics.ToolCalls {
		displayKey := workflow.PrettifyToolName(toolCall.Name)
		if existing, exists := toolStats[displayKey]; exists {
			existing.CallCount += toolCall.CallCount
			if toolCall.MaxInputSize > existing.MaxInputSize {
				existing.MaxInputSize = toolCall.MaxInputSize
			}
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
			info := &ToolUsageInfo{
				Name:          displayKey,
				CallCount:     toolCall.CallCount,
				MaxInputSize:  toolCall.MaxInputSize,
				MaxOutputSize: toolCall.MaxOutputSize,
			}
			if toolCall.MaxDuration > 0 {
				info.MaxDuration = timeutil.FormatDuration(toolCall.MaxDuration)
			}
			toolStats[displayKey] = info
		}
	}
	for _, info := range toolStats {
		toolUsage = append(toolUsage, *info)
	}

	// Generate key findings
	findings := generateFindings(processedRun, metricsData, errors, warnings)

	// Generate recommendations
	recommendations := generateRecommendations(processedRun, metricsData, findings)

	// Generate failure analysis if workflow failed
	var failureAnalysis *FailureAnalysis
	if run.Conclusion == "failure" || run.Conclusion == "timed_out" || run.Conclusion == "cancelled" {
		failureAnalysis = generateFailureAnalysis(processedRun, errors)
	}

	// Generate performance metrics
	performanceMetrics := generatePerformanceMetrics(processedRun, metricsData, toolUsage)

	if auditReportLog.Enabled() {
		auditReportLog.Printf("Built audit data: %d jobs, %d errors, %d warnings, %d tool types, %d findings, %d recommendations",
			len(jobs), len(errors), len(warnings), len(toolUsage), len(findings), len(recommendations))
	}

	return AuditData{
		Overview:                overview,
		Metrics:                 metricsData,
		KeyFindings:             findings,
		Recommendations:         recommendations,
		FailureAnalysis:         failureAnalysis,
		PerformanceMetrics:      performanceMetrics,
		Jobs:                    jobs,
		DownloadedFiles:         downloadedFiles,
		MissingTools:            processedRun.MissingTools,
		MissingData:             processedRun.MissingData,
		Noops:                   processedRun.Noops,
		MCPFailures:             processedRun.MCPFailures,
		FirewallAnalysis:        processedRun.FirewallAnalysis,
		RedactedDomainsAnalysis: processedRun.RedactedDomainsAnalysis,
		Errors:                  errors,
		Warnings:                warnings,
		ToolUsage:               toolUsage,
		MCPToolUsage:            mcpToolUsage,
	}
}

// extractDownloadedFiles scans the logs directory and returns file information
func extractDownloadedFiles(logsPath string) []FileInfo {
	auditReportLog.Printf("Extracting downloaded files from: %s", logsPath)
	var files []FileInfo

	entries, err := os.ReadDir(logsPath)
	if err != nil {
		auditReportLog.Printf("Failed to read logs directory: %v", err)
		return files
	}

	// Get current working directory to calculate relative paths
	cwd, err := os.Getwd()
	if err != nil {
		auditReportLog.Printf("Failed to get current directory: %v", err)
		cwd = ""
	}

	for _, entry := range entries {
		// Skip directories
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		fullPath := filepath.Join(logsPath, name)

		// Calculate relative path from workspace root (current working directory)
		relativePath := fullPath
		if cwd != "" {
			if relPath, err := filepath.Rel(cwd, fullPath); err == nil {
				relativePath = relPath
			}
		}

		fileInfo := FileInfo{
			Path:        relativePath,
			Description: describeFile(name),
		}

		if info, err := os.Stat(fullPath); err == nil {
			fileInfo.Size = info.Size()
		}

		files = append(files, fileInfo)
	}

	auditReportLog.Printf("Extracted %d files from logs directory", len(files))
	return files
}

// describeFile provides a short description for known artifact files
func describeFile(filename string) string {
	descriptions := map[string]string{
		"aw_info.json":                "Engine configuration and workflow metadata",
		"safe_output.jsonl":           "Safe outputs from workflow execution",
		constants.AgentOutputFilename: "Validated safe outputs",
		"aw.patch":                    "Git patch of changes made during execution",
		"agent-stdio.log":             "Agent standard output/error logs",
		"log.md":                      "Human-readable agent session summary",
		"firewall.md":                 "Firewall log analysis report",
		"run_summary.json":            "Cached summary of workflow run analysis",
		"prompt.txt":                  "Input prompt for AI agent",
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

// renderJSON outputs the audit data as JSON
