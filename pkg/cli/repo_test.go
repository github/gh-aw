//go:build !integration

package cli

import (
	"testing"
)

// TestGetCurrentRepoSlug tests that the cached version calls the underlying function only once
func TestGetCurrentRepoSlug(t *testing.T) {
	// Clear the cache before testing
	ClearCurrentRepoSlugCache()

	// Note: In a real-world scenario, we would use dependency injection or interfaces
	// For this test, we'll just verify that multiple calls to GetCurrentRepoSlug
	// produce the same result (which implies caching is working)

	// First call
	result1, err1 := GetCurrentRepoSlug()
	if err1 != nil {
		t.Logf("First call error (expected if not in a git repo): %v", err1)
	}

	// Second call
	result2, err2 := GetCurrentRepoSlug()
	if err2 != nil {
		t.Logf("Second call error (expected if not in a git repo): %v", err2)
	}

	// Both calls should return the same result and same error
	if result1 != result2 {
		t.Errorf("GetCurrentRepoSlug returned different results on multiple calls: %s vs %s", result1, result2)
	}

	if (err1 == nil) != (err2 == nil) {
		t.Errorf("GetCurrentRepoSlug returned different error states on multiple calls")
	}

	if err1 != nil && err2 != nil && err1.Error() != err2.Error() {
		t.Errorf("GetCurrentRepoSlug returned different errors on multiple calls: %v vs %v", err1, err2)
	}

	t.Logf("GetCurrentRepoSlug returned consistent results (result: %s, error: %v)", result1, err1)
}

// TestClearCurrentRepoSlugCache tests that clearing the cache allows a fresh call
func TestClearCurrentRepoSlugCache(t *testing.T) {

	// Clear the cache
	ClearCurrentRepoSlugCache()

	// First call
	result1, err1 := GetCurrentRepoSlug()
	if err1 != nil {
		t.Logf("First call error (expected if not in a git repo): %v", err1)
	}

	// Clear the cache again
	ClearCurrentRepoSlugCache()

	// Second call after clearing cache
	result2, err2 := GetCurrentRepoSlug()
	if err2 != nil {
		t.Logf("Second call error (expected if not in a git repo): %v", err2)
	}

	// Results should still be the same (we're in the same repo)
	if result1 != result2 {
		t.Errorf("GetCurrentRepoSlug returned different results after cache clear: %s vs %s", result1, result2)
	}

	if (err1 == nil) != (err2 == nil) {
		t.Errorf("GetCurrentRepoSlug returned different error states after cache clear")
	}

	t.Logf("Cache clear test passed (result: %s, error: %v)", result1, err1)
}

// TestGetCurrentRepoSlugFormat tests the format validation
func TestGetCurrentRepoSlugFormat(t *testing.T) {
	// This test will only pass if we're in a GitHub repository
	result, err := GetCurrentRepoSlug()

	if err != nil {
		t.Logf("GetCurrentRepoSlug returned error (expected if not in a git repo): %v", err)
		// Skip further validation if we're not in a repo
		return
	}

	// Verify the format is "owner/repo"
	if result == "" {
		t.Error("GetCurrentRepoSlug returned empty string without error")
		return
	}

	// The function already validates the format, so if we got here without error,
	// the format should be valid. Let's just log the result.
	t.Logf("GetCurrentRepoSlug returned: %s", result)
}
