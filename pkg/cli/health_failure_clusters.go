package cli

import (
	"path/filepath"
	"strconv"

	"github.com/github/gh-aw/pkg/workflow"
)

// Reuse logs clustering without downloading artifacts or retaining full cached analyses.
func buildHealthFailureClusters(runs []WorkflowRun, config HealthConfig) ([]FailureCluster, *FailureInvestigation) {
	runs = currentHealthRuns(runs, healthRevisionStatuses(runs, config.collection))
	var failures []RunData
	intentional := make(map[string]bool)
	for _, run := range runs {
		if !isClusterFailureConclusion(run.Conclusion) {
			continue
		}
		data := RunData{
			RunID: run.DatabaseID, Repository: run.Repository, URL: run.URL,
			WorkflowName: run.WorkflowName, WorkflowPath: run.WorkflowPath,
			Conclusion: run.Conclusion, CreatedAt: run.CreatedAt,
			Branch: run.HeadBranch, Event: run.Event, HeadSHA: run.HeadSha,
			RunAttempt: strconv.Itoa(run.Attempt), UpdatedAt: run.UpdatedAt,
		}
		if config.RepoOverride == "" {
			value, known := intentional[run.WorkflowPath]
			if !known {
				value = workflow.IsIntentionalFailure(run.WorkflowPath)
				intentional[run.WorkflowPath] = value
			}
			data.IntentionalFailure = value
		}
		enrichHealthFailureFromCache(&data, run, config)
		if config.collection != nil {
			if jobs := config.collection.jobs[healthRunAttempt{id: run.DatabaseID, attempt: run.Attempt}]; jobs != nil && jobs.complete {
				pr := ProcessedRun{Run: run, JobDetails: jobs.details}
				data.FailureSignals = append(data.FailureSignals, failureSignalsForRun(pr)...)
			}
		}
		failures = append(failures, data)
	}
	clusters := annotateFailureClusters(failures)
	return clusters, buildFailureInvestigation(failures, clusters)
}

func enrichHealthFailureFromCache(data *RunData, run WorkflowRun, config HealthConfig) {
	summary, dir := loadHealthRunSummary(run, config)
	if summary == nil {
		return
	}
	pr := ProcessedRun{
		Run: summary.Run, JobDetails: summary.JobDetails, MCPFailures: summary.MCPFailures,
		MissingTools: summary.MissingTools, MissingData: summary.MissingData, FirewallAnalysis: summary.FirewallAnalysis,
	}
	var agg logsAggregate
	data.FailureKind = agg.classifyRunFailure(pr)
	data.FailureSignals = failureSignalsForRun(pr)
	data.LogsPath = dir
}

func loadHealthRunSummary(run WorkflowRun, config HealthConfig) (*RunSummary, string) {
	root := config.LogsDir
	if root == "" {
		root = defaultLogsOutputDir
	}
	dir := filepath.Join(root, "run-"+strconv.FormatInt(run.DatabaseID, 10))
	key := healthRunAttempt{id: run.DatabaseID, attempt: run.Attempt}
	if config.collection != nil {
		if summary, checked := config.collection.cacheSummaries[key]; checked {
			return summary, dir
		}
		if checkHealthTimeBudget(config.collection) != nil {
			recordHealthCacheEvidence(run, nil, config.collection.Reason, config.collection)
			config.collection.cacheSummaries[key] = nil
			return nil, dir
		}
	}
	summary, reason := readHealthCache(dir)
	if reason != "" {
		recordHealthCacheEvidence(run, nil, reason, config.collection)
		if config.collection != nil {
			config.collection.cacheSummaries[key] = nil
		}
		return nil, dir // Cache loader logs missing or invalid evidence.
	}
	cached := summary.Run
	if summary.RunID != run.DatabaseID || cached.DatabaseID != run.DatabaseID ||
		run.URL == "" || cached.URL != run.URL || cached.Status != run.Status || cached.Conclusion != run.Conclusion ||
		cached.HeadSha != run.HeadSha || cached.WorkflowPath != run.WorkflowPath ||
		cached.Repository != run.Repository || cached.Attempt != run.Attempt || !cached.UpdatedAt.Equal(run.UpdatedAt) {
		healthLog.Printf("Ignoring stale health evidence for run %d", run.DatabaseID)
		recordHealthCacheEvidence(run, summary, "stale", config.collection)
		if config.collection != nil {
			config.collection.cacheSummaries[key] = nil
		}
		return nil, dir
	}
	status := "compatible"
	producer := summary
	if summary.CLIVersion != GetVersion() {
		// Jobs are raw API observations. Never reuse version-dependent derived
		// usage, classification, or sub-agent metrics from another producer.
		status = "version_mismatch_raw_jobs_only"
		summary = &RunSummary{CLIVersion: summary.CLIVersion, RunID: summary.RunID,
			Run: run, JobDetails: summary.JobDetails}
		healthLog.Printf("Health run %d reuses raw jobs only: cached=%s current=%s", run.DatabaseID, summary.CLIVersion, GetVersion())
	}
	recordHealthCacheEvidence(run, producer, status, config.collection)
	if config.collection != nil {
		config.collection.cacheSummaries[key] = summary
	}
	return summary, dir
}
