//go:build !integration

// Package parser_test contains contract tests for topological import ordering.
//
// These tests lock in the guarantee that import ordering is always topological
// (dependency semantics: dependencies appear before their dependents) regardless
// of lexical filename ordering.
//
// Tie-break rule (documented contract): when multiple nodes are ready at the same
// time (in-degree 0 in Kahn's algorithm), they retain declaration order.
//
// Cross-path contract: each fixture is verified via
//
//	(1) the import processor path (ProcessImportsFromFrontmatterWithManifest), and
//	(2) an independent dependency-map extracted directly from the fixture files,
//
// ensuring both representations agree on what constitutes a valid topological order.
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

// verifyTopologicalConstraints asserts the core topological property:
// for every (importer, dep) pair where dep is listed as a dependency of importer,
// dep must appear at a strictly lower index than importer in result.
//
// deps maps a relative filename to the set of relative filenames it directly imports
// (matching the format stored in result, e.g. "b.md" not "b.md#Section").
// Files absent from result are silently skipped (they may be filtered out).
func verifyTopologicalConstraints(t *testing.T, result []string, deps map[string][]string) {
	t.Helper()

	pos := make(map[string]int, len(result))
	for i, f := range result {
		pos[f] = i
	}

	for importer, imports := range deps {
		importerIdx, ok := pos[importer]
		if !ok {
			continue
		}
		for _, dep := range imports {
			depIdx, ok2 := pos[dep]
			if !ok2 {
				continue
			}
			assert.Less(t, depIdx, importerIdx,
				"topological constraint violated: %q (pos %d) must appear before %q (pos %d) because %q depends on %q",
				dep, depIdx, importer, importerIdx, importer, dep)
		}
	}
}

// buildDepMapFromFiles reads each file in the given directory and returns a map
// of relative filename → list of relative filenames it imports. Section suffixes
// (e.g. "#Tools") are stripped from dependency values to match the flat filenames
// used as keys in result.
func buildDepMapFromFiles(t *testing.T, dir string, files map[string]string) map[string][]string {
	t.Helper()

	depMap := make(map[string][]string, len(files))
	for filename, content := range files {
		fm, err := parser.ExtractFrontmatterFromContent(content)
		require.NoError(t, err, "parsing frontmatter for %s", filename)

		var deps []string
		if importsRaw, ok := fm.Frontmatter["imports"]; ok {
			switch v := importsRaw.(type) {
			case []any:
				for _, item := range v {
					switch iv := item.(type) {
					case string:
						base := iv
						if idx := indexOf(iv, "#"); idx != -1 {
							base = iv[:idx]
						}
						deps = append(deps, base)
					case map[string]any:
						if p, ok2 := iv["path"].(string); ok2 {
							base := p
							if idx := indexOf(p, "#"); idx != -1 {
								base = p[:idx]
							}
							deps = append(deps, base)
						}
					}
				}
			case []string:
				for _, iv := range v {
					base := iv
					if idx := indexOf(iv, "#"); idx != -1 {
						base = iv[:idx]
					}
					deps = append(deps, base)
				}
			}
		}
		depMap[filename] = deps
	}
	return depMap
}

// indexOf returns the index of the first occurrence of sub in s, or -1 if not found.
func indexOf(s, sub string) int {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// writeFiles creates all entries of the files map as real files under dir.
func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		path := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(path, []byte(content), 0600),
			"writing fixture file %s", name)
	}
}

// runImportProcessor processes the given top-level import list via
// ProcessImportsFromFrontmatterWithManifest and returns the ImportedFiles slice.
func runImportProcessor(t *testing.T, dir string, topImports []string) []string {
	t.Helper()
	fm := map[string]any{"imports": topImports}
	result, err := parser.ProcessImportsFromFrontmatterWithSource(fm, dir, nil, "", "")
	require.NoError(t, err, "ProcessImportsFromFrontmatterWithManifest must not fail")
	return result.ImportedFiles
}

// ---------------------------------------------------------------------------
// TestImportTopologicalContractLexicalConflict
// ---------------------------------------------------------------------------

// TestImportTopologicalContractLexicalConflict verifies that when dependency order
// conflicts with lexical filename order the output remains topological.
//
// Each sub-test is designed so that a naive lexical sort would produce wrong
// results (placing a dependent before its dependency). The test therefore acts
// as an explicit regression lock: it will fail the moment the implementation
// regresses to lexical ordering.
func TestImportTopologicalContractLexicalConflict(t *testing.T) {
	tests := []struct {
		name       string
		files      map[string]string
		topImports []string
		// deps mirrors the logical dependency relationships declared in the files.
		// Used by verifyTopologicalConstraints (independent of expected order).
		deps map[string][]string
		// wantOrder is the expected deterministic output, useful as a documentation
		// anchor for the tie-break rule. The property check via deps is authoritative.
		wantOrder []string
	}{
		{
			// "z-uses-a" would be wrong under lexical sort (a < z, but z imports a →
			// a must come first).
			name: "z-parent imports a-child: dependency beats lexical order",
			files: map[string]string{
				"z-parent.md": `---
imports:
  - a-child.md
tools:
  tool-z: {}
---`,
				"a-child.md": `---
tools:
  tool-a: {}
---`,
			},
			topImports: []string{"z-parent.md"},
			deps: map[string][]string{
				"z-parent.md": {"a-child.md"},
				"a-child.md":  {},
			},
			wantOrder: []string{"a-child.md", "z-parent.md"},
		},
		{
			// Reversed three-node chain: lexical order (a < b < c) would be wrong
			// because c imports b which imports a, so correct order is a, b, c.
			name: "chain c→b→a: leaves must precede internal nodes precede roots",
			files: map[string]string{
				"c-root.md": `---
imports:
  - b-mid.md
tools:
  tool-c: {}
---`,
				"b-mid.md": `---
imports:
  - a-leaf.md
tools:
  tool-b: {}
---`,
				"a-leaf.md": `---
tools:
  tool-a: {}
---`,
			},
			topImports: []string{"c-root.md"},
			deps: map[string][]string{
				"c-root.md": {"b-mid.md"},
				"b-mid.md":  {"a-leaf.md"},
				"a-leaf.md": {},
			},
			wantOrder: []string{"a-leaf.md", "b-mid.md", "c-root.md"},
		},
		{
			// Two-branch tree where each branch's leaf has a lexically later name than
			// its parent. Lexical sort would interleave parents before leaves.
			name: "two branches with lexically-later leaves",
			files: map[string]string{
				"a-parent.md": `---
imports:
  - z-leaf-a.md
tools:
  tool-a-parent: {}
---`,
				"b-parent.md": `---
imports:
  - y-leaf-b.md
tools:
  tool-b-parent: {}
---`,
				"z-leaf-a.md": `---
tools:
  tool-z: {}
---`,
				"y-leaf-b.md": `---
tools:
  tool-y: {}
---`,
			},
			topImports: []string{"a-parent.md", "b-parent.md"},
			deps: map[string][]string{
				"a-parent.md": {"z-leaf-a.md"},
				"b-parent.md": {"y-leaf-b.md"},
				"z-leaf-a.md": {},
				"y-leaf-b.md": {},
			},
			// The branch rooted at the first declared parent is emitted first.
			// Key invariant: every leaf precedes its own parent (topological guarantee).
			wantOrder: []string{"z-leaf-a.md", "a-parent.md", "y-leaf-b.md", "b-parent.md"},
		},
		{
			// Diamond: two parents share the same leaf. Lexical sort would not
			// guarantee the leaf precedes both parents.
			name: "diamond: shared leaf must precede both parents",
			files: map[string]string{
				"z-left.md": `---
imports:
  - a-base.md
tools:
  tool-z-left: {}
---`,
				"y-right.md": `---
imports:
  - a-base.md
tools:
  tool-y-right: {}
---`,
				"a-base.md": `---
tools:
  tool-a-base: {}
---`,
			},
			topImports: []string{"z-left.md", "y-right.md"},
			deps: map[string][]string{
				"z-left.md":  {"a-base.md"},
				"y-right.md": {"a-base.md"},
				"a-base.md":  {},
			},
			wantOrder: []string{"a-base.md", "z-left.md", "y-right.md"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := testutil.TempDir(t, "contract-lexical-*")
			writeFiles(t, dir, tt.files)

			got := runImportProcessor(t, dir, tt.topImports)

			// 1. Property check (authoritative): every dependency must come before
			//    its importer in the output.
			verifyTopologicalConstraints(t, got, tt.deps)

			// 2. Exact-order check: documents the deterministic tie-break behaviour.
			assert.Equal(t, tt.wantOrder, got,
				"import processor output must match documented deterministic order")
		})
	}
}

// ---------------------------------------------------------------------------
// TestImportTopologicalContractTieBreakIsDeclarationOrder
// ---------------------------------------------------------------------------

// TestImportTopologicalContractTieBreakIsDeclarationOrder explicitly documents and
// verifies the tie-break rule: when multiple nodes are simultaneously ready
// (in-degree 0 in Kahn's algorithm), they retain declaration order.
//
// This test uses fixtures where all nodes are roots (no dependencies), so the
// only ordering constraint is the tie-break rule itself.
func TestImportTopologicalContractTieBreakIsDeclarationOrder(t *testing.T) {
	t.Run("all roots retain declaration order", func(t *testing.T) {
		files := map[string]string{
			"omega.md": "---\ntools:\n  t: {}\n---",
			"alpha.md": "---\ntools:\n  t: {}\n---",
			"gamma.md": "---\ntools:\n  t: {}\n---",
			"beta.md":  "---\ntools:\n  t: {}\n---",
		}
		topImports := []string{"omega.md", "alpha.md", "gamma.md", "beta.md"}

		dir := testutil.TempDir(t, "contract-tiebreak-*")
		writeFiles(t, dir, files)

		got := runImportProcessor(t, dir, topImports)

		require.Len(t, got, 4, "all four files must appear in the result")
		assert.Equal(t, topImports, got,
			"independent nodes must retain declaration order")
	})

	t.Run("independent branches retain declaration order", func(t *testing.T) {
		files := map[string]string{
			"c-parent.md": `---
imports:
  - z-leaf.md
tools:
  tool-c: {}
---`,
			"b-parent.md": `---
imports:
  - a-leaf.md
tools:
  tool-b: {}
---`,
			"z-leaf.md": "---\ntools:\n  tz: {}\n---",
			"a-leaf.md": "---\ntools:\n  ta: {}\n---",
		}
		topImports := []string{"c-parent.md", "b-parent.md"}

		dir := testutil.TempDir(t, "contract-tiebreak-wave-*")
		writeFiles(t, dir, files)

		got := runImportProcessor(t, dir, topImports)

		require.Len(t, got, 4)
		assert.Equal(t, []string{"z-leaf.md", "c-parent.md", "a-leaf.md", "b-parent.md"}, got,
			"independent branches must retain declaration order")

		// Property check: no dependency constraint violated.
		verifyTopologicalConstraints(t, got, map[string][]string{
			"c-parent.md": {"z-leaf.md"},
			"b-parent.md": {"a-leaf.md"},
		})
	})
}

// ---------------------------------------------------------------------------
// TestImportTopologicalContractCrossPath
// ---------------------------------------------------------------------------

// TestImportTopologicalContractCrossPath is the cross-path contract test.
//
// It verifies that for the same set of fixture files:
//
//  1. The import processor path (ProcessImportsFromFrontmatterWithManifest) produces
//     a valid topological ordering of ImportedFiles.
//  2. An independently extracted dependency map (built directly from the raw
//     frontmatter without invoking the import processor) agrees with the ordering
//     produced by (1).
//
// This separates the ordering-producer (import processor) from the
// ordering-verifier (dependency extractor), so a regression in either code path
// will surface as a test failure.
func TestImportTopologicalContractCrossPath(t *testing.T) {
	tests := []struct {
		name       string
		files      map[string]string
		topImports []string
	}{
		{
			name: "linear chain: lexical reverse of topological",
			files: map[string]string{
				"z-top.md": `---
imports:
  - m-mid.md
tools:
  tool-z: {}
---`,
				"m-mid.md": `---
imports:
  - a-bot.md
tools:
  tool-m: {}
---`,
				"a-bot.md": `---
tools:
  tool-a: {}
---`,
			},
			topImports: []string{"z-top.md"},
		},
		{
			name: "diamond: shared dependency",
			files: map[string]string{
				"z-a.md": `---
imports:
  - a-shared.md
tools:
  tool-za: {}
---`,
				"z-b.md": `---
imports:
  - a-shared.md
tools:
  tool-zb: {}
---`,
				"a-shared.md": `---
tools:
  tool-shared: {}
---`,
			},
			topImports: []string{"z-a.md", "z-b.md"},
		},
		{
			name: "wide: many independent branches",
			files: map[string]string{
				"c-root.md": `---
imports:
  - z-leaf.md
tools:
  tool-c: {}
---`,
				"b-root.md": `---
imports:
  - y-leaf.md
tools:
  tool-b: {}
---`,
				"a-root.md": `---
imports:
  - x-leaf.md
tools:
  tool-a: {}
---`,
				"z-leaf.md": "---\ntools:\n  tz: {}\n---",
				"y-leaf.md": "---\ntools:\n  ty: {}\n---",
				"x-leaf.md": "---\ntools:\n  tx: {}\n---",
			},
			topImports: []string{"c-root.md", "b-root.md", "a-root.md"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := testutil.TempDir(t, "contract-crosspath-*")
			writeFiles(t, dir, tt.files)

			// Path 1: import processor produces the ordering.
			got := runImportProcessor(t, dir, tt.topImports)

			// Path 2: independently extract the dependency map from raw frontmatter.
			depMap := buildDepMapFromFiles(t, dir, tt.files)

			// Cross-path contract: the ordering from path 1 must satisfy the
			// constraints captured by path 2.
			verifyTopologicalConstraints(t, got, depMap)

			t.Logf("cross-path verified: import processor order = %v", got)
		})
	}
}
