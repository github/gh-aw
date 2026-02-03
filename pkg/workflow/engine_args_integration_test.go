//go:build integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

func TestEngineArgsIntegration(t *testing.T) {
	// Create a temporary directory for the test
	tmpDir := testutil.TempDir(t, "test-*")

	// Create a test workflow with engine args
	workflowContent := `---
on: workflow_dispatch
engine:
  id: copilot
  args: ["--add-dir", "/"]
---

# Test Workflow

This is a test workflow to verify engine args injection.
`

	workflowPath := filepath.Join(tmpDir, "test-workflow.md")
	err := os.WriteFile(workflowPath, []byte(workflowContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	// Compile the workflow
	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(workflowPath); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	// Read the generated lock file
	lockFile := filepath.Join(tmpDir, "test-workflow.lock.yml")
	content, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read generated lock file: %v", err)
	}

	result := string(content)

	// Check that the compiled YAML contains the custom args
	if !strings.Contains(result, "--add-dir /") {
		t.Errorf("Expected compiled YAML to contain '--add-dir /', got:\n%s", result)
	}

	// Verify args come before --prompt
	addDirIdx := strings.Index(result, "--add-dir /")
	promptIdx := strings.Index(result, "--prompt")
	if addDirIdx == -1 || promptIdx == -1 {
		t.Fatal("Could not find both --add-dir and --prompt in compiled YAML")
	}
	if addDirIdx > promptIdx {
		t.Error("Expected --add-dir to come before --prompt in compiled YAML")
	}
}

func TestEngineArgsIntegrationMultipleArgs(t *testing.T) {
	// Create a temporary directory for the test
	tmpDir := testutil.TempDir(t, "test-*")

	// Create a test workflow with multiple engine args
	workflowContent := `---
on: workflow_dispatch
engine:
  id: copilot
  args: ["--add-dir", "/workspace", "--verbose"]
---

# Test Workflow with Multiple Args

This is a test workflow to verify multiple engine args injection.
`

	workflowPath := filepath.Join(tmpDir, "test-workflow.md")
	err := os.WriteFile(workflowPath, []byte(workflowContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	// Compile the workflow
	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(workflowPath); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	// Read the generated lock file
	lockFile := filepath.Join(tmpDir, "test-workflow.lock.yml")
	content, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read generated lock file: %v", err)
	}

	result := string(content)

	// Check that the compiled YAML contains all custom args
	if !strings.Contains(result, "--add-dir /workspace") {
		t.Errorf("Expected compiled YAML to contain '--add-dir /workspace'")
	}
	if !strings.Contains(result, "--verbose") {
		t.Errorf("Expected compiled YAML to contain '--verbose'")
	}

	// Verify args come before --prompt
	verboseIdx := strings.Index(result, "--verbose")
	promptIdx := strings.Index(result, "--prompt")
	if verboseIdx == -1 || promptIdx == -1 {
		t.Fatal("Could not find both --verbose and --prompt in compiled YAML")
	}
	if verboseIdx > promptIdx {
		t.Error("Expected --verbose to come before --prompt in compiled YAML")
	}
}

func TestEngineArgsIntegrationNoArgs(t *testing.T) {
	// Create a temporary directory for the test
	tmpDir := testutil.TempDir(t, "test-*")

	// Create a test workflow without engine args
	workflowContent := `---
on: workflow_dispatch
engine:
  id: copilot
---

# Test Workflow without Args

This is a test workflow without engine args.
`

	workflowPath := filepath.Join(tmpDir, "test-workflow.md")
	err := os.WriteFile(workflowPath, []byte(workflowContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	// Compile the workflow
	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(workflowPath); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	// Read the generated lock file
	lockFile := filepath.Join(tmpDir, "test-workflow.lock.yml")
	content, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read generated lock file: %v", err)
	}

	result := string(content)

	// Should still have the --prompt flag
	if !strings.Contains(result, "--prompt") {
		t.Errorf("Expected compiled YAML to contain '--prompt'")
	}

	// Verify the workflow compiles successfully
	if result == "" {
		t.Error("Expected non-empty compiled YAML")
	}
}

func TestEngineArgsIntegrationClaude(t *testing.T) {
	// Create a temporary directory for the test
	tmpDir := testutil.TempDir(t, "test-*")

	// Create a test workflow with claude engine args
	workflowContent := `---
on: workflow_dispatch
engine:
  id: claude
  args: ["--custom-flag", "value"]
---

# Test Workflow with Claude Args

This is a test workflow to verify claude engine args injection.
`

	workflowPath := filepath.Join(tmpDir, "test-workflow.md")
	err := os.WriteFile(workflowPath, []byte(workflowContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	// Compile the workflow
	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(workflowPath); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	// Read the generated lock file
	lockFile := filepath.Join(tmpDir, "test-workflow.lock.yml")
	content, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read generated lock file: %v", err)
	}

	result := string(content)

	// Check that the compiled YAML contains the custom args
	if !strings.Contains(result, "--custom-flag") {
		t.Errorf("Expected compiled YAML to contain '--custom-flag'")
	}
	if !strings.Contains(result, "value") {
		t.Errorf("Expected compiled YAML to contain 'value'")
	}
}

func TestEngineArgsIntegrationCodex(t *testing.T) {
	// Create a temporary directory for the test
	tmpDir := testutil.TempDir(t, "test-*")

	// Create a test workflow with codex engine args
	workflowContent := `---
on: workflow_dispatch
engine:
  id: codex
  args: ["--custom-flag", "value"]
---

# Test Workflow with Codex Args

This is a test workflow to verify codex engine args injection.
`

	workflowPath := filepath.Join(tmpDir, "test-workflow.md")
	err := os.WriteFile(workflowPath, []byte(workflowContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	// Compile the workflow
	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(workflowPath); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	// Read the generated lock file
	lockFile := filepath.Join(tmpDir, "test-workflow.lock.yml")
	content, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read generated lock file: %v", err)
	}

	result := string(content)

	// Check that the compiled YAML contains the custom args before INSTRUCTION
	if !strings.Contains(result, "--custom-flag value") {
		t.Errorf("Expected compiled YAML to contain '--custom-flag value'")
	}

	// Verify args come before "$INSTRUCTION"
	customFlagIdx := strings.Index(result, "--custom-flag value")
	instructionIdx := strings.Index(result, "\"$INSTRUCTION\"")
	if customFlagIdx == -1 || instructionIdx == -1 {
		t.Fatal("Could not find both --custom-flag and $INSTRUCTION in compiled YAML")
	}
	if customFlagIdx > instructionIdx {
		t.Error("Expected --custom-flag to come before $INSTRUCTION in compiled YAML")
	}
}
