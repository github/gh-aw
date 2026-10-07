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
			fmt.Fprintf(&output, "    snapshot: protocol=3 commits=%d (captured ledger, not live authority)\n", current.CommitCount) //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
			if current.Role == "observer" {
				output.WriteString("    role: observer (read-only diagnostics; no worker or publisher authority)\n")
				if !current.PolicyInstalled {
					output.WriteString("    policy: absent (no queue initialized)\n")
				}
			}
			fmt.Fprintf(&output, "    queue_tip: %s policy_epoch=%s\n", escapeWorkQueueDisplay(current.Tip), escapeWorkQueueDisplay(current.PolicyEpoch)) //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
			fmt.Fprintf(&output, "    control: admission_paused=%t grants_paused=%t (not preemption)\n", current.Paused, current.GrantsPaused)            //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
			if assignment := current.Assignment; assignment != nil {
				fmt.Fprintf(&output, "    assignment: %s request=%s commit=%s native=%s released=%t\n", //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
					escapeWorkQueueDisplay(assignment.DispatchID), escapeWorkQueueDisplay(assignment.RequestID),
					escapeWorkQueueDisplay(assignment.CommitID), escapeWorkQueueDisplay(assignment.State), assignment.Released)
				for _, claim := range assignment.Claims[:min(len(assignment.Claims), 16)] {
					fmt.Fprintf(&output, "    claim: %s work=%s durable=%s delivery=%s\n", escapeWorkQueueDisplay(claim.Handle), //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
						escapeWorkQueueDisplay(claim.WorkID), escapeWorkQueueDisplay(claim.State), escapeWorkQueueDisplay(claim.Barrier))
				}
				output.WriteString("    delivery: verified means a ledger Result attestation, not a live API recheck\n")
			}
		} else {
			fmt.Fprintf(&output, "    snapshot: %d transactions (historical read-only diagnostic)\n", len(snapshot.Transactions)) //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
		}
		if snapshot.SHA != nil {
			fmt.Fprintf(&output, "    snapshot_sha: %s\n", escapeWorkQueueDisplay(*snapshot.SHA)) //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
		}
		if snapshot.Worker != nil {
			fmt.Fprintf(&output, "    worker: work=%s claim=%s\n", escapeWorkQueueDisplay(snapshot.Worker.WorkID), escapeWorkQueueDisplay(snapshot.Worker.ClaimID)) //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
		}
	}
	if report.FinishIntent != "" {
		fmt.Fprintf(&output, "    finish_intent: %s (requested, not a verified outcome)\n", escapeWorkQueueDisplay(report.FinishIntent)) //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
	}
	for _, intent := range report.FinishIntents[:min(len(report.FinishIntents), 256)] {
		fmt.Fprintf(&output, "    finish_intent: claim=%s outcome=%s (staged, not durable or verified)\n", //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
			escapeWorkQueueDisplay(intent.Handle), escapeWorkQueueDisplay(intent.Outcome))
	}
	if len(report.Operations) > 0 {
		output.WriteString("    operation_logs: diagnostics only, not independent delivery evidence\n")
	}
	for _, operation := range report.Operations[:min(len(report.Operations), 256)] {
		fmt.Fprintf(&output, "    %s: %s\n", escapeWorkQueueDisplay(operation.Timestamp), escapeWorkQueueDisplay(operation.Message)) //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
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
