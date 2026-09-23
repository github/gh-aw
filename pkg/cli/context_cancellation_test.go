//go:build !integration

package cli

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRunWorkflowOnGitHubWithCancellation tests that RunWorkflowOnGitHub respects context cancellation
func TestRunWorkflowOnGitHubWithCancellation(t *testing.T) {
	t.Parallel()
	// Create a context that's already cancelled
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Try to run a workflow with a cancelled context
	err := RunWorkflowOnGitHub(ctx, "test-workflow", RunOptions{})

	// Should return context.Canceled error
	assert.ErrorIs(t, err, context.Canceled, "Should return context.Canceled error when context is cancelled")
}

// TestRunWorkflowsOnGitHubWithCancellation tests that RunWorkflowsOnGitHub respects context cancellation
func TestRunWorkflowsOnGitHubWithCancellation(t *testing.T) {
	t.Parallel()
	// Create a context that's already cancelled
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Try to run workflows with a cancelled context
	err := RunWorkflowsOnGitHub(ctx, []string{"test-workflow"}, RunOptions{})

	// Should return context.Canceled error
	assert.ErrorIs(t, err, context.Canceled, "Should return context.Canceled error when context is cancelled")
}

// TestCompileWorkflowsWithCancellation tests that CompileWorkflows respects context cancellation
func TestCompileWorkflowsWithCancellation(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
	// Create a context that's already cancelled
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Try to download logs with a cancelled context
	err := DownloadWorkflowLogs(ctx, LogsDownloadOptions{
		Count:     10,
		OutputDir: "/tmp/test-logs",
	})

	// Should return context.Canceled error
	assert.ErrorIs(t, err, context.Canceled, "Should return context.Canceled error when context is cancelled")
}

// TestAuditWorkflowRunWithCancellation tests that AuditWorkflowRun respects context cancellation
func TestAuditWorkflowRunWithCancellation(t *testing.T) {
	t.Parallel()
	// Create a context that's already cancelled
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Try to audit a run with a cancelled context
	_, err := AuditWorkflowRun(ctx, 123456, AuditOptions{
		OutputDir: "/tmp/test-audit",
	})

	// Should return context.Canceled error
	assert.ErrorIs(t, err, context.Canceled, "Should return context.Canceled error when context is cancelled")
}

// TestRunWorkflowsOnGitHubCancellationDuringExecution tests cancellation during workflow execution
func TestRunWorkflowsOnGitHubCancellationDuringExecution(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Try to run multiple workflows that would take a long time
	// This should fail validation before timeout, but if it gets past validation,
	// it should respect the context cancellation
	err := RunWorkflowsOnGitHub(ctx, []string{"nonexistent-workflow-1", "nonexistent-workflow-2"}, RunOptions{})

	// Should return an error (either validation error or context error)
	assert.Error(t, err, "Should return an error")
}

// TestDownloadWorkflowLogsTimeoutRespected tests that timeout-minutes is respected
func TestDownloadWorkflowLogsTimeoutRespected(t *testing.T) {
	originalFetch := logsFetchWorkflowRunBatch
	t.Cleanup(func() {
		logsFetchWorkflowRunBatch = originalFetch
	})
	logsFetchWorkflowRunBatch = func(ctx context.Context, _ LogsDownloadOptions, _ string, _ int, _ bool) (workflowRunBatch, error) {
		<-ctx.Done()
		return workflowRunBatch{}, ctx.Err()
	}

	start := time.Now()
	err := DownloadWorkflowLogs(context.Background(), LogsDownloadOptions{
		WorkflowName:   "test-workflow",
		Count:          100,
		OutputDir:      t.TempDir(),
		TimeoutMinutes: 1,
		TimeoutSeconds: 1,
	})
	elapsed := time.Since(start)

	require.NoError(t, err)
	assert.GreaterOrEqual(t, elapsed, time.Second, "Should wait for the configured timeout")
	assert.Less(t, elapsed, 3*time.Second, "Should stop promptly after the configured timeout")
}
