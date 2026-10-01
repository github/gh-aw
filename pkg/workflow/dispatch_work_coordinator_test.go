package workflow

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseDispatchWorkCoordinatorConfig(t *testing.T) {
	config, err := parseDispatchWorkCoordinatorConfig(map[string]any{})
	require.NoError(t, err)
	assert.NotNil(t, config)

	_, err = parseDispatchWorkCoordinatorConfig(true)
	require.ErrorContains(t, err, "must be an object")

	_, err = parseDispatchWorkCoordinatorConfig(map[string]any{"branch": "main"})
	assert.ErrorContains(t, err, "does not support properties")
}

func TestDispatchWorkCoordinatorRequiresContentsWrite(t *testing.T) {
	data := &WorkflowData{Permissions: "permissions:\n  contents: read\n", Tools: map[string]any{}}
	err := validateDispatchWorkCoordinatorPermissions(data)
	require.ErrorContains(t, err, "requires contents: write")

	data.Permissions = "permissions:\n  contents: write\n"
	require.NoError(t, validateDispatchWorkCoordinatorPermissions(data))
}

func TestCollectMCPToolsIncludesDispatchWorkCoordinator(t *testing.T) {
	data := &WorkflowData{
		Tools:                   map[string]any{},
		DispatchWorkCoordinator: &DispatchWorkCoordinatorConfig{},
	}
	assert.Contains(t, collectMCPTools(data), "dispatch-work-coordinator")
}

func TestRenderDispatchWorkCoordinatorMCP(t *testing.T) {
	data := &WorkflowData{}
	var rendered strings.Builder
	NewMCPConfigRenderer(MCPRendererOptions{IsLast: true}).RenderDispatchWorkCoordinatorMCP(&rendered, data)
	assert.Contains(t, rendered.String(), `"dispatch-work-coordinator": {`)
	assert.Contains(t, rendered.String(), "dispatch_work_coordinator_mcp_server.cjs")
	assert.Contains(t, rendered.String(), `GH_AW_DISPATCH_WORK_COORDINATOR_TOKEN`)

	rendered.Reset()
	NewMCPConfigRenderer(MCPRendererOptions{Format: "toml"}).RenderDispatchWorkCoordinatorMCP(&rendered, data)
	assert.Contains(t, rendered.String(), "[mcp_servers.dispatch-work-coordinator]")
}
