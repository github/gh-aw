//go:build !integration

package workflow

import (
	"testing"
)

// TestCopilotTokenCountAccumulation tests that Copilot log parser correctly accumulates
// token counts from multiple API responses in the debug log.
//
// Background:
// Copilot CLI makes multiple API calls during a workflow run (one per turn).
// Each API response contains a usage object with token counts.
// The total token usage for a run should be the sum of all API responses.
//
// Implementation:
// - Go parser (copilot_logs.go): Parses JSON blocks and accumulates via ExtractJSONMetrics
// - JS parser (parse_copilot_log.cjs): Accumulates via entries._accumulatedUsage
// Both implementations should produce identical results.
func TestCopilotTokenCountAccumulation(t *testing.T) {
	// This log contains two API responses with token usage:
	// Response 1: prompt_tokens=1524, completion_tokens=89, total_tokens=1613
	// Response 2: prompt_tokens=1689, completion_tokens=23, total_tokens=1712
	// Expected accumulated total: 1613 + 1712 = 3325

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
2025-09-26T11:13:17.990Z [DEBUG]         "content": "I'll help you summarize and print the message using the print tool.",
2025-09-26T11:13:17.990Z [DEBUG]         "tool_calls": [
2025-09-26T11:13:17.990Z [DEBUG]           {
2025-09-26T11:13:17.990Z [DEBUG]             "id": "call_abc123",
2025-09-26T11:13:17.990Z [DEBUG]             "type": "function",
2025-09-26T11:13:17.990Z [DEBUG]             "function": {
2025-09-26T11:13:17.990Z [DEBUG]               "name": "bash",
2025-09-26T11:13:17.990Z [DEBUG]               "arguments": "{\"command\":\"echo 'Lorem ipsum summary'\",\"description\":\"Print summary\",\"sessionId\":\"s1\",\"async\":false}"
2025-09-26T11:13:17.990Z [DEBUG]             }
2025-09-26T11:13:17.990Z [DEBUG]           }
2025-09-26T11:13:17.990Z [DEBUG]         ]
2025-09-26T11:13:17.990Z [DEBUG]       },
2025-09-26T11:13:17.990Z [DEBUG]       "finish_reason": "tool_calls"
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
2025-09-26T11:13:18.501Z [DEBUG]         "content": "Task completed successfully. The message has been printed."
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

	engine := NewCopilotEngine()
	metrics := engine.ParseLogMetrics(logContent, false)

	// The token count should be the sum of both responses
	// Response 1: 1613 tokens
	// Response 2: 1712 tokens
	// Total expected: 3325 tokens
	expectedTokens := 3325

	if metrics.TokenUsage != expectedTokens {
		t.Errorf("Expected accumulated token count %d, got %d", expectedTokens, metrics.TokenUsage)
	}

	// Validate that token count is greater than 0 (as required by CI test)
	if metrics.TokenUsage <= 0 {
		t.Errorf("Token count should be greater than 0, got %d", metrics.TokenUsage)
	}
}
