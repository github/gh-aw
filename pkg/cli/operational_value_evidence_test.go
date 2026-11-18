//go:build !integration

package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOperationalValueEvidenceCommand(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	config := filepath.Join(dir, "value.json")
	require.NoError(t, os.WriteFile(config, []byte(`{
"version":1,"id":"benchmark-v1","name":"Independent benchmark","unit":"ns/op","direction":"lower_is_better",
"evaluator":"evidence-number-v1","config":{"scope":"benchmark","aggregation":"median","minimum_samples":4,"require_population":true}}`), 0o600))
	before := valueTestEvidenceObservation(t)
	before.Values = []float64{9, 3, 5, 7}
	after := before
	after.ID, after.Phase, after.At = "after", "outcome", before.At.Add(valueDays(1))
	after.Values = []float64{4, 1, 3, 2}
	unrequested := before
	unrequested.ID, unrequested.Workflow = "unrequested", "another-workflow"
	path := filepath.Join(dir, "evidence.json")
	valueWriteJSON(t, path, valueEvidenceBundle{Version: 1, Repository: "github.com/acme/repo",
		Observations: []valueEvidenceObservation{unrequested, after, before}})
	var output bytes.Buffer
	command := NewValueCommand()
	command.SetOut(&output)
	command.SetArgs([]string{"performance", "--repo", "acme/repo", "--value-function", config, "--evidence", path,
		"--start", "2026-09-01", "--end", "2026-10-06", "--json"})
	require.NoError(t, command.Execute())
	var report operationalValueReport
	require.NoError(t, json.Unmarshal(output.Bytes(), &report))
	require.NotNil(t, report.Evidence)
	assert.Equal(t, "configured", report.MeasurementStatus)
	assert.Equal(t, "available", report.Evidence.Coverage)
	require.Len(t, report.Evidence.Observations, 2, "only the declared workflow/metric/scope is passed to the evaluator")
	assert.Equal(t, new(6.0), report.Evidence.Observations[0].Value)
	assert.Equal(t, new(2.5), report.Evidence.Observations[1].Value)
	require.Len(t, report.Evidence.Changes, 1)
	assert.Equal(t, new(3.5), report.Evidence.Changes[0].Change.Value)
	assert.Nil(t, report.AttributedValue)
	assert.Nil(t, report.Evidence.Changes[0].Attributed)
	assert.Contains(t, report.Evidence.Trust, "caller_supplied")
	assert.Contains(t, report.Evidence.Digest, "sha256:")
	output.Reset()
	require.NoError(t, renderOperationalValueReport(&output, valueEvidenceMeasurement{}, report))
	assert.Contains(t, output.String(), "Measured improvement: 3.5 ns/op")
	assert.Contains(t, output.String(), "not collected", "typed evidence does not search for PRs")
}

func TestOperationalValueEvidenceMissingSample(t *testing.T) {
	input := valueTestEvidenceObservation(t)
	definition, err := resolveValueWorkflow(input.Workflow, "acme/repo")
	require.NoError(t, err)
	data, err := json.Marshal(input)
	require.NoError(t, err)
	var wire map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(data, &wire))
	path := filepath.Join(t.TempDir(), "evidence.json")
	load := func(values string) (valueEvidenceBundle, error) {
		t.Helper()
		wire["values"] = json.RawMessage(values)
		valueWriteJSON(t, path, map[string]any{"version": 1, "repository": "github.com/acme/repo", "observations": []any{wire}})
		bundle, _, err := loadValueEvidenceBundle(path, definition, input.ObservedAt)
		return bundle, err
	}
	for _, invalid := range []string{`[null]`, `[5,null,0]`} {
		_, err := load(invalid)
		require.ErrorContains(t, err, "is null")
		require.ErrorContains(t, err, "mark coverage unavailable")
	}
	bundle, err := load(`[0]`)
	require.NoError(t, err)
	require.Len(t, bundle.Observations, 1)
	assert.Equal(t, valueEvidenceSamples{0}, bundle.Observations[0].Values)
	wire["coverage"], wire["reason"] = json.RawMessage(`"unavailable"`), json.RawMessage(`"sample_missing"`)
	bundle, err = load(`null`)
	require.NoError(t, err)
	assert.Nil(t, bundle.Observations[0].Values)
}

func valueTestEvidenceObservation(t *testing.T) valueEvidenceObservation {
	t.Helper()
	return valueEvidenceObservation{
		ID: "before", Workflow: "performance", Metric: "benchmark-v1", Scope: "benchmark", Subject: "benchmark:compile",
		RunID: 42, At: valueTestTime(t, "2026-09-20T12:00:00Z"), ObservedAt: valueTestTime(t, "2026-10-01T12:00:00Z"),
		Phase: "baseline", Coverage: "available", Population: new([]string{"fixture:compile-v1"}),
		Context: "runner-v1:go-v1:fixture-v1", References: []string{"https://example.com/benchmark/42"}, Values: []float64{1, 2, 3, 4},
	}
}

func TestOperationalValueEvidenceGuards(t *testing.T) {
	measurement := valueEvidenceMeasurement{config: valueEvidenceConfig{
		Scope: "benchmark", Aggregation: "median", MinimumSamples: 4, RequirePopulation: true,
	}}
	input := valueTestEvidenceObservation(t)
	input.Values = []float64{1, 2}
	result, err := measurement.evaluate(input)
	require.NoError(t, err)
	assert.Nil(t, result.Value)
	assert.Equal(t, "insufficient_measurement_samples", result.Reason)
	input.Values, input.Population = []float64{1, 2, 3, 4}, nil
	result, err = measurement.evaluate(input)
	require.NoError(t, err)
	assert.Equal(t, "eligible_population_missing", result.Reason)
	input = valueTestEvidenceObservation(t)
	input.Context = ""
	result, err = measurement.evaluate(input)
	require.NoError(t, err)
	assert.Equal(t, "benchmark_comparability_context_missing", result.Reason)
	input = valueTestEvidenceObservation(t)
	input.Coverage, input.Reason, input.Values = "unavailable", "independent_reference_missing", nil
	require.NoError(t, validateValueEvidenceObservation(input, input.ObservedAt))
	result, err = measurement.evaluate(input)
	require.NoError(t, err)
	assert.Nil(t, result.Value)
	assert.Equal(t, input.Reason, result.Reason)
	input.Values = []float64{0}
	require.Error(t, validateValueEvidenceObservation(input, input.ObservedAt), "missing evidence cannot masquerade as zero")
	input = valueTestEvidenceObservation(t)
	input.ObservedAt = input.At.Add(-valueDays(1))
	require.Error(t, validateValueEvidenceObservation(input, input.At))
	measurement.config = valueEvidenceConfig{Scope: "git_snapshot", Aggregation: "excess_lines", MinimumSamples: 1,
		RequirePopulation: true, MaximumLines: new(799)}
	input = valueTestEvidenceObservation(t)
	input.Values, input.Population = []float64{1202, 417, 199}, new([]string{"old.go", "new.go", "other.go"})
	result, err = measurement.evaluate(input)
	require.NoError(t, err)
	assert.Equal(t, new(403.0), result.Value)
	input.Values = []float64{799, 800, 801}
	result, err = measurement.evaluate(input)
	require.NoError(t, err)
	assert.Equal(t, new(3.0), result.Value, "the policy treats exactly 800 lines as over the healthy budget")
}

func TestOperationalValueEvidenceIdentity(t *testing.T) {
	definition, err := resolveValueWorkflow("performance", "acme/repo")
	require.NoError(t, err)
	input := valueTestEvidenceObservation(t)
	path := filepath.Join(t.TempDir(), "evidence.json")
	valueWriteJSON(t, path, valueEvidenceBundle{Version: 1, Repository: "github.com/elsewhere/repo", Observations: []valueEvidenceObservation{input}})
	_, _, err = loadValueEvidenceBundle(path, definition, input.ObservedAt)
	require.ErrorContains(t, err, "selected HOST/OWNER/REPO")
	valueWriteJSON(t, path, valueEvidenceBundle{Version: 1, Repository: "github.com/acme/repo", Observations: []valueEvidenceObservation{input, input}})
	_, _, err = loadValueEvidenceBundle(path, definition, input.ObservedAt)
	require.ErrorContains(t, err, "duplicate evidence")
	content := `{"version":1,"repository":"github.com/acme/repo","observations":[],"adoption":{"human_verified":true}}`
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	_, _, err = loadValueEvidenceBundle(path, definition, input.ObservedAt)
	require.Error(t, err, "the bundle cannot supply native adoption")
	for _, config := range []valueEvidenceConfig{
		{Scope: "arbitrary_query", Aggregation: "scalar", MinimumSamples: 1},
		{Scope: "benchmark", Aggregation: "scalar", MinimumSamples: 1},
		{Scope: "run", Aggregation: "median", MinimumSamples: 0},
	} {
		require.Error(t, validateValueEvidenceConfig(config))
	}
}

func TestOperationalValueEvidenceComparisons(t *testing.T) {
	input := valueTestEvidenceObservation(t)
	before := valueEvidenceResult{Input: input, Value: new(6.0)}
	after := before
	after.Input.ID, after.Input.Phase, after.Input.At = "after", "outcome", input.At.Add(valueDays(1))
	after.Input.RunID, after.Value = 43, new(2.5)
	report := operationalValueReport{End: input.At.Add(valueDays(30)), WindowPolicy: defaultValueWindowPolicy()}
	measurement := valueEvidenceMeasurement{info: valueMeasurementDefinition{Direction: "lower_is_better"}}
	change, err := compareValueEvidence(&report, measurement, before, after)
	require.NoError(t, err)
	assert.Equal(t, new(3.5), change.Change.Value)
	assert.True(t, change.WindowMature)
	assert.Equal(t, "not_evaluated", change.SamplingCoverage)
	assert.Nil(t, change.Attributed, "elapsed time and valid numeric inputs cannot establish attribution")
	after.Input.Context = "different-runner"
	change, err = compareValueEvidence(&report, measurement, before, after)
	require.NoError(t, err)
	assert.Nil(t, change.Change.Value)
	assert.Equal(t, "measurement_context_changed", change.Change.Reason)
	after.Input.Context, after.Input.Population = input.Context, new([]string{"different-fixture"})
	change, err = compareValueEvidence(&report, measurement, before, after)
	require.NoError(t, err)
	assert.Nil(t, change.Change.Value)
	after.Input.At = before.Input.At.Add(-valueDays(1))
	_, err = compareValueEvidence(&report, measurement, before, after)
	require.ErrorContains(t, err, "ordered event clocks")
	report.Evidence = &valueEvidenceReport{Observations: []valueEvidenceResult{before, before}}
	require.ErrorContains(t, finishValueEvidenceReport(&report, measurement), "multiple baselines")
	after.Input.Phase = "outcome"
	report.Evidence.Observations = []valueEvidenceResult{after, after}
	require.ErrorContains(t, finishValueEvidenceReport(&report, measurement), "multiple outcomes")
	input.ObservedAt = input.ObservedAt.Add(valueDays(1))
	require.ErrorContains(t, validateValueEvidenceObservation(input, input.ObservedAt.Add(-valueDays(1))), "clocks")
}
