//go:build !integration

package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunListWorkflows_JSONOutput(t *testing.T) {
	// Save current directory
	originalDir, err := os.Getwd()
	require.NoError(t, err, "Failed to get current directory")

	// Change to repository root
	repoRoot := filepath.Join(originalDir, "..", "..")
	err = os.Chdir(repoRoot)
	require.NoError(t, err, "Failed to change to repository root")
	defer os.Chdir(originalDir)

	// Test JSON output without pattern
	t.Run("JSON output without pattern", func(t *testing.T) {
		err := RunListWorkflows(t.Context(), "", ".github/workflows", "", false, true, "", false)
		require.NoError(t, err, "RunListWorkflows with JSON flag should not error")
	})

	// Test JSON output with pattern
	t.Run("JSON output with pattern", func(t *testing.T) {
		err := RunListWorkflows(t.Context(), "", ".github/workflows", "smoke", false, true, "", false)
		require.NoError(t, err, "RunListWorkflows with JSON flag and pattern should not error")
	})

	// Test JSON output with label filter
	t.Run("JSON output with label filter", func(t *testing.T) {
		err := RunListWorkflows(t.Context(), "", ".github/workflows", "", false, true, "test", false)
		require.NoError(t, err, "RunListWorkflows with JSON flag and label filter should not error")
	})
}

func TestWorkflowListItem_JSONMarshaling(t *testing.T) {
	t.Parallel()
	// Test that WorkflowListItem can be marshaled to JSON
	item := WorkflowListItem{
		Workflow: "test-workflow",
		EngineID: "copilot",
		Compiled: "Yes",
		Labels:   []string{"test", "automation"},
		On: map[string]any{
			"workflow_dispatch": nil,
		},
	}

	jsonBytes, err := json.Marshal(item)
	require.NoError(t, err, "Failed to marshal WorkflowListItem")

	// Verify JSON contains expected fields
	var unmarshaled map[string]any
	err = json.Unmarshal(jsonBytes, &unmarshaled)
	require.NoError(t, err, "Failed to unmarshal JSON")

	assert.Equal(t, "test-workflow", unmarshaled["workflow"], "workflow field should match")
	assert.Equal(t, "copilot", unmarshaled["engine_id"], "engine_id field should match")
	assert.Equal(t, "Yes", unmarshaled["compiled"], "compiled field should match")

	// Verify labels array
	labels, ok := unmarshaled["labels"].([]any)
	require.True(t, ok, "labels should be an array")
	assert.Len(t, labels, 2, "Should have 2 labels")
	assert.Equal(t, "test", labels[0], "First label should be 'test'")
	assert.Equal(t, "automation", labels[1], "Second label should be 'automation'")

	// Verify "on" field is included
	onField, ok := unmarshaled["on"].(map[string]any)
	require.True(t, ok, "on field should be a map")
	_, exists := onField["workflow_dispatch"]
	assert.True(t, exists, "on field should contain 'workflow_dispatch' key")
}

func TestRunListWorkflows_TextOutput(t *testing.T) {
	// Save current directory
	originalDir, err := os.Getwd()
	require.NoError(t, err, "Failed to get current directory")

	// Change to repository root
	repoRoot := filepath.Join(originalDir, "..", "..")
	err = os.Chdir(repoRoot)
	require.NoError(t, err, "Failed to change to repository root")
	defer os.Chdir(originalDir)

	// Test text output
	t.Run("Text output without pattern", func(t *testing.T) {
		err := RunListWorkflows(t.Context(), "", ".github/workflows", "", false, false, "", false)
		require.NoError(t, err, "RunListWorkflows without JSON flag should not error")
	})

	// Test text output with pattern
	t.Run("Text output with pattern", func(t *testing.T) {
		err := RunListWorkflows(t.Context(), "", ".github/workflows", "ci-", false, false, "", false)
		require.NoError(t, err, "RunListWorkflows with pattern should not error")
	})
}

func TestNewListCommand(t *testing.T) {
	t.Parallel()
	cmd := NewListCommand()

	// Verify command properties
	assert.Equal(t, "list", cmd.Use[:4], "Command use should start with 'list'")
	assert.NotEmpty(t, cmd.Short, "Command should have short description")
	assert.NotEmpty(t, cmd.Long, "Command should have long description")
	assert.NotNil(t, cmd.RunE, "Command should have RunE function")

	// Verify flags exist
	jsonFlag := cmd.Flags().Lookup("json")
	assert.NotNil(t, jsonFlag, "Command should have --json flag")

	labelFlag := cmd.Flags().Lookup("label")
	assert.NotNil(t, labelFlag, "Command should have --label flag")

	staleFlag := cmd.Flags().Lookup("stale")
	assert.NotNil(t, staleFlag, "Command should have --stale flag")
}

// captureListOutput calls RunListWorkflows and returns the captured stdout as a string.
func captureListOutput(t *testing.T, dir, pattern string) string {
	t.Helper()
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err, "Should create pipe")
	os.Stdout = w

	runErr := RunListWorkflows(t.Context(), "", dir, pattern, false, true, "", false)

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	require.NoError(t, runErr, "RunListWorkflows should not error")
	return buf.String()
}

func TestRunListWorkflows_CompiledField(t *testing.T) {
	tmpDir := t.TempDir()

	// Minimal valid workflow markdown
	mdContent := `---
name: test-workflow
on:
  workflow_dispatch:
engine: copilot
---

# Test Workflow

A simple test workflow.
`
	mdPath := filepath.Join(tmpDir, "test-workflow.md")
	require.NoError(t, os.WriteFile(mdPath, []byte(mdContent), 0644), "Should write test workflow")

	// Compute the real frontmatter hash
	cache := parser.NewImportCache("")
	realHash, err := parser.ComputeFrontmatterHashFromFile(mdPath, cache)
	require.NoError(t, err, "Should compute frontmatter hash")

	t.Run("compiled Yes when hash matches", func(t *testing.T) {
		lockPath := filepath.Join(tmpDir, "test-workflow.lock.yml")
		lockContent := `# gh-aw-metadata: {"schema_version":"v1","frontmatter_hash":"` + realHash + `"}
name: test-workflow
`
		require.NoError(t, os.WriteFile(lockPath, []byte(lockContent), 0644), "Should write lock file")

		output := captureListOutput(t, tmpDir, "")

		var items []WorkflowListItem
		require.NoError(t, json.Unmarshal([]byte(output), &items), "Should unmarshal JSON output")
		require.Len(t, items, 1, "Should have one workflow")
		assert.Equal(t, "Yes", items[0].Compiled, "Compiled should be Yes when hash matches")

		output = captureStaleListOutput(t, tmpDir)
		require.Equal(t, "[]\n", output, "Stale output should be empty when the lock file matches")
	})

	t.Run("compiled No when hash mismatches", func(t *testing.T) {
		lockPath := filepath.Join(tmpDir, "test-workflow.lock.yml")
		lockContent := `# gh-aw-metadata: {"schema_version":"v1","frontmatter_hash":"0000000000000000000000000000000000000000000000000000000000000000"}
name: test-workflow
`
		require.NoError(t, os.WriteFile(lockPath, []byte(lockContent), 0644), "Should write lock file with wrong hash")

		output := captureListOutput(t, tmpDir, "")

		var items []WorkflowListItem
		require.NoError(t, json.Unmarshal([]byte(output), &items), "Should unmarshal JSON output")
		require.Len(t, items, 1, "Should have one workflow")
		assert.Equal(t, "No", items[0].Compiled, "Compiled should be No when hash mismatches")

		output = captureStaleListOutput(t, tmpDir)
		require.NoError(t, json.Unmarshal([]byte(output), &items), "Should unmarshal stale JSON output")
		require.Len(t, items, 1, "Should include workflow with stale lock file")
		assert.Equal(t, "No", items[0].Compiled, "Stale workflow should retain compilation status")
	})

	t.Run("compiled NA when no lock file", func(t *testing.T) {
		// Remove lock file
		_ = os.Remove(filepath.Join(tmpDir, "test-workflow.lock.yml"))

		output := captureListOutput(t, tmpDir, "")

		var items []WorkflowListItem
		require.NoError(t, json.Unmarshal([]byte(output), &items), "Should unmarshal JSON output")
		require.Len(t, items, 1, "Should have one workflow")
		assert.Equal(t, "N/A", items[0].Compiled, "Compiled should be N/A when no lock file exists")

		output = captureStaleListOutput(t, tmpDir)
		require.NoError(t, json.Unmarshal([]byte(output), &items), "Should unmarshal stale JSON output")
		require.Len(t, items, 1, "Should include workflow with missing lock file")
		assert.Equal(t, "N/A", items[0].Compiled, "Missing lock file should retain compilation status")
	})
}

func TestRunListWorkflows_StaleOnlyRejectsRemoteRepository(t *testing.T) {
	err := RunListWorkflows(t.Context(), "owner/repo", ".github/workflows", "", false, false, "", true)
	require.EqualError(t, err, "--stale cannot be used with --repo because remote workflow compilation status is unavailable")
}

func captureStaleListOutput(t *testing.T, dir string) string {
	t.Helper()
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err, "Should create pipe")
	os.Stdout = w

	runErr := RunListWorkflows(t.Context(), "", dir, "", false, true, "", true)

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	require.NoError(t, runErr, "RunListWorkflows with stale filter should not error")
	return buf.String()
}
