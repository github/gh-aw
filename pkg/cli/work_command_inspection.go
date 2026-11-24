package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/workqueue"
	"github.com/spf13/cobra"
)

type workQueueSnapshot struct {
	Status     string `json:"status"`
	Tip        string `json:"tip"`
	Repository string `json:"repository"`
	Branch     string `json:"branch"`
}

func workQueueSnapshotOf(state workqueue.Projection, branch string) workQueueSnapshot {
	return workQueueSnapshot{Status: "committed_snapshot", Tip: state.Tip, Repository: state.Repository, Branch: branch}
}

type workQueueWorkMetadata struct {
	WorkID               string                 `json:"work_id"`
	GraphID              string                 `json:"graph_id"`
	NodeKey              string                 `json:"node_key"`
	Pool                 string                 `json:"pool"`
	WorkerProfile        string                 `json:"worker_profile"`
	State                string                 `json:"state"`
	Priority             int                    `json:"priority"`
	FairnessKey          string                 `json:"fairness_key"`
	Position             workqueue.Position     `json:"position"`
	Attempts             int                    `json:"attempts"`
	ClaimID              string                 `json:"claim_id,omitempty"`
	CompletionID         string                 `json:"completion_id,omitempty"`
	Effects              json.RawMessage        `json:"effects,omitempty"`
	CancellationReason   string                 `json:"cancellation_reason,omitempty"`
	CancellationCommitID string                 `json:"cancellation_commit_id,omitempty"`
	DependsOn            []workqueue.Dependency `json:"depends_on"`
	Subject              *workqueue.Resource    `json:"subject,omitempty"`
	BackingIssue         *workqueue.Resource    `json:"backing_issue,omitempty"`
	IssueComment         string                 `json:"issue_comment,omitempty"`
	Ready                bool                   `json:"ready"`
	Reason               string                 `json:"reason"`
}

type workQueueClaimMetadata struct {
	ClaimID            string          `json:"claim_id"`
	WorkID             string          `json:"work_id"`
	DispatchID         string          `json:"dispatch_id"`
	Handle             string          `json:"claim_handle"`
	State              string          `json:"state"`
	Current            bool            `json:"current"`
	RequestID          string          `json:"request_id"`
	CommitID           string          `json:"commit_id"`
	TerminalCommitID   string          `json:"terminal_commit_id,omitempty"`
	CancellationReason string          `json:"cancellation_reason,omitempty"`
	Effects            json.RawMessage `json:"effects,omitempty"`
}

type workQueueAssignmentMember struct {
	ClaimID string `json:"claim_id"`
	WorkID  string `json:"work_id"`
	Handle  string `json:"claim_handle"`
	State   string `json:"state"`
}

type workQueueDispatchMetadata struct {
	DispatchID    string                      `json:"dispatch_id"`
	RequestID     string                      `json:"request_id"`
	CommitID      string                      `json:"commit_id"`
	Pool          string                      `json:"pool"`
	WorkerProfile string                      `json:"worker_profile"`
	Workflow      string                      `json:"workflow"`
	State         string                      `json:"state"`
	Run           *workqueue.RunBinding       `json:"run"`
	Claims        []workQueueAssignmentMember `json:"claims"`
}

type workQueueInspection struct {
	workQueueSnapshot
	Selection workQueueRow               `json:"selection"`
	Work      *workQueueWorkMetadata     `json:"work,omitempty"`
	Claim     *workQueueClaimMetadata    `json:"claim,omitempty"`
	Dispatch  *workQueueDispatchMetadata `json:"dispatch,omitempty"`
	Details   string                     `json:"details"`
}

func workQueueWorkMetadataOf(state workqueue.Projection, work *workqueue.WorkState) workQueueWorkMetadata {
	ready, reason := state.DependencyStatus(work)
	if work.State != "available" {
		ready, reason = false, "state_"+work.State
	}
	return workQueueWorkMetadata{
		WorkID: work.WorkID, GraphID: work.GraphID, NodeKey: work.NodeKey, Pool: work.Pool,
		WorkerProfile: work.WorkerProfile, State: work.State, Priority: work.Priority,
		FairnessKey: work.FairnessKey, Position: work.Position, Attempts: work.Attempts,
		ClaimID: work.ClaimID, CompletionID: work.CompletionID, Effects: work.Effects,
		CancellationReason: work.CancellationReason, CancellationCommitID: work.CancellationCommitID,
		DependsOn: append([]workqueue.Dependency{}, work.DependsOn...), Subject: work.Subject,
		BackingIssue: work.BackingIssue, IssueComment: work.IssueComment, Ready: ready, Reason: reason,
	}
}

func workQueueClaimMetadataOf(state workqueue.Projection, claim *workqueue.ClaimState) workQueueClaimMetadata {
	work := state.Works[claim.WorkID]
	return workQueueClaimMetadata{
		ClaimID: claim.ClaimID, WorkID: claim.WorkID, DispatchID: claim.DispatchID, Handle: claim.Handle,
		State: claim.State, Current: work != nil && work.ClaimID == claim.ClaimID && work.State != "cancelled",
		RequestID: claim.RequestID, CommitID: claim.CommitID, TerminalCommitID: claim.TerminalCommitID,
		CancellationReason: claim.CancellationReason, Effects: claim.Effects,
	}
}

func workQueueDispatchMetadataOf(state workqueue.Projection, dispatch *workqueue.DispatchState) workQueueDispatchMetadata {
	result := workQueueDispatchMetadata{
		DispatchID: dispatch.DispatchID, RequestID: dispatch.RequestID, CommitID: dispatch.CommitID,
		Pool: dispatch.Pool, WorkerProfile: dispatch.WorkerProfile, Workflow: dispatch.Workflow,
		State: dispatch.State, Run: dispatch.Run, Claims: []workQueueAssignmentMember{},
	}
	for _, id := range dispatch.Claims {
		if claim := state.Claims[id]; claim != nil {
			result.Claims = append(result.Claims, workQueueAssignmentMember{ClaimID: claim.ClaimID, WorkID: claim.WorkID, Handle: claim.Handle, State: claim.State})
		}
	}
	return result
}

// workQueueInspect resolves the selected row's Work, Claim (current or selected) and Dispatch.
func workQueueInspect(state workqueue.Projection, branch string, row workQueueRow) (workQueueInspection, error) {
	result := workQueueInspection{workQueueSnapshot: workQueueSnapshotOf(state, branch), Selection: row}
	if row.Kind == "graph" {
		result.Details = "Graph " + workQueueText(row.ID) + "\nSelect a Work or Claim for details.\nDependencies are cross-references, not forest parenthood."
		return result, nil
	}
	work := state.Works[row.WorkID]
	if work == nil {
		return result, errors.New("work_missing: selected Work is absent; refresh state")
	}
	metadata := workQueueWorkMetadataOf(state, work)
	result.Work = &metadata
	claimID := work.ClaimID
	if row.Kind == "claim" {
		claimID = row.ID
	}
	if claimID != "" {
		claim := state.Claims[claimID]
		if claim == nil {
			return result, errors.New("claim_missing: selected Claim is absent; refresh state")
		}
		claimMetadata := workQueueClaimMetadataOf(state, claim)
		result.Claim = &claimMetadata
		if dispatch := state.Dispatches[claim.DispatchID]; dispatch != nil {
			dispatchMetadata := workQueueDispatchMetadataOf(state, dispatch)
			result.Dispatch = &dispatchMetadata
		}
	}
	result.Details = workQueueInspectionText(state, result)
	return result, nil
}

func workQueueInspectionText(state workqueue.Projection, inspection workQueueInspection) string {
	work := inspection.Work
	blocks := []string{
		"Tip: " + workQueueText(inspection.Tip),
		fmt.Sprintf("WORK %s\nID: %s\nGraph: %s\nPool: %s\nPriority: %d (1 is dispatched first)\nFairness key: %s\nWorker profile: %s\nState: %s | scheduling: %s\nPosition: %d/%d | attempts: %d\nCurrent/last Claim: %s\nCompletion: %s | effects: %d\nCancellation: %s",
			workQueueText(work.NodeKey), workQueueText(work.WorkID), workQueueText(work.GraphID), workQueueText(work.Pool), work.Priority,
			workQueueText(work.FairnessKey), workQueueText(work.WorkerProfile), work.State, work.Reason, work.Position.Commit, work.Position.Operation,
			work.Attempts, workQueueText(work.ClaimID), workQueueText(work.CompletionID), workQueueEffectCount(work.Effects), workQueueText(work.CancellationReason)),
	}
	if work.BackingIssue != nil {
		blocks = append(blocks, fmt.Sprintf("Backing Issue: %s #%s", workQueueText(work.BackingIssue.Repository), workQueueText(work.BackingIssue.Number)))
	}
	if claim := inspection.Claim; claim != nil {
		blocks = append(blocks, fmt.Sprintf("CLAIM %s\nState: %s | current: %t\nHandle: %s\nClaim commit: %s\nRequest: %s\nTerminal commit: %s\nReason: %s\nEffects: %d",
			workQueueText(claim.ClaimID), claim.State, claim.Current, workQueueText(claim.Handle), workQueueText(claim.CommitID),
			workQueueText(claim.RequestID), workQueueText(claim.TerminalCommitID), workQueueText(claim.CancellationReason), workQueueEffectCount(claim.Effects)))
	}
	if dispatch := inspection.Dispatch; dispatch != nil {
		run := "none"
		if dispatch.Run != nil {
			run = fmt.Sprintf("%s attempt %d", workQueueText(dispatch.Run.RunID), dispatch.Run.RunAttempt)
		}
		lines := []string{fmt.Sprintf("DISPATCH %s\nState: %s (reserved -> started -> bound -> closed)\nWorkflow: %s | profile: %s\nRun: %s",
			workQueueText(dispatch.DispatchID), dispatch.State, workQueueText(dispatch.Workflow), workQueueText(dispatch.WorkerProfile), run)}
		for _, member := range dispatch.Claims {
			lines = append(lines, fmt.Sprintf("  member: %s -> Work %s handle=%s state=%s", workQueueText(member.ClaimID), workQueueText(member.WorkID), workQueueText(member.Handle), member.State))
		}
		blocks = append(blocks, strings.Join(lines, "\n"))
	}
	blocks = append(blocks, workQueueDependencyText(state, work.DependsOn),
		"Cancel is terminal for available Work; claimed Work is released through its dispatch.\nReleasing does not stop a running workflow: its late finish finds the Claim closed.\nNo payloads or effect resources are displayed; use --json for recorded effects.")
	text := strings.Join(blocks, "\n\n")
	if len(text) > workQueueViewBytes-256 {
		end := strings.LastIndex(text[:workQueueViewBytes-256], "\n")
		text = text[:end] + "\n... metadata omitted at the view byte bound; use --json."
	}
	return text
}

func workQueueDependencyText(state workqueue.Projection, edges []workqueue.Dependency) string {
	lines := []string{"DEPENDENCIES (cross-references; successors require completed predecessors)"}
	if len(edges) == 0 {
		lines = append(lines, "None")
	}
	for _, edge := range edges {
		if edge.Kind == "work" {
			status := "missing"
			if parent := state.Works[edge.WorkID]; parent != nil {
				status = parent.State
			}
			lines = append(lines, fmt.Sprintf("  Work %s: %s", workQueueText(edge.WorkID), status))
		} else if edge.Resource != nil {
			observed := "unobserved"
			if observation, ok := state.Observations[workqueue.GateKey(*edge.Resource, edge.Condition)]; ok {
				observed = observation.State
			}
			lines = append(lines, fmt.Sprintf("  %s %s #%s: requires %s (observed %s)", edge.Kind, workQueueText(edge.Resource.Repository), workQueueText(edge.Resource.Number), edge.Condition, observed))
		}
	}
	return strings.Join(lines, "\n")
}

func workInspectCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "inspect", Short: "Inspect metadata, dependencies, dispatch, run and effects for exact Work or Claim", Args: cobra.NoArgs}
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
		}
		branch, _ := cmd.Flags().GetString("branch")
		inspection, err := workQueueInspect(state, branch, row)
		if err != nil {
			return err
		}
		return workPrint(cmd, inspection, inspection.Details)
	}
	return cmd
}
