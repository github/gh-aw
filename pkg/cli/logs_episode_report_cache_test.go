package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func noEpisodeCacheRequest(t *testing.T) episodeAPIRequest {
	t.Helper()
	return func(context.Context, string, string) ([]byte, error) {
		t.Error("cached report must not make a GitHub request")
		return nil, errors.New("unexpected GitHub request")
	}
}

func TestEpisodeReportCacheReusesSnapshotAcrossViews(t *testing.T) {
	fixture := newEpisodeRetrievalFixture(t)
	fixture.add("org/one", 2, "canonical", "org/one", 2)
	opts := episodeRetrievalTestOptions(t)
	fresh, err := retrieveCachedEpisodeLogs(t.Context(), opts, fixture.request)
	require.NoError(t, err)
	require.False(t, fresh.EpisodeRetrieval.Cached)
	require.False(t, fresh.EpisodeRetrieval.CollectedAt.IsZero())
	cached, err := retrieveCachedEpisodeLogs(t.Context(), opts, noEpisodeCacheRequest(t))
	require.NoError(t, err)
	require.True(t, cached.EpisodeRetrieval.Cached)
	require.Equal(t, fresh.EpisodeRetrieval.CollectedAt, cached.EpisodeRetrieval.CollectedAt)
	require.Equal(t, fresh.Episodes, cached.Episodes)
	require.Equal(t, fresh.EpisodeRetrieval.APIRequests, cached.EpisodeRetrieval.APIRequests)
	for _, view := range []string{"summary", "hops"} {
		var output bytes.Buffer
		require.NoError(t, renderEpisodeView(&output, cached, view, false))
		require.Contains(t, output.String(), "Cached episode snapshot")
		require.Contains(t, output.String(), "no GitHub requests")
		require.Contains(t, output.String(), "--refresh")
		require.Contains(t, output.String(), "canonical")
		output.Reset()
		require.NoError(t, renderEpisodeView(&output, cached, view, true))
		var decoded LogsData
		require.NoError(t, json.Unmarshal(output.Bytes(), &decoded))
		require.True(t, decoded.EpisodeRetrieval.Cached)
		require.Len(t, decoded.Episodes, 1)
		require.NotNil(t, decoded.Episodes[0].Explanation)
	}
	require.ErrorIs(t, renderEpisodeView(episodeFailWriter{}, cached, "hops", false), io.ErrClosedPipe)
}

func TestEpisodeReportCacheRefreshPreservesLastGoodSnapshot(t *testing.T) {
	fixture := newEpisodeRetrievalFixture(t)
	fixture.add("org/one", 2, "canonical", "org/one", 2)
	opts := episodeRetrievalTestOptions(t)
	first, err := retrieveCachedEpisodeLogs(t.Context(), opts, fixture.request)
	require.NoError(t, err)
	fixture.add("org/one", 3, "canonical", "org/one", 2)
	opts.Refresh = true
	_, err = retrieveCachedEpisodeLogs(t.Context(), opts, func(context.Context, string, string) ([]byte, error) {
		return nil, errors.New("offline")
	})
	require.ErrorContains(t, err, "offline")
	opts.Refresh = false
	cached, err := retrieveCachedEpisodeLogs(t.Context(), opts, noEpisodeCacheRequest(t))
	require.NoError(t, err)
	require.Equal(t, first.Episodes, cached.Episodes)
	opts.Refresh = true
	refreshed, err := retrieveCachedEpisodeLogs(t.Context(), opts, fixture.request)
	require.NoError(t, err)
	require.False(t, refreshed.EpisodeRetrieval.Cached)
	require.ElementsMatch(t, []int64{2, 3}, refreshed.Episodes[0].RunIDs)
	require.False(t, refreshed.EpisodeRetrieval.CollectedAt.Before(first.EpisodeRetrieval.CollectedAt))
	opts.Refresh = false
	cached, err = retrieveCachedEpisodeLogs(t.Context(), opts, noEpisodeCacheRequest(t))
	require.NoError(t, err)
	require.Equal(t, refreshed.Episodes, cached.Episodes)
}

func TestEpisodeReportCacheKeysRespectRetrievalScope(t *testing.T) {
	opts := episodeRetrievalTestOptions(t)
	path, err := episodeReportCachePath(opts)
	require.NoError(t, err)
	for _, change := range []func(*EpisodeRetrievalOptions){
		func(o *EpisodeRetrievalOptions) { o.SeedRunID++ },
		func(o *EpisodeRetrievalOptions) { o.Repository = "org/two" },
		func(o *EpisodeRetrievalOptions) { o.Repository = "ghe.example/org/one" },
		func(o *EpisodeRetrievalOptions) { o.AllowRepositories = []string{"org/two"} },
		func(o *EpisodeRetrievalOptions) { o.StartDate = "2026-10-17" },
		func(o *EpisodeRetrievalOptions) { o.EndDate = "2026-10-21" },
		func(o *EpisodeRetrievalOptions) { o.MaxRuns++ },
		func(o *EpisodeRetrievalOptions) { o.MaxAPIRequests++ },
		func(o *EpisodeRetrievalOptions) { o.Timeout += time.Second },
		func(o *EpisodeRetrievalOptions) { o.OutputDir = filepath.Join(o.OutputDir, "other") },
	} {
		changed := opts
		change(&changed)
		other, err := episodeReportCachePath(changed)
		require.NoError(t, err)
		require.NotEqual(t, path, other)
	}
	opts.Refresh, opts.Repository = true, "github.com/ORG/ONE"
	opts.AllowRepositories = []string{"ORG/ONE"}
	same, err := episodeReportCachePath(opts)
	require.NoError(t, err)
	require.Equal(t, path, same)
	opts.AllowRepositories = []string{"org/three", "org/two"}
	first, err := episodeReportCachePath(opts)
	require.NoError(t, err)
	opts.AllowRepositories = []string{"ORG/TWO", "org/three", "org/two"}
	second, err := episodeReportCachePath(opts)
	require.NoError(t, err)
	require.Equal(t, first, second)
}

func TestEpisodeReportCacheRejectsCorruptSnapshotsWithoutRequests(t *testing.T) {
	opts := episodeRetrievalTestOptions(t)
	path, err := episodeReportCachePath(opts)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	for _, content := range []string{"{", "{}", `{"version":99}`, `{"version":1,"data":{"episodes":[]}}`} {
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
		_, err := retrieveCachedEpisodeLogs(t.Context(), opts, noEpisodeCacheRequest(t))
		require.ErrorContains(t, err, "episode cache")
		require.ErrorContains(t, err, "--refresh")
	}
	fixture := newEpisodeRetrievalFixture(t)
	fixture.add("org/one", 2, "canonical", "org/one", 2)
	opts.Refresh = true
	_, err = retrieveCachedEpisodeLogs(t.Context(), opts, fixture.request)
	require.NoError(t, err, "explicit refresh replaces a corrupt cache")
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	opts.Refresh = false
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = retrieveCachedEpisodeLogs(ctx, opts, noEpisodeCacheRequest(t))
	require.ErrorIs(t, err, context.Canceled)
	opts.MaxRuns = 0
	_, err = retrieveCachedEpisodeLogs(t.Context(), opts, noEpisodeCacheRequest(t))
	require.ErrorContains(t, err, "positive")
}

func TestEpisodeReportCacheRejectsLegacyNullableAICSnapshot(t *testing.T) {
	opts := episodeRetrievalTestOptions(t)
	path, err := episodeReportCachePath(opts)
	require.NoError(t, err)
	require.Contains(t, path, "report-v2-")

	report := LogsData{
		Runs:     []RunData{{RunID: opts.SeedRunID}},
		Episodes: []EpisodeData{{EpisodeID: "canonical", RunIDs: []int64{opts.SeedRunID}}},
		EpisodeRetrieval: &EpisodeRetrieval{
			EpisodeID: "canonical", CollectedAt: time.Now().UTC(),
		},
	}
	encoded, err := json.Marshal(episodeReportCache{Version: 1, Data: report})
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, writeSensitiveFile(path, encoded))

	_, found, err := readEpisodeReportCache(path, opts.SeedRunID)
	require.ErrorContains(t, err, "invalid episode cache")
	require.False(t, found)
}

func TestEpisodeReportCacheConcurrentPublication(t *testing.T) {
	fixture := newEpisodeRetrievalFixture(t)
	fixture.add("org/one", 2, "canonical", "org/one", 2)
	opts := episodeRetrievalTestOptions(t)
	var mutex sync.Mutex
	request := func(ctx context.Context, host, endpoint string) ([]byte, error) {
		mutex.Lock()
		defer mutex.Unlock()
		return fixture.request(ctx, host, endpoint)
	}
	var workers sync.WaitGroup
	results := make([]LogsData, 2)
	errs := make([]error, 2)
	for i := range results {
		workers.Go(func() {
			results[i], errs[i] = retrieveCachedEpisodeLogs(t.Context(), opts, request)
		})
	}
	workers.Wait()
	for _, err := range errs {
		require.NoError(t, err)
	}
	cached, err := retrieveCachedEpisodeLogs(t.Context(), opts, noEpisodeCacheRequest(t))
	require.NoError(t, err)
	require.True(t, cached.EpisodeRetrieval.Cached)
	for _, result := range results {
		require.Equal(t, result.Episodes, cached.Episodes)
	}
}

func TestEpisodeReportCacheReportsStorageErrors(t *testing.T) {
	opts := episodeRetrievalTestOptions(t)
	path, err := episodeReportCachePath(opts)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(path, 0o700))
	_, err = retrieveCachedEpisodeLogs(t.Context(), opts, noEpisodeCacheRequest(t))
	require.ErrorContains(t, err, "read episode cache")
	fixture := newEpisodeRetrievalFixture(t)
	fixture.add("org/one", 2, "canonical", "org/one", 2)
	opts.Refresh = true
	_, err = retrieveCachedEpisodeLogs(t.Context(), opts, fixture.request)
	require.ErrorContains(t, err, "save episode cache")
}

func TestEpisodeReportCachePreservesHistoricalRelativeWindow(t *testing.T) {
	opts := episodeRetrievalTestOptions(t)
	opts.StartDate, opts.EndDate = "-7d", "2000-01-02"
	path, err := episodeReportCachePath(opts)
	require.NoError(t, err, "looking up a snapshot must not re-evaluate relative dates")
	report := LogsData{
		Runs:     []RunData{{RunID: 2}},
		Episodes: []EpisodeData{{EpisodeID: "canonical", RunIDs: []int64{2}}},
		EpisodeRetrieval: &EpisodeRetrieval{
			EpisodeID: "canonical", CollectedAt: time.Date(2000, 1, 2, 0, 0, 0, 0, time.UTC),
			StartDate: time.Date(1999, 12, 26, 0, 0, 0, 0, time.UTC),
		},
	}
	encoded, err := json.Marshal(episodeReportCache{Version: episodeReportCacheVersion, Data: report})
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, writeSensitiveFile(path, encoded))
	cached, err := retrieveCachedEpisodeLogs(t.Context(), opts, noEpisodeCacheRequest(t))
	require.NoError(t, err)
	require.Equal(t, report.EpisodeRetrieval.StartDate, cached.EpisodeRetrieval.StartDate)
	opts.Refresh = true
	_, err = retrieveCachedEpisodeLogs(t.Context(), opts, noEpisodeCacheRequest(t))
	require.ErrorContains(t, err, "--start-date must not be after --end-date")
}
