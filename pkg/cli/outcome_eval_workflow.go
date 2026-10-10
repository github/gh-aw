package cli

import (
	"context"
	"fmt"
	"math"

	"github.com/github/gh-aw/pkg/logger"
)

// maxSafeFloat64Int is the largest integer that can be represented exactly as
// float64 (2^53). GitHub run IDs decoded from JSON arrive as float64; any value
// beyond this threshold cannot be round-tripped to int64 without precision loss.
const maxSafeFloat64Int = 1 << 53

var outcomeEvalWorkflowLog = logger.New("cli:outcome_eval_workflow")
var workflowOutcomeGHAPIGet = ghAPIGet

// evalDispatchWorkflow checks whether a dispatched workflow run completed successfully.
// It looks for a run_id in the item metadata and queries the workflow run status.
// Spec: specs/safe-output-outcome-evaluation.md §20
func evalDispatchWorkflow(ctx context.Context, item CreatedItemReport, repoOverride string) OutcomeReport {
	repo := resolveItemRepo(item, repoOverride)
	outcomeEvalWorkflowLog.Printf("Evaluating dispatch_workflow: repo=%s, url=%s", repo, item.URL)

	report := OutcomeReport{
		Type:      item.Type,
		ObjectURL: item.URL,
		Repo:      repo,
	}

	// Extract run_id from metadata if available.
	// JSON numbers unmarshal as float64; convert carefully to int64 to avoid
	// precision loss for large GitHub run IDs (which can exceed 2^32).
	var runID int64
	if item.Metadata != nil {
		if v, ok := item.Metadata["run_id"]; ok {
			switch id := v.(type) {
			case float64:
				// Guard against float64 values that cannot round-trip to int64.
				// math.MaxInt64 is not representable exactly as float64; use 2^53
				// (maxSafeFloat64Int) as the safe upper bound for lossless conversion.
				if id > 0 && id <= maxSafeFloat64Int && id == math.Trunc(id) {
					runID = int64(id)
				}
			case int:
				runID = int64(id)
			case int64:
				runID = id
			}
		}
	}

	if runID <= 0 {
		// No run ID available — workflow may not have been dispatched or ID not captured
		report.OutcomeStatus = OutcomeStatusPending
		report.Detail = "no run ID available; dispatch may still be queued"
		report.OutcomeEvaluation = outcomeEvidence(OutcomeStatusPending, EvidenceNone, "missing_run_id")
		return report
	}

	data, err := workflowOutcomeGHAPIGet(ctx, fmt.Sprintf("actions/runs/%d", runID), repo)
	if err != nil {
		report.OutcomeStatus = OutcomeStatusError
		report.EvalError = err.Error()
		return report
	}

	status, _ := data["status"].(string)
	conclusion, _ := data["conclusion"].(string)
	outcomeEvalWorkflowLog.Printf("dispatch_workflow run %d: status=%s, conclusion=%s", runID, status, conclusion)

	switch {
	case status == "completed" && conclusion == "success":
		report.OutcomeStatus = OutcomeStatusAccepted
		report.Detail = "workflow run completed with success"
		report.OutcomeEvaluation = outcomeEvidence(OutcomeStatusAccepted, EvidenceStrong, "workflow_success")
	case status == "completed" && (conclusion == "failure" || conclusion == "timed_out" || conclusion == "cancelled" || conclusion == "action_required"):
		// action_required means the run is blocked and requires manual intervention;
		// treat it as rejected rather than ignored since it does not self-resolve.
		report.OutcomeStatus = OutcomeStatusRejected
		report.Detail = "workflow run completed with " + conclusion
		report.OutcomeEvaluation = outcomeEvidence(OutcomeStatusRejected, EvidenceStrong, "workflow_failed")
	case status == "completed":
		// neutral and skipped indicate the run did not contribute meaningful output.
		report.OutcomeStatus = OutcomeStatusIgnored
		report.Detail = "workflow run completed with " + conclusion
		report.OutcomeEvaluation = outcomeEvidence(OutcomeStatusIgnored, EvidenceMedium, "workflow_no_effect")
	default:
		report.OutcomeStatus = OutcomeStatusPending
		report.Detail = "workflow run status: " + status
		report.OutcomeEvaluation = outcomeEvidence(OutcomeStatusPending, EvidenceMedium, "workflow_pending")
	}
	return report
}

// evalUpdateDiscussion checks whether a discussion edit stuck.
// Full evaluation requires GraphQL (same pattern as evalCloseDiscussion).
// Until implemented, absence of an evaluator is unknown, not lack of engagement.
// Spec: specs/safe-output-outcome-evaluation.md §12
func evalUpdateDiscussion(ctx context.Context, item CreatedItemReport, repoOverride string) OutcomeReport {
	return unsupportedOutcome(item, repoOverride)
}
