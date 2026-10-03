//go:build integration

package workflow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPiSafeOutputsPromptStatesCLIIsOnlyTransportIntegration verifies that a compiled
// Pi workflow with cli-proxy enabled never describes the safeoutputs CLI as an
// optional alternative to a direct tool call.
func TestPiSafeOutputsPromptStatesCLIIsOnlyTransportIntegration(t *testing.T) {
	tmpDir := testutil.TempDir(t, "pi-safeoutputs-cli-only")
	workflowPath := filepath.Join(tmpDir, "pi-safeoutputs.md")
	workflowContent := `---
on: issues
name: Pi Safe Outputs
engine: pi
tools:
  bash: ["*"]
  cli-proxy: true
safe-outputs:
  create-pull-request:
---

Fix the bug and open a pull request.
`
	require.NoError(t, os.WriteFile(workflowPath, []byte(workflowContent), 0o600))

	compiler := NewCompiler()
	require.NoError(t, compiler.CompileWorkflow(workflowPath))

	lockPath := filepath.Join(tmpDir, "pi-safeoutputs.lock.yml")
	compiledBytes, err := os.ReadFile(lockPath)
	require.NoError(t, err)
	compiled := string(compiledBytes)

	assert.NotContains(t, compiled, "optional equivalent transport",
		"CLI-proxy Pi workflows must not describe the safeoutputs CLI as an optional transport")
	assert.NotContains(t, compiled, "you may use that CLI form instead",
		"CLI-proxy Pi workflows must not describe the safeoutputs CLI as an alternative form")
	assert.Contains(t, compiled, safeOutputsCLIOnlyTransportPromptFile,
		"CLI-proxy Pi workflows must load the CLI-only safe-output transport prompt")
	assert.NotContains(t, compiled, safeOutputsMCPTransportPromptFile,
		"CLI-proxy Pi workflows must not load the MCP-safe-output transport prompt")
}

func TestPiSafeOutputsPromptUsesMCPTransportIntegration(t *testing.T) {
	tmpDir := testutil.TempDir(t, "pi-safeoutputs-mcp")
	workflowPath := filepath.Join(tmpDir, "pi-safeoutputs.md")
	workflowContent := `---
on: issues
name: Pi Safe Outputs
engine: pi
tools:
  bash: ["echo"]
  cli-proxy: false
safe-outputs:
  create-pull-request:
---

Fix the bug and open a pull request.
`
	require.NoError(t, os.WriteFile(workflowPath, []byte(workflowContent), 0o600))

	compiler := NewCompiler()
	require.NoError(t, compiler.CompileWorkflow(workflowPath))

	compiledBytes, err := os.ReadFile(filepath.Join(tmpDir, "pi-safeoutputs.lock.yml"))
	require.NoError(t, err)
	compiled := string(compiledBytes)

	assert.Contains(t, compiled, safeOutputsMCPTransportPromptFile,
		"Pi with native MCP must load the MCP-safe-output transport prompt")
	assert.NotContains(t, compiled, safeOutputsCLIOnlyTransportPromptFile,
		"Pi with native MCP must not load the CLI-only safe-output transport prompt")
}

// TestCopilotSafeOutputsPromptKeepsOptionalCLITransportIntegration verifies that engines
// with native MCP tool-calling keep the transport-neutral wording.
func TestCopilotSafeOutputsPromptKeepsOptionalCLITransportIntegration(t *testing.T) {
	tmpDir := testutil.TempDir(t, "copilot-safeoutputs-optional-cli")
	workflowPath := filepath.Join(tmpDir, "copilot-safeoutputs.md")
	workflowContent := `---
on: issues
name: Copilot Safe Outputs
engine: copilot
tools:
  bash: ["echo"]
safe-outputs:
  create-pull-request:
---

Fix the bug and open a pull request.
`
	require.NoError(t, os.WriteFile(workflowPath, []byte(workflowContent), 0o600))

	compiler := NewCompiler()
	require.NoError(t, compiler.CompileWorkflow(workflowPath))

	lockPath := filepath.Join(tmpDir, "copilot-safeoutputs.lock.yml")
	compiledBytes, err := os.ReadFile(lockPath)
	require.NoError(t, err)
	compiled := string(compiledBytes)

	assert.Contains(t, compiled, safeOutputsMCPTransportPromptFile,
		"MCP-capable workflows should load the MCP-safe-output transport prompt")
	assert.NotContains(t, compiled, safeOutputsCLIOnlyTransportPromptFile,
		"MCP-capable workflows must not load the CLI-only safe-output transport prompt")
}
