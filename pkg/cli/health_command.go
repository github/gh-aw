package cli

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"slices"
	"strconv"
	"time"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/spf13/cobra"
)

var healthLog = logger.New("cli:health")

// healthListWorkflowRuns is the run-listing entry point used by fetchWorkflowRuns.
// It is a variable so that pagination behaviour can be exercised in tests.
var healthListWorkflowRuns = listHealthWorkflowRuns

// HealthConfig holds configuration for health command execution
type HealthConfig struct {
	WorkflowName       string
	Days               int
	StartDate          string
	EndDate            string
	Threshold          float64
	Verbose            bool
	JSONOutput         bool
	RepoOverride       string
	LogsDir            string
	Timeout            time.Duration
	collection         *HealthCollection
	explicitTimeWindow bool
}

const healthCommandDescription = `Display workflow health metrics, success rates, and execution trends.

Shows health metrics for workflows including:
- Current-revision success/failure rates from recent runs
- Revision-aware recovery evidence, shared job/step failure clusters, and cached sub-agent failures
- Trend indicators (↑ improving, → stable, ↓ degrading)
- Average execution duration
- Warnings when success rate drops below threshold
- Work-queue diagnostics when a selected published workflow uses work-queue

Inventories failure signals from run-list payloads, verifies one representative
per branch/event, then samples successful and pending runs. Reuses local Git
objects when the checkout matches the target, with GitHub API fallback.
When called without a workflow name, displays summary for all workflows.
When called with a specific workflow name, displays detailed metrics for that workflow.

Use --start-date and --end-date to bound run creation times. Accepts UTC dates,
RFC3339 timestamps, or relative times such as -24h and -1w. An explicit start
replaces --days; otherwise --days counts backward from --end-date (default: now).
The report remains a bounded current-revision sample, not an exhaustive history.
Health compares each workflow's Markdown source with its pushed branch version,
stops at source changes, and excludes superseded failures from investigation.
An updated source without new runs is insufficient evidence, not proof of a fix.
Skipped/action_required runs and confirmed jobless waiting/failed attempts are ignored; job-backed waiting runs remain pending evidence.
Confirmed approval expiries are also removed from potential failure candidates.
Attempt-specific annotations and unavailable evidence are reported explicitly;
approval-annotation requests are cached and capped at 20 per collection.
PR cancellations are ignored only with merge/closure or head-deletion evidence
during the attempt and complete jobs without genuine failures. Failed PR checkouts
require an explicit closed-PR diagnostic and matching closure evidence. Ignored
runs are not successes. Unavailable lifecycle evidence remains explicit and the
run is retained. Lifecycle requests are cached and capped at 40 per collection,
including targeted failed-checkout job logs; no artifacts are downloaded.
Job-presence checks do not decode job/step details. Optional representative Jobs
enrichment is best-effort: incomplete inventories warn and remain metadata-only
unless compatible cached evidence is available. API quota and request limits
still stop collection; primary inventory and presence errors remain errors.
Collection has a two-minute time budget by default. Use --timeout to change it
(0 disables the deadline). Expiry returns available evidence with
collection.partial=true and reason=time_budget, not a healthy verdict.

`

// NewHealthCommand creates the health command
func NewHealthCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "health [workflow]",
		Short: "Display workflow health metrics and success rates",
		Long:  healthCommandDescription + WorkflowIDExplanation,
		Example: `  ` + string(constants.CLIExtensionPrefix) + ` health                       # Current status of all workflows
  ` + string(constants.CLIExtensionPrefix) + ` health issue-monster         # Detailed metrics for specific workflow
  ` + string(constants.CLIExtensionPrefix) + ` health --days 30             # Look back at most 30 days
  ` + string(constants.CLIExtensionPrefix) + ` health --threshold 90        # Warn if below 90% success rate
  ` + string(constants.CLIExtensionPrefix) + ` health --json                # Output in JSON format
  ` + string(constants.CLIExtensionPrefix) + ` health --start-date -24h      # Evidence from the last 24 hours
  ` + string(constants.CLIExtensionPrefix) + ` health --start-date 2026-10-14T04:49:11Z  # Evidence since a release
  ` + string(constants.CLIExtensionPrefix) + ` health --start-date 2026-10-14 --end-date 2026-10-15  # Bounded window
  ` + string(constants.CLIExtensionPrefix) + ` health issue-monster --days 90  # Current status; at most 90 days`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			threshold, _ := cmd.Flags().GetFloat64("threshold")

			var workflowName string
			if len(args) > 0 {
				workflowName = args[0]
			}

			timeout, _ := cmd.Flags().GetDuration("timeout")
			return runHealth(cmd.Context(), HealthConfig{
				WorkflowName: workflowName,
				Days:         getIntFlag(cmd, "days"),
				StartDate:    getStringFlag(cmd, "start-date"),
				EndDate:      getStringFlag(cmd, "end-date"),
				Threshold:    threshold,
				Verbose:      getBoolFlag(cmd, "verbose"),
				JSONOutput:   getBoolFlag(cmd, "json"),
				RepoOverride: getStringFlag(cmd, "repo"),
				LogsDir:      getStringFlag(cmd, "logs-dir"),
				Timeout:      timeout,
			})
		},
	}

	addHealthFlags(cmd)

	// Register completions
	cmd.ValidArgsFunction = CompleteWorkflowNames

	return cmd
}

func addHealthFlags(cmd *cobra.Command) {
	cmd.Flags().Int("days", 7, "Maximum evidence horizon in days (7, 30, or 90)")
	cmd.Flags().String("start-date", "", "Include runs created at or after this UTC date, RFC3339 timestamp, or relative time (e.g. -24h)")
	cmd.Flags().String("end-date", "", "Include runs created at or before this UTC date, RFC3339 timestamp, or relative time (default: now)")
	cmd.MarkFlagsMutuallyExclusive("days", "start-date")
	cmd.Flags().Float64("threshold", 80.0, "Success rate threshold for warnings (percentage)")
	cmd.Flags().String("logs-dir", defaultLogsOutputDir, "Existing logs cache for failure clustering and sub-agent failures (reports cache compatibility; no artifact downloads)")
	cmd.Flags().Duration("timeout", 2*time.Minute, "Maximum collection duration (e.g., 30s or 5m; 0 disables the deadline). Returns explicitly partial results when exhausted")
	addRepoFlag(cmd)
	addJSONFlag(cmd)
}

// RunHealth executes the health command with the given configuration
func RunHealth(config HealthConfig) error {
	return runHealth(context.Background(), config)
}

func runHealth(ctx context.Context, config HealthConfig) error {
	healthLog.Printf("Running health check: workflow=%s, days=%d, threshold=%.1f", config.WorkflowName, config.Days, config.Threshold)
	if config.Timeout < 0 {
		return errors.New("--timeout must not be negative")
	}
	started := time.Now()
	if config.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, config.Timeout)
		defer cancel()
	}

	config.explicitTimeWindow = config.StartDate != "" || config.EndDate != ""
	start, end, err := resolveHealthTimeWindow(config, time.Now())
	if err != nil {
		return err
	}
	config.StartDate, config.EndDate = start.Format(time.RFC3339Nano), end.Format(time.RFC3339Nano)

	if config.Verbose {
		fmt.Fprintln(os.Stderr, console.FormatVerboseMessage("Fetching workflow runs created from "+config.StartDate+" to "+config.EndDate))
	}

	// Fetch workflow runs from GitHub
	config.collection = newHealthCollection(ctx, config, start, end)
	config.collection.startedAt = started
	if err := discoverHealthWorkflows(&config); err != nil {
		return err
	}
	runs, err := fetchWorkflowRuns("", config.StartDate, config.RepoOverride, config.Verbose, config.collection)
	if err != nil {
		return fmt.Errorf("failed to fetch workflow runs: %w", err)
	}
	collectHealthWorkQueue(config.collection)
	checkHealthTimeBudget(config.collection)

	if len(runs) == 0 && len(config.collection.workflows) == 0 && !config.collection.Partial {
		if config.WorkflowName != "" {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("No runs found for workflow '%s' created from %s to %s", config.WorkflowName, config.StartDate, config.EndDate)))
			// When JSON output is requested for a specific workflow, still output a valid
			// zero-run JSON structure so callers can parse the result programmatically.
			if config.JSONOutput {
				return displayDetailedHealth(runs, config)
			}
		} else {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("No workflow runs found created from %s to %s", config.StartDate, config.EndDate)))
			if config.JSONOutput {
				return displayHealthSummary(runs, config)
			}
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

func newHealthCollection(ctx context.Context, config HealthConfig, start, end time.Time) *HealthCollection {
	return &HealthCollection{
		ProducerVersion:     GetVersion(),
		ReportSchemaVersion: 1,
		ObservedAt:          time.Now().UTC(),
		Capabilities:        []string{"failure_inventory", "source_revisions", "typed_job_steps", "cache_diagnostics", "cross_version_raw_jobs", "pr_lifecycle_filtering", "approval_expiry_filtering", "work_queue", "time_budget"},
		TimeoutSeconds:      config.Timeout.Seconds(),
		ctx:                 ctx,
		Host:                normalizedGitHubAPIHost(logsRateLimitHost(config.RepoOverride)),
		StartDate:           config.StartDate,
		EndDate:             config.EndDate,
		MaximumDays:         max(1, int(math.Ceil(end.Sub(start).Hours()/24))),
	}
}

// displayHealthSummary displays a summary of health metrics for all workflows
func displayHealthSummary(runs []WorkflowRun, config HealthConfig) error {
	healthLog.Printf("Displaying health summary: %d runs", len(runs))

	workflowHealths := calculateHealthWorkflowReports(runs, config)

	// Sort by success rate ascending (lowest first to highlight issues)
	slices.SortFunc(workflowHealths, func(a, b WorkflowHealth) int {
		return cmp.Or(cmp.Compare(a.SuccessRate, b.SuccessRate), cmp.Compare(a.WorkflowPath, b.WorkflowPath), cmp.Compare(a.WorkflowName, b.WorkflowName))
	})

	// Calculate summary (intentional-failure workflows excluded from rollup counts)
	summary := CalculateHealthSummary(workflowHealths, config.healthPeriod(), config.Threshold)
	summary.Collection = config.collection
	summary.FailureClusters, summary.FailureInvestigation = buildHealthFailureClusters(runs, config)
	finishHealthCollection(config.collection)

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
	health := calculateCurrentWorkflowHealth(config.WorkflowName, runs, config)
	health.Collection = config.collection
	health.FailureClusters, health.FailureInvestigation = buildHealthFailureClusters(runs, config)
	finishHealthCollection(config.collection)

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
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Workflow Health: %s (%s)", config.WorkflowName, config.healthPeriod())))
	fmt.Fprintln(os.Stderr, "")

	// Create detailed view
	type DetailedHealth struct {
		Metric string `console:"header:Metric"`
		Value  string `console:"header:Value"`
	}

	details := []DetailedHealth{
		{"Status", health.Status},
		{"Total Runs", strconv.Itoa(health.TotalRuns)},
		{"Successful", strconv.Itoa(health.SuccessCount)},
		{"Failed", strconv.Itoa(health.FailureCount)},
		{"Sub-agent Failures", strconv.Itoa(health.SubagentFailureCount)},
		{"Runs with Failed Sub-agents", strconv.Itoa(health.SubagentFailureRuns)},
		{"Success Rate", health.DisplayRate},
		{"Trend", health.Trend},
		{"Avg Duration", health.DisplayDur},
		{"Avg Tokens", health.DisplayTokens},
	}

	fmt.Fprint(os.Stderr, console.RenderStruct(details))
	fmt.Fprintln(os.Stderr, "")

	displayDetailedHealthStatus(health, config.Threshold)

	if err := renderHealthCollection(os.Stderr, health.Collection); err != nil {
		return err
	}
	if err := renderHealthRevisions(os.Stderr, health.Revisions); err != nil {
		return err
	}
	if err := renderHealthSubagentFailures(os.Stderr, health); err != nil {
		return err
	}
	return renderFailureReportToWriter(os.Stderr, health.FailureClusters, health.FailureInvestigation, "", config.Verbose)
}

func displayDetailedHealthStatus(health WorkflowHealth, threshold float64) {
	if health.SubagentFailureCount > 0 {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("%d sub-agent invocation(s) failed across %d run(s); Actions success rate does not establish healthy delegation", health.SubagentFailureCount, health.SubagentFailureRuns)))
	} else if health.Status == "no_current_revision_runs" {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Current workflow revision has no matching runs; awaiting new evidence instead of investigating superseded failures"))
	} else if health.BelowThresh {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Success rate (%.1f%%) is below threshold (%.1f%%)", health.SuccessRate, threshold)))
	} else if health.Status == "healthy" || health.Status == "recovery_observed" {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("Success rate (%.1f%%) is above threshold (%.1f%%)", health.SuccessRate, threshold)))
	} else {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Current revision has insufficient evidence for a healthy status"))
	}
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
	fmt.Fprintf(os.Stderr, "[health_population] workflows=%d healthy=%d observed_failure_workflows=%d subagent_degraded_workflows=%d statuses=%v\n",
		summary.TotalWorkflows, summary.HealthyWorkflows, summary.ObservedFailureWorkflows, summary.DegradedWorkflows, summary.StatusCounts)
	if summary.ObservedFailureWorkflows > 0 {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("%d workflow(s) have sampled current-revision Actions failures", summary.ObservedFailureWorkflows)))
	}
	if summary.DegradedWorkflows > 0 {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("%d workflow(s) have failed sub-agent invocations across %d run(s)", summary.DegradedWorkflows, summary.SubagentFailureRuns)))
	}
	if summary.BelowThreshold > 0 {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("%d workflow(s) below %.0f%% success threshold", summary.BelowThreshold, threshold)))
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Run '%s health <workflow-name>' for details", string(constants.CLIExtensionPrefix))))
	} else if summary.ObservedFailureWorkflows == 0 && summary.DegradedWorkflows == 0 && summary.InsufficientWorkflows == 0 && (summary.Collection == nil || !summary.Collection.Partial) {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("All evaluated workflows above %.0f%% success threshold", threshold)))
	} else if summary.InsufficientWorkflows > 0 {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("%d workflows have insufficient current-revision evidence", summary.InsufficientWorkflows)))
	} else if summary.Collection != nil && summary.Collection.Partial {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Health collection is partial; see collection limits"))
	}

	if err := renderHealthCollection(os.Stderr, summary.Collection); err != nil {
		return err
	}
	for _, health := range summary.Workflows {
		if err := renderHealthRevisions(os.Stderr, health.Revisions); err != nil {
			return err
		}
		if err := renderHealthSubagentFailures(os.Stderr, health); err != nil {
			return err
		}
	}
	return renderFailureReportToWriter(os.Stderr, summary.FailureClusters, summary.FailureInvestigation, "", false)
}
