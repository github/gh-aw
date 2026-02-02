//go:build integration

package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/github/gh-aw/pkg/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCopilotLogParsingAfterFlattening tests that the Copilot parser can find session logs
// after artifact flattening. This simulates the actual workflow:
// 1. Download artifacts with gh run download (creates agent_outputs/)
// 2. Flatten artifacts (moves agent_outputs/sandbox/agent/logs/ to sandbox/agent/logs/)
// 3. Parse logs (findAgentLogFile should find the session log in the flattened location)
func TestCopilotLogParsingAfterFlattening(t *testing.T) {
	tmpDir := testutil.TempDir(t, "copilot-flatten-*")

	// Step 1: Simulate downloaded artifacts structure (before flattening)
	// gh run download creates: agent_outputs/sandbox/agent/logs/session-*.log
	agentOutputsDir := filepath.Join(tmpDir, "agent_outputs")
	sessionLogsDir := filepath.Join(agentOutputsDir, "sandbox", "agent", "logs")
	err := os.MkdirAll(sessionLogsDir, 0755)
	require.NoError(t, err)

	sessionLogPath := filepath.Join(sessionLogsDir, "session-copilot-20701642088.log")
	sessionLogContent := `2025-01-04T10:00:00Z [DEBUG] Test session log with token usage data
2025-01-04T10:00:01Z [DEBUG] data:
{
  "choices": [
    {
      "message": {
        "tool_calls": [
          {
            "function": {
              "name": "bash",
              "arguments": "{\"command\": \"ls -la\"}"
            }
          }
        ]
      }
    }
  ],
  "usage": {
    "total_tokens": 1234,
    "prompt_tokens": 500,
    "completion_tokens": 734
  }
}
2025-01-04T10:00:02Z [DEBUG] Tool execution completed`
	err = os.WriteFile(sessionLogPath, []byte(sessionLogContent), 0644)
	require.NoError(t, err)

	// Verify agent_outputs exists before flattening
	_, err = os.Stat(agentOutputsDir)
	require.NoError(t, err, "agent_outputs directory should exist before flattening")

	// Step 2: Flatten the artifact (mimics flattenAgentOutputsArtifact)
	err = flattenAgentOutputsArtifact(tmpDir, false)
	require.NoError(t, err, "flattenAgentOutputsArtifact should succeed")

	// Verify agent_outputs was removed after flattening
	_, err = os.Stat(agentOutputsDir)
	assert.True(t, os.IsNotExist(err), "agent_outputs directory should be removed after flattening")

	// Verify session log is now at flattened location
	flattenedLogPath := filepath.Join(tmpDir, "sandbox", "agent", "logs", "session-copilot-20701642088.log")
	_, err = os.Stat(flattenedLogPath)
	require.NoError(t, err, "Session log should exist at flattened location")

	// Verify content is intact
	content, err := os.ReadFile(flattenedLogPath)
	require.NoError(t, err)
	assert.Contains(t, string(content), "Test session log with token usage data")
	assert.Contains(t, string(content), "total_tokens")

	// Step 3: Test that findAgentLogFile can find the session log after flattening
	copilotEngine := workflow.NewCopilotEngine()
	found, ok := findAgentLogFile(tmpDir, copilotEngine)
	require.True(t, ok, "findAgentLogFile should find the session log after flattening")
	assert.Equal(t, flattenedLogPath, found, "findAgentLogFile should return the correct flattened path")

	// Verify the log file is readable and contains expected content
	foundContent, err := os.ReadFile(found)
	require.NoError(t, err)
	assert.Contains(t, string(foundContent), "Test session log with token usage data")
}

// TestCopilotLogParsingDirectFlattening tests that the Copilot parser can find session logs
// when they're flattened directly to the root directory (actual gh run download behavior)
func TestCopilotLogParsingDirectFlattening(t *testing.T) {
	tmpDir := testutil.TempDir(t, "copilot-direct-flatten-*")

	// When gh run download downloads the agent_outputs artifact,
	// it puts the contents directly in agent_outputs/ without preserving the full path
	// So if we upload /tmp/gh-aw/sandbox/agent/logs/session-*.log,
	// it ends up as agent_outputs/session-*.log (not agent_outputs/sandbox/agent/logs/session-*.log)

	// Step 1: Simulate downloaded artifacts structure (before flattening)
	agentOutputsDir := filepath.Join(tmpDir, "agent_outputs")
	err := os.MkdirAll(agentOutputsDir, 0755)
	require.NoError(t, err)

	// Create session log directly in agent_outputs
	sessionLogPath := filepath.Join(agentOutputsDir, "session-copilot-direct.log")
	sessionLogContent := `2025-01-05T10:00:00Z [DEBUG] Direct flatten test
2025-01-05T10:00:01Z [DEBUG] data:
{
  "choices": [
    {
      "message": {
        "tool_calls": [
          {
            "function": {
              "name": "bash",
              "arguments": "{\"command\": \"ls\"}"
            }
          }
        ]
      }
    }
  ],
  "usage": {
    "total_tokens": 100,
    "prompt_tokens": 50,
    "completion_tokens": 50
  }
}
2025-01-05T10:00:02Z [DEBUG] Done`
	err = os.WriteFile(sessionLogPath, []byte(sessionLogContent), 0644)
	require.NoError(t, err)

	// Step 2: Flatten the artifact
	err = flattenAgentOutputsArtifact(tmpDir, false)
	require.NoError(t, err, "flattenAgentOutputsArtifact should succeed")

	// Verify agent_outputs was removed
	_, err = os.Stat(agentOutputsDir)
	assert.True(t, os.IsNotExist(err), "agent_outputs directory should be removed after flattening")

	// Verify session log is now directly in tmpDir
	flattenedLogPath := filepath.Join(tmpDir, "session-copilot-direct.log")
	_, err = os.Stat(flattenedLogPath)
	require.NoError(t, err, "Session log should exist at flattened location")

	// Verify content is intact
	content, err := os.ReadFile(flattenedLogPath)
	require.NoError(t, err)
	assert.Contains(t, string(content), "Direct flatten test")

	// Step 3: Test that findAgentLogFile can find the session log via recursive search
	copilotEngine := workflow.NewCopilotEngine()
	found, ok := findAgentLogFile(tmpDir, copilotEngine)
	require.True(t, ok, "findAgentLogFile should find the session log via recursive search")
	assert.Equal(t, flattenedLogPath, found, "findAgentLogFile should return the correct path")

	// Verify the log file is readable and contains expected content
	foundContent, err := os.ReadFile(found)
	require.NoError(t, err)
	assert.Contains(t, string(foundContent), "Direct flatten test")
}

// TestCopilotLogParsingMultipleSessionFiles tests that the parser finds the first session log
// when multiple session log files exist in the flattened location
func TestCopilotLogParsingMultipleSessionFiles(t *testing.T) {
	tmpDir := testutil.TempDir(t, "copilot-multiple-sessions-*")

	// Create flattened session logs directory with multiple files
	sessionLogsDir := filepath.Join(tmpDir, "sandbox", "agent", "logs")
	err := os.MkdirAll(sessionLogsDir, 0755)
	require.NoError(t, err)

	// Create multiple session log files
	sessionFiles := []string{
		"session-copilot-001.log",
		"session-copilot-002.log",
		"session-copilot-003.log",
	}

	for _, filename := range sessionFiles {
		filePath := filepath.Join(sessionLogsDir, filename)
		err = os.WriteFile(filePath, []byte("test content for "+filename), 0644)
		require.NoError(t, err)
	}

	// Test that findAgentLogFile finds one of the session logs
	copilotEngine := workflow.NewCopilotEngine()
	found, ok := findAgentLogFile(tmpDir, copilotEngine)
	require.True(t, ok, "findAgentLogFile should find a session log")

	// Verify the found file is a .log file in the correct directory
	assert.Equal(t, ".log", filepath.Ext(found), "Found file should have .log extension")
	assert.Contains(t, found, "sandbox/agent/logs", "Found file should be in sandbox/agent/logs")
}

// TestCopilotLogParsingBackwardCompatibility tests that the old agent_output directory
// is still supported (before flattening)
func TestCopilotLogParsingBackwardCompatibility(t *testing.T) {
	tmpDir := testutil.TempDir(t, "copilot-backward-compat-*")

	// Create old-style agent_output directory (without 's')
	agentOutputDir := filepath.Join(tmpDir, "agent_output")
	err := os.MkdirAll(agentOutputDir, 0755)
	require.NoError(t, err)

	oldLogPath := filepath.Join(agentOutputDir, "debug.log")
	err = os.WriteFile(oldLogPath, []byte("old style debug log"), 0644)
	require.NoError(t, err)

	// Test that findAgentLogFile still finds logs in the old location
	copilotEngine := workflow.NewCopilotEngine()
	found, ok := findAgentLogFile(tmpDir, copilotEngine)
	require.True(t, ok, "findAgentLogFile should find logs in old agent_output directory")
	assert.Equal(t, oldLogPath, found)
}
