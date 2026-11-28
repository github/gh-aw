//go:build !integration

package parser

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHashConsistency_GoAndJavaScript validates that both Go and JavaScript
// implementations produce identical hashes for the same inputs.
//
// This test confirms that the text-based hash algorithm is consistent between
// Go and JavaScript, including for workflows with inlined-imports: true.
func TestHashConsistency_GoAndJavaScript(t *testing.T) {
	testCases := []struct {
		name    string
		content string
	}{
		{
			name: "empty frontmatter",
			content: `---
---

# Empty Workflow
`,
		},
		{
			name: "simple frontmatter",
			content: `---
engine: copilot
description: Test workflow
on:
  schedule: daily
---

# Test Workflow
`,
		},
		{
			name: "complex frontmatter with tools",
			content: `---
engine: claude
description: Complex workflow
tracker-id: complex-test
timeout-minutes: 30
on:
  schedule: daily
  workflow_dispatch: true
permissions:
  contents: read
  actions: read
tools:
  playwright:
    version: v1.41.0
labels:
  - test
  - complex
bots:
  - copilot
---

# Complex Workflow
`,
		},
		{
			name: "frontmatter with env template expressions",
			content: `---
engine: copilot
description: Workflow with template expressions
---

# Test

Use environment variable: ${{ env.MY_VAR }}
Use config variable: ${{ vars.MY_CONFIG }}
`,
		},
		{
			name: "frontmatter with nested objects",
			content: `---
engine: copilot
tools:
  playwright:
    version: v1.41.0
    domains:
      - github.com
      - example.com
  mcp:
    server: remote
permissions:
  contents: read
  actions: write
network:
  allowed:
    - api.github.com
---

# Nested Objects Test
`,
		},
		{
			name: "frontmatter with arrays",
			content: `---
engine: copilot
labels:
  - audit
  - automation
  - daily
bots:
  - copilot
steps:
  - name: Step 1
    run: echo 'test'
  - name: Step 2
    run: echo 'test2'
---

# Array Test
`,
		},
		{
			name: "inlined-imports enabled with body content",
			content: `---
engine: copilot
description: Inlined imports workflow
inlined-imports: true
---

# Inlined Imports Workflow

This is the body content that should be included in the hash.

## Step 1

Do something useful here.

## Step 2

Do something else here.
`,
		},
		{
			name: "inlined-imports enabled with env template expressions in body",
			content: `---
engine: copilot
description: Inlined imports with expressions
inlined-imports: true
---

# Workflow

Use env variable: ${{ env.MY_VAR }}
Use config variable: ${{ vars.MY_CONFIG }}
`,
		},
	}

	tempDir := t.TempDir()
	cache := NewImportCache("")

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create temporary workflow file
			workflowFile := filepath.Join(tempDir, "test-"+strings.ReplaceAll(tc.name, " ", "-")+".md")
			err := os.WriteFile(workflowFile, []byte(tc.content), 0644)
			require.NoError(t, err, "Should write test file")

			// Compute hash with Go implementation - iteration 1
			goHash1, err := ComputeFrontmatterHashFromFile(workflowFile, cache)
			require.NoError(t, err, "Go should compute hash")
			assert.Len(t, goHash1, 64, "Go hash should be 64 characters")
			assert.Regexp(t, "^[a-f0-9]{64}$", goHash1, "Go hash should be lowercase hex")

			// Compute hash with Go implementation - iteration 2 (stability check)
			goHash2, err := ComputeFrontmatterHashFromFile(workflowFile, cache)
			require.NoError(t, err, "Go should compute hash again")
			assert.Equal(t, goHash1, goHash2, "Go hashes should be stable (same input → same output)")

			// Compute hash with JavaScript implementation - iteration 1
			jsHash1, err := computeHashViaNode(workflowFile)
			if err != nil {
				// JavaScript implementation might not be available in all test environments
				t.Logf("  ⚠ JavaScript hash computation not available: %v", err)
				t.Logf("  ✓ Go hash (stable): %s", goHash1)
				return
			}
			assert.Len(t, jsHash1, 64, "JS hash should be 64 characters")
			assert.Regexp(t, "^[a-f0-9]{64}$", jsHash1, "JS hash should be lowercase hex")

			// Compute hash with JavaScript implementation - iteration 2 (stability check)
			jsHash2, err := computeHashViaNode(workflowFile)
			require.NoError(t, err, "JS should compute hash again")
			assert.Equal(t, jsHash1, jsHash2, "JS hashes should be stable (same input → same output)")

			// Cross-language validation: Go and JS must produce identical hashes.
			// Both implementations use the same text-based parsing algorithm.
			assert.Equal(t, goHash1, jsHash1, "Go and JS must produce identical hashes for the same input")

			t.Logf("  ✓ Go hash (stable): %s", goHash1)
			t.Logf("  ✓ JS hash (stable): %s", jsHash1)
			if goHash1 == jsHash1 {
				t.Logf("  ✓ Match: YES - implementations are consistent!")
			} else {
				t.Logf("  ⚠ Match: NO - JS implementation needs update (expected)")
			}
		})
	}
}

func TestHashConsistencyWithBuiltinConnectorAlias(t *testing.T) {
	connectorContent, err := os.ReadFile("../../.github/workflows/connectors/linear.md")
	require.NoError(t, err)
	RegisterBuiltinVirtualFile(BuiltinPathPrefix+"connectors/linear.md", connectorContent)

	workflowPath := filepath.Join(t.TempDir(), "workflow.md")
	workflowContent := "---\nengine: copilot\nimports:\n  - connector:linear\n---\nReview approved evidence.\n"
	require.NoError(t, os.WriteFile(workflowPath, []byte(workflowContent), 0600))

	goHash, err := ComputeFrontmatterHashFromFile(workflowPath, NewImportCache(""))
	require.NoError(t, err)
	jsHash, err := computeHashViaNode(workflowPath)
	require.NoError(t, err)
	assert.Equal(t, goHash, jsHash)
}

// TestHashStability_SameInputSameOutput validates that computing the hash
// multiple times for the same input always produces the same result.
func TestHashStability_SameInputSameOutput(t *testing.T) {
	content := `---
engine: copilot
description: Stability test workflow
on:
  schedule: daily
tools:
  playwright:
    version: v1.41.0
permissions:
  contents: read
---

# Stability Test

Use ${{ env.TEST_VAR }} and ${{ vars.CONFIG }}
`

	tempDir := t.TempDir()
	workflowFile := filepath.Join(tempDir, "stability-test.md")
	err := os.WriteFile(workflowFile, []byte(content), 0644)
	require.NoError(t, err, "Should write test file")

	cache := NewImportCache("")

	// Compute hash 10 times with Go
	var goHashes []string
	for i := range 10 {
		hash, err := ComputeFrontmatterHashFromFile(workflowFile, cache)
		require.NoError(t, err, "Should compute hash iteration %d", i+1)
		goHashes = append(goHashes, hash)
	}

	// All Go hashes should be identical
	for i := 1; i < len(goHashes); i++ {
		assert.Equal(t, goHashes[0], goHashes[i],
			"Go hash iteration %d should match iteration 1", i+1)
	}

	// Compute hash 10 times with JavaScript
	var jsHashes []string
	for range 10 {
		hash, err := computeHashViaNode(workflowFile)
		if err != nil {
			t.Logf("JavaScript not available, skipping JS stability test")
			break
		}
		jsHashes = append(jsHashes, hash)
	}

	// All JS hashes should be identical
	if len(jsHashes) > 0 {
		for i := 1; i < len(jsHashes); i++ {
			assert.Equal(t, jsHashes[0], jsHashes[i],
				"JS hash iteration %d should match iteration 1", i+1)
		}

		// Go and JS must match - both use the same text-based algorithm
		assert.Equal(t, goHashes[0], jsHashes[0], "Go and JS hashes should be identical")
	}

	t.Logf("✓ Computed hash 10 times with Go - all identical: %s", goHashes[0])
	if len(jsHashes) > 0 {
		t.Logf("✓ Computed hash 10 times with JS - all identical: %s", jsHashes[0])
		if goHashes[0] == jsHashes[0] {
			t.Logf("✓ Go and JS match - implementations are consistent!")
		} else {
			t.Logf("⚠ Go and JS differ - JS implementation needs update (expected)")
		}
	}
}

func TestComputeFrontmatterHash_LFAndCRLFMatch(t *testing.T) {
	baseContentLF := `---
engine: copilot
description: newline stability
on:
  workflow_dispatch: true
---

# Newline Stability
`
	baseContentCRLF := strings.ReplaceAll(baseContentLF, "\n", "\r\n")

	tempDir := t.TempDir()
	lfFile := filepath.Join(tempDir, "workflow-lf.md")
	crlfFile := filepath.Join(tempDir, "workflow-crlf.md")

	require.NoError(t, os.WriteFile(lfFile, []byte(baseContentLF), 0644))
	require.NoError(t, os.WriteFile(crlfFile, []byte(baseContentCRLF), 0644))

	cache := NewImportCache("")
	lfHash, err := ComputeFrontmatterHashFromFile(lfFile, cache)
	require.NoError(t, err)
	crlfHash, err := ComputeFrontmatterHashFromFile(crlfFile, cache)
	require.NoError(t, err)

	assert.Equal(t, lfHash, crlfHash, "LF and CRLF frontmatter should hash identically")
}

func TestComputeFrontmatterHash_ImportedFrontmatterLFAndCRLFMatch(t *testing.T) {
	tempDir := t.TempDir()

	mainLF := `---
engine: copilot
imports:
  - ./shared.md
---

# Main
`
	mainCRLF := strings.ReplaceAll(mainLF, "\n", "\r\n")

	importLF := `---
description: shared settings
tools:
  playwright: {}
---

# Shared
`
	importCRLF := strings.ReplaceAll(importLF, "\n", "\r\n")

	lfDir := filepath.Join(tempDir, "lf")
	crlfDir := filepath.Join(tempDir, "crlf")
	require.NoError(t, os.MkdirAll(lfDir, 0755))
	require.NoError(t, os.MkdirAll(crlfDir, 0755))

	lfMain := filepath.Join(lfDir, "main.md")
	crlfMain := filepath.Join(crlfDir, "main.md")

	require.NoError(t, os.WriteFile(lfMain, []byte(mainLF), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(lfDir, "shared.md"), []byte(importLF), 0644))
	require.NoError(t, os.WriteFile(crlfMain, []byte(mainCRLF), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(crlfDir, "shared.md"), []byte(importCRLF), 0644))

	cache := NewImportCache("")
	lfHash, err := ComputeFrontmatterHashFromFile(lfMain, cache)
	require.NoError(t, err)
	crlfHash, err := ComputeFrontmatterHashFromFile(crlfMain, cache)
	require.NoError(t, err)

	assert.Equal(
		t,
		lfHash,
		crlfHash,
		"LF and CRLF variants should hash identically with imported frontmatter",
	)
}

// TestHashConsistency_WithImports validates hash consistency for workflows
// that import other workflows.
func TestHashConsistency_WithImports(t *testing.T) {
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
`
	err = os.WriteFile(mainFile, []byte(mainContent), 0644)
	require.NoError(t, err, "Should write main file")

	cache := NewImportCache("")

	// Compute hash with Go - 2 iterations
	goHash1, err := ComputeFrontmatterHashFromFile(mainFile, cache)
	require.NoError(t, err, "Go should compute hash with imports")
	goHash2, err := ComputeFrontmatterHashFromFile(mainFile, cache)
	require.NoError(t, err, "Go should compute hash again")
	assert.Equal(t, goHash1, goHash2, "Go hashes with imports should be stable")

	// Compute hash with JavaScript - 2 iterations
	jsHash1, err := computeHashViaNode(mainFile)
	if err != nil {
		t.Logf("JavaScript not available: %v", err)
		t.Logf("✓ Go hash with imports (stable): %s", goHash1)
		return
	}
	jsHash2, err := computeHashViaNode(mainFile)
	require.NoError(t, err, "JS should compute hash again")
	assert.Equal(t, jsHash1, jsHash2, "JS hashes with imports should be stable")

	// Cross-language validation: Go and JS must match
	assert.Equal(t, goHash1, jsHash1, "Go and JS should produce identical hashes with imports")

	t.Logf("✓ Go hash with imports (stable): %s", goHash1)
	t.Logf("✓ JS hash with imports (stable): %s", jsHash1)
	if goHash1 == jsHash1 {
		t.Logf("✓ Match: YES - implementations are consistent!")
	} else {
		t.Logf("⚠ Match: NO - JS implementation needs update (expected)")
	}
}

// TestHashConsistency_KeyOrdering validates that different key ordering
// in frontmatter produces different hashes (text-based approach).
// With text-based parsing, the hash is based on the literal frontmatter text,
// so reordering keys changes the hash.
func TestHashConsistency_KeyOrdering(t *testing.T) {
	tempDir := t.TempDir()
	cache := NewImportCache("")

	// Create two workflows with identical content but different key ordering
	content1 := `---
engine: copilot
description: Test
on:
  schedule: daily
permissions:
  contents: read
---

# Test
`

	content2 := `---
permissions:
  contents: read
on:
  schedule: daily
description: Test
engine: copilot
---

# Test
`

	file1 := filepath.Join(tempDir, "test1.md")
	file2 := filepath.Join(tempDir, "test2.md")

	err := os.WriteFile(file1, []byte(content1), 0644)
	require.NoError(t, err, "Should write file1")
	err = os.WriteFile(file2, []byte(content2), 0644)
	require.NoError(t, err, "Should write file2")

	// Compute hashes with Go
	goHash1, err := ComputeFrontmatterHashFromFile(file1, cache)
	require.NoError(t, err, "Go should compute hash for file1")
	goHash2, err := ComputeFrontmatterHashFromFile(file2, cache)
	require.NoError(t, err, "Go should compute hash for file2")

	// Text-based approach: Different key ordering produces different text, thus different hashes
	// This is expected behavior - the hash is based on the literal frontmatter text
	assert.NotEqual(t, goHash1, goHash2, "Go (text-based): Different key ordering produces different hash")

	// Compute hashes with JavaScript
	jsHash1, err := computeHashViaNode(file1)
	if err != nil {
		t.Logf("JavaScript not available")
		t.Logf("✓ Go hashes identical regardless of key order: %s", goHash1)
		return
	}
	jsHash2, err := computeHashViaNode(file2)
	require.NoError(t, err, "JS should compute hash for file2")

	// JavaScript implementation uses text-based parsing, so key ordering affects hash
	t.Logf("JS hash for file 1: %s", jsHash1)
	t.Logf("JS hash for file 2: %s", jsHash2)
	if jsHash1 == jsHash2 {
		t.Logf("✓ JS: Different key ordering produces same hash")
	} else {
		t.Logf("✓ JS: Different key ordering produces different hashes (expected with text-based parsing)")
	}

	// Cross-language validation: Both Go and JS should match since both use text-based approach
	assert.Equal(t, goHash1, jsHash1, "Go and JS should produce same hash for file 1")
	assert.Equal(t, goHash2, jsHash2, "Go and JS should produce same hash for file 2")
	assert.NotEqual(t, jsHash1, jsHash2, "JS: Different key ordering should produce different hash (text-based)")

	t.Logf("✓ File 1 - Go: %s, JS: %s", goHash1, jsHash1)
	t.Logf("✓ File 2 - Go: %s, JS: %s", goHash2, jsHash2)

	// Check consistency
	goMatch := goHash1 != goHash2     // Different orderings should produce different hashes
	jsMatch := jsHash1 != jsHash2     // Different orderings should produce different hashes
	crossMatch1 := goHash1 == jsHash1 // Same ordering should match across languages
	crossMatch2 := goHash2 == jsHash2 // Same ordering should match across languages

	if goMatch && jsMatch && crossMatch1 && crossMatch2 {
		t.Logf("✓ All checks pass - text-based implementations are consistent!")
	} else {
		t.Logf("⚠ Go ordering-dependent: %v, JS ordering-dependent: %v, File 1 cross-match: %v, File 2 cross-match: %v",
			goMatch, jsMatch, crossMatch1, crossMatch2)
	}
}

// TestHashConsistency_LFvsCRLF validates that workflow files with identical content but
// different newline conventions (LF vs CRLF) produce the same frontmatter hash.
// This is a regression test for cross-platform hash stability.
func TestHashConsistency_LFvsCRLF(t *testing.T) {
	tempDir := t.TempDir()
	cache := NewImportCache("")

	testCases := []struct {
		name    string
		content string // LF version; CRLF version is derived automatically
	}{
		{
			name: "simple frontmatter",
			content: `---
engine: copilot
description: Test workflow
on:
  schedule: daily
---

# Test Workflow
`,
		},
		{
			name: "complex frontmatter",
			content: `---
engine: claude
description: Complex workflow
tracker-id: complex-test
timeout-minutes: 30
on:
  schedule: daily
  workflow_dispatch: true
permissions:
  contents: read
  actions: read
tools:
  playwright:
    version: v1.41.0
labels:
  - test
  - complex
bots:
  - copilot
---

# Complex Workflow
`,
		},
		{
			name: "frontmatter with env template expressions",
			content: `---
engine: copilot
description: Workflow with template expressions
---

# Test

Use environment variable: ${{ env.MY_VAR }}
Use config variable: ${{ vars.MY_CONFIG }}
`,
		},
		{
			name: "frontmatter with inlined-imports",
			content: `---
engine: copilot
inlined-imports: true
description: Inlined imports workflow
---

# Inlined body content
Some multi-line
content here
`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			lfContent := tc.content
			crlfContent := strings.ReplaceAll(tc.content, "\n", "\r\n")

			lfFile := filepath.Join(tempDir, "lf-"+strings.ReplaceAll(tc.name, " ", "-")+".md")
			crlfFile := filepath.Join(tempDir, "crlf-"+strings.ReplaceAll(tc.name, " ", "-")+".md")

			err := os.WriteFile(lfFile, []byte(lfContent), 0644)
			require.NoError(t, err, "Should write LF test file")
			err = os.WriteFile(crlfFile, []byte(crlfContent), 0644)
			require.NoError(t, err, "Should write CRLF test file")

			lfHash, err := ComputeFrontmatterHashFromFile(lfFile, cache)
			require.NoError(t, err, "Should compute hash for LF file")
			assert.Len(t, lfHash, 64, "LF hash should be 64 characters")

			crlfHash, err := ComputeFrontmatterHashFromFile(crlfFile, cache)
			require.NoError(t, err, "Should compute hash for CRLF file")
			assert.Len(t, crlfHash, 64, "CRLF hash should be 64 characters")

			assert.Equal(t, lfHash, crlfHash, "LF and CRLF variants must produce identical hashes")
			t.Logf("  ✓ LF=CRLF hash: %s", lfHash)
		})
	}
}

// TestHashConsistency_LFvsCRLF_WithImports validates that LF/CRLF invariance holds
// when workflows import other workflows.
func TestHashConsistency_LFvsCRLF_WithImports(t *testing.T) {
	tempDir := t.TempDir()
	sharedDir := filepath.Join(tempDir, "shared")
	err := os.MkdirAll(sharedDir, 0755)
	require.NoError(t, err, "Should create shared directory")

	sharedLF := `---
tools:
  playwright:
    version: v1.41.0
labels:
  - shared
  - common
---

# Shared Content
`
	mainLF := `---
engine: copilot
description: Main workflow
imports:
  - shared/common.md
labels:
  - main
---

# Main Workflow
`

	cache := NewImportCache("")

	// LF variant: both main and shared use LF
	err = os.WriteFile(filepath.Join(sharedDir, "common.md"), []byte(sharedLF), 0644)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(tempDir, "main-lf.md"), []byte(mainLF), 0644)
	require.NoError(t, err)
	lfHash, err := ComputeFrontmatterHashFromFile(filepath.Join(tempDir, "main-lf.md"), cache)
	require.NoError(t, err, "Should compute hash for LF files with imports")

	// CRLF variant: both main and shared use CRLF
	sharedCRLF := strings.ReplaceAll(sharedLF, "\n", "\r\n")
	mainCRLF := strings.ReplaceAll(mainLF, "\n", "\r\n")
	err = os.WriteFile(filepath.Join(sharedDir, "common.md"), []byte(sharedCRLF), 0644)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(tempDir, "main-crlf.md"), []byte(mainCRLF), 0644)
	require.NoError(t, err)
	crlfHash, err := ComputeFrontmatterHashFromFile(filepath.Join(tempDir, "main-crlf.md"), cache)
	require.NoError(t, err, "Should compute hash for CRLF files with imports")

	assert.Equal(t, lfHash, crlfHash, "LF and CRLF import variants must produce identical hashes")
	t.Logf("  ✓ LF with imports hash:   %s", lfHash)
	t.Logf("  ✓ CRLF with imports hash: %s", crlfHash)
}

// TestHashConsistency_InlinedImports validates hash behavior when
// inlined-imports: true is set in the frontmatter. With this flag, the full
// markdown body is included in the canonical hash so that any content change
// (not just frontmatter changes) invalidates the hash.
func TestHashConsistency_InlinedImports(t *testing.T) {
	tempDir := t.TempDir()
	cache := NewImportCache("")

	// ── Workflow with inlined-imports: true, body variant A ──────────────────
	inlinedBodyA := `---
engine: copilot
description: Inlined imports workflow
inlined-imports: true
---

# Inlined Workflow

This is the original body content.
`

	// ── Same frontmatter, different body (inlined-imports: true) ─────────────
	inlinedBodyB := `---
engine: copilot
description: Inlined imports workflow
inlined-imports: true
---

# Inlined Workflow

This is DIFFERENT body content.
`

	// ── Workflow WITHOUT inlined-imports flag, body variant A ─────────────────
	// Body changes should NOT affect the hash without the flag.
	noFlagBodyA := `---
engine: copilot
description: Inlined imports workflow
---

# Inlined Workflow

This is the original body content.
`

	// ── Same frontmatter (no flag), different body ────────────────────────────
	noFlagBodyB := `---
engine: copilot
description: Inlined imports workflow
---

# Inlined Workflow

This is DIFFERENT body content.
`

	inlinedBodyAFile := filepath.Join(tempDir, "inlined-a.md")
	inlinedBodyBFile := filepath.Join(tempDir, "inlined-b.md")
	noFlagBodyAFile := filepath.Join(tempDir, "noflag-a.md")
	noFlagBodyBFile := filepath.Join(tempDir, "noflag-b.md")

	require.NoError(t, os.WriteFile(inlinedBodyAFile, []byte(inlinedBodyA), 0644))
	require.NoError(t, os.WriteFile(inlinedBodyBFile, []byte(inlinedBodyB), 0644))
	require.NoError(t, os.WriteFile(noFlagBodyAFile, []byte(noFlagBodyA), 0644))
	require.NoError(t, os.WriteFile(noFlagBodyBFile, []byte(noFlagBodyB), 0644))

	inlinedHashA, err := ComputeFrontmatterHashFromFile(inlinedBodyAFile, cache)
	require.NoError(t, err, "Should compute hash for inlined-imports with body A")

	inlinedHashB, err := ComputeFrontmatterHashFromFile(inlinedBodyBFile, cache)
	require.NoError(t, err, "Should compute hash for inlined-imports with body B")

	noFlagHashA, err := ComputeFrontmatterHashFromFile(noFlagBodyAFile, cache)
	require.NoError(t, err, "Should compute hash for no-flag workflow with body A")

	noFlagHashB, err := ComputeFrontmatterHashFromFile(noFlagBodyBFile, cache)
	require.NoError(t, err, "Should compute hash for no-flag workflow with body B")

	// Body change MUST invalidate the hash when inlined-imports: true
	assert.NotEqual(t, inlinedHashA, inlinedHashB,
		"Different body content should produce different hash when inlined-imports: true")

	// Without the flag, body changes do NOT affect the hash
	assert.Equal(t, noFlagHashA, noFlagHashB,
		"Body changes should not affect hash when inlined-imports flag is absent")

	// The same body content compiles differently with vs without the flag,
	// since the flag itself changes the frontmatter text included in the hash.
	assert.NotEqual(t, inlinedHashA, noFlagHashA,
		"Workflow with inlined-imports: true should hash differently from one without the flag")

	t.Logf("  inlined-imports:true,  body A: %s", inlinedHashA)
	t.Logf("  inlined-imports:true,  body B: %s", inlinedHashB)
	t.Logf("  no flag,               body A: %s", noFlagHashA)
	t.Logf("  no flag,               body B: %s", noFlagHashB)

	// ── Cross-language validation ─────────────────────────────────────────────
	for _, tc := range []struct {
		name string
		file string
	}{
		{"inlined-imports:true body A", inlinedBodyAFile},
		{"inlined-imports:true body B", inlinedBodyBFile},
		{"no-flag body A", noFlagBodyAFile},
	} {
		jsHash, err := computeHashViaNode(tc.file)
		if err != nil {
			t.Logf("JavaScript not available for %s: %v", tc.name, err)
			return
		}
		goHash, err := ComputeFrontmatterHashFromFile(tc.file, cache)
		require.NoError(t, err, "Should compute Go hash for %s", tc.name)
		assert.Equal(t, goHash, jsHash, "Go and JS hashes must match for %s", tc.name)
		t.Logf("  ✓ Go=JS for %s: %s", tc.name, goHash)
	}
}

// computeHashViaNode computes the hash using the JavaScript implementation via Node.js
func computeHashViaNode(workflowPath string) (string, error) {
	// Get working directory and construct path to JavaScript file
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}

	// Try to find the repository root from working directory
	repoRoot := wd
	for {
		jsScript := filepath.Join(repoRoot, "actions", "setup", "js", "frontmatter_hash.cjs")
		if _, err := os.Stat(jsScript); err == nil {
			// Found it, use this as repo root
			break
		}

		// Try parent directory
		parent := filepath.Dir(repoRoot)
		if parent == repoRoot {
			// Reached filesystem root without finding the file
			return "", os.ErrNotExist
		}
		repoRoot = parent
	}

	// Path to the JavaScript hash computation script
	jsScript := filepath.Join(repoRoot, "actions", "setup", "js", "frontmatter_hash.cjs")

	// Create a temporary Node.js script that calls the hash function
	tmpDir, err := os.MkdirTemp("", "js-hash-test-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmpDir)

	testScript := filepath.Join(tmpDir, "test-hash.js")
	scriptContent := `
const { computeFrontmatterHash } = require("` + jsScript + `");

async function main() {
	try {
		const hash = await computeFrontmatterHash(process.argv[2]);
		console.log(hash);
	} catch (err) {
		console.error("Error:", err.message);
		process.exit(1);
	}
}

main();
`

	if err := os.WriteFile(testScript, []byte(scriptContent), 0644); err != nil {
		return "", err
	}

	// Run the Node.js script
	cmd := exec.Command("node", testScript, workflowPath)
	cmd.Dir = repoRoot

	// Use Output() instead of CombinedOutput() to only capture stdout
	// This avoids capturing stderr messages like [one-shot-token]
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}

	hash := strings.TrimSpace(string(output))
	return hash, nil
}
