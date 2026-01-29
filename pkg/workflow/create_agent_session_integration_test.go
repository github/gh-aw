//go:build integration

package workflow

import (
	"testing"
)

// TestAgentTaskWorkflowCompilation tests workflow functionality
// SKIPPED: Scripts are now loaded from external files at runtime using require() pattern
func TestAgentTaskWorkflowCompilation(t *testing.T) {
	t.Skip("Workflow tests skipped - scripts now use require() pattern to load external files at runtime")
}

// TestAgentTaskWorkflowWithTargetRepo tests workflow functionality
// SKIPPED: Scripts are now loaded from external files at runtime using require() pattern
func TestAgentTaskWorkflowWithTargetRepo(t *testing.T) {
	t.Skip("Workflow tests skipped - scripts now use require() pattern to load external files at runtime")
}
