//go:build !integration

package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseRPCMessagesMCPErrorResult(t *testing.T) {
	t.Parallel()
	logPath := filepath.Join(t.TempDir(), "rpc-messages.jsonl")
	content := `{"timestamp":"2026-08-15T23:48:42.233Z","event":"rpc_request","_schema":"rpc-message/v2","direction":"OUT","server_id":"github","payload":{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"issue_read","arguments":{"number":1}}}}
{"timestamp":"2026-08-15T23:48:42.259Z","event":"rpc_response","_schema":"rpc-message/v2","direction":"IN","server_id":"github","payload":{"jsonrpc":"2.0","id":1,"result":{"isError":true,"content":[{"type":"text","text":"denied"}]}}}
`
	require.NoError(t, os.WriteFile(logPath, []byte(content), 0o644))

	metrics, err := parseRPCMessages(logPath, false)
	require.NoError(t, err)
	assert.Equal(t, 1, metrics.TotalErrors, "MCP isError results should count as failures")
	require.NotNil(t, metrics.Servers["github"])
	require.NotNil(t, metrics.Servers["github"].Tools["issue_read"])
	assert.Equal(t, 1, metrics.Servers["github"].Tools["issue_read"].ErrorCount)

	calls, err := buildToolCallsFromRPCMessages(logPath)
	require.NoError(t, err)
	require.Len(t, calls, 1)
	assert.Equal(t, "error", calls[0].Status)
	assert.Equal(t, "MCP tool returned an error result", calls[0].Error)
}
