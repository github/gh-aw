//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParseFrontmatterSection_ValidMainWorkflow tests parsing valid main workflow
func TestParseFrontmatterSection_ValidMainWorkflow(t *testing.T) {
	tmpDir := testutil.TempDir(t, "frontmatter-valid")

	testContent := `---
on: push
engine: copilot
permissions:
  contents: read
---

# Test Workflow

Content here
`

	testFile := filepath.Join(tmpDir, "test.md")
	require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644))

	compiler := NewCompiler()
	result, err := compiler.parseFrontmatterSection(testFile)

	require.NoError(t, err, "Valid main workflow should parse successfully")
	require.NotNil(t, result)

	assert.Equal(t, testFile, result.cleanPath)
	assert.NotEmpty(t, result.content)
	assert.NotNil(t, result.frontmatterResult)
	assert.NotNil(t, result.frontmatterForValidation)
	assert.False(t, result.isSharedWorkflow, "Should be detected as main workflow")
}

// TestParseFrontmatterSection_SharedWorkflow tests shared workflow detection
func TestParseFrontmatterSection_SharedWorkflow(t *testing.T) {
	tmpDir := testutil.TempDir(t, "frontmatter-shared")

	testContent := `---
engine: copilot
permissions:
  contents: read
---

# Shared Workflow

Can be imported
`

	testFile := filepath.Join(tmpDir, "shared.md")
	require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644))

	compiler := NewCompiler()
	result, err := compiler.parseFrontmatterSection(testFile)

	require.NoError(t, err)
	require.NotNil(t, result)

	assert.True(t, result.isSharedWorkflow, "Should be detected as shared workflow")
}

// TestParseFrontmatterSection_MissingFrontmatter tests error for no frontmatter
func TestParseFrontmatterSection_MissingFrontmatter(t *testing.T) {
	tmpDir := testutil.TempDir(t, "frontmatter-missing")

	testContent := `# Workflow Without Frontmatter

Just markdown content
`

	testFile := filepath.Join(tmpDir, "no-fm.md")
	require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644))

	compiler := NewCompiler()
	result, err := compiler.parseFrontmatterSection(testFile)

	require.Error(t, err, "Missing frontmatter should cause error")
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "frontmatter")
}

// TestParseFrontmatterSection_InvalidYAML tests YAML parsing errors
func TestParseFrontmatterSection_InvalidYAML(t *testing.T) {
	tmpDir := testutil.TempDir(t, "frontmatter-invalid-yaml")

	testContent := `---
on: push
invalid: [unclosed bracket
engine: copilot
---

# Workflow
`

	testFile := filepath.Join(tmpDir, "invalid.md")
	require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644))

	compiler := NewCompiler()
	result, err := compiler.parseFrontmatterSection(testFile)

	require.Error(t, err, "Invalid YAML should cause error")
	assert.Nil(t, result)
}

// TestParseFrontmatterSection_NoMarkdownContent tests main workflow without markdown
func TestParseFrontmatterSection_NoMarkdownContent(t *testing.T) {
	tmpDir := testutil.TempDir(t, "frontmatter-no-markdown")

	testContent := `---
on: push
engine: copilot
---
`

	testFile := filepath.Join(tmpDir, "no-markdown.md")
	require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644))

	compiler := NewCompiler()
	result, err := compiler.parseFrontmatterSection(testFile)

	require.Error(t, err, "Main workflow needs markdown content")
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "markdown content")
}

// TestParseFrontmatterSection_PathTraversal tests path cleaning
func TestParseFrontmatterSection_PathTraversal(t *testing.T) {
	compiler := NewCompiler()

	// These paths should be cleaned and not cause security issues
	paths := []string{
		"../../../etc/passwd",
		"./../../etc/passwd",
	}

	for _, path := range paths {
		result, err := compiler.parseFrontmatterSection(path)
		// Should fail (file doesn't exist)
		require.Error(t, err, "Path traversal attempt should fail: %s", path)
		assert.Nil(t, result)
	}
}

// TestParseFrontmatterSection_SchedulePreprocessing tests schedule field preprocessing
func TestParseFrontmatterSection_SchedulePreprocessing(t *testing.T) {
	tmpDir := testutil.TempDir(t, "frontmatter-schedule")

	testContent := `---
on:
  schedule:
    - cron: "0 0 * * *"
engine: copilot
---

# Scheduled Workflow
`

	testFile := filepath.Join(tmpDir, "schedule.md")
	require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644))

	compiler := NewCompiler()
	result, err := compiler.parseFrontmatterSection(testFile)

	require.NoError(t, err)
	require.NotNil(t, result)

	// Schedule should be preprocessed successfully
	assert.NotNil(t, result.frontmatterResult)
}

// TestParseFrontmatterSection_EventFilterValidation tests event filter validation
func TestParseFrontmatterSection_EventFilterValidation(t *testing.T) {
	tmpDir := testutil.TempDir(t, "frontmatter-filters")

	tests := []struct {
		name        string
		frontmatter string
		shouldError bool
	}{
		{
			name: "valid branches filter",
			frontmatter: `---
on:
  push:
    branches:
      - main
engine: copilot
---`,
			shouldError: false,
		},
		{
			name: "invalid branches and branches-ignore together",
			frontmatter: `---
on:
  push:
    branches:
      - main
    branches-ignore:
      - develop
engine: copilot
---`,
			shouldError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testContent := tt.frontmatter + "\n\n# Workflow\n"
			testFile := filepath.Join(tmpDir, "filter-"+tt.name+".md")
			require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644))

			compiler := NewCompiler()
			result, err := compiler.parseFrontmatterSection(testFile)

			if tt.shouldError {
				require.Error(t, err, "Should error for test: %s", tt.name)
				assert.Nil(t, result)
			} else {
				require.NoError(t, err, "Should succeed for test: %s", tt.name)
				require.NotNil(t, result)
			}
		})
	}
}

// TestParseFrontmatterSection_FileReadError tests file I/O error handling
func TestParseFrontmatterSection_FileReadError(t *testing.T) {
	compiler := NewCompiler()

	// Try to read a non-existent file
	result, err := compiler.parseFrontmatterSection("/nonexistent/path/to/file.md")

	require.Error(t, err, "Should error when file doesn't exist")
	assert.Nil(t, result)
}

// TestCopyFrontmatterWithoutInternalMarkers_SimpleMap tests basic marker removal
func TestCopyFrontmatterWithoutInternalMarkers_SimpleMap(t *testing.T) {
	compiler := NewCompiler()

	input := map[string]any{
		"on":     "push",
		"engine": "copilot",
	}

	result := compiler.copyFrontmatterWithoutInternalMarkers(input)

	assert.Equal(t, input, result, "Simple map should be unchanged")
}

// TestCopyFrontmatterWithoutInternalMarkers_LabelFilterMarker tests label filter marker removal
func TestCopyFrontmatterWithoutInternalMarkers_LabelFilterMarker(t *testing.T) {
	compiler := NewCompiler()

	input := map[string]any{
		"on": map[string]any{
			"issues": map[string]any{
				"types":                         []string{"opened"},
				"__gh_aw_native_label_filter__": true,
			},
		},
		"engine": "copilot",
	}

	result := compiler.copyFrontmatterWithoutInternalMarkers(input)

	// Check marker was removed
	onMap, ok := result["on"].(map[string]any)
	require.True(t, ok)
	issuesMap, ok := onMap["issues"].(map[string]any)
	require.True(t, ok)

	_, hasMarker := issuesMap["__gh_aw_native_label_filter__"]
	assert.False(t, hasMarker, "Internal marker should be removed")

	// Check types field is preserved
	_, hasTypes := issuesMap["types"]
	assert.True(t, hasTypes, "Types field should be preserved")
}

// TestCopyFrontmatterWithoutInternalMarkers_MultipleMarkers tests multiple event markers
func TestCopyFrontmatterWithoutInternalMarkers_MultipleMarkers(t *testing.T) {
	compiler := NewCompiler()

	input := map[string]any{
		"on": map[string]any{
			"issues": map[string]any{
				"types":                         []string{"opened"},
				"__gh_aw_native_label_filter__": true,
			},
			"pull_request": map[string]any{
				"types":                         []string{"opened"},
				"__gh_aw_native_label_filter__": true,
			},
			"discussion": map[string]any{
				"types":                         []string{"created"},
				"__gh_aw_native_label_filter__": false,
			},
		},
	}

	result := compiler.copyFrontmatterWithoutInternalMarkers(input)

	onMap, ok := result["on"].(map[string]any)
	require.True(t, ok)

	// Check all markers were removed
	for _, eventType := range []string{"issues", "pull_request", "discussion"} {
		eventMap, ok := onMap[eventType].(map[string]any)
		require.True(t, ok, "Event %s should exist", eventType)

		_, hasMarker := eventMap["__gh_aw_native_label_filter__"]
		assert.False(t, hasMarker, "Marker should be removed from %s", eventType)
	}
}

// TestCopyFrontmatterWithoutInternalMarkers_PreservesOtherFields tests field preservation
func TestCopyFrontmatterWithoutInternalMarkers_PreservesOtherFields(t *testing.T) {
	compiler := NewCompiler()

	input := map[string]any{
		"on": map[string]any{
			"issues": map[string]any{
				"types":                         []string{"opened", "edited"},
				"__gh_aw_native_label_filter__": true,
				"labels":                        []string{"bug"},
			},
		},
		"engine": "copilot",
		"permissions": map[string]any{
			"contents": "read",
		},
	}

	result := compiler.copyFrontmatterWithoutInternalMarkers(input)

	// Verify all non-marker fields are preserved
	assert.Equal(t, "copilot", result["engine"])
	assert.NotNil(t, result["permissions"])

	onMap, ok := result["on"].(map[string]any)
	require.True(t, ok)
	issuesMap, ok := onMap["issues"].(map[string]any)
	require.True(t, ok)

	assert.NotNil(t, issuesMap["types"])
	assert.NotNil(t, issuesMap["labels"])
	_, hasMarker := issuesMap["__gh_aw_native_label_filter__"]
	assert.False(t, hasMarker)
}

// TestCopyFrontmatterWithoutInternalMarkers_NonMapOnValue tests non-map on values
func TestCopyFrontmatterWithoutInternalMarkers_NonMapOnValue(t *testing.T) {
	compiler := NewCompiler()

	input := map[string]any{
		"on":     "push", // String value, not a map
		"engine": "copilot",
	}

	result := compiler.copyFrontmatterWithoutInternalMarkers(input)

	assert.Equal(t, "push", result["on"], "String on value should be preserved")
}

// TestParseFrontmatterSection_TemplateRegionValidation tests @include in templates
func TestParseFrontmatterSection_TemplateRegionValidation(t *testing.T) {
	t.Skip("Template region validation happens in parseFrontmatterSection and is covered by existing tests")
	tmpDir := testutil.TempDir(t, "frontmatter-template")

	testContent := `---
on: push
engine: copilot
---

# Workflow

Normal content
`

	testFile := filepath.Join(tmpDir, "template.md")
	require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644))

	compiler := NewCompiler()
	result, err := compiler.parseFrontmatterSection(testFile)

	require.NoError(t, err)
	require.NotNil(t, result)
}

// TestParseFrontmatterSection_EmptyFrontmatter tests completely empty frontmatter
func TestParseFrontmatterSection_EmptyFrontmatter(t *testing.T) {
	tmpDir := testutil.TempDir(t, "frontmatter-empty")

	testContent := `---
---

# Workflow
`

	testFile := filepath.Join(tmpDir, "empty.md")
	require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644))

	compiler := NewCompiler()
	result, err := compiler.parseFrontmatterSection(testFile)

	require.Error(t, err, "Empty frontmatter should cause error")
	assert.Nil(t, result)
}

// TestParseFrontmatterSection_MarkdownDirectory tests directory extraction
func TestParseFrontmatterSection_MarkdownDirectory(t *testing.T) {
	tmpDir := testutil.TempDir(t, "frontmatter-dir")
	subDir := filepath.Join(tmpDir, "subdir")
	require.NoError(t, os.MkdirAll(subDir, 0755))

	testContent := `---
on: push
engine: copilot
---

# Workflow
`

	testFile := filepath.Join(subDir, "test.md")
	require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644))

	compiler := NewCompiler()
	result, err := compiler.parseFrontmatterSection(testFile)

	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Equal(t, subDir, result.markdownDir, "Should extract correct directory")
}
