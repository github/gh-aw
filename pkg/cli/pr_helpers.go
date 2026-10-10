package cli

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"os"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
)

var prHelpersLog = logger.New("cli:pr_helpers")

// PreflightCheckForCreatePR validates preconditions for creating a pull request.
// Returns an error if the working directory is dirty or the GitHub CLI is unavailable.
func PreflightCheckForCreatePR(verbose bool) error {
	if !isGHCLIAvailable() {
		return errors.New("GitHub CLI (gh) is required for PR creation but not available")
	}
	if err := checkCleanWorkingDirectory(verbose); err != nil {
		return fmt.Errorf("--create-pull-request requires a clean working directory: %w", err)
	}
	return nil
}

// CreatePRWithChanges creates a new branch, stages and commits all current changes,
// pushes the branch, creates a pull request, and returns to the original branch.
// branchPrefix is used as the prefix for the auto-generated branch name.
// Returns the PR URL on success.
func CreatePRWithChanges(ctx context.Context, branchPrefix, commitMessage, prTitle, prBody string, verbose bool) (string, error) {
	prHelpersLog.Printf("Creating PR with branch prefix: %s", branchPrefix)

	currentBranch, err := getCurrentBranch()
	if err != nil {
		return "", fmt.Errorf("failed to get current branch: %w", err)
	}

	branchName := fmt.Sprintf("%s-%d", branchPrefix, rand.Intn(9000)+1000)
	prHelpersLog.Printf("Creating branch: %s", branchName)
	if err := createAndSwitchBranch(branchName, verbose); err != nil {
		return "", fmt.Errorf("failed to create branch %s: %w", branchName, err)
	}

	// Stage all changes before committing
	if err := stageAllChanges(verbose); err != nil {
		_ = switchBranch(currentBranch, verbose)
		return "", fmt.Errorf("failed to stage changes: %w", err)
	}

	if err := commitChanges(commitMessage, verbose); err != nil {
		_ = switchBranch(currentBranch, verbose)
		return "", fmt.Errorf("failed to commit changes: %w", err)
	}

	if err := pushBranch(branchName, verbose); err != nil {
		_ = switchBranch(currentBranch, verbose)
		return "", err
	}

	_, prURL, err := createPR(ctx, branchName, prTitle, prBody, verbose)
	if err != nil {
		_ = switchBranch(currentBranch, verbose)
		return "", fmt.Errorf("failed to create PR: %w", err)
	}

	if err := switchBranch(currentBranch, verbose); err != nil {
		return prURL, fmt.Errorf("failed to switch back to branch %s: %w", currentBranch, err)
	}

	fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Created PR: "+prURL))
	prHelpersLog.Printf("Created PR: %s", prURL)
	return prURL, nil
}
