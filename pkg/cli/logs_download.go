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
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/github/gh-aw/pkg/cli/fileutil"
	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/workflow"
)

var logsDownloadLog = logger.New("cli:logs_download")

// flattenSingleFileArtifacts checks artifact directories and flattens any that contain a single file
// This handles the case where gh CLI creates a directory for each artifact, even if it's just one file
func flattenSingleFileArtifacts(outputDir string, verbose bool) error {
	logsDownloadLog.Printf("Flattening single-file artifacts in: %s", outputDir)
	entries, err := os.ReadDir(outputDir)
	if err != nil {
		return fmt.Errorf("failed to read output directory: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		artifactDir := filepath.Join(outputDir, entry.Name())

		// Read contents of artifact directory
		artifactEntries, err := os.ReadDir(artifactDir)
		if err != nil {
			logsDownloadLog.Printf("Failed to read artifact directory %s: %v", artifactDir, err)
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to read artifact directory %s: %v", artifactDir, err)))
			}
			continue
		}

		logsDownloadLog.Printf("Artifact directory %s contains %d entries", entry.Name(), len(artifactEntries))

		// Apply unfold rule: Check if directory contains exactly one entry and it's a file
		if len(artifactEntries) != 1 {
			if verbose && len(artifactEntries) > 1 {
				// Log what's in multi-file artifacts for debugging
				var fileNames []string
				for _, e := range artifactEntries {
					fileNames = append(fileNames, e.Name())
				}
				logsDownloadLog.Printf("Artifact directory %s has %d files, not flattening: %v", entry.Name(), len(artifactEntries), fileNames)
			}
			continue
		}

		singleEntry := artifactEntries[0]
		if singleEntry.IsDir() {
			logsDownloadLog.Printf("Artifact directory %s contains a subdirectory, not flattening", entry.Name())
			continue
		}

		// Unfold: Move the single file to parent directory and remove the artifact folder
		sourcePath := filepath.Join(artifactDir, singleEntry.Name())
		destPath := filepath.Join(outputDir, singleEntry.Name())

		logsDownloadLog.Printf("Flattening: %s → %s", sourcePath, destPath)

		// Move the file to root (parent directory)
		if err := os.Rename(sourcePath, destPath); err != nil {
			logsDownloadLog.Printf("Failed to move file %s to %s: %v", sourcePath, destPath, err)
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to move file %s to %s: %v", sourcePath, destPath, err)))
			}
			continue
		}

		// Delete the now-empty artifact folder
		if err := os.Remove(artifactDir); err != nil {
			logsDownloadLog.Printf("Failed to remove empty directory %s: %v", artifactDir, err)
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to remove empty directory %s: %v", artifactDir, err)))
			}
			continue
		}

		logsDownloadLog.Printf("Successfully flattened: %s/%s → %s", entry.Name(), singleEntry.Name(), singleEntry.Name())
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatVerboseMessage(fmt.Sprintf("Unfolded single-file artifact: %s → %s", filepath.Join(entry.Name(), singleEntry.Name()), singleEntry.Name())))
		}
	}

	return nil
}

// flattenUnifiedArtifact flattens the unified agent-artifacts directory structure
// After artifact refactoring, files are stored directly in agent-artifacts/ without the tmp/gh-aw/ prefix
// This function moves those files to the root output directory and removes the nested structure
// For backward compatibility, it also handles the old structure (agent-artifacts/tmp/gh-aw/...)
func flattenUnifiedArtifact(outputDir string, verbose bool) error {
	agentArtifactsDir := filepath.Join(outputDir, "agent-artifacts")

	// Check if agent-artifacts directory exists
	if _, err := os.Stat(agentArtifactsDir); os.IsNotExist(err) {
		// No unified artifact, nothing to flatten
		return nil
	}

	logsDownloadLog.Printf("Flattening unified agent-artifacts directory: %s", agentArtifactsDir)

	// Check for old nested structure (agent-artifacts/tmp/gh-aw/)
	tmpGhAwPath := filepath.Join(agentArtifactsDir, "tmp", "gh-aw")
	hasOldStructure := false
	if _, err := os.Stat(tmpGhAwPath); err == nil {
		hasOldStructure = true
		logsDownloadLog.Printf("Found old artifact structure with tmp/gh-aw prefix")
	}

	// Determine the source path for flattening
	var sourcePath string
	if hasOldStructure {
		// Old structure: flatten from agent-artifacts/tmp/gh-aw/
		sourcePath = tmpGhAwPath
	} else {
		// New structure: flatten from agent-artifacts/ directly
		sourcePath = agentArtifactsDir
		logsDownloadLog.Printf("Found new artifact structure without tmp/gh-aw prefix")
	}

	// Walk through source path and move all files to root output directory
	err := filepath.Walk(sourcePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip the source directory itself
		if path == sourcePath {
			return nil
		}

		// Calculate relative path from source
		relPath, err := filepath.Rel(sourcePath, path)
		if err != nil {
			return fmt.Errorf("failed to get relative path for %s: %w", path, err)
		}

		destPath := filepath.Join(outputDir, relPath)

		if info.IsDir() {
			// Create directory in destination with owner+group permissions only (0750)
			if err := os.MkdirAll(destPath, 0750); err != nil {
				return fmt.Errorf("failed to create directory %s: %w", destPath, err)
			}
			logsDownloadLog.Printf("Created directory: %s", destPath)
		} else {
			// Move file to destination
			// Ensure parent directory exists with owner+group permissions only (0750)
			if err := os.MkdirAll(filepath.Dir(destPath), 0750); err != nil {
				return fmt.Errorf("failed to create parent directory for %s: %w", destPath, err)
			}

			if err := os.Rename(path, destPath); err != nil {
				return fmt.Errorf("failed to move file %s to %s: %w", path, destPath, err)
			}
			logsDownloadLog.Printf("Moved file: %s → %s", path, destPath)
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatVerboseMessage(fmt.Sprintf("Flattened: %s → %s", relPath, relPath)))
			}
		}

		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to flatten unified artifact: %w", err)
	}

	// Remove the now-empty agent-artifacts directory structure
	if err := os.RemoveAll(agentArtifactsDir); err != nil {
		logsDownloadLog.Printf("Failed to remove agent-artifacts directory %s: %v", agentArtifactsDir, err)
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to remove agent-artifacts directory: %v", err)))
		}
		// Don't fail the entire operation if cleanup fails
	} else {
		logsDownloadLog.Printf("Removed agent-artifacts directory: %s", agentArtifactsDir)
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatVerboseMessage("Flattened unified agent-artifacts and removed nested structure"))
		}
	}

	return nil
}

// flattenAgentOutputsArtifact flattens the agent_outputs artifact directory
// The agent_outputs artifact contains session logs with detailed token usage data
// that are critical for accurate token count parsing
func flattenAgentOutputsArtifact(outputDir string, verbose bool) error {
	agentOutputsDir := filepath.Join(outputDir, "agent_outputs")

	// Check if agent_outputs directory exists
	if _, err := os.Stat(agentOutputsDir); os.IsNotExist(err) {
		// No agent_outputs artifact, nothing to flatten
		logsDownloadLog.Print("No agent_outputs artifact found (session logs may be missing)")
		return nil
	}

	logsDownloadLog.Printf("Flattening agent_outputs directory: %s", agentOutputsDir)

	// Walk through agent_outputs and move all files to root output directory
	err := filepath.Walk(agentOutputsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip the root directory itself
		if path == agentOutputsDir {
			return nil
		}

		// Calculate relative path from agent_outputs
		relPath, err := filepath.Rel(agentOutputsDir, path)
		if err != nil {
			return fmt.Errorf("failed to get relative path for %s: %w", path, err)
		}

		destPath := filepath.Join(outputDir, relPath)

		if info.IsDir() {
			// Create directory in destination
			if err := os.MkdirAll(destPath, 0750); err != nil {
				return fmt.Errorf("failed to create directory %s: %w", destPath, err)
			}
			logsDownloadLog.Printf("Created directory: %s", destPath)
		} else {
			// Move file to destination
			// Ensure parent directory exists
			if err := os.MkdirAll(filepath.Dir(destPath), 0750); err != nil {
				return fmt.Errorf("failed to create parent directory for %s: %w", destPath, err)
			}

			if err := os.Rename(path, destPath); err != nil {
				return fmt.Errorf("failed to move file %s to %s: %w", path, destPath, err)
			}
			logsDownloadLog.Printf("Moved file: %s → %s", path, destPath)
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatVerboseMessage(fmt.Sprintf("Flattened: %s → %s", relPath, relPath)))
			}
		}

		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to flatten agent_outputs artifact: %w", err)
	}

	// Remove the now-empty agent_outputs directory
	if err := os.RemoveAll(agentOutputsDir); err != nil {
		logsDownloadLog.Printf("Failed to remove agent_outputs directory %s: %v", agentOutputsDir, err)
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to remove agent_outputs directory: %v", err)))
		}
		// Don't fail the entire operation if cleanup fails
	} else {
		logsDownloadLog.Printf("Removed agent_outputs directory: %s", agentOutputsDir)
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatVerboseMessage("Flattened agent_outputs artifact and removed nested structure"))
		}
	}

	return nil
}

// downloadWorkflowRunLogs downloads and unzips workflow run logs using GitHub API
func downloadWorkflowRunLogs(runID int64, outputDir string, verbose bool) error {
	logsDownloadLog.Printf("Downloading workflow run logs: run_id=%d, output_dir=%s", runID, outputDir)

	// Create a temporary file for the zip download
	tmpZip := filepath.Join(os.TempDir(), fmt.Sprintf("workflow-logs-%d.zip", runID))
	defer os.RemoveAll(tmpZip)

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Downloading workflow run logs for run %d...", runID)))
	}

	// Use gh api to download the logs zip file
	// The endpoint returns a 302 redirect to the actual zip file
	output, err := workflow.RunGH("Downloading workflow logs...", "api", "repos/{owner}/{repo}/actions/runs/"+strconv.FormatInt(runID, 10)+"/logs")
	if err != nil {
		// Check for authentication errors
		if strings.Contains(err.Error(), "exit status 4") {
			return fmt.Errorf("GitHub CLI authentication required. Run 'gh auth login' first")
		}
		// If logs are not found or run has no logs, this is not a critical error
		if strings.Contains(string(output), "not found") || strings.Contains(err.Error(), "410") {
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("No logs found for run %d (may be expired or unavailable)", runID)))
			}
			return nil
		}
		return fmt.Errorf("failed to download workflow run logs for run %d: %w", runID, err)
	}

	// Write the downloaded zip content to temporary file
	if err := os.WriteFile(tmpZip, output, 0644); err != nil {
		return fmt.Errorf("failed to write logs zip file: %w", err)
	}

	// Create a subdirectory for workflow logs to keep the run directory organized
	workflowLogsDir := filepath.Join(outputDir, "workflow-logs")
	if err := os.MkdirAll(workflowLogsDir, 0755); err != nil {
		return fmt.Errorf("failed to create workflow-logs directory: %w", err)
	}

	// Unzip the logs into the workflow-logs subdirectory
	if err := unzipFile(tmpZip, workflowLogsDir, verbose); err != nil {
		return fmt.Errorf("failed to unzip workflow logs: %w", err)
	}

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("Downloaded and extracted workflow run logs to %s", workflowLogsDir)))
	}

	return nil
}

// unzipFile extracts a zip file to a destination directory
func unzipFile(zipPath, destDir string, verbose bool) error {
	// Open the zip file
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("failed to open zip file: %w", err)
	}
	defer r.Close()

	// Extract each file in the zip
	for _, f := range r.File {
		if err := extractZipFile(f, destDir, verbose); err != nil {
			return err
		}
	}

	return nil
}

// extractZipFile extracts a single file from a zip archive
func extractZipFile(f *zip.File, destDir string, verbose bool) (extractErr error) {
	// #nosec G305 - Path traversal is prevented by filepath.Clean and prefix check below
	// Validate file name doesn't contain path traversal attempts
	cleanName := filepath.Clean(f.Name)
	if strings.Contains(cleanName, "..") {
		return fmt.Errorf("invalid file path in zip (contains ..): %s", f.Name)
	}

	// Construct the full path for the file
	filePath := filepath.Join(destDir, cleanName)

	// Prevent zip slip vulnerability - ensure extracted path is within destDir
	cleanDest := filepath.Clean(destDir)
	if !strings.HasPrefix(filepath.Clean(filePath), cleanDest+string(os.PathSeparator)) && filepath.Clean(filePath) != cleanDest {
		return fmt.Errorf("invalid file path in zip (outside destination): %s", f.Name)
	}

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatVerboseMessage(fmt.Sprintf("Extracting: %s", cleanName)))
	}

	// Create directory if it's a directory entry
	if f.FileInfo().IsDir() {
		return os.MkdirAll(filePath, os.ModePerm)
	}

	// Decompression bomb protection - limit individual file size to 1GB
	// #nosec G110 - Decompression bomb is mitigated by size check below
	const maxFileSize = 1 * 1024 * 1024 * 1024 // 1GB
	if f.UncompressedSize64 > maxFileSize {
		return fmt.Errorf("file too large in zip (>1GB): %s (%d bytes)", f.Name, f.UncompressedSize64)
	}

	// Create parent directory if needed
	if err := os.MkdirAll(filepath.Dir(filePath), os.ModePerm); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Open the file in the zip
	srcFile, err := f.Open()
	if err != nil {
		return fmt.Errorf("failed to open file in zip: %w", err)
	}
	defer srcFile.Close()

	// Create the destination file
	destFile, err := os.OpenFile(filePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
	if err != nil {
		return fmt.Errorf("failed to create destination file: %w", err)
	}
	defer func() {
		// Handle errors from closing the writable file to prevent data loss
		// Data written to a file may be cached in memory and only flushed when the file is closed.
		// If Close() fails and the error is ignored, data loss can occur silently.
		if err := destFile.Close(); extractErr == nil && err != nil {
			extractErr = fmt.Errorf("failed to close destination file: %w", err)
		}
	}()

	// Copy the content with size limit enforcement
	// Use LimitReader to prevent reading more than declared size
	limitedReader := io.LimitReader(srcFile, int64(maxFileSize))
	written, err := io.Copy(destFile, limitedReader)
	if err != nil {
		extractErr = fmt.Errorf("failed to extract file: %w", err)
		return extractErr
	}

	// Verify we didn't exceed the size limit
	if uint64(written) > maxFileSize {
		extractErr = fmt.Errorf("file extraction exceeded size limit: %s", f.Name)
		return extractErr
	}

	return nil
}

// listArtifacts creates a list of all artifact files in the output directory
func listArtifacts(outputDir string) ([]string, error) {
	var artifacts []string

	err := filepath.Walk(outputDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories and the summary file itself
		if info.IsDir() || filepath.Base(path) == runSummaryFileName {
			return nil
		}

		// Get relative path from outputDir
		relPath, err := filepath.Rel(outputDir, path)
		if err != nil {
			return err
		}

		artifacts = append(artifacts, relPath)
		return nil
	})

	if err != nil {
		return nil, err
	}

	return artifacts, nil
}

// downloadRunArtifacts downloads artifacts for a specific workflow run
func downloadRunArtifacts(runID int64, outputDir string, verbose bool) error {
	logsDownloadLog.Printf("Downloading run artifacts: run_id=%d, output_dir=%s", runID, outputDir)

	// Check if artifacts already exist on disk (since they're immutable)
	if fileutil.DirExists(outputDir) && !fileutil.IsDirEmpty(outputDir) {
		// Try to load cached summary
		if summary, ok := loadRunSummary(outputDir, verbose); ok {
			// Valid cached summary exists, skip download
			logsDownloadLog.Printf("Using cached artifacts for run %d", runID)
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Using cached artifacts for run %d at %s (from %s)", runID, outputDir, summary.ProcessedAt.Format("2006-01-02 15:04:05"))))
			}
			return nil
		}
		// Summary doesn't exist or version mismatch - artifacts exist but need reprocessing
		// Don't re-download, just reprocess what's there
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Run folder exists with artifacts, will reprocess run %d without re-downloading", runID)))
		}
		// Return nil to indicate success - the artifacts are already there
		return nil
	}

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create run output directory: %w", err)
	}
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatVerboseMessage(fmt.Sprintf("Created output directory %s", outputDir)))
	}

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Executing: gh run download %s --dir %s", strconv.FormatInt(runID, 10), outputDir)))
	}

	// Start spinner for network operation
	spinner := console.NewSpinner(fmt.Sprintf("Downloading artifacts for run %d...", runID))
	if !verbose {
		spinner.Start()
	}

	cmd := workflow.ExecGH("run", "download", strconv.FormatInt(runID, 10), "--dir", outputDir)
	output, err := cmd.CombinedOutput()

	if err != nil {
		// Stop spinner on error
		if !verbose {
			spinner.Stop()
		}
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatVerboseMessage(string(output)))
		}

		// Check if it's because there are no artifacts
		if strings.Contains(string(output), "no valid artifacts") || strings.Contains(string(output), "not found") {
			// Clean up empty directory
			if err := os.RemoveAll(outputDir); err != nil && verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to clean up empty directory %s: %v", outputDir, err)))
			}
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("No artifacts found for run %d (gh run download reported none)", runID)))
			}
			return ErrNoArtifacts
		}
		// Check for authentication errors
		if strings.Contains(err.Error(), "exit status 4") {
			return fmt.Errorf("GitHub CLI authentication required. Run 'gh auth login' first")
		}
		return fmt.Errorf("failed to download artifacts for run %d: %w (output: %s)", runID, err, string(output))
	}

	// Stop spinner with success message
	if !verbose {
		spinner.StopWithMessage(fmt.Sprintf("✓ Downloaded artifacts for run %d", runID))
	}

	// Flatten single-file artifacts
	if err := flattenSingleFileArtifacts(outputDir, verbose); err != nil {
		return fmt.Errorf("failed to flatten artifacts: %w", err)
	}

	// Flatten unified agent-artifacts directory structure
	if err := flattenUnifiedArtifact(outputDir, verbose); err != nil {
		return fmt.Errorf("failed to flatten unified artifact: %w", err)
	}

	// Flatten agent_outputs artifact if present
	if err := flattenAgentOutputsArtifact(outputDir, verbose); err != nil {
		return fmt.Errorf("failed to flatten agent_outputs artifact: %w", err)
	}

	// Download and unzip workflow run logs
	if err := downloadWorkflowRunLogs(runID, outputDir, verbose); err != nil {
		// Log the error but don't fail the entire download process
		// Logs may not be available for all runs (e.g., expired or deleted)
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to download workflow run logs: %v", err)))
		}
	}

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("Downloaded artifacts for run %d to %s", runID, outputDir)))
		// Enumerate created files (shallow + summary) for immediate visibility
		var fileCount int
		var firstFiles []string
		_ = filepath.Walk(outputDir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			if info.IsDir() {
				return nil
			}
			fileCount++
			if len(firstFiles) < 12 { // capture a reasonable preview
				rel, relErr := filepath.Rel(outputDir, path)
				if relErr == nil {
					firstFiles = append(firstFiles, rel)
				}
			}
			return nil
		})
		if fileCount == 0 {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Download completed but no artifact files were created (empty run)"))
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

	return nil
}
