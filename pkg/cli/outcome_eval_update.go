package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var outcomeEvalUpdateLog = logger.New("cli:outcome_eval_update")

var outcomeUpdateGHAPIGet = ghAPIGet

func evalUpdateIssue(ctx context.Context, item CreatedItemReport, repoOverride string) OutcomeReport {
	return evalRetainedUpdate(ctx, item, repoOverride, "issue", extractCurrentIssueUpdateState, false)
}

func evalUpdatePullRequest(ctx context.Context, item CreatedItemReport, repoOverride string) OutcomeReport {
	return evalRetainedUpdate(ctx, item, repoOverride, "pull request", extractCurrentPullRequestUpdateState, true)
}

type mutableStateLoader func(ctx context.Context, repo string, number int) (map[string]any, bool, error)

func evalRetainedUpdate(ctx context.Context, item CreatedItemReport, repoOverride string, objectKind string, load mutableStateLoader, strongOnMerge bool) OutcomeReport {
	repo := resolveItemRepo(item, repoOverride)
	num := resolveItemNumber(item)
	outcomeEvalUpdateLog.Printf("Evaluating retained update: kind=%s, type=%s, repo=%s, num=%d", objectKind, item.Type, repo, num)
	report := OutcomeReport{
		Type:         item.Type,
		ObjectURL:    item.URL,
		ObjectNumber: num,
		Repo:         repo,
	}
	if num == 0 || repo == "" {
		outcomeEvalUpdateLog.Printf("Missing execution state: num=%d, repo=%s", num, repo)
		report.Detail = "missing execution state"
		report.OutcomeEvaluation = outcomeEvidence(OutcomeStatusUnknown, EvidenceNone, "missing_execution_state")
		return report
	}
	if item.BeforeState == nil || item.AfterState == nil {
		report.Detail = "missing execution state"
		report.OutcomeEvaluation = outcomeEvidence(OutcomeStatusUnknown, EvidenceNone, "missing_execution_state")
		return report
	}

	currentState, merged, err := load(ctx, repo, num)
	if err != nil {
		report.OutcomeStatus = OutcomeStatusError
		report.EvalError = err.Error()
		return report
	}

	comparison := compareRetainedUpdateState(item.BeforeState, item.AfterState, currentState, mutableTrackedFields(item.Type))
	outcomeEvalUpdateLog.Printf("State comparison for %s #%d: changed=%d, retained=%d, reverted=%d, replaced=%d, merged=%v",
		objectKind, num, len(comparison.Changed), len(comparison.Retained), len(comparison.Reverted), len(comparison.Replaced), merged)
	if len(comparison.Changed) == 0 {
		report.Detail = "no persisted state delta"
		report.OutcomeEvaluation = outcomeEvidence(OutcomeStatusUnknown, EvidenceNone, "no_state_delta")
		return report
	}

	switch {
	case len(comparison.Retained) == len(comparison.Changed):
		report.OutcomeStatus = OutcomeStatusAccepted
		if strongOnMerge && merged {
			report.Detail = objectKind + " update retained and merged"
			report.OutcomeEvaluation = outcomeEvidence(OutcomeStatusAccepted, EvidenceStrong, "state_retained_and_merged")
			return report
		}
		report.Detail = objectKind + " update retained"
		report.OutcomeEvaluation = outcomeEvidence(OutcomeStatusAccepted, EvidenceMedium, "state_retained")
		return report
	case len(comparison.Reverted) == len(comparison.Changed):
		report.Detail = objectKind + " update reverted"
		report.OutcomeEvaluation = outcomeEvidence(OutcomeStatusRejected, EvidenceStrong, "state_reverted")
		return report
	default:
		report.Detail = objectKind + " update replaced"
		report.OutcomeEvaluation = outcomeEvidence(OutcomeStatusRejected, EvidenceStrong, "state_replaced")
		return report
	}
}

type retainedStateComparison struct {
	Changed  []string
	Retained []string
	Reverted []string
	Replaced []string
}

func compareRetainedUpdateState(beforeState map[string]any, afterState map[string]any, currentState map[string]any, fields []string) retainedStateComparison {
	var out retainedStateComparison
	for _, field := range fields {
		afterValue, ok := afterState[field]
		if !ok {
			continue
		}
		beforeValue := beforeState[field]
		if mutableStateEqual(field, beforeValue, afterValue) {
			continue
		}
		out.Changed = append(out.Changed, field)
		currentValue := currentState[field]
		switch {
		case mutableStateEqual(field, currentValue, afterValue):
			out.Retained = append(out.Retained, field)
		case mutableStateEqual(field, currentValue, beforeValue):
			out.Reverted = append(out.Reverted, field)
		default:
			out.Replaced = append(out.Replaced, field)
		}
	}
	return out
}

func mutableTrackedFields(itemType string) []string {
	switch itemType {
	case "update_issue":
		return []string{"title", "body_hash", "state", "labels", "assignees"}
	case "update_pull_request":
		return []string{"title", "body_hash", "state", "base", "draft", "head_sha"}
	case "replace_label":
		return []string{"labels"}
	default:
		return nil
	}
}

func mutableStateEqual(field string, left any, right any) bool {
	switch field {
	case "labels", "assignees":
		return slices.Equal(mutableStringSlice(left), mutableStringSlice(right))
	case "draft":
		return mutableBool(left) == mutableBool(right)
	default:
		return strings.TrimSpace(mutableString(left)) == strings.TrimSpace(mutableString(right))
	}
}

func mutableString(raw any) string {
	switch value := raw.(type) {
	case string:
		return value
	case fmt.Stringer:
		return value.String()
	case bool:
		if value {
			return "true"
		}
		return "false"
	case nil:
		return ""
	default:
		return strings.TrimSpace(fmt.Sprint(value))
	}
}

func mutableBool(raw any) bool {
	value, ok := raw.(bool)
	return ok && value
}

func mutableStringSlice(raw any) []string {
	switch values := raw.(type) {
	case []string:
		out := slices.Clone(values)
		for i := range out {
			out[i] = strings.TrimSpace(out[i])
		}
		slices.Sort(out)
		return out
	case []any:
		out := make([]string, 0, len(values))
		for _, value := range values {
			s := strings.TrimSpace(fmt.Sprint(value))
			if s != "" {
				out = append(out, s)
			}
		}
		slices.Sort(out)
		return out
	default:
		return nil
	}
}

func extractCurrentIssueUpdateState(ctx context.Context, repo string, number int) (map[string]any, bool, error) {
	issue, err := outcomeUpdateGHAPIGet(ctx, fmt.Sprintf("issues/%d", number), repo)
	if err != nil {
		return nil, false, err
	}
	return map[string]any{
		"title":     mutableString(issue["title"]),
		"body_hash": mutableBodyHash(issue["body"]),
		"state":     mutableString(issue["state"]),
		"labels":    extractNamedItems(issue["labels"], "name"),
		"assignees": extractNamedItems(issue["assignees"], "login"),
	}, false, nil
}

func extractCurrentPullRequestUpdateState(ctx context.Context, repo string, number int) (map[string]any, bool, error) {
	pullRequest, err := outcomeUpdateGHAPIGet(ctx, fmt.Sprintf("pulls/%d", number), repo)
	if err != nil {
		return nil, false, err
	}
	base := outcomeValue[map[string]any](pullRequest["base"])
	head := outcomeValue[map[string]any](pullRequest["head"])
	merged := outcomeValue[bool](pullRequest["merged"])
	return map[string]any{
		"title":     mutableString(pullRequest["title"]),
		"body_hash": mutableBodyHash(pullRequest["body"]),
		"state":     mutableString(pullRequest["state"]),
		"base":      mutableString(base["ref"]),
		"draft":     mutableBool(pullRequest["draft"]),
		"head_sha":  mutableString(head["sha"]),
	}, merged, nil
}

func extractNamedItems(raw any, key string) []string {
	items := outcomeValue[[]any](raw)
	out := make([]string, 0, len(items))
	for _, item := range items {
		if value := outcomeNestedString(item, key); value != "" {
			out = append(out, value)
		}
	}
	slices.Sort(out)
	return out
}

func mutableBodyHash(raw any) string {
	sum := sha256.Sum256([]byte(normalizeMutableBody(raw)))
	return hex.EncodeToString(sum[:])
}

func normalizeMutableBody(raw any) string {
	body := strings.ReplaceAll(mutableString(raw), "\r\n", "\n")
	lines := strings.Split(body, "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " \t")
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}
