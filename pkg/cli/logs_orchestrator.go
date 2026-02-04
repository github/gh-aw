// This file provides command-line interface functionality for gh-aw.
// This file (logs_orchestrator.go) contains the main orchestration logic for downloading
// and processing workflow logs from GitHub Actions.
//
// Key responsibilities:
//   - Coordinating the main download workflow (DownloadWorkflowLogs)
//   - Managing pagination and iteration through workflow runs
//   - Concurrent downloading of artifacts from multiple runs
//   - Applying filters (engine, firewall, staged, etc.)
//   - Building and rendering output (console, JSON, tool graphs)

package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/envutil"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/workflow"
	"github.com/sourcegraph/conc/pool"
)

var logsOrchestratorLog = logger.New("cli:logs_orchestrator")

// getMaxConcurrentDownloads returns the maximum number of concurrent downloads.
// It reads from the GH_AW_MAX_CONCURRENT_DOWNLOADS environment variable if set,
// validates the value is between 1 and 100, and falls back to the default if invalid.
func getMaxConcurrentDownloads() int {
	return envutil.GetIntFromEnv("GH_AW_MAX_CONCURRENT_DOWNLOADS", MaxConcurrentDownloads, 1, 100, logsOrchestratorLog)
}

// DownloadWorkflowLogs downloads and analyzes workflow logs with metrics
func DownloadWorkflowLogs(ctx context.Context, workflowName string, count int, startDate, endDate, outputDir, engine, ref string, beforeRunID, afterRunID int64, repoOverride string, verbose bool, toolGraph bool, noStaged bool, firewallOnly bool, noFirewall bool, parse bool, jsonOutput bool, timeout int, summaryFile string, safeOutputType string) error {
	logsOrchestratorLog.Printf("Starting workflow log download: workflow=%s, count=%d, startDate=%s, endDate=%s, outputDir=%s, summaryFile=%s, safeOutputType=%s", workflowName, count, startDate, endDate, outputDir, summaryFile, safeOutputType)

	// Ensure .github/aw/logs/.gitignore exists on every invocation
	if err := ensureLogsGitignore(); err != nil {
		// Log but don't fail - this is not critical for downloading logs
		logsOrchestratorLog.Printf("Failed to ensure logs .gitignore: %v", err)
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to ensure .github/aw/logs/.gitignore: %v", err)))
		}
	}

	// Check context cancellation at the start
	select {
	case <-ctx.Done():
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Operation cancelled"))
		return ctx.Err()
	default:
	}

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Fetching workflow runs from GitHub Actions..."))
	}

	// Start timeout timer if specified
	var startTime time.Time
	var timeoutReached bool
	if timeout > 0 {
		startTime = time.Now()
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Timeout set to %d seconds", timeout)))
		}
	}

	var processedRuns []ProcessedRun
	var beforeDate string
	iteration := 0

	// Determine if we should fetch all runs (when date filters are specified) or limit by count
	// When date filters are specified, we fetch all runs within that range and apply count to final output
	// When no date filters, we fetch up to 'count' runs with artifacts (old behavior for backward compatibility)
	fetchAllInRange := startDate != "" || endDate != ""

	// Iterative algorithm: keep fetching runs until we have enough or exhaust available runs
	for iteration < MaxIterations {
		// Check context cancellation
		select {
		case <-ctx.Done():
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Operation cancelled"))
			return ctx.Err()
		default:
		}

		// Check timeout if specified
		if timeout > 0 {
			elapsed := time.Since(startTime).Seconds()
			if elapsed >= float64(timeout) {
				timeoutReached = true
				if verbose {
					fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Timeout reached after %.1f seconds, stopping download", elapsed)))
				}
				break
			}
		}

		// Stop if we've collected enough processed runs
		if len(processedRuns) >= count {
			break
		}

		iteration++

		if verbose && iteration > 1 {
			if fetchAllInRange {
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Iteration %d: Fetching more runs in date range...", iteration)))
			} else {
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Iteration %d: Need %d more runs with artifacts, fetching more...", iteration, count-len(processedRuns))))
			}
		}

		// Fetch a batch of runs
		batchSize := BatchSize
		if workflowName == "" {
			// When searching for all agentic workflows, use a larger batch size
			// since there may be many CI runs interspersed with agentic runs
			batchSize = BatchSizeForAllWorkflows
		}

		// When not fetching all in range, optimize batch size based on how many we still need
		if !fetchAllInRange && count-len(processedRuns) < batchSize {
			// If we need fewer runs than the batch size, request exactly what we need
			// but add some buffer since many runs might not have artifacts
			needed := count - len(processedRuns)
			batchSize = needed * 3 // Request 3x what we need to account for runs without artifacts
			if workflowName == "" && batchSize < BatchSizeForAllWorkflows {
				// For all-workflows search, maintain a minimum batch size
				batchSize = BatchSizeForAllWorkflows
			}
			if batchSize > BatchSizeForAllWorkflows {
				batchSize = BatchSizeForAllWorkflows
			}
		}

		runs, totalFetched, err := listWorkflowRunsWithPagination(ListWorkflowRunsOptions{
			WorkflowName:   workflowName,
			Limit:          batchSize,
			StartDate:      startDate,
			EndDate:        endDate,
			BeforeDate:     beforeDate,
			Ref:            ref,
			BeforeRunID:    beforeRunID,
			AfterRunID:     afterRunID,
			RepoOverride:   repoOverride,
			ProcessedCount: len(processedRuns),
			TargetCount:    count,
			Verbose:        verbose,
		})
		if err != nil {
			return err
		}

		if len(runs) == 0 {
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage("No more workflow runs found, stopping iteration"))
			}
			break
		}

		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Found %d workflow runs in batch %d", len(runs), iteration)))
		}

		// Process runs in chunks so cache hits can satisfy the count without
		// forcing us to scan the entire batch.
		batchProcessed := 0
		runsRemaining := runs
		for len(runsRemaining) > 0 && len(processedRuns) < count {
			remainingNeeded := count - len(processedRuns)
			if remainingNeeded <= 0 {
				break
			}

			// Process slightly more than we need to account for skips due to filters.
			chunkSize := remainingNeeded * 3
			if chunkSize < remainingNeeded {
				chunkSize = remainingNeeded
			}
			if chunkSize > len(runsRemaining) {
				chunkSize = len(runsRemaining)
			}

			chunk := runsRemaining[:chunkSize]
			runsRemaining = runsRemaining[chunkSize:]

			downloadResults := downloadRunArtifactsConcurrent(ctx, chunk, outputDir, verbose, remainingNeeded)

			for _, result := range downloadResults {
				if result.Skipped {
					if verbose {
						if result.Error != nil {
							fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Skipping run %d: %v", result.Run.DatabaseID, result.Error)))
						}
					}
					continue
				}

				if result.Error != nil {
					fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to download artifacts for run %d: %v", result.Run.DatabaseID, result.Error)))
					continue
				}

				// Parse aw_info.json once for all filters that need it (optimization)
				var awInfo *AwInfo
				var awInfoErr error
				awInfoPath := filepath.Join(result.LogsPath, "aw_info.json")

				// Only parse if we need it for any filter
				if engine != "" || noStaged || firewallOnly || noFirewall {
					awInfo, awInfoErr = parseAwInfo(awInfoPath, verbose)
				}

				// Apply engine filtering if specified
				if engine != "" {
					// Check if the run's engine matches the filter
					detectedEngine := extractEngineFromAwInfo(awInfoPath, verbose)

					var engineMatches bool
					if detectedEngine != nil {
						// Get the engine ID to compare with the filter
						registry := workflow.GetGlobalEngineRegistry()
						for _, supportedEngine := range constants.AgenticEngines {
							if testEngine, err := registry.GetEngine(supportedEngine); err == nil && testEngine == detectedEngine {
								engineMatches = (supportedEngine == engine)
								break
							}
						}
					}

					if !engineMatches {
						if verbose {
							engineName := "unknown"
							if detectedEngine != nil {
								// Try to get a readable name for the detected engine
								registry := workflow.GetGlobalEngineRegistry()
								for _, supportedEngine := range constants.AgenticEngines {
									if testEngine, err := registry.GetEngine(supportedEngine); err == nil && testEngine == detectedEngine {
										engineName = supportedEngine
										break
									}
								}
							}
							fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Skipping run %d: engine '%s' does not match filter '%s'", result.Run.DatabaseID, engineName, engine)))
						}
						continue
					}
				}

				// Apply staged filtering if --no-staged flag is specified
				if noStaged {
					var isStaged bool
					if awInfoErr == nil && awInfo != nil {
						isStaged = awInfo.Staged
					}

					if isStaged {
						if verbose {
							fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Skipping run %d: workflow is staged (filtered out by --no-staged)", result.Run.DatabaseID)))
						}
						continue
					}
				}

				// Apply firewall filtering if --firewall or --no-firewall flag is specified
				if firewallOnly || noFirewall {
					var hasFirewall bool
					if awInfoErr == nil && awInfo != nil {
						// Firewall is enabled if steps.firewall is non-empty (e.g., "squid")
						hasFirewall = awInfo.Steps.Firewall != ""
					}

					// Check if the run matches the filter
					if firewallOnly && !hasFirewall {
						if verbose {
							fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Skipping run %d: workflow does not use firewall (filtered by --firewall)", result.Run.DatabaseID)))
						}
						continue
					}
					if noFirewall && hasFirewall {
						if verbose {
							fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Skipping run %d: workflow uses firewall (filtered by --no-firewall)", result.Run.DatabaseID)))
						}
						continue
					}
				}

				// Apply safe output type filtering if --safe-output flag is specified
				if safeOutputType != "" {
					hasSafeOutputType, checkErr := runContainsSafeOutputType(result.LogsPath, safeOutputType, verbose)
					if checkErr != nil && verbose {
						fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to check safe output type for run %d: %v", result.Run.DatabaseID, checkErr)))
					}

					if !hasSafeOutputType {
						if verbose {
							fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Skipping run %d: no '%s' safe output messages found", result.Run.DatabaseID, safeOutputType)))
						}
						continue
					}
				}

				// Update run with metrics and path
				run := result.Run
				run.TokenUsage = result.Metrics.TokenUsage
				run.EstimatedCost = result.Metrics.EstimatedCost
				run.Turns = result.Metrics.Turns
				run.ErrorCount = 0
				run.WarningCount = 0
				run.LogsPath = result.LogsPath

				// Add failed jobs to error count
				if failedJobCount, err := fetchJobStatuses(run.DatabaseID, verbose); err == nil {
					run.ErrorCount += failedJobCount
					if verbose && failedJobCount > 0 {
						fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Added %d failed jobs to error count for run %d", failedJobCount, run.DatabaseID)))
					}
				}

				// Always use GitHub API timestamps for duration calculation
				if !run.StartedAt.IsZero() && !run.UpdatedAt.IsZero() {
					run.Duration = run.UpdatedAt.Sub(run.StartedAt)
				}

				processedRun := ProcessedRun{
					Run:                     run,
					AccessAnalysis:          result.AccessAnalysis,
					FirewallAnalysis:        result.FirewallAnalysis,
					RedactedDomainsAnalysis: result.RedactedDomainsAnalysis,
					MissingTools:            result.MissingTools,
					MissingData:             result.MissingData,
					Noops:                   result.Noops,
					MCPFailures:             result.MCPFailures,
					MCPToolUsage:            result.MCPToolUsage,
					JobDetails:              result.JobDetails,
				}
				processedRuns = append(processedRuns, processedRun)
				batchProcessed++

				// If --parse flag is set, parse the agent log and write to log.md
				if parse {
					// Get the engine from aw_info.json
					awInfoPath := filepath.Join(result.LogsPath, "aw_info.json")
					detectedEngine := extractEngineFromAwInfo(awInfoPath, verbose)

					if err := parseAgentLog(result.LogsPath, detectedEngine, verbose); err != nil {
						fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to parse log for run %d: %v", run.DatabaseID, err)))
					} else {
						// Always show success message for parsing, not just in verbose mode
						logMdPath := filepath.Join(result.LogsPath, "log.md")
						if _, err := os.Stat(logMdPath); err == nil {
							fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("✓ Parsed log for run %d → %s", run.DatabaseID, logMdPath)))
						}
					}

					// Also parse firewall logs if they exist
					if err := parseFirewallLogs(result.LogsPath, verbose); err != nil {
						fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to parse firewall logs for run %d: %v", run.DatabaseID, err)))
					} else {
						// Show success message if firewall.md was created
						firewallMdPath := filepath.Join(result.LogsPath, "firewall.md")
						if _, err := os.Stat(firewallMdPath); err == nil {
							fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("✓ Parsed firewall logs for run %d → %s", run.DatabaseID, firewallMdPath)))
						}
					}
				}

				// Stop processing this batch once we've collected enough runs.
				if len(processedRuns) >= count {
					break
				}
			}
		}

		if verbose {
			if fetchAllInRange {
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Processed %d runs with artifacts in batch %d (total: %d)", batchProcessed, iteration, len(processedRuns))))
			} else {
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Processed %d runs with artifacts in batch %d (total: %d/%d)", batchProcessed, iteration, len(processedRuns), count)))
			}
		}

		// Prepare for next iteration: set beforeDate to the oldest processed run from this batch
		if len(runs) > 0 && len(runsRemaining) == 0 {
			oldestRun := runs[len(runs)-1] // runs are typically ordered by creation date descending
			beforeDate = oldestRun.CreatedAt.Format(time.RFC3339)
		}

		// If we got fewer runs than requested in this batch, we've likely hit the end
		// IMPORTANT: Use totalFetched (API response size before filtering) not len(runs) (after filtering)
		// to detect end. When workflowName is empty, runs are filtered to only agentic workflows,
		// so len(runs) may be much smaller than totalFetched even when more data is available from GitHub.
		// Example: API returns 250 total runs, but only 5 are agentic workflows after filtering.
		//   Old buggy logic: len(runs)=5 < batchSize=250, stop iteration (WRONG - misses more agentic workflows!)
		//   Fixed logic: totalFetched=250 < batchSize=250 is false, continue iteration (CORRECT)
		if totalFetched < batchSize {
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Received fewer runs than requested, likely reached end of available runs"))
			}
			break
		}
	}

	// Check if we hit the maximum iterations limit
	if iteration >= MaxIterations {
		if fetchAllInRange {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Reached maximum iterations (%d), collected %d runs with artifacts", MaxIterations, len(processedRuns))))
		} else if len(processedRuns) < count {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Reached maximum iterations (%d), collected %d runs with artifacts out of %d requested", MaxIterations, len(processedRuns), count)))
		}
	}

	// Report if timeout was reached
	if timeoutReached && len(processedRuns) > 0 {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Timeout reached, returning %d processed runs", len(processedRuns))))
	}

	if len(processedRuns) == 0 {
		// When JSON output is requested, output JSON first to stdout before any stderr messages
		// This prevents stderr messages from corrupting JSON when both streams are redirected together
		if jsonOutput {
			logsData := buildLogsData([]ProcessedRun{}, outputDir, nil)
			if err := renderLogsJSON(logsData); err != nil {
				return fmt.Errorf("failed to render JSON output: %w", err)
			}
		}
		// Now print warning messages to stderr after JSON output (if any) is complete
		if timeoutReached {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Timeout reached before any runs could be downloaded"))
		} else {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage("No workflow runs with artifacts found matching the specified criteria"))
		}
		return nil
	}

	// Apply count limit to final results (truncate to count if we fetched more)
	if len(processedRuns) > count {
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Limiting output to %d most recent runs (fetched %d total)", count, len(processedRuns))))
		}
		processedRuns = processedRuns[:count]
	}

	// Update MissingToolCount, MissingDataCount, and NoopCount in runs
	for i := range processedRuns {
		processedRuns[i].Run.MissingToolCount = len(processedRuns[i].MissingTools)
		processedRuns[i].Run.MissingDataCount = len(processedRuns[i].MissingData)
		processedRuns[i].Run.NoopCount = len(processedRuns[i].Noops)
	}

	// Build continuation data if timeout was reached and there are processed runs
	var continuation *ContinuationData
	if timeoutReached && len(processedRuns) > 0 {
		// Get the oldest run ID from processed runs to use as before_run_id for continuation
		oldestRunID := processedRuns[len(processedRuns)-1].Run.DatabaseID

		continuation = &ContinuationData{
			Message:      "Timeout reached. Use these parameters to continue fetching more logs.",
			WorkflowName: workflowName,
			Count:        count,
			StartDate:    startDate,
			EndDate:      endDate,
			Engine:       engine,
			Branch:       ref,
			AfterRunID:   afterRunID,
			BeforeRunID:  oldestRunID, // Continue from where we left off
			Timeout:      timeout,
		}
	}

	// Build structured logs data
	logsData := buildLogsData(processedRuns, outputDir, continuation)

	// Write summary file if requested (default behavior unless disabled with empty string)
	if summaryFile != "" {
		summaryPath := filepath.Join(outputDir, summaryFile)
		if err := writeSummaryFile(summaryPath, logsData, verbose); err != nil {
			return fmt.Errorf("failed to write summary file: %w", err)
		}
	}

	// Render output based on format preference
	if jsonOutput {
		if err := renderLogsJSON(logsData); err != nil {
			return fmt.Errorf("failed to render JSON output: %w", err)
		}
	} else {
		renderLogsConsole(logsData)

		// Display aggregated gateway metrics if any runs have gateway.jsonl files
		displayAggregatedGatewayMetrics(processedRuns, outputDir, verbose)

		// Generate tool sequence graph if requested (console output only)
		if toolGraph {
			generateToolGraph(processedRuns, verbose)
		}
	}

	return nil
}

// downloadRunArtifactsConcurrent downloads artifacts for multiple workflow runs concurrently
func downloadRunArtifactsConcurrent(ctx context.Context, runs []WorkflowRun, outputDir string, verbose bool, maxRuns int) []DownloadResult {
	logsOrchestratorLog.Printf("Starting concurrent artifact download: runs=%d, outputDir=%s, maxRuns=%d", len(runs), outputDir, maxRuns)
	if len(runs) == 0 {
		return []DownloadResult{}
	}

	// Process all runs in the batch to account for caching and filtering
	// The maxRuns parameter indicates how many successful results we need, but we may need to
	// process more runs to account for:
	// 1. Cached runs that may fail filters (engine, firewall, etc.)
	// 2. Runs that may be skipped due to errors
	// 3. Runs without artifacts
	//
	// By processing all runs in the batch, we ensure that the count parameter correctly
	// reflects the number of matching logs (both downloaded and cached), not just attempts.
	actualRuns := runs

	totalRuns := len(actualRuns)

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Processing %d runs in parallel...", totalRuns)))
	}

	// Create progress bar for tracking run processing (only in non-verbose mode)
	var progressBar *console.ProgressBar
	if !verbose {
		progressBar = console.NewProgressBar(int64(totalRuns))
		fmt.Fprintf(os.Stderr, "Processing runs: %s\r", progressBar.Update(0))
	}

	// Use atomic counter for thread-safe progress tracking
	var completedCount int64

	// Get configured max concurrent downloads (default or from environment variable)
	maxConcurrent := getMaxConcurrentDownloads()

	// Configure concurrent download pool with bounded parallelism and context cancellation.
	// The conc pool automatically handles panic recovery and prevents goroutine leaks.
	// WithContext enables graceful cancellation via Ctrl+C.
	p := pool.NewWithResults[DownloadResult]().
		WithContext(ctx).
		WithMaxGoroutines(maxConcurrent)

	// Each download task runs concurrently with context awareness.
	// Context cancellation (e.g., via Ctrl+C) will stop all in-flight downloads gracefully.
	// Panics are automatically recovered by the pool and re-raised with full stack traces
	// after all tasks complete. This ensures one failing download doesn't break others.
	for _, run := range actualRuns {
		run := run // capture loop variable
		p.Go(func(ctx context.Context) (DownloadResult, error) {
			// Check for context cancellation before starting download
			select {
			case <-ctx.Done():
				return DownloadResult{
					Run:     run,
					Skipped: true,
					Error:   ctx.Err(),
				}, nil
			default:
			}
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Processing run %d (%s)...", run.DatabaseID, run.Status)))
			}

			// Download artifacts and logs for this run
			runOutputDir := filepath.Join(outputDir, fmt.Sprintf("run-%d", run.DatabaseID))

			// Try to load cached summary first
			if summary, ok := loadRunSummary(runOutputDir, verbose); ok {
				// Valid cached summary exists, use it directly
				result := DownloadResult{
					Run:                     summary.Run,
					Metrics:                 summary.Metrics,
					AccessAnalysis:          summary.AccessAnalysis,
					FirewallAnalysis:        summary.FirewallAnalysis,
					RedactedDomainsAnalysis: summary.RedactedDomainsAnalysis,
					MissingTools:            summary.MissingTools,
					MissingData:             summary.MissingData,
					Noops:                   summary.Noops,
					MCPFailures:             summary.MCPFailures,
					MCPToolUsage:            summary.MCPToolUsage,
					JobDetails:              summary.JobDetails,
					LogsPath:                runOutputDir,
					Cached:                  true, // Mark as cached
				}
				// Update progress counter
				completed := atomic.AddInt64(&completedCount, 1)
				if progressBar != nil {
					fmt.Fprintf(os.Stderr, "Processing runs: %s\r", progressBar.Update(completed))
				}
				return result, nil
			}

			// No cached summary or version mismatch - download and process
			err := downloadRunArtifacts(run.DatabaseID, runOutputDir, verbose)

			result := DownloadResult{
				Run:      run,
				LogsPath: runOutputDir,
			}

			if err != nil {
				// Check if this is a "no artifacts" case
				if errors.Is(err, ErrNoArtifacts) {
					// For runs with important conclusions (timed_out, failure, cancelled),
					// still process them even without artifacts to show the failure in reports
					if isFailureConclusion(run.Conclusion) {
						// Don't skip - we want these to appear in the report
						// Just use empty metrics
						result.Metrics = LogMetrics{}

						// Try to fetch job details to get error count
						if failedJobCount, jobErr := fetchJobStatuses(run.DatabaseID, verbose); jobErr == nil {
							run.ErrorCount = failedJobCount
						}
					} else {
						// For other runs (success, neutral, etc.) without artifacts, skip them
						result.Skipped = true
						result.Error = err
					}
				} else {
					result.Error = err
				}
			} else {
				// Extract metrics from logs
				metrics, metricsErr := extractLogMetrics(runOutputDir, verbose)
				if metricsErr != nil {
					if verbose {
						fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to extract metrics for run %d: %v", run.DatabaseID, metricsErr)))
					}
					// Don't fail the whole download for metrics errors
					metrics = LogMetrics{}
				}
				result.Metrics = metrics

				// Analyze access logs if available
				accessAnalysis, accessErr := analyzeAccessLogs(runOutputDir, verbose)
				if accessErr != nil {
					if verbose {
						fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to analyze access logs for run %d: %v", run.DatabaseID, accessErr)))
					}
				}
				result.AccessAnalysis = accessAnalysis

				// Analyze firewall logs if available
				firewallAnalysis, firewallErr := analyzeFirewallLogs(runOutputDir, verbose)
				if firewallErr != nil {
					if verbose {
						fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to analyze firewall logs for run %d: %v", run.DatabaseID, firewallErr)))
					}
				}
				result.FirewallAnalysis = firewallAnalysis

				// Analyze redacted domains if available
				redactedDomainsAnalysis, redactedErr := analyzeRedactedDomains(runOutputDir, verbose)
				if redactedErr != nil {
					if verbose {
						fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to analyze redacted domains for run %d: %v", run.DatabaseID, redactedErr)))
					}
				}
				result.RedactedDomainsAnalysis = redactedDomainsAnalysis

				// Extract missing tools if available
				missingTools, missingErr := extractMissingToolsFromRun(runOutputDir, run, verbose)
				if missingErr != nil {
					if verbose {
						fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to extract missing tools for run %d: %v", run.DatabaseID, missingErr)))
					}
				}
				result.MissingTools = missingTools

				// Extract missing data if available
				missingData, missingDataErr := extractMissingDataFromRun(runOutputDir, run, verbose)
				if missingDataErr != nil {
					if verbose {
						fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to extract missing data for run %d: %v", run.DatabaseID, missingDataErr)))
					}
				}
				result.MissingData = missingData

				// Extract noops if available
				noops, noopErr := extractNoopsFromRun(runOutputDir, run, verbose)
				if noopErr != nil {
					if verbose {
						fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to extract noops for run %d: %v", run.DatabaseID, noopErr)))
					}
				}
				result.Noops = noops

				// Extract MCP failures if available
				mcpFailures, mcpErr := extractMCPFailuresFromRun(runOutputDir, run, verbose)
				if mcpErr != nil {
					if verbose {
						fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to extract MCP failures for run %d: %v", run.DatabaseID, mcpErr)))
					}
				}
				result.MCPFailures = mcpFailures

				// Extract MCP tool usage data from gateway logs if available
				mcpToolUsage, mcpToolErr := extractMCPToolUsageData(runOutputDir, verbose)
				if mcpToolErr != nil {
					if verbose {
						fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to extract MCP tool usage for run %d: %v", run.DatabaseID, mcpToolErr)))
					}
				}
				result.MCPToolUsage = mcpToolUsage

				// Fetch job details for the summary
				jobDetails, jobErr := fetchJobDetails(run.DatabaseID, verbose)
				if jobErr != nil {
					if verbose {
						fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to fetch job details for run %d: %v", run.DatabaseID, jobErr)))
					}
				}

				// List all artifacts
				artifacts, listErr := listArtifacts(runOutputDir)
				if listErr != nil {
					if verbose {
						fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to list artifacts for run %d: %v", run.DatabaseID, listErr)))
					}
				}

				// Create and save run summary
				summary := &RunSummary{
					CLIVersion:              GetVersion(),
					RunID:                   run.DatabaseID,
					ProcessedAt:             time.Now(),
					Run:                     run,
					Metrics:                 metrics,
					AccessAnalysis:          accessAnalysis,
					FirewallAnalysis:        firewallAnalysis,
					RedactedDomainsAnalysis: redactedDomainsAnalysis,
					MissingTools:            missingTools,
					MissingData:             missingData,
					Noops:                   noops,
					MCPFailures:             mcpFailures,
					MCPToolUsage:            mcpToolUsage,
					ArtifactsList:           artifacts,
					JobDetails:              jobDetails,
				}

				if saveErr := saveRunSummary(runOutputDir, summary, verbose); saveErr != nil {
					if verbose {
						fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to save run summary for run %d: %v", run.DatabaseID, saveErr)))
					}
				}
			}

			// Update progress counter for completed downloads
			completed := atomic.AddInt64(&completedCount, 1)
			if progressBar != nil {
				fmt.Fprintf(os.Stderr, "Processing runs: %s\r", progressBar.Update(completed))
			}

			return result, nil
		})
	}

	// Wait blocks until all downloads complete, context is cancelled, or panic occurs.
	// With context support, the pool guarantees:
	// - All goroutines finish gracefully on cancellation (no leaks)
	// - Panics are propagated with stack traces
	// - Partial results are returned when context is cancelled
	// - Results are collected in submission order
	results, err := p.Wait()

	// Handle context cancellation
	if err != nil && verbose {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Download interrupted: %v", err)))
	}

	// Clear progress bar silently - detailed summary shown at the end
	if progressBar != nil {
		console.ClearLine() // Clear the line
	}

	if verbose {
		successCount := 0
		for _, result := range results {
			if result.Error == nil && !result.Skipped {
				successCount++
			}
		}
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("Completed parallel processing: %d successful, %d total", successCount, len(results))))
	}

	return results
}

// normalizeSafeOutputType converts dashes to underscores for matching
// This allows users to use either "missing-tool" or "missing_tool" interchangeably
func normalizeSafeOutputType(safeOutputType string) string {
	return strings.ReplaceAll(safeOutputType, "-", "_")
}

// runContainsSafeOutputType checks if a run's agent_output.json contains a specific safe output type
func runContainsSafeOutputType(runDir string, safeOutputType string, verbose bool) (bool, error) {
	// Normalize the type for comparison (convert dashes to underscores)
	normalizedType := normalizeSafeOutputType(safeOutputType)

	// Look for agent_output.json in the run directory
	agentOutputPath := filepath.Join(runDir, constants.AgentOutputFilename)

	// Support both new flattened form and old directory form
	if stat, err := os.Stat(agentOutputPath); err != nil || stat.IsDir() {
		// Try old structure
		oldPath := filepath.Join(runDir, constants.AgentOutputArtifactName, constants.AgentOutputArtifactName)
		if _, err := os.Stat(oldPath); err == nil {
			agentOutputPath = oldPath
		} else {
			// No agent_output.json found
			return false, nil
		}
	}

	// Read the file
	content, err := os.ReadFile(agentOutputPath)
	if err != nil {
		// File doesn't exist or can't be read
		return false, nil
	}

	// Parse the JSON
	var safeOutput struct {
		Items []json.RawMessage `json:"items"`
	}

	if err := json.Unmarshal(content, &safeOutput); err != nil {
		return false, fmt.Errorf("failed to parse agent_output.json: %w", err)
	}

	// Check each item for the specified type
	for _, itemRaw := range safeOutput.Items {
		var item struct {
			Type string `json:"type"`
		}

		if err := json.Unmarshal(itemRaw, &item); err != nil {
			continue // Skip malformed items
		}

		// Normalize the item type for comparison
		normalizedItemType := normalizeSafeOutputType(item.Type)

		if normalizedItemType == normalizedType {
			return true, nil
		}
	}

	return false, nil
}
