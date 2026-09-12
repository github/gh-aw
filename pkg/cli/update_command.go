package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/gitutil"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/spf13/cobra"
)

var updateLog = logger.New("cli:update_command")

const updateTargetRepoCheckoutDir = ".github/aw/updates"

// NewUpdateCommand creates the update command
func NewUpdateCommand(validateEngine func(string) error) *cobra.Command { //nolint:largefunc // Existing command setup remains centralized.
	cmd := &cobra.Command{
		Use:   "update [workflow]...",
		Short: "Update agentic workflows from their source repositories",
		Long: `Update one or more agentic workflows from their source repositories.

The update command fetches the latest version of each workflow from its source
repository, merges upstream changes with any local modifications, and recompiles.

If no workflow names are specified, all workflows with a 'source' field are updated.

By default, the update performs a 3-way merge to preserve your local changes.
Use --no-merge to override local changes with the upstream version.

By default, update also bumps all referenced GitHub Actions to their latest major version.
Use --no-release-bump to restrict auto-bumping to core actions/* actions only.

For workflow updates, it fetches the latest version based on the current ref:
- If the ref is a tag, it updates to the latest release (use --major for major version updates)
- If the ref is a branch, it fetches the latest commit from that branch
- If the ref is a commit SHA, it fetches the latest commit from the default branch

For extension updates, action updates, agent files, and codemods, use 'gh aw upgrade'.

Note: In GitHub Enterprise repos, shorthand source specs resolve on your enterprise host by default.
      For github/*, githubnext/*, and microsoft/* sources, shorthand resolves on github.com.
      Use full https://github.com/... source URLs for other public github.com workflows.

` + WorkflowIDExplanation,
		Example: `  ` + string(constants.CLIExtensionPrefix) + ` update                    # Update all workflows from source
  ` + string(constants.CLIExtensionPrefix) + ` update repo-assist        # Update a specific workflow
  ` + string(constants.CLIExtensionPrefix) + ` update repo-assist.md     # Same (alternative format)
  ` + string(constants.CLIExtensionPrefix) + ` update --org my-org       # Preview workflow updates across an organization
  ` + string(constants.CLIExtensionPrefix) + ` update --org my-org --repos '*-service'  # Limit org mode to matching repositories
  ` + string(constants.CLIExtensionPrefix) + ` update --org my-org --create-issue  # Open issues in repos with pending updates
  ` + string(constants.CLIExtensionPrefix) + ` update --org my-org --create-issue --yes  # Auto-accept per-repo confirmations (required in CI)
  ` + string(constants.CLIExtensionPrefix) + ` update --org my-org --create-pull-request --yes  # Auto-accept per-repo confirmations for PR creation (required in CI)
  ` + string(constants.CLIExtensionPrefix) + ` update --no-merge         # Override local changes with upstream
  ` + string(constants.CLIExtensionPrefix) + ` update repo-assist --major # Allow major version updates
  ` + string(constants.CLIExtensionPrefix) + ` update --force            # Force update even if no changes
  ` + string(constants.CLIExtensionPrefix) + ` update --no-release-bump     # Disable force-bumping non-core actions (core actions/* are still force-updated)
  ` + string(constants.CLIExtensionPrefix) + ` update --no-compile           # Update without regenerating lock files
  ` + string(constants.CLIExtensionPrefix) + ` update --no-redirect          # Refuse workflows that use redirect frontmatter
  ` + string(constants.CLIExtensionPrefix) + ` update --dir custom/workflows  # Update workflows in custom directory
  ` + string(constants.CLIExtensionPrefix) + ` update --repo owner/repo        # Update workflows in another repository
  ` + string(constants.CLIExtensionPrefix) + ` update --create-pull-request   # Update and open a pull request
  ` + string(constants.CLIExtensionPrefix) + ` update --cool-down 0           # Disable cooldown and apply all pending releases immediately
  ` + string(constants.CLIExtensionPrefix) + ` update --cool-down 3d          # Apply a custom 3-day cooldown period`,
		RunE: func(cmd *cobra.Command, args []string) error { //nolint:largefunc // Existing update option validation and routing remain centralized.
			majorFlag, _ := cmd.Flags().GetBool("major")
			forceFlag, _ := cmd.Flags().GetBool("force")
			engineOverride, _ := cmd.Flags().GetString("engine")
			verbose, _ := cmd.Flags().GetBool("verbose")
			workflowDir, _ := cmd.Flags().GetString("dir")
			noStopAfter, _ := cmd.Flags().GetBool("no-stop-after")
			stopAfter, _ := cmd.Flags().GetString("stop-after")
			noMergeFlag, _ := cmd.Flags().GetBool("no-merge")
			disableReleaseBump := resolveDeprecatedBoolFlag(cmd, "no-release-bump", "disable-release-bump")
			noCompile, _ := cmd.Flags().GetBool("no-compile")
			noRedirect, _ := cmd.Flags().GetBool("no-redirect")
			disableSecurityScanner := resolveDeprecatedBoolFlag(cmd, "no-security-scanner", "disable-security-scanner")
			approveFlag, _ := cmd.Flags().GetBool("approve")
			createPRFlag, _ := cmd.Flags().GetBool("create-pull-request")
			prFlagAlias, _ := cmd.Flags().GetBool("pr")
			createPR := createPRFlag || prFlagAlias
			createIssue, _ := cmd.Flags().GetBool("create-issue")
			yes, _ := cmd.Flags().GetBool("yes")
			coolDownStr, _ := cmd.Flags().GetString("cool-down")
			targetRepo, _ := cmd.Flags().GetString("repo")
			targetOrg, _ := cmd.Flags().GetString("org")
			repoGlobs, _ := cmd.Flags().GetStringSlice("repos")

			if err := validateEngine(engineOverride); err != nil {
				return err
			}

			coolDown, err := parseCoolDownFlag(coolDownStr)
			if err != nil {
				return fmt.Errorf("invalid --cool-down value: %w", err)
			}

			if targetRepo != "" && targetOrg != "" {
				return errors.New("cannot specify both --repo and --org flags; use --repo for a single repository or --org for organization-wide updates")
			}

			if createIssue && targetOrg == "" {
				return errors.New("--create-issue requires --org to be specified")
			}

			if createPR && createIssue {
				return errors.New("cannot specify both --create-pull-request and --create-issue")
			}

			if createPR && targetRepo == "" && targetOrg == "" {
				if err := PreflightCheckForCreatePR(verbose); err != nil {
					return err
				}
			}

			opts := UpdateWorkflowsOptions{
				WorkflowNames:          args,
				AllowMajor:             majorFlag,
				Force:                  forceFlag,
				Yes:                    yes,
				Verbose:                verbose,
				EngineOverride:         engineOverride,
				WorkflowsDir:           workflowDir,
				NoStopAfter:            noStopAfter,
				StopAfter:              stopAfter,
				NoMerge:                noMergeFlag,
				DisableReleaseBump:     disableReleaseBump,
				NoCompile:              noCompile,
				NoRedirect:             noRedirect,
				DisableSecurityScanner: disableSecurityScanner,
				CoolDown:               coolDown,
				Approve:                approveFlag,
				DraftPullRequest:       prFlagAlias,
			}

			if targetRepo != "" {
				return runUpdateForTargetRepo(cmd.Context(), targetRepo, opts, createPR, verbose)
			}

			if targetOrg != "" {
				return runUpdateForOrg(cmd.Context(), targetOrg, repoGlobs, opts, createPR, createIssue, verbose)
			}

			if err := RunUpdateWorkflows(cmd.Context(), opts); err != nil {
				return err
			}

			if createPR {
				prBody := "This PR updates agentic workflows from their source repositories."
				createPRFn := CreatePRWithChanges
				if opts.DraftPullRequest {
					createPRFn = CreateDraftPRWithChanges
				}
				_, err := createPRFn(cmd.Context(), "update-workflows", "chore: update workflows",
					"Update workflows from source", prBody, verbose)
				return err
			}
			return nil
		},
	}

	cmd.Flags().Bool("major", false, "Allow major version updates when updating tagged releases")
	cmd.Flags().BoolP("force", "f", false, "Force update of workflow files even if no changes are detected")
	addEngineFlag(cmd)
	cmd.Flags().StringP("dir", "d", "", "Workflow directory (default: $GH_AW_WORKFLOWS_DIR or .github/workflows)")
	cmd.Flags().Bool("no-stop-after", false, "Remove any stop-after field from the workflow")
	cmd.Flags().String("stop-after", "", "Override stop-after value in the workflow (e.g., '+48h', '2025-12-31 23:59:59')")
	cmd.Flags().Bool("no-merge", false, "Skip merging; override local changes with the upstream version")
	cmd.Flags().Bool("no-release-bump", false, "Skip automatic major version bumps for non-core actions (only core actions/* are bumped)")
	cmd.Flags().Bool("disable-release-bump", false, "Skip automatic major version bumps for non-core actions (only core actions/* are bumped)")
	_ = cmd.Flags().MarkDeprecated("disable-release-bump", "use --no-release-bump instead")
	addSecurityScannerFlag(cmd)
	cmd.Flags().Bool("approve", false, "Approve all safe update changes. When strict mode is active (the default), the compiler emits warnings for new restricted secrets or unapproved action additions/removals not present in the existing gh-aw-manifest. Use this flag to approve and skip safe update enforcement")
	cmd.Flags().Bool("no-compile", false, "Skip recompiling workflows during update (do not modify lock files)")
	cmd.Flags().Bool("no-redirect", false, "Skip following redirects; refuse updates when redirect frontmatter is present")
	cmd.Flags().String("org", "", "Preview or create workflow update pull requests across an organization")
	cmd.Flags().StringSlice("repos", nil, "Limit --org mode to repositories matching one or more glob patterns")
	addRepoFlag(cmd)
	cmd.Flags().Bool("create-pull-request", false, "Create a pull request with the update changes")
	cmd.Flags().Bool("pr", false, "Create a draft pull request with the update changes")
	cmd.Flags().Bool("create-issue", false, "Open a GitHub issue in each org repository that has pending workflow updates (requires --org)")
	cmd.Flags().BoolP("yes", "y", false, "Auto-accept org-mode update confirmations (required in CI)")
	cmd.Flags().String("cool-down", "7d", coolDownFlagUsage)
	// Register completions for update command
	cmd.ValidArgsFunction = CompleteWorkflowNames
	RegisterEngineFlagCompletion(cmd)
	RegisterDirFlagCompletion(cmd, "dir")

	return cmd
}

// RunUpdateWorkflows updates workflows from their source repositories.
// Each workflow is compiled immediately after update.
func RunUpdateWorkflows(ctx context.Context, opts UpdateWorkflowsOptions) error { //nolint:largefunc // Existing update stages remain centralized.
	updateLog.Printf("Starting update process: workflows=%v, allowMajor=%v, force=%v, noMerge=%v, disableReleaseBump=%v, noCompile=%v, noRedirect=%v, coolDown=%v", opts.WorkflowNames, opts.AllowMajor, opts.Force, opts.NoMerge, opts.DisableReleaseBump, opts.NoCompile, opts.NoRedirect, opts.CoolDown)

	var firstErr error
	actionDeps := newCachedActionUpdateDeps(defaultActionUpdateDeps())

	if err := UpdateWorkflows(ctx, opts); err != nil {
		firstErr = fmt.Errorf("workflow update failed: %w", err)
	}

	// Update GitHub Actions versions in actions-lock.json.
	// By default all actions are updated to the latest major version.
	// Pass --no-release-bump to revert to only forcing updates for core (actions/*) actions.
	updateLog.Printf("Updating GitHub Actions versions in actions-lock.json: allowMajor=%v, disableReleaseBump=%v", opts.AllowMajor, opts.DisableReleaseBump)
	if err := updateActions(ctx, actionDeps, opts.AllowMajor, opts.Verbose, opts.DisableReleaseBump, opts.CoolDown); err != nil {
		// Non-fatal: warn but don't fail the update
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Could not update actions-lock.json: %v", err)))
	}

	// Update action references in user-provided steps within workflow .md files.
	// By default all org/repo@version references are updated to the latest major version.
	updateLog.Print("Updating action references in workflow .md files")
	if err := updateActionsInWorkflowFiles(ctx, actionDeps, updateActionsOptions{
		workflowsDir:       opts.WorkflowsDir,
		engineOverride:     opts.EngineOverride,
		verbose:            opts.Verbose,
		disableReleaseBump: opts.DisableReleaseBump,
		noCompile:          opts.NoCompile,
		coolDown:           opts.CoolDown,
		approve:            opts.Approve,
	}); err != nil {
		var compilationErr *updateCompilationError
		if errors.As(err, &compilationErr) {
			compileErr := fmt.Errorf("workflow compilation after action reference update failed: %w", err)
			if firstErr == nil {
				firstErr = compileErr
			} else {
				firstErr = errors.Join(firstErr, compileErr)
			}
		} else {
			// Non-fatal: warn but don't fail the update
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Could not update action references in workflow files: %v", err)))
		}
	}

	// Resolve and store SHA-256 digest pins for container images referenced in lock files.
	// This runs after compilation (via UpdateActionsInWorkflowFiles) so that the lock files
	// already reflect the current AWF version; stale pins from superseded versions are pruned
	// and new versions are resolved in a single pass.
	updateLog.Print("Updating container image digest pins")
	newContainerPins, err := updateContainerPins(ctx, defaultContainerPinUpdateDeps(), opts.WorkflowsDir, opts.Verbose, containerPinUpdateOptions{refreshExisting: true})
	if err != nil {
		// Non-fatal: Docker may not be available in all environments.
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Could not update container pins: %v", err)))
	}

	// Recompile all workflows when new container pins were added so that the
	// lock files embed the digest-pinned image references (image:tag@sha256:…).
	if newContainerPins && !opts.NoCompile {
		updateLog.Print("Recompiling workflows to embed new container digest pins")
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Recompiling workflows to embed container digest pins..."))
		recompileErr := recompileAllWorkflows(ctx, opts.WorkflowsDir, opts.EngineOverride, opts.Verbose, opts.Approve)
		if recompileErr != nil {
			compileErr := fmt.Errorf("workflow compilation after container pin update failed: %w", recompileErr)
			if firstErr == nil {
				firstErr = compileErr
			} else {
				firstErr = errors.Join(firstErr, compileErr)
			}
		}
	}

	if firstErr == nil {
		updateLog.Print("Validating action and container SHAs in actions-lock.json")
		if err := validateUpdateSHAEntries(ctx, "."); err != nil {
			return fmt.Errorf("update validation failed: %w", err)
		}
		if opts.Verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Validated action and container SHAs in actions-lock.json"))
		}
	}

	updateLog.Printf("Update process complete: had_error=%v", firstErr != nil)
	return firstErr
}

// recompileAllWorkflows recompiles all .md workflow files in the given directory.
// This is used after container pin updates to embed digest-pinned image references
// in the generated lock files.
func recompileAllWorkflows(ctx context.Context, workflowsDir, engineOverride string, verbose bool, approve bool) error {
	if workflowsDir == "" {
		workflowsDir = getWorkflowsDir()
	}
	return compileWorkflowsForUpdate(ctx, nil, workflowsDir, engineOverride, verbose, approve)
}

func runUpdateForTargetRepo(ctx context.Context, targetRepo string, opts UpdateWorkflowsOptions, createPR bool, verbose bool) error { //nolint:largefunc // Keeps the temporary checkout and PR lifecycle atomic.
	gitRoot, err := gitutil.FindGitRoot()
	if err != nil {
		return fmt.Errorf("--repo requires running inside a git repository: %w", err)
	}

	updatesDir, err := ensureUpdateTargetRepoGitignore(gitRoot)
	if err != nil {
		return err
	}

	checkoutDir := filepath.Join(updatesDir, sanitizeRepoPath(targetRepo))
	if err := shallowCloneTargetRepo(ctx, targetRepo, checkoutDir); err != nil {
		return err
	}

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Checked out "+targetRepo+" at "+checkoutDir))
	}

	originalDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to read current directory: %w", err)
	}
	defer func() {
		_ = os.Chdir(originalDir)
	}()

	if err := os.Chdir(checkoutDir); err != nil {
		return fmt.Errorf("failed to change directory to checkout %s: %w", checkoutDir, err)
	}

	if createPR {
		if err := PreflightCheckForCreatePR(verbose); err != nil {
			return err
		}
	}

	if err := RunUpdateWorkflows(ctx, opts); err != nil {
		return err
	}

	if createPR {
		changed, err := hasPendingChanges()
		if err != nil {
			return err
		}
		if !changed {
			updateLog.Printf("Skipping PR for %s: no pending changes after update", targetRepo)
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatVerboseMessage("Skipping PR for "+targetRepo+": already up to date"))
			}
			return nil
		}

		releaseTag, releaseURL := getGhawReleaseInfo(ctx)
		xmlMarker := buildOrgXMLMarker(ghawUpdateMarkerPrefix, releaseTag)

		// Close any stale update PRs in the target repo before creating the new one.
		closeExistingOrgPRsByMarker(ctx, targetRepo, ghawUpdateMarkerPrefix, verbose)

		var releaseLine string
		if releaseURL != "" {
			releaseLine = fmt.Sprintf("\n[View gh-aw release %s](%s)\n", releaseTag, releaseURL)
		}
		prBody := "This PR updates agentic workflows from their source repositories." +
			releaseLine + "\n" + xmlMarker

		createPRFn := CreatePRWithChanges
		if opts.DraftPullRequest {
			createPRFn = CreateDraftPRWithChanges
		}
		prURL, err := createPRFn(ctx, "update-workflows", "chore: update workflows",
			"Update workflows from source", prBody, verbose)
		if err != nil {
			return err
		}
		if prURL != "" {
			addLabelToOrgPR(ctx, prURL, agenticWorkflowsLabel, verbose)
		}
		return nil
	}
	return nil
}

func ensureUpdateTargetRepoGitignore(gitRoot string) (string, error) {
	updatesDir := filepath.Join(gitRoot, updateTargetRepoCheckoutDir)
	if err := os.MkdirAll(updatesDir, constants.DirPermPublic); err != nil {
		return "", fmt.Errorf("failed to create %s: %w", updateTargetRepoCheckoutDir, err)
	}

	gitignorePath := filepath.Join(updatesDir, ".gitignore")
	if _, err := os.Stat(gitignorePath); err == nil {
		return updatesDir, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("failed to stat %s: %w", gitignorePath, err)
	}

	const gitignoreContent = `# Ignore checked-out repositories used by 'gh aw update --repo'
*

# Keep this file in version control
!.gitignore
`
	if err := os.WriteFile(gitignorePath, []byte(gitignoreContent), constants.FilePermSensitive); err != nil {
		return "", fmt.Errorf("failed to write %s: %w", gitignorePath, err)
	}
	return updatesDir, nil
}

func shallowCloneTargetRepo(ctx context.Context, repo, destination string) error {
	if err := os.RemoveAll(destination); err != nil {
		return fmt.Errorf("failed to clean previous checkout %s: %w", destination, err)
	}

	// Use a sparse shallow clone to minimize bandwidth and disk usage when update mode
	// needs a temporary checkout solely for workflow and agent files.
	cmd := exec.CommandContext(ctx, "gh", "repo", "clone", repo, destination, "--", "--depth=1", "--filter=blob:none", "--sparse")
	output, err := cmd.CombinedOutput()
	if err != nil {
		trimmed := strings.TrimSpace(string(output))
		if trimmed == "" {
			return fmt.Errorf("failed to shallow clone %s: %w", repo, err)
		}
		return fmt.Errorf("failed to shallow clone %s: %w: %s", repo, err, trimmed)
	}

	sparseArgs := []string{"-C", destination, "sparse-checkout", "set", ".github"}
	sparseCmd := exec.CommandContext(ctx, "git", sparseArgs...)
	output, err = sparseCmd.CombinedOutput()
	if err != nil {
		trimmed := strings.TrimSpace(string(output))
		if trimmed == "" {
			return fmt.Errorf("failed to configure sparse checkout for %s: %w", repo, err)
		}
		return fmt.Errorf("failed to configure sparse checkout for %s: %w: %s", repo, err, trimmed)
	}
	return nil
}

func sanitizeRepoPath(repo string) string {
	replacer := strings.NewReplacer("/", "__", "\\", "__", ":", "__", "@", "__")
	return replacer.Replace(repo)
}
