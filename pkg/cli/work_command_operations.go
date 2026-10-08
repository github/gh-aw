package cli

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/github/gh-aw/pkg/workqueue"
	"github.com/spf13/cobra"
)

type workOperatorTarget struct{ WorkID, ClaimID string }

func workCancelCommand() *cobra.Command      { return workOperatorCommand("cancel-work", false, false) }
func workCancelClaimCommand() *cobra.Command { return workOperatorCommand("cancel-claim", true, false) }
func workPriorityCommand() *cobra.Command    { return workOperatorCommand("reprioritize", false, true) }

func workOperatorCommand(name string, claims, priority bool) *cobra.Command {
	selector := "work-id"
	short := "Terminally cancel selected Work atomically; retain native reservations"
	if claims {
		selector, short = "claim-id", "Fence selected current Claims and terminally cancel their Work atomically"
	}
	if priority {
		short = "Change available Work's future scheduling priority without resetting age or debt"
	}
	cmd := &cobra.Command{Use: name, Short: short, Args: cobra.NoArgs}
	cmd.Flags().StringSlice(selector, nil, "Exact IDs, comma-separated or repeated; all-or-nothing (maximum 256)")
	cmd.Flags().String("reason", "", "Required sanitized reason code, for example operator_cancelled")
	if priority {
		cmd.Flags().Int("priority", 0, "New class 1..5 (configured weighted shares, not strict priority)")
	}
	cmd.RunE = func(cmd *cobra.Command, _ []string) error { return workRunOperator(cmd, selector, claims, priority) }
	return cmd
}

func workOperatorSelectors(cmd *cobra.Command, selector string, priority bool) ([]string, string, int, error) {
	ids, _ := cmd.Flags().GetStringSlice(selector)
	reason, _ := cmd.Flags().GetString("reason")
	value := 0
	extra := ""
	if priority {
		value, _ = cmd.Flags().GetInt("priority")
		extra = " and --priority 1..5"
	}
	if len(ids) < 1 || len(ids) > 256 || reason == "" || priority && (value < 1 || value > 5) {
		return nil, "", 0, fmt.Errorf("--%s (1..256 IDs), --reason%s are required", selector, extra)
	}
	slices.Sort(ids)
	if slices.Contains(ids, "") || len(slices.Compact(slices.Clone(ids))) != len(ids) {
		return nil, "", 0, errors.New("selectors must be nonempty, distinct exact IDs")
	}
	return ids, reason, value, nil
}

func workRunOperator(cmd *cobra.Command, selector string, claims, priority bool) error {
	ids, reason, value, err := workOperatorSelectors(cmd, selector, priority)
	if err != nil {
		return err
	}
	state, err := workRead(cmd)
	if err != nil {
		return err
	}
	targets, err := workOperatorTargets(state, ids, claims)
	if err != nil {
		return err
	}
	id, err := workRequestID(cmd)
	if err != nil {
		return err
	}
	params, kind, err := workOperatorIntent(state, targets, reason, value, id)
	if err != nil {
		return err
	}
	published, err := workPublishOperator(cmd, id, kind, params)
	if err != nil {
		return err
	}
	message := fmt.Sprintf("Cancelled %d selected Work; native reservations retained until definitive evidence", len(targets))
	if priority {
		message = fmt.Sprintf("Priority %d recorded for %d available Work; age and debt preserved", value, len(targets))
	}
	return workPrint(cmd, published, message+"; request="+id)
}

func workOperatorTargets(state workqueue.Projection, ids []string, claims bool) ([]workOperatorTarget, error) {
	targets := make([]workOperatorTarget, 0, len(ids))
	for _, id := range ids {
		target := workOperatorTarget{WorkID: id}
		if claims {
			claim := state.Claims[id]
			if claim == nil {
				return nil, fmt.Errorf("claim_missing: unknown Claim %s", workQueueText(id))
			}
			target.WorkID, target.ClaimID = claim.WorkID, id
		}
		targets = append(targets, target)
	}
	return targets, nil
}

func workPublishOperator(cmd *cobra.Command, id, kind string, params workqueue.OperationsParameters) (workqueue.Publication, error) {
	branch := workBranch(cmd)
	actor, err := branch.Authenticate(cmd.Context(), "administrator")
	if err != nil {
		return workqueue.Publication{}, err
	}
	request, err := workqueue.NewRequest(id, kind, actor, params)
	if err != nil {
		return workqueue.Publication{}, err
	}
	return branch.Publish(cmd.Context(), actor, request)
}

func workOperatorIntent(state workqueue.Projection, targets []workOperatorTarget, reason string, priority int, requestID string) (workqueue.OperationsParameters, string, error) {
	kind := "cancel_work"
	if priority != 0 {
		kind = "control"
	}
	slices.SortFunc(targets, func(a, b workOperatorTarget) int { return cmp.Compare(a.WorkID, b.WorkID) })
	params := workqueue.OperationsParameters{Operations: []workqueue.Operation{}}
	previous := ""
	for _, target := range targets {
		work := state.Works[target.WorkID]
		if work == nil {
			return params, kind, fmt.Errorf("work_missing: unknown Work %s", workQueueText(target.WorkID))
		}
		if previous == target.WorkID {
			return params, kind, errors.New("selectors must identify distinct Work")
		}
		previous = target.WorkID
		fields := map[string]any{"kind": "WorkCancellation", "work_id": target.WorkID, "reason": reason}
		if target.ClaimID != "" {
			fields["claim_id"] = target.ClaimID
		}
		if priority != 0 {
			fields["kind"], fields["priority"], fields["expected_priority"] = "WorkPriority", priority, work.SchedulingPriority()
		}
		operation, err := workqueue.Op(fields)
		if err != nil {
			return params, kind, err
		}
		params.Operations = append(params.Operations, operation)
	}
	recovered, err := recoverWorkOperatorIntent(state, params, kind, requestID)
	return recovered, kind, err
}

// Recover original compare-and-set values, not a new fingerprint derived from
// the state an accepted action already changed.
func recoverWorkOperatorIntent(state workqueue.Projection, wanted workqueue.OperationsParameters, kind, requestID string) (workqueue.OperationsParameters, error) {
	accepted, ok := state.Requests[requestID]
	if !ok {
		return wanted, nil
	}
	var old workqueue.OperationsParameters
	if err := json.Unmarshal(accepted.Request.Parameters, &old); err != nil {
		return wanted, err
	}
	if accepted.Request.Kind != kind || len(old.Operations) != len(wanted.Operations) {
		return wanted, errors.New("request_reuse: accepted request has different intent")
	}
	for index, operation := range old.Operations {
		if index < 0 || index >= len(wanted.Operations) {
			return wanted, errors.New("request_reuse: accepted request has different operation count")
		}
		left, err := workOperatorComparison(operation)
		if err != nil {
			return wanted, err
		}
		right, err := workOperatorComparison(wanted.Operations[index])
		if err != nil {
			return wanted, err
		}
		if !bytes.Equal(left, right) {
			return wanted, errors.New("request_reuse: accepted request has different selectors, reason or priority")
		}
	}
	return old, nil
}

func workOperatorComparison(operation workqueue.Operation) ([]byte, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(operation, &fields); err != nil {
		return nil, err
	}
	delete(fields, "expected_priority")
	return json.Marshal(fields)
}
