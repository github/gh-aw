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

// formatTSVAIC renders unavailable cost as "-" so it is not mistaken for a measured zero.
func formatTSVAIC(aic *float64) string {
	if aic == nil {
		return "-"
	}
	return fmt.Sprintf("%.3f", *aic)
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
func renderLogsTSVToWriter(w io.Writer, data LogsData) error {
	logsTSVLog.Printf("Rendering %d runs as TSV", data.Summary.TotalRuns)

	s := data.Summary
	if _, err := fmt.Fprintf(w, "# %d runs | %s duration | %s tokens | %d turns | %d errors\n",
		s.TotalRuns, s.TotalDuration, formatTSVSummaryTokens(s.TotalTokens), s.TotalTurns, s.TotalErrors); err != nil {
		return err
	}
	if len(data.Runs) == 0 {
		return nil
	}
	if err := writeTSVRow(w, []string{
		"run_id", "workflow", "engine", "status", "duration",
		"tokens", "aic", "turns", "errors",
		"event", "branch", "created_at", "classification", "url",
	}); err != nil {
		return err
	}
	if err := renderLogsTSVRunRows(w, data.Runs); err != nil {
		return err
	}
	return renderLogsTSVFooter(w, data)
}

func renderLogsTSVRunRows(w io.Writer, runs []RunData) error {
	for _, r := range runs {
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
		url := r.URL
		if idx := strings.Index(url, "/actions/runs/"); idx > 0 {
			url = url[idx:]
		}
		fields := []string{
			strconv.FormatInt(r.RunID, 10), r.WorkflowName, r.EngineID, conclusion, duration,
			strconv.Itoa(r.TokenUsage), formatTSVAIC(r.AIC), strconv.Itoa(r.Turns),
			strconv.Itoa(r.ErrorCount), r.Event, r.Branch, r.CreatedAt.Format("2006-01-02T15:04"),
			classification, url,
		}
		if err := writeTSVRow(w, fields); err != nil {
			return err
		}
	}
	return nil
}

func renderLogsTSVFooter(w io.Writer, data LogsData) error {
	if err := renderTSVInsights(w, data.Observability); err != nil {
		return err
	}
	if data.FirewallLog != nil && data.FirewallLog.TotalRequests > 0 {
		if _, err := fmt.Fprintf(w, "# firewall: %d requests (%d allowed, %d blocked)\n",
			data.FirewallLog.TotalRequests, data.FirewallLog.AllowedRequests, data.FirewallLog.BlockedRequests); err != nil {
			return err
		}
	}
	if len(data.Summary.EngineCounts) > 1 {
		parts := make([]string, 0, len(data.Summary.EngineCounts))
		for engine, count := range data.Summary.EngineCounts {
			parts = append(parts, fmt.Sprintf("%s:%d", engine, count))
		}
		if _, err := fmt.Fprintf(w, "# engines: %s\n", strings.Join(parts, " ")); err != nil {
			return err
		}
	}
	return nil
}

func renderTSVInsights(w io.Writer, insights []ObservabilityInsight) error {
	if len(insights) == 0 {
		return nil
	}
	if _, err := fmt.Fprintln(w, "# insights:"); err != nil {
		return err
	}
	for _, obs := range insights {
		if _, err := fmt.Fprintf(w, "# [%s] %s: %s\n", obs.Severity, obs.Title, obs.Summary); err != nil {
			return err
		}
	}
	return nil
}

func writeTSVRow(w io.Writer, fields []string) error {
	_, err := fmt.Fprintln(w, strings.Join(fields, "\t"))
	return err
}

// renderLogsTSV outputs the logs data as tab-separated values to os.Stdout.
func renderLogsTSV(data LogsData) error {
	return renderLogsTSVToWriter(os.Stdout, data)
}

// renderLogsTSVVerboseToWriter outputs a more detailed TSV with additional columns for audit use to w.
func renderLogsTSVVerboseToWriter(w io.Writer, data LogsData) error {
	logsTSVLog.Printf("Rendering %d runs as verbose TSV", data.Summary.TotalRuns)

	s := data.Summary
	if _, err := fmt.Fprintf(w, "# %d runs | %s duration | %s tokens | %d turns | %d errors | %d missing_tools | %d github_api_calls\n",
		s.TotalRuns, s.TotalDuration, formatTSVSummaryTokens(s.TotalTokens), s.TotalTurns, s.TotalErrors, s.TotalMissingTools, s.TotalGitHubAPICalls); err != nil {
		return err
	}
	if len(data.Runs) == 0 {
		return nil
	}
	if err := writeTSVRow(w, []string{
		"run_id", "workflow", "engine", "status", "duration",
		"tokens", "aic", "turns", "errors",
		"warnings", "missing_tools", "missing_data", "github_api",
		"event", "branch", "actor", "created_at", "tbt",
		"classification", "action_min", "display_title", "url",
	}); err != nil {
		return err
	}
	if err := renderLogsTSVVerboseRunRows(w, data.Runs); err != nil {
		return err
	}
	return renderTSVInsights(w, data.Observability)
}

func renderLogsTSVVerboseRunRows(w io.Writer, runs []RunData) error {
	for _, r := range runs {
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
			strconv.FormatInt(r.RunID, 10), r.WorkflowName, r.EngineID, conclusion, duration,
			strconv.Itoa(r.TokenUsage), formatTSVAIC(r.AIC), strconv.Itoa(r.Turns),
			strconv.Itoa(r.ErrorCount), strconv.Itoa(r.WarningCount), strconv.Itoa(r.MissingToolCount),
			strconv.Itoa(r.MissingDataCount), strconv.Itoa(r.GitHubAPICalls), r.Event, r.Branch,
			r.Actor, r.CreatedAt.Format("2006-01-02T15:04"), tbt, classification,
			fmt.Sprintf("%.0f", r.ActionMinutes), displayTitle, r.URL,
		}
		if err := writeTSVRow(w, fields); err != nil {
			return err
		}
	}
	return nil
}

// renderLogsTSVVerbose outputs a more detailed TSV with additional columns to os.Stdout.
func renderLogsTSVVerbose(data LogsData) error {
	return renderLogsTSVVerboseToWriter(os.Stdout, data)
}
