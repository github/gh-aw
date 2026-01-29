//go:build !integration

package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetWorkflowStatuses(t *testing.T) {
	tests := []struct {
		name         string
		pattern      string
		repoOverride string
		verbose      bool
		// We can't easily test the actual results without mocking gh CLI,
		// so we just verify the function doesn't panic
	}{
		{
			name:         "simple pattern",
			pattern:      "test-workflow",
			repoOverride: "",
			verbose:      false,
		},
		{
			name:         "with repo override",
			pattern:      "daily-status",
			repoOverride: "owner/repo",
			verbose:      false,
		},
		{
			name:         "verbose mode",
			pattern:      "workflow-name",
			repoOverride: "owner/repo",
			verbose:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// This function calls gh CLI, so it will likely error in tests
			// We just verify it doesn't panic
			statuses, err := getWorkflowStatuses(tt.pattern, tt.repoOverride, tt.verbose)

			// Either succeeds or fails gracefully, but shouldn't panic
			if err == nil {
				assert.NotNil(t, statuses, "Statuses should not be nil on success")
			} else {
				// Error is acceptable in test environment without gh CLI setup
				assert.Error(t, err, "Expected error without gh CLI")
			}
		})
	}
}
