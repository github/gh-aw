//go:build !integration

package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCopilotTokenExtractionFromLogs tests that token counts are correctly extracted
// from Copilot CLI logs during the audit flow.
//
// This test verifies the complete flow:
// 1. aw_info.json indicates copilot engine
// 2. Log file contains JSON blocks with token usage
// 3. extractLogMetrics correctly parses and accumulates token counts
func TestCopilotTokenExtractionFromLogs(t *testing.T) {
	tempDir := t.TempDir()

	// Create aw_info.json with copilot engine
	awInfoContent := `{
		"engine_id": "copilot",
		"engine_name": "GitHub Copilot CLI",
		"model": "gpt-4"
	}`
	awInfoPath := filepath.Join(tempDir, "aw_info.json")
	err := os.WriteFile(awInfoPath, []byte(awInfoContent), 0644)
	require.NoError(t, err)

	// Create a log file with Copilot debug output containing token usage
	logContent := `2025-09-26T11:13:11.798Z [DEBUG] Using model: claude-sonnet-4
2025-09-26T11:13:11.798Z [DEBUG] Starting Copilot CLI: 0.0.327
2025-09-26T11:13:12.575Z [START-GROUP] Sending request to the AI model
2025-09-26T11:13:17.989Z [DEBUG] response (Request-ID 00000-4ceedfde-6029-4de1-8779-91e88341692f):
2025-09-26T11:13:17.989Z [DEBUG] data:
2025-09-26T11:13:17.989Z [DEBUG] {
2025-09-26T11:13:17.990Z [DEBUG]   "id": "chatcmpl-ABC123",
2025-09-26T11:13:17.990Z [DEBUG]   "object": "chat.completion",
2025-09-26T11:13:17.990Z [DEBUG]   "created": 1727348000,
2025-09-26T11:13:17.990Z [DEBUG]   "model": "claude-sonnet-4",
2025-09-26T11:13:17.990Z [DEBUG]   "choices": [
2025-09-26T11:13:17.990Z [DEBUG]     {
2025-09-26T11:13:17.990Z [DEBUG]       "index": 0,
2025-09-26T11:13:17.990Z [DEBUG]       "message": {
2025-09-26T11:13:17.990Z [DEBUG]         "role": "assistant",
2025-09-26T11:13:17.990Z [DEBUG]         "content": "I'll help you summarize and print the message.",
2025-09-26T11:13:17.990Z [DEBUG]         "tool_calls": []
2025-09-26T11:13:17.990Z [DEBUG]       },
2025-09-26T11:13:17.990Z [DEBUG]       "finish_reason": "stop"
2025-09-26T11:13:17.990Z [DEBUG]     }
2025-09-26T11:13:17.990Z [DEBUG]   ],
2025-09-26T11:13:17.990Z [DEBUG]   "usage": {
2025-09-26T11:13:17.990Z [DEBUG]     "prompt_tokens": 1524,
2025-09-26T11:13:17.990Z [DEBUG]     "completion_tokens": 89,
2025-09-26T11:13:17.990Z [DEBUG]     "total_tokens": 1613
2025-09-26T11:13:17.990Z [DEBUG]   }
2025-09-26T11:13:17.990Z [DEBUG] }
2025-09-26T11:13:17.990Z [DEBUG] Executing tool: bash
2025-09-26T11:13:18.123Z [DEBUG] Tool execution completed
2025-09-26T11:13:18.500Z [DEBUG] response (Request-ID 00000-5df7e8ff-7139-5ef2-9889-a2f99452803g):
2025-09-26T11:13:18.500Z [DEBUG] data:
2025-09-26T11:13:18.501Z [DEBUG] {
2025-09-26T11:13:18.501Z [DEBUG]   "id": "chatcmpl-XYZ789",
2025-09-26T11:13:18.501Z [DEBUG]   "object": "chat.completion",
2025-09-26T11:13:18.501Z [DEBUG]   "created": 1727348001,
2025-09-26T11:13:18.501Z [DEBUG]   "model": "claude-sonnet-4",
2025-09-26T11:13:18.501Z [DEBUG]   "choices": [
2025-09-26T11:13:18.501Z [DEBUG]     {
2025-09-26T11:13:18.501Z [DEBUG]       "index": 0,
2025-09-26T11:13:18.501Z [DEBUG]       "message": {
2025-09-26T11:13:18.501Z [DEBUG]         "role": "assistant",
2025-09-26T11:13:18.501Z [DEBUG]         "content": "Task completed successfully."
2025-09-26T11:13:18.501Z [DEBUG]       },
2025-09-26T11:13:18.501Z [DEBUG]       "finish_reason": "stop"
2025-09-26T11:13:18.501Z [DEBUG]     }
2025-09-26T11:13:18.501Z [DEBUG]   ],
2025-09-26T11:13:18.501Z [DEBUG]   "usage": {
2025-09-26T11:13:18.501Z [DEBUG]     "prompt_tokens": 1689,
2025-09-26T11:13:18.501Z [DEBUG]     "completion_tokens": 23,
2025-09-26T11:13:18.501Z [DEBUG]     "total_tokens": 1712
2025-09-26T11:13:18.501Z [DEBUG]   }
2025-09-26T11:13:18.501Z [DEBUG] }
2025-09-26T11:13:18.502Z [DEBUG] Workflow completed`

	logPath := filepath.Join(tempDir, "agent.log")
	err = os.WriteFile(logPath, []byte(logContent), 0644)
	require.NoError(t, err)

	// Extract metrics
	metrics, err := extractLogMetrics(tempDir, false)
	require.NoError(t, err, "extractLogMetrics should succeed")

	// Verify that token counts were extracted and accumulated
	// Expected: 1613 + 1712 = 3325 tokens
	expectedTokens := 3325
	assert.Equal(t, expectedTokens, metrics.TokenUsage,
		"Token count should be accumulated from all API responses")

	// Verify it's greater than 0 (minimum requirement)
	assert.Positive(t, metrics.TokenUsage,
		"Token usage must be greater than 0 when log contains usage data")
}

// TestCopilotTokenExtractionWithSingleResponse tests extraction with just one API response
func TestCopilotTokenExtractionWithSingleResponse(t *testing.T) {
	tempDir := t.TempDir()

	// Create aw_info.json with copilot engine
	awInfoContent := `{"engine_id": "copilot"}`
	awInfoPath := filepath.Join(tempDir, "aw_info.json")
	err := os.WriteFile(awInfoPath, []byte(awInfoContent), 0644)
	require.NoError(t, err)

	// Create a log file with a single API response
	logContent := `2025-09-26T11:13:17.989Z [DEBUG] Starting Copilot CLI: 0.0.327
2025-09-26T11:13:17.989Z [DEBUG] response (Request-ID 00000-test):
2025-09-26T11:13:17.989Z [DEBUG] data:
2025-09-26T11:13:17.990Z [DEBUG] {
2025-09-26T11:13:17.990Z [DEBUG]   "usage": {
2025-09-26T11:13:17.990Z [DEBUG]     "prompt_tokens": 1000,
2025-09-26T11:13:17.990Z [DEBUG]     "completion_tokens": 500,
2025-09-26T11:13:17.990Z [DEBUG]     "total_tokens": 1500
2025-09-26T11:13:17.990Z [DEBUG]   }
2025-09-26T11:13:17.990Z [DEBUG] }`

	logPath := filepath.Join(tempDir, "agent.log")
	err = os.WriteFile(logPath, []byte(logContent), 0644)
	require.NoError(t, err)

	// Extract metrics
	metrics, err := extractLogMetrics(tempDir, false)
	require.NoError(t, err)

	// Verify token extraction
	assert.Equal(t, 1500, metrics.TokenUsage,
		"Should extract tokens from single API response")
}

// TestCopilotTokenExtractionWithNoUsageData tests when logs don't contain usage data
func TestCopilotTokenExtractionWithNoUsageData(t *testing.T) {
	tempDir := t.TempDir()

	// Create aw_info.json with copilot engine
	awInfoContent := `{"engine_id": "copilot"}`
	awInfoPath := filepath.Join(tempDir, "aw_info.json")
	err := os.WriteFile(awInfoPath, []byte(awInfoContent), 0644)
	require.NoError(t, err)

	// Create a log file without usage data
	logContent := `2025-09-26T11:13:17.989Z [DEBUG] Starting Copilot CLI: 0.0.327
2025-09-26T11:13:17.989Z [DEBUG] Some log message
2025-09-26T11:13:17.990Z [DEBUG] Another log message`

	logPath := filepath.Join(tempDir, "agent.log")
	err = os.WriteFile(logPath, []byte(logContent), 0644)
	require.NoError(t, err)

	// Extract metrics
	metrics, err := extractLogMetrics(tempDir, false)
	require.NoError(t, err)

	// When no usage data is present, token count should be 0
	assert.Equal(t, 0, metrics.TokenUsage,
		"Token usage should be 0 when log contains no usage data")
}

// TestCopilotTokenExtractionWithRealLogData tests token extraction with actual log data
// from workflow run 20696085597 (Smoke Copilot test)
func TestCopilotTokenExtractionWithRealLogData(t *testing.T) {
	// This test validates real log data if available
	realLogPath := "/tmp/run-20696085597/sandbox/agent/logs/session-dd1eedf4-2b6d-4373-942c-1447d5a6e00a.log"

	// Skip if real log is not available
	if _, err := os.Stat(realLogPath); os.IsNotExist(err) {
		t.Skip("Real log file from run 20696085597 not available")
	}

	tempDir := t.TempDir()

	// Create aw_info.json with copilot engine
	awInfoContent := `{"engine_id": "copilot"}`
	awInfoPath := filepath.Join(tempDir, "aw_info.json")
	err := os.WriteFile(awInfoPath, []byte(awInfoContent), 0644)
	require.NoError(t, err)

	// Copy real log to temp directory structure
	logDir := filepath.Join(tempDir, "sandbox", "agent", "logs")
	err = os.MkdirAll(logDir, 0755)
	require.NoError(t, err)

	realLogContent, err := os.ReadFile(realLogPath)
	require.NoError(t, err)

	logPath := filepath.Join(logDir, "session-test.log")
	err = os.WriteFile(logPath, realLogContent, 0644)
	require.NoError(t, err)

	// Extract metrics
	metrics, err := extractLogMetrics(tempDir, false)
	require.NoError(t, err, "extractLogMetrics should succeed with real log data")

	// Verify token extraction from real log
	// The real log from run 20696085597 contains approximately 530k tokens
	// (528.7k input + 1.9k output based on the summary in agent-stdio.log)
	assert.Greater(t, metrics.TokenUsage, 500000,
		"Should extract at least 500k tokens from real Smoke Copilot log")

	assert.Less(t, metrics.TokenUsage, 600000,
		"Should extract less than 600k tokens (reasonable upper bound)")

	t.Logf("Successfully extracted %d tokens from real workflow run 20696085597", metrics.TokenUsage)
}
