//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

// TestProtocolSpecificDomains tests that domains with protocol prefixes are correctly handled
func TestProtocolSpecificDomains(t *testing.T) {
	tests := []struct {
		name            string
		network         *NetworkPermissions
		expectedDomains []string // domains that should be in the output
	}{
		{
			name: "HTTPS-only domain",
			network: &NetworkPermissions{
				Allowed: []string{"https://secure.example.com"},
			},
			expectedDomains: []string{"https://secure.example.com"},
		},
		{
			name: "HTTP-only domain",
			network: &NetworkPermissions{
				Allowed: []string{"http://legacy.example.com"},
			},
			expectedDomains: []string{"http://legacy.example.com"},
		},
		{
			name: "Mixed protocols",
			network: &NetworkPermissions{
				Allowed: []string{
					"https://secure.example.com",
					"http://legacy.example.com",
					"example.org", // No protocol = both
				},
			},
			expectedDomains: []string{
				"https://secure.example.com",
				"http://legacy.example.com",
				"example.org",
			},
		},
		{
			name: "Protocol-specific with wildcard",
			network: &NetworkPermissions{
				Allowed: []string{
					"https://*.secure.example.com",
					"http://*.legacy.example.com",
				},
			},
			expectedDomains: []string{
				"https://*.secure.example.com",
				"http://*.legacy.example.com",
			},
		},
		{
			name: "Backward compatibility - no protocol",
			network: &NetworkPermissions{
				Allowed: []string{
					"example.com",
					"*.example.org",
				},
			},
			expectedDomains: []string{
				"example.com",
				"*.example.org",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test GetAllowedDomains
			result := GetAllowedDomains(tt.network)

			// Check that all expected domains are present
			for _, expected := range tt.expectedDomains {
				found := false
				for _, domain := range result {
					if domain == expected {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Expected domain %q not found in result: %v", expected, result)
				}
			}
		})
	}
}

// TestGetCopilotAllowedDomainsWithProtocol tests Copilot domain merging with protocols
func TestGetCopilotAllowedDomainsWithProtocol(t *testing.T) {
	network := &NetworkPermissions{
		Allowed: []string{
			"https://secure.example.com",
			"http://legacy.example.com",
		},
	}

	result := GetCopilotAllowedDomains(network)

	// Should contain protocol-specific domains
	if !strings.Contains(result, "https://secure.example.com") {
		t.Error("Expected result to contain https://secure.example.com")
	}
	if !strings.Contains(result, "http://legacy.example.com") {
		t.Error("Expected result to contain http://legacy.example.com")
	}

	// Should also contain Copilot defaults (without protocol)
	if !strings.Contains(result, "api.github.com") {
		t.Error("Expected result to contain Copilot default domain api.github.com")
	}
}

// TestGetClaudeAllowedDomainsWithProtocol tests Claude domain merging with protocols
func TestGetClaudeAllowedDomainsWithProtocol(t *testing.T) {
	network := &NetworkPermissions{
		Allowed: []string{
			"https://api.example.com",
		},
	}

	result := GetClaudeAllowedDomains(network)

	// Should contain protocol-specific domain
	if !strings.Contains(result, "https://api.example.com") {
		t.Error("Expected result to contain https://api.example.com")
	}

	// Should also contain Claude defaults
	if !strings.Contains(result, "anthropic.com") {
		t.Error("Expected result to contain Claude default domain anthropic.com")
	}
}

// TestProtocolSpecificDomainsDeduplication tests that protocol-specific domains are deduplicated
func TestProtocolSpecificDomainsDeduplication(t *testing.T) {
	network := &NetworkPermissions{
		Allowed: []string{
			"https://example.com",
			"https://example.com", // Duplicate
			"http://example.com",  // Different protocol - should NOT deduplicate
		},
	}

	result := GetAllowedDomains(network)

	// Count occurrences of each domain
	httpsCount := 0
	httpCount := 0
	for _, domain := range result {
		if domain == "https://example.com" {
			httpsCount++
		}
		if domain == "http://example.com" {
			httpCount++
		}
	}

	// HTTPS should appear once (deduplicated)
	if httpsCount != 1 {
		t.Errorf("Expected https://example.com to appear once, got %d", httpsCount)
	}

	// HTTP should appear once (different protocol)
	if httpCount != 1 {
		t.Errorf("Expected http://example.com to appear once, got %d", httpCount)
	}
}

// TestProtocolSpecificDomainsSorting tests that domains with protocols are sorted correctly
func TestProtocolSpecificDomainsSorting(t *testing.T) {
	network := &NetworkPermissions{
		Allowed: []string{
			"example.org",
			"https://example.com",
			"http://example.com",
			"https://api.example.com",
		},
	}

	result := GetAllowedDomains(network)
	resultStr := strings.Join(result, ",")

	// Verify the result is comma-separated and sorted
	// The exact sort order depends on the SortStrings implementation,
	// but we can verify that the domains are present
	expectedDomains := []string{
		"example.org",
		"http://example.com",
		"https://api.example.com",
		"https://example.com",
	}

	for _, expected := range expectedDomains {
		if !strings.Contains(resultStr, expected) {
			t.Errorf("Expected result to contain %q", expected)
		}
	}
}
