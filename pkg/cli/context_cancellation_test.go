//go:build !integration

package cli

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestRunWorkflowOnGitHubWithCancellation tests that RunWorkflowOnGitHub respects context cancellation
func TestRunWorkflowOnGitHubWithCancellation(t *testing.T) {
	// Create a context that's already cancelled
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Try to run a workflow with a cancelled context
	err := RunWorkflowOnGitHub(ctx, "test-workflow", false, "", "", "", false, false, false, false, []string{}, false, false)

	// Should return context.Canceled error
	assert.ErrorIs(t, err, context.Canceled, "Should return context.Canceled error when context is cancelled")
}

// TestRunWorkflowsOnGitHubWithCancellation tests that RunWorkflowsOnGitHub respects context cancellation
func TestRunWorkflowsOnGitHubWithCancellation(t *testing.T) {
	// Create a context that's already cancelled
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Try to run workflows with a cancelled context
	err := RunWorkflowsOnGitHub(ctx, []string{"test-workflow"}, 0, false, "", "", "", false, false, false, []string{}, false, false)

	// Should return context.Canceled error
	assert.ErrorIs(t, err, context.Canceled, "Should return context.Canceled error when context is cancelled")
}

// TestCompileWorkflowsWithCancellation tests that CompileWorkflows respects context cancellation
func TestCompileWorkflowsWithCancellation(t *testing.T) {
	// Create a context that's already cancelled
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	config := CompileConfig{
		MarkdownFiles:        []string{"test.md"},
		Verbose:              false,
		EngineOverride:       "",
		Validate:             false,
		Watch:                false,
		WorkflowDir:          "",
		SkipInstructions:     false,
		NoEmit:               true,
		Purge:                false,
		TrialMode:            false,
		TrialLogicalRepoSlug: "",
		Strict:               false,
	}

	// Try to compile with a cancelled context
	_, err := CompileWorkflows(ctx, config)

	// Should return context.Canceled error
	assert.ErrorIs(t, err, context.Canceled, "Should return context.Canceled error when context is cancelled")
}

// TestDownloadWorkflowLogsWithCancellation tests that DownloadWorkflowLogs respects context cancellation
func TestDownloadWorkflowLogsWithCancellation(t *testing.T) {
	// Create a context that's already cancelled
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Try to download logs with a cancelled context
	err := DownloadWorkflowLogs(ctx, "", 10, "", "", "/tmp/test-logs", "", "", 0, 0, "", false, false, false, false, false, false, false, 0, "", "")

	// Should return context.Canceled error
	assert.ErrorIs(t, err, context.Canceled, "Should return context.Canceled error when context is cancelled")
}

// TestAuditWorkflowRunWithCancellation tests that AuditWorkflowRun respects context cancellation
func TestAuditWorkflowRunWithCancellation(t *testing.T) {
	// Create a context that's already cancelled
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Try to audit a run with a cancelled context
	err := AuditWorkflowRun(ctx, 123456, "", "", "", "/tmp/test-audit", false, false, false, 0, 0)

	// Should return context.Canceled error
	assert.ErrorIs(t, err, context.Canceled, "Should return context.Canceled error when context is cancelled")
}

// TestRunWorkflowsOnGitHubCancellationDuringExecution tests cancellation during workflow execution
func TestRunWorkflowsOnGitHubCancellationDuringExecution(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Try to run multiple workflows that would take a long time
	// This should fail validation before timeout, but if it gets past validation,
	// it should respect the context cancellation
	err := RunWorkflowsOnGitHub(ctx, []string{"nonexistent-workflow-1", "nonexistent-workflow-2"}, 0, false, "", "", "", false, false, false, []string{}, false, false)

	// Should return an error (either validation error or context error)
	assert.Error(t, err, "Should return an error")
}

// TestDownloadWorkflowLogsTimeoutRespected tests that timeout is converted to context deadline
func TestDownloadWorkflowLogsTimeoutRespected(t *testing.T) {
	// Test with a very short timeout (1 second) and verify the function returns quickly
	ctx := context.Background()

	start := time.Now()
	// Use a workflow name that doesn't exist to avoid actual network calls
	_ = DownloadWorkflowLogs(ctx, "nonexistent-workflow-12345", 100, "", "", "/tmp/test-logs", "", "", 0, 0, "", false, false, false, false, false, false, false, 1, "", "")
	elapsed := time.Since(start)

	// Should complete within reasonable time (give 5 seconds buffer for test overhead)
	assert.Less(t, elapsed, 5*time.Second, "Should complete quickly when workflow doesn't exist")
}
