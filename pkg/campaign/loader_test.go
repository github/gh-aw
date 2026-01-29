//go:build !integration

package campaign

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadSpecs_EmptyDirectory(t *testing.T) {
	// Create temporary directory
	tmpDir := t.TempDir()
	campaignsDir := filepath.Join(tmpDir, ".github", "workflows")
	if err := os.MkdirAll(campaignsDir, 0755); err != nil {
		t.Fatalf("Failed to create .github/workflows directory: %v", err)
	}

	specs, err := LoadSpecs(tmpDir)
	if err != nil {
		t.Fatalf("LoadSpecs failed: %v", err)
	}

	if len(specs) != 0 {
		t.Errorf("Expected 0 specs in empty directory, got %d", len(specs))
	}
}

func TestLoadSpecs_NonExistentDirectory(t *testing.T) {
	tmpDir := t.TempDir()

	specs, err := LoadSpecs(tmpDir)
	if err != nil {
		t.Fatalf("LoadSpecs should not fail for non-existent .github/workflows directory: %v", err)
	}

	if len(specs) != 0 {
		t.Errorf("Expected 0 specs when .github/workflows directory doesn't exist, got %d", len(specs))
	}
}

func TestLoadSpecs_InvalidFrontmatter(t *testing.T) {
	tmpDir := t.TempDir()
	campaignsDir := filepath.Join(tmpDir, ".github", "workflows")
	if err := os.MkdirAll(campaignsDir, 0755); err != nil {
		t.Fatalf("Failed to create .github/workflows directory: %v", err)
	}

	// Create file with invalid frontmatter
	invalidFile := filepath.Join(campaignsDir, "invalid.campaign.md")
	content := `---
id: test
name: [invalid yaml here
---
Test content`
	if err := os.WriteFile(invalidFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	_, err := LoadSpecs(tmpDir)
	if err == nil {
		t.Fatalf("Expected error for invalid frontmatter, got nil")
	}

	if !strings.Contains(err.Error(), "failed to parse") {
		t.Errorf("Expected parse error, got: %v", err)
	}
}

func TestLoadSpecs_MissingFrontmatter(t *testing.T) {
	tmpDir := t.TempDir()
	campaignsDir := filepath.Join(tmpDir, ".github", "workflows")
	if err := os.MkdirAll(campaignsDir, 0755); err != nil {
		t.Fatalf("Failed to create .github/workflows directory: %v", err)
	}

	// Create file without frontmatter
	noFrontmatterFile := filepath.Join(campaignsDir, "no-frontmatter.campaign.md")
	content := `# Test Campaign

This file has no frontmatter.`
	if err := os.WriteFile(noFrontmatterFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	_, err := LoadSpecs(tmpDir)
	if err == nil {
		t.Fatalf("Expected error for missing frontmatter, got nil")
	}

	if !strings.Contains(err.Error(), "must start with YAML frontmatter") {
		t.Errorf("Expected frontmatter error, got: %v", err)
	}
}

func TestLoadSpecs_IDDefaults(t *testing.T) {
	tmpDir := t.TempDir()
	campaignsDir := filepath.Join(tmpDir, ".github", "workflows")
	if err := os.MkdirAll(campaignsDir, 0755); err != nil {
		t.Fatalf("Failed to create .github/workflows directory: %v", err)
	}

	// Create file without ID in frontmatter
	testFile := filepath.Join(campaignsDir, "test-campaign.campaign.md")
	content := `---
name: Test Campaign
version: v1
---
Test content`
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	specs, err := LoadSpecs(tmpDir)
	if err != nil {
		t.Fatalf("LoadSpecs failed: %v", err)
	}

	if len(specs) != 1 {
		t.Fatalf("Expected 1 spec, got %d", len(specs))
	}

	if specs[0].ID != "test-campaign" {
		t.Errorf("Expected ID 'test-campaign' (derived from filename), got '%s'", specs[0].ID)
	}
}

func TestLoadSpecs_NameDefaults(t *testing.T) {
	tmpDir := t.TempDir()
	campaignsDir := filepath.Join(tmpDir, ".github", "workflows")
	if err := os.MkdirAll(campaignsDir, 0755); err != nil {
		t.Fatalf("Failed to create .github/workflows directory: %v", err)
	}

	// Create file without name in frontmatter
	testFile := filepath.Join(campaignsDir, "test-id.campaign.md")
	content := `---
id: test-id
version: v1
---
Test content`
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	specs, err := LoadSpecs(tmpDir)
	if err != nil {
		t.Fatalf("LoadSpecs failed: %v", err)
	}

	if len(specs) != 1 {
		t.Fatalf("Expected 1 spec, got %d", len(specs))
	}

	if specs[0].Name != "test-id" {
		t.Errorf("Expected Name 'test-id' (derived from ID), got '%s'", specs[0].Name)
	}
}

func TestLoadSpecs_ConfigPath(t *testing.T) {
	tmpDir := t.TempDir()
	campaignsDir := filepath.Join(tmpDir, ".github", "workflows")
	if err := os.MkdirAll(campaignsDir, 0755); err != nil {
		t.Fatalf("Failed to create .github/workflows directory: %v", err)
	}

	testFile := filepath.Join(campaignsDir, "test.campaign.md")
	content := `---
id: test
name: Test
---
Test content`
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	specs, err := LoadSpecs(tmpDir)
	if err != nil {
		t.Fatalf("LoadSpecs failed: %v", err)
	}

	if len(specs) != 1 {
		t.Fatalf("Expected 1 spec, got %d", len(specs))
	}

	expectedPath := ".github/workflows/test.campaign.md"
	if specs[0].ConfigPath != expectedPath {
		t.Errorf("Expected ConfigPath '%s', got '%s'", expectedPath, specs[0].ConfigPath)
	}
}

func TestLoadSpecs_MultipleSpecs(t *testing.T) {
	tmpDir := t.TempDir()
	campaignsDir := filepath.Join(tmpDir, ".github", "workflows")
	if err := os.MkdirAll(campaignsDir, 0755); err != nil {
		t.Fatalf("Failed to create .github/workflows directory: %v", err)
	}

	// Create multiple campaign files
	campaigns := []struct {
		filename string
		id       string
	}{
		{"campaign1.campaign.md", "campaign1"},
		{"campaign2.campaign.md", "campaign2"},
		{"campaign3.campaign.md", "campaign3"},
	}

	for _, c := range campaigns {
		content := `---
id: ` + c.id + `
name: ` + c.id + `
---
Content`
		testFile := filepath.Join(campaignsDir, c.filename)
		if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to write test file %s: %v", c.filename, err)
		}
	}

	specs, err := LoadSpecs(tmpDir)
	if err != nil {
		t.Fatalf("LoadSpecs failed: %v", err)
	}

	if len(specs) != 3 {
		t.Fatalf("Expected 3 specs, got %d", len(specs))
	}

	// Verify all IDs are present
	foundIDs := make(map[string]bool)
	for _, spec := range specs {
		foundIDs[spec.ID] = true
	}

	for _, c := range campaigns {
		if !foundIDs[c.id] {
			t.Errorf("Expected to find campaign with ID '%s'", c.id)
		}
	}
}
