//go:build integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSandboxAgentMandatory(t *testing.T) {
	t.Run("sandbox.agent: false is rejected", func(t *testing.T) {
		// Create temp directory for test workflows
		workflowsDir := t.TempDir()

		markdown := `---
engine: copilot
network:
  allowed:
    - defaults
    - github.com
sandbox:
  agent: false
on: workflow_dispatch
---

Test workflow to verify sandbox.agent: false is rejected.
`

		workflowPath := filepath.Join(workflowsDir, "test-agent-false.md")
		err := os.WriteFile(workflowPath, []byte(markdown), 0644)
		if err != nil {
			t.Fatalf("Failed to write workflow file: %v", err)
		}

		// Compile the workflow
		compiler := NewCompiler()

		// Should fail due to schema validation error
		if err := compiler.CompileWorkflow(workflowPath); err == nil {
			t.Fatal("Expected compilation to fail with sandbox.agent: false, but it succeeded")
		} else {
			// Verify error message mentions that boolean is not allowed
			if !strings.Contains(err.Error(), "got boolean") || !strings.Contains(err.Error(), "/sandbox/agent") {
				t.Errorf("Expected error message to mention boolean value not allowed for sandbox.agent, got: %v", err)
			}
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

		// Verify that AWF installation IS present
		if !strings.Contains(lockStr, "sudo -E awf") {
			t.Error("Expected AWF firewall to be enabled, but did not find 'sudo -E awf' command in lock file")
		}
	})

	t.Run("default sandbox enables firewall (awf)", func(t *testing.T) {
		// Create temp directory for test workflows
		workflowsDir := t.TempDir()

		markdown := `---
engine: copilot
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
		if !strings.Contains(lockStr, "sudo -E awf") {
			t.Error("Expected firewall to be enabled by default with network restrictions, but did not find 'sudo -E awf' command in lock file")
		}
	})
}

func TestNetworkFirewallDeprecationWarning(t *testing.T) {
	t.Run("network.firewall compiles successfully (deprecated)", func(t *testing.T) {
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

Test workflow to verify network.firewall still works (deprecated).
`

		workflowPath := filepath.Join(workflowsDir, "test-firewall-deprecated.md")
		err := os.WriteFile(workflowPath, []byte(markdown), 0644)
		if err != nil {
			t.Fatalf("Failed to write workflow file: %v", err)
		}

		// Compile the workflow
		compiler := NewCompiler()
		compiler.SetSkipValidation(true)

		// The compilation should succeed (deprecated fields should still work)
		if err := compiler.CompileWorkflow(workflowPath); err != nil {
			t.Fatalf("Compilation failed: %v", err)
		}
	})
}

func TestSandboxAgentFalseExtraction(t *testing.T) {
	t.Run("extractAgentSandboxConfig rejects false", func(t *testing.T) {
		compiler := NewCompiler()

		// Test with false value - should return nil now (invalid)
		agentConfig := compiler.extractAgentSandboxConfig(false)
		if agentConfig != nil {
			t.Error("Expected agentConfig to be nil for false value (no longer supported)")
		}
	})

	t.Run("extractAgentSandboxConfig rejects true (invalid)", func(t *testing.T) {
		compiler := NewCompiler()

		// Test with true value (should be invalid)
		agentConfig := compiler.extractAgentSandboxConfig(true)
		if agentConfig != nil {
			t.Error("Expected agentConfig to be nil for true value (invalid)")
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
