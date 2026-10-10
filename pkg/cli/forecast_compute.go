package cli

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/errorutil"
	"github.com/github/gh-aw/pkg/fileutil"
	"github.com/github/gh-aw/pkg/workflow"
)

// defaultForecastDownloadConcurrency is the number of usage-artifact downloads that
// run in parallel when DownloadConcurrency is not set (or is <= 0).
// Increasing this value reduces wall-clock time for the download phase at the cost of
// more simultaneous GitHub API requests; 8 is chosen as a balance between throughput
// and rate-limit headroom.
const defaultForecastDownloadConcurrency = 8

// errNoMatchingArtifact is returned by forecastDownloadUsageArtifact when
// the artifact listing succeeds but no artifact name matches the requested
// filter.  It is distinct from ErrNoArtifacts, which is used when a download
// is attempted but the output directory ends up empty (a transient failure
// that should not be negatively cached).
var errNoMatchingArtifact = errors.New("no matching artifact found for filter")

var (
	forecastLoadCachedRunAIC = loadCachedRunAIC
	forecastLoadRunAIC       = loadRunAICObservation
	// forecastDownloadRunArtifacts uses a forecast-specific implementation that downloads
	// only the usage artifact and skips workflow run log downloads (not needed for AIC computation).
	forecastDownloadRunArtifacts = forecastDownloadUsageArtifact
	// Forecast only needs TotalAIC; avoid the full token-usage breakdown/logging path.
	forecastAnalyzeTokenUsage = analyzeTokenUsageAICOnly
)

func forecastWorkflow(ctx context.Context, workflowName, startDate string, config ForecastConfig, periodDays int) (ForecastWorkflowResult, error) {
	result := ForecastWorkflowResult{
		WorkflowID:  extractWorkflowIDFromName(workflowName),
		Period:      config.Period,
		HistoryDays: config.Days,
	}

	// Load frontmatter metadata (triggers, concurrency, experiments).
	meta := loadWorkflowMeta(workflowName, config.Verbose)
	result.ActiveTriggers = meta.activeTriggers
	result.ConcurrencyLimit = meta.concurrencyLimit
	result.ExperimentVariants = meta.variants
	result.Engines = meta.engines

	sampledRuns, err := loadForecastSampleRuns(ctx, workflowName, startDate, config, result.WorkflowID)
	if err != nil {
		if errorutil.IsRateLimitError(err.Error()) {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(
				fmt.Sprintf("Skipping %s: GitHub API rate limit exceeded", result.WorkflowID)))
			return result, nil
		}
		return result, err
	}
	if len(sampledRuns) == 0 {
		forecastRunLog.Printf("No sampled runs found for %s in last %d days", workflowName, config.Days)
		return result, nil
	}

	// Download usage artifacts in parallel, then process results in run order.
	aicMap := parallelLoadRunAICs(ctx, sampledRuns, config)
	stats := collectForecastRunStats(sampledRuns, aicMap, workflowName)
	result.RunSamples = stats.samples
	if result.WorkflowPath == "" {
		for _, r := range sampledRuns {
			if r.WorkflowPath != "" {
				result.WorkflowPath = r.WorkflowPath
				break
			}
		}
	}

	n := len(stats.aicObservations)
	result.SampledRuns = n
	if n == 0 {
		forecastRunLog.Printf("No AIC run samples found for %s in last %d days", workflowName, config.Days)
		return result, nil
	}

	populateForecastProjection(&result, stats, config.Days, periodDays)

	// Populate experiment variant fractions from run history when metadata has variants.
	result.ExperimentVariants = computeVariantFractions(result.ExperimentVariants, sampledRuns)

	return result, nil
}

func loadForecastSampleRuns(ctx context.Context, workflowName, startDate string, config ForecastConfig, workflowID string) ([]WorkflowRun, error) {
	apiName := workflowName
	if lockFile, err := workflow.GetWorkflowLockFileName(workflowName); err == nil {
		apiName = lockFile
	}

	opts := ListWorkflowRunsOptions{
		WorkflowName: apiName,
		StartDate:    startDate,
		Limit:        config.SampleSize,
		TargetCount:  config.SampleSize,
		RepoOverride: config.RepoOverride,
		Verbose:      config.Verbose,
	}
	runs, _, err := listRunsWithBackoff(ctx, opts, workflowID)
	if err != nil {
		return nil, err
	}
	return filterForecastSampleRuns(runs, startDate, config.SampleSize), nil
}

type forecastRunStats struct {
	totalAIC        float64
	totalDurSec     float64
	successCount    int
	aicObservations []int
	samples         []ForecastRunSample
}

type forecastAICResult struct {
	runID int64
	aic   float64
}

func collectForecastRunStats(runs []WorkflowRun, aicMap map[int64]float64, workflowName string) forecastRunStats {
	stats := forecastRunStats{
		aicObservations: make([]int, 0, len(runs)),
		samples:         make([]ForecastRunSample, 0, len(runs)),
	}
	for _, r := range runs {
		runAIC, ok := aicMap[r.DatabaseID]
		if !ok {
			forecastRunLog.Printf("Skipping run %d for %s: AIC unavailable", r.DatabaseID, workflowName)
			continue
		}
		stats.totalAIC += runAIC
		stats.totalDurSec += r.Duration.Seconds()
		stats.aicObservations = append(stats.aicObservations, int(math.Round(runAIC*1000)))
		if r.Conclusion == "success" {
			stats.successCount++
		}
		stats.samples = append(stats.samples, newForecastRunSample(r, runAIC))
	}
	return stats
}

func newForecastRunSample(r WorkflowRun, runAIC float64) ForecastRunSample {
	sample := ForecastRunSample{RunID: r.DatabaseID, AIC: roundForecastAIC(runAIC)}
	if !r.StartedAt.IsZero() {
		sample.Date = r.StartedAt.Format("2006-01-02")
	}
	if r.URL != "" {
		sample.RunURL = r.URL
	}
	return sample
}

func populateForecastProjection(result *ForecastWorkflowResult, stats forecastRunStats, historyDays, periodDays int) {
	n := len(stats.aicObservations)
	result.AvgAIC = roundForecastAIC(stats.totalAIC / float64(n))
	result.AvgDurationSeconds = stats.totalDurSec / float64(n)
	result.SuccessRate = float64(stats.successCount) / float64(n)

	sortedAIC := append([]int(nil), stats.aicObservations...)
	sort.Ints(sortedAIC)
	result.P50AIC = roundForecastAIC(float64(percentileInt(sortedAIC, 50)) / 1000)
	result.P95AIC = roundForecastAIC(float64(percentileInt(sortedAIC, 95)) / 1000)

	observedRunsPerDay := float64(n) / float64(historyDays)
	result.ObservedRunsPerPeriod = observedRunsPerDay * float64(periodDays)
	weeklyRuns := observedRunsPerDay * 7
	monthlyRuns := observedRunsPerDay * 30
	result.WeeklyProjectedAIC = roundForecastAIC(weeklyRuns * result.AvgAIC)
	result.MonthlyProjectedAIC = roundForecastAIC(monthlyRuns * result.AvgAIC)
	result.ProjectedAIC = roundForecastAIC(result.ObservedRunsPerPeriod * result.AvgAIC)

	seed := time.Now().UnixNano()
	rng := rand.New(rand.NewSource(seed))      //nolint:gosec // non-cryptographic simulation RNG
	rng2 := rand.New(rand.NewSource(seed + 1)) //nolint:gosec
	rng3 := rand.New(rand.NewSource(seed + 2)) //nolint:gosec
	result.MonteCarlo = runMonteCarlo(stats.aicObservations, stats.successCount, result.ObservedRunsPerPeriod, rng)
	result.WeeklyMonteCarlo = runMonteCarlo(stats.aicObservations, stats.successCount, weeklyRuns, rng2)
	result.MonthlyMonteCarlo = runMonteCarlo(stats.aicObservations, stats.successCount, monthlyRuns, rng3)
	if result.MonteCarlo != nil {
		result.ProjectedAIC = result.MonteCarlo.P50ProjectedAIC
	}
}

// parallelLoadRunAICs fetches AIC data for all sampled runs concurrently and returns
// a map from DatabaseID to AIC value. Downloads run at most concurrency goroutines at a
// time; when config.DownloadConcurrency is <= 0, defaultForecastDownloadConcurrency is
// used. Runs that complete loading with no artifact or no AIC data are returned with
// a zero value; runs interrupted before loading are omitted.
func parallelLoadRunAICs(ctx context.Context, runs []WorkflowRun, config ForecastConfig) map[int64]float64 {
	n := len(runs)
	if n == 0 {
		return make(map[int64]float64)
	}
	concurrency := config.DownloadConcurrency
	if concurrency <= 0 {
		concurrency = defaultForecastDownloadConcurrency
	}
	if concurrency > n {
		concurrency = n
	}

	sem := make(chan struct{}, concurrency)
	resultsCh := make(chan forecastAICResult, n)
	var wg sync.WaitGroup

	for _, r := range runs {
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		go loadForecastRunAIC(ctx, sem, r.DatabaseID, config.Verbose, resultsCh, &wg)
	}

	go func() {
		defer func() {
			if r := recover(); r != nil {
				forecastRunLog.Printf("Panic in AIC results collector (recovered): %v", r)
			}
		}()
		wg.Wait()
		close(resultsCh)
	}()

	aicMap := make(map[int64]float64, n)
	for r := range resultsCh {
		aicMap[r.runID] = r.aic
	}
	return aicMap
}

func loadForecastRunAIC(ctx context.Context, sem chan struct{}, runID int64, verbose bool, resultsCh chan<- forecastAICResult, wg *sync.WaitGroup) {
	defer wg.Done()
	defer func() {
		if r := recover(); r != nil {
			forecastRunLog.Printf("Panic in AIC worker for run %d (recovered): %v", runID, r)
		}
	}()
	select {
	case sem <- struct{}{}:
	case <-ctx.Done():
		return
	}
	defer func() { <-sem }()
	aic, ok := forecastLoadRunAIC(ctx, runID, verbose)
	if !ok {
		return
	}
	resultsCh <- forecastAICResult{runID: runID, aic: aic}
}

// loadCachedRunAIC looks up a locally-cached AIC value for the given run ID.
// It checks, in order: (1) a fully-processed run_summary.json written by `gh aw logs`,
// (2) a forecast-specific forecast_aic.json written by a previous forecast run, and
// only on a miss (3) downloads the usage artifact, computes the AIC, and persists it to
// the forecast cache for next time.
// Returns 0 when no cache exists or the run has no AIC data.
// This avoids re-downloading and re-parsing aw_info.json/usage artifacts for runs already
// seen while still providing accurate AIC observations for the simulation.
//
// Cache locations (under <defaultLogsOutputDir>/run-<runID>/, i.e. ".github/aw/logs"):
//   - run_summary.json   (shared cache produced by `gh aw logs`)
//   - forecast_aic.json  (forecast-only cache produced by this function)
func loadCachedRunAIC(ctx context.Context, runID int64, verbose bool) float64 {
	aic, _ := loadRunAICObservation(ctx, runID, verbose)
	return aic
}

func loadRunAICObservation(ctx context.Context, runID int64, verbose bool) (float64, bool) {
	dir := filepath.Join(defaultLogsOutputDir, fmt.Sprintf("run-%d", runID))
	if aic, ok := loadRunSummaryAIC(dir, runID, verbose); ok {
		return aic, true
	}

	// Second fast path: a forecast-specific AIC cache written by a previous forecast run.
	// This lets repeated forecast runs reuse the computed AIC without re-scanning the run
	// directory or re-parsing the usage artifact.
	if aic, ok := loadForecastAICCache(dir, runID); ok {
		forecastRunLog.Printf("AIC forecast-cache hit for run %d: aic=%.3f (from %s)", runID, aic, forecastAICCacheFileName)
		return aic, true
	}

	forecastRunLog.Printf("AIC cache miss for run %d; downloading usage artifact to %s", runID, dir)
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatVerboseMessage(fmt.Sprintf("Downloading usage artifact for run %d…", runID)))
	}

	if err := downloadForecastUsageForAIC(ctx, runID, dir, verbose); err != nil {
		if errors.Is(err, errNoMatchingArtifact) {
			return 0, true
		}
		return 0, false
	}
	return analyzeAndCacheForecastAIC(dir, runID, verbose)
}

func loadRunSummaryAIC(dir string, runID int64, verbose bool) (float64, bool) {
	summary, ok := loadRunSummary(dir, verbose)
	if ok && summary != nil && summary.TokenUsage != nil && summary.TokenUsage.TotalAIC > 0 {
		forecastRunLog.Printf("AIC cache hit for run %d: aic=%.3f (from run_summary.json)", runID, summary.TokenUsage.TotalAIC)
		return summary.TokenUsage.TotalAIC, true
	}
	if ok && summary != nil && summary.TokenUsage != nil && summary.TokenUsage.TotalAIC <= 0 {
		forecastRunLog.Printf("AIC cache stale/empty for run %d: cached_total_aic=%.3f, token_file_recompute_required=true", runID, summary.TokenUsage.TotalAIC)
	}
	return 0, false
}

func downloadForecastUsageForAIC(ctx context.Context, runID int64, dir string, verbose bool) error {
	err := forecastDownloadRunArtifacts(ctx, runID, dir, verbose, "", "", "", []string{"usage"})
	if err == nil {
		return nil
	}
	if errors.Is(err, errNoMatchingArtifact) {
		forecastRunLog.Printf("No usage artifact for run %d; AIC will be 0", runID)
		saveForecastNoDataCache(dir, runID)
		return err
	}
	forecastRunLog.Printf("Usage artifact download for run %d failed: %v", runID, err)
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatVerboseMessage(fmt.Sprintf("Usage artifact download for run %d failed: %v", runID, err)))
	}
	return err
}

func analyzeAndCacheForecastAIC(dir string, runID int64, verbose bool) (float64, bool) {
	tokenUsage, err := forecastAnalyzeTokenUsage(dir, verbose)
	if err != nil || tokenUsage == nil || tokenUsage.TotalAIC <= 0 {
		forecastRunLog.Printf("No AIC data in usage artifact for run %d (err=%v, tokenUsage=%v)", runID, err, tokenUsage)
		// The usage artifact was fetched but carries no AIC data; this is permanent for a
		// completed run, so negative-cache it to skip the download next time.
		saveForecastNoDataCache(dir, runID)
		return 0, true
	}
	forecastRunLog.Printf("AIC from usage artifact for run %d: aic=%.3f", runID, tokenUsage.TotalAIC)
	// Persist the computed AIC so subsequent forecast runs hit the fast forecast cache
	// instead of re-scanning the directory and re-parsing the usage artifact.
	saveForecastAICCache(dir, runID, tokenUsage.TotalAIC)
	return tokenUsage.TotalAIC, true
}

// forecastDownloadUsageArtifact is a forecast-specific replacement for
// downloadRunArtifacts. Unlike the general-purpose downloader, it:
//   - Downloads only artifacts matching artifactFilter (typically ["usage"]).
//   - Skips workflow run log downloads entirely — logs are not needed for
//     AIC computation and downloading them wastes time when forecasting
//     many runs.
//   - Returns errNoMatchingArtifact when the listing succeeds but no artifact
//     name matches the filter (safe to negatively cache — the run has no usage
//     data). Returns ErrNoArtifacts when a listed artifact was attempted but the
//     output directory is empty after download (transient; must not be cached).
//
// It is referenced by forecastDownloadRunArtifacts so that tests can substitute
// a mock implementation without modifying the general artifact download path.
func forecastDownloadUsageArtifact(ctx context.Context, runID int64, outputDir string, verbose bool, owner, repo, hostname string, artifactFilter []string) error {
	forecastRunLog.Printf("Downloading usage artifact: run_id=%d, output_dir=%s, filter=%v", runID, outputDir, artifactFilter)
	shouldLogProgress := IsRunningInCI() || verbose

	missing, complete := existingForecastArtifactFilter(runID, outputDir, artifactFilter, shouldLogProgress)
	if complete {
		return nil
	}
	artifactFilter = missing

	if err := os.MkdirAll(outputDir, constants.DirPermPublic); err != nil {
		return fmt.Errorf("failed to create output directory for run %d: %w", runID, err)
	}

	downloadableNames, err := listDownloadableForecastArtifacts(ctx, runID, outputDir, verbose, owner, repo, hostname, artifactFilter)
	if err != nil {
		return err
	}
	if len(downloadableNames) == 0 {
		removeEmptyDir(outputDir)
		return errNoMatchingArtifact
	}
	if shouldLogProgress {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(
			fmt.Sprintf("Downloading usage artifact(s) for run %d: %v", runID, downloadableNames)))
	}

	artifactOpts := downloadArtifactsOptions{runID: runID, outputDir: outputDir, verbose: verbose, owner: owner, repo: repo, hostname: hostname}
	if err := downloadArtifactsByName(ctx, artifactOpts, downloadableNames); err != nil {
		return fmt.Errorf("failed to download usage artifact for run %d: %w", runID, err)
	}

	if fileutil.IsDirEmpty(outputDir) {
		return ErrNoArtifacts
	}

	forecastRunLog.Printf("Downloaded usage artifact for run %d to %s", runID, outputDir)
	return nil
}

func existingForecastArtifactFilter(runID int64, outputDir string, artifactFilter []string, shouldLogProgress bool) ([]string, bool) {
	if !fileutil.DirExists(outputDir) || fileutil.IsDirEmpty(outputDir) {
		return artifactFilter, false
	}
	missing := findMissingFilterEntries(artifactFilter, outputDir)
	if len(missing) == 0 {
		forecastRunLog.Printf("Usage artifact already on disk for run %d, skipping download", runID)
		if shouldLogProgress {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(
				fmt.Sprintf("Usage artifact already present for run %d, skipping download", runID)))
		}
		return nil, true
	}
	forecastRunLog.Printf("Usage artifact partially missing for run %d: %v; downloading missing entries", runID, missing)
	return missing, false
}

func listDownloadableForecastArtifacts(ctx context.Context, runID int64, outputDir string, verbose bool, owner, repo, hostname string, artifactFilter []string) ([]string, error) {
	artifactNames, listErr := listRunArtifactNames(ctx, runID, owner, repo, hostname, verbose)
	if listErr != nil {
		forecastRunLog.Printf("Failed to list artifacts for run %d: %v", runID, listErr)
		removeEmptyDir(outputDir)
		return nil, fmt.Errorf("failed to list artifacts for run %d: %w", runID, listErr)
	}

	var downloadableNames []string
	for _, name := range artifactNames {
		if !isDockerBuildArtifact(name) && artifactMatchesFilter(name, artifactFilter) {
			downloadableNames = append(downloadableNames, name)
		}
	}
	forecastRunLog.Printf("Run %d: listed artifacts=%v, filter=%v, downloadable=%v", runID, artifactNames, artifactFilter, downloadableNames)
	return downloadableNames, nil
}

func removeEmptyDir(dir string) {
	if fileutil.IsDirEmpty(dir) {
		_ = os.RemoveAll(dir)
	}
}

// emitPartialForecastResults outputs whatever workflow results have been collected so
// far when the forecast computation is interrupted (timeout or user cancellation).
// Partial results are only meaningful when at least one workflow has been fully
// processed; the function is a no-op when results is empty so callers do not need to
// guard against it.
func emitPartialForecastResults(results []ForecastWorkflowResult, config ForecastConfig, now time.Time) {
	if len(results) == 0 {
		return
	}
	forecastRunLog.Printf("Emitting %d partial forecast result(s) before early exit", len(results))
	fmt.Fprintln(os.Stderr, console.FormatWarningMessage(
		fmt.Sprintf("Forecast interrupted; emitting partial results for %d workflow(s) processed so far.", len(results))))

	// Sort partial results by Monte Carlo P50 descending (mirrors the full-results sort).
	slices.SortFunc(results, func(a, b ForecastWorkflowResult) int {
		pi := a.ProjectedAIC
		if mc := a.MonteCarlo; mc != nil {
			pi = mc.P50ProjectedAIC
		}
		pj := b.ProjectedAIC
		if mc := b.MonteCarlo; mc != nil {
			pj = mc.P50ProjectedAIC
		}
		if pi > pj {
			return -1
		}
		if pi < pj {
			return 1
		}
		return 0
	})

	output := ForecastResult{
		Period:    config.Period,
		AsOf:      now.UTC().Format(time.RFC3339),
		EvalMode:  config.EvalMode,
		Workflows: results,
	}
	if config.JSONOutput {
		_ = renderForecastJSON(output)
	} else {
		_ = renderForecastTable(output, config)
	}
}

// isCompletedDispatchedRun reports whether a run actually dispatched and completed,
// so it can be used for metric computation. Runs that never dispatched a job
// (skipped, action_required) are excluded.
func isCompletedDispatchedRun(r WorkflowRun) bool {
	return r.Status == "completed" && !isNonDispatchedConclusion(r.Conclusion)
}

// isForecastSampleRun reports whether a workflow run should be included in the
// forecast sample. Completed runs provide final observations; in-progress runs
// may provide partial usage snapshots and count as non-success observations.
func isForecastSampleRun(r WorkflowRun) bool {
	if isNonDispatchedConclusion(r.Conclusion) {
		return false
	}
	return r.Status == "completed" || r.Status == "in_progress"
}

func filterForecastSampleRuns(runs []WorkflowRun, startDate string, sampleSize int) []WorkflowRun {
	var cutoff time.Time
	var hasCutoff bool
	if parsed, err := time.Parse("2006-01-02", startDate); err == nil {
		cutoff = parsed
		hasCutoff = true
	}

	sampled := make([]WorkflowRun, 0, len(runs))
	for _, r := range runs {
		if !isForecastSampleRun(r) {
			continue
		}
		if hasCutoff && !r.StartedAt.IsZero() && r.StartedAt.Before(cutoff) {
			continue
		}
		// Compute Duration from StartedAt/UpdatedAt when not already set (gh run list
		// does not populate the Duration field; health_command uses the same approach).
		if r.Duration == 0 && !r.StartedAt.IsZero() && !r.UpdatedAt.IsZero() {
			r.Duration = r.UpdatedAt.Sub(r.StartedAt)
		}
		sampled = append(sampled, r)
	}
	if sampleSize > 0 && len(sampled) > sampleSize {
		return sampled[:sampleSize]
	}
	return sampled
}

// evaluateForecast fetches actual completed runs in the validation window and
// returns a ForecastEvaluation comparing them against the Monte Carlo forecast.
//
// validationStartDate / validationEndDate are ISO-8601 strings bracketing the
// period that was forecast (= one projection period immediately before now).
// Actual runs are fetched with the same pagination helper used for training,
// but with the validation date range.
func evaluateForecast(ctx context.Context, workflowName string, forecast ForecastWorkflowResult, validationStartDate, validationEndDate string, config ForecastConfig) *ForecastEvaluation {
	eval := &ForecastEvaluation{
		TrainingStartDate: forecastTrainingStartDate(validationStartDate, forecast.HistoryDays),
		TrainingEndDate:   validationStartDate,
		ValidationEndDate: validationEndDate,
	}

	apiName := workflowName
	if lockFile, err := workflow.GetWorkflowLockFileName(workflowName); err == nil {
		apiName = lockFile
	}
	opts := ListWorkflowRunsOptions{
		WorkflowName: apiName,
		Status:       "completed",
		StartDate:    validationStartDate,
		Limit:        config.SampleSize,
		TargetCount:  config.SampleSize,
		RepoOverride: config.RepoOverride,
		Verbose:      config.Verbose,
	}
	opts.Context = ctx
	runs, _, err := listWorkflowRunsWithPagination(opts)
	if err != nil {
		forecastRunLog.Printf("Eval: failed to fetch validation runs for %s: %v", workflowName, err)
		return eval
	}
	addValidationActuals(ctx, eval, runs, validationStartDate, config.Verbose)
	applyForecastEvaluationMetrics(eval, forecast)
	return eval
}

func forecastTrainingStartDate(validationStartDate string, historyDays int) string {
	if t, err := time.Parse("2006-01-02", validationStartDate); err == nil {
		return t.AddDate(0, 0, -historyDays).Format("2006-01-02")
	}
	return validationStartDate
}

func addValidationActuals(ctx context.Context, eval *ForecastEvaluation, runs []WorkflowRun, validationStartDate string, verbose bool) {
	validationStart, _ := time.Parse("2006-01-02", validationStartDate)
	validationEnd := time.Now()
	for _, r := range runs {
		if !isCompletedDispatchedRun(r) {
			continue
		}
		// Skip runs with no timestamp — we cannot verify they belong to the
		// validation window, so including them would introduce undefined bias.
		if r.StartedAt.IsZero() {
			continue
		}
		if r.StartedAt.Before(validationStart) || r.StartedAt.After(validationEnd) {
			continue
		}
		eval.ActualRuns++
		eval.ActualAIC += forecastLoadCachedRunAIC(ctx, r.DatabaseID, verbose)
	}
}

func applyForecastEvaluationMetrics(eval *ForecastEvaluation, forecast ForecastWorkflowResult) {
	p50 := forecast.ProjectedAIC
	p10 := forecast.ProjectedAIC
	p90 := forecast.ProjectedAIC
	if mc := forecast.MonteCarlo; mc != nil {
		p50 = mc.P50ProjectedAIC
		p10 = mc.P10ProjectedAIC
		p90 = mc.P90ProjectedAIC
	}

	eval.P50ErrorAbs = eval.ActualAIC - p50
	if p50 > 0 {
		eval.P50ErrorPct = eval.P50ErrorAbs / p50 * 100
	}
	eval.InCI = eval.ActualAIC >= p10 && eval.ActualAIC <= p90
}
