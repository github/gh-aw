package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/repoutil"
	"github.com/github/gh-aw/pkg/syncutil"
	"github.com/github/gh-aw/pkg/workflow"
)

var repoLog = logger.New("cli:repo")
var localRepoSlugPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

// Global cache for current repository info
var currentRepoSlugCache syncutil.OnceLoader[string]

// getCurrentRepoSlugUncached preserves gh's canonical, selected repository.
// Local origin parsing is a fallback when gh cannot resolve a repository.
func getCurrentRepoSlugUncached() (string, error) {
	return getCurrentRepoSlugWithContext(context.Background())
}

func getCurrentRepoSlugWithContext(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	repoLog.Print("Fetching current repository slug")

	repoLog.Print("Attempting to get repository slug via gh CLI")
	output, err := workflow.RunGHContext(ctx, "Fetching repository info...", "repo", "view", "--json", "owner,name", "--jq", ".owner.login + \"/\" + .name")
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err == nil {
		repoSlug := strings.TrimSpace(string(output))
		if repoSlug != "" {
			// Validate format (should be owner/repo)
			if _, _, err := repoutil.SplitRepoSlug(repoSlug); err == nil {
				repoLog.Printf("Successfully got repository slug via gh CLI: %s", repoSlug)
				return repoSlug, nil
			}
		}
	}

	if err != nil {
		repoLog.Print("gh CLI lookup unavailable")
	}
	// An explicit selection must never silently fall back to another checkout.
	if os.Getenv("GH_REPO") != "" { //nolint:osgetenvlibrary // Preserve gh's explicit repository selection at the CLI boundary.
		if err != nil {
			return "", fmt.Errorf("failed to get current repository via gh CLI: %w", err)
		}
		return "", errors.New("failed to get current repository: gh CLI returned an invalid repository slug")
	}
	return getCurrentRepoSlugFromGitContext(ctx)
}

// getWorkQueueRepoSlug discovers only the queue target locally. Other commands
// retain gh's selected repository, including a configured parent of a fork.
func getWorkQueueRepoSlug() (string, error) {
	if os.Getenv("GH_REPO") == "" { //nolint:osgetenvlibrary // Preserve gh's explicit repository selection at the CLI boundary.
		if repo, err := getCurrentRepoSlugFromGit(); err == nil {
			return repo, nil
		}
	}
	return GetCurrentRepoSlug()
}

func getCurrentRepoSlugFromGit() (string, error) {
	return getCurrentRepoSlugFromGitContext(context.Background())
}

func getCurrentRepoSlugFromGitContext(ctx context.Context) (string, error) {
	gitCmd := exec.CommandContext(ctx, "git", "remote", "get-url", "origin")
	gitOutput, err := gitCmd.Output()
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err != nil {
		repoLog.Printf("Failed to get git remote URL: %v", err)
		return "", fmt.Errorf("failed to get local Git origin: %w", err)
	}

	remoteURL := strings.TrimSpace(string(gitOutput))
	// Delegate to the shared helper which supports both HTTPS and SSH formats,
	// including GitHub Enterprise hosts configured via getGitHubHost().
	repoPath := parseGitHubRepoSlugFromURL(remoteURL)
	if repoPath == "" {
		return "", errors.New("local origin does not identify a GitHub repository")
	}

	// Validate format (should be owner/repo)
	if !localRepoSlugPattern.MatchString(repoPath) {
		return "", errors.New("invalid repository format. Expected format: owner/repo. Example: github/gh-aw")
	}
	for part := range strings.SplitSeq(repoPath, "/") {
		if part == "." || part == ".." {
			return "", errors.New("invalid repository format. Expected format: owner/repo. Example: github/gh-aw")
		}
	}

	repoLog.Printf("Successfully parsed repository slug from git remote: %s", repoPath)
	return repoPath, nil
}

// GetCurrentRepoSlug gets the current repository slug with caching.
// This is the recommended function to use for repository access across the codebase.
func GetCurrentRepoSlug() (string, error) {
	result, err := currentRepoSlugCache.Get(getCurrentRepoSlugUncached)

	if err != nil {
		return "", err
	}

	repoLog.Printf("Using cached repository slug: %s", result)
	return result, nil
}
