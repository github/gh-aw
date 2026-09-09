//go:build !integration

package parser_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestImportCycleDetection_TwoFiles tests that a 2-file cycle is detected and reported
func TestImportCycleDetection_TwoFiles(t *testing.T) {
	tempDir := testutil.TempDir(t, "test-*")

	// Create file A that imports B
	fileA := filepath.Join(tempDir, "a.md")
	fileAContent := `---
imports:
  - b.md
---
# File A
`
	require.NoError(t, os.WriteFile(fileA, []byte(fileAContent), 0644), "Failed to write file A")

	// Create file B that imports A (creating a cycle)
	fileB := filepath.Join(tempDir, "b.md")
	fileBContent := `---
imports:
  - a.md
---
# File B
`
	require.NoError(t, os.WriteFile(fileB, []byte(fileBContent), 0644), "Failed to write file B")

	// Process imports from file A - should detect cycle
	frontmatter := map[string]any{
		"imports": []string{"a.md"},
	}

	_, err := parser.ProcessImportsFromFrontmatterWithSource(frontmatter, tempDir, nil, fileA, fileAContent)
	require.Error(t, err, "Should detect import cycle")

	// Verify error is an ImportCycleError
	var cycleErr *parser.ImportCycleError
	require.ErrorAs(t, err, &cycleErr, "Error should be ImportCycleError")

	// Verify the cycle chain is present
	if cycleErr != nil {
		assert.NotEmpty(t, cycleErr.Chain, "Cycle chain should not be empty")
		assert.GreaterOrEqual(t, len(cycleErr.Chain), 2, "Cycle chain should have at least 2 elements")

		// First and last elements should form the cycle
		firstFile := cycleErr.Chain[0]
		lastFile := cycleErr.Chain[len(cycleErr.Chain)-1]
		assert.Equal(t, firstFile, lastFile, "Cycle should loop back to starting file")
	}
}

// TestImportCycleDetection_SelfImport tests that a file importing itself is detected
// as a cycle and reported with an actionable chain (e.g. ["a.md", "a.md"]).
func TestImportCycleDetection_SelfImport(t *testing.T) {
	tempDir := testutil.TempDir(t, "test-self-import-*")

	fileA := filepath.Join(tempDir, "a.md")
	fileAContent := `---
imports:
  - a.md
---
# File A
`
	require.NoError(t, os.WriteFile(fileA, []byte(fileAContent), 0644), "Failed to write file A")

	frontmatter := map[string]any{
		"imports": []string{"a.md"},
	}

	_, err := parser.ProcessImportsFromFrontmatterWithSource(frontmatter, tempDir, nil, fileA, fileAContent)
	require.Error(t, err, "Should detect self-import cycle")

	var cycleErr *parser.ImportCycleError
	require.ErrorAs(t, err, &cycleErr, "Error should be ImportCycleError")
	require.NotNil(t, cycleErr)
	assert.Equal(t, []string{"a.md", "a.md"}, cycleErr.Chain)
	assert.Contains(t, err.Error(), "remove the import of \"a.md\" from \"a.md\"")
}

// TestImportCycleDetection_FourFiles tests that a 4-file cycle (A→B→C→D→B) is detected
// This is the exact scenario from the issue requirements
func TestImportCycleDetection_FourFiles(t *testing.T) {
	tempDir := testutil.TempDir(t, "test-*")

	// Create workflow A that imports B
	fileA := filepath.Join(tempDir, "a.md")
	fileAContent := `---
imports:
  - b.md
---
# Workflow A
`
	require.NoError(t, os.WriteFile(fileA, []byte(fileAContent), 0644), "Failed to write file A")

	// Create file B that imports C
	fileB := filepath.Join(tempDir, "b.md")
	fileBContent := `---
imports:
  - c.md
---
# File B
`
	require.NoError(t, os.WriteFile(fileB, []byte(fileBContent), 0644), "Failed to write file B")

	// Create file C that imports D
	fileC := filepath.Join(tempDir, "c.md")
	fileCContent := `---
imports:
  - d.md
---
# File C
`
	require.NoError(t, os.WriteFile(fileC, []byte(fileCContent), 0644), "Failed to write file C")

	// Create file D that imports B (creating cycle back to B)
	fileD := filepath.Join(tempDir, "d.md")
	fileDContent := `---
imports:
  - b.md
---
# File D
`
	require.NoError(t, os.WriteFile(fileD, []byte(fileDContent), 0644), "Failed to write file D")

	// Process imports from file A - should detect cycle B→C→D→B
	frontmatter := map[string]any{
		"imports": []string{"b.md"},
	}

	_, err := parser.ProcessImportsFromFrontmatterWithSource(frontmatter, tempDir, nil, fileA, fileAContent)
	require.Error(t, err, "Should detect import cycle")

	// Verify error is an ImportCycleError
	var cycleErr *parser.ImportCycleError
	require.ErrorAs(t, err, &cycleErr, "Error should be ImportCycleError")

	if cycleErr != nil {
		// Verify the full chain is present
		assert.NotEmpty(t, cycleErr.Chain, "Cycle chain should not be empty")

		// Chain should show: b.md → c.md → d.md → b.md (4 elements)
		assert.Len(t, cycleErr.Chain, 4, "Cycle chain should have exactly 4 elements for B→C→D→B")

		// Verify the cycle pattern
		assert.Equal(t, "b.md", cycleErr.Chain[0], "Cycle should start with b.md")
		assert.Equal(t, "c.md", cycleErr.Chain[1], "Second element should be c.md")
		assert.Equal(t, "d.md", cycleErr.Chain[2], "Third element should be d.md")
		assert.Equal(t, "b.md", cycleErr.Chain[3], "Cycle should loop back to b.md")
	}

	// Verify formatted error message contains the full chain
	formattedErr := parser.FormatImportCycleError(cycleErr)
	require.Error(t, formattedErr, "Formatted error should not be nil")

	errMsg := formattedErr.Error()
	assert.Contains(t, errMsg, "circular import detected", "Error should mention import cycle")
	assert.Contains(t, errMsg, "b.md", "Error should mention b.md")
	assert.Contains(t, errMsg, "c.md", "Error should mention c.md")
	assert.Contains(t, errMsg, "d.md", "Error should mention d.md")
	assert.Contains(t, errMsg, "Imports must form a directed acyclic graph", "Error should state the expected structure")
	assert.Contains(t, errMsg, "Example:", "Error should include an actionable example")
}

// TestImportCycleDetection_Deterministic verifies that cycle detection is deterministic
func TestImportCycleDetection_Deterministic(t *testing.T) {
	tempDir := testutil.TempDir(t, "test-*")

	// Create files with a cycle
	files := map[string]string{
		"a.md": `---
imports:
  - b.md
---
# A`,
		"b.md": `---
imports:
  - c.md
---
# B`,
		"c.md": `---
imports:
  - a.md
---
# C`,
	}

	for filename, content := range files {
		path := filepath.Join(tempDir, filename)
		require.NoError(t, os.WriteFile(path, []byte(content), 0644))
	}

	// Run the same import processing multiple times
	var chains [][]string
	for i := range 5 {
		frontmatter := map[string]any{
			"imports": []string{"a.md"},
		}

		_, err := parser.ProcessImportsFromFrontmatterWithSource(frontmatter, tempDir, nil, filepath.Join(tempDir, "main.md"), "")
		require.Error(t, err, "Should detect cycle on iteration %d", i)

		var cycleErr *parser.ImportCycleError
		if assert.ErrorAs(t, err, &cycleErr, "Error should be ImportCycleError on iteration %d", i) {
			chains = append(chains, cycleErr.Chain)
		}
	}

	// Verify all chains are identical (deterministic)
	require.GreaterOrEqual(t, len(chains), 2, "Should have multiple chains to compare")

	firstChain := strings.Join(chains[0], "→")
	for i, chain := range chains {
		chainStr := strings.Join(chain, "→")
		assert.Equal(t, firstChain, chainStr, "Chain %d should match first chain (deterministic)", i)
	}
}

func TestImportCycleDetection_InSubdirectories(t *testing.T) {
	tests := []struct {
		name          string
		files         map[string]string
		mainImport    string
		expectedCycle []string
	}{
		{
			name: "two file cycle reached through entry",
			files: map[string]string{
				"entry.md":        "---\nimports:\n  - shared/sub-a.md\n---",
				"shared/sub-a.md": "---\nimports:\n  - sub-b.md\n---",
				"shared/sub-b.md": "---\nimports:\n  - sub-a.md\n---",
			},
			mainImport:    "entry.md",
			expectedCycle: []string{"shared/sub-a.md", "shared/sub-b.md", "shared/sub-a.md"},
		},
		{
			name: "three file cycle spanning two directories",
			files: map[string]string{
				"entry.md": "---\nimports:\n  - one/a.md\n---",
				"one/a.md": "---\nimports:\n  - two/b.md\n---",
				"two/b.md": "---\nimports:\n  - two/c.md\n---",
				"two/c.md": "---\nimports:\n  - one/a.md\n---",
			},
			mainImport:    "entry.md",
			expectedCycle: []string{"one/a.md", "two/b.md", "two/c.md", "one/a.md"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempDir := testutil.TempDir(t, "import-subdirectory-cycle-*")
			for name, content := range tt.files {
				fullPath := filepath.Join(tempDir, name)
				require.NoError(t, os.MkdirAll(filepath.Dir(fullPath), 0755))
				require.NoError(t, os.WriteFile(fullPath, []byte(content), 0600))
			}

			_, err := parser.ProcessImportsFromFrontmatterWithSource(
				map[string]any{"imports": []string{tt.mainImport}}, tempDir, nil, "", "")
			require.Error(t, err)
			var cycleErr *parser.ImportCycleError
			require.ErrorAs(t, err, &cycleErr)
			assert.Equal(t, tt.expectedCycle, cycleErr.Chain)
		})
	}
}

// TestImportCycleError_FormattedOutput tests the formatted error message
func TestImportCycleError_FormattedOutput(t *testing.T) {
	tests := []struct {
		name          string
		chain         []string
		expectedParts []string
	}{
		{
			name:  "simple 2-file cycle",
			chain: []string{"a.md", "b.md", "a.md"},
			expectedParts: []string{
				"circular import detected: a.md → b.md → a.md",
				"Imports must form a directed acyclic graph",
				"remove the import of \"a.md\" from \"b.md\"",
			},
		},
		{
			name:  "4-file cycle as per issue",
			chain: []string{"b.md", "c.md", "d.md", "b.md"},
			expectedParts: []string{
				"circular import detected: b.md → c.md → d.md → b.md",
				"Imports must form a directed acyclic graph",
				"remove the import of \"b.md\" from \"d.md\"",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cycleErr := &parser.ImportCycleError{
				Chain:        tt.chain,
				WorkflowFile: "test.md",
			}

			formatted := parser.FormatImportCycleError(cycleErr)
			require.Error(t, formatted, "Formatted error should not be nil")

			errMsg := formatted.Error()
			for _, part := range tt.expectedParts {
				assert.Contains(t, errMsg, part, "Error message should contain: %s", part)
			}

			assert.NotContains(t, errMsg, "\n", "Error should follow the concise validation message style")
		})
	}
}

func TestImportCycleError_QuoteBearingPaths(t *testing.T) {
	cycleErr := &parser.ImportCycleError{
		Chain: []string{"entry.md", `importer's "path".md`, `imported's "path".md`},
	}

	formatted := parser.FormatImportCycleError(cycleErr)

	assert.Equal(t,
		`circular import detected: entry.md → importer's "path".md → imported's "path".md. Imports must form a directed acyclic graph. Example: remove the import of "imported's \"path\".md" from "importer's \"path\".md"`,
		formatted.Error(),
	)
}

// TestImportCycleError_InvalidChain tests handling of invalid cycle chains
func TestImportCycleError_InvalidChain(t *testing.T) {
	tests := []struct {
		name  string
		chain []string
	}{
		{
			name:  "empty chain",
			chain: []string{},
		},
		{
			name:  "single element",
			chain: []string{"a.md"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cycleErr := &parser.ImportCycleError{
				Chain:        tt.chain,
				WorkflowFile: "test.md",
			}

			// Should still return an error, just not a detailed one
			formatted := parser.FormatImportCycleError(cycleErr)
			require.Error(t, formatted, "Should return error even for invalid chain")

			errMsg := formatted.Error()
			assert.Contains(t, errMsg, "circular import", "Error should mention circular import")
		})
	}
}
