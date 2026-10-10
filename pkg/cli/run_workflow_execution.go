package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/github/gh-aw/pkg/workflow"
)

var executionLog = logger.New("cli:run_workflow_execution")

// workflowCompletionWaitTimeoutMinutes matches the GitHub Actions maximum job runtime.
const workflowCompletionWaitTimeoutMinutes = 6 * 60

// betweenWorkflowsDelay paces sequential workflow triggers to avoid overwhelming the GitHub API.
const betweenWorkflowsDelay = 1 * time.Second

// RunOptions contains all configuration options for running workflows
type RunOptions struct {
	Enable            bool     // Enable the workflow if it's disabled
	EngineOverride    string   // Override AI engine
	RepoOverride      string   // Target repository (owner/repo format)
	RefOverride       string   // Branch or tag name
	AutoMergePRs      bool     // Auto-merge PRs created during execution
	Push              bool     // Commit and push workflow files before running
	WaitForCompletion bool     // Wait for workflow completion
	RepeatCount       int      // Number of times to repeat (0 = run once)
	Inputs            []string // Workflow inputs in key=value format
	Verbose           bool     // Enable verbose output
	DryRun            bool     // Validate without actually triggering
	JSON              bool     // Output results in JSON format
	Approve           bool     // Approve safe update changes during compilation
}

// WorkflowRunResult contains the result of a single workflow run trigger for JSON output
type WorkflowRunResult struct {
	Workflow string `json:"workflow"`
	LockFile string `json:"lock_file"`
	Status   string `json:"status"` // "triggered", "dry_run", "error"
	RunID    int64  `json:"run_id,omitempty"`
	RunURL   string `json:"run_url,omitempty"`
	Error    string `json:"error,omitempty"`
}

type workflowEnableState struct {
	wasDisabled bool
	workflowID  int64
}

type workflowRunPreparation struct {
	enableState  workflowEnableState
	normalizedID string
	lockFileName string
	lockFilePath string
}

type workflowRunExecutionResult struct {
	runInfo    *WorkflowRunInfo
	runInfoErr error
}

// RunWorkflowOnGitHub runs an agentic workflow on GitHub Actions
func RunWorkflowOnGitHub(ctx context.Context, workflowIdOrName string, opts RunOptions) error {
	executionLog.Printf("Starting workflow run: workflow=%s, enable=%v, engineOverride=%s, repo=%s, ref=%s, push=%v, wait=%v, inputs=%v", workflowIdOrName, opts.Enable, opts.EngineOverride, opts.RepoOverride, opts.RefOverride, opts.Push, opts.WaitForCompletion, opts.Inputs)
	if err := checkWorkflowRunContext(ctx, workflowIdOrName); err != nil {
		return err
	}
	if err := validateRunInputs(opts.Inputs); err != nil {
		return err
	}
	if opts.Verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Running workflow on GitHub Actions: "+workflowIdOrName))
	}
	if !isGHCLIAvailable() {
		return errors.New("GitHub CLI (gh) is not available. Expected gh to be installed and on PATH before running workflows. Example: brew install gh")
	}
	prep, err := prepareWorkflowRun(ctx, workflowIdOrName, opts)
	if err != nil {
		return err
	}
	defer restoreEnabledWorkflow(workflowIdOrName, opts, prep.enableState)
	args, ref := buildWorkflowRunArgs(prep.lockFileName, opts)
	workflowStartTime := time.Now()
	if opts.DryRun {
		return handleWorkflowDryRun(prep.lockFileName, args, opts)
	}
	runResult, err := executeWorkflowRun(ctx, prep.lockFileName, args, opts)
	if err != nil {
		return err
	}
	handleWorkflowRunInfo(runResult.runInfo, runResult.runInfoErr, opts)
	return waitForWorkflowRunCompletion(ctx, opts, runResult.runInfo, runResult.runInfoErr, workflowStartTime, ref)
}

func checkWorkflowRunContext(ctx context.Context, workflowIdOrName string) error {
	select {
	case <-ctx.Done():
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Operation cancelled"))
		return ctx.Err()
	default:
	}
	if workflowIdOrName == "" {
		return errors.New("workflow name or ID is missing. Expected a workflow file name or numeric workflow ID. Example: gh aw run ci")
	}
	return nil
}

func validateRunInputs(inputs []string) error {
	for _, input := range inputs {
		if !strings.Contains(input, "=") {
			return fmt.Errorf("input '%s' is not in key=value format. Expected each --input value as key=value. Example: --input environment=staging", input)
		}
		if parts := strings.SplitN(input, "=", 2); parts[0] == "" {
			return fmt.Errorf("input '%s' has an empty key before '='. Expected a non-empty key in key=value format. Example: --input environment=staging", input)
		}
	}
	return nil
}

func prepareWorkflowRun(ctx context.Context, workflowIdOrName string, opts RunOptions) (*workflowRunPreparation, error) {
	if err := validateWorkflowForRun(workflowIdOrName, opts); err != nil {
		return nil, err
	}
	enableState, err := handleWorkflowEnablement(ctx, workflowIdOrName, opts)
	if err != nil {
		return nil, err
	}
	prep := &workflowRunPreparation{
		enableState:  enableState,
		normalizedID: normalizeWorkflowID(workflowIdOrName),
	}
	prep.lockFileName = prep.normalizedID + ".lock.yml"
	prep.lockFilePath, err = resolveWorkflowLockFile(prep.normalizedID, prep.lockFileName, opts.RepoOverride)
	if err != nil {
		return nil, err
	}
	if err := applyLocalWorkflowOverrides(ctx, workflowIdOrName, prep, opts); err != nil {
		return nil, err
	}
	return prep, nil
}

func validateWorkflowForRun(workflowIdOrName string, opts RunOptions) error {
	if opts.RepoOverride != "" {
		executionLog.Printf("Validating remote workflow: %s in repo %s", workflowIdOrName, opts.RepoOverride)
		if err := validateRemoteWorkflow(workflowIdOrName, opts.RepoOverride, opts.Verbose); err != nil {
			return fmt.Errorf("failed to validate remote workflow: %w", err)
		}
		return nil
	}
	return validateLocalWorkflowForRun(workflowIdOrName, opts.Inputs, opts.Verbose)
}

func validateLocalWorkflowForRun(workflowIdOrName string, inputs []string, verbose bool) error {
	executionLog.Printf("Validating local workflow: %s", workflowIdOrName)
	workflowFile, err := resolveWorkflowFile(workflowIdOrName, verbose)
	if err != nil {
		return err
	}
	if err := ensureWorkflowRunnable(workflowFile, workflowIdOrName); err != nil {
		return err
	}
	if err := validateWorkflowInputs(workflowFile, inputs); err != nil {
		return fmt.Errorf("%w", err)
	}
	warnLocalWorkflowStatus(workflowFile)
	return nil
}

func ensureWorkflowRunnable(workflowFile, workflowIdOrName string) error {
	runnable, err := IsRunnable(workflowFile)
	if err != nil {
		return fmt.Errorf("failed to check if workflow %s is runnable: %w", workflowFile, err)
	}
	if !runnable {
		return fmt.Errorf("workflow '%s' does not declare a workflow_dispatch trigger, so it cannot be run manually on GitHub Actions. Expected an `on: workflow_dispatch` trigger in the source workflow frontmatter, then recompile. Example:\non:\n  workflow_dispatch:\n\nRun: gh aw compile", workflowIdOrName)
	}
	executionLog.Printf("Workflow is runnable: %s", workflowFile)
	return nil
}

func warnLocalWorkflowStatus(workflowFile string) {
	status, err := checkWorkflowFileStatus(workflowFile)
	if err != nil || status == nil {
		return
	}
	var warnings []string
	if status.IsModified {
		warnings = append(warnings, "The workflow file has unstaged changes")
	}
	if status.IsStaged {
		warnings = append(warnings, "The workflow file has staged changes")
	}
	if status.HasUnpushedCommits {
		warnings = append(warnings, "The workflow file has unpushed commits")
	}
	if len(warnings) == 0 {
		return
	}
	fmt.Fprintln(os.Stderr, console.FormatWarningMessage(strings.Join(warnings, ", ")))
	fmt.Fprintln(os.Stderr, console.FormatWarningMessage("These changes will not be reflected in the GitHub Actions run"))
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Consider pushing your changes before running the workflow"))
}

func handleWorkflowEnablement(ctx context.Context, workflowIdOrName string, opts RunOptions) (workflowEnableState, error) {
	if !opts.Enable {
		return workflowEnableState{}, nil
	}
	wf, err := getWorkflowStatus(ctx, workflowIdOrName, opts.RepoOverride, opts.Verbose)
	if err != nil {
		if opts.Verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Could not check workflow status: %v", err)))
		}
		return workflowEnableState{}, nil
	}
	state := workflowEnableState{workflowID: wf.ID}
	if wf.State != "disabled_manually" {
		executionLog.Printf("Workflow %s is already enabled (state=%s)", workflowIdOrName, wf.State)
		return state, nil
	}
	state.wasDisabled = true
	executionLog.Printf("Workflow %s is disabled, temporarily enabling for this run (id=%d)", workflowIdOrName, wf.ID)
	if opts.Verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Workflow '%s' is disabled, enabling it temporarily...", workflowIdOrName)))
	}
	if err := enableWorkflowForRun(wf.ID, opts.RepoOverride); err != nil {
		return workflowEnableState{}, fmt.Errorf("failed to enable workflow '%s': %w", workflowIdOrName, err)
	}
	fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Enabled workflow: "+workflowIdOrName))
	return state, nil
}

func enableWorkflowForRun(workflowID int64, repoOverride string) error {
	args := []string{"workflow", "enable", strconv.FormatInt(workflowID, 10)}
	if repoOverride != "" {
		args = append(args, "--repo", repoOverride)
	}
	return workflow.ExecGH(args...).Run()
}

func resolveWorkflowLockFile(normalizedID, lockFileName, repoOverride string) (string, error) {
	if repoOverride != "" {
		return "", nil
	}
	workflowsDir := getWorkflowsDir()
	if _, _, err := readWorkflowFile(normalizedID+".md", workflowsDir); err != nil {
		return "", fmt.Errorf("failed to find workflow in local %s: %w", workflowsDir, err)
	}
	lockFilePath := filepath.Join(constants.GetWorkflowDir(), lockFileName)
	if _, err := os.Stat(lockFilePath); os.IsNotExist(err) {
		suggestions := []string{
			fmt.Sprintf("Run '%s compile' to compile all workflows", string(constants.CLIExtensionPrefix)),
			fmt.Sprintf("Run '%s compile %s' to compile this specific workflow", string(constants.CLIExtensionPrefix), normalizedID),
		}
		return "", errors.New(console.FormatErrorWithSuggestions(
			fmt.Sprintf("workflow lock file '%s' not found in %s", lockFileName, constants.GetWorkflowDir()),
			suggestions,
		))
	}
	executionLog.Printf("Found lock file: %s", lockFilePath)
	return lockFilePath, nil
}

func applyLocalWorkflowOverrides(ctx context.Context, workflowIdOrName string, prep *workflowRunPreparation, opts RunOptions) error {
	if err := maybeRecompileWorkflowOverride(ctx, prep.lockFilePath, opts); err != nil {
		return err
	}
	if opts.Verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Using lock file: "+prep.lockFileName))
	}
	maybeWarnAboutLockFile(workflowIdOrName, prep.lockFilePath, opts)
	return maybePushWorkflowFiles(ctx, workflowIdOrName, prep.lockFilePath, opts)
}

func maybeRecompileWorkflowOverride(ctx context.Context, lockFilePath string, opts RunOptions) error {
	if opts.EngineOverride == "" || opts.RepoOverride != "" {
		if opts.EngineOverride != "" && opts.RepoOverride != "" && opts.Verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Note: Engine override ignored for remote repository workflows"))
		}
		return nil
	}
	if opts.Verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Recompiling workflow with engine override: "+opts.EngineOverride))
	}
	config := CompileConfig{
		MarkdownFiles:        []string{stringutil.LockFileToMarkdown(lockFilePath)},
		Verbose:              opts.Verbose,
		EngineOverride:       opts.EngineOverride,
		Validate:             true,
		Watch:                false,
		WorkflowDir:          "",
		NoEmit:               false,
		Purge:                false,
		TrialMode:            false,
		TrialLogicalRepoSlug: "",
		Strict:               false,
	}
	if _, err := CompileWorkflows(ctx, config); err != nil {
		return fmt.Errorf("failed to recompile workflow with engine override: %w", err)
	}
	if opts.Verbose {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Successfully recompiled workflow with engine: "+opts.EngineOverride))
	}
	return nil
}

func maybeWarnAboutLockFile(workflowIdOrName, lockFilePath string, opts RunOptions) {
	if opts.Push || opts.RepoOverride != "" {
		return
	}
	workflowMarkdownPath := stringutil.LockFileToMarkdown(lockFilePath)
	status, err := checkLockFileStatus(workflowMarkdownPath)
	if err != nil {
		return
	}
	if status.Missing {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Lock file is missing"))
	} else if status.Outdated {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Lock file is outdated (workflow file is newer)"))
	} else {
		return
	}
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Run 'gh aw run %s --push' to automatically compile and push the lock file", workflowIdOrName)))
}

func maybePushWorkflowFiles(ctx context.Context, workflowIdOrName, lockFilePath string, opts RunOptions) error {
	if !opts.Push {
		return nil
	}
	if opts.RepoOverride != "" {
		return errors.New("--push flag is only supported for local workflows, not remote repositories")
	}
	if opts.Verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Collecting workflow files for push..."))
	}
	files, err := collectWorkflowFiles(ctx, stringutil.LockFileToMarkdown(lockFilePath), opts.Verbose, opts.Approve)
	if err != nil {
		return fmt.Errorf("failed to collect workflow files: %w", err)
	}
	if err := pushWorkflowFiles(ctx, workflowIdOrName, files, opts.RefOverride, opts.Verbose); err != nil {
		return fmt.Errorf("failed to push workflow files: %w", err)
	}
	fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("Successfully pushed %d file(s) for workflow %s", len(files), workflowIdOrName)))
	return nil
}

func buildWorkflowRunArgs(lockFileName string, opts RunOptions) ([]string, string) {
	args := []string{"workflow", "run", lockFileName}
	if opts.RepoOverride != "" {
		args = append(args, "--repo", opts.RepoOverride)
	}
	ref := resolveWorkflowRunRef(opts)
	if ref != "" {
		args = append(args, "--ref", ref)
	}
	for _, input := range opts.Inputs {
		args = append(args, "-f", input)
	}
	return args, ref
}

func resolveWorkflowRunRef(opts RunOptions) string {
	if opts.RefOverride != "" || opts.RepoOverride != "" {
		return opts.RefOverride
	}
	currentBranch, err := getCurrentBranch()
	if err == nil {
		executionLog.Printf("Using current branch for workflow run: %s", currentBranch)
		return currentBranch
	}
	if opts.Verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Note: Could not determine current branch: %v", err)))
	}
	return ""
}

func handleWorkflowDryRun(lockFileName string, args []string, opts RunOptions) error {
	if opts.Verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Dry run mode - command that would be executed:"))
		fmt.Fprintln(os.Stderr, console.FormatCommandMessage("gh "+strings.Join(args, " ")))
	}
	fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("Validation passed for workflow: %s (dry run - not executed)", lockFileName)))
	return nil
}

func executeWorkflowRun(ctx context.Context, lockFileName string, args []string, opts RunOptions) (*workflowRunExecutionResult, error) {
	if opts.Verbose {
		fmt.Fprintln(os.Stderr, console.FormatCommandMessage("gh "+strings.Join(args, " ")))
	}
	stdout, err := workflow.ExecGHContext(ctx, args...).Output()
	if err != nil {
		return nil, formatWorkflowRunError(err)
	}
	output := strings.TrimSpace(string(stdout))
	if output != "" {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(output))
	}
	fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Triggered workflow: "+lockFileName))
	executionLog.Printf("Workflow triggered successfully: %s", lockFileName)
	runInfo, runErr := resolveWorkflowRunInfo(lockFileName, output, opts)
	return &workflowRunExecutionResult{
		runInfo:    runInfo,
		runInfoErr: runErr,
	}, nil
}

func formatWorkflowRunError(err error) error {
	var stderrOutput string
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		stderrOutput = string(exitError.Stderr)
		fmt.Fprintf(os.Stderr, "%s", exitError.Stderr)
	}
	errorMsg := err.Error() + " " + stderrOutput
	if isRunningInCodespace() && is403PermissionError(errorMsg) {
		fmt.Fprint(os.Stderr, getCodespacePermissionErrorMessage())
		return errors.New("failed to run workflow on GitHub Actions: permission denied (403)")
	}
	return fmt.Errorf("failed to run workflow on GitHub Actions: %w", err)
}

func resolveWorkflowRunInfo(lockFileName, output string, opts RunOptions) (*WorkflowRunInfo, error) {
	if parsedRunInfo := parseRunInfoFromOutput(output); parsedRunInfo != nil {
		executionLog.Printf("Parsed run info from gh output: id=%d, url=%s", parsedRunInfo.DatabaseID, parsedRunInfo.URL)
		return parsedRunInfo, nil
	}
	return getLatestWorkflowRunWithRetry(lockFileName, opts.RepoOverride, opts.Verbose)
}

func handleWorkflowRunInfo(runInfo *WorkflowRunInfo, runErr error, opts RunOptions) {
	if runErr == nil && runInfo != nil && runInfo.URL != "" {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("View workflow run: "+runInfo.URL))
		executionLog.Printf("Workflow run URL: %s (ID: %d)", runInfo.URL, runInfo.DatabaseID)
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Analyze this run: %s audit %d", string(constants.CLIExtensionPrefix), runInfo.DatabaseID)))
		return
	}
	if opts.Verbose && runErr != nil {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Note: Could not get workflow run URL: %v", runErr)))
	}
}

func waitForWorkflowRunCompletion(ctx context.Context, opts RunOptions, runInfo *WorkflowRunInfo, runErr error, workflowStartTime time.Time, _ string) error {
	if !opts.WaitForCompletion && !opts.AutoMergePRs {
		return nil
	}
	if runErr != nil {
		printWorkflowRunInfoWarning(opts, runErr)
		return nil
	}
	targetRepo := resolveWorkflowTargetRepo(opts)
	if targetRepo == "" || runInfo == nil {
		return nil
	}
	printWorkflowWaitMessage(opts.AutoMergePRs)
	runIDStr := strconv.FormatInt(runInfo.DatabaseID, 10)
	if err := WaitForWorkflowCompletion(ctx, targetRepo, runIDStr, workflowCompletionWaitTimeoutMinutes, opts.Verbose); err != nil {
		if ctx.Err() != nil || errors.Is(err, ErrInterrupted) {
			return err
		}
		printWorkflowCompletionWarning(opts.AutoMergePRs, err)
		return nil
	}
	if opts.AutoMergePRs {
		if err := AutoMergePullRequestsCreatedAfter(targetRepo, workflowStartTime, opts.Verbose); err != nil {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to auto-merge pull requests: %v", err)))
		}
	}
	return nil
}

func printWorkflowRunInfoWarning(opts RunOptions, runErr error) {
	if opts.AutoMergePRs {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Could not get workflow run information for auto-merge: %v", runErr)))
	} else if opts.WaitForCompletion {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Could not get workflow run information: %v", runErr)))
	}
}

func resolveWorkflowTargetRepo(opts RunOptions) string {
	if opts.RepoOverride != "" {
		return opts.RepoOverride
	}
	currentRepo, err := GetCurrentRepoSlug()
	if err != nil {
		if opts.AutoMergePRs {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Could not determine target repository for auto-merge: %v", err)))
		}
		return ""
	}
	return currentRepo
}

func printWorkflowWaitMessage(autoMerge bool) {
	message := "Waiting for workflow to complete..."
	if autoMerge {
		message = "Auto-merge PRs enabled - waiting for workflow completion..."
	}
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage(message))
}

func printWorkflowCompletionWarning(autoMerge bool, err error) {
	message := fmt.Sprintf("Workflow did not complete successfully: %v", err)
	if autoMerge {
		message = fmt.Sprintf("Workflow did not complete successfully, skipping auto-merge: %v", err)
	}
	fmt.Fprintln(os.Stderr, console.FormatWarningMessage(message))
}

func restoreEnabledWorkflow(workflowIdOrName string, opts RunOptions, state workflowEnableState) {
	if opts.Enable && state.wasDisabled && state.workflowID != 0 {
		restoreWorkflowState(workflowIdOrName, state.workflowID, opts.RepoOverride, opts.Verbose)
	}
}

// validateWorkflowsForRun validates all workflow names before running.
func validateWorkflowsForRun(workflowNames []string, opts RunOptions) error {
	for _, workflowName := range workflowNames {
		if workflowName == "" {
			return errors.New("workflow name cannot be empty")
		}
		if opts.RepoOverride != "" {
			if err := validateRemoteWorkflow(workflowName, opts.RepoOverride, opts.Verbose); err != nil {
				return fmt.Errorf("failed to validate remote workflow '%s': %w", workflowName, err)
			}
		} else {
			workflowFile, err := resolveWorkflowFile(workflowName, opts.Verbose)
			if err != nil {
				return err
			}
			runnable, err := IsRunnable(workflowFile)
			if err != nil {
				return fmt.Errorf("failed to check if workflow '%s' is runnable: %w", workflowName, err)
			}
			if !runnable {
				return fmt.Errorf("workflow '%s' does not declare a workflow_dispatch trigger, so it cannot be run manually on GitHub Actions. Expected an `on: workflow_dispatch` trigger in the source workflow frontmatter, then recompile. Example:\non:\n  workflow_dispatch:\n\nRun: gh aw compile", workflowName)
			}
		}
	}
	return nil
}

// executeAllWorkflowsOnce runs each workflow name once in sequence.
func executeAllWorkflowsOnce(ctx context.Context, workflowNames []string, opts RunOptions) error {
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Running %d workflow(s)...", len(workflowNames))))
	for i, workflowName := range workflowNames {
		select {
		case <-ctx.Done():
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Operation cancelled"))
			return ctx.Err()
		default:
		}
		if len(workflowNames) > 1 {
			fmt.Fprintln(os.Stderr, console.FormatProgressMessage(fmt.Sprintf("Running workflow %d/%d: %s", i+1, len(workflowNames), workflowName)))
		}
		workflowOpts := opts
		if opts.RepeatCount > 0 {
			workflowOpts.WaitForCompletion = true
		}
		if err := RunWorkflowOnGitHub(ctx, workflowName, workflowOpts); err != nil {
			return fmt.Errorf("failed to run workflow '%s': %w", workflowName, err)
		}
		if i < len(workflowNames)-1 {
			timer := time.NewTimer(betweenWorkflowsDelay)
			select {
			case <-ctx.Done():
				timer.Stop()
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Operation cancelled"))
				return ctx.Err()
			case <-timer.C:
			}
		}
	}
	fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("Successfully triggered %d workflow(s)", len(workflowNames))))
	return nil
}

// wrapRunWithJSONOutput wraps a run-all function to emit a JSON summary to stdout.
func wrapRunWithJSONOutput(inner func() error, workflowNames []string, opts RunOptions) func() error {
	return func() error {
		var results []WorkflowRunResult
		for _, workflowName := range workflowNames {
			normalizedID := normalizeWorkflowID(workflowName)
			status := "triggered"
			if opts.DryRun {
				status = "dry_run"
			}
			results = append(results, WorkflowRunResult{Workflow: normalizedID, LockFile: normalizedID + ".lock.yml", Status: status})
		}
		runErr := inner()
		if runErr != nil {
			for i := range results {
				results[i].Status = "error"
				results[i].Error = runErr.Error()
			}
		}
		jsonBytes, err := marshalIndentJSONOrWrap(results, "workflow run results")
		if err != nil {
			return err
		}
		fmt.Fprintln(os.Stdout, string(jsonBytes))
		return runErr
	}
}

// RunWorkflowsOnGitHub runs multiple agentic workflows on GitHub Actions, optionally repeating a specified number of times
func RunWorkflowsOnGitHub(ctx context.Context, workflowNames []string, opts RunOptions) error {
	if len(workflowNames) == 0 {
		return errors.New("workflow list is empty. Expected at least one workflow file name or numeric workflow ID. Example: gh aw run ci")
	}
	select {
	case <-ctx.Done():
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Operation cancelled"))
		return ctx.Err()
	default:
	}
	if err := validateWorkflowsForRun(workflowNames, opts); err != nil {
		return err
	}
	runAllWorkflows := func() error {
		return executeAllWorkflowsOnce(ctx, workflowNames, opts)
	}
	if opts.JSON {
		runAllWorkflows = wrapRunWithJSONOutput(runAllWorkflows, workflowNames, opts)
	}
	return ExecuteWithRepeat(RepeatOptions{
		Ctx:           ctx,
		RepeatCount:   opts.RepeatCount,
		RepeatMessage: "Repeating workflow run",
		ExecuteFunc:   runAllWorkflows,
		UseStderr:     false,
	})
}

// runInfoURLRegexp matches GitHub Actions run URLs of the form:
// https://{host}/{owner}/{repo}/actions/runs/{run_id}
// Supports both public GitHub (github.com) and GitHub Enterprise Server deployments.
var runInfoURLRegexp = regexp.MustCompile(`https://[^/\s]+/[^/\s]+/[^/\s]+/actions/runs/(\d+)`)

// parseRunInfoFromOutput tries to extract workflow run information from the
// output of `gh workflow run` (v2.87+), which now returns the run URL.
// Returns nil if the run URL cannot be found in the output.
func parseRunInfoFromOutput(output string) *WorkflowRunInfo {
	matches := runInfoURLRegexp.FindStringSubmatch(output)
	if len(matches) < 2 {
		return nil
	}
	runID, err := strconv.ParseInt(matches[1], 10, 64)
	if err != nil {
		return nil
	}
	return &WorkflowRunInfo{
		URL:        matches[0],
		DatabaseID: runID,
	}
}
