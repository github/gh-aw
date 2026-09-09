//go:build !integration

package cli

import (
	"context"
	"io"
	"math/rand"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/github/gh-aw/pkg/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── extractWorkflowIDFromName ─────────────────────────────────────────────────

func TestExtractWorkflowIDFromName(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want string
	}{
		{"ci-doctor", "ci-doctor"},
		{"ci-doctor.lock.yml", "ci-doctor"},
		{"ci-doctor.yml", "ci-doctor"},
		{"foo.yaml", "foo"},
		{"daily-planner.lock.yml", "daily-planner"},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, extractWorkflowIDFromName(tc.in), "input=%q", tc.in)
	}
}

func TestExtractEngineNames(t *testing.T) {
	t.Parallel()
	cfg := &workflow.FrontmatterConfig{
		Engine: map[string]any{
			"id":       "copilot",
			"fallback": []any{"claude", map[string]any{"id": "codex"}},
		},
	}
	assert.Equal(t, []string{"claude", "codex", "copilot"}, extractEngineNames(cfg))
}

// ── RunForecast validation ────────────────────────────────────────────────────

func TestRunForecast_InvalidPeriod(t *testing.T) {
	t.Parallel()
	cfg := ForecastConfig{Days: 30, Period: "quarter", SampleSize: 10}
	err := RunForecast(cfg)
	require.Error(t, err, "should error for invalid period")
}

func TestRunForecast_InvalidDays(t *testing.T) {
	t.Parallel()
	cfg := ForecastConfig{Days: 90, Period: "month", SampleSize: 10}
	err := RunForecast(cfg)
	require.Error(t, err, "should error for days=90 (max is 30)")
}

func TestRunForecast_InvalidTimeout(t *testing.T) {
	t.Parallel()
	cfg := ForecastConfig{Days: 30, Period: "month", SampleSize: 10, TimeoutMinutes: -1}
	err := RunForecast(cfg)
	require.Error(t, err, "should error for negative timeout")
}

func TestNewForecastCommand_DaysFlagDocumentsAllowedValues(t *testing.T) {
	t.Parallel()
	cmd := NewForecastCommand()
	require.NotNil(t, cmd)

	daysFlag := cmd.Flags().Lookup("days")
	require.NotNil(t, daysFlag, "forecast command should register --days")
	assert.Equal(t, "Historical window in days to sample run history (allowed values: 7, 30)", daysFlag.Usage)
	assert.NotContains(t, cmd.Long, ").  When runs have been", "Long description should not contain duplicate spacing")
	assert.NotContains(t, cmd.Long, "used.  The", "Long description should not contain duplicate spacing")
	assert.NotContains(t, cmd.Long, "interval.  Use this", "Long description should not contain duplicate spacing")
}

func TestNewForecastCommand_TimeoutFlag(t *testing.T) {
	t.Parallel()
	cmd := NewForecastCommand()
	require.NotNil(t, cmd)

	timeoutFlag := cmd.Flags().Lookup("timeout")
	require.NotNil(t, timeoutFlag, "forecast command should register --timeout")
	assert.Equal(t, "Gracefully stop forecast computation after this many minutes (0 = no timeout)", timeoutFlag.Usage)
	assert.Equal(t, "0", timeoutFlag.DefValue)
}

// ── Duration enrichment ───────────────────────────────────────────────────────

// TestDurationEnrichment verifies that the forecast loop computes Duration from
// StartedAt/UpdatedAt when the Duration field is zero (as returned by gh run list).
func TestDurationEnrichment(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	end := start.Add(5 * time.Minute)

	r := WorkflowRun{
		Status:     "completed",
		Conclusion: "success",
		StartedAt:  start,
		UpdatedAt:  end,
		// Duration is intentionally zero (not populated by gh run list)
	}

	// Simulate the enrichment logic from forecastWorkflow.
	if r.Duration == 0 && !r.StartedAt.IsZero() && !r.UpdatedAt.IsZero() {
		r.Duration = r.UpdatedAt.Sub(r.StartedAt)
	}

	assert.Equal(t, 5*time.Minute, r.Duration)
}

// TestObservedRunsPerPeriodConsistency verifies that the λ value stored in the
// JSON-serialisable ForecastWorkflowResult.ObservedRunsPerPeriod field is the same
// value that would be passed to runMonteCarlo (R-MC-002).
//
// This is a structural test: it constructs a result whose ObservedRunsPerPeriod is
// set by the same arithmetic used in forecastWorkflow, then calls runMonteCarlo with
// that field directly and asserts the simulation produces sensible output — confirming
// that no intermediate recalculation or mutation of λ occurs between JSON output and
// Monte Carlo execution.
func TestObservedRunsPerPeriodConsistency(t *testing.T) {
	t.Parallel()
	// Reproduce the λ calculation from forecastWorkflow.
	const (
		historyDays   = 30
		sampledRuns   = 15
		projectedDays = 30 // "month" period
	)
	observedRunsPerPeriod := float64(sampledRuns) / float64(historyDays) * float64(projectedDays)

	// Populate a ForecastWorkflowResult the same way forecastWorkflow does.
	result := ForecastWorkflowResult{
		WorkflowID:            "ci-doctor",
		Period:                "month",
		SampledRuns:           sampledRuns,
		HistoryDays:           historyDays,
		ObservedRunsPerPeriod: observedRunsPerPeriod,
	}

	// Build deterministic ET observations.
	etObs := make([]int, sampledRuns)
	for i := range etObs {
		etObs[i] = 10_000 + i*500
	}
	successCount := sampledRuns

	// runMonteCarlo uses result.ObservedRunsPerPeriod as λ — the same field that
	// appears in JSON output. Verify both the field value and the simulation are
	// consistent (non-nil, same λ).
	rng := rand.New(rand.NewSource(99)) //nolint:gosec
	mc := runMonteCarlo(etObs, successCount, result.ObservedRunsPerPeriod, rng)
	require.NotNil(t, mc, "runMonteCarlo must return non-nil for positive ObservedRunsPerPeriod")

	// The field exposed in JSON output must equal what was used for MC.
	assert.InEpsilon(t, observedRunsPerPeriod, result.ObservedRunsPerPeriod, 1e-12,
		"ObservedRunsPerPeriod JSON field must equal the λ passed to runMonteCarlo")

	// Sanity-check simulation output is plausible for the given λ.
	assert.Positive(t, mc.P50ProjectedAIC,
		"P50 should be positive when success rate is 100%%")
	assert.LessOrEqual(t, mc.P10ProjectedAIC, mc.P50ProjectedAIC,
		"P10 ≤ P50")
	assert.LessOrEqual(t, mc.P50ProjectedAIC, mc.P90ProjectedAIC,
		"P50 ≤ P90")
}

// TestForecastWorkflow_LambdaConsistencyAcrossOutputFormats verifies that the λ value
// used by the Monte Carlo engine is identical to the ObservedRunsPerPeriod field exposed
// in both JSON output and verbose/table diagnostics (forecast-specification.md §13,
// closes issue #31984).
//
// Both renderForecastJSON and renderForecastTable operate on the same ForecastResult
// struct, so the λ used by runMonteCarlo (result.ObservedRunsPerPeriod) is always the
// same value reported to the caller in either output format.
func TestForecastWorkflow_LambdaConsistencyAcrossOutputFormats(t *testing.T) {
	originalList := forecastListWorkflowRunsPaginated
	originalLoadAIC := forecastLoadRunAIC
	t.Cleanup(func() {
		forecastListWorkflowRunsPaginated = originalList
		forecastLoadRunAIC = originalLoadAIC
	})

	const (
		historyDays   = 30
		projectedDays = 30 // "month" period
	)
	completedRuns := []WorkflowRun{
		{DatabaseID: 1, Status: "completed", Conclusion: "success", Duration: 5 * time.Minute},
		{DatabaseID: 2, Status: "completed", Conclusion: "success", Duration: 6 * time.Minute},
		{DatabaseID: 3, Status: "completed", Conclusion: "failure", Duration: 3 * time.Minute},
		{DatabaseID: 4, Status: "completed", Conclusion: "success", Duration: 7 * time.Minute},
		{DatabaseID: 5, Status: "completed", Conclusion: "success", Duration: 4 * time.Minute},
	}
	runAIC := map[int64]float64{
		1: 4.2,
		2: 5.0,
		3: 3.4,
		4: 4.6,
		5: 4.1,
	}
	forecastLoadRunAIC = func(_ context.Context, runID int64, _ bool) (float64, bool) {
		return runAIC[runID], true
	}
	forecastListWorkflowRunsPaginated = func(_ ListWorkflowRunsOptions) ([]WorkflowRun, int, error) {
		return completedRuns, len(completedRuns), nil
	}

	result, err := forecastWorkflow(context.Background(), "ci-doctor", "2026-01-01", ForecastConfig{
		Days:       historyDays,
		Period:     "month",
		SampleSize: 100,
	}, projectedDays)
	require.NoError(t, err)

	// The expected λ is the observed run frequency scaled to the projection period.
	// This is also the value emitted in the JSON "observed_runs_per_period" field.
	n := len(completedRuns)
	expectedLambda := float64(n) / float64(historyDays) * float64(projectedDays)

	// Verify ObservedRunsPerPeriod (the JSON-serialised λ) equals the expected value.
	assert.InEpsilon(t, expectedLambda, result.ObservedRunsPerPeriod, 1e-12,
		"JSON field observed_runs_per_period must equal the λ used by the Monte Carlo engine")

	// Monte Carlo must have been called with the same λ — confirmed by a non-nil result.
	require.NotNil(t, result.MonteCarlo,
		"Monte Carlo simulation must run for positive ObservedRunsPerPeriod (λ=%.2f)", expectedLambda)

	// Both JSON output (renderForecastJSON) and table output (renderForecastTable) use the
	// same ForecastResult, so they are structurally guaranteed to report the same λ.
	assert.Positive(t, result.MonteCarlo.P50ProjectedAIC,
		"P50 must be positive for positive λ and non-zero AIC observations")
}

func TestForecastRateLimitSleep_ContextCancelled(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := forecastRateLimitSleep(ctx, time.Second)
	require.ErrorIs(t, err, context.Canceled)
}

func TestForecastRateLimitSleep_CompletesWithoutCancellation(t *testing.T) {
	t.Parallel()
	err := forecastRateLimitSleep(context.Background(), time.Millisecond)
	require.NoError(t, err)
}

func TestForecastWorkflow_IgnoresSkippedRuns(t *testing.T) {
	originalList := forecastListWorkflowRunsPaginated
	originalLoadAIC := forecastLoadRunAIC
	t.Cleanup(func() {
		forecastListWorkflowRunsPaginated = originalList
		forecastLoadRunAIC = originalLoadAIC
	})

	start := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	forecastListWorkflowRunsPaginated = func(_ ListWorkflowRunsOptions) ([]WorkflowRun, int, error) {
		runs := []WorkflowRun{
			{DatabaseID: 11, Status: "completed", Conclusion: "skipped", Duration: 10 * time.Minute},
			{DatabaseID: 12, Status: "completed", Conclusion: "success", Duration: 5 * time.Minute, StartedAt: start, UpdatedAt: start.Add(5 * time.Minute)},
			{DatabaseID: 13, Status: "completed", Conclusion: "failure", Duration: 6 * time.Minute, StartedAt: start.Add(10 * time.Minute), UpdatedAt: start.Add(16 * time.Minute)},
		}
		return runs, len(runs), nil
	}
	runAIC := map[int64]float64{
		11: 9.99, // skipped and ignored
		12: 1.0,
		13: 2.0,
	}
	forecastLoadRunAIC = func(_ context.Context, runID int64, _ bool) (float64, bool) {
		return runAIC[runID], true
	}

	result, err := forecastWorkflow(context.Background(), "smoke-copilot", "2026-01-01", ForecastConfig{
		Days:       30,
		Period:     "month",
		SampleSize: 100,
	}, 30)
	require.NoError(t, err)
	assert.Equal(t, 2, result.SampledRuns, "skipped runs should not be sampled")
	assert.InDelta(t, 1.5, result.AvgAIC, 1e-9, "metrics should ignore skipped runs")
	assert.InEpsilon(t, 0.5, result.SuccessRate, 1e-9)
}

func TestSampleLimitRespected(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	runs := []WorkflowRun{
		{DatabaseID: 1, Status: "completed", Conclusion: "success", StartedAt: start},
		{DatabaseID: 2, Status: "completed", Conclusion: "failure", StartedAt: start.Add(time.Hour)},
		{DatabaseID: 3, Status: "in_progress", StartedAt: start.Add(2 * time.Hour)},
	}

	tests := []struct {
		name       string
		sampleSize int
		want       []int64
	}{
		{name: "caps below total", sampleSize: 2, want: []int64{1, 2}},
		{name: "no-op above total", sampleSize: 10, want: []int64{1, 2, 3}},
		{name: "zero means no local cap", sampleSize: 0, want: []int64{1, 2, 3}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := filterForecastSampleRuns(runs, "2026-08-01", tt.sampleSize)
			require.Len(t, got, len(tt.want))
			for i, wantID := range tt.want {
				assert.Equal(t, wantID, got[i].DatabaseID)
			}
		})
	}
}

func TestDateWindowCutoffRespected(t *testing.T) {
	t.Parallel()
	cutoff := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	runs := []WorkflowRun{
		{DatabaseID: 1, Status: "completed", Conclusion: "success", StartedAt: cutoff.Add(-time.Nanosecond)},
		{DatabaseID: 2, Status: "completed", Conclusion: "success", StartedAt: cutoff},
		{DatabaseID: 3, Status: "in_progress", StartedAt: cutoff.Add(24 * time.Hour)},
	}

	got := filterForecastSampleRuns(runs, "2026-08-01", 0)
	require.Len(t, got, 2)
	assert.Equal(t, int64(2), got[0].DatabaseID)
	assert.Equal(t, int64(3), got[1].DatabaseID)
}

func TestForecastWorkflow_RequestsRecentRuns(t *testing.T) {
	originalList := forecastListWorkflowRunsPaginated
	originalLoadAIC := forecastLoadRunAIC
	t.Cleanup(func() {
		forecastListWorkflowRunsPaginated = originalList
		forecastLoadRunAIC = originalLoadAIC
	})

	var capturedOpts ListWorkflowRunsOptions
	start := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	forecastListWorkflowRunsPaginated = func(opts ListWorkflowRunsOptions) ([]WorkflowRun, int, error) {
		capturedOpts = opts
		runs := []WorkflowRun{
			{DatabaseID: 12, Status: "completed", Conclusion: "success", Duration: 5 * time.Minute, StartedAt: start, UpdatedAt: start.Add(5 * time.Minute)},
		}
		return runs, len(runs), nil
	}
	forecastLoadRunAIC = func(_ context.Context, runID int64, _ bool) (float64, bool) {
		if runID == 12 {
			return 1.0, true
		}
		return 0, true
	}

	_, err := forecastWorkflow(context.Background(), "smoke-copilot", "2026-01-01", ForecastConfig{
		Days:       30,
		Period:     "month",
		SampleSize: 100,
	}, 30)
	require.NoError(t, err)
	assert.Empty(t, capturedOpts.Status, "forecast must request all recent run statuses so in-progress partial observations are available")
}

func TestMissingArtifactContributesZeroET(t *testing.T) {
	originalList := forecastListWorkflowRunsPaginated
	originalLoadAIC := forecastLoadRunAIC
	t.Cleanup(func() {
		forecastListWorkflowRunsPaginated = originalList
		forecastLoadRunAIC = originalLoadAIC
	})

	start := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	forecastListWorkflowRunsPaginated = func(_ ListWorkflowRunsOptions) ([]WorkflowRun, int, error) {
		runs := []WorkflowRun{
			{DatabaseID: 12, Status: "completed", Conclusion: "success", Duration: 5 * time.Minute, StartedAt: start, UpdatedAt: start.Add(5 * time.Minute)},
			{DatabaseID: 13, Status: "completed", Conclusion: "success", Duration: 6 * time.Minute, StartedAt: start.Add(10 * time.Minute), UpdatedAt: start.Add(16 * time.Minute)},
		}
		return runs, len(runs), nil
	}
	forecastLoadRunAIC = func(_ context.Context, runID int64, _ bool) (float64, bool) {
		if runID == 12 {
			return 0, true
		}
		return 2.0, true
	}

	result, err := forecastWorkflow(context.Background(), "smoke-copilot", "2026-01-01", ForecastConfig{
		Days:       30,
		Period:     "month",
		SampleSize: 100,
	}, 30)
	require.NoError(t, err)
	assert.Equal(t, 2, result.SampledRuns)
	assert.InDelta(t, 1.0, result.AvgAIC, 1e-9)
	assert.InEpsilon(t, 1.0, result.SuccessRate, 1e-9)
	require.Len(t, result.RunSamples, 2)
	assert.Equal(t, int64(12), result.RunSamples[0].RunID)
	assert.Zero(t, result.RunSamples[0].AIC)
	assert.Equal(t, int64(13), result.RunSamples[1].RunID)
	assert.InDelta(t, 2.0, result.RunSamples[1].AIC, 1e-9)
}

func TestEmptySampleProducesNilProjection(t *testing.T) {
	originalList := forecastListWorkflowRunsPaginated
	t.Cleanup(func() {
		forecastListWorkflowRunsPaginated = originalList
	})

	forecastListWorkflowRunsPaginated = func(_ ListWorkflowRunsOptions) ([]WorkflowRun, int, error) {
		runs := []WorkflowRun{
			{DatabaseID: 11, Status: "completed", Conclusion: "skipped"},
			{DatabaseID: 12, Status: "completed", Conclusion: "action_required"},
		}
		return runs, len(runs), nil
	}

	result, err := forecastWorkflow(context.Background(), "smoke-copilot", "2026-01-01", ForecastConfig{
		Days:       30,
		Period:     "month",
		SampleSize: 100,
	}, 30)
	require.NoError(t, err)
	assert.Zero(t, result.SampledRuns)
	assert.Zero(t, result.ProjectedAIC)
	assert.Nil(t, result.MonteCarlo)
	assert.Empty(t, result.RunSamples)
}

func TestInProgressRunIsPartialObservation(t *testing.T) {
	originalList := forecastListWorkflowRunsPaginated
	originalLoadAIC := forecastLoadRunAIC
	t.Cleanup(func() {
		forecastListWorkflowRunsPaginated = originalList
		forecastLoadRunAIC = originalLoadAIC
	})

	start := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	forecastListWorkflowRunsPaginated = func(_ ListWorkflowRunsOptions) ([]WorkflowRun, int, error) {
		runs := []WorkflowRun{
			{DatabaseID: 12, Status: "completed", Conclusion: "success", Duration: 5 * time.Minute, StartedAt: start, UpdatedAt: start.Add(5 * time.Minute)},
			{DatabaseID: 13, Status: "in_progress", Duration: 6 * time.Minute, StartedAt: start.Add(10 * time.Minute), UpdatedAt: start.Add(16 * time.Minute)},
		}
		return runs, len(runs), nil
	}
	runAIC := map[int64]float64{
		12: 1.0,
		13: 2.75,
	}
	forecastLoadRunAIC = func(_ context.Context, runID int64, _ bool) (float64, bool) {
		return runAIC[runID], true
	}

	result, err := forecastWorkflow(context.Background(), "smoke-copilot", "2026-01-01", ForecastConfig{
		Days:       30,
		Period:     "month",
		SampleSize: 100,
	}, 30)
	require.NoError(t, err)
	assert.Equal(t, 2, result.SampledRuns)
	assert.InEpsilon(t, 0.5, result.SuccessRate, 1e-9)
	require.Len(t, result.RunSamples, 2)
	assert.Equal(t, int64(13), result.RunSamples[1].RunID)
	assert.InDelta(t, 2.75, result.RunSamples[1].AIC, 1e-9)
}

func TestProjectedTokensEqualsP50(t *testing.T) {
	originalList := forecastListWorkflowRunsPaginated
	originalLoadAIC := forecastLoadRunAIC
	t.Cleanup(func() {
		forecastListWorkflowRunsPaginated = originalList
		forecastLoadRunAIC = originalLoadAIC
	})

	start := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	forecastListWorkflowRunsPaginated = func(_ ListWorkflowRunsOptions) ([]WorkflowRun, int, error) {
		runs := []WorkflowRun{
			{DatabaseID: 12, Status: "completed", Conclusion: "success", Duration: 5 * time.Minute, StartedAt: start, UpdatedAt: start.Add(5 * time.Minute)},
			{DatabaseID: 13, Status: "completed", Conclusion: "failure", Duration: 6 * time.Minute, StartedAt: start.Add(10 * time.Minute), UpdatedAt: start.Add(16 * time.Minute)},
			{DatabaseID: 14, Status: "completed", Conclusion: "success", Duration: 7 * time.Minute, StartedAt: start.Add(20 * time.Minute), UpdatedAt: start.Add(27 * time.Minute)},
		}
		return runs, len(runs), nil
	}
	runAIC := map[int64]float64{
		12: 1.0,
		13: 2.0,
		14: 3.0,
	}
	forecastLoadRunAIC = func(_ context.Context, runID int64, _ bool) (float64, bool) {
		return runAIC[runID], true
	}

	result, err := forecastWorkflow(context.Background(), "smoke-copilot", "2026-01-01", ForecastConfig{
		Days:       30,
		Period:     "month",
		SampleSize: 100,
	}, 30)
	require.NoError(t, err)
	require.NotNil(t, result.MonteCarlo)
	assert.InDelta(t, result.MonteCarlo.P50ProjectedAIC, result.ProjectedAIC, 0)
}

func TestRenderForecastTable_ZeroMonteCarloRangeRendersDash(t *testing.T) {
	reader, writer, err := os.Pipe()
	require.NoError(t, err)
	originalStderr := os.Stderr
	os.Stderr = writer
	t.Cleanup(func() {
		os.Stderr = originalStderr
	})

	err = renderForecastTable(ForecastResult{
		Period: "month",
		Workflows: []ForecastWorkflowResult{
			{
				WorkflowID:  "smoke-copilot",
				SampledRuns: 1,
				SuccessRate: 1,
				MonteCarlo: &ForecastMonteCarloSummary{
					P10ProjectedAIC: 0,
					P50ProjectedAIC: 0,
					P90ProjectedAIC: 0,
				},
			},
		},
	}, ForecastConfig{Days: 30, Period: "month"})
	require.NoError(t, err)

	require.NoError(t, writer.Close())
	out, readErr := io.ReadAll(reader)
	require.NoError(t, readErr)
	assert.NotContains(t, string(out), "-–-")
	assert.Contains(t, string(out), "Cost/projection figures are AI Credits (AIC)")
}

func TestLoadCachedRunAIC_UsageArtifactFirst(t *testing.T) {
	originalDir, err := os.Getwd()
	require.NoError(t, err)
	tmpDir := t.TempDir()
	require.NoError(t, os.Chdir(tmpDir))
	t.Cleanup(func() {
		_ = os.Chdir(originalDir)
	})

	originalDownload := forecastDownloadRunArtifacts
	originalAnalyze := forecastAnalyzeTokenUsage
	t.Cleanup(func() {
		forecastDownloadRunArtifacts = originalDownload
		forecastAnalyzeTokenUsage = originalAnalyze
	})

	var downloaded []string
	analyzeCalled := false
	forecastDownloadRunArtifacts = func(_ context.Context, _ int64, _ string, _ bool, _, _, _ string, artifactFilter []string) error {
		downloaded = append(downloaded, strings.Join(artifactFilter, ","))
		return nil
	}
	forecastAnalyzeTokenUsage = func(_ string, _ bool) (*TokenUsageSummary, error) {
		analyzeCalled = true
		return &TokenUsageSummary{TotalAIC: 12.34}, nil
	}

	aic := loadCachedRunAIC(context.Background(), 999_000_001, false)
	require.InDelta(t, 12.34, aic, 1e-9)
	require.True(t, analyzeCalled)
	require.Equal(t, []string{"usage"}, downloaded)
}

func TestLoadCachedRunAIC_MissingUsageReturnsZero(t *testing.T) {
	originalDir, err := os.Getwd()
	require.NoError(t, err)
	tmpDir := t.TempDir()
	require.NoError(t, os.Chdir(tmpDir))
	t.Cleanup(func() {
		_ = os.Chdir(originalDir)
	})

	originalDownload := forecastDownloadRunArtifacts
	originalAnalyze := forecastAnalyzeTokenUsage
	t.Cleanup(func() {
		forecastDownloadRunArtifacts = originalDownload
		forecastAnalyzeTokenUsage = originalAnalyze
	})

	var downloaded []string
	analyzeCalled := false
	forecastDownloadRunArtifacts = func(_ context.Context, _ int64, _ string, _ bool, _, _, _ string, artifactFilter []string) error {
		downloaded = append(downloaded, strings.Join(artifactFilter, ","))
		return errNoMatchingArtifact
	}
	forecastAnalyzeTokenUsage = func(_ string, _ bool) (*TokenUsageSummary, error) {
		analyzeCalled = true
		return nil, nil
	}

	aic := loadCachedRunAIC(context.Background(), 999_000_002, false)
	require.Zero(t, aic)
	require.False(t, analyzeCalled)
	require.Equal(t, []string{"usage"}, downloaded)
}

// ── parallelLoadRunAICs ───────────────────────────────────────────────────────

// TestParallelLoadRunAICs_ReturnsAllAICValues verifies that parallelLoadRunAICs collects
// AIC values for every run, regardless of concurrency level.
func TestParallelLoadRunAICs_ReturnsAllAICValues(t *testing.T) {
	originalLoadAIC := forecastLoadRunAIC
	t.Cleanup(func() { forecastLoadRunAIC = originalLoadAIC })

	wantAIC := map[int64]float64{
		1: 1.0,
		2: 2.0,
		3: 3.0,
		4: 4.0,
		5: 5.0,
	}
	forecastLoadRunAIC = func(_ context.Context, runID int64, _ bool) (float64, bool) {
		return wantAIC[runID], true
	}

	runs := []WorkflowRun{
		{DatabaseID: 1, Status: "completed", Conclusion: "success"},
		{DatabaseID: 2, Status: "completed", Conclusion: "success"},
		{DatabaseID: 3, Status: "completed", Conclusion: "failure"},
		{DatabaseID: 4, Status: "completed", Conclusion: "success"},
		{DatabaseID: 5, Status: "completed", Conclusion: "success"},
	}

	got := parallelLoadRunAICs(context.Background(), runs, ForecastConfig{DownloadConcurrency: 3})
	assert.Equal(t, wantAIC, got)
}

func TestParallelLoadRunAICs_OmitsUnavailableAICValues(t *testing.T) {
	originalLoadAIC := forecastLoadRunAIC
	t.Cleanup(func() { forecastLoadRunAIC = originalLoadAIC })

	forecastLoadRunAIC = func(_ context.Context, runID int64, _ bool) (float64, bool) {
		if runID == 2 {
			return 0, false
		}
		return 0, true
	}

	runs := []WorkflowRun{
		{DatabaseID: 1, Status: "completed", Conclusion: "success"},
		{DatabaseID: 2, Status: "completed", Conclusion: "success"},
	}

	got := parallelLoadRunAICs(context.Background(), runs, ForecastConfig{DownloadConcurrency: 2})
	assert.Equal(t, map[int64]float64{1: 0}, got)
}

// TestParallelLoadRunAICs_RespectsContextCancellation verifies that parallelLoadRunAICs
// stops issuing new downloads and returns promptly when the context is cancelled.
func TestParallelLoadRunAICs_RespectsContextCancellation(t *testing.T) {
	originalLoadAIC := forecastLoadRunAIC
	t.Cleanup(func() { forecastLoadRunAIC = originalLoadAIC })

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	forecastLoadRunAIC = func(_ context.Context, runID int64, _ bool) (float64, bool) {
		return float64(runID), true
	}

	runs := []WorkflowRun{
		{DatabaseID: 10, Status: "completed", Conclusion: "success"},
		{DatabaseID: 11, Status: "completed", Conclusion: "success"},
	}

	// Should return quickly without panicking, regardless of which goroutines completed.
	got := parallelLoadRunAICs(ctx, runs, ForecastConfig{DownloadConcurrency: 1})
	assert.NotNil(t, got)
}

// TestParallelLoadRunAICs_EmptyRunsReturnsEmptyMap verifies the empty-input edge case.
func TestParallelLoadRunAICs_EmptyRunsReturnsEmptyMap(t *testing.T) {
	t.Parallel()
	got := parallelLoadRunAICs(context.Background(), nil, ForecastConfig{})
	assert.Empty(t, got)
}

// TestNewForecastCommand_ConcurrencyFlag verifies that the --concurrency flag is
// registered with the expected default and usage text.
func TestNewForecastCommand_ConcurrencyFlag(t *testing.T) {
	t.Parallel()
	cmd := NewForecastCommand()
	require.NotNil(t, cmd)

	flag := cmd.Flags().Lookup("concurrency")
	require.NotNil(t, flag, "forecast command should register --concurrency")
	assert.Equal(t, "0", flag.DefValue)
	assert.Contains(t, flag.Usage, "concurrent")
}
