//go:build integration

package workflow

import (
	"strings"
)

// getGitHubRepoURLIntegration converts a repo path to a GitHub URL
// For "actions/checkout" -> "https://github.com/actions/checkout.git"
// For "github/codeql-action/upload-sarif" -> "https://github.com/github/codeql-action.git"
func getGitHubRepoURLIntegration(repo string) string {
	// For actions with subpaths (like codeql-action/upload-sarif), extract the base repo
	parts := strings.Split(repo, "/")
	if len(parts) >= 2 {
		// Take first two parts (owner/repo)
		baseRepo := parts[0] + "/" + parts[1]
		return "https://github.com/" + baseRepo + ".git"
	}
	return "https://github.com/" + repo + ".git"
}
