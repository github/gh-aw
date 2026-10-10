package cli

import (
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var outcomeEvaluationLog = logger.New("cli:outcome_evaluation")

// OutcomeStatus is the normalized classification for a safe output outcome.
type OutcomeStatus string

const (
	OutcomeStatusAccepted       OutcomeStatus = "accepted"
	OutcomeStatusRejected       OutcomeStatus = "rejected"
	OutcomeStatusPending        OutcomeStatus = "pending"
	OutcomeStatusIgnored        OutcomeStatus = "ignored"
	OutcomeStatusSkipped        OutcomeStatus = "skipped"
	OutcomeStatusUnknown        OutcomeStatus = "unknown"
	OutcomeStatusLifecycle      OutcomeStatus = "lifecycle"
	OutcomeStatusLifecycleClose OutcomeStatus = "lifecycle_close"
	OutcomeStatusError          OutcomeStatus = "error"
)

// EvidenceStrength describes how confidently the outcome can be inferred.
type EvidenceStrength string

const (
	EvidenceStrong EvidenceStrength = "strong"
	EvidenceMedium EvidenceStrength = "medium"
	EvidenceWeak   EvidenceStrength = "weak"
	EvidenceNone   EvidenceStrength = "none"
)

// OutcomeEvaluation is the shared normalized outcome model.
type OutcomeEvaluation struct {
	OutcomeStatus    OutcomeStatus    `json:"outcome_status" console:"header:Outcome"`
	EvidenceStrength EvidenceStrength `json:"evidence_strength"`
	Signal           string           `json:"signal,omitempty"`
}

func normalizeOutcomeEvaluation(report OutcomeReport) OutcomeEvaluation {
	if report.OutcomeStatus != "" && report.EvidenceStrength != "" {
		return report.OutcomeEvaluation
	}

	outcomeEvaluationLog.Printf("Normalizing outcome from heuristics: type=%s, result=%s, detail=%q", report.Type, report.OutcomeStatus, report.Detail)

	if report.EvalError != "" || report.OutcomeStatus == OutcomeStatusError {
		if strings.HasPrefix(report.EvalError, "missing ") || strings.HasPrefix(report.EvalError, "cannot extract comment ID") {
			return outcomeEvidence(OutcomeStatusUnknown, EvidenceNone, "missing_reference")
		}
		return outcomeEvidence(OutcomeStatusError, EvidenceWeak, "evaluation_error")
	}

	detail := strings.ToLower(strings.TrimSpace(report.Detail))

	switch {
	case strings.Contains(detail, "object still exists"):
		return outcomeEvidence(OutcomeStatusUnknown, EvidenceWeak, "target_exists_only")
	case strings.Contains(detail, "closed without merge"):
		return outcomeEvidence(OutcomeStatusRejected, EvidenceStrong, "closed_without_merge")
	case strings.Contains(detail, "closed as not planned"):
		return outcomeEvidence(OutcomeStatusRejected, EvidenceStrong, "closed_not_planned")
	case strings.Contains(detail, "closed by bot") && strings.Contains(detail, "lifecycle_close"):
		return outcomeEvidence(OutcomeStatusLifecycleClose, EvidenceMedium, "lifecycle_close")
	case strings.Contains(detail, "closed by bot"):
		return outcomeEvidence(OutcomeStatusLifecycle, EvidenceMedium, "lifecycle")
	case strings.Contains(detail, "merged"):
		if report.OutcomeStatus == OutcomeStatusRejected {
			return outcomeEvidence(OutcomeStatusRejected, EvidenceStrong, "closed_by_merge")
		}
		return outcomeEvidence(OutcomeStatusAccepted, EvidenceStrong, "merged")
	case strings.Contains(detail, "reopened"):
		return outcomeEvidence(OutcomeStatusRejected, EvidenceStrong, "reopened")
	case strings.Contains(detail, "deleted"):
		return outcomeEvidence(OutcomeStatusRejected, EvidenceStrong, "deleted")
	case strings.Contains(detail, "completed"):
		return outcomeEvidence(OutcomeStatusAccepted, EvidenceStrong, "completed")
	case strings.Contains(detail, "milestone still assigned"):
		return outcomeEvidence(OutcomeStatusAccepted, EvidenceMedium, "milestone_assigned")
	case strings.Contains(detail, "milestone removed"):
		return outcomeEvidence(OutcomeStatusRejected, EvidenceMedium, "milestone_removed")
	case strings.Contains(detail, "reviews submitted"):
		return outcomeEvidence(OutcomeStatusAccepted, EvidenceMedium, "reviewed")
	case strings.Contains(detail, "awaiting review"), strings.Contains(detail, "no reviews yet"):
		return outcomeEvidence(OutcomeStatusPending, EvidenceMedium, "awaiting_review")
	case strings.Contains(detail, "no engagement"):
		return outcomeEvidence(OutcomeStatusIgnored, EvidenceMedium, "no_engagement")
	case strings.Contains(detail, "human comments"), strings.Contains(detail, "with comments"):
		return outcomeEvidence(OutcomeStatusPending, EvidenceMedium, "acted_on")
	case strings.Contains(detail, "open"):
		return outcomeEvidence(OutcomeStatusPending, EvidenceMedium, "open")
	case strings.Contains(detail, "closed"):
		if report.OutcomeStatus == OutcomeStatusRejected {
			return outcomeEvidence(OutcomeStatusRejected, EvidenceStrong, "closed")
		}
		return outcomeEvidence(OutcomeStatusAccepted, EvidenceStrong, "closed")
	}

	return normalizeOutcomeStatus(report.OutcomeStatus)

}

func normalizeOutcomeStatus(status OutcomeStatus) OutcomeEvaluation {
	switch status {
	case OutcomeStatusAccepted:
		return outcomeEvidence(OutcomeStatusAccepted, EvidenceMedium, "acted_on")
	case OutcomeStatusRejected:
		return outcomeEvidence(OutcomeStatusRejected, EvidenceMedium, "rejected")
	case OutcomeStatusPending:
		return outcomeEvidence(OutcomeStatusPending, EvidenceMedium, "pending")
	case OutcomeStatusIgnored:
		return outcomeEvidence(OutcomeStatusIgnored, EvidenceMedium, "ignored")
	case OutcomeStatusLifecycle:
		return outcomeEvidence(OutcomeStatusLifecycle, EvidenceMedium, "lifecycle")
	case OutcomeStatusLifecycleClose:
		return outcomeEvidence(OutcomeStatusLifecycleClose, EvidenceMedium, "lifecycle_close")
	case OutcomeStatusUnknown:
		return outcomeEvidence(OutcomeStatusUnknown, EvidenceWeak, "unknown")
	case OutcomeStatusSkipped:
		return outcomeEvidence(OutcomeStatusSkipped, EvidenceNone, "skipped")
	case OutcomeStatusError:
		return outcomeEvidence(OutcomeStatusError, EvidenceWeak, "evaluation_error")
	default:
		return outcomeEvidence(OutcomeStatusUnknown, EvidenceWeak, "unknown")
	}
}
