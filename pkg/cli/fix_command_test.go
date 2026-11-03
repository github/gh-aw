//go:build !integration

package cli

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestProcessWorkflowFileWithInfo_WriteOutputUsesSingleCheckmark(t *testing.T) {
	tmpDir := t.TempDir()
	workflowFile := filepath.Join(tmpDir, "test-workflow.md")

	content := `---
timeout_minutes: 30
---`
	require.NoError(t, os.WriteFile(workflowFile, []byte(content), 0644))

	timeoutCodemod := getCodemodByID("timeout-minutes-migration")
	require.NotNil(t, timeoutCodemod)

	originalStderr := os.Stderr
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stderr = w
	t.Cleanup(func() { os.Stderr = originalStderr })

	fixed, _, err := processWorkflowFileWithInfo(workflowFile, []Codemod{*timeoutCodemod}, true, false)
	require.NoError(t, err)
	require.True(t, fixed)

	require.NoError(t, w.Close())
	outputBytes, err := io.ReadAll(r)
	require.NoError(t, err)
	output := string(outputBytes)

	assert.Contains(t, output, "✓ test-workflow.md")
	assert.NotContains(t, output, "✓ ✓ test-workflow.md")
}

func TestProcessWorkflowFileWithInfo_IteratesUntilStable(t *testing.T) {
	file := filepath.Join(t.TempDir(), "workflow.md")
	content := "---\ntools:\n  web-fetch: true\n---\nBody\n"
	require.NoError(t, os.WriteFile(file, []byte(content), 0o600))
	codemods := []Codemod{
		{
			ID: "remove-fetch", Name: "Remove fetch",
			Apply: func(content string, fm map[string]any) (string, bool, error) {
				if _, ok := fm["engine"]; !ok {
					return content, false, nil
				}
				updated := strings.Replace(content, "  web-fetch: true\n", "", 1)
				return updated, updated != content, nil
			},
		},
		{
			ID: "add-engine", Name: "Add engine",
			Apply: func(content string, fm map[string]any) (string, bool, error) {
				if _, ok := fm["engine"]; ok {
					return content, false, nil
				}
				return strings.Replace(content, "---\n", "---\nengine: copilot\n", 1), true, nil
			},
		},
	}
	fixed, applied, err := processWorkflowFileWithInfo(file, codemods, true, false)
	require.NoError(t, err)
	require.True(t, fixed)
	assert.Equal(t, []string{"Add engine", "Remove fetch"}, applied)
	updated, err := os.ReadFile(file)
	require.NoError(t, err)
	assert.Equal(t, "---\nengine: copilot\ntools:\n---\nBody\n", string(updated))

	fixed, _, err = processWorkflowFileWithInfo(file, codemods, true, false)
	require.NoError(t, err)
	assert.False(t, fixed)
}

func TestProcessWorkflowFileWithInfo_RejectsNonConvergingCodemods(t *testing.T) {
	file := filepath.Join(t.TempDir(), "workflow.md")
	content := "---\nengine: copilot\n---\nBody\n"
	require.NoError(t, os.WriteFile(file, []byte(content), 0o600))
	codemods := []Codemod{
		{
			ID: "toggle", Name: "Toggle",
			Apply: func(content string, _ map[string]any) (string, bool, error) {
				if strings.Contains(content, "engine: copilot") {
					return strings.Replace(content, "engine: copilot", "engine: codex", 1), true, nil
				}
				return strings.Replace(content, "engine: codex", "engine: copilot", 1), true, nil
			},
		},
	}
	_, _, err := processWorkflowFileWithInfo(file, codemods, true, false)
	require.ErrorContains(t, err, "did not converge")
	unchanged, err := os.ReadFile(file)
	require.NoError(t, err)
	assert.Equal(t, content, string(unchanged))
}

func TestRunFixDryRunPreservesNestedWorkflowPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "shared", "foo.md")
	content := `---
jobs:
  publish:
    steps:
      - uses: actions/create-github-app-token@v3
---`
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	originalStderr := os.Stderr
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stderr = w
	t.Cleanup(func() { os.Stderr = originalStderr })

	require.NoError(t, RunFix(FixConfig{
		WorkflowDir:                dir,
		SkipInstructionFileUpdates: true,
	}))
	require.NoError(t, w.Close())
	outputBytes, err := io.ReadAll(r)
	require.NoError(t, err)
	output := string(outputBytes)
	assert.Contains(t, output, "gh aw fix shared/foo --write")
	assert.NotContains(t, output, "gh aw fix foo --write")
}

func TestProcessWorkflowFileWithInfo_StopsAfterThreePasses(t *testing.T) {
	file := filepath.Join(t.TempDir(), "workflow.md")
	content := "---\nstage: 0\n---\nBody\n"
	require.NoError(t, os.WriteFile(file, []byte(content), 0o600))
	transitions := [][2]string{
		{"stage: 0", "stage: 1"},
		{"stage: 1", "stage: 2"},
		{"stage: 2", "stage: 3"},
		{"stage: 3", "stage: 4"},
	}
	calls := 0
	codemods := []Codemod{{
		ID: "advance-stage", Name: "Advance stage",
		Apply: func(content string, _ map[string]any) (string, bool, error) {
			from, to := transitions[calls][0], transitions[calls][1]
			calls++
			updated := strings.Replace(content, from, to, 1)
			return updated, updated != content, nil
		},
	}}

	_, _, err := processWorkflowFileWithInfo(file, codemods, true, false)
	require.ErrorContains(t, err, "did not converge")
	assert.Equal(t, maxCodemodPasses, calls)
	unchanged, err := os.ReadFile(file)
	require.NoError(t, err)
	assert.Equal(t, content, string(unchanged))
}

func TestFixCommand_TimeoutMinutesMigration(t *testing.T) {
	t.Parallel()
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
	fixed, _, err := processWorkflowFileWithInfo(workflowFile, []Codemod{*timeoutCodemod}, true, false)
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
	t.Parallel()
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
	fixed, _, err := processWorkflowFileWithInfo(workflowFile, codemods, false, false)
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
	t.Parallel()
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
	fixed, _, err := processWorkflowFileWithInfo(workflowFile, []Codemod{*firewallCodemod}, true, false)
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

	if !strings.Contains(updatedStr, "sandbox:") {
		t.Errorf("Expected sandbox field to be added for null firewall, got:\n%s", updatedStr)
	}
	if !strings.Contains(updatedStr, "agent: awf") {
		t.Errorf("Expected sandbox.agent: awf to be added for null firewall, got:\n%s", updatedStr)
	}
}

func TestFixCommand_NetworkFirewallMigrationWithNestedProperties(t *testing.T) {
	t.Parallel()
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
	fixed, _, err := processWorkflowFileWithInfo(workflowFile, []Codemod{*firewallCodemod}, true, false)
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

	parsed, err := parser.ExtractFrontmatterFromContent(updatedStr)
	require.NoError(t, err)
	assert.Equal(t, map[string]any{
		"allowed": []any{"defaults", "node", "github"},
	}, parsed.Frontmatter["network"], "Only allowed domains should remain under network")

	if !strings.Contains(updatedStr, "sandbox:") {
		t.Errorf("Expected sandbox field to be added for nested firewall, got:\n%s", updatedStr)
	}
	if !strings.Contains(updatedStr, "agent:") {
		t.Errorf("Expected sandbox.agent object to be added for nested firewall, got:\n%s", updatedStr)
	}
	assert.Equal(t, map[string]any{
		"agent": map[string]any{"id": "awf", "version": "v1.0.0"},
	}, parsed.Frontmatter["sandbox"], "Firewall version should migrate to sandbox.agent as a string")

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
	t.Parallel()
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
	fixed, _, err := processWorkflowFileWithInfo(workflowFile, []Codemod{*firewallCodemod}, true, false)
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

	parsed, err := parser.ExtractFrontmatterFromContent(updatedStr)
	require.NoError(t, err)
	assert.Equal(t, map[string]any{
		"allowed": []any{"defaults", "github"},
	}, parsed.Frontmatter["network"], "Only allowed domains should remain under network")
	assert.Equal(t, map[string]any{
		"agent": map[string]any{"id": "awf", "version": "v1.0.0"},
	}, parsed.Frontmatter["sandbox"], "Firewall version should migrate to sandbox.agent as a string")

	// Comments within the firewall block should also be removed
	if strings.Contains(updatedStr, "# Firewall configuration") {
		t.Error("Expected comment within firewall block to be removed, but it still exists")
	}

	if strings.Contains(updatedStr, "# Version setting") {
		t.Error("Expected comment within firewall block to be removed, but it still exists")
	}

	if !strings.Contains(updatedStr, "sandbox:") {
		t.Errorf("Expected sandbox field to be added for nested firewall, got:\n%s", updatedStr)
	}

	// Verify other network fields are preserved
	if !strings.Contains(updatedStr, "allowed:") {
		t.Error("Expected allowed field to be preserved")
	}
}

func TestFixCommand_PreservesFormatting(t *testing.T) {
	t.Parallel()
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
	fixed, _, err := processWorkflowFileWithInfo(workflowFile, []Codemod{*timeoutCodemod}, true, false)
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
	t.Parallel()
	codemods := GetAllCodemods()

	if len(codemods) == 0 {
		t.Fatal("Expected at least one codemod, got none")
	}
	// Check for required codemods
	expectedIDs := []string{
		"timeout-minutes-migration",
		"network-firewall-migration",
		"command-to-slash-command-migration",
		"mcp-scripts-mode-removal",
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

func TestNewFixCommand_HasDisableCodemodFlag(t *testing.T) {
	t.Parallel()
	cmd := NewFixCommand()
	require.NotNil(t, cmd)

	flag := cmd.Flags().Lookup("disable-codemod")
	require.NotNil(t, flag, "fix command should register --disable-codemod")
	assert.Equal(t, "stringSlice", flag.Value.Type())
	assert.Contains(t, flag.Usage, "Disable specific codemod IDs")
}

func TestRunFix_DisabledCodemodSkipsMatchingFix(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	workflowFile := filepath.Join(tmpDir, "test.md")

	content := `---
on: workflow_dispatch
timeout_minutes: 30
---
# Test Workflow
`
	require.NoError(t, os.WriteFile(workflowFile, []byte(content), 0644))

	err := RunFix(FixConfig{
		Write:              true,
		WorkflowDir:        tmpDir,
		DisabledCodemodIDs: []string{"timeout-minutes-migration"},
	})
	require.NoError(t, err)

	updatedContent, err := os.ReadFile(workflowFile)
	require.NoError(t, err)
	assert.Contains(t, string(updatedContent), "timeout_minutes: 30")
	assert.NotContains(t, string(updatedContent), "timeout-minutes: 30")
}

func TestFixCommand_CommandToSlashCommandMigration(t *testing.T) {
	t.Parallel()
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
	fixed, _, err := processWorkflowFileWithInfo(workflowFile, []Codemod{*commandCodemod}, true, false)
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
	lines := strings.SplitSeq(updatedStr, "\n")
	for line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "command:") && !strings.Contains(line, "slash_command") {
			t.Errorf("Found unreplaced 'command:' field: %s", line)
		}
	}

	if !strings.Contains(updatedStr, "slash_command: my-bot") {
		t.Errorf("Expected on.slash_command: my-bot in updated content, got:\n%s", updatedStr)
	}
}

func TestFixCommand_MCPScriptsModeRemoval(t *testing.T) {
	t.Parallel()
	// Create a temporary directory for test files
	tmpDir := t.TempDir()
	workflowFile := filepath.Join(tmpDir, "test-workflow.md")

	// Create a workflow with deprecated mcp-scripts.mode field
	content := `---
on: workflow_dispatch
engine: copilot
mcp-scripts:
  mode: http
  test-tool:
    description: Test tool
    script: |
      return { result: "test" };
---

# Test Workflow

This is a test workflow with mcp-scripts mode field.
`

	if err := os.WriteFile(workflowFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Get the mcp-scripts mode removal codemod
	modeCodemod := getCodemodByID("mcp-scripts-mode-removal")
	if modeCodemod == nil {
		t.Fatal("mcp-scripts-mode-removal codemod not found")
	}

	// Process the file
	fixed, _, err := processWorkflowFileWithInfo(workflowFile, []Codemod{*modeCodemod}, true, false)
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

	// Verify mcp-scripts block and test-tool are preserved
	if !strings.Contains(updatedStr, "mcp-scripts:") {
		t.Error("Expected mcp-scripts block to be preserved")
	}

	if !strings.Contains(updatedStr, "test-tool:") {
		t.Error("Expected test-tool to be preserved")
	}

	if !strings.Contains(updatedStr, "description: Test tool") {
		t.Error("Expected test-tool description to be preserved")
	}
}

func stubAgenticWorkflowsMarkdownFilesForTest(t *testing.T) {
	t.Helper()

	original := listAgenticWorkflowsMarkdownFiles
	listAgenticWorkflowsMarkdownFiles = func(context.Context) ([]string, error) {
		return []string{
			"create-agentic-workflow.md",
			"update-agentic-workflow.md",
		}, nil
	}
	t.Cleanup(func() {
		listAgenticWorkflowsMarkdownFiles = original
	})
}

func TestWriteGeneratedRepositoryInstructionFile_RefusesDryRun(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	targetPath := filepath.Join(tmpDir, ".github", "skills", "agentic-workflows", "SKILL.md")
	originalContent := []byte("existing content\n")

	require.NoError(t, os.MkdirAll(filepath.Dir(targetPath), 0755))
	require.NoError(t, os.WriteFile(targetPath, originalContent, 0644))

	err := writeGeneratedRepositoryInstructionFile(
		targetPath,
		[]byte("new content\n"),
		false,
		".github/skills/agentic-workflows directory",
		"dispatcher skill",
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "refusing to write dispatcher skill without --write")

	content, readErr := os.ReadFile(targetPath)
	require.NoError(t, readErr)
	assert.Equal(t, string(originalContent), string(content))

	missingPath := filepath.Join(tmpDir, ".github", "agents", "nested", "agentic-workflows.md")
	err = writeGeneratedRepositoryInstructionFile(
		missingPath,
		[]byte("new content\n"),
		false,
		".github/agents directory",
		"Agentic Workflows custom agent",
	)
	require.Error(t, err)
	assert.NoFileExists(t, missingPath)
	assert.NoDirExists(t, filepath.Dir(missingPath))
}

func TestFixCommand_DryRunDoesNotUpdatePromptAndAgentFiles(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		// Skip when git isn't available in the test environment.
		t.Skip("Git not available")
	}
	stubAgenticWorkflowsMarkdownFilesForTest(t)

	// Create a temporary directory for test files
	tmpDir := t.TempDir()
	workflowFile := filepath.Join(tmpDir, "test-workflow.md")
	skillPath := filepath.Join(tmpDir, ".github", "skills", "agentic-workflows", "SKILL.md")
	agentPath := filepath.Join(tmpDir, ".github", "agents", "agentic-workflows.md")

	// Save and restore original directory
	originalDir, err := os.Getwd()
	require.NoError(t, err, "Failed to get current directory")
	t.Cleanup(func() {
		if chdirErr := os.Chdir(originalDir); chdirErr != nil {
			t.Errorf("Failed to restore current directory: %v", chdirErr)
		}
	})

	require.NoError(t, os.Chdir(tmpDir), "Failed to change to temp directory")

	// Initialize git repo (required for ensure functions)
	require.NoError(t, exec.Command("git", "init").Run(), "Failed to initialize git repo")

	// Configure git
	require.NoError(t, exec.Command("git", "config", "user.name", "Test User").Run(), "Failed to configure git user.name")
	require.NoError(t, exec.Command("git", "config", "user.email", "test@example.com").Run(), "Failed to configure git user.email")

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

	require.NoError(t, os.WriteFile(workflowFile, []byte(content), 0644), "Failed to create test file")
	require.NoError(t, os.MkdirAll(filepath.Dir(skillPath), 0755), "Failed to create skill directory")
	require.NoError(t, os.MkdirAll(filepath.Dir(agentPath), 0755), "Failed to create agent directory")

	originalSkill := "local skill change\n"
	originalAgent := "local agent change\n"
	require.NoError(t, os.WriteFile(skillPath, []byte(originalSkill), 0644), "Failed to create skill file")
	require.NoError(t, os.WriteFile(agentPath, []byte(originalAgent), 0644), "Failed to create agent file")

	originalStderr := os.Stderr
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stderr = w
	t.Cleanup(func() { os.Stderr = originalStderr })

	// Run fix command (which refreshes the generated skill and agent files)
	config := FixConfig{
		WorkflowIDs: []string{"test-workflow"},
		Write:       false,
		Verbose:     false,
		WorkflowDir: tmpDir,
	}

	require.NoError(t, RunFix(config), "RunFix failed")
	require.NoError(t, w.Close())

	outputBytes, err := io.ReadAll(r)
	require.NoError(t, err)
	output := string(outputBytes)

	skillContent, err := os.ReadFile(skillPath)
	require.NoError(t, err, "Expected skill file to remain in place after dry-run")
	assert.Equal(t, originalSkill, string(skillContent))

	agentContent, err := os.ReadFile(agentPath)
	require.NoError(t, err, "Expected agent file to remain in place after dry-run")
	assert.Equal(t, originalAgent, string(agentContent))

	assert.Contains(t, output, "Would update dispatcher skill: "+skillPath)
	assert.Contains(t, output, "Would update Agentic Workflows custom agent: "+agentPath)
	assert.Contains(t, output, "No workflow fixes needed")
}

func TestFixCommand_WriteUpdatesPromptAndAgentFiles(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("Git not available")
	}
	stubAgenticWorkflowsMarkdownFilesForTest(t)

	tmpDir := t.TempDir()
	workflowFile := filepath.Join(tmpDir, "test-workflow.md")
	skillPath := filepath.Join(tmpDir, ".github", "skills", "agentic-workflows", "SKILL.md")
	agentPath := filepath.Join(tmpDir, ".github", "agents", "agentic-workflows.md")

	originalDir, err := os.Getwd()
	require.NoError(t, err, "Failed to get current directory")
	t.Cleanup(func() {
		if chdirErr := os.Chdir(originalDir); chdirErr != nil {
			t.Errorf("Failed to restore current directory: %v", chdirErr)
		}
	})

	require.NoError(t, os.Chdir(tmpDir), "Failed to change to temp directory")
	require.NoError(t, exec.Command("git", "init").Run(), "Failed to initialize git repo")
	require.NoError(t, exec.Command("git", "config", "user.name", "Test User").Run(), "Failed to configure git user.name")
	require.NoError(t, exec.Command("git", "config", "user.email", "test@example.com").Run(), "Failed to configure git user.email")

	content := `---
on:
  workflow_dispatch:

permissions:
  contents: read
---

# Test Workflow

This is a test workflow.
`

	require.NoError(t, os.WriteFile(workflowFile, []byte(content), 0644), "Failed to create test file")
	require.NoError(t, os.MkdirAll(filepath.Dir(skillPath), 0755), "Failed to create skill directory")
	require.NoError(t, os.MkdirAll(filepath.Dir(agentPath), 0755), "Failed to create agent directory")
	require.NoError(t, os.WriteFile(skillPath, []byte("local skill change\n"), 0644), "Failed to create skill file")
	require.NoError(t, os.WriteFile(agentPath, []byte("local agent change\n"), 0644), "Failed to create agent file")

	config := FixConfig{
		WorkflowIDs: []string{"test-workflow"},
		Write:       true,
		Verbose:     false,
		WorkflowDir: tmpDir,
	}

	require.NoError(t, RunFix(config), "RunFix failed")

	skillContent, err := os.ReadFile(skillPath)
	require.NoError(t, err, "Expected generated skill file to exist after RunFix")
	assert.Contains(t, string(skillContent), "# Agentic Workflows Router")
	assert.NotContains(t, string(skillContent), "local skill change")

	agentContent, err := os.ReadFile(agentPath)
	require.NoError(t, err, "Expected generated agent file to exist after RunFix")
	assert.Contains(t, string(agentContent), ".github/aw/create-agentic-workflow.md")
	assert.NotContains(t, string(agentContent), ".github/skills/agentic-workflows/SKILL.md")
	assert.NotContains(t, string(agentContent), "local agent change")
}

func TestFixCommand_DryRunTreatsExistingEmptyDispatcherArtifactsAsUpdates(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("Git not available")
	}
	stubAgenticWorkflowsMarkdownFilesForTest(t)

	tmpDir := t.TempDir()
	workflowFile := filepath.Join(tmpDir, "test-workflow.md")
	skillPath := filepath.Join(tmpDir, ".github", "skills", "agentic-workflows", "SKILL.md")
	agentPath := filepath.Join(tmpDir, ".github", "agents", "agentic-workflows.md")

	originalDir, err := os.Getwd()
	require.NoError(t, err)
	t.Cleanup(func() {
		if chdirErr := os.Chdir(originalDir); chdirErr != nil {
			t.Errorf("Failed to restore current directory: %v", chdirErr)
		}
	})

	require.NoError(t, os.Chdir(tmpDir))
	require.NoError(t, exec.Command("git", "init").Run())
	require.NoError(t, exec.Command("git", "config", "user.name", "Test User").Run())
	require.NoError(t, exec.Command("git", "config", "user.email", "test@example.com").Run())

	content := `---
on:
  workflow_dispatch:

permissions:
  contents: read
---
`
	require.NoError(t, os.WriteFile(workflowFile, []byte(content), 0644))
	require.NoError(t, os.MkdirAll(filepath.Dir(skillPath), 0755))
	require.NoError(t, os.MkdirAll(filepath.Dir(agentPath), 0755))
	require.NoError(t, os.WriteFile(skillPath, []byte{}, 0644))
	require.NoError(t, os.WriteFile(agentPath, []byte{}, 0644))

	originalStderr := os.Stderr
	r, w, err := os.Pipe()
	require.NoError(t, err)
	// NOTE: this test redirects os.Stderr and must not run in parallel.
	os.Stderr = w
	t.Cleanup(func() { os.Stderr = originalStderr })

	require.NoError(t, RunFix(FixConfig{
		WorkflowIDs: []string{"test-workflow"},
		Write:       false,
		WorkflowDir: tmpDir,
	}))

	require.NoError(t, w.Close())
	outputBytes, err := io.ReadAll(r)
	require.NoError(t, err)
	output := string(outputBytes)

	assert.Contains(t, output, "Would update dispatcher skill: "+skillPath)
	assert.Contains(t, output, "Would update Agentic Workflows custom agent: "+agentPath)
	assert.NotContains(t, output, "Would create dispatcher skill:")
	assert.NotContains(t, output, "Would create Agentic Workflows custom agent:")
}

func TestFixCommand_GrepToolRemoval(t *testing.T) {
	t.Parallel()
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
	fixed, _, err := processWorkflowFileWithInfo(workflowFile, []Codemod{*grepCodemod}, true, false)
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
	t.Parallel()
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
	fixed, _, err := processWorkflowFileWithInfo(workflowFile, []Codemod{*grepCodemod}, true, false)
	if err != nil {
		t.Fatalf("Failed to process workflow file: %v", err)
	}

	if fixed {
		t.Error("Expected file to not be modified when grep is not present")
	}
}

func TestFixCommand_SandboxFalseToAgentFalseMigration(t *testing.T) {
	t.Parallel()
	// Create a temporary directory for test files
	tmpDir := t.TempDir()
	workflowFile := filepath.Join(tmpDir, "test-workflow.md")

	// Create a workflow with top-level sandbox: false
	content := `---
on:
  workflow_dispatch:

engine: copilot
sandbox: false
strict: false

network:
  allowed:
    - defaults

permissions:
  contents: read
---

# Test Workflow

This workflow has sandbox disabled.
`

	if err := os.WriteFile(workflowFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Get the sandbox migration codemod
	sandboxCodemod := getCodemodByID("sandbox-false-to-agent-false")
	if sandboxCodemod == nil {
		t.Fatal("sandbox-false-to-agent-false codemod not found")
	}

	// Process the file
	fixed, _, err := processWorkflowFileWithInfo(workflowFile, []Codemod{*sandboxCodemod}, true, false)
	if err != nil {
		t.Fatalf("Failed to process workflow file: %v", err)
	}

	if !fixed {
		t.Error("Expected file to be modified")
	}

	// Read the updated file
	updated, err := os.ReadFile(workflowFile)
	if err != nil {
		t.Fatalf("Failed to read updated file: %v", err)
	}
	updatedStr := string(updated)

	// Verify that sandbox: false was converted to sandbox.agent: false
	if strings.Contains(updatedStr, "sandbox: false") {
		t.Error("Expected 'sandbox: false' to be removed")
	}

	if !strings.Contains(updatedStr, "sandbox:") {
		t.Error("Expected 'sandbox:' block to exist")
	}

	if !strings.Contains(updatedStr, "agent: false") {
		t.Error("Expected 'agent: false' to be added")
	}

	// Verify markdown is preserved
	if !strings.Contains(updatedStr, "# Test Workflow") {
		t.Error("Expected markdown heading to be preserved")
	}

	if !strings.Contains(updatedStr, "This workflow has sandbox disabled.") {
		t.Error("Expected markdown body to be preserved")
	}
}

func TestFixCommand_AppToGitHubAppMigration(t *testing.T) {
	t.Parallel()
	// Create a temporary directory for test files
	tmpDir := t.TempDir()
	workflowFile := filepath.Join(tmpDir, "test-workflow.md")

	content := `---
on:
  workflow_dispatch:

engine: copilot
app:
  app-id: ${{ vars.APP_ID }}
  private-key: ${{ secrets.APP_PRIVATE_KEY }}

permissions:
  contents: read
---

# Test Workflow

This workflow uses top-level app auth.
`

	if err := os.WriteFile(workflowFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	appCodemod := getCodemodByID("app-to-github-app")
	if appCodemod == nil {
		t.Fatal("app-to-github-app codemod not found")
	}

	fixed, _, err := processWorkflowFileWithInfo(workflowFile, []Codemod{*appCodemod}, true, false)
	if err != nil {
		t.Fatalf("Failed to process workflow file: %v", err)
	}

	if !fixed {
		t.Error("Expected file to be modified")
	}

	updated, err := os.ReadFile(workflowFile)
	if err != nil {
		t.Fatalf("Failed to read updated file: %v", err)
	}
	updatedStr := string(updated)

	if strings.Contains(updatedStr, "\napp:\n") {
		t.Error("Expected top-level 'app:' to be removed")
	}

	if !strings.Contains(updatedStr, "\ngithub-app:\n") {
		t.Error("Expected top-level 'github-app:' to be added")
	}

	if !strings.Contains(updatedStr, "# Test Workflow") {
		t.Error("Expected markdown heading to be preserved")
	}

	if !strings.Contains(updatedStr, "This workflow uses top-level app auth.") {
		t.Error("Expected markdown body to be preserved")
	}
}

func TestFixCommand_AppToGitHubAppMigration_Combined(t *testing.T) {
	t.Parallel()
	// Combined scenario: top-level app: and nested app: under tools.github both renamed in one pass.
	tmpDir := t.TempDir()
	workflowFile := filepath.Join(tmpDir, "test-workflow.md")

	content := `---
on:
  workflow_dispatch:

engine: copilot
app:
  app-id: ${{ vars.APP_ID }}
  private-key: ${{ secrets.APP_PRIVATE_KEY }}

tools:
  github:
    mode: remote
    app:
      app-id: ${{ vars.APP_ID }}
      private-key: ${{ secrets.APP_PRIVATE_KEY }}

permissions:
  contents: read
---

# Test Workflow

This workflow uses both top-level app auth and nested app auth.
`

	if err := os.WriteFile(workflowFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	appCodemod := getCodemodByID("app-to-github-app")
	if appCodemod == nil {
		t.Fatal("app-to-github-app codemod not found")
	}

	fixed, _, err := processWorkflowFileWithInfo(workflowFile, []Codemod{*appCodemod}, true, false)
	if err != nil {
		t.Fatalf("Failed to process workflow file: %v", err)
	}

	if !fixed {
		t.Error("Expected file to be modified")
	}

	updated, err := os.ReadFile(workflowFile)
	if err != nil {
		t.Fatalf("Failed to read updated file: %v", err)
	}
	updatedStr := string(updated)

	if strings.Contains(updatedStr, "\napp:\n") {
		t.Error("Expected all 'app:' occurrences to be removed")
	}

	if strings.Count(updatedStr, "github-app:") != 2 {
		t.Errorf("Expected two 'github-app:' occurrences, got %d", strings.Count(updatedStr, "github-app:"))
	}

	if !strings.Contains(updatedStr, "# Test Workflow") {
		t.Error("Expected markdown heading to be preserved")
	}

	if !strings.Contains(updatedStr, "This workflow uses both top-level app auth and nested app auth.") {
		t.Error("Expected markdown body to be preserved")
	}
}

func TestFixCommand_SandboxFalseToAgentFalseMigration_NoSandbox(t *testing.T) {
	t.Parallel()
	// Create a temporary directory for test files
	tmpDir := t.TempDir()
	workflowFile := filepath.Join(tmpDir, "test-workflow.md")

	// Create a workflow without sandbox field
	content := `---
on:
  workflow_dispatch:

engine: copilot

permissions:
  contents: read
---

# Test Workflow
`

	if err := os.WriteFile(workflowFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Get the sandbox migration codemod
	sandboxCodemod := getCodemodByID("sandbox-false-to-agent-false")
	if sandboxCodemod == nil {
		t.Fatal("sandbox-false-to-agent-false codemod not found")
	}

	// Process the file
	fixed, _, err := processWorkflowFileWithInfo(workflowFile, []Codemod{*sandboxCodemod}, true, false)
	if err != nil {
		t.Fatalf("Failed to process workflow file: %v", err)
	}

	if fixed {
		t.Error("Expected file to not be modified when sandbox is not present")
	}
}

func TestFixCommand_ScaffoldsSerenaSharedWorkflow(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	workflowDir := filepath.Join(tmpDir, ".github", "workflows")
	workflowFile := filepath.Join(workflowDir, "test-workflow.md")
	if err := os.MkdirAll(workflowDir, 0755); err != nil {
		t.Fatalf("Failed to create workflow directory: %v", err)
	}

	content := `---
engine: copilot
tools:
  serena: ["typescript"]
---

# Test Workflow
`
	if err := os.WriteFile(workflowFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create workflow file: %v", err)
	}

	serenaCodemod := getCodemodByID("serena-tools-to-shared-import")
	if serenaCodemod == nil {
		t.Fatal("serena-tools-to-shared-import codemod not found")
	}

	fixed, _, err := processWorkflowFileWithInfo(workflowFile, []Codemod{*serenaCodemod}, true, false)
	if err != nil {
		t.Fatalf("Failed to process workflow file: %v", err)
	}
	if !fixed {
		t.Fatal("Expected workflow file to be modified")
	}

	scaffoldedPath := filepath.Join(workflowDir, "shared", "mcp", "serena.md")
	scaffoldedContent, err := os.ReadFile(scaffoldedPath)
	if err != nil {
		t.Fatalf("Expected scaffolded Serena workflow to exist: %v", err)
	}

	scaffolded := string(scaffoldedContent)
	if !strings.Contains(scaffolded, "github/gh-aw/.github/workflows/shared/mcp/serena.md@main") {
		t.Errorf("Expected scaffolded Serena workflow to import upstream shared workflow, got:\n%s", scaffolded)
	}

	// The scaffolded file must declare import-schema so that the caller's
	// "languages" input is accepted and can be forwarded to the upstream.
	if !strings.Contains(scaffolded, "import-schema:") {
		t.Errorf("Expected scaffolded Serena workflow to declare import-schema, got:\n%s", scaffolded)
	}
	if !strings.Contains(scaffolded, "languages:") {
		t.Errorf("Expected scaffolded Serena workflow to declare 'languages' in import-schema, got:\n%s", scaffolded)
	}

	// The scaffolded file must forward the languages input to the upstream import via
	// ${{ github.aw.import-inputs.languages }}, otherwise compilation fails with:
	// "required 'with' input 'languages' is missing (declared in import-schema)".
	if !strings.Contains(scaffolded, "github.aw.import-inputs.languages") {
		t.Errorf("Expected scaffolded Serena workflow to pass languages through to upstream import, got:\n%s", scaffolded)
	}
}

func TestRunFix_ScaffoldFailureDoesNotReportSuccessfulCodemods(t *testing.T) {
	t.Chdir(t.TempDir())
	workflowDir := filepath.Join(".github", "workflows")
	require.NoError(t, os.MkdirAll(workflowDir, 0o755))
	workflowFile := filepath.Join(workflowDir, "test-workflow.md")
	content := "---\nengine: copilot\ntools:\n  serena: [\"typescript\"]\n---\nTest\n"
	require.NoError(t, os.WriteFile(workflowFile, []byte(content), 0o600))
	// A file where the shared directory belongs prevents scaffolding after the workflow is written.
	require.NoError(t, os.WriteFile(filepath.Join(workflowDir, "shared"), []byte("blocked"), 0o600))

	var disabled []string
	for _, codemod := range GetAllCodemods() {
		if codemod.ID != "serena-tools-to-shared-import" {
			disabled = append(disabled, codemod.ID)
		}
	}
	serenaCodemod := getCodemodByID("serena-tools-to-shared-import")
	require.NotNil(t, serenaCodemod)

	summary := &updatePRSummary{before: map[string]updatePRFile{}}
	var runErr error
	_, stderr := captureOutput(t, func() error {
		runErr = RunFix(FixConfig{
			Write:              true,
			WorkflowDir:        workflowDir,
			DisabledCodemodIDs: disabled,
			OnApplied:          summary.recordFix,
		})
		return nil
	})
	var exitErr *ExitCodeError
	require.ErrorAs(t, runErr, &exitErr)
	assert.Equal(t, 1, exitErr.Code)
	assert.Contains(t, stderr, "Error processing test-workflow.md")
	assert.Contains(t, stderr, "failed to scaffold shared Serena workflow")
	assert.NotContains(t, stderr, "Applied fixes to")
	assert.NotContains(t, stderr, "No fixes needed")
	assert.Empty(t, summary.applied, "an incomplete migration must not be reported as successfully applied")

	written, err := os.ReadFile(workflowFile)
	require.NoError(t, err)
	assert.Contains(t, string(written), "shared/mcp/serena.md", "the workflow was written before scaffolding failed")
	body := summary.render("upgrade", map[string]updatePRFile{})
	assert.NotContains(t, body, serenaCodemod.Name)
	assert.NotContains(t, body, "Codemod step ran")
	assert.NotContains(t, body, "Execution notes")
}

func TestRunFix_GuidedErrorReturnsExitCode2(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	workflowFile := filepath.Join(tmpDir, "byok-workflow.md")

	// Workflow with a secret in top-level env (triggers the guided-error codemod)
	content := `---
on: workflow_dispatch
env:
  AZURE_CLIENT_ID: ${{ secrets.AZURE_CLIENT_ID }}
---

# BYOK Workflow
`
	require.NoError(t, os.WriteFile(workflowFile, []byte(content), 0644))

	err := RunFix(FixConfig{
		Write:       false,
		WorkflowDir: tmpDir,
	})

	require.Error(t, err, "RunFix should return an error when a guided error is triggered")
	var exitErr *ExitCodeError
	require.ErrorAs(t, err, &exitErr, "error should be an ExitCodeError")
	assert.Equal(t, 2, exitErr.Code, "exit code should be 2 for guided errors")
}

func TestRunFix_GuidedErrorDoesNotPrintNoFixesNeeded(t *testing.T) {
	tmpDir := t.TempDir()
	workflowFile := filepath.Join(tmpDir, "byok-workflow.md")

	// Workflow with a secret in top-level env (triggers the guided-error codemod)
	content := `---
on: workflow_dispatch
env:
  AZURE_CLIENT_ID: ${{ secrets.AZURE_CLIENT_ID }}
---

# BYOK Workflow
`
	require.NoError(t, os.WriteFile(workflowFile, []byte(content), 0644))

	// Capture stderr
	origStderr := os.Stderr
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stderr = w
	t.Cleanup(func() { os.Stderr = origStderr })

	_ = RunFix(FixConfig{
		Write:       false,
		WorkflowDir: tmpDir,
	})

	require.NoError(t, w.Close())
	output, err := io.ReadAll(r)
	require.NoError(t, err)
	outputStr := string(output)

	assert.NotContains(t, outputStr, "No fixes needed", "should not print 'No fixes needed' when a guided error is present")
	assert.Contains(t, outputStr, "1 file needs a manual fix", "should print singular form for a single guided error")
}

func TestRunFix_GuidedErrorWithMultipleFiles(t *testing.T) {
	tmpDir := t.TempDir()

	// File 1: has guided error
	content1 := `---
on: workflow_dispatch
env:
  TOKEN: ${{ secrets.MY_TOKEN }}
---

# Workflow 1
`
	// File 2: has guided error
	content2 := `---
on: workflow_dispatch
env:
  API_KEY: ${{ secrets.API_KEY }}
---

# Workflow 2
`
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "workflow-one.md"), []byte(content1), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "workflow-two.md"), []byte(content2), 0644))

	// Capture stderr
	origStderr := os.Stderr
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stderr = w
	t.Cleanup(func() { os.Stderr = origStderr })

	runErr := RunFix(FixConfig{
		Write:       false,
		WorkflowDir: tmpDir,
	})

	require.NoError(t, w.Close())
	output, err := io.ReadAll(r)
	require.NoError(t, err)
	outputStr := string(output)

	require.Error(t, runErr)
	var exitErr *ExitCodeError
	require.ErrorAs(t, runErr, &exitErr)
	assert.Equal(t, 2, exitErr.Code)

	assert.Contains(t, outputStr, "2 files need a manual fix", "should report plural count")
}

func TestProcessWorkflowFileWithInfo_GuidedErrorWrapped(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	workflowFile := filepath.Join(tmpDir, "test.md")

	content := `---
on: workflow_dispatch
env:
  TOKEN: ${{ secrets.MY_TOKEN }}
---

# Test
`
	require.NoError(t, os.WriteFile(workflowFile, []byte(content), 0644))

	guidedCodemod := getCodemodByID("top-level-env-secrets-guided-error")
	require.NotNil(t, guidedCodemod)

	_, _, err := processWorkflowFileWithInfo(workflowFile, []Codemod{*guidedCodemod}, false, false)

	require.Error(t, err)
	var guidedErr *GuidedError
	assert.ErrorAs(t, err, &guidedErr, "error should be wrapped as GuidedError")
}

func TestRunFix_HardProcessingErrorReturnsExitCode1(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	// Create a directory where a file is expected — reading it will fail
	subdir := filepath.Join(tmpDir, "not-a-file.md")
	require.NoError(t, os.Mkdir(subdir, 0755))

	err := RunFix(FixConfig{
		Write:       false,
		WorkflowDir: tmpDir,
	})

	require.Error(t, err, "RunFix should return an error when a hard processing error occurs")
	var exitErr *ExitCodeError
	require.ErrorAs(t, err, &exitErr, "error should be an ExitCodeError")
	assert.Equal(t, 1, exitErr.Code, "exit code should be 1 for hard processing errors")
}

func TestRunFix_HardProcessingErrorDoesNotPrintNoFixesNeeded(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a directory where a file is expected — reading it will fail
	subdir := filepath.Join(tmpDir, "not-a-file.md")
	require.NoError(t, os.Mkdir(subdir, 0755))

	origStderr := os.Stderr
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stderr = w
	t.Cleanup(func() { os.Stderr = origStderr })

	_ = RunFix(FixConfig{
		Write:       false,
		WorkflowDir: tmpDir,
	})

	require.NoError(t, w.Close())
	output, readErr := io.ReadAll(r)
	require.NoError(t, readErr)

	assert.NotContains(t, string(output), "No fixes needed", "should not print 'No fixes needed' when a hard processing error occurred")
}

func TestRunFix_GuidedErrorCountsInTotalFiles(t *testing.T) {
	tmpDir := t.TempDir()

	// File 1: clean (no fixes needed)
	clean := `---
on: workflow_dispatch
---

# Clean Workflow
`
	// File 2: guided error
	guided := `---
on: workflow_dispatch
env:
  TOKEN: ${{ secrets.MY_TOKEN }}
---

# BYOK Workflow
`
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "a-clean.md"), []byte(clean), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "b-guided.md"), []byte(guided), 0644))

	origStderr := os.Stderr
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stderr = w
	t.Cleanup(func() { os.Stderr = origStderr })

	runErr := RunFix(FixConfig{
		Write:       false,
		WorkflowDir: tmpDir,
	})

	require.NoError(t, w.Close())
	output, readErr := io.ReadAll(r)
	require.NoError(t, readErr)

	// Should still report guided error count and exit 2
	require.Error(t, runErr)
	var exitErr *ExitCodeError
	require.ErrorAs(t, runErr, &exitErr)
	assert.Equal(t, 2, exitErr.Code)

	// "No fixes needed" must be absent
	assert.NotContains(t, string(output), "No fixes needed")
}
