//go:build !integration

package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMCPConcurrentCompileAndLogsDiagnostics(t *testing.T) {
	started := make(chan string, 3)
	release := make(chan struct{})
	defer close(release)
	execCmd := func(ctx context.Context, args ...string) *exec.Cmd {
		name := "logs"
		if args[0] == "compile" {
			name = args[len(args)-1]
		}
		started <- name
		select {
		case <-release:
		case <-ctx.Done():
		}
		if name == "logs" {
			return mockCommandWithOutput(`{"runs":[]}`, "Error: logs-only diagnostic\n")(ctx)
		}
		return exec.CommandContext(ctx, "sh", "-c", `printf 'Error: %s-only diagnostic\n' "$1" >&2; sleep 0.05; printf '  %s-only remediation\n' "$1" >&2; exit 1`, "sh", name)
	}
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "test"}, nil)
	require.NoError(t, registerCompileTool(server, execCmd, "", true))
	require.NoError(t, registerLogsTool(server, execCmd, "", false))
	session := connectInMemory(t, server)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	type response struct {
		name   string
		result *mcp.CallToolResult
		err    error
	}
	responses := make(chan response, 3)
	for _, name := range []string{"alpha", "beta", "logs"} {
		go func() {
			params := &mcp.CallToolParams{Name: "compile", Arguments: map[string]any{"workflows": []string{name}, "strict": true, "dry_run": true}}
			if name == "logs" {
				params = &mcp.CallToolParams{Name: "logs"}
			}
			result, err := session.CallTool(ctx, params)
			responses <- response{name, result, err}
		}()
	}
	for range 3 {
		select {
		case <-started:
		case <-ctx.Done():
			t.Fatal("calls did not overlap")
		}
	}
	for range 3 {
		release <- struct{}{}
	}
	for range 3 {
		got := <-responses
		require.NoError(t, got.err)
		text := extractTextResult(t, got.result)
		if got.name == "logs" {
			assert.NotContains(t, text, "-only diagnostic")
			continue
		}
		assert.Contains(t, text, got.name+"-only diagnostic")
		for _, other := range []string{"alpha", "beta", "logs"} {
			if other != got.name {
				assert.NotContains(t, text, other+"-only diagnostic")
			}
		}
	}
}

func TestMCPCompileTimeoutReturnsPartialResults(t *testing.T) {
	for _, output := range []string{`[{"workflow":"done.md","valid":true,"errors":[],"warnings":[]}]`, `[{"workflow":`, ""} {
		t.Run(output, func(t *testing.T) {
			server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "test"}, nil)
			execCmd := func(ctx context.Context, _ ...string) *exec.Cmd {
				cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestMCPTimeoutChild$")
				cmd.Env = append(os.Environ(), "GH_AW_MCP_TIMEOUT_CHILD=1", "GH_AW_MCP_TIMEOUT_OUTPUT="+output)
				return cmd
			}
			require.NoError(t, registerCompileTool(server, execCmd, "", true))
			server.AddReceivingMiddleware(func(next mcp.MethodHandler) mcp.MethodHandler {
				return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
					if method == "tools/call" {
						var cancel context.CancelFunc
						ctx, cancel = context.WithTimeout(ctx, 5250*time.Millisecond)
						defer cancel()
					}
					return next(ctx, method, req)
				}
			})
			result, err := connectInMemory(t, server).CallTool(t.Context(), &mcp.CallToolParams{
				Name: "compile", Arguments: map[string]any{"workflows": []string{"done", "pending"}},
			})
			require.NoError(t, err)
			require.True(t, result.IsError)
			var envelope map[string]any
			require.NoError(t, json.Unmarshal([]byte(extractTextResult(t, result)), &envelope))
			assert.Equal(t, true, envelope["partial"])
			assert.Equal(t, false, envelope["complete"])
			assert.Equal(t, "compilation", envelope["stage"])
			assert.Contains(t, envelope["error"], "compilation")
			assert.Contains(t, envelope["diagnostics"], "pending-only diagnostic")
			if json.Valid([]byte(output)) {
				assert.NotEmpty(t, envelope["partial_results"])
			} else {
				assert.Empty(t, envelope["partial_results"])
			}
		})
	}
}

func TestMCPTimeoutChild(t *testing.T) {
	if os.Getenv("GH_AW_MCP_TIMEOUT_CHILD") != "1" {
		return
	}
	fmt.Fprint(os.Stdout, os.Getenv("GH_AW_MCP_TIMEOUT_OUTPUT"))
	fmt.Fprintln(os.Stderr, "Error: pending-only diagnostic")
	time.Sleep(time.Minute)
	os.Exit(0)
}

func TestMCPAuditTimeoutReturnsStage(t *testing.T) {
	for _, output := range []string{`{"partial":true,"complete":false,"stage":"analysis","overview":{"run_id":123}}`, ""} {
		t.Run(output, func(t *testing.T) {
			execCmd := func(ctx context.Context, _ ...string) *exec.Cmd {
				cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestMCPTimeoutChild$")
				cmd.Env = append(os.Environ(), "GH_AW_MCP_TIMEOUT_CHILD=1", "GH_AW_MCP_TIMEOUT_OUTPUT="+output)
				return cmd
			}
			ctx, cancel := context.WithTimeout(t.Context(), 10250*time.Millisecond)
			defer cancel()
			result, _, err := newAuditToolHandler(execCmd, "", false)(ctx, nil, auditArgs{RunID: "123"})
			require.NoError(t, err)
			assert.True(t, result.IsError)
			assert.NoError(t, ctx.Err(), "the response must precede the caller's deadline")
			var envelope map[string]any
			require.NoError(t, json.Unmarshal([]byte(extractTextResult(t, result)), &envelope))
			assert.Equal(t, true, envelope["partial"])
			assert.Equal(t, false, envelope["complete"])
			stage := "audit"
			if output != "" {
				stage = "analysis"
				assert.NotEmpty(t, envelope["partial_results"])
			}
			assert.Equal(t, stage, envelope["stage"])
			assert.Contains(t, envelope["error"], stage)
		})
	}
}

func TestMCPTimeoutWaitingForSubprocessSlot(t *testing.T) {
	guardrail := defaultMCPSubprocessGuardrail
	for range maxActiveMCPChildProcesses {
		require.NoError(t, guardrail.acquire(t.Context()))
		defer guardrail.release()
	}
	execCmd := func(ctx context.Context, args ...string) *exec.Cmd {
		t.Error("a queued request must not start a subprocess")
		return exec.CommandContext(ctx, "echo")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	_, diagnostics, err := runMCPCompileOutput(ctx, execCmd)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Contains(t, diagnostics.failureMessage(err), "subprocess_slot")

	auditCtx, auditCancel := context.WithTimeout(t.Context(), 5020*time.Millisecond)
	defer auditCancel()
	result, _, err := newAuditToolHandler(execCmd, "", false)(auditCtx, nil, auditArgs{RunID: "123"})
	require.NoError(t, err)
	var envelope map[string]any
	require.NoError(t, json.Unmarshal([]byte(extractTextResult(t, result)), &envelope))
	assert.Equal(t, "subprocess_slot", envelope["stage"])
}

func TestMCPCompileReservesResponseTime(t *testing.T) {
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "test"}, nil)
	var deadline time.Time
	execCmd := func(ctx context.Context, args ...string) *exec.Cmd {
		var ok bool
		deadline, ok = ctx.Deadline()
		assert.True(t, ok)
		assert.True(t, slices.Contains(args, "--strict"))
		return mockCommandWithOutput("[]", "")(ctx)
	}
	require.NoError(t, registerCompileTool(server, execCmd, "", true))
	before := time.Now()
	result, err := connectInMemory(t, server).CallTool(t.Context(), &mcp.CallToolParams{
		Name: "compile", Arguments: map[string]any{"strict": true},
	})
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.WithinDuration(t, before.Add(55*time.Second), deadline, time.Second)
}
