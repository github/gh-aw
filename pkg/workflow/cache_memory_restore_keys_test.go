//go:build integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/github/gh-aw/pkg/testutil"
)

// TestCacheMemoryRestoreKeysNoGenericFallback verifies that cache-memory restore-keys
// do NOT include a generic fallback that would match caches from other workflows.
// This prevents cross-workflow cache poisoning attacks.
func TestCacheMemoryRestoreKeysNoGenericFallback(t *testing.T) {
	tests := []struct {
		name              string
		frontmatter       string
		expectedInLock    []string
		notExpectedInLock []string
	}{
		{
			name: "default cache-memory should NOT have generic memory- fallback",
			frontmatter: `---
name: Test Cache Memory Restore Keys
on: workflow_dispatch
permissions:
  contents: read
engine: claude
tools:
  cache-memory: true
  github:
    allowed: [get_repository]
---`,
			expectedInLock: []string{
				// Should have workflow-specific restore key
				"restore-keys: |",
				"memory-${{ github.workflow }}-",
			},
			notExpectedInLock: []string{
				// Should NOT have generic fallback that would match other workflows
				"            memory-\n",
				// More specific check: "memory-" followed by newline at the right indent level
			},
		},
		{
			name: "cache-memory with custom ID should NOT have generic fallbacks",
			frontmatter: `---
name: Test Cache Memory Custom ID
on: workflow_dispatch
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: claude
tools:
  cache-memory:
    - id: chroma
      key: memory-chroma-${{ github.workflow }}
  github:
    allowed: [get_repository]
---`,
			expectedInLock: []string{
				// Custom key becomes memory-chroma-${{ github.workflow }}-${{ github.run_id }}
				// Restore key should only remove run_id: memory-chroma-${{ github.workflow }}-
				"restore-keys: |",
				"memory-chroma-${{ github.workflow }}-",
			},
			notExpectedInLock: []string{
				// Should NOT have generic fallbacks that would match other workflows
				"            memory-chroma-\n",
				"            memory-\n",
			},
		},
		{
			name: "multiple cache-memory should NOT have generic fallbacks",
			frontmatter: `---
name: Test Multiple Cache Memory
on: workflow_dispatch
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: claude
tools:
  cache-memory:
    - id: default
      key: memory-default-${{ github.workflow }}
    - id: session
      key: memory-session-${{ github.workflow }}
  github:
    allowed: [get_repository]
---`,
			expectedInLock: []string{
				// Custom keys become memory-*-${{ github.workflow }}-${{ github.run_id }}
				// Restore keys should only remove run_id
				"memory-default-${{ github.workflow }}-",
				"memory-session-${{ github.workflow }}-",
			},
			notExpectedInLock: []string{
				// Should NOT have generic fallbacks for either cache
				"            memory-default-\n",
				"            memory-session-\n",
				"            memory-\n",
			},
		},
		{
			name: "cache-memory with threat detection should NOT have generic fallback",
			frontmatter: `---
name: Test Cache Memory with Threat Detection
on: workflow_dispatch
permissions:
  contents: read
engine: claude
tools:
  cache-memory: true
  github:
    allowed: [get_repository]
safe-outputs:
  create-issue:
  threat-detection: true
---`,
			expectedInLock: []string{
				// Should use restore action
				"uses: actions/cache/restore@",
				// Should have workflow-specific restore key
				"restore-keys: |",
				"memory-${{ github.workflow }}-",
			},
			notExpectedInLock: []string{
				// Should NOT have generic fallback
				"            memory-\n",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a temporary directory
			tmpDir := testutil.TempDir(t, "test-*")

			// Write the markdown file
			mdPath := filepath.Join(tmpDir, "test-workflow.md")
			content := tt.frontmatter + "\n\n# Test Workflow\n\nTest cache-memory restore-keys configuration.\n"
			if err := os.WriteFile(mdPath, []byte(content), 0644); err != nil {
				t.Fatalf("Failed to write test markdown file: %v", err)
			}

			// Compile the workflow
			compiler := NewCompiler()
			if err := compiler.CompileWorkflow(mdPath); err != nil {
				t.Fatalf("Failed to compile workflow: %v", err)
			}

			// Read the generated lock file
			lockPath := stringutil.MarkdownToLockFile(mdPath)
			lockContent, err := os.ReadFile(lockPath)
			if err != nil {
				t.Fatalf("Failed to read lock file: %v", err)
			}
			lockStr := string(lockContent)

			// Check expected strings
			for _, expected := range tt.expectedInLock {
				if !strings.Contains(lockStr, expected) {
					t.Errorf("Expected to find '%s' in lock file but it was missing.\nLock file content:\n%s", expected, lockStr)
				}
			}

			// Check that unexpected strings are NOT present
			for _, notExpected := range tt.notExpectedInLock {
				if strings.Contains(lockStr, notExpected) {
					t.Errorf("Did not expect to find '%s' in lock file but it was present.\nLock file content:\n%s", notExpected, lockStr)
				}
			}
		})
	}
}
