// This file provides command-line interface functionality for gh-aw.
// This file (logs_orchestrator.go) contains the main orchestration logic for downloading
// and processing workflow logs from GitHub Actions.
//
// Key responsibilities:
//   - Coordinating the main download workflow (DownloadWorkflowLogs)
//   - Managing pagination and iteration through workflow runs
//   - Applying filters (engine, firewall, staged, etc.)
//   - Building and rendering output (console, JSON, tool graphs)

package cli

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/github/gh-aw/pkg/envutil"
	"github.com/github/gh-aw/pkg/logger"
)

var logsOrchestratorLog = logger.New("cli:logs_orchestrator")

// isDeadlineExceeded reports whether ctx.Err() is context.DeadlineExceeded,
// returning false for any other error (including nil).  It is used to
// distinguish our own timeout cancellation (graceful partial results) from a
// user-initiated cancellation or other error.
func isDeadlineExceeded(ctx context.Context) bool {
	// errors.Is handles nil gracefully (returns false), so no nil check needed.
	return errors.Is(ctx.Err(), context.DeadlineExceeded)
}

// applyMetricsTurnsToRun sets run.Turns from metrics when a log-derived count is
// available. It deliberately does NOT overwrite when metrics.Turns is zero so that
// a backfilled value from applyUsageActivitySummaryToResult (session.turns) is
// preserved for usage-only artifact downloads where events.jsonl/.log are absent.
func applyMetricsTurnsToRun(run *WorkflowRun, metrics LogMetrics) {
	if metrics.Turns > 0 {
		run.Turns = metrics.Turns
	}
}

// staleLogsWarningThreshold is how old the most recent run in a result set may be,
// when no explicit start_date/end_date was requested, before we warn the caller
// that the data may not reflect the truly latest workflow runs.
//
// When no date range is supplied, pagination walks backwards through time (paging
// on run creation date) until it has collected the requested count of runs. In a
// repository with heavy non-agentic or still-in-progress run volume, this walk can
// silently settle on an old window without any indication to the caller that more
// recent runs exist. Passing an explicit start_date/end_date bypasses this ambiguity
// entirely because it bounds the query server-side, so no warning is needed there.
const staleLogsWarningThreshold = 48 * time.Hour

// humanizeDuration formats a duration as a coarse, human-readable age such as
// "11 days" or "5 hours", rather than a raw Go duration string like "264h0m0s".
func humanizeDuration(d time.Duration) string {
	if days := int(d.Hours()) / 24; days >= 1 {
		if days == 1 {
			return "1 day"
		}
		return fmt.Sprintf("%d days", days)
	}
	if hours := int(d.Hours()); hours >= 1 {
		if hours == 1 {
			return "1 hour"
		}
		return fmt.Sprintf("%d hours", hours)
	}
	return "less than 1 hour"
}

// staleLogsWarning returns a warning message when a date-unbounded logs query
// (no start_date/end_date requested) returns a result set whose most recent run
// is unexpectedly old. Returns "" when no warning is warranted, i.e. when an
// explicit date range was requested, there are no runs, or the newest run is
// recent enough.
func staleLogsWarning(processedRuns []ProcessedRun, startDate, endDate string) string {
	if startDate != "" || endDate != "" {
		// Caller supplied an explicit bound; the result is exactly what was asked for.
		return ""
	}
	if len(processedRuns) == 0 {
		return ""
	}
	var newest time.Time
	for _, pr := range processedRuns {
		if pr.Run.CreatedAt.After(newest) {
			newest = pr.Run.CreatedAt
		}
	}
	if newest.IsZero() {
		return ""
	}
	age := time.Since(newest)
	if age < staleLogsWarningThreshold {
		return ""
	}
	return fmt.Sprintf(
		"No start_date/end_date was specified, and the most recent run in this result is %s old (created %s). "+
			"Retry with an explicit start_date (e.g. \"-1d\") to confirm you are seeing the latest workflow runs.",
		humanizeDuration(age), newest.Format(time.RFC3339))
}

// dateRangeCoverageMinFraction is the minimum fraction of the requested
// start_date/end_date window that returned runs must span before a partial
// (count-limit-truncated) result is considered a reasonable sample of the
// requested range. Below this fraction, callers are warned that the result is
// a narrow slice of the range, not a representative multi-day sample.
const dateRangeCoverageMinFraction = 0.2

// dateRangeCoverageWarning returns a warning when an explicit start_date/end_date
// range was requested, the result was truncated before the range was fully
// scanned (partial=true, i.e. a continuation cursor was produced), and the runs
// actually returned span only a small fraction of the requested window. Without
// this warning, a caller can mistake a single busy (and possibly old) day for a
// representative sample of a much wider requested range, silently invalidating
// multi-day trend analysis built on top of it (see github/gh-aw#53995).
// Returns "" when no warning is warranted.
func dateRangeCoverageWarning(processedRuns []ProcessedRun, startDate, endDate string, partial bool) string {
	if !partial || startDate == "" || len(processedRuns) == 0 {
		return ""
	}
	start, err := parseFilterDate(startDate)
	if err != nil {
		return ""
	}
	end := time.Now()
	if endDate != "" {
		if parsedEnd, err := parseFilterDate(endDate); err == nil {
			end = parsedEnd
		}
	}
	requestedSpan := end.Sub(start)
	if requestedSpan <= 0 {
		return ""
	}
	// A single returned run trivially has a zero-length covered span
	// (newest.Sub(oldest) == 0), which would always look like a narrow slice
	// even when that one run legitimately falls within the requested window.
	// Require at least two runs before computing a meaningful coverage ratio.
	if len(processedRuns) < 2 {
		return ""
	}
	var oldest, newest time.Time
	for _, pr := range processedRuns {
		created := pr.Run.CreatedAt
		if oldest.IsZero() || created.Before(oldest) {
			oldest = created
		}
		if created.After(newest) {
			newest = created
		}
	}
	coveredSpan := newest.Sub(oldest)
	if coveredSpan.Seconds()/requestedSpan.Seconds() >= dateRangeCoverageMinFraction {
		return ""
	}
	return fmt.Sprintf(
		"An explicit date range was requested (%s), but the count limit was reached after collecting runs spanning only %s "+
			"(from %s to %s). This is a narrow slice of the requested range and may not represent overall trends. "+
			"Use the continuation cursor to fetch the remaining time range, or increase 'count' to cover the full window.",
		humanizeDuration(requestedSpan), humanizeDuration(coveredSpan), oldest.Format(time.RFC3339), newest.Format(time.RFC3339))
}

// noRunsMessage returns a human-readable explanation for why zero workflow runs
// were returned.  It inspects the startDate filter and the timeoutReached flag
// so callers receive actionable guidance instead of a silent empty result.
//
// Priority order (timeout is checked first because it is the most definitive
// cause — the date filter may still be valid but no data was collected):
//  1. Timeout – the download was cut short before any run was collected.
//  2. Future start date – GitHub cannot have runs in the future.
//  3. Start date older than GitHubActionsRetentionDays – beyond GitHub's default retention window.
//  4. Generic fallback for any other combination of filters.
func noRunsMessage(startDate string, timeoutReached, storageLimitReached bool) string {
	if timeoutReached {
		return "No runs found. Timeout reached before any runs could be downloaded."
	}
	if storageLimitReached {
		return "No runs found. Storage limit reached before any new runs could be downloaded."
	}
	if startDate != "" {
		if t, err := parseFilterDate(startDate); err == nil {
			now := time.Now()
			if t.After(now) {
				return fmt.Sprintf("No runs found. The start_date %q is in the future.", startDate)
			}
			// GitHub Actions retains logs for GitHubActionsRetentionDays by default.
			if t.Before(now.AddDate(0, 0, -GitHubActionsRetentionDays)) {
				return fmt.Sprintf("No runs found. Data may not be available beyond the %d-day retention period.", GitHubActionsRetentionDays)
			}
		}
	}
	return "No runs found matching the specified criteria."
}

// parseFilterDate tries to parse a date or datetime string in the formats used
// by the logs command's --start-date / --end-date flags after date resolution.
// Both plain dates ("2006-01-02") and RFC 3339 timestamps are accepted.
func parseFilterDate(s string) (time.Time, error) {
	for _, layout := range []string{time.RFC3339, "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unable to parse date %q", s)
}

// It reads from the GH_AW_MAX_CONCURRENT_DOWNLOADS environment variable if set,
// validates the value is between 1 and 100, and falls back to the default if invalid.
func getMaxConcurrentDownloads() int {
	return envutil.GetIntFromEnv("GH_AW_MAX_CONCURRENT_DOWNLOADS", MaxConcurrentDownloads, 1, 100, logsOrchestratorLog)
}

func shouldStopPagination(totalFetched, batchSize int) bool {
	return totalFetched < batchSize
}

func selectPaginationCursorDate(filteredRuns []WorkflowRun, oldestFetchedCreatedAt time.Time) (string, bool) {
	if !oldestFetchedCreatedAt.IsZero() {
		return oldestFetchedCreatedAt.Format(time.RFC3339), true
	}
	if len(filteredRuns) == 0 {
		return "", false
	}
	return filteredRuns[len(filteredRuns)-1].CreatedAt.Format(time.RFC3339), true
}

// buildContinuationIfNeeded returns a ContinuationData cursor when more runs may
// be available after this batch, or nil if the full result set was collected.
//
// A continuation is emitted in two cases:
//   - timeoutReached: the caller's timeout expired mid-download; runs beyond the
//     deadline were not fetched and may still exist.
//   - countLimitReached: in fetchAllInRange mode the count cap was hit before the
//     date window was exhausted; the next page starts just before the oldest run
//     returned in this batch.
func buildContinuationIfNeeded(
	processedRuns []ProcessedRun,
	timeoutReached, countLimitReached, storageLimitReached bool,
	opts continuationOptions,
) *ContinuationData {
	if !timeoutReached && !countLimitReached && !storageLimitReached {
		return nil
	}
	if len(processedRuns) == 0 && !storageLimitReached {
		return nil
	}
	// Use the oldest processed run as the before_run_id cursor for the next page.
	// When storage prevented any progress in this batch, fall back to the incoming
	// before_run_id instead of resetting it to zero: a zero cursor re-scans from the
	// newest run again, which re-triggers the same storage limit with zero progress
	// and never advances (see github/gh-aw#58022).
	oldestRunID := opts.previousBeforeRunID
	if len(processedRuns) > 0 {
		oldestRunID = processedRuns[len(processedRuns)-1].Run.DatabaseID
	}
	// Prefer the actual pagination date cursor over the fixed request end_date: when
	// many non-matching runs are interspersed across the window (the scenario this
	// guards against), the oldest *matching* run can be far newer than the point the
	// scan actually reached. Using before_run_id alone in that case makes a resumed
	// request re-fetch pages already scanned (from end_date/now down to oldestRunID),
	// wasting iterations and potentially exhausting them again with zero new matches
	// and no further continuation (see github/gh-aw#54110). Persisting the real
	// fetch cursor as end_date bounds the resumed query server-side instead.
	endDate := opts.endDate
	if opts.lastFetchedBeforeDate != "" {
		endDate = opts.lastFetchedBeforeDate
	}
	logsOrchestratorLog.Printf("Building continuation cursor: before_run_id=%d, end_date=%s, timeoutReached=%v, countLimitReached=%v", oldestRunID, endDate, timeoutReached, countLimitReached)
	message := "Timeout reached. Use these parameters to continue fetching more logs."
	if countLimitReached {
		// In fetchAllInRange mode the date window may contain more runs than count.
		message = "Count limit reached. Use these parameters to continue fetching more logs from the same date range."
	} else if storageLimitReached {
		message = "Storage limit reached. Use these parameters to continue fetching more logs after freeing space or changing max_storage."
	}
	return &ContinuationData{
		Message:               message,
		WorkflowName:          opts.workflowName,
		Count:                 opts.count,
		StartDate:             opts.startDate,
		EndDate:               endDate,
		Engine:                opts.engine,
		Branch:                opts.branch,
		AfterRunID:            opts.afterRunID,
		BeforeRunID:           oldestRunID,
		Timeout:               opts.timeoutMinutes,
		MaxGitHubAPIRateLimit: opts.maxGitHubAPIRateLimit,
		MaxStorageMB:          opts.maxStorageMB,
		PruneOlderRuns:        opts.pruneOlderRuns,
	}
}

// DownloadWorkflowLogs downloads and analyzes workflow logs with metrics
func DownloadWorkflowLogs(ctx context.Context, opts LogsDownloadOptions) error {
	logsOrchestratorLog.Printf("Downloading workflow logs: workflow=%q, count=%d, outputDir=%q", opts.WorkflowName, opts.Count, opts.OutputDir)
	apiRateLimit := startGitHubAPIRateLimitReport(ctx, logsRateLimitHost(opts.RepoOverride))
	result, err := collectWorkflowLogs(ctx, opts)
	if err != nil {
		return err
	}
	finishGitHubAPIRateLimitReport(ctx, apiRateLimit, opts.JSONOutput)
	if handled, err := handleEmptyProcessedRuns(result.processedRuns, opts, result.timeoutReached, result.storageLimitReached, result.continuation, nil, apiRateLimit, nil); handled || err != nil {
		logsOrchestratorLog.Printf("No processed runs to render (timeoutReached=%v, err=%v)", result.timeoutReached, err)
		return err
	}

	return renderLogsOutput(result.processedRuns, renderLogsOutputOptions{
		outputDir:         opts.OutputDir,
		summaryFile:       opts.SummaryFile,
		format:            opts.Format,
		reportFile:        opts.ReportFile,
		jsonOutput:        opts.JSONOutput,
		toolGraph:         opts.ToolGraph,
		train:             opts.Train,
		audit:             opts.Audit,
		continuation:      result.continuation,
		verbose:           opts.Verbose,
		artifactFilter:    result.artifactFilter,
		startDate:         opts.StartDate,
		endDate:           opts.EndDate,
		checkStaleness:    true,
		countLimitReached: result.countLimitReached,
		suppressRender:    opts.SuppressRender,
		apiRateLimit:      apiRateLimit,
	})
}

func collectWorkflowLogs(ctx context.Context, opts LogsDownloadOptions) (workflowLogsResult, error) {
	runtime, err := prepareLogsDownload(ctx, opts)
	if err != nil {
		return workflowLogsResult{}, err
	}
	defer cancelLogsDownload(runtime.timeoutCancel)

	processedRuns, timeoutReached, countLimitReached, storageLimitReached, lastFetchedBeforeDate, err := collectProcessedWorkflowRuns(runtime, opts)
	if err != nil {
		return workflowLogsResult{}, err
	}
	processedRuns = limitProcessedRuns(processedRuns, opts.Count, opts.Verbose)
	logsOrchestratorLog.Printf("Collected %d processed runs (timeoutReached=%v, countLimitReached=%v)", len(processedRuns), timeoutReached, countLimitReached)
	continuation := buildContinuationIfNeeded(processedRuns, timeoutReached, countLimitReached, storageLimitReached, continuationOptions{
		workflowName:          opts.WorkflowName,
		startDate:             opts.StartDate,
		endDate:               opts.EndDate,
		engine:                opts.Engine,
		branch:                opts.Ref,
		afterRunID:            opts.AfterRunID,
		count:                 opts.Count,
		timeoutMinutes:        opts.TimeoutMinutes,
		maxGitHubAPIRateLimit: opts.MaxGitHubAPIRateLimit,
		maxStorageMB:          opts.MaxStorageMB,
		pruneOlderRuns:        opts.PruneOlderRuns,
		lastFetchedBeforeDate: lastFetchedBeforeDate,
		previousBeforeRunID:   opts.BeforeRunID,
	})
	return workflowLogsResult{
		processedRuns:       processedRuns,
		artifactFilter:      runtime.artifactFilter,
		continuation:        continuation,
		countLimitReached:   countLimitReached,
		timeoutReached:      timeoutReached,
		storageLimitReached: storageLimitReached,
	}, nil
}
