package cli

import (
	"fmt"
	"time"
)

type valueMeasurementDefinition struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Unit         string `json:"unit"`
	Host         string `json:"host"`
	Repository   string `json:"repository"`
	Workflow     string `json:"workflow"`
	WorkflowPath string `json:"workflow_path"`
	Direction    string `json:"direction,omitempty"`
	Digest       string `json:"digest,omitempty"`
	Evaluator    string `json:"evaluator,omitempty"`
}

type valueMeasurement interface {
	definition() valueMeasurementDefinition
	measure(valueGit, string) (valueSnapshot, error)
	change(valueSnapshot, valueSnapshot) valueChange
	cohort([]valueSnapshot) *valueCohort
	format(valueSnapshot) string
}

type valueSnapshot struct {
	At         time.Time              `json:"at,omitzero"`
	SHA        string                 `json:"sha"`
	Value      *float64               `json:"value"`
	Coverage   *valueCoverageSnapshot `json:"coverage,omitempty"`
	Population *[]string              `json:"population,omitempty"`
}

type valueChange struct {
	Value    *float64             `json:"value"`
	Reason   string               `json:"reason,omitempty"`
	Coverage *valueCoverageChange `json:"coverage,omitempty"`
}

func formatValueSnapshot(snapshot valueSnapshot, unit string) string {
	if snapshot.Value == nil {
		return "unavailable"
	}
	if snapshot.Coverage != nil {
		return fmt.Sprintf("%d/%d fields", snapshot.Coverage.Covered, len(snapshot.Coverage.Fields))
	}
	return fmt.Sprintf("%g %s", *snapshot.Value, unit)
}
