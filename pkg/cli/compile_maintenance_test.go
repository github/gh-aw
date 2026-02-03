//go:build !integration

package cli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

func TestCompileSpecificFiles_GeneratesMaintenanceWorkflow(t *testing.T) {
	// Create temporary directory structure
	tempDir := testutil.TempDir(t, "test-*")
	workflowsDir := filepath.Join(tempDir, ".github/workflows")
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatalf("Failed to create workflows directory: %v", err)
	}

	// Change to temp directory
	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("Failed to change directory: %v", err)
	}

	// Initialize git repo
	initCmd := exec.Command("git", "init")
	initCmd.Dir = tempDir
	if err := initCmd.Run(); err != nil {
		t.Fatalf("Failed to initialize git repo: %v", err)
	}

	// Create a workflow with expires field
	workflowContent := `---
name: "Test Workflow with Expires"
on:
  workflow_dispatch:
engine: copilot
safe-outputs:
  create-issue:
    max: 1
    expires: 24
---

Test workflow that creates issues with expiration.
`
	workflowPath := filepath.Join(workflowsDir, "test-expires.md")
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	// Compile all workflows in the directory (maintenance workflow is only generated
	// when compiling entire directory, not specific files)
	config := CompileConfig{
		MarkdownFiles:        []string{}, // Empty = compile all
		Verbose:              false,
		EngineOverride:       "",
		Validate:             false,
		Watch:                false,
		WorkflowDir:          "", // Use default directory (empty = .github/workflows)
		SkipInstructions:     false,
		NoEmit:               false,
		Purge:                false,
		TrialMode:            false,
		TrialLogicalRepoSlug: "",
		Strict:               false,
	}

	_, err := CompileWorkflows(context.Background(), config)
	if err != nil {
		t.Fatalf("CompileWorkflows failed: %v", err)
	}

	// Verify that the maintenance workflow was generated
	maintenancePath := filepath.Join(workflowsDir, "agentics-maintenance.yml")
	if _, err := os.Stat(maintenancePath); os.IsNotExist(err) {
		t.Error("Expected maintenance workflow to be generated, but it was not")
	} else if err != nil {
		t.Errorf("Error checking maintenance workflow: %v", err)
	}

	// Read the maintenance workflow and verify it contains expected content
	if content, err := os.ReadFile(maintenancePath); err == nil {
		contentStr := string(content)
		if !strings.Contains(contentStr, "Agentic Maintenance") {
			t.Error("Maintenance workflow does not contain expected workflow name 'Agentic Maintenance'")
		}
		if !strings.Contains(contentStr, "schedule:") {
			t.Error("Maintenance workflow does not contain schedule trigger")
		}
	} else {
		t.Errorf("Failed to read maintenance workflow: %v", err)
	}
}

func TestCompileSpecificFiles_DeletesMaintenanceWorkflow(t *testing.T) {
	// Create temporary directory structure
	tempDir := testutil.TempDir(t, "test-*")
	workflowsDir := filepath.Join(tempDir, ".github/workflows")
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatalf("Failed to create workflows directory: %v", err)
	}

	// Change to temp directory
	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("Failed to change directory: %v", err)
	}

	// Initialize git repo
	initCmd := exec.Command("git", "init")
	initCmd.Dir = tempDir
	if err := initCmd.Run(); err != nil {
		t.Fatalf("Failed to initialize git repo: %v", err)
	}

	// Create a maintenance workflow file manually
	maintenancePath := filepath.Join(workflowsDir, "agentics-maintenance.yml")
	maintenanceContent := `name: agentics-maintenance
on:
  schedule:
    - cron: '37 0 * * *'
`
	if err := os.WriteFile(maintenancePath, []byte(maintenanceContent), 0644); err != nil {
		t.Fatalf("Failed to create maintenance workflow: %v", err)
	}

	// Create a workflow WITHOUT expires field
	workflowContent := `---
name: "Test Workflow No Expires"
on:
  workflow_dispatch:
engine: copilot
safe-outputs:
  create-issue:
    max: 1
---

Test workflow that creates issues without expiration.
`
	workflowPath := filepath.Join(workflowsDir, "test-no-expires.md")
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	// Compile all workflows in the directory (maintenance workflow is only generated
	// when compiling entire directory, not specific files)
	config := CompileConfig{
		MarkdownFiles:        []string{}, // Empty = compile all
		Verbose:              false,
		EngineOverride:       "",
		Validate:             false,
		Watch:                false,
		WorkflowDir:          "", // Use default directory (empty = .github/workflows)
		SkipInstructions:     false,
		NoEmit:               false,
		Purge:                false,
		TrialMode:            false,
		TrialLogicalRepoSlug: "",
		Strict:               false,
	}

	_, err := CompileWorkflows(context.Background(), config)
	if err != nil {
		t.Fatalf("CompileWorkflows failed: %v", err)
	}

	// Verify that the maintenance workflow WAS deleted
	// When compiling specific files, we parse ALL workflows in the directory
	// and if NONE of them have expires, the maintenance workflow should be deleted
	if _, err := os.Stat(maintenancePath); !os.IsNotExist(err) {
		t.Error("Maintenance workflow should be deleted when no workflows have expires field")
	}
}

func TestCompileWithCustomDir_SkipsMaintenanceWorkflow(t *testing.T) {
	// Create temporary directory structure
	tempDir := testutil.TempDir(t, "test-*")
	customDir := filepath.Join(tempDir, "custom/workflows")
	if err := os.MkdirAll(customDir, 0755); err != nil {
		t.Fatalf("Failed to create custom workflows directory: %v", err)
	}

	// Change to temp directory
	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("Failed to change directory: %v", err)
	}

	// Initialize git repo
	initCmd := exec.Command("git", "init")
	initCmd.Dir = tempDir
	if err := initCmd.Run(); err != nil {
		t.Fatalf("Failed to initialize git repo: %v", err)
	}

	// Create a workflow with expires field in custom directory
	workflowContent := `---
name: "Test Workflow with Expires"
on:
  workflow_dispatch:
engine: copilot
safe-outputs:
  create-issue:
    max: 1
    expires: 24
---

Test workflow that creates issues with expiration.
`
	workflowPath := filepath.Join(customDir, "test-expires.md")
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	// Compile all workflows in custom --dir
	config := CompileConfig{
		MarkdownFiles:        []string{}, // Empty = compile all files in directory
		Verbose:              false,
		EngineOverride:       "",
		Validate:             false,
		Watch:                false,
		WorkflowDir:          "custom/workflows", // Custom directory
		SkipInstructions:     false,
		NoEmit:               false,
		Purge:                false,
		TrialMode:            false,
		TrialLogicalRepoSlug: "",
		Strict:               false,
	}

	_, err := CompileWorkflows(context.Background(), config)
	if err != nil {
		t.Fatalf("CompileWorkflows failed: %v", err)
	}

	// Verify that the maintenance workflow was NOT generated in custom directory
	maintenancePath := filepath.Join(customDir, "agentics-maintenance.yml")
	if _, err := os.Stat(maintenancePath); !os.IsNotExist(err) {
		t.Error("Maintenance workflow should NOT be generated when using custom --dir option")
	}
}
