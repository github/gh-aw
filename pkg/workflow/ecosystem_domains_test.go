//go:build !integration

package workflow

import (
	"slices"
	"strings"
	"testing"
)

func TestDefaultsExcludeThirdPartySources(t *testing.T) {
	thirdPartySources := []string{
		"ppa.launchpad.net",
		"packagecloud.io",
		"api.snapcraft.io",
		"packages.cloud.google.com",
		"packages.microsoft.com",
	}
	optedIn := GetAllowedDomains(&NetworkPermissions{Allowed: []string{"defaults", "linux-distros"}})
	for _, domain := range thirdPartySources {
		if !slices.Contains(optedIn, domain) {
			t.Errorf("Expected third-party source %q with linux-distros opt-in", domain)
		}
		if ecosystem := GetDomainEcosystem(domain); ecosystem != "linux-distros" {
			t.Errorf("Expected %q to classify as linux-distros, got %q", domain, ecosystem)
		}
	}

	for _, tc := range []struct {
		name    string
		network *NetworkPermissions
	}{
		{name: "implicit"},
		{name: "explicit", network: &NetworkPermissions{Allowed: []string{"defaults"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			domains := GetAllowedDomains(tc.network)
			for _, domain := range append(thirdPartySources, "www.googleapis.com") {
				if slices.Contains(domains, domain) {
					t.Errorf("Default baseline must not include %q", domain)
				}
			}
			for _, domain := range []string{
				"crl3.digicert.com", "json-schema.org", "json.schemastore.org",
				"archive.ubuntu.com", "security.ubuntu.com",
				"keyserver.ubuntu.com", "azure.archive.ubuntu.com",
			} {
				if !slices.Contains(domains, domain) {
					t.Errorf("Default baseline must retain %q", domain)
				}
			}
		})
	}
}

func TestEcosystemPriorityCoversAllEcosystems(t *testing.T) {
	prioritized := make(map[string]struct{}, len(ecosystemPriority))
	for _, ecosystem := range ecosystemPriority {
		if _, exists := prioritized[ecosystem]; exists {
			t.Errorf("ecosystem %q appears more than once in ecosystemPriority", ecosystem)
		}
		prioritized[ecosystem] = struct{}{}
	}

	for ecosystem := range getLoadedDomainSets().Ecosystems {
		if _, exists := prioritized[ecosystem]; !exists {
			t.Errorf("ecosystem %q is missing from ecosystemPriority", ecosystem)
		}
	}
	for ecosystem := range compoundEcosystems {
		if _, exists := prioritized[ecosystem]; !exists {
			t.Errorf("compound ecosystem %q is missing from ecosystemPriority", ecosystem)
		}
	}
}

func TestRuntimeSelectedEcosystemsExcludeTelemetryDomains(t *testing.T) {
	for runtime := range runtimeToEcosystem {
		domains := getDomainsFromRuntimes(map[string]any{runtime: nil})
		for _, domain := range domains {
			if strings.Contains(strings.ToLower(domain), "telemetry") {
				t.Errorf("runtime %q automatically includes telemetry domain %q", runtime, domain)
			}
		}
	}

	dotnetDomains := getDomainsFromRuntimes(map[string]any{"dotnet": nil})
	if slices.Contains(dotnetDomains, "dc.services.visualstudio.com") {
		t.Error("dotnet runtime automatically includes dc.services.visualstudio.com")
	}
}

func TestEcosystemDomainExpansion(t *testing.T) {
	t.Run("defaults ecosystem includes basic infrastructure", func(t *testing.T) {
		permissions := &NetworkPermissions{
			Allowed: []string{"defaults"},
		}
		domains := GetAllowedDomains(permissions)

		// Check that basic infrastructure domains are included
		expectedDomains := []string{
			"crl3.digicert.com",  // Certificates
			"json-schema.org",    // JSON Schema
			"archive.ubuntu.com", // Ubuntu
		}

		for _, expectedDomain := range expectedDomains {
			found := slices.Contains(domains, expectedDomain)
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
			found := slices.Contains(domains, excludedDomain)
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
			found := slices.Contains(domains, expectedDomain)
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
			found := slices.Contains(domains, expectedDomain)
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
			found := slices.Contains(domains, expectedDomain)
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
			"storage.googleapis.com",
		}

		for _, expectedDomain := range expectedDomains {
			found := slices.Contains(domains, expectedDomain)
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
			"repo1.maven.org",
			"services.gradle.org",
			"plugins.gradle.org",
			"download.oracle.com",
			"dlcdn.apache.org",
			"archive.apache.org",
			"download.java.net",
			"api.foojay.io",
			"cdn.azul.com",
			"central.sonatype.com",
			"maven.google.com",
			"dl.google.com",
			"repo.gradle.org",
			"maven-central.storage-download.googleapis.com",
			"repository.apache.org",
		}

		for _, expectedDomain := range expectedDomains {
			found := slices.Contains(domains, expectedDomain)
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
			"esm.sh",
			"googleapis.deno.dev",
			"googlechromelabs.github.io",
			"cdn.jsdelivr.net",
		}

		for _, expectedDomain := range expectedDomains {
			found := slices.Contains(domains, expectedDomain)
			if !found {
				t.Errorf("Expected domain '%s' to be included in node ecosystem, but it was not found", expectedDomain)
			}
		}
	})

	t.Run("python-native ecosystem includes Rust FFI domains for native packages", func(t *testing.T) {
		permissions := &NetworkPermissions{
			Allowed: []string{"python-native"},
		}
		domains := GetAllowedDomains(permissions)

		expectedDomains := []string{
			"pypi.org",
			"pip.pypa.io",
			"crates.io",
			"index.crates.io",
			"static.crates.io",
		}

		for _, expectedDomain := range expectedDomains {
			found := slices.Contains(domains, expectedDomain)
			if !found {
				t.Errorf("Expected domain '%s' to be included in python-native ecosystem, but it was not found", expectedDomain)
			}
		}
	})

	t.Run("python ecosystem does not include crates domains", func(t *testing.T) {
		permissions := &NetworkPermissions{
			Allowed: []string{"python"},
		}
		domains := GetAllowedDomains(permissions)

		cratesDomains := []string{
			"crates.io",
			"index.crates.io",
			"static.crates.io",
		}

		for _, domain := range cratesDomains {
			found := slices.Contains(domains, domain)
			if found {
				t.Errorf("Expected domain '%s' to NOT be included in python ecosystem, but it was found", domain)
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
			"patchdiff.githubusercontent.com",
			"patch-diff.githubusercontent.com",
			"lfs.github.com",
			"github.githubassets.com",
		}

		for _, expectedDomain := range expectedDomains {
			found := slices.Contains(domains, expectedDomain)
			if !found {
				t.Errorf("Expected domain '%s' to be included in github ecosystem, but it was not found", expectedDomain)
			}
		}
	})

	t.Run("bazel ecosystem includes Bazel registry and download domains", func(t *testing.T) {
		permissions := &NetworkPermissions{
			Allowed: []string{"bazel"},
		}
		domains := GetAllowedDomains(permissions)

		expectedDomains := []string{
			"releases.bazel.build",
			"mirror.bazel.build",
			"bcr.bazel.build",
		}

		for _, expectedDomain := range expectedDomains {
			found := slices.Contains(domains, expectedDomain)
			if !found {
				t.Errorf("Expected domain '%s' to be included in bazel ecosystem, but it was not found", expectedDomain)
			}
		}
	})

	t.Run("julia ecosystem includes Julia package registry domains", func(t *testing.T) {
		permissions := &NetworkPermissions{
			Allowed: []string{"julia"},
		}
		domains := GetAllowedDomains(permissions)

		expectedDomains := []string{
			"pkg.julialang.org",
			"julialang.org",
		}

		for _, expectedDomain := range expectedDomains {
			found := slices.Contains(domains, expectedDomain)
			if !found {
				t.Errorf("Expected domain '%s' to be included in julia ecosystem, but it was not found", expectedDomain)
			}
		}
	})

	t.Run("lua ecosystem includes LuaRocks domains", func(t *testing.T) {
		permissions := &NetworkPermissions{
			Allowed: []string{"lua"},
		}
		domains := GetAllowedDomains(permissions)

		expectedDomains := []string{
			"luarocks.org",
			"www.luarocks.org",
		}

		for _, expectedDomain := range expectedDomains {
			found := slices.Contains(domains, expectedDomain)
			if !found {
				t.Errorf("Expected domain '%s' to be included in lua ecosystem, but it was not found", expectedDomain)
			}
		}
	})

	t.Run("latex ecosystem includes CTAN and TeX Live domains", func(t *testing.T) {
		permissions := &NetworkPermissions{
			Allowed: []string{"latex"},
		}
		domains := GetAllowedDomains(permissions)

		expectedDomains := []string{
			"ctan.org",
			"mirror.ctan.org",
			"mirrors.ctan.org",
			"tug.org",
			"www.tug.org",
			"ftp.tug.org",
			"latex-project.org",
			"www.latex-project.org",
			"miktex.org",
			"packages.miktex.org",
		}

		for _, expectedDomain := range expectedDomains {
			found := slices.Contains(domains, expectedDomain)
			if !found {
				t.Errorf("Expected domain '%s' to be included in latex ecosystem, but it was not found", expectedDomain)
			}
		}
	})

	t.Run("ocaml ecosystem includes opam domains", func(t *testing.T) {
		permissions := &NetworkPermissions{
			Allowed: []string{"ocaml"},
		}
		domains := GetAllowedDomains(permissions)

		expectedDomains := []string{
			"opam.ocaml.org",
			"ocaml.org",
			"erratique.ch",
		}

		for _, expectedDomain := range expectedDomains {
			found := slices.Contains(domains, expectedDomain)
			if !found {
				t.Errorf("Expected domain '%s' to be included in ocaml ecosystem, but it was not found", expectedDomain)
			}
		}
	})

	t.Run("r ecosystem includes CRAN domains", func(t *testing.T) {
		permissions := &NetworkPermissions{
			Allowed: []string{"r"},
		}
		domains := GetAllowedDomains(permissions)

		expectedDomains := []string{
			"cloud.r-project.org",
			"cran.r-project.org",
			"cran.rstudio.com",
		}

		for _, expectedDomain := range expectedDomains {
			found := slices.Contains(domains, expectedDomain)
			if !found {
				t.Errorf("Expected domain '%s' to be included in r ecosystem, but it was not found", expectedDomain)
			}
		}
	})

	t.Run("kotlin ecosystem includes JetBrains and Kotlin domains", func(t *testing.T) {
		permissions := &NetworkPermissions{
			Allowed: []string{"kotlin"},
		}
		domains := GetAllowedDomains(permissions)

		expectedDomains := []string{
			"download.jetbrains.com",
			"ge.jetbrains.com",
			"packages.jetbrains.team",
			"kotlin.bintray.com",
			"maven.pkg.jetbrains.space",
		}

		for _, expectedDomain := range expectedDomains {
			found := slices.Contains(domains, expectedDomain)
			if !found {
				t.Errorf("Expected domain '%s' to be included in kotlin ecosystem, but it was not found", expectedDomain)
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
			found := slices.Contains(domains, expectedDomain)
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
			found := slices.Contains(domains, expectedDomain)
			if !found {
				t.Errorf("Expected domain '%s' to be included as literal domain, but it was not found", expectedDomain)
			}
		}
	})
}

func TestAllEcosystemDomainFunctions(t *testing.T) {
	// Test that all ecosystem categories return non-empty slices
	ecosystemCategories := []string{
		"defaults", "containers", "bazel", "dotnet", "dart", "github", "go",
		"terraform", "haskell", "java", "julia", "kotlin", "latex", "linux-distros", "lua", "node",
		"ocaml", "perl", "php", "playwright", "python", "python-native", "r", "ruby", "rust", "swift",
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
		"defaults", "containers", "bazel", "dotnet", "dart", "github", "go",
		"terraform", "haskell", "java", "julia", "kotlin", "latex", "linux-distros", "lua", "node",
		"ocaml", "perl", "php", "playwright", "python", "python-native", "r", "ruby", "rust", "swift",
	}

	for _, category := range ecosystemCategories {
		t.Run("getEcosystemDomains_"+category+"_uniqueness", func(t *testing.T) {
			domains := getEcosystemDomains(category)
			seen := make(map[string]struct{})

			for _, domain := range domains {
				if _, ok := seen[domain]; ok {
					t.Errorf("getEcosystemDomains(%q) returned duplicate domain: %s", category, domain)
				}
				seen[domain] = struct{}{}
			}
		})
	}
}
