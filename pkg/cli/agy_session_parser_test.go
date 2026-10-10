//go:build !integration

package cli

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAgyUnifiedSessionReconstruction(t *testing.T) {
	t.Parallel()
	requireSessionTestNode(t)
	root := t.TempDir()
	content := `::add-mask::agy-private-fixture
{"event":"init","conversation_id":"agy-1","init":{"model":"gemini-3.8-flash-medium"}}
{"event":"step_update","step_update":{"step_type":"agent_response","step_index":0,"state":"ACTIVE","text_delta":"Smoke "}}
{"event":"step_update","step_update":{"step_type":"agent_response","step_index":0,"state":"DONE","text_delta":"passed."}}
{"event":"step_update","step_update":{"step_type":"tool","step_index":1,"state":"ACTIVE","tool_info":{"name":"native_challenge","parameters":{"token":"agy-private-fixture"}}}}
{"event":"step_update","step_update":{"step_type":"tool","step_index":1,"state":"DONE","tool_info":{"name":"native_challenge","output":"receipt"}}}
{"event":"result","result":{"status":"SUCCESS","num_turns":1,"usage":{"input_tokens":50,"output_tokens":10,"thinking_tokens":2,"cache_read_tokens":20,"total_tokens":60}}}
{"event":"result","result":{"status":"SUCCESS","num_turns":2,"usage":{"input_tokens":100,"output_tokens":20,"thinking_tokens":8,"cache_read_tokens":30,"total_tokens":120}}}
`
	writeSessionTestFile(t, root, "agent-stdio.log", content)
	writeSessionTestFile(t, root, "agent_execution_exit_code.txt", "0")
	session, err := runSessionParser(context.Background(), "reconstruct", root, "agy")
	require.NoError(t, err)
	require.NoError(t, validateSessionJSONL(session))
	require.NotContains(t, string(session), "agy-private-fixture")
	require.NotContains(t, string(session), "non_canonical_agent_event")
	results := 0
	for line := range strings.SplitSeq(string(session), "\n") {
		if line == "" {
			continue
		}
		var event struct {
			Type string `json:"type"`
			Data struct {
				Content string `json:"content"`
				Usage   struct {
					InputTokens             int  `json:"inputTokens"`
					OutputTokens            int  `json:"outputTokens"`
					ReasoningOutputTokens   int  `json:"reasoningOutputTokens"`
					CacheReadInputTokens    int  `json:"cacheReadInputTokens"`
					InputTokensIncludeCache bool `json:"inputTokensIncludeCache"`
				} `json:"usage"`
			} `json:"data"`
		}
		require.NoError(t, json.Unmarshal([]byte(line), &event))
		if event.Type == "assistant.message" {
			require.Equal(t, "Smoke passed.", event.Data.Content)
		}
		if event.Type == "session.result" {
			results++
			require.Equal(t, 100, event.Data.Usage.InputTokens)
			require.Equal(t, 20, event.Data.Usage.OutputTokens)
			require.Equal(t, 8, event.Data.Usage.ReasoningOutputTokens)
			require.Equal(t, 30, event.Data.Usage.CacheReadInputTokens)
			require.False(t, event.Data.Usage.InputTokensIncludeCache)
		}
	}
	require.Equal(t, 1, results, "cumulative snapshots must not double-count inference")
}
