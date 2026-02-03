package cli

import (
	"os"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var githubLog = logger.New("cli:github")

// getGitHubHost returns the GitHub host URL from environment variables.
// It checks GITHUB_SERVER_URL first (GitHub Actions standard),
// then falls back to GH_HOST (gh CLI standard),
// and finally defaults to https://github.com
func getGitHubHost() string {
	host := os.Getenv("GITHUB_SERVER_URL")
	if host == "" {
		host = os.Getenv("GH_HOST")
	}
	if host == "" {
		host = "https://github.com"
		githubLog.Print("Using default GitHub host: https://github.com")
	} else {
		githubLog.Printf("Resolved GitHub host: %s", host)
	}

	// Remove trailing slash for consistency
	return strings.TrimSuffix(host, "/")
}
