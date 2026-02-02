// This file provides batch operations for workflow compilation.
//
// This file contains functions that perform batch operations on compiled workflows,
// such as running linters, security scanners, and cleaning up orphaned files.
//
// # Organization Rationale
//
// These batch operation functions are grouped here because they:
//   - Operate on multiple files at once
//   - Run external tools (actionlint, zizmor, poutine)
//   - Have a clear domain focus (batch operations)
//   - Keep the main orchestrator focused on coordination
//
// # Key Functions
//
// Batch Linting:
//   - runBatchActionlint() - Run actionlint on multiple lock files
//
// File Cleanup:
//   - purgeOrphanedLockFiles() - Remove orphaned .lock.yml files
//   - purgeInvalidFiles() - Remove .invalid.yml files
//
// These functions abstract batch operations, allowing the main compile
// orchestrator to focus on coordination while these handle batch processing.

package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
)

var compileBatchOperationsLog = logger.New("cli:compile_batch_operations")

// runBatchActionlint runs actionlint on all lock files in batch
func runBatchActionlint(lockFiles []string, verbose bool, strict bool) error {
	if len(lockFiles) == 0 {
		compileBatchOperationsLog.Print("No lock files to lint with actionlint")
		return nil
	}

	compileBatchOperationsLog.Printf("Running batch actionlint on %d lock files", len(lockFiles))

	if err := RunActionlintOnFiles(lockFiles, verbose, strict); err != nil {
		if strict {
			return fmt.Errorf("actionlint linter failed: %w", err)
		}
		// In non-strict mode, actionlint errors are warnings
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("actionlint warnings: %v", err)))
		}
	}

	return nil
}

// runBatchZizmor runs zizmor security scanner on all lock files in batch
func runBatchZizmor(lockFiles []string, verbose bool, strict bool) error {
	if len(lockFiles) == 0 {
		compileBatchOperationsLog.Print("No lock files to scan with zizmor")
		return nil
	}

	compileBatchOperationsLog.Printf("Running batch zizmor on %d lock files", len(lockFiles))

	if err := RunZizmorOnFiles(lockFiles, verbose, strict); err != nil {
		if strict {
			return fmt.Errorf("zizmor security scan failed: %w", err)
		}
		// In non-strict mode, zizmor errors are warnings
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("zizmor warnings: %v", err)))
		}
	}

	return nil
}

// runBatchPoutine runs poutine security scanner once for the entire directory
func runBatchPoutine(workflowDir string, verbose bool, strict bool) error {
	compileBatchOperationsLog.Printf("Running batch poutine on directory: %s", workflowDir)

	if err := RunPoutineOnDirectory(workflowDir, verbose, strict); err != nil {
		if strict {
			return fmt.Errorf("poutine security scan failed: %w", err)
		}
		// In non-strict mode, poutine errors are warnings
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("poutine warnings: %v", err)))
		}
	}

	return nil
}

// purgeOrphanedLockFiles removes orphaned .lock.yml files
// These are lock files that exist but don't have a corresponding .md file
func purgeOrphanedLockFiles(workflowsDir string, expectedLockFiles []string, verbose bool) error {
	compileBatchOperationsLog.Printf("Purging orphaned lock files in %s", workflowsDir)

	// Find all existing .lock.yml files
	existingLockFiles, err := filepath.Glob(filepath.Join(workflowsDir, "*.lock.yml"))
	if err != nil {
		return fmt.Errorf("failed to find existing lock files: %w", err)
	}

	if len(existingLockFiles) == 0 {
		compileBatchOperationsLog.Print("No lock files found")
		return nil
	}

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Found %d existing .lock.yml files", len(existingLockFiles))))
	}

	// Build a set of expected lock files
	expectedLockFileSet := make(map[string]bool)
	for _, expected := range expectedLockFiles {
		expectedLockFileSet[expected] = true
	}

	// Find lock files that should be deleted (exist but aren't expected)
	var orphanedFiles []string
	for _, existing := range existingLockFiles {
		// Skip .campaign.lock.yml files - they're handled by purgeOrphanedCampaignOrchestratorLockFiles
		if strings.HasSuffix(existing, ".campaign.lock.yml") {
			continue
		}
		if !expectedLockFileSet[existing] {
			orphanedFiles = append(orphanedFiles, existing)
		}
	}

	// Delete orphaned lock files
	if len(orphanedFiles) > 0 {
		for _, orphanedFile := range orphanedFiles {
			if err := os.Remove(orphanedFile); err != nil {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to remove orphaned lock file %s: %v", filepath.Base(orphanedFile), err)))
			} else {
				fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("Removed orphaned lock file: %s", filepath.Base(orphanedFile))))
			}
		}
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("Purged %d orphaned .lock.yml files", len(orphanedFiles))))
		}
	} else if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("No orphaned .lock.yml files found to purge"))
	}

	compileBatchOperationsLog.Printf("Purged %d orphaned lock files", len(orphanedFiles))
	return nil
}

// purgeInvalidFiles removes all .invalid.yml files
// These are temporary debugging artifacts that should not persist
func purgeInvalidFiles(workflowsDir string, verbose bool) error {
	compileBatchOperationsLog.Printf("Purging invalid files in %s", workflowsDir)

	// Find all existing .invalid.yml files
	existingInvalidFiles, err := filepath.Glob(filepath.Join(workflowsDir, "*.invalid.yml"))
	if err != nil {
		return fmt.Errorf("failed to find existing invalid files: %w", err)
	}

	if len(existingInvalidFiles) == 0 {
		compileBatchOperationsLog.Print("No invalid files found")
		return nil
	}

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Found %d existing .invalid.yml files", len(existingInvalidFiles))))
	}

	// Delete all .invalid.yml files
	for _, invalidFile := range existingInvalidFiles {
		if err := os.Remove(invalidFile); err != nil {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to remove invalid file %s: %v", filepath.Base(invalidFile), err)))
		} else {
			fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("Removed invalid file: %s", filepath.Base(invalidFile))))
		}
	}

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("Purged %d .invalid.yml files", len(existingInvalidFiles))))
	}

	compileBatchOperationsLog.Printf("Purged %d invalid files", len(existingInvalidFiles))
	return nil
}
