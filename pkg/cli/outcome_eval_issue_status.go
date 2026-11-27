package cli

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/github/gh-aw/pkg/repoutil"
)

var outcomeIssueStatusGHAPIGraphQL = ghAPIGraphQL

func evalSetIssueStatus(ctx context.Context, item CreatedItemReport, repoOverride string) OutcomeReport {
	before, beforeOK := nativeIssueStatusState(item.BeforeState, true)
	after, afterOK := nativeIssueStatusState(item.AfterState, false)
	if !beforeOK || !afterOK {
		return OutcomeReport{
			Type: item.Type, ObjectURL: item.URL, ObjectNumber: resolveItemNumber(item),
			Repo: resolveItemRepo(item, repoOverride), Detail: "missing planning status execution state",
			OutcomeEvaluation: outcomeEvidence(OutcomeStatusUnknown, EvidenceNone, "missing_execution_state"),
		}
	}
	item.BeforeState, item.AfterState = before, after
	return evalRetainedUpdate(ctx, item, repoOverride, "issue planning status", loadNativeIssueStatusState, false)
}

func loadNativeIssueStatusState(ctx context.Context, repo string, number int) (map[string]any, bool, error) {
	issue, err := outcomeUpdateGHAPIGet(ctx, fmt.Sprintf("issues/%d", number), repo)
	if err != nil {
		return nil, false, err
	}
	snapshot := map[string]any{
		"planning_status": issue["status"],
		"state":           issue["state"],
		"state_reason":    issue["state_reason"],
	}
	if status, ok := issue["status"].(map[string]any); ok && status["category"] == "duplicate" {
		id, err := loadNativeIssueDuplicate(ctx, repo, number)
		if err != nil {
			return nil, false, err
		}
		snapshot["duplicate_issue_id"] = id
	}
	state, ok := nativeIssueStatusState(snapshot, false)
	if !ok {
		return nil, false, errors.New("native issue planning status is unavailable or malformed")
	}
	return state, false, nil
}

func loadNativeIssueDuplicate(ctx context.Context, repo string, number int) (string, error) {
	slug, _ := repoutil.NormalizeRepoForAPI(repo)
	owner, name, _ := strings.Cut(slug, "/")
	result, err := outcomeIssueStatusGHAPIGraphQL(ctx,
		`query($owner: String!, $name: String!, $number: Int!) {
			repository(owner: $owner, name: $name) {
				issue(number: $number) { duplicateOf { fullDatabaseId } }
			}
		}`, map[string]any{"owner": owner, "name": name, "number": number}, repo)
	if err != nil {
		return "", err
	}
	data := outcomeValue[map[string]any](result["data"])
	repository := outcomeValue[map[string]any](data["repository"])
	issue := outcomeValue[map[string]any](repository["issue"])
	duplicate := outcomeValue[map[string]any](issue["duplicateOf"])
	id, ok := nativeStatusID(duplicate["fullDatabaseId"])
	if !ok {
		return "", errors.New("native duplicate issue relationship is unavailable")
	}
	return id, nil
}

// Compare stable IDs and categories, not mutable display names.
func nativeIssueStatusState(snapshot map[string]any, allowAbsent bool) (map[string]any, bool) {
	raw, present := snapshot["planning_status"]
	state := mutableString(snapshot["state"])
	if !present || state != "open" && state != "closed" {
		return nil, false
	}
	result := map[string]any{"state": state, "state_reason": "", "planning_status_id": "", "planning_status_category": "", "duplicate_issue_id": ""}
	if state == "closed" {
		result["state_reason"] = mutableString(snapshot["state_reason"])
	}
	if raw == nil {
		return result, allowAbsent
	}
	status, ok := raw.(map[string]any)
	if !ok {
		return nil, false
	}
	id, ok := nativeStatusID(status["id"])
	if !ok {
		return nil, false
	}
	category := mutableString(status["category"])
	switch category {
	case "backlog", "planned", "started":
		if state != "open" {
			return nil, false
		}
	case "completed", "not_planned", "duplicate":
		if state != "closed" || mutableString(snapshot["state_reason"]) != category {
			return nil, false
		}
		result["state_reason"] = category
	default:
		return nil, false
	}
	if category == "duplicate" {
		duplicateID, ok := nativeStatusID(snapshot["duplicate_issue_id"])
		if !ok {
			return nil, false
		}
		result["duplicate_issue_id"] = duplicateID
	}
	result["planning_status_id"], result["planning_status_category"] = id, category
	return result, true
}

func nativeStatusID(raw any) (string, bool) {
	value := mutableString(raw)
	if number, ok := raw.(float64); ok {
		const maxSafeInteger = 9007199254740991
		if math.IsNaN(number) || math.IsInf(number, 0) || math.Trunc(number) != number || number <= 0 || number > maxSafeInteger {
			return "", false
		}
		value = strconv.FormatFloat(number, 'f', 0, 64)
	}
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id <= 0 || id > 9007199254740991 {
		return "", false
	}
	return strconv.FormatInt(id, 10), true
}
