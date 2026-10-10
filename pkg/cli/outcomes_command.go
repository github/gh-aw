package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/github"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/spf13/cobra"
)

var outcomesLog = logger.New("cli:outcomes")

// NewOutcomesCommand creates the outcomes command
func NewOutcomesCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "outcomes <run-id>",
		Short: "Check what happened to a workflow run's safe outputs",
		Long: `Evaluate the outcomes of safe output actions from a workflow run.

For each safe output (created issue, PR, comment, label, etc.), checks the current
state of the GitHub object to determine whether the action was accepted, rejected,
ignored, or is still pending.

This answers the question: "Did this workflow's actions actually help?"`,
		Example: `  ` + string(constants.CLIExtensionPrefix) + ` outcomes 1234567890                # Check outcomes for a specific run
  ` + string(constants.CLIExtensionPrefix) + ` outcomes 1234567890 --json         # JSON output
  ` + string(constants.CLIExtensionPrefix) + ` outcomes 1234567890 --repo o/r     # Specify repository
  ` + string(constants.CLIExtensionPrefix) + ` outcomes 1234567890 -v             # Verbose output`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			verbose, _ := cmd.Flags().GetBool("verbose")
			jsonOutput, _ := cmd.Flags().GetBool("json")
			repoOverride, _ := cmd.Flags().GetString("repo")
			outputDir, _ := cmd.Flags().GetString("output")
			outcomesDir, _ := cmd.Flags().GetString("outcomes-dir")

			runID, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid run ID %q: %w", args[0], err)
			}

			return RunOutcomes(cmd.Context(), OutcomesConfig{
				RunID:        runID,
				Verbose:      verbose,
				JSONOutput:   jsonOutput,
				RepoOverride: repoOverride,
				OutputDir:    outputDir,
				OutcomesDir:  outcomesDir,
			})
		},
	}

	addJSONFlag(cmd)
	addRepoFlag(cmd)
	addOutputFlag(cmd, defaultLogsOutputDir)
	cmd.Flags().String("outcomes-dir", "", "Write outcome JSONL to this directory for OTLP export")
	cmd.AddCommand(NewOutcomesHistorySubcommand())

	return cmd
}

// OutcomesConfig holds configuration for the outcomes command.
type OutcomesConfig struct {
	RunID        int64
	Verbose      bool
	JSONOutput   bool
	RepoOverride string
	OutputDir    string
	OutcomesDir  string
}

// OutcomesData is the structured output of the outcomes command.
type OutcomesData struct {
	RunID    int64           `json:"run_id"`
	Workflow string          `json:"workflow,omitempty"`
	Items    []OutcomeReport `json:"items"`
	Summary  OutcomeSummary  `json:"summary"`
}

// RunOutcomes executes the outcomes evaluation for a single run.
func RunOutcomes(ctx context.Context, config OutcomesConfig) error {
	outcomesLog.Printf("Evaluating outcomes for run %d", config.RunID)

	// Resolve repo
	repo := config.RepoOverride
	if repo == "" {
		slug, err := GetCurrentRepoSlug()
		if err != nil {
			return fmt.Errorf("could not determine repository: %w", err)
		}
		repo = slug
	}

	// Parse owner/repo for artifact download
	var owner, repoName, hostname string
	parts := strings.SplitN(repo, "/", 2)
	if len(parts) == 2 {
		owner = parts[0]
		repoName = parts[1]
	}

	// Determine output directory for this run
	outputDir := config.OutputDir
	if outputDir == "" {
		outputDir = defaultLogsOutputDir
	}
	runDir := filepath.Join(outputDir, fmt.Sprintf("run-%d", config.RunID))

	// Try to load from cache first
	summary, cached := loadRunSummary(runDir, config.Verbose)
	var items []CreatedItemReport

	if cached && summary != nil {
		items = extractCreatedItemsFromManifest(runDir)
		if len(items) == 0 {
			items = extractCreatedItemsFromManifest(filepath.Join(runDir, "safe-outputs-items"))
		}
		// Enrich with data from raw agent output (has issue_number etc.)
		items = enrichItemsFromAgentOutput(items, runDir, repo)
		if config.Verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Loaded %d safe output items from cache", len(items))))
		}
	}

	if len(items) == 0 {
		if config.Verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Downloading artifacts for run %d...", config.RunID)))
		}
		err := downloadRunArtifacts(ctx, downloadArtifactsOptions{runID: config.RunID, outputDir: runDir, verbose: config.Verbose, owner: owner, repo: repoName, hostname: hostname})
		if err != nil {
			return fmt.Errorf("failed to download artifacts for run %d: %w", config.RunID, err)
		}
		items = extractCreatedItemsFromManifest(runDir)
		if len(items) == 0 {
			items = extractCreatedItemsFromManifest(filepath.Join(runDir, "safe-outputs-items"))
		}
		items = enrichItemsFromAgentOutput(items, runDir, repo)
	}

	if len(items) == 0 {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("No safe output items found for this run"))
		if config.JSONOutput {
			data := OutcomesData{
				RunID:   config.RunID,
				Items:   []OutcomeReport{},
				Summary: OutcomeSummary{},
			}
			out, err := marshalIndentJSONOrWrap(data, "outcomes report")
			if err != nil {
				return err
			}
			fmt.Fprintln(os.Stdout, string(out))
		}
		return nil
	}

	if config.Verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Evaluating outcomes for %d safe output items...", len(items))))
	}

	// Run the evaluations
	mapping := github.LoadObjectiveMapping()
	reports := EvaluateOutcomes(ctx, items, repo, mapping)
	outcomeSummary := ComputeOutcomeSummary(reports, mapping)

	// Write outcome JSONL if requested (for OTLP export or downstream processing).
	// The --outcomes-dir flag takes precedence over the GH_AW_OUTCOMES_DIR env var.
	outcomesDir := config.OutcomesDir
	if outcomesDir == "" {
		outcomesDir = lookupEnv("GH_AW_OUTCOMES_DIR")
	}
	if outcomesDir != "" {
		writeOutcomeJSONL(outcomesDir, config.RunID, reports)
	}

	// Get workflow name from cache if available
	workflowName := ""
	if cached && summary != nil {
		workflowName = summary.Run.WorkflowName
	}

	if config.JSONOutput {
		data := OutcomesData{
			RunID:    config.RunID,
			Workflow: workflowName,
			Items:    reports,
			Summary:  outcomeSummary,
		}
		out, err := marshalIndentJSONOrWrap(data, "outcomes report")
		if err != nil {
			return err
		}
		fmt.Fprintln(os.Stdout, string(out))
		return nil
	}

	// Console output
	if workflowName != "" {
		fmt.Fprintf(os.Stderr, "\n%s\n", console.FormatInfoMessage(fmt.Sprintf("Outcomes for %s (run %d)", workflowName, config.RunID)))
	} else {
		fmt.Fprintf(os.Stderr, "\n%s\n", console.FormatInfoMessage(fmt.Sprintf("Outcomes for run %d", config.RunID)))
	}

	// Render the items
	fmt.Fprintln(os.Stderr)
	for _, r := range reports {
		resultStr := string(r.OutcomeStatus)
		detail := r.Detail
		if detail != "" {
			resultStr += " (" + detail + ")"
		}
		numStr := ""
		if r.ObjectNumber > 0 {
			numStr = fmt.Sprintf("#%d", r.ObjectNumber)
		}
		timeStr := ""
		if r.TimeToOutcomeHours > 0 {
			if r.TimeToOutcomeHours < 1 {
				timeStr = fmt.Sprintf("%.0fm", r.TimeToOutcomeHours*60)
			} else {
				timeStr = fmt.Sprintf("%.1fh", r.TimeToOutcomeHours)
			}
		}
		fmt.Fprintf(os.Stderr, "  %-28s %-12s %-40s %s\n", r.Type, numStr, resultStr, timeStr)
	}
	fmt.Fprintln(os.Stderr)

	// Render summary
	resolved := outcomeSummary.Accepted + outcomeSummary.Rejected
	fmt.Fprintf(os.Stderr, "  Acceptance: %d/%d", outcomeSummary.Accepted, resolved)
	if resolved > 0 {
		fmt.Fprintf(os.Stderr, " (%.0f%%)", outcomeSummary.AcceptanceRate*100)
	}
	fmt.Fprintln(os.Stderr)

	if outcomeSummary.Accepted > 0 {
		fmt.Fprintf(os.Stderr, "  Zero-touch: %d/%d (%.0f%%)\n",
			outcomeSummary.ZeroTouch, outcomeSummary.Accepted, outcomeSummary.ZeroTouchRate*100)
	}

	if outcomeSummary.Rejected > 0 {
		fmt.Fprintf(os.Stderr, "  Waste: %d/%d (%.0f%%)\n",
			outcomeSummary.Rejected, outcomeSummary.Total, outcomeSummary.WasteRate*100)
	}

	if outcomeSummary.Pending > 0 {
		fmt.Fprintf(os.Stderr, "  Pending: %d\n", outcomeSummary.Pending)
	}

	if outcomeSummary.MedianTimeToOutcome > 0 {
		fmt.Fprintf(os.Stderr, "  Median time to outcome: %.1fh\n", outcomeSummary.MedianTimeToOutcome)
	}

	fmt.Fprintln(os.Stderr)

	return nil
}
