//go:build !integration

package parser_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestImportTopologicalSort tests that imports are sorted in topological order
// (roots first, dependencies before dependents)
func TestImportTopologicalSort(t *testing.T) {
	tests := []struct {
		name          string
		files         map[string]string // filename -> content
		mainImports   []string          // imports in the main file
		expectedOrder []string          // expected order of imports (roots first)
	}{
		{
			name: "linear dependency chain",
			files: map[string]string{
				"a.md": `---
imports:
  - b.md
tools:
  tool-a: {}
---`,
				"b.md": `---
imports:
  - c.md
tools:
  tool-b: {}
---`,
				"c.md": `---
tools:
  tool-c: {}
---`,
			},
			mainImports:   []string{"a.md"},
			expectedOrder: []string{"c.md", "b.md", "a.md"},
		},
		{
			name: "multiple roots",
			files: map[string]string{
				"a.md": `---
tools:
  tool-a: {}
---`,
				"b.md": `---
tools:
  tool-b: {}
---`,
				"c.md": `---
tools:
  tool-c: {}
---`,
			},
			mainImports:   []string{"a.md", "b.md", "c.md"},
			expectedOrder: []string{"a.md", "b.md", "c.md"}, // alphabetical when all are roots
		},
		{
			name: "diamond dependency",
			files: map[string]string{
				"a.md": `---
imports:
  - c.md
tools:
  tool-a: {}
---`,
				"b.md": `---
imports:
  - c.md
tools:
  tool-b: {}
---`,
				"c.md": `---
tools:
  tool-c: {}
---`,
			},
			mainImports:   []string{"a.md", "b.md"},
			expectedOrder: []string{"c.md", "a.md", "b.md"},
		},
		{
			name: "complex tree",
			files: map[string]string{
				"a.md": `---
imports:
  - c.md
  - d.md
tools:
  tool-a: {}
---`,
				"b.md": `---
imports:
  - e.md
tools:
  tool-b: {}
---`,
				"c.md": `---
imports:
  - f.md
tools:
  tool-c: {}
---`,
				"d.md": `---
tools:
  tool-d: {}
---`,
				"e.md": `---
tools:
  tool-e: {}
---`,
				"f.md": `---
tools:
  tool-f: {}
---`,
			},
			mainImports: []string{"a.md", "b.md"},
			// Expected: roots (d, e, f) first, then their dependents
			// Multiple valid orderings exist due to independence between branches
			// Key constraints: f before c, c and d before a, e before b
			expectedOrder: []string{"d.md", "e.md", "b.md", "f.md", "c.md", "a.md"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary directory
			tempDir := testutil.TempDir(t, "import-topo-*")

			// Create all test files
			for filename, content := range tt.files {
				filePath := filepath.Join(tempDir, filename)
				err := os.WriteFile(filePath, []byte(content), 0644)
				require.NoError(t, err, "Failed to create test file %s", filename)
			}

			// Create frontmatter with imports
			frontmatter := map[string]any{
				"imports": tt.mainImports,
			}

			// Process imports
			result, err := parser.ProcessImportsFromFrontmatterWithManifest(frontmatter, tempDir, nil)
			require.NoError(t, err, "ProcessImportsFromFrontmatterWithManifest should not fail")

			// Verify the order
			assert.Len(t, result.ImportedFiles, len(tt.expectedOrder),
				"Number of imported files should match expected")

			// Check that the order matches expected topological order
			for i, expected := range tt.expectedOrder {
				if i < len(result.ImportedFiles) {
					assert.Equal(t, expected, result.ImportedFiles[i],
						"Import at position %d should be %s but got %s", i, expected, result.ImportedFiles[i])
				}
			}

			t.Logf("Expected order: %v", tt.expectedOrder)
			t.Logf("Actual order:   %v", result.ImportedFiles)
		})
	}
}

// TestImportTopologicalSortWithSections tests topological sorting with section references
func TestImportTopologicalSortWithSections(t *testing.T) {
	tempDir := testutil.TempDir(t, "import-topo-sections-*")

	// Create files with sections
	files := map[string]string{
		"a.md": `---
imports:
  - b.md#Tools
tools:
  tool-a: {}
---`,
		"b.md": `---
tools:
  tool-b: {}
---

## Tools

Tool configuration here.`,
	}

	for filename, content := range files {
		filePath := filepath.Join(tempDir, filename)
		err := os.WriteFile(filePath, []byte(content), 0644)
		require.NoError(t, err)
	}

	frontmatter := map[string]any{
		"imports": []string{"a.md"},
	}

	result, err := parser.ProcessImportsFromFrontmatterWithManifest(frontmatter, tempDir, nil)
	require.NoError(t, err)

	// b.md should come before a.md (even with section reference)
	assert.Len(t, result.ImportedFiles, 2)
	assert.Equal(t, "b.md#Tools", result.ImportedFiles[0])
	assert.Equal(t, "a.md", result.ImportedFiles[1])
}

// TestImportTopologicalSortPreservesAlphabeticalForSameLevel tests that
// imports at the same level (same in-degree) are sorted alphabetically
func TestImportTopologicalSortPreservesAlphabeticalForSameLevel(t *testing.T) {
	tempDir := testutil.TempDir(t, "import-topo-alpha-*")

	// Create multiple root files (no dependencies)
	files := map[string]string{
		"z-root.md": `---
tools:
  tool-z: {}
---`,
		"a-root.md": `---
tools:
  tool-a: {}
---`,
		"m-root.md": `---
tools:
  tool-m: {}
---`,
	}

	for filename, content := range files {
		filePath := filepath.Join(tempDir, filename)
		err := os.WriteFile(filePath, []byte(content), 0644)
		require.NoError(t, err)
	}

	frontmatter := map[string]any{
		"imports": []string{"z-root.md", "a-root.md", "m-root.md"},
	}

	result, err := parser.ProcessImportsFromFrontmatterWithManifest(frontmatter, tempDir, nil)
	require.NoError(t, err)

	// All are roots, should be sorted alphabetically
	assert.Len(t, result.ImportedFiles, 3)
	assert.Equal(t, "a-root.md", result.ImportedFiles[0])
	assert.Equal(t, "m-root.md", result.ImportedFiles[1])
	assert.Equal(t, "z-root.md", result.ImportedFiles[2])
}
