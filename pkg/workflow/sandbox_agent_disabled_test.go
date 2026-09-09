//go:build integration

package workflow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSandboxBooleanRejected tests that top-level sandbox: false is now rejected
func TestSandboxBooleanRejected(t *testing.T) {
	t.Run("sandbox: false is rejected (use sandbox.agent: false instead)", func(t *testing.T) {
		workflowsDir := t.TempDir()

		markdown := `---
engine: copilot
sandbox: false
strict: false
on: workflow_dispatch
---

Test workflow with top-level sandbox: false (no longer supported).
`

		workflowPath := filepath.Join(workflowsDir, "test-sandbox-false.md")
		err := os.WriteFile(workflowPath, []byte(markdown), 0644)
		require.NoError(t, err)

		compiler := NewCompiler()
		compiler.SetStrictMode(false)
		compiler.SetSkipValidation(false) // Enable validation to catch schema error

		err = compiler.CompileWorkflow(workflowPath)
		require.Error(t, err, "Expected error when using sandbox: false (top-level boolean no longer supported)")
		require.ErrorContains(t, err, "sandbox", "Error should mention sandbox field")
	})

	t.Run("sandbox: true is also rejected", func(t *testing.T) {
		workflowsDir := t.TempDir()

		markdown := `---
engine: copilot
sandbox: true
network:
  allowed:
    - defaults
on: workflow_dispatch
---

Test workflow with sandbox: true (meaningless).
`

		workflowPath := filepath.Join(workflowsDir, "test-sandbox-true.md")
		err := os.WriteFile(workflowPath, []byte(markdown), 0644)
		require.NoError(t, err)

		compiler := NewCompiler()
		compiler.SetStrictMode(false)
		compiler.SetSkipValidation(false)

		err = compiler.CompileWorkflow(workflowPath)
		require.Error(t, err, "Expected error when using sandbox: true (top-level boolean no longer supported)")
	})
}

// TestSandboxAgentFalse tests that sandbox.agent: false works correctly
func TestSandboxAgentFalse(t *testing.T) {
	t.Run("sandbox.agent: false disables firewall but keeps MCP gateway", func(t *testing.T) {
		workflowsDir := t.TempDir()

		markdown := `---
engine: copilot
features:
  dangerously-disable-sandbox-agent: true
sandbox:
  agent: false
strict: false
network:
  allowed:
    - example.com
on: workflow_dispatch
---

Test workflow with agent sandbox disabled.
`

		workflowPath := filepath.Join(workflowsDir, "test-agent-false.md")
		err := os.WriteFile(workflowPath, []byte(markdown), 0644)
		require.NoError(t, err)

		compiler := NewCompiler()
		compiler.SetStrictMode(false)
		compiler.SetSkipValidation(true)

		err = compiler.CompileWorkflow(workflowPath)
		require.NoError(t, err, "Compilation should succeed with sandbox.agent: false in non-strict mode")

		// Read the compiled workflow
		lockPath := filepath.Join(workflowsDir, "test-agent-false.lock.yml")
		lockContent, err := os.ReadFile(lockPath)
		require.NoError(t, err)
		result := string(lockContent)

		// The compiled workflow should NOT contain AWF commands
		assert.NotContains(t, result, "sudo -E ", "Workflow should not contain privileged AWF command when agent sandbox is disabled")
		assert.NotContains(t, result, "awf --", "Workflow should not contain AWF wrapper when agent sandbox is disabled")

		// Should contain direct copilot command instead
		assert.Contains(t, result, "copilot", "Workflow should contain direct copilot command")

		// COPILOT_API_KEY (BYOK dummy key) must NOT be injected when sandbox is disabled
		// — no api-proxy is running, so the key would break authentication
		assert.NotContains(t, result, "COPILOT_API_KEY", "COPILOT_API_KEY must not be present when sandbox.agent: false")

		// AWF_REFLECT_ENABLED must NOT be injected when sandbox is disabled
		// — the /reflect endpoint is only available when the api-proxy sidecar is running
		assert.NotContains(t, result, "AWF_REFLECT_ENABLED", "AWF_REFLECT_ENABLED must not be present when sandbox.agent: false")

		// MCP gateway should still be present (always enabled)
		assert.Contains(t, result, "Start MCP Gateway", "MCP gateway should be present even when agent sandbox is disabled")
		assert.Contains(t, result, "MCP_GATEWAY_PORT", "Gateway port should be set")
		assert.Contains(t, result, "MCP_GATEWAY_AGENT_ID", "Gateway agent ID should be set")
	})

	t.Run("sandbox.agent: false is refused in strict mode", func(t *testing.T) {
		workflowsDir := t.TempDir()

		markdown := `---
engine: copilot
features:
  dangerously-disable-sandbox-agent: true
sandbox:
  agent: false
strict: true
on: workflow_dispatch
---

Test workflow with agent sandbox disabled in strict mode.
`

		workflowPath := filepath.Join(workflowsDir, "test-agent-false-strict.md")
		err := os.WriteFile(workflowPath, []byte(markdown), 0644)
		require.NoError(t, err)

		compiler := NewCompiler()
		compiler.SetStrictMode(true)
		compiler.SetSkipValidation(true)

		err = compiler.CompileWorkflow(workflowPath)
		require.Error(t, err, "Expected error when sandbox.agent: false in strict mode")
		require.ErrorContains(t, err, "strict mode")
		require.ErrorContains(t, err, "sandbox.agent: false")
	})

	t.Run("sandbox.agent: false does not show a deprecation warning", func(t *testing.T) {
		workflowsDir := t.TempDir()

		markdown := `---
engine: copilot
features:
  dangerously-disable-sandbox-agent: true
sandbox:
  agent: false
strict: false
on: workflow_dispatch
---

Test workflow.
`

		workflowPath := filepath.Join(workflowsDir, "test-agent-false-warning.md")
		err := os.WriteFile(workflowPath, []byte(markdown), 0644)
		require.NoError(t, err)

		compiler := NewCompiler()
		compiler.SetStrictMode(false)
		compiler.SetSkipValidation(true)

		// Capture warning count before compilation
		initialWarnings := compiler.GetWarningCount()

		err = compiler.CompileWorkflow(workflowPath)
		require.NoError(t, err)

		// Supported non-strict usage should not increment the warning count
		finalWarnings := compiler.GetWarningCount()
		assert.Equal(t, initialWarnings, finalWarnings, "Expected no warning for supported sandbox.agent: false")
	})
}

// TestSandboxAgentFalseWithTools tests that MCP servers work when agent sandbox is disabled
func TestSandboxAgentFalseWithTools(t *testing.T) {
	workflowsDir := t.TempDir()

	markdown := `---
engine: copilot
features:
  dangerously-disable-sandbox-agent: true
sandbox:
  agent: false
strict: false
tools:
  github:
    mode: local
    toolsets: [repos, issues]
on: workflow_dispatch
---

Test workflow with tools and agent sandbox disabled.
`

	workflowPath := filepath.Join(workflowsDir, "test-agent-false-tools.md")
	err := os.WriteFile(workflowPath, []byte(markdown), 0644)
	require.NoError(t, err)

	compiler := NewCompiler()
	compiler.SetStrictMode(false)
	compiler.SetSkipValidation(true)

	err = compiler.CompileWorkflow(workflowPath)
	require.NoError(t, err, "Compilation should succeed with tools and sandbox.agent: false")

	// Read the compiled workflow
	lockPath := filepath.Join(workflowsDir, "test-agent-false-tools.lock.yml")
	lockContent, err := os.ReadFile(lockPath)
	require.NoError(t, err)
	result := string(lockContent)

	// Verify MCP config is generated
	assert.Contains(t, result, "mcp-config.json", "MCP config should be generated")

	// Verify tools are configured in MCP config
	assert.Contains(t, result, "github", "GitHub MCP Server should be configured")

	// Verify gateway configuration is present
	assert.Contains(t, result, "MCP_GATEWAY_PORT", "Gateway port should be present")
	assert.Contains(t, result, "MCP_GATEWAY_AGENT_ID", "Gateway agent ID should be present")
	assert.Contains(t, result, "MCP_GATEWAY_DOMAIN", "Gateway domain should be present")
}

// TestHelperFunctions tests the new helper functions
func TestHelperFunctions(t *testing.T) {
	t.Run("isSandboxDisabled always returns false", func(t *testing.T) {
		// Test nil workflow data
		assert.False(t, isSandboxDisabled(nil))

		// Test nil sandbox config
		workflowData := &WorkflowData{Name: "test"}
		assert.False(t, isSandboxDisabled(workflowData))

		// Test with agent disabled
		workflowData.SandboxConfig = &SandboxConfig{
			Agent: &AgentSandboxConfig{
				Disabled: true,
			},
		}
		assert.False(t, isSandboxDisabled(workflowData), "isSandboxDisabled should always return false (deprecated)")
	})

	t.Run("isAgentSandboxDisabled detects agent sandbox disabled", func(t *testing.T) {
		// Test nil workflow data
		assert.False(t, isAgentSandboxDisabled(nil))

		// Test nil sandbox config
		workflowData := &WorkflowData{Name: "test"}
		assert.False(t, isAgentSandboxDisabled(workflowData))

		// Test enabled sandbox
		workflowData.SandboxConfig = &SandboxConfig{
			Agent: &AgentSandboxConfig{
				Type: SandboxTypeAWF,
			},
		}
		assert.False(t, isAgentSandboxDisabled(workflowData))

		// Test disabled agent sandbox
		workflowData.SandboxConfig = &SandboxConfig{
			Agent: &AgentSandboxConfig{
				Disabled: true,
			},
		}
		assert.True(t, isAgentSandboxDisabled(workflowData))
	})
}
