//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

// TestValidateStrictFirewall_LLMGatewaySupport tests the LLM gateway validation in strict mode
func TestValidateStrictFirewall_LLMGatewaySupport(t *testing.T) {
	t.Run("codex engine with LLM gateway support also rejects custom domains in strict mode", func(t *testing.T) {
		compiler := NewCompiler()
		compiler.strictMode = true

		networkPerms := &NetworkPermissions{
			Allowed: []string{"custom-domain.com", "another-custom.com"},
			Firewall: &FirewallConfig{
				Enabled: true,
			},
		}

		err := compiler.validateStrictFirewall("codex", networkPerms, nil)
		if err == nil {
			t.Error("Expected error for codex engine with custom domains in strict mode, got nil")
		}
		if err != nil && !strings.Contains(err.Error(), "network domains must be from known ecosystems") {
			t.Errorf("Expected error about known ecosystems, got: %v", err)
		}
	})

	t.Run("copilot engine without LLM gateway support rejects custom domains", func(t *testing.T) {
		compiler := NewCompiler()
		compiler.strictMode = true

		networkPerms := &NetworkPermissions{
			Allowed: []string{"custom-domain.com"},
			Firewall: &FirewallConfig{
				Enabled: true,
			},
		}

		err := compiler.validateStrictFirewall("copilot", networkPerms, nil)
		if err == nil {
			t.Error("Expected error for copilot engine with custom domains, got nil")
		}
		if err != nil && !strings.Contains(err.Error(), "network domains must be from known ecosystems") {
			t.Errorf("Expected error about known ecosystems, got: %v", err)
		}
	})

	t.Run("copilot engine without LLM gateway support allows defaults", func(t *testing.T) {
		compiler := NewCompiler()
		compiler.strictMode = true

		networkPerms := &NetworkPermissions{
			Allowed: []string{"defaults"},
			Firewall: &FirewallConfig{
				Enabled: true,
			},
		}

		err := compiler.validateStrictFirewall("copilot", networkPerms, nil)
		if err != nil {
			t.Errorf("Expected no error for copilot engine with 'defaults', got: %v", err)
		}
	})

	t.Run("copilot engine without LLM gateway support allows known ecosystems", func(t *testing.T) {
		compiler := NewCompiler()
		compiler.strictMode = true

		networkPerms := &NetworkPermissions{
			Allowed: []string{"python", "node", "github"},
			Firewall: &FirewallConfig{
				Enabled: true,
			},
		}

		err := compiler.validateStrictFirewall("copilot", networkPerms, nil)
		if err != nil {
			t.Errorf("Expected no error for copilot engine with known ecosystem identifiers, got: %v", err)
		}
	})

	t.Run("copilot engine without LLM gateway support allows domains from known ecosystems", func(t *testing.T) {
		compiler := NewCompiler()
		compiler.strictMode = true

		// These domains are from known ecosystems (python, node)
		networkPerms := &NetworkPermissions{
			Allowed: []string{"pypi.org", "registry.npmjs.org"},
			Firewall: &FirewallConfig{
				Enabled: true,
			},
		}

		err := compiler.validateStrictFirewall("copilot", networkPerms, nil)
		if err != nil {
			t.Errorf("Expected no error for copilot engine with known ecosystem domains, got: %v", err)
		}
	})

	t.Run("codex engine with LLM gateway also allows known ecosystems", func(t *testing.T) {
		compiler := NewCompiler()
		compiler.strictMode = true

		networkPerms := &NetworkPermissions{
			Allowed: []string{"python", "node", "github"},
			Firewall: &FirewallConfig{
				Enabled: true,
			},
		}

		err := compiler.validateStrictFirewall("codex", networkPerms, nil)
		if err != nil {
			t.Errorf("Expected no error for codex engine with known ecosystem identifiers, got: %v", err)
		}
	})

	t.Run("codex engine with LLM gateway allows domains from known ecosystems", func(t *testing.T) {
		compiler := NewCompiler()
		compiler.strictMode = true

		// These domains are from known ecosystems (python, node)
		networkPerms := &NetworkPermissions{
			Allowed: []string{"pypi.org", "registry.npmjs.org"},
			Firewall: &FirewallConfig{
				Enabled: true,
			},
		}

		err := compiler.validateStrictFirewall("codex", networkPerms, nil)
		if err != nil {
			t.Errorf("Expected no error for codex engine with known ecosystem domains, got: %v", err)
		}
	})

	t.Run("copilot engine without LLM gateway support rejects mixed ecosystems and custom domains", func(t *testing.T) {
		compiler := NewCompiler()
		compiler.strictMode = true

		networkPerms := &NetworkPermissions{
			Allowed: []string{"python", "custom-domain.com"},
			Firewall: &FirewallConfig{
				Enabled: true,
			},
		}

		err := compiler.validateStrictFirewall("copilot", networkPerms, nil)
		if err == nil {
			t.Error("Expected error for copilot engine with mixed ecosystems and custom domains, got nil")
		}
		if err != nil && !strings.Contains(err.Error(), "network domains must be from known ecosystems") {
			t.Errorf("Expected error about known ecosystems, got: %v", err)
		}
	})

	t.Run("claude engine without LLM gateway support rejects custom domains", func(t *testing.T) {
		compiler := NewCompiler()
		compiler.strictMode = true

		networkPerms := &NetworkPermissions{
			Allowed: []string{"custom-domain.com"},
			Firewall: &FirewallConfig{
				Enabled: true,
			},
		}

		err := compiler.validateStrictFirewall("claude", networkPerms, nil)
		if err == nil {
			t.Error("Expected error for claude engine with custom domains, got nil")
		}
		if err != nil && !strings.Contains(err.Error(), "network domains must be from known ecosystems") {
			t.Errorf("Expected error about known ecosystems, got: %v", err)
		}
	})

	t.Run("copilot engine without LLM gateway requires sandbox.agent to be enabled", func(t *testing.T) {
		compiler := NewCompiler()
		compiler.strictMode = true

		networkPerms := &NetworkPermissions{
			Allowed: []string{"defaults"},
		}

		sandboxConfig := &SandboxConfig{
			Agent: &AgentSandboxConfig{
				Disabled: true,
			},
		}

		err := compiler.validateStrictFirewall("copilot", networkPerms, sandboxConfig)
		if err == nil {
			t.Error("Expected error for copilot engine with sandbox.agent: false, got nil")
		}
		if err != nil && !strings.Contains(err.Error(), "does not support LLM gateway") {
			t.Errorf("Expected error about LLM gateway support, got: %v", err)
		}
		if err != nil && !strings.Contains(err.Error(), "sandbox.agent") {
			t.Errorf("Expected error about sandbox.agent, got: %v", err)
		}
	})

	t.Run("codex engine with LLM gateway rejects sandbox.agent: false in strict mode", func(t *testing.T) {
		compiler := NewCompiler()
		compiler.strictMode = true

		networkPerms := &NetworkPermissions{
			Allowed: []string{"defaults"},
			Firewall: &FirewallConfig{
				Enabled: true,
			},
		}

		sandboxConfig := &SandboxConfig{
			Agent: &AgentSandboxConfig{
				Disabled: true,
			},
		}

		// sandbox.agent: false is not allowed in strict mode for any engine
		err := compiler.validateStrictFirewall("codex", networkPerms, sandboxConfig)
		if err == nil {
			t.Error("Expected error for sandbox.agent: false in strict mode, got nil")
		}
		if err != nil && strings.Contains(err.Error(), "does not support LLM gateway") {
			t.Errorf("Expected error about sandbox.agent (not LLM gateway), got: %v", err)
		}
		if err != nil && !strings.Contains(err.Error(), "sandbox.agent") {
			t.Errorf("Expected error about sandbox.agent, got: %v", err)
		}
	})

	t.Run("strict mode disabled allows custom domains for any engine", func(t *testing.T) {
		compiler := NewCompiler()
		compiler.strictMode = false

		networkPerms := &NetworkPermissions{
			Allowed: []string{"custom-domain.com"},
		}

		err := compiler.validateStrictFirewall("copilot", networkPerms, nil)
		if err != nil {
			t.Errorf("Expected no error when strict mode is disabled, got: %v", err)
		}
	})

	t.Run("copilot engine with wildcard allows bypass without LLM gateway check", func(t *testing.T) {
		compiler := NewCompiler()
		compiler.strictMode = true

		networkPerms := &NetworkPermissions{
			Allowed: []string{"*"},
		}

		err := compiler.validateStrictFirewall("copilot", networkPerms, nil)
		if err != nil {
			t.Errorf("Expected no error for wildcard (skips all validation), got: %v", err)
		}
	})

	t.Run("custom engine without LLM gateway support rejects custom domains", func(t *testing.T) {
		compiler := NewCompiler()
		compiler.strictMode = true

		networkPerms := &NetworkPermissions{
			Allowed: []string{"custom-domain.com"},
		}

		err := compiler.validateStrictFirewall("custom", networkPerms, nil)
		if err == nil {
			t.Error("Expected error for custom engine with custom domains, got nil")
		}
		if err != nil && !strings.Contains(err.Error(), "network domains must be from known ecosystems") {
			t.Errorf("Expected error about known ecosystems, got: %v", err)
		}
	})
}

// TestSupportsLLMGateway tests the SupportsLLMGateway method for each engine
func TestSupportsLLMGateway(t *testing.T) {
	registry := NewEngineRegistry()

	tests := []struct {
		engineID           string
		expectedLLMGateway bool
		description        string
	}{
		{
			engineID:           "codex",
			expectedLLMGateway: true,
			description:        "Codex engine supports LLM gateway",
		},
		{
			engineID:           "copilot",
			expectedLLMGateway: false,
			description:        "Copilot engine does not support LLM gateway",
		},
		{
			engineID:           "claude",
			expectedLLMGateway: false,
			description:        "Claude engine does not support LLM gateway",
		},
		{
			engineID:           "custom",
			expectedLLMGateway: false,
			description:        "Custom engine does not support LLM gateway",
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			engine, err := registry.GetEngine(tt.engineID)
			if err != nil {
				t.Fatalf("Failed to get engine '%s': %v", tt.engineID, err)
			}

			supportsLLMGateway := engine.SupportsLLMGateway()
			if supportsLLMGateway != tt.expectedLLMGateway {
				t.Errorf("Engine '%s': expected SupportsLLMGateway() = %v, got %v",
					tt.engineID, tt.expectedLLMGateway, supportsLLMGateway)
			}
		})
	}
}
