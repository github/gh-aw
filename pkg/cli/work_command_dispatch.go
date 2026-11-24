package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

func workDispatchCommand() *cobra.Command {
	return workDispatchCommandWithRunner(RunWorkflowOnGitHub)
}

func workDispatchCommandWithRunner(run func(context.Context, string, RunOptions) error) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dispatch <dispatcher>",
		Short: "Run a work-queue dispatcher workflow to claim and launch the next Work",
		Long: "Run a work-queue dispatcher workflow through the GitHub CLI on the target\n" +
			"repository's default branch. The dispatcher claims eligible Work in priority,\n" +
			"then submission order, within its pool's compiled concurrency, and launches\n" +
			"one worker per dispatch. This command cannot choose a Work or reassign a Claim.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dispatcher := args[0] //nolint:uncheckedsliceindex // cobra.ExactArgs(1) guarantees one argument.
			if dispatcher == "" || strings.ContainsAny(dispatcher, "/\\") {
				return errors.New("dispatcher_invalid: expected a dispatcher workflow basename")
			}
			branch := workBranch(cmd)
			result := map[string]string{"dispatcher": dispatcher, "repository": branch.Remote, "status": "preview"}
			if preview, _ := cmd.Flags().GetBool("dry-run"); preview {
				return workPrint(cmd, result, fmt.Sprintf("Dispatcher preview: %s on %s; no Claims or workflows launched", dispatcher, branch.Remote))
			}
			wait, _ := cmd.Flags().GetBool("wait")
			if err := run(cmd.Context(), dispatcher, RunOptions{
				WorkflowRunFlags:  WorkflowRunFlags{RepoOverride: branch.Remote},
				WaitForCompletion: wait,
			}); err != nil {
				return err
			}
			result["status"] = "activated"
			return workPrint(cmd, result, fmt.Sprintf("Dispatcher activated: %s on %s; inspect the ledger for dispatches and Claims", dispatcher, branch.Remote))
		},
	}
	cmd.Flags().Bool("dry-run", false, "Preview the dispatcher run without launching it")
	cmd.Flags().Bool("wait", false, "Wait for the dispatcher run to finish")
	return cmd
}
