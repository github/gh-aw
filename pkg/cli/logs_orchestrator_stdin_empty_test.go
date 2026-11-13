//go:build !integration

package cli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/github/gh-aw/pkg/console"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
