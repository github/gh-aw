package cli

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/console"
)

type valueRunGradesReport struct {
	Coverage string          `json:"coverage"`
	Reason   string          `json:"reason,omitempty"`
	Runs     []valueRunGrade `json:"runs"`
}

type valueRunGrade struct {
	RunID      int64        `json:"run_id"`
	RunAttempt int          `json:"run_attempt"`
	CreatedAt  time.Time    `json:"created_at"`
	Grader     GraderResult `json:"grader"`
}

func loadValueRunGrades(path string, definition valueMeasurementDefinition, start, end time.Time) (*valueRunGradesReport, error) {
	data, err := regradeReadEvidence(path, maxGraderPayloadBytes)
	if errors.Is(err, os.ErrNotExist) {
		return &valueRunGradesReport{Coverage: "unavailable", Reason: "logs_summary_not_found", Runs: []valueRunGrade{}}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read recorded value grades: %w", err)
	}
	var document struct {
		Runs json.RawMessage `json:"runs"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, fmt.Errorf("parse recorded value grades: %w", err)
	}
	if len(document.Runs) == 0 {
		return nil, errors.New("recorded value grades require a logs summary containing runs")
	}
	var runs []RunData
	if err := json.Unmarshal(document.Runs, &runs); err != nil {
		return nil, fmt.Errorf("parse recorded value runs: %w", err)
	}
	return buildValueRunGrades(runs, definition, start, end)
}

func buildValueRunGrades(runs []RunData, definition valueMeasurementDefinition, start, end time.Time) (*valueRunGradesReport, error) {
	report := &valueRunGradesReport{Coverage: "unavailable", Reason: "no_matching_completed_runs", Runs: []valueRunGrade{}}
	seen := make(map[string]struct{})
	for _, run := range runs {
		if !strings.EqualFold(run.Repository, definition.Repository) || run.WorkflowPath != definition.WorkflowPath || run.Status != "completed" {
			continue
		}
		// A missing clock cannot establish that a completed run is outside the window.
		if !run.CreatedAt.IsZero() && (run.CreatedAt.Before(start) || run.CreatedAt.After(end)) {
			continue
		}
		runURL, err := url.Parse(run.URL)
		if err != nil {
			return nil, fmt.Errorf("recorded value run %d has an invalid canonical run URL", run.RunID)
		}
		if runURL.Host == "" {
			return nil, fmt.Errorf("recorded value run %d has no canonical run URL", run.RunID)
		}
		if !strings.EqualFold(runURL.Host, definition.Host) {
			continue
		}
		attempt, err := strconv.Atoi(run.RunAttempt)
		expectedPath := fmt.Sprintf("/%s/actions/runs/%d", definition.Repository, run.RunID)
		if run.RunID <= 0 || err != nil || attempt <= 0 || run.CreatedAt.IsZero() ||
			runURL.Scheme != "https" || runURL.User != nil || runURL.RawQuery != "" ||
			runURL.Fragment != "" || !strings.EqualFold(runURL.Path, expectedPath) {
			return nil, fmt.Errorf("recorded value run %d has incomplete or conflicting run identity", run.RunID)
		}
		key := fmt.Sprintf("%d:%d", run.RunID, attempt)
		if _, exists := seen[key]; exists {
			return nil, fmt.Errorf("duplicate recorded value run %s", key)
		}
		seen[key] = struct{}{}
		grade, err := recordedOperationalValueGrade(run)
		if err != nil {
			return nil, err
		}
		// Free-form diagnostics can contain sensitive data. Keep them in the source artifact only.
		grade.Message, grade.Error = "", ""
		report.Runs = append(report.Runs, valueRunGrade{
			RunID: run.RunID, RunAttempt: attempt, CreatedAt: run.CreatedAt, Grader: grade,
		})
	}
	slices.SortFunc(report.Runs, func(a, b valueRunGrade) int {
		if order := a.CreatedAt.Compare(b.CreatedAt); order != 0 {
			return order
		}
		if order := cmp.Compare(a.RunID, b.RunID); order != 0 {
			return order
		}
		return cmp.Compare(a.RunAttempt, b.RunAttempt)
	})
	if len(report.Runs) > 0 {
		report.Coverage, report.Reason = "retained_subset", ""
	}
	return report, nil
}

func recordedOperationalValueGrade(run RunData) (GraderResult, error) {
	grade := GraderResult{ID: "operational-value", Status: "unavailable"}
	if run.Graders == nil {
		return grade, nil
	}
	found := false
	for _, candidate := range run.Graders.Results {
		if candidate.ID != grade.ID {
			continue
		}
		if found {
			return grade, fmt.Errorf("run %d has duplicate operational-value grader results", run.RunID)
		}
		grade, found = candidate, true
		switch grade.Status {
		case "pass", "fail":
			if grade.Value == nil {
				return grade, fmt.Errorf("run %d has an operational-value %s result without a numeric value", run.RunID, grade.Status)
			}
		case "unavailable", "error":
			if grade.Value != nil {
				return grade, fmt.Errorf("run %d has a numeric operational-value result marked %s", run.RunID, grade.Status)
			}
		default:
			return grade, fmt.Errorf("run %d has unsupported operational-value status %q", run.RunID, grade.Status)
		}
	}
	return grade, nil
}

func renderValueRunGrades(out io.Writer, report *valueRunGradesReport) error {
	if report == nil {
		return nil
	}
	if report.Coverage == "unavailable" {
		_, err := fmt.Fprintf(out, "Recorded operational-value grades unavailable: %s.\n", report.Reason)
		return err
	}
	type gradeRow struct {
		Run     int64  `console:"header:Run"`
		Attempt int    `console:"header:Attempt"`
		Created string `console:"header:Created (UTC)"`
		Status  string `console:"header:Grade status"`
		Value   string `console:"header:Recorded value"`
	}
	rows := make([]gradeRow, 0, len(report.Runs))
	for _, run := range report.Runs {
		value := "unavailable"
		if run.Grader.Value != nil {
			value = strings.TrimSpace(strconv.FormatFloat(*run.Grader.Value, 'g', -1, 64) + " " + run.Grader.Unit)
		}
		rows = append(rows, gradeRow{
			Run: run.RunID, Attempt: run.RunAttempt, Created: run.CreatedAt.UTC().Format(time.RFC3339),
			Status: run.Grader.Status, Value: value,
		})
	}
	if _, err := fmt.Fprint(out, console.RenderStructStdout(rows)); err != nil {
		return err
	}
	_, err := fmt.Fprintln(out, "Recorded operational-value grades are a retained run subset, not longitudinal attributed value. Runs are not aggregated or regraded.")
	return err
}
