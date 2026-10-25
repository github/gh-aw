package cli

import (
	"github.com/github/gh-aw/pkg/constants"
	"github.com/spf13/cobra"
)

// ForecastConfig holds configuration for forecast command execution.
type ForecastConfig struct {
	// WorkflowIDs is the set of workflow IDs to forecast. When empty, all agentic
	// workflows in the repository are included.
	WorkflowIDs []string
	// Days is the historical window used to sample workflow runs.
	Days int
	// Period controls the aggregation granularity: "week" or "month".
	Period string
	// JSONOutput enables machine-readable JSON output.
	JSONOutput bool
	// Verbose enables verbose diagnostic output.
	Verbose bool
	// RepoOverride optionally targets a different repository.
	RepoOverride string
	// SampleSize is the maximum number of completed runs to sample per workflow.
	SampleSize int
	// EvalMode enables backtesting mode: the training window is shifted back by
	// one projection period and forecast quality is evaluated against the actual
	// runs observed in that period.
	EvalMode bool
	// TimeoutMinutes gracefully cancels forecast computation after the configured
	// number of minutes. Zero disables timeout.
	TimeoutMinutes int
	// DownloadConcurrency is the maximum number of usage-artifact downloads to run in
	// parallel. Zero or negative uses the default (defaultForecastDownloadConcurrency).
	DownloadConcurrency int
}

// NewForecastCommand creates the forecast command.
func NewForecastCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "forecast [workflow]...",
		Short: "Forecast AI Credit (AIC) usage for agentic workflows",
		Long: `Forecast AI Credit (AIC) usage for agentic workflows by sampling
recent run history and projecting forward on a per-week or per-month basis.

The forecaster downloads a sample of recent completed workflow runs and derives
per-run metrics (AIC, duration, success rate). When runs have been
previously processed by 'gh aw logs', cached token-usage data is used. The
observed run frequency is then projected to the target period using a statistical
simulation that models three sources of uncertainty: run count (Poisson), per-run
AIC usage (bootstrap resampling), and per-run success (Bernoulli).

All forecasts are estimates derived from historical samples and may be inaccurate.

Accounts for:
  - A/B experiment variants (results are split per variant when present)
  - Observed run frequency from GitHub Actions history
  - Per-run success rate

If no workflow arguments are provided, all agentic workflows in the repository
are included and displayed side-by-side for easy comparison.

Multiple workflow IDs may be provided to compare specific workflows.

Backtesting (--eval):
  Shifts the training window back by one projection period, builds the forecast,
  then measures actual runs in that period and computes quality metrics:
  P50 absolute/percentage error and whether the actual value fell inside the
  P10–P90 confidence interval. Use this to validate the model before relying on
  forward projections.

` + WorkflowIDExplanation,
		Example: `  ` + string(constants.CLIExtensionPrefix) + ` forecast                        # Forecast all workflows (monthly)
  ` + string(constants.CLIExtensionPrefix) + ` forecast ci-doctor              # Forecast a specific workflow
  ` + string(constants.CLIExtensionPrefix) + ` forecast ci-doctor daily-news    # Compare two workflows
  ` + string(constants.CLIExtensionPrefix) + ` forecast --period week           # Weekly projections
  ` + string(constants.CLIExtensionPrefix) + ` forecast --days 7               # Use 7-day history window
  ` + string(constants.CLIExtensionPrefix) + ` forecast --sample 50            # Sample up to 50 runs per workflow
  ` + string(constants.CLIExtensionPrefix) + ` forecast --timeout 10           # Stop gracefully after 10 minutes
  ` + string(constants.CLIExtensionPrefix) + ` forecast --json                 # Machine-readable JSON output
  ` + string(constants.CLIExtensionPrefix) + ` forecast --repo owner/repo      # Forecast in another repository
  ` + string(constants.CLIExtensionPrefix) + ` forecast --eval                 # Backtest: evaluate forecast quality against past data`,
		Args: cobra.ArbitraryArgs,
		RunE: runForecastCommand,
	}
	configureForecastCommand(cmd)
	return cmd
}

func runForecastCommand(cmd *cobra.Command, args []string) error {
	days, _ := cmd.Flags().GetInt("days")
	period, _ := cmd.Flags().GetString("period")
	jsonOutput, _ := cmd.Flags().GetBool("json")
	verbose, _ := cmd.Flags().GetBool("verbose")
	repoOverride, _ := cmd.Flags().GetString("repo")
	sampleSize, _ := cmd.Flags().GetInt("sample")
	evalMode, _ := cmd.Flags().GetBool("eval")
	timeoutMinutes, _ := cmd.Flags().GetInt("timeout")
	downloadConcurrency, _ := cmd.Flags().GetInt("concurrency")

	forecastRunLog.Printf("Forecast command invoked: workflow_count=%d, days=%d, period=%s, sample_size=%d, eval=%v, timeout_minutes=%d, json=%v, repo=%q",
		len(args), days, period, sampleSize, evalMode, timeoutMinutes, jsonOutput, repoOverride)

	config := ForecastConfig{
		WorkflowIDs:         args,
		Days:                days,
		Period:              period,
		JSONOutput:          jsonOutput,
		Verbose:             verbose,
		RepoOverride:        repoOverride,
		SampleSize:          sampleSize,
		EvalMode:            evalMode,
		TimeoutMinutes:      timeoutMinutes,
		DownloadConcurrency: downloadConcurrency,
	}

	return RunForecast(config)
}

func configureForecastCommand(cmd *cobra.Command) {
	cmd.Flags().Int("days", 30, "Historical window in days to sample run history (allowed values: 7, 30)")
	cmd.Flags().String("period", "month", "Aggregation period for projections: week or month")
	cmd.Flags().Int("sample", 100, "Maximum number of completed runs to sample per workflow")
	cmd.Flags().Bool("eval", false, "Evaluate forecast quality against past data (backtesting mode)")
	cmd.Flags().Int("timeout", 0, "Gracefully stop forecast computation after this many minutes (0 = no timeout)")
	cmd.Flags().Int("concurrency", 0, "Maximum number of concurrent usage-artifact downloads (0 = use default)")
	addRepoFlag(cmd)
	addJSONFlag(cmd)

	cmd.ValidArgsFunction = CompleteWorkflowNames
	_ = cmd.RegisterFlagCompletionFunc("days", func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return []string{"7", "30"}, cobra.ShellCompDirectiveNoFileComp
	})

}
