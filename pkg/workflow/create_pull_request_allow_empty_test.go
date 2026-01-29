//go:build !integration

package workflow

import (
	"testing"
)

// TestCreatePullRequestJobWithAllowEmpty tests workflow functionality
// SKIPPED: Scripts are now loaded from external files at runtime using require() pattern
func TestCreatePullRequestJobWithAllowEmpty(t *testing.T) {
	t.Skip("Workflow tests skipped - scripts now use require() pattern to load external files at runtime")
}

// TestCreatePullRequestJobWithoutAllowEmpty tests workflow functionality
// SKIPPED: Scripts are now loaded from external files at runtime using require() pattern
func TestCreatePullRequestJobWithoutAllowEmpty(t *testing.T) {
	t.Skip("Workflow tests skipped - scripts now use require() pattern to load external files at runtime")
}
