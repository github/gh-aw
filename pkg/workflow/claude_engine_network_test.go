//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

func TestClaudeEngineNetworkPermissions(t *testing.T) {
	engine := NewClaudeEngine()

	t.Run("InstallationSteps without network permissions", func(t *testing.T) {
		workflowData := &WorkflowData{
			EngineConfig: &EngineConfig{
				ID:    "claude",
				Model: "claude-3-5-sonnet-20241022",
			},
		}

		steps := engine.GetInstallationSteps(workflowData)
		if len(steps) != 3 {
			t.Errorf("Expected 3 installation steps without network permissions (secret validation + Node.js setup + install), got %d", len(steps))
		}
	})

	t.Run("InstallationSteps with network permissions and firewall enabled", func(t *testing.T) {
		workflowData := &WorkflowData{
			EngineConfig: &EngineConfig{
				ID:    "claude",
				Model: "claude-3-5-sonnet-20241022",
			},
			NetworkPermissions: &NetworkPermissions{
				Allowed:  []string{"example.com", "*.trusted.com"},
				Firewall: &FirewallConfig{Enabled: true},
			},
		}

		steps := engine.GetInstallationSteps(workflowData)
		// With AWF enabled: secret validation + Node.js setup + AWF install + Claude install
		if len(steps) != 4 {
			t.Errorf("Expected 4 installation steps with network permissions and AWF (secret validation + Node.js setup + AWF install + Claude install), got %d", len(steps))
		}

		// Check AWF installation step (3rd step, index 2)
		awfStepStr := strings.Join(steps[2], "\n")
		if !strings.Contains(awfStepStr, "Install awf binary") {
			t.Error("Third step should install AWF binary")
		}
	})

	t.Run("ExecutionSteps without network permissions", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
			EngineConfig: &EngineConfig{
				ID:    "claude",
				Model: "claude-3-5-sonnet-20241022",
			},
		}

		steps := engine.GetExecutionSteps(workflowData, "test-log")
		if len(steps) == 0 {
			t.Fatal("Expected at least one execution step")
		}

		// Convert steps to string for analysis
		stepYAML := strings.Join(steps[0], "\n")

		// Verify AWF is not used without network permissions
		if strings.Contains(stepYAML, "sudo -E awf") {
			t.Error("AWF should not be used without network permissions")
		}

		// Verify model parameter is present
		if !strings.Contains(stepYAML, "--model claude-3-5-sonnet-20241022") {
			t.Error("Expected model 'claude-3-5-sonnet-20241022' in step YAML")
		}
	})

	t.Run("ExecutionSteps with network permissions and firewall enabled", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
			EngineConfig: &EngineConfig{
				ID:    "claude",
				Model: "claude-3-5-sonnet-20241022",
			},
			NetworkPermissions: &NetworkPermissions{
				Allowed:  []string{"example.com"},
				Firewall: &FirewallConfig{Enabled: true},
			},
		}

		steps := engine.GetExecutionSteps(workflowData, "test-log")
		if len(steps) == 0 {
			t.Fatal("Expected at least one execution step")
		}

		// Convert steps to string for analysis
		stepYAML := strings.Join(steps[0], "\n")

		// Verify AWF is used
		if !strings.Contains(stepYAML, "sudo -E awf") {
			t.Error("AWF should be used with network permissions")
		}

		// Verify --tty flag is present (required for Claude)
		if !strings.Contains(stepYAML, "--tty") {
			t.Error("--tty flag should be present for Claude with AWF")
		}

		// Verify --allow-domains is present
		if !strings.Contains(stepYAML, "--allow-domains") {
			t.Error("--allow-domains should be present with AWF")
		}

		// Verify model parameter is present
		if !strings.Contains(stepYAML, "--model claude-3-5-sonnet-20241022") {
			t.Error("Expected model 'claude-3-5-sonnet-20241022' in step YAML")
		}
	})

	t.Run("ExecutionSteps with empty allowed domains and firewall enabled", func(t *testing.T) {
		config := &EngineConfig{
			ID:    "claude",
			Model: "claude-3-5-sonnet-20241022",
		}

		networkPermissions := &NetworkPermissions{
			Allowed:  []string{}, // Empty list means deny all
			Firewall: &FirewallConfig{Enabled: true},
		}

		steps := engine.GetExecutionSteps(&WorkflowData{Name: "test-workflow", EngineConfig: config, NetworkPermissions: networkPermissions}, "test-log")
		if len(steps) == 0 {
			t.Fatal("Expected at least one execution step")
		}

		// Convert steps to string for analysis
		stepYAML := strings.Join(steps[0], "\n")

		// Verify AWF is used even with deny-all policy
		if !strings.Contains(stepYAML, "sudo -E awf") {
			t.Error("AWF should be used even with deny-all network permissions")
		}
	})

	t.Run("ExecutionSteps with non-Claude engine ID in config", func(t *testing.T) {
		// Note: This test uses Claude engine but with non-Claude engine config ID
		// The behavior should still be based on the actual engine type, not the config ID
		config := &EngineConfig{
			ID:    "codex", // Non-Claude engine ID
			Model: "gpt-4",
		}

		networkPermissions := &NetworkPermissions{
			Allowed:  []string{"example.com"},
			Firewall: &FirewallConfig{Enabled: true},
		}

		steps := engine.GetExecutionSteps(&WorkflowData{Name: "test-workflow", EngineConfig: config, NetworkPermissions: networkPermissions}, "test-log")
		if len(steps) == 0 {
			t.Fatal("Expected at least one execution step")
		}

		// The Claude engine will still generate AWF-wrapped command since it's the Claude engine
		// Convert steps to string for analysis
		stepYAML := strings.Join(steps[0], "\n")

		// AWF should be present because the engine is Claude (not based on config ID)
		if !strings.Contains(stepYAML, "sudo -E awf") {
			t.Error("AWF should be used because the engine type is Claude")
		}
	})
}

func TestNetworkPermissionsIntegration(t *testing.T) {
	t.Run("Full workflow generation with AWF", func(t *testing.T) {
		engine := NewClaudeEngine()
		config := &EngineConfig{
			ID:    "claude",
			Model: "claude-3-5-sonnet-20241022",
		}

		networkPermissions := &NetworkPermissions{
			Allowed:  []string{"api.github.com", "*.example.com", "trusted.org"},
			Firewall: &FirewallConfig{Enabled: true},
		}

		// Get installation steps
		steps := engine.GetInstallationSteps(&WorkflowData{EngineConfig: config, NetworkPermissions: networkPermissions})
		// With AWF enabled: secret validation + Node.js setup + AWF install + Claude install
		if len(steps) != 4 {
			t.Fatalf("Expected 4 installation steps (secret validation + Node.js setup + AWF install + Claude install), got %d", len(steps))
		}

		// Verify AWF installation step (third step, index 2)
		awfStep := strings.Join(steps[2], "\n")
		if !strings.Contains(awfStep, "Install awf binary") {
			t.Error("Third step should install AWF binary")
		}

		// Get execution steps
		execSteps := engine.GetExecutionSteps(&WorkflowData{Name: "test-workflow", EngineConfig: config, NetworkPermissions: networkPermissions}, "test-log")
		if len(execSteps) == 0 {
			t.Fatal("Expected at least one execution step")
		}

		// Convert steps to string for analysis
		stepYAML := strings.Join(execSteps[0], "\n")

		// Verify AWF is configured
		if !strings.Contains(stepYAML, "sudo -E awf") {
			t.Error("AWF should be present")
		}

		// Verify --tty flag is present
		if !strings.Contains(stepYAML, "--tty") {
			t.Error("--tty flag should be present for Claude with AWF")
		}

		// Test the GetAllowedDomains function - domains should be sorted
		domains := GetAllowedDomains(networkPermissions)
		if len(domains) != 3 {
			t.Fatalf("Expected 3 allowed domains, got %d", len(domains))
		}

		// Domains should be sorted alphabetically
		expectedDomainsList := []string{"*.example.com", "api.github.com", "trusted.org"}
		for i, expected := range expectedDomainsList {
			if domains[i] != expected {
				t.Errorf("Expected domain %d to be '%s', got '%s'", i, expected, domains[i])
			}
		}
	})

	t.Run("Engine consistency", func(t *testing.T) {
		engine1 := NewClaudeEngine()
		engine2 := NewClaudeEngine()

		config := &EngineConfig{
			ID:    "claude",
			Model: "claude-3-5-sonnet-20241022",
		}

		networkPermissions := &NetworkPermissions{
			Allowed:  []string{"example.com"},
			Firewall: &FirewallConfig{Enabled: true},
		}

		steps1 := engine1.GetInstallationSteps(&WorkflowData{EngineConfig: config, NetworkPermissions: networkPermissions})
		steps2 := engine2.GetInstallationSteps(&WorkflowData{EngineConfig: config, NetworkPermissions: networkPermissions})

		if len(steps1) != len(steps2) {
			t.Errorf("Engine instances should produce same number of steps, got %d and %d", len(steps1), len(steps2))
		}

		execSteps1 := engine1.GetExecutionSteps(&WorkflowData{Name: "test", EngineConfig: config, NetworkPermissions: networkPermissions}, "log")
		execSteps2 := engine2.GetExecutionSteps(&WorkflowData{Name: "test", EngineConfig: config, NetworkPermissions: networkPermissions}, "log")

		if len(execSteps1) != len(execSteps2) {
			t.Errorf("Engine instances should produce same number of execution steps, got %d and %d", len(execSteps1), len(execSteps2))
		}

		// Compare the first execution step if they exist
		if len(execSteps1) > 0 && len(execSteps2) > 0 {
			step1YAML := strings.Join(execSteps1[0], "\n")
			step2YAML := strings.Join(execSteps2[0], "\n")
			if step1YAML != step2YAML {
				t.Error("Engine instances should produce identical execution steps")
			}
		}
	})
}
