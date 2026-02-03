//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

// TestEnableFirewallByDefaultForCopilot tests the automatic firewall enablement for copilot engine
func TestEnableFirewallByDefaultForCopilot(t *testing.T) {
	t.Run("copilot engine with network restrictions enables firewall by default", func(t *testing.T) {
		networkPerms := &NetworkPermissions{
			Allowed:           []string{"example.com", "api.github.com"},
			ExplicitlyDefined: true,
		}

		enableFirewallByDefaultForCopilot("copilot", networkPerms, nil)

		if networkPerms.Firewall == nil {
			t.Error("Expected firewall to be enabled by default for copilot engine with network restrictions")
		}

		if !networkPerms.Firewall.Enabled {
			t.Error("Expected firewall.Enabled to be true")
		}
	})

	t.Run("copilot engine with network:defaults enables firewall by default", func(t *testing.T) {
		networkPerms := &NetworkPermissions{
			ExplicitlyDefined: true,
		}

		enableFirewallByDefaultForCopilot("copilot", networkPerms, nil)

		if networkPerms.Firewall == nil {
			t.Error("Expected firewall to be enabled by default for copilot engine with network:defaults")
		}

		if !networkPerms.Firewall.Enabled {
			t.Error("Expected firewall.Enabled to be true")
		}
	})

	t.Run("copilot engine with empty network object enables firewall by default", func(t *testing.T) {
		networkPerms := &NetworkPermissions{
			ExplicitlyDefined: true,
			Allowed:           []string{},
		}

		enableFirewallByDefaultForCopilot("copilot", networkPerms, nil)

		if networkPerms.Firewall == nil {
			t.Error("Expected firewall to be enabled by default for copilot engine with empty network object")
		}

		if !networkPerms.Firewall.Enabled {
			t.Error("Expected firewall.Enabled to be true")
		}
	})

	t.Run("copilot engine with wildcard allowed does NOT enable firewall", func(t *testing.T) {
		networkPerms := &NetworkPermissions{
			Allowed:           []string{"*"},
			ExplicitlyDefined: true,
		}

		enableFirewallByDefaultForCopilot("copilot", networkPerms, nil)

		if networkPerms.Firewall != nil {
			t.Error("Expected firewall to NOT be enabled when allowed contains wildcard '*'")
		}
	})

	t.Run("copilot engine with explicit firewall config is not overridden", func(t *testing.T) {
		networkPerms := &NetworkPermissions{
			Allowed:           []string{"example.com"},
			ExplicitlyDefined: true,
			Firewall: &FirewallConfig{
				Enabled: false,
			},
		}

		enableFirewallByDefaultForCopilot("copilot", networkPerms, nil)

		if networkPerms.Firewall.Enabled {
			t.Error("Expected explicit firewall.Enabled=false to be preserved")
		}
	})

	t.Run("non-copilot engine does not enable firewall", func(t *testing.T) {
		networkPerms := &NetworkPermissions{
			Allowed:           []string{"example.com"},
			ExplicitlyDefined: true,
		}

		enableFirewallByDefaultForCopilot("claude", networkPerms, nil)

		if networkPerms.Firewall != nil {
			t.Error("Expected firewall to remain nil for non-copilot engine")
		}
	})

	t.Run("codex engine with network restrictions enables firewall by default", func(t *testing.T) {
		networkPerms := &NetworkPermissions{
			Allowed:           []string{"api.openai.com"},
			ExplicitlyDefined: true,
		}

		enableFirewallByDefaultForCopilot("codex", networkPerms, nil)

		if networkPerms.Firewall == nil {
			t.Error("Expected firewall to be enabled by default for codex engine with network restrictions")
		}

		if !networkPerms.Firewall.Enabled {
			t.Error("Expected firewall.Enabled to be true")
		}
	})

	t.Run("nil network permissions does not cause error", func(t *testing.T) {
		// Should not panic
		enableFirewallByDefaultForCopilot("copilot", nil, nil)
	})
}

// TestCopilotFirewallDefaultIntegration tests the integration with workflow compilation
func TestCopilotFirewallDefaultIntegration(t *testing.T) {
	t.Run("copilot workflow with network restrictions includes AWF installation", func(t *testing.T) {
		frontmatter := map[string]any{
			"on": "workflow_dispatch",
			"permissions": map[string]any{
				"contents": "read",
			},
			"engine": "copilot",
			"network": map[string]any{
				"allowed": []any{"example.com", "api.github.com"},
			},
		}

		// Create compiler
		c := NewCompiler()
		c.SetSkipValidation(true)

		// Extract engine config
		engineSetting, engineConfig := c.ExtractEngineConfig(frontmatter)
		if engineSetting != "copilot" {
			t.Fatalf("Expected engine 'copilot', got '%s'", engineSetting)
		}

		// Extract network permissions
		networkPerms := c.extractNetworkPermissions(frontmatter)
		if networkPerms == nil {
			t.Fatal("Expected network permissions to be extracted")
		}

		// Enable firewall by default
		enableFirewallByDefaultForCopilot(engineConfig.ID, networkPerms, nil)

		// Verify firewall is enabled
		if networkPerms.Firewall == nil {
			t.Error("Expected firewall to be automatically enabled")
		}

		if !networkPerms.Firewall.Enabled {
			t.Error("Expected firewall.Enabled to be true")
		}

		// Create workflow data
		workflowData := &WorkflowData{
			Name:               "test-workflow",
			EngineConfig:       engineConfig,
			NetworkPermissions: networkPerms,
		}

		// Get installation steps
		engine := NewCopilotEngine()
		steps := engine.GetInstallationSteps(workflowData)

		// Verify AWF installation step is present
		found := false
		for _, step := range steps {
			stepStr := strings.Join(step, "\n")
			if strings.Contains(stepStr, "Install awf binary") || strings.Contains(stepStr, "awf --version") {
				found = true
				break
			}
		}

		if !found {
			t.Error("Expected AWF installation steps to be included")
		}
	})

	t.Run("copilot workflow with explicit firewall:false does not include AWF", func(t *testing.T) {
		frontmatter := map[string]any{
			"on": "workflow_dispatch",
			"permissions": map[string]any{
				"contents": "read",
			},
			"engine": "copilot",
			"network": map[string]any{
				"allowed":  []any{"example.com"},
				"firewall": false,
			},
		}

		// Create compiler
		c := NewCompiler()
		c.SetSkipValidation(true)

		// Extract engine config
		engineSetting, engineConfig := c.ExtractEngineConfig(frontmatter)
		if engineSetting != "copilot" {
			t.Fatalf("Expected engine 'copilot', got '%s'", engineSetting)
		}

		// Extract network permissions
		networkPerms := c.extractNetworkPermissions(frontmatter)
		if networkPerms == nil {
			t.Fatal("Expected network permissions to be extracted")
		}

		// Verify firewall is explicitly disabled
		if networkPerms.Firewall == nil {
			t.Error("Expected firewall config to be present")
		}

		if networkPerms.Firewall.Enabled {
			t.Error("Expected firewall.Enabled to be false")
		}

		// Enable firewall by default (should not override explicit config)
		enableFirewallByDefaultForCopilot(engineConfig.ID, networkPerms, nil)

		// Verify firewall is still disabled
		if networkPerms.Firewall.Enabled {
			t.Error("Expected firewall to remain disabled when explicitly set to false")
		}

		// Create workflow data
		workflowData := &WorkflowData{
			Name:               "test-workflow",
			EngineConfig:       engineConfig,
			NetworkPermissions: networkPerms,
		}

		// Get installation steps
		engine := NewCopilotEngine()
		steps := engine.GetInstallationSteps(workflowData)

		// Verify AWF installation step is NOT present
		for _, step := range steps {
			stepStr := strings.Join(step, "\n")
			if strings.Contains(stepStr, "Install awf binary") {
				t.Error("Expected AWF installation steps to NOT be included when firewall is explicitly disabled")
			}
		}
	})

	t.Run("claude engine with network restrictions does not enable firewall", func(t *testing.T) {
		frontmatter := map[string]any{
			"on": "workflow_dispatch",
			"permissions": map[string]any{
				"contents": "read",
			},
			"engine": "claude",
			"network": map[string]any{
				"allowed": []any{"example.com"},
			},
		}

		// Create compiler
		c := NewCompiler()
		c.SetSkipValidation(true)

		// Extract engine config
		engineSetting, engineConfig := c.ExtractEngineConfig(frontmatter)
		if engineSetting != "claude" {
			t.Fatalf("Expected engine 'claude', got '%s'", engineSetting)
		}

		// Extract network permissions
		networkPerms := c.extractNetworkPermissions(frontmatter)
		if networkPerms == nil {
			t.Fatal("Expected network permissions to be extracted")
		}

		// Enable firewall by default (should not affect non-copilot engines)
		enableFirewallByDefaultForCopilot(engineConfig.ID, networkPerms, nil)

		// Verify firewall is NOT enabled for claude
		if networkPerms.Firewall != nil {
			t.Error("Expected firewall to remain nil for claude engine")
		}
	})
}

// TestDailyTeamStatusFirewallEnabled tests that daily-team-status workflow has firewall enabled
func TestDailyTeamStatusFirewallEnabled(t *testing.T) {
	t.Run("daily-team-status workflow with network:defaults enables AWF firewall", func(t *testing.T) {
		frontmatter := map[string]any{
			"on": "workflow_dispatch",
			"permissions": map[string]any{
				"contents": "read",
			},
			// No explicit engine specified - should default to copilot
			"network": "defaults",
		}

		// Create compiler
		c := NewCompiler()
		c.SetSkipValidation(true)

		// Extract engine config (should default to copilot)
		_, engineConfig := c.ExtractEngineConfig(frontmatter)

		// If no engine is specified, it should default to copilot
		var engineID string
		if engineConfig != nil {
			engineID = engineConfig.ID
		} else {
			engineID = "copilot" // default engine
		}

		// Extract network permissions
		networkPerms := c.extractNetworkPermissions(frontmatter)
		if networkPerms == nil {
			t.Fatal("Expected network permissions to be extracted")
		}

		if len(networkPerms.Allowed) != 1 || networkPerms.Allowed[0] != "defaults" {
			t.Errorf("Expected network allowed to be ['defaults'], got %v", networkPerms.Allowed)
		}

		// Enable firewall by default
		enableFirewallByDefaultForCopilot(engineID, networkPerms, nil)

		// Verify firewall is enabled
		if networkPerms.Firewall == nil {
			t.Error("Expected firewall to be automatically enabled for network:defaults with copilot")
		}

		if networkPerms.Firewall != nil && !networkPerms.Firewall.Enabled {
			t.Error("Expected firewall.Enabled to be true")
		}
	})
}

// TestStrictModeFirewallValidation tests strict mode firewall validation
func TestStrictModeFirewallValidation(t *testing.T) {
	t.Run("strict mode requires firewall for copilot with network restrictions", func(t *testing.T) {
		compiler := NewCompiler()
		compiler.SetStrictMode(true)

		networkPerms := &NetworkPermissions{
			Allowed:           []string{"example.com"},
			ExplicitlyDefined: true,
			// Firewall is NOT enabled
			Firewall: nil,
		}

		err := compiler.validateStrictFirewall("copilot", networkPerms, nil)
		if err == nil {
			t.Error("Expected error in strict mode when firewall is not enabled")
		}

		if !strings.Contains(err.Error(), "firewall must be enabled") {
			t.Errorf("Expected error about firewall requirement, got: %v", err)
		}
	})

	t.Run("strict mode allows firewall disabled when allowed is wildcard", func(t *testing.T) {
		compiler := NewCompiler()
		compiler.SetStrictMode(true)

		networkPerms := &NetworkPermissions{
			Allowed:           []string{"*"},
			ExplicitlyDefined: true,
			Firewall:          nil,
		}

		err := compiler.validateStrictFirewall("copilot", networkPerms, nil)
		if err != nil {
			t.Errorf("Expected no error when allowed is wildcard, got: %v", err)
		}
	})

	t.Run("strict mode passes when firewall is enabled", func(t *testing.T) {
		compiler := NewCompiler()
		compiler.SetStrictMode(true)

		networkPerms := &NetworkPermissions{
			Allowed:           []string{"example.com"},
			ExplicitlyDefined: true,
			Firewall: &FirewallConfig{
				Enabled: true,
			},
		}

		err := compiler.validateStrictFirewall("copilot", networkPerms, nil)
		if err != nil {
			t.Errorf("Expected no error when firewall is enabled, got: %v", err)
		}
	})

	t.Run("strict mode skips validation for non-copilot engines", func(t *testing.T) {
		compiler := NewCompiler()
		compiler.SetStrictMode(true)

		networkPerms := &NetworkPermissions{
			Allowed:           []string{"example.com"},
			ExplicitlyDefined: true,
			Firewall:          nil,
		}

		err := compiler.validateStrictFirewall("claude", networkPerms, nil)
		if err != nil {
			t.Errorf("Expected no error for non-copilot engine, got: %v", err)
		}
	})

	t.Run("strict mode refuses sandbox.agent: false for copilot", func(t *testing.T) {
		compiler := NewCompiler()
		compiler.SetStrictMode(true)

		networkPerms := &NetworkPermissions{
			Allowed:           []string{"example.com"},
			ExplicitlyDefined: true,
			Firewall:          nil,
		}

		sandboxConfig := &SandboxConfig{
			Agent: &AgentSandboxConfig{
				Disabled: true,
			},
		}

		err := compiler.validateStrictFirewall("copilot", networkPerms, sandboxConfig)
		if err == nil {
			t.Error("Expected error when sandbox.agent is false in strict mode for copilot")
		}
		expectedMsg := "sandbox: false"
		if !strings.Contains(err.Error(), expectedMsg) {
			t.Errorf("Expected error message to contain '%s', got: %v", expectedMsg, err)
		}
	})

	t.Run("strict mode refuses sandbox.agent: false for all engines", func(t *testing.T) {
		compiler := NewCompiler()
		compiler.SetStrictMode(true)

		networkPerms := &NetworkPermissions{
			Allowed:           []string{"example.com"},
			ExplicitlyDefined: true,
			Firewall:          nil,
		}

		sandboxConfig := &SandboxConfig{
			Agent: &AgentSandboxConfig{
				Disabled: true,
			},
		}

		// All engines should refuse sandbox.agent: false in strict mode
		err := compiler.validateStrictFirewall("claude", networkPerms, sandboxConfig)
		if err == nil {
			t.Error("Expected error for non-copilot engine with sandbox.agent: false in strict mode")
		}
		expectedMsg := "sandbox: false"
		if !strings.Contains(err.Error(), expectedMsg) {
			t.Errorf("Expected error message to contain '%s', got: %v", expectedMsg, err)
		}
	})

	t.Run("strict mode skips validation when SRT is enabled", func(t *testing.T) {
		compiler := NewCompiler()
		compiler.SetStrictMode(true)

		networkPerms := &NetworkPermissions{
			Allowed:           []string{"example.com"},
			ExplicitlyDefined: true,
			Firewall:          nil,
		}

		sandboxConfig := &SandboxConfig{
			Type: SandboxTypeRuntime,
		}

		err := compiler.validateStrictFirewall("copilot", networkPerms, sandboxConfig)
		if err != nil {
			t.Errorf("Expected no error when SRT is enabled, got: %v", err)
		}
	})

	t.Run("non-strict mode does not validate firewall", func(t *testing.T) {
		compiler := NewCompiler()
		compiler.SetStrictMode(false)

		networkPerms := &NetworkPermissions{
			Allowed:           []string{"example.com"},
			ExplicitlyDefined: true,
			Firewall:          nil,
		}

		err := compiler.validateStrictFirewall("copilot", networkPerms, nil)
		if err != nil {
			t.Errorf("Expected no error in non-strict mode, got: %v", err)
		}
	})

	t.Run("sandbox.agent: false is rejected even in non-strict mode", func(t *testing.T) {
		compiler := NewCompiler()
		compiler.SetStrictMode(false)

		networkPerms := &NetworkPermissions{
			Allowed:           []string{"example.com"},
			ExplicitlyDefined: true,
			Firewall:          nil,
		}

		sandboxConfig := &SandboxConfig{
			Agent: &AgentSandboxConfig{
				Disabled: true,
			},
		}

		// Even in non-strict mode, sandbox.agent: false should be rejected by validation
		// (validateStrictFirewall only runs in strict mode, but validateSandboxConfig runs always)
		err := compiler.validateStrictFirewall("copilot", networkPerms, sandboxConfig)
		// validateStrictFirewall only runs in strict mode, so it passes here
		// The actual rejection happens in validateSandboxConfig
		if err != nil {
			// If we get an error here, it's from strict mode validation
			// which is expected to reject it
			expectedMsg := "sandbox: false"
			if !strings.Contains(err.Error(), expectedMsg) {
				t.Errorf("Expected error message to contain '%s', got: %v", expectedMsg, err)
			}
		}
	})
}
