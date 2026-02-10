// This file provides helper functions for workflow compilation.
//
// This file contains shared utilities used by the compile command to process workflow
// files, manage compilation statistics, and handle campaign workflows. These helpers
// support both single-file and batch compilation operations.
//
// # Organization Rationale
//
// These helper functions are grouped here because they:
//   - Are used by multiple compile command variants (compile, watch, campaign)
//   - Provide common compilation patterns and error handling
//   - Have a clear domain focus (compilation operations)
//   - Keep the main compile_command.go file focused on CLI interaction
//
// This follows the helper file conventions documented in skills/developer/SKILL.md.
//
// # Key Functions
//
// Single File Compilation:
//   - compileSingleFile() - Compile a single markdown workflow with stats tracking
//
// Batch Compilation:
//   - compileBatchWorkflows() - Compile multiple workflows in parallel
//   - scanAllWorkflows() - Scan directories for workflow files
//
// Campaign Compilation:
//   - compileAllCampaignOrchestrators() - Generate and compile campaign orchestrators
//
// Statistics:
//   - CompilationStats - Track compilation success/failure/skip counts
//
// These functions abstract common compilation patterns, allowing the main compile
// command to focus on CLI interaction while these helpers handle the mechanics.

package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/github/gh-aw/pkg/stringutil"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/workflow"
)

var compileHelpersLog = logger.New("cli:compile_helpers")

// getRepositoryRelativePath converts an absolute file path to a repository-relative path
// This ensures stable workflow identifiers regardless of where the repository is cloned
func getRepositoryRelativePath(absPath string) (string, error) {
	// Get the repository root for the specific file
	repoRoot, err := findGitRootForPath(absPath)
	if err != nil {
		// If we can't get the repo root, just use the basename as fallback
		compileHelpersLog.Printf("Warning: could not get repository root for %s: %v, using basename", absPath, err)
		return filepath.Base(absPath), nil
	}

	// Convert both paths to absolute to ensure they can be compared
	absPath, err = filepath.Abs(absPath)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path: %w", err)
	}

	// Get the relative path from repo root
	relPath, err := filepath.Rel(repoRoot, absPath)
	if err != nil {
		return "", fmt.Errorf("failed to get relative path: %w", err)
	}

	// Normalize path separators to forward slashes for consistency across platforms
	// This ensures the same hash value on Windows, Linux, and macOS
	relPath = filepath.ToSlash(relPath)

	return relPath, nil
}

// compileSingleFile compiles a single markdown workflow file and updates compilation statistics
// If checkExists is true, the function will check if the file exists before compiling
// Returns true if compilation was attempted (file exists or checkExists is false), false otherwise
func compileSingleFile(compiler *workflow.Compiler, file string, stats *CompilationStats, verbose bool, checkExists bool) bool {
	// Check if file exists if requested (for watch mode)
	if checkExists {
		if _, err := os.Stat(file); os.IsNotExist(err) {
			compileHelpersLog.Printf("File %s was deleted, skipping compilation", file)
			return false
		}
	}

	stats.Total++

	// Regular workflow file - compile normally
	compileHelpersLog.Printf("Compiling as regular workflow: %s", file)
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatProgressMessage(fmt.Sprintf("Compiling: %s", file)))
	}

	if err := CompileWorkflowWithValidation(compiler, file, verbose, false, false, false, false, false); err != nil {
		// Always show compilation errors on new line
		// Note: Don't wrap in FormatErrorMessage as the error is already formatted by console.FormatError
		fmt.Fprintln(os.Stderr, err.Error())
		stats.Errors++
		stats.FailedWorkflows = append(stats.FailedWorkflows, filepath.Base(file))
	} else {
		compileHelpersLog.Printf("Successfully compiled: %s", file)
	}

	return true
}

// compileAllWorkflowFiles compiles all markdown files in the workflows directory
func compileAllWorkflowFiles(compiler *workflow.Compiler, workflowsDir string, verbose bool) (*CompilationStats, error) {
	compileHelpersLog.Printf("Compiling all workflow files in directory: %s", workflowsDir)
	// Reset warning count before compilation
	compiler.ResetWarningCount()

	// Track compilation statistics
	stats := &CompilationStats{}

	// Find all markdown files
	mdFiles, err := filepath.Glob(filepath.Join(workflowsDir, "*.md"))
	if err != nil {
		return stats, fmt.Errorf("failed to find markdown files: %w", err)
	}

	// Filter out README.md files
	mdFiles = filterWorkflowFiles(mdFiles)

	if len(mdFiles) == 0 {
		compileHelpersLog.Printf("No markdown files found in %s", workflowsDir)
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("No markdown files found in %s", workflowsDir)))
		}
		return stats, nil
	}

	compileHelpersLog.Printf("Found %d markdown files to compile", len(mdFiles))

	// Compile each file
	for _, file := range mdFiles {
		compileSingleFile(compiler, file, stats, verbose, false)
	}

	// Get warning count from compiler
	stats.Warnings = compiler.GetWarningCount()

	// Save the action cache after all compilations
	actionCache := compiler.GetSharedActionCache()
	hasActionCacheEntries := actionCache != nil && len(actionCache.Entries) > 0
	successCount := stats.Total - stats.Errors

	if actionCache != nil {
		if err := actionCache.Save(); err != nil {
			compileHelpersLog.Printf("Failed to save action cache: %v", err)
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to save action cache: %v", err)))
			}
		} else {
			compileHelpersLog.Print("Action cache saved successfully")
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("Action cache saved to %s", actionCache.GetCachePath())))
			}
		}
	}

	// Ensure .gitattributes marks .lock.yml files as generated
	// Only update if we successfully compiled workflows or have action cache entries
	if successCount > 0 || hasActionCacheEntries {
		if err := ensureGitAttributes(); err != nil {
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to update .gitattributes: %v", err)))
			}
		}
	} else {
		compileHelpersLog.Print("Skipping .gitattributes update (no compiled workflows and no action cache entries)")
	}

	return stats, nil
}

// compileModifiedFiles compiles a list of modified markdown files
func compileModifiedFiles(compiler *workflow.Compiler, files []string, verbose bool) {
	if len(files) == 0 {
		return
	}

	// Clear screen before emitting new output in watch mode
	console.ClearScreen()

	fmt.Fprintln(os.Stderr, "Watching for file changes")
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatProgressMessage(fmt.Sprintf("Compiling %d modified file(s)...", len(files))))
	}

	// Reset warning count before compilation
	compiler.ResetWarningCount()

	// Track compilation statistics
	stats := &CompilationStats{}

	for _, file := range files {
		compileSingleFile(compiler, file, stats, verbose, true)
	}

	// Get warning count from compiler
	stats.Warnings = compiler.GetWarningCount()

	// Save the action cache after compilations
	actionCache := compiler.GetSharedActionCache()
	hasActionCacheEntries := actionCache != nil && len(actionCache.Entries) > 0
	successCount := stats.Total - stats.Errors

	if actionCache != nil {
		if err := actionCache.Save(); err != nil {
			compileHelpersLog.Printf("Failed to save action cache: %v", err)
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to save action cache: %v", err)))
			}
		} else {
			compileHelpersLog.Print("Action cache saved successfully")
		}
	}

	// Ensure .gitattributes marks .lock.yml files as generated
	// Only update if we successfully compiled workflows or have action cache entries
	if successCount > 0 || hasActionCacheEntries {
		if err := ensureGitAttributes(); err != nil {
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to update .gitattributes: %v", err)))
			}
		}
	} else {
		compileHelpersLog.Print("Skipping .gitattributes update (no compiled workflows and no action cache entries)")
	}

	// Print summary instead of just "Recompiled"
	printCompilationSummary(stats)
}

// compileModifiedFilesWithDependencies compiles modified files and their dependencies using the dependency graph
func compileModifiedFilesWithDependencies(compiler *workflow.Compiler, depGraph *DependencyGraph, files []string, verbose bool) {
	if len(files) == 0 {
		return
	}

	// Clear screen before emitting new output in watch mode
	console.ClearScreen()

	// Use dependency graph to determine what needs to be recompiled
	var workflowsToCompile []string
	uniqueWorkflows := make(map[string]bool)

	for _, modifiedFile := range files {
		compileHelpersLog.Printf("Processing modified file: %s", modifiedFile)

		// Update the workflow in the dependency graph
		if err := depGraph.UpdateWorkflow(modifiedFile, compiler); err != nil {
			compileHelpersLog.Printf("Warning: failed to update workflow in dependency graph: %v", err)
		}

		// Get affected workflows from dependency graph
		affected := depGraph.GetAffectedWorkflows(modifiedFile)
		compileHelpersLog.Printf("File %s affects %d workflow(s)", modifiedFile, len(affected))

		// Add to unique set
		for _, workflow := range affected {
			if !uniqueWorkflows[workflow] {
				uniqueWorkflows[workflow] = true
				workflowsToCompile = append(workflowsToCompile, workflow)
			}
		}
	}

	fmt.Fprintln(os.Stderr, "Watching for file changes")
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatProgressMessage(fmt.Sprintf("Recompiling %d workflow(s) affected by %d change(s)...", len(workflowsToCompile), len(files))))
	}

	// Reset warning count before compilation
	compiler.ResetWarningCount()

	// Track compilation statistics
	stats := &CompilationStats{}

	for _, file := range workflowsToCompile {
		compileSingleFile(compiler, file, stats, verbose, true)
	}

	// Get warning count from compiler
	stats.Warnings = compiler.GetWarningCount()

	// Save the action cache after compilations
	actionCache := compiler.GetSharedActionCache()
	hasActionCacheEntries := actionCache != nil && len(actionCache.Entries) > 0
	successCount := stats.Total - stats.Errors

	if actionCache != nil {
		if err := actionCache.Save(); err != nil {
			compileHelpersLog.Printf("Failed to save action cache: %v", err)
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to save action cache: %v", err)))
			}
		} else {
			compileHelpersLog.Print("Action cache saved successfully")
		}
	}

	// Ensure .gitattributes marks .lock.yml files as generated
	// Only update if we successfully compiled workflows or have action cache entries
	if successCount > 0 || hasActionCacheEntries {
		if err := ensureGitAttributes(); err != nil {
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to update .gitattributes: %v", err)))
			}
		}
	} else {
		compileHelpersLog.Print("Skipping .gitattributes update (no compiled workflows and no action cache entries)")
	}

	// Print summary instead of just "Recompiled"
	printCompilationSummary(stats)
}

// handleFileDeleted handles the deletion of a markdown file by removing its corresponding lock file
func handleFileDeleted(mdFile string, verbose bool) {
	// Regular workflow file - generate the corresponding lock file path
	lockFile := stringutil.MarkdownToLockFile(mdFile)

	// Check if the lock file exists and remove it
	if _, err := os.Stat(lockFile); err == nil {
		if err := os.Remove(lockFile); err != nil {
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to remove lock file %s: %v", lockFile, err)))
			}
		} else {
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Removed corresponding lock file: %s", lockFile)))
			}
		}
	}
}

// trackWorkflowFailure adds a workflow failure to the compilation statistics
func trackWorkflowFailure(stats *CompilationStats, workflowPath string, errorCount int) {
	// Add to FailedWorkflows for backward compatibility
	stats.FailedWorkflows = append(stats.FailedWorkflows, filepath.Base(workflowPath))

	// Add detailed failure information
	stats.FailureDetails = append(stats.FailureDetails, WorkflowFailure{
		Path:       workflowPath,
		ErrorCount: errorCount,
	})
}

// printCompilationSummary prints a summary of the compilation results
func printCompilationSummary(stats *CompilationStats) {
	if stats.Total == 0 {
		return
	}

	summary := fmt.Sprintf("Compiled %d workflow(s): %d error(s), %d warning(s)",
		stats.Total, stats.Errors, stats.Warnings)

	// Use different formatting based on whether there were errors
	if stats.Errors > 0 {
		fmt.Fprintln(os.Stderr, console.FormatErrorMessage(summary))
		// List the failed workflows with their error counts
		if len(stats.FailureDetails) > 0 {
			fmt.Fprintln(os.Stderr, console.FormatErrorMessage("Failed workflows:"))
			for _, failure := range stats.FailureDetails {
				errorWord := "error"
				if failure.ErrorCount > 1 {
					errorWord = "errors"
				}
				fmt.Fprintf(os.Stderr, "  - %s (%d %s)\n", failure.Path, failure.ErrorCount, errorWord)
			}
		} else if len(stats.FailedWorkflows) > 0 {
			// Fallback for backward compatibility if FailureDetails is not populated
			fmt.Fprintln(os.Stderr, console.FormatErrorMessage("Failed workflows:"))
			for _, workflow := range stats.FailedWorkflows {
				fmt.Fprintf(os.Stderr, "  - %s\n", workflow)
			}
		}
	} else if stats.Warnings > 0 {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(summary))
	} else {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(summary))
	}
}
