// This file provides command-line interface functionality for gh-aw.
// This file (logs_command.go) contains the CLI command definition for the logs command.
//
// Key responsibilities:
//   - Defining the Cobra command structure and flags for gh aw logs
//   - Parsing command-line arguments and flags
//   - Validating inputs (workflow names, dates, engine parameters)
//   - Delegating execution to the orchestrator (DownloadWorkflowLogs)

package cli

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/sliceutil"
	"github.com/github/gh-aw/pkg/workflow"
	"github.com/spf13/cobra"
)

var logsCommandLog = logger.New("cli:logs_command")

// NewLogsCommand creates the logs command
func NewLogsCommand() *cobra.Command {
	logsCmd := &cobra.Command{
		Use:   "logs [workflow]",
		Short: "Download and analyze agentic workflow logs with aggregated metrics",
		Long: `Download workflow run logs and artifacts from GitHub Actions for agentic workflows.

This command fetches workflow runs, downloads their artifacts, and extracts them into
organized folders named by run ID. It also provides an overview table with aggregate
metrics including duration, token usage, and cost information.

Downloaded artifacts include:
- aw_info.json: Engine configuration and workflow metadata
- safe_output.jsonl: Agent's final output content (available when non-empty)
- agent_output/: Agent logs directory (if the workflow produced logs)
- agent-stdio.log: Agent standard output/error logs
- aw.patch: Git patch of changes made during execution
- workflow-logs/: GitHub Actions workflow run logs (job logs organized in subdirectory)
- summary.json: Complete metrics and run data for all downloaded runs

Orchestrator Usage:
	In an orchestrator workflow, use this command in a pre-step to download logs,
  then access the data in subsequent steps without needing GitHub CLI access:

    steps:
      - name: Download logs from last 30 days
        env:
          GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
        run: |
          mkdir -p /tmp/portfolio-logs
          gh aw logs <worker> --start-date -1mo -o /tmp/portfolio-logs

  In your analysis step, reference the pre-downloaded data:
    
    **All workflow execution data has been pre-downloaded for you in the previous workflow step.**
    
    - **JSON Summary**: /tmp/portfolio-logs/summary.json - Contains all metrics and run data you need
    - **Run Logs**: /tmp/portfolio-logs/run-{database-id}/ - Individual run logs (if needed for detailed analysis)
    
    **DO NOT call 'gh aw logs' or any GitHub CLI commands** - they will not work in your environment.
    All data you need is in the summary.json file.

	Live Tracking with Project Boards:
		Use the summary.json data to update your project board, treating issues/PRs (workers)
		on the board as the real-time view of progress, ownership, and status. The orchestrator workflow
		can use the 'update-project' safe output to sync status fields without modifying worker workflow
		files. Workers remain unchanged while the board reflects current execution state.
    
    For incremental updates, pull data for each worker based on the last pull time using --start-date
    (e.g., --start-date -1d for daily updates) and align with existing board items. Compare run data
    from summary.json with board status to update only changed workers, preserving board state.

` + WorkflowIDExplanation + `

Examples:
  ` + string(constants.CLIExtensionPrefix) + ` logs                           # Download logs for all workflows
  ` + string(constants.CLIExtensionPrefix) + ` logs weekly-research           # Download logs for specific workflow
  ` + string(constants.CLIExtensionPrefix) + ` logs weekly-research.md        # Download logs (alternative format)
  ` + string(constants.CLIExtensionPrefix) + ` logs -c 10                     # Download last 10 matching runs
  ` + string(constants.CLIExtensionPrefix) + ` logs --start-date 2024-01-01   # Download all runs after date
  ` + string(constants.CLIExtensionPrefix) + ` logs --end-date 2024-01-31     # Download all runs before date
  ` + string(constants.CLIExtensionPrefix) + ` logs --start-date -1w          # Download all runs from last week
  ` + string(constants.CLIExtensionPrefix) + ` logs --start-date -1w -c 5     # Download all runs from last week, show up to 5
  ` + string(constants.CLIExtensionPrefix) + ` logs --end-date -1d            # Download all runs until yesterday
  ` + string(constants.CLIExtensionPrefix) + ` logs --start-date -1mo         # Download all runs from last month
  ` + string(constants.CLIExtensionPrefix) + ` logs --engine claude           # Filter logs by claude engine
  ` + string(constants.CLIExtensionPrefix) + ` logs --engine codex            # Filter logs by codex engine
  ` + string(constants.CLIExtensionPrefix) + ` logs --engine copilot          # Filter logs by copilot engine
  ` + string(constants.CLIExtensionPrefix) + ` logs --firewall                # Filter logs with firewall enabled
  ` + string(constants.CLIExtensionPrefix) + ` logs --no-firewall             # Filter logs without firewall
  ` + string(constants.CLIExtensionPrefix) + ` logs --safe-output missing-tool     # Filter logs with missing_tool messages
  ` + string(constants.CLIExtensionPrefix) + ` logs --safe-output missing-data     # Filter logs with missing_data messages
  ` + string(constants.CLIExtensionPrefix) + ` logs --safe-output create-issue     # Filter logs with create_issue messages
  ` + string(constants.CLIExtensionPrefix) + ` logs -o ./my-logs              # Custom output directory
  ` + string(constants.CLIExtensionPrefix) + ` logs --ref main                # Filter logs by branch or tag
  ` + string(constants.CLIExtensionPrefix) + ` logs --ref feature-xyz         # Filter logs by feature branch
  ` + string(constants.CLIExtensionPrefix) + ` logs --after-run-id 1000       # Filter runs after run ID 1000
  ` + string(constants.CLIExtensionPrefix) + ` logs --before-run-id 2000      # Filter runs before run ID 2000
  ` + string(constants.CLIExtensionPrefix) + ` logs --after-run-id 1000 --before-run-id 2000  # Filter runs in range
  ` + string(constants.CLIExtensionPrefix) + ` logs --tool-graph              # Generate Mermaid tool sequence graph
  ` + string(constants.CLIExtensionPrefix) + ` logs --parse                   # Parse logs and generate Markdown reports
  ` + string(constants.CLIExtensionPrefix) + ` logs --json                    # Output metrics in JSON format
  ` + string(constants.CLIExtensionPrefix) + ` logs --parse --json            # Generate both Markdown and JSON
  ` + string(constants.CLIExtensionPrefix) + ` logs weekly-research --repo owner/repo  # Download logs from specific repository`,
		RunE: func(cmd *cobra.Command, args []string) error {
			logsCommandLog.Printf("Starting logs command: args=%d", len(args))

			var workflowName string
			if len(args) > 0 && args[0] != "" {
				logsCommandLog.Printf("Resolving workflow name from argument: %s", args[0])

				// Convert workflow ID to GitHub Actions workflow name
				// First try to resolve as a workflow ID
				resolvedName, err := workflow.ResolveWorkflowName(args[0])
				if err != nil {
					// If that fails, check if it's already a GitHub Actions workflow name
					// by checking if any .lock.yml files have this as their name
					agenticWorkflowNames, nameErr := getAgenticWorkflowNames(false)
					if nameErr == nil && sliceutil.Contains(agenticWorkflowNames, args[0]) {
						// It's already a valid GitHub Actions workflow name
						workflowName = args[0]
					} else {
						// Neither workflow ID nor valid GitHub Actions workflow name
						suggestions := []string{
							fmt.Sprintf("Run '%s status' to see all available workflows", string(constants.CLIExtensionPrefix)),
							"Check for typos in the workflow name",
							"Use the workflow ID (e.g., 'test-claude') or GitHub Actions workflow name (e.g., 'Test Claude')",
						}

						// Add fuzzy match suggestions
						similarNames := suggestWorkflowNames(args[0])
						if len(similarNames) > 0 {
							suggestions = append([]string{fmt.Sprintf("Did you mean: %s?", strings.Join(similarNames, ", "))}, suggestions...)
						}

						return errors.New(console.FormatErrorWithSuggestions(
							fmt.Sprintf("workflow '%s' not found", args[0]),
							suggestions,
						))
					}
				} else {
					workflowName = resolvedName
				}
			}

			count, _ := cmd.Flags().GetInt("count")
			startDate, _ := cmd.Flags().GetString("start-date")
			endDate, _ := cmd.Flags().GetString("end-date")
			outputDir, _ := cmd.Flags().GetString("output")
			engine, _ := cmd.Flags().GetString("engine")
			ref, _ := cmd.Flags().GetString("ref")
			beforeRunID, _ := cmd.Flags().GetInt64("before-run-id")
			afterRunID, _ := cmd.Flags().GetInt64("after-run-id")
			verbose, _ := cmd.Flags().GetBool("verbose")
			toolGraph, _ := cmd.Flags().GetBool("tool-graph")
			noStaged, _ := cmd.Flags().GetBool("no-staged")
			firewallOnly, _ := cmd.Flags().GetBool("firewall")
			noFirewall, _ := cmd.Flags().GetBool("no-firewall")
			parse, _ := cmd.Flags().GetBool("parse")
			jsonOutput, _ := cmd.Flags().GetBool("json")
			timeout, _ := cmd.Flags().GetInt("timeout")
			repoOverride, _ := cmd.Flags().GetString("repo")
			summaryFile, _ := cmd.Flags().GetString("summary-file")
			safeOutputType, _ := cmd.Flags().GetString("safe-output")

			// Resolve relative dates to absolute dates for GitHub CLI
			now := time.Now()
			if startDate != "" {
				logsCommandLog.Printf("Resolving start date: %s", startDate)
				resolvedStartDate, err := workflow.ResolveRelativeDate(startDate, now)
				if err != nil {
					return fmt.Errorf("invalid start-date format '%s': %v", startDate, err)
				}
				startDate = resolvedStartDate
				logsCommandLog.Printf("Resolved start date to: %s", startDate)
			}
			if endDate != "" {
				logsCommandLog.Printf("Resolving end date: %s", endDate)
				resolvedEndDate, err := workflow.ResolveRelativeDate(endDate, now)
				if err != nil {
					return fmt.Errorf("invalid end-date format '%s': %v", endDate, err)
				}
				endDate = resolvedEndDate
				logsCommandLog.Printf("Resolved end date to: %s", endDate)
			}

			// Validate engine parameter using the engine registry
			if engine != "" {
				logsCommandLog.Printf("Validating engine parameter: %s", engine)
				registry := workflow.GetGlobalEngineRegistry()
				if !registry.IsValidEngine(engine) {
					supportedEngines := registry.GetSupportedEngines()
					return fmt.Errorf("invalid engine value '%s'. Must be one of: %s", engine, strings.Join(supportedEngines, ", "))
				}
			}

			logsCommandLog.Printf("Executing logs download: workflow=%s, count=%d, engine=%s", workflowName, count, engine)

			return DownloadWorkflowLogs(cmd.Context(), workflowName, count, startDate, endDate, outputDir, engine, ref, beforeRunID, afterRunID, repoOverride, verbose, toolGraph, noStaged, firewallOnly, noFirewall, parse, jsonOutput, timeout, summaryFile, safeOutputType)
		},
	}

	// Add flags to logs command
	logsCmd.Flags().IntP("count", "c", 10, "Maximum number of matching workflow runs to return (after applying filters)")
	logsCmd.Flags().String("start-date", "", "Filter runs created after this date (YYYY-MM-DD or delta like -1d, -1w, -1mo)")
	logsCmd.Flags().String("end-date", "", "Filter runs created before this date (YYYY-MM-DD or delta like -1d, -1w, -1mo)")
	addOutputFlag(logsCmd, defaultLogsOutputDir)
	addEngineFilterFlag(logsCmd)
	logsCmd.Flags().String("ref", "", "Filter runs by branch or tag name (e.g., main, v1.0.0)")
	logsCmd.Flags().Int64("before-run-id", 0, "Filter runs with database ID before this value (exclusive)")
	logsCmd.Flags().Int64("after-run-id", 0, "Filter runs with database ID after this value (exclusive)")
	addRepoFlag(logsCmd)
	logsCmd.Flags().Bool("tool-graph", false, "Generate Mermaid tool sequence graph from agent logs")
	logsCmd.Flags().Bool("no-staged", false, "Filter out staged workflow runs (exclude runs with staged: true in aw_info.json)")
	logsCmd.Flags().Bool("firewall", false, "Filter to only runs with firewall enabled")
	logsCmd.Flags().Bool("no-firewall", false, "Filter to only runs without firewall enabled")
	logsCmd.Flags().String("safe-output", "", "Filter to runs containing a specific safe output type (e.g., create-issue, missing-tool, missing-data)")
	logsCmd.Flags().Bool("parse", false, "Run JavaScript parsers on agent logs and firewall logs, writing Markdown to log.md and firewall.md")
	addJSONFlag(logsCmd)
	logsCmd.Flags().Int("timeout", 0, "Download timeout in seconds (0 = no timeout)")
	logsCmd.Flags().String("summary-file", "summary.json", "Path to write the summary JSON file relative to output directory (use empty string to disable)")
	logsCmd.MarkFlagsMutuallyExclusive("firewall", "no-firewall")

	// Register completions for logs command
	logsCmd.ValidArgsFunction = CompleteWorkflowNames
	RegisterEngineFlagCompletion(logsCmd)
	RegisterDirFlagCompletion(logsCmd, "output")

	return logsCmd
}

// flattenSingleFileArtifacts applies the artifact unfold rule to downloaded artifacts
// Unfold rule: If an artifact download folder contains a single file, move the file to root and delete the folder
// This simplifies artifact access by removing unnecessary nesting for single-file artifacts

// downloadWorkflowRunLogs downloads and unzips workflow run logs using GitHub API

// unzipFile extracts a zip file to a destination directory

// extractZipFile extracts a single file from a zip archive

// loadRunSummary attempts to load a run summary from disk
// Returns the summary and a boolean indicating if it was successfully loaded and is valid
// displayToolCallReport displays a table of tool usage statistics across all runs
// ExtractLogMetricsFromRun extracts log metrics from a processed run's log directory

// findAgentOutputFile searches for a file named agent_output.json within the logDir tree.
// Returns the first path found (depth-first) and a boolean indicating success.

// findAgentLogFile searches for agent logs within the logDir.
// It uses engine.GetLogFileForParsing() to determine which log file to use:
//   - If GetLogFileForParsing() returns a non-empty value that doesn't point to agent-stdio.log,
//     look for files in the "agent_output" artifact directory
//   - Otherwise, look for the "agent-stdio.log" artifact file
//
// Returns the first path found and a boolean indicating success.

// fileExists checks if a file exists

// copyFileSimple copies a file from src to dst using buffered IO.

// dirExists checks if a directory exists

// isDirEmpty checks if a directory is empty

// extractMissingToolsFromRun extracts missing tool reports from a workflow run's artifacts

// extractMCPFailuresFromRun extracts MCP server failure reports from a workflow run's logs

// extractMCPFailuresFromLogFile parses a single log file for MCP server failures

// MCPFailureSummary aggregates MCP server failures across runs
// displayMCPFailuresAnalysis displays a summary of MCP server failures across all runs
// parseAgentLog runs the JavaScript log parser on agent logs and writes markdown to log.md

// parseFirewallLogs runs the JavaScript firewall log parser and writes markdown to firewall.md
