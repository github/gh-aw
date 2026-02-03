package parser

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var githubLog = logger.New("parser:github")

// GetGitHubToken attempts to get GitHub token from environment or gh CLI
func GetGitHubToken() (string, error) {
	githubLog.Print("Getting GitHub token")

	// First try environment variable
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		githubLog.Print("Found GITHUB_TOKEN environment variable")
		return token, nil
	}
	if token := os.Getenv("GH_TOKEN"); token != "" {
		githubLog.Print("Found GH_TOKEN environment variable")
		return token, nil
	}

	// Fall back to gh auth token command
	githubLog.Print("Attempting to get token from gh auth token command")
	cmd := exec.Command("gh", "auth", "token")
	// Note: gh auth token should respect GH_HOST environment variable for enterprise
	output, err := cmd.Output()
	if err != nil {
		githubLog.Printf("Failed to get token from gh auth token: %v", err)
		return "", fmt.Errorf("GITHUB_TOKEN environment variable not set and 'gh auth token' failed: %w", err)
	}

	token := strings.TrimSpace(string(output))
	if token == "" {
		githubLog.Print("gh auth token returned empty token")
		return "", fmt.Errorf("GITHUB_TOKEN environment variable not set and 'gh auth token' returned empty token")
	}

	githubLog.Print("Successfully retrieved token from gh auth token")
	return token, nil
}
