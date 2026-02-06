//go:build !integration

package workflow

import (
	"strings"
	"testing"
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
		"api.business.githubcopilot.com",
		"api.enterprise.githubcopilot.com",
		"api.github.com",
		"api.githubcopilot.com",
		"api.individual.githubcopilot.com",
		"github.com",
		"host.docker.internal",
		"raw.githubusercontent.com",
		"registry.npmjs.org",
		"telemetry.enterprise.githubcopilot.com",
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

	// Verify the count matches (no extra domains)
	if len(CopilotDefaultDomains) != len(expectedDomains) {
		t.Errorf("CopilotDefaultDomains has %d domains, expected %d", len(CopilotDefaultDomains), len(expectedDomains))
	}
}

func TestCodexDefaultDomains(t *testing.T) {
	// Verify that expected Codex domains are present
	expectedDomains := []string{
		"172.30.0.1", // AWF gateway IP - Codex resolves host.docker.internal to this IP
		"api.openai.com",
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

func TestGetCodexAllowedDomains(t *testing.T) {
	t.Run("nil network permissions returns only defaults", func(t *testing.T) {
		result := GetCodexAllowedDomains(nil)
		// Should contain default Codex domains, sorted
		if result != "172.30.0.1,api.openai.com,host.docker.internal,openai.com" {
			t.Errorf("Expected '172.30.0.1,api.openai.com,host.docker.internal,openai.com', got %q", result)
		}
	})

	t.Run("with network permissions merges domains", func(t *testing.T) {
		network := &NetworkPermissions{
			Allowed: []string{"example.com"},
		}
		result := GetCodexAllowedDomains(network)
		// Should contain both default Codex domains and user-specified domain
		if result != "172.30.0.1,api.openai.com,example.com,host.docker.internal,openai.com" {
			t.Errorf("Expected '172.30.0.1,api.openai.com,example.com,host.docker.internal,openai.com', got %q", result)
		}
	})

	t.Run("deduplicates domains", func(t *testing.T) {
		network := &NetworkPermissions{
			Allowed: []string{"api.openai.com", "example.com"},
		}
		result := GetCodexAllowedDomains(network)
		// api.openai.com should not appear twice
		if result != "172.30.0.1,api.openai.com,example.com,host.docker.internal,openai.com" {
			t.Errorf("Expected '172.30.0.1,api.openai.com,example.com,host.docker.internal,openai.com', got %q", result)
		}
	})

	t.Run("empty allowed list returns only defaults", func(t *testing.T) {
		network := &NetworkPermissions{
			Allowed: []string{},
		}
		result := GetCodexAllowedDomains(network)
		// Empty allowed list should still return Codex defaults
		if result != "172.30.0.1,api.openai.com,host.docker.internal,openai.com" {
			t.Errorf("Expected '172.30.0.1,api.openai.com,host.docker.internal,openai.com', got %q", result)
		}
	})
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
		"registry.npmjs.org",
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

	// Verify minimum count (Claude has many more domains than the critical ones)
	if len(ClaudeDefaultDomains) < len(criticalDomains) {
		t.Errorf("ClaudeDefaultDomains has %d domains, expected at least %d", len(ClaudeDefaultDomains), len(criticalDomains))
	}
}

func TestGetClaudeAllowedDomains(t *testing.T) {
	t.Run("returns Claude defaults when no network permissions", func(t *testing.T) {
		result := GetClaudeAllowedDomains(nil)
		// Should contain Claude default domains
		if !strings.Contains(result, "api.anthropic.com") {
			t.Error("Expected api.anthropic.com in result")
		}
		if !strings.Contains(result, "anthropic.com") {
			t.Error("Expected anthropic.com in result")
		}
	})

	t.Run("merges network permissions with Claude defaults", func(t *testing.T) {
		network := &NetworkPermissions{
			Allowed: []string{"custom.example.com"},
		}
		result := GetClaudeAllowedDomains(network)
		// Should contain both Claude defaults and custom domain
		if !strings.Contains(result, "api.anthropic.com") {
			t.Error("Expected api.anthropic.com in result")
		}
		if !strings.Contains(result, "custom.example.com") {
			t.Error("Expected custom.example.com in result")
		}
	})

	t.Run("domains are sorted", func(t *testing.T) {
		result := GetClaudeAllowedDomains(nil)
		// Should be comma-separated and sorted
		domains := strings.Split(result, ",")
		for i := 1; i < len(domains); i++ {
			if domains[i-1] > domains[i] {
				t.Errorf("Domains not sorted: %s > %s", domains[i-1], domains[i])
				break
			}
		}
	})
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
				found := false
				for _, domain := range domains {
					if domain == expectedDomain {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Expected domain '%s' not found in result. Got: %v", expectedDomain, domains)
				}
			}

			// Check that unexpected domains are NOT present
			for _, unexpectedDomain := range tt.expectNotContains {
				for _, domain := range domains {
					if domain == unexpectedDomain {
						t.Errorf("Domain '%s' should not be in result, but was found", unexpectedDomain)
						break
					}
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
				found := false
				for _, domain := range domains {
					if domain == expectedDomain {
						found = true
						break
					}
				}
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
			name: "elixir has no ecosystem mapping",
			runtimes: map[string]any{
				"elixir": map[string]any{"version": "1.15"},
			},
			expectEmpty: true,
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
				found := false
				for _, domain := range domains {
					if domain == expected {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Expected domain '%s' not found in result: %v", expected, domains)
				}
			}

			t.Logf("Test '%s': Got %d domains", tt.name, len(domains))
		})
	}
}

// TestGetCopilotAllowedDomainsWithToolsAndRuntimes tests the full integration of runtimes with Copilot domains
func TestGetCopilotAllowedDomainsWithToolsAndRuntimes(t *testing.T) {
	t.Run("includes runtime ecosystem domains", func(t *testing.T) {
		network := &NetworkPermissions{
			Allowed: []string{"defaults"},
		}
		runtimes := map[string]any{
			"go": map[string]any{"version": "1.22"},
		}

		result := GetCopilotAllowedDomainsWithToolsAndRuntimes(network, nil, runtimes)

		// Should contain Copilot defaults
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
			Allowed: []string{"custom.example.com"},
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

		result := GetCopilotAllowedDomainsWithToolsAndRuntimes(network, tools, runtimes)

		// Should contain Copilot defaults
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

	t.Run("nil runtimes works correctly", func(t *testing.T) {
		result := GetCopilotAllowedDomainsWithToolsAndRuntimes(nil, nil, nil)

		// Should still contain Copilot defaults
		if !strings.Contains(result, "api.githubcopilot.com") {
			t.Error("Expected api.githubcopilot.com in result")
		}
	})
}

// TestGetClaudeAllowedDomainsWithToolsAndRuntimes tests the full integration of runtimes with Claude domains
func TestGetClaudeAllowedDomainsWithToolsAndRuntimes(t *testing.T) {
	t.Run("includes runtime ecosystem domains", func(t *testing.T) {
		runtimes := map[string]any{
			"python": map[string]any{"version": "3.11"},
		}

		result := GetClaudeAllowedDomainsWithToolsAndRuntimes(nil, nil, runtimes)

		// Should contain Claude defaults
		if !strings.Contains(result, "api.anthropic.com") {
			t.Error("Expected api.anthropic.com in result")
		}
		// Should contain Python ecosystem domains
		if !strings.Contains(result, "pypi.org") {
			t.Error("Expected pypi.org from python runtime in result")
		}
	})
}

// TestGetCodexAllowedDomainsWithToolsAndRuntimes tests the full integration of runtimes with Codex domains
func TestGetCodexAllowedDomainsWithToolsAndRuntimes(t *testing.T) {
	t.Run("includes runtime ecosystem domains", func(t *testing.T) {
		runtimes := map[string]any{
			"java": map[string]any{"version": "21"},
		}

		result := GetCodexAllowedDomainsWithToolsAndRuntimes(nil, nil, runtimes)

		// Should contain Codex defaults
		if !strings.Contains(result, "api.openai.com") {
			t.Error("Expected api.openai.com in result")
		}
		// Should contain Java ecosystem domains
		if !strings.Contains(result, "repo.maven.apache.org") {
			t.Error("Expected repo.maven.apache.org from java runtime in result")
		}
	})
}
