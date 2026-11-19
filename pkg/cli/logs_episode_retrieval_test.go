package cli

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type episodeRetrievalFixture struct {
	t        *testing.T
	runs     map[string]workflowRunAPIResponse
	contexts map[int64]*AwContext
	pages    map[string][]int64
	failures map[string]error
	archives map[int64][]byte
	calls    []string
}

func newEpisodeRetrievalFixture(t *testing.T) *episodeRetrievalFixture {
	return &episodeRetrievalFixture{
		t: t, runs: make(map[string]workflowRunAPIResponse), contexts: make(map[int64]*AwContext),
		pages: make(map[string][]int64), failures: make(map[string]error), archives: make(map[int64][]byte),
	}
}

func (f *episodeRetrievalFixture) add(repository string, id int64, episode, parentRepository string, parent int64) {
	run := workflowRunAPIResponse{
		ID: id, RunAttempt: 1, Name: "example", Path: ".github/workflows/example.lock.yml",
		Status: "completed", Conclusion: "success", CreatedAt: time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC),
		HTMLURL: fmt.Sprintf("https://github.com/%s/actions/runs/%d", repository, id),
	}
	run.Repository.FullName = repository
	f.runs[fmt.Sprintf("%s/%d", repository, id)] = run
	f.contexts[id] = &AwContext{EpisodeID: episode, Repo: parentRepository, RunID: strconv.FormatInt(parent, 10)}
	f.pages[repository] = append(f.pages[repository], id)
}

func (f *episodeRetrievalFixture) request(ctx context.Context, host, endpoint string) ([]byte, error) {
	f.t.Helper()
	require.NoError(f.t, ctx.Err())
	require.Equal(f.t, "github.com", host)
	f.calls = append(f.calls, endpoint)
	if err := f.failures[endpoint]; err != nil {
		return nil, err
	}
	parsed, err := url.Parse(endpoint)
	require.NoError(f.t, err)
	parts := strings.Split(parsed.Path, "/")
	require.GreaterOrEqual(f.t, len(parts), 5)
	repository := strings.Join(parts[1:3], "/")
	switch {
	case strings.Contains(endpoint, "/actions/artifacts/"):
		require.Len(f.t, parts, 7)
		id, err := strconv.ParseInt(parts[5], 10, 64)
		require.NoError(f.t, err)
		if archive, ok := f.archives[id]; ok {
			return archive, nil
		}
		return f.archive(id / 10), nil
	case len(parts) == 5:
		var runs []workflowRunAPIResponse
		page, err := strconv.Atoi(parsed.Query().Get("page"))
		require.NoError(f.t, err)
		ids := f.pages[repository]
		start := min((page-1)*100, len(ids))
		end := min(page*100, len(ids))
		for _, id := range ids[start:end] {
			runs = append(runs, f.runs[fmt.Sprintf("%s/%d", repository, id)])
		}
		return json.Marshal(map[string]any{"total_count": len(ids), "workflow_runs": runs})
	case len(parts) == 7 && parts[6] == "artifacts":
		id, err := strconv.ParseInt(parts[5], 10, 64)
		require.NoError(f.t, err)
		return json.Marshal(map[string]any{"artifacts": []episodeArtifact{{ID: id * 10, Name: "usage"}}})
	case len(parts) == 6:
		run, exists := f.runs[repository+"/"+parts[5]]
		if !exists {
			return nil, errors.New("HTTP 404: run not found")
		}
		return json.Marshal(run)
	default:
		f.t.Fatalf("unexpected endpoint: %s", endpoint)
		return nil, errors.New("unexpected endpoint")
	}
}

func (f *episodeRetrievalFixture) archive(id int64) []byte {
	f.t.Helper()
	return f.archiveWithAttempt(id, 1, fmt.Sprintf("request-%d", id), 0.25, "")
}

func (f *episodeRetrievalFixture) archiveWithAttempt(id int64, attempt int, requestID string, aic float64, summary string) []byte {
	line := fmt.Sprintf("{\"event\":\"token_usage\",\"request_id\":%q,\"provider\":\"unknown\",\"model\":\"gpt-4.1\",\"input_tokens\":10,\"output_tokens\":2,\"ai_credits_this_response\":%g,\"ai_credits_total\":%g}\n", requestID, aic, aic)
	return f.archiveWithRecords(id, attempt, line, summary)
}

func (f *episodeRetrievalFixture) archiveWithRecords(id int64, attempt int, records, summary string) []byte {
	f.t.Helper()
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	info := AwInfo{EngineID: "copilot", Context: f.contexts[id], RunID: NumericID(id), RunAttempt: strconv.Itoa(attempt)}
	file, err := writer.Create("aw_info.json")
	require.NoError(f.t, err)
	require.NoError(f.t, json.NewEncoder(file).Encode(info))
	usage, err := writer.Create("token_usage.jsonl")
	require.NoError(f.t, err)
	_, err = usage.Write([]byte(records))
	require.NoError(f.t, err)
	if summary != "" {
		file, err := writer.Create("activity/summary.json")
		require.NoError(f.t, err)
		_, err = file.Write([]byte(summary))
		require.NoError(f.t, err)
	}
	require.NoError(f.t, writer.Close())
	return archive.Bytes()
}

func episodeRetrievalTestOptions(t *testing.T) EpisodeRetrievalOptions {
	return EpisodeRetrievalOptions{
		Repository: "org/one", SeedRunID: 2, StartDate: "2026-10-18", EndDate: "2026-10-20",
		MaxRuns: 100, MaxAPIRequests: 500, Timeout: time.Minute, OutputDir: t.TempDir(),
	}
}

func countEpisodeCalls(calls []string, match func(string) bool) int {
	count := 0
	for _, call := range calls {
		if match(call) {
			count++
		}
	}
	return count
}

func TestEpisodeRetrievalCanonicalMembershipAndCycles(t *testing.T) {
	f := newEpisodeRetrievalFixture(t)
	f.add("org/one", 2, "canonical", "org/one", 1)
	f.add("org/one", 1, "canonical", "ORG/ONE", 2)
	f.add("org/two", 3, "canonical", "org/one", 2)
	f.add("org/two", 4, "different", "org/one", 2)
	opts := episodeRetrievalTestOptions(t)
	opts.AllowRepositories = []string{"ORG/TWO", "org/two"}
	data, err := retrieveEpisodeLogs(context.Background(), opts, f.request)
	require.NoError(t, err)
	require.Len(t, data.Episodes, 1)
	require.ElementsMatch(t, []int64{1, 2, 3}, data.Episodes[0].RunIDs)
	require.Equal(t, "canonical", data.Episodes[0].EpisodeID)
	require.Equal(t, "unknown", data.Episodes[0].Coverage.Status)
	require.False(t, data.Episodes[0].Coverage.CollectionTruncated)
	require.Equal(t, []string{"org/one", "org/two"}, data.EpisodeRetrieval.Repositories)
	require.Equal(t, 4, data.EpisodeRetrieval.RunsExamined)
	require.Equal(t, len(f.calls), data.EpisodeRetrieval.APIRequests)
	require.Equal(t, 1, countEpisodeCalls(f.calls, func(s string) bool { return s == "repos/org/one/actions/runs/1" }))
	require.Empty(t, data.EpisodeRetrieval.Issues)
	require.Equal(t, 36, data.Summary.TotalTokens)
}

func TestEpisodeRetrievalRunBudgetCountsNonmembersAndMissingRuns(t *testing.T) {
	f := newEpisodeRetrievalFixture(t)
	f.add("org/one", 2, "canonical", "org/one", 1)
	f.add("org/one", 3, "different", "org/one", 2)
	f.add("org/one", 4, "canonical", "org/one", 2)
	opts := episodeRetrievalTestOptions(t)
	opts.MaxRuns = 3
	data, err := retrieveEpisodeLogs(context.Background(), opts, f.request)
	require.NoError(t, err)
	require.Equal(t, 3, data.EpisodeRetrieval.RunsExamined)
	require.Equal(t, []int64{2}, data.Episodes[0].RunIDs)
	require.Equal(t, []string{"run_limit"}, data.EpisodeRetrieval.StopReasons)
	require.True(t, data.Episodes[0].Coverage.CollectionTruncated)
	require.Contains(t, data.Episodes[0].Coverage.Reasons, "retrieval_run_limit")
	require.NotContains(t, strings.Join(f.calls, "\n"), "/runs/4/artifacts")
}

func TestEpisodeRetrievalRetriesCandidateAfterInitialFailure(t *testing.T) {
	f := newEpisodeRetrievalFixture(t)
	f.add("org/one", 2, "canonical", "org/one", 2)
	f.add("org/one", 3, "canonical", "org/one", 3)
	f.add("org/one", 4, "canonical", "org/one", 3)
	var failed bool
	request := func(ctx context.Context, host, endpoint string) ([]byte, error) {
		if strings.Contains(endpoint, "/runs/3/artifacts?") && !failed {
			failed = true
			f.calls = append(f.calls, endpoint)
			return nil, errors.New("temporary artifact inventory failure")
		}
		return f.request(ctx, host, endpoint)
	}
	data, err := retrieveEpisodeLogs(context.Background(), episodeRetrievalTestOptions(t), request)
	require.NoError(t, err)
	require.ElementsMatch(t, []int64{2, 3, 4}, data.Episodes[0].RunIDs)
	require.Equal(t, 2, countEpisodeCalls(f.calls, func(call string) bool {
		return strings.Contains(call, "/runs/3/artifacts?")
	}))
	require.Contains(t, data.EpisodeRetrieval.Issues[0].Reason, "candidate_unavailable")
}

func TestEpisodeRetrievalHardAPIBudget(t *testing.T) {
	for budget := 1; budget <= 10; budget++ {
		t.Run(strconv.Itoa(budget), func(t *testing.T) {
			f := newEpisodeRetrievalFixture(t)
			f.add("org/one", 2, "canonical", "org/one", 1)
			f.add("org/one", 1, "canonical", "org/one", 1)
			opts := episodeRetrievalTestOptions(t)
			opts.MaxAPIRequests = budget
			data, err := retrieveEpisodeLogs(context.Background(), opts, f.request)
			require.LessOrEqual(t, len(f.calls), budget)
			if budget < 3 {
				require.ErrorIs(t, err, errEpisodeAPILimit)
				return
			}
			require.NoError(t, err)
			require.Equal(t, len(f.calls), data.EpisodeRetrieval.APIRequests)
			if budget < 7 {
				require.Contains(t, data.EpisodeRetrieval.StopReasons, "api_request_limit")
				require.True(t, data.Episodes[0].Coverage.CollectionTruncated)
			}
		})
	}
}

func TestEpisodeRetrievalAllowlistNeverFollowsArbitraryRepositories(t *testing.T) {
	f := newEpisodeRetrievalFixture(t)
	f.add("org/one", 2, "canonical", "attacker/secret", 1)
	f.contexts[2].RootRepo = "other/root"
	f.contexts[2].RootRunID = "3"
	data, err := retrieveEpisodeLogs(context.Background(), episodeRetrievalTestOptions(t), f.request)
	require.NoError(t, err)
	require.Equal(t, 1, data.EpisodeRetrieval.RunsExamined)
	require.Len(t, data.EpisodeRetrieval.Issues, 2)
	require.Contains(t, data.Episodes[0].Coverage.Reasons, "retrieval_repository_not_allowed")
	for _, call := range f.calls {
		require.True(t, strings.HasPrefix(call, "repos/org/one/"), call)
	}
}

func TestEpisodeRetrievalTimeWindowAppliesToRecordedRuns(t *testing.T) {
	f := newEpisodeRetrievalFixture(t)
	f.add("org/one", 2, "canonical", "org/one", 1)
	f.add("org/one", 1, "canonical", "org/one", 1)
	run := f.runs["org/one/1"]
	run.CreatedAt = run.CreatedAt.AddDate(0, 0, -10)
	f.runs["org/one/1"] = run
	data, err := retrieveEpisodeLogs(context.Background(), episodeRetrievalTestOptions(t), f.request)
	require.NoError(t, err)
	require.Equal(t, []int64{2}, data.Episodes[0].RunIDs)
	require.NotContains(t, strings.Join(f.calls, "\n"), "/runs/1/artifacts")
	require.Contains(t, data.EpisodeRetrieval.Issues[0].Message, "outside the retrieval time window")
	listCall := f.calls[len(f.calls)-1]
	parsed, err := url.Parse(listCall)
	require.NoError(t, err)
	require.Equal(t, "2026-10-18T00:00:00Z..2026-10-20T23:59:59Z", parsed.Query().Get("created"))
}

func TestEpisodeRetrievalDateOnlyEndIncludesWholeDay(t *testing.T) {
	f := newEpisodeRetrievalFixture(t)
	f.add("org/one", 2, "canonical", "org/one", 2)
	opts := episodeRetrievalTestOptions(t)
	opts.StartDate = "2026-10-19"
	opts.EndDate = "2026-10-19"
	data, err := retrieveEpisodeLogs(context.Background(), opts, f.request)
	require.NoError(t, err)
	require.Equal(t, []int64{2}, data.Episodes[0].RunIDs)
	require.Equal(t, time.Date(2026, 10, 19, 23, 59, 59, 0, time.UTC), data.EpisodeRetrieval.EndDate)
	opts.EndDate = "2026-10-19T12:30:00Z"
	_, end, err := episodeRetrievalDates(opts.StartDate, opts.EndDate, time.Now())
	require.NoError(t, err)
	require.Equal(t, time.Date(2026, 10, 19, 12, 30, 0, 0, time.UTC), end)
}

func TestEpisodeRetrievalConflictingRecordedIdentity(t *testing.T) {
	f := newEpisodeRetrievalFixture(t)
	f.add("org/one", 2, "canonical", "org/one", 1)
	f.add("org/one", 1, "different", "org/one", 1)
	data, err := retrieveEpisodeLogs(context.Background(), episodeRetrievalTestOptions(t), f.request)
	require.NoError(t, err)
	require.Equal(t, []int64{2}, data.Episodes[0].RunIDs)
	require.Contains(t, data.EpisodeRetrieval.Issues[0].Message, "different canonical episode")
	require.Equal(t, "partial", data.Episodes[0].Coverage.Status)
}

func TestEpisodeRetrievalArtifactCacheAndRunAttempts(t *testing.T) {
	f := newEpisodeRetrievalFixture(t)
	f.add("org/one", 2, "canonical", "org/one", 2)
	opts := episodeRetrievalTestOptions(t)
	first, err := retrieveEpisodeLogs(context.Background(), opts, f.request)
	require.NoError(t, err)
	f.calls = nil
	second, err := retrieveEpisodeLogs(context.Background(), opts, f.request)
	require.NoError(t, err)
	require.Len(t, f.calls, 3) // Seed metadata, artifact inventory, run discovery; no archive.
	require.Equal(t, first.Episodes, second.Episodes)
	run := f.runs["org/one/2"]
	run.RunAttempt = 2
	f.runs["org/one/2"] = run
	_, err = retrieveEpisodeLogs(context.Background(), opts, f.request)
	require.ErrorContains(t, err, "no current-attempt canonical identity")
	require.Equal(t, 1, countEpisodeCalls(f.calls, func(s string) bool { return strings.HasSuffix(s, "/20/zip") }))
}

func TestEpisodeRetrievalExpiredArtifactReusesCompletedCache(t *testing.T) {
	f := newEpisodeRetrievalFixture(t)
	f.add("org/one", 2, "canonical", "org/one", 2)
	opts := episodeRetrievalTestOptions(t)
	first, err := retrieveEpisodeLogs(context.Background(), opts, f.request)
	require.NoError(t, err)
	f.calls = nil
	request := func(ctx context.Context, host, endpoint string) ([]byte, error) {
		if strings.Contains(endpoint, "/artifacts?") {
			f.calls = append(f.calls, endpoint)
			return json.Marshal(map[string]any{"artifacts": []episodeArtifact{{ID: 20, Name: "usage", Expired: true}}})
		}
		return f.request(ctx, host, endpoint)
	}
	second, err := retrieveEpisodeLogs(context.Background(), opts, request)
	require.NoError(t, err)
	require.Equal(t, first.Episodes, second.Episodes)
	require.NotContains(t, fmt.Sprintf("%+v", second.EpisodeRetrieval.Issues), "expired_artifact")
	require.Equal(t, 0, countEpisodeCalls(f.calls, func(call string) bool {
		return strings.HasSuffix(call, "/20/zip")
	}))
}

func TestEpisodeArtifactConcurrentPublicationKeepsCompletedCache(t *testing.T) {
	f := newEpisodeRetrievalFixture(t)
	archive := f.archive(2)
	var mu sync.Mutex
	requests := 0
	ready := make(chan struct{})
	request := func(context.Context, string, string) ([]byte, error) {
		mu.Lock()
		requests++
		if requests == 2 {
			close(ready)
		}
		mu.Unlock()
		<-ready
		return archive, nil
	}
	run := WorkflowRun{DatabaseID: 2, Attempt: 1, Repository: "org/one"}
	opts := EpisodeRetrievalOptions{OutputDir: t.TempDir(), MaxAPIRequests: 10}
	runDir := filepath.Join(opts.OutputDir, "run-2", "attempt-1")
	require.NoError(t, os.MkdirAll(runDir, 0o700))
	artifact := episodeArtifact{ID: 20, Name: "run-usage"}
	dirs := make([]string, 2)
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i := range dirs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			collector := &episodeCollector{
				opts: opts, report: EpisodeRetrieval{Host: "github.com"}, request: request,
			}
			dirs[i], errs[i] = collector.loadArtifact(context.Background(), run, artifact, runDir)
		}(i)
	}
	wg.Wait()
	require.NoError(t, errs[0])
	require.NoError(t, errs[1])
	require.Equal(t, dirs[0], dirs[1])
	marker, err := os.ReadFile(filepath.Join(dirs[0], ".episode-complete"))
	require.NoError(t, err)
	require.Equal(t, "v1\n", string(marker))
}

func TestEpisodeRetrievalUnavailableUsagePreservesVerifiedMember(t *testing.T) {
	f := newEpisodeRetrievalFixture(t)
	f.add("org/one", 2, "canonical", "org/one", 2)
	request := func(ctx context.Context, host, endpoint string) ([]byte, error) {
		if strings.Contains(endpoint, "/artifacts?") {
			f.calls = append(f.calls, endpoint)
			return json.Marshal(map[string]any{"artifacts": []episodeArtifact{
				{ID: 20, Name: "run-info"},
				{ID: 21, Name: "invocation-usage"},
			}})
		}
		if strings.HasSuffix(endpoint, "/21/zip") {
			f.calls = append(f.calls, endpoint)
			return nil, errors.New("gh: artifact unavailable (HTTP 410)")
		}
		return f.request(ctx, host, endpoint)
	}
	data, err := retrieveEpisodeLogs(context.Background(), episodeRetrievalTestOptions(t), request)
	require.NoError(t, err)
	require.Equal(t, []int64{2}, data.Episodes[0].RunIDs)
	require.Contains(t, data.EpisodeRetrieval.Issues[0].Reason, "missing_artifact")
	require.Contains(t, data.Episodes[0].Coverage.Reasons, "retrieval_missing_artifact")
}

func TestEpisodeRetrievalFiltersOldAttemptsAndAggregatesUsageArtifacts(t *testing.T) {
	f := newEpisodeRetrievalFixture(t)
	f.add("org/one", 2, "canonical", "org/one", 2)
	run := f.runs["org/one/2"]
	run.RunAttempt = 2
	f.runs["org/one/2"] = run
	activity := `{"schema":"usage-activity-summary/v1","session":{"turns":7},"firewall":{"total_requests":2,"allowed_requests":2},"working_set":{"measurement_state":"measured","invocations":3},"ledger":{"transactions_added":2},"friction":{},"skills":{"total_invocations":1,"unique_skills":1,"items":[{"name":"skill-one","invocation_count":1}]},"safe_outputs":{"total_items":1,"items":[{}]},"steering":{"total_events":2}}`
	f.archives[20] = f.archiveWithAttempt(2, 1, "old-attempt", 5, "")
	f.archives[21] = f.archiveWithAttempt(2, 1, "old-usage", 5, "")
	f.archives[30] = f.archiveWithRecords(2, 2, "", "")
	f.archives[31] = f.archiveWithAttempt(2, 2, "stream-one", 0.25, activity)
	f.archives[32] = f.archiveWithRecords(2, 2,
		"{\"event\":\"token_usage\",\"request_id\":\"stream-one\",\"provider\":\"unknown\",\"model\":\"gpt-4.1\",\"input_tokens\":10,\"output_tokens\":2,\"ai_credits_this_response\":0.25,\"ai_credits_total\":0.25}\n"+
			"{\"event\":\"token_usage\",\"request_id\":\"stream-two\",\"provider\":\"unknown\",\"model\":\"gpt-4.1\",\"input_tokens\":10,\"output_tokens\":2,\"ai_credits_this_response\":0.75,\"ai_credits_total\":1.0}\n", "")
	request := func(ctx context.Context, host, endpoint string) ([]byte, error) {
		if strings.Contains(endpoint, "/artifacts?") {
			f.calls = append(f.calls, endpoint)
			return json.Marshal(map[string]any{"artifacts": []episodeArtifact{
				{ID: 20, Name: "old-info"},
				{ID: 21, Name: "old-usage"},
				{ID: 30, Name: "current-info"},
				{ID: 31, Name: "stream-one-usage"},
				{ID: 32, Name: "stream-two-usage"},
			}})
		}
		return f.request(ctx, host, endpoint)
	}
	opts := episodeRetrievalTestOptions(t)
	data, err := retrieveEpisodeLogs(context.Background(), opts, request)
	require.NoError(t, err)
	require.Equal(t, 24, data.Summary.TotalTokens)
	require.NotNil(t, data.Summary.TotalAIC)
	require.InDelta(t, 1.0, *data.Summary.TotalAIC, 0.000001)
	require.Equal(t, "available", data.Summary.AICCoverage)

	runData := f.runs["org/one/2"].workflowRun()
	runDir := data.Runs[0].LogsPath
	info, err := parseAwInfo(filepath.Join(runDir, "aw_info.json"), false)
	require.NoError(t, err)
	dirs, isEpisode, err := readEpisodeArtifactDirs(runDir)
	require.NoError(t, err)
	require.True(t, isEpisode)
	var usageDirs []string
	for _, dir := range dirs {
		if strings.HasSuffix(dir, "-usage") {
			usageDirs = append(usageDirs, dir)
		}
	}
	require.Len(t, usageDirs, 2)
	processed, err := processEpisodeArtifacts(runData, runDir, info)
	require.NoError(t, err)
	require.Equal(t, 7, processed.Run.Turns)
	require.NotNil(t, processed.WorkingSet)
	require.NotNil(t, processed.Ledger)
	require.NotNil(t, processed.Friction)
	require.Len(t, processed.SkillActivations, 1)
	require.Len(t, processed.SafeOutputs, 1)
	require.NotNil(t, processed.TokenUsage)
	require.Equal(t, 2, processed.TokenUsage.TotalSteeringEvents)

	usage, err := analyzeTokenUsageAICOnly(data.Runs[0].LogsPath, false)
	require.NoError(t, err)
	require.InDelta(t, 1.0, usage.TotalAIC, 0.000001)
}

func TestEpisodeRetrievalMissingContextAndDiscoveryFailure(t *testing.T) {
	t.Run("missing seed context fails explicitly", func(t *testing.T) {
		f := newEpisodeRetrievalFixture(t)
		f.add("org/one", 2, "", "org/one", 2)
		_, err := retrieveEpisodeLogs(context.Background(), episodeRetrievalTestOptions(t), f.request)
		require.ErrorContains(t, err, "context.episode_id")
	})
	t.Run("failed discovery retains seed and gap", func(t *testing.T) {
		f := newEpisodeRetrievalFixture(t)
		f.add("org/one", 2, "canonical", "org/one", 2)
		request := func(ctx context.Context, host, endpoint string) ([]byte, error) {
			if strings.Contains(endpoint, "/actions/runs?") {
				f.calls = append(f.calls, endpoint)
				return nil, errors.New("HTTP 403: repository unavailable")
			}
			return f.request(ctx, host, endpoint)
		}
		data, err := retrieveEpisodeLogs(context.Background(), episodeRetrievalTestOptions(t), request)
		require.NoError(t, err)
		require.Len(t, data.Runs, 1)
		require.Contains(t, data.Episodes[0].Coverage.Reasons, "retrieval_discovery_failed")
		require.Contains(t, data.EpisodeRetrieval.Issues[0].Message, "HTTP 403")
	})
}

func TestEpisodeRetrievalTimeoutAndCancellation(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		t.Run(strconv.FormatBool(cancelled), func(t *testing.T) {
			f := newEpisodeRetrievalFixture(t)
			f.add("org/one", 2, "canonical", "org/one", 2)
			opts := episodeRetrievalTestOptions(t)
			opts.Timeout = 100 * time.Millisecond
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			request := func(ctx context.Context, host, endpoint string) ([]byte, error) {
				if strings.Contains(endpoint, "/actions/runs?") {
					if cancelled {
						cancel()
					}
					<-ctx.Done()
					return nil, ctx.Err()
				}
				return f.request(ctx, host, endpoint)
			}
			data, err := retrieveEpisodeLogs(ctx, opts, request)
			if cancelled {
				require.ErrorIs(t, err, context.Canceled)
				return
			}
			require.NoError(t, err)
			require.Equal(t, []string{"timeout"}, data.EpisodeRetrieval.StopReasons)
			require.Len(t, data.Runs, 1)
		})
	}
}

func TestEpisodeRetrievalPaginationIsBounded(t *testing.T) {
	f := newEpisodeRetrievalFixture(t)
	f.add("org/one", 2, "canonical", "org/one", 2)
	f.pages["org/one"] = slices.Repeat([]int64{2}, 100)
	f.pages["org/one"] = append(f.pages["org/one"], 3)
	f.add("org/one", 3, "canonical", "org/one", 2)
	data, err := retrieveEpisodeLogs(context.Background(), episodeRetrievalTestOptions(t), f.request)
	require.NoError(t, err)
	require.Equal(t, 2, data.EpisodeRetrieval.RunsExamined)
	require.ElementsMatch(t, []int64{2, 3}, data.Episodes[0].RunIDs)
	require.Equal(t, 2, countEpisodeCalls(f.calls, func(s string) bool { return strings.Contains(s, "/actions/runs?") }))
}

func TestEpisodeRetrievalSupersededArtifactDoesNotDoubleCountUsage(t *testing.T) {
	f := newEpisodeRetrievalFixture(t)
	f.add("org/one", 2, "canonical", "org/one", 2)
	opts := episodeRetrievalTestOptions(t)
	first, err := retrieveEpisodeLogs(context.Background(), opts, f.request)
	require.NoError(t, err)
	request := func(ctx context.Context, host, endpoint string) ([]byte, error) {
		if strings.Contains(endpoint, "/artifacts?") {
			f.calls = append(f.calls, endpoint)
			return json.Marshal(map[string]any{"artifacts": []episodeArtifact{{ID: 21, Name: "usage"}}})
		}
		return f.request(ctx, host, endpoint)
	}
	second, err := retrieveEpisodeLogs(context.Background(), opts, request)
	require.NoError(t, err)
	require.Equal(t, first.Summary.TotalTokens, second.Summary.TotalTokens)
	_, err = os.Stat(filepath.Join(filepath.Dir(second.Runs[0].LogsPath), "20-usage"))
	require.NoError(t, err)
	usage, err := analyzeTokenUsageAICOnly(second.Runs[0].LogsPath, false)
	require.NoError(t, err)
	require.InDelta(t, 0.25, usage.TotalAIC, 0.000001)
}

func TestEpisodeRetrievalRetainsVerifiedSeedWhenUsageHitsBudget(t *testing.T) {
	f := newEpisodeRetrievalFixture(t)
	f.add("org/one", 2, "canonical", "org/one", 2)
	opts := episodeRetrievalTestOptions(t)
	opts.MaxAPIRequests = 3
	request := func(ctx context.Context, host, endpoint string) ([]byte, error) {
		if strings.Contains(endpoint, "/artifacts?") {
			f.calls = append(f.calls, endpoint)
			return json.Marshal(map[string]any{"artifacts": []episodeArtifact{{ID: 20, Name: "info"}, {ID: 21, Name: "usage"}}})
		}
		return f.request(ctx, host, endpoint)
	}
	data, err := retrieveEpisodeLogs(context.Background(), opts, request)
	require.NoError(t, err)
	require.Equal(t, []int64{2}, data.Episodes[0].RunIDs)
	require.Equal(t, 3, data.EpisodeRetrieval.APIRequests)
	require.Contains(t, data.Episodes[0].Coverage.Reasons, "retrieval_api_request_limit")
}

func TestEpisodeRetrievalCompactArchiveGuards(t *testing.T) {
	for _, tc := range []struct {
		name string
		size uint64
		want string
	}{
		{"../outside", 0, "invalid file path"},
		{"large.json", maxEpisodeArtifactBytes + 1, "extracted-size limit"},
	} {
		var data bytes.Buffer
		writer := zip.NewWriter(&data)
		_, err := writer.CreateRaw(&zip.FileHeader{Name: tc.name, UncompressedSize64: tc.size})
		require.NoError(t, err)
		require.NoError(t, writer.Close())
		require.ErrorContains(t, extractEpisodeArtifact(context.Background(), data.Bytes(), t.TempDir()), tc.want)
	}
	f := newEpisodeRetrievalFixture(t)
	f.add("org/one", 2, "canonical", "org/one", 2)
	request := func(ctx context.Context, host, endpoint string) ([]byte, error) {
		if strings.Contains(endpoint, "/artifacts?") {
			f.calls = append(f.calls, endpoint)
			return json.Marshal(map[string]any{"artifacts": []episodeArtifact{{ID: 20, Name: "usage", SizeInBytes: maxEpisodeArtifactBytes + 1}}})
		}
		return f.request(ctx, host, endpoint)
	}
	_, err := retrieveEpisodeLogs(context.Background(), episodeRetrievalTestOptions(t), request)
	require.ErrorContains(t, err, "archive limit")
	require.Len(t, f.calls, 2)
}

func TestEpisodeRetrievalGitHubSearchCeilingIsNotCompleteness(t *testing.T) {
	f := newEpisodeRetrievalFixture(t)
	f.add("org/one", 2, "canonical", "org/one", 2)
	request := func(ctx context.Context, host, endpoint string) ([]byte, error) {
		if strings.Contains(endpoint, "/actions/runs?") {
			f.calls = append(f.calls, endpoint)
			runs := slices.Repeat([]workflowRunAPIResponse{f.runs["org/one/2"]}, 100)
			return json.Marshal(map[string]any{"total_count": 2000, "workflow_runs": runs})
		}
		return f.request(ctx, host, endpoint)
	}
	data, err := retrieveEpisodeLogs(context.Background(), episodeRetrievalTestOptions(t), request)
	require.NoError(t, err)
	require.Equal(t, 1, data.EpisodeRetrieval.RunsExamined)
	require.Equal(t, 10, countEpisodeCalls(f.calls, func(call string) bool { return strings.Contains(call, "/actions/runs?") }))
	require.Contains(t, data.EpisodeRetrieval.StopReasons, "search_result_limit")
	require.True(t, data.Episodes[0].Coverage.CollectionTruncated)
	require.Equal(t, "partial", data.Episodes[0].Coverage.Status)
}
