package cli

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/stringutil"
)

var logsTSVLog = logger.New("cli:logs_format_tsv")

func formatTSVSummaryTokens(totalTokens int) string {
	if totalTokens == 0 {
		return "n/a"
	}
	return strconv.Itoa(totalTokens)
}

// tsvDefault returns value, or "-" when value is empty.
func tsvDefault(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

// tsvRunStatus returns the run conclusion, falling back to its status.
func tsvRunStatus(r RunData) string {
	if r.Conclusion == "" {
		return r.Status
	}
	return r.Conclusion
}

// renderTSVInsights appends observability insights as comment lines.
func renderTSVInsights(w *compactLogsWriter, insights []ObservabilityInsight) {
	if len(insights) == 0 {
		return
	}
	w.println("# insights:")
	for _, obs := range insights {
		w.printf("# [%s] %s: %s\n", obs.Severity, obs.Title, obs.Summary)
	}
}

// renderLogsTSVToWriter outputs the logs data as tab-separated values to w.
// This format is ~24x more compact than JSON, making it ideal for agentic consumption
// where LLM context window tokens are the primary constraint.
//
// Output format:
//
//	Line 1: Summary line (total_runs, total_duration, total_tokens, total_turns, total_errors)
//	Line 2: Column headers
//	Lines 3+: One line per run with tab-separated fields
func renderLogsTSVToWriter(writer io.Writer, data LogsData) error {
	logsTSVLog.Printf("Rendering %d runs as TSV", data.Summary.TotalRuns)
	w := &compactLogsWriter{writer: writer}

	s := data.Summary
	// Summary line with key aggregates
	w.printf("# %d runs | %s duration | %s tokens | %d turns | %d errors\n",
		s.TotalRuns, s.TotalDuration, formatTSVSummaryTokens(s.TotalTokens), s.TotalTurns, s.TotalErrors)

	if len(data.Runs) == 0 {
		renderTSVContinuationRequestSummary(w, s)
		return w.err
	}

	headers := []string{
		"run_id", "workflow", "engine", "status", "duration",
		"tokens", "aic", "turns", "errors",
		"event", "branch", "created_at", "classification", "url",
	}
	w.println(strings.Join(headers, "\t"))
	for _, r := range data.Runs {
		w.println(strings.Join(tsvRunFields(r), "\t"))
	}

	// Append observability insights as comments (high signal density)
	renderTSVInsights(w, data.Observability)

	// Append firewall summary if present
	if data.FirewallLog != nil && data.FirewallLog.TotalRequests > 0 {
		w.printf("# firewall: %d requests (%d allowed, %d blocked)\n",
			data.FirewallLog.TotalRequests, data.FirewallLog.AllowedRequests, data.FirewallLog.BlockedRequests)
	}

	// Append engine distribution if multiple engines
	if len(data.Summary.EngineCounts) > 1 {
		parts := make([]string, 0, len(data.Summary.EngineCounts))
		for engine, count := range data.Summary.EngineCounts {
			parts = append(parts, fmt.Sprintf("%s:%d", engine, count))
		}
		w.printf("# engines: %s\n", strings.Join(parts, " "))
	}
	renderTSVContinuationRequestSummary(w, s)
	return w.err
}

// tsvRunFields returns the compact TSV row fields for a run.
func tsvRunFields(r RunData) []string {
	// Shorten URL to just the run path for density
	url := r.URL
	if idx := strings.Index(url, "/actions/runs/"); idx > 0 {
		url = url[idx:]
	}
	return []string{
		strconv.FormatInt(r.RunID, 10),
		r.WorkflowName,
		r.EngineID,
		tsvRunStatus(r),
		tsvDefault(r.Duration),
		strconv.Itoa(r.TokenUsage),
		fmt.Sprintf("%.3f", runAICValue(r)),
		strconv.Itoa(r.Turns),
		strconv.Itoa(r.ErrorCount),
		r.Event,
		r.Branch,
		r.CreatedAt.Format("2006-01-02T15:04"),
		tsvDefault(r.Classification),
		url,
	}
}

// renderLogsTSV outputs the logs data as tab-separated values to os.Stdout.
func renderLogsTSV(data LogsData) {
	if err := renderLogsTSVToWriter(os.Stdout, data); err != nil {
		console.PrintErrorMessage("Cannot render log diagnostics: " + err.Error())
	}
}

// renderLogsTSVVerboseToWriter outputs a more detailed TSV with additional columns for audit use to w.
func renderLogsTSVVerboseToWriter(writer io.Writer, data LogsData) error {
	logsTSVLog.Printf("Rendering %d runs as verbose TSV", data.Summary.TotalRuns)
	w := &compactLogsWriter{writer: writer}

	s := data.Summary
	w.printf("# %d runs | %s duration | %s tokens | %d turns | %d errors | %d missing_tools | %d github_api_calls\n",
		s.TotalRuns, s.TotalDuration, formatTSVSummaryTokens(s.TotalTokens), s.TotalTurns, s.TotalErrors, s.TotalMissingTools, s.TotalGitHubAPICalls)

	if len(data.Runs) == 0 {
		renderTSVContinuationRequestSummary(w, s)
		return w.err
	}

	headers := []string{
		"run_id", "workflow", "engine", "status", "duration",
		"tokens", "aic", "turns", "errors",
		"warnings", "missing_tools", "missing_data", "github_api",
		"event", "branch", "actor", "created_at", "tbt",
		"classification", "action_min", "display_title", "url",
	}
	w.println(strings.Join(headers, "\t"))
	for _, r := range data.Runs {
		w.println(strings.Join(tsvVerboseRunFields(r), "\t"))
	}

	renderTSVInsights(w, data.Observability)
	renderTSVContinuationRequestSummary(w, s)
	return w.err
}

// tsvVerboseRunFields returns the verbose TSV row fields for a run.
func tsvVerboseRunFields(r RunData) []string {
	return []string{
		strconv.FormatInt(r.RunID, 10),
		r.WorkflowName,
		r.EngineID,
		tsvRunStatus(r),
		tsvDefault(r.Duration),
		strconv.Itoa(r.TokenUsage),
		fmt.Sprintf("%.3f", runAICValue(r)),
		strconv.Itoa(r.Turns),
		strconv.Itoa(r.ErrorCount),
		strconv.Itoa(r.WarningCount),
		strconv.Itoa(r.MissingToolCount),
		strconv.Itoa(r.MissingDataCount),
		strconv.Itoa(r.GitHubAPICalls),
		r.Event,
		r.Branch,
		r.Actor,
		r.CreatedAt.Format("2006-01-02T15:04"),
		tsvDefault(r.AvgTimeBetweenTurns),
		tsvDefault(r.Classification),
		fmt.Sprintf("%.0f", r.ActionMinutes),
		stringutil.Truncate(r.DisplayTitle, 50),
		r.URL,
	}
}

// renderLogsTSVVerbose outputs a more detailed TSV with additional columns to os.Stdout.
func renderLogsTSVVerbose(data LogsData) {
	if err := renderLogsTSVVerboseToWriter(os.Stdout, data); err != nil {
		console.PrintErrorMessage("Cannot render log diagnostics: " + err.Error())
	}
}
