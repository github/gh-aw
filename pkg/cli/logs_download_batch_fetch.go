package cli

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/github/gh-aw/pkg/console"
)

type workflowRunBatch struct {
	runs                   []WorkflowRun
	totalFetched           int
	batchSize              int
	oldestFetchedCreatedAt time.Time
	oldestFetchedRunID     int64
}

func fetchWorkflowRunBatch(ctx context.Context, opts LogsDownloadOptions, beforeDate string, processedCount int, fetchAllInRange bool) (workflowRunBatch, error) {
	batchSize := computeLogsBatchSize(opts.WorkflowName, opts.Count, processedCount, fetchAllInRange)
	var oldestFetchedCreatedAt time.Time
	var oldestFetchedRunID int64
	runs, totalFetched, err := listWorkflowRunsWithPagination(ListWorkflowRunsOptions{
		Context:                ctx,
		WorkflowName:           opts.WorkflowName,
		Limit:                  batchSize,
		StartDate:              opts.StartDate,
		EndDate:                opts.EndDate,
		BeforeDate:             beforeDate,
		Ref:                    opts.Ref,
		BeforeRunID:            opts.BeforeRunID,
		AfterRunID:             opts.AfterRunID,
		IgnoreWorkflowRuns:     opts.IgnoreWorkflowRuns,
		RepoOverride:           opts.RepoOverride,
		OldestFetchedCreatedAt: &oldestFetchedCreatedAt,
		OldestFetchedRunID:     &oldestFetchedRunID,
		ProcessedCount:         processedCount,
		TargetCount:            opts.Count,
		Verbose:                opts.Verbose,
		CachedJSONLCache:       opts.cachedJSONLCache,
		CachedJSONLWriter:      opts.cachedJSONLWriter,
	})
	return workflowRunBatch{runs: runs, totalFetched: totalFetched, batchSize: batchSize, oldestFetchedCreatedAt: oldestFetchedCreatedAt, oldestFetchedRunID: oldestFetchedRunID}, err
}

func computeLogsBatchSize(workflowName string, count, processedCount int, fetchAllInRange bool) int {
	batchSize := BatchSize
	if workflowName == "" {
		batchSize = BatchSizeForAllWorkflows
	}
	if fetchAllInRange || count-processedCount >= batchSize {
		return batchSize
	}
	needed := count - processedCount
	batchSize = needed * 3
	if workflowName == "" && batchSize < BatchSizeForAllWorkflows {
		batchSize = BatchSizeForAllWorkflows
	}
	if batchSize > BatchSizeForAllWorkflows {
		batchSize = BatchSizeForAllWorkflows
	}
	return batchSize
}

func handleEmptyWorkflowRunBatch(batch workflowRunBatch, verbose bool) (string, bool, bool) {
	if len(batch.runs) > 0 {
		return "", false, false
	}
	if shouldStopPagination(batch.totalFetched, batch.batchSize) {
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("No more workflow runs found, stopping iteration"))
		}
		return "", false, true
	}
	cursor, ok := selectPaginationCursorDate(nil, batch.oldestFetchedCreatedAt)
	if !ok {
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Workflow batch filtered to zero runs but no pagination cursor was found, stopping iteration"))
		}
		return "", false, true
	}
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Batch filtered to zero runs; advancing pagination cursor and continuing"))
	}
	return cursor, true, false
}

func logWorkflowRunBatchFound(batch workflowRunBatch, iteration int, verbose bool) {
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Found %d workflow runs in batch %d", len(batch.runs), iteration)))
	}
}
