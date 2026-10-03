package workflow

import (
	"encoding/json"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var piLogsLog = logger.New("workflow:pi_logs")

type piLogEvent struct {
	Type           string           `json:"type"`
	Content        string           `json:"content,omitempty"`
	Delta          bool             `json:"delta,omitempty"`
	ToolName       string           `json:"tool_name,omitempty"`
	Stats          map[string]any   `json:"stats,omitempty"`
	Message        map[string]any   `json:"message,omitempty"`
	Messages       []map[string]any `json:"messages,omitempty"`
	NativeToolName string           `json:"toolName,omitempty"`
	NativeToolID   string           `json:"toolCallId,omitempty"`
	Result         map[string]any   `json:"result,omitempty"`
	Entry          map[string]any   `json:"entry,omitempty"`
}

// ParseLogMetrics handles both legacy flat logs and Pi's v3 streaming events.
func (e *PiEngine) ParseLogMetrics(logContent string, verbose bool) LogMetrics {
	piLogsLog.Printf("Parsing Pi log metrics: log_size=%d bytes, verbose=%v", len(logContent), verbose)
	state := newPiMetricState()
	for line := range strings.SplitSeq(logContent, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !strings.HasPrefix(line, "{") {
			continue
		}
		var event piLogEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}
		state.consume(event)
	}
	toolMap := make(map[string]*ToolCallInfo, len(state.tools))
	for name, count := range state.tools {
		toolMap[name] = &ToolCallInfo{Name: name, CallCount: count}
	}
	metrics := LogMetrics{EstimatedCost: state.cost, ToolCalls: []ToolCallInfo{}}
	FinalizeToolMetrics(FinalizeToolMetricsOptions{Metrics: &metrics, ToolCallMap: toolMap, Turns: state.turns, TokenUsage: state.tokens})
	return metrics
}
