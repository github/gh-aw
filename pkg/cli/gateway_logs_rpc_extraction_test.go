//go:build !integration

package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildToolCallsFromRPCMessagesPairsBeforeEmittingUnanswered(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "rpc-messages.jsonl")
	content := `{"timestamp":"2026-01-01T00:00:02Z","server_id":"github","direction":"IN","type":"RESPONSE","payload":{"id":1,"result":{}}}
{"timestamp":"2026-01-01T00:00:01Z","server_id":"github","direction":"OUT","type":"REQUEST","payload":{"id":1,"method":"tools/call","params":{"name":"matched"}}}
{"timestamp":"2026-01-01T00:00:03Z","server_id":"github","direction":"OUT","type":"REQUEST","payload":{"method":"tools/call","params":{"name":"idless"}}}
{"timestamp":"2026-01-01T00:00:04Z","server_id":"github","direction":"OUT","type":"REQUEST","payload":{"id":2,"method":"tools/call","params":{"name":"unanswered"}}}
`
	require.NoError(t, os.WriteFile(path, []byte(content), 0600))
	calls, err := buildToolCallsFromRPCMessages(path)
	require.NoError(t, err)
	require.Len(t, calls, 3)
	require.Equal(t, "idless", calls[0].ToolName)
	require.Equal(t, "unknown", calls[0].Status)
	require.Equal(t, "matched", calls[1].ToolName)
	require.Equal(t, "success", calls[1].Status)
	require.NotEmpty(t, calls[1].Duration)
	require.Equal(t, "unanswered", calls[2].ToolName)
	require.Equal(t, "unknown", calls[2].Status)
}
