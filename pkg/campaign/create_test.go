//go:build !integration

package campaign

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCreateSpecSkeleton_Basic(t *testing.T) {
	tmpDir := t.TempDir()

	path, err := CreateSpecSkeleton(tmpDir, "test-campaign", false)
	if err != nil {
		t.Fatalf("CreateSpecSkeleton failed: %v", err)
	}

	expectedPath := ".github/workflows/test-campaign.campaign.md"
	if path != expectedPath {
		t.Errorf("Expected path '%s', got '%s'", expectedPath, path)
	}

	// Verify file was created
	fullPath := filepath.Join(tmpDir, ".github", "workflows", "test-campaign.campaign.md")
	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		t.Errorf("Expected file to be created at %s", fullPath)
	}

	// Read and verify content
	content, err := os.ReadFile(fullPath)
	if err != nil {
		t.Fatalf("Failed to read created file: %v", err)
	}

	contentStr := string(content)
	if !strings.Contains(contentStr, "id: test-campaign") {
		t.Error("Expected file to contain 'id: test-campaign'")
	}
	if !strings.Contains(contentStr, "name: Test campaign") {
		t.Error("Expected file to contain 'name: Test campaign'")
	}
	if !strings.Contains(contentStr, "version: v1") {
		t.Error("Expected file to contain 'version: v1'")
	}
	if !strings.Contains(contentStr, "state: planned") {
		t.Error("Expected file to contain 'state: planned'")
	}
	if strings.Contains(contentStr, "tracker-label:") {
		t.Error("Did not expect file to contain legacy 'tracker-label' frontmatter")
	}
	if !strings.Contains(contentStr, "project-url: https://github.com/orgs/ORG/projects/1") {
		t.Error("Expected file to contain 'project-url: https://github.com/orgs/ORG/projects/1'")
	}
	if !strings.Contains(contentStr, "governance:") {
		t.Error("Expected file to contain 'governance:'")
	}
	if !strings.Contains(contentStr, "max-new-items-per-run: 25") {
		t.Error("Expected file to contain 'max-new-items-per-run: 25'")
	}
	if !strings.Contains(contentStr, "max-discovery-items-per-run: 200") {
		t.Error("Expected file to contain 'max-discovery-items-per-run: 200'")
	}
	if !strings.Contains(contentStr, "max-discovery-pages-per-run: 10") {
		t.Error("Expected file to contain 'max-discovery-pages-per-run: 10'")
	}
	if !strings.Contains(contentStr, "max-project-updates-per-run: 10") {
		t.Error("Expected file to contain 'max-project-updates-per-run: 10'")
	}
	if !strings.Contains(contentStr, "max-comments-per-run: 10") {
		t.Error("Expected file to contain 'max-comments-per-run: 10'")
	}
	if !strings.Contains(contentStr, "cursor-glob: memory/campaigns/test-campaign/cursor.json") {
		t.Error("Expected file to contain 'cursor-glob: memory/campaigns/test-campaign/cursor.json'")
	}
}

func TestCreateSpecSkeleton_InvalidID_Empty(t *testing.T) {
	tmpDir := t.TempDir()

	_, err := CreateSpecSkeleton(tmpDir, "", false)
	if err == nil {
		t.Fatal("Expected error for empty ID")
	}

	if !strings.Contains(err.Error(), "id is required") {
		t.Errorf("Expected 'id is required' error, got: %v", err)
	}
}

func TestCreateSpecSkeleton_InvalidID_Uppercase(t *testing.T) {
	tmpDir := t.TempDir()

	_, err := CreateSpecSkeleton(tmpDir, "Test-Campaign", false)
	if err == nil {
		t.Fatal("Expected error for uppercase in ID")
	}

	if !strings.Contains(err.Error(), "lowercase letters, digits, and hyphens") {
		t.Errorf("Expected character restriction error, got: %v", err)
	}
}

func TestCreateSpecSkeleton_InvalidID_Underscore(t *testing.T) {
	tmpDir := t.TempDir()

	_, err := CreateSpecSkeleton(tmpDir, "test_campaign", false)
	if err == nil {
		t.Fatal("Expected error for underscore in ID")
	}

	if !strings.Contains(err.Error(), "lowercase letters, digits, and hyphens") {
		t.Errorf("Expected character restriction error, got: %v", err)
	}
}

func TestCreateSpecSkeleton_InvalidID_Space(t *testing.T) {
	tmpDir := t.TempDir()

	_, err := CreateSpecSkeleton(tmpDir, "test campaign", false)
	if err == nil {
		t.Fatal("Expected error for space in ID")
	}

	if !strings.Contains(err.Error(), "lowercase letters, digits, and hyphens") {
		t.Errorf("Expected character restriction error, got: %v", err)
	}
}

func TestCreateSpecSkeleton_FileExists_NoForce(t *testing.T) {
	tmpDir := t.TempDir()

	// Create first time
	_, err := CreateSpecSkeleton(tmpDir, "test-campaign", false)
	if err != nil {
		t.Fatalf("First CreateSpecSkeleton failed: %v", err)
	}

	// Try to create again without force
	_, err = CreateSpecSkeleton(tmpDir, "test-campaign", false)
	if err == nil {
		t.Fatal("Expected error when file exists without force flag")
	}

	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("Expected 'already exists' error, got: %v", err)
	}
}

func TestCreateSpecSkeleton_FileExists_WithForce(t *testing.T) {
	tmpDir := t.TempDir()

	// Create first time
	_, err := CreateSpecSkeleton(tmpDir, "test-campaign", false)
	if err != nil {
		t.Fatalf("First CreateSpecSkeleton failed: %v", err)
	}

	// Try to create again with force
	_, err = CreateSpecSkeleton(tmpDir, "test-campaign", true)
	if err != nil {
		t.Errorf("CreateSpecSkeleton with force should succeed: %v", err)
	}
}

func TestCreateSpecSkeleton_NameFormatting(t *testing.T) {
	tests := []struct {
		id           string
		expectedName string
	}{
		{"test", "Test"},
		{"test-campaign", "Test campaign"},
		{"security-q1-2025", "Security q1 2025"},
		{"org-modernization", "Org modernization"},
	}

	for _, tt := range tests {
		tmpDir := t.TempDir()

		_, err := CreateSpecSkeleton(tmpDir, tt.id, false)
		if err != nil {
			t.Fatalf("CreateSpecSkeleton failed for ID '%s': %v", tt.id, err)
		}

		// Load the created spec
		specs, err := LoadSpecs(tmpDir)
		if err != nil {
			t.Fatalf("LoadSpecs failed: %v", err)
		}

		if len(specs) != 1 {
			t.Fatalf("Expected 1 spec, got %d", len(specs))
		}

		if specs[0].Name != tt.expectedName {
			t.Errorf("For ID '%s', expected name '%s', got '%s'", tt.id, tt.expectedName, specs[0].Name)
		}
	}
}

func TestCreateSpecSkeleton_CreatesDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	// Don't create .github/workflows directory beforehand

	_, err := CreateSpecSkeleton(tmpDir, "test-campaign", false)
	if err != nil {
		t.Fatalf("CreateSpecSkeleton failed: %v", err)
	}

	// Verify .github/workflows directory was created
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	if _, err := os.Stat(workflowsDir); os.IsNotExist(err) {
		t.Error("Expected .github/workflows directory to be created")
	}
}

func TestCreateSpecSkeleton_ValidIDs(t *testing.T) {
	validIDs := []string{
		"test",
		"test-campaign",
		"test123",
		"test-123-campaign",
		"123-test",
		"a",
		"1",
	}

	for _, id := range validIDs {
		tmpDir := t.TempDir()

		_, err := CreateSpecSkeleton(tmpDir, id, false)
		if err != nil {
			t.Errorf("Expected ID '%s' to be valid, got error: %v", id, err)
		}
	}
}
