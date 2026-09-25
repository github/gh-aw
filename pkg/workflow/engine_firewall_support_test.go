//go:build !integration

package workflow

import (
	"fmt"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

	t.Run("blocked domains restrict defaults mode", func(t *testing.T) {
		perms := &NetworkPermissions{
			Allowed: []string{"defaults"},
			Blocked: []string{"tracker.example.com"},
		}
		if !hasNetworkRestrictions(perms) {
			t.Error("blocked domains should restrict defaults mode")
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
	t.Run("copilot engine with restrictions - warning", func(t *testing.T) {
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
		if compiler.warningCount != initialWarnings+1 {
			t.Error("Should emit warning for copilot engine with network restrictions")
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

}

func TestCheckNetworkSupport_StrictMode(t *testing.T) {
	t.Run("strict mode: copilot engine with restrictions - error", func(t *testing.T) {
		compiler := NewCompiler()
		compiler.strictMode = true
		engine := NewCopilotEngine()
		perms := &NetworkPermissions{
			Allowed: []string{"example.com"},
		}

		err := compiler.checkNetworkSupport(engine, perms)
		require.ErrorContains(t, err, "engine 'copilot' is not bound by firewall policies")
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

func TestCheckToolsNetworkSupport(t *testing.T) {
	restrictedNetwork := &NetworkPermissions{Allowed: []string{"example.com"}}
	tests := []struct {
		name         string
		tools        map[string]any
		network      *NetworkPermissions
		strictMode   bool
		wantErr      bool
		wantWarnings int
		errContains  []string
	}{
		{
			name:         "non-strict web-fetch emits warning",
			tools:        map[string]any{"web-fetch": nil},
			network:      restrictedNetwork,
			wantWarnings: 1,
		},
		{
			name:         "non-strict web-search emits warning",
			tools:        map[string]any{"web-search": map[string]any{}},
			network:      restrictedNetwork,
			wantWarnings: 1,
		},
		{
			name:         "non-strict enabled tools emit separate warnings",
			tools:        map[string]any{"web-fetch": true, "web-search": true},
			network:      restrictedNetwork,
			wantWarnings: 2,
		},
		{
			name:        "defaults with blocked domains rejects enabled tools",
			tools:       map[string]any{"web-fetch": true},
			network:     &NetworkPermissions{Allowed: []string{"defaults"}, Blocked: []string{"tracker.example.com"}},
			strictMode:  true,
			wantErr:     true,
			errContains: []string{"tools.web-fetch", "not bound by firewall policies"},
		},
		{
			name:       "strict enabled tools produce errors",
			tools:      map[string]any{"web-fetch": nil, "web-search": true},
			network:    restrictedNetwork,
			strictMode: true,
			wantErr:    true,
			errContains: []string{
				"tools.web-fetch",
				"tools.web-search",
				"not bound by firewall policies",
			},
		},
		{
			name:       "disabled tools are allowed",
			tools:      map[string]any{"web-fetch": false, "web-search": false},
			network:    restrictedNetwork,
			strictMode: true,
		},
		{
			name:       "unrelated tools are allowed",
			tools:      map[string]any{"github": nil, "bash": []any{"git"}},
			network:    restrictedNetwork,
			strictMode: true,
		},
		{
			name:       "tools are allowed without network restrictions",
			tools:      map[string]any{"web-fetch": nil, "web-search": nil},
			network:    &NetworkPermissions{Allowed: []string{"defaults"}},
			strictMode: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler(WithFailFast(false))
			compiler.strictMode = tt.strictMode

			err := compiler.checkToolsNetworkSupport(tt.tools, tt.network)

			if tt.wantErr {
				require.Error(t, err)
				for _, expected := range tt.errContains {
					require.ErrorContains(t, err, expected)
				}
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tt.wantWarnings, compiler.GetWarningCount())
		})
	}
}

func TestToolsNetworkSupportCompilation(t *testing.T) {
	workflow := func(strict bool) string {
		return fmt.Sprintf(`---
on: workflow_dispatch
engine: claude
strict: %t
network:
  allowed:
    - example.com
tools:
  web-fetch:
  web-search:
---
Test firewall-bound tool validation.
`, strict)
	}

	t.Run("non-strict mode warns and compiles", func(t *testing.T) {
		compiler := NewCompiler()
		_, err := compiler.ParseWorkflowString(workflow(false), "non-strict-tools.md")
		require.NoError(t, err)
		assert.Equal(t, 2, compiler.GetWarningCount())
	})

	t.Run("strict mode rejects tools", func(t *testing.T) {
		compiler := NewCompiler(WithFailFast(false))
		_, err := compiler.ParseWorkflowString(workflow(true), "strict-tools.md")
		require.Error(t, err)
		require.ErrorContains(t, err, "tools.web-fetch")
		require.ErrorContains(t, err, "tools.web-search")
	})
}

func TestCopilotNetworkSupportCompilation(t *testing.T) {
	workflow := func(strict bool) string {
		return fmt.Sprintf(`---
on: workflow_dispatch
engine: copilot
strict: %t
network:
  allowed:
    - example.com
---
Test Copilot firewall-bound engine validation.
`, strict)
	}

	t.Run("non-strict mode warns and compiles", func(t *testing.T) {
		compiler := NewCompiler()
		_, err := compiler.ParseWorkflowString(workflow(false), "non-strict-copilot.md")
		require.NoError(t, err)
		assert.Equal(t, 1, compiler.GetWarningCount())
	})

	t.Run("strict mode rejects engine", func(t *testing.T) {
		compiler := NewCompiler()
		_, err := compiler.ParseWorkflowString(workflow(true), "strict-copilot.md")
		require.ErrorContains(t, err, "engine 'copilot' is not bound by firewall policies")
	})
}

func TestCheckFirewallDisable(t *testing.T) {
	t.Run("firewall enabled - no validation", func(t *testing.T) {
		compiler := NewCompiler()
		perms := &NetworkPermissions{
			Allowed: []string{"example.com"},
			Firewall: &FirewallConfig{
				Enabled: true,
			},
		}

		err := compiler.checkFirewallDisable(perms)
		if err != nil {
			t.Errorf("Expected no error when firewall is enabled, got: %v", err)
		}
	})

	t.Run("firewall disabled with no restrictions - no warning", func(t *testing.T) {
		compiler := NewCompiler()
		perms := &NetworkPermissions{
			Firewall: &FirewallConfig{
				Enabled: false,
			},
		}

		initialWarnings := compiler.warningCount
		err := compiler.checkFirewallDisable(perms)
		if err != nil {
			t.Errorf("Expected no error when firewall is disabled with no restrictions, got: %v", err)
		}
		if compiler.warningCount != initialWarnings {
			t.Error("Should not emit warning when firewall is disabled with no restrictions")
		}
	})

	t.Run("firewall disabled with restrictions - warning emitted", func(t *testing.T) {
		compiler := NewCompiler()
		perms := &NetworkPermissions{
			Allowed: []string{"example.com"},
			Firewall: &FirewallConfig{
				Enabled: false,
			},
		}

		initialWarnings := compiler.warningCount
		err := compiler.checkFirewallDisable(perms)
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
		perms := &NetworkPermissions{
			Allowed: []string{"example.com"},
			Firewall: &FirewallConfig{
				Enabled: false,
			},
		}

		err := compiler.checkFirewallDisable(perms)
		if err == nil {
			t.Error("Expected error in strict mode when firewall is disabled with restrictions")
		}
		if !strings.Contains(err.Error(), "strict mode") {
			t.Errorf("Error should mention strict mode, got: %v", err)
		}
	})

	t.Run("nil firewall config - no validation", func(t *testing.T) {
		compiler := NewCompiler()
		perms := &NetworkPermissions{
			Allowed: []string{"example.com"},
		}

		err := compiler.checkFirewallDisable(perms)
		if err != nil {
			t.Errorf("Expected no error when firewall config is nil, got: %v", err)
		}
	})
}

func TestGenerateFirewallLogParsingStepFixesFirewallPermissions(t *testing.T) {
	step := generateFirewallLogParsingStep("test-workflow", nil)
	stepContent := strings.Join(step, "\n")
	expectedLogsDir := constants.AWFProxyLogsDir.String()

	if !strings.Contains(stepContent, "AWF_LOGS_DIR: "+expectedLogsDir) {
		t.Error("Expected firewall log parsing step to keep AWF_LOGS_DIR set to logs directory")
	}

	// Step should invoke the extracted script (not --rootless for non-network-isolation)
	if !strings.Contains(stepContent, `bash "${RUNNER_TEMP}/gh-aw/actions/print_firewall_logs.sh"`) {
		t.Error("Expected firewall log parsing step to invoke print_firewall_logs.sh")
	}

	// Default (non-network-isolation) mode must NOT pass --rootless
	if strings.Contains(stepContent, "--rootless") {
		t.Error("Expected firewall log parsing step to not pass --rootless when network isolation is disabled")
	}
}

func TestGenerateFirewallLogParsingStepNetworkIsolationOmitsSudo(t *testing.T) {
	workflowData := &WorkflowData{
		Name: "test-workflow",
		SandboxConfig: &SandboxConfig{
			Agent: &AgentSandboxConfig{
				ID: "awf",
			},
		},
	}
	step := generateFirewallLogParsingStep("test-workflow", workflowData)
	stepContent := strings.Join(step, "\n")

	// Step should invoke the extracted script with --rootless for network-isolation mode
	if !strings.Contains(stepContent, `bash "${RUNNER_TEMP}/gh-aw/actions/print_firewall_logs.sh" --rootless`) {
		t.Error("Expected firewall log parsing step to invoke print_firewall_logs.sh --rootless in network-isolation mode")
	}
}

func TestGenerateFirewallLogParsingStepWithPrivilegedRuntime(t *testing.T) {
	workflowData := &WorkflowData{
		Name: "test-workflow",
		SandboxConfig: &SandboxConfig{
			Agent: &AgentSandboxConfig{
				ID:      "awf",
				Runtime: AgentRuntimeCloudHypervisor,
			},
		},
	}
	step := generateFirewallLogParsingStep("test-workflow", workflowData)
	stepContent := strings.Join(step, "\n")

	// Runtime profiles that run AWF with sudo must invoke the script without --rootless
	if !strings.Contains(stepContent, `bash "${RUNNER_TEMP}/gh-aw/actions/print_firewall_logs.sh"`) {
		t.Error("Expected firewall log parsing step to invoke print_firewall_logs.sh for a privileged runtime")
	}
	if strings.Contains(stepContent, "--rootless") {
		t.Error("Expected no --rootless flag for a privileged runtime profile")
	}
}

func TestGenerateFirewallLogParsingStepLegacySecurityOmitsRootless(t *testing.T) {
	// When legacy-security: enable is set, AWF ran with full sudo access, so the
	// log parsing script must use plain sudo (no --rootless), even though
	// NetworkIsolation defaults to true.
	workflowData := &WorkflowData{
		Name: "test-workflow",
		SandboxConfig: &SandboxConfig{
			Agent: &AgentSandboxConfig{
				ID:      "awf",
				Runtime: AgentRuntimeDockerSudoIptables,
			},
		},
	}
	step := generateFirewallLogParsingStep("test-workflow", workflowData)
	stepContent := strings.Join(step, "\n")

	// Legacy-security mode must NOT pass --rootless to the log parsing script.
	if strings.Contains(stepContent, "--rootless") {
		t.Error("Expected no --rootless flag when legacy-security: enable is set (AWF has full sudo access)")
	}

	if !strings.Contains(stepContent, `bash "${RUNNER_TEMP}/gh-aw/actions/print_firewall_logs.sh"`) {
		t.Error("Expected firewall log parsing step to invoke print_firewall_logs.sh")
	}
}
