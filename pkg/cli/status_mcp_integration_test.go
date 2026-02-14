//go:build !integration

package cli

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGetWorkflowStatuses_MCPIntegration verifies that the GetWorkflowStatuses function
// returns valid data that can be marshaled to JSON
func TestGetWorkflowStatuses_MCPIntegration(t *testing.T) {
	// This test requires being run from the repository root
	// since it needs .github/workflows directory
	statuses, err := GetWorkflowStatuses("", "", "", "")
	// We expect either:
	// - No error and a valid (possibly empty) slice
	// - An error if we're not in a repository with workflows
	if err != nil {
		// If we're not in a repo with workflows, that's ok
		t.Logf("GetWorkflowStatuses returned error (expected if not in repo): %v", err)
		return
	}

	// Verify we got a slice (even if empty)
	require.NotNil(t, statuses, "GetWorkflowStatuses should return a non-nil slice")

	// Verify that the result can be marshaled to JSON
	jsonBytes, err := json.Marshal(statuses)
	require.NoError(t, err, "Should be able to marshal statuses to JSON")
	require.NotEmpty(t, jsonBytes, "JSON output should not be empty")

	// Verify that the JSON is valid by unmarshaling it back
	var unmarshaled []WorkflowStatus
	err = json.Unmarshal(jsonBytes, &unmarshaled)
	require.NoError(t, err, "Should be able to unmarshal JSON back to WorkflowStatus slice")

	t.Logf("GetWorkflowStatuses returned %d workflows", len(statuses))
}

// TestGetWorkflowStatuses_WithPattern tests filtering by pattern
func TestGetWorkflowStatuses_WithPattern(t *testing.T) {
	// Get all statuses first
	allStatuses, err := GetWorkflowStatuses("", "", "", "")
	if err != nil {
		t.Skipf("Skipping test: not in a repository with workflows: %v", err)
		return
	}

	if len(allStatuses) == 0 {
		t.Skip("Skipping test: no workflows found")
		return
	}

	// Use the first workflow's name as a pattern
	firstWorkflowName := allStatuses[0].Workflow
	pattern := firstWorkflowName[:min(3, len(firstWorkflowName))] // Use first 3 chars as pattern

	// Get filtered statuses
	filteredStatuses, err := GetWorkflowStatuses(pattern, "", "", "")
	require.NoError(t, err, "GetWorkflowStatuses with pattern should not error")

	// Verify that filtered results are a subset
	assert.LessOrEqual(t, len(filteredStatuses), len(allStatuses),
		"Filtered results should be <= all results")

	// Verify that all filtered results contain the pattern
	for _, status := range filteredStatuses {
		assert.Contains(t, status.Workflow, pattern,
			"Filtered workflow should contain pattern")
	}

	t.Logf("Pattern '%s' matched %d of %d workflows", pattern, len(filteredStatuses), len(allStatuses))
}

// TestGetWorkflowStatuses_MCPJSONStructure verifies the JSON structure
func TestGetWorkflowStatuses_MCPJSONStructure(t *testing.T) {
	statuses, err := GetWorkflowStatuses("", "", "", "")
	if err != nil {
		t.Skipf("Skipping test: not in a repository with workflows: %v", err)
		return
	}

	if len(statuses) == 0 {
		t.Skip("Skipping test: no workflows found")
		return
	}

	// Take the first workflow and verify its structure
	firstStatus := statuses[0]

	// Verify required fields are present
	assert.NotEmpty(t, firstStatus.Workflow, "Workflow name should not be empty")
	assert.NotEmpty(t, firstStatus.EngineID, "Engine ID should not be empty")
	assert.NotEmpty(t, firstStatus.Compiled, "Compiled status should not be empty")
	assert.NotEmpty(t, firstStatus.Status, "Status should not be empty")
	assert.NotEmpty(t, firstStatus.TimeRemaining, "Time remaining should not be empty")

	// Verify JSON marshaling
	jsonBytes, err := json.Marshal(firstStatus)
	require.NoError(t, err, "Should marshal to JSON")

	// Verify JSON structure
	var jsonMap map[string]any
	err = json.Unmarshal(jsonBytes, &jsonMap)
	require.NoError(t, err, "Should unmarshal JSON")

	// Verify expected fields
	assert.Contains(t, jsonMap, "workflow", "JSON should contain 'workflow' field")
	assert.Contains(t, jsonMap, "engine_id", "JSON should contain 'engine_id' field")
	assert.Contains(t, jsonMap, "compiled", "JSON should contain 'compiled' field")
	assert.Contains(t, jsonMap, "status", "JSON should contain 'status' field")
	assert.Contains(t, jsonMap, "time_remaining", "JSON should contain 'time_remaining' field")
}
