package cli

import (
	"strings"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/safeoutput"
)

var adoptionEvaluationLog = logger.New("cli:adoption_evaluation")

// AdoptionStatus is the normalized classification for a safe output adoption.
type AdoptionStatus string

const (
	AdoptionStatusAccepted      AdoptionStatus = "accepted"
	AdoptionStatusRejected      AdoptionStatus = "rejected"
	AdoptionStatusPending       AdoptionStatus = "pending"
	AdoptionStatusLifecycle     AdoptionStatus = "lifecycle"
	AdoptionStatusNotApplicable AdoptionStatus = "not_applicable"
	AdoptionStatusUnknown       AdoptionStatus = "unknown"
)

// EvidenceStrength describes how confidently the adoption can be inferred.
type EvidenceStrength string

const (
	EvidenceStrong EvidenceStrength = "strong"
	EvidenceMedium EvidenceStrength = "medium"
	EvidenceWeak   EvidenceStrength = "weak"
	EvidenceNone   EvidenceStrength = "none"
)

// AdoptionEvaluation is the shared normalized adoption model.
type AdoptionEvaluation struct {
	AdoptionStatus      AdoptionStatus         `json:"adoption_status" console:"header:Adoption"`
	EvidenceStrength    EvidenceStrength       `json:"evidence_strength"`
	Signal              string                 `json:"signal,omitempty"`
	Source              string                 `json:"source"`
	RuleVersion         string                 `json:"rule_version"`
	EffectClass         safeoutput.EffectClass `json:"effect_class,omitempty"`
	FallbackToDelivery  bool                   `json:"fallback_to_delivery"`
	AttributionEligible bool                   `json:"attribution_eligible"`
	SurvivalRequired    bool                   `json:"survival_required"`
	AdoptionAt          string                 `json:"adoption_at,omitempty"`
	AcceptanceKind      string                 `json:"acceptance_kind,omitempty"`
}

func normalizeAdoptionEvaluation(report AdoptionReport) AdoptionEvaluation {
	if report.AdoptionStatus != "" && report.EvidenceStrength != "" {
		return report.AdoptionEvaluation
	}

	adoptionEvaluationLog.Printf("Normalizing adoption from heuristics: type=%s, result=%s, detail=%q", report.Type, report.AdoptionStatus, report.Detail)

	if report.EvalError != "" {
		if strings.HasPrefix(report.EvalError, "missing ") || strings.HasPrefix(report.EvalError, "cannot extract comment ID") {
			return adoptionEvidence(AdoptionStatusUnknown, EvidenceNone, "missing_reference")
		}
		return adoptionEvidence(AdoptionStatusUnknown, EvidenceWeak, "evaluation_error")
	}

	detail := strings.ToLower(strings.TrimSpace(report.Detail))

	switch {
	case strings.Contains(detail, "object still exists"):
		return adoptionEvidence(AdoptionStatusUnknown, EvidenceWeak, "target_exists_only")
	case strings.Contains(detail, "closed without merge"):
		return adoptionEvidence(AdoptionStatusRejected, EvidenceStrong, "closed_without_merge")
	case strings.Contains(detail, "closed as not planned"):
		return adoptionEvidence(AdoptionStatusRejected, EvidenceStrong, "closed_not_planned")
	case strings.Contains(detail, "closed by bot") && strings.Contains(detail, "lifecycle_bot_close"):
		return adoptionEvidence(AdoptionStatusLifecycle, EvidenceMedium, "lifecycle_bot_close")
	case strings.Contains(detail, "closed by bot"):
		return adoptionEvidence(AdoptionStatusLifecycle, EvidenceMedium, "lifecycle")
	case strings.Contains(detail, "merged"):
		if report.AdoptionStatus == AdoptionStatusRejected {
			return adoptionEvidence(AdoptionStatusRejected, EvidenceStrong, "closed_by_merge")
		}
		return adoptionEvidence(AdoptionStatusAccepted, EvidenceStrong, "merged")
	case strings.Contains(detail, "reopened"):
		return adoptionEvidence(AdoptionStatusRejected, EvidenceStrong, "reopened")
	case strings.Contains(detail, "deleted"):
		return adoptionEvidence(AdoptionStatusRejected, EvidenceStrong, "deleted")
	case strings.Contains(detail, "completed"):
		return adoptionEvidence(AdoptionStatusAccepted, EvidenceStrong, "completed")
	case strings.Contains(detail, "milestone still assigned"):
		return adoptionEvidence(AdoptionStatusAccepted, EvidenceMedium, "milestone_assigned")
	case strings.Contains(detail, "milestone removed"):
		return adoptionEvidence(AdoptionStatusRejected, EvidenceMedium, "milestone_removed")
	case strings.Contains(detail, "reviews submitted"):
		return adoptionEvidence(AdoptionStatusAccepted, EvidenceMedium, "reviewed")
	case strings.Contains(detail, "awaiting review"), strings.Contains(detail, "no reviews yet"):
		return adoptionEvidence(AdoptionStatusPending, EvidenceMedium, "awaiting_review")
	case strings.Contains(detail, "no engagement"):
		return adoptionEvidence(AdoptionStatusRejected, EvidenceMedium, "no_engagement")
	case strings.Contains(detail, "human comments"), strings.Contains(detail, "with comments"):
		return adoptionEvidence(AdoptionStatusPending, EvidenceMedium, "acted_on")
	case strings.Contains(detail, "open"):
		return adoptionEvidence(AdoptionStatusPending, EvidenceMedium, "open")
	case strings.Contains(detail, "closed"):
		if report.AdoptionStatus == AdoptionStatusRejected {
			return adoptionEvidence(AdoptionStatusRejected, EvidenceStrong, "closed")
		}
		return adoptionEvidence(AdoptionStatusAccepted, EvidenceStrong, "closed")
	}

	return normalizeAdoptionStatus(report.AdoptionStatus)
}

func normalizeAdoptionStatus(status AdoptionStatus) AdoptionEvaluation {
	switch status {
	case AdoptionStatusAccepted:
		return adoptionEvidence(AdoptionStatusAccepted, EvidenceMedium, "accepted")
	case AdoptionStatusRejected:
		return adoptionEvidence(AdoptionStatusRejected, EvidenceMedium, "rejected")
	case AdoptionStatusPending:
		return adoptionEvidence(AdoptionStatusPending, EvidenceMedium, "pending")
	case AdoptionStatusLifecycle:
		return adoptionEvidence(AdoptionStatusLifecycle, EvidenceMedium, "lifecycle")
	case AdoptionStatusNotApplicable:
		return adoptionEvidence(AdoptionStatusNotApplicable, EvidenceNone, "not_applicable")
	default:
		return adoptionEvidence(AdoptionStatusUnknown, EvidenceWeak, "unknown")
	}
}
