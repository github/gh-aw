package cli

import (
	"context"
	"fmt"
	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/gitutil"
	"github.com/github/gh-aw/pkg/workflow"
	"os"
	"path/filepath"
)

// runPostProcessing runs post-processing for specific files compilation
func runPostProcessing(
	ctx context.Context,
	compiler *workflow.Compiler,
	workflowDataList []*workflow.WorkflowData,
	compiledLockFiles []string,
	config CompileConfig,
	successCount int,
) error {
	// Get action cache
	actionCache := compiler.GetSharedActionCache()

	// Update .gitattributes (errors are non-fatal)
	_ = updateGitAttributes(successCount, actionCache, config.Verbose)

	// Generate Dependabot manifests and reconcile compiler-managed ignore entries if requested.
	if config.Dependabot && !config.NoEmit {
		if gitRoot, err := gitutil.FindGitRoot(); err == nil {
			absWorkflowDir := filepath.Join(gitRoot, config.WorkflowDir)
			if err := generateDependabotManifestsWrapper(ctx, compiler, workflowDataList, absWorkflowDir, config.ForceOverwrite, config.Strict); err != nil {
				if config.Strict {
					return err
				}
			}
			if err := compiler.ReconcileManagedDependabotIgnoresInRepo(gitRoot); err != nil {
				if config.Strict {
					return err
				}
				fmt.Fprintln(os.Stderr, console.FormatWarningMessageStderr(fmt.Sprintf("Failed to reconcile compiler-managed Dependabot ignore entries: %v", err)))
			}
		}
	}

	// Reconcile the implicit action-failure expiry marker so specific-file
	// compiles agree with what a full directory compile would produce.
	// Maintenance workflow generation itself is skipped for specific-file
	// compiles because it requires parsing every workflow in the directory to
	// check for expires fields; only reconcile when using the default
	// workflow directory (custom --dir compiles and --no-emit compiles are
	// left untouched).
	if !config.NoEmit && config.usesDefaultWorkflowDir() && len(compiledLockFiles) > 0 {
		if gitRoot, err := gitutil.FindGitRoot(); err == nil {
			absWorkflowDir := getAbsoluteWorkflowDir(getWorkflowsDir(), gitRoot)
			repoConfig, err := workflow.LoadRepoConfig(gitRoot)
			if err != nil {
				repoConfig = nil
			}
			workflow.DisableDefaultActionFailureExpiryMarkersIfUnenforced(compiledLockFiles, absWorkflowDir, repoConfig)
		}
	}

	// Prune stale gh-aw-actions entries before saving
	pruneStaleActionCacheEntries(compiler, actionCache)

	// Save action cache (errors are logged but non-fatal)
	_ = saveActionCache(actionCache, config.Verbose)

	return nil
}

// runPostProcessingForDirectory runs post-processing for directory compilation
func runPostProcessingForDirectory(
	ctx context.Context,
	compiler *workflow.Compiler,
	workflowDataList []*workflow.WorkflowData,
	config CompileConfig,
	workflowsDir string,
	gitRoot string,
	successCount int,
	errorCount int,
) error {
	// Get action cache
	actionCache := compiler.GetSharedActionCache()

	// Update .gitattributes (errors are non-fatal)
	_ = updateGitAttributes(successCount, actionCache, config.Verbose)

	// Generate Dependabot manifests if requested
	if config.Dependabot && !config.NoEmit {
		absWorkflowDir := getAbsoluteWorkflowDir(workflowsDir, gitRoot)
		if err := generateDependabotManifestsWrapper(ctx, compiler, workflowDataList, absWorkflowDir, config.ForceOverwrite, config.Strict); err != nil {
			if config.Strict {
				return err
			}
		}
	}

	// Reconcile compiler-managed Dependabot ignore entries for compiler-emitted action refs.
	if config.Dependabot && !config.NoEmit {
		if err := compiler.ReconcileManagedDependabotIgnoresInRepo(gitRoot); err != nil {
			if config.Strict {
				return err
			}
			fmt.Fprintln(os.Stderr, console.FormatWarningMessageStderr(fmt.Sprintf("Failed to reconcile compiler-managed Dependabot ignore entries: %v", err)))
		}
	}

	// Generate maintenance workflow if needed.
	// Skip maintenance workflow generation when using custom --dir option.
	// Keep invoking generators for empty workflowDataList so stale generated files are cleaned up.
	if !config.NoEmit && config.usesDefaultWorkflowDir() {
		absWorkflowDir := getAbsoluteWorkflowDir(workflowsDir, gitRoot)
		if err := generateMaintenanceWorkflowWrapper(ctx, compiler, workflowDataList, absWorkflowDir, gitRoot, config.Verbose, config.Strict, config.NoLogo); err != nil {
			if config.Strict {
				return err
			}
		}
		if err := generateWorkQueueEventWorkflowWrapper(ctx, workflowDataList, absWorkflowDir, gitRoot, config.Strict, config.NoLogo); err != nil {
			if config.Strict {
				return err
			}
		}
		if err := generateCentralSlashCommandWorkflowWrapper(ctx, workflowDataList, absWorkflowDir, gitRoot, config.Strict, config.NoLogo); err != nil {
			if config.Strict {
				return err
			}
		}
	}

	// Prune stale gh-aw-actions entries before saving
	pruneStaleActionCacheEntries(compiler, actionCache)

	// Prune orphaned entries — entries for action versions no longer referenced
	// by any workflow in the directory (e.g. old pins left after a version bump).
	// Safe to call only after a full-directory compilation with zero compile errors.
	pruneOrphanedActionCacheEntries(compiler, actionCache, errorCount)

	// Save action cache (errors are logged but non-fatal)
	_ = saveActionCache(actionCache, config.Verbose)

	return nil
}

// outputResults outputs compilation results in the requested format
func outputResults(
	stats *CompilationStats,
	validationResults *[]ValidationResult,
	config CompileConfig,
) error {
	if !config.DryRun && len(config.modelValidationWarnings) > 0 {
		issues := modelInventoryWarnings(config)
		*validationResults = append(*validationResults, ValidationResult{
			Scope: "batch", Workflow: "models", Valid: true, Errors: []ValidationIssue{}, Warnings: issues,
		})
		stats.Warnings += len(issues)
		if !config.JSONOutput {
			for _, issue := range issues {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessageStderr(issue.Message))
			}
		}
	}
	appendDryRunCompileReport(config, stats, validationResults)

	// Collect and display stats if requested
	if config.Stats && !config.NoEmit && !config.JSONOutput {
		var statsList []*WorkflowStats
		if len(config.MarkdownFiles) > 0 {
			statsList = collectWorkflowStatisticsWrapper(config.MarkdownFiles)
		}
		displayStatsTable(statsList)
		displayScheduleCalendar(statsList)
	}

	// Output JSON if requested
	if config.JSONOutput {
		jsonStr, err := formatValidationOutput(*validationResults)
		if err != nil {
			return err
		}
		fmt.Fprintln(os.Stdout, jsonStr)
	} else if !config.Stats {
		// Print summary for text output (skip if stats mode)
		printCompilationSummary(stats, config.ShowAllErrors)
	}

	// Display actionlint summary if enabled
	if config.Actionlint && !config.NoEmit && !config.JSONOutput {
		displayActionlintSummary()
	}

	return nil
}
