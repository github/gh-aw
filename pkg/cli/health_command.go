package cli

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
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
	Full               bool
	Checkpoint         string
	collection         *HealthCollection
	explicitTimeWindow bool
	inputStartDate     bool
	inputEndDate       bool
	inputLogsDir       bool
}

const healthCommandDescription = `Display workflow health metrics, success rates, and execution trends.

Without a workflow name, returns a compact, resumable fleet report for active
agentic workflows on the default branch, excluding pull request events. Historical
run outcomes and current-revision verdicts are reported separately. Use --full
for per-workflow evidence and --checkpoint to save or resume collection.

With a workflow name, shows detailed metrics including:
- Current-revision success/failure rates from recent runs
- Revision-aware recovery evidence, shared job/step failure clusters, and cached sub-agent failures
- Trend indicators (↑ improving, → stable, ↓ degrading)
- Average execution duration
- Warnings when success rate drops below threshold
- Work-queue diagnostics when a selected published workflow uses work-queue

Named checks scan non-PR runs on the repository's default branch before reviewing PR and other
branch runs, even when a PR's source branch has the default branch's name.
Their failures may reflect unmerged code rather than a default-branch regression.
Within each pass, inventories failure signals from run-list payloads, verifies one
representative per branch/event, then samples successful and pending runs.
The later pass reuses default-branch evidence for stopping, but still samples known
other-branch failure scopes independently. Reuses local Git objects when the
checkout matches the target, with GitHub API fallback.

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
(0 disables the deadline). Expiry returns explicitly partial evidence, not a
healthy verdict. In fleet reports, check coverage and next_action.

`

// NewHealthCommand creates the health command
func NewHealthCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "health [workflow]",
		Short: "Display workflow health metrics and success rates",
		Long:  healthCommandDescription + WorkflowIDExplanation,
		Example: `  ` + string(constants.CLIExtensionPrefix) + ` health                       # Default-branch fleet report
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
				Full:         getBoolFlag(cmd, "full"),
				Checkpoint:   getStringFlag(cmd, "checkpoint"),
				inputLogsDir: cmd.Flags().Changed("logs-dir"),
			})
		},
	}

	addHealthFlags(cmd)

	// Register completions
	cmd.ValidArgsFunction = CompleteWorkflowNames

	return cmd
}

func addHealthFlags(cmd *cobra.Command) {
	cmd.Flags().Int("days", 7, "Maximum evidence horizon in days (allowed values: 7, 30, or 90)")
	cmd.Flags().String("start-date", "", "Include runs created at or after this UTC date, RFC3339 timestamp, or relative time (e.g. -24h)")
	cmd.Flags().String("end-date", "", "Include runs created at or before this UTC date, RFC3339 timestamp, or relative time (default: now)")
	cmd.MarkFlagsMutuallyExclusive("days", "start-date")
	cmd.Flags().Float64("threshold", 80.0, "Success rate threshold for warnings (percentage)")
	cmd.Flags().String("logs-dir", defaultLogsOutputDir, "Existing logs cache for failure clustering and sub-agent failures (reports cache compatibility; no artifact downloads)")
	cmd.Flags().Duration("timeout", 2*time.Minute, "Maximum collection duration (e.g., 30s or 5m; 0 disables the deadline). Returns explicitly partial results when exhausted")
	cmd.Flags().Bool("full", false, "Include per-workflow and full failure evidence in the fleet report")
	cmd.Flags().String("checkpoint", "", "Create or resume a private fleet checkpoint at this path")
	addRepoFlag(cmd)
	addJSONFlag(cmd)
}

func runHealth(ctx context.Context, config HealthConfig) error {
	healthLog.Printf("Running health check: workflow=%s, days=%d, threshold=%.1f", config.WorkflowName, config.Days, config.Threshold)
	fleet := config.WorkflowName == ""
	if !fleet && (config.Checkpoint != "" || config.Full) {
		return errors.New("--checkpoint and --full require omitting the workflow name")
	}
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
	config.inputStartDate, config.inputEndDate = config.StartDate != "", config.EndDate != ""
	start, end, err := resolveHealthTimeWindow(config, time.Now())
	if err != nil {
		return err
	}
	config.StartDate, config.EndDate = start.Format(time.RFC3339Nano), end.Format(time.RFC3339Nano)
	if fleet {
		return runHealthFleet(ctx, config, start, end, started)
	}
	return runNamedHealth(ctx, config, start, end, started)
}

func runNamedHealth(ctx context.Context, config HealthConfig, start, end, started time.Time) error {
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
	if err := checkHealthTimeBudget(config.collection); err != nil {
		healthLog.Printf("Collection budget expired after work-queue checks: %v", err)
	}

	if len(runs) == 0 && len(config.collection.workflows) == 0 && !config.collection.Partial {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("No runs found for workflow '%s' created from %s to %s", config.WorkflowName, config.StartDate, config.EndDate)))
		if !config.JSONOutput {
			return nil
		}
	}
	return displayDetailedHealth(runs, config)
}

func newHealthCollection(ctx context.Context, config HealthConfig, start, end time.Time) *HealthCollection {
	return &HealthCollection{
		ProducerVersion:     GetVersion(),
		ReportSchemaVersion: 1,
		ObservedAt:          time.Now().UTC(),
		Capabilities:        []string{"default_branch_first", "failure_inventory", "source_revisions", "typed_job_steps", "cache_diagnostics", "cross_version_raw_jobs", "pr_lifecycle_filtering", "approval_expiry_filtering", "work_queue", "time_budget"},
		TimeoutSeconds:      config.Timeout.Seconds(),
		ctx:                 ctx,
		Host:                normalizedGitHubAPIHost(logsRateLimitHost(config.RepoOverride)),
		StartDate:           config.StartDate,
		EndDate:             config.EndDate,
		MaximumDays:         max(1, int(math.Ceil(end.Sub(start).Hours()/24))),
	}
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
		{"Runs with Detection Failures", strconv.Itoa(health.DetectionFailureRuns)},
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
	if err := renderHealthSessionEvidence(os.Stderr, health); err != nil {
		return err
	}
	return renderFailureReportToWriter(os.Stderr, health.FailureClusters, health.FailureInvestigation, "", config.Verbose)
}

func displayDetailedHealthStatus(health WorkflowHealth, threshold float64) {
	if health.DetectionFailureRuns > 0 {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("%d run(s) have threat-detection failures or threats; Actions success rate does not establish successful threat detection", health.DetectionFailureRuns)))
	}
	if health.SubagentFailureCount > 0 {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("%d sub-agent invocation(s) failed across %d run(s); Actions success rate does not establish healthy delegation", health.SubagentFailureCount, health.SubagentFailureRuns)))
	}
	if health.DetectionFailureRuns > 0 || health.SubagentFailureCount > 0 {
		return
	}
	if health.Status == "no_current_revision_runs" {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Current workflow revision has no matching runs; awaiting new evidence instead of investigating superseded failures"))
	} else if health.BelowThresh {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Success rate (%.1f%%) is below threshold (%.1f%%)", health.SuccessRate, threshold)))
	} else if health.Status == "healthy" || health.Status == "recovery_observed" {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("Success rate (%.1f%%) is above threshold (%.1f%%)", health.SuccessRate, threshold)))
	} else {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Current revision has insufficient evidence for a healthy status"))
	}
}
