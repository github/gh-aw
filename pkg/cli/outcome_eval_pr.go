package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/logger"
)

var outcomeEvalPRLog = logger.New("cli:outcome_eval_pr")
var outcomeEvalPRGHAPIGet = ghAPIGet
var outcomeEvalPRGHAPIGetArray = ghAPIGetArray

// evalCreatePullRequest checks whether a PR was merged, closed, or is still open.
func evalCreatePullRequest(ctx context.Context, item CreatedItemReport, repoOverride string) OutcomeReport {
	repo := resolveItemRepo(item, repoOverride)
	num := resolveItemNumber(item)
	outcomeEvalPRLog.Printf("Evaluating create_pull_request: repo=%s, num=%d, url=%s", repo, num, item.URL)
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

	data, err := outcomeEvalPRGHAPIGet(ctx, fmt.Sprintf("pulls/%d", num), repo)
	if err != nil {
		outcomeAPIError(&report, err, true)
		return report
	}

	merged, _ := data["merged"].(bool)
	state, _ := data["state"].(string)
	mergedAt, _ := data["merged_at"].(string)
	closedAt, _ := data["closed_at"].(string)

	switch {
	case merged:
		report.OutcomeStatus = OutcomeStatusAccepted
		report.Detail = "merged"
		if mergedAt != "" && item.Timestamp != "" {
			report.TimeToOutcomeHours = timeBetween(item.Timestamp, mergedAt)
		}
	case state == "closed":
		report.OutcomeStatus = OutcomeStatusRejected
		report.Detail = "closed without merge"
		if closedAt != "" && item.Timestamp != "" {
			report.TimeToOutcomeHours = timeBetween(item.Timestamp, closedAt)
		}
	case state == "open":
		report.OutcomeStatus = OutcomeStatusPending
		report.Detail = "open"
	default:
		report.OutcomeEvaluation = outcomeEvidence(OutcomeStatusUnknown, EvidenceWeak, "unknown")
		return report
	}

	comments, err := outcomeEvalPRGHAPIGetArray(ctx, fmt.Sprintf("issues/%d/comments", num), repo)
	if err == nil {
		report.HumanComments = nonBotCommentsAfter(comments, item.Timestamp)
	} else {
		outcomeEvalPRLog.Printf("Comment evidence unavailable: %v", err)
	}
	commentsKnown := err == nil && outcomeActivityKnown(comments, "created_at", false)

	// Count reviews (used for ZeroTouch, stored separately from edits to avoid conflation)
	reviews, err := outcomeEvalPRGHAPIGetArray(ctx, fmt.Sprintf("pulls/%d/reviews", num), repo)
	if err == nil {
		for _, review := range reviews {
			if outcomeString(review["state"]) != "PENDING" && isNonBotActor(review["user"]) && outcomeAfter(outcomeString(review["submitted_at"]), item.Timestamp) {
				report.HumanReviews++
			}
		}
	}
	if err != nil {
		outcomeEvalPRLog.Printf("Review evidence unavailable: %v", err)
	}
	reviewsKnown := err == nil && outcomeActivityKnown(reviews, "submitted_at", true)
	commits, err := outcomeEvalPRGHAPIGetArray(ctx, fmt.Sprintf("pulls/%d/commits", num), repo)
	commitsKnown := err == nil
	if err != nil {
		outcomeEvalPRLog.Printf("Commit evidence unavailable: %v", err)
	}
	if commitsKnown {
		for _, commit := range commits {
			commitData, _ := commit["commit"].(map[string]any)
			_, committerErr := time.Parse(time.RFC3339, outcomeNestedString(commitData["committer"], "date"))
			_, authorErr := time.Parse(time.RFC3339, outcomeNestedString(commitData["author"], "date"))
			if outcomeNestedString(commit["author"], "login") == "" || committerErr != nil && authorErr != nil {
				commitsKnown = false
			}
			if isNonBotActor(commit["author"]) && !strings.EqualFold(outcomeNestedString(commit["author"], "login"), outcomeNestedString(data["user"], "login")) &&
				(outcomeAfter(outcomeNestedString(commitData["committer"], "date"), item.Timestamp) || outcomeAfter(outcomeNestedString(commitData["author"], "date"), item.Timestamp)) {
				report.HumanEdits++
			}
		}
	}

	if report.OutcomeStatus == OutcomeStatusAccepted {
		_, timestampErr := time.Parse(time.RFC3339, item.Timestamp)
		report.ZeroTouch = timestampErr == nil && commentsKnown && reviewsKnown && commitsKnown && report.HumanComments == 0 && report.HumanReviews == 0 && report.HumanEdits == 0
	}

	return report
}

func outcomeActivityKnown(activity []map[string]any, timestampKey string, skipPending bool) bool {
	for _, entry := range activity {
		if skipPending && outcomeString(entry["state"]) == "PENDING" {
			continue
		}
		_, err := time.Parse(time.RFC3339, outcomeString(entry[timestampKey]))
		if outcomeNestedString(entry["user"], "login") == "" || err != nil {
			return false
		}
	}
	return true
}
