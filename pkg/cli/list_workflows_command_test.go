//go:build !integration

package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunListWorkflows_JSONOutput(t *testing.T) {
	// Save current directory
	originalDir, err := os.Getwd()
	require.NoError(t, err, "Failed to get current directory")

	// Change to repository root
	repoRoot := filepath.Join(originalDir, "..", "..")
	err = os.Chdir(repoRoot)
	require.NoError(t, err, "Failed to change to repository root")
	defer os.Chdir(originalDir)

	// Test JSON output without pattern
	t.Run("JSON output without pattern", func(t *testing.T) {
		err := RunListWorkflows("", false, true, "")
		assert.NoError(t, err, "RunListWorkflows with JSON flag should not error")
	})

	// Test JSON output with pattern
	t.Run("JSON output with pattern", func(t *testing.T) {
		err := RunListWorkflows("smoke", false, true, "")
		assert.NoError(t, err, "RunListWorkflows with JSON flag and pattern should not error")
	})

	// Test JSON output with label filter
	t.Run("JSON output with label filter", func(t *testing.T) {
		err := RunListWorkflows("", false, true, "test")
		assert.NoError(t, err, "RunListWorkflows with JSON flag and label filter should not error")
	})
}

func TestWorkflowListItem_JSONMarshaling(t *testing.T) {
	// Test that WorkflowListItem can be marshaled to JSON
	item := WorkflowListItem{
		Workflow: "test-workflow",
		EngineID: "copilot",
		Compiled: "Yes",
		Labels:   []string{"test", "automation"},
		On: map[string]any{
			"workflow_dispatch": nil,
		},
	}

	jsonBytes, err := json.Marshal(item)
	require.NoError(t, err, "Failed to marshal WorkflowListItem")

	// Verify JSON contains expected fields
	var unmarshaled map[string]any
	err = json.Unmarshal(jsonBytes, &unmarshaled)
	require.NoError(t, err, "Failed to unmarshal JSON")

	assert.Equal(t, "test-workflow", unmarshaled["workflow"], "workflow field should match")
	assert.Equal(t, "copilot", unmarshaled["engine_id"], "engine_id field should match")
	assert.Equal(t, "Yes", unmarshaled["compiled"], "compiled field should match")

	// Verify labels array
	labels, ok := unmarshaled["labels"].([]any)
	require.True(t, ok, "labels should be an array")
	assert.Len(t, labels, 2, "Should have 2 labels")
	assert.Equal(t, "test", labels[0], "First label should be 'test'")
	assert.Equal(t, "automation", labels[1], "Second label should be 'automation'")

	// Verify "on" field is included
	onField, ok := unmarshaled["on"].(map[string]any)
	require.True(t, ok, "on field should be a map")
	_, exists := onField["workflow_dispatch"]
	assert.True(t, exists, "on field should contain 'workflow_dispatch' key")
}

func TestRunListWorkflows_TextOutput(t *testing.T) {
	// Save current directory
	originalDir, err := os.Getwd()
	require.NoError(t, err, "Failed to get current directory")

	// Change to repository root
	repoRoot := filepath.Join(originalDir, "..", "..")
	err = os.Chdir(repoRoot)
	require.NoError(t, err, "Failed to change to repository root")
	defer os.Chdir(originalDir)

	// Test text output
	t.Run("Text output without pattern", func(t *testing.T) {
		err := RunListWorkflows("", false, false, "")
		assert.NoError(t, err, "RunListWorkflows without JSON flag should not error")
	})

	// Test text output with pattern
	t.Run("Text output with pattern", func(t *testing.T) {
		err := RunListWorkflows("ci-", false, false, "")
		assert.NoError(t, err, "RunListWorkflows with pattern should not error")
	})
}

func TestNewListCommand(t *testing.T) {
	cmd := NewListCommand()

	// Verify command properties
	assert.Equal(t, "list", cmd.Use[:4], "Command use should start with 'list'")
	assert.NotEmpty(t, cmd.Short, "Command should have short description")
	assert.NotEmpty(t, cmd.Long, "Command should have long description")
	assert.NotNil(t, cmd.RunE, "Command should have RunE function")

	// Verify flags exist
	jsonFlag := cmd.Flags().Lookup("json")
	assert.NotNil(t, jsonFlag, "Command should have --json flag")

	labelFlag := cmd.Flags().Lookup("label")
	assert.NotNil(t, labelFlag, "Command should have --label flag")
}
