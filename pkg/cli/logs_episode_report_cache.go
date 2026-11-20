package cli

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"time"

	"github.com/github/gh-aw/pkg/constants"
)

const episodeReportCacheVersion = 2

type episodeReportCache struct {
	Version int      `json:"version"`
	Data    LogsData `json:"data"`
}

func retrieveCachedEpisodeLogs(ctx context.Context, opts EpisodeRetrievalOptions, request episodeAPIRequest) (LogsData, error) {
	if err := ctx.Err(); err != nil {
		return LogsData{}, err
	}
	path, err := episodeReportCachePath(opts)
	if err != nil {
		return LogsData{}, err
	}
	if !opts.Refresh {
		data, found, err := readEpisodeReportCache(path, opts.SeedRunID)
		if err != nil || found {
			return data, err
		}
	}
	data, err := retrieveEpisodeLogs(ctx, opts, request)
	if err != nil {
		return LogsData{}, err
	}
	data.EpisodeRetrieval.CollectedAt = time.Now().UTC()
	encoded, err := json.Marshal(episodeReportCache{Version: episodeReportCacheVersion, Data: data})
	if err != nil {
		return LogsData{}, fmt.Errorf("encode episode cache: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), constants.DirPermSensitive); err != nil {
		return LogsData{}, fmt.Errorf("create episode cache directory: %w", err)
	}
	if err := writeSensitiveFile(path, encoded); err != nil {
		return LogsData{}, fmt.Errorf("save episode cache: %w", err)
	}
	return data, nil
}

func episodeReportCachePath(opts EpisodeRetrievalOptions) (string, error) {
	collector := &episodeCollector{opts: opts}
	if err := collector.initializeScope(); err != nil {
		return "", err
	}
	key := opts
	key.Repository = path.Join(collector.report.Host, collector.opts.Repository)
	key.AllowRepositories = slices.Clone(collector.report.Repositories)
	slices.Sort(key.AllowRepositories)
	key.OutputDir, key.Refresh = "", false
	if key.StartDate == "" {
		key.StartDate = "-7d"
	}
	encoded, err := json.Marshal(key)
	if err != nil {
		return "", fmt.Errorf("encode episode cache key: %w", err)
	}
	return filepath.Join(opts.OutputDir, "episodes", collector.report.Host, collector.opts.Repository,
		"run-"+strconv.FormatInt(opts.SeedRunID, 10), fmt.Sprintf("report-v%d-%x.json", episodeReportCacheVersion, sha256.Sum256(encoded))), nil
}

func readEpisodeReportCache(path string, seedRunID int64) (LogsData, bool, error) {
	encoded, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return LogsData{}, false, nil
	}
	if err != nil {
		return LogsData{}, false, fmt.Errorf("read episode cache (use --refresh to collect again): %w", err)
	}
	var cache episodeReportCache
	if err := json.Unmarshal(encoded, &cache); err != nil {
		return LogsData{}, false, fmt.Errorf("decode episode cache (use --refresh to collect again): %w", err)
	}
	valid := cache.Version == episodeReportCacheVersion && cache.Data.EpisodeRetrieval != nil
	if valid {
		valid = !cache.Data.EpisodeRetrieval.CollectedAt.IsZero() && cache.Data.EpisodeRetrieval.EpisodeID != "" &&
			slices.ContainsFunc(cache.Data.Episodes, func(episode EpisodeData) bool {
				return episode.EpisodeID == cache.Data.EpisodeRetrieval.EpisodeID && slices.Contains(episode.RunIDs, seedRunID)
			}) && slices.ContainsFunc(cache.Data.Runs, func(run RunData) bool { return run.RunID == seedRunID })
	}
	if !valid {
		return LogsData{}, false, errors.New("invalid episode cache: use --refresh to collect again")
	}
	cache.Data.EpisodeRetrieval.Cached = true
	return cache.Data, true, nil
}
