//go:build !integration

package workflow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/github/gh-aw/pkg/semverutil"
	"github.com/github/gh-aw/pkg/testutil"
)

func TestActionCache(t *testing.T) {
	// Create temporary directory for testing
	tmpDir := testutil.TempDir(t, "test-*")

	cache := NewActionCache(tmpDir)

	// Test setting and getting
	cache.Set("actions/checkout", "v5", "abc123")

	sha, found := cache.Get("actions/checkout", "v5")
	if !found {
		t.Error("Expected to find cached entry")
	}
	if sha != "abc123" {
		t.Errorf("Expected SHA 'abc123', got '%s'", sha)
	}

	// Test cache miss
	_, found = cache.Get("actions/unknown", "v1")
	if found {
		t.Error("Expected cache miss for unknown action")
	}
}

func TestActionCacheSaveLoad(t *testing.T) {
	// Create temporary directory for testing
	tmpDir := testutil.TempDir(t, "test-*")

	// Create and populate cache
	cache1 := NewActionCache(tmpDir)
	cache1.Set("actions/checkout", "v5", "abc123")
	cache1.Set("actions/setup-node", "v4", "def456")

	// Save to disk
	err := cache1.Save()
	if err != nil {
		t.Fatalf("Failed to save cache: %v", err)
	}

	// Verify file exists
	cachePath := filepath.Join(tmpDir, ".github", "aw", CacheFileName)
	if _, err := os.Stat(cachePath); os.IsNotExist(err) {
		t.Fatalf("Cache file was not created at %s", cachePath)
	}

	// Load into new cache instance
	cache2 := NewActionCache(tmpDir)
	err = cache2.Load()
	if err != nil {
		t.Fatalf("Failed to load cache: %v", err)
	}

	// Verify entries were loaded
	sha, found := cache2.Get("actions/checkout", "v5")
	if !found || sha != "abc123" {
		t.Errorf("Expected to find actions/checkout@v5 with SHA 'abc123', got '%s' (found=%v)", sha, found)
	}

	sha, found = cache2.Get("actions/setup-node", "v4")
	if !found || sha != "def456" {
		t.Errorf("Expected to find actions/setup-node@v6 with SHA 'def456', got '%s' (found=%v)", sha, found)
	}
}

func TestActionCacheLoadNonExistent(t *testing.T) {
	// Create temporary directory for testing
	tmpDir := testutil.TempDir(t, "test-*")

	cache := NewActionCache(tmpDir)

	// Try to load non-existent cache - should not error
	err := cache.Load()
	if err != nil {
		t.Errorf("Loading non-existent cache should not error, got: %v", err)
	}

	// Cache should be empty
	if len(cache.Entries) != 0 {
		t.Errorf("Expected empty cache, got %d entries", len(cache.Entries))
	}
}

func TestActionCacheGetCachePath(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")
	cache := NewActionCache(tmpDir)

	expectedPath := filepath.Join(tmpDir, ".github", "aw", CacheFileName)
	if cache.GetCachePath() != expectedPath {
		t.Errorf("Expected cache path '%s', got '%s'", expectedPath, cache.GetCachePath())
	}
}

func TestActionCacheTrailingNewline(t *testing.T) {
	// Create temporary directory for testing
	tmpDir := testutil.TempDir(t, "test-*")

	// Create and populate cache
	cache := NewActionCache(tmpDir)
	cache.Set("actions/checkout", "v5", "abc123")

	// Save to disk
	err := cache.Save()
	if err != nil {
		t.Fatalf("Failed to save cache: %v", err)
	}

	// Read the file and check for trailing newline
	cachePath := filepath.Join(tmpDir, ".github", "aw", CacheFileName)
	data, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatalf("Failed to read cache file: %v", err)
	}

	// Verify file ends with newline (prettier compliance)
	if len(data) == 0 || data[len(data)-1] != '\n' {
		t.Error("Cache file should end with a trailing newline for prettier compliance")
	}
}

func TestActionCacheSortedEntries(t *testing.T) {
	// Create temporary directory for testing
	tmpDir := testutil.TempDir(t, "test-*")

	// Create cache and add entries in non-alphabetical order
	cache := NewActionCache(tmpDir)
	cache.Set("zzz/last-action", "v1", "sha111")
	cache.Set("actions/checkout", "v5", "sha222")
	cache.Set("mmm/middle-action", "v2", "sha333")
	cache.Set("actions/setup-node", "v4", "sha444")
	cache.Set("aaa/first-action", "v3", "sha555")

	// Save to disk
	err := cache.Save()
	if err != nil {
		t.Fatalf("Failed to save cache: %v", err)
	}

	// Read the file content
	cachePath := filepath.Join(tmpDir, ".github", "aw", CacheFileName)
	data, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatalf("Failed to read cache file: %v", err)
	}

	content := string(data)

	// Verify that entries appear in alphabetical order by checking their positions
	entries := []string{
		"aaa/first-action@v3",
		"actions/checkout@v5",
		"actions/setup-node@v4",
		"mmm/middle-action@v2",
		"zzz/last-action@v1",
	}

	lastPos := -1
	for _, entry := range entries {
		pos := indexOf(content, entry)
		if pos == -1 {
			t.Errorf("Entry %s not found in cache file", entry)
			continue
		}
		if pos < lastPos {
			t.Errorf("Entry %s appears before previous entry (not sorted)", entry)
		}
		lastPos = pos
	}

	// Also verify the file is valid JSON
	var loadedCache ActionCache
	err = json.Unmarshal(data, &loadedCache)
	if err != nil {
		t.Fatalf("Saved cache is not valid JSON: %v", err)
	}

	// Verify all entries are present
	if len(loadedCache.Entries) != 5 {
		t.Errorf("Expected 5 entries, got %d", len(loadedCache.Entries))
	}
}

// indexOf returns the index of substr in s, or -1 if not found
func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

func TestActionCacheEmptySaveDoesNotCreateFile(t *testing.T) {
	// Create temporary directory for testing
	tmpDir := testutil.TempDir(t, "test-*")

	// Create empty cache
	cache := NewActionCache(tmpDir)

	// Save empty cache
	err := cache.Save()
	if err != nil {
		t.Fatalf("Failed to save empty cache: %v", err)
	}

	// Verify file does NOT exist
	cachePath := filepath.Join(tmpDir, ".github", "aw", CacheFileName)
	if _, err := os.Stat(cachePath); !os.IsNotExist(err) {
		t.Error("Empty cache should not create a file")
	}
}

func TestActionCacheEmptySaveDeletesExistingFile(t *testing.T) {
	// Create temporary directory for testing
	tmpDir := testutil.TempDir(t, "test-*")

	// Create cache with entries and save
	cache := NewActionCache(tmpDir)
	cache.Set("actions/checkout", "v5", "abc123")
	err := cache.Save()
	if err != nil {
		t.Fatalf("Failed to save cache: %v", err)
	}

	// Verify file exists
	cachePath := filepath.Join(tmpDir, ".github", "aw", CacheFileName)
	if _, err := os.Stat(cachePath); os.IsNotExist(err) {
		t.Fatal("Cache file should exist after saving with entries")
	}

	// Clear cache and save again
	cache.Entries = make(map[string]ActionCacheEntry)
	cache.dirty = true // Mark as dirty so save actually processes the empty cache
	err = cache.Save()
	if err != nil {
		t.Fatalf("Failed to save empty cache: %v", err)
	}

	// Verify file is now deleted
	if _, err := os.Stat(cachePath); !os.IsNotExist(err) {
		t.Error("Empty cache should delete existing file")
	}
}

// TestActionCacheSaveWithContainerPinsOnly verifies that a cache with no Entries
// but with ContainerPins is still written to disk (not treated as "empty").
func TestActionCacheSaveWithContainerPinsOnly(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")
	cache := NewActionCache(tmpDir)

	// Add a container pin without any action entries
	cache.SetContainerPin("node:lts-alpine", "sha256:abc123", "node:lts-alpine@sha256:abc123")

	if err := cache.Save(); err != nil {
		t.Fatalf("Failed to save cache with container pins only: %v", err)
	}

	// File should exist because ContainerPins is non-empty
	cachePath := filepath.Join(tmpDir, ".github", "aw", CacheFileName)
	if _, err := os.Stat(cachePath); os.IsNotExist(err) {
		t.Error("Cache file should exist when ContainerPins is non-empty, even if Entries is empty")
	}

	// Reload and confirm the pin round-trips correctly
	reloaded := NewActionCache(tmpDir)
	if err := reloaded.Load(); err != nil {
		t.Fatalf("Failed to reload cache: %v", err)
	}
	pin, ok := reloaded.GetContainerPin("node:lts-alpine")
	if !ok {
		t.Fatal("Container pin should have been reloaded")
	}
	if pin.Digest != "sha256:abc123" {
		t.Errorf("Expected digest sha256:abc123, got %s", pin.Digest)
	}
}

// TestSetContainerPinEdgeCases verifies SetContainerPin behaviour with edge case inputs.
func TestSetContainerPinEdgeCases(t *testing.T) {
	tests := []struct {
		name        string
		image       string
		digest      string
		pinnedImage string
		wantOK      bool // whether GetContainerPin should find it
	}{
		{
			name:        "valid pin",
			image:       "node:lts-alpine",
			digest:      "sha256:abc123",
			pinnedImage: "node:lts-alpine@sha256:abc123",
			wantOK:      true,
		},
		{
			name:        "empty digest stored as-is",
			image:       "myimage:tag",
			digest:      "",
			pinnedImage: "myimage:tag@",
			wantOK:      true,
		},
		{
			name:        "empty pinned_image stored as-is",
			image:       "myimage:v1",
			digest:      "sha256:deadbeef",
			pinnedImage: "",
			wantOK:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := testutil.TempDir(t, "test-*")
			cache := NewActionCache(tmpDir)
			cache.SetContainerPin(tt.image, tt.digest, tt.pinnedImage)

			pin, ok := cache.GetContainerPin(tt.image)
			if ok != tt.wantOK {
				t.Errorf("GetContainerPin returned ok=%v, want %v", ok, tt.wantOK)
			}
			if ok {
				if pin.Digest != tt.digest {
					t.Errorf("Digest: got %q, want %q", pin.Digest, tt.digest)
				}
				if pin.PinnedImage != tt.pinnedImage {
					t.Errorf("PinnedImage: got %q, want %q", pin.PinnedImage, tt.pinnedImage)
				}
			}
		})
	}
}

// TestActionCacheDeduplication tests that duplicate entries are removed
func TestActionCacheDeduplication(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")
	cache := NewActionCache(tmpDir)

	// Add duplicate entries - same repo and SHA but different version references
	// Both point to the same version v5.0.1
	cache.Entries["actions/checkout@v5"] = ActionCacheEntry{
		Repo:    "actions/checkout",
		Version: "v5.0.1",
		SHA:     "abc123",
	}
	cache.Entries["actions/checkout@v5.0.1"] = ActionCacheEntry{
		Repo:    "actions/checkout",
		Version: "v5.0.1",
		SHA:     "abc123",
	}
	cache.dirty = true // Mark as dirty so save processes the cache

	// Verify we have 2 entries before deduplication
	if len(cache.Entries) != 2 {
		t.Fatalf("Expected 2 entries before deduplication, got %d", len(cache.Entries))
	}

	// Save (which triggers deduplication)
	err := cache.Save()
	if err != nil {
		t.Fatalf("Failed to save cache: %v", err)
	}

	// Verify only the more precise version remains
	if len(cache.Entries) != 1 {
		t.Errorf("Expected 1 entry after deduplication, got %d", len(cache.Entries))
	}

	// Verify the correct entry remains (v5.0.1 is more precise than v5)
	if _, exists := cache.Entries["actions/checkout@v5.0.1"]; !exists {
		t.Error("Expected actions/checkout@v5.0.1 to remain after deduplication")
	}

	if _, exists := cache.Entries["actions/checkout@v5"]; exists {
		t.Error("Expected actions/checkout@v5 to be removed after deduplication")
	}
}

// TestActionCacheDeduplicationMultipleActions tests deduplication with multiple actions
func TestActionCacheDeduplicationMultipleActions(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")
	cache := NewActionCache(tmpDir)

	// Add multiple actions with duplicates
	// actions/cache: v4 and v4.3.0 both point to same SHA and version
	cache.Entries["actions/cache@v4"] = ActionCacheEntry{
		Repo:    "actions/cache",
		Version: "v4.3.0",
		SHA:     "sha1",
	}
	cache.Entries["actions/cache@v4.3.0"] = ActionCacheEntry{
		Repo:    "actions/cache",
		Version: "v4.3.0",
		SHA:     "sha1",
	}

	// actions/setup-go: v6 and v6.1.0 both point to same SHA and version
	cache.Entries["actions/setup-go@v6"] = ActionCacheEntry{
		Repo:    "actions/setup-go",
		Version: "v6.1.0",
		SHA:     "sha2",
	}
	cache.Entries["actions/setup-go@v6.1.0"] = ActionCacheEntry{
		Repo:    "actions/setup-go",
		Version: "v6.1.0",
		SHA:     "sha2",
	}

	// actions/setup-node: no duplicates
	cache.Set("actions/setup-node", "v6.1.0", "sha3")

	// Since we set Entries directly, we need to mark as dirty for the test
	cache.dirty = true

	// Verify we have 5 entries before deduplication
	if len(cache.Entries) != 5 {
		t.Fatalf("Expected 5 entries before deduplication, got %d", len(cache.Entries))
	}

	// Save (which triggers deduplication)
	err := cache.Save()
	if err != nil {
		t.Fatalf("Failed to save cache: %v", err)
	}

	// Verify only 3 entries remain (one for each action)
	if len(cache.Entries) != 3 {
		t.Errorf("Expected 3 entries after deduplication, got %d", len(cache.Entries))
	}

	// Verify the correct entries remain
	if _, exists := cache.Entries["actions/cache@v4.3.0"]; !exists {
		t.Error("Expected actions/cache@v4.3.0 to remain")
	}
	if _, exists := cache.Entries["actions/cache@v4"]; exists {
		t.Error("Expected actions/cache@v4 to be removed")
	}

	if _, exists := cache.Entries["actions/setup-go@v6.1.0"]; !exists {
		t.Error("Expected actions/setup-go@v6.1.0 to remain")
	}
	if _, exists := cache.Entries["actions/setup-go@v6"]; exists {
		t.Error("Expected actions/setup-go@v6 to be removed")
	}

	if _, exists := cache.Entries["actions/setup-node@v6.1.0"]; !exists {
		t.Error("Expected actions/setup-node@v6.1.0 to remain")
	}
}

// TestActionCacheDeduplicationPreservesUnique tests that unique entries are preserved
func TestActionCacheDeduplicationPreservesUnique(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")
	cache := NewActionCache(tmpDir)

	// Add entries with different SHAs - no duplicates
	cache.Set("actions/checkout", "v5", "sha1")
	cache.Set("actions/checkout", "v5.0.1", "sha2") // Different SHA

	// Verify we have 2 entries before deduplication
	if len(cache.Entries) != 2 {
		t.Fatalf("Expected 2 entries before deduplication, got %d", len(cache.Entries))
	}

	// Save (which triggers deduplication)
	err := cache.Save()
	if err != nil {
		t.Fatalf("Failed to save cache: %v", err)
	}

	// Verify both entries remain (different SHAs)
	if len(cache.Entries) != 2 {
		t.Errorf("Expected 2 entries after deduplication (different SHAs), got %d", len(cache.Entries))
	}
}

// TestIsMorePreciseVersion tests the version precision comparison
func TestIsMorePreciseVersion(t *testing.T) {
	tests := []struct {
		name     string
		v1       string
		v2       string
		expected bool
	}{
		{
			name:     "v4.3.0 is more precise than v4",
			v1:       "v4.3.0",
			v2:       "v4",
			expected: true,
		},
		{
			name:     "v4 is less precise than v4.3.0",
			v1:       "v4",
			v2:       "v4.3.0",
			expected: false,
		},
		{
			name:     "v5.0.1 is more precise than v5",
			v1:       "v5.0.1",
			v2:       "v5",
			expected: true,
		},
		{
			name:     "v6.1.0 is more precise than v6",
			v1:       "v6.1.0",
			v2:       "v6",
			expected: true,
		},
		{
			name:     "v1.2.3 vs v1.2.3 (same precision)",
			v1:       "v1.2.3",
			v2:       "v1.2.3",
			expected: false,
		},
		{
			name:     "v1.2.10 vs v1.2.3 (same precision, lexicographic)",
			v1:       "v1.2.10",
			v2:       "v1.2.3",
			expected: false, // "v1.2.3" > "v1.2.10" lexicographically
		},
		{
			name:     "v8.0.0 is more precise than v8",
			v1:       "v8.0.0",
			v2:       "v8",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := semverutil.IsMorePreciseVersion(tt.v1, tt.v2)
			if result != tt.expected {
				t.Errorf("semverutil.IsMorePreciseVersion(%q, %q) = %v, want %v", tt.v1, tt.v2, result, tt.expected)
			}
		})
	}
}

// TestActionCacheDirtyFlag verifies that the cache dirty flag prevents unnecessary saves
func TestActionCacheDirtyFlag(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")
	cache := NewActionCache(tmpDir)

	// Initially, cache should be clean (no data)
	err := cache.Save()
	if err != nil {
		t.Fatalf("Failed to save empty cache: %v", err)
	}

	// Cache file should not exist (empty cache)
	cachePath := cache.GetCachePath()
	if _, err := os.Stat(cachePath); !os.IsNotExist(err) {
		t.Error("Empty cache should not create a file")
	}

	// Add an entry - should mark as dirty
	cache.Set("actions/checkout", "v5", "abc123")

	// Save should work now
	err = cache.Save()
	if err != nil {
		t.Fatalf("Failed to save dirty cache: %v", err)
	}

	// Cache file should now exist
	if _, err := os.Stat(cachePath); err != nil {
		t.Errorf("Cache file should exist after save: %v", err)
	}

	// Save again without changes - should skip (cache is clean)
	// We can't directly verify the skip, but we can ensure it doesn't error
	err = cache.Save()
	if err != nil {
		t.Fatalf("Failed to save clean cache: %v", err)
	}

	// Add another entry - should mark as dirty again
	cache.Set("actions/setup-node", "v4", "def456")

	// Save should work
	err = cache.Save()
	if err != nil {
		t.Fatalf("Failed to save dirty cache after modification: %v", err)
	}
}

// TestActionCacheFindEntryBySHA tests finding cache entries by SHA
func TestActionCacheFindEntryBySHA(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")
	cache := NewActionCache(tmpDir)

	// Add entries with same SHA
	cache.Set("actions/github-script", "v9", "3a2844b7e9c422d3c10d287c895573f7108da1b3")
	cache.Set("actions/github-script", "v9.0.0", "3a2844b7e9c422d3c10d287c895573f7108da1b3")

	// Find entry by SHA
	entry, found := cache.FindEntryBySHA("actions/github-script", "3a2844b7e9c422d3c10d287c895573f7108da1b3")
	if !found {
		t.Fatal("Expected to find entry by SHA")
	}

	// Should find one of the entries (either v9 or v9.0.0)
	if entry.Repo != "actions/github-script" {
		t.Errorf("Expected repo 'actions/github-script', got '%s'", entry.Repo)
	}
	if entry.SHA != "3a2844b7e9c422d3c10d287c895573f7108da1b3" {
		t.Errorf("Expected SHA to match")
	}
	if entry.Version != "v9" && entry.Version != "v9.0.0" {
		t.Errorf("Expected version 'v9' or 'v9.0.0', got '%s'", entry.Version)
	}

	// Test not found case
	_, found = cache.FindEntryBySHA("actions/unknown", "unknown-sha")
	if found {
		t.Error("Expected not to find entry with unknown SHA")
	}

	// Test different repo with same SHA
	_, found = cache.FindEntryBySHA("actions/checkout", "3a2844b7e9c422d3c10d287c895573f7108da1b3")
	if found {
		t.Error("Expected not to find entry for different repo")
	}
}

// TestActionCacheInputs tests caching and retrieving action inputs
func TestActionCacheInputs(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")
	cache := NewActionCache(tmpDir)
	cache.Set("owner/repo", "v1", "abc123sha456789012345678901234567890123")

	// Initially no inputs cached
	inputs, ok := cache.GetInputs("owner/repo", "v1")
	if ok {
		t.Error("Expected no cached inputs, got some")
	}
	if inputs != nil {
		t.Error("Expected nil inputs, got non-nil")
	}

	// Store inputs
	toCache := map[string]*ActionYAMLInput{
		"labels": {Description: "Labels to add.", Required: true},
		"number": {Description: "PR number."},
	}
	cache.SetInputs("owner/repo", "v1", toCache)

	// Retrieve inputs
	inputs, ok = cache.GetInputs("owner/repo", "v1")
	if !ok {
		t.Fatal("Expected cached inputs to exist")
	}
	if len(inputs) != 2 {
		t.Errorf("Expected 2 inputs, got %d", len(inputs))
	}
	if inputs["labels"] == nil || !inputs["labels"].Required {
		t.Error("Expected 'labels' input to be required")
	}

	// Set with the SAME SHA - inputs must be preserved
	cache.Set("owner/repo", "v1", "abc123sha456789012345678901234567890123")
	inputs, ok = cache.GetInputs("owner/repo", "v1")
	if !ok {
		t.Error("Expected inputs to survive Set() with same SHA")
	}
	if len(inputs) != 2 {
		t.Errorf("Expected inputs to be preserved after Set() with same SHA, got %d", len(inputs))
	}

	// Set with a NEW SHA - inputs must be cleared (stale inputs no longer match pinned commit)
	cache.Set("owner/repo", "v1", "newsha456789012345678901234567890123456")
	inputs, ok = cache.GetInputs("owner/repo", "v1")
	if ok {
		t.Error("Expected inputs to be cleared after Set() with new SHA")
	}
	if inputs != nil {
		t.Error("Expected nil inputs after SHA change, got non-nil")
	}

	// SetInputs on a missing key is now a no-op — it does not create empty-SHA entries.
	cache.SetInputs("owner/repo", "v99", map[string]*ActionYAMLInput{
		"x": {Description: "x"},
	})
	inputs, ok = cache.GetInputs("owner/repo", "v99")
	if ok {
		t.Error("Expected SetInputs on missing key to be a no-op (not create entry)")
	}
	if inputs != nil {
		t.Error("Expected nil inputs for missing entry")
	}
}

// TestPruneStaleGHAWEntries tests that stale gh-aw-actions entries are pruned
func TestPruneStaleGHAWEntries(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")
	cache := NewActionCache(tmpDir)

	// Set up a scenario that mirrors the bug:
	// - An old setup action entry from a previous compiler version
	// - A current setup action entry from the current compiler version
	// - A non-gh-aw-actions entry that should be preserved
	cache.Set("github/gh-aw-actions/setup", "v0.67.1", "sha_old")
	cache.Set("github/gh-aw-actions/setup", "v0.67.3", "sha_new")
	cache.Set("actions/checkout", "v5", "sha_checkout")

	if len(cache.Entries) != 3 {
		t.Fatalf("Expected 3 entries before pruning, got %d", len(cache.Entries))
	}

	// Prune stale entries for version v0.67.3
	cache.PruneStaleGHAWEntries("v0.67.3", "github/gh-aw-actions")

	// Should have 2 entries: current setup + checkout
	if len(cache.Entries) != 2 {
		t.Errorf("Expected 2 entries after pruning, got %d", len(cache.Entries))
	}

	// The old setup entry should be gone
	if _, exists := cache.Entries["github/gh-aw-actions/setup@v0.67.1"]; exists {
		t.Error("Expected stale setup@v0.67.1 to be pruned")
	}

	// The current setup entry should remain
	if _, exists := cache.Entries["github/gh-aw-actions/setup@v0.67.3"]; !exists {
		t.Error("Expected current setup@v0.67.3 to remain")
	}

	// Non-gh-aw-actions entries should remain
	if _, exists := cache.Entries["actions/checkout@v5"]; !exists {
		t.Error("Expected actions/checkout@v5 to remain")
	}
}

// TestPruneStaleGHAWEntriesMultipleActions tests pruning with multiple gh-aw-actions
func TestPruneStaleGHAWEntriesMultipleActions(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")
	cache := NewActionCache(tmpDir)

	// Multiple gh-aw-actions at the old version, plus one at the current version
	cache.Set("github/gh-aw-actions/setup", "v0.67.1", "sha1")
	cache.Set("github/gh-aw-actions/setup", "v0.67.3", "sha2")
	cache.Set("github/gh-aw-actions/create-issue", "v0.67.1", "sha3")
	cache.Set("github/gh-aw-actions/create-issue", "v0.67.3", "sha4")
	cache.Set("actions/checkout", "v5", "sha5")

	cache.PruneStaleGHAWEntries("v0.67.3", "github/gh-aw-actions")

	// Should keep only the v0.67.3 entries + checkout
	if len(cache.Entries) != 3 {
		t.Errorf("Expected 3 entries after pruning, got %d", len(cache.Entries))
	}

	if _, exists := cache.Entries["github/gh-aw-actions/setup@v0.67.1"]; exists {
		t.Error("Expected stale setup@v0.67.1 to be pruned")
	}
	if _, exists := cache.Entries["github/gh-aw-actions/create-issue@v0.67.1"]; exists {
		t.Error("Expected stale create-issue@v0.67.1 to be pruned")
	}
}

// TestPruneStaleGHAWEntriesNoOp tests that pruning is a no-op for non-release or empty versions
func TestPruneStaleGHAWEntriesNoOp(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")
	cache := NewActionCache(tmpDir)

	cache.Set("github/gh-aw-actions/setup", "v0.67.1", "sha1")
	cache.Set("actions/checkout", "v5", "sha2")

	// Should be a no-op for "dev" version (not a release)
	cache.PruneStaleGHAWEntries("dev", "github/gh-aw-actions")
	if len(cache.Entries) != 2 {
		t.Errorf("Expected 2 entries (no pruning for dev), got %d", len(cache.Entries))
	}

	// Should be a no-op for empty version
	cache.PruneStaleGHAWEntries("", "github/gh-aw-actions")
	if len(cache.Entries) != 2 {
		t.Errorf("Expected 2 entries (no pruning for empty version), got %d", len(cache.Entries))
	}

	// Should be a no-op for empty prefix
	cache.PruneStaleGHAWEntries("v0.67.3", "")
	if len(cache.Entries) != 2 {
		t.Errorf("Expected 2 entries (no pruning for empty prefix), got %d", len(cache.Entries))
	}

	// Should be a no-op for dirty dev builds (e.g., "abc123-dirty")
	cache.PruneStaleGHAWEntries("abc123-dirty", "github/gh-aw-actions")
	if len(cache.Entries) != 2 {
		t.Errorf("Expected 2 entries (no pruning for dirty build), got %d", len(cache.Entries))
	}

	// Should be a no-op for dirty release builds (e.g., "v0.67.3-dirty")
	cache.PruneStaleGHAWEntries("v0.67.3-dirty", "github/gh-aw-actions")
	if len(cache.Entries) != 2 {
		t.Errorf("Expected 2 entries (no pruning for dirty release build), got %d", len(cache.Entries))
	}
}

// TestActionCacheReleasedAt verifies that GetReleasedAt / SetReleasedAt
// round-trip correctly and are preserved across a Save/Load cycle.
func TestActionCacheReleasedAt(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")
	cache := NewActionCache(tmpDir)
	cache.Set("owner/action", "v1", "sha1234567890123456789012345678901234567890")

	// No date cached yet
	_, ok := cache.GetReleasedAt("owner/action", "v1")
	if ok {
		t.Error("Expected no release date before SetReleasedAt")
	}

	// Store a date
	ts := time.Date(2024, 3, 15, 12, 0, 0, 0, time.UTC)
	cache.SetReleasedAt("owner/action", "v1", ts)

	got, ok := cache.GetReleasedAt("owner/action", "v1")
	if !ok {
		t.Fatal("Expected release date after SetReleasedAt")
	}
	if !got.Equal(ts) {
		t.Errorf("GetReleasedAt() = %v, want %v", got, ts)
	}

	// Persist to disk and reload
	if err := cache.Save(); err != nil {
		t.Fatalf("Failed to save: %v", err)
	}
	cache2 := NewActionCache(tmpDir)
	if err := cache2.Load(); err != nil {
		t.Fatalf("Failed to load: %v", err)
	}
	got2, ok2 := cache2.GetReleasedAt("owner/action", "v1")
	if !ok2 {
		t.Fatal("Expected release date to survive save/load")
	}
	if !got2.Equal(ts) {
		t.Errorf("After reload: GetReleasedAt() = %v, want %v", got2, ts)
	}
}

// TestActionCacheReleasedAtPreservedOnSHAMatch verifies that ReleasedAt is
// preserved by Set when the SHA does not change.
func TestActionCacheReleasedAtPreservedOnSHAMatch(t *testing.T) {
	cache := NewActionCache(t.TempDir())
	ts := time.Date(2024, 5, 1, 0, 0, 0, 0, time.UTC)
	cache.Set("owner/action", "v2", "samesha1234567890123456789012345678901234")
	cache.SetReleasedAt("owner/action", "v2", ts)

	// Set again with the same SHA — ReleasedAt must be preserved.
	cache.Set("owner/action", "v2", "samesha1234567890123456789012345678901234")
	got, ok := cache.GetReleasedAt("owner/action", "v2")
	if !ok || !got.Equal(ts) {
		t.Errorf("ReleasedAt should be preserved when SHA unchanged; got %v, ok=%v", got, ok)
	}
}

// TestActionCacheReleasedAtClearedOnSHAChange verifies that ReleasedAt is
// cleared by Set when the SHA changes (stale date no longer valid).
func TestActionCacheReleasedAtClearedOnSHAChange(t *testing.T) {
	cache := NewActionCache(t.TempDir())
	ts := time.Date(2024, 5, 1, 0, 0, 0, 0, time.UTC)
	cache.Set("owner/action", "v2", "oldsha1234567890123456789012345678901234a")
	cache.SetReleasedAt("owner/action", "v2", ts)

	// Set with a different SHA — ReleasedAt must be cleared.
	cache.Set("owner/action", "v2", "newsha1234567890123456789012345678901234b")
	_, ok := cache.GetReleasedAt("owner/action", "v2")
	if ok {
		t.Error("ReleasedAt should be cleared when SHA changes")
	}
}

// TestPruneOrphanedEntries verifies that PruneOrphanedEntries removes entries
// whose keys are absent from the referenced set, while preserving referenced ones.
func TestPruneOrphanedEntries(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")
	cache := NewActionCache(tmpDir)

	// Simulate a version bump: v1.7.2 was old, v1.9.1 is now used.
	cache.Set("microsoft/apm-action", "v1.7.2", "sha_old")
	cache.Set("microsoft/apm-action", "v1.9.1", "sha_new")
	cache.Set("actions/checkout", "v4", "sha_checkout")

	if len(cache.Entries) != 3 {
		t.Fatalf("Expected 3 entries before pruning, got %d", len(cache.Entries))
	}

	// Only v1.9.1 and checkout are referenced by compiled workflows.
	referenced := map[string]struct{}{
		"microsoft/apm-action@v1.9.1": {},
		"actions/checkout@v4":         {},
	}
	pruned := cache.PruneOrphanedEntries(referenced)

	if pruned != 1 {
		t.Errorf("Expected 1 pruned entry, got %d", pruned)
	}
	if len(cache.Entries) != 2 {
		t.Errorf("Expected 2 entries after pruning, got %d", len(cache.Entries))
	}
	if _, exists := cache.Entries["microsoft/apm-action@v1.7.2"]; exists {
		t.Error("Expected orphaned entry microsoft/apm-action@v1.7.2 to be removed")
	}
	if _, exists := cache.Entries["microsoft/apm-action@v1.9.1"]; !exists {
		t.Error("Expected referenced entry microsoft/apm-action@v1.9.1 to remain")
	}
	if _, exists := cache.Entries["actions/checkout@v4"]; !exists {
		t.Error("Expected referenced entry actions/checkout@v4 to remain")
	}
}

// TestPruneOrphanedEntries_EmptyReferenced verifies that passing an empty
// referenced set is a no-op (safe guard: an empty set most likely means no
// compilation happened, not that all entries are orphaned).
func TestPruneOrphanedEntries_EmptyReferenced(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")
	cache := NewActionCache(tmpDir)

	cache.Set("actions/checkout", "v4", "sha1")
	cache.Set("actions/setup-node", "v4", "sha2")

	pruned := cache.PruneOrphanedEntries(map[string]struct{}{})

	if pruned != 0 {
		t.Errorf("Expected 0 pruned entries (empty referenced set is a no-op), got %d", pruned)
	}
	if len(cache.Entries) != 2 {
		t.Errorf("Expected 2 entries (unchanged), got %d", len(cache.Entries))
	}
}

// TestPruneOrphanedEntries_NoneOrphaned verifies that no entries are removed
// when all cached entries are referenced.
func TestPruneOrphanedEntries_NoneOrphaned(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")
	cache := NewActionCache(tmpDir)

	cache.Set("actions/checkout", "v4", "sha1")
	cache.Set("actions/setup-node", "v4", "sha2")

	referenced := map[string]struct{}{
		"actions/checkout@v4":   {},
		"actions/setup-node@v4": {},
	}
	pruned := cache.PruneOrphanedEntries(referenced)

	if pruned != 0 {
		t.Errorf("Expected 0 pruned entries, got %d", pruned)
	}
	if len(cache.Entries) != 2 {
		t.Errorf("Expected 2 entries (unchanged), got %d", len(cache.Entries))
	}
}

// TestPruneOrphanedEntries_AllOrphaned verifies that all entries are removed
// when none are referenced (but referenced set is non-empty).
func TestPruneOrphanedEntries_AllOrphaned(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")
	cache := NewActionCache(tmpDir)

	// Use actions that are NOT runtime-managed or compiler-generated
	cache.Set("microsoft/apm-action", "v1.7.2", "sha1")
	cache.Set("cli/gh-extension-precompile", "v2.1.0", "sha2")

	// Referenced set is non-empty but contains neither of the cached entries.
	referenced := map[string]struct{}{
		"some/other-action@v1": {},
	}
	pruned := cache.PruneOrphanedEntries(referenced)

	// Both entries should be pruned (neither is compiler-generated or runtime-managed)
	if pruned != 2 {
		t.Errorf("Expected 2 pruned entries, got %d", pruned)
	}
	if len(cache.Entries) != 0 {
		t.Errorf("Expected 0 entries after pruning all orphans, got %d", len(cache.Entries))
	}
}

// TestPruneOrphanedEntries_PreservesCompilerGenerated verifies that compiler-generated
// actions are never pruned, even when not in the referenced set.
func TestPruneOrphanedEntries_PreservesCompilerGenerated(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")
	cache := NewActionCache(tmpDir)

	// Add compiler-generated actions, runtime-managed actions, and a regular action
	cache.Set("actions/cache/save", "v4", "sha_cache_save")
	cache.Set("actions/checkout", "v4", "sha_checkout")
	cache.Set("github/codeql-action/upload-sarif", "v4", "sha_codeql")
	cache.Set("actions/setup-node", "v6", "sha_node")     // runtime-managed
	cache.Set("actions/setup-python", "v5", "sha_python") // runtime-managed
	cache.Set("ruby/setup-ruby", "v1", "sha_ruby")        // runtime-managed
	cache.Set("microsoft/apm-action", "v1.7.2", "sha_old")

	if len(cache.Entries) != 7 {
		t.Fatalf("Expected 7 entries before pruning, got %d", len(cache.Entries))
	}

	// Only reference microsoft/apm-action, but not the compiler-generated or runtime-managed ones
	referenced := map[string]struct{}{
		"microsoft/apm-action@v1.7.2": {},
	}
	pruned := cache.PruneOrphanedEntries(referenced)

	// Should not prune compiler-generated or runtime-managed actions
	if pruned != 0 {
		t.Errorf("Expected 0 pruned entries (compiler-generated and runtime-managed actions preserved), got %d", pruned)
	}
	if len(cache.Entries) != 7 {
		t.Errorf("Expected 7 entries after pruning (all preserved), got %d", len(cache.Entries))
	}

	// Verify compiler-generated actions are preserved
	if _, exists := cache.Entries["actions/cache/save@v4"]; !exists {
		t.Error("Expected compiler-generated actions/cache/save@v4 to be preserved")
	}
	if _, exists := cache.Entries["actions/checkout@v4"]; !exists {
		t.Error("Expected compiler-generated actions/checkout@v4 to be preserved")
	}
	if _, exists := cache.Entries["github/codeql-action/upload-sarif@v4"]; !exists {
		t.Error("Expected compiler-generated github/codeql-action/upload-sarif@v4 to be preserved")
	}

	// Verify runtime-managed actions are preserved
	if _, exists := cache.Entries["actions/setup-node@v6"]; !exists {
		t.Error("Expected runtime-managed actions/setup-node@v6 to be preserved")
	}
	if _, exists := cache.Entries["actions/setup-python@v5"]; !exists {
		t.Error("Expected runtime-managed actions/setup-python@v5 to be preserved")
	}
	if _, exists := cache.Entries["ruby/setup-ruby@v1"]; !exists {
		t.Error("Expected runtime-managed ruby/setup-ruby@v1 to be preserved")
	}
}

// TestActionCache_Set_EmptySHARejected verifies that calling Set with an empty SHA
// is a no-op: the entry must not be stored in the cache, preventing an invalid
// (SHA-less) pin from being persisted to actions-lock.json.
func TestActionCache_Set_EmptySHARejected(t *testing.T) {
	cache := NewActionCache(t.TempDir())

	// Calling Set with an empty SHA must not create any entry.
	if ok := cache.Set("docker/login-action", "v3.1.0", ""); ok {
		t.Error("Set with empty SHA must report failure")
	}

	if _, exists := cache.Entries["docker/login-action@v3.1.0"]; exists {
		t.Error("Set with empty SHA must not create a cache entry")
	}
	if len(cache.Entries) != 0 {
		t.Errorf("Expected 0 entries after Set with empty SHA, got %d", len(cache.Entries))
	}

	// A subsequent Set with a real SHA must succeed normally.
	const sha = "abc1234567890123456789012345678901234567"
	if ok := cache.Set("docker/login-action", "v3.1.0", sha); !ok {
		t.Fatal("Set with valid SHA must report success")
	}
	entry, exists := cache.Entries["docker/login-action@v3.1.0"]
	if !exists {
		t.Fatal("Set with valid SHA did not create a cache entry")
	}
	if entry.SHA != sha {
		t.Errorf("entry SHA = %q, want %q", entry.SHA, sha)
	}
}

func TestActionCache_SettersNoOpOnEmptySHAEntry(t *testing.T) {
	cache := NewActionCache(t.TempDir())
	key := formatActionCacheKey("owner/action", "v9")
	cache.Entries[key] = ActionCacheEntry{
		Repo:    "owner/action",
		Version: "v9",
	}
	cache.dirty = false

	cache.SetReleasedAt("owner/action", "v9", time.Now())
	cache.SetInputs("owner/action", "v9", map[string]*ActionYAMLInput{
		"input": {Description: "desc"},
	})
	cache.SetActionDescription("owner/action", "v9", "description")

	entry := cache.Entries[key]
	if entry.ReleasedAt != nil {
		t.Error("Expected ReleasedAt to remain nil for empty-SHA entry")
	}
	if entry.Inputs != nil {
		t.Error("Expected Inputs to remain nil for empty-SHA entry")
	}
	if entry.ActionDescription != "" {
		t.Error("Expected ActionDescription to remain empty for empty-SHA entry")
	}
	if cache.dirty {
		t.Error("Expected cache to remain clean when setters are no-op on empty-SHA entry")
	}
}

func TestActionCache_SetInputsNilNoOp(t *testing.T) {
	cache := NewActionCache(t.TempDir())
	cache.Set("owner/action", "v1", "sha1234567890123456789012345678901234567890")
	cache.SetInputs("owner/action", "v1", map[string]*ActionYAMLInput{
		"existing": {Description: "existing"},
	})
	cache.dirty = false

	cache.SetInputs("owner/action", "v1", nil)

	inputs, ok := cache.GetInputs("owner/action", "v1")
	if !ok {
		t.Fatal("Expected existing inputs to remain cached")
	}
	if _, exists := inputs["existing"]; !exists {
		t.Error("Expected existing input to be preserved after nil SetInputs")
	}
	if cache.dirty {
		t.Error("Expected cache to remain clean after nil SetInputs")
	}
}

func TestActionCache_FindNewestEntryForRepo(t *testing.T) {
	cache := NewActionCache(t.TempDir())
	cache.Set("owner/action", "v999", "major-sha")
	cache.Set("owner/action", "v999.0.0", "precise-sha")
	cache.Set("owner/action", "zzz", "branch-sha")
	cache.Set("other/action", "v1000.0.0", "other-sha")

	check := func(repo, wantVersion, wantSHA string) {
		t.Helper()
		key, entry, found := cache.FindNewestEntryForRepo(repo)
		if !found || entry.Version != wantVersion || entry.SHA != wantSHA || key != formatActionCacheKey(repo, wantVersion) {
			t.Fatalf("FindNewestEntryForRepo(%q) = %q, %+v, %v; want %s@%s (%s)", repo, key, entry, found, repo, wantVersion, wantSHA)
		}
	}
	for range 100 {
		check("owner/action", "v999.0.0", "precise-sha")
	}
	check("other/action", "v1000.0.0", "other-sha")
	if _, _, found := cache.FindNewestEntryForRepo("missing/action"); found {
		t.Fatal("expected no entry for missing repository")
	}

	cache.Set("owner/action", "v1000.0.0", "newest-sha")
	check("owner/action", "v1000.0.0", "newest-sha")
	cache.Set("owner/action", "v1000.0.0", "updated-sha")
	check("owner/action", "v1000.0.0", "updated-sha")
	cache.Delete("owner/action", "v1000.0.0")
	check("owner/action", "v999.0.0", "precise-sha")
	cache.DeleteByKey("owner/action@v999.0.0")
	check("owner/action", "v999", "major-sha")

	if err := cache.Save(); err != nil {
		t.Fatal(err)
	}
	cache.Set("owner/action", "v999", "temporary-sha")
	check("owner/action", "v999", "temporary-sha")
	if err := cache.Load(); err != nil {
		t.Fatal(err)
	}
	check("owner/action", "v999", "major-sha")
}
