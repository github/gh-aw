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
	WindowPolicy        valueWindowPolicy          `json:"window_policy"`
	Evidence            *valueEvidenceReport       `json:"evidence,omitempty"`
	RunGrades           *valueRunGradesReport      `json:"run_grades,omitempty"`
}

type valueWindow struct {
	PR                   int               `json:"pr"`
	RunID                int64             `json:"run_id"`
	MergedAt             time.Time         `json:"merged_at"`
	Before               valueSnapshot     `json:"before"`
	After                valueSnapshot     `json:"after"`
	End                  *valueSnapshot    `json:"end"`
	Samples              []*valueSnapshot  `json:"samples"`
	Change               valueChange       `json:"change"`
	Survival             *valueSurvival    `json:"survival"`
	Adoption             *lifecycle.Record `json:"adoption"`
	Status               string            `json:"status"`
	Reason               string            `json:"reason"`
	Attributed           *float64          `json:"attributed_value"`
	Policy               valueWindowPolicy `json:"policy"`
	Outcome              *valueSnapshot    `json:"outcome"`
	ObservationThrough   time.Time         `json:"observation_through,omitzero"`
	ObservationsComplete bool              `json:"observations_complete"`
	ChangedFiles         []string          `json:"changed_files,omitempty"`
}

type valueReportOptions struct {
	Start, End, ObservedAt time.Time
	Policy                 valueWindowPolicy
}

func buildOperationalValueReport(git valueGit, measurement valueMeasurement, tip string, options valueReportOptions, outputs []valueOutput, records map[string]lifecycle.Record) (operationalValueReport, error) {
	start, end := options.Start, options.End
	report := newOperationalValueReport(measurement.definition(), start, end, options.ObservedAt, records)
	report.WindowPolicy = options.Policy.resolved()
	if report.WindowPolicy.Overlap == "changed_files" {
		if err := prepareValueOverlap(git, measurement.definition(), outputs, end); err != nil {
			return report, err
		}
	}
	report.Ref, report.MeasurementStatus = tip, "configured"
	report.Reason = ""
	for at := start; ; at = at.Add(valueDays(report.WindowPolicy.TrendEveryDays)) {
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
			if output.SHA == "" {
				sha, err := resolveValueMerge(git.ctx, measurement.definition(), output)
				if err != nil {
					return report, err
				}
				output.SHA = sha
			}
			window, err := buildValueWindow(git, measurement, tip, output, end, outputs, records, report.WindowPolicy)
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
		WindowPolicy:      defaultValueWindowPolicy(),
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

func buildValueWindow(git valueGit, measurement valueMeasurement, tip string, output valueOutput, cutoff time.Time, outputs []valueOutput, records map[string]lifecycle.Record, policies ...valueWindowPolicy) (valueWindow, error) {
	window := valueWindow{
		PR: output.PR, RunID: output.RunID, MergedAt: output.Merged,
		Samples:      []*valueSnapshot{},
		ChangedFiles: output.Files,
	}
	if len(policies) > 0 {
		window.Policy = policies[0]
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
	if err := collectValueWindowEvidence(git, measurement, tip, cutoff, &window); err != nil {
		return window, err
	}
	window.Change = valueChange{Reason: "outcome_observation_pending"}
	if window.Outcome != nil {
		window.Change = measurement.change(window.Before, *window.Outcome)
	}
	window.Adoption = findValueAdoption(measurement.definition(), output, records)
	assessValueWindow(&window, outputs)
	return window, nil
}

func collectValueWindowEvidence(git valueGit, measurement valueMeasurement, tip string, cutoff time.Time, window *valueWindow) error {
	policy := window.Policy.resolved()
	step := time.Duration(policy.SampleEveryHours) * time.Hour
	for offset := -valueDays(policy.BeforeDays); offset < valueDays(policy.AfterDays); offset += step {
		at := window.MergedAt.Add(offset)
		if offset >= 0 {
			at = at.Add(valueDays(policy.OutcomeLagDays))
		}
		if at.After(cutoff) {
			window.Samples = append(window.Samples, nil)
			continue
		}
		snapshot := window.After
		if !at.Equal(window.MergedAt) {
			var err error
			snapshot, err = git.snapshot(measurement, tip, at)
			if err != nil {
				return err
			}
		}
		window.Samples = append(window.Samples, &snapshot)
	}
	outcomeAt := window.MergedAt.Add(valueDays(policy.OutcomeLagDays))
	if !outcomeAt.After(cutoff) {
		outcome := window.After
		if policy.OutcomeLagDays != 0 {
			var err error
			outcome, err = git.snapshot(measurement, tip, outcomeAt)
			if err != nil {
				return err
			}
		}
		window.Outcome = &outcome
	}
	window.ObservationThrough = window.MergedAt.Add(valueDays(max(policy.OutcomeLagDays+policy.AfterDays, policy.DurabilityDays, policy.IsolationAfterDays)))
	window.ObservationsComplete = !window.ObservationThrough.After(cutoff)
	end := window.MergedAt.Add(valueDays(policy.DurabilityDays))
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
	policy := window.Policy.resolved()
	window.Status, window.Reason, window.Attributed = "unavailable", "native_human_adoption_missing", nil
	adoption := window.Adoption
	switch {
	case window.End == nil || window.Survival == nil ||
		!window.End.At.Equal(window.MergedAt.Add(valueDays(policy.DurabilityDays))) ||
		!window.Survival.Through.Equal(window.MergedAt.Add(valueDays(policy.DurabilityDays))):
		window.Status, window.Reason = "pending", "post_adoption_window_incomplete"
	case !window.ObservationThrough.IsZero() && !window.ObservationsComplete:
		window.Status, window.Reason = "pending", "outcome_window_incomplete"
	case adoption == nil || adoption.Delivery.State != "delivered" || adoption.Adoption.State != "accepted" ||
		adoption.Adoption.Coverage != "available" || adoption.Adoption.AcceptanceKind != "explicit_human" ||
		adoption.Adoption.Source != "github-api" || !adoption.Adoption.HumanVerified ||
		!adoption.Adoption.At.Equal(window.MergedAt):
	case window.Change.Value == nil:
		window.Reason = window.Change.Reason
	case !valueSamplesAvailable(window) || window.Before.Value == nil || window.After.Value == nil || window.End.Value == nil:
		window.Reason = "measurement_unavailable_in_window"
	case policy.OutcomeLagDays > 0:
		window.Reason = "delayed_outcome_attribution_requires_subject_linkage"
	case valueOverlapReason(window, outputs) != "":
		window.Reason = valueOverlapReason(window, outputs)
	case window.Survival.Touched != 0:
		window.Reason = "changed_files_touched_in_window"
	default:
		window.Status, window.Reason, window.Attributed = "attributed", "all_gates_passed", window.Change.Value
	}
}

func valueSamplesAvailable(window *valueWindow) bool {
	if len(window.Samples) == 0 {
		return window.Policy.Version == 0
	}
	policy := window.Policy.resolved()
	pre := policy.BeforeDays * 24 / policy.SampleEveryHours
	if len(window.Samples) != pre+policy.AfterDays*24/policy.SampleEveryHours {
		return false
	}
	count := func(samples []*valueSnapshot) int {
		return len(slices.DeleteFunc(slices.Clone(samples), func(s *valueSnapshot) bool { return s == nil || s.Value == nil }))
	}
	return count(window.Samples[:pre]) >= policy.MinimumBefore && count(window.Samples[pre:]) >= policy.MinimumAfter
}

func minValueTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
