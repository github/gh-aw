// This file provides post-processing operations for workflow compilation.
//
// This file contains functions that perform post-compilation operations such as
// generating Dependabot manifests, maintenance workflows, file cleanup, and
// displaying compiler warnings.
//
// # Organization Rationale
//
// These post-processing functions are grouped here because they:
//   - Run after workflow compilation completes
//   - Generate auxiliary files and manifests
//   - Clean up orphaned or invalid workflow files
//   - Display accumulated warnings from the compiler
//   - Have a clear domain focus (post-compilation processing)
//   - Keep the main orchestrator focused on coordination
//
// # Key Functions
//
// Generation:
//   - generateDependabotManifestsWrapper() - Generate Dependabot manifests
//   - generateMaintenanceWorkflowWrapper() - Generate maintenance workflow
//
// Cleanup:
//   - purgeOrphanedLockFiles() - Remove orphaned .lock.yml files
//   - purgeInvalidFiles() - Remove .invalid.yml files
//
// Warnings and Cache:
//   - displayScheduleWarnings() - Display schedule warnings from the compiler
//   - displaySafeUpdateWarnings() - Display safe update warning prompts
//   - pruneStaleActionCacheEntries() - Remove stale gh-aw-actions cache entries
//
// These functions abstract post-processing operations, allowing the main compile
// orchestrator to focus on coordination while these handle generation and validation.

package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/setutil"
	"github.com/github/gh-aw/pkg/workflow"
)

var compilePostProcessingLog = logger.New("cli:compile_post_processing")

// generateDependabotManifestsWrapper generates Dependabot manifests for compiled workflows
func generateDependabotManifestsWrapper(
	ctx context.Context,
	compiler *workflow.Compiler,
	workflowDataList []*workflow.WorkflowData,
	workflowsDir string,
	forceOverwrite bool,
	strict bool,
) error {
	compilePostProcessingLog.Print("Generating Dependabot manifests for compiled workflows")

	if err := compiler.GenerateDependabotManifests(ctx, workflowDataList, workflowsDir, forceOverwrite); err != nil {
		if strict {
			return fmt.Errorf("failed to generate Dependabot manifests: %w", err)
		}
		// Non-strict mode: just report as warning
		fmt.Fprintln(os.Stderr, console.FormatWarningMessageStderr(fmt.Sprintf("Failed to generate Dependabot manifests: %v", err)))
	}

	return nil
}

// generateMaintenanceWorkflowWrapper generates maintenance workflow if any workflow uses expires field
func generateMaintenanceWorkflowWrapper(
	ctx context.Context,
	compiler *workflow.Compiler,
	workflowDataList []*workflow.WorkflowData,
	workflowsDir string,
	gitRoot string,
	verbose bool,
	strict bool,
) error {
	compilePostProcessingLog.Print("Generating maintenance workflow")

	// Load repo-level configuration (optional file).
	repoConfig, err := workflow.LoadRepoConfig(gitRoot)
	if err != nil {
		if strict {
			return fmt.Errorf("failed to load repo config: %w", err)
		}
		fmt.Fprintln(os.Stderr, console.FormatWarningMessageStderr(fmt.Sprintf("Failed to load repo config: %v", err)))
		repoConfig = nil
	}

	if err := workflow.GenerateMaintenanceWorkflow(ctx, workflow.GenerateMaintenanceWorkflowOptions{
		WorkflowDataList: workflowDataList,
		WorkflowDir:      workflowsDir,
		Version:          compiler.GetVersion(),
		ActionMode:       compiler.GetActionMode(),
		ActionTag:        compiler.GetActionTag(),
		RepoConfig:       repoConfig,
		RepoSlug:         compiler.GetRepositorySlug(),
	}); err != nil {
		if strict {
			return fmt.Errorf("failed to generate maintenance workflow: %w", err)
		}
		// Non-strict mode: just report as warning
		fmt.Fprintln(os.Stderr, console.FormatWarningMessageStderr(fmt.Sprintf("Failed to generate maintenance workflow: %v", err)))
	}

	return nil
}

// generateCentralSlashCommandWorkflowWrapper generates a single centralized
// slash-command trigger workflow for all participating workflows.
func generateCentralSlashCommandWorkflowWrapper(
	ctx context.Context,
	workflowDataList []*workflow.WorkflowData,
	workflowsDir string,
	gitRoot string,
	strict bool,
) error {
	compilePostProcessingLog.Print("Generating centralized slash-command workflow")

	repoConfig, err := workflow.LoadRepoConfig(gitRoot)
	if err != nil {
		if strict {
			return fmt.Errorf("failed to load repo config: %w", err)
		}
		fmt.Fprintln(os.Stderr, console.FormatWarningMessageStderr(fmt.Sprintf(
			"Failed to load repo config; repo-config flags (e.g. help_command) will use defaults: %v", err)))
		repoConfig = nil
	}

	if err := workflow.GenerateCentralSlashCommandWorkflow(ctx, workflowDataList, workflowsDir, repoConfig); err != nil {
		if strict {
			return fmt.Errorf("failed to generate centralized slash-command workflow: %w", err)
		}
		fmt.Fprintln(os.Stderr, console.FormatWarningMessageStderr(fmt.Sprintf("Failed to generate centralized slash-command workflow: %v", err)))
	}

	return nil
}

// purgeOrphanedLockFiles removes orphaned .lock.yml files
// These are lock files that exist but don't have a corresponding .md file
func purgeOrphanedLockFiles(workflowsDir string, expectedLockFiles []string, verbose bool) error {
	compilePostProcessingLog.Printf("Purging orphaned lock files in %s", workflowsDir)

	// Find all existing .lock.yml files
	existingLockFiles, err := filepath.Glob(filepath.Join(workflowsDir, "*.lock.yml"))
	if err != nil {
		return fmt.Errorf("failed to find existing lock files: %w", err)
	}

	if len(existingLockFiles) == 0 {
		compilePostProcessingLog.Print("No lock files found")
		return nil
	}

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessageStderr(fmt.Sprintf("Found %d existing .lock.yml files", len(existingLockFiles))))
	}

	// Build a set of expected lock files
	expectedLockFileSet := make(map[string]struct {
	})
	for _, expected := range expectedLockFiles {
		expectedLockFileSet[expected] = struct {
		}{}
	}

	// Find lock files that should be deleted (exist but aren't expected)
	var orphanedFiles []string
	for _, existing := range existingLockFiles {
		// Skip .campaign.lock.yml files - they're handled by purgeOrphanedCampaignOrchestratorLockFiles
		if strings.HasSuffix(existing, ".campaign.lock.yml") {
			continue
		}
		if !setutil.Contains(expectedLockFileSet, existing) {
			orphanedFiles = append(orphanedFiles, existing)
		}
	}

	// Delete orphaned lock files
	if len(orphanedFiles) > 0 {
		for _, orphanedFile := range orphanedFiles {
			if err := os.Remove(orphanedFile); err != nil {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessageStderr(fmt.Sprintf("Failed to remove orphaned lock file %s: %v", filepath.Base(orphanedFile), err)))
			} else {
				fmt.Fprintln(os.Stderr, console.FormatSuccessMessageStderr("Removed orphaned lock file: "+filepath.Base(orphanedFile)))
			}
		}
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatSuccessMessageStderr(fmt.Sprintf("Purged %d orphaned .lock.yml files", len(orphanedFiles))))
		}
	}

	compilePostProcessingLog.Printf("Purged %d orphaned lock files", len(orphanedFiles))
	return nil
}

// purgeInvalidFiles removes all .invalid.yml files
// These are temporary debugging artifacts that should not persist
func purgeInvalidFiles(workflowsDir string, verbose bool) error {
	compilePostProcessingLog.Printf("Purging invalid files in %s", workflowsDir)

	// Find all existing .invalid.yml files
	existingInvalidFiles, err := filepath.Glob(filepath.Join(workflowsDir, "*.invalid.yml"))
	if err != nil {
		return fmt.Errorf("failed to find existing invalid files: %w", err)
	}

	if len(existingInvalidFiles) == 0 {
		compilePostProcessingLog.Print("No invalid files found")
		return nil
	}

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessageStderr(fmt.Sprintf("Found %d existing .invalid.yml files", len(existingInvalidFiles))))
	}

	// Delete all .invalid.yml files
	for _, invalidFile := range existingInvalidFiles {
		if err := os.Remove(invalidFile); err != nil {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessageStderr(fmt.Sprintf("Failed to remove invalid file %s: %v", filepath.Base(invalidFile), err)))
		} else {
			fmt.Fprintln(os.Stderr, console.FormatSuccessMessageStderr("Removed invalid file: "+filepath.Base(invalidFile)))
		}
	}

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessageStderr(fmt.Sprintf("Purged %d .invalid.yml files", len(existingInvalidFiles))))
	}

	compilePostProcessingLog.Printf("Purged %d invalid files", len(existingInvalidFiles))
	return nil
}

// displayScheduleWarnings displays any schedule warnings from the compiler
func displayScheduleWarnings(compiler *workflow.Compiler, jsonOutput bool) {
	scheduleWarnings := compiler.GetScheduleWarnings()
	if len(scheduleWarnings) > 0 && !jsonOutput {
		for _, warning := range scheduleWarnings {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessageStderr(warning))
		}
	}
}

// displaySafeUpdateWarnings displays any safe update warning prompts accumulated by the
// compiler.  Each entry is a structured message that instructs the calling agent to:
//   - Review new secrets/actions for malicious use
//   - Add a security review note to the pull request description
func displaySafeUpdateWarnings(compiler *workflow.Compiler, jsonOutput bool) {
	warnings := compiler.GetSafeUpdateWarnings()
	if len(warnings) == 0 || jsonOutput {
		return
	}
	for _, w := range warnings {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessageStderr(w))
	}
}

// displayCentralizedSlashCommandRecommendation warns when a repository has many
// slash commands still using non-centralized strategy.
func displayCentralizedSlashCommandRecommendation(compiler *workflow.Compiler, workflowDataList []*workflow.WorkflowData, jsonOutput bool) {
	if jsonOutput {
		return
	}

	totalSlashCommands := 0
	nonCentralizedSlashCommands := 0
	for _, wd := range workflowDataList {
		if wd == nil || len(wd.Command) == 0 {
			continue
		}
		totalSlashCommands += len(wd.Command)
		if !wd.CommandCentralized {
			nonCentralizedSlashCommands += len(wd.Command)
		}
	}

	if totalSlashCommands < 3 || nonCentralizedSlashCommands == 0 {
		return
	}

	verb := "are"
	if nonCentralizedSlashCommands == 1 {
		verb = "is"
	}
	msg := fmt.Sprintf(
		"Detected %d slash_command entries in this repository; %d %s not using centralized routing. Consider setting `on.slash_command.strategy: centralized` to reduce duplicate triggers and route through `agentic_commands.yml`.",
		totalSlashCommands,
		nonCentralizedSlashCommands,
		verb,
	)
	fmt.Fprintln(os.Stderr, console.FormatWarningMessageStderr(msg))
	compiler.IncrementWarningCount()
}

// pruneStaleActionCacheEntries removes stale gh-aw-actions entries from the
// action cache whose version does not match the compiler's current version.
// This prevents actions-lock.json from accumulating entries for old compiler
// releases that are no longer referenced by any compiled workflow.
func pruneStaleActionCacheEntries(compiler *workflow.Compiler, actionCache *workflow.ActionCache) {
	if actionCache == nil {
		return
	}

	// Determine the effective version: actionTag takes precedence when explicitly
	// set (e.g., via --action-tag for testing against a specific release), otherwise
	// fall back to the compiler's built-in version from the binary.
	version := compiler.GetActionTag()
	if version == "" {
		version = compiler.GetVersion()
	}

	actionCache.PruneStaleGHAWEntries(version, compiler.EffectiveActionsRepo())
}

// pruneOrphanedActionCacheEntries removes entries from the action cache that were
// not referenced during the current compilation run. This garbage-collects entries
// for action versions no longer used by any workflow in the target directory (e.g.
// old version pins left behind after bumping a `uses:` tag).
//
// This is only safe to call after a full-directory compilation — compiling a
// subset of files would incorrectly prune entries still referenced by other
// (uncompiled) workflows — and only when there were zero compile errors.
func pruneOrphanedActionCacheEntries(compiler *workflow.Compiler, actionCache *workflow.ActionCache, errorCount int) {
	if actionCache == nil {
		return
	}
	if errorCount > 0 {
		return
	}

	resolver := compiler.GetSharedActionResolver()
	if resolver == nil {
		return
	}

	usedKeys := resolver.GetUsedCacheKeys()
	pruned := actionCache.PruneOrphanedEntries(usedKeys)
	if pruned > 0 {
		compilePostProcessingLog.Printf("Pruned %d orphaned entries from actions-lock.json", pruned)
	}
}
