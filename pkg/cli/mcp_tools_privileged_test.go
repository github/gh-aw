//go:build !integration

package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// connectInMemoryWithProgress creates an in-memory MCP client-server connection
// that captures progress notifications. Returns the client session and a
// function to retrieve captured notifications. The returned getNotifications
// function returns a snapshot copy of all captured notifications and is safe
// to call concurrently with ongoing notification capture.
func connectInMemoryWithProgress(t *testing.T, server *mcp.Server) (*mcp.ClientSession, func() []*mcp.ProgressNotificationParams) {
	t.Helper()
	ctx := context.Background()
	t1, t2 := mcp.NewInMemoryTransports()
	_, err := server.Connect(ctx, t1, nil)
	require.NoError(t, err, "server.Connect should succeed")

	var mu sync.Mutex
	var captured []*mcp.ProgressNotificationParams

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "1.0"}, &mcp.ClientOptions{
		ProgressNotificationHandler: func(_ context.Context, req *mcp.ProgressNotificationClientRequest) {
			mu.Lock()
			defer mu.Unlock()
			p := *req.Params
			captured = append(captured, &p)
		},
	})
	session, err := client.Connect(ctx, t2, nil)
	require.NoError(t, err, "client.Connect should succeed")
	t.Cleanup(func() { session.Close() })

	getNotifications := func() []*mcp.ProgressNotificationParams {
		mu.Lock()
		defer mu.Unlock()
		result := make([]*mcp.ProgressNotificationParams, len(captured))
		copy(result, captured)
		return result
	}
	return session, getNotifications
}

// TestExtractLastConsoleMessage verifies that extractLastConsoleMessage correctly
// filters debug log lines and returns only user-facing console messages.
func TestExtractLastConsoleMessage(t *testing.T) {
	tests := []struct {
		name     string
		stderr   string
		expected string
	}{
		{
			name: "filters debug logs and returns error message",
			stderr: `workflow:script_registry Creating new script registry +151ns
workflow:domains Loading ecosystem domains from embedded JSON +760µs
workflow:domains Loaded 31 ecosystem categories +161µs
cli:audit Starting audit for workflow run: runID=99999999999 +916µs
cli:audit Using output directory: /tmp/gh-aw/aw-mcp/logs/run-99999999999 +14µs
✗ failed to fetch run metadata: workflow run 99999999999 not found. Please verify the run ID is correct`,
			expected: "✗ failed to fetch run metadata: workflow run 99999999999 not found. Please verify the run ID is correct",
		},
		{
			name:     "empty stderr returns empty string",
			stderr:   "",
			expected: "",
		},
		{
			name:     "only whitespace returns empty string",
			stderr:   "   \n\n  ",
			expected: "",
		},
		{
			name:     "only debug logs falls back to last non-empty line",
			stderr:   "workflow:foo Starting +100ns\ncli:bar Processing +200µs",
			expected: "cli:bar Processing +200µs",
		},
		{
			name:     "console error message with no debug logs",
			stderr:   "✗ some error occurred",
			expected: "✗ some error occurred",
		},
		{
			name:     "console success message",
			stderr:   "✓ operation completed",
			expected: "✓ operation completed",
		},
		{
			name:     "console info message",
			stderr:   "ℹ loading configuration",
			expected: "ℹ loading configuration",
		},
		{
			name:     "console warning message",
			stderr:   "⚠ deprecated option",
			expected: "⚠ deprecated option",
		},
		{
			name: "multiple console messages returns last one",
			stderr: `ℹ starting up
✗ first error
✗ second error`,
			expected: "✗ second error",
		},
		{
			name: "debug logs after console message are skipped (last console returned)",
			stderr: `✗ some error
workflow:foo Cleanup +50ms`,
			expected: "✗ some error",
		},
		{
			name: "authentication error from logs command (GitHub Actions context)",
			stderr: `cli:logs_orchestrator Starting workflow log download: workflow=, count=100
ℹ Fetching workflow runs from GitHub Actions...
✗ GitHub CLI authentication required. Run 'gh auth login' first`,
			expected: "✗ GitHub CLI authentication required. Run 'gh auth login' first",
		},
		{
			name:     "cobra error format without console symbols",
			stderr:   "Error: GitHub CLI authentication required. Run 'gh auth login' first",
			expected: "Error: GitHub CLI authentication required. Run 'gh auth login' first",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractLastConsoleMessage(tt.stderr)
			assert.Equal(t, tt.expected, result, "should extract correct message from stderr")
		})
	}
}

// connectInMemory creates an in-memory MCP client-server connection for testing.
// The session is closed automatically when the test ends via t.Cleanup.
func connectInMemory(t *testing.T, server *mcp.Server) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	t1, t2 := mcp.NewInMemoryTransports()
	_, err := server.Connect(ctx, t1, nil)
	require.NoError(t, err, "server.Connect should succeed")
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "1.0"}, nil)
	session, err := client.Connect(ctx, t2, nil)
	require.NoError(t, err, "client.Connect should succeed")
	t.Cleanup(func() { session.Close() })
	return session
}

// TestLogsToolPassesGithubRepositoryAsRepoFlag verifies that the logs MCP tool
// appends --repo <owner/repo> to the subprocess command when GITHUB_REPOSITORY
// is set, allowing gh run list to work in environments without git installed.
func TestLogsToolPassesGithubRepositoryAsRepoFlag(t *testing.T) {
	tests := []struct {
		name             string
		githubRepository string
		wantRepoFlag     bool
	}{
		{
			name:             "passes --repo when GITHUB_REPOSITORY is set",
			githubRepository: "github/gh-aw",
			wantRepoFlag:     true,
		},
		{
			name:             "omits --repo when GITHUB_REPOSITORY is empty",
			githubRepository: "",
			wantRepoFlag:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("GITHUB_REPOSITORY", tt.githubRepository)

			var capturedArgs []string
			mockExecCmd := func(ctx context.Context, args ...string) *exec.Cmd {
				capturedArgs = append([]string(nil), args...)
				// Use a non-existent command so the subprocess fails on all platforms
				// without depending on Unix-specific commands like "false".
				// cmd.Output() will return a "executable file not found" error, which
				// the handler treats as a failure — we only care about the captured args.
				return exec.CommandContext(ctx, "nonexistent-command-for-testing-only")
			}

			server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1.0"}, nil)
			err := registerLogsTool(server, mockExecCmd, "", false)
			require.NoError(t, err, "registerLogsTool should succeed")

			session := connectInMemory(t, server)

			// Call the tool — it will fail because the mock command is not found,
			// but we only care about the captured args.
			ctx := context.Background()
			_, _ = session.CallTool(ctx, &mcp.CallToolParams{
				Name:      "logs",
				Arguments: map[string]any{},
			})

			require.NotNil(t, capturedArgs, "execCmd should have been called")

			// Locate --repo flag in captured args
			var repoValue string
			for i, arg := range capturedArgs {
				if arg == "--repo" && i+1 < len(capturedArgs) {
					repoValue = capturedArgs[i+1]
					break
				}
			}

			if tt.wantRepoFlag {
				assert.Equal(t, tt.githubRepository, repoValue,
					"--repo flag should be set to GITHUB_REPOSITORY value; args: %v", capturedArgs)
			} else {
				assert.Empty(t, repoValue,
					"--repo flag should not be present when GITHUB_REPOSITORY is empty; args: %v", capturedArgs)
			}
		})
	}
}

func TestLogsToolPassesArtifactsArgument(t *testing.T) {
	var capturedArgs []string
	mockExecCmd := func(ctx context.Context, args ...string) *exec.Cmd {
		capturedArgs = append([]string(nil), args...)
		return exec.CommandContext(ctx, "sh", "-c", `printf '%s' "$1"`, "sh", `{"file_path":"/tmp/gh-aw/aw-mcp/logs/runs.json"}`)
	}

	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1.0"}, nil)
	err := registerLogsTool(server, mockExecCmd, "", false)
	require.NoError(t, err, "registerLogsTool should succeed")

	session := connectInMemory(t, server)
	_, err = session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "logs",
		Arguments: map[string]any{
			"artifacts": []any{"agent", "firewall"},
		},
	})
	require.NoError(t, err, "logs tool should succeed")

	require.Contains(t, capturedArgs, "--artifacts", "logs tool should pass --artifacts through to the CLI")
	for i, arg := range capturedArgs {
		if arg == "--artifacts" {
			require.Less(t, i+1, len(capturedArgs), "--artifacts should have a value")
			assert.Equal(t, "agent,firewall,usage", capturedArgs[i+1], "logs tool should join artifact sets for the CLI and always add the usage set")
			return
		}
	}
	t.Fatal("expected --artifacts flag in command args")
}

func TestEffectiveMCPLogsToolArtifacts(t *testing.T) {
	tests := []struct {
		name      string
		artifacts []string
		expected  []string
	}{
		{
			name:      "omitted artifacts use info and usage defaults",
			artifacts: nil,
			expected:  []string{"info", "usage"},
		},
		{
			name:      "usage set is added so token usage stays populated",
			artifacts: []string{"info"},
			expected:  []string{"info", "usage"},
		},
		{
			name:      "explicit usage set is preserved as-is",
			artifacts: []string{"usage"},
			expected:  []string{"usage"},
		},
		{
			name:      "all set is preserved as-is",
			artifacts: []string{"all"},
			expected:  []string{"all"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, effectiveMCPLogsToolArtifacts(tt.artifacts))
		})
	}
}

// TestLogsToolDefaultsToUsageArtifact guards against the regression where the logs
// tool downloaded only the "info" artifact, leaving token_usage at 0 for every run.
func TestLogsToolDefaultsToUsageArtifact(t *testing.T) {
	var capturedArgs []string
	mockExecCmd := func(ctx context.Context, args ...string) *exec.Cmd {
		capturedArgs = append([]string(nil), args...)
		return exec.CommandContext(ctx, "sh", "-c", `printf '%s' "$1"`, "sh", `{"file_path":"/tmp/gh-aw/aw-mcp/logs/runs.json"}`)
	}

	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1.0"}, nil)
	err := registerLogsTool(server, mockExecCmd, "", false)
	require.NoError(t, err, "registerLogsTool should succeed")

	session := connectInMemory(t, server)
	_, err = session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "logs",
		Arguments: map[string]any{},
	})
	require.NoError(t, err, "logs tool should succeed")

	artifactsIndex := slices.Index(capturedArgs, "--artifacts")
	require.NotEqual(t, -1, artifactsIndex, "logs tool should pass --artifacts")
	require.Less(t, artifactsIndex+1, len(capturedArgs), "--artifacts should have a value")
	assert.Equal(t, "info,usage", capturedArgs[artifactsIndex+1],
		"logs tool should download the usage artifact by default so token_usage is populated")
}

func TestLogsToolPassesGradersArgument(t *testing.T) {
	var capturedArgs []string
	mockExecCmd := func(ctx context.Context, args ...string) *exec.Cmd {
		capturedArgs = append([]string(nil), args...)
		return exec.CommandContext(ctx, "sh", "-c", `printf '%s' "$1"`, "sh", `{"file_path":"/tmp/gh-aw/aw-mcp/logs/runs.json"}`)
	}

	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1.0"}, nil)
	err := registerLogsTool(server, mockExecCmd, "", false)
	require.NoError(t, err, "registerLogsTool should succeed")

	session := connectInMemory(t, server)
	_, err = session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "logs",
		Arguments: map[string]any{
			"graders": true,
		},
	})
	require.NoError(t, err, "logs tool should succeed")

	require.Contains(t, capturedArgs, "--graders", "logs tool should pass --graders through to the CLI")
}

func TestLogsToolUsesEffectiveCountForTimeoutScaling(t *testing.T) {
	t.Run("omitted count and no workflow name use all-workflow minimum timeout", func(t *testing.T) {
		var capturedArgs []string
		mockExecCmd := func(ctx context.Context, args ...string) *exec.Cmd {
			capturedArgs = append([]string(nil), args...)
			return exec.CommandContext(ctx, "sh", "-c", `printf '%s' "$1"`, "sh", `{"file_path":"/tmp/gh-aw/aw-mcp/logs/runs.json"}`)
		}

		server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1.0"}, nil)
		err := registerLogsTool(server, mockExecCmd, "", false)
		require.NoError(t, err, "registerLogsTool should succeed")

		session := connectInMemory(t, server)
		_, err = session.CallTool(context.Background(), &mcp.CallToolParams{
			Name:      "logs",
			Arguments: map[string]any{},
		})
		require.NoError(t, err, "logs tool should succeed")

		countIndex := slices.Index(capturedArgs, "-c")
		require.NotEqual(t, -1, countIndex, "logs tool should pass -c to keep MCP/CLI defaults aligned")
		require.Less(t, countIndex+1, len(capturedArgs), "-c should have a value")
		assert.Equal(t, strconv.Itoa(defaultMCPLogsToolCount), capturedArgs[countIndex+1])

		timeoutIndex := slices.Index(capturedArgs, "--timeout")
		require.NotEqual(t, -1, timeoutIndex, "logs tool should pass --timeout")
		require.Less(t, timeoutIndex+1, len(capturedArgs), "--timeout should have a value")
		// Without a workflow_name the timeout uses the all-workflow minimum floor.
		// The timeout schema default was intentionally removed so that args.Timeout == 0
		// (the zero value) when the caller omits it, allowing the runtime to apply the floor.
		expectedTimeout := effectiveMCPLogsToolTimeoutMinutes(0, defaultMCPLogsToolCount, "", "")
		assert.Equal(t, strconv.Itoa(expectedTimeout), capturedArgs[timeoutIndex+1])
	})
	// Note: the named-workflow timeout behaviour (count-based, no all-workflow floor) is
	// verified by TestEffectiveMCPLogsToolTimeoutMinutes in logs_timeout_test.go.
	// An integration test is not feasible here because validateMCPWorkflowName rejects
	// synthetic names that lack a corresponding .lock.yml file in the test environment.
}

// TestAuditToolPassesGithubRepositoryAsRepoFlag verifies that the audit MCP tool
// appends --repo <owner/repo> to the subprocess command when GITHUB_REPOSITORY
// is set, allowing the audit command to resolve the repository without git.
func TestAuditToolPassesGithubRepositoryAsRepoFlag(t *testing.T) {
	tests := []struct {
		name             string
		githubRepository string
		wantRepoFlag     bool
	}{
		{
			name:             "passes --repo when GITHUB_REPOSITORY is set",
			githubRepository: "github/gh-aw",
			wantRepoFlag:     true,
		},
		{
			name:             "omits --repo when GITHUB_REPOSITORY is empty",
			githubRepository: "",
			wantRepoFlag:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("GITHUB_REPOSITORY", tt.githubRepository)

			var capturedArgs []string
			mockExecCmd := func(ctx context.Context, args ...string) *exec.Cmd {
				capturedArgs = append([]string(nil), args...)
				// Use a non-existent command so the subprocess fails on all platforms
				// without depending on Unix-specific commands like "false".
				return exec.CommandContext(ctx, "nonexistent-command-for-testing-only")
			}

			server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1.0"}, nil)
			err := registerAuditTool(server, mockExecCmd, "", false)
			require.NoError(t, err, "registerAuditTool should succeed")

			session := connectInMemory(t, server)

			ctx := context.Background()
			_, _ = session.CallTool(ctx, &mcp.CallToolParams{
				Name:      "audit",
				Arguments: map[string]any{"run_id_or_url": "1234567890"},
			})

			require.NotNil(t, capturedArgs, "execCmd should have been called")

			var repoValue string
			for i, arg := range capturedArgs {
				if arg == "--repo" && i+1 < len(capturedArgs) {
					repoValue = capturedArgs[i+1]
					break
				}
			}

			if tt.wantRepoFlag {
				assert.Equal(t, tt.githubRepository, repoValue,
					"--repo flag should be set to GITHUB_REPOSITORY value; args: %v", capturedArgs)
			} else {
				assert.Empty(t, repoValue,
					"--repo flag should not be present when GITHUB_REPOSITORY is empty; args: %v", capturedArgs)
			}
		})
	}
}

// TestAuditToolErrorEnvelopeSetsIsErrorFalse verifies that audit command failures
// returned as JSON envelopes use IsError=false so callers receive graceful JSON
// rather than a fatal MCP protocol error.
func TestAuditToolErrorEnvelopeSetsIsErrorFalse(t *testing.T) {
	mockExecCmd := func(ctx context.Context, args ...string) *exec.Cmd {
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=TestAuditToolErrorEnvelopeHelperProcess")
		cmd.Env = append(os.Environ(), "GH_AW_AUDIT_HELPER_PROCESS=1")
		return cmd
	}

	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1.0"}, nil)
	err := registerAuditTool(server, mockExecCmd, "", false)
	require.NoError(t, err, "registerAuditTool should succeed")

	session := connectInMemory(t, server)

	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "audit",
		Arguments: map[string]any{"run_id_or_url": "9999999999"},
	})
	require.NoError(t, err, "audit tool should return result envelope without protocol error")
	require.NotNil(t, result, "result should not be nil")
	assert.False(t, result.IsError, "audit error envelope should set IsError=false (graceful JSON error)")
	require.NotEmpty(t, result.Content, "result should contain text content")

	textContent, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok, "expected text content in audit error response")

	var envelope map[string]any
	require.NoError(t, json.Unmarshal([]byte(textContent.Text), &envelope), "error response should be valid JSON")
	runIDsRaw, hasRunIDs := envelope["run_ids_or_urls"]
	require.True(t, hasRunIDs, "error envelope should include run_ids_or_urls field")
	runIDs, ok := runIDsRaw.([]any)
	require.True(t, ok, "run_ids_or_urls should be an array")
	require.Len(t, runIDs, 1, "run_ids_or_urls should contain the single run ID")
	assert.Equal(t, "9999999999", runIDs[0], "error envelope should include original run ID")
	errorMessage, ok := envelope["error"].(string)
	require.True(t, ok, "error envelope should include string error field")
	assert.Contains(t, errorMessage, "failed to audit workflow run", "error envelope should include contextual prefix")
	suggestions, hasSuggestions := envelope["suggestions"]
	assert.True(t, hasSuggestions, "error envelope should include suggestions")
	assert.NotEmpty(t, suggestions, "suggestions should not be empty")
}

func TestAuditToolErrorEnvelopeHelperProcess(t *testing.T) {
	if os.Getenv("GH_AW_AUDIT_HELPER_PROCESS") != "1" {
		return
	}

	_, _ = fmt.Fprintln(os.Stderr, "✗ failed to fetch run metadata")
	os.Exit(1)
}

func TestAuditTool_AcceptsDeprecatedMaxTokensParameter(t *testing.T) {
	const expectedStdout = `{"overview":{"run_id":"1234567890"}}`

	var capturedArgs []string
	mockExecCmd := func(ctx context.Context, args ...string) *exec.Cmd {
		capturedArgs = slices.Clone(args)
		return exec.CommandContext(ctx, "sh", "-c", `printf '%s' "$1"`, "sh", expectedStdout)
	}

	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1.0"}, nil)
	err := registerAuditTool(server, mockExecCmd, "", false)
	require.NoError(t, err, "registerAuditTool should succeed")

	session := connectInMemory(t, server)
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "audit",
		Arguments: map[string]any{
			"run_id_or_url": "1234567890",
			"max_tokens":    5000,
		},
	})
	require.NoError(t, err, "audit tool should accept deprecated max_tokens parameter")
	require.NotNil(t, result, "result should not be nil")

	textContent, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok, "expected text content in audit response")
	assert.JSONEq(t, expectedStdout, textContent.Text, "audit tool should return subprocess stdout")
	assert.NotContains(t, strings.Join(capturedArgs, " "), "max_tokens", "audit command args should ignore max_tokens")
}

func TestAuditTool_AcceptsRunIDAlias(t *testing.T) {
	testCases := []struct {
		name           string
		runIDValue     any
		expectedRunArg string
	}{
		{
			name:           "string_run_id",
			runIDValue:     "1234567890",
			expectedRunArg: "1234567890",
		},
		{
			name:           "numeric_run_id",
			runIDValue:     1234567890,
			expectedRunArg: "1234567890",
		},
		{
			name:           "run_url",
			runIDValue:     "https://github.com/owner/repo/actions/runs/1234567890",
			expectedRunArg: "https://github.com/owner/repo/actions/runs/1234567890",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			const expectedStdout = `{"overview":{"run_id":"1234567890"}}`

			var capturedArgs []string
			mockExecCmd := func(ctx context.Context, args ...string) *exec.Cmd {
				capturedArgs = slices.Clone(args)
				return exec.CommandContext(ctx, "sh", "-c", `printf '%s' "$1"`, "sh", expectedStdout)
			}

			server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1.0"}, nil)
			err := registerAuditTool(server, mockExecCmd, "", false)
			require.NoError(t, err, "registerAuditTool should succeed")

			session := connectInMemory(t, server)
			result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
				Name: "audit",
				Arguments: map[string]any{
					"run_id": tc.runIDValue,
				},
			})
			require.NoError(t, err, "audit tool should accept run_id alias")
			require.NotNil(t, result, "result should not be nil")

			textContent, ok := result.Content[0].(*mcp.TextContent)
			require.True(t, ok, "expected text content in audit response")
			assert.JSONEq(t, expectedStdout, textContent.Text, "audit tool should return subprocess stdout")

			require.GreaterOrEqual(t, len(capturedArgs), 2, "captured args should include command and run ID")
			assert.Equal(t, "audit", capturedArgs[0], "first arg should be audit command")
			assert.Equal(t, tc.expectedRunArg, capturedArgs[1], "run_id alias should be forwarded as positional run input")
		})
	}
}

func TestAuditTool_AcceptsNumericRunIDOrURLField(t *testing.T) {
	const expectedStdout = `{"overview":{"run_id":"1234567890"}}`

	var capturedArgs []string
	mockExecCmd := func(ctx context.Context, args ...string) *exec.Cmd {
		capturedArgs = slices.Clone(args)
		return exec.CommandContext(ctx, "sh", "-c", `printf '%s' "$1"`, "sh", expectedStdout)
	}

	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1.0"}, nil)
	err := registerAuditTool(server, mockExecCmd, "", false)
	require.NoError(t, err, "registerAuditTool should succeed")

	session := connectInMemory(t, server)
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "audit",
		Arguments: map[string]any{
			"run_id_or_url": 1234567890,
		},
	})
	require.NoError(t, err, "audit tool should accept numeric run_id_or_url")
	require.NotNil(t, result, "result should not be nil")

	textContent, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok, "expected text content in audit response")
	assert.JSONEq(t, expectedStdout, textContent.Text, "audit tool should return subprocess stdout")

	require.GreaterOrEqual(t, len(capturedArgs), 2, "captured args should include command and run ID")
	assert.Equal(t, "audit", capturedArgs[0], "first arg should be audit command")
	assert.Equal(t, "1234567890", capturedArgs[1], "numeric run_id_or_url should be normalized to positional string run ID")
}

// TestAuditTool_MultiRunDiffMode verifies that when run_ids_or_urls contains
// multiple entries the audit tool passes all of them as positional arguments
// to the audit command (which then runs in diff mode).
func TestAuditTool_MultiRunDiffMode(t *testing.T) {
	const expectedStdout = `[{"base_run_id":111,"compare_run_id":222}]`

	var capturedArgs []string
	mockExecCmd := func(ctx context.Context, args ...string) *exec.Cmd {
		capturedArgs = slices.Clone(args)
		return exec.CommandContext(ctx, "sh", "-c", `printf '%s' "$1"`, "sh", expectedStdout)
	}

	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1.0"}, nil)
	err := registerAuditTool(server, mockExecCmd, "", false)
	require.NoError(t, err, "registerAuditTool should succeed")

	session := connectInMemory(t, server)
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "audit",
		Arguments: map[string]any{
			"run_ids_or_urls": []string{"111", "222", "333"},
		},
	})
	require.NoError(t, err, "audit tool should succeed with multiple run IDs")
	require.NotNil(t, result, "result should not be nil")
	assert.False(t, result.IsError, "result should not be an error")

	textContent, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok, "expected text content in audit response")
	assert.JSONEq(t, expectedStdout, textContent.Text, "audit tool should return subprocess stdout")

	// All three run IDs must appear as positional args immediately after "audit"
	require.GreaterOrEqual(t, len(capturedArgs), 4, "captured args should include audit + 3 run IDs: %v", capturedArgs)
	assert.Equal(t, "audit", capturedArgs[0], "first arg should be 'audit'")
	assert.Equal(t, "111", capturedArgs[1], "second arg should be first run ID")
	assert.Equal(t, "222", capturedArgs[2], "third arg should be second run ID")
	assert.Equal(t, "333", capturedArgs[3], "fourth arg should be third run ID")
}

// TestAuditTool_FailsWhenNoRunIDProvided verifies that the audit tool
// returns an error when neither run_id_or_url nor run_ids_or_urls is provided.
func TestAuditTool_FailsWhenNoRunIDProvided(t *testing.T) {
	mockExecCmd := func(ctx context.Context, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "nonexistent-command-for-testing-only")
	}

	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1.0"}, nil)
	err := registerAuditTool(server, mockExecCmd, "", false)
	require.NoError(t, err, "registerAuditTool should succeed")

	session := connectInMemory(t, server)
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "audit",
		Arguments: map[string]any{},
	})
	// The MCP SDK surfaces InvalidParams as a protocol-level error
	assert.True(t, err != nil || (result != nil && result.IsError),
		"audit tool should return an error when no run ID is provided")
}

// TestAuditTool_ExperimentVariantFlags verifies that --experiment and --variant
// are forwarded as CLI flags when provided via the MCP tool arguments.
func TestAuditTool_ExperimentVariantFlags(t *testing.T) {
	const expectedStdout = `{"overview":{"run_id":"1234567890"}}`

	var capturedArgs []string
	mockExecCmd := func(ctx context.Context, args ...string) *exec.Cmd {
		capturedArgs = slices.Clone(args)
		return exec.CommandContext(ctx, "sh", "-c", `printf '%s' "$1"`, "sh", expectedStdout)
	}

	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1.0"}, nil)
	err := registerAuditTool(server, mockExecCmd, "", false)
	require.NoError(t, err, "registerAuditTool should succeed")

	session := connectInMemory(t, server)
	_, err = session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "audit",
		Arguments: map[string]any{
			"run_ids_or_urls": []string{"1234567890"},
			"experiment":      "style",
			"variant":         "concise",
		},
	})
	require.NoError(t, err, "audit tool should succeed with experiment/variant flags")

	joined := strings.Join(capturedArgs, " ")
	assert.Contains(t, joined, "--experiment style", "audit command should include --experiment flag")
	assert.Contains(t, joined, "--variant concise", "audit command should include --variant flag")
}

func TestAuditTool_VariantWithoutExperimentFails(t *testing.T) {
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1.0"}, nil)
	err := registerAuditTool(server, func(context.Context, ...string) *exec.Cmd {
		t.Fatal("audit command should not execute")
		return nil
	}, "", false)
	require.NoError(t, err, "registerAuditTool should succeed")

	session := connectInMemory(t, server)
	_, err = session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "audit",
		Arguments: map[string]any{
			"run_ids_or_urls": []string{"1234567890"},
			"variant":         "concise",
		},
	})
	require.ErrorContains(t, err, "--variant requires --experiment")
}

// TestAuditTool_ExperimentFlagWithoutVariant verifies that --experiment is forwarded
// even when --variant is not provided.
func TestAuditTool_ExperimentFlagWithoutVariant(t *testing.T) {
	const expectedStdout = `{"overview":{"run_id":"9999"}}`

	var capturedArgs []string
	mockExecCmd := func(ctx context.Context, args ...string) *exec.Cmd {
		capturedArgs = slices.Clone(args)
		return exec.CommandContext(ctx, "sh", "-c", `printf '%s' "$1"`, "sh", expectedStdout)
	}

	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1.0"}, nil)
	err := registerAuditTool(server, mockExecCmd, "", false)
	require.NoError(t, err, "registerAuditTool should succeed")

	session := connectInMemory(t, server)
	_, err = session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "audit",
		Arguments: map[string]any{
			"run_ids_or_urls": []string{"9999"},
			"experiment":      "caveman",
		},
	})
	require.NoError(t, err, "audit tool should succeed with experiment flag only")

	joined := strings.Join(capturedArgs, " ")
	assert.Contains(t, joined, "--experiment caveman", "audit command should include --experiment flag")
	assert.NotContains(t, joined, "--variant", "audit command should not include --variant when not set")
}

func TestAuditDiffToolErrorEnvelopeSetsIsErrorFalse(t *testing.T) {
	mockExecCmd := func(ctx context.Context, args ...string) *exec.Cmd {
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=TestAuditDiffToolErrorEnvelopeHelperProcess")
		cmd.Env = append(os.Environ(), "GH_AW_AUDIT_DIFF_HELPER_PROCESS=1")
		return cmd
	}

	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1.0"}, nil)
	err := registerAuditDiffTool(server, mockExecCmd, "", false)
	require.NoError(t, err, "registerAuditDiffTool should succeed")

	session := connectInMemory(t, server)

	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "audit-diff",
		Arguments: map[string]any{
			"base_run_id":     "100",
			"compare_run_ids": []string{"200"},
		},
	})
	require.NoError(t, err, "audit-diff tool should return result envelope without protocol error")
	require.NotNil(t, result, "result should not be nil")
	assert.False(t, result.IsError, "audit-diff error envelope should set IsError=false (graceful JSON error)")
	require.NotEmpty(t, result.Content, "result should contain text content")

	textContent, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok, "expected text content in audit-diff error response")

	var envelope map[string]any
	require.NoError(t, json.Unmarshal([]byte(textContent.Text), &envelope), "error response should be valid JSON")
	assert.Equal(t, "100", envelope["base_run_id"], "error envelope should include base run ID")
	errorMessage, ok := envelope["error"].(string)
	require.True(t, ok, "error envelope should include string error field")
	assert.Contains(t, errorMessage, "failed to diff workflow runs", "error envelope should include contextual prefix")
	suggestions, hasSuggestions := envelope["suggestions"]
	assert.True(t, hasSuggestions, "error envelope should include suggestions")
	assert.NotEmpty(t, suggestions, "suggestions should not be empty")
}

func TestAuditDiffToolErrorEnvelopeHelperProcess(t *testing.T) {
	if os.Getenv("GH_AW_AUDIT_DIFF_HELPER_PROCESS") != "1" {
		return
	}

	_, _ = fmt.Fprintln(os.Stderr, "✗ failed to diff workflow runs")
	os.Exit(1)
}

// TestLogsToolEmitsProgressNotifications verifies that the logs MCP tool
// sends progress notifications when a progress token is provided.
func TestLogsToolEmitsProgressNotifications(t *testing.T) {
	const fakeOutput = `{"file_path":"/tmp/gh-aw/aw-mcp/logs/runs.json"}`
	const progressDelta = 0.001

	mockExecCmd := func(ctx context.Context, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "sh", "-c", `printf '%s' "$1"`, "sh", fakeOutput)
	}

	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1.0"}, nil)
	err := registerLogsTool(server, mockExecCmd, "", false)
	require.NoError(t, err, "registerLogsTool should succeed")

	session, getNotifications := connectInMemoryWithProgress(t, server)

	params := &mcp.CallToolParams{Name: "logs", Arguments: map[string]any{}}
	params.SetProgressToken("logs-progress-token")
	_, err = session.CallTool(context.Background(), params)
	require.NoError(t, err, "logs tool should succeed")

	var notifications []*mcp.ProgressNotificationParams
	require.Eventually(t, func() bool {
		notifications = getNotifications()
		if len(notifications) < 2 {
			return false
		}
		first := notifications[0]
		last := notifications[len(notifications)-1]
		return first.Progress >= -progressDelta && first.Progress <= progressDelta &&
			last.Progress >= 100-progressDelta && last.Progress <= 100+progressDelta
	}, time.Second, 10*time.Millisecond, "logs tool should emit start and completion progress notifications")

	first := notifications[0]
	assert.InDelta(t, float64(0), first.Progress, progressDelta, "first notification should have progress=0")
	assert.InDelta(t, float64(100), first.Total, 0.001, "first notification should have total=100")
	assert.NotEmpty(t, first.Message, "first notification should have a message")

	last := notifications[len(notifications)-1]
	assert.InDelta(t, float64(100), last.Progress, progressDelta, "last notification should have progress=100")
	assert.InDelta(t, float64(100), last.Total, 0.001, "last notification should have total=100")
	assert.NotEmpty(t, last.Message, "last notification should have a message")
}

// TestLogsToolNoProgressWithoutToken verifies that the logs MCP tool
// does not send progress notifications when no progress token is provided.
func TestLogsToolNoProgressWithoutToken(t *testing.T) {
	const fakeOutput = `{"file_path":"/tmp/gh-aw/aw-mcp/logs/runs.json"}`

	mockExecCmd := func(ctx context.Context, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "sh", "-c", `printf '%s' "$1"`, "sh", fakeOutput)
	}

	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1.0"}, nil)
	err := registerLogsTool(server, mockExecCmd, "", false)
	require.NoError(t, err, "registerLogsTool should succeed")

	session, getNotifications := connectInMemoryWithProgress(t, server)

	// Call without setting a progress token
	_, err = session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "logs",
		Arguments: map[string]any{},
	})
	require.NoError(t, err, "logs tool should succeed")

	assert.Empty(t, getNotifications(), "logs tool should not emit progress notifications without a token")
}

// TestAuditToolEmitsProgressNotifications verifies that the audit MCP tool
// sends progress notifications when a progress token is provided.
func TestAuditToolEmitsProgressNotifications(t *testing.T) {
	const fakeOutput = `{"overview":{"run_id":"1234567890"}}`
	const progressDelta = 0.001

	mockExecCmd := func(ctx context.Context, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "sh", "-c", `printf '%s' "$1"`, "sh", fakeOutput)
	}

	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1.0"}, nil)
	err := registerAuditTool(server, mockExecCmd, "", false)
	require.NoError(t, err, "registerAuditTool should succeed")

	session, getNotifications := connectInMemoryWithProgress(t, server)

	params := &mcp.CallToolParams{
		Name:      "audit",
		Arguments: map[string]any{"run_id_or_url": "1234567890"},
	}
	params.SetProgressToken("audit-progress-token")
	_, err = session.CallTool(context.Background(), params)
	require.NoError(t, err, "audit tool should succeed")

	var notifications []*mcp.ProgressNotificationParams
	require.Eventually(t, func() bool {
		notifications = getNotifications()
		if len(notifications) < 2 {
			return false
		}
		first := notifications[0]
		last := notifications[len(notifications)-1]
		return first.Progress >= -progressDelta && first.Progress <= progressDelta &&
			last.Progress >= 100-progressDelta && last.Progress <= 100+progressDelta
	}, time.Second, 10*time.Millisecond, "audit tool should emit start and completion progress notifications")

	first := notifications[0]
	assert.InDelta(t, float64(0), first.Progress, progressDelta, "first notification should have progress=0")
	assert.InDelta(t, float64(100), first.Total, 0.001, "first notification should have total=100")
	assert.NotEmpty(t, first.Message, "first notification should have a message")

	last := notifications[len(notifications)-1]
	assert.InDelta(t, float64(100), last.Progress, progressDelta, "last notification should have progress=100")
	assert.InDelta(t, float64(100), last.Total, 0.001, "last notification should have total=100")
	assert.NotEmpty(t, last.Message, "last notification should have a message")
}

// TestAuditDiffToolEmitsProgressNotifications verifies that the audit-diff MCP
// tool sends progress notifications when a progress token is provided.
func TestAuditDiffToolEmitsProgressNotifications(t *testing.T) {
	const fakeOutput = `[{"base_run_id":100,"compare_run_id":200}]`
	const progressDelta = 0.001

	mockExecCmd := func(ctx context.Context, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "sh", "-c", `printf '%s' "$1"`, "sh", fakeOutput)
	}

	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1.0"}, nil)
	err := registerAuditDiffTool(server, mockExecCmd, "", false)
	require.NoError(t, err, "registerAuditDiffTool should succeed")

	session, getNotifications := connectInMemoryWithProgress(t, server)

	params := &mcp.CallToolParams{
		Name: "audit-diff",
		Arguments: map[string]any{
			"base_run_id":     "100",
			"compare_run_ids": []string{"200"},
		},
	}
	params.SetProgressToken("audit-diff-progress-token")
	_, err = session.CallTool(context.Background(), params)
	require.NoError(t, err, "audit-diff tool should succeed")

	var notifications []*mcp.ProgressNotificationParams
	require.Eventually(t, func() bool {
		notifications = getNotifications()
		if len(notifications) < 2 {
			return false
		}
		first := notifications[0]
		last := notifications[len(notifications)-1]
		return first.Progress >= -progressDelta && first.Progress <= progressDelta &&
			last.Progress >= 100-progressDelta && last.Progress <= 100+progressDelta
	}, time.Second, 10*time.Millisecond, "audit-diff tool should emit start and completion progress notifications")

	first := notifications[0]
	assert.InDelta(t, float64(0), first.Progress, progressDelta, "first notification should have progress=0")
	assert.InDelta(t, float64(100), first.Total, 0.001, "first notification should have total=100")
	assert.NotEmpty(t, first.Message, "first notification should have a message")

	last := notifications[len(notifications)-1]
	assert.InDelta(t, float64(100), last.Progress, progressDelta, "last notification should have progress=100")
	assert.InDelta(t, float64(100), last.Total, 0.001, "last notification should have total=100")
	assert.NotEmpty(t, last.Message, "last notification should have a message")
}

// TestAuditToolNoProgressWithoutToken verifies that the audit MCP tool
// does not send progress notifications when no progress token is provided.
func TestAuditToolNoProgressWithoutToken(t *testing.T) {
	const fakeOutput = `{"overview":{"run_id":"1234567890"}}`

	mockExecCmd := func(ctx context.Context, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "sh", "-c", `printf '%s' "$1"`, "sh", fakeOutput)
	}

	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1.0"}, nil)
	err := registerAuditTool(server, mockExecCmd, "", false)
	require.NoError(t, err, "registerAuditTool should succeed")

	session, getNotifications := connectInMemoryWithProgress(t, server)

	// Call without setting a progress token
	_, err = session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "audit",
		Arguments: map[string]any{"run_id_or_url": "1234567890"},
	})
	require.NoError(t, err, "audit tool should succeed")

	assert.Empty(t, getNotifications(), "audit tool should not emit progress notifications without a token")
}

// TestAuditDiffToolNoProgressWithoutToken verifies that the audit-diff MCP tool
// does not send progress notifications when no progress token is provided.
func TestAuditDiffToolNoProgressWithoutToken(t *testing.T) {
	const fakeOutput = `[{"base_run_id":100,"compare_run_id":200}]`

	mockExecCmd := func(ctx context.Context, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "sh", "-c", `printf '%s' "$1"`, "sh", fakeOutput)
	}

	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1.0"}, nil)
	err := registerAuditDiffTool(server, mockExecCmd, "", false)
	require.NoError(t, err, "registerAuditDiffTool should succeed")

	session, getNotifications := connectInMemoryWithProgress(t, server)

	// Call without setting a progress token
	_, err = session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "audit-diff",
		Arguments: map[string]any{
			"base_run_id":     "100",
			"compare_run_ids": []string{"200"},
		},
	})
	require.NoError(t, err, "audit-diff tool should succeed")

	assert.Empty(t, getNotifications(), "audit-diff tool should not emit progress notifications without a token")
}

// TestLogsToolSubprocessContextIgnoresGatewayDeadline verifies that the logs
// tool creates a subprocess context rooted at context.Background() so the
// subprocess deadline is independent of the MCP gateway's per-request deadline.
// This is the regression test for the original bug where a 60 s gateway deadline
// silently overrode the caller-requested --timeout value.
func TestLogsToolSubprocessContextIgnoresGatewayDeadline(t *testing.T) {
	// Track the deadline of the subprocess context captured when mockExecCmd is called.
	var capturedDeadline time.Time
	var capturedHasDeadline bool

	mockExecCmd := func(ctx context.Context, args ...string) *exec.Cmd {
		capturedDeadline, capturedHasDeadline = ctx.Deadline()
		// Return a command that succeeds immediately with minimal JSON.
		return exec.CommandContext(ctx, "sh", "-c", `printf '%s' "$1"`, "sh", `{"file_path":"/tmp/gh-aw/aw-mcp/logs/runs.json"}`)
	}

	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1.0"}, nil)
	err := registerLogsTool(server, mockExecCmd, "", false)
	require.NoError(t, err, "registerLogsTool should succeed")

	session := connectInMemory(t, server)

	// Call the logs tool with an explicit 5-minute subprocess timeout, but use a
	// gateway context that expires in 2 seconds — simulating the MCP gateway's
	// hardcoded per-tool RPC deadline.  The subprocess context deadline must still
	// be ~5 minutes out, proving it is rooted at context.Background() rather than
	// the (short-lived) gateway context.
	const requestedTimeoutMinutes = 5
	before := time.Now()

	gatewayCtx, gatewayCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer gatewayCancel()

	// The call may return an error because the gateway context expires before the
	// tool completes (the mock command exits quickly, but the error path is
	// acceptable here — we only care about the subprocess context deadline).
	_, _ = session.CallTool(gatewayCtx, &mcp.CallToolParams{
		Name: "logs",
		Arguments: map[string]any{
			"timeout": requestedTimeoutMinutes,
		},
	})

	// The subprocess context must have a deadline.
	require.True(t, capturedHasDeadline, "subprocess context should have a deadline")

	// The subprocess deadline must be ~requestedTimeoutMinutes in the future,
	// not bounded by the 2-second gateway context.  This assertion catches the
	// regression where someone re-introduces the bug by rooting subCtx at ctx
	// instead of context.Background().
	expectedMinDeadline := before.Add(time.Duration(requestedTimeoutMinutes)*time.Minute - 5*time.Second)
	assert.True(t, capturedDeadline.After(expectedMinDeadline),
		"subprocess context deadline (%v) should be ≥ %d minutes from call start (%v) regardless of the 2s gateway deadline; got %v from start",
		capturedDeadline, requestedTimeoutMinutes, before, capturedDeadline.Sub(before))
}

// TestAuditToolSubprocessContextIgnoresGatewayDeadline verifies that the audit
// tool creates a subprocess context rooted at context.Background() so the
// subprocess deadline is independent of the MCP gateway's 60 s per-request
// deadline. This is the regression test for the bug where a 60 s gateway
// deadline caused context deadline exceeded on every audit call.
//
// The test calls newMCPSubprocessContext directly with a deadline-bearing
// context so it is not affected by the MCP in-memory transport's context
// isolation, which strips deadlines from server handler contexts.
func TestAuditToolSubprocessContextIgnoresGatewayDeadline(t *testing.T) {
	// Simulate the MCP gateway's short per-tool RPC deadline (2 s) by passing a
	// deadline-bearing context directly to the detachment helper — the same
	// context the handler receives in production.
	gatewayCtx, gatewayCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer gatewayCancel()

	before := time.Now()

	subCtx, subCancel := newMCPSubprocessContext(gatewayCtx, time.Duration(defaultMCPAuditTimeoutMinutes)*time.Minute, "audit")
	defer subCancel()

	deadline, ok := subCtx.Deadline()
	require.True(t, ok, "subprocess context should have a deadline")

	// The subprocess deadline must be at least defaultMCPAuditTimeoutMinutes out,
	// not bounded by the 2-second gateway context.  This assertion would fail if
	// newMCPSubprocessContext were changed to context.WithTimeout(ctx, ...) without
	// the context.WithoutCancel detachment step.
	expectedMinDeadline := before.Add(time.Duration(defaultMCPAuditTimeoutMinutes)*time.Minute - 5*time.Second)
	assert.True(t, deadline.After(expectedMinDeadline),
		"subprocess context deadline (%v) should be ≥ %d minutes from call start (%v) regardless of the 2s gateway deadline; got %v from start",
		deadline, defaultMCPAuditTimeoutMinutes, before, deadline.Sub(before))
}

// TestAuditDiffToolSubprocessContextIgnoresGatewayDeadline verifies that the
// audit-diff tool creates a subprocess context rooted at context.Background()
// so the subprocess deadline is independent of the MCP gateway's 60 s
// per-request deadline.
//
// The test calls newMCPSubprocessContext directly with a deadline-bearing
// context so it is not affected by the MCP in-memory transport's context
// isolation, which strips deadlines from server handler contexts.
func TestAuditDiffToolSubprocessContextIgnoresGatewayDeadline(t *testing.T) {
	// Simulate the MCP gateway's short per-tool RPC deadline (2 s).
	gatewayCtx, gatewayCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer gatewayCancel()

	before := time.Now()

	subCtx, subCancel := newMCPSubprocessContext(gatewayCtx, time.Duration(defaultMCPAuditDiffTimeoutMinutes)*time.Minute, "audit-diff")
	defer subCancel()

	deadline, ok := subCtx.Deadline()
	require.True(t, ok, "subprocess context should have a deadline")

	// The subprocess deadline must be at least defaultMCPAuditDiffTimeoutMinutes out.
	expectedMinDeadline := before.Add(time.Duration(defaultMCPAuditDiffTimeoutMinutes)*time.Minute - 5*time.Second)
	assert.True(t, deadline.After(expectedMinDeadline),
		"subprocess context deadline (%v) should be ≥ %d minutes from call start (%v) regardless of the 2s gateway deadline; got %v from start",
		deadline, defaultMCPAuditDiffTimeoutMinutes, before, deadline.Sub(before))
}
