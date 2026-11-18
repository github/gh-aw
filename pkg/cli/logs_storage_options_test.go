//go:build !integration

package cli

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLogsStorageOptionsNormalization(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		mb   int
		set  bool
		want int
	}{
		{name: "omitted", want: 1024},
		{name: "explicit unlimited", set: true},
		{name: "positive without presence flag", mb: 2048, want: 2048},
		{name: "explicit positive", mb: 4096, set: true, want: 4096},
		{name: "negative still rejected", mb: -1, want: -1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			opts := normalizeLogsDownloadOptions(LogsDownloadOptions{MaxStorageMB: tt.mb, MaxStorageMBSet: tt.set})
			assert.Equal(t, tt.want, opts.MaxStorageMB)
			assert.Equal(t, opts, normalizeLogsDownloadOptions(opts))
			stdinOpts := normalizeStdinLogsOptions(StdinLogsOptions{MaxStorageMB: tt.mb, MaxStorageMBSet: tt.set})
			assert.Equal(t, tt.want, stdinOpts.MaxStorageMB)
			assert.Equal(t, stdinOpts, normalizeStdinLogsOptions(stdinOpts))
		})
	}
}

func TestDownloadWorkflowLogsStorageDefaultsAndOverrides(t *testing.T) {
	originalFetch := logsFetchWorkflowRunBatch
	originalRateLimit := fetchRateLimitForHostFunc
	t.Cleanup(func() {
		logsFetchWorkflowRunBatch = originalFetch
		fetchRateLimitForHostFunc = originalRateLimit
	})
	stop := errors.New("stop after observing normalized options")
	fetchRateLimitForHostFunc = func(context.Context, string) (rateLimitResource, error) {
		return rateLimitResource{}, stop
	}
	var observed LogsDownloadOptions
	logsFetchWorkflowRunBatch = func(_ context.Context, opts LogsDownloadOptions, _ string, _ int, _ bool) (workflowRunBatch, error) {
		observed = opts
		return workflowRunBatch{}, stop
	}
	for _, tt := range []struct {
		name string
		mb   int
		set  bool
		want int
	}{
		{name: "omitted", want: 1024},
		{name: "unlimited", set: true},
		{name: "custom", mb: 2048, want: 2048},
	} {
		t.Run(tt.name, func(t *testing.T) {
			opts := LogsDownloadOptions{
				Count: 1, OutputDir: t.TempDir(), MaxStorageMB: tt.mb,
				MaxStorageMBSet: tt.set, skipEnsureGitignore: true,
			}
			require.ErrorIs(t, DownloadWorkflowLogs(context.Background(), opts), stop)
			assert.Equal(t, tt.want, observed.MaxStorageMB)
			runtime, err := prepareLogsDownload(context.Background(), opts)
			require.NoError(t, err)
			defer cancelLogsDownload(runtime.timeoutCancel)
			if tt.want == 0 {
				assert.Nil(t, runtime.storageLimit)
			} else {
				require.NotNil(t, runtime.storageLimit)
				assert.Equal(t, int64(tt.want)*bytesPerMegabyte, runtime.storageLimit.maxBytes)
			}
		})
	}
	t.Run("models refresh uses shared default", func(t *testing.T) {
		require.ErrorIs(t, refreshObservedArtifacts(context.Background(), t.TempDir(), 1, ""), stop)
		assert.Equal(t, 1024, observed.MaxStorageMB)
	})
}

func TestCollectLogsTargetsStorageDefaultsAndOverrides(t *testing.T) {
	originalCollector := collectWorkflowLogsForTarget
	t.Cleanup(func() { collectWorkflowLogsForTarget = originalCollector })
	for _, tt := range []struct {
		name string
		mb   int
		set  bool
		want int
	}{
		{name: "omitted", want: 1024},
		{name: "unlimited", set: true},
		{name: "custom", mb: 2048, want: 2048},
	} {
		t.Run(tt.name, func(t *testing.T) {
			collectWorkflowLogsForTarget = func(_ context.Context, opts LogsDownloadOptions) (workflowLogsResult, error) {
				assert.Equal(t, tt.want, opts.MaxStorageMB)
				if tt.want == 0 {
					assert.Nil(t, opts.storageLimit)
				} else {
					require.NotNil(t, opts.storageLimit)
					assert.Equal(t, int64(tt.want)*bytesPerMegabyte, opts.storageLimit.maxBytes)
				}
				return workflowLogsResult{continuation: buildContinuationIfNeeded(nil, true, false, false, false, logsTargetContinuationOptions(opts))}, nil
			}
			results := collectLogsTargets(context.Background(), LogsDownloadOptions{
				OutputDir: t.TempDir(), MaxStorageMB: tt.mb, MaxStorageMBSet: tt.set,
			}, []logsWorkflowTarget{{workflowName: "first"}, {workflowName: "second"}})
			require.Len(t, results, 2)
			for _, result := range results {
				require.NotNil(t, result.result.continuation)
				assert.Equal(t, tt.want, result.result.continuation.MaxStorageMB)
			}
		})
	}
}

func TestLogsContinuationStorageBudgetRoundTripThroughMCP(t *testing.T) {
	t.Parallel()
	for _, mb := range []int{0, 1024, 2048} {
		continuation := buildContinuationIfNeeded(nil, true, false, false, false, continuationOptions{maxStorageMB: mb})
		data, err := json.Marshal(continuation)
		require.NoError(t, err)
		var args logsArgs
		require.NoError(t, json.Unmarshal(data, &args))
		require.NotNil(t, args.MaxStorageMB, "continuation must preserve explicit unlimited storage")
		assert.Equal(t, mb, *args.MaxStorageMB)
		cmdArgs, _, _, pathErr := buildLogsCommandArgs(context.Background(), args)
		require.NoError(t, pathErr)
		cmd := NewLogsCommand()
		require.NoError(t, cmd.Flags().Parse(cmdArgs[1:]))
		opts, err := loadCommonLogsOptions(cmd)
		require.NoError(t, err)
		assert.Equal(t, mb, normalizeLogsDownloadOptions(opts).MaxStorageMB)
	}
}
