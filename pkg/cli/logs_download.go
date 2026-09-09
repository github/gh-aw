// This file provides command-line interface functionality for gh-aw.
// This file (logs_download.go) contains functions for downloading and extracting
// GitHub Actions workflow artifacts and logs.
//
// Key responsibilities:
//   - Downloading workflow run artifacts via gh CLI
//   - Extracting and organizing zip archives
//   - Flattening single-file artifact directories
//   - Managing local file system operations

package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/github/gh-aw/pkg/constants"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/errorutil"
	"github.com/github/gh-aw/pkg/fileutil"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/github/gh-aw/pkg/workflow"
)

var logsDownloadLog = logger.New("cli:logs_download")

// downloadArtifactsOptions bundles the common parameters shared by the artifact
// download helpers, avoiding repeated positional argument lists.
// runID identifies the workflow run; outputDir is the local destination directory;
// verbose enables progress messages; owner, repo, hostname identify the GitHub repository;
// artifactFilter is an optional list of artifact base names to download (nil means all).
type downloadArtifactsOptions struct {
	runID          int64
	outputDir      string
	verbose        bool
	owner          string
	repo           string
	hostname       string
	artifactFilter []string
}

// isUsageOnlyArtifactFilter reports whether the caller requested only the compact
// usage artifact. In this mode, workflow-run log downloads are intentionally skipped
// to minimize API and transfer volume for lightweight reporting paths.
func isUsageOnlyArtifactFilter(artifactFilter []string) bool {
	return len(artifactFilter) == 1 && artifactFilter[0] == constants.UsageArtifactName.String()
}

func shouldDownloadWorkflowRunLogs(artifactFilter []string) bool {
	if len(artifactFilter) == 0 {
		return true
	}
	for _, artifact := range artifactFilter {
		if artifact != constants.ActivationArtifactName.String() && artifact != constants.UsageArtifactName.String() {
			return true
		}
	}
	return false
}

// fetchWorkflowRunLogsArchive downloads the raw workflow run logs zip content via the
// GitHub API. The returned bool is false when the logs are unavailable (not found or
// expired), which callers treat as a non-critical condition.
func fetchWorkflowRunLogsArchive(ctx context.Context, runID int64, verbose bool, owner, repo, hostname string) ([]byte, bool, error) {
	// Use gh api to download the logs zip file
	// The endpoint returns a 302 redirect to the actual zip file
	var endpoint string
	if owner != "" && repo != "" {
		endpoint = fmt.Sprintf("repos/%s/%s/actions/runs/%d/logs", owner, repo, runID)
	} else {
		endpoint = fmt.Sprintf("repos/{owner}/{repo}/actions/runs/%d/logs", runID)
	}

	args := []string{"api", endpoint}
	if hostname != "" && hostname != "github.com" {
		args = append(args, "--hostname", hostname)
	}

	output, err := workflow.RunGHContext(ctx, "Downloading workflow logs...", args...)
	if err != nil {
		// Check for authentication errors
		if isPermissionError(err) {
			return nil, false, errors.New("GitHub CLI authentication required. Run 'gh auth login' first")
		}
		// If logs are not found or run has no logs, this is not a critical error.
		// Check both the Go error (via errorutil.IsNotFoundError) and the raw CLI output,
		// as the gh CLI may write "not found" to stdout without reflecting it in the error.
		// Also treat HTTP 410 Gone as non-critical (logs may be expired).
		if errorutil.IsNotFoundError(err) || errorutil.IsNotFoundOutput(string(output)) || errorutil.IsGoneError(err) {
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("No logs found for run %d (may be expired or unavailable)", runID)))
			}
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("failed to download workflow run logs for run %d: %w", runID, err)
	}
	return output, true, nil
}

// downloadWorkflowRunLogs downloads and unzips workflow run logs using GitHub API
func downloadWorkflowRunLogs(ctx context.Context, runID int64, outputDir string, verbose bool, owner, repo, hostname string) error {
	logsDownloadLog.Printf("Downloading workflow run logs: run_id=%d, output_dir=%s, owner=%s, repo=%s", runID, outputDir, owner, repo)

	// Create a temporary file for the zip download
	tmpZip := filepath.Join(os.TempDir(), fmt.Sprintf("workflow-logs-%d.zip", runID))
	defer os.RemoveAll(tmpZip)

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Downloading workflow run logs for run %d...", runID)))
	}

	output, ok, err := fetchWorkflowRunLogsArchive(ctx, runID, verbose, owner, repo, hostname)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}

	// Write the downloaded zip content to temporary file
	if err := os.WriteFile(tmpZip, output, constants.FilePermPublic); err != nil {
		return fmt.Errorf("failed to write logs zip file: %w", err)
	}

	// Create a subdirectory for workflow logs to keep the run directory organized
	workflowLogsDir := filepath.Join(outputDir, "workflow-logs")
	if err := os.MkdirAll(workflowLogsDir, constants.DirPermPublic); err != nil {
		return fmt.Errorf("failed to create workflow-logs directory: %w", err)
	}

	// Unzip the logs into the workflow-logs subdirectory
	if err := unzipFile(tmpZip, workflowLogsDir, verbose); err != nil {
		return fmt.Errorf("failed to unzip workflow logs: %w", err)
	}

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Downloaded and extracted workflow run logs to "+workflowLogsDir))
	}

	return nil
}

// downloadRunArtifacts downloads artifacts for a specific workflow run.
// artifactFilter is a list of artifact base names to download; nil means download all.
func downloadRunArtifacts(ctx context.Context, opts downloadArtifactsOptions) error {
	logsDownloadLog.Printf("Downloading run artifacts: run_id=%d, output_dir=%s, owner=%s, repo=%s, artifactFilter=%v", opts.runID, opts.outputDir, opts.owner, opts.repo, opts.artifactFilter)
	shouldLogProgress := IsRunningInCI() || opts.verbose

	// Check if artifacts already exist on disk (since they're immutable)
	if fileutil.DirExists(opts.outputDir) && !fileutil.IsDirEmpty(opts.outputDir) {
		filter, done := resolveCachedArtifacts(ctx, opts, shouldLogProgress)
		if done {
			return nil
		}
		opts.artifactFilter = filter
	}

	if err := os.MkdirAll(opts.outputDir, constants.DirPermPublic); err != nil {
		return fmt.Errorf("failed to create run output directory: %w", err)
	}
	if opts.verbose {
		fmt.Fprintln(os.Stderr, console.FormatVerboseMessage("Created output directory "+opts.outputDir))
	}

	downloadableNames, individualDownload, done, err := planArtifactDownload(ctx, opts, shouldLogProgress)
	if done || err != nil {
		return err
	}

	// Start spinner for network operation
	spinner := console.NewSpinner(fmt.Sprintf("Downloading artifacts for run %d...", opts.runID))
	if !opts.verbose {
		spinner.Start()
	}

	if individualDownload {
		if !opts.verbose {
			spinner.Stop()
		}
		if err := downloadArtifactsIndividually(ctx, opts, downloadableNames); err != nil {
			return err
		}
	} else {
		// No .dockerbuild artifacts detected (or listing failed) — use efficient bulk download.
		if err := bulkDownloadArtifacts(ctx, opts, spinner, downloadableNames); err != nil {
			if !opts.verbose {
				spinner.Stop()
			}
			return err
		}
	}

	// Stop spinner with success message
	if !opts.verbose {
		spinner.StopWithMessage(fmt.Sprintf("✓ Downloaded artifacts for run %d", opts.runID))
	}

	return finalizeArtifactDownload(ctx, opts)
}

// planArtifactDownload determines which artifacts must be downloaded and whether they have
// to be fetched individually rather than through the bulk downloader.
// done is true when nothing needs to be downloaded.
func planArtifactDownload(ctx context.Context, opts downloadArtifactsOptions, shouldLogProgress bool) (names []string, individual bool, done bool, err error) {
	// Proactively list artifacts to detect .dockerbuild files that gh run download cannot
	// extract (they are not zip archives). When found, skip them and download the
	// remaining artifacts individually so the bulk download never encounters them.
	downloadableNames, dockerBuildCount, listErr := enumerateDownloadableArtifacts(ctx, opts)

	// Incremental download: when the output directory already holds some artifacts but
	// lacks the complete-download marker, check which artifacts are actually missing and
	// restrict the download to those.  This avoids re-fetching data that was already
	// transferred during a previous filtered pass (e.g. activation+usage) even when the
	// caller now requests the full artifact set.
	var incrementalUnfilteredDownload bool
	if listErr == nil && len(downloadableNames) > 0 && len(opts.artifactFilter) == 0 &&
		fileutil.DirExists(opts.outputDir) && !fileutil.IsDirEmpty(opts.outputDir) {
		incrementalNames, incremental, incrementalDone, incrementalErr := planIncrementalDownload(opts, downloadableNames, shouldLogProgress)
		if incrementalDone || incrementalErr != nil {
			return nil, false, true, incrementalErr
		}
		downloadableNames = incrementalNames
		incrementalUnfilteredDownload = incremental
	}

	// When .dockerbuild artifacts are present, an artifact filter is active, or an
	// incremental top-up is needed, the artifacts must be downloaded individually:
	// the bulk downloader (gh run download without --name) cannot apply a name filter,
	// and it aborts on non-zip artifacts.
	individual = dockerBuildCount > 0 || len(opts.artifactFilter) > 0 || incrementalUnfilteredDownload
	return downloadableNames, individual, false, nil
}

// finalizeArtifactDownload flattens the downloaded artifacts and fetches workflow run logs.
func finalizeArtifactDownload(ctx context.Context, opts downloadArtifactsOptions) error {
	if err := flattenDownloadedArtifacts(ctx, opts); err != nil {
		return err
	}

	// Download and unzip workflow run logs unless caller requested usage-only mode.
	if shouldDownloadWorkflowRunLogs(opts.artifactFilter) {
		if err := downloadWorkflowRunLogs(ctx, opts.runID, opts.outputDir, opts.verbose, opts.owner, opts.repo, opts.hostname); err != nil {
			// Log the error but don't fail the entire download process
			// Logs may not be available for all runs (e.g., expired or deleted)
			if opts.verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to download workflow run logs: %v", err)))
			}
		}
	}

	if opts.verbose {
		logVerboseDownloadSummary(opts)
	}

	return nil
}

// resolveCachedArtifacts inspects the existing run directory and decides whether the
// download can be skipped entirely (done=true) or narrowed to the artifacts that are
// still missing (returned as the new artifact filter).
func resolveCachedArtifacts(ctx context.Context, opts downloadArtifactsOptions, shouldLogProgress bool) ([]string, bool) {
	if len(opts.artifactFilter) > 0 {
		// A specific artifact set is requested. Check whether each requested
		// artifact base name already has a matching directory on disk so we
		// can avoid re-downloading artifacts that are already present and only
		// fetch the ones that are missing.
		missing := findMissingFilterEntries(opts.artifactFilter, opts.outputDir)
		if len(missing) == 0 {
			logsDownloadLog.Printf("All requested artifacts already on disk for run %d", opts.runID)
			if shouldLogProgress {
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("All requested artifacts already present for run %d, skipping download", opts.runID)))
			}
			ensureUsageAwInfoFallback(ctx, opts)
			return opts.artifactFilter, true
		}
		// Restrict the download to only the artifacts that are not yet on disk.
		logsDownloadLog.Printf("Downloading missing artifacts for run %d: %v (already have: %v)", opts.runID, missing, opts.artifactFilter)
		if shouldLogProgress {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Downloading missing artifacts for run %d: %v", opts.runID, missing)))
		}
		// Fall through to the download code (MkdirAll is a no-op for existing dir).
		return missing, false
	}

	// No filter — caller wants all artifacts. The complete-download marker is
	// sufficient to skip the download; no cached summary is required because
	// the marker itself guarantees all artifact data is present on disk.
	if len(findMissingFilterEntries([]string{string(ArtifactSetAll)}, opts.outputDir)) == 0 {
		logsDownloadLog.Printf("Using cached artifacts for run %d", opts.runID)
		if shouldLogProgress {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("All artifacts already present for run %d, skipping download", opts.runID)))
		}
		return opts.artifactFilter, true
	}
	if opts.verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Run folder for %d is missing the complete artifact marker; downloading all artifacts", opts.runID)))
	}
	return opts.artifactFilter, false
}

// enumerateDownloadableArtifacts lists the run artifacts and splits them into the names
// that can be downloaded (matching the filter) and the count of .dockerbuild artifacts
// that must be skipped because they are not valid zip archives.
func enumerateDownloadableArtifacts(ctx context.Context, opts downloadArtifactsOptions) ([]string, int, error) {
	artifactNames, listErr := listRunArtifactNames(ctx, opts.runID, opts.owner, opts.repo, opts.hostname, opts.verbose)
	if listErr != nil {
		if len(opts.artifactFilter) > 0 {
			// When artifact listing fails, still try the requested artifact names directly
			// so filtered downloads don't incorrectly return ErrNoArtifacts.
			logsDownloadLog.Printf("Could not list artifacts (will try requested artifact names directly): %v", listErr)
			return append([]string(nil), opts.artifactFilter...), 0, listErr
		}
		logsDownloadLog.Printf("Could not list artifacts (will use bulk download): %v", listErr)
		return nil, 0, listErr
	}

	var dockerBuildArtifacts, downloadableNames []string
	for _, name := range artifactNames {
		if isDockerBuildArtifact(name) {
			dockerBuildArtifacts = append(dockerBuildArtifacts, name)
		} else if artifactMatchesFilter(name, opts.artifactFilter) {
			downloadableNames = append(downloadableNames, name)
		}
	}
	if len(dockerBuildArtifacts) > 0 {
		skipDockerBuildMessage := fmt.Sprintf("Skipping %d .dockerbuild artifact(s) (not valid zip archives): %s", len(dockerBuildArtifacts), strings.Join(dockerBuildArtifacts, ", "))
		logsDownloadLog.Printf("Found %d .dockerbuild artifact(s) that will be skipped: %v", len(dockerBuildArtifacts), dockerBuildArtifacts)
		if opts.verbose {
			fmt.Fprintln(os.Stderr, console.FormatVerboseMessage(skipDockerBuildMessage))
		}
	}
	return downloadableNames, len(dockerBuildArtifacts), nil
}

// planIncrementalDownload narrows an unfiltered download to the artifacts that are still
// missing from an existing run directory. done is true when nothing needs downloading.
func planIncrementalDownload(opts downloadArtifactsOptions, downloadableNames []string, shouldLogProgress bool) (names []string, incremental bool, done bool, err error) {
	missingNames := findMissingFilterEntries(downloadableNames, opts.outputDir)
	if len(missingNames) == 0 {
		// All artifacts are already on disk.  Confirm with the complete-download marker
		// so that future unfiltered requests benefit from the fast-path check above.
		logsDownloadLog.Printf("All %d artifacts already present for run %d (incremental check)", len(downloadableNames), opts.runID)
		if shouldLogProgress {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("All artifacts already present for run %d, skipping download", opts.runID)))
		}
		if markerErr := markArtifactDownloaded(opts.outputDir, string(ArtifactSetAll)); markerErr != nil {
			return nil, false, true, markerErr
		}
		return nil, false, true, nil
	}
	if len(missingNames) < len(downloadableNames) {
		// Some artifacts are already present; narrow the download to the remainder.
		logsDownloadLog.Printf("Incremental download for run %d: %d/%d artifacts missing: %v",
			opts.runID, len(missingNames), len(downloadableNames), missingNames)
		if shouldLogProgress {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf(
				"Incremental download for run %d: fetching %d missing artifact(s): %v",
				opts.runID, len(missingNames), missingNames)))
		}
		return missingNames, true, false, nil
	}
	return downloadableNames, false, false, nil
}

// downloadArtifactsIndividually downloads the selected artifacts one by one, which is
// required when .dockerbuild artifacts are present, a filter is active, or only a subset
// of artifacts is missing.
func downloadArtifactsIndividually(ctx context.Context, opts downloadArtifactsOptions, downloadableNames []string) error {
	if len(downloadableNames) == 0 {
		// Nothing to download (all artifacts are either .dockerbuild or excluded by filter).
		// For usage-only mode, skip workflow logs entirely to keep downloads lightweight.
		if shouldDownloadWorkflowRunLogs(opts.artifactFilter) {
			// Attempt workflow run logs for diagnostics before returning.
			downloadWorkflowRunLogsForDiagnostics(ctx, opts)
		}
		return ErrNoArtifacts
	}
	if err := downloadArtifactsByName(ctx, opts, downloadableNames); err != nil {
		return err
	}
	artifacts, err := listArtifacts(opts.outputDir)
	if err != nil {
		return err
	}
	if len(artifacts) == 0 {
		// Downloads were attempted but none succeeded; treat as no artifacts.
		return ErrNoArtifacts
	}
	// Write the complete-download marker when this was an unfiltered request routed
	// through the individual download path (dockerbuild artifacts present, or an
	// incremental top-up of an existing run directory).  Only mark as complete when
	// all expected artifacts are actually present, so that partially-failed downloads
	// are retried next time rather than treated as complete.
	if len(opts.artifactFilter) == 0 {
		if len(findMissingFilterEntries(downloadableNames, opts.outputDir)) == 0 {
			if markerErr := markArtifactDownloaded(opts.outputDir, string(ArtifactSetAll)); markerErr != nil {
				return markerErr
			}
		}
	}
	return nil
}

// downloadWorkflowRunLogsForDiagnostics downloads workflow run logs on a best-effort
// basis so that pre-agent step failures can still be diagnosed when no artifacts exist.
// The run directory is removed when it remains empty.
func downloadWorkflowRunLogsForDiagnostics(ctx context.Context, opts downloadArtifactsOptions) {
	logErr := downloadWorkflowRunLogs(ctx, opts.runID, opts.outputDir, opts.verbose, opts.owner, opts.repo, opts.hostname)
	if logErr == nil {
		return
	}
	if opts.verbose {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to download workflow run logs: %v", logErr)))
	}
	// Clean up empty directory only if logs download also produced nothing
	if fileutil.IsDirEmpty(opts.outputDir) {
		if removeErr := os.RemoveAll(opts.outputDir); removeErr != nil && opts.verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to clean up empty directory %s: %v", opts.outputDir, removeErr)))
		}
	}
}

// buildBulkDownloadArgs builds the gh run download command arguments, including optional
// repo/hostname overrides for cross-repo and multi-host support.
func buildBulkDownloadArgs(opts downloadArtifactsOptions) []string {
	ghArgs := []string{"run", "download", strconv.FormatInt(opts.runID, 10), "--dir", opts.outputDir}
	if opts.owner != "" && opts.repo != "" {
		if opts.hostname != "" && opts.hostname != "github.com" {
			ghArgs = append(ghArgs, "-R", filepath.Join(opts.hostname, opts.owner, opts.repo))
		} else {
			ghArgs = append(ghArgs, "-R", filepath.Join(opts.owner, opts.repo))
		}
	}
	return ghArgs
}

// bulkDownloadArtifacts downloads all artifacts of a run in a single gh run download call,
// recovering individually from non-zip and case-colliding artifacts when possible.
func bulkDownloadArtifacts(ctx context.Context, opts downloadArtifactsOptions, spinner *console.SpinnerWrapper, downloadableNames []string) error {
	ghArgs := buildBulkDownloadArgs(opts)

	if opts.verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Executing: gh "+strings.Join(ghArgs, " ")))
	}

	cmd := workflow.ExecGHContext(ctx, ghArgs...)
	output, err := cmd.CombinedOutput()

	// skippedNonZipArtifacts is set when gh run download fails due to non-zip artifacts
	// that were not detected during the listing phase (e.g., listing failed).
	var skippedNonZipArtifacts bool
	// skippedCaseCollisionArtifacts is set when gh run download fails because a single
	// artifact contains case-colliding paths on case-insensitive filesystems.
	var skippedCaseCollisionArtifacts bool

	if err != nil {
		// Stop spinner on error
		if !opts.verbose {
			spinner.Stop()
		}
		if opts.verbose {
			fmt.Fprintln(os.Stderr, console.FormatVerboseMessage(string(output)))
		}

		skippedNonZipArtifacts, skippedCaseCollisionArtifacts, err = classifyBulkDownloadError(ctx, opts, output, err)
		if err != nil {
			return err
		}
	}

	return recoverBulkDownloadArtifacts(ctx, opts, downloadableNames, skippedNonZipArtifacts, skippedCaseCollisionArtifacts)
}

// recoverBulkDownloadArtifacts retries artifacts individually after a partially failed bulk
// download and writes the complete-download marker when the run directory is usable.
func recoverBulkDownloadArtifacts(ctx context.Context, opts downloadArtifactsOptions, downloadableNames []string, skippedNonZipArtifacts, skippedCaseCollisionArtifacts bool) error {
	// When the bulk download failed due to non-zip artifacts, gh CLI may have aborted
	// before downloading all valid artifacts. Retry individually for critical artifacts
	// that are missing, so flattening and audit analysis can proceed.
	if skippedNonZipArtifacts {
		retryCriticalArtifacts(ctx, opts)
	}

	// When bulk download fails on case-colliding entries, gh CLI aborts and may skip
	// artifacts that appear later in the run. Retry artifacts individually so one bad
	// artifact does not block the rest.
	if skippedCaseCollisionArtifacts {
		if retryErr := retryCaseCollisionArtifacts(ctx, opts, downloadableNames); retryErr != nil {
			return retryErr
		}
	}

	artifacts, err := listArtifacts(opts.outputDir)
	if err != nil {
		return err
	}
	if skippedNonZipArtifacts && len(artifacts) == 0 {
		// All artifacts were non-zip (none could be extracted) so nothing was downloaded.
		// Treat this the same as a run with no artifacts — the audit will rely solely on
		// workflow logs rather than artifact content.
		return ErrNoArtifacts
	}
	// Write the complete-download marker when the bulk download succeeded with no
	// errors, when some non-zip artifacts were skipped but critical artifacts were
	// recovered via retryCriticalArtifacts, or when a case-collision caused the bulk
	// download to abort but all artifacts were successfully retried individually.
	// In all three cases the directory is non-empty (guarded above for non-zip),
	// so marking prevents an unbounded re-download loop on subsequent runs.
	if markerErr := markArtifactDownloaded(opts.outputDir, string(ArtifactSetAll)); markerErr != nil {
		return markerErr
	}
	return nil
}

// classifyBulkDownloadError inspects a failed gh run download invocation and reports
// whether the failure can be recovered from by retrying artifacts individually.
// err is the error from CombinedOutput and is used to detect authentication failures;
// output is the combined stdout and stderr from the same invocation.
// A non-nil returned error means the failure is fatal for this run.
func classifyBulkDownloadError(ctx context.Context, opts downloadArtifactsOptions, output []byte, err error) (skippedNonZip bool, skippedCaseCollision bool, fatal error) {
	// Check if it's because there are no artifacts
	if strings.Contains(string(output), "no valid artifacts") || strings.Contains(string(output), "not found") {
		if opts.verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("No artifacts found for run %d (gh run download reported none)", opts.runID)))
		}
		// Even with no artifacts, attempt to download workflow run logs so that
		// pre-agent step failures (e.g., activation job errors) can be diagnosed.
		downloadWorkflowRunLogsForDiagnostics(ctx, opts)
		return false, false, ErrNoArtifacts
	}
	// Check for authentication errors
	if isPermissionError(err) {
		return false, false, errors.New("GitHub CLI authentication required. Run 'gh auth login' first")
	}
	// Check if the error is due to non-zip artifacts (e.g., .dockerbuild files).
	// The gh CLI fails when it encounters artifacts that are not valid zip archives.
	// We warn and continue with any artifacts that were successfully downloaded.
	if isNonZipArtifactError(output) {
		// Show a concise warning; preserve legacy behavior of 200 chars + "...".
		msg := stringutil.Truncate(string(output), 203)
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Some artifacts could not be extracted (not a valid zip archive) and were skipped: "+msg))
		return true, false, nil
	}
	if isCaseCollisionArtifactError(output) {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Some artifacts could not be fully extracted due to case-colliding file paths. Retrying artifacts individually and continuing."))
		return false, true, nil
	}
	return false, false, fmt.Errorf("failed to download artifacts for run %d: %w (output: %s)", opts.runID, err, string(output))
}

// retryCaseCollisionArtifacts re-downloads artifacts individually after a bulk download
// aborted on case-colliding file paths.
func retryCaseCollisionArtifacts(ctx context.Context, opts downloadArtifactsOptions, downloadableNames []string) error {
	retryNames := downloadableNames
	if len(retryNames) == 0 {
		// Initial artifact listing was unavailable, so fetch names now for targeted retry.
		artifactNamesRetry, retryListErr := listRunArtifactNames(ctx, opts.runID, opts.owner, opts.repo, opts.hostname, opts.verbose)
		if retryListErr != nil {
			return fmt.Errorf("bulk artifact download hit case-colliding entries and could not list artifacts for individual retry: %w", retryListErr)
		}
		for _, name := range artifactNamesRetry {
			if isDockerBuildArtifact(name) {
				continue
			}
			if artifactMatchesFilter(name, opts.artifactFilter) {
				retryNames = append(retryNames, name)
			}
		}
	}
	if len(retryNames) > 0 {
		if err := downloadArtifactsByName(ctx, opts, retryNames); err != nil {
			return err
		}
	}
	return nil
}

func ensureUsageAwInfoFallback(ctx context.Context, opts downloadArtifactsOptions) {
	if !isUsageOnlyArtifactFilter(opts.artifactFilter) {
		return
	}

	awInfoPath := filepath.Join(opts.outputDir, "aw_info.json")
	if fileutil.FileExists(awInfoPath) {
		return
	}
	if copyUsageAwInfoToRunRoot(opts.outputDir, awInfoPath) {
		return
	}

	logsDownloadLog.Printf("aw_info.json missing from usage artifact, downloading activation artifact as fallback")
	if opts.verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("aw_info.json missing from usage artifact; downloading activation artifact as fallback"))
	}

	activationNames := resolveActivationArtifactNames(ctx, opts)

	if err := downloadArtifactsByName(ctx, opts, activationNames); err != nil {
		logsDownloadLog.Printf("Activation artifact fallback download failed: %v", err)
		if opts.verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Could not download activation artifact fallback: %v", err)))
		}
	}
	flattenActivationFallback(opts, activationNames)
	if _, err := os.Stat(awInfoPath); os.IsNotExist(err) {
		logsDownloadLog.Print("aw_info.json still absent after activation artifact fallback")
		if opts.verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage("aw_info.json still absent after activation artifact fallback"))
		}
	}
}

// copyUsageAwInfoToRunRoot copies aw_info.json from the usage artifact directory to the
// run root when present. It reports whether the copy succeeded.
func copyUsageAwInfoToRunRoot(outputDir, awInfoPath string) bool {
	usageDir := findArtifactDir(outputDir, constants.UsageArtifactName.String(), "")
	if usageDir == "" {
		return false
	}
	usageAwInfoPath := filepath.Join(usageDir, "aw_info.json")
	if !fileutil.FileExists(usageAwInfoPath) {
		return false
	}
	data, err := os.ReadFile(usageAwInfoPath)
	if err != nil {
		logsDownloadLog.Printf("Failed to read usage aw_info.json: %v", err)
		return false
	}
	if err := os.WriteFile(awInfoPath, data, constants.FilePermPublic); err != nil {
		logsDownloadLog.Printf("Failed to copy usage aw_info.json to run root: %v", err)
		return false
	}
	logsDownloadLog.Printf("Copied usage aw_info.json to run root")
	return true
}

// resolveActivationArtifactNames determines the activation artifact names to download,
// falling back to the default name when artifact listing is unavailable.
func resolveActivationArtifactNames(ctx context.Context, opts downloadArtifactsOptions) []string {
	artifactNames, err := listRunArtifactNames(ctx, opts.runID, opts.owner, opts.repo, opts.hostname, opts.verbose)
	if err != nil {
		logsDownloadLog.Printf("Failed to list artifacts for activation fallback: %v", err)
		return []string{constants.ActivationArtifactName.String()}
	}
	var matched []string
	for _, name := range artifactNames {
		if artifactMatchesFilter(name, []string{constants.ActivationArtifactName.String()}) {
			matched = append(matched, name)
		}
	}
	if len(matched) == 0 {
		return []string{constants.ActivationArtifactName.String()}
	}
	return matched
}

// flattenActivationFallback flattens the first activation artifact directory found on disk
// after the fallback download attempt.
func flattenActivationFallback(opts downloadArtifactsOptions, activationNames []string) {
	for _, name := range activationNames {
		activationDir := findArtifactDir(opts.outputDir, name, "")
		if activationDir == "" {
			continue
		}
		logsDownloadLog.Printf("Found activation artifact fallback directory: %s", activationDir)
		if err := flattenActivationArtifact(opts.outputDir, opts.verbose); err != nil {
			logsDownloadLog.Printf("Failed to flatten fallback activation artifact: %v", err)
			if opts.verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to flatten activation artifact fallback: %v", err)))
			}
		}
		return
	}
	logsDownloadLog.Print("Activation artifact fallback directory not found after download attempt")
}
