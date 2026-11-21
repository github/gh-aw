package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"

	"github.com/github/gh-aw/pkg/workqueue"
	"github.com/spf13/cobra"
)

// workCancelOperations cancels exact available Work. Work already cancelled with
// the same reason is skipped; claimed Work must be released first.
func workCancelOperations(state workqueue.Projection, ids []string, reason string) ([]workqueue.Operation, error) {
	operations := []workqueue.Operation{}
	for _, id := range ids {
		work := state.Works[id]
		switch {
		case work == nil:
			return nil, fmt.Errorf("work_missing: unknown Work %s", workQueueText(id))
		case work.State == "cancelled" && work.CancellationReason == reason:
			continue
		case work.State == "claimed":
			return nil, fmt.Errorf("ownership_conflict: Work %s is claimed; release dispatch %s first", workQueueText(id), workQueueText(workQueueClaimDispatch(state, work)))
		case work.State != "available":
			return nil, fmt.Errorf("work_terminal: Work %s is %s", workQueueText(id), work.State)
		}
		operation, err := workqueue.Op(workqueue.WorkCancellationOperation{Kind: "WorkCancellation", WorkID: id, Reason: reason})
		if err != nil {
			return nil, err
		}
		operations = append(operations, operation)
	}
	return operations, nil
}

func workQueueClaimDispatch(state workqueue.Projection, work *workqueue.WorkState) string {
	if claim := state.Claims[work.ClaimID]; claim != nil {
		return claim.DispatchID
	}
	return ""
}

// workReleaseOperations releases a non-closed dispatch: Release cancels every open
// Claim, and Work whose next attempt would exceed its pool's max_attempts is cancelled.
func workReleaseOperations(state workqueue.Projection, dispatchID, reason string, maxAttempts func(string) int) ([]workqueue.Operation, error) {
	dispatch := state.Dispatches[dispatchID]
	if dispatch == nil {
		return nil, fmt.Errorf("dispatch_missing: unknown dispatch %s", workQueueText(dispatchID))
	}
	if dispatch.State == "closed" {
		return []workqueue.Operation{}, nil
	}
	values := []any{workqueue.ReleaseOperation{Kind: "Release", DispatchID: dispatchID, Reason: reason}}
	for _, claim := range state.OpenClaims(dispatch) {
		work := state.Works[claim.WorkID]
		if work.Attempts+1 >= maxAttempts(work.Pool) {
			values = append(values, workqueue.WorkCancellationOperation{Kind: "WorkCancellation", WorkID: work.WorkID, Reason: "attempts_exhausted", ClaimID: claim.ClaimID})
		}
	}
	operations := make([]workqueue.Operation, 0, len(values))
	for _, value := range values {
		operation, err := workqueue.Op(value)
		if err != nil {
			return nil, err
		}
		operations = append(operations, operation)
	}
	return operations, nil
}

func workPublicationSummary(published workqueue.Publication) map[string]any {
	summary := map[string]any{"committed": published.Committed, "recovered": published.Recovered, "request_id": published.Request.ID, "head": published.Head}
	if published.Commit != nil {
		summary["commit_id"] = published.Commit.ID
		summary["operations"] = len(published.Commit.Operations)
	}
	return summary
}

func workCancelCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use: "cancel-work", Short: "Cancel exact available Work (claimed Work must be released first)",
		Args: cobra.NoArgs,
	}
	cmd.Flags().StringSlice("work-id", nil, "Exact Work IDs, comma-separated or repeated (maximum 256)")
	cmd.Flags().String("reason", "operator", "Cancellation reason: "+workQueueReasonList(workqueue.WorkCancellationReasons))
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		ids, _ := cmd.Flags().GetStringSlice("work-id")
		reason, _ := cmd.Flags().GetString("reason")
		if !slices.Contains(workqueue.WorkCancellationReasons, reason) {
			return fmt.Errorf("reason_invalid: --reason must be one of %s", workQueueReasonList(workqueue.WorkCancellationReasons))
		}
		ids = slices.Compact(slices.Sorted(slices.Values(ids)))
		if len(ids) < 1 || len(ids) > workqueue.OperationsPerCommit || slices.Contains(ids, "") {
			return errors.New("--work-id requires 1..256 nonempty Work IDs")
		}
		published, err := workBranch(cmd).Publish(cmd.Context(), workActor(cmd), "cancel_work", workqueue.CancelWorkParameters{WorkIDs: ids, Reason: reason},
			func(state workqueue.Projection, _ workqueue.Request) ([]workqueue.Operation, error) {
				return workCancelOperations(state, ids, reason)
			})
		if err != nil {
			return err
		}
		if !published.Committed {
			return workPrint(cmd, workPublicationSummary(published), "Selected Work is already cancelled; nothing published")
		}
		return workPrint(cmd, workPublicationSummary(published), fmt.Sprintf("Cancelled %d Work (%s); request=%s", len(published.Commit.Operations), reason, published.Request.ID))
	}
	return cmd
}

func workReleaseCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use: "release", Short: "Release a dispatch: return its open Claims' Work to the pool (or cancel it after max attempts)",
		Long: "Release a non-closed dispatch. Every open Claim is cancelled with the given\n" +
			"reason and its Work becomes available again with one more attempt; Work that\n" +
			"reaches its pool's max_attempts (from .github/workflows/aw.json, default 3) is\n" +
			"cancelled with attempts_exhausted. Running workflow runs are not stopped: a\n" +
			"late worker finds its Claims closed and cannot complete them.",
		Args: cobra.NoArgs,
	}
	cmd.Flags().String("dispatch-id", "", "Exact dispatch ID")
	cmd.Flags().String("reason", "operator", "Release reason: "+workQueueReasonList(workqueue.ReleaseReasons))
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		dispatchID, _ := cmd.Flags().GetString("dispatch-id")
		reason, _ := cmd.Flags().GetString("reason")
		if dispatchID == "" {
			return errors.New("--dispatch-id is required")
		}
		if !slices.Contains(workqueue.ReleaseReasons, reason) {
			return fmt.Errorf("reason_invalid: --reason must be one of %s", workQueueReasonList(workqueue.ReleaseReasons))
		}
		maxAttempts := workMaxAttempts()
		published, err := workBranch(cmd).Publish(cmd.Context(), workActor(cmd), "release", workqueue.ReleaseParameters{DispatchID: dispatchID, Reason: reason},
			func(state workqueue.Projection, _ workqueue.Request) ([]workqueue.Operation, error) {
				return workReleaseOperations(state, dispatchID, reason, maxAttempts)
			})
		if err != nil {
			return err
		}
		if !published.Committed {
			return workPrint(cmd, workPublicationSummary(published), "Dispatch is already closed; nothing published")
		}
		return workPrint(cmd, workPublicationSummary(published), fmt.Sprintf("Released dispatch %s (%s); request=%s", workQueueText(dispatchID), reason, published.Request.ID))
	}
	return cmd
}

func workQueueReasonList(reasons []string) string {
	return fmt.Sprintf("%v", reasons)
}

func workSubmitCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use: "submit-work", Short: "Submit one Work item as the repository administrator",
		Args: cobra.NoArgs,
		RunE: workRunSubmitCommand,
	}
	cmd.Flags().String("file", "", "JSON payload object path (- for stdin)")
	cmd.Flags().String("graph-id", "", "Nonempty graph namespace (when omitted: canonical payload SHA256)")
	cmd.Flags().String("node-key", "root", "Nonempty node identity inside its graph")
	cmd.Flags().String("pool", "default", "Scheduling pool")
	cmd.Flags().Int("priority", 3, "Priority 1..5 (1 is dispatched first)")
	cmd.Flags().String("fairness-key", "", "Account key for per_account_limit (default: one shared bucket)")
	cmd.Flags().String("worker-profile", "", "Worker profile: the worker workflow ID (required)")
	return cmd
}

func workRunSubmitCommand(cmd *cobra.Command, _ []string) error {
	graph, _ := cmd.Flags().GetString("graph-id")
	key, _ := cmd.Flags().GetString("node-key")
	pool, _ := cmd.Flags().GetString("pool")
	priority, _ := cmd.Flags().GetInt("priority")
	fairness, _ := cmd.Flags().GetString("fairness-key")
	profile, _ := cmd.Flags().GetString("worker-profile")
	if cmd.Flags().Changed("graph-id") && graph == "" || key == "" {
		return errors.New("work_invalid: graph and node identities must be nonempty; omit --graph-id for the payload-hash default")
	}
	if profile == "" {
		return errors.New("work_invalid: --worker-profile is required (the worker workflow ID)")
	}
	payload, err := workReadJSON(cmd)
	if err != nil {
		return err
	}
	if !cmd.Flags().Changed("graph-id") {
		if graph, err = workqueue.IndependentGraphID(payload); err != nil {
			return err
		}
	}
	work, err := workqueue.NewWork(graph, key, pool, profile, fairness, priority, payload, nil)
	if err != nil {
		return err
	}
	published, err := workPublishWork(cmd, []workqueue.WorkDefinition{work})
	if err != nil {
		return err
	}
	if !published.Committed {
		return workPrint(cmd, map[string]any{"work_id": work.WorkID, "created": false}, "Work "+work.WorkID+" already admitted")
	}
	return workPrint(cmd, map[string]any{"work_id": work.WorkID, "created": !published.Recovered, "publication": workPublicationSummary(published)}, "Admitted Work "+work.WorkID)
}

// workPublishWork submits Work; already admitted identities are skipped (replay
// treats an existing Work ID as a no-op).
func workPublishWork(cmd *cobra.Command, works []workqueue.WorkDefinition) (workqueue.Publication, error) {
	ids := make([]string, 0, len(works))
	for _, work := range works {
		ids = append(ids, work.WorkID)
	}
	return workBranch(cmd).Publish(cmd.Context(), workActor(cmd), "submit", workqueue.WorkIDsParameters{WorkIDs: ids},
		func(state workqueue.Projection, _ workqueue.Request) ([]workqueue.Operation, error) {
			operations := []workqueue.Operation{}
			for _, work := range works {
				if _, exists := state.Works[work.WorkID]; exists {
					continue
				}
				operation, err := workqueue.Op(work)
				if err != nil {
					return nil, err
				}
				operations = append(operations, operation)
			}
			return operations, nil
		})
}

func workSubmitGraphCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use: "submit-graph", Short: "Atomically submit 1..256 complete v4 Work operations",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			data, err := workReadJSON(cmd)
			if err != nil {
				return err
			}
			var nodes []workqueue.WorkDefinition
			decoder := json.NewDecoder(bytes.NewReader(data))
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&nodes); err != nil || len(nodes) == 0 || len(nodes) > workqueue.OperationsPerCommit {
				return errors.New("graph_invalid: expected a JSON array of 1..256 complete Work operations")
			}
			if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
				return errors.New("graph_invalid: expected a JSON array of 1..256 complete Work operations")
			}
			for _, node := range nodes {
				if err := workqueue.ValidateWork(node); err != nil {
					return err
				}
			}
			published, err := workPublishWork(cmd, nodes)
			if err != nil {
				return err
			}
			if !published.Committed {
				return workPrint(cmd, workPublicationSummary(published), "Every graph node is already admitted")
			}
			return workPrint(cmd, workPublicationSummary(published), fmt.Sprintf("Admitted %d graph nodes atomically", len(published.Commit.Operations)))
		},
	}
	cmd.Flags().String("file", "", "JSON array of v4 Work operations (- for stdin)")
	return cmd
}
