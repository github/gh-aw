package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/githubnext/gh-aw/pkg/console"
	"github.com/githubnext/gh-aw/pkg/constants"
	"github.com/githubnext/gh-aw/pkg/logger"
	"github.com/spf13/cobra"
)

var healthLog = logger.New("cli:health")

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
- Success/failure rates over time period
- Trend indicators (↑ improving, → stable, ↓ degrading)
- Average execution duration
- Alerts when success rate drops below threshold

When called without a workflow name, displays summary for all workflows.
When called with a specific workflow name, displays detailed metrics for that workflow.

` + WorkflowIDExplanation + `

Examples:
  ` + string(constants.CLIExtensionPrefix) + ` health                       # Summary of all workflows (last 7 days)
  ` + string(constants.CLIExtensionPrefix) + ` health issue-monster         # Detailed metrics for specific workflow
  ` + string(constants.CLIExtensionPrefix) + ` health --days 30             # Summary for last 30 days
  ` + string(constants.CLIExtensionPrefix) + ` health --threshold 90        # Alert if below 90% success rate
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

	// Calculate start date
	startDate := time.Now().AddDate(0, 0, -config.Days).Format("2006-01-02")

	if config.Verbose {
		fmt.Fprintln(os.Stderr, console.FormatVerboseMessage(fmt.Sprintf("Fetching workflow runs since %s", startDate)))
	}

	// Fetch workflow runs from GitHub
	runs, err := fetchWorkflowRuns(config.WorkflowName, startDate, config.RepoOverride, config.Verbose)
	if err != nil {
		return fmt.Errorf("failed to fetch workflow runs: %w", err)
	}

	if len(runs) == 0 {
		if config.WorkflowName != "" {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("No runs found for workflow '%s' in the last %d days", config.WorkflowName, config.Days)))
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

	opts := ListWorkflowRunsOptions{
		WorkflowName: workflowName,
		StartDate:    startDate,
		Limit:        100,
		RepoOverride: repoOverride,
		Verbose:      verbose,
	}

	allRuns := make([]WorkflowRun, 0)

	// Fetch runs in batches
	for i := 0; i < MaxIterations; i++ {
		runs, totalCount, err := listWorkflowRunsWithPagination(opts)
		if err != nil {
			return nil, err
		}

		if len(runs) == 0 {
			break
		}

		// Filter to only agentic workflow runs (those ending in .lock.yml)
		for _, run := range runs {
			if strings.HasSuffix(run.WorkflowPath, ".lock.yml") {
				// Calculate duration if not set
				if run.Duration == 0 && !run.StartedAt.IsZero() && !run.UpdatedAt.IsZero() {
					run.Duration = run.UpdatedAt.Sub(run.StartedAt)
				}
				allRuns = append(allRuns, run)
			}
		}

		healthLog.Printf("Fetched batch %d: got %d runs, total agentic runs so far: %d", i+1, len(runs), len(allRuns))

		// If we got fewer runs than requested, we've reached the end
		if len(runs) < opts.Limit {
			break
		}

		// Update pagination for next batch
		if len(runs) > 0 {
			lastRun := runs[len(runs)-1]
			opts.BeforeDate = lastRun.CreatedAt.Format(time.RFC3339)
		}

		// Avoid fetching more than necessary
		if totalCount > 0 && len(allRuns) >= totalCount {
			break
		}
	}

	healthLog.Printf("Total workflow runs fetched: %d", len(allRuns))
	return allRuns, nil
}

// displayHealthSummary displays a summary of health metrics for all workflows
func displayHealthSummary(runs []WorkflowRun, config HealthConfig) error {
	healthLog.Printf("Displaying health summary: %d runs", len(runs))

	// Group runs by workflow
	groupedRuns := GroupRunsByWorkflow(runs)

	// Calculate health for each workflow
	workflowHealths := make([]WorkflowHealth, 0, len(groupedRuns))
	for workflowName, workflowRuns := range groupedRuns {
		health := CalculateWorkflowHealth(workflowName, workflowRuns, config.Threshold)
		workflowHealths = append(workflowHealths, health)
	}

	// Sort by success rate (lowest first to highlight issues)
	// Using a simple bubble sort for simplicity
	for i := 0; i < len(workflowHealths); i++ {
		for j := i + 1; j < len(workflowHealths); j++ {
			if workflowHealths[i].SuccessRate > workflowHealths[j].SuccessRate {
				workflowHealths[i], workflowHealths[j] = workflowHealths[j], workflowHealths[i]
			}
		}
	}

	// Calculate summary
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
		jsonBytes, err := json.MarshalIndent(health, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal JSON: %w", err)
		}
		fmt.Println(string(jsonBytes))
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
		{"Total Runs", fmt.Sprintf("%d", health.TotalRuns)},
		{"Successful", fmt.Sprintf("%d", health.SuccessCount)},
		{"Failed", fmt.Sprintf("%d", health.FailureCount)},
		{"Success Rate", health.DisplayRate},
		{"Trend", health.Trend},
		{"Avg Duration", health.DisplayDur},
		{"Avg Tokens", health.DisplayTokens},
		{"Avg Cost", fmt.Sprintf("$%s", health.DisplayCost)},
		{"Total Cost", fmt.Sprintf("$%.3f", health.TotalCost)},
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
	jsonBytes, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}
	fmt.Println(string(jsonBytes))
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
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("All workflows above %.0f%% success threshold", threshold)))
	}

	return nil
}
