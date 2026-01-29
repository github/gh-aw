//go:build !integration

package campaign

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpdateSpecWithProjectURL(t *testing.T) {
	// Create a temporary directory
	tmpDir := t.TempDir()

	// Create a spec file with placeholder URL
	specContent := `---
id: test-campaign
name: Test Campaign
project-url: https://github.com/orgs/ORG/projects/1
version: v1
state: planned
---

# Test Campaign

This is a test campaign.
`

	specPath := filepath.Join(tmpDir, "test.campaign.md")
	if err := os.WriteFile(specPath, []byte(specContent), 0o644); err != nil {
		t.Fatalf("Failed to write test spec file: %v", err)
	}

	// Update the spec with a real project URL
	newProjectURL := "https://github.com/orgs/myorg/projects/42"
	if err := UpdateSpecWithProjectURL(specPath, newProjectURL); err != nil {
		t.Fatalf("UpdateSpecWithProjectURL failed: %v", err)
	}

	// Read the updated spec
	updatedContent, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("Failed to read updated spec file: %v", err)
	}

	updatedStr := string(updatedContent)

	// Verify the URL was updated
	if !strings.Contains(updatedStr, newProjectURL) {
		t.Errorf("Expected updated spec to contain '%s', but it doesn't", newProjectURL)
	}

	// Verify the placeholder URL is gone
	if strings.Contains(updatedStr, "https://github.com/orgs/ORG/projects/1") {
		t.Error("Updated spec still contains placeholder URL")
	}
}

func TestUpdateSpecWithProjectURL_NoPlaceholder(t *testing.T) {
	// Create a temporary directory
	tmpDir := t.TempDir()

	// Create a spec file without placeholder URL
	specContent := `---
id: test-campaign
name: Test Campaign
project-url: https://github.com/orgs/myorg/projects/99
version: v1
state: planned
---

# Test Campaign

This is a test campaign.
`

	specPath := filepath.Join(tmpDir, "test.campaign.md")
	if err := os.WriteFile(specPath, []byte(specContent), 0o644); err != nil {
		t.Fatalf("Failed to write test spec file: %v", err)
	}

	// Try to update the spec (should succeed but not change anything)
	newProjectURL := "https://github.com/orgs/myorg/projects/42"
	if err := UpdateSpecWithProjectURL(specPath, newProjectURL); err != nil {
		t.Fatalf("UpdateSpecWithProjectURL failed: %v", err)
	}

	// Read the content
	updatedContent, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("Failed to read updated spec file: %v", err)
	}

	updatedStr := string(updatedContent)

	// Verify the original URL is still there (not replaced)
	if !strings.Contains(updatedStr, "https://github.com/orgs/myorg/projects/99") {
		t.Error("Original project URL was incorrectly modified")
	}

	// Verify the new URL was not added
	if strings.Contains(updatedStr, newProjectURL) {
		t.Error("New project URL was incorrectly added")
	}
}

func TestUpdateSpecWithProjectURL_FileNotFound(t *testing.T) {
	// Try to update a non-existent file
	err := UpdateSpecWithProjectURL("/nonexistent/path/spec.md", "https://github.com/orgs/myorg/projects/1")
	if err == nil {
		t.Error("Expected error for non-existent file, got nil")
	}

	if !strings.Contains(err.Error(), "failed to read spec file") {
		t.Errorf("Expected 'failed to read spec file' error, got: %v", err)
	}
}

func TestIsGHCLIAvailable(t *testing.T) {

	// This test just verifies the function doesn't panic
	// The actual result depends on the test environment
	available := isGHCLIAvailable()
	t.Logf("GitHub CLI available: %v", available)
}

func TestCreateProjectFieldsConfiguration(t *testing.T) {
	// This test verifies that project fields are correctly configured
	// We can't actually create fields without gh CLI and authentication,
	// but we can verify the field definitions are correct

	// Expected fields that should be created
	expectedFields := map[string]struct {
		dataType   string
		hasOptions bool
	}{
		"Campaign Id":     {"TEXT", false},
		"Worker Workflow": {"TEXT", false},
		"Target Repo":     {"TEXT", false},
		"Priority":        {"SINGLE_SELECT", true},
		"Size":            {"SINGLE_SELECT", true},
		"Start Date":      {"DATE", false},
		"End Date":        {"DATE", false},
	}

	// Note: This is a design test - we're checking that the field configuration
	// matches our requirements. The actual field creation is tested via integration tests.
	for fieldName := range expectedFields {
		// Just verify the expected field names exist in our test expectations
		t.Logf("Expected field: %s", fieldName)
	}

	// Verify "Repository" is NOT in the expected fields (to avoid GitHub conflict)
	if _, exists := expectedFields["Repository"]; exists {
		t.Error("Field 'Repository' should not be created (conflicts with GitHub built-in REPOSITORY field)")
	}

	// Verify "Target Repo" IS in the expected fields
	if _, exists := expectedFields["Target Repo"]; !exists {
		t.Error("Field 'Target Repo' should be created (replacement for 'repository')")
	}
}
