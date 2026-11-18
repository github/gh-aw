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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func valueGradeTestRun(t *testing.T, id int64) RunData {
	t.Helper()
	return RunData{
		RunID: id, RunAttempt: "1", Repository: "acme/repo",
		WorkflowPath: ".github/workflows/research.lock.yml", Status: "completed",
		URL:       fmt.Sprintf("https://github.com/acme/repo/actions/runs/%d", id),
		CreatedAt: valueTestTime(t, "2026-09-28T07:00:00Z"),
		Graders: &GradersData{Results: []GraderResult{{
			ID: "operational-value", Status: "pass", Value: new(0.0), Unit: "gaps",
		}}},
	}
}

func writeValueGradeSummary(t *testing.T, dir string, runs []RunData) string {
	t.Helper()
	data, err := json.Marshal(LogsData{Runs: runs})
	require.NoError(t, err)
	path := filepath.Join(dir, "summary.json")
	require.NoError(t, os.WriteFile(path, data, 0o600))
	return path
}

func TestOperationalValueRecordedGradesCommand(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	runs := []RunData{
		valueGradeTestRun(t, 6), valueGradeTestRun(t, 5), valueGradeTestRun(t, 4),
		valueGradeTestRun(t, 3), valueGradeTestRun(t, 2), valueGradeTestRun(t, 1),
	}
	runs[0].Graders.Results[0] = GraderResult{ID: "artifact-production", Status: "pass", Value: new(1.0)}
	runs[1].Graders = nil
	const recordedError = "private verification diagnostic"
	const recordedMessage = "private evidence diagnostic"
	runs[2].Graders.Results[0] = GraderResult{ID: "operational-value", Status: "error", Error: recordedError}
	runs[3].Graders.Results[0] = GraderResult{ID: "operational-value", Status: "unavailable", Message: recordedMessage}
	runs[4].Graders.Results[0].Status = "fail"
	runs[4].Graders.Results[0].Value = new(0.00001)
	path := writeValueGradeSummary(t, dir, runs)
	before, err := os.ReadFile(path)
	require.NoError(t, err)
	t.Setenv("PATH", t.TempDir()) // No git, gh, Node, or grader process is available.

	var first []byte
	for range 2 {
		var output bytes.Buffer
		command := NewValueCommand()
		command.SetOut(&output)
		command.SetArgs([]string{"research", "--repo", "acme/repo", "--logs-dir", dir,
			"--start", "2026-09-21", "--end", "2026-10-06", "--json"})
		require.NoError(t, command.Execute())
		var report operationalValueReport
		require.NoError(t, json.Unmarshal(output.Bytes(), &report))
		require.NotNil(t, report.RunGrades)
		assert.Equal(t, "retained_subset", report.RunGrades.Coverage)
		require.Len(t, report.RunGrades.Runs, 6)
		assert.Equal(t, int64(1), report.RunGrades.Runs[0].RunID)
		assert.Equal(t, new(0.0), report.RunGrades.Runs[0].Grader.Value)
		assert.Equal(t, "gaps", report.RunGrades.Runs[0].Grader.Unit)
		assert.Equal(t, "fail", report.RunGrades.Runs[1].Grader.Status)
		assert.Equal(t, "unavailable", report.RunGrades.Runs[2].Grader.Status)
		assert.Equal(t, "error", report.RunGrades.Runs[3].Grader.Status)
		for _, row := range report.RunGrades.Runs[2:] {
			assert.Nil(t, row.Grader.Value)
		}
		assert.Equal(t, "unavailable", report.RunGrades.Runs[4].Grader.Status)
		assert.Equal(t, "unavailable", report.RunGrades.Runs[5].Grader.Status)
		assert.Equal(t, "measurement_not_configured", report.Reason)
		assert.Nil(t, report.AttributedValue)

		var text bytes.Buffer
		require.NoError(t, renderOperationalValueReport(&text, nil, report))
		assert.Contains(t, text.String(), "Recorded value")
		assert.Contains(t, text.String(), "1e-05 gaps")
		for _, diagnostic := range []string{recordedError, recordedMessage} {
			assert.NotContains(t, output.String(), diagnostic)
			assert.NotContains(t, text.String(), diagnostic)
		}
		assert.Contains(t, text.String(), "not longitudinal attributed value")
		assert.Contains(t, text.String(), "Longitudinal value unavailable")
		// Collection clocks vary between invocations; the recorded grades do not.
		encoded, err := json.Marshal(report.RunGrades)
		require.NoError(t, err)
		if first != nil {
			assert.Equal(t, first, encoded)
		}
		first = encoded
	}
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, before, after)
}

func TestOperationalValueRecordedGradeScope(t *testing.T) {
	definition, err := resolveValueWorkflow("research", "acme/repo")
	require.NoError(t, err)
	start := valueTestTime(t, "2026-09-21T12:00:00Z")
	end := valueTestTime(t, "2026-10-06T12:00:00Z")
	runs := []RunData{valueGradeTestRun(t, 1)}
	for _, change := range []func(*RunData){
		func(run *RunData) { run.Repository = "other/repo" },
		func(run *RunData) { run.WorkflowPath = ".github/workflows/other.lock.yml" },
		func(run *RunData) { run.URL = "https://enterprise.example/acme/repo/actions/runs/2" },
		func(run *RunData) {
			run.CreatedAt = start.Add(-time.Second)
			run.URL = "%"
		},
		func(run *RunData) {
			run.CreatedAt = end.Add(time.Second)
			run.RunAttempt = ""
		},
		func(run *RunData) {
			run.Status = "in_progress"
			run.CreatedAt = time.Time{}
			run.URL = ""
		},
	} {
		run := valueGradeTestRun(t, int64(len(runs)+1))
		change(&run)
		runs = append(runs, run)
	}
	report, err := loadValueRunGrades(writeValueGradeSummary(t, t.TempDir(), runs), definition, start, end)
	require.NoError(t, err)
	require.Len(t, report.Runs, 1)
	assert.Equal(t, int64(1), report.Runs[0].RunID)

	path := writeValueGradeSummary(t, t.TempDir(), runs[1:])
	report, err = loadValueRunGrades(path, definition, start, end)
	require.NoError(t, err)
	assert.Equal(t, "unavailable", report.Coverage)
	assert.Equal(t, "no_matching_completed_runs", report.Reason)
	assert.Empty(t, report.Runs)
}

func TestOperationalValueRecordedGradeFailures(t *testing.T) {
	definition, err := resolveValueWorkflow("research", "acme/repo")
	require.NoError(t, err)
	start := valueTestTime(t, "2026-09-21T12:00:00Z")
	end := valueTestTime(t, "2026-10-06T12:00:00Z")
	for name, change := range map[string]func(*RunData){
		"missing attempt":          func(run *RunData) { run.RunAttempt = "" },
		"conflicting URL":          func(run *RunData) { run.URL = "https://github.com/acme/repo/actions/runs/999" },
		"missing URL":              func(run *RunData) { run.URL = "" },
		"missing clock":            func(run *RunData) { run.CreatedAt = time.Time{} },
		"missing value":            func(run *RunData) { run.Graders.Results[0].Value = nil },
		"unsupported status":       func(run *RunData) { run.Graders.Results[0].Status = "unknown" },
		"value marked unavailable": func(run *RunData) { run.Graders.Results[0].Status = "unavailable" },
		"duplicate grade": func(run *RunData) {
			run.Graders.Results = append(run.Graders.Results, run.Graders.Results[0])
		},
	} {
		t.Run(name, func(t *testing.T) {
			run := valueGradeTestRun(t, 1)
			change(&run)
			_, err := loadValueRunGrades(writeValueGradeSummary(t, t.TempDir(), []RunData{run}), definition, start, end)
			require.Error(t, err)
		})
	}
	run := valueGradeTestRun(t, 1)
	_, err = loadValueRunGrades(writeValueGradeSummary(t, t.TempDir(), []RunData{run, run}), definition, start, end)
	require.ErrorContains(t, err, "duplicate recorded value run")

	path := filepath.Join(t.TempDir(), "summary.json")
	for _, data := range []string{`null`, `{}`, `{"runs":"wrong"}`, `{"runs":[]} {}`} {
		require.NoError(t, os.WriteFile(path, []byte(data), 0o600))
		_, err := loadValueRunGrades(path, definition, start, end)
		require.Error(t, err, data)
	}
	require.NoError(t, os.Remove(path))
	report, err := loadValueRunGrades(path, definition, start, end)
	require.NoError(t, err)
	assert.Equal(t, "logs_summary_not_found", report.Reason)
	assert.Empty(t, report.Runs)
}
