package cli

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/github/gh-aw/pkg/console"
)

func renderConsoleWorkQueue(report *WorkQueueReport) {
	if err := renderWorkQueueToWriter(os.Stderr, report); err != nil {
		console.PrintErrorMessage("Cannot render work queue diagnostics: " + err.Error())
	}
}

func renderWorkQueueToWriter(w io.Writer, report *WorkQueueReport) error {
	if report == nil {
		return nil
	}
	var output strings.Builder
	output.WriteString("  work_queue:\n")
	if snapshot := report.Snapshot; snapshot != nil {
		if current := snapshot.Current; current != nil {
			output.WriteString(fmt.Sprintf("    snapshot: protocol=3 commits=%d (captured ledger, not live authority)\n", current.CommitCount))
			if current.Role == "observer" {
				output.WriteString("    role: observer (read-only diagnostics; no worker or publisher authority)\n")
				if !current.PolicyInstalled {
					output.WriteString("    policy: absent (no queue initialized)\n")
				}
			}
			output.WriteString(fmt.Sprintf("    queue_tip: %s policy_epoch=%s\n", escapeWorkQueueDisplay(current.Tip), escapeWorkQueueDisplay(current.PolicyEpoch)))
			output.WriteString(fmt.Sprintf("    control: admission_paused=%t grants_paused=%t (not preemption)\n", current.Paused, current.GrantsPaused))
			if assignment := current.Assignment; assignment != nil {
				output.WriteString(fmt.Sprintf("    assignment: %s request=%s commit=%s native=%s released=%t\n",
					escapeWorkQueueDisplay(assignment.DispatchID), escapeWorkQueueDisplay(assignment.RequestID),
					escapeWorkQueueDisplay(assignment.CommitID), escapeWorkQueueDisplay(assignment.State), assignment.Released))
				for _, claim := range assignment.Claims[:min(len(assignment.Claims), 16)] {
					output.WriteString(fmt.Sprintf("    claim: %s work=%s durable=%s delivery=%s\n", escapeWorkQueueDisplay(claim.Handle),
						escapeWorkQueueDisplay(claim.WorkID), escapeWorkQueueDisplay(claim.State), escapeWorkQueueDisplay(claim.Barrier)))
				}
				output.WriteString("    delivery: verified means a ledger Result attestation, not a live API recheck\n")
			}
		} else {
			output.WriteString(fmt.Sprintf("    snapshot: %d transactions (historical read-only diagnostic)\n", len(snapshot.Transactions)))
		}
		if snapshot.SHA != nil {
			output.WriteString(fmt.Sprintf("    snapshot_sha: %s\n", escapeWorkQueueDisplay(*snapshot.SHA)))
		}
		if snapshot.Worker != nil {
			output.WriteString(fmt.Sprintf("    worker: work=%s claim=%s\n", escapeWorkQueueDisplay(snapshot.Worker.WorkID), escapeWorkQueueDisplay(snapshot.Worker.ClaimID)))
		}
	}
	if report.FinishIntent != "" {
		output.WriteString(fmt.Sprintf("    finish_intent: %s (requested, not a verified outcome)\n", escapeWorkQueueDisplay(report.FinishIntent)))
	}
	for _, intent := range report.FinishIntents[:min(len(report.FinishIntents), 256)] {
		output.WriteString(fmt.Sprintf("    finish_intent: claim=%s outcome=%s (staged, not durable or verified)\n",
			escapeWorkQueueDisplay(intent.Handle), escapeWorkQueueDisplay(intent.Outcome)))
	}
	if len(report.Operations) > 0 {
		output.WriteString("    operation_logs: diagnostics only, not independent delivery evidence\n")
	}
	for _, operation := range report.Operations[:min(len(report.Operations), 256)] {
		output.WriteString(fmt.Sprintf("    %s: %s\n", escapeWorkQueueDisplay(operation.Timestamp), escapeWorkQueueDisplay(operation.Message)))
	}
	_, err := io.WriteString(w, output.String())
	return err
}

func renderLogsWorkQueueToWriter(w io.Writer, runs []RunData) error {
	for _, run := range runs {
		if run.WorkQueue == nil {
			continue
		}
		if _, err := fmt.Fprintf(w, "[work-queue] run=%d workflow=%s\n", run.RunID, escapeWorkQueueDisplay(run.WorkflowName)); err != nil {
			return err
		}
		if err := renderWorkQueueToWriter(w, run.WorkQueue); err != nil {
			return err
		}
	}
	return nil
}

func escapeWorkQueueDisplay(value string) string {
	return strings.TrimSuffix(strings.TrimPrefix(strconv.QuoteToGraphic(boundWorkQueueDisplay(value, 512)), `"`), `"`)
}

func workQueueReportRuns(runs []RunData, include bool) []RunData {
	if include {
		return runs
	}
	projected := make([]RunData, 0, len(runs))
	for _, run := range runs {
		run.WorkQueue = nil
		projected = append(projected, run)
	}
	return projected
}
