//go:build !integration

package cli

import (
	"context"
	"encoding/json"
	"maps"
	"os/exec"
	"slices"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHealthToolArguments(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name    string
		options map[string]any
		flags   []string
		output  string
	}{
		{"defaults", map[string]any{}, []string{"health", "--json"}, `{"total_workflows":0}`},
		{"workflow", map[string]any{"workflow_name": "daily"}, []string{"health", "--json", "--", "daily"}, `{"workflow_name":"daily"}`},
		{"options", map[string]any{
			"workflow_name": "./-daily.md", "days": 30, "threshold": 90.5,
			"end_date": "-1h", "repo": "owner/repo", "logs_dir": "/tmp/existing cache",
		}, []string{"health", "--json", "--days=30", "--threshold=90.5", "--end-date=-1h", "--repo=owner/repo", "--logs-dir=/tmp/existing cache", "--", "./-daily.md"}, `{"total_runs":2}`},
		{"date range", map[string]any{"start_date": "-24h", "end_date": "2026-10-19"},
			[]string{"health", "--json", "--start-date=-24h", "--end-date=2026-10-19"}, `{"total_workflows":1}`},
		{"zero threshold", map[string]any{"threshold": 0}, []string{"health", "--json", "--threshold=0"}, `{}`},
		{"timeout", map[string]any{"timeout": "30s"}, []string{"health", "--json", "--timeout=30s"}, `{"collection":{"partial":true,"reason":"time_budget"}}`},
		{"unlimited", map[string]any{"timeout": "0"}, []string{"health", "--json", "--timeout=0"}, `{}`},
		{"flag-like value", map[string]any{"repo": "--help"}, []string{"health", "--json", "--repo=--help"}, `{}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var captured []string
			mockExec := func(ctx context.Context, args ...string) *exec.Cmd {
				captured = slices.Clone(args)
				return mockCommandWithOutput(tt.output, "diagnostic noise")(ctx, args...)
			}
			server := mcp.NewServer(&mcp.Implementation{Name: "test"}, nil)
			server.AddReceivingMiddleware(mcpWriteToolsMiddleware(false, "", true))
			registerHealthTool(server, mockExec)
			session := connectInMemory(t, server)
			result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "health", Arguments: tt.options})
			require.NoError(t, err)
			require.False(t, result.IsError)
			assert.Equal(t, tt.output, extractTextResult(t, result))
			assert.Equal(t, tt.flags, captured)
		})
	}
}

func TestHealthToolRejectsInvalidArguments(t *testing.T) {
	t.Parallel()
	for _, options := range []map[string]any{
		{"workflow_name": "--help"},
		{"days": 7, "start_date": "-24h"},
		{"days": "30"},
		{"threshold": "80"},
		{"unknown": true},
		{"timeout": "-1s"},
		{"timeout": "invalid"},
		{"timeout": 30},
	} {
		name, err := json.Marshal(options)
		require.NoError(t, err)
		t.Run(string(name), func(t *testing.T) {
			t.Parallel()
			executed := false
			mockExec := func(ctx context.Context, args ...string) *exec.Cmd {
				executed = true
				return mockCommandWithOutput(`{}`, "")(ctx, args...)
			}
			server := mcp.NewServer(&mcp.Implementation{Name: "test"}, nil)
			registerHealthTool(server, mockExec)
			session := connectInMemory(t, server)
			result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "health", Arguments: options})
			if err == nil {
				require.NotNil(t, result)
				assert.True(t, result.IsError)
			}
			assert.False(t, executed)
		})
	}
}

func TestHealthToolErrors(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name   string
		exec   execCmdFunc
		expect string
	}{
		{"failure", func(ctx context.Context, args ...string) *exec.Cmd {
			return exec.CommandContext(ctx, "sh", "-c", `printf '%s' '{"success":true}'; printf '%s' 'GitHub unavailable' >&2; exit 1`)
		}, "GitHub unavailable"},
		{"invalid JSON", mockCommandWithOutput("not JSON", ""), "invalid JSON"},
		{"empty output", mockCommandWithOutput("", ""), "invalid JSON"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			server := mcp.NewServer(&mcp.Implementation{Name: "test"}, nil)
			registerHealthTool(server, tt.exec)
			session := connectInMemory(t, server)
			_, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "health", Arguments: map[string]any{}})
			var rpcErr *jsonrpc.Error
			require.ErrorAs(t, err, &rpcErr)
			assert.Equal(t, int64(jsonrpc.CodeInternalError), rpcErr.Code)
			assert.Contains(t, rpcErr.Message, tt.expect)
		})
	}
}

func TestHealthToolSchema(t *testing.T) {
	t.Parallel()
	server := createMCPServer("", "", true, false, "", nil)
	session := connectInMemory(t, server)
	result, err := session.ListTools(context.Background(), &mcp.ListToolsParams{})
	require.NoError(t, err)
	var tool *mcp.Tool
	for _, candidate := range result.Tools {
		if candidate.Name == "health" {
			tool = candidate
		}
	}
	require.NotNil(t, tool)
	require.NotNil(t, tool.Annotations)
	assert.True(t, tool.Annotations.ReadOnlyHint)
	assert.True(t, tool.Annotations.IdempotentHint)
	require.NotNil(t, tool.Annotations.DestructiveHint)
	assert.False(t, *tool.Annotations.DestructiveHint)
	require.NotNil(t, tool.Annotations.OpenWorldHint)
	assert.True(t, *tool.Annotations.OpenWorldHint)
	require.NotEmpty(t, tool.Icons)
	encoded, err := json.Marshal(tool.InputSchema)
	require.NoError(t, err)
	var schema struct {
		Required   []string                   `json:"required"`
		Properties map[string]json.RawMessage `json:"properties"`
	}
	require.NoError(t, json.Unmarshal(encoded, &schema))
	assert.Empty(t, schema.Required)
	assert.ElementsMatch(t, jsonFieldNames(healthArgs{}), slices.Collect(maps.Keys(schema.Properties)))
}
