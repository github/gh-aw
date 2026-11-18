//go:build !integration

package cli

import (
	"context"
	"maps"
	"os/exec"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func registerArgumentTestTools(t *testing.T, execCmd execCmdFunc) *mcp.Server {
	t.Helper()
	server := mcp.NewServer(&mcp.Implementation{Name: "argument-test", Version: "test"}, nil)
	registerAddTool(server, execCmd)
	registerUpdateTool(server, execCmd)
	registerFixTool(server, execCmd)
	require.NoError(t, registerCompileTool(server, execCmd, "/trusted/manifest.json", true))
	registerMCPInspectTool(server, execCmd)
	require.NoError(t, registerLogsTool(server, execCmd, "", false))
	require.NoError(t, registerAuditTool(server, execCmd, "", false))
	require.NoError(t, registerAuditDiffTool(server, execCmd, "", false))
	return server
}

func TestMCPToolsRejectOptionPositionals(t *testing.T) {
	t.Parallel()
	for _, tool := range []struct {
		name  string
		field string
		base  map[string]any
		array bool
	}{
		{"add", "workflows", nil, true},
		{"update", "workflows", nil, true},
		{"fix", "workflows", nil, true},
		{"compile", "workflows", nil, true},
		{"mcp-inspect", "workflow_file", nil, false},
		{"audit", "run_ids_or_urls", nil, true},
		{"audit", "run_id", nil, false},
		{"audit", "run_id_or_url", nil, false},
		{"audit-diff", "base_run_id", map[string]any{"compare_run_ids": []string{"456"}}, false},
		{"audit-diff", "compare_run_ids", map[string]any{"base_run_id": "123"}, true},
	} {
		t.Run(tool.name+"/"+tool.field, func(t *testing.T) {
			t.Parallel()
			for _, payload := range []string{"--dir=../..", "--force", "--disable-security-scanner", "--approve", "--prior-manifest-file=/tmp/other", "-d", "--"} {
				t.Run(payload, func(t *testing.T) {
					executed := false
					execCmd := func(ctx context.Context, args ...string) *exec.Cmd {
						executed = true
						return mockCommandWithOutput(`[]`, "")(ctx, args...)
					}
					session := connectInMemory(t, registerArgumentTestTools(t, execCmd))
					args := make(map[string]any)
					maps.Copy(args, tool.base)
					if tool.array {
						args[tool.field] = []string{"valid", payload}
					} else {
						args[tool.field] = payload
					}
					result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: tool.name, Arguments: args})
					if err != nil {
						require.ErrorContains(t, err, "must not contain values beginning with '-'")
					} else {
						require.NotNil(t, result)
						require.True(t, result.IsError)
						assert.Contains(t, extractTextResult(t, result), "must not contain values beginning with '-'")
					}
					assert.False(t, executed, "invalid parameters must never spawn a command")
				})
			}
		})
	}
}

func TestMCPToolsSeparatePositionalsAndPreserveOptions(t *testing.T) {
	t.Setenv("GITHUB_REPOSITORY", "owner/repo")
	for _, tool := range []struct {
		name        string
		args        map[string]any
		positionals []string
		options     []string
	}{
		{"add", map[string]any{"workflows": []string{"owner/repo/workflow"}, "name": "renamed"}, []string{"owner/repo/workflow"}, []string{"-n", "renamed"}},
		{"update", map[string]any{"workflows": []string{"one", "two"}, "major": true, "force": true}, []string{"one", "two"}, []string{"--major", "--force"}},
		{"fix", map[string]any{"workflows": []string{"one"}, "write": true, "list_codemods": true}, []string{"one"}, []string{"--write", "--list-codemods"}},
		{"compile", map[string]any{"workflows": []string{"one.md"}, "strict": true, "fix": true, "environment": "test"}, []string{"one.md"}, []string{"--strict", "--fix", "--environment", "test", "--prior-manifest-file", "/trusted/manifest.json"}},
		{"mcp-inspect", map[string]any{"workflow_file": "one.md"}, []string{"one.md"}, []string{"list", "--verbose"}},
		{"audit", map[string]any{"run_ids_or_urls": []string{"123", "https://github.com/owner/repo/actions/runs/456"}, "experiment": "test", "variant": "a"}, []string{"123", "https://github.com/owner/repo/actions/runs/456"}, []string{"--json", "--repo", "owner/repo", "--experiment", "test", "--variant", "a", "--timeout"}},
		{"audit-diff", map[string]any{"base_run_id": "123", "compare_run_ids": []string{"456", "789"}, "artifacts": []string{"info"}}, []string{"123", "456", "789"}, []string{"--json", "--repo", "owner/repo", "--artifacts", "info"}},
		{"update", map[string]any{}, []string{}, nil},
		{"fix", map[string]any{"list_codemods": true}, []string{}, []string{"--list-codemods"}},
		{"compile", map[string]any{}, []string{}, []string{"--prior-manifest-file", "/trusted/manifest.json"}},
	} {
		t.Run(tool.name+"/"+strings.Join(tool.positionals, ","), func(t *testing.T) {
			var captured []string
			execCmd := func(ctx context.Context, args ...string) *exec.Cmd {
				captured = slices.Clone(args)
				return mockCommandWithOutput(`[]`, "")(ctx, args...)
			}
			session := connectInMemory(t, registerArgumentTestTools(t, execCmd))
			result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: tool.name, Arguments: tool.args})
			require.NoError(t, err)
			require.False(t, result.IsError)
			boundary := slices.Index(captured, "--")
			require.NotEqual(t, -1, boundary, "missing option boundary: %v", captured)
			assert.Equal(t, tool.positionals, captured[boundary+1:])
			for _, option := range tool.options {
				assert.Contains(t, captured[:boundary], option)
			}
			if tool.name == "mcp-inspect" {
				assert.Equal(t, []string{"mcp", "list", "--verbose", "--", "one.md"}, captured)
			}
			if tool.name == "add" {
				command := NewAddCommand(validateEngineStub)
				require.NoError(t, command.ParseFlags(captured[1:]))
				assert.Equal(t, tool.positionals, command.Flags().Args())
				force, err := command.Flags().GetBool("force")
				require.NoError(t, err)
				assert.False(t, force)
				name, err := command.Flags().GetString("name")
				require.NoError(t, err)
				assert.Equal(t, "renamed", name)
			}
		})
	}
	args, _, _, pathErr := buildLogsCommandArgs(context.Background(), logsArgs{WorkflowName: "one", Count: 3, Branch: "main"})
	require.NoError(t, pathErr)
	boundary := slices.Index(args, "--")
	require.NotEqual(t, -1, boundary)
	assert.Equal(t, []string{"one"}, args[boundary+1:])
	command := NewLogsCommand()
	require.NoError(t, command.ParseFlags(args[1:]))
	assert.Equal(t, []string{"one"}, command.Flags().Args())
	count, err := command.Flags().GetInt("count")
	require.NoError(t, err)
	assert.Equal(t, 3, count)
}

func TestMCPPositionalBoundaryPreventsFlagParsing(t *testing.T) {
	t.Parallel()
	payload := []string{"--dir", "/tmp/escape", "--force", "--disable-security-scanner", "https://example.com/payload.md"}
	for _, protected := range []bool{false, true} {
		flags := pflag.NewFlagSet("add", pflag.ContinueOnError)
		flags.String("dir", "", "")
		flags.Bool("force", false, "")
		flags.Bool("disable-security-scanner", false, "")
		args := payload
		if protected {
			args = appendMCPPositionals(nil, payload...)
		}
		require.NoError(t, flags.Parse(args))
		force, err := flags.GetBool("force")
		require.NoError(t, err)
		assert.Equal(t, !protected, force)
		scannerOff, err := flags.GetBool("disable-security-scanner")
		require.NoError(t, err)
		assert.Equal(t, !protected, scannerOff)
		dir, err := flags.GetString("dir")
		require.NoError(t, err)
		if protected {
			assert.Empty(t, dir)
			assert.Equal(t, payload, flags.Args())
		} else {
			assert.Equal(t, "/tmp/escape", dir)
		}
	}
}
