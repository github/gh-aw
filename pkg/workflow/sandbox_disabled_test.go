//go:build integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSandboxDisabled tests that sandbox: false disables all sandbox features
func TestSandboxDisabled(t *testing.T) {
	t.Run("sandbox: false is parsed correctly", func(t *testing.T) {
		workflowsDir := t.TempDir()

		markdown := `---
engine: copilot
sandbox: false
strict: false
on: workflow_dispatch
---

Test workflow with sandbox disabled.
`

		workflowPath := filepath.Join(workflowsDir, "test-sandbox-disabled.md")
		err := os.WriteFile(workflowPath, []byte(markdown), 0644)
		require.NoError(t, err)

		compiler := NewCompiler()
		compiler.SetStrictMode(false) // Non-strict mode to allow sandbox: false
		compiler.SetSkipValidation(true)

		err = compiler.CompileWorkflow(workflowPath)
		require.NoError(t, err, "Compilation should succeed with sandbox: false in non-strict mode")
	})

	t.Run("sandbox: false is refused in strict mode", func(t *testing.T) {
		workflowsDir := t.TempDir()

		markdown := `---
engine: copilot
sandbox: false
strict: false
on: workflow_dispatch
---

Test workflow with sandbox disabled in strict mode.
`

		workflowPath := filepath.Join(workflowsDir, "test-sandbox-disabled-strict.md")
		err := os.WriteFile(workflowPath, []byte(markdown), 0644)
		require.NoError(t, err)

		compiler := NewCompiler()
		compiler.SetStrictMode(true)
		compiler.SetSkipValidation(true)

		err = compiler.CompileWorkflow(workflowPath)
		require.Error(t, err, "Expected error when sandbox: false in strict mode")
		assert.Contains(t, err.Error(), "strict mode")
		assert.Contains(t, err.Error(), "sandbox: false")
	})

	t.Run("sandbox: false disables firewall", func(t *testing.T) {
		workflowsDir := t.TempDir()

		markdown := `---
engine: copilot
sandbox: false
strict: false
network:
  allowed:
    - example.com
on: workflow_dispatch
---

Test workflow with network restrictions but sandbox disabled.
`

		workflowPath := filepath.Join(workflowsDir, "test-sandbox-disabled-firewall.md")
		err := os.WriteFile(workflowPath, []byte(markdown), 0644)
		require.NoError(t, err)

		compiler := NewCompiler()
		compiler.SetStrictMode(false)
		compiler.SetSkipValidation(true)

		err = compiler.CompileWorkflow(workflowPath)
		require.NoError(t, err)

		// Read the compiled workflow
		lockPath := filepath.Join(workflowsDir, "test-sandbox-disabled-firewall.lock.yml")
		lockContent, err := os.ReadFile(lockPath)
		require.NoError(t, err)
		result := string(lockContent)

		// The compiled workflow should NOT contain AWF commands
		assert.NotContains(t, result, "sudo -E awf", "Workflow should not contain AWF command when sandbox is disabled")
		assert.NotContains(t, result, "awf --", "Workflow should not contain AWF wrapper when sandbox is disabled")

		// Should contain direct copilot command instead
		assert.Contains(t, result, "copilot", "Workflow should contain direct copilot command")
	})

	t.Run("sandbox: false skips MCP gateway configuration", func(t *testing.T) {
		workflowsDir := t.TempDir()

		markdown := `---
engine: copilot
sandbox: false
strict: false
tools:
  github:
    mode: local
on: workflow_dispatch
---

Test workflow with tools but sandbox disabled.
`

		workflowPath := filepath.Join(workflowsDir, "test-sandbox-disabled-mcp.md")
		err := os.WriteFile(workflowPath, []byte(markdown), 0644)
		require.NoError(t, err)

		compiler := NewCompiler()
		compiler.SetStrictMode(false)
		compiler.SetSkipValidation(true)

		err = compiler.CompileWorkflow(workflowPath)
		require.NoError(t, err)

		// Read the compiled workflow
		lockPath := filepath.Join(workflowsDir, "test-sandbox-disabled-mcp.lock.yml")
		lockContent, err := os.ReadFile(lockPath)
		require.NoError(t, err)
		result := string(lockContent)

		// The MCP config should NOT contain gateway section when sandbox is disabled
		// Check that MCP config is generated but without gateway
		assert.Contains(t, result, "mcp-config.json", "MCP config should still be generated")
		// Gateway-specific variables should not be present
		assert.NotContains(t, result, "MCP_GATEWAY_PORT", "Gateway port should not be set when sandbox is disabled")
		assert.NotContains(t, result, "MCP_GATEWAY_API_KEY", "Gateway API key should not be set when sandbox is disabled")
	})

	t.Run("sandbox: false shows warning at compile time", func(t *testing.T) {
		workflowsDir := t.TempDir()

		markdown := `---
engine: copilot
sandbox: false
strict: false
on: workflow_dispatch
---

Test workflow.
`

		workflowPath := filepath.Join(workflowsDir, "test-sandbox-disabled-warning.md")
		err := os.WriteFile(workflowPath, []byte(markdown), 0644)
		require.NoError(t, err)

		compiler := NewCompiler()
		compiler.SetStrictMode(false)
		compiler.SetSkipValidation(true)

		// Capture warning count before compilation
		initialWarnings := compiler.GetWarningCount()

		err = compiler.CompileWorkflow(workflowPath)
		require.NoError(t, err)

		// Should have incremented warning count
		finalWarnings := compiler.GetWarningCount()
		assert.Greater(t, finalWarnings, initialWarnings, "Expected warning to be emitted for sandbox: false")
	})

	t.Run("sandbox: true is treated as unconfigured", func(t *testing.T) {
		workflowsDir := t.TempDir()

		markdown := `---
engine: copilot
sandbox: true
network:
  allowed:
    - defaults
on: workflow_dispatch
---

Test workflow with sandbox: true.
`

		workflowPath := filepath.Join(workflowsDir, "test-sandbox-true.md")
		err := os.WriteFile(workflowPath, []byte(markdown), 0644)
		require.NoError(t, err)

		compiler := NewCompiler()
		compiler.SetStrictMode(false)
		compiler.SetSkipValidation(true)

		err = compiler.CompileWorkflow(workflowPath)
		require.NoError(t, err)

		// Read the compiled workflow
		lockPath := filepath.Join(workflowsDir, "test-sandbox-true.lock.yml")
		lockContent, err := os.ReadFile(lockPath)
		require.NoError(t, err)
		result := string(lockContent)

		// sandbox: true should be treated as if no sandbox config was specified
		// This means AWF should be enabled by default
		assert.Contains(t, result, "sudo -E awf", "Workflow should contain AWF command by default when sandbox: true")
	})

	t.Run("sandbox: false applies defaults correctly", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test",
			SandboxConfig: &SandboxConfig{
				Agent: &AgentSandboxConfig{
					Disabled: true,
				},
			},
		}

		// Apply defaults
		sandboxConfig := applySandboxDefaults(workflowData.SandboxConfig, nil)

		// Should preserve the disabled state
		assert.NotNil(t, sandboxConfig)
		assert.NotNil(t, sandboxConfig.Agent)
		assert.True(t, sandboxConfig.Agent.Disabled, "Disabled state should be preserved")
	})

	t.Run("isSandboxDisabled helper function", func(t *testing.T) {
		// Test nil workflow data
		assert.False(t, isSandboxDisabled(nil))

		// Test nil sandbox config
		workflowData := &WorkflowData{Name: "test"}
		assert.False(t, isSandboxDisabled(workflowData))

		// Test enabled sandbox
		workflowData.SandboxConfig = &SandboxConfig{
			Agent: &AgentSandboxConfig{
				Type: SandboxTypeAWF,
			},
		}
		assert.False(t, isSandboxDisabled(workflowData))

		// Test disabled sandbox
		workflowData.SandboxConfig = &SandboxConfig{
			Agent: &AgentSandboxConfig{
				Disabled: true,
			},
		}
		assert.True(t, isSandboxDisabled(workflowData))
	})

	t.Run("MCP gateway config is nil when sandbox disabled", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test",
			SandboxConfig: &SandboxConfig{
				Agent: &AgentSandboxConfig{
					Disabled: true,
				},
			},
		}

		gatewayConfig := buildMCPGatewayConfig(workflowData)
		assert.Nil(t, gatewayConfig, "Gateway config should be nil when sandbox is disabled")
	})

	t.Run("MCP gateway config is not nil when sandbox enabled", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test",
			SandboxConfig: &SandboxConfig{
				Agent: &AgentSandboxConfig{
					Type: SandboxTypeAWF,
				},
			},
		}

		gatewayConfig := buildMCPGatewayConfig(workflowData)
		assert.NotNil(t, gatewayConfig, "Gateway config should not be nil when sandbox is enabled")
		assert.Equal(t, "${MCP_GATEWAY_API_KEY}", gatewayConfig.APIKey)
	})
}

// TestSandboxDisabledWithToolsConfiguration tests that MCP servers work without gateway when sandbox is disabled
func TestSandboxDisabledWithToolsConfiguration(t *testing.T) {
	workflowsDir := t.TempDir()

	markdown := `---
engine: copilot
sandbox: false
strict: false
tools:
  github:
    mode: local
    toolsets: [repos, issues]
on: workflow_dispatch
---

Test workflow with tools and sandbox disabled.
`

	workflowPath := filepath.Join(workflowsDir, "test-sandbox-disabled-tools.md")
	err := os.WriteFile(workflowPath, []byte(markdown), 0644)
	require.NoError(t, err)

	compiler := NewCompiler()
	compiler.SetStrictMode(false)
	compiler.SetSkipValidation(true)

	err = compiler.CompileWorkflow(workflowPath)
	require.NoError(t, err, "Compilation should succeed with tools and sandbox: false")

	// Read the compiled workflow
	lockPath := filepath.Join(workflowsDir, "test-sandbox-disabled-tools.lock.yml")
	lockContent, err := os.ReadFile(lockPath)
	require.NoError(t, err)
	result := string(lockContent)

	// Verify MCP config is generated
	assert.Contains(t, result, "mcp-config.json", "MCP config should be generated")

	// Verify tools are configured in MCP config
	assert.Contains(t, result, "github", "GitHub MCP server should be configured")

	// Verify no gateway configuration
	assert.NotContains(t, result, "MCP_GATEWAY_PORT", "Gateway port should not be present")
	assert.NotContains(t, result, "MCP_GATEWAY_API_KEY", "Gateway API key should not be present")
	assert.NotContains(t, result, "MCP_GATEWAY_DOMAIN", "Gateway domain should not be present")
}

// TestSandboxDisabledCopilotExecution tests that copilot execution is direct (not wrapped with AWF) when sandbox is disabled
func TestSandboxDisabledCopilotExecution(t *testing.T) {
	workflowsDir := t.TempDir()

	markdown := `---
engine: copilot
sandbox: false
strict: false
network:
  allowed:
    - api.github.com
on: workflow_dispatch
---

Test workflow with direct copilot execution.
`

	workflowPath := filepath.Join(workflowsDir, "test-sandbox-disabled-execution.md")
	err := os.WriteFile(workflowPath, []byte(markdown), 0644)
	require.NoError(t, err)

	compiler := NewCompiler()
	compiler.SetStrictMode(false)
	compiler.SetSkipValidation(true)

	err = compiler.CompileWorkflow(workflowPath)
	require.NoError(t, err)

	// Read the compiled workflow
	lockPath := filepath.Join(workflowsDir, "test-sandbox-disabled-execution.lock.yml")
	lockContent, err := os.ReadFile(lockPath)
	require.NoError(t, err)
	result := string(lockContent)

	// The copilot command should be executed directly, not wrapped with AWF
	// Look for direct copilot invocation without AWF
	lines := strings.Split(result, "\n")
	foundDirectCopilot := false
	foundAWF := false

	for _, line := range lines {
		if strings.Contains(line, "copilot ") && !strings.Contains(line, "#") { // Not a comment
			foundDirectCopilot = true
		}
		if strings.Contains(line, "sudo -E awf") || strings.Contains(line, "awf --") {
			foundAWF = true
		}
	}

	assert.True(t, foundDirectCopilot, "Should find direct copilot command")
	assert.False(t, foundAWF, "Should not find AWF wrapper when sandbox is disabled")
}
