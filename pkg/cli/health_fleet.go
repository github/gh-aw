package cli

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/repoutil"
	"github.com/github/gh-aw/pkg/workflow"
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
	Health              WorkflowHealth                `json:"health"`
	Failures            []RunData                     `json:"failures,omitempty"`
	IncompleteJobs      bool                          `json:"incomplete_jobs,omitempty"`
	IncompleteRevisions bool                          `json:"incomplete_revisions,omitempty"`
	CacheEvidence       map[int64]HealthCacheEvidence `json:"cache_evidence,omitempty"`
}

type healthFleetState struct {
	Version          int                                    `json:"version"`
	Scope            healthFleetScope                       `json:"scope"`
	Threshold        float64                                `json:"threshold"`
	LogsDir          string                                 `json:"logs_dir,omitempty"`
	RegistryPage     int                                    `json:"registry_page"`
	RegistryDrift    bool                                   `json:"registry_drift,omitempty"`
	RegistryIssues   map[string]string                      `json:"registry_issues,omitempty"`
	RegistrySeen     map[string]bool                        `json:"registry_seen,omitempty"`
	Workflows        []GitHubWorkflow                       `json:"workflows"`
	Windows          []healthFleetWindow                    `json:"windows"`
	UnresolvedSlices int                                    `json:"unresolved_slices"`
	Runs             map[int64]WorkflowRun                  `json:"runs"`
	Verified         map[string]healthFleetWorkflowEvidence `json:"verified"`
	Responses        map[string][]byte                      `json:"responses"`
}

func runHealthFleet(ctx context.Context, config HealthConfig, start, end, started time.Time) error {
	repo, host := repoutil.NormalizeRepoForAPI(config.RepoOverride)
	if config.RepoOverride != "" && !validHealthFleetRepository(host, repo) {
		return errors.New("--repo must be [HOST/]OWNER/REPO")
	}
	host = normalizedGitHubAPIHost(host)
	if config.LogsDir == "" {
		config.LogsDir = defaultLogsOutputDir
	}
	logsDir, err := filepath.Abs(config.LogsDir)
	if err != nil {
		return fmt.Errorf("resolve health logs directory: %w", err)
	}
	config.LogsDir = logsDir
	state, err := loadHealthFleetCheckpoint(config, host, repo, start, end)
	if err != nil {
		return err
	}
	config.StartDate, config.EndDate = state.Scope.Start.Format(time.RFC3339Nano), state.Scope.End.Format(time.RFC3339Nano)
	config.Threshold = state.Threshold
	if state.LogsDir != "" {
		savedLogsDir, err := filepath.Abs(state.LogsDir)
		if err != nil {
			return fmt.Errorf("resolve checkpoint logs directory: %w", err)
		}
		if config.inputLogsDir && config.LogsDir != savedLogsDir {
			return errors.New("health checkpoint logs directory does not match --logs-dir; omit the flag to resume")
		}
		config.LogsDir = savedLogsDir
	}
	if state.Scope.Repository != "" {
		_, host = repoutil.NormalizeRepoForAPI(state.Scope.Repository)
		host = normalizedGitHubAPIHost(host)
	}
	collection := newHealthCollection(ctx, config, state.Scope.Start, state.Scope.End)
	config.collection = collection
	collection.Host, collection.startedAt = host, started
	collection.MinimumRuns = healthMinimumRuns
	collection.fleetResponses = state.Responses
	collection.repoOverride = config.RepoOverride
	collectErr := collectHealthFleet(state, config)
	if config.Checkpoint != "" {
		evictInvalidHealthFleetResponses(state.Responses)
		if err := saveHealthFleetCheckpoint(config.Checkpoint, state); err != nil {
			return errors.Join(collectErr, err)
		}
	}
	if collectErr != nil && !collection.stopped {
		return collectErr
	}
	return renderHealthFleet(state, config)
}

func validHealthFleetRepository(host, repo string) bool {
	owner, name, found := strings.Cut(repo, "/")
	return found && localRepoSlugPattern.MatchString(repo) && owner != "." && owner != ".." && name != "." && name != ".." &&
		(host == "" || healthFleetHostPattern.MatchString(host))
}

var healthFleetHostPattern = regexp.MustCompile(`^[a-zA-Z0-9]+(?:[a-zA-Z0-9-]*[a-zA-Z0-9])?(?:\.[a-zA-Z0-9]+(?:[a-zA-Z0-9-]*[a-zA-Z0-9])?)*(?::[0-9]+)?$`)

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
			delete(state.Responses, healthRepositoryEndpoint(collection.repoOverride)+"\x00--jq\x00{full_name,default_branch}")
			return errors.New("health fleet repository response is missing its identity or default branch")
		}
		state.Scope.Branch = repo.DefaultBranch
		state.Scope.Repository = path.Join(collection.Host, repo.FullName)
		clear(state.Responses)
	}
	collection.repoOverride = state.Scope.Repository
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
			delete(state.Responses, endpoint+"\x00--jq\x00.object.sha")
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
	snapshot := healthWorkflowFiles(state.Scope.Branch, collection)
	if snapshot.err != nil {
		return snapshot.err
	}
	if state.RegistrySeen == nil {
		state.RegistrySeen = make(map[string]bool)
		for _, workflow := range state.Workflows {
			state.RegistrySeen[workflow.Path] = true
		}
	}
	state.Workflows = slices.DeleteFunc(state.Workflows, func(w GitHubWorkflow) bool {
		if _, exists := snapshot.files[w.Path]; !exists {
			state.flagRegistryIssue(w.Path, "active_registry_only")
			delete(state.Verified, w.Path)
			return true
		}
		return false
	})
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
		if err := state.compareRegistryPage(payload.Workflows, snapshot.files, collection); err != nil {
			return err
		}
		if len(payload.Workflows) < 100 {
			state.RegistryPage = 0
			break
		}
		state.RegistryPage++
	}
	if state.RegistryPage == 0 {
		for file := range snapshot.files {
			if validHealthFleetPath(file) && !state.RegistrySeen[file] {
				state.flagRegistryIssue(file, "missing_registry_entry")
			}
		}
	}
	collection.workflows = state.Workflows
	return nil
}

func (state *healthFleetState) compareRegistryPage(workflows []GitHubWorkflow, files map[string]string, collection *HealthCollection) error {
	var exclusions []string
	for _, workflow := range workflows {
		file := workflow.Path
		if !validHealthFleetPath(file) {
			continue
		}
		state.RegistrySeen[file] = true
		if workflow.State != "active" {
			continue
		}
		if _, exists := files[file]; exists {
			if !slices.ContainsFunc(state.Workflows, func(previous GitHubWorkflow) bool { return previous.Path == file }) {
				state.Workflows = append(state.Workflows, workflow)
			}
			continue
		}
		if _, checked := state.RegistryIssues[file]; checked {
			continue
		}
		outsideScope, err := healthFleetRegistryEntryOutsideScope(state.Scope, workflow, collection)
		if err != nil {
			if collection.stopped {
				return err
			}
			healthLog.Printf("Workflow registry evidence unavailable for %q", file)
		}
		if outsideScope {
			exclusions = append(exclusions, file)
		} else {
			state.flagRegistryIssue(file, "active_registry_only")
		}
	}
	if len(exclusions) > 0 {
		// Check after collecting the page's evidence: an addition at a newer
		// head may not have any runs yet, so the pinned history is insufficient.
		head, err := healthFleetRegistryHead(state.Scope, collection)
		if err != nil && collection.stopped {
			return err
		}
		if head != state.Scope.HeadSHA {
			for _, file := range exclusions {
				state.flagRegistryIssue(file, "active_registry_only")
			}
		}
	}
	return nil
}

func (state *healthFleetState) flagRegistryIssue(file, reason string) {
	state.RegistryDrift = true
	if state.RegistryIssues == nil {
		state.RegistryIssues = make(map[string]string)
	}
	state.RegistryIssues[file] = reason
}

func healthFleetRegistryEntryOutsideScope(scope healthFleetScope, workflow GitHubWorkflow, collection *HealthCollection) (bool, error) {
	if !validHealthBlobSHA(scope.HeadSHA) || scope.Start.IsZero() {
		return false, nil
	}
	commits := path.Join(healthRepositoryEndpoint(collection.repoOverride), "commits")
	query := url.Values{"sha": {scope.HeadSHA}, "path": {workflow.Path}, "per_page": {"1"}}
	var history []struct {
		SHA string `json:"sha"`
	}
	if err := healthFleetAPI(collection, commits+"?"+query.Encode(), &history, `if type == "array" then map({sha}) else null end`); err != nil {
		return false, err
	}
	if history != nil && len(history) == 0 {
		// Actions can register workflows from non-default branches. Empty pinned
		// history alone is insufficient; corroborate it with branch run inventory.
		return healthFleetHasNoBranchRuns(scope, workflow, collection)
	}
	if len(history) != 1 {
		return false, nil
	}
	var sha string
	for _, entry := range history {
		sha = entry.SHA
	}
	if !validHealthBlobSHA(sha) {
		return false, nil
	}
	var commit struct {
		SHA     string    `json:"sha"`
		Date    time.Time `json:"date"`
		Removed []string  `json:"removed"`
	}
	// With path history, require an explicit deletion in the latest path commit.
	if err := healthFleetAPI(collection, path.Join(commits, sha), &commit, `{sha,date:.commit.committer.date,removed:[.files[] | select(.status=="removed") | .filename]}`); err != nil {
		return false, err
	}
	return commit.SHA == sha && !commit.Date.IsZero() && commit.Date.Before(scope.Start) &&
		slices.Contains(commit.Removed, workflow.Path), nil
}

func healthFleetHasNoBranchRuns(scope healthFleetScope, workflow GitHubWorkflow, collection *HealthCollection) (bool, error) {
	if workflow.ID <= 0 {
		return false, nil
	}
	query := url.Values{"branch": {scope.Branch}, "per_page": {"1"}}
	endpoint := fmt.Sprintf("%s/actions/workflows/%d/runs?%s", healthRepositoryEndpoint(collection.repoOverride), workflow.ID, query.Encode())
	const projection = `{total_count,workflow_runs}`
	// Run evidence, like the registry, must be fresh after resuming.
	defer delete(collection.fleetResponses, endpoint+"\x00--jq\x00"+projection)
	var runs struct {
		Total *int              `json:"total_count"`
		Runs  []json.RawMessage `json:"workflow_runs"`
	}
	if err := healthFleetAPI(collection, endpoint, &runs, projection); err != nil {
		return false, err
	}
	return runs.Total != nil && *runs.Total == 0 && runs.Runs != nil && len(runs.Runs) == 0, nil
}

func healthFleetRegistryHead(scope healthFleetScope, collection *HealthCollection) (string, error) {
	endpoint := path.Join(healthRepositoryEndpoint(collection.repoOverride), "git/ref/heads", url.PathEscape(scope.Branch))
	const projection = `{sha:.object.sha}`
	// Unlike the pin, this observation must not be reused across invocations.
	defer delete(collection.fleetResponses, endpoint+"\x00--jq\x00"+projection)
	var ref struct {
		SHA string `json:"sha"`
	}
	if err := healthFleetAPI(collection, endpoint, &ref, projection); err != nil {
		return "", err
	}
	if !validHealthBlobSHA(ref.SHA) {
		return "", errors.New("health fleet registry comparison could not determine the live branch SHA")
	}
	return ref.SHA, nil
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
		delete(state.Responses, endpoint+"\x00--jq\x00"+healthFleetRunProjection)
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
		delete(collection.fleetResponses, endpoint+"\x00--jq\x00"+projection)
		return fmt.Errorf("invalid health fleet API response: %w", err)
	}
	return nil
}

// Job responses are reused across resumptions, including optional failed reads.
func evictInvalidHealthFleetResponses(responses map[string][]byte) {
	for key, body := range responses {
		endpoint, extra, _ := strings.Cut(key, "\x00")
		switch {
		case extra == "--jq\x00.object.sha" || extra == "--jq\x00.sha":
			if !validHealthBlobSHA(strings.TrimSpace(string(body))) {
				delete(responses, key)
			}
		case extra == "--jq\x00"+`map({path,sha,type})`:
			if _, err := parseHealthWorkflowFiles(body); err != nil {
				delete(responses, key)
			}
		case strings.Contains(endpoint, "/jobs?"):
			var payload struct {
				Jobs       []JobInfoWithDuration `json:"jobs"`
				TotalCount *int                  `json:"total_count"`
			}
			if json.Unmarshal(body, &payload) != nil || payload.Jobs == nil {
				delete(responses, key)
			}
		case (strings.Contains(endpoint, "/git/blobs/") || strings.Contains(endpoint, ".lock.yml?")) &&
			strings.HasPrefix(extra, "--jq\x00.content | @base64d"):
			header, _, _ := strings.Cut(string(body), "\n")
			metadata, _, err := workflow.ExtractMetadataFromLockFile(header)
			if err != nil || metadata == nil || metadata.FrontmatterHash == "" || metadata.BodyHash == "" {
				delete(responses, key)
			}
		}
	}
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
		if previous, done := state.Verified[target.Path]; done && !previous.IncompleteJobs {
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
