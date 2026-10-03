package cli

import (
	"fmt"
	"io"
	"os"
)

func renderConsoleDispatchCoordinator(report *DispatchCoordinatorReport) {
	renderDispatchCoordinatorToWriter(os.Stderr, report)
}

func renderDispatchCoordinatorToWriter(w io.Writer, report *DispatchCoordinatorReport) {
	if report == nil {
		return
	}
	fmt.Fprintln(w, "  work_queue:")
	if snapshot := report.Snapshot; snapshot != nil {
		fmt.Fprintf(w, "    snapshot: %d transactions\n", len(snapshot.Transactions))
		if snapshot.SHA != nil {
			fmt.Fprintf(w, "    snapshot_sha: %s\n", *snapshot.SHA)
		}
		if snapshot.Worker != nil {
			fmt.Fprintf(w, "    worker: work=%s claim=%s\n", snapshot.Worker.WorkID, snapshot.Worker.ClaimID)
		}
	}
	if report.FinishIntent != "" {
		fmt.Fprintf(w, "    finish_intent: %s (requested, not a verified outcome)\n", report.FinishIntent)
	}
	for _, operation := range report.Operations {
		fmt.Fprintf(w, "    %s: %s\n", operation.Timestamp, operation.Message)
	}
}

func renderLogsDispatchCoordinatorToWriter(w io.Writer, runs []RunData) {
	for _, run := range runs {
		if run.DispatchCoordinator == nil {
			continue
		}
		fmt.Fprintf(w, "[work-queue] run=%d workflow=%s\n", run.RunID, run.WorkflowName)
		renderDispatchCoordinatorToWriter(w, run.DispatchCoordinator)
	}
}
