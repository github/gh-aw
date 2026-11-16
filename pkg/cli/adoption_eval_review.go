package cli

import (
	"context"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/logger"
)

var adoptionReviewLog = logger.New("cli:adoption_eval_review")

var adoptionReviewGHAPIGet = ghAPIGet
var adoptionReviewGHAPIGetArray = ghAPIGetArray

func evalAddReviewer(ctx context.Context, item CreatedItemReport, repoOverride string) AdoptionReport {
	repo := resolveItemRepo(item, repoOverride)
	num := resolveItemNumber(item)
	report := AdoptionReport{
		Type:         item.Type,
		ObjectURL:    item.URL,
		ObjectNumber: num,
		Repo:         repo,
	}
	adoptionReviewLog.Printf("Evaluating add-reviewer adoption: repo=%s, pr=%d", repo, num)
	if num == 0 || repo == "" {
		report.AdoptionStatus = AdoptionStatusUnknown
		report.EvalError = "missing PR number or repo"
		return report
	}

	requestedReviewers := metadataStringSlice(item.Metadata, "requested_reviewers")
	requestedTeams := metadataStringSlice(item.Metadata, "requested_team_reviewers")

	reviews, err := adoptionReviewGHAPIGetArray(ctx, fmt.Sprintf("pulls/%d/reviews", num), repo)
	if err != nil {
		report.AdoptionStatus = AdoptionStatusUnknown
		report.EvalError = err.Error()
		return report
	}

	requested, err := adoptionReviewGHAPIGet(ctx, fmt.Sprintf("pulls/%d/requested_reviewers", num), repo)
	if err != nil {
		report.AdoptionStatus = AdoptionStatusUnknown
		report.EvalError = err.Error()
		return report
	}

	return classifyRequestedReview(item, requestedReviewers, requestedTeams, reviews, requested, report)

}

func evalSubmitPullRequestReview(ctx context.Context, item CreatedItemReport, repoOverride string) AdoptionReport {
	repo := resolveItemRepo(item, repoOverride)
	num := resolveItemNumber(item)
	report := AdoptionReport{
		Type:         item.Type,
		ObjectURL:    item.URL,
		ObjectNumber: num,
		Repo:         repo,
	}
	adoptionReviewLog.Printf("Evaluating submit-review adoption: repo=%s, pr=%d", repo, num)
	if num == 0 || repo == "" {
		report.AdoptionStatus = AdoptionStatusUnknown
		report.EvalError = "missing PR number or repo"
		return report
	}

	pr, err := adoptionReviewGHAPIGet(ctx, fmt.Sprintf("pulls/%d", num), repo)
	if err != nil {
		report.AdoptionStatus = AdoptionStatusUnknown
		report.EvalError = err.Error()
		return report
	}
	reviews, err := adoptionReviewGHAPIGetArray(ctx, fmt.Sprintf("pulls/%d/reviews", num), repo)
	if err != nil {
		report.AdoptionStatus = AdoptionStatusUnknown
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
	var parsed int64
	switch value := metadata[key].(type) {
	case int:
		parsed = int64(value)
	case int64:
		parsed = value
	case float64:
		if math.IsNaN(value) || math.IsInf(value, 0) || math.Trunc(value) != value || value <= 0 || value > 9007199254740991 {
			return 0
		}
		parsed = int64(value)
	case string:
		var err error
		value = strings.TrimSpace(value)
		if strings.HasPrefix(value, "+") {
			return 0
		}
		parsed, err = strconv.ParseInt(value, 10, 64)
		if err != nil {
			return 0
		}
	default:
		return 0
	}
	if parsed <= 0 || parsed > 9007199254740991 || parsed > int64(math.MaxInt) {
		return 0
	}
	return int(parsed)
}

func adoptionString(raw any) string {
	s := adoptionValue[string](raw)
	return s
}

func adoptionNestedString(raw any, nestedKey string) string {
	obj := adoptionValue[map[string]any](raw)
	if obj == nil {
		return ""
	}
	value := adoptionValue[string](obj[nestedKey])
	return value
}

func extractLogins(raw any) []string {
	items := adoptionValue[[]any](raw)
	out := make([]string, 0, len(items))
	for _, item := range items {
		if login := adoptionNestedString(item, "login"); login != "" {
			out = append(out, login)
		}
	}
	return out
}

func extractTeamSlugs(raw any) []string {
	items := adoptionValue[[]any](raw)
	out := make([]string, 0, len(items))
	for _, item := range items {
		slug := adoptionNestedString(item, "slug")
		if slug == "" {
			slug = adoptionNestedString(item, "name")
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
		state := strings.ToUpper(adoptionString(review["state"]))
		if state == "" || state == "PENDING" { //nolint:tolowerequalfold
			continue
		}
		if timestampOnOrAfter(adoptionString(review["submitted_at"]), threshold) {
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
		state := strings.ToUpper(adoptionString(review["state"]))
		submittedAt := adoptionString(review["submitted_at"])
		if state == "" || state == "PENDING" || submittedAt == "" { //nolint:tolowerequalfold
			continue
		}
		if !timestampOnOrAfter(submittedAt, threshold) {
			continue
		}
		if latest == nil || timestampOnOrAfter(submittedAt, adoptionString(latest["submitted_at"])) {
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
		commitObj := adoptionValue[map[string]any](commit["commit"])
		if timestampOnOrAfter(adoptionNestedString(commitObj["committer"], "date"), threshold) || timestampOnOrAfter(adoptionNestedString(commitObj["author"], "date"), threshold) {
			return true
		}
	}
	return false
}

func classifySubmittedReview(ctx context.Context, item CreatedItemReport, pr map[string]any, reviews []map[string]any, report AdoptionReport) AdoptionReport {
	num, repo := report.ObjectNumber, report.Repo
	reviewID := metadataInt(item.Metadata, "review_id")
	review := findReviewByID(reviews, reviewID)
	if review == nil {
		adoptionReviewLog.Printf("Submitted review not found for PR #%d (reviewID=%d, reviews=%d)", num, reviewID, len(reviews))
		report.Detail = "submitted review not found"
		report.AdoptionEvaluation = adoptionEvidence(AdoptionStatusUnknown, EvidenceWeak, "review_missing")
		return report
	}

	reviewState := strings.ToUpper(adoptionString(review["state"]))
	reviewSubmittedAt := adoptionString(review["submitted_at"])
	prMerged := adoptionValue[bool](pr["merged"])
	prState := adoptionValue[string](pr["state"])

	switch {
	case reviewState == "DISMISSED": //nolint:tolowerequalfold
		report.Detail = "review dismissed by repo admin"
		report.AdoptionEvaluation = adoptionEvidence(AdoptionStatusRejected, EvidenceStrong, "review_dismissed")
		return report
	case prMerged && reviewState == "APPROVED": //nolint:tolowerequalfold
		report.AdoptionStatus = AdoptionStatusAccepted
		report.Detail = "approved review followed by merge"
		report.AdoptionEvaluation = adoptionEvidence(AdoptionStatusAccepted, EvidenceStrong, "review_approved")
		setAdoptionDecisionTime(&report, item.Timestamp, adoptionString(pr["merged_at"]))
		return report
	case prMerged && reviewState == "CHANGES_REQUESTED": //nolint:tolowerequalfold
		commits, err := adoptionReviewGHAPIGetArray(ctx, fmt.Sprintf("pulls/%d/commits", num), repo)
		if err == nil && hasCommitAfterTimestamp(commits, reviewSubmittedAt) {
			report.AdoptionStatus = AdoptionStatusAccepted
			report.Detail = "changes requested, updated, and merged"
			report.AdoptionEvaluation = adoptionEvidence(AdoptionStatusAccepted, EvidenceMedium, "changes_requested_addressed")
			setAdoptionDecisionTime(&report, item.Timestamp, adoptionString(pr["merged_at"]))
			return report
		}
	case prState == "closed" && !prMerged:
		report.AdoptionStatus = AdoptionStatusRejected
		report.Detail = "PR closed without merge after review submission"
		setAdoptionDecisionTime(&report, item.Timestamp, adoptionString(pr["closed_at"]))
		report.AdoptionEvaluation = adoptionEvidence(AdoptionStatusRejected, EvidenceMedium, "closed_without_merge_after_review")
		return report
	case prState == "open" && isLatestReview(reviews, review):
		report.Detail = "review is latest review on open PR"
		report.AdoptionEvaluation = adoptionEvidence(AdoptionStatusPending, EvidenceMedium, "latest_review_pending")
		return report
	}
	report.Detail = "review adoption could not be determined"

	report.AdoptionEvaluation = adoptionEvidence(AdoptionStatusUnknown, EvidenceWeak, "unknown")
	return report
}

func classifyRequestedReview(item CreatedItemReport, requestedReviewers, requestedTeams []string, reviews []map[string]any, requested map[string]any, report AdoptionReport) AdoptionReport {
	adoptionReviewLog.Printf("Fetched %d reviews for PR #%d (requested reviewers=%d, teams=%d)", len(reviews), report.ObjectNumber, len(requestedReviewers), len(requestedTeams))
	latestByReviewer := requestedReviewsAfter(reviews, requestedReviewers, item.Timestamp)

	var approvedReviewer string
	var submittedReviewer string
	for login, review := range latestByReviewer {
		if strings.EqualFold(adoptionString(review["state"]), "APPROVED") {
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
		report.AdoptionEvaluation = adoptionEvidence(AdoptionStatusAccepted, EvidenceStrong, "review_approved")
		return report
	case submittedReviewer != "":
		report.Detail = fmt.Sprintf("requested reviewer %s submitted a review", submittedReviewer)
		report.AdoptionEvaluation = adoptionEvidence(AdoptionStatusAccepted, EvidenceMedium, "review_submitted")
		return report
	}

	// A submitted review does not establish membership in the requested team.
	if len(requestedTeams) > 0 && hasReviewAfterTimestamp(reviews, item.Timestamp) {
		report.Detail = "team reviewer membership is unverified"
		report.AdoptionEvaluation = adoptionEvidence(AdoptionStatusUnknown, EvidenceWeak, "team_membership_unverified")
		return report
	}

	currentUsers := extractLogins(requested["users"])
	currentTeams := extractTeamSlugs(requested["teams"])
	stillPending := intersectsFold(requestedReviewers, currentUsers) || intersectsFold(requestedTeams, currentTeams)
	if stillPending {
		report.Detail = "review request still pending"
		report.AdoptionEvaluation = adoptionEvidence(AdoptionStatusPending, EvidenceMedium, "awaiting_review")
		return report
	}

	if len(requestedReviewers) > 0 || len(requestedTeams) > 0 {
		report.Detail = "review request removed without submitted review"
		report.AdoptionEvaluation = adoptionEvidence(AdoptionStatusRejected, EvidenceStrong, "review_request_removed")
		return report
	}
	report.Detail = "no persisted reviewer request metadata"

	report.AdoptionEvaluation = adoptionEvidence(AdoptionStatusUnknown, EvidenceWeak, "unknown")
	return report
}

func requestedReviewsAfter(reviews []map[string]any, requestedReviewers []string, timestamp string) map[string]map[string]any {
	requestedReviewerSet := make(map[string]struct{}, len(requestedReviewers))
	for _, reviewer := range requestedReviewers {
		requestedReviewerSet[strings.ToLower(reviewer)] = struct{}{}
	}

	latestByReviewer := make(map[string]map[string]any, len(requestedReviewerSet))
	for _, review := range reviews {
		login := strings.ToLower(adoptionNestedString(review["user"], "login"))
		if _, ok := requestedReviewerSet[login]; !ok {
			continue
		}
		state := strings.ToUpper(adoptionString(review["state"]))
		submittedAt := adoptionString(review["submitted_at"])
		if state == "" || state == "PENDING" || submittedAt == "" { //nolint:tolowerequalfold
			continue
		}
		if !timestampOnOrAfter(submittedAt, timestamp) {
			continue
		}
		prev, ok := latestByReviewer[login]
		if !ok || timestampOnOrAfter(submittedAt, adoptionString(prev["submitted_at"])) {
			latestByReviewer[login] = review
		}
	}

	return latestByReviewer
}
