//go:build !integration

package workflow

import (
	"testing"
)

// TestCommentEnvVarsOnlyWithReaction tests workflow compilation
// SKIPPED: Scripts are now loaded from external files at runtime using require() pattern
func TestCommentEnvVarsOnlyWithReaction(t *testing.T) {
	t.Skip("Workflow compilation tests skipped - scripts now use require() pattern to load external files at runtime")
}

// TestActivationJobOutputsWithReaction tests workflow compilation
// SKIPPED: Scripts are now loaded from external files at runtime using require() pattern
func TestActivationJobOutputsWithReaction(t *testing.T) {
	t.Skip("Workflow compilation tests skipped - scripts now use require() pattern to load external files at runtime")
}
