//go:build integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func compileMCPGatewayWorkflowLock(t *testing.T, workflowContent string) string {
	t.Helper()

	tmpDir := testutil.TempDir(t, "mcp-gateway-env-security")
	workflowPath := filepath.Join(tmpDir, "test-workflow.md")
	require.NoError(t, os.WriteFile(workflowPath, []byte(workflowContent), 0o644))

	compiler := NewCompiler()
	require.NoError(t, compiler.CompileWorkflow(workflowPath))

	lockPath := stringutil.MarkdownToLockFile(workflowPath)
	lockContent, err := os.ReadFile(lockPath)
	require.NoError(t, err)
	return string(lockContent)
}

func extractMCPGatewayStepSection(jobSection, stepName string) string {
	lines := strings.Split(jobSection, "\n")
	var stepLines []string
	var stepPrefix string
	inStep := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !inStep {
			if trimmed != "- name: "+stepName {
				continue
			}
			inStep = true
			idx := strings.Index(line, "- name: ")
			if idx < 0 {
				return ""
			}
			stepPrefix = line[:idx]
			stepLines = append(stepLines, line)
			continue
		}

		if strings.HasPrefix(line, stepPrefix+"- ") {
			break
		}
		stepLines = append(stepLines, line)
	}

	if len(stepLines) == 0 {
		return ""
	}

	return strings.Join(stepLines, "\n")
}

func TestMCPGatewayCustomEnvIntegrationUsesTransportVariables(t *testing.T) {
	lockContent := compileMCPGatewayWorkflowLock(t, `---
on: workflow_dispatch
strict: false
engine: copilot
tools:
  github:
    toolsets: [repos]
sandbox:
  mcp:
    env:
      API_TOKEN: custom-token
      BASH_ENV: "$(touch /tmp/pwned)"
---

# Test Workflow

Verify MCP gateway custom env transport.
`)

	agentSection := extractJobSection(lockContent, "agent")
	require.NotEmpty(t, agentSection)

	gatewayStep := extractMCPGatewayStepSection(agentSection, "Start MCP Gateway")
	require.NotEmpty(t, gatewayStep)

	assert.Contains(t, gatewayStep, `GH_AW_MCP_GATEWAY_CUSTOM_ENV_NAMES: "[\"API_TOKEN\",\"BASH_ENV\"]"`)
	assert.Contains(t, gatewayStep, `GH_AW_MCP_GATEWAY_ENV_0: "custom-token"`)
	assert.Contains(t, gatewayStep, `GH_AW_MCP_GATEWAY_ENV_1: "$(touch /tmp/pwned)"`)
	assert.NotContains(t, gatewayStep, "\n          API_TOKEN:")
	assert.NotContains(t, gatewayStep, "\n          BASH_ENV:")
	assert.NotContains(t, gatewayStep, "export API_TOKEN=")
	assert.NotContains(t, gatewayStep, "export BASH_ENV=")
}

func TestMCPGatewayGuardListsUseStepEnvironment(t *testing.T) {
	lockContent := compileMCPGatewayWorkflowLock(t, `---
on: workflow_dispatch
strict: false
engine: copilot
tools:
  github:
    min-integrity: approved
    blocked-users: [spam-bot]
    trusted-users: [reviewer]
    approval-labels: [approved]
---

# Guard lists
`)
	agentSection := extractJobSection(lockContent, "agent")
	gatewayStep := extractMCPGatewayStepSection(agentSection, "Start MCP Gateway")
	require.NotEmpty(t, gatewayStep)
	for _, name := range []string{"blocked_users", "trusted_users", "approval_labels"} {
		envName := "GH_AW_GUARD_" + strings.ToUpper(name)
		assert.Contains(t, gatewayStep, envName+": ${{ steps.parse-guard-vars.outputs."+name+" }}")
		assert.Contains(t, gatewayStep, "${"+envName+"}")
		runIndex := strings.Index(gatewayStep, "        run: |")
		require.GreaterOrEqual(t, runIndex, 0)
		assert.NotContains(t, gatewayStep[runIndex:], "steps.parse-guard-vars.outputs."+name)
	}
	assert.Contains(t, agentSection, "id: parse-guard-vars")
	assert.Contains(t, agentSection, "GH_AW_BLOCKED_USERS_EXTRA: spam-bot")
	assert.Contains(t, agentSection, "GH_AW_TRUSTED_USERS_EXTRA: reviewer")
	assert.Contains(t, agentSection, "GH_AW_APPROVAL_LABELS_EXTRA: approved")
}

func TestMCPGatewayCustomEnvIntegrationKeepsMetadataNameUnique(t *testing.T) {
	tmpDir := testutil.TempDir(t, "mcp-gateway-env-collision")
	sharedMCPPath := filepath.Join(tmpDir, "shared-mcp.md")
	require.NoError(t, os.WriteFile(sharedMCPPath, []byte(`---
on: workflow_dispatch
mcp-servers:
  collision:
    url: "https://example.com/mcp"
    env:
      FORWARDED_NAMES: ${{ env.GH_AW_MCP_GATEWAY_CUSTOM_ENV_NAMES }}
      FORWARDED_VALUE: ${{ env.GH_AW_MCP_GATEWAY_ENV_5 }}
---`), 0o644))

	workflowPath := filepath.Join(tmpDir, "test-workflow.md")
	require.NoError(t, os.WriteFile(workflowPath, []byte(`---
on: workflow_dispatch
strict: false
engine: copilot
imports:
  - shared-mcp.md
tools:
  github:
    toolsets: [repos]
sandbox:
  mcp:
    env:
      API_TOKEN: custom-token
---

# Test Workflow

Verify MCP gateway reserved env name handling.
`), 0o644))

	compiler := NewCompiler()
	require.NoError(t, compiler.CompileWorkflow(workflowPath))

	lockPath := stringutil.MarkdownToLockFile(workflowPath)
	lockBytes, err := os.ReadFile(lockPath)
	require.NoError(t, err)
	lockContent := string(lockBytes)
	assert.Contains(t, lockContent, `"collision": {`)
	assert.NotContains(t, lockContent, "FORWARDED_NAMES")

	agentSection := extractJobSection(lockContent, "agent")
	require.NotEmpty(t, agentSection)

	gatewayStep := extractMCPGatewayStepSection(agentSection, "Start MCP Gateway")
	require.NotEmpty(t, gatewayStep)

	assert.Equal(t, 1, strings.Count(gatewayStep, mcpGatewayCustomEnvNamesVar+":"))
	assert.Contains(t, gatewayStep, `GH_AW_MCP_GATEWAY_CUSTOM_ENV_NAMES: "[\"API_TOKEN\"]"`)
	assert.Contains(t, gatewayStep, `GH_AW_MCP_GATEWAY_ENV_0: "custom-token"`)
	assert.NotContains(t, gatewayStep, `${{ env.GH_AW_MCP_GATEWAY_CUSTOM_ENV_NAMES }}`)
	assert.NotContains(t, gatewayStep, `${{ env.GH_AW_MCP_GATEWAY_ENV_5 }}`)
	assert.NotContains(t, gatewayStep, "GH_AW_MCP_GATEWAY_ENV_5:")
}

// TestMCPGatewayRunBlockDoesNotInterpolateSecrets is a regression guard for RGS-008:
// secrets (and github.token) referenced by the "Start MCP Gateway" step must be passed
// through the step's env: mapping and read from the shell environment inside the run:
// script body, never interpolated directly as ${{ secrets.* }} / ${{ github.token }}
// expressions in the script text itself.
func TestMCPGatewayRunBlockDoesNotInterpolateSecrets(t *testing.T) {
	lockContent := compileMCPGatewayWorkflowLock(t, `---
on: workflow_dispatch
strict: false
engine: copilot
tools:
  github:
    toolsets: [repos]
sandbox:
  mcp:
    env:
      MY_SECRET: ${{ secrets.MY_CUSTOM_SECRET }}
---

# Test Workflow

Verify MCP gateway run block does not interpolate secrets.
`)

	agentSection := extractJobSection(lockContent, "agent")
	require.NotEmpty(t, agentSection)

	gatewayStep := extractMCPGatewayStepSection(agentSection, "Start MCP Gateway")
	require.NotEmpty(t, gatewayStep)

	runIdx := strings.Index(gatewayStep, "\n        run: |\n")
	require.GreaterOrEqual(t, runIdx, 0, "expected Start MCP Gateway step to contain a run: block")
	runBody := gatewayStep[runIdx:]

	// These three expression forms are the risky patterns called out by RGS-008: a bare
	// ${{ }} expression evaluated inline by GitHub Actions before the shell script runs.
	// This is distinct from (and does not flag) the safe pattern of reading a value that
	// was already passed through the step's env: mapping via a shell variable like
	// "$GITHUB_TOKEN".
	assert.NotContains(t, runBody, "${{ secrets.")
	assert.NotContains(t, runBody, "${{ github.token")
	assert.NotContains(t, runBody, "${{ env.GITHUB_TOKEN")
}
