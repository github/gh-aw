//go:build !integration

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/github/gh-aw/pkg/console"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLogsJSONLFlags(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		args     []string
		conflict bool
	}{
		{args: []string{"--format", "jsonl"}},
		{args: []string{"--stdin", "--format", "jsonl", "--cached-jsonl", "cache.jsonl"}},
		{args: []string{"--format", "jsonl", "--json"}, conflict: true},
		{args: []string{"--format", "jsonl", "--report-file", "report.md"}, conflict: true},
	} {
		t.Run(strings.Join(tt.args, " "), func(t *testing.T) {
			cmd := NewLogsCommand()
			require.NoError(t, cmd.ParseFlags(tt.args))
			opts, err := loadCommonLogsOptions(cmd)
			if tt.conflict {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, "jsonl", opts.Format)
			assert.False(t, opts.JSONOutput)
			stdinOpts, err := loadStdinLogsOptions(cmd)
			require.NoError(t, err)
			assert.Equal(t, "jsonl", stdinOpts.Format)
		})
	}
	t.Run("no standalone jsonl flag", func(t *testing.T) {
		cmd := NewLogsCommand()
		require.ErrorContains(t, cmd.ParseFlags([]string{"--jsonl"}), "unknown flag: --jsonl")
	})
}

func TestLogsJSONLOrchestrationEmptyResults(t *testing.T) {
	t.Chdir(t.TempDir())
	originalFetch, originalRateLimit := logsFetchWorkflowRunBatch, fetchRateLimitForHostFunc
	t.Cleanup(func() {
		logsFetchWorkflowRunBatch, fetchRateLimitForHostFunc = originalFetch, originalRateLimit
	})
	logsFetchWorkflowRunBatch = func(context.Context, LogsDownloadOptions, string, int, bool) (workflowRunBatch, error) {
		return workflowRunBatch{batchSize: 10}, nil
	}
	fetchRateLimitForHostFunc = func(context.Context, string) (rateLimitResource, error) {
		return rateLimitResource{}, errors.New("rate limit unavailable in test")
	}
	for _, mode := range []string{"discovery", "targets", "stdin"} {
		t.Run(mode, func(t *testing.T) {
			output, _ := captureOutput(t, func() error {
				opts := LogsDownloadOptions{Format: "jsonl", Count: 2, OutputDir: t.TempDir(), ArtifactSets: []string{"usage"}, RepoOverride: "owner/repo"}
				switch mode {
				case "discovery":
					return DownloadWorkflowLogs(context.Background(), opts)
				case "targets":
					return DownloadWorkflowLogsForTargets(context.Background(), opts, []logsWorkflowTarget{
						{workflowName: "one", repoOverride: "owner/repo"},
						{workflowName: "two", repoOverride: "owner/repo"},
					}, nil)
				default:
					return DownloadWorkflowLogsFromStdin(context.Background(), StdinLogsOptions{Format: "jsonl", OutputDir: opts.OutputDir})
				}
			})
			events := decodeLogsJSONLEvents(t, output)
			require.GreaterOrEqual(t, len(events), 3)
			assert.Equal(t, "started", events[0].Stage)
			assert.Equal(t, "summary", events[len(events)-2].Type)
			require.NotNil(t, events[len(events)-2].Data)
			assert.Empty(t, events[len(events)-2].Data.Runs)
			assert.Equal(t, "complete", events[len(events)-1].Type)
		})
	}
}

func TestLogsJSONLStreamsRunBeforeBatchFinishes(t *testing.T) {
	originalProcess, originalBuild := processConcurrentRunDownload, buildLogsProcessedRun
	t.Cleanup(func() {
		processConcurrentRunDownload, buildLogsProcessedRun = originalProcess, originalBuild
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	stream := newLogsJSONLStream(writer, false)
	ctx = context.WithValue(ctx, logsJSONLContextKey{}, stream)
	events := make(chan logsJSONLEvent, 10)
	decodeDone := make(chan error, 1)
	go func() {
		decoder := json.NewDecoder(reader)
		for {
			var event logsJSONLEvent
			if err := decoder.Decode(&event); err != nil {
				decodeDone <- err
				return
			}
			events <- event
		}
	}()
	releaseSecond := make(chan struct{})
	processConcurrentRunDownload = func(ctx context.Context, run WorkflowRun, _ concurrentRunDownloadParams, _ *atomic.Int64, _ *console.ProgressBar) (DownloadResult, error) {
		if run.DatabaseID == 2 {
			select {
			case <-releaseSecond:
			case <-ctx.Done():
				return canceledDownloadResult(run, ctx.Err()), nil
			}
		}
		return DownloadResult{RunAnalysis: RunAnalysis{Run: run}}, nil
	}
	buildLogsProcessedRun = func(_ context.Context, result DownloadResult, _, _ bool) ProcessedRun {
		return ProcessedRun{Run: result.Run}
	}
	done := make(chan []ProcessedRun, 1)
	go func() {
		processed, _, _ := appendProcessedWorkflowRuns(ctx, nil, []WorkflowRun{
			{DatabaseID: 1, WorkflowPath: ".github/workflows/test.lock.yml"},
			{DatabaseID: 2, WorkflowPath: ".github/workflows/test.lock.yml"},
		}, 0, processWorkflowRunBatchOptions{count: 2, maxConcurrentDownloads: 2})
		done <- processed
	}()
	// Always join the worker before restoring the package-level test hooks.
	defer func() {
		cancel()
		<-done
		writer.Close()
		<-decodeDone
	}()
	for {
		select {
		case event := <-events:
			if event.Type != "run" {
				continue
			}
			require.NotNil(t, event.Run)
			assert.Equal(t, int64(1), event.Run.RunID)
			select {
			case runs := <-done:
				done <- runs
				t.Fatal("batch finished while its second download was blocked")
			default:
			}
			close(releaseSecond)
			select {
			case runs := <-done:
				done <- runs
				require.Len(t, runs, 2)
			case <-ctx.Done():
				t.Fatal("downloads did not finish")
			}
			return
		case <-ctx.Done():
			t.Fatal("no run was streamed before batch completion")
		}
	}
}

func TestLogsJSONLConcurrentProgressAndCachedRun(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	stream := newLogsJSONLStream(&output, false)
	ctx := context.WithValue(context.Background(), logsJSONLContextKey{}, stream)
	var workers sync.WaitGroup
	for id := range 20 {
		workers.Go(func() {
			emitLogsDownloadProgress(ctx, DownloadResult{
				RunAnalysis: RunAnalysis{Run: WorkflowRun{DatabaseID: int64(id + 1), Repository: "owner/repo"}},
				Cached:      true,
			})
		})
	}
	workers.Wait()
	cached := RunData{RunID: 42, WorkflowName: "cached", Repository: "owner/repo", WorkflowPath: ".github/workflows/cached.lock.yml"}
	stream.run(ProcessedRun{Run: WorkflowRun{DatabaseID: 42}, cachedData: &cached})
	events := decodeLogsJSONLEvents(t, output.String())
	require.Len(t, events, 21)
	for _, event := range events[:20] {
		assert.Equal(t, "progress", event.Type)
		assert.True(t, event.Cached)
		assert.Equal(t, "owner/repo", event.Repository)
	}
	require.NotNil(t, events[20].Run)
	assert.Equal(t, int64(42), events[20].Run.RunID)
}

func TestLogsJSONLDownloadProgressStages(t *testing.T) {
	originalProcess := processConcurrentRunDownload
	t.Cleanup(func() { processConcurrentRunDownload = originalProcess })
	processConcurrentRunDownload = func(context.Context, WorkflowRun, concurrentRunDownloadParams, *atomic.Int64, *console.ProgressBar) (DownloadResult, error) {
		panic("download failed")
	}
	run := WorkflowRun{DatabaseID: 42, Repository: "owner/repo"}
	panicked := executeConcurrentRunDownload(context.Background(), run, concurrentRunDownloadParams{}, nil, nil)
	require.True(t, panicked.Skipped)
	require.ErrorContains(t, panicked.Error, "run download panicked")

	for _, tt := range []struct {
		name    string
		result  DownloadResult
		stage   string
		message string
	}{
		{name: "canceled", result: canceledDownloadResult(run, context.Canceled), stage: "skipped"},
		{name: "deadline exceeded", result: canceledDownloadResult(run, context.DeadlineExceeded), stage: "skipped"},
		{name: "wrapped cancellation", result: canceledDownloadResult(run, errors.Join(errors.New("download interrupted"), context.Canceled)), stage: "skipped"},
		{name: "panicked", result: panicked, stage: "failed", message: panicked.Error.Error()},
		{name: "skipped", result: DownloadResult{RunAnalysis: RunAnalysis{Run: run}, Skipped: true}, stage: "skipped"},
		{
			name: "error without skipped flag",
			result: DownloadResult{
				RunAnalysis: RunAnalysis{Run: run},
				Error:       context.Canceled,
			},
			stage: "failed", message: context.Canceled.Error(),
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var output bytes.Buffer
			stream := newLogsJSONLStream(&output, false)
			ctx := context.WithValue(context.Background(), logsJSONLContextKey{}, stream)
			emitLogsDownloadProgress(ctx, tt.result)
			events := decodeLogsJSONLEvents(t, output.String())
			require.Len(t, events, 1)
			assert.Equal(t, "progress", events[0].Type)
			assert.Equal(t, tt.stage, events[0].Stage)
			assert.Equal(t, tt.message, events[0].Message)
			assert.Equal(t, run.DatabaseID, events[0].RunID)
			assert.Equal(t, run.Repository, events[0].Repository)
		})
	}
}

func TestLogsJSONLSummaryAndTerminalEvents(t *testing.T) {
	t.Parallel()
	for _, verbose := range []bool{false, true} {
		t.Run(strconv.FormatBool(verbose), func(t *testing.T) {
			var output bytes.Buffer
			stream := newLogsJSONLStream(&output, verbose)
			continuation := &ContinuationData{BeforeRunID: 123, Message: "Count limit reached"}
			err := renderLogsOutput(nil, renderLogsOutputOptions{
				format: "jsonl", jsonlStream: stream, episodes: true,
				continuation: continuation, message: "No matches",
			})
			stream.finish(&err)
			require.NoError(t, err)
			events := decodeLogsJSONLEvents(t, output.String())
			require.Len(t, events, 2)
			assert.Equal(t, "summary", events[0].Type)
			require.NotNil(t, events[0].Data)
			assert.Empty(t, events[0].Data.Runs)
			require.NotNil(t, events[0].Data.Continuation)
			assert.Equal(t, int64(123), events[0].Data.Continuation.BeforeRunID)
			assert.Equal(t, "complete", events[1].Type)
		})
	}
	var output bytes.Buffer
	stream := newLogsJSONLStream(&output, false)
	err := errors.New("collection failed")
	stream.finish(&err)
	require.ErrorContains(t, err, "collection failed")
	events := decodeLogsJSONLEvents(t, output.String())
	require.Len(t, events, 1)
	assert.Equal(t, "error", events[0].Type)
	assert.Equal(t, "collection failed", events[0].Message)
}

func TestLogsJSONLRunMatchesFinalReportCounts(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	stream := newLogsJSONLStream(&output, false)
	run := ProcessedRun{
		Run:          WorkflowRun{DatabaseID: 42, WorkflowPath: ".github/workflows/test.lock.yml"},
		MissingTools: []MissingToolReport{{}},
		MissingData:  []MissingDataReport{{}},
		Noops:        []NoopReport{{}},
	}
	stream.run(run)
	require.NoError(t, renderLogsOutput([]ProcessedRun{run}, renderLogsOutputOptions{format: "jsonl", jsonlStream: stream}))
	events := decodeLogsJSONLEvents(t, output.String())
	require.Len(t, events, 2)
	require.NotNil(t, events[0].Run)
	require.NotNil(t, events[1].Data)
	require.Len(t, events[1].Data.Runs, 1)
	assert.Equal(t, 1, events[0].Run.MissingToolCount)
	assert.Equal(t, 1, events[0].Run.MissingDataCount)
	assert.Equal(t, events[1].Data.Runs[0].MissingToolCount, events[0].Run.MissingToolCount)
	assert.Equal(t, events[1].Data.Runs[0].MissingDataCount, events[0].Run.MissingDataCount)
}

type failingLogsJSONLWriter struct{}

func (failingLogsJSONLWriter) Write([]byte) (int, error) {
	return 0, io.ErrClosedPipe
}

func TestLogsJSONLWriteFailureCancelsCollection(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream := newLogsJSONLStream(failingLogsJSONLWriter{}, false)
	stream.cancel = cancel
	stream.emit(logsJSONLEvent{Type: "progress"})
	require.ErrorIs(t, ctx.Err(), context.Canceled)
	var err error
	stream.finish(&err)
	require.ErrorIs(t, err, io.ErrClosedPipe)
	require.ErrorContains(t, err, "failed to write logs JSONL output")
}

func decodeLogsJSONLEvents(t *testing.T, output string) []logsJSONLEvent {
	t.Helper()
	var events []logsJSONLEvent
	for line := range strings.SplitSeq(strings.TrimSpace(output), "\n") {
		var event logsJSONLEvent
		require.NoError(t, json.Unmarshal([]byte(line), &event), "each line must be a complete JSON object")
		events = append(events, event)
	}
	return events
}
