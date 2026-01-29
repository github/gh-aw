//go:build !integration

package workflow

import (
	"testing"
)

// TestCopilotSessionJSONLParsing tests parsing of Copilot session JSONL format
// This format is written to ~/.copilot/session-state/*.jsonl and copied to logs
func TestCopilotSessionJSONLParsing(t *testing.T) {
	// Sample JSONL session content with all entry types
	logContent := `{"type":"system","subtype":"init","session_id":"copilot-test-123","tools":["Bash","Read","mcp__github__create_issue"],"model":"gpt-4"}
{"type":"assistant","message":{"content":[{"type":"text","text":"I'll help you with this task."},{"type":"tool_use","id":"tool_123","name":"Bash","input":{"command":"echo 'Hello World'","description":"Print greeting"}}]}}
{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"tool_123","content":"Hello World\n"}]}}
{"type":"assistant","message":{"content":[{"type":"tool_use","id":"tool_456","name":"mcp__github__create_issue","input":{"title":"Test Issue","body":"This is a test"}}]}}
{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"tool_456","content":"Issue created: #123"}]}}
{"type":"result","usage":{"input_tokens":150,"output_tokens":50},"num_turns":2}`

	engine := NewCopilotEngine()
	metrics := engine.ParseLogMetrics(logContent, false)

	// Verify token usage (input + output)
	expectedTokens := 200 // 150 input + 50 output
	if metrics.TokenUsage != expectedTokens {
		t.Errorf("Expected token usage %d, got %d", expectedTokens, metrics.TokenUsage)
	}

	// Verify turn count
	if metrics.Turns != 2 {
		t.Errorf("Expected 2 turns, got %d", metrics.Turns)
	}

	// Verify tool calls were tracked
	if len(metrics.ToolCalls) == 0 {
		t.Error("Expected tool calls to be tracked")
	}

	// Verify specific tools
	foundBash := false
	foundGitHub := false
	for _, tool := range metrics.ToolCalls {
		if tool.Name == "Bash" {
			foundBash = true
			if tool.CallCount != 1 {
				t.Errorf("Expected Bash call count 1, got %d", tool.CallCount)
			}
		}
		if tool.Name == "mcp__github__create_issue" {
			foundGitHub = true
			if tool.CallCount != 1 {
				t.Errorf("Expected GitHub create_issue call count 1, got %d", tool.CallCount)
			}
		}
	}

	if !foundBash {
		t.Error("Expected to find Bash tool call")
	}
	if !foundGitHub {
		t.Error("Expected to find GitHub create_issue tool call")
	}
}

// TestCopilotSessionJSONLWithDebugLogs tests parsing JSONL mixed with debug logs
func TestCopilotSessionJSONLWithDebugLogs(t *testing.T) {
	// JSONL format mixed with debug log lines (should skip debug lines)
	logContent := `[DEBUG] Starting Copilot CLI
{"type":"system","subtype":"init","session_id":"copilot-456","tools":["Bash"],"model":"gpt-4"}
[DEBUG] Processing user prompt
{"type":"assistant","message":{"content":[{"type":"tool_use","id":"tool_1","name":"Bash","input":{"command":"ls -la"}}]}}
{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"tool_1","content":"file1.txt\nfile2.txt"}]}}
[DEBUG] Workflow completed
{"type":"result","usage":{"input_tokens":100,"output_tokens":25},"num_turns":1}`

	engine := NewCopilotEngine()
	metrics := engine.ParseLogMetrics(logContent, false)

	// Verify token usage
	expectedTokens := 125 // 100 input + 25 output
	if metrics.TokenUsage != expectedTokens {
		t.Errorf("Expected token usage %d, got %d", expectedTokens, metrics.TokenUsage)
	}

	// Verify turn count
	if metrics.Turns != 1 {
		t.Errorf("Expected 1 turn, got %d", metrics.Turns)
	}

	// Verify tool was tracked
	if len(metrics.ToolCalls) != 1 {
		t.Errorf("Expected 1 tool call, got %d", len(metrics.ToolCalls))
	}
}

// TestCopilotSessionJSONLMultipleTurns tests token accumulation across multiple turns
func TestCopilotSessionJSONLMultipleTurns(t *testing.T) {
	// Session with multiple tool calls across turns
	logContent := `{"type":"system","subtype":"init","session_id":"copilot-789","tools":["Bash","Read"],"model":"gpt-4"}
{"type":"assistant","message":{"content":[{"type":"tool_use","id":"tool_1","name":"Bash","input":{"command":"pwd"}}]}}
{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"tool_1","content":"/home/user"}]}}
{"type":"assistant","message":{"content":[{"type":"tool_use","id":"tool_2","name":"Bash","input":{"command":"ls"}}]}}
{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"tool_2","content":"file.txt"}]}}
{"type":"assistant","message":{"content":[{"type":"tool_use","id":"tool_3","name":"Read","input":{"path":"file.txt"}}]}}
{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"tool_3","content":"content"}]}}
{"type":"result","usage":{"input_tokens":300,"output_tokens":120},"num_turns":3}`

	engine := NewCopilotEngine()
	metrics := engine.ParseLogMetrics(logContent, false)

	// Verify token usage
	expectedTokens := 420 // 300 input + 120 output
	if metrics.TokenUsage != expectedTokens {
		t.Errorf("Expected token usage %d, got %d", expectedTokens, metrics.TokenUsage)
	}

	// Verify turn count
	if metrics.Turns != 3 {
		t.Errorf("Expected 3 turns, got %d", metrics.Turns)
	}

	// Verify Bash was called multiple times
	foundBash := false
	for _, tool := range metrics.ToolCalls {
		if tool.Name == "Bash" {
			foundBash = true
			if tool.CallCount != 2 {
				t.Errorf("Expected Bash call count 2, got %d", tool.CallCount)
			}
		}
	}

	if !foundBash {
		t.Error("Expected to find Bash tool calls")
	}
}

// TestCopilotSessionJSONLFallbackToDebugLog tests fallback when JSONL is not present
func TestCopilotSessionJSONLFallbackToDebugLog(t *testing.T) {
	// Pure debug log format (no JSONL entries) - should fall back to old parser
	logContent := `2025-09-26T11:13:11.798Z [DEBUG] Using model: gpt-4
2025-09-26T11:13:11.798Z [DEBUG] Starting Copilot CLI: 0.0.327
2025-09-26T11:13:12.575Z [DEBUG] data:
2025-09-26T11:13:12.575Z [DEBUG] {
2025-09-26T11:13:12.575Z [DEBUG]   "id": "chatcmpl-ABC123",
2025-09-26T11:13:12.575Z [DEBUG]   "model": "gpt-4",
2025-09-26T11:13:12.575Z [DEBUG]   "choices": [{
2025-09-26T11:13:12.575Z [DEBUG]     "message": {
2025-09-26T11:13:12.575Z [DEBUG]       "role": "assistant",
2025-09-26T11:13:12.575Z [DEBUG]       "content": "Test response",
2025-09-26T11:13:12.575Z [DEBUG]       "tool_calls": [{
2025-09-26T11:13:12.575Z [DEBUG]         "id": "call_123",
2025-09-26T11:13:12.575Z [DEBUG]         "type": "function",
2025-09-26T11:13:12.575Z [DEBUG]         "function": {
2025-09-26T11:13:12.575Z [DEBUG]           "name": "bash",
2025-09-26T11:13:12.575Z [DEBUG]           "arguments": "{\"command\":\"echo test\"}"
2025-09-26T11:13:12.575Z [DEBUG]         }
2025-09-26T11:13:12.575Z [DEBUG]       }]
2025-09-26T11:13:12.575Z [DEBUG]     }
2025-09-26T11:13:12.575Z [DEBUG]   }],
2025-09-26T11:13:12.575Z [DEBUG]   "usage": {
2025-09-26T11:13:12.575Z [DEBUG]     "prompt_tokens": 100,
2025-09-26T11:13:12.575Z [DEBUG]     "completion_tokens": 20,
2025-09-26T11:13:12.575Z [DEBUG]     "total_tokens": 120
2025-09-26T11:13:12.575Z [DEBUG]   }
2025-09-26T11:13:12.575Z [DEBUG] }`

	engine := NewCopilotEngine()
	metrics := engine.ParseLogMetrics(logContent, false)

	// Should successfully parse using debug log fallback
	expectedTokens := 120
	if metrics.TokenUsage != expectedTokens {
		t.Errorf("Expected token usage %d from fallback parser, got %d", expectedTokens, metrics.TokenUsage)
	}

	// Should have at least some metrics
	if metrics.TokenUsage == 0 {
		t.Error("Fallback parser should have extracted token usage")
	}
}

// TestCopilotSessionJSONLToolSizes tests tool input/output size tracking
func TestCopilotSessionJSONLToolSizes(t *testing.T) {
	// Session with tools that have varying input sizes
	logContent := `{"type":"system","subtype":"init","session_id":"copilot-size-test","tools":["Bash"],"model":"gpt-4"}
{"type":"assistant","message":{"content":[{"type":"tool_use","id":"tool_1","name":"Bash","input":{"command":"echo 'small'","description":"Small command"}}]}}
{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"tool_1","content":"small output"}]}}
{"type":"assistant","message":{"content":[{"type":"tool_use","id":"tool_2","name":"Bash","input":{"command":"echo 'This is a much longer command with more content'","description":"Larger command"}}]}}
{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"tool_2","content":"This is a much longer output with significantly more content"}]}}
{"type":"result","usage":{"input_tokens":200,"output_tokens":80},"num_turns":2}`

	engine := NewCopilotEngine()
	metrics := engine.ParseLogMetrics(logContent, false)

	// Find Bash tool
	var bashTool *ToolCallInfo
	for i := range metrics.ToolCalls {
		if metrics.ToolCalls[i].Name == "Bash" {
			bashTool = &metrics.ToolCalls[i]
			break
		}
	}

	if bashTool == nil {
		t.Fatal("Expected to find Bash tool call")
	}

	// Verify call count
	if bashTool.CallCount != 2 {
		t.Errorf("Expected 2 Bash calls, got %d", bashTool.CallCount)
	}

	// Verify that max input size was tracked (should be larger than 0)
	if bashTool.MaxInputSize == 0 {
		t.Error("Expected MaxInputSize to be tracked")
	}
}
