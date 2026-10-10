package cli

import (
	"context"
	"fmt"

	"github.com/github/gh-aw/pkg/logger"
)

var outcomeEvalIssueLog = logger.New("cli:outcome_eval_issue")

// evalCreateIssue checks whether an issue was resolved, dismissed, or is still open.
// Bot-initiated closes (e.g. close-older-issues) are classified as lifecycle, not rejection.
func evalCreateIssue(ctx context.Context, item CreatedItemReport, repoOverride string) OutcomeReport {
	repo := resolveItemRepo(item, repoOverride)
	num := resolveItemNumber(item)
	outcomeEvalIssueLog.Printf("Evaluating create_issue: repo=%s, num=%d, url=%s", repo, num, item.URL)
	report := OutcomeReport{
		Type:         item.Type,
		ObjectURL:    item.URL,
		ObjectNumber: num,
		Repo:         repo,
	}
	if num == 0 || repo == "" {
		outcomeEvalIssueLog.Printf("Missing issue number or repo: num=%d, repo=%s", num, repo)
		report.OutcomeStatus = OutcomeStatusError
		report.EvalError = "missing issue number or repo"
		return report
	}

	data, err := outcomeEvidenceGHAPIGet(ctx, fmt.Sprintf("issues/%d", num), repo)
	if err != nil {
		outcomeAPIError(&report, err, true)
		return report
	}

	return classifyCreatedIssue(ctx, item, data, report)
}

func classifyCreatedIssue(ctx context.Context, item CreatedItemReport, data map[string]any, report OutcomeReport) OutcomeReport {
	num, repo := report.ObjectNumber, report.Repo
	state := outcomeValue[string](data["state"])
	stateReason := outcomeValue[string](data["state_reason"])
	closedAt := outcomeValue[string](data["closed_at"])

	switch {
	case state == "closed" && stateReason == "completed":
		report.Detail = "completed"
		report.OutcomeEvaluation = outcomeEvidence(OutcomeStatusAccepted, EvidenceStrong, "completed")
		if closedAt != "" && item.Timestamp != "" {
			report.TimeToOutcomeHours = timeBetween(item.Timestamp, closedAt)
		}

	case state == "closed" && stateReason == "not_planned":
		// Check if closed by a bot (lifecycle) or human (rejection)
		closedByBot, err := outcomeCloseActor(ctx, num, repo)
		if err != nil {
			outcomeAPIError(&report, err, false)
			return report
		}
		outcomeEvalIssueLog.Printf("Issue #%d closed as not_planned, closed_by_bot=%v", num, closedByBot)
		if closedByBot {
			report.Detail = "closed by bot (lifecycle)"
			report.OutcomeEvaluation = outcomeEvidence(OutcomeStatusLifecycle, EvidenceMedium, "lifecycle")
		} else {
			report.Detail = "closed as not planned"
			report.OutcomeEvaluation = outcomeEvidence(OutcomeStatusRejected, EvidenceStrong, "closed_not_planned")
		}
		if closedAt != "" && item.Timestamp != "" {
			report.TimeToOutcomeHours = timeBetween(item.Timestamp, closedAt)
		}

	case state == "closed":
		report.OutcomeEvaluation = outcomeEvidence(OutcomeStatusUnknown, EvidenceWeak, "unknown")
		report.Detail = "closed without a resolution reason"
		if closedAt != "" && item.Timestamp != "" {
			report.TimeToOutcomeHours = timeBetween(item.Timestamp, closedAt)
		}

	case state == "open":
		commentList, err := outcomeEvidenceGHAPIGetArray(ctx, fmt.Sprintf("issues/%d/comments", num), repo)
		if err != nil {
			outcomeAPIError(&report, err, false)
			return report
		}
		report.HumanComments = nonBotCommentsAfter(commentList, item.Timestamp)
		if report.HumanComments > 0 {
			report.OutcomeEvaluation = outcomeEvidence(OutcomeStatusPending, EvidenceMedium, "acted_on")
			report.Detail = "open with non-bot engagement"
		} else {
			report.OutcomeEvaluation = outcomeEvidence(OutcomeStatusPending, EvidenceMedium, "open")
			report.Detail = "open"
		}
	default:
		report.OutcomeEvaluation = outcomeEvidence(OutcomeStatusUnknown, EvidenceWeak, "unknown")
		report.Detail = "unsupported issue state"
	}

	return report
}
