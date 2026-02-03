//go:build integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"
)

func TestCacheMemoryRestoreOnly(t *testing.T) {
	tests := []struct {
		name              string
		frontmatter       string
		expectedInLock    []string
		notExpectedInLock []string
	}{
		{
			name: "cache-memory with restore-only flag (object notation)",
			frontmatter: `---
name: Test Cache Memory Restore Only Object
on: workflow_dispatch
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: copilot
tools:
  cache-memory:
    restore-only: true
---`,
			expectedInLock: []string{
				"# Cache memory file share configuration from frontmatter processed below",
				"- name: Restore cache-memory file share data",
				"actions/cache/restore@0057852bfaa89a56745cba8c7296529d2fc39830",
				"key: memory-${{ github.workflow }}-${{ github.run_id }}",
				"path: /tmp/gh-aw/cache-memory",
			},
			notExpectedInLock: []string{
				"- name: Upload cache-memory data as artifact",
				"uses: actions/cache@0057852bfaa89a56745cba8c7296529d2fc39830",
			},
		},
		{
			name: "cache-memory with restore-only in array notation",
			frontmatter: `---
name: Test Cache Memory Restore Only Array
on: workflow_dispatch
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: copilot
tools:
  cache-memory:
    - id: default
      key: memory-default
    - id: readonly
      key: memory-readonly
      restore-only: true
---`,
			expectedInLock: []string{
				"# Cache memory file share configuration from frontmatter processed below",
				"- name: Cache cache-memory file share data (default)",
				"actions/cache@0057852bfaa89a56745cba8c7296529d2fc39830",
				"key: memory-default-${{ github.run_id }}",
				"- name: Restore cache-memory file share data (readonly)",
				"actions/cache/restore@0057852bfaa89a56745cba8c7296529d2fc39830",
				"key: memory-readonly-${{ github.run_id }}",
			},
			notExpectedInLock: []string{
				// Should NOT upload artifacts when detection is disabled
				"- name: Upload cache-memory data as artifact (default)",
				"name: cache-memory-default",
				"- name: Upload cache-memory data as artifact (readonly)",
				"name: cache-memory-readonly",
			},
		},
		{
			name: "cache-memory mixed restore-only and normal caches",
			frontmatter: `---
name: Test Cache Memory Mixed
on: workflow_dispatch
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: copilot
tools:
  cache-memory:
    - id: writeable
      key: memory-write
      restore-only: false
    - id: readonly1
      key: memory-read1
      restore-only: true
    - id: readonly2
      key: memory-read2
      restore-only: true
---`,
			expectedInLock: []string{
				"- name: Cache cache-memory file share data (writeable)",
				"actions/cache@0057852bfaa89a56745cba8c7296529d2fc39830",
				"- name: Restore cache-memory file share data (readonly1)",
				"actions/cache/restore@0057852bfaa89a56745cba8c7296529d2fc39830",
				"- name: Restore cache-memory file share data (readonly2)",
			},
			notExpectedInLock: []string{
				// Should NOT upload artifacts when detection is disabled
				"- name: Upload cache-memory data as artifact (writeable)",
				"name: cache-memory-writeable",
				"- name: Upload cache-memory data as artifact (readonly1)",
				"- name: Upload cache-memory data as artifact (readonly2)",
				"name: cache-memory-readonly1",
				"name: cache-memory-readonly2",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary directory for test
			tmpDir := t.TempDir()
			mdPath := filepath.Join(tmpDir, "test-workflow.md")

			// Write frontmatter + minimal prompt
			content := tt.frontmatter + "\n\nTest workflow for cache-memory restore-only flag.\n"
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

			// Check expected strings are present
			for _, expected := range tt.expectedInLock {
				if !strings.Contains(lockStr, expected) {
					t.Errorf("Expected to find '%s' in lock file but it was missing.\nLock file content:\n%s", expected, lockStr)
				}
			}

			// Check unexpected strings are NOT present
			for _, notExpected := range tt.notExpectedInLock {
				if strings.Contains(lockStr, notExpected) {
					t.Errorf("Did not expect to find '%s' in lock file but it was present.\nLock file content:\n%s", notExpected, lockStr)
				}
			}
		})
	}
}
