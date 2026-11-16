package cli

import (
	"context"
	"fmt"

	"github.com/github/gh-aw/pkg/logger"
)

var adoptionEvalGenericLog = logger.New("cli:adoption_eval_generic")
var genericAdoptionGHAPIGet = ghAPIGet
var closeStickyGHAPIGet = ghAPIGet
var closeStickyGHAPIGetArray = ghAPIGetArray

// evalCloseSticky checks whether a closed issue or PR stayed closed.
func evalCloseSticky(ctx context.Context, item CreatedItemReport, repoOverride string) AdoptionReport {
	repo := resolveItemRepo(item, repoOverride)
	num := resolveItemNumber(item)
	adoptionEvalGenericLog.Printf("Evaluating close_sticky: type=%s, repo=%s, num=%d", item.Type, repo, num)
	report := AdoptionReport{
		Type:         item.Type,
		ObjectURL:    item.URL,
		ObjectNumber: num,
		Repo:         repo,
	}
	if num == 0 || repo == "" {
		report.AdoptionStatus = AdoptionStatusUnknown
		report.EvalError = "missing number or repo"
		return report
	}

	endpoint := fmt.Sprintf("issues/%d", num)
	if item.Type == "close_pull_request" {
		endpoint = fmt.Sprintf("pulls/%d", num)
	}

	data, err := closeStickyGHAPIGet(ctx, endpoint, repo)
	if err != nil {
		adoptionAPIError(&report, err, false)
		return report
	}

	state := adoptionValue[string](data["state"])
	merged := adoptionValue[bool](data["merged"])
	if state != "closed" {
		report.AdoptionStatus = AdoptionStatusRejected
		report.Detail = "reopened"
		return report
	}

	if merged {
		report.AdoptionEvaluation = adoptionEvidence(AdoptionStatusRejected, EvidenceStrong, "closed_by_merge")
		report.Detail = "merged"
		return report
	}

	closedByBot, err := isClosedByLifecycleBot(ctx, num, repo)
	if err != nil {
		report.AdoptionStatus = AdoptionStatusUnknown
		report.EvalError = err.Error()
		report.Detail = "close provenance unavailable"
		return report
	}

	if closedByBot {
		report.AdoptionStatus = AdoptionStatusLifecycle
		report.Detail = "closed by bot (lifecycle_bot_close)"
	} else {
		report.AdoptionEvaluation = adoptionEvidence(AdoptionStatusAccepted, EvidenceStrong, "closed")
		report.Detail = "closed"
	}
	return report
}

func isClosedByLifecycleBot(ctx context.Context, number int, repo string) (bool, error) {
	return isLatestCloseByBot(ctx, number, repo, closeStickyGHAPIGetArray)
}

// evalCloseDiscussion has no GraphQL evaluator yet.
func evalCloseDiscussion(ctx context.Context, item CreatedItemReport, repoOverride string) AdoptionReport {
	return unsupportedAdoption(item, repoOverride)
}

// evalCreateDiscussion checks whether a discussion received replies.
func evalCreateDiscussion(ctx context.Context, item CreatedItemReport, repoOverride string) AdoptionReport {
	return unsupportedAdoption(item, repoOverride)
}

// evalHideComment checks whether a hidden comment is still hidden.
func evalHideComment(ctx context.Context, item CreatedItemReport, repoOverride string) AdoptionReport {
	return unsupportedAdoption(item, repoOverride)
}

// evalAssignMilestone checks whether a milestone assignment stuck.
func evalAssignMilestone(ctx context.Context, item CreatedItemReport, repoOverride string) AdoptionReport {
	repo := resolveItemRepo(item, repoOverride)
	num := resolveItemNumber(item)
	adoptionEvalGenericLog.Printf("Evaluating assign_milestone: repo=%s, num=%d", repo, num)
	report := AdoptionReport{
		Type:         item.Type,
		ObjectURL:    item.URL,
		ObjectNumber: num,
		Repo:         repo,
	}
	if num == 0 || repo == "" {
		report.AdoptionStatus = AdoptionStatusUnknown
		report.EvalError = "missing number or repo"
		return report
	}

	expected := metadataInt(item.Metadata, "milestone_number")
	if expected <= 0 {
		report.AdoptionEvaluation = adoptionEvidence(AdoptionStatusUnknown, EvidenceNone, "missing_execution_state")
		report.Detail = "missing execution state"
		return report
	}
	data, err := adoptionEvidenceGHAPIGet(ctx, fmt.Sprintf("issues/%d", num), repo)
	if err != nil {
		report.AdoptionStatus = AdoptionStatusUnknown
		report.EvalError = err.Error()
		return report
	}

	milestone := adoptionValue[map[string]any](data["milestone"])
	if metadataInt(milestone, "number") == expected {
		report.Detail = "milestone still assigned"
		report.AdoptionEvaluation = adoptionEvidence(AdoptionStatusAccepted, EvidenceMedium, "milestone_assigned")
	} else {
		report.Detail = "milestone removed"
		report.AdoptionEvaluation = adoptionEvidence(AdoptionStatusRejected, EvidenceMedium, "milestone_removed")
	}
	return report
}

// evalReviewComment checks whether a PR review comment thread was resolved or engaged.
func evalReviewComment(ctx context.Context, item CreatedItemReport, repoOverride string) AdoptionReport {
	return unsupportedAdoption(item, repoOverride)
}

// evalResolveThread checks whether a resolved review thread stayed resolved.
func evalResolveThread(ctx context.Context, item CreatedItemReport, repoOverride string) AdoptionReport {
	return unsupportedAdoption(item, repoOverride)
}

// evalMarkReady checks whether a PR marked as ready received reviews.
func evalMarkReady(ctx context.Context, item CreatedItemReport, repoOverride string) AdoptionReport {
	repo := resolveItemRepo(item, repoOverride)
	num := resolveItemNumber(item)
	adoptionEvalGenericLog.Printf("Evaluating mark_ready: repo=%s, num=%d", repo, num)
	report := AdoptionReport{
		Type:         item.Type,
		ObjectURL:    item.URL,
		ObjectNumber: num,
		Repo:         repo,
	}
	if num == 0 || repo == "" {
		report.AdoptionStatus = AdoptionStatusUnknown
		report.EvalError = "missing number or repo"
		return report
	}

	reviews, err := adoptionEvidenceGHAPIGetArray(ctx, fmt.Sprintf("pulls/%d/reviews", num), repo)
	if err != nil {
		report.AdoptionStatus = AdoptionStatusUnknown
		report.EvalError = err.Error()
		return report
	}

	reviewed := false
	for _, review := range reviews {
		if isNonBotActor(review["user"]) && adoptionString(review["state"]) != "PENDING" && adoptionAfter(adoptionString(review["submitted_at"]), item.Timestamp) {
			reviewed = true
		}
	}
	if reviewed {
		report.Detail = fmt.Sprintf("%d reviews submitted", len(reviews))
		report.AdoptionEvaluation = adoptionEvidence(AdoptionStatusAccepted, EvidenceMedium, "reviewed")
	} else {
		data, derr := adoptionEvidenceGHAPIGet(ctx, fmt.Sprintf("pulls/%d", num), repo)
		if derr == nil {
			state := adoptionValue[string](data["state"])
			if state == "open" {
				report.AdoptionEvaluation = adoptionEvidence(AdoptionStatusPending, EvidenceMedium, "awaiting_review")
				report.Detail = "awaiting review"
			} else {
				report.AdoptionEvaluation = adoptionEvidence(AdoptionStatusRejected, EvidenceMedium, "no_effect")
				report.Detail = "closed/merged without review"
			}
		} else {
			adoptionAPIError(&report, derr, false)
		}
	}
	return report
}

// evalPushToPRBranch checks whether the PR the code was pushed to got merged.
func evalPushToPRBranch(ctx context.Context, item CreatedItemReport, repoOverride string) AdoptionReport {
	repo := resolveItemRepo(item, repoOverride)
	num := resolveItemNumber(item)
	adoptionEvalGenericLog.Printf("Evaluating push_to_pr_branch: repo=%s, num=%d", repo, num)
	report := AdoptionReport{
		Type:         item.Type,
		ObjectURL:    item.URL,
		ObjectNumber: num,
		Repo:         repo,
	}
	if num == 0 || repo == "" {
		report.AdoptionStatus = AdoptionStatusUnknown
		report.EvalError = "missing PR number or repo"
		return report
	}

	data, err := adoptionEvidenceGHAPIGet(ctx, fmt.Sprintf("pulls/%d", num), repo)
	if err != nil {
		report.AdoptionStatus = AdoptionStatusUnknown
		report.EvalError = err.Error()
		return report
	}

	merged := adoptionValue[bool](data["merged"])
	state := adoptionValue[string](data["state"])
	adoptionEvalGenericLog.Printf("push_to_pr_branch PR #%d state: merged=%t, state=%s", num, merged, state)

	switch {
	case merged:
		return verifyPushedCommitRetention(ctx, item, data, report)
	case state == "closed":
		report.AdoptionStatus = AdoptionStatusRejected
		report.Detail = "PR closed without merge"
	default:
		report.AdoptionStatus = AdoptionStatusPending
		report.Detail = "open"
	}
	return report
}

func verifyPushedCommitRetention(ctx context.Context, item CreatedItemReport, data map[string]any, report AdoptionReport) AdoptionReport {
	shas := metadataStringSlice(item.Metadata, "pushed_commit_shas")
	if sha := adoptionString(item.Metadata["commit_sha"]); sha != "" {
		shas = append(shas, sha)
	}
	if len(shas) == 0 {
		report.AdoptionEvaluation = adoptionEvidence(AdoptionStatusUnknown, EvidenceNone, "missing_execution_state")
		report.Detail = "missing pushed commit evidence"
		return report
	}
	base := adoptionString(data["merge_commit_sha"])
	for _, sha := range shas {
		if !validAdoptionSHA(sha) || !validAdoptionSHA(base) {
			report.AdoptionEvaluation = adoptionEvidence(AdoptionStatusUnknown, EvidenceNone, "missing_execution_state")
			return report
		}
		compare, err := adoptionEvidenceGHAPIGet(ctx, fmt.Sprintf("compare/%s...%s", sha, base), report.Repo)
		if err != nil {
			adoptionAPIError(&report, err, false)
			return report
		}
		status := adoptionString(compare["status"])
		if status != "ahead" && status != "identical" {
			report.AdoptionEvaluation = adoptionEvidence(AdoptionStatusUnknown, EvidenceWeak, "commit_retention_unknown")
			report.Detail = "pushed commits not verified in merged history"
			return report
		}
	}
	report.AdoptionEvaluation = adoptionEvidence(AdoptionStatusAccepted, EvidenceStrong, "merged")
	report.Detail = "pushed commits merged"
	return report
}

// evalGenericSticky never infers action acceptance from target existence.
func evalGenericSticky(ctx context.Context, item CreatedItemReport, repoOverride string) AdoptionReport {
	repo := resolveItemRepo(item, repoOverride)
	num := resolveItemNumber(item)
	report := AdoptionReport{
		Type:         item.Type,
		ObjectURL:    item.URL,
		ObjectNumber: num,
		Repo:         repo,
	}

	if num == 0 || repo == "" {
		report.AdoptionEvaluation = adoptionEvidence(AdoptionStatusUnknown, EvidenceNone, "missing_reference")
		report.Detail = "no object reference to check"
		return report
	}

	_, err := genericAdoptionGHAPIGet(ctx, fmt.Sprintf("issues/%d", num), repo)
	if err != nil {
		report.AdoptionStatus = AdoptionStatusUnknown
		report.EvalError = err.Error()
		return report
	}
	report.Detail = "object still exists"

	report.AdoptionEvaluation = adoptionEvidence(AdoptionStatusUnknown, EvidenceWeak, "target_exists_only")
	return report
}
