//go:build !integration

package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/github/gh-aw/pkg/lifecycle"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOperationalValueRecipeCommand(t *testing.T) {
	definitionPath, err := filepath.Abs("../../.github/aw/value/schema-feature-coverage.json")
	require.NoError(t, err)
	git, commit := valueTestGit(t)
	commit("2026-03-20T12:00:00Z", map[string]string{
		"pkg/parser/schemas/main_workflow_schema.json": `{"properties":{"a":{},"b":{},"c":{},"d":{},"e":{},"f":{},"g":{},"h":{}}}`,
		".github/workflows/base.md":                    "a:\n",
	})
	batch1 := commit("2026-05-04T10:00:00Z", map[string]string{"schema-demos/b.md": "b:\n"})
	batch2 := commit("2026-05-04T11:00:00Z", map[string]string{"schema-demos/c.md": "c:\n"})
	witness := commit("2026-09-28T16:37:55Z", map[string]string{"schema-demos/new.md": "d:\ne:\nf:\ng:\nh:\n"})
	tip := commit("2026-11-18T12:00:00Z", nil)
	shas := []string{batch1, batch2, witness}
	clocks := []string{"2026-05-04T10:00:00Z", "2026-05-04T11:00:00Z", "2026-09-28T16:37:55Z"}
	fixture := t.TempDir()
	items := []valuePullRequest{}
	var witnessOutput valueOutput
	for i, sha := range shas {
		pr := i + 1
		at := valueTestTime(t, clocks[i])
		url := fmt.Sprintf("https://github.com/github/gh-aw/pull/%d", pr)
		item := valuePullRequest{Number: pr, URL: url, CreatedAt: at.Add(-time.Hour),
			Body: fmt.Sprintf("<!-- gh-aw-agentic-workflow: Coverage, workflow_id: schema-feature-coverage, run: https://github.com/github/gh-aw/actions/runs/%d -->", pr)}
		item.User.Login, item.PullRequest.MergedAt = "github-actions[bot]", at
		items = append(items, item)
		valueWriteJSON(t, filepath.Join(fixture, fmt.Sprintf("pr-%d.json", pr)),
			map[string]any{"html_url": url, "merged_at": at, "merge_commit_sha": sha, "commits": 1})
		if i == 2 {
			witnessOutput = valueOutput{PR: pr, RunID: int64(pr), URL: url, Delivered: item.CreatedAt, Merged: at}
		}
	}
	valueWriteJSON(t, filepath.Join(fixture, "search.json"), map[string]any{"total_count": len(items), "items": items})
	recipe := valueRecipe{Version: 1, ID: "synthetic-slides-v1", Repository: "github.com/github/gh-aw",
		Workflow: "schema-feature-coverage", Start: "2026-03-22", End: "2026-11-17",
		Author: "app/github-actions", AuthorLogin: "github-actions[bot]", EarlyBatchesThrough: "2026-06-30", Witnesses: []int{3}}
	path := filepath.Join(fixture, "recipe.json")
	valueWriteJSON(t, path, recipe)
	record := valueTestRecord(t, witnessOutput)
	require.NoError(t, writeLifecycleRecords(filepath.Join(fixture, "logs", "lifecycle.jsonl"), map[string]lifecycle.Record{record.ID: record}))
	t.Setenv("GH_AW_TEST_VALUE_FIXTURE", fixture)
	installOutcomeAPIFixture(t, `case "$*" in
*search/issues*) printf '%s\n' "$*" > "$GH_AW_TEST_VALUE_FIXTURE/query"; cat "$GH_AW_TEST_VALUE_FIXTURE/search.json";;
*pulls/1*) cat "$GH_AW_TEST_VALUE_FIXTURE/pr-1.json";;
*pulls/2*) cat "$GH_AW_TEST_VALUE_FIXTURE/pr-2.json";;
*pulls/3*) cat "$GH_AW_TEST_VALUE_FIXTURE/pr-3.json";;
*) exit 1;;
esac
`)
	t.Chdir(git.dir)
	var output bytes.Buffer
	command := NewValueCommand()
	command.SetOut(&output)
	command.SetArgs([]string{"schema-feature-coverage", "--repo", "github/gh-aw", "--ref", "main",
		"--value-function", definitionPath, "--recipe", path, "--logs-dir", filepath.Join(fixture, "logs"), "--json"})
	require.NoError(t, command.Execute())
	var report operationalValueReport
	require.NoError(t, json.Unmarshal(output.Bytes(), &report))
	require.NotNil(t, report.Recipe)
	assert.Contains(t, report.Recipe.Digest, "sha256:")
	assert.Contains(t, report.Measurement.Digest, "sha256:")
	assert.Equal(t, tip, report.Ref)
	require.Len(t, report.Weekly, 36)
	require.Len(t, report.Windows, 3)
	require.Len(t, report.InspectedWindows, 2)
	batch, single := report.InspectedWindows[0], report.InspectedWindows[1]
	assert.Equal(t, []int{1, 2}, batch.PRs)
	assert.Equal(t, []string{"b", "c"}, batch.Closed)
	assert.Equal(t, clocks[0], batch.First.Format(time.RFC3339))
	assert.Equal(t, clocks[1], batch.Last.Format(time.RFC3339))
	assert.Nil(t, batch.Attributed)
	assert.Equal(t, "batch_not_attributed", batch.Reason)
	assert.Equal(t, new(5.0), single.Attributed)
	assert.Equal(t, new(5.0), report.AttributedValue)
	for _, inspected := range report.InspectedWindows {
		require.Len(t, inspected.Samples, 14)
		for _, sample := range inspected.Samples {
			require.NotNil(t, sample)
		}
	}
	query, err := os.ReadFile(filepath.Join(fixture, "query"))
	require.NoError(t, err)
	assert.Contains(t, string(query), "author:app/github-actions")
	output.Reset()
	require.NoError(t, renderOperationalValueReport(&output, schemaCoverageMeasurement{}, report))
	assert.Contains(t, output.String(), "batch_not_attributed")
	assert.Contains(t, output.String(), "5 fields")
	command.SetArgs([]string{"schema-feature-coverage", "--repo", "github/gh-aw", "--recipe", path, "--end", recipe.End})
	require.ErrorContains(t, command.Execute(), "do not combine")
}

func valueWriteJSON(t *testing.T, path string, value any) {
	t.Helper()
	content, err := json.Marshal(value)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, content, 0o600))
}

func TestOperationalValueRecipeValidation(t *testing.T) {
	definition := schemaCoverageMeasurement{}.definition()
	path := filepath.Join(t.TempDir(), "recipe.json")
	base := valueRecipe{Version: 1, ID: "recipe-v1", Repository: "github.com/github/gh-aw",
		Workflow: definition.Workflow, Start: "2026-03-22", End: "2026-11-17", Witnesses: []int{1}}
	for _, mutate := range []func(*valueRecipe){
		func(r *valueRecipe) { r.Version++ },
		func(r *valueRecipe) { r.Repository = "github.com/other/repo" },
		func(r *valueRecipe) { r.Workflow = "other" },
		func(r *valueRecipe) { r.Start = "invalid" },
		func(r *valueRecipe) { r.EarlyBatchesThrough = "2026-12-01" },
		func(r *valueRecipe) { r.Author = "app/github-actions" },
		func(r *valueRecipe) { r.Author, r.AuthorLogin = "bot is:issue", "bot" },
		func(r *valueRecipe) { r.Witnesses = []int{1, 1} },
		func(r *valueRecipe) { r.Witnesses = []int{0} },
	} {
		recipe := base
		mutate(&recipe)
		valueWriteJSON(t, path, recipe)
		_, err := loadValueRecipe(path, definition)
		require.Error(t, err)
	}
	report := operationalValueReport{Measurement: definition}
	require.ErrorContains(t, inspectValueRecipe(&report, &base), "not a merged output")
}

func TestOperationalValueSlidesWindowSelection(t *testing.T) {
	recipe, err := loadValueRecipe("../../.github/aw/value/schema-feature-coverage.slides.json", schemaCoverageMeasurement{}.definition())
	require.NoError(t, err)
	report := operationalValueReport{Measurement: schemaCoverageMeasurement{}.definition()}
	for day, prs := range map[string][]int{
		"2026-05-04": {1, 2}, "2026-05-18": {3, 4}, "2026-06-15": {5, 6},
		"2026-09-28": {63949}, "2026-10-26": {72028}, "2026-11-02": {74061},
		"2026-08-17": {7}, // An uninspected merge must stay outside the selected rows.
	} {
		for i, pr := range prs {
			at := valueTestTime(t, day+"T10:00:00Z").Add(time.Duration(i) * time.Hour)
			window := valueWindow{
				PR: pr, MergedAt: at, Before: valueSnapshot{Value: new(1.0)},
				After: valueSnapshot{Value: new(2.0)}, Change: valueChange{Value: new(1.0),
					Coverage: &valueCoverageChange{Closed: []string{"same-gap"}, Removed: []string{"retired"}}},
				Status: "unavailable", Reason: "native_human_adoption_missing",
			}
			for d := -7; d < 7; d++ {
				window.Samples = append(window.Samples, &valueSnapshot{At: at.Add(time.Duration(d) * 24 * time.Hour)})
			}
			report.Windows = append(report.Windows, window)
		}
	}
	require.NoError(t, inspectValueRecipe(&report, recipe))
	require.Len(t, report.InspectedWindows, 6)
	for _, row := range report.InspectedWindows {
		require.Len(t, row.Samples, 14)
		assert.Equal(t, row.First.Add(-7*24*time.Hour), row.Samples[0].At)
		assert.Equal(t, row.Last, row.Samples[7].At)
		assert.Equal(t, []string{"same-gap"}, row.Closed, "batch gaps must be a set, not a sum")
		assert.Equal(t, []string{"retired"}, row.Removed)
		assert.Nil(t, row.Attributed, "neither selected witnesses nor batches establish human adoption")
	}
	first := report.InspectedWindows[0]
	assert.Equal(t, "batch", first.Kind)
	assert.Equal(t, []int{1, 2}, first.PRs)
	assert.Equal(t, "May 4", first.Label)
	require.NoError(t, inspectValueRecipe(&report, recipe))
	require.Len(t, report.InspectedWindows, 6, "repeated selection must not append duplicate rows")
	recipe.Witnesses = []int{1}
	require.ErrorContains(t, inspectValueRecipe(&report, recipe), "also belongs to an early batch")
}
