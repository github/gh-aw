package cli

// This file iterates over Markdown workflow files, applying the content rewrites
// implemented in update_actions_content_refs.go, and optionally recompiles them.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
)

// UpdateActionsInWorkflowFiles scans all workflow .md files under workflowsDir
// (recursively) and updates any "uses: org/repo@version" references to the latest
// major version. Updated files are recompiled. By default all actions are updated to
// the latest major version; pass disableReleaseBump=true to only update core
// (actions/*) references.
func UpdateActionsInWorkflowFiles(ctx context.Context, workflowsDir, engineOverride string, verbose, disableReleaseBump bool, noCompile bool, coolDown time.Duration, approve bool) error {
	return updateActionsInWorkflowFiles(ctx, defaultActionUpdateDeps(), updateActionsOptions{
		workflowsDir:       workflowsDir,
		engineOverride:     engineOverride,
		verbose:            verbose,
		disableReleaseBump: disableReleaseBump,
		noCompile:          noCompile,
		coolDown:           coolDown,
		approve:            approve,
	})
}

// updateActionsOptions bundles the configuration parameters for updateActionsInWorkflowFiles,
// collapsing a long positional parameter list into a struct.
// engineOverride sets a non-default agentic engine for recompiled workflows.
// disableReleaseBump prevents upgrading action/skill references to newer releases.
// noCompile skips recompilation of updated workflow files.
// coolDown is the minimum age a release must have before it is considered for upgrade.
// approve auto-approves any interactive prompts during recompilation.
type updateActionsOptions struct {
	workflowsDir       string
	engineOverride     string
	verbose            bool
	disableReleaseBump bool
	noCompile          bool
	coolDown           time.Duration
	approve            bool
}

// updateActionsInWorkflowFile updates action, skill and plugin references in one
// workflow file and reports whether the file was rewritten.
func updateActionsInWorkflowFile(ctx context.Context, deps actionUpdateDeps, opts updateActionsOptions, path string, cache map[string]latestReleaseResult, coolDownCache map[string]coolDownCheckResult) (bool, error) {
	warn := func(format string, args ...any) {
		if opts.verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf(format, args...)))
		}
	}

	content, err := os.ReadFile(path)
	if err != nil {
		warn("Failed to read %s: %v", path, err)
		return false, nil
	}
	updatedActions, newContent, err := updateActionRefsInContentWithDeps(ctx, deps, string(content), cache, coolDownCache, !opts.disableReleaseBump, opts.verbose, opts.coolDown)
	if err != nil {
		warn("Failed to update action refs in %s: %v", path, err)
		return false, nil
	}
	updatedSkills, newContent, err := updateSkillRefsInContent(ctx, newContent, !opts.disableReleaseBump, opts.verbose, opts.coolDown)
	if err != nil {
		warn("Failed to update skill refs in %s: %v", path, err)
		return false, nil
	}
	updatedPlugins, newContent, err := updatePluginRefsInContent(ctx, newContent, !opts.disableReleaseBump, opts.verbose, opts.coolDown)
	if err != nil {
		warn("Failed to update plugin refs in %s: %v", path, err)
		return false, nil
	}
	if !updatedActions && !updatedSkills && !updatedPlugins {
		return false, nil
	}
	if err := os.WriteFile(path, []byte(newContent), constants.FilePermPublic); err != nil {
		return false, fmt.Errorf("unable to write updated workflow %s: %w", path, err)
	}
	return true, nil
}

func updateActionsInWorkflowFiles(ctx context.Context, deps actionUpdateDeps, opts updateActionsOptions) error {
	if opts.workflowsDir == "" {
		opts.workflowsDir = getWorkflowsDir()
	}

	updateLog.Printf("Updating action references in workflow files: dir=%s", opts.workflowsDir)

	// Per-invocation cache: key = "repo@currentVersion", avoids repeated API calls
	cache := make(map[string]latestReleaseResult)
	// Per-invocation cooldown cache: key = "repo@tag", avoids redundant date API calls
	coolDownCache := make(map[string]coolDownCheckResult)

	if conflictedWorkflow, err := findWorkflowWithMergeConflictMarkers(opts.workflowsDir); err != nil {
		return fmt.Errorf("unable to scan workflows for merge conflicts: %w", err)
	} else if conflictedWorkflow != "" {
		message := "Skipping action, skill, and plugin updates because merge conflict markers are present in " + conflictedWorkflow
		updateLog.Print(message)
		if opts.verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(message))
		}
		return nil
	}

	var updatedFiles []string

	err := filepath.WalkDir(opts.workflowsDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".md") {
			return nil
		}

		if updated, err := updateActionsInWorkflowFile(ctx, deps, opts, path, cache, coolDownCache); err != nil || !updated {
			return err
		}
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Updated action/skill/plugin references in "+d.Name()))
		updatedFiles = append(updatedFiles, path)
		return nil
	})
	if err != nil {
		return fmt.Errorf("unable to walk workflows directory: %w", err)
	}

	if len(updatedFiles) > 0 && !opts.noCompile {
		if err := compileWorkflowsForUpdate(ctx, updatedFiles, opts.workflowsDir, opts.engineOverride, opts.verbose, opts.approve); err != nil {
			return fmt.Errorf("unable to compile workflows with updated action references: %w", err)
		}
	}

	if len(updatedFiles) == 0 && opts.verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("No action references needed updating in workflow files"))
	}

	return nil
}

func containsMergeConflictMarkers(content string) bool {
	for line := range strings.SplitSeq(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "<<<<<<<") ||
			strings.HasPrefix(trimmed, "|||||||") ||
			strings.HasPrefix(trimmed, ">>>>>>>") {
			return true
		}
	}
	return false
}

func findWorkflowWithMergeConflictMarkers(workflowsDir string) (string, error) {
	if workflowsDir == "" {
		workflowsDir = getWorkflowsDir()
	}
	var conflictedWorkflow string
	err := filepath.WalkDir(workflowsDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".md") {
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if containsMergeConflictMarkers(string(content)) && conflictedWorkflow == "" {
			conflictedWorkflow = path
		}
		return nil
	})
	return conflictedWorkflow, err
}
