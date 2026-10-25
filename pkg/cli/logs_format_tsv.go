package cli

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

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

// renderLogsTSVToWriter outputs the logs data as tab-separated values to w.
// This format is ~24x more compact than JSON, making it ideal for agentic consumption
// where LLM context window tokens are the primary constraint.
//
// Output format:
//
//	Line 1: Summary line (total_runs, total_duration, total_tokens, total_turns, total_errors)
//	Line 2: Column headers
//	Lines 3+: One line per run with tab-separated fields
func renderLogsTSVToWriter(w io.Writer, data LogsData) {
	logsTSVLog.Printf("Rendering %d runs as TSV", data.Summary.TotalRuns)

	s := data.Summary
	// Summary line with key aggregates
	if _, err := fmt.Fprintf(w, "# %d runs | %s duration | %s tokens | %d turns | %d errors\n",
		s.TotalRuns, s.TotalDuration, formatTSVSummaryTokens(s.TotalTokens), s.TotalTurns, s.TotalErrors); err != nil {
		logsTSVLog.Printf("Failed to write TSV summary: %v", err)
		return
	}

	if len(data.Runs) == 0 {
		return
	}

	// Header
	headers := []string{
		"run_id", "workflow", "engine", "status", "duration",
		"tokens", "aic", "turns", "errors",
		"event", "branch", "created_at", "classification", "url",
	}
	if _, err := fmt.Fprintln(w, strings.Join(headers, "\t")); err != nil {
		logsTSVLog.Printf("Failed to write TSV headers: %v", err)
		return
	}

	// Rows
	for _, r := range data.Runs {
		conclusion := r.Conclusion
		if conclusion == "" {
			conclusion = r.Status
		}
		duration := r.Duration
		if duration == "" {
			duration = "-"
		}
		classification := r.Classification
		if classification == "" {
			classification = "-"
		}
		// Shorten URL to just the run path for density
		url := r.URL
		if idx := strings.Index(url, "/actions/runs/"); idx > 0 {
			url = url[idx:]
		}

		fields := []string{
			strconv.FormatInt(r.RunID, 10),
			r.WorkflowName,
			r.EngineID,
			conclusion,
			duration,
			strconv.Itoa(r.TokenUsage),
			fmt.Sprintf("%.3f", r.AIC),
			strconv.Itoa(r.Turns),
			strconv.Itoa(r.ErrorCount),
			r.Event,
			r.Branch,
			r.CreatedAt.Format("2006-01-02T15:04"),
			classification,
			url,
		}
		if _, err := fmt.Fprintln(w, strings.Join(fields, "\t")); err != nil {
			logsTSVLog.Printf("Failed to write TSV run: %v", err)
			return
		}
	}

	// Append observability insights as comments (high signal density)
	if len(data.Observability) > 0 {
		if _, err := fmt.Fprintln(w, "# insights:"); err != nil {
			logsTSVLog.Printf("Failed to write TSV insights header: %v", err)
			return
		}
		for _, obs := range data.Observability {
			if _, err := fmt.Fprintf(w, "# [%s] %s: %s\n", obs.Severity, obs.Title, obs.Summary); err != nil {
				logsTSVLog.Printf("Failed to write TSV insight: %v", err)
				return
			}
		}
	}

	// Append firewall summary if present
	if data.FirewallLog != nil && data.FirewallLog.TotalRequests > 0 {
		if _, err := fmt.Fprintf(w, "# firewall: %d requests (%d allowed, %d blocked)\n",
			data.FirewallLog.TotalRequests, data.FirewallLog.AllowedRequests, data.FirewallLog.BlockedRequests); err != nil {
			logsTSVLog.Printf("Failed to write TSV firewall summary: %v", err)
			return
		}
	}

	// Append engine distribution if multiple engines
	if len(data.Summary.EngineCounts) > 1 {
		parts := make([]string, 0, len(data.Summary.EngineCounts))
		for engine, count := range data.Summary.EngineCounts {
			parts = append(parts, fmt.Sprintf("%s:%d", engine, count))
		}
		if _, err := fmt.Fprintf(w, "# engines: %s\n", strings.Join(parts, " ")); err != nil {
			logsTSVLog.Printf("Failed to write TSV engine distribution: %v", err)
		}
	}
}

// renderLogsTSV outputs the logs data as tab-separated values to os.Stdout.
func renderLogsTSV(data LogsData) {
	renderLogsTSVToWriter(os.Stdout, data)
}

// renderLogsTSVVerboseToWriter outputs a more detailed TSV with additional columns for audit use to w.
func renderLogsTSVVerboseToWriter(w io.Writer, data LogsData) {
	logsTSVLog.Printf("Rendering %d runs as verbose TSV", data.Summary.TotalRuns)

	s := data.Summary
	if _, err := fmt.Fprintf(w, "# %d runs | %s duration | %s tokens | %d turns | %d errors | %d missing_tools | %d github_api_calls\n",
		s.TotalRuns, s.TotalDuration, formatTSVSummaryTokens(s.TotalTokens), s.TotalTurns, s.TotalErrors, s.TotalMissingTools, s.TotalGitHubAPICalls); err != nil {
		logsTSVLog.Printf("Failed to write verbose TSV summary: %v", err)
		return
	}

	if len(data.Runs) == 0 {
		return
	}

	headers := []string{
		"run_id", "workflow", "engine", "status", "duration",
		"tokens", "aic", "turns", "errors",
		"warnings", "missing_tools", "missing_data", "github_api",
		"event", "branch", "actor", "created_at", "tbt",
		"classification", "action_min", "display_title", "url",
	}
	if _, err := fmt.Fprintln(w, strings.Join(headers, "\t")); err != nil {
		logsTSVLog.Printf("Failed to write verbose TSV headers: %v", err)
		return
	}

	for _, r := range data.Runs {
		conclusion := r.Conclusion
		if conclusion == "" {
			conclusion = r.Status
		}
		duration := r.Duration
		if duration == "" {
			duration = "-"
		}
		tbt := r.AvgTimeBetweenTurns
		if tbt == "" {
			tbt = "-"
		}
		classification := r.Classification
		if classification == "" {
			classification = "-"
		}
		displayTitle := stringutil.Truncate(r.DisplayTitle, 50)

		fields := []string{
			strconv.FormatInt(r.RunID, 10),
			r.WorkflowName,
			r.EngineID,
			conclusion,
			duration,
			strconv.Itoa(r.TokenUsage),
			fmt.Sprintf("%.3f", r.AIC),
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
			tbt,
			classification,
			fmt.Sprintf("%.0f", r.ActionMinutes),
			displayTitle,
			r.URL,
		}
		if _, err := fmt.Fprintln(w, strings.Join(fields, "\t")); err != nil {
			logsTSVLog.Printf("Failed to write verbose TSV run: %v", err)
			return
		}
	}

	if len(data.Observability) > 0 {
		if _, err := fmt.Fprintln(w, "# insights:"); err != nil {
			logsTSVLog.Printf("Failed to write verbose TSV insights header: %v", err)
			return
		}
		for _, obs := range data.Observability {
			if _, err := fmt.Fprintf(w, "# [%s] %s: %s\n", obs.Severity, obs.Title, obs.Summary); err != nil {
				logsTSVLog.Printf("Failed to write verbose TSV insight: %v", err)
				return
			}
		}
	}
}

// renderLogsTSVVerbose outputs a more detailed TSV with additional columns to os.Stdout.
func renderLogsTSVVerbose(data LogsData) {
	renderLogsTSVVerboseToWriter(os.Stdout, data)
}
