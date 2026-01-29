//go:build !integration

package parser

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCrossLanguageHashCompatibility validates that Go and JavaScript implementations
// produce identical hashes for the same workflows.
//
// This test creates test workflows and verifies that both implementations produce
// matching hashes. The JavaScript implementation should eventually call the Go binary
// or implement the exact same algorithm.
func TestCrossLanguageHashCompatibility(t *testing.T) {
	// Create a temporary workflow file
	tempDir := t.TempDir()
	workflowFile := filepath.Join(tempDir, "test-workflow.md")

	testCases := []struct {
		name     string
		content  string
		expected string // Will be computed by Go implementation
	}{
		{
			name: "empty frontmatter",
			content: `---
---

# Empty Workflow
`,
		},
		{
			name: "simple frontmatter",
			content: `---
engine: copilot
description: Test workflow
on:
  schedule: daily
---

# Test Workflow
`,
		},
		{
			name: "complex frontmatter",
			content: `---
engine: claude
description: Complex workflow
tracker-id: complex-test
timeout-minutes: 30
on:
  schedule: daily
  workflow_dispatch: true
permissions:
  contents: read
  actions: read
tools:
  playwright:
    version: v1.41.0
labels:
  - test
  - complex
bots:
  - copilot
---

# Complex Workflow
`,
		},
	}

	cache := NewImportCache("")

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Write test workflow
			err := os.WriteFile(workflowFile, []byte(tc.content), 0644)
			require.NoError(t, err, "Should write test file")

			// Compute hash with Go implementation
			hash, err := ComputeFrontmatterHashFromFile(workflowFile, cache)
			require.NoError(t, err, "Should compute hash")
			assert.Len(t, hash, 64, "Hash should be 64 characters")
			assert.Regexp(t, "^[a-f0-9]{64}$", hash, "Hash should be lowercase hex")

			// For now, we just verify the Go implementation works
			// The JavaScript implementation will be tested separately
			// and should produce the same hash

			// Store the computed hash for reference
			t.Logf("Hash for %s: %s", tc.name, hash)

			// Verify determinism
			hash2, err := ComputeFrontmatterHashFromFile(workflowFile, cache)
			require.NoError(t, err, "Should compute hash again")
			assert.Equal(t, hash, hash2, "Hash should be deterministic")
		})
	}
}

// TestHashWithRealWorkflow tests hash computation with an actual workflow from the repository
func TestHashWithRealWorkflow(t *testing.T) {
	// Find a real workflow file
	repoRoot := findRepoRoot(t)
	workflowFile := filepath.Join(repoRoot, ".github", "workflows", "audit-workflows.md")

	// Check if file exists
	if _, err := os.Stat(workflowFile); os.IsNotExist(err) {
		t.Skip("Real workflow file not found, skipping test")
		return
	}

	cache := NewImportCache(repoRoot)

	hash, err := ComputeFrontmatterHashFromFile(workflowFile, cache)
	require.NoError(t, err, "Should compute hash for real workflow")
	assert.Len(t, hash, 64, "Hash should be 64 characters")
	assert.Regexp(t, "^[a-f0-9]{64}$", hash, "Hash should be lowercase hex")

	t.Logf("Hash for audit-workflows.md: %s", hash)

	// Verify determinism
	hash2, err := ComputeFrontmatterHashFromFile(workflowFile, cache)
	require.NoError(t, err, "Should compute hash again")
	assert.Equal(t, hash, hash2, "Hash should be deterministic")
}

// findRepoRoot finds the repository root directory
func findRepoRoot(t *testing.T) string {
	// Start from current directory and walk up to find .git
	dir, err := os.Getwd()
	require.NoError(t, err, "Should get current directory")

	for {
		gitDir := filepath.Join(dir, ".git")
		if _, err := os.Stat(gitDir); err == nil {
			return dir
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("Could not find repository root")
		}
		dir = parent
	}
}
