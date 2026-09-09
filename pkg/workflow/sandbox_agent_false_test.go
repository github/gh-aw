//go:build integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSandboxAgentMandatory(t *testing.T) {
	t.Run("sandbox.agent: false with feature flag is accepted and disables agent sandbox", func(t *testing.T) {
		// Create temp directory for test workflows
		workflowsDir := t.TempDir()

		markdown := `---
engine: copilot
network:
  allowed:
    - defaults
    - github.com
features:
  dangerously-disable-sandbox-agent: true
sandbox:
  agent: false
strict: false
on: workflow_dispatch
---

Test workflow to verify sandbox.agent: false is accepted when the feature flag is set.
`

		workflowPath := filepath.Join(workflowsDir, "test-agent-false.md")
		err := os.WriteFile(workflowPath, []byte(markdown), 0644)
		if err != nil {
			t.Fatalf("Failed to write workflow file: %v", err)
		}

		// Compile the workflow (validation runs unconditionally; validateSandboxConfig
		// is not gated by skipValidation, so this exercises the feature-flag check)
		compiler := NewCompiler()

		// Should succeed when the feature flag is set
		if err := compiler.CompileWorkflow(workflowPath); err != nil {
			t.Fatalf("Expected compilation to succeed with sandbox.agent: false and feature flag, but got error: %v", err)
		}

		// Read the compiled workflow
		lockPath := filepath.Join(workflowsDir, "test-agent-false.lock.yml")
		lockContent, err := os.ReadFile(lockPath)
		if err != nil {
			t.Fatalf("Failed to read compiled workflow: %v", err)
		}

		lockStr := string(lockContent)

		// Verify that AWF firewall is NOT present (agent sandbox disabled)
		if strings.Contains(lockStr, "sudo -E ") {
			t.Error("Expected AWF firewall to be disabled, but found privileged AWF command in lock file")
		}

		// Verify that MCP gateway IS present (gateway always enabled)
		if !strings.Contains(lockStr, "Start MCP Gateway") {
			t.Error("Expected MCP gateway to be enabled, but did not find 'Start MCP Gateway' in lock file")
		}
	})

	t.Run("sandbox.agent: awf enables firewall", func(t *testing.T) {
		// Create temp directory for test workflows
		workflowsDir := t.TempDir()

		markdown := `---
engine: copilot
network:
  allowed:
    - defaults
sandbox:
  agent: awf
on: workflow_dispatch
---

Test workflow to verify sandbox.agent: awf enables firewall.
`

		workflowPath := filepath.Join(workflowsDir, "test-agent-awf.md")
		err := os.WriteFile(workflowPath, []byte(markdown), 0644)
		if err != nil {
			t.Fatalf("Failed to write workflow file: %v", err)
		}

		// Compile the workflow
		compiler := NewCompiler()
		compiler.SetSkipValidation(true)

		if err := compiler.CompileWorkflow(workflowPath); err != nil {
			t.Fatalf("Compilation failed: %v", err)
		}

		// Read the compiled workflow
		lockPath := filepath.Join(workflowsDir, "test-agent-awf.lock.yml")
		lockContent, err := os.ReadFile(lockPath)
		if err != nil {
			t.Fatalf("Failed to read compiled workflow: %v", err)
		}

		lockStr := string(lockContent)

		// Verify that AWF installation IS present (rootless by default)
		if !strings.Contains(lockStr, "awf --config ") {
			t.Error("Expected AWF firewall to be enabled, but did not find rootless 'awf --config' command in lock file")
		}
	})

	t.Run("default sandbox enables firewall (awf)", func(t *testing.T) {
		// Create temp directory for test workflows
		workflowsDir := t.TempDir()

		markdown := `---
engine: copilot
strict: false
network:
  allowed:
    - defaults
    - github.com
on: workflow_dispatch
---

Test workflow to verify default sandbox.agent behavior (awf).
`

		workflowPath := filepath.Join(workflowsDir, "test-default-firewall.md")
		err := os.WriteFile(workflowPath, []byte(markdown), 0644)
		if err != nil {
			t.Fatalf("Failed to write workflow file: %v", err)
		}

		// Compile the workflow
		compiler := NewCompiler()
		compiler.SetSkipValidation(true)

		if err := compiler.CompileWorkflow(workflowPath); err != nil {
			t.Fatalf("Compilation failed: %v", err)
		}

		// Read the compiled workflow
		lockPath := filepath.Join(workflowsDir, "test-default-firewall.lock.yml")
		lockContent, err := os.ReadFile(lockPath)
		if err != nil {
			t.Fatalf("Failed to read compiled workflow: %v", err)
		}

		lockStr := string(lockContent)

		// With network restrictions and no sandbox config, firewall should be enabled by default
		if !strings.Contains(lockStr, "awf --config ") {
			t.Error("Expected firewall to be enabled by default with network restrictions, but did not find rootless 'awf --config' command in lock file")
		}
	})
}

func TestSandboxAgentFalseRequiresFeatureFlag(t *testing.T) {
	t.Run("sandbox.agent: false without feature flag is rejected", func(t *testing.T) {
		workflowsDir := t.TempDir()

		markdown := `---
engine: copilot
network:
  allowed:
    - defaults
    - github.com
sandbox:
  agent: false
strict: false
on: workflow_dispatch
---

Test workflow to verify sandbox.agent: false is rejected without the feature flag.
`

		workflowPath := filepath.Join(workflowsDir, "test-agent-false-no-flag.md")
		err := os.WriteFile(workflowPath, []byte(markdown), 0644)
		if err != nil {
			t.Fatalf("Failed to write workflow file: %v", err)
		}

		compiler := NewCompiler()

		err = compiler.CompileWorkflow(workflowPath)
		if err == nil {
			t.Fatal("Expected compilation to fail when sandbox.agent: false without feature flag, but got nil error")
		}
		if !strings.Contains(err.Error(), "dangerously-disable-sandbox-agent") {
			t.Fatalf("Expected error to reference 'dangerously-disable-sandbox-agent', got: %v", err)
		}
	})

	t.Run("sandbox.agent: false with feature false is rejected", func(t *testing.T) {
		workflowsDir := t.TempDir()

		markdown := `---
engine: copilot
network:
  allowed:
    - defaults
    - github.com
features:
  dangerously-disable-sandbox-agent: false
sandbox:
  agent: false
strict: false
on: workflow_dispatch
---

Test workflow to verify sandbox.agent: false is rejected when the feature is false.
`

		workflowPath := filepath.Join(workflowsDir, "test-agent-false-disabled-flag.md")
		err := os.WriteFile(workflowPath, []byte(markdown), 0644)
		if err != nil {
			t.Fatalf("Failed to write workflow file: %v", err)
		}

		compiler := NewCompiler()
		err = compiler.CompileWorkflow(workflowPath)
		if err == nil {
			t.Fatal("Expected compilation to fail when the feature is false, but got nil error")
		}
		if !strings.Contains(err.Error(), "dangerously-disable-sandbox-agent") {
			t.Fatalf("Expected error to reference 'dangerously-disable-sandbox-agent', got: %v", err)
		}
	})

	t.Run("sandbox.agent: false with string feature is rejected", func(t *testing.T) {
		workflowsDir := t.TempDir()

		markdown := `---
engine: copilot
network:
  allowed:
    - defaults
    - github.com
features:
  dangerously-disable-sandbox-agent: "true"
sandbox:
  agent: false
strict: false
on: workflow_dispatch
---

Test workflow to verify sandbox.agent: false is rejected when the feature is not a boolean.
`

		workflowPath := filepath.Join(workflowsDir, "test-agent-false-string-flag.md")
		err := os.WriteFile(workflowPath, []byte(markdown), 0644)
		if err != nil {
			t.Fatalf("Failed to write workflow file: %v", err)
		}

		compiler := NewCompiler()
		err = compiler.CompileWorkflow(workflowPath)
		if err == nil {
			t.Fatal("Expected compilation to fail when the feature is a string, but got nil error")
		}
		if !strings.Contains(err.Error(), "dangerously-disable-sandbox-agent") {
			t.Fatalf("Expected error to reference 'dangerously-disable-sandbox-agent', got: %v", err)
		}
	})
}

func TestNetworkFirewallFrontmatterRejected(t *testing.T) {
	t.Run("network.firewall is rejected by schema", func(t *testing.T) {
		// Create temp directory for test workflows
		workflowsDir := t.TempDir()

		markdown := `---
engine: copilot
network:
  allowed:
    - defaults
  firewall: false
strict: false
on: workflow_dispatch
---

		Test workflow to verify network.firewall is rejected.
`

		workflowPath := filepath.Join(workflowsDir, "test-firewall-deprecated.md")
		err := os.WriteFile(workflowPath, []byte(markdown), 0644)
		if err != nil {
			t.Fatalf("Failed to write workflow file: %v", err)
		}

		compiler := NewCompiler()

		err = compiler.CompileWorkflow(workflowPath)
		if err == nil {
			t.Fatal("Expected compilation to fail for deprecated network.firewall frontmatter, but got nil error")
		}
		if !strings.Contains(err.Error(), "firewall") {
			t.Fatalf("Expected error to reference firewall field, got: %v", err)
		}
	})
}

func TestSandboxAgentFalseExtraction(t *testing.T) {
	t.Run("extractAgentSandboxConfig accepts false", func(t *testing.T) {
		compiler := NewCompiler()

		// Test with false value - should return config with Disabled=true
		agentConfig := compiler.extractAgentSandboxConfig(false)
		if agentConfig == nil {
			t.Fatal("Expected agentConfig to be non-nil for false value")
		}
		if !agentConfig.Disabled {
			t.Error("Expected agentConfig.Disabled to be true for false value")
		}
	})

	t.Run("extractAgentSandboxConfig rejects true (meaningless)", func(t *testing.T) {
		compiler := NewCompiler()

		// Test with true value (should return nil as it's meaningless)
		agentConfig := compiler.extractAgentSandboxConfig(true)
		if agentConfig != nil {
			t.Error("Expected agentConfig to be nil for true value (meaningless)")
		}
	})

	t.Run("extractAgentSandboxConfig handles string", func(t *testing.T) {
		compiler := NewCompiler()

		// Test with "awf" string
		agentConfig := compiler.extractAgentSandboxConfig("awf")
		if agentConfig == nil {
			t.Fatal("Expected agentConfig to be non-nil for 'awf' value")
		}
		if agentConfig.Disabled {
			t.Error("Expected agentConfig.Disabled to be false for 'awf' value")
		}
		if agentConfig.Type != SandboxTypeAWF {
			t.Errorf("Expected agentConfig.Type to be 'awf', got %s", agentConfig.Type)
		}
	})
}
