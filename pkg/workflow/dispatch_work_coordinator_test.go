package workflow

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseDispatchWorkCoordinatorConfig(t *testing.T) {
	schema := map[string]any{
		"type":     "object",
		"required": []any{"title"},
		"properties": map[string]any{
			"title": map[string]any{"type": "string"},
		},
		"additionalProperties": false,
	}
	config, err := parseDispatchWorkCoordinatorConfig(map[string]any{"schema": schema})
	require.NoError(t, err)
	assert.Equal(t, schema, config.Schema)
	assert.JSONEq(t, `{"additionalProperties":false,"properties":{"title":{"type":"string"}},"required":["title"],"type":"object"}`, config.SchemaJSON)
	assert.True(t, config.shouldAutoClaim())

	disabled := false
	config, err = parseDispatchWorkCoordinatorConfig(map[string]any{"id": "shared-queue", "auto-claim": disabled, "schema": schema})
	require.NoError(t, err)
	assert.Equal(t, "shared-queue", config.ID)
	assert.False(t, config.shouldAutoClaim())

	_, err = parseDispatchWorkCoordinatorConfig(true)
	require.ErrorContains(t, err, "must be an object")

	_, err = parseDispatchWorkCoordinatorConfig(map[string]any{})
	require.ErrorContains(t, err, ".schema must be a JSON Schema object")

	_, err = parseDispatchWorkCoordinatorConfig(map[string]any{"schema": schema, "branch": "main"})
	require.ErrorContains(t, err, "unsupported property")

	_, err = parseDispatchWorkCoordinatorConfig(map[string]any{"id": "../unsafe", "schema": schema})
	require.ErrorContains(t, err, ".id must be")

	_, err = parseDispatchWorkCoordinatorConfig(map[string]any{"auto-claim": "false", "schema": schema})
	require.ErrorContains(t, err, ".auto-claim must be a boolean")

	_, err = parseDispatchWorkCoordinatorConfig(map[string]any{"schema": map[string]any{"type": "string"}})
	require.ErrorContains(t, err, "schema.type must be object")

	_, err = parseDispatchWorkCoordinatorConfig(map[string]any{"schema": map[string]any{"type": "object", "minProperties": 1}})
	require.ErrorContains(t, err, "unsupported JSON Schema keyword")

	_, err = parseDispatchWorkCoordinatorConfig(map[string]any{
		"schema": map[string]any{
			"type":       "object",
			"properties": map[string]any{"title": false},
		},
	})
	require.ErrorContains(t, err, "each JSON Schema definition must be an object")

	tools, err := ParseToolsConfig(map[string]any{"dispatch-work-coordinator": map[string]any{"schema": schema}})
	require.NoError(t, err)
	assert.Equal(t, schema, tools.ToMap()["dispatch-work-coordinator"].(map[string]any)["schema"])
	tools, err = ParseToolsConfig(map[string]any{"dispatch-work-coordinator": map[string]any{"id": "shared-queue", "auto-claim": false, "schema": schema}})
	require.NoError(t, err)
	coordinatorTools := tools.ToMap()["dispatch-work-coordinator"].(map[string]any)
	assert.Equal(t, "shared-queue", coordinatorTools["id"])
	assert.False(t, coordinatorTools["auto-claim"].(bool))
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
		DispatchWorkCoordinator: &DispatchWorkCoordinatorConfig{SchemaJSON: `{"type":"object"}`},
	}
	assert.Contains(t, collectMCPTools(data), "dispatch-work-coordinator")
}

func TestRenderDispatchWorkCoordinatorMCP(t *testing.T) {
	data := &WorkflowData{
		DispatchWorkCoordinator: &DispatchWorkCoordinatorConfig{SchemaJSON: `{"type":"object"}`},
	}
	var rendered strings.Builder
	NewMCPConfigRenderer(MCPRendererOptions{IsLast: true}).RenderDispatchWorkCoordinatorMCP(&rendered, data)
	assert.Contains(t, rendered.String(), `"dispatch-work-coordinator": {`)
	assert.Contains(t, rendered.String(), "dispatch_work_coordinator_mcp_server.cjs")
	assert.Contains(t, rendered.String(), `GH_AW_DISPATCH_WORK_COORDINATOR_TOKEN`)
	assert.Contains(t, rendered.String(), `GH_AW_DISPATCH_WORK_COORDINATOR_SCHEMA`)
	assert.Contains(t, rendered.String(), `GH_AW_DISPATCH_WORK_COORDINATOR_ID`)

	rendered.Reset()
	NewMCPConfigRenderer(MCPRendererOptions{Format: "toml"}).RenderDispatchWorkCoordinatorMCP(&rendered, data)
	assert.Contains(t, rendered.String(), "[mcp_servers.dispatch-work-coordinator]")
	assert.Contains(t, rendered.String(), "GH_AW_DISPATCH_WORK_COORDINATOR_SCHEMA")
	assert.Contains(t, rendered.String(), "GH_AW_DISPATCH_WORK_COORDINATOR_ID")
}

func TestDispatchWorkCoordinatorEnvironmentDoesNotRequireRepoMemory(t *testing.T) {
	data := &WorkflowData{
		DispatchWorkCoordinator: &DispatchWorkCoordinatorConfig{
			SchemaJSON: `{"type":"object"}`,
		},
	}

	env := collectMCPEnvironmentVariables(nil, nil, data, false)
	assert.Equal(t, "${{ secrets.GITHUB_TOKEN }}", env["GH_AW_DISPATCH_WORK_COORDINATOR_TOKEN"])
	assert.Empty(t, env["GH_AW_DISPATCH_WORK_COORDINATOR_ID"])
	assert.JSONEq(t, `{"type":"object"}`, env["GH_AW_DISPATCH_WORK_COORDINATOR_SCHEMA"])

	var rendered strings.Builder
	writeMCPGatewayStepEnvWithCustomGatewayEnvNames(&rendered, map[string]string{
		"GH_AW_DISPATCH_WORK_COORDINATOR_SCHEMA": env["GH_AW_DISPATCH_WORK_COORDINATOR_SCHEMA"],
	}, nil, nil, nil, "")
	assert.Contains(t, rendered.String(), `GH_AW_DISPATCH_WORK_COORDINATOR_SCHEMA: "{\"type\":\"object\"}"`)
}

func TestDispatchWorkCoordinatorActivationPassesWorkSchema(t *testing.T) {
	schema := `{"type":"object","required":["title"]}`
	step := buildDispatchWorkCoordinatorActivationStep(schema, "shared-queue")
	assert.Contains(t, step, `GH_AW_DISPATCH_WORK_COORDINATOR_ID: 'shared-queue'`)
	assert.Contains(t, step, `GH_AW_DISPATCH_WORK_COORDINATOR_SCHEMA: '{"type":"object","required":["title"]}'`)
}
