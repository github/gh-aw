package atif

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const header = `{"type":"session.format","data":{"version":1},"provenance":{"component":"collector"}}` + "\n"

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
	require.Equal(t, "s1", trajectory.SessionID)
	require.Equal(t, Agent{Name: "copilot", Version: "unknown", ModelName: "test-model"}, trajectory.Agent)
	require.Len(t, trajectory.Steps, 7)
	require.Equal(t, "Be helpful.\n", trajectory.Steps[1].Message)
	require.Equal(t, "user", trajectory.Steps[2].Source)
	require.Equal(t, "2026-10-01T10:00:00Z", trajectory.Steps[2].Timestamp)
	require.Equal(t, "Reasoning\n", trajectory.Steps[3].ReasoningContent)
	require.Equal(t, "Hi\n", trajectory.Steps[4].Message)
	require.Equal(t, "agent", trajectory.Steps[5].Source)
	require.Equal(t, "system", trajectory.Steps[6].Source)
	require.Empty(t, trajectory.Steps[6].Timestamp)
	raw := trajectory.Steps[6].Extra["unified_event"].(json.RawMessage)
	require.Contains(t, string(raw), `"opaque":[false,0,null]`)
	var first, second bytes.Buffer
	require.NoError(t, Encode(&first, trajectory))
	require.NoError(t, Encode(&second, convert(t, `{"type":"assistant.message","data":{"content":"Hi"}}`)))
	require.True(t, strings.HasSuffix(first.String(), "\n"))
	first.Reset()
	require.NoError(t, Encode(&first, convert(t, `{"type":"assistant.message","data":{"content":"Hi"}}`)))
	require.Equal(t, first.String(), second.String())
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
	require.Equal(t, "0", trajectory.Steps[0].Observation.Results[0].Content)
	require.Equal(t, "a", trajectory.Steps[0].Observation.Results[0].SourceCallID)
	require.Contains(t, string(trajectory.Steps[0].Observation.Results[0].Extra["unified_event"].(json.RawMessage)), `"success":false`)
	require.Equal(t, "false", trajectory.Steps[1].Observation.Results[0].Content)
	for _, index := range []int{2, 3, 4, 8} {
		require.Equal(t, "system", trajectory.Steps[index].Source)
		require.Empty(t, trajectory.Steps[index].ToolCalls)
	}
	require.Nil(t, trajectory.Steps[5].Observation)
	require.NotNil(t, trajectory.Steps[5].ToolCalls[0].Arguments)
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
	trajectory := convert(t, `{"type":"tool.execution_start","data":{"toolCallId":"a","toolName":"read","input":{}},"provenance":{"component":"agent","path":"parent.jsonl"}}
{"type":"tool.execution_complete","data":{"toolCallId":"a","output":"wrong path"},"provenance":{"component":"agent","path":"child.jsonl"}}
{"type":"tool.execution_complete","data":{"toolCallId":"a","output":"correct path"},"provenance":{"component":"agent","path":"parent.jsonl"}}
{"type":"session.result","data":{"usage":{"output_tokens":999}},"provenance":{"component":"detection"}}
{"type":"assistant.message","data":{"content":"not the primary agent"},"provenance":{"component":"evals"}}
`)
	require.Equal(t, "correct path", trajectory.Steps[0].Observation.Results[0].Content)
	require.Equal(t, "system", trajectory.Steps[1].Source)
	require.Nil(t, trajectory.FinalMetrics.CompletionTokens)
	require.Empty(t, trajectory.Steps[3].Message)
}

func TestConvertNestedCallerContext(t *testing.T) {
	trajectory := convert(t, `{"type":"session.init","data":{"sourceEngine":"copilot","model":"primary"}}
{"type":"session.init","data":{"sourceEngine":"claude","model":"nested","parentToolUseId":"launch"}}
{"type":"assistant.message","data":{"agentId":"child","content":"nested caller"}}
{"type":"tool.execution_start","data":{"toolCallId":"a","toolName":"read","parentToolUseId":"launch","input":{}}}
{"type":"tool.execution_complete","data":{"toolCallId":"a","output":"not a parent call"}}
{"type":"session.result","data":{"parentToolUseId":"launch","usage":{"output_tokens":999}}}
`)
	require.Equal(t, "copilot", trajectory.Agent.Name)
	require.Equal(t, "primary", trajectory.Agent.ModelName)
	for _, step := range trajectory.Steps {
		require.Equal(t, "system", step.Source)
		require.Empty(t, step.Message)
		require.Empty(t, step.ToolCalls)
	}
	require.Nil(t, trajectory.FinalMetrics.CompletionTokens)
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
		{"impossible", `{"input_tokens":1,"cache_read_input_tokens":2,"input_tokens_include_cache":true}`, nil, nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			trajectory := convert(t, `{"type":"session.result","data":{"totalCostUsd":99,"usage":{"output_tokens":99}}}
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
	_, err = Convert(t.Context(), io.MultiReader(strings.NewReader(header), io.LimitReader(repeatingReader{}, MaxSessionBytes)))
	require.Error(t, err)
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) { return 0, errors.New("source failed") }

type repeatingReader struct{}

func (repeatingReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = ' '
	}
	return len(p), nil
}

func TestEncodeRejectsInvalidTrajectory(t *testing.T) {
	trajectory := convert(t, `{"type":"assistant.message","data":{"content":"Hi"}}`)
	trajectory.Steps[0].Observation = &Observation{Results: []ObservationResult{{SourceCallID: "missing"}}}
	var output bytes.Buffer
	require.ErrorContains(t, Encode(&output, trajectory), "unmatched")
	require.Empty(t, output.String())
}

// Fixtures are also checked with Harbor's official TrajectoryValidator. Set
// ATIF_VALIDATION_DIR to capture generated documents for that external check.
func TestCompatibilityFixtures(t *testing.T) {
	files, err := filepath.Glob("testdata/*.jsonl")
	require.NoError(t, err)
	require.NotEmpty(t, files)
	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			input, err := os.ReadFile(file)
			require.NoError(t, err)
			trajectory, err := Convert(t.Context(), bytes.NewReader(input))
			require.NoError(t, err)
			var output bytes.Buffer
			require.NoError(t, Encode(&output, trajectory))
			golden := strings.TrimSuffix(file, ".jsonl") + ".atif"
			expected, err := os.ReadFile(golden)
			require.NoError(t, err)
			require.JSONEq(t, string(expected), output.String())
			if dir := os.Getenv("ATIF_VALIDATION_DIR"); dir != "" {
				require.NoError(t, os.WriteFile(filepath.Join(dir, filepath.Base(golden)), output.Bytes(), 0o600))
			}
		})
	}
}
