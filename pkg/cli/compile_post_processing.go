// This file provides post-processing operations for workflow compilation.
//
// This file contains functions that perform post-compilation operations such as
// generating Dependabot manifests and maintenance workflows.
//
// # Organization Rationale
//
// These post-processing functions are grouped here because they:
//   - Run after workflow compilation completes
//   - Generate auxiliary files and manifests
//   - Have a clear domain focus (post-compilation processing)
//   - Keep the main orchestrator focused on coordination
//
// # Key Functions
//
// Generation:
//   - generateDependabotManifestsWrapper() - Generate Dependabot manifests
//   - generateMaintenanceWorkflowWrapper() - Generate maintenance workflow
//
// Statistics:
//   - collectWorkflowStatisticsWrapper() - Collect workflow statistics
//
// These functions abstract post-processing operations, allowing the main compile
// orchestrator to focus on coordination while these handle generation and validation.

package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/github/gh-aw/pkg/workflow"
)

var compilePostProcessingLog = logger.New("cli:compile_post_processing")

// generateDependabotManifestsWrapper generates Dependabot manifests for compiled workflows
func generateDependabotManifestsWrapper(
	compiler *workflow.Compiler,
	workflowDataList []*workflow.WorkflowData,
	workflowsDir string,
	forceOverwrite bool,
	strict bool,
) error {
	compilePostProcessingLog.Print("Generating Dependabot manifests for compiled workflows")

	if err := compiler.GenerateDependabotManifests(workflowDataList, workflowsDir, forceOverwrite); err != nil {
		if strict {
			return fmt.Errorf("failed to generate Dependabot manifests: %w", err)
		}
		// Non-strict mode: just report as warning
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to generate Dependabot manifests: %v", err)))
	}

	return nil
}

// generateMaintenanceWorkflowWrapper generates maintenance workflow if any workflow uses expires field
func generateMaintenanceWorkflowWrapper(
	compiler *workflow.Compiler,
	workflowDataList []*workflow.WorkflowData,
	workflowsDir string,
	verbose bool,
	strict bool,
) error {
	compilePostProcessingLog.Print("Generating maintenance workflow")

	if err := workflow.GenerateMaintenanceWorkflow(workflowDataList, workflowsDir, compiler.GetVersion(), compiler.GetActionMode(), verbose); err != nil {
		if strict {
			return fmt.Errorf("failed to generate maintenance workflow: %w", err)
		}
		// Non-strict mode: just report as warning
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to generate maintenance workflow: %v", err)))
	}

	return nil
}

// collectWorkflowStatisticsWrapper collects and returns workflow statistics
func collectWorkflowStatisticsWrapper(markdownFiles []string) []*WorkflowStats {
	compilePostProcessingLog.Printf("Collecting workflow statistics for %d files", len(markdownFiles))

	var statsList []*WorkflowStats
	for _, file := range markdownFiles {
		resolvedFile, err := resolveWorkflowFile(file, false)
		if err != nil {
			continue // Skip files that couldn't be resolved
		}
		lockFile := stringutil.MarkdownToLockFile(resolvedFile)
		if workflowStats, err := collectWorkflowStats(lockFile); err == nil {
			statsList = append(statsList, workflowStats)
		}
	}

	compilePostProcessingLog.Printf("Collected statistics for %d workflows", len(statsList))
	return statsList
}

// updateGitAttributes ensures .gitattributes marks .lock.yml files as generated
func updateGitAttributes(successCount int, actionCache *workflow.ActionCache, verbose bool) error {
	compilePostProcessingLog.Printf("Updating .gitattributes (compiled=%d, actionCache=%v)", successCount, actionCache != nil)

	hasActionCacheEntries := actionCache != nil && len(actionCache.Entries) > 0

	// Only update if we successfully compiled workflows or have action cache entries
	if successCount > 0 || hasActionCacheEntries {
		compilePostProcessingLog.Printf("Updating .gitattributes (compiled=%d, actionCache=%v)", successCount, hasActionCacheEntries)
		if err := ensureGitAttributes(); err != nil {
			compilePostProcessingLog.Printf("Failed to update .gitattributes: %v", err)
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to update .gitattributes: %v", err)))
			}
			return err
		}
		compilePostProcessingLog.Printf("Successfully updated .gitattributes")
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Updated .gitattributes to mark .lock.yml files as generated"))
		}
	} else {
		compilePostProcessingLog.Print("Skipping .gitattributes update (no compiled workflows and no action cache entries)")
	}

	return nil
}

// saveActionCache saves the action cache after all compilations
func saveActionCache(actionCache *workflow.ActionCache, verbose bool) error {
	if actionCache == nil {
		return nil
	}

	compilePostProcessingLog.Print("Saving action cache")

	if err := actionCache.Save(); err != nil {
		compilePostProcessingLog.Printf("Failed to save action cache: %v", err)
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to save action cache: %v", err)))
		}
		return err
	}

	compilePostProcessingLog.Print("Action cache saved successfully")
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("Action cache saved to %s", actionCache.GetCachePath())))
	}

	return nil
}

// getAbsoluteWorkflowDir converts a relative workflow dir to absolute path
func getAbsoluteWorkflowDir(workflowDir string, gitRoot string) string {
	absWorkflowDir := workflowDir
	if !filepath.IsAbs(absWorkflowDir) {
		absWorkflowDir = filepath.Join(gitRoot, workflowDir)
	}
	return absWorkflowDir
}
