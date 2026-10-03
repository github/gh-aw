//go:build !integration

package workflow

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCodexParseLogMetricsNativeFixtures(t *testing.T) {
	for _, test := range []struct {
		name       string
		tokens     int
		toolCounts map[string]int
	}{
		{"codex_ci_smoke", 36716, map[string]int{"bash_example-check --vers...": 1}},
		{"codex_ci_mcp", 118247, map[string]int{"bash_example-check": 1, "github_search_pull_requests": 1}},
	} {
		t.Run(test.name, func(t *testing.T) {
			content, err := os.ReadFile("../../actions/setup/js/test_data/" + test.name + ".jsonl")
			require.NoError(t, err)
			metrics := NewCodexEngine().ParseLogMetrics(string(content), false)
			assert.Equal(t, test.tokens, metrics.TokenUsage)
			assert.Equal(t, 1, metrics.Turns)
			require.Len(t, metrics.ToolCalls, len(test.toolCounts))
			for _, tool := range metrics.ToolCalls {
				assert.Equal(t, test.toolCounts[tool.Name], tool.CallCount, tool.Name)
				assert.Positive(t, tool.MaxOutputSize)
				assert.Zero(t, tool.MaxDuration, "Native fixture contains no observed duration")
			}
			assert.Zero(t, metrics.EstimatedCost)
			require.Len(t, metrics.ToolSequences, 1)
			assert.Len(t, metrics.ToolSequences[0], len(test.toolCounts))
		})
	}
}

func TestCodexParseLogMetricsJSONLAccountingAndLifecycle(t *testing.T) {
	log := strings.Join([]string{
		`debug noise`,
		`{"type":"thread.started","thread_id":"first"}`,
		`{"type":"turn.started"}`,
		`{"type":"item.started","item":{"id":"tool","type":"mcp_tool_call","server":"github","tool":"lookup","arguments":{"key":"x"}}}`,
		`{"type":"item.updated","item":{"id":"tool","type":"mcp_tool_call","server":"github","tool":"lookup","arguments":{"key":"x"}}}`,
		`{"type":"item.completed","item":{"id":"tool","type":"mcp_tool_call","result":{"content":[{"text":"ok"}]},"duration_ms":12}}`,
		`{"type":"item.completed","item":{"id":"tool","type":"mcp_tool_call","result":{"content":[{"text":"ok"}]},"duration_ms":12}}`,
		`{"type":"turn.completed","turn_id":"turn","usage":{"input_tokens":100,"cached_input_tokens":80,"output_tokens":10,"reasoning_output_tokens":5}}`,
		`{"type":"turn.completed","turn_id":"turn","usage":{"input_tokens":100,"cached_input_tokens":80,"output_tokens":10}}`,
		`{"broken`,
		`{"type":"thread.started","thread_id":"second"}`,
		`{"type":"turn.started"}`,
		`{"type":"item.completed","item":{"id":"tool","type":"mcp_tool_call","server":"github","tool":"lookup","arguments":{},"result":{"content":[{"text":"longer"}]},"duration_ms":25}}`,
		`{"type":"turn.completed","turn_id":"turn","usage":{"input_tokens":200,"cached_input_tokens":180,"output_tokens":20}}`,
		`{"type":"result","num_turns":2,"usage":{"input_tokens":300,"output_tokens":30,"cache_read_input_tokens":260}}`,
	}, "\n")
	metrics := NewCodexEngine().ParseLogMetrics(log, false)
	assert.Equal(t, 330, metrics.TokenUsage, "Caches/reasoning and the compatibility snapshot must not be added twice")
	assert.Equal(t, 2, metrics.Turns)
	require.Len(t, metrics.ToolCalls, 1)
	assert.Equal(t, "github_lookup", metrics.ToolCalls[0].Name)
	assert.Equal(t, 2, metrics.ToolCalls[0].CallCount)
	assert.Equal(t, 6, metrics.ToolCalls[0].MaxOutputSize)
	assert.Equal(t, 25*time.Millisecond, metrics.ToolCalls[0].MaxDuration)
	assert.Len(t, metrics.ToolSequences, 2)
}

func TestCodexParseLogMetricsCanonicalJSONL(t *testing.T) {
	content := `[
{"type":"tool.execution_start","data":{"toolCallId":"command","toolName":"bash","input":{"command":"example"}}},
{"type":"tool.execution_complete","data":{"toolCallId":"command","success":false,"output":"failed","durationMs":40}},
{"type":"session.result","data":{"numTurns":2,"usage":{"input_tokens":10,"output_tokens":5,"cache_read_input_tokens":20,"input_tokens_include_cache":false}}},
{"type":"session.result","data":{"usage":{"input_tokens":0}}}
]`
	metrics := NewCodexEngine().ParseLogMetrics(content, false)
	assert.Equal(t, 25, metrics.TokenUsage)
	assert.Equal(t, 2, metrics.Turns)
	require.Len(t, metrics.ToolCalls, 1)
	assert.Equal(t, "bash_example", metrics.ToolCalls[0].Name)
	assert.Equal(t, 6, metrics.ToolCalls[0].MaxOutputSize)
	assert.Equal(t, 40*time.Millisecond, metrics.ToolCalls[0].MaxDuration)
}

func TestCodexParseLogMetricsJSONLDoesNotParseMessageContentAsMetrics(t *testing.T) {
	log := `{"type":"item.completed","item":{"type":"agent_message","text":"tokens used: 12,345; tool github.lookup({}) success in 10s"}}`
	metrics := NewCodexEngine().ParseLogMetrics(log, false)
	assert.Zero(t, metrics.TokenUsage)
	assert.Zero(t, metrics.Turns)
	assert.Empty(t, metrics.ToolCalls)
}

func TestCodexParseLogMetricsJSONLPartialAccounting(t *testing.T) {
	for _, test := range []struct {
		name   string
		log    string
		tokens int
	}{
		{"partial native usage", `{"type":"turn.completed","usage":{"input_tokens":10}}`, 10},
		{"known zero snapshot", `{"type":"turn.completed","usage":{"input_tokens":10,"output_tokens":2}}
{"type":"session.result","data":{"usage":{"input_tokens":0}}}`, 2},
		{"unavailable component", `{"type":"turn.completed","usage":{"input_tokens":10,"output_tokens":2}}
{"type":"session.result","data":{"usage":{"overflowed_tokens":["input_tokens"]}}}`, 2},
		{"cache only", `{"type":"session.result","data":{"usage":{"cache_read_input_tokens":20,"input_tokens_include_cache":false}}}`, 0},
		{"invalid components", `{"type":"turn.completed","usage":{"input_tokens":-1,"output_tokens":1.5}}`, 0},
		{"unsafe components", `{"type":"turn.completed","usage":{"input_tokens":9007199254740992}}`, 0},
		{"observed total", `{"type":"turn.completed","usage":{"total_tokens":100,"input_tokens":70,"output_tokens":20}}`, 100},
	} {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.tokens, NewCodexEngine().ParseLogMetrics(test.log, false).TokenUsage)
		})
	}
}

func TestCodexParseLogMetricsEnrichedLegacyLog(t *testing.T) {
	log := `tool api.fetch({})
api.fetch({}) success in 2ms:
{"content":[{"text":"ok"}]}
tokens used
100
{"type":"result","num_turns":1,"usage":{"input_tokens":80,"output_tokens":20}}`
	metrics := NewCodexEngine().ParseLogMetrics(log, false)
	assert.Equal(t, 100, metrics.TokenUsage)
	assert.Equal(t, 1, metrics.Turns)
	require.Len(t, metrics.ToolCalls, 1)
	assert.Equal(t, "api_fetch", metrics.ToolCalls[0].Name)
	assert.Equal(t, 2, metrics.ToolCalls[0].MaxOutputSize)
}

func TestCodexParseLogMetricsIgnoresLifecycleRecordsInLegacyToolOutput(t *testing.T) {
	for _, payload := range []string{
		`{"type":"result","num_turns":999,"usage":{"input_tokens":999}}`,
		`{"type":"turn.completed","usage":{"input_tokens":999}}`,
		`{"type":"session.result","data":{"numTurns":999,"usage":{"input_tokens":999}}}`,
		"[\n{\"type\":\"result\",\"usage\":{\"input_tokens\":999}},\n{\"type\":\"turn.completed\",\"usage\":{\"input_tokens\":999}}\n]",
	} {
		t.Run(payload, func(t *testing.T) {
			log := "tool api.fetch({})\napi.fetch(...) success in 2ms:\n" + payload
			metrics := NewCodexEngine().ParseLogMetrics(log, false)
			assert.Zero(t, metrics.TokenUsage)
			assert.Zero(t, metrics.Turns)
			require.Len(t, metrics.ToolCalls, 1)
			assert.Equal(t, "api_fetch", metrics.ToolCalls[0].Name)
			log += "\n{\"type\":\"thread.started\",\"thread_id\":\"real\"}\n{\"type\":\"turn.completed\",\"usage\":{\"input_tokens\":10,\"output_tokens\":2}}"
			metrics = NewCodexEngine().ParseLogMetrics(log, false)
			assert.Equal(t, 12, metrics.TokenUsage)
			assert.Equal(t, 1, metrics.Turns)
		})
	}
}
