//go:build !integration

package workflow

import (
	"strings"
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
		require.ErrorContains(t, err, "allow-urls requires ssl-bump: true", "Error should mention the ssl-bump requirement")
		require.ErrorContains(t, err, "network.firewall.allow-urls", "Error should identify the field")
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
		require.ErrorContains(t, err, "allow-urls requires ssl-bump: true", "Error should mention the ssl-bump requirement")
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
		require.ErrorContains(t, err, "allow-urls requires ssl-bump: true", "Error should mention the ssl-bump requirement")
	})
}

func TestValidateNetworkAllowedDomains_EcosystemIdentifiers(t *testing.T) {
	compiler := NewCompiler()

	t.Run("known ecosystem identifiers pass validation", func(t *testing.T) {
		// Use getValidEcosystemIdentifiers() to stay in sync with production code
		for _, ecosystem := range getValidEcosystemIdentifiers() {
			t.Run(ecosystem, func(t *testing.T) {
				network := &NetworkPermissions{Allowed: []string{ecosystem}}
				err := compiler.validateNetworkAllowedDomains(network)
				assert.NoError(t, err, "Known ecosystem identifier '%s' should pass validation", ecosystem)
			})
		}
	})

	t.Run("unknown single-word identifiers fail validation", func(t *testing.T) {
		invalidEcosystems := []string{
			"rustxxxx",
			"defaults2",
			"githubx",
			"nodexyz",
			"unknown",
			"fakeecosystem",
			"notarealecosystem",
			"invalidname123",
		}
		for _, ecosystem := range invalidEcosystems {
			t.Run(ecosystem, func(t *testing.T) {
				network := &NetworkPermissions{Allowed: []string{ecosystem}}
				err := compiler.validateNetworkAllowedDomains(network)
				require.Error(t, err, "Unknown ecosystem identifier '%s' should fail validation", ecosystem)
				require.ErrorContains(t, err, "not a valid ecosystem identifier", "Error should indicate invalid ecosystem identifier")
			})
		}
	})

	t.Run("domain names with dots still pass validation", func(t *testing.T) {
		validDomains := []string{"github.com", "api.example.com", "*.example.org"}
		for _, domain := range validDomains {
			t.Run(domain, func(t *testing.T) {
				network := &NetworkPermissions{Allowed: []string{domain}}
				err := compiler.validateNetworkAllowedDomains(network)
				assert.NoError(t, err, "Valid domain '%s' should pass validation", domain)
			})
		}
	})

	t.Run("mixed valid and invalid entries collects all errors", func(t *testing.T) {
		network := &NetworkPermissions{
			Allowed: []string{"defaults", "rustxxxx", "github.com", "fakeecosystem"},
		}
		err := compiler.validateNetworkAllowedDomains(network)
		require.Error(t, err, "Should fail when invalid ecosystem identifiers are present")
		require.ErrorContains(t, err, "rustxxxx", "Error should mention the invalid identifier")
		require.ErrorContains(t, err, "fakeecosystem", "Error should mention the other invalid identifier")
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
		require.ErrorContains(t, err, "allow-urls requires ssl-bump: true", "Error should explain the requirement")
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

func TestValidateHostedWebPolicy(t *testing.T) {
	tests := []struct {
		name            string
		engine          string
		runtime         string
		inline          bool
		policy          *HostedWebPolicy
		explicitNetwork bool
		firewallVersion string
		wantErr         string
	}{
		{
			name:   "allows a lowercase allowlist",
			engine: "claude",
			policy: &HostedWebPolicy{Enabled: true, Allowed: []string{"docs.github.com"}, MaxUses: 1},
		},
		{
			name:    "requires a policy list",
			engine:  "claude",
			policy:  &HostedWebPolicy{Enabled: true},
			wantErr: "requires exactly one",
		},
		{
			name:    "rejects simultaneous lists",
			engine:  "codex",
			policy:  &HostedWebPolicy{Enabled: true, Allowed: []string{"docs.github.com"}, Blocked: []string{"example.com"}},
			wantErr: "cannot both be set",
		},
		{
			name:    "rejects uppercase domains",
			engine:  "claude",
			policy:  &HostedWebPolicy{Enabled: true, Allowed: []string{"Docs.GitHub.com"}},
			wantErr: "lowercase DNS hostname",
		},
		{
			name:    "rejects IP addresses",
			engine:  "claude",
			policy:  &HostedWebPolicy{Enabled: true, Allowed: []string{"192.0.2.1"}},
			wantErr: "lowercase DNS hostname",
		},
		{
			name:    "rejects duplicate domains",
			engine:  "claude",
			policy:  &HostedWebPolicy{Enabled: true, Allowed: []string{"docs.github.com", "docs.github.com"}},
			wantErr: "duplicate domain",
		},
		{
			name:    "rejects overlong domains",
			engine:  "claude",
			policy:  &HostedWebPolicy{Enabled: true, Allowed: []string{strings.Repeat("a", 63) + "." + strings.Repeat("b", 63) + "." + strings.Repeat("c", 63) + "." + strings.Repeat("d", 61) + ".com"}},
			wantErr: "253 characters or fewer",
		},
		{
			name:   "accepts Codex runtime alias",
			engine: "codex-experimental",
			policy: &HostedWebPolicy{Enabled: true, Allowed: []string{"docs.github.com"}},
		},
		{
			name:    "rejects unrelated Codex prefix",
			engine:  "codexbridge",
			policy:  &HostedWebPolicy{Enabled: true, Allowed: []string{"docs.github.com"}},
			wantErr: "only supported",
		},
		{
			name:    "accepts custom engine backed by Codex runtime",
			engine:  "my-codex-wrapper",
			runtime: "codex",
			policy:  &HostedWebPolicy{Enabled: true, Allowed: []string{"docs.github.com"}},
		},
		{
			name:   "accepts inline Claude runtime prefix",
			engine: "claude-custom",
			inline: true,
			policy: &HostedWebPolicy{Enabled: true, Allowed: []string{"docs.github.com"}},
		},
		{
			name:            "rejects explicit policy with older AWF",
			engine:          "claude",
			policy:          &HostedWebPolicy{Enabled: true, Allowed: []string{"docs.github.com"}},
			firewallVersion: "v0.28.24",
			wantErr:         "requires AWF",
		},
		{
			name:            "rejects implicit deny with older AWF",
			engine:          "claude",
			explicitNetwork: true,
			firewallVersion: "v0.28.24",
			wantErr:         "requires AWF",
		},
		{
			name:    "rejects unsupported engine",
			engine:  "copilot",
			policy:  &HostedWebPolicy{Enabled: false},
			wantErr: "only supported",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			network := &NetworkPermissions{
				HostedWeb:         tt.policy,
				ExplicitlyDefined: tt.explicitNetwork,
			}
			if tt.firewallVersion != "" {
				network.Firewall = &FirewallConfig{Version: tt.firewallVersion}
			}
			compiler := NewCompiler()
			if tt.runtime != "" {
				compiler.engineCatalog.Register(&EngineDefinition{ID: tt.engine, RuntimeID: tt.runtime})
			}
			err := compiler.validateHostedWebPolicy(&WorkflowData{
				EngineConfig:       &EngineConfig{ID: tt.engine, IsInlineDefinition: tt.inline},
				NetworkPermissions: network,
			})
			if tt.wantErr == "" {
				assert.NoError(t, err)
			} else {
				assert.ErrorContains(t, err, tt.wantErr)
			}
		})
	}
}
