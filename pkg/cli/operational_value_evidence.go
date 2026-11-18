package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/gitutil"
)

const valueEvidenceEvaluator = "evidence-number-v1"

type valueEvidenceConfig struct {
	Scope             string `json:"scope"`
	Aggregation       string `json:"aggregation"`
	MinimumSamples    int    `json:"minimum_samples"`
	RequirePopulation bool   `json:"require_population"`
	MaximumLines      *int   `json:"maximum_lines,omitempty"`
}

type valueEvidenceMeasurement struct {
	info   valueMeasurementDefinition
	config valueEvidenceConfig
}

type valueEvidenceBundle struct {
	Version      int                        `json:"version"`
	Repository   string                     `json:"repository"`
	Observations []valueEvidenceObservation `json:"observations"`
}

type valueEvidenceObservation struct {
	ID         string               `json:"id"`
	Workflow   string               `json:"workflow"`
	Metric     string               `json:"metric"`
	Scope      string               `json:"scope"`
	Subject    string               `json:"subject"`
	RunID      int64                `json:"run_id"`
	SHA        string               `json:"sha,omitempty"`
	At         time.Time            `json:"at"`
	ObservedAt time.Time            `json:"observed_at"`
	Phase      string               `json:"phase"`
	Coverage   string               `json:"coverage"`
	Reason     string               `json:"reason,omitempty"`
	Population *[]string            `json:"population,omitempty"`
	Context    string               `json:"context,omitempty"`
	References []string             `json:"references"`
	Values     valueEvidenceSamples `json:"values"`
}

type valueEvidenceSamples []float64

func (s *valueEvidenceSamples) UnmarshalJSON(data []byte) error {
	// encoding/json silently turns a null element in []float64 into zero.
	var samples []*float64
	if err := json.Unmarshal(data, &samples); err != nil {
		return fmt.Errorf("decode evidence values: %w", err)
	}
	if samples == nil {
		*s = nil
		return nil
	}
	values := make(valueEvidenceSamples, 0, len(samples))
	for i, sample := range samples {
		if sample == nil {
			return fmt.Errorf("values[%d] is null; supply an observed number (for example 0), or mark coverage unavailable with a reason and no values", i)
		}
		values = append(values, *sample)
	}
	*s = values
	return nil
}

type valueEvidenceResult struct {
	Input  valueEvidenceObservation `json:"input"`
	Value  *float64                 `json:"value"`
	Reason string                   `json:"reason,omitempty"`
}

func validateValueEvidenceConfig(config valueEvidenceConfig) error {
	if !slices.Contains([]string{"git_snapshot", "run", "benchmark", "object_state"}, config.Scope) ||
		!slices.Contains([]string{"scalar", "median", "excess_lines"}, config.Aggregation) {
		return errors.New("evidence-number-v1 requires scope git_snapshot, run, benchmark, or object_state and aggregation scalar, median, or excess_lines")
	}
	if config.MinimumSamples < 1 || config.MinimumSamples > 10000 {
		return errors.New("evidence-number-v1 minimum_samples must be between 1 and 10000")
	}
	if config.Aggregation == "excess_lines" {
		if config.Scope != "git_snapshot" || !config.RequirePopulation || config.MaximumLines == nil || *config.MaximumLines < 0 {
			return errors.New("excess_lines requires git_snapshot, require_population, and nonnegative maximum_lines")
		}
	} else if config.MaximumLines != nil {
		return errors.New("maximum_lines is only valid for excess_lines")
	}
	if config.Aggregation == "scalar" && config.MinimumSamples != 1 {
		return errors.New("scalar evidence requires minimum_samples 1")
	}
	if config.Scope == "benchmark" && config.Aggregation != "median" {
		return errors.New("benchmark evidence requires median aggregation")
	}
	return nil
}

func loadValueEvidenceBundle(filename string, definition valueMeasurementDefinition, now time.Time) (valueEvidenceBundle, string, error) {
	var bundle valueEvidenceBundle
	digest, err := readValueDeclaration(filename, &bundle)
	if err != nil {
		return bundle, "", err
	}
	if bundle.Version != 1 || bundle.Repository != path.Join(definition.Host, definition.Repository) || len(bundle.Observations) > 1024 {
		return bundle, "", errors.New("evidence requires version 1, the selected HOST/OWNER/REPO, and at most 1024 observations")
	}
	seen := make(map[string]struct{})
	for _, observation := range bundle.Observations {
		if _, duplicate := seen[observation.ID]; duplicate {
			return bundle, "", fmt.Errorf("duplicate evidence observation %q", observation.ID)
		}
		seen[observation.ID] = struct{}{}
		if err := validateValueEvidenceObservation(observation, now); err != nil {
			return bundle, "", fmt.Errorf("evidence observation %q: %w", observation.ID, err)
		}
	}
	return bundle, digest, nil
}

func validateValueEvidenceObservation(o valueEvidenceObservation, now time.Time) error {
	if strings.TrimSpace(o.ID) == "" || !valueWorkflowID.MatchString(o.Workflow) ||
		strings.TrimSpace(o.Metric) == "" || strings.TrimSpace(o.Subject) == "" || o.RunID <= 0 {
		return errors.New("identity requires id, workflow, metric, subject, and positive run_id")
	}
	if !slices.Contains([]string{"git_snapshot", "run", "benchmark", "object_state"}, o.Scope) ||
		!slices.Contains([]string{"baseline", "outcome", "sample"}, o.Phase) ||
		!slices.Contains([]string{"available", "partial", "unavailable"}, o.Coverage) {
		return errors.New("invalid evidence scope, phase, or coverage")
	}
	if o.At.IsZero() || o.ObservedAt.IsZero() || o.At.After(o.ObservedAt) || o.ObservedAt.After(now) ||
		len(o.References) == 0 || slices.ContainsFunc(o.References, func(ref string) bool { return strings.TrimSpace(ref) == "" }) {
		return errors.New("evidence requires ordered event/observation clocks and nonempty source references")
	}
	if o.Scope == "git_snapshot" && o.Coverage == "available" && !gitutil.IsValidFullSHA(o.SHA) {
		return errors.New("available git_snapshot evidence requires a full commit SHA")
	}
	if o.Coverage != "available" && (o.Reason == "" || len(o.Values) != 0) {
		return errors.New("partial or unavailable evidence requires a reason and no numeric values")
	}
	if o.Coverage == "available" && len(o.Values) == 0 {
		return errors.New("available evidence requires numeric observations")
	}
	if o.Population != nil {
		seen := make(map[string]struct{})
		for _, subject := range *o.Population {
			_, duplicate := seen[subject]
			if strings.TrimSpace(subject) == "" || duplicate {
				return errors.New("population requires unique nonempty subject IDs")
			}
			seen[subject] = struct{}{}
		}
	}
	for _, value := range o.Values {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return errors.New("evidence observations must be finite")
		}
	}
	return nil
}

func (m valueEvidenceMeasurement) evaluate(input valueEvidenceObservation) (valueEvidenceResult, error) {
	result := valueEvidenceResult{Input: input}
	switch {
	case input.Coverage != "available":
		result.Reason = input.Reason
	case m.config.RequirePopulation && input.Population == nil:
		result.Reason = "eligible_population_missing"
	case len(input.Values) < m.config.MinimumSamples:
		result.Reason = "insufficient_measurement_samples"
	case input.Scope == "benchmark" && input.Context == "":
		result.Reason = "benchmark_comparability_context_missing"
	default:
		value, err := m.aggregate(input)
		if err != nil {
			return result, err
		}
		result.Value = &value
	}
	return result, nil
}

func (m valueEvidenceMeasurement) aggregate(input valueEvidenceObservation) (float64, error) {
	values := input.Values
	var value float64
	switch m.config.Aggregation {
	case "scalar":
		if len(values) != 1 {
			return 0, errors.New("scalar evidence requires exactly one numeric observation")
		}
		value = values[0] //nolint:uncheckedsliceindex // Exactly one value is checked above.
	case "median":
		values = slices.Clone(values)
		slices.Sort(values)
		middle := len(values) / 2
		value = values[middle] //nolint:uncheckedsliceindex // evaluate requires a positive minimum sample count.
		if len(values)%2 == 0 {
			value = value/2 + values[middle-1]/2 //nolint:uncheckedsliceindex // A nonempty even sample has a predecessor.
		}
	case "excess_lines":
		if input.Population == nil || len(*input.Population) != len(values) {
			return 0, errors.New("excess_lines requires one observed line count per eligible file, including verified absences")
		}
		for _, count := range values {
			if count < 0 || math.Trunc(count) != count {
				return 0, errors.New("excess_lines requires nonnegative integer newline counts")
			}
			value += max(0, count-float64(*m.config.MaximumLines))
		}
	}
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, errors.New("evidence aggregation exceeds finite numeric range")
	}
	return value, nil
}

func (m valueEvidenceMeasurement) definition() valueMeasurementDefinition { return m.info }

func (valueEvidenceMeasurement) measure(valueGit, string) (valueSnapshot, error) {
	return valueSnapshot{}, errors.New("evidence-number-v1 requires --evidence rather than implicit Git or session inputs")
}

func (m valueEvidenceMeasurement) change(before, after valueSnapshot) valueChange {
	return (jsonNumberMeasurement{info: m.info}).change(before, after)
}

func (valueEvidenceMeasurement) cohort([]valueSnapshot) *valueCohort { return nil }

func (m valueEvidenceMeasurement) format(snapshot valueSnapshot) string {
	return formatValueSnapshot(snapshot, m.info.Unit)
}
