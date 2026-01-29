//go:build integration

package workflow

import (
	"os/exec"
	"strings"
	"testing"
)

// TestActionPinSHAsMatchVersionTags verifies that the SHAs in actionPins actually correspond to their version tags
// by querying the GitHub repositories using git ls-remote
func TestActionPinSHAsMatchVersionTags(t *testing.T) {
	actionPins := getActionPins()
	// Test all action pins in parallel for faster execution
	for _, pin := range actionPins {
		pin := pin // Capture for parallel execution
		t.Run(pin.Repo, func(t *testing.T) {
			t.Parallel() // Run subtests in parallel

			// Extract the repository URL from the repo field
			// For actions like "actions/checkout", the URL is https://github.com/actions/checkout.git
			// For actions like "github/codeql-action/upload-sarif", we need the base repo
			repoURL := getGitHubRepoURLIntegration(pin.Repo)

			// Use git ls-remote to get the SHA for the version tag
			cmd := exec.Command("git", "ls-remote", repoURL, "refs/tags/"+pin.Version)
			output, err := cmd.Output()
			if err != nil {
				t.Logf("Warning: Could not verify %s@%s - git ls-remote failed: %v", pin.Repo, pin.Version, err)
				t.Logf("This may be expected for actions that don't follow standard tagging or private repos")
				return // Skip verification but don't fail the test
			}

			outputStr := strings.TrimSpace(string(output))
			if outputStr == "" {
				t.Logf("Warning: No tag found for %s@%s", pin.Repo, pin.Version)
				return // Skip verification but don't fail the test
			}

			// Extract SHA from git ls-remote output (format: "SHA\trefs/tags/version")
			parts := strings.Fields(outputStr)
			if len(parts) < 1 {
				t.Errorf("Unexpected git ls-remote output format for %s@%s: %s", pin.Repo, pin.Version, outputStr)
				return
			}

			actualSHA := parts[0]

			// Verify the SHA matches
			if actualSHA != pin.SHA {
				t.Errorf("SHA mismatch for %s@%s:\n  Expected: %s\n  Got:      %s",
					pin.Repo, pin.Version, pin.SHA, actualSHA)
				t.Logf("To fix, update the SHA in action_pins.go to: %s", actualSHA)
			}
		})
	}
}

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
