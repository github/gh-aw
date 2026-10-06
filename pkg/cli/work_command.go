package cli

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/workqueue"
	"github.com/spf13/cobra"
)

func NewWorkCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use: "work-queue", Short: "Inspect and publish the current fair Git-backed DAG work queue",
		Long: "Inspect and publish the current fair Git-backed DAG work queue.\n\n" +
			"Direct claim and operator finish are unsupported. Use dispatch-next for mandatory\n" +
			"scheduler grants; only the originally bound worker may stage work_queue_claim_finish.\n" +
			"Use trace and historical explain for read-only provenance. compact normalizes\n" +
			"representation without dropping history, resetting debt, or adopting legacy queues.",
		Args: cobra.NoArgs,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			storage, _ := cmd.Flags().GetString("storage")
			if storage != "git" {
				return errors.New("unsupported_backend: work queues require Git storage; Issues storage is not supported")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	cmd.PersistentFlags().String("repo", "", "GitHub repository owner/repo")
	cmd.PersistentFlags().String("branch", workqueue.DefaultBranch, "Queue branch (an explicit branch is a separate authority)")
	cmd.PersistentFlags().String("storage", "git", "Queue storage (git only)")
	cmd.PersistentFlags().String("request-id", "", "Stable publication handle, or committed request to inspect with explain/trace")
	cmd.PersistentFlags().Bool("json", false, "Output JSON")
	cmd.AddCommand(workReplayCommand(), workStatsCommand(), workExplainCommand(),
		workSubmitCommand(), workSubmitGraphCommand(), workDispatchNextCommand(),
		workPolicyCommand(), workControlCommand(), workCancelCommand(),
		workReconcileCommand(), workEvidenceCommand(), workCompactCommand(), workTraceCommand())
	return cmd
}

func workBranch(cmd *cobra.Command) workqueue.Branch {
	repo, _ := cmd.Flags().GetString("repo")
	branch, _ := cmd.Flags().GetString("branch")
	return workqueue.Branch{Remote: repo, Name: branch}
}

func workPrint(cmd *cobra.Command, value any, human string) error {
	jsonOutput, _ := cmd.Flags().GetBool("json")
	if !jsonOutput {
		_, err := fmt.Fprintln(cmd.OutOrStdout(), human)
		return err
	}
	encoder := json.NewEncoder(cmd.OutOrStdout())
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func workRead(cmd *cobra.Command) (workqueue.Projection, error) {
	commits, err := workBranch(cmd).Read(cmd.Context())
	if err != nil {
		return workqueue.Projection{}, err
	}
	return workqueue.Replay(commits)
}

func workReplayCommand() *cobra.Command {
	return &cobra.Command{
		Use: "replay", Aliases: []string{"read"}, Short: "Validate the full causal queue and display ownership and barriers",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			projection, err := workRead(cmd)
			if err != nil {
				return err
			}
			ids := make([]string, 0, len(projection.Works))
			for id := range projection.Works {
				ids = append(ids, id)
			}
			slices.Sort(ids)
			lines := []string{}
			for _, id := range ids {
				item := projection.Works[id]
				lines = append(lines, fmt.Sprintf("%s  %s  delivery=%s", id, item.State, item.Barrier))
			}
			if len(lines) == 0 {
				lines = []string{"No Work; policy " + projection.PolicyEpoch}
			}
			return workPrint(cmd, projection, strings.Join(lines, "\n"))
		},
	}
}

func workStatsCommand() *cobra.Command {
	return &cobra.Command{
		Use: "stats", Short: "Count ownership, typed graph nodes, and native reservations separately",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			projection, err := workRead(cmd)
			if err != nil {
				return err
			}
			s := projection.Stats
			return workPrint(cmd, s, fmt.Sprintf("Work: %d; available: %d; claimed: %d; completed: %d; cancelled: %d; Claims: %d; native reservations: %d; graph nodes: %d",
				s.Work, s.Available, s.Claimed, s.Completed, s.Cancelled, s.Claims, s.Dispatches, s.Nodes))
		},
	}
}

func workExplainCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use: "explain", Short: "Explain live eligibility or an exact committed request/Claim using the authoritative scheduler",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			id, _ := cmd.Flags().GetString("work-id")
			beforeClaim, _ := cmd.Flags().GetString("before-claim")
			requestID, _ := cmd.Flags().GetString("request-id")
			selectors := 0
			for _, value := range []string{id, beforeClaim, requestID} {
				if value != "" {
					selectors++
				}
			}
			if selectors > 1 || (beforeClaim != "" || requestID != "") && cmd.Flags().Changed("pool") {
				return errors.New("inspection_invalid: explain selectors are mutually exclusive; historical requests determine their pool")
			}
			commits, err := workBranch(cmd).Read(cmd.Context())
			if err != nil {
				return err
			}
			if beforeClaim != "" {
				explanation, err := workqueue.ExplainBeforeClaim(commits, beforeClaim)
				if err != nil {
					return err
				}
				return workPrint(cmd, explanation, fmt.Sprintf("Committed Claim %s: selected %s before %s operation %d; tip %s",
					explanation.ClaimID, explanation.Selection.WorkID, explanation.CommitID, explanation.Position.Operation, explanation.Tip))
			}
			if requestID != "" {
				explanation, err := workqueue.ExplainRequest(commits, requestID)
				if err != nil {
					return err
				}
				return workPrint(cmd, explanation, fmt.Sprintf("Committed request %s (%s): %d Claims in %s; tip %s",
					explanation.RequestID, explanation.Kind, len(explanation.Claims), explanation.CommitID, explanation.Tip))
			}
			state, err := workqueue.Replay(commits)
			if err != nil {
				return err
			}
			at := time.Now().UnixMilli()
			if id != "" {
				explanation, err := workqueue.ExplainWork(state, id, at)
				if err != nil {
					return err
				}
				return workPrint(cmd, struct {
					workqueue.WorkExplanation
					Tip    string `json:"tip"`
					At     int64  `json:"at"`
					Status string `json:"status"`
				}{explanation, state.Tip, at, "operator_live_read"},
					explanation.Reason+": "+strings.Join(explanation.Path, " -> "))
			}
			pool, _ := cmd.Flags().GetString("pool")
			selection, err := workqueue.PlanNext(state, pool, at)
			if err != nil {
				return err
			}
			decision, err := workqueue.PlanDispatch(state, workqueue.DispatchParameters{
				Pool: pool, MaxClaims: 1, MaxDispatches: 1, MaxBytes: state.Policy.Limits.AssignmentBytes,
			}, "snapshot_prediction", "snapshot_commit", at)
			if err != nil {
				return err
			}
			return workPrint(cmd, map[string]any{"tip": state.Tip, "at": at, "status": "snapshot_prediction", "selection": selection, "decision": decision},
				"Snapshot prediction: "+decision.Reason+" "+selection.WorkID+" (not a grant)")
		},
	}
	cmd.Flags().String("pool", "default", "Approved scheduling pool")
	cmd.Flags().String("work-id", "", "Inspect this Work's dependency path (read only)")
	cmd.Flags().String("before-claim", "", "Replay the exact predecessor and earlier operations before this committed Claim")
	return cmd
}

func workCompactCommand() *cobra.Command {
	return &cobra.Command{
		Use: "compact", Short: "Canonicalize and deduplicate an existing queue without dropping history or resetting debt",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			result, err := workBranch(cmd).Compact(cmd.Context())
			if err != nil {
				return err
			}
			status := "Already canonical"
			if result.Changed {
				status = "Canonicalized"
			} else if result.AcknowledgmentRecovered {
				status = "Confirmed canonical after uncertain publication"
			}
			return workPrint(cmd, result, fmt.Sprintf("%s: retained %d commits; removed %d duplicate records; tip %s",
				status, result.Commits, result.DuplicatesRemoved, result.Tip))
		},
	}
}

func workTraceCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use: "trace", Short: "Trace exact committed request/Claim grants, bindings and delivery without exposing payloads or receipts",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			requestID, _ := cmd.Flags().GetString("request-id")
			claimID, _ := cmd.Flags().GetString("claim-id")
			offset, _ := cmd.Flags().GetInt("offset")
			limit, _ := cmd.Flags().GetInt("limit")
			if (requestID == "") == (claimID == "") || offset < 0 || limit < 1 || limit > 256 {
				return errors.New("inspection_invalid: trace requires exactly one --request-id/--claim-id and --offset >= 0, --limit 1..256")
			}
			commits, err := workBranch(cmd).Read(cmd.Context())
			if err != nil {
				return err
			}
			page, err := workqueue.TraceQueue(commits, workqueue.TraceOptions{
				RequestID: requestID, ClaimID: claimID, Offset: offset, Limit: limit,
			}, time.Now().UnixMilli())
			if err != nil {
				return err
			}
			lines := []string{fmt.Sprintf("Read-only trace: tip %s; events %d..%d of %d; %s",
				page.Tip, page.Offset, page.Offset+len(page.Events), page.TotalEvents, page.TraceAvailability)}
			for _, event := range page.Events {
				lines = append(lines, fmt.Sprintf("%s[%d] %s request=%s claim=%s dispatch=%s %s",
					event.CommitID, event.Position.Operation, event.Kind, event.RequestID, event.ClaimID, event.DispatchID, event.State))
			}
			return workPrint(cmd, page, strings.Join(lines, "\n"))
		},
	}
	cmd.Flags().String("claim-id", "", "Exact original Claim to trace (mutually exclusive with --request-id)")
	cmd.Flags().Int("offset", 0, "Matching event offset")
	cmd.Flags().Int("limit", 100, "Maximum events on this page (1..256)")
	return cmd
}

func workReadJSON(cmd *cobra.Command) ([]byte, error) {
	path, _ := cmd.Flags().GetString("file")
	if path == "" {
		return nil, errors.New("--file is required")
	}
	var reader io.Reader
	var file *os.File
	if path == "-" {
		reader = cmd.InOrStdin()
	} else {
		var err error
		file, err = os.Open(path)
		if err != nil {
			return nil, err
		}
		defer file.Close()
		reader = file
	}
	data, err := io.ReadAll(io.LimitReader(reader, 4<<20+1))
	if err != nil {
		return nil, err
	}
	if len(data) > 4<<20 {
		return nil, errors.New("resource_limit: input exceeds 4 MiB")
	}
	return workqueue.Canonical(data)
}

func workRequestID(cmd *cobra.Command) (string, error) {
	id, _ := cmd.Flags().GetString("request-id")
	if id != "" {
		return id, nil
	}
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes[:]), nil
}

func workPublish(cmd *cobra.Command, role, kind string, params any) (workqueue.Publication, error) {
	branch := workBranch(cmd)
	actor, err := branch.Authenticate(cmd.Context(), role)
	if err != nil {
		return workqueue.Publication{}, err
	}
	id, err := workRequestID(cmd)
	if err != nil {
		return workqueue.Publication{}, err
	}
	request, err := workqueue.NewRequest(id, kind, actor, params)
	if err != nil {
		return workqueue.Publication{}, err
	}
	return branch.Publish(cmd.Context(), actor, request)
}

func workSubmitCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use: "submit-work", Short: "Admit immutable Work with queue-like defaults and trusted policy entitlements",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			payload, err := workReadJSON(cmd)
			if err != nil {
				return err
			}
			branch := workBranch(cmd)
			actor, err := branch.Authenticate(cmd.Context(), "producer")
			if err != nil {
				return err
			}
			state, err := workRead(cmd)
			var policy workqueue.Policy
			if err == nil {
				policy = *state.Policy
			} else if strings.Contains(err.Error(), "queue_missing:") {
				policy = workqueue.DefaultPolicy(actor.Principal, actor.Repository)
			} else {
				return err
			}
			graph, _ := cmd.Flags().GetString("graph-id")
			key, _ := cmd.Flags().GetString("node-key")
			pool, _ := cmd.Flags().GetString("pool")
			if graph == "" {
				graph = workqueue.NodeID(string(payload), "root")
			}
			work, err := workqueue.NewWork(payload, graph, key, pool, policy, time.Now().UnixMilli())
			if err != nil {
				return err
			}
			work.Priority, _ = cmd.Flags().GetInt("priority")
			work.FairnessKey, _ = cmd.Flags().GetString("fairness-key")
			profile, _ := cmd.Flags().GetString("worker-profile")
			if profile != "" {
				approved, ok := policy.Pools[pool].Profiles[profile]
				if !ok {
					return errors.New("profile_invalid: worker profile is not approved")
				}
				work.WorkerProfile, work.BatchTrustDomain = profile, approved.TrustDomain
			}
			if existing := state.Works[work.WorkID]; existing != nil {
				work.Enqueued = existing.Enqueued
				left, _ := json.Marshal(existing.WorkDefinition)
				right, _ := json.Marshal(work)
				left, _ = workqueue.Canonical(left)
				right, _ = workqueue.Canonical(right)
				if string(left) != string(right) {
					return errors.New("work_conflict: idempotent submission changes immutable metadata")
				}
			}
			published, err := workPublish(cmd, "producer", "submit", workqueue.SubmitParameters{Nodes: []workqueue.WorkDefinition{work}})
			if err != nil {
				return err
			}
			if !published.Changed {
				return workPrint(cmd, map[string]any{
					"work_id": work.WorkID, "created": false, "status": "already_admitted", "publication": published,
				}, "Work "+work.WorkID+" already admitted")
			}
			return workPrint(cmd, map[string]any{"work_id": work.WorkID, "created": published.Changed, "publication": published},
				"Admitted Work "+work.WorkID)
		},
	}
	cmd.Flags().String("file", "", "JSON work object path (- for stdin)")
	cmd.Flags().String("graph-id", "", "Explicit graph namespace (default: identity-based independent root)")
	cmd.Flags().String("node-key", "root", "Stable node identity inside its graph")
	cmd.Flags().String("pool", "default", "Approved scheduling pool")
	cmd.Flags().Int("priority", 3, "Entitled priority 1..5")
	cmd.Flags().String("fairness-key", "", "Entitled accounting key (default: one shared bucket)")
	cmd.Flags().String("worker-profile", "", "Approved profile (default: pool default)")
	return cmd
}

func workSubmitGraphCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use: "submit-graph", Short: "Atomically admit a bounded array of normalized current-protocol graph nodes",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			data, err := workReadJSON(cmd)
			if err != nil {
				return err
			}
			var nodes []json.RawMessage
			if err := json.Unmarshal(data, &nodes); err != nil || len(nodes) == 0 || len(nodes) > 256 {
				return errors.New("graph_invalid: expected 1..256 complete normalized Work nodes")
			}
			published, err := workPublish(cmd, "producer", "submit", struct {
				Nodes []json.RawMessage `json:"nodes"`
			}{Nodes: nodes})
			if err != nil {
				return err
			}
			return workPrint(cmd, published, fmt.Sprintf("Admitted %d graph nodes atomically", len(nodes)))
		},
	}
	cmd.Flags().String("file", "", "Normalized current-protocol Work array (- for stdin)")
	return cmd
}

func workDispatchNextCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use: "dispatch-next", Short: "Grant a deterministic fair prefix; launch and binding remain trusted lifecycle steps",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			pool, _ := cmd.Flags().GetString("pool")
			claims, _ := cmd.Flags().GetInt("max-claims")
			dispatches, _ := cmd.Flags().GetInt("max-dispatches")
			bytes, _ := cmd.Flags().GetInt64("max-bytes")
			published, err := workPublish(cmd, "administrator", "dispatch_next", workqueue.DispatchParameters{
				Pool: pool, MaxClaims: claims, MaxDispatches: dispatches, MaxBytes: bytes,
			})
			if err != nil {
				return err
			}
			return workPrint(cmd, published, fmt.Sprintf("Fair prefix: %d Claims, %d reserved assignments; %s",
				len(published.Decision.Operations), len(published.Decision.Assignments), published.Decision.Reason))
		},
	}
	cmd.Flags().String("pool", "default", "Complete approved scheduling pool")
	cmd.Flags().Int("max-claims", 1, "Logical Claim budget for this request")
	cmd.Flags().Int("max-dispatches", 1, "Native dispatch budget for this request")
	cmd.Flags().Int64("max-bytes", 48<<10, "Assignment byte budget, bounded by policy and host")
	return cmd
}

func workPolicyCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use: "policy", Short: "Install an authenticated prospective policy epoch only on a quiescent queue",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			data, err := workReadJSON(cmd)
			if err != nil {
				return err
			}
			epoch, _ := cmd.Flags().GetString("epoch")
			if epoch == "" {
				return errors.New("--epoch is required")
			}
			var policy map[string]any
			if err := json.Unmarshal(data, &policy); err != nil {
				return err
			}
			operation := workqueue.Op(map[string]any{"kind": "Policy", "epoch": epoch, "policy": json.RawMessage(data)})
			published, err := workPublish(cmd, "administrator", "policy", workqueue.OperationsParameters{Operations: []workqueue.Operation{operation}})
			if err != nil {
				return err
			}
			return workPrint(cmd, published, "Installed prospective policy epoch "+epoch)
		},
	}
	cmd.Flags().String("file", "", "Complete QueuePolicy JSON (- for stdin)")
	cmd.Flags().String("epoch", "", "New unique policy epoch")
	return cmd
}

func workControlCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use: "control", Short: "Pause/resume admission or grants, or rotate equivalent credential generation; never reset debt",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			control, _ := cmd.Flags().GetString("name")
			reason, _ := cmd.Flags().GetString("reason")
			if reason == "" || !slices.Contains([]string{"admission_paused", "grants_paused", "credential_generation"}, control) {
				return errors.New("--name must be admission_paused, grants_paused, or credential_generation; --reason is required")
			}
			var value any
			if control == "credential_generation" {
				generation, _ := cmd.Flags().GetString("generation")
				if generation == "" {
					return errors.New("--generation is required for credential_generation")
				}
				value = generation
			} else {
				if !cmd.Flags().Changed("paused") {
					return errors.New("--paused=true or --paused=false is required")
				}
				value, _ = cmd.Flags().GetBool("paused")
			}
			published, err := workPublish(cmd, "administrator", "control", workqueue.OperationsParameters{Operations: []workqueue.Operation{
				workqueue.Op(map[string]any{"kind": "Control", "control": control, "value": value, "reason": reason}),
			}})
			if err != nil {
				return err
			}
			return workPrint(cmd, published, "Published "+control+"; ownership and fairness debt unchanged")
		},
	}
	cmd.Flags().String("name", "", "Closed operational control name")
	cmd.Flags().Bool("paused", false, "Explicit pause/resume value")
	cmd.Flags().String("generation", "", "Opaque equivalent-scope credential generation, never a secret")
	cmd.Flags().String("reason", "", "Sanitized operational reason code")
	return cmd
}

func workCancelCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use: "cancel-work", Short: "Cancel nonterminal ownership without releasing possibly executing native workers",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			id, _ := cmd.Flags().GetString("work-id")
			reason, _ := cmd.Flags().GetString("reason")
			if id == "" || reason == "" {
				return errors.New("--work-id and --reason are required")
			}
			published, err := workPublish(cmd, "administrator", "cancel_work", workqueue.OperationsParameters{Operations: []workqueue.Operation{
				workqueue.Op(map[string]any{"kind": "WorkCancellation", "work_id": id, "reason": reason}),
			}})
			if err != nil {
				return err
			}
			return workPrint(cmd, published, "Cancelled Work "+id+"; native reservation retained until definitive evidence")
		},
	}
	cmd.Flags().String("work-id", "", "Work to terminally cancel, not a Claim selector")
	cmd.Flags().String("reason", "", "Sanitized cancellation reason")
	return cmd
}

func workReconcileCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use: "reconcile", Short: "Reconcile exact native termination; retain uncertain reservations and independent delivery barriers",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			id, _ := cmd.Flags().GetString("dispatch-id")
			if id == "" {
				return errors.New("--dispatch-id is required")
			}
			request, err := workRequestID(cmd)
			if err != nil {
				return err
			}
			cancelReserved, _ := cmd.Flags().GetBool("cancel-reserved")
			var result workqueue.Reconciliation
			if cancelReserved {
				result, err = workBranch(cmd).CancelReserved(cmd.Context(), id, request)
			} else {
				result, err = workBranch(cmd).Reconcile(cmd.Context(), id, request)
			}
			if err != nil {
				return err
			}
			return workPrint(cmd, result, result.Reason+"; dispatch "+id)
		},
	}
	cmd.Flags().String("dispatch-id", "", "Immutable native reservation identity")
	cmd.Flags().Bool("cancel-reserved", false, "Cancel only if CAS proves no launch start marker exists (not a force release)")
	return cmd
}

func workEvidenceCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use: "evidence", Short: "Read authenticated native-run evidence (diagnostic, not an authorization or force release)",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			id, _ := cmd.Flags().GetString("dispatch-id")
			if id == "" {
				return errors.New("--dispatch-id is required")
			}
			result, err := workBranch(cmd).InspectEvidence(cmd.Context(), id)
			if err != nil {
				return err
			}
			return workPrint(cmd, result, result.Reason+"; dispatch "+id)
		},
	}
	cmd.Flags().String("dispatch-id", "", "Immutable native reservation identity")
	return cmd
}
