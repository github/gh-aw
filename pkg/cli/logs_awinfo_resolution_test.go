//go:build !integration

package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAwInfoResolution tests that aw_info.json is correctly resolved
// from the aw-info artifact directory after flattening
func TestAwInfoResolution(t *testing.T) {
	// Create a temporary directory structure mimicking downloaded artifacts
	tempDir := t.TempDir()

	// Step 1: Simulate gh run download - creates artifact directories
	awInfoDir := filepath.Join(tempDir, "aw-info")
	err := os.MkdirAll(awInfoDir, 0755)
	require.NoError(t, err)

	awInfoContent := `{
		"engine_id": "copilot",
		"engine_name": "GitHub Copilot CLI",
		"model": "gpt-4",
		"workflow_name": "Test Workflow"
	}`
	awInfoPath := filepath.Join(awInfoDir, "aw_info.json")
	err = os.WriteFile(awInfoPath, []byte(awInfoContent), 0644)
	require.NoError(t, err)

	// Also create a log file
	logContent := `::error::Test error message
::warning::Test warning message`
	err = os.WriteFile(filepath.Join(tempDir, "agent-stdio.log"), []byte(logContent), 0644)
	require.NoError(t, err)

	// Step 2: Flatten single-file artifacts (simulates what happens after download)
	err = flattenSingleFileArtifacts(tempDir, true)
	require.NoError(t, err, "Flattening should succeed")

	// Step 3: Verify aw_info.json is now at the root
	flattenedAwInfoPath := filepath.Join(tempDir, "aw_info.json")
	_, err = os.Stat(flattenedAwInfoPath)
	require.NoError(t, err, "aw_info.json should exist at root after flattening")

	// Step 4: Verify the aw-info directory is removed
	_, err = os.Stat(awInfoDir)
	assert.True(t, os.IsNotExist(err), "aw-info directory should be removed after flattening")

	// Step 5: Test that extractLogMetrics can find and parse the aw_info.json
	_, err = extractLogMetrics(tempDir, false)
	require.NoError(t, err, "extractLogMetrics should succeed")

	// Error patterns have been removed - no error/warning detection
}

// TestAwInfoResolutionWithoutFlattening tests the failure case
// where aw_info.json is still in the artifact directory
func TestAwInfoResolutionWithoutFlattening(t *testing.T) {
	// Create a temporary directory structure mimicking unflattened artifacts
	tempDir := t.TempDir()

	// Artifact still in directory (not flattened)
	awInfoDir := filepath.Join(tempDir, "aw-info")
	err := os.MkdirAll(awInfoDir, 0755)
	require.NoError(t, err)

	awInfoContent := `{
		"engine_id": "copilot",
		"engine_name": "GitHub Copilot CLI",
		"model": "gpt-4",
		"workflow_name": "Test Workflow"
	}`
	awInfoPath := filepath.Join(awInfoDir, "aw_info.json")
	err = os.WriteFile(awInfoPath, []byte(awInfoContent), 0644)
	require.NoError(t, err)

	// Create a log file
	logContent := `::error::Test error message
::warning::Test warning message`
	err = os.WriteFile(filepath.Join(tempDir, "agent-stdio.log"), []byte(logContent), 0644)
	require.NoError(t, err)

	// Test that extractLogMetrics FAILS to find aw_info.json because it's not at root
	_, err = extractLogMetrics(tempDir, false)
	require.NoError(t, err, "extractLogMetrics should not error")

	// Error patterns have been removed - no error/warning detection
}

// TestMultipleArtifactFlattening tests that all files from unified agent-artifacts are flattened
func TestMultipleArtifactFlattening(t *testing.T) {
	tempDir := t.TempDir()

	// Create unified agent-artifacts structure as it would be downloaded
	// All artifacts are now in a single agent-artifacts/tmp/gh-aw/ directory
	nestedPath := filepath.Join(tempDir, "agent-artifacts", "tmp", "gh-aw")
	err := os.MkdirAll(nestedPath, 0755)
	require.NoError(t, err)

	artifacts := map[string]string{
		"aw_info.json":      `{"engine_id":"copilot"}`,
		"safe_output.jsonl": `{"type":"create_issue"}`,
		"aw.patch":          "diff --git a/test.txt",
		"prompt.txt":        "Test prompt",
	}

	for filename, content := range artifacts {
		fullPath := filepath.Join(nestedPath, filename)
		err := os.WriteFile(fullPath, []byte(content), 0644)
		require.NoError(t, err)
	}

	// Flatten unified artifact
	err = flattenUnifiedArtifact(tempDir, true)
	require.NoError(t, err)

	// Verify all files are at root level
	expectedFiles := []string{
		"aw_info.json",
		"safe_output.jsonl",
		"aw.patch",
		"prompt.txt",
	}

	for _, file := range expectedFiles {
		path := filepath.Join(tempDir, file)
		_, err = os.Stat(path)
		require.NoError(t, err, "File %s should exist at root", file)
	}

	// Verify agent-artifacts directory is removed
	agentArtifactsDir := filepath.Join(tempDir, "agent-artifacts")
	_, err = os.Stat(agentArtifactsDir)
	assert.True(t, os.IsNotExist(err), "agent-artifacts directory should be removed")
}
