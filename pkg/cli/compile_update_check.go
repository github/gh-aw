package cli

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/semverutil"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/tty"
	"github.com/github/gh-aw/pkg/workflow"
)

var compileUpdateCheckLog = logger.New("cli:compile_update_check")

const (
	compileUpdateCheckDisableEnv = "GH_AW_DISABLE_UPDATE_CHECK"
	compileUpdateCheckFileName   = "gh-aw-last-compile-update-check"
	compileUpdateCheckInterval   = 24 * time.Hour
	compileUpdateCheckTimeout    = 3 * time.Second
	compileUpdateCheckNoWait     = 0
)

var (
	compileUpdateCheckLatestReleaseURL = "https://github.com/github/gh-aw/releases/latest"
	compileUpdateCheckProbeURLFunc     = func(tag string) string {
		return fmt.Sprintf("https://raw.githubusercontent.com/github/gh-aw/refs/tags/%s/go.mod", tag)
	}
	compileUpdateCheckHTTPClientFactory = func() *http.Client {
		return &http.Client{Timeout: compileUpdateCheckTimeout}
	}
	compileUpdateCheckIsTerminalFunc  = tty.IsStderrTerminal
	getCompileUpdateCheckFilePathFunc = getCompileUpdateCheckFilePathImpl
)

type compileUpdateNotificationKind string

const (
	compileUpdateNotificationMinorBehind compileUpdateNotificationKind = "minor_behind"
	compileUpdateNotificationRemovedTag  compileUpdateNotificationKind = "removed_tag"
)

type compileUpdateNotification struct {
	Kind           compileUpdateNotificationKind
	CurrentVersion string
	LatestVersion  string
}

// StartCompileUpdateCheck begins a best-effort update check for the compile command.
// The returned function should be called once before the command exits to print any
// ready notification without blocking compilation for long.
func StartCompileUpdateCheck(ctx context.Context, noCheckUpdate bool, verbose bool) func() {
	if !shouldRunCompileUpdateCheck(noCheckUpdate) {
		return func() {}
	}
	ctx, cancel := context.WithTimeout(ctx, compileUpdateCheckTimeout)
	updateCompileUpdateCheckTime()

	results := make(chan *compileUpdateNotification, 1) // buffered channel closed by sender goroutine via defer

	go func() {
		defer close(results)
		defer cancel()
		defer func() {
			if r := recover(); r != nil {
				compileUpdateCheckLog.Printf("Panic in compile update check (recovered): %v", r)
			}
		}()

		if ctx.Err() != nil {
			compileUpdateCheckLog.Printf("Compile update check cancelled before starting: %v", ctx.Err())
			return
		}

		result, err := runCompileUpdateCheck(ctx, compileUpdateCheckHTTPClientFactory())
		if err != nil {
			compileUpdateCheckLog.Printf("Compile update check failed (ignoring): %v", err)
			return
		}
		if result == nil {
			if verbose {
				compileUpdateCheckLog.Print("No compile update notification needed")
			}
			return
		}

		select {
		case results <- result:
		case <-ctx.Done():
		}
	}()

	return func() {
		defer cancel()
		result := waitForCompileUpdateNotification(ctx, results, compileUpdateCheckNoWait)
		if result != nil {
			printCompileUpdateNotification(result)
		}
	}
}

func shouldRunCompileUpdateCheck(noCheckUpdate bool) bool {
	if noCheckUpdate {
		compileUpdateCheckLog.Print("Update check disabled via --no-check-update flag")
		return false
	}
	if os.Getenv(compileUpdateCheckDisableEnv) != "" { //nolint:osgetenvlibrary
		compileUpdateCheckLog.Printf("Update check disabled via %s", compileUpdateCheckDisableEnv)
		return false
	}
	if IsRunningInCI() {
		compileUpdateCheckLog.Print("Update check disabled in CI environment")
		return false
	}
	if isRunningAsMCPServer() {
		compileUpdateCheckLog.Print("Update check disabled in MCP server mode")
		return false
	}
	if !compileUpdateCheckIsTerminalFunc() {
		compileUpdateCheckLog.Print("Update check disabled when stderr is not a terminal")
		return false
	}

	lastCheckFile := getCompileUpdateCheckFilePath()
	return shouldRunUpdateCheckAtPath(lastCheckFile, compileUpdateCheckInterval, "compile update check", compileUpdateCheckLog)
}

func waitForCompileUpdateNotification(ctx context.Context, results <-chan *compileUpdateNotification, timeout time.Duration) *compileUpdateNotification {
	if results == nil {
		return nil
	}

	if timeout <= 0 {
		select {
		case result, ok := <-results:
			if !ok {
				return nil
			}
			return result
		default:
			return nil
		}
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case result, ok := <-results:
		if !ok {
			return nil
		}
		return result
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return nil
	}
}

func runCompileUpdateCheck(ctx context.Context, client *http.Client) (*compileUpdateNotification, error) {
	currentVersion := GetVersion()
	if !workflow.IsReleasedVersion(currentVersion) {
		compileUpdateCheckLog.Print("Not a released version, skipping update check")
		return nil, nil
	}

	latestVersion, err := fetchLatestReleaseTag(ctx, client)
	if err != nil {
		return nil, err
	}
	if latestVersion == "" {
		return nil, nil
	}

	latestTagExists, err := downloadReleaseProbeFile(ctx, client, latestVersion)
	if err != nil {
		return nil, err
	}
	if !latestTagExists {
		compileUpdateCheckLog.Printf("Latest release tag %s did not expose the probe file; skipping", latestVersion)
		return nil, nil
	}

	currentTagExists := true
	if currentVersion != latestVersion {
		currentTagExists, err = downloadReleaseProbeFile(ctx, client, currentVersion)
		if err != nil {
			return nil, err
		}
	}
	if !currentTagExists {
		return &compileUpdateNotification{
			Kind:           compileUpdateNotificationRemovedTag,
			CurrentVersion: currentVersion,
			LatestVersion:  latestVersion,
		}, nil
	}

	if !isMinorVersionBehind(currentVersion, latestVersion) {
		return nil, nil
	}

	return &compileUpdateNotification{
		Kind:           compileUpdateNotificationMinorBehind,
		CurrentVersion: currentVersion,
		LatestVersion:  latestVersion,
	}, nil
}

func fetchLatestReleaseTag(ctx context.Context, client *http.Client) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, compileUpdateCheckLatestReleaseURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create latest release request: %w", err)
	}
	req.Header.Set("User-Agent", "gh-aw/"+GetVersion())

	// The redirect already identifies the tag; fetching its release page adds a round trip.
	lookupClient := *client
	lookupClient.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	resp, err := lookupClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to query latest release: %w", err)
	}
	defer resp.Body.Close()

	finalURL := resp.Request.URL
	switch resp.StatusCode {
	case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther, http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
		finalURL, err = resp.Location()
		if err != nil {
			return "", fmt.Errorf("invalid latest release redirect: %w", err)
		}
	default:
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return "", fmt.Errorf("latest release request returned status %d", resp.StatusCode)
		}
	}

	finalPath := finalURL.Path
	if !strings.Contains(finalPath, "/releases/tag/") {
		return "", fmt.Errorf("unexpected latest release path %q", finalPath)
	}

	tag := path.Base(finalPath)
	if tag == "" || tag == "." || tag == "latest" {
		return "", fmt.Errorf("could not determine latest release tag from %q", finalPath)
	}

	return tag, nil
}

func downloadReleaseProbeFile(ctx context.Context, client *http.Client, tag string) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, compileUpdateCheckProbeURLFunc(tag), nil)
	if err != nil {
		return false, fmt.Errorf("failed to create probe request for %s: %w", tag, err)
	}
	req.Header.Set("User-Agent", "gh-aw/"+GetVersion())

	resp, err := client.Do(req)
	if err != nil {
		return false, fmt.Errorf("failed to download probe file for %s: %w", tag, err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		return true, nil
	case http.StatusNotFound:
		return false, nil
	default:
		return false, fmt.Errorf("probe download for %s returned status %d", tag, resp.StatusCode)
	}
}

func getCompileUpdateCheckFilePath() string {
	return getCompileUpdateCheckFilePathFunc()
}

func getCompileUpdateCheckFilePathImpl() string {
	return getUpdateCheckFilePathFor(compileUpdateCheckFileName, compileUpdateCheckLog)
}

func updateCompileUpdateCheckTime() {
	writeUpdateCheckTime(getCompileUpdateCheckFilePath(), constants.FilePermSensitive, "compile update check", compileUpdateCheckLog)
}

func isMinorVersionBehind(currentVersion string, latestVersion string) bool {
	currentSV := semverutil.EnsureVPrefix(currentVersion)
	latestSV := semverutil.EnsureVPrefix(latestVersion)

	if !semverutil.IsValid(currentSV) || !semverutil.IsValid(latestSV) {
		return false
	}
	if semverutil.Compare(currentSV, latestSV) >= 0 {
		return false
	}
	if !hasExplicitMinorComponent(currentSV) || !hasExplicitMinorComponent(latestSV) {
		return false
	}

	currentParsed := semverutil.ParseVersion(currentSV)
	latestParsed := semverutil.ParseVersion(latestSV)

	return currentParsed.Major == latestParsed.Major && latestParsed.Minor > currentParsed.Minor
}

func hasExplicitMinorComponent(version string) bool {
	core := strings.TrimPrefix(version, "v")
	if idx := strings.IndexAny(core, "-+"); idx >= 0 {
		core = core[:idx]
	}
	return strings.Contains(core, ".")
}

func printCompileUpdateNotification(notification *compileUpdateNotification) {
	if notification == nil {
		return
	}

	fmt.Fprintln(os.Stderr)

	switch notification.Kind {
	case compileUpdateNotificationRemovedTag:
		fmt.Fprintln(os.Stderr, console.FormatWarningMessageStderr(fmt.Sprintf(
			"The installed gh-aw compiler version %s is no longer available as a repository tag.", notification.CurrentVersion,
		)))
		fmt.Fprintln(os.Stderr, console.FormatWarningMessageStderr(fmt.Sprintf(
			"Update the compiler before recompiling workflows (latest release: %s).", notification.LatestVersion,
		)))
	default:
		fmt.Fprintln(os.Stderr, console.FormatInfoMessageStderr(fmt.Sprintf(
			"Compiler upgrade recommended: gh-aw %s is behind the latest release %s.", notification.CurrentVersion, notification.LatestVersion,
		)))
		fmt.Fprintln(os.Stderr, console.FormatInfoMessageStderr("Hint: upgrade the compiler with: gh extension upgrade github/gh-aw"))
	}

	fmt.Fprintln(os.Stderr)
}
