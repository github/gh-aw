package cli

// audit_render_output.go: rendering of the final audit report/output and
// post-processing of agent and firewall logs.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/fileutil"
)

// renderAuditReport builds and renders the audit report from a fully-populated processedRun.
// It is called both when serving from a cached run summary and after a fresh processing pass,
// ensuring that the two paths produce identical output.
func renderAuditReport(ctx context.Context, processedRun ProcessedRun, metrics LogMetrics, mcpToolUsage *MCPToolUsageData, opts AuditOptions) error {
	runID := processedRun.Run.DatabaseID
	runOutputDir := opts.OutputDir
	processedRun.Run.SafeItemsCount = len(extractCreatedItemsFromManifest(runOutputDir))
	auditData, ok := loadCachedAuditData(runOutputDir, processedRun.Run, auditCacheSourceFull)
	if !ok {
		auditData = buildRenderedAuditDataFromCache(ctx, processedRun, metrics, mcpToolUsage, runOutputDir, opts)
		auditData.CacheSource = auditCacheSourceFull
		if err := writeAuditData(runOutputDir, auditData); err != nil {
			return err
		}
	}
	if err := renderAuditOutput(auditData, runOutputDir, opts.JSONOutput, opts.Verbose); err != nil {
		return err
	}
	renderAuditGatewayMetrics(runOutputDir, opts.Verbose)
	renderAuditUnifiedTimeline(runOutputDir, opts.Verbose)
	parseAuditLogsIfRequested(runID, runOutputDir, opts)
	renderAuditCompletion(runOutputDir, opts.JSONOutput)
	return nil
}

func buildRenderedAuditDataFromCache(ctx context.Context, processedRun ProcessedRun, metrics LogMetrics, mcpToolUsage *MCPToolUsageData, runOutputDir string, opts AuditOptions) AuditData {
	auditData, ok := loadCachedAuditData(runOutputDir, processedRun.Run, auditCacheSourceLogs)
	if !ok {
		return buildRenderedAuditData(ctx, processedRun, metrics, mcpToolUsage, runOutputDir, opts)
	}
	createdItems := extractCreatedItemsFromManifest(runOutputDir)
	addAuditOutcomeSummary(ctx, &auditData, createdItems)
	currentSnapshot := buildAuditComparisonSnapshot(processedRun, createdItems)
	auditData.Comparison = buildAuditComparisonForRun(ctx, processedRun, currentSnapshot, runOutputDir, opts.Owner, opts.Repo, opts.Hostname, opts.Verbose)
	return auditData
}

func buildRenderedAuditData(ctx context.Context, processedRun ProcessedRun, metrics LogMetrics, mcpToolUsage *MCPToolUsageData, runOutputDir string, opts AuditOptions) AuditData {
	currentCreatedItems := extractCreatedItemsFromManifest(runOutputDir)
	currentSnapshot := buildAuditComparisonSnapshot(processedRun, currentCreatedItems)
	comparison := buildAuditComparisonForRun(ctx, processedRun, currentSnapshot, runOutputDir, opts.Owner, opts.Repo, opts.Hostname, opts.Verbose)
	auditData := buildAuditData(ctx, processedRun, metrics, mcpToolUsage)
	auditData.Comparison = comparison
	return auditData
}

func renderAuditOutput(auditData AuditData, runOutputDir string, jsonOutput, verbose bool) error {
	if jsonOutput {
		if err := renderJSON(auditData); err != nil {
			return fmt.Errorf("failed to render JSON output: %w", err)
		}
		return nil
	}
	renderConsole(auditData, runOutputDir)
	if verbose {
		auditLog.Printf("Rendered console audit report for %s", runOutputDir)
	}
	return nil
}

func renderAuditGatewayMetrics(runOutputDir string, verbose bool) {
	gatewayMetrics, err := parseGatewayLogs(runOutputDir, verbose)
	if err != nil {
		return
	}
	if metricsOutput := renderGatewayMetricsTable(gatewayMetrics, verbose); metricsOutput != "" {
		fmt.Fprint(os.Stderr, metricsOutput)
	}
}

// renderAuditUnifiedTimeline builds the unified event timeline from the run output
// directory (combining MCP Gateway, AWF firewall, and agent events) and writes the
// rendered table to stderr.  It is a no-op when no events can be collected.
func renderAuditUnifiedTimeline(runOutputDir string, verbose bool) {
	events, err := BuildUnifiedTimeline(runOutputDir, verbose)
	if err != nil {
		auditLog.Printf("BuildUnifiedTimeline error for %s: %v", runOutputDir, err)
		return
	}
	if output := renderUnifiedTimeline(events); output != "" {
		fmt.Fprint(os.Stderr, output)
	}
}

func parseAuditLogsIfRequested(runID int64, runOutputDir string, opts AuditOptions) {
	if !opts.Parse {
		return
	}
	parseAgentLogIfRequested(runID, runOutputDir, opts.Verbose)
	parseFirewallLogsIfRequested(runID, runOutputDir, opts.Verbose)
}

func parseAgentLogIfRequested(runID int64, runOutputDir string, verbose bool) {
	awInfoPath := filepath.Join(runOutputDir, "aw_info.json")
	engine := extractEngineFromAwInfo(awInfoPath, verbose)
	if engine == nil {
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("No engine detected (aw_info.json missing or invalid); skipping agent log rendering"))
		}
		return
	}
	if err := parseAgentLog(runOutputDir, engine, verbose); err != nil {
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to parse agent log for run %d: %v", runID, err)))
		}
		return
	}
	logMdPath := filepath.Join(runOutputDir, "log.md")
	if fileutil.FileExists(logMdPath) {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("✓ Parsed log for run %d → %s", runID, logMdPath)))
	}
}

func parseFirewallLogsIfRequested(runID int64, runOutputDir string, verbose bool) {
	if err := parseFirewallLogs(runOutputDir, verbose); err != nil {
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to parse firewall logs for run %d: %v", runID, err)))
		}
		return
	}
	firewallMdPath := filepath.Join(runOutputDir, "firewall.md")
	if fileutil.FileExists(firewallMdPath) {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("✓ Parsed firewall logs for run %d → %s", runID, firewallMdPath)))
	}
}

func renderAuditCompletion(runOutputDir string, jsonOutput bool) {
	if jsonOutput {
		return
	}
	absOutputDir, _ := filepath.Abs(runOutputDir)
	fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Audit complete. Logs saved to "+absOutputDir))
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Tip: use --artifacts to select specific artifact sets (agent, firewall, mcp, activation, detection, etc.)"))
}
