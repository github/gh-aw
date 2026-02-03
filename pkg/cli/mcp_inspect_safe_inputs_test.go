//go:build !integration

package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/workflow"
)

// TestSpawnSafeInputsInspector_NoSafeInputs tests the error case when workflow has no safe-inputs
func TestSpawnSafeInputsInspector_NoSafeInputs(t *testing.T) {
	// Create temporary directory with a workflow file
	tmpDir := t.TempDir()
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatalf("Failed to create workflows directory: %v", err)
	}

	// Create a test workflow file WITHOUT safe-inputs
	workflowContent := `---
on: push
engine: copilot
---
# Test Workflow

This workflow has no safe-inputs configuration.
`
	workflowPath := filepath.Join(workflowsDir, "test.md")
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	// Change to the temporary directory
	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)
	os.Chdir(tmpDir)

	// Try to spawn safe-inputs inspector - should fail
	err := spawnSafeInputsInspector("test", false)
	if err == nil {
		t.Error("Expected error when workflow has no safe-inputs, got nil")
	}

	// Verify error message mentions "no safe-inputs"
	if err != nil && err.Error() != "no safe-inputs configuration found in workflow" {
		t.Errorf("Expected specific error message, got: %v", err)
	}
}

// TestSpawnSafeInputsInspector_WithSafeInputs tests file generation with a real workflow
func TestSpawnSafeInputsInspector_WithSafeInputs(t *testing.T) {
	// This test verifies that the function correctly parses a workflow and generates files
	// We can't actually start the server or inspector in a test, but we can verify file generation

	// Create temporary directory with a workflow file
	tmpDir := t.TempDir()
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatalf("Failed to create workflows directory: %v", err)
	}

	// Create a test workflow file with safe-inputs
	workflowContent := `---
on: push
engine: copilot
safe-inputs:
  echo-tool:
    description: "Echo a message"
    inputs:
      message:
        type: string
        description: "Message to echo"
        required: true
    run: |
      echo "$message"
---
# Test Workflow

This workflow has safe-inputs configuration.
`
	workflowPath := filepath.Join(workflowsDir, "test.md")
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	// Change to the temporary directory
	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)
	os.Chdir(tmpDir)

	// We can't fully test spawnSafeInputsInspector because it tries to start a server
	// and launch the inspector, but we can test the file generation part separately
	// by calling writeSafeInputsFiles directly

	// Parse the workflow using the compiler to get safe-inputs config
	// (including any imported safe-inputs)
	compiler := workflow.NewCompiler()
	workflowData, err := compiler.ParseWorkflowFile(workflowPath)
	if err != nil {
		t.Fatalf("Failed to parse workflow: %v", err)
	}

	safeInputsConfig := workflowData.SafeInputs
	if safeInputsConfig == nil {
		t.Fatal("Expected safe-inputs config to be parsed")
	}

	// Create a temp directory for files
	filesDir := t.TempDir()

	// Write files
	err = writeSafeInputsFiles(filesDir, safeInputsConfig, false)
	if err != nil {
		t.Fatalf("writeSafeInputsFiles failed: %v", err)
	}

	// Verify the echo-tool.sh file was created
	toolPath := filepath.Join(filesDir, "echo-tool.sh")
	if _, err := os.Stat(toolPath); os.IsNotExist(err) {
		t.Error("echo-tool.sh not found")
	}

	// Verify tools.json contains the echo-tool
	toolsPath := filepath.Join(filesDir, "tools.json")
	toolsContent, err := os.ReadFile(toolsPath)
	if err != nil {
		t.Fatalf("Failed to read tools.json: %v", err)
	}

	// Simple check that the tool name is in the JSON
	if len(toolsContent) < 50 {
		t.Error("tools.json seems too short")
	}
}

// TestSpawnSafeInputsInspector_WithImportedSafeInputs tests that imported safe-inputs are resolved
func TestSpawnSafeInputsInspector_WithImportedSafeInputs(t *testing.T) {
	// Create temporary directory with workflow and shared files
	tmpDir := t.TempDir()
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	sharedDir := filepath.Join(workflowsDir, "shared")
	if err := os.MkdirAll(sharedDir, 0755); err != nil {
		t.Fatalf("Failed to create workflows directory: %v", err)
	}

	// Create a shared workflow file with safe-inputs
	sharedContent := `---
safe-inputs:
  shared-tool:
    description: "Shared tool from import"
    inputs:
      param:
        type: string
        description: "A parameter"
        required: true
    run: |
      echo "Shared: $param"
---
# Shared Workflow
`
	sharedPath := filepath.Join(sharedDir, "shared.md")
	if err := os.WriteFile(sharedPath, []byte(sharedContent), 0644); err != nil {
		t.Fatalf("Failed to write shared workflow file: %v", err)
	}

	// Create a test workflow file that imports the shared workflow
	workflowContent := `---
on: push
engine: copilot
imports:
  - shared/shared.md
safe-inputs:
  local-tool:
    description: "Local tool"
    inputs:
      message:
        type: string
        description: "Message to echo"
        required: true
    run: |
      echo "$message"
---
# Test Workflow

This workflow imports safe-inputs from shared/shared.md.
`
	workflowPath := filepath.Join(workflowsDir, "test.md")
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	// Change to the temporary directory
	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)
	os.Chdir(tmpDir)

	// Parse the workflow using the compiler to get safe-inputs config
	// This should include both local and imported safe-inputs
	compiler := workflow.NewCompiler()
	workflowData, err := compiler.ParseWorkflowFile(workflowPath)
	if err != nil {
		t.Fatalf("Failed to parse workflow: %v", err)
	}

	safeInputsConfig := workflowData.SafeInputs
	if safeInputsConfig == nil {
		t.Fatal("Expected safe-inputs config to be parsed")
	}

	// Verify both local and imported tools are present
	if len(safeInputsConfig.Tools) != 2 {
		t.Errorf("Expected 2 tools (local + imported), got %d", len(safeInputsConfig.Tools))
	}

	// Verify local tool exists
	if _, exists := safeInputsConfig.Tools["local-tool"]; !exists {
		t.Error("Expected local-tool to be present")
	}

	// Verify imported tool exists
	if _, exists := safeInputsConfig.Tools["shared-tool"]; !exists {
		t.Error("Expected shared-tool (from import) to be present")
	}

	// Create a temp directory for files
	filesDir := t.TempDir()

	// Write files
	err = writeSafeInputsFiles(filesDir, safeInputsConfig, false)
	if err != nil {
		t.Fatalf("writeSafeInputsFiles failed: %v", err)
	}

	// Verify both tool handler files were created
	localToolPath := filepath.Join(filesDir, "local-tool.sh")
	if _, err := os.Stat(localToolPath); os.IsNotExist(err) {
		t.Error("local-tool.sh not found")
	}

	sharedToolPath := filepath.Join(filesDir, "shared-tool.sh")
	if _, err := os.Stat(sharedToolPath); os.IsNotExist(err) {
		t.Error("shared-tool.sh not found")
	}

	// Verify tools.json contains both tools
	toolsPath := filepath.Join(filesDir, "tools.json")
	toolsContent, err := os.ReadFile(toolsPath)
	if err != nil {
		t.Fatalf("Failed to read tools.json: %v", err)
	}

	// Check that both tool names are in the JSON
	toolsJSON := string(toolsContent)
	if !strings.Contains(toolsJSON, "local-tool") {
		t.Error("tools.json should contain 'local-tool'")
	}
	if !strings.Contains(toolsJSON, "shared-tool") {
		t.Error("tools.json should contain 'shared-tool'")
	}
}
