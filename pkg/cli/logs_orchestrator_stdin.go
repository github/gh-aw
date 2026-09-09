// This file provides command-line interface functionality for gh-aw.
// This file (logs_orchestrator_stdin.go) contains DownloadWorkflowLogsFromStdin,
// the stdin-driven run-discovery and processing path used when --stdin is passed
// to the logs command.

package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/fileutil"
	"github.com/github/gh-aw/pkg/parser"
)

// DownloadWorkflowLogsFromStdin fetches and processes workflow run logs for runs
// provided as IDs or URLs, bypassing the GitHub API run-discovery step.
// This is used when the --stdin flag is passed to the logs command.
func DownloadWorkflowLogsFromStdin(ctx context.Context, opts StdinLogsOptions) error { //nolint:largefunc // Existing stdin orchestration remains centralized.
	logsOrchestratorLog.Printf("Starting stdin log download: runs=%d, outputDir=%s", len(opts.RunURLs), opts.OutputDir)

	if err := ValidateArtifactSets(opts.ArtifactSets); err != nil {
		return err
	}
	if err := validateMaxStorageMB(opts.MaxStorageMB); err != nil {
		return err
	}
	artifactFilter := ResolveArtifactFilter(opts.ArtifactSets)
	if len(artifactFilter) > 0 {
		logsOrchestratorLog.Printf("Artifact filter active: %v", artifactFilter)
		if opts.Verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Artifact filter: downloading only "+strings.Join(artifactFilter, ", ")))
		}
	}

	if err := ensureLogsGitignore(); err != nil {
		logsOrchestratorLog.Printf("Failed to ensure logs .gitignore: %v", err)
		if opts.Verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to ensure .github/aw/logs/.gitignore: %v", err)))
		}
	}

	select {
	case <-ctx.Done():
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Operation cancelled"))
		return ctx.Err()
	default:
	}

	if len(opts.RunURLs) == 0 {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage("No run IDs or URLs provided on stdin"))
		return nil
	}
	// Parse owner/repo (and optional GHES host) from --repo override if provided.
	// Accepted formats: "owner/repo" or "HOST/owner/repo".
	var hostOverride, ownerOverride, repoNameOverride string
	if opts.RepoOverride != "" {
		parts := strings.SplitN(opts.RepoOverride, "/", 3)
		switch len(parts) {
		case 3: // HOST/owner/repo
			if parts[0] == "" || parts[1] == "" || parts[2] == "" {
				return fmt.Errorf("invalid repository format '%s': expected '[HOST/]owner/repo'", opts.RepoOverride)
			}
			hostOverride, ownerOverride, repoNameOverride = parts[0], parts[1], parts[2]
		case 2: // owner/repo
			if parts[0] == "" || parts[1] == "" {
				return fmt.Errorf("invalid repository format '%s': expected '[HOST/]owner/repo'", opts.RepoOverride)
			}
			ownerOverride, repoNameOverride = parts[0], parts[1]
		default:
			return fmt.Errorf("invalid repository format '%s': expected '[HOST/]owner/repo'", opts.RepoOverride)
		}
	}
	rateLimitHosts := make([]string, 0, len(opts.RunURLs))
	seenRateLimitHosts := make(map[string]struct{}, len(opts.RunURLs))
	for _, rawURL := range opts.RunURLs {
		components, err := parser.ParseRunURLExtended(rawURL)
		if err != nil {
			continue
		}
		host := components.Host
		if host == "" {
			host = hostOverride
		}
		host = normalizedGitHubAPIHost(host)
		if _, ok := seenRateLimitHosts[host]; ok {
			continue
		}
		seenRateLimitHosts[host] = struct{}{}
		rateLimitHosts = append(rateLimitHosts, host)
	}
	if len(rateLimitHosts) == 0 {
		rateLimitHosts = append(rateLimitHosts, normalizedGitHubAPIHost(hostOverride))
	}
	allAPIRateLimits := startGitHubAPIRateLimitReports(ctx, rateLimitHosts)

	// Start timeout timer if specified.
	var startTime time.Time
	if opts.Timeout > 0 {
		startTime = time.Now()
		if opts.Verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Timeout set to %d minutes", opts.Timeout)))
		}
	}

	if opts.Verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Fetching metadata for %d runs from stdin...", len(opts.RunURLs))))
	}

	// Build WorkflowRun objects by fetching metadata for each provided URL.
	var runs []WorkflowRun
	for _, rawURL := range opts.RunURLs {
		select {
		case <-ctx.Done():
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Operation cancelled"))
			return ctx.Err()
		default:
		}

		if opts.Timeout > 0 && time.Since(startTime).Seconds() >= float64(opts.Timeout)*60 {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Timeout reached before all run metadata could be fetched"))
			break
		}

		components, err := parser.ParseRunURLExtended(rawURL)
		if err != nil {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Skipping invalid run %q: %v", rawURL, err)))
			continue
		}

		// Prefer owner/repo embedded in the URL; fall back to --repo override.
		// If neither source provides owner, the run cannot be fetched — return an
		// actionable error rather than silently continuing with a broken API call.
		owner := components.Owner
		repo := components.Repo
		host := components.Host
		if owner == "" {
			owner = ownerOverride
			repo = repoNameOverride
			if host == "" {
				host = hostOverride
			}
		}
		if owner == "" {
			return fmt.Errorf("run %q does not include repository information; pass --repo owner/repo or provide full run URLs", rawURL)
		}

		run, err := fetchWorkflowRunMetadata(ctx, components.Number, owner, repo, host, opts.Verbose)
		if err != nil {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Skipping run %d: failed to fetch metadata: %v", components.Number, err)))
			continue
		}
		runs = append(runs, run)
	}

	if len(runs) == 0 {
		finishGitHubAPIRateLimitReports(ctx, allAPIRateLimits, opts.JSONOutput)
		apiRateLimit, apiRateLimits := partitionGitHubAPIRateLimitReports(allAPIRateLimits)
		if opts.JSONOutput {
			logsData := buildLogsData([]ProcessedRun{}, opts.OutputDir, nil)
			logsData.GitHubAPIRateLimit = populatedGitHubAPIRateLimitReport(apiRateLimit)
			logsData.GitHubAPIRateLimits = populatedGitHubAPIRateLimitReports(apiRateLimits)
			logsData.Message = "No runs found. No valid runs could be loaded from the provided input."
			if err := renderLogsJSON(logsData, opts.Verbose); err != nil {
				return fmt.Errorf("failed to render JSON output: %w", err)
			}
		}
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage("No valid runs could be loaded from stdin"))
		return nil
	}

	// Download artifacts for all runs concurrently.
	storageLimit := newLogsStorageLimit(opts.OutputDir, opts.MaxStorageMB, opts.PruneOlderRuns)
	downloadResults := downloadRunArtifactsConcurrent(ctx, runs, runArtifactsConcurrentOptions{outputDir: opts.OutputDir, verbose: opts.Verbose, maxRuns: len(runs), repoOverride: opts.RepoOverride, artifactFilter: artifactFilter, evalsOnly: opts.EvalsOnly, artifactSets: opts.ArtifactSets, storageLimit: storageLimit})

	filters := runFilterOpts{
		engine:            opts.Engine,
		runtime:           opts.Runtime,
		noStaged:          opts.NoStaged,
		firewallOnly:      opts.FirewallOnly,
		noFirewall:        opts.NoFirewall,
		safeOutputType:    opts.SafeOutputType,
		filteredIntegrity: opts.FilteredIntegrity,
		evalsOnly:         opts.EvalsOnly,
	}

	// Process download results applying the same filters as DownloadWorkflowLogs.
	var processedRuns []ProcessedRun
	var storageLimitReached bool
	for _, result := range downloadResults {
		if errors.Is(result.Error, errLogsStorageLimitReached) {
			storageLimitReached = true
		}
		if result.Skipped {
			if opts.Verbose && result.Error != nil {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Skipping run %d: %v", result.Run.DatabaseID, result.Error)))
			}
			finalizeLogsRunDownload(storageLimit, result)
			continue
		}

		if result.Error != nil {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to download artifacts for run %d: %v", result.Run.DatabaseID, result.Error)))
			finalizeLogsRunDownload(storageLimit, result)
			continue
		}

		if applyRunFilters(ctx, result, filters, opts.Verbose) {
			finalizeLogsRunDownload(storageLimit, result)
			continue
		}

		processedRun := buildProcessedRun(ctx, result, opts.Verbose, false)

		if opts.Parse {
			awInfoPath := filepath.Join(result.LogsPath, "aw_info.json")
			detectedEngine := extractEngineFromAwInfo(awInfoPath, opts.Verbose)
			if err := parseAgentLog(result.LogsPath, detectedEngine, opts.Verbose); err != nil {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to parse log for run %d: %v", processedRun.Run.DatabaseID, err)))
			} else {
				logMdPath := filepath.Join(result.LogsPath, "log.md")
				if fileutil.FileExists(logMdPath) {
					fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("✓ Parsed log for run %d → %s", processedRun.Run.DatabaseID, logMdPath)))
				}
			}
			if err := parseFirewallLogs(result.LogsPath, opts.Verbose); err != nil {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to parse firewall logs for run %d: %v", processedRun.Run.DatabaseID, err)))
			} else {
				firewallMdPath := filepath.Join(result.LogsPath, "firewall.md")
				if fileutil.FileExists(firewallMdPath) {
					fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("✓ Parsed firewall logs for run %d → %s", processedRun.Run.DatabaseID, firewallMdPath)))
				}
			}
		}

		processedRuns = append(processedRuns, processedRun)
		finalizeLogsRunDownload(storageLimit, result)
	}

	if len(processedRuns) == 0 {
		finishGitHubAPIRateLimitReports(ctx, allAPIRateLimits, opts.JSONOutput)
		apiRateLimit, apiRateLimits := partitionGitHubAPIRateLimitReports(allAPIRateLimits)
		if opts.JSONOutput {
			logsData := buildLogsData([]ProcessedRun{}, opts.OutputDir, nil)
			logsData.GitHubAPIRateLimit = populatedGitHubAPIRateLimitReport(apiRateLimit)
			logsData.GitHubAPIRateLimits = populatedGitHubAPIRateLimitReports(apiRateLimits)
			logsData.Message = noRunsMessage("", false, storageLimitReached)
			if err := renderLogsJSON(logsData, opts.Verbose); err != nil {
				return fmt.Errorf("failed to render JSON output: %w", err)
			}
		}
		if storageLimitReached {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Storage limit reached before any new runs could be processed"))
		} else {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage("No workflow runs with artifacts found matching the specified criteria"))
		}
		return nil
	}

	message := ""
	if storageLimitReached {
		message = "Storage limit reached. Results are partial because some input runs were not downloaded."
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(message))
	}
	finishGitHubAPIRateLimitReports(ctx, allAPIRateLimits, opts.JSONOutput)
	apiRateLimit, apiRateLimits := partitionGitHubAPIRateLimitReports(allAPIRateLimits)
	return renderLogsOutput(processedRuns, renderLogsOutputOptions{
		outputDir:      opts.OutputDir,
		summaryFile:    opts.SummaryFile,
		format:         opts.Format,
		reportFile:     opts.ReportFile,
		jsonOutput:     opts.JSONOutput,
		toolGraph:      opts.ToolGraph,
		train:          opts.Train,
		audit:          opts.Audit,
		message:        message,
		verbose:        opts.Verbose,
		artifactFilter: artifactFilter,
		apiRateLimit:   apiRateLimit,
		apiRateLimits:  apiRateLimits,
	})
}
