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

var logsCompactLog = logger.New("cli:logs_format_compact")

// workflowIDFromPath extracts the workflow ID from a workflow path.
// e.g. ".github/workflows/smoke-copilot.lock.yml" → "smoke-copilot"
func workflowIDFromPath(path string) string {
	// Get the base filename
	base := path
	if idx := strings.LastIndex(path, "/"); idx >= 0 {
		base = path[idx+1:]
	}
	// Strip .lock.yml suffix
	base = strings.TrimSuffix(base, ".lock.yml")
	// Strip .yml/.yaml suffix (in case it's not a lock file)
	base = strings.TrimSuffix(base, ".yml")
	base = strings.TrimSuffix(base, ".yaml")
	return base
}

// workflowIDFromRun returns the workflow ID preferring the path-derived ID,
// falling back to a lowercased/hyphenated version of the display name.
func workflowIDFromRun(path, name string) string {
	if id := workflowIDFromPath(path); id != "" {
		return id
	}
	// Normalize display name to kebab-case ID
	id := strings.ToLower(name)
	id = strings.ReplaceAll(id, " ", "-")
	return id
}

// renderLogsCompactToWriter outputs maximally information-dense output optimized for agentic
// consumption to w. Designed for LLM context windows: minimal formatting, no decoration,
// structured but flat.
//
// NOTE: w is always a plain-text writer (os.Stdout or a bytes.Buffer in tests).
// This function is never wired to an HTTP response, so HTML escaping is not applicable.
//
// Format sections:
//
//	[summary] key=value pairs on one line
//	[runs] aligned table with essential per-run metrics
//	[errors] one-line-per-error entries (only if errors exist)
//	[insights] observability insights (only medium/high severity)
//	[firewall] firewall summary with per-domain breakdown
//	[tools] top tool usage (only if present)
//	[mcp] MCP failures (only if present)
func renderLogsCompactToWriter(w io.Writer, data LogsData) {
	logsCompactLog.Printf("Rendering %d runs in compact format", data.Summary.TotalRuns)

	s := data.Summary

	// [summary] single line of key=value pairs
	summaryParts := []string{
		"runs=" + strconv.Itoa(s.TotalRuns),
		"duration=" + s.TotalDuration,
		"turns=" + strconv.Itoa(s.TotalTurns),
		"errors=" + strconv.Itoa(s.TotalErrors),
	}
	if s.TotalAIC > 0 {
		summaryParts = append(summaryParts, "aic="+formatCompactAIC(s.TotalAIC))
	}
	if s.TotalTokens > 0 {
		summaryParts = append(summaryParts, "tokens="+strconv.Itoa(s.TotalTokens))
	}
	if s.TotalWarnings > 0 {
		summaryParts = append(summaryParts, "warnings="+strconv.Itoa(s.TotalWarnings))
	}
	if s.TotalMissingTools > 0 {
		summaryParts = append(summaryParts, "missing_tools="+strconv.Itoa(s.TotalMissingTools))
	}
	if s.TotalGitHubAPICalls > 0 {
		summaryParts = append(summaryParts, "github_api="+strconv.Itoa(s.TotalGitHubAPICalls))
	}
	if len(s.EngineCounts) > 0 {
		parts := make([]string, 0, len(s.EngineCounts))
		for engine, count := range s.EngineCounts {
			parts = append(parts, engine+":"+strconv.Itoa(count))
		}
		summaryParts = append(summaryParts, "engines="+strings.Join(parts, ","))
	}
	// Outcome metrics if available
	if s.OutcomeAccepted > 0 || s.OutcomeRejected > 0 {
		summaryParts = append(summaryParts,
			"accepted="+strconv.Itoa(s.OutcomeAccepted),
			"rejected="+strconv.Itoa(s.OutcomeRejected),
		)
		if s.OutcomeAcceptanceRate > 0 {
			summaryParts = append(summaryParts, "acceptance="+fmt.Sprintf("%.0f%%", s.OutcomeAcceptanceRate*100))
		}
	}
	fmt.Fprintf(w, "[summary] %s\n", strings.Join(summaryParts, " "))

	if len(data.Runs) == 0 {
		return
	}

	// [runs] aligned table
	fmt.Fprintln(w, "[runs]")
	rows := make([][]string, 0, len(data.Runs))

	for _, r := range data.Runs {
		status := r.Conclusion
		if status == "" {
			status = r.Status
		}
		if status == "skipped" || status == "cancelled" {
			continue
		}
		dur := r.Duration
		if dur == "" {
			dur = "-"
		}
		branch := stringutil.Truncate(r.Branch, 30)
		actor := r.Actor
		if actor == "" {
			actor = "-"
		}
		wfID := workflowIDFromRun(r.WorkflowPath, r.WorkflowName)

		rows = append(rows, []string{
			strconv.FormatInt(r.RunID, 10), wfID, r.EngineID, status, dur,
			strconv.Itoa(r.TokenUsage), formatCompactAIC(r.AIC),
			strconv.Itoa(r.Turns), strconv.Itoa(r.ErrorCount),
			formatCompactWSRF(r.WSRF),
			r.Event, actor, branch,
		})
	}
	// RenderTable appends a trailing newline, so following section headers remain separated.
	fmt.Fprint(w, console.RenderTable(console.TableConfig{
		Headers: []string{"RUNID", "WORKFLOW", "ENGINE", "STATUS", "DUR", "TOKENS", "AIC", "TURNS", "ERR", "WSRF", "EVENT", "ACTOR", "BRANCH"},
		Rows:    rows,
	}))

	// [errors] — aggregated error/warning messages
	if len(data.ErrorsAndWarnings) > 0 {
		fmt.Fprintln(w, "[errors]")
		for _, ew := range data.ErrorsAndWarnings {
			msg := stringutil.Truncate(ew.Message, 120)
			fmt.Fprintf(w, "%s run=%d count=%d: %s\n", ew.Type, ew.RunID, ew.Count, msg)
		}
	}

	// [insights] — only medium/high severity (skip info-level noise)
	if len(data.Observability) > 0 {
		var hasActionable bool
		for _, obs := range data.Observability {
			if obs.Severity != "info" {
				hasActionable = true
				break
			}
		}
		if hasActionable {
			fmt.Fprintln(w, "[insights]")
			for _, obs := range data.Observability {
				if obs.Severity == "info" {
					continue
				}
				fmt.Fprintf(w, "[%s] %s: %s\n", obs.Severity, obs.Title, obs.Summary)
			}
		}
	}

	// [firewall] — summary + per-domain breakdown
	if data.FirewallLog != nil && data.FirewallLog.TotalRequests > 0 {
		fw := data.FirewallLog
		fmt.Fprintf(w, "[firewall] requests=%d allowed=%d blocked=%d\n",
			fw.TotalRequests, fw.AllowedRequests, fw.BlockedRequests)
		if len(fw.RequestsByDomain) > 0 {
			for domain, counts := range fw.RequestsByDomain {
				if counts.Blocked > 0 {
					fmt.Fprintf(w, "  %s allowed=%d blocked=%d\n", domain, counts.Allowed, counts.Blocked)
				}
			}
		} else if len(fw.BlockedDomains) > 0 {
			fmt.Fprintf(w, "  blocked: %s\n", strings.Join(fw.BlockedDomains, " "))
		}
	}

	// [tools] — top tools by call count
	if len(data.ToolUsage) > 0 {
		fmt.Fprintln(w, "[tools]")
		limit := min(10, len(data.ToolUsage))
		for i := range limit {
			t := data.ToolUsage[i]
			fmt.Fprintf(w, "%s calls=%d runs=%d\n", t.Name, t.TotalCalls, t.Runs)
		}
		if len(data.ToolUsage) > limit {
			fmt.Fprintf(w, "... +%d more tools\n", len(data.ToolUsage)-limit)
		}
	}

	// [mcp-failures]
	if len(data.MCPFailures) > 0 {
		fmt.Fprintln(w, "[mcp-failures]")
		for _, f := range data.MCPFailures {
			fmt.Fprintf(w, "server=%s count=%d runs=%v\n", f.ServerName, f.Count, f.RunIDs)
		}
	}

	// [missing-tools] — missing tool summary
	if len(data.MissingTools) > 0 {
		fmt.Fprintln(w, "[missing-tools]")
		for _, mt := range data.MissingTools {
			fmt.Fprintf(w, "%s count=%d runs=%v\n", mt.Tool, mt.Count, mt.RunIDs)
		}
	}

	// [location]
	renderLogsWorkQueueToWriter(w, data.Runs)
	if data.LogsLocation != "" {
		fmt.Fprintf(w, "[location] %s\n", data.LogsLocation)
	}

	// [hint] — dynamic artifact hint + static usage guidance rendered as a single line
	hint := "use --json for full details, -v for verbose, --format console for tables"
	if data.Message != "" {
		hint = data.Message + " " + hint
	}
	fmt.Fprintf(w, "[hint] %s\n", hint)
}

// renderLogsCompact outputs maximally information-dense output to os.Stdout.
func renderLogsCompact(data LogsData) {
	renderLogsCompactToWriter(os.Stdout, data)
}

// renderLogsCompactVerboseToWriter adds extra columns and sections for deeper analysis, writing to w.
func renderLogsCompactVerboseToWriter(w io.Writer, data LogsData) {
	logsCompactLog.Printf("Rendering %d runs in verbose compact format", data.Summary.TotalRuns)

	s := data.Summary

	// [summary] extended
	summaryParts := []string{
		"runs=" + strconv.Itoa(s.TotalRuns),
		"duration=" + s.TotalDuration,
		"action_min=" + fmt.Sprintf("%.1f", s.TotalActionMinutes),
		"turns=" + strconv.Itoa(s.TotalTurns),
		"errors=" + strconv.Itoa(s.TotalErrors),
		"warnings=" + strconv.Itoa(s.TotalWarnings),
		"missing_tools=" + strconv.Itoa(s.TotalMissingTools),
		"github_api=" + strconv.Itoa(s.TotalGitHubAPICalls),
		"episodes=" + strconv.Itoa(s.TotalEpisodes),
	}
	if s.TotalAIC > 0 {
		summaryParts = append(summaryParts, "aic="+formatCompactAIC(s.TotalAIC))
	}
	if len(s.EngineCounts) > 0 {
		parts := make([]string, 0, len(s.EngineCounts))
		for engine, count := range s.EngineCounts {
			parts = append(parts, engine+":"+strconv.Itoa(count))
		}
		summaryParts = append(summaryParts, "engines="+strings.Join(parts, ","))
	}
	if s.OutcomeAccepted > 0 || s.OutcomeRejected > 0 {
		summaryParts = append(summaryParts,
			"accepted="+strconv.Itoa(s.OutcomeAccepted),
			"rejected="+strconv.Itoa(s.OutcomeRejected),
			"ignored="+strconv.Itoa(s.OutcomeIgnored),
			"pending="+strconv.Itoa(s.OutcomePending),
		)
		if s.OutcomeAcceptanceRate > 0 {
			summaryParts = append(summaryParts, "acceptance="+fmt.Sprintf("%.0f%%", s.OutcomeAcceptanceRate*100))
		}
		if s.OutcomeWasteRate > 0 {
			summaryParts = append(summaryParts, "waste="+fmt.Sprintf("%.0f%%", s.OutcomeWasteRate*100))
		}
	}
	fmt.Fprintf(w, "[summary] %s\n", strings.Join(summaryParts, " "))

	if len(data.Runs) == 0 {
		return
	}

	// [runs] verbose aligned table
	fmt.Fprintln(w, "[runs]")
	rows := make([][]string, 0, len(data.Runs))

	for _, r := range data.Runs {
		status := r.Conclusion
		if status == "" {
			status = r.Status
		}
		if status == "skipped" || status == "cancelled" {
			continue
		}
		dur := r.Duration
		if dur == "" {
			dur = "-"
		}
		tbt := r.AvgTimeBetweenTurns
		if tbt == "" {
			tbt = "-"
		}
		classification := r.Classification
		if classification == "" {
			classification = "-"
		}
		actor := r.Actor
		if actor == "" {
			actor = "-"
		}
		wfID := workflowIDFromRun(r.WorkflowPath, r.WorkflowName)

		rows = append(rows, []string{
			strconv.FormatInt(r.RunID, 10), wfID, r.EngineID, status, dur,
			strconv.Itoa(r.TokenUsage), formatCompactAIC(r.AIC),
			strconv.Itoa(r.Turns), strconv.Itoa(r.ErrorCount), strconv.Itoa(r.WarningCount),
			formatCompactWSRF(r.WSRF),
			r.Event, actor, tbt, classification,
			r.CreatedAt.Format("01-02 15:04"), r.Branch,
		})
	}
	// RenderTable appends a trailing newline, so following section headers remain separated.
	fmt.Fprint(w, console.RenderTable(console.TableConfig{
		Headers: []string{"RUNID", "WORKFLOW", "ENGINE", "STATUS", "DUR", "TOKENS", "AIC", "TURNS", "ERR", "WARN", "WSRF", "EVENT", "ACTOR", "TBT", "CLASS", "CREATED", "BRANCH"},
		Rows:    rows,
	}))

	// [errors]
	if len(data.ErrorsAndWarnings) > 0 {
		fmt.Fprintln(w, "[errors]")
		for _, ew := range data.ErrorsAndWarnings {
			fmt.Fprintf(w, "%s run=%d count=%d: %s\n", ew.Type, ew.RunID, ew.Count, ew.Message)
		}
	}

	// [insights] — all severities in verbose mode
	if len(data.Observability) > 0 {
		fmt.Fprintln(w, "[insights]")
		for _, obs := range data.Observability {
			fmt.Fprintf(w, "[%s] %s: %s\n", obs.Severity, obs.Title, obs.Summary)
		}
	}

	// [firewall] — full breakdown
	if data.FirewallLog != nil && data.FirewallLog.TotalRequests > 0 {
		fw := data.FirewallLog
		fmt.Fprintf(w, "[firewall] requests=%d allowed=%d blocked=%d\n",
			fw.TotalRequests, fw.AllowedRequests, fw.BlockedRequests)
		if len(fw.RequestsByDomain) > 0 {
			for domain, counts := range fw.RequestsByDomain {
				fmt.Fprintf(w, "  %s allowed=%d blocked=%d\n", domain, counts.Allowed, counts.Blocked)
			}
		}
	}

	// [tools]
	if len(data.ToolUsage) > 0 {
		fmt.Fprintln(w, "[tools]")
		for _, t := range data.ToolUsage {
			fmt.Fprintf(w, "%s calls=%d runs=%d\n", t.Name, t.TotalCalls, t.Runs)
		}
	}

	// [mcp-tools]
	if data.MCPToolUsage != nil && len(data.MCPToolUsage.Summary) > 0 {
		fmt.Fprintln(w, "[mcp-tools]")
		for _, t := range data.MCPToolUsage.Summary {
			fmt.Fprintf(w, "%s.%s calls=%d\n", t.ServerName, t.ToolName, t.CallCount)
		}
	}

	// [mcp-failures]
	if len(data.MCPFailures) > 0 {
		fmt.Fprintln(w, "[mcp-failures]")
		for _, f := range data.MCPFailures {
			fmt.Fprintf(w, "server=%s count=%d runs=%v\n", f.ServerName, f.Count, f.RunIDs)
		}
	}

	// [missing-tools]
	if len(data.MissingTools) > 0 {
		fmt.Fprintln(w, "[missing-tools]")
		for _, mt := range data.MissingTools {
			fmt.Fprintf(w, "%s count=%d runs=%v\n", mt.Tool, mt.Count, mt.RunIDs)
		}
	}

	// [episodes]
	if len(data.Episodes) > 0 {
		fmt.Fprintln(w, "[episodes]")
		for _, ep := range data.Episodes {
			fmt.Fprintf(w, "%s runs=%d conf=%s duration=%s\n",
				ep.Kind, ep.TotalRuns, ep.Confidence, ep.TotalDuration)
		}
	}

	renderLogsWorkQueueToWriter(w, data.Runs)

	// [location]
	if data.LogsLocation != "" {
		fmt.Fprintf(w, "[location] %s\n", data.LogsLocation)
	}
}

// renderLogsCompactVerbose adds extra columns and sections for deeper analysis, writing to os.Stdout.
func renderLogsCompactVerbose(data LogsData) {
	renderLogsCompactVerboseToWriter(os.Stdout, data)
}

// formatCompactWSRF returns a table-ready cell value for the Working-Set
// Rebuild Factor, falling back to "-" when the metric was not measured.
func formatCompactWSRF(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

func formatCompactAIC(value float64) string {
	if value <= 0 {
		return "-"
	}
	if value >= 1000 {
		return fmt.Sprintf("%.1fK", value/1000)
	}
	if value >= 10 {
		return fmt.Sprintf("%.1f", value)
	}
	if value >= 1 {
		return fmt.Sprintf("%.2f", value)
	}
	return fmt.Sprintf("%.3f", value)
}
