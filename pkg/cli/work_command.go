package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/gitutil"
	"github.com/github/gh-aw/pkg/workflow"
	"github.com/github/gh-aw/pkg/workqueue"
	"github.com/spf13/cobra"
)

const workQueueBranchDescription = "Queue branch holding work-queue.jsonl"

// defaultWorkQueueMaxAttempts applies when no compiled pool configuration is available.
const defaultWorkQueueMaxAttempts = 3

func NewWorkCommand() *cobra.Command {
	var root *cobra.Command
	root = &cobra.Command{
		Use: "work-queue", Aliases: []string{"work"}, Short: "Inspect and operate the Git-backed priority-FIFO work queue",
		Long: "Inspect and operate the Git-backed work queue (protocol v4).\n\n" +
			"Dispatchers claim and launch Work; workers bind and finish their own Claims;\n" +
			"agentics-maintenance releases dispatches whose runs ended. Operator commands\n" +
			"publish as the administrator of the target repository: cancel-work, release,\n" +
			"clear, and compact (checkpoint).",
		Args: cobra.NoArgs,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			repo, _ := cmd.Flags().GetString("repo")
			fileReplay := cmd.Name() == "replay" && cmd.Flags().Changed("file")
			if repo == "" && cmd.Parent() == root && !fileReplay {
				repo, err := getWorkQueueRepoSlug()
				if err != nil {
					return err
				}
				return cmd.Flags().Set("repo", repo)
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	root.PersistentFlags().StringP("repo", "r", "", "Target repository (owner/repo format)")
	root.PersistentFlags().String("branch", workqueue.DefaultBranch, workQueueBranchDescription)
	root.PersistentFlags().BoolP("json", "j", false, "Output results in JSON format")
	root.AddCommand(workReplayCommand(), workStatsCommand(), workStateCommand(), workInspectCommand(), workTUICommand(),
		workSubmitCommand(), workSubmitGraphCommand(), workCancelCommand(), workReleaseCommand(), workClearCommand(),
		workCompactCommand(), workDispatchCommand())
	return root
}

func workBranch(cmd *cobra.Command) workqueue.Branch {
	repo, _ := cmd.Flags().GetString("repo")
	branch, _ := cmd.Flags().GetString("branch")
	return workqueue.Branch{Remote: repo, Name: branch}
}

// workActor is the operator actor: write access to the queue branch is the only
// authority v4 relies on, so no API self-authentication is performed.
func workActor(cmd *cobra.Command) workqueue.Actor {
	repo, _ := cmd.Flags().GetString("repo")
	return workqueue.Actor{Role: "administrator", Repository: repo}
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
	snapshot, err := workBranch(cmd).Read(cmd.Context())
	if err != nil {
		return workqueue.Projection{}, err
	}
	state := snapshot.State
	if state.Repository == "" {
		state.Repository, _ = cmd.Flags().GetString("repo")
	}
	return state, nil
}

// workMaxAttempts resolves each pool's compiled max_attempts from the checkout's
// .github/workflows/aw.json (the source of the compiled policy environment).
func workMaxAttempts() func(string) int {
	settings := workqueue.Settings{}
	loaded := false
	if root, err := gitutil.FindGitRoot(); err == nil {
		if config, err := workflow.LoadRepoConfig(root); err == nil && config != nil {
			if parsed, err := workqueue.ParseSettings(config.WorkQueue); err == nil {
				settings, loaded = parsed, true
			}
		}
	}
	return func(pool string) int {
		if !loaded {
			return defaultWorkQueueMaxAttempts
		}
		if configured, ok := settings.Pools[pool]; ok {
			return configured.Retry.MaxAttempts
		}
		return settings.Retry.MaxAttempts
	}
}

func workReadJSON(cmd *cobra.Command) (data []byte, retErr error) {
	path, _ := cmd.Flags().GetString("file")
	if path == "" {
		return nil, errors.New("--file is required")
	}
	var reader io.Reader
	if path == "-" {
		reader = cmd.InOrStdin()
	} else {
		file, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer func() {
			if closeErr := file.Close(); closeErr != nil {
				retErr = errors.Join(retErr, closeErr)
			}
		}()
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

func workReplayCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use: "replay", Aliases: []string{"read"}, Short: "Validate the queue ledger and list Work states",
		Long: "Replay the live ledger, or use --run-id to download an exact Actions activation\n" +
			"snapshot (--file accepts an already downloaded snapshot). Artifact replay is\n" +
			"read-only; --pool predicts the next dispatch from the captured ledger.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			runID, _ := cmd.Flags().GetInt64("run-id")
			file, _ := cmd.Flags().GetString("file")
			if cmd.Flags().Changed("run-id") || cmd.Flags().Changed("file") {
				return workReplayArtifact(cmd, runID, file)
			}
			for _, flag := range []string{"pool", "max-claims", "max-dispatches", "concurrency"} {
				if cmd.Flags().Changed(flag) {
					return errors.New("artifact_replay_invalid: scheduling predictions require --run-id or --file")
				}
			}
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
				lines = append(lines, fmt.Sprintf("%s  %s  attempts=%d", id, item.State, item.Attempts))
			}
			if len(lines) == 0 {
				lines = []string{"No Work"}
			}
			return workPrint(cmd, workQueueProjectionJSON(projection), strings.Join(lines, "\n"))
		},
	}
	cmd.Flags().Int64("run-id", 0, "Replay the activation snapshot from this exact Actions run (read only)")
	cmd.Flags().String("file", "", "Replay a downloaded work-queue.snapshot.json (read only)")
	cmd.Flags().String("pool", "", "Predict the next dispatch from the captured ledger without writes or launches")
	cmd.Flags().Int("max-claims", 1, "Predicted Claim budget")
	cmd.Flags().Int("max-dispatches", 1, "Predicted dispatch budget")
	cmd.Flags().Int("concurrency", 16, "Predicted pool concurrency (non-closed dispatches)")
	return cmd
}

// workQueueProjectionJSON is the canonical projection (the checkpoint state format) plus stats.
func workQueueProjectionJSON(state workqueue.Projection) any {
	data, err := workqueue.SerializeProjection(state)
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	return map[string]any{"projection": json.RawMessage(data), "stats": state.Stats}
}

func workStatsCommand() *cobra.Command {
	return &cobra.Command{
		Use: "stats", Short: "Count Work, Claims and dispatches per pool",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			projection, err := workRead(cmd)
			if err != nil {
				return err
			}
			branch, _ := cmd.Flags().GetString("branch")
			report := workQueueStats(projection, branch)
			return workPrint(cmd, report, workQueueStatsText(report))
		},
	}
}

func workCompactCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use: "compact", Short: "Replace the ledger with one Checkpoint commit (Git history keeps the prior log)",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			minimum, _ := cmd.Flags().GetInt("min-lines")
			if minimum < 1 {
				return errors.New("--min-lines must be at least 1")
			}
			result, err := workBranch(cmd).Checkpoint(cmd.Context(), workActor(cmd), minimum)
			if err != nil {
				return err
			}
			summary := map[string]any{"checkpointed": result.Checkpointed, "head": result.Head, "lines": result.Lines, "tip": result.State.Tip}
			if !result.Checkpointed {
				return workPrint(cmd, summary, fmt.Sprintf("No checkpoint: the ledger has %d lines (threshold %d)", result.Lines, minimum))
			}
			return workPrint(cmd, summary, fmt.Sprintf("Checkpointed %d lines into one commit; prior log retained at Git parent of %s", result.Lines, result.Head))
		},
	}
	cmd.Flags().Int("min-lines", 1, "Checkpoint only when the ledger has more than this many lines")
	return cmd
}
