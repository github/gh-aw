//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestFirewallBlockedDomainsInCopilotEngine tests that blocked domains are included in AWF command
func TestFirewallBlockedDomainsInCopilotEngine(t *testing.T) {
	t.Run("blocked domains are added to AWF command", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
			EngineConfig: &EngineConfig{
				ID: "copilot",
			},
			NetworkPermissions: &NetworkPermissions{
				Allowed: []string{"defaults", "github"},
				Blocked: []string{"tracker.example.com", "analytics.example.com"},
				Firewall: &FirewallConfig{
					Enabled: true,
				},
			},
		}

		engine := NewCopilotEngine()
		steps := engine.GetExecutionSteps(workflowData, "test.log")

		assert.NotEmpty(t, steps, "Expected at least one execution step")

		stepContent := strings.Join(steps[0], "\n")

		// Verify --allow-domains is present
		assert.Contains(t, stepContent, "--allow-domains", "Expected command to contain '--allow-domains'")

		// Verify --block-domains is present
		assert.Contains(t, stepContent, "--block-domains", "Expected command to contain '--block-domains'")

		// Verify blocked domains are in the command
		assert.Contains(t, stepContent, "analytics.example.com", "Expected command to contain blocked domain")
		assert.Contains(t, stepContent, "tracker.example.com", "Expected command to contain blocked domain")
	})

	t.Run("no blocked domains means no --block-domains flag", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
			EngineConfig: &EngineConfig{
				ID: "copilot",
			},
			NetworkPermissions: &NetworkPermissions{
				Allowed: []string{"defaults", "github"},
				Firewall: &FirewallConfig{
					Enabled: true,
				},
			},
		}

		engine := NewCopilotEngine()
		steps := engine.GetExecutionSteps(workflowData, "test.log")

		assert.NotEmpty(t, steps, "Expected at least one execution step")

		stepContent := strings.Join(steps[0], "\n")

		// Verify --allow-domains is present
		assert.Contains(t, stepContent, "--allow-domains", "Expected command to contain '--allow-domains'")

		// Verify --block-domains is NOT present when there are no blocked domains
		assert.NotContains(t, stepContent, "--block-domains", "Expected command to NOT contain '--block-domains' when no domains are blocked")
	})

	t.Run("ecosystem identifiers are expanded in blocked domains", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
			EngineConfig: &EngineConfig{
				ID: "copilot",
			},
			NetworkPermissions: &NetworkPermissions{
				Allowed: []string{"defaults", "github"},
				Blocked: []string{"python"},
				Firewall: &FirewallConfig{
					Enabled: true,
				},
			},
		}

		engine := NewCopilotEngine()
		steps := engine.GetExecutionSteps(workflowData, "test.log")

		assert.NotEmpty(t, steps, "Expected at least one execution step")

		stepContent := strings.Join(steps[0], "\n")

		// Verify --block-domains is present
		assert.Contains(t, stepContent, "--block-domains", "Expected command to contain '--block-domains'")

		// Verify that python ecosystem domains are expanded and included
		// Get python domains to verify at least one is present
		pythonDomains := getEcosystemDomains("python")
		assert.NotEmpty(t, pythonDomains, "Python ecosystem should have domains")

		// Check that at least one python domain is in the blocked domains list
		foundPythonDomain := false
		for _, domain := range pythonDomains {
			if strings.Contains(stepContent, domain) {
				foundPythonDomain = true
				break
			}
		}
		assert.True(t, foundPythonDomain, "Expected at least one Python ecosystem domain in blocked domains")
	})
}

// TestFirewallBlockedDomainsInClaudeEngine tests that blocked domains work with Claude engine
func TestFirewallBlockedDomainsInClaudeEngine(t *testing.T) {
	t.Run("blocked domains are added to Claude AWF command", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
			EngineConfig: &EngineConfig{
				ID: "claude",
			},
			NetworkPermissions: &NetworkPermissions{
				Allowed: []string{"defaults"},
				Blocked: []string{"tracker.example.com"},
				Firewall: &FirewallConfig{
					Enabled: true,
				},
			},
		}

		engine := NewClaudeEngine()
		steps := engine.GetExecutionSteps(workflowData, "test.log")

		assert.NotEmpty(t, steps, "Expected at least one execution step")

		stepContent := strings.Join(steps[0], "\n")

		// Verify --block-domains is present
		assert.Contains(t, stepContent, "--block-domains", "Expected command to contain '--block-domains'")
		assert.Contains(t, stepContent, "tracker.example.com", "Expected command to contain blocked domain")
	})
}

// TestFirewallBlockedDomainsInCodexEngine tests that blocked domains work with Codex engine
func TestFirewallBlockedDomainsInCodexEngine(t *testing.T) {
	t.Run("blocked domains are added to Codex AWF command", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
			EngineConfig: &EngineConfig{
				ID: "codex",
			},
			NetworkPermissions: &NetworkPermissions{
				Allowed: []string{"defaults"},
				Blocked: []string{"tracker.example.com"},
				Firewall: &FirewallConfig{
					Enabled: true,
				},
			},
		}

		engine := NewCodexEngine()
		steps := engine.GetExecutionSteps(workflowData, "test.log")

		assert.NotEmpty(t, steps, "Expected at least one execution step")

		stepContent := strings.Join(steps[0], "\n")

		// Verify --block-domains is present
		assert.Contains(t, stepContent, "--block-domains", "Expected command to contain '--block-domains'")
		assert.Contains(t, stepContent, "tracker.example.com", "Expected command to contain blocked domain")
	})
}
