package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var adoptionEvalCommentLog = logger.New("cli:adoption_eval_comment")

// evalAddComment checks whether a comment received replies, reactions, or was deleted/hidden.
func evalAddComment(ctx context.Context, item CreatedItemReport, repoOverride string) AdoptionReport {
	repo := resolveItemRepo(item, repoOverride)
	adoptionEvalCommentLog.Printf("Evaluating add_comment: repo=%s, url=%s", repo, item.URL)
	report := AdoptionReport{
		Type:      item.Type,
		ObjectURL: item.URL,
		Repo:      repo,
	}
	issueNumber := parseNumberFromURL(item.URL)
	if repo == "" || issueNumber == 0 {
		report.AdoptionEvaluation = adoptionEvidence(AdoptionStatusUnknown, EvidenceNone, "missing_reference")
		return report
	}

	// Extract comment ID from URL: .../issues/123#issuecomment-456789 or .../comments/456789
	commentID := extractCommentID(item.URL)
	if commentID == "" {
		adoptionEvalCommentLog.Printf("Unable to extract comment ID from URL: %s", item.URL)
		report.AdoptionStatus = AdoptionStatusUnknown
		report.EvalError = "cannot extract comment ID from URL"
		return report
	}

	data, err := adoptionEvidenceGHAPIGet(ctx, "issues/comments/"+commentID, repo)
	if err != nil {
		adoptionAPIError(&report, err, true)
		return report
	}

	return classifyComment(ctx, item, data, report, issueNumber)
}

// extractCommentID extracts the numeric comment ID from a GitHub comment URL.
// Handles formats like:
//
//	https://github.com/owner/repo/issues/123#issuecomment-456789
//	https://github.com/owner/repo/pull/123#issuecomment-456789
func extractCommentID(url string) string {
	if _, after, found := strings.Cut(url, "#issuecomment-"); found {
		return after
	}
	// Fallback: look for /comments/ID pattern
	const commentsPrefix = "/comments/"
	if idx := strings.LastIndex(url, commentsPrefix); idx >= 0 {
		rest := url[idx+len(commentsPrefix):]
		// Take only digits
		end := len(rest)
		for i, digit := range rest {
			if digit < '0' || digit > '9' {
				end = i
				break
			}
		}
		if end > 0 {
			return rest[:end]
		}
	}
	return ""
}

func classifyComment(ctx context.Context, item CreatedItemReport, data map[string]any, report AdoptionReport, issueNumber int) AdoptionReport {
	repo := report.Repo
	// Check reactions
	reactions := adoptionValue[map[string]any](data["reactions"])
	totalReactions := 0
	if reactions != nil {
		if tc, ok := reactions["total_count"].(float64); ok {
			totalReactions = int(tc)
		}
	}

	// Check if the comment is minimized (hidden)
	// The REST API field is "performed_via_github_app" but minimized state
	// is not directly in REST. We approximate: if the comment body is empty
	// or the node_id can be checked via GraphQL. For now, use reactions+replies.

	// To check replies, we need the issue number and look for comments posted after this one
	replyCount := 0
	if issueNumber > 0 {
		commentList, cerr := adoptionEvidenceGHAPIGetArray(ctx, fmt.Sprintf("issues/%d/comments", issueNumber), repo)
		if cerr == nil {
			createdAt := adoptionValue[string](data["created_at"])
			replyCount = nonBotCommentsAfter(commentList, createdAt)
		} else if totalReactions == 0 {
			adoptionAPIError(&report, cerr, false)
			return report
		} else {
			adoptionEvalCommentLog.Printf("Reply evidence unavailable: %v", cerr)
		}
	}

	report.HumanComments = replyCount

	switch {
	case totalReactions > 0 || replyCount > 0:
		report.Detail = fmt.Sprintf("%d reactions, %d replies", totalReactions, replyCount)
		report.AdoptionEvaluation = adoptionEvidence(AdoptionStatusAccepted, EvidenceMedium, "acted_on")
	default:
		report.Detail = "no follow-up"
		report.AdoptionEvaluation = adoptionEvidence(AdoptionStatusPending, EvidenceMedium, "pending")
	}

	return report
}
