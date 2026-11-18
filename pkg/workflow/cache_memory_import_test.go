//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"

	"github.com/github/gh-aw/pkg/testutil"
)

func TestCacheMemoryImportOnly(t *testing.T) {
	// Create a temporary directory
	tmpDir := testutil.TempDir(t, "test-*")

	// Create a shared workflow directory
	sharedDir := filepath.Join(tmpDir, ".github", "workflows", "shared")
	if err := os.MkdirAll(sharedDir, 0755); err != nil {
		t.Fatalf("Failed to create shared directory: %v", err)
	}

	// Write a shared workflow with cache-memory configuration
	sharedPath := filepath.Join(sharedDir, "cache-config.md")
	sharedContent := `---
tools:
  cache-memory:
    - id: session
      key: shared-session
    - id: logs
      key: shared-logs
---

# Shared Cache Configuration
`
	if err := os.WriteFile(sharedPath, []byte(sharedContent), 0644); err != nil {
		t.Fatalf("Failed to write shared workflow file: %v", err)
	}

	// Write the main workflow that imports the shared config WITHOUT defining its own cache-memory
	mainPath := filepath.Join(tmpDir, ".github", "workflows", "main.md")
	mainContent := `---
name: Test Import Only
on: workflow_dispatch
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: claude
imports:
  - shared/cache-config.md
tools:
  github:
    allowed: [get_file_contents]
---

# Main Workflow

Test cache-memory import without local definition.
`
	if err := os.WriteFile(mainPath, []byte(mainContent), 0644); err != nil {
		t.Fatalf("Failed to write main workflow file: %v", err)
	}

	// Compile the workflow
	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(mainPath); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	// Read the generated lock file
	lockPath := stringutil.MarkdownToLockFile(mainPath)
	lockContent, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}
	lockStr := string(lockContent)

	// We expect the imported caches to be present
	// Custom keys now get the integrity/policy prefix to prevent cross-integrity cache sharing
	expectedStrings := []string{
		"- name: Create cache-memory directory (session)",
		"path: .gh-aw-cache-v1-29adc3a27b5dcc3e7281f68a/runtime.tar",
		"key: memory-none-nopolicy-shared-session-${{ github.run_id }}",
		"- name: Create cache-memory directory (logs)",
		"path: .gh-aw-cache-v1-da21cb7d62936d1a863f0f95/runtime.tar",
		"key: memory-none-nopolicy-shared-logs-${{ github.run_id }}",
		"cache_memory_prompt_multi.md", // Template file reference instead of literal content
		"- **session**: `" + runtimeRootExpression + "/cache-memory-session/`",
		"- **logs**: `" + runtimeRootExpression + "/cache-memory-logs/`",
	}

	for _, expected := range expectedStrings {
		if !strings.Contains(lockStr, expected) {
			t.Errorf("Expected to find '%s' in lock file but it was missing", expected)
		}
	}
}
