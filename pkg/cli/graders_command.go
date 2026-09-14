package cli

import (
	"os"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/spf13/cobra"
)

// NewGradersCommand creates commands for running workflow graders.
func NewGradersCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "graders",
		Short: "Run workflow graders",
	}
	cmd.AddCommand(newGradersRunCommand())
	return cmd
}

func newGradersRunCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run <workflow-id> <grader-id> [run-id]",
		Short: "Run one workflow grader with a JSON payload",
		Long: `Run one grader declared by a local workflow. When run-id is provided, the
preprocessed payload is downloaded from the run's agent artifact. Otherwise,
the JSON payload is read from standard input. Operational-value graders execute
either embedded Bash from the script field or a Bash file referenced by run.`,
		Example: `  ` + string(constants.CLIExtensionPrefix) + ` graders run weekly-research loops 123456789
  cat payload.json | ` + string(constants.CLIExtensionPrefix) + ` graders run weekly-research loops`,
		Args: cobra.RangeArgs(2, 3),
		RunE: func(cmd *cobra.Command, args []string) error {
			var runID int64
			var err error
			if len(args) == 3 {
				runID, err = parseGraderRunID(args[2])
				if err != nil {
					return err
				}
			}
			repoOverride, _ := cmd.Flags().GetString("repo")
			return runGrader(cmd.Context(), graderRunConfig{
				Workflow: args[0],
				GraderID: args[1],
				RunID:    runID,
				Repo:     repoOverride,
				Input:    cmd.InOrStdin(),
				Output:   cmd.OutOrStdout(),
			})
		},
	}
	addRepoFlag(cmd)
	cmd.SetIn(os.Stdin)
	cmd.SetOut(os.Stdout)
	return cmd
}
