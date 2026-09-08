//go:build !integration

package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLogsDirectorySize(t *testing.T) {
	outputDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(outputDir, "run-1", "usage"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "run-1", "usage", "a.json"), make([]byte, 128), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "summary.json"), make([]byte, 64), 0o644))

	size, err := logsDirectorySize(outputDir)

	require.NoError(t, err)
	assert.Equal(t, int64(192), size)
}

func TestLogsDirectorySizeMissingDirectory(t *testing.T) {
	size, err := logsDirectorySize(filepath.Join(t.TempDir(), "missing"))

	require.NoError(t, err)
	assert.Zero(t, size)
}

func TestLogsFolderSizesLargestFirst(t *testing.T) {
	outputDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(outputDir, "run-small"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(outputDir, "run-large"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "run-small", "data"), make([]byte, 64), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "run-large", "data"), make([]byte, 128), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "summary.json"), make([]byte, 256), 0o644))

	_, folders, _, _, err := computeLogsCacheStats(outputDir, maxReportedLogsCacheFiles)

	require.NoError(t, err)
	require.Len(t, folders, 2)
	assert.Equal(t, logsFolderSize{name: "run-large", size: 128}, folders[0])
	assert.Equal(t, logsFolderSize{name: "run-small", size: 64}, folders[1])
}

func TestLargestLogsFilesSortedAndLimited(t *testing.T) {
	outputDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(outputDir, "run-1", "agent"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "run-1", "agent", "large.log"), make([]byte, 256), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "run-1", "medium.log"), make([]byte, 128), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "small.log"), make([]byte, 64), 0o600))

	_, _, files, count, err := computeLogsCacheStats(outputDir, 2)

	require.NoError(t, err)
	assert.Equal(t, 3, count)
	require.Equal(t, []logsFileSize{
		{path: filepath.Join("run-1", "agent", "large.log"), size: 256},
		{path: filepath.Join("run-1", "medium.log"), size: 128},
	}, files)
}

// TestLargestLogsFilesTieBreaksByPath guards the secondary ordering contract: when
// files are the same size, results must be ordered by relative path ascending
// regardless of filesystem traversal order. Reverse-lexical filenames ("z" before
// "a") ensure the assertion would fail if the path tie-breaker were removed or
// reversed.
func TestLargestLogsFilesTieBreaksByPath(t *testing.T) {
	outputDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "z.log"), make([]byte, 128), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "a.log"), make([]byte, 128), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "m.log"), make([]byte, 128), 0o600))

	_, _, files, count, err := computeLogsCacheStats(outputDir, 2)

	require.NoError(t, err)
	assert.Equal(t, 3, count)
	require.Equal(t, []logsFileSize{
		{path: "a.log", size: 128},
		{path: "m.log", size: 128},
	}, files, "equally sized files must be ordered by relative path ascending")
}

func captureLogsStorageLimitStderr(t *testing.T, fn func()) string {
	t.Helper()
	oldStderr := os.Stderr
	r, w, err := os.Pipe()
	require.NoError(t, err, "failed to create stderr pipe")
	os.Stderr = w
	t.Cleanup(func() { os.Stderr = oldStderr })

	fn()

	w.Close()
	var buf bytes.Buffer
	_, copyErr := io.Copy(&buf, r)
	require.NoError(t, copyErr, "failed to read stderr output")
	return buf.String()
}

func TestReportStartingUsageIndicatesTruncation(t *testing.T) {
	limit := &logsStorageLimit{maxBytes: bytesPerMegabyte}
	files := []logsFileSize{{path: "a.log", size: 128}}

	output := captureLogsStorageLimitStderr(t, func() {
		limit.reportStartingUsage(128, nil, files, 5)
	})

	assert.Contains(t, output, "Logs cache files: showing 1 largest of 5")
	assert.NotContains(t, output, "Logs cache files: 5")
}

func TestReportStartingUsageNoTruncation(t *testing.T) {
	limit := &logsStorageLimit{maxBytes: bytesPerMegabyte}
	files := []logsFileSize{{path: "a.log", size: 128}}

	output := captureLogsStorageLimitStderr(t, func() {
		limit.reportStartingUsage(128, nil, files, 1)
	})

	assert.Contains(t, output, "Logs cache files: 1")
	assert.NotContains(t, output, "showing")
}

func TestLargestLogsFilesMissingDirectory(t *testing.T) {
	_, folders, files, count, err := computeLogsCacheStats(filepath.Join(t.TempDir(), "missing"), maxReportedLogsCacheFiles)

	require.NoError(t, err)
	assert.Empty(t, folders)
	assert.Empty(t, files)
	assert.Zero(t, count)
}

func TestLogsStorageLimitPrunesNonEssentialAgentData(t *testing.T) {
	outputDir := t.TempDir()
	runDir := filepath.Join(outputDir, "run-1")
	require.NoError(t, os.MkdirAll(filepath.Join(runDir, "sandbox", "agent", "logs"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(runDir, "sandbox", "agent", "logs", downloadedArtifactsMarkerDir), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(runDir, "usage"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(runDir, "sandbox", "agent", "logs", "events.jsonl"), make([]byte, bytesPerMegabyte), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(runDir, jobsAPIResponseFileName), []byte("{}"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(runDir, runSummaryFileName), []byte("{}"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(runDir, "usage", "summary.json"), []byte("{}"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(runDir, filepath.Base(constants.AgentStdioLogPath)), []byte("log"), 0o600))
	require.NoError(t, markArtifactDownloaded(runDir, string(ArtifactSetAll)))
	require.NoError(t, markArtifactDownloaded(runDir, constants.AgentArtifactName.String()))
	require.NoError(t, markArtifactDownloaded(runDir, constants.AgentOutputFallbackArtifactName.String()))

	limit := newLogsStorageLimit(outputDir, 1, false)
	called := false
	err := limit.runDownload(context.Background(), filepath.Join(outputDir, "run-2"), func() error {
		called = true
		return nil
	})

	require.NoError(t, err)
	assert.True(t, called, "pruning should free space so the download can proceed")
	assert.NoFileExists(t, filepath.Join(runDir, "sandbox", "agent", "logs", "events.jsonl"))
	assert.FileExists(t, filepath.Join(runDir, jobsAPIResponseFileName))
	assert.FileExists(t, filepath.Join(runDir, runSummaryFileName))
	assert.FileExists(t, filepath.Join(runDir, "usage", "summary.json"))
	assert.FileExists(t, filepath.Join(runDir, filepath.Base(constants.AgentStdioLogPath)))
	assert.NoFileExists(t, filepath.Join(runDir, downloadedArtifactsMarkerDir, string(ArtifactSetAll)))
	assert.NoFileExists(t, filepath.Join(runDir, downloadedArtifactsMarkerDir, constants.AgentArtifactName.String()))
	assert.NoFileExists(t, filepath.Join(runDir, downloadedArtifactsMarkerDir, constants.AgentOutputFallbackArtifactName.String()))
	assert.False(t, limit.isReached())
}

func TestLogsStorageLimitPrunesEarlierCompletedRuns(t *testing.T) {
	outputDir := t.TempDir()
	firstRunDir := filepath.Join(outputDir, "run-1")
	require.NoError(t, os.MkdirAll(filepath.Join(firstRunDir, "sandbox", "agent", "logs"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(firstRunDir, "sandbox", "agent", "logs", "events.jsonl"), make([]byte, 3*bytesPerMegabyte/4), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(firstRunDir, runSummaryFileName), []byte("{}"), 0o600))

	limit := newLogsStorageLimit(outputDir, 1, false)
	secondRunDir := filepath.Join(outputDir, "run-2")
	err := limit.runDownload(context.Background(), secondRunDir, func() error {
		require.NoError(t, os.MkdirAll(secondRunDir, 0o755))
		return os.WriteFile(filepath.Join(secondRunDir, runSummaryFileName), make([]byte, bytesPerMegabyte/2), 0o600)
	})

	require.NoError(t, err)
	assert.NoFileExists(t, filepath.Join(firstRunDir, "sandbox", "agent", "logs", "events.jsonl"))
	assert.FileExists(t, filepath.Join(secondRunDir, runSummaryFileName))
	assert.False(t, limit.isReached())
}

func TestLogsStorageLimitPrunesCompletedDownloadToBudget(t *testing.T) {
	outputDir := t.TempDir()
	limit := newLogsStorageLimit(outputDir, 1, false)
	runDir := filepath.Join(outputDir, "run-1")

	err := limit.runDownload(context.Background(), runDir, func() error {
		require.NoError(t, os.MkdirAll(filepath.Join(runDir, "mcp-logs"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(runDir, "mcp-logs", "large.log"), make([]byte, 2*bytesPerMegabyte), 0o600))
		return os.WriteFile(filepath.Join(runDir, runSummaryFileName), []byte("{}"), 0o600)
	})

	require.NoError(t, err)
	assert.NoFileExists(t, filepath.Join(runDir, "mcp-logs", "large.log"))
	assert.FileExists(t, filepath.Join(runDir, runSummaryFileName))
	assert.False(t, limit.isReached())
	size, sizeErr := logsDirectorySize(outputDir)
	require.NoError(t, sizeErr)
	assert.Less(t, size, bytesPerMegabyte)
}

func TestLogsStorageLimitDeferredDownloadPrunesExistingCacheAtLimit(t *testing.T) {
	outputDir := t.TempDir()
	existingRunDir := filepath.Join(outputDir, "run-existing")
	require.NoError(t, os.MkdirAll(filepath.Join(existingRunDir, "mcp-logs"), 0o755))
	existingCache := filepath.Join(existingRunDir, "mcp-logs", "large.log")
	require.NoError(t, os.WriteFile(existingCache, make([]byte, 3*bytesPerMegabyte/4), 0o600))

	limit := newLogsStorageLimit(outputDir, 1, false)
	freshRunDir := filepath.Join(outputDir, "run-fresh")
	freshCache := filepath.Join(freshRunDir, "mcp-logs", "large.log")
	err := limit.runDownloadDeferred(context.Background(), freshRunDir, func() error {
		require.NoError(t, os.MkdirAll(filepath.Dir(freshCache), 0o755))
		return os.WriteFile(freshCache, make([]byte, bytesPerMegabyte/2), 0o600)
	})

	require.NoError(t, err)
	assert.NoFileExists(t, existingCache)
	assert.FileExists(t, freshCache, "fresh run data must remain available for parsing")
	assert.False(t, limit.isReached())
}

func TestLogsStorageLimitStopsNewDownloadsAtExistingLimit(t *testing.T) {
	outputDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "existing.bin"), make([]byte, bytesPerMegabyte), 0o644))
	limit := newLogsStorageLimit(outputDir, 1, false)
	called := false

	err := limit.runDownload(context.Background(), filepath.Join(outputDir, "run-1"), func() error {
		called = true
		return nil
	})

	require.ErrorIs(t, err, errLogsStorageLimitReached)
	assert.False(t, called)
	assert.True(t, limit.isReached())
}

func TestLogsStorageLimitAllowsFinalDownloadThenStops(t *testing.T) {
	outputDir := t.TempDir()
	limit := newLogsStorageLimit(outputDir, 1, false)

	runDir := filepath.Join(outputDir, "run-1")
	err := limit.runDownload(context.Background(), runDir, func() error {
		require.NoError(t, os.MkdirAll(runDir, 0o755))
		return os.WriteFile(filepath.Join(runDir, "download.bin"), make([]byte, bytesPerMegabyte), 0o644)
	})

	require.NoError(t, err)
	assert.True(t, limit.isReached())
	secondCalled := false
	err = limit.runDownload(context.Background(), filepath.Join(outputDir, "run-2"), func() error {
		secondCalled = true
		return nil
	})
	require.ErrorIs(t, err, errLogsStorageLimitReached)
	assert.False(t, secondCalled)
}

func TestLogsStorageLimitPrunesOldestRunAfterCachePruningIsExhausted(t *testing.T) {
	outputDir := t.TempDir()
	oldestRunDir := filepath.Join(outputDir, "run-1")
	newerCachedRunDir := filepath.Join(outputDir, "run-2")
	require.NoError(t, os.MkdirAll(oldestRunDir, 0o755))
	require.NoError(t, os.MkdirAll(newerCachedRunDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(oldestRunDir, runSummaryFileName), make([]byte, 3*bytesPerMegabyte/4), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(newerCachedRunDir, runSummaryFileName), make([]byte, bytesPerMegabyte/8), 0o600))

	limit := newLogsStorageLimit(outputDir, 1, true)
	newestRunDir := filepath.Join(outputDir, "run-3")
	err := limit.runDownload(context.Background(), newestRunDir, func() error {
		require.NoError(t, os.MkdirAll(newestRunDir, 0o755))
		return os.WriteFile(filepath.Join(newestRunDir, runSummaryFileName), make([]byte, bytesPerMegabyte/2), 0o600)
	})

	require.NoError(t, err)
	assert.NoDirExists(t, oldestRunDir)
	assert.DirExists(t, newerCachedRunDir)
	assert.DirExists(t, newestRunDir)
	assert.False(t, limit.isReached())
}

func TestLogsStorageLimitPrunesOldestRunByRunID(t *testing.T) {
	outputDir := t.TempDir()
	oldestRunDir := filepath.Join(outputDir, "run-10")
	newerRunDir := filepath.Join(outputDir, "run-20")
	for _, runDir := range []string{oldestRunDir, newerRunDir} {
		require.NoError(t, os.MkdirAll(runDir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(runDir, "essential.bin"), make([]byte, 3*bytesPerMegabyte/8), 0o600))
	}
	require.NoError(t, os.Chtimes(oldestRunDir, time.Now(), time.Now()))
	require.NoError(t, os.Chtimes(newerRunDir, time.Now().Add(-time.Hour), time.Now().Add(-time.Hour)))

	limit := newLogsStorageLimit(outputDir, 1, true)
	currentRunDir := filepath.Join(outputDir, "run-30")
	err := limit.runDownload(context.Background(), currentRunDir, func() error {
		require.NoError(t, os.MkdirAll(currentRunDir, 0o755))
		return os.WriteFile(filepath.Join(currentRunDir, runSummaryFileName), make([]byte, bytesPerMegabyte/2), 0o600)
	})

	require.NoError(t, err)
	assert.NoDirExists(t, oldestRunDir)
	assert.DirExists(t, newerRunDir)
}

func TestLogsStorageLimitKeepsRunWhenNonEssentialPruningIsSufficient(t *testing.T) {
	outputDir := t.TempDir()
	oldRunDir := filepath.Join(outputDir, "run-1")
	oldCache := filepath.Join(oldRunDir, "mcp-logs", "large.log")
	require.NoError(t, os.MkdirAll(filepath.Dir(oldCache), 0o755))
	require.NoError(t, os.WriteFile(oldCache, make([]byte, 3*bytesPerMegabyte/4), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(oldRunDir, runSummaryFileName), []byte("{}"), 0o600))

	limit := newLogsStorageLimit(outputDir, 1, true)
	newRunDir := filepath.Join(outputDir, "run-2")
	err := limit.runDownload(context.Background(), newRunDir, func() error {
		require.NoError(t, os.MkdirAll(newRunDir, 0o755))
		return os.WriteFile(filepath.Join(newRunDir, runSummaryFileName), make([]byte, bytesPerMegabyte/2), 0o600)
	})

	require.NoError(t, err)
	assert.NoFileExists(t, oldCache)
	assert.DirExists(t, oldRunDir, "the whole run must remain when ordinary cache pruning frees enough space")
	assert.FileExists(t, filepath.Join(oldRunDir, runSummaryFileName))
}

func TestLogsStorageLimitDoesNotPruneNewerRunForOlderDownload(t *testing.T) {
	outputDir := t.TempDir()
	newerRunDir := filepath.Join(outputDir, "run-3")
	require.NoError(t, os.MkdirAll(newerRunDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(newerRunDir, runSummaryFileName), make([]byte, bytesPerMegabyte), 0o600))

	limit := newLogsStorageLimit(outputDir, 1, true)
	called := false
	err := limit.runDownload(context.Background(), filepath.Join(outputDir, "run-2"), func() error {
		called = true
		return nil
	})

	require.ErrorIs(t, err, errLogsStorageLimitReached)
	assert.False(t, called)
	assert.DirExists(t, newerRunDir)
}

func TestLogsStorageLimitDisabled(t *testing.T) {
	var limit *logsStorageLimit
	expected := errors.New("download failed")

	err := limit.runDownload(context.Background(), "", func() error {
		return expected
	})

	require.ErrorIs(t, err, expected)
	assert.False(t, limit.isReached())
}

func TestValidateMaxStorageMB(t *testing.T) {
	assert.NoError(t, validateMaxStorageMB(0))
	assert.NoError(t, validateMaxStorageMB(10240))
	assert.Error(t, validateMaxStorageMB(-1))
}

// TestLogsStorageLimitConcurrentDownloadsRunInParallel guards against a regression
// where the storage limiter's internal lock was held across the entire download()
// call, silently serializing every download whenever --max-storage was configured
// (even though the concurrent download pool itself allowed parallelism). Run with
// `go test -race` to also catch data races on the shared usedBytes/reached state.
func TestLogsStorageLimitConcurrentDownloadsRunInParallel(t *testing.T) {
	outputDir := t.TempDir()
	limit := newLogsStorageLimit(outputDir, 10240, false) // large budget: never reached

	const numDownloads = 8
	var inFlight atomic.Int32
	var maxObservedConcurrency atomic.Int32
	release := make(chan struct{})

	var wg sync.WaitGroup
	for i := range numDownloads {
		wg.Go(func() {
			runDir := filepath.Join(outputDir, fmt.Sprintf("run-%d", i))
			err := limit.runDownload(context.Background(), runDir, func() error {
				current := inFlight.Add(1)
				defer inFlight.Add(-1)
				for {
					observedMax := maxObservedConcurrency.Load()
					if current <= observedMax || maxObservedConcurrency.CompareAndSwap(observedMax, current) {
						break
					}
				}
				// Block until every goroutine has entered its download closure so
				// we can observe genuine overlap rather than accidental interleaving.
				<-release
				return os.MkdirAll(runDir, 0o755)
			})
			assert.NoError(t, err)
		})
	}

	require.Eventually(t, func() bool { return inFlight.Load() == numDownloads }, 2*time.Second, time.Millisecond,
		"expected all downloads to run concurrently, not serialized by the storage limiter")
	close(release)
	wg.Wait()

	assert.EqualValues(t, numDownloads, maxObservedConcurrency.Load(), "storage limiter must not serialize concurrent downloads")
}

func TestLogsStorageLimitConcurrentDeferredDownloadsProtectFreshRuns(t *testing.T) {
	outputDir := t.TempDir()
	limit := newLogsStorageLimit(outputDir, 1, false)

	const numDownloads = 2
	ready := make(chan struct{}, numDownloads)
	release := make(chan struct{})
	errs := make(chan error, numDownloads)
	files := make([]string, numDownloads)

	var wg sync.WaitGroup
	for i := range numDownloads {
		runDir := filepath.Join(outputDir, fmt.Sprintf("run-%d", i))
		files[i] = filepath.Join(runDir, "mcp-logs", "large.log")
		wg.Go(func() {
			errs <- limit.runDownloadDeferred(context.Background(), runDir, func() error {
				if err := os.MkdirAll(filepath.Dir(files[i]), 0o755); err != nil {
					return err
				}
				if err := os.WriteFile(files[i], make([]byte, 3*bytesPerMegabyte/4), 0o600); err != nil {
					return err
				}
				ready <- struct{}{}
				<-release
				return nil
			})
		})
	}

	for range numDownloads {
		<-ready
	}
	close(release)
	wg.Wait()
	close(errs)

	for err := range errs {
		require.NoError(t, err)
	}
	for _, file := range files {
		assert.FileExists(t, file, "fresh run data must not be pruned before finalization")
	}
	for i := range numDownloads {
		require.NoError(t, limit.finalizeDownload(filepath.Join(outputDir, fmt.Sprintf("run-%d", i))))
	}
	assert.False(t, limit.isReached())

	subsequentRun := filepath.Join(outputDir, "run-subsequent")
	called := false
	err := limit.runDownload(context.Background(), subsequentRun, func() error {
		called = true
		return os.MkdirAll(subsequentRun, 0o755)
	})
	require.NoError(t, err)
	assert.True(t, called, "a later run should be admitted after all deferred runs are finalized")
}

func TestLogsStorageLimitPrunesOldRunInlineDuringConcurrentDownloads(t *testing.T) {
	outputDir := t.TempDir()
	oldRunDir := filepath.Join(outputDir, "run-1")
	require.NoError(t, os.MkdirAll(oldRunDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(oldRunDir, runSummaryFileName), make([]byte, 3*bytesPerMegabyte/4), 0o600))
	limit := newLogsStorageLimit(outputDir, 1, true)

	const numDownloads = 2
	ready := make(chan struct{}, numDownloads)
	release := make(chan struct{})
	errs := make(chan error, numDownloads)
	var wg sync.WaitGroup
	for i := range numDownloads {
		runDir := filepath.Join(outputDir, fmt.Sprintf("run-%d", i+2))
		wg.Go(func() {
			errs <- limit.runDownloadDeferred(context.Background(), runDir, func() error {
				if err := os.MkdirAll(runDir, 0o755); err != nil {
					return err
				}
				if err := os.WriteFile(filepath.Join(runDir, runSummaryFileName), make([]byte, bytesPerMegabyte/2), 0o600); err != nil {
					return err
				}
				ready <- struct{}{}
				<-release
				return nil
			})
		})
	}

	for range numDownloads {
		<-ready
	}
	close(release)
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}

	assert.NoDirExists(t, oldRunDir, "oldest completed run should be pruned before concurrent downloads finish processing")
	for i := range numDownloads {
		runDir := filepath.Join(outputDir, fmt.Sprintf("run-%d", i+2))
		assert.DirExists(t, runDir)
		require.NoError(t, limit.finalizeDownload(runDir))
	}
}
