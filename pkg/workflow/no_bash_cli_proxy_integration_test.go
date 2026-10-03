//go:build integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNoBashSafeOutputsUsesMCPOnlyPromptIntegration(t *testing.T) {
	tmpDir := testutil.TempDir(t, "no-bash-safeoutputs-mcp-only")
	workflowPath := filepath.Join(tmpDir, "no-bash-safeoutputs.md")
	workflowContent := `---
on: issues
name: No Bash Safe Outputs
engine: codex
tools:
  bash: false
  cli-proxy: false
  github:
    mode: local
    min-integrity: none
safe-outputs:
  add-labels:
---

Add a label safely.
`
	require.NoError(t, os.WriteFile(workflowPath, []byte(workflowContent), 0o600))

	compiler := NewCompiler()
	require.NoError(t, compiler.CompileWorkflow(workflowPath))

	lockPath := filepath.Join(tmpDir, "no-bash-safeoutputs.lock.yml")
	compiledBytes, err := os.ReadFile(lockPath)
	require.NoError(t, err)
	compiled := string(compiledBytes)

	assert.Contains(t, compiled, "-c features.shell_tool=false",
		"Codex should receive the no-shell runtime setting when bash is disabled")
	assert.Contains(t, compiled, "Mount MCP servers as CLIs",
		"safeoutputs should still be mounted as a CLI for command-based harnesses")
	assert.Contains(t, compiled, `"safeoutputs": {`,
		"safeoutputs must remain available in the gateway MCP server config")
	assert.Contains(t, compiled, "convert_gateway_config_codex.cjs",
		"Codex must convert the gateway config to its native config at runtime")
	assert.Contains(t, compiled, "<safe-output-tools>",
		"safe output MCP guidance should remain in the prompt")
	assert.NotContains(t, compiled, "mcp_cli_tools_with_safeoutputs_prompt.md",
		"no-shell workflows must not advertise the bash-only safeoutputs CLI prompt")
	assert.NotContains(t, compiled, "mcp_cli_tools_prompt.md",
		"no-shell workflows must not advertise MCP CLI prompts")
	assert.NotContains(t, compiled, "GH_AW_MCP_CLI_SERVERS_LIST",
		"the prompt substitution env should be omitted with the MCP CLI prompt")
}

func TestNoBashShellBackedToolModesRejectedIntegration(t *testing.T) {
	tests := []struct {
		name          string
		tools         string
		errorContains string
	}{
		{
			name: "cli proxy true",
			tools: `  bash: false
  cli-proxy: true
  github:
    mode: local`,
			errorContains: "tools.cli-proxy",
		},
		{
			name: "github gh proxy",
			tools: `  bash: false
  cli-proxy: false
  github:
    mode: gh-proxy`,
			errorContains: "tools.github.mode: gh-proxy",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := testutil.TempDir(t, "no-bash-shell-backed-mode")
			workflowPath := filepath.Join(tmpDir, strings.ReplaceAll(tt.name, " ", "-")+".md")
			workflowContent := `---
on: push
name: No Bash Invalid Tools
engine: codex
tools:
` + tt.tools + `
safe-outputs:
  create-issue:
---

Invalid no-shell workflow.
`
			require.NoError(t, os.WriteFile(workflowPath, []byte(workflowContent), 0o600))

			compiler := NewCompiler()
			err := compiler.CompileWorkflow(workflowPath)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.errorContains)
		})
	}
}
