package cli

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/workflow"
	"golang.org/x/sync/errgroup"
)

type operationalValueGitHubWorkflowRun struct {
	ID         int64     `json:"id"`
	RunAttempt int       `json:"run_attempt"`
	HTMLURL    string    `json:"html_url"`
	Status     string    `json:"status"`
	Conclusion string    `json:"conclusion"`
	CreatedAt  time.Time `json:"created_at"`
	Event      string    `json:"event"`
	HeadBranch string    `json:"head_branch"`
	HeadSHA    string    `json:"head_sha"`
}

type operationalValueGitHubWorkflowRunsPage struct {
	TotalCount   int                                 `json:"total_count"`
	WorkflowRuns []operationalValueGitHubWorkflowRun `json:"workflow_runs"`
}

var operationalValueReportListRuns = listOperationalValueReportRuns
var operationalValueReportGradeRun = gradeOperationalValueReportRun
var operationalValueReportRunGH = workflow.RunGHCombinedContext

const operationalValueReportCreatedSearchCap = 1000
const defaultOperationalValueReportConcurrency = 8

func listOperationalValueReportRuns(ctx context.Context, repository, hostname, workflowFile string, startAt, endAt time.Time) ([]operationalValueReportRun, error) {
	startAt = startAt.UTC().Truncate(time.Second)
	endAt = endAt.UTC().Truncate(time.Second)
	runs := make([]operationalValueReportRun, 0)
	seen := make(map[string]struct{})
	if err := collectOperationalValueReportRuns(ctx, repository, hostname, workflowFile, startAt, endAt, &runs, seen); err != nil {
		return nil, err
	}
	slices.SortFunc(runs, func(left, right operationalValueReportRun) int {
		if operationalValueReportRunLess(left, right) {
			return -1
		}
		if operationalValueReportRunLess(right, left) {
			return 1
		}
		return 0
	})
	return runs, nil
}

func collectOperationalValueReportRuns(ctx context.Context, repository, hostname, workflowFile string, startAt, endAt time.Time, runs *[]operationalValueReportRun, seen map[string]struct{}) error {
	pages, totalCount, err := fetchOperationalValueReportRunsRange(ctx, repository, hostname, workflowFile, startAt, endAt)
	if err != nil {
		return err
	}
	if totalCount >= operationalValueReportCreatedSearchCap {
		if !startAt.Before(endAt) {
			return fmt.Errorf("cannot enumerate complete operational-value history: more than %d runs share created_at=%s", operationalValueReportCreatedSearchCap, startAt.Format(time.RFC3339))
		}
		mid := time.Unix((startAt.Unix()+endAt.Unix())/2, 0).UTC()
		if err := collectOperationalValueReportRuns(ctx, repository, hostname, workflowFile, startAt, mid, runs, seen); err != nil {
			return err
		}
		nextStart := mid
		if nextStart.Equal(startAt) {
			nextStart = nextStart.Add(time.Second)
		}
		if nextStart.After(endAt) {
			return nil
		}
		return collectOperationalValueReportRuns(ctx, repository, hostname, workflowFile, nextStart, endAt, runs, seen)
	}
	for _, page := range pages {
		for _, run := range page.WorkflowRuns {
			if run.Status != "completed" || run.ID <= 0 || run.CreatedAt.Before(startAt) || run.CreatedAt.After(endAt) {
				continue
			}
			attempt := run.RunAttempt
			if attempt <= 0 {
				attempt = 1
			}
			key := strconv.FormatInt(run.ID, 10) + ":" + strconv.Itoa(attempt)
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			ref := run.HeadBranch
			if ref != "" && !strings.HasPrefix(ref, "refs/") {
				ref = "refs/heads/" + ref
			}
			*runs = append(*runs, operationalValueReportRun{
				ID:         strconv.FormatInt(run.ID, 10),
				Attempt:    attempt,
				CreatedAt:  run.CreatedAt.UTC(),
				Conclusion: run.Conclusion,
				URL:        run.HTMLURL,
				SHA:        run.HeadSHA,
				Ref:        ref,
				EventName:  run.Event,
			})
		}
	}
	return nil
}

func fetchOperationalValueReportRunsRange(ctx context.Context, repository, hostname, workflowFile string, startAt, endAt time.Time) ([]operationalValueGitHubWorkflowRunsPage, int, error) {
	endpoint := fmt.Sprintf("repos/%s/actions/workflows/%s/runs", repository, url.PathEscape(workflowFile))
	args := []string{"api"}
	if hostname != "" && hostname != "github.com" {
		args = append(args, "--hostname", hostname)
	}
	args = append(args,
		"--method", "GET",
		"--paginate",
		"--slurp",
		"-f", "per_page=100",
		"-f", "created="+startAt.UTC().Format(time.RFC3339)+".."+endAt.UTC().Format(time.RFC3339),
		endpoint,
	)
	output, err := operationalValueReportRunGH(ctx, "Fetching operational-value history...", args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list workflow runs for operational-value report: %w", err)
	}
	var pages []operationalValueGitHubWorkflowRunsPage
	if err := json.Unmarshal(output, &pages); err != nil {
		return nil, 0, fmt.Errorf("failed to parse workflow runs for operational-value report: %w", err)
	}
	totalCount := 0
	for _, page := range pages {
		if page.TotalCount > totalCount {
			totalCount = page.TotalCount
		}
	}
	return pages, totalCount, nil
}

func gradeOperationalValueReportRun(ctx context.Context, evaluator *operationalValueReportEvaluator, run operationalValueReportRun, evidenceAt time.Time, evaluatorHost string) operationalValueReportObservation { //nolint:largefunc // Keeps one evaluator invocation and observation mapping atomic.
	createdAt := run.CreatedAt.UTC().Format(time.RFC3339)
	subject := graderArtifactSubject{
		Type:       "workflow-run",
		RunID:      run.ID,
		Attempt:    run.Attempt,
		Repository: evaluator.Definition.Repository,
		Workflow:   evaluator.Definition.WorkflowName,
		Ref:        run.Ref,
		SHA:        run.SHA,
		EventName:  run.EventName,
		CreatedAt:  &createdAt,
	}
	config := evaluator.GraderConfig
	if config == nil {
		config = map[string]any{}
	}
	request := operationalValueRunRequest{
		SchemaVersion: 1,
		Run: operationalValueRunSubject{
			ID:         run.ID,
			Attempt:    run.Attempt,
			Repository: evaluator.Definition.Repository,
			Workflow:   evaluator.Definition.WorkflowName,
			Ref:        run.Ref,
			SHA:        run.SHA,
			EventName:  run.EventName,
			CreatedAt:  &createdAt,
		},
		EvidenceAt: evidenceAt.UTC().Format(time.RFC3339),
		Case:       nil,
		Event:      nil,
		Config:     config,
	}
	requestJSON, err := json.Marshal(request)
	if err != nil {
		return failedOperationalValueReportObservation(run, evaluator.EvaluatorDigest, err)
	}
	output, err := runOperationalValueEvaluatorBash(ctx, "/bin/bash", evaluator.EvaluatorPath,
		[]string{evaluator.EvaluatorPath, "--grade-run"}, requestJSON, operationalValueEvaluatorTimeout, evaluatorHost)
	if err != nil {
		return failedOperationalValueReportObservation(run, evaluator.EvaluatorDigest, err)
	}
	execution, err := parseOperationalValueEvaluatorOutput(output, subject, request.EvidenceAt, evidenceAt, evaluator.Definition.Baseline.Value)
	if err != nil {
		return failedOperationalValueReportObservation(run, evaluator.EvaluatorDigest, err)
	}
	status := "unavailable"
	if execution.Value != nil {
		status = "pass"
		if passed := evaluateOperationalValueThreshold(execution.Value, evaluator.GraderDirection, evaluator.GraderThreshold); passed != nil && !*passed {
			status = "fail"
		}
	}
	return operationalValueReportObservation{
		Run:               run,
		Value:             execution.Value,
		Status:            status,
		Message:           execution.Message,
		OpportunityKey:    execution.Observation.OpportunityKey,
		EvidenceAt:        execution.Observation.EvidenceAt,
		EvidenceCutoff:    execution.Observation.EvidenceCutoff,
		MaturesAt:         execution.Observation.MaturesAt,
		Mature:            execution.Observation.Mature,
		Case:              execution.Observation.Case,
		Provenance:        execution.Observation.Provenance,
		Diagnostics:       execution.Diagnostics,
		BaselineValue:     execution.BaselineValue,
		DeltaFromBaseline: execution.DeltaFromBaseline,
		EvaluatorDigest:   evaluator.EvaluatorDigest,
		Source:            "evaluator-replay",
	}
}

func failedOperationalValueReportObservation(run operationalValueReportRun, evaluatorDigest string, err error) operationalValueReportObservation {
	return operationalValueReportObservation{
		Run:             run,
		Status:          "error",
		Message:         err.Error(),
		EvaluatorDigest: evaluatorDigest,
		Source:          "evaluator-replay",
	}
}

type operationalValueReportBackfillStats struct {
	CacheHits int
	Evaluated int
}

type operationalValueReportWeekBackfill struct {
	observations []operationalValueReportObservation
	stats        operationalValueReportBackfillStats
}

func backfillOperationalValueReportObservations(ctx context.Context, evaluator *operationalValueReportEvaluator, runs []operationalValueReportRun, evidenceAt time.Time, cacheRoot, evaluatorHost string, refresh bool, concurrency int) ([]operationalValueReportObservation, operationalValueReportBackfillStats, error) {
	weeks := make(map[time.Time][]operationalValueReportRun)
	for _, run := range runs {
		week := operationalValueUTCWeekStart(run.CreatedAt)
		weeks[week] = append(weeks[week], run)
	}
	weekStarts := make([]time.Time, 0, len(weeks))
	for week := range weeks {
		weekStarts = append(weekStarts, week)
	}
	slices.SortFunc(weekStarts, func(left, right time.Time) int { return cmp.Compare(left.Unix(), right.Unix()) })

	if concurrency <= 0 {
		concurrency = defaultOperationalValueReportConcurrency
	}
	results := make([]operationalValueReportWeekBackfill, len(weekStarts))
	group, groupContext := errgroup.WithContext(ctx)
	group.SetLimit(concurrency)
	for index, weekStart := range weekStarts {
		group.Go(func() error {
			result, err := backfillOperationalValueReportWeek(groupContext, evaluator, weeks[weekStart], weekStart, evidenceAt, cacheRoot, evaluatorHost, refresh)
			if err != nil {
				return err
			}
			results[index] = result
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		return nil, operationalValueReportBackfillStats{}, err
	}

	observations := make([]operationalValueReportObservation, 0, len(runs))
	stats := operationalValueReportBackfillStats{}
	for _, result := range results {
		observations = append(observations, result.observations...)
		stats.CacheHits += result.stats.CacheHits
		stats.Evaluated += result.stats.Evaluated
	}
	slices.SortFunc(observations, func(left, right operationalValueReportObservation) int {
		if operationalValueReportRunLess(left.Run, right.Run) {
			return -1
		}
		if operationalValueReportRunLess(right.Run, left.Run) {
			return 1
		}
		return 0
	})
	return observations, stats, nil
}

func backfillOperationalValueReportWeek(ctx context.Context, evaluator *operationalValueReportEvaluator, runs []operationalValueReportRun, weekStart, evidenceAt time.Time, cacheRoot, evaluatorHost string, refresh bool) (operationalValueReportWeekBackfill, error) {
	result := operationalValueReportWeekBackfill{observations: make([]operationalValueReportObservation, 0, len(runs))}
	cachePath, err := operationalValueReportWeeklyCachePath(cacheRoot, evaluator.Definition.Repository, evaluator.WorkflowID, evaluator.EvaluatorDigest, weekStart)
	if err != nil {
		return result, err
	}
	cached := []operationalValueReportObservation(nil)
	if !refresh {
		var hit bool
		cached, hit, err = loadOperationalValueReportWeeklyCache(cachePath, evaluator.Definition.Repository, evaluator.WorkflowID, evaluator.EvaluatorDigest, weekStart)
		if err != nil {
			return result, err
		}
		if !hit {
			cached = nil
		}
	}
	cachedByRun := make(map[string]operationalValueReportObservation, len(cached))
	for _, observation := range cached {
		if observation.Mature && observation.Value != nil &&
			(observation.Status == "pass" || observation.Status == "fail") &&
			observation.EvaluatorDigest == evaluator.EvaluatorDigest {
			cachedByRun[operationalValueReportObservationKey(observation)] = observation
		}
	}
	weekCache := make(map[string]operationalValueReportObservation, len(cached)+len(runs))
	maps.Copy(weekCache, cachedByRun)
	for _, run := range runs {
		key := run.ID + ":" + strconv.Itoa(run.Attempt)
		if observation, ok := cachedByRun[key]; ok {
			result.observations = append(result.observations, observation)
			result.stats.CacheHits++
			continue
		}
		observation := operationalValueReportGradeRun(ctx, evaluator, run, evidenceAt, evaluatorHost)
		result.observations = append(result.observations, observation)
		result.stats.Evaluated++
		if observation.Mature && (observation.Status == "pass" || observation.Status == "fail") {
			weekCache[key] = observation
		}
	}
	cacheObservations := make([]operationalValueReportObservation, 0, len(weekCache))
	for _, observation := range weekCache {
		cacheObservations = append(cacheObservations, observation)
	}
	slices.SortFunc(cacheObservations, func(left, right operationalValueReportObservation) int {
		if operationalValueReportRunLess(left.Run, right.Run) {
			return -1
		}
		if operationalValueReportRunLess(right.Run, left.Run) {
			return 1
		}
		return 0
	})
	if len(cacheObservations) > 0 {
		if err := saveOperationalValueReportWeeklyCache(cachePath, evaluator.Definition.Repository, evaluator.WorkflowID, evaluator.EvaluatorDigest, weekStart, cacheObservations); err != nil {
			return result, err
		}
	}
	return result, nil
}

func operationalValueReportRunLess(left, right operationalValueReportRun) bool {
	if !left.CreatedAt.Equal(right.CreatedAt) {
		return left.CreatedAt.Before(right.CreatedAt)
	}
	if left.ID != right.ID {
		return left.ID < right.ID
	}
	return left.Attempt < right.Attempt
}

func defaultOperationalValueReportCacheRoot() (string, error) {
	cacheRoot, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("cannot resolve user cache directory: %w", err)
	}
	if strings.TrimSpace(cacheRoot) == "" {
		return "", errors.New("user cache directory is empty")
	}
	return filepath.Join(cacheRoot, "gh-aw", "operational-value"), nil
}
