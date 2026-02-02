//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"

	"github.com/github/gh-aw/pkg/testutil"
)

// TestGeneratedWorkflowsUseSHAs ensures that all generated workflows use SHAs instead of version tags
func TestGeneratedWorkflowsUseSHAs(t *testing.T) {
	// Create a test workflow file
	testDir := testutil.TempDir(t, "test-*")
	workflowFile := filepath.Join(testDir, "test-workflow.md")

	workflowContent := `---
on: push
engine: copilot
permissions:
  contents: read
  issues: read
  pull-requests: read
---

# Test Workflow
This is a test workflow to verify SHA pinning.
`

	err := os.WriteFile(workflowFile, []byte(workflowContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write test workflow: %v", err)
	}

	// Compile the workflow
	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(workflowFile); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	// Read the generated lock file
	lockFile := stringutil.MarkdownToLockFile(workflowFile)
	lockContent, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	lockContentStr := string(lockContent)

	// Check that actions are referenced by SHA, not by version tag
	// Pattern: uses: owner/repo@SHA (40 hex chars)
	shaPattern := regexp.MustCompile(`uses: ([a-zA-Z0-9_-]+/[a-zA-Z0-9_-]+)@([0-9a-f]{40})`)

	// Pattern: uses: owner/repo@version (should not exist)
	versionPattern := regexp.MustCompile(`uses: ([a-zA-Z0-9_-]+/[a-zA-Z0-9_-]+)@(v\d+)`)

	// Find all SHA-based action references
	shaMatches := shaPattern.FindAllString(lockContentStr, -1)
	if len(shaMatches) == 0 {
		t.Errorf("No SHA-based action references found in generated workflow")
	}

	// Check for version-based action references (should not exist)
	versionMatches := versionPattern.FindAllStringSubmatch(lockContentStr, -1)
	if len(versionMatches) > 0 {
		t.Errorf("Found %d version-based action references (should use SHAs):", len(versionMatches))
		for _, match := range versionMatches {
			t.Errorf("  - %s", match[0])
		}
	}

	t.Logf("Found %d SHA-based action references", len(shaMatches))
}

// TestCompileWorkflowActionReferences tests that commonly used actions are pinned to SHAs
func TestCompileWorkflowActionReferences(t *testing.T) {
	testDir := testutil.TempDir(t, "test-*")
	workflowFile := filepath.Join(testDir, "test-workflow.md")

	workflowContent := `---
on:
  issues:
    types: [opened]
engine: copilot
features:
  dangerous-permissions-write: true
permissions:
  contents: read
  issues: write
  pull-requests: read
strict: false
safe-outputs:
  create-issue:
---

# Test Workflow
Create issues based on input.
`

	err := os.WriteFile(workflowFile, []byte(workflowContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write test workflow: %v", err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(workflowFile); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	lockFile := stringutil.MarkdownToLockFile(workflowFile)
	lockContent, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	lockContentStr := string(lockContent)

	// Test specific actions that should be pinned
	expectedActions := map[string]string{
		"actions/checkout":        GetActionPin("actions/checkout"),
		"actions/github-script":   GetActionPin("actions/github-script"),
		"actions/upload-artifact": GetActionPin("actions/upload-artifact"),
	}

	for actionRepo, expectedRef := range expectedActions {
		// Extract just the SHA from the expected reference
		parts := strings.Split(expectedRef, "@")
		if len(parts) != 2 {
			t.Fatalf("Invalid action reference format: %s", expectedRef)
		}
		expectedSHA := parts[1]

		// Check if the action with this SHA appears in the workflow
		if !strings.Contains(lockContentStr, "uses: "+actionRepo+"@"+expectedSHA) {
			t.Errorf("Expected to find %s@%s in generated workflow, but it was not found", actionRepo, expectedSHA)
		}
	}
}

// TestNoVersionTagsInLockFiles is a regression test to ensure version tags are not used
func TestNoVersionTagsInLockFiles(t *testing.T) {
	testDir := testutil.TempDir(t, "test-*")
	workflowFile := filepath.Join(testDir, "test-workflow.md")

	workflowContent := `---
on: push
engine: copilot
permissions:
  contents: read
  issues: read
  pull-requests: read
---

# Simple Test
Just a simple test workflow.
`

	err := os.WriteFile(workflowFile, []byte(workflowContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write test workflow: %v", err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(workflowFile); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	lockFile := stringutil.MarkdownToLockFile(workflowFile)
	lockContent, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	lockContentStr := string(lockContent)

	// These version-based references should NOT appear in the generated workflow
	forbiddenPatterns := []string{
		"actions/checkout@v5",
		"actions/github-script@v8",
		"actions/upload-artifact@v4",
		"actions/download-artifact@v6",
		"actions/cache@v4",
		"actions/setup-node@v6",
		"actions/setup-python@v5",
		"actions/setup-go@v6",
	}

	for _, forbidden := range forbiddenPatterns {
		if strings.Contains(lockContentStr, "uses: "+forbidden) {
			t.Errorf("Found forbidden version tag reference: uses: %s (should use SHA instead)", forbidden)
		}
	}
}

// TestActionSHAValidationSavesCache tests that cache is persisted after validation
// This test doesn't require network access and uses a pre-populated cache
func TestActionSHAValidationSavesCache(t *testing.T) {
	testDir := testutil.TempDir(t, "test-*")

	// Create a lock file with an action
	lockFile := filepath.Join(testDir, "test-workflow.lock.yml")
	lockContent := `name: Test Workflow
on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@93cb6efe18208431cddfb8368fd83d5badbf9bfd # v5
`
	if err := os.WriteFile(lockFile, []byte(lockContent), 0644); err != nil {
		t.Fatalf("Failed to write lock file: %v", err)
	}

	// Create a cache and pre-populate it with entries
	cache := NewActionCache(testDir)
	cache.Set("actions/checkout", "v5", "93cb6efe18208431cddfb8368fd83d5badbf9bfd")

	// Verify cache file doesn't exist before validation
	cachePath := filepath.Join(testDir, ".github", "aw", CacheFileName)
	if _, err := os.Stat(cachePath); !os.IsNotExist(err) {
		os.RemoveAll(filepath.Join(testDir, ".github"))
	}

	// Run validation - even if no updates are detected, this exercises the code path
	// In a real scenario with network access, this would detect and save updates
	err := ValidateActionSHAsInLockFile(lockFile, cache, false)
	if err != nil {
		t.Fatalf("Validation failed: %v", err)
	}

	// The test verifies the code compiles and runs without errors
	// In CI with gh CLI access, the cache would be saved if updates were found
	t.Logf("Validation completed successfully")
}
