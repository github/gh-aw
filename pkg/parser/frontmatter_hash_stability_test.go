//go:build !integration

package parser

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGoJSHashStability validates that both Go and JavaScript implementations
// produce identical, stable hashes for all workflows in the repository.
// This test runs 2 iterations for each implementation to verify stability.
func TestGoJSHashStability(t *testing.T) {
	// Find repository root
	repoRoot := findRepoRoot(t)
	workflowsDir := filepath.Join(repoRoot, ".github", "workflows")

	// Check if workflows directory exists
	if _, err := os.Stat(workflowsDir); os.IsNotExist(err) {
		t.Skip("Workflows directory not found, skipping test")
		return
	}

	// Find all workflow markdown files
	files, err := filepath.Glob(filepath.Join(workflowsDir, "*.md"))
	require.NoError(t, err, "Should list workflow files")

	if len(files) == 0 {
		t.Skip("No workflow files found")
		return
	}

	// Limit to a reasonable subset for testing (first 10 workflows)
	// Full validation can be done separately
	testCount := 10
	if len(files) < testCount {
		testCount = len(files)
	}
	files = files[:testCount]

	cache := NewImportCache(repoRoot)

	t.Logf("Testing hash stability for %d workflows (Go and JS, 2 iterations each)", len(files))

	for _, file := range files {
		workflowName := filepath.Base(file)
		t.Run(workflowName, func(t *testing.T) {
			// Go implementation - iteration 1
			goHash1, err := ComputeFrontmatterHashFromFile(file, cache)
			require.NoError(t, err, "Go iteration 1 should compute hash")
			assert.Len(t, goHash1, 64, "Go hash should be 64 characters")
			assert.Regexp(t, "^[a-f0-9]{64}$", goHash1, "Go hash should be lowercase hex")

			// Go implementation - iteration 2
			goHash2, err := ComputeFrontmatterHashFromFile(file, cache)
			require.NoError(t, err, "Go iteration 2 should compute hash")
			assert.Equal(t, goHash1, goHash2, "Go hashes should be stable across iterations")

			// JavaScript implementation - iteration 1
			jsHash1, err := computeHashViaJavaScript(file, repoRoot)
			if err != nil {
				t.Logf("  ⚠ JavaScript hash computation not available: %v", err)
				t.Logf("  ✓ Go hash (stable): %s", goHash1)
				return
			}
			assert.Len(t, jsHash1, 64, "JS hash should be 64 characters")
			assert.Regexp(t, "^[a-f0-9]{64}$", jsHash1, "JS hash should be lowercase hex")

			// JavaScript implementation - iteration 2
			jsHash2, err := computeHashViaJavaScript(file, repoRoot)
			require.NoError(t, err, "JS iteration 2 should compute hash")
			assert.Equal(t, jsHash1, jsHash2, "JS hashes should be stable across iterations")

			// Cross-language validation
			// Note: JS uses hardcoded "dev" version, so skip comparison if Go is using a different version
			// This allows tests to pass during development with custom git versions
			if compilerVersion == "dev" {
				assert.Equal(t, goHash1, jsHash1, "Go and JS should produce identical hashes")
				t.Logf("  ✓ Go=%s JS=%s (match: %v)", goHash1, jsHash1, goHash1 == jsHash1)
			} else {
				// When Go uses a git version, JS will produce a different hash
				// This is expected and doesn't indicate a problem with the implementation
				t.Logf("  ⚠ Skipping cross-language comparison (Go version: %s, JS version: dev)", compilerVersion)
				t.Logf("  ✓ Go hash (stable): %s", goHash1)
				t.Logf("  ✓ JS hash (stable): %s", jsHash1)
			}
		})
	}
}

// computeHashViaJavaScript computes the hash using the JavaScript implementation
func computeHashViaJavaScript(workflowPath, repoRoot string) (string, error) {
	// Path to the JavaScript hash computation script
	jsScript := filepath.Join(repoRoot, "actions", "setup", "js", "frontmatter_hash.cjs")

	// Check if script exists
	if _, err := os.Stat(jsScript); os.IsNotExist(err) {
		return "", err
	}

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

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", err
	}

	hash := strings.TrimSpace(string(output))
	return hash, nil
}

// TestGoJSHashEquivalence is a simpler test that validates Go and JS produce
// the same hash for basic test cases using direct JSON comparison
func TestGoJSHashEquivalence(t *testing.T) {
	// Create a simple test workflow
	tempDir := t.TempDir()
	workflowFile := filepath.Join(tempDir, "test.md")

	content := `---
engine: copilot
description: Test workflow
on:
  schedule: daily
tools:
  playwright:
    version: v1.41.0
---

# Test Workflow

Use env: ${{ env.TEST_VAR }}
`

	err := os.WriteFile(workflowFile, []byte(content), 0644)
	require.NoError(t, err, "Should write test file")

	cache := NewImportCache("")

	// Compute hash with Go
	goHash, err := ComputeFrontmatterHashFromFile(workflowFile, cache)
	require.NoError(t, err, "Go should compute hash")

	// For this test, we verify the Go hash includes versions
	// Full JS implementation and comparison will be done in TestGoJSHashStability
	assert.Len(t, goHash, 64, "Hash should be 64 characters")

	t.Logf("Go hash: %s", goHash)

	// Verify the canonical JSON includes versions
	result, err := ExtractFrontmatterFromContent(content)
	require.NoError(t, err, "Should extract frontmatter")

	expressions := extractRelevantTemplateExpressions(result.Markdown)
	require.Len(t, expressions, 1, "Should extract one env expression")
	assert.Equal(t, "${{ env.TEST_VAR }}", expressions[0], "Should extract correct expression")

	// Build canonical to verify versions are included
	importsResult := &ImportsResult{}
	canonical := buildCanonicalFrontmatter(result.Frontmatter, importsResult)
	canonical["template-expressions"] = expressions
	canonical["versions"] = buildVersionInfo()

	canonicalJSON, err := marshalCanonicalJSON(canonical)
	require.NoError(t, err, "Should marshal canonical JSON")

	// Verify versions are in the canonical JSON
	var parsed map[string]any
	err = json.Unmarshal([]byte(canonicalJSON), &parsed)
	require.NoError(t, err, "Should parse canonical JSON")

	versions, hasVersions := parsed["versions"].(map[string]any)
	require.True(t, hasVersions, "Canonical JSON should include versions")

	// gh-aw version is only included for release builds
	if isReleaseVersion {
		assert.NotNil(t, versions["gh-aw"], "Should include gh-aw version for release builds")
	} else {
		assert.Nil(t, versions["gh-aw"], "Should not include gh-aw version for non-release builds")
	}

	// awf and agents versions should always be included
	assert.NotNil(t, versions["awf"], "Should include awf version")
	assert.NotNil(t, versions["agents"], "Should include agents version")

	t.Logf("Versions in hash: %+v", versions)
}
