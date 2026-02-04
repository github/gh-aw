package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/cli/fileutil"
	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/repoutil"
	"github.com/github/gh-aw/pkg/workflow"
	"github.com/spf13/cobra"
)

var trialLog = logger.New("cli:trial_command")

// WorkflowTrialResult represents the result of running a single workflow trial
type WorkflowTrialResult struct {
	WorkflowName string         `json:"workflow_name"`
	RunID        string         `json:"run_id"`
	SafeOutputs  map[string]any `json:"safe_outputs"`
	//AgentStdioLogs      []string               `json:"agent_stdio_logs,omitempty"`
	AgenticRunInfo      map[string]any `json:"agentic_run_info,omitempty"`
	AdditionalArtifacts map[string]any `json:"additional_artifacts,omitempty"`
	Timestamp           time.Time      `json:"timestamp"`
}

// CombinedTrialResult represents the combined results of multiple workflow trials
type CombinedTrialResult struct {
	WorkflowNames []string              `json:"workflow_names"`
	Results       []WorkflowTrialResult `json:"results"`
	Timestamp     time.Time             `json:"timestamp"`
}

// RepoConfig groups repository-related configuration for trial execution
type RepoConfig struct {
	LogicalRepo string // The repo to simulate execution against
	CloneRepo   string // Alternative to LogicalRepo: clone this repo's contents
	HostRepo    string // The host repository where workflows will be installed
}

// TrialOptions contains all configuration options for running workflow trials
type TrialOptions struct {
	Repos          RepoConfig
	DeleteHostRepo bool
	ForceDelete    bool
	Quiet          bool
	DryRun         bool
	TimeoutMinutes int
	TriggerContext string
	RepeatCount    int
	AutoMergePRs   bool
	EngineOverride string
	AppendText     string
	PushSecrets    bool
	Verbose        bool
}

// NewTrialCommand creates the trial command
func NewTrialCommand(validateEngine func(string) error) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "trial <workflow-spec>...",
		Short: "Trial one or more agentic workflows as if they were running in a repository",
		Long: `Trial one or more agentic workflows as if they were running in a repository.

This command creates a temporary private repository in your GitHub space, installs the specified
workflow(s) from their source repositories, and runs them in "trial mode" to capture safe outputs without
making actual changes to the "simulated" host repository

Single workflow:
  ` + string(constants.CLIExtensionPrefix) + ` trial githubnext/agentics/weekly-research
  Outputs: stdout + local trials/weekly-research.DATETIME-ID.json + trial repo trials/

Multiple workflows (for comparison):
  ` + string(constants.CLIExtensionPrefix) + ` trial githubnext/agentics/daily-plan githubnext/agentics/weekly-research
  Outputs: stdout + local trials/ + trial repo trials/ (individual + combined results)

Workflows from different repositories:
  ` + string(constants.CLIExtensionPrefix) + ` trial githubnext/agentics/daily-plan myorg/myrepo/custom-workflow

Repository mode examples:
  ` + string(constants.CLIExtensionPrefix) + ` trial githubnext/agentics/my-workflow --repo myorg/myrepo            # Run directly in myorg/myrepo (no simulation)
  ` + string(constants.CLIExtensionPrefix) + ` trial githubnext/agentics/my-workflow --logical-repo myorg/myrepo  # Simulate running against myorg/myrepo
  ` + string(constants.CLIExtensionPrefix) + ` trial githubnext/agentics/my-workflow --clone-repo myorg/myrepo   # Clone myorg/myrepo contents into host

Repeat and cleanup examples:
  ` + string(constants.CLIExtensionPrefix) + ` trial githubnext/agentics/my-workflow --repeat 3                # Run 3 times total
  ` + string(constants.CLIExtensionPrefix) + ` trial githubnext/agentics/my-workflow --delete-host-repo-after  # Delete repo after completion
  ` + string(constants.CLIExtensionPrefix) + ` trial githubnext/agentics/my-workflow --quiet --host-repo my-trial # Custom host repo
  ` + string(constants.CLIExtensionPrefix) + ` trial githubnext/agentics/my-workflow --dry-run                 # Show what would be done without changes

Auto-merge examples:
  ` + string(constants.CLIExtensionPrefix) + ` trial githubnext/agentics/my-workflow --auto-merge-prs          # Auto-merge any PRs created during trial

Advanced examples:
  ` + string(constants.CLIExtensionPrefix) + ` trial githubnext/agentics/my-workflow --host-repo . # Use current repo as host
  ` + string(constants.CLIExtensionPrefix) + ` trial ./local-workflow.md --clone-repo upstream/repo --repeat 2

Repository modes:
- Default mode (no flags): Creates a temporary trial repository and simulates execution as if running against the current repository (github.repository context points to current repo)
- --logical-repo REPO: Simulates execution against a specified repository (github.repository context points to REPO while actually running in a temporary trial repository)
- --repo REPO: Runs directly in the specified repository (no simulation, workflows installed and executed in REPO)
- --clone-repo REPO: Clones the specified repository's contents into the trial repository before execution (useful for testing against actual repository state)

All workflows must support workflow_dispatch trigger to be used in trial mode.
The host repository will be created as private and kept by default unless --delete-host-repo-after is specified.
Trial results are saved both locally (in trials/ directory) and in the host repository for future reference.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			workflowSpecs := args
			logicalRepoSpec, _ := cmd.Flags().GetString("logical-repo")
			cloneRepoSpec, _ := cmd.Flags().GetString("clone-repo")
			hostRepoSpec, _ := cmd.Flags().GetString("host-repo")
			repoSpec, _ := cmd.Flags().GetString("repo")
			deleteHostRepo, _ := cmd.Flags().GetBool("delete-host-repo-after")
			forceDeleteHostRepo, _ := cmd.Flags().GetBool("force-delete-host-repo-before")
			yes, _ := cmd.Flags().GetBool("yes")
			dryRun, _ := cmd.Flags().GetBool("dry-run")
			timeout, _ := cmd.Flags().GetInt("timeout")
			triggerContext, _ := cmd.Flags().GetString("trigger-context")
			repeatCount, _ := cmd.Flags().GetInt("repeat")
			autoMergePRs, _ := cmd.Flags().GetBool("auto-merge-prs")
			engineOverride, _ := cmd.Flags().GetString("engine")
			appendText, _ := cmd.Flags().GetString("append")
			pushSecrets, _ := cmd.Flags().GetBool("use-local-secrets")
			verbose, _ := cmd.Root().PersistentFlags().GetBool("verbose")

			if err := validateEngine(engineOverride); err != nil {
				return err
			}
			// If --repo was used instead of --host-repo, use its value
			if repoSpec != "" {
				hostRepoSpec = repoSpec
			}

			opts := TrialOptions{
				Repos: RepoConfig{
					LogicalRepo: logicalRepoSpec,
					CloneRepo:   cloneRepoSpec,
					HostRepo:    hostRepoSpec,
				},
				DeleteHostRepo: deleteHostRepo,
				ForceDelete:    forceDeleteHostRepo,
				Quiet:          yes,
				DryRun:         dryRun,
				TimeoutMinutes: timeout,
				TriggerContext: triggerContext,
				RepeatCount:    repeatCount,
				AutoMergePRs:   autoMergePRs,
				EngineOverride: engineOverride,
				AppendText:     appendText,
				PushSecrets:    pushSecrets,
				Verbose:        verbose,
			}

			if err := RunWorkflowTrials(cmd.Context(), workflowSpecs, opts); err != nil {
				return err
			}
			return nil
		},
	}

	// Add flags
	cmd.Flags().StringP("logical-repo", "s", "", "The repo we're simulating the execution for, as if the workflow was installed in that repo (defaults to current repository)")
	cmd.Flags().String("clone-repo", "", "Alternative to --logical-repo: clone the contents of the specified repo into the host repo instead of using logical repository simulation")

	// Get current username for default trial repo description
	username, _ := getCurrentGitHubUsername()
	defaultHostRepo := "gh-aw-trial"
	if username != "" {
		defaultHostRepo = fmt.Sprintf("%s/gh-aw-trial", username)
	}

	cmd.Flags().String("host-repo", "", fmt.Sprintf("Custom host repository slug (defaults to '%s'). Use '.' for current repository", defaultHostRepo))
	cmd.Flags().String("repo", "", "Alias for --host-repo")
	cmd.Flags().Bool("delete-host-repo-after", false, "Delete the host repository after completion (default: keep)")
	cmd.Flags().Bool("force-delete-host-repo-before", false, "Force delete the host repository before creation, if it exists before creating it")
	cmd.Flags().BoolP("yes", "y", false, "Skip confirmation prompts")
	cmd.Flags().Bool("dry-run", false, "Show what would be done without making any changes")
	cmd.Flags().Int("timeout", 30, "Execution timeout in minutes (default: 30)")
	cmd.Flags().String("trigger-context", "", "Trigger context URL (e.g., GitHub issue URL) for issue-triggered workflows")
	cmd.Flags().Int("repeat", 0, "Number of times to repeat running workflows (0 = run once)")
	cmd.Flags().Bool("auto-merge-prs", false, "Auto-merge any pull requests created during trial execution")
	addEngineFlag(cmd)
	cmd.Flags().String("append", "", "Append extra content to the end of agentic workflow on installation")
	cmd.Flags().Bool("use-local-secrets", false, "Use local environment API key secrets for trial execution (pushes and cleans up secrets in repository)")
	cmd.MarkFlagsMutuallyExclusive("host-repo", "repo")
	cmd.MarkFlagsMutuallyExclusive("logical-repo", "clone-repo")

	return cmd
}

// RunWorkflowTrials executes the main logic for trialing one or more workflows
func RunWorkflowTrials(ctx context.Context, workflowSpecs []string, opts TrialOptions) error {
	trialLog.Printf("Starting trial execution: specs=%v, logicalRepo=%s, cloneRepo=%s, hostRepo=%s, repeat=%d", workflowSpecs, opts.Repos.LogicalRepo, opts.Repos.CloneRepo, opts.Repos.HostRepo, opts.RepeatCount)

	// Parse all workflow specifications
	var parsedSpecs []*WorkflowSpec
	for _, spec := range workflowSpecs {
		parsedSpec, err := parseWorkflowSpec(spec)
		if err != nil {
			return fmt.Errorf("invalid workflow specification '%s': %w", spec, err)
		}
		parsedSpecs = append(parsedSpecs, parsedSpec)
	}

	if opts.DryRun {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("[DRY RUN] Showing what would be done without making changes"))
	}

	if len(parsedSpecs) == 1 {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Starting trial of workflow '%s' from '%s'", parsedSpecs[0].WorkflowName, parsedSpecs[0].RepoSlug)))
	} else {
		workflowNames := make([]string, len(parsedSpecs))
		for i, spec := range parsedSpecs {
			workflowNames[i] = spec.WorkflowName
		}
		joinedNames := strings.Join(workflowNames, ", ")
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Starting trial of %d workflows (%s)", len(parsedSpecs), joinedNames)))
	}

	// Step 0: Determine workflow mode (mutual exclusion is enforced by Cobra)
	var logicalRepoSlug string
	var cloneRepoSlug string
	var cloneRepoVersion string
	var directTrialMode bool

	if opts.Repos.CloneRepo != "" {
		// Use clone-repo mode: clone the specified repo contents into host repo
		cloneRepo, err := parseRepoSpec(opts.Repos.CloneRepo)
		if err != nil {
			return fmt.Errorf("invalid --clone-repo specification '%s': %w", opts.Repos.CloneRepo, err)
		}

		cloneRepoSlug = cloneRepo.RepoSlug
		cloneRepoVersion = cloneRepo.Version
		logicalRepoSlug = "" // Empty string means skip logical repo simulation
		directTrialMode = false
		trialLog.Printf("Using clone-repo mode: %s (version=%s)", cloneRepoSlug, cloneRepoVersion)
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Clone mode: Will clone contents from %s into host repository", cloneRepoSlug)))
	} else if opts.Repos.LogicalRepo != "" {
		// Use logical-repo mode: simulate the workflow running against the specified repo
		logicalRepo, err := parseRepoSpec(opts.Repos.LogicalRepo)

		if err != nil {
			return fmt.Errorf("invalid --logical-repo specification '%s': %w", opts.Repos.LogicalRepo, err)
		}

		logicalRepoSlug = logicalRepo.RepoSlug
		directTrialMode = false
		trialLog.Printf("Using logical-repo mode: %s", logicalRepoSlug)
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Target repository (specified): %s", logicalRepoSlug)))
	} else {
		// No --clone-repo or --logical-repo specified
		// If --repo is specified without simulation flags, it's direct trial mode
		// Otherwise, fall back to current repository for logical-repo mode
		if opts.Repos.HostRepo != "" {
			// Direct trial mode: run workflows directly in the specified repo without simulation
			logicalRepoSlug = ""
			cloneRepoSlug = ""
			directTrialMode = true
			trialLog.Print("Using direct trial mode (no simulation)")
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Direct trial mode: Workflows will be installed and run directly in the specified repository"))
		} else {
			// Fall back to current repository for logical-repo mode
			var err error
			logicalRepoSlug, err = GetCurrentRepoSlug()
			if err != nil {
				return fmt.Errorf("failed to determine simulated host repository: %w", err)
			}
			directTrialMode = false
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Target repository (current): %s", logicalRepoSlug)))
		}
	}

	// Step 1: Determine host repository slug
	var hostRepoSlug string
	if opts.Repos.HostRepo != "" {

		hostRepo, err := parseRepoSpec(opts.Repos.HostRepo)

		if err != nil {
			return fmt.Errorf("invalid --host-repo specification '%s': %w", opts.Repos.HostRepo, err)
		}
		hostRepoSlug = hostRepo.RepoSlug
		trialLog.Printf("Using specified host repository: %s", hostRepoSlug)
	} else {
		// Use default trial repo with current username
		username, err := getCurrentGitHubUsername()
		if err != nil {
			return fmt.Errorf("failed to get GitHub username for default trial repo: %w", err)
		}
		hostRepoSlug = fmt.Sprintf("%s/gh-aw-trial", username)
		trialLog.Printf("Using default host repository: %s", hostRepoSlug)
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Host repository (default): %s", hostRepoSlug)))
	}

	// Step 1.5: Show confirmation unless quiet mode
	if !opts.Quiet {
		if err := showTrialConfirmation(parsedSpecs, logicalRepoSlug, cloneRepoSlug, hostRepoSlug, opts.DeleteHostRepo, opts.ForceDelete, opts.PushSecrets, opts.AutoMergePRs, opts.RepeatCount, directTrialMode); err != nil {
			return err
		}
	}

	// Step 2: Create or reuse host repository
	trialLog.Printf("Ensuring trial repository exists: %s", hostRepoSlug)
	if err := ensureTrialRepository(hostRepoSlug, cloneRepoSlug, opts.ForceDelete, opts.DryRun, opts.Verbose); err != nil {
		return fmt.Errorf("failed to ensure host repository: %w", err)
	}

	// In dry-run mode, stop here after showing what would be done
	if opts.DryRun {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("[DRY RUN] Stopping here. No actual changes were made."))
		return nil
	}

	// Step 2.5: Create secret tracker
	var secretTracker *TrialSecretTracker
	if opts.PushSecrets {
		secretTracker = NewTrialSecretTracker(hostRepoSlug)
		trialLog.Print("Created secret tracker for trial")

		// Set up secret cleanup to always run on exit
		defer func() {
			if err := cleanupTrialSecrets(hostRepoSlug, secretTracker, opts.Verbose); err != nil {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to cleanup secrets: %v", err)))
			}
		}()
	} else {
		trialLog.Print("Secret pushing disabled, workflows must have required secrets already configured")
	}

	// Set up cleanup if requested
	if opts.DeleteHostRepo {
		defer func() {
			if err := cleanupTrialRepository(hostRepoSlug, opts.Verbose); err != nil {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to cleanup host repository: %v", err)))
			}
		}()
	}

	// Step 2.7: Clone source repository contents if in clone-repo mode
	if cloneRepoSlug != "" {
		if err := cloneRepoContentsIntoHost(cloneRepoSlug, cloneRepoVersion, hostRepoSlug, opts.Verbose); err != nil {
			return fmt.Errorf("failed to clone repository contents: %w", err)
		}
	}

	// Step 2.8: Disable all workflows except the ones being trialled (only in clone-repo mode, done once before all trials)
	if cloneRepoSlug != "" {
		// Build list of workflow names to keep enabled
		var workflowsToKeep []string
		for _, spec := range parsedSpecs {
			workflowsToKeep = append(workflowsToKeep, spec.WorkflowName)
		}

		if opts.Verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Disabling workflows in cloned repository (keeping: %s)", strings.Join(workflowsToKeep, ", "))))
		}

		// Clone host repository temporarily to access workflows
		tempDirForDisable, err := cloneTrialHostRepository(hostRepoSlug, opts.Verbose)
		if err != nil {
			return fmt.Errorf("failed to clone host repository for workflow disabling: %w", err)
		}
		defer func() {
			if err := os.RemoveAll(tempDirForDisable); err != nil {
				trialLog.Printf("Failed to cleanup temp directory for workflow disabling: %v", err)
			}
		}()

		// Change to temp directory to access local .github/workflows
		originalDir, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get current directory: %w", err)
		}

		if err := os.Chdir(tempDirForDisable); err != nil {
			return fmt.Errorf("failed to change to temp directory: %w", err)
		}
		// Always attempt to change back to the original directory
		defer func() {
			if err := os.Chdir(originalDir); err != nil {
				trialLog.Printf("Failed to change back to original directory: %v", err)
			}
		}()

		// Disable workflows (pass empty string for repoSlug since we're working locally)
		disableErr := DisableAllWorkflowsExcept("", workflowsToKeep, opts.Verbose)
		// Check for disable errors after changing back
		if disableErr != nil {
			// Log warning but don't fail the trial - workflow disabling is not critical
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to disable workflows: %v", disableErr)))
		}
	}

	// Function to run all trials once
	runAllTrials := func() error {
		// Generate a unique datetime-ID for this trial session
		dateTimeID := fmt.Sprintf("%s-%d", time.Now().Format("20060102-150405"), time.Now().UnixNano()%1000000)
		trialLog.Printf("Starting trial run: dateTimeID=%s", dateTimeID)

		// Determine target repo slug for filenames once
		// In direct trial mode, use hostRepoSlug; otherwise use logicalRepoSlug
		targetRepoForFilename := logicalRepoSlug
		if directTrialMode {
			targetRepoForFilename = hostRepoSlug
		}

		// Step 3: Clone host repository to local temp directory
		trialLog.Printf("Cloning trial host repository: %s", hostRepoSlug)
		tempDir, err := cloneTrialHostRepository(hostRepoSlug, opts.Verbose)
		if err != nil {
			return fmt.Errorf("failed to clone host repository: %w", err)
		}
		trialLog.Printf("Cloned repository to: %s", tempDir)
		defer func() {
			if err := os.RemoveAll(tempDir); err != nil {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to cleanup local temp directory: %v", err)))
			}
		}()

		// Step 4: Create trials directory
		if err := os.MkdirAll("trials", 0755); err != nil {
			return fmt.Errorf("failed to create trials directory: %w", err)
		}

		// Step 5: Run trials for each workflow
		var workflowResults []WorkflowTrialResult

		for i, parsedSpec := range parsedSpecs {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("=== Running trial for workflow: %s ===", parsedSpec.WorkflowName)))

			// Install workflow with trial mode compilation
			if err := installWorkflowInTrialMode(ctx, tempDir, parsedSpec, logicalRepoSlug, cloneRepoSlug, hostRepoSlug, secretTracker, opts.EngineOverride, opts.AppendText, opts.PushSecrets, directTrialMode, opts.Verbose); err != nil {
				return fmt.Errorf("failed to install workflow '%s' in trial mode: %w", parsedSpec.WorkflowName, err)
			}

			// Display workflow description if present
			workflowPath := filepath.Join(tempDir, ".github/workflows", parsedSpec.WorkflowName+".md")
			if description := ExtractWorkflowDescriptionFromFile(workflowPath); description != "" {
				fmt.Fprintln(os.Stderr, "")
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage(description))
				fmt.Fprintln(os.Stderr, "")
			}

			// Add user's PAT as repository secret (only once)
			if i == 0 && opts.PushSecrets {
				if err := addGitHubTokenSecret(hostRepoSlug, secretTracker, opts.Verbose); err != nil {
					return fmt.Errorf("failed to add GitHub token secret: %w", err)
				}
			}

			// Run the workflow and wait for completion (with trigger context if provided)
			runID, err := triggerWorkflowRun(hostRepoSlug, parsedSpec.WorkflowName, opts.TriggerContext, opts.Verbose)
			if err != nil {
				return fmt.Errorf("failed to trigger workflow run for '%s': %w", parsedSpec.WorkflowName, err)
			}

			// Generate workflow run URL
			workflowRunURL := fmt.Sprintf("https://github.com/%s/actions/runs/%s", hostRepoSlug, runID)
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Workflow run started with ID: %s (%s)", runID, workflowRunURL)))

			// Wait for workflow completion
			if err := WaitForWorkflowCompletion(hostRepoSlug, runID, opts.TimeoutMinutes, opts.Verbose); err != nil {
				return fmt.Errorf("workflow '%s' execution failed or timed out: %w", parsedSpec.WorkflowName, err)
			}

			// Auto-merge PRs if requested
			if opts.AutoMergePRs {
				if err := AutoMergePullRequestsLegacy(hostRepoSlug, opts.Verbose); err != nil {
					fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to auto-merge pull requests: %v", err)))
				}
			}

			// Download and process all artifacts
			artifacts, err := downloadAllArtifacts(hostRepoSlug, runID, opts.Verbose)
			if err != nil {
				return fmt.Errorf("failed to download artifacts for '%s': %w", parsedSpec.WorkflowName, err)
			}

			// Save individual workflow results
			result := WorkflowTrialResult{
				WorkflowName: parsedSpec.WorkflowName,
				RunID:        runID,
				SafeOutputs:  artifacts.SafeOutputs,
				//AgentStdioLogs:      artifacts.AgentStdioLogs,
				AgenticRunInfo:      artifacts.AgenticRunInfo,
				AdditionalArtifacts: artifacts.AdditionalArtifacts,
				Timestamp:           time.Now(),
			}
			workflowResults = append(workflowResults, result)

			// Save individual trial file
			sanitizedTargetRepo := repoutil.SanitizeForFilename(targetRepoForFilename)
			individualFilename := fmt.Sprintf("trials/%s-%s.%s.json", parsedSpec.WorkflowName, sanitizedTargetRepo, dateTimeID)
			if err := saveTrialResult(individualFilename, result, opts.Verbose); err != nil {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to save individual trial result: %v", err)))
			}

			// Display safe outputs to stdout
			if len(artifacts.SafeOutputs) > 0 {
				outputBytes, _ := json.MarshalIndent(artifacts.SafeOutputs, "", "  ")
				fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("=== Safe Outputs from %s ===", parsedSpec.WorkflowName)))
				fmt.Println(string(outputBytes))
				fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("=== End of Safe Outputs ==="))
			} else {
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("=== No Safe Outputs Generated by %s ===", parsedSpec.WorkflowName)))
			}

			// Display additional artifact information if available
			// if len(artifacts.AgentStdioLogs) > 0 {
			// 	fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("=== Agent Stdio Logs Available from %s (%d files) ===", parsedSpec.WorkflowName, len(artifacts.AgentStdioLogs))))
			// }
			if len(artifacts.AgenticRunInfo) > 0 {
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("=== Agentic Run Information Available from %s ===", parsedSpec.WorkflowName)))
			}
			if len(artifacts.AdditionalArtifacts) > 0 {
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("=== Additional Artifacts Available from %s (%d files) ===", parsedSpec.WorkflowName, len(artifacts.AdditionalArtifacts))))
			}

			fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("Trial completed for workflow: %s", parsedSpec.WorkflowName)))
		}

		// Step 6: Save combined results for multi-workflow trials
		if len(parsedSpecs) > 1 {
			workflowNames := make([]string, len(parsedSpecs))
			for i, spec := range parsedSpecs {
				workflowNames[i] = spec.WorkflowName
			}
			workflowNamesStr := strings.Join(workflowNames, "-")
			sanitizedTargetRepo := repoutil.SanitizeForFilename(targetRepoForFilename)
			combinedFilename := fmt.Sprintf("trials/%s-%s.%s.json", workflowNamesStr, sanitizedTargetRepo, dateTimeID)
			combinedResult := CombinedTrialResult{
				WorkflowNames: workflowNames,
				Results:       workflowResults,
				Timestamp:     time.Now(),
			}
			if err := saveTrialResult(combinedFilename, combinedResult, opts.Verbose); err != nil {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to save combined trial result: %v", err)))
			}
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Combined results saved to: %s", combinedFilename)))
		}

		// Step 6.5: Copy trial results to host repository and commit them
		workflowNames := make([]string, len(parsedSpecs))
		for i, spec := range parsedSpecs {
			workflowNames[i] = spec.WorkflowName
		}
		if err := copyTrialResultsToHostRepo(tempDir, dateTimeID, workflowNames, targetRepoForFilename, opts.Verbose); err != nil {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to copy trial results to repository: %v", err)))
		}

		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("All trials completed successfully"))
		return nil
	}

	// Execute trials with optional repeat functionality
	return ExecuteWithRepeat(RepeatOptions{
		RepeatCount:   opts.RepeatCount,
		RepeatMessage: "Repeating trial run",
		ExecuteFunc:   runAllTrials,
		CleanupFunc: func() {
			if opts.DeleteHostRepo {
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Host repository will be cleaned up"))
			} else {
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Host repository preserved: https://github.com/%s", hostRepoSlug)))
			}
		},
		UseStderr: true,
	})

}

// getCurrentGitHubUsername gets the current GitHub username from gh CLI
func getCurrentGitHubUsername() (string, error) {
	output, err := workflow.RunGH("Fetching GitHub username...", "api", "user", "--jq", ".login")
	if err != nil {
		return "", fmt.Errorf("failed to get GitHub username: %w", err)
	}

	username := strings.TrimSpace(string(output))
	if username == "" {
		return "", fmt.Errorf("GitHub username is empty")
	}

	return username, nil
}

// showTrialConfirmation displays a confirmation prompt to the user using parsed workflow specs
func showTrialConfirmation(parsedSpecs []*WorkflowSpec, logicalRepoSlug, cloneRepoSlug, hostRepoSlug string, deleteHostRepo bool, forceDeleteHostRepo bool, pushSecrets bool, autoMergePRs bool, repeatCount int, directTrialMode bool) error {
	hostRepoSlugURL := fmt.Sprintf("https://github.com/%s", hostRepoSlug)

	var sections []string

	// Title box with double border
	titleText := "Trial Execution Plan"
	sections = append(sections, console.RenderTitleBox(titleText, 80)...)

	sections = append(sections, "")

	// Workflow information section
	var workflowInfo strings.Builder
	if len(parsedSpecs) == 1 {
		fmt.Fprintf(&workflowInfo, "Workflow:  %s (from %s)", parsedSpecs[0].WorkflowName, parsedSpecs[0].RepoSlug)
	} else {
		workflowInfo.WriteString("Workflows:")
		for _, spec := range parsedSpecs {
			fmt.Fprintf(&workflowInfo, "\n  • %s (from %s)", spec.WorkflowName, spec.RepoSlug)
		}
	}

	sections = append(sections, console.RenderInfoSection(workflowInfo.String())...)

	sections = append(sections, "")

	// Display target repository info based on mode
	var modeInfo strings.Builder
	if cloneRepoSlug != "" {
		// Clone-repo mode
		fmt.Fprintf(&modeInfo, "Source:    %s (will be cloned)\n", cloneRepoSlug)
		modeInfo.WriteString("Mode:      Clone repository contents into host repository")
	} else if directTrialMode {
		// Direct trial mode
		fmt.Fprintf(&modeInfo, "Target:    %s (direct)\n", hostRepoSlug)
		modeInfo.WriteString("Mode:      Run workflows directly in repository (no simulation)")
	} else {
		// Logical-repo mode
		fmt.Fprintf(&modeInfo, "Target:    %s (simulated)\n", logicalRepoSlug)
		modeInfo.WriteString("Mode:      Simulate execution against target repository")
	}

	sections = append(sections, console.RenderInfoSection(modeInfo.String())...)

	sections = append(sections, "")

	// Host repository info
	var hostInfo strings.Builder
	fmt.Fprintf(&hostInfo, "Host Repo:  %s\n", hostRepoSlug)
	fmt.Fprintf(&hostInfo, "            %s", hostRepoSlugURL)

	sections = append(sections, console.RenderInfoSection(hostInfo.String())...)

	sections = append(sections, "")

	// Configuration settings
	var configInfo strings.Builder
	if deleteHostRepo {
		configInfo.WriteString("Cleanup:   Host repository will be deleted after completion")
	} else {
		configInfo.WriteString("Cleanup:   Host repository will be preserved")
	}

	// Display secret usage information
	configInfo.WriteString("\n")
	if pushSecrets {
		configInfo.WriteString("Secrets:   Local API keys will be pushed and cleaned up after execution")
	} else {
		configInfo.WriteString("Secrets:   Workflows must use pre-configured repository secrets")
	}

	// Display repeat count if set
	if repeatCount > 0 {
		fmt.Fprintf(&configInfo, "\nRepeat:    Will run %d times (total executions: %d)", repeatCount, repeatCount+1)
	}

	// Display auto-merge setting if enabled
	if autoMergePRs {
		configInfo.WriteString("\nAuto-merge: Pull requests will be automatically merged")
	}

	sections = append(sections, console.RenderInfoSection(configInfo.String())...)

	sections = append(sections, "")

	// Compose and output all sections
	console.RenderComposedSections(sections)

	// Add "Execution Steps" section separator
	executionStepsSections := console.RenderTitleBox("Execution Steps", 80)
	console.RenderComposedSections(executionStepsSections)

	// Check if host repository already exists to update messaging
	hostRepoExists := false
	checkCmd := workflow.ExecGH("repo", "view", hostRepoSlug)
	if err := checkCmd.Run(); err == nil {
		hostRepoExists = true
	}

	// Step 1: Repository creation/reuse
	stepNum := 1
	if hostRepoExists && forceDeleteHostRepo {
		fmt.Fprintf(os.Stderr, console.FormatInfoMessage("  %d. Delete and recreate host repository\n"), stepNum)
	} else if hostRepoExists {
		fmt.Fprintf(os.Stderr, console.FormatInfoMessage("  %d. Reuse existing host repository\n"), stepNum)
	} else {
		fmt.Fprintf(os.Stderr, console.FormatInfoMessage("  %d. Create a private host repository\n"), stepNum)
	}
	stepNum++

	// Step 2: Clone contents (only in clone-repo mode)
	if cloneRepoSlug != "" {
		if hostRepoExists && !forceDeleteHostRepo {
			fmt.Fprintf(os.Stderr, console.FormatInfoMessage("  %d. Force push contents from %s (overwriting existing content)\n"), stepNum, cloneRepoSlug)
		} else {
			fmt.Fprintf(os.Stderr, console.FormatInfoMessage("  %d. Clone contents from %s\n"), stepNum, cloneRepoSlug)
		}
		stepNum++

		// Show that workflows will be disabled
		if len(parsedSpecs) == 1 {
			fmt.Fprintf(os.Stderr, console.FormatInfoMessage("  %d. Disable all workflows in cloned repository except %s\n"), stepNum, parsedSpecs[0].WorkflowName)
		} else {
			workflowNames := make([]string, len(parsedSpecs))
			for i, spec := range parsedSpecs {
				workflowNames[i] = spec.WorkflowName
			}
			fmt.Fprintf(os.Stderr, console.FormatInfoMessage("  %d. Disable all workflows in cloned repository except: %s\n"), stepNum, strings.Join(workflowNames, ", "))
		}
		stepNum++
	}

	// Step 3/2: Install and compile workflows
	if len(parsedSpecs) == 1 {
		fmt.Fprintf(os.Stderr, console.FormatInfoMessage("  %d. Install and compile %s\n"), stepNum, parsedSpecs[0].WorkflowName)
	} else {
		workflowNames := make([]string, len(parsedSpecs))
		for i, spec := range parsedSpecs {
			workflowNames[i] = spec.WorkflowName
		}
		fmt.Fprintf(os.Stderr, console.FormatInfoMessage("  %d. Install and compile: %s\n"), stepNum, strings.Join(workflowNames, ", "))
	}
	stepNum++

	// Step 4/3: Push secrets (if enabled)
	if pushSecrets {
		fmt.Fprintf(os.Stderr, console.FormatInfoMessage("  %d. Push required API key secrets (will be cleaned up later)\n"), stepNum)
		stepNum++
	}

	// Step 5/4: Execute workflows and auto-merge (repeated if --repeat is used)
	if len(parsedSpecs) == 1 {
		workflowName := parsedSpecs[0].WorkflowName
		if repeatCount > 0 && autoMergePRs {
			fmt.Fprintf(os.Stderr, console.FormatInfoMessage("  %d. For each of %d executions:\n"), stepNum, repeatCount+1)
			fmt.Fprintf(os.Stderr, "     a. Execute %s\n", workflowName)
			fmt.Fprintf(os.Stderr, "     b. Auto-merge any pull requests created during execution\n")
		} else if repeatCount > 0 {
			fmt.Fprintf(os.Stderr, console.FormatInfoMessage("  %d. Execute %s %d times\n"), stepNum, workflowName, repeatCount+1)
		} else if autoMergePRs {
			fmt.Fprintf(os.Stderr, console.FormatInfoMessage("  %d. Execute %s\n"), stepNum, workflowName)
			stepNum++
			fmt.Fprintf(os.Stderr, console.FormatInfoMessage("  %d. Auto-merge any pull requests created during execution\n"), stepNum)
		} else {
			fmt.Fprintf(os.Stderr, console.FormatInfoMessage("  %d. Execute %s\n"), stepNum, workflowName)
		}
	} else {
		workflowNames := make([]string, len(parsedSpecs))
		for i, spec := range parsedSpecs {
			workflowNames[i] = spec.WorkflowName
		}
		workflowList := strings.Join(workflowNames, ", ")

		if repeatCount > 0 && autoMergePRs {
			fmt.Fprintf(os.Stderr, console.FormatInfoMessage("  %d. For each of %d executions:\n"), stepNum, repeatCount+1)
			fmt.Fprintf(os.Stderr, "     a. Execute: %s\n", workflowList)
			fmt.Fprintf(os.Stderr, "     b. Auto-merge any pull requests created during execution\n")
		} else if repeatCount > 0 {
			fmt.Fprintf(os.Stderr, console.FormatInfoMessage("  %d. Execute %d times: %s\n"), stepNum, repeatCount+1, workflowList)
		} else if autoMergePRs {
			fmt.Fprintf(os.Stderr, console.FormatInfoMessage("  %d. Execute: %s\n"), stepNum, workflowList)
			stepNum++
			fmt.Fprintf(os.Stderr, console.FormatInfoMessage("  %d. Auto-merge any pull requests created during execution\n"), stepNum)
		} else {
			fmt.Fprintf(os.Stderr, console.FormatInfoMessage("  %d. Execute: %s\n"), stepNum, workflowList)
		}
	}
	stepNum++

	// Step 6/5: Clean up secrets (if pushed)
	if pushSecrets {
		fmt.Fprintf(os.Stderr, console.FormatInfoMessage("  %d. Clean up API key secrets from the host repository\n"), stepNum)
		stepNum++
	}

	// Final step: Delete/preserve repository
	if deleteHostRepo {
		fmt.Fprintf(os.Stderr, console.FormatInfoMessage("  %d. Delete the host repository\n"), stepNum)
	} else {
		fmt.Fprintf(os.Stderr, console.FormatInfoMessage("  %d. Preserve the host repository for inspection\n"), stepNum)
	}

	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"))
	fmt.Fprintln(os.Stderr, "")

	// Ask for confirmation using console helper
	confirmed, err := console.ConfirmAction(
		"Do you want to continue?",
		"Yes, proceed",
		"No, cancel",
	)
	if err != nil {
		return fmt.Errorf("confirmation failed: %w", err)
	}

	if !confirmed {
		return fmt.Errorf("trial cancelled by user")
	}

	return nil
}

func triggerWorkflowRun(repoSlug, workflowName string, triggerContext string, verbose bool) (string, error) {
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Triggering workflow run for: %s", workflowName)))
	}

	// Trigger workflow using gh CLI
	lockFileName := fmt.Sprintf("%s.lock.yml", workflowName)

	// Build the command args
	args := []string{"workflow", "run", lockFileName, "--repo", repoSlug}

	// If trigger context is provided, extract issue number and add it as input
	if triggerContext != "" {
		issueNumber := parseIssueSpec(triggerContext)
		if issueNumber != "" {
			args = append(args, "--field", fmt.Sprintf("issue_number=%s", issueNumber))
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Using issue number %s from trigger context", issueNumber)))
			}
		} else if verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Could not extract issue number from trigger context, running without inputs"))
		}
	}

	output, err := workflow.RunGHCombined("Triggering workflow...", args...)

	if err != nil {
		return "", fmt.Errorf("failed to trigger workflow run: %w (output: %s)", err, string(output))
	}

	// Get the most recent run ID for this workflow using shared retry logic
	runInfo, err := getLatestWorkflowRunWithRetry(lockFileName, repoSlug, verbose)
	if err != nil {
		return "", fmt.Errorf("failed to get workflow run ID: %w", err)
	}

	runID := fmt.Sprintf("%d", runInfo.DatabaseID)

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("Workflow run started with ID: %s (status: %s)", runID, runInfo.Status)))
	}

	return runID, nil
}

// parseIssueSpec extracts the issue number from various formats
// Supports:
// - GitHub issue URLs: https://github.com/owner/repo/issues/123
// - Issue references: #123
// - Plain numbers: 123
func parseIssueSpec(input string) string {
	input = strings.TrimSpace(input)

	// First try to match GitHub issue URLs
	urlRegex := regexp.MustCompile(`https://github\.com/[^/]+/[^/]+/issues/(\d+)`)
	if matches := urlRegex.FindStringSubmatch(input); len(matches) >= 2 {
		return matches[1]
	}

	// Try to match issue references like #123
	refRegex := regexp.MustCompile(`^#(\d+)$`)
	if matches := refRegex.FindStringSubmatch(input); len(matches) >= 2 {
		return matches[1]
	}

	// Try to match plain numbers like 123
	numberRegex := regexp.MustCompile(`^\d+$`)
	if numberRegex.MatchString(input) {
		return input
	}

	return ""
}

// saveTrialResult saves a trial result to a JSON file
func saveTrialResult(filename string, result any, verbose bool) error {
	jsonBytes, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal result to JSON: %w", err)
	}

	if err := os.WriteFile(filename, jsonBytes, 0644); err != nil {
		return fmt.Errorf("failed to write result file: %w", err)
	}

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Saved trial result to: %s", filename)))
	}

	return nil
}

// copyTrialResultsToHostRepo copies trial result files to the host repository and commits them
func copyTrialResultsToHostRepo(tempDir, dateTimeID string, workflowNames []string, targetRepoSlug string, verbose bool) error {
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Copying trial results to host repository"))
	}

	// Create trials directory in the host repository
	trialsDir := filepath.Join(tempDir, "trials")
	if err := os.MkdirAll(trialsDir, 0755); err != nil {
		return fmt.Errorf("failed to create trials directory in repository: %w", err)
	}

	// Copy individual workflow result files
	sanitizedTargetRepo := repoutil.SanitizeForFilename(targetRepoSlug)
	for _, workflowName := range workflowNames {
		sourceFile := fmt.Sprintf("trials/%s-%s.%s.json", workflowName, sanitizedTargetRepo, dateTimeID)
		destFile := filepath.Join(trialsDir, fmt.Sprintf("%s-%s.%s.json", workflowName, sanitizedTargetRepo, dateTimeID))

		if err := fileutil.CopyFile(sourceFile, destFile); err != nil {
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to copy %s: %v", sourceFile, err)))
			}
			continue
		}

		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatVerboseMessage(fmt.Sprintf("Copied %s to repository", sourceFile)))
		}
	}

	// Copy combined results file if it exists (for multi-workflow trials)
	if len(workflowNames) > 1 {
		workflowNamesStr := strings.Join(workflowNames, "-")
		combinedSourceFile := fmt.Sprintf("trials/%s-%s.%s.json", workflowNamesStr, sanitizedTargetRepo, dateTimeID)
		combinedDestFile := filepath.Join(trialsDir, fmt.Sprintf("%s-%s.%s.json", workflowNamesStr, sanitizedTargetRepo, dateTimeID))

		if err := fileutil.CopyFile(combinedSourceFile, combinedDestFile); err != nil {
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to copy combined results: %v", err)))
			}
		} else if verbose {
			fmt.Fprintln(os.Stderr, console.FormatVerboseMessage(fmt.Sprintf("Copied %s to repository", combinedSourceFile)))
		}
	}

	// Change to temp directory to commit the changes
	originalDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}
	defer os.Chdir(originalDir)

	if err := os.Chdir(tempDir); err != nil {
		return fmt.Errorf("failed to change to temp directory: %w", err)
	}

	// Add trial results to git
	cmd := exec.Command("git", "add", "trials/")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to add trial results: %w (output: %s)", err, string(output))
	}

	// Check if there are any changes to commit
	statusCmd := exec.Command("git", "status", "--porcelain", "trials/")
	statusOutput, err := statusCmd.Output()
	if err != nil {
		return fmt.Errorf("failed to check git status: %w", err)
	}

	// If no changes, skip commit and push
	if len(strings.TrimSpace(string(statusOutput))) == 0 {
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("No new trial results to commit"))
		}
		return nil
	}

	// Commit trial results
	commitMsg := fmt.Sprintf("Add trial results for %s (%s)", strings.Join(workflowNames, ", "), dateTimeID)
	cmd = exec.Command("git", "commit", "-m", commitMsg)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to commit trial results: %w (output: %s)", err, string(output))
	}

	// Pull latest changes from main before pushing to avoid conflicts
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Pulling latest changes from main branch"))
	}
	cmd = exec.Command("git", "pull", "origin", "main")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to pull latest changes: %w (output: %s)", err, string(output))
	}

	// Push to main
	cmd = exec.Command("git", "push", "origin", "main")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to push trial results: %w (output: %s)", err, string(output))
	}

	fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Trial results copied to repository and pushed"))

	return nil
}
