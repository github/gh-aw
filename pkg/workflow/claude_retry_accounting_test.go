//go:build !integration

package workflow

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClaudeRetryAccounting(t *testing.T) {
	log := `{"type":"result","session_id":"a","total_cost_usd":0.1,"usage":{"input_tokens":1000},"num_turns":3}
{"type":"result","session_id":"a","total_cost_usd":0.2,"usage":{"input_tokens":2000},"num_turns":4}
{"type":"result","session_id":"b","total_cost_usd":0.3,"usage":{"input_tokens":3000},"num_turns":5}
{"type":"result","session_id":"child","parent_tool_use_id":"task","total_cost_usd":10,"usage":{"input_tokens":100000},"num_turns":50}`
	metrics := NewClaudeEngine().ParseLogMetrics(log, false)
	assert.Equal(t, 5000, metrics.TokenUsage)
	assert.Equal(t, 9, metrics.Turns)
	assert.InDelta(t, 0.5, metrics.EstimatedCost, 0.000001)
}

func TestClaudeToolResultAttribution(t *testing.T) {
	entries := []map[string]any{
		{"type": "assistant", "message": map[string]any{"content": []any{
			map[string]any{"type": "tool_use", "id": "read", "name": "Read"},
			map[string]any{"type": "tool_use", "id": "grep", "name": "Grep"},
		}}},
		{"type": "user", "message": map[string]any{"content": []any{
			map[string]any{"type": "tool_result", "tool_use_id": "read", "content": "small123"},
			map[string]any{"type": "tool_result", "tool_use_id": "grep", "content": strings.Repeat("x", 8000)},
		}}},
	}
	raw, err := json.Marshal(entries)
	require.NoError(t, err)
	metrics := NewClaudeEngine().ParseLogMetrics(string(raw), false)
	for _, tool := range metrics.ToolCalls {
		switch tool.Name {
		case "Read":
			assert.Equal(t, 2, tool.MaxOutputSize)
		case "Grep":
			assert.Equal(t, 2000, tool.MaxOutputSize)
		}
	}
}
