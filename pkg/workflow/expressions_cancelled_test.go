//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/githubnext/gh-aw/pkg/constants"
)

// TestBuildSafeOutputTypeWithCancelled verifies that BuildSafeOutputType properly handles workflow cancellation.
//
// Background:
// - always() ensures jobs run even when upstream dependencies are skipped (but completed successfully)
// - !cancelled() prevents running when the workflow itself is cancelled
// - needs.agent.outputs.activated == 'true' ensures the agent job completed successfully
//
// The combined pattern: always() && !cancelled() && needs.agent.outputs.activated == 'true'
// This test ensures safe-output jobs:
// 1. Run when dependencies succeed
// 2. Run when dependencies are skipped (but successful)
// 3. Skip when the workflow is cancelled
func TestBuildSafeOutputTypeWithCancelled(t *testing.T) {
	tests := []struct {
		name               string
		outputType         string
		expectedContains   []string
		unexpectedContains []string
	}{
		{
			name:       "create_issue should use always() && !cancelled() && activated pattern",
			outputType: "create_issue",
			expectedContains: []string{
				"always()",
				"!cancelled()",
				"needs." + string(constants.AgentJobName) + ".outputs." + constants.ActivatedOutput + " == 'true'",
				"contains(needs.agent.outputs.output_types, 'create_issue')",
			},
			unexpectedContains: []string{
				"needs.agent.result != 'skipped'",
			},
		},
		{
			name:       "push-to-pull-request-branch should use always() && !cancelled() && activated pattern",
			outputType: "push_to_pull_request_branch",
			expectedContains: []string{
				"always()",
				"!cancelled()",
				"needs." + string(constants.AgentJobName) + ".outputs." + constants.ActivatedOutput + " == 'true'",
				"contains(needs.agent.outputs.output_types, 'push_to_pull_request_branch')",
			},
			unexpectedContains: []string{
				"needs.agent.result != 'skipped'",
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
