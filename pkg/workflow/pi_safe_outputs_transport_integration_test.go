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
// `engine: pi` workflow (EngineCapabilities.MCP == false) never describes the
// safeoutputs CLI as an optional alternative to a direct tool call, because pi has no
// MCP tool-call interface at all.
func TestPiSafeOutputsPromptStatesCLIIsOnlyTransportIntegration(t *testing.T) {
	tmpDir := testutil.TempDir(t, "pi-safeoutputs-cli-only")
	workflowPath := filepath.Join(tmpDir, "pi-safeoutputs.md")
	workflowContent := `---
on: issues
name: Pi Safe Outputs
engine: pi
tools:
  bash: ["*"]
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
		"pi workflows must not describe the safeoutputs CLI as an optional transport")
	assert.NotContains(t, compiled, "you may use that CLI form instead",
		"pi workflows must not describe the safeoutputs CLI as an alternative form")
	assert.Contains(t, compiled, safeOutputsTransportEnvVar,
		"compiled workflow should substitute the safe-output transport wording")
	assert.Contains(t, compiled, "is the ONLY way to invoke the tools listed in",
		"pi workflows must state the safeoutputs CLI is the only transport")
	assert.Contains(t, compiled, "the CLI commands above are the ONLY transport",
		"pi workflows must state the safeoutputs CLI commands are the only transport")
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

	assert.Contains(t, compiled, "optional equivalent transport",
		"MCP-capable engines should keep the optional CLI transport wording")
	assert.NotContains(t, compiled, "the CLI commands above are the ONLY transport",
		"MCP-capable engines must not be told the CLI is the only transport")
}
