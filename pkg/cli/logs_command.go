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
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/repoutil"
	"github.com/github/gh-aw/pkg/workflow"
	"github.com/spf13/cobra"
)

var logsCommandLog = logger.New("cli:logs_command")

type logsCommandValues struct {
	targets      []logsWorkflowTarget
	targetErrors []error
	cacheBefore  string
	LogsDownloadOptions
}

const logsCommandExampleTemplate = `  # Basic usage
  %[1]s logs                           # Download logs for all workflows
  %[1]s logs weekly-research           # Download logs for a specific workflow
  %[1]s logs workflow-a workflow-b     # Download and combine reports concurrently
  %[1]s logs owner/repo/workflow-a     # Download using a full repository/workflow path
  %[1]s logs owner/repo/.github/workflows/workflow-a.yml # Full workflow file path
  %[1]s logs weekly-research.md        # Download logs (alternative format)
  %[1]s logs -c 10                     # Download last 10 matching runs

  # Date filtering
  %[1]s logs --start-date 2024-01-01   # Download up to 10 runs after date
  %[1]s logs --end-date 2024-01-31     # Download up to 10 runs before date
  %[1]s logs --start-date -1w          # Download up to 10 runs from last week
  %[1]s logs --start-date -1w -c 5     # Download up to 5 runs from last week
  %[1]s logs --end-date -1d            # Download up to 10 runs before yesterday
  %[1]s logs --start-date -1mo         # Download up to 10 runs from last month

  # Content filtering
  %[1]s logs --engine claude           # Filter logs by claude engine
  %[1]s logs --engine codex            # Filter logs by codex engine
  %[1]s logs --engine copilot          # Filter logs by copilot engine
  %[1]s logs --runtime gvisor          # Filter logs by sandbox agent runtime
  %[1]s logs --firewall                # Filter logs with firewall enabled
  %[1]s logs --no-firewall             # Filter logs without firewall
  %[1]s logs --safe-output missing-tool     # Filter logs with missing-tool messages
  %[1]s logs --safe-output missing-data     # Filter logs with missing-data messages
  %[1]s logs --safe-output create-issue     # Filter logs with create-issue messages
  %[1]s logs --safe-output noop             # Filter logs with noop messages
  %[1]s logs --safe-output report-incomplete # Filter logs with report-incomplete messages
  %[1]s logs --ref main                # Filter logs by branch or tag
  %[1]s logs --ref feature-xyz         # Filter logs by feature branch
  %[1]s logs --filtered-integrity      # Filter logs containing items that were filtered by gateway integrity checks
  %[1]s logs --evals                    # Filter logs from workflows with evals results
  %[1]s logs --graders                  # Filter logs from workflows with grader results
  %[1]s logs --exclude-staged          # Exclude staged workflow runs from results

  # Run ID range filtering
  %[1]s logs --after-run-id 1000       # Filter runs after run ID 1000
  %[1]s logs --before-run-id 2000      # Filter runs before run ID 2000
  %[1]s logs --after-run-id 1000 --before-run-id 2000  # Filter runs in range

  # Artifact selection (default: usage only - the compact conclusion artifact)
  %[1]s logs --artifacts all           # Download all artifacts (agent logs, firewall, etc.)
  %[1]s logs --artifacts agent         # Download only agent logs
  %[1]s logs --artifacts agent,firewall # Download agent and firewall artifacts
  %[1]s logs --artifacts mcp           # Download only MCP gateway logs

  # Output options (default output is compact format optimized for agents)
  %[1]s logs -o ./my-logs              # Custom output directory
  %[1]s logs --tool-graph              # Generate Mermaid tool sequence graph
  %[1]s logs --parse                   # Parse logs and generate Markdown reports
  %[1]s logs --audit                   # Generate audit.json for each downloaded run
  %[1]s logs -v                        # Verbose compact output (extra columns + sections)
  %[1]s logs --json                    # JSON format (compact by default, use -v for full)
  %[1]s logs --json -v                 # Full JSON with audit metadata
  %[1]s logs --format tsv              # Tab-separated (minimal, raw data)
  %[1]s logs --format console          # Decorated console tables (human-friendly)
  %[1]s logs --format markdown         # Cross-run security audit report (Markdown)
  %[1]s logs --format pretty           # Cross-run security audit report (console)
  %[1]s logs weekly-research --format markdown --last 10  # Cross-run report for last 10 runs
  %[1]s logs --train                   # Train log pattern weights from last 10 runs
  %[1]s logs my-workflow --train -c 50 # Train log pattern weights from up to 50 runs of a specific workflow

  # Cross-repository
  %[1]s logs weekly-research --repo owner/repo  # Download logs from specific repository

  # Resource budgets
  %[1]s logs --timeout 30 --max-github-api-rate-limit 12000 # Wait after 12000 core API requests are used
  %[1]s logs --timeout 30 --max-github-api-rate-limit -2000 # Keep 2000 core API requests available
  %[1]s logs --timeout 30 --max-storage 10240                # Prune cache data and stop downloads at 10 GB
  %[1]s logs --max-storage 10240 --prune-older-runs          # Remove oldest runs if cache pruning is insufficient

  # Cache maintenance
  %[1]s logs --cache-before -1w          # Evict local cache older than 1 week before downloading runs
  %[1]s logs --cache-before -30d         # Evict local cache older than 30 days before downloading runs
  %[1]s logs --cache-before -1mo         # Evict local cache older than 1 month before downloading runs
  %[1]s logs --cache-before 2024-01-01   # Evict local cache older than 2024-01-01 before downloading runs`

// NewLogsCommand creates the logs command
func NewLogsCommand() *cobra.Command {
	validArtifactSets := strings.Join(ValidArtifactSetNames(), ", ")
	logsCmd := &cobra.Command{
		Use:     "logs [workflow]...",
		Short:   "Download and analyze agentic workflow logs and artifacts",
		Long:    buildLogsCommandLongDescription(validArtifactSets),
		Example: buildLogsCommandExample(),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLogsCommand(cmd, args)
		},
	}
	addLogsCommandFlags(logsCmd, validArtifactSets)
	registerLogsCommandCompletions(logsCmd)
	return logsCmd
}

func buildLogsCommandLongDescription(validArtifactSets string) string {
	return fmt.Sprintf(`Download and analyze agentic workflow logs and artifacts from GitHub Actions.

This command fetches workflow runs, downloads their artifacts, and extracts them into
organized folders named by run ID. It also provides an overview table with aggregate
metrics including duration, token usage, and cost information.

Pass multiple workflow arguments to download them concurrently and produce one combined
report. Cross-repository targets accept owner/repo/workflow or
[HOST/]owner/repo/.github/workflows/workflow.yml paths. The count limit applies per workflow.

By default, only the compact usage artifact is downloaded (token usage, run metadata).
Use --artifacts all to download all artifacts, or specify individual sets such as
--artifacts agent,firewall to fetch only what you need.

All available artifact sets: %s.

Downloaded artifacts include (when using --artifacts all):
- Workflow metadata: Engine configuration and run metadata
- safe_output.jsonl: Agent's final output content (available when non-empty)
- agent_output/: Agent logs directory (if the workflow produced logs)
- agent-stdio.log: Agent standard output/error logs
- aw.patch: Git patch of changes made during execution (legacy; see aw-{branch}.patch)
- aw-{branch}.patch: Git patch of changes for each branch (one file per PR/push)
- workflow-logs/: GitHub Actions workflow run logs (job logs organized in subdirectory)
- summary.json: Complete metrics and run data for all downloaded runs
`, validArtifactSets) + "\n\n" + WorkflowIDExplanation
}

func buildLogsCommandExample() string {
	return fmt.Sprintf(logsCommandExampleTemplate, string(constants.CLIExtensionPrefix))
}

func runLogsCommand(cmd *cobra.Command, args []string) error {
	logsCommandLog.Printf("Starting logs command: args=%d", len(args))
	stdin, _ := cmd.Flags().GetBool("stdin")
	if stdin {
		return runLogsCommandFromStdin(cmd, args)
	}
	values, err := loadLogsCommandValues(cmd, args)
	if err != nil {
		return err
	}
	if len(values.targets) > 1 || len(values.targetErrors) > 0 {
		return DownloadWorkflowLogsForTargets(cmd.Context(), values.LogsDownloadOptions, values.targets, values.targetErrors)
	}
	if len(values.targets) == 1 {
		values.WorkflowName = values.targets[0].workflowName
		values.RepoOverride = values.targets[0].repoOverride
	}
	logsCommandLog.Printf("Executing logs download: workflow=%s, count=%d, engine=%s, train=%v, cache_before=%s",
		values.WorkflowName, values.Count, values.Engine, values.Train, values.cacheBefore)
	return DownloadWorkflowLogs(cmd.Context(), values.LogsDownloadOptions)
}

func runLogsCommandFromStdin(cmd *cobra.Command, args []string) error {
	if len(args) > 0 {
		return errors.New(console.FormatErrorWithSuggestions(
			"positional arguments are not allowed with --stdin",
			[]string{"Remove the workflow name argument, or omit --stdin to use the normal discovery mode"},
		))
	}
	if cmd.Flags().Changed("max-github-api-rate-limit") {
		return errors.New(console.FormatErrorWithSuggestions(
			"--max-github-api-rate-limit requires discovery mode",
			[]string{"Remove --stdin to let the logs command paginate and enforce the API usage ceiling"},
		))
	}
	logsCommandLog.Printf("Reading run IDs from stdin")
	runURLs, err := readRunIDsFromStdin(os.Stdin)
	if err != nil {
		return fmt.Errorf("failed to read run IDs from stdin: %w", err)
	}
	options, err := loadStdinLogsOptions(cmd)
	if err != nil {
		return err
	}
	options.RunURLs = runURLs
	return DownloadWorkflowLogsFromStdin(cmd.Context(), options)
}

func loadStdinLogsOptions(cmd *cobra.Command) (StdinLogsOptions, error) {
	values, err := loadCommonLogsOptions(cmd)
	if err != nil {
		return StdinLogsOptions{}, err
	}
	return StdinLogsOptions{
		OutputDir:         values.OutputDir,
		Engine:            values.Engine,
		Runtime:           values.Runtime,
		RepoOverride:      values.RepoOverride,
		Verbose:           values.Verbose,
		ToolGraph:         values.ToolGraph,
		NoStaged:          values.NoStaged,
		FirewallOnly:      values.FirewallOnly,
		NoFirewall:        values.NoFirewall,
		Parse:             values.Parse,
		JSONOutput:        values.JSONOutput,
		Timeout:           values.TimeoutMinutes,
		MaxStorageMB:      values.MaxStorageMB,
		PruneOlderRuns:    values.PruneOlderRuns,
		SummaryFile:       values.SummaryFile,
		SafeOutputType:    values.SafeOutputType,
		FilteredIntegrity: values.FilteredIntegrity,
		EvalsOnly:         values.EvalsOnly,
		GradersOnly:       values.GradersOnly,
		Audit:             values.Audit,
		Train:             values.Train,
		Format:            values.Format,
		ReportFile:        values.ReportFile,
		ArtifactSets:      values.ArtifactSets,
	}, nil
}

func loadLogsCommandValues(cmd *cobra.Command, args []string) (*logsCommandValues, error) {
	options, err := loadCommonLogsOptions(cmd)
	if err != nil {
		return nil, err
	}
	targets, targetErrors := resolveLogsWorkflowTargets(cmd, args)
	targets, remoteTargetErrors := resolveRemoteLogsWorkflowTargets(cmd.Context(), targets, options.Verbose)
	targetErrors = append(targetErrors, remoteTargetErrors...)
	if len(args) <= 1 && len(targetErrors) > 0 {
		return nil, targetErrors[0]
	}
	cacheBefore, _ := cmd.Flags().GetString("cache-before")
	if !cmd.Flags().Changed("cache-before") && cmd.Flags().Changed("after") {
		cacheBefore, _ = cmd.Flags().GetString("after")
	}
	options.After = cacheBefore
	return &logsCommandValues{
		targets:             targets,
		targetErrors:        targetErrors,
		cacheBefore:         cacheBefore,
		LogsDownloadOptions: options,
	}, nil
}

func resolveLogsWorkflowTargets(cmd *cobra.Command, args []string) ([]logsWorkflowTarget, []error) {
	if len(args) == 0 {
		return []logsWorkflowTarget{{repoOverride: getStringFlag(cmd, "repo")}}, nil
	}
	targets := make([]logsWorkflowTarget, 0, len(args))
	var targetErrors []error
	for _, arg := range args {
		target, err := resolveLogsWorkflowTarget(cmd, arg)
		if err != nil {
			targetErrors = append(targetErrors, fmt.Errorf("%s: %w", arg, err))
			continue
		}
		targets = append(targets, target)
	}
	return targets, targetErrors
}

func resolveLogsWorkflowTarget(cmd *cobra.Command, arg string) (logsWorkflowTarget, error) {
	repoOverride := getStringFlag(cmd, "repo")
	if repoOverride != "" {
		return logsWorkflowTarget{
			workflowName: resolveLogsWorkflowNameForRepo(arg, repoOverride),
			repoOverride: repoOverride,
		}, nil
	}
	if repo, workflowName, ok := splitCrossRepoWorkflowTarget(arg); ok {
		return logsWorkflowTarget{
			workflowName: normalizeWorkflowID(workflowName),
			repoOverride: repo,
		}, nil
	}
	workflowName, err := resolveLogsWorkflowNameLocally(arg)
	return logsWorkflowTarget{workflowName: workflowName}, err
}

func resolveRemoteLogsWorkflowTargets(ctx context.Context, targets []logsWorkflowTarget, verbose bool) ([]logsWorkflowTarget, []error) {
	return resolveRemoteLogsWorkflowTargetsWithFetcher(ctx, targets, verbose, fetchGitHubWorkflows)
}

func resolveRemoteLogsWorkflowTargetsWithFetcher(
	ctx context.Context,
	targets []logsWorkflowTarget,
	verbose bool,
	fetchWorkflows func(context.Context, string, bool) (map[string]*GitHubWorkflow, error),
) ([]logsWorkflowTarget, []error) {
	workflowsByRepo := make(map[string]map[string]*GitHubWorkflow)
	errorsByRepo := make(map[string]error)
	resolvedTargets := make([]logsWorkflowTarget, 0, len(targets))
	var targetErrors []error

	for i := range targets {
		target := &targets[i]
		if target.workflowName == "" || target.repoOverride == "" || repoIsLocal(target.repoOverride) {
			resolvedTargets = append(resolvedTargets, *target)
			continue
		}

		githubWorkflows, ok := workflowsByRepo[target.repoOverride]
		if !ok {
			if err := errorsByRepo[target.repoOverride]; err != nil {
				targetErrors = append(targetErrors, fmt.Errorf("%s: failed to resolve workflow name: %w", target.displayName(), err))
				continue
			}
			var err error
			githubWorkflows, err = fetchWorkflows(ctx, target.repoOverride, verbose)
			if err != nil {
				errorsByRepo[target.repoOverride] = err
				targetErrors = append(targetErrors, fmt.Errorf("%s: failed to resolve workflow name: %w", target.displayName(), err))
				continue
			}
			workflowsByRepo[target.repoOverride] = githubWorkflows
		}

		resolvedName := matchRemoteWorkflowName(target.workflowName, githubWorkflows)
		if resolvedName == "" {
			targetErrors = append(targetErrors, fmt.Errorf("%s: workflow not found", target.displayName()))
			continue
		}
		logsCommandLog.Printf("Resolved remote workflow name: %s -> %s", target.displayName(), resolvedName)
		target.workflowName = resolvedName
		resolvedTargets = append(resolvedTargets, *target)
	}

	return resolvedTargets, targetErrors
}

func splitCrossRepoWorkflowTarget(arg string) (string, string, bool) {
	if isLocalWorkflowPath(arg) || strings.HasPrefix(filepath.ToSlash(arg), constants.GithubDir) {
		return "", "", false
	}
	if _, err := os.Stat(arg); err == nil {
		return "", "", false
	}
	parts := strings.Split(filepath.ToSlash(strings.TrimSpace(arg)), "/")
	if len(parts) < 3 || parts[0] == "" || parts[1] == "" || strings.Join(parts[2:], "/") == "" {
		return "", "", false
	}
	repoParts := 2
	if len(parts) >= 4 && strings.Contains(parts[0], ".") {
		repoParts = 3
	}
	if strings.Join(parts[repoParts:], "/") == "" {
		return "", "", false
	}
	return strings.Join(parts[:repoParts], "/"), strings.Join(parts[repoParts:], "/"), true
}

func loadCommonLogsOptions(cmd *cobra.Command) (LogsDownloadOptions, error) {
	count, _ := cmd.Flags().GetInt("count")
	if last, _ := cmd.Flags().GetInt("last"); last > 0 {
		count = last
	}
	startDate, _ := cmd.Flags().GetString("start-date")
	endDate, _ := cmd.Flags().GetString("end-date")
	startDate, endDate, err := resolveLogsDateRange(startDate, endDate, time.Now())
	if err != nil {
		return LogsDownloadOptions{}, err
	}
	options := LogsDownloadOptions{
		Count:                 count,
		StartDate:             startDate,
		EndDate:               endDate,
		OutputDir:             getStringFlag(cmd, "output"),
		Engine:                getStringFlag(cmd, "engine"),
		Runtime:               getStringFlag(cmd, "runtime"),
		Ref:                   getStringFlag(cmd, "ref"),
		BeforeRunID:           getInt64Flag(cmd, "before-run-id"),
		AfterRunID:            getInt64Flag(cmd, "after-run-id"),
		RepoOverride:          getStringFlag(cmd, "repo"),
		Verbose:               getBoolFlag(cmd, "verbose"),
		ToolGraph:             getBoolFlag(cmd, "tool-graph"),
		NoStaged:              getBoolFlag(cmd, "exclude-staged"),
		FirewallOnly:          getBoolFlag(cmd, "firewall"),
		NoFirewall:            getBoolFlag(cmd, "no-firewall"),
		Parse:                 getBoolFlag(cmd, "parse"),
		JSONOutput:            getBoolFlag(cmd, "json"),
		TimeoutMinutes:        getIntFlag(cmd, "timeout"),
		TimeoutSeconds:        getIntFlag(cmd, "timeout-seconds"),
		MaxGitHubAPIRateLimit: getIntFlag(cmd, "max-github-api-rate-limit"),
		MaxStorageMB:          getIntFlag(cmd, "max-storage"),
		PruneOlderRuns:        getBoolFlag(cmd, "prune-older-runs"),
		SummaryFile:           getStringFlag(cmd, "summary-file"),
		SafeOutputType:        getStringFlag(cmd, "safe-output"),
		FilteredIntegrity:     getBoolFlag(cmd, "filtered-integrity"),
		EvalsOnly:             getBoolFlag(cmd, "evals"),
		GradersOnly:           getBoolFlag(cmd, "graders"),
		Audit:                 getBoolFlag(cmd, "audit"),
		Train:                 getBoolFlag(cmd, "train"),
		Format:                getStringFlag(cmd, "format"),
		ReportFile:            getStringFlag(cmd, "report-file"),
		ArtifactSets:          getStringSliceFlag(cmd, "artifacts"),
	}
	if err := validateLogsOptions(options); err != nil {
		return LogsDownloadOptions{}, err
	}
	if len(options.ArtifactSets) > 0 {
		options.ArtifactSets = applyEvalsArtifact(options.ArtifactSets, options.EvalsOnly)
		options.ArtifactSets = applyGradersArtifact(options.ArtifactSets, options.GradersOnly)
	}
	return options, nil
}

func resolveLogsDateRange(startDate, endDate string, now time.Time) (string, string, error) {
	resolve := func(label, value string) (string, error) {
		if value == "" {
			return "", nil
		}
		logsCommandLog.Printf("Resolving %s date: %s", label, value)
		resolved, err := workflow.ResolveRelativeDate(value, now)
		if err != nil {
			return "", fmt.Errorf("invalid %s-date format '%s': %w", label, value, err)
		}
		logsCommandLog.Printf("Resolved %s date to: %s", label, resolved)
		return resolved, nil
	}
	resolvedStart, err := resolve("start", startDate)
	if err != nil {
		return "", "", err
	}
	resolvedEnd, err := resolve("end", endDate)
	if err != nil {
		return "", "", err
	}
	return resolvedStart, resolvedEnd, nil
}

func validateLogsOptions(options LogsDownloadOptions) error {
	if err := validateMaxStorageMB(options.MaxStorageMB); err != nil {
		return err
	}
	if err := validateLogsEngine(options.Engine); err != nil {
		return err
	}
	if err := validateLogsRuntime(options.Runtime); err != nil {
		return err
	}
	return validateReportFileFlags(options.ReportFile, options.Format, options.JSONOutput)
}

func validateLogsRuntime(runtime string) error {
	if runtime == "" {
		return nil
	}
	logsCommandLog.Printf("Validating runtime parameter: %s", runtime)
	validRuntimes := []string{string(workflow.AgentRuntimeGVisor), string(workflow.AgentRuntimeDockerSbx), string(workflow.AgentRuntimeCloudHypervisor)}
	if slices.Contains(validRuntimes, runtime) {
		return nil
	}
	return fmt.Errorf("invalid runtime value '%s'. Must be one of: %s", runtime, strings.Join(validRuntimes, ", "))
}

func validateLogsEngine(engine string) error {
	if engine == "" {
		return nil
	}
	logsCommandLog.Printf("Validating engine parameter: %s", engine)
	registry := workflow.GetGlobalEngineRegistry()
	if registry.IsValidEngine(engine) {
		return nil
	}
	supportedEngines := registry.GetSupportedEngines()
	return fmt.Errorf("invalid engine value '%s'. Must be one of: %s", engine, strings.Join(supportedEngines, ", "))
}

func resolveLogsWorkflowNameForRepo(arg, repoOverride string) string {
	if !repoIsLocal(repoOverride) {
		workflowName := normalizeWorkflowID(arg)
		logsCommandLog.Printf("Using normalized workflow name for remote repo: %s", workflowName)
		return workflowName
	}
	if resolved, err := workflow.FindWorkflowName(arg); err == nil {
		logsCommandLog.Printf("Resolved workflow name via local lock files: %s -> %s", arg, resolved)
		return resolved
	}
	workflowName := normalizeWorkflowID(arg)
	logsCommandLog.Printf("Local resolution failed, using normalized workflow name: %s", workflowName)
	return workflowName
}

func resolveLogsWorkflowNameLocally(arg string) (string, error) {
	resolvedName, err := workflow.FindWorkflowName(arg)
	if err == nil {
		return resolvedName, nil
	}
	suggestions := []string{
		fmt.Sprintf("Run '%s status' to see all available workflows", string(constants.CLIExtensionPrefix)),
		"Check for typos in the workflow name",
		"Use the workflow ID (e.g., 'test-claude') or GitHub Actions workflow name (e.g., 'Test Claude')",
	}
	if similarNames := suggestWorkflowNames(arg); len(similarNames) > 0 {
		suggestions = append([]string{fmt.Sprintf("Did you mean: %s?", strings.Join(similarNames, ", "))}, suggestions...)
	}
	return "", errors.New(console.FormatErrorWithSuggestions(
		fmt.Sprintf("workflow '%s' not found", arg),
		suggestions,
	))
}

func addLogsCommandFlags(logsCmd *cobra.Command, validArtifactSets string) {
	logsCmd.Flags().IntP("count", "c", 10, "Maximum matching workflow runs to return per workflow (after applying filters)")
	logsCmd.Flags().String("start-date", "", "Filter runs created after this date (YYYY-MM-DD or delta like -1d, -1w, -1mo)")
	logsCmd.Flags().String("end-date", "", "Filter runs created before this date (YYYY-MM-DD or delta like -1d, -1w, -1mo)")
	addOutputFlag(logsCmd, defaultLogsOutputDir)
	addEngineFilterFlag(logsCmd)
	logsCmd.Flags().String("runtime", "", "Filter to runs using a specific sandbox agent runtime (e.g., gvisor, docker-sbx, cloud-hypervisor)")
	logsCmd.Flags().String("ref", "", "Filter runs by branch or tag name (e.g., main, v1.0.0)")
	logsCmd.Flags().Int64("before-run-id", 0, "Filter runs with database ID before this value (exclusive)")
	logsCmd.Flags().Int64("after-run-id", 0, "Filter runs with database ID after this value (exclusive)")
	addRepoFlag(logsCmd)
	logsCmd.Flags().Bool("tool-graph", false, "Generate Mermaid tool sequence graph from agent logs")
	logsCmd.Flags().Bool("exclude-staged", false, "Exclude workflow runs that executed in staged mode (safe outputs previewed but not applied)")
	logsCmd.Flags().Bool("firewall", false, "Filter to only runs with firewall enabled")
	logsCmd.Flags().Bool("no-firewall", false, "Filter to only runs without firewall enabled")
	logsCmd.Flags().String("safe-output", "", "Filter to runs containing a specific safe output type (e.g., create-issue, missing-tool, missing-data, noop, report-incomplete)")
	logsCmd.Flags().Bool("filtered-integrity", false, "Filter to runs containing items that were filtered by gateway integrity checks")
	logsCmd.Flags().Bool("evals", false, "Filter to runs containing evals results (evals.jsonl); automatically includes the usage artifact (which contains evals)")
	logsCmd.Flags().Bool("graders", false, "Filter to runs containing deterministic grader results; automatically includes grader artifacts")
	logsCmd.Flags().Bool("parse", false, "Run JavaScript parsers on agent logs and firewall logs, writing Markdown to log.md and firewall.md")
	logsCmd.Flags().Bool("audit", false, "Generate audit.json in each workflow run cache directory (comparisons use downloaded runs only)")
	addJSONFlag(logsCmd)
	logsCmd.Flags().Int("timeout", 0, "Download timeout in minutes (0 = no timeout)")
	logsCmd.Flags().Int("timeout-seconds", 0, "Download timeout in seconds (0 = use --timeout)")
	_ = logsCmd.Flags().MarkHidden("timeout-seconds")
	logsCmd.Flags().Int("max-github-api-rate-limit", 0, "Maximum used GitHub core API requests before waiting for reset (positive = absolute, negative = reserve from API limit; e.g. 12000 or -2000)")
	logsCmd.Flags().Int("max-storage", 0, "Maximum logs storage in MB after pruning non-essential cache data (0 = unlimited)")
	logsCmd.Flags().Bool("prune-older-runs", false, "Remove oldest completed runs when non-essential cache pruning cannot satisfy --max-storage")
	logsCmd.Flags().String("summary-file", "summary.json", "Path to write the summary JSON file relative to output directory (use empty string to disable)")
	logsCmd.Flags().Bool("train", false, "Analyze log patterns across downloaded runs and save pattern weights to drain3_weights.json in the output directory")
	logsCmd.Flags().String("format", "", "Output format: console (decorated tables), tsv (tab-separated), pretty (cross-run report), markdown (cross-run Markdown). Default: compact agent-optimized output")
	logsCmd.Flags().String("report-file", "", "Write --format markdown output directly to this file path instead of stdout (creates parent directories as needed)")
	logsCmd.Flags().Int("last", 0, "Alias for --count/-c: number of recent runs to download")
	logsCmd.Flags().StringSlice("artifacts", []string{"usage"}, "Artifact sets to download (default: usage — compact summary for faster downloads). Use 'all' for everything, or comma-separate sets. Valid sets: "+validArtifactSets)
	logsCmd.Flags().String("cache-before", "", "(Cache eviction) Evict locally cached run folders for runs before this date, prior to downloading. Accepts deltas like -1d, -1w, -1mo (or explicit day counts like -30d), or an absolute date YYYY-MM-DD. Unlike --start-date, this only clears local cache and does not filter which runs are fetched.")
	logsCmd.Flags().String("after", "", "Alias for --cache-before")
	_ = logsCmd.Flags().MarkHidden("after")
	_ = logsCmd.Flags().MarkDeprecated("after", "use --cache-before")
	logsCmd.Flags().Bool("stdin", false, "Read workflow run IDs or URLs from stdin (one per line) instead of discovering runs via the GitHub API")
	logsCmd.MarkFlagsMutuallyExclusive("firewall", "no-firewall")
}

func registerLogsCommandCompletions(logsCmd *cobra.Command) {
	logsCmd.ValidArgsFunction = CompleteWorkflowNames
	RegisterEngineFlagCompletion(logsCmd)
	RegisterDirFlagCompletion(logsCmd, "output")
}

func getStringFlag(cmd *cobra.Command, name string) string {
	value, _ := cmd.Flags().GetString(name)
	return value
}

func getStringSliceFlag(cmd *cobra.Command, name string) []string {
	value, _ := cmd.Flags().GetStringSlice(name)
	return value
}

func getBoolFlag(cmd *cobra.Command, name string) bool {
	value, _ := cmd.Flags().GetBool(name)
	return value
}

func getIntFlag(cmd *cobra.Command, name string) int {
	value, _ := cmd.Flags().GetInt(name)
	return value
}

func getInt64Flag(cmd *cobra.Command, name string) int64 {
	value, _ := cmd.Flags().GetInt64(name)
	return value
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

// repoIsLocal reports whether the given --repo flag value refers to the current local
// repository. It extracts the owner/repo portion (stripping an optional HOST/ prefix),
// then compares against the GITHUB_REPOSITORY environment variable (set by the MCP
// server container) and, if that is absent, against the repository detected from the
// local git checkout via GetCurrentRepoSlug.
//
// This is used by the logs command to decide whether local lock files are authoritative
// for resolving a workflow display name: they are authoritative only when --repo points
// to the same repository that is checked out locally.
func repoIsLocal(repo string) bool {
	// Strip optional HOST/ prefix (e.g. "github.com/owner/repo" → "owner/repo")
	ownerRepo, _ := repoutil.NormalizeRepoForAPI(repo)

	// Fast path: GITHUB_REPOSITORY is always the current repo in MCP server containers.
	if envRepo := os.Getenv("GITHUB_REPOSITORY"); envRepo != "" { //nolint:osgetenvlibrary
		return strings.EqualFold(ownerRepo, envRepo)
	}

	// Fallback: detect from git remote / gh CLI (result is cached on first call).
	currentRepo, err := GetCurrentRepoSlug()
	if err != nil {
		logsCommandLog.Printf("Could not determine current repo slug for comparison: %v", err)
		return false
	}
	return strings.EqualFold(ownerRepo, currentRepo)
}

// validateReportFileFlags returns an error if --report-file is combined with an
// incompatible flag. --report-file only takes effect for --format markdown output
// and is bypassed when --json is set.
func validateReportFileFlags(reportFile, format string, jsonOutput bool) error {
	if reportFile == "" {
		return nil
	}
	if format != "markdown" {
		return errors.New("--report-file requires --format markdown")
	}
	if jsonOutput {
		return errors.New("--report-file cannot be used with --json")
	}
	return nil
}
