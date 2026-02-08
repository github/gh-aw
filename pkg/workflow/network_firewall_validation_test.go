//go:build !integration

package workflow

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateNetworkFirewallConfig_AllowURLsRequiresSSLBump(t *testing.T) {
	t.Run("allow-urls without ssl-bump fails validation", func(t *testing.T) {
		networkPermissions := &NetworkPermissions{
			Firewall: &FirewallConfig{
				Enabled:   true,
				SSLBump:   false,
				AllowURLs: []string{"https://github.com/githubnext/*"},
			},
		}

		err := validateNetworkFirewallConfig(networkPermissions)

		require.Error(t, err, "Expected validation error when allow-urls is specified without ssl-bump")
		assert.Contains(t, err.Error(), "allow-urls requires ssl-bump: true", "Error should mention the ssl-bump requirement")
		assert.Contains(t, err.Error(), "network.firewall.allow-urls", "Error should identify the field")
	})

	t.Run("allow-urls with ssl-bump passes validation", func(t *testing.T) {
		networkPermissions := &NetworkPermissions{
			Firewall: &FirewallConfig{
				Enabled:   true,
				SSLBump:   true,
				AllowURLs: []string{"https://github.com/githubnext/*"},
			},
		}

		err := validateNetworkFirewallConfig(networkPermissions)

		assert.NoError(t, err, "Should not return error when ssl-bump is enabled with allow-urls")
	})

	t.Run("multiple allow-urls without ssl-bump fails validation", func(t *testing.T) {
		networkPermissions := &NetworkPermissions{
			Firewall: &FirewallConfig{
				Enabled: true,
				SSLBump: false,
				AllowURLs: []string{
					"https://github.com/githubnext/*",
					"https://api.github.com/repos/*",
					"https://example.com/api/*",
				},
			},
		}

		err := validateNetworkFirewallConfig(networkPermissions)

		require.Error(t, err, "Expected validation error when multiple allow-urls are specified without ssl-bump")
		assert.Contains(t, err.Error(), "allow-urls requires ssl-bump: true", "Error should mention the ssl-bump requirement")
	})

	t.Run("multiple allow-urls with ssl-bump passes validation", func(t *testing.T) {
		networkPermissions := &NetworkPermissions{
			Firewall: &FirewallConfig{
				Enabled: true,
				SSLBump: true,
				AllowURLs: []string{
					"https://github.com/githubnext/*",
					"https://api.github.com/repos/*",
					"https://example.com/api/*",
				},
			},
		}

		err := validateNetworkFirewallConfig(networkPermissions)

		assert.NoError(t, err, "Should not return error when ssl-bump is enabled with multiple allow-urls")
	})

	t.Run("ssl-bump without allow-urls passes validation", func(t *testing.T) {
		networkPermissions := &NetworkPermissions{
			Firewall: &FirewallConfig{
				Enabled:   true,
				SSLBump:   true,
				AllowURLs: nil,
			},
		}

		err := validateNetworkFirewallConfig(networkPermissions)

		assert.NoError(t, err, "Should not return error when ssl-bump is enabled without allow-urls")
	})

	t.Run("empty allow-urls with ssl-bump passes validation", func(t *testing.T) {
		networkPermissions := &NetworkPermissions{
			Firewall: &FirewallConfig{
				Enabled:   true,
				SSLBump:   true,
				AllowURLs: []string{},
			},
		}

		err := validateNetworkFirewallConfig(networkPermissions)

		assert.NoError(t, err, "Should not return error when allow-urls is empty")
	})

	t.Run("empty allow-urls without ssl-bump passes validation", func(t *testing.T) {
		networkPermissions := &NetworkPermissions{
			Firewall: &FirewallConfig{
				Enabled:   true,
				SSLBump:   false,
				AllowURLs: []string{},
			},
		}

		err := validateNetworkFirewallConfig(networkPermissions)

		assert.NoError(t, err, "Should not return error when allow-urls is empty")
	})

	t.Run("no firewall config passes validation", func(t *testing.T) {
		networkPermissions := &NetworkPermissions{
			Firewall: nil,
		}

		err := validateNetworkFirewallConfig(networkPermissions)

		assert.NoError(t, err, "Should not return error when firewall is not configured")
	})

	t.Run("nil network permissions passes validation", func(t *testing.T) {
		err := validateNetworkFirewallConfig(nil)

		assert.NoError(t, err, "Should not return error when network permissions is nil")
	})

	t.Run("firewall enabled false with allow-urls and no ssl-bump fails validation", func(t *testing.T) {
		networkPermissions := &NetworkPermissions{
			Firewall: &FirewallConfig{
				Enabled:   false,
				SSLBump:   false,
				AllowURLs: []string{"https://github.com/*"},
			},
		}

		err := validateNetworkFirewallConfig(networkPermissions)

		require.Error(t, err, "Expected validation error even when firewall is disabled")
		assert.Contains(t, err.Error(), "allow-urls requires ssl-bump: true", "Error should mention the ssl-bump requirement")
	})
}

func TestValidateNetworkFirewallConfig_Integration(t *testing.T) {
	t.Run("compiler rejects workflow with allow-urls but no ssl-bump", func(t *testing.T) {
		compiler := NewCompiler()
		compiler.SetStrictMode(false) // Test in non-strict mode

		workflowData := &WorkflowData{
			Name: "test-workflow",
			EngineConfig: &EngineConfig{
				ID: "copilot",
			},
			NetworkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{
					Enabled:   true,
					SSLBump:   false,
					AllowURLs: []string{"https://github.com/githubnext/*"},
				},
			},
		}

		// Manually call validation (simulating what happens in CompileWorkflowData)
		err := validateNetworkFirewallConfig(workflowData.NetworkPermissions)

		require.Error(t, err, "Compiler should reject workflow with allow-urls but no ssl-bump")
		assert.Contains(t, err.Error(), "allow-urls requires ssl-bump: true", "Error should explain the requirement")
	})

	t.Run("compiler accepts workflow with allow-urls and ssl-bump", func(t *testing.T) {
		compiler := NewCompiler()
		compiler.SetStrictMode(false)

		workflowData := &WorkflowData{
			Name: "test-workflow",
			EngineConfig: &EngineConfig{
				ID: "copilot",
			},
			NetworkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{
					Enabled:   true,
					SSLBump:   true,
					AllowURLs: []string{"https://github.com/githubnext/*"},
				},
			},
		}

		// Manually call validation
		err := validateNetworkFirewallConfig(workflowData.NetworkPermissions)

		assert.NoError(t, err, "Compiler should accept workflow with allow-urls and ssl-bump enabled")
	})
}
