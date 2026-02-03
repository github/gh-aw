//go:build integration

package workflow

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"

	"github.com/github/gh-aw/pkg/testutil"
)

// TestWebSearchValidationForCopilot tests that when a Copilot workflow uses web-search,
// compilation succeeds but emits a warning with documentation link
func TestWebSearchValidationForCopilot(t *testing.T) {
	// Create a temporary directory for the test
	tmpDir := testutil.TempDir(t, "test-*")

	// Create a test workflow that uses web-search with Copilot engine (which doesn't support web-search)
	workflowContent := `---
on: workflow_dispatch
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: copilot
tools:
  web-search:
---

# Test Workflow

Search the web for information.
`

	workflowPath := filepath.Join(tmpDir, "test-workflow.md")
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to write test workflow: %v", err)
	}

	// Capture stderr to verify warning message
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	// Create a compiler
	compiler := NewCompiler()

	// Compile the workflow - should succeed with a warning
	err := compiler.CompileWorkflow(workflowPath)

	// Restore stderr
	w.Close()
	os.Stderr = oldStderr

	// Read captured stderr
	var buf bytes.Buffer
	io.Copy(&buf, r)
	stderrOutput := buf.String()

	if err != nil {
		t.Fatalf("Expected compilation to succeed for Copilot engine with web-search tool (with warning), but got error: %v", err)
	}

	// Verify the warning message includes the documentation link
	if !strings.Contains(stderrOutput, "does not support the web-search tool") {
		t.Errorf("Expected warning about web-search not being supported, but got: %s", stderrOutput)
	}

	if !strings.Contains(stderrOutput, "https://github.github.com/gh-aw/guides/web-search/") {
		t.Errorf("Expected warning to include documentation link, but got: %s", stderrOutput)
	}

	// Verify the lock file was created
	lockFile := stringutil.MarkdownToLockFile(workflowPath)
	if _, err := os.Stat(lockFile); os.IsNotExist(err) {
		t.Fatal("Expected lock file to be created")
	}
}

// TestWebSearchValidationForClaude tests that when a Claude workflow uses web-search,
// compilation succeeds (because Claude has native support)
func TestWebSearchValidationForClaude(t *testing.T) {
	// Create a temporary directory for the test
	tmpDir := testutil.TempDir(t, "test-*")

	// Create a test workflow that uses web-search with Claude engine (which supports web-search)
	workflowContent := `---
on: workflow_dispatch
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: claude
tools:
  web-search:
---

# Test Workflow

Search the web for information.
`

	workflowPath := filepath.Join(tmpDir, "test-workflow.md")
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to write test workflow: %v", err)
	}

	// Create a compiler
	compiler := NewCompiler()

	// Compile the workflow - should succeed
	err := compiler.CompileWorkflow(workflowPath)
	if err != nil {
		t.Fatalf("Expected compilation to succeed for Claude engine with web-search tool, but got error: %v", err)
	}

	// Verify the lock file was created
	lockFile := stringutil.MarkdownToLockFile(workflowPath)
	if _, err := os.Stat(lockFile); os.IsNotExist(err) {
		t.Fatal("Expected lock file to be created")
	}

	// Read and verify the lock file contains web-search configuration
	lockContent, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	lockStr := string(lockContent)
	if !strings.Contains(lockStr, "WebSearch") {
		t.Errorf("Expected Claude workflow to have WebSearch in allowed tools, but it didn't")
	}
}

// TestWebSearchValidationForCodex tests that when a Codex workflow uses web-search,
// compilation succeeds (because Codex has native support)
func TestWebSearchValidationForCodex(t *testing.T) {
	// Create a temporary directory for the test
	tmpDir := testutil.TempDir(t, "test-*")

	// Create a test workflow that uses web-search with Codex engine (which supports web-search)
	workflowContent := `---
on: workflow_dispatch
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: codex
tools:
  web-search:
---

# Test Workflow

Search the web for information.
`

	workflowPath := filepath.Join(tmpDir, "test-workflow.md")
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to write test workflow: %v", err)
	}

	// Create a compiler
	compiler := NewCompiler()

	// Compile the workflow - should succeed
	err := compiler.CompileWorkflow(workflowPath)
	if err != nil {
		t.Fatalf("Expected compilation to succeed for Codex engine with web-search tool, but got error: %v", err)
	}

	// Verify the lock file was created
	lockFile := stringutil.MarkdownToLockFile(workflowPath)
	if _, err := os.Stat(lockFile); os.IsNotExist(err) {
		t.Fatal("Expected lock file to be created")
	}
}

// TestNoWebSearchNoValidation tests that when a workflow doesn't use web-search,
// compilation succeeds regardless of engine support
func TestNoWebSearchNoValidation(t *testing.T) {
	// Create a temporary directory for the test
	tmpDir := testutil.TempDir(t, "test-*")

	// Create a test workflow that doesn't use web-search with Copilot engine
	workflowContent := `---
on: workflow_dispatch
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: copilot
tools:
  github:
---

# Test Workflow

Do something without web search.
`

	workflowPath := filepath.Join(tmpDir, "test-workflow.md")
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to write test workflow: %v", err)
	}

	// Create a compiler
	compiler := NewCompiler()

	// Compile the workflow - should succeed
	err := compiler.CompileWorkflow(workflowPath)
	if err != nil {
		t.Fatalf("Expected compilation to succeed for workflow without web-search, but got error: %v", err)
	}
}
