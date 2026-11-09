package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/github/gh-aw/pkg/workqueue"
	"github.com/spf13/cobra"
)

const workObsoleteReason = "worker_contract_obsolete"

type workObsoleteResult struct {
	Status      string                 `json:"status"`
	RequestID   string                 `json:"request_id"`
	SnapshotTip string                 `json:"snapshot_tip"`
	WorkIDs     []string               `json:"work_ids"`
	Publication *workqueue.Publication `json:"publication,omitempty"`
}

func workObsoleteIntent(state workqueue.Projection, at int64, limit int, id string) (workqueue.OperationsParameters, []string, error) {
	ids, err := workqueue.ObsoleteWorkIDs(state, at, limit)
	if err != nil {
		return workqueue.OperationsParameters{}, nil, err
	}
	if accepted, ok := state.Requests[id]; ok {
		params, err := workRecoverClearIntent(accepted, workObsoleteReason)
		if err != nil {
			return params, nil, err
		}
		if len(params.Operations) > min(limit, state.Policy.Limits.Operations) {
			return params, nil, errors.New("obsolete_limit: original cancellation exceeds this batch limit; retry with its original limit")
		}
		ids = []string{}
		for _, operation := range params.Operations {
			var fields struct {
				WorkID  string `json:"work_id"`
				ClaimID string `json:"claim_id"`
			}
			if err := json.Unmarshal(operation, &fields); err != nil {
				return params, nil, err
			}
			if fields.ClaimID != "" {
				return params, nil, errors.New("request_reuse: accepted cancellation selected Claims; retry its original operator command")
			}
			ids = append(ids, fields.WorkID)
		}
		return params, ids, nil
	}
	targets := make([]workOperatorTarget, 0, len(ids))
	for _, id := range ids {
		targets = append(targets, workOperatorTarget{WorkID: id})
	}
	params, _, err := workOperatorIntent(state, targets, workObsoleteReason, 0, id)
	return params, ids, err
}

func workRunObsoleteCancellation(cmd *cobra.Command) error {
	limit, _ := cmd.Flags().GetInt("limit")
	if limit < 1 || limit > 256 {
		return errors.New("obsolete_limit: --limit must be 1..256")
	}
	branch := workBranch(cmd)
	commits, err := branch.Read(cmd.Context())
	if err != nil {
		return err
	}
	if len(commits) == 0 {
		return errors.New("queue_missing: obsolete cancellation requires an installed queue")
	}
	at := time.Now().UnixMilli()
	state, err := workqueue.ReplayAt(commits, at)
	if err != nil {
		return err
	}
	id, err := workRequestID(cmd)
	if err != nil {
		return err
	}
	params, ids, err := workObsoleteIntent(state, at, limit, id)
	if err != nil {
		return err
	}
	result := workObsoleteResult{Status: "no_work", RequestID: id, SnapshotTip: state.Tip, WorkIDs: ids}
	message := "No eligible obsolete Work; no request recorded"
	if len(ids) == 0 {
		return workPrint(cmd, result, message)
	}
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	_, accepted := state.Requests[id]
	if dryRun {
		result.Status = "dry_run"
		message = fmt.Sprintf("Would cancel %d obsolete Work; read-only selection, publication authority not checked", len(ids))
		if accepted {
			result.Status = "already_committed"
			message = fmt.Sprintf("Original cancellation of %d Work already committed; read-only receipt recovery", len(ids))
		}
	} else {
		return workPublishObsoleteCancellation(cmd, params, result)
	}
	return workPrint(cmd, result, message+"; native runs, reservations, policy, debt and history retained; request="+workQueueText(id))
}

func workPublishObsoleteCancellation(cmd *cobra.Command, params workqueue.OperationsParameters, result workObsoleteResult) error {
	id := result.RequestID
	published, err := workPublishOperator(cmd, id, "cancel_work", params)
	if err != nil {
		return fmt.Errorf("obsolete cancellation failed; inspect the ledger with explain --request-id %s before retrying the same request: %w", workQueueText(id), err)
	}
	result.Publication = &published
	result.Status = "cancelled"
	message := fmt.Sprintf("Cancelled %d obsolete Work", len(result.WorkIDs))
	if !published.Changed {
		result.Status = "already_committed"
		message = fmt.Sprintf("Confirmed original cancellation of %d Work", len(result.WorkIDs))
	}
	if _, err := workBranch(cmd).SynchronizeIssueTransition(cmd.Context(), id); err != nil {
		return fmt.Errorf("queue cancellation committed; Issue synchronization pending; retry sync-issues --request-id %s: %w", workQueueText(id), err)
	}
	return workPrint(cmd, result, message+"; native runs, reservations, policy, debt and history retained; request="+workQueueText(id))
}
