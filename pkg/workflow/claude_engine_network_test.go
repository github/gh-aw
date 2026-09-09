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
			Model: "claude-3-5-sonnet-20241022",
			EngineConfig: &EngineConfig{
				ID: "claude",
			},
		}

		steps := engine.GetInstallationSteps(workflowData)
		// Secret validation is now in the activation job; installation has Node.js setup + install = 2 steps
		if len(steps) != 2 {
			t.Errorf("Expected 2 installation steps without network permissions (Node.js setup + install), got %d", len(steps))
		}
	})

	t.Run("InstallationSteps with network permissions and firewall enabled", func(t *testing.T) {
		workflowData := &WorkflowData{
			Model: "claude-3-5-sonnet-20241022",
			EngineConfig: &EngineConfig{
				ID: "claude",
			},
			NetworkPermissions: &NetworkPermissions{
				Allowed:  []string{"example.com", "*.trusted.com"},
				Firewall: &FirewallConfig{Enabled: true},
			},
		}

		steps := engine.GetInstallationSteps(workflowData)
		// With AWF enabled: Node.js setup + AWF install + Claude install = 3 steps
		// (secret validation is now in the activation job)
		if len(steps) != 3 {
			t.Errorf("Expected 3 installation steps with network permissions and AWF (Node.js setup + AWF install + Claude install), got %d", len(steps))
		}

		// Check AWF installation step (2nd step, index 1)
		awfStepStr := strings.Join(steps[1], "\n")
		if !strings.Contains(awfStepStr, "Install AWF binary") {
			t.Error("Second step should install AWF binary")
		}
	})

	t.Run("ExecutionSteps without network permissions", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name:  "test-workflow",
			Model: "claude-3-5-sonnet-20241022",
			EngineConfig: &EngineConfig{
				ID: "claude",
			},
		}

		steps := engine.GetExecutionSteps(workflowData, "test-log")
		if len(steps) == 0 {
			t.Fatal("Expected at least one execution step")
		}

		// Convert steps to string for analysis
		stepYAML := strings.Join(steps[0], "\n")

		// Verify AWF is not used without network permissions
		if strings.Contains(stepYAML, "sudo -E ") {
			t.Error("AWF should not be used without network permissions")
		}

		// Verify model is passed via ANTHROPIC_MODEL env var (not as --model flag)
		if !strings.Contains(stepYAML, "ANTHROPIC_MODEL: claude-3-5-sonnet-20241022") {
			t.Error("Expected ANTHROPIC_MODEL env var for model 'claude-3-5-sonnet-20241022' in step YAML")
		}
	})

	t.Run("ExecutionSteps with network permissions and firewall enabled", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name:  "test-workflow",
			Model: "claude-3-5-sonnet-20241022",
			EngineConfig: &EngineConfig{
				ID: "claude",
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
		if !strings.Contains(stepYAML, "awf") {
			t.Error("AWF should be used with network permissions")
		}

		// Verify --tty flag is present (required for Claude)
		if !strings.Contains(stepYAML, "--tty") {
			t.Error("--tty flag should be present for Claude with AWF")
		}

		if !strings.Contains(stepYAML, "--debug-file /tmp/gh-aw/claude-debug.log") {
			t.Error("Claude debug output should use a file separate from the stream-json transcript")
		}

		if !strings.Contains(stepYAML, "(umask 177 && touch /tmp/gh-aw/claude-debug.log)") {
			t.Error("Claude debug log should be created with restrictive permissions before AWF starts")
		}

		// Verify domains are in the AWF config JSON (not as --allow-domains CLI flag)
		if !strings.Contains(stepYAML, "allowDomains") {
			t.Error("allowDomains should be present in AWF config JSON")
		}

		// Verify model is passed via ANTHROPIC_MODEL env var (not as --model flag)
		if !strings.Contains(stepYAML, "ANTHROPIC_MODEL: claude-3-5-sonnet-20241022") {
			t.Error("Expected ANTHROPIC_MODEL env var for model 'claude-3-5-sonnet-20241022' in step YAML")
		}
	})

	t.Run("ExecutionSteps with empty allowed domains and firewall enabled", func(t *testing.T) {
		config := &EngineConfig{
			ID: "claude",
		}

		networkPermissions := &NetworkPermissions{
			Allowed:  []string{}, // Empty list means deny all
			Firewall: &FirewallConfig{Enabled: true},
		}

		steps := engine.GetExecutionSteps(&WorkflowData{Name: "test-workflow", Model: "claude-3-5-sonnet-20241022", EngineConfig: config, NetworkPermissions: networkPermissions}, "test-log")
		if len(steps) == 0 {
			t.Fatal("Expected at least one execution step")
		}

		// Convert steps to string for analysis
		stepYAML := strings.Join(steps[0], "\n")

		// Verify AWF is used even with deny-all policy
		if !strings.Contains(stepYAML, "awf") {
			t.Error("AWF should be used even with deny-all network permissions")
		}
	})

	t.Run("ExecutionSteps with non-Claude engine ID in config", func(t *testing.T) {
		// Note: This test uses Claude engine but with non-Claude engine config ID
		// The behavior should still be based on the actual engine type, not the config ID
		config := &EngineConfig{
			ID: "codex", // Non-Claude engine ID
		}

		networkPermissions := &NetworkPermissions{
			Allowed:  []string{"example.com"},
			Firewall: &FirewallConfig{Enabled: true},
		}

		steps := engine.GetExecutionSteps(&WorkflowData{Name: "test-workflow", Model: "gpt-4", EngineConfig: config, NetworkPermissions: networkPermissions}, "test-log")
		if len(steps) == 0 {
			t.Fatal("Expected at least one execution step")
		}

		// The Claude engine will still generate AWF-wrapped command since it's the Claude engine
		// Convert steps to string for analysis
		stepYAML := strings.Join(steps[0], "\n")

		// AWF should be present because the engine is Claude (not based on config ID)
		if !strings.Contains(stepYAML, "awf") {
			t.Error("AWF should be used because the engine type is Claude")
		}
	})
}

func TestNetworkPermissionsIntegration(t *testing.T) {
	t.Run("Full workflow generation with AWF", func(t *testing.T) {
		engine := NewClaudeEngine()
		config := &EngineConfig{
			ID: "claude",
		}

		networkPermissions := &NetworkPermissions{
			Allowed:  []string{"api.github.com", "*.example.com", "trusted.org"},
			Firewall: &FirewallConfig{Enabled: true},
		}

		// Get installation steps
		steps := engine.GetInstallationSteps(&WorkflowData{Model: "claude-3-5-sonnet-20241022", EngineConfig: config, NetworkPermissions: networkPermissions})
		// With AWF enabled: Node.js setup + AWF install + Claude install = 3 steps
		// (secret validation is now in the activation job)
		if len(steps) != 3 {
			t.Fatalf("Expected 3 installation steps (Node.js setup + AWF install + Claude install), got %d", len(steps))
		}

		// Verify AWF installation step (second step, index 1)
		awfStep := strings.Join(steps[1], "\n")
		if !strings.Contains(awfStep, "Install AWF binary") {
			t.Error("Second step should install AWF binary")
		}

		// Get execution steps
		execSteps := engine.GetExecutionSteps(&WorkflowData{Name: "test-workflow", Model: "claude-3-5-sonnet-20241022", EngineConfig: config, NetworkPermissions: networkPermissions}, "test-log")
		if len(execSteps) == 0 {
			t.Fatal("Expected at least one execution step")
		}

		// Convert steps to string for analysis
		stepYAML := strings.Join(execSteps[0], "\n")

		// Verify AWF is configured
		if !strings.Contains(stepYAML, "awf") {
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
			ID: "claude",
		}

		networkPermissions := &NetworkPermissions{
			Allowed:  []string{"example.com"},
			Firewall: &FirewallConfig{Enabled: true},
		}

		steps1 := engine1.GetInstallationSteps(&WorkflowData{Model: "claude-3-5-sonnet-20241022", EngineConfig: config, NetworkPermissions: networkPermissions})
		steps2 := engine2.GetInstallationSteps(&WorkflowData{Model: "claude-3-5-sonnet-20241022", EngineConfig: config, NetworkPermissions: networkPermissions})

		if len(steps1) != len(steps2) {
			t.Errorf("Engine instances should produce same number of steps, got %d and %d", len(steps1), len(steps2))
		}

		execSteps1 := engine1.GetExecutionSteps(&WorkflowData{Name: "test", Model: "claude-3-5-sonnet-20241022", EngineConfig: config, NetworkPermissions: networkPermissions}, "log")
		execSteps2 := engine2.GetExecutionSteps(&WorkflowData{Name: "test", Model: "claude-3-5-sonnet-20241022", EngineConfig: config, NetworkPermissions: networkPermissions}, "log")

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
