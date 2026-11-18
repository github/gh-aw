//go:build !integration

package cli

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/github/gh-aw/pkg/lifecycle"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOperationalValuePolicySampling(t *testing.T) {
	git, commit := valueTestGit(t)
	parent := commit("2026-09-01T12:00:00Z", map[string]string{
		"pkg/parser/schemas/main_workflow_schema.json": `{"properties":{"a":{},"b":{},"c":{}}}`,
		".github/workflows/a.md":                       "a:\n",
	})
	sha := commit("2026-09-28T16:37:55Z", map[string]string{"schema-demos/b.md": "b:\n"})
	commit("2026-09-30T16:37:55Z", map[string]string{"schema-demos/c.md": "c:\n"})
	tip := commit("2026-10-20T12:00:00Z", nil)
	at := valueTestTime(t, "2026-09-28T16:37:55Z")
	output := valueOutput{PR: 1, RunID: 1, SHA: sha, base: parent, URL: "https://github.com/github/gh-aw/pull/1", Delivered: at.Add(-time.Hour), Merged: at}
	record := valueTestRecord(t, output)
	records := map[string]lifecycle.Record{record.ID: record}
	policy, err := resolveValueWindowPolicy(&valueRecipe{Window: json.RawMessage(`{
"before_days":2,"after_days":3,"outcome_lag_days":2,"durability_days":9,"sample_every_hours":12,
"minimum_before_samples":4,"minimum_after_samples":6,"trend_every_days":3,"isolation_before_days":1,"isolation_after_days":1}`)})
	require.NoError(t, err)
	window, err := buildValueWindow(git, schemaCoverageMeasurement{}, tip, output, valueTestTime(t, "2026-10-10T12:00:00Z"), []valueOutput{output}, records, policy)
	require.NoError(t, err)
	require.Len(t, window.Samples, 10)
	assert.Equal(t, at.Add(-48*time.Hour), window.Samples[0].At)
	assert.Equal(t, at.Add(48*time.Hour), window.Samples[4].At)
	assert.Equal(t, at.Add(108*time.Hour), window.Samples[9].At)
	assert.Equal(t, new(2.0), window.Change.Value)
	assert.Equal(t, at.Add(9*24*time.Hour), window.End.At)
	assert.True(t, window.ObservationsComplete)
	assert.Nil(t, window.Attributed, "unrelated delayed improvements must not acquire attribution")
	assert.Equal(t, "delayed_outcome_attribution_requires_subject_linkage", window.Reason)
	policy.OutcomeLagDays = 0
	window, err = buildValueWindow(git, schemaCoverageMeasurement{}, tip, output, valueTestTime(t, "2026-10-10T12:00:00Z"), []valueOutput{output}, records, policy)
	require.NoError(t, err)
	assert.Equal(t, new(1.0), window.Attributed)
	policy.AfterDays, policy.DurabilityDays, policy.MinimumAfter = 14, 1, 4
	window, err = buildValueWindow(git, schemaCoverageMeasurement{}, tip, output, valueTestTime(t, "2026-10-02T12:00:00Z"), []valueOutput{output}, records, policy)
	require.NoError(t, err)
	assert.Equal(t, "pending", window.Status)
	assert.Equal(t, "outcome_window_incomplete", window.Reason)
	options := valueReportOptions{Start: valueTestTime(t, "2026-09-22T12:00:00Z"), End: valueTestTime(t, "2026-10-06T12:00:00Z"),
		ObservedAt: valueTestTime(t, "2026-10-21T12:00:00Z"), Policy: policy}
	report, err := buildOperationalValueReport(git, schemaCoverageMeasurement{}, tip, options, nil, nil)
	require.NoError(t, err)
	require.Len(t, report.Weekly, 6)
}

func TestOperationalValuePolicyOverlap(t *testing.T) {
	at := valueTestTime(t, "2026-09-28T16:37:55Z")
	policy := defaultValueWindowPolicy()
	policy.Overlap, policy.IsolationBeforeDays, policy.IsolationAfterDays = "changed_files", 1, 2
	window := valueWindow{PR: 1, MergedAt: at, ChangedFiles: []string{"a.go"}, Policy: policy}
	outputs := []valueOutput{{PR: 2, Merged: at.Add(24 * time.Hour), Files: []string{"b.go"}}}
	assert.Empty(t, valueOverlapReason(&window, outputs))
	outputs[0].Files = []string{"a.go"}
	assert.Equal(t, "overlapping_changed_files", valueOverlapReason(&window, outputs))
	outputs[0].Files = nil
	assert.Equal(t, "overlap_evidence_missing", valueOverlapReason(&window, outputs))
	outputs[0].Merged = at.Add(2*24*time.Hour + time.Second)
	assert.Empty(t, valueOverlapReason(&window, outputs))
	window.Policy = defaultValueWindowPolicy()
	assert.Equal(t, "shared_workflow_merge_window", valueOverlapReason(&window, outputs))
}

func TestOperationalValuePolicyValidation(t *testing.T) {
	for _, invalid := range []string{
		`{"version":2}`, `{"baseline":"best_observed"}`, `{"before_days":0}`, `{"after_days":366}`,
		`{"sample_every_hours":7}`, `{"minimum_before_samples":0}`, `{"before_days":2}`,
		`{"outcome_lag_days":8}`, `{"overlap":"ignore"}`, `{"sample_every_hours":1,"before_days":365}`,
		`{"unknown_policy":true}`,
	} {
		_, err := resolveValueWindowPolicy(&valueRecipe{Window: json.RawMessage(invalid)})
		require.Error(t, err, invalid)
	}
}

func TestOperationalValueRecipeAsymmetricSamples(t *testing.T) {
	policy := defaultValueWindowPolicy()
	policy.BeforeDays, policy.AfterDays, policy.SampleEveryHours = 2, 3, 12
	policy.MinimumBefore, policy.MinimumAfter = 4, 6
	first := valueWindow{PR: 1, MergedAt: valueTestTime(t, "2026-09-10T12:00:00Z"), Policy: policy}
	last := valueWindow{PR: 2, MergedAt: first.MergedAt.Add(time.Hour), Policy: policy}
	for i := range 10 {
		first.Samples = append(first.Samples, &valueSnapshot{SHA: "first", Value: new(float64(i))})
		last.Samples = append(last.Samples, &valueSnapshot{SHA: "last", Value: new(float64(i))})
	}
	report := operationalValueReport{Windows: []valueWindow{first, last}}
	recipe := &valueRecipe{EarlyBatchesThrough: "2026-09-12"}
	require.NoError(t, inspectValueRecipe(&report, recipe))
	require.Len(t, report.InspectedWindows, 1)
	samples := report.InspectedWindows[0].Samples
	require.Len(t, samples, 10)
	assert.Equal(t, "first", samples[3].SHA)
	assert.Equal(t, "last", samples[4].SHA)
	assert.Equal(t, new(4.0), samples[4].Value)
	report.Windows[0].Samples = first.Samples[:9]
	require.ErrorContains(t, inspectValueRecipe(&report, recipe), "incomplete sample slots")
}
