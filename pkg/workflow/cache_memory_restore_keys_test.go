//go:build integration

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

// hasGenericRestoreKey checks if the lock file contains a generic restore key pattern
// that would match caches from other workflows. Returns true if found (which is bad).
func hasGenericRestoreKey(lockContent, prefix string) bool {
	// Look for restore-keys sections
	restoreKeysPattern := regexp.MustCompile(`restore-keys:\s*\|`)
	matches := restoreKeysPattern.FindAllStringIndex(lockContent, -1)

	for _, match := range matches {
		// Get the content after "restore-keys: |"
		start := match[1]
		// Find the next non-indented line (which marks the end of restore-keys)
		lines := strings.Split(lockContent[start:], "\n")
		for _, line := range lines {
			// Check if this line is a restore key (starts with whitespace)
			if strings.HasPrefix(line, "            ") || strings.HasPrefix(line, "          ") {
				restoreKey := strings.TrimSpace(line)
				// Check if this is a generic fallback (ends with just the prefix and nothing else)
				// For example: "memory-" (bad) vs "memory-${{ github.workflow }}-" (good)
				if restoreKey == prefix {
					return true
				}
			} else if strings.TrimSpace(line) != "" {
				// We've hit a non-restore-key line, stop checking this section
				break
			}
		}
	}
	return false
}

// TestCacheMemoryRestoreKeysNoGenericFallback verifies that cache-memory restore-keys
// do NOT include a generic fallback that would match caches from other workflows.
// This prevents cross-workflow cache poisoning attacks.
func TestCacheMemoryRestoreKeysNoGenericFallback(t *testing.T) {
	tests := []struct {
		name             string
		frontmatter      string
		expectedInLock   []string
		genericFallbacks []string // Generic restore key prefixes that should NOT be present
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
			genericFallbacks: []string{"memory-"},
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
			genericFallbacks: []string{"memory-chroma-", "memory-"},
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
			genericFallbacks: []string{"memory-default-", "memory-session-", "memory-"},
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
			genericFallbacks: []string{"memory-"},
		},
		{
			name: "cache-memory with repo scope should have two restore keys",
			frontmatter: `---
name: Test Cache Memory Repo Scope
on: workflow_dispatch
strict: false
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: claude
tools:
  cache-memory:
    - id: shared
      key: shared-cache-${{ github.workflow }}
      scope: repo
  github:
    allowed: [get_repository]
---`,
			expectedInLock: []string{
				// Repo scope generates two restore keys:
				// 1. With workflow ID (try same workflow first)
				// 2. Without workflow ID (allows cross-workflow sharing)
				"restore-keys: |",
				"shared-cache-${{ github.workflow }}-",
				"shared-cache-",
			},
			genericFallbacks: []string{}, // No check - repo scope intentionally allows generic restore key
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

			// Check that generic fallback restore keys are NOT present using the helper
			for _, genericFallback := range tt.genericFallbacks {
				if hasGenericRestoreKey(lockStr, genericFallback) {
					t.Errorf("Found generic restore key '%s' in lock file, which creates a security vulnerability.\nLock file content:\n%s", genericFallback, lockStr)
				}
			}
		})
	}
}
