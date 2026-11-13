//go:build !integration

package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/github/gh-aw/pkg/console"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLogsStdinMetadataFetchFailure(t *testing.T) {
	t.Chdir(t.TempDir())
	originalRateLimit := fetchRateLimitForHostFunc
	t.Cleanup(func() {
		fetchRateLimitForHostFunc = originalRateLimit
	})
	fetchRateLimitForHostFunc = func(context.Context, string) (rateLimitResource, error) {
		return rateLimitResource{}, errors.New("rate limit unavailable in test")
	}
	binDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(binDir, "gh"), []byte(`#!/bin/sh
echo 'gh: Not Found (HTTP 404)' >&2
exit 1
`), 0o700))
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	for _, format := range []string{"text", "json", "jsonl"} {
		t.Run(format, func(t *testing.T) {
			opts := StdinLogsOptions{
				RunURLs:      []string{"123", "https://github.com/other/project/actions/runs/456"},
				RepoOverride: "owner/repo",
				OutputDir:    t.TempDir(),
				ArtifactSets: []string{"usage"},
				JSONOutput:   format == "json",
			}
			if format == "jsonl" {
				opts.Format = format
			}
			output, stderr := captureOutput(t, func() error {
				return DownloadWorkflowLogsFromStdin(t.Context(), opts)
			})
			for _, runID := range []int64{123, 456} {
				assert.Contains(t, stderr, fmt.Sprintf("Skipping run %d: failed to fetch metadata: workflow run %d not found", runID, runID))
			}
			switch format {
			case "text":
				assert.Empty(t, output)
			case "json":
				var data LogsData
				require.NoError(t, json.Unmarshal([]byte(output), &data))
				assert.Empty(t, data.Runs)
				assert.Equal(t, "No runs found. No valid runs could be loaded from the provided input.", data.Message)
			case "jsonl":
				events := decodeLogsJSONLEvents(t, output)
				require.Len(t, events, 7)
				assert.Equal(t, "started", events[0].Stage)
				for i, run := range []struct {
					id   int64
					repo string
				}{{123, "owner/repo"}, {456, "other/project"}} {
					fetching, failed := events[1+2*i], events[2+2*i]
					assert.Equal(t, "progress", fetching.Type)
					assert.Equal(t, "fetching", fetching.Stage)
					assert.Equal(t, run.id, fetching.RunID)
					assert.Equal(t, run.repo, fetching.Repository)
					assert.Equal(t, "progress", failed.Type)
					assert.Equal(t, "failed", failed.Stage)
					assert.Equal(t, fetching.RunID, failed.RunID)
					assert.Equal(t, fetching.Repository, failed.Repository)
					assert.Equal(t, fmt.Sprintf("workflow run %d not found. Please verify the run ID is correct and that you have access to the repository", run.id), failed.Message)
				}
				assert.Equal(t, "summary", events[5].Type)
				require.NotNil(t, events[5].Data)
				assert.Empty(t, events[5].Data.Runs)
				assert.Equal(t, "complete", events[6].Type)
			}
		})
	}
}

func TestLogsStdinEmptyRequestedRunsSummary(t *testing.T) {
	t.Chdir(t.TempDir())
	originalProcess, originalRateLimit := processConcurrentRunDownload, fetchRateLimitForHostFunc
	t.Cleanup(func() {
		processConcurrentRunDownload, fetchRateLimitForHostFunc = originalProcess, originalRateLimit
	})
	fetchRateLimitForHostFunc = func(context.Context, string) (rateLimitResource, error) {
		return rateLimitResource{}, errors.New("rate limit unavailable in test")
	}
	binDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(binDir, "gh"), []byte(`#!/bin/sh
case "$*" in
  *actions/runs/123*) echo '{"databaseId":123,"workflowName":"test","workflowPath":".github/workflows/test.lock.yml"}' ;;
  *) echo '{}' ;;
esac
`), 0o700))
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	processCalls := 0
	processConcurrentRunDownload = func(_ context.Context, run WorkflowRun, _ concurrentRunDownloadParams, _ *atomic.Int64, _ *console.ProgressBar) (DownloadResult, error) {
		processCalls++
		return DownloadResult{RunAnalysis: RunAnalysis{Run: run}, Skipped: true}, nil
	}

	for _, scenario := range []struct {
		name        string
		input       string
		message     string
		wantProcess int
	}{
		{name: "none loaded", input: "invalid-run", message: "No runs found. No valid runs could be loaded from the provided input."},
		{name: "none matched", input: "123", message: noRunsMessage("", false, false), wantProcess: 1},
	} {
		for _, format := range []string{"json", "jsonl"} {
			t.Run(scenario.name+"/"+format, func(t *testing.T) {
				processCalls = 0
				opts := StdinLogsOptions{
					RunURLs: []string{scenario.input}, RepoOverride: "owner/repo",
					OutputDir: t.TempDir(), ArtifactSets: []string{"usage"},
					JSONOutput: format == "json",
				}
				if format == "jsonl" {
					opts.Format = format
				}
				output, _ := captureOutput(t, func() error {
					return DownloadWorkflowLogsFromStdin(t.Context(), opts)
				})
				assert.Equal(t, scenario.wantProcess, processCalls)
				var data LogsData
				if format == "jsonl" {
					events := decodeLogsJSONLEvents(t, output)
					require.GreaterOrEqual(t, len(events), 3)
					summaryCount := 0
					for _, event := range events {
						if event.Type == "summary" {
							summaryCount++
						}
						assert.NotEqual(t, "run", event.Type)
					}
					require.Equal(t, 1, summaryCount)
					assert.Equal(t, "summary", events[len(events)-2].Type)
					require.NotNil(t, events[len(events)-2].Data)
					data = *events[len(events)-2].Data
					assert.Equal(t, "complete", events[len(events)-1].Type)
				} else {
					require.NoError(t, json.Unmarshal([]byte(output), &data))
				}
				assert.Empty(t, data.Runs)
				assert.Equal(t, scenario.message, data.Message)
			})
		}
	}
}
