package cli

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
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
			fmt.Fprintf(w, "    snapshot_sha: %s\n", escapeWorkQueueDisplay(*snapshot.SHA))
		}
		if snapshot.Worker != nil {
			fmt.Fprintf(w, "    worker: work=%s claim=%s\n", escapeWorkQueueDisplay(snapshot.Worker.WorkID), escapeWorkQueueDisplay(snapshot.Worker.ClaimID))
		}
	}
	if report.FinishIntent != "" {
		fmt.Fprintf(w, "    finish_intent: %s (requested, not a verified outcome)\n", escapeWorkQueueDisplay(report.FinishIntent))
	}
	for _, operation := range report.Operations {
		fmt.Fprintf(w, "    %s: %s\n", escapeWorkQueueDisplay(operation.Timestamp), escapeWorkQueueDisplay(operation.Message))
	}
}

func renderLogsDispatchCoordinatorToWriter(w io.Writer, runs []RunData) {
	for _, run := range runs {
		if run.DispatchCoordinator == nil {
			continue
		}
		fmt.Fprintf(w, "[work-queue] run=%d workflow=%s\n", run.RunID, escapeWorkQueueDisplay(run.WorkflowName))
		renderDispatchCoordinatorToWriter(w, run.DispatchCoordinator)
	}
}

func escapeWorkQueueDisplay(value string) string {
	return strings.TrimSuffix(strings.TrimPrefix(strconv.QuoteToGraphic(value), `"`), `"`)
}

func workQueueReportRuns(runs []RunData, include bool) []RunData {
	if include {
		return runs
	}
	projected := make([]RunData, 0, len(runs))
	for _, run := range runs {
		run.DispatchCoordinator = nil
		projected = append(projected, run)
	}
	return projected
}
