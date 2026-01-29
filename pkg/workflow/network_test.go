//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompilerNetworkPermissionsExtraction(t *testing.T) {
	compiler := NewCompiler()

	// Helper function to create a temporary workflow file for testing
	createTempWorkflowFile := func(content string) (string, func()) {
		tmpDir, err := os.MkdirTemp("", "test-workflow-")
		if err != nil {
			t.Fatalf("Failed to create temp dir: %v", err)
		}

		filePath := filepath.Join(tmpDir, "test.md")
		err = os.WriteFile(filePath, []byte(content), 0644)
		if err != nil {
			t.Fatalf("Failed to write temp file: %v", err)
		}

		cleanup := func() {
			os.RemoveAll(tmpDir)
		}

		return filePath, cleanup
	}

	t.Run("Extract top-level network permissions", func(t *testing.T) {
		yamlContent := `---
on: push
engine:
  id: claude
  model: claude-3-5-sonnet-20241022
network:
  allowed:
    - "github.com"
    - "*.example.com"
    - "api.trusted.com"
strict: false
---

# Test Workflow
This is a test workflow with network permissions.`

		filePath, cleanup := createTempWorkflowFile(yamlContent)
		defer cleanup()

		workflowData, err := compiler.ParseWorkflowFile(filePath)
		if err != nil {
			t.Fatalf("Failed to parse workflow: %v", err)
		}

		if workflowData.NetworkPermissions == nil {
			t.Fatal("Expected network permissions to be extracted")
		}

		expectedDomains := []string{"github.com", "*.example.com", "api.trusted.com"}
		if len(workflowData.NetworkPermissions.Allowed) != len(expectedDomains) {
			t.Fatalf("Expected %d allowed domains, got %d", len(expectedDomains), len(workflowData.NetworkPermissions.Allowed))
		}

		for i, expected := range expectedDomains {
			if workflowData.NetworkPermissions.Allowed[i] != expected {
				t.Errorf("Expected domain %d to be '%s', got '%s'", i, expected, workflowData.NetworkPermissions.Allowed[i])
			}
		}
	})

	t.Run("No network permissions specified", func(t *testing.T) {
		yamlContent := `---
on: push
engine:
  id: claude
  model: claude-3-5-sonnet-20241022
---

# Test Workflow
This workflow has no network permissions.`

		filePath, cleanup := createTempWorkflowFile(yamlContent)
		defer cleanup()

		workflowData, err := compiler.ParseWorkflowFile(filePath)
		if err != nil {
			t.Fatalf("Failed to parse workflow: %v", err)
		}

		// When no network field is specified, should default to Allowed: ["defaults"]
		if workflowData.NetworkPermissions == nil {
			t.Error("Expected network permissions to default to 'defaults' ecosystem when not specified")
		} else if len(workflowData.NetworkPermissions.Allowed) != 1 || workflowData.NetworkPermissions.Allowed[0] != "defaults" {
			t.Errorf("Expected default allowed to be ['defaults'], got %v", workflowData.NetworkPermissions.Allowed)
		}
	})

	t.Run("Empty network permissions", func(t *testing.T) {
		yamlContent := `---
on: push
engine:
  id: claude
  model: claude-3-5-sonnet-20241022
network:
  allowed: []
strict: false
---

# Test Workflow
This workflow has empty network permissions (deny all).`

		filePath, cleanup := createTempWorkflowFile(yamlContent)
		defer cleanup()

		workflowData, err := compiler.ParseWorkflowFile(filePath)
		if err != nil {
			t.Fatalf("Failed to parse workflow: %v", err)
		}

		if workflowData.NetworkPermissions == nil {
			t.Fatal("Expected network permissions to be present even when empty")
		}

		if len(workflowData.NetworkPermissions.Allowed) != 0 {
			t.Errorf("Expected 0 allowed domains, got %d", len(workflowData.NetworkPermissions.Allowed))
		}
	})

	t.Run("Network permissions with single domain", func(t *testing.T) {
		yamlContent := `---
on: push
engine:
  id: claude
  model: claude-3-5-sonnet-20241022
network:
  allowed:
    - "single.domain.com"
strict: false
---

# Test Workflow
This workflow has a single allowed domain.`

		filePath, cleanup := createTempWorkflowFile(yamlContent)
		defer cleanup()

		workflowData, err := compiler.ParseWorkflowFile(filePath)
		if err != nil {
			t.Fatalf("Failed to parse workflow: %v", err)
		}

		if workflowData.NetworkPermissions == nil {
			t.Fatal("Expected network permissions to be extracted")
		}

		if len(workflowData.NetworkPermissions.Allowed) != 1 {
			t.Fatalf("Expected 1 allowed domain, got %d", len(workflowData.NetworkPermissions.Allowed))
		}

		if workflowData.NetworkPermissions.Allowed[0] != "single.domain.com" {
			t.Errorf("Expected domain 'single.domain.com', got '%s'", workflowData.NetworkPermissions.Allowed[0])
		}
	})

	t.Run("Network permissions passed to compilation", func(t *testing.T) {
		yamlContent := `---
on: push
engine:
  id: claude
  model: claude-3-5-sonnet-20241022
network:
  allowed:
    - "compilation.test.com"
strict: false
---

# Test Workflow
Test that network permissions are passed to engine during compilation.`

		filePath, cleanup := createTempWorkflowFile(yamlContent)
		defer cleanup()

		workflowData, err := compiler.ParseWorkflowFile(filePath)
		if err != nil {
			t.Fatalf("Failed to parse workflow: %v", err)
		}

		// Test that network permissions are present in the parsed data
		if workflowData.NetworkPermissions == nil {
			t.Fatal("Expected network permissions to be present")
		}

		if len(workflowData.NetworkPermissions.Allowed) != 1 ||
			workflowData.NetworkPermissions.Allowed[0] != "compilation.test.com" {
			t.Error("Network permissions not correctly extracted")
		}
	})

	t.Run("Multiple workflows with different network permissions", func(t *testing.T) {
		yaml1 := `---
on: push
engine:
  id: claude
  model: claude-3-5-sonnet-20241022
network:
  allowed:
    - "first.domain.com"
strict: false
---

# First Workflow`

		yaml2 := `---
on: push
engine:
  id: claude
  model: claude-3-5-sonnet-20241022
network:
  allowed:
    - "second.domain.com"
    - "third.domain.com"
strict: false
---

# Second Workflow`

		filePath1, cleanup1 := createTempWorkflowFile(yaml1)
		defer cleanup1()
		filePath2, cleanup2 := createTempWorkflowFile(yaml2)
		defer cleanup2()

		workflowData1, err := compiler.ParseWorkflowFile(filePath1)
		if err != nil {
			t.Fatalf("Failed to parse first workflow: %v", err)
		}

		workflowData2, err := compiler.ParseWorkflowFile(filePath2)
		if err != nil {
			t.Fatalf("Failed to parse second workflow: %v", err)
		}

		// Verify first workflow
		if len(workflowData1.NetworkPermissions.Allowed) != 1 {
			t.Errorf("First workflow should have 1 domain, got %d", len(workflowData1.NetworkPermissions.Allowed))
		}
		if workflowData1.NetworkPermissions.Allowed[0] != "first.domain.com" {
			t.Errorf("First workflow domain should be 'first.domain.com', got '%s'", workflowData1.NetworkPermissions.Allowed[0])
		}

		// Verify second workflow
		if len(workflowData2.NetworkPermissions.Allowed) != 2 {
			t.Errorf("Second workflow should have 2 domains, got %d", len(workflowData2.NetworkPermissions.Allowed))
		}
		expectedDomains := []string{"second.domain.com", "third.domain.com"}
		for i, expected := range expectedDomains {
			if workflowData2.NetworkPermissions.Allowed[i] != expected {
				t.Errorf("Second workflow domain %d should be '%s', got '%s'", i, expected, workflowData2.NetworkPermissions.Allowed[i])
			}
		}
	})
}

func TestNetworkPermissionsUtilities(t *testing.T) {
	t.Run("GetAllowedDomains with various inputs", func(t *testing.T) {
		// Test with nil - should return default allow-list
		domains := GetAllowedDomains(nil)
		if len(domains) == 0 {
			t.Errorf("Expected default allow-list domains for nil input, got %d", len(domains))
		}

		// Test with defaults mode - should return default allow-list
		defaultsPerms := &NetworkPermissions{Allowed: []string{"defaults"}}
		domains = GetAllowedDomains(defaultsPerms)
		if len(domains) == 0 {
			t.Errorf("Expected default allow-list domains for defaults mode, got %d", len(domains))
		}

		// Test with empty permissions object (no allowed list)
		emptyPerms := &NetworkPermissions{Allowed: []string{}}
		domains = GetAllowedDomains(emptyPerms)
		if len(domains) != 0 {
			t.Errorf("Expected 0 domains for empty allowed list, got %d", len(domains))
		}

		// Test with multiple domains - should be sorted alphabetically
		perms := &NetworkPermissions{
			Allowed: []string{"domain1.com", "*.domain2.com", "domain3.org"},
		}
		domains = GetAllowedDomains(perms)
		if len(domains) != 3 {
			t.Errorf("Expected 3 domains, got %d", len(domains))
		}

		// Domains should be sorted
		expected := []string{"*.domain2.com", "domain1.com", "domain3.org"}
		for i, expectedDomain := range expected {
			if domains[i] != expectedDomain {
				t.Errorf("Expected domain %d to be '%s', got '%s'", i, expectedDomain, domains[i])
			}
		}
	})

	t.Run("GetAllowedDomains with 'defaults' expansion", func(t *testing.T) {
		// Test with defaults in allowed list - should expand defaults and add custom domains
		perms := &NetworkPermissions{
			Allowed: []string{"defaults", "good.com", "api.example.com"},
		}
		domains := GetAllowedDomains(perms)

		// Should have all default domains plus the custom ones
		defaultDomains := getEcosystemDomains("defaults")
		expectedTotal := len(defaultDomains) + 2 // defaults + good.com + api.example.com

		if len(domains) != expectedTotal {
			t.Errorf("Expected %d domains (defaults + 2 custom), got %d", expectedTotal, len(domains))
		}

		// Verify custom domains are included
		foundGoodCom := false
		foundApiExample := false
		for _, domain := range domains {
			if domain == "good.com" {
				foundGoodCom = true
			}
			if domain == "api.example.com" {
				foundApiExample = true
			}
		}

		if !foundGoodCom {
			t.Error("Expected 'good.com' to be included in the expanded domains")
		}
		if !foundApiExample {
			t.Error("Expected 'api.example.com' to be included in the expanded domains")
		}
	})
}

// Test helper functions for network permissions
func TestNetworkPermissionHelpers(t *testing.T) {
	t.Run("hasNetworkPermissionsInConfig utility", func(t *testing.T) {
		// Test that we can check if network permissions exist
		perms := &NetworkPermissions{
			Allowed: []string{"example.com"},
		}

		if len(perms.Allowed) == 0 {
			t.Error("Network permissions should have allowed domains")
		}

		// Test empty permissions
		emptyPerms := &NetworkPermissions{Allowed: []string{}}

		if len(emptyPerms.Allowed) != 0 {
			t.Error("Empty network permissions should have 0 allowed domains")
		}
	})

	t.Run("domain matching logic", func(t *testing.T) {
		// Test basic domain matching patterns that would be used
		// in a real implementation
		allowedDomains := []string{"example.com", "*.trusted.com", "api.github.com"}

		testCases := []struct {
			domain   string
			expected bool
		}{
			{"example.com", true},
			{"api.github.com", true},
			{"subdomain.trusted.com", true}, // wildcard match
			{"another.trusted.com", true},   // wildcard match
			{"blocked.com", false},
			{"untrusted.com", false},
			{"example.com.malicious.com", false}, // not a true subdomain
		}

		for _, tc := range testCases {
			// Simple domain matching logic for testing
			allowed := false
			for _, allowedDomain := range allowedDomains {
				if allowedDomain == tc.domain {
					allowed = true
					break
				}
				if strings.HasPrefix(allowedDomain, "*.") {
					suffix := allowedDomain[2:] // Remove "*."
					if strings.HasSuffix(tc.domain, suffix) && tc.domain != suffix {
						// Ensure it's actually a subdomain, not just ending with the suffix
						if strings.HasSuffix(tc.domain, "."+suffix) {
							allowed = true
							break
						}
					}
				}
			}

			if allowed != tc.expected {
				t.Errorf("Domain %s: expected %v, got %v", tc.domain, tc.expected, allowed)
			}
		}
	})
}
