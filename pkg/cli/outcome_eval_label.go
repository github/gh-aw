package cli

import (
	"context"
	"fmt"
	"slices"

	"github.com/github/gh-aw/pkg/logger"
)

var outcomeEvalLabelLog = logger.New("cli:outcome_eval_label")

// evalReplaceLabel checks whether the label delta applied at execution time is
// still intact in the current issue state.  It computes the set of labels added
// and removed by the replacement and verifies only that delta, ignoring any
// unrelated labels that may have been added or removed since execution.
func evalReplaceLabel(ctx context.Context, item CreatedItemReport, repoOverride string) OutcomeReport {
	repo := resolveItemRepo(item, repoOverride)
	num := resolveItemNumber(item)
	outcomeEvalLabelLog.Printf("Evaluating replace_label outcome: repo=%s, number=%d", repo, num)
	report := OutcomeReport{
		Type:         item.Type,
		ObjectURL:    item.URL,
		ObjectNumber: num,
		Repo:         repo,
	}
	if num == 0 || repo == "" || item.BeforeState == nil || item.AfterState == nil {
		report.Detail = "missing execution state"
		report.OutcomeEvaluation = outcomeEvidence(OutcomeStatusUnknown, EvidenceNone, "missing_execution_state")
		return report
	}

	beforeLabels := mutableStringSlice(item.BeforeState["labels"])
	afterLabels := mutableStringSlice(item.AfterState["labels"])

	// Compute the replacement delta: labels added and labels removed.
	added := labelSetDiff(afterLabels, beforeLabels)
	removed := labelSetDiff(beforeLabels, afterLabels)

	if len(added) == 0 && len(removed) == 0 {
		report.Detail = "no label delta"
		report.OutcomeEvaluation = outcomeEvidence(OutcomeStatusUnknown, EvidenceNone, "no_state_delta")
		return report
	}

	currentState, _, err := extractCurrentIssueUpdateState(ctx, repo, num)
	if err != nil {
		report.OutcomeStatus = OutcomeStatusError
		report.EvalError = err.Error()
		return report
	}
	currentLabels := mutableStringSlice(currentState["labels"])

	// The replacement is retained when all added labels are still present and
	// all removed labels are still absent, regardless of any other label changes.
	addedRetained := labelSetContainsAll(currentLabels, added)
	removedStillAbsent := !labelSetContainsAny(currentLabels, removed)

	if addedRetained && removedStillAbsent {
		report.Detail = "label replacement retained"
		report.OutcomeEvaluation = outcomeEvidence(OutcomeStatusAccepted, EvidenceMedium, "state_retained")
		return report
	}

	// Reverted: all added labels are gone and all removed labels are back.
	addedReverted := !labelSetContainsAny(currentLabels, added)
	removedBack := labelSetContainsAll(currentLabels, removed)
	if addedReverted && removedBack {
		report.Detail = "label replacement reverted"
		report.OutcomeEvaluation = outcomeEvidence(OutcomeStatusRejected, EvidenceStrong, "state_reverted")
		return report
	}
	report.Detail = "label replacement replaced"

	report.OutcomeEvaluation = outcomeEvidence(OutcomeStatusRejected, EvidenceStrong, "state_replaced")
	return report
}

// labelSetDiff returns the elements of a that are not in b.
// Both slices must be sorted (as produced by mutableStringSlice).
// Uses binary search for O(n log m) performance.
func labelSetDiff(a, b []string) []string {
	var out []string
	for _, v := range a {
		if _, found := slices.BinarySearch(b, v); !found {
			out = append(out, v)
		}
	}
	return out
}

// labelSetContainsAll reports whether current contains every element of want.
// Both slices must be sorted. Uses binary search for O(n log m) performance.
func labelSetContainsAll(current, want []string) bool {
	for _, v := range want {
		if _, found := slices.BinarySearch(current, v); !found {
			return false
		}
	}
	return true
}

// labelSetContainsAny reports whether current contains at least one element of want.
// Both slices must be sorted. Uses binary search for O(n log m) performance.
func labelSetContainsAny(current, want []string) bool {
	for _, v := range want {
		if _, found := slices.BinarySearch(current, v); found {
			return true
		}
	}
	return false
}

// evalAddLabels checks whether labels added by the workflow are still present.
func evalAddLabels(ctx context.Context, item CreatedItemReport, repoOverride string) OutcomeReport {
	repo := resolveItemRepo(item, repoOverride)
	num := resolveItemNumber(item)
	outcomeEvalLabelLog.Printf("Evaluating add_labels outcome: repo=%s, number=%d", repo, num)
	report := OutcomeReport{
		Type:         item.Type,
		ObjectURL:    item.URL,
		ObjectNumber: num,
		Repo:         repo,
	}
	if num == 0 || repo == "" {
		outcomeEvalLabelLog.Print("Missing issue number or repo, returning error outcome")
		report.OutcomeStatus = OutcomeStatusError
		report.EvalError = "missing issue number or repo"
		return report
	}

	before := item.LabelsBefore
	if item.BeforeState != nil {
		before = mutableStringSlice(item.BeforeState["labels"])
	}
	added := item.LabelsAdded
	if len(added) == 0 {
		for _, label := range item.Labels {
			added = append(added, label.Name)
		}
	}
	if before == nil || len(added) == 0 {
		report.OutcomeEvaluation = outcomeEvidence(OutcomeStatusUnknown, EvidenceNone, "missing_execution_state")
		report.Detail = "missing execution state"
		return report
	}
	delta := labelSetDiff(mutableStringSlice(added), mutableStringSlice(before))
	if len(delta) == 0 {
		report.OutcomeEvaluation = outcomeEvidence(OutcomeStatusUnknown, EvidenceNone, "no_state_delta")
		report.Detail = "no persisted state delta"
		return report
	}
	labels, err := outcomeEvidenceGHAPIGetArray(ctx, fmt.Sprintf("issues/%d/labels", num), repo)
	if err != nil {
		outcomeEvalLabelLog.Printf("Failed to fetch labels for %s#%d: %v", repo, num, err)
		report.OutcomeStatus = OutcomeStatusError
		report.EvalError = err.Error()
		return report
	}

	current := make([]string, 0, len(labels))
	for _, label := range labels {
		current = append(current, outcomeString(label["name"]))
	}
	if labelSetContainsAll(mutableStringSlice(current), delta) {
		report.OutcomeEvaluation = outcomeEvidence(OutcomeStatusAccepted, EvidenceMedium, "state_retained")
		report.Detail = "label addition retained"
	} else {
		report.OutcomeEvaluation = outcomeEvidence(OutcomeStatusRejected, EvidenceStrong, "state_replaced")
		report.Detail = "added labels removed"
	}

	outcomeEvalLabelLog.Printf("Label evaluation result: result=%s, label_count=%d", report.OutcomeStatus, len(labels))
	return report
}
