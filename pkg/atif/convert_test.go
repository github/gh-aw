package atif

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const header = `{"type":"session.format","data":{"version":1},"provenance":{"component":"collector"}}` + "\n"

func TestReconcileMetricsMapGrowth(t *testing.T) {
	for _, tc := range []struct {
		name     string
		previous map[string]json.RawMessage
		incoming map[string]json.RawMessage
		want     map[string]json.RawMessage
	}{
		{name: "nil snapshots", want: map[string]json.RawMessage{}},
		{
			name:     "first snapshot",
			incoming: map[string]json.RawMessage{"turns": json.RawMessage("2")},
			want:     map[string]json.RawMessage{"turns": json.RawMessage("2")},
		},
		{
			name:     "previous snapshot only",
			previous: map[string]json.RawMessage{"turns": json.RawMessage("1")},
			want:     map[string]json.RawMessage{"turns": json.RawMessage("1")},
		},
		{
			name:     "disjoint snapshots",
			previous: map[string]json.RawMessage{"turns": json.RawMessage("1")},
			incoming: map[string]json.RawMessage{"diagnostics": json.RawMessage("2")},
			want: map[string]json.RawMessage{
				"turns": json.RawMessage("1"), "diagnostics": json.RawMessage("2"),
			},
		},
		{
			name:     "overlapping snapshots",
			previous: map[string]json.RawMessage{"turns": json.RawMessage("1")},
			incoming: map[string]json.RawMessage{"turns": json.RawMessage("2")},
			want:     map[string]json.RawMessage{"turns": json.RawMessage("2")},
		},
		{
			name:     "non-object usage",
			previous: map[string]json.RawMessage{"usage": json.RawMessage(`{"input_tokens":1}`)},
			incoming: map[string]json.RawMessage{"usage": json.RawMessage(`[]`)},
			want:     map[string]json.RawMessage{"usage": json.RawMessage(`[]`)},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			previous, err := json.Marshal(tc.previous)
			require.NoError(t, err)
			incoming, err := json.Marshal(tc.incoming)
			require.NoError(t, err)
			merged, err := reconcileMetrics(tc.previous, tc.incoming)
			require.NoError(t, err)
			require.Equal(t, tc.want, merged)
			afterPrevious, err := json.Marshal(tc.previous)
			require.NoError(t, err)
			afterIncoming, err := json.Marshal(tc.incoming)
			require.NoError(t, err)
			require.Equal(t, previous, afterPrevious)
			require.Equal(t, incoming, afterIncoming)
		})
	}
}

func convert(t *testing.T, events string) *Trajectory {
	t.Helper()
	trajectory, err := Convert(t.Context(), strings.NewReader(header+events))
	require.NoError(t, err)
	require.NoError(t, trajectory.Validate())
	return trajectory
}

func TestConvertConversation(t *testing.T) {
	trajectory := convert(t, `{"type":"session.init","data":{"sourceEngine":"copilot","model":"test-model","sessionId":"s1"}}
{"type":"prompt.system","data":{"content":"Be helpful.\n"}}
{"type":"user.message","data":{"content":"Hello\n"},"timestamp":"2026-10-01T10:00:00Z","id":"u1"}
{"type":"assistant.reasoning","data":{"content":"Reasoning\n"}}
{"type":"assistant.message","data":{"content":"Hi\n"}}
{"type":"assistant.refusal","data":{"reason":"refusal","content":"No"}}
{"type":"vendor.extension","data":{"opaque":[false,0,null],"secret":"retained locally"},"timestamp":123}
`)
	assert.Equal(t, "s1", trajectory.SessionID)
	assert.Equal(t, Agent{Name: "copilot", Version: "unknown", ModelName: "test-model"}, trajectory.Agent)
	require.Len(t, trajectory.Steps, 7)
	assert.Equal(t, "Be helpful.\n", trajectory.Steps[1].Message)
	assert.Equal(t, "user", trajectory.Steps[2].Source)
	assert.Equal(t, "2026-10-01T10:00:00Z", trajectory.Steps[2].Timestamp)
	assert.Equal(t, "Reasoning\n", trajectory.Steps[3].ReasoningContent)
	assert.Equal(t, "Hi\n", trajectory.Steps[4].Message)
	assert.Equal(t, "agent", trajectory.Steps[5].Source)
	assert.Equal(t, "system", trajectory.Steps[6].Source)
	assert.Empty(t, trajectory.Steps[6].Timestamp)
	raw, ok := trajectory.Steps[6].Extra["unified_event"].(json.RawMessage)
	require.True(t, ok, "unified_event should retain its raw JSON value")
	assert.Contains(t, string(raw), `"opaque":[false,0,null]`)
	var first, second bytes.Buffer
	require.NoError(t, Encode(&first, trajectory))
	require.NoError(t, Encode(&second, convert(t, `{"type":"assistant.message","data":{"content":"Hi"}}`)))
	assert.True(t, strings.HasSuffix(first.String(), "\n"))
	first.Reset()
	require.NoError(t, Encode(&first, convert(t, `{"type":"assistant.message","data":{"content":"Hi"}}`)))
	assert.Equal(t, first.String(), second.String())
}

func TestConvertTools(t *testing.T) {
	trajectory := convert(t, `{"type":"tool.execution_start","data":{"toolCallId":"a","toolName":"bash","input":{"command":"echo 1"}}}
{"type":"tool.execution_start","data":{"toolCallId":"b","toolName":"read","parameters":{"path":"x"}}}
{"type":"tool.execution_complete","data":{"toolCallId":"b","output":false}}
{"type":"tool.execution_complete","data":{"toolCallId":"a","output":0,"success":false,"error":"failed"}}
{"type":"tool.execution_complete","data":{"toolCallId":"orphan","output":null}}
{"type":"tool.execution_start","data":{"toolName":"no-id","input":{}}}
{"type":"tool.execution_start","data":{"toolCallId":"bad-args","toolName":"tool","input":false}}
{"type":"tool.execution_start","data":{"toolCallId":"dangling","toolName":"tool"}}
{"type":"tool.execution_start","data":{"toolCallId":"duplicate","toolName":"tool","input":{}}}
{"type":"tool.execution_start","data":{"toolCallId":"duplicate","toolName":"tool","input":{}}}
{"type":"tool.execution_complete","data":{"toolCallId":"duplicate","output":"ambiguous"}}
`)
	require.Len(t, trajectory.Steps, 9)
	require.NotNil(t, trajectory.Steps[0].Observation)
	require.Len(t, trajectory.Steps[0].Observation.Results, 1)
	assert.Equal(t, "0", trajectory.Steps[0].Observation.Results[0].Content)
	assert.Equal(t, "a", trajectory.Steps[0].Observation.Results[0].SourceCallID)
	raw, ok := trajectory.Steps[0].Observation.Results[0].Extra["unified_event"].(json.RawMessage)
	require.True(t, ok, "unified_event should retain its raw JSON value")
	assert.Contains(t, string(raw), `"success":false`)
	require.NotNil(t, trajectory.Steps[1].Observation)
	require.Len(t, trajectory.Steps[1].Observation.Results, 1)
	assert.Equal(t, "false", trajectory.Steps[1].Observation.Results[0].Content)
	for _, index := range []int{2, 3, 4, 8} {
		assert.Equal(t, "system", trajectory.Steps[index].Source)
		assert.Empty(t, trajectory.Steps[index].ToolCalls)
	}
	assert.Nil(t, trajectory.Steps[5].Observation)
	require.Len(t, trajectory.Steps[5].ToolCalls, 1)
	assert.NotNil(t, trajectory.Steps[5].ToolCalls[0].Arguments)
}

func TestConvertEngineVersion(t *testing.T) {
	for _, tc := range []struct {
		data, version string
	}{
		{`{"sourceEngine":"copilot","version":"session-version"}`, "unknown"},
		{`{"sourceEngine":"copilot","version":"session-version","sourceEngineVersion":"1.2.3"}`, "1.2.3"},
		{`{"sourceEngine":"pydantic-ai","cliVersion":"1.2.3"}`, "1.2.3"},
	} {
		trajectory := convert(t, `{"type":"session.init","data":`+tc.data+`}`)
		require.Equal(t, tc.version, trajectory.Agent.Version)
	}
}

func TestConvertSubagentsAndStructuredContent(t *testing.T) {
	trajectory := convert(t, `{"type":"assistant.message","agentId":"child","data":{"content":"child only"}}
{"type":"session.result","agentId":"child","data":{"totalCostUsd":42,"usage":{"output_tokens":999}}}
{"type":"tool.execution_start","agentId":"child","data":{"toolCallId":"a","toolName":"bash","input":{}}}
{"type":"tool.execution_complete","data":{"toolCallId":"a","output":"not a parent call"}}
{"type":"user.message","data":{"content":[{"type":"text","text":"hello"},{"type":"image","url":"not-an-atif-media-source"}]}}
{"type":"assistant.message","data":{"content":false},"timestamp":"not-a-time"}
`)
	for _, step := range trajectory.Steps[:4] {
		require.Equal(t, "system", step.Source)
		require.Empty(t, step.Message)
	}
	require.Contains(t, trajectory.Steps[4].Message, `"type":"image"`)
	require.Equal(t, "false", trajectory.Steps[5].Message)
	require.Empty(t, trajectory.Steps[5].Timestamp)
	require.Nil(t, trajectory.FinalMetrics.CompletionTokens)
	require.Nil(t, trajectory.FinalMetrics.CostUSD)
}

func TestConvertToolProvenanceScope(t *testing.T) {
	for _, tc := range []struct {
		name, start, mismatch, match, metrics, excluded string
	}{
		{
			name:     "path",
			start:    `"path":"parent.jsonl"`,
			mismatch: `"path":"child.jsonl"`,
			match:    `"path":"parent.jsonl"`,
			metrics:  `"component":"detection"`,
			excluded: `"component":"evals"`,
		},
		{
			name:     "phase",
			start:    `"path":"parent.jsonl","phase":"first"`,
			mismatch: `"path":"parent.jsonl","phase":"second"`,
			match:    `"path":"parent.jsonl","phase":"first"`,
			metrics:  `"path":"parent.jsonl","phase":"detection","component":"detection"`,
			excluded: `"path":"parent.jsonl","phase":"evals","component":"evals"`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			trajectory := convert(t, `{"type":"tool.execution_start","data":{"toolCallId":"a","toolName":"read","input":{}},"provenance":{"component":"agent",`+tc.start+`}}
{"type":"tool.execution_complete","data":{"toolCallId":"a","output":"wrong scope"},"provenance":{"component":"agent",`+tc.mismatch+`}}
{"type":"tool.execution_complete","data":{"toolCallId":"a","output":"correct scope"},"provenance":{"component":"agent",`+tc.match+`}}
{"type":"session.result","data":{"usage":{"output_tokens":999}},"provenance":{`+tc.metrics+`}}
{"type":"assistant.message","data":{"content":"not the primary agent"},"provenance":{`+tc.excluded+`}}
`)
			require.Len(t, trajectory.Steps, 4)
			require.NotNil(t, trajectory.Steps[0].Observation)
			require.Len(t, trajectory.Steps[0].Observation.Results, 1)
			assert.Equal(t, "correct scope", trajectory.Steps[0].Observation.Results[0].Content)
			assert.Equal(t, "system", trajectory.Steps[1].Source)
			assert.Nil(t, trajectory.FinalMetrics.CompletionTokens)
			assert.Empty(t, trajectory.Steps[3].Message)
		})
	}
}

func TestConvertNestedCallerContext(t *testing.T) {
	for _, tc := range []struct {
		name   string
		events string
	}{
		{
			name: "nested session initialization",
			events: `{"type":"session.init","data":{"sourceEngine":"copilot","model":"primary"}}
{"type":"session.init","data":{"sourceEngine":"claude","model":"nested","parentToolUseId":"launch"}}
{"type":"assistant.message","data":{"agentId":"child","content":"nested caller"}}
{"type":"tool.execution_start","data":{"toolCallId":"a","toolName":"read","parentToolUseId":"launch","input":{}}}
{"type":"tool.execution_complete","data":{"toolCallId":"a","output":"not a parent call"}}
{"type":"session.result","data":{"parentToolUseId":"launch","usage":{"output_tokens":999}}}
`,
		},
		{
			name: "nested caller events",
			events: `{"type":"session.init","data":{"sourceEngine":"copilot","model":"primary"}}
{"type":"assistant.message","data":{"parentToolCallId":"launch","content":"nested caller"}}
{"type":"tool.execution_start","data":{"parentToolCallId":"launch","toolCallId":"a","toolName":"read","input":{}}}
{"type":"tool.execution_complete","data":{"parentToolCallId":"launch","toolCallId":"a","output":"not a parent call"}}
{"type":"session.result","data":{"parentToolCallId":"launch","usage":{"output_tokens":999}}}
`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			trajectory := convert(t, tc.events)
			assert.Equal(t, "copilot", trajectory.Agent.Name)
			assert.Equal(t, "primary", trajectory.Agent.ModelName)
			for _, step := range trajectory.Steps {
				assert.Equal(t, "system", step.Source)
				assert.Empty(t, step.Message)
				assert.Empty(t, step.ToolCalls)
			}
			assert.Nil(t, trajectory.FinalMetrics.CompletionTokens)
		})
	}
}

func TestConvertCopilotParentCallContext(t *testing.T) {
	for _, tc := range []struct {
		name, engine, context string
		child                 bool
	}{
		{"copilot child", "copilot", `"parentToolCallId":"launch"`, true},
		{"pi relationship", "pi", `"parentToolCallId":"launch"`, false},
		{"unknown engine", "", `"parentToolCallId":"launch"`, false},
		{"explicit parent", "pi", `"parentToolUseId":"launch","parentToolCallId":null`, true},
		{"explicit null parent", "copilot", `"parentToolUseId":null,"parentToolCallId":"launch"`, false},
		{"explicit empty parent", "copilot", `"parentToolUseId":"","parentToolCallId":"launch"`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			trajectory := convert(t, `{"type":"session.init","data":{"sourceEngine":"`+tc.engine+`"}}
{"type":"assistant.message","data":{`+tc.context+`,"content":"dialogue"}}
{"type":"tool.execution_start","data":{`+tc.context+`,"toolCallId":"a","toolName":"read","input":{}}}
{"type":"tool.execution_complete","data":{`+tc.context+`,"toolCallId":"a","output":"result"}}
{"type":"session.result","data":{`+tc.context+`,"totalCostUsd":1,"usage":{"outputTokens":9}}}
`)
			if tc.child {
				require.Len(t, trajectory.Steps, 5)
				for _, step := range trajectory.Steps {
					require.Equal(t, "system", step.Source)
					require.Empty(t, step.Message)
					require.Empty(t, step.ToolCalls)
				}
				require.Nil(t, trajectory.FinalMetrics.CompletionTokens)
				require.Nil(t, trajectory.FinalMetrics.CostUSD)
			} else {
				require.Equal(t, "dialogue", trajectory.Steps[1].Message)
				require.Equal(t, "result", trajectory.Steps[2].Observation.Results[0].Content)
				require.Equal(t, new(int64(9)), trajectory.FinalMetrics.CompletionTokens)
			}
		})
	}
	t.Run("event engine and source context", func(t *testing.T) {
		trajectory := convert(t, `{"type":"session.start","data":{"sourceEngine":"copilot"},"provenance":{"component":"agent","path":"copilot.jsonl"}}
{"type":"session.init","data":{"sourceEngine":"pi"},"provenance":{"component":"agent","path":"pi.jsonl"}}
{"type":"assistant.message","data":{"parentToolCallId":"launch","content":"child"},"provenance":{"component":"agent","path":"copilot.jsonl"}}
{"type":"assistant.message","data":{"parentToolCallId":"ordinary","content":"pi"},"provenance":{"component":"agent","path":"pi.jsonl"}}
{"type":"session.result","data":{"sourceEngine":"copilot","parentToolCallId":"launch","usage":{"outputTokens":9}}}
`)
		require.Empty(t, trajectory.Steps[2].Message)
		require.Equal(t, "pi", trajectory.Steps[3].Message)
		require.Nil(t, trajectory.FinalMetrics.CompletionTokens)
	})
}

func TestConvertToolSessionAndPhaseScope(t *testing.T) {
	for _, tc := range []struct{ name, first, second string }{
		{"session", `"sessionId":"a"`, `"sessionId":"b"`},
		{"session alias", `"session_id":"a"`, `"session_id":"b"`},
		{"phase", `"provenance":{"component":"agent","path":"same.jsonl","phase":"first"}`, `"provenance":{"component":"agent","path":"same.jsonl","phase":"second"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			trajectory := convert(t, `{"type":"tool.execution_start",`+tc.first+`,"data":{"toolCallId":"reused","toolName":"read","input":{}}}
{"type":"tool.execution_complete",`+tc.second+`,"data":{"toolCallId":"reused","output":"orphan"}}
{"type":"tool.execution_start",`+tc.second+`,"data":{"toolCallId":"reused","toolName":"read","input":{}}}
{"type":"tool.execution_complete",`+tc.first+`,"data":{"toolCallId":"reused","output":"first"}}
{"type":"tool.execution_complete",`+tc.second+`,"data":{"toolCallId":"reused","output":"second"}}
`)
			require.Len(t, trajectory.Steps, 3)
			require.Equal(t, "first", trajectory.Steps[0].Observation.Results[0].Content)
			require.Equal(t, "system", trajectory.Steps[1].Source)
			require.Equal(t, "second", trajectory.Steps[2].Observation.Results[0].Content)
		})
	}
	t.Run("data session takes precedence", func(t *testing.T) {
		trajectory := convert(t, `{"type":"tool.execution_start","sessionId":"outer","data":{"sessionId":"a","toolCallId":"id","toolName":"read"}}
{"type":"tool.execution_complete","sessionId":"outer","data":{"sessionId":"b","toolCallId":"id","output":"orphan"}}
{"type":"tool.execution_complete","sessionId":"different","data":{"sessionId":"a","toolCallId":"id","output":"matched"}}
`)
		require.Len(t, trajectory.Steps, 2)
		require.Equal(t, "matched", trajectory.Steps[0].Observation.Results[0].Content)
		require.Equal(t, "system", trajectory.Steps[1].Source)
	})
}

func TestConvertMetrics(t *testing.T) {
	for _, tc := range []struct {
		name, usage string
		prompt      *int64
		completion  *int64
		cached      *int64
	}{
		{"included", `{"input_tokens":100,"output_tokens":0,"cache_read_input_tokens":30,"input_tokens_include_cache":true}`, new(int64(100)), new(int64(0)), new(int64(30))},
		{"excluded", `{"inputTokens":100,"outputTokens":20,"cacheReadInputTokens":30,"cacheCreationInputTokens":10,"input_tokens_include_cache":false}`, new(int64(140)), new(int64(20)), new(int64(30))},
		{"unknown cache", `{"input_tokens":100,"cache_read_input_tokens":30}`, nil, nil, new(int64(30))},
		{"no cache", `{"input_tokens":0,"output_tokens":0}`, new(int64(0)), new(int64(0)), nil},
		{"null", `{"input_tokens":null,"output_tokens":null,"cache_read_input_tokens":null}`, nil, nil, nil},
		{"invalid", `{"input_tokens":-1,"output_tokens":1.5,"cache_read_input_tokens":"3"}`, nil, nil, nil},
		{"overflow", `{"input_tokens":9007199254740991,"cache_read_input_tokens":1,"input_tokens_include_cache":false}`, nil, nil, new(int64(1))},
		{"overflowed camel cache", `{"inputTokens":10,"inputTokensIncludeCache":false,"overflowedTokens":["cacheReadInputTokens"]}`, nil, nil, nil},
		{"overflowed snake cache", `{"input_tokens":10,"input_tokens_include_cache":false,"overflowed_tokens":["cache_read_input_tokens"]}`, nil, nil, nil},
		{"overflowed cache write", `{"inputTokens":10,"cacheReadInputTokens":2,"inputTokensIncludeCache":false,"overflowedTokens":["cacheCreationInputTokens"]}`, nil, nil, new(int64(2))},
		{"overflowed included cache", `{"inputTokens":10,"cacheReadInputTokens":2,"inputTokensIncludeCache":true,"overflowedTokens":["cacheReadInputTokens"]}`, new(int64(10)), nil, nil},
		{"overflowed input", `{"inputTokens":10,"inputTokensIncludeCache":true,"overflowed_tokens":["input_tokens"]}`, nil, nil, nil},
		{"overflowed output", `{"outputTokens":10,"overflowedTokens":["outputTokens"]}`, nil, nil, nil},
		{"unknown overflowed cache", `{"inputTokens":10,"overflowedTokens":["cacheReadInputTokens"]}`, nil, nil, nil},
		{"invalid required cache", `{"inputTokens":10,"cacheReadInputTokens":null,"inputTokensIncludeCache":false}`, nil, nil, nil},
		{"impossible", `{"input_tokens":1,"cache_read_input_tokens":2,"input_tokens_include_cache":true}`, nil, nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			trajectory := convert(t, `{"type":"session.result","data":{"totalCostUsd":99}}
{"type":"usage.report","data":{"usage":{"output_tokens":999}}}
{"type":"session.result","data":{"totalCostUsd":0,"usage":`+tc.usage+`}}`)
			require.Equal(t, tc.prompt, trajectory.FinalMetrics.PromptTokens)
			require.Equal(t, tc.completion, trajectory.FinalMetrics.CompletionTokens)
			require.Equal(t, tc.cached, trajectory.FinalMetrics.CachedTokens)
			require.NotNil(t, trajectory.FinalMetrics.CostUSD)
			require.Zero(t, *trajectory.FinalMetrics.CostUSD)
			require.Equal(t, 3, trajectory.FinalMetrics.TotalSteps)
		})
	}
}

func TestConvertReconcilesMetricSnapshots(t *testing.T) {
	trajectory := convert(t, `{"type":"session.result","data":{"totalCostUsd":1,"usage":{"input_tokens":10,"output_tokens":3,"cache_read_input_tokens":2,"input_tokens_include_cache":false}}}
{"type":"session.result","data":{"total_cost_usd":2,"usage":{"inputTokens":20}}}
{"type":"session.result","data":{"numTurns":3}}
{"type":"session.result","data":{"errors":["diagnostic only"],"usage":{}}}
`)
	require.Equal(t, new(int64(22)), trajectory.FinalMetrics.PromptTokens)
	require.Equal(t, new(int64(3)), trajectory.FinalMetrics.CompletionTokens)
	require.Equal(t, new(int64(2)), trajectory.FinalMetrics.CachedTokens)
	require.Equal(t, new(float64(2)), trajectory.FinalMetrics.CostUSD)

	t.Run("explicit invalidation persists", func(t *testing.T) {
		trajectory := convert(t, `{"type":"session.result","data":{"totalCostUsd":1,"usage":{"inputTokens":10,"outputTokens":3,"cacheReadInputTokens":2,"inputTokensIncludeCache":false}}}
{"type":"session.result","data":{"totalCostUsd":null,"usage":{"overflowed_tokens":["cache_read_input_tokens","output_tokens"]}}}
{"type":"session.result","data":{"numTurns":3}}
`)
		require.Nil(t, trajectory.FinalMetrics.PromptTokens)
		require.Nil(t, trajectory.FinalMetrics.CachedTokens)
		require.Nil(t, trajectory.FinalMetrics.CompletionTokens)
		require.Nil(t, trajectory.FinalMetrics.CostUSD)
	})
	t.Run("latest supplied value recovers", func(t *testing.T) {
		trajectory := convert(t, `{"type":"session.result","data":{"usage":{"inputTokens":10,"inputTokensIncludeCache":false,"overflowedTokens":["cacheReadInputTokens"]}}}
{"type":"session.result","data":{"usage":{"cache_read_input_tokens":2}}}
{"type":"session.result","data":{"numTurns":3}}
`)
		require.Equal(t, new(int64(12)), trajectory.FinalMetrics.PromptTokens)
		require.Equal(t, new(int64(2)), trajectory.FinalMetrics.CachedTokens)
	})
}

func TestConvertMetricScopes(t *testing.T) {
	for _, tc := range []struct{ name, first, second string }{
		{"session", `"sessionId":"a"`, `"sessionId":"b"`},
		{"phase", `"provenance":{"component":"agent","path":"same.jsonl","phase":"first"}`, `"provenance":{"component":"agent","path":"same.jsonl","phase":"second"}`},
		{"path", `"provenance":{"component":"agent","path":"a.jsonl"}`, `"provenance":{"component":"agent","path":"b.jsonl"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			events := `{"type":"session.result",` + tc.first + `,"data":{"totalCostUsd":1,"usage":{"inputTokens":10,"outputTokens":3}}}
{"type":"session.result",` + tc.second + `,"data":{"usage":{"outputTokens":4}}}
`
			trajectory := convert(t, events)
			require.Nil(t, trajectory.FinalMetrics.PromptTokens)
			require.Nil(t, trajectory.FinalMetrics.CostUSD)
			require.Equal(t, new(int64(4)), trajectory.FinalMetrics.CompletionTokens)
			trajectory = convert(t, events+`{"type":"session.result",`+tc.first+`,"data":{"numTurns":3}}`)
			require.Equal(t, new(int64(10)), trajectory.FinalMetrics.PromptTokens)
			require.Equal(t, new(int64(3)), trajectory.FinalMetrics.CompletionTokens)
			require.Equal(t, new(float64(1)), trajectory.FinalMetrics.CostUSD)
		})
	}
}

func TestConvertRejectsInvalidInput(t *testing.T) {
	for _, tc := range []struct{ name, input, want string }{
		{"empty", "", "missing its leading"},
		{"native", `{"type":"assistant.message","data":{}}`, "missing its leading"},
		{"version", strings.Replace(header, `"version":1`, `"version":2`, 1), "unsupported"},
		{"duplicate header", header + header, "multiple"},
		{"header only", header, "at least one step"},
		{"bad json", header + `{"bad":`, "line 2"},
		{"no object", header + `{"type":"assistant.message","data":[]}`, "line 2"},
		{"null data", header + `{"type":"assistant.message","data":null}`, "line 2"},
		{"bad type", header + `{"type":"assistant","data":{}}`, "line 2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Convert(t.Context(), strings.NewReader(tc.input))
			require.ErrorContains(t, err, tc.want)
		})
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := Convert(ctx, strings.NewReader(header))
	require.ErrorIs(t, err, context.Canceled)
	_, err = Convert(t.Context(), errorReader{})
	require.ErrorContains(t, err, "source failed")
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) { return 0, errors.New("source failed") }

func TestEncodeRejectsInvalidTrajectory(t *testing.T) {
	for _, tc := range []struct {
		name   string
		nil    bool
		mutate func(*Trajectory)
		want   string
	}{
		{
			name: "nil trajectory",
			nil:  true,
			want: "supported schema",
		},
		{
			name: "unmatched observation",
			mutate: func(trajectory *Trajectory) {
				trajectory.Steps[0].Observation = &Observation{Results: []ObservationResult{{SourceCallID: "missing"}}}
			},
			want: "unmatched",
		},
		{
			name: "unsupported schema",
			mutate: func(trajectory *Trajectory) {
				trajectory.SchemaVersion = "unsupported"
			},
			want: "supported schema",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var trajectory *Trajectory
			if !tc.nil {
				trajectory = convert(t, `{"type":"assistant.message","data":{"content":"Hi"}}`)
				tc.mutate(trajectory)
			}
			var output bytes.Buffer
			require.ErrorContains(t, Encode(&output, trajectory), tc.want)
			assert.Empty(t, output.String())
		})
	}
}

func TestCompatibilityFixtures(t *testing.T) {
	files, err := filepath.Glob("testdata/*.jsonl")
	require.NoError(t, err)
	require.NotEmpty(t, files)
	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			output := compatibilityFixtureOutput(t, file)
			golden := strings.TrimSuffix(file, ".jsonl") + ".atif"
			expected, err := os.ReadFile(golden)
			require.NoError(t, err)
			require.JSONEq(t, string(expected), string(output))
		})
	}
}

// TestCompatibilityFixturesWritesValidationOutput writes generated documents
// for Harbor's official TrajectoryValidator when ATIF_VALIDATION_DIR is set.
func TestCompatibilityFixturesWritesValidationOutput(t *testing.T) {
	dir := os.Getenv("ATIF_VALIDATION_DIR")
	if dir == "" {
		t.Skip("ATIF_VALIDATION_DIR is not set")
	}
	files, err := filepath.Glob("testdata/*.jsonl")
	require.NoError(t, err)
	require.NotEmpty(t, files)
	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			output := compatibilityFixtureOutput(t, file)
			golden := strings.TrimSuffix(file, ".jsonl") + ".atif"
			require.NoError(t, os.WriteFile(filepath.Join(dir, filepath.Base(golden)), output, 0o600))
		})
	}
}

func compatibilityFixtureOutput(t *testing.T, file string) []byte {
	t.Helper()
	input, err := os.ReadFile(file)
	require.NoError(t, err)
	trajectory, err := Convert(t.Context(), bytes.NewReader(input))
	require.NoError(t, err)
	var output bytes.Buffer
	require.NoError(t, Encode(&output, trajectory))
	return output.Bytes()
}

func TestTrajectoryValidateRejectsInvalidInput(t *testing.T) {
	for _, tc := range []struct {
		name   string
		nil    bool
		mutate func(*Trajectory)
		want   string
	}{
		{
			name: "nil trajectory",
			nil:  true,
			want: "supported schema",
		},
		{
			name: "unsupported schema",
			mutate: func(trajectory *Trajectory) {
				trajectory.SchemaVersion = "unsupported"
			},
			want: "supported schema",
		},
		{
			name: "missing agent name",
			mutate: func(trajectory *Trajectory) {
				trajectory.Agent.Name = ""
			},
			want: "supported schema",
		},
		{
			name: "missing agent version",
			mutate: func(trajectory *Trajectory) {
				trajectory.Agent.Version = ""
			},
			want: "supported schema",
		},
		{
			name: "no steps",
			mutate: func(trajectory *Trajectory) {
				trajectory.Steps = nil
			},
			want: "supported schema",
		},
		{
			name: "non-sequential step ID",
			mutate: func(trajectory *Trajectory) {
				trajectory.Steps[0].StepID = 2
			},
			want: "not sequential",
		},
		{
			name: "invalid source",
			mutate: func(trajectory *Trajectory) {
				trajectory.Steps[0].Source = "tool"
			},
			want: "invalid ATIF source",
		},
		{
			name: "agent-only fields on non-agent step",
			mutate: func(trajectory *Trajectory) {
				trajectory.Steps[0].Source = "user"
				trajectory.Steps[0].ReasoningContent = "not allowed"
			},
			want: "agent-only fields",
		},
		{
			name: "invalid timestamp",
			mutate: func(trajectory *Trajectory) {
				trajectory.Steps[0].Timestamp = "not-a-time"
			},
			want: "invalid ATIF timestamp",
		},
		{
			name: "empty tool call ID",
			mutate: func(trajectory *Trajectory) {
				trajectory.Steps[0].ToolCalls = []ToolCall{{FunctionName: "read", Arguments: map[string]json.RawMessage{}}}
			},
			want: "invalid ATIF tool call",
		},
		{
			name: "empty tool function name",
			mutate: func(trajectory *Trajectory) {
				trajectory.Steps[0].ToolCalls = []ToolCall{{ToolCallID: "call", Arguments: map[string]json.RawMessage{}}}
			},
			want: "invalid ATIF tool call",
		},
		{
			name: "nil tool arguments",
			mutate: func(trajectory *Trajectory) {
				trajectory.Steps[0].ToolCalls = []ToolCall{{ToolCallID: "call", FunctionName: "read"}}
			},
			want: "invalid ATIF tool call",
		},
		{
			name: "duplicate tool call ID",
			mutate: func(trajectory *Trajectory) {
				call := ToolCall{ToolCallID: "call", FunctionName: "read", Arguments: map[string]json.RawMessage{}}
				trajectory.Steps[0].ToolCalls = []ToolCall{call, call}
			},
			want: "invalid ATIF tool call",
		},
		{
			name: "unmatched observation",
			mutate: func(trajectory *Trajectory) {
				trajectory.Steps[0].Observation = &Observation{Results: []ObservationResult{{SourceCallID: "missing"}}}
			},
			want: "unmatched ATIF observation",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var trajectory *Trajectory
			if !tc.nil {
				trajectory = &Trajectory{
					SchemaVersion: SchemaVersion,
					Agent:         Agent{Name: "agent", Version: "1"},
					Steps:         []Step{{StepID: 1, Source: "agent", Message: "message"}},
				}
				tc.mutate(trajectory)
			}
			require.ErrorContains(t, trajectory.Validate(), tc.want)
		})
	}
}

type failingWriter struct {
	err error
}

func (w failingWriter) Write([]byte) (int, error) {
	return 0, w.err
}

func TestEncodeReturnsWriterError(t *testing.T) {
	want := errors.New("writer failed")
	trajectory := convert(t, `{"type":"assistant.message","data":{"content":"Hi"}}`)

	err := Encode(failingWriter{err: want}, trajectory)

	require.ErrorIs(t, err, want)
}
