package cli

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
)

func verifyHealthFleetWorkflow(target GitHubWorkflow, history []WorkflowRun, config HealthConfig) (healthFleetWorkflowEvidence, error) {
	collection := config.collection
	var sample []WorkflowRun
	incompleteJobs := false
	boundaries := make(map[string]struct{})
	representatives := make(map[string]struct{})
	for _, run := range history {
		if _, stopped := boundaries[run.Event]; stopped || isIgnoredHealthRun(run) {
			continue
		}
		resolveHealthRevision(run, collection)
		if err := checkHealthTimeBudget(collection); err != nil {
			return healthFleetWorkflowEvidence{}, err
		}
		if collection.stopped {
			return healthFleetWorkflowEvidence{}, errors.New(collection.Warning)
		}
		if healthSourceChanged(run, collection) {
			boundaries[run.Event] = struct{}{}
			sample = append(sample, run)
			// Examine other events too: a green schedule sample must not hide an
			// older failure from a different trigger on the same revision.
			continue
		}
		failed := isClusterFailureConclusion(run.Conclusion)
		if _, sampled := representatives[run.Event]; failed && !sampled {
			if err := collectHealthRepresentativeJobs(run, collection); err != nil && collection.stopped {
				return healthFleetWorkflowEvidence{}, err
			}
			incompleteJobs = incompleteJobs || !collection.JobEvidence[run.DatabaseID].Complete
		}
		if failed || run.Status == "waiting" {
			hasJobs, err := healthRunHasJobs(run, collection)
			if err != nil {
				return healthFleetWorkflowEvidence{}, err
			}
			if !hasJobs {
				continue
			}
		}
		if failed {
			representatives[run.Event] = struct{}{}
		}
		if !run.StartedAt.IsZero() && !run.UpdatedAt.IsZero() {
			run.Duration = run.UpdatedAt.Sub(run.StartedAt)
		}
		sample = append(sample, run)

	}
	health := calculateCurrentWorkflowHealth(target.Name, sample, config)
	health.WorkflowPath = target.Path
	failures := healthFailureRecords(currentHealthRuns(sample, health.Revisions), config)
	if err := checkHealthTimeBudget(collection); err != nil {
		return healthFleetWorkflowEvidence{}, err
	}
	return healthFleetWorkflowEvidence{Health: health, Failures: failures, IncompleteJobs: incompleteJobs}, nil
}

type healthFleetFailure struct {
	WorkflowName string `json:"workflow_name"`
	WorkflowPath string `json:"workflow_path"`
	Failures     int    `json:"failures"`
	RunURL       string `json:"run_url"`
}

type healthFleetCluster struct {
	ID     string        `json:"id"`
	Signal FailureSignal `json:"signal"`
	Count  int           `json:"count"`
	RunURL string        `json:"run_url,omitempty"`
}

type healthFleetAction struct {
	Type       string `json:"type"`
	Checkpoint string `json:"checkpoint,omitempty"`
	RunURL     string `json:"run_url,omitempty"`
}

type healthFleetCoverage struct {
	RegistryComplete   bool   `json:"registry_complete"`
	InventoryComplete  bool   `json:"inventory_complete"`
	VerificationDone   bool   `json:"verification_complete"`
	WorkflowsVerified  int    `json:"workflows_verified"`
	WorkflowsRemaining int    `json:"workflows_remaining"`
	WindowsRemaining   int    `json:"windows_remaining"`
	UnresolvedSlices   int    `json:"unresolved_slices"`
	IncompleteJobs     int    `json:"workflows_with_incomplete_job_evidence"`
	Partial            bool   `json:"partial"`
	Reason             string `json:"reason,omitempty"`
}

type healthFleetReport struct {
	SchemaVersion  int                  `json:"schema_version"`
	Scope          healthFleetScope     `json:"scope"`
	Coverage       healthFleetCoverage  `json:"coverage"`
	TotalWorkflows int                  `json:"total_workflows"`
	HistoricalRuns int                  `json:"historical_runs"`
	Outcomes       map[string]int       `json:"historical_outcomes"`
	StatusCounts   map[string]int       `json:"status_counts"`
	TopFailures    []healthFleetFailure `json:"top_historical_failures"`
	Clusters       []healthFleetCluster `json:"top_current_failure_clusters"`
	NextAction     *healthFleetAction   `json:"next_action,omitempty"`
	Details        *healthFleetDetails  `json:"details,omitempty"`
}

type healthFleetDetails struct {
	Workflows              []WorkflowHealth      `json:"workflows"`
	FailureRepresentatives []healthFleetRunRef   `json:"failure_representatives"`
	FailureClusters        []FailureCluster      `json:"failure_clusters"`
	FailureInvestigation   *FailureInvestigation `json:"failure_investigation,omitempty"`
}

type healthFleetRunRef struct {
	RunID        int64  `json:"run_id"`
	WorkflowPath string `json:"workflow_path"`
}

func fleetFailureRepresentatives(evidence healthFleetWorkflowEvidence) []healthFleetRunRef {
	if evidence.Health.IntentionalFailure {
		return nil
	}
	failures := make(map[int64]RunData, len(evidence.Failures))
	for _, failure := range evidence.Failures {
		if failure.Conclusion == "failure" && !failure.IntentionalFailure {
			failures[failure.RunID] = failure
		}
	}
	var representatives []healthFleetRunRef
	for _, revision := range evidence.Health.Revisions {
		if revision.Current == nil || revision.Status == "revision_unknown" {
			continue
		}
		for _, id := range revision.Current.RunIDs {
			if failure, ok := failures[id]; ok {
				representatives = append(representatives, healthFleetRunRef{RunID: id, WorkflowPath: failure.WorkflowPath})
				break
			}
		}
	}
	return representatives
}

func renderHealthFleet(state *healthFleetState, config HealthConfig) error {
	report := makeHealthFleetReport(state, config)
	if config.JSONOutput {
		return json.NewEncoder(os.Stdout).Encode(report)
	}
	fmt.Fprintf(os.Stdout, "Workflow health: %s (%s)\n", report.Scope.Repository, report.Scope.Branch)
	fmt.Fprintf(os.Stdout, "Window: %s to %s\n", report.Scope.Start, report.Scope.End)
	fmt.Fprintf(os.Stdout, "Historical runs: %d (inventory complete: %t) %v\n", report.HistoricalRuns, report.Coverage.InventoryComplete, report.Outcomes)
	fmt.Fprintf(os.Stdout, "Revision-checked workflows: %d/%d %v\n", report.Coverage.WorkflowsVerified, report.TotalWorkflows, report.StatusCounts)
	for _, failure := range report.TopFailures {
		fmt.Fprintf(os.Stdout, "Historical failure: %q (%d) %s\n", failure.WorkflowName, failure.Failures, failure.RunURL)
	}
	for _, cluster := range report.Clusters {
		fmt.Fprintf(os.Stdout, "Current failure cluster: %q (%d) %s\n", cluster.Signal.Value, cluster.Count, cluster.RunURL)
	}
	if report.Coverage.Partial {
		fmt.Fprintf(os.Stdout, "Partial: %s\n", report.Coverage.Reason)
	}
	if action := report.NextAction; action != nil {
		fmt.Fprintf(os.Stdout, "Next action: %s %q\n", action.Type, cmp.Or(action.Checkpoint, action.RunURL))
	}
	if report.Details != nil {
		return json.NewEncoder(os.Stdout).Encode(report.Details)
	}
	return nil
}

func makeHealthFleetReport(state *healthFleetState, config HealthConfig) healthFleetReport {
	collection := config.collection
	coverage := healthFleetCoverage{
		RegistryComplete:  state.RegistryPage == 0,
		InventoryComplete: state.RegistryPage == 0 && len(state.Windows) == 0 && state.UnresolvedSlices == 0,
		WorkflowsVerified: len(state.Verified), WorkflowsRemaining: len(state.Workflows) - len(state.Verified),
		WindowsRemaining: len(state.Windows), UnresolvedSlices: state.UnresolvedSlices,
		Reason: collection.Reason,
	}
	coverage.VerificationDone = coverage.InventoryComplete && coverage.WorkflowsRemaining == 0
	for _, evidence := range state.Verified {
		if evidence.IncompleteJobs {
			coverage.IncompleteJobs++
		}
	}
	if coverage.Reason == "" && coverage.IncompleteJobs > 0 {
		coverage.Reason = "job_evidence_incomplete"
	}
	coverage.Partial = collection.Partial || !coverage.VerificationDone || coverage.IncompleteJobs > 0
	report := healthFleetReport{SchemaVersion: 1, Scope: state.Scope, Coverage: coverage,
		TotalWorkflows: len(state.Workflows), HistoricalRuns: len(state.Runs), Outcomes: make(map[string]int),
		StatusCounts: make(map[string]int), TopFailures: []healthFleetFailure{}, Clusters: []healthFleetCluster{}}
	report.summarizeInventory(state)
	var failures []RunData
	workflows := make([]WorkflowHealth, 0, len(state.Workflows))
	representatives := []healthFleetRunRef{}
	for _, workflow := range state.Workflows {
		health := WorkflowHealth{WorkflowName: workflow.Name, WorkflowPath: workflow.Path, Status: "not_collected"}
		if evidence, done := state.Verified[workflow.Path]; done {
			health = evidence.Health
			failures = append(failures, evidence.Failures...)
			if config.Full {
				representatives = append(representatives, fleetFailureRepresentatives(evidence)...)
			}
		}
		report.StatusCounts[health.Status]++
		workflows = append(workflows, health)
	}
	clusters := annotateFailureClusters(failures)
	investigation := buildFailureInvestigation(failures, clusters)
	for _, cluster := range clusters[:min(5, len(clusters))] {
		row := healthFleetCluster{ID: cluster.ID, Signal: cluster.Signal, Count: cluster.Count}
		if len(cluster.RepresentativeRuns) > 0 {
			row.RunURL = cluster.RepresentativeRuns[0].URL
		}
		report.Clusters = append(report.Clusters, row)
	}
	if config.Checkpoint != "" && !coverage.VerificationDone && collection.stopped {
		report.NextAction = &healthFleetAction{Type: "resume", Checkpoint: config.Checkpoint}
	} else if investigation != nil && len(investigation.SelectedRuns) > 0 {
		report.NextAction = &healthFleetAction{Type: "audit", RunURL: investigation.SelectedRuns[0].URL}
	}
	if config.Full {
		report.Details = &healthFleetDetails{Workflows: workflows, FailureRepresentatives: representatives, FailureClusters: clusters, FailureInvestigation: investigation}
	}
	return report
}

func (report *healthFleetReport) summarizeInventory(state *healthFleetState) {
	for _, history := range state.groupedRuns() {
		var failure healthFleetFailure
		for _, run := range history {
			report.Outcomes[cmp.Or(run.Conclusion, run.Status, "unknown")]++
			if run.Conclusion != "failure" {
				continue
			}
			if failure.Failures == 0 {
				failure = healthFleetFailure{WorkflowName: run.WorkflowName, WorkflowPath: run.WorkflowPath, RunURL: run.URL}
			}
			failure.Failures++
		}
		if failure.Failures > 0 {
			report.TopFailures = append(report.TopFailures, failure)
		}
	}
	slices.SortFunc(report.TopFailures, func(a, b healthFleetFailure) int {
		return cmp.Or(cmp.Compare(b.Failures, a.Failures), cmp.Compare(a.WorkflowPath, b.WorkflowPath))
	})
	report.TopFailures = report.TopFailures[:min(5, len(report.TopFailures))]
}
