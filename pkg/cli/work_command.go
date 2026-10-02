package cli

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/github/gh-aw/pkg/workqueue"
	"github.com/spf13/cobra"
)

func NewWorkCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "work",
		Short: "Inspect and update a Git-backed dispatch work queue",
		RunE:  func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	cmd.PersistentFlags().String("repo", "", "Repository owner/repo (or absolute local Git remote)")
	cmd.PersistentFlags().String("branch", workqueue.DefaultBranch, "Coordinator branch")
	cmd.PersistentFlags().Bool("json", false, "Output JSON")
	cmd.AddCommand(workReplayCommand(), workCompactCommand(), workStatsCommand(),
		workSubmitCommand(), workClaimCommand(), workFinishCommand(),
		workCancelCommand(), workCancelClaimCommand())
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

func workReplayCommand() *cobra.Command {
	return &cobra.Command{
		Use: "replay", Short: "Replay and validate the current queue",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			transactions, err := workBranch(cmd).Read(cmd.Context())
			if err != nil {
				return err
			}
			projection, err := workqueue.Replay(transactions)
			if err != nil {
				return err
			}
			var lines []string
			for _, item := range projection.Works {
				lines = append(lines, fmt.Sprintf("%s  %s  %s", item.WorkID, item.State, item.Winner))
			}
			if len(lines) == 0 {
				lines = []string{"No work"}
			}
			return workPrint(cmd, projection, strings.Join(lines, "\n"))
		},
	}
}

func workStatsCommand() *cobra.Command {
	return &cobra.Command{
		Use: "stats", Short: "Count work and claims by state",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			transactions, err := workBranch(cmd).Read(cmd.Context())
			if err != nil {
				return err
			}
			projection, err := workqueue.Replay(transactions)
			if err != nil {
				return err
			}
			s := projection.Stats
			return workPrint(cmd, s, fmt.Sprintf("Work: %d (available: %d, claimed: %d, completed: %d, cancelled: %d); claims: %d; transactions: %d",
				s.Work, s.Available, s.Claimed, s.Completed, s.Cancelled, s.Claims, s.Transactions))
		},
	}
}

func workCompactCommand() *cobra.Command {
	return &cobra.Command{
		Use: "compact", Short: "Deduplicate and canonically order the queue log",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var removed int
			_, changed, err := workBranch(cmd).Update(cmd.Context(), func(current []workqueue.Transaction) ([]workqueue.Transaction, bool, error) {
				next, err := workqueue.Compact(current)
				if err != nil {
					return nil, false, err
				}
				removed = len(current) - len(next)
				before, _ := workqueue.Serialize(current)
				after, _ := workqueue.Serialize(next)
				return next, !bytes.Equal(before, after), nil
			})
			if err != nil {
				return err
			}
			return workPrint(cmd, map[string]any{"changed": changed, "duplicates_removed": removed}, fmt.Sprintf("Compacted queue (removed %d duplicates, changed: %t)", removed, changed))
		},
	}
}

func workSubmitCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use: "submit-work", Short: "Submit a canonical JSON work payload",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			path, _ := cmd.Flags().GetString("file")
			if path == "" {
				return errors.New("--file is required")
			}
			var data []byte
			var err error
			if path == "-" {
				data, err = io.ReadAll(cmd.InOrStdin())
			} else {
				data, err = os.ReadFile(path)
			}
			if err != nil {
				return err
			}
			id, payload, err := workqueue.WorkID(data)
			if err != nil {
				return err
			}
			tx := workqueue.Transaction{Kind: "Work", WorkID: id, Work: payload}
			_, changed, err := workBranch(cmd).Update(cmd.Context(), func(current []workqueue.Transaction) ([]workqueue.Transaction, bool, error) {
				return workqueue.Apply(current, tx)
			})
			if err != nil {
				return err
			}
			return workPrint(cmd, map[string]any{"work_id": id, "created": changed}, fmt.Sprintf("Work %s (created: %t)", id, changed))
		},
	}
	cmd.Flags().String("file", "", "JSON work object path (- for stdin)")
	return cmd
}

func workID() (string, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes[:]), nil
}

func workClaimCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use: "claim", Short: "Add a claim with trusted owning run provenance",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			workIDValue, _ := cmd.Flags().GetString("work-id")
			runID, _ := cmd.Flags().GetString("run-id")
			if workIDValue == "" || runID == "" {
				return errors.New("--work-id and --run-id are required")
			}
			id, err := workID()
			if err != nil {
				return err
			}
			tx := workqueue.Transaction{Kind: "Claim", WorkID: workIDValue, ClaimID: id, RunID: runID}
			_, _, err = workBranch(cmd).Update(cmd.Context(), func(current []workqueue.Transaction) ([]workqueue.Transaction, bool, error) {
				return workqueue.Apply(current, tx)
			})
			if err != nil {
				return err
			}
			return workPrint(cmd, tx, fmt.Sprintf("Claim %s added to Work %s (replay to check effectiveness)", id, workIDValue))
		},
	}
	cmd.Flags().String("work-id", "", "Work identity")
	cmd.Flags().String("run-id", "", "Owning workflow run identity")
	return cmd
}

func workFinishCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use: "finish", Short: "Record Completion for the effective claim",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			claimID, _ := cmd.Flags().GetString("claim-id")
			attemptID, _ := cmd.Flags().GetString("attempt-id")
			outcome, _ := cmd.Flags().GetString("outcome")
			if claimID == "" || attemptID == "" {
				return errors.New("--claim-id and --attempt-id are required")
			}
			var completion workqueue.Transaction
			_, _, err := workBranch(cmd).Update(cmd.Context(), func(current []workqueue.Transaction) ([]workqueue.Transaction, bool, error) {
				for _, tx := range current {
					if tx.Kind == "Claim" && tx.ClaimID == claimID {
						completion = workqueue.Transaction{Kind: "Completion", WorkID: tx.WorkID, ClaimID: claimID, AttemptID: attemptID, Outcome: outcome}
						return workqueue.Apply(current, completion)
					}
				}
				return nil, false, fmt.Errorf("claim %s does not exist", claimID)
			})
			if err != nil {
				return err
			}
			return workPrint(cmd, completion, fmt.Sprintf("Completed Work %s with Claim %s", completion.WorkID, claimID))
		},
	}
	cmd.Flags().String("claim-id", "", "Effective claim identity")
	cmd.Flags().String("attempt-id", "", "Worker attempt identity")
	cmd.Flags().String("outcome", "", "Completion outcome")
	return cmd
}

func workCancelCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use: "cancel-work", Short: "Cancel nonterminal work",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			id, _ := cmd.Flags().GetString("work-id")
			if id == "" {
				return errors.New("--work-id is required")
			}
			tx := workqueue.Transaction{Kind: "WorkCancellation", WorkID: id}
			_, changed, err := workBranch(cmd).Update(cmd.Context(), func(current []workqueue.Transaction) ([]workqueue.Transaction, bool, error) {
				return workqueue.Apply(current, tx)
			})
			if err != nil {
				return err
			}
			return workPrint(cmd, map[string]any{"work_id": id, "cancelled": changed}, fmt.Sprintf("Work %s cancelled (changed: %t)", id, changed))
		},
	}
	cmd.Flags().String("work-id", "", "Work identity")
	return cmd
}

func workCancelClaimCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use: "cancel-claim", Short: "Cancel a claim on nonterminal work",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			id, _ := cmd.Flags().GetString("claim-id")
			if id == "" {
				return errors.New("--claim-id is required")
			}
			var cancellation workqueue.Transaction
			_, changed, err := workBranch(cmd).Update(cmd.Context(), func(current []workqueue.Transaction) ([]workqueue.Transaction, bool, error) {
				for _, tx := range current {
					if tx.Kind == "Claim" && tx.ClaimID == id {
						cancellation = workqueue.Transaction{Kind: "ClaimCancellation", WorkID: tx.WorkID, ClaimID: id}
						return workqueue.Apply(current, cancellation)
					}
				}
				return nil, false, fmt.Errorf("claim %s does not exist", id)
			})
			if err != nil {
				return err
			}
			return workPrint(cmd, map[string]any{"claim_id": id, "cancelled": changed}, fmt.Sprintf("Claim %s cancelled (changed: %t)", id, changed))
		},
	}
	cmd.Flags().String("claim-id", "", "Claim identity")
	return cmd
}
