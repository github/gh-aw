//go:build !integration

package workflow

import (
	"sort"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestExtractHTTPMCPDomains tests extraction of domain names from HTTP MCP server URLs
func TestExtractHTTPMCPDomains(t *testing.T) {
	tests := []struct {
		name     string
		tools    map[string]any
		expected []string
	}{
		{
			name: "single HTTP MCP server with type",
			tools: map[string]any{
				"tavily": map[string]any{
					"type": "http",
					"url":  "https://mcp.tavily.com/mcp/",
				},
			},
			expected: []string{"mcp.tavily.com"},
		},
		{
			name: "single HTTP MCP server without type (inferred from URL)",
			tools: map[string]any{
				"example": map[string]any{
					"url": "https://api.example.com/mcp",
				},
			},
			expected: []string{"api.example.com"},
		},
		{
			name: "multiple HTTP MCP servers",
			tools: map[string]any{
				"tavily": map[string]any{
					"type": "http",
					"url":  "https://mcp.tavily.com/mcp/",
				},
				"custom": map[string]any{
					"url": "https://custom.api.com:8080/path",
				},
			},
			expected: []string{"mcp.tavily.com", "custom.api.com"},
		},
		{
			name: "mixed tools with stdio and HTTP MCP",
			tools: map[string]any{
				"github": map[string]any{
					"mode": "remote",
				},
				"tavily": map[string]any{
					"type": "http",
					"url":  "https://mcp.tavily.com/mcp/",
				},
				"playwright": map[string]any{},
			},
			expected: []string{constants.GitHubCopilotMCPDomain, "mcp.tavily.com"},
		},
		{
			name: "github MCP in remote mode",
			tools: map[string]any{
				"github": map[string]any{
					"mode": "remote",
				},
			},
			expected: []string{constants.GitHubCopilotMCPDomain},
		},
		{
			name: "github MCP in local mode (no domain extraction)",
			tools: map[string]any{
				"github": map[string]any{
					"mode": "local",
				},
			},
			expected: []string{},
		},
		{
			name:     "no tools",
			tools:    map[string]any{},
			expected: []string{},
		},
		{
			name:     "nil tools",
			tools:    nil,
			expected: []string{},
		},
		{
			name: "stdio MCP server (should not extract)",
			tools: map[string]any{
				"custom": map[string]any{
					"type":    "stdio",
					"command": "node server.js",
				},
			},
			expected: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractHTTPMCPDomains(tt.tools)

			// Sort both slices for comparison
			sort.Strings(result)
			sort.Strings(tt.expected)

			assert.Equal(t, tt.expected, result, "Extracted domains should match expected")
		})
	}
}

// TestExtractDomainFromURL tests domain extraction from various URL formats
func TestExtractDomainFromURL(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		expected string
	}{
		{
			name:     "HTTPS URL with path",
			url:      "https://mcp.tavily.com/mcp/",
			expected: "mcp.tavily.com",
		},
		{
			name:     "HTTP URL with port and path",
			url:      "http://api.example.com:8080/path",
			expected: "api.example.com",
		},
		{
			name:     "domain only",
			url:      "mcp.example.com",
			expected: "mcp.example.com",
		},
		{
			name:     "URL with port",
			url:      "https://api.example.com:3000",
			expected: "api.example.com",
		},
		{
			name:     "URL with subdomain",
			url:      "https://api.mcp.example.com/v1/endpoint",
			expected: "api.mcp.example.com",
		},
		{
			name:     "localhost URL",
			url:      "http://localhost:8080/api",
			expected: "localhost",
		},
		{
			name:     "empty string",
			url:      "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := stringutil.ExtractDomainFromURL(tt.url)
			assert.Equal(t, tt.expected, result, "Extracted domain should match expected")
		})
	}
}

// TestGetCodexAllowedDomainsWithTools tests that HTTP MCP domains are included in Codex allowed domains
func TestGetCodexAllowedDomainsWithTools(t *testing.T) {
	network := &NetworkPermissions{
		Allowed: []string{"codex", "github"},
	}

	tools := map[string]any{
		"tavily": map[string]any{
			"type": "http",
			"url":  "https://mcp.tavily.com/mcp/",
		},
	}

	result := GetAllowedDomainsForEngine(constants.CodexEngine, network, tools, nil)

	// Should include explicitly requested Codex transport set, GitHub ecosystem, and Tavily domain
	require.Contains(t, result, "mcp.tavily.com", "Should include HTTP MCP domain")
	require.NotContains(t, result, "api.openai.com", "Codex transport set must not grant direct model API egress")
	require.Contains(t, result, "github.githubassets.com", "Should include GitHub ecosystem")
}

// TestGetCopilotAllowedDomainsWithTools tests that HTTP MCP domains are included in Copilot allowed domains
func TestGetCopilotAllowedDomainsWithTools(t *testing.T) {
	network := &NetworkPermissions{
		Allowed: []string{"copilot", "python"},
	}

	tools := map[string]any{
		"custom": map[string]any{
			"url": "https://api.custom.com/mcp",
		},
	}

	result := GetAllowedDomainsForEngine(constants.CopilotEngine, network, tools, nil)

	// Should include explicitly requested Copilot transport set, Python ecosystem, and custom HTTP MCP domain
	require.Contains(t, result, "api.custom.com", "Should include HTTP MCP domain")
	require.NotContains(t, result, "api.githubcopilot.com", "Copilot transport set must not grant direct model API egress")
	require.Contains(t, result, "pypi.org", "Should include Python ecosystem")
}

// TestGetClaudeAllowedDomainsWithTools tests that HTTP MCP domains are included in Claude allowed domains
func TestGetClaudeAllowedDomainsWithTools(t *testing.T) {
	network := &NetworkPermissions{
		Allowed: []string{"claude", "node"},
	}

	tools := map[string]any{
		"example": map[string]any{
			"type": "http",
			"url":  "https://mcp.example.org:443/api/v1",
		},
	}

	result := GetAllowedDomainsForEngine(constants.ClaudeEngine, network, tools, nil)

	// Should include explicitly requested Claude domain set, Node ecosystem, and example HTTP MCP domain
	require.Contains(t, result, "mcp.example.org", "Should include HTTP MCP domain")
	require.NotContains(t, result, "api.anthropic.com", "Static Claude domain set should exclude Anthropic's API")
	require.Contains(t, result, "registry.npmjs.org", "Should include Node ecosystem")
}

// TestExtractPlaywrightDomains tests extraction of Playwright ecosystem domains when Playwright tool is configured
func TestExtractPlaywrightDomains(t *testing.T) {
	tests := []struct {
		name     string
		tools    map[string]any
		expected []string
	}{
		{
			name: "playwright tool configured",
			tools: map[string]any{
				"playwright": map[string]any{},
			},
			expected: []string{"playwright.download.prss.microsoft.com", "cdn.playwright.dev"},
		},
		{
			name: "playwright tool with empty config",
			tools: map[string]any{
				"playwright": map[string]any{},
			},
			expected: []string{"playwright.download.prss.microsoft.com", "cdn.playwright.dev"},
		},
		{
			name: "playwright tool with null config",
			tools: map[string]any{
				"playwright": nil,
			},
			expected: []string{"playwright.download.prss.microsoft.com", "cdn.playwright.dev"},
		},
		{
			name: "no playwright tool",
			tools: map[string]any{
				"github": map[string]any{
					"mode": "local",
				},
			},
			expected: []string{},
		},
		{
			name:     "empty tools",
			tools:    map[string]any{},
			expected: []string{},
		},
		{
			name:     "nil tools",
			tools:    nil,
			expected: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractPlaywrightDomains(tt.tools)

			// Sort both slices for comparison
			sort.Strings(result)
			sort.Strings(tt.expected)

			assert.Equal(t, tt.expected, result, "Extracted Playwright domains should match expected")
		})
	}
}

// TestGetCopilotAllowedDomainsWithPlaywright tests that Playwright domains are automatically included for Copilot engine
func TestGetCopilotAllowedDomainsWithPlaywright(t *testing.T) {
	network := &NetworkPermissions{
		Allowed: []string{"copilot", "defaults"},
	}

	tools := map[string]any{
		"playwright": map[string]any{},
	}

	result := GetAllowedDomainsForEngine(constants.CopilotEngine, network, tools, nil)

	// Should include explicitly requested Copilot domain set and Playwright ecosystem domains
	require.Contains(t, result, "playwright.download.prss.microsoft.com", "Should include Playwright download domain")
	require.Contains(t, result, "cdn.playwright.dev", "Should include Playwright CDN domain")
	require.NotContains(t, result, "api.githubcopilot.com", "Copilot transport set must not grant direct model API egress")
}

// TestGetCodexAllowedDomainsWithPlaywright tests that Playwright domains are automatically included for Codex engine
func TestGetCodexAllowedDomainsWithPlaywright(t *testing.T) {
	network := &NetworkPermissions{
		Allowed: []string{"codex", "defaults"},
	}

	tools := map[string]any{
		"playwright": map[string]any{},
	}

	result := GetAllowedDomainsForEngine(constants.CodexEngine, network, tools, nil)

	// Should include explicitly requested Codex domain set and Playwright ecosystem domains
	require.Contains(t, result, "playwright.download.prss.microsoft.com", "Should include Playwright download domain")
	require.Contains(t, result, "cdn.playwright.dev", "Should include Playwright CDN domain")
	require.NotContains(t, result, "api.openai.com", "Codex transport set must not grant direct model API egress")
}
