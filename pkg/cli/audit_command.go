package cli

// audit_command.go: cobra command definition, flag parsing, and dispatch
// between single-run audit and multi-run diff mode.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/spf13/cobra"
)

var auditCommandLog = logger.New("cli:audit_command")

var auditCommandLong = `Audit one or more workflow runs by downloading artifacts and logs, detecting errors,
analyzing MCP tool usage, and generating a concise report suitable for AI agents.

When a single run is provided, generates a detailed Markdown report for that run.
When two or more runs are provided, the first is used as the base (reference) and the
remaining runs are compared against it, producing a diff report.

Each argument accepts:
- A numeric run ID (e.g., 1234567890)
- A GitHub Actions run URL (e.g., https://github.com/owner/repo/actions/runs/1234567890)
- A GitHub Actions job URL (e.g., https://github.com/owner/repo/actions/runs/1234567890/job/9876543210)
- A GitHub Actions job URL with step (e.g., https://github.com/owner/repo/actions/runs/1234567890/job/9876543210#step:7:1)
- A GitHub workflow run URL (e.g., https://github.com/owner/repo/runs/1234567890)
- GitHub Enterprise URLs (e.g., https://github.example.com/owner/repo/actions/runs/1234567890)

When a job URL is provided (single-run mode only):
- If a step number is included (#step:7:1), extracts that specific step's output
- If no step number, finds and extracts the first failing step's output
- Saves job logs to the output directory`

var auditCommandExample = `  ` + string(constants.CLIExtensionPrefix) + ` audit 1234567890 --repo owner/repo  # Audit with bare run ID (--repo required)
  ` + string(constants.CLIExtensionPrefix) + ` audit https://github.com/owner/repo/actions/runs/1234567890  # Audit from run URL
  ` + string(constants.CLIExtensionPrefix) + ` audit https://github.com/owner/repo/actions/runs/1234567890/job/9876543210  # Audit job and extract first failing step
  ` + string(constants.CLIExtensionPrefix) + ` audit https://github.com/owner/repo/actions/runs/1234567890/job/9876543210#step:7:1  # Extract step 7 output
  ` + string(constants.CLIExtensionPrefix) + ` audit https://github.com/owner/repo/runs/1234567890  # Audit from workflow run URL
  ` + string(constants.CLIExtensionPrefix) + ` audit https://github.example.com/owner/repo/actions/runs/1234567890  # Audit from GitHub Enterprise
  ` + string(constants.CLIExtensionPrefix) + ` audit 1234567890 -o ./audit-reports # Custom output directory
  ` + string(constants.CLIExtensionPrefix) + ` audit 1234567890 -v                 # Verbose output
  ` + string(constants.CLIExtensionPrefix) + ` audit 1234567890 --parse            # Parse agent logs and firewall logs, generating log.md and firewall.md
  ` + string(constants.CLIExtensionPrefix) + ` audit 1234567890 --repo owner/repo  # Audit run from a specific repository
  ` + string(constants.CLIExtensionPrefix) + ` audit 1234567890 1234567891         # Diff two runs (base vs comparison)
  ` + string(constants.CLIExtensionPrefix) + ` audit 1234567890 1234567891 1234567892  # Diff base against multiple runs
  ` + string(constants.CLIExtensionPrefix) + ` audit 1234567890 1234567891 --format markdown  # Markdown diff output for PR comments
  ` + string(constants.CLIExtensionPrefix) + ` audit 1234567890 --runtime gvisor   # Skip run unless sandbox agent runtime matches`

type auditCommandOptions struct {
	outputDir        string
	verbose          bool
	jsonOutput       bool
	parse            bool
	repoFlag         string
	format           string
	artifacts        []string
	stdin            bool
	experimentFilter string
	variantFilter    string
	runtimeFilter    string
	evalsOnly        bool
}

// NewAuditCommand creates the audit command
func NewAuditCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "audit <run-id-or-url> [run-id-or-url]...",
		Short:   "Audit workflow runs and generate detailed reports",
		Long:    auditCommandLong,
		Example: auditCommandExample,
		Args:    cobra.ArbitraryArgs,
		RunE:    runAuditCommand,
	}
	registerAuditCommandFlags(cmd)
	cmd.AddCommand(NewAuditDiffSubcommand())
	return cmd
}

func registerAuditCommandFlags(cmd *cobra.Command) {
	addOutputFlag(cmd, defaultLogsOutputDir)
	addJSONFlag(cmd)
	addRepoFlag(cmd)
	cmd.Flags().Bool("parse", false, "Run JavaScript parsers on agent logs and firewall logs, writing Markdown to log.md and firewall.md")
	cmd.Flags().String("format", "pretty", "Diff output format for multi-run mode: pretty, markdown")
	cmd.Flags().StringSlice("artifacts", nil, "Artifact sets to download (default: all — comprehensive artifacts required for analysis). Valid sets: "+strings.Join(ValidArtifactSetNames(), ", "))
	cmd.Flags().Bool("stdin", false, "Read workflow run IDs or URLs from stdin (one per line) instead of positional arguments")
	cmd.Flags().String("experiment", "", "Filter to runs that include this experiment name")
	cmd.Flags().String("variant", "", "Filter to runs with a specific variant value (requires --experiment)")
	cmd.Flags().String("runtime", "", "Filter to runs using a specific sandbox agent runtime (e.g., gvisor, docker-sbx, cloud-hypervisor)")
	cmd.Flags().Bool("evals", false, "Filter to runs containing evals results (evals.jsonl); automatically downloads the usage artifact (which includes evals) when --artifacts is narrowed")
	RegisterDirFlagCompletion(cmd, "output")
}

func runAuditCommand(cmd *cobra.Command, args []string) error {
	opts, err := getAuditCommandOptions(cmd)
	if err != nil {
		return err
	}
	args, handled, err := resolveAuditCommandArgs(args, opts.stdin)
	if err != nil || handled {
		return err
	}
	auditCommandLog.Printf("Dispatching audit command: run_count=%d, format=%s, parse=%t, evals_only=%t", len(args), opts.format, opts.parse, opts.evalsOnly)
	if len(args) == 1 {
		return runAuditSingle(cmd.Context(), args[0], opts)
	}
	if opts.evalsOnly {
		return errors.New(console.FormatErrorWithSuggestions(
			"--evals is not supported in multi-run diff mode",
			[]string{"Provide a single run ID with --evals to filter by evals results"},
		))
	}
	return runAuditMulti(cmd.Context(), args, opts.repoFlag, opts.outputDir, opts.verbose, opts.jsonOutput, opts.format, opts.artifacts)
}

func getAuditCommandOptions(cmd *cobra.Command) (auditCommandOptions, error) {
	opts := auditCommandOptions{}
	opts.outputDir, _ = cmd.Flags().GetString("output")
	opts.verbose, _ = cmd.Flags().GetBool("verbose")
	opts.jsonOutput, _ = cmd.Flags().GetBool("json")
	opts.parse, _ = cmd.Flags().GetBool("parse")
	opts.repoFlag, _ = cmd.Flags().GetString("repo")
	opts.format, _ = cmd.Flags().GetString("format")
	opts.artifacts, _ = cmd.Flags().GetStringSlice("artifacts")
	opts.stdin, _ = cmd.Flags().GetBool("stdin")
	opts.experimentFilter, _ = cmd.Flags().GetString("experiment")
	opts.variantFilter, _ = cmd.Flags().GetString("variant")
	opts.runtimeFilter, _ = cmd.Flags().GetString("runtime")
	opts.evalsOnly, _ = cmd.Flags().GetBool("evals")
	if opts.variantFilter != "" && opts.experimentFilter == "" {
		return auditCommandOptions{}, errors.New(console.FormatErrorWithSuggestions(
			"--variant requires --experiment to be specified",
			[]string{"Add --experiment <name> to filter by experiment name alongside --variant"},
		))
	}
	if err := validateLogsRuntime(opts.runtimeFilter); err != nil {
		return auditCommandOptions{}, err
	}
	// Auto-include the usage artifact (which now contains evals) when --evals is
	// specified and the user has narrowed the artifact set (non-empty --artifacts).
	// When --artifacts is empty the default is "all", which already includes usage,
	// so we must not append here: doing so would change the default from "all" to
	// "evals-only" and omit the activation/agent artifacts required for a full report.
	if len(opts.artifacts) > 0 {
		opts.artifacts = applyEvalsArtifact(opts.artifacts, opts.evalsOnly)
	}
	return opts, nil
}

func resolveAuditCommandArgs(args []string, stdin bool) ([]string, bool, error) {
	if stdin {
		if len(args) > 0 {
			return nil, false, errors.New(console.FormatErrorWithSuggestions(
				"positional arguments are not allowed with --stdin",
				[]string{"Remove the run ID arguments, or omit --stdin to use positional arguments"},
			))
		}
		stdinURLs, err := readRunIDsFromStdin(os.Stdin)
		if err != nil {
			return nil, false, fmt.Errorf("failed to read run IDs from stdin: %w", err)
		}
		if len(stdinURLs) == 0 {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage("No run IDs or URLs provided on stdin"))
			return nil, true, nil
		}
		auditCommandLog.Printf("Read %d run ID(s)/URL(s) from stdin", len(stdinURLs))
		args = stdinURLs
	}
	if len(args) == 0 {
		return nil, false, errors.New(console.FormatErrorWithSuggestions(
			"at least one run ID or URL is required",
			[]string{
				"Provide a run ID or URL as a positional argument",
				"Use --stdin to read run IDs from stdin (one per line)",
			},
		))
	}
	return args, false, nil
}

func runAuditSingle(ctx context.Context, runIDOrURL string, opts auditCommandOptions) error {
	components, err := parser.ParseRunURLExtended(runIDOrURL)
	if err != nil {
		return err
	}
	if err := applyAuditRepoFlag(opts.repoFlag, components); err != nil {
		return err
	}
	auditCommandLog.Printf("Running single-run audit: run=%d, owner=%s, repo=%s, job_id=%d", components.Number, components.Owner, components.Repo, components.JobID)
	return AuditWorkflowRun(ctx, components.Number, AuditOptions{
		Owner:            components.Owner,
		Repo:             components.Repo,
		Hostname:         components.Host,
		OutputDir:        opts.outputDir,
		Verbose:          opts.verbose,
		Parse:            opts.parse,
		JSONOutput:       opts.jsonOutput,
		JobID:            components.JobID,
		StepNumber:       components.StepNumber,
		ArtifactSets:     opts.artifacts,
		ExperimentFilter: opts.experimentFilter,
		VariantFilter:    opts.variantFilter,
		RuntimeFilter:    opts.runtimeFilter,
		EvalsOnly:        opts.evalsOnly,
	})
}

func applyAuditRepoFlag(repoFlag string, components *parser.GitHubURLComponents) error {
	if repoFlag == "" || components.Owner != "" {
		return nil
	}
	parts := strings.SplitN(repoFlag, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return fmt.Errorf("invalid repository format '%s': expected 'owner/repo'", repoFlag)
	}
	components.Owner = parts[0]
	components.Repo = parts[1]
	return nil
}

// runAuditMulti handles the multi-run diff mode for the audit command.
// The first argument is the base run; remaining arguments are comparison runs.
// Each argument may be a numeric run ID, a GitHub Actions run URL, or a job/step
// URL — job and step specificity is silently normalized to the parent run ID.
func runAuditMulti(ctx context.Context, args []string, repoFlag, outputDir string, verbose, jsonOutput bool, format string, artifacts []string) error {
	// Parse base run (job/step URLs are accepted; only the run number is used)
	baseComponents, err := parser.ParseRunURLExtended(args[0])
	if err != nil {
		return fmt.Errorf("invalid base run %q: %w", args[0], err)
	}

	// Resolve owner/repo/hostname from --repo flag or base URL
	if err := applyAuditRepoFlag(repoFlag, baseComponents); err != nil {
		return err
	}
	owner := baseComponents.Owner
	repo := baseComponents.Repo
	hostname := baseComponents.Host

	// Parse comparison run IDs (job/step URLs are accepted; only the run number is used)
	seen := make(map[int64]bool)
	compareRunIDs := make([]int64, 0, len(args)-1)
	for _, arg := range args[1:] {
		c, err := parser.ParseRunURLExtended(arg)
		if err != nil {
			return fmt.Errorf("invalid comparison run %q: %w", arg, err)
		}
		if c.Number == baseComponents.Number {
			return fmt.Errorf("comparison run ID %d is the same as the base run ID: cannot diff a run against itself", c.Number)
		}
		if seen[c.Number] {
			return fmt.Errorf("duplicate comparison run ID %d: each run ID must appear only once", c.Number)
		}
		seen[c.Number] = true
		compareRunIDs = append(compareRunIDs, c.Number)
	}

	auditCommandLog.Printf("Running multi-run diff audit: base=%d, compare_count=%d, format=%s", baseComponents.Number, len(compareRunIDs), format)
	return RunAuditDiff(ctx, baseComponents.Number, compareRunIDs, AuditOptions{
		Owner:        owner,
		Repo:         repo,
		Hostname:     hostname,
		OutputDir:    outputDir,
		Verbose:      verbose,
		JSONOutput:   jsonOutput,
		Format:       format,
		ArtifactSets: artifacts,
	})
}
