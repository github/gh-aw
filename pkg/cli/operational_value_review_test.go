//go:build !integration

package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOperationalValueTextPreservesStructuredEvidence(t *testing.T) {
	const name = "metric\u0085name"
	const unit = "units\tper task"
	report := operationalValueReport{
		Measurement: valueMeasurementDefinition{Name: name, Unit: unit},
		Evidence: &valueEvidenceReport{Observations: []valueEvidenceResult{
			{Input: valueEvidenceObservation{ID: "id\rone", Subject: "subject\none"}, Value: new(1.0)},
			{Reason: "reason\u202eone"},
		}},
	}
	var text bytes.Buffer
	require.NoError(t, renderOperationalValueReport(&text, valueEvidenceMeasurement{}, report))
	for _, s := range []string{name, unit, "id\rone", "subject\none", "reason\u202eone"} {
		assert.NotContains(t, text.String(), s)
		assert.Contains(t, text.String(), valueDisplayText(s))
	}
	raw, err := json.Marshal(report)
	require.NoError(t, err)
	var unchanged operationalValueReport
	require.NoError(t, json.Unmarshal(raw, &unchanged))
	assert.Equal(t, name, unchanged.Measurement.Name)
	assert.Equal(t, "subject\none", unchanged.Evidence.Observations[0].Input.Subject)
}

func TestOperationalValueGitAndRetainedLimits(t *testing.T) {
	git, commit := valueTestGit(t)
	sha := commit("2026-09-01T10:00:00Z", map[string]string{
		"metric.json": `{"value":0,"population":["first","second"]}`,
	})
	read := func(limit int) ([]byte, error) {
		cmd := exec.CommandContext(t.Context(), "git", "show", sha+":metric.json")
		cmd.Dir = git.dir
		return valueCommandOutput(cmd, limit)
	}
	content, err := read(128)
	require.NoError(t, err)
	_, err = read(len(content))
	require.NoError(t, err)
	truncated, err := read(len(content) - 1)
	require.ErrorContains(t, err, "output limit")
	assert.Nil(t, truncated)

	metric := jsonNumberMeasurement{config: jsonNumberConfig{Path: "metric.json"}}
	day := valueTestTime(t, "2026-09-01T12:00:00Z")
	git.budget = &valueEvidenceBudget{remaining: 400}
	_, err = buildOperationalValueReport(git, metric, sha, valueReportOptions{Start: day, End: day}, nil, nil)
	require.NoError(t, err)
	git.budget = &valueEvidenceBudget{remaining: 400}
	_, err = buildOperationalValueReport(git, metric, sha, valueReportOptions{Start: day, End: day.AddDate(0, 0, 1)}, nil, nil)
	require.ErrorContains(t, err, "retained evidence budget")
}

func TestOperationalValueSummaryReaderBoundaries(t *testing.T) {
	dir := t.TempDir()
	path := writeValueGradeSummary(t, dir, []RunData{valueGradeTestRun(t, 1)})
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gh-aw-export.json"), []byte("{}"), 0o600))
	def, err := resolveValueWorkflow("research", "acme/repo")
	require.NoError(t, err)
	start := valueTestTime(t, "2026-09-01T12:00:00Z")
	read := func(p string) (*valueRunGradesReport, error) {
		return loadValueRunGrades(p, def, start, start.AddDate(0, 1, 0))
	}
	grades, err := read(path)
	require.NoError(t, err)
	require.Len(t, grades.Runs, 1)
	_, err = readRegularGraderEvidence(path, 8)
	require.ErrorContains(t, err, "exceeds")
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(dir, alias); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	grades, err = read(filepath.Join(alias, "summary.json"))
	require.NoError(t, err)
	require.Len(t, grades.Runs, 1)
	missing, err := read(filepath.Join(alias, "absent.json"))
	require.NoError(t, err)
	assert.Equal(t, "logs_summary_not_found", missing.Reason)
	leaf := filepath.Join(dir, "linked-summary.json")
	require.NoError(t, os.Symlink(path, leaf))
	_, err = read(leaf)
	require.ErrorContains(t, err, "regular file")
}

func TestOperationalValueSchemaSignedAndConfigIndependent(t *testing.T) {
	git, commit := valueTestGit(t)
	m := schemaCoverageMeasurement{}
	before := commit("2026-09-01T10:00:00Z", map[string]string{
		m.settings().Schema: `{"properties":{"a":{},"b":{},"c":{}}}`, "schema-demos/fixture.md": "a:\nb:\n",
	})
	after := commit("2026-09-02T10:00:00Z", map[string]string{"schema-demos/fixture.md": "c:\n"})
	lost := commit("2026-09-03T10:00:00Z", map[string]string{"schema-demos/fixture.md": "no matches\n"})
	for key, value := range map[string]string{"grep.lineNumber": "true", "grep.column": "true", "color.grep": "always"} {
		_, err := git.run("config", key, value)
		require.NoError(t, err)
	}
	snapshot := func(sha string) valueSnapshot {
		s, err := m.measure(git, sha)
		require.NoError(t, err)
		return s
	}
	a, b, c := snapshot(before), snapshot(after), snapshot(lost)
	assert.Equal(t, new(2.0), a.Value)
	mixed := m.change(a, b)
	assert.Equal(t, []string{"c"}, mixed.Coverage.Closed)
	assert.Equal(t, new(-1.0), mixed.Value)
	assert.Equal(t, new(-2.0), m.change(a, c).Value)
	assert.Equal(t, new(0.0), m.change(a, a).Value)
}

func TestOperationalValueSearchFiltersBeforePagination(t *testing.T) {
	m := schemaCoverageMeasurement{}.definition()
	tmp := t.TempDir()
	t.Setenv("VALUE_SEARCH", tmp)
	installOutcomeAPIFixture(t, `case "$*" in *" page=1 "*) cat "$VALUE_SEARCH/1";; *" page=2 "*) cat "$VALUE_SEARCH/2";; *) exit 1;; esac`)
	footer := "<!-- gh-aw-agentic-workflow: Coverage, workflow_id: schema-feature-coverage, run: https://github.com/github/gh-aw/actions/runs/42 -->"
	item := valuePullRequest{Number: 1, Body: strings.Replace(footer, "coverage, run:", "coverage-extra, run:", 1)}
	quote := valuePullRequest{Number: 2, Body: "> " + footer}
	valid := valuePullRequest{Number: 3, URL: "https://github.com/github/gh-aw/pull/3", Body: footer,
		CreatedAt: valueTestTime(t, "2026-09-01T12:00:00Z")}
	valid.User.Login = "producer"
	valueWriteJSON(t, filepath.Join(tmp, "1"), map[string]any{"total_count": 3, "items": []valuePullRequest{item, quote}})
	valueWriteJSON(t, filepath.Join(tmp, "2"), map[string]any{"total_count": 3, "items": []valuePullRequest{valid}})
	got, err := collectValueOutputs(t.Context(), m, &valueRecipe{Author: "producer", AuthorLogin: "producer"})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, 3, got[0].PR)
}
