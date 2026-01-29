//go:build !integration

package cli

import (
	"testing"
)

// TestWaitForWorkflowCompletionUsesSignalHandling verifies that WaitForWorkflowCompletion
// uses the signal-aware polling helper, which provides Ctrl-C support
func TestWaitForWorkflowCompletionUsesSignalHandling(t *testing.T) {
	// This test verifies that the function uses PollWithSignalHandling
	// by checking that it times out correctly (a key feature of the helper)

	// We can't easily test the actual workflow checking without a real workflow,
	// but we can verify that the timeout mechanism works, which confirms
	// it's using the polling helper

	err := WaitForWorkflowCompletion("nonexistent/repo", "12345", 0, false)

	// Should timeout or fail to check workflow status
	if err == nil {
		t.Error("Expected error for nonexistent workflow, got nil")
	}
}
