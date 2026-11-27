package cli

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/workqueue"
)

type WorkQueueDiagnostics struct {
	workQueueStatsReport
	AssignedClaims []WorkQueueAssignedClaimDiagnostics `json:"assigned_claims,omitempty"`
}

type WorkQueueAssignedClaimDiagnostics struct {
	Work  workQueueWorkMetadata  `json:"work"`
	Claim workQueueClaimMetadata `json:"claim"`
}

func capturedWorkQueueDiagnostics(state workqueue.Projection, envelope currentWorkQueueEnvelope) *WorkQueueDiagnostics {
	summary := workQueueStats(state, "")
	summary.Status = "captured_snapshot"
	result := &WorkQueueDiagnostics{workQueueStatsReport: summary}
	if envelope.Worker != nil {
		for _, member := range envelope.Worker.Claims {
			claim, work := state.Claims[member.ClaimID], state.Works[member.WorkID]
			if claim == nil || work == nil {
				continue
			}
			result.AssignedClaims = append(result.AssignedClaims, WorkQueueAssignedClaimDiagnostics{
				Work: workQueueWorkMetadataOf(state, work), Claim: workQueueClaimMetadataOf(state, claim),
			})
		}
	}
	return result
}

func equalWorkQueueDiagnostics(left, right *WorkQueueDiagnostics) bool {
	if left == nil || right == nil {
		return left == right
	}
	return left.Stats == right.Stats && left.workQueueSnapshot == right.workQueueSnapshot &&
		equalWorkQueueDiagnosticCounts(left.ClaimsByState, right.ClaimsByState) &&
		equalWorkQueueDiagnosticCounts(left.ClaimCancellationReasons, right.ClaimCancellationReasons) &&
		equalWorkQueueDiagnosticCounts(left.WorkCancellationReasons, right.WorkCancellationReasons) &&
		equalWorkQueueDiagnosticCounts(left.DispatchesByState, right.DispatchesByState) &&
		(left.Pools == nil) == (right.Pools == nil) && slices.EqualFunc(left.Pools, right.Pools, equalWorkQueuePoolStats) &&
		(left.AssignedClaims == nil) == (right.AssignedClaims == nil) &&
		slices.EqualFunc(left.AssignedClaims, right.AssignedClaims, equalWorkQueueAssignedClaimDiagnostics)
}

func equalWorkQueuePoolStats(left, right workQueuePoolStats) bool {
	return left.Pool == right.Pool && left.OpenDispatches == right.OpenDispatches && left.Eligible == right.Eligible &&
		equalWorkQueueDiagnosticCounts(left.WorkByState, right.WorkByState) &&
		equalWorkQueueDiagnosticCounts(left.ClaimsByState, right.ClaimsByState) &&
		equalWorkQueueDiagnosticCounts(left.Blocked, right.Blocked) &&
		equalWorkQueueSlices(left.NextEligible, right.NextEligible)
}

func equalWorkQueueAssignedClaimDiagnostics(left, right WorkQueueAssignedClaimDiagnostics) bool {
	return equalWorkQueueWorkMetadata(left.Work, right.Work) && equalWorkQueueClaimMetadata(left.Claim, right.Claim)
}

func equalWorkQueueWorkMetadata(a, b workQueueWorkMetadata) bool {
	return a.WorkID == b.WorkID && a.GraphID == b.GraphID && a.NodeKey == b.NodeKey && a.Pool == b.Pool &&
		a.WorkerProfile == b.WorkerProfile && a.State == b.State && a.Priority == b.Priority && a.FairnessKey == b.FairnessKey &&
		a.Position == b.Position && a.Attempts == b.Attempts && a.ClaimID == b.ClaimID && a.CompletionID == b.CompletionID &&
		equalWorkQueueSlices(a.Effects, b.Effects) && a.CancellationReason == b.CancellationReason &&
		a.CancellationCommitID == b.CancellationCommitID &&
		(a.DependsOn == nil) == (b.DependsOn == nil) &&
		slices.EqualFunc(a.DependsOn, b.DependsOn, func(x, y workqueue.Dependency) bool {
			return x.Kind == y.Kind && x.WorkID == y.WorkID && x.Condition == y.Condition && equalWorkQueuePointers(x.Resource, y.Resource)
		}) &&
		equalWorkQueuePointers(a.Subject, b.Subject) && equalWorkQueuePointers(a.BackingIssue, b.BackingIssue) &&
		a.IssueComment == b.IssueComment && a.Ready == b.Ready && a.Reason == b.Reason
}

func equalWorkQueueClaimMetadata(a, b workQueueClaimMetadata) bool {
	return a.ClaimID == b.ClaimID && a.WorkID == b.WorkID && a.DispatchID == b.DispatchID && a.Handle == b.Handle &&
		a.State == b.State && a.Current == b.Current && a.RequestID == b.RequestID && a.CommitID == b.CommitID &&
		a.TerminalCommitID == b.TerminalCommitID && a.CancellationReason == b.CancellationReason &&
		equalWorkQueueSlices(a.Effects, b.Effects)
}

func equalWorkQueueDiagnosticCounts(left, right map[string]int) bool {
	return (left == nil) == (right == nil) && maps.Equal(left, right)
}

func workQueueDiagnosticsText(diagnostics *WorkQueueDiagnostics) string {
	if diagnostics == nil {
		return ""
	}
	var output strings.Builder
	fmt.Fprintf(&output, "    work: available=%d claimed=%d completed=%d cancelled=%d (activation snapshot; no live reads)\n", diagnostics.Available, diagnostics.Claimed, diagnostics.Completed, diagnostics.Cancelled) //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
	fmt.Fprintf(&output, "    claims: open=%d completed=%d cancelled=%d | dispatches: reserved=%d started=%d bound=%d closed=%d\n",                                                                                      //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
		diagnostics.ClaimsByState["open"], diagnostics.ClaimsByState["completed"], diagnostics.ClaimsByState["cancelled"],
		diagnostics.DispatchesByState["reserved"], diagnostics.DispatchesByState["started"], diagnostics.DispatchesByState["bound"], diagnostics.DispatchesByState["closed"])
	if reasons := workQueueReasonCounts(diagnostics.ClaimCancellationReasons); reasons != "" {
		fmt.Fprintf(&output, "    claim_cancellations: %s\n", reasons) //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
	}
	if reasons := workQueueReasonCounts(diagnostics.WorkCancellationReasons); reasons != "" {
		fmt.Fprintf(&output, "    work_cancellations: %s\n", reasons) //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
	}
	for _, pool := range diagnostics.Pools[:min(len(diagnostics.Pools), 16)] {
		output.WriteString(workQueuePoolDiagnosticsText(pool))
	}
	if len(diagnostics.Pools) > 16 {
		output.WriteString("    pools: additional pools available in JSON\n")
	}
	for _, assigned := range diagnostics.AssignedClaims[:min(len(diagnostics.AssignedClaims), 16)] {
		fmt.Fprintf(&output, "    claim_diagnostics: %s state=%s work_attempts=%d\n", //nolint:fprintferrorunchecked // strings.Builder writes cannot fail.
			escapeWorkQueueDisplay(assigned.Claim.ClaimID), escapeWorkQueueDisplay(assigned.Claim.State), assigned.Work.Attempts)
	}
	if len(diagnostics.AssignedClaims) > 16 {
		output.WriteString("    claim_diagnostics: additional Claims available in JSON\n")
	}
	output.WriteString("    Finish intents and later operation logs do not update this snapshot.\n")
	return output.String()
}
