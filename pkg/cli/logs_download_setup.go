package cli

import (
	"context"
	"fmt"
	"github.com/github/gh-aw/pkg/console"
	"os"
	"strings"
	"time"
)

type logsDownloadRuntime struct {
	activeCtx        context.Context
	startTime        time.Time
	timeoutDuration  time.Duration
	timeoutCancel    context.CancelFunc
	artifactFilter   []string
	fetchAllInRange  bool
	filters          runFilterOpts
	storageLimit     *logsStorageLimit
	cachedRuns       cachedLogsRuns
	cachedJSONLCache *cachedLogsJSONLCache
}

func prepareLogsDownload(ctx context.Context, opts LogsDownloadOptions) (logsDownloadRuntime, error) {
	opts = normalizeLogsDownloadOptions(opts)
	logLogsDownloadStart(opts)
	if err := validateMaxStorageMB(opts.MaxStorageMB); err != nil {
		return logsDownloadRuntime{}, err
	}
	artifactFilter, err := resolveLogsArtifactFilter(opts.ArtifactSets, opts.Verbose)
	if err != nil {
		return logsDownloadRuntime{}, err
	}
	var cachedRuns cachedLogsRuns
	if opts.cachedJSONLCache != nil {
		cachedRuns = opts.cachedJSONLCache.runs
	}
	if !cachedJSONLCanSatisfy(artifactFilter, opts.Parse, opts.Train, opts.ToolGraph) {
		cachedRuns = nil
	}
	if err := prepareLogsDownloadOutput(ctx, opts); err != nil {
		return logsDownloadRuntime{}, err
	}
	activeCtx, timeoutCancel, startTime, timeoutDuration := buildLogsDownloadContext(ctx, opts.TimeoutMinutes, opts.TimeoutSeconds, opts.Verbose)
	if opts.inheritTimeoutContext {
		// The shared deadline already lives on ctx; do not install a second one.
		cancelLogsDownload(timeoutCancel)
		activeCtx, timeoutCancel, startTime, timeoutDuration = ctx, nil, time.Time{}, 0
	}
	storageLimit := opts.storageLimit
	if storageLimit == nil {
		storageLimit = newLogsStorageLimit(opts.OutputDir, opts.MaxStorageMB, opts.PruneOlderRuns)
	}
	return logsDownloadRuntime{
		activeCtx:        activeCtx,
		startTime:        startTime,
		timeoutDuration:  timeoutDuration,
		timeoutCancel:    timeoutCancel,
		artifactFilter:   artifactFilter,
		fetchAllInRange:  opts.StartDate != "" || opts.EndDate != "",
		storageLimit:     storageLimit,
		cachedRuns:       cachedRuns,
		cachedJSONLCache: opts.cachedJSONLCache,
		filters: runFilterOpts{
			engine:            opts.Engine,
			runtime:           opts.Runtime,
			noStaged:          opts.NoStaged,
			dryRun:            opts.DryRun,
			noDryRun:          opts.NoDryRun,
			firewallOnly:      opts.FirewallOnly,
			noFirewall:        opts.NoFirewall,
			safeOutputType:    opts.SafeOutputType,
			filteredIntegrity: opts.FilteredIntegrity,
			evalsOnly:         opts.EvalsOnly,
			gradersOnly:       opts.GradersOnly,
		},
	}, nil
}

func logLogsDownloadStart(opts LogsDownloadOptions) {
	maxConcurrentDownloads := opts.maxConcurrentDownloads
	if maxConcurrentDownloads == 0 {
		maxConcurrentDownloads = getMaxConcurrentDownloads()
	}
	logsOrchestratorLog.Printf("Starting workflow log download: workflow=%s, count=%d, maxIterations=%d, maxConcurrentDownloads=%d, maxGitHubAPIRateLimit=%d, maxStorageMB=%d, startDate=%s, endDate=%s, outputDir=%s, summaryFile=%s, safeOutputType=%s, filteredIntegrity=%v, evalsOnly=%v, gradersOnly=%v, train=%v, format=%s, artifactSets=%v, after=%s", opts.WorkflowName, opts.Count, MaxIterations, maxConcurrentDownloads, opts.MaxGitHubAPIRateLimit, opts.MaxStorageMB, opts.StartDate, opts.EndDate, opts.OutputDir, opts.SummaryFile, opts.SafeOutputType, opts.FilteredIntegrity, opts.EvalsOnly, opts.GradersOnly, opts.Train, opts.Format, opts.ArtifactSets, opts.After)
}

func resolveLogsArtifactFilter(artifactSets []string, verbose bool) ([]string, error) {
	if err := ValidateArtifactSets(artifactSets); err != nil {
		return nil, err
	}
	artifactFilter := ResolveArtifactFilter(artifactSets)
	if len(artifactFilter) > 0 {
		logsOrchestratorLog.Printf("Artifact filter active: %v", artifactFilter)
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Artifact filter: downloading only "+strings.Join(artifactFilter, ", ")))
		}
	}
	return artifactFilter, nil
}

func prepareLogsDownloadOutput(ctx context.Context, opts LogsDownloadOptions) error {
	if !opts.skipEnsureGitignore {
		if err := ensureLogsGitignoreWithWarning(opts.Verbose); err != nil {
			return err
		}
	}
	if err := checkLogsDownloadContext(ctx); err != nil {
		return err
	}
	if err := cleanupLogsOutputDir(opts); err != nil {
		return err
	}
	if opts.Verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Fetching workflow runs from GitHub Actions..."))
	}
	return nil
}

func ensureLogsGitignoreWithWarning(verbose bool) error {
	if err := ensureLogsGitignore(); err != nil {
		logsOrchestratorLog.Printf("Failed to ensure logs .gitignore: %v", err)
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to ensure .github/aw/logs/.gitignore: %v", err)))
		}
	}
	return nil
}

func checkLogsDownloadContext(ctx context.Context) error {
	select {
	case <-ctx.Done():
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Operation cancelled"))
		return ctx.Err()
	default:
		return nil
	}
}

func cleanupLogsOutputDir(opts LogsDownloadOptions) error {
	if opts.After == "" {
		return nil
	}
	cutoff, err := parseCleanupCutoff(opts.After)
	if err != nil {
		return err
	}
	logsOrchestratorLog.Printf("Cleaning up run folders older than %s (cutoff: %s)", opts.After, cutoff.Format(time.RFC3339))
	removed, cleanErr := cleanupOldRunFolders(opts.OutputDir, cutoff, opts.Verbose)
	if cleanErr != nil {
		logsOrchestratorLog.Printf("Failed to clean up old run folders: %v", cleanErr)
		if !opts.JSONOutput {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to clean up old run folders: %v", cleanErr)))
		}
		return nil
	}
	if removed > 0 && !opts.JSONOutput {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("Removed %d cached run folder(s) older than %s", removed, opts.After)))
	} else if removed == 0 && opts.Verbose && !opts.JSONOutput {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("No cached run folders older than %s found", opts.After)))
	}
	return nil
}

func buildLogsDownloadContext(ctx context.Context, timeoutMinutes, timeoutSeconds int, verbose bool) (context.Context, context.CancelFunc, time.Time, time.Duration) {
	if timeoutMinutes <= 0 {
		return ctx, nil, time.Time{}, 0
	}
	timeoutDuration := time.Duration(timeoutMinutes) * time.Minute
	timeoutLabel := fmt.Sprintf("%d minutes", timeoutMinutes)
	// --timeout-seconds is a soft deadline derived from the caller's own budget;
	// it narrows (never extends) the --timeout window.
	if timeoutSeconds > 0 {
		timeoutDuration = time.Duration(timeoutSeconds) * time.Second
		timeoutLabel = fmt.Sprintf("%d seconds", timeoutSeconds)
	}
	startTime := time.Now()
	activeCtx, timeoutCancel := context.WithTimeout(ctx, timeoutDuration)
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Timeout set to "+timeoutLabel))
	}
	return activeCtx, timeoutCancel, startTime, timeoutDuration
}

func cancelLogsDownload(cancel context.CancelFunc) {
	if cancel != nil {
		cancel()
	}
}
