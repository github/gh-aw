//go:build integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCreatePullRequestWithCustomBaseBranch tests end-to-end workflow compilation with custom base-branch
func TestCreatePullRequestWithCustomBaseBranch(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "base-branch-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create test workflow with custom base-branch
	workflowContent := `---
on: push
permissions:
  contents: read
  actions: read
  issues: read
  pull-requests: read
engine: copilot
safe-outputs:
  create-pull-request:
    target-repo: "microsoft/vscode-docs"
    base-branch: vnext
    draft: true
---

# Test Workflow

Create a pull request targeting vnext branch in cross-repo.
`

	workflowPath := filepath.Join(tmpDir, "test-workflow.md")
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	// Compile the workflow
	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(workflowPath); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	// Read the compiled output
	outputFile := filepath.Join(tmpDir, "test-workflow.lock.yml")
	compiledBytes, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("Failed to read compiled output: %v", err)
	}

	compiledContent := string(compiledBytes)

	// Verify GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG contains base_branch set to "vnext"
	// The JSON is escaped in YAML, so we need to look for the escaped version
	if !strings.Contains(compiledContent, `\"base_branch\":\"vnext\"`) {
		t.Error("Expected handler config to contain base_branch set to vnext in compiled workflow")
	}

	// Verify it does NOT contain the default github.ref_name expression
	if strings.Contains(compiledContent, `\"base_branch\":\"${{ github.ref_name }}\"`) {
		t.Error("Did not expect handler config to use github.ref_name when base-branch is explicitly set")
	}
}

// TestCreatePullRequestWithDefaultBaseBranch tests workflow compilation with default base-branch
func TestCreatePullRequestWithDefaultBaseBranch(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "default-base-branch-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create test workflow without base-branch field
	workflowContent := `---
on: push
permissions:
  contents: read
  actions: read
  issues: read
  pull-requests: read
engine: copilot
safe-outputs:
  create-pull-request:
    draft: true
---

# Test Workflow

Create a pull request with default base branch.
`

	workflowPath := filepath.Join(tmpDir, "test-default.md")
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	// Compile the workflow
	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(workflowPath); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	// Read the compiled output
	outputFile := filepath.Join(tmpDir, "test-default.lock.yml")
	compiledBytes, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("Failed to read compiled output: %v", err)
	}

	compiledContent := string(compiledBytes)

	// Verify GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG uses github.ref_name by default
	// The JSON is escaped in YAML, so we need to look for the escaped version
	if !strings.Contains(compiledContent, `\"base_branch\":\"${{ github.ref_name }}\"`) {
		t.Error("Expected handler config to use github.ref_name when base-branch is not specified")
	}
}

// TestCreatePullRequestWithBranchSlash tests workflow compilation with branch containing slash
func TestCreatePullRequestWithBranchSlash(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "branch-slash-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create test workflow with base-branch containing slash
	workflowContent := `---
on: push
permissions:
  contents: read
  actions: read
  issues: read
  pull-requests: read
engine: copilot
safe-outputs:
  create-pull-request:
    base-branch: release/v1.0
    draft: true
---

# Test Workflow

Create a pull request targeting release/v1.0 branch.
`

	workflowPath := filepath.Join(tmpDir, "test-slash.md")
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	// Compile the workflow
	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(workflowPath); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	// Read the compiled output
	outputFile := filepath.Join(tmpDir, "test-slash.lock.yml")
	compiledBytes, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("Failed to read compiled output: %v", err)
	}

	compiledContent := string(compiledBytes)

	// Verify GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG contains base_branch set to "release/v1.0"
	// The JSON is escaped in YAML, so we need to look for the escaped version
	if !strings.Contains(compiledContent, `\"base_branch\":\"release/v1.0\"`) {
		t.Error("Expected handler config to contain base_branch set to release/v1.0 in compiled workflow")
	}
}
