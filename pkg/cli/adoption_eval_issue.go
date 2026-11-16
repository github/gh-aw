package cli

import (
	"context"
	"fmt"

	"github.com/github/gh-aw/pkg/logger"
)

var adoptionEvalIssueLog = logger.New("cli:adoption_eval_issue")

// evalCreateIssue checks whether an issue was resolved, dismissed, or is still open.
// Bot-initiated closes (e.g. close-older-issues) are classified as lifecycle, not rejection.
func evalCreateIssue(ctx context.Context, item CreatedItemReport, repoOverride string) AdoptionReport {
	repo := resolveItemRepo(item, repoOverride)
	num := resolveItemNumber(item)
	adoptionEvalIssueLog.Printf("Evaluating create_issue: repo=%s, num=%d, url=%s", repo, num, item.URL)
	report := AdoptionReport{
		Type:         item.Type,
		ObjectURL:    item.URL,
		ObjectNumber: num,
		Repo:         repo,
	}
	if num == 0 || repo == "" {
		adoptionEvalIssueLog.Printf("Missing issue number or repo: num=%d, repo=%s", num, repo)
		report.AdoptionStatus = AdoptionStatusUnknown
		report.EvalError = "missing issue number or repo"
		return report
	}

	data, err := adoptionEvidenceGHAPIGet(ctx, fmt.Sprintf("issues/%d", num), repo)
	if err != nil {
		adoptionAPIError(&report, err, true)
		return report
	}

	return classifyCreatedIssue(ctx, item, data, report)
}

func classifyCreatedIssue(ctx context.Context, item CreatedItemReport, data map[string]any, report AdoptionReport) AdoptionReport {
	num, repo := report.ObjectNumber, report.Repo
	state := adoptionValue[string](data["state"])
	stateReason := adoptionValue[string](data["state_reason"])
	closedAt := adoptionValue[string](data["closed_at"])

	switch {
	case state == "closed" && stateReason == "completed":
		report.Detail = "completed"
		report.AdoptionEvaluation = adoptionEvidence(AdoptionStatusAccepted, EvidenceStrong, "completed")
		setAdoptionDecisionTime(&report, item.Timestamp, closedAt)

	case state == "closed" && stateReason == "not_planned":
		// Check if closed by a bot (lifecycle) or human (rejection)
		closedByBot, err := adoptionCloseActor(ctx, num, repo)
		if err != nil {
			adoptionAPIError(&report, err, false)
			return report
		}
		adoptionEvalIssueLog.Printf("Issue #%d closed as not_planned, closed_by_bot=%v", num, closedByBot)
		if closedByBot {
			report.Detail = "closed by bot (lifecycle)"
			report.AdoptionEvaluation = adoptionEvidence(AdoptionStatusLifecycle, EvidenceMedium, "lifecycle")
		} else {
			report.Detail = "closed as not planned"
			report.AdoptionEvaluation = adoptionEvidence(AdoptionStatusRejected, EvidenceStrong, "closed_not_planned")
		}
		setAdoptionDecisionTime(&report, item.Timestamp, closedAt)

	case state == "closed":
		report.AdoptionEvaluation = adoptionEvidence(AdoptionStatusUnknown, EvidenceWeak, "unknown")
		report.Detail = "closed without a resolution reason"
		setAdoptionDecisionTime(&report, item.Timestamp, closedAt)

	case state == "open":
		commentList, err := adoptionEvidenceGHAPIGetArray(ctx, fmt.Sprintf("issues/%d/comments", num), repo)
		if err != nil {
			adoptionAPIError(&report, err, false)
			return report
		}
		report.HumanComments = nonBotCommentsAfter(commentList, item.Timestamp)
		if report.HumanComments > 0 {
			report.AdoptionEvaluation = adoptionEvidence(AdoptionStatusPending, EvidenceMedium, "acted_on")
			report.Detail = "open with non-bot engagement"
		} else {
			report.AdoptionEvaluation = adoptionEvidence(AdoptionStatusPending, EvidenceMedium, "open")
			report.Detail = "open"
		}
	default:
		report.AdoptionEvaluation = adoptionEvidence(AdoptionStatusUnknown, EvidenceWeak, "unknown")
		report.Detail = "unsupported issue state"
	}

	return report
}
