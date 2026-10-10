package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/sliceutil"
	"github.com/github/gh-aw/pkg/workflow"
)

var trialLog = logger.New("cli:trial_command")

// RunWorkflowTrials executes the main logic for trialing one or more workflows
func RunWorkflowTrials(ctx context.Context, workflowSpecs []string, opts TrialOptions) error {
	trialLog.Printf("Starting trial execution: specs=%v, logicalRepo=%s, cloneRepo=%s, hostRepo=%s, repeat=%d", workflowSpecs, opts.Repos.LogicalRepo, opts.Repos.CloneRepo, opts.Repos.HostRepo, opts.RepeatCount)

	// Show welcome banner for interactive mode
	console.ShowWelcomeBanner("This tool will run a trial of your workflow in a test repository.")

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
		workflowNames := sliceutil.Map(parsedSpecs, func(spec *WorkflowSpec) string { return spec.WorkflowName })
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
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Target repository (specified): "+logicalRepoSlug))
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
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Target repository (current): "+logicalRepoSlug))
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
		username, err := getCurrentGitHubUsername(ctx)
		if err != nil {
			return fmt.Errorf("failed to get GitHub username for default trial repo: %w", err)
		}
		hostRepoSlug = username + "/gh-aw-trial"
		trialLog.Printf("Using default host repository: %s", hostRepoSlug)
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Host repository (default): "+hostRepoSlug))
	}

	// Step 1.5: Show confirmation unless quiet mode
	if !opts.Quiet {
		if err := showTrialConfirmation(trialConfirmationOptions{
			parsedSpecs:         parsedSpecs,
			logicalRepoSlug:     logicalRepoSlug,
			cloneRepoSlug:       cloneRepoSlug,
			hostRepoSlug:        hostRepoSlug,
			deleteHostRepo:      opts.DeleteHostRepo,
			forceDeleteHostRepo: opts.ForceDelete,
			autoMergePRs:        opts.AutoMergePRs,
			repeatCount:         opts.RepeatCount,
			directTrialMode:     directTrialMode,
			engineOverride:      opts.EngineOverride,
		}); err != nil {
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

	// Step 2.5: Ensure engine secrets are configured when an explicit engine override is provided
	// When no override is specified, the workflow will use its frontmatter engine and handle secrets during compilation
	if opts.EngineOverride != "" {
		// Check what secrets already exist in the repository
		existingSecrets, err := getExistingSecretsInRepo(hostRepoSlug)
		if err != nil {
			trialLog.Printf("Warning: could not check existing secrets: %v", err)
			existingSecrets = make(map[string]struct {
			})
		}

		// Ensure the required engine secret is available (prompts interactively if needed)
		secretConfig := EngineSecretConfig{
			Ctx:                  ctx,
			RepoSlug:             hostRepoSlug,
			Engine:               opts.EngineOverride,
			Verbose:              opts.Verbose,
			ExistingSecrets:      existingSecrets,
			IncludeSystemSecrets: false,
			IncludeOptional:      false,
		}
		if err := checkAndEnsureEngineSecretsForEngine(secretConfig); err != nil {
			return fmt.Errorf("failed to configure engine secret: %w", err)
		}
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

	// Execute trials with optional repeat functionality
	return ExecuteWithRepeat(RepeatOptions{
		RepeatCount:   opts.RepeatCount,
		RepeatMessage: "Repeating trial run",
		ExecuteFunc: func() error {
			return executeTrialRun(ctx, parsedSpecs, hostRepoSlug, logicalRepoSlug, cloneRepoSlug, directTrialMode, opts)
		},
		CleanupFunc: func() {
			if opts.DeleteHostRepo {
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Host repository will be cleaned up"))
			} else {
				githubHost := getGitHubHost()
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Host repository preserved: %s/%s", githubHost, hostRepoSlug)))
			}
		},
		UseStderr: true,
	})

}

// getCurrentGitHubUsername gets the current GitHub username from gh CLI
func getCurrentGitHubUsername(ctx context.Context) (string, error) {
	output, err := workflow.RunGHContext(ctx, "Fetching GitHub username...", "api", "user", "--jq", ".login")
	if err != nil {
		return "", fmt.Errorf("failed to get GitHub username: %w", err)
	}

	username := strings.TrimSpace(string(output))
	if username == "" {
		return "", errors.New("GitHub username is empty")
	}

	return username, nil
}
