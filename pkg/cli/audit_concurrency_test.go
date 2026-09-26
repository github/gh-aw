package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sync/errgroup"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCollectAuditAnalysisResultsReturnsContextCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := collectAuditAnalysisResults(ctx, WorkflowRun{}, t.TempDir(), false, false)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled error, got %v", err)
	}
}

func TestCollectAuditAnalysisResultsBackfillsSkillsFromUsageSummary(t *testing.T) {
	t.Parallel()

	runDir := t.TempDir()
	summaryPath := filepath.Join(runDir, "usage", "activity", "summary.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(summaryPath), 0o755))
	require.NoError(t, os.WriteFile(summaryPath, []byte(`{
		"schema":"usage-activity-summary/v1",
		"skills":{"items":[{"name":"documentation","invocation_count":2,"failed_count":1}]}
	}`), 0o644))

	results, err := collectAuditAnalysisResults(context.Background(), WorkflowRun{}, runDir, false, false)
	require.NoError(t, err)
	assert.Equal(t, []SkillActivation{{
		Name:            "documentation",
		Status:          "invoked",
		Source:          "usage_summary",
		InvocationCount: 2,
		FailedCount:     1,
	}}, results.skillActivations)
}

func TestRunAuditAnalysisSoftFailuresRemainNonFatal(t *testing.T) {
	t.Parallel()
	g, gctx := errgroup.WithContext(context.Background())
	called := false

	runAuditAnalysis(g, gctx, false, "test", "test warning", func(v int) {
		called = true
	}, func() (int, error) {
		return 0, errors.New("soft failure")
	})

	if err := g.Wait(); err != nil {
		t.Fatalf("expected nil errgroup error for soft failure, got %v", err)
	}
	if called {
		t.Fatal("expected setter not to be called on soft failure")
	}
}

func TestRunAuditAnalysisReturnsCancellationForSoftFailuresWhenContextCanceled(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	g, gctx := errgroup.WithContext(ctx)
	called := false
	started := make(chan struct{})

	runAuditAnalysis(g, gctx, false, "test", "test warning", func(v int) {
		called = true
	}, func() (int, error) {
		close(started)
		<-gctx.Done()
		return 0, errors.New("soft failure")
	})

	<-started
	cancel()

	if err := g.Wait(); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled error for canceled context soft failure, got %v", err)
	}
	if called {
		t.Fatal("expected setter not to be called when context is canceled")
	}
}
