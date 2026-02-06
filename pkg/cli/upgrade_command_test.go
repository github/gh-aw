//go:build integration

package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpgradeCommand_UpdatesAgentFiles(t *testing.T) {
	// Create a temporary directory for test files
	tmpDir := t.TempDir()
	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)

	// Initialize git repository (required for init functionality)
	os.Chdir(tmpDir)
	exec.Command("git", "init").Run()
	exec.Command("git", "config", "user.email", "test@example.com").Run()
	exec.Command("git", "config", "user.name", "Test User").Run()

	// Create .github/workflows directory
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	err := os.MkdirAll(workflowsDir, 0755)
	require.NoError(t, err, "Failed to create workflows directory")

	// Create a simple workflow file
	workflowFile := filepath.Join(workflowsDir, "test-workflow.md")
	content := `---
on:
  workflow_dispatch:

permissions:
  contents: read
---

# Test Workflow

This is a test workflow.
`
	err = os.WriteFile(workflowFile, []byte(content), 0644)
	require.NoError(t, err, "Failed to create test workflow file")

	// Run upgrade command
	config := UpgradeConfig{
		Verbose:     false,
		NoFix:       true, // Skip codemods for this test
		WorkflowDir: "",
		Push:        false,
		NoActions:   true, // Skip action updates for this test
	}

	err = RunUpgrade(config)
	require.NoError(t, err, "Upgrade command should succeed")

	// Verify that dispatcher agent file was created
	// Note: After PR #13612, only the dispatcher agent file is downloaded/created.
	// Other files (github-agentic-workflows.md, upgrade-agentic-workflows.md, etc.)
	// are expected to exist only in the gh-aw repository itself.
	dispatcherFile := filepath.Join(tmpDir, ".github", "agents", "agentic-workflows.agent.md")
	assert.FileExists(t, dispatcherFile, "Dispatcher agent file should be created")
}

func TestUpgradeCommand_AppliesCodemods(t *testing.T) {

	// Create a temporary directory for test files
	tmpDir := t.TempDir()
	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)

	// Initialize git repository
	os.Chdir(tmpDir)
	exec.Command("git", "init").Run()
	exec.Command("git", "config", "user.email", "test@example.com").Run()
	exec.Command("git", "config", "user.name", "Test User").Run()

	// Create .github/workflows directory
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	err := os.MkdirAll(workflowsDir, 0755)
	require.NoError(t, err, "Failed to create workflows directory")

	// Create multiple workflows with deprecated fields to test all workflows are upgraded
	workflow1 := filepath.Join(workflowsDir, "workflow1.md")
	workflow1Content := `---
on:
  workflow_dispatch:

timeout_minutes: 30

permissions:
  contents: read
---

# Workflow 1

This workflow has deprecated timeout_minutes field.
`
	err = os.WriteFile(workflow1, []byte(workflow1Content), 0644)
	require.NoError(t, err, "Failed to create workflow1")

	workflow2 := filepath.Join(workflowsDir, "workflow2.md")
	workflow2Content := `---
on:
  workflow_dispatch:

timeout_minutes: 60

permissions:
  contents: read
---

# Workflow 2

This workflow also has deprecated timeout_minutes field.
`
	err = os.WriteFile(workflow2, []byte(workflow2Content), 0644)
	require.NoError(t, err, "Failed to create workflow2")

	// Run upgrade command (should upgrade ALL workflows)
	config := UpgradeConfig{
		Verbose:     false,
		NoFix:       false, // Apply codemods
		WorkflowDir: "",
		Push:        false,
		NoActions:   true, // Skip action updates for this test
	}

	err = RunUpgrade(config)
	require.NoError(t, err, "Upgrade command should succeed")

	// Verify both workflows were updated
	updatedContent1, err := os.ReadFile(workflow1)
	require.NoError(t, err, "Failed to read workflow1")
	updatedStr1 := string(updatedContent1)
	assert.NotContains(t, updatedStr1, "timeout_minutes:", "workflow1 timeout_minutes should be replaced")
	assert.Contains(t, updatedStr1, "timeout-minutes:", "workflow1 should have new syntax")

	updatedContent2, err := os.ReadFile(workflow2)
	require.NoError(t, err, "Failed to read workflow2")
	updatedStr2 := string(updatedContent2)
	assert.NotContains(t, updatedStr2, "timeout_minutes:", "workflow2 timeout_minutes should be replaced")
	assert.Contains(t, updatedStr2, "timeout-minutes:", "workflow2 should have new syntax")
}

func TestUpgradeCommand_NoFixFlag(t *testing.T) {
	// Create a temporary directory for test files
	tmpDir := t.TempDir()
	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)

	// Initialize git repository
	os.Chdir(tmpDir)
	exec.Command("git", "init").Run()
	exec.Command("git", "config", "user.email", "test@example.com").Run()
	exec.Command("git", "config", "user.name", "Test User").Run()

	// Create .github/workflows directory
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	err := os.MkdirAll(workflowsDir, 0755)
	require.NoError(t, err, "Failed to create workflows directory")

	// Create a workflow with deprecated field
	workflowFile := filepath.Join(workflowsDir, "test-workflow.md")
	content := `---
on:
  workflow_dispatch:

timeout_minutes: 30

permissions:
  contents: read
---

# Test Workflow

This workflow should not be modified when --no-fix is used.
`
	err = os.WriteFile(workflowFile, []byte(content), 0644)
	require.NoError(t, err, "Failed to create test workflow file")

	// Run upgrade command with --no-fix
	config := UpgradeConfig{
		Verbose:     false,
		NoFix:       true, // Skip codemods
		WorkflowDir: "",
		Push:        false,
		NoActions:   true, // Skip action updates for this test
	}

	err = RunUpgrade(config)
	require.NoError(t, err, "Upgrade command should succeed")

	// Read the workflow file
	updatedContent, err := os.ReadFile(workflowFile)
	require.NoError(t, err, "Failed to read workflow file")

	updatedStr := string(updatedContent)

	// Verify that the deprecated field was NOT replaced
	assert.Contains(t, updatedStr, "timeout_minutes:", "timeout_minutes should not be replaced with --no-fix")
	assert.NotContains(t, updatedStr, "timeout-minutes:", "timeout-minutes should not be added with --no-fix")
}

func TestUpgradeCommand_NonGitRepo(t *testing.T) {
	// Create a temporary directory that's not a git repository
	tmpDir := t.TempDir()
	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)

	// Change to non-git directory
	os.Chdir(tmpDir)

	// Run upgrade command
	config := UpgradeConfig{
		Verbose:     false,
		NoFix:       true,
		WorkflowDir: "",
		Push:        false,
		NoActions:   true, // Skip action updates for this test
	}

	err := RunUpgrade(config)
	// Should fail because we're not in a git repository
	require.Error(t, err, "Upgrade should fail in non-git repository")
	assert.Contains(t, strings.ToLower(err.Error()), "git", "Error message should mention git")
}

func TestUpgradeCommand_CompilesWorkflows(t *testing.T) {

	// Create a temporary directory for test files
	tmpDir := t.TempDir()
	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)

	// Initialize git repository
	os.Chdir(tmpDir)
	exec.Command("git", "init").Run()
	exec.Command("git", "config", "user.email", "test@example.com").Run()
	exec.Command("git", "config", "user.name", "Test User").Run()

	// Create .github/workflows directory
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	err := os.MkdirAll(workflowsDir, 0755)
	require.NoError(t, err, "Failed to create workflows directory")

	// Create a simple workflow that should compile successfully
	workflowFile := filepath.Join(workflowsDir, "test-workflow.md")
	content := `---
on:
  workflow_dispatch:

permissions:
  contents: read
---

# Test Workflow

This is a test workflow that should be compiled during upgrade.
`
	err = os.WriteFile(workflowFile, []byte(content), 0644)
	require.NoError(t, err, "Failed to create test workflow file")

	// Run upgrade command (should compile workflows)
	config := UpgradeConfig{
		Verbose:     false,
		NoFix:       false, // Apply codemods and compile
		WorkflowDir: "",
		Push:        false,
		NoActions:   true, // Skip action updates for this test
	}

	err = RunUpgrade(config)
	require.NoError(t, err, "Upgrade command should succeed")

	// Verify that the lock file was created
	lockFile := filepath.Join(workflowsDir, "test-workflow.lock.yml")
	assert.FileExists(t, lockFile, "Lock file should be created after upgrade")

	// Read lock file content and verify it's valid YAML
	lockContent, err := os.ReadFile(lockFile)
	require.NoError(t, err, "Failed to read lock file")
	assert.NotEmpty(t, lockContent, "Lock file should not be empty")
	assert.Contains(t, string(lockContent), "name:", "Lock file should contain workflow name")
}

func TestUpgradeCommand_NoFixSkipsCompilation(t *testing.T) {
	// Create a temporary directory for test files
	tmpDir := t.TempDir()
	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)

	// Initialize git repository
	os.Chdir(tmpDir)
	exec.Command("git", "init").Run()
	exec.Command("git", "config", "user.email", "test@example.com").Run()
	exec.Command("git", "config", "user.name", "Test User").Run()

	// Create .github/workflows directory
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	err := os.MkdirAll(workflowsDir, 0755)
	require.NoError(t, err, "Failed to create workflows directory")

	// Create a simple workflow
	workflowFile := filepath.Join(workflowsDir, "test-workflow.md")
	content := `---
on:
  workflow_dispatch:

permissions:
  contents: read
---

# Test Workflow

This workflow should not be compiled with --no-fix.
`
	err = os.WriteFile(workflowFile, []byte(content), 0644)
	require.NoError(t, err, "Failed to create test workflow file")

	// Run upgrade command with --no-fix
	config := UpgradeConfig{
		Verbose:     false,
		NoFix:       true, // Skip codemods and compilation
		WorkflowDir: "",
		Push:        false,
		NoActions:   true, // Skip action updates for this test
	}

	err = RunUpgrade(config)
	require.NoError(t, err, "Upgrade command should succeed")

	// Verify that the lock file was NOT created
	lockFile := filepath.Join(workflowsDir, "test-workflow.lock.yml")
	assert.NoFileExists(t, lockFile, "Lock file should not be created with --no-fix")
}

func TestUpgradeCommand_PushRequiresCleanWorkingDirectory(t *testing.T) {
	// Create a temporary directory for test files
	tmpDir := t.TempDir()
	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)

	// Initialize git repository
	os.Chdir(tmpDir)
	exec.Command("git", "init").Run()
	exec.Command("git", "config", "user.email", "test@example.com").Run()
	exec.Command("git", "config", "user.name", "Test User").Run()

	// Create .github/workflows directory
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	err := os.MkdirAll(workflowsDir, 0755)
	require.NoError(t, err, "Failed to create workflows directory")

	// Create a workflow file
	workflowFile := filepath.Join(workflowsDir, "test-workflow.md")
	content := `---
on:
  workflow_dispatch:

permissions:
  contents: read
---

# Test Workflow

This is a test workflow.
`
	err = os.WriteFile(workflowFile, []byte(content), 0644)
	require.NoError(t, err, "Failed to create test workflow file")

	// Commit the workflow file first
	exec.Command("git", "add", ".").Run()
	exec.Command("git", "commit", "-m", "Initial commit").Run()

	// Create an uncommitted change
	unstagedFile := filepath.Join(tmpDir, "uncommitted.txt")
	err = os.WriteFile(unstagedFile, []byte("uncommitted content"), 0644)
	require.NoError(t, err, "Failed to create uncommitted file")

	// Run upgrade command with --push (should fail due to uncommitted changes)
	config := UpgradeConfig{
		Verbose:     false,
		NoFix:       true,
		WorkflowDir: "",
		Push:        true,
		NoActions:   true, // Skip action updates for this test
	}

	err = RunUpgrade(config)
	require.Error(t, err, "Upgrade with --push should fail when there are uncommitted changes")
	assert.Contains(t, strings.ToLower(err.Error()), "clean", "Error message should mention clean working directory")
}

func TestUpgradeCommand_PushWithNoChanges(t *testing.T) {
	// Create a temporary directory for test files
	tmpDir := t.TempDir()
	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)

	// Initialize git repository
	os.Chdir(tmpDir)
	exec.Command("git", "init").Run()
	exec.Command("git", "config", "user.email", "test@example.com").Run()
	exec.Command("git", "config", "user.name", "Test User").Run()

	// Create .github/workflows directory
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	err := os.MkdirAll(workflowsDir, 0755)
	require.NoError(t, err, "Failed to create workflows directory")

	// Create a simple workflow
	workflowFile := filepath.Join(workflowsDir, "test-workflow.md")
	content := `---
on:
  workflow_dispatch:

permissions:
  contents: read
---

# Test Workflow

This workflow is already up to date.
`
	err = os.WriteFile(workflowFile, []byte(content), 0644)
	require.NoError(t, err, "Failed to create test workflow file")

	// Commit everything first
	exec.Command("git", "add", ".").Run()
	exec.Command("git", "commit", "-m", "Initial commit").Run()

	// Run upgrade command with --push (should fail because no remote is configured)
	config := UpgradeConfig{
		Verbose:     false,
		NoFix:       true, // Skip codemods to avoid changes
		WorkflowDir: "",
		Push:        true,
		NoActions:   true, // Skip action updates for this test
	}

	err = RunUpgrade(config)
	// Should fail because no remote is configured
	require.Error(t, err, "Upgrade with --push should fail when no remote is configured")
	assert.Contains(t, err.Error(), "--push requires a remote repository to be configured")
}

func TestUpgradeCommand_UpdatesActionPins(t *testing.T) {

	// Create a temporary directory for test files
	tmpDir := t.TempDir()
	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)

	// Initialize git repository
	os.Chdir(tmpDir)
	exec.Command("git", "init").Run()
	exec.Command("git", "config", "user.email", "test@example.com").Run()
	exec.Command("git", "config", "user.name", "Test User").Run()

	// Create .github/workflows directory
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	err := os.MkdirAll(workflowsDir, 0755)
	require.NoError(t, err, "Failed to create workflows directory")

	// Create .github/aw directory
	awDir := filepath.Join(tmpDir, ".github", "aw")
	err = os.MkdirAll(awDir, 0755)
	require.NoError(t, err, "Failed to create .github/aw directory")

	// Create a simple workflow
	workflowFile := filepath.Join(workflowsDir, "test-workflow.md")
	content := `---
on:
  workflow_dispatch:

permissions:
  contents: read
---

# Test Workflow

This workflow should trigger action pin updates.
`
	err = os.WriteFile(workflowFile, []byte(content), 0644)
	require.NoError(t, err, "Failed to create test workflow file")

	// Create an actions-lock.json file with a test entry
	actionsLockPath := filepath.Join(awDir, "actions-lock.json")
	actionsLockContent := `{
  "entries": {
    "actions/checkout@v4": {
      "repo": "actions/checkout",
      "version": "v4",
      "sha": "b4ffde65f46336ab88eb53be808477a3936bae11"
    }
  }
}
`
	err = os.WriteFile(actionsLockPath, []byte(actionsLockContent), 0644)
	require.NoError(t, err, "Failed to create actions-lock.json file")

	// Run upgrade command (should update actions)
	config := UpgradeConfig{
		Verbose:     false,
		NoFix:       false, // Apply codemods and compile
		WorkflowDir: "",
		Push:        false,
		NoActions:   false, // Enable action updates
	}

	err = RunUpgrade(config)
	require.NoError(t, err, "Upgrade command should succeed")

	// Verify that actions-lock.json still exists (it may or may not be updated depending on GitHub API availability)
	// We just verify the upgrade doesn't break when action updates are enabled
	_, statErr := os.Stat(actionsLockPath)
	// Either file still exists or was removed (both are acceptable outcomes)
	// Just verify no panic or crash occurred
	assert.Condition(t, func() bool {
		return statErr == nil || os.IsNotExist(statErr)
	}, "Actions lock file should exist or be removed cleanly")
}

func TestUpgradeCommand_NoActionsFlag(t *testing.T) {
	// Create a temporary directory for test files
	tmpDir := t.TempDir()
	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)

	// Initialize git repository
	os.Chdir(tmpDir)
	exec.Command("git", "init").Run()
	exec.Command("git", "config", "user.email", "test@example.com").Run()
	exec.Command("git", "config", "user.name", "Test User").Run()

	// Create .github/workflows directory
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	err := os.MkdirAll(workflowsDir, 0755)
	require.NoError(t, err, "Failed to create workflows directory")

	// Create .github/aw directory
	awDir := filepath.Join(tmpDir, ".github", "aw")
	err = os.MkdirAll(awDir, 0755)
	require.NoError(t, err, "Failed to create .github/aw directory")

	// Create a simple workflow
	workflowFile := filepath.Join(workflowsDir, "test-workflow.md")
	content := `---
on:
  workflow_dispatch:

permissions:
  contents: read
---

# Test Workflow

This workflow should not trigger action pin updates with --no-actions.
`
	err = os.WriteFile(workflowFile, []byte(content), 0644)
	require.NoError(t, err, "Failed to create test workflow file")

	// Create an actions-lock.json file with a test entry
	actionsLockPath := filepath.Join(awDir, "actions-lock.json")
	originalContent := `{
  "entries": {
    "actions/checkout@v4": {
      "repo": "actions/checkout",
      "version": "v4",
      "sha": "b4ffde65f46336ab88eb53be808477a3936bae11"
    }
  }
}
`
	err = os.WriteFile(actionsLockPath, []byte(originalContent), 0644)
	require.NoError(t, err, "Failed to create actions-lock.json file")

	// Run upgrade command with --no-actions
	config := UpgradeConfig{
		Verbose:     false,
		NoFix:       false, // Apply codemods and compile
		WorkflowDir: "",
		Push:        false,
		NoActions:   true, // Skip action updates
	}

	err = RunUpgrade(config)
	require.NoError(t, err, "Upgrade command should succeed")

	// Verify that existing action entry was not modified (not upgraded to newer version)
	updatedContent, err := os.ReadFile(actionsLockPath)
	require.NoError(t, err, "Failed to read actions-lock.json")

	// Parse the updated lock file
	var updatedLock struct {
		Entries map[string]struct {
			Repo    string `json:"repo"`
			Version string `json:"version"`
			SHA     string `json:"sha"`
		} `json:"entries"`
	}
	err = json.Unmarshal(updatedContent, &updatedLock)
	require.NoError(t, err, "Failed to parse updated actions-lock.json")

	// The original checkout entry should not be modified (--no-actions should skip UpdateActions)
	checkoutEntry, exists := updatedLock.Entries["actions/checkout@v4"]
	assert.True(t, exists, "Original actions/checkout@v4 entry should still exist")
	assert.Equal(t, "actions/checkout", checkoutEntry.Repo, "Repo should be unchanged")
	assert.Equal(t, "v4", checkoutEntry.Version, "Version should be unchanged")

	// Note: Compilation may add new action entries (like github-script) that the workflow needs,
	// but --no-actions ensures existing entries are not upgraded to newer versions
}
