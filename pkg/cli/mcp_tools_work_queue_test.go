//go:build !integration

package cli

import (
	"context"
	"encoding/json"
	"io"
	"maps"
	"os/exec"
	"slices"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWorkQueueToolOperations(t *testing.T) {
	t.Parallel()
	tests := []struct {
		operation string
		options   map[string]any
		flags     []string
		output    string
	}{
		{"state", map[string]any{"graph": "graph", "pool": "default", "state": "available", "search": "--request-id=evil", "offset": 0, "limit": 2},
			[]string{"--graph=graph", "--pool=default", "--state=available", "--search=--request-id=evil", "--offset=0", "--limit=2"},
			`{"status":"committed_snapshot","rows":[],"next_offset":2}`},
		{"inspect", map[string]any{"work_id": "work"}, []string{"--work-id=work"}, `{"details":"metadata"}`},
		{"inspect", map[string]any{"claim_id": "claim"}, []string{"--claim-id=claim"}, `{"details":"claim metadata"}`},
		{"state", map[string]any{"limit": 256}, []string{"--limit=256"}, `{"rows":[]}`},
	}
	for _, tt := range tests {
		t.Run(tt.operation, func(t *testing.T) {
			t.Parallel()
			var captured []string
			mockExec := func(ctx context.Context, args ...string) *exec.Cmd {
				captured = slices.Clone(args)
				return mockCommandWithOutput(tt.output, "diagnostic noise")(ctx, args...)
			}
			server := mcp.NewServer(&mcp.Implementation{Name: "test"}, nil)
			require.NoError(t, registerWorkQueueTool(server, mockExec))
			session := connectInMemory(t, server)
			options := map[string]any{"operation": tt.operation, "repo": "owner/repo", "branch": "queue"}
			maps.Copy(options, tt.options)
			result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "work-queue", Arguments: options})
			require.NoError(t, err)
			require.False(t, result.IsError)
			assert.Equal(t, tt.output, extractTextResult(t, result))
			require.GreaterOrEqual(t, len(captured), 4)
			assert.Equal(t, []string{"work-queue", tt.operation, "--json", "--storage=git"}, captured[:4])
			expected := append([]string{"--repo=owner/repo", "--branch=queue"}, tt.flags...)
			assert.ElementsMatch(t, expected, captured[4:])
		})
	}
}

func TestWorkQueueToolRejectsInvalidArguments(t *testing.T) {
	t.Parallel()
	tests := []map[string]any{
		{},
		{"operation": "replay"},
		{"operation": "stats"},
		{"operation": "explain"},
		{"operation": "trace"},
		{"operation": "evidence"},
		{"operation": "submit"},
		{"operation": "submit-graph"},
		{"operation": "dispatch-next"},
		{"operation": "cancel"},
		{"operation": "cancel-claim"},
		{"operation": "priority"},
		{"operation": "reprioritize"},
		{"operation": "policy"},
		{"operation": "control"},
		{"operation": "reconcile"},
		{"operation": "compact"},
		{"operation": "tui"},
		{"operation": "--help"},
		{"operation": "inspect", "pool": ""},
		{"operation": "state", "work_id": "work"},
		{"operation": "inspect", "request_id": "request"},
		{"operation": "inspect", "before_claim": "claim"},
		{"operation": "inspect", "dispatch_id": "dispatch"},
		{"operation": "state", "offset": -1},
		{"operation": "state", "limit": 0},
		{"operation": "state", "limit": 257},
		{"operation": "state", "offset": nil},
		{"operation": "state", "search": nil},
		{"operation": "state", "storage": "issues"},
		{"operation": "state", "file": "payload.json"},
	}
	for _, options := range tests {
		name, err := json.Marshal(options)
		require.NoError(t, err)
		t.Run(string(name), func(t *testing.T) {
			t.Parallel()
			options["repo"] = "owner/repo"
			executed := false
			mockExec := func(ctx context.Context, args ...string) *exec.Cmd {
				executed = true
				return mockCommandWithOutput(`{}`, "")(ctx, args...)
			}
			server := mcp.NewServer(&mcp.Implementation{Name: "test"}, nil)
			require.NoError(t, registerWorkQueueTool(server, mockExec))
			session := connectInMemory(t, server)
			result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "work-queue", Arguments: options})
			if err == nil {
				require.NotNil(t, result)
				assert.True(t, result.IsError)
			}
			assert.False(t, executed)
		})
	}
}

func TestWorkQueueToolDefaultsAndExplicitEmptyFlags(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		raw   string
		flags []string
	}{
		{`{"operation":"state"}`, nil},
		{`{"operation":"state","pool":""}`, []string{"--pool="}},
	} {
		var args workQueueArgs
		require.NoError(t, json.Unmarshal([]byte(tt.raw), &args))
		command, err := workQueueMCPCommand(args, json.RawMessage(tt.raw))
		require.NoError(t, err)
		expected := append([]string{"work-queue", "state", "--json", "--storage=git"}, tt.flags...)
		assert.Equal(t, expected, command)
	}
}

func TestWorkQueueToolErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		exec   execCmdFunc
		expect string
	}{
		{"failure", func(ctx context.Context, args ...string) *exec.Cmd {
			return exec.CommandContext(ctx, "sh", "-c", `printf '%s' '{"success":true}'; printf '%s' 'queue_integrity_failed' >&2; exit 1`)
		}, "queue_integrity_failed"},
		{"invalid JSON", mockCommandWithOutput("not JSON", ""), "invalid JSON"},
		{"empty output", mockCommandWithOutput("", ""), "invalid JSON"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			server := mcp.NewServer(&mcp.Implementation{Name: "test"}, nil)
			require.NoError(t, registerWorkQueueTool(server, tt.exec))
			session := connectInMemory(t, server)
			result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "work-queue", Arguments: map[string]any{"operation": "state", "repo": "owner/repo"}})
			if err != nil {
				assert.Contains(t, err.Error(), tt.expect)
			} else {
				require.True(t, result.IsError)
				assert.Contains(t, extractTextResult(t, result), tt.expect)
			}
		})
	}
}

func TestWorkQueueToolSchema(t *testing.T) {
	t.Parallel()
	server := createMCPServer("", "", true, "", nil)
	session := connectInMemory(t, server)
	result, err := session.ListTools(context.Background(), &mcp.ListToolsParams{})
	require.NoError(t, err)
	var tool *mcp.Tool
	for _, candidate := range result.Tools {
		if candidate.Name == "work-queue" {
			tool = candidate
		}
	}
	require.NotNil(t, tool, "read-only work-queue tool remains available with actor validation")
	require.NotNil(t, tool.Annotations)
	assert.True(t, tool.Annotations.ReadOnlyHint)
	assert.True(t, tool.Annotations.IdempotentHint)
	require.NotNil(t, tool.Annotations.OpenWorldHint)
	assert.True(t, *tool.Annotations.OpenWorldHint)
	require.NotEmpty(t, tool.Icons)
	encoded, err := json.Marshal(tool.InputSchema)
	require.NoError(t, err)
	var schema struct {
		Required   []string `json:"required"`
		Properties map[string]struct {
			Enum []string `json:"enum"`
		} `json:"properties"`
	}
	require.NoError(t, json.Unmarshal(encoded, &schema))
	assert.ElementsMatch(t, []string{"operation", "repo"}, schema.Required)
	assert.ElementsMatch(t, []string{"state", "inspect"}, schema.Properties["operation"].Enum)
	for _, removed := range []string{"request_id", "before_claim", "dispatch_id"} {
		assert.NotContains(t, schema.Properties, removed)
	}
	for _, options := range []map[string]any{
		{"operation": "state"},
		{"operation": "inspect", "repo": "owner/repo", "work-id": "work"},
	} {
		result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "work-queue", Arguments: options})
		require.NoError(t, err)
		require.True(t, result.IsError)
		if _, typo := options["work-id"]; typo {
			assert.Contains(t, extractTextResult(t, result), "Did you mean 'work_id'?")
		} else {
			assert.Contains(t, extractTextResult(t, result), "repo")
		}
	}
}

func TestWorkQueueToolCLIValidation(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{
		`{"operation":"inspect","work_id":"work","claim_id":"claim"}`,
		`{"operation":"inspect"}`,
		`{"operation":"state","state":"invalid"}`,
	} {
		t.Run(raw, func(t *testing.T) {
			t.Parallel()
			var args workQueueArgs
			require.NoError(t, json.Unmarshal([]byte(raw), &args))
			cmdArgs, err := workQueueMCPCommand(args, json.RawMessage(raw))
			require.NoError(t, err)
			cmd := NewWorkCommand()
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			cmd.SetArgs(cmdArgs[1:])
			err = cmd.Execute()
			require.Error(t, err)
			assert.NotContains(t, err.Error(), "repo must be owner/repo", "CLI selector validation must fail before reading the queue")
		})
	}
}
