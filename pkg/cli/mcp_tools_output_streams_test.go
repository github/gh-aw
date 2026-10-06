//go:build !integration

package cli

import (
	"context"
	"os/exec"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mockCommandWithOutput(stdoutText, stderrText string) execCmdFunc {
	return func(ctx context.Context, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "sh", "-c", `printf '%s' "$1"; printf '%s' "$2" 1>&2`, "sh", stdoutText, stderrText)
	}
}

func extractTextResult(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	require.NotNil(t, result, "Tool result should not be nil")
	require.NotEmpty(t, result.Content, "Tool result should contain content")

	textContent, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok, "Tool result content should be text")
	return textContent.Text
}

func TestCompileToolDevelopmentAndEnvironmentArguments(t *testing.T) {
	t.Parallel()
	var capturedArgs []string
	mockExec := func(ctx context.Context, args ...string) *exec.Cmd {
		capturedArgs = slices.Clone(args)
		return mockCommandWithOutput(`[]`, "")(ctx, args...)
	}
	server := mcp.NewServer(&mcp.Implementation{Name: "gh-aw", Version: "test"}, nil)
	require.NoError(t, registerCompileTool(server, mockExec, ""))
	session := connectInMemory(t, server)
	_, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "compile",
		Arguments: map[string]any{
			"dev":         true,
			"environment": "test: #1",
			"workflows":   []string{"test.md"},
		},
	})
	require.NoError(t, err)
	assert.Contains(t, capturedArgs, "--dev")
	index := slices.Index(capturedArgs, "--environment")
	require.NotEqual(t, -1, index)
	require.Greater(t, len(capturedArgs), index+1)
	assert.Equal(t, "test: #1", capturedArgs[index+1])
	assert.Contains(t, capturedArgs, "test.md")
}

func TestCompileTool_UsesOnlyStdoutOnSuccess(t *testing.T) {
	t.Parallel()
	const (
		expectedStdout = `[{"workflow":"test.md","valid":true,"errors":[],"warnings":[]}]`
		stderrNoise    = "diagnostic noise should not be returned"
	)

	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1.0"}, nil)
	err := registerCompileTool(server, mockCommandWithOutput(expectedStdout, stderrNoise), "")
	require.NoError(t, err, "registerCompileTool should succeed")

	session := connectInMemory(t, server)
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "compile",
		Arguments: map[string]any{},
	})
	require.NoError(t, err, "compile tool call should succeed")

	output := extractTextResult(t, result)
	assert.JSONEq(t, expectedStdout, output, "compile tool should return subprocess stdout only")
	assert.NotContains(t, output, stderrNoise, "compile tool output should not contain stderr noise")
}

func TestCompileTool_AcceptsDeprecatedMaxTokensParameter(t *testing.T) {
	t.Parallel()
	const expectedStdout = `[{"workflow":"test.md","valid":true,"errors":[],"warnings":[]}]`

	var capturedArgs []string
	mockExecCmd := func(ctx context.Context, args ...string) *exec.Cmd {
		capturedArgs = slices.Clone(args)
		return exec.CommandContext(ctx, "sh", "-c", `printf '%s' "$1"`, "sh", expectedStdout)
	}

	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1.0"}, nil)
	err := registerCompileTool(server, mockExecCmd, "")
	require.NoError(t, err, "registerCompileTool should succeed")

	session := connectInMemory(t, server)
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "compile",
		Arguments: map[string]any{
			"max_tokens": 5000,
		},
	})
	require.NoError(t, err, "compile tool should accept deprecated max_tokens parameter")

	output := extractTextResult(t, result)
	assert.JSONEq(t, expectedStdout, output, "compile tool should still return subprocess stdout")
	assert.NotContains(t, strings.Join(capturedArgs, " "), "max_tokens", "compile command args should ignore max_tokens")
}
