//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestGetBlockedDomains tests the GetBlockedDomains function
func TestGetBlockedDomains(t *testing.T) {
	tests := []struct {
		name     string
		network  *NetworkPermissions
		expected []string
	}{
		{
			name:     "nil network permissions",
			network:  nil,
			expected: []string{},
		},
		{
			name: "empty blocked list",
			network: &NetworkPermissions{
				Blocked: []string{},
			},
			expected: []string{},
		},
		{
			name: "single domain",
			network: &NetworkPermissions{
				Blocked: []string{"tracker.example.com"},
			},
			expected: []string{"tracker.example.com"},
		},
		{
			name: "multiple domains",
			network: &NetworkPermissions{
				Blocked: []string{"tracker.example.com", "analytics.example.com"},
			},
			expected: []string{"analytics.example.com", "tracker.example.com"}, // Sorted
		},
		{
			name: "ecosystem identifier",
			network: &NetworkPermissions{
				Blocked: []string{"python"},
			},
			expected: func() []string {
				// Get python ecosystem domains and sort them
				domains := getEcosystemDomains("python")
				SortStrings(domains)
				return domains
			}(),
		},
		{
			name: "mixed domains and ecosystems",
			network: &NetworkPermissions{
				Blocked: []string{"python", "tracker.example.com"},
			},
			expected: func() []string {
				// Get python ecosystem domains and add custom domain
				domainMap := make(map[string]bool)
				for _, d := range getEcosystemDomains("python") {
					domainMap[d] = true
				}
				domainMap["tracker.example.com"] = true

				domains := make([]string, 0, len(domainMap))
				for d := range domainMap {
					domains = append(domains, d)
				}
				SortStrings(domains)
				return domains
			}(),
		},
		{
			name: "duplicate domains are deduplicated",
			network: &NetworkPermissions{
				Blocked: []string{"tracker.example.com", "tracker.example.com"},
			},
			expected: []string{"tracker.example.com"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetBlockedDomains(tt.network)
			assert.Equal(t, tt.expected, result, "GetBlockedDomains should return expected domains")
		})
	}
}

// TestFormatBlockedDomains tests the formatBlockedDomains function
func TestFormatBlockedDomains(t *testing.T) {
	tests := []struct {
		name     string
		network  *NetworkPermissions
		expected string
	}{
		{
			name:     "nil network permissions",
			network:  nil,
			expected: "",
		},
		{
			name: "empty blocked list",
			network: &NetworkPermissions{
				Blocked: []string{},
			},
			expected: "",
		},
		{
			name: "single domain",
			network: &NetworkPermissions{
				Blocked: []string{"tracker.example.com"},
			},
			expected: "tracker.example.com",
		},
		{
			name: "multiple domains",
			network: &NetworkPermissions{
				Blocked: []string{"tracker.example.com", "analytics.example.com"},
			},
			expected: "analytics.example.com,tracker.example.com", // Sorted and comma-separated
		},
		{
			name: "ecosystem identifier",
			network: &NetworkPermissions{
				Blocked: []string{"python"},
			},
			expected: func() string {
				// Get python ecosystem domains, sort, and join
				domains := getEcosystemDomains("python")
				SortStrings(domains)
				return strings.Join(domains, ",")
			}(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatBlockedDomains(tt.network)
			assert.Equal(t, tt.expected, result, "formatBlockedDomains should return expected string")
		})
	}
}

// TestBlockedDomainsWithEngines tests that blocked domains are properly formatted for each engine
func TestBlockedDomainsWithEngines(t *testing.T) {
	network := &NetworkPermissions{
		Allowed: []string{"defaults", "github"},
		Blocked: []string{"tracker.example.com", "analytics.example.com"},
	}

	t.Run("blocked domains formatted correctly", func(t *testing.T) {
		blockedStr := formatBlockedDomains(network)
		assert.NotEmpty(t, blockedStr, "blocked domains string should not be empty")
		assert.Contains(t, blockedStr, "tracker.example.com", "should contain tracker.example.com")
		assert.Contains(t, blockedStr, "analytics.example.com", "should contain analytics.example.com")

		// Verify comma-separated format
		blockedDomains := strings.Split(blockedStr, ",")
		assert.Len(t, blockedDomains, 2, "should have 2 blocked domains")

		// Verify sorted order
		assert.Equal(t, "analytics.example.com", blockedDomains[0], "first domain should be analytics.example.com (sorted)")
		assert.Equal(t, "tracker.example.com", blockedDomains[1], "second domain should be tracker.example.com")
	})
}
