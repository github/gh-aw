package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/gitutil"
)

const jsonNumberEvaluator = "json-number-v1"

type jsonNumberConfig struct {
	Path string `json:"path"`
}

type jsonNumberMeasurement struct {
	info   valueMeasurementDefinition
	config jsonNumberConfig
}

type jsonNumberEvidence struct {
	Value      json.RawMessage `json:"value"`
	Population json.RawMessage `json:"population,omitempty"`
}

func (m jsonNumberMeasurement) definition() valueMeasurementDefinition {
	return m.info
}

func (m jsonNumberMeasurement) measure(git valueGit, sha string) (valueSnapshot, error) {
	snapshot := valueSnapshot{SHA: sha}
	if err := gitutil.ValidateGitPath(m.config.Path); err != nil {
		return snapshot, err
	}
	content, err := git.output("show", sha+":"+m.config.Path)
	if err != nil {
		return snapshot, err
	}
	var evidence jsonNumberEvidence
	if err := decodeValueDeclaration(content, &evidence); err != nil {
		return snapshot, fmt.Errorf("numeric value evidence at %s: %w", sha, err)
	}
	if len(evidence.Value) == 0 {
		return snapshot, errors.New("numeric value evidence requires value: a finite number or null")
	}
	if err := json.Unmarshal(evidence.Value, &snapshot.Value); err != nil {
		return snapshot, fmt.Errorf("numeric value evidence must be a finite number or null: %w", err)
	}
	if len(evidence.Population) != 0 {
		var population []string
		if err := json.Unmarshal(evidence.Population, &population); err != nil {
			return snapshot, fmt.Errorf("numeric value population must be an array of subject IDs: %w", err)
		}
		if population == nil {
			return snapshot, errors.New("numeric value population must be an array, not null")
		}
		slices.Sort(population)
		for i, subject := range population {
			if strings.TrimSpace(subject) == "" || i > 0 && population[i-1] == subject { //nolint:uncheckedsliceindex // i > 0 guards the predecessor.
				return snapshot, errors.New("numeric value population requires unique nonempty subject IDs")
			}
		}
		snapshot.Population = &population
	}
	return snapshot, nil
}

func (m jsonNumberMeasurement) change(before, after valueSnapshot) valueChange {
	if before.Value == nil || after.Value == nil {
		return valueChange{Reason: "numeric_evidence_unavailable"}
	}
	if (before.Population == nil) != (after.Population == nil) ||
		before.Population != nil && !slices.Equal(*before.Population, *after.Population) {
		return valueChange{Reason: "numeric_population_changed"}
	}
	delta := *after.Value - *before.Value
	if m.info.Direction == "lower_is_better" {
		delta = -delta
	}
	if math.IsInf(delta, 0) || math.IsNaN(delta) {
		return valueChange{Reason: "numeric_change_out_of_range"}
	}
	return valueChange{Value: &delta}
}

func (jsonNumberMeasurement) cohort([]valueSnapshot) *valueCohort {
	return nil
}

func (m jsonNumberMeasurement) format(snapshot valueSnapshot) string {
	return formatValueSnapshot(snapshot, m.info.Unit)
}
