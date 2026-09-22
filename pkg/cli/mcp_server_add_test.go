//go:build integration

package cli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testMCPServerListToolsTimeout = 10 * time.Second
	testMCPServerAddToolTimeout   = 60 * time.Second
	// Pinned commit SHA keeps this integration test deterministic across future upstream changes.
	testAddToolWorkflowSource = "githubnext/agentics/workflows/daily-team-status.md@d3422bf940923ef1d43db5559652b8e1e71869f3"
)

func setupMCPServerSession(t *testing.T, workingDir string, timeout time.Duration) (context.Context, *mcp.ClientSession) {
	t.Helper()

	binaryPath := testutil.RequireGhAwBinary(t)

	client := mcp.NewClient(&mcp.Implementation{
		Name:    "test-client",
		Version: "1.0.0",
	}, nil)

	serverCmd := exec.Command(binaryPath, "mcp-server", "--cmd", binaryPath)
	if workingDir != "" {
		serverCmd.Dir = workingDir
	}
	transport := &mcp.CommandTransport{Command: serverCmd}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	session, err := client.Connect(ctx, transport, nil)
	require.NoError(t, err, "Expected MCP client to connect to subprocess server")
	t.Cleanup(func() {
		cancel()
		session.Close()
	})

	return ctx, session
}

func setupMCPServerAddRepo(t *testing.T) (string, string) {
	t.Helper()

	tmpDir := testutil.TempDir(t, "test-*")
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	require.NoError(t, os.MkdirAll(workflowsDir, 0755), "Expected workflows directory to be created")
	require.NoError(t, initTestGitRepo(tmpDir), "Expected git repository scaffolding to be initialized")

	return tmpDir, workflowsDir
}

func extractTextContent(result *mcp.CallToolResult) string {
	var outputText strings.Builder
	for _, content := range result.Content {
		if textContent, ok := content.(*mcp.TextContent); ok {
			outputText.WriteString(textContent.Text)
		}
	}

	return outputText.String()
}

// TestMCPServer_AddTool tests that the add tool is exposed and functional
func TestMCPServer_AddTool(t *testing.T) {
	ctx, session := setupMCPServerSession(t, "", testMCPServerListToolsTimeout)

	result, err := session.ListTools(ctx, &mcp.ListToolsParams{})
	require.NoError(t, err, "Expected list tools request to succeed")

	var addTool *mcp.Tool
	for i := range result.Tools {
		if result.Tools[i].Name == "add" {
			addTool = result.Tools[i]
			break
		}
	}

	require.NotNil(t, addTool, "Expected add tool to be registered by MCP server")
	assert.NotEmpty(t, addTool.Description, "Expected add tool description to be non-empty")
	assert.GreaterOrEqual(t, len(addTool.Description), 50, "Expected add tool description to be sufficiently descriptive")
	assert.Contains(t, addTool.Description, "workflows", "Expected add tool description to mention workflows")
	require.NotNil(t, addTool.InputSchema, "Expected add tool to expose input schema for MCP clients")
	assert.Contains(t, addTool.InputSchema, "properties", "Expected add tool input schema to define properties")
	assert.Contains(t, addTool.InputSchema, "required", "Expected add tool input schema to mark required fields")
}

// TestMCPServer_AddTool_Success tests that add tool can add a workflow successfully.
func TestMCPServer_AddTool_Success(t *testing.T) {
	tmpDir, workflowsDir := setupMCPServerAddRepo(t)
	ctx, session := setupMCPServerSession(t, tmpDir, testMCPServerAddToolTimeout)

	callResult, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "add",
		Arguments: map[string]any{
			"workflows": []any{testAddToolWorkflowSource},
			"name":      "daily-team-status-test",
		},
	})
	require.NoError(t, err, "Expected add tool call to succeed for valid workflow source")
	require.NotNil(t, callResult, "Expected add tool call result to be returned")
	assert.False(t, callResult.IsError, "Expected add tool result to indicate success")

	outputText := extractTextContent(callResult)
	assert.NotEmpty(t, outputText, "Expected add tool to return non-empty output text")
	assert.Contains(t, outputText, "daily-team-status-test", "Expected add tool output to mention the target workflow name")

	addedWorkflowPath := filepath.Join(workflowsDir, "daily-team-status-test.md")
	assert.FileExists(t, addedWorkflowPath, "Expected add tool to write workflow file in .github/workflows")

	addedWorkflowContent, err := os.ReadFile(addedWorkflowPath)
	require.NoError(t, err, "Expected to read workflow file added by MCP add tool")
	assert.Contains(t, string(addedWorkflowContent), "source:", "Expected added workflow frontmatter to include source metadata")
}

// TestMCPServer_AddToolInvocation tests calling the add tool
func TestMCPServer_AddToolInvocation(t *testing.T) {
	tmpDir, workflowsDir := setupMCPServerAddRepo(t)
	require.DirExists(t, workflowsDir, "Expected setup helper to create workflows directory")
	ctx, session := setupMCPServerSession(t, tmpDir, testMCPServerAddToolTimeout)

	tests := []struct {
		name           string
		arguments      map[string]any
		expectErr      bool
		errContains    []string
		allowToolError bool
	}{
		{
			name: "repo-only spec returns error",
			arguments: map[string]any{
				"workflows": []any{"githubnext/agentics"},
			},
			expectErr:   true,
			errContains: []string{"failed to add workflows"},
		},
		{
			name:           "missing workflows returns validation error",
			arguments:      map[string]any{},
			allowToolError: true,
			errContains:    []string{"workflows", "required"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := session.CallTool(ctx, &mcp.CallToolParams{
				Name:      "add",
				Arguments: tt.arguments,
			})

			if tt.allowToolError {
				if err != nil {
					for _, expectedErrPart := range tt.errContains {
						assert.ErrorContains(t, err, expectedErrPart, "Expected validation protocol error to include required detail")
					}
					return
				}

				require.NotNil(t, result, "Expected tool result when validation error is returned in MCP content")
				assert.True(t, result.IsError, "Expected tool result to indicate error for missing required workflows argument")

				outputText := extractTextContent(result)
				if outputText == "" {
					t.Log("Validation error returned without text content")
					return
				}

				t.Logf("Validation tool error: %s", outputText)
				for _, expectedErrPart := range tt.errContains {
					assert.Contains(t, outputText, expectedErrPart, "Expected MCP validation output to contain required error details")
				}
				return
			}

			if tt.expectErr {
				require.Error(t, err, "Expected add tool call to fail for invalid input scenario")
				addErr := err
				for _, expectedErrPart := range tt.errContains {
					t.Run(expectedErrPart, func(t *testing.T) {
						require.ErrorContains(t, addErr, expectedErrPart, "Expected error to include informative failure details")
					})
				}
				return
			}

			require.NoError(t, err, "Expected add tool call to succeed")
			assert.NotNil(t, result, "Expected add tool call to return a result")
		})
	}
}
