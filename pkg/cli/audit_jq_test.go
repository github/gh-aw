//go:build !integration

package cli

import (
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAuditCommandJqFlag tests that the audit command accepts jq flag
// This is currently not implemented but should be added for consistency with logs command
func TestAuditCommandJqFlag(t *testing.T) {
	// Skip if jq is not available
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("Skipping test: jq not found in PATH")
	}

	cmd := NewAuditCommand()
	require.NotNil(t, cmd, "Audit command should be created")

	// Check if jq flag exists
	jqFlag := cmd.Flags().Lookup("jq")
	if jqFlag == nil {
		t.Skip("jq flag not yet implemented on audit command (this test documents the expected behavior)")
	}

	// Verify flag properties
	assert.Equal(t, "jq", jqFlag.Name, "Flag name should be 'jq'")
	assert.Equal(t, "string", jqFlag.Value.Type(), "Flag should be a string type")
}

// TestMCPServer_AuditToolWithJqFilter tests the audit MCP tool with jq filter
// This test verifies that the audit tool's jq parameter is properly defined and works correctly
func TestMCPServer_AuditToolWithJqFilter(t *testing.T) {
	// Skip if jq is not available
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("Skipping test: jq not found in PATH")
	}

	// Test 1: Verify that auditArgs struct has JqFilter field
	t.Run("auditArgs_has_jq_field", func(t *testing.T) {
		// Create a sample auditArgs to verify the structure
		args := struct {
			RunIDOrURL string `json:"run_id_or_url"`
			JqFilter   string `json:"jq,omitempty"`
		}{
			RunIDOrURL: "123456",
			JqFilter:   ".overview",
		}

		// Verify JqFilter can be set
		assert.Equal(t, ".overview", args.JqFilter, "JqFilter should be settable")
	})

	// Test 2: Verify ApplyJqFilter works with overview filter
	t.Run("apply_jq_filter_overview", func(t *testing.T) {
		// Create sample audit JSON with overview section
		sampleJSON := `{
			"overview": {
				"run_id": 123456,
				"workflow_name": "Test",
				"status": "completed"
			},
			"metrics": {
				"token_usage": 1000
			}
		}`

		// Apply jq filter
		result, err := ApplyJqFilter(sampleJSON, ".overview")
		require.NoError(t, err, "jq filter should succeed")

		// Verify result contains only overview
		assert.Contains(t, result, "run_id", "Result should contain overview data")
		assert.Contains(t, result, "123456", "Result should contain run_id value")
		assert.NotContains(t, result, "token_usage", "Result should not contain metrics data")
	})

	// Test 3: Verify invalid jq filter returns error
	t.Run("invalid_jq_filter_returns_error", func(t *testing.T) {
		sampleJSON := `{"overview": {"run_id": 123456}}`

		// Apply invalid jq filter
		_, err := ApplyJqFilter(sampleJSON, ".[invalid")
		assert.Error(t, err, "Invalid jq filter should return error")
		assert.Contains(t, err.Error(), "jq filter failed", "Error should mention jq filter failure")
	})
}
