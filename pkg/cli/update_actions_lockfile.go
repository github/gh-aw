package cli

// This file handles updating GitHub Actions versions in .github/aw/actions-lock.json
// (the "lockfile"). See update_actions_deps.go for the shared caching/DI scaffolding
// and update_actions_release.go for release/SHA resolution logic.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/gitutil"
	"github.com/github/gh-aw/pkg/semverutil"
	"github.com/github/gh-aw/pkg/workflow"
)

// UpdateActions updates GitHub Actions versions in .github/aw/actions-lock.json
// It checks each action for newer releases and updates the SHA if a newer version is found.
// By default all actions are updated to the latest major version; pass disableReleaseBump=true
// to revert to the old behaviour where only core (actions/*) actions bypass the --major flag.
//
// coolDown specifies the minimum age a release must have before it is applied. Repos under the
// "actions/" and "github/" namespaces are always exempt from the cooldown.
//
// The ActionCache helpers from pkg/workflow are used so that cached inputs and descriptions
// for safe-outputs.actions entries are preserved when their SHA is unchanged, and cleared
// when the SHA changes (prompting a re-fetch on the next compile).
func UpdateActions(ctx context.Context, allowMajor, verbose, disableReleaseBump bool, coolDown time.Duration) error {
	return updateActions(ctx, defaultActionUpdateDeps(), allowMajor, verbose, disableReleaseBump, coolDown)
}

func updateActions(ctx context.Context, deps actionUpdateDeps, allowMajor, verbose, disableReleaseBump bool, coolDown time.Duration) error {
	updateLog.Print("Starting action updates")

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Checking for GitHub Actions updates..."))
	}

	// Load the action cache (actions-lock.json) using the shared ActionCache helpers
	// so that cached inputs/descriptions for safe-outputs.actions entries are preserved.
	actionsLockPath := filepath.Join(".github", "aw", "actions-lock.json")
	if _, err := os.Stat(actionsLockPath); os.IsNotExist(err) {
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatVerboseMessage("Actions lock file not found: "+actionsLockPath))
		}
		return nil // Not an error, just skip
	}

	actionCache := workflow.NewActionCache(".")
	if err := actionCache.Load(); err != nil {
		return fmt.Errorf("failed to parse actions lock file: %w", err)
	}

	updateLog.Printf("Loaded %d action entries from actions-lock.json", len(actionCache.Entries))

	// Track updates
	var updatedActions []string
	var failedActions []actionUpdateFailure
	var skippedActions []string

	// Snapshot entries before iteration to avoid mutating the map mid-loop.
	type entrySnapshot struct {
		key   string
		entry workflow.ActionCacheEntry
	}
	snapshot := make([]entrySnapshot, 0, len(actionCache.Entries))
	for key, entry := range actionCache.Entries {
		snapshot = append(snapshot, entrySnapshot{key: key, entry: entry})
	}

	for _, s := range snapshot {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		entry := s.entry
		refreshingCurrentVersion := false
		updateLog.Printf("Checking action: %s@%s", entry.Repo, entry.Version)

		// By default all actions are force-updated to the latest major version.
		// When disableReleaseBump is set, only core actions (actions/*) bypass the --major flag.
		effectiveAllowMajor := !disableReleaseBump || allowMajor || isCoreAction(entry.Repo)

		// Check for latest release using the injectable function (also used by updateActionRefsInContent)
		latestVersion, latestSHA, err := deps.getLatestRelease(ctx, entry.Repo, entry.Version, effectiveAllowMajor, verbose)
		if err != nil {
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to check %s: %v", entry.Repo, err)))
			}
			failedActions = append(failedActions, actionUpdateFailure{name: entry.Repo, err: err.Error()})
			continue
		}

		// For gh-aw native actions (github/gh-aw/* and github/gh-aw-actions/*), the action
		// versions are published in lock-step with the CLI. Never update these actions beyond
		// the version of the currently running CLI — doing so would pin a newer (possibly
		// pre-release) action that may be incompatible with the user's installed CLI.
		if isGhAwNativeAction(entry.Repo) {
			cliVersion := GetVersion()
			cliVer := parseVersion(cliVersion)
			latestVer := parseVersion(latestVersion)
			if cliVer != nil && latestVer != nil && latestVer.IsNewer(cliVer) {
				cappedVersion := semverutil.EnsureVPrefix(cliVersion)
				updateLog.Printf("Capping %s update to CLI version %s (latest available %s exceeds running CLI)", entry.Repo, cappedVersion, latestVersion)
				if verbose {
					fmt.Fprintln(os.Stderr, console.FormatVerboseMessage(fmt.Sprintf("%s: capping update target to CLI version %s (latest %s is newer than running CLI)", entry.Repo, cappedVersion, latestVersion)))
				}
				cappedSHA, shaErr := deps.getActionSHAForTag(ctx, gitutil.ExtractBaseRepo(entry.Repo), cappedVersion)
				if shaErr != nil {
					updateLog.Printf("Cannot resolve SHA for %s@%s (CLI version cap): %v; skipping update", entry.Repo, cappedVersion, shaErr)
					fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Skipping %s: cannot resolve SHA for CLI version %s: %v", entry.Repo, cappedVersion, shaErr)))
					failedActions = append(failedActions, actionUpdateFailure{
						name: entry.Repo,
						err:  fmt.Sprintf("cannot resolve SHA for CLI version %s: %v", cappedVersion, shaErr),
					})
					continue
				}
				latestVersion = cappedVersion
				latestSHA = cappedSHA
			}
		}

		// Prevent downgrades: if the proposed version is older than the current, skip.
		// This can happen when GitHub Releases do not include every tag
		// (e.g., v1.1.3 was pushed as a tag-only release without a formal GitHub
		// Release, so the Releases API only returns v1.1.0 as the latest).
		currentVer := parseVersion(entry.Version)
		latestVer := parseVersion(latestVersion)
		if currentVer != nil && latestVer != nil && currentVer.IsNewer(latestVer) {
			updateLog.Printf("Proposed version %s for %s is older than current %s; refreshing the current tag SHA instead", latestVersion, entry.Repo, entry.Version)
			currentSHA, shaErr := deps.getActionSHAForTag(ctx, gitutil.ExtractBaseRepo(entry.Repo), entry.Version)
			if shaErr != nil {
				skipErr := fmt.Sprintf("cannot refresh current tag %s: %v", entry.Version, shaErr)
				updateLog.Printf("Skipping %s: %s", entry.Repo, skipErr)
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Skipping %s: %s", entry.Repo, skipErr)))
				failedActions = append(failedActions, actionUpdateFailure{name: entry.Repo, err: skipErr})
				continue
			}
			latestVersion = entry.Version
			latestSHA = currentSHA
			refreshingCurrentVersion = true
		}

		// Check if update is available
		if latestVersion == entry.Version && latestSHA == entry.SHA {
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatVerboseMessage(fmt.Sprintf("%s@%s is up to date", entry.Repo, entry.Version)))
			}
			skippedActions = append(skippedActions, entry.Repo)
			continue
		}

		// Apply cooldown: if the repo is not exempt and the release is too recent, skip.
		if !refreshingCurrentVersion && !isExemptFromCoolDown(entry.Repo) {
			var coolDownResult coolDownCheckResult
			if cachedDate, ok := actionCache.GetReleasedAt(entry.Repo, latestVersion); ok {
				// Use cached release date to avoid an extra API call.
				coolDownResult = checkReleaseCoolDownWithDate(entry.Repo, latestVersion, cachedDate, coolDown)
			} else {
				// Fetch from API and cache the date for future runs.
				coolDownResult = deps.checkCoolDown(ctx, entry.Repo, latestVersion, coolDown)
				if !coolDownResult.PublishedAt.IsZero() {
					actionCache.SetReleasedAt(entry.Repo, latestVersion, coolDownResult.PublishedAt)
				}
			}
			if coolDownResult.InCoolDown {
				cooldownLog.Printf("Action %s: %s", entry.Repo, coolDownResult.Message)

				// Try to find an older release that has passed the cooldown period.
				olderVersion, olderSHA, findErr := findCooledDownActionVersion(ctx, deps, entry.Repo, entry.Version, effectiveAllowMajor, verbose, coolDown, latestVersion)
				if findErr != nil || olderVersion == "" || olderSHA == "" {
					fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Skipping release candidate %s@%s: %s", entry.Repo, latestVersion, coolDownResult.Message)))
					skippedActions = append(skippedActions, entry.Repo)
					continue
				}
				if verbose {
					fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Falling back to %s for %s (latest release candidate is still in cooldown)", olderVersion, entry.Repo)))
				}
				// Use the older, cooled-down release instead.
				latestVersion = olderVersion
				latestSHA = olderSHA
			}
		}
		if latestSHA == "" {
			skipErr := "could not resolve SHA for " + latestVersion
			updateLog.Printf("Skipping update for %s: %s", entry.Repo, skipErr)
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Skipping %s: %s", entry.Repo, skipErr)))
			failedActions = append(failedActions, actionUpdateFailure{
				name: entry.Repo,
				err:  skipErr,
			})
			continue
		}

		// Update the entry using ActionCache.Set which:
		// - Preserves cached inputs/descriptions when the SHA is unchanged (moving tag)
		// - Clears cached inputs/descriptions when the SHA changes, prompting a re-fetch
		//   of the updated action.yml on the next compile
		oldSHAStr := entry.SHA
		if len(oldSHAStr) > 7 {
			oldSHAStr = oldSHAStr[:7]
		}
		newSHAStr := latestSHA
		if len(newSHAStr) > 7 {
			newSHAStr = newSHAStr[:7]
		}
		if refreshingCurrentVersion {
			updateLog.Printf("Refreshing %s@%s SHA from %s to %s", entry.Repo, entry.Version, oldSHAStr, newSHAStr)
			fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("Refreshed %s@%s SHA", entry.Repo, entry.Version)))
		} else {
			updateLog.Printf("Updating %s from %s (%s) to %s (%s)", entry.Repo, entry.Version, oldSHAStr, latestVersion, newSHAStr)
			fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("Updated %s from %s to %s", entry.Repo, entry.Version, latestVersion)))
		}

		// Set the new entry first; ActionCache.Set handles inputs/description preservation.
		// If the write is rejected (e.g. empty SHA), keep the old entry untouched.
		if !actionCache.Set(entry.Repo, latestVersion, latestSHA) {
			skipErr := fmt.Sprintf("failed to write action cache entry for %s@%s (resolved SHA may be empty)", entry.Repo, latestVersion)
			updateLog.Printf("Skipping update for %s: %s", entry.Repo, skipErr)
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Skipping %s: %s", entry.Repo, skipErr)))
			failedActions = append(failedActions, actionUpdateFailure{
				name: entry.Repo,
				err:  skipErr,
			})
			continue
		}
		// Remove the old key when the version changes, using the original map key from
		// the snapshot to handle any key/version mismatches in the stored cache file.
		if latestVersion != entry.Version {
			actionCache.DeleteByKey(s.key)
		}

		updatedActions = append(updatedActions, entry.Repo)
	}

	// Show summary
	fmt.Fprintln(os.Stderr, "")

	if len(updatedActions) > 0 {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("Updated %d action(s):", len(updatedActions))))
		for _, action := range updatedActions {
			fmt.Fprintln(os.Stderr, console.FormatListItem(action))
		}
		fmt.Fprintln(os.Stderr, "")
	}

	if len(skippedActions) > 0 && verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("%d action(s) already up to date", len(skippedActions))))
		fmt.Fprintln(os.Stderr, "")
	}

	if len(failedActions) > 0 {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to check %d action(s):", len(failedActions))))
		for _, f := range failedActions {
			fmt.Fprintf(os.Stderr, "  %s: %s\n", f.name, f.err)
		}
		fmt.Fprintln(os.Stderr, "")
	}

	// Save the updated actions lock file using ActionCache.Save which preserves
	// all entry fields (including inputs/descriptions for safe-outputs actions).
	if len(updatedActions) > 0 {
		if err := actionCache.Save(); err != nil {
			return fmt.Errorf("failed to save actions lock file: %w", err)
		}

		updateLog.Printf("Successfully wrote updated actions-lock.json with %d updates", len(updatedActions))
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Updated actions-lock.json file"))
	}

	return nil
}
