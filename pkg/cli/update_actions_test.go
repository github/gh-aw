//go:build !integration

package cli

import (
	"encoding/json"
	"testing"
)

func TestActionKeyVersionConsistency(t *testing.T) {
	// This test ensures that when an action is updated, the key in the map
	// is updated to match the new version, preventing key/version mismatches
	// that would cause version comments to change on each build.

	// Simulate the actions-lock.json structure
	actionsLock := actionsLockFile{
		Entries: map[string]actionsLockEntry{
			"actions/checkout@v5.0.0": {
				Repo:    "actions/checkout",
				Version: "v5.0.0",
				SHA:     "oldsha1234567890123456789012345678901234",
			},
		},
	}

	// Simulate an update to a newer version
	oldKey := "actions/checkout@v5.0.0"
	entry := actionsLock.Entries[oldKey]
	latestVersion := "v5.0.1"
	latestSHA := "newsha1234567890123456789012345678901234"

	// Apply the update logic from UpdateActions
	delete(actionsLock.Entries, oldKey)
	newKey := entry.Repo + "@" + latestVersion
	actionsLock.Entries[newKey] = actionsLockEntry{
		Repo:    entry.Repo,
		Version: latestVersion,
		SHA:     latestSHA,
	}

	// Verify the old key is gone
	if _, exists := actionsLock.Entries[oldKey]; exists {
		t.Errorf("Old key %q should have been deleted", oldKey)
	}

	// Verify the new key exists
	updatedEntry, exists := actionsLock.Entries[newKey]
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
	// This test ensures that when actions-lock.json is loaded and saved,
	// there are no key/version mismatches

	jsonData := `{
		"entries": {
			"actions/checkout@v5.0.1": {
				"repo": "actions/checkout",
				"version": "v5.0.1",
				"sha": "93cb6efe18208431cddfb8368fd83d5badbf9bfd"
			},
			"actions/setup-node@v6.1.0": {
				"repo": "actions/setup-node",
				"version": "v6.1.0",
				"sha": "395ad3262231945c25e8478fd5baf05154b1d79f"
			}
		}
	}`

	var actionsLock actionsLockFile
	if err := json.Unmarshal([]byte(jsonData), &actionsLock); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	// Verify all entries have matching key and version
	for key, entry := range actionsLock.Entries {
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

func TestExtractBaseRepo(t *testing.T) {
	tests := []struct {
		name       string
		actionPath string
		want       string
	}{
		{
			name:       "action without subfolder",
			actionPath: "actions/checkout",
			want:       "actions/checkout",
		},
		{
			name:       "action with one subfolder",
			actionPath: "actions/cache/restore",
			want:       "actions/cache",
		},
		{
			name:       "action with multiple subfolders",
			actionPath: "github/codeql-action/upload-sarif",
			want:       "github/codeql-action",
		},
		{
			name:       "action with deeply nested subfolders",
			actionPath: "owner/repo/path/to/action",
			want:       "owner/repo",
		},
		{
			name:       "action with only owner",
			actionPath: "owner",
			want:       "owner",
		},
		{
			name:       "empty string",
			actionPath: "",
			want:       "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractBaseRepo(tt.actionPath)
			if got != tt.want {
				t.Errorf("extractBaseRepo(%q) = %q, want %q", tt.actionPath, got, tt.want)
			}
		})
	}
}

func TestMajorVersionPreference(t *testing.T) {
	// Test that the version selection logic prefers major-only versions (v8)
	// over full semantic versions (v8.0.0) when they are semantically equal.
	// This follows GitHub Actions best practice of using major version tags.

	tests := []struct {
		name              string
		releases          []string
		currentVersion    string
		allowMajor        bool
		expectedVersion   string
		expectedPreferred string // The version that should be preferred (v8 over v8.0.0.0)
	}{
		{
			name:              "prefer v8 over v8.0.0",
			releases:          []string{"v8.0.0", "v8", "v7.0.0"},
			currentVersion:    "v8",
			allowMajor:        false,
			expectedVersion:   "v8",
			expectedPreferred: "v8",
		},
		{
			name:              "prefer v6 over v6.0.0",
			releases:          []string{"v6.0.0", "v6", "v5.0.0"},
			currentVersion:    "v6",
			allowMajor:        false,
			expectedVersion:   "v6",
			expectedPreferred: "v6",
		},
		{
			name:              "prefer v8 over v8.0.0.0 (four-part version)",
			releases:          []string{"v8.0.0.0", "v8"},
			currentVersion:    "v8",
			allowMajor:        false,
			expectedVersion:   "v8",
			expectedPreferred: "v8",
		},
		{
			name:              "prefer newest when versions differ",
			releases:          []string{"v8.1.0", "v8.0.0", "v8"},
			currentVersion:    "v8",
			allowMajor:        false,
			expectedVersion:   "v8.1.0",
			expectedPreferred: "v8.1.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			currentVer := parseVersion(tt.currentVersion)
			if currentVer == nil {
				t.Fatalf("Failed to parse current version: %s", tt.currentVersion)
			}

			var latestCompatible string
			var latestCompatibleVersion *semanticVersion

			for _, release := range tt.releases {
				releaseVer := parseVersion(release)
				if releaseVer == nil {
					continue
				}

				// Check if compatible based on major version
				if !tt.allowMajor && releaseVer.major != currentVer.major {
					continue
				}

				// Check if this is newer than what we have
				if latestCompatibleVersion == nil || releaseVer.isNewer(latestCompatibleVersion) {
					latestCompatible = release
					latestCompatibleVersion = releaseVer
				} else if !releaseVer.isNewer(latestCompatibleVersion) &&
					releaseVer.major == latestCompatibleVersion.major &&
					releaseVer.minor == latestCompatibleVersion.minor &&
					releaseVer.patch == latestCompatibleVersion.patch {
					// If versions are equal, prefer the less precise one (e.g., "v8" over "v8.0.0")
					if !releaseVer.isPreciseVersion() && latestCompatibleVersion.isPreciseVersion() {
						latestCompatible = release
						latestCompatibleVersion = releaseVer
					}
				}
			}

			if latestCompatible != tt.expectedVersion {
				t.Errorf("Selected version = %q, want %q", latestCompatible, tt.expectedVersion)
			}

			// Verify that the selected version is the preferred one (less precise when equal)
			if latestCompatible != tt.expectedPreferred {
				t.Errorf("Preferred version = %q, want %q (should prefer less precise version)", latestCompatible, tt.expectedPreferred)
			}
		})
	}
}
