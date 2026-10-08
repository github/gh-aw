package cli

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/workqueue"
	"github.com/spf13/cobra"
)

func workQueueDetails(state workqueue.Projection, row workQueueRow, at int64) (string, error) {
	if row.Kind == "graph" {
		return "Graph " + workQueueText(row.ID) + "\nSelect a Work or Claim for details.\nDependencies are cross-references, not forest parenthood.", nil
	}
	work := state.Works[row.WorkID]
	if work == nil {
		return "", errors.New("work_missing: selected Work is absent; refresh state")
	}
	explanation, err := workqueue.ExplainWork(state, work.WorkID, at)
	if err != nil {
		return "", err
	}
	blocks := []string{fmt.Sprintf("Tip: %s\nPolicy: %s\nEvaluated at: %d", workQueueText(state.Tip), workQueueText(state.PolicyEpoch), at), workQueueWorkDetails(work, explanation)}
	claimID := work.ClaimID
	if row.Kind == "claim" {
		claimID = row.ID
	}
	if claimID != "" {
		claim := state.Claims[claimID]
		if claim == nil {
			return "", errors.New("claim_missing: selected Claim is absent; refresh state")
		}
		blocks = append(blocks, workQueueClaimDetails(state, work, claim)...)
	}
	dependencies, err := workQueueDependencyDetails(state, work, explanation)
	if err != nil {
		return "", err
	}
	blocks = append(blocks, dependencies, "Cancel is terminal for Work, not a retry or native-run stop.\nNative reservations require separate definitive reconciliation.\nNo payloads, receipt bodies or executable output are displayed.")
	text := strings.Join(blocks, "\n\n")
	if len(text) > workQueueViewBytes-256 {
		end := strings.LastIndex(text[:workQueueViewBytes-256], "\n")
		text = text[:end] + "\n... metadata omitted at the view byte bound; use trace or authoritative replay JSON."
	}
	return text, nil
}

func workQueueWorkDetails(work *workqueue.WorkState, explanation workqueue.WorkExplanation) string {
	return fmt.Sprintf("WORK %s\nID: %s\nGraph: %s\nPool: %s\nPriority: %d (admitted %d; policy-weighted class)\nFairness key: %s\nWorker profile: %s\nState: %s | scheduling: %s (snapshot)\nPosition: %d/%d | attempts: %d\nRetry not before: %d\nCurrent/last Claim: %s\nDelivery: %s | disposition: %s\nCompletion: %s\nResult commit: %s\nCancellation: %s",
		workQueueText(work.NodeKey), workQueueText(work.WorkID), workQueueText(work.GraphID),
		workQueueText(work.Pool), work.SchedulingPriority(), work.Priority, workQueueText(work.FairnessKey),
		workQueueText(work.WorkerProfile), work.State, explanation.Reason, work.Position.Commit, work.Position.Operation,
		work.Attempts, work.RetryNotBefore, workQueueText(work.ClaimID), work.Barrier, workQueueText(work.Disposition),
		workQueueText(work.CompletionID), workQueueText(work.ResultCommitID), workQueueText(work.CancellationReason))
}

func workQueueClaimDetails(state workqueue.Projection, work *workqueue.WorkState, claim *workqueue.ClaimState) []string {
	blocks := []string{fmt.Sprintf("CLAIM %s\nState: %s | current: %t\nHandle: %s\nGrant commit: %s\nRequest: %s\nTerminal commit: %s\nReason: %s\nRetry not before: %d",
		workQueueText(claim.ClaimID), claim.State, work.ClaimID == claim.ClaimID && work.State != "cancelled", workQueueText(claim.Handle),
		workQueueText(claim.CommitID), workQueueText(claim.RequestID), workQueueText(claim.TerminalCommitID), workQueueText(claim.CancellationReason), claim.RetryNotBefore)}
	dispatch := state.Dispatches[claim.DispatchID]
	if dispatch == nil {
		return blocks
	}
	blocks = append(blocks, fmt.Sprintf("DISPATCH %s\nState: %s | native reservation retained: %t\nOriginal assignment: %d Claims (immutable)\nProfile: %s",
		workQueueText(dispatch.DispatchID), dispatch.State, !dispatch.Released, len(dispatch.Claims), workQueueText(dispatch.WorkerProfile)))
	if dispatch.Run != nil {
		blocks = append(blocks, fmt.Sprintf("Run: %s attempt %d\nWorkflow: %s\nRevision: %s\nPrincipal: %s",
			workQueueText(dispatch.Run.RunID), dispatch.Run.RunAttempt, workQueueText(dispatch.Run.Workflow), workQueueText(dispatch.Run.Ref), workQueueText(dispatch.Run.Principal)))
	}
	for _, member := range dispatch.Claims {
		blocks = append(blocks, fmt.Sprintf("  member: %s -> Work %s handle=%s", workQueueText(member.ClaimID), workQueueText(member.WorkID), workQueueText(member.Handle)))
	}
	return blocks
}

func workQueueDependencyDetails(state workqueue.Projection, work *workqueue.WorkState, explanation workqueue.WorkExplanation) (string, error) {
	lines := []string{"DEPENDENCIES (cross-references; successors require verified Result)"}
	if len(work.DependsOn) == 0 {
		lines = append(lines, "None")
	}
	for _, edge := range work.DependsOn {
		if edge.Kind == "work" {
			parent := state.Works[edge.WorkID]
			if parent == nil {
				return "", errors.New("dependency_missing: replayed Work dependency is absent")
			}
			lines = append(lines, fmt.Sprintf("  Work %s %s: %s delivery=%s", workQueueText(edge.WorkID), workQueueText(parent.NodeKey), parent.State, parent.Barrier))
		} else if edge.Resource != nil {
			lines = append(lines, fmt.Sprintf("  %s %s #%s: requires %s (typed fresh observation)", edge.Kind, workQueueText(edge.Resource.Repository), workQueueText(edge.Resource.Number), edge.Condition))
		}
	}
	for _, id := range explanation.Observations {
		lines = append(lines, "Observation: "+workQueueText(id))
	}
	return strings.Join(lines, "\n"), nil
}

func workInspectCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "inspect", Short: "Inspect metadata, dependencies and original assignment for exact Work or Claim", Args: cobra.NoArgs}
	cmd.Flags().String("work-id", "", "Exact Work ID")
	cmd.Flags().String("claim-id", "", "Exact Claim ID")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		workID, _ := cmd.Flags().GetString("work-id")
		claimID, _ := cmd.Flags().GetString("claim-id")
		if (workID == "") == (claimID == "") {
			return errors.New("provide exactly one of --work-id or --claim-id")
		}
		state, err := workRead(cmd)
		if err != nil {
			return err
		}
		row := workQueueRow{Kind: "work", ID: workID, WorkID: workID}
		if claimID != "" {
			claim := state.Claims[claimID]
			if claim == nil {
				return errors.New("claim_missing: unknown Claim")
			}
			row = workQueueRow{Kind: "claim", ID: claimID, WorkID: claim.WorkID}
			work := state.Works[claim.WorkID]
			row.Current = work != nil && work.ClaimID == claimID && work.State != "cancelled"
		}
		details, err := workQueueDetails(state, row, time.Now().UnixMilli())
		if err != nil {
			return err
		}
		return workPrint(cmd, struct {
			Status    string       `json:"status"`
			Tip       string       `json:"tip"`
			Selection workQueueRow `json:"selection"`
			Details   string       `json:"details"`
		}{"committed_snapshot", state.Tip, row, details}, details)
	}
	return cmd
}
