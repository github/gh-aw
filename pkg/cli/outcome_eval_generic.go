package cli

import (
	"context"
	"fmt"

	"github.com/github/gh-aw/pkg/logger"
)

var outcomeEvalGenericLog = logger.New("cli:outcome_eval_generic")
var genericOutcomeGHAPIGet = ghAPIGet
var closeStickyGHAPIGet = ghAPIGet
var closeStickyGHAPIGetArray = ghAPIGetArray

// evalCloseSticky checks whether a closed issue or PR stayed closed.
func evalCloseSticky(ctx context.Context, item CreatedItemReport, repoOverride string) OutcomeReport {
	repo := resolveItemRepo(item, repoOverride)
	num := resolveItemNumber(item)
	outcomeEvalGenericLog.Printf("Evaluating close_sticky: type=%s, repo=%s, num=%d", item.Type, repo, num)
	report := OutcomeReport{
		Type:         item.Type,
		ObjectURL:    item.URL,
		ObjectNumber: num,
		Repo:         repo,
	}
	if num == 0 || repo == "" {
		report.OutcomeStatus = OutcomeStatusError
		report.EvalError = "missing number or repo"
		return report
	}

	endpoint := fmt.Sprintf("issues/%d", num)
	if item.Type == "close_pull_request" {
		endpoint = fmt.Sprintf("pulls/%d", num)
	}

	data, err := closeStickyGHAPIGet(ctx, endpoint, repo)
	if err != nil {
		outcomeAPIError(&report, err, false)
		return report
	}

	state := outcomeValue[string](data["state"])
	merged := outcomeValue[bool](data["merged"])
	if state != "closed" {
		report.OutcomeStatus = OutcomeStatusRejected
		report.Detail = "reopened"
		return report
	}

	if merged {
		report.OutcomeEvaluation = outcomeEvidence(OutcomeStatusRejected, EvidenceStrong, "closed_by_merge")
		report.Detail = "merged"
		return report
	}

	closedByBot, err := isClosedByLifecycleBot(ctx, num, repo)
	if err != nil {
		report.OutcomeStatus = OutcomeStatusError
		report.EvalError = err.Error()
		report.Detail = "close provenance unavailable"
		return report
	}

	if closedByBot {
		report.OutcomeStatus = OutcomeStatusLifecycleClose
		report.Detail = "closed by bot (lifecycle_close)"
	} else {
		report.OutcomeEvaluation = outcomeEvidence(OutcomeStatusAccepted, EvidenceStrong, "closed")
		report.Detail = "closed"
	}
	return report
}

func isClosedByLifecycleBot(ctx context.Context, number int, repo string) (bool, error) {
	return isLatestCloseByBot(ctx, number, repo, closeStickyGHAPIGetArray)
}

// evalCloseDiscussion has no GraphQL evaluator yet.
func evalCloseDiscussion(ctx context.Context, item CreatedItemReport, repoOverride string) OutcomeReport {
	return unsupportedOutcome(item, repoOverride)
}

// evalCreateDiscussion checks whether a discussion received replies.
func evalCreateDiscussion(ctx context.Context, item CreatedItemReport, repoOverride string) OutcomeReport {
	return unsupportedOutcome(item, repoOverride)
}

// evalHideComment checks whether a hidden comment is still hidden.
func evalHideComment(ctx context.Context, item CreatedItemReport, repoOverride string) OutcomeReport {
	return unsupportedOutcome(item, repoOverride)
}

// evalAssignMilestone checks whether a milestone assignment stuck.
func evalAssignMilestone(ctx context.Context, item CreatedItemReport, repoOverride string) OutcomeReport {
	repo := resolveItemRepo(item, repoOverride)
	num := resolveItemNumber(item)
	outcomeEvalGenericLog.Printf("Evaluating assign_milestone: repo=%s, num=%d", repo, num)
	report := OutcomeReport{
		Type:         item.Type,
		ObjectURL:    item.URL,
		ObjectNumber: num,
		Repo:         repo,
	}
	if num == 0 || repo == "" {
		report.OutcomeStatus = OutcomeStatusError
		report.EvalError = "missing number or repo"
		return report
	}

	expected := metadataInt(item.Metadata, "milestone_number")
	if expected <= 0 {
		report.OutcomeEvaluation = outcomeEvidence(OutcomeStatusUnknown, EvidenceNone, "missing_execution_state")
		report.Detail = "missing execution state"
		return report
	}
	data, err := outcomeEvidenceGHAPIGet(ctx, fmt.Sprintf("issues/%d", num), repo)
	if err != nil {
		report.OutcomeStatus = OutcomeStatusError
		report.EvalError = err.Error()
		return report
	}

	milestone := outcomeValue[map[string]any](data["milestone"])
	if metadataInt(milestone, "number") == expected {
		report.Detail = "milestone still assigned"
		report.OutcomeEvaluation = outcomeEvidence(OutcomeStatusAccepted, EvidenceMedium, "milestone_assigned")
	} else {
		report.Detail = "milestone removed"
		report.OutcomeEvaluation = outcomeEvidence(OutcomeStatusRejected, EvidenceMedium, "milestone_removed")
	}
	return report
}

// evalReviewComment checks whether a PR review comment thread was resolved or engaged.
func evalReviewComment(ctx context.Context, item CreatedItemReport, repoOverride string) OutcomeReport {
	return unsupportedOutcome(item, repoOverride)
}

// evalResolveThread checks whether a resolved review thread stayed resolved.
func evalResolveThread(ctx context.Context, item CreatedItemReport, repoOverride string) OutcomeReport {
	return unsupportedOutcome(item, repoOverride)
}

// evalMarkReady checks whether a PR marked as ready received reviews.
func evalMarkReady(ctx context.Context, item CreatedItemReport, repoOverride string) OutcomeReport {
	repo := resolveItemRepo(item, repoOverride)
	num := resolveItemNumber(item)
	outcomeEvalGenericLog.Printf("Evaluating mark_ready: repo=%s, num=%d", repo, num)
	report := OutcomeReport{
		Type:         item.Type,
		ObjectURL:    item.URL,
		ObjectNumber: num,
		Repo:         repo,
	}
	if num == 0 || repo == "" {
		report.OutcomeStatus = OutcomeStatusError
		report.EvalError = "missing number or repo"
		return report
	}

	reviews, err := outcomeEvidenceGHAPIGetArray(ctx, fmt.Sprintf("pulls/%d/reviews", num), repo)
	if err != nil {
		report.OutcomeStatus = OutcomeStatusError
		report.EvalError = err.Error()
		return report
	}

	reviewed := false
	for _, review := range reviews {
		if isNonBotActor(review["user"]) && outcomeString(review["state"]) != "PENDING" && outcomeAfter(outcomeString(review["submitted_at"]), item.Timestamp) {
			reviewed = true
		}
	}
	if reviewed {
		report.Detail = fmt.Sprintf("%d reviews submitted", len(reviews))
		report.OutcomeEvaluation = outcomeEvidence(OutcomeStatusAccepted, EvidenceMedium, "reviewed")
	} else {
		data, derr := outcomeEvidenceGHAPIGet(ctx, fmt.Sprintf("pulls/%d", num), repo)
		if derr == nil {
			state := outcomeValue[string](data["state"])
			if state == "open" {
				report.OutcomeEvaluation = outcomeEvidence(OutcomeStatusPending, EvidenceMedium, "awaiting_review")
				report.Detail = "awaiting review"
			} else {
				report.OutcomeEvaluation = outcomeEvidence(OutcomeStatusIgnored, EvidenceMedium, "ignored")
				report.Detail = "closed/merged without review"
			}
		} else {
			outcomeAPIError(&report, derr, false)
		}
	}
	return report
}

// evalPushToPRBranch checks whether the PR the code was pushed to got merged.
func evalPushToPRBranch(ctx context.Context, item CreatedItemReport, repoOverride string) OutcomeReport {
	repo := resolveItemRepo(item, repoOverride)
	num := resolveItemNumber(item)
	outcomeEvalGenericLog.Printf("Evaluating push_to_pr_branch: repo=%s, num=%d", repo, num)
	report := OutcomeReport{
		Type:         item.Type,
		ObjectURL:    item.URL,
		ObjectNumber: num,
		Repo:         repo,
	}
	if num == 0 || repo == "" {
		report.OutcomeStatus = OutcomeStatusError
		report.EvalError = "missing PR number or repo"
		return report
	}

	data, err := outcomeEvidenceGHAPIGet(ctx, fmt.Sprintf("pulls/%d", num), repo)
	if err != nil {
		report.OutcomeStatus = OutcomeStatusError
		report.EvalError = err.Error()
		return report
	}

	merged := outcomeValue[bool](data["merged"])
	state := outcomeValue[string](data["state"])
	outcomeEvalGenericLog.Printf("push_to_pr_branch PR #%d state: merged=%t, state=%s", num, merged, state)

	switch {
	case merged:
		return verifyPushedCommitRetention(ctx, item, data, report)
	case state == "closed":
		report.OutcomeStatus = OutcomeStatusRejected
		report.Detail = "PR closed without merge"
	default:
		report.OutcomeStatus = OutcomeStatusPending
		report.Detail = "open"
	}
	return report
}

func verifyPushedCommitRetention(ctx context.Context, item CreatedItemReport, data map[string]any, report OutcomeReport) OutcomeReport {
	shas := metadataStringSlice(item.Metadata, "pushed_commit_shas")
	if sha := outcomeString(item.Metadata["commit_sha"]); sha != "" {
		shas = append(shas, sha)
	}
	if len(shas) == 0 {
		report.OutcomeEvaluation = outcomeEvidence(OutcomeStatusUnknown, EvidenceNone, "missing_execution_state")
		report.Detail = "missing pushed commit evidence"
		return report
	}
	base := outcomeString(data["merge_commit_sha"])
	for _, sha := range shas {
		if !validOutcomeSHA(sha) || !validOutcomeSHA(base) {
			report.OutcomeEvaluation = outcomeEvidence(OutcomeStatusUnknown, EvidenceNone, "missing_execution_state")
			return report
		}
		compare, err := outcomeEvidenceGHAPIGet(ctx, fmt.Sprintf("compare/%s...%s", sha, base), report.Repo)
		if err != nil {
			outcomeAPIError(&report, err, false)
			return report
		}
		status := outcomeString(compare["status"])
		if status != "ahead" && status != "identical" {
			report.OutcomeEvaluation = outcomeEvidence(OutcomeStatusUnknown, EvidenceWeak, "commit_retention_unknown")
			report.Detail = "pushed commits not verified in merged history"
			return report
		}
	}
	report.OutcomeEvaluation = outcomeEvidence(OutcomeStatusAccepted, EvidenceStrong, "merged")
	report.Detail = "pushed commits merged"
	return report
}

// evalGenericSticky never infers action acceptance from target existence.
func evalGenericSticky(ctx context.Context, item CreatedItemReport, repoOverride string) OutcomeReport {
	repo := resolveItemRepo(item, repoOverride)
	num := resolveItemNumber(item)
	report := OutcomeReport{
		Type:         item.Type,
		ObjectURL:    item.URL,
		ObjectNumber: num,
		Repo:         repo,
	}

	if num == 0 || repo == "" {
		report.OutcomeEvaluation = outcomeEvidence(OutcomeStatusUnknown, EvidenceNone, "missing_reference")
		report.Detail = "no object reference to check"
		return report
	}

	_, err := genericOutcomeGHAPIGet(ctx, fmt.Sprintf("issues/%d", num), repo)
	if err != nil {
		report.OutcomeStatus = OutcomeStatusError
		report.EvalError = err.Error()
		return report
	}
	report.Detail = "object still exists"

	report.OutcomeEvaluation = outcomeEvidence(OutcomeStatusUnknown, EvidenceWeak, "target_exists_only")
	return report
}
