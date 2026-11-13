package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/gitutil"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/spf13/cobra"
)

var deployLog = logger.New("cli:deploy_command")
var runDeployFn = runDeploy
var runDeployForOrgFn = runDeployForOrg

const defaultDeployCooldown = "7d"
const deployCommitMessage = "chore: deploy agentic workflows"

// NewDeployCommand creates the deploy command.
func NewDeployCommand(validateEngine func(string) error) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "deploy <workflow>...",
		Short: "Deploy agentic workflows to a target repository using a pull request",
		Long: `Deploy one or more workflows to a target repository by cloning the target repository, updating existing workflows, adding specified workflows, compiling lock files, and opening a pull request.

The command clones the target repository, updates existing workflows from source, adds the specified workflows, recompiles lock files with purge enabled, and opens a pull request. This command always creates a pull request. The --create-pull-request and --pr flags are compatibility aliases and do not change this behavior; one is required with --auto-merge to explicitly enable auto-merge on the created pull request.`,
		Example: `  ` + string(constants.CLIExtensionPrefix) + ` deploy githubnext/agentics/ci-doctor --repo owner/repo
  ` + string(constants.CLIExtensionPrefix) + ` deploy githubnext/agentics/repo-assist githubnext/agentics/ci-doctor --repo owner/repo --force
  ` + string(constants.CLIExtensionPrefix) + ` deploy ./my-workflow.md --repo owner/repo
  ` + string(constants.CLIExtensionPrefix) + ` deploy ./my-workflow.md --repo owner/repo --create-pull-request --auto-merge
  ` + string(constants.CLIExtensionPrefix) + ` deploy githubnext/agentics/ci-doctor --org my-org --yes
  ` + string(constants.CLIExtensionPrefix) + ` deploy githubnext/agentics/ci-doctor --org my-org --repos '*-service' --yes`,
		Args: validateDeployArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDeployCommand(cmd, args, validateEngine)
		},
	}

	registerDeployFlags(cmd)

	return cmd
}

func runDeploy(ctx context.Context, targetRepo string, workflows []string, addOpts AddOptions, coolDown time.Duration) error {
	checkoutDir, originalDir, err := prepareDeployCheckout(ctx, targetRepo)
	if err != nil {
		return err
	}
	defer func() {
		_ = os.Chdir(originalDir)
	}()

	resolvedWorkflows := resolveDeployWorkflowSpecs(workflows, originalDir)

	if err := os.Chdir(checkoutDir); err != nil {
		return fmt.Errorf("failed to change directory to checkout %s: %w", checkoutDir, err)
	}

	summary, err := newUpdatePRSummary(ctx, addOpts.WorkflowDir)
	if err != nil {
		return err
	}

	if err := runDeployUpdatePass(ctx, addOpts, coolDown); err != nil {
		return err
	}

	if err := runDeployAddPass(ctx, resolvedWorkflows, addOpts); err != nil {
		return err
	}

	if err := runDeployCompilePass(ctx, addOpts); err != nil {
		return err
	}

	details, err := summary.body(ctx, "deploy")
	if err != nil {
		return err
	}
	if err := createDeployPR(ctx, resolvedWorkflows, targetRepo, addOpts.Verbose, addOpts.AutoMerge, details, addOpts.PRMessage); err != nil {
		return err
	}

	deployLog.Printf("Successfully deployed workflows to %s", targetRepo)
	return nil
}

func validateDeployArgs(cmd *cobra.Command, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("missing workflow specification\n\nUsage:\n  %s <workflow>...\n\nExamples:\n  %[1]s githubnext/agentics/ci-doctor --repo owner/repo\n\nRun '%[1]s --help' for more information", cmd.CommandPath())
	}
	return nil
}

func registerDeployFlags(cmd *cobra.Command) {
	cmd.Flags().StringP("repo", "r", "", "Target repository ([HOST/]owner/repo format). Required unless --org is provided")
	cmd.Flags().StringP("name", "n", "", "Specify name for the added workflow (without .md extension)")
	addEngineFlag(cmd)
	cmd.Flags().BoolP("force", "f", false, "Overwrite existing workflow files without confirmation")
	cmd.Flags().Bool("create-pull-request", false, "Create a pull request (always enabled for deploy)")
	addPRMessageFlag(cmd)
	cmd.Flags().Bool("pr", false, "Alias for --create-pull-request")
	_ = cmd.Flags().MarkHidden("pr")
	cmd.Flags().Bool("auto-merge", false, "Enable auto-merge on the deployment pull request (requires --create-pull-request)")
	cmd.Flags().String("append", "", "Append extra content to the end of the agentic workflow on installation")
	cmd.Flags().Bool("no-gitattributes", false, "Skip updating .gitattributes file")
	cmd.Flags().StringP("dir", "d", "", "Workflow directory (default: $GH_AW_WORKFLOWS_DIR or .github/workflows)")
	cmd.Flags().Bool("no-stop-after", false, "Remove any stop-after field from the workflow")
	cmd.Flags().String("stop-after", "", "Override stop-after value in the workflow (e.g., '+48h', '2025-12-31 23:59:59')")
	addSecurityScannerFlag(cmd)
	cmd.Flags().String("cool-down", defaultDeployCooldown, coolDownFlagUsage)
	cmd.Flags().String("org", "", "Deploy workflows across repositories in an organization")
	cmd.Flags().StringSlice("repos", nil, "Limit --org mode to repositories matching one or more glob patterns")
	cmd.Flags().BoolP("yes", "y", false, "Auto-accept org-mode deploy confirmations (required in CI)")

	RegisterEngineFlagCompletion(cmd)
	RegisterDirFlagCompletion(cmd, "dir")
}

func runDeployCommand(cmd *cobra.Command, workflows []string, validateEngine func(string) error) error {
	targetRepo, _ := cmd.Flags().GetString("repo")
	targetOrg, _ := cmd.Flags().GetString("org")
	repoGlobs, _ := cmd.Flags().GetStringSlice("repos")
	yes, _ := cmd.Flags().GetBool("yes")

	opts, coolDown, err := parseDeployCommandOptions(cmd, workflows, validateEngine)
	if err != nil {
		return err
	}

	if strings.TrimSpace(targetRepo) != "" && strings.TrimSpace(targetOrg) != "" {
		return errors.New("cannot specify both --repo and --org")
	}
	if len(repoGlobs) > 0 && strings.TrimSpace(targetOrg) == "" {
		return errors.New("--repos requires --org to be specified")
	}
	if strings.TrimSpace(targetRepo) == "" && strings.TrimSpace(targetOrg) == "" {
		return errors.New("either --repo (owner/repo) or --org must be provided")
	}

	if strings.TrimSpace(targetOrg) != "" {
		return runDeployForOrgFn(cmd.Context(), targetOrg, repoGlobs, workflows, opts, coolDown, yes, opts.Verbose)
	}

	return runDeployFn(cmd.Context(), targetRepo, workflows, opts, coolDown)
}

func parseDeployCommandOptions(cmd *cobra.Command, workflows []string, validateEngine func(string) error) (AddOptions, time.Duration, error) {
	engineOverride, _ := cmd.Flags().GetString("engine")
	nameFlag, _ := cmd.Flags().GetString("name")
	forceFlag, _ := cmd.Flags().GetBool("force")
	autoMerge, _ := cmd.Flags().GetBool("auto-merge")
	createPRFlag, _ := cmd.Flags().GetBool("create-pull-request")
	prFlagAlias, _ := cmd.Flags().GetBool("pr")
	if err := requireAutoMergePR(autoMerge, createPRFlag || prFlagAlias); err != nil {
		return AddOptions{}, 0, err
	}
	appendText, _ := cmd.Flags().GetString("append")
	verbose, _ := cmd.Flags().GetBool("verbose")
	noGitattributes, _ := cmd.Flags().GetBool("no-gitattributes")
	workflowDir, _ := cmd.Flags().GetString("dir")
	noStopAfter, _ := cmd.Flags().GetBool("no-stop-after")
	stopAfter, _ := cmd.Flags().GetString("stop-after")
	disableSecurityScanner := resolveDeprecatedBoolFlag(cmd, "no-security-scanner", "disable-security-scanner")
	coolDownStr, _ := cmd.Flags().GetString("cool-down")

	if nameFlag != "" && len(workflows) > 1 {
		return AddOptions{}, 0, errors.New("--name flag cannot be used when adding multiple workflows at once")
	}
	if err := validateEngine(engineOverride); err != nil {
		return AddOptions{}, 0, err
	}

	coolDown, err := parseCoolDownFlag(coolDownStr)
	if err != nil {
		return AddOptions{}, 0, fmt.Errorf("invalid --cool-down value: %w", err)
	}

	return AddOptions{
		Verbose:                verbose,
		EngineOverride:         engineOverride,
		Name:                   nameFlag,
		Force:                  forceFlag,
		AutoMerge:              autoMerge,
		PRMessage:              cmd.Flag("pr-message").Value.String(),
		AppendText:             appendText,
		NoGitattributes:        noGitattributes,
		WorkflowDir:            workflowDir,
		NoStopAfter:            noStopAfter,
		StopAfter:              stopAfter,
		DisableSecurityScanner: disableSecurityScanner,
	}, coolDown, nil
}

func prepareDeployCheckout(ctx context.Context, targetRepo string) (string, string, error) {
	gitRoot, err := gitutil.FindGitRoot()
	if err != nil {
		return "", "", fmt.Errorf("deploy command requires running inside a git repository: %w", err)
	}

	updatesDir, err := ensureUpdateTargetRepoGitignore(gitRoot)
	if err != nil {
		return "", "", err
	}

	checkoutDir := filepath.Join(updatesDir, sanitizeRepoPath(targetRepo))
	if err := shallowCloneTargetRepo(ctx, targetRepo, checkoutDir); err != nil {
		return "", "", err
	}

	originalDir, err := os.Getwd()
	if err != nil {
		return "", "", fmt.Errorf("failed to read current directory: %w", err)
	}

	return checkoutDir, originalDir, nil
}

func runDeployUpdatePass(ctx context.Context, addOpts AddOptions, coolDown time.Duration) error {
	if err := PreflightCheckForCreatePR(addOpts.Verbose); err != nil {
		return err
	}

	updateOpts := UpdateWorkflowsOptions{
		Verbose:                addOpts.Verbose,
		EngineOverride:         addOpts.EngineOverride,
		WorkflowsDir:           addOpts.WorkflowDir,
		NoStopAfter:            addOpts.NoStopAfter,
		StopAfter:              addOpts.StopAfter,
		DisableSecurityScanner: addOpts.DisableSecurityScanner,
		CoolDown:               coolDown,
	}
	if err := RunUpdateWorkflows(ctx, updateOpts); err != nil {
		return fmt.Errorf("failed to update existing workflows: %w", err)
	}
	return nil
}

func runDeployAddPass(ctx context.Context, resolvedWorkflows []string, addOpts AddOptions) error {
	workflowsToAdd, skippedWorkflows, err := excludeExistingSourcedWorkflows(resolvedWorkflows, addOpts)
	if err != nil {
		return fmt.Errorf("failed to inspect existing workflows: %w", err)
	}
	if len(skippedWorkflows) > 0 {
		deployLog.Printf("Skipping add for existing sourced workflows (already handled by update): %s", strings.Join(skippedWorkflows, ", "))
	}
	if len(workflowsToAdd) == 0 {
		deployLog.Print("No new workflows to add after update pass")
		return nil
	}

	// Deploy creates its PR after all update, add, and compile passes complete.
	addOpts.CreatePR = false
	addOpts.AutoMerge = false
	if _, err := AddWorkflows(ctx, workflowsToAdd, addOpts); err != nil {
		return fmt.Errorf("failed to add workflows: %w", err)
	}
	return nil
}

func runDeployCompilePass(ctx context.Context, addOpts AddOptions) error {
	compileConfig := CompileConfig{
		Verbose:        addOpts.Verbose,
		EngineOverride: addOpts.EngineOverride,
		WorkflowDir:    addOpts.WorkflowDir,
		Purge:          true,
	}
	if _, err := CompileWorkflows(ctx, compileConfig); err != nil {
		return fmt.Errorf("failed to compile workflows with purge: %w", err)
	}
	return nil
}

func createDeployPR(ctx context.Context, resolvedWorkflows []string, targetRepo string, verbose bool, autoMerge bool, details, prMessage string) error {
	prTitle, prBody := buildDeployPRMetadata(resolvedWorkflows, targetRepo)
	prBody = appendPRMessage(prBody, details)
	prBody = appendPRMessage(prBody, prMessage)
	_, err := createPRWithChanges(ctx, "deploy-workflows", deployCommitMessage, prTitle, prBody, verbose, autoMerge)
	if err != nil {
		return fmt.Errorf("failed to create deploy pull request: %w", err)
	}
	return nil
}

// resolveDeployWorkflowSpecs rewrites relative local workflow specs to be anchored
// to the caller's original working directory before deploy switches into checkoutDir.
// Remote specs and already-absolute local paths are preserved unchanged.
func resolveDeployWorkflowSpecs(workflows []string, baseDir string) []string {
	resolved := make([]string, 0, len(workflows))
	for _, workflow := range workflows {
		if !isLocalWorkflowPath(workflow) || filepath.IsAbs(workflow) {
			resolved = append(resolved, workflow)
			continue
		}

		resolved = append(resolved, filepath.Join(baseDir, workflow))
	}

	return resolved
}

func buildDeployPRMetadata(workflows []string, targetRepo string) (string, string) {
	workflowDescription := normalizeWorkflowID(workflows[0])
	if len(workflows) > 1 {
		workflowDescription = fmt.Sprintf("%d workflows", len(workflows))
	}
	var body strings.Builder
	body.WriteString("## Summary\n\nDeploy " + markdownText(workflowDescription) + " to " + markdownText(targetRepo) + ".\n\n")
	body.WriteString("## Workflows requested\n\n")
	for _, spec := range workflows {
		if isLocalWorkflowPath(spec) {
			spec = filepath.Base(spec)
		}
		body.WriteString("- " + summaryCode(spec) + "\n")
	}
	return deployCommitMessage, body.String()
}

func excludeExistingSourcedWorkflows(workflows []string, addOpts AddOptions) ([]string, []string, error) {
	workflowsDir := addOpts.WorkflowDir
	if workflowsDir == "" {
		workflowsDir = getWorkflowsDir()
	}

	workflowsToAdd := make([]string, 0, len(workflows))
	skippedWorkflows := make([]string, 0)

	for _, workflowSpec := range workflows {
		workflowName := normalizeWorkflowID(workflowSpec)
		if addOpts.Name != "" && len(workflows) == 1 {
			workflowName = normalizeWorkflowID(addOpts.Name)
		}

		existingPath := filepath.Join(workflowsDir, workflowName+".md")
		hasSource, err := existingWorkflowHasSource(existingPath)
		if err != nil {
			return nil, nil, err
		}

		if hasSource {
			skippedWorkflows = append(skippedWorkflows, workflowName)
			continue
		}

		workflowsToAdd = append(workflowsToAdd, workflowSpec)
	}

	return workflowsToAdd, skippedWorkflows, nil
}

func existingWorkflowHasSource(path string) (bool, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("failed to read workflow %s: %w", path, err)
	}

	result, err := parser.ExtractFrontmatterFromContent(string(content))
	if err != nil {
		deployLog.Printf("Failed to parse frontmatter in %s while checking source field: %v (treating workflow as non-sourced and proceeding with add)", path, err)
		return false, nil
	}

	sourceValue, ok := result.Frontmatter["source"]
	if !ok {
		return false, nil
	}

	source, ok := sourceValue.(string)
	return ok && strings.TrimSpace(source) != "", nil
}
