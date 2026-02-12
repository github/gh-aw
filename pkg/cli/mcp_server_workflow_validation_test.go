//go:build !integration

package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMCPValidateWorkflowName(t *testing.T) {
	tests := []struct {
		name          string
		workflowName  string
		shouldSucceed bool
		errorContains string
	}{
		{
			name:          "empty workflow name is valid (all workflows)",
			workflowName:  "",
			shouldSucceed: true,
		},
		{
			name:          "non-existent workflow returns error",
			workflowName:  "nonexistent-workflow-xyz-12345",
			shouldSucceed: false,
			errorContains: "workflow 'nonexistent-workflow-xyz-12345' not found",
		},
		{
			name:          "error includes suggestions",
			workflowName:  "invalid-name",
			shouldSucceed: false,
			errorContains: "Use the 'status' tool to see all available workflows",
		},
		{
			name:          "error includes fuzzy matched suggestions for similar names",
			workflowName:  "brave-test", // Similar to "brave" workflow
			shouldSucceed: false,
			errorContains: "workflow 'brave-test' not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateWorkflowName(tt.workflowName)

			if tt.shouldSucceed {
				assert.NoError(t, err, "Validation should succeed for workflow: %s", tt.workflowName)
			} else {
				assert.Error(t, err, "Validation should fail for workflow: %s", tt.workflowName)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains, "Error message should contain expected text")
				}
			}
		})
	}
}
