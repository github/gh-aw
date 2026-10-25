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
//   - Keep the compile orchestrator focused on CLI interaction
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
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/github/gh-aw/pkg/setutil"
	"github.com/github/gh-aw/pkg/stringutil"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/workflow"
)

var compileHelpersLog = logger.New("cli:compile_file_operations")

// compileSingleFile compiles a single markdown workflow file and updates compilation statistics
// If checkExists is true, the function will check if the file exists before compiling
// Returns true if compilation was attempted (file exists or checkExists is false), false otherwise
func compileSingleFile(ctx context.Context, compiler *workflow.Compiler, file string, stats *CompilationStats, verbose bool, checkExists bool) bool {
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
		fmt.Fprintln(os.Stderr, console.FormatProgressMessageStderr("Compiling: "+file))
	}

	if err := CompileWorkflowWithValidation(ctx, compiler, file, CompileValidationOptions{Verbose: verbose, NoEmit: compiler.IsNoEmit()}); err != nil {
		// Always show compilation errors on a new line using standard CLI error styling.
		fmt.Fprintln(os.Stderr, console.FormatErrorMessage(err.Error()))
		stats.Errors++
		stats.FailedWorkflows = append(stats.FailedWorkflows, filepath.Base(file))
	} else {
		compileHelpersLog.Printf("Successfully compiled: %s", file)
	}

	return true
}

// compileAllWorkflowFiles compiles all markdown files in the workflows directory
func compileAllWorkflowFiles(ctx context.Context, compiler *workflow.Compiler, workflowsDir string, verbose bool) (*CompilationStats, error) {
	compileHelpersLog.Printf("Compiling all workflow files in directory: %s", workflowsDir)
	// Reset warning count before compilation
	compiler.ResetWarningCount()

	// Track compilation statistics
	stats := &CompilationStats{}

	// Find and filter markdown files (shared helper keeps logic in one place)
	mdFiles, err := getMarkdownWorkflowFiles(workflowsDir)
	if err != nil {
		return stats, fmt.Errorf("failed to find markdown files: %w", err)
	}
	mdFiles, err = filterMarkdownFilesWithFrontmatter(mdFiles)
	if err != nil {
		return stats, fmt.Errorf("failed to filter markdown files: %w", err)
	}
	if len(mdFiles) == 0 {
		compileHelpersLog.Printf("No workflow markdown files found in %s after frontmatter filtering", workflowsDir)
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessageStderr("No workflow markdown files found in "+workflowsDir+" (workflow files must start with a frontmatter opener on the first line)"))
		}
		return stats, nil
	}

	compileHelpersLog.Printf("Found %d markdown files to compile", len(mdFiles))

	// Compile each file
	for _, file := range mdFiles {
		// Resolve to absolute path so that runtime-import macros and dispatch-workflow
		// input extraction work correctly regardless of the caller's working directory.
		absFile, err := filepath.Abs(file)
		if err != nil {
			compileHelpersLog.Printf("Failed to resolve absolute path for %s: %v", file, err)
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessageStderr(fmt.Sprintf("Failed to resolve absolute path for %s: %v", file, err)))
			}
		} else {
			file = absFile
		}
		compileSingleFile(ctx, compiler, file, stats, verbose, false)
	}

	// Get warning count from compiler
	stats.Warnings = compiler.GetWarningCount()

	// Save action cache and update .gitattributes (shared post-compile helpers)
	actionCache := compiler.GetSharedActionCache()
	successCount := stats.Total - stats.Errors
	_ = saveActionCache(actionCache, verbose)
	_ = updateGitAttributes(successCount, actionCache, verbose)

	return stats, nil
}

// compileModifiedFilesWithDependencies compiles modified files and their dependencies using the dependency graph
func compileModifiedFilesWithDependencies(ctx context.Context, compiler *workflow.Compiler, depGraph *DependencyGraph, files []string, verbose bool) {
	if len(files) == 0 {
		return
	}

	// Clear screen before emitting new output in watch mode
	console.ClearScreen()

	// Use dependency graph to determine what needs to be recompiled
	workflowsToCompile := collectModifiedWorkflowDependencies(compiler, depGraph, files)

	fmt.Fprintln(os.Stderr, console.FormatProgressMessageStderr("Watching for file changes"))
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatProgressMessageStderr(fmt.Sprintf("Recompiling %d workflow(s) affected by %d change(s)...", len(workflowsToCompile), len(files))))
	}

	compiler.ResetWarningCount()
	stats := &CompilationStats{}
	for _, file := range workflowsToCompile {
		compileSingleFile(ctx, compiler, file, stats, verbose, true)
	}
	stats.Warnings = compiler.GetWarningCount()
	saveModifiedWorkflowCompilationCache(compiler, stats, verbose)
	printCompilationSummary(stats, false)
}

func collectModifiedWorkflowDependencies(compiler *workflow.Compiler, depGraph *DependencyGraph, files []string) []string {
	var workflowsToCompile []string
	uniqueWorkflows := make(map[string]struct {
	})

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
			if !setutil.Contains(uniqueWorkflows, workflow) {
				uniqueWorkflows[workflow] = struct {
				}{}
				workflowsToCompile = append(workflowsToCompile, workflow)
			}
		}
	}

	return workflowsToCompile
}

func saveModifiedWorkflowCompilationCache(compiler *workflow.Compiler, stats *CompilationStats, verbose bool) {
	// Save the action cache after compilations
	actionCache := compiler.GetSharedActionCache()
	hasActionCacheEntries := actionCache != nil && len(actionCache.Entries) > 0
	successCount := stats.Total - stats.Errors

	if actionCache != nil {
		pruneStaleActionCacheEntries(compiler, actionCache)
		if err := actionCache.Save(); err != nil {
			compileHelpersLog.Printf("Failed to save action cache: %v", err)
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessageStderr(fmt.Sprintf("Failed to save action cache: %v", err)))
			}
		} else {
			compileHelpersLog.Print("Action cache saved successfully")
		}
	}

	// Ensure .gitattributes marks .lock.yml files as generated
	// Only update if we successfully compiled workflows or have action cache entries
	if successCount > 0 || hasActionCacheEntries {
		if _, err := ensureGitAttributes(); err != nil {
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessageStderr(fmt.Sprintf("Failed to update .gitattributes: %v", err)))
			}
		}
	} else {
		compileHelpersLog.Print("Skipping .gitattributes update (no compiled workflows and no action cache entries)")
	}

}

// handleFileDeleted handles the deletion of a markdown file by removing its corresponding lock file
func handleFileDeleted(mdFile string, verbose bool) {
	// Regular workflow file - generate the corresponding lock file path
	lockFile := stringutil.MarkdownToLockFile(mdFile)

	// Check if the lock file exists and remove it
	if _, err := os.Stat(lockFile); err == nil {
		if err := os.Remove(lockFile); err != nil {
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessageStderr(fmt.Sprintf("Failed to remove lock file %s: %v", lockFile, err)))
			}
		} else {
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatInfoMessageStderr("Removed corresponding lock file: "+lockFile))
			}
		}
	}
}
