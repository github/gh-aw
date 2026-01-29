//go:build !integration

package workflow

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdateIssueToolSupportsBodyOperationsAndMetadata(t *testing.T) {
	toolsJSON := GetSafeOutputsToolsJSON()
	require.NotEmpty(t, toolsJSON, "Tools JSON should not be empty")

	var tools []map[string]any
	err := json.Unmarshal([]byte(toolsJSON), &tools)
	require.NoError(t, err, "Tools JSON should be valid")

	var updateIssueTool map[string]any
	for _, tool := range tools {
		if tool["name"] == "update_issue" {
			updateIssueTool = tool
			break
		}
	}
	require.NotNil(t, updateIssueTool, "update_issue tool should be present")

	inputSchema, ok := updateIssueTool["inputSchema"].(map[string]any)
	require.True(t, ok, "inputSchema should be an object")

	properties, ok := inputSchema["properties"].(map[string]any)
	require.True(t, ok, "properties should be an object")

	// These are required for a great campaign-generator UX: append-by-default, plus metadata updates.
	assert.Contains(t, properties, "operation", "update_issue should support body operations")
	assert.Contains(t, properties, "labels", "update_issue should support labels")
	assert.Contains(t, properties, "assignees", "update_issue should support assignees")
	assert.Contains(t, properties, "milestone", "update_issue should support milestone")

	body, ok := properties["body"].(map[string]any)
	require.True(t, ok, "body schema should be an object")
	bodyDesc, ok := body["description"].(string)
	require.True(t, ok, "body.description should be a string")
	assert.Contains(t, bodyDesc, "append", "body description should document append")
	assert.Contains(t, bodyDesc, "prepend", "body description should document prepend")
	assert.Contains(t, bodyDesc, "replace-island", "body description should document replace-island")
}
