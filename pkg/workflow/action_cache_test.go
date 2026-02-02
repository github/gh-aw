//go:build !integration

package workflow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

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
			result := isMorePreciseVersion(tt.v1, tt.v2)
			if result != tt.expected {
				t.Errorf("isMorePreciseVersion(%q, %q) = %v, want %v", tt.v1, tt.v2, result, tt.expected)
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
	cache.Set("actions/github-script", "v8", "ed597411d8f924073f98dfc5c65a23a2325f34cd")
	cache.Set("actions/github-script", "v8.0.0", "ed597411d8f924073f98dfc5c65a23a2325f34cd")

	// Find entry by SHA
	entry, found := cache.FindEntryBySHA("actions/github-script", "ed597411d8f924073f98dfc5c65a23a2325f34cd")
	if !found {
		t.Fatal("Expected to find entry by SHA")
	}

	// Should find one of the entries (either v8 or v8.0.0)
	if entry.Repo != "actions/github-script" {
		t.Errorf("Expected repo 'actions/github-script', got '%s'", entry.Repo)
	}
	if entry.SHA != "ed597411d8f924073f98dfc5c65a23a2325f34cd" {
		t.Errorf("Expected SHA to match")
	}
	if entry.Version != "v8" && entry.Version != "v8.0.0" {
		t.Errorf("Expected version 'v8' or 'v8.0.0', got '%s'", entry.Version)
	}

	// Test not found case
	_, found = cache.FindEntryBySHA("actions/unknown", "unknown-sha")
	if found {
		t.Error("Expected not to find entry with unknown SHA")
	}

	// Test different repo with same SHA
	_, found = cache.FindEntryBySHA("actions/checkout", "ed597411d8f924073f98dfc5c65a23a2325f34cd")
	if found {
		t.Error("Expected not to find entry for different repo")
	}
}
