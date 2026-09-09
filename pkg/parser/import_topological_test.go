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
			expectedOrder: []string{"d.md", "f.md", "c.md", "a.md", "e.md", "b.md"},
		},
		{
			name: "wide tree with many independent branches",
			files: map[string]string{
				"a.md": `---
imports:
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
			mainImports: []string{"a.md", "b.md", "c.md"},
			// Each dependency is processed as soon as it's ready:
			// d (root) -> a (d's dependent), e (root) -> b (e's dependent), f (root) -> c (f's dependent)
			// Alphabetical order within same level
			expectedOrder: []string{"d.md", "a.md", "e.md", "b.md", "f.md", "c.md"},
		},
		{
			name: "reverse alphabetical with dependencies",
			files: map[string]string{
				"z-parent.md": `---
imports:
  - a-child.md
tools:
  tool-z: {}
---`,
				"y-parent.md": `---
imports:
  - b-child.md
tools:
  tool-y: {}
---`,
				"a-child.md": `---
tools:
  tool-a: {}
---`,
				"b-child.md": `---
tools:
  tool-b: {}
---`,
			},
			mainImports: []string{"z-parent.md", "y-parent.md"},
			// Each dependency precedes its parent, with independent branches
			// retaining the declaration order of their parents.
			expectedOrder: []string{"a-child.md", "z-parent.md", "b-child.md", "y-parent.md"},
		},
		{
			name: "multi-level dependency chain",
			files: map[string]string{
				"level-0.md": `---
imports:
  - level-1.md
tools:
  tool-0: {}
---`,
				"level-1.md": `---
imports:
  - level-2.md
tools:
  tool-1: {}
---`,
				"level-2.md": `---
imports:
  - level-3.md
tools:
  tool-2: {}
---`,
				"level-3.md": `---
imports:
  - level-4.md
tools:
  tool-3: {}
---`,
				"level-4.md": `---
tools:
  tool-4: {}
---`,
			},
			mainImports:   []string{"level-0.md"},
			expectedOrder: []string{"level-4.md", "level-3.md", "level-2.md", "level-1.md", "level-0.md"},
		},
		{
			name: "parallel branches with shared dependency",
			files: map[string]string{
				"branch-a-top.md": `---
imports:
  - branch-a-mid.md
tools:
  tool-a-top: {}
---`,
				"branch-a-mid.md": `---
imports:
  - shared-base.md
tools:
  tool-a-mid: {}
---`,
				"branch-b-top.md": `---
imports:
  - branch-b-mid.md
tools:
  tool-b-top: {}
---`,
				"branch-b-mid.md": `---
imports:
  - shared-base.md
tools:
  tool-b-mid: {}
---`,
				"shared-base.md": `---
tools:
  tool-shared: {}
---`,
			},
			mainImports: []string{"branch-a-top.md", "branch-b-top.md"},
			// shared-base (root) -> branch-a-mid (becomes ready) -> branch-a-top (becomes ready)
			// -> branch-b-mid (becomes ready) -> branch-b-top (becomes ready)
			// Alphabetical order when multiple items are at the same level
			expectedOrder: []string{"shared-base.md", "branch-a-mid.md", "branch-a-top.md", "branch-b-mid.md", "branch-b-top.md"},
		},
		{
			name: "mixed naming with special characters",
			files: map[string]string{
				"01-first.md": `---
imports:
  - 99-last.md
tools:
  tool-first: {}
---`,
				"02-second.md": `---
imports:
  - 99-last.md
tools:
  tool-second: {}
---`,
				"99-last.md": `---
tools:
  tool-last: {}
---`,
			},
			mainImports: []string{"01-first.md", "02-second.md"},
			// 99-last is shared dependency, comes first
			// Then dependents in alphabetical order
			expectedOrder: []string{"99-last.md", "01-first.md", "02-second.md"},
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
			result, err := parser.ProcessImportsFromFrontmatterWithSource(frontmatter, tempDir, nil, "", "")
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

	result, err := parser.ProcessImportsFromFrontmatterWithSource(frontmatter, tempDir, nil, "", "")
	require.NoError(t, err)

	// b.md should come before a.md (even with section reference)
	assert.Len(t, result.ImportedFiles, 2)
	assert.Equal(t, "b.md#Tools", result.ImportedFiles[0])
	assert.Equal(t, "a.md", result.ImportedFiles[1])
}

// TestImportTopologicalSortPreservesDeclarationOrder tests that independent
// imports retain their declaration order.
func TestImportTopologicalSortPreservesDeclarationOrder(t *testing.T) {
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

	result, err := parser.ProcessImportsFromFrontmatterWithSource(frontmatter, tempDir, nil, "", "")
	require.NoError(t, err)

	// All are roots, so declaration order is preserved.
	assert.Len(t, result.ImportedFiles, 3)
	assert.Equal(t, "z-root.md", result.ImportedFiles[0])
	assert.Equal(t, "a-root.md", result.ImportedFiles[1])
	assert.Equal(t, "m-root.md", result.ImportedFiles[2])
}

// TestImportTopologicalSortBeatsLexicalParentOrdering ensures that dependency
// edges always win over lexical filename order when both conflict.
func TestImportTopologicalSortBeatsLexicalParentOrdering(t *testing.T) {
	tempDir := testutil.TempDir(t, "import-topo-contract-*")

	files := map[string]string{
		"z-parent.md": `---
imports:
  - a-dependency.md
tools:
  parent-tool: {}
---`,
		"a-dependency.md": `---
tools:
  dependency-tool: {}
---`,
	}

	for filename, content := range files {
		filePath := filepath.Join(tempDir, filename)
		err := os.WriteFile(filePath, []byte(content), 0644)
		require.NoError(t, err, "Failed to create test file %s", filename)
	}

	frontmatter := map[string]any{
		"imports": []string{"z-parent.md"},
	}

	result, err := parser.ProcessImportsFromFrontmatterWithSource(frontmatter, tempDir, nil, "", "")
	require.NoError(t, err, "ProcessImportsFromFrontmatterWithManifest should not fail")
	assert.Len(t, result.ImportedFiles, 2)

	// The dependency must precede the parent even though its name is lexically earlier.
	assert.Equal(t, "a-dependency.md", result.ImportedFiles[0])
	assert.Equal(t, "z-parent.md", result.ImportedFiles[1])
}

func TestImportTopologicalSortStableAcrossRuns(t *testing.T) {
	tempDir := testutil.TempDir(t, "import-topo-stable-*")

	files := map[string]string{
		"root.md": `---
imports:
  - b.md
  - a.md
---`,
		"a.md": `---
imports:
  - c.md
tools:
  a-tool: {}
---`,
		"b.md": `---
tools:
  b-tool: {}
---`,
		"c.md": `---
tools:
  c-tool: {}
---`,
	}

	for filename, content := range files {
		filePath := filepath.Join(tempDir, filename)
		err := os.WriteFile(filePath, []byte(content), 0644)
		require.NoError(t, err)
	}

	frontmatter := map[string]any{"imports": []string{"root.md"}}

	var baseline []string
	for i := range 5 {
		result, err := parser.ProcessImportsFromFrontmatterWithSource(frontmatter, tempDir, nil, "", "")
		require.NoError(t, err)
		if i == 0 {
			baseline = append([]string(nil), result.ImportedFiles...)
			continue
		}
		assert.Equal(t, baseline, result.ImportedFiles)
	}
}

func TestImportedStepFieldsFollowDependencyOrder(t *testing.T) {
	files := map[string]string{
		"shared/base.md": `---
steps:
  - name: STEP-BASE
pre-agent-steps:
  - name: PRE-BASE
post-steps:
  - name: POST-BASE
---`,
		"shared/a.md": `---
imports:
  - base.md
steps:
  - name: STEP-A
pre-agent-steps:
  - name: PRE-A
post-steps:
  - name: POST-A
max-turns: 10
---`,
		"shared/b.md": `---
imports:
  - base.md
steps:
  - name: STEP-B
pre-agent-steps:
  - name: PRE-B
post-steps:
  - name: POST-B
max-turns: 20
---`,
	}
	tests := []struct {
		name          string
		imports       []string
		expectedFiles []string
		expectedNames []string
		expectedMax   string
	}{
		{
			name:          "diamond preserves sibling declaration order",
			imports:       []string{"shared/a.md", "shared/b.md"},
			expectedFiles: []string{"shared/base.md", "shared/a.md", "shared/b.md"},
			expectedNames: []string{"BASE", "A", "B"},
			expectedMax:   "10",
		},
		{
			name:          "reversed diamond preserves sibling declaration order",
			imports:       []string{"shared/b.md", "shared/a.md"},
			expectedFiles: []string{"shared/base.md", "shared/b.md", "shared/a.md"},
			expectedNames: []string{"BASE", "B", "A"},
			expectedMax:   "20",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempDir := testutil.TempDir(t, "import-step-order-*")
			for name, content := range files {
				fullPath := filepath.Join(tempDir, name)
				require.NoError(t, os.MkdirAll(filepath.Dir(fullPath), 0755))
				require.NoError(t, os.WriteFile(fullPath, []byte(content), 0600))
			}

			result, err := parser.ProcessImportsFromFrontmatterWithSource(
				map[string]any{"imports": tt.imports}, tempDir, nil, "", "")
			require.NoError(t, err)
			assert.Equal(t, tt.expectedFiles, result.ImportedFiles)
			assert.Equal(t, tt.expectedMax, result.MergedMaxTurns, "scalar precedence must remain discovery-order first-wins")
			assertNamesInOrder(t, result.MergedSteps, "STEP-", tt.expectedNames)
			assertNamesInOrder(t, result.MergedPreAgentSteps, "PRE-", tt.expectedNames)
			assertNamesInOrder(t, result.MergedPostSteps, "POST-", tt.expectedNames)
		})
	}
}

func TestImportedStepsAreRoleInvariant(t *testing.T) {
	tempDir := testutil.TempDir(t, "import-role-order-*")
	files := map[string]string{
		"a.md": `---
imports:
  - b.md
steps:
  - name: STEP-A
---`,
		"b.md": `---
steps:
  - name: STEP-B
---`,
	}
	for name, content := range files {
		require.NoError(t, os.WriteFile(filepath.Join(tempDir, name), []byte(content), 0600))
	}

	importedA, err := parser.ProcessImportsFromFrontmatterWithSource(
		map[string]any{"imports": []string{"a.md"}}, tempDir, nil, "", "")
	require.NoError(t, err)
	assertNamesInOrder(t, importedA.MergedSteps, "STEP-", []string{"B", "A"})

	rootA, err := parser.ProcessImportsFromFrontmatterWithSource(
		map[string]any{"imports": []string{"b.md"}}, tempDir, nil, "", "")
	require.NoError(t, err)
	assertNamesInOrder(t, rootA.MergedSteps+"\n"+files["a.md"], "STEP-", []string{"B", "A"})
}

func TestUnrelatedImportedStepsPreserveDeclarationOrder(t *testing.T) {
	tempDir := testutil.TempDir(t, "import-sibling-order-*")
	files := map[string]string{
		"z.md": "---\nsteps:\n  - name: STEP-Z\n---",
		"a.md": "---\nsteps:\n  - name: STEP-A\n---",
	}
	for name, content := range files {
		require.NoError(t, os.WriteFile(filepath.Join(tempDir, name), []byte(content), 0600))
	}

	result, err := parser.ProcessImportsFromFrontmatterWithSource(
		map[string]any{"imports": []string{"z.md", "a.md"}}, tempDir, nil, "", "")
	require.NoError(t, err)
	assert.Equal(t, []string{"z.md", "a.md"}, result.ImportedFiles)
	assertNamesInOrder(t, result.MergedSteps, "STEP-", []string{"Z", "A"})
}

func assertNamesInOrder(t *testing.T, content, prefix string, names []string) {
	t.Helper()
	previous := -1
	for _, name := range names {
		current := strings.Index(content, "name: "+prefix+name+"\n")
		assert.Greater(t, current, previous, "%s%s must follow the previous entry in %q", prefix, name, content)
		previous = current
	}
}
