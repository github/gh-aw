//go:build !integration

package cli

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMCPWriteTools_DefaultServerDenies(t *testing.T) {
	t.Parallel()
	server := createMCPServer("must-not-run", "operator", false, false, "", nil)
	session := connectInMemory(t, server)
	for _, tool := range []string{"add", "update", "fix"} {
		_, err := session.CallTool(context.Background(), &mcp.CallToolParams{
			Name: tool, Arguments: map[string]any{
				"workflows": []string{"owner/repo/workflow"}, "allow_write_tools": true,
			},
		})
		require.ErrorContains(t, err, "workflow-writing tools disabled")
		require.ErrorContains(t, err, "--allow-write-tools")
	}
}

func TestMCPWriteTools_HTTPServerDenies(t *testing.T) {
	t.Parallel()
	server := createMCPServer("must-not-run", "operator", false, false, "", nil)
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
		return server
	}, nil)
	httpServer := httptest.NewServer(handler)
	defer httpServer.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "test"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint: httpServer.URL, DisableStandaloneSSE: true,
	}, nil)
	require.NoError(t, err)
	defer session.Close()
	for _, tool := range []string{"add", "update", "fix"} {
		_, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name: tool, Arguments: map[string]any{"workflows": []string{"owner/repo/workflow"}},
		})
		require.ErrorContains(t, err, "workflow-writing tools disabled")
		require.ErrorContains(t, err, "--allow-write-tools")
	}
}

func TestMCPWriteTools_StartupCapabilityAndActorChecks(t *testing.T) {
	oldCache := mcpCache
	mcpCache = newMCPCacheStore()
	t.Cleanup(func() { mcpCache = oldCache })
	mcpCache.SetRepo("owner/repo")
	mcpCache.SetPermission("writer", "owner/repo", "write")
	mcpCache.SetPermission("reader", "owner/repo", "read")

	for _, tc := range []struct {
		name          string
		allowWrite    bool
		actor         string
		validateActor bool
		wantError     string
	}{
		{name: "default", wantError: "workflow-writing tools disabled"},
		{name: "write role cannot enable tools", actor: "writer", validateActor: true, wantError: "workflow-writing tools disabled"},
		{name: "operator opt-in", allowWrite: true},
		{name: "missing actor", allowWrite: true, validateActor: true, wantError: "permission denied"},
		{name: "read role", allowWrite: true, actor: "reader", validateActor: true, wantError: "permission denied"},
		{name: "write role and opt-in", allowWrite: true, actor: "writer", validateActor: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var invocations atomic.Int32
			execCmd := func(ctx context.Context, args ...string) *exec.Cmd {
				invocations.Add(1)
				cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestMCPWriteToolsHelper$")
				cmd.Env = append(os.Environ(), "GH_AW_MCP_WRITE_HELPER=1")
				return cmd
			}
			server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "test"}, nil)
			server.AddReceivingMiddleware(mcpWriteToolsMiddleware(tc.allowWrite, tc.actor, tc.validateActor))
			require.NoError(t, registerMCPTools(server, execCmd, tc.actor, tc.validateActor, "", tc.allowWrite))
			session := connectInMemory(t, server)

			for _, tool := range []string{"add", "update", "fix", "compile", "compile-dry-run"} {
				t.Run(tool, func(t *testing.T) {
					before := invocations.Load()
					name := tool
					args := map[string]any{"workflows": []string{"owner/repo/workflow"}}
					if tool == "compile-dry-run" {
						name = "compile"
						args["dry_run"] = true
						args["strict"] = true
					}
					result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
						Name: name, Arguments: args,
					})
					wantError := tc.wantError
					if !tc.allowWrite && tool == "compile" {
						wantError = ""
					} else if !tc.allowWrite && tool == "compile-dry-run" {
						wantError = "read-only compilation"
					}
					if wantError != "" {
						require.ErrorContains(t, err, wantError)
						if wantError == "workflow-writing tools disabled" {
							require.ErrorContains(t, err, "--allow-write-tools")
						}
						assert.Equal(t, before, invocations.Load(), "denial must precede subprocess creation")
					} else {
						require.NoError(t, err)
						require.NotNil(t, result)
						assert.False(t, result.IsError)
						assert.Equal(t, before+1, invocations.Load())
					}
				})
			}
		})
	}
}

func TestMCPWriteToolsHelper(t *testing.T) {
	if os.Getenv("GH_AW_MCP_WRITE_HELPER") != "1" {
		return
	}
	fmt.Print(`[{"workflow":"test.md","valid":true,"errors":[],"warnings":[]}]`)
	os.Exit(0)
}

func TestMCPInspect_ConfigurationOnlyArguments(t *testing.T) {
	t.Parallel()
	var gotArgs []string
	execCmd := func(ctx context.Context, args ...string) *exec.Cmd {
		gotArgs = append([]string(nil), args...)
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestMCPWriteToolsHelper$")
		cmd.Env = append(os.Environ(), "GH_AW_MCP_WRITE_HELPER=1")
		return cmd
	}
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "test"}, nil)
	registerMCPInspectTool(server, execCmd)
	session := connectInMemory(t, server)
	for _, workflow := range []string{"", "test", "workflow.md"} {
		result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
			Name: "mcp-inspect", Arguments: map[string]any{"workflow_file": workflow},
		})
		require.NoError(t, err)
		assert.False(t, result.IsError)
		wantArgs := []string{"mcp", "list", "--verbose", "--"}
		if workflow != "" {
			wantArgs = append(wantArgs, workflow)
		}
		assert.Equal(t, wantArgs, gotArgs)
	}
	for _, filter := range []string{"server", "tool"} {
		gotArgs = nil
		_, err := session.CallTool(context.Background(), &mcp.CallToolParams{
			Name: "mcp-inspect", Arguments: map[string]any{filter: "test"},
		})
		require.ErrorContains(t, err, "live MCP discovery is not available")
		assert.Nil(t, gotArgs, "unsupported live discovery must not execute a command")
	}
}

func TestMCPServerCommand_WriteToolsRequireOptIn(t *testing.T) {
	t.Parallel()
	flag := NewMCPServerCommand().Flags().Lookup("allow-write-tools")
	require.NotNil(t, flag)
	assert.Equal(t, "false", flag.DefValue)
}

func TestMCPCompile_WriteCapability(t *testing.T) {
	t.Parallel()
	for _, allowWrite := range []bool{false, true} {
		t.Run(fmt.Sprintf("allow-write=%t", allowWrite), func(t *testing.T) {
			t.Parallel()
			var gotArgs []string
			execCmd := func(ctx context.Context, args ...string) *exec.Cmd {
				gotArgs = append([]string(nil), args...)
				return mockCommandWithOutput(`[]`, "")(ctx, args...)
			}
			server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "test"}, nil)
			require.NoError(t, registerMCPTools(server, execCmd, "", false, "", allowWrite))
			session := connectInMemory(t, server)
			tools, err := session.ListTools(context.Background(), nil)
			require.NoError(t, err)
			for _, tool := range tools.Tools {
				if tool.Name == "compile" {
					assert.Equal(t, !allowWrite, tool.Annotations.ReadOnlyHint)
				}
			}
			result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
				Name: "compile", Arguments: map[string]any{"workflows": []string{"test"}},
			})
			require.NoError(t, err)
			assert.False(t, result.IsError)
			wantArgs := []string{"compile", "--validate", "--json"}
			if !allowWrite {
				wantArgs = append(wantArgs, "--no-emit", "--no-check-update", "--read-only")
			}
			wantArgs = append(wantArgs, "--strict", "--", "test")
			progressFlagIndex := slices.Index(gotArgs, "--mcp-progress-file")
			require.NotEqual(t, -1, progressFlagIndex)
			require.Less(t, progressFlagIndex, slices.Index(gotArgs, "--"))
			require.NotEmpty(t, gotArgs[progressFlagIndex+1])
			gotArgs = append(gotArgs[:progressFlagIndex], gotArgs[progressFlagIndex+2:]...)
			assert.Equal(t, wantArgs, gotArgs)
		})
	}
}

func TestMCPCompile_ReadOnlyRejectsWritingOptions(t *testing.T) {
	t.Parallel()
	for _, option := range []string{"fix", "dry_run", "zizmor", "poutine", "actionlint", "runner-guard", "syft", "grype", "grant", "yamllint"} {
		t.Run(option, func(t *testing.T) {
			t.Parallel()
			server := createMCPServer("must-not-run", "", false, false, "", nil)
			session := connectInMemory(t, server)
			_, err := session.CallTool(context.Background(), &mcp.CallToolParams{
				Name: "compile", Arguments: map[string]any{option: true},
			})
			require.ErrorContains(t, err, "read-only compilation")
			require.ErrorContains(t, err, "--allow-write-tools")
		})
	}
}

func TestMCPCompile_ArgumentCapabilityGates(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		args         compileArgs
		wantScanners bool
		wantWrites   bool
	}{
		{"default", compileArgs{}, false, false},
		{"validation", compileArgs{Strict: true, ValidateImages: true, Environment: "test"}, false, false},
		{"fix", compileArgs{Fix: true}, false, true},
		{"dry-run", compileArgs{DryRun: true}, false, true},
		{"zizmor", compileArgs{Zizmor: true}, true, true},
		{"poutine", compileArgs{Poutine: true}, true, true},
		{"actionlint", compileArgs{Actionlint: true}, true, true},
		{"runner-guard", compileArgs{RunnerGuard: true}, true, true},
		{"syft", compileArgs{Syft: true}, true, true},
		{"grype", compileArgs{Grype: true}, true, true},
		{"grant", compileArgs{Grant: true}, true, true},
		{"yamllint", compileArgs{Yamllint: true}, true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.wantScanners, tt.args.hasExternalScanners())
			assert.Equal(t, tt.wantWrites, tt.args.requiresWriteTools())
		})
	}
}
