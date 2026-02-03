// Package repoutil provides utility functions for working with GitHub repository slugs and URLs.
package repoutil

import (
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var log = logger.New("repoutil:repoutil")

// SplitRepoSlug splits a repository slug (owner/repo) into owner and repo parts.
// Returns an error if the slug format is invalid.
func SplitRepoSlug(slug string) (owner, repo string, err error) {
	log.Printf("Splitting repo slug: %s", slug)
	parts := strings.Split(slug, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		log.Printf("Invalid repo slug format: %s", slug)
		return "", "", fmt.Errorf("invalid repo format: %s", slug)
	}
	log.Printf("Split result: owner=%s, repo=%s", parts[0], parts[1])
	return parts[0], parts[1], nil
}

// ParseGitHubURL extracts the owner and repo from a GitHub URL.
// Handles both SSH (git@github.com:owner/repo.git) and HTTPS (https://github.com/owner/repo.git) formats.
func ParseGitHubURL(url string) (owner, repo string, err error) {
	log.Printf("Parsing GitHub URL: %s", url)
	var repoPath string

	// SSH format: git@github.com:owner/repo.git
	if strings.HasPrefix(url, "git@github.com:") {
		repoPath = strings.TrimPrefix(url, "git@github.com:")
		log.Printf("Detected SSH format, extracted path: %s", repoPath)
	} else if strings.Contains(url, "github.com/") {
		// HTTPS format: https://github.com/owner/repo.git
		parts := strings.Split(url, "github.com/")
		if len(parts) >= 2 {
			repoPath = parts[1]
			log.Printf("Detected HTTPS format, extracted path: %s", repoPath)
		}
	} else {
		log.Printf("URL does not match known GitHub formats: %s", url)
		return "", "", fmt.Errorf("URL does not appear to be a GitHub repository: %s", url)
	}

	// Remove .git suffix if present
	repoPath = strings.TrimSuffix(repoPath, ".git")

	// Split into owner/repo
	return SplitRepoSlug(repoPath)
}

// SanitizeForFilename converts a repository slug (owner/repo) to a filename-safe string.
// Replaces "/" with "-". Returns "clone-mode" if the slug is empty.
func SanitizeForFilename(slug string) string {
	if slug == "" {
		return "clone-mode"
	}
	return strings.ReplaceAll(slug, "/", "-")
}
