package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/stringutil"
)

// purgeTrackingData holds data needed for purge operations
type purgeTrackingData struct {
	existingLockFiles    []string
	existingInvalidFiles []string
	expectedLockFiles    []string
}

// collectPurgeData collects existing files for purge operations
func collectPurgeData(workflowsDir string, mdFiles []string, verbose bool) (*purgeTrackingData, error) {
	return collectPurgeDataWithPatterns(workflowsDir, mdFiles, verbose, "*.lock.yml", "*.invalid.yml")
}

// collectPurgeDataWithPatterns is the testable implementation of collectPurgeData.
// lockPattern and invalidPattern are appended to workflowsDir for the glob calls.
func collectPurgeDataWithPatterns(workflowsDir string, mdFiles []string, verbose bool, lockPattern, invalidPattern string) (*purgeTrackingData, error) {
	data := &purgeTrackingData{}

	// Find all existing files
	var err error
	data.existingLockFiles, err = filepath.Glob(filepath.Join(workflowsDir, lockPattern))
	if err != nil {
		return nil, fmt.Errorf("failed to glob existing .lock.yml files in %s: %w", workflowsDir, err)
	}
	data.existingInvalidFiles, err = filepath.Glob(filepath.Join(workflowsDir, invalidPattern))
	if err != nil {
		return nil, fmt.Errorf("failed to glob existing .invalid.yml files in %s: %w", workflowsDir, err)
	}

	// Create expected files list
	for _, mdFile := range mdFiles {
		lockFile := stringutil.MarkdownToLockFile(mdFile)
		data.expectedLockFiles = append(data.expectedLockFiles, lockFile)
	}

	if verbose {
		if len(data.existingLockFiles) > 0 {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessageStderr(fmt.Sprintf("Found %d existing .lock.yml files", len(data.existingLockFiles))))
		}
		if len(data.existingInvalidFiles) > 0 {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessageStderr(fmt.Sprintf("Found %d existing .invalid.yml files", len(data.existingInvalidFiles))))
		}
	}

	return data, nil
}

// runPurgeOperations runs all purge operations
func runPurgeOperations(workflowsDir string, data *purgeTrackingData, verbose bool) {
	// Errors from purge operations are logged but don't stop compilation
	logNonFatalCompileError("Orphaned lock file purge", purgeOrphanedLockFiles(workflowsDir, data.expectedLockFiles, verbose))
	logNonFatalCompileError("Invalid workflow file purge", purgeInvalidFiles(workflowsDir, verbose))
}
