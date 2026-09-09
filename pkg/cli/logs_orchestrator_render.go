// This file provides command-line interface functionality for gh-aw.
// This file (logs_orchestrator_render.go) contains output-rendering helpers for the
// logs orchestrator: transforming []ProcessedRun into console, JSON, markdown,
// TSV, or "pretty" cross-run audit output.

package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"golang.org/x/sync/errgroup"
)

// renderLogsOutput finalizes processedRuns and renders them in the appropriate output
// format: JSON, console metrics table, or cross-run audit report (pretty/markdown).
// continuation is optional and only set when a timeout was reached during a paginated download.
func renderLogsOutput(processedRuns []ProcessedRun, opts renderLogsOutputOptions) error {
	logsData, err := prepareLogsData(processedRuns, opts)
	if err != nil {
		return err
	}

	// Render output based on format preference.
	if opts.suppressRender {
		return nil
	}
	switch opts.format {
	case "tsv":
		return renderLogsOutputTSV(logsData, opts.verbose)
	case "markdown", "pretty":
		return renderLogsOutputCrossRun(processedRuns, logsData, opts)
	case "console":
		return renderLogsOutputConsole(processedRuns, logsData, opts)
	}

	// Default: compact format optimised for agentic consumption
	if opts.jsonOutput {
		if err := renderLogsJSON(logsData, opts.verbose); err != nil {
			return fmt.Errorf("failed to render JSON output: %w", err)
		}
	} else {
		if opts.verbose {
			renderLogsCompactVerbose(logsData)
		} else {
			renderLogsCompact(logsData)
		}
	}

	return nil
}

// prepareLogsData builds the LogsData structure and performs pre-render steps
// (staleness checks, summary writing, drain3 training).
func prepareLogsData(processedRuns []ProcessedRun, opts renderLogsOutputOptions) (LogsData, error) {
	// Update MissingToolCount, MissingDataCount, and NoopCount in runs
	for i := range processedRuns {
		processedRuns[i].Run.MissingToolCount = len(processedRuns[i].MissingTools)
		processedRuns[i].Run.MissingDataCount = len(processedRuns[i].MissingData)
		processedRuns[i].Run.NoopCount = len(processedRuns[i].Noops)
	}
	if opts.audit {
		if err := writeLogsAuditsAndTrain(processedRuns, opts); err != nil {
			return LogsData{}, fmt.Errorf("audit log pattern training: %w", err)
		}
	}

	// Build structured logs data
	logsOrchestratorLog.Printf("Building logs data from %d processed runs (continuation=%t)", len(processedRuns), opts.continuation != nil)
	logsData := buildLogsData(processedRuns, opts.outputDir, opts.continuation)
	logsData.Continuations = opts.continuations
	logsData.GitHubAPIRateLimit = populatedGitHubAPIRateLimitReport(opts.apiRateLimit)
	logsData.GitHubAPIRateLimits = populatedGitHubAPIRateLimitReports(opts.apiRateLimits)

	// When no explicit start_date/end_date was requested and the newest run in the
	// result is unexpectedly old, warn the caller so stale data is never served
	// silently (see issue: logs MCP tool returns stale data without date params).
	if opts.checkStaleness {
		if warning := staleLogsWarning(processedRuns, opts.startDate, opts.endDate); warning != "" {
			logsData.StaleWarning = warning
		} else if warning := dateRangeCoverageWarning(processedRuns, opts.startDate, opts.endDate, opts.countLimitReached); warning != "" {
			logsData.StaleWarning = warning
		}
	}

	// When only the usage artifact was downloaded, add a hint so consumers know how
	// to fetch additional artifact sets (agent logs, firewall data, etc.).
	var hints []string
	if opts.message != "" {
		hints = append(hints, opts.message)
	}
	if isUsageOnlyArtifactFilter(opts.artifactFilter) {
		hints = append(hints, usageOnlyArtifactHintMessage())
	}
	if len(hints) > 0 {
		logsData.Message = strings.Join(hints, " ")
	}

	// Write summary file if requested
	if opts.summaryFile != "" {
		summaryPath := filepath.Join(opts.outputDir, opts.summaryFile)
		if err := writeSummaryFile(summaryPath, logsData, opts.verbose); err != nil {
			return logsData, fmt.Errorf("failed to write summary file: %w", err)
		}
	}

	// Train drain3 weights if requested, or inline with audit generation.
	if opts.train && !opts.audit {
		if err := TrainDrain3Weights(processedRuns, opts.outputDir, opts.verbose); err != nil {
			return logsData, fmt.Errorf("log pattern training: %w", err)
		}
	}

	return logsData, nil
}

func writeLogsAuditsAndTrain(processedRuns []ProcessedRun, opts renderLogsOutputOptions) error {
	var group errgroup.Group
	group.Go(func() error {
		writeLogsAuditFiles(processedRuns, opts.verbose)
		return nil
	})
	group.Go(func() error {
		return TrainDrain3Weights(processedRuns, opts.outputDir, opts.verbose)
	})
	return group.Wait()
}

func renderLogsOutputTSV(logsData LogsData, verbose bool) error {
	if verbose {
		renderLogsTSVVerbose(logsData)
	} else {
		renderLogsTSV(logsData)
	}
	renderLogsArtifactHint(os.Stderr, logsData.Message)
	return nil
}

func renderLogsOutputCrossRun(processedRuns []ProcessedRun, logsData LogsData, opts renderLogsOutputOptions) error {
	inputs := processedRunsToCrossRunInputs(processedRuns)
	report := buildCrossRunAuditReport(inputs)
	report.GitHubAPIRateLimit = logsData.GitHubAPIRateLimit
	report.GitHubAPIRateLimits = logsData.GitHubAPIRateLimits
	if opts.jsonOutput {
		return renderCrossRunReportJSON(report)
	}
	if opts.format == "pretty" {
		renderCrossRunReportPretty(report)
		renderLogsArtifactHint(os.Stderr, logsData.Message)
		return nil
	}
	if opts.reportFile != "" {
		if err := os.MkdirAll(filepath.Dir(opts.reportFile), constants.DirPermPublic); err != nil {
			return fmt.Errorf("failed to create report file directory: %w", err)
		}
		f, err := os.Create(opts.reportFile)
		if err != nil {
			return fmt.Errorf("failed to create report file: %w", err)
		}
		if err := func() (retErr error) {
			defer func() {
				if cerr := f.Close(); cerr != nil && retErr == nil {
					retErr = cerr
				}
			}()
			renderCrossRunReportMarkdownToWriter(f, report)
			return nil
		}(); err != nil {
			return fmt.Errorf("failed to write report file: %w", err)
		}
	} else {
		renderCrossRunReportMarkdown(report)
	}
	renderLogsArtifactHint(os.Stderr, logsData.Message)
	return nil
}

func renderLogsOutputConsole(processedRuns []ProcessedRun, logsData LogsData, opts renderLogsOutputOptions) error {
	if opts.jsonOutput {
		if err := renderLogsJSON(logsData, opts.verbose); err != nil {
			return fmt.Errorf("failed to render JSON output: %w", err)
		}
	} else {
		renderLogsConsole(logsData)
		displayAggregatedGatewayMetrics(processedRuns, opts.outputDir, opts.verbose)
		displayUnifiedTimeline(processedRuns, opts.verbose)
		if opts.toolGraph {
			generateToolGraph(processedRuns, opts.verbose)
		}
		renderLogsArtifactHint(os.Stderr, logsData.Message)
	}
	return nil
}

// renderLogsArtifactHint writes a [hint] line to w when message is non-empty.
func renderLogsArtifactHint(w io.Writer, message string) {
	if message == "" {
		return
	}
	fmt.Fprintf(w, "[hint] %s\n", message)
}

// processedRunsToCrossRunInputs converts ProcessedRun slices to crossRunInput slices
// for cross-run aggregation.
func processedRunsToCrossRunInputs(processedRuns []ProcessedRun) []crossRunInput {
	inputs := make([]crossRunInput, 0, len(processedRuns))
	for _, pr := range processedRuns {
		inputs = append(inputs, crossRunInput{
			RunID:               pr.Run.DatabaseID,
			WorkflowName:        pr.Run.WorkflowName,
			Conclusion:          pr.Run.Conclusion,
			Duration:            pr.Run.Duration,
			FirewallAnalysis:    pr.FirewallAnalysis,
			Metrics:             LogMetrics{TokenUsage: pr.Run.TokenUsage, Turns: pr.Run.Turns},
			MCPToolUsage:        pr.MCPToolUsage,
			MCPFailures:         pr.MCPFailures,
			ErrorCount:          pr.Run.ErrorCount,
			TaskDomain:          pr.TaskDomain,
			BehaviorFingerprint: pr.BehaviorFingerprint,
			GradersCluster:      deriveGradersClusterValue(pr.Run.LogsPath),
			EvalsCluster:        deriveEvalsClusterValue(pr.Run.LogsPath),
		})
	}
	return inputs
}

func deriveGradersClusterValue(logsPath string) string {
	graders := extractGradersData(logsPath)
	if graders == nil || graders.Total == 0 {
		return "absent"
	}
	switch graders.Total {
	case graders.Passed:
		return "pass"
	case graders.Failed:
		return "fail"
	case graders.ErrorCount:
		return "error"
	case graders.UnavailableCount:
		return "unavailable"
	}
	return "mixed"
}

func deriveEvalsClusterValue(logsPath string) string {
	if runHasEvals(logsPath, false) {
		return "present"
	}
	return "absent"
}
