package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"charm.land/huh/v2"
	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/errorutil"
	"github.com/github/gh-aw/pkg/workflow"
)

func isAlreadyMergedGHError(err error) bool {
	if err == nil {
		return false
	}
	return errorutil.IsAlreadyMergedError(err)
}

type mergeAction string

const (
	mergeActionAttempt   mergeAction = "attempt"
	mergeActionEditTitle mergeAction = "editTitle"
	mergeActionReview    mergeAction = "review"
	mergeActionConfirmed mergeAction = "confirmed"
	mergeActionExit      mergeAction = "exit"
)

// createWorkflowChangesAndConfigureSecret writes the workflows, optionally creates and merges a PR, and adds the secret.
func (c *AddInteractiveConfig) createWorkflowChangesAndConfigureSecret(ctx context.Context, workflowFiles []string, initFiles []addInitializedFile, secretName, secretValue string, createPR bool) error {
	addInteractiveLog.Print("Applying changes")

	// Add the workflow using the existing implementation.
	// Pass the resolved workflows to avoid re-fetching them
	// Pass Quiet=true to suppress detailed output (already shown earlier in interactive mode)
	// This returns the result including PR number and HasWorkflowDispatch
	opts := AddOptions{
		Verbose:                          c.Verbose,
		Quiet:                            true,
		EngineOverride:                   c.EngineOverride,
		Name:                             "",
		Force:                            c.forceOverwrite,
		AppendText:                       c.AppendText,
		CreatePR:                         createPR,
		NoGitattributes:                  c.NoGitattributes,
		WorkflowDir:                      c.WorkflowDir,
		NoStopAfter:                      c.NoStopAfter,
		StopAfter:                        c.StopAfter,
		DisableSecurityScanner:           c.DisableSecurityScanner,
		RepoSlug:                         c.RepoOverride,
		AddCopilotRequestsPermission:     c.UseCopilotRequests,
		AddCopilotRequestsNonePermission: c.UseCopilotPAT,
		GhAwRef:                          c.GhAwRef,
		addWizard: &addWizardOptions{
			initializedFiles:                    initFiles,
			workingTreePrevalidated:             createPR,
			showInteractiveProgress:             true,
			skipSecret:                          c.SkipSecret,
			disableGitHubAppPermissionInference: c.DisableGitHubAppPermissionInference,
		},
	}
	opts.addWizard.secretSource = c.secretSources["COPILOT_GITHUB_TOKEN"]
	result, err := AddResolvedWorkflows(ctx, c.WorkflowSpecs, c.resolvedWorkflows, opts)
	if err != nil {
		return fmt.Errorf("failed to add workflow: %w", err)
	}
	c.addResult = result

	if !createPR {
		return nil
	}

	if err := c.ensurePullRequestMerged(result.PRNumber, result.PRURL); err != nil {
		return err
	}

	// Step 8c: Add the secret (skip if no secret configured or already exists in repository).
	return c.configureRepositorySecret(secretName, secretValue)
}

func (c *AddInteractiveConfig) ensurePullRequestMerged(prNumber int, prURL string) error {
	addInteractiveLog.Printf("Ensuring PR merged: prNumber=%d", prNumber)
	if prNumber == 0 {
		if prURL == "" {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Requested workflow files already exist locally; no pull request was created."))
			return nil
		}
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Could not determine PR number"))
		fmt.Fprintln(os.Stderr, "Please merge the PR manually from the GitHub web interface.")
		return nil
	}

	fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Pull request created: "+prURL))
	return c.runPRMergeLoop(prNumber, prURL)
}

func (c *AddInteractiveConfig) runPRMergeLoop(prNumber int, prURL string) error {
	mergeDone := false
	mergeFailed := false
	userReviewing := false

	for !mergeDone {
		chosen, err := promptMergeAction(prURL, mergeFailed, userReviewing)
		if err != nil {
			return err
		}

		switch chosen {
		case mergeActionAttempt:
			done, failed := c.handleMergeAttempt(prNumber, prURL, mergeFailed)
			mergeDone = done
			mergeFailed = failed
		case mergeActionEditTitle:
			updated, err := c.promptAndEditPRTitle(prNumber)
			if err != nil {
				return err
			}
			if updated {
				mergeFailed = false
			}
		case mergeActionReview:
			userReviewing = true
		case mergeActionConfirmed:
			fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Great – continuing with the merged pull request"))
			mergeDone = true
		case mergeActionExit:
			fmt.Fprintln(os.Stderr, "")
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Exiting. You can merge the pull request later: "+prURL))
			return errors.New("user exited before PR was merged")
		}
	}

	return nil
}

func promptMergeAction(prURL string, mergeFailed, userReviewing bool) (mergeAction, error) {
	var chosen mergeAction
	selectField := huh.NewSelect[mergeAction]().
		Title("What would you like to do with pull request " + prURL + "?").
		Options(buildMergeOptions(mergeFailed, userReviewing)...).
		Value(&chosen)
	if userReviewing {
		selectField = selectField.Description("Please review and merge the pull request before continuing: " + prURL)
	}
	selectForm := console.NewSelectForm(selectField)
	if err := selectForm.Run(); err != nil {
		return "", fmt.Errorf("failed to get user input: %w", err)
	}
	return chosen, nil
}

func buildMergeOptions(mergeFailed, userReviewing bool) []huh.Option[mergeAction] {
	options := []huh.Option[mergeAction]{
		huh.NewOption("Attempt to merge", mergeActionAttempt),
	}
	if mergeFailed {
		options = append(options, huh.NewOption("Edit PR title and retry", mergeActionEditTitle))
	}
	if userReviewing {
		options = append(options, huh.NewOption("PR has been manually merged", mergeActionConfirmed))
		options = append(options, huh.NewOption("Exit, I'm done here", mergeActionExit))
		return options
	}
	options = append(options, huh.NewOption("I'll review/merge myself", mergeActionReview))
	options = append(options, huh.NewOption("Exit", mergeActionExit))
	return options
}

func (c *AddInteractiveConfig) handleMergeAttempt(prNumber int, prURL string, mergeFailed bool) (mergeDone bool, nowFailed bool) {
	if mergeErr := c.mergePullRequest(prNumber); mergeErr != nil {
		if isAlreadyMergedGHError(mergeErr) {
			fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Merged pull request "+prURL))
			return true, mergeFailed
		}
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to merge PR: %v", mergeErr)))
		if mergeFailed {
			fmt.Fprintln(os.Stderr, "Please merge the PR manually: "+prURL)
		}
		return false, true
	}

	fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Merged pull request "+prURL))
	return true, mergeFailed
}

func (c *AddInteractiveConfig) promptAndEditPRTitle(prNumber int) (bool, error) {
	var newTitle string
	titleForm := console.NewInputForm(
		huh.NewInput().
			Title("Enter new PR title").
			Description("Add a prefix if required, for example: feat: or fix:").
			Value(&newTitle),
	)
	if err := titleForm.Run(); err != nil {
		return false, fmt.Errorf("failed to get user input: %w", err)
	}
	newTitle = strings.TrimSpace(newTitle)
	if newTitle == "" {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage("PR title cannot be empty, keeping current title"))
		return false, nil
	}
	if err := editPRTitle(prNumber, newTitle, c.RepoOverride); err != nil {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to update PR title: %v", err)))
		return false, nil
	}
	fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("PR title updated to: "+newTitle))
	return true, nil
}

func (c *AddInteractiveConfig) configureRepositorySecret(secretName, secretValue string) error {
	if secretName == "" {
		// No secret to configure (e.g., user doesn't have write access to the repository)
	} else if secretValue == "" {
		// Secret already exists in repo, nothing to do
		if c.Verbose {
			fmt.Fprintln(os.Stderr, "")
			fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("Secret '%s' already configured", secretName)))
		}
	} else {
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, console.FormatProgressMessage(fmt.Sprintf("Adding secret '%s' to repository...", secretName)))

		if err := c.addRepositorySecret(secretName, secretValue); err != nil {
			fmt.Fprintln(os.Stderr, console.FormatErrorMessage(fmt.Sprintf("Failed to add secret: %v", err)))
			fmt.Fprintln(os.Stderr, "")
			fmt.Fprintln(os.Stderr, "Please add the secret manually:")
			fmt.Fprintln(os.Stderr, "  1. Go to your repository Settings → Secrets and variables → Actions")
			fmt.Fprintf(os.Stderr, "  2. Click 'New repository secret' and add '%s'\n", secretName)
			return fmt.Errorf("failed to add secret: %w", err)
		}

		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("Secret '%s' added", secretName)))
	}

	return nil
}

// updateLocalBranch fetches and pulls the latest changes from GitHub after PR merge.
// It switches to the default branch before pulling so that the working tree contains
// the merged workflow files, which are required when offering to run the workflow.
func (c *AddInteractiveConfig) updateLocalBranch() error {
	addInteractiveLog.Print("Updating local branch with merged changes")
	// Get the default branch name using gh
	output, err := workflow.RunGHCombined("Getting default branch...", "repo", "view", "--repo", c.RepoOverride, "--json", "defaultBranchRef", "--jq", ".defaultBranchRef.name")
	defaultBranch := ""
	if err == nil {
		defaultBranch = strings.TrimSpace(string(output))
	}
	// Fallback: query the local origin remote directly (works even when gh repo
	// view fails, e.g. forks without a default remote set).
	if defaultBranch == "" {
		addInteractiveLog.Print("gh repo view failed, trying git ls-remote to detect default branch")
		lsCmd := exec.Command("git", "ls-remote", "--symref", "origin", "HEAD")
		lsOutput, lsErr := lsCmd.CombinedOutput()
		if lsErr == nil {
			defaultBranch = parseDefaultBranchFromLsRemote(string(lsOutput))
		}
	}
	if defaultBranch == "" {
		defaultBranch = "main"
	}
	addInteractiveLog.Printf("Default branch: %s", defaultBranch)

	// Fetch the latest changes from origin
	if c.Verbose {
		fmt.Fprintln(os.Stderr, console.FormatProgressMessage("Fetching latest changes from GitHub..."))
	}

	fetchCmd := exec.Command("git", "fetch", "origin", defaultBranch)
	fetchOutput, err := fetchCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git fetch failed: %w (output: %s)", err, string(fetchOutput))
	}

	// Switch to the default branch so the working tree contains the merged workflow
	// files. Without this, users on a feature branch won't have the files locally and
	// the subsequent "run workflow" step will fail with "workflow file not found".
	currentBranch, err := getCurrentBranch()
	if err != nil {
		addInteractiveLog.Printf("Could not determine current branch: %v", err)
		currentBranch = ""
	}

	if currentBranch != defaultBranch {
		addInteractiveLog.Printf("Switching from %q to default branch %q", currentBranch, defaultBranch)
		if err := switchBranch(defaultBranch, c.Verbose); err != nil {
			return fmt.Errorf("failed to switch to default branch %s: %w", defaultBranch, err)
		}
	}

	pullCmd := exec.Command("git", "pull", "origin", defaultBranch)
	pullOutput, err := pullCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git pull failed: %w (output: %s)", err, string(pullOutput))
	}

	if c.Verbose {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Local branch updated with merged changes"))
	}

	return nil
}

type addWorkingTreeBlockers struct {
	staged      []string
	overlapping []string
}

func (b addWorkingTreeBlockers) empty() bool {
	return len(b.staged) == 0 && len(b.overlapping) == 0
}

type workingTreeResolution string

const (
	workingTreeOverwrite workingTreeResolution = "overwrite"
	workingTreeCleaned   workingTreeResolution = "cleaned"
	workingTreeExit      workingTreeResolution = "exit"
)

// checkCleanWorkingDirectoryForPR allows unrelated unstaged and untracked files,
// but requires staged changes and edits to files the wizard will write to be cleaned.
func (c *AddInteractiveConfig) checkCleanWorkingDirectoryForPR(workflowFiles, initFiles []string) error {
	addInteractiveLog.Print("Checking working tree changes before PR creation")
	gitRoot, err := addFindGitRoot()
	if err != nil {
		return fmt.Errorf("failed to determine repository root for PR preflight: %w", err)
	}
	plannedPaths, err := c.plannedAddPathsAtRoot(gitRoot, workflowFiles, initFiles)
	if err != nil {
		return err
	}

	for {
		if c.Ctx != nil {
			select {
			case <-c.Ctx.Done():
				return c.Ctx.Err()
			default:
			}
		}
		blockers, inspectErr := inspectAddWorkingTreeAtRoot(gitRoot, plannedPaths)
		if inspectErr != nil {
			return inspectErr
		}
		if blockers.empty() {
			fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Working tree is ready for pull request creation"))
			return nil
		}

		allowOverwrite := len(blockers.staged) == 0 && len(blockers.overlapping) > 0
		resolution, promptErr := promptWorkingTreeResolution(c.Ctx, blockers, allowOverwrite)
		if promptErr != nil {
			return promptErr
		}
		switch resolution {
		case workingTreeOverwrite:
			c.forceOverwrite = true
			fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Overlapping workflow files will be overwritten"))
			return nil
		case workingTreeExit:
			return errors.New("user exited before cleaning the working tree")
		}
	}
}

func (c *AddInteractiveConfig) plannedAddPathsAtRoot(gitRoot string, workflowFiles, initFiles []string) ([]string, error) {
	workflowDir := c.WorkflowDir
	if workflowDir == "" {
		workflowDir = getWorkflowsDir()
	}
	planned := make([]string, 0, len(workflowFiles)+len(initFiles))
	for _, path := range workflowFiles {
		planned = append(planned, filepath.Join(workflowDir, path))
	}
	planned = append(planned, initFiles...)
	for index, path := range planned {
		if filepath.IsAbs(path) {
			rel, relErr := filepath.Rel(gitRoot, path)
			if relErr != nil {
				return nil, fmt.Errorf("failed to resolve planned path %s: %w", path, relErr)
			}
			path = rel
		}
		planned[index] = filepath.ToSlash(filepath.Clean(path))
	}
	return planned, nil
}

func inspectAddWorkingTreeAtRoot(gitRoot string, plannedPaths []string) (addWorkingTreeBlockers, error) {
	cmd := exec.Command("git", "status", "--porcelain=v1", "-z", "--untracked-files=all")
	cmd.Dir = gitRoot
	output, err := cmd.Output()
	if err != nil {
		return addWorkingTreeBlockers{}, fmt.Errorf("failed to inspect working tree: %w", err)
	}

	planned := make(map[string]struct{}, len(plannedPaths))
	for _, path := range plannedPaths {
		planned[filepath.ToSlash(filepath.Clean(path))] = struct{}{}
	}
	var blockers addWorkingTreeBlockers
	entries := strings.Split(string(output), "\x00")
	for index := 0; index < len(entries); index++ {
		entry := entries[index]
		if len(entry) < 4 {
			continue
		}
		status := entry[:2]
		path := filepath.ToSlash(filepath.Clean(entry[3:]))
		if status[0] != ' ' && status[0] != '?' {
			blockers.staged = appendUniqueString(blockers.staged, path)
		}
		if _, overlaps := planned[path]; overlaps {
			blockers.overlapping = appendUniqueString(blockers.overlapping, path)
		}
		if status[0] == 'R' || status[0] == 'C' {
			index++
		}
	}
	return blockers, nil
}

func appendUniqueString(values []string, value string) []string {
	if slices.Contains(values, value) {
		return values
	}
	return append(values, value)
}

func promptWorkingTreeResolution(ctx context.Context, blockers addWorkingTreeBlockers, allowOverwrite bool) (workingTreeResolution, error) {
	var resolution workingTreeResolution
	form := console.NewSelectForm(
		huh.NewSelect[workingTreeResolution]().
			Title("Some working tree changes must be resolved before creating the pull request.").
			Description(formatWorkingTreeBlockers(blockers)).
			Options(buildWorkingTreeResolutionOptions(allowOverwrite)...).
			Value(&resolution),
	)
	if err := form.RunWithContext(ctx); err != nil {
		return "", fmt.Errorf("working tree confirmation failed: %w", err)
	}
	return resolution, nil
}

func formatWorkingTreeBlockers(blockers addWorkingTreeBlockers) string {
	sections := make([]string, 0, 2)
	if len(blockers.staged) > 0 {
		sections = append(sections, "Staged changes:\n  • "+strings.Join(blockers.staged, "\n  • "))
	}
	if len(blockers.overlapping) > 0 {
		sections = append(sections, "Changes overlapping files the wizard will add:\n  • "+strings.Join(blockers.overlapping, "\n  • "))
	}
	return strings.Join(sections, "\n")
}

func buildWorkingTreeResolutionOptions(allowOverwrite bool) []huh.Option[workingTreeResolution] {
	options := make([]huh.Option[workingTreeResolution], 0, 3)
	if allowOverwrite {
		options = append(options, huh.NewOption("Overwrite", workingTreeOverwrite).Selected(true))
	}
	return append(options,
		huh.NewOption("I've cleaned the working tree", workingTreeCleaned),
		huh.NewOption("Exit, I'm done here", workingTreeExit),
	)
}

// squashMergeNotAllowedErr is the lowercase substring of the GitHub GraphQL API error
// returned when a repository does not permit squash merges. It is used to detect when
// a squash merge should be retried with a merge-commit strategy.
const squashMergeNotAllowedErr = "squash merges are not allowed"

// mergePullRequest merges the specified PR, attempting a squash merge first and
// falling back to a merge commit if squash merges are not allowed on the repository.
func (c *AddInteractiveConfig) mergePullRequest(prNumber int) error {
	prArg := strconv.Itoa(prNumber)
	squashOutput, squashErr := workflow.RunGHCombined("Merging pull request (squash)...", "pr", "merge", prArg, "--repo", c.RepoOverride, "--squash")
	if squashErr == nil {
		return nil
	}

	// If squash merges are not allowed on this repository (e.g. only merge commits or rebase
	// merges are enabled), fall back to a merge commit. The error text comes from the GitHub
	// GraphQL API and is surfaced verbatim in the gh CLI output.
	combinedText := strings.ToLower(string(squashOutput) + squashErr.Error())
	if strings.Contains(combinedText, squashMergeNotAllowedErr) {
		addInteractiveLog.Printf("Squash merge rejected for PR #%d, retrying with merge commit", prNumber)
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Squash merges are not allowed on this repository, retrying with merge commit"))
		mergeOutput, mergeErr := workflow.RunGHCombined("Merging pull request...", "pr", "merge", prArg, "--repo", c.RepoOverride, "--merge")
		if mergeErr != nil {
			return fmt.Errorf("merge failed: %w (output: %s)", mergeErr, string(mergeOutput))
		}
		return nil
	}

	return fmt.Errorf("merge failed: %w (output: %s)", squashErr, string(squashOutput))
}

// editPRTitle updates the title of the specified PR via the gh CLI.
func editPRTitle(prNumber int, newTitle, repoOverride string) error {
	args := []string{"pr", "edit", strconv.Itoa(prNumber), "--title", newTitle}
	if repoOverride != "" {
		args = append(args, "--repo", repoOverride)
	}
	output, err := workflow.RunGHCombined("Updating PR title...", args...)
	if err != nil {
		return fmt.Errorf("failed to update PR title: %w (output: %s)", err, string(output))
	}
	return nil
}

// parseDefaultBranchFromLsRemote extracts the default branch name from
// the output of `git ls-remote --symref origin HEAD`.
//
// Example output:
//
//	ref: refs/heads/main	abc123
//	abc123	HEAD
//
// Returns "" if the branch cannot be determined.
func parseDefaultBranchFromLsRemote(output string) string {
	for line := range strings.SplitSeq(output, "\n") {
		if !strings.HasPrefix(line, "ref: refs/heads/") {
			continue
		}
		// line is e.g. "ref: refs/heads/main\tabc123"
		// Split on tab first to isolate the symref part from the hash.
		tabParts := strings.SplitN(line, "\t", 2)
		ref := strings.TrimPrefix(tabParts[0], "ref: refs/heads/")
		ref = strings.TrimSpace(ref)
		if ref != "" {
			return ref
		}
	}
	return ""
}
