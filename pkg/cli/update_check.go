package cli

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/githubapi"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/semverutil"
	"github.com/github/gh-aw/pkg/workflow"
)

var updateCheckLog = logger.New("cli:update_check")

const (
	// lastCheckFileName is the name of the file that tracks the last update check timestamp
	lastCheckFileName = "gh-aw-last-update-check"
	// checkInterval is how often we check for updates (24 hours)
	checkInterval = 24 * time.Hour
	// maxReleasesToQuery is the maximum number of releases queried when prereleases are included.
	maxReleasesToQuery = 50
)

// Release represents a GitHub release
type Release struct {
	TagName    string `json:"tag_name"`
	Name       string `json:"name"`
	HTMLURL    string `json:"html_url"`
	Prerelease bool   `json:"prerelease"`
	Draft      bool   `json:"draft"`
}

// shouldCheckForUpdate determines if we should check for updates based on:
// - CI mode (disabled)
// - MCP server mode (disabled via parent command detection)
// - Time since last check (once per day)
// - --no-check-update flag
func shouldCheckForUpdate(noCheckUpdate bool) bool {
	// Skip if explicitly disabled
	if noCheckUpdate {
		updateCheckLog.Print("Update check disabled via --no-check-update flag")
		return false
	}

	// Skip in CI environments
	if IsRunningInCI() {
		updateCheckLog.Print("Update check disabled in CI environment")
		return false
	}

	// Skip if running as MCP server (detected by checking if parent command is "mcp-server")
	// When gh aw is invoked from MCP server, it's spawned as a subprocess
	if isRunningAsMCPServer() {
		updateCheckLog.Print("Update check disabled in MCP server mode")
		return false
	}

	// Check if we've already checked recently
	lastCheckFile := getLastCheckFilePath()
	if !shouldRunUpdateCheckAtPath(lastCheckFile, checkInterval, "update check", updateCheckLog) {
		return false
	}

	updateCheckLog.Print("Last check was more than 24 hours ago, performing check")
	return true
}

// isRunningAsMCPServer detects if we're running as a subprocess of mcp-server
// This is a heuristic - we can't reliably detect this, so we're conservative
func isRunningAsMCPServer() bool {
	// Check for MCP_SERVER environment variable that could be set by the MCP server
	return os.Getenv("GH_AW_MCP_SERVER") != "" //nolint:osgetenvlibrary
}

var (
	// getLastCheckFilePathFunc allows overriding in tests
	getLastCheckFilePathFunc = getLastCheckFilePathImpl
	// checkForUpdatesWithContextFunc allows overriding in tests
	checkForUpdatesWithContextFunc = checkForUpdatesWithContext
)

// getLastCheckFilePath returns the path to the last check timestamp file
func getLastCheckFilePath() string {
	return getLastCheckFilePathFunc()
}

// getLastCheckFilePathImpl is the actual implementation
func getLastCheckFilePathImpl() string {
	return getUpdateCheckFilePathFor(lastCheckFileName, updateCheckLog)
}

// updateLastCheckTime updates the timestamp of the last update check
func updateLastCheckTime() {
	writeUpdateCheckTime(getLastCheckFilePath(), constants.FilePermPublic, "update check", updateCheckLog)
}

func checkForUpdatesWithContext(ctx context.Context, noCheckUpdate bool, verbose bool) {
	// Quick check if we should even attempt the update check
	if !shouldCheckForUpdate(noCheckUpdate) {
		return
	}

	updateCheckLog.Print("Checking for gh-aw updates...")

	// Update the last check time immediately to prevent concurrent checks
	updateLastCheckTime()

	// Get current version
	currentVersion := GetVersion()
	if !workflow.IsReleasedVersion(currentVersion) {
		updateCheckLog.Print("Not a released version, skipping update check")
		return
	}

	// Query GitHub API for latest release
	latestVersion, err := getLatestRelease(ctx, false)
	if err != nil {
		// Silently ignore errors - update check should never fail the command
		updateCheckLog.Printf("Error checking for updates (ignoring): %v", err)
		return
	}

	if latestVersion == "" {
		updateCheckLog.Print("Could not determine latest version")
		return
	}

	if latestVersion == currentVersion {
		if verbose {
			updateCheckLog.Print("gh-aw is up to date")
		}
		return
	}

	currentVersionNormalized := strings.TrimPrefix(currentVersion, "v")
	latestVersionNormalized := strings.TrimPrefix(latestVersion, "v")
	if currentVersionNormalized == latestVersionNormalized {
		if verbose {
			updateCheckLog.Print("gh-aw is up to date (version format differs)")
		}
		return
	}

	if isCurrentVersionAtLeastLatest(currentVersion, latestVersion) {
		updateCheckLog.Printf("Current version (%s) appears newer than latest release (%s), skipping notification", currentVersion, latestVersion)
		return
	}

	// A newer version is available - display update message
	updateCheckLog.Printf("Newer version available: %s (current: %s)", latestVersion, currentVersion)
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("A new version of gh-aw is available: %s (current: %s)", renderReleaseVersion(latestVersion), renderReleaseVersion(currentVersion))))
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Update with: gh extension upgrade github/gh-aw"))
	fmt.Fprintln(os.Stderr, "")
}

func isCurrentVersionAtLeastLatest(currentVersion, latestVersion string) bool {
	if latestVersion == currentVersion {
		return true
	}

	currentVersionNormalized := strings.TrimPrefix(currentVersion, "v")
	latestVersionNormalized := strings.TrimPrefix(latestVersion, "v")

	if currentVersionNormalized == latestVersionNormalized {
		return true
	}

	currentSV := semverutil.EnsureVPrefix(currentVersion)
	latestSV := semverutil.EnsureVPrefix(latestVersion)
	if semverutil.IsValid(currentSV) && semverutil.IsValid(latestSV) {
		return semverutil.Compare(currentSV, latestSV) >= 0
	}

	return currentVersionNormalized > latestVersionNormalized
}

// getLatestRelease queries GitHub API for the latest release of gh-aw
func getLatestRelease(ctx context.Context, includePrereleases bool) (string, error) {
	updateCheckLog.Print("Querying GitHub API for latest release...")

	// Always target github.com explicitly: gh-aw is only published to github.com,
	// and users in mixed-host environments (e.g. a GHE active auth host) must
	// still reach the canonical registry to get the correct release metadata.
	client, err := api.NewRESTClient(gitHubDotComRESTClientOptions())
	if err != nil {
		return "", fmt.Errorf("failed to create GitHub client: %w", err)
	}
	return getLatestReleaseWithClient(ctx, client, includePrereleases)
}

type releaseRESTClient interface {
	DoWithContext(ctx context.Context, method string, path string, body io.Reader, response any) error
}

func getLatestReleaseWithClient(ctx context.Context, client releaseRESTClient, includePrereleases bool) (string, error) {
	if includePrereleases {
		var releases []Release
		err := client.DoWithContext(ctx, http.MethodGet, fmt.Sprintf("repos/github/gh-aw/releases?per_page=%d", maxReleasesToQuery), nil, &releases)
		if err != nil {
			return "", fmt.Errorf("failed to query releases: %w", err)
		}

		tag := findLatestPublishedReleaseTag(releases)
		updateCheckLog.Printf("Latest published release (pre-releases allowed): %s", tag)
		return tag, nil
	}

	// Query the latest stable release
	var release Release
	err := client.DoWithContext(ctx, http.MethodGet, "repos/github/gh-aw/releases/latest", nil, &release)
	if err != nil {
		return "", fmt.Errorf("failed to query latest release: %w", err)
	}

	updateCheckLog.Printf("Latest release: %s (prerelease: %v)", release.TagName, release.Prerelease)

	// /releases/latest already excludes prereleases per the GitHub API contract,
	// but guard defensively in case the response ever changes.
	if release.Prerelease {
		return "", nil
	}

	return release.TagName, nil
}

func gitHubDotComRESTClientOptions() api.ClientOptions {
	return githubapi.ClientOptions("github.com", "")
}

// findLatestPublishedReleaseTag returns the first non-draft release tag from the
// releases API response, skipping entries without tag names.
func findLatestPublishedReleaseTag(releases []Release) string {
	for _, release := range releases {
		if release.Draft || release.TagName == "" {
			continue
		}
		return release.TagName
	}
	return ""
}

// CheckForUpdatesAsync performs update check in background (best effort)
// This is called from compile command and should never block or fail the compilation
// The context can be used to cancel the update check if the program is shutting down.
// The returned function joins the goroutine; call it before the program exits to ensure
// the update check completes and the goroutine is properly cleaned up.
func CheckForUpdatesAsync(ctx context.Context, noCheckUpdate bool, verbose bool) func() {
	done := make(chan struct{})
	checkCtx, cancelCheck := context.WithCancel(ctx)

	// Run check in goroutine to avoid blocking compilation
	go func() {
		defer close(done)
		// Recover from any panics in the update check
		defer func() {
			if r := recover(); r != nil {
				updateCheckLog.Printf("Panic in update check (recovered): %v", r)
			}
		}()

		// Check if context was cancelled before starting
		if checkCtx.Err() != nil {
			updateCheckLog.Printf("Update check cancelled before starting: %v", checkCtx.Err())
			return
		}

		checkForUpdatesWithContextFunc(checkCtx, noCheckUpdate, verbose)
	}()

	// Give the goroutine a small window to complete quickly
	// This allows the message to appear before compilation starts
	// but doesn't block if the check takes longer
	timer := time.NewTimer(100 * time.Millisecond)
	defer timer.Stop()

	select {
	case <-done:
		// Goroutine finished within the window
	case <-timer.C:
		// Continue after timeout
	case <-ctx.Done():
		// Context cancelled during wait
	}

	return func() {
		cancelCheck()
		<-done
	}
}
