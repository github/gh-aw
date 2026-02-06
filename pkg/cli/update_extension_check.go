package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/workflow"
)

var updateExtensionCheckLog = logger.New("cli:update_extension_check")

// checkExtensionUpdate checks if a newer version of gh-aw is available
func checkExtensionUpdate(verbose bool) error {
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatVerboseMessage("Checking for gh-aw extension updates..."))
	}

	// Run gh extension upgrade --dry-run to check for updates
	output, err := workflow.RunGHCombined("Checking for extension updates...", "extension", "upgrade", "github/gh-aw", "--dry-run")
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
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Run 'gh extension upgrade github/gh-aw' to update"))
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

// ensureLatestExtensionVersion checks if the current release matches the latest release
// and issues a warning if an update is needed. This function fails silently if the
// release URL is not available or blocked.
func ensureLatestExtensionVersion(verbose bool) error {
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatVerboseMessage("Checking for gh-aw extension updates..."))
	}

	// Get current version
	currentVersion := GetVersion()
	updateExtensionCheckLog.Printf("Current version: %s", currentVersion)

	// Skip check for non-release versions (dev builds)
	if !workflow.IsReleasedVersion(currentVersion) {
		updateExtensionCheckLog.Print("Not a released version, skipping update check")
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Skipping version check (development build)"))
		}
		return nil
	}

	// Query GitHub API for latest release
	latestVersion, err := getLatestReleaseVersion(verbose)
	if err != nil {
		// Fail silently - don't block upgrade if we can't check for updates
		updateExtensionCheckLog.Printf("Failed to check for updates (silently ignoring): %v", err)
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Could not check for updates: %v", err)))
		}
		return nil
	}

	if latestVersion == "" {
		updateExtensionCheckLog.Print("Could not determine latest version")
		return nil
	}

	updateExtensionCheckLog.Printf("Latest version: %s", latestVersion)

	// Normalize versions for comparison (remove 'v' prefix)
	currentVersionNormalized := strings.TrimPrefix(currentVersion, "v")
	latestVersionNormalized := strings.TrimPrefix(latestVersion, "v")

	// Compare versions
	if currentVersionNormalized == latestVersionNormalized {
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("✓ gh-aw extension is up to date"))
		}
		updateExtensionCheckLog.Print("Extension is up to date")
		return nil
	}

	// Check if we're on a newer version (development/prerelease)
	if currentVersionNormalized > latestVersionNormalized {
		updateExtensionCheckLog.Printf("Current version (%s) appears newer than latest release (%s)", currentVersion, latestVersion)
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Running a development or pre-release version"))
		}
		return nil
	}

	// A newer version is available - display warning message (not error)
	updateExtensionCheckLog.Printf("Newer version available: %s (current: %s)", latestVersion, currentVersion)
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("A newer version of gh-aw is available: %s (current: %s)", latestVersion, currentVersion)))
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Consider upgrading with: gh extension upgrade github/gh-aw"))
	fmt.Fprintln(os.Stderr, "")

	return nil
}

// getLatestReleaseVersion queries GitHub API for the latest release version of gh-aw
func getLatestReleaseVersion(verbose bool) (string, error) {
	updateExtensionCheckLog.Print("Querying GitHub API for latest release...")

	// Create GitHub REST client using go-gh
	client, err := api.NewRESTClient(api.ClientOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to create GitHub client: %w", err)
	}

	// Query the latest release
	var release Release
	err = client.Get("repos/github/gh-aw/releases/latest", &release)
	if err != nil {
		return "", fmt.Errorf("failed to query latest release: %w", err)
	}

	updateExtensionCheckLog.Printf("Latest release: %s", release.TagName)
	return release.TagName, nil
}
