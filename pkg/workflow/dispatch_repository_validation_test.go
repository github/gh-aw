//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

func TestValidateDispatchRepositoryToolStopsAtMissingWorkflow(t *testing.T) {
	collector := NewErrorCollector(false)
	tool := &DispatchRepositoryToolConfig{Repository: "invalid"}
	if err := validateDispatchRepositoryTool("trigger", tool, collector); err != nil {
		t.Fatal(err)
	}
	if collector.Count() != 1 {
		t.Fatalf("collected %d errors, want only the missing workflow error", collector.Count())
	}
	err := collector.FormattedError("dispatch_repository")
	if err == nil || !strings.Contains(err.Error(), "must specify a 'workflow' field") {
		t.Fatalf("unexpected validation error: %v", err)
	}
}

func TestValidateDispatchRepositoryToolAggregatesTargetErrors(t *testing.T) {
	for _, failFast := range []bool{false, true} {
		collector := NewErrorCollector(failFast)
		tool := &DispatchRepositoryToolConfig{
			Workflow:            "ci.yml",
			EventType:           "trigger",
			Repository:          "invalid",
			AllowedRepositories: []string{"org/*", "${{ inputs.repo }}", "also-invalid"},
		}
		err := validateDispatchRepositoryTool("trigger", tool, collector)
		if failFast {
			if err == nil || !strings.Contains(err.Error(), "invalid 'repository' format") {
				t.Fatalf("unexpected fail-fast error: %v", err)
			}
		} else {
			if err != nil {
				t.Fatal(err)
			}
			if collector.Count() != 2 {
				t.Fatalf("collected %d errors, want both invalid target errors", collector.Count())
			}
		}
	}
}
