//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

func TestSupportsFirewall(t *testing.T) {
	t.Run("copilot engine supports firewall", func(t *testing.T) {
		engine := NewCopilotEngine()
		if !engine.SupportsFirewall() {
			t.Error("Copilot engine should support firewall")
		}
	})

	t.Run("claude engine supports firewall", func(t *testing.T) {
		engine := NewClaudeEngine()
		if !engine.SupportsFirewall() {
			t.Error("Claude engine should support firewall")
		}
	})

	t.Run("codex engine supports firewall", func(t *testing.T) {
		engine := NewCodexEngine()
		if !engine.SupportsFirewall() {
			t.Error("Codex engine should support firewall")
		}
	})

	t.Run("custom engine does not support firewall", func(t *testing.T) {
		engine := NewCustomEngine()
		if engine.SupportsFirewall() {
			t.Error("Custom engine should not support firewall")
		}
	})
}

func TestHasNetworkRestrictions(t *testing.T) {
	t.Run("nil permissions have no restrictions", func(t *testing.T) {
		if hasNetworkRestrictions(nil) {
			t.Error("nil permissions should not have restrictions")
		}
	})

	t.Run("defaults mode has no restrictions", func(t *testing.T) {
		perms := &NetworkPermissions{
			Allowed: []string{"defaults"},
		}
		if hasNetworkRestrictions(perms) {
			t.Error("defaults mode should not have restrictions")
		}
	})

	t.Run("allowed domains define restrictions", func(t *testing.T) {
		perms := &NetworkPermissions{
			Allowed: []string{"example.com", "api.github.com"},
		}
		if !hasNetworkRestrictions(perms) {
			t.Error("allowed domains should indicate restrictions")
		}
	})

	t.Run("empty allowed list with no mode is a restriction", func(t *testing.T) {
		perms := &NetworkPermissions{
			Allowed:           []string{},
			ExplicitlyDefined: true,
		}
		if !hasNetworkRestrictions(perms) {
			t.Error("empty object {} should indicate deny-all restriction")
		}
	})
}

func TestCheckNetworkSupport_NoRestrictions(t *testing.T) {
	compiler := NewCompiler()

	t.Run("no restrictions with copilot engine", func(t *testing.T) {
		engine := NewCopilotEngine()
		perms := &NetworkPermissions{Allowed: []string{"defaults"}}
		err := compiler.checkNetworkSupport(engine, perms)
		if err != nil {
			t.Errorf("Expected no error, got: %v", err)
		}
	})

	t.Run("no restrictions with claude engine", func(t *testing.T) {
		engine := NewClaudeEngine()
		perms := &NetworkPermissions{Allowed: []string{"defaults"}}
		err := compiler.checkNetworkSupport(engine, perms)
		if err != nil {
			t.Errorf("Expected no error, got: %v", err)
		}
	})

	t.Run("nil permissions with any engine", func(t *testing.T) {
		engine := NewCodexEngine()
		err := compiler.checkNetworkSupport(engine, nil)
		if err != nil {
			t.Errorf("Expected no error, got: %v", err)
		}
	})
}

func TestCheckNetworkSupport_WithRestrictions(t *testing.T) {
	t.Run("copilot engine with restrictions - no warning", func(t *testing.T) {
		compiler := NewCompiler()
		engine := NewCopilotEngine()
		perms := &NetworkPermissions{
			Allowed: []string{"example.com", "api.github.com"},
		}

		initialWarnings := compiler.warningCount
		err := compiler.checkNetworkSupport(engine, perms)
		if err != nil {
			t.Errorf("Expected no error, got: %v", err)
		}
		if compiler.warningCount != initialWarnings {
			t.Error("Should not emit warning for copilot engine with network restrictions")
		}
	})

	t.Run("claude engine with restrictions - no warning (supports firewall)", func(t *testing.T) {
		compiler := NewCompiler()
		engine := NewClaudeEngine()
		perms := &NetworkPermissions{
			Allowed: []string{"example.com"},
		}

		initialWarnings := compiler.warningCount
		err := compiler.checkNetworkSupport(engine, perms)
		if err != nil {
			t.Errorf("Expected no error, got: %v", err)
		}
		if compiler.warningCount != initialWarnings {
			t.Error("Should not emit warning for claude engine with network restrictions (supports firewall)")
		}
	})

	t.Run("codex engine with restrictions - no warning", func(t *testing.T) {
		compiler := NewCompiler()
		engine := NewCodexEngine()
		perms := &NetworkPermissions{
			Allowed: []string{"api.openai.com"},
		}

		initialWarnings := compiler.warningCount
		err := compiler.checkNetworkSupport(engine, perms)
		if err != nil {
			t.Errorf("Expected no error, got: %v", err)
		}
		if compiler.warningCount != initialWarnings {
			t.Error("Should not emit warning for codex engine with network restrictions")
		}
	})

	t.Run("custom engine with restrictions - warning emitted", func(t *testing.T) {
		compiler := NewCompiler()
		engine := NewCustomEngine()
		perms := &NetworkPermissions{
			Allowed: []string{"example.com"},
		}

		initialWarnings := compiler.warningCount
		err := compiler.checkNetworkSupport(engine, perms)
		if err != nil {
			t.Errorf("Expected no error, got: %v", err)
		}
		if compiler.warningCount != initialWarnings+1 {
			t.Error("Should emit warning for custom engine with network restrictions")
		}
	})
}

func TestCheckNetworkSupport_StrictMode(t *testing.T) {
	t.Run("strict mode: copilot engine with restrictions - no error", func(t *testing.T) {
		compiler := NewCompiler()
		compiler.strictMode = true
		engine := NewCopilotEngine()
		perms := &NetworkPermissions{
			Allowed: []string{"example.com"},
		}

		err := compiler.checkNetworkSupport(engine, perms)
		if err != nil {
			t.Errorf("Expected no error for copilot in strict mode, got: %v", err)
		}
	})

	t.Run("strict mode: claude engine with restrictions - no error (claude supports firewall)", func(t *testing.T) {
		compiler := NewCompiler()
		compiler.strictMode = true
		engine := NewClaudeEngine()
		perms := &NetworkPermissions{
			Allowed: []string{"example.com"},
		}

		err := compiler.checkNetworkSupport(engine, perms)
		if err != nil {
			t.Errorf("Expected no error for claude in strict mode (supports firewall), got: %v", err)
		}
	})

	t.Run("strict mode: codex engine with restrictions - no error", func(t *testing.T) {
		compiler := NewCompiler()
		compiler.strictMode = true
		engine := NewCodexEngine()
		perms := &NetworkPermissions{
			Allowed: []string{"api.openai.com"},
		}

		err := compiler.checkNetworkSupport(engine, perms)
		if err != nil {
			t.Errorf("Expected no error for codex in strict mode, got: %v", err)
		}
	})

	t.Run("strict mode: custom engine with restrictions - error", func(t *testing.T) {
		compiler := NewCompiler()
		compiler.strictMode = true
		engine := NewCustomEngine()
		perms := &NetworkPermissions{
			Allowed: []string{"example.com"},
		}

		err := compiler.checkNetworkSupport(engine, perms)
		if err == nil {
			t.Error("Expected error in strict mode for custom engine with restrictions")
		}
	})

	t.Run("strict mode: no restrictions - no error", func(t *testing.T) {
		compiler := NewCompiler()
		compiler.strictMode = true
		engine := NewClaudeEngine()
		perms := &NetworkPermissions{Allowed: []string{"defaults"}}

		err := compiler.checkNetworkSupport(engine, perms)
		if err != nil {
			t.Errorf("Expected no error when no restrictions in strict mode, got: %v", err)
		}
	})
}

func TestCheckFirewallDisable(t *testing.T) {
	t.Run("firewall enabled - no validation", func(t *testing.T) {
		compiler := NewCompiler()
		engine := NewCopilotEngine()
		perms := &NetworkPermissions{
			Allowed: []string{"example.com"},
			Firewall: &FirewallConfig{
				Enabled: true,
			},
		}

		err := compiler.checkFirewallDisable(engine, perms)
		if err != nil {
			t.Errorf("Expected no error when firewall is enabled, got: %v", err)
		}
	})

	t.Run("firewall disabled with no restrictions - no warning", func(t *testing.T) {
		compiler := NewCompiler()
		engine := NewCopilotEngine()
		perms := &NetworkPermissions{
			Firewall: &FirewallConfig{
				Enabled: false,
			},
		}

		initialWarnings := compiler.warningCount
		err := compiler.checkFirewallDisable(engine, perms)
		if err != nil {
			t.Errorf("Expected no error when firewall is disabled with no restrictions, got: %v", err)
		}
		if compiler.warningCount != initialWarnings {
			t.Error("Should not emit warning when firewall is disabled with no restrictions")
		}
	})

	t.Run("firewall disabled with restrictions - warning emitted", func(t *testing.T) {
		compiler := NewCompiler()
		engine := NewCopilotEngine()
		perms := &NetworkPermissions{
			Allowed: []string{"example.com"},
			Firewall: &FirewallConfig{
				Enabled: false,
			},
		}

		initialWarnings := compiler.warningCount
		err := compiler.checkFirewallDisable(engine, perms)
		if err != nil {
			t.Errorf("Expected no error in non-strict mode, got: %v", err)
		}
		if compiler.warningCount != initialWarnings+1 {
			t.Error("Should emit warning when firewall is disabled with restrictions")
		}
	})

	t.Run("strict mode: firewall disabled with restrictions - error", func(t *testing.T) {
		compiler := NewCompiler()
		compiler.strictMode = true
		engine := NewCopilotEngine()
		perms := &NetworkPermissions{
			Allowed: []string{"example.com"},
			Firewall: &FirewallConfig{
				Enabled: false,
			},
		}

		err := compiler.checkFirewallDisable(engine, perms)
		if err == nil {
			t.Error("Expected error in strict mode when firewall is disabled with restrictions")
		}
		if !strings.Contains(err.Error(), "strict mode") {
			t.Errorf("Error should mention strict mode, got: %v", err)
		}
	})

	t.Run("strict mode: firewall disabled with unsupported engine - error", func(t *testing.T) {
		compiler := NewCompiler()
		compiler.strictMode = true
		engine := NewCustomEngine() // Custom engine doesn't support firewall
		perms := &NetworkPermissions{
			Firewall: &FirewallConfig{
				Enabled: false,
			},
		}

		err := compiler.checkFirewallDisable(engine, perms)
		if err == nil {
			t.Error("Expected error in strict mode when firewall is disabled with unsupported engine")
		} else if !strings.Contains(err.Error(), "does not support firewall") {
			t.Errorf("Error should mention firewall support, got: %v", err)
		}
	})

	t.Run("nil firewall config - no validation", func(t *testing.T) {
		compiler := NewCompiler()
		engine := NewCopilotEngine()
		perms := &NetworkPermissions{
			Allowed: []string{"example.com"},
		}

		err := compiler.checkFirewallDisable(engine, perms)
		if err != nil {
			t.Errorf("Expected no error when firewall config is nil, got: %v", err)
		}
	})
}
