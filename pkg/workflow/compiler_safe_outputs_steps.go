package workflow

import (
	"fmt"

	"github.com/githubnext/gh-aw/pkg/logger"
)

var consolidatedSafeOutputsStepsLog = logger.New("workflow:compiler_safe_outputs_steps")

// buildConsolidatedSafeOutputStep builds a single step for a safe output operation
// within the consolidated safe-outputs job. This function handles both inline script
// mode and file mode (requiring from local filesystem).
func (c *Compiler) buildConsolidatedSafeOutputStep(data *WorkflowData, config SafeOutputStepConfig) []string {
	var steps []string

	// Build step condition if provided
	var conditionStr string
	if config.Condition != nil {
		conditionStr = config.Condition.Render()
	}

	// Step name and metadata
	steps = append(steps, fmt.Sprintf("      - name: %s\n", config.StepName))
	steps = append(steps, fmt.Sprintf("        id: %s\n", config.StepID))
	if conditionStr != "" {
		steps = append(steps, fmt.Sprintf("        if: %s\n", conditionStr))
	}
	steps = append(steps, fmt.Sprintf("        uses: %s\n", GetActionPin("actions/github-script")))

	// Environment variables section
	steps = append(steps, "        env:\n")
	steps = append(steps, "          GH_AW_AGENT_OUTPUT: ${{ env.GH_AW_AGENT_OUTPUT }}\n")
	steps = append(steps, config.CustomEnvVars...)

	// Add custom safe output env vars
	c.addCustomSafeOutputEnvVars(&steps, data)

	// With section for github-token
	steps = append(steps, "        with:\n")
	if config.UseAgentToken {
		c.addSafeOutputAgentGitHubTokenForConfig(&steps, data, config.Token)
	} else if config.UseCopilotToken {
		c.addSafeOutputCopilotGitHubTokenForConfig(&steps, data, config.Token)
	} else {
		c.addSafeOutputGitHubTokenForConfig(&steps, data, config.Token)
	}

	steps = append(steps, "          script: |\n")

	// Add the formatted JavaScript script
	// Use require mode if ScriptName is set, otherwise inline the bundled script
	if config.ScriptName != "" {
		// Require mode: Use setup_globals helper
		steps = append(steps, "            const { setupGlobals } = require('"+SetupActionDestination+"/setup_globals.cjs');\n")
		steps = append(steps, "            setupGlobals(core, github, context, exec, io);\n")
		steps = append(steps, fmt.Sprintf("            const { main } = require('"+SetupActionDestination+"/%s.cjs');\n", config.ScriptName))
		steps = append(steps, "            await main();\n")
	} else {
		// Inline JavaScript: Use setup_globals helper
		steps = append(steps, "            const { setupGlobals } = require('"+SetupActionDestination+"/setup_globals.cjs');\n")
		steps = append(steps, "            setupGlobals(core, github, context, exec, io);\n")
		// Inline mode: embed the bundled script directly
		formattedScript := FormatJavaScriptForYAML(config.Script)
		steps = append(steps, formattedScript...)
	}

	return steps
}

// buildSharedPRCheckoutSteps builds checkout and git configuration steps that are shared
// between create-pull-request and push-to-pull-request-branch operations.
// These steps are added once with a combined condition to avoid duplication.
func (c *Compiler) buildSharedPRCheckoutSteps(data *WorkflowData) []string {
	consolidatedSafeOutputsStepsLog.Print("Building shared PR checkout steps")
	var steps []string

	// Determine which token to use for checkout
	var checkoutToken string
	var gitRemoteToken string
	if data.SafeOutputs.App != nil {
		// nolint:gosec // G101: False positive - this is a GitHub Actions expression template placeholder, not a hardcoded credential
		checkoutToken = "${{ steps.safe-outputs-app-token.outputs.token }}" //nolint:gosec
		// nolint:gosec // G101: False positive - this is a GitHub Actions expression template placeholder, not a hardcoded credential
		gitRemoteToken = "${{ steps.safe-outputs-app-token.outputs.token }}"
	} else {
		// nolint:gosec // G101: False positive - this is a GitHub Actions expression template placeholder, not a hardcoded credential
		checkoutToken = "${{ github.token }}"
		// nolint:gosec // G101: False positive - this is a GitHub Actions expression template placeholder, not a hardcoded credential
		gitRemoteToken = "${{ github.token }}"
	}

	// Build combined condition: execute if either create_pull_request or push_to_pull_request_branch will run
	var condition ConditionNode
	if data.SafeOutputs.CreatePullRequests != nil && data.SafeOutputs.PushToPullRequestBranch != nil {
		// Both enabled: combine conditions with OR
		condition = BuildOr(
			BuildSafeOutputType("create_pull_request"),
			BuildSafeOutputType("push_to_pull_request_branch"),
		)
	} else if data.SafeOutputs.CreatePullRequests != nil {
		// Only create_pull_request
		condition = BuildSafeOutputType("create_pull_request")
	} else {
		// Only push_to_pull_request_branch
		condition = BuildSafeOutputType("push_to_pull_request_branch")
	}

	// Step 1: Checkout repository with conditional execution
	steps = append(steps, "      - name: Checkout repository\n")
	steps = append(steps, fmt.Sprintf("        if: %s\n", condition.Render()))
	steps = append(steps, fmt.Sprintf("        uses: %s\n", GetActionPin("actions/checkout")))
	steps = append(steps, "        with:\n")
	steps = append(steps, fmt.Sprintf("          token: %s\n", checkoutToken))
	steps = append(steps, "          persist-credentials: false\n")
	steps = append(steps, "          fetch-depth: 1\n")
	if c.trialMode {
		if c.trialLogicalRepoSlug != "" {
			steps = append(steps, fmt.Sprintf("          repository: %s\n", c.trialLogicalRepoSlug))
		}
	}

	// Step 2: Configure Git credentials with conditional execution
	// Security: Pass GitHub token through environment variable to prevent template injection
	gitConfigSteps := []string{
		"      - name: Configure Git credentials\n",
		fmt.Sprintf("        if: %s\n", condition.Render()),
		"        env:\n",
		"          REPO_NAME: ${{ github.repository }}\n",
		"          SERVER_URL: ${{ github.server_url }}\n",
		fmt.Sprintf("          GIT_TOKEN: %s\n", gitRemoteToken),
		"        run: |\n",
		"          git config --global user.email \"github-actions[bot]@users.noreply.github.com\"\n",
		"          git config --global user.name \"github-actions[bot]\"\n",
		"          # Re-authenticate git with GitHub token\n",
		"          SERVER_URL_STRIPPED=\"${SERVER_URL#https://}\"\n",
		"          git remote set-url origin \"https://x-access-token:${GIT_TOKEN}@${SERVER_URL_STRIPPED}/${REPO_NAME}.git\"\n",
		"          echo \"Git configured with standard GitHub Actions identity\"\n",
	}
	steps = append(steps, gitConfigSteps...)

	consolidatedSafeOutputsStepsLog.Printf("Added shared checkout with condition: %s", condition.Render())
	return steps
}

// buildHandlerManagerStep builds a single step that uses the safe output handler manager
// to dispatch messages to appropriate handlers. This replaces multiple individual steps
// with a single dispatcher step that processes all safe output types.
func (c *Compiler) buildHandlerManagerStep(data *WorkflowData) []string {
	consolidatedSafeOutputsStepsLog.Print("Building handler manager step")

	var steps []string

	// Step name and metadata
	steps = append(steps, "      - name: Process Safe Outputs\n")
	steps = append(steps, "        id: process_safe_outputs\n")
	steps = append(steps, fmt.Sprintf("        uses: %s\n", GetActionPin("actions/github-script")))

	// Environment variables
	steps = append(steps, "        env:\n")
	steps = append(steps, "          GH_AW_AGENT_OUTPUT: ${{ env.GH_AW_AGENT_OUTPUT }}\n")

	// Check if any project-handler types are enabled
	// If so, pass the temporary project map from the project handler step
	hasProjectHandlerTypes := data.SafeOutputs.CreateProjects != nil ||
		data.SafeOutputs.CreateProjectStatusUpdates != nil ||
		data.SafeOutputs.UpdateProjects != nil ||
		data.SafeOutputs.CopyProjects != nil

	if hasProjectHandlerTypes {
		// If project handler ran before this, pass its temporary project map
		// This allows update_issue and other text-based handlers to resolve project temporary IDs
		steps = append(steps, "          GH_AW_TEMPORARY_PROJECT_MAP: ${{ steps.process_project_safe_outputs.outputs.temporary_project_map }}\n")
	}

	// Add custom safe output env vars
	c.addCustomSafeOutputEnvVars(&steps, data)

	// Add handler manager config as JSON
	c.addHandlerManagerConfigEnvVar(&steps, data)

	// Add all safe output configuration env vars (still needed by individual handlers)
	c.addAllSafeOutputConfigEnvVars(&steps, data)

	// With section for github-token
	// Use the standard safe outputs token for all operations
	// Project-specific handlers (create_project) will use custom tokens from their handler config
	steps = append(steps, "        with:\n")
	c.addSafeOutputGitHubTokenForConfig(&steps, data, "")

	steps = append(steps, "          script: |\n")
	steps = append(steps, "            const { setupGlobals } = require('"+SetupActionDestination+"/setup_globals.cjs');\n")
	steps = append(steps, "            setupGlobals(core, github, context, exec, io);\n")
	steps = append(steps, "            const { main } = require('"+SetupActionDestination+"/safe_output_handler_manager.cjs');\n")
	steps = append(steps, "            await main();\n")

	return steps
}

// buildProjectHandlerManagerStep builds a single step that uses the safe output project handler manager
// to dispatch project-related messages (create_project, update_project, copy_project, create_project_status_update) to appropriate handlers.
// These types require GH_AW_PROJECT_GITHUB_TOKEN and are separated from the main handler manager.
func (c *Compiler) buildProjectHandlerManagerStep(data *WorkflowData) []string {
	consolidatedSafeOutputsStepsLog.Print("Building project handler manager step")

	var steps []string

	// Step name and metadata
	steps = append(steps, "      - name: Process Project-Related Safe Outputs\n")
	steps = append(steps, "        id: process_project_safe_outputs\n")
	steps = append(steps, fmt.Sprintf("        uses: %s\n", GetActionPin("actions/github-script")))

	// Environment variables
	steps = append(steps, "        env:\n")
	steps = append(steps, "          GH_AW_AGENT_OUTPUT: ${{ env.GH_AW_AGENT_OUTPUT }}\n")

	// Add project handler manager config as JSON
	c.addProjectHandlerManagerConfigEnvVar(&steps, data)

	// Add custom safe output env vars
	c.addCustomSafeOutputEnvVars(&steps, data)

	// Add all safe output configuration env vars (still needed by individual handlers)
	c.addAllSafeOutputConfigEnvVars(&steps, data)

	// Add GH_AW_PROJECT_GITHUB_TOKEN - this is the critical difference from the main handler manager
	// Project operations require this special token that has Projects permissions
	// Determine which custom token to use: check all project-related types
	var customToken string
	if data.SafeOutputs.CreateProjects != nil && data.SafeOutputs.CreateProjects.GitHubToken != "" {
		customToken = data.SafeOutputs.CreateProjects.GitHubToken
	} else if data.SafeOutputs.CreateProjectStatusUpdates != nil && data.SafeOutputs.CreateProjectStatusUpdates.GitHubToken != "" {
		customToken = data.SafeOutputs.CreateProjectStatusUpdates.GitHubToken
	} else if data.SafeOutputs.UpdateProjects != nil && data.SafeOutputs.UpdateProjects.GitHubToken != "" {
		customToken = data.SafeOutputs.UpdateProjects.GitHubToken
	} else if data.SafeOutputs.CopyProjects != nil && data.SafeOutputs.CopyProjects.GitHubToken != "" {
		customToken = data.SafeOutputs.CopyProjects.GitHubToken
	}
	token := getEffectiveProjectGitHubToken(customToken, data.GitHubToken)
	steps = append(steps, fmt.Sprintf("          GH_AW_PROJECT_GITHUB_TOKEN: %s\n", token))

	// Add GH_AW_PROJECT_URL if project is configured in frontmatter
	// This provides a default project URL for update-project and create-project-status-update operations
	// when target=context (or target not specified). Users can override by setting target=* and
	// providing an explicit project field in the safe output message.
	if data.ParsedFrontmatter != nil && data.ParsedFrontmatter.Project != nil && data.ParsedFrontmatter.Project.URL != "" {
		consolidatedSafeOutputsStepsLog.Printf("Adding GH_AW_PROJECT_URL environment variable: %s", data.ParsedFrontmatter.Project.URL)
		steps = append(steps, fmt.Sprintf("          GH_AW_PROJECT_URL: %q\n", data.ParsedFrontmatter.Project.URL))
	}

	// With section for github-token
	// Use the project token for authentication
	steps = append(steps, "        with:\n")
	steps = append(steps, fmt.Sprintf("          github-token: %s\n", token))

	steps = append(steps, "          script: |\n")
	steps = append(steps, "            const { setupGlobals } = require('"+SetupActionDestination+"/setup_globals.cjs');\n")
	steps = append(steps, "            setupGlobals(core, github, context, exec, io);\n")
	steps = append(steps, "            const { main } = require('"+SetupActionDestination+"/safe_output_project_handler_manager.cjs');\n")
	steps = append(steps, "            await main();\n")

	return steps
}
