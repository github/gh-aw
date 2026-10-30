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
		Short: "Run one workflow grader with a unified session log",
		Long: `Run one enabled built-in, JavaScript, or operational-value Bash grader from a local workflow. When run-id is provided,
the unified session is downloaded from the run's usage artifact. Otherwise,
read the unified session from standard input as JSONL or a JSON event array.
Always writes a versioned JSON envelope with the resolved grader definition and
its result. Executable source and config values are excluded from the definition;
config is {} with config_omitted: true. Results may contain sensitive diagnostics.
Built-ins and JavaScript compute over the session in memory. Repository-owned
operational-value Bash reads a staged session file and emits legacy metric arrays.
Shell is trusted code, not yet microVM-isolated; detected file writes are rejected.`,
		Example: `  ` + string(constants.CLIExtensionPrefix) + ` graders run weekly-research loops 123456789
  cat aw_session.jsonl | ` + string(constants.CLIExtensionPrefix) + ` graders run weekly-research loops`,
		Args: cobra.RangeArgs(2, 3),
		RunE: func(cmd *cobra.Command, args []string) error {
			var runID int64
			var err error
			if len(args) == 3 {
				runID, err = parseGraderRunID(args[2]) //nolint:uncheckedsliceindex // len(args) == 3.
				if err != nil {
					return err
				}
			}
			repoOverride, err := cmd.Flags().GetString("repo")
			if err != nil {
				return err
			}
			return runGrader(cmd.Context(), graderRunConfig{
				Workflow: args[0], //nolint:uncheckedsliceindex // cobra.RangeArgs requires at least two arguments.
				GraderID: args[1], //nolint:uncheckedsliceindex // cobra.RangeArgs requires at least two arguments.
				RunID:    runID,
				Repo:     repoOverride,
				Input:    cmd.InOrStdin(),
				Output:   cmd.OutOrStdout(),
				Path:     os.Getenv("PATH"), //nolint:osgetenvlibrary // CLI boundary supplies executable search paths, not credentials.
			})
		},
	}
	addRepoFlag(cmd)
	cmd.SetIn(os.Stdin)
	cmd.SetOut(os.Stdout)
	return cmd
}
