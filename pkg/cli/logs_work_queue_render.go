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
			fmt.Fprintf(&output, "    snapshot: protocol=%d role=%s commits=%d (captured ledger, not live state)\n", snapshot.Version, escapeWorkQueueDisplay(current.Role), current.CommitCount) //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
			fmt.Fprintf(&output, "    queue_tip: %s\n", escapeWorkQueueDisplay(current.Tip))                                                                                                      //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
			output.WriteString(workQueueDiagnosticsText(current.Diagnostics))
			if assignment := current.Assignment; assignment != nil {
				run := "none"
				if assignment.Run != nil {
					run = fmt.Sprintf("%s/%d", assignment.Run.RunID, assignment.Run.RunAttempt)
				}
				fmt.Fprintf(&output, "    assignment: %s dispatch=%s run=%s\n", //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
					escapeWorkQueueDisplay(assignment.DispatchID), escapeWorkQueueDisplay(assignment.State), escapeWorkQueueDisplay(run))
				for _, claim := range assignment.Claims[:min(len(assignment.Claims), 16)] {
					fmt.Fprintf(&output, "    claim: %s work=%s state=%s\n", escapeWorkQueueDisplay(claim.Handle), //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
						escapeWorkQueueDisplay(claim.WorkID), escapeWorkQueueDisplay(claim.State))
				}
			}
		} else {
			fmt.Fprintf(&output, "    snapshot: protocol=%d (unsupported; not replayed)\n", snapshot.Version) //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
		}
		if snapshot.SHA != nil {
			fmt.Fprintf(&output, "    snapshot_sha: %s\n", escapeWorkQueueDisplay(*snapshot.SHA)) //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
		}
	}
	for _, intent := range report.FinishIntents[:min(len(report.FinishIntents), 256)] {
		fmt.Fprintf(&output, "    finish_intent: claim=%s outcome=%s (staged, not the committed outcome)\n", //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
			escapeWorkQueueDisplay(intent.Handle), escapeWorkQueueDisplay(intent.Outcome))
	}
	if len(report.Operations) > 0 {
		output.WriteString("    operation_logs: diagnostics only\n")
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
