//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

func TestCompilerSharedActionCache(t *testing.T) {
	// Create a temporary directory for test workflows
	tmpDir := testutil.TempDir(t, "test-*")

	// Change to the temp directory so the cache path is consistent
	t.Chdir(tmpDir)

	// Create a compiler instance
	compiler := NewCompiler()

	// Get the shared action resolver (first time - should initialize)
	cache1, resolver1 := compiler.getSharedActionResolver()
	if cache1 == nil {
		t.Error("Expected cache to be initialized")
	}
	if resolver1 == nil {
		t.Error("Expected resolver to be initialized")
	}

	// Add an entry to the cache
	cache1.Set("actions/checkout", "v5", "test-sha-abc")

	// Get the shared action resolver again (should be same instance)
	cache2, resolver2 := compiler.getSharedActionResolver()

	// Verify it's the same instance
	if cache1 != cache2 {
		t.Error("Expected same cache instance to be returned")
	}
	if resolver1 != resolver2 {
		t.Error("Expected same resolver instance to be returned")
	}

	// Verify the cache entry is still there (proves it's shared)
	sha, found := cache2.Get("actions/checkout", "v5")
	if !found {
		t.Error("Expected to find cached entry")
	}
	if sha != "test-sha-abc" {
		t.Errorf("Expected SHA 'test-sha-abc', got '%s'", sha)
	}
}

func TestCompilerSharedCacheAcrossWorkflows(t *testing.T) {
	// Create a temporary directory for test
	tmpDir := testutil.TempDir(t, "test-*")

	// Change to the temp directory
	t.Chdir(tmpDir)

	// Create test workflow files
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	err := os.MkdirAll(workflowsDir, 0755)
	if err != nil {
		t.Fatalf("Failed to create workflows directory: %v", err)
	}

	workflow1Content := `---
on: push
engine: copilot
---
# Test Workflow 1
Test content
`

	workflow2Content := `---
on: pull_request
engine: copilot
---
# Test Workflow 2
Test content
`

	workflow1Path := filepath.Join(workflowsDir, "workflow1.md")
	workflow2Path := filepath.Join(workflowsDir, "workflow2.md")

	if err := os.WriteFile(workflow1Path, []byte(workflow1Content), 0644); err != nil {
		t.Fatalf("Failed to write workflow1: %v", err)
	}
	if err := os.WriteFile(workflow2Path, []byte(workflow2Content), 0644); err != nil {
		t.Fatalf("Failed to write workflow2: %v", err)
	}

	// Create a compiler
	compiler := NewCompiler()
	compiler.SetSkipValidation(true)
	compiler.SetNoEmit(true)

	// Parse the first workflow
	data1, err := compiler.ParseWorkflowFile(workflow1Path)
	if err != nil {
		t.Fatalf("Failed to parse workflow1: %v", err)
	}

	// Manually add a cache entry via the first workflow's cache
	data1.ActionCache.Set("actions/checkout", "v5", "shared-sha-123")

	// Parse the second workflow
	data2, err := compiler.ParseWorkflowFile(workflow2Path)
	if err != nil {
		t.Fatalf("Failed to parse workflow2: %v", err)
	}

	// Verify the second workflow uses the same cache instance
	if data1.ActionCache != data2.ActionCache {
		t.Error("Expected both workflows to share the same cache instance")
	}

	// Verify the cache entry is available in the second workflow
	sha, found := data2.ActionCache.Get("actions/checkout", "v5")
	if !found {
		t.Error("Expected to find cached entry in second workflow")
	}
	if sha != "shared-sha-123" {
		t.Errorf("Expected SHA 'shared-sha-123', got '%s'", sha)
	}
}

func TestCompilerForceRefreshClearsOnlyOnce(t *testing.T) {
	// Create a temporary directory for test
	tmpDir := testutil.TempDir(t, "test-*")

	// Change to the temp directory
	t.Chdir(tmpDir)

	// Create a compiler with forceRefreshActionPins enabled
	compiler := NewCompiler()
	compiler.SetForceRefreshActionPins(true)

	// Get the shared action resolver (first time - should initialize empty)
	cache1, _ := compiler.getSharedActionResolver()
	if cache1 == nil {
		t.Fatal("Expected cache to be initialized")
	}

	// Verify cache is empty (not loaded from disk)
	if len(cache1.Entries) != 0 {
		t.Error("Expected cache to be empty on initialization with forceRefreshActionPins")
	}

	// Add some entries to the cache (simulating resolution during compilation)
	cache1.Set("actions/checkout", "v5", "sha-abc-123")
	cache1.Set("actions/setup-node", "v4", "sha-def-456")

	// Verify entries were added
	if len(cache1.Entries) != 2 {
		t.Errorf("Expected 2 entries in cache, got %d", len(cache1.Entries))
	}

	// Get the shared action resolver again (second workflow in same run)
	cache2, _ := compiler.getSharedActionResolver()

	// Verify it's the same instance
	if cache1 != cache2 {
		t.Error("Expected same cache instance to be returned")
	}

	// Verify the cache still has the entries (NOT cleared again)
	if len(cache2.Entries) != 2 {
		t.Errorf("Expected cache to still have 2 entries, got %d", len(cache2.Entries))
	}

	// Verify specific entries are still there
	sha, found := cache2.Get("actions/checkout", "v5")
	if !found {
		t.Error("Expected to find cached entry for actions/checkout")
	}
	if sha != "sha-abc-123" {
		t.Errorf("Expected SHA 'sha-abc-123', got '%s'", sha)
	}

	sha, found = cache2.Get("actions/setup-node", "v4")
	if !found {
		t.Error("Expected to find cached entry for actions/setup-node")
	}
	if sha != "sha-def-456" {
		t.Errorf("Expected SHA 'sha-def-456', got '%s'", sha)
	}

	// Get the resolver a third time (third workflow in same run)
	cache3, _ := compiler.getSharedActionResolver()

	// Verify it's still the same instance with entries intact
	if cache1 != cache3 {
		t.Error("Expected same cache instance on third call")
	}
	if len(cache3.Entries) != 2 {
		t.Errorf("Expected cache to still have 2 entries on third call, got %d", len(cache3.Entries))
	}
}
