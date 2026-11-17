package cli

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/repoutil"
)

// EpisodeRetrievalOptions bounds a single canonical episode collection.
// MaxRuns counts distinct attempted candidates, not just matching episode members.
type EpisodeRetrievalOptions struct {
	SeedRunID         int64
	Repository        string
	AllowRepositories []string
	StartDate         string
	EndDate           string
	MaxRuns           int
	MaxAPIRequests    int
	Timeout           time.Duration
	OutputDir         string
	Refresh           bool
	Outcomes          bool
	Delivery          bool
}

// EpisodeRetrieval records collection bounds and evidence gaps, not completeness.
type EpisodeRetrieval struct {
	EpisodeID      string                  `json:"episode_id"`
	Host           string                  `json:"host"`
	Repositories   []string                `json:"repositories"`
	StartDate      time.Time               `json:"start_date"`
	EndDate        time.Time               `json:"end_date"`
	MaxRuns        int                     `json:"max_runs"`
	MaxAPIRequests int                     `json:"max_api_requests"`
	TimeoutSeconds float64                 `json:"timeout_seconds"`
	RunsExamined   int                     `json:"runs_examined"`
	APIRequests    int                     `json:"api_requests"`
	CollectedAt    time.Time               `json:"collected_at,omitzero"`
	Cached         bool                    `json:"cached,omitempty"`
	StopReasons    []string                `json:"stop_reasons,omitempty"`
	Issues         []EpisodeRetrievalIssue `json:"issues,omitempty"`
}

type EpisodeRetrievalIssue struct {
	Repository string `json:"repository"`
	RunID      int64  `json:"run_id,omitempty"`
	Reason     string `json:"reason"`
	Message    string `json:"message"`
}

type episodeCandidate struct {
	repository string
	runID      int64
}

type episodeCollector struct {
	opts      EpisodeRetrievalOptions
	report    EpisodeRetrieval
	request   episodeAPIRequest
	attempted map[episodeCandidate]struct{}
	seen      map[episodeCandidate]string
	pending   []episodeCandidate
	members   []ProcessedRun
}

var errEpisodeAPILimit = errors.New("episode API request limit reached")
var errEpisodeRunLimit = errors.New("episode candidate run limit reached")

// RetrieveEpisodeLogs collects canonical members using compact artifacts and the
// standard logs report builder. It does not evaluate outcomes or infer lineage.
func RetrieveEpisodeLogs(ctx context.Context, opts EpisodeRetrievalOptions) (LogsData, error) {
	return retrieveCachedEpisodeLogs(ctx, opts, requestEpisodeAPI)
}

func retrieveEpisodeLogs(ctx context.Context, opts EpisodeRetrievalOptions, request episodeAPIRequest) (LogsData, error) {
	collector := &episodeCollector{
		opts: opts, request: request, attempted: make(map[episodeCandidate]struct{}), seen: make(map[episodeCandidate]string),
	}
	if err := collector.initialize(time.Now()); err != nil {
		return LogsData{}, err
	}
	activeCtx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()
	seed := episodeCandidate{repository: collector.opts.Repository, runID: opts.SeedRunID}
	if err := collector.visit(activeCtx, seed, nil); err != nil {
		return LogsData{}, fmt.Errorf("retrieve seed run: %w", err)
	}
	if len(collector.members) == 0 {
		return LogsData{}, fmt.Errorf("seed run %d has no recorded context.episode_id; canonical episode retrieval requires compact info or usage artifacts", opts.SeedRunID)
	}
	collector.collect(activeCtx)
	if err := ctx.Err(); err != nil {
		return LogsData{}, err
	}
	if errors.Is(activeCtx.Err(), context.DeadlineExceeded) {
		collector.stop("timeout")
	}
	return collector.logsData(), nil
}

func (c *episodeCollector) initialize(now time.Time) error {
	if err := c.initializeScope(); err != nil {
		return err
	}
	start, end, err := episodeRetrievalDates(c.opts.StartDate, c.opts.EndDate, now)
	if err != nil {
		return err
	}
	c.report.StartDate, c.report.EndDate = start, end
	return nil
}

func (c *episodeCollector) initializeScope() error {
	if c.opts.SeedRunID <= 0 || c.opts.MaxRuns <= 0 || c.opts.MaxAPIRequests <= 0 || c.opts.Timeout <= 0 || c.opts.OutputDir == "" {
		return errors.New("episode retrieval requires a positive seed run ID, --max-runs, --max-api-requests, --timeout, and a nonempty --output")
	}
	repository, host := repoutil.NormalizeRepoForAPI(c.opts.Repository)
	if host == "" {
		host = strings.TrimPrefix(getGitHubHost(), "https://")
	}
	if !validEpisodeHost(host) || !validEpisodeRepository(repository) {
		return fmt.Errorf("invalid seed repository %q: expected [HOST/]owner/repo", c.opts.Repository)
	}
	c.opts.Repository = strings.ToLower(repository)
	c.report = EpisodeRetrieval{
		Host: strings.ToLower(host), Repositories: []string{strings.ToLower(repository)},
		MaxRuns: c.opts.MaxRuns, MaxAPIRequests: c.opts.MaxAPIRequests,
		TimeoutSeconds: c.opts.Timeout.Seconds(),
	}
	for _, allowed := range c.opts.AllowRepositories {
		if !validEpisodeRepository(allowed) {
			return fmt.Errorf("invalid --allow-repo %q: expected owner/repo on host %s", allowed, host)
		}
		allowed = strings.ToLower(allowed)
		if !slices.Contains(c.report.Repositories, allowed) {
			c.report.Repositories = append(c.report.Repositories, allowed)
		}
	}
	return nil
}

func episodeRetrievalDates(startDate, endDate string, now time.Time) (time.Time, time.Time, error) {
	if startDate == "" {
		startDate = now.AddDate(0, 0, -7).UTC().Format(time.RFC3339)
	}
	if endDate == "" {
		endDate = now.UTC().Format(time.RFC3339)
	}
	startDate, endDate, err := resolveLogsDateRange(startDate, endDate, now)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	start, err := parseFilterDate(startDate)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	end, err := parseFilterDate(endDate)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	if len(endDate) == len("2006-01-02") {
		end = end.AddDate(0, 0, 1).Add(-time.Second)
	}
	if start.After(end) {
		return time.Time{}, time.Time{}, errors.New("--start-date must not be after --end-date")
	}
	return start, end, nil
}

func validEpisodeRepository(repository string) bool {
	owner, repo, err := repoutil.SplitRepoSlug(repository)
	return err == nil && validEpisodePathSegment(owner) && validEpisodePathSegment(repo)
}

func validEpisodePathSegment(value string) bool {
	if value == "" || value == "." || value == ".." {
		return false
	}
	for _, r := range value {
		valid := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.'
		if !valid {
			return false
		}
	}
	return true
}

func validEpisodeHost(host string) bool {
	return validEpisodePathSegment(host) && !strings.HasPrefix(host, "-")
}

func (c *episodeCollector) collect(ctx context.Context) {
	if !c.drainParents(ctx) {
		return
	}
	for _, repository := range c.report.Repositories {
		for page := 1; ; page++ {
			runs, err := c.listRuns(ctx, repository, page)
			if c.handleStop(err) {
				return
			}
			if err != nil {
				c.issue(episodeCandidate{repository: repository}, "discovery_failed", err)
				c.stop("discovery_failed")
				break
			}
			for _, run := range runs {
				candidate := episodeCandidate{repository: repository, runID: run.DatabaseID}
				if err := c.visit(ctx, candidate, &run); c.handleStop(err) {
					return
				} else if err != nil {
					c.issue(candidate, "candidate_unavailable", err)
				}
				if !c.drainParents(ctx) {
					return
				}
			}
			if len(runs) < 100 || page == 10 {
				break
			}
		}
	}
}

func (c *episodeCollector) drainParents(ctx context.Context) bool {
	for {
		var candidate episodeCandidate
		if len(c.pending) > 0 {
			candidate = c.pending[0]
			c.pending = c.pending[1:]
		} else {
			return true
		}
		err := c.visit(ctx, candidate, nil)
		if c.handleStop(err) {
			return false
		}
		if err != nil {
			c.issue(candidate, "recorded_run_unavailable", err)
		}
	}
}

func (c *episodeCollector) visit(ctx context.Context, candidate episodeCandidate, run *WorkflowRun) error {
	recorded := run == nil && c.report.EpisodeID != ""
	if episodeID, seen := c.seen[candidate]; seen {
		if recorded && episodeID != c.report.EpisodeID {
			return errors.New("recorded run has no matching canonical episode identity")
		}
		return nil
	}
	if _, attempted := c.attempted[candidate]; !attempted {
		if c.report.RunsExamined >= c.opts.MaxRuns {
			return errEpisodeRunLimit
		}
		c.attempted[candidate] = struct{}{}
		c.report.RunsExamined++
	}
	run, err := c.resolveCandidate(ctx, candidate, run)
	if err != nil {
		return err
	}
	processed, err := c.loadRun(ctx, *run)
	if err != nil {
		return err
	}
	c.seen[candidate] = ""
	if processed.AwContext == nil || strings.TrimSpace(processed.AwContext.EpisodeID) == "" {
		return fmt.Errorf("run %d has no recorded context.episode_id", run.DatabaseID)
	}
	episodeID := strings.TrimSpace(processed.AwContext.EpisodeID)
	c.seen[candidate] = episodeID
	if c.report.EpisodeID == "" {
		c.report.EpisodeID = episodeID
	}
	if episodeID != c.report.EpisodeID {
		if recorded {
			return errors.New("recorded run belongs to a different canonical episode")
		}
		return nil
	}
	c.members = append(c.members, processed)
	if run.Status != "completed" {
		c.issue(candidate, "run_not_completed", errors.New("compact artifacts may be incomplete until the run completes"))
	}
	return nil
}

func (c *episodeCollector) resolveCandidate(ctx context.Context, candidate episodeCandidate, run *WorkflowRun) (*WorkflowRun, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if run == nil {
		fetched, err := c.getRun(ctx, candidate)
		if err != nil {
			return nil, err
		}
		run = &fetched
	}
	if run.DatabaseID <= 0 || run.Attempt <= 0 || run.DatabaseID != candidate.runID || !strings.EqualFold(run.Repository, candidate.repository) {
		return nil, errors.New("run metadata does not match requested repository/run identity and attempt")
	}
	if run.CreatedAt.Before(c.report.StartDate) || run.CreatedAt.After(c.report.EndDate) {
		return nil, fmt.Errorf("run %d is outside the retrieval time window", run.DatabaseID)
	}
	return run, nil
}

func (c *episodeCollector) enqueueRecorded(repository, id string, source episodeCandidate) {
	if id == "" {
		return
	}
	runID, valid := parseEpisodeRunID(id)
	candidate := episodeCandidate{repository: strings.ToLower(repository), runID: runID}
	if !valid {
		c.issue(source, "invalid_recorded_run_id", fmt.Errorf("recorded run ID %q is not a positive integer", id))
		return
	}
	if runID == source.runID && (repository == "" || strings.EqualFold(repository, source.repository)) {
		return
	}
	if !slices.Contains(c.report.Repositories, candidate.repository) {
		c.issue(candidate, "repository_not_allowed", fmt.Errorf("recorded repository %q is outside the explicit retrieval allowlist", repository))
		return
	}
	if episodeID, seen := c.seen[candidate]; seen {
		if episodeID != c.report.EpisodeID && c.report.EpisodeID != "" {
			c.issue(candidate, "conflicting_recorded_identity", errors.New("previously examined recorded run has no matching canonical episode identity"))
		}
	} else if !slices.Contains(c.pending, candidate) {
		c.pending = append(c.pending, candidate)
	}
}

func (c *episodeCollector) handleStop(err error) bool {
	switch {
	case errors.Is(err, errEpisodeAPILimit):
		c.stop("api_request_limit")
	case errors.Is(err, errEpisodeRunLimit):
		c.stop("run_limit")
	case errors.Is(err, context.DeadlineExceeded):
		c.stop("timeout")
	case errors.Is(err, context.Canceled):
		c.stop("cancelled")
	default:
		return false
	}
	return true
}

func (c *episodeCollector) stop(reason string) {
	if !slices.Contains(c.report.StopReasons, reason) {
		c.report.StopReasons = append(c.report.StopReasons, reason)
	}
}

func (c *episodeCollector) issue(candidate episodeCandidate, reason string, err error) {
	c.report.Issues = append(c.report.Issues, EpisodeRetrievalIssue{
		Repository: candidate.repository, RunID: candidate.runID, Reason: reason, Message: err.Error(),
	})
}

func episodeRetrievalCoverageReason(reason string) string {
	return "retrieval_" + reason
}

func (c *episodeCollector) logsData() LogsData {
	slices.SortFunc(c.members, func(a, b ProcessedRun) int { return b.Run.CreatedAt.Compare(a.Run.CreatedAt) })
	data := buildLogsDataWithCollectionCoverage(c.members, c.opts.OutputDir, nil, logsCollectionCoverageOptions{disableLocalClassification: true})
	data.EpisodeRetrieval = &c.report
	for i := range data.Episodes {
		episode := &data.Episodes[i]
		for _, reason := range c.report.StopReasons {
			episode.Coverage.addGap(episodeRetrievalCoverageReason(reason))
			episode.Coverage.CollectionTruncated = true
		}
		for _, issue := range c.report.Issues {
			episode.Coverage.addGap(episodeRetrievalCoverageReason(issue.Reason))
		}
	}
	data.Message = "Canonical episode retrieval is bounded to the reported repositories and time window; membership coverage is not complete."
	return data
}

func (c *episodeCollector) runListEndpoint(repository string, page int) string {
	values := url.Values{
		"created":  {c.report.StartDate.UTC().Format(time.RFC3339) + ".." + c.report.EndDate.UTC().Format(time.RFC3339)},
		"per_page": {"100"},
		"page":     {strconv.Itoa(page)},
	}
	return fmt.Sprintf("repos/%s/actions/runs?%s", repository, values.Encode())
}
