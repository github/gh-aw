//go:build !integration

package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFlattenAgentOutputsArtifact tests that agent_outputs artifact is properly flattened
// to ensure session logs with token usage data are accessible for parsing
func TestFlattenAgentOutputsArtifact(t *testing.T) {
	tempDir := t.TempDir()

	// Create agent_outputs directory structure as downloaded by gh run download
	agentOutputsDir := filepath.Join(tempDir, "agent_outputs")
	sessionLogsDir := filepath.Join(agentOutputsDir, "sandbox", "agent", "logs")
	err := os.MkdirAll(sessionLogsDir, 0755)
	require.NoError(t, err)

	// Create a test session log file
	sessionLogPath := filepath.Join(sessionLogsDir, "session-test-123.log")
	sessionLogContent := "2025-01-04T10:00:00Z [DEBUG] Test session log with token usage data"
	err = os.WriteFile(sessionLogPath, []byte(sessionLogContent), 0644)
	require.NoError(t, err)

	// Verify agent_outputs directory exists before flattening
	_, err = os.Stat(agentOutputsDir)
	require.NoError(t, err, "agent_outputs directory should exist before flattening")

	// Flatten the artifact
	err = flattenAgentOutputsArtifact(tempDir, false)
	require.NoError(t, err, "flattenAgentOutputsArtifact should succeed")

	// Verify agent_outputs directory was removed after flattening
	_, err = os.Stat(agentOutputsDir)
	assert.True(t, os.IsNotExist(err), "agent_outputs directory should be removed after flattening")

	// Verify session log file is now at the flattened location
	flattenedLogPath := filepath.Join(tempDir, "sandbox", "agent", "logs", "session-test-123.log")
	_, err = os.Stat(flattenedLogPath)
	require.NoError(t, err, "Session log should exist at flattened location")

	// Verify content is preserved
	content, err := os.ReadFile(flattenedLogPath)
	require.NoError(t, err)
	assert.Equal(t, sessionLogContent, string(content), "Session log content should be preserved")
}

// TestFlattenAgentOutputsArtifactMissing tests behavior when agent_outputs is not present
func TestFlattenAgentOutputsArtifactMissing(t *testing.T) {
	tempDir := t.TempDir()

	// Don't create agent_outputs directory
	// Call flatten - should succeed without error
	err := flattenAgentOutputsArtifact(tempDir, false)
	assert.NoError(t, err, "flattenAgentOutputsArtifact should succeed even when agent_outputs is missing")
}

// TestFlattenAgentOutputsArtifactPreservesStructure tests that nested directory structure is preserved
func TestFlattenAgentOutputsArtifactPreservesStructure(t *testing.T) {
	tempDir := t.TempDir()

	// Create complex nested structure in agent_outputs
	agentOutputsDir := filepath.Join(tempDir, "agent_outputs")
	dirs := []string{
		"sandbox/agent/logs",
		"sandbox/firewall/logs",
		"mcp-logs/safeoutputs",
	}

	for _, dir := range dirs {
		fullPath := filepath.Join(agentOutputsDir, dir)
		err := os.MkdirAll(fullPath, 0755)
		require.NoError(t, err)

		// Create a test file in each directory
		testFile := filepath.Join(fullPath, "test.log")
		err = os.WriteFile(testFile, []byte("test content"), 0644)
		require.NoError(t, err)
	}

	// Flatten
	err := flattenAgentOutputsArtifact(tempDir, false)
	require.NoError(t, err)

	// Verify all directories and files are preserved at flattened locations
	for _, dir := range dirs {
		flattenedPath := filepath.Join(tempDir, dir, "test.log")
		_, err := os.Stat(flattenedPath)
		require.NoError(t, err, "File should exist at flattened location: %s", dir)
	}

	// Verify agent_outputs directory was removed
	_, err = os.Stat(agentOutputsDir)
	assert.True(t, os.IsNotExist(err), "agent_outputs directory should be removed")
}
