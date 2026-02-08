package cli

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/workflow"
)

var initLog = logger.New("cli:init")

// InitRepositoryInteractive runs an interactive setup for the repository
func InitRepositoryInteractive(verbose bool, rootCmd CommandProvider) error {
	initLog.Print("Starting interactive repository initialization")

	// Assert this function is not running in automated unit tests
	if os.Getenv("GO_TEST_MODE") == "true" || os.Getenv("CI") != "" {
		return fmt.Errorf("interactive init cannot be used in automated tests or CI environments")
	}

	// Run shared precondition checks (same as `gh aw add`)
	// This verifies: gh auth, git repo, Actions enabled, user permissions
	preconditionResult, err := CheckInteractivePreconditions(verbose)
	if err != nil {
		return err
	}
	initLog.Printf("Precondition checks passed, repo: %s, isPublic: %v", preconditionResult.RepoSlug, preconditionResult.IsPublicRepo)

	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Welcome to GitHub Agentic Workflows setup!"))
	fmt.Fprintln(os.Stderr, "")

	// Prompt for engine selection
	var selectedEngine string

	// Use interactive prompt to select engine
	form := createEngineSelectionForm(&selectedEngine, constants.EngineOptions)
	if err := form.Run(); err != nil {
		return fmt.Errorf("engine selection failed: %w", err)
	}

	initLog.Printf("User selected engine: %s", selectedEngine)
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Configuring repository for %s engine...", selectedEngine)))
	fmt.Fprintln(os.Stderr, "")

	// Initialize repository with basic settings
	if err := initializeBasicRepository(verbose); err != nil {
		return err
	}

	// Configure engine-specific settings
	copilotMcp := false
	if selectedEngine == string(constants.CopilotEngine) {
		copilotMcp = true
		initLog.Print("Copilot engine selected, enabling MCP configuration")
	}

	// Configure MCP if copilot is selected
	if copilotMcp {
		initLog.Print("Configuring GitHub Copilot Agent MCP integration")

		// Detect action mode for setup steps generation
		actionMode := workflow.DetectActionMode(GetVersion())
		initLog.Printf("Using action mode for copilot-setup-steps.yml: %s", actionMode)

		// Create copilot-setup-steps.yml
		if err := ensureCopilotSetupSteps(verbose, actionMode, GetVersion()); err != nil {
			initLog.Printf("Failed to create copilot-setup-steps.yml: %v", err)
			return fmt.Errorf("failed to create copilot-setup-steps.yml: %w", err)
		}
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Created .github/workflows/copilot-setup-steps.yml"))
		}

		// Create .vscode/mcp.json
		if err := ensureMCPConfig(verbose); err != nil {
			initLog.Printf("Failed to create MCP config: %v", err)
			return fmt.Errorf("failed to create MCP config: %w", err)
		}
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Created .vscode/mcp.json"))
		}
	}

	// Configure VSCode settings
	initLog.Print("Configuring VSCode settings")
	if err := ensureVSCodeSettings(verbose); err != nil {
		initLog.Printf("Failed to update VSCode settings: %v", err)
		return fmt.Errorf("failed to update VSCode settings: %w", err)
	}
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Updated .vscode/settings.json"))
	}

	// Check and setup secrets for the selected engine
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Checking required secrets for the selected engine..."))
	fmt.Fprintln(os.Stderr, "")

	if err := setupEngineSecrets(selectedEngine, verbose); err != nil {
		// Secret setup is non-fatal, just warn the user
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Secret setup encountered an issue: %v", err)))
	}

	// Display success message
	initLog.Print("Interactive repository initialization completed successfully")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Repository initialized for agentic workflows!"))
	fmt.Fprintln(os.Stderr, "")
	if copilotMcp {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("GitHub Copilot Agent MCP integration configured"))
		fmt.Fprintln(os.Stderr, "")
	}
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage("To create a workflow, launch Copilot CLI: npx @github/copilot"))
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Then type /agent and select agentic-workflows"))
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Or add workflows from the catalog: "+string(constants.CLIExtensionPrefix)+" add <workflow-name>"))
	fmt.Fprintln(os.Stderr, "")

	return nil
}

// createEngineSelectionForm creates an interactive form for engine selection
func createEngineSelectionForm(selectedEngine *string, engineOptions []constants.EngineOption) *huh.Form {
	// Build options for huh.Select
	var options []huh.Option[string]
	for _, opt := range engineOptions {
		options = append(options, huh.NewOption(fmt.Sprintf("%s - %s", opt.Label, opt.Description), opt.Value))
	}

	return huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Which coding agent would you like to use?").
				Description("Select the coding agent that will power your agentic workflows").
				Options(options...).
				Value(selectedEngine),
		),
	).WithAccessible(console.IsAccessibleMode())
}

// initializeBasicRepository sets up the basic repository structure
func initializeBasicRepository(verbose bool) error {
	// Configure .gitattributes
	initLog.Print("Configuring .gitattributes")
	if err := ensureGitAttributes(); err != nil {
		initLog.Printf("Failed to configure .gitattributes: %v", err)
		return fmt.Errorf("failed to configure .gitattributes: %w", err)
	}
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Configured .gitattributes"))
	}

	// Write dispatcher agent
	initLog.Print("Writing agentic workflows dispatcher agent")
	if err := ensureAgenticWorkflowsDispatcher(verbose, false); err != nil {
		initLog.Printf("Failed to write dispatcher agent: %v", err)
		return fmt.Errorf("failed to write dispatcher agent: %w", err)
	}
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Created dispatcher agent"))
	}

	// Delete existing setup agentic workflows agent if it exists
	initLog.Print("Cleaning up setup agentic workflows agent")
	if err := deleteSetupAgenticWorkflowsAgent(verbose); err != nil {
		initLog.Printf("Failed to delete setup agentic workflows agent: %v", err)
		return fmt.Errorf("failed to delete setup agentic workflows agent: %w", err)
	}

	return nil
}

// setupEngineSecrets checks for engine-specific secrets and attempts to configure them
func setupEngineSecrets(engine string, verbose bool) error {
	initLog.Printf("Setting up secrets for engine: %s", engine)

	// Get current repository
	repoSlug, err := GetCurrentRepoSlug()
	if err != nil {
		initLog.Printf("Failed to get current repository: %v", err)
		return fmt.Errorf("failed to get current repository: %w", err)
	}

	// Get required secrets for the engine
	tokensToCheck := getRecommendedTokensForEngine(engine)

	// Check environment for secrets
	var availableSecrets []string
	var missingSecrets []tokenSpec

	for _, spec := range tokensToCheck {
		// Check if secret is available in environment
		secretValue := os.Getenv(spec.Name)

		// Try alternative environment variable names
		// NOTE: We intentionally do NOT use GetGitHubToken() fallback for COPILOT_GITHUB_TOKEN here.
		// The init command should only detect explicitly set environment variables, not scrape
		// the user's gh auth token. Using gh auth token for secrets would be a security risk
		// as users may not realize their personal token is being uploaded to the repository.
		// The trial command handles this differently with explicit warnings.
		if secretValue == "" {
			switch spec.Name {
			case "ANTHROPIC_API_KEY":
				secretValue = os.Getenv("ANTHROPIC_KEY")
			case "OPENAI_API_KEY":
				secretValue = os.Getenv("OPENAI_KEY")
			case "COPILOT_GITHUB_TOKEN":
				// Only check explicit environment variable, do NOT use gh auth token fallback
				// This prevents accidentally uploading user's personal token to the repository
				secretValue = os.Getenv("COPILOT_GITHUB_TOKEN")
			}
		}

		if secretValue != "" {
			availableSecrets = append(availableSecrets, spec.Name)
		} else {
			missingSecrets = append(missingSecrets, spec)
		}
	}

	// Display found secrets
	if len(availableSecrets) > 0 {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Found the following secrets in your environment:"))
		for _, secretName := range availableSecrets {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("  ✓ %s", secretName)))
		}
		fmt.Fprintln(os.Stderr, "")

		// Ask for confirmation before configuring secrets
		// SECURITY: Default to "No" to prevent accidental token uploads
		var confirmSetSecrets bool
		confirmForm := huh.NewForm(
			huh.NewGroup(
				huh.NewConfirm().
					Title("Would you like to configure these secrets as repository Actions secrets?").
					Description("This will upload the API keys or tokens as secrets in your repository. Default: No").
					Affirmative("Yes, configure secrets").
					Negative("No, skip").
					Value(&confirmSetSecrets),
			),
		).WithAccessible(console.IsAccessibleMode())

		if err := confirmForm.Run(); err != nil {
			return fmt.Errorf("confirmation failed: %w", err)
		}

		if !confirmSetSecrets {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Skipped configuring secrets"))
			fmt.Fprintln(os.Stderr, "")
		} else {
			// Attempt to configure them as repository secrets
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Configuring secrets for repository Actions..."))
			fmt.Fprintln(os.Stderr, "")

			successCount := 0
			for _, secretName := range availableSecrets {
				if err := attemptSetSecret(secretName, repoSlug, verbose); err != nil {
					// Handle different types of errors gracefully
					errMsg := err.Error()
					if strings.Contains(errMsg, "403") || strings.Contains(errMsg, "Forbidden") ||
						strings.Contains(errMsg, "permissions") || strings.Contains(errMsg, "Resource not accessible") {
						fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("  ✗ Insufficient permissions to set %s", secretName)))
						fmt.Fprintln(os.Stderr, console.FormatInfoMessage("    You may need to grant additional permissions to your GitHub token"))
					} else {
						fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("  ✗ Failed to set %s: %v", secretName, err)))
					}
				} else {
					successCount++
					fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("  ✓ Configured %s", secretName)))
				}
			}

			if successCount > 0 {
				fmt.Fprintln(os.Stderr, "")
				fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("Successfully configured %d secret(s) for repository Actions", successCount)))
			} else {
				fmt.Fprintln(os.Stderr, "")
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage("No secrets were configured. You may need to set them manually."))
			}
		}
	}

	// Display missing secrets
	if len(missingSecrets) > 0 {
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage("The following required secrets are not available in your environment:"))

		parts := splitRepoSlug(repoSlug)
		cmdOwner := parts[0]
		cmdRepo := parts[1]

		for _, spec := range missingSecrets {
			fmt.Fprintln(os.Stderr, "")
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("  ✗ %s", spec.Name)))
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("    When needed: %s", spec.When)))
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("    Description: %s", spec.Description)))
			fmt.Fprintln(os.Stderr, console.FormatCommandMessage(fmt.Sprintf("    gh aw secrets set %s --owner %s --repo %s", spec.Name, cmdOwner, cmdRepo)))
		}
		fmt.Fprintln(os.Stderr, "")
	}

	return nil
}

// attemptSetSecret attempts to set a secret for the repository
func attemptSetSecret(secretName, repoSlug string, verbose bool) error {
	initLog.Printf("Attempting to set secret: %s for repo: %s", secretName, repoSlug)

	// Check if secret already exists
	exists, err := checkSecretExistsInRepo(secretName, repoSlug)
	if err != nil {
		// If we get a permission error, return it immediately
		if strings.Contains(err.Error(), "403") || strings.Contains(err.Error(), "Forbidden") {
			return fmt.Errorf("insufficient permissions to access repository secrets: %w", err)
		}
		// For other errors, log but try to set anyway
		if verbose {
			initLog.Printf("Could not check if secret exists: %v", err)
		}
	} else if exists {
		// Secret already exists, skip
		if verbose {
			initLog.Printf("Secret %s already exists, skipping", secretName)
		}
		return nil
	}

	// Get secret value from environment
	// NOTE: We intentionally do NOT use GetGitHubToken() fallback for COPILOT_GITHUB_TOKEN here.
	// The init command should only use explicitly set environment variables to avoid
	// accidentally uploading the user's personal gh auth token to the repository.
	secretValue := os.Getenv(secretName)
	if secretValue == "" {
		// Try alternative names (but NOT gh auth token fallback for security)
		switch secretName {
		case "ANTHROPIC_API_KEY":
			secretValue = os.Getenv("ANTHROPIC_KEY")
		case "OPENAI_API_KEY":
			secretValue = os.Getenv("OPENAI_KEY")
		case "COPILOT_GITHUB_TOKEN":
			// Only check explicit environment variable, do NOT use gh auth token fallback
			// This prevents accidentally uploading user's personal token to the repository
			secretValue = os.Getenv("COPILOT_GITHUB_TOKEN")
		}
	}

	if secretValue == "" {
		return fmt.Errorf("secret value not found in environment")
	}

	// Set the secret using gh CLI
	if output, err := workflow.RunGHCombined("Setting secret...", "secret", "set", secretName, "--repo", repoSlug, "--body", secretValue); err != nil {
		outputStr := string(output)
		// Check for permission-related errors
		if strings.Contains(outputStr, "403") || strings.Contains(outputStr, "Forbidden") ||
			strings.Contains(outputStr, "Resource not accessible") || strings.Contains(err.Error(), "403") {
			return fmt.Errorf("insufficient permissions to set secrets in repository: %w", err)
		}
		return fmt.Errorf("failed to set secret: %w (output: %s)", err, outputStr)
	}

	initLog.Printf("Successfully set secret: %s", secretName)
	return nil
}

// InitRepository initializes the repository for agentic workflows
func InitRepository(verbose bool, mcp bool, tokens bool, engine string, codespaceRepos []string, codespaceEnabled bool, completions bool, push bool, shouldCreatePR bool, rootCmd CommandProvider) error {
	initLog.Print("Starting repository initialization for agentic workflows")

	// If --push or --create-pull-request is enabled, ensure git status is clean before starting
	if push || shouldCreatePR {
		if shouldCreatePR {
			initLog.Print("Checking for clean working directory (--create-pull-request enabled)")
		} else {
			initLog.Print("Checking for clean working directory (--push enabled)")
		}
		if err := checkCleanWorkingDirectory(verbose); err != nil {
			initLog.Printf("Git status check failed: %v", err)
			if shouldCreatePR {
				return fmt.Errorf("--create-pull-request requires a clean working directory: %w", err)
			}
			return fmt.Errorf("--push requires a clean working directory: %w", err)
		}
	}

	// If creating a PR, check GitHub CLI is available
	if shouldCreatePR {
		if !isGHCLIAvailable() {
			return fmt.Errorf("GitHub CLI (gh) is required for PR creation but not available")
		}
	}

	// Ensure we're in a git repository
	if !isGitRepo() {
		initLog.Print("Not in a git repository, initialization failed")
		return fmt.Errorf("not in a git repository")
	}
	initLog.Print("Verified git repository")

	// Configure .gitattributes
	initLog.Print("Configuring .gitattributes")
	if err := ensureGitAttributes(); err != nil {
		initLog.Printf("Failed to configure .gitattributes: %v", err)
		return fmt.Errorf("failed to configure .gitattributes: %w", err)
	}
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Configured .gitattributes"))
	}

	// Write dispatcher agent
	initLog.Print("Writing agentic workflows dispatcher agent")
	if err := ensureAgenticWorkflowsDispatcher(verbose, false); err != nil {
		initLog.Printf("Failed to write dispatcher agent: %v", err)
		return fmt.Errorf("failed to write dispatcher agent: %w", err)
	}
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Created dispatcher agent"))
	}

	// Delete existing setup agentic workflows agent if it exists
	initLog.Print("Cleaning up setup agentic workflows agent")
	if err := deleteSetupAgenticWorkflowsAgent(verbose); err != nil {
		initLog.Printf("Failed to delete setup agentic workflows agent: %v", err)
		return fmt.Errorf("failed to delete setup agentic workflows agent: %w", err)
	}

	// Configure MCP if requested
	if mcp {
		initLog.Print("Configuring GitHub Copilot Agent MCP integration")

		// Detect action mode for setup steps generation
		actionMode := workflow.DetectActionMode(GetVersion())
		initLog.Printf("Using action mode for copilot-setup-steps.yml: %s", actionMode)

		// Create copilot-setup-steps.yml
		if err := ensureCopilotSetupSteps(verbose, actionMode, GetVersion()); err != nil {
			initLog.Printf("Failed to create copilot-setup-steps.yml: %v", err)
			return fmt.Errorf("failed to create copilot-setup-steps.yml: %w", err)
		}
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Created .github/workflows/copilot-setup-steps.yml"))
		}

		// Create .vscode/mcp.json
		if err := ensureMCPConfig(verbose); err != nil {
			initLog.Printf("Failed to create MCP config: %v", err)
			return fmt.Errorf("failed to create MCP config: %w", err)
		}
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Created .vscode/mcp.json"))
		}
	}

	// Configure Codespaces if requested
	if codespaceEnabled {
		initLog.Printf("Configuring GitHub Codespaces devcontainer with additional repos: %v", codespaceRepos)

		// Create or update .devcontainer/devcontainer.json
		if err := ensureDevcontainerConfig(verbose, codespaceRepos); err != nil {
			initLog.Printf("Failed to configure devcontainer: %v", err)
			return fmt.Errorf("failed to configure devcontainer: %w", err)
		}
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Configured .devcontainer/devcontainer.json"))
		}
	}

	// Configure VSCode settings
	initLog.Print("Configuring VSCode settings")

	// Update .vscode/settings.json
	if err := ensureVSCodeSettings(verbose); err != nil {
		initLog.Printf("Failed to update VSCode settings: %v", err)
		return fmt.Errorf("failed to update VSCode settings: %w", err)
	}
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Updated .vscode/settings.json"))
	}

	// Validate tokens if requested
	if tokens {
		initLog.Print("Validating repository secrets for agentic workflows")
		fmt.Fprintln(os.Stderr, "")

		// Run token bootstrap validation
		if err := runTokensBootstrap(engine, "", ""); err != nil {
			initLog.Printf("Token validation failed: %v", err)
			// Don't fail init if token validation has issues
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Token validation encountered an issue: %v", err)))
		}
		fmt.Fprintln(os.Stderr, "")
	}

	// Install shell completions if requested
	if completions {
		initLog.Print("Installing shell completions")
		fmt.Fprintln(os.Stderr, "")

		if err := InstallShellCompletion(verbose, rootCmd); err != nil {
			initLog.Printf("Shell completion installation failed: %v", err)
			// Don't fail init if completion installation has issues
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Shell completion installation encountered an issue: %v", err)))
		}
		fmt.Fprintln(os.Stderr, "")
	}

	// Generate/update maintenance workflow if any workflows use expires field
	initLog.Print("Checking for workflows with expires field to generate maintenance workflow")
	if err := ensureMaintenanceWorkflow(verbose); err != nil {
		initLog.Printf("Failed to generate maintenance workflow: %v", err)
		// Don't fail init if maintenance workflow generation has issues
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to generate maintenance workflow: %v", err)))
	}

	initLog.Print("Repository initialization completed successfully")

	// If --create-pull-request is enabled, create branch, commit, push, and create PR
	if shouldCreatePR {
		initLog.Print("Create PR enabled - preparing to create branch, commit, push, and create PR")
		fmt.Fprintln(os.Stderr, "")

		// Get current branch for restoration later
		currentBranch, err := getCurrentBranch()
		if err != nil {
			return fmt.Errorf("failed to get current branch: %w", err)
		}

		// Create temporary branch
		branchName := fmt.Sprintf("init-agentic-workflows-%d", rand.Intn(9000)+1000)
		if err := createAndSwitchBranch(branchName, verbose); err != nil {
			return fmt.Errorf("failed to create branch %s: %w", branchName, err)
		}

		// Commit changes
		commitMessage := "chore: initialize agentic workflows"
		if err := commitChanges(commitMessage, verbose); err != nil {
			// Switch back to original branch before returning error
			_ = switchBranch(currentBranch, verbose)
			return fmt.Errorf("failed to commit changes: %w", err)
		}

		// Push branch
		if err := pushBranch(branchName, verbose); err != nil {
			// Switch back to original branch before returning error
			_ = switchBranch(currentBranch, verbose)
			return fmt.Errorf("failed to push branch %s: %w", branchName, err)
		}

		// Create PR
		prTitle := "Initialize agentic workflows"
		prBody := "This PR initializes the repository for agentic workflows by:\n" +
			"- Configuring .gitattributes\n" +
			"- Creating GitHub Copilot custom instructions\n" +
			"- Setting up workflow prompts and agents"
		if _, _, err := createPR(branchName, prTitle, prBody, verbose); err != nil {
			// Switch back to original branch before returning error
			_ = switchBranch(currentBranch, verbose)
			return fmt.Errorf("failed to create PR: %w", err)
		}

		// Switch back to original branch
		if err := switchBranch(currentBranch, verbose); err != nil {
			return fmt.Errorf("failed to switch back to branch %s: %w", currentBranch, err)
		}

		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Created PR for initialization"))
	} else if push {
		// If --push is enabled, commit and push changes
		initLog.Print("Push enabled - preparing to commit and push changes")
		fmt.Fprintln(os.Stderr, "")

		// Check if we're on the default branch
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Checking current branch..."))
		if err := checkOnDefaultBranch(verbose); err != nil {
			initLog.Printf("Default branch check failed: %v", err)
			return fmt.Errorf("cannot push: %w", err)
		}

		// Confirm with user (skip in CI)
		if err := confirmPushOperation(verbose); err != nil {
			initLog.Printf("Push operation not confirmed: %v", err)
			return fmt.Errorf("push operation cancelled: %w", err)
		}

		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Preparing to commit and push changes..."))

		// Use the helper function to orchestrate the full workflow
		commitMessage := "chore: initialize agentic workflows"
		if err := commitAndPushChanges(commitMessage, verbose); err != nil {
			// Check if it's the "no changes" case
			hasChanges, checkErr := hasChangesToCommit()
			if checkErr == nil && !hasChanges {
				initLog.Print("No changes to commit")
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage("No changes to commit"))
			} else {
				return err
			}
		} else {
			// Print success messages based on whether remote exists
			fmt.Fprintln(os.Stderr, "")
			if hasRemote() {
				fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("✓ Changes pushed to remote"))
			} else {
				fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("✓ Changes committed locally (no remote configured)"))
			}
		}
	}

	// Display success message with next steps
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Repository initialized for agentic workflows!"))
	fmt.Fprintln(os.Stderr, "")
	if mcp {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("GitHub Copilot Agent MCP integration configured"))
		fmt.Fprintln(os.Stderr, "")
	}
	if len(codespaceRepos) > 0 {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("GitHub Codespaces devcontainer configured"))
		fmt.Fprintln(os.Stderr, "")
	}
	if tokens {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("To configure missing secrets, use: gh aw secret set <secret-name> --owner <owner> --repo <repo>"))
		fmt.Fprintln(os.Stderr, "")
	}
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage("To create a workflow, launch Copilot CLI: npx @github/copilot"))
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Then type /agent and select agentic-workflows"))
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Or add workflows from the catalog: "+string(constants.CLIExtensionPrefix)+" add <workflow-name>"))
	fmt.Fprintln(os.Stderr, "")

	return nil
}

// ensureMaintenanceWorkflow checks existing workflows for expires field and generates/updates
// the maintenance workflow file if any workflows use it
func ensureMaintenanceWorkflow(verbose bool) error {
	initLog.Print("Checking for workflows with expires field")

	// Find git root
	gitRoot, err := findGitRoot()
	if err != nil {
		return fmt.Errorf("failed to find git root: %w", err)
	}

	// Determine the workflows directory
	workflowsDir := filepath.Join(gitRoot, ".github", "workflows")
	if _, err := os.Stat(workflowsDir); os.IsNotExist(err) {
		// No workflows directory yet, skip maintenance workflow generation
		initLog.Print("No workflows directory found, skipping maintenance workflow generation")
		return nil
	}

	// Find all workflow markdown files
	files, err := filepath.Glob(filepath.Join(workflowsDir, "*.md"))
	if err != nil {
		return fmt.Errorf("failed to find workflow files: %w", err)
	}

	// Filter out README.md files
	files = filterWorkflowFiles(files)

	// Create a compiler to parse workflows (version and action mode auto-detected)
	compiler := workflow.NewCompiler()
	initLog.Printf("Action mode detected for maintenance workflow: %s", compiler.GetActionMode())

	// Parse all workflows to collect WorkflowData
	var workflowDataList []*workflow.WorkflowData
	for _, file := range files {
		initLog.Printf("Parsing workflow: %s", file)
		workflowData, err := compiler.ParseWorkflowFile(file)
		if err != nil {
			// Ignore parse errors - workflows might be incomplete during init
			initLog.Printf("Skipping workflow %s due to parse error: %v", file, err)
			continue
		}

		workflowDataList = append(workflowDataList, workflowData)
	}

	// Always call GenerateMaintenanceWorkflow even with empty list
	// This allows it to delete existing maintenance workflow if no workflows have expires
	initLog.Printf("Generating maintenance workflow for %d workflows", len(workflowDataList))
	if err := workflow.GenerateMaintenanceWorkflow(workflowDataList, workflowsDir, GetVersion(), compiler.GetActionMode(), verbose); err != nil {
		return fmt.Errorf("failed to generate maintenance workflow: %w", err)
	}

	if verbose && len(workflowDataList) > 0 {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Generated/updated maintenance workflow"))
	}

	return nil
}
