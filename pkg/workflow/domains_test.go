//go:build !integration

package workflow

import (
	"slices"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetDomainEcosystem(t *testing.T) {
	tests := []struct {
		name     string
		domain   string
		expected string
	}{
		// Exact matches for defaults ecosystem
		{
			name:     "defaults ecosystem - exact match",
			domain:   "json-schema.org",
			expected: "defaults",
		},
		{
			name:     "defaults ecosystem - ubuntu archive",
			domain:   "archive.ubuntu.com",
			expected: "defaults",
		},
		{
			name:     "defaults ecosystem - digicert",
			domain:   "ocsp.digicert.com",
			expected: "defaults",
		},

		// Container ecosystem exact matches
		{
			name:     "containers ecosystem - ghcr.io",
			domain:   "ghcr.io",
			expected: "containers",
		},
		{
			name:     "containers ecosystem - quay.io",
			domain:   "quay.io",
			expected: "containers",
		},

		// Fonts ecosystem (takes priority over chrome for fonts.googleapis.com)
		{
			name:     "fonts ecosystem - fonts.googleapis.com",
			domain:   "fonts.googleapis.com",
			expected: "fonts",
		},
		{
			name:     "fonts ecosystem - fonts.gstatic.com",
			domain:   "fonts.gstatic.com",
			expected: "fonts",
		},

		// Chrome ecosystem (headless Chrome/Puppeteer browser testing)
		{
			name:     "chrome ecosystem - accounts.google.com",
			domain:   "accounts.google.com",
			expected: "chrome",
		},
		{
			name:     "chrome ecosystem - www.google.com",
			domain:   "www.google.com",
			expected: "chrome",
		},
		{
			name:     "chrome ecosystem - safebrowsing.googleapis.com",
			domain:   "safebrowsing.googleapis.com",
			expected: "chrome",
		},
		{
			name:     "chrome ecosystem - optimizationguide-pa.googleapis.com",
			domain:   "optimizationguide-pa.googleapis.com",
			expected: "chrome",
		},
		{
			name:     "chrome ecosystem - update.googleapis.com",
			domain:   "update.googleapis.com",
			expected: "chrome",
		},
		{
			name:     "chrome ecosystem - redirector.gvt1.com",
			domain:   "redirector.gvt1.com",
			expected: "chrome",
		},
		// Java ecosystem takes priority over chrome for its Google domains
		{
			name:     "java ecosystem - maven.google.com (not chrome)",
			domain:   "maven.google.com",
			expected: "java",
		},
		{
			name:     "java ecosystem - dl.google.com (not chrome)",
			domain:   "dl.google.com",
			expected: "java",
		},
		// Defaults ecosystem takes priority over chrome for packages.cloud.google.com
		{
			name:     "defaults ecosystem - packages.cloud.google.com (not chrome)",
			domain:   "packages.cloud.google.com",
			expected: "defaults",
		},

		// Deno ecosystem
		{
			name:     "deno ecosystem - fresh.deno.dev",
			domain:   "fresh.deno.dev",
			expected: "deno",
		},
		{
			name:     "deno ecosystem - googleapis.deno.dev",
			domain:   "googleapis.deno.dev",
			expected: "deno",
		},
		{
			name:     "deno ecosystem - deno.land",
			domain:   "deno.land",
			expected: "deno",
		},
		{
			name:     "deno ecosystem - jsr.io exact",
			domain:   "jsr.io",
			expected: "deno",
		},

		// Node CDNs ecosystem
		{
			name:     "node-cdns ecosystem - cdn.jsdelivr.net",
			domain:   "cdn.jsdelivr.net",
			expected: "node-cdns",
		},

		// Container ecosystem wildcard matches
		{
			name:     "containers ecosystem - docker.io subdomain",
			domain:   "registry-1.docker.io",
			expected: "containers",
		},
		{
			name:     "containers ecosystem - docker.com subdomain",
			domain:   "hub.docker.com",
			expected: "containers",
		},
		{
			name:     "containers ecosystem - docker.io base domain",
			domain:   "docker.io",
			expected: "containers",
		},

		// Python ecosystem (assuming pypi.org exists)
		{
			name:     "python ecosystem - pypi",
			domain:   "pypi.org",
			expected: "python",
		},

		// Non-matching domain
		{
			name:     "no ecosystem match - custom domain",
			domain:   "example.com",
			expected: "",
		},
		{
			name:     "no ecosystem match - empty string",
			domain:   "",
			expected: "",
		},

		// Edge cases
		{
			name:     "no ecosystem match - partial match should not work",
			domain:   "notdocker.io",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetDomainEcosystem(tt.domain)
			if result != tt.expected {
				t.Errorf("GetDomainEcosystem(%q) = %q, expected %q", tt.domain, result, tt.expected)
			}
		})
	}
}

// TestGetDomainEcosystem_Determinism verifies that GetDomainEcosystem returns the same result
// across repeated calls for domains that exist in multiple ecosystems (e.g. cdn.jsdelivr.net
// is in both "node" and "node-cdns" and must always resolve to "node-cdns").
func TestGetDomainEcosystem_Determinism(t *testing.T) {
	cases := []struct {
		domain   string
		expected string
	}{
		{"cdn.jsdelivr.net", "node-cdns"},
		{"crates.io", "rust"}, // also appears in python ecosystem
		{"index.crates.io", "rust"},
		{"static.crates.io", "rust"},
	}
	for _, c := range cases {
		for i := range 20 {
			got := GetDomainEcosystem(c.domain)
			if got != c.expected {
				t.Errorf("call %d: GetDomainEcosystem(%q) = %q, want %q", i, c.domain, got, c.expected)
			}
		}
	}
}

func TestMatchesDomain(t *testing.T) {
	tests := []struct {
		name     string
		domain   string
		pattern  string
		expected bool
	}{
		// Exact matches
		{
			name:     "exact match - same string",
			domain:   "example.com",
			pattern:  "example.com",
			expected: true,
		},
		{
			name:     "exact match - github.com",
			domain:   "github.com",
			pattern:  "github.com",
			expected: true,
		},
		{
			name:     "no match - different domains",
			domain:   "example.com",
			pattern:  "different.com",
			expected: false,
		},

		// Wildcard matches with subdomains
		{
			name:     "wildcard match - subdomain of docker.io",
			domain:   "registry-1.docker.io",
			pattern:  "*.docker.io",
			expected: true,
		},
		{
			name:     "wildcard match - multiple levels deep",
			domain:   "a.b.c.docker.io",
			pattern:  "*.docker.io",
			expected: true,
		},
		{
			name:     "wildcard match - base domain without wildcard",
			domain:   "docker.io",
			pattern:  "*.docker.io",
			expected: true,
		},
		{
			name:     "wildcard match - docker.com subdomain",
			domain:   "hub.docker.com",
			pattern:  "*.docker.com",
			expected: true,
		},
		{
			name:     "wildcard match - base domain docker.com",
			domain:   "docker.com",
			pattern:  "*.docker.com",
			expected: true,
		},

		// Wildcard non-matches
		{
			name:     "no wildcard match - wrong domain",
			domain:   "example.com",
			pattern:  "*.docker.io",
			expected: false,
		},
		{
			name:     "no wildcard match - partial suffix",
			domain:   "notdocker.io",
			pattern:  "*.docker.io",
			expected: false,
		},
		{
			name:     "no wildcard match - prefix instead of suffix",
			domain:   "docker.io.example",
			pattern:  "*.docker.io",
			expected: false,
		},

		// Edge cases
		{
			name:     "empty domain and pattern",
			domain:   "",
			pattern:  "",
			expected: true,
		},
		{
			name:     "empty domain with pattern",
			domain:   "",
			pattern:  "example.com",
			expected: false,
		},
		{
			name:     "domain with empty pattern",
			domain:   "example.com",
			pattern:  "",
			expected: false,
		},
		{
			name:     "wildcard with empty base",
			domain:   "example.com",
			pattern:  "*.",
			expected: false,
		},
		{
			name:     "just wildcard",
			domain:   "example.com",
			pattern:  "*",
			expected: false,
		},
		{
			name:     "pattern with only *. matches empty domain",
			domain:   "",
			pattern:  "*.",
			expected: true, // Edge case: suffix is "", domain == suffix returns true
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := matchesDomain(tt.domain, tt.pattern)
			if result != tt.expected {
				t.Errorf("matchesDomain(%q, %q) = %v, expected %v", tt.domain, tt.pattern, result, tt.expected)
			}
		})
	}
}

func TestCopilotDefaultDomains(t *testing.T) {
	// Verify that expected Copilot domains are present
	expectedDomains := []string{
		"api.github.com",
		"api.githubcopilot.com",
		"github.com",
		"host.docker.internal",
		"raw.githubusercontent.com",
	}

	// Create a map for O(1) lookups
	domainMap := make(map[string]bool)
	for _, domain := range CopilotDefaultDomains {
		domainMap[domain] = true
	}

	for _, expected := range expectedDomains {
		if !domainMap[expected] {
			t.Errorf("Expected domain %q not found in CopilotDefaultDomains", expected)
		}
	}

	// Package registries must never be part of the engine defaults: they would be
	// reachable even when the workflow declares network: {} (see security hardening
	// for engine default domains).
	assert.NotContains(t, CopilotDefaultDomains, "registry.npmjs.org",
		"CopilotDefaultDomains must not include the npm registry; it requires explicit node opt-in")

	// Verify the count matches (no extra domains)
	if len(CopilotDefaultDomains) != len(expectedDomains) {
		t.Errorf("CopilotDefaultDomains has %d domains, expected %d", len(CopilotDefaultDomains), len(expectedDomains))
	}
}

// TestCopilotVendorDomainsRequireOptIn verifies that plan-specific Copilot API hosts and
// Copilot telemetry are not unconditional engine defaults: agents route inference through
// the AWF api-proxy, so these vendor hosts must be requested explicitly via the
// "copilot-vendor" ecosystem.
func TestCopilotVendorDomainsRequireOptIn(t *testing.T) {
	vendorDomains := []string{
		"api.business.githubcopilot.com",
		"api.enterprise.githubcopilot.com",
		"api.individual.githubcopilot.com",
		"telemetry.enterprise.githubcopilot.com",
	}

	optIn := getEcosystemDomains("copilot-vendor")
	require.NotEmpty(t, optIn, "copilot-vendor ecosystem must be defined")

	for _, domain := range vendorDomains {
		assert.NotContains(t, CopilotDefaultDomains, domain,
			"CopilotDefaultDomains must not include vendor domain %q; it requires explicit copilot-vendor opt-in", domain)
		assert.NotContains(t, PiDefaultDomains, domain,
			"PiDefaultDomains must not include vendor domain %q; it requires explicit copilot-vendor opt-in", domain)
		assert.Contains(t, optIn, domain,
			"copilot-vendor ecosystem must include %q", domain)
	}

	// The opt-in set must resolve through the regular network allow-list expansion.
	expanded := GetAllowedDomains(&NetworkPermissions{Allowed: []string{"copilot-vendor"}})
	for _, domain := range vendorDomains {
		assert.Contains(t, expanded, domain,
			"network: { allowed: [copilot-vendor] } must expand to %q", domain)
	}
}

func TestThreatDetectionDomains(t *testing.T) {
	detectionDomains := engineDefaultDomainSets["threat-detection"]

	// Detection domains must include every required Copilot API domain
	requiredDomains := []string{
		"api.business.githubcopilot.com",
		"api.enterprise.githubcopilot.com",
		"api.github.com",
		"api.githubcopilot.com",
		"api.individual.githubcopilot.com",
		"github.com",
		"host.docker.internal",
		"registry.npmjs.org",
		"telemetry.enterprise.githubcopilot.com",
	}

	detectionMap := make(map[string]bool)
	for _, d := range detectionDomains {
		detectionMap[d] = true
	}
	for _, required := range requiredDomains {
		assert.True(t, detectionMap[required], "Required domain %q not found in threat-detection domain set", required)
	}

	// Detection domains must NOT include the domains excluded for supply-chain reduction
	excludedDomains := []string{
		"raw.githubusercontent.com",
	}
	for _, excluded := range excludedDomains {
		assert.False(t, detectionMap[excluded], "Domain %q should not be in threat-detection domain set (excluded to reduce supply chain surface)", excluded)
	}

	// Verify exact count — no silent additions
	assert.Len(t, detectionDomains, len(requiredDomains),
		"threat-detection domain set should have exactly %d entries", len(requiredDomains))
}

func TestThreatDetectionNetworkAllowedCompatibilityAlias(t *testing.T) {
	engineDefaults := engineDefaultDomainSets["threat-detection"]
	expanded := GetAllowedDomains(&NetworkPermissions{Allowed: []string{"threat-detection"}})

	assert.Equal(t, engineDefaults, expanded,
		"legacy network.allowed threat-detection alias must expand to the Copilot detection domain set")
}

func TestEngineDomainSetsRequireExplicitNetworkAllowedOptIn(t *testing.T) {
	result := GetAllowedDomainsForEngine(constants.CopilotEngine, &NetworkPermissions{Allowed: []string{"github"}}, nil, nil)

	assert.NotContains(t, result, "api.githubcopilot.com",
		"selecting the Copilot engine must not automatically add the copilot domain set")

	result = GetAllowedDomainsForEngine(constants.CopilotEngine, &NetworkPermissions{Allowed: []string{"copilot", "github"}}, nil, nil)
	assert.Contains(t, result, "api.githubcopilot.com",
		"the copilot domain set should expand when explicitly referenced in network.allowed")
	assert.True(t, isKnownEcosystemIdentifier("copilot"),
		"engine domain sets should validate like other domain set identifiers")
}

func TestEmbeddedDomainSets(t *testing.T) {
	sets := getLoadedDomainSets()

	assert.Contains(t, sets.Ecosystems, "defaults")
	assert.Contains(t, sets.Ecosystems, "threat-detection")
	assert.Contains(t, sets.EngineDefaults, "copilot")
	assert.Equal(t, "api.githubcopilot.com", sets.PiProviderDomains["copilot"])
	assert.Equal(t, []string{"github.com", "localhost"}, sets.SanitizationDefaults)
	assert.Equal(t, sets.EngineDefaults["threat-detection"], sets.Ecosystems["threat-detection"],
		"legacy network.allowed threat-detection alias must stay in sync with Copilot detection defaults")
}

func TestGetThreatDetectionAllowedDomains(t *testing.T) {
	// With empty network permissions the result equals the sorted detection domains
	result := GetThreatDetectionAllowedDomains(&NetworkPermissions{Allowed: []string{}})
	assert.NotEmpty(t, result, "GetThreatDetectionAllowedDomains should return non-empty string")

	// Must include essential Copilot API domain
	assert.Contains(t, result, "api.githubcopilot.com", "Detection domains must include Copilot API domain")

	// Must include npm registry for read-only package validation
	assert.Contains(t, result, "registry.npmjs.org", "Detection domains must include npm registry for read-only package validation")

	// Must NOT include raw GitHub downloads
	assert.NotContains(t, result, "raw.githubusercontent.com", "Detection domains must not include raw.githubusercontent.com")

	// Must NOT include ecosystem-expansion domains (no tools/runtimes passed for detection)
	assert.NotContains(t, result, "pypi.org", "Detection domains must not include Python ecosystem domains")
	assert.NotContains(t, result, "archive.ubuntu.com", "Detection domains must not include Ubuntu apt domains")
}

func TestCodexDefaultDomains(t *testing.T) {
	// Verify that expected Codex domains are present
	expectedDomains := []string{
		"172.30.0.1", // AWF gateway IP - Codex resolves host.docker.internal to this IP
		"api.github.com",
		"api.openai.com",
		"chatgpt.com", // Codex CLI connects to chatgpt.com (and subdomains e.g. ab.chatgpt.com) for auth/telemetry
		"github.com",
		"host.docker.internal",
		"openai.com",
	}

	// Create a map for O(1) lookups
	domainMap := make(map[string]bool)
	for _, domain := range CodexDefaultDomains {
		domainMap[domain] = true
	}

	for _, expected := range expectedDomains {
		if !domainMap[expected] {
			t.Errorf("Expected domain %q not found in CodexDefaultDomains", expected)
		}
	}

	// Verify the count matches (no extra domains)
	if len(CodexDefaultDomains) != len(expectedDomains) {
		t.Errorf("CodexDefaultDomains has %d domains, expected %d", len(CodexDefaultDomains), len(expectedDomains))
	}
}

func TestClaudeDefaultDomains(t *testing.T) {
	// Verify that critical Claude domains are present
	criticalDomains := []string{
		"anthropic.com",
		"api.anthropic.com",
		"statsig.anthropic.com",
		"api.github.com",
		"github.com",
		"host.docker.internal",
	}

	// Create a map for O(1) lookups
	domainMap := make(map[string]bool)
	for _, domain := range ClaudeDefaultDomains {
		domainMap[domain] = true
	}

	for _, expected := range criticalDomains {
		if !domainMap[expected] {
			t.Errorf("Expected domain %q not found in ClaudeDefaultDomains", expected)
		}
	}

	// Package registries must never be part of the engine defaults: they would be
	// reachable even when the workflow declares network: {}.
	for _, registry := range []string{"registry.npmjs.org", "pypi.org", "files.pythonhosted.org"} {
		assert.NotContains(t, ClaudeDefaultDomains, registry,
			"ClaudeDefaultDomains must not include %q; package registries require explicit node/python opt-in", registry)
	}

	// Verify minimum count (Claude has many more domains than the critical ones)
	if len(ClaudeDefaultDomains) < len(criticalDomains) {
		t.Errorf("ClaudeDefaultDomains has %d domains, expected at least %d", len(ClaudeDefaultDomains), len(criticalDomains))
	}
}

// TestGetAllowedDomains_ModeDefaultsWithAllowedList verifies that when there's an Allowed list
// with multiple ecosystems, it processes and expands all of them
func TestGetAllowedDomains_ModeDefaultsWithAllowedList(t *testing.T) {
	network := &NetworkPermissions{
		Allowed: []string{
			"defaults",
			"github",
		},
	}

	domains := GetAllowedDomains(network)

	// Should include both defaults and github ecosystem domains
	// Check for some representative domains from each ecosystem
	hasDefaults := false
	hasGitHub := false

	for _, domain := range domains {
		if domain == "json-schema.org" {
			hasDefaults = true
		}
		if domain == "github.githubassets.com" {
			hasGitHub = true
		}
	}

	if !hasDefaults {
		t.Error("Expected domains list to include 'json-schema.org' from defaults ecosystem")
	}
	if !hasGitHub {
		t.Error("Expected domains list to include 'github.githubassets.com' from github ecosystem")
	}

	t.Logf("Total domains: %d", len(domains))
}

// TestGetAllowedDomains_VariousCombinations tests various combinations of domain configurations
func TestGetAllowedDomains_VariousCombinations(t *testing.T) {
	tests := []struct {
		name              string
		allowed           []string
		expectContains    []string // Domains that must be in the result
		expectNotContains []string // Domains that must NOT be in the result
		expectEmpty       bool     // If true, expect empty result
	}{
		{
			name:           "nil network permissions returns defaults",
			allowed:        nil,
			expectContains: []string{"json-schema.org", "archive.ubuntu.com"},
		},
		{
			name:           "single defaults ecosystem",
			allowed:        []string{"defaults"},
			expectContains: []string{"json-schema.org", "archive.ubuntu.com", "ocsp.digicert.com"},
		},
		{
			name:           "defaults + github ecosystems",
			allowed:        []string{"defaults", "github"},
			expectContains: []string{"json-schema.org", "github.githubassets.com", "*.githubusercontent.com", "lfs.github.com"},
		},
		{
			name:              "defaults + github + python ecosystems",
			allowed:           []string{"defaults", "github", "python"},
			expectContains:    []string{"json-schema.org", "github.githubassets.com", "pypi.org", "files.pythonhosted.org"},
			expectNotContains: []string{"registry.npmjs.org"}, // node ecosystem not included
		},
		{
			name:           "defaults + node + containers",
			allowed:        []string{"defaults", "node", "containers"},
			expectContains: []string{"json-schema.org", "registry.npmjs.org", "ghcr.io", "registry.hub.docker.com"},
		},
		{
			name:           "fonts ecosystem",
			allowed:        []string{"fonts"},
			expectContains: []string{"fonts.googleapis.com", "fonts.gstatic.com"},
		},
		{
			name:           "chrome ecosystem",
			allowed:        []string{"chrome"},
			expectContains: []string{"*.google.com", "*.googleapis.com", "*.gvt1.com"},
		},
		{
			name:           "deno ecosystem",
			allowed:        []string{"deno"},
			expectContains: []string{"deno.land", "jsr.io", "googleapis.deno.dev", "fresh.deno.dev"},
		},
		{
			name:           "node-cdns ecosystem",
			allowed:        []string{"node-cdns"},
			expectContains: []string{"cdn.jsdelivr.net"},
		},
		{
			name:           "node + node-cdns ecosystems",
			allowed:        []string{"node", "node-cdns"},
			expectContains: []string{"registry.npmjs.org", "cdn.jsdelivr.net"},
		},
		{
			name:              "single literal domain",
			allowed:           []string{"example.com"},
			expectContains:    []string{"example.com"},
			expectNotContains: []string{"json-schema.org", "github.com"},
		},
		{
			name:           "literal domain + ecosystem",
			allowed:        []string{"example.com", "github"},
			expectContains: []string{"example.com", "github.githubassets.com", "*.githubusercontent.com"},
		},
		{
			name:           "multiple literal domains",
			allowed:        []string{"example.com", "test.org", "api.custom.io"},
			expectContains: []string{"example.com", "test.org", "api.custom.io"},
		},
		{
			name:        "empty allowed list (deny all)",
			allowed:     []string{},
			expectEmpty: true,
		},
		{
			name:           "go + rust + java ecosystems",
			allowed:        []string{"go", "rust", "java"},
			expectContains: []string{"proxy.golang.org", "crates.io", "repo.maven.apache.org"},
		},
		{
			name:           "mixed ecosystems and literals",
			allowed:        []string{"defaults", "github", "custom.domain.com", "python", "api.test.io"},
			expectContains: []string{"json-schema.org", "github.githubassets.com", "custom.domain.com", "pypi.org", "api.test.io"},
		},
		{
			name:              "overlapping ecosystems (defaults already contains some basics)",
			allowed:           []string{"defaults", "linux-distros"},
			expectContains:    []string{"json-schema.org", "archive.ubuntu.com", "deb.debian.org"},
			expectNotContains: []string{"github.githubassets.com"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var network *NetworkPermissions
			if tt.allowed != nil {
				network = &NetworkPermissions{
					Allowed: tt.allowed,
				}
			}

			domains := GetAllowedDomains(network)

			if tt.expectEmpty {
				if len(domains) != 0 {
					t.Errorf("Expected empty domain list, got %d domains", len(domains))
				}
				return
			}

			// Check that expected domains are present
			for _, expectedDomain := range tt.expectContains {
				found := slices.Contains(domains, expectedDomain)
				if !found {
					t.Errorf("Expected domain '%s' not found in result. Got: %v", expectedDomain, domains)
				}
			}

			// Check that unexpected domains are NOT present
			for _, unexpectedDomain := range tt.expectNotContains {
				if slices.Contains(domains, unexpectedDomain) {
					t.Errorf("Domain '%s' should not be in result, but was found", unexpectedDomain)
				}
			}

			t.Logf("Test '%s': Got %d domains", tt.name, len(domains))
		})
	}
}

// TestGetAllowedDomains_DeduplicationAcrossEcosystems tests that domains are deduplicated
// even when they appear in multiple ecosystems
func TestGetAllowedDomains_DeduplicationAcrossEcosystems(t *testing.T) {
	// Some domains might theoretically appear in multiple ecosystems
	// The function should deduplicate them
	network := &NetworkPermissions{
		Allowed: []string{
			"defaults",
			"github",
			"python",
			"node",
		},
	}

	domains := GetAllowedDomains(network)

	// Check for duplicates
	seen := make(map[string]bool)
	for _, domain := range domains {
		if seen[domain] {
			t.Errorf("Duplicate domain found: %s", domain)
		}
		seen[domain] = true
	}

	// Verify we got a reasonable number of unique domains
	if len(domains) < 10 {
		t.Errorf("Expected at least 10 unique domains from 4 ecosystems, got %d", len(domains))
	}

	t.Logf("Total unique domains from [defaults, github, python, node]: %d", len(domains))
}

// TestGetAllowedDomains_SortingConsistency tests that the output is always sorted
func TestGetAllowedDomains_SortingConsistency(t *testing.T) {
	tests := []struct {
		name    string
		allowed []string
	}{
		{
			name:    "single ecosystem",
			allowed: []string{"defaults"},
		},
		{
			name:    "multiple ecosystems",
			allowed: []string{"github", "defaults", "python"},
		},
		{
			name:    "mixed literals and ecosystems",
			allowed: []string{"zzz.com", "aaa.com", "defaults", "github"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			network := &NetworkPermissions{
				Allowed: tt.allowed,
			}

			domains := GetAllowedDomains(network)

			// Verify sorting
			for i := 1; i < len(domains); i++ {
				if domains[i-1] > domains[i] {
					t.Errorf("Domains not sorted: %s comes before %s", domains[i-1], domains[i])
				}
			}

			t.Logf("Test '%s': All %d domains are sorted", tt.name, len(domains))
		})
	}
}

// TestNetworkPermissions_EdgeCases tests edge cases in network configuration
func TestNetworkPermissions_EdgeCases(t *testing.T) {
	tests := []struct {
		name           string
		network        *NetworkPermissions
		expectCount    int
		expectContains []string
	}{
		{
			name: "wildcard domain with ecosystem",
			network: &NetworkPermissions{
				Allowed: []string{"*.example.com", "defaults"},
			},
			expectContains: []string{"*.example.com", "json-schema.org"},
		},
		{
			name: "duplicate ecosystems in allowed list",
			network: &NetworkPermissions{
				Allowed: []string{"defaults", "github", "defaults", "github"},
			},
			// Should deduplicate - each ecosystem domain appears only once
			expectContains: []string{"json-schema.org", "github.githubassets.com"},
		},
		{
			name: "unknown ecosystem identifier treated as literal",
			network: &NetworkPermissions{
				Allowed: []string{"unknown-ecosystem"},
			},
			expectContains: []string{"unknown-ecosystem"},
		},
		{
			name: "mixed case sensitivity",
			network: &NetworkPermissions{
				Allowed: []string{"Example.COM", "test.ORG"},
			},
			expectContains: []string{"Example.COM", "test.ORG"}, // Preserved as-is
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			domains := GetAllowedDomains(tt.network)

			if tt.expectCount > 0 && len(domains) != tt.expectCount {
				t.Errorf("Expected %d domains, got %d", tt.expectCount, len(domains))
			}

			for _, expectedDomain := range tt.expectContains {
				found := slices.Contains(domains, expectedDomain)
				if !found {
					t.Errorf("Expected domain '%s' not found in result: %v", expectedDomain, domains)
				}
			}

			t.Logf("Test '%s': Got %d domains", tt.name, len(domains))
		})
	}
}

// TestGetDomainsFromRuntimes tests the runtime-to-ecosystem domain mapping
func TestGetDomainsFromRuntimes(t *testing.T) {
	tests := []struct {
		name           string
		runtimes       map[string]any
		expectContains []string // Domains that must be in the result
		expectEmpty    bool
	}{
		{
			name:        "nil runtimes returns empty",
			runtimes:    nil,
			expectEmpty: true,
		},
		{
			name:        "empty runtimes returns empty",
			runtimes:    map[string]any{},
			expectEmpty: true,
		},
		{
			name: "go runtime adds go ecosystem domains",
			runtimes: map[string]any{
				"go": map[string]any{"version": "1.22"},
			},
			expectContains: []string{"proxy.golang.org", "sum.golang.org", "go.dev"},
		},
		{
			name: "node runtime adds node ecosystem domains",
			runtimes: map[string]any{
				"node": map[string]any{"version": "20"},
			},
			expectContains: []string{"registry.npmjs.org", "nodejs.org", "yarnpkg.com"},
		},
		{
			name: "python runtime adds python ecosystem domains",
			runtimes: map[string]any{
				"python": map[string]any{"version": "3.11"},
			},
			expectContains: []string{"pypi.org", "files.pythonhosted.org"},
		},
		{
			name: "multiple runtimes add all ecosystem domains",
			runtimes: map[string]any{
				"go":   map[string]any{"version": "1.22"},
				"node": map[string]any{"version": "20"},
			},
			expectContains: []string{"proxy.golang.org", "registry.npmjs.org"},
		},
		{
			name: "bun maps to node ecosystem",
			runtimes: map[string]any{
				"bun": map[string]any{"version": "1.0"},
			},
			expectContains: []string{"bun.sh", "registry.npmjs.org"},
		},
		{
			name: "deno maps to node ecosystem",
			runtimes: map[string]any{
				"deno": map[string]any{"version": "1.40"},
			},
			expectContains: []string{"deno.land", "registry.npmjs.org"},
		},
		{
			name: "uv maps to python ecosystem",
			runtimes: map[string]any{
				"uv": map[string]any{},
			},
			expectContains: []string{"pypi.org", "files.pythonhosted.org"},
		},
		{
			name: "java runtime adds java ecosystem domains",
			runtimes: map[string]any{
				"java": map[string]any{"version": "21"},
			},
			expectContains: []string{"repo.maven.apache.org", "gradle.org"},
		},
		{
			name: "ruby runtime adds ruby ecosystem domains",
			runtimes: map[string]any{
				"ruby": map[string]any{"version": "3.2"},
			},
			expectContains: []string{"rubygems.org", "api.rubygems.org"},
		},
		{
			name: "dotnet runtime adds dotnet ecosystem domains",
			runtimes: map[string]any{
				"dotnet": map[string]any{"version": "8.0"},
			},
			expectContains: []string{"nuget.org", "api.nuget.org"},
		},
		{
			name: "haskell runtime adds haskell ecosystem domains",
			runtimes: map[string]any{
				"haskell": map[string]any{"version": "9.4"},
			},
			expectContains: []string{"haskell.org", "downloads.haskell.org"},
		},
		{
			name: "unknown runtime is ignored",
			runtimes: map[string]any{
				"unknown": map[string]any{"version": "1.0"},
			},
			expectEmpty: true,
		},
		{
			name: "elixir runtime adds elixir ecosystem domains",
			runtimes: map[string]any{
				"elixir": map[string]any{"version": "1.15"},
			},
			expectContains: []string{"hex.pm", "repo.hex.pm"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			domains := getDomainsFromRuntimes(tt.runtimes)

			if tt.expectEmpty {
				if len(domains) != 0 {
					t.Errorf("Expected empty result, got %d domains: %v", len(domains), domains)
				}
				return
			}

			for _, expected := range tt.expectContains {
				found := slices.Contains(domains, expected)
				if !found {
					t.Errorf("Expected domain '%s' not found in result: %v", expected, domains)
				}
			}

			t.Logf("Test '%s': Got %d domains", tt.name, len(domains))
		})
	}
}

// TestGetCopilotAllowedDomainsWithToolsAndRuntimes tests explicit Copilot domain-set opt-in with runtimes
func TestGetCopilotAllowedDomainsWithToolsAndRuntimes(t *testing.T) {
	t.Run("includes runtime ecosystem domains", func(t *testing.T) {
		network := &NetworkPermissions{
			Allowed: []string{"defaults", "copilot"},
		}
		runtimes := map[string]any{
			"go": map[string]any{"version": "1.22"},
		}

		result := GetAllowedDomainsForEngine(constants.CopilotEngine, network, nil, runtimes)

		// Should contain explicitly requested Copilot domain set
		if !strings.Contains(result, "api.githubcopilot.com") {
			t.Error("Expected api.githubcopilot.com in result")
		}
		// Should contain Go ecosystem domains
		if !strings.Contains(result, "proxy.golang.org") {
			t.Error("Expected proxy.golang.org from go runtime in result")
		}
	})

	t.Run("combines network permissions, tools, and runtimes", func(t *testing.T) {
		network := &NetworkPermissions{
			Allowed: []string{"copilot", "custom.example.com"},
		}
		tools := map[string]any{
			"tavily": map[string]any{
				"type": "http",
				"url":  "https://mcp.tavily.com/mcp/",
			},
		}
		runtimes := map[string]any{
			"node": map[string]any{"version": "20"},
		}

		result := GetAllowedDomainsForEngine(constants.CopilotEngine, network, tools, runtimes)

		// Should contain explicitly requested Copilot domain set
		if !strings.Contains(result, "api.githubcopilot.com") {
			t.Error("Expected api.githubcopilot.com in result")
		}
		// Should contain network allowed domain
		if !strings.Contains(result, "custom.example.com") {
			t.Error("Expected custom.example.com from network permissions in result")
		}
		// Should contain HTTP MCP domain
		if !strings.Contains(result, "mcp.tavily.com") {
			t.Error("Expected mcp.tavily.com from tools in result")
		}
		// Should contain Node ecosystem domains
		if !strings.Contains(result, "registry.npmjs.org") {
			t.Error("Expected registry.npmjs.org from node runtime in result")
		}
	})

	t.Run("nil network does not include engine domain set", func(t *testing.T) {
		result := GetAllowedDomainsForEngine(constants.CopilotEngine, nil, nil, nil)

		if strings.Contains(result, "api.githubcopilot.com") {
			t.Error("Did not expect api.githubcopilot.com without explicit copilot domain-set opt-in")
		}
	})
}

// TestGetClaudeAllowedDomainsWithToolsAndRuntimes tests explicit Claude domain-set opt-in with runtimes
func TestGetClaudeAllowedDomainsWithToolsAndRuntimes(t *testing.T) {
	t.Run("includes runtime ecosystem domains", func(t *testing.T) {
		network := &NetworkPermissions{
			Allowed: []string{"claude"},
		}
		runtimes := map[string]any{
			"python": map[string]any{"version": "3.11"},
		}

		result := GetAllowedDomainsForEngine(constants.ClaudeEngine, network, nil, runtimes)

		// Should contain explicitly requested Claude domain set
		if !strings.Contains(result, "api.anthropic.com") {
			t.Error("Expected api.anthropic.com in result")
		}
		// Should contain Python ecosystem domains
		if !strings.Contains(result, "pypi.org") {
			t.Error("Expected pypi.org from python runtime in result")
		}
	})
}

// TestGetCodexAllowedDomainsWithToolsAndRuntimes tests explicit Codex domain-set opt-in with runtimes
func TestGetCodexAllowedDomainsWithToolsAndRuntimes(t *testing.T) {
	t.Run("includes runtime ecosystem domains", func(t *testing.T) {
		network := &NetworkPermissions{
			Allowed: []string{"codex"},
		}
		runtimes := map[string]any{
			"java": map[string]any{"version": "21"},
		}

		result := GetAllowedDomainsForEngine(constants.CodexEngine, network, nil, runtimes)

		// Should contain explicitly requested Codex domain set
		if !strings.Contains(result, "api.openai.com") {
			t.Error("Expected api.openai.com in result")
		}
		// Should contain Java ecosystem domains
		if !strings.Contains(result, "repo.maven.apache.org") {
			t.Error("Expected repo.maven.apache.org from java runtime in result")
		}
	})
}

// TestExpandAllowedDomains tests the expandAllowedDomains function
func TestExpandAllowedDomains(t *testing.T) {
	t.Run("plain domains are returned as-is", func(t *testing.T) {
		result := expandAllowedDomains([]string{"example.com", "test.org"})
		if !strings.Contains(strings.Join(result, ","), "example.com") {
			t.Error("Expected example.com in result")
		}
		if !strings.Contains(strings.Join(result, ","), "test.org") {
			t.Error("Expected test.org in result")
		}
	})

	t.Run("ecosystem identifiers are expanded", func(t *testing.T) {
		result := expandAllowedDomains([]string{"python"})
		joined := strings.Join(result, ",")
		if !strings.Contains(joined, "pypi.org") {
			t.Error("Expected pypi.org from python ecosystem in result")
		}
	})

	t.Run("dev-tools ecosystem is expanded", func(t *testing.T) {
		result := expandAllowedDomains([]string{"dev-tools"})
		joined := strings.Join(result, ",")
		if !strings.Contains(joined, "codecov.io") {
			t.Error("Expected codecov.io from dev-tools ecosystem in result")
		}
		if !strings.Contains(joined, "snyk.io") {
			t.Error("Expected snyk.io from dev-tools ecosystem in result")
		}
	})

	t.Run("mixed plain domains and ecosystem identifiers", func(t *testing.T) {
		result := expandAllowedDomains([]string{"example.com", "python"})
		joined := strings.Join(result, ",")
		if !strings.Contains(joined, "example.com") {
			t.Error("Expected example.com in result")
		}
		if !strings.Contains(joined, "pypi.org") {
			t.Error("Expected pypi.org from python ecosystem in result")
		}
	})

	t.Run("empty input returns empty result", func(t *testing.T) {
		result := expandAllowedDomains([]string{})
		if len(result) != 0 {
			t.Errorf("Expected empty result, got %v", result)
		}
	})
}

// TestComputeExpandedAllowedDomainsForSanitization tests that allowed-domains are unioned with
// the engine/network base set and always includes localhost and github.com
func TestComputeExpandedAllowedDomainsForSanitization(t *testing.T) {
	compiler := NewCompiler()

	t.Run("unions with explicit engine domain set", func(t *testing.T) {
		data := &WorkflowData{
			EngineConfig: &EngineConfig{ID: "copilot"},
			NetworkPermissions: &NetworkPermissions{
				Allowed: []string{"copilot", "example.com"},
			},
			SafeOutputs: &SafeOutputsConfig{
				AllowedDomains: []string{"extra-domain.com"},
			},
		}
		result, err := compiler.computeExpandedAllowedDomainsForSanitization(data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(result, "extra-domain.com") {
			t.Error("Expected extra-domain.com in result")
		}
		if !strings.Contains(result, "example.com") {
			t.Errorf("Expected network domain example.com in result, got: %s", result)
		}
		if !strings.Contains(result, "api.github.com") {
			t.Error("Expected explicitly requested Copilot domain set api.github.com in result")
		}
	})

	t.Run("always includes localhost", func(t *testing.T) {
		data := &WorkflowData{
			EngineConfig: &EngineConfig{ID: "copilot"},
			SafeOutputs: &SafeOutputsConfig{
				AllowedDomains: []string{"extra-domain.com"},
			},
		}
		result, err := compiler.computeExpandedAllowedDomainsForSanitization(data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(result, "localhost") {
			t.Error("Expected localhost to always be in allowed-domains result")
		}
	})

	t.Run("always includes github.com", func(t *testing.T) {
		data := &WorkflowData{
			EngineConfig: &EngineConfig{ID: "codex"},
			SafeOutputs: &SafeOutputsConfig{
				AllowedDomains: []string{"extra-domain.com"},
			},
		}
		result, err := compiler.computeExpandedAllowedDomainsForSanitization(data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(result, "github.com") {
			t.Error("Expected github.com to always be in allowed-domains result")
		}
	})

	t.Run("supports ecosystem identifiers", func(t *testing.T) {
		data := &WorkflowData{
			EngineConfig: &EngineConfig{ID: "copilot"},
			SafeOutputs: &SafeOutputsConfig{
				AllowedDomains: []string{"python", "dev-tools"},
			},
		}
		result, err := compiler.computeExpandedAllowedDomainsForSanitization(data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(result, "pypi.org") {
			t.Error("Expected pypi.org from python ecosystem in result")
		}
		if !strings.Contains(result, "codecov.io") {
			t.Error("Expected codecov.io from dev-tools ecosystem in result")
		}
	})
}

// TestDefaultSafeOutputsEcosystem tests that the default-safe-outputs compound ecosystem
// correctly expands to the union of defaults + dev-tools + github + local
func TestDefaultSafeOutputsEcosystem(t *testing.T) {
	result := expandAllowedDomains([]string{"default-safe-outputs"})
	joined := strings.Join(result, ",")

	// From defaults ecosystem
	defaultsSamples := []string{"json-schema.org", "archive.ubuntu.com", "ocsp.digicert.com"}
	// From dev-tools ecosystem
	devToolsSamples := []string{"codecov.io", "snyk.io", "shields.io"}
	// From github ecosystem
	githubSamples := []string{"github.com", "docs.github.com", "github.blog", "*.githubusercontent.com"}
	// From local ecosystem
	localSamples := []string{"localhost", "127.0.0.1", "::1"}

	for _, d := range append(append(append(defaultsSamples, devToolsSamples...), githubSamples...), localSamples...) {
		if !strings.Contains(joined, d) {
			t.Errorf("Expected domain %q from default-safe-outputs ecosystem in result", d)
		}
	}

	// Verify the size matches the union of all component ecosystems
	expectedDomains := make(map[string]bool)
	for _, component := range []string{"defaults", "dev-tools", "github", "local"} {
		for _, d := range getEcosystemDomains(component) {
			expectedDomains[d] = true
		}
	}
	if len(result) != len(expectedDomains) {
		t.Errorf("Expected %d unique domains in default-safe-outputs (union of components), got %d", len(expectedDomains), len(result))
	}
}

func TestGetAPITargetDomains(t *testing.T) {
	tests := []struct {
		name      string
		apiTarget string
		expected  []string
	}{
		{
			name:      "empty api-target returns nil",
			apiTarget: "",
			expected:  nil,
		},
		{
			name:      "GHES api-target with api. prefix returns both api and base domains",
			apiTarget: "api.acme.ghe.com",
			expected:  []string{"api.acme.ghe.com", "acme.ghe.com"},
		},
		{
			name:      "GHES api-target custom domain",
			apiTarget: "api.contoso-aw.ghe.com",
			expected:  []string{"api.contoso-aw.ghe.com", "contoso-aw.ghe.com"},
		},
		{
			name:      "enterprise githubcopilot.com api-target",
			apiTarget: "api.enterprise.githubcopilot.com",
			expected:  []string{"api.enterprise.githubcopilot.com", "enterprise.githubcopilot.com"},
		},
		{
			name:      "non-api. prefix hostname returns only itself",
			apiTarget: "copilot.example.com",
			expected:  []string{"copilot.example.com"},
		},
		{
			name:      "single label hostname (no dot) returns only itself",
			apiTarget: "localhost",
			expected:  []string{"localhost"},
		},
		{
			name:      "two-label hostname does not add TLD alone",
			apiTarget: "example.com",
			expected:  []string{"example.com"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetAPITargetDomains(tt.apiTarget)
			if tt.expected == nil {
				if result != nil {
					t.Errorf("Expected nil, got %v", result)
				}
				return
			}
			if len(result) != len(tt.expected) {
				t.Errorf("Expected %d domains %v, got %d domains %v", len(tt.expected), tt.expected, len(result), result)
				return
			}
			for _, expected := range tt.expected {
				if !slices.Contains(result, expected) {
					t.Errorf("Expected domain %q not found in result %v", expected, result)
				}
			}
		})
	}
}

func TestMergeAPITargetDomains(t *testing.T) {
	tests := []struct {
		name       string
		domainsStr string
		apiTarget  string
		wantIn     []string
		wantNotIn  []string
	}{
		{
			name:       "empty api-target leaves domains unchanged",
			domainsStr: "github.com,api.github.com",
			apiTarget:  "",
			wantIn:     []string{"github.com", "api.github.com"},
		},
		{
			name:       "GHES api-target adds both api and base domains",
			domainsStr: "github.com,api.github.com",
			apiTarget:  "api.acme.ghe.com",
			wantIn:     []string{"github.com", "api.github.com", "api.acme.ghe.com", "acme.ghe.com"},
		},
		{
			name:       "result is sorted and deduplicated",
			domainsStr: "api.acme.ghe.com,github.com",
			apiTarget:  "api.acme.ghe.com",
			wantIn:     []string{"api.acme.ghe.com", "acme.ghe.com", "github.com"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mergeAPITargetDomains(tt.domainsStr, tt.apiTarget)
			domains := strings.Split(result, ",")
			domainSet := make(map[string]struct{})
			for _, d := range domains {
				domainSet[d] = struct{}{}
			}
			for _, want := range tt.wantIn {
				if _, ok := domainSet[want]; !ok {
					t.Errorf("Expected domain %q in result %q, but not found", want, result)
				}
			}
			for _, notWant := range tt.wantNotIn {
				if _, ok := domainSet[notWant]; ok {
					t.Errorf("Did not expect domain %q in result %q", notWant, result)
				}
			}
		})
	}
}

func TestExtractProviderFromModel(t *testing.T) {
	t.Run("standard provider/model format", func(t *testing.T) {
		provider, err := extractProviderFromModel("anthropic/claude-sonnet-4-20250514")
		require.NoError(t, err)
		assert.Equal(t, "anthropic", provider)

		provider, err = extractProviderFromModel("openai/gpt-4.1")
		require.NoError(t, err)
		assert.Equal(t, "openai", provider)

		provider, err = extractProviderFromModel("google/gemini-2.5-pro")
		require.NoError(t, err)
		assert.Equal(t, "google", provider)
	})

	t.Run("empty model returns empty provider", func(t *testing.T) {
		provider, err := extractProviderFromModel("")
		require.NoError(t, err)
		assert.Empty(t, provider)
	})

	t.Run("no slash returns empty provider", func(t *testing.T) {
		provider, err := extractProviderFromModel("claude-sonnet-4-20250514")
		require.NoError(t, err)
		assert.Empty(t, provider)
	})

	t.Run("case insensitive provider", func(t *testing.T) {
		provider, err := extractProviderFromModel("OpenAI/gpt-4.1")
		require.NoError(t, err)
		assert.Equal(t, "openai", provider)
	})

	t.Run("leading slash returns error", func(t *testing.T) {
		_, err := extractProviderFromModel("/gpt-4.1")
		require.Error(t, err, "Leading slash (empty provider prefix) must return an error")
		require.ErrorContains(t, err, "provider prefix is empty")
	})
}
