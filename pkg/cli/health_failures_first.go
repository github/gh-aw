package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path"
	"slices"
	"strconv"
	"time"
)

var healthListFailureCandidates = listHealthFailureCandidates

type HealthPotentialIssue struct {
	WorkflowPath         string `json:"workflow_path"`
	WorkflowName         string `json:"workflow_name"`
	FailedRuns           int    `json:"failed_runs"`
	RunID                int64  `json:"representative_run_id"`
	URL                  string `json:"url,omitempty"`
	Evidence             string `json:"evidence"`
	RepresentativeStatus string `json:"representative_status"`
}

// Inventory list-only signals before verifying one representative per scope.
func collectHealthFailures(startDate, repoOverride string, collection *HealthCollection) ([]WorkflowRun, error) {
	inventory := newHealthFailureInventory(startDate, collection)
	candidates, err := inventory.fill(repoOverride, collection)
	if err != nil {
		return nil, err
	}
	collection.failureInventory = candidates
	scopes := make(map[string]struct{})
	var runs []WorkflowRun
	for index := 0; index >= 0 && index < len(collection.failureInventory); index++ {
		run := collection.failureInventory[index]
		scope := failureClusterKey(run.Repository, run.WorkflowPath, run.HeadBranch, run.Event)
		if _, sampled := scopes[scope]; sampled {
			continue
		}
		hasJobs, err := healthRunHasJobs(run, collection)
		if err != nil {
			return runs, err
		}
		if !hasJobs {
			if err := inventory.observeJobless(run, repoOverride, collection); err != nil {
				return runs, err
			}
			continue
		}
		resolveHealthRevision(run, collection)
		verification := "revision_unknown"
		if healthSourceChanged(run, collection) {
			markHealthPotentialIssue(run, collection, "superseded")
			scopes[scope] = struct{}{}
			continue
		}
		if metadata, _ := healthRunRevision(run, collection); metadata != nil {
			verification = "revision_checked"
		}
		markHealthPotentialIssue(run, collection, verification)
		if !run.StartedAt.IsZero() && !run.UpdatedAt.IsZero() {
			run.Duration = run.UpdatedAt.Sub(run.StartedAt)
		}
		scopes[scope] = struct{}{}
		if collection.failureScopes == nil {
			collection.failureScopes = make(map[string]map[string]struct{})
		}
		if collection.failureScopes[run.WorkflowPath] == nil {
			collection.failureScopes[run.WorkflowPath] = make(map[string]struct{})
		}
		collection.failureScopes[run.WorkflowPath][healthFailureSampleScope(run)] = struct{}{}
		runs = append(runs, run)
		if err := collectHealthRepresentativeJobs(run, collection); err != nil && collection.stopped {
			return runs, err
		}
		if collection.APIRequests >= healthMaximumAPIRequests && !collection.stopped {
			stopHealthCollection(collection, errHealthAPIBudget)
		}
		if collection.stopped {
			return runs, errHealthAPIBudget
		}
	}
	collection.FailurePassComplete = collection.FailureListingComplete
	return runs, nil
}

func healthFailureSampleScope(run WorkflowRun) string {
	return failureClusterKey(run.Repository, run.WorkflowPath, run.HeadBranch, run.Event)
}

type healthFailureInventory struct {
	targets   map[string]GitHubWorkflow
	seen      map[int64]struct{}
	opts      ListWorkflowRunsOptions
	batches   [][]workflowRunAPIResponse
	page      int
	pages     int
	exhausted bool
}

func (inventory *healthFailureInventory) observeJobless(run WorkflowRun, repoOverride string, collection *HealthCollection) error {
	if err := observeHealthApprovalExpiry(run, collection); err != nil {
		return err
	}
	markHealthPotentialIssue(run, collection, "jobless")
	if collection.ApprovalExpiryEvidence[run.DatabaseID].Status == "ignored" {
		backfill, err := inventory.fill(repoOverride, collection)
		collection.failureInventory = append(collection.failureInventory, backfill...)
		return err
	}
	return nil
}

func newHealthFailureInventory(startDate string, collection *HealthCollection) *healthFailureInventory {
	targets := make(map[string]GitHubWorkflow)
	for _, target := range collection.workflows {
		targets[target.Path] = target
	}
	return &healthFailureInventory{
		targets: targets, seen: make(map[int64]struct{}), page: 1,
		opts: ListWorkflowRunsOptions{StartDate: startDate, EndDate: collection.EndDate, Limit: 100},
	}
}

func (inventory *healthFailureInventory) fill(repoOverride string, collection *HealthCollection) ([]WorkflowRun, error) {
	issues := make(map[string]int)
	candidatesByWorkflow := make(map[string]int)
	for index, issue := range collection.PotentialIssues {
		issues[issue.WorkflowPath] = index
		candidatesByWorkflow[issue.WorkflowPath] = issue.FailedRuns
	}
	var runs []WorkflowRun
	// Reuse already fetched overflow before requesting older pages after an exclusion.
	for _, batch := range inventory.batches {
		runs = appendHealthFailureCandidates(batch, inventory.targets, inventory.seen, candidatesByWorkflow, issues, runs, collection)
	}
	for {
		if inventory.exhausted || allHealthFailureWorkflowsSaturated(inventory.targets, candidatesByWorkflow) {
			collection.FailureListingComplete = true
			return runs, nil
		}
		collection.FailureListingComplete = false
		if inventory.pages >= healthMaximumFailureListingPages {
			collection.Partial, collection.Reason = true, "failure_listing_limit"
			collection.Warning = fmt.Sprintf("Health failure inventory stopped after %d pages; chronological sampling will continue", inventory.pages)
			return runs, nil
		}
		batch, oldest, err := healthListFailureCandidates(inventory.opts, inventory.page, repoOverride, collection)
		if err != nil {
			return runs, err
		}
		inventory.pages++
		inventory.batches = append(inventory.batches, batch)
		runs = appendHealthFailureCandidates(batch, inventory.targets, inventory.seen, candidatesByWorkflow, issues, runs, collection)
		inventory.exhausted = len(batch) < inventory.opts.Limit
		if !inventory.exhausted && inventory.page*inventory.opts.Limit >= healthMaximumRunSearchResults &&
			!allHealthFailureWorkflowsSaturated(inventory.targets, candidatesByWorkflow) {
			if !advanceHealthRunWindow(&inventory.opts, oldest, collection) {
				return runs, nil
			}
			inventory.page = 0
		}
		inventory.page++
	}
}

func appendHealthFailureCandidates(batch []workflowRunAPIResponse, targets map[string]GitHubWorkflow, seen map[int64]struct{},
	candidatesByWorkflow, issues map[string]int, runs []WorkflowRun, collection *HealthCollection) []WorkflowRun {
	for _, response := range batch {
		run := response.workflowRun()
		target, registered := targets[run.WorkflowPath]
		if _, duplicate := seen[run.DatabaseID]; duplicate || !registered || run.Conclusion != "failure" {
			continue
		}
		if candidatesByWorkflow[run.WorkflowPath] >= healthMinimumRuns {
			continue
		}
		seen[run.DatabaseID] = struct{}{}
		if filterHealthPRLifecycleRun(run, collection) {
			continue
		}
		candidatesByWorkflow[run.WorkflowPath]++
		if run.WorkflowName == "" {
			run.WorkflowName = target.Name
		}
		runs = append(runs, run)
		collection.FailureCandidates++
		index, exists := issues[run.WorkflowPath]
		if !exists {
			index = len(collection.PotentialIssues)
			issues[run.WorkflowPath] = index
			collection.PotentialIssues = append(collection.PotentialIssues, HealthPotentialIssue{
				WorkflowPath: run.WorkflowPath, WorkflowName: run.WorkflowName,
				RunID: run.DatabaseID, URL: run.URL, Evidence: "run_list_only", RepresentativeStatus: "unverified",
			})
			collection.FailureCandidateWorkflows++
		}
		issue := &collection.PotentialIssues[index] //nolint:uncheckedsliceindex // Index is inserted into the map only after appending to this slice.
		issue.FailedRuns++
	}
	return runs
}

func allHealthFailureWorkflowsSaturated(targets map[string]GitHubWorkflow, candidatesByWorkflow map[string]int) bool {
	for workflowPath := range targets {
		if candidatesByWorkflow[workflowPath] < healthMinimumRuns {
			return false
		}
	}
	return true
}

func listHealthFailureCandidates(opts ListWorkflowRunsOptions, page int, repoOverride string, collection *HealthCollection) ([]workflowRunAPIResponse, time.Time, error) {
	base := path.Join(healthRepositoryEndpoint(repoOverride), "actions/runs")
	if len(collection.workflows) == 1 {
		for _, target := range collection.workflows {
			id := path.Base(target.Path)
			if target.ID > 0 {
				id = strconv.FormatInt(target.ID, 10)
			}
			base = path.Join(healthRepositoryEndpoint(repoOverride), "actions/workflows", url.PathEscape(id), "runs")
		}
	}
	endpoint := fmt.Sprintf("%s?status=failure&per_page=%d&page=%d&created=%s",
		base, opts.Limit, page,
		url.QueryEscape(buildCreatedFilter(opts.StartDate, opts.EndDate, opts.BeforeDate)))
	out, err := healthAPI(collection, endpoint)
	if err != nil {
		return nil, time.Time{}, err
	}
	var data struct {
		Runs []workflowRunAPIResponse `json:"workflow_runs"`
	}
	if err := json.Unmarshal(out, &data); err != nil {
		return nil, time.Time{}, fmt.Errorf("failed to parse health failure runs: %w", err)
	}
	if data.Runs == nil {
		return nil, time.Time{}, errors.New("failed to parse health failure runs: missing workflow_runs array")
	}
	var oldest time.Time
	for _, run := range data.Runs {
		if oldest.IsZero() || run.CreatedAt.Before(oldest) {
			oldest = run.CreatedAt
		}
	}
	return data.Runs, oldest, nil
}

func markHealthPotentialIssue(run WorkflowRun, collection *HealthCollection, status string) {
	for i := range collection.PotentialIssues {
		if collection.PotentialIssues[i].RunID == run.DatabaseID {
			collection.PotentialIssues[i].RepresentativeStatus = status
			return
		}
	}
}

func prioritizeHealthFailureWorkflows(targets []GitHubWorkflow, failures []WorkflowRun) []GitHubWorkflow {
	recent := make(map[string]time.Time)
	for _, run := range failures {
		if run.CreatedAt.After(recent[run.WorkflowPath]) {
			recent[run.WorkflowPath] = run.CreatedAt
		}
	}
	ordered := slices.Clone(targets)
	slices.SortStableFunc(ordered, func(a, b GitHubWorkflow) int {
		return recent[b.Path].Compare(recent[a.Path])
	})
	return ordered
}

func mergeHealthRunSamples(runs, failures []WorkflowRun) []WorkflowRun {
	seen := make(map[int64]struct{})
	var merged []WorkflowRun
	for _, sample := range [][]WorkflowRun{runs, failures} {
		for _, run := range sample {
			if _, duplicate := seen[run.DatabaseID]; duplicate {
				continue
			}
			seen[run.DatabaseID] = struct{}{}
			merged = append(merged, run)
		}
	}
	return merged
}
