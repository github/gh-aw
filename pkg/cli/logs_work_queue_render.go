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
	return strings.TrimSuffix(strings.TrimPrefix(strconv.QuoteToGraphic(value), `"`), `"`)
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
