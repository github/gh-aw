package cli

import (
	"errors"
	"fmt"
	"time"

	"github.com/github/gh-aw/pkg/stringutil"
)

const doctorTokenFailureListingPages = 2

func collectDoctorTokenRunEvidence(checks []*DoctorTokenValidation, miner *doctorAssignmentRunMiner) {
	prioritizeDoctorTokenChecks(checks)
	targets, byPath := doctorTokenFailureTargets(checks)
	if len(targets) == 0 {
		return
	}
	failures, inventoryErr := listDoctorTokenFailureCandidates(miner, targets)
	representatives := map[string]WorkflowRun{}
	for _, run := range failures {
		if run.CreatedAt.After(representatives[run.WorkflowPath].CreatedAt) {
			representatives[run.WorkflowPath] = run
		}
	}
	for _, target := range prioritizeHealthFailureWorkflows(targets, failures) {
		check := byPath[target.Path]
		if run, exists := representatives[target.Path]; exists {
			check.RecentRuns = collectDoctorTokenFailedRun(miner, run)
		} else {
			check.RecentRuns = miner.collect(check.Workflow)
			if inventoryErr != nil {
				check.RecentRuns.Error = fmt.Sprintf("%s Failure-first inventory is incomplete: %s", check.RecentRuns.Error, inventoryErr)
			}
		}
		applyDoctorTokenRunEvidence(check)
	}
}

func doctorTokenFailureTargets(checks []*DoctorTokenValidation) ([]GitHubWorkflow, map[string]*DoctorTokenValidation) {
	var targets []GitHubWorkflow
	byPath := make(map[string]*DoctorTokenValidation)
	for _, check := range checks {
		if len(check.Secrets) == 0 && check.Status == "unknown" {
			continue
		}
		path := stringutil.MarkdownToLockFile(check.Workflow)
		targets = append(targets, GitHubWorkflow{Path: path})
		byPath[path] = check
	}
	return targets, byPath
}

func listDoctorTokenFailureCandidates(miner *doctorAssignmentRunMiner, targets []GitHubWorkflow) ([]WorkflowRun, error) {
	opts := ListWorkflowRunsOptions{
		StartDate: miner.now.Add(-doctorAssignmentRunLookback).Format(time.RFC3339),
		EndDate:   miner.now.Format(time.RFC3339), Limit: 100,
	}
	targetPaths := make(map[string]struct{})
	for _, target := range targets {
		targetPaths[target.Path] = struct{}{}
	}
	var runs []WorkflowRun
	for page := 1; page <= doctorTokenFailureListingPages; page++ {
		endpoint := healthFailureCandidatesEndpoint(opts, page, miner.repo, targets)
		out, err := miner.api(endpoint)
		if err != nil {
			return runs, err
		}
		batch, _, err := parseHealthFailureCandidates(out)
		if err != nil {
			return runs, err
		}
		for _, response := range batch {
			run := response.workflowRun()
			_, targeted := targetPaths[run.WorkflowPath]
			if targeted && run.Conclusion == "failure" && run.Status == "completed" {
				runs = append(runs, run)
			}
		}
		if len(batch) < opts.Limit {
			return runs, nil
		}
	}
	return runs, errors.New("failure inventory reached its two-page limit; older failures may be absent")
}

func collectDoctorTokenFailedRun(miner *doctorAssignmentRunMiner, run WorkflowRun) *DoctorAssignmentRunHistory {
	since := miner.now.Add(-doctorAssignmentRunLookback)
	evidence := miner.inspect(doctorAssignmentRunMetadata{
		ID: run.DatabaseID, Attempt: run.Attempt, URL: run.URL, CreatedAt: run.CreatedAt,
		Status: run.Status, Conclusion: run.Conclusion,
	}, since)
	return &DoctorAssignmentRunHistory{Since: since, Until: miner.now, Limit: 1, Runs: []DoctorAssignmentRunEvidence{evidence}}
}
