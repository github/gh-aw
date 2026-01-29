//go:build !integration

package workflow

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

// TestActionPinResolutionWithMismatchedVersions demonstrates the issue where
// TestActionPinResolutionWithMismatchedVersions verifies that when falling back
// to a semver-compatible pin, the comment uses the requested version, not the pin's version
func TestActionPinResolutionWithMismatchedVersions(t *testing.T) {
	// This test demonstrates that when requesting actions/ai-inference@v1,
	// if dynamic resolution fails, it falls back to the hardcoded pin which has
	// version v2, but the comment still shows v1 (the requested version)

	tests := []struct {
		name               string
		repo               string
		requestedVer       string
		expectedCommentVer string // The version that should appear in the comment
		fallbackPinVer     string // The actual pin version used (for warning message)
		expectMismatch     bool
	}{
		{
			name:               "ai-inference v1 resolves to v2 pin but comment shows v1",
			repo:               "actions/ai-inference",
			requestedVer:       "v1",
			expectedCommentVer: "v1", // Comment shows requested version
			fallbackPinVer:     "v2", // Falls back to semver-compatible v2
			expectMismatch:     true,
		},
		{
			name:               "setup-dotnet v4 resolves to v4.3.1 pin but comment shows v4",
			repo:               "actions/setup-dotnet",
			requestedVer:       "v4",
			expectedCommentVer: "v4", // Comment shows requested version
			fallbackPinVer:     "v4.3.1",
			expectMismatch:     true,
		},
		{
			name:               "github-script v7 resolves to v7 pin (exact match)",
			repo:               "actions/github-script",
			requestedVer:       "v7",
			expectedCommentVer: "v7", // Exact match exists in hardcoded pins
			fallbackPinVer:     "v7",
			expectMismatch:     false, // No mismatch since exact match found
		},
		{
			name:               "checkout v5.0.1 exact match",
			repo:               "actions/checkout",
			requestedVer:       "v5.0.1",
			expectedCommentVer: "v5.0.1",
			fallbackPinVer:     "v5.0.1",
			expectMismatch:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a WorkflowData without a resolver to force fallback to hardcoded pins
			data := &WorkflowData{
				StrictMode:     false, // Non-strict mode allows version mismatch
				ActionResolver: nil,   // No resolver to force hardcoded pin usage
			}

			// Capture stderr to check for warning messages
			oldStderr := os.Stderr
			r, w, _ := os.Pipe()
			os.Stderr = w

			result, err := GetActionPinWithData(tt.repo, tt.requestedVer, data)

			w.Close()
			os.Stderr = oldStderr

			var buf bytes.Buffer
			buf.ReadFrom(r)
			stderr := buf.String()

			if err != nil {
				t.Errorf("GetActionPinWithData() error = %v", err)
				return
			}

			if result == "" {
				t.Errorf("GetActionPinWithData() returned empty result")
				return
			}

			// Check if the result contains the expected version in the comment
			if !strings.Contains(result, "# "+tt.expectedCommentVer) {
				t.Errorf("GetActionPinWithData() = %s, expected to contain '# %s'", result, tt.expectedCommentVer)
			}

			// For mismatched versions, we should see a warning
			if tt.expectMismatch {
				if !strings.Contains(stderr, "⚠") {
					t.Errorf("Expected warning message in stderr for version mismatch, got: %s", stderr)
				}
				// Verify the warning mentions both versions
				if !strings.Contains(stderr, tt.requestedVer) || !strings.Contains(stderr, tt.fallbackPinVer) {
					t.Errorf("Warning should mention both requested version (%s) and hardcoded version (%s), got: %s",
						tt.requestedVer, tt.fallbackPinVer, stderr)
				}
			}

			// Log the resolution for debugging
			t.Logf("Resolution: %s@%s → %s", tt.repo, tt.requestedVer, result)
			if stderr != "" {
				t.Logf("Stderr: %s", strings.TrimSpace(stderr))
			}
		})
	}
}

// TestActionPinResolutionWithStrictMode tests that strict mode prevents version mismatches
func TestActionPinResolutionWithStrictMode(t *testing.T) {
	tests := []struct {
		name         string
		repo         string
		requestedVer string
		expectError  bool
	}{
		{
			name:         "ai-inference v1 fails in strict mode",
			repo:         "actions/ai-inference",
			requestedVer: "v1",
			expectError:  true,
		},
		{
			name:         "checkout v5.0.1 succeeds in strict mode",
			repo:         "actions/checkout",
			requestedVer: "v5.0.1",
			expectError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a WorkflowData in strict mode without a resolver
			data := &WorkflowData{
				StrictMode:     true, // Strict mode should error on version mismatch
				ActionResolver: nil,
			}

			// Capture stderr
			oldStderr := os.Stderr
			r, w, _ := os.Pipe()
			os.Stderr = w

			result, err := GetActionPinWithData(tt.repo, tt.requestedVer, data)

			w.Close()
			os.Stderr = oldStderr

			var buf bytes.Buffer
			buf.ReadFrom(r)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error in strict mode for %s@%s, got nil", tt.repo, tt.requestedVer)
				}
				if result != "" {
					t.Errorf("Expected empty result on error, got: %s", result)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
				if result == "" {
					t.Errorf("Expected non-empty result")
				}
			}
		})
	}
}

// TestActionCacheDuplicateSHAWarning verifies that we log warnings when multiple
// version references resolve to the same SHA, which can cause version comment flipping
func TestActionCacheDuplicateSHAWarning(t *testing.T) {
	// Create a test cache with one entry
	cache := &ActionCache{
		Entries: map[string]ActionCacheEntry{
			"actions/github-script@v8": {
				Repo:    "actions/github-script",
				Version: "v8",
				SHA:     "ed597411d8f924073f98dfc5c65a23a2325f34cd",
			},
		},
		path: "/tmp/test-cache.json",
	}

	// Add a second entry with the same SHA but different version
	cache.Set("actions/github-script", "v8.0.0", "ed597411d8f924073f98dfc5c65a23a2325f34cd")

	// Verify both entries are in the cache
	if len(cache.Entries) != 2 {
		t.Errorf("Expected 2 cache entries, got %d", len(cache.Entries))
	}

	// Verify both have the same SHA (this is what causes the issue)
	v8Entry := cache.Entries["actions/github-script@v8"]
	v800Entry := cache.Entries["actions/github-script@v8.0.0"]
	if v8Entry.SHA != v800Entry.SHA {
		t.Error("Expected both entries to have the same SHA")
	}

	t.Logf("Cache has duplicate SHA entries with different versions:")
	t.Logf("  v8: %s", v8Entry.SHA[:8])
	t.Logf("  v8.0.0: %s", v800Entry.SHA[:8])
	t.Logf("This configuration causes version comment flipping in lock files")
}

// TestDeduplicationRemovesLessPreciseVersions verifies that deduplication
// keeps the most precise version and logs detailed information
func TestDeduplicationRemovesLessPreciseVersions(t *testing.T) {
	tests := []struct {
		name                string
		entries             map[string]ActionCacheEntry
		expectedKeep        string
		expectedRemoveCount int
	}{
		{
			name: "v8.0.0 is kept over v8",
			entries: map[string]ActionCacheEntry{
				"actions/github-script@v8": {
					Repo:    "actions/github-script",
					Version: "v8",
					SHA:     "ed597411d8f924073f98dfc5c65a23a2325f34cd",
				},
				"actions/github-script@v8.0.0": {
					Repo:    "actions/github-script",
					Version: "v8.0.0",
					SHA:     "ed597411d8f924073f98dfc5c65a23a2325f34cd",
				},
			},
			expectedKeep:        "actions/github-script@v8.0.0",
			expectedRemoveCount: 1,
		},
		{
			name: "v6.1.0 is kept over v6",
			entries: map[string]ActionCacheEntry{
				"actions/setup-node@v6": {
					Repo:    "actions/setup-node",
					Version: "v6",
					SHA:     "395ad3262231945c25e8478fd5baf05154b1d79f",
				},
				"actions/setup-node@v6.1.0": {
					Repo:    "actions/setup-node",
					Version: "v6.1.0",
					SHA:     "395ad3262231945c25e8478fd5baf05154b1d79f",
				},
			},
			expectedKeep:        "actions/setup-node@v6.1.0",
			expectedRemoveCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cache := &ActionCache{
				Entries: tt.entries,
				path:    "/tmp/test-cache.json",
			}

			initialCount := len(cache.Entries)
			cache.deduplicateEntries()

			if _, exists := cache.Entries[tt.expectedKeep]; !exists {
				t.Errorf("Expected entry %s to be kept, but it was removed", tt.expectedKeep)
			}

			removed := initialCount - len(cache.Entries)
			if removed != tt.expectedRemoveCount {
				t.Errorf("Expected %d entries to be removed, but %d were removed",
					tt.expectedRemoveCount, removed)
			}

			t.Logf("Deduplication kept %s, removed %d less precise entries",
				tt.expectedKeep, removed)
		})
	}
}
