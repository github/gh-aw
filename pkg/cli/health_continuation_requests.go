package cli

import (
	"cmp"
	"io"
	"slices"
)

type HealthContinuationRequestRun struct {
	RunID        int64  `json:"run_id"`
	URL          string `json:"url,omitempty"`
	RequestCount *int   `json:"request_count"`
	Evidence     string `json:"evidence"`
}

// HealthContinuationRequests counts cached requests, never successful continuations.
type HealthContinuationRequests struct {
	RequestCount    int                            `json:"request_count"`
	RequestedRuns   int                            `json:"requested_runs"`
	UnavailableRuns int                            `json:"unavailable_runs"`
	Runs            []HealthContinuationRequestRun `json:"runs"`
}

func applyHealthContinuationRequests(health *WorkflowHealth, runs []WorkflowRun, config HealthConfig) {
	report := &HealthContinuationRequests{Runs: []HealthContinuationRequestRun{}}
	seen := make(map[int64]bool)
	orderedRuns := slices.Clone(runs)
	slices.SortFunc(orderedRuns, func(a, b WorkflowRun) int {
		if !a.CreatedAt.Equal(b.CreatedAt) {
			if a.CreatedAt.After(b.CreatedAt) {
				return -1
			}
			return 1
		}
		switch {
		case a.DatabaseID > b.DatabaseID:
			return -1
		case a.DatabaseID < b.DatabaseID:
			return 1
		default:
			return 0
		}
	})
	for _, run := range orderedRuns {
		if seen[run.DatabaseID] {
			continue
		}
		seen[run.DatabaseID] = true
		evidence := HealthContinuationRequestRun{RunID: run.DatabaseID, URL: run.URL, Evidence: "unavailable"}
		summary, dir := loadHealthRunSummary(run, config)
		if summary != nil && summary.CLIVersion == GetVersion() {
			backfillContinuationRequests(&summary.ContinuationRequests, run, dir, false)
		}
		if summary == nil || summary.ContinuationRequests == nil {
			report.UnavailableRuns++
		} else {
			count := len(summary.ContinuationRequests)
			evidence.RequestCount = &count
			evidence.Evidence = "cached_safe_output"
			report.RequestCount += count
			if count > 0 {
				report.RequestedRuns++
			}
		}
		report.Runs = append(report.Runs, evidence)
	}
	slices.SortFunc(report.Runs, func(a, b HealthContinuationRequestRun) int { return cmp.Compare(b.RunID, a.RunID) })
	health.ContinuationRequests = report
}

func renderHealthContinuationRequests(w io.Writer, health WorkflowHealth) error {
	report := health.ContinuationRequests
	if report == nil {
		return nil
	}
	out := &compactLogsWriter{writer: w}
	out.printf("\n[continuation_requests] workflow=%q requests=%d requested_runs=%d unavailable_runs=%d evidence=cached_safe_output\n",
		health.WorkflowName, report.RequestCount, report.RequestedRuns, report.UnavailableRuns)
	for _, run := range report.Runs {
		if run.RequestCount == nil {
			out.printf("run=%d requests=unavailable evidence=%s url=%q\n", run.RunID, run.Evidence, run.URL)
		} else {
			out.printf("run=%d requests=%d evidence=%s url=%q\n", run.RunID, *run.RequestCount, run.Evidence, run.URL)
		}
	}
	out.println("Requests express intent only, not successful continuations; no automatic dispatch or resume. Missing cached evidence is unavailable, not zero.")
	return out.err
}
