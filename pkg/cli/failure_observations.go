package cli

import "fmt"

// FailureObservation is a diagnostic projection, not a replacement for grader status.
// The original diagnostics JSON remains available for unknown fields and versions.
type FailureObservation struct {
	SchemaVersion  int               `json:"schemaVersion"`
	CategoryID     string            `json:"categoryId"`
	Severity       string            `json:"severity"`
	Confidence     string            `json:"confidence"`
	EvidenceState  string            `json:"evidenceState"`
	Recoverability string            `json:"recoverability"`
	Evidence       []FailureEvidence `json:"evidence"`
	Summary        string            `json:"summary"`
}

// FailureEvidence identifies the recorded event supporting an observation.
type FailureEvidence struct {
	EventIndex int               `json:"eventIndex"`
	Type       string            `json:"type"`
	Provenance FailureProvenance `json:"provenance"`
}

// FailureProvenance preserves source-local indexes separately from unified event indexes.
type FailureProvenance struct {
	Component        string `json:"component"`
	Phase            string `json:"phase"`
	Path             string `json:"path,omitempty"`
	Index            *int   `json:"index,omitempty"`
	AttemptID        string `json:"attemptId,omitempty"`
	StartupAttemptID string `json:"startupAttemptId,omitempty"`
}

type failureObservationDisplay struct {
	RunID        int64  `console:"header:Run"`
	GraderStatus string `console:"header:Grader Status"`
	Observation  string `console:"header:Observation"`
}

func failureObservationDisplays(runs []RunData) []failureObservationDisplay {
	var rows []failureObservationDisplay
	for _, run := range runs {
		if run.Graders == nil {
			continue
		}

		func formatFailureObservation(observation FailureObservation) string {
			line := fmt.Sprintf("%s %s evidence=%s confidence=%s recoverability=%s: %s",
				observation.Severity, observation.CategoryID, observation.EvidenceState,
				observation.Confidence, observation.Recoverability, observation.Summary)
			for _, evidence := range observation.Evidence {
				line += fmt.Sprintf(" [event=%d type=%s component=%s phase=%s",
					evidence.EventIndex, evidence.Type, evidence.Provenance.Component, evidence.Provenance.Phase)
				if evidence.Provenance.Path != "" {
					line += " path=" + evidence.Provenance.Path
				}
				if evidence.Provenance.Index != nil {
					line += fmt.Sprintf(" index=%d", *evidence.Provenance.Index)
				}
				if evidence.Provenance.AttemptID != "" {
					line += " attemptId=" + evidence.Provenance.AttemptID
				}
				if evidence.Provenance.StartupAttemptID != "" {
					line += " startupAttemptId=" + evidence.Provenance.StartupAttemptID
				}
				line += "]"
			}
			return line
		}
		for _, result := range run.Graders.Results {
			for _, observation := range result.FailureObservations {
				rows = append(rows, failureObservationDisplay{
					RunID:        run.RunID,
					GraderStatus: result.Status,
					Observation:  formatFailureObservation(observation),
				})
			}
		}
	}
	return rows
}
