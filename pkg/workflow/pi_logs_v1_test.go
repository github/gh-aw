//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPiEngine_ParseLogMetrics_V1UsageAndNestedTools(t *testing.T) {
	assistant := map[string]any{"role": "assistant", "timestamp": 2, "content": []any{}, "usage": map[string]any{"input": 100, "output": 10, "cacheRead": 20, "cacheWrite": 5, "cost": map[string]any{"total": 0.1}}}
	toolResult := map[string]any{"role": "toolResult", "toolCallId": "call-1", "usage": map[string]any{"input": 50, "output": 2, "cost": map[string]any{"total": 0.2}}}
	lines := []string{
		toJSON(map[string]any{"type": "message_end", "message": assistant}),
		toJSON(map[string]any{"type": "turn_end", "message": assistant}),
		toJSON(map[string]any{"type": "tool_execution_start", "toolCallId": "call-1", "toolName": "codemode"}),
		toJSON(map[string]any{"type": "tool_execution_start", "toolCallId": "call-1/1", "parentToolCallId": "call-1", "toolName": "bash"}),
		toJSON(map[string]any{"type": "message_end", "message": toolResult}),
		toJSON(map[string]any{"type": "compaction_end", "result": map[string]any{"firstKeptEntryId": "entry-1", "usage": map[string]any{"input": 200, "output": 20, "cost": map[string]any{"total": 0.3}}}}),
		toJSON(map[string]any{"type": "agent_end", "messages": []any{assistant, toolResult}}),
	}
	metrics := NewPiEngine().ParseLogMetrics(strings.Join(lines, "\n"), false)
	assert.Equal(t, 1, metrics.Turns)
	assert.Equal(t, 407, metrics.TokenUsage)
	assert.InDelta(t, 0.6, metrics.EstimatedCost, 0.00001)
	assert.Len(t, metrics.ToolCalls, 2)
}
