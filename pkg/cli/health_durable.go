package cli

import "io"

type HealthDurableRun struct {
	RunID   int64                 `json:"run_id"`
	URL     string                `json:"url,omitempty"`
	Session *DurableSessionReport `json:"session"`
}

// HealthDurableSessions is current-revision cached evidence, not a continuation rate.
type HealthDurableSessions struct {
	ResumedRuns     int                `json:"resumed_runs"`
	FreshRuns       int                `json:"fresh_runs"`
	RestoredRuns    int                `json:"restored_runs"`
	FailedRuns      int                `json:"restore_failed_runs"`
	UnavailableRuns int                `json:"unavailable_runs"`
	Runs            []HealthDurableRun `json:"runs"`
}

func applyHealthDurableSessions(health *WorkflowHealth, runs []WorkflowRun, config HealthConfig) {
	report := &HealthDurableSessions{}
	var missing []HealthDurableRun
	knownDurable := false
	for _, run := range runs {
		summary, dir, compatible := loadHealthRunSummaryWithSessionCompatibility(run, config)
		configured := false
		if summary != nil {
			configured = hasDurableRestoreStep(summary.JobDetails)
		}
		if config.collection != nil {
			if jobs := config.collection.jobs[healthRunAttempt{id: run.DatabaseID, attempt: run.Attempt}]; jobs != nil {
				configured = configured || hasDurableRestoreStep(jobs.details)
			}
			knownDurable = knownDurable || configured
			if checkHealthTimeBudget(config.collection) != nil {
				missing = append(missing, HealthDurableRun{RunID: run.DatabaseID, URL: run.URL,
					Session: &DurableSessionReport{Status: "unavailable", Reason: config.collection.Reason}})
				continue
			}
		}
		knownDurable = knownDurable || configured
		if !compatible {
			missing = append(missing, HealthDurableRun{RunID: run.DatabaseID, URL: run.URL,
				Session: &DurableSessionReport{Status: "unavailable", Reason: "incompatible_session_cache"}})
			continue
		}
		session := readDurableSessionReport(dir, run, configured)
		if session == nil {
			missing = append(missing, HealthDurableRun{RunID: run.DatabaseID, URL: run.URL,
				Session: &DurableSessionReport{Status: "unavailable", Reason: "session_not_cached"}})
			continue
		}
		report.Runs = append(report.Runs, HealthDurableRun{RunID: run.DatabaseID, URL: run.URL, Session: session})
		switch session.Status {
		case "resumed":
			report.ResumedRuns++
		case "fresh":
			if session.Started {
				report.FreshRuns++
			} else {
				report.UnavailableRuns++
			}
		case "restored":
			report.RestoredRuns++
		case "restore_failed":
			report.FailedRuns++
		default:
			report.UnavailableRuns++
		}
	}
	if knownDurable || len(report.Runs) > 0 {
		for _, run := range missing {
			report.Runs = append(report.Runs, run)
			report.UnavailableRuns++
		}
		health.DurableSessions = report
	}
}

func renderHealthDurableSessions(w io.Writer, health WorkflowHealth) error {
	if health.DurableSessions == nil {
		return nil
	}
	out := &compactLogsWriter{writer: w}
	report := health.DurableSessions
	out.printf("\n[durable_sessions] workflow=%q resumed=%d fresh=%d restored_without_start=%d restore_failed=%d unavailable=%d evidence=cached_session\n",
		health.WorkflowName, report.ResumedRuns, report.FreshRuns, report.RestoredRuns, report.FailedRuns, report.UnavailableRuns)
	for _, run := range report.Runs {
		out.printf("run=%d ", run.RunID)
		if err := renderDurableSession(w, run.Session); err != nil {
			return err
		}
	}
	out.printf("Missing durable events are unavailable evidence, not a fresh session. Use 'gh aw audit <run-id> --artifacts info,usage' to populate the cache.\n")
	return out.err
}

func renderHealthSessionEvidence(w io.Writer, health WorkflowHealth) error {
	if err := renderHealthSubagentFailures(w, health); err != nil {
		return err
	}
	if err := renderHealthDetectionFailures(w, health); err != nil {
		return err
	}
	return renderHealthDurableSessions(w, health)
}
