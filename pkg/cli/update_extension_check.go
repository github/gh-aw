package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/workflow"
)

// checkExtensionUpdate checks if a newer version of gh-aw is available
func checkExtensionUpdate(verbose bool) error {
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatVerboseMessage("Checking for gh-aw extension updates..."))
	}

	// Run gh extension upgrade --dry-run to check for updates
	output, err := workflow.RunGHCombined("Checking for extension updates...", "extension", "upgrade", "githubnext/gh-aw", "--dry-run")
	if err != nil {
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to check for extension updates: %v", err)))
		}
		return nil // Don't fail the whole command if update check fails
	}

	outputStr := strings.TrimSpace(string(output))
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatVerboseMessage(fmt.Sprintf("Extension update check output: %s", outputStr)))
	}

	// Parse the output to see if an update is available
	// Expected format: "[agentics]: would have upgraded from v0.14.0 to v0.18.1"
	lines := strings.Split(outputStr, "\n")
	for _, line := range lines {
		if strings.Contains(line, "[agentics]: would have upgraded from") {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(line))
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Run 'gh extension upgrade githubnext/gh-aw' to update"))
			return nil
		}
	}

	if strings.Contains(outputStr, "✓ Successfully checked extension upgrades") {
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("gh-aw extension is up to date"))
		}
	}

	return nil
}

// isAuthenticationError checks if an error message indicates an authentication issue
func isAuthenticationError(output string) bool {
	lowerOutput := strings.ToLower(output)
	return strings.Contains(lowerOutput, "authentication required") ||
		strings.Contains(lowerOutput, "gh_token") ||
		strings.Contains(lowerOutput, "github_token") ||
		strings.Contains(output, "set the GH_TOKEN environment variable") ||
		strings.Contains(lowerOutput, "permission") ||
		strings.Contains(lowerOutput, "not authenticated") ||
		strings.Contains(lowerOutput, "invalid token")
}

// ensureLatestExtensionVersion checks if a newer version of gh-aw is available
// and returns an error if an update is needed. This is used by the upgrade command
// to ensure users are on the latest version before upgrading workflows.
func ensureLatestExtensionVersion(verbose bool) error {
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatVerboseMessage("Checking for gh-aw extension updates..."))
	}

	// Run gh extension upgrade --dry-run to check for updates
	output, err := workflow.RunGHCombined("Checking for extension updates...", "extension", "upgrade", "githubnext/gh-aw", "--dry-run")
	outputStr := strings.TrimSpace(string(output))

	// Check for authentication errors (missing or invalid token)
	if err != nil || isAuthenticationError(outputStr) {
		if verbose {
			if err != nil {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to check for extension updates: %v", err)))
			} else {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Authentication required to check for updates"))
			}
		}
		// If we can't check for updates due to auth issues, allow the upgrade to proceed
		return nil
	}

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatVerboseMessage(fmt.Sprintf("Extension update check output: %s", outputStr)))
	}

	// Parse the output to see if an update is available
	// Expected format: "[agentics]: would have upgraded from v0.14.0 to v0.18.1"
	lines := strings.Split(outputStr, "\n")
	for _, line := range lines {
		if strings.Contains(line, "[agentics]: would have upgraded from") {
			fmt.Fprintln(os.Stderr, "")
			fmt.Fprintln(os.Stderr, console.FormatErrorMessage("gh-aw extension is not on the latest version"))
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(line))
			fmt.Fprintln(os.Stderr, "")
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Please upgrade the gh extension first:"))
			fmt.Fprintln(os.Stderr, console.FormatCommandMessage("  gh extension upgrade githubnext/gh-aw"))
			fmt.Fprintln(os.Stderr, "")
			return fmt.Errorf("gh-aw extension must be upgraded before running this command")
		}
	}

	if strings.Contains(outputStr, "✓ Successfully checked extension upgrades") {
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("✓ gh-aw extension is up to date"))
		}
	}

	return nil
}
