package cli

import (
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/console"
)

type valueEvidenceReport struct {
	Digest       string                `json:"digest,omitempty"`
	Scope        string                `json:"scope"`
	Trust        string                `json:"trust"`
	Coverage     string                `json:"coverage"`
	Observations []valueEvidenceResult `json:"observations"`
	Changes      []valueEvidenceChange `json:"changes"`
}

func renderValueEvidenceReport(out io.Writer, report operationalValueReport) error {
	type row struct {
		Observation string `console:"header:Observation"`
		Subject     string `console:"header:Subject"`
		Value       string `console:"header:Measurement"`
	}
	rows := make([]row, 0, len(report.Evidence.Observations))
	for _, result := range report.Evidence.Observations {
		value := "unavailable: " + valueDisplayText(result.Reason)
		if result.Value != nil {
			value = fmt.Sprintf("%g %s", *result.Value, valueDisplayText(report.Measurement.Unit))
		}
		rows = append(rows, row{valueDisplayText(result.Input.ID), valueDisplayText(result.Input.Subject), value})
	}
	if _, err := fmt.Fprint(out, console.RenderStructStdout(rows)); err != nil {
		return err
	}
	for _, change := range report.Evidence.Changes {
		if change.Change.Value != nil {
			if _, err := fmt.Fprintf(out, "Measured improvement: %g %s; attributed value unavailable (%s).\n", *change.Change.Value, valueDisplayText(report.Measurement.Unit), change.AttributionReason); err != nil {
				return err
			}
		}
	}
	_, err := fmt.Fprintf(out, "Evidence coverage: %s. Caller-supplied observations are not native adoption, full-window survival, or causal proof.\n", report.Evidence.Coverage)
	return err
}

type valueEvidenceChange struct {
	Subject           string      `json:"subject"`
	BaselineID        string      `json:"baseline_id"`
	OutcomeID         string      `json:"outcome_id"`
	Change            valueChange `json:"change"`
	MaturesAt         time.Time   `json:"matures_at"`
	WindowMature      bool        `json:"window_mature"`
	SamplingCoverage  string      `json:"sampling_coverage"`
	Attributed        *float64    `json:"attributed_value"`
	AttributionReason string      `json:"attribution_reason"`
}

func collectValueEvidenceReport(report *operationalValueReport, measurement valueEvidenceMeasurement, filename string, recipe *valueRecipe) error {
	report.Measurement = measurement.info
	report.MeasurementStatus, report.Reason = "configured", "required_evidence_missing"
	policy, err := resolveValuePolicy(recipe, "paired_observation")
	if err != nil {
		return err
	}
	if policy.Baseline != "paired_observation" {
		return errors.New("evidence-only measurement requires paired_observation baseline; imported SHAs do not prove Git ancestry")
	}
	report.WindowPolicy = policy
	if recipe != nil {
		if recipe.Author != "" || recipe.EarlyBatchesThrough != "" || len(recipe.Witnesses) > 0 {
			return errors.New("evidence-only recipes cannot select PR author filters, batches, or witnesses; use historical Git evaluation for native attribution")
		}
		report.Recipe = &valueRecipeEvidence{Definition: *recipe, Digest: recipe.Digest}
	}
	evidence := &valueEvidenceReport{Scope: measurement.config.Scope, Trust: "caller_supplied_not_native_adoption",
		Coverage: "unavailable", Observations: []valueEvidenceResult{}, Changes: []valueEvidenceChange{}}
	report.Evidence = evidence
	if filename == "" {
		return nil
	}
	bundle, digest, err := loadValueEvidenceBundle(filename, measurement.info, report.ObservedAt)
	if err != nil {
		return err
	}
	evidence.Digest = digest
	for _, input := range bundle.Observations {
		if input.Workflow != measurement.info.Workflow || input.Metric != measurement.info.ID || input.Scope != measurement.config.Scope ||
			input.At.Before(report.Start) || input.At.After(report.End) {
			continue
		}
		result, err := measurement.evaluate(input)
		if err != nil {
			return fmt.Errorf("evaluate evidence %q: %w", input.ID, err)
		}
		evidence.Observations = append(evidence.Observations, result)
	}
	slices.SortFunc(evidence.Observations, func(a, b valueEvidenceResult) int {
		if order := a.Input.At.Compare(b.Input.At); order != 0 {
			return order
		}
		return strings.Compare(a.Input.ID, b.Input.ID)
	})
	return finishValueEvidenceReport(report, measurement)
}

func finishValueEvidenceReport(report *operationalValueReport, measurement valueEvidenceMeasurement) error {
	evidence := report.Evidence
	available := 0
	for _, result := range evidence.Observations {
		if result.Value != nil {
			available++
		}
	}
	if available > 0 {
		evidence.Coverage, report.Reason = "partial", "evidence_only_no_attribution"
		if available == len(evidence.Observations) {
			evidence.Coverage = "available"
		}
	}
	baselines, outcomes := make(map[string]valueEvidenceResult), make(map[string]struct{})
	for _, result := range evidence.Observations {
		switch result.Input.Phase {
		case "baseline":
			if _, duplicate := baselines[result.Input.Subject]; duplicate {
				return errors.New("evidence contains multiple baselines for a subject; predeclare separate subject/experiment identities")
			}
			baselines[result.Input.Subject] = result
		case "outcome":
			if _, duplicate := outcomes[result.Input.Subject]; duplicate {
				return errors.New("evidence contains multiple outcomes for a subject; do not select a favorable outcome after measurement")
			}
			outcomes[result.Input.Subject] = struct{}{}
		}
	}
	for _, result := range evidence.Observations {
		if result.Input.Phase != "outcome" {
			continue
		}
		if baseline, ok := baselines[result.Input.Subject]; ok {
			change, err := compareValueEvidence(report, measurement, baseline, result)
			if err != nil {
				return err
			}
			evidence.Changes = append(evidence.Changes, change)
		}
	}
	return nil
}

func compareValueEvidence(report *operationalValueReport, measurement valueEvidenceMeasurement, before, after valueEvidenceResult) (valueEvidenceChange, error) {
	if after.Input.At.Before(before.Input.At) {
		return valueEvidenceChange{}, errors.New("paired evidence requires ordered event clocks")
	}
	snapshot := func(result valueEvidenceResult) valueSnapshot {
		population := result.Input.Population
		if population != nil {
			subjects := slices.Clone(*population)
			slices.Sort(subjects)
			population = &subjects
		}
		return valueSnapshot{Value: result.Value, Population: population}
	}
	change := measurement.change(snapshot(before), snapshot(after))
	if before.Input.Context != after.Input.Context {
		change = valueChange{Reason: "measurement_context_changed"}
	}
	anchor := after.Input.At
	for _, record := range report.Lifecycle.Records {
		if record.RunID == after.Input.RunID && record.TargetURL == after.Input.Subject &&
			record.Adoption.State == "accepted" && record.Adoption.Coverage == "available" &&
			record.Adoption.AcceptanceKind == "explicit_human" && record.Adoption.Source == "github-api" &&
			record.Adoption.HumanVerified && !record.Adoption.At.IsZero() {
			anchor = record.Adoption.At
		}
	}
	policy := report.WindowPolicy
	matures := anchor.Add(valueDays(max(policy.OutcomeLagDays+policy.AfterDays, policy.DurabilityDays, policy.IsolationAfterDays)))
	return valueEvidenceChange{Subject: after.Input.Subject, BaselineID: before.Input.ID, OutcomeID: after.Input.ID, Change: change,
		MaturesAt: matures, WindowMature: !matures.After(report.End), SamplingCoverage: "not_evaluated",
		AttributionReason: "evidence_only_native_attribution_not_evaluated"}, nil
}
