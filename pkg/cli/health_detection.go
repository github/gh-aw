package cli

import "io"

// HealthDetectionFailure exposes security outcomes without changing Actions conclusions.
type HealthDetectionFailure struct {
	RunID       int64            `json:"run_id"`
	URL         string           `json:"url,omitempty"`
	Code        AuditFindingCode `json:"code"`
	Title       string           `json:"title"`
	Description string           `json:"description"`
}

func applyHealthDetectionFailures(health *WorkflowHealth, runs []WorkflowRun, config HealthConfig) {
	failuresByRun := make(map[int64]bool)
	for _, run := range runs {
		processed := ProcessedRun{Run: run}
		// Artifact paths supplied by callers are not evidence of cache compatibility.
		processed.Run.LogsPath = ""
		summary, dir := loadHealthRunSummary(run, config)
		if summary != nil {
			processed.JobDetails = summary.JobDetails
			if summary.CLIVersion == GetVersion() {
				processed.Run.LogsPath = dir
			}
		}
		if config.collection != nil {
			if jobs := config.collection.jobs[healthRunAttempt{id: run.DatabaseID, attempt: run.Attempt}]; jobs != nil && jobs.complete {
				processed.JobDetails = jobs.details
			}
		}
		for _, finding := range generateThreatDetectionFindings(processed) {
			health.DetectionFailures = append(health.DetectionFailures, HealthDetectionFailure{
				RunID: run.DatabaseID, URL: run.URL, Code: finding.Code,
				Title: finding.Title, Description: finding.Description,
			})
			failuresByRun[run.DatabaseID] = true
		}
	}
	health.DetectionFailureRuns = len(failuresByRun)
	if health.DetectionFailureRuns == 0 {
		return
	}
	health.Status = "detection_failures_observed"
	for i := range health.Revisions {
		revision := &health.Revisions[i]
		for _, id := range revision.Current.RunIDs {
			if failuresByRun[id] {
				revision.Current.DetectionFailureRuns++
			}
		}
		if revision.Current.DetectionFailureRuns > 0 {
			revision.Status = health.Status
		}
	}
}

func renderHealthDetectionFailures(w io.Writer, health WorkflowHealth) error {
	if health.DetectionFailureRuns == 0 {
		return nil
	}
	out := &compactLogsWriter{writer: w}
	out.printf("\n[detection_failures] workflow=%q affected_runs=%d\n", health.WorkflowName, health.DetectionFailureRuns)
	for _, failure := range health.DetectionFailures {
		out.printf("run=%d code=%q title=%q description=%q url=%q\n",
			failure.RunID, failure.Code, failure.Title, failure.Description, failure.URL)
	}
	out.printf("Inspect affected runs with 'gh aw audit <run-id>'; Actions success does not imply successful threat detection.\n")
	return out.err
}
