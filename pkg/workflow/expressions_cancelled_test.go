//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

// TestBuildSafeOutputTypeWithCancelled verifies that BuildSafeOutputType properly handles workflow cancellation.
//
// Background:
// - always() runs even when the workflow is cancelled (incorrect behavior)
// - !cancelled() alone is insufficient (returns true when dependencies are skipped during cancellation)
// - !cancelled() && needs.agent.result != 'skipped' is correct (prevents running when workflow is cancelled)
//
// This test ensures safe-output jobs:
// 1. Run when dependencies succeed
// 2. Run when dependencies fail (for error reporting)
// 3. Skip when the workflow is cancelled (dependencies get skipped, not cancelled)
func TestBuildSafeOutputTypeWithCancelled(t *testing.T) {
	tests := []struct {
		name               string
		outputType         string
		expectedContains   []string
		unexpectedContains []string
	}{
		{
			name:       "create_issue should use !cancelled() and agent not skipped with contains check",
			outputType: "create_issue",
			expectedContains: []string{
				"!cancelled()",
				"needs.agent.result != 'skipped'",
				"contains(needs.agent.outputs.output_types, 'create_issue')",
			},
			unexpectedContains: []string{
				"always()",
			},
		},
		{
			name:       "push-to-pull-request-branch should use !cancelled() and agent not skipped",
			outputType: "push_to_pull_request_branch",
			expectedContains: []string{
				"!cancelled()",
				"needs.agent.result != 'skipped'",
				"contains(needs.agent.outputs.output_types, 'push_to_pull_request_branch')",
			},
			unexpectedContains: []string{
				"always()",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			condition := BuildSafeOutputType(tt.outputType).Render()

			// Verify expected strings are present
			for _, expected := range tt.expectedContains {
				if !strings.Contains(condition, expected) {
					t.Errorf("Expected condition to contain '%s', but got: %s", expected, condition)
				}
			}

			// Verify unexpected strings are NOT present
			for _, unexpected := range tt.unexpectedContains {
				if strings.Contains(condition, unexpected) {
					t.Errorf("Expected condition NOT to contain '%s', but got: %s", unexpected, condition)
				}
			}
		})
	}
}
