package cli

import (
	"fmt"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/spf13/cobra"
)

// NewTrialCommand creates the trial command
func NewTrialCommand(validateEngine func(string) error) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "trial <workflow-spec>...",
		Short: "Run one or more agentic workflows in trial mode against a simulated repository",
		Long: `Run one or more agentic workflows in trial mode against a simulated repository.

This command creates a temporary private repository in your GitHub account, installs the specified
workflows from their source repositories, and runs them in "trial mode" to capture safe outputs without
making actual changes to the simulated host repository.

Repository modes:
- Default mode (no flags): Creates a temporary trial repository and simulates execution as if running against the current repository (github.repository context points to the current repository)
- --logical-repo REPO: Simulates execution against a specified repository (github.repository context points to REPO while actually running in a temporary trial repository)
- --host-repo REPO: Uses the specified repository as the host for trial execution instead of creating a temporary one
- --clone-repo REPO: Clones the specified repository's contents into the trial repository before execution (useful for testing against actual repository state)

All workflows must support the workflow_dispatch trigger to be used in trial mode.
The host repository will be created as a private repository and retained by default unless --delete-host-repo-after is specified.
Trial results are saved both locally (in the trials/ directory) and in the host repository for future reference.`,
		Example: `  ` + string(constants.CLIExtensionPrefix) + ` trial githubnext/agentics/weekly-research                         # Run a single workflow in a temporary trial repository
  ` + string(constants.CLIExtensionPrefix) + ` trial githubnext/agentics/daily-plan githubnext/agentics/weekly-research # Compare multiple workflows
  ` + string(constants.CLIExtensionPrefix) + ` trial githubnext/agentics/daily-plan myorg/myrepo/custom-workflow # Run workflows from different repositories
  ` + string(constants.CLIExtensionPrefix) + ` trial githubnext/agentics/my-workflow --host-repo myorg/myrepo    # Use an existing host repository
  ` + string(constants.CLIExtensionPrefix) + ` trial githubnext/agentics/my-workflow --logical-repo myorg/myrepo # Simulate a different github.repository value
  ` + string(constants.CLIExtensionPrefix) + ` trial githubnext/agentics/my-workflow --clone-repo myorg/myrepo   # Clone repository contents into the trial host
  ` + string(constants.CLIExtensionPrefix) + ` trial githubnext/agentics/my-workflow --repeat 3                  # Run 4 times total (1 initial + 3 repeats)
  ` + string(constants.CLIExtensionPrefix) + ` trial githubnext/agentics/my-workflow --delete-host-repo-after    # Delete the trial host repository when done
  ` + string(constants.CLIExtensionPrefix) + ` trial githubnext/agentics/my-workflow --dry-run                   # Preview changes without executing
  ` + string(constants.CLIExtensionPrefix) + ` trial githubnext/agentics/my-workflow --json                      # Output trial results in JSON format
  ` + string(constants.CLIExtensionPrefix) + ` trial githubnext/agentics/my-workflow --auto-merge-prs            # Auto-merge PRs created during the trial
  ` + string(constants.CLIExtensionPrefix) + ` trial githubnext/agentics/my-workflow --host-repo .               # Use the current repository as the host
  ` + string(constants.CLIExtensionPrefix) + ` trial ./local-workflow.md --clone-repo upstream/repo --repeat 2   # Run a local workflow against cloned contents
  ` + string(constants.CLIExtensionPrefix) + ` trial githubnext/agentics/my-workflow --trigger-context https://github.com/owner/repo/issues/123 # Provide issue context for issue-triggered workflows`,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) < 1 {
				return fmt.Errorf("missing workflow specification\n\nUsage:\n  %s <workflow-spec>...\n\nExample:\n  %[1]s githubnext/agentics/daily-plan             Trial a workflow from a repository\n  %[1]s ./local-workflow.md                         Trial a local workflow\n\nRun '%[1]s --help' for more information", cmd.CommandPath())
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTrialCommand(cmd, args, validateEngine)
		},
	}
	configureTrialCommandFlags(cmd)
	return cmd
}

func runTrialCommand(cmd *cobra.Command, args []string, validateEngine func(string) error) error {
	workflowSpecs := args
	trialLog.Printf("Trial command invoked: workflow_count=%d", len(workflowSpecs))
	logicalRepoSpec, _ := cmd.Flags().GetString("logical-repo")
	cloneRepoSpec, _ := cmd.Flags().GetString("clone-repo")
	hostRepoSpec, _ := cmd.Flags().GetString("host-repo")
	deleteHostRepo, _ := cmd.Flags().GetBool("delete-host-repo-after")
	forceDeleteHostRepo := resolveDeprecatedBoolFlag(cmd, "delete-host-repo-before", "force-delete-host-repo-before")
	yes, _ := cmd.Flags().GetBool("yes")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	jsonOutput, _ := cmd.Flags().GetBool("json")
	timeout, _ := cmd.Flags().GetInt("timeout")
	triggerContext, _ := cmd.Flags().GetString("trigger-context")
	repeatCount, _ := cmd.Flags().GetInt("repeat")
	autoMergePRs, _ := cmd.Flags().GetBool("auto-merge-prs")
	engineOverride, _ := cmd.Flags().GetString("engine")
	appendText, _ := cmd.Flags().GetString("append")
	verbose, _ := cmd.Root().PersistentFlags().GetBool("verbose")
	disableSecurityScanner := resolveDeprecatedBoolFlag(cmd, "no-security-scanner", "disable-security-scanner")

	if err := validateEngine(engineOverride); err != nil {
		trialLog.Printf("Engine validation failed: engine=%s, err=%v", engineOverride, err)
		return err
	}
	if trialLog.Enabled() {
		trialLog.Printf("Trial options: dry_run=%v, repeat=%d, timeout_min=%d, auto_merge_prs=%v, logical_repo=%q, clone_repo=%q, host_repo=%q",
			dryRun, repeatCount, timeout, autoMergePRs, logicalRepoSpec, cloneRepoSpec, hostRepoSpec)
	}
	opts := TrialOptions{
		Repos: TrialRepoContext{
			LogicalRepo: logicalRepoSpec,
			CloneRepo:   cloneRepoSpec,
			HostRepo:    hostRepoSpec,
		},
		DeleteHostRepo:         deleteHostRepo,
		ForceDelete:            forceDeleteHostRepo,
		Quiet:                  yes,
		DryRun:                 dryRun,
		JSONOutput:             jsonOutput,
		TimeoutMinutes:         timeout,
		TriggerContext:         triggerContext,
		RepeatCount:            repeatCount,
		AutoMergePRs:           autoMergePRs,
		EngineOverride:         engineOverride,
		AppendText:             appendText,
		Verbose:                verbose,
		DisableSecurityScanner: disableSecurityScanner,
	}

	if err := RunWorkflowTrials(cmd.Context(), workflowSpecs, opts); err != nil {
		return err
	}
	return nil
}

func configureTrialCommandFlags(cmd *cobra.Command) {
	// Add flags
	cmd.Flags().StringP("logical-repo", "l", "", "Repository to simulate workflow execution against, as if the workflow was installed there (defaults to current repository)")
	cmd.Flags().String("clone-repo", "", "Clone the contents of the specified repository into the host repository before execution (useful for testing against actual repository state)")

	cmd.Flags().String("host-repo", "", "Custom host repository slug (defaults to '<username>/gh-aw-trial'). Use '.' for current repository")
	cmd.Flags().Bool("delete-host-repo-after", false, "Delete the host repository after completion (retained by default)")
	cmd.Flags().Bool("delete-host-repo-before", false, "Delete the host repository before creation if it already exists")
	cmd.Flags().Bool("force-delete-host-repo-before", false, "Delete the host repository before creation if it already exists")
	_ = cmd.Flags().MarkDeprecated("force-delete-host-repo-before", "use --delete-host-repo-before instead")
	cmd.Flags().BoolP("yes", "y", false, "Auto-accept trial confirmations (required in CI)")
	cmd.Flags().Bool("dry-run", false, "Preview trial execution without creating repos or running workflows")
	cmd.Flags().Int("timeout", 30, "Execution timeout in minutes (0 = no timeout)")
	cmd.Flags().String("trigger-context", "", "Trigger context URL (e.g., GitHub issue URL) for issue-triggered workflows")
	cmd.Flags().Int("repeat", 0, "Number of additional times to run after the initial execution (e.g., --repeat 3 runs 4 times total)")
	cmd.Flags().Bool("auto-merge-prs", false, "Auto-merge any pull requests created during trial execution")
	addEngineFlag(cmd)
	addJSONFlag(cmd)
	cmd.Flags().String("append", "", "Append extra content to the end of the agentic workflow on installation")
	addSecurityScannerFlag(cmd)
	cmd.MarkFlagsMutuallyExclusive("logical-repo", "clone-repo")

}
