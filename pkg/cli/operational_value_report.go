package cli

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/lifecycle"
)

const valueWindowDuration = 7 * 24 * time.Hour

type operationalValueReport struct {
	SchemaVersion       int                        `json:"schema_version"`
	Measurement         valueMeasurementDefinition `json:"measurement"`
	MeasurementStatus   string                     `json:"measurement_status"`
	Reason              string                     `json:"reason,omitempty"`
	Lifecycle           valueLifecycleReport       `json:"lifecycle"`
	Ref                 string                     `json:"ref"`
	Start               time.Time                  `json:"start"`
	End                 time.Time                  `json:"end"`
	ObservedAt          time.Time                  `json:"observed_at"`
	Weekly              []valueSnapshot            `json:"weekly"`
	Cohort              *valueCohort               `json:"cohort,omitempty"`
	Outputs             []valueOutput              `json:"outputs"`
	Windows             []valueWindow              `json:"windows"`
	AttributedValue     *float64                   `json:"attributed_value"`
	AttributionCoverage string                     `json:"attribution_coverage"`
	Limitations         []string                   `json:"limitations"`
	Recipe              *valueRecipeEvidence       `json:"recipe,omitempty"`
	InspectedWindows    []valueInspectedWindow     `json:"inspected_windows,omitempty"`
}

type valueWindow struct {
	PR         int               `json:"pr"`
	RunID      int64             `json:"run_id"`
	MergedAt   time.Time         `json:"merged_at"`
	Before     valueSnapshot     `json:"before"`
	After      valueSnapshot     `json:"after"`
	End        *valueSnapshot    `json:"end"`
	Samples    []*valueSnapshot  `json:"samples"`
	Change     valueChange       `json:"change"`
	Survival   *valueSurvival    `json:"survival"`
	Adoption   *lifecycle.Record `json:"adoption"`
	Status     string            `json:"status"`
	Reason     string            `json:"reason"`
	Attributed *float64          `json:"attributed_value"`
}

func buildOperationalValueReport(git valueGit, measurement valueMeasurement, tip string, start, end, now time.Time, outputs []valueOutput, records map[string]lifecycle.Record) (operationalValueReport, error) {
	report := newOperationalValueReport(measurement.definition(), start, end, now, records)
	report.Ref, report.MeasurementStatus = tip, "configured"
	report.Reason = ""
	for at := start; ; at = at.Add(valueWindowDuration) {
		at = minValueTime(at, end)
		snapshot, err := git.snapshot(measurement, tip, at)
		if err != nil {
			return report, err
		}
		report.Weekly = append(report.Weekly, snapshot)
		if at.Equal(end) {
			break
		}
	}
	report.Cohort = measurement.cohort(report.Weekly)
	for _, output := range outputs {
		if output.Delivered.Before(start) || output.Delivered.After(end) {
			continue
		}
		if output.Merged.After(end) {
			output.Merged = time.Time{}
		}
		if !output.Merged.IsZero() {
			sha, err := resolveValueMerge(git.ctx, measurement.definition(), output)
			if err != nil {
				return report, err
			}
			output.SHA = sha
			window, err := buildValueWindow(git, measurement, tip, output, end, outputs, records)
			if err != nil {
				return report, fmt.Errorf("measure PR %d: %w", output.PR, err)
			}
			report.Windows = append(report.Windows, window)
		}
		report.Outputs = append(report.Outputs, output)
	}
	err := summarizeValueWindows(&report)
	return report, err
}

func summarizeValueWindows(report *operationalValueReport) error {
	assessed, total := 0, 0.0
	for _, window := range report.Windows {
		if window.Attributed != nil {
			assessed++
			total += *window.Attributed
		}
	}
	if math.IsNaN(total) || math.IsInf(total, 0) {
		return errors.New("attributed-value subtotal exceeds the finite numeric range")
	}
	if assessed > 0 {
		report.AttributedValue = &total
		report.AttributionCoverage = "partial"
		if assessed == len(report.Windows) {
			report.AttributionCoverage = "complete"
		}
	}
	return nil
}

func newOperationalValueReport(definition valueMeasurementDefinition, start, end, now time.Time, records map[string]lifecycle.Record) operationalValueReport {
	return operationalValueReport{
		SchemaVersion: 1, Measurement: definition, Start: start, End: end, ObservedAt: now,
		MeasurementStatus: "unavailable", Reason: "measurement_not_configured",
		Lifecycle: reportValueLifecycle(definition, start, end, records),
		Weekly:    []valueSnapshot{}, Outputs: []valueOutput{}, Windows: []valueWindow{},
		AttributionCoverage: "unavailable",
		Limitations: []string{
			"Retrospective replay, not a prospectively adopted measurement contract.",
			"Repository measurements include all authors and do not prove exclusive causality or saved time.",
			"PR search is a producer-linked subset, not complete native delivery history.",
			"Merges are adoption proxies; only matching native explicit-human adoption can establish acceptance.",
			"Attributed value is a verified subtotal; unavailable windows are not zero.",
			"Isolation excludes other producer-linked workflow merges, not every possible confounder.",
		},
	}
}

func buildValueWindow(git valueGit, measurement valueMeasurement, tip string, output valueOutput, cutoff time.Time, outputs []valueOutput, records map[string]lifecycle.Record) (valueWindow, error) {
	window := valueWindow{
		PR: output.PR, RunID: output.RunID, MergedAt: output.Merged,
		Samples: []*valueSnapshot{},
	}
	// Ancestor checks alone would admit side-branch commits into a main-only replay.
	chain, err := git.run("rev-list", "--first-parent", tip)
	if err != nil {
		return window, err
	}
	if !slices.Contains(strings.Fields(chain), output.SHA) {
		return window, errors.New("merge is not on the selected first-parent main history")
	}
	parent, err := git.run("rev-parse", output.SHA+"^1")
	if err != nil {
		return window, err
	}
	window.Before, err = measurement.measure(git, parent)
	if err != nil {
		return window, err
	}
	window.After, err = measurement.measure(git, output.SHA)
	if err != nil {
		return window, err
	}
	window.After.At = output.Merged
	window.Change = measurement.change(window.Before, window.After)
	if err := collectValueWindowEvidence(git, measurement, tip, cutoff, &window); err != nil {
		return window, err
	}
	window.Adoption = findValueAdoption(measurement.definition(), output, records)
	assessValueWindow(&window, outputs)
	return window, nil
}

func collectValueWindowEvidence(git valueGit, measurement valueMeasurement, tip string, cutoff time.Time, window *valueWindow) error {
	for day := -7; day < 7; day++ {
		at := window.MergedAt.Add(time.Duration(day) * 24 * time.Hour)
		if at.After(cutoff) {
			window.Samples = append(window.Samples, nil)
			continue
		}
		snapshot := window.After
		if day != 0 {
			var err error
			snapshot, err = git.snapshot(measurement, tip, at)
			if err != nil {
				return err
			}
		}
		window.Samples = append(window.Samples, &snapshot)
	}
	end := window.MergedAt.Add(valueWindowDuration)
	if !end.After(cutoff) {
		snapshot, err := git.snapshot(measurement, tip, end)
		if err != nil {
			return err
		}
		window.End = &snapshot
		survival, err := git.survival(tip, window.After.SHA, end)
		if err != nil {
			return err
		}
		window.Survival = &survival
	}
	return nil
}

func findValueAdoption(definition valueMeasurementDefinition, output valueOutput, records map[string]lifecycle.Record) *lifecycle.Record {
	var found *lifecycle.Record
	for _, record := range records {
		if record.Host != definition.Host || record.Repository != definition.Repository || record.RunID != output.RunID ||
			record.Workflow != definition.WorkflowPath ||
			record.Type != "create_pull_request" || record.TargetURL != output.URL {
			continue
		}
		if found != nil {
			return nil
		}
		copy := record
		found = &copy
	}
	return found
}

func assessValueWindow(window *valueWindow, outputs []valueOutput) {
	window.Status, window.Reason, window.Attributed = "unavailable", "native_human_adoption_missing", nil
	adoption := window.Adoption
	switch {
	case window.End == nil || window.Survival == nil ||
		!window.End.At.Equal(window.MergedAt.Add(valueWindowDuration)) ||
		!window.Survival.Through.Equal(window.MergedAt.Add(valueWindowDuration)):
		window.Status, window.Reason = "pending", "post_adoption_window_incomplete"
	case adoption == nil || adoption.Delivery.State != "delivered" || adoption.Adoption.State != "accepted" ||
		adoption.Adoption.Coverage != "available" || adoption.Adoption.AcceptanceKind != "explicit_human" ||
		adoption.Adoption.Source != "github-api" || !adoption.Adoption.HumanVerified ||
		!adoption.Adoption.At.Equal(window.MergedAt):
	case window.Change.Value == nil:
		window.Reason = window.Change.Reason
	case slices.ContainsFunc(window.Samples, func(snapshot *valueSnapshot) bool {
		return snapshot == nil || snapshot.Value == nil
	}) || window.Before.Value == nil || window.After.Value == nil || window.End.Value == nil:
		window.Reason = "measurement_unavailable_in_window"
	case slices.ContainsFunc(outputs, func(other valueOutput) bool {
		delta := other.Merged.Sub(window.MergedAt)
		return other.PR != window.PR && !other.Merged.IsZero() && delta >= -valueWindowDuration && delta <= valueWindowDuration
	}):
		window.Reason = "shared_workflow_merge_window"
	case window.Survival.Touched != 0:
		window.Reason = "changed_files_touched_in_window"
	default:
		window.Status, window.Reason, window.Attributed = "attributed", "all_gates_passed", window.Change.Value
	}
}

func minValueTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
