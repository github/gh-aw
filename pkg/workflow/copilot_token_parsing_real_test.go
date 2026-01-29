//go:build !integration

package workflow

import (
	"os"
	"testing"
)

// TestCopilotTokenCountWithRealLog tests the Copilot log parser with a real workflow run log
// from GitHub Actions run 20680961415, job 59375124033.
//
// This test validates that the token parser correctly accumulates tokens from all API responses
// in a real-world Copilot CLI log file containing 50 API responses.
//
// Expected behavior:
// - Parse all 50 API responses from the debug log
// - Accumulate token counts from each response
// - Total should be 2,484,450 tokens (sum of all responses)
func TestCopilotTokenCountWithRealLog(t *testing.T) {
	// Read the real log file from the test run
	logPath := "/tmp/test-logs/sandbox/agent/logs/session-352c1c9a-cace-4e06-8b1c-c6d922210736.log"

	// Check if file exists (this test only runs if the real log is available)
	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		t.Skip("Real log file not available - skipping real-world test")
	}

	logContent, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("Failed to read log file: %v", err)
	}

	engine := NewCopilotEngine()
	metrics := engine.ParseLogMetrics(string(logContent), false)

	// The log contains 50 API responses with the following token counts:
	// Sum of all total_tokens values = 2,484,450 tokens
	expectedTokens := 2484450

	// Allow for some variation in case the test data changes slightly
	// but the value should be very close to the expected sum
	tolerance := 1000 // Allow 1k tokens difference

	if metrics.TokenUsage < expectedTokens-tolerance || metrics.TokenUsage > expectedTokens+tolerance {
		t.Errorf("Expected token count around %d (±%d), got %d", expectedTokens, tolerance, metrics.TokenUsage)
	}

	t.Logf("Successfully parsed %d tokens from real Copilot log with 50 API responses", metrics.TokenUsage)
}
