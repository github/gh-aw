//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// packageRegistryDomains are the package-registry domains that must never be reachable
// from the agent sandbox unless the workflow explicitly opts into the corresponding
// ecosystem (via network.allowed or runtimes).
var packageRegistryDomains = []string{
	"registry.npmjs.org",
	"registry.npmjs.com",
	"npm.pkg.github.com",
	"pypi.org",
	"files.pythonhosted.org",
}

// allEngines is the set of engines whose default domain lists are asserted below.
var allEngines = []constants.EngineName{
	constants.CopilotEngine,
	constants.ClaudeEngine,
	constants.CodexEngine,
	constants.GeminiEngine,
	constants.AgyEngine,
	constants.PiEngine,
}

func domainSet(t *testing.T, csv string) map[string]struct{} {
	t.Helper()
	set := make(map[string]struct{})
	for d := range strings.SplitSeq(csv, ",") {
		d = strings.TrimSpace(d)
		if d != "" {
			set[d] = struct{}{}
		}
	}
	return set
}

// TestEngineDefaultDomainsExcludePackageRegistries verifies that selecting an engine alone
// never grants access to npm or PyPI. Engine CLIs and SDKs are installed by trusted runner
// steps outside the AWF sandbox, so registries do not belong in the engine default lists.
func TestEngineDefaultDomainsExcludePackageRegistries(t *testing.T) {
	for _, engine := range allEngines {
		t.Run(string(engine), func(t *testing.T) {
			defaults, err := GetDefaultDomainsForEngine(engine, "")
			require.NoError(t, err)
			for _, registry := range packageRegistryDomains {
				assert.NotContains(t, defaults, registry,
					"engine %q default domains must not include package registry %q", engine, registry)
			}
		})
	}

	// Pi's provider-specific and static default lists must be clean too.
	for _, registry := range packageRegistryDomains {
		assert.NotContains(t, PiBaseDefaultDomains, registry)
		assert.NotContains(t, PiDefaultDomains, registry)
	}
}

// TestEngineDefaultDomainsKeepTransportDomains verifies that removing package registries did
// not remove the model/API transport domains each engine genuinely requires.
func TestEngineDefaultDomainsKeepTransportDomains(t *testing.T) {
	required := map[constants.EngineName][]string{
		constants.CopilotEngine: {"api.githubcopilot.com", "host.docker.internal"},
		constants.ClaudeEngine:  {"api.anthropic.com", "host.docker.internal"},
		constants.CodexEngine:   {"api.openai.com", "host.docker.internal"},
		constants.GeminiEngine:  {"generativelanguage.googleapis.com", "host.docker.internal"},
		constants.PiEngine:      {"api.githubcopilot.com", "host.docker.internal"},
	}

	for engine, domains := range required {
		t.Run(string(engine), func(t *testing.T) {
			defaults, err := GetDefaultDomainsForEngine(engine, "")
			require.NoError(t, err)
			for _, domain := range domains {
				assert.Contains(t, defaults, domain,
					"engine %q must keep required transport domain %q", engine, domain)
			}
		})
	}
}

// TestEngineDefaultDomainsDoNotOverlapEcosystems is a broader, self-updating regression guard
// for the invariant enforced above: it does not rely on a manually curated list of registry
// domains (packageRegistryDomains), so it also catches future additions of *any* domain from
// the full "node" or "python" ecosystem definitions (pkg/workflow/data/ecosystem_domains.json)
// into an engine's static default domain list, even ones not anticipated when this test was
// written. If this test fails, the fix is almost always to remove the offending domain from
// the engine default list, not to relax this test — see the invariant documented above
// CopilotDefaultDomains in domains.go.
func TestEngineDefaultDomainsDoNotOverlapEcosystems(t *testing.T) {
	gatedEcosystems := []string{"node", "python"}

	for _, ecosystem := range gatedEcosystems {
		ecosystemDomains := getEcosystemDomains(ecosystem)
		require.NotEmpty(t, ecosystemDomains, "ecosystem %q must resolve to a non-empty domain list", ecosystem)

		for _, engine := range allEngines {
			t.Run(ecosystem+"/"+string(engine), func(t *testing.T) {
				defaults, err := GetDefaultDomainsForEngine(engine, "")
				require.NoError(t, err)
				for _, domain := range ecosystemDomains {
					assert.NotContains(t, defaults, domain,
						"engine %q default domains must not include %q ecosystem domain %q; "+
							"package/language ecosystems require explicit opt-in via network.allowed or runtimes",
						engine, ecosystem, domain)
				}
			})
		}

		t.Run(ecosystem+"/PiBaseDefaultDomains", func(t *testing.T) {
			for _, domain := range ecosystemDomains {
				assert.NotContains(t, PiBaseDefaultDomains, domain,
					"PiBaseDefaultDomains must not include %q ecosystem domain %q", ecosystem, domain)
			}
		})
		t.Run(ecosystem+"/PiDefaultDomains", func(t *testing.T) {
			for _, domain := range ecosystemDomains {
				assert.NotContains(t, PiDefaultDomains, domain,
					"PiDefaultDomains must not include %q ecosystem domain %q", ecosystem, domain)
			}
		})
	}
}

// TestNetworkPermissionsGatePackageRegistries is the end-to-end guarantee: a restrictive
// network configuration excludes package registries, and explicit ecosystem opt-in
// (network.allowed or runtimes) brings them back.
func TestNetworkPermissionsGatePackageRegistries(t *testing.T) {
	tests := []struct {
		name              string
		network           *NetworkPermissions
		runtimes          map[string]any
		expectContains    []string
		expectNotContains []string
	}{
		{
			name:              "deny-all network excludes registries",
			network:           &NetworkPermissions{Allowed: []string{}},
			expectNotContains: packageRegistryDomains,
		},
		{
			name:              "defaults + github excludes registries",
			network:           &NetworkPermissions{Allowed: []string{"defaults", "github"}},
			expectContains:    []string{"github.com"},
			expectNotContains: packageRegistryDomains,
		},
		{
			name:              "nil network excludes registries",
			network:           nil,
			expectNotContains: packageRegistryDomains,
		},
		{
			name:              "explicit node ecosystem includes npm but not pypi",
			network:           &NetworkPermissions{Allowed: []string{"defaults", "node"}},
			expectContains:    []string{"registry.npmjs.org"},
			expectNotContains: []string{"pypi.org", "files.pythonhosted.org"},
		},
		{
			name:              "explicit python ecosystem includes pypi but not npm",
			network:           &NetworkPermissions{Allowed: []string{"defaults", "python"}},
			expectContains:    []string{"pypi.org", "files.pythonhosted.org"},
			expectNotContains: []string{"registry.npmjs.org"},
		},
		{
			name:           "node runtime declaration includes npm",
			network:        &NetworkPermissions{Allowed: []string{"defaults"}},
			runtimes:       map[string]any{"node": map[string]any{"version": "24"}},
			expectContains: []string{"registry.npmjs.org"},
		},
		{
			name:           "python runtime declaration includes pypi",
			network:        &NetworkPermissions{Allowed: []string{"defaults"}},
			runtimes:       map[string]any{"python": map[string]any{"version": "3.12"}},
			expectContains: []string{"pypi.org", "files.pythonhosted.org"},
		},
	}

	for _, tt := range tests {
		for _, engine := range allEngines {
			t.Run(tt.name+"/"+string(engine), func(t *testing.T) {
				result, err := GetAllowedDomainsForEngineWithModel(engine, "", tt.network, nil, tt.runtimes)
				require.NoError(t, err)
				domains := domainSet(t, result)

				for _, expected := range tt.expectContains {
					assert.Contains(t, domains, expected,
						"engine %q should allow %q for %s", engine, expected, tt.name)
				}
				for _, unexpected := range tt.expectNotContains {
					assert.NotContains(t, domains, unexpected,
						"engine %q must not allow %q for %s", engine, unexpected, tt.name)
				}
			})
		}
	}
}
