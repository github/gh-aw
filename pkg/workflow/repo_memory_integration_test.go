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

// TestRepoMemoryIntegrationSimple tests basic repo-memory workflow compilation
func TestRepoMemoryIntegrationSimple(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")
	workflowPath := filepath.Join(tmpDir, "test-workflow.md")

	content := `---
name: Test Repo Memory
on: workflow_dispatch
engine: copilot
tools:
  repo-memory: true
---

# Test Workflow

This workflow uses repo memory.
`

	if err := os.WriteFile(workflowPath, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(workflowPath); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	// Read the generated lock file
	lockPath := stringutil.MarkdownToLockFile(workflowPath)
	lockContent, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}
	lockFile := string(lockContent)

	// Check for clone step
	if !strings.Contains(lockFile, "Clone repo-memory branch (default)") {
		t.Error("Expected clone step in compiled workflow")
	}

	// Check for push step
	if !strings.Contains(lockFile, "Push repo-memory changes (default)") {
		t.Error("Expected push step in compiled workflow")
	}

	// Check for prompt
	if !strings.Contains(lockFile, "## Repo Memory Available") {
		t.Error("Expected repo memory prompt in compiled workflow")
	}

	// Check for memory directory path
	if !strings.Contains(lockFile, "/tmp/gh-aw/repo-memory/default") {
		t.Error("Expected memory directory path in compiled workflow")
	}
}

// TestRepoMemoryIntegrationCustomConfig tests repo-memory with custom configuration
func TestRepoMemoryIntegrationCustomConfig(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")
	workflowPath := filepath.Join(tmpDir, "test-workflow.md")

	content := `---
name: Test Repo Memory Custom
on: workflow_dispatch
engine: copilot
tools:
  repo-memory:
    target-repo: myorg/memory-repo
    branch-name: memory/agent-state
    max-file-size: 524288
    description: Agent state storage
---

# Test Workflow

This workflow uses custom repo memory configuration.
`

	if err := os.WriteFile(workflowPath, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(workflowPath); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	// Read the generated lock file
	lockPath := stringutil.MarkdownToLockFile(workflowPath)
	lockContent, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}
	lockFile := string(lockContent)

	// Check for custom branch name
	if !strings.Contains(lockFile, "memory/agent-state") {
		t.Error("Expected custom branch name in compiled workflow")
	}

	// Check for custom target repo
	if !strings.Contains(lockFile, "myorg/memory-repo") {
		t.Error("Expected custom target repo in compiled workflow")
	}

	// Check for custom description in prompt
	if !strings.Contains(lockFile, "Agent state storage") {
		t.Error("Expected custom description in prompt")
	}
}

// TestRepoMemoryIntegrationMultiple tests multiple repo-memory configurations
func TestRepoMemoryIntegrationMultiple(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")
	workflowPath := filepath.Join(tmpDir, "test-workflow.md")

	content := `---
name: Test Multiple Repo Memories
on: workflow_dispatch
engine: copilot
tools:
  repo-memory:
    - id: session
      branch-name: memory/session
      description: Session data
    - id: logs
      branch-name: memory/logs
      max-file-size: 2097152
---

# Test Workflow

This workflow uses multiple repo memories.
`

	if err := os.WriteFile(workflowPath, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(workflowPath); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	// Read the generated lock file
	lockPath := stringutil.MarkdownToLockFile(workflowPath)
	lockContent, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}
	lockFile := string(lockContent)

	// Check for both memory clones
	if !strings.Contains(lockFile, "Clone repo-memory branch (session)") {
		t.Error("Expected clone step for session memory")
	}
	if !strings.Contains(lockFile, "Clone repo-memory branch (logs)") {
		t.Error("Expected clone step for logs memory")
	}

	// Check for both memory pushes
	if !strings.Contains(lockFile, "Push repo-memory changes (session)") {
		t.Error("Expected push step for session memory")
	}
	if !strings.Contains(lockFile, "Push repo-memory changes (logs)") {
		t.Error("Expected push step for logs memory")
	}

	// Check for both directories
	if !strings.Contains(lockFile, "/tmp/gh-aw/repo-memory/session") {
		t.Error("Expected session memory directory")
	}
	if !strings.Contains(lockFile, "/tmp/gh-aw/repo-memory/logs") {
		t.Error("Expected logs memory directory")
	}

	// Check for plural form in prompt
	if !strings.Contains(lockFile, "## Repo Memory Locations Available") {
		t.Error("Expected plural form in prompt for multiple memories")
	}
}

// TestRepoMemoryIntegrationFileValidation tests file size and count validation
func TestRepoMemoryIntegrationFileValidation(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")
	workflowPath := filepath.Join(tmpDir, "test-workflow.md")

	content := `---
name: Test Repo Memory Validation
on: workflow_dispatch
engine: copilot
tools:
  repo-memory:
    max-file-size: 524288
    max-file-count: 50
---

# Test Workflow

This workflow has file validation.
`

	if err := os.WriteFile(workflowPath, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(workflowPath); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	// Read the generated lock file
	lockPath := stringutil.MarkdownToLockFile(workflowPath)
	lockContent, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}
	lockFile := string(lockContent)

	// Check for file size validation environment variable
	if !strings.Contains(lockFile, "MAX_FILE_SIZE: 524288") {
		t.Error("Expected file size validation in push step")
	}

	// Check for file count validation environment variable
	if !strings.Contains(lockFile, "MAX_FILE_COUNT: 50") {
		t.Error("Expected file count validation in push step")
	}

	// Check that push_repo_memory.cjs is being required (not inlined)
	if !strings.Contains(lockFile, "require('/opt/gh-aw/actions/push_repo_memory.cjs')") {
		t.Error("Expected push_repo_memory script to be loaded via require")
	}

	// Check for git user configuration
	if !strings.Contains(lockFile, "github-actions[bot]") {
		t.Error("Expected git user configuration as github-actions[bot]")
	}

	// Check constraints in prompt
	if !strings.Contains(lockFile, "**Constraints:**") {
		t.Error("Expected constraints section in prompt")
	}
}

// TestRepoMemoryDisabled tests that repo-memory can be disabled with false
func TestRepoMemoryDisabled(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")
	workflowPath := filepath.Join(tmpDir, "test-workflow.md")

	content := `---
name: Test Repo Memory Disabled
on: workflow_dispatch
engine: copilot
tools:
  repo-memory: false
---

# Test Workflow

This workflow has repo-memory disabled.
`

	if err := os.WriteFile(workflowPath, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(workflowPath); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	// Read the generated lock file
	lockPath := stringutil.MarkdownToLockFile(workflowPath)
	lockContent, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}
	lockFile := string(lockContent)

	// Check that repo-memory steps are NOT present
	if strings.Contains(lockFile, "Clone repo-memory branch") {
		t.Error("Should not have clone step when repo-memory is disabled")
	}

	if strings.Contains(lockFile, "Push repo-memory changes") {
		t.Error("Should not have push step when repo-memory is disabled")
	}

	if strings.Contains(lockFile, "## Repo Memory") {
		t.Error("Should not have repo memory prompt when disabled")
	}
}
