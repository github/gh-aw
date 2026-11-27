package cli

import (
	"errors"
	"fmt"
	"slices"

	"github.com/github/gh-aw/pkg/workqueue"
	"github.com/spf13/cobra"
)

type workClearResult struct {
	Status            string   `json:"status"`
	SnapshotTip       string   `json:"snapshot_tip"`
	DispatchIDs       []string `json:"dispatch_ids"`
	WorkIDs           []string `json:"work_ids"`
	CompletedRetained int      `json:"completed_retained"`
	Requests          []string `json:"requests"`
}

func workClearCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use: "clear", Short: "Release every open dispatch and cancel all unfinished Work (operator)",
		Long: "Release every non-closed dispatch with reason operator, then cancel every\n" +
			"available Work with reason operator in batches of at most 256. Released Work\n" +
			"that reaches its pool's max_attempts is cancelled with attempts_exhausted.\n\n" +
			"Running workflow runs are not stopped: a late worker finds its Claims closed\n" +
			"and cannot complete them. Completed Work and ledger history are retained.\n" +
			"Work admitted after the preview is not cancelled.\n\n" +
			"Mutations show a preview and require interactive confirmation; non-TTY input\n" +
			"refuses to clear. --dry-run previews without publishing. Refuses to run in CI.",
		Args: cobra.NoArgs,
		RunE: workRunClear,
	}
	cmd.Flags().Bool("dry-run", false, "Preview clearing without writing")
	return cmd
}

func workClearTargets(state workqueue.Projection) (dispatches, works []string) {
	for _, id := range state.DispatchOrder() {
		if state.Dispatches[id].State != "closed" {
			dispatches = append(dispatches, id)
		}
	}
	for _, work := range workQueueOrderedWork(state, workQueueFilter{}) {
		if work.State == "available" || work.State == "claimed" {
			works = append(works, work.WorkID)
		}
	}
	return dispatches, works
}

func workRunClear(cmd *cobra.Command, _ []string) error {
	if IsRunningInCI() {
		return errors.New("clear_ci_forbidden: work-queue clear refuses to run in CI, including --dry-run; use an interactive operator environment")
	}
	branch := workBranch(cmd)
	state, err := workRead(cmd)
	if err != nil {
		return err
	}
	dispatches, works := workClearTargets(state)
	result := workClearResult{Status: "no_work", SnapshotTip: state.Tip, DispatchIDs: dispatches, WorkIDs: works,
		CompletedRetained: state.Stats.Completed, Requests: []string{}}
	if result.DispatchIDs == nil {
		result.DispatchIDs = []string{}
	}
	if result.WorkIDs == nil {
		result.WorkIDs = []string{}
	}
	if len(dispatches) == 0 && len(works) == 0 {
		return workPrint(cmd, result, "Nothing to clear: no open dispatches or unfinished Work")
	}
	if dryRun, _ := cmd.Flags().GetBool("dry-run"); dryRun {
		result.Status = "dry_run"
		return workPrint(cmd, result, fmt.Sprintf("Would release %d dispatches and cancel %d Work; completed Work and history retained", len(dispatches), len(works)))
	}
	if err := workConfirmClear(cmd, branch, state, dispatches, works); err != nil {
		return err
	}
	actor := workActor(cmd)
	maxAttempts := workMaxAttempts()
	for _, id := range dispatches {
		published, err := branch.Publish(cmd.Context(), actor, "release", workqueue.ReleaseParameters{DispatchID: id, Reason: "operator"},
			func(state workqueue.Projection, _ workqueue.Request) ([]workqueue.Operation, error) {
				return workReleaseOperations(state, id, "operator", maxAttempts)
			})
		if err != nil {
			return fmt.Errorf("clear stopped after %d requests: %w", len(result.Requests), err)
		}
		if published.Committed {
			result.Requests = append(result.Requests, published.Request.ID)
		}
	}
	for chunk := range slices.Chunk(works, workqueue.OperationsPerCommit) {
		published, err := branch.Publish(cmd.Context(), actor, "cancel_work", workqueue.CancelWorkParameters{WorkIDs: chunk, Reason: "operator"},
			func(state workqueue.Projection, _ workqueue.Request) ([]workqueue.Operation, error) {
				return workClearCancelOperations(state, chunk)
			})
		if err != nil {
			return fmt.Errorf("clear stopped after %d requests: %w", len(result.Requests), err)
		}
		if published.Committed {
			result.Requests = append(result.Requests, published.Request.ID)
		}
	}
	result.Status = "cleared"
	return workPrint(cmd, result, fmt.Sprintf("Released %d dispatches and cancelled the remaining unfinished Work of %d in %d requests; running workflows were not stopped",
		len(dispatches), len(works), len(result.Requests)))
}

// workClearCancelOperations cancels the chunk's Work that is still available;
// Work that completed, was cancelled, or was claimed after the preview is skipped.
func workClearCancelOperations(state workqueue.Projection, ids []string) ([]workqueue.Operation, error) {
	operations := []workqueue.Operation{}
	for _, id := range ids {
		if work := state.Works[id]; work == nil || work.State != "available" {
			continue
		}
		operation, err := workqueue.Op(workqueue.WorkCancellationOperation{Kind: "WorkCancellation", WorkID: id, Reason: "operator"})
		if err != nil {
			return nil, err
		}
		operations = append(operations, operation)
	}
	return operations, nil
}
