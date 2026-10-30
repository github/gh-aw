package cli

import (
	"errors"
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
	var logsDir string
	var additionalGraders []string
	cmd := &cobra.Command{
		Use:   "run <workflow-id> <grader-id> [run-id]",
		Short: "Run workflow graders on a session or downloaded logs",
		Long: `Run one enabled built-in, JavaScript, or operational-value Bash grader from a local workflow. When run-id is provided,
the unified session is downloaded from the run's usage artifact. Otherwise,
read the unified session from standard input as JSONL or a JSON event array.
Built-ins and JavaScript compute over the session in memory. Repository-owned
operational-value Bash reads a staged session file and emits legacy metric arrays.
Shell is trusted code, not yet microVM-isolated; detected file writes are rejected.

With --logs-dir, regrade existing downloaded runs offline and emit a JSON collection
containing original scores and separate additional results with provenance.
Select graders with workflow-id grader-id, --additional-grader workflow:grader, or both.
Collection regrading supports only built-ins and isolated JavaScript, never Bash.
Downloaded evidence is not changed; exported Harbor bundles are not accepted.`,
		Example: `  ` + string(constants.CLIExtensionPrefix) + ` graders run weekly-research loops 123456789
  cat aw_session.jsonl | ` + string(constants.CLIExtensionPrefix) + ` graders run weekly-research loops
  ` + string(constants.CLIExtensionPrefix) + ` graders run --logs-dir .github/aw/logs --additional-grader weekly-research:loops`,
		Args: func(cmd *cobra.Command, args []string) error {
			return validateGradersRunArgs(cmd, args, logsDir, additionalGraders)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return executeGradersRun(cmd, args, logsDir, additionalGraders)
		},
	}
	cmd.Flags().StringVar(&logsDir, "logs-dir", "", "Regrade existing downloaded run directories or a logs summary collection offline")
	cmd.Flags().StringSliceVar(&additionalGraders, "additional-grader", nil, "Additional workflow:grader selector (repeatable or comma-separated; requires --logs-dir)")
	addRepoFlag(cmd)
	cmd.SetIn(os.Stdin)
	cmd.SetOut(os.Stdout)
	return cmd
}

func validateGradersRunArgs(cmd *cobra.Command, args []string, logsDir string, additionalGraders []string) error {
	if logsDir != "" {
		if len(args) != 0 && len(args) != 2 {
			return errors.New("--logs-dir accepts either workflow-id grader-id or no positional arguments")
		}
		if len(args) == 0 && len(additionalGraders) == 0 {
			return errors.New("--logs-dir requires workflow-id grader-id or --additional-grader workflow:grader")
		}
		return nil
	}
	if len(additionalGraders) != 0 {
		return errors.New("--additional-grader requires --logs-dir for graders run")
	}
	return cobra.RangeArgs(2, 3)(cmd, args)
}

func executeGradersRun(cmd *cobra.Command, args []string, logsDir string, additionalGraders []string) error {
	if logsDir != "" {
		selectors := append([]string(nil), additionalGraders...)
		if len(args) == 2 {
			selectors = append(selectors, args[0]+":"+args[1]) //nolint:uncheckedsliceindex // len(args) == 2.
		}
		return runOfflineGraders(cmd.Context(), logsDir, selectors, cmd.OutOrStdout())
	}
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
		Workflow: args[0], //nolint:uncheckedsliceindex // Args validation requires two positional arguments outside collection mode.
		GraderID: args[1], //nolint:uncheckedsliceindex // Args validation requires two positional arguments outside collection mode.
		RunID:    runID,
		Repo:     repoOverride,
		Input:    cmd.InOrStdin(),
		Output:   cmd.OutOrStdout(),
		Path:     os.Getenv("PATH"), //nolint:osgetenvlibrary // CLI boundary supplies executable search paths, not credentials.
	})
}
