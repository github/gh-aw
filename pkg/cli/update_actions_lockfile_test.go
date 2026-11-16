//go:build !integration

package cli

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/github/gh-aw/pkg/workflow"
)

func TestActionKeyVersionConsistency(t *testing.T) {
	// This test ensures that when an action is updated, the key in the map
	// is updated to match the new version, preventing key/version mismatches
	// that would cause version comments to change on each build.

	// Simulate the actions-lock.json structure using ActionCache
	tmpDir := testutil.TempDir(t, "test-*")
	cache := workflow.NewActionCache(tmpDir)
	cache.Set("actions/checkout", "v5.0.0", "oldsha1234567890123456789012345678901234")

	// Simulate an update to a newer version
	oldVersion := "v5.0.0"
	latestVersion := "v5.0.1"
	latestSHA := "newsha1234567890123456789012345678901234"

	// Apply the update logic from UpdateActions: delete old key, set new entry
	cache.Delete("actions/checkout", oldVersion)
	cache.Set("actions/checkout", latestVersion, latestSHA)

	oldKey := "actions/checkout@v5.0.0"
	newKey := "actions/checkout@v5.0.1"

	// Verify the old key is gone
	if _, exists := cache.Entries[oldKey]; exists {
		t.Errorf("Old key %q should have been deleted", oldKey)
	}

	// Verify the new key exists
	updatedEntry, exists := cache.Entries[newKey]
	if !exists {
		t.Errorf("New key %q should exist", newKey)
	}

	// Verify the entry has the correct version
	if updatedEntry.Version != latestVersion {
		t.Errorf("Entry version = %q, want %q", updatedEntry.Version, latestVersion)
	}

	// Most importantly: verify key and version field match
	keyVersion := newKey[len("actions/checkout@"):]
	if keyVersion != updatedEntry.Version {
		t.Errorf("Key version %q does not match entry version %q", keyVersion, updatedEntry.Version)
	}
}
func TestActionKeyVersionConsistencyInJSON(t *testing.T) {
	// This test ensures that when actions-lock.json is saved to disk and reloaded,
	// there are no key/version mismatches between the map key and the entry's Version field.

	tmpDir := testutil.TempDir(t, "test-*")
	cache := workflow.NewActionCache(tmpDir)
	cache.Set("actions/checkout", "v5.0.1", "93cb6efe18208431cddfb8368fd83d5badbf9bfd")
	cache.Set("actions/setup-node", "v6.1.0", "395ad3262231945c25e8478fd5baf05154b1d79f")

	// Save to disk and reload to exercise the JSON round-trip.
	if err := cache.Save(); err != nil {
		t.Fatalf("Failed to save cache: %v", err)
	}
	reloaded := workflow.NewActionCache(tmpDir)
	if err := reloaded.Load(); err != nil {
		t.Fatalf("Failed to reload cache: %v", err)
	}

	// Verify all entries have matching key and version after a round-trip.
	for key, entry := range reloaded.Entries {
		// Extract version from key (format: "repo@version")
		atIndex := len(key)
		for i := len(key) - 1; i >= 0; i-- {
			if key[i] == '@' {
				atIndex = i
				break
			}
		}

		if atIndex < len(key) {
			keyVersion := key[atIndex+1:]
			if keyVersion != entry.Version {
				t.Errorf("Key %q has version in key %q but entry version is %q - this mismatch causes version comments to change on each build",
					key, keyVersion, entry.Version)
			}
		}
	}
}

// TestUpdateActions_SafeOutputsInputsPreserved verifies that cached inputs and descriptions
// for safe-outputs.actions entries are preserved in actions-lock.json when other (unrelated)
// actions are updated. Previously, actionsLockEntry lacked Inputs/ActionDescription fields,
// causing them to be silently dropped whenever the file was rewritten.
func TestUpdateActions_SafeOutputsInputsPreserved(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")

	// Stub the release-fetch function so no network calls are made.
	// actions/checkout gets a bump; owner/my-safe-action is already at latest.
	deps := newActionUpdateDepsWithLatestRelease(func(_ context.Context, repo, currentVersion string, allowMajor, verbose bool) (string, string, error) {
		switch repo {
		case "actions/checkout":
			return "v5", "newcheckoutsha1234567890123456789012345", nil
		case "owner/my-safe-action":
			// Same version/SHA → no update needed
			return "v1", "mysafesha12345678901234567890123456789012", nil
		default:
			return currentVersion, "", nil
		}
	})

	// Build actions-lock.json with a regular action and a safe-outputs action (with cached inputs).
	cache := workflow.NewActionCache(tmpDir)
	cache.Set("actions/checkout", "v4", "oldcheckoutsha234567890123456789012345678")
	cache.Set("owner/my-safe-action", "v1", "mysafesha12345678901234567890123456789012")
	cache.SetInputs("owner/my-safe-action", "v1", map[string]*workflow.ActionYAMLInput{
		"foo": {Description: "Foo input", Required: true},
	})
	if err := cache.Save(); err != nil {
		t.Fatalf("failed to save initial cache: %v", err)
	}

	// Run UpdateActions from tmpDir
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(wd); err != nil {
			t.Errorf("failed to restore working directory: %v", err)
		}
	})
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}

	if err := updateActions(context.Background(), deps, false, false, false, 0); err != nil {
		t.Fatalf("UpdateActions() error = %v", err)
	}

	// Reload the saved cache and verify safe-outputs inputs were preserved.
	saved := workflow.NewActionCache(tmpDir)
	if err := saved.Load(); err != nil {
		t.Fatalf("failed to reload cache: %v", err)
	}

	// actions/checkout should have been updated to v5
	checkoutEntry, ok := saved.Entries["actions/checkout@v5"]
	if !ok {
		t.Error("expected actions/checkout@v5 entry after update")
	} else if checkoutEntry.SHA != "newcheckoutsha1234567890123456789012345" {
		t.Errorf("actions/checkout SHA = %q, want newcheckoutsha...", checkoutEntry.SHA)
	}

	// safe-outputs action inputs must still be present
	safeEntry, ok := saved.Entries["owner/my-safe-action@v1"]
	if !ok {
		t.Fatal("expected owner/my-safe-action@v1 entry to be present after update")
	}
	if safeEntry.Inputs == nil {
		t.Error("safe-outputs action inputs were lost after update (expected to be preserved)")
	} else if _, hasFoo := safeEntry.Inputs["foo"]; !hasFoo {
		t.Errorf("safe-outputs action inputs missing 'foo' key; got %v", safeEntry.Inputs)
	}
}
func TestUpdateActions_GhAwNativeActionCappedAtCLIVersion(t *testing.T) {
	// Set the running CLI version to v0.68.3
	origVersion := GetVersion()
	SetVersionInfo("v0.68.3")
	defer SetVersionInfo(origVersion)

	// Stub latest release resolution to return a newer version (v0.68.7) simulating
	// the scenario where a newer release exists but the CLI is still at v0.68.3.
	deps := newTestActionUpdateDeps()
	deps.getLatestRelease = func(_ context.Context, repo, currentVersion string, allowMajor, verbose bool) (string, string, error) {
		switch repo {
		case "github/gh-aw-actions/setup":
			return "v0.68.7", "newersha1234567890123456789012345678901234", nil
		case "github/gh-aw/actions/setup":
			return "v0.68.7", "newersha1234567890123456789012345678901234", nil
		default:
			return currentVersion, "defaultsha12345678901234567890123456789012", nil
		}
	}

	// Stub tag-to-SHA resolution to return a SHA for the CLI version tag (v0.68.3).
	const cliVersionSHA = "cliversha12345678901234567890123456789012"
	deps.getActionSHAForTag = func(_ context.Context, repo, tag string) (string, error) {
		if tag == "v0.68.3" {
			return cliVersionSHA, nil
		}
		return "othersha12345678901234567890123456789012", nil
	}

	tmpDir := testutil.TempDir(t, "test-*")
	cache := workflow.NewActionCache(tmpDir)
	cache.Set("github/gh-aw-actions/setup", "v0.68.1", "oldsha1234567890123456789012345678901234a")
	cache.Set("github/gh-aw/actions/setup", "v0.68.1", "oldsha1234567890123456789012345678901234b")
	if err := cache.Save(); err != nil {
		t.Fatalf("failed to save initial cache: %v", err)
	}

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(wd); err != nil {
			t.Errorf("failed to restore working directory: %v", err)
		}
	})
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}

	if err := updateActions(context.Background(), deps, false, false, false, 0); err != nil {
		t.Fatalf("UpdateActions() error = %v", err)
	}

	saved := workflow.NewActionCache(tmpDir)
	if err := saved.Load(); err != nil {
		t.Fatalf("failed to reload cache: %v", err)
	}

	// Both gh-aw native actions must have been updated to the CLI version (v0.68.3),
	// not the latest release (v0.68.7).
	for _, repo := range []string{"github/gh-aw-actions/setup", "github/gh-aw/actions/setup"} {
		expectedKey := repo + "@v0.68.3"
		entry, ok := saved.Entries[expectedKey]
		if !ok {
			t.Errorf("expected entry %q in actions-lock.json (capped at CLI version), got entries: %v", expectedKey, savedEntryKeys(saved))
			continue
		}
		if entry.SHA != cliVersionSHA {
			t.Errorf("%s SHA = %q, want CLI-version SHA %q", repo, entry.SHA, cliVersionSHA)
		}
		// The newer version must NOT appear.
		newerKey := repo + "@v0.68.7"
		if _, found := saved.Entries[newerKey]; found {
			t.Errorf("found unexpected entry %q (gh-aw native action must not exceed CLI version)", newerKey)
		}
	}
}

// TestUpdateActions_SetupActionPinnedToCompilerVersion verifies that the gh-aw setup
// action is pinned to exactly the compiler version, even when the latest release is
// older, the lock already holds a newer version, or release bumps are disabled.
func TestUpdateActions_SetupActionPinnedToCompilerVersion(t *testing.T) {
	setReleaseVersionForTest(t, "v0.68.3", true)

	const cliVersionSHA = "cliversha12345678901234567890123456789012"
	tests := []struct {
		name               string
		lockedVersion      string
		disableReleaseBump bool
	}{
		{name: "older lock, latest release below compiler", lockedVersion: "v0.67.0"},
		{name: "lock newer than compiler", lockedVersion: "v0.69.0"},
		{name: "release bumps disabled", lockedVersion: "v0.67.0", disableReleaseBump: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deps := newTestActionUpdateDeps()
			deps.getLatestRelease = func(_ context.Context, repo, currentVersion string, allowMajor, verbose bool) (string, string, error) {
				if repo == "github/gh-aw-actions/setup" {
					t.Errorf("getLatestRelease must not be called for the compiler-pinned setup action")
				}
				return currentVersion, "defaultsha12345678901234567890123456789012", nil
			}
			deps.getActionSHAForTag = func(_ context.Context, repo, tag string) (string, error) {
				if repo != "github/gh-aw-actions" || tag != "v0.68.3" {
					t.Errorf("unexpected tag lookup %s@%s", repo, tag)
				}
				return cliVersionSHA, nil
			}

			tmpDir := testutil.TempDir(t, "test-*")
			cache := workflow.NewActionCache(tmpDir)
			cache.Set("github/gh-aw-actions/setup", tt.lockedVersion, "oldsha1234567890123456789012345678901234a")
			if err := cache.Save(); err != nil {
				t.Fatalf("failed to save initial cache: %v", err)
			}
			wd, err := os.Getwd()
			if err != nil {
				t.Fatalf("failed to get working directory: %v", err)
			}
			t.Cleanup(func() { _ = os.Chdir(wd) })
			if err := os.Chdir(tmpDir); err != nil {
				t.Fatalf("failed to chdir: %v", err)
			}

			if err := updateActions(context.Background(), deps, false, false, tt.disableReleaseBump, 0); err != nil {
				t.Fatalf("updateActions() error = %v", err)
			}

			saved := workflow.NewActionCache(tmpDir)
			if err := saved.Load(); err != nil {
				t.Fatalf("failed to reload cache: %v", err)
			}
			entry, ok := saved.Entries["github/gh-aw-actions/setup@v0.68.3"]
			if !ok {
				t.Fatalf("expected setup pinned to compiler version v0.68.3, got entries: %v", savedEntryKeys(saved))
			}
			if entry.SHA != cliVersionSHA {
				t.Errorf("setup SHA = %q, want %q", entry.SHA, cliVersionSHA)
			}
			if _, found := saved.Entries["github/gh-aw-actions/setup@"+tt.lockedVersion]; found {
				t.Errorf("stale entry for %s should have been replaced", tt.lockedVersion)
			}
		})
	}
}

// setReleaseVersionForTest sets the compiler version and release flag, restoring both on cleanup.
func setReleaseVersionForTest(t *testing.T, version string, release bool) {
	t.Helper()
	origVersion, origRelease := GetVersion(), workflow.IsRelease()
	SetVersionInfo(version)
	workflow.SetIsRelease(release)
	t.Cleanup(func() {
		SetVersionInfo(origVersion)
		workflow.SetIsRelease(origRelease)
	})
}

// TestCompilerPinnedActionTag_RequiresReleaseBuild verifies that a semver-shaped
// version on a non-release binary does not pin the setup action.
func TestCompilerPinnedActionTag_RequiresReleaseBuild(t *testing.T) {
	setReleaseVersionForTest(t, "v0.68.3", false)
	if got := compilerPinnedActionTag("github/gh-aw-actions/setup"); got != "" {
		t.Errorf("compilerPinnedActionTag() = %q for non-release build, want empty", got)
	}

	workflow.SetIsRelease(true)
	if got := compilerPinnedActionTag("github/gh-aw-actions/setup"); got != "v0.68.3" {
		t.Errorf("compilerPinnedActionTag() = %q for release build, want v0.68.3", got)
	}

	SetVersionInfo("v0.69.0-beta.1")
	if got := compilerPinnedActionTag("github/gh-aw-actions/setup"); got != "v0.69.0-beta.1" {
		t.Errorf("compilerPinnedActionTag() = %q for prerelease build, want v0.69.0-beta.1", got)
	}
}

// TestUpdateActions_NeverDowngradesRefreshesCurrentTagSHA verifies that when the
// resolved latest version is older than the pinned version, UpdateActions refreshes
// the SHA for the current version instead of downgrading.
func TestUpdateActions_NeverDowngradesRefreshesCurrentTagSHA(t *testing.T) {
	deps := newTestActionUpdateDeps()
	// Simulate the Releases API returning a lower version than what is already pinned
	// in actions-lock.json (e.g. actions-ecosystem/action-add-labels: v1.1.3 → v1.1.0).
	deps.getLatestRelease = func(_ context.Context, repo, currentVersion string, allowMajor, verbose bool) (string, string, error) {
		if repo == "actions-ecosystem/action-add-labels" {
			// API only knows about v1.1.0 even though v1.1.3 is already pinned
			return "v1.1.0", "oldsha1234567890123456789012345678901234a", nil
		}
		// Other actions are already at their latest version
		return currentVersion, "somesha12345678901234567890123456789012b", nil
	}
	const refreshedSHA = "18f1af5e3544586314bbe15c0273249c770b2daf"
	deps.getActionSHAForTag = func(_ context.Context, repo, tag string) (string, error) {
		if repo != "actions-ecosystem/action-add-labels" || tag != "v1.1.3" {
			t.Fatalf("getActionSHAForTag(%q, %q), want actions-ecosystem/action-add-labels@v1.1.3", repo, tag)
		}
		return refreshedSHA, nil
	}

	tmpDir := testutil.TempDir(t, "test-*")
	cache := workflow.NewActionCache(tmpDir)
	const currentSHA = "c96b68fec76a0987cd93957189e9abd0b9a72ff1"
	cache.Set("actions-ecosystem/action-add-labels", "v1.1.3", currentSHA)
	if err := cache.Save(); err != nil {
		t.Fatalf("failed to save initial cache: %v", err)
	}

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(wd); err != nil {
			t.Errorf("failed to restore working directory: %v", err)
		}
	})
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}

	if err := updateActions(context.Background(), deps, true, false, false, 0); err != nil {
		t.Fatalf("UpdateActions() error = %v", err)
	}

	saved := workflow.NewActionCache(tmpDir)
	if err := saved.Load(); err != nil {
		t.Fatalf("failed to reload cache: %v", err)
	}

	// The action must remain at v1.1.3, but its SHA must be refreshed.
	entry, ok := saved.Entries["actions-ecosystem/action-add-labels@v1.1.3"]
	if !ok {
		t.Errorf("expected entry actions-ecosystem/action-add-labels@v1.1.3 to be preserved; got entries: %v", savedEntryKeys(saved))
	} else if entry.SHA != refreshedSHA {
		t.Errorf("SHA = %q, want refreshed SHA %q", entry.SHA, refreshedSHA)
	}

	// The downgraded entry must NOT appear.
	if _, found := saved.Entries["actions-ecosystem/action-add-labels@v1.1.0"]; found {
		t.Error("downgraded entry actions-ecosystem/action-add-labels@v1.1.0 must not appear")
	}
}
func savedEntryKeys(cache *workflow.ActionCache) []string {
	keys := make([]string, 0, len(cache.Entries))
	for k := range cache.Entries {
		keys = append(keys, k)
	}
	return keys
}

// TestUpdateActions_CooldownFallbackToOlderRelease verifies that updateActions
// falls back to an older cooled-down release instead of skipping the update
// entirely when the newest candidate is still in cooldown.
func TestUpdateActions_CooldownFallbackToOlderRelease(t *testing.T) {
	deps := newTestActionUpdateDeps()

	// Latest version for ruby/setup-ruby is v1.321.0 (in cooldown).
	deps.getLatestRelease = func(_ context.Context, repo, _ string, _, _ bool) (string, string, error) {
		if repo == "ruby/setup-ruby" {
			return "v1.321.0", "sha321_12345678901234567890123456789012", nil
		}
		return "v1.0.0", "default_1234567890123456789012345678901234", nil
	}

	// v1.321.0 is in cooldown; v1.320.0 has cooled down.
	deps.runGHReleasesAPI = func(_ context.Context, _ string) ([]byte, error) {
		return []byte("v1.321.0\nv1.320.0\nv1.300.0"), nil
	}
	deps.checkCoolDown = func(_ context.Context, repo, tag string, cd time.Duration) coolDownCheckResult {
		switch tag {
		case "v1.321.0":
			return checkReleaseCoolDownWithDate(repo, tag, time.Now().Add(-1*24*time.Hour), cd)
		default:
			return coolDownCheckResult{}
		}
	}
	const fallbackSHA = "sha320_12345678901234567890123456789012"
	deps.getActionSHAForTag = func(_ context.Context, _, tag string) (string, error) {
		if tag == "v1.320.0" {
			return fallbackSHA, nil
		}
		return "sha321_12345678901234567890123456789012", nil
	}

	tmpDir := testutil.TempDir(t, "test-*")
	cache := workflow.NewActionCache(tmpDir)
	cache.Set("ruby/setup-ruby", "v1.300.0", "sha300_12345678901234567890123456789012")
	if err := cache.Save(); err != nil {
		t.Fatalf("failed to save initial cache: %v", err)
	}

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}

	if err := updateActions(context.Background(), deps, true, false, false, 7*24*time.Hour); err != nil {
		t.Fatalf("updateActions() error = %v", err)
	}

	saved := workflow.NewActionCache(tmpDir)
	if err := saved.Load(); err != nil {
		t.Fatalf("failed to reload cache: %v", err)
	}

	// Should be updated to v1.320.0 (cooled-down fallback), not v1.321.0.
	entry, ok := saved.Entries["ruby/setup-ruby@v1.320.0"]
	if !ok {
		t.Errorf("expected ruby/setup-ruby@v1.320.0 in cache after cooldown fallback; got entries: %v", savedEntryKeys(saved))
	} else if entry.SHA != fallbackSHA {
		t.Errorf("SHA = %q, want %q", entry.SHA, fallbackSHA)
	}
}

// TestUpdateActions_EmptyResolvedSHARetainsExistingEntry verifies that
// updateActions keeps the existing cache entry unchanged when the resolved
// SHA for a candidate release comes back empty.
func TestUpdateActions_EmptyResolvedSHARetainsExistingEntry(t *testing.T) {
	deps := newTestActionUpdateDeps()
	deps.getLatestRelease = func(_ context.Context, repo, currentVersion string, _, _ bool) (string, string, error) {
		if repo == "ruby/setup-ruby" {
			return "v1.321.0", "", nil
		}
		return currentVersion, "", nil
	}

	tmpDir := testutil.TempDir(t, "test-*")
	cache := workflow.NewActionCache(tmpDir)
	cache.Set("ruby/setup-ruby", "v1.300.0", "sha300_12345678901234567890123456789012")
	if err := cache.Save(); err != nil {
		t.Fatalf("failed to save initial cache: %v", err)
	}

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}

	if err := updateActions(context.Background(), deps, true, false, false, 0); err != nil {
		t.Fatalf("updateActions() error = %v", err)
	}

	saved := workflow.NewActionCache(tmpDir)
	if err := saved.Load(); err != nil {
		t.Fatalf("failed to reload cache: %v", err)
	}

	existing, ok := saved.Entries["ruby/setup-ruby@v1.300.0"]
	if !ok {
		t.Fatalf("expected existing entry to be retained when resolved SHA is empty; got entries: %v", savedEntryKeys(saved))
	}
	if existing.SHA != "sha300_12345678901234567890123456789012" {
		t.Fatalf("expected retained entry SHA to stay unchanged, got %q", existing.SHA)
	}
	if _, ok := saved.Entries["ruby/setup-ruby@v1.321.0"]; ok {
		t.Fatalf("did not expect new empty-SHA version entry to be created; got entries: %v", savedEntryKeys(saved))
	}
}
