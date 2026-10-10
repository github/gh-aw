package cli

import (
	"cmp"
	"fmt"
	"os"
	"slices"
	"strconv"
	"time"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/errorutil"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/workflow"
	"github.com/spf13/cobra"
)

var healthLog = logger.New("cli:health")

// healthListWorkflowRuns is the run-listing entry point used by fetchWorkflowRuns.
// It is a variable so that pagination behaviour can be exercised in tests.
var healthListWorkflowRuns = listWorkflowRunsWithPagination

// HealthConfig holds configuration for health command execution
type HealthConfig struct {
	WorkflowName string
	Days         int
	Threshold    float64
	Verbose      bool
	JSONOutput   bool
	RepoOverride string
}

// NewHealthCommand creates the health command
func NewHealthCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "health [workflow]",
		Short: "Display workflow health metrics and success rates",
		Long: `Display workflow health metrics, success rates, and execution trends.

Shows health metrics for workflows including:
- Success/failure rates over a time period
- Trend indicators (↑ improving, → stable, ↓ degrading)
- Average execution duration
- Warnings when success rate drops below threshold

When called without a workflow name, displays summary for all workflows.
When called with a specific workflow name, displays detailed metrics for that workflow.

` + WorkflowIDExplanation,
		Example: `  ` + string(constants.CLIExtensionPrefix) + ` health                       # Summary of all workflows (last 7 days)
  ` + string(constants.CLIExtensionPrefix) + ` health issue-monster         # Detailed metrics for specific workflow
  ` + string(constants.CLIExtensionPrefix) + ` health --days 30             # Summary for last 30 days
  ` + string(constants.CLIExtensionPrefix) + ` health --threshold 90        # Warn if below 90% success rate
  ` + string(constants.CLIExtensionPrefix) + ` health --json                # Output in JSON format
  ` + string(constants.CLIExtensionPrefix) + ` health issue-monster --days 90  # 90-day metrics for workflow`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			days, _ := cmd.Flags().GetInt("days")
			threshold, _ := cmd.Flags().GetFloat64("threshold")
			verbose, _ := cmd.Flags().GetBool("verbose")
			jsonOutput, _ := cmd.Flags().GetBool("json")
			repoOverride, _ := cmd.Flags().GetString("repo")

			var workflowName string
			if len(args) > 0 {
				workflowName = args[0]
			}

			config := HealthConfig{
				WorkflowName: workflowName,
				Days:         days,
				Threshold:    threshold,
				Verbose:      verbose,
				JSONOutput:   jsonOutput,
				RepoOverride: repoOverride,
			}

			return RunHealth(config)
		},
	}

	// Add flags
	cmd.Flags().Int("days", 7, "Number of days to analyze (7, 30, or 90)")
	cmd.Flags().Float64("threshold", 80.0, "Success rate threshold for warnings (percentage)")
	addRepoFlag(cmd)
	addJSONFlag(cmd)

	// Register completions
	cmd.ValidArgsFunction = CompleteWorkflowNames

	return cmd
}

// RunHealth executes the health command with the given configuration
func RunHealth(config HealthConfig) error {
	healthLog.Printf("Running health check: workflow=%s, days=%d, threshold=%.1f", config.WorkflowName, config.Days, config.Threshold)

	// Validate days parameter
	if config.Days != 7 && config.Days != 30 && config.Days != 90 {
		return fmt.Errorf("invalid days value: %d. Must be 7, 30, or 90", config.Days)
	}

	// workflowAPIName is used for gh run list API calls. Using the lock file name
	// (e.g. "smoke-copilot.lock.yml") is more reliable than the display name because
	// the GitHub CLI matches by filename directly, avoiding "workflow not found" errors
	// that can occur when the workflow's display name doesn't match the registry.
	var workflowAPIName string

	// Resolve workflow name from workflow ID to GitHub Actions display name
	if config.WorkflowName != "" {
		resolvedName, err := workflow.FindWorkflowName(config.WorkflowName)
		if err != nil {
			return fmt.Errorf("workflow '%s' not found: %w", config.WorkflowName, err)
		}

		lockFileName, lockErr := workflow.GetWorkflowLockFileName(config.WorkflowName)
		if lockErr == nil {
			workflowAPIName = lockFileName
		} else {
			// Fall back to resolved display name if lock file lookup fails
			workflowAPIName = resolvedName
		}

		healthLog.Printf("Resolved workflow name: %s -> %s (API name: %s)", config.WorkflowName, resolvedName, workflowAPIName)
		config.WorkflowName = resolvedName
	}

	// Calculate start date
	startDate := time.Now().AddDate(0, 0, -config.Days).Format("2006-01-02")

	if config.Verbose {
		fmt.Fprintln(os.Stderr, console.FormatVerboseMessage("Fetching workflow runs since "+startDate))
	}

	// Fetch workflow runs from GitHub
	runs, err := fetchWorkflowRuns(workflowAPIName, startDate, config.RepoOverride, config.Verbose)
	if err != nil {
		if errorutil.IsRateLimitError(err.Error()) {
			// Rate limiting is a transient infrastructure condition, not a code error.
			// Warn and exit cleanly so CI jobs are not marked as failed.
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Skipping health check: GitHub API rate limit exceeded"))
			if config.JSONOutput && config.WorkflowName != "" {
				// Emit an empty-run JSON structure so callers can still parse the output.
				return displayDetailedHealth(nil, config)
			}
			return nil
		}
		return fmt.Errorf("failed to fetch workflow runs: %w", err)
	}

	if len(runs) == 0 {
		if config.WorkflowName != "" {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("No runs found for workflow '%s' in the last %d days", config.WorkflowName, config.Days)))
			// When JSON output is requested for a specific workflow, still output a valid
			// zero-run JSON structure so callers can parse the result programmatically.
			if config.JSONOutput {
				return displayDetailedHealth(runs, config)
			}
		} else {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("No workflow runs found in the last %d days", config.Days)))
		}
		return nil
	}

	if config.WorkflowName != "" {
		// Detailed view for specific workflow
		return displayDetailedHealth(runs, config)
	}

	// Summary view for all workflows
	return displayHealthSummary(runs, config)
}

// fetchWorkflowRuns fetches workflow runs from GitHub for the specified time period
func fetchWorkflowRuns(workflowName, startDate, repoOverride string, verbose bool) ([]WorkflowRun, error) {
	healthLog.Printf("Fetching workflow runs: workflow=%s, startDate=%s", workflowName, startDate)

	var oldestFetchedCreatedAt time.Time
	opts := ListWorkflowRunsOptions{
		WorkflowName:           workflowName,
		StartDate:              startDate,
		Limit:                  100,
		RepoOverride:           repoOverride,
		OldestFetchedCreatedAt: &oldestFetchedCreatedAt,
		Verbose:                verbose,
	}
	targetCount := opts.Limit

	allRuns := make([]WorkflowRun, 0)

	// Fetch runs in batches
	for i := range MaxIterations {
		runs, totalFetched, err := healthListWorkflowRuns(opts)
		if err != nil {
			return nil, err
		}

		// Accumulate runs; duration calculation is done here since the GitHub API
		// does not return a pre-computed duration field.
		for _, run := range runs {
			if run.Duration == 0 && !run.StartedAt.IsZero() && !run.UpdatedAt.IsZero() {
				run.Duration = run.UpdatedAt.Sub(run.StartedAt)
			}
			allRuns = append(allRuns, run)
		}

		healthLog.Printf("Fetched batch %d: got %d runs (%d before filtering), total agentic runs so far: %d", i+1, len(runs), totalFetched, len(allRuns))

		// If GitHub returned fewer raw runs than requested, the source is exhausted.
		// This must be measured on the raw batch, not on the filtered result: a batch
		// made entirely of skipped or approval-pending runs filters down to zero while
		// older dispatched runs are still available.
		if totalFetched < opts.Limit {
			break
		}

		// Stop once enough dispatched runs have been collected.
		if len(allRuns) >= targetCount {
			break
		}

		// Advance the cursor using the oldest raw run in the batch so that fully
		// filtered batches still move the window backwards.
		if oldestFetchedCreatedAt.IsZero() {
			break
		}
		opts.BeforeDate = oldestFetchedCreatedAt.Format(time.RFC3339)
	}

	healthLog.Printf("Total workflow runs fetched: %d", len(allRuns))
	return allRuns, nil
}

// displayHealthSummary displays a summary of health metrics for all workflows
func displayHealthSummary(runs []WorkflowRun, config HealthConfig) error {
	healthLog.Printf("Displaying health summary: %d runs", len(runs))

	// Group runs by workflow
	groupedRuns := GroupRunsByWorkflow(runs)

	// Calculate health for each workflow, marking intentional-failure workflows so they
	// are excluded from the fleet-health / prod-main success-rate rollup in CalculateHealthSummary.
	// Only classify against the local checkout when no remote repo override is active; when
	// --repo targets a different repository we cannot reliably read its frontmatter from the
	// local filesystem, so we fail open (IntentionalFailure stays false).
	workflowHealths := make([]WorkflowHealth, 0, len(groupedRuns))
	for workflowName, workflowRuns := range groupedRuns {
		health := CalculateWorkflowHealth(workflowName, workflowRuns, config.Threshold)
		if config.RepoOverride == "" {
			// Derive the workflow path from the first available run and check frontmatter.
			for _, r := range workflowRuns {
				if r.WorkflowPath != "" {
					health.IntentionalFailure = workflow.IsIntentionalFailure(r.WorkflowPath)
					break
				}
			}
		}
		workflowHealths = append(workflowHealths, health)
	}

	// Sort by success rate ascending (lowest first to highlight issues)
	slices.SortFunc(workflowHealths, func(a, b WorkflowHealth) int {
		return cmp.Compare(a.SuccessRate, b.SuccessRate)
	})

	// Calculate summary (intentional-failure workflows excluded from rollup counts)
	summary := CalculateHealthSummary(workflowHealths, fmt.Sprintf("Last %d Days", config.Days), config.Threshold)

	// Output results
	if config.JSONOutput {
		return outputHealthJSON(summary)
	}

	return outputHealthTable(summary, config.Threshold)
}

// displayDetailedHealth displays detailed health metrics for a specific workflow
func displayDetailedHealth(runs []WorkflowRun, config HealthConfig) error {
	healthLog.Printf("Displaying detailed health: workflow=%s, %d runs", config.WorkflowName, len(runs))

	// Calculate health metrics
	health := CalculateWorkflowHealth(config.WorkflowName, runs, config.Threshold)

	// Output results
	if config.JSONOutput {
		jsonBytes, err := marshalIndentJSONOrWrap(health, "workflow health report")
		if err != nil {
			return err
		}
		fmt.Fprintln(os.Stdout, string(jsonBytes))
		return nil
	}

	// Display header message
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Workflow Health: %s (Last %d Days)", config.WorkflowName, config.Days)))
	fmt.Fprintln(os.Stderr, "")

	// Create detailed view
	type DetailedHealth struct {
		Metric string `console:"header:Metric"`
		Value  string `console:"header:Value"`
	}

	details := []DetailedHealth{
		{"Total Runs", strconv.Itoa(health.TotalRuns)},
		{"Successful", strconv.Itoa(health.SuccessCount)},
		{"Failed", strconv.Itoa(health.FailureCount)},
		{"Success Rate", health.DisplayRate},
		{"Trend", health.Trend},
		{"Avg Duration", health.DisplayDur},
		{"Avg Tokens", health.DisplayTokens},
	}

	fmt.Fprint(os.Stderr, console.RenderStruct(details))
	fmt.Fprintln(os.Stderr, "")

	// Display warning if below threshold
	if health.BelowThresh {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Success rate (%.1f%%) is below threshold (%.1f%%)", health.SuccessRate, config.Threshold)))
	} else {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("Success rate (%.1f%%) is above threshold (%.1f%%)", health.SuccessRate, config.Threshold)))
	}

	return nil
}

// outputHealthJSON outputs health summary in JSON format
func outputHealthJSON(summary HealthSummary) error {
	jsonBytes, err := marshalIndentJSONOrWrap(summary, "health summary")
	if err != nil {
		return err
	}
	fmt.Fprintln(os.Stdout, string(jsonBytes))
	return nil
}

// outputHealthTable outputs health summary as a formatted table
func outputHealthTable(summary HealthSummary, threshold float64) error {
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Workflow Health Summary (%s)", summary.Period)))
	fmt.Fprintln(os.Stderr, "")

	// Render table
	fmt.Fprint(os.Stderr, console.RenderStruct(summary.Workflows))
	fmt.Fprintln(os.Stderr, "")

	// Display summary message
	if summary.BelowThreshold > 0 {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("%d workflow(s) below %.0f%% success threshold", summary.BelowThreshold, threshold)))
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Run '%s health <workflow-name>' for details", string(constants.CLIExtensionPrefix))))
	} else {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("All evaluated workflows above %.0f%% success threshold", threshold)))
	}

	return nil
}
