//go:build !integration

package parser

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestImportCache(t *testing.T) {
	// Create temp directory for testing
	tempDir, err := os.MkdirTemp("", "import-cache-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create a new cache
	cache := NewImportCache(tempDir)

	// Test Set and Get
	testContent := []byte("# Test Workflow\n\nTest content")
	owner := "testowner"
	repo := "testrepo"
	path := "workflows/test.md"
	sha := "abc123"

	cachedPath, err := cache.Set(owner, repo, path, sha, testContent)
	if err != nil {
		t.Fatalf("Failed to set cache entry: %v", err)
	}

	// Verify file was created
	if _, err := os.Stat(cachedPath); os.IsNotExist(err) {
		t.Errorf("Cache file was not created: %s", cachedPath)
	}

	// Verify content
	content, err := os.ReadFile(cachedPath)
	if err != nil {
		t.Fatalf("Failed to read cached file: %v", err)
	}
	if string(content) != string(testContent) {
		t.Errorf("Content mismatch. Expected %q, got %q", testContent, content)
	}

	// Test Get
	retrievedPath, found := cache.Get(owner, repo, path, sha)
	if !found {
		t.Error("Cache entry not found after Set")
	}
	if retrievedPath != cachedPath {
		t.Errorf("Path mismatch. Expected %s, got %s", cachedPath, retrievedPath)
	}

	// Test that a new cache instance can find the file
	cache2 := NewImportCache(tempDir)
	retrievedPath2, found := cache2.Get(owner, repo, path, sha)
	if !found {
		t.Error("Cache entry not found from new cache instance")
	}
	if retrievedPath2 != cachedPath {
		t.Errorf("Path mismatch from new instance. Expected %s, got %s", cachedPath, retrievedPath2)
	}
}

func TestImportCacheDirectory(t *testing.T) {
	// Create temp directory for testing
	tempDir, err := os.MkdirTemp("", "import-cache-dir-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	cache := NewImportCache(tempDir)

	// Test cache directory path
	expectedDir := filepath.Join(tempDir, ImportCacheDir)
	if cache.GetCacheDir() != expectedDir {
		t.Errorf("Cache dir mismatch. Expected %s, got %s", expectedDir, cache.GetCacheDir())
	}

	// Create a cache entry to trigger directory creation
	testContent := []byte("test")
	_, err = cache.Set("owner", "repo", "test.md", "sha1", testContent)
	if err != nil {
		t.Fatalf("Failed to set cache entry: %v", err)
	}

	// Verify directory was created
	if _, err := os.Stat(expectedDir); os.IsNotExist(err) {
		t.Errorf("Cache directory was not created: %s", expectedDir)
	}

	// Verify .gitattributes was auto-generated
	gitAttributesPath := filepath.Join(expectedDir, ".gitattributes")
	if _, err := os.Stat(gitAttributesPath); os.IsNotExist(err) {
		t.Errorf(".gitattributes file was not created: %s", gitAttributesPath)
	}

	// Verify .gitattributes content
	content, err := os.ReadFile(gitAttributesPath)
	if err != nil {
		t.Fatalf("Failed to read .gitattributes: %v", err)
	}
	contentStr := string(content)
	if !strings.Contains(contentStr, "linguist-generated=true") {
		t.Error(".gitattributes missing 'linguist-generated=true'")
	}
	if !strings.Contains(contentStr, "merge=ours") {
		t.Error(".gitattributes missing 'merge=ours'")
	}
}

func TestImportCacheMissingFile(t *testing.T) {
	// Create temp directory for testing
	tempDir, err := os.MkdirTemp("", "import-cache-missing-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	cache := NewImportCache(tempDir)

	// Add entry to cache
	testContent := []byte("test")
	cachedPath, err := cache.Set("owner", "repo", "test.md", "sha1", testContent)
	if err != nil {
		t.Fatalf("Failed to set cache entry: %v", err)
	}

	// Delete the cached file
	if err := os.Remove(cachedPath); err != nil {
		t.Fatalf("Failed to remove cached file: %v", err)
	}

	// Try to get the entry - should return not found since file is missing
	_, found := cache.Get("owner", "repo", "test.md", "sha1")
	if found {
		t.Error("Expected cache miss for deleted file, but got hit")
	}
}

func TestImportCacheEmptyCache(t *testing.T) {
	// Create temp directory for testing
	tempDir, err := os.MkdirTemp("", "import-cache-empty-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	cache := NewImportCache(tempDir)

	// Try to get from empty cache - should return not found
	_, found := cache.Get("owner", "repo", "test.md", "nonexistent-sha")
	if found {
		t.Error("Expected cache miss for empty cache, but got hit")
	}
}
