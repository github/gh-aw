//go:build !integration

package cli

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMCPServerUnit_ListTools verifies that the MCP server exposes exactly the
// expected set of tools without spawning a subprocess.
func TestMCPServerUnit_ListTools(t *testing.T) {
	t.Parallel()
	server := createMCPServer("", "", false, false, "", nil)
	session := connectInMemory(t, server)

	ctx := context.Background()
	result, err := session.ListTools(ctx, &mcp.ListToolsParams{})
	require.NoError(t, err, "ListTools should succeed")

	// expectedTools must match the tools registered in createMCPServer.
	// Keep this list in sync with mcp_server_tools_test.go (integration tests).
	expectedTools := []string{"status", "health", "compile", "logs", "audit", "audit-diff", "checks", "mcp-inspect", "work-queue", "add", "update", "fix"}
	toolNames := make(map[string]bool)
	for _, tool := range result.Tools {
		toolNames[tool.Name] = true
	}

	for _, name := range expectedTools {
		assert.True(t, toolNames[name], "expected tool %q to be registered", name)
	}
	assert.Len(t, result.Tools, len(expectedTools), "server should expose exactly %d tools", len(expectedTools))
}

func TestMCPServerUnit_AuditArtifactDescriptions(t *testing.T) {
	t.Parallel()
	server := createMCPServer("", "", false, false, "", nil)
	session := connectInMemory(t, server)

	result, err := session.ListTools(context.Background(), &mcp.ListToolsParams{})
	require.NoError(t, err)
	expected := "Artifact sets to download (default: all). Valid sets: " + strings.Join(ValidArtifactSetNames(), ", ")
	for _, name := range []string{"audit", "audit-diff"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var tool *mcp.Tool
			for _, candidate := range result.Tools {
				if candidate.Name == name {
					tool = candidate
					break
				}
			}
			require.NotNil(t, tool)
			encoded, err := json.Marshal(tool.InputSchema)
			require.NoError(t, err)
			var schema struct {
				Properties map[string]struct {
					Description string `json:"description"`
				} `json:"properties"`
			}
			require.NoError(t, json.Unmarshal(encoded, &schema))
			artifacts, ok := schema.Properties["artifacts"]
			require.True(t, ok)
			assert.Equal(t, expected, artifacts.Description)
		})
	}
}

// TestMCPServerUnit_ServerCapabilities verifies that the server advertises the
// Tools capability with ListChanged=false (tools are static, no notifications needed).
func TestMCPServerUnit_ServerCapabilities(t *testing.T) {
	t.Parallel()
	server := createMCPServer("", "", false, false, "", nil)
	session := connectInMemory(t, server)

	initResult := session.InitializeResult()
	require.NotNil(t, initResult, "InitializeResult should not be nil")
	require.NotNil(t, initResult.Capabilities.Tools, "server should advertise Tools capability")
	assert.False(t, initResult.Capabilities.Tools.ListChanged, "Tools.ListChanged should be false (tools are static)")
}

func TestMCPServerUnit_ProtocolVersions(t *testing.T) {
	t.Parallel()
	for _, version := range mcp.SupportedProtocolVersions() {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			server := createMCPServer("", "", false, false, "", nil)
			clientTransport, serverTransport := mcp.NewInMemoryTransports()
			serverSession, err := server.Connect(context.Background(), serverTransport, nil)
			require.NoError(t, err)
			t.Cleanup(func() { _ = serverSession.Close() })
			client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "1.0"}, nil)
			session, err := client.Connect(context.Background(), clientTransport, &mcp.ClientSessionOptions{ProtocolVersion: version})
			require.NoError(t, err)
			t.Cleanup(func() { _ = session.Close() })
			assert.Equal(t, version, session.InitializeResult().ProtocolVersion)
		})
	}
}

func TestMCPServerUnit_ToolListCache(t *testing.T) {
	t.Parallel()
	server := createMCPServer("", "", false, false, "", nil)
	session := connectInMemory(t, server)
	result, err := session.ListTools(context.Background(), &mcp.ListToolsParams{})
	require.NoError(t, err)
	assert.Equal(t, mcpDiscoveryCacheTTLMs, result.TTLMs)
	assert.Equal(t, "private", result.CacheScope)
}

func TestMCPServerUnit_DiscoveryCache(t *testing.T) {
	t.Parallel()
	server := createMCPServer("", "", false, false, "", nil)
	var discovered *mcp.DiscoverResult
	server.AddReceivingMiddleware(func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			result, err := next(ctx, method, req)
			if method == "server/discover" && err == nil {
				discovered = result.(*mcp.DiscoverResult)
			}
			return result, err
		}
	})
	session := connectInMemory(t, server)
	require.NotNil(t, session.InitializeResult())
	require.NotNil(t, discovered)
	assert.Equal(t, mcpDiscoveryCacheTTLMs, discovered.TTLMs)
	assert.Equal(t, "private", discovered.CacheScope)
}

// TestMCPServerUnit_StatusTool verifies that the status tool can be called
// in-process and returns valid JSON output without spawning a subprocess.
func TestMCPServerUnit_StatusTool(t *testing.T) {
	// Create a temporary directory with an empty .github/workflows dir so
	// GetWorkflowStatuses returns an empty array rather than an error.
	tmpDir := t.TempDir()
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	require.NoError(t, os.MkdirAll(workflowsDir, 0o755), "should create workflows dir")

	origDir, err := os.Getwd()
	require.NoError(t, err, "should get current dir")
	require.NoError(t, os.Chdir(tmpDir), "should change to temp dir")
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	server := createMCPServer("", "", false, false, "", nil)
	session := connectInMemory(t, server)

	ctx := context.Background()
	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "status",
		Arguments: map[string]any{},
	})
	require.NoError(t, err, "status tool should not return a protocol error")
	require.NotNil(t, result, "status tool should return a result")
	assert.False(t, result.IsError, "status tool should not return an error envelope")
	require.NotEmpty(t, result.Content, "status tool should return content")

	textContent, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok, "status tool should return text content")

	// With no workflow files in the directory, the response should be a valid empty JSON array
	// (not null — the contract is always an array).
	var statuses []any
	require.NoError(t, json.Unmarshal([]byte(textContent.Text), &statuses), "status tool should return valid JSON")
	require.NotNil(t, statuses, "status tool should return a JSON array, not null")
	assert.Empty(t, statuses, "status should be empty when no workflow files exist")
}

// TestMCPServerUnit_CompileTool verifies that the compile tool can be called
// in-process using a mock execCmd so no compiled binary is required.
func TestMCPServerUnit_CompileTool(t *testing.T) {
	t.Parallel()
	const fakeOutput = `[{"workflow":"test.md","valid":true,"errors":[],"warnings":[]}]`

	var capturedArgs []string
	mockExecCmd := func(ctx context.Context, args ...string) *exec.Cmd {
		capturedArgs = append([]string(nil), args...)
		return exec.CommandContext(ctx, "sh", "-c", `printf '%s' "$1"`, "sh", fakeOutput)
	}

	server := mcp.NewServer(&mcp.Implementation{Name: "gh-aw", Version: "test"}, nil)
	require.NoError(t, registerCompileTool(server, mockExecCmd, "", true), "registerCompileTool should succeed")
	session := connectInMemory(t, server)

	ctx := context.Background()
	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "compile",
		Arguments: map[string]any{},
	})
	require.NoError(t, err, "compile tool should not return a protocol error")
	require.NotNil(t, result, "compile tool should return a result")
	assert.False(t, result.IsError, "compile tool should not return an error envelope")
	require.NotEmpty(t, result.Content, "compile tool should return content")

	textContent, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok, "compile tool should return text content")
	assert.JSONEq(t, fakeOutput, textContent.Text, "compile tool should return the subprocess stdout")

	// Verify the compile subcommand was invoked with the expected flags.
	require.NotEmpty(t, capturedArgs, "execCmd should have been called")
	assert.Equal(t, "compile", capturedArgs[0], "first arg should be 'compile'")
	assert.Contains(t, strings.Join(capturedArgs, " "), "--json", "compile should pass --json flag")
}

func TestMCPServerUnit_CompileToolProgress(t *testing.T) {
	t.Parallel()
	mockExecCmd := func(ctx context.Context, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "sh", "-c", `printf '%s' '[]'`)
	}
	server := mcp.NewServer(&mcp.Implementation{Name: "test"}, nil)
	require.NoError(t, registerCompileTool(server, mockExecCmd, "", true))
	session, getNotifications := connectInMemoryWithProgress(t, server)
	params := &mcp.CallToolParams{Name: "compile", Arguments: map[string]any{}}
	params.SetProgressToken("compile-progress")
	result, err := session.CallTool(context.Background(), params)
	require.NoError(t, err)
	require.False(t, result.IsError)
	require.Eventually(t, func() bool { return len(getNotifications()) == 2 }, time.Second, 10*time.Millisecond)
	notifications := getNotifications()
	assert.InDelta(t, 0, notifications[0].Progress, 0.001)
	assert.InDelta(t, 100, notifications[1].Progress, 0.001)
	assert.Equal(t, "compile-progress", notifications[0].ProgressToken)
}

func TestMCPServerUnit_CompileToolProgressOnEmptyOutputError(t *testing.T) {
	t.Parallel()
	mockExecCmd := func(ctx context.Context, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "sh", "-c", "exit 1")
	}
	server := mcp.NewServer(&mcp.Implementation{Name: "test"}, nil)
	require.NoError(t, registerCompileTool(server, mockExecCmd, "", true))
	session, getNotifications := connectInMemoryWithProgress(t, server)
	params := &mcp.CallToolParams{Name: "compile", Arguments: map[string]any{}}
	params.SetProgressToken("compile-progress")

	result, err := session.CallTool(context.Background(), params)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.False(t, result.IsError)
	require.NotEmpty(t, result.Content)
	require.Eventually(t, func() bool { return len(getNotifications()) == 2 }, time.Second, 10*time.Millisecond)
	notifications := getNotifications()
	assert.InDelta(t, 0, notifications[0].Progress, 0.001)
	assert.InDelta(t, 100, notifications[1].Progress, 0.001)
	assert.Equal(t, "compile-progress", notifications[0].ProgressToken)
}
