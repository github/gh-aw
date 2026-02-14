//go:build integration

package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCompletionWorkflowNamesIntegration tests the workflow name completion via the CLI binary
func TestCompletionWorkflowNamesIntegration(t *testing.T) {
	setup := setupIntegrationTest(t)
	defer setup.cleanup()

	// Create some test workflow files
	testWorkflows := []string{"ci-doctor.md", "weekly-research.md", "issue-triage.md"}
	for _, wf := range testWorkflows {
		workflowPath := filepath.Join(setup.workflowsDir, wf)
		content := `---
name: Test Workflow
on:
  workflow_dispatch:
permissions:
  contents: read
engine: copilot
---

# Test Workflow
`
		if err := os.WriteFile(workflowPath, []byte(content), 0o644); err != nil {
			t.Fatalf("Failed to write test workflow file %s: %v", wf, err)
		}
	}

	// Test completion for compile command with empty prefix
	cmd := exec.Command(setup.binaryPath, "__complete", "compile", "")
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "CLI __complete command failed: %s", string(output))

	outputStr := string(output)

	// Should contain all workflow names (without .md extension)
	assert.Contains(t, outputStr, "ci-doctor")
	assert.Contains(t, outputStr, "weekly-research")
	assert.Contains(t, outputStr, "issue-triage")

	// Test completion for compile command with prefix filter
	cmd = exec.Command(setup.binaryPath, "__complete", "compile", "ci")
	output, err = cmd.CombinedOutput()
	require.NoError(t, err, "CLI __complete command with prefix failed: %s", string(output))

	outputStr = string(output)

	// Should contain ci-doctor but not others
	assert.Contains(t, outputStr, "ci-doctor")
	assert.NotContains(t, outputStr, "weekly-research")
	assert.NotContains(t, outputStr, "issue-triage")
}

// TestCompletionEngineNamesIntegration tests the engine flag completion via the CLI binary
func TestCompletionEngineNamesIntegration(t *testing.T) {
	setup := setupIntegrationTest(t)
	defer setup.cleanup()

	// Test completion for --engine flag
	cmd := exec.Command(setup.binaryPath, "__complete", "run", "--engine", "")
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "CLI __complete command for --engine failed: %s", string(output))

	outputStr := string(output)

	// Should contain all engine names
	assert.Contains(t, outputStr, "copilot")
	assert.Contains(t, outputStr, "claude")
	assert.Contains(t, outputStr, "codex")
	assert.Contains(t, outputStr, "custom")
}

// TestCompletionEngineNamesPrefixIntegration tests the engine flag completion with prefix filtering
func TestCompletionEngineNamesPrefixIntegration(t *testing.T) {
	setup := setupIntegrationTest(t)
	defer setup.cleanup()

	// Test completion for --engine flag with "co" prefix
	cmd := exec.Command(setup.binaryPath, "__complete", "run", "--engine", "co")
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "CLI __complete command for --engine with prefix failed: %s", string(output))

	outputStr := string(output)

	// Should contain copilot and codex (start with "co")
	assert.Contains(t, outputStr, "copilot")
	assert.Contains(t, outputStr, "codex")
	// Should not contain claude or custom
	assert.NotContains(t, outputStr, "claude")
	assert.NotContains(t, outputStr, "custom")
}

// TestCompletionMCPListToolsIntegration tests the MCP list-tools completion via the CLI binary
func TestCompletionMCPListToolsIntegration(t *testing.T) {
	setup := setupIntegrationTest(t)
	defer setup.cleanup()

	// Test completion for mcp list-tools first argument (common server names)
	cmd := exec.Command(setup.binaryPath, "__complete", "mcp", "list-tools", "")
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "CLI __complete command for mcp list-tools failed: %s", string(output))

	outputStr := string(output)

	// Should contain common MCP server names
	assert.Contains(t, outputStr, "github")
	assert.Contains(t, outputStr, "playwright")
	assert.Contains(t, outputStr, "tavily")
	assert.Contains(t, outputStr, "safe-outputs")
}

// TestCompletionMCPListToolsWorkflowIntegration tests the MCP list-tools second argument completion
func TestCompletionMCPListToolsWorkflowIntegration(t *testing.T) {
	setup := setupIntegrationTest(t)
	defer setup.cleanup()

	// Create a test workflow file
	workflowPath := filepath.Join(setup.workflowsDir, "test-mcp.md")
	content := `---
name: Test MCP Workflow
on:
  workflow_dispatch:
permissions:
  contents: read
engine: copilot
---

# Test MCP Workflow
`
	if err := os.WriteFile(workflowPath, []byte(content), 0o644); err != nil {
		t.Fatalf("Failed to write test workflow file: %v", err)
	}

	// Test completion for mcp list-tools second argument (workflow names after server name)
	cmd := exec.Command(setup.binaryPath, "__complete", "mcp", "list-tools", "github", "")
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "CLI __complete command for mcp list-tools workflow failed: %s", string(output))

	outputStr := string(output)

	// Should contain workflow name (without .md extension)
	assert.Contains(t, outputStr, "test-mcp")
}

// TestCompletionMCPInspectIntegration tests the MCP inspect completion via the CLI binary
func TestCompletionMCPInspectIntegration(t *testing.T) {
	setup := setupIntegrationTest(t)
	defer setup.cleanup()

	// Create a test workflow file
	workflowPath := filepath.Join(setup.workflowsDir, "mcp-test.md")
	content := `---
name: MCP Test Workflow
on:
  workflow_dispatch:
permissions:
  contents: read
engine: copilot
---

# MCP Test Workflow
`
	if err := os.WriteFile(workflowPath, []byte(content), 0o644); err != nil {
		t.Fatalf("Failed to write test workflow file: %v", err)
	}

	// Test completion for mcp inspect (workflow names)
	cmd := exec.Command(setup.binaryPath, "__complete", "mcp", "inspect", "")
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "CLI __complete command for mcp inspect failed: %s", string(output))

	outputStr := string(output)

	// Should contain workflow name (without .md extension)
	assert.Contains(t, outputStr, "mcp-test")
}

// TestCompletionStatusIntegration tests the status command completion via the CLI binary
func TestCompletionStatusIntegration(t *testing.T) {
	setup := setupIntegrationTest(t)
	defer setup.cleanup()

	// Create test workflow files
	testWorkflows := []string{"alpha-workflow.md", "beta-workflow.md"}
	for _, wf := range testWorkflows {
		workflowPath := filepath.Join(setup.workflowsDir, wf)
		content := `---
name: Test Workflow
on:
  workflow_dispatch:
permissions:
  contents: read
engine: copilot
---

# Test Workflow
`
		if err := os.WriteFile(workflowPath, []byte(content), 0o644); err != nil {
			t.Fatalf("Failed to write test workflow file %s: %v", wf, err)
		}
	}

	// Test completion for status command
	cmd := exec.Command(setup.binaryPath, "__complete", "status", "")
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "CLI __complete command for status failed: %s", string(output))

	outputStr := string(output)

	// Should contain all workflow names (without .md extension)
	assert.Contains(t, outputStr, "alpha-workflow")
	assert.Contains(t, outputStr, "beta-workflow")
}

// TestCompletionLogsIntegration tests the logs command completion via the CLI binary
func TestCompletionLogsIntegration(t *testing.T) {
	setup := setupIntegrationTest(t)
	defer setup.cleanup()

	// Create a test workflow file
	workflowPath := filepath.Join(setup.workflowsDir, "logs-test.md")
	content := `---
name: Logs Test Workflow
on:
  workflow_dispatch:
permissions:
  contents: read
engine: copilot
---

# Logs Test Workflow
`
	if err := os.WriteFile(workflowPath, []byte(content), 0o644); err != nil {
		t.Fatalf("Failed to write test workflow file: %v", err)
	}

	// Test completion for logs command (workflow names)
	cmd := exec.Command(setup.binaryPath, "__complete", "logs", "")
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "CLI __complete command for logs failed: %s", string(output))

	outputStr := string(output)

	// Should contain workflow name (without .md extension)
	assert.Contains(t, outputStr, "logs-test")

	// Test completion for logs --engine flag
	cmd = exec.Command(setup.binaryPath, "__complete", "logs", "--engine", "")
	output, err = cmd.CombinedOutput()
	require.NoError(t, err, "CLI __complete command for logs --engine failed: %s", string(output))

	outputStr = string(output)

	// Should contain engine names
	assert.Contains(t, outputStr, "copilot")
	assert.Contains(t, outputStr, "claude")
}

// TestCompletionDirectiveNoFileComp tests that completion returns NoFileComp directive
func TestCompletionDirectiveNoFileComp(t *testing.T) {
	setup := setupIntegrationTest(t)
	defer setup.cleanup()

	// Test that workflow completion returns ShellCompDirectiveNoFileComp (value 4)
	cmd := exec.Command(setup.binaryPath, "__complete", "compile", "")
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "CLI __complete command failed: %s", string(output))

	outputStr := string(output)

	// The output should contain ":4" which is ShellCompDirectiveNoFileComp
	assert.Contains(t, outputStr, ":4", "Expected ShellCompDirectiveNoFileComp (:4) in output")
}

// setupIntegrationTestForCompletions is a local version of setupIntegrationTest for this file
// This avoids redefinition errors since compile_integration_test.go already defines it
// Note: If tests are run together, the compile_integration_test.go's TestMain and setup are used
func init() {
	// This file relies on the TestMain and setupIntegrationTest from compile_integration_test.go
	// which builds the binary once before running all integration tests.
}
