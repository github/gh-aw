package cli

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/logger"
)

var outcomeReviewLog = logger.New("cli:outcome_eval_review")

var outcomeReviewGHAPIGet = ghAPIGet
var outcomeReviewGHAPIGetArray = ghAPIGetArray

func evalAddReviewer(ctx context.Context, item CreatedItemReport, repoOverride string) OutcomeReport {
	repo := resolveItemRepo(item, repoOverride)
	num := resolveItemNumber(item)
	report := OutcomeReport{
		Type:         item.Type,
		ObjectURL:    item.URL,
		ObjectNumber: num,
		Repo:         repo,
	}
	outcomeReviewLog.Printf("Evaluating add-reviewer outcome: repo=%s, pr=%d", repo, num)
	if num == 0 || repo == "" {
		report.OutcomeStatus = OutcomeStatusError
		report.EvalError = "missing PR number or repo"
		return report
	}

	requestedReviewers := metadataStringSlice(item.Metadata, "requested_reviewers")
	requestedTeams := metadataStringSlice(item.Metadata, "requested_team_reviewers")

	reviews, err := outcomeReviewGHAPIGetArray(ctx, fmt.Sprintf("pulls/%d/reviews", num), repo)
	if err != nil {
		report.OutcomeStatus = OutcomeStatusError
		report.EvalError = err.Error()
		return report
	}

	requested, err := outcomeReviewGHAPIGet(ctx, fmt.Sprintf("pulls/%d/requested_reviewers", num), repo)
	if err != nil {
		report.OutcomeStatus = OutcomeStatusError
		report.EvalError = err.Error()
		return report
	}

	return classifyRequestedReview(item, requestedReviewers, requestedTeams, reviews, requested, report)

}

func evalSubmitPullRequestReview(ctx context.Context, item CreatedItemReport, repoOverride string) OutcomeReport {
	repo := resolveItemRepo(item, repoOverride)
	num := resolveItemNumber(item)
	report := OutcomeReport{
		Type:         item.Type,
		ObjectURL:    item.URL,
		ObjectNumber: num,
		Repo:         repo,
	}
	outcomeReviewLog.Printf("Evaluating submit-review outcome: repo=%s, pr=%d", repo, num)
	if num == 0 || repo == "" {
		report.OutcomeStatus = OutcomeStatusError
		report.EvalError = "missing PR number or repo"
		return report
	}

	pr, err := outcomeReviewGHAPIGet(ctx, fmt.Sprintf("pulls/%d", num), repo)
	if err != nil {
		report.OutcomeStatus = OutcomeStatusError
		report.EvalError = err.Error()
		return report
	}
	reviews, err := outcomeReviewGHAPIGetArray(ctx, fmt.Sprintf("pulls/%d/reviews", num), repo)
	if err != nil {
		report.OutcomeStatus = OutcomeStatusError
		report.EvalError = err.Error()
		return report
	}

	return classifySubmittedReview(ctx, item, pr, reviews, report)

}

func metadataStringSlice(metadata map[string]any, key string) []string {
	if metadata == nil {
		return nil
	}
	raw, ok := metadata[key]
	if !ok {
		return nil
	}
	switch values := raw.(type) {
	case []string:
		return values
	case []any:
		out := make([]string, 0, len(values))
		for _, value := range values {
			s := strings.TrimSpace(fmt.Sprint(value))
			if s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func metadataInt(metadata map[string]any, key string) int {
	if metadata == nil {
		return 0
	}
	switch value := metadata[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	case string:
		var parsed int
		if n, err := fmt.Sscanf(value, "%d", &parsed); err != nil || n != 1 {
			return 0
		}
		return parsed
	}
	return 0
}

func outcomeString(raw any) string {
	s := outcomeValue[string](raw)
	return s
}

func outcomeNestedString(raw any, nestedKey string) string {
	obj := outcomeValue[map[string]any](raw)
	if obj == nil {
		return ""
	}
	value := outcomeValue[string](obj[nestedKey])
	return value
}

func extractLogins(raw any) []string {
	items := outcomeValue[[]any](raw)
	out := make([]string, 0, len(items))
	for _, item := range items {
		if login := outcomeNestedString(item, "login"); login != "" {
			out = append(out, login)
		}
	}
	return out
}

func extractTeamSlugs(raw any) []string {
	items := outcomeValue[[]any](raw)
	out := make([]string, 0, len(items))
	for _, item := range items {
		slug := outcomeNestedString(item, "slug")
		if slug == "" {
			slug = outcomeNestedString(item, "name")
		}
		if slug != "" {
			out = append(out, slug)
		}
	}
	return out
}

func intersectsFold(a []string, b []string) bool {
	for _, left := range a {
		if slices.ContainsFunc(b, func(right string) bool {
			return strings.EqualFold(left, right)
		}) {
			return true
		}
	}
	return false
}

func timestampOnOrAfter(candidate string, threshold string) bool {
	if candidate == "" {
		return false
	}
	if threshold == "" {
		return true
	}
	candidateTime, err := time.Parse(time.RFC3339, candidate)
	if err != nil {
		return false
	}
	thresholdTime, err := time.Parse(time.RFC3339, threshold)
	if err != nil {
		return false
	}
	return !candidateTime.Before(thresholdTime)
}

func hasReviewAfterTimestamp(reviews []map[string]any, threshold string) bool {
	for _, review := range reviews {
		state := strings.ToUpper(outcomeString(review["state"]))
		if state == "" || state == "PENDING" { //nolint:tolowerequalfold
			continue
		}
		if timestampOnOrAfter(outcomeString(review["submitted_at"]), threshold) {
			return true
		}
	}
	return false
}

func findReviewByID(reviews []map[string]any, reviewID int) map[string]any {
	if reviewID == 0 {
		return nil
	}
	for _, review := range reviews {
		if metadataInt(review, "id") == reviewID {
			return review
		}
	}
	return nil
}

func latestReviewAfterTimestamp(reviews []map[string]any, threshold string) map[string]any {
	var latest map[string]any
	for _, review := range reviews {
		state := strings.ToUpper(outcomeString(review["state"]))
		submittedAt := outcomeString(review["submitted_at"])
		if state == "" || state == "PENDING" || submittedAt == "" { //nolint:tolowerequalfold
			continue
		}
		if !timestampOnOrAfter(submittedAt, threshold) {
			continue
		}
		if latest == nil || timestampOnOrAfter(submittedAt, outcomeString(latest["submitted_at"])) {
			latest = review
		}
	}
	return latest
}

func isLatestReview(reviews []map[string]any, review map[string]any) bool {
	latest := latestReviewAfterTimestamp(reviews, "")
	return latest != nil && metadataInt(latest, "id") == metadataInt(review, "id")
}

func hasCommitAfterTimestamp(commits []map[string]any, threshold string) bool {
	for _, commit := range commits {
		commitObj := outcomeValue[map[string]any](commit["commit"])
		if timestampOnOrAfter(outcomeNestedString(commitObj["committer"], "date"), threshold) || timestampOnOrAfter(outcomeNestedString(commitObj["author"], "date"), threshold) {
			return true
		}
	}
	return false
}

func classifySubmittedReview(ctx context.Context, item CreatedItemReport, pr map[string]any, reviews []map[string]any, report OutcomeReport) OutcomeReport {
	num, repo := report.ObjectNumber, report.Repo
	reviewID := metadataInt(item.Metadata, "review_id")
	review := findReviewByID(reviews, reviewID)
	if review == nil {
		outcomeReviewLog.Printf("Submitted review not found for PR #%d (reviewID=%d, reviews=%d)", num, reviewID, len(reviews))
		report.Detail = "submitted review not found"
		report.OutcomeEvaluation = outcomeEvidence(OutcomeStatusUnknown, EvidenceWeak, "review_missing")
		return report
	}

	reviewState := strings.ToUpper(outcomeString(review["state"]))
	reviewSubmittedAt := outcomeString(review["submitted_at"])
	prMerged := outcomeValue[bool](pr["merged"])
	prState := outcomeValue[string](pr["state"])

	switch {
	case reviewState == "DISMISSED": //nolint:tolowerequalfold
		report.Detail = "review dismissed by repo admin"
		report.OutcomeEvaluation = outcomeEvidence(OutcomeStatusRejected, EvidenceStrong, "review_dismissed")
		return report
	case prMerged && reviewState == "APPROVED": //nolint:tolowerequalfold
		report.OutcomeStatus = OutcomeStatusAccepted
		report.Detail = "approved review followed by merge"
		report.TimeToOutcomeHours = timeBetween(item.Timestamp, outcomeString(pr["merged_at"]))
		report.OutcomeEvaluation = outcomeEvidence(OutcomeStatusAccepted, EvidenceStrong, "review_approved")
		return report
	case prMerged && reviewState == "CHANGES_REQUESTED": //nolint:tolowerequalfold
		commits, err := outcomeReviewGHAPIGetArray(ctx, fmt.Sprintf("pulls/%d/commits", num), repo)
		if err == nil && hasCommitAfterTimestamp(commits, reviewSubmittedAt) {
			report.OutcomeStatus = OutcomeStatusAccepted
			report.Detail = "changes requested, updated, and merged"
			report.TimeToOutcomeHours = timeBetween(item.Timestamp, outcomeString(pr["merged_at"]))
			report.OutcomeEvaluation = outcomeEvidence(OutcomeStatusAccepted, EvidenceMedium, "changes_requested_addressed")
			return report
		}
	case prState == "closed" && !prMerged:
		report.OutcomeStatus = OutcomeStatusRejected
		report.Detail = "PR closed without merge after review submission"
		report.TimeToOutcomeHours = timeBetween(item.Timestamp, outcomeString(pr["closed_at"]))
		report.OutcomeEvaluation = outcomeEvidence(OutcomeStatusRejected, EvidenceMedium, "closed_without_merge_after_review")
		return report
	case prState == "open" && isLatestReview(reviews, review):
		report.Detail = "review is latest review on open PR"
		report.OutcomeEvaluation = outcomeEvidence(OutcomeStatusPending, EvidenceMedium, "latest_review_pending")
		return report
	}
	report.Detail = "review outcome could not be determined"

	report.OutcomeEvaluation = outcomeEvidence(OutcomeStatusUnknown, EvidenceWeak, "unknown")
	return report
}

func classifyRequestedReview(item CreatedItemReport, requestedReviewers, requestedTeams []string, reviews []map[string]any, requested map[string]any, report OutcomeReport) OutcomeReport {
	outcomeReviewLog.Printf("Fetched %d reviews for PR #%d (requested reviewers=%d, teams=%d)", len(reviews), report.ObjectNumber, len(requestedReviewers), len(requestedTeams))
	latestByReviewer := requestedReviewsAfter(reviews, requestedReviewers, item.Timestamp)

	var approvedReviewer string
	var submittedReviewer string
	for login, review := range latestByReviewer {
		if strings.EqualFold(outcomeString(review["state"]), "APPROVED") {
			approvedReviewer = login
			break
		}
		if submittedReviewer == "" {
			submittedReviewer = login
		}
	}

	switch {
	case approvedReviewer != "":
		report.Detail = fmt.Sprintf("requested reviewer %s approved", approvedReviewer)
		report.OutcomeEvaluation = outcomeEvidence(OutcomeStatusAccepted, EvidenceStrong, "review_approved")
		return report
	case submittedReviewer != "":
		report.Detail = fmt.Sprintf("requested reviewer %s submitted a review", submittedReviewer)
		report.OutcomeEvaluation = outcomeEvidence(OutcomeStatusAccepted, EvidenceMedium, "review_submitted")
		return report
	}

	// A submitted review does not establish membership in the requested team.
	if len(requestedTeams) > 0 && hasReviewAfterTimestamp(reviews, item.Timestamp) {
		report.Detail = "team reviewer membership is unverified"
		report.OutcomeEvaluation = outcomeEvidence(OutcomeStatusUnknown, EvidenceWeak, "team_membership_unverified")
		return report
	}

	currentUsers := extractLogins(requested["users"])
	currentTeams := extractTeamSlugs(requested["teams"])
	stillPending := intersectsFold(requestedReviewers, currentUsers) || intersectsFold(requestedTeams, currentTeams)
	if stillPending {
		report.Detail = "review request still pending"
		report.OutcomeEvaluation = outcomeEvidence(OutcomeStatusPending, EvidenceMedium, "awaiting_review")
		return report
	}

	if len(requestedReviewers) > 0 || len(requestedTeams) > 0 {
		report.Detail = "review request removed without submitted review"
		report.OutcomeEvaluation = outcomeEvidence(OutcomeStatusRejected, EvidenceStrong, "review_request_removed")
		return report
	}
	report.Detail = "no persisted reviewer request metadata"

	report.OutcomeEvaluation = outcomeEvidence(OutcomeStatusUnknown, EvidenceWeak, "unknown")
	return report
}

func requestedReviewsAfter(reviews []map[string]any, requestedReviewers []string, timestamp string) map[string]map[string]any {
	requestedReviewerSet := make(map[string]struct{}, len(requestedReviewers))
	for _, reviewer := range requestedReviewers {
		requestedReviewerSet[strings.ToLower(reviewer)] = struct{}{}
	}

	latestByReviewer := make(map[string]map[string]any, len(requestedReviewerSet))
	for _, review := range reviews {
		login := strings.ToLower(outcomeNestedString(review["user"], "login"))
		if _, ok := requestedReviewerSet[login]; !ok {
			continue
		}
		state := strings.ToUpper(outcomeString(review["state"]))
		submittedAt := outcomeString(review["submitted_at"])
		if state == "" || state == "PENDING" || submittedAt == "" { //nolint:tolowerequalfold
			continue
		}
		if !timestampOnOrAfter(submittedAt, timestamp) {
			continue
		}
		prev, ok := latestByReviewer[login]
		if !ok || timestampOnOrAfter(submittedAt, outcomeString(prev["submitted_at"])) {
			latestByReviewer[login] = review
		}
	}

	return latestByReviewer
}
