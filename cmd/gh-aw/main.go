package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"

	"github.com/github/gh-aw/pkg/cli"
	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/workflow"
	"github.com/spf13/cobra"
)

// Build-time variables set by GoReleaser
var (
	version   = "dev"
	isRelease = "false" // Set to "true" during release builds
)

// Global flags
var verboseFlag bool
var bannerFlag bool

// formatListWithOr formats a list of strings with commas and "or" before the last item
// Example: ["a", "b", "c"] -> "a, b, or c"
func formatListWithOr(items []string) string {
	if len(items) == 0 {
		return ""
	}
	if len(items) == 1 {
		return items[0]
	}
	if len(items) == 2 {
		return items[0] + " or " + items[1]
	}
	// For 3+ items: "a, b, or c"
	return strings.Join(items[:len(items)-1], ", ") + ", or " + items[len(items)-1]
}

// validateEngine validates the engine flag value
func validateEngine(engine string) error {
	// Get the global engine registry
	registry := workflow.GetGlobalEngineRegistry()
	validEngines := registry.GetSupportedEngines()

	if engine != "" && !registry.IsValidEngine(engine) {
		// Sort engines for deterministic output
		sortedEngines := make([]string, len(validEngines))
		copy(sortedEngines, validEngines)
		sort.Strings(sortedEngines)

		// Format engines with quotes and "or" conjunction
		quotedEngines := make([]string, len(sortedEngines))
		for i, e := range sortedEngines {
			quotedEngines[i] = "'" + e + "'"
		}
		formattedList := formatListWithOr(quotedEngines)

		// Try to find close matches for "did you mean" suggestion
		suggestions := parser.FindClosestMatches(engine, validEngines, 1)

		errMsg := fmt.Sprintf("invalid engine value '%s'. Must be %s", engine, formattedList)

		if len(suggestions) > 0 {
			errMsg = fmt.Sprintf("invalid engine value '%s'. Must be %s.\n\nDid you mean: %s?",
				engine, formattedList, suggestions[0])
		}

		return fmt.Errorf("%s", errMsg)
	}
	return nil
}

var rootCmd = &cobra.Command{
	Use:     string(constants.CLIExtensionPrefix),
	Short:   "GitHub Agentic Workflows CLI",
	Version: version,
	Long: `GitHub Agentic Workflows CLI

Common Tasks:
  ` + string(constants.CLIExtensionPrefix) + ` init                  		# Set up a new repository
  ` + string(constants.CLIExtensionPrefix) + ` doctor --repo owner/repo 		# Run diagnostics for authentication and repository setup
  ` + string(constants.CLIExtensionPrefix) + ` add-wizard            		# Add workflows with interactive guided setup
  ` + string(constants.CLIExtensionPrefix) + ` new my-workflow       		# Create your first workflow
  ` + string(constants.CLIExtensionPrefix) + ` compile               		# Compile all workflows
  ` + string(constants.CLIExtensionPrefix) + ` run my-workflow       		# Execute a workflow
  ` + string(constants.CLIExtensionPrefix) + ` status                		# Check workflow status
  ` + string(constants.CLIExtensionPrefix) + ` logs my-workflow      		# Download and analyze execution logs
  ` + string(constants.CLIExtensionPrefix) + ` audit <run-id-or-url> 		# Audit and compare workflow runs

For detailed help on any command, use:
  ` + string(constants.CLIExtensionPrefix) + ` [command] --help`,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		cli.ConfigureProjectTimezone()
		if bannerFlag {
			console.PrintBanner()
		}
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

var newCmd = &cobra.Command{
	Use:   "new [workflow]",
	Short: "Create a new agentic workflow file with example configuration",
	Long: `Create a new agentic workflow file with example configuration and explanations of all available options.

When called without a workflow name (or with --interactive flag), launches an interactive wizard
to guide you through creating a workflow with custom settings.

When called with a workflow name, creates a template file with comprehensive examples of:
- All trigger types (on: events)
- Permissions configuration
- AI engine settings
- Tools configuration (GitHub, Claude, MCPs)
- All frontmatter options with explanations

` + cli.WorkflowIDExplanation,
	Example: `  ` + string(constants.CLIExtensionPrefix) + ` new                      # Interactive mode
  ` + string(constants.CLIExtensionPrefix) + ` new my-workflow          # Create template file
  ` + string(constants.CLIExtensionPrefix) + ` new my-workflow.md       # Same as above (.md extension stripped)
  ` + string(constants.CLIExtensionPrefix) + ` new my-workflow --force  # Overwrite if exists
  ` + string(constants.CLIExtensionPrefix) + ` new my-workflow --engine copilot  # Create template with specific engine`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		forceFlag, _ := cmd.Flags().GetBool("force")
		verbose, _ := cmd.Flags().GetBool("verbose")
		interactiveFlag, _ := cmd.Flags().GetBool("interactive")
		engineOverride, _ := cmd.Flags().GetString("engine")

		if engineOverride != "" {
			if err := validateEngine(engineOverride); err != nil {
				return err
			}
		}

		// If no arguments provided or interactive flag is set, use interactive mode
		if len(args) == 0 || interactiveFlag {
			// Check if running in CI environment
			if cli.IsRunningInCI() {
				return errors.New("interactive mode cannot be used in CI environments. Please provide a workflow name")
			}

			// Use default workflow name for interactive mode
			workflowName := "my-workflow"
			if len(args) > 0 {
				workflowName = args[0]
			}

			return cli.CreateWorkflowInteractively(cmd.Context(), workflowName, verbose, forceFlag)
		}

		// Template mode with workflow name
		workflowName := args[0]
		return cli.CreateWorkflowMarkdownFile(workflowName, verbose, forceFlag, engineOverride)
	},
}

var removeCmd = &cobra.Command{
	Use:   "remove [filter]",
	Short: "Remove agentic workflow files matching the given filter",
	Long: `Remove agentic workflow files matching the given filter.

The workflow-id is the basename of the Markdown file without the .md extension.
You can provide a substring to match multiple workflows, or a specific workflow-id.

By default, this command also removes orphaned include files that are no longer referenced
by any workflow. Use --no-remove-orphans to skip this cleanup.`,
	Example: `  ` + string(constants.CLIExtensionPrefix) + ` remove my-workflow                    # Remove specific workflow
  ` + string(constants.CLIExtensionPrefix) + ` remove test-                          # Remove all workflows containing 'test-' in name
  ` + string(constants.CLIExtensionPrefix) + ` remove old- --no-remove-orphans       # Remove workflows but keep orphaned includes
  ` + string(constants.CLIExtensionPrefix) + ` remove my-workflow --dir .github/workflows/shared  # Remove from custom directory`,
	RunE: func(cmd *cobra.Command, args []string) error {
		var pattern string
		if len(args) > 0 {
			pattern = args[0]
		}
		keepOrphans, _ := cmd.Flags().GetBool("keep-orphans")
		noRemoveOrphans, _ := cmd.Flags().GetBool("no-remove-orphans")
		keepOrphans = keepOrphans || noRemoveOrphans
		workflowDir, _ := cmd.Flags().GetString("dir")
		return cli.RemoveWorkflows(pattern, keepOrphans, workflowDir)
	},
}

var enableCmd = &cobra.Command{
	Use:   "enable [workflow]...",
	Short: "Enable agentic workflows",
	Long: `Enable one or more workflows by ID, or all workflows if no IDs are provided.

` + cli.WorkflowIDExplanation,
	Example: `  ` + string(constants.CLIExtensionPrefix) + ` enable                   # Enable all workflows
  ` + string(constants.CLIExtensionPrefix) + ` enable ci-doctor         # Enable specific workflow
  ` + string(constants.CLIExtensionPrefix) + ` enable ci-doctor.md      # Enable specific workflow (alternative format)
  ` + string(constants.CLIExtensionPrefix) + ` enable ci-doctor daily   # Enable multiple workflows
  ` + string(constants.CLIExtensionPrefix) + ` enable ci-doctor --repo owner/repo  # Enable workflow in specific repository`,
	RunE: func(cmd *cobra.Command, args []string) error {
		repoOverride, _ := cmd.Flags().GetString("repo")
		return cli.EnableWorkflowsByNames(cmd.Context(), args, repoOverride)
	},
}

var disableCmd = &cobra.Command{
	Use:   "disable [workflow]...",
	Short: "Disable agentic workflows",
	Long: `Disable one or more workflows by ID, or all workflows if no IDs are provided.

Any in-progress runs will be canceled before disabling.

` + cli.WorkflowIDExplanation,
	Example: `  ` + string(constants.CLIExtensionPrefix) + ` disable                   # Disable all workflows
  ` + string(constants.CLIExtensionPrefix) + ` disable ci-doctor         # Disable specific workflow
  ` + string(constants.CLIExtensionPrefix) + ` disable ci-doctor.md      # Disable specific workflow (alternative format)
  ` + string(constants.CLIExtensionPrefix) + ` disable ci-doctor daily   # Disable multiple workflows
  ` + string(constants.CLIExtensionPrefix) + ` disable ci-doctor --repo owner/repo  # Disable workflow in specific repository`,
	RunE: func(cmd *cobra.Command, args []string) error {
		repoOverride, _ := cmd.Flags().GetString("repo")
		return cli.DisableWorkflowsByNames(cmd.Context(), args, repoOverride)
	},
}

var compileCmd = &cobra.Command{
	Use:   "compile [workflow]...",
	Short: "Compile agentic workflow Markdown files into GitHub Actions YAML",
	Long: `Compile agentic workflow Markdown files into GitHub Actions YAML.

If no workflows are specified, all Markdown files in .github/workflows will be compiled.

` + cli.WorkflowIDExplanation + `

The --dependabot flag generates dependency manifests when dependencies are detected:
  - For npm: Creates package.json and package-lock.json (requires npm in PATH)
  - For Python: Creates requirements.txt for pip packages
  - For Go: Creates go.mod for go install/get packages
  - For all detected ecosystems: Generates .github/dependabot.yml
  - Use --force to overwrite existing dependabot.yml
  - Cannot be used with specific workflow files or custom --dir
  - Only processes workflows in the default .github/workflows directory

Action mode controls how gh-aw action scripts are referenced in compiled workflows.
Three flags govern this. --gh-aw-ref is mutually exclusive with the other two;
--action-tag and --action-mode may be combined (e.g. --action-mode action --action-tag v1.2.3):

Unlike ` + "`gh aw upgrade`" + `, ` + "`gh aw compile`" + ` only applies codemods when you opt in with ` + "`--fix`" + `.

  --action-mode <mode>
    Explicit mode selection. Values:
      dev      Local paths (./actions/...). For developing inside the gh-aw repo.
      release  SHA-pinned refs from github/gh-aw (e.g. github/gh-aw/actions/setup@<sha>).
               The SHA is derived from the binary version or from --action-tag.
      action   SHA-pinned refs from the github/gh-aw-actions repository.
               Used by release binaries. Can be combined with --action-tag to pin a version.
    Auto-detected from the binary build type when not set.

  --action-tag <sha-or-tag>
    Pin to a specific SHA or version tag (e.g. v1, v1.2.3, <full-sha>).
    Implies --action-mode release unless --action-mode action is also specified.
    The value is used as-is without SHA resolution. Use --gh-aw-ref to resolve
    branches or tags at compile time.

  --gh-aw-ref <branch-tag-or-sha>
    Resolve a branch name, tag, or SHA from github/gh-aw to its full commit SHA
    at compile time and pin the compiled workflow to that immutable SHA.
    Equivalent to --action-mode release --action-tag <resolved-sha>.
    Branch and tag names are resolved via the GitHub API.
    Cannot be combined with --action-tag or --action-mode.
    Use this when E2E-testing compiled workflows against a specific gh-aw revision.`,
	Example: `  ` + string(constants.CLIExtensionPrefix) + ` compile                    # Compile all Markdown files
  ` + string(constants.CLIExtensionPrefix) + ` compile ci-doctor          # Compile a specific workflow
  ` + string(constants.CLIExtensionPrefix) + ` compile ci-doctor daily-plan  # Compile multiple workflows
  ` + string(constants.CLIExtensionPrefix) + ` compile workflow.md        # Compile by file path
  ` + string(constants.CLIExtensionPrefix) + ` compile .github/workflows  # Compile all workflows in a directory
  ` + string(constants.CLIExtensionPrefix) + ` compile --dir custom/workflows  # Compile from custom directory
  ` + string(constants.CLIExtensionPrefix) + ` compile ci-doctor --watch     # Watch and auto-compile
  ` + string(constants.CLIExtensionPrefix) + ` compile --trial --logical-repo owner/repo  # Compile for trial mode
  ` + string(constants.CLIExtensionPrefix) + ` compile --dependabot        # Generate Dependabot manifests
  ` + string(constants.CLIExtensionPrefix) + ` compile --dependabot --force  # Force overwrite existing dependabot.yml
  ` + string(constants.CLIExtensionPrefix) + ` compile --gh-aw-ref main       # Pin workflows to the SHA of github/gh-aw main at compile time
  ` + string(constants.CLIExtensionPrefix) + ` compile --action-tag v1.2.3    # Pin workflows to a specific release tag`,
	RunE: runCompileCmd,
}

var runCmd = &cobra.Command{
	Use:   "run [workflow]...",
	Short: "Run one or more agentic workflows on GitHub Actions",
	Long: `Run one or more agentic workflows on GitHub Actions using the workflow_dispatch trigger.

When called without workflow arguments, this command enters interactive mode and shows:
- List of workflows that support workflow_dispatch
- Display of required and optional inputs
- Input collection with validation
- Command display for future reference

This command accepts one or more workflow IDs.
The workflows must have been compiled into GitHub Actions YAML files.

This command only works with workflows that have workflow_dispatch triggers.

` + cli.WorkflowIDExplanation,
	Example: `  ` + string(constants.CLIExtensionPrefix) + ` run                          # Interactive mode
  ` + string(constants.CLIExtensionPrefix) + ` run daily-perf-improver
  ` + string(constants.CLIExtensionPrefix) + ` run daily-perf-improver.md   # Alternative format
  ` + string(constants.CLIExtensionPrefix) + ` run daily-perf-improver --ref main  # Run on specific branch
  ` + string(constants.CLIExtensionPrefix) + ` run daily-perf-improver --repeat 3  # Run 4 times total (1 initial + 3 repeats)
  ` + string(constants.CLIExtensionPrefix) + ` run daily-perf-improver --enable-if-needed  # Enable if disabled, run, then restore state
  ` + string(constants.CLIExtensionPrefix) + ` run daily-perf-improver --auto-merge-prs  # Auto-merge any PRs created during execution
  ` + string(constants.CLIExtensionPrefix) + ` run daily-perf-improver --raw-field name=value --raw-field env=prod  # Pass workflow inputs
  ` + string(constants.CLIExtensionPrefix) + ` run daily-perf-improver --push  # Commit, push, and dispatch the workflow
  ` + string(constants.CLIExtensionPrefix) + ` run daily-perf-improver --dry-run  # Preview without triggering workflow runs
  ` + string(constants.CLIExtensionPrefix) + ` run daily-perf-improver --json  # Output results in JSON format`,
	Args: cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		repeatCount, _ := cmd.Flags().GetInt("repeat")
		enable, _ := cmd.Flags().GetBool("enable-if-needed")
		engineOverride, _ := cmd.Flags().GetString("engine")
		repoOverride, _ := cmd.Flags().GetString("repo")
		refOverride, _ := cmd.Flags().GetString("ref")
		autoMergePRs, _ := cmd.Flags().GetBool("auto-merge-prs")
		inputs, _ := cmd.Flags().GetStringArray("raw-field")
		push, _ := cmd.Flags().GetBool("push")
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		jsonOutput, _ := cmd.Flags().GetBool("json")
		approveRun, _ := cmd.Flags().GetBool("approve")

		if err := validateEngine(engineOverride); err != nil {
			return err
		}

		// If no arguments provided, enter interactive mode
		if len(args) == 0 {
			// Check if running in CI environment
			if cli.IsRunningInCI() {
				return errors.New("interactive mode cannot be used in CI environments. Please provide a workflow name")
			}

			// Interactive mode doesn't support repeat or enable flags
			if repeatCount > 0 {
				return errors.New("--repeat flag is not supported in interactive mode")
			}
			if enable {
				return errors.New("--enable-if-needed flag is not supported in interactive mode")
			}
			if len(inputs) > 0 {
				return errors.New("workflow inputs cannot be specified in interactive mode (they will be collected interactively)")
			}

			return cli.RunWorkflowInteractively(cmd.Context(), cli.RunWorkflowOptions{
				Verbose:        verboseFlag,
				RepoOverride:   repoOverride,
				RefOverride:    refOverride,
				AutoMergePRs:   autoMergePRs,
				Push:           push,
				EngineOverride: engineOverride,
				DryRun:         dryRun,
				Approve:        approveRun,
			})
		}

		return cli.RunWorkflowsOnGitHub(cmd.Context(), args, cli.RunOptions{
			RepeatCount:    repeatCount,
			Enable:         enable,
			EngineOverride: engineOverride,
			RepoOverride:   repoOverride,
			RefOverride:    refOverride,
			AutoMergePRs:   autoMergePRs,
			Push:           push,
			Inputs:         inputs,
			Verbose:        verboseFlag,
			DryRun:         dryRun,
			JSON:           jsonOutput,
			Approve:        approveRun,
		})
	},
}

var versionCmd = &cobra.Command{
	Use:     "version",
	Short:   "Print the current version",
	Long:    `Print the current version and build information for the gh aw CLI extension.`,
	Example: `  ` + string(constants.CLIExtensionPrefix) + ` version   # Print the current version`,
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Fprintf(os.Stderr, "%s version %s\n", string(constants.CLIExtensionPrefix), version)
		return nil
	},
}

type compileCmdOptions struct {
	engineOverride            string
	actionMode                string
	actionTag                 string
	actionsRepo               string
	ghAwRef                   string
	dir                       string
	workflowsDir              string
	logicalRepo               string
	scheduleSeed              string
	priorManifestFile         string
	validate                  bool
	watch                     bool
	noEmit                    bool
	purge                     bool
	strict                    bool
	trial                     bool
	dependabot                bool
	forceOverwrite            bool
	refreshStopTime           bool
	forceRefreshActionPins    bool
	forceRefreshContainerPins bool
	allowActionRefs           bool
	zizmor                    bool
	poutine                   bool
	actionlint                bool
	runnerGuard               bool
	syft                      bool
	grype                     bool
	grant                     bool
	yamllint                  bool
	shellcheck                bool
	jsonOutput                bool
	showAllErrors             bool
	fix                       bool
	stats                     bool
	models                    bool
	failFast                  bool
	noCheckUpdate             bool
	staged                    bool
	approve                   bool
	validateImages            bool
	ghes                      bool
	verbose                   bool
	useSamples                bool
}

func getCompileCmdOptions(cmd *cobra.Command) compileCmdOptions {
	engineOverride, _ := cmd.Flags().GetString("engine")
	actionMode, _ := cmd.Flags().GetString("action-mode")
	actionTag, _ := cmd.Flags().GetString("action-tag")
	actionsRepo, _ := cmd.Flags().GetString("actions-repo")
	ghAwRef, _ := cmd.Flags().GetString("gh-aw-ref")
	validate, _ := cmd.Flags().GetBool("validate")
	watch, _ := cmd.Flags().GetBool("watch")
	dir, _ := cmd.Flags().GetString("dir")
	workflowsDir, _ := cmd.Flags().GetString("workflows-dir")
	noEmit, _ := cmd.Flags().GetBool("no-emit")
	purge, _ := cmd.Flags().GetBool("purge")
	strict, _ := cmd.Flags().GetBool("strict")
	trial, _ := cmd.Flags().GetBool("trial")
	logicalRepo, _ := cmd.Flags().GetString("logical-repo")
	dependabot, _ := cmd.Flags().GetBool("dependabot")
	forceOverwrite, _ := cmd.Flags().GetBool("force")
	refreshStopTime, _ := cmd.Flags().GetBool("refresh-stop-time")
	forceRefreshActionPins, _ := cmd.Flags().GetBool("force-refresh-action-pins")
	forceRefreshContainerPins, _ := cmd.Flags().GetBool("force-refresh-container-pins")
	allowActionRefs, _ := cmd.Flags().GetBool("allow-action-refs")
	zizmor, _ := cmd.Flags().GetBool("zizmor")
	poutine, _ := cmd.Flags().GetBool("poutine")
	actionlint, _ := cmd.Flags().GetBool("actionlint")
	runnerGuard, _ := cmd.Flags().GetBool("runner-guard")
	syft, _ := cmd.Flags().GetBool("syft")
	grype, _ := cmd.Flags().GetBool("grype")
	grant, _ := cmd.Flags().GetBool("grant")
	yamllint, _ := cmd.Flags().GetBool("yamllint")
	shellcheck, _ := cmd.Flags().GetBool("shellcheck")
	jsonOutput, _ := cmd.Flags().GetBool("json")
	showAllErrors, _ := cmd.Flags().GetBool("show-all")
	fix, _ := cmd.Flags().GetBool("fix")
	stats, _ := cmd.Flags().GetBool("stats")
	models, _ := cmd.Flags().GetBool("models")
	failFast, _ := cmd.Flags().GetBool("fail-fast")
	noCheckUpdate, _ := cmd.Flags().GetBool("no-check-update")
	scheduleSeed, _ := cmd.Flags().GetString("schedule-seed")
	staged, _ := cmd.Flags().GetBool("staged")
	approve, _ := cmd.Flags().GetBool("approve")
	validateImages, _ := cmd.Flags().GetBool("validate-images")
	priorManifestFile, _ := cmd.Flags().GetString("prior-manifest-file")
	ghes, _ := cmd.Flags().GetBool("ghes")
	verbose, _ := cmd.Flags().GetBool("verbose")
	useSamples, _ := cmd.Flags().GetBool("use-samples")
	return compileCmdOptions{
		engineOverride: engineOverride, actionMode: actionMode, actionTag: actionTag, actionsRepo: actionsRepo, ghAwRef: ghAwRef,
		dir: dir, workflowsDir: workflowsDir, logicalRepo: logicalRepo, scheduleSeed: scheduleSeed, priorManifestFile: priorManifestFile,
		validate: validate, watch: watch, noEmit: noEmit, purge: purge, strict: strict, trial: trial, dependabot: dependabot,
		forceOverwrite: forceOverwrite, refreshStopTime: refreshStopTime, forceRefreshActionPins: forceRefreshActionPins, forceRefreshContainerPins: forceRefreshContainerPins, allowActionRefs: allowActionRefs,
		zizmor: zizmor, poutine: poutine, actionlint: actionlint, runnerGuard: runnerGuard, syft: syft, grype: grype, grant: grant, yamllint: yamllint, shellcheck: shellcheck,
		jsonOutput: jsonOutput, showAllErrors: showAllErrors, fix: fix, stats: stats, models: models, failFast: failFast, noCheckUpdate: noCheckUpdate,
		staged: staged, approve: approve, validateImages: validateImages, ghes: ghes, verbose: verbose, useSamples: useSamples,
	}
}

func (o *compileCmdOptions) resolveGhAwRef(ctx context.Context) error {
	if o.ghAwRef == "" {
		return nil
	}
	resolvedRef, resolveErr := workflow.ResolveGhAwRef(ctx, o.ghAwRef)
	if resolveErr != nil {
		return fmt.Errorf("--gh-aw-ref: %w", resolveErr)
	}
	o.actionMode = string(workflow.ActionModeRelease)
	o.actionTag = resolvedRef
	return nil
}

func (o *compileCmdOptions) workflowDir() string {
	if o.workflowsDir != "" {
		return o.workflowsDir
	}
	return o.dir
}

func (o *compileCmdOptions) toCompileConfig(args []string) cli.CompileConfig {
	return cli.CompileConfig{
		MarkdownFiles: args, Verbose: o.verbose, EngineOverride: o.engineOverride, ActionMode: o.actionMode, ActionTag: o.actionTag,
		ActionsRepo: o.actionsRepo, Validate: o.validate, Watch: o.watch, WorkflowDir: o.workflowDir(),
		NoEmit: o.noEmit, Purge: o.purge, TrialMode: o.trial, TrialLogicalRepoSlug: o.logicalRepo, Strict: o.strict,
		Dependabot: o.dependabot, ForceOverwrite: o.forceOverwrite, RefreshStopTime: o.refreshStopTime, ForceRefreshActionPins: o.forceRefreshActionPins, ForceRefreshContainerPins: o.forceRefreshContainerPins,
		AllowActionRefs: o.allowActionRefs, Zizmor: o.zizmor, Poutine: o.poutine, Actionlint: o.actionlint, RunnerGuard: o.runnerGuard,
		Syft: o.syft, Grype: o.grype, Grant: o.grant, Yamllint: o.yamllint, Shellcheck: o.shellcheck, JSONOutput: o.jsonOutput, ShowAllErrors: o.showAllErrors,
		Stats: o.stats, Models: o.models, FailFast: o.failFast, ScheduleSeed: o.scheduleSeed, Staged: o.staged, Approve: o.approve,
		ValidateImages: o.validateImages, PriorManifestFile: o.priorManifestFile, GHESCompat: o.ghes, UseSamples: o.useSamples,
	}
}

func runCompileCmd(cmd *cobra.Command, args []string) error {
	opts := getCompileCmdOptions(cmd)
	if err := opts.resolveGhAwRef(cmd.Context()); err != nil {
		return err
	}
	if err := validateEngine(opts.engineOverride); err != nil {
		return err
	}
	finishCompileUpdateCheck := cli.StartCompileUpdateCheck(cmd.Context(), opts.noCheckUpdate, opts.verbose)
	defer finishCompileUpdateCheck()
	if opts.fix {
		if err := cli.RunFix(cli.FixConfig{
			WorkflowIDs: args,
			Write:       true,
			Verbose:     opts.verbose,
			WorkflowDir: opts.dir,
		}); err != nil {
			return err
		}
	}
	config := opts.toCompileConfig(args)
	cli.PrepareCompileModelValidation(cmd.Context(), &config)
	if _, err := cli.CompileWorkflows(cmd.Context(), config); err != nil {
		return err
	}
	return nil
}

type commandSet struct {
	addCmd, addWizardCmd, editCmd, updateCmd, deployCmd, trialCmd, initCmd, statusCmd, listCmd         *cobra.Command
	mcpCmd, logsCmd, auditCmd, viewCmd, healthCmd, outcomesCmd, mcpServerCmd, prCmd, secretsCmd        *cobra.Command
	fixCmd, upgradeCmd, completionCmd, hashCmd, projectCmd, doctorCmd, checksCmd, validateCmd, lintCmd *cobra.Command
	domainsCmd, experimentsCmd, forecastCmd, gradersCmd, modelsCmd, envCmd                             *cobra.Command
}

func fixPathForCommand(s string) string {
	if s == "gh" {
		return "gh aw"
	}
	if strings.HasPrefix(s, "gh ") && !strings.HasPrefix(s, "gh aw") {
		return "gh aw " + s[3:]
	}
	return s
}

func writeUsageCommands(cmd *cobra.Command) {
	out := cmd.OutOrStderr()
	cmds := cmd.Commands()
	colWidth := 0
	for _, sub := range cmds {
		if (sub.IsAvailableCommand() || sub.Name() == "help") && len(sub.Name()) > colWidth {
			colWidth = len(sub.Name())
		}
	}
	colFmt := fmt.Sprintf("\n  %%-%ds %%s", colWidth)
	if len(cmd.Groups()) == 0 {
		fmt.Fprint(out, "\n\nAvailable Commands:")
		for _, sub := range cmds {
			if sub.IsAvailableCommand() || sub.Name() == "help" {
				fmt.Fprintf(out, colFmt, sub.Name(), sub.Short)
			}
		}
		return
	}
	for _, group := range cmd.Groups() {
		fmt.Fprintf(out, "\n\n%s", group.Title)
		for _, sub := range cmds {
			if sub.GroupID == group.ID && (sub.IsAvailableCommand() || sub.Name() == "help") {
				fmt.Fprintf(out, colFmt, sub.Name(), sub.Short)
			}
		}
	}
	if !cmd.AllChildCommandsHaveGroup() {
		fmt.Fprint(out, "\n\nAdditional Commands:")
		for _, sub := range cmds {
			if sub.GroupID == "" && (sub.IsAvailableCommand() || sub.Name() == "help") {
				fmt.Fprintf(out, colFmt, sub.Name(), sub.Short)
			}
		}
	}
}

func rootUsageFunc(cmd *cobra.Command) error {
	out := cmd.OutOrStderr()
	fmt.Fprint(out, "Usage:")
	if cmd.Runnable() {
		fmt.Fprintf(out, "\n  %s", fixPathForCommand(cmd.UseLine()))
	}
	if cmd.HasAvailableSubCommands() {
		fmt.Fprintf(out, "\n  %s [command]", fixPathForCommand(cmd.CommandPath()))
	}
	if len(cmd.Aliases) > 0 {
		fmt.Fprintf(out, "\n\nAliases:\n  %s", cmd.NameAndAliases())
	}
	if cmd.HasExample() {
		fmt.Fprintf(out, "\n\nExamples:\n%s", cmd.Example)
	}
	if cmd.HasAvailableSubCommands() {
		writeUsageCommands(cmd)
	}
	if cmd.HasAvailableLocalFlags() {
		fmt.Fprintf(out, "\n\nFlags:\n%s", strings.TrimRight(cmd.LocalFlags().FlagUsages(), " \t\n"))
	}
	if cmd.HasAvailableInheritedFlags() {
		fmt.Fprintf(out, "\n\nGlobal Flags:\n%s", strings.TrimRight(cmd.InheritedFlags().FlagUsages(), " \t\n"))
	}
	if cmd.HasAvailableSubCommands() {
		fmt.Fprintf(out, "\n\nUse \"%s [command] --help\" for more information about a command.\n", fixPathForCommand(cmd.CommandPath()))
	} else {
		fmt.Fprintln(out)
	}
	return nil
}

func customHelpRunE(c *cobra.Command, args []string) error {
	if len(args) == 1 && args[0] == "all" {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("GitHub Agentic Workflows CLI - Complete Command Reference"))
		fmt.Fprintln(os.Stderr, "")
		for _, subCmd := range rootCmd.Commands() {
			if subCmd.Hidden || subCmd.Name() == "help" {
				continue
			}
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("═══════════════════════════════════════════════════════════════"))
			fmt.Fprintf(os.Stderr, "\n%s\n\n", console.FormatInfoMessage(fmt.Sprintf("Command: %s %s", string(constants.CLIExtensionPrefix), subCmd.Name())))
			_ = subCmd.Help()
			fmt.Fprintln(os.Stderr, "")
		}
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("═══════════════════════════════════════════════════════════════"))
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("For more information, visit: https://github.github.com/gh-aw/"))
		return nil
	}
	cmd, _, e := rootCmd.Find(args)
	if cmd == nil || e != nil {
		return fmt.Errorf("unknown help topic [%#q]", args)
	}
	cmd.InitDefaultHelpFlag()
	return cmd.Help()
}

func newCustomHelpCmd() *cobra.Command {
	helpCmd := &cobra.Command{
		Use:   "help [command]",
		Short: "Help about any command",
		Long: `Help provides help for any command in the application.
Simply type ` + string(constants.CLIExtensionPrefix) + ` help [path to command] for full details.

Use "` + string(constants.CLIExtensionPrefix) + ` help all" to show help for all commands.`,
		RunE: customHelpRunE,
	}
	helpCmd.InitDefaultHelpFlag()
	if f := helpCmd.Flags().Lookup("help"); f != nil {
		f.Usage = "Show help for " + string(constants.CLIExtensionPrefix) + " help"
	}
	return helpCmd
}

func configureRootCommand() {
	rootCmd.AddGroup(&cobra.Group{ID: "setup", Title: "Setup Commands:"})
	rootCmd.AddGroup(&cobra.Group{ID: "development", Title: "Development Commands:"})
	rootCmd.AddGroup(&cobra.Group{ID: "execution", Title: "Execution Commands:"})
	rootCmd.AddGroup(&cobra.Group{ID: "analysis", Title: "Analysis Commands:"})
	rootCmd.AddGroup(&cobra.Group{ID: "utilities", Title: "Utilities:"})
	rootCmd.PersistentFlags().BoolVarP(&verboseFlag, "verbose", "v", false, "Enable verbose output showing detailed information")
	rootCmd.PersistentFlags().BoolVar(&bannerFlag, "banner", false, "Display ASCII logo banner with purple GitHub color theme")
	rootCmd.SetOut(os.Stderr)
	rootCmd.SilenceUsage = true
	rootCmd.SilenceErrors = true
	rootCmd.SetVersionTemplate(string(constants.CLIExtensionPrefix) + " version {{.Version}}\n")
	rootCmd.InitDefaultHelpFlag()
	if f := rootCmd.Flags().Lookup("help"); f != nil {
		f.Usage = "Show help for " + string(constants.CLIExtensionPrefix)
	}
	rootCmd.InitDefaultVersionFlag()
	if f := rootCmd.Flags().Lookup("version"); f != nil {
		f.Usage = "Print the current version"
	}
	rootCmd.SetUsageFunc(rootUsageFunc)
}

func createCommandSet() commandSet {
	cmds := commandSet{
		addCmd:         cli.NewAddCommand(validateEngine),
		addWizardCmd:   cli.NewAddWizardCommand(validateEngine),
		editCmd:        cli.NewEditCommand(),
		updateCmd:      cli.NewUpdateCommand(validateEngine),
		deployCmd:      cli.NewDeployCommand(validateEngine),
		trialCmd:       cli.NewTrialCommand(validateEngine),
		initCmd:        cli.NewInitCommand(),
		statusCmd:      cli.NewStatusCommand(),
		listCmd:        cli.NewListCommand(),
		mcpCmd:         cli.NewMCPCommand(),
		logsCmd:        cli.NewLogsCommand(),
		auditCmd:       cli.NewAuditCommand(),
		viewCmd:        cli.NewViewCommand(),
		healthCmd:      cli.NewHealthCommand(),
		outcomesCmd:    cli.NewOutcomesCommand(),
		mcpServerCmd:   cli.NewMCPServerCommand(),
		prCmd:          cli.NewPRCommand(),
		secretsCmd:     cli.NewSecretsCommand(),
		fixCmd:         cli.NewFixCommand(),
		upgradeCmd:     cli.NewUpgradeCommand(validateEngine),
		completionCmd:  cli.NewCompletionCommand(),
		hashCmd:        cli.NewHashCommand(),
		projectCmd:     cli.NewProjectCommand(),
		doctorCmd:      cli.NewDoctorCommand(),
		checksCmd:      cli.NewChecksCommand(),
		validateCmd:    cli.NewValidateCommand(validateEngine),
		lintCmd:        cli.NewLintCommand(),
		domainsCmd:     cli.NewDomainsCommand(),
		experimentsCmd: cli.NewExperimentsCommand(),
		forecastCmd:    cli.NewForecastCommand(),
		gradersCmd:     cli.NewGradersCommand(),
		envCmd:         cli.NewEnvCommand(),
		modelsCmd:      cli.NewModelsCommand(),
	}
	cli.RegisterEngineFlagCompletion(cmds.initCmd)
	return cmds
}

func configureNewCmdFlags() {
	newCmd.Flags().BoolP("force", "f", false, "Overwrite existing workflow files without confirmation")
	newCmd.Flags().BoolP("interactive", "i", false, "Launch interactive workflow creation wizard")
	newCmd.Flags().StringP("engine", "e", "", cli.EngineFlagOverrideUsage)
	cli.RegisterEngineFlagCompletion(newCmd)
}

func configureCompileBuildFlags() {
	compileCmd.Flags().StringP("engine", "e", "", cli.EngineFlagOverrideUsage)
	compileCmd.Flags().String("action-mode", "", "How gh-aw action scripts are referenced in compiled workflows: 'dev' uses local paths (for developing gh-aw itself), 'release' emits SHA-pinned remote refs from github/gh-aw, 'action' uses the github/gh-aw-actions repository. Auto-detected from the binary build type if not specified")
	compileCmd.Flags().String("action-tag", "", "Pin compiled workflows to a specific version of gh-aw actions. Accepts a full commit SHA or a version tag (e.g. v1, v1.2.3). Sets --action-mode to 'release' unless --action-mode action is also specified. Cannot be combined with --gh-aw-ref; use --gh-aw-ref when you want to resolve a branch or tag name to its current SHA")
	compileCmd.Flags().String("actions-repo", "", "Override the external actions repository used in action mode (default: github/gh-aw-actions)")
	compileCmd.Flags().String("gh-aw-ref", "", "Pin compiled workflows to a specific branch, tag, or commit SHA of github/gh-aw (e.g. main, my-feature, abc123). Branch and tag names are resolved to their full commit SHA at compile time so the baked-in ref is immutable. Equivalent to --action-mode release --action-tag <resolved-sha>. Cannot be combined with --action-tag or --action-mode. Use this to E2E-test workflows against a specific gh-aw revision")
	compileCmd.Flags().Bool("validate", false, "Enable GitHub Actions workflow schema validation, container image validation, and action SHA validation")
	compileCmd.Flags().BoolP("watch", "w", false, "Watch for changes to workflow files and recompile automatically")
	compileCmd.Flags().StringP("dir", "d", "", "Workflow directory (default: $GH_AW_WORKFLOWS_DIR or .github/workflows)")
	compileCmd.Flags().String("workflows-dir", "", "Deprecated: use --dir instead")
	_ = compileCmd.Flags().MarkDeprecated("workflows-dir", "use --dir instead")
	compileCmd.Flags().Bool("no-emit", false, "Validate workflow without generating lock files")
	compileCmd.Flags().Bool("purge", false, "Delete .lock.yml files that were not regenerated during compilation (only when no specific files are provided)")
	compileCmd.Flags().Bool("strict", false, "Override frontmatter to enforce strict mode validation for all workflows (enforces action pinning, network config, safe-outputs, disallows write permissions and deprecated fields). Note: Workflows default to strict mode unless frontmatter sets strict: false")
	compileCmd.Flags().Bool("trial", false, "Enable trial mode compilation (modifies workflows for trial execution)")
	compileCmd.Flags().StringP("logical-repo", "l", "", "Repository to simulate workflow execution against (for trial mode)")
	compileCmd.Flags().Bool("use-samples", false, "Hidden: replace the agentic 'Execute coding agent' step with a deterministic driver that replays the workflow's safe-outputs `samples` frontmatter entries through the safe-outputs MCP server. Used to make end-to-end tests deterministic.")
	_ = compileCmd.Flags().MarkHidden("use-samples")
}

func configureCompileToolFlags() {
	compileCmd.Flags().Bool("dependabot", false, "Generate dependency manifests (package.json, requirements.txt, go.mod) and Dependabot config when dependencies are detected")
	compileCmd.Flags().BoolP("force", "f", false, "Force overwrite of existing dependency files (only applies when --dependabot is set; e.g., dependabot.yml)")
	compileCmd.Flags().Bool("refresh-stop-time", false, "Force regeneration of stop-after times instead of preserving existing values from lock files")
	compileCmd.Flags().Bool("force-refresh-action-pins", false, "Force refresh of action pins by clearing the cache and resolving all action SHAs from GitHub API")
	compileCmd.Flags().Bool("force-refresh-container-pins", false, "Force refresh existing container image digest pins before compiling")
	compileCmd.Flags().Bool("allow-action-refs", false, "Allow unresolved action refs and emit warnings instead of failing validation")
	compileCmd.Flags().Bool("zizmor", false, "Run zizmor security scanner on generated .lock.yml files")
	compileCmd.Flags().Bool("poutine", false, "Run poutine security scanner on generated .lock.yml files")
	compileCmd.Flags().Bool("actionlint", false, "Run actionlint linter on generated .lock.yml files")
	compileCmd.Flags().Bool("runner-guard", false, "Run runner-guard taint analysis scanner on generated .lock.yml files (uses Docker image "+cli.RunnerGuardImage+")")
	compileCmd.Flags().Bool("syft", false, "Run syft SBOM scanner on container images referenced in compiled .lock.yml files (uses Docker image "+cli.SyftImage+")")
	compileCmd.Flags().Bool("grype", false, "Run grype vulnerability scanner on container images referenced in compiled .lock.yml files (uses Docker image "+cli.GrypeImage+")")
	compileCmd.Flags().Bool("grant", false, "Run grant license scanner on container images referenced in compiled .lock.yml files (uses Docker image "+cli.GrantImage+")")
	compileCmd.Flags().Bool("yamllint", false, "Run yamllint YAML linter on generated .lock.yml files (uses Docker image "+cli.YamllintImage+")")
	compileCmd.Flags().Bool("shellcheck", false, "Run shellcheck linting of run step scripts")
	compileCmd.Flags().Bool("no-shellcheck", false, "Deprecated: shellcheck is now opt-in via --shellcheck; this flag is a no-op and will be removed in a future release")
	_ = compileCmd.Flags().MarkDeprecated("no-shellcheck", "shellcheck is now opt-in; use --shellcheck to enable it. This flag has no effect and will be removed in a future release")
	compileCmd.Flags().Bool("fix", false, "Apply automatic codemod fixes to workflows before compiling")
	compileCmd.Flags().BoolP("json", "j", false, "Output results in JSON format")
	compileCmd.Flags().Bool("show-all", false, "Display all compilation errors instead of only the highest-priority subset (default: top 5)")
	compileCmd.Flags().Bool("stats", false, "Display statistics table sorted by workflow file size (shows jobs, steps, scripts, and shells)")
	compileCmd.Flags().Bool("models", false, "Warn when models configured in models or engine.models are absent from the active model inventory")
	compileCmd.Flags().Bool("fail-fast", false, "Stop at the first validation error instead of collecting all errors")
	compileCmd.Flags().Bool("no-check-update", false, "Skip checking for gh-aw updates")
	compileCmd.Flags().String("schedule-seed", "", "Override the repository slug (owner/repo) used as seed for fuzzy schedule scattering (e.g., \"github/gh-aw\"). Bypasses git remote detection entirely. Use this when your git remote is not named \"origin\" and you have multiple remotes configured")
	compileCmd.Flags().Bool("staged", false, "Force all safe-outputs into staged mode")
	compileCmd.Flags().Bool("approve", false, "Approve all safe update changes. When strict mode is active (the default), the compiler emits warnings for new restricted secrets or unapproved action additions/removals not present in the existing gh-aw-manifest. Use this flag to approve and skip safe update enforcement")
	compileCmd.Flags().Bool("validate-images", false, "Require Docker to be available for container image validation. Without this flag, container image validation is silently skipped when Docker is not installed or the daemon is not running")
	compileCmd.Flags().String("prior-manifest-file", "", "Path to a JSON file containing pre-cached gh-aw-manifests (map[lockFile]*GHAWManifest); used by the MCP server to supply a tamper-proof manifest baseline captured at startup")
	compileCmd.Flags().Bool("ghes", false, "Enable GitHub Enterprise Server (GHES) compatibility mode: emit upload-artifact@v3.2.2 and download-artifact@v3.1.0. Overrides the aw.json ghes field.")
}

func finalizeCompileFlags() {
	if err := compileCmd.Flags().MarkHidden("prior-manifest-file"); err != nil {
		_ = err
	}
	compileCmd.MarkFlagsMutuallyExclusive("dir", "workflows-dir")
	compileCmd.MarkFlagsMutuallyExclusive("gh-aw-ref", "action-tag")
	compileCmd.MarkFlagsMutuallyExclusive("gh-aw-ref", "action-mode")
	compileCmd.ValidArgsFunction = cli.CompleteWorkflowNames
	cli.RegisterEngineFlagCompletion(compileCmd)
	cli.RegisterDirFlagCompletion(compileCmd, "dir")
}

func configureOtherCommandFlags() {
	removeCmd.Flags().Bool("no-remove-orphans", false, "Skip removal of orphaned include files that are no longer referenced by any workflow")
	removeCmd.Flags().Bool("keep-orphans", false, "Skip removal of orphaned include files that are no longer referenced by any workflow")
	_ = removeCmd.Flags().MarkDeprecated("keep-orphans", "use --no-remove-orphans instead")
	removeCmd.Flags().StringP("dir", "d", "", "Workflow directory (default: $GH_AW_WORKFLOWS_DIR or .github/workflows)")
	removeCmd.ValidArgsFunction = cli.CompleteWorkflowNames
	cli.RegisterDirFlagCompletion(removeCmd, "dir")

	enableCmd.Flags().StringP("repo", "r", "", "Target repository ([HOST/]owner/repo format). Defaults to current repository")
	disableCmd.Flags().StringP("repo", "r", "", "Target repository ([HOST/]owner/repo format). Defaults to current repository")
	enableCmd.ValidArgsFunction = cli.CompleteWorkflowNames
	disableCmd.ValidArgsFunction = cli.CompleteWorkflowNames

	runCmd.Flags().Int("repeat", 0, "Number of additional times to run after the initial execution (e.g., --repeat 3 runs 4 times total)")
	runCmd.Flags().Bool("enable-if-needed", false, "Enable the workflow before running if needed, and restore state afterward")
	runCmd.Flags().StringP("engine", "e", "", cli.EngineFlagOverrideUsage)
	runCmd.Flags().StringP("repo", "r", "", "Target repository ([HOST/]owner/repo format). Defaults to current repository")
	runCmd.Flags().String("ref", "", "Branch or tag name to run the workflow on (default: current branch)")
	runCmd.Flags().Bool("auto-merge-prs", false, "Auto-merge any pull requests created during the workflow execution")
	runCmd.Flags().StringArrayP("raw-field", "F", []string{}, "Pass a workflow dispatch input in key=value format (can be specified multiple times)")
	_ = runCmd.Flags().MarkShorthandDeprecated("raw-field", "use the long form --raw-field instead")
	runCmd.Flags().Bool("push", false, "Commit and push workflow files (including transitive imports) before running. Refuses to proceed when unrelated files are already staged.")
	runCmd.Flags().Bool("dry-run", false, "Preview workflow execution without triggering runs on GitHub Actions")
	runCmd.Flags().BoolP("json", "j", false, "Output results in JSON format")
	runCmd.Flags().Bool("approve", false, "Approve all safe update changes. When strict mode is active (the default), the compiler emits warnings for new restricted secrets or unapproved action additions/removals not present in the existing gh-aw-manifest. Use this flag to approve and skip safe update enforcement")
	runCmd.ValidArgsFunction = cli.CompleteWorkflowNames
	cli.RegisterEngineFlagCompletion(runCmd)
}

func assignCommandGroups(cmds commandSet) {
	cmds.initCmd.GroupID, newCmd.GroupID, cmds.addCmd.GroupID, cmds.addWizardCmd.GroupID = "setup", "setup", "setup", "setup"
	removeCmd.GroupID, cmds.editCmd.GroupID, cmds.updateCmd.GroupID, cmds.deployCmd.GroupID, cmds.upgradeCmd.GroupID = "setup", "setup", "setup", "setup", "setup"
	cmds.secretsCmd.GroupID, cmds.envCmd.GroupID, cmds.doctorCmd.GroupID = "setup", "setup", "setup"
	compileCmd.GroupID, cmds.validateCmd.GroupID, cmds.lintCmd.GroupID = "development", "development", "development"
	cmds.mcpCmd.GroupID, cmds.fixCmd.GroupID, cmds.domainsCmd.GroupID = "development", "development", "development"
	runCmd.GroupID, enableCmd.GroupID, disableCmd.GroupID, cmds.trialCmd.GroupID = "execution", "execution", "execution", "execution"
	cmds.logsCmd.GroupID, cmds.auditCmd.GroupID, cmds.viewCmd.GroupID = "analysis", "analysis", "analysis"
	cmds.healthCmd.GroupID, cmds.outcomesCmd.GroupID, cmds.checksCmd.GroupID = "analysis", "analysis", "analysis"
	cmds.statusCmd.GroupID, cmds.listCmd.GroupID, cmds.experimentsCmd.GroupID, cmds.forecastCmd.GroupID, cmds.modelsCmd.GroupID = "analysis", "analysis", "analysis", "analysis", "analysis"
	cmds.gradersCmd.GroupID = "analysis"
	cmds.mcpServerCmd.GroupID, cmds.prCmd.GroupID, cmds.completionCmd.GroupID, cmds.hashCmd.GroupID, cmds.projectCmd.GroupID = "utilities", "utilities", "utilities", "utilities", "utilities"
}

func addCommandsToRoot(cmds commandSet) {
	rootCmd.AddCommand(
		compileCmd, cmds.addCmd, cmds.addWizardCmd, cmds.editCmd, cmds.updateCmd, cmds.deployCmd, cmds.upgradeCmd, cmds.trialCmd, newCmd, cmds.initCmd,
		runCmd, removeCmd, cmds.statusCmd, cmds.listCmd, enableCmd, disableCmd, cmds.logsCmd, cmds.auditCmd, cmds.viewCmd,
		cmds.healthCmd, cmds.outcomesCmd, cmds.checksCmd, cmds.mcpCmd, cmds.mcpServerCmd, cmds.prCmd, versionCmd, cmds.secretsCmd,
		cmds.fixCmd, cmds.validateCmd, cmds.lintCmd, cmds.completionCmd, cmds.hashCmd, cmds.projectCmd, cmds.doctorCmd,
		cmds.domainsCmd, cmds.experimentsCmd, cmds.forecastCmd, cmds.gradersCmd, cmds.modelsCmd, cmds.envCmd,
	)
}

func fixSubCommandHelpFlags() {
	var fix func(cmd *cobra.Command)
	fix = func(cmd *cobra.Command) {
		cmd.InitDefaultHelpFlag()
		if f := cmd.Flags().Lookup("help"); f != nil {
			f.Usage = "Show help for " + fixPathForCommand(cmd.CommandPath())
		}
		for _, sub := range cmd.Commands() {
			fix(sub)
		}
	}
	for _, sub := range rootCmd.Commands() {
		fix(sub)
	}
}

func init() {
	configureRootCommand()
	rootCmd.SetHelpCommand(newCustomHelpCmd())
	cmds := createCommandSet()
	configureNewCmdFlags()
	configureCompileBuildFlags()
	configureCompileToolFlags()
	finalizeCompileFlags()
	configureOtherCommandFlags()
	assignCommandGroups(cmds)
	addCommandsToRoot(cmds)
	fixSubCommandHelpFlags()
}

func main() {
	// Set version information in the CLI package
	cli.SetVersionInfo(version)

	// Set version information in the workflow package for generated file headers
	workflow.SetVersion(version)

	// Set release flag in the workflow package
	workflow.SetIsRelease(isRelease == "true")

	// Set up a context that is cancelled when Ctrl-C (SIGINT) or SIGTERM is received.
	// This ensures all commands and subprocesses are properly interrupted on Ctrl-C.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := rootCmd.ExecuteContext(ctx); err != nil {
		// ExitCodeError signals an intentional exit with a specific code (e.g.
		// after relaunching the upgraded binary). Honour it without printing an
		// error message.
		var exitCodeErr *cli.ExitCodeError
		if errors.As(err, &exitCodeErr) {
			os.Exit(exitCodeErr.Code)
		}

		errMsg := err.Error()
		// Check if error is already formatted to avoid double formatting:
		// - Contains suggestions (FormatErrorWithSuggestions)
		// - Starts with ✗ (FormatErrorMessage)
		// - Contains file:line:column: pattern (console.FormatError)
		isAlreadyFormatted := strings.Contains(errMsg, "Suggestions:") ||
			strings.HasPrefix(errMsg, "✗") ||
			strings.Contains(errMsg, ":") && (strings.Contains(errMsg, "error:") || strings.Contains(errMsg, "warning:"))

		if isAlreadyFormatted {
			fmt.Fprintln(os.Stderr, errMsg)
		} else {
			fmt.Fprintln(os.Stderr, console.FormatErrorChain(err))
		}
		os.Exit(1)
	}
}
