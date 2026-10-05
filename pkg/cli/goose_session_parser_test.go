//go:build !integration

package cli

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGooseUnifiedSessionReconstruction(t *testing.T) {
	t.Parallel()
	requireSessionTestNode(t)
	root := t.TempDir()
	content := `::add-mask::goose-private-fixture
{"type":"message","message":{"id":"assistant-1","role":"assistant","created":1791174000,"content":[{"type":"toolRequest","id":"call-1","toolCall":{"status":"success","value":{"name":"github__pull_request_read","arguments":{"token":"goose-private-fixture"}}}}]}}
{"type":"message","message":{"id":"user-1","role":"user","created":1791174001,"content":[{"type":"toolResponse","id":"call-1","toolResult":{"status":"success","value":{"content":[{"type":"text","text":"PR 42"}]}}}]}}
{"type":"message","message":{"id":"assistant-2","role":"assistant","created":1791174002,"content":[{"type":"text","text":"Smoke "}]}}
{"type":"message","message":{"id":"assistant-2","role":"assistant","created":1791174002,"content":[{"type":"text","text":"passed."}]}}
{"type":"complete","input_tokens":100,"output_tokens":20,"total_tokens":120,"cache_read_input_tokens":30,"cache_write_input_tokens":4}
`
	writeSessionTestFile(t, root, "agent-stdio.log", content)
	writeSessionTestFile(t, root, "agent_execution_exit_code.txt", "0")
	session, err := runSessionParser(context.Background(), "reconstruct", root, "goose")
	require.NoError(t, err)
	require.NoError(t, validateSessionJSONL(session))
	require.NotContains(t, string(session), "goose-private-fixture")
	require.NotContains(t, string(session), "non_canonical_agent_event")
	var kinds []string
	for line := range strings.SplitSeq(string(session), "\n") {
		if line == "" {
			continue
		}
		var event struct {
			Type string `json:"type"`
			Data struct {
				Content string `json:"content"`
				Usage   struct {
					InputTokens              int  `json:"inputTokens"`
					OutputTokens             int  `json:"outputTokens"`
					CacheReadInputTokens     int  `json:"cacheReadInputTokens"`
					CacheCreationInputTokens int  `json:"cacheCreationInputTokens"`
					InputTokensIncludeCache  bool `json:"inputTokensIncludeCache"`
				} `json:"usage"`
			} `json:"data"`
		}
		require.NoError(t, json.Unmarshal([]byte(line), &event))
		kinds = append(kinds, event.Type)
		if event.Type == "assistant.message" {
			require.Equal(t, "Smoke passed.", event.Data.Content)
		}
		if event.Type == "session.result" {
			require.Equal(t, 100, event.Data.Usage.InputTokens)
			require.Equal(t, 20, event.Data.Usage.OutputTokens)
			require.Equal(t, 30, event.Data.Usage.CacheReadInputTokens)
			require.Equal(t, 4, event.Data.Usage.CacheCreationInputTokens)
			require.True(t, event.Data.Usage.InputTokensIncludeCache)
		}
	}
	for _, kind := range []string{"session.format", "session.init", "tool.execution_start", "tool.execution_complete", "assistant.message", "session.result", "agent.execution"} {
		require.Contains(t, kinds, kind)
	}
}
