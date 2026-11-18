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
	foreign, err := resolveValueWorkflow("other-workflow", "acme/repo")
	require.NoError(t, err)
	missing := newOperationalValueReport(foreign, report.Start, report.End, report.ObservedAt, map[string]lifecycle.Record{record.ID: record})
	assert.Nil(t, missing.Lifecycle.Delivered)
	assert.Nil(t, missing.Lifecycle.Adoption)
}

func TestOperationalValueNativeDefinition(t *testing.T) {
	git, commit := valueTestGit(t)
	before := commit("2026-09-20T12:00:00Z", map[string]string{
		"metrics/schema.json": `{"properties":{"a":{},"b":{},"c":{}}}`,
		"examples[1]/base.md": "a:\n  b:\n", "unselected/demo.md": "c:\n",
	})
	after := commit("2026-09-28T16:37:55Z", map[string]string{"examples[1]/nested/b.md": "```aw\nb:\n```\n"})
	definition, err := resolveValueWorkflow("any-workflow", "acme/repo")
	require.NoError(t, err)
	config := valueFunctionConfig{
		Version: 1, ID: "test-coverage-v1", Name: "Documented fields", Unit: "fields",
		Direction: "higher_is_better", Evaluator: schemaCoverageEvaluator,
		Config: json.RawMessage(`{"schema":"metrics/schema.json","examples":["examples[1]"]}`),
	}
	path := filepath.Join(t.TempDir(), "value.json")
	content, err := json.Marshal(config)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, content, 0o600))
	gitPath, err := exec.LookPath("git")
	require.NoError(t, err)
	bin := t.TempDir()
	require.NoError(t, os.Symlink(gitPath, filepath.Join(bin, "git")))
	t.Setenv("PATH", bin) // Neither Node nor a query runtime is available.
	measurement, err := loadNativeValueMeasurement(definition, path)
	require.NoError(t, err)
	baseline, err := measurement.measure(git, before)
	require.NoError(t, err)
	measured, err := measurement.measure(git, after)
	require.NoError(t, err)
	repeated, err := measurement.measure(git, after)
	require.NoError(t, err)
	assert.Equal(t, measured, repeated)
	assert.Equal(t, new(1.0), baseline.Value)
	assert.Equal(t, new(2.0), measured.Value)
	assert.Equal(t, []string{"c"}, measured.Coverage.Missing)
	assert.Equal(t, []string{"b"}, measurement.change(baseline, measured).Coverage.Closed)
	assert.Equal(t, valueDeclarationDigest(content), measurement.definition().Digest)
	assert.Equal(t, definition.WorkflowPath, measurement.definition().WorkflowPath)
	assert.Equal(t, schemaCoverageEvaluator, measurement.definition().Evaluator)
}

func TestOperationalValueNumericOverflow(t *testing.T) {
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
		`{"unexpected":"field"}`, `{} {}`, `null`, `[]`,
		`{"version":2}`,
		`{"version":1,"id":"x","name":"x","unit":"fields","direction":"higher_is_better","evaluator":"unsupported"}`,
		`{"version":1,"id":"x","name":"x","unit":"fields","direction":"higher_is_better","evaluator":"schema-example-coverage-v1","config":{"schema":"../secret","examples":["examples"]}}`,
		`{"version":1,"id":"x","name":"x","unit":"fields","direction":"lower_is_better","evaluator":"schema-example-coverage-v1"}`,
		`{"version":1,"id":"x","name":"x","unit":"fields","direction":"higher_is_better","evaluator":"schema-example-coverage-v1","config":{"schema":"schema.json","examples":["examples"],"script":"return 1"}}`,
		`{"version":1,"id":"x","name":"x","unit":"defects","direction":"sideways","evaluator":"json-number-v1","config":{"path":"value.json"}}`,
		`{"version":1,"id":"x","name":"x","unit":"defects","direction":"lower_is_better","evaluator":"json-number-v1","config":{"path":"../value.json"}}`,
		`{"version":1,"id":"x","name":"x","unit":"defects","direction":"lower_is_better","evaluator":"json-number-v1","config":{"path":"value.json","script":"return 1"}}`,
	} {
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
		_, err := loadNativeValueMeasurement(definition, path)
		require.Error(t, err, content)
	}
	require.NoError(t, os.WriteFile(path, []byte(`{"script":"return 1;"}`), 0o600))
	_, err = loadNativeValueMeasurement(definition, path)
	require.ErrorContains(t, err, "no longer support script")
	_, err = selectValueMeasurement(definition, "", filepath.Join(t.TempDir(), "missing.json"))
	require.ErrorContains(t, err, "read value-function")
	require.NoError(t, os.WriteFile(path, bytes.Repeat([]byte(" "), 512*1024+1), 0o600))
	_, err = loadNativeValueMeasurement(definition, path)
	require.ErrorContains(t, err, "512 KiB")
	for _, repo := range []string{"acme", "../repo", "acme/..", "https://github.com/acme/repo"} {
		_, err := resolveValueWorkflow("any-workflow", repo)
		require.Error(t, err)
	}
}
