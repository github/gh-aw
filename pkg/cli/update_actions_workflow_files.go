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

func updateActionsInWorkflowFiles(ctx context.Context, deps actionUpdateDeps, opts updateActionsOptions) error {
	if opts.workflowsDir == "" {
		opts.workflowsDir = getWorkflowsDir()
	}

	updateLog.Printf("Updating action references in workflow files: dir=%s", opts.workflowsDir)

	// Per-invocation cache: key = "repo@currentVersion", avoids repeated API calls
	cache := make(map[string]latestReleaseResult)
	// Per-invocation cooldown cache: key = "repo@tag", avoids redundant date API calls
	coolDownCache := make(map[string]coolDownCheckResult)

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

		content, err := os.ReadFile(path)
		if err != nil {
			if opts.verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to read %s: %v", path, err)))
			}
			return nil
		}

		updatedActions, newContent, err := updateActionRefsInContentWithDeps(ctx, deps, string(content), cache, coolDownCache, !opts.disableReleaseBump, opts.verbose, opts.coolDown)
		if err != nil {
			if opts.verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to update action refs in %s: %v", path, err)))
			}
			return nil
		}
		updatedSkills, newContent, err := updateSkillRefsInContent(ctx, newContent, !opts.disableReleaseBump, opts.verbose, opts.coolDown)
		if err != nil {
			if opts.verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to update skill refs in %s: %v", path, err)))
			}
			return nil
		}
		updatedPlugins, newContent, err := updatePluginRefsInContent(ctx, newContent, !opts.disableReleaseBump, opts.verbose, opts.coolDown)
		if err != nil {
			if opts.verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to update plugin refs in %s: %v", path, err)))
			}
			return nil
		}

		if !updatedActions && !updatedSkills && !updatedPlugins {
			return nil
		}

		if err := os.WriteFile(path, []byte(newContent), constants.FilePermPublic); err != nil {
			return fmt.Errorf("unable to write updated workflow %s: %w", path, err)
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
