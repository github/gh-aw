package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/spf13/cobra"
)

var addLog = logger.New("cli:add_command")

// This file contains only the add command's Cobra wiring and flag handling.

var (
	addCommandLong = `Add one or more agentic workflows from repositories, local files, or URLs to .github/workflows.

This command adds workflows directly without interactive prompts. Use 'add-wizard'
for a guided setup that configures secrets, creates a pull request, and more.

Workflow specifications:
  - Two parts: "owner/repo[@version]" (loads repository-root aw.yml package)
  - Three+ parts without .md: "owner/repo/folder[@version]" (loads nested aw.yml package when present)
  - Three parts: "owner/repo/workflow-name[@version]" (implicitly looks in workflows/ directory)
  - Four+ parts: "owner/repo/workflows/workflow-name.md[@version]" (requires explicit .md extension)
  - GitHub URL: "https://github.com/owner/repo/blob/branch/path/to/workflow.md"
  - Arbitrary URL: "https://example.com/workflow.md" (fetches and dispatches on Content-Type)
    - text/markdown → treated as a gh-aw workflow markdown file
    - application/json → converted from a JSON workflow definition
  - Local file: "./path/to/workflow.md" (adds a workflow from local filesystem)
  - Local wildcard: "./*.md" or "./dir/*.md" (adds all .md files matching pattern)
  - Version can be tag, branch, or SHA (for remote workflows)

The -n flag allows you to specify a custom name for the workflow file (not allowed when adding multiple workflows at once).
The --dir flag allows you to specify the workflow directory (default: .github/workflows).
The --create-pull-request flag creates a pull request with the workflow changes.
The --force flag overwrites existing workflow files.
When a package contains .github/workflows/aw.json, its project settings are
merged into the target repository and the added package settings take precedence.

Note: In GitHub Enterprise repos, shorthand source specs resolve on your enterprise host by default.
      For github/*, githubnext/*, and microsoft/* sources, shorthand resolves on github.com.
      Use full https://github.com/... source URLs for other public github.com workflows.
Note: To create a new workflow from scratch, use the 'new' command instead.
Note: For guided interactive setup, use the 'add-wizard' command instead.`
	addCommandExample = `  ` + string(constants.CLIExtensionPrefix) + ` add githubnext/agentics/daily-repo-status        # Add workflow directly
  ` + string(constants.CLIExtensionPrefix) + ` add githubnext/agentics/repo-assist              # Add package from repository root aw.yml
  ` + string(constants.CLIExtensionPrefix) + ` add githubnext/agentics/packages/repo-assist     # Add package from nested aw.yml
  ` + string(constants.CLIExtensionPrefix) + ` add githubnext/agentics/ci-doctor@v1.0.0         # Add with version
  ` + string(constants.CLIExtensionPrefix) + ` add githubnext/agentics/workflows/ci-doctor.md@main
  ` + string(constants.CLIExtensionPrefix) + ` add https://github.com/githubnext/agentics/blob/main/workflows/ci-doctor.md
  ` + string(constants.CLIExtensionPrefix) + ` add https://example.com/my-workflow.md           # Add workflow from any HTTPS URL
  ` + string(constants.CLIExtensionPrefix) + ` add https://example.com/workflow.json            # Import JSON workflow definition
  ` + string(constants.CLIExtensionPrefix) + ` add githubnext/agentics/ci-doctor --create-pull-request --force
  ` + string(constants.CLIExtensionPrefix) + ` add ./my-workflow.md                             # Add local workflow
  ` + string(constants.CLIExtensionPrefix) + ` add ./*.md                                       # Add all local workflows
  ` + string(constants.CLIExtensionPrefix) + ` add githubnext/agentics/ci-doctor --dir .github/workflows/shared   # Add to .github/workflows/shared/`
)

// AddOptions contains all configuration options for adding workflows
type AddOptions struct {
	Verbose                bool
	Quiet                  bool
	EngineOverride         string
	Name                   string
	Force                  bool
	AppendText             string
	CreatePR               bool
	NoGitattributes        bool
	FromWildcard           bool
	WorkflowDir            string
	NoStopAfter            bool
	StopAfter              string
	DisableSecurityScanner bool
	// RepoSlug is the already-resolved target repository in owner/repo format.
	// When set, PR creation avoids fetching the same repository metadata again.
	RepoSlug string
	// GhAwRef is the resolved github/gh-aw commit SHA used by compiled action references.
	GhAwRef string
	// AddCopilotRequestsPermission injects permissions.copilot-requests: write into
	// the workflow frontmatter, enabling GitHub Actions token auth for Copilot.
	// Set by the add-wizard when the user selects org-billing auth instead of a PAT.
	AddCopilotRequestsPermission bool
	// AddCopilotRequestsNonePermission injects permissions.copilot-requests: none into
	// the workflow frontmatter, explicitly selecting PAT-based Copilot authentication.
	// Set by the add-wizard only when the user actively selects PAT auth.
	AddCopilotRequestsNonePermission bool
	addWizard                        *addWizardOptions
}

type addWizardOptions struct {
	initializedFiles                    []addInitializedFile
	workingTreePrevalidated             bool
	showInteractiveProgress             bool
	secretSource                        secretSource
	skipSecret                          bool
	disableGitHubAppPermissionInference bool
}

func (opts AddOptions) wizardInitializedPaths() []string {
	if opts.addWizard == nil {
		return nil
	}
	paths := make([]string, 0, len(opts.addWizard.initializedFiles))
	for _, file := range opts.addWizard.initializedFiles {
		paths = append(paths, file.path)
	}
	return paths
}

func (opts AddOptions) showInteractiveProgress() bool {
	return opts.addWizard != nil && opts.addWizard.showInteractiveProgress
}

// NewAddCommand creates the add command
func NewAddCommand(validateEngine func(string) error) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "add <workflow>...",
		Short:   "Add agentic workflows from repositories, local files, or URLs to .github/workflows",
		Long:    addCommandLong,
		Example: addCommandExample,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) < 1 {
				return fmt.Errorf("missing workflow specification. Expected at least one workflow source argument. Example: %[1]s githubnext/agentics/daily-repo-status\n\nUsage:\n  %[1]s <workflow>...\n\nExamples:\n  %[1]s githubnext/agentics/daily-repo-status      Add from repository\n  %[1]s ./my-workflow.md                           Add local workflow\n\nRun '%[1]s --help' for more information", cmd.CommandPath())
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAddCommand(cmd, args, validateEngine)
		},
	}

	registerAddCommandFlags(cmd)
	return cmd
}

func runAddCommand(cmd *cobra.Command, args []string, validateEngine func(string) error) error {
	engineOverride, _ := cmd.Flags().GetString("engine")
	nameFlag, _ := cmd.Flags().GetString("name")
	createPRFlag, _ := cmd.Flags().GetBool("create-pull-request")
	prFlagAlias, _ := cmd.Flags().GetBool("pr")
	forceFlag, _ := cmd.Flags().GetBool("force")
	appendText, _ := cmd.Flags().GetString("append")
	verbose, _ := cmd.Flags().GetBool("verbose")
	noGitattributes, _ := cmd.Flags().GetBool("no-gitattributes")
	workflowDir, _ := cmd.Flags().GetString("dir")
	noStopAfter, _ := cmd.Flags().GetBool("no-stop-after")
	stopAfter, _ := cmd.Flags().GetString("stop-after")
	disableSecurityScanner := resolveDeprecatedBoolFlag(cmd, "no-security-scanner", "disable-security-scanner")
	ghAwRef, _ := cmd.Flags().GetString("gh-aw-ref")
	resolvedGhAwRef, err := resolveAddGhAwRef(cmd.Context(), ghAwRef)
	if err != nil {
		return err
	}

	if nameFlag != "" && len(args) > 1 {
		return errors.New("--name was set while multiple workflows were provided. Expected --name only with a single workflow source. Example: gh aw add githubnext/agentics/daily-repo-status --name daily-repo-status")
	}
	if err := validateEngine(engineOverride); err != nil {
		return err
	}
	addLog.Printf("Adding %d workflow source(s): force=%t, create_pr=%t", len(args), forceFlag, createPRFlag || prFlagAlias)

	opts := AddOptions{
		Verbose:                verbose,
		EngineOverride:         engineOverride,
		Name:                   nameFlag,
		Force:                  forceFlag,
		AppendText:             appendText,
		CreatePR:               createPRFlag || prFlagAlias,
		NoGitattributes:        noGitattributes,
		WorkflowDir:            workflowDir,
		NoStopAfter:            noStopAfter,
		StopAfter:              stopAfter,
		DisableSecurityScanner: disableSecurityScanner,
		GhAwRef:                resolvedGhAwRef,
	}
	resolved, err := ResolveWorkflows(cmd.Context(), args, verbose)
	if err != nil {
		return err
	}
	if err := rejectBootstrapProfileForRegularAdd(args, resolved.BootstrapProfile); err != nil {
		return err
	}
	if err := ensureAddRepositoryInitialized(engineOverride, verbose, noGitattributes); err != nil {
		return err
	}
	if _, err := AddResolvedWorkflows(cmd.Context(), args, resolved, opts); err != nil {
		addLog.Printf("Add command failed while installing resolved workflows: %v", err)
		return err
	}
	return nil
}

func rejectBootstrapProfileForRegularAdd(sources []string, profile *resolvedBootstrapProfile) error {
	if profile == nil || profile.Profile == nil || len(profile.Profile.Config) == 0 {
		return nil
	}

	requestedSources := strings.Join(sources, " ")
	if requestedSources == "" {
		requestedSources = profile.PackageID
	}

	return fmt.Errorf("package %s declares aw.yml config, so 'gh aw add' cannot run its interactive setup. Expected interactive setup via add-wizard for packages with aw.yml config. Example: gh aw add-wizard %s", profile.PackageID, requestedSources)
}

func registerAddCommandFlags(cmd *cobra.Command) {
	// Add name flag to add command
	cmd.Flags().StringP("name", "n", "", "Specify name for the added workflow (without .md extension)")

	// Add AI flag to add command
	addEngineFlag(cmd)

	// Add repository flag to add command.
	// Note: the repo is specified directly in the workflow path argument (e.g., "owner/repo/workflow-name"),
	// so this flag is not read by the command. It is kept hidden to avoid breaking existing scripts
	// that may pass --repo but should not be advertised in help text.
	cmd.Flags().StringP("repo", "r", "", "Source repository containing workflows (owner/repo format)")
	_ = cmd.Flags().MarkHidden("repo") // Hidden: repo is already embedded in the workflow path spec

	// Add PR flag to add command (--create-pull-request with --pr as alias)
	cmd.Flags().Bool("create-pull-request", false, "Create a pull request with the workflow changes")
	cmd.Flags().Bool("pr", false, "Alias for --create-pull-request")
	_ = cmd.Flags().MarkHidden("pr") // Hide the short alias from help output

	// Add force flag to add command
	cmd.Flags().BoolP("force", "f", false, "Overwrite existing workflow files without confirmation")

	// Add append flag to add command
	cmd.Flags().String("append", "", "Append extra content to the end of the agentic workflow on installation")

	// Add no-gitattributes flag to add command
	cmd.Flags().Bool("no-gitattributes", false, "Skip updating .gitattributes file")

	// Add workflow directory flag to add command
	cmd.Flags().StringP("dir", "d", "", "Workflow directory (default: $GH_AW_WORKFLOWS_DIR or .github/workflows)")

	// Add no-stop-after flag to add command
	cmd.Flags().Bool("no-stop-after", false, "Remove any stop-after field from the workflow")

	// Add stop-after flag to add command
	cmd.Flags().String("stop-after", "", "Override stop-after value in the workflow (e.g., '+48h', '2025-12-31 23:59:59')")

	// Add no-security-scanner flag to add command (--disable-security-scanner is kept as a deprecated alias)
	addSecurityScannerFlag(cmd)

	cmd.Flags().String("gh-aw-ref", "", "Pin compiled workflows to a branch, tag, or commit SHA of github/gh-aw; branch and tag names are resolved to an immutable full SHA")

	// Register completions for add command
	RegisterEngineFlagCompletion(cmd)
	RegisterDirFlagCompletion(cmd, "dir")
}
