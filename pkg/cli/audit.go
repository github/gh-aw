package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/cli/fileutil"
	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/timeutil"
	"github.com/github/gh-aw/pkg/workflow"
	"github.com/spf13/cobra"
)

var auditLog = logger.New("cli:audit")

// NewAuditCommand creates the audit command
func NewAuditCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "audit <run-id>",
		Short: "Investigate an agentic workflow run and generate a detailed report",
		Long: `Audit a single workflow run by downloading artifacts and logs, detecting errors,
analyzing MCP tool usage, and generating a concise Markdown report suitable for AI agents.

This command accepts:
- A numeric run ID (e.g., 1234567890)
- A GitHub Actions run URL (e.g., https://github.com/owner/repo/actions/runs/1234567890)
- A GitHub Actions job URL (e.g., https://github.com/owner/repo/actions/runs/1234567890/job/9876543210)
- A GitHub Actions job URL with step (e.g., https://github.com/owner/repo/actions/runs/1234567890/job/9876543210#step:7:1)
- A GitHub workflow run URL (e.g., https://github.com/owner/repo/runs/1234567890)
- GitHub Enterprise URLs (e.g., https://github.example.com/owner/repo/actions/runs/1234567890)

When a job URL is provided:
- If a step number is included (#step:7:1), extracts that specific step's output
- If no step number, finds and extracts the first failing step's output
- Saves job logs to the output directory

This command:
- Downloads artifacts and logs for the specified run ID
- Detects errors and warnings in the logs
- Analyzes MCP tool usage statistics
- Extracts missing tool reports
- Generates a concise Markdown report

Examples:
  ` + string(constants.CLIExtensionPrefix) + ` audit 1234567890     # Audit run with ID 1234567890
  ` + string(constants.CLIExtensionPrefix) + ` audit https://github.com/owner/repo/actions/runs/1234567890  # Audit from run URL
  ` + string(constants.CLIExtensionPrefix) + ` audit https://github.com/owner/repo/actions/runs/1234567890/job/9876543210  # Audit job and extract first failing step
  ` + string(constants.CLIExtensionPrefix) + ` audit https://github.com/owner/repo/actions/runs/1234567890/job/9876543210#step:7:1  # Extract step 7 output
  ` + string(constants.CLIExtensionPrefix) + ` audit https://github.com/owner/repo/runs/1234567890  # Audit from workflow run URL
  ` + string(constants.CLIExtensionPrefix) + ` audit https://github.example.com/owner/repo/actions/runs/1234567890  # Audit from GitHub Enterprise
  ` + string(constants.CLIExtensionPrefix) + ` audit 1234567890 -o ./audit-reports  # Custom output directory
  ` + string(constants.CLIExtensionPrefix) + ` audit 1234567890 -v  # Verbose output
  ` + string(constants.CLIExtensionPrefix) + ` audit 1234567890 --parse  # Parse agent logs and firewall logs, generating log.md and firewall.md`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			runIDOrURL := args[0]

			// Parse run information from input (either numeric ID or URL)
			// Use extended parsing to capture job ID and step information
			components, err := parser.ParseRunURLExtended(runIDOrURL)
			if err != nil {
				return err
			}

			outputDir, _ := cmd.Flags().GetString("output")
			verbose, _ := cmd.Flags().GetBool("verbose")
			jsonOutput, _ := cmd.Flags().GetBool("json")
			parse, _ := cmd.Flags().GetBool("parse")

			return AuditWorkflowRun(
				cmd.Context(),
				components.Number,
				components.Owner,
				components.Repo,
				components.Host,
				outputDir,
				verbose,
				parse,
				jsonOutput,
				components.JobID,
				components.StepNumber,
			)
		},
	}

	// Add flags to audit command
	addOutputFlag(cmd, defaultLogsOutputDir)
	addJSONFlag(cmd)
	cmd.Flags().Bool("parse", false, "Run JavaScript parsers on agent logs and firewall logs, writing Markdown to log.md and firewall.md")

	// Register completions for audit command
	RegisterDirFlagCompletion(cmd, "output")

	return cmd
}

// extractRunID extracts the run ID from either a numeric string or a GitHub Actions URL
func extractRunID(input string) (int64, error) {
	runID, _, _, _, err := parser.ParseRunURL(input)
	if err != nil {
		return 0, err
	}
	return runID, nil
}

// isPermissionError checks if an error is related to permissions/authentication
func isPermissionError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "authentication required") ||
		strings.Contains(errStr, "exit status 4") ||
		strings.Contains(errStr, "GitHub CLI authentication") ||
		strings.Contains(errStr, "permission") ||
		strings.Contains(errStr, "GH_TOKEN")
}

// AuditWorkflowRun audits a single workflow run and generates a report
// If jobID is provided (>0), focuses audit on that specific job
// If stepNumber is provided (>0), extracts output for that specific step
func AuditWorkflowRun(ctx context.Context, runID int64, owner, repo, hostname string, outputDir string, verbose bool, parse bool, jsonOutput bool, jobID int64, stepNumber int) error {
	auditLog.Printf("Starting audit for workflow run: runID=%d, owner=%s, repo=%s, jobID=%d, stepNumber=%d", runID, owner, repo, jobID, stepNumber)

	// Check context cancellation at the start
	select {
	case <-ctx.Done():
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Operation cancelled"))
		return ctx.Err()
	default:
	}

	if verbose {
		if jobID > 0 {
			if stepNumber > 0 {
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Auditing workflow run %d, job %d, step %d...", runID, jobID, stepNumber)))
			} else {
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Auditing workflow run %d, job %d...", runID, jobID)))
			}
		} else {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Auditing workflow run %d...", runID)))
		}
	}

	runOutputDir := filepath.Join(outputDir, fmt.Sprintf("run-%d", runID))
	auditLog.Printf("Using output directory: %s", runOutputDir)

	// If job ID is provided, handle job-specific audit
	if jobID > 0 {
		return auditJobRun(runID, jobID, stepNumber, owner, repo, hostname, runOutputDir, verbose, jsonOutput)
	}

	// Check if we have locally cached artifacts first
	hasLocalCache := fileutil.DirExists(runOutputDir) && !fileutil.IsDirEmpty(runOutputDir)

	// Try to get run metadata from GitHub API
	run, metadataErr := fetchWorkflowRunMetadata(runID, owner, repo, hostname, verbose)
	var useLocalCache bool

	if metadataErr != nil {
		// Check if it's a permission error
		if isPermissionError(metadataErr) {
			if hasLocalCache {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage("GitHub API access denied, but found locally cached artifacts. Processing cached data..."))
				useLocalCache = true
			} else {
				// Provide helpful message about using GitHub MCP server
				return fmt.Errorf("GitHub API access denied and no local cache found.\n\n"+
					"To download artifacts, use the GitHub MCP server:\n\n"+
					"1. Use the github-mcp-server tool 'download_workflow_run_artifacts' with:\n"+
					"   - run_id: %d\n"+
					"   - output_directory: %s\n\n"+
					"2. After downloading, run this audit command again to analyze the cached artifacts.\n\n"+
					"Original error: %v", runID, runOutputDir, metadataErr)
			}
		} else {
			return fmt.Errorf("failed to fetch run metadata: %w", metadataErr)
		}
	}

	if !useLocalCache {
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Run: %s (Status: %s, Conclusion: %s)", run.WorkflowName, run.Status, run.Conclusion)))
		}

		// Download artifacts for the run
		auditLog.Printf("Downloading artifacts for run %d", runID)
		err := downloadRunArtifacts(runID, runOutputDir, verbose)
		if err != nil {
			// Gracefully handle cases where the run legitimately has no artifacts
			if errors.Is(err, ErrNoArtifacts) {
				auditLog.Printf("No artifacts found for run %d", runID)
				if verbose {
					fmt.Fprintln(os.Stderr, console.FormatWarningMessage("No artifacts attached to this run. Proceeding with metadata-only audit."))
				}
			} else if isPermissionError(err) {
				if hasLocalCache {
					fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Artifact download failed due to permissions, but found locally cached artifacts. Processing cached data..."))
					useLocalCache = true
				} else {
					return fmt.Errorf("failed to download artifacts due to permissions and no local cache found.\n\n"+
						"To download artifacts, use the GitHub MCP server:\n\n"+
						"1. Use the github-mcp-server tool 'download_workflow_run_artifacts' with:\n"+
						"   - run_id: %d\n"+
						"   - output_directory: %s\n\n"+
						"2. After downloading, run this audit command again to analyze the cached artifacts.\n\n"+
						"Original error: %v", runID, runOutputDir, err)
				}
			} else {
				return fmt.Errorf("failed to download artifacts: %w", err)
			}
		}
	}

	// If using local cache without metadata, create a minimal run structure
	if useLocalCache && run.DatabaseID == 0 {
		run = WorkflowRun{
			DatabaseID:   runID,
			WorkflowName: fmt.Sprintf("Workflow Run %d", runID),
			Status:       "unknown",
			LogsPath:     runOutputDir,
		}
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Using locally cached artifacts without metadata. Some report details may be unavailable."))
	}

	// Extract metrics from logs
	metrics, err := extractLogMetrics(runOutputDir, verbose, run.WorkflowPath)
	if err != nil {
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to extract metrics: %v", err)))
		}
		metrics = LogMetrics{}
	}

	// Update run with metrics
	run.TokenUsage = metrics.TokenUsage
	run.EstimatedCost = metrics.EstimatedCost
	run.Turns = metrics.Turns
	run.ErrorCount = 0
	run.WarningCount = 0
	run.LogsPath = runOutputDir

	// Calculate duration
	if !run.StartedAt.IsZero() && !run.UpdatedAt.IsZero() {
		run.Duration = run.UpdatedAt.Sub(run.StartedAt)
	}

	// Add failed jobs to error count
	if failedJobCount, err := fetchJobStatuses(run.DatabaseID, verbose); err == nil {
		run.ErrorCount += failedJobCount
		if verbose && failedJobCount > 0 {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Added %d failed jobs to error count", failedJobCount)))
		}
	}

	// Fetch detailed job information including durations
	jobDetails, err := fetchJobDetails(run.DatabaseID, verbose)
	if err != nil && verbose {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to fetch job details: %v", err)))
	}

	// Extract missing tools
	missingTools, err := extractMissingToolsFromRun(runOutputDir, run, verbose)
	if err != nil && verbose {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to extract missing tools: %v", err)))
	}

	// Extract missing data
	missingData, err := extractMissingDataFromRun(runOutputDir, run, verbose)
	if err != nil && verbose {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to extract missing data: %v", err)))
	}

	// Extract noops
	noops, noopErr := extractNoopsFromRun(runOutputDir, run, verbose)
	if noopErr != nil && verbose {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to extract noops: %v", noopErr)))
	}

	// Extract MCP failures
	mcpFailures, err := extractMCPFailuresFromRun(runOutputDir, run, verbose)
	if err != nil && verbose {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to extract MCP failures: %v", err)))
	}

	// Analyze access logs if available
	accessAnalysis, err := analyzeAccessLogs(runOutputDir, verbose)
	if err != nil && verbose {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to analyze access logs: %v", err)))
	}

	// Analyze firewall logs if available
	firewallAnalysis, err := analyzeFirewallLogs(runOutputDir, verbose)
	if err != nil && verbose {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to analyze firewall logs: %v", err)))
	}

	// Analyze redacted domains if available
	redactedDomainsAnalysis, err := analyzeRedactedDomains(runOutputDir, verbose)
	if err != nil && verbose {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to analyze redacted domains: %v", err)))
	}

	// Extract MCP tool usage data from gateway logs
	mcpToolUsage, err := extractMCPToolUsageData(runOutputDir, verbose)
	if err != nil && verbose {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to extract MCP tool usage: %v", err)))
	}

	// List all artifacts
	artifacts, err := listArtifacts(runOutputDir)
	if err != nil && verbose {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to list artifacts: %v", err)))
	}

	// Create processed run for report generation
	processedRun := ProcessedRun{
		Run:                     run,
		FirewallAnalysis:        firewallAnalysis,
		RedactedDomainsAnalysis: redactedDomainsAnalysis,
		MissingTools:            missingTools,
		MissingData:             missingData,
		Noops:                   noops,
		MCPFailures:             mcpFailures,
		JobDetails:              jobDetails,
	}

	// Build structured audit data
	auditData := buildAuditData(processedRun, metrics, mcpToolUsage)

	// Render output based on format preference
	if jsonOutput {
		if err := renderJSON(auditData); err != nil {
			return fmt.Errorf("failed to render JSON output: %w", err)
		}
	} else {
		renderConsole(auditData, runOutputDir)
	}

	// Display gateway metrics if available
	if gatewayMetrics, err := parseGatewayLogs(runOutputDir, verbose); err == nil {
		if metricsOutput := renderGatewayMetricsTable(gatewayMetrics, verbose); metricsOutput != "" {
			fmt.Fprint(os.Stderr, metricsOutput)
		}
	}

	// Conditionally attempt to render agentic log (similar to `logs --parse`) if --parse flag is set
	// This creates a log.md file in the run directory for a rich, human-readable agent session summary.
	// We intentionally do not fail the audit on parse errors; they are reported as warnings.
	if parse {
		awInfoPath := filepath.Join(runOutputDir, "aw_info.json")
		if engine := extractEngineFromAwInfo(awInfoPath, verbose); engine != nil { // reuse existing helper in same package
			if err := parseAgentLog(runOutputDir, engine, verbose); err != nil {
				if verbose {
					fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to parse agent log for run %d: %v", runID, err)))
				}
			} else {
				// Always show success message for parsing, not just in verbose mode
				logMdPath := filepath.Join(runOutputDir, "log.md")
				if _, err := os.Stat(logMdPath); err == nil {
					fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("✓ Parsed log for run %d → %s", runID, logMdPath)))
				}
			}
		} else if verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("No engine detected (aw_info.json missing or invalid); skipping agent log rendering"))
		}

		// Also parse firewall logs if they exist
		if err := parseFirewallLogs(runOutputDir, verbose); err != nil {
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to parse firewall logs for run %d: %v", runID, err)))
			}
		} else {
			// Show success message if firewall.md was created
			firewallMdPath := filepath.Join(runOutputDir, "firewall.md")
			if _, err := os.Stat(firewallMdPath); err == nil {
				fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("✓ Parsed firewall logs for run %d → %s", runID, firewallMdPath)))
			}
		}
	}

	// Save run summary for caching future audit runs
	summary := &RunSummary{
		CLIVersion:              GetVersion(),
		RunID:                   run.DatabaseID,
		ProcessedAt:             time.Now(),
		Run:                     run,
		Metrics:                 metrics,
		AccessAnalysis:          accessAnalysis,
		FirewallAnalysis:        firewallAnalysis,
		RedactedDomainsAnalysis: redactedDomainsAnalysis,
		MissingTools:            missingTools,
		MissingData:             missingData,
		Noops:                   noops,
		MCPFailures:             mcpFailures,
		ArtifactsList:           artifacts,
		JobDetails:              jobDetails,
	}

	if err := saveRunSummary(runOutputDir, summary, verbose); err != nil {
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to save run summary: %v", err)))
		}
	}

	// Display logs location (only for console output)
	if !jsonOutput {
		absOutputDir, _ := filepath.Abs(runOutputDir)
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("Audit complete. Logs saved to %s", absOutputDir)))
	}

	return nil
}

// auditJobRun performs a targeted audit of a specific job within a workflow run
// If stepNumber > 0, focuses on extracting output for that specific step
func auditJobRun(runID int64, jobID int64, stepNumber int, owner, repo, hostname string, outputDir string, verbose bool, jsonOutput bool) error {
	auditLog.Printf("Starting job-specific audit: runID=%d, jobID=%d, stepNumber=%d", runID, jobID, stepNumber)

	// Create output directory for job-specific artifacts
	if err := os.MkdirAll(outputDir, 0750); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Fetch job logs using gh CLI
	args := []string{"run", "view"}

	// Add hostname flag if specified (for GitHub Enterprise)
	if hostname != "" && hostname != "github.com" {
		args = append(args, "--hostname", hostname)
	}

	// Add repository flag if specified
	if owner != "" && repo != "" {
		args = append(args, "-R", fmt.Sprintf("%s/%s", owner, repo))
	}

	args = append(args, "--job", fmt.Sprintf("%d", jobID), "--log")

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Fetching logs for job %d...", jobID)))
		fmt.Fprintln(os.Stderr, console.FormatVerboseMessage(fmt.Sprintf("Executing: gh %s", strings.Join(args, " "))))
	}

	output, err := workflow.RunGHCombined("Fetching job logs...", args...)
	if err != nil {
		return fmt.Errorf("failed to fetch job logs: %w\nOutput: %s", err, string(output))
	}

	jobLogContent := string(output)

	// Save full job log
	jobLogPath := filepath.Join(outputDir, fmt.Sprintf("job-%d.log", jobID))
	if err := os.WriteFile(jobLogPath, []byte(jobLogContent), 0600); err != nil {
		return fmt.Errorf("failed to write job log: %w", err)
	}

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("Job log saved to %s", jobLogPath)))
	}

	// If step number is specified, extract that step's output
	if stepNumber > 0 {
		stepOutput, err := extractStepOutput(jobLogContent, stepNumber)
		if err != nil {
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Could not extract step %d output: %v", stepNumber, err)))
			}
		} else {
			stepLogPath := filepath.Join(outputDir, fmt.Sprintf("job-%d-step-%d.log", jobID, stepNumber))
			if err := os.WriteFile(stepLogPath, []byte(stepOutput), 0600); err != nil {
				return fmt.Errorf("failed to write step log: %w", err)
			}
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("Step %d output saved to %s", stepNumber, stepLogPath)))
			}
		}
	} else {
		// No step specified, find and extract first failing step
		failingStepNum, failingStepOutput := findFirstFailingStep(jobLogContent)
		if failingStepNum > 0 {
			stepLogPath := filepath.Join(outputDir, fmt.Sprintf("job-%d-step-%d-failed.log", jobID, failingStepNum))
			if err := os.WriteFile(stepLogPath, []byte(failingStepOutput), 0600); err != nil {
				return fmt.Errorf("failed to write failing step log: %w", err)
			}
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("First failing step %d output saved to %s", failingStepNum, stepLogPath)))
			}
		} else if verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("No failing steps found in job"))
		}
	}

	// Display summary
	if !jsonOutput {
		absOutputDir, _ := filepath.Abs(outputDir)
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("Job audit complete. Logs saved to %s", absOutputDir)))

		// Display file locations
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("\nDownloaded files:"))
		fmt.Fprintf(os.Stderr, "  - %s (full job log)\n", jobLogPath)

		if stepNumber > 0 {
			stepLogPath := filepath.Join(outputDir, fmt.Sprintf("job-%d-step-%d.log", jobID, stepNumber))
			if _, err := os.Stat(stepLogPath); err == nil {
				fmt.Fprintf(os.Stderr, "  - %s (step %d output)\n", stepLogPath, stepNumber)
			}
		} else {
			failingStepPath := filepath.Join(outputDir, fmt.Sprintf("job-%d-step-*-failed.log", jobID))
			matches, _ := filepath.Glob(failingStepPath)
			for _, match := range matches {
				fmt.Fprintf(os.Stderr, "  - %s (first failing step)\n", match)
			}
		}
	}

	return nil
}

// extractStepOutput extracts the output of a specific step from job logs
func extractStepOutput(jobLog string, stepNumber int) (string, error) {
	lines := strings.Split(jobLog, "\n")
	var stepOutput []string
	inStep := false
	stepPattern := "##[group]Run " // GitHub Actions step marker
	stepEndPattern := "##[endgroup]"
	currentStep := 0

	for _, line := range lines {
		// Detect step boundaries
		if strings.Contains(line, stepPattern) || strings.HasPrefix(line, fmt.Sprintf("##[group]Step %d:", stepNumber)) {
			currentStep++
			if currentStep == stepNumber {
				inStep = true
			}
		} else if strings.Contains(line, stepEndPattern) {
			if inStep {
				break // End of target step
			}
		}

		if inStep {
			stepOutput = append(stepOutput, line)
		}
	}

	if len(stepOutput) == 0 {
		return "", fmt.Errorf("step %d not found in job logs", stepNumber)
	}

	return strings.Join(stepOutput, "\n"), nil
}

// findFirstFailingStep finds the first step that failed in the job logs
func findFirstFailingStep(jobLog string) (int, string) {
	lines := strings.Split(jobLog, "\n")
	var stepOutput []string
	inStep := false
	currentStep := 0
	foundFailure := false

	for _, line := range lines {
		// Detect step start
		if strings.Contains(line, "##[group]") {
			if inStep && foundFailure {
				break // We found a complete failing step
			}
			inStep = true
			currentStep++
			stepOutput = []string{line}
			foundFailure = false
		} else if inStep {
			stepOutput = append(stepOutput, line)

			// Detect failure indicators
			if strings.Contains(line, "##[error]") ||
				strings.Contains(line, "Error:") ||
				strings.Contains(line, "FAILED") ||
				strings.Contains(line, "exit code") && !strings.Contains(line, "exit code 0") {
				foundFailure = true
			}
		}
	}

	if foundFailure && len(stepOutput) > 0 {
		return currentStep, strings.Join(stepOutput, "\n")
	}

	return 0, ""
}

// fetchWorkflowRunMetadata fetches metadata for a single workflow run
func fetchWorkflowRunMetadata(runID int64, owner, repo, hostname string, verbose bool) (WorkflowRun, error) {
	// Build the API endpoint
	var endpoint string
	if owner != "" && repo != "" {
		// Use explicit owner/repo from the URL
		endpoint = fmt.Sprintf("repos/%s/%s/actions/runs/%d", owner, repo, runID)
	} else {
		// Fall back to {owner}/{repo} placeholders for context-based resolution
		endpoint = fmt.Sprintf("repos/{owner}/{repo}/actions/runs/%d", runID)
	}

	args := []string{"api"}

	// Add hostname flag if specified (for GitHub Enterprise)
	if hostname != "" && hostname != "github.com" {
		args = append(args, "--hostname", hostname)
	}

	args = append(args,
		endpoint,
		"--jq",
		"{databaseId: .id, number: .run_number, url: .html_url, status: .status, conclusion: .conclusion, workflowName: .name, workflowPath: .path, createdAt: .created_at, startedAt: .run_started_at, updatedAt: .updated_at, event: .event, headBranch: .head_branch, headSha: .head_sha, displayTitle: .display_title}",
	)

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Executing: gh %s", strings.Join(args, " "))))
	}

	output, err := workflow.RunGHCombined("Fetching run metadata...", args...)
	if err != nil {
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatVerboseMessage(string(output)))
		}
		return WorkflowRun{}, fmt.Errorf("failed to fetch run metadata: %w", err)
	}

	var run WorkflowRun
	if err := json.Unmarshal(output, &run); err != nil {
		return WorkflowRun{}, fmt.Errorf("failed to parse run metadata: %w", err)
	}

	return run, nil
}

// generateAuditReport generates a concise markdown report for AI agent consumption
func generateAuditReport(processedRun ProcessedRun, metrics LogMetrics, downloadedFiles []FileInfo) string {
	run := processedRun.Run
	var report strings.Builder

	report.WriteString("# Workflow Run Audit Report\n\n")

	// Basic information
	report.WriteString("## Overview\n\n")
	fmt.Fprintf(&report, "- **Run ID**: %d\n", run.DatabaseID)
	fmt.Fprintf(&report, "- **Workflow**: %s\n", run.WorkflowName)
	fmt.Fprintf(&report, "- **Status**: %s", run.Status)
	if run.Conclusion != "" && run.Status == "completed" {
		fmt.Fprintf(&report, " (%s)", run.Conclusion)
	}
	report.WriteString("\n")
	fmt.Fprintf(&report, "- **Created**: %s\n", run.CreatedAt.Format(time.RFC3339))
	if !run.StartedAt.IsZero() {
		fmt.Fprintf(&report, "- **Started**: %s\n", run.StartedAt.Format(time.RFC3339))
	}
	if !run.UpdatedAt.IsZero() {
		fmt.Fprintf(&report, "- **Updated**: %s\n", run.UpdatedAt.Format(time.RFC3339))
	}
	if run.Duration > 0 {
		fmt.Fprintf(&report, "- **Duration**: %s\n", timeutil.FormatDuration(run.Duration))
	}
	fmt.Fprintf(&report, "- **Event**: %s\n", run.Event)
	fmt.Fprintf(&report, "- **Branch**: %s\n", run.HeadBranch)
	fmt.Fprintf(&report, "- **URL**: %s\n", run.URL)
	report.WriteString("\n")

	// Metrics
	report.WriteString("## Metrics\n\n")
	if run.TokenUsage > 0 {
		fmt.Fprintf(&report, "- **Token Usage**: %s\n", console.FormatNumber(run.TokenUsage))
	}
	if run.EstimatedCost > 0 {
		fmt.Fprintf(&report, "- **Estimated Cost**: $%.3f\n", run.EstimatedCost)
	}
	if run.Turns > 0 {
		fmt.Fprintf(&report, "- **Turns**: %d\n", run.Turns)
	}
	fmt.Fprintf(&report, "- **Errors**: %d\n", run.ErrorCount)
	fmt.Fprintf(&report, "- **Warnings**: %d\n", run.WarningCount)
	report.WriteString("\n")

	// MCP Tool Usage
	if len(metrics.ToolCalls) > 0 {
		report.WriteString("## MCP Tool Usage\n\n")

		// Aggregate tool statistics
		toolStats := make(map[string]*workflow.ToolCallInfo)
		for _, toolCall := range metrics.ToolCalls {
			displayKey := workflow.PrettifyToolName(toolCall.Name)
			if existing, exists := toolStats[displayKey]; exists {
				existing.CallCount += toolCall.CallCount
				if toolCall.MaxInputSize > existing.MaxInputSize {
					existing.MaxInputSize = toolCall.MaxInputSize
				}
				if toolCall.MaxOutputSize > existing.MaxOutputSize {
					existing.MaxOutputSize = toolCall.MaxOutputSize
				}
				if toolCall.MaxDuration > existing.MaxDuration {
					existing.MaxDuration = toolCall.MaxDuration
				}
			} else {
				toolStats[displayKey] = &workflow.ToolCallInfo{
					Name:          displayKey,
					CallCount:     toolCall.CallCount,
					MaxInputSize:  toolCall.MaxInputSize,
					MaxOutputSize: toolCall.MaxOutputSize,
					MaxDuration:   toolCall.MaxDuration,
				}
			}
		}

		// Sort tools by call count
		var toolNames []string
		for name := range toolStats {
			toolNames = append(toolNames, name)
		}

		// Display top tools
		report.WriteString("| Tool | Calls | Max Input | Max Output | Max Duration |\n")
		report.WriteString("|------|-------|-----------|------------|-------------|\n")
		for _, name := range toolNames {
			tool := toolStats[name]
			inputStr := "N/A"
			if tool.MaxInputSize > 0 {
				inputStr = console.FormatNumber(tool.MaxInputSize)
			}
			outputStr := "N/A"
			if tool.MaxOutputSize > 0 {
				outputStr = console.FormatNumber(tool.MaxOutputSize)
			}
			durationStr := "N/A"
			if tool.MaxDuration > 0 {
				durationStr = timeutil.FormatDuration(tool.MaxDuration)
			}
			fmt.Fprintf(&report, "| %s | %d | %s | %s | %s |\n",
				name, tool.CallCount, inputStr, outputStr, durationStr)
		}
		report.WriteString("\n")
	}

	// MCP Failures
	if len(processedRun.MCPFailures) > 0 {
		report.WriteString("## MCP Server Failures\n\n")
		for _, failure := range processedRun.MCPFailures {
			fmt.Fprintf(&report, "- **%s**: %s\n", failure.ServerName, failure.Status)
		}
		report.WriteString("\n")
	}

	// Firewall Analysis
	if processedRun.FirewallAnalysis != nil && processedRun.FirewallAnalysis.TotalRequests > 0 {
		report.WriteString("## Firewall Analysis\n\n")
		fw := processedRun.FirewallAnalysis
		fmt.Fprintf(&report, "- **Total Requests**: %d\n", fw.TotalRequests)
		fmt.Fprintf(&report, "- **Allowed Requests**: %d\n", fw.AllowedRequests)
		fmt.Fprintf(&report, "- **Blocked Requests**: %d\n", fw.BlockedRequests)
		report.WriteString("\n")

		if len(fw.AllowedDomains) > 0 {
			report.WriteString("### Allowed Domains\n\n")
			for _, domain := range fw.AllowedDomains {
				if stats, ok := fw.RequestsByDomain[domain]; ok {
					fmt.Fprintf(&report, "- %s (%d requests)\n", domain, stats.Allowed)
				}
			}
			report.WriteString("\n")
		}

		if len(fw.BlockedDomains) > 0 {
			report.WriteString("### Blocked Domains\n\n")
			for _, domain := range fw.BlockedDomains {
				if stats, ok := fw.RequestsByDomain[domain]; ok {
					fmt.Fprintf(&report, "- %s (%d requests)\n", domain, stats.Blocked)
				}
			}
			report.WriteString("\n")
		}
	}

	// Missing Tools
	if len(processedRun.MissingTools) > 0 {
		report.WriteString("## Missing Tools\n\n")
		for _, tool := range processedRun.MissingTools {
			fmt.Fprintf(&report, "### %s\n\n", tool.Tool)
			fmt.Fprintf(&report, "- **Reason**: %s\n", tool.Reason)
			if tool.Alternatives != "" {
				fmt.Fprintf(&report, "- **Alternatives**: %s\n", tool.Alternatives)
			}
			if tool.Timestamp != "" {
				fmt.Fprintf(&report, "- **Timestamp**: %s\n", tool.Timestamp)
			}
			report.WriteString("\n")
		}
	}

	// Missing Data
	if len(processedRun.MissingData) > 0 {
		report.WriteString("## Missing Data\n\n")
		for _, data := range processedRun.MissingData {
			fmt.Fprintf(&report, "### %s\n\n", data.DataType)
			fmt.Fprintf(&report, "- **Reason**: %s\n", data.Reason)
			if data.Context != "" {
				fmt.Fprintf(&report, "- **Context**: %s\n", data.Context)
			}
			if data.Alternatives != "" {
				fmt.Fprintf(&report, "- **Alternatives**: %s\n", data.Alternatives)
			}
			if data.Timestamp != "" {
				fmt.Fprintf(&report, "- **Timestamp**: %s\n", data.Timestamp)
			}
			report.WriteString("\n")
		}
	}

	// No-Op Messages
	if len(processedRun.Noops) > 0 {
		report.WriteString("## No-Op Messages\n\n")
		for i, noop := range processedRun.Noops {
			fmt.Fprintf(&report, "### Message %d\n\n", i+1)
			fmt.Fprintf(&report, "%s\n\n", noop.Message)
			if noop.Timestamp != "" {
				fmt.Fprintf(&report, "**Timestamp**: %s\n\n", noop.Timestamp)
			}
		}
	}

	// Error Summary
	if run.ErrorCount > 0 || run.WarningCount > 0 {
		report.WriteString("## Issue Summary\n\n")
		if run.ErrorCount > 0 {
			fmt.Fprintf(&report, "This run encountered **%d error(s)**. ", run.ErrorCount)
		}
		if run.WarningCount > 0 {
			fmt.Fprintf(&report, "This run had **%d warning(s)**. ", run.WarningCount)
		}
		report.WriteString("\n\n")
	}

	// Downloaded Files Section
	report.WriteString("## Downloaded Files\n\n")
	fmt.Fprintf(&report, "Logs and artifacts are available at: `%s`\n\n", run.LogsPath)

	if len(downloadedFiles) > 0 {
		// Display all downloaded files with size and description
		for _, file := range downloadedFiles {
			formattedSize := console.FormatFileSize(file.Size)
			fmt.Fprintf(&report, "- **%s** (%s)", file.Path, formattedSize)
			// Add description if available
			if file.Description != "" {
				fmt.Fprintf(&report, " - %s", file.Description)
			}
			report.WriteString("\n")
		}
		report.WriteString("\n")
	} else {
		report.WriteString("(No artifact or log files were downloaded for this run)\n\n")
	}

	return report.String()
}
