//go:build !integration

package cli

import (
	"fmt"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOperationalValueWholePRBoundaries(t *testing.T) {
	git, commit := valueTestGit(t)
	parent := commit("2026-08-15T10:00:00Z", map[string]string{
		"pkg/parser/schemas/main_workflow_schema.json": `{"properties":{"demo":{}}}`,
		"schema-demos/example.md":                      "demo:\n",
	})
	_, err := git.run("switch", "-c", "topic")
	require.NoError(t, err)
	commit("2026-09-03T10:00:00Z", map[string]string{"one": "first"})
	linear := commit("2026-09-04T10:00:00Z", map[string]string{"two": "last"})
	_, err = git.run("switch", "main")
	require.NoError(t, err)
	_, err = git.run("-c", "commit.gpgsign=false", "-c", "user.name=Test", "-c", "user.email=test@example.com",
		"merge", "--no-ff", "-m", "merge fixture", "topic")
	require.NoError(t, err)
	merge, err := git.run("rev-parse", "HEAD")
	require.NoError(t, err)
	m := schemaCoverageMeasurement{}
	out := valueOutput{PR: 1, URL: "https://github.com/github/gh-aw/pull/1", Merged: valueTestTime(t, "2026-09-04T10:00:00Z")}
	file := filepath.Join(t.TempDir(), "pull.json")
	t.Setenv("VALUE_PULL", file)
	installOutcomeAPIFixture(t, `cat "$VALUE_PULL"`)
	resolve := func(sha string) valueOutput {
		o := out
		valueWriteJSON(t, file, map[string]any{"html_url": o.URL, "merged_at": o.Merged, "merge_commit_sha": sha, "commits": 2})
		require.NoError(t, resolveValueMerge(git, m.definition(), &o))
		return o
	}
	proven := resolve(merge)
	require.Equal(t, parent, proven.base)
	paths, err := git.changedPaths(proven.base, proven.SHA)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"one", "two"}, paths)
	ambiguous := resolve(linear)
	require.Empty(t, ambiguous.base)
	window, err := buildValueWindow(git, m, linear, ambiguous, out.Merged.Add(valueDays(14)), nil, nil)
	require.NoError(t, err)
	assert.Equal(t, valueBoundaryUnavailable, window.Reason)
	assert.Nil(t, window.Attributed)
	assert.Nil(t, window.Change.Value)
	assert.Nil(t, window.Survival)
	assert.Len(t, window.Samples, 14)
	assert.NotNil(t, window.End)
	assert.Empty(t, window.Before.SHA)
	assert.Empty(t, window.ChangedFiles)

	windows := []valueWindow{window, window}
	windows[1].PR = 2
	report := operationalValueReport{Windows: windows}
	require.NoError(t, inspectValueRecipe(&report, &valueRecipe{EarlyBatchesThrough: "2026-09-30"}))
	require.Len(t, report.InspectedWindows, 1)
	assert.Equal(t, []int{1, 2}, report.InspectedWindows[0].PRs)
	assert.Equal(t, valueBoundaryUnavailable, report.InspectedWindows[0].Reason)
	assert.Nil(t, report.InspectedWindows[0].Closed)
}

func TestOperationalValueOverlapLookupBounds(t *testing.T) {
	git, commit := valueTestGit(t)
	commit("2026-09-01T10:00:00Z", nil)
	merge := commit("2026-09-22T12:00:00Z", map[string]string{"a.md": "note"})
	at := valueTestTime(t, "2026-09-22T12:00:00Z")
	start, end := at.Add(-valueDays(1)), at.Add(valueDays(4))
	fixture := t.TempDir()
	t.Setenv("VALUE_PULLS", fixture)
	installOutcomeAPIFixture(t, `case "$*" in *pulls/1) cat "$VALUE_PULLS/1";; *pulls/2) cat "$VALUE_PULLS/2";; *pulls/4) cat "$VALUE_PULLS/4";; *) exit 1;; esac`)
	policy := defaultValueWindowPolicy()
	policy.Overlap, policy.IsolationBeforeDays, policy.IsolationAfterDays = "changed_files", 2, 1
	merges := []time.Time{at, at.Add(-valueDays(2)), at.Add(-valueDays(2) - time.Second), at.Add(valueDays(1)), at.Add(valueDays(1) + time.Second), end.Add(time.Second)}
	var outs []valueOutput
	for i, tMerge := range merges {
		n := i + 1
		o := valueOutput{PR: n, Merged: tMerge, Delivered: start.Add(-valueDays(5)), URL: fmt.Sprintf("https://github.com/github/gh-aw/pull/%d", n)}
		if n == 1 {
			o.Delivered = start
		}
		commits := 1
		if n == 2 {
			commits = 2 // An ambiguous neighbor still matters for isolation.
		}
		valueWriteJSON(t, filepath.Join(fixture, strconv.Itoa(n)), map[string]any{"html_url": o.URL, "merged_at": tMerge, "merge_commit_sha": merge, "commits": commits})
		outs = append(outs, o)
	}
	require.NoError(t, prepareValueOverlap(git, schemaCoverageMeasurement{}.definition(), outs, start, end, policy))
	assert.Equal(t, []string{"a.md"}, outs[0].Files)
	assert.Equal(t, merge, outs[1].SHA)
	assert.Nil(t, outs[1].Files)
	assert.Empty(t, outs[2].SHA)
	assert.Equal(t, []string{"a.md"}, outs[3].Files)
	assert.Empty(t, outs[4].SHA)
	assert.Empty(t, outs[5].SHA)
	w := valueWindow{PR: 1, MergedAt: at, ChangedFiles: outs[0].Files, Policy: policy}
	assert.Equal(t, "overlap_evidence_missing", valueOverlapReason(&w, outs))
}
