package cli

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

func renderConsoleWorkQueue(report *WorkQueueReport) {
	renderWorkQueueToWriter(os.Stderr, report)
}

func renderWorkQueueToWriter(w io.Writer, report *WorkQueueReport) {
	if report == nil {
		return
	}
	fmt.Fprintln(w, "  work_queue:")
	if snapshot := report.Snapshot; snapshot != nil {
		if current := snapshot.Current; current != nil {
			fmt.Fprintf(w, "    snapshot: protocol=3 commits=%d (captured ledger, not live authority)\n", current.CommitCount)
			if current.Role == "observer" {
				fmt.Fprintln(w, "    role: observer (read-only diagnostics; no worker or publisher authority)")
				if !current.PolicyInstalled {
					fmt.Fprintln(w, "    policy: absent (no queue initialized)")
				}
			}
			fmt.Fprintf(w, "    queue_tip: %s policy_epoch=%s\n", escapeWorkQueueDisplay(current.Tip), escapeWorkQueueDisplay(current.PolicyEpoch))
			fmt.Fprintf(w, "    control: admission_paused=%t grants_paused=%t (not preemption)\n", current.Paused, current.GrantsPaused)
			if assignment := current.Assignment; assignment != nil {
				fmt.Fprintf(w, "    assignment: %s request=%s commit=%s native=%s released=%t\n",
					escapeWorkQueueDisplay(assignment.DispatchID), escapeWorkQueueDisplay(assignment.RequestID),
					escapeWorkQueueDisplay(assignment.CommitID), escapeWorkQueueDisplay(assignment.State), assignment.Released)
				for _, claim := range assignment.Claims[:min(len(assignment.Claims), 16)] {
					fmt.Fprintf(w, "    claim: %s work=%s durable=%s delivery=%s\n", escapeWorkQueueDisplay(claim.Handle),
						escapeWorkQueueDisplay(claim.WorkID), escapeWorkQueueDisplay(claim.State), escapeWorkQueueDisplay(claim.Barrier))
				}
				fmt.Fprintln(w, "    delivery: verified means a ledger Result attestation, not a live API recheck")
			}
		} else {
			fmt.Fprintf(w, "    snapshot: %d transactions (historical read-only diagnostic)\n", len(snapshot.Transactions))
		}
		if snapshot.SHA != nil {
			fmt.Fprintf(w, "    snapshot_sha: %s\n", escapeWorkQueueDisplay(*snapshot.SHA))
		}
		if snapshot.Worker != nil {
			fmt.Fprintf(w, "    worker: work=%s claim=%s\n", escapeWorkQueueDisplay(snapshot.Worker.WorkID), escapeWorkQueueDisplay(snapshot.Worker.ClaimID))
		}
	}
	if report.FinishIntent != "" {
		fmt.Fprintf(w, "    finish_intent: %s (requested, not a verified outcome)\n", escapeWorkQueueDisplay(report.FinishIntent))
	}
	for _, intent := range report.FinishIntents[:min(len(report.FinishIntents), 256)] {
		fmt.Fprintf(w, "    finish_intent: claim=%s outcome=%s (staged, not durable or verified)\n",
			escapeWorkQueueDisplay(intent.Handle), escapeWorkQueueDisplay(intent.Outcome))
	}
	if len(report.Operations) > 0 {
		fmt.Fprintln(w, "    operation_logs: diagnostics only, not independent delivery evidence")
	}
	for _, operation := range report.Operations[:min(len(report.Operations), 256)] {
		fmt.Fprintf(w, "    %s: %s\n", escapeWorkQueueDisplay(operation.Timestamp), escapeWorkQueueDisplay(operation.Message))
	}
}

func renderLogsWorkQueueToWriter(w io.Writer, runs []RunData) {
	for _, run := range runs {
		if run.WorkQueue == nil {
			continue
		}
		fmt.Fprintf(w, "[work-queue] run=%d workflow=%s\n", run.RunID, escapeWorkQueueDisplay(run.WorkflowName))
		renderWorkQueueToWriter(w, run.WorkQueue)
	}
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
