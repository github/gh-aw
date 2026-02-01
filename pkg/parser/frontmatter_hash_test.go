//go:build !integration

package parser

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestComputeFrontmatterHash_EmptyFrontmatter(t *testing.T) {
	frontmatter := map[string]any{}
	baseDir := "."
	cache := NewImportCache("")

	hash, err := ComputeFrontmatterHash(frontmatter, baseDir, cache)
	require.NoError(t, err, "Should compute hash for empty frontmatter")
	assert.Len(t, hash, 64, "Hash should be 64 characters (SHA-256 hex)")
	assert.Regexp(t, "^[a-f0-9]{64}$", hash, "Hash should be lowercase hex")
}

func TestComputeFrontmatterHash_SimpleFrontmatter(t *testing.T) {
	frontmatter := map[string]any{
		"engine":      "copilot",
		"description": "Test workflow",
		"on": map[string]any{
			"schedule": "daily",
		},
	}
	baseDir := "."
	cache := NewImportCache("")

	hash, err := ComputeFrontmatterHash(frontmatter, baseDir, cache)
	require.NoError(t, err, "Should compute hash for simple frontmatter")
	assert.Len(t, hash, 64, "Hash should be 64 characters")

	// Compute again to verify determinism
	hash2, err := ComputeFrontmatterHash(frontmatter, baseDir, cache)
	require.NoError(t, err, "Should compute hash again")
	assert.Equal(t, hash, hash2, "Hash should be deterministic")
}

func TestComputeFrontmatterHash_KeyOrdering(t *testing.T) {
	// Test that different key ordering produces the same hash
	frontmatter1 := map[string]any{
		"engine":      "copilot",
		"description": "Test",
		"on":          map[string]any{"schedule": "daily"},
	}

	frontmatter2 := map[string]any{
		"on":          map[string]any{"schedule": "daily"},
		"description": "Test",
		"engine":      "copilot",
	}

	cache := NewImportCache("")

	hash1, err := ComputeFrontmatterHash(frontmatter1, ".", cache)
	require.NoError(t, err, "Should compute hash for frontmatter1")

	hash2, err := ComputeFrontmatterHash(frontmatter2, ".", cache)
	require.NoError(t, err, "Should compute hash for frontmatter2")

	assert.Equal(t, hash1, hash2, "Hashes should be identical regardless of key order")
}

func TestComputeFrontmatterHash_NestedObjects(t *testing.T) {
	frontmatter := map[string]any{
		"tools": map[string]any{
			"playwright": map[string]any{
				"version": "v1.41.0",
				"domains": []any{"github.com", "example.com"},
			},
			"mcp": map[string]any{
				"server": "remote",
			},
		},
		"permissions": map[string]any{
			"contents": "read",
			"actions":  "write",
		},
	}

	cache := NewImportCache("")

	hash, err := ComputeFrontmatterHash(frontmatter, ".", cache)
	require.NoError(t, err, "Should compute hash for nested objects")
	assert.Len(t, hash, 64, "Hash should be 64 characters")
}

func TestComputeFrontmatterHash_Arrays(t *testing.T) {
	frontmatter := map[string]any{
		"labels": []any{"audit", "automation", "daily"},
		"bots":   []any{"copilot"},
		"steps": []any{
			map[string]any{
				"name": "Step 1",
				"run":  "echo 'test'",
			},
			map[string]any{
				"name": "Step 2",
				"run":  "echo 'test2'",
			},
		},
	}

	cache := NewImportCache("")

	hash, err := ComputeFrontmatterHash(frontmatter, ".", cache)
	require.NoError(t, err, "Should compute hash with arrays")
	assert.Len(t, hash, 64, "Hash should be 64 characters")

	// Array order matters - different order should produce different hash
	frontmatter2 := map[string]any{
		"labels": []any{"automation", "audit", "daily"}, // Different order
		"bots":   []any{"copilot"},
		"steps": []any{
			map[string]any{
				"name": "Step 1",
				"run":  "echo 'test'",
			},
			map[string]any{
				"name": "Step 2",
				"run":  "echo 'test2'",
			},
		},
	}

	hash2, err := ComputeFrontmatterHash(frontmatter2, ".", cache)
	require.NoError(t, err, "Should compute hash with reordered arrays")
	assert.NotEqual(t, hash, hash2, "Array order should affect hash")
}

func TestComputeFrontmatterHash_AllFieldTypes(t *testing.T) {
	frontmatter := map[string]any{
		"engine":          "claude",
		"description":     "Full workflow",
		"tracker-id":      "test-workflow",
		"timeout-minutes": 30,
		"on": map[string]any{
			"schedule":          "daily",
			"workflow_dispatch": true,
		},
		"permissions": map[string]any{
			"contents": "read",
			"actions":  "read",
		},
		"tools": map[string]any{
			"playwright": map[string]any{
				"version": "v1.41.0",
			},
		},
		"network": map[string]any{
			"allowed": []any{"api.github.com"},
		},
		"runtimes": map[string]any{
			"node": map[string]any{
				"version": "20",
			},
		},
		"labels": []any{"test"},
		"bots":   []any{"copilot"},
	}

	cache := NewImportCache("")

	hash, err := ComputeFrontmatterHash(frontmatter, ".", cache)
	require.NoError(t, err, "Should compute hash with all field types")
	assert.Len(t, hash, 64, "Hash should be 64 characters")
}

func TestMarshalSorted_Primitives(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		expected string
	}{
		{"string", "test", `"test"`},
		{"number", 42, "42"},
		{"float", 3.14, "3.14"},
		{"bool true", true, "true"},
		{"bool false", false, "false"},
		{"nil", nil, "null"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := marshalSorted(tt.input)
			assert.Equal(t, tt.expected, result, "Should marshal primitive correctly")
		})
	}
}

func TestMarshalSorted_EmptyContainers(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		expected string
	}{
		{"empty object", map[string]any{}, "{}"},
		{"empty array", []any{}, "[]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := marshalSorted(tt.input)
			assert.Equal(t, tt.expected, result, "Should marshal empty container correctly")
		})
	}
}

func TestMarshalSorted_SortedKeys(t *testing.T) {
	input := map[string]any{
		"zebra":   1,
		"apple":   2,
		"banana":  3,
		"charlie": 4,
	}

	result := marshalSorted(input)
	expected := `{"apple":2,"banana":3,"charlie":4,"zebra":1}`
	assert.Equal(t, expected, result, "Keys should be sorted alphabetically")
}

func TestMarshalSorted_NestedSorting(t *testing.T) {
	input := map[string]any{
		"outer": map[string]any{
			"z": 1,
			"a": 2,
		},
		"another": map[string]any{
			"nested": map[string]any{
				"y": 3,
				"b": 4,
			},
		},
	}

	result := marshalSorted(input)
	// Keys at all levels should be sorted
	assert.Contains(t, result, `"another":`, "Should contain outer key")
	assert.Contains(t, result, `"outer":`, "Should contain outer key")
	assert.Contains(t, result, `"a":2`, "Should contain sorted nested keys")
	assert.Contains(t, result, `"z":1`, "Should contain sorted nested keys")
}

func TestComputeFrontmatterHashFromFile_NonExistent(t *testing.T) {
	cache := NewImportCache("")

	hash, err := ComputeFrontmatterHashFromFile("/nonexistent/file.md", cache)
	require.Error(t, err, "Should error for nonexistent file")
	assert.Empty(t, hash, "Hash should be empty on error")
}

func TestComputeFrontmatterHashFromFile_ValidFile(t *testing.T) {
	// Create a temporary workflow file
	tempDir := t.TempDir()
	workflowFile := filepath.Join(tempDir, "test-workflow.md")

	content := `---
engine: copilot
description: Test workflow
on:
  schedule: daily
---

# Test Workflow

This is a test workflow.
`

	err := os.WriteFile(workflowFile, []byte(content), 0644)
	require.NoError(t, err, "Should write test file")

	cache := NewImportCache("")

	hash, err := ComputeFrontmatterHashFromFile(workflowFile, cache)
	require.NoError(t, err, "Should compute hash from file")
	assert.Len(t, hash, 64, "Hash should be 64 characters")

	// Compute again to verify determinism
	hash2, err := ComputeFrontmatterHashFromFile(workflowFile, cache)
	require.NoError(t, err, "Should compute hash again")
	assert.Equal(t, hash, hash2, "Hash should be deterministic")
}

func TestComputeFrontmatterHash_WithImports(t *testing.T) {
	// Create a temporary directory structure
	tempDir := t.TempDir()

	// Create a shared workflow
	sharedDir := filepath.Join(tempDir, "shared")
	err := os.MkdirAll(sharedDir, 0755)
	require.NoError(t, err, "Should create shared directory")

	sharedFile := filepath.Join(sharedDir, "common.md")
	sharedContent := `---
tools:
  playwright:
    version: v1.41.0
labels:
  - shared
  - common
---

# Shared Content

This is shared.
`
	err = os.WriteFile(sharedFile, []byte(sharedContent), 0644)
	require.NoError(t, err, "Should write shared file")

	// Create a main workflow that imports the shared workflow
	mainFile := filepath.Join(tempDir, "main.md")
	mainContent := `---
engine: copilot
description: Main workflow
imports:
  - shared/common.md
labels:
  - main
---

# Main Workflow

This is the main workflow.
`
	err = os.WriteFile(mainFile, []byte(mainContent), 0644)
	require.NoError(t, err, "Should write main file")

	cache := NewImportCache("")

	hash, err := ComputeFrontmatterHashFromFile(mainFile, cache)
	require.NoError(t, err, "Should compute hash with imports")
	assert.Len(t, hash, 64, "Hash should be 64 characters")

	// The hash should include contributions from the imported file
	// We can't easily verify the exact hash, but we can verify it's deterministic
	hash2, err := ComputeFrontmatterHashFromFile(mainFile, cache)
	require.NoError(t, err, "Should compute hash again with imports")
	assert.Equal(t, hash, hash2, "Hash with imports should be deterministic")
}

func TestBuildCanonicalFrontmatter(t *testing.T) {
	frontmatter := map[string]any{
		"engine":      "copilot",
		"description": "Test",
		"on": map[string]any{
			"schedule": "daily",
		},
		"tools": map[string]any{
			"playwright": map[string]any{
				"version": "v1.41.0",
			},
		},
	}

	result := &ImportsResult{
		MergedTools:   `{"mcp":{"server":"remote"}}`,
		MergedEngines: []string{"claude", "copilot"},
		ImportedFiles: []string{"shared/common.md"},
	}

	canonical := buildCanonicalFrontmatter(frontmatter, result)

	// Verify expected fields are present
	assert.Equal(t, "copilot", canonical["engine"], "Should include engine")
	assert.Equal(t, "Test", canonical["description"], "Should include description")
	assert.NotNil(t, canonical["on"], "Should include on")
	assert.NotNil(t, canonical["tools"], "Should include tools")

	// Verify merged content is included
	assert.JSONEq(t, `{"mcp":{"server":"remote"}}`, canonical["merged-tools"].(string), "Should include merged tools")
	assert.Equal(t, []string{"claude", "copilot"}, canonical["merged-engines"], "Should include merged engines")
	assert.Equal(t, []string{"shared/common.md"}, canonical["imports"], "Should include imported files")
}

func TestComputeFrontmatterHashFromFileWithReader_CustomReader(t *testing.T) {
	// Create in-memory file system mock
	mockFS := map[string]string{
		"/test/workflow.md": `---
engine: copilot
description: Test workflow
---

# Workflow Body`,
		"/test/shared/imported.md": `---
tools:
  bash: true
---

# Imported Content`,
	}

	// Create custom file reader
	customReader := func(filePath string) ([]byte, error) {
		content, exists := mockFS[filePath]
		if !exists {
			return nil, os.ErrNotExist
		}
		return []byte(content), nil
	}

	// Test basic hash computation
	hash, err := ComputeFrontmatterHashFromFileWithReader("/test/workflow.md", nil, customReader)
	require.NoError(t, err, "Should compute hash with custom reader")
	assert.Len(t, hash, 64, "Hash should be 64 characters")
	assert.Regexp(t, "^[a-f0-9]{64}$", hash, "Hash should be lowercase hex")

	// Verify determinism
	hash2, err := ComputeFrontmatterHashFromFileWithReader("/test/workflow.md", nil, customReader)
	require.NoError(t, err, "Should compute hash again")
	assert.Equal(t, hash, hash2, "Hash should be deterministic")
}

func TestComputeFrontmatterHashFromFileWithReader_WithImports(t *testing.T) {
	// Create in-memory file system mock with imports
	mockFS := map[string]string{
		"/test/workflow.md": `---
engine: copilot
imports:
  - shared/imported.md
---

# Main Workflow`,
		"/test/shared/imported.md": `---
tools:
  bash: true
---

# Imported Content`,
	}

	// Create custom file reader
	customReader := func(filePath string) ([]byte, error) {
		content, exists := mockFS[filePath]
		if !exists {
			return nil, os.ErrNotExist
		}
		return []byte(content), nil
	}

	// Test hash computation with imports
	hash, err := ComputeFrontmatterHashFromFileWithReader("/test/workflow.md", nil, customReader)
	require.NoError(t, err, "Should compute hash with imports using custom reader")
	assert.Len(t, hash, 64, "Hash should be 64 characters")
	assert.Regexp(t, "^[a-f0-9]{64}$", hash, "Hash should be lowercase hex")
}
