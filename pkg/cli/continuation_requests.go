package cli

import (
	"fmt"
	"io"
	"slices"
	"strings"
)

func continuationRequestCount(requests []ContinuationRequestReport) *int {
	if requests == nil {
		return nil
	}
	count := len(requests)
	return &count
}

func backfillContinuationRequests(requests *[]ContinuationRequestReport, run WorkflowRun, runDir string, verbose bool) bool {
	if *requests != nil {
		return false
	}
	if !continuationEvidenceMatchesRun(run, runDir, verbose) {
		*requests = nil
		return false
	}
	experiment, variant, _ := firstExperimentAssignment(extractExperimentData(runDir))
	*requests, _ = extractContinuationRequestsFromRun(runDir, run, verbose, experiment, variant)
	return *requests != nil
}

func continuationEvidenceMatchesRun(run WorkflowRun, runDir string, verbose bool) bool {
	if run.DatabaseID <= 0 || run.Attempt <= 0 || run.Repository == "" || run.UpdatedAt.IsZero() {
		return false
	}
	summary, ok := loadRunSummary(runDir, verbose)
	if !ok || summary == nil || summary.RunID != run.DatabaseID ||
		summary.Run.DatabaseID != run.DatabaseID ||
		summary.Run.Attempt != run.Attempt ||
		!strings.EqualFold(summary.Run.Repository, run.Repository) ||
		!summary.Run.UpdatedAt.Equal(run.UpdatedAt) ||
		summary.ProcessedAt.IsZero() || summary.ProcessedAt.Before(summary.Run.UpdatedAt) {
		return false
	}
	return true
}

func mergeContinuationRequestsIntoAudit(auditData *AuditData, requests []ContinuationRequestReport) bool {
	if auditData == nil || requests == nil {
		return false
	}
	changed := auditData.ContinuationRequests == nil || !slices.Equal(auditData.ContinuationRequests, requests)
	auditData.ContinuationRequests = requests
	count := len(requests)
	if auditData.SessionAnalysis == nil {
		auditData.SessionAnalysis = &SessionAnalysis{}
		changed = true
	}
	if auditData.SessionAnalysis.ContinuationRequestCount == nil || *auditData.SessionAnalysis.ContinuationRequestCount != count {
		auditData.SessionAnalysis.ContinuationRequestCount = &count
		changed = true
	}
	return changed
}

func renderTSVContinuationRequestSummary(w io.Writer, summary LogsSummary) {
	if summary.TotalContinuationRequests > 0 || summary.ContinuationRequestsUnavailableRuns > 0 {
		fmt.Fprintf(w, "# continuation_requests=%d continuation_requested_runs=%d continuation_requests_unavailable_runs=%d (requests only; no automatic dispatch or resume)\n",
			summary.TotalContinuationRequests, summary.ContinuationRequestedRuns, summary.ContinuationRequestsUnavailableRuns)
	}
}

func renderContinuationRequests(w io.Writer, requests []ContinuationRequestReport) error {
	if len(requests) == 0 {
		return nil
	}
	out := &compactLogsWriter{writer: w}
	out.printf("[continuation_requests] requests=%d (intent only; no automatic dispatch or resume)\n", len(requests))
	for _, request := range requests {
		out.printf("run=%d workflow=%q message=%q timestamp=%q", request.RunID, request.WorkflowName, request.Message, request.Timestamp)
		if request.ExperimentName != "" {
			out.printf(" experiment=%q variant=%q", request.ExperimentName, request.Variant)
		}
		out.println()
	}
	return out.err
}
