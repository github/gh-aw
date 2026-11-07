//go:build !integration

package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCachedJSONDownloadResultRejectsNonAgenticWorkflow(t *testing.T) {
	updatedAt := time.Now()
	run := WorkflowRun{
		DatabaseID: 2,
		Status:     "completed",
		Conclusion: "success",
		Attempt:    1,
		UpdatedAt:  updatedAt,
		Repository: "owner/repo",
	}
	cachedRuns := cachedLogsRuns{
		2: {RunData: RunData{
			RunID:        2,
			WorkflowPath: ".github/workflows/ci.yml",
			Status:       "completed",
			Conclusion:   "success",
			RunAttempt:   "1",
			UpdatedAt:    updatedAt,
			Repository:   "owner/repo",
		}},
	}

	_, ok := cachedJSONDownloadResult(run, cachedRuns, runFilterOpts{})

	assert.False(t, ok)
}

func TestCachedJSONDownloadResultRejectsRegularCurrentWorkflow(t *testing.T) {
	updatedAt := time.Now()
	run := WorkflowRun{
		DatabaseID:   2,
		WorkflowPath: ".github/workflows/ci.yml",
		Status:       "completed",
		Conclusion:   "success",
		Attempt:      1,
		UpdatedAt:    updatedAt,
		Repository:   "owner/repo",
	}
	cachedRuns := cachedLogsRuns{
		2: {RunData: RunData{
			RunID:        2,
			WorkflowPath: ".github/workflows/stale.lock.yml",
			Status:       "completed",
			Conclusion:   "success",
			RunAttempt:   "1",
			UpdatedAt:    updatedAt,
			Repository:   "owner/repo",
		}},
	}

	_, ok := cachedJSONDownloadResult(run, cachedRuns, runFilterOpts{})

	assert.False(t, ok)
}

func TestApplyCachedRunWorkflowPathGateHonorsCurrentPath(t *testing.T) {
	t.Run("agentic current path is retained", func(t *testing.T) {
		result := &DownloadResult{RunAnalysis: RunAnalysis{Run: WorkflowRun{
			DatabaseID:   10,
			WorkflowPath: ".github/workflows/agentic.lock.yml",
		}}}

		applyCachedRunWorkflowPathGate(result, WorkflowRun{WorkflowPath: ".github/workflows/agentic.lock.yml"}, false, false)

		assert.False(t, result.Skipped)
		assert.Equal(t, ".github/workflows/agentic.lock.yml", result.Run.WorkflowPath)
	})

	t.Run("regular current path is skipped", func(t *testing.T) {
		result := &DownloadResult{RunAnalysis: RunAnalysis{Run: WorkflowRun{
			DatabaseID:   11,
			WorkflowPath: ".github/workflows/stale.lock.yml",
		}}}

		applyCachedRunWorkflowPathGate(result, WorkflowRun{WorkflowPath: ".github/workflows/ci.yml"}, true, false)

		assert.True(t, result.Skipped)
		assert.Empty(t, result.Run.WorkflowPath)
	})

	t.Run("unverified empty current path clears stale cached path", func(t *testing.T) {
		result := &DownloadResult{RunAnalysis: RunAnalysis{Run: WorkflowRun{
			DatabaseID:   12,
			WorkflowPath: ".github/workflows/stale.lock.yml",
		}}}

		applyCachedRunWorkflowPathGate(result, WorkflowRun{}, false, false)

		assert.True(t, result.Skipped)
		assert.Empty(t, result.Run.WorkflowPath)
	})

	t.Run("verified empty current path preserves metadata path", func(t *testing.T) {
		result := &DownloadResult{RunAnalysis: RunAnalysis{Run: WorkflowRun{
			DatabaseID:   13,
			WorkflowPath: ".github/workflows/agentic.lock.yml",
		}}}

		applyCachedRunWorkflowPathGate(result, WorkflowRun{}, true, false)

		assert.False(t, result.Skipped)
		assert.Equal(t, ".github/workflows/agentic.lock.yml", result.Run.WorkflowPath)
	})
}

func TestTryLoadCachedRunResultBypassesForExplicitEvalsArtifactRequest(t *testing.T) {
	t.Parallel()
	runOutputDir := t.TempDir()
	summary := &RunSummary{
		CLIVersion:  GetVersion(),
		RunID:       123,
		ProcessedAt: time.Now(),
		RunAnalysis: RunAnalysis{
			Run: WorkflowRun{
				DatabaseID: 123,
			},
		},
	}
	require.NoError(t, saveRunSummary(runOutputDir, summary, false))
	require.NoError(t, markArtifactDownloaded(runOutputDir, string(ArtifactSetAll)))

	result, ok := tryLoadCachedRunResult(context.Background(), WorkflowRun{DatabaseID: 123}, runOutputDir, concurrentRunDownloadParams{
		evalsOnly:              false,
		evalsArtifactRequested: true,
		verbose:                false,
	})
	assert.False(t, ok, "cache should be bypassed so fallback download can run for explicit --artifacts evals")
	assert.Nil(t, result)
}

func TestTryLoadCachedRunResultUsesCacheWhenEvalsNotRequested(t *testing.T) {
	t.Parallel()
	runOutputDir := t.TempDir()
	summary := &RunSummary{
		CLIVersion:  GetVersion(),
		RunID:       124,
		ProcessedAt: time.Now(),
		RunAnalysis: RunAnalysis{
			Run: WorkflowRun{
				DatabaseID: 124,
			},
		},
	}

	require.NoError(t, saveRunSummary(runOutputDir, summary, false))
	require.NoError(t, markArtifactDownloaded(runOutputDir, string(ArtifactSetAll)))

	result, ok := tryLoadCachedRunResult(context.Background(), WorkflowRun{DatabaseID: 124}, runOutputDir, concurrentRunDownloadParams{
		evalsOnly:              false,
		evalsArtifactRequested: false,
		verbose:                false,
	})
	require.True(t, ok)
	require.NotNil(t, result)
	assert.True(t, result.Cached)
}

func TestTryLoadCachedRunResultRejectsStaleRegularWorkflowPath(t *testing.T) {
	t.Parallel()
	runOutputDir := t.TempDir()
	summary := &RunSummary{
		CLIVersion:  GetVersion(),
		RunID:       130,
		ProcessedAt: time.Now(),
		RunAnalysis: RunAnalysis{
			Run: WorkflowRun{
				DatabaseID:   130,
				WorkflowPath: ".github/workflows/stale.lock.yml",
			},
		},
	}
	require.NoError(t, saveRunSummary(runOutputDir, summary, false))
	require.NoError(t, markArtifactDownloaded(runOutputDir, string(ArtifactSetAll)))

	result, ok := tryLoadCachedRunResult(context.Background(), WorkflowRun{
		DatabaseID:   130,
		WorkflowPath: ".github/workflows/ci.yml",
	}, runOutputDir, concurrentRunDownloadParams{})

	require.True(t, ok)
	require.NotNil(t, result)
	assert.True(t, result.Cached)
	assert.True(t, result.Skipped)
	assert.Empty(t, result.Run.WorkflowPath)

	reloaded, ok := loadRunSummary(runOutputDir, false)
	require.True(t, ok)
	assert.Equal(t, ".github/workflows/stale.lock.yml", reloaded.Run.WorkflowPath)
}

func TestTryLoadCachedRunResultDoesNotPersistRejectedCurrentPathAfterMetadataRefresh(t *testing.T) {
	t.Parallel()
	runOutputDir := t.TempDir()
	summary := &RunSummary{
		CLIVersion:  GetVersion(),
		RunID:       132,
		ProcessedAt: time.Now(),
		RunAnalysis: RunAnalysis{
			Run: WorkflowRun{
				DatabaseID:   132,
				WorkflowPath: ".github/workflows/stale.lock.yml",
			},
		},
	}
	require.NoError(t, saveRunSummary(runOutputDir, summary, false))
	require.NoError(t, markArtifactDownloaded(runOutputDir, string(ArtifactSetAll)))
	require.NoError(t, os.WriteFile(
		filepath.Join(runOutputDir, runAPIResponseFileName),
		[]byte(`{"id":132,"html_url":"https://github.com/owner/repo/actions/runs/132","status":"completed","conclusion":"success","name":"Refreshed Agentic","path":".github/workflows/refreshed.lock.yml"}`),
		0o600,
	))

	result, ok := tryLoadCachedRunResult(context.Background(), WorkflowRun{
		DatabaseID:   132,
		URL:          "https://github.com/owner/repo/actions/runs/132",
		WorkflowPath: ".github/workflows/ci.yml",
	}, runOutputDir, concurrentRunDownloadParams{})

	require.True(t, ok)
	require.NotNil(t, result)
	assert.True(t, result.Skipped)
	assert.Empty(t, result.Run.WorkflowPath)

	reloaded, ok := loadRunSummary(runOutputDir, false)
	require.True(t, ok)
	assert.Equal(t, ".github/workflows/refreshed.lock.yml", reloaded.Run.WorkflowPath)
	assert.NotEqual(t, ".github/workflows/ci.yml", reloaded.Run.WorkflowPath)
}

func TestTryLoadCachedRunResultClearsStalePathWhenMetadataOmitsPath(t *testing.T) {
	t.Parallel()
	runOutputDir := t.TempDir()
	summary := &RunSummary{
		CLIVersion:  GetVersion(),
		RunID:       131,
		ProcessedAt: time.Now(),
		RunAnalysis: RunAnalysis{
			Run: WorkflowRun{
				DatabaseID:   131,
				WorkflowPath: ".github/workflows/stale.lock.yml",
			},
		},
	}
	require.NoError(t, saveRunSummary(runOutputDir, summary, false))
	require.NoError(t, markArtifactDownloaded(runOutputDir, string(ArtifactSetAll)))
	require.NoError(t, os.WriteFile(
		filepath.Join(runOutputDir, runAPIResponseFileName),
		[]byte(`{"id":131,"status":"completed","conclusion":"success"}`),
		0o600,
	))

	result, ok := tryLoadCachedRunResult(context.Background(), WorkflowRun{DatabaseID: 131}, runOutputDir, concurrentRunDownloadParams{})

	require.True(t, ok)
	require.NotNil(t, result)
	assert.True(t, result.Cached)
	assert.True(t, result.Skipped)
	assert.Empty(t, result.Run.WorkflowPath)

	reloaded, ok := loadRunSummary(runOutputDir, false)
	require.True(t, ok)
	assert.Equal(t, ".github/workflows/stale.lock.yml", reloaded.Run.WorkflowPath)
}

func TestTryLoadCachedRunResultBackfillsGatewaySteeringEvents(t *testing.T) {
	t.Parallel()
	runOutputDir := t.TempDir()
	summary := &RunSummary{
		CLIVersion:  GetVersion(),
		RunID:       301,
		ProcessedAt: time.Now(),
		RunAnalysis: RunAnalysis{
			Run: WorkflowRun{DatabaseID: 301},
		},
	}
	require.NoError(t, saveRunSummary(runOutputDir, summary, false))
	require.NoError(t, markArtifactDownloaded(runOutputDir, string(ArtifactSetAll)))

	logsDir := filepath.Join(runOutputDir, "sandbox", "firewall", "audit", "api-proxy-logs")
	require.NoError(t, os.MkdirAll(logsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(logsDir, "events.jsonl"), []byte(
		`{"timestamp":"2026-09-23T12:00:00Z","event":"token_steering","message":"[AWF TOKEN WARNING] You are running out of AI Credits."}`+"\n",
	), 0o644))

	result, ok := tryLoadCachedRunResult(context.Background(), WorkflowRun{DatabaseID: 301}, runOutputDir, concurrentRunDownloadParams{})
	require.True(t, ok)
	require.NotNil(t, result)
	require.Len(t, result.GatewaySteeringEvents, 1)
	assert.Equal(t, tokenSteeringEventName, result.GatewaySteeringEvents[0].Type)

	reloaded, ok := loadRunSummary(runOutputDir, false)
	require.True(t, ok)
	require.Len(t, reloaded.GatewaySteeringEvents, 1)
}

func TestTryLoadCachedRunResultBypassesCacheWhenRequestedArtifactIsMissing(t *testing.T) {
	t.Parallel()
	runOutputDir := t.TempDir()
	summary := &RunSummary{
		CLIVersion:  GetVersion(),
		RunID:       125,
		ProcessedAt: time.Now(),
		RunAnalysis: RunAnalysis{
			Run: WorkflowRun{DatabaseID: 125},
		},
	}
	require.NoError(t, saveRunSummary(runOutputDir, summary, false))
	require.NoError(t, markArtifactDownloaded(runOutputDir, "activation"))

	result, ok := tryLoadCachedRunResult(context.Background(), WorkflowRun{DatabaseID: 125}, runOutputDir, concurrentRunDownloadParams{
		artifactFilter: []string{"agent"},
	})

	assert.False(t, ok)
	assert.Nil(t, result)
}

func TestTryLoadCachedRunResultUsesCacheWhenRequestedArtifactsArePresent(t *testing.T) {
	t.Parallel()
	runOutputDir := t.TempDir()
	summary := &RunSummary{
		CLIVersion:  GetVersion(),
		RunID:       126,
		ProcessedAt: time.Now(),
		RunAnalysis: RunAnalysis{
			Run: WorkflowRun{DatabaseID: 126},
		},
	}
	require.NoError(t, saveRunSummary(runOutputDir, summary, false))
	require.NoError(t, markArtifactDownloaded(runOutputDir, "activation"))
	require.NoError(t, markArtifactDownloaded(runOutputDir, "usage"))

	result, ok := tryLoadCachedRunResult(context.Background(), WorkflowRun{DatabaseID: 126}, runOutputDir, concurrentRunDownloadParams{
		artifactFilter: []string{"activation", "usage"},
	})

	require.True(t, ok)
	require.NotNil(t, result)
	assert.True(t, result.Cached)
}

func TestTryLoadCachedRunResultRequiresCompleteMarkerForAllArtifacts(t *testing.T) {
	t.Parallel()
	runOutputDir := t.TempDir()
	summary := &RunSummary{
		CLIVersion:  GetVersion(),
		RunID:       127,
		ProcessedAt: time.Now(),
		RunAnalysis: RunAnalysis{
			Run: WorkflowRun{DatabaseID: 127},
		},
	}
	require.NoError(t, saveRunSummary(runOutputDir, summary, false))
	require.NoError(t, markArtifactDownloaded(runOutputDir, "activation"))
	require.NoError(t, markArtifactDownloaded(runOutputDir, "usage"))

	result, ok := tryLoadCachedRunResult(context.Background(), WorkflowRun{DatabaseID: 127}, runOutputDir, concurrentRunDownloadParams{})
	assert.False(t, ok)
	assert.Nil(t, result)

	require.NoError(t, markArtifactDownloaded(runOutputDir, string(ArtifactSetAll)))
	result, ok = tryLoadCachedRunResult(context.Background(), WorkflowRun{DatabaseID: 127}, runOutputDir, concurrentRunDownloadParams{})
	require.True(t, ok)
	require.NotNil(t, result)
	assert.True(t, result.Cached)
}

// TestTryLoadCachedRunResultPersistsSafeItemsCountAfterBackfill verifies that when
// tryLoadCachedRunResult heals a stale SafeItemsCount (0 → N) via backfillCacheHitIfNeeded,
// the healed value is written back to run_summary.json on disk so downstream readers
// (e.g. api-consumption-report) see the correct count without falling back to the
// activity summary.
func TestTryLoadCachedRunResultPersistsSafeItemsCountAfterBackfill(t *testing.T) {
	t.Parallel()
	runOutputDir := t.TempDir()

	// Write a run_summary.json with SafeItemsCount=0 (stale cache).
	summary := &RunSummary{
		CLIVersion:  GetVersion(),
		RunID:       200,
		ProcessedAt: time.Now(),
		RunAnalysis: RunAnalysis{
			Run: WorkflowRun{
				DatabaseID:     200,
				SafeItemsCount: 0,
			},
		},
	}
	require.NoError(t, saveRunSummary(runOutputDir, summary, false))
	require.NoError(t, markArtifactDownloaded(runOutputDir, string(ArtifactSetAll)))

	// Write a usage/activity/summary.json so backfill has something to pull from.
	activityPath := filepath.Join(runOutputDir, "usage", "activity", "summary.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(activityPath), 0o755))
	require.NoError(t, os.WriteFile(activityPath, []byte(`{
		"schema":"usage-activity-summary/v1",
		"safe_outputs":{"total_items":5,"items_by_type":{"create_issue":5}}
	}`), 0o644))

	result, ok := tryLoadCachedRunResult(context.Background(), WorkflowRun{DatabaseID: 200}, runOutputDir, concurrentRunDownloadParams{})
	require.True(t, ok)
	require.NotNil(t, result)

	// In-memory value should be healed.
	assert.Equal(t, 5, result.Run.SafeItemsCount, "in-memory SafeItemsCount should be backfilled")

	// The on-disk run_summary.json must also reflect the healed value.
	reloaded, ok := loadRunSummary(runOutputDir, false)
	require.True(t, ok)
	assert.Equal(t, 5, reloaded.Run.SafeItemsCount, "on-disk run_summary.json SafeItemsCount should be persisted after backfill")
}
