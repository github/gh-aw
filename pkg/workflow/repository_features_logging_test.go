//go:build !integration

package workflow

import (
	"testing"
)

// TestRepositoryFeaturesLoggedCache verifies that the logged cache is properly maintained
func TestRepositoryFeaturesLoggedCache(t *testing.T) {
	// Clear all caches before test
	ClearRepositoryFeaturesCache()

	// Verify that the logged cache is cleared
	count := 0
	repositoryFeaturesLoggedCache.Range(func(key, value any) bool {
		count++
		return true
	})

	if count != 0 {
		t.Errorf("Expected logged cache to be empty after clear, but found %d entries", count)
	}

	// Manually populate the logged cache
	testRepo := "test/repo"
	repositoryFeaturesLoggedCache.Store(testRepo, true)

	// Verify it's stored
	if _, exists := repositoryFeaturesLoggedCache.Load(testRepo); !exists {
		t.Error("Expected logged cache to contain test repository")
	}

	// Clear again and verify
	ClearRepositoryFeaturesCache()
	if _, exists := repositoryFeaturesLoggedCache.Load(testRepo); exists {
		t.Error("Expected logged cache to be cleared")
	}
}

// TestGetRepositoryFeaturesLogOnce verifies that verbose logging only happens once per repository
func TestGetRepositoryFeaturesLogOnce(t *testing.T) {
	// This test verifies the structure but can't fully test the logging without API access
	// The key behavior is that the logged cache prevents duplicate logging

	// Clear all caches before test
	ClearRepositoryFeaturesCache()

	testRepo := "owner/repo"

	// First time: LoadOrStore returns loaded=false, should log (if verbose=true and API succeeds)
	// Second time: LoadOrStore returns loaded=true, should NOT log even if verbose=true

	// We can't actually test the full flow without API access, but we can verify:
	// 1. The logged cache mechanism exists
	// 2. The clear function clears both caches
	// 3. LoadOrStore is used correctly

	// Verify that logged cache can be used
	_, loaded := repositoryFeaturesLoggedCache.LoadOrStore(testRepo, true)
	if loaded {
		t.Error("First LoadOrStore should return loaded=false")
	}

	// Second call should return loaded=true
	_, loaded = repositoryFeaturesLoggedCache.LoadOrStore(testRepo, true)
	if !loaded {
		t.Error("Second LoadOrStore should return loaded=true")
	}
}
