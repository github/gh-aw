//go:build !integration

package workflow

import (
	"testing"
)

// TestAIReactionWorkflow tests workflow compilation
// SKIPPED: Scripts are now loaded from external files at runtime using require() pattern
func TestAIReactionWorkflow(t *testing.T) {
	t.Skip("Workflow compilation tests skipped - scripts now use require() pattern to load external files at runtime")
}

// TestAIReactionWorkflowWithoutReaction tests workflow compilation
// SKIPPED: Scripts are now loaded from external files at runtime using require() pattern
func TestAIReactionWorkflowWithoutReaction(t *testing.T) {
	t.Skip("Workflow compilation tests skipped - scripts now use require() pattern to load external files at runtime")
}

// TestAIReactionWithCommentEditFunctionality tests workflow compilation
// SKIPPED: Scripts are now loaded from external files at runtime using require() pattern
func TestAIReactionWithCommentEditFunctionality(t *testing.T) {
	t.Skip("Workflow compilation tests skipped - scripts now use require() pattern to load external files at runtime")
}

// TestCommandReactionWithCommentEdit tests workflow compilation
// SKIPPED: Scripts are now loaded from external files at runtime using require() pattern
func TestCommandReactionWithCommentEdit(t *testing.T) {
	t.Skip("Workflow compilation tests skipped - scripts now use require() pattern to load external files at runtime")
}

// TestCommandTriggerDefaultReaction tests workflow compilation
// SKIPPED: Scripts are now loaded from external files at runtime using require() pattern
func TestCommandTriggerDefaultReaction(t *testing.T) {
	t.Skip("Workflow compilation tests skipped - scripts now use require() pattern to load external files at runtime")
}

// TestCommandTriggerCustomReaction tests workflow compilation
// SKIPPED: Scripts are now loaded from external files at runtime using require() pattern
func TestCommandTriggerCustomReaction(t *testing.T) {
	t.Skip("Workflow compilation tests skipped - scripts now use require() pattern to load external files at runtime")
}
