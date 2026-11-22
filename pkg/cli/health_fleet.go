package cli

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/repoutil"
)

type healthFleetScope struct {
	Repository string    `json:"repository"`
	Branch     string    `json:"branch"`
	HeadSHA    string    `json:"head_sha"`
	Start      time.Time `json:"start_date"`
	End        time.Time `json:"end_date"`
}

type healthFleetWindow struct {
	Start int64 `json:"start"`
	End   int64 `json:"end"`
	Page  int   `json:"page"`
}

type healthFleetWorkflowEvidence struct {
	Health         WorkflowHealth `json:"health"`
	Failures       []RunData      `json:"failures,omitempty"`
	IncompleteJobs bool           `json:"incomplete_jobs,omitempty"`
}

type healthFleetState struct {
	Version          int                                    `json:"version"`
	Scope            healthFleetScope                       `json:"scope"`
	Threshold        float64                                `json:"threshold"`
	RegistryPage     int                                    `json:"registry_page"`
	Workflows        []GitHubWorkflow                       `json:"workflows"`
	Windows          []healthFleetWindow                    `json:"windows"`
	UnresolvedSlices int                                    `json:"unresolved_slices"`
	Runs             map[int64]WorkflowRun                  `json:"runs"`
	Verified         map[string]healthFleetWorkflowEvidence `json:"verified"`
	Responses        map[string][]byte                      `json:"responses"`
}

func runHealthFleet(ctx context.Context, config HealthConfig, start, end, started time.Time) error {
	if config.WorkflowName != "" {
		return errors.New("--fleet cannot be combined with a workflow name")
	}
	repo, host := repoutil.NormalizeRepoForAPI(config.RepoOverride)
	if config.RepoOverride != "" && !validHealthFleetRepository(host, repo) {
		return errors.New("--repo must be [HOST/]OWNER/REPO")
	}
	host = normalizedGitHubAPIHost(host)
	state, err := loadHealthFleetCheckpoint(config, host, repo, start, end)
	if err != nil {
		return err
	}
	config.StartDate, config.EndDate = state.Scope.Start.Format(time.RFC3339Nano), state.Scope.End.Format(time.RFC3339Nano)
	config.Threshold = state.Threshold
	collection := newHealthCollection(ctx, config, state.Scope.Start, state.Scope.End)
	config.collection = collection
	collection.Host, collection.startedAt = host, started
	collection.MinimumRuns = healthMinimumRuns
	collection.fleetResponses = state.Responses
	collection.repoOverride = config.RepoOverride
	if err := collectHealthFleet(state, config); err != nil && !collection.stopped {
		return err
	}
	if config.Checkpoint != "" {
		if err := saveHealthFleetCheckpoint(config.Checkpoint, state); err != nil {
			return err
		}
	}
	return renderHealthFleet(state, config)
}

func validHealthFleetRepository(host, repo string) bool {
	owner, name, found := strings.Cut(repo, "/")
	return found && localRepoSlugPattern.MatchString(repo) && owner != "." && owner != ".." && name != "." && name != ".." &&
		(host == "" || healthFleetHostPattern.MatchString(host))
}

var healthFleetHostPattern = regexp.MustCompile(`^[a-zA-Z0-9]+(?:[a-zA-Z0-9-]*[a-zA-Z0-9])?(?:\.[a-zA-Z0-9]+(?:[a-zA-Z0-9-]*[a-zA-Z0-9])?)+$`)

func validHealthFleetPath(file string) bool {
	return path.Dir(file) == constants.WorkflowsDir && path.Clean(file) == file && strings.HasSuffix(file, ".lock.yml")
}

func collectHealthFleet(state *healthFleetState, config HealthConfig) error {
	collection := config.collection
	if state.Scope.Branch == "" {
		var repo struct {
			FullName      string `json:"full_name"`
			DefaultBranch string `json:"default_branch"`
		}
		if err := healthFleetAPI(collection, healthRepositoryEndpoint(collection.repoOverride), &repo, "{full_name,default_branch}"); err != nil {
			return err
		}
		if !validHealthFleetRepository(collection.Host, repo.FullName) || repo.DefaultBranch == "" {
			return errors.New("health fleet repository response is missing its identity or default branch")
		}
		state.Scope.Branch = repo.DefaultBranch
		state.Scope.Repository = path.Join(collection.Host, repo.FullName)
		clear(state.Responses)
	}
	collection.repoOverride = state.Scope.Repository
	config.RepoOverride = state.Scope.Repository
	collection.scanBranch, collection.DefaultBranch = state.Scope.Branch, state.Scope.Branch
	if state.Scope.HeadSHA == "" {
		endpoint := path.Join(healthRepositoryEndpoint(collection.repoOverride), "git/ref/heads", url.PathEscape(state.Scope.Branch))
		out, err := healthAPI(collection, endpoint, "--jq", ".object.sha")
		if err != nil {
			return err
		}
		if sha := strings.TrimSpace(string(out)); validHealthBlobSHA(sha) {
			state.Scope.HeadSHA = sha
		} else {
			return errors.New("health fleet default branch response is missing its commit SHA")
		}
	}
	collection.pinnedBranchSHA = state.Scope.HeadSHA
	if err := collectHealthFleetRegistry(state, collection); err != nil {
		return err
	}
	if err := collectHealthFleetInventory(state, collection); err != nil {
		return err
	}
	if state.UnresolvedSlices != 0 {
		collection.Partial, collection.Reason = true, "pagination_limit"
		return nil
	}
	return verifyHealthFleet(state, config)
}

func collectHealthFleetRegistry(state *healthFleetState, collection *HealthCollection) error {
	for state.RegistryPage > 0 {
		endpoint := fmt.Sprintf("%s/actions/workflows?per_page=100&page=%d",
			healthRepositoryEndpoint(collection.repoOverride), state.RegistryPage)
		var payload struct {
			Workflows []GitHubWorkflow `json:"workflows"`
		}
		if err := healthFleetAPI(collection, endpoint, &payload, `{workflows: [.workflows[] | {id,name,path,state}]}`); err != nil {
			return err
		}
		delete(state.Responses, endpoint+"\x00--jq\x00"+`{workflows: [.workflows[] | {id,name,path,state}]}`)
		if payload.Workflows == nil {
			return errors.New("health fleet registry response did not contain workflows")
		}
		for _, workflow := range payload.Workflows {
			if workflow.State == "active" && validHealthFleetPath(workflow.Path) {
				if !slices.ContainsFunc(state.Workflows, func(previous GitHubWorkflow) bool { return previous.Path == workflow.Path }) {
					state.Workflows = append(state.Workflows, workflow)
				}
			}
		}
		if len(payload.Workflows) < 100 {
			state.RegistryPage = 0
			break
		}
		state.RegistryPage++
	}
	collection.workflows = state.Workflows
	return nil
}

func collectHealthFleetInventory(state *healthFleetState, collection *HealthCollection) error {
	paths := make(map[string]string, len(state.Workflows))
	for _, workflow := range state.Workflows {
		paths[workflow.Path] = workflow.Name
	}
	for len(state.Windows) > 0 {
		window := state.Windows[0] //nolint:uncheckedsliceindex // guarded by the loop condition
		dates := time.Unix(window.Start, 0).UTC().Format(time.RFC3339) + ".." + time.Unix(window.End, 0).UTC().Format(time.RFC3339)
		endpoint := fmt.Sprintf("%s/actions/runs?branch=%s&created=%s&per_page=100&page=%d",
			healthRepositoryEndpoint(collection.repoOverride), url.QueryEscape(state.Scope.Branch), url.QueryEscape(dates), window.Page)
		var payload struct {
			Total *int                     `json:"total_count"`
			Runs  []workflowRunAPIResponse `json:"workflow_runs"`
		}
		if err := healthFleetAPI(collection, endpoint, &payload, healthFleetRunProjection); err != nil {
			return err
		}
		if payload.Total == nil || payload.Runs == nil {
			return errors.New("health fleet run response did not contain total_count and workflow_runs")
		}
		for _, raw := range payload.Runs {
			name, registered := paths[raw.Path]
			run := raw.workflowRun()
			if !registered || run.DatabaseID <= 0 || !healthRunInScan(run, collection) ||
				run.CreatedAt.Before(state.Scope.Start) || run.CreatedAt.After(state.Scope.End) {
				continue
			}
			if _, seen := state.Runs[run.DatabaseID]; !seen {
				run.WorkflowName = name
				run.Repository, _ = repoutil.NormalizeRepoForAPI(state.Scope.Repository)
				run.URL = fmt.Sprintf("https://%s/%s/actions/runs/%d", collection.Host, run.Repository, run.DatabaseID)
				state.Runs[run.DatabaseID] = run
			}
		}
		state.advanceWindow(window, *payload.Total, len(payload.Runs))
		delete(state.Responses, endpoint+"\x00--jq\x00"+healthFleetRunProjection)
	}
	return nil
}

const healthFleetRunProjection = `{total_count, workflow_runs: [.workflow_runs[] |
  {id,run_attempt,status,conclusion,path,created_at,run_started_at,updated_at,event,head_branch,head_sha}]}`

func (state *healthFleetState) advanceWindow(window healthFleetWindow, total, count int) {
	if len(state.Windows) == 0 {
		return
	}
	switch {
	case total >= healthMaximumRunSearchResults && window.Start < window.End:
		mid := window.Start + (window.End-window.Start)/2
		state.Windows = append([]healthFleetWindow{{Start: mid + 1, End: window.End, Page: 1},
			{Start: window.Start, End: mid, Page: 1}}, state.Windows[1:]...)
	case count == 100 && window.Page*100 < healthMaximumRunSearchResults:
		state.Windows[0].Page++ //nolint:uncheckedsliceindex // guarded by the length check above
	default:
		if total >= healthMaximumRunSearchResults || (count < 100 && total > (window.Page-1)*100+count) {
			state.UnresolvedSlices++
		}
		state.Windows = state.Windows[1:]
	}
}

func healthFleetAPI(collection *HealthCollection, endpoint string, dst any, projection string) error {
	out, err := healthAPI(collection, endpoint, "--jq", projection)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(out, dst); err != nil {
		return fmt.Errorf("invalid health fleet API response: %w", err)
	}
	return nil
}

func (state *healthFleetState) groupedRuns() map[string][]WorkflowRun {
	grouped := make(map[string][]WorkflowRun)
	for _, run := range state.Runs {
		grouped[run.WorkflowPath] = append(grouped[run.WorkflowPath], run)
	}
	for _, runs := range grouped {
		slices.SortFunc(runs, func(a, b WorkflowRun) int {
			return cmp.Or(b.CreatedAt.Compare(a.CreatedAt), cmp.Compare(b.DatabaseID, a.DatabaseID))
		})
	}
	return grouped
}

func verifyHealthFleet(state *healthFleetState, config HealthConfig) error {
	grouped := state.groupedRuns()
	registry := slices.Clone(state.Workflows)
	failures := make(map[string]int, len(grouped))
	for path, runs := range grouped {
		for _, run := range runs {
			if isClusterFailureConclusion(run.Conclusion) {
				failures[path]++
			}
		}
	}
	slices.SortFunc(registry, func(a, b GitHubWorkflow) int {
		return cmp.Or(cmp.Compare(failures[b.Path], failures[a.Path]), cmp.Compare(a.Path, b.Path))
	})
	for _, target := range registry {
		if _, done := state.Verified[target.Path]; done {
			continue
		}
		if err := checkHealthTimeBudget(config.collection); err != nil {
			return err
		}
		evidence, err := verifyHealthFleetWorkflow(target, grouped[target.Path], config)
		if err != nil {
			return err
		}
		state.Verified[target.Path] = evidence
	}
	return nil
}
