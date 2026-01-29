//go:build !integration

package workflow

import (
	"testing"
)

func TestEcosystemDomainExpansion(t *testing.T) {
	t.Run("defaults ecosystem includes basic infrastructure", func(t *testing.T) {
		permissions := &NetworkPermissions{
			Allowed: []string{"defaults"},
		}
		domains := GetAllowedDomains(permissions)

		// Check that basic infrastructure domains are included
		expectedDomains := []string{
			"crl3.digicert.com",      // Certificates
			"json-schema.org",        // JSON Schema
			"archive.ubuntu.com",     // Ubuntu
			"packagecloud.io",        // Common Package Mirrors
			"packages.microsoft.com", // Microsoft Sources
		}

		for _, expectedDomain := range expectedDomains {
			found := false
			for _, domain := range domains {
				if domain == expectedDomain {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("Expected domain '%s' to be included in defaults, but it was not found", expectedDomain)
			}
		}

		// Check that ecosystem-specific domains are NOT included in defaults
		excludedDomains := []string{
			"ghcr.io",    // Container registries
			"nuget.org",  // .NET
			"github.com", // GitHub (not in defaults anymore)
			"golang.org", // Go
			"npmjs.org",  // Node
			"pypi.org",   // Python
		}

		for _, excludedDomain := range excludedDomains {
			found := false
			for _, domain := range domains {
				if domain == excludedDomain {
					found = true
					break
				}
			}
			if found {
				t.Errorf("Domain '%s' should NOT be included in defaults, but it was found", excludedDomain)
			}
		}
	})

	t.Run("containers ecosystem includes container registries", func(t *testing.T) {
		permissions := &NetworkPermissions{
			Allowed: []string{"containers"},
		}
		domains := GetAllowedDomains(permissions)

		expectedDomains := []string{
			"ghcr.io",
			"registry.hub.docker.com",
			"*.docker.io",
			"quay.io",
			"gcr.io",
		}

		for _, expectedDomain := range expectedDomains {
			found := false
			for _, domain := range domains {
				if domain == expectedDomain {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("Expected domain '%s' to be included in containers ecosystem, but it was not found", expectedDomain)
			}
		}
	})

	t.Run("dotnet ecosystem includes .NET and NuGet domains", func(t *testing.T) {
		permissions := &NetworkPermissions{
			Allowed: []string{"dotnet"},
		}
		domains := GetAllowedDomains(permissions)

		expectedDomains := []string{
			"nuget.org",
			"dist.nuget.org",
			"api.nuget.org",
			"dotnet.microsoft.com",
			"dot.net",
		}

		for _, expectedDomain := range expectedDomains {
			found := false
			for _, domain := range domains {
				if domain == expectedDomain {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("Expected domain '%s' to be included in dotnet ecosystem, but it was not found", expectedDomain)
			}
		}
	})

	t.Run("python ecosystem includes Python package domains", func(t *testing.T) {
		permissions := &NetworkPermissions{
			Allowed: []string{"python"},
		}
		domains := GetAllowedDomains(permissions)

		expectedDomains := []string{
			"pypi.org",
			"pip.pypa.io",
			"*.pythonhosted.org",
			"files.pythonhosted.org",
			"anaconda.org",
		}

		for _, expectedDomain := range expectedDomains {
			found := false
			for _, domain := range domains {
				if domain == expectedDomain {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("Expected domain '%s' to be included in python ecosystem, but it was not found", expectedDomain)
			}
		}
	})

	t.Run("go ecosystem includes Go package domains", func(t *testing.T) {
		permissions := &NetworkPermissions{
			Allowed: []string{"go"},
		}
		domains := GetAllowedDomains(permissions)

		expectedDomains := []string{
			"go.dev",
			"golang.org",
			"proxy.golang.org",
			"sum.golang.org",
			"pkg.go.dev",
		}

		for _, expectedDomain := range expectedDomains {
			found := false
			for _, domain := range domains {
				if domain == expectedDomain {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("Expected domain '%s' to be included in go ecosystem, but it was not found", expectedDomain)
			}
		}
	})

	t.Run("java ecosystem includes Java package and tooling domains", func(t *testing.T) {
		permissions := &NetworkPermissions{
			Allowed: []string{"java"},
		}
		domains := GetAllowedDomains(permissions)

		expectedDomains := []string{
			"repo.maven.apache.org",
			"services.gradle.org",
			"download.oracle.com",
			"dlcdn.apache.org",
			"archive.apache.org",
			"download.java.net",
			"api.foojay.io",
			"cdn.azul.com",
		}

		for _, expectedDomain := range expectedDomains {
			found := false
			for _, domain := range domains {
				if domain == expectedDomain {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("Expected domain '%s' to be included in java ecosystem, but it was not found", expectedDomain)
			}
		}
	})

	t.Run("node ecosystem includes Node.js package domains", func(t *testing.T) {
		permissions := &NetworkPermissions{
			Allowed: []string{"node"},
		}
		domains := GetAllowedDomains(permissions)

		expectedDomains := []string{
			"npmjs.org",
			"registry.npmjs.com",
			"nodejs.org",
			"yarnpkg.com",
			"bun.sh",
			"deno.land",
			"jsr.io",
			"*.jsr.io",
		}

		for _, expectedDomain := range expectedDomains {
			found := false
			for _, domain := range domains {
				if domain == expectedDomain {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("Expected domain '%s' to be included in node ecosystem, but it was not found", expectedDomain)
			}
		}
	})

	t.Run("github ecosystem includes GitHub domains", func(t *testing.T) {
		permissions := &NetworkPermissions{
			Allowed: []string{"github"},
		}
		domains := GetAllowedDomains(permissions)

		expectedDomains := []string{
			"*.githubusercontent.com",
			"raw.githubusercontent.com",
			"objects.githubusercontent.com",
			"lfs.github.com",
			"github.githubassets.com",
		}

		for _, expectedDomain := range expectedDomains {
			found := false
			for _, domain := range domains {
				if domain == expectedDomain {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("Expected domain '%s' to be included in github ecosystem, but it was not found", expectedDomain)
			}
		}
	})

	t.Run("multiple ecosystems can be combined", func(t *testing.T) {
		permissions := &NetworkPermissions{
			Allowed: []string{"defaults", "dotnet", "python", "example.com"},
		}
		domains := GetAllowedDomains(permissions)

		// Should include domains from all specified ecosystems plus custom domain
		expectedFromDefaults := []string{"json-schema.org", "archive.ubuntu.com"}
		expectedFromDotnet := []string{"nuget.org", "dotnet.microsoft.com"}
		expectedFromPython := []string{"pypi.org", "*.pythonhosted.org"}
		expectedCustom := []string{"example.com"}

		allExpected := append(expectedFromDefaults, expectedFromDotnet...)
		allExpected = append(allExpected, expectedFromPython...)
		allExpected = append(allExpected, expectedCustom...)

		for _, expectedDomain := range allExpected {
			found := false
			for _, domain := range domains {
				if domain == expectedDomain {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("Expected domain '%s' to be included in combined ecosystems, but it was not found", expectedDomain)
			}
		}
	})

	t.Run("unknown ecosystem identifier is treated as domain", func(t *testing.T) {
		permissions := &NetworkPermissions{
			Allowed: []string{"unknown-ecosystem", "example.com"},
		}
		domains := GetAllowedDomains(permissions)

		// Should include both as literal domains
		expectedDomains := []string{"unknown-ecosystem", "example.com"}

		if len(domains) != 2 {
			t.Fatalf("Expected 2 domains, got %d: %v", len(domains), domains)
		}

		for _, expectedDomain := range expectedDomains {
			found := false
			for _, domain := range domains {
				if domain == expectedDomain {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("Expected domain '%s' to be included as literal domain, but it was not found", expectedDomain)
			}
		}
	})
}

func TestAllEcosystemDomainFunctions(t *testing.T) {
	// Test that all ecosystem categories return non-empty slices
	ecosystemCategories := []string{
		"defaults", "containers", "dotnet", "dart", "github", "go",
		"terraform", "haskell", "java", "linux-distros", "node",
		"perl", "php", "playwright", "python", "ruby", "rust", "swift",
	}

	for _, category := range ecosystemCategories {
		t.Run("getEcosystemDomains_"+category, func(t *testing.T) {
			domains := getEcosystemDomains(category)
			if len(domains) == 0 {
				t.Errorf("getEcosystemDomains(%q) returned empty slice, expected at least one domain", category)
			}

			// Check that all domains are non-empty strings
			for i, domain := range domains {
				if domain == "" {
					t.Errorf("getEcosystemDomains(%q) returned empty domain at index %d", category, i)
				}
			}
		})
	}
}

func TestEcosystemDomainsUniqueness(t *testing.T) {
	// Test that each ecosystem category returns unique domains (no duplicates)
	ecosystemCategories := []string{
		"defaults", "containers", "dotnet", "dart", "github", "go",
		"terraform", "haskell", "java", "linux-distros", "node",
		"perl", "php", "playwright", "python", "ruby", "rust", "swift",
	}

	for _, category := range ecosystemCategories {
		t.Run("getEcosystemDomains_"+category+"_uniqueness", func(t *testing.T) {
			domains := getEcosystemDomains(category)
			seen := make(map[string]bool)

			for _, domain := range domains {
				if seen[domain] {
					t.Errorf("getEcosystemDomains(%q) returned duplicate domain: %s", category, domain)
				}
				seen[domain] = true
			}
		})
	}
}
