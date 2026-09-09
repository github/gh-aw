package cli

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBackfillOperationalValueReportObservationsUsesWeeklyCache(t *testing.T) {
	originalGradeRun := operationalValueReportGradeRun
	t.Cleanup(func() { operationalValueReportGradeRun = originalGradeRun })

	gradeCalls := 0
	operationalValueReportGradeRun = func(_ context.Context, evaluator *operationalValueReportEvaluator, run operationalValueReportRun, _ time.Time, _ string) operationalValueReportObservation {
		gradeCalls++
		value := float64(gradeCalls) / 10
		return operationalValueReportObservation{
			Run: run, Value: &value, Status: "pass", Mature: true,
			EvidenceAt: "2026-08-31T00:00:00Z", EvidenceCutoff: "2026-08-28T00:00:00Z",
			MaturesAt: "2026-08-28T00:00:00Z", EvaluatorDigest: evaluator.EvaluatorDigest,
			Source: "evaluator-replay",
		}
	}

	evaluator := &operationalValueReportEvaluator{
		WorkflowID: "daily-file-diet", EvaluatorDigest: "abc123",
		Definition: operationalValueReportDefinition{Repository: "github/gh-aw"},
	}
	runs := []operationalValueReportRun{
		{ID: "1", Attempt: 1, CreatedAt: time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)},
		{ID: "2", Attempt: 1, CreatedAt: time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)},
	}
	cacheRoot := t.TempDir()

	first, firstStats, err := backfillOperationalValueReportObservations(context.Background(), evaluator, runs, time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC), cacheRoot, "", false, 1)
	require.NoError(t, err)
	require.Len(t, first, 2)
	assert.Equal(t, 2, firstStats.Evaluated)
	assert.Equal(t, 0, firstStats.CacheHits)
	assert.Equal(t, 2, gradeCalls)

	second, secondStats, err := backfillOperationalValueReportObservations(context.Background(), evaluator, runs, time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC), cacheRoot, "", false, 1)
	require.NoError(t, err)
	require.Len(t, second, 2)
	assert.Equal(t, 0, secondStats.Evaluated)
	assert.Equal(t, 2, secondStats.CacheHits)
	assert.Equal(t, 2, gradeCalls)
	assert.Equal(t, "evaluator-replay", second[0].Source)
}

func TestBackfillOperationalValueReportObservationsDoesNotCacheNonFinalResults(t *testing.T) {
	originalGradeRun := operationalValueReportGradeRun
	t.Cleanup(func() { operationalValueReportGradeRun = originalGradeRun })

	for _, test := range []struct {
		name   string
		status string
		mature bool
	}{
		{name: "error", status: "error", mature: false},
		{name: "immature", status: "pass", mature: false},
		{name: "unavailable", status: "unavailable", mature: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			gradeCalls := 0
			operationalValueReportGradeRun = func(_ context.Context, evaluator *operationalValueReportEvaluator, run operationalValueReportRun, _ time.Time, _ string) operationalValueReportObservation {
				gradeCalls++
				return operationalValueReportObservation{Run: run, Status: test.status, Mature: test.mature, EvaluatorDigest: evaluator.EvaluatorDigest}
			}
			evaluator := &operationalValueReportEvaluator{WorkflowID: "daily-file-diet", EvaluatorDigest: "abc123", Definition: operationalValueReportDefinition{Repository: "github/gh-aw"}}
			runs := []operationalValueReportRun{{ID: "1", Attempt: 1, CreatedAt: time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)}}
			cacheRoot := t.TempDir()

			_, _, err := backfillOperationalValueReportObservations(context.Background(), evaluator, runs, time.Now(), cacheRoot, "", false, 1)
			require.NoError(t, err)
			_, _, err = backfillOperationalValueReportObservations(context.Background(), evaluator, runs, time.Now(), cacheRoot, "", false, 1)
			require.NoError(t, err)
			assert.Equal(t, 2, gradeCalls)
		})
	}
}

func TestBackfillOperationalValueReportObservationsRunsWeeksConcurrently(t *testing.T) {
	originalGradeRun := operationalValueReportGradeRun
	t.Cleanup(func() { operationalValueReportGradeRun = originalGradeRun })

	started := make(chan struct{}, 2)
	release := make(chan struct{})
	var active atomic.Int32
	var maximum atomic.Int32
	operationalValueReportGradeRun = func(_ context.Context, evaluator *operationalValueReportEvaluator, run operationalValueReportRun, _ time.Time, _ string) operationalValueReportObservation {
		current := active.Add(1)
		for current > maximum.Load() && !maximum.CompareAndSwap(maximum.Load(), current) {
		}
		started <- struct{}{}
		<-release
		active.Add(-1)
		value := 1.0
		return operationalValueReportObservation{Run: run, Value: &value, Status: "pass", Mature: true, EvaluatorDigest: evaluator.EvaluatorDigest}
	}

	evaluator := &operationalValueReportEvaluator{WorkflowID: "daily-file-diet", EvaluatorDigest: "abc123", Definition: operationalValueReportDefinition{Repository: "github/gh-aw"}}
	runs := []operationalValueReportRun{
		{ID: "1", Attempt: 1, CreatedAt: time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)},
		{ID: "2", Attempt: 1, CreatedAt: time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)},
	}
	done := make(chan error, 1)
	go func() {
		_, _, err := backfillOperationalValueReportObservations(context.Background(), evaluator, runs, time.Now(), t.TempDir(), "", true, 2)
		done <- err
	}()

	for range 2 {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for concurrent evaluator executions")
		}
	}
	close(release)
	require.NoError(t, <-done)
	assert.Equal(t, int32(2), maximum.Load())
}

func TestListOperationalValueReportRunsPathEscapesWorkflowFile(t *testing.T) {
	originalRunGH := operationalValueReportRunGH
	t.Cleanup(func() { operationalValueReportRunGH = originalRunGH })

	operationalValueReportRunGH = func(_ context.Context, _ string, args ...string) ([]byte, error) {
		require.Contains(t, args[len(args)-1], "daily%23file.lock.yml")
		return []byte(`[{"total_count":1,"workflow_runs":[{"id":42,"run_attempt":1,"html_url":"https://example.test/42","status":"completed","conclusion":"success","created_at":"2026-08-10T00:00:00Z","event":"schedule","head_branch":"main","head_sha":"abc"}]}]`), nil
	}

	runs, err := listOperationalValueReportRuns(context.Background(), "github/gh-aw", "github.com", "daily#file.lock.yml", time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	require.Len(t, runs, 1)
	assert.Equal(t, "42", runs[0].ID)
}

func TestListOperationalValueReportRunsSplitsCreatedWindowAtCap(t *testing.T) {
	originalRunGH := operationalValueReportRunGH
	t.Cleanup(func() { operationalValueReportRunGH = originalRunGH })

	seenRanges := make([]string, 0)
	operationalValueReportRunGH = func(_ context.Context, _ string, args ...string) ([]byte, error) {
		var createdRange string
		for index := range len(args) - 1 {
			if args[index] == "-f" && strings.HasPrefix(args[index+1], "created=") {
				createdRange = strings.TrimPrefix(args[index+1], "created=")
				break
			}
		}
		require.NotEmpty(t, createdRange)
		seenRanges = append(seenRanges, createdRange)

		switch createdRange {
		case "2026-08-01T00:00:00Z..2026-08-03T00:00:00Z":
			return []byte(`[{"total_count":1000,"workflow_runs":[]}]`), nil
		case "2026-08-01T00:00:00Z..2026-08-02T00:00:00Z":
			return []byte(`[{"total_count":2,"workflow_runs":[{"id":1,"run_attempt":1,"html_url":"https://example.test/1","status":"completed","conclusion":"success","created_at":"2026-08-01T00:00:00Z","event":"schedule","head_branch":"main","head_sha":"a"}]}]`), nil
		case "2026-08-02T00:00:00Z..2026-08-03T00:00:00Z":
			return []byte(`[{"total_count":1,"workflow_runs":[{"id":2,"run_attempt":1,"html_url":"https://example.test/2","status":"completed","conclusion":"success","created_at":"2026-08-03T00:00:00Z","event":"schedule","head_branch":"main","head_sha":"b"}]}]`), nil
		default:
			return []byte(`[{"total_count":0,"workflow_runs":[]}]`), nil
		}
	}

	runs, err := listOperationalValueReportRuns(context.Background(), "github/gh-aw", "github.com", "daily-file-diet.lock.yml", time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	require.Len(t, runs, 2)
	assert.Equal(t, "1", runs[0].ID)
	assert.Equal(t, "2", runs[1].ID)

	payload, err := json.Marshal(seenRanges)
	require.NoError(t, err)
	assert.Contains(t, string(payload), "2026-08-01T00:00:00Z..2026-08-03T00:00:00Z")
	assert.Contains(t, string(payload), "2026-08-01T00:00:00Z..2026-08-02T00:00:00Z")
	assert.Contains(t, string(payload), "2026-08-02T00:00:00Z..2026-08-03T00:00:00Z")
}
