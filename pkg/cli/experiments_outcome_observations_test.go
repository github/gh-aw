//go:build !integration

package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/github/gh-aw/pkg/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func experimentOutcome(status OutcomeStatus, complete, zeroTouch bool) OutcomeReport {
	return OutcomeReport{
		Type: "create_pull_request", OutcomeEvaluation: OutcomeEvaluation{OutcomeStatus: status},
		ZeroTouchEvidenceComplete: complete, ZeroTouch: zeroTouch,
	}
}

func TestOutcomeMetricRunValue(t *testing.T) {
	t.Parallel()
	merged := experimentOutcome(OutcomeStatusAccepted, true, true)
	touched := experimentOutcome(OutcomeStatusAccepted, true, false)
	incomplete := experimentOutcome(OutcomeStatusAccepted, false, false)
	rejected := experimentOutcome(OutcomeStatusRejected, false, false)
	pending := experimentOutcome(OutcomeStatusPending, false, false)
	unknown := experimentOutcome(OutcomeStatusUnknown, false, false)
	issue := OutcomeReport{Type: "create_issue", OutcomeEvaluation: OutcomeEvaluation{OutcomeStatus: OutcomeStatusAccepted}}
	for _, tc := range []struct {
		name     string
		kind     string
		items    []OutcomeReport
		value    float64
		measured bool
		coverage OutcomeMetricCoverage
	}{
		{name: "merged", kind: "accepted", items: []OutcomeReport{merged}, value: 1, measured: true},
		{name: "closed", kind: "accepted", items: []OutcomeReport{rejected}, measured: true},
		{name: "stale pending", kind: "accepted", items: []OutcomeReport{pending}, measured: true, coverage: OutcomeMetricCoverage{StalePending: 1}},
		{name: "multiple PRs", kind: "accepted", items: []OutcomeReport{merged, rejected, pending}, value: 1.0 / 3, measured: true, coverage: OutcomeMetricCoverage{StalePending: 1}},
		{name: "multiple pending count runs", kind: "accepted", items: []OutcomeReport{pending, pending}, measured: true, coverage: OutcomeMetricCoverage{StalePending: 1}},
		{name: "unknown excluded from denominator", kind: "accepted", items: []OutcomeReport{merged, unknown}, value: 1, measured: true, coverage: OutcomeMetricCoverage{Unavailable: 1}},
		{name: "unknown only unavailable not noop", kind: "accepted", items: []OutcomeReport{unknown}, coverage: OutcomeMetricCoverage{Unavailable: 1}},
		{name: "error only unavailable", kind: "accepted", items: []OutcomeReport{experimentOutcome(OutcomeStatusError, false, false)}, coverage: OutcomeMetricCoverage{Unavailable: 1}},
		{name: "empty noop", kind: "accepted", coverage: OutcomeMetricCoverage{NoOp: 1}},
		{name: "non outcome statuses noop", kind: "accepted", items: []OutcomeReport{
			experimentOutcome(OutcomeStatusIgnored, false, false), experimentOutcome(OutcomeStatusSkipped, false, false),
			experimentOutcome(OutcomeStatusLifecycle, false, false), experimentOutcome(OutcomeStatusLifecycleClose, false, false),
		}, coverage: OutcomeMetricCoverage{NoOp: 1}},
		{name: "zero touch", kind: "zero_touch", items: []OutcomeReport{merged}, value: 1, measured: true},
		{name: "human touch is zero", kind: "zero_touch", items: []OutcomeReport{touched}, measured: true},
		{name: "incomplete is excluded", kind: "zero_touch", items: []OutcomeReport{incomplete}, coverage: OutcomeMetricCoverage{Unavailable: 1}},
		{name: "non PR acceptance contributes zero to zero touch", kind: "zero_touch", items: []OutcomeReport{issue}, measured: true},
		{name: "rejected needs no touch evidence", kind: "zero_touch", items: []OutcomeReport{rejected}, measured: true},
		{name: "pending needs no touch evidence", kind: "zero_touch", items: []OutcomeReport{pending}, measured: true, coverage: OutcomeMetricCoverage{StalePending: 1}},
		{name: "incomplete does not affect acceptance", kind: "accepted", items: []OutcomeReport{incomplete}, value: 1, measured: true},
		{name: "zero touch all resolved denominator", kind: "zero_touch", items: []OutcomeReport{merged, touched, rejected, pending}, value: .25, measured: true, coverage: OutcomeMetricCoverage{StalePending: 1}},
		{name: "zero touch partial evidence", kind: "zero_touch", items: []OutcomeReport{merged, incomplete}, value: 1, measured: true, coverage: OutcomeMetricCoverage{Unavailable: 1}},
		{name: "mixed PR and issue denominator", kind: "zero_touch", items: []OutcomeReport{merged, issue}, value: .5, measured: true},
		{name: "mixed output acceptance", kind: "accepted", items: []OutcomeReport{merged, issue}, value: 1, measured: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			value, measured, coverage := outcomeMetricRunValue(tc.kind, tc.items)
			assert.InDelta(t, tc.value, value, 1e-12)
			assert.Equal(t, tc.measured, measured)
			assert.Equal(t, tc.coverage, coverage)
		})
	}
}

type fixtureOutcomeRunSource struct {
	mu    sync.Mutex
	calls []string
}

func (s *fixtureOutcomeRunSource) Load(_ context.Context, id string) outcomeRunData {
	s.mu.Lock()
	s.calls = append(s.calls, id)
	s.mu.Unlock()
	return outcomeRunData{Report: OutcomesData{Items: []OutcomeReport{experimentOutcome(OutcomeStatusAccepted, true, true)}}}
}

func TestOutcomeMetricMaturityAndAssignmentJoin(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 4, 0, 0, 0, 0, time.UTC)
	timestamp := func(age time.Duration) string { return now.Add(-age).Format(time.RFC3339) }
	runs := []ExperimentRunRecord{
		{RunID: "mature", Timestamp: timestamp(72 * time.Hour), Assignments: map[string]string{"prompt": "control"}},
		{RunID: "young", Timestamp: timestamp(71 * time.Hour), Assignments: map[string]string{"prompt": "candidate"}},
		{RunID: "bad timestamp", Timestamp: "bad", Assignments: map[string]string{"prompt": "candidate"}},
		{RunID: "unassigned", Timestamp: timestamp(100 * time.Hour)},
		{RunID: "mature", Timestamp: timestamp(72 * time.Hour), Assignments: map[string]string{"prompt": "control"}},
	}
	experiments := []ExperimentVariantStats{{Name: "prompt", Variants: map[string]int{"control": 3, "candidate": 2}, Total: 5}}
	refs := map[string]string{"prompt": "accepted"}
	windows := map[string]time.Duration{"prompt": defaultExperimentOutcomeWindow}
	source := &fixtureOutcomeRunSource{}
	data := loadOutcomeRunData(context.Background(), runs, windows, nil, now, source)
	assert.Equal(t, []string{"mature"}, source.calls, "only assigned, mature runs are collected and shared across metrics")
	set := buildOutcomeMetricObservationSets(experiments, runs, refs, windows, now, data)["prompt"]
	require.Len(t, set.ByVariant["control"], 1)
	assert.Equal(t, "mature", set.ByVariant["control"][0].RunID)
	assert.InDelta(t, 1, set.ByVariant["control"][0].Value, 1e-12)
	assert.Equal(t, &OutcomeMetricCoverage{Unavailable: 1}, set.Coverage["control"], "missing ledger entries are unavailable")
	assert.Equal(t, &OutcomeMetricCoverage{Immature: 1, Unavailable: 1}, set.Coverage["candidate"])
	assert.Empty(t, set.ByVariant["candidate"])
	assert.Equal(t, 1, exclusionsByReason(set.Exclusions["control"])[exclusionDuplicateAssignment].Count)

	windows["prompt"] = 70 * time.Hour
	source = &fixtureOutcomeRunSource{}
	_ = loadOutcomeRunData(context.Background(), runs, windows, nil, now, source)
	assert.ElementsMatch(t, []string{"mature", "young"}, source.calls)
}

func TestOutcomeMetricCoverageForMatureRuns(t *testing.T) {
	t.Parallel()
	now := time.Now()
	runs := []ExperimentRunRecord{}
	data := map[string]outcomeRunData{
		"pending": {Report: OutcomesData{Items: []OutcomeReport{experimentOutcome(OutcomeStatusPending, false, false)}}},
		"noop":    {Report: OutcomesData{}},
		"missing": {ExclusionReason: exclusionArtifactUnavailable},
		"unknown": {Report: OutcomesData{Items: []OutcomeReport{experimentOutcome(OutcomeStatusUnknown, false, false)}}},
		"error":   {Report: OutcomesData{EvalError: "manifest unavailable"}},
	}
	for _, id := range []string{"pending", "noop", "missing", "unknown", "error"} {
		runs = append(runs, ExperimentRunRecord{
			RunID: id, Timestamp: now.Add(-80 * time.Hour).Format(time.RFC3339),
			Assignments: map[string]string{"prompt": "control"},
		})
	}
	experiments := []ExperimentVariantStats{{Name: "prompt", Variants: map[string]int{"control": 5}, Total: 5}}
	set := buildOutcomeMetricObservationSets(experiments, runs, map[string]string{"prompt": "accepted"}, nil, now, data)["prompt"]
	require.Len(t, set.ByVariant["control"], 1)
	assert.Equal(t, "pending", set.ByVariant["control"][0].RunID)
	assert.Zero(t, set.ByVariant["control"][0].Value)
	assert.Equal(t, &OutcomeMetricCoverage{StalePending: 1, Unavailable: 3, NoOp: 1}, set.Coverage["control"])

	cfg := &workflow.ExperimentConfig{Metric: "outcome:accepted", Variants: []string{"control", "candidate"}, MinSamples: 2}
	analysis := computeExperimentAnalysisWithObservationBundle(experiments[0], cfg, nil, nil, set, nil)
	assert.Equal(t, ExperimentReadinessCollecting, analysis.Readiness)
	assert.Equal(t, "binary", analysis.MetricType)
	variants := variantsByName(analysis.Variants)
	require.NotNil(t, variants["candidate"].OutcomeCoverage)
	assert.Equal(t, &OutcomeMetricCoverage{}, variants["candidate"].OutcomeCoverage)
}

func TestEmptyOutcomeMetricObservationsDoNotInferBinaryType(t *testing.T) {
	set := &graderMetricObservationSet{
		ByVariant: map[string][]GraderMetricObservation{"control": {}, "candidate": {}},
		Coverage:  map[string]*OutcomeMetricCoverage{"control": {Unavailable: 1}, "candidate": {Unavailable: 1}},
	}
	cfg := &workflow.ExperimentConfig{
		Metric: "outcome:accepted", Variants: []string{"control", "candidate"}, MinSamples: 1,
	}
	exp := ExperimentVariantStats{
		Name: "prompt", Variants: map[string]int{"control": 1, "candidate": 1}, Total: 2,
	}
	analysis := computeExperimentAnalysisWithObservationBundle(exp, cfg, nil, nil, set, nil)
	assert.Empty(t, analysis.MetricType)
	assert.Empty(t, analysis.Comparisons, "no observations must not run statistical tests")
}

func TestOutcomeMetricPerExperimentWindowsShareCollection(t *testing.T) {
	t.Parallel()
	now := time.Now()
	run := ExperimentRunRecord{
		RunID: "shared", Timestamp: now.Add(-48 * time.Hour).Format(time.RFC3339),
		Assignments: map[string]string{"short": "control", "long": "candidate"},
	}
	runs := []ExperimentRunRecord{run, run}
	configs := map[string]*workflow.ExperimentConfig{
		"short": {Metric: "outcome:accepted", OutcomeWindow: "24h"},
		"long":  {Metric: "outcome:zero_touch", OutcomeWindow: "72h"},
	}
	refs, _, windows, automatic, err := resolveOutcomeMetricReferences(configs)
	require.NoError(t, err)
	source := &fixtureOutcomeRunSource{}
	data := loadOutcomeRunData(context.Background(), runs, windows, automatic, now, source)
	assert.Equal(t, []string{"shared"}, source.calls)
	experiments := []ExperimentVariantStats{
		{Name: "short", Variants: map[string]int{"control": 2}, Total: 2},
		{Name: "long", Variants: map[string]int{"candidate": 2}, Total: 2},
	}
	sets := buildOutcomeMetricObservationSets(experiments, runs, refs, windows, now, data)
	require.Len(t, sets["short"].ByVariant["control"], 1, "duplicate assignments cannot add samples")
	assert.Empty(t, sets["long"].ByVariant["candidate"], "shared data must not bypass a longer maturity window")
	assert.Equal(t, 1, sets["long"].Coverage["candidate"].Immature)

	configs["long"].OutcomeWindow = "36h"
	refs, _, windows, automatic, err = resolveOutcomeMetricReferences(configs)
	require.NoError(t, err)
	source = &fixtureOutcomeRunSource{}
	data = loadOutcomeRunData(context.Background(), runs, windows, automatic, now, source)
	assert.Equal(t, []string{"shared"}, source.calls, "both mature experiments share one collection")
	sets = buildOutcomeMetricObservationSets(experiments, runs, refs, windows, now, data)
	assert.Len(t, sets["short"].ByVariant["control"], 1)
	assert.Len(t, sets["long"].ByVariant["candidate"], 1)
}

func TestResolveOutcomeMetricReferences(t *testing.T) {
	t.Parallel()
	configs := map[string]*workflow.ExperimentConfig{
		"prompt": {
			Metric: "outcome:accepted", SecondaryMetrics: []string{"outcome:zero_touch"},
			GuardrailMetrics: []workflow.GuardrailMetric{{Name: "outcome:zero_touch", Threshold: ">=0.5"}},
		},
	}
	primary, guardrails, windows, automatic, err := resolveOutcomeMetricReferences(configs)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"prompt": "accepted"}, primary)
	assert.Equal(t, "zero_touch", guardrails["prompt"]["outcome:zero_touch"])
	assert.Equal(t, 72*time.Hour, windows["prompt"])
	assert.Contains(t, automatic, "prompt")
	secondary, err := resolveOutcomeSecondaryMetricReferences(map[string]*workflow.ExperimentConfig{
		"prompt": {SecondaryMetrics: []string{"outcome:zero_touch"}},
	})
	require.NoError(t, err)
	assert.Equal(t, map[string]map[string]string{"prompt": {"outcome:zero_touch": "zero_touch"}}, secondary)
	configs["prompt"].OutcomeWindow = "24h"
	_, _, windows, automatic, err = resolveOutcomeMetricReferences(configs)
	require.NoError(t, err)
	assert.Equal(t, 24*time.Hour, windows["prompt"])
	assert.Empty(t, automatic)
	configs["prompt"].OutcomeWindow = "auto"
	_, _, windows, automatic, err = resolveOutcomeMetricReferences(configs)
	require.NoError(t, err)
	assert.Equal(t, 72*time.Hour, windows["prompt"])
	assert.Contains(t, automatic, "prompt")
	for _, duration := range []string{"-1h", "0s", "bad"} {
		configs["prompt"].OutcomeWindow = duration
		_, _, _, _, err = resolveOutcomeMetricReferences(configs)
		require.ErrorContains(t, err, "outcome_window")
	}
	configs["prompt"].OutcomeWindow = ""
	configs["prompt"].Metric = "outcome:merged"
	_, _, _, _, err = resolveOutcomeMetricReferences(configs)
	require.ErrorContains(t, err, "Expected outcome:accepted or outcome:zero_touch")
	configs["prompt"].Metric = "grader:score"
	configs["prompt"].GuardrailMetrics[0].Name = "outcome:typo"
	_, _, _, _, err = resolveOutcomeMetricReferences(configs)
	require.ErrorContains(t, err, "outcome:typo")
}

func TestEstimateOutcomeWindow(t *testing.T) {
	now := time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC)
	makeFixture := func(decisions []float64, pending []time.Duration) ([]ExperimentRunRecord, map[string]outcomeRunData) {
		runs := make([]ExperimentRunRecord, 0, len(decisions)+len(pending))
		data := make(map[string]outcomeRunData, len(decisions)+len(pending))
		for i, hours := range decisions {
			id := fmt.Sprintf("decision-%d", i)
			runs = append(runs, ExperimentRunRecord{RunID: id, Assignments: map[string]string{"quality": []string{"control", "candidate"}[i%2]}})
			data[id] = outcomeRunData{Report: OutcomesData{Items: []OutcomeReport{{OutcomeEvaluation: OutcomeEvaluation{OutcomeStatus: OutcomeStatusAccepted}, TimeToOutcomeHours: hours}}}}
		}
		for i, age := range pending {
			id := fmt.Sprintf("pending-%d", i)
			runs = append(runs, ExperimentRunRecord{RunID: id, Assignments: map[string]string{"quality": "control"}})
			data[id] = outcomeRunData{Report: OutcomesData{Items: []OutcomeReport{{
				OutcomeEvaluation: OutcomeEvaluation{OutcomeStatus: OutcomeStatusPending},
				CreatedAt:         now.Add(-age).Format(time.RFC3339),
			}}}}
		}
		return runs, data
	}

	t.Run("fallback below twenty decisions", func(t *testing.T) {
		decisions := make([]float64, minExperimentOutcomeDecisions-1)
		for i := range decisions {
			decisions[i] = 48
		}
		runs, data := makeFixture(decisions, nil)
		assert.Equal(t, defaultExperimentOutcomeWindow, estimateOutcomeWindow(runs, map[string]struct{}{"quality": {}}, data, now))
		info := estimateOutcomeWindowInfo(runs, map[string]struct{}{"quality": {}}, data, now)
		assert.Equal(t, "default", info.Source)
		assert.Equal(t, minExperimentOutcomeDecisions-1, info.SampleSize)
	})

	t.Run("p95 includes censored pending outputs", func(t *testing.T) {
		decisions := make([]float64, minExperimentOutcomeDecisions)
		for i := range decisions {
			decisions[i] = 60
			if i < minExperimentOutcomeDecisions-1 {
				decisions[i] = 48
			}
		}
		runs, data := makeFixture(decisions, []time.Duration{49 * time.Hour})
		assert.Equal(t, 60*time.Hour, estimateOutcomeWindow(runs, map[string]struct{}{"quality": {}}, data, now))
	})

	t.Run("p95 is clamped", func(t *testing.T) {
		decisions := make([]float64, minExperimentOutcomeDecisions)
		for i := range decisions {
			decisions[i] = float64((maxExperimentOutcomeWindow + 24*time.Hour).Hours())
		}
		runs, data := makeFixture(decisions, nil)
		assert.Equal(t, maxExperimentOutcomeWindow, estimateOutcomeWindow(runs, map[string]struct{}{"quality": {}}, data, now))
	})

	t.Run("minimum p95 is clamped", func(t *testing.T) {
		decisions := make([]float64, minExperimentOutcomeDecisions)
		for i := range decisions {
			decisions[i] = 6
		}
		runs, data := makeFixture(decisions, nil)
		assert.Equal(t, minExperimentOutcomeWindow, estimateOutcomeWindow(runs, map[string]struct{}{"quality": {}}, data, now))
	})

	t.Run("maximum when p95 is not reached", func(t *testing.T) {
		decisions := make([]float64, minExperimentOutcomeDecisions)
		for i := range decisions {
			decisions[i] = 24 * 31
		}
		runs, data := makeFixture(decisions, nil)
		assert.Equal(t, maxExperimentOutcomeWindow, estimateOutcomeWindow(runs, map[string]struct{}{"quality": {}}, data, now))
	})
}

func TestOutcomeMetricAnalysisCoverageAndDefaults(t *testing.T) {
	for _, tc := range []struct {
		name, testType string
		values         []float64
	}{
		{name: "binary numeric", testType: "proportion_test", values: []float64{0, 1}},
		{name: "fractional multi PR", testType: "t_test", values: []float64{0.5, 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			set := &graderMetricObservationSet{
				MetricID: "outcome:accepted",
				ByVariant: map[string][]GraderMetricObservation{
					"control": decisionTestObservations("control", tc.values), "candidate": decisionTestObservations("candidate", tc.values),
				},
				Coverage: map[string]*OutcomeMetricCoverage{"control": {Immature: 1, StalePending: 2, Unavailable: 3, NoOp: 4}, "candidate": {}},
			}
			for variant, observations := range set.ByVariant {
				for i := range observations {
					observations[i].Binary = observations[i].Value == 0 || observations[i].Value == 1
				}
				set.ByVariant[variant] = observations
			}
			cfg := &workflow.ExperimentConfig{
				Metric: "outcome:accepted", Variants: []string{"control", "candidate"}, MinSamples: 2,
				GuardrailMetrics: []workflow.GuardrailMetric{{Name: "outcome:zero_touch", Threshold: ">=0"}},
			}
			exp := ExperimentVariantStats{Name: "prompt", Variants: map[string]int{"control": 10, "candidate": 10}, Total: 20}
			analysis := computeExperimentAnalysisWithObservationBundle(exp, cfg, nil, nil, set, map[string]*graderMetricObservationSet{"outcome:zero_touch": set})
			require.Len(t, analysis.Comparisons, 1)
			assert.Equal(t, tc.testType, analysis.Comparisons[0].AnalysisType)
			assert.Empty(t, analysis.Comparisons[0].Error)
			assert.Equal(t, ExperimentReadinessReady, analysis.Readiness)
			assert.Equal(t, GuardrailStatusPass, analysis.Guardrails[0].Status)
			encoded, err := json.Marshal(analysis)
			require.NoError(t, err)
			assert.Contains(t, string(encoded), `"outcome_coverage":{"immature":1,"stale_pending":2,"unavailable":3,"no_op":4}`)
			output := captureExperimentDetailsStderr(t, &ExperimentDetails{Analyses: []ExperimentAnalysis{analysis}})
			assert.Contains(t, output, "immature=1 stale_pending=2 unavailable=3 no_op=4")
			assert.Contains(t, output, "outcome:zero_touch / control")
		})
	}
}

func installExperimentOutcomeFixture(t *testing.T, manifest string) string {
	t.Helper()
	fixtures := t.TempDir()
	t.Setenv("GH_AW_TEST_EXPERIMENT_ARTIFACTS", fixtures)
	t.Setenv("GH_AW_TEST_EXPERIMENT_DOWNLOAD_ERROR", "")
	t.Setenv("GH_AW_TEST_EXPERIMENT_NAMES", "called-safe-outputs-items\ncalled-agent-output-fallback\n")
	writeSessionTestFile(t, fixtures, "called-safe-outputs-items/"+safeOutputItemsManifestFilename, manifest)
	writeSessionTestFile(t, fixtures, "called-agent-output-fallback/gh-aw/safeoutputs.jsonl", "")
	return installOutcomeAPIFixture(t, `
printf '%s\n' "$*" >> "$GH_AW_TEST_OUTCOME_CALLS"
if [ "$1" = "run" ] && [ "$2" = "download" ]; then
  shift 3
  while [ "$#" -gt 0 ]; do
    case "$1" in
      --name) name="$2"; shift 2 ;;
      --dir) dir="$2"; shift 2 ;;
      -R) shift 2 ;;
      *) exit 1 ;;
    esac
  done
  if [ -n "$GH_AW_TEST_EXPERIMENT_DOWNLOAD_ERROR" ]; then exit 1; fi
  cp -R "$GH_AW_TEST_EXPERIMENT_ARTIFACTS/$name/." "$dir"
  exit 0
fi
case "$*" in
  *actions/runs/101/artifacts*) printf '%s' "$GH_AW_TEST_EXPERIMENT_NAMES" ;;
  *actions/runs/101*) printf '%s' '{"databaseId":101,"status":"completed","conclusion":"success","repository":"acme/repo"}' ;;
  *repos/acme/repo/pulls/42/reviews*|*repos/acme/repo/pulls/42/commits*|*repos/acme/repo/issues/42/comments*) printf '%s' '[[]]' ;;
  *repos/acme/repo/pulls/42*) printf '%s' '{"merged":true,"state":"closed","labels":[]}' ;;
  *graphql*) printf '%s' '{"data":{"repository":{"pullRequest":{"id":"PR42","closingIssuesReferences":{"nodes":[]}}}}}' ;;
  *) printf 'unexpected command: %s\n' "$*" >&2; exit 1 ;;
esac
`)
}

func TestOutcomeMetricsRequireOptIn(t *testing.T) {
	for _, tc := range []struct {
		name   string
		config *workflow.ExperimentConfig
	}{
		{name: "primary", config: &workflow.ExperimentConfig{Metric: "outcome:accepted"}},
		{name: "secondary", config: &workflow.ExperimentConfig{SecondaryMetrics: []string{"outcome:zero_touch"}}},
		{name: "guardrail", config: &workflow.ExperimentConfig{
			Metric: "grader:tool-success-rate",
			GuardrailMetrics: []workflow.GuardrailMetric{
				{Name: "outcome:accepted", Threshold: ">=0.8"},
			},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := installOutcomeAPIFixture(t, "exit 1\n")
			frontmatter := experimentFrontmatterResult{
				ExperimentConfigs: map[string]*workflow.ExperimentConfig{"prompt": tc.config},
			}
			bundle, cleanup, err := loadMetricObservationSetsForAnalysis(&ExperimentDetails{}, frontmatter, "acme/repo", false)
			defer cleanup()
			require.ErrorContains(t, err, "require --outcome")
			assert.Contains(t, err.Error(), "additional GitHub API requests")
			assert.Nil(t, bundle)
			_, err = os.Stat(calls)
			assert.True(t, os.IsNotExist(err), "opt-in must be checked before any metric API requests")
		})
	}
}

func TestOutcomeOptInWithoutOutcomeMetrics(t *testing.T) {
	for _, outcome := range []bool{false, true} {
		t.Run(strconv.FormatBool(outcome), func(t *testing.T) {
			calls := installOutcomeAPIFixture(t, "exit 1\n")
			bundle, cleanup, err := loadMetricObservationSetsForAnalysis(
				&ExperimentDetails{}, experimentFrontmatterResult{}, "acme/repo", outcome,
			)
			defer cleanup()
			require.NoError(t, err)
			require.NotNil(t, bundle)
			assert.Empty(t, bundle.Primary)
			assert.Empty(t, bundle.Secondary)
			assert.Empty(t, bundle.Guardrails)
			_, err = os.Stat(calls)
			assert.True(t, os.IsNotExist(err), "workflows without outcome metrics need no outcome API requests")
		})
	}
}

func TestLoadOutcomeObservationSetsUsesLiveCollector(t *testing.T) {
	calls := installExperimentOutcomeFixture(t, `{"type":"create_pull_request","number":42,"timestamp":"2026-01-01T00:00:00Z","url":"https://github.com/acme/repo/pull/42"}`+"\n")
	details := &ExperimentDetails{
		Experiments: []ExperimentVariantStats{{Name: "prompt", Variants: map[string]int{"control": 1, "candidate": 1}, Total: 2}},
		Runs: []ExperimentRunRecord{
			{RunID: "101", Timestamp: time.Now().Add(-80 * time.Hour).Format(time.RFC3339), Assignments: map[string]string{"prompt": "control"}},
			{RunID: "102", Timestamp: time.Now().Format(time.RFC3339), Assignments: map[string]string{"prompt": "candidate"}},
		},
	}
	frontmatter := experimentFrontmatterResult{ExperimentConfigs: map[string]*workflow.ExperimentConfig{
		"prompt": {
			Metric: "outcome:accepted", SecondaryMetrics: []string{"outcome:zero_touch"},
			GuardrailMetrics: []workflow.GuardrailMetric{{Name: "outcome:zero_touch", Threshold: ">=0.5"}},
		},
	}}
	bundle, cleanup, err := loadMetricObservationSetsForAnalysis(details, frontmatter, "acme/repo", true)
	require.NoError(t, err)
	defer cleanup()
	require.Len(t, bundle.Primary["prompt"].ByVariant["control"], 1)
	assert.InDelta(t, 1, bundle.Primary["prompt"].ByVariant["control"][0].Value, 1e-12)
	require.Len(t, bundle.Guardrails["prompt"]["outcome:zero_touch"].ByVariant["control"], 1)
	assert.InDelta(t, 1, bundle.Guardrails["prompt"]["outcome:zero_touch"].ByVariant["control"][0].Value, 1e-12)
	require.Len(t, bundle.Secondary["prompt"]["outcome:zero_touch"].ByVariant["control"], 1)
	assert.Equal(t, "outcome:zero_touch", bundle.Secondary["prompt"]["outcome:zero_touch"].MetricID)
	assert.Equal(t, 1, bundle.Primary["prompt"].Coverage["candidate"].Immature)
	assert.Equal(t, "default", bundle.Primary["prompt"].OutcomeWindow.Source)
	analyses := computeExperimentAnalysesWithObservationBundle(
		details.Experiments, frontmatter.ExperimentConfigs, nil, nil, bundle,
	)
	require.Len(t, analyses, 1)
	require.Len(t, analyses[0].SecondaryMetrics, 1)
	assert.Equal(t, "outcome:zero_touch", analyses[0].SecondaryMetrics[0].Metric)
	assert.Equal(t, 1, analyses[0].SecondaryMetrics[0].Variants[1].ObservationCount)
	assert.NotNil(t, analyses[0].OutcomeWindow)
	commands, err := os.ReadFile(calls)
	require.NoError(t, err)
	assert.Contains(t, string(commands), "--name called-safe-outputs-items")
	assert.Contains(t, string(commands), "--name called-agent-output-fallback")
	assert.Contains(t, string(commands), "repos/acme/repo/pulls/42")
	assert.Contains(t, string(commands), "runs/102", "automatic windows need recent outputs as censored observations")
	assert.Equal(t, 1, strings.Count(string(commands), "api repos/acme/repo/actions/runs/101 --jq"), "primary and guardrail share one live collection")
}

func TestOutcomeArtifactUnavailabilityIsNotNoOp(t *testing.T) {
	for _, tc := range []struct {
		name, manifest, names string
		downloadError         bool
		unavailable           bool
	}{
		{name: "explicit noop", manifest: "{\"type\":\"noop\"}\n"},
		{name: "empty manifest"},
		{name: "missing data has no outcome", manifest: "{\"type\":\"report_missing_data\"}\n"},
		{name: "missing manifest", names: "called-agent-output-fallback", unavailable: true},
		{name: "no artifacts", names: "usage", unavailable: true},
		{name: "corrupt manifest", manifest: "{", unavailable: true},
		{name: "failed download", downloadError: true, unavailable: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			installExperimentOutcomeFixture(t, tc.manifest)
			if tc.names != "" {
				t.Setenv("GH_AW_TEST_EXPERIMENT_NAMES", tc.names)
			}
			if tc.downloadError {
				t.Setenv("GH_AW_TEST_EXPERIMENT_DOWNLOAD_ERROR", "1")
			}
			source := &githubOutcomeRunSource{artifacts: newGitHubGraderRunArtifactSource(t.TempDir(), "acme/repo")}
			data := source.Load(context.Background(), "101")
			if tc.unavailable {
				assert.NotEmpty(t, data.ExclusionReason)
			} else {
				assert.Empty(t, data.ExclusionReason)
				_, measured, coverage := outcomeMetricRunValue("accepted", data.Report.Items)
				assert.False(t, measured)
				assert.Equal(t, 1, coverage.NoOp)
			}
		})
	}
}

func TestZeroTouchEvidenceCompleteness(t *testing.T) {
	oldGet, oldArray := outcomeEvalPRGHAPIGet, outcomeEvalPRGHAPIGetArray
	t.Cleanup(func() { outcomeEvalPRGHAPIGet, outcomeEvalPRGHAPIGetArray = oldGet, oldArray })
	outcomeEvalPRGHAPIGet = func(context.Context, string, string) (map[string]any, error) {
		return map[string]any{"merged": true, "state": "closed"}, nil
	}
	for _, tc := range []struct {
		name, failedEndpoint, malformedEndpoint string
		badTimestamp, touched                   bool
		complete                                bool
	}{
		{name: "complete no touch", complete: true},
		{name: "complete human touch", touched: true, complete: true},
		{name: "missing comments", failedEndpoint: "issues/1/comments"},
		{name: "missing reviews", failedEndpoint: "pulls/1/reviews"},
		{name: "missing commits", failedEndpoint: "pulls/1/commits"},
		{name: "malformed comments", malformedEndpoint: "issues/1/comments"},
		{name: "malformed reviews", malformedEndpoint: "pulls/1/reviews"},
		{name: "malformed commits", malformedEndpoint: "pulls/1/commits"},
		{name: "missing creation timestamp", badTimestamp: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			outcomeEvalPRGHAPIGetArray = func(_ context.Context, endpoint, _ string) ([]map[string]any, error) {
				if endpoint == tc.failedEndpoint {
					return nil, errors.New("unavailable")
				}
				if endpoint == tc.malformedEndpoint {
					return []map[string]any{{}}, nil
				}
				if endpoint == "issues/1/comments" {
					if tc.touched {
						return []map[string]any{{"user": map[string]any{"login": "alice"}, "created_at": "2026-01-02T00:00:00Z"}}, nil
					}
				}
				return nil, nil
			}
			timestamp := "2026-01-01T00:00:00Z"
			if tc.badTimestamp {
				timestamp = ""
			}
			report := evalCreatePullRequest(context.Background(), CreatedItemReport{Type: "create_pull_request", Number: 1, Timestamp: timestamp}, "acme/repo")
			assert.Equal(t, tc.complete, report.ZeroTouchEvidenceComplete)
			assert.Equal(t, tc.complete && !tc.touched, report.ZeroTouch)
		})
	}
}
