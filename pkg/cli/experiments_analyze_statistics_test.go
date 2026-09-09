//go:build !integration

package cli

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/github/gh-aw/pkg/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestChiSquarePValue verifies the Wilson-Hilferty approximation against known values.
func TestChiSquarePValue(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		chi2    float64
		df      int
		wantMin float64
		wantMax float64
	}{
		{
			// χ²(1) = 3.841 corresponds to the 0.05 critical value — p ≈ 0.050
			name:    "chi2=3.841 df=1 critical value for alpha=0.05",
			chi2:    3.841,
			df:      1,
			wantMin: 0.04,
			wantMax: 0.06,
		},
		{
			// χ²(1) = 0 → p = 1 (perfect fit)
			name:    "chi2=0 df=1 returns 1.0",
			chi2:    0,
			df:      1,
			wantMin: 1.0,
			wantMax: 1.0,
		},
		{
			// Very large chi2 → p near 0
			name:    "very large chi2 df=2 returns near-zero p",
			chi2:    50.0,
			df:      2,
			wantMin: 0.0,
			wantMax: 0.0001,
		},
		{
			// χ²(2) = 5.991 is the 0.05 critical value — p ≈ 0.050
			name:    "chi2=5.991 df=2 critical value for alpha=0.05",
			chi2:    5.991,
			df:      2,
			wantMin: 0.04,
			wantMax: 0.06,
		},
		{
			// Degenerate: negative chi2
			name:    "negative chi2 returns 1.0",
			chi2:    -1.0,
			df:      1,
			wantMin: 1.0,
			wantMax: 1.0,
		},
		{
			// Degenerate: df=0
			name:    "df=0 returns 1.0",
			chi2:    3.0,
			df:      0,
			wantMin: 1.0,
			wantMax: 1.0,
		},
		{
			// Small chi2 (df=1): p should be large (balanced)
			name:    "chi2=0.5 df=1 returns large p (balanced)",
			chi2:    0.5,
			df:      1,
			wantMin: 0.4,
			wantMax: 0.6,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := chiSquarePValue(tt.chi2, tt.df)
			assert.True(t, got >= tt.wantMin && got <= tt.wantMax,
				"chiSquarePValue(%.3f, %d) = %.6f, want [%.4f, %.4f]",
				tt.chi2, tt.df, got, tt.wantMin, tt.wantMax)
		})
	}
}

// TestExpectedProportions verifies equal and weighted proportion computations.
func TestExpectedProportions(t *testing.T) {
	t.Parallel()
	t.Run("equal proportions when no config", func(t *testing.T) {
		names := []string{"A", "B", "C"}
		got := expectedProportions(names, nil)
		require.Len(t, got, 3, "should return one proportion per variant")
		for _, p := range got {
			assert.InDelta(t, 1.0/3.0, p, 0.0001, "each proportion should be 1/3")
		}
		total := got[0] + got[1] + got[2]
		assert.InDelta(t, 1.0, total, 0.0001, "proportions should sum to 1.0")
	})

	t.Run("equal proportions when config has no weights", func(t *testing.T) {
		names := []string{"concise", "detailed"}
		cfg := &workflow.ExperimentConfig{
			Variants: []string{"concise", "detailed"},
		}
		got := expectedProportions(names, cfg)
		require.Len(t, got, 2, "should return two proportions")
		assert.InDelta(t, 0.5, got[0], 0.0001, "concise should be 0.5")
		assert.InDelta(t, 0.5, got[1], 0.0001, "detailed should be 0.5")
	})

	t.Run("weighted proportions from config", func(t *testing.T) {
		names := []string{"concise", "detailed"}
		cfg := &workflow.ExperimentConfig{
			Variants: []string{"concise", "detailed"},
			Weight:   []int{70, 30},
		}
		got := expectedProportions(names, cfg)
		require.Len(t, got, 2, "should return two proportions")
		assert.InDelta(t, 0.70, got[0], 0.0001, "concise should be 0.70")
		assert.InDelta(t, 0.30, got[1], 0.0001, "detailed should be 0.30")
	})

	t.Run("equal proportions when weight length mismatches variants", func(t *testing.T) {
		names := []string{"A", "B", "C"}
		cfg := &workflow.ExperimentConfig{
			Variants: []string{"A", "B", "C"},
			Weight:   []int{50, 50}, // wrong length
		}
		got := expectedProportions(names, cfg)
		require.Len(t, got, 3, "should return three proportions")
		for _, p := range got {
			assert.InDelta(t, 1.0/3.0, p, 0.0001, "should fall back to equal proportions")
		}
	})

	t.Run("equal proportions when all weights are zero", func(t *testing.T) {
		names := []string{"A", "B"}
		cfg := &workflow.ExperimentConfig{
			Variants: []string{"A", "B"},
			Weight:   []int{0, 0},
		}
		got := expectedProportions(names, cfg)
		require.Len(t, got, 2, "should return two proportions")
		for _, p := range got {
			assert.InDelta(t, 0.5, p, 0.0001, "should fall back to equal proportions")
		}
	})

	t.Run("empty variant list returns nil", func(t *testing.T) {
		got := expectedProportions(nil, nil)
		assert.Nil(t, got, "nil input should return nil")
	})

	t.Run("sorted variant names matched correctly to weights", func(t *testing.T) {
		// Config declares weights in declaration order; sorted names are alphabetical.
		names := []string{"alpha", "beta", "gamma"}
		cfg := &workflow.ExperimentConfig{
			Variants: []string{"gamma", "alpha", "beta"},
			Weight:   []int{10, 60, 30},
		}
		got := expectedProportions(names, cfg)
		require.Len(t, got, 3, "should return three proportions")
		// alpha has weight 60, beta 30, gamma 10 → total 100
		assert.InDelta(t, 0.60, got[0], 0.0001, "alpha should be 0.60")
		assert.InDelta(t, 0.30, got[1], 0.0001, "beta should be 0.30")
		assert.InDelta(t, 0.10, got[2], 0.0001, "gamma should be 0.10")
	})
}

func TestExperimentVariantCounts(t *testing.T) {
	t.Parallel()

	t.Run("includes declared variants with zero counts", func(t *testing.T) {
		exp := ExperimentVariantStats{
			Variants: map[string]int{"control": 3},
		}
		cfg := &workflow.ExperimentConfig{
			Variants: []string{"control", "candidate"},
		}

		got := experimentVariantCounts(exp, cfg, true)

		assert.Equal(t, map[string]int{"control": 3, "candidate": 0}, got)
	})

	t.Run("returns observed variants when declared variants are excluded", func(t *testing.T) {
		exp := ExperimentVariantStats{
			Variants: map[string]int{"control": 3},
		}
		cfg := &workflow.ExperimentConfig{
			Variants: []string{"control", "candidate"},
		}

		got := experimentVariantCounts(exp, cfg, false)

		assert.Equal(t, exp.Variants, got)
	})

	t.Run("drops stale variant keys no longer declared", func(t *testing.T) {
		exp := ExperimentVariantStats{
			Variants: map[string]int{"control": 3, "candidate": 5, "legacy-variant": 1},
		}
		cfg := &workflow.ExperimentConfig{
			Variants: []string{"control", "candidate"},
		}

		got := experimentVariantCounts(exp, cfg, false)

		assert.Equal(t, map[string]int{"control": 3, "candidate": 5}, got, "stale variant no longer in cfg.Variants should be dropped")
	})

	t.Run("drops stale variant keys and adds missing declared variants with zero counts", func(t *testing.T) {
		exp := ExperimentVariantStats{
			Variants: map[string]int{"control": 3, "legacy-variant": 1},
		}
		cfg := &workflow.ExperimentConfig{
			Variants: []string{"control", "candidate"},
		}

		got := experimentVariantCounts(exp, cfg, true)

		assert.Equal(t, map[string]int{"control": 3, "candidate": 0}, got)
	})

	t.Run("returns observed variants when config is nil", func(t *testing.T) {
		exp := ExperimentVariantStats{
			Variants: map[string]int{"control": 3},
		}

		got := experimentVariantCounts(exp, nil, true)

		assert.Equal(t, exp.Variants, got)
	})
}

// TestComputeExperimentAnalysis verifies the end-to-end statistical computation.
func TestComputeExperimentAnalysis(t *testing.T) {
	t.Parallel()
	t.Run("balanced two-variant experiment below min_samples", func(t *testing.T) {
		exp := ExperimentVariantStats{
			Name:     "style",
			Variants: map[string]int{"concise": 5, "detailed": 5},
			Total:    10,
		}
		a := computeExperimentAnalysisWithObservationBundle(exp, nil, nil, nil, nil, nil)

		assert.Equal(t, "style", a.ExperimentName, "experiment name")
		assert.Equal(t, defaultMinSamples, a.MinSamples, "default min_samples")
		assert.Equal(t, 10, a.TotalRuns, "total runs")
		assert.Len(t, a.Variants, 2, "two variants")
		assert.Equal(t, "EXTEND", a.Recommendation, "below min_samples → EXTEND")
		assert.True(t, a.IsBalanced, "perfectly balanced → is_balanced")
		assert.Equal(t, 1, a.DegreesOfFreedom, "df=1 for two variants")
		assert.Greater(t, a.PValue, balanceSignificanceThreshold, "perfect balance p > 0.05")
		assert.InDelta(t, 0.0, a.BonferroniAlpha, 1e-10, "no Bonferroni for K=2")
	})

	t.Run("uses min_samples from config", func(t *testing.T) {
		exp := ExperimentVariantStats{
			Name:     "tone",
			Variants: map[string]int{"formal": 8, "casual": 8},
			Total:    16,
		}
		cfg := &workflow.ExperimentConfig{
			Variants:   []string{"formal", "casual"},
			MinSamples: 5,
		}
		a := computeExperimentAnalysisWithObservationBundle(exp, cfg, nil, nil, nil, nil)
		assert.Equal(t, 5, a.MinSamples, "min_samples from config")
		assert.Equal(t, ExperimentReadinessReady, a.Readiness, "count >= min_samples → READY")
		assert.Equal(t, "READY_FOR_ANALYSIS", a.Recommendation, "count >= min_samples → READY")
		for _, v := range a.Variants {
			assert.False(t, v.BelowMinSamples, "no variant should be below min_samples=5")
		}
	})

	t.Run("hypothesis and analysis_type from config", func(t *testing.T) {
		exp := ExperimentVariantStats{
			Name:     "prompt",
			Variants: map[string]int{"short": 20, "long": 20},
			Total:    40,
		}
		cfg := &workflow.ExperimentConfig{
			Variants:     []string{"short", "long"},
			Hypothesis:   "H0: no change. H1: short reduces tokens by 15%.",
			AnalysisType: "t_test",
			MinSamples:   20,
		}
		a := computeExperimentAnalysisWithObservationBundle(exp, cfg, nil, nil, nil, nil)
		assert.Equal(t, "H0: no change. H1: short reduces tokens by 15%.", a.Hypothesis, "hypothesis from config")
		assert.Equal(t, "t_test", a.AnalysisType, "analysis_type from config")
		assert.Equal(t, "READY_FOR_ANALYSIS", a.Recommendation, "count >= min_samples → READY")
	})

	t.Run("guardrails propagated from config", func(t *testing.T) {
		exp := ExperimentVariantStats{
			Name:     "quality",
			Variants: map[string]int{"A": 3, "B": 2},
			Total:    5,
		}
		cfg := &workflow.ExperimentConfig{
			Variants: []string{"A", "B"},
			GuardrailMetrics: []workflow.GuardrailMetric{
				{Name: "success_rate", Threshold: ">=0.95"},
				{Name: "empty_output_rate", Threshold: "==0"},
			},
		}
		a := computeExperimentAnalysisWithObservationBundle(exp, cfg, nil, nil, nil, nil)
		require.Len(t, a.Guardrails, 2, "should have two guardrails")
		assert.Equal(t, "success_rate", a.Guardrails[0].Name, "first guardrail name")
		assert.Equal(t, ">=0.95", a.Guardrails[0].Threshold, "first guardrail threshold")
		assert.Equal(t, "empty_output_rate", a.Guardrails[1].Name, "second guardrail name")
	})

	t.Run("Bonferroni alpha for K=3 variants (§11.3)", func(t *testing.T) {
		exp := ExperimentVariantStats{
			Name:     "multi",
			Variants: map[string]int{"A": 5, "B": 5, "C": 5},
			Total:    15,
		}
		a := computeExperimentAnalysisWithObservationBundle(exp, nil, nil, nil, nil, nil)
		assert.Len(t, a.Variants, 3, "three variants")
		// α_adjusted = 0.05 / (K − 1) = 0.05 / 2 = 0.025
		assert.InDelta(t, 0.025, a.BonferroniAlpha, 0.0001, "Bonferroni alpha for K=3")
		assert.Equal(t, 2, a.DegreesOfFreedom, "df=2 for K=3")
	})

	t.Run("Bonferroni alpha for K=4 variants", func(t *testing.T) {
		exp := ExperimentVariantStats{
			Name:     "four",
			Variants: map[string]int{"A": 5, "B": 5, "C": 5, "D": 5},
			Total:    20,
		}
		a := computeExperimentAnalysisWithObservationBundle(exp, nil, nil, nil, nil, nil)
		// α_adjusted = 0.05 / (4 − 1) = 0.05 / 3 ≈ 0.0167
		assert.InDelta(t, 0.05/3.0, a.BonferroniAlpha, 0.0001, "Bonferroni alpha for K=4")
	})

	t.Run("unbalanced distribution detected", func(t *testing.T) {
		// 19:1 split on 20 runs — extreme imbalance, χ² should be large.
		exp := ExperimentVariantStats{
			Name:     "skewed",
			Variants: map[string]int{"A": 19, "B": 1},
			Total:    20,
		}
		a := computeExperimentAnalysisWithObservationBundle(exp, nil, nil, nil, nil, nil)
		assert.False(t, a.IsBalanced, "extreme imbalance should not be balanced")
		assert.Less(t, a.PValue, balanceSignificanceThreshold, "p < 0.05 for extreme imbalance")
	})

	t.Run("stale variant labels excluded from balance test when reconciled against config", func(t *testing.T) {
		// Regression test for github/gh-aw#58489: legacy variant labels left over from a
		// renamed variant set (e.g. "small-agent"/"agent") must not be counted toward the
		// chi-square balance test once the workflow's frontmatter no longer declares them.
		exp := ExperimentVariantStats{
			Name:     "model_size",
			Variants: map[string]int{"gpt-5.4": 28, "gpt-5.4-mini": 18, "small-agent": 1, "agent": 1},
			Total:    48,
		}
		cfg := &workflow.ExperimentConfig{
			Variants: []string{"gpt-5.4", "gpt-5.4-mini"},
		}
		a := computeExperimentAnalysisWithObservationBundle(exp, cfg, nil, nil, nil, nil)
		assert.True(t, a.IsBalanced, "28/18 split should be balanced once stale labels are excluded")
		assert.GreaterOrEqual(t, a.PValue, balanceSignificanceThreshold)
		require.Len(t, a.Variants, 2, "stale variant keys should not appear in the analysis")
		for _, v := range a.Variants {
			assert.NotEqual(t, "small-agent", v.Name)
			assert.NotEqual(t, "agent", v.Name)
		}
	})

	t.Run("empty experiment returns EXTEND with zero total", func(t *testing.T) {
		exp := ExperimentVariantStats{
			Name:     "empty",
			Variants: map[string]int{"A": 0, "B": 0},
			Total:    0,
		}
		a := computeExperimentAnalysisWithObservationBundle(exp, nil, nil, nil, nil, nil)
		assert.Equal(t, "EXTEND", a.Recommendation, "zero runs → EXTEND")
		assert.True(t, a.IsBalanced, "insufficient data → default to balanced")
		assert.InDelta(t, 0.0, a.ChiSquare, 1e-10, "no chi-square for zero total")
	})

	t.Run("variants sorted alphabetically", func(t *testing.T) {
		exp := ExperimentVariantStats{
			Name:     "order",
			Variants: map[string]int{"z_last": 3, "a_first": 7},
			Total:    10,
		}
		a := computeExperimentAnalysisWithObservationBundle(exp, nil, nil, nil, nil, nil)
		require.Len(t, a.Variants, 2, "two variants")
		assert.Equal(t, "a_first", a.Variants[0].Name, "first variant alphabetically")
		assert.Equal(t, "z_last", a.Variants[1].Name, "second variant alphabetically")
	})

	t.Run("weighted proportions used for balance test", func(t *testing.T) {
		// Perfect 70/30 split — should be balanced when weights are declared.
		exp := ExperimentVariantStats{
			Name:     "weighted",
			Variants: map[string]int{"control": 70, "variant": 30},
			Total:    100,
		}
		cfg := &workflow.ExperimentConfig{
			Variants: []string{"control", "variant"},
			Weight:   []int{70, 30},
		}
		a := computeExperimentAnalysisWithObservationBundle(exp, cfg, nil, nil, nil, nil)
		assert.True(t, a.IsBalanced, "70/30 split with 70/30 weights should be balanced")
		// Expected proportions: control=0.7, variant=0.3
		require.Len(t, a.Variants, 2, "two variants")
		assert.InDelta(t, 70.0, a.Variants[0].ExpectedPct, 0.1, "control expected 70%")
		assert.InDelta(t, 30.0, a.Variants[1].ExpectedPct, 0.1, "variant expected 30%")
	})

	t.Run("continual ramp skips fixed-allocation balance check", func(t *testing.T) {
		exp := ExperimentVariantStats{
			Name:     "optimization",
			Variants: map[string]int{"candidate": 2, "control": 18},
			Total:    20,
		}
		cfg := &workflow.ExperimentConfig{
			Variants:  []string{"control", "candidate"},
			Continual: &workflow.ContinualExperimentConfig{Seed: "stable-seed", Ramp: []int{10, 25}},
		}
		a := computeExperimentAnalysisWithObservationBundle(exp, cfg, nil, nil, nil, nil)
		assert.True(t, a.IsBalanced, "changing ramp allocations should not be tested against a fixed split")
		assert.Zero(t, a.ChiSquare)
		assert.Zero(t, a.PValue)
	})

	t.Run("metric from config (plain)", func(t *testing.T) {
		exp := ExperimentVariantStats{
			Name:     "perf",
			Variants: map[string]int{"A": 5, "B": 5},
			Total:    10,
		}
		cfg := &workflow.ExperimentConfig{
			Variants: []string{"A", "B"},
			Metric:   "effective_tokens",
		}
		a := computeExperimentAnalysisWithObservationBundle(exp, cfg, nil, nil, nil, nil)
		assert.Equal(t, "effective_tokens", a.Metric, "metric should be set")
		assert.Empty(t, a.MetricQuestion, "MetricQuestion empty for plain metric")
	})

	t.Run("metric resolves eval question when evals provided", func(t *testing.T) {
		exp := ExperimentVariantStats{
			Name:     "quality",
			Variants: map[string]int{"A": 5, "B": 5},
			Total:    10,
		}
		cfg := &workflow.ExperimentConfig{
			Variants: []string{"A", "B"},
			Metric:   "evals.builds",
		}
		evals := &workflow.EvalsConfig{
			Questions: []workflow.EvalDefinition{
				{ID: "builds", Question: "Does the generated code compile?"},
				{ID: "tests", Question: "Do the tests pass?"},
			},
		}
		a := computeExperimentAnalysisWithObservationBundle(exp, cfg, evals, nil, nil, nil)
		assert.Equal(t, "evals.builds", a.Metric, "metric set to original reference")
		assert.Equal(t, "Does the generated code compile?", a.MetricQuestion, "eval question resolved")
	})

	t.Run("metric eval reference with eval: prefix resolves question", func(t *testing.T) {
		exp := ExperimentVariantStats{
			Name:     "coverage",
			Variants: map[string]int{"A": 5, "B": 5},
			Total:    10,
		}
		cfg := &workflow.ExperimentConfig{
			Variants: []string{"A", "B"},
			Metric:   "eval:tests",
		}
		evals := &workflow.EvalsConfig{
			Questions: []workflow.EvalDefinition{
				{ID: "tests", Question: "Do the tests pass?"},
			},
		}
		a := computeExperimentAnalysisWithObservationBundle(exp, cfg, evals, nil, nil, nil)
		assert.Equal(t, "eval:tests", a.Metric, "metric set to original reference")
		assert.Equal(t, "Do the tests pass?", a.MetricQuestion, "eval question resolved via eval: prefix")
	})

	t.Run("metric eval reference includes eval result summary when available", func(t *testing.T) {
		exp := ExperimentVariantStats{
			Name:     "quality",
			Variants: map[string]int{"A": 8, "B": 12},
			Total:    20,
		}
		cfg := &workflow.ExperimentConfig{
			Variants: []string{"A", "B"},
			Metric:   "evals.builds",
		}
		evals := &workflow.EvalsConfig{
			Questions: []workflow.EvalDefinition{
				{ID: "builds", Question: "Does the generated code compile?"},
			},
		}
		metricResults := map[string]MetricEvalResults{
			"builds": {
				Yes:          12,
				No:           5,
				Unknown:      3,
				Total:        20,
				LatestAnswer: "YES",
				LatestRunID:  "123456",
			},
		}
		a := computeExperimentAnalysisWithObservationBundle(exp, cfg, evals, metricResults, nil, nil)
		require.NotNil(t, a.MetricEvalResults, "eval result summary should be attached")
		assert.Equal(t, 12, a.MetricEvalResults.Yes)
		assert.Equal(t, 5, a.MetricEvalResults.No)
		assert.Equal(t, 3, a.MetricEvalResults.Unknown)
		assert.Equal(t, 20, a.MetricEvalResults.Total)
		assert.Equal(t, "YES", a.MetricEvalResults.LatestAnswer)
		assert.Equal(t, "123456", a.MetricEvalResults.LatestRunID)
	})

	t.Run("eval reference with unknown id leaves MetricQuestion empty", func(t *testing.T) {
		exp := ExperimentVariantStats{
			Name:     "x",
			Variants: map[string]int{"A": 5, "B": 5},
			Total:    10,
		}
		cfg := &workflow.ExperimentConfig{
			Variants: []string{"A", "B"},
			Metric:   "evals.unknown_id",
		}
		evals := &workflow.EvalsConfig{
			Questions: []workflow.EvalDefinition{
				{ID: "builds", Question: "Does the generated code compile?"},
			},
		}
		a := computeExperimentAnalysisWithObservationBundle(exp, cfg, evals, nil, nil, nil)
		assert.Equal(t, "evals.unknown_id", a.Metric, "metric preserved")
		assert.Empty(t, a.MetricQuestion, "MetricQuestion empty when eval id not found")
	})

	t.Run("eval reference with nil evals leaves MetricQuestion empty", func(t *testing.T) {
		exp := ExperimentVariantStats{
			Name:     "y",
			Variants: map[string]int{"A": 5, "B": 5},
			Total:    10,
		}
		cfg := &workflow.ExperimentConfig{
			Variants: []string{"A", "B"},
			Metric:   "evals.builds",
		}
		a := computeExperimentAnalysisWithObservationBundle(exp, cfg, nil, nil, nil, nil)
		assert.Equal(t, "evals.builds", a.Metric, "metric preserved")
		assert.Empty(t, a.MetricQuestion, "MetricQuestion empty when evals is nil")
	})

	t.Run("degenerate experiment still includes metric metadata", func(t *testing.T) {
		exp := ExperimentVariantStats{
			Name:     "first_run",
			Variants: map[string]int{"A": 1},
			Total:    1,
		}
		cfg := &workflow.ExperimentConfig{
			Variants: []string{"A", "B"},
			Metric:   "evals.builds",
		}
		evals := &workflow.EvalsConfig{
			Questions: []workflow.EvalDefinition{
				{ID: "builds", Question: "Does the generated code compile?"},
			},
		}
		a := computeExperimentAnalysisWithObservationBundle(exp, cfg, evals, nil, nil, nil)
		assert.Equal(t, "evals.builds", a.Metric, "metric preserved for single-variant state")
		assert.Equal(t, "Does the generated code compile?", a.MetricQuestion, "metric question resolved before degenerate return")
		assert.Equal(t, "EXTEND", a.Recommendation, "single-variant state still extends")
	})
}

// TestComputeExperimentAnalyses tests the bulk analysis function.
func TestComputeExperimentAnalyses(t *testing.T) {
	t.Parallel()
	t.Run("empty experiments returns nil", func(t *testing.T) {
		result := computeExperimentAnalysesWithObservations(nil, nil, nil, nil, nil)
		assert.Nil(t, result, "nil experiments should return nil")
	})

	t.Run("multiple experiments analysed independently", func(t *testing.T) {
		experiments := []ExperimentVariantStats{
			{Name: "exp1", Variants: map[string]int{"A": 5, "B": 5}, Total: 10},
			{Name: "exp2", Variants: map[string]int{"X": 3, "Y": 7}, Total: 10},
		}
		analyses := computeExperimentAnalysesWithObservations(experiments, nil, nil, nil, nil)
		require.Len(t, analyses, 2, "should produce one analysis per experiment")
		assert.Equal(t, "exp1", analyses[0].ExperimentName, "first analysis name")
		assert.Equal(t, "exp2", analyses[1].ExperimentName, "second analysis name")
	})

	t.Run("config map applied per experiment", func(t *testing.T) {
		experiments := []ExperimentVariantStats{
			{Name: "alpha", Variants: map[string]int{"on": 25, "off": 25}, Total: 50},
		}
		configs := map[string]*workflow.ExperimentConfig{
			"alpha": {
				Variants:     []string{"on", "off"},
				Hypothesis:   "test hypothesis",
				AnalysisType: "proportion_test",
				MinSamples:   20,
			},
		}
		analyses := computeExperimentAnalysesWithObservations(experiments, configs, nil, nil, nil)
		require.Len(t, analyses, 1, "one analysis")
		assert.Equal(t, "test hypothesis", analyses[0].Hypothesis, "hypothesis from config")
		assert.Equal(t, "proportion_test", analyses[0].AnalysisType, "analysis type from config")
		assert.Equal(t, "READY_FOR_ANALYSIS", analyses[0].Recommendation, "above min_samples")
	})
}

// TestExperimentAnalysisJSONOutput verifies that ExperimentAnalysis serialises correctly.
func TestExperimentAnalysisJSONOutput(t *testing.T) {
	t.Parallel()
	exp := ExperimentVariantStats{
		Name:     "style",
		Variants: map[string]int{"concise": 12, "detailed": 8},
		Total:    20,
	}
	cfg := &workflow.ExperimentConfig{
		Variants:     []string{"concise", "detailed"},
		Hypothesis:   "H0: no change. H1: concise is better.",
		AnalysisType: "t_test",
		MinSamples:   30,
		GuardrailMetrics: []workflow.GuardrailMetric{
			{Name: "success_rate", Threshold: ">=0.95"},
		},
	}

	a := computeExperimentAnalysisWithObservationBundle(exp, cfg, nil, nil, nil, nil)
	jsonBytes, err := json.MarshalIndent(a, "", "  ")
	require.NoError(t, err, "should marshal analysis to JSON")

	var result map[string]any
	require.NoError(t, json.Unmarshal(jsonBytes, &result), "should unmarshal JSON")

	assert.Equal(t, "style", result["experiment_name"], "experiment_name field")
	assert.Equal(t, "H0: no change. H1: concise is better.", result["hypothesis"], "hypothesis field")
	assert.Equal(t, "t_test", result["analysis_type"], "analysis_type field")
	assert.EqualValues(t, 30, result["min_samples"], "min_samples field")
	assert.Equal(t, "COLLECTING", result["readiness"], "readiness field")
	assert.Equal(t, "EXTEND", result["recommendation"], "recommendation field (below min_samples)")
	assert.Equal(t, "EXTEND", result["decision"], "deterministic decision field")
	assert.Equal(t, "insufficient_samples", result["reason_code"], "structured decision reason")
	assert.Contains(t, result, "decision_policy", "normalized policy should be machine-readable")
	assert.Contains(t, result, "decision_guardrails", "guardrail summary should be machine-readable")

	variants, ok := result["variants"].([]any)
	require.True(t, ok, "variants should be an array")
	require.Len(t, variants, 2, "two variants")

	guardrails, ok := result["guardrails"].([]any)
	require.True(t, ok, "guardrails should be an array")
	require.Len(t, guardrails, 1, "one guardrail")
}

// TestExperimentAnalysisBonferroniAbsent verifies BonferroniAlpha is omitted for K < 3.
func TestExperimentAnalysisBonferroniAbsent(t *testing.T) {
	t.Parallel()
	exp := ExperimentVariantStats{
		Name:     "binary",
		Variants: map[string]int{"yes": 10, "no": 10},
		Total:    20,
	}
	a := computeExperimentAnalysisWithObservationBundle(exp, nil, nil, nil, nil, nil)
	assert.InDelta(t, 0.0, a.BonferroniAlpha, 1e-10, "BonferroniAlpha should be zero for K=2")

	jsonBytes, err := json.MarshalIndent(a, "", "  ")
	require.NoError(t, err, "should marshal to JSON")

	var result map[string]any
	require.NoError(t, json.Unmarshal(jsonBytes, &result), "should unmarshal JSON")
	_, present := result["bonferroni_alpha"]
	assert.False(t, present, "bonferroni_alpha should be omitted from JSON for K=2")
}

// TestMinSamplesDefaultApplied verifies the default min_samples value is 20.
func TestMinSamplesDefaultApplied(t *testing.T) {
	t.Parallel()
	exp := ExperimentVariantStats{
		Name:     "test",
		Variants: map[string]int{"A": 10, "B": 10},
		Total:    20,
	}
	a := computeExperimentAnalysisWithObservationBundle(exp, nil, nil, nil, nil, nil)
	assert.Equal(t, defaultMinSamples, a.MinSamples, "default min_samples should be 20")
}

// TestChiSquarePValueMonotonicity verifies that larger chi2 values produce smaller p-values.
func TestChiSquarePValueMonotonicity(t *testing.T) {
	t.Parallel()
	prev := chiSquarePValue(0.1, 1)
	for _, chi2 := range []float64{0.5, 1.0, 2.0, 3.841, 6.0, 10.0} {
		p := chiSquarePValue(chi2, 1)
		assert.Less(t, p, prev, "p-value should decrease as chi2 increases (chi2=%.1f)", chi2)
		prev = p
	}
}

// TestChiSquarePValueReturnRange verifies p-values are always in [0, 1].
func TestChiSquarePValueReturnRange(t *testing.T) {
	t.Parallel()
	inputs := []struct {
		chi2 float64
		df   int
	}{
		{0, 1}, {0.1, 1}, {1.0, 1}, {3.841, 1}, {100, 1},
		{0.1, 2}, {5.991, 2}, {0.1, 10}, {20.0, 5},
	}
	for _, tc := range inputs {
		p := chiSquarePValue(tc.chi2, tc.df)
		assert.True(t, p >= 0 && p <= 1,
			"p-value %.6f out of [0,1] for chi2=%.2f df=%d", p, tc.chi2, tc.df)
	}
}

// TestObservedPctSumsToHundred verifies that observed percentages sum to ~100%.
func TestObservedPctSumsToHundred(t *testing.T) {
	t.Parallel()
	exp := ExperimentVariantStats{
		Name:     "pct_test",
		Variants: map[string]int{"A": 7, "B": 13},
		Total:    20,
	}
	a := computeExperimentAnalysisWithObservationBundle(exp, nil, nil, nil, nil, nil)
	total := 0.0
	for _, v := range a.Variants {
		total += v.ObservedPct
	}
	assert.InDelta(t, 100.0, total, 0.01, "observed percentages should sum to 100")
}

// TestExpectedPctSumsToHundred verifies expected percentages sum to ~100%.
func TestExpectedPctSumsToHundred(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		variants map[string]int
		cfg      *workflow.ExperimentConfig
	}{
		{"equal split", map[string]int{"A": 10, "B": 10}, nil},
		{"three equal", map[string]int{"A": 5, "B": 5, "C": 5}, nil},
		{
			"weighted 60/40",
			map[string]int{"A": 12, "B": 8},
			&workflow.ExperimentConfig{Variants: []string{"A", "B"}, Weight: []int{60, 40}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			total := 0
			for _, c := range tt.variants {
				total += c
			}
			exp := ExperimentVariantStats{Name: "e", Variants: tt.variants, Total: total}
			a := computeExperimentAnalysisWithObservationBundle(exp, tt.cfg, nil, nil, nil, nil)
			sum := 0.0
			for _, v := range a.Variants {
				sum += v.ExpectedPct
			}
			assert.InDelta(t, 100.0, sum, 0.01, "expected percentages should sum to 100")
		})
	}
}

// TestReadyForAnalysisAllAboveMinSamples verifies the recommendation when all counts >= min_samples.
func TestReadyForAnalysisAllAboveMinSamples(t *testing.T) {
	t.Parallel()
	exp := ExperimentVariantStats{
		Name:     "ready",
		Variants: map[string]int{"X": 25, "Y": 30},
		Total:    55,
	}
	cfg := &workflow.ExperimentConfig{
		Variants:   []string{"X", "Y"},
		MinSamples: 20,
	}
	a := computeExperimentAnalysisWithObservationBundle(exp, cfg, nil, nil, nil, nil)
	assert.Equal(t, "READY_FOR_ANALYSIS", a.Recommendation, "all variants above min_samples → READY")
	assert.Contains(t, a.Rationale, "min_samples", "rationale should mention min_samples")

	for _, v := range a.Variants {
		assert.False(t, v.BelowMinSamples, "no variant should be below min_samples")
	}
}

// TestPartiallyBelowMinSamples verifies EXTEND when only some variants are below threshold.
func TestPartiallyBelowMinSamples(t *testing.T) {
	t.Parallel()
	exp := ExperimentVariantStats{
		Name:     "partial",
		Variants: map[string]int{"above": 25, "below": 5},
		Total:    30,
	}
	cfg := &workflow.ExperimentConfig{
		Variants:   []string{"above", "below"},
		MinSamples: 20,
	}
	a := computeExperimentAnalysisWithObservationBundle(exp, cfg, nil, nil, nil, nil)
	assert.Equal(t, "EXTEND", a.Recommendation, "one variant below threshold → EXTEND")
	assert.Contains(t, a.Rationale, "1 of 2", "rationale should count variants below threshold")

	// Variants are sorted alphabetically: "above" comes before "below".
	require.Len(t, a.Variants, 2, "should have two variants")
	assert.Equal(t, "above", a.Variants[0].Name, "first variant alphabetically")
	assert.Equal(t, "below", a.Variants[1].Name, "second variant alphabetically")
	assert.False(t, a.Variants[0].BelowMinSamples, "'above' (count=25) should not be flagged")
	assert.True(t, a.Variants[1].BelowMinSamples, "'below' (count=5) should be flagged")
}

// TestChiSquarePerfectBalance verifies that chi² = 0 for a perfectly balanced sample.
func TestChiSquarePerfectBalance(t *testing.T) {
	t.Parallel()
	exp := ExperimentVariantStats{
		Name:     "perfect",
		Variants: map[string]int{"A": 10, "B": 10, "C": 10},
		Total:    30,
	}
	a := computeExperimentAnalysisWithObservationBundle(exp, nil, nil, nil, nil, nil)
	assert.InDelta(t, 0.0, a.ChiSquare, 1e-10, "chi² should be 0 for perfectly balanced sample")
	assert.InDelta(t, 1.0, a.PValue, 1e-10, "p should be 1.0 for chi²=0")
	assert.True(t, a.IsBalanced, "perfectly balanced → is_balanced")
}

// TestFindWorkflowFileForExperiment verifies that the function returns "" in isolation
// (no .github/workflows directory in the test working directory).
func TestFindWorkflowFileForExperiment(t *testing.T) {
	t.Parallel()
	// In the test environment, no .github/workflows directory is available relative to cwd.
	// The function should return "" without panicking.
	result := findWorkflowFileForExperiment("nonexistent_experiment")
	assert.Empty(t, result, "should return empty string when no matching file found")
}

// TestMatchWorkflowFilenameByExperiment verifies that the helper correctly resolves
// hyphenated workflow filenames from sanitized experiment branch names.
func TestMatchWorkflowFilenameByExperiment(t *testing.T) {
	t.Parallel()
	files := []string{
		"ci-coach.md",
		"daily-issues-report.md",
		"smoke-copilot.md",
		"agent-performance-analyzer.md",
		"deep-report.md",
		"noHyphen.md",
	}
	tests := []struct {
		experimentName string
		want           string
	}{
		{"cicoach", "ci-coach"},
		{"dailyissuesreport", "daily-issues-report"},
		{"smokecopilot", "smoke-copilot"},
		{"agentperformanceanalyzer", "agent-performance-analyzer"},
		{"deepreport", "deep-report"},
		{"nohyphen", "noHyphen"},
		{"notfound", ""},
	}
	for _, tt := range tests {
		t.Run(tt.experimentName, func(t *testing.T) {
			got := matchWorkflowFilenameByExperiment(files, tt.experimentName)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestMatchWorkflowFilenameByExperimentAmbiguous verifies that the first match is returned
// and a warning is logged when multiple files share the same sanitized name.
func TestMatchWorkflowFilenameByExperimentAmbiguous(t *testing.T) {
	t.Parallel()
	// "ci-coach.md" and "cicoach.md" both sanitize to "cicoach".
	files := []string{"ci-coach.md", "cicoach.md"}
	got := matchWorkflowFilenameByExperiment(files, "cicoach")
	assert.Equal(t, "ci-coach", got, "should return the first match")
}

// TestWorkflowFileCandidates verifies the fallback candidate list for remote lookups.
// The real resolution is handled by findRemoteWorkflowFilenameForExperiment; this list
// is only used as a last-resort fallback when the directory listing is unavailable.
func TestWorkflowFileCandidates(t *testing.T) {
	t.Parallel()
	tests := []struct {
		experimentName string
		wantContains   string
	}{
		{"myworkflow", "myworkflow"},
		{"dailyreport", "dailyreport"},
		{"test123", "test123"},
	}
	for _, tt := range tests {
		t.Run(tt.experimentName, func(t *testing.T) {
			candidates := workflowFileCandidates(tt.experimentName)
			assert.NotEmpty(t, candidates, "should return at least one candidate")
			assert.Contains(t, candidates, tt.wantContains, "should contain the experiment name itself")
		})
	}
}

// TestAnalysisWithNilConfig verifies analysis runs cleanly without a config.
func TestAnalysisWithNilConfig(t *testing.T) {
	t.Parallel()
	exp := ExperimentVariantStats{
		Name:     "no_config",
		Variants: map[string]int{"on": 8, "off": 12},
		Total:    20,
	}
	a := computeExperimentAnalysisWithObservationBundle(exp, nil, nil, nil, nil, nil)
	assert.Equal(t, "no_config", a.ExperimentName, "experiment name preserved")
	assert.Empty(t, a.Hypothesis, "no hypothesis without config")
	assert.Empty(t, a.AnalysisType, "no analysis type without config")
	assert.Equal(t, defaultMinSamples, a.MinSamples, "default min_samples")
	assert.Empty(t, a.Guardrails, "no guardrails without config")
	// With 20 total runs and min_samples=20, both variants (8,12) are below 20 → EXTEND
	assert.Equal(t, "EXTEND", a.Recommendation, "below default min_samples → EXTEND")
	// Verify p-value is a real number in [0,1].
	assert.False(t, math.IsNaN(a.PValue), "p-value should not be NaN")
	assert.True(t, a.PValue >= 0 && a.PValue <= 1, "p-value should be in [0,1]")
}

// TestComputeExperimentAnalysisDegenerateVariants tests degenerate experiments with < 2 variants.
func TestComputeExperimentAnalysisDegenerateVariants(t *testing.T) {
	t.Parallel()
	t.Run("zero variants returns EXTEND", func(t *testing.T) {
		exp := ExperimentVariantStats{
			Name:     "no_variants",
			Variants: map[string]int{},
			Total:    0,
		}
		a := computeExperimentAnalysisWithObservationBundle(exp, nil, nil, nil, nil, nil)
		assert.Equal(t, "EXTEND", a.Recommendation, "zero variants → EXTEND")
		assert.True(t, a.IsBalanced, "degenerate case defaults to balanced")
		assert.Empty(t, a.Variants, "no variant entries")
		assert.Contains(t, a.Rationale, "fewer than 2 variants", "rationale mentions variant count")
	})

	t.Run("single variant returns EXTEND", func(t *testing.T) {
		exp := ExperimentVariantStats{
			Name:     "one_variant",
			Variants: map[string]int{"only": 10},
			Total:    10,
		}
		a := computeExperimentAnalysisWithObservationBundle(exp, nil, nil, nil, nil, nil)
		assert.Equal(t, "EXTEND", a.Recommendation, "single variant → EXTEND")
		assert.True(t, a.IsBalanced, "degenerate case defaults to balanced")
		assert.Empty(t, a.Variants, "no variant entries emitted for degenerate case")
		assert.Contains(t, a.Rationale, "fewer than 2 variants", "rationale is clear")
	})
}
