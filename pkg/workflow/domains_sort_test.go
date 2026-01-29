//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

// TestGetAllowedDomainsSorted tests that domains are returned in sorted order
func TestGetAllowedDomainsSorted(t *testing.T) {
	t.Run("single ecosystem returns sorted domains", func(t *testing.T) {
		permissions := &NetworkPermissions{
			Allowed: []string{"defaults"},
		}
		domains := GetAllowedDomains(permissions)

		// Verify that domains are sorted
		for i := 0; i < len(domains)-1; i++ {
			if domains[i] >= domains[i+1] {
				t.Errorf("Domains not sorted: %q >= %q at index %d", domains[i], domains[i+1], i)
			}
		}
	})

	t.Run("multiple ecosystems return sorted domains", func(t *testing.T) {
		permissions := &NetworkPermissions{
			Allowed: []string{"python", "node", "containers"},
		}
		domains := GetAllowedDomains(permissions)

		// Verify that domains are sorted
		for i := 0; i < len(domains)-1; i++ {
			if domains[i] >= domains[i+1] {
				t.Errorf("Domains not sorted: %q >= %q at index %d", domains[i], domains[i+1], i)
			}
		}
	})

	t.Run("mixed domains and ecosystems return sorted", func(t *testing.T) {
		permissions := &NetworkPermissions{
			Allowed: []string{"zulu.com", "python", "alpha.com", "node"},
		}
		domains := GetAllowedDomains(permissions)

		// Verify that domains are sorted
		for i := 0; i < len(domains)-1; i++ {
			if domains[i] >= domains[i+1] {
				t.Errorf("Domains not sorted: %q >= %q at index %d", domains[i], domains[i+1], i)
			}
		}

		// Verify alpha.com comes before zulu.com
		alphaIdx := -1
		zuluIdx := -1
		for i, d := range domains {
			if d == "alpha.com" {
				alphaIdx = i
			}
			if d == "zulu.com" {
				zuluIdx = i
			}
		}

		if alphaIdx == -1 || zuluIdx == -1 {
			t.Fatal("Expected both alpha.com and zulu.com to be in the list")
		}

		if alphaIdx >= zuluIdx {
			t.Errorf("Expected alpha.com (index %d) to come before zulu.com (index %d)", alphaIdx, zuluIdx)
		}
	})
}

// TestGetAllowedDomainsDeduplication tests that duplicate domains are removed
func TestGetAllowedDomainsDeduplication(t *testing.T) {
	t.Run("duplicate individual domains are removed", func(t *testing.T) {
		permissions := &NetworkPermissions{
			Allowed: []string{"example.com", "test.org", "example.com", "test.org"},
		}
		domains := GetAllowedDomains(permissions)

		// Count occurrences of each domain
		domainCount := make(map[string]int)
		for _, domain := range domains {
			domainCount[domain]++
		}

		// Verify no duplicates
		for domain, count := range domainCount {
			if count > 1 {
				t.Errorf("Domain %q appears %d times, expected 1", domain, count)
			}
		}

		// Verify expected domains are present exactly once
		if domainCount["example.com"] != 1 {
			t.Errorf("Expected example.com to appear once, got %d", domainCount["example.com"])
		}
		if domainCount["test.org"] != 1 {
			t.Errorf("Expected test.org to appear once, got %d", domainCount["test.org"])
		}

		// Should have exactly 2 unique domains
		if len(domains) != 2 {
			t.Errorf("Expected 2 unique domains, got %d", len(domains))
		}
	})

	t.Run("overlapping ecosystem domains are deduplicated", func(t *testing.T) {
		// Some ecosystems may share common domains
		// For example, if we request the same ecosystem twice
		permissions := &NetworkPermissions{
			Allowed: []string{"python", "python"},
		}
		domains := GetAllowedDomains(permissions)

		// Count occurrences of each domain
		domainCount := make(map[string]int)
		for _, domain := range domains {
			domainCount[domain]++
		}

		// Verify no duplicates
		for domain, count := range domainCount {
			if count > 1 {
				t.Errorf("Domain %q appears %d times, expected 1", domain, count)
			}
		}
	})

	t.Run("ecosystem and explicit domain overlap", func(t *testing.T) {
		// Get domains from python ecosystem first
		pythonDomains := getEcosystemDomains("python")
		if len(pythonDomains) == 0 {
			t.Skip("Python ecosystem has no domains")
		}

		// Pick one domain from python ecosystem
		explicitDomain := pythonDomains[0]

		permissions := &NetworkPermissions{
			Allowed: []string{"python", explicitDomain},
		}
		domains := GetAllowedDomains(permissions)

		// Count occurrences of the explicit domain
		count := 0
		for _, domain := range domains {
			if domain == explicitDomain {
				count++
			}
		}

		if count != 1 {
			t.Errorf("Expected domain %q to appear once (deduplicated), got %d occurrences", explicitDomain, count)
		}
	})

	t.Run("multiple ecosystems with potential overlaps", func(t *testing.T) {
		permissions := &NetworkPermissions{
			Allowed: []string{"node", "python", "ruby", "node"},
		}
		domains := GetAllowedDomains(permissions)

		// Count occurrences of each domain
		domainCount := make(map[string]int)
		for _, domain := range domains {
			domainCount[domain]++
		}

		// Verify no duplicates
		for domain, count := range domainCount {
			if count > 1 {
				t.Errorf("Domain %q appears %d times, expected 1", domain, count)
			}
		}
	})
}

// TestGetAllowedDomainsSortedAndUnique tests both sorting and deduplication together
func TestGetAllowedDomainsSortedAndUnique(t *testing.T) {
	t.Run("complex mix: sorted and deduplicated", func(t *testing.T) {
		permissions := &NetworkPermissions{
			Allowed: []string{
				"zebra.com",
				"alpha.com",
				"python",
				"beta.com",
				"alpha.com", // duplicate
				"node",
				"charlie.com",
				"beta.com", // duplicate
			},
		}
		domains := GetAllowedDomains(permissions)

		// Verify sorted
		for i := 0; i < len(domains)-1; i++ {
			if domains[i] >= domains[i+1] {
				t.Errorf("Domains not sorted: %q >= %q at index %d", domains[i], domains[i+1], i)
			}
		}

		// Verify deduplicated
		domainCount := make(map[string]int)
		for _, domain := range domains {
			domainCount[domain]++
		}
		for domain, count := range domainCount {
			if count > 1 {
				t.Errorf("Domain %q appears %d times, expected 1", domain, count)
			}
		}

		// Verify expected explicit domains are present
		expectedExplicit := []string{"alpha.com", "beta.com", "charlie.com", "zebra.com"}
		for _, expected := range expectedExplicit {
			if domainCount[expected] != 1 {
				t.Errorf("Expected %q to be present exactly once, got %d", expected, domainCount[expected])
			}
		}
	})

	t.Run("ecosystem domains are sorted", func(t *testing.T) {
		// Test that getEcosystemDomains returns sorted domains
		domains := getEcosystemDomains("defaults")

		if len(domains) == 0 {
			t.Skip("Defaults ecosystem has no domains")
		}

		// Verify sorted
		for i := 0; i < len(domains)-1; i++ {
			if domains[i] >= domains[i+1] {
				t.Errorf("Ecosystem domains not sorted: %q >= %q at index %d", domains[i], domains[i+1], i)
			}
		}
	})
}

// TestGetCopilotAllowedDomainsSorted tests that Copilot domains are sorted
func TestGetCopilotAllowedDomainsSorted(t *testing.T) {
	t.Run("Copilot with network permissions returns sorted CSV", func(t *testing.T) {
		permissions := &NetworkPermissions{
			Allowed: []string{"zebra.com", "alpha.com", "python"},
		}
		domainsStr := GetCopilotAllowedDomains(permissions)

		// Split the CSV and verify sorted
		domains := strings.Split(domainsStr, ",")
		for i := 0; i < len(domains)-1; i++ {
			if domains[i] >= domains[i+1] {
				t.Errorf("Copilot domains not sorted: %q >= %q at index %d", domains[i], domains[i+1], i)
			}
		}
	})

	t.Run("Copilot with duplicates returns unique CSV", func(t *testing.T) {
		permissions := &NetworkPermissions{
			Allowed: []string{"example.com", "example.com", "test.org"},
		}
		domainsStr := GetCopilotAllowedDomains(permissions)

		// Split the CSV and verify no duplicates
		domains := strings.Split(domainsStr, ",")
		domainCount := make(map[string]int)
		for _, domain := range domains {
			domainCount[domain]++
		}

		for domain, count := range domainCount {
			if count > 1 {
				t.Errorf("Domain %q appears %d times in CSV, expected 1", domain, count)
			}
		}
	})
}
