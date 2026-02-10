package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/stringutil"
)

// renderJSON outputs the audit data as JSON
func renderJSON(data AuditData) error {
	auditReportLog.Print("Rendering audit report as JSON")
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(data)
}

// renderConsole outputs the audit data as formatted console tables
func renderConsole(data AuditData, logsPath string) {
	auditReportLog.Print("Rendering audit report to console")
	fmt.Fprintln(os.Stderr, console.FormatSectionHeader("Workflow Run Audit Report"))
	fmt.Fprintln(os.Stderr)

	// Overview Section - use new rendering system
	fmt.Fprintln(os.Stderr, console.FormatSectionHeader("Overview"))
	fmt.Fprintln(os.Stderr)
	renderOverview(data.Overview)

	// Key Findings Section - NEW
	if len(data.KeyFindings) > 0 {
		auditReportLog.Printf("Rendering %d key findings", len(data.KeyFindings))
		fmt.Fprintln(os.Stderr, console.FormatSectionHeader("Key Findings"))
		fmt.Fprintln(os.Stderr)
		renderKeyFindings(data.KeyFindings)
	}

	// Recommendations Section - NEW
	if len(data.Recommendations) > 0 {
		auditReportLog.Printf("Rendering %d recommendations", len(data.Recommendations))
		fmt.Fprintln(os.Stderr, console.FormatSectionHeader("Recommendations"))
		fmt.Fprintln(os.Stderr)
		renderRecommendations(data.Recommendations)
	}

	// Failure Analysis Section - NEW
	if data.FailureAnalysis != nil {
		auditReportLog.Print("Rendering failure analysis")
		fmt.Fprintln(os.Stderr, console.FormatSectionHeader("Failure Analysis"))
		fmt.Fprintln(os.Stderr)
		renderFailureAnalysis(data.FailureAnalysis)
	}

	// Performance Metrics Section - NEW
	if data.PerformanceMetrics != nil {
		fmt.Fprintln(os.Stderr, console.FormatSectionHeader("Performance Metrics"))
		fmt.Fprintln(os.Stderr)
		renderPerformanceMetrics(data.PerformanceMetrics)
	}

	// Metrics Section - use new rendering system
	fmt.Fprintln(os.Stderr, console.FormatSectionHeader("Metrics"))
	fmt.Fprintln(os.Stderr)
	renderMetrics(data.Metrics)

	// Jobs Section - use new table rendering
	if len(data.Jobs) > 0 {
		auditReportLog.Printf("Rendering jobs table with %d jobs", len(data.Jobs))
		fmt.Fprintln(os.Stderr, console.FormatSectionHeader("Jobs"))
		fmt.Fprintln(os.Stderr)
		renderJobsTable(data.Jobs)
	}

	// Downloaded Files Section
	if len(data.DownloadedFiles) > 0 {
		fmt.Fprintln(os.Stderr, console.FormatSectionHeader("Downloaded Files"))
		fmt.Fprintln(os.Stderr)
		for _, file := range data.DownloadedFiles {
			formattedSize := console.FormatFileSize(file.Size)
			fmt.Fprintf(os.Stderr, "  • %s (%s)", file.Path, formattedSize)
			if file.Description != "" {
				fmt.Fprintf(os.Stderr, " - %s", file.Description)
			}
			fmt.Fprintln(os.Stderr)
		}
		fmt.Fprintln(os.Stderr)
	}

	// Missing Tools Section
	if len(data.MissingTools) > 0 {
		fmt.Fprintln(os.Stderr, console.FormatSectionHeader("Missing Tools"))
		fmt.Fprintln(os.Stderr)
		for _, tool := range data.MissingTools {
			fmt.Fprintf(os.Stderr, "  • %s\n", tool.Tool)
			fmt.Fprintf(os.Stderr, "    Reason: %s\n", tool.Reason)
			if tool.Alternatives != "" {
				fmt.Fprintf(os.Stderr, "    Alternatives: %s\n", tool.Alternatives)
			}
		}
		fmt.Fprintln(os.Stderr)
	}

	// MCP Failures Section
	if len(data.MCPFailures) > 0 {
		fmt.Fprintln(os.Stderr, console.FormatSectionHeader("MCP Server Failures"))
		fmt.Fprintln(os.Stderr)
		for _, failure := range data.MCPFailures {
			fmt.Fprintf(os.Stderr, "  • %s: %s\n", failure.ServerName, failure.Status)
		}
		fmt.Fprintln(os.Stderr)
	}

	// Firewall Analysis Section
	if data.FirewallAnalysis != nil && data.FirewallAnalysis.TotalRequests > 0 {
		fmt.Fprintln(os.Stderr, console.FormatSectionHeader("Firewall Analysis"))
		fmt.Fprintln(os.Stderr)
		renderFirewallAnalysis(data.FirewallAnalysis)
	}

	// Redacted Domains Section
	if data.RedactedDomainsAnalysis != nil && data.RedactedDomainsAnalysis.TotalDomains > 0 {
		fmt.Fprintln(os.Stderr, console.FormatSectionHeader("🔒 Redacted URL Domains"))
		fmt.Fprintln(os.Stderr)
		renderRedactedDomainsAnalysis(data.RedactedDomainsAnalysis)
	}

	// Tool Usage Section - use new table rendering
	if len(data.ToolUsage) > 0 {
		fmt.Fprintln(os.Stderr, console.FormatSectionHeader("Tool Usage"))
		fmt.Fprintln(os.Stderr)
		renderToolUsageTable(data.ToolUsage)
	}

	// MCP Tool Usage Section - detailed MCP statistics
	if data.MCPToolUsage != nil && len(data.MCPToolUsage.Summary) > 0 {
		fmt.Fprintln(os.Stderr, console.FormatSectionHeader("MCP Tool Usage"))
		fmt.Fprintln(os.Stderr)
		renderMCPToolUsageTable(data.MCPToolUsage)
	}

	// Errors and Warnings Section
	if len(data.Errors) > 0 || len(data.Warnings) > 0 {
		fmt.Fprintln(os.Stderr, console.FormatSectionHeader("Errors and Warnings"))
		fmt.Fprintln(os.Stderr)

		if len(data.Errors) > 0 {
			fmt.Fprintln(os.Stderr, console.FormatErrorMessage(fmt.Sprintf("Errors (%d):", len(data.Errors))))
			for _, err := range data.Errors {
				if err.File != "" && err.Line > 0 {
					fmt.Fprintf(os.Stderr, "    %s:%d: %s\n", filepath.Base(err.File), err.Line, err.Message)
				} else {
					fmt.Fprintf(os.Stderr, "    %s\n", err.Message)
				}
			}
			fmt.Fprintln(os.Stderr)
		}

		if len(data.Warnings) > 0 {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Warnings (%d):", len(data.Warnings))))
			for _, warn := range data.Warnings {
				if warn.File != "" && warn.Line > 0 {
					fmt.Fprintf(os.Stderr, "    %s:%d: %s\n", filepath.Base(warn.File), warn.Line, warn.Message)
				} else {
					fmt.Fprintf(os.Stderr, "    %s\n", warn.Message)
				}
			}
			fmt.Fprintln(os.Stderr)
		}
	}

	// Location
	fmt.Fprintln(os.Stderr, console.FormatSectionHeader("Logs Location"))
	fmt.Fprintln(os.Stderr)
	absPath, _ := filepath.Abs(logsPath)
	fmt.Fprintf(os.Stderr, "  %s\n", absPath)
	fmt.Fprintln(os.Stderr)
}

// renderOverview renders the overview section using the new rendering system
func renderOverview(overview OverviewData) {
	// Format Status with optional Conclusion
	statusLine := overview.Status
	if overview.Conclusion != "" && overview.Status == "completed" {
		statusLine = fmt.Sprintf("%s (%s)", overview.Status, overview.Conclusion)
	}

	display := OverviewDisplay{
		RunID:    overview.RunID,
		Workflow: overview.WorkflowName,
		Status:   statusLine,
		Duration: overview.Duration,
		Event:    overview.Event,
		Branch:   overview.Branch,
		URL:      overview.URL,
		Files:    overview.LogsPath,
	}

	fmt.Fprint(os.Stderr, console.RenderStruct(display))
}

// renderMetrics renders the metrics section using the new rendering system
func renderMetrics(metrics MetricsData) {
	fmt.Fprint(os.Stderr, console.RenderStruct(metrics))
}

// renderJobsTable renders the jobs as a table using console.RenderTable
func renderJobsTable(jobs []JobData) {
	auditReportLog.Printf("Rendering jobs table with %d jobs", len(jobs))
	config := console.TableConfig{
		Headers: []string{"Name", "Status", "Conclusion", "Duration"},
		Rows:    make([][]string, 0, len(jobs)),
	}

	for _, job := range jobs {
		conclusion := job.Conclusion
		if conclusion == "" {
			conclusion = "-"
		}
		duration := job.Duration
		if duration == "" {
			duration = "-"
		}

		row := []string{
			stringutil.Truncate(job.Name, 40),
			job.Status,
			conclusion,
			duration,
		}
		config.Rows = append(config.Rows, row)
	}

	fmt.Fprint(os.Stderr, console.RenderTable(config))
}

// renderToolUsageTable renders tool usage as a table with custom formatting
func renderToolUsageTable(toolUsage []ToolUsageInfo) {
	auditReportLog.Printf("Rendering tool usage table with %d tools", len(toolUsage))
	config := console.TableConfig{
		Headers: []string{"Tool", "Calls", "Max Input", "Max Output", "Max Duration"},
		Rows:    make([][]string, 0, len(toolUsage)),
	}

	for _, tool := range toolUsage {
		inputStr := "N/A"
		if tool.MaxInputSize > 0 {
			inputStr = console.FormatNumber(tool.MaxInputSize)
		}
		outputStr := "N/A"
		if tool.MaxOutputSize > 0 {
			outputStr = console.FormatNumber(tool.MaxOutputSize)
		}
		durationStr := "N/A"
		if tool.MaxDuration != "" {
			durationStr = tool.MaxDuration
		}

		row := []string{
			stringutil.Truncate(tool.Name, 40),
			fmt.Sprintf("%d", tool.CallCount),
			inputStr,
			outputStr,
			durationStr,
		}
		config.Rows = append(config.Rows, row)
	}

	fmt.Fprint(os.Stderr, console.RenderTable(config))
}

// renderMCPToolUsageTable renders MCP tool usage with detailed statistics
func renderMCPToolUsageTable(mcpData *MCPToolUsageData) {
	auditReportLog.Printf("Rendering MCP tool usage table with %d tools", len(mcpData.Summary))

	// Render server-level statistics first
	if len(mcpData.Servers) > 0 {
		fmt.Fprintln(os.Stderr, "  Server Statistics:")
		fmt.Fprintln(os.Stderr)

		serverConfig := console.TableConfig{
			Headers: []string{"Server", "Requests", "Tool Calls", "Total Input", "Total Output", "Avg Duration", "Errors"},
			Rows:    make([][]string, 0, len(mcpData.Servers)),
		}

		for _, server := range mcpData.Servers {
			inputStr := console.FormatFileSize(int64(server.TotalInputSize))
			outputStr := console.FormatFileSize(int64(server.TotalOutputSize))
			durationStr := server.AvgDuration
			if durationStr == "" {
				durationStr = "N/A"
			}
			errorStr := fmt.Sprintf("%d", server.ErrorCount)
			if server.ErrorCount == 0 {
				errorStr = "-"
			}

			row := []string{
				stringutil.Truncate(server.ServerName, 25),
				fmt.Sprintf("%d", server.RequestCount),
				fmt.Sprintf("%d", server.ToolCallCount),
				inputStr,
				outputStr,
				durationStr,
				errorStr,
			}
			serverConfig.Rows = append(serverConfig.Rows, row)
		}

		fmt.Fprint(os.Stderr, console.RenderTable(serverConfig))
		fmt.Fprintln(os.Stderr)
	}

	// Render tool-level statistics
	if len(mcpData.Summary) > 0 {
		fmt.Fprintln(os.Stderr, "  Tool Statistics:")
		fmt.Fprintln(os.Stderr)

		toolConfig := console.TableConfig{
			Headers: []string{"Server", "Tool", "Calls", "Total In", "Total Out", "Max In", "Max Out"},
			Rows:    make([][]string, 0, len(mcpData.Summary)),
		}

		for _, tool := range mcpData.Summary {
			totalInStr := console.FormatFileSize(int64(tool.TotalInputSize))
			totalOutStr := console.FormatFileSize(int64(tool.TotalOutputSize))
			maxInStr := console.FormatFileSize(int64(tool.MaxInputSize))
			maxOutStr := console.FormatFileSize(int64(tool.MaxOutputSize))

			row := []string{
				stringutil.Truncate(tool.ServerName, 20),
				stringutil.Truncate(tool.ToolName, 30),
				fmt.Sprintf("%d", tool.CallCount),
				totalInStr,
				totalOutStr,
				maxInStr,
				maxOutStr,
			}
			toolConfig.Rows = append(toolConfig.Rows, row)
		}

		fmt.Fprint(os.Stderr, console.RenderTable(toolConfig))
	}
}

// renderFirewallAnalysis renders firewall analysis with summary and domain breakdown
func renderFirewallAnalysis(analysis *FirewallAnalysis) {
	// Summary statistics
	fmt.Fprintf(os.Stderr, "  Total Requests : %d\n", analysis.TotalRequests)
	fmt.Fprintf(os.Stderr, "  Allowed        : %d\n", analysis.AllowedRequests)
	fmt.Fprintf(os.Stderr, "  Blocked        : %d\n", analysis.BlockedRequests)
	fmt.Fprintln(os.Stderr)

	// Allowed domains
	if len(analysis.AllowedDomains) > 0 {
		fmt.Fprintln(os.Stderr, "  Allowed Domains:")
		for _, domain := range analysis.AllowedDomains {
			if stats, ok := analysis.RequestsByDomain[domain]; ok {
				fmt.Fprintf(os.Stderr, "    ✓ %s (%d requests)\n", domain, stats.Allowed)
			}
		}
		fmt.Fprintln(os.Stderr)
	}

	// Blocked domains
	if len(analysis.BlockedDomains) > 0 {
		fmt.Fprintln(os.Stderr, "  Blocked Domains:")
		for _, domain := range analysis.BlockedDomains {
			if stats, ok := analysis.RequestsByDomain[domain]; ok {
				fmt.Fprintf(os.Stderr, "    ✗ %s (%d requests)\n", domain, stats.Blocked)
			}
		}
		fmt.Fprintln(os.Stderr)
	}
}

// renderRedactedDomainsAnalysis renders redacted domains analysis
func renderRedactedDomainsAnalysis(analysis *RedactedDomainsAnalysis) {
	// Summary statistics
	fmt.Fprintf(os.Stderr, "  Total Domains Redacted: %d\n", analysis.TotalDomains)
	fmt.Fprintln(os.Stderr)

	// List domains
	if len(analysis.Domains) > 0 {
		fmt.Fprintln(os.Stderr, "  Redacted Domains:")
		for _, domain := range analysis.Domains {
			fmt.Fprintf(os.Stderr, "    🔒 %s\n", domain)
		}
		fmt.Fprintln(os.Stderr)
	}
}

// renderKeyFindings renders key findings with colored severity indicators
func renderKeyFindings(findings []Finding) {
	// Group findings by severity for better presentation
	critical := []Finding{}
	high := []Finding{}
	medium := []Finding{}
	low := []Finding{}
	info := []Finding{}

	for _, finding := range findings {
		switch finding.Severity {
		case "critical":
			critical = append(critical, finding)
		case "high":
			high = append(high, finding)
		case "medium":
			medium = append(medium, finding)
		case "low":
			low = append(low, finding)
		default:
			info = append(info, finding)
		}
	}

	// Render critical findings first
	for _, finding := range critical {
		fmt.Fprintf(os.Stderr, "  🔴 %s [%s]\n", console.FormatErrorMessage(finding.Title), finding.Category)
		fmt.Fprintf(os.Stderr, "     %s\n", finding.Description)
		if finding.Impact != "" {
			fmt.Fprintf(os.Stderr, "     Impact: %s\n", finding.Impact)
		}
		fmt.Fprintln(os.Stderr)
	}

	// Then high severity
	for _, finding := range high {
		fmt.Fprintf(os.Stderr, "  🟠 %s [%s]\n", console.FormatWarningMessage(finding.Title), finding.Category)
		fmt.Fprintf(os.Stderr, "     %s\n", finding.Description)
		if finding.Impact != "" {
			fmt.Fprintf(os.Stderr, "     Impact: %s\n", finding.Impact)
		}
		fmt.Fprintln(os.Stderr)
	}

	// Medium severity
	for _, finding := range medium {
		fmt.Fprintf(os.Stderr, "  🟡 %s [%s]\n", finding.Title, finding.Category)
		fmt.Fprintf(os.Stderr, "     %s\n", finding.Description)
		if finding.Impact != "" {
			fmt.Fprintf(os.Stderr, "     Impact: %s\n", finding.Impact)
		}
		fmt.Fprintln(os.Stderr)
	}

	// Low severity
	for _, finding := range low {
		fmt.Fprintf(os.Stderr, "  ℹ️  %s [%s]\n", finding.Title, finding.Category)
		fmt.Fprintf(os.Stderr, "     %s\n", finding.Description)
		if finding.Impact != "" {
			fmt.Fprintf(os.Stderr, "     Impact: %s\n", finding.Impact)
		}
		fmt.Fprintln(os.Stderr)
	}

	// Info findings
	for _, finding := range info {
		fmt.Fprintf(os.Stderr, "  ✅ %s [%s]\n", console.FormatSuccessMessage(finding.Title), finding.Category)
		fmt.Fprintf(os.Stderr, "     %s\n", finding.Description)
		if finding.Impact != "" {
			fmt.Fprintf(os.Stderr, "     Impact: %s\n", finding.Impact)
		}
		fmt.Fprintln(os.Stderr)
	}
}

// renderRecommendations renders actionable recommendations
func renderRecommendations(recommendations []Recommendation) {
	// Group by priority
	high := []Recommendation{}
	medium := []Recommendation{}
	low := []Recommendation{}

	for _, rec := range recommendations {
		switch rec.Priority {
		case "high":
			high = append(high, rec)
		case "medium":
			medium = append(medium, rec)
		default:
			low = append(low, rec)
		}
	}

	// Render high priority first
	for i, rec := range high {
		fmt.Fprintf(os.Stderr, "  %d. [HIGH] %s\n", i+1, console.FormatWarningMessage(rec.Action))
		fmt.Fprintf(os.Stderr, "     Reason: %s\n", rec.Reason)
		if rec.Example != "" {
			fmt.Fprintf(os.Stderr, "     Example: %s\n", rec.Example)
		}
		fmt.Fprintln(os.Stderr)
	}

	// Medium priority
	startIdx := len(high) + 1
	for i, rec := range medium {
		fmt.Fprintf(os.Stderr, "  %d. [MEDIUM] %s\n", startIdx+i, rec.Action)
		fmt.Fprintf(os.Stderr, "     Reason: %s\n", rec.Reason)
		if rec.Example != "" {
			fmt.Fprintf(os.Stderr, "     Example: %s\n", rec.Example)
		}
		fmt.Fprintln(os.Stderr)
	}

	// Low priority
	startIdx += len(medium)
	for i, rec := range low {
		fmt.Fprintf(os.Stderr, "  %d. [LOW] %s\n", startIdx+i, rec.Action)
		fmt.Fprintf(os.Stderr, "     Reason: %s\n", rec.Reason)
		if rec.Example != "" {
			fmt.Fprintf(os.Stderr, "     Example: %s\n", rec.Example)
		}
		fmt.Fprintln(os.Stderr)
	}
}

// renderFailureAnalysis renders failure analysis information
func renderFailureAnalysis(analysis *FailureAnalysis) {
	fmt.Fprintf(os.Stderr, "  Primary Failure: %s\n", console.FormatErrorMessage(analysis.PrimaryFailure))
	fmt.Fprintln(os.Stderr)

	if len(analysis.FailedJobs) > 0 {
		fmt.Fprintf(os.Stderr, "  Failed Jobs:\n")
		for _, job := range analysis.FailedJobs {
			fmt.Fprintf(os.Stderr, "    • %s\n", job)
		}
		fmt.Fprintln(os.Stderr)
	}

	fmt.Fprintf(os.Stderr, "  Error Summary: %s\n", analysis.ErrorSummary)
	fmt.Fprintln(os.Stderr)

	if analysis.RootCause != "" {
		fmt.Fprintf(os.Stderr, "  Identified Root Cause: %s\n", console.FormatWarningMessage(analysis.RootCause))
		fmt.Fprintln(os.Stderr)
	}
}

// renderPerformanceMetrics renders performance metrics
func renderPerformanceMetrics(metrics *PerformanceMetrics) {
	if metrics.TokensPerMinute > 0 {
		fmt.Fprintf(os.Stderr, "  Tokens per Minute: %.1f\n", metrics.TokensPerMinute)
	}

	if metrics.CostEfficiency != "" {
		efficiencyDisplay := metrics.CostEfficiency
		switch metrics.CostEfficiency {
		case "excellent", "good":
			efficiencyDisplay = console.FormatSuccessMessage(metrics.CostEfficiency)
		case "moderate":
			efficiencyDisplay = console.FormatWarningMessage(metrics.CostEfficiency)
		case "poor":
			efficiencyDisplay = console.FormatErrorMessage(metrics.CostEfficiency)
		}
		fmt.Fprintf(os.Stderr, "  Cost Efficiency: %s\n", efficiencyDisplay)
	}

	if metrics.AvgToolDuration != "" {
		fmt.Fprintf(os.Stderr, "  Average Tool Duration: %s\n", metrics.AvgToolDuration)
	}

	if metrics.MostUsedTool != "" {
		fmt.Fprintf(os.Stderr, "  Most Used Tool: %s\n", metrics.MostUsedTool)
	}

	if metrics.NetworkRequests > 0 {
		fmt.Fprintf(os.Stderr, "  Network Requests: %d\n", metrics.NetworkRequests)
	}

	fmt.Fprintln(os.Stderr)
}
