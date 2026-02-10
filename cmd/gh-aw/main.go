package main

import (
	"fmt"
	"os"
	"sort"
	"strings"

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
	Short:   "GitHub Agentic Workflows CLI from GitHub Next",
	Version: version,
	Long: `GitHub Agentic Workflows from GitHub Next

Common Tasks:
  gh aw init                  # Set up a new repository
  gh aw new my-workflow       # Create your first workflow
  gh aw compile               # Compile all workflows
  gh aw run my-workflow       # Execute a workflow
  gh aw logs my-workflow      # View execution logs
  gh aw audit <run-id>        # Debug a failed run

For detailed help on any command, use:
  gh aw [command] --help`,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
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
	Short: "Create a new workflow Markdown file with example configuration",
	Long: `Create a new workflow Markdown file with commented examples and explanations of all available options.

When called without a workflow name (or with --interactive flag), launches an interactive wizard
to guide you through creating a workflow with custom settings.

When called with a workflow name, creates a template file with comprehensive examples of:
- All trigger types (on: events)
- Permissions configuration
- AI processor settings
- Tools configuration (github, claude, MCPs)
- All frontmatter options with explanations

` + cli.WorkflowIDExplanation + `

Examples:
  ` + string(constants.CLIExtensionPrefix) + ` new                      # Interactive mode
  ` + string(constants.CLIExtensionPrefix) + ` new my-workflow          # Create template file
  ` + string(constants.CLIExtensionPrefix) + ` new my-workflow.md       # Same as above (.md extension stripped)
  ` + string(constants.CLIExtensionPrefix) + ` new my-workflow --force  # Overwrite if exists`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		forceFlag, _ := cmd.Flags().GetBool("force")
		verbose, _ := cmd.Flags().GetBool("verbose")
		interactiveFlag, _ := cmd.Flags().GetBool("interactive")

		// If no arguments provided or interactive flag is set, use interactive mode
		if len(args) == 0 || interactiveFlag {
			// Check if running in CI environment
			if cli.IsRunningInCI() {
				return fmt.Errorf("interactive mode cannot be used in CI environments. Please provide a workflow name")
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
		return cli.NewWorkflow(workflowName, verbose, forceFlag)
	},
}

var removeCmd = &cobra.Command{
	Use:   "remove [pattern]",
	Short: "Remove agentic workflow files matching the given name prefix",
	Long: `Remove workflow files matching the given workflow-id pattern.

The workflow-id is the basename of the Markdown file without the .md extension.
You can provide a workflow-id prefix to remove multiple workflows, or a specific workflow-id.

By default, this command also removes orphaned include files that are no longer referenced
by any workflow. Use --keep-orphans to skip this cleanup.

Examples:
  ` + string(constants.CLIExtensionPrefix) + ` remove my-workflow       # Remove specific workflow
  ` + string(constants.CLIExtensionPrefix) + ` remove test-             # Remove all workflows starting with 'test-'
  ` + string(constants.CLIExtensionPrefix) + ` remove old- --keep-orphans  # Remove workflows but keep orphaned includes`,
	RunE: func(cmd *cobra.Command, args []string) error {
		var pattern string
		if len(args) > 0 {
			pattern = args[0]
		}
		keepOrphans, _ := cmd.Flags().GetBool("keep-orphans")
		return cli.RemoveWorkflows(pattern, keepOrphans)
	},
}

var enableCmd = &cobra.Command{
	Use:   "enable [workflow]...",
	Short: "Enable agentic workflows",
	Long: `Enable one or more workflows by ID, or all workflows if no IDs are provided.

` + cli.WorkflowIDExplanation + `

Examples:
  ` + string(constants.CLIExtensionPrefix) + ` enable                    # Enable all workflows
  ` + string(constants.CLIExtensionPrefix) + ` enable ci-doctor         # Enable specific workflow
  ` + string(constants.CLIExtensionPrefix) + ` enable ci-doctor.md      # Enable specific workflow (alternative format)
  ` + string(constants.CLIExtensionPrefix) + ` enable ci-doctor daily   # Enable multiple workflows
  ` + string(constants.CLIExtensionPrefix) + ` enable ci-doctor --repo owner/repo  # Enable workflow in specific repository`,
	RunE: func(cmd *cobra.Command, args []string) error {
		repoOverride, _ := cmd.Flags().GetString("repo")
		return cli.EnableWorkflowsByNames(args, repoOverride)
	},
}

var disableCmd = &cobra.Command{
	Use:   "disable [workflow]...",
	Short: "Disable agentic workflows and cancel any in-progress runs",
	Long: `Disable one or more workflows by ID, or all workflows if no IDs are provided.

` + cli.WorkflowIDExplanation + `

Examples:
  ` + string(constants.CLIExtensionPrefix) + ` disable                    # Disable all workflows
  ` + string(constants.CLIExtensionPrefix) + ` disable ci-doctor         # Disable specific workflow
  ` + string(constants.CLIExtensionPrefix) + ` disable ci-doctor.md      # Disable specific workflow (alternative format)
  ` + string(constants.CLIExtensionPrefix) + ` disable ci-doctor daily   # Disable multiple workflows
  ` + string(constants.CLIExtensionPrefix) + ` disable ci-doctor --repo owner/repo  # Disable workflow in specific repository`,
	RunE: func(cmd *cobra.Command, args []string) error {
		repoOverride, _ := cmd.Flags().GetString("repo")
		return cli.DisableWorkflowsByNames(args, repoOverride)
	},
}

var compileCmd = &cobra.Command{
	Use:   "compile [workflow]...",
	Short: "Compile agentic workflows from Markdown to GitHub Actions YAML",
	Long: `Compile one or more agentic workflows to YAML workflows.

If no workflows are specified, all Markdown files in .github/workflows will be compiled.

` + cli.WorkflowIDExplanation + `

The --dependabot flag generates dependency manifests when dependencies are detected:
  - For npm: Creates package.json and package-lock.json (requires npm in PATH)
  - For Python: Creates requirements.txt for pip packages
  - For Go: Creates go.mod for go install/get packages
  - Creates .github/dependabot.yml with all detected ecosystems
  - Use --force to overwrite existing dependabot.yml
  - Cannot be used with specific workflow files or custom --dir
  - Only processes workflows in the default .github/workflows directory

Examples:
  ` + string(constants.CLIExtensionPrefix) + ` compile                    # Compile all Markdown files
  ` + string(constants.CLIExtensionPrefix) + ` compile ci-doctor    # Compile a specific workflow
  ` + string(constants.CLIExtensionPrefix) + ` compile ci-doctor daily-plan  # Compile multiple workflows
  ` + string(constants.CLIExtensionPrefix) + ` compile workflow.md        # Compile by file path
  ` + string(constants.CLIExtensionPrefix) + ` compile --dir custom/workflows  # Compile from custom directory
  ` + string(constants.CLIExtensionPrefix) + ` compile --watch ci-doctor     # Watch and auto-compile
  ` + string(constants.CLIExtensionPrefix) + ` compile --trial --logical-repo owner/repo  # Compile for trial mode
  ` + string(constants.CLIExtensionPrefix) + ` compile --dependabot        # Generate Dependabot manifests
  ` + string(constants.CLIExtensionPrefix) + ` compile --dependabot --force  # Force overwrite existing dependabot.yml`,
	RunE: func(cmd *cobra.Command, args []string) error {
		engineOverride, _ := cmd.Flags().GetString("engine")
		actionMode, _ := cmd.Flags().GetString("action-mode")
		actionTag, _ := cmd.Flags().GetString("action-tag")
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
		zizmor, _ := cmd.Flags().GetBool("zizmor")
		poutine, _ := cmd.Flags().GetBool("poutine")
		actionlint, _ := cmd.Flags().GetBool("actionlint")
		jsonOutput, _ := cmd.Flags().GetBool("json")
		fix, _ := cmd.Flags().GetBool("fix")
		stats, _ := cmd.Flags().GetBool("stats")
		failFast, _ := cmd.Flags().GetBool("fail-fast")
		noCheckUpdate, _ := cmd.Flags().GetBool("no-check-update")
		verbose, _ := cmd.Flags().GetBool("verbose")
		if err := validateEngine(engineOverride); err != nil {
			return err
		}

		// Check for updates (non-blocking, runs once per day)
		cli.CheckForUpdatesAsync(cmd.Context(), noCheckUpdate, verbose)

		// If --fix is specified, run fix --write first
		if fix {
			fixConfig := cli.FixConfig{
				WorkflowIDs: args,
				Write:       true,
				Verbose:     verbose,
				WorkflowDir: dir,
			}
			if err := cli.RunFix(fixConfig); err != nil {
				return err
			}
		}

		// Handle --workflows-dir deprecation (mutual exclusion is enforced by Cobra)
		workflowDir := dir
		if workflowsDir != "" {
			workflowDir = workflowsDir
		}
		config := cli.CompileConfig{
			MarkdownFiles:          args,
			Verbose:                verbose,
			EngineOverride:         engineOverride,
			ActionMode:             actionMode,
			ActionTag:              actionTag,
			Validate:               validate,
			Watch:                  watch,
			WorkflowDir:            workflowDir,
			SkipInstructions:       false, // Deprecated field, kept for backward compatibility
			NoEmit:                 noEmit,
			Purge:                  purge,
			TrialMode:              trial,
			TrialLogicalRepoSlug:   logicalRepo,
			Strict:                 strict,
			Dependabot:             dependabot,
			ForceOverwrite:         forceOverwrite,
			RefreshStopTime:        refreshStopTime,
			ForceRefreshActionPins: forceRefreshActionPins,
			Zizmor:                 zizmor,
			Poutine:                poutine,
			Actionlint:             actionlint,
			JSONOutput:             jsonOutput,
			Stats:                  stats,
			FailFast:               failFast,
		}
		if _, err := cli.CompileWorkflows(cmd.Context(), config); err != nil {
			// Return error as-is without additional formatting
			// Errors from CompileWorkflows are already formatted with console.FormatError
			// which provides IDE-parseable location information (file:line:column)
			return err
		}
		return nil
	},
}

var runCmd = &cobra.Command{
	Use:   "run [workflow]...",
	Short: "Run one or more agentic workflows on GitHub Actions",
	Long: `Run one or more agentic workflows on GitHub Actions using the workflow_dispatch trigger.

When called without workflow arguments, enters interactive mode with:
- List of workflows that support workflow_dispatch
- Display of required and optional inputs
- Input collection with validation
- Command display for future reference

This command accepts one or more workflow IDs.
The workflows must have been added as actions and compiled.

This command only works with workflows that have workflow_dispatch triggers.
It executes 'gh workflow run <workflow-lock-file>' to trigger each workflow on GitHub Actions.

By default, workflows are run on the current branch. Use --ref to specify a different branch or tag.

` + cli.WorkflowIDExplanation + `

Examples:
  gh aw run                          # Interactive mode
  gh aw run daily-perf-improver
  gh aw run daily-perf-improver.md   # Alternative format
  gh aw run daily-perf-improver --ref main  # Run on specific branch
  gh aw run daily-perf-improver --repeat 3  # Run 3 times total
  gh aw run daily-perf-improver --enable-if-needed # Enable if disabled, run, then restore state
  gh aw run daily-perf-improver --auto-merge-prs # Auto-merge any PRs created during execution
  gh aw run daily-perf-improver -f name=value -f env=prod  # Pass workflow inputs
  gh aw run daily-perf-improver --push  # Commit and push workflow files before running
  gh aw run daily-perf-improver --dry-run  # Validate without actually running`,
	Args: cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		repeatCount, _ := cmd.Flags().GetInt("repeat")
		enable, _ := cmd.Flags().GetBool("enable-if-needed")
		engineOverride, _ := cmd.Flags().GetString("engine")
		repoOverride, _ := cmd.Flags().GetString("repo")
		refOverride, _ := cmd.Flags().GetString("ref")
		autoMergePRs, _ := cmd.Flags().GetBool("auto-merge-prs")
		pushSecrets, _ := cmd.Flags().GetBool("use-local-secrets")
		inputs, _ := cmd.Flags().GetStringArray("raw-field")
		push, _ := cmd.Flags().GetBool("push")
		dryRun, _ := cmd.Flags().GetBool("dry-run")

		if err := validateEngine(engineOverride); err != nil {
			return err
		}

		// If no arguments provided, enter interactive mode
		if len(args) == 0 {
			// Check if running in CI environment
			if cli.IsRunningInCI() {
				return fmt.Errorf("interactive mode cannot be used in CI environments. Please provide a workflow name")
			}

			// Interactive mode doesn't support repeat or enable flags
			if repeatCount > 0 {
				return fmt.Errorf("--repeat flag is not supported in interactive mode")
			}
			if enable {
				return fmt.Errorf("--enable-if-needed flag is not supported in interactive mode")
			}
			if len(inputs) > 0 {
				return fmt.Errorf("workflow inputs cannot be specified in interactive mode (they will be collected interactively)")
			}

			return cli.RunWorkflowInteractively(cmd.Context(), verboseFlag, repoOverride, refOverride, autoMergePRs, pushSecrets, push, engineOverride, dryRun)
		}

		return cli.RunWorkflowsOnGitHub(cmd.Context(), args, repeatCount, enable, engineOverride, repoOverride, refOverride, autoMergePRs, pushSecrets, push, inputs, verboseFlag, dryRun)
	},
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show gh aw extension version information",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Fprintf(os.Stderr, "%s version %s\n", string(constants.CLIExtensionPrefix), version)
		return nil
	},
}

func init() {
	// Add command groups to root command
	rootCmd.AddGroup(&cobra.Group{
		ID:    "setup",
		Title: "Setup Commands:",
	})
	rootCmd.AddGroup(&cobra.Group{
		ID:    "development",
		Title: "Development Commands:",
	})
	rootCmd.AddGroup(&cobra.Group{
		ID:    "execution",
		Title: "Execution Commands:",
	})
	rootCmd.AddGroup(&cobra.Group{
		ID:    "analysis",
		Title: "Analysis Commands:",
	})
	rootCmd.AddGroup(&cobra.Group{
		ID:    "utilities",
		Title: "Utilities:",
	})

	// Add global verbose flag to root command
	rootCmd.PersistentFlags().BoolVarP(&verboseFlag, "verbose", "v", false, "Enable verbose output showing detailed information")

	// Add global banner flag to root command
	rootCmd.PersistentFlags().BoolVar(&bannerFlag, "banner", false, "Display ASCII logo banner with purple GitHub color theme")

	// Set output to stderr for consistency with CLI logging guidelines
	rootCmd.SetOut(os.Stderr)

	// Silence usage output on errors - prevents cluttering terminal output with
	// full usage text when application errors occur (e.g., compilation errors,
	// network timeouts). Users can still run --help for usage information.
	rootCmd.SilenceUsage = true

	// Silence errors - since we're using RunE and returning errors, Cobra will
	// print errors automatically. We handle error formatting ourselves in main().
	rootCmd.SilenceErrors = true

	// Set version template to match the version subcommand format
	rootCmd.SetVersionTemplate(fmt.Sprintf("%s version {{.Version}}\n", string(constants.CLIExtensionPrefix)))

	// Create custom help command that supports "all" subcommand
	customHelpCmd := &cobra.Command{
		Use:   "help [command]",
		Short: "Help about any command",
		Long: `Help provides help for any command in the application.
Simply type ` + string(constants.CLIExtensionPrefix) + ` help [path to command] for full details.

Use "` + string(constants.CLIExtensionPrefix) + ` help all" to show help for all commands.`,
		RunE: func(c *cobra.Command, args []string) error {
			// Check if the argument is "all"
			if len(args) == 1 && args[0] == "all" {
				// Print header
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage("GitHub Agentic Workflows CLI - Complete Command Reference"))
				fmt.Fprintln(os.Stderr, "")

				// Iterate through all commands and print their help
				for _, subCmd := range rootCmd.Commands() {
					// Skip hidden commands and help itself
					if subCmd.Hidden || subCmd.Name() == "help" {
						continue
					}

					// Print command separator
					fmt.Fprintln(os.Stderr, console.FormatInfoMessage("═══════════════════════════════════════════════════════════════"))
					fmt.Fprintf(os.Stderr, "\n%s\n\n", console.FormatInfoMessage(fmt.Sprintf("Command: %s %s", string(constants.CLIExtensionPrefix), subCmd.Name())))

					// Print the command's help
					_ = subCmd.Help()
					fmt.Fprintln(os.Stderr, "")
				}

				// Print footer
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage("═══════════════════════════════════════════════════════════════"))
				fmt.Fprintln(os.Stderr, "")
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage("For more information, visit: https://github.github.com/gh-aw/"))
				return nil
			}

			// Otherwise, use the default help behavior
			cmd, _, e := rootCmd.Find(args)
			if cmd == nil || e != nil {
				return fmt.Errorf("unknown help topic [%#q]", args)
			} else {
				cmd.InitDefaultHelpFlag() // make possible 'help' flag to be shown
				return cmd.Help()
			}
		},
	}

	// Replace the default help command
	rootCmd.SetHelpCommand(customHelpCmd)

	// Create and setup add command
	addCmd := cli.NewAddCommand(validateEngine)

	// Create and setup update command
	updateCmd := cli.NewUpdateCommand(validateEngine)

	// Create and setup trial command
	trialCmd := cli.NewTrialCommand(validateEngine)

	// Create and setup init command
	initCmd := cli.NewInitCommand()

	// Add flags to new command
	newCmd.Flags().BoolP("force", "f", false, "Overwrite existing files without confirmation")
	newCmd.Flags().BoolP("interactive", "i", false, "Launch interactive workflow creation wizard")

	// Add AI flag to compile and add commands
	compileCmd.Flags().StringP("engine", "e", "", "Override AI engine (claude, codex, copilot, custom)")
	compileCmd.Flags().String("action-mode", "", "Action script inlining mode (inline, dev, release). Auto-detected if not specified")
	compileCmd.Flags().String("action-tag", "", "Override action SHA or tag for actions/setup (overrides action-mode to release). Accepts full SHA or tag name")
	compileCmd.Flags().Bool("validate", false, "Enable GitHub Actions workflow schema validation, container image validation, and action SHA validation")
	compileCmd.Flags().BoolP("watch", "w", false, "Watch for changes to workflow files and recompile automatically")
	compileCmd.Flags().StringP("dir", "d", "", "Workflow directory (default: .github/workflows)")
	compileCmd.Flags().String("workflows-dir", "", "Deprecated: use --dir instead")
	_ = compileCmd.Flags().MarkDeprecated("workflows-dir", "use --dir instead")
	compileCmd.Flags().Bool("no-emit", false, "Validate workflow without generating lock files")
	compileCmd.Flags().Bool("purge", false, "Delete .lock.yml files that were not regenerated during compilation (only when no specific files are specified)")
	compileCmd.Flags().Bool("strict", false, "Override frontmatter to enforce strict mode validation for all workflows (enforces action pinning, network config, safe-outputs, refuses write permissions and deprecated fields). Note: Workflows default to strict mode unless frontmatter sets strict: false")
	compileCmd.Flags().Bool("trial", false, "Enable trial mode compilation (modifies workflows for trial execution)")
	compileCmd.Flags().String("logical-repo", "", "Repository to simulate workflow execution against (for trial mode)")
	compileCmd.Flags().Bool("dependabot", false, "Generate dependency manifests (package.json, requirements.txt, go.mod) and Dependabot config when dependencies are detected")
	compileCmd.Flags().Bool("force", false, "Force overwrite of existing dependency files (e.g., dependabot.yml)")
	compileCmd.Flags().Bool("refresh-stop-time", false, "Force regeneration of stop-after times instead of preserving existing values from lock files")
	compileCmd.Flags().Bool("force-refresh-action-pins", false, "Force refresh of action pins by clearing the cache and resolving all action SHAs from GitHub API")
	compileCmd.Flags().Bool("zizmor", false, "Run zizmor security scanner on generated .lock.yml files")
	compileCmd.Flags().Bool("poutine", false, "Run poutine security scanner on generated .lock.yml files")
	compileCmd.Flags().Bool("actionlint", false, "Run actionlint linter on generated .lock.yml files")
	compileCmd.Flags().Bool("fix", false, "Apply automatic codemod fixes to workflows before compiling")
	compileCmd.Flags().BoolP("json", "j", false, "Output results in JSON format")
	compileCmd.Flags().Bool("stats", false, "Display statistics table sorted by file size (shows jobs, steps, scripts, and shells)")
	compileCmd.Flags().Bool("fail-fast", false, "Stop at the first validation error instead of collecting all errors")
	compileCmd.Flags().Bool("no-check-update", false, "Skip checking for gh-aw updates")
	compileCmd.MarkFlagsMutuallyExclusive("dir", "workflows-dir")

	// Register completions for compile command
	compileCmd.ValidArgsFunction = cli.CompleteWorkflowNames
	cli.RegisterEngineFlagCompletion(compileCmd)
	cli.RegisterDirFlagCompletion(compileCmd, "dir")

	rootCmd.AddCommand(compileCmd)

	// Add flags to remove command
	removeCmd.Flags().Bool("keep-orphans", false, "Skip removal of orphaned include files that are no longer referenced by any workflow")
	// Register completions for remove command
	removeCmd.ValidArgsFunction = cli.CompleteWorkflowNames

	// Add flags to enable/disable commands
	enableCmd.Flags().StringP("repo", "r", "", "Target repository (owner/repo format). Defaults to current repository")
	disableCmd.Flags().StringP("repo", "r", "", "Target repository (owner/repo format). Defaults to current repository")
	// Register completions for enable/disable commands
	enableCmd.ValidArgsFunction = cli.CompleteWorkflowNames
	disableCmd.ValidArgsFunction = cli.CompleteWorkflowNames

	// Add flags to run command
	runCmd.Flags().Int("repeat", 0, "Number of times to repeat running workflows (0 = run once)")
	runCmd.Flags().Bool("enable-if-needed", false, "Enable the workflow before running if needed, and restore state afterward")
	runCmd.Flags().StringP("engine", "e", "", "Override AI engine (claude, codex, copilot, custom)")
	runCmd.Flags().StringP("repo", "r", "", "Target repository (owner/repo format). Defaults to current repository")
	runCmd.Flags().String("ref", "", "Branch or tag name to run the workflow on (default: current branch)")
	runCmd.Flags().Bool("auto-merge-prs", false, "Auto-merge any pull requests created during the workflow execution")
	runCmd.Flags().Bool("use-local-secrets", false, "Use local environment API key secrets for workflow execution (pushes and cleans up secrets in repository)")
	runCmd.Flags().StringArrayP("raw-field", "F", []string{}, "Add a string parameter in key=value format (can be used multiple times)")
	runCmd.Flags().Bool("push", false, "Commit and push workflow files (including transitive imports) before running")
	runCmd.Flags().Bool("dry-run", false, "Validate workflow without actually triggering execution on GitHub Actions")
	// Register completions for run command
	runCmd.ValidArgsFunction = cli.CompleteWorkflowNames
	cli.RegisterEngineFlagCompletion(runCmd)

	// Create and setup status command
	statusCmd := cli.NewStatusCommand()

	// Create and setup list command
	listCmd := cli.NewListCommand()

	// Create commands that need group assignment
	mcpCmd := cli.NewMCPCommand()
	logsCmd := cli.NewLogsCommand()
	auditCmd := cli.NewAuditCommand()
	healthCmd := cli.NewHealthCommand()
	mcpServerCmd := cli.NewMCPServerCommand()
	prCmd := cli.NewPRCommand()
	secretsCmd := cli.NewSecretsCommand()
	fixCmd := cli.NewFixCommand()
	upgradeCmd := cli.NewUpgradeCommand()
	completionCmd := cli.NewCompletionCommand()
	hashCmd := cli.NewHashCommand()
	projectCmd := cli.NewProjectCommand()

	// Assign commands to groups
	// Setup Commands
	initCmd.GroupID = "setup"
	newCmd.GroupID = "setup"
	addCmd.GroupID = "setup"
	removeCmd.GroupID = "setup"
	updateCmd.GroupID = "setup"
	upgradeCmd.GroupID = "setup"
	secretsCmd.GroupID = "setup"

	// Development Commands
	compileCmd.GroupID = "development"
	mcpCmd.GroupID = "development"
	statusCmd.GroupID = "development"
	listCmd.GroupID = "development"
	fixCmd.GroupID = "development"

	// Execution Commands
	runCmd.GroupID = "execution"
	enableCmd.GroupID = "execution"
	disableCmd.GroupID = "execution"
	trialCmd.GroupID = "execution"

	// Analysis Commands
	logsCmd.GroupID = "analysis"
	auditCmd.GroupID = "analysis"
	healthCmd.GroupID = "analysis"

	// Utilities
	mcpServerCmd.GroupID = "utilities"
	prCmd.GroupID = "utilities"
	completionCmd.GroupID = "utilities"
	hashCmd.GroupID = "utilities"
	projectCmd.GroupID = "utilities"

	// version command is intentionally left without a group (common practice)

	// Add all commands to root
	rootCmd.AddCommand(addCmd)
	rootCmd.AddCommand(updateCmd)
	rootCmd.AddCommand(upgradeCmd)
	rootCmd.AddCommand(trialCmd)
	rootCmd.AddCommand(newCmd)
	rootCmd.AddCommand(initCmd)

	rootCmd.AddCommand(runCmd)
	rootCmd.AddCommand(removeCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(enableCmd)
	rootCmd.AddCommand(disableCmd)
	rootCmd.AddCommand(logsCmd)
	rootCmd.AddCommand(auditCmd)
	rootCmd.AddCommand(healthCmd)
	rootCmd.AddCommand(mcpCmd)
	rootCmd.AddCommand(mcpServerCmd)
	rootCmd.AddCommand(prCmd)
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(secretsCmd)
	rootCmd.AddCommand(fixCmd)
	rootCmd.AddCommand(completionCmd)
	rootCmd.AddCommand(hashCmd)
	rootCmd.AddCommand(projectCmd)
}

func main() {
	// Set version information in the CLI package
	cli.SetVersionInfo(version)

	// Set version information in the workflow package for generated file headers
	workflow.SetVersion(version)

	// Set release flag in the workflow package
	workflow.SetIsRelease(isRelease == "true")

	if err := rootCmd.Execute(); err != nil {
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
			fmt.Fprintln(os.Stderr, console.FormatErrorMessage(errMsg))
		}
		os.Exit(1)
	}
}
