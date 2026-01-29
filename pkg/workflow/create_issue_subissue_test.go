//go:build !integration

package workflow

import (
	"testing"
)

// TestCreateIssueSubissueFeature tests the sub-issue feature in create_issue scripts
// SKIPPED: Scripts are now loaded from external files at runtime using require() pattern
func TestCreateIssueSubissueFeature(t *testing.T) {
	t.Skip("Create issue sub-issue feature tests skipped - scripts now use require() pattern to load external files at runtime")
}

// TestCreateIssueWorkflowCompilationWithSubissue tests workflow compilation with sub-issues
// SKIPPED: Scripts are now loaded from external files at runtime using require() pattern
func TestCreateIssueWorkflowCompilationWithSubissue(t *testing.T) {
	t.Skip("Create issue workflow compilation with sub-issue tests skipped - scripts now use require() pattern to load external files at runtime")
}
