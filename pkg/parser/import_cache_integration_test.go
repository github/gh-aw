//go:build integration

package parser

import (
	"os"
	"path/filepath"
	"testing"
)

// TestImportCacheIntegration tests the cache with the full import flow
func TestImportCacheIntegration(t *testing.T) {
	// Create temp directories for testing
	tempDir, err := os.MkdirTemp("", "import-cache-integration-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create a cache
	cache := NewImportCache(tempDir)

	// Simulate a workflow file that imports from another repo
	workflowContent := `---
imports:
  - testowner/testrepo/workflows/shared.md@main
---

# Test Workflow

Use shared configuration.
`

	workflowPath := filepath.Join(tempDir, "test-workflow.md")
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	// Simulate a remote file being cached
	sharedContent := []byte(`---
tools:
  edit:
---

# Shared Configuration

This is shared configuration.
`)

	// Cache the "remote" file
	sha := "abc123"
	cachedPath, err := cache.Set("testowner", "testrepo", "workflows/shared.md", sha, sharedContent)
	if err != nil {
		t.Fatalf("Failed to cache file: %v", err)
	}

	// Verify cache can retrieve the file
	retrievedPath, found := cache.Get("testowner", "testrepo", "workflows/shared.md", sha)
	if !found {
		t.Error("Failed to retrieve cached file")
	}
	if retrievedPath != cachedPath {
		t.Errorf("Retrieved path mismatch. Expected %s, got %s", cachedPath, retrievedPath)
	}

	// Verify the cached file contains correct content
	content, err := os.ReadFile(retrievedPath)
	if err != nil {
		t.Fatalf("Failed to read cached file: %v", err)
	}
	if string(content) != string(sharedContent) {
		t.Errorf("Content mismatch. Expected %q, got %q", sharedContent, content)
	}

	// Test new cache instance can find the file (simulating offline scenario)
	cache2 := NewImportCache(tempDir)

	// Verify we can still retrieve the file using filesystem lookup
	retrievedPath2, found := cache2.Get("testowner", "testrepo", "workflows/shared.md", sha)
	if !found {
		t.Error("Failed to retrieve cached file from new cache instance")
	}
	if retrievedPath2 != cachedPath {
		t.Errorf("Retrieved path mismatch from new instance. Expected %s, got %s", cachedPath, retrievedPath2)
	}
}

// TestImportCacheMultipleFiles tests caching multiple files from different repos
func TestImportCacheMultipleFiles(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "import-cache-multi-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	cache := NewImportCache(tempDir)

	// Cache multiple files
	files := []struct {
		owner   string
		repo    string
		path    string
		ref     string
		sha     string
		content string
	}{
		{"owner1", "repo1", "workflows/a.md", "main", "sha1", "Content A"},
		{"owner1", "repo1", "workflows/b.md", "v1.0", "sha2", "Content B"},
		{"owner2", "repo2", "config/c.md", "main", "sha3", "Content C"},
	}

	for _, f := range files {
		_, err := cache.Set(f.owner, f.repo, f.path, f.sha, []byte(f.content))
		if err != nil {
			t.Fatalf("Failed to cache file %s/%s/%s@%s: %v", f.owner, f.repo, f.path, f.sha, err)
		}
	}

	// Verify all files are retrievable
	for _, f := range files {
		path, found := cache.Get(f.owner, f.repo, f.path, f.sha)
		if !found {
			t.Errorf("Failed to retrieve cached file %s/%s/%s@%s", f.owner, f.repo, f.path, f.sha)
			continue
		}

		content, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("Failed to read cached file: %v", err)
			continue
		}

		if string(content) != f.content {
			t.Errorf("Content mismatch for %s/%s/%s@%s. Expected %q, got %q",
				f.owner, f.repo, f.path, f.sha, f.content, string(content))
		}
	}

	// Verify from new cache instance using filesystem lookup
	cache2 := NewImportCache(tempDir)

	for _, f := range files {
		path, found := cache2.Get(f.owner, f.repo, f.path, f.sha)
		if !found {
			t.Errorf("Failed to retrieve cached file from new instance %s/%s/%s@%s", f.owner, f.repo, f.path, f.sha)
			continue
		}

		content, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("Failed to read cached file: %v", err)
			continue
		}

		if string(content) != f.content {
			t.Errorf("Content mismatch from new instance for %s/%s/%s@%s. Expected %q, got %q",
				f.owner, f.repo, f.path, f.sha, f.content, string(content))
		}
	}
}
