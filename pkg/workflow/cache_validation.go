// This file provides validation for sandbox cache-memory configuration.
//
// # Cache Memory Validation
//
// This file validates that cache-memory entries in a workflow's sandbox
// configuration have unique IDs, preventing runtime conflicts when multiple
// cache entries are defined.
//
// # Validation Functions
//
//   - validateNoDuplicateCacheIDs() - Ensures each cache entry has a unique ID
//   - validateNoCacheKeyRunID() - Rejects cache keys that reference github.run_id
//
// # When to Add Validation Here
//
// Add validation to this file when:
//   - Adding new cache-memory configuration constraints
//   - Adding cross-cache validation rules (e.g., total size limits)

package workflow

import "slices"

// validateNoCacheKeyRunID returns an error when a user-supplied cache key
// contains the ${{ github.run_id }} expression.
//
// Including run_id in the key means every run writes to a unique cache slot and
// the cache can never be restored from a previous run. The compiler already
// appends run_id automatically to the save key while generating a stable
// restore-keys prefix — users must not add it themselves.
func validateNoCacheKeyRunID(key string) error {
	if slices.ContainsFunc(interpolationReferences(key), func(path []string) bool {
		return referenceHasPrefix(path, []string{"github", "run_id"})
	}) {
		return NewValidationError(
			"tools.cache-memory.key",
			key,
			"cache key must not reference github.run_id — every run would write to a unique cache slot, preventing cross-run cache restoration",
			"Remove github.run_id from the key. The compiler appends it automatically to the save key and generates a stable restore-keys prefix.\n\nExample:\n\ntools:\n  cache-memory:\n    key: my-data-${{ env.GH_AW_WORKFLOW_ID_SANITIZED }}\n    # ✓ compiler adds run_id to the save key; restore-keys prefix enables cross-run restoration",
		)
	}
	return nil
}

// validateNoDuplicateCacheIDs checks for duplicate cache IDs and returns an error if found.
// Uses the generic validateNoDuplicateIDs helper for consistent duplicate detection.
func validateNoDuplicateCacheIDs(caches []CacheMemoryEntry) error {
	cacheLog.Printf("Validating cache IDs: checking %d caches for duplicates", len(caches))
	err := validateNoDuplicateIDs(caches, func(c CacheMemoryEntry) string { return c.ID }, func(id string) error {
		cacheLog.Printf("Duplicate cache ID found: %s", id)
		return NewValidationError(
			"sandbox.cache-memory",
			id,
			"duplicate cache-memory ID found - each cache must have a unique ID",
			"Change the cache ID to a unique value. Example:\n\nsandbox:\n  cache-memory:\n    - id: cache-1\n      size: 100MB\n    - id: cache-2  # Use unique IDs\n      size: 50MB",
		)
	})
	if err != nil {
		return err
	}
	cacheLog.Print("Cache ID validation passed: no duplicates found")
	return nil
}
