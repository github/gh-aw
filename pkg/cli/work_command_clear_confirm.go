package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/workqueue"
	"github.com/spf13/cobra"
)

var workClearConfirmAction = console.ConfirmAction

func workConfirmClear(cmd *cobra.Command, branch workqueue.Branch, state workqueue.Projection, dispatches, works []string) error {
	preview := workClearPreview(branch, state, dispatches, works)
	if _, err := fmt.Fprintln(cmd.ErrOrStderr(), preview); err != nil {
		return fmt.Errorf("clear_preview_failed: queue unchanged: %w", err)
	}
	confirmed, err := workClearConfirmAction("Apply this queue clear?", "Yes, clear the queue", "No, keep queue")
	if err != nil {
		return fmt.Errorf("clear_confirmation_failed: queue unchanged: %w", err)
	}
	if !confirmed {
		return errors.New("clear_cancelled: confirmation declined; queue unchanged")
	}
	return nil
}

func workClearPreview(branch workqueue.Branch, state workqueue.Projection, dispatches, works []string) string {
	lines := []string{
		fmt.Sprintf("Queue clear preview: repository=%s branch=%s\nSnapshot: %s",
			workQueueText(branch.Remote), workQueueText(branch.Name), workQueueText(state.Tip)),
		fmt.Sprintf("Work: %d; available: %d; claimed: %d; completed: %d; cancelled: %d; open dispatches: %d",
			state.Stats.Work, state.Stats.Available, state.Stats.Claimed, state.Stats.Completed, state.Stats.Cancelled, state.Stats.Dispatches),
		fmt.Sprintf("Release %d dispatches, then cancel %d Work (reason operator).\nRunning workflows are not stopped; completed Work and history are retained.", len(dispatches), len(works)),
	}
	for _, id := range dispatches {
		dispatch := state.Dispatches[id]
		lines = append(lines, fmt.Sprintf("RELEASE %s state=%s workflow=%s", workQueueText(id), dispatch.State, workQueueText(dispatch.Workflow)))
	}
	for _, id := range works {
		lines = append(lines, fmt.Sprintf("CANCEL %s state=%s", workQueueText(id), state.Works[id].State))
	}
	return strings.Join(lines, "\n")
}
