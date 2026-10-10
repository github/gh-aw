//go:build !integration

package cli

import (
	"context"
	"encoding/json"
	"os/exec"
	"runtime"
	"slices"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompileToolPreservesLocalActionlintWhenDockerUnavailable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test binaries use a POSIX shell")
	}

	dir := t.TempDir()
	installTestScanner(t, dir, "actionlint", minActionlintVersion)
	t.Setenv("PATH", dir)
	ResetDockerPullState()
	t.Cleanup(ResetDockerPullState)
	SetMockDockerAvailable(false)

	var capturedArgs []string
	mockExec := func(ctx context.Context, args ...string) *exec.Cmd {
		capturedArgs = slices.Clone(args)
		return exec.CommandContext(ctx, "/bin/sh", "-c", `printf '%s' "$1"`, "sh",
			`[{"workflow":"test.md","valid":true,"errors":[],"warnings":[]}]`)
	}
	server := mcp.NewServer(&mcp.Implementation{Name: "gh-aw", Version: "test"}, nil)
	require.NoError(t, registerCompileTool(server, mockExec, ""))
	session := connectInMemory(t, server)

	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "compile",
		Arguments: map[string]any{
			"workflows":  []string{"test.md"},
			"actionlint": true,
			"grant":      true,
		},
	})
	require.NoError(t, err)
	require.False(t, result.IsError)
	assert.Contains(t, capturedArgs, "--actionlint", "the local scanner must survive the Docker fallback")
	assert.NotContains(t, capturedArgs, "--grant", "Docker-only scanning must be skipped")
	assert.Contains(t, capturedArgs, "compile")
	assert.Contains(t, capturedArgs, "--validate")
	assert.Contains(t, capturedArgs, "--json")
	assert.Contains(t, capturedArgs, "test.md")

	var results []ValidationResult
	require.NoError(t, json.Unmarshal([]byte(extractTextResult(t, result)), &results))
	require.Len(t, results, 1)
	assert.Equal(t, "test.md", results[0].Workflow)
	assert.True(t, results[0].Valid, "skipping Docker-only scanners must not invalidate compilation")
	assert.Empty(t, results[0].Errors)
	require.Len(t, results[0].Warnings, 1)
	assert.Equal(t, "docker_unavailable", results[0].Warnings[0].Type)
	assert.Contains(t, results[0].Warnings[0].Message, "Docker")
}
