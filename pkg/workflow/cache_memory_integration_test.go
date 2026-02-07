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

func TestCacheMemoryMultipleIntegration(t *testing.T) {
	tests := []struct {
		name              string
		frontmatter       string
		expectedInLock    []string
		notExpectedInLock []string
	}{
		{
			name: "single cache-memory (backward compatible)",
			frontmatter: `---
name: Test Cache Memory Single
on: workflow_dispatch
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: claude
tools:
  cache-memory: true
  github:
    allowed: [get_repository]
---`,
			expectedInLock: []string{
				"# Cache memory file share configuration from frontmatter processed below",
				"- name: Create cache-memory directory",
				"- name: Cache cache-memory file share data",
				"uses: actions/cache@0057852bfaa89a56745cba8c7296529d2fc39830",
				"key: memory-${{ github.workflow }}-${{ github.run_id }}",
				"path: /tmp/gh-aw/cache-memory",
				"cat \"/opt/gh-aw/prompts/cache_memory_prompt.md\"",
				"GH_AW_CACHE_DIR: '/tmp/gh-aw/cache-memory/'",
				"GH_AW_CACHE_DIR: process.env.GH_AW_CACHE_DIR",
			},
			notExpectedInLock: []string{
				// Should NOT upload artifact when detection is disabled
				"- name: Upload cache-memory data as artifact",
				"## Cache Folders Available",
				"cache-memory/default/",
				"cache-memory/session/",
			},
		},
		{
			name: "multiple cache-memory with array notation",
			frontmatter: `---
name: Test Cache Memory Multiple
on: workflow_dispatch
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: claude
tools:
  cache-memory:
    - id: default
      key: memory-default
    - id: session
      key: memory-session
  github:
    allowed: [get_repository]
---`,
			expectedInLock: []string{
				"# Cache memory file share configuration from frontmatter processed below",
				"- name: Create cache-memory directory (default)",
				"mkdir -p /tmp/gh-aw/cache-memory",
				"- name: Cache cache-memory file share data (default)",
				"key: memory-default-${{ github.run_id }}",
				"path: /tmp/gh-aw/cache-memory",
				"- name: Create cache-memory directory (session)",
				"mkdir -p /tmp/gh-aw/cache-memory-session",
				"- name: Cache cache-memory file share data (session)",
				"key: memory-session-${{ github.run_id }}",
				"path: /tmp/gh-aw/cache-memory-session",
				"## Cache Folders Available",
				"- **default**: `/tmp/gh-aw/cache-memory/`",
				"- **session**: `/tmp/gh-aw/cache-memory-session/`",
			},
			notExpectedInLock: []string{
				// Should NOT upload artifacts when detection is disabled
				"- name: Upload cache-memory data as artifact (default)",
				"- name: Upload cache-memory data as artifact (session)",
				"## Cache Folder Available",
			},
		},
		{
			name: "multiple cache-memory with custom keys",
			frontmatter: `---
name: Test Cache Memory Multiple Custom Keys
on: workflow_dispatch
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: claude
tools:
  cache-memory:
    - id: data
      key: memory-data-${{ github.workflow }}
    - id: logs
      key: memory-logs-${{ github.workflow }}
  github:
    allowed: [get_repository]
---`,
			expectedInLock: []string{
				"- name: Create cache-memory directory (data)",
				"mkdir -p /tmp/gh-aw/cache-memory-data",
				"key: memory-data-${{ github.workflow }}-${{ github.run_id }}",
				"path: /tmp/gh-aw/cache-memory-data",
				"- name: Create cache-memory directory (logs)",
				"mkdir -p /tmp/gh-aw/cache-memory-logs",
				"key: memory-logs-${{ github.workflow }}-${{ github.run_id }}",
				"path: /tmp/gh-aw/cache-memory-logs",
				"## Cache Folders Available",
				"- **data**: `/tmp/gh-aw/cache-memory-data/`",
				"- **logs**: `/tmp/gh-aw/cache-memory-logs/`",
			},
			notExpectedInLock: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a temporary directory
			tmpDir := testutil.TempDir(t, "test-*")

			// Write the markdown file
			mdPath := filepath.Join(tmpDir, "test-workflow.md")
			content := tt.frontmatter + "\n\n# Test Workflow\n\nTest cache-memory configuration.\n"
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
