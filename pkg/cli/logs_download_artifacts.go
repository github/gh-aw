// This file provides command-line interface functionality for gh-aw.
// This file (logs_download_artifacts.go) contains functions for discovering,
// filtering, and downloading individual workflow run artifacts by name via
// the GitHub CLI.

package cli

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/fileutil"
	"github.com/github/gh-aw/pkg/workflow"
)

// buildRepoFlag returns the "-R" flag value for gh commands given the owner,
// repo, and optional hostname. Returns an empty string when owner or repo is
// unset (the gh CLI will infer the repository from git context in that case).
func buildRepoFlag(owner, repo, hostname string) string {
	if owner == "" || repo == "" {
		return ""
	}
	if hostname != "" && hostname != "github.com" {
		return path.Join(hostname, owner, repo)
	}
	return path.Join(owner, repo)
}

// listArtifacts creates a list of all artifact files in the output directory
func listArtifacts(outputDir string) ([]string, error) {
	var artifacts []string

	walkErr := filepath.Walk(outputDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		// Get relative path from outputDir
		relPath, err := filepath.Rel(outputDir, path)
		if err != nil {
			return err
		}
		// Skip only root-level synthesized cache/summary files.
		if relPath == runSummaryFileName || relPath == jobsAPIResponseFileName || relPath == runAPIResponseFileName {
			return nil
		}

		artifacts = append(artifacts, relPath)
		return nil
	})

	if walkErr != nil {
		return nil, walkErr
	}

	return artifacts, nil
}

// isNonZipArtifactError reports whether the output from gh run download indicates
// that the failure was caused by one or more non-zip artifacts (e.g. .dockerbuild files).
// Such artifacts cannot be extracted as zip archives and should be skipped rather than
// failing the entire download.
func isNonZipArtifactError(output []byte) bool {
	s := string(output)
	return strings.Contains(s, "zip: not a valid zip file")
}

// isCaseCollisionArtifactError reports whether gh run download failed because
// a zip extraction attempted to write a file that already exists. This can
// happen on case-insensitive filesystems (e.g. macOS) when an artifact
// contains files whose names differ only by case.
func isCaseCollisionArtifactError(output []byte) bool {
	s := string(output)
	return strings.Contains(s, "error extracting zip archive") && strings.Contains(s, "file exists")
}

// isDockerBuildArtifact reports whether an artifact name represents a .dockerbuild artifact.
// These are not zip archives and cannot be extracted by gh run download.
func isDockerBuildArtifact(name string) bool {
	return strings.HasSuffix(name, ".dockerbuild")
}

// listRunArtifactNames returns the names of all artifacts for the given workflow run
// by querying the GitHub Actions API. Returns an error if the API call fails.
func listRunArtifactNames(ctx context.Context, runID int64, owner, repo, hostname string, verbose bool) ([]string, error) {
	var endpoint string
	if owner != "" && repo != "" {
		endpoint = fmt.Sprintf("repos/%s/%s/actions/runs/%d/artifacts", owner, repo, runID)
	} else {
		endpoint = fmt.Sprintf("repos/{owner}/{repo}/actions/runs/%d/artifacts", runID)
	}

	args := []string{"api", "--paginate", endpoint, "--jq", ".artifacts[].name"}
	if hostname != "" && hostname != "github.com" {
		args = append(args, "--hostname", hostname)
	}

	logsDownloadLog.Printf("Listing artifacts for run %d: gh %s", runID, strings.Join(args, " "))
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatVerboseMessage("Listing artifacts: gh "+strings.Join(args, " ")))
	}

	cmd := workflow.ExecGHContext(ctx, args...)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to list artifacts for run %d: %w", runID, err)
	}

	var names []string
	for line := range strings.SplitSeq(strings.TrimSpace(string(output)), "\n") {
		name := strings.TrimSpace(line)
		if name != "" {
			names = append(names, name)
		}
	}
	return names, nil
}

// downloadArtifactsByName downloads a list of artifacts individually by name.
// This is used when some artifacts (e.g. .dockerbuild) need to be skipped and
// only a subset of the run's artifacts should be downloaded.
func downloadArtifactsByName(ctx context.Context, opts downloadArtifactsOptions, names []string) error {
	repoFlag := buildRepoFlag(opts.owner, opts.repo, opts.hostname)
	shouldLogProgress := IsRunningInCI() || opts.verbose

	for _, name := range names {
		if err := validateArtifactName(name); err != nil {
			return err
		}
		// Stage next to the output directory so promotion can use an atomic same-filesystem rename.
		stagingDir, err := os.MkdirTemp(filepath.Dir(opts.outputDir), "."+filepath.Base(opts.outputDir)+"-"+name+"-")
		if err != nil {
			return fmt.Errorf("failed to create staging directory for artifact %q: %w", name, err)
		}

		artifactDir := filepath.Join(opts.outputDir, name)
		args := []string{"run", "download", strconv.FormatInt(opts.runID, 10), "--name", name, "--dir", stagingDir}
		if repoFlag != "" {
			args = append(args, "-R", repoFlag)
		}

		logsDownloadLog.Printf("Downloading artifact %q individually: gh %s", name, strings.Join(args, " "))
		if shouldLogProgress {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Downloading artifact: "+name))
		}

		cmd := workflow.ExecGHContext(ctx, args...)
		cmdOutput, cmdErr := cmd.CombinedOutput()
		if cmdErr != nil {
			_ = os.RemoveAll(stagingDir)
			logsDownloadLog.Printf("Failed to download artifact %q: %v (%s)", name, cmdErr, string(cmdOutput))
			if opts.verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to download artifact %q: %v", name, cmdErr)))
			}
			// Non-fatal: continue downloading other artifacts
		} else {
			logsDownloadLog.Printf("Downloaded artifact %q", name)
			if err := os.RemoveAll(artifactDir); err != nil {
				_ = os.RemoveAll(stagingDir)
				return fmt.Errorf("failed to remove existing artifact directory %q: %w", artifactDir, err)
			}
			if err := os.Rename(stagingDir, artifactDir); err != nil {
				_ = os.RemoveAll(stagingDir)
				return fmt.Errorf("failed to promote artifact %q from staging: %w", name, err)
			}
			if err := markArtifactDownloaded(opts.outputDir, name); err != nil {
				return err
			}
		}
	}

	return nil
}

func validateArtifactName(name string) error {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\`) || filepath.Base(name) != name {
		return fmt.Errorf("invalid artifact name %q", name)
	}
	return nil
}

// criticalArtifactNames lists the artifact names that are essential for audit analysis.
// When a bulk download fails partially (e.g., due to non-zip artifacts), these artifacts
// are retried individually so that flattening and audit extraction have data to work with.
var criticalArtifactNames = []string{constants.ActivationArtifactName.String(), constants.AgentArtifactName.String(), constants.AgentOutputFallbackArtifactName.String()}

// retryCriticalArtifacts downloads critical artifacts individually when the bulk download
// was only partially successful. gh run download aborts on the first non-zip artifact,
// which may prevent valid artifacts from being downloaded.
// artifactFilter limits which critical artifacts are retried; nil means retry all.
func retryCriticalArtifacts(ctx context.Context, opts downloadArtifactsOptions) {
	// Build the repo flag once for reuse across retries
	repoFlag := buildRepoFlag(opts.owner, opts.repo, opts.hostname)

	for _, name := range criticalArtifactNames {
		if err := validateArtifactName(name); err != nil {
			logsDownloadLog.Printf("Skipping invalid critical artifact name: %v", err)
			continue
		}
		// Skip artifacts not included in the active filter.
		if !artifactMatchesFilter(name, opts.artifactFilter) {
			logsDownloadLog.Printf("Skipping critical artifact %q (not in artifact filter)", name)
			continue
		}
		artifactDir := filepath.Join(opts.outputDir, name)
		if fileutil.DirExists(artifactDir) {
			logsDownloadLog.Printf("Critical artifact %q already present, skipping retry", name)
			continue
		}
		retryCriticalArtifact(ctx, opts, repoFlag, name, artifactDir)
	}
}

func retryCriticalArtifact(ctx context.Context, opts downloadArtifactsOptions, repoFlag, name, artifactDir string) {
	// Stage next to the output directory so promotion can use an atomic same-filesystem rename.
	stagingDir, err := os.MkdirTemp(filepath.Dir(opts.outputDir), "."+filepath.Base(opts.outputDir)+"-"+name+"-")
	if err != nil {
		logsDownloadLog.Printf("Failed to create staging directory for critical artifact %q: %v", name, err)
		return
	}

	retryArgs := []string{"run", "download", strconv.FormatInt(opts.runID, 10), "--name", name, "--dir", stagingDir}
	if repoFlag != "" {
		retryArgs = append(retryArgs, "-R", repoFlag)
	}

	logsDownloadLog.Printf("Retrying individual download for artifact %q: gh %s", name, strings.Join(retryArgs, " "))
	if opts.verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Retrying download for missing artifact: "+name))
	}

	retryCmd := workflow.ExecGHContext(ctx, retryArgs...)
	retryOutput, retryErr := retryCmd.CombinedOutput()
	if retryErr != nil {
		_ = os.RemoveAll(stagingDir)
		logsDownloadLog.Printf("Failed to download artifact %q individually: %v (%s)", name, retryErr, string(retryOutput))
		if opts.verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Could not download artifact %q: %v", name, retryErr)))
		}
		return
	}

	logsDownloadLog.Printf("Successfully downloaded artifact %q individually", name)
	if err := os.RemoveAll(artifactDir); err != nil {
		_ = os.RemoveAll(stagingDir)
		logsDownloadLog.Printf("Failed to remove existing critical artifact directory %q: %v", artifactDir, err)
		return
	}
	if err := os.Rename(stagingDir, artifactDir); err != nil {
		_ = os.RemoveAll(stagingDir)
		logsDownloadLog.Printf("Failed to promote critical artifact %q from staging: %v", name, err)
		return
	}
	// Marker write failures are non-fatal in the retry path: retryCriticalArtifacts
	// is a best-effort recovery after a partial bulk download, so a missing marker
	// only causes a redundant re-download on the next run (not data loss).
	if err := markArtifactDownloaded(opts.outputDir, name); err != nil {
		logsDownloadLog.Printf("Failed to mark artifact %q as downloaded: %v", name, err)
	}
	if opts.verbose {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Downloaded missing artifact: "+name))
	}
}

// logVerboseDownloadSummary prints a success message and a shallow enumeration of the
// files created under opts.outputDir. It is only invoked when verbose mode is enabled.
func logVerboseDownloadSummary(opts downloadArtifactsOptions) {
	fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("Downloaded artifacts for run %d to %s", opts.runID, opts.outputDir)))
	// Enumerate created files (shallow + summary) for immediate visibility
	var fileCount int
	var firstFiles []string
	var walkFailed bool
	if walkErr := filepath.Walk(opts.outputDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			logsDownloadLog.Printf("walk error at %s: %v", path, err)
			walkFailed = true
			return nil
		}
		if info.IsDir() {
			return nil
		}
		fileCount++
		if len(firstFiles) < 12 { // capture a reasonable preview
			rel, relErr := filepath.Rel(opts.outputDir, path)
			if relErr == nil {
				firstFiles = append(firstFiles, rel)
			}
		}
		return nil
	}); walkErr != nil {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("filesystem error enumerating artifacts in %s: %v", opts.outputDir, walkErr)))
	}
	if fileCount == 0 {
		if walkFailed {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Download completed but artifact files could not be enumerated (filesystem error)"))
		} else {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Download completed but no artifact files were created (empty run)"))
		}
	} else {
		fmt.Fprintln(os.Stderr, console.FormatVerboseMessage(fmt.Sprintf("Artifact file count: %d", fileCount)))
		for _, f := range firstFiles {
			fmt.Fprintln(os.Stderr, console.FormatVerboseMessage("  • "+f))
		}
		if fileCount > len(firstFiles) {
			fmt.Fprintln(os.Stderr, console.FormatVerboseMessage(fmt.Sprintf("  … %d more files omitted", fileCount-len(firstFiles))))
		}
	}
}
