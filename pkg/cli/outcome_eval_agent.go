package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var outcomeEvalAgentLog = logger.New("cli:outcome_eval_agent")

// evalAssignToAgent checks whether an agent assignment led to a PR that was merged.
func evalAssignToAgent(ctx context.Context, item CreatedItemReport, repoOverride string) OutcomeReport {
	repo := resolveItemRepo(item, repoOverride)
	num := resolveItemNumber(item)
	outcomeEvalAgentLog.Printf("Evaluating assign_to_agent: repo=%s, num=%d, url=%s", repo, num, item.URL)
	report := OutcomeReport{
		Type:         item.Type,
		ObjectURL:    item.URL,
		ObjectNumber: num,
		Repo:         repo,
	}
	if num == 0 || repo == "" {
		outcomeEvalAgentLog.Printf("Missing issue number or repo: num=%d, repo=%s", num, repo)
		report.OutcomeStatus = OutcomeStatusError
		report.EvalError = "missing issue number or repo"
		return report
	}

	// Check issue state first
	issueData, err := ghAPIGet(ctx, fmt.Sprintf("issues/%d", num), repo)
	if err != nil {
		report.OutcomeStatus = OutcomeStatusError
		report.EvalError = err.Error()
		return report
	}

	state, hasState := issueData["state"].(string)
	if !hasState {
		state = ""
	}
	stateReason, hasStateReason := issueData["state_reason"].(string)
	if !hasStateReason {
		stateReason = ""
	}

	// Search for linked PRs from copilot-swe-agent via timeline events
	events, err := ghAPIGetArray(ctx, fmt.Sprintf("issues/%d/timeline", num), repo)
	var agentPR map[string]any
	if err == nil {
		for _, event := range events {
			eventType, isString := event["event"].(string)
			if !isString || eventType != "cross-referenced" {
				continue
			}
			source, isObject := event["source"].(map[string]any)
			if !isObject || source == nil {
				continue
			}
			issue, isObject := source["issue"].(map[string]any)
			if !isObject || issue == nil {
				continue
			}
			// Check if it's a PR (has pull_request field)
			if _, hasPR := issue["pull_request"]; !hasPR {
				continue
			}
			user, isObject := issue["user"].(map[string]any)
			if !isObject {
				continue
			}
			login, isString := user["login"].(string)
			if isString && (strings.Contains(login, "copilot") || strings.Contains(login, "github-actions")) {
				agentPR = issue
				break
			}
		}
	}

	if agentPR != nil {
		prNumber := 0
		if n, ok := agentPR["number"].(float64); ok {
			prNumber = int(n)
		}
		outcomeEvalAgentLog.Printf("Found agent-linked PR for issue #%d: pr=%d", num, prNumber)

		// Fetch the actual PR to check merge status
		if prNumber > 0 {
			prData, perr := ghAPIGet(ctx, fmt.Sprintf("pulls/%d", prNumber), repo)
			if perr == nil {
				merged, hasMerged := prData["merged"].(bool)
				if !hasMerged {
					merged = false
				}
				prState, hasPRState := prData["state"].(string)
				if !hasPRState {
					prState = ""
				}
				mergedAt, hasMergedAt := prData["merged_at"].(string)
				if !hasMergedAt {
					mergedAt = ""
				}
				closedAt, hasClosedAt := prData["closed_at"].(string)
				if !hasClosedAt {
					closedAt = ""
				}

				switch {
				case merged:
					report.OutcomeStatus = OutcomeStatusAccepted
					report.Detail = fmt.Sprintf("agent PR #%d merged", prNumber)
					if mergedAt != "" && item.Timestamp != "" {
						report.TimeToOutcomeHours = timeBetween(item.Timestamp, mergedAt)
					}
					return report
				case prState == "closed":
					report.OutcomeStatus = OutcomeStatusRejected
					report.Detail = fmt.Sprintf("agent PR #%d closed without merge", prNumber)
					if closedAt != "" && item.Timestamp != "" {
						report.TimeToOutcomeHours = timeBetween(item.Timestamp, closedAt)
					}
					return report
				default:
					report.OutcomeStatus = OutcomeStatusPending
					report.Detail = fmt.Sprintf("agent PR #%d open", prNumber)
					return report
				}
			}
		}
	}

	// No agent PR found — check if issue was resolved by other means
	switch {
	case state == "closed" && stateReason == "completed":
		report.OutcomeStatus = OutcomeStatusAccepted
		report.Detail = "issue resolved (no agent PR found)"
		closedAt, hasClosedAt := issueData["closed_at"].(string)
		if !hasClosedAt {
			closedAt = ""
		}
		if closedAt != "" && item.Timestamp != "" {
			report.TimeToOutcomeHours = timeBetween(item.Timestamp, closedAt)
		}
	case state == "closed":
		report.OutcomeStatus = OutcomeStatusRejected
		report.Detail = "issue closed without resolution, no agent PR"
	default:
		report.OutcomeStatus = OutcomeStatusIgnored
		report.Detail = "no agent PR created"
	}

	return report
}
