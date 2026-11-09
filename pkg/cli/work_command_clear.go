package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/github/gh-aw/pkg/workqueue"
	"github.com/spf13/cobra"
)

type workClearResult struct {
	Status                    string                 `json:"status"`
	RequestID                 string                 `json:"request_id"`
	SnapshotTip               string                 `json:"snapshot_tip"`
	WorkIDs                   []string               `json:"work_ids"`
	CompletedRetained         int                    `json:"completed_retained"`
	PendingDeliveriesRetained int                    `json:"pending_deliveries_retained"`
	ReservationsRetained      int                    `json:"reservations_retained"`
	Publication               *workqueue.Publication `json:"publication,omitempty"`
}

func workClearCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use: "clear", Short: "Terminally cancel unfinished snapshot Work atomically; preserve history and native reservations",
		Long: "Terminally cancel available and claimed Work without pending deliveries\n" +
			"in one queue snapshot. Pending keep-open deliveries are retained.\n\n" +
			"Requires repository maintain or admin permission and --reason CODE.\n" +
			"Mutations show a queue preview and require interactive confirmation;\n" +
			"there is no confirmation bypass. Non-TTY input refuses to clear. --dry-run\n" +
			"validates and previews the same cancellation without publishing anything.\n" +
			"Completed Work, delivery barriers, audit history, admission identities, and\n" +
			"fairness debt are retained. Native runs are not stopped and reservations\n" +
			"are not released; reconcile them separately using definitive evidence.\n\n" +
			"The targets must fit the installed operation limit (at most 256 Work);\n" +
			"otherwise nothing is cancelled. Use cancel-work in bounded batches for\n" +
			"larger backlogs. New admissions are not included and admission/grants are\n" +
			"not paused. Use control to pause them separately when draining a queue.\n" +
			"A concurrent completion, stale selected Claim, or replacement ledger\n" +
			"fails the whole action, including on publication retries.\n" +
			"Retry an accepted --request-id with the same reason to recover only its\n" +
			"original cancellation, never to sweep newly admitted Work. A snapshot\n" +
			"with no cancellable Work is a no-op and does not reserve the request ID.\n\n" +
			"--hard instead resets the active ledger to a fresh, paused queue. Both\n" +
			"admission and grants must already be paused, all native reservations\n" +
			"released, and all effects settled. Unresolved or failed delivery blocks\n" +
			"hard clearing. Current Policy, worker routes/availability, and credential\n" +
			"generation are retained; old worker revisions, Work/Claim/request identities,\n" +
			"and fairness debt are forgotten. Old records remain in Git ancestry.\n" +
			"WARNING: --hard permits resubmitting old identities and repeating settled\n" +
			"effects; it is not compaction. A queue change after the hard-clear preview\n" +
			"fails without resetting the newer snapshot. Use a stable --request-id.\n\n" +
			"All clear modes, including --dry-run, refuse to run in CI.",
		Args: cobra.NoArgs,
		RunE: workRunClear,
	}
	cmd.Flags().String("reason", "", "Required sanitized reason code (1..128 letters, digits, or _.:-; start with a letter or digit)")
	cmd.Flags().Bool("dry-run", false, "Validate and preview clearing without writing or stopping runs")
	cmd.Flags().Bool("hard", false, "Reset a paused, fully settled ledger; forget identities/debt, retain Git ancestry (never in CI)")
	return cmd
}

func workRunClear(cmd *cobra.Command, _ []string) error {
	if IsRunningInCI() {
		return errors.New("clear_ci_forbidden: work-queue clear refuses to run in CI, including --hard and --dry-run; use an interactive operator environment")
	}
	reason, _ := cmd.Flags().GetString("reason")
	if !workqueue.ValidReason(reason) {
		return errors.New("reason_invalid: --reason must be a sanitized code of 1..128 characters, starting with a letter or digit and containing only letters, digits, or _.:-")
	}
	id, err := workRequestID(cmd)
	if err != nil {
		return err
	}
	branch := workBranch(cmd)
	actor, err := branch.AuthenticateClear(cmd.Context())
	if err != nil {
		return err
	}
	commits, err := branch.Read(cmd.Context())
	if err != nil {
		return err
	}
	state, err := workqueue.Replay(commits)
	if err != nil {
		return err
	}
	hard, _ := cmd.Flags().GetBool("hard")
	if hard {
		return workRunHardClear(cmd, branch, id, reason, state)
	}
	params, err := workClearIntent(state, reason, id)
	if err != nil {
		return err
	}
	result, err := workClearReport(state, params, id)
	if err != nil {
		return err
	}
	if len(result.WorkIDs) == 0 {
		return workPrint(cmd, result, "No cancellable Work in snapshot; history, completed Work, pending deliveries, and native reservations retained; no request recorded")
	}
	return workPublishClear(cmd, branch, actor, commits, params, result)
}

func workClearReport(state workqueue.Projection, params workqueue.OperationsParameters, id string) (workClearResult, error) {
	result := workClearResult{
		Status: "no_work", RequestID: id, SnapshotTip: state.Tip,
		WorkIDs: []string{}, CompletedRetained: state.Stats.Completed,
	}
	for _, dispatch := range state.Dispatches {
		if !dispatch.Released {
			result.ReservationsRetained++
		}
	}
	for _, work := range state.Works {
		if work.Barrier == "pending" {
			result.PendingDeliveriesRetained++
		}
	}
	for _, operation := range params.Operations {
		var fields struct {
			WorkID string `json:"work_id"`
		}
		if err := json.Unmarshal(operation, &fields); err != nil {
			return result, err
		}
		result.WorkIDs = append(result.WorkIDs, fields.WorkID)
	}
	return result, nil
}

func workRunHardClear(cmd *cobra.Command, branch workqueue.Branch, id, reason string, state workqueue.Projection) error {
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	result, err := branch.HardClear(cmd.Context(), id, reason, true)
	if err != nil {
		return err
	}
	if !dryRun && result.Status != "already_committed" {
		if state.Tip != result.PriorTip {
			return errors.New("hard_clear_conflict: queue changed before confirmation preview; nothing was reset")
		}
		if err := workConfirmClear(cmd, branch, state, nil, true); err != nil {
			return err
		}
		result, err = branch.HardClearFromSnapshot(cmd.Context(), id, reason, result.PriorGitSHA)
		if err != nil {
			return err
		}
	}
	action := "Hard cleared active ledger"
	if result.Status == "already_committed" {
		action = "Confirmed original hard clear"
	} else if dryRun {
		action = "Would hard clear active ledger"
	}
	return workPrint(cmd, result, fmt.Sprintf("%s; reset initializes admission/grants paused; prior ledger retained in Git at %s; WARNING: identities and fairness debt reset; old identities may repeat effects; request=%s",
		action, result.PriorGitSHA, workQueueText(id)))
}

func workPublishClear(cmd *cobra.Command, branch workqueue.Branch, actor workqueue.Actor, commits []workqueue.QueueCommit, params workqueue.OperationsParameters, result workClearResult) error {
	request, err := workqueue.NewRequest(result.RequestID, "cancel_work", actor, params)
	if err != nil {
		return err
	}
	// Preview through the authoritative replay validator, not just target counting.
	if _, _, _, err := workqueue.BuildCandidate(commits, actor, request, time.Now().UnixMilli()); err != nil {
		return err
	}
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	action := "Would cancel"
	result.Status = "dry_run"
	if !dryRun {
		state, err := workqueue.Replay(commits)
		if err != nil {
			return err
		}
		if _, accepted := state.Requests[result.RequestID]; !accepted {
			if err := workConfirmClear(cmd, branch, state, result.WorkIDs, false); err != nil {
				return err
			}
		}
		published, err := branch.PublishFromSnapshot(cmd.Context(), actor, request, commits)
		if err != nil {
			return err
		}
		result.Publication = &published
		action, result.Status = "Cancelled", "cancelled"
		if !published.Changed {
			action, result.Status = "Confirmed original cancellation of", "already_committed"
		}
		if _, err := branch.SynchronizeIssueTransition(cmd.Context(), result.RequestID); err != nil {
			return fmt.Errorf("queue cancellation committed; Issue synchronization pending; retry sync-issues --request-id %s: %w", result.RequestID, err)
		}
	}
	return workPrint(cmd, result, fmt.Sprintf("%s %d snapshot Work; completed Work, pending deliveries, and history retained; native runs not stopped or released; admission/grants unchanged; request=%s",
		action, len(result.WorkIDs), workQueueText(result.RequestID)))
}

func workClearIntent(state workqueue.Projection, reason, requestID string) (workqueue.OperationsParameters, error) {
	if accepted, ok := state.Requests[requestID]; ok {
		return workRecoverClearIntent(accepted, reason)
	}
	targets := []workOperatorTarget{}
	for id, work := range state.Works {
		if (work.State == "available" || work.State == "claimed") && work.Barrier != "pending" {
			target := workOperatorTarget{WorkID: id}
			if work.State == "claimed" {
				target.ClaimID = work.ClaimID
			}
			targets = append(targets, target)
		}
	}
	if len(targets) > state.Policy.Limits.Operations {
		return workqueue.OperationsParameters{}, fmt.Errorf("clear_limit: snapshot has %d unfinished Work but the installed atomic operation limit is %d; pause admission/grants with control and use cancel-work in bounded batches; nothing was cancelled",
			len(targets), state.Policy.Limits.Operations)
	}
	params, _, err := workOperatorIntent(state, targets, reason, 0, requestID)
	return params, err
}

func workRecoverClearIntent(accepted workqueue.QueueCommit, reason string) (workqueue.OperationsParameters, error) {
	params := workqueue.OperationsParameters{Operations: []workqueue.Operation{}}
	if accepted.Request.Kind != "cancel_work" {
		return params, errors.New("request_reuse: accepted request is not a Work cancellation")
	}
	// Checkpoints retain cancellation trace fields, but not raw request parameters.
	for _, operation := range accepted.Operations {
		var fields struct {
			Kind    string `json:"kind"`
			Version *int   `json:"version,omitempty"`
			WorkID  string `json:"work_id"`
			ClaimID string `json:"claim_id,omitempty"`
			Reason  string `json:"reason"`
		}
		if err := json.Unmarshal(operation, &fields); err != nil {
			return params, err
		}
		if fields.Kind != "WorkCancellation" || fields.Reason != reason {
			return params, errors.New("request_reuse: accepted cancellation has a different reason or operation")
		}
		recovered, err := workqueue.Op(fields)
		if err != nil {
			return params, err
		}
		params.Operations = append(params.Operations, recovered)
	}
	request, err := workqueue.NewRequest(accepted.Request.ID, "cancel_work", accepted.Actor, params)
	if err != nil {
		return params, err
	}
	if request.Fingerprint != accepted.Request.Fingerprint {
		return params, errors.New("request_reuse: original cancellation intent cannot be recovered exactly")
	}
	return params, nil
}
