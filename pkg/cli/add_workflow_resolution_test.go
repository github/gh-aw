package cli

import (
	"context"
	"testing"
)

func TestResolveWorkflowsRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name      string
		workflows []string
		wantError string
	}{
		{
			name:      "no workflows",
			wantError: "at least one workflow name is required",
		},
		{
			name:      "empty workflow name",
			workflows: []string{"owner/repo/workflow.md", ""},
			wantError: "workflow name cannot be empty (workflow 2)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ResolveWorkflows(context.Background(), tt.workflows, false)
			if err == nil || err.Error() != tt.wantError {
				t.Fatalf("ResolveWorkflows() error = %v, want %q", err, tt.wantError)
			}
		})
	}
}
