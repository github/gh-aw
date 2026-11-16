package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/github/gh-aw/pkg/logger"
)

var adoptionEvalPRLog = logger.New("cli:adoption_eval_pr")
var adoptionEvalPRGHAPIGet = ghAPIGet
var adoptionEvalPRGHAPIGetArray = ghAPIGetArray

// evalCreatePullRequest checks whether a PR was merged, closed, or is still open.
func evalCreatePullRequest(ctx context.Context, item CreatedItemReport, repoOverride string) AdoptionReport {
	repo := resolveItemRepo(item, repoOverride)
	num := resolveItemNumber(item)
	adoptionEvalPRLog.Printf("Evaluating create_pull_request: repo=%s, num=%d, url=%s", repo, num, item.URL)
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

	data, err := adoptionEvalPRGHAPIGet(ctx, fmt.Sprintf("pulls/%d", num), repo)
	if err != nil {
		adoptionAPIError(&report, err, true)
		return report
	}

	merged := adoptionValue[bool](data["merged"])
	state := adoptionValue[string](data["state"])
	mergedAt := adoptionValue[string](data["merged_at"])
	closedAt := adoptionValue[string](data["closed_at"])

	switch {
	case merged:
		report.AdoptionStatus = AdoptionStatusAccepted
		report.Detail = "merged"
		setAdoptionDecisionTime(&report, item.Timestamp, mergedAt)
	case state == "closed":
		report.AdoptionStatus = AdoptionStatusRejected
		report.Detail = "closed without merge"
		setAdoptionDecisionTime(&report, item.Timestamp, closedAt)
	case state == "open":
		report.AdoptionStatus = AdoptionStatusPending
		report.Detail = "open"
	default:
		report.AdoptionEvaluation = adoptionEvidence(AdoptionStatusUnknown, EvidenceWeak, "unknown")
		return report
	}

	enrichPullRequestEffort(ctx, item, &report)

	return report
}

func adoptionActivityKnown(activity []map[string]any, timestampKey string, skipPending bool) bool {
	for _, entry := range activity {
		if skipPending && adoptionString(entry["state"]) == "PENDING" {
			continue
		}
		_, err := time.Parse(time.RFC3339, adoptionString(entry[timestampKey]))
		if adoptionNestedString(entry["user"], "login") == "" || err != nil {
			return false
		}
	}
	return true
}

func enrichPullRequestEffort(ctx context.Context, item CreatedItemReport, report *AdoptionReport) {
	num, repo := report.ObjectNumber, report.Repo
	comments, err := adoptionEvalPRGHAPIGetArray(ctx, fmt.Sprintf("issues/%d/comments", num), repo)
	if err == nil {
		report.HumanComments = nonBotCommentsAfter(comments, item.Timestamp)
	} else {
		adoptionEvalPRLog.Printf("Comment evidence unavailable: %v", err)
	}
	commentsKnown := err == nil && adoptionActivityKnown(comments, "created_at", false)

	// Count reviews (used for ZeroTouch, stored separately from edits to avoid conflation)
	reviews, err := adoptionEvalPRGHAPIGetArray(ctx, fmt.Sprintf("pulls/%d/reviews", num), repo)
	if err == nil {
		for _, review := range reviews {
			if adoptionString(review["state"]) != "PENDING" && isNonBotActor(review["user"]) && adoptionAfter(adoptionString(review["submitted_at"]), item.Timestamp) {
				report.HumanReviews++
			}
		}
	}
	if err != nil {
		adoptionEvalPRLog.Printf("Review evidence unavailable: %v", err)
	}
	reviewsKnown := err == nil && adoptionActivityKnown(reviews, "submitted_at", true)
	commits, err := adoptionEvalPRGHAPIGetArray(ctx, fmt.Sprintf("pulls/%d/commits", num), repo)
	commitsKnown := err == nil
	if err != nil {
		adoptionEvalPRLog.Printf("Commit evidence unavailable: %v", err)
	}
	if commitsKnown {
		for _, commit := range commits {
			commitData := adoptionValue[map[string]any](commit["commit"])
			_, committerErr := time.Parse(time.RFC3339, adoptionNestedString(commitData["committer"], "date"))
			_, authorErr := time.Parse(time.RFC3339, adoptionNestedString(commitData["author"], "date"))
			if adoptionNestedString(commit["author"], "login") == "" || committerErr != nil && authorErr != nil {
				commitsKnown = false
			}
			if isNonBotActor(commit["author"]) &&
				(adoptionAfter(adoptionNestedString(commitData["committer"], "date"), item.Timestamp) || adoptionAfter(adoptionNestedString(commitData["author"], "date"), item.Timestamp)) {
				report.HumanEdits++
			}
		}
	}

	_, timestampErr := time.Parse(time.RFC3339, item.Timestamp)
	report.ZeroTouchEvidenceComplete = timestampErr == nil && commentsKnown && reviewsKnown && commitsKnown
	report.ZeroTouch = report.AdoptionStatus == AdoptionStatusAccepted && report.ZeroTouchEvidenceComplete && report.HumanComments == 0 && report.HumanReviews == 0 && report.HumanEdits == 0

}
