//go:build !integration

package workflow_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/github/gh-aw/pkg/workflow"
)

// TestImportWithInputs tests that imports with inputs correctly substitute values
func TestImportWithInputs(t *testing.T) {
	// Create a temporary directory for test files
	tempDir := testutil.TempDir(t, "test-import-inputs-*")

	// Create shared workflow file with inputs
	sharedPath := filepath.Join(tempDir, "shared", "data-fetch.md")
	sharedDir := filepath.Dir(sharedPath)
	if err := os.MkdirAll(sharedDir, 0755); err != nil {
		t.Fatalf("Failed to create shared directory: %v", err)
	}

	sharedContent := `---
inputs:
  count:
    description: Number of items to fetch
    type: number
    default: 100
  category:
    description: Category to filter
    type: string
    default: "general"
---

# Data Fetch Instructions

Fetch ${{ github.aw.inputs.count }} items from the ${{ github.aw.inputs.category }} category.
`
	if err := os.WriteFile(sharedPath, []byte(sharedContent), 0644); err != nil {
		t.Fatalf("Failed to write shared file: %v", err)
	}

	// Create main workflow that imports shared with inputs
	workflowPath := filepath.Join(tempDir, "test-workflow.md")
	workflowContent := `---
on: issues
permissions:
  contents: read
  issues: read
engine: copilot
imports:
  - path: shared/data-fetch.md
    inputs:
      count: 50
      category: "technology"
---

# Test Workflow

This workflow tests import with inputs.
`
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	// Compile the workflow
	compiler := workflow.NewCompiler()
	if err := compiler.CompileWorkflow(workflowPath); err != nil {
		t.Fatalf("CompileWorkflow failed: %v", err)
	}

	// Read the generated lock file
	lockFilePath := stringutil.MarkdownToLockFile(workflowPath)
	lockFileContent, err := os.ReadFile(lockFilePath)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	lockContent := string(lockFileContent)

	// The prompt is assembled from multiple heredocs:
	// - the first heredoc is often just the <system> wrapper and built-in context
	// - user/imported content is appended in later heredocs
	// So validate substitutions against the lock content as a whole.
	if !strings.Contains(lockContent, "Fetch 50 items") {
		t.Errorf("Expected generated workflow to include substituted count value '50' in prompt content")
	}
	if !strings.Contains(lockContent, "technology") {
		t.Errorf("Expected generated workflow to include substituted category value 'technology' in prompt content")
	}

	// Ensure github.aw.inputs.* expressions were substituted during compilation.
	if strings.Contains(lockContent, "github.aw.inputs.count") {
		t.Error("Generated workflow should not contain unsubstituted github.aw.inputs.count expression")
	}
	if strings.Contains(lockContent, "github.aw.inputs.category") {
		t.Error("Generated workflow should not contain unsubstituted github.aw.inputs.category expression")
	}
}

// TestImportInputsForwardedToNestedImports tests that ${{ github.aw.import-inputs.* }}
// expressions in the imports: section of a shared workflow are resolved before nested
// imports are processed. This enables multi-level workflow composition where a shared
// workflow can forward its own inputs to the workflows it depends on.
func TestImportInputsForwardedToNestedImports(t *testing.T) {
	tempDir := testutil.TempDir(t, "test-import-inputs-forwarding-*")

	sharedDir := filepath.Join(tempDir, "shared")
	if err := os.MkdirAll(sharedDir, 0755); err != nil {
		t.Fatalf("Failed to create shared directory: %v", err)
	}

	// Create the leaf shared workflow that accepts a branch-name input
	leafPath := filepath.Join(sharedDir, "repo-memory.md")
	leafContent := `---
import-schema:
  branch-name:
    type: string
    required: true
    description: "Branch name for storage"
tools:
  bash:
    - "git *"
---

Store data in branch ${{ github.aw.import-inputs.branch-name }}.
`
	if err := os.WriteFile(leafPath, []byte(leafContent), 0644); err != nil {
		t.Fatalf("Failed to write leaf file: %v", err)
	}

	// Create the intermediate shared workflow that accepts branch-name and forwards it
	// to the leaf workflow via an expression in its own imports: section
	intermediatePath := filepath.Join(sharedDir, "daily-report.md")
	intermediateContent := `---
import-schema:
  branch-name:
    type: string
    required: true
    description: "Branch name for repo-memory storage"

imports:
  - uses: shared/repo-memory.md
    with:
      branch-name: ${{ github.aw.import-inputs.branch-name }}
---

Daily report workflow.
`
	if err := os.WriteFile(intermediatePath, []byte(intermediateContent), 0644); err != nil {
		t.Fatalf("Failed to write intermediate file: %v", err)
	}

	// Create the consuming workflow that imports the intermediate workflow with a concrete value
	workflowPath := filepath.Join(tempDir, "consumer.md")
	workflowContent := `---
on: issues
permissions:
  contents: read
  issues: read
engine: copilot
imports:
  - uses: shared/daily-report.md
    with:
      branch-name: "memory/my-workflow"
---

Consumer workflow.
`
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to write consumer workflow: %v", err)
	}

	compiler := workflow.NewCompiler()
	if err := compiler.CompileWorkflow(workflowPath); err != nil {
		t.Fatalf("CompileWorkflow failed: %v", err)
	}

	lockFilePath := stringutil.MarkdownToLockFile(workflowPath)
	lockFileContent, err := os.ReadFile(lockFilePath)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}
	lockContent := compiledWorkflowBody(string(lockFileContent))

	// The leaf workflow's tools (bash) should be merged.
	// "git *" normalizes to the canonical stem form shell(git:*) at compile time.
	if !strings.Contains(lockContent, "git:*") {
		t.Error("Expected lock file to contain bash tool from leaf workflow (normalized to git:*)")
	}

	// The substituted branch-name value should appear in the compiled output
	if !strings.Contains(lockContent, "memory/my-workflow") {
		t.Error("Expected compiled workflow to contain forwarded branch-name value 'memory/my-workflow'")
	}

	// No unresolved import-inputs expressions should remain
	if strings.Contains(lockContent, "github.aw.import-inputs.branch-name") {
		t.Error("Generated workflow should not contain unsubstituted github.aw.import-inputs.branch-name expression")
	}
}

func TestImportPromptOrderInterleavesRuntimeAndParameterizedImports(t *testing.T) {
	tempDir := testutil.TempDir(t, "test-import-order-*")

	sharedDir := filepath.Join(tempDir, "shared")
	if err := os.MkdirAll(sharedDir, 0755); err != nil {
		t.Fatalf("Failed to create shared directory: %v", err)
	}

	runtimeImportPath := filepath.Join(sharedDir, "infra.md")
	runtimeImportContent := `---
steps:
  - name: Setup
    run: echo setup
---

# Infrastructure Setup

Ensure environment is ready.`
	if err := os.WriteFile(runtimeImportPath, []byte(runtimeImportContent), 0o644); err != nil {
		t.Fatalf("Failed to write runtime import file: %v", err)
	}

	parameterizedImportPath := filepath.Join(sharedDir, "instructions.md")
	parameterizedImportContent := `---
import-schema:
  mode:
    type: string
    required: true
---

# Mode Instructions

Use mode: ${{ github.aw.import-inputs.mode }}.`
	if err := os.WriteFile(parameterizedImportPath, []byte(parameterizedImportContent), 0o644); err != nil {
		t.Fatalf("Failed to write parameterized import file: %v", err)
	}

	workflowPath := filepath.Join(tempDir, "order-workflow.md")
	workflowContent := `---
on: issues
permissions:
  contents: read
  issues: read
engine: copilot
imports:
  - shared/infra.md
  - uses: shared/instructions.md
    with:
      mode: strict
---

# Main Workflow

Handle the issue.`
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0o644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	compiler := workflow.NewCompiler()
	if err := compiler.CompileWorkflow(workflowPath); err != nil {
		t.Fatalf("CompileWorkflow failed: %v", err)
	}

	lockFilePath := stringutil.MarkdownToLockFile(workflowPath)
	lockFileContent, err := os.ReadFile(lockFilePath)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}
	lockContent := string(lockFileContent)

	runtimeMacro := "{{#runtime-import shared/infra.md}}"
	parameterizedContent := "Use mode: strict."

	runtimeIdx := strings.Index(lockContent, runtimeMacro)
	if runtimeIdx == -1 {
		t.Fatalf("Expected lock file to contain %q", runtimeMacro)
	}
	parameterizedIdx := strings.Index(lockContent, parameterizedContent)
	if parameterizedIdx == -1 {
		t.Fatalf("Expected lock file to contain substituted parameterized import content %q", parameterizedContent)
	}
	if runtimeIdx > parameterizedIdx {
		t.Fatalf("Expected runtime import macro to appear before parameterized import content (runtimeIdx=%d, parameterizedIdx=%d)", runtimeIdx, parameterizedIdx)
	}
}

// TestImportWithInputsStringFormat tests that string import format still works
func TestImportWithInputsStringFormat(t *testing.T) {
	// Create a temporary directory for test files
	tempDir := testutil.TempDir(t, "test-import-string-*")

	// Create shared workflow file (no inputs needed for this test)
	sharedPath := filepath.Join(tempDir, "shared", "simple.md")
	sharedDir := filepath.Dir(sharedPath)
	if err := os.MkdirAll(sharedDir, 0755); err != nil {
		t.Fatalf("Failed to create shared directory: %v", err)
	}

	sharedContent := `---
tools:
  bash:
    - "echo *"
---

# Simple Shared Instructions

Do something simple.
`
	if err := os.WriteFile(sharedPath, []byte(sharedContent), 0644); err != nil {
		t.Fatalf("Failed to write shared file: %v", err)
	}

	// Create main workflow that imports using string format
	workflowPath := filepath.Join(tempDir, "test-workflow.md")
	workflowContent := `---
on: issues
permissions:
  contents: read
  issues: read
engine: copilot
imports:
  - shared/simple.md
---

# Test Workflow

This workflow tests that string imports still work.
`
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	// Compile the workflow
	compiler := workflow.NewCompiler()
	if err := compiler.CompileWorkflow(workflowPath); err != nil {
		t.Fatalf("CompileWorkflow failed: %v", err)
	}

	// Read the generated lock file
	lockFilePath := stringutil.MarkdownToLockFile(workflowPath)
	lockFileContent, err := os.ReadFile(lockFilePath)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	lockContent := string(lockFileContent)

	// With runtime-import approach, imports without inputs use runtime-import macros
	// (not inlined content)
	if !strings.Contains(lockContent, "{{#runtime-import shared/simple.md}}") {
		t.Error("Expected lock file to contain runtime-import macro for shared workflow")
	}

	// The content should NOT be inlined (it's loaded at runtime)
	if strings.Contains(lockContent, "Simple Shared Instructions") {
		t.Error("Expected shared content to NOT be inlined (should use runtime-import)")
	}
}

// TestImportWithArrayInputs tests that array-typed import-inputs are serialized as
// JSON (e.g. ["a","b"]) rather than the Go default slice format ("[a b]").
// This is the regression test for the bug where goccy/go-yaml returns []string
// instead of []any, causing the Go fmt.Sprintf fallback to produce "[a b]".
func TestImportWithArrayInputs(t *testing.T) {
	tempDir := testutil.TempDir(t, "test-import-array-inputs-*")

	sharedDir := filepath.Join(tempDir, "shared")
	if err := os.MkdirAll(sharedDir, 0755); err != nil {
		t.Fatalf("Failed to create shared directory: %v", err)
	}

	// Shared workflow that accepts an array input and references it in a run: step
	sharedContent := `---
import-schema:
  packages:
    type: array
    items:
      type: string
    required: true
    description: "List of packages"
on: workflow_call
jobs:
  show:
    runs-on: ubuntu-latest
    steps:
      - env:
          PKGS: ${{ github.aw.import-inputs.packages }}
        run: echo "$PKGS"
---

Process packages ${{ github.aw.import-inputs.packages }}.
`
	sharedPath := filepath.Join(sharedDir, "pkgs.md")
	if err := os.WriteFile(sharedPath, []byte(sharedContent), 0644); err != nil {
		t.Fatalf("Failed to write shared file: %v", err)
	}

	// Caller workflow that provides a YAML array for the 'packages' input
	callerContent := `---
on: push
permissions:
  contents: read
engine: copilot
imports:
  - uses: shared/pkgs.md
    with:
      packages:
        - microsoft/apm#main
        - github/awesome-copilot/skills/foo
---

Caller workflow.
`
	callerPath := filepath.Join(tempDir, "caller.md")
	if err := os.WriteFile(callerPath, []byte(callerContent), 0644); err != nil {
		t.Fatalf("Failed to write caller file: %v", err)
	}

	compiler := workflow.NewCompiler()
	if err := compiler.CompileWorkflow(callerPath); err != nil {
		t.Fatalf("CompileWorkflow failed: %v", err)
	}

	lockFilePath := stringutil.MarkdownToLockFile(callerPath)
	lockFileContent, err := os.ReadFile(lockFilePath)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}
	lockContent := string(lockFileContent)

	// The substituted value must be valid JSON, not the Go slice format "[a b]"
	if strings.Contains(lockContent, "[microsoft/apm#main github/awesome-copilot/skills/foo]") {
		t.Error("Array input was serialized as Go slice format '[a b]'; expected JSON array")
	}
	if !strings.Contains(lockContent, `[\"microsoft/apm#main\",\"github/awesome-copilot/skills/foo\"]`) {
		t.Errorf("Expected JSON array in lock file, got:\n%s", lockContent)
	}

	// No unresolved expressions should remain
	if strings.Contains(lockContent, "github.aw.import-inputs.packages") {
		t.Error("Unresolved import-inputs expression remained in lock file")
	}
}
