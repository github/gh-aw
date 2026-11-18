//go:build !integration

package cli

import (
	"bytes"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/lifecycle"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOperationalValueExternalRepository(t *testing.T) {
	git, commit := valueTestGit(t)
	_, err := git.run("remote", "set-url", "origin", "https://github.com/acme/security")
	require.NoError(t, err)
	config := `{"version":1,"id":"critical-vulnerabilities-v1","name":"Critical vulnerabilities","unit":"vulnerabilities",
"direction":"lower_is_better","evaluator":"json-number-v1","config":{"path":"metrics/findings.json"}}`
	commit("2026-09-20T12:00:00Z", map[string]string{
		".github/aw/value/security-sweep.json": config, "metrics/findings.json": `{"value":12}`,
		filepath.Join(constants.WorkflowsDir, "security-sweep.md"): "# Reduce verified critical vulnerabilities",
	})
	merge := commit("2026-09-28T16:37:55Z", map[string]string{"metrics/findings.json": `{"value":7}`})
	tip := commit("2026-10-09T12:00:00Z", nil)
	fixture := t.TempDir()
	query := filepath.Join(fixture, "query")
	t.Setenv("GH_AW_TEST_VALUE_QUERY", query)
	t.Setenv("GH_AW_TEST_VALUE_FIXTURE", fixture)
	merged := valueTestTime(t, "2026-09-28T16:37:55Z")
	url := "https://github.com/acme/security/pull/1"
	item := valuePullRequest{Number: 1, URL: url, CreatedAt: merged.Add(-time.Hour),
		Body: "<!-- gh-aw-agentic-workflow: Security sweep, workflow_id: security-sweep, run: https://github.com/acme/security/actions/runs/42 -->"}
	item.User.Login, item.PullRequest.MergedAt = "security-bot[bot]", merged
	valueWriteJSON(t, filepath.Join(fixture, "search.json"), map[string]any{"total_count": 1, "items": []valuePullRequest{item}})
	valueWriteJSON(t, filepath.Join(fixture, "pr.json"), map[string]any{
		"html_url": url, "merged_at": merged, "merge_commit_sha": merge, "commits": 1,
	})
	record, err := lifecycle.Delivered(lifecycle.Identity{
		Host: "github.com", Repository: "acme/security", Workflow: filepath.Join(constants.WorkflowsDir, "security-sweep.lock.yml"),
		RunID: 42, RunAttempt: 1, InvocationID: "safe-outputs", EvidenceID: "pr-1",
		Type: "create_pull_request", TargetURL: url,
	}, item.CreatedAt, item.CreatedAt, "safe-output-manifest")
	require.NoError(t, err)
	record, err = lifecycle.Observe(record, lifecycle.Observation{
		State: "accepted", Signal: "merged", At: merged, ObservedAt: merged.Add(time.Hour),
		AcceptanceKind: "explicit_human", HumanVerified: true,
	})
	require.NoError(t, err)
	logs := filepath.Join(fixture, "logs")
	require.NoError(t, writeLifecycleRecords(filepath.Join(logs, "lifecycle.jsonl"), map[string]lifecycle.Record{record.ID: record}))
	installOutcomeAPIFixture(t, `case "$*" in
*search/issues*) printf '%s\n' "$*" > "$GH_AW_TEST_VALUE_QUERY"; cat "$GH_AW_TEST_VALUE_FIXTURE/search.json";;
*pulls/1*) cat "$GH_AW_TEST_VALUE_FIXTURE/pr.json";;
*) exit 1;;
esac`)
	recipe := valueRecipe{Version: 1, ID: "external-v1", Repository: "github.com/acme/security",
		Workflow: "security-sweep", Start: "2026-09-21", End: "2026-10-06", Witnesses: []int{1}}
	recipePath := filepath.Join(fixture, "recipe.json")
	valueWriteJSON(t, recipePath, recipe)
	t.Chdir(filepath.Join(git.dir, "metrics"))
	var output bytes.Buffer
	command := NewValueCommand()
	command.SetOut(&output)
	command.SetArgs([]string{"security-sweep", "--repo", "acme/security", "--ref", "main", "--recipe", recipePath, "--logs-dir", logs, "--json"})
	require.NoError(t, command.Execute())
	var report operationalValueReport
	require.NoError(t, json.Unmarshal(output.Bytes(), &report))
	assert.Equal(t, "configured", report.MeasurementStatus)
	assert.Equal(t, "acme/security", report.Measurement.Repository)
	assert.Equal(t, "json-number-v1", report.Measurement.Evaluator)
	assert.Equal(t, tip, report.Ref)
	require.Len(t, report.Weekly, 4)
	assert.Equal(t, new(12.0), report.Weekly[0].Value)
	assert.Equal(t, new(7.0), report.Weekly[3].Value)
	require.NotNil(t, report.Recipe)
	assert.Equal(t, "external-v1", report.Recipe.Definition.ID)
	assert.Equal(t, new(5.0), report.AttributedValue)
	require.Len(t, report.InspectedWindows, 1)
	assert.Equal(t, new(5.0), report.InspectedWindows[0].Attributed)
	require.Len(t, report.InspectedWindows[0].Samples, 14)
	request, err := os.ReadFile(query)
	require.NoError(t, err)
	assert.Contains(t, string(request), "repo:acme/security")
	output.Reset()
	require.NoError(t, renderValueInspectedWindows(&output, report))
	assert.Contains(t, output.String(), "5 vulnerabilities")
	assert.NotContains(t, output.String(), "fields")
}

func TestOperationalValueJSONNumber(t *testing.T) {
	git, commit := valueTestGit(t)
	path := "metrics/result[1].json"
	beforeSHA := commit("2026-09-20T12:00:00Z", map[string]string{path: `{"value":12,"population":["b","a"]}`})
	afterSHA := commit("2026-09-28T16:37:55Z", map[string]string{path: `{"value":7,"population":["a","b"]}`})
	measurement := jsonNumberMeasurement{info: valueMeasurementDefinition{
		Unit: "vulnerabilities", Direction: "lower_is_better",
	}, config: jsonNumberConfig{Path: path}}
	before, err := measurement.measure(git, beforeSHA)
	require.NoError(t, err)
	after, err := measurement.measure(git, afterSHA)
	require.NoError(t, err)
	repeated, err := measurement.measure(git, afterSHA)
	require.NoError(t, err)
	assert.Equal(t, after, repeated)
	assert.Equal(t, new(5.0), measurement.change(before, after).Value)
	assert.Equal(t, new(-5.0), measurement.change(after, before).Value)
	assert.Equal(t, new(0.0), measurement.change(after, after).Value)
	assert.Equal(t, "7 vulnerabilities", measurement.format(after))
	changed := after
	changed.Population = new([]string{"a", "c"})
	assert.Equal(t, "numeric_population_changed", measurement.change(before, changed).Reason)
	changed.Population = nil
	assert.Equal(t, "numeric_population_changed", measurement.change(before, changed).Reason)
	measurement.info.Direction = "higher_is_better"
	assert.Equal(t, new(-5.0), measurement.change(before, after).Value)
	assert.Equal(t, "numeric_change_out_of_range", measurement.change(
		valueSnapshot{Value: new(-math.MaxFloat64)}, valueSnapshot{Value: new(math.MaxFloat64)}).Reason)
	nullSHA := commit("2026-10-01T12:00:00Z", map[string]string{path: `{"value":null}`})
	unavailable, err := measurement.measure(git, nullSHA)
	require.NoError(t, err)
	assert.Nil(t, unavailable.Value)
	assert.Equal(t, "numeric_evidence_unavailable", measurement.change(before, unavailable).Reason)
	zeroSHA := commit("2026-10-02T12:00:00Z", map[string]string{path: `{"value":0,"population":[]}`})
	zero, err := measurement.measure(git, zeroSHA)
	require.NoError(t, err)
	assert.Equal(t, new(0.0), zero.Value)
	require.NotNil(t, zero.Population)
	assert.Empty(t, *zero.Population)
	measurement.config.Path = "missing.json"
	_, err = measurement.measure(git, afterSHA)
	require.Error(t, err)
}

func TestOperationalValueJSONNumberMalformed(t *testing.T) {
	git, commit := valueTestGit(t)
	measurement := jsonNumberMeasurement{config: jsonNumberConfig{Path: "value.json"}}
	for _, content := range []string{
		`{}`, `null`, `{"value":"7"}`, `{"value":1e309}`, `{"value":false}`, `{"value":7} {}`,
		`{"value":7,"unexpected":true}`, `{"value":7,"population":["a","a"]}`,
		`{"value":7,"population":[" "]}`, `{"value":7,"population":[1]}`,
		`{"value":7,"population":null}`,
	} {
		sha := commit("2026-10-01T12:00:00Z", map[string]string{"value.json": content})
		_, err := measurement.measure(git, sha)
		require.Error(t, err, content)
	}
}
