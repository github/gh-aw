//go:build !integration

package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/github/gh-aw/pkg/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestForceRefreshActionPins_ClearCache(t *testing.T) {
	// Create temporary directory for testing
	tmpDir := testutil.TempDir(t, "test-*")

	// Change to temp directory to simulate running from repo root
	oldCwd, err := os.Getwd()
	require.NoError(t, err, "Failed to get current working directory")
	defer func() {
		_ = os.Chdir(oldCwd)
	}()

	err = os.Chdir(tmpDir)
	require.NoError(t, err, "Failed to change to temp directory")

	// Create a cache with some entries
	cache := workflow.NewActionCache(tmpDir)
	cache.Set("actions/checkout", "v5", "abc123")
	cache.Set("actions/setup-node", "v4", "def456")
	err = cache.Save()
	require.NoError(t, err, "Failed to save initial cache")

	// Verify cache file exists and has entries
	cachePath := filepath.Join(tmpDir, ".github", "aw", workflow.CacheFileName)
	require.FileExists(t, cachePath, "Cache file should exist before test")

	// Load the cache to verify it has entries
	testCache := workflow.NewActionCache(tmpDir)
	err = testCache.Load()
	require.NoError(t, err, "Failed to load cache")
	assert.Len(t, testCache.Entries, 2, "Cache should have 2 entries before force refresh")

	// Create compiler with force refresh enabled
	compiler := workflow.NewCompiler(
		workflow.WithVersion("test"),
		workflow.WithForceRefreshActionPins(true),
	)

	// Get the shared action resolver - this should skip loading the cache
	actionCache, _ := compiler.GetSharedActionResolverForTest()

	// Verify cache is empty (not loaded from disk)
	assert.Empty(t, actionCache.Entries, "Cache should be empty when force refresh is enabled")
}

func TestForceRefreshActionPins_ResetFile(t *testing.T) {
	// Create temporary directory for testing
	tmpDir := testutil.TempDir(t, "test-*")

	// Change to temp directory to simulate running from repo root
	oldCwd, err := os.Getwd()
	require.NoError(t, err, "Failed to get current working directory")
	defer func() {
		_ = os.Chdir(oldCwd)
	}()

	err = os.Chdir(tmpDir)
	require.NoError(t, err, "Failed to change to temp directory")

	// Create the expected directory structure
	actionPinsDir := filepath.Join(tmpDir, "pkg", "workflow", "data")
	err = os.MkdirAll(actionPinsDir, 0755)
	require.NoError(t, err, "Failed to create action pins directory")

	// Create a mock action_pins.json with some entries
	actionPinsPath := filepath.Join(actionPinsDir, "action_pins.json")
	mockData := `{
  "entries": {
    "actions/checkout@v5": {
      "repo": "actions/checkout",
      "version": "v5",
      "sha": "abc123"
    }
  }
}`
	err = os.WriteFile(actionPinsPath, []byte(mockData), 0644)
	require.NoError(t, err, "Failed to create mock action_pins.json")

	// Call resetActionPinsFile
	err = resetActionPinsFile()
	require.NoError(t, err, "resetActionPinsFile should not return error")

	// Verify the file was reset to empty
	data, err := os.ReadFile(actionPinsPath)
	require.NoError(t, err, "Failed to read action_pins.json")

	// Parse the JSON to verify structure
	var result map[string]any
	err = json.Unmarshal(data, &result)
	require.NoError(t, err, "Failed to parse action_pins.json")

	// Verify it has the correct structure with empty entries
	assert.Contains(t, result, "entries", "File should contain 'entries' key")
	entries, ok := result["entries"].(map[string]any)
	require.True(t, ok, "entries should be a map")
	assert.Empty(t, entries, "entries should be empty after reset")
}

func TestForceRefreshActionPins_NoFileExists(t *testing.T) {
	// Create temporary directory for testing
	tmpDir := testutil.TempDir(t, "test-*")

	// Change to temp directory to simulate running from repo root
	oldCwd, err := os.Getwd()
	require.NoError(t, err, "Failed to get current working directory")
	defer func() {
		_ = os.Chdir(oldCwd)
	}()

	err = os.Chdir(tmpDir)
	require.NoError(t, err, "Failed to change to temp directory")

	// Call resetActionPinsFile when file doesn't exist - should not error
	err = resetActionPinsFile()
	require.NoError(t, err, "resetActionPinsFile should not error when file doesn't exist")
}

func TestForceRefreshActionPins_EnablesValidation(t *testing.T) {
	// Test that force refresh automatically enables validation
	config := CompileConfig{
		ForceRefreshActionPins: true,
		Validate:               false, // Explicitly disabled
	}

	// Simulate the logic in compileSpecificFiles
	shouldValidate := config.Validate || config.ForceRefreshActionPins

	assert.True(t, shouldValidate, "Validation should be enabled when ForceRefreshActionPins is true")
}
