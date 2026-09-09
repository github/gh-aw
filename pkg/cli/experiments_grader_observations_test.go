//go:build !integration

package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/github/gh-aw/pkg/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func graderArtifact(results ...graderArtifactResult) *graderResultsArtifact {
	return &graderResultsArtifact{Version: 1, Results: results}
}

func graderResult(id, status, value string) graderArtifactResult {
	return graderArtifactResult{ID: id, Status: status, Value: json.RawMessage(value)}
}

func TestResolveGraderMetricReferences(t *testing.T) {
	t.Parallel()
	enabled := true
	disabled := false
	graders := &workflow.GradersConfig{Graders: map[string]*workflow.GraderDefinition{
		"score":    {ID: "score", Enabled: &enabled},
		"disabled": {ID: "disabled", Enabled: &disabled},
	}}

	t.Run("normalizes canonical and dotted forms", func(t *testing.T) {
		t.Parallel()
		refs, err := resolveGraderMetricReferences(map[string]*workflow.ExperimentConfig{
			"canonical": {Metric: "grader:score"},
			"dotted":    {Metric: "graders.score.value"},
		}, graders)
		require.NoError(t, err)
		assert.Equal(t, map[string]string{"canonical": "score", "dotted": "score"}, refs)
	})

	t.Run("rejects missing grader", func(t *testing.T) {
		t.Parallel()
		_, err := resolveGraderMetricReferences(map[string]*workflow.ExperimentConfig{
			"test": {Metric: "grader:missing"},
		}, graders)
		require.EqualError(t, err, `experiments.test.metric: references unknown grader "missing"`)
	})

	t.Run("rejects disabled grader", func(t *testing.T) {
		t.Parallel()
		_, err := resolveGraderMetricReferences(map[string]*workflow.ExperimentConfig{
			"test": {Metric: "grader:disabled"},
		}, graders)
		require.EqualError(t, err, `experiments.test.metric: references disabled grader "disabled"`)
	})

	t.Run("rejects malformed reference", func(t *testing.T) {
		t.Parallel()
		_, err := resolveGraderMetricReferences(map[string]*workflow.ExperimentConfig{
			"test": {Metric: "grader:"},
		}, graders)
		require.ErrorContains(t, err, "expected grader reference format")
	})

	t.Run("rejects unsupported dotted suffix", func(t *testing.T) {
		t.Parallel()
		_, err := resolveGraderMetricReferences(map[string]*workflow.ExperimentConfig{
			"test": {Metric: "graders.score.passed"},
		}, graders)
		require.ErrorContains(t, err, "expected grader reference format")
	})

	t.Run("rejects misspelled value suffix", func(t *testing.T) {
		t.Parallel()
		_, err := resolveGraderMetricReferences(map[string]*workflow.ExperimentConfig{
			"test": {Metric: "graders.score.vaule"},
		}, graders)
		require.ErrorContains(t, err, "expected grader reference format")
	})
}

func TestExtractGraderObservation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		artifact   *graderResultsArtifact
		wantValue  float64
		wantBinary bool
		wantReason string
	}{
		{
			name:      "valid numeric pass",
			artifact:  graderArtifact(graderResult("score", "pass", "0.82")),
			wantValue: 0.82,
		},
		{
			name:      "threshold fail remains a valid measurement",
			artifact:  graderArtifact(graderResult("score", "fail", "0.2")),
			wantValue: 0.2,
		},
		{
			name:       "valid boolean",
			artifact:   graderArtifact(graderResult("score", "pass", "true")),
			wantValue:  1,
			wantBinary: true,
		},
		{
			name:       "missing grader",
			artifact:   graderArtifact(graderResult("other", "pass", "1")),
			wantReason: exclusionGraderMissing,
		},
		{
			name:       "grader execution failed",
			artifact:   graderArtifact(graderResult("score", "error", "null")),
			wantReason: exclusionGraderFailed,
		},
		{
			name:       "grader unavailable",
			artifact:   graderArtifact(graderResult("score", "unavailable", "null")),
			wantReason: exclusionGraderUnavailable,
		},
		{
			name:       "unsupported string value",
			artifact:   graderArtifact(graderResult("score", "pass", `"high"`)),
			wantReason: exclusionInvalidValue,
		},
		{
			name:       "null value",
			artifact:   graderArtifact(graderResult("score", "pass", "null")),
			wantReason: exclusionInvalidValue,
		},
		{
			name: "duplicate grader result",
			artifact: graderArtifact(
				graderResult("score", "pass", "1"),
				graderResult("score", "pass", "2"),
			),
			wantReason: exclusionMalformedArtifact,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			observation, reason := extractGraderObservation(tt.artifact, "42", "candidate", "score")
			assert.Equal(t, tt.wantReason, reason)
			if reason == "" {
				assert.Equal(t, "42", observation.RunID)
				assert.Equal(t, "candidate", observation.Variant)
				assert.Equal(t, "score", observation.GraderID)
				assert.InDelta(t, tt.wantValue, observation.Value, 0.0001)
				assert.Equal(t, tt.wantBinary, observation.Binary)
			}
		})
	}
}

func TestReadGraderResultsArtifact(t *testing.T) {
	t.Parallel()
	t.Run("loads unified agent grader results", func(t *testing.T) {
		t.Parallel()
		runDir := t.TempDir()
		resultsDir := filepath.Join(runDir, "agent", "graders")
		require.NoError(t, os.MkdirAll(resultsDir, 0o755))
		require.NoError(t, os.WriteFile(
			filepath.Join(resultsDir, "grader_results.json"),
			[]byte(`{"version":1,"results":[{"id":"score","status":"pass","value":0.75}]}`),
			0o644,
		))

		data := readGraderResultsArtifact(runDir)
		require.NotNil(t, data.Artifact)
		assert.Empty(t, data.ExclusionReason)
	})

	t.Run("loads fallback artifact grader results", func(t *testing.T) {
		t.Parallel()
		runDir := t.TempDir()
		resultsDir := filepath.Join(runDir, "agent-output-fallback", "agent", "graders")
		require.NoError(t, os.MkdirAll(resultsDir, 0o755))
		require.NoError(t, os.WriteFile(
			filepath.Join(resultsDir, "grader_results.json"),
			[]byte(`{"version":1,"results":[]}`),
			0o644,
		))
		require.NoError(t, flattenAgentOutputFallbackArtifact(runDir, false))

		data := readGraderResultsArtifact(runDir)
		require.NotNil(t, data.Artifact)
		assert.Empty(t, data.ExclusionReason)
	})

	t.Run("missing artifact is excluded", func(t *testing.T) {
		t.Parallel()
		data := readGraderResultsArtifact(t.TempDir())
		assert.Equal(t, exclusionArtifactUnavailable, data.ExclusionReason)
	})

	t.Run("malformed artifact is excluded", func(t *testing.T) {
		t.Parallel()
		runDir := t.TempDir()
		resultsDir := filepath.Join(runDir, "graders")
		require.NoError(t, os.MkdirAll(resultsDir, 0o755))
		require.NoError(t, os.WriteFile(
			filepath.Join(resultsDir, "grader_results.json"),
			[]byte(`{"version":1,"results":`),
			0o644,
		))

		data := readGraderResultsArtifact(runDir)
		assert.Equal(t, exclusionMalformedArtifact, data.ExclusionReason)
	})

	t.Run("oversized artifact is excluded as too large", func(t *testing.T) {
		t.Parallel()
		runDir := t.TempDir()
		resultsDir := filepath.Join(runDir, "graders")
		require.NoError(t, os.MkdirAll(resultsDir, 0o755))
		oversized := make([]byte, maxGraderResultsBytes+1)
		require.NoError(t, os.WriteFile(filepath.Join(resultsDir, "grader_results.json"), oversized, 0o644))

		data := readGraderResultsArtifact(runDir)
		assert.Equal(t, exclusionArtifactTooLarge, data.ExclusionReason)
	})
}

func TestBuildGraderMetricObservationSetsJoinsAssignments(t *testing.T) {
	t.Parallel()
	experiments := []ExperimentVariantStats{{
		Name:     "prompt",
		Variants: map[string]int{"control": 3, "candidate": 3},
		Total:    6,
	}}
	runs := []ExperimentRunRecord{
		{RunID: "1", Assignments: map[string]string{"prompt": "control"}},
		{RunID: "2", Assignments: map[string]string{"prompt": "candidate"}},
		{RunID: "3", Assignments: map[string]string{"prompt": "candidate"}},
		{RunID: "2", Assignments: map[string]string{"prompt": "candidate"}},
	}
	runData := map[string]graderRunData{
		"1": {Artifact: graderArtifact(graderResult("score", "pass", "0.6"))},
		"2": {ExclusionReason: exclusionRunCancelled},
		"3": {ExclusionReason: exclusionArtifactUnavailable},
	}

	sets := buildGraderMetricObservationSets(
		experiments,
		runs,
		map[string]string{"prompt": "score"},
		runData,
		nil,
	)
	set := sets["prompt"]
	require.NotNil(t, set)
	require.Len(t, set.ByVariant["control"], 1)
	assert.Equal(t, "1", set.ByVariant["control"][0].RunID)
	assert.Empty(t, set.ByVariant["candidate"])

	controlExclusions := exclusionsByReason(set.Exclusions["control"])
	assert.Equal(t, 2, controlExclusions[exclusionAssignmentHistoryUnavailable].Count)
	candidateExclusions := exclusionsByReason(set.Exclusions["candidate"])
	assert.Equal(t, 1, candidateExclusions[exclusionRunCancelled].Count)
	assert.Equal(t, []string{"2"}, candidateExclusions[exclusionRunCancelled].RunIDs)
	assert.Equal(t, 1, candidateExclusions[exclusionArtifactUnavailable].Count)
	assert.Equal(t, 1, candidateExclusions[exclusionDuplicateAssignment].Count)
}

func TestComputeGraderBackedExperimentAnalysis(t *testing.T) {
	t.Parallel()
	set := &graderMetricObservationSet{
		MetricID: "trajectory-efficiency",
		ByVariant: map[string][]GraderMetricObservation{
			"control": {
				{RunID: "1", Variant: "control", Value: 0.6},
				{RunID: "2", Variant: "control", Value: 0.7},
				{RunID: "3", Variant: "control", Value: 0.8},
			},
			"candidate": {
				{RunID: "4", Variant: "candidate", Value: 0.8},
				{RunID: "5", Variant: "candidate", Value: 0.9},
				{RunID: "6", Variant: "candidate", Value: 1.0},
			},
		},
		Exclusions: map[string][]ExcludedObservationSummary{},
	}
	cfg := &workflow.ExperimentConfig{
		Variants:     []string{"control", "candidate"},
		Metric:       "grader:trajectory-efficiency",
		MinSamples:   3,
		AnalysisType: "mann_whitney",
	}
	exp := ExperimentVariantStats{
		Name:     "prompt",
		Variants: map[string]int{"control": 3, "candidate": 3},
		Total:    6,
	}

	analysis := computeExperimentAnalysisWithObservationBundle(exp, cfg, nil, nil, set, nil)
	assert.Equal(t, "trajectory-efficiency", analysis.MetricGraderID)
	assert.Equal(t, "continuous", analysis.MetricType)
	assert.Equal(t, "READY_FOR_ANALYSIS", analysis.Recommendation)
	require.Len(t, analysis.Variants, 2)
	variants := variantsByName(analysis.Variants)
	assert.Equal(t, 3, variants["control"].ObservationCount)
	assert.InDelta(t, 0.7, *variants["control"].Mean, 0.0001)
	assert.InDelta(t, 0.9, *variants["candidate"].Mean, 0.0001)
	require.Len(t, variants["control"].Observations, 3)
	assert.Equal(t, "1", variants["control"].Observations[0].RunID)
	require.Len(t, variants["candidate"].Observations, 3)
	assert.Equal(t, "4", variants["candidate"].Observations[0].RunID)
	require.Len(t, analysis.Comparisons, 1)
	assert.Equal(t, "control", analysis.Comparisons[0].ControlVariant)
	assert.Equal(t, "candidate", analysis.Comparisons[0].Variant)
	assert.InDelta(t, 0.2, analysis.Comparisons[0].Delta, 0.0001)
	assert.NotNil(t, analysis.Comparisons[0].PValue)

	jsonBytes, err := json.Marshal(analysis)
	require.NoError(t, err)
	var jsonResult map[string]any
	require.NoError(t, json.Unmarshal(jsonBytes, &jsonResult))
	assert.Equal(t, "trajectory-efficiency", jsonResult["metric_grader_id"])
	assert.Equal(t, "continuous", jsonResult["metric_type"])
	assert.Contains(t, jsonResult, "comparisons")
	variantsJSON, ok := jsonResult["variants"].([]any)
	require.True(t, ok)
	firstVariant, ok := variantsJSON[0].(map[string]any)
	require.True(t, ok)
	assert.Contains(t, firstVariant, "observations")
}

func TestComputeGraderBackedExperimentDecision(t *testing.T) {
	t.Parallel()
	set := &graderMetricObservationSet{
		MetricID:  "trajectory-efficiency",
		Direction: "higher_is_better",
		ByVariant: map[string][]GraderMetricObservation{
			"control": {
				{RunID: "1", Variant: "control", Value: 0.48},
				{RunID: "2", Variant: "control", Value: 0.50},
				{RunID: "3", Variant: "control", Value: 0.52},
			},
			"candidate": {
				{RunID: "4", Variant: "candidate", Value: 0.78},
				{RunID: "5", Variant: "candidate", Value: 0.80},
				{RunID: "6", Variant: "candidate", Value: 0.82},
			},
		},
		Exclusions: map[string][]ExcludedObservationSummary{},
	}
	cfg := &workflow.ExperimentConfig{
		Variants:     []string{"control", "candidate"},
		Metric:       "grader:trajectory-efficiency",
		MinSamples:   3,
		AnalysisType: "t_test",
		Decision:     &workflow.ExperimentDecisionConfig{MinimumEffect: 0.1},
	}
	exp := ExperimentVariantStats{
		Name:     "prompt",
		Variants: map[string]int{"control": 3, "candidate": 3},
		Total:    6,
	}

	analysis := computeExperimentAnalysisWithObservationBundle(exp, cfg, nil, nil, set, nil)

	assert.Equal(t, ExperimentReadinessReady, analysis.Readiness)
	assert.Equal(t, ExperimentDecisionPromote, analysis.Decision)
	assert.Equal(t, ExperimentDecisionReasonCandidateImproved, analysis.ReasonCode)
	assert.Equal(t, "control", analysis.Control)
	assert.Equal(t, "candidate", analysis.Candidate)
	require.NotNil(t, analysis.Effect)
	assert.InDelta(t, 0.3, analysis.Effect.NormalizedAbsolute, 0.0001)
	assert.Equal(t, map[string]int{"control": 3, "candidate": 3}, analysis.Samples)
}

func TestWelchTTestZeroVarianceFallback(t *testing.T) {
	t.Parallel()

	pValue, err := welchTTest([]float64{0.5, 0.5, 0.5}, []float64{0.8, 0.8, 0.8})

	require.NoError(t, err)
	assert.Zero(t, pValue)
}

func TestGraderReadinessUsesValidObservations(t *testing.T) {
	t.Parallel()
	observations := func(variant string, count int) []GraderMetricObservation {
		result := make([]GraderMetricObservation, count)
		for i := range result {
			result[i] = GraderMetricObservation{Variant: variant, Value: float64(i)}
		}
		return result
	}
	set := &graderMetricObservationSet{
		MetricID: "score",
		ByVariant: map[string][]GraderMetricObservation{
			"control":   observations("control", 7),
			"candidate": observations("candidate", 8),
		},
		Exclusions: map[string][]ExcludedObservationSummary{
			"control": {{Reason: exclusionArtifactUnavailable, Count: 3}},
		},
	}
	cfg := &workflow.ExperimentConfig{
		Variants:   []string{"control", "candidate"},
		Metric:     "grader:score",
		MinSamples: 8,
	}
	exp := ExperimentVariantStats{
		Name:     "prompt",
		Variants: map[string]int{"control": 10, "candidate": 10},
		Total:    20,
	}

	analysis := computeExperimentAnalysisWithObservationBundle(exp, cfg, nil, nil, set, nil)
	assert.Equal(t, "EXTEND", analysis.Recommendation)
	variants := variantsByName(analysis.Variants)
	assert.True(t, variants["control"].BelowMinSamples)
	assert.False(t, variants["candidate"].BelowMinSamples)
	assert.Equal(t, 10, variants["control"].Count)
	assert.Equal(t, 7, variants["control"].ObservationCount)
}

type fakeGraderRunSource struct {
	mu    sync.Mutex
	loads map[string]int
	data  map[string]graderRunData
}

func (s *fakeGraderRunSource) Load(_ context.Context, runID string) graderRunData {
	s.mu.Lock()
	s.loads[runID]++
	s.mu.Unlock()
	return s.data[runID]
}

func TestLoadGraderRunDataDeduplicatesRunIDs(t *testing.T) {
	t.Parallel()
	source := &fakeGraderRunSource{
		loads: map[string]int{},
		data:  map[string]graderRunData{"1": {Artifact: graderArtifact()}},
	}

	runs := []ExperimentRunRecord{
		{RunID: "1", Assignments: map[string]string{"prompt": "control"}},
		{RunID: "1", Assignments: map[string]string{"prompt": "control"}},
	}

	result := loadGraderRunData(context.Background(), runs, map[string]struct{}{"prompt": {}}, source)
	require.Contains(t, result, "1")
	assert.Equal(t, 1, source.loads["1"])
}

func TestGraderStatisticalMethods(t *testing.T) {
	t.Parallel()
	t.Run("welch t test", func(t *testing.T) {
		t.Parallel()
		pValue, err := welchTTest([]float64{0.6, 0.7, 0.8}, []float64{0.8, 0.9, 1.0})
		require.NoError(t, err)
		assert.GreaterOrEqual(t, pValue, 0.0)
		assert.LessOrEqual(t, pValue, 1.0)
	})
	t.Run("proportion test", func(t *testing.T) {
		t.Parallel()
		pValue, err := twoProportionTest([]float64{0, 0, 1, 0}, []float64{1, 1, 1, 0})
		require.NoError(t, err)
		assert.GreaterOrEqual(t, pValue, 0.0)
		assert.LessOrEqual(t, pValue, 1.0)
	})
	t.Run("bayesian ab", func(t *testing.T) {
		t.Parallel()
		probability, err := betaBinomialProbability([]float64{0, 0, 1}, []float64{1, 1, 1})
		require.NoError(t, err)
		assert.Greater(t, probability, 0.5)
		assert.LessOrEqual(t, probability, 1.0)
	})
	t.Run("proportion rejects continuous values", func(t *testing.T) {
		t.Parallel()
		_, err := twoProportionTest([]float64{0.2}, []float64{0.8})
		require.ErrorContains(t, err, "0/1")
	})
}

func TestComputeGraderMetricComparisonsBayesianDirection(t *testing.T) {
	t.Parallel()
	cfg := &workflow.ExperimentConfig{
		Variants:     []string{"control", "candidate"},
		AnalysisType: "bayesian_ab",
	}
	byVariant := map[string][]GraderMetricObservation{
		"control":   {{Value: 0}, {Value: 0}, {Value: 1}},
		"candidate": {{Value: 1}, {Value: 1}, {Value: 1}},
	}

	higherIsBetter := &graderMetricObservationSet{Direction: "higher_is_better", ByVariant: byVariant}
	comparisons := computeGraderMetricComparisons(cfg, higherIsBetter, []string{"control", "candidate"}, "binary")
	require.Len(t, comparisons, 1)
	require.NotNil(t, comparisons[0].ProbabilitySuperiority)
	assert.Greater(t, *comparisons[0].ProbabilitySuperiority, 0.5)

	lowerIsBetter := &graderMetricObservationSet{Direction: "lower_is_better", ByVariant: byVariant}
	invertedComparisons := computeGraderMetricComparisons(cfg, lowerIsBetter, []string{"control", "candidate"}, "binary")
	require.Len(t, invertedComparisons, 1)
	require.NotNil(t, invertedComparisons[0].ProbabilitySuperiority)
	assert.InDelta(t, 1-*comparisons[0].ProbabilitySuperiority, *invertedComparisons[0].ProbabilitySuperiority, 0.0001)
	assert.Less(t, *invertedComparisons[0].ProbabilitySuperiority, 0.5)
}

func exclusionsByReason(exclusions []ExcludedObservationSummary) map[string]ExcludedObservationSummary {
	result := make(map[string]ExcludedObservationSummary, len(exclusions))
	for _, exclusion := range exclusions {
		result[exclusion.Reason] = exclusion
	}
	return result
}

func variantsByName(variants []VariantAnalysis) map[string]VariantAnalysis {
	result := make(map[string]VariantAnalysis, len(variants))
	for _, variant := range variants {
		result[variant.Name] = variant
	}
	return result
}
