package workflow

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var gitRepositoryIDLog = logger.New("workflow:git_repository_id")

// getStableRepositoryIdentifier returns a stable identifier for the git repository
// that doesn't change when git remote configuration changes.
//
// It tries the following approaches in order:
// 1. Use repository slug from git remote (for shallow clones, since initial commit is unstable)
// 2. Use the initial commit SHA (for full clones, most stable option)
// 3. Use a hash of the repository directory path (fallback if no commits exist or shallow clone without remote)
//
// Returns a string that can be used as a stable repository identifier.
func getStableRepositoryIdentifier(gitRoot string, repositorySlug string) string {
	gitRepositoryIDLog.Printf("Getting stable repository identifier for git root: %s", gitRoot)

	// Check if this is a shallow clone
	isShallow, err := isShallowClone(gitRoot)
	if err != nil {
		gitRepositoryIDLog.Printf("Failed to check if shallow clone: %v", err)
		// Continue with full clone logic
		isShallow = false
	}

	if isShallow {
		gitRepositoryIDLog.Print("Repository is a shallow clone")
		// For shallow clones, prefer repository slug if available
		// since the initial commit changes with clone depth
		if repositorySlug != "" {
			gitRepositoryIDLog.Printf("Using repository slug for shallow clone: %s", repositorySlug)
			return repositorySlug
		}
		// No repository slug for shallow clone - fall back to directory hash
		gitRepositoryIDLog.Print("No repository slug available for shallow clone, using directory hash")
		hash := sha256.Sum256([]byte(gitRoot))
		shortHash := hex.EncodeToString(hash[:])[:12]
		identifier := "git-" + shortHash
		gitRepositoryIDLog.Printf("Using directory hash as repository identifier: %s", identifier)
		return identifier
	}

	// For full clones, try to get the initial commit SHA (most stable option)
	initialCommit, err := getInitialCommitSHA(gitRoot)
	if err == nil && initialCommit != "" {
		// Use first 12 characters of the initial commit SHA
		shortSHA := initialCommit
		if len(shortSHA) > 12 {
			shortSHA = shortSHA[:12]
		}
		identifier := "git-" + shortSHA
		gitRepositoryIDLog.Printf("Using initial commit SHA as repository identifier: %s", identifier)
		return identifier
	}

	gitRepositoryIDLog.Printf("Could not get initial commit SHA: %v, falling back", err)

	// Fallback: Use repository slug if available
	if repositorySlug != "" {
		gitRepositoryIDLog.Printf("Using repository slug as fallback: %s", repositorySlug)
		return repositorySlug
	}

	// Final fallback: Use a hash of the git root directory path
	// This is less stable (changes if directory is moved) but works for repos without commits
	hash := sha256.Sum256([]byte(gitRoot))
	shortHash := hex.EncodeToString(hash[:])[:12]
	identifier := "git-" + shortHash
	gitRepositoryIDLog.Printf("Using directory hash as repository identifier: %s", identifier)
	return identifier
}

// getInitialCommitSHA returns the SHA of the first commit in the repository
// This is stable and never changes for a given repository
//
// For shallow clones, this returns an error since the initial commit is not available
// and would change if the repository is re-cloned with a different depth.
func getInitialCommitSHA(gitRoot string) (string, error) {
	gitRepositoryIDLog.Printf("Getting initial commit SHA for git root: %s", gitRoot)

	// Check if this is a shallow clone - shallow clones don't have stable initial commits
	// because the initial commit changes based on clone depth
	isShallow, err := isShallowClone(gitRoot)
	if err != nil {
		gitRepositoryIDLog.Printf("Failed to check if shallow clone: %v", err)
		// Continue anyway - we'll try to get the initial commit
	} else if isShallow {
		gitRepositoryIDLog.Print("Repository is a shallow clone - initial commit is not stable")
		return "", fmt.Errorf("repository is a shallow clone - initial commit is not stable across different clone depths")
	}

	// Use git rev-list to get the initial commit (the one with no parents)
	cmd := exec.Command("git", "-C", gitRoot, "rev-list", "--max-parents=0", "HEAD")
	output, err := cmd.Output()
	if err != nil {
		gitRepositoryIDLog.Printf("Failed to get initial commit: %v", err)
		return "", fmt.Errorf("failed to get initial commit: %w", err)
	}

	commitSHA := strings.TrimSpace(string(output))
	if commitSHA == "" {
		return "", fmt.Errorf("no initial commit found")
	}

	// If there are multiple root commits (rare), take the first one
	commits := strings.Split(commitSHA, "\n")
	commitSHA = commits[0]

	gitRepositoryIDLog.Printf("Initial commit SHA: %s", commitSHA)
	return commitSHA, nil
}

// isShallowClone checks if the git repository is a shallow clone
func isShallowClone(gitRoot string) (bool, error) {
	// Check for the existence of .git/shallow file
	// This file exists in shallow clones and contains the list of shallow commit SHAs
	shallowFile := gitRoot + "/.git/shallow"
	_, err := os.Stat(shallowFile)
	if err != nil {
		if os.IsNotExist(err) {
			// No shallow file means this is a full clone
			return false, nil
		}
		// Some other error occurred
		return false, err
	}
	// Shallow file exists, this is a shallow clone
	return true, nil
}
