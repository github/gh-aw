//go:build !integration

package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/github/gh-aw/pkg/lifecycle"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func valueTestTime(t *testing.T, text string) time.Time {
	t.Helper()
	at, err := time.Parse(time.RFC3339, text)
	require.NoError(t, err)
	return at
}

func valueTestRecord(t *testing.T, output valueOutput) lifecycle.Record {
	t.Helper()
	record, err := lifecycle.Delivered(lifecycle.Identity{
		Host: "github.com", Repository: "github/gh-aw", Workflow: ".github/workflows/schema-feature-coverage.lock.yml",
		RunID: output.RunID, RunAttempt: 1, InvocationID: "safe-outputs", EvidenceID: "item-1",
		Type: "create_pull_request", TargetURL: output.URL,
	}, output.Delivered, output.Delivered, "safe-output-manifest")
	require.NoError(t, err)
	record, err = lifecycle.Observe(record, lifecycle.Observation{
		State: "accepted", Signal: "merged", At: output.Merged, ObservedAt: output.Merged.Add(time.Hour),
		AcceptanceKind: "explicit_human", HumanVerified: true,
	})
	require.NoError(t, err)
	return record
}

func TestOperationalValueAttributionGates(t *testing.T) {
	measurement := schemaCoverageMeasurement{}
	merged := valueTestTime(t, "2026-09-28T16:37:55Z")
	output := valueOutput{PR: 63949, URL: "https://github.com/github/gh-aw/pull/63949", RunID: 36389625388, Delivered: merged.Add(-9 * time.Hour), Merged: merged}
	record := valueTestRecord(t, output)
	tests := []struct {
		name   string
		change func(*valueWindow, *[]valueOutput)
		status string
		reason string
		value  *float64
	}{
		{"attained", func(*valueWindow, *[]valueOutput) {}, "attributed", "all_gates_passed", new(5.0)},
		{"observed zero", func(w *valueWindow, _ *[]valueOutput) { w.Change.Value = new(0.0) }, "attributed", "all_gates_passed", new(0.0)},
		{"no native evidence", func(w *valueWindow, _ *[]valueOutput) { w.Adoption = nil }, "unavailable", "native_human_adoption_missing", nil},
		{"snapshot only", func(w *valueWindow, _ *[]valueOutput) { w.Survival = nil }, "pending", "post_adoption_window_incomplete", nil},
		{"short survival window", func(w *valueWindow, _ *[]valueOutput) { w.Survival.Through = merged.Add(6 * 24 * time.Hour) }, "pending", "post_adoption_window_incomplete", nil},
		{"cutoff before window end", func(w *valueWindow, _ *[]valueOutput) { w.End = nil }, "pending", "post_adoption_window_incomplete", nil},
		{"fallback", func(w *valueWindow, _ *[]valueOutput) { w.Adoption.Adoption.AcceptanceKind = "fallback_delivery" }, "unavailable", "native_human_adoption_missing", nil},
		{"not human", func(w *valueWindow, _ *[]valueOutput) { w.Adoption.Adoption.HumanVerified = false }, "unavailable", "native_human_adoption_missing", nil},
		{"not native", func(w *valueWindow, _ *[]valueOutput) { w.Adoption.Adoption.Source = "other" }, "unavailable", "native_human_adoption_missing", nil},
		{"provider unavailable", func(w *valueWindow, _ *[]valueOutput) { w.Adoption.Adoption.Coverage = "unavailable" }, "unavailable", "native_human_adoption_missing", nil},
		{"wrong decision clock", func(w *valueWindow, _ *[]valueOutput) { w.Adoption.Adoption.At = merged.Add(time.Second) }, "unavailable", "native_human_adoption_missing", nil},
		{"invalid measurement delta", func(w *valueWindow, _ *[]valueOutput) { w.Change = valueChange{Reason: "schema_population_changed"} }, "unavailable", "schema_population_changed", nil},
		{"touched file", func(w *valueWindow, _ *[]valueOutput) { w.Survival.Touched = 1 }, "unavailable", "changed_files_touched_in_window", nil},
		{"shared batch", func(_ *valueWindow, outputs *[]valueOutput) {
			*outputs = append(*outputs, valueOutput{PR: 2, Merged: merged})
		}, "unavailable", "shared_workflow_merge_window", nil},
		{"neighbor before", func(_ *valueWindow, outputs *[]valueOutput) {
			*outputs = append(*outputs, valueOutput{PR: 2, Merged: merged.Add(-valueWindowDuration)})
		}, "unavailable", "shared_workflow_merge_window", nil},
		{"neighbor after", func(_ *valueWindow, outputs *[]valueOutput) {
			*outputs = append(*outputs, valueOutput{PR: 2, Merged: merged.Add(valueWindowDuration)})
		}, "unavailable", "shared_workflow_merge_window", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adoption := record
			window := valueWindow{
				PR: output.PR, MergedAt: merged, Change: valueChange{Value: new(5.0)},
				Before: valueSnapshot{Value: new(0.0)}, After: valueSnapshot{Value: new(5.0)},
				Adoption: &adoption, End: &valueSnapshot{At: merged.Add(valueWindowDuration), Value: new(5.0)}, Survival: &valueSurvival{Through: merged.Add(valueWindowDuration)},
			}
			outputs := []valueOutput{output}
			tt.change(&window, &outputs)
			assessValueWindow(&window, outputs)
			assert.Equal(t, tt.status, window.Status)
			assert.Equal(t, tt.reason, window.Reason)
			assert.Equal(t, tt.value, window.Attributed)
		})
	}
	for _, change := range []func(*lifecycle.Record){
		func(r *lifecycle.Record) { r.Host = "elsewhere.example" },
		func(r *lifecycle.Record) { r.Repository = "other/repo" },
		func(r *lifecycle.Record) { r.RunID++ },
		func(r *lifecycle.Record) { r.Workflow = ".github/workflows/other.lock.yml" },
		func(r *lifecycle.Record) { r.TargetURL += "0" },
		func(r *lifecycle.Record) { r.Type = "create_issue" },
	} {
		mismatch := record
		change(&mismatch)
		assert.Nil(t, findValueAdoption(measurement.definition(), output, map[string]lifecycle.Record{"wrong": mismatch}))
	}
	assert.Nil(t, findValueAdoption(measurement.definition(), output, map[string]lifecycle.Record{"one": record, "two": record}), "ambiguous deliveries cannot establish attribution")
}

func valueTestGit(t *testing.T) (valueGit, func(string, map[string]string) string) {
	t.Helper()
	git := valueGit{ctx: t.Context(), dir: t.TempDir()}
	_, err := git.run("init", "--initial-branch=main")
	require.NoError(t, err)
	_, err = git.run("remote", "add", "origin", "https://github.com/github/gh-aw")
	require.NoError(t, err)
	commit := func(clock string, files map[string]string) string {
		t.Helper()
		for name, content := range files {
			path := filepath.Join(git.dir, name)
			require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
			require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
		}
		_, err := git.run("add", ".")
		require.NoError(t, err)
		cmd := exec.CommandContext(t.Context(), "git", "-c", "user.name=Test", "-c", "user.email=test@example.com",
			"-c", "commit.gpgsign=false", "commit", "--allow-empty", "-m", "fixture")
		cmd.Dir = git.dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_DATE="+clock, "GIT_COMMITTER_DATE="+clock)
		output, err := cmd.CombinedOutput()
		require.NoError(t, err, "%s", output)
		sha, err := git.run("rev-parse", "HEAD")
		require.NoError(t, err)
		return sha
	}
	return git, commit
}

func TestOperationalValueGitReplay(t *testing.T) {
	measurement := schemaCoverageMeasurement{}
	git, commit := valueTestGit(t)
	parent := commit("2026-09-20T12:00:00Z", map[string]string{
		"pkg/parser/schemas/main_workflow_schema.json": `{"properties":{"a":{},"b":{},"c":{}}}`,
		".github/workflows/base.md":                    "a:\n  b:\n",       // Only column-zero fields count.
		"schema-demos/nested/b.md":                     "```aw\nb:\n```\n", // Code fences count, as in the slides.
	})
	before, err := measurement.measure(git, parent)
	require.NoError(t, err)
	assert.Equal(t, 2, before.Coverage.Covered)
	assert.Equal(t, []string{"c"}, before.Coverage.Missing)
	merge := commit("2026-09-28T16:37:55Z", map[string]string{"schema-demos/c[1].md": "c:\n"})
	tip := commit("2026-10-10T12:00:00Z", map[string]string{"unrelated.txt": "not a demo"})
	cutoff := valueTestTime(t, "2026-10-06T12:00:00Z")
	resolved, err := git.prepare(measurement.definition(), "main", cutoff)
	require.NoError(t, err)
	assert.Equal(t, tip, resolved)
	output := valueOutput{PR: 1, URL: "https://github.com/github/gh-aw/pull/1", RunID: 42, Delivered: valueTestTime(t, "2026-09-28T07:00:00Z"), Merged: valueTestTime(t, "2026-09-28T16:37:55Z"), SHA: merge}
	record := valueTestRecord(t, output)
	window, err := buildValueWindow(git, measurement, tip, output, cutoff, []valueOutput{output}, map[string]lifecycle.Record{record.ID: record})
	require.NoError(t, err)
	assert.Equal(t, "attributed", window.Status)
	require.NotNil(t, window.Attributed)
	assert.InDelta(t, 1.0, *window.Attributed, 1e-12)
	assert.Equal(t, []string{"c"}, window.Change.Coverage.Closed)
	require.Len(t, window.Samples, 14)
	for i, sample := range window.Samples {
		require.NotNil(t, sample)
		expected := 2
		if i >= 7 {
			expected = 3
		}

		assert.Equal(t, expected, sample.Coverage.Covered)
	}
	require.NotNil(t, window.Survival)
	assert.Zero(t, window.Survival.Touched)
	assert.Equal(t, 1, window.Survival.Files)
	pending, err := buildValueWindow(git, measurement, tip, output, output.Merged.Add(2*24*time.Hour), []valueOutput{output}, nil)
	require.NoError(t, err)
	assert.Equal(t, "pending", pending.Status)
	assert.Nil(t, pending.End)
	assert.Nil(t, pending.Samples[10])
	assert.Nil(t, pending.Attributed)
	// A similarly named file must not match the brackets as a glob.
	tip = commit("2026-10-11T12:00:00Z", map[string]string{"schema-demos/c1.md": "a:\n"})
	survival, err := git.survival(tip, merge, valueTestTime(t, "2026-10-12T12:00:00Z"))
	require.NoError(t, err)
	assert.Zero(t, survival.Touched)
	tip = commit("2026-10-12T12:00:00Z", map[string]string{"schema-demos/c[1].md": "c:\nchanged\n"})
	survival, err = git.survival(tip, merge, valueTestTime(t, "2026-10-13T12:00:00Z"))
	require.NoError(t, err)
	assert.Equal(t, 1, survival.Touched)
	_, err = git.prepare(measurement.definition(), "main", valueTestTime(t, "2026-12-01T12:00:00Z"))
	require.ErrorContains(t, err, "does not cover")
	_, err = git.prepare(measurement.definition(), "--help", cutoff)
	require.ErrorContains(t, err, "must not start")
}

func TestOperationalValueReportAndSearch(t *testing.T) {
	measurement := schemaCoverageMeasurement{}
	git, commit := valueTestGit(t)
	commit("2026-09-20T12:00:00Z", map[string]string{
		"pkg/parser/schemas/main_workflow_schema.json": `{"properties":{"a":{},"b":{}}}`,
		".github/workflows/base.md":                    "a:\n",
	})
	merge := commit("2026-09-28T16:37:55Z", map[string]string{"schema-demos/b.md": "b:\n"})
	tip := commit("2026-10-10T12:00:00Z", nil)
	response := filepath.Join(t.TempDir(), "response.json")
	t.Setenv("GH_AW_TEST_VALUE_RESPONSE", response)
	installOutcomeAPIFixture(t, "cat \"$GH_AW_TEST_VALUE_RESPONSE\"\n")
	footer := "<!-- gh-aw-agentic-workflow: Coverage, workflow_id: schema-feature-coverage, run: https://github.com/github/gh-aw/actions/runs/42 -->"
	item := map[string]any{
		"number": 1, "html_url": "https://github.com/github/gh-aw/pull/1", "body": footer,
		"created_at": "2026-09-28T07:00:00Z", "user": map[string]string{"login": "github-actions[bot]", "type": "Bot"},
		"pull_request": map[string]string{"merged_at": "2026-09-28T16:37:55Z"},
	}
	writeResponse := func(data any) {
		t.Helper()
		content, err := json.Marshal(data)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(response, content, 0o600))
	}
	search := map[string]any{"total_count": 1, "incomplete_results": false, "items": []any{item}}
	writeResponse(search)
	outputs, err := collectValueOutputs(t.Context(), measurement.definition())
	require.NoError(t, err)
	require.Len(t, outputs, 1)
	assert.Equal(t, int64(42), outputs[0].RunID)
	record := valueTestRecord(t, outputs[0])
	writeResponse(map[string]any{"html_url": outputs[0].URL, "merged_at": outputs[0].Merged, "merge_commit_sha": merge})
	start, end, err := operationalValueDates(operationalValueConfig{Start: "2026-09-21", End: "2026-10-06"}, valueTestTime(t, "2026-11-17T12:00:00Z"))
	require.NoError(t, err)
	options := valueReportOptions{Start: start, End: end, ObservedAt: end}
	report, err := buildOperationalValueReport(git, measurement, tip, options, outputs, map[string]lifecycle.Record{record.ID: record})
	require.NoError(t, err)
	require.Len(t, report.Weekly, 4)
	assert.Equal(t, start, report.Weekly[0].At)
	assert.Equal(t, end, report.Weekly[3].At)
	assert.Equal(t, &valueCohort{Fields: []string{"a", "b"}, Before: 1, After: 2}, report.Cohort)
	require.NotNil(t, report.AttributedValue)
	assert.InDelta(t, 1.0, *report.AttributedValue, 1e-12)
	assert.Equal(t, "complete", report.AttributionCoverage)
	var text bytes.Buffer
	require.NoError(t, renderOperationalValueReport(&text, measurement, report))
	assert.Contains(t, text.String(), "verified subtotal")
	report, err = buildOperationalValueReport(git, measurement, tip, options, outputs, nil)
	require.NoError(t, err)
	assert.Nil(t, report.AttributedValue)
	assert.Equal(t, "unavailable", report.AttributionCoverage)
	recipe := &valueRecipe{Author: "app/github-actions", AuthorLogin: "github-actions[bot]"}
	user := item["user"]
	item["user"] = map[string]string{"login": "someone-else"}
	writeResponse(search)
	_, err = collectValueOutputs(t.Context(), measurement.definition(), recipe)
	require.ErrorContains(t, err, "does not match recipe author_login")
	item["user"] = user
	for _, mutation := range []func(){
		func() { item["body"] = "missing footer" },
		func() { item["body"] = footer; search["incomplete_results"] = true },
		func() { search["incomplete_results"] = false; search["total_count"] = 1001 },
		func() { search["total_count"] = 2; search["items"] = []any{item, item} },
	} {
		mutation()
		writeResponse(search)
		_, err := collectValueOutputs(t.Context(), measurement.definition())
		require.Error(t, err)
	}
	require.NoError(t, os.WriteFile(response, []byte("not JSON"), 0o600))
	_, err = collectValueOutputs(t.Context(), measurement.definition())
	require.ErrorContains(t, err, "decode search/issues")
}

func TestOperationalValueInputFailures(t *testing.T) {
	measurement := schemaCoverageMeasurement{}
	now := valueTestTime(t, "2026-11-18T04:00:00Z")
	for _, config := range []operationalValueConfig{
		{Start: "2026-11-18", End: "2026-11-18"},
		{Start: "2026-11-17", End: "2026-11-16"},
		{Start: "invalid", End: "2026-11-17"},
		{Start: "2020-01-01", End: "2026-11-17"},
	} {
		_, _, err := operationalValueDates(config, now)
		require.Error(t, err)
	}
	_, end, err := operationalValueDates(operationalValueConfig{Start: "2026-03-22"}, now)
	require.NoError(t, err)
	assert.Equal(t, "2026-11-17T12:00:00Z", end.Format(time.RFC3339))
	command := NewValueCommand()
	command.SetArgs([]string{"other workflow", "--repo", "acme/repo"})
	err = command.Execute()
	require.ErrorContains(t, err, "workflow must be a workflow ID")
	git, commit := valueTestGit(t)
	empty := commit("2026-09-20T12:00:00Z", map[string]string{
		"pkg/parser/schemas/main_workflow_schema.json": `{"properties":{}}`,
	})
	_, err = measurement.measure(git, empty)
	require.ErrorContains(t, err, "empty schema population")
	missing := commit("2026-09-21T12:00:00Z", map[string]string{
		"pkg/parser/schemas/main_workflow_schema.json": `{"properties":{"a":{}}}`,
	})
	_, err = measurement.measure(git, missing)
	require.ErrorContains(t, err, "Markdown inventory")
	zero := commit("2026-09-22T12:00:00Z", map[string]string{
		".github/workflows/empty.md": "# No schema fields covered\n",
	})
	snapshot, err := measurement.measure(git, zero)
	require.NoError(t, err)
	assert.Equal(t, new(0.0), snapshot.Value, "an observed inventory with no matches is zero, not unavailable")
	require.NoError(t, os.WriteFile(filepath.Join(git.dir, ".git", "shallow"), fmt.Appendf(nil, "%s\n", missing), 0o600))
	_, err = git.prepare(measurement.definition(), "main", now)
	require.ErrorContains(t, err, "requires full Git history")
	assert.NotContains(t, err.Error(), "fetch --unshallow", "the command must not fetch implicitly")
}
func TestOperationalValueDailyEvidence(t *testing.T) {
	git, commit := valueTestGit(t)
	m := schemaCoverageMeasurement{}
	commit("2026-09-20T07:00:00Z", map[string]string{
		"pkg/parser/schemas/main_workflow_schema.json": `{"properties":{"a":{}}}`,
		"schema-demos/example.md":                      "no example yet",
	})
	tip := commit("2026-09-22T07:00:00Z", map[string]string{"schema-demos/example.md": "a:\n"})
	start := valueTestTime(t, "2026-09-21T12:00:00Z")
	end := start.Add(48 * time.Hour)
	record := valueTestRecord(t, valueOutput{RunID: 1, URL: "https://github.com/github/gh-aw/pull/1",
		Delivered: start.Add(-time.Hour), Merged: start.Add(time.Minute)})
	atNoon := valueTestRecord(t, valueOutput{RunID: 2, URL: "https://github.com/github/gh-aw/pull/2",
		Delivered: start, Merged: start.Add(24 * time.Hour)})
	foreign, fallback := record, record
	foreign.Workflow = "other"
	fallback.Adoption.AcceptanceKind = "fallback_delivery"
	records := map[string]lifecycle.Record{"1": record, "2": atNoon, "3": foreign, "4": fallback}
	opts := valueReportOptions{Start: start, End: end, ObservedAt: end, Daily: true}
	report, err := buildOperationalValueReport(git, m, tip, opts, nil, records)
	require.NoError(t, err)
	require.Len(t, report.Daily, 3)
	assert.Equal(t, new(0.0), report.Daily[0].Snapshot.Value)
	assert.Nil(t, report.Daily[0].RetainedAcceptances)
	assert.Equal(t, new(1.0), report.Daily[1].Snapshot.Value)
	assert.Equal(t, new(2), report.Daily[1].RetainedAcceptances)
	assert.Equal(t, "retained_subset", report.Daily[1].AcceptanceCoverage)
	assert.Equal(t, "unknown", report.Daily[2].AcceptanceCoverage)
	var text bytes.Buffer
	require.NoError(t, renderOperationalValueReport(&text, m, report))
	assert.Contains(t, text.String(), "Retained human acceptances")
	opts.Daily = false
	original, err := buildOperationalValueReport(git, m, tip, opts, nil, records)
	require.NoError(t, err)
	report.Daily = nil
	assert.Equal(t, original, report)
}
