package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/stringutil"
)

type logsWorkflowTarget struct {
	workflowName string
	repoOverride string
}

type logsTargetResult struct {
	target logsWorkflowTarget
	result workflowLogsResult
	err    error
}

var collectWorkflowLogsForTarget = collectWorkflowLogs

// DownloadWorkflowLogsForTargets downloads several workflow reports concurrently
// and renders one combined report. Each target gets an isolated output directory
// so run IDs from different repositories cannot collide in the local cache.
func DownloadWorkflowLogsForTargets(
	ctx context.Context,
	opts LogsDownloadOptions,
	targets []logsWorkflowTarget,
	initialErrors []error,
) error {
	if len(targets) == 0 {
		return errors.Join(initialErrors...)
	}
	if err := ensureLogsGitignoreWithWarning(opts.Verbose); err != nil {
		return err
	}

	allAPIRateLimits := startGitHubAPIRateLimitReports(ctx, logsTargetRateLimitHosts(targets))
	results := collectLogsTargets(ctx, opts, targets)
	processedRuns, continuations, timeoutReached, countLimitReached, storageLimitReached, allErrors := mergeLogsTargetResults(results, initialErrors)
	for _, err := range allErrors {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Skipping workflow target: "+err.Error()))
	}
	if ctx.Err() != nil {
		return context.Cause(ctx)
	}
	finishGitHubAPIRateLimitReports(ctx, allAPIRateLimits, opts.JSONOutput)
	apiRateLimit, apiRateLimits := partitionGitHubAPIRateLimitReports(allAPIRateLimits)
	if len(processedRuns) == 0 {
		if len(allErrors) > 0 {
			return errors.Join(allErrors...)
		}
		_, err := handleEmptyProcessedRuns(nil, opts, timeoutReached, storageLimitReached, nil, continuations, apiRateLimit, apiRateLimits)
		return err
	}

	slices.SortStableFunc(processedRuns, func(a, b ProcessedRun) int {
		return b.Run.CreatedAt.Compare(a.Run.CreatedAt)
	})
	artifactFilter, err := resolveLogsArtifactFilter(opts.ArtifactSets, opts.Verbose)
	if err != nil {
		return err
	}
	return renderLogsOutput(processedRuns, renderLogsOutputOptions{
		outputDir:         opts.OutputDir,
		summaryFile:       opts.SummaryFile,
		format:            opts.Format,
		reportFile:        opts.ReportFile,
		jsonOutput:        opts.JSONOutput,
		toolGraph:         opts.ToolGraph,
		train:             opts.Train,
		audit:             opts.Audit,
		verbose:           opts.Verbose,
		artifactFilter:    artifactFilter,
		startDate:         opts.StartDate,
		endDate:           opts.EndDate,
		checkStaleness:    true,
		countLimitReached: countLimitReached,
		suppressRender:    opts.SuppressRender,
		continuations:     continuations,
		apiRateLimit:      apiRateLimit,
		apiRateLimits:     apiRateLimits,
	})
}

func logsTargetRateLimitHosts(targets []logsWorkflowTarget) []string {
	hosts := make([]string, 0, len(targets))
	seen := make(map[string]struct{}, len(targets))
	for _, target := range targets {
		host := normalizedGitHubAPIHost(logsRateLimitHost(target.repoOverride))
		if _, ok := seen[host]; ok {
			continue
		}
		seen[host] = struct{}{}
		hosts = append(hosts, host)
	}
	return hosts
}

func collectLogsTargets(ctx context.Context, opts LogsDownloadOptions, targets []logsWorkflowTarget) []logsTargetResult {
	resultChannel := make(chan logsTargetResult, len(targets))
	var wg sync.WaitGroup
	workerCount := min(len(targets), getMaxConcurrentWorkflowDownloads())
	sem := make(chan struct{}, workerCount)
	perTargetDownloads := max(1, getMaxConcurrentDownloads()/workerCount)
	cleanupErrors := make(map[string]error, len(targets))
	for _, target := range targets {
		cleanupOpts := opts
		cleanupOpts.OutputDir = logsTargetOutputDir(opts.OutputDir, target)
		if err := cleanupLogsOutputDir(cleanupOpts); err != nil {
			cleanupErrors[target.displayName()] = err
		}
	}
	storageLimit := newLogsStorageLimit(opts.OutputDir, opts.MaxStorageMB, opts.PruneOlderRuns)
	for _, target := range targets {
		wg.Go(func() {
			defer func() {
				if recovered := recover(); recovered != nil {
					resultChannel <- logsTargetResult{target: target, err: fmt.Errorf("workflow collector panicked: %v", recovered)}
				}
			}()
			if err := cleanupErrors[target.displayName()]; err != nil {
				resultChannel <- logsTargetResult{target: target, err: err}
				return
			}
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				resultChannel <- logsTargetResult{target: target, err: ctx.Err()}
				return
			}

			targetOpts := opts
			targetOpts.WorkflowName = target.workflowName
			targetOpts.RepoOverride = target.repoOverride
			targetOpts.OutputDir = logsTargetOutputDir(opts.OutputDir, target)
			targetOpts.SummaryFile = ""
			targetOpts.Train = false
			targetOpts.SuppressRender = true
			targetOpts.skipEnsureGitignore = true
			targetOpts.rateLimitFirstRequest = true
			targetOpts.maxConcurrentDownloads = perTargetDownloads
			targetOpts.storageLimit = storageLimit
			result, err := collectWorkflowLogsForTarget(ctx, targetOpts)
			resultChannel <- logsTargetResult{target: target, result: result, err: err}
		})
	}
	wg.Wait()
	close(resultChannel)
	results := make([]logsTargetResult, 0, len(targets))
	for result := range resultChannel {
		results = append(results, result)
	}
	return results
}

func mergeLogsTargetResults(
	results []logsTargetResult,
	initialErrors []error,
) ([]ProcessedRun, []WorkflowContinuation, bool, bool, bool, []error) {
	allErrors := append([]error(nil), initialErrors...)
	var processedRuns []ProcessedRun
	timeoutReached := false
	countLimitReached := false
	storageLimitReached := false
	var continuations []WorkflowContinuation
	for _, targetResult := range results {
		if targetResult.err != nil {
			allErrors = append(allErrors, fmt.Errorf("%s: %w", targetResult.target.displayName(), targetResult.err))
			continue
		}
		processedRuns = append(processedRuns, targetResult.result.processedRuns...)
		timeoutReached = timeoutReached || targetResult.result.timeoutReached
		countLimitReached = countLimitReached || targetResult.result.countLimitReached
		storageLimitReached = storageLimitReached || targetResult.result.storageLimitReached
		if targetResult.result.continuation != nil {
			continuations = append(continuations, WorkflowContinuation{
				Repository:       targetResult.target.repoOverride,
				ContinuationData: *targetResult.result.continuation,
			})
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(
				"Partial results for workflow target "+targetResult.target.displayName()+"; continuation parameters were written to the report",
			))
		}
	}
	return processedRuns, continuations, timeoutReached, countLimitReached, storageLimitReached, allErrors
}

func (t logsWorkflowTarget) displayName() string {
	if t.repoOverride == "" {
		return t.workflowName
	}
	return filepath.Join(t.repoOverride, t.workflowName)
}

func logsTargetOutputDir(root string, target logsWorkflowTarget) string {
	workflowDir := "workflow-" + stringutil.SanitizeForFilename(target.workflowName)
	if target.repoOverride == "" {
		return filepath.Join(root, workflowDir)
	}
	repoDir := "repo-" + stringutil.SanitizeForFilename(target.repoOverride)
	return filepath.Join(root, repoDir, workflowDir)
}

func getMaxConcurrentWorkflowDownloads() int {
	const maxConcurrentWorkflows = 4
	return min(maxConcurrentWorkflows, getMaxConcurrentDownloads())
}
