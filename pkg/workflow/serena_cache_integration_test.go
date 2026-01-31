//go:build integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/githubnext/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSerenaCacheIntegration(t *testing.T) {
	tmpDir := testutil.TempDir(t, "serena-cache-test")

	testContent := `---
on: push
engine: copilot
tools:
  serena: ["go"]
---
# Test Workflow with Serena
Test Serena cache integration
`

	testFile := filepath.Join(tmpDir, "test-workflow.md")
	require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644))

	compiler := NewCompiler()
	compiler.SetSkipValidation(true)
	err := compiler.CompileWorkflow(testFile)
	require.NoError(t, err, "Workflow with Serena should compile without errors")

	// Read the generated lock file
	lockFile := strings.TrimSuffix(testFile, ".md") + ".lock.yml"
	lockContent, err := os.ReadFile(lockFile)
	require.NoError(t, err, "Lock file should be generated")

	lockStr := string(lockContent)

	// Verify Serena cache is present
	assert.Contains(t, lockStr, "name: Cache Serena", "Lock file should contain Serena cache step")
	assert.Contains(t, lockStr, "path: .serena/cache", "Lock file should contain .serena/cache path")
	assert.Contains(t, lockStr, "continue-on-error: true", "Lock file should have continue-on-error")
	assert.Contains(t, lockStr, "save-always: true", "Lock file should have save-always for last-wins strategy")

	// Verify cache comes after checkout
	checkoutIndex := strings.Index(lockStr, "name: Checkout repository")
	cacheIndex := strings.Index(lockStr, "name: Cache Serena")
	assert.True(t, checkoutIndex >= 0, "Checkout step should be present")
	assert.True(t, cacheIndex > checkoutIndex, "Cache step should come after checkout")
}

func TestSerenaCacheNotAddedWithoutSerena(t *testing.T) {
	tmpDir := testutil.TempDir(t, "no-serena-cache-test")

	testContent := `---
on: push
engine: copilot
tools:
  github:
    allowed: ["issue_read"]
---
# Test Workflow without Serena
Test that cache is not added
`

	testFile := filepath.Join(tmpDir, "test-workflow.md")
	require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644))

	compiler := NewCompiler()
	compiler.SetSkipValidation(true)
	err := compiler.CompileWorkflow(testFile)
	require.NoError(t, err, "Workflow without Serena should compile without errors")

	// Read the generated lock file
	lockFile := strings.TrimSuffix(testFile, ".md") + ".lock.yml"
	lockContent, err := os.ReadFile(lockFile)
	require.NoError(t, err, "Lock file should be generated")

	lockStr := string(lockContent)

	// Verify Serena cache is NOT present
	assert.NotContains(t, lockStr, "name: Cache Serena", "Lock file should not contain Serena cache step")
	assert.NotContains(t, lockStr, ".serena/cache", "Lock file should not contain .serena/cache path")
}

func TestSerenaCacheWithMultipleLanguages(t *testing.T) {
	tmpDir := testutil.TempDir(t, "serena-multi-lang-test")

	testContent := `---
on: push
engine: copilot
tools:
  serena: ["go", "typescript", "python"]
---
# Test Workflow with Multiple Serena Languages
Test Serena cache with multiple languages
`

	testFile := filepath.Join(tmpDir, "test-workflow.md")
	require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644))

	compiler := NewCompiler()
	compiler.SetSkipValidation(true)
	err := compiler.CompileWorkflow(testFile)
	require.NoError(t, err, "Workflow with multiple Serena languages should compile without errors")

	// Read the generated lock file
	lockFile := strings.TrimSuffix(testFile, ".md") + ".lock.yml"
	lockContent, err := os.ReadFile(lockFile)
	require.NoError(t, err, "Lock file should be generated")

	lockStr := string(lockContent)

	// Verify Serena cache is present (should be the same regardless of language count)
	assert.Contains(t, lockStr, "name: Cache Serena", "Lock file should contain Serena cache step")
	assert.Contains(t, lockStr, "path: .serena/cache", "Lock file should contain .serena/cache path")
}

func TestSerenaCacheWithDetailedConfig(t *testing.T) {
	tmpDir := testutil.TempDir(t, "serena-detailed-config-test")

	testContent := `---
on: push
engine: copilot
tools:
  serena:
    languages:
      go:
        version: "1.21"
---
# Test Workflow with Detailed Serena Config
Test Serena cache with detailed configuration
`

	testFile := filepath.Join(tmpDir, "test-workflow.md")
	require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644))

	compiler := NewCompiler()
	compiler.SetSkipValidation(true)
	err := compiler.CompileWorkflow(testFile)
	require.NoError(t, err, "Workflow with detailed Serena config should compile without errors")

	// Read the generated lock file
	lockFile := strings.TrimSuffix(testFile, ".md") + ".lock.yml"
	lockContent, err := os.ReadFile(lockFile)
	require.NoError(t, err, "Lock file should be generated")

	lockStr := string(lockContent)

	// Verify Serena cache is present
	assert.Contains(t, lockStr, "name: Cache Serena", "Lock file should contain Serena cache step")
	assert.Contains(t, lockStr, "path: .serena/cache", "Lock file should contain .serena/cache path")
}
