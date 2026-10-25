package cli

import (
	"errors"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/tty"
	"github.com/spf13/cobra"
)

var addWizardLog = logger.New("cli:add_wizard_command")

// NewAddWizardCommand creates the add-wizard command, which is always interactive.
func NewAddWizardCommand(validateEngine func(string) error) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "add-wizard <workflow>...",
		Short:   "Interactively add one or more agentic workflows with guided setup",
		Long:    addWizardLongDescription(),
		Example: addWizardExamples(),
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) < 1 {
				return errors.New("missing workflow specification\n\nRun 'gh aw add-wizard --help' for usage information")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAddWizardCommand(cmd, args, validateEngine)
		},
	}
	addWizardFlags(cmd)
	return cmd
}

func addWizardLongDescription() string {
	return `Interactively add one or more agentic workflows with guided setup.

This command walks you through:
  - Selecting an AI engine (Copilot, Claude, Codex, Gemini, or Pi)
  - Configuring API keys and secrets
  - Writing the workflow locally or creating a pull request with it
  - Optionally running the workflow immediately

Use 'add' for non-interactive workflow addition.

Workflow specifications:
  - Two parts: "owner/repo[@version]" (loads repository-root aw.yml package)
  - Three+ parts without .md: "owner/repo/folder[@version]" (loads nested aw.yml package when present)
  - Three parts: "owner/repo/workflow-name[@version]" (implicitly looks in workflows/ directory)
  - Four+ parts: "owner/repo/workflows/workflow-name.md[@version]" (requires explicit .md extension)
  - GitHub URL: "https://github.com/owner/repo/blob/branch/path/to/workflow.md"
  - Arbitrary URL: "https://example.com/workflow.md" (fetches and dispatches on Content-Type)
    - text/markdown → treated as a gh-aw workflow markdown file
    - application/json → converted from a JSON workflow definition
  - Local file: "./path/to/workflow.md"
  - Version can be tag, branch, or SHA (for remote workflows)

Note: Requires an interactive terminal. Use 'add' for CI/automation environments.
Note: In GitHub Enterprise repos, shorthand source specs resolve on your enterprise host by default.
      For github/*, githubnext/*, and microsoft/* sources, shorthand resolves on github.com.
      Use full https://github.com/... source URLs for other public github.com workflows.
Note: To create a new workflow from scratch, use the 'new' command instead.`
}

func addWizardExamples() string {
	return `  ` + string(constants.CLIExtensionPrefix) + ` add-wizard githubnext/agentics                    # Guided setup for repository-root aw.yml package
  ` + string(constants.CLIExtensionPrefix) + ` add-wizard githubnext/agentics/packages/repo-assist # Guided setup for nested aw.yml package
  ` + string(constants.CLIExtensionPrefix) + ` add-wizard githubnext/agentics/daily-repo-status    # Guided setup
  ` + string(constants.CLIExtensionPrefix) + ` add-wizard githubnext/agentics/ci-doctor@v1.0.0     # Guided setup with version
  ` + string(constants.CLIExtensionPrefix) + ` add-wizard ./my-workflow.md                         # Guided setup for local workflow
  ` + string(constants.CLIExtensionPrefix) + ` add-wizard https://example.com/my-workflow.md       # Guided setup from any HTTPS URL
  ` + string(constants.CLIExtensionPrefix) + ` add-wizard https://example.com/workflow.json        # Import JSON workflow definition with guided setup
  ` + string(constants.CLIExtensionPrefix) + ` add-wizard githubnext/agentics/ci-doctor --engine copilot   # Pre-select engine
  ` + string(constants.CLIExtensionPrefix) + ` add-wizard githubnext/agentics/ci-doctor --no-secret        # Skip secret prompt
  ` + string(constants.CLIExtensionPrefix) + ` add-wizard githubnext/agentics/ci-doctor --pr --auto-merge  # Use pull request delivery with auto-merge
  ` + string(constants.CLIExtensionPrefix) + ` add-wizard githubnext/agentics/ci-doctor --append "custom footer"            # Append custom content
  ` + string(constants.CLIExtensionPrefix) + ` add-wizard githubnext/agentics/ci-doctor --no-security-scanner             # Skip security scan
  ` + string(constants.CLIExtensionPrefix) + ` add-wizard githubnext/agentics/ci-doctor --no-config # Skip GitHub App permission/event inference from package workflows`
}

func runAddWizardCommand(cmd *cobra.Command, args []string, validateEngine func(string) error) error {
	config, err := addWizardConfigFromFlags(cmd, args)
	if err != nil {
		return err
	}
	addWizardLog.Printf("Starting add-wizard: workflows=%v, engine=%s, verbose=%v", config.WorkflowSpecs, config.EngineOverride, config.Verbose)
	if err := validateEngine(config.EngineOverride); err != nil {
		return err
	}
	// add-wizard requires an interactive terminal
	isTerminal := tty.IsStdoutTerminal()
	isCIEnv := IsRunningInCI()
	addWizardLog.Printf("Terminal check: is_terminal=%v, is_ci=%v", isTerminal, isCIEnv)
	if !isTerminal || isCIEnv {
		return errors.New("add-wizard requires an interactive terminal; use 'add' for non-interactive environments")
	}
	config.PRMessage = cmd.Flag("pr-message").Value.String()
	return RunAddInteractive(cmd.Context(), config)
}

func addWizardConfigFromFlags(cmd *cobra.Command, args []string) (*AddInteractiveConfig, error) {
	workflows := args
	engineOverride, _ := cmd.Flags().GetString("engine")
	verbose, _ := cmd.Flags().GetBool("verbose")
	noGitattributes, _ := cmd.Flags().GetBool("no-gitattributes")
	workflowDir, _ := cmd.Flags().GetString("dir")
	noStopAfter, _ := cmd.Flags().GetBool("no-stop-after")
	stopAfter, _ := cmd.Flags().GetString("stop-after")
	noSecret, _ := cmd.Flags().GetBool("no-secret")
	skipSecretLegacy, _ := cmd.Flags().GetBool("skip-secret")
	skipSecret := noSecret || skipSecretLegacy
	appendText, _ := cmd.Flags().GetString("append")
	disableSecurityScanner := resolveDeprecatedBoolFlag(cmd, "no-security-scanner", "disable-security-scanner")
	noGitHubAppInference, _ := cmd.Flags().GetBool("no-config")
	autoMerge, _ := cmd.Flags().GetBool("auto-merge")
	createPRFlag, _ := cmd.Flags().GetBool("create-pull-request")
	prFlagAlias, _ := cmd.Flags().GetBool("pr")
	createPR := createPRFlag || prFlagAlias
	if err := requireAutoMergePR(autoMerge, createPR); err != nil {
		return nil, err
	}
	ghAwRef, _ := cmd.Flags().GetString("gh-aw-ref")
	resolvedGhAwRef, err := resolveAddGhAwRef(cmd.Context(), ghAwRef)
	if err != nil {
		return nil, err
	}
	return &AddInteractiveConfig{
		WorkflowSpecs:                       workflows,
		Verbose:                             verbose,
		EngineOverride:                      engineOverride,
		NoGitattributes:                     noGitattributes,
		WorkflowDir:                         workflowDir,
		NoStopAfter:                         noStopAfter,
		StopAfter:                           stopAfter,
		SkipSecret:                          skipSecret,
		AppendText:                          appendText,
		DisableSecurityScanner:              disableSecurityScanner,
		DisableGitHubAppPermissionInference: noGitHubAppInference,
		GhAwRef:                             resolvedGhAwRef,
		AutoMerge:                           autoMerge,
		CreatePR:                            createPR,
	}, nil
}

func addWizardFlags(cmd *cobra.Command) {
	// Add AI engine flag
	addEngineFlag(cmd)

	// Add no-gitattributes flag
	cmd.Flags().Bool("no-gitattributes", false, "Skip updating .gitattributes file")

	// Add workflow directory flag
	cmd.Flags().StringP("dir", "d", "", "Workflow directory (default: $GH_AW_WORKFLOWS_DIR or .github/workflows)")

	// Add no-stop-after flag
	cmd.Flags().Bool("no-stop-after", false, "Remove any stop-after field from the workflow")

	// Add stop-after flag
	cmd.Flags().String("stop-after", "", "Override stop-after value in the workflow (e.g., '+48h', '2025-12-31 23:59:59')")

	// Add no-secret flag (--skip-secret is kept as an undocumented alias)
	cmd.Flags().Bool("no-secret", false, "Skip the API secret prompt (use when the secret is already set at the org or repo level)")
	cmd.Flags().Bool("skip-secret", false, "Skip the API secret prompt (use when the secret is already set at the org or repo level)")
	_ = cmd.Flags().MarkHidden("skip-secret")

	// Add append flag (matches --append in add command)
	cmd.Flags().String("append", "", "Append extra content to the end of the agentic workflow on installation")

	// Add no-security-scanner flag (--disable-security-scanner is kept as a deprecated alias
	// for consistency with add and other install entry points)
	addSecurityScannerFlag(cmd)

	// Add no-config flag to allow disabling automatic inference of GitHub App
	// permissions/events from resolved package workflows.
	cmd.Flags().Bool("no-config", false, "Disable inferring GitHub App permissions/events from the package's workflows; use only permissions/events declared in aw.yml")
	cmd.Flags().String("gh-aw-ref", "", "Pin compiled workflows to a branch, tag, or commit SHA of github/gh-aw; branch and tag names are resolved to an immutable full SHA")
	cmd.Flags().Bool("create-pull-request", false, "Create a pull request instead of prompting for delivery")
	addPRMessageFlag(cmd)
	cmd.Flags().Bool("pr", false, "Alias for --create-pull-request")
	_ = cmd.Flags().MarkHidden("pr")
	cmd.Flags().Bool("auto-merge", false, "Enable auto-merge on the created pull request (requires --pr or --create-pull-request)")

	// Register completions
	RegisterEngineFlagCompletion(cmd)
	RegisterDirFlagCompletion(cmd, "dir")

}
