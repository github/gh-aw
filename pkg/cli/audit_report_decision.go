package cli

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/github/gh-aw/pkg/constants"
)

const maxDecisionGateArtifactBytes = 64 << 10

// DecisionGateData is the bounded receipt produced before agent activation.
// Question text and inference inputs are deliberately not included.
type DecisionGateData struct {
	Version         int    `json:"version"`
	Decision        string `json:"decision"`
	Status          string `json:"status"`
	Cause           string `json:"cause,omitempty"`
	ConfiguredAlias string `json:"configured_alias,omitempty"`
	ResolvedModel   string `json:"resolved_model,omitempty"`
	Provider        string `json:"provider,omitempty"`
	DurationMS      int64  `json:"duration_ms"`
	// ActivationAllowed describes admission by this gate, not downstream job execution.
	ActivationAllowed     bool                              `json:"activation_allowed"`
	DeterministicEligible bool                              `json:"deterministic_eligible"`
	FailOpen              bool                              `json:"fail_open"`
	QuestionCount         int                               `json:"question_count"`
	EvaluatedCount        int                               `json:"evaluated_count"`
	UnevaluatedCount      int                               `json:"unevaluated_count"`
	UnevaluatedQuestions  []DecisionGateUnevaluatedQuestion `json:"unevaluated_questions"`
	Usage                 *DecisionGateUsage                `json:"usage,omitempty"`
	UsageComplete         bool                              `json:"usage_complete"`
	Questions             []DecisionGateQuestion            `json:"questions"`
}

type DecisionGateUnevaluatedQuestion struct {
	Index  int    `json:"index"`
	Reason string `json:"reason"`
}

type DecisionGateQuestion struct {
	Index           int                `json:"index"`
	Decision        string             `json:"decision"`
	Status          string             `json:"status"`
	Cause           string             `json:"cause,omitempty"`
	Reason          string             `json:"reason,omitempty"`
	ConfiguredAlias string             `json:"configured_alias,omitempty"`
	ResolvedModel   string             `json:"resolved_model,omitempty"`
	Provider        string             `json:"provider,omitempty"`
	DurationMS      int64              `json:"duration_ms"`
	Evaluated       bool               `json:"evaluated"`
	FailOpen        bool               `json:"fail_open"`
	Usage           *DecisionGateUsage `json:"usage,omitempty"`
}

type DecisionGateUsage struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
	TotalTokens  int64 `json:"total_tokens"`
}

func extractDecisionGateData(logsPath string) *DecisionGateData {
	if logsPath == "" {
		return nil
	}
	artifact := constants.DecisionGateArtifactName.String()
	candidates := []string{"decision-gate.json", filepath.Join(artifact, "decision-gate.json")}
	if entries, err := os.ReadDir(logsPath); err == nil {
		for _, entry := range entries {
			if entry.IsDir() && entry.Name() != artifact && artifactNameMatchesBase(entry.Name(), artifact) {
				candidates = append(candidates, filepath.Join(entry.Name(), "decision-gate.json"))
			}
		}
	}
	for _, candidate := range candidates {
		// Reuse the bounded reader that rejects symlinks in every artifact path component.
		raw, err := readModelAvailabilityArtifact(logsPath, filepath.ToSlash(candidate), maxDecisionGateArtifactBytes)
		if err != nil {
			continue
		}
		var data DecisionGateData
		if json.Unmarshal(raw, &data) != nil || data.Version != 1 || data.Status == "" ||
			(data.Decision != "run" && data.Decision != "skip") ||
			len(data.Questions) > 8 || len(data.UnevaluatedQuestions) > 8 {
			auditReportLog.Print("Ignoring invalid decision gate receipt")
			continue
		}
		return &data
	}
	return nil
}
