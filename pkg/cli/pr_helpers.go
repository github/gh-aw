package cli

import (
	"context"
	"errors"
	"fmt"
	"math/rand"

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

func requireAutoMergePR(autoMerge, createPR bool) error {
	if autoMerge && !createPR {
		return errors.New("--auto-merge requires --create-pull-request")
	}
	return nil
}

var switchPRBranch = switchBranch

func createPRWithChanges(ctx context.Context, branchPrefix, commitMessage, prTitle, prBody string, verbose, autoMerge bool) (string, error) {
	if err := validatePRBody(prBody); err != nil {
		return "", err
	}
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
		_ = switchPRBranch(currentBranch, verbose)
		return "", fmt.Errorf("failed to stage changes: %w", err)
	}

	if err := commitChanges(commitMessage, verbose); err != nil {
		_ = switchPRBranch(currentBranch, verbose)
		return "", fmt.Errorf("failed to commit changes: %w", err)
	}

	if err := pushBranch(branchName, verbose); err != nil {
		_ = switchPRBranch(currentBranch, verbose)
		return "", err
	}

	_, prURL, err := createPR(ctx, branchName, prTitle, prBody, verbose)
	if err != nil {
		_ = switchPRBranch(currentBranch, verbose)
		return "", fmt.Errorf("failed to create PR: %w", err)
	}

	var autoMergeErr error
	if autoMerge {
		autoMergeErr = enablePRAutoMerge(ctx, prURL)
	}
	if err := switchPRBranch(currentBranch, verbose); err != nil {
		return prURL, errors.Join(autoMergeErr, fmt.Errorf("failed to switch back to branch %s: %w", currentBranch, err))
	}
	if autoMergeErr != nil {
		return prURL, autoMergeErr
	}

	prHelpersLog.Printf("Created PR: %s", prURL)
	return prURL, nil
}
