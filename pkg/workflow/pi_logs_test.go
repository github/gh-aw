//go:build !integration

package workflow

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPiEngine_ParseLogMetrics_Empty(t *testing.T) {
	engine := NewPiEngine()
	metrics := engine.ParseLogMetrics("", false)

	assert.Equal(t, 0, metrics.Turns, "Empty log should have 0 turns")
	assert.Equal(t, 0, metrics.TokenUsage, "Empty log should have 0 token usage")
	assert.Empty(t, metrics.ToolCalls, "Empty log should have no tool calls")
}

func TestPiEngine_ParseLogMetrics_NonJSON(t *testing.T) {
	engine := NewPiEngine()
	metrics := engine.ParseLogMetrics("not json at all\nmore text", false)

	assert.Equal(t, 0, metrics.Turns, "Non-JSON log should have 0 turns")
	assert.Equal(t, 0, metrics.TokenUsage, "Non-JSON log should have 0 token usage")
}

func TestPiEngine_ParseLogMetrics_AssistantTurns(t *testing.T) {
	engine := NewPiEngine()

	lines := []string{
		toJSON(map[string]any{"type": "assistant", "content": "I will help you.", "delta": false}),
		toJSON(map[string]any{"type": "assistant", "content": "Here is the result.", "delta": false}),
	}
	logContent := strings.Join(lines, "\n")

	metrics := engine.ParseLogMetrics(logContent, false)
	assert.Equal(t, 2, metrics.Turns, "Should count 2 non-delta assistant messages as turns")
}

func TestPiEngine_ParseLogMetrics_DeltaNotCounted(t *testing.T) {
	engine := NewPiEngine()

	lines := []string{
		toJSON(map[string]any{"type": "assistant", "content": "part 1", "delta": true}),
		toJSON(map[string]any{"type": "assistant", "content": " part 2", "delta": true}),
	}
	logContent := strings.Join(lines, "\n")

	metrics := engine.ParseLogMetrics(logContent, false)
	assert.Equal(t, 0, metrics.Turns, "Delta assistant messages should not count as turns")
}

func TestPiEngine_ParseLogMetrics_ToolCalls(t *testing.T) {
	engine := NewPiEngine()

	lines := []string{
		toJSON(map[string]any{"type": "tool_use", "tool_name": "bash", "tool_id": "t1"}),
		toJSON(map[string]any{"type": "tool_use", "tool_name": "read_file", "tool_id": "t2"}),
		toJSON(map[string]any{"type": "tool_use", "tool_name": "bash", "tool_id": "t3"}),
	}
	logContent := strings.Join(lines, "\n")

	metrics := engine.ParseLogMetrics(logContent, false)

	toolMap := make(map[string]int)
	for _, tc := range metrics.ToolCalls {
		toolMap[tc.Name] = tc.CallCount
	}
	assert.Equal(t, 2, toolMap["bash"], "bash should be called 2 times")
	assert.Equal(t, 1, toolMap["read_file"], "read_file should be called 1 time")
}

func TestPiEngine_ParseLogMetrics_TokenUsage(t *testing.T) {
	engine := NewPiEngine()

	lines := []string{
		toJSON(map[string]any{
			"type": "result",
			"stats": map[string]any{
				"input_tokens":  float64(750),
				"output_tokens": float64(250),
				"duration_ms":   float64(5000),
			},
		}),
	}
	logContent := strings.Join(lines, "\n")

	metrics := engine.ParseLogMetrics(logContent, false)
	assert.Equal(t, 1000, metrics.TokenUsage, "Token usage should sum input + output tokens")
}

func TestPiEngine_ParseLogMetrics_FullConversation(t *testing.T) {
	engine := NewPiEngine()

	lines := []string{
		toJSON(map[string]any{"type": "init", "model": "pi-3", "session_id": "sess-1"}),
		toJSON(map[string]any{"type": "assistant", "content": "Let me check the code.", "delta": false}),
		toJSON(map[string]any{"type": "tool_use", "tool_name": "read_file", "tool_id": "t1"}),
		toJSON(map[string]any{"type": "tool_result", "tool_id": "t1", "status": "success", "output": "file content"}),
		toJSON(map[string]any{"type": "assistant", "content": "I found the issue.", "delta": false}),
		toJSON(map[string]any{
			"type": "result",
			"stats": map[string]any{
				"input_tokens":  float64(300),
				"output_tokens": float64(100),
			},
		}),
	}

	logContent := strings.Join(lines, "\n")

	metrics := engine.ParseLogMetrics(logContent, false)
	assert.Equal(t, 2, metrics.Turns, "Should count 2 assistant turns")
	assert.Equal(t, 400, metrics.TokenUsage, "Should sum 300 + 100 tokens")
	assert.Len(t, metrics.ToolCalls, 1, "Should have 1 distinct tool")
	assert.Equal(t, "read_file", metrics.ToolCalls[0].Name, "Tool call should be read_file")
	assert.Equal(t, 1, metrics.ToolCalls[0].CallCount, "read_file should be called once")
}

// toJSON serialises a map to a compact JSON string for use in test log lines.
func toJSON(v map[string]any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic("toJSON: " + err.Error())
	}
	return string(b)
}
