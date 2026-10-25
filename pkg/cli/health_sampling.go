package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/repoutil"
)

var healthDiscoverWorkflows = fetchHealthWorkflows

func healthRepositoryEndpoint(repoOverride string) string {
	if repoOverride == "" {
		return "repos/{owner}/{repo}"
	}
	repository, _ := repoutil.NormalizeRepoForAPI(repoOverride)
	return "repos/" + repository
}

func discoverHealthWorkflows(config *HealthConfig) error {
	collection := config.collection
	collection.repoOverride = config.RepoOverride
	collection.MinimumRuns = healthMinimumRuns
	if collection.MaximumDays == 0 {
		collection.MaximumDays = config.Days
	}
	registered, err := healthDiscoverWorkflows(collection)
	if err != nil {
		if !collection.Partial {
			return err
		}
	}
	requested := config.WorkflowName
	for _, entry := range registered {
		if !strings.HasSuffix(entry.Path, ".lock.yml") {
			continue
		}
		if requested != "" {
			if requested != entry.Name && normalizeWorkflowID(requested) != normalizeWorkflowID(entry.Path) {
				continue
			}
			config.WorkflowName = entry.Name
		} else if entry.State != "active" {
			continue
		}
		collection.workflows = append(collection.workflows, entry)
	}
	if requested != "" && len(collection.workflows) > 1 {
		return fmt.Errorf("workflow name %q is ambiguous; specify its .github/workflows/ lock-file path", requested)
	}
	if config.WorkflowName != "" && len(collection.workflows) == 0 && !collection.Partial {
		return fmt.Errorf("agentic workflow %q not found in the repository workflow registry", config.WorkflowName)
	}
	slices.SortFunc(collection.workflows, func(a, b GitHubWorkflow) int { return strings.Compare(a.Path, b.Path) })
	return nil
}

func fetchHealthWorkflows(collection *HealthCollection) ([]GitHubWorkflow, error) {
	var registered []GitHubWorkflow
	for page := 1; page <= MaxIterations; page++ {
		endpoint := fmt.Sprintf("%s/actions/workflows?per_page=100&page=%d", healthRepositoryEndpoint(collection.repoOverride), page)
		out, err := healthAPI(collection, endpoint)
		if err != nil {
			return registered, err
		}
		var data struct {
			Workflows []GitHubWorkflow `json:"workflows"`
		}
		if err := json.Unmarshal(out, &data); err != nil {
			return registered, fmt.Errorf("failed to parse health workflow registry: %w", err)
		}
		registered = append(registered, data.Workflows...)
		if len(data.Workflows) < 100 {
			return registered, nil
		}
	}
	collection.Partial, collection.Reason = true, "workflow_registry_limit"
	return registered, nil
}

func listHealthWorkflowRuns(opts ListWorkflowRunsOptions, page int, collection *HealthCollection) ([]WorkflowRun, int, error) {
	endpoint := path.Join(healthRepositoryEndpoint(opts.RepoOverride), "actions/runs")
	if opts.WorkflowName != "" {
		endpoint = path.Join(healthRepositoryEndpoint(opts.RepoOverride), "actions/workflows", url.PathEscape(opts.WorkflowName), "runs")
	}
	endpoint += fmt.Sprintf("?per_page=%d&page=%d&created=%s", opts.Limit, page, url.QueryEscape(buildCreatedFilter(opts.StartDate, opts.EndDate, opts.BeforeDate)))
	out, err := healthAPI(collection, endpoint)
	if err != nil {
		return nil, 0, err
	}
	var data struct {
		Runs []workflowRunAPIResponse `json:"workflow_runs"`
	}
	if err := json.Unmarshal(out, &data); err != nil {
		return nil, 0, fmt.Errorf("failed to parse health runs: %w", err)
	}
	var runs []WorkflowRun
	var oldest time.Time
	for _, response := range data.Runs {
		if oldest.IsZero() || response.CreatedAt.Before(oldest) {
			oldest = response.CreatedAt
		}
		run := response.workflowRun()
		if isIgnoredHealthRun(run) || filterHealthPRLifecycleRun(run, collection) {
			continue
		}
		if run.Status == "waiting" || isClusterFailureConclusion(run.Conclusion) {
			hasJobs, err := healthRunHasJobs(run, collection)
			if err != nil {
				return runs, len(data.Runs), err
			}
			if !hasJobs {
				if err := observeHealthApprovalExpiry(run, collection); err != nil {
					return runs, len(data.Runs), err
				}
				healthLog.Printf("Ignoring run %d with no jobs", run.DatabaseID)
				continue
			}
		}
		runs = append(runs, run)
	}
	if opts.OldestFetchedCreatedAt != nil {
		*opts.OldestFetchedCreatedAt = oldest
	}
	return runs, len(data.Runs), nil
}

type healthRunAttempt struct {
	id      int64
	attempt int
}

func healthRunHasJobs(run WorkflowRun, collection *HealthCollection) (bool, error) {
	key := healthRunAttempt{id: run.DatabaseID, attempt: run.Attempt}
	if hasJobs, checked := collection.jobPresence[key]; checked {
		return hasJobs, nil
	}
	if jobs := collection.jobs[key]; jobs != nil && jobs.complete {
		return len(jobs.details) > 0, nil
	}
	endpoint := fmt.Sprintf("%s/actions/runs/%d", healthRepositoryEndpoint(collection.repoOverride), run.DatabaseID)
	if run.Attempt > 0 {
		endpoint += fmt.Sprintf("/attempts/%d", run.Attempt)
	}
	before := collection.APIRequests
	out, err := healthAPI(collection, path.Join(endpoint, "jobs")+"?per_page=1")
	collection.JobRequests += collection.APIRequests - before
	if err != nil {
		return false, fmt.Errorf("failed to check jobs for health run %d: %w", run.DatabaseID, err)
	}
	var data struct {
		Jobs []json.RawMessage `json:"jobs"`
	}
	if err := json.Unmarshal(out, &data); err != nil {
		return false, fmt.Errorf("failed to parse jobs for health run %d: %w", run.DatabaseID, err)
	}
	if data.Jobs == nil {
		return false, fmt.Errorf("failed to parse jobs for health run %d: missing jobs array", run.DatabaseID)
	}
	if collection.jobPresence == nil {
		collection.jobPresence = make(map[healthRunAttempt]bool)
	}
	hasJobs := len(data.Jobs) > 0
	collection.jobPresence[key] = hasJobs
	return hasJobs, nil
}

func fetchWorkflowRuns(workflowName, startDate, repoOverride string, verbose bool, collections ...*HealthCollection) ([]WorkflowRun, error) {
	collection := &HealthCollection{Host: normalizedGitHubAPIHost(logsRateLimitHost(repoOverride)), repoOverride: repoOverride}
	for _, supplied := range collections {
		collection = supplied
	}
	targets := collection.workflows
	if len(targets) == 0 {
		if collection.Partial || collection.MaximumDays > 0 {
			return nil, nil
		}
		targets = []GitHubWorkflow{{Name: workflowName, Path: workflowName}}
	}
	var allRuns []WorkflowRun
	if len(collection.workflows) > 0 {
		failures, err := collectHealthFailures(startDate, repoOverride, collection)
		allRuns = failures
		if err != nil {
			if collection.stopped {
				return allRuns, nil
			}
			return allRuns, err
		}
		targets = prioritizeHealthFailureWorkflows(targets, failures)
	}
	for _, target := range targets {
		runs, err := sampleHealthWorkflow(target, startDate, repoOverride, verbose, collection)
		allRuns = mergeHealthRunSamples(runs, allRuns)
		if err != nil {
			if collection.stopped || errors.Is(err, errHealthAPIBudget) {
				return allRuns, nil
			}
			return allRuns, err
		}
	}
	return allRuns, nil
}

func sampleHealthWorkflow(target GitHubWorkflow, startDate, repoOverride string, verbose bool, collection *HealthCollection) ([]WorkflowRun, error) {
	var oldest time.Time
	workflowID := path.Base(target.Path)
	if target.ID > 0 {
		workflowID = strconv.FormatInt(target.ID, 10)
	}
	opts := ListWorkflowRunsOptions{WorkflowName: workflowID, StartDate: startDate, EndDate: collection.EndDate, Limit: healthMinimumRuns * 2,
		RepoOverride: repoOverride, Verbose: verbose, OldestFetchedCreatedAt: &oldest}
	var sample []WorkflowRun
	seen := make(map[int64]struct{})
	sourceBoundaries := make(map[string]struct{})
	for page := 1; ; page++ {
		batch, total, listErr := healthListWorkflowRuns(opts, page, collection)
		if collection.visited == nil {
			collection.visited = make(map[string]struct{})
		}
		collection.visited[target.Path] = struct{}{}
		for _, run := range batch {
			if _, duplicate := seen[run.DatabaseID]; duplicate {
				continue
			}
			seen[run.DatabaseID] = struct{}{}
			if run.WorkflowPath == "" {
				run.WorkflowPath = target.Path
			}
			if run.WorkflowName == "" {
				run.WorkflowName = target.Name
			}
			scope := failureClusterKey(run.Repository, run.WorkflowPath, run.HeadBranch, run.Event)
			if _, stopped := sourceBoundaries[scope]; stopped {
				continue
			}
			if !run.StartedAt.IsZero() && !run.UpdatedAt.IsZero() {
				run.Duration = run.UpdatedAt.Sub(run.StartedAt)
			}
			if !collection.stopped {
				resolveHealthRevision(run, collection)
			}
			sample = append(sample, run)
			if healthSourceChanged(run, collection) {
				healthLog.Printf("Stopping health history for %s: workflow source changed on %s after run %d", run.WorkflowPath, run.HeadBranch, run.DatabaseID)
				sourceBoundaries[scope] = struct{}{}
			}
		}
		if listErr != nil {
			return sample, listErr
		}
		if collection.stopped {
			return sample, errHealthAPIBudget
		}
		if total < opts.Limit || (healthWorkflowEvidenceEnough(sample, collection) && healthWorkflowFailureScopesCovered(sample, target.Path, collection)) {
			return sample, nil
		}
		if page*opts.Limit >= healthMaximumRunSearchResults {
			if !advanceHealthRunWindow(&opts, oldest, collection) {
				return sample, nil
			}
			page = 0
		}
	}
}

func healthWorkflowFailureScopesCovered(runs []WorkflowRun, workflowPath string, collection *HealthCollection) bool {
	if collection == nil || len(collection.failureScopes[workflowPath]) == 0 {
		return true
	}
	covered := make(map[string]struct{})
	for _, status := range healthRevisionStatuses(runs, collection) {
		if status.Current.IndependentRuns >= healthMinimumRuns && len(runs) > 0 {
			covered[healthFailureSampleScope(WorkflowRun{
				Repository: runs[0].Repository, WorkflowPath: status.WorkflowPath,
				HeadBranch: status.Branch, Event: status.Event,
			})] = struct{}{}
		}
	}
	for scope := range collection.failureScopes[workflowPath] {
		if _, ok := covered[scope]; !ok {
			return false
		}
	}
	return true
}
