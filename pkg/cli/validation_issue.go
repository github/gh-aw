package cli

import "github.com/github/gh-aw/pkg/scanfindings"

// ValidationIssue represents a single validation, warning, or audit issue entry.
type ValidationIssue struct {
	Type     string                     `json:"type"`
	Message  string                     `json:"message"`
	RuleID   string                     `json:"rule_id,omitempty"`
	Line     int                        `json:"line,omitempty"`
	Column   int                        `json:"column,omitempty"`
	File     string                     `json:"file,omitempty"`
	Severity scanfindings.SeverityLevel `json:"severity,omitempty"`
	Script   *ValidationScriptLocation  `json:"script,omitempty"`
}

// ValidationScriptLocation identifies snippet-relative coordinates, not YAML
// file coordinates. StepIndex is one-based; Sanitized marks replaced expressions.
type ValidationScriptLocation struct {
	JobID     string `json:"job_id,omitempty"`
	StepID    string `json:"step_id,omitempty"`
	StepName  string `json:"step_name,omitempty"`
	StepIndex int    `json:"step_index,omitempty"`
	Source    string `json:"source,omitempty"`
	Resource  string `json:"resource,omitempty"`
	Line      int    `json:"line"`
	Column    int    `json:"column"`
	Sanitized bool   `json:"sanitized,omitempty"`
}

// ToFinding converts the validation issue to the shared finding representation.
// The caller supplies severity because Type is a diagnostic category, not a
// severity level.
func (v ValidationIssue) ToFinding(severity scanfindings.SeverityLevel) scanfindings.Finding {
	ruleID := v.RuleID
	if ruleID == "" {
		ruleID = v.Type
	}
	return scanfindings.Finding{
		RuleID:   ruleID,
		Severity: severity,
		Message:  v.Message,
		File:     v.File,
		Line:     v.Line,
		Column:   v.Column,
	}
}
