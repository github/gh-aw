package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"golang.org/x/mod/semver"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/semverutil"
	"github.com/github/gh-aw/pkg/workflow"
)

var updateExtensionCheckLog = logger.New("cli:update_extension_check")

// maxBackupCleanupAttempts is the number of times cleanupStaleWindowsBackups
// retries removing a stale .bak file before giving up.
const maxBackupCleanupAttempts = 3

// backupCleanupRetryDelay is the pause between successive cleanup attempts.
// The delay allows transient locks (e.g. Windows Defender scanning) to clear.
const backupCleanupRetryDelay = 300 * time.Millisecond

// upgradeExtensionIfOutdated checks if a newer version of the gh-aw extension is available
// and, if so, upgrades it automatically.
//
// Returns:
//   - upgraded: true if an upgrade was performed.
//   - installPath: on Linux or Windows, the resolved path where the new binary
//     was installed (captured before any rename so the caller can relaunch the
//     new binary from the correct path; on Linux os.Executable() may return a
//     "(deleted)"-suffixed path after the rename). Empty string on other systems
//     or when the path cannot be determined.
//   - err: non-nil if the upgrade failed.
//
// When upgraded is true the CURRENTLY RUNNING PROCESS still has the old version
// baked in. The caller should re-launch the freshly-installed binary (at
// installPath) so that subsequent work (e.g. lock-file compilation) uses the
// correct new version string.
func upgradeExtensionIfOutdated(ctx context.Context, verbose bool, includePrereleases bool) (bool, string, error) {
	currentVersion := GetVersion()
	updateExtensionCheckLog.Printf("Checking if extension needs upgrade (current: %s)", currentVersion)

	// Skip for non-release versions (dev builds)
	if !workflow.IsReleasedVersion(currentVersion) {
		updateExtensionCheckLog.Print("Not a released version, skipping upgrade check")
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Skipping extension upgrade check (development build)"))
		}
		return false, "", nil
	}

	// Query GitHub API for latest release
	latestVersion, err := getLatestRelease(ctx, includePrereleases)
	if err != nil {
		// Fail silently - don't block the upgrade command if we can't reach GitHub
		updateExtensionCheckLog.Printf("Failed to check for latest release (silently ignoring): %v", err)
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Could not check for extension updates: %v", err)))
		}
		return false, "", nil
	}

	if latestVersion == "" {
		updateExtensionCheckLog.Print("Could not determine latest version, skipping upgrade")
		return false, "", nil
	}

	updateExtensionCheckLog.Printf("Latest version: %s", latestVersion)

	// Ensure both versions have the 'v' prefix required by the semver package.
	currentSV := "v" + strings.TrimPrefix(currentVersion, "v")
	latestSV := "v" + strings.TrimPrefix(latestVersion, "v")

	// Already on the latest (or newer) version – use proper semver comparison so
	// that e.g. "0.10.0" is correctly treated as newer than "0.9.0".
	if semver.IsValid(currentSV) && semver.IsValid(latestSV) {
		if semver.Compare(currentSV, latestSV) >= 0 {
			updateExtensionCheckLog.Print("Extension is already up to date")
			if notice := prereleaseChannelNotice(currentVersion, latestVersion, includePrereleases); len(notice) > 0 {
				for _, line := range notice {
					fmt.Fprintln(os.Stderr, console.FormatInfoMessage(line))
				}
			} else if verbose {
				fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("gh-aw extension is up to date"))
			}
			return false, "", nil
		}
	} else {
		// Versions are not valid semver; skip unreliable string comparison and
		// proceed with the upgrade to avoid incorrectly treating an outdated
		// version as up to date (lexicographic comparison breaks for e.g. "0.9.0" vs "0.10.0").
		updateExtensionCheckLog.Printf("Non-semver versions detected (current=%q, latest=%q); proceeding with upgrade", currentVersion, latestVersion)
	}

	// A newer version is available – upgrade automatically
	updateExtensionCheckLog.Printf("Upgrading extension from %s to %s", currentVersion, latestVersion)
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Upgrading gh-aw extension from %s to %s...", renderReleaseVersion(currentVersion), renderReleaseVersion(latestVersion))))

	// When targeting a prerelease version on platforms that do not lock running
	// binaries (i.e., not Linux or Windows), gh extension upgrade --force resolves
	// the upgrade target via /releases/latest which excludes prereleases.  On
	// those platforms the first attempt would silently install an older stable
	// release instead of the desired prerelease.  Skip directly to the pin-based
	// install to ensure the exact target version is installed.
	// Note: on Linux/Windows the binary is locked, so gh extension upgrade --force
	// fails with ETXTBSY/Access-denied and we fall through to the rename+retry path
	// which already uses pin-based install; the check below is only needed for
	// platforms (e.g. macOS) where the first attempt would "succeed" with the wrong
	// version.
	if includePrereleases && !needsRenameWorkaround() {
		updateExtensionCheckLog.Printf("Prerelease upgrade on macOS: skipping gh extension upgrade (uses /releases/latest, ignores prereleases), using pin-based install for %s", latestVersion)
		removeCmd := ghCmdForExtension("extension", "remove", extensionRepo)
		removeCmd.Stdout = os.Stderr
		removeCmd.Stderr = os.Stderr
		if removeErr := removeCmd.Run(); removeErr != nil {
			updateExtensionCheckLog.Printf("Could not remove extension before pin-based install (continuing anyway): %v", removeErr)
		}
		pinCmd := ghCmdForExtension("extension", "install", extensionRepo, "--pin", latestVersion)
		pinCmd.Stdout = os.Stderr
		pinCmd.Stderr = os.Stderr
		if pinErr := pinCmd.Run(); pinErr != nil {
			return false, "", fmt.Errorf("failed to install gh-aw extension at version %s: %w", renderReleaseVersion(latestVersion), pinErr)
		}
		_, versionErr := verifyInstalledVersion(latestVersion)
		if versionErr != nil {
			return false, "", fmt.Errorf("failed to verify gh-aw extension version after upgrade: %w", versionErr)
		}
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("gh-aw extension upgraded to "+renderReleaseVersion(latestVersion)))
		return true, "", nil
	}

	// First attempt: run the upgrade without touching the filesystem.
	// On most systems this will succeed.  On Linux with WSL the kernel may
	// return ETXTBSY when gh tries to open the currently-executing binary for
	// writing; on Windows the OS returns "Access is denied" for the same
	// reason.  In both cases we fall through to the rename+retry path below.
	//
	// On Linux and Windows we buffer the first attempt's output rather than
	// printing it directly, so that the error message is suppressed when the
	// rename+retry path succeeds and the user is not shown a confusing failure.
	var firstAttemptBuf bytes.Buffer
	firstAttemptOut := firstAttemptWriter(os.Stderr, &firstAttemptBuf)
	firstCmd := ghCmdForExtension(extensionUpgradeArgs()...)
	firstCmd.Stdout = firstAttemptOut
	firstCmd.Stderr = firstAttemptOut
	firstErr := firstCmd.Run()
	if firstErr == nil {
		// First attempt succeeded without any file manipulation.
		if needsRenameWorkaround() {
			// Replay the buffered output that was not shown during the attempt.
			_, _ = io.Copy(os.Stderr, &firstAttemptBuf)
		}
		installedVersion, versionErr := installedExtensionVersion()
		if versionErr != nil {
			return false, "", fmt.Errorf("failed to verify gh-aw extension version after upgrade: %w", versionErr)
		}
		if normalizeVersion(installedVersion) != normalizeVersion(latestVersion) {
			updateExtensionCheckLog.Printf("First upgrade attempt reported success but installed version is %s (expected %s)", renderReleaseVersion(installedVersion), renderReleaseVersion(latestVersion))
			mismatchErr := fmt.Errorf("failed to upgrade gh-aw extension: expected %s, got %s", renderReleaseVersion(latestVersion), renderReleaseVersion(installedVersion))
			if !needsRenameWorkaround() {
				return false, "", mismatchErr
			}
			firstErr = fmt.Errorf("gh extension upgrade reported success without installing target version: %w", mismatchErr)
		} else {
			fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("gh-aw extension upgraded to "+renderReleaseVersion(latestVersion)))
			return true, "", nil
		}
	}

	// First attempt failed.
	if !needsRenameWorkaround() {
		// On platforms other than Linux and Windows there is nothing more to try.
		return false, "", fmt.Errorf("failed to upgrade gh-aw extension: %w", firstErr)
	}

	// On Linux the failure is likely ETXTBSY; on Windows it is likely
	// "Access is denied". Both arise because the OS prevents overwriting a
	// running binary. Attempt the rename+retry workaround: rename the
	// currently-running binary away to free up its path, then retry the
	// upgrade so that gh can write the new binary at the original location.
	updateExtensionCheckLog.Printf("First upgrade attempt failed (likely locked binary); retrying with rename workaround. First attempt output: %s", firstAttemptBuf.String())

	// Resolve the current executable path before renaming; after the rename
	// os.Executable() returns a "(deleted)"-suffixed path on Linux.
	var installPath string
	var backupPath string
	if exe, exeErr := os.Executable(); exeErr == nil {
		if resolved, resolveErr := filepath.EvalSymlinks(exe); resolveErr == nil {
			exe = resolved
		}
		if iPath, bPath, renameErr := renamePathForUpgrade(exe); renameErr != nil {
			// Rename failed; the retry will likely fail again.
			updateExtensionCheckLog.Printf("Could not rename executable for retry (upgrade will likely fail): %v", renameErr)
		} else {
			installPath = iPath
			backupPath = bPath
			// On Windows, gh extension remove cannot delete the extension directory
			// while it still contains a running binary (even a renamed one).  Move
			// the backup to a location outside the extension directory so that
			// gh extension remove can succeed.
			//
			// We first try os.TempDir(); if that fails because TEMP is on a
			// different drive (common on GitHub Actions runners where the extension
			// lives on C: but TEMP is on D:), we fall back to the parent of the
			// extension directory which is guaranteed to be on the same drive.
			if runtime.GOOS == "windows" {
				extDir := filepath.Dir(backupPath)
				moved := false

				// Attempt 1: OS temp directory
				tmpBackup := filepath.Join(os.TempDir(), filepath.Base(backupPath))
				if moveErr := os.Rename(backupPath, tmpBackup); moveErr == nil {
					updateExtensionCheckLog.Printf("Moved Windows backup %s -> %s to free extension directory for removal", backupPath, tmpBackup)
					backupPath = tmpBackup
					moved = true
				} else {
					updateExtensionCheckLog.Printf("Could not move backup to %s (cross-drive?): %v; trying same-drive fallback", tmpBackup, moveErr)
				}

				// Attempt 2: parent of the extension directory (same drive as backup)
				if !moved {
					sameDriveDir := filepath.Dir(extDir)
					sameDriveTmp := filepath.Join(sameDriveDir, filepath.Base(backupPath))
					if moveErr2 := os.Rename(backupPath, sameDriveTmp); moveErr2 == nil {
						updateExtensionCheckLog.Printf("Moved Windows backup %s -> %s (same-drive fallback) to free extension directory for removal", backupPath, sameDriveTmp)
						backupPath = sameDriveTmp
					} else {
						updateExtensionCheckLog.Printf("Could not move backup out of extension directory (gh extension remove may fail): %v", moveErr2)
					}
				}

				// After moving our own backup out of the extension directory, try to
				// remove any stale .bak files left by previous upgrade attempts or the
				// gh CLI's own rename mechanism.  These may be temporarily locked by
				// Windows Defender; retry a few times with short delays.
				cleanupStaleWindowsBackups(extDir, backupPath)
			}
		}
	}

	// Retry path: remove + reinstall at the exact target version.
	//
	// Using "gh extension upgrade --force" again would call fetchLatestRelease
	// (/releases/latest) internally, which returns 404 for prerelease-only repos
	// and causes "unable to retrieve latest version for extension" errors.
	// Using "gh extension install --pin VERSION" instead calls fetchReleaseFromTag,
	// which accepts any tag (stable or prerelease).
	//
	// We must remove the extension first because "gh extension install" checks
	// whether the extension is already present via its manifest.yml.  With the
	// manifest in place the install command takes the "already installed" code
	// path and does nothing; removing the extension clears that guard.
	//
	// Note: on Linux the backup file lives inside the extension directory and is
	// gone once the remove step succeeds (unlink frees the directory entry even
	// though the process still holds the file open).  On Windows the backup has
	// been moved to the OS temp directory (above) so the remove step can always
	// succeed.  In both cases we clear backupPath after a successful remove to
	// avoid a misleading restore attempt on subsequent failures.
	removeCmd := ghCmdForExtension("extension", "remove", extensionRepo)
	removeCmd.Stdout = os.Stderr
	removeCmd.Stderr = os.Stderr
	if removeErr := removeCmd.Run(); removeErr == nil {
		// Extension directory has been deleted.
		backupPath = ""
	} else {
		updateExtensionCheckLog.Printf("Could not remove extension before reinstall (will attempt install anyway): %v", removeErr)
	}

	retryCmd := ghCmdForExtension("extension", "install", extensionRepo, "--pin", latestVersion)
	retryCmd.Stdout = os.Stderr
	retryCmd.Stderr = os.Stderr
	if retryErr := retryCmd.Run(); retryErr != nil {
		// Retry also failed. Restore the backup so the user still has gh-aw
		// (only possible when the remove step above did not succeed).
		if backupPath != "" {
			restoreExecutableBackup(installPath, backupPath)
		}
		if runtime.GOOS == "windows" && isWindowsLockError(firstAttemptBuf.String(), retryErr) {
			// On Windows, self-upgrade may not be possible while the binary is
			// running. Guide the user to upgrade manually from a separate shell.
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("On Windows, gh-aw cannot self-upgrade while it is running."))
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Please upgrade manually by running one of the following:"))
			fmt.Fprintln(os.Stderr, "  "+extensionUpgradeHelpCommand(latestVersion))
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("If that does not work, try reinstalling:"))
			fmt.Fprintln(os.Stderr, "  gh extension remove gh-aw")
			fmt.Fprintln(os.Stderr, "  "+extensionInstallHelpCommand(latestVersion))
		}
		return false, "", fmt.Errorf("failed to upgrade gh-aw extension: %w", retryErr)
	}

	_, versionErr := verifyInstalledVersion(latestVersion)
	if versionErr != nil {
		// Verification failed; leave the backup in place so the user can roll
		// back manually if needed.
		return false, "", fmt.Errorf("failed to verify gh-aw extension version after upgrade: %w", versionErr)
	}

	// Verification passed. Clean up the backup now that it is safe to do so
	// (it will already be gone when the remove step above succeeded).
	if backupPath != "" {
		cleanupExecutableBackup(backupPath)
	}

	fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("gh-aw extension upgraded to "+renderReleaseVersion(latestVersion)))
	return true, installPath, nil
}

// needsRenameWorkaround reports whether the current platform requires the
// rename+retry workaround when upgrading the running binary.
//
// On Linux, overwriting a running binary returns ETXTBSY.
// On Windows, the same operation returns "Access is denied".
// Both errors are resolved by renaming the current binary away first.
func needsRenameWorkaround() bool {
	return runtime.GOOS == "linux" || runtime.GOOS == "windows"
}

// firstAttemptWriter returns a writer that buffers output on platforms that
// use the rename+retry workaround (Linux and Windows), so that error messages
// from a failed first upgrade attempt are suppressed when the retry succeeds.
// On other platforms it writes directly to dst.
func firstAttemptWriter(dst io.Writer, buf *bytes.Buffer) io.Writer {
	if needsRenameWorkaround() {
		return buf
	}
	return dst
}

// renamePathForUpgrade renames the binary at exe to a PID-qualified backup
// path (exe+".<pid>.bak"), freeing the original path for the new binary to be
// written by gh extension upgrade.  Using a PID-qualified name ensures each
// invocation gets a unique backup so that a failed cleanup (e.g. Windows cannot
// remove a running binary) does not cause the destination to already exist on
// a subsequent upgrade attempt.
// Returns the install path (exe) and the backup path so the caller can
// relaunch the new binary and restore or clean up the backup.
func renamePathForUpgrade(exe string) (string, string, error) {
	backup := fmt.Sprintf("%s.%d.bak", exe, os.Getpid())
	if err := os.Rename(exe, backup); err != nil {
		return "", "", fmt.Errorf("could not rename %s → %s: %w", exe, backup, err)
	}
	updateExtensionCheckLog.Printf("Renamed %s → %s to free path for upgrade", exe, backup)
	return exe, backup, nil
}

// restoreExecutableBackup renames backupPath back to installPath.
// Called when the upgrade command failed and the new binary was not written.
func restoreExecutableBackup(installPath, backupPath string) {
	if _, statErr := os.Stat(installPath); os.IsNotExist(statErr) {
		// New binary was not installed; restore the backup.
		if renErr := os.Rename(backupPath, installPath); renErr != nil {
			updateExtensionCheckLog.Printf("could not restore backup %s → %s: %v", backupPath, installPath, renErr)
			fmt.Fprintln(os.Stderr, console.FormatErrorMessage(fmt.Sprintf("Failed to restore gh-aw backup after upgrade failure. Manually rename %s to %s to recover.", backupPath, installPath)))
		} else {
			updateExtensionCheckLog.Printf("Restored backup %s → %s after failed upgrade", backupPath, installPath)
		}
	} else {
		// New binary is present (upgrade partially succeeded); just clean up.
		_ = os.Remove(backupPath)
	}
}

// cleanupExecutableBackup removes backupPath after a successful upgrade.
func cleanupExecutableBackup(backupPath string) {
	if err := os.Remove(backupPath); err != nil && !os.IsNotExist(err) {
		updateExtensionCheckLog.Printf("Could not remove backup %s: %v", backupPath, err)
	}
}

// cleanupStaleWindowsBackups attempts to remove any .bak files left in extDir
// by previous upgrade attempts — either by our own code or the gh CLI's own
// rename mechanism.  The file at ownBackup (our currently-active backup for
// this upgrade attempt) is excluded so we do not remove our own relocated file.
//
// Retries with short delays to handle transient locks from antivirus scanners
// (e.g. Windows Defender) that may briefly hold the file open after a process
// exits.  The function is best-effort: if a file cannot be removed it is
// logged and skipped; gh extension remove may still fail but the caller's
// existing error-handling path covers that case.
func cleanupStaleWindowsBackups(extDir string, ownBackup string) {
	entries, err := os.ReadDir(extDir)
	if err != nil {
		updateExtensionCheckLog.Printf("Could not read extension directory for stale .bak cleanup: %v", err)
		return
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".bak") {
			continue
		}
		bakFile := filepath.Join(extDir, entry.Name())
		if bakFile == ownBackup {
			continue // do not remove our own active backup
		}
		for attempt := range maxBackupCleanupAttempts {
			if removeErr := os.Remove(bakFile); removeErr == nil {
				updateExtensionCheckLog.Printf("Removed stale .bak file: %s", bakFile)
				break
			} else if attempt < maxBackupCleanupAttempts-1 {
				updateExtensionCheckLog.Printf("Could not remove stale .bak file %s (attempt %d/%d, retrying in %v): %v",
					bakFile, attempt+1, maxBackupCleanupAttempts, backupCleanupRetryDelay, removeErr)
				time.Sleep(backupCleanupRetryDelay)
			} else {
				updateExtensionCheckLog.Printf("Could not remove stale .bak file %s after %d attempts (gh extension remove may fail): %v",
					bakFile, maxBackupCleanupAttempts, removeErr)
			}
		}
	}
}

// isWindowsLockError reports whether the output or error from an upgrade
// attempt indicate a Windows file-locking issue (the running-binary-lock
// symptom).  Only when a lock error is detected should the Windows-specific
// self-upgrade guidance be shown; other failures should propagate the
// underlying error message instead.
func isWindowsLockError(output string, err error) bool {
	lockMsgs := []string{
		"Access is denied",
		"The process cannot access the file",
		// The gh CLI prints this when it finds a stale .bak file it cannot
		// remove, which is a symptom of the same locked-binary problem.
		"failed to remove previous extension update state",
	}
	for _, msg := range lockMsgs {
		if strings.Contains(output, msg) {
			return true
		}
		//nolint:errstringmatch // gh extension upgrade reports Windows locked-binary failures only via stderr text fragments.
		if err != nil && strings.Contains(err.Error(), msg) {
			return true
		}
	}
	return false
}

// extensionUpgradeArgs returns the gh extension upgrade invocation used by
// self-upgrade checks.
//
// --force is required so pinned installs (e.g. `gh extension install ... --pin`)
// can be upgraded in-place.
func extensionUpgradeArgs() []string {
	return []string{"extension", "upgrade", extensionRepo, "--force"}
}

// ghCmdForExtension creates an exec.Cmd for a gh CLI invocation that must
// target github.com.  gh-aw is only published to github.com; in mixed-host
// environments where GH_HOST points at a GHE instance, the default gh
// commands would hit the wrong host and report "already up to date" or fail.
// Pinning GH_HOST=github.com in the child process environment prevents that.
func ghCmdForExtension(args ...string) *exec.Cmd {
	cmd := exec.Command("gh", args...)
	// Inherit the full environment so that PATH, HOME, etc. remain intact,
	// then override (or add) GH_HOST to ensure github.com is always used.
	env := make([]string, 0, len(os.Environ())+1)
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "GH_HOST=") {
			env = append(env, e)
		}
	}
	env = append(env, "GH_HOST=github.com")
	cmd.Env = env
	return cmd
}

func prereleaseChannelNotice(currentVersion, latestStable string, includePrereleases bool) []string {
	if includePrereleases || latestStable == "" || !isPrereleaseVersion(currentVersion) {
		return nil
	}
	return []string{
		fmt.Sprintf("Current gh-aw version %s is newer than the latest stable release %s; upgrading without --pre-releases would downgrade to that release.", renderReleaseVersion(currentVersion), renderReleaseVersion(latestStable)),
		"Run `gh aw upgrade --pre-releases` to get the latest pre-release.",
	}
}

func renderReleaseVersion(version string) string {
	if isPrereleaseVersion(version) {
		return version + " (pre-release)"
	}
	return version
}

func extensionUpgradeHelpCommand(targetVersion string) string {
	if isPrereleaseVersion(targetVersion) {
		return "gh extension install " + extensionRepo + " --force --pin " + targetVersion
	}
	return "gh " + strings.Join(extensionUpgradeArgs(), " ")
}

func extensionInstallHelpCommand(targetVersion string) string {
	if isPrereleaseVersion(targetVersion) {
		return "gh extension install " + extensionRepo + " --force --pin " + targetVersion
	}
	return "gh extension install " + extensionRepo
}

func installedExtensionVersion() (string, error) {
	var out bytes.Buffer
	cmd := ghCmdForExtension("aw", "version")
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("failed to query installed gh-aw version: %w (output: %s)", err, summarizeCommandOutput(out.String()))
	}
	version, err := parseInstalledVersionOutput(out.String())
	if err != nil {
		return "", err
	}
	return version, nil
}

func verifyInstalledVersion(targetVersion string) (string, error) {
	installedVersion, err := installedExtensionVersion()
	if err != nil {
		return "", err
	}
	if normalizeVersion(installedVersion) != normalizeVersion(targetVersion) {
		return installedVersion, fmt.Errorf("failed to upgrade gh-aw extension: expected %s, got %s", renderReleaseVersion(targetVersion), renderReleaseVersion(installedVersion))
	}
	return installedVersion, nil
}

func normalizeVersion(version string) string {
	return strings.TrimPrefix(version, "v")
}

// parseInstalledVersionOutput scans the output of `gh aw version` for the
// first token that is a valid semantic version (with or without a leading "v").
// It returns the version normalized to include a "v" prefix.
func parseInstalledVersionOutput(output string) (string, error) {
	for token := range strings.FieldsSeq(output) {
		if semverutil.IsValid(token) {
			return semverutil.EnsureVPrefix(token), nil
		}
	}
	return "", fmt.Errorf("could not parse installed gh-aw version from output: %s", summarizeCommandOutput(output))
}

func summarizeCommandOutput(output string) string {
	const maxOutputLen = 300
	trimmed := strings.TrimSpace(output)
	if len(trimmed) <= maxOutputLen {
		return trimmed
	}
	return trimmed[:maxOutputLen] + "…"
}

func isPrereleaseVersion(version string) bool {
	version = "v" + strings.TrimPrefix(version, "v")
	return semver.IsValid(version) && semver.Prerelease(version) != ""
}

// extensionRepo is the GitHub repo slug used in all gh-extension CLI invocations.
const extensionRepo = "github/gh-aw"
