package cli

import (
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/spf13/cobra"
)

var validateLog = logger.New("cli:validate_command")

// NewValidateCommand creates the validate command
func NewValidateCommand(validateEngine func(string) error) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "validate [workflow]...",
		Short: "Validate agentic workflows without generating lock files",
		Long: `Validate one or more agentic workflows by compiling and running all linters without
generating lock files. This is equivalent to:
  gh aw compile --validate --no-emit --zizmor --actionlint --poutine

If no workflows are specified, all Markdown files in .github/workflows will be validated.

` + WorkflowIDExplanation,
		Example: `  ` + string(constants.CLIExtensionPrefix) + ` validate                         # Validate all workflows
  ` + string(constants.CLIExtensionPrefix) + ` validate ci-doctor               # Validate a specific workflow
  ` + string(constants.CLIExtensionPrefix) + ` validate ci-doctor daily         # Validate multiple workflows
  ` + string(constants.CLIExtensionPrefix) + ` validate workflow.md             # Validate by file path
  ` + string(constants.CLIExtensionPrefix) + ` validate --dir custom/workflows  # Validate from custom directory
  ` + string(constants.CLIExtensionPrefix) + ` validate --json                  # Output results in JSON format
  ` + string(constants.CLIExtensionPrefix) + ` validate --strict                # Enforce strict mode validation
  ` + string(constants.CLIExtensionPrefix) + ` validate --fail-fast             # Stop at the first validation error`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runValidateCommand(cmd, args, validateEngine)
		},
	}
	configureValidateCommand(cmd)
	return cmd
}

func runValidateCommand(cmd *cobra.Command, args []string, validateEngine func(string) error) error {
	engineOverride, _ := cmd.Flags().GetString("engine")
	dir, _ := cmd.Flags().GetString("dir")
	strict, _ := cmd.Flags().GetBool("strict")
	jsonOutput, _ := cmd.Flags().GetBool("json")
	failFast, _ := cmd.Flags().GetBool("fail-fast")
	stats, _ := cmd.Flags().GetBool("stats")
	allowActionRefs, _ := cmd.Flags().GetBool("allow-action-refs")
	noCheckUpdate, _ := cmd.Flags().GetBool("no-check-update")
	validateImages, _ := cmd.Flags().GetBool("validate-images")
	verbose, _ := cmd.Flags().GetBool("verbose")

	if err := validateEngine(engineOverride); err != nil {
		return err
	}

	// Check for updates (non-blocking, runs once per day); join before exit
	joinUpdateCheck := CheckForUpdatesAsync(cmd.Context(), noCheckUpdate, verbose)
	defer joinUpdateCheck()

	validateLog.Printf("Running validate command: workflows=%v, dir=%s", args, dir)

	config := CompileConfig{
		MarkdownFiles:   args,
		Verbose:         verbose,
		EngineOverride:  engineOverride,
		Validate:        true,
		NoEmit:          true,
		Zizmor:          true,
		Actionlint:      true,
		Poutine:         true,
		WorkflowDir:     dir,
		Strict:          strict,
		JSONOutput:      jsonOutput,
		FailFast:        failFast,
		Stats:           stats,
		AllowActionRefs: allowActionRefs,
		ValidateImages:  validateImages,
	}
	if _, err := CompileWorkflows(cmd.Context(), config); err != nil {
		return err
	}
	return nil
}

func configureValidateCommand(cmd *cobra.Command) {
	addEngineFlag(cmd)
	cmd.Flags().StringP("dir", "d", "", "Workflow directory (default: $GH_AW_WORKFLOWS_DIR or .github/workflows)")
	cmd.Flags().Bool("strict", false, "Override frontmatter to enforce strict mode validation for all workflows (enforces action pinning, network config, safe-outputs, disallows write permissions and deprecated fields). Note: Workflows default to strict mode unless frontmatter sets strict: false")
	addJSONFlag(cmd)
	cmd.Flags().Bool("fail-fast", false, "Stop at the first validation error instead of collecting all errors")
	cmd.Flags().Bool("validate-images", false, "Require Docker to be available for container image validation. Without this flag, container image validation is silently skipped when Docker is not installed or the daemon is not running")
	cmd.Flags().Bool("stats", false, "Display statistics table sorted by workflow file size (shows jobs, steps, scripts, and shells)")
	cmd.Flags().Bool("allow-action-refs", false, "Allow unresolved action refs and emit warnings instead of failing validation")
	cmd.Flags().Bool("no-check-update", false, "Skip checking for gh-aw updates")

	// Register completions
	cmd.ValidArgsFunction = CompleteWorkflowNames
	RegisterEngineFlagCompletion(cmd)
	RegisterDirFlagCompletion(cmd, "dir")

}
