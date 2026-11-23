package cli

import "io"

// HealthSubagentFailure preserves Actions conclusions while exposing failed delegation.
type HealthSubagentFailure struct {
	RunID          int64  `json:"run_id"`
	URL            string `json:"url,omitempty"`
	AgentName      string `json:"agent_name"`
	RequestedModel string `json:"requested_model"`
	FailedCount    int    `json:"failed_count"`
}

func applyHealthSubagentFailures(health *WorkflowHealth, runs []WorkflowRun, config HealthConfig) {
	failuresByRun := make(map[int64]int)
	for _, run := range runs {
		summary, _ := loadHealthRunSummary(run, config)
		if summary == nil || summary.TokenUsage == nil {
			continue
		}
		// DeclaredSubagentModels mirrors observed requests; counting both doubles failures.
		for _, request := range summary.TokenUsage.SubagentModelRequests {
			if request.FailedCount <= 0 {
				continue
			}
			health.SubagentFailures = append(health.SubagentFailures, HealthSubagentFailure{
				RunID: run.DatabaseID, URL: run.URL, AgentName: request.AgentName,
				RequestedModel: request.RequestedModel, FailedCount: request.FailedCount,
			})
			health.SubagentFailureCount += request.FailedCount
			failuresByRun[run.DatabaseID] += request.FailedCount
		}
	}
	health.SubagentFailureRuns = len(failuresByRun)
	if health.SubagentFailureCount == 0 {
		return
	}
	health.Status = "subagent_failures_observed"
	for i := range health.Revisions {
		revision := &health.Revisions[i]
		for _, id := range revision.Current.RunIDs {
			revision.Current.SubagentFailures += failuresByRun[id]
		}
		if revision.Current.SubagentFailures > 0 && revision.Status != "revision_unknown" {
			revision.Status = health.Status
		}
	}
}

func renderHealthSubagentFailures(w io.Writer, health WorkflowHealth) error {
	if health.SubagentFailureCount == 0 {
		return nil
	}
	out := &compactLogsWriter{writer: w}
	out.printf("\n[subagent_failures] workflow=%q failed_invocations=%d affected_runs=%d evidence=cached_usage\n",
		health.WorkflowName, health.SubagentFailureCount, health.SubagentFailureRuns)
	for _, failure := range health.SubagentFailures {
		out.printf("run=%d agent=%q requested_model=%q failed_invocations=%d url=%q\n",
			failure.RunID, failure.AgentName, failure.RequestedModel, failure.FailedCount, failure.URL)
	}
	out.printf("Inspect affected runs with 'gh aw audit <run-id>'; Actions success does not imply successful delegation.\n")
	return out.err
}
