//go:build !integration

package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// getCodemodByID is a helper function to find a codemod by ID
func getCodemodByID(id string) *Codemod {
	codemods := GetAllCodemods()
	for _, cm := range codemods {
		if cm.ID == id {
			return &cm
		}
	}
	return nil
}

func TestFixCommand_TimeoutMinutesMigration(t *testing.T) {
	// Create a temporary directory for test files
	tmpDir := t.TempDir()
	workflowFile := filepath.Join(tmpDir, "test-workflow.md")

	// Create a workflow with deprecated timeout_minutes field
	content := `---
on:
  workflow_dispatch:

timeout_minutes: 30

permissions:
  contents: read
---

# Test Workflow

This is a test workflow.
`

	if err := os.WriteFile(workflowFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Get the timeout migration codemod
	timeoutCodemod := getCodemodByID("timeout-minutes-migration")
	if timeoutCodemod == nil {
		t.Fatal("timeout-minutes-migration codemod not found")
	}

	// Process the file
	fixed, err := processWorkflowFile(workflowFile, []Codemod{*timeoutCodemod}, true, false)
	if err != nil {
		t.Fatalf("Failed to process workflow file: %v", err)
	}

	if !fixed {
		t.Error("Expected file to be fixed, but no changes were made")
	}

	// Read the updated content
	updatedContent, err := os.ReadFile(workflowFile)
	if err != nil {
		t.Fatalf("Failed to read updated file: %v", err)
	}

	updatedStr := string(updatedContent)

	// Verify the change
	if strings.Contains(updatedStr, "timeout_minutes:") {
		t.Error("Expected timeout_minutes to be replaced, but it still exists")
	}

	if !strings.Contains(updatedStr, "timeout-minutes: 30") {
		t.Errorf("Expected timeout-minutes: 30 in updated content, got:\n%s", updatedStr)
	}
}

func TestFixCommand_NoChangesNeeded(t *testing.T) {
	// Create a temporary directory for test files
	tmpDir := t.TempDir()
	workflowFile := filepath.Join(tmpDir, "test-workflow.md")

	// Create a workflow with no deprecated fields
	content := `---
on:
  workflow_dispatch:

timeout-minutes: 30

permissions:
  contents: read
---

# Test Workflow

This is a test workflow.
`

	if err := os.WriteFile(workflowFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Run all codemods
	codemods := GetAllCodemods()

	// Process the file
	fixed, err := processWorkflowFile(workflowFile, codemods, false, false)
	if err != nil {
		t.Fatalf("Failed to process workflow file: %v", err)
	}

	if fixed {
		t.Error("Expected no changes, but file was marked as fixed")
	}

	// Read the content to verify it's unchanged
	updatedContent, err := os.ReadFile(workflowFile)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}

	if string(updatedContent) != content {
		t.Error("Expected content to be unchanged")
	}
}

func TestFixCommand_NetworkFirewallMigration(t *testing.T) {
	// Create a temporary directory for test files
	tmpDir := t.TempDir()
	workflowFile := filepath.Join(tmpDir, "test-workflow.md")

	// Create a workflow with deprecated network.firewall field
	content := `---
on:
  workflow_dispatch:

network:
  allowed:
    - "*.example.com"
  firewall: null

permissions:
  contents: read
---

# Test Workflow

This is a test workflow.
`

	if err := os.WriteFile(workflowFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Get the firewall migration codemod
	firewallCodemod := getCodemodByID("network-firewall-migration")
	if firewallCodemod == nil {
		t.Fatal("network-firewall-migration codemod not found")
	}

	// Process the file
	fixed, err := processWorkflowFile(workflowFile, []Codemod{*firewallCodemod}, true, false)
	if err != nil {
		t.Fatalf("Failed to process workflow file: %v", err)
	}

	if !fixed {
		t.Error("Expected file to be fixed, but no changes were made")
	}

	// Read the updated content
	updatedContent, err := os.ReadFile(workflowFile)
	if err != nil {
		t.Fatalf("Failed to read updated file: %v", err)
	}

	updatedStr := string(updatedContent)

	// Verify the change
	if strings.Contains(updatedStr, "firewall:") {
		t.Error("Expected firewall field to be removed, but it still exists")
	}

	// firewall: null should NOT add sandbox.agent (only true values do)
	if strings.Contains(updatedStr, "sandbox:") {
		t.Errorf("Expected sandbox field NOT to be added for null firewall, got:\n%s", updatedStr)
	}
}

func TestFixCommand_NetworkFirewallMigrationWithNestedProperties(t *testing.T) {
	// Create a temporary directory for test files
	tmpDir := t.TempDir()
	workflowFile := filepath.Join(tmpDir, "test-workflow.md")

	// Create a workflow with deprecated network.firewall field with nested properties
	content := `---
on:
  workflow_dispatch:

network:
  allowed:
    - defaults
    - node
    - github
  firewall:
    log-level: debug
    version: v1.0.0

permissions:
  contents: read
---

# Test Workflow

This is a test workflow.
`

	if err := os.WriteFile(workflowFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Get the firewall migration codemod
	firewallCodemod := getCodemodByID("network-firewall-migration")
	if firewallCodemod == nil {
		t.Fatal("network-firewall-migration codemod not found")
	}

	// Process the file
	fixed, err := processWorkflowFile(workflowFile, []Codemod{*firewallCodemod}, true, false)
	if err != nil {
		t.Fatalf("Failed to process workflow file: %v", err)
	}

	if !fixed {
		t.Error("Expected file to be fixed, but no changes were made")
	}

	// Read the updated content
	updatedContent, err := os.ReadFile(workflowFile)
	if err != nil {
		t.Fatalf("Failed to read updated file: %v", err)
	}

	updatedStr := string(updatedContent)

	// Verify the change - firewall and all nested properties should be removed
	if strings.Contains(updatedStr, "firewall:") {
		t.Error("Expected firewall field to be removed, but it still exists")
	}

	if strings.Contains(updatedStr, "log-level:") {
		t.Error("Expected log-level field to be removed, but it still exists")
	}

	if strings.Contains(updatedStr, "version: v1.0.0") {
		t.Error("Expected version field to be removed, but it still exists")
	}

	// firewall with nested properties (non-boolean) should NOT add sandbox.agent
	if strings.Contains(updatedStr, "sandbox:") {
		t.Errorf("Expected sandbox field NOT to be added for nested firewall, got:\n%s", updatedStr)
	}

	// Verify compilation works
	// This ensures the codemod produces valid YAML
	if strings.Contains(updatedStr, "    log-level:") {
		t.Error("log-level should not be at wrong indentation level")
	}

	// Verify other network fields are preserved
	if !strings.Contains(updatedStr, "allowed:") {
		t.Error("Expected allowed field to be preserved")
	}
}

func TestFixCommand_NetworkFirewallMigrationWithCommentsAndEmptyLines(t *testing.T) {
	// Create a temporary directory for test files
	tmpDir := t.TempDir()
	workflowFile := filepath.Join(tmpDir, "test-workflow.md")

	// Create a workflow with firewall containing comments and empty lines
	content := `---
on:
  workflow_dispatch:

network:
  allowed:
    - defaults
    - github
  firewall:
    # Firewall configuration

    log-level: debug
    # Version setting
    version: v1.0.0

permissions:
  contents: read
---

# Test Workflow

This workflow tests comment and empty line handling.
`

	if err := os.WriteFile(workflowFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Get the firewall migration codemod
	firewallCodemod := getCodemodByID("network-firewall-migration")
	if firewallCodemod == nil {
		t.Fatal("network-firewall-migration codemod not found")
	}

	// Process the file
	fixed, err := processWorkflowFile(workflowFile, []Codemod{*firewallCodemod}, true, false)
	if err != nil {
		t.Fatalf("Failed to process workflow file: %v", err)
	}

	if !fixed {
		t.Error("Expected file to be fixed, but no changes were made")
	}

	// Read the updated content
	updatedContent, err := os.ReadFile(workflowFile)
	if err != nil {
		t.Fatalf("Failed to read updated file: %v", err)
	}

	updatedStr := string(updatedContent)

	// Verify the change - firewall and all nested content (including comments) should be removed
	if strings.Contains(updatedStr, "firewall:") {
		t.Error("Expected firewall field to be removed, but it still exists")
	}

	if strings.Contains(updatedStr, "log-level:") {
		t.Error("Expected log-level field to be removed, but it still exists")
	}

	if strings.Contains(updatedStr, "version: v1.0.0") {
		t.Error("Expected version field to be removed, but it still exists")
	}

	// Comments within the firewall block should also be removed
	if strings.Contains(updatedStr, "# Firewall configuration") {
		t.Error("Expected comment within firewall block to be removed, but it still exists")
	}

	if strings.Contains(updatedStr, "# Version setting") {
		t.Error("Expected comment within firewall block to be removed, but it still exists")
	}

	// firewall with nested properties should NOT add sandbox.agent
	if strings.Contains(updatedStr, "sandbox:") {
		t.Errorf("Expected sandbox field NOT to be added for nested firewall, got:\n%s", updatedStr)
	}

	// Verify other network fields are preserved
	if !strings.Contains(updatedStr, "allowed:") {
		t.Error("Expected allowed field to be preserved")
	}
}

func TestFixCommand_PreservesFormatting(t *testing.T) {
	// Create a temporary directory for test files
	tmpDir := t.TempDir()
	workflowFile := filepath.Join(tmpDir, "test-workflow.md")

	// Create a workflow with comments and specific formatting
	content := `---
on:
  workflow_dispatch:

# Timeout configuration
timeout_minutes: 30  # 30 minutes should be enough

permissions:
  contents: read
---

# Test Workflow

This is a test workflow.
`

	if err := os.WriteFile(workflowFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Get the timeout migration codemod
	timeoutCodemod := getCodemodByID("timeout-minutes-migration")
	if timeoutCodemod == nil {
		t.Fatal("timeout-minutes-migration codemod not found")
	}

	// Process the file
	fixed, err := processWorkflowFile(workflowFile, []Codemod{*timeoutCodemod}, true, false)
	if err != nil {
		t.Fatalf("Failed to process workflow file: %v", err)
	}

	if !fixed {
		t.Error("Expected file to be fixed, but no changes were made")
	}

	// Read the updated content
	updatedContent, err := os.ReadFile(workflowFile)
	if err != nil {
		t.Fatalf("Failed to read updated file: %v", err)
	}

	updatedStr := string(updatedContent)

	// Verify the comment is preserved
	if !strings.Contains(updatedStr, "# 30 minutes should be enough") {
		t.Error("Expected inline comment to be preserved")
	}

	// Verify the block comment is preserved
	if !strings.Contains(updatedStr, "# Timeout configuration") {
		t.Error("Expected block comment to be preserved")
	}

	// Verify the field was changed
	if !strings.Contains(updatedStr, "timeout-minutes: 30") {
		t.Errorf("Expected timeout-minutes: 30 in updated content, got:\n%s", updatedStr)
	}
}

func TestGetAllCodemods(t *testing.T) {
	codemods := GetAllCodemods()

	if len(codemods) == 0 {
		t.Fatal("Expected at least one codemod, got none")
	}

	// Check for required codemods
	expectedIDs := []string{
		"timeout-minutes-migration",
		"network-firewall-migration",
		"command-to-slash-command-migration",
		"safe-inputs-mode-removal",
	}

	foundIDs := make(map[string]bool)
	for _, cm := range codemods {
		foundIDs[cm.ID] = true

		// Verify each codemod has required fields
		if cm.ID == "" {
			t.Error("Codemod has empty ID")
		}
		if cm.Name == "" {
			t.Error("Codemod has empty Name")
		}
		if cm.Description == "" {
			t.Error("Codemod has empty Description")
		}
		if cm.Apply == nil {
			t.Error("Codemod has nil Apply function")
		}
	}

	for _, expectedID := range expectedIDs {
		if !foundIDs[expectedID] {
			t.Errorf("Expected codemod with ID %s not found", expectedID)
		}
	}
}

func TestFixCommand_CommandToSlashCommandMigration(t *testing.T) {
	// Create a temporary directory for test files
	tmpDir := t.TempDir()
	workflowFile := filepath.Join(tmpDir, "test-workflow.md")

	// Create a workflow with deprecated on.command field
	content := `---
on:
  command: my-bot

permissions:
  contents: read
---

# Test Workflow

This is a test workflow with slash command.
`

	if err := os.WriteFile(workflowFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Get the command migration codemod
	commandCodemod := getCodemodByID("command-to-slash-command-migration")
	if commandCodemod == nil {
		t.Fatal("command-to-slash-command-migration codemod not found")
	}

	// Process the file
	fixed, err := processWorkflowFile(workflowFile, []Codemod{*commandCodemod}, true, false)
	if err != nil {
		t.Fatalf("Failed to process workflow file: %v", err)
	}

	if !fixed {
		t.Error("Expected file to be fixed, but no changes were made")
	}

	// Read the updated content
	updatedContent, err := os.ReadFile(workflowFile)
	if err != nil {
		t.Fatalf("Failed to read updated file: %v", err)
	}

	updatedStr := string(updatedContent)

	// Debug: print the content to see what we got
	t.Logf("Updated content:\n%s", updatedStr)

	// Verify the change - check for the presence of slash_command
	if !strings.Contains(updatedStr, "slash_command:") {
		t.Errorf("Expected slash_command field, got:\n%s", updatedStr)
	}

	// Check that standalone "command" field was replaced (not part of slash_command)
	lines := strings.Split(updatedStr, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "command:") && !strings.Contains(line, "slash_command") {
			t.Errorf("Found unreplaced 'command:' field: %s", line)
		}
	}

	if !strings.Contains(updatedStr, "slash_command: my-bot") {
		t.Errorf("Expected on.slash_command: my-bot in updated content, got:\n%s", updatedStr)
	}
}

func TestFixCommand_SafeInputsModeRemoval(t *testing.T) {
	// Create a temporary directory for test files
	tmpDir := t.TempDir()
	workflowFile := filepath.Join(tmpDir, "test-workflow.md")

	// Create a workflow with deprecated safe-inputs.mode field
	content := `---
on: workflow_dispatch
engine: copilot
safe-inputs:
  mode: http
  test-tool:
    description: Test tool
    script: |
      return { result: "test" };
---

# Test Workflow

This is a test workflow with safe-inputs mode field.
`

	if err := os.WriteFile(workflowFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Get the safe-inputs mode removal codemod
	modeCodemod := getCodemodByID("safe-inputs-mode-removal")
	if modeCodemod == nil {
		t.Fatal("safe-inputs-mode-removal codemod not found")
	}

	// Process the file
	fixed, err := processWorkflowFile(workflowFile, []Codemod{*modeCodemod}, true, false)
	if err != nil {
		t.Fatalf("Failed to process workflow file: %v", err)
	}

	if !fixed {
		t.Error("Expected file to be fixed, but no changes were made")
	}

	// Read the updated content
	updatedContent, err := os.ReadFile(workflowFile)
	if err != nil {
		t.Fatalf("Failed to read updated file: %v", err)
	}

	updatedStr := string(updatedContent)

	t.Logf("Updated content:\n%s", updatedStr)

	// Verify the change - mode field should be removed
	if strings.Contains(updatedStr, "mode:") {
		t.Errorf("Expected mode field to be removed, but it still exists:\n%s", updatedStr)
	}

	// Verify safe-inputs block and test-tool are preserved
	if !strings.Contains(updatedStr, "safe-inputs:") {
		t.Error("Expected safe-inputs block to be preserved")
	}

	if !strings.Contains(updatedStr, "test-tool:") {
		t.Error("Expected test-tool to be preserved")
	}

	if !strings.Contains(updatedStr, "description: Test tool") {
		t.Error("Expected test-tool description to be preserved")
	}
}

func TestFixCommand_UpdatesPromptAndAgentFiles(t *testing.T) {
	// Create a temporary directory for test files
	tmpDir := t.TempDir()
	workflowFile := filepath.Join(tmpDir, "test-workflow.md")

	// Save and restore original directory
	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}
	defer func() {
		_ = os.Chdir(originalDir)
	}()

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Failed to change to temp directory: %v", err)
	}

	// Initialize git repo (required for ensure functions)
	if err := exec.Command("git", "init").Run(); err != nil {
		t.Skip("Git not available")
	}

	// Configure git
	exec.Command("git", "config", "user.name", "Test User").Run()
	exec.Command("git", "config", "user.email", "test@example.com").Run()

	// Create a simple workflow file (no fixes needed)
	content := `---
on:
  workflow_dispatch:

permissions:
  contents: read
---

# Test Workflow

This is a test workflow.
`

	if err := os.WriteFile(workflowFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Run fix command (which checks prompt and agent files exist)
	config := FixConfig{
		WorkflowIDs: []string{"test-workflow"},
		Write:       false,
		Verbose:     false,
		WorkflowDir: tmpDir,
	}

	err = RunFix(config)
	if err != nil {
		t.Fatalf("RunFix failed: %v", err)
	}

	// Note: The ensure functions no longer create files from templates.
	// They just check if files exist. Since we're in a temp directory,
	// the files won't exist, but that's expected behavior.
	// This test now just verifies that RunFix completes without error.
}

func TestFixCommand_GrepToolRemoval(t *testing.T) {
	// Create a temporary directory for test files
	tmpDir := t.TempDir()
	workflowFile := filepath.Join(tmpDir, "test-workflow.md")

	// Create a workflow with deprecated tools.grep field
	content := `---
on:
  workflow_dispatch:

tools:
  bash: ["echo", "ls"]
  grep: true
  github:

permissions:
  contents: read
---

# Test Workflow

This workflow uses the deprecated grep tool.
`

	if err := os.WriteFile(workflowFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Get the grep removal codemod
	grepCodemod := getCodemodByID("grep-tool-removal")
	if grepCodemod == nil {
		t.Fatal("grep-tool-removal codemod not found")
	}

	// Process the file
	fixed, err := processWorkflowFile(workflowFile, []Codemod{*grepCodemod}, true, false)
	if err != nil {
		t.Fatalf("Failed to process workflow file: %v", err)
	}

	if !fixed {
		t.Error("Expected file to be fixed, but no changes were made")
	}

	// Read the updated content
	updatedContent, err := os.ReadFile(workflowFile)
	if err != nil {
		t.Fatalf("Failed to read updated file: %v", err)
	}

	updatedStr := string(updatedContent)

	// Verify the change - grep should be removed
	if strings.Contains(updatedStr, "grep:") {
		t.Errorf("Expected grep to be removed, but it still exists:\n%s", updatedStr)
	}

	// Verify other tools are preserved
	if !strings.Contains(updatedStr, "bash:") {
		t.Error("Expected bash tool to be preserved")
	}

	if !strings.Contains(updatedStr, "github:") {
		t.Error("Expected github tool to be preserved")
	}
}

func TestFixCommand_GrepToolRemoval_NoGrep(t *testing.T) {
	// Create a temporary directory for test files
	tmpDir := t.TempDir()
	workflowFile := filepath.Join(tmpDir, "test-workflow.md")

	// Create a workflow without grep field
	content := `---
on:
  workflow_dispatch:

tools:
  bash: ["echo", "ls"]
  github:

permissions:
  contents: read
---

# Test Workflow

This workflow doesn't have grep.
`

	if err := os.WriteFile(workflowFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Get the grep removal codemod
	grepCodemod := getCodemodByID("grep-tool-removal")
	if grepCodemod == nil {
		t.Fatal("grep-tool-removal codemod not found")
	}

	// Process the file
	fixed, err := processWorkflowFile(workflowFile, []Codemod{*grepCodemod}, true, false)
	if err != nil {
		t.Fatalf("Failed to process workflow file: %v", err)
	}

	if fixed {
		t.Error("Expected file to not be modified when grep is not present")
	}
}
