//go:build !integration

package cli

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRpcEntryToTimelineEvent(t *testing.T) {
	t.Parallel()
	t.Run("invalid timestamp returns zero value and false", func(t *testing.T) {
		t.Parallel()
		entry := RPCMessageEntry{
			Timestamp: "not-a-timestamp",
			Type:      "DIFC_FILTERED",
		}
		evt, ok := rpcEntryToTimelineEvent(entry)
		assert.False(t, ok)
		assert.Equal(t, UnifiedTimelineEvent{}, evt)
	})

	t.Run("empty timestamp returns zero value and false", func(t *testing.T) {
		t.Parallel()
		entry := RPCMessageEntry{Type: "REQUEST", Direction: "OUT"}
		evt, ok := rpcEntryToTimelineEvent(entry)
		assert.False(t, ok)
		assert.Equal(t, UnifiedTimelineEvent{}, evt)
	})

	t.Run("DIFC_FILTERED entry converts with tool name, reason and author", func(t *testing.T) {
		t.Parallel()
		entry := RPCMessageEntry{
			Timestamp:   "2024-01-01T12:00:00Z",
			Type:        "DIFC_FILTERED",
			ServerID:    "server-1",
			ToolName:    "create_issue",
			Reason:      "secrecy violation",
			AuthorLogin: "octocat",
		}
		evt, ok := rpcEntryToTimelineEvent(entry)
		require.True(t, ok)
		assert.Equal(t, TimelineSourceGateway, evt.Source)
		assert.Equal(t, TimelineKindDIFCFiltered, evt.Kind)
		assert.Equal(t, "server-1", evt.ServerName)
		assert.Equal(t, "create_issue", evt.ToolName)
		assert.Equal(t, "secrecy violation", evt.Reason)
		assert.Equal(t, "octocat", evt.AuthorLogin)
	})

	t.Run("REQUEST with Direction IN is rejected", func(t *testing.T) {
		t.Parallel()
		entry := RPCMessageEntry{
			Timestamp: "2024-01-01T12:00:00Z",
			Type:      "REQUEST",
			Direction: "IN",
		}
		evt, ok := rpcEntryToTimelineEvent(entry)
		assert.False(t, ok)
		assert.Equal(t, UnifiedTimelineEvent{}, evt)
	})

	t.Run("REQUEST OUT with tools/call payload extracts method and tool name", func(t *testing.T) {
		t.Parallel()
		params, err := json.Marshal(rpcToolCallParams{Name: "bash"})
		require.NoError(t, err)
		payload, err := json.Marshal(rpcRequestPayload{
			Method: "tools/call",
			Params: params,
		})
		require.NoError(t, err)

		entry := RPCMessageEntry{
			Timestamp: "2024-01-01T12:00:00Z",
			Type:      "REQUEST",
			Direction: "OUT",
			ServerID:  "server-2",
			Payload:   payload,
		}
		evt, ok := rpcEntryToTimelineEvent(entry)
		require.True(t, ok)
		assert.Equal(t, TimelineSourceGateway, evt.Source)
		assert.Equal(t, TimelineKindToolCall, evt.Kind)
		assert.Equal(t, "initiated", evt.Status)
		assert.Equal(t, "server-2", evt.ServerName)
		assert.Equal(t, "tools/call", evt.Method)
		assert.Equal(t, "bash", evt.ToolName)
	})

	t.Run("REQUEST OUT with non tools/call method sets method only", func(t *testing.T) {
		t.Parallel()
		payload, err := json.Marshal(rpcRequestPayload{Method: "initialize"})
		require.NoError(t, err)

		entry := RPCMessageEntry{
			Timestamp: "2024-01-01T12:00:00Z",
			Type:      "REQUEST",
			Direction: "OUT",
			Payload:   payload,
		}
		evt, ok := rpcEntryToTimelineEvent(entry)
		require.True(t, ok)
		assert.Equal(t, "initialize", evt.Method)
		assert.Empty(t, evt.ToolName)
	})

	t.Run("REQUEST OUT with nil payload and no method/tool name is rejected", func(t *testing.T) {
		t.Parallel()
		entry := RPCMessageEntry{
			Timestamp: "2024-01-01T12:00:00Z",
			Type:      "REQUEST",
			Direction: "OUT",
			Payload:   nil,
		}
		evt, ok := rpcEntryToTimelineEvent(entry)
		assert.False(t, ok)
		assert.Equal(t, UnifiedTimelineEvent{}, evt)
	})

	t.Run("REQUEST OUT with invalid JSON payload is rejected", func(t *testing.T) {
		t.Parallel()
		entry := RPCMessageEntry{
			Timestamp: "2024-01-01T12:00:00Z",
			Type:      "REQUEST",
			Direction: "OUT",
			Payload:   json.RawMessage(`{invalid`),
		}
		evt, ok := rpcEntryToTimelineEvent(entry)
		assert.False(t, ok)
		assert.Equal(t, UnifiedTimelineEvent{}, evt)
	})

	t.Run("REQUEST OUT tools/call with invalid params JSON still uses method", func(t *testing.T) {
		t.Parallel()
		payload := json.RawMessage(`{"method":"tools/call","id":1,"params":"not-an-object"}`)

		entry := RPCMessageEntry{
			Timestamp: "2024-01-01T12:00:00Z",
			Type:      "REQUEST",
			Direction: "OUT",
			Payload:   payload,
		}
		evt, ok := rpcEntryToTimelineEvent(entry)
		require.True(t, ok)
		assert.Equal(t, "tools/call", evt.Method)
		assert.Empty(t, evt.ToolName)
	})

	t.Run("unrecognized entry type is rejected", func(t *testing.T) {
		t.Parallel()
		entry := RPCMessageEntry{
			Timestamp: "2024-01-01T12:00:00Z",
			Type:      "RESPONSE",
		}
		evt, ok := rpcEntryToTimelineEvent(entry)
		assert.False(t, ok)
		assert.Equal(t, UnifiedTimelineEvent{}, evt)
	})

	t.Run("schema rpc-message/v2 REQUEST OUT with event field (no top-level type) converts", func(t *testing.T) {
		t.Parallel()
		payload := json.RawMessage(`{"method":"tools/call","params":{"name":"list_issues"}}`)

		entry := RPCMessageEntry{
			Timestamp: "2024-01-01T12:00:00Z",
			Event:     "rpc_request",
			Direction: "OUT",
			ServerID:  "server-1",
			Payload:   payload,
		}
		evt, ok := rpcEntryToTimelineEvent(entry)
		require.True(t, ok)
		assert.Equal(t, TimelineKindToolCall, evt.Kind)
		assert.Equal(t, "list_issues", evt.ToolName)
		assert.Equal(t, "server-1", evt.ServerName)
	})

	t.Run("schema rpc-message/v2 DIFC_FILTERED entry with event field converts", func(t *testing.T) {
		t.Parallel()
		entry := RPCMessageEntry{
			Timestamp: "2024-01-01T12:00:00Z",
			Event:     "difc_filtered",
			ServerID:  "server-1",
			ToolName:  "create_issue",
			Reason:    "secrecy violation",
		}
		evt, ok := rpcEntryToTimelineEvent(entry)
		require.True(t, ok)
		assert.Equal(t, TimelineKindDIFCFiltered, evt.Kind)
		assert.Equal(t, "create_issue", evt.ToolName)
	})
}
