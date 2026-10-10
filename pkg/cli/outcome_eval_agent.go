package cli

import (
	"context"
	"fmt"
	"strings"
)

// evalAssignToAgent requires a post-assignment link to a visible agent-authored PR.
func evalAssignToAgent(ctx context.Context, item CreatedItemReport, repoOverride string) OutcomeReport {
	repo := resolveItemRepo(item, repoOverride)
	num := resolveItemNumber(item)
	report := OutcomeReport{Type: item.Type, ObjectURL: item.URL, ObjectNumber: num, Repo: repo}
	report.OutcomeEvaluation = outcomeEvidence(OutcomeStatusUnknown, EvidenceNone, "missing_reference")
	if num == 0 || repo == "" {
		return report
	}
	_, err := outcomeEvidenceGHAPIGet(ctx, fmt.Sprintf("issues/%d", num), repo)
	if err != nil {
		outcomeAPIError(&report, err, false)
		return report
	}
	events, err := outcomeEvidenceGHAPIGetArray(ctx, fmt.Sprintf("issues/%d/timeline", num), repo)
	if err != nil {
		outcomeAPIError(&report, err, false)
		return report
	}
	for _, event := range events {
		if outcomeString(event["event"]) != "cross-referenced" {
			continue
		}
		at := outcomeString(event["created_at"])
		if !outcomeAfter(at, item.Timestamp) {
			continue
		}
		source, _ := event["source"].(map[string]any)
		issue, _ := source["issue"].(map[string]any)
		if issue["pull_request"] == nil {
			continue
		}
		login := outcomeNestedString(issue["user"], "login")
		if !strings.EqualFold(login, "copilot-swe-agent") && !strings.EqualFold(login, "github-actions[bot]") {
			continue
		}
		prNumber := metadataInt(issue, "number")
		if prNumber <= 0 {
			continue
		}
		pr, err := outcomeEvidenceGHAPIGet(ctx, fmt.Sprintf("pulls/%d", prNumber), repo)
		if err != nil {
			outcomeAPIError(&report, err, false)
			return report
		}
		if merged, _ := pr["merged"].(bool); merged {
			report.OutcomeEvaluation = outcomeEvidence(OutcomeStatusAccepted, EvidenceStrong, "merged")
		} else if outcomeString(pr["state"]) == "closed" {
			report.OutcomeEvaluation = outcomeEvidence(OutcomeStatusRejected, EvidenceStrong, "closed_without_merge")
		} else {
			report.OutcomeEvaluation = outcomeEvidence(OutcomeStatusPending, EvidenceMedium, "open")
		}
		report.Detail = report.Signal
		return report
	}
	report.OutcomeEvaluation = outcomeEvidence(OutcomeStatusUnknown, EvidenceNone, "missing_execution_state")
	report.Detail = "no attributable agent PR"
	return report
}
