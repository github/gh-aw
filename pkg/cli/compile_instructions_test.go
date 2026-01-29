//go:build !integration

package cli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestCompileDoesNotWriteInstructions verifies that the compile command does not write instruction files
func TestCompileDoesNotWriteInstructions(t *testing.T) {
	// Create a temporary directory for the test
	tempDir, err := os.MkdirTemp("", "compile-instructions-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Change to temp directory
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}
	defer func() {
		_ = os.Chdir(oldWd)
	}()
	err = os.Chdir(tempDir)
	if err != nil {
		t.Fatalf("Failed to change directory: %v", err)
	}

	// Initialize git repo
	if err := exec.Command("git", "init").Run(); err != nil {
		t.Fatalf("Failed to init git repo: %v", err)
	}
	if err := exec.Command("git", "config", "user.email", "test@example.com").Run(); err != nil {
		t.Fatalf("Failed to configure git email: %v", err)
	}
	if err := exec.Command("git", "config", "user.name", "Test User").Run(); err != nil {
		t.Fatalf("Failed to configure git name: %v", err)
	}

	// Create .github/workflows directory
	workflowsDir := filepath.Join(tempDir, ".github", "workflows")
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatalf("Failed to create workflows directory: %v", err)
	}

	// Create a simple markdown workflow file
	workflowContent := `---
on: workflow_dispatch
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: claude
---

# Test Workflow

This is a test workflow for compilation.
`
	workflowPath := filepath.Join(workflowsDir, "test-workflow.md")
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	// Define paths for instruction files
	copilotInstructionsPath := filepath.Join(tempDir, ".github", "instructions", "github-agentic-workflows.instructions.md")
	agenticWorkflowAgentPath := filepath.Join(tempDir, ".github", "agents", "create-agentic-workflow.md")
	sharedAgenticWorkflowAgentPath := filepath.Join(tempDir, ".github", "agents", "create-shared-agentic-workflow.md")

	// Compile the workflow
	config := CompileConfig{
		MarkdownFiles:        []string{workflowPath},
		Verbose:              false,
		EngineOverride:       "",
		Validate:             false,
		Watch:                false,
		WorkflowDir:          "",
		SkipInstructions:     false, // Deprecated field, has no effect
		NoEmit:               false,
		Purge:                false,
		TrialMode:            false,
		TrialLogicalRepoSlug: "",
		Strict:               false,
	}

	_, err = CompileWorkflows(context.Background(), config)
	if err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	// Verify that the lock file was created
	lockFilePath := filepath.Join(workflowsDir, "test-workflow.lock.yml")
	if _, err := os.Stat(lockFilePath); os.IsNotExist(err) {
		t.Fatalf("Expected lock file to be created at %s", lockFilePath)
	}

	// Verify that instruction files were NOT created
	if _, err := os.Stat(copilotInstructionsPath); !os.IsNotExist(err) {
		t.Errorf("Expected copilot instructions file NOT to exist, but it was created at %s", copilotInstructionsPath)
	}

	if _, err := os.Stat(agenticWorkflowAgentPath); !os.IsNotExist(err) {
		t.Errorf("Expected agentic workflow agent file NOT to exist, but it was created at %s", agenticWorkflowAgentPath)
	}

	if _, err := os.Stat(sharedAgenticWorkflowAgentPath); !os.IsNotExist(err) {
		t.Errorf("Expected shared agentic workflow agent file NOT to exist, but it was created at %s", sharedAgenticWorkflowAgentPath)
	}
}

// TestCompileDoesNotWriteInstructionsWhenCompilingAll verifies that compiling all workflows does not write instruction files
func TestCompileDoesNotWriteInstructionsWhenCompilingAll(t *testing.T) {
	// Create a temporary directory for the test
	tempDir, err := os.MkdirTemp("", "compile-all-instructions-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Change to temp directory
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}
	defer func() {
		_ = os.Chdir(oldWd)
	}()
	err = os.Chdir(tempDir)
	if err != nil {
		t.Fatalf("Failed to change directory: %v", err)
	}

	// Initialize git repo
	if err := exec.Command("git", "init").Run(); err != nil {
		t.Fatalf("Failed to init git repo: %v", err)
	}
	if err := exec.Command("git", "config", "user.email", "test@example.com").Run(); err != nil {
		t.Fatalf("Failed to configure git email: %v", err)
	}
	if err := exec.Command("git", "config", "user.name", "Test User").Run(); err != nil {
		t.Fatalf("Failed to configure git name: %v", err)
	}

	// Create .github/workflows directory
	workflowsDir := filepath.Join(tempDir, ".github", "workflows")
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatalf("Failed to create workflows directory: %v", err)
	}

	// Create a simple markdown workflow file
	workflowContent := `---
on: workflow_dispatch
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: claude
---

# Test Workflow

This is a test workflow for compilation.
`
	workflowPath := filepath.Join(workflowsDir, "test-workflow.md")
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	// Define paths for instruction files
	copilotInstructionsPath := filepath.Join(tempDir, ".github", "instructions", "github-agentic-workflows.instructions.md")
	agenticWorkflowAgentPath := filepath.Join(tempDir, ".github", "agents", "create-agentic-workflow.md")
	sharedAgenticWorkflowAgentPath := filepath.Join(tempDir, ".github", "agents", "create-shared-agentic-workflow.md")

	// Compile all workflows (no specific files)
	config := CompileConfig{
		MarkdownFiles:        []string{}, // Empty means compile all
		Verbose:              false,
		EngineOverride:       "",
		Validate:             false,
		Watch:                false,
		WorkflowDir:          "",
		SkipInstructions:     false, // Deprecated field, has no effect
		NoEmit:               false,
		Purge:                false,
		TrialMode:            false,
		TrialLogicalRepoSlug: "",

		Strict: false,
	}

	_, err = CompileWorkflows(context.Background(), config)
	if err != nil {
		t.Fatalf("Failed to compile workflows: %v", err)
	}

	// Verify that the lock file was created
	lockFilePath := filepath.Join(workflowsDir, "test-workflow.lock.yml")
	if _, err := os.Stat(lockFilePath); os.IsNotExist(err) {
		t.Fatalf("Expected lock file to be created at %s", lockFilePath)
	}

	// Verify that instruction files were NOT created
	if _, err := os.Stat(copilotInstructionsPath); !os.IsNotExist(err) {
		t.Errorf("Expected copilot instructions file NOT to exist, but it was created at %s", copilotInstructionsPath)
	}

	if _, err := os.Stat(agenticWorkflowAgentPath); !os.IsNotExist(err) {
		t.Errorf("Expected agentic workflow agent file NOT to exist, but it was created at %s", agenticWorkflowAgentPath)
	}

	if _, err := os.Stat(sharedAgenticWorkflowAgentPath); !os.IsNotExist(err) {
		t.Errorf("Expected shared agentic workflow agent file NOT to exist, but it was created at %s", sharedAgenticWorkflowAgentPath)
	}
}
