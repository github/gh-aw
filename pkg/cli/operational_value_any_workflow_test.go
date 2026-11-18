//go:build !integration

package cli

import (
	"bytes"
	"encoding/json"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/lifecycle"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOperationalValueAnyWorkflowWithoutMeasurement(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	definition, err := resolveValueWorkflow("weekly-research", "acme/repo")
	require.NoError(t, err)
	delivered := valueTestTime(t, "2026-09-28T07:00:00Z")
	record, err := lifecycle.Delivered(lifecycle.Identity{
		Host: definition.Host, Repository: definition.Repository, Workflow: definition.WorkflowPath,
		RunID: 42, RunAttempt: 1, InvocationID: "safe-outputs", EvidenceID: "comment-1",
		Type: "add_comment", TargetURL: "https://github.com/acme/repo/issues/1#issuecomment-2",
	}, delivered, delivered, "safe-output-manifest")
	require.NoError(t, err)
	logs := filepath.Join(dir, "logs")
	require.NoError(t, writeLifecycleRecords(filepath.Join(logs, "lifecycle.jsonl"), map[string]lifecycle.Record{record.ID: record}))
	var output bytes.Buffer
	command := NewValueCommand()
	command.SetOut(&output)
	command.SetArgs([]string{"weekly-research", "--repo", "acme/repo", "--logs-dir", logs,
		"--start", "2026-09-21", "--end", "2026-10-06", "--json"})
	require.NoError(t, command.Execute())
	var report operationalValueReport
	require.NoError(t, json.Unmarshal(output.Bytes(), &report))
	assert.Equal(t, "measurement_not_configured", report.Reason)
	assert.Equal(t, "unavailable", report.MeasurementStatus)
	assert.Nil(t, report.AttributedValue)
	assert.Empty(t, report.Weekly)
	require.NotNil(t, report.Lifecycle.Delivered)
	assert.Equal(t, 1, *report.Lifecycle.Delivered)
	assert.Equal(t, "retained_subset", report.Lifecycle.Coverage)
	require.NotNil(t, report.Lifecycle.Adoption)
	assert.Equal(t, 1, report.Lifecycle.Adoption.AcceptedFallback)
	assert.Zero(t, report.Lifecycle.Adoption.AcceptedHuman)
	output.Reset()
	require.NoError(t, renderOperationalValueReport(&output, nil, report))
	assert.Contains(t, output.String(), "not configured")
	assert.Contains(t, output.String(), "1 delivery fallback")
	assert.Contains(t, output.String(), "measurement_not_configured")
	foreign, err := resolveValueWorkflow("other-workflow", "acme/repo")
	require.NoError(t, err)
	missing := newOperationalValueReport(foreign, report.Start, report.End, report.ObservedAt, map[string]lifecycle.Record{record.ID: record})
	assert.Nil(t, missing.Lifecycle.Delivered, "another workflow must not establish delivery or zero")
	assert.Nil(t, missing.Lifecycle.Adoption)
	assert.Equal(t, "unavailable", missing.Lifecycle.Coverage)
}

func TestOperationalValueSnapshotFunction(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node unavailable")
	}
	git, commit := valueTestGit(t)
	before := commit("2026-09-20T12:00:00Z", map[string]string{
		"metrics/value.json": "{\"covered\":1.25}\n",
		"unselected.txt":     "not available to the value function",
	})
	after := commit("2026-09-28T16:37:55Z", map[string]string{"metrics/value.json": "{\"covered\":3.75}\n"})
	definition, err := resolveValueWorkflow("any-workflow", "github/gh-aw")
	require.NoError(t, err)
	config := valueFunctionConfig{
		ID: "test-value-v1", Name: "Measured outcome", Unit: "units", Direction: "higher_is_better",
		Paths: []string{"metrics"}, Parameters: map[string]any{"offset": 0.5},
		Script: `if (typeof process !== "undefined" || typeof require !== "undefined" ||
typeof fetch !== "undefined" || typeof Date !== "undefined" || typeof Math.random !== "undefined")
  throw new Error("ambient authority exposed");
if (!Object.isFrozen(snapshot.files) || Object.hasOwn(snapshot.files, "unselected.txt"))
  throw new Error("snapshot boundary violated");
const text = snapshot.files["metrics/value.json"];
if (text === undefined) return null;
if (!text.endsWith("\n")) throw new Error("file bytes changed");
return JSON.parse(text).covered + parameters.offset;`,
	}
	path := filepath.Join(t.TempDir(), "value.json")
	load := func() valueMeasurement {
		t.Helper()
		content, err := json.Marshal(config)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(path, content, 0o600))
		measurement, cleanup, err := loadScriptValueMeasurement(t.Context(), definition, path)
		require.NoError(t, err)
		t.Cleanup(cleanup)
		return measurement
	}
	measurement := load()
	baseline, err := measurement.measure(git, before)
	require.NoError(t, err)
	measured, err := measurement.measure(git, after)
	require.NoError(t, err)
	repeated, err := measurement.measure(git, after)
	require.NoError(t, err)
	assert.Equal(t, measured.Value, repeated.Value)
	assert.Equal(t, new(1.75), baseline.Value)
	assert.Equal(t, new(4.25), measured.Value)
	assert.Equal(t, new(2.5), measurement.change(baseline, measured).Value)
	assert.Contains(t, measurement.definition().Digest, "sha256:")
	assert.Nil(t, measurement.cohort(nil), "the shared pipeline must not require schema-coverage evidence")
	tip := commit("2026-10-10T12:00:00Z", map[string]string{"unrelated.txt": "later history"})
	merged := valueTestTime(t, "2026-09-28T16:37:55Z")
	output := valueOutput{
		PR: 1, RunID: 42, URL: "https://github.com/github/gh-aw/pull/1", SHA: after,
		Delivered: valueTestTime(t, "2026-09-28T07:00:00Z"), Merged: merged,
	}
	record, err := lifecycle.Delivered(lifecycle.Identity{
		Host: definition.Host, Repository: definition.Repository, Workflow: definition.WorkflowPath,
		RunID: output.RunID, RunAttempt: 1, InvocationID: "safe-outputs", EvidenceID: "pr-1",
		Type: "create_pull_request", TargetURL: output.URL,
	}, output.Delivered, output.Delivered, "safe-output-manifest")
	require.NoError(t, err)
	record, err = lifecycle.Observe(record, lifecycle.Observation{
		State: "accepted", Signal: "merged", At: merged, ObservedAt: merged,
		AcceptanceKind: "explicit_human", HumanVerified: true,
	})
	require.NoError(t, err)
	window, err := buildValueWindow(git, measurement, tip, output, valueTestTime(t, "2026-10-06T12:00:00Z"),
		[]valueOutput{output}, map[string]lifecycle.Record{record.ID: record})
	require.NoError(t, err)
	assert.Equal(t, "attributed", window.Status)
	assert.Equal(t, new(2.5), window.Attributed, "custom functions must use the same native adoption and survival gates")
	require.Len(t, window.Samples, 14)
	config.Direction = "lower_is_better"
	lower := load()
	assert.Equal(t, new(-2.5), lower.change(baseline, measured).Value, "regressions must remain signed, not clamped")
	assert.NotEqual(t, lower.definition().Digest, measurement.definition().Digest)
	config.Script = "return null;"
	missing := load()
	snapshot, err := missing.measure(git, after)
	require.NoError(t, err)
	assert.Nil(t, snapshot.Value)
	assert.Nil(t, missing.change(baseline, snapshot).Value)
	config.Script = "return Promise.resolve(1);"
	invalid := load()
	_, err = invalid.measure(git, after)
	require.ErrorContains(t, err, "synchronous")
	config.Script = "return NaN;"
	invalid = load()
	_, err = invalid.measure(git, after)
	require.ErrorContains(t, err, "finite numeric value")
	config.Script = "throw new Error('x'.repeat(70000));"
	invalid = load()
	_, err = invalid.measure(git, after)
	require.ErrorContains(t, err, "output exceeds 64 KiB")
}

func TestOperationalValueNumericOverflow(t *testing.T) {
	measurement := scriptValueMeasurement{info: valueMeasurementDefinition{Direction: "higher_is_better"}}
	change := measurement.change(valueSnapshot{Value: new(-math.MaxFloat64)}, valueSnapshot{Value: new(math.MaxFloat64)})
	assert.Nil(t, change.Value)
	assert.Equal(t, "non_finite_measurement_change", change.Reason)
	report := operationalValueReport{Windows: []valueWindow{
		{Attributed: new(math.MaxFloat64)}, {Attributed: new(math.MaxFloat64)},
	}}
	require.ErrorContains(t, summarizeValueWindows(&report), "finite numeric range")
	assert.Nil(t, report.AttributedValue)
}

func TestOperationalValueFunctionDefinitionFailures(t *testing.T) {
	definition, err := resolveValueWorkflow("any-workflow", "acme/repo")
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "value.json")
	for _, content := range []string{
		`{"unexpected":"field"}`,
		`{} {}`,
		`{"id":"x","name":"x","unit":"items","direction":"wrong","paths":["a"],"script":"return 1;"}`,
		`{"id":"x","name":"x","unit":"items","direction":"higher_is_better","paths":["../secret"],"script":"return 1;"}`,
	} {
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
		_, _, err := loadScriptValueMeasurement(t.Context(), definition, path)
		require.Error(t, err)
	}
	_, _, err = selectValueMeasurement(t.Context(), definition, "", filepath.Join(t.TempDir(), "missing.json"))
	require.ErrorContains(t, err, "read value-function")
	require.NoError(t, os.WriteFile(path, bytes.Repeat([]byte(" "), 512*1024+1), 0o600))
	_, _, err = loadScriptValueMeasurement(t.Context(), definition, path)
	require.ErrorContains(t, err, "512 KiB")
	for _, repo := range []string{"acme", "../repo", "acme/..", "https://github.com/acme/repo"} {
		_, err := resolveValueWorkflow("any-workflow", repo)
		require.Error(t, err)
	}
}
