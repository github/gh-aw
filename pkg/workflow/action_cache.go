package workflow

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var actionCacheLog = logger.New("workflow:action_cache")

const (
	// CacheFileName is the name of the cache file in .github/aw/.
	CacheFileName = "actions-lock.json"
)

// ActionCacheEntry represents a cached action pin resolution.
type ActionCacheEntry struct {
	Repo    string `json:"repo"`
	Version string `json:"version"`
	SHA     string `json:"sha"`
}

// ActionCache manages cached action pin resolutions.
type ActionCache struct {
	Entries map[string]ActionCacheEntry `json:"entries"` // key: "repo@version"
	path    string
	dirty   bool // tracks if cache has unsaved changes
}

// NewActionCache creates a new action cache instance
func NewActionCache(repoRoot string) *ActionCache {
	cachePath := filepath.Join(repoRoot, ".github", "aw", CacheFileName)
	actionCacheLog.Printf("Creating action cache with path: %s", cachePath)
	return &ActionCache{
		Entries: make(map[string]ActionCacheEntry),
		path:    cachePath,
		// dirty is initialized to false (zero value)
	}
}

// Load loads the cache from disk
func (c *ActionCache) Load() error {
	actionCacheLog.Printf("Loading action cache from: %s", c.path)
	data, err := os.ReadFile(c.path)
	if err != nil {
		if os.IsNotExist(err) {
			// Cache file doesn't exist yet, that's OK
			actionCacheLog.Print("Cache file does not exist, starting with empty cache")
			return nil
		}
		actionCacheLog.Printf("Failed to read cache file: %v", err)
		return err
	}

	if err := json.Unmarshal(data, c); err != nil {
		actionCacheLog.Printf("Failed to unmarshal cache data: %v", err)
		return err
	}

	// Mark cache as clean after successful load (it matches disk state)
	c.dirty = false

	actionCacheLog.Printf("Successfully loaded cache with %d entries", len(c.Entries))
	return nil
}

// Save saves the cache to disk with sorted entries
// If the cache is empty, the file is not created or is deleted if it exists
// Deduplicates entries by keeping only the most precise version reference for each repo+SHA combination
// Only saves if the cache has been modified (dirty flag is true)
func (c *ActionCache) Save() error {
	// Skip saving if cache hasn't been modified
	if !c.dirty {
		actionCacheLog.Printf("Cache is clean (no changes), skipping save")
		return nil
	}

	actionCacheLog.Printf("Saving action cache to: %s with %d entries", c.path, len(c.Entries))

	// If cache is empty, skip saving and delete the file if it exists
	if len(c.Entries) == 0 {
		actionCacheLog.Print("Cache is empty, skipping file creation")
		// Remove the file if it exists
		if _, err := os.Stat(c.path); err == nil {
			actionCacheLog.Printf("Removing existing empty cache file: %s", c.path)
			if err := os.Remove(c.path); err != nil {
				actionCacheLog.Printf("Failed to remove empty cache file: %v", err)
				return err
			}
		}
		c.dirty = false
		return nil
	}

	// Deduplicate entries before saving
	c.deduplicateEntries()

	// Ensure directory exists
	dir := filepath.Dir(c.path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		actionCacheLog.Printf("Failed to create cache directory: %v", err)
		return err
	}

	// Marshal with sorted entries
	data, err := c.marshalSorted()
	if err != nil {
		actionCacheLog.Printf("Failed to marshal cache data: %v", err)
		return err
	}

	// Add trailing newline for prettier compliance
	data = append(data, '\n')

	if err := os.WriteFile(c.path, data, 0644); err != nil {
		actionCacheLog.Printf("Failed to write cache file: %v", err)
		return err
	}

	actionCacheLog.Print("Successfully saved action cache")
	c.dirty = false
	return nil
}

// marshalSorted marshals the cache with entries sorted by key
func (c *ActionCache) marshalSorted() ([]byte, error) {
	// Extract and sort the keys
	keys := make([]string, 0, len(c.Entries))
	for key := range c.Entries {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	// Manually construct JSON with sorted keys
	var result []byte
	result = append(result, []byte("{\n  \"entries\": {\n")...)

	for i, key := range keys {
		entry := c.Entries[key]

		// Marshal the entry
		entryJSON, err := json.MarshalIndent(entry, "    ", "  ")
		if err != nil {
			return nil, err
		}

		// Add the key and entry
		result = append(result, []byte("    \""+key+"\": ")...)
		result = append(result, entryJSON...)

		// Add comma if not the last entry
		if i < len(keys)-1 {
			result = append(result, ',')
		}
		result = append(result, '\n')
	}

	result = append(result, []byte("  }\n}")...)
	return result, nil
}

// Get retrieves a cached entry if it exists
func (c *ActionCache) Get(repo, version string) (string, bool) {
	key := formatActionCacheKey(repo, version)
	entry, exists := c.Entries[key]
	if !exists {
		actionCacheLog.Printf("Cache miss for key=%s", key)
		return "", false
	}

	actionCacheLog.Printf("Cache hit for key=%s, sha=%s", key, entry.SHA)
	return entry.SHA, true
}

// FindEntryBySHA finds a cache entry with the given repo and SHA
// Returns the entry and true if found, or empty entry and false if not found
func (c *ActionCache) FindEntryBySHA(repo, sha string) (ActionCacheEntry, bool) {
	for key, entry := range c.Entries {
		if entry.Repo == repo && entry.SHA == sha {
			actionCacheLog.Printf("Found cache entry for %s with SHA %s: %s", repo, sha[:8], key)
			return entry, true
		}
	}
	return ActionCacheEntry{}, false
}

// Set stores a new cache entry
func (c *ActionCache) Set(repo, version, sha string) {
	key := formatActionCacheKey(repo, version)

	// Check if there are existing entries with the same repo+SHA but different version
	for existingKey, entry := range c.Entries {
		if entry.Repo == repo && entry.SHA == sha && entry.Version != version {
			// Truncate SHA for logging (handle short SHAs in tests)
			shortSHA := sha
			if len(sha) > 8 {
				shortSHA = sha[:8]
			}
			actionCacheLog.Printf("WARNING: Adding cache entry %s with SHA %s that already exists as %s",
				key, shortSHA, existingKey)
			actionCacheLog.Printf("This may cause version comment flipping in lock files. Consider using consistent version tags.")
		}
	}

	actionCacheLog.Printf("Setting cache entry: key=%s, sha=%s", key, sha)
	c.Entries[key] = ActionCacheEntry{
		Repo:    repo,
		Version: version,
		SHA:     sha,
	}
	c.dirty = true // Mark cache as modified
}

// GetCachePath returns the path to the cache file
func (c *ActionCache) GetCachePath() string {
	return c.path
}

// deduplicateEntries removes duplicate entries by keeping only the most precise version reference
// for each repo+SHA combination. For example, if both "actions/cache@v4" and "actions/cache@v4.3.0"
// point to the same SHA and version, only "actions/cache@v4.3.0" is kept.
func (c *ActionCache) deduplicateEntries() {
	// Group entries by repo+SHA
	type entryKey struct {
		repo string
		sha  string
	}
	groups := make(map[entryKey][]string)

	for key, entry := range c.Entries {
		ek := entryKey{repo: entry.Repo, sha: entry.SHA}
		groups[ek] = append(groups[ek], key)
	}

	// For each group with multiple entries, keep only the most precise one
	var toDelete []string
	var deduplicationDetails []string // Track details for user-friendly message

	for ek, keys := range groups {
		if len(keys) <= 1 {
			continue
		}

		// Truncate SHA for logging (handle short SHAs in tests)
		shortSHA := ek.sha
		if len(ek.sha) > 8 {
			shortSHA = ek.sha[:8]
		}
		actionCacheLog.Printf("Found %d cache entries for %s with SHA %s", len(keys), ek.repo, shortSHA)

		// Find the most precise version reference
		// Extract the version reference from each key (format: "repo@versionRef")
		type keyInfo struct {
			key        string
			versionRef string
		}
		keyInfos := make([]keyInfo, len(keys))
		for i, key := range keys {
			parts := strings.SplitN(key, "@", 2)
			versionRef := ""
			if len(parts) == 2 {
				versionRef = parts[1]
			}
			keyInfos[i] = keyInfo{key: key, versionRef: versionRef}
		}

		// Sort by version precision (most precise first)
		sort.Slice(keyInfos, func(i, j int) bool {
			return isMorePreciseVersion(keyInfos[i].versionRef, keyInfos[j].versionRef)
		})

		// Keep the most precise version, mark others for deletion
		keepVersion := keyInfos[0].versionRef
		var removedVersions []string
		for i := 1; i < len(keyInfos); i++ {
			toDelete = append(toDelete, keyInfos[i].key)
			removedVersions = append(removedVersions, keyInfos[i].versionRef)
			actionCacheLog.Printf("Deduplicating: keeping %s, removing %s", keyInfos[0].key, keyInfos[i].key)
		}

		// Build user-friendly message
		detail := fmt.Sprintf("%s: kept %s, removed %s", ek.repo, keepVersion, strings.Join(removedVersions, ", "))
		deduplicationDetails = append(deduplicationDetails, detail)
	}

	// Delete the less precise entries
	for _, key := range toDelete {
		delete(c.Entries, key)
	}

	if len(toDelete) > 0 {
		actionCacheLog.Printf("Deduplicated %d entries, %d entries remaining", len(toDelete), len(c.Entries))
		// Log detailed deduplication info at verbose level
		for _, detail := range deduplicationDetails {
			actionCacheLog.Printf("Deduplication detail: %s", detail)
		}
	}
}

// isMorePreciseVersion returns true if v1 is more precise than v2
// For example: "v4.3.0" is more precise than "v4"
func isMorePreciseVersion(v1, v2 string) bool {
	// Count the number of dots in each version string
	// More dots means more precision
	dots1 := strings.Count(v1, ".")
	dots2 := strings.Count(v2, ".")

	if dots1 != dots2 {
		return dots1 > dots2
	}

	// If same number of dots, compare lexicographically
	// This handles cases like "v1.2.3" vs "v1.2.10" correctly
	return v1 > v2
}
