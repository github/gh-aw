//go:build !integration

package stringutil

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExtractDomainFromURL(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		expected string
	}{
		// Test cases from pkg/cli/access_log_test.go
		{
			name:     "HTTP URL with path",
			url:      "http://example.com/path",
			expected: "example.com",
		},
		{
			name:     "HTTPS URL with path",
			url:      "https://api.github.com/repos",
			expected: "api.github.com",
		},
		{
			name:     "domain with port",
			url:      "github.com:443",
			expected: "github.com",
		},
		{
			name:     "plain domain",
			url:      "malicious.site",
			expected: "malicious.site",
		},
		{
			name:     "HTTP URL with port and path",
			url:      "http://sub.domain.com:8080/path",
			expected: "sub.domain.com",
		},
		// Test cases from pkg/workflow/http_mcp_domains_test.go
		{
			name:     "HTTPS URL with MCP path",
			url:      "https://mcp.tavily.com/mcp/",
			expected: "mcp.tavily.com",
		},
		{
			name:     "HTTP URL with port and API path",
			url:      "http://api.example.com:8080/path",
			expected: "api.example.com",
		},
		{
			name:     "domain only",
			url:      "mcp.example.com",
			expected: "mcp.example.com",
		},
		{
			name:     "HTTPS URL with port",
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
		// Additional edge cases
		{
			name:     "domain with trailing slash",
			url:      "example.com/",
			expected: "example.com",
		},
		{
			name:     "HTTPS URL with query parameters",
			url:      "https://api.example.com/search?q=test",
			expected: "api.example.com",
		},
		{
			name:     "HTTP URL with fragment",
			url:      "http://docs.example.com/page#section",
			expected: "docs.example.com",
		},
		{
			name:     "domain with multiple ports (CONNECT format)",
			url:      "proxy.example.com:8080",
			expected: "proxy.example.com",
		},
		{
			name:     "IPv4 address with port",
			url:      "http://192.168.1.1:8080/path",
			expected: "192.168.1.1",
		},
		{
			name:     "localhost without port",
			url:      "http://localhost/api",
			expected: "localhost",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExtractDomainFromURL(tt.url)
			assert.Equal(t, tt.expected, result, "Extracted domain should match expected")
		})
	}
}

func BenchmarkExtractDomainFromURL(b *testing.B) {
	testCases := []string{
		"https://api.github.com/repos",
		"http://example.com:8080/path",
		"mcp.example.com",
		"github.com:443",
	}

	for _, tc := range testCases {
		b.Run(tc, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				_ = ExtractDomainFromURL(tc)
			}
		})
	}
}
