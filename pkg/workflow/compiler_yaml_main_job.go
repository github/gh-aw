package workflow

import (
	"encoding/json"
	"fmt"
	"strings"
)

// generateMainJobSteps generates the complete sequence of steps for the main agent execution job
// This is the heart of the workflow, orchestrating all steps from checkout through AI execution to artifact upload
func (c *Compiler) generateMainJobSteps(yaml *strings.Builder, data *WorkflowData) error {
	compilerYamlLog.Printf("Generating main job steps for workflow: %s", data.Name)

	// Determine if we need to add a checkout step
	needsCheckout := c.shouldAddCheckoutStep(data)
	compilerYamlLog.Printf("Checkout step needed: %t", needsCheckout)

	// Add checkout step first if needed
	if needsCheckout {
		yaml.WriteString("      - name: Checkout repository\n")
		fmt.Fprintf(yaml, "        uses: %s\n", GetActionPin("actions/checkout"))
		// Always add with section for persist-credentials
		yaml.WriteString("        with:\n")
		yaml.WriteString("          persist-credentials: false\n")
		// In trial mode without cloning, checkout the logical repo if specified
		if c.trialMode {
			if c.trialLogicalRepoSlug != "" {
				fmt.Fprintf(yaml, "          repository: %s\n", c.trialLogicalRepoSlug)
				// trialTargetRepoName := strings.Split(c.trialLogicalRepoSlug, "/")
				// if len(trialTargetRepoName) == 2 {
				// 	yaml.WriteString(fmt.Sprintf("          path: %s\n", trialTargetRepoName[1]))
				// }
			}
			effectiveToken := getEffectiveGitHubToken("", data.GitHubToken)
			fmt.Fprintf(yaml, "          token: %s\n", effectiveToken)
		}

		// Add CLI build steps in dev mode (after automatic checkout, before other steps)
		// This builds the gh-aw CLI and Docker image for use by the agentic-workflows MCP server
		// Only generate build steps if agentic-workflows tool is enabled
		if c.actionMode.IsDev() {
			if _, hasAgenticWorkflows := data.Tools["agentic-workflows"]; hasAgenticWorkflows {
				compilerYamlLog.Printf("Generating CLI build steps for dev mode (agentic-workflows tool enabled)")
				c.generateDevModeCLIBuildSteps(yaml)
			} else {
				compilerYamlLog.Printf("Skipping CLI build steps in dev mode (agentic-workflows tool not enabled)")
			}
		}
	}

	// Add checkout steps for repository imports
	// Each repository import needs to be checked out into a temporary folder
	// so the merge script can copy files from it
	if len(data.RepositoryImports) > 0 {
		compilerYamlLog.Printf("Adding checkout steps for %d repository imports", len(data.RepositoryImports))
		c.generateRepositoryImportCheckouts(yaml, data.RepositoryImports)
	}

	// Add checkout step for legacy agent import (if present)
	// This handles the older import format where a specific agent file is imported
	if data.AgentFile != "" && data.AgentImportSpec != "" {
		compilerYamlLog.Printf("Adding checkout step for legacy agent import: %s", data.AgentImportSpec)
		c.generateLegacyAgentImportCheckout(yaml, data.AgentImportSpec)
	}

	// Add merge remote .github folder step for repository imports or agent imports
	needsGithubMerge := (len(data.RepositoryImports) > 0) || (data.AgentFile != "" && data.AgentImportSpec != "")
	if needsGithubMerge {
		compilerYamlLog.Printf("Adding merge remote .github folder step")
		yaml.WriteString("      - name: Merge remote .github folder\n")
		fmt.Fprintf(yaml, "        uses: %s\n", GetActionPin("actions/github-script"))
		yaml.WriteString("        env:\n")

		// Set repository imports if present
		if len(data.RepositoryImports) > 0 {
			// Convert to JSON array for the script
			repoImportsJSON, err := json.Marshal(data.RepositoryImports)
			if err != nil {
				return fmt.Errorf("failed to marshal repository imports for merge step: %w", err)
			}
			fmt.Fprintf(yaml, "          GH_AW_REPOSITORY_IMPORTS: '%s'\n", string(repoImportsJSON))
		}

		// Set agent import spec if present (legacy path)
		if data.AgentFile != "" && data.AgentImportSpec != "" {
			fmt.Fprintf(yaml, "          GH_AW_AGENT_FILE: \"%s\"\n", data.AgentFile)
			fmt.Fprintf(yaml, "          GH_AW_AGENT_IMPORT_SPEC: \"%s\"\n", data.AgentImportSpec)
		}

		yaml.WriteString("        with:\n")
		yaml.WriteString("          script: |\n")
		yaml.WriteString("            const { setupGlobals } = require('/opt/gh-aw/actions/setup_globals.cjs');\n")
		yaml.WriteString("            setupGlobals(core, github, context, exec, io);\n")
		yaml.WriteString("            const { main } = require('/opt/gh-aw/actions/merge_remote_agent_github_folder.cjs');\n")
		yaml.WriteString("            await main();\n")
	}

	// Add automatic runtime setup steps if needed
	// This detects runtimes from custom steps and MCP configs
	runtimeRequirements := DetectRuntimeRequirements(data)

	// Deduplicate runtime setup steps from custom steps
	// This removes any runtime setup action steps (like actions/setup-go) from custom steps
	// since we're adding them. It also preserves user-customized setup actions and
	// filters those runtimes from requirements so we don't generate duplicates.
	if len(runtimeRequirements) > 0 && data.CustomSteps != "" {
		deduplicatedCustomSteps, filteredRequirements, err := DeduplicateRuntimeSetupStepsFromCustomSteps(data.CustomSteps, runtimeRequirements)
		if err != nil {
			compilerYamlLog.Printf("Warning: failed to deduplicate runtime setup steps: %v", err)
		} else {
			data.CustomSteps = deduplicatedCustomSteps
			runtimeRequirements = filteredRequirements
		}
	}

	// Generate runtime setup steps (after filtering out user-customized ones)
	runtimeSetupSteps := GenerateRuntimeSetupSteps(runtimeRequirements)
	compilerYamlLog.Printf("Detected runtime requirements: %d runtimes, %d setup steps", len(runtimeRequirements), len(runtimeSetupSteps))

	// Decision logic for where to place runtime steps:
	// 1. If we added checkout above (needsCheckout == true), add runtime steps now (after checkout, before custom steps)
	// 2. If custom steps contain checkout, add runtime steps AFTER the first checkout in custom steps
	// 3. Otherwise, add runtime steps now (before custom steps)

	customStepsContainCheckout := data.CustomSteps != "" && ContainsCheckout(data.CustomSteps)
	compilerYamlLog.Printf("Custom steps contain checkout: %t (len(customSteps)=%d)", customStepsContainCheckout, len(data.CustomSteps))

	if needsCheckout || !customStepsContainCheckout {
		// Case 1 or 3: Add runtime steps before custom steps
		// This ensures checkout -> runtime -> custom steps order
		compilerYamlLog.Printf("Adding %d runtime steps before custom steps (needsCheckout=%t, !customStepsContainCheckout=%t)", len(runtimeSetupSteps), needsCheckout, !customStepsContainCheckout)
		for _, step := range runtimeSetupSteps {
			for _, line := range step {
				yaml.WriteString(line + "\n")
			}
		}

		// Add Serena language service installation steps if Serena is configured
		serenaLanguageSteps := GenerateSerenaLanguageServiceSteps(data.ParsedTools)
		if len(serenaLanguageSteps) > 0 {
			compilerYamlLog.Printf("Adding %d Serena language service installation steps", len(serenaLanguageSteps))
			for _, step := range serenaLanguageSteps {
				for _, line := range step {
					yaml.WriteString(line + "\n")
				}
			}
		}
	}

	// Create /tmp/gh-aw/ base directory for all temporary files
	// This must be created before custom steps so they can use the temp directory
	yaml.WriteString("      - name: Create gh-aw temp directory\n")
	yaml.WriteString("        run: bash /opt/gh-aw/actions/create_gh_aw_tmp_dir.sh\n")

	// Add custom steps if present
	if data.CustomSteps != "" {
		if customStepsContainCheckout && len(runtimeSetupSteps) > 0 {
			// Custom steps contain checkout and we have runtime steps to insert
			// Insert runtime steps after the first checkout step
			compilerYamlLog.Printf("Calling addCustomStepsWithRuntimeInsertion: %d runtime steps to insert after checkout", len(runtimeSetupSteps))
			c.addCustomStepsWithRuntimeInsertion(yaml, data.CustomSteps, runtimeSetupSteps, data.ParsedTools)
		} else {
			// No checkout in custom steps or no runtime steps, just add custom steps as-is
			compilerYamlLog.Printf("Calling addCustomStepsAsIs (customStepsContainCheckout=%t, runtimeStepsCount=%d)", customStepsContainCheckout, len(runtimeSetupSteps))
			c.addCustomStepsAsIs(yaml, data.CustomSteps)
		}
	}

	// Add cache steps if cache configuration is present
	compilerYamlLog.Printf("Generating cache steps for workflow")
	generateCacheSteps(yaml, data, c.verbose)

	// Add cache-memory steps if cache-memory configuration is present
	compilerYamlLog.Printf("Generating cache-memory steps for workflow")
	generateCacheMemorySteps(yaml, data)

	// Add repo-memory clone steps if repo-memory configuration is present
	compilerYamlLog.Printf("Generating repo-memory steps for workflow")
	generateRepoMemorySteps(yaml, data)

	// Configure git credentials for agentic workflows
	gitConfigSteps := c.generateGitConfigurationSteps()
	for _, line := range gitConfigSteps {
		yaml.WriteString(line)
	}

	// Add step to checkout PR branch if the event is pull_request
	c.generatePRReadyForReviewCheckout(yaml, data)

	// Add Node.js setup if the engine requires it and it's not already set up in custom steps
	engine, err := c.getAgenticEngine(data.AI)

	if err != nil {
		return err
	}

	// Generate aw_info.json with agentic run metadata (must run before secret validation and workflow overview)
	c.generateCreateAwInfo(yaml, data, engine)

	// Add engine-specific installation steps (includes Node.js setup and secret validation for npm-based engines)
	installSteps := engine.GetInstallationSteps(data)
	compilerYamlLog.Printf("Adding %d engine installation steps for %s", len(installSteps), engine.GetID())
	for _, step := range installSteps {
		for _, line := range step {
			yaml.WriteString(line + "\n")
		}
	}

	// GH_AW_SAFE_OUTPUTS is now set at job level, no setup step needed

	// Add GitHub MCP lockdown detection step if needed
	c.generateGitHubMCPLockdownDetectionStep(yaml, data)

	// Add GitHub MCP app token minting step if configured
	c.generateGitHubMCPAppTokenMintingStep(yaml, data)

	// Add MCP setup
	c.generateMCPSetup(yaml, data.Tools, engine, data)

	// Stop-time safety checks are now handled by a dedicated job (stop_time_check)
	// No longer generated in the main job steps

	// Generate workflow overview to step summary early, before prompts
	// This reads from aw_info.json for consistent data
	c.generateWorkflowOverviewStep(yaml, data, engine)

	// Add prompt creation step
	c.generatePrompt(yaml, data)

	// Collect artifact paths for unified upload at the end
	var artifactPaths []string
	artifactPaths = append(artifactPaths, "/tmp/gh-aw/aw-prompts/prompt.txt")
	artifactPaths = append(artifactPaths, "/tmp/gh-aw/aw_info.json")

	logFileFull := "/tmp/gh-aw/agent-stdio.log"

	// Clean git credentials before executing the agentic engine
	// This ensures that any credentials left on disk by custom steps are removed
	// to prevent the agent from accessing or exfiltrating them
	gitCleanerSteps := c.generateGitCredentialsCleanerStep()
	for _, line := range gitCleanerSteps {
		yaml.WriteString(line)
	}

	// Add AI execution step using the agentic engine
	compilerYamlLog.Printf("Generating engine execution steps for %s", engine.GetID())
	c.generateEngineExecutionSteps(yaml, data, engine, logFileFull)

	// Mark that we've completed agent execution - step order validation starts from here
	compilerYamlLog.Print("Marking agent execution as complete for step order tracking")
	c.stepOrderTracker.MarkAgentExecutionComplete()

	// Regenerate git credentials after agent execution
	// This allows safe-outputs operations (like create_pull_request) to work properly
	// We regenerate the credentials rather than restoring from backup
	gitConfigStepsAfterAgent := c.generateGitConfigurationSteps()
	for _, line := range gitConfigStepsAfterAgent {
		yaml.WriteString(line)
	}

	// Collect firewall logs BEFORE secret redaction so secrets in logs can be redacted
	if copilotEngine, ok := engine.(*CopilotEngine); ok {
		collectionSteps := copilotEngine.GetFirewallLogsCollectionStep(data)
		for _, step := range collectionSteps {
			for _, line := range step {
				yaml.WriteString(line + "\n")
			}
		}
	}
	if codexEngine, ok := engine.(*CodexEngine); ok {
		collectionSteps := codexEngine.GetFirewallLogsCollectionStep(data)
		for _, step := range collectionSteps {
			for _, line := range step {
				yaml.WriteString(line + "\n")
			}
		}
	}
	if claudeEngine, ok := engine.(*ClaudeEngine); ok {
		collectionSteps := claudeEngine.GetFirewallLogsCollectionStep(data)
		for _, step := range collectionSteps {
			for _, line := range step {
				yaml.WriteString(line + "\n")
			}
		}
	}
	if codexEngine, ok := engine.(*CodexEngine); ok {
		collectionSteps := codexEngine.GetFirewallLogsCollectionStep(data)
		for _, step := range collectionSteps {
			for _, line := range step {
				yaml.WriteString(line + "\n")
			}
		}
	}

	// Stop MCP gateway after agent execution and before secret redaction
	// This ensures the gateway process is properly cleaned up
	// Skip if sandbox is disabled (sandbox: false)
	if !isSandboxDisabled(data) {
		c.generateStopMCPGateway(yaml, data)
	}

	// Add secret redaction step BEFORE any artifact uploads
	// This ensures all artifacts are scanned for secrets before being uploaded
	c.generateSecretRedactionStep(yaml, yaml.String(), data)

	// Add output collection step only if safe-outputs feature is used (GH_AW_SAFE_OUTPUTS functionality)
	if data.SafeOutputs != nil {
		c.generateOutputCollectionStep(yaml, data)
	}

	// Add engine-declared output files collection (if any)
	if len(engine.GetDeclaredOutputFiles()) > 0 {
		c.generateEngineOutputCollection(yaml, engine)
	}

	// Extract and upload squid access logs (if any proxy tools were used)
	c.generateExtractAccessLogs(yaml, data.Tools)
	c.generateUploadAccessLogs(yaml, data.Tools)

	// Collect MCP logs path if any MCP tools were used
	artifactPaths = append(artifactPaths, "/tmp/gh-aw/mcp-logs/")

	// Collect SafeInputs logs path if safe-inputs is enabled
	if IsSafeInputsEnabled(data.SafeInputs, data) {
		artifactPaths = append(artifactPaths, "/tmp/gh-aw/safe-inputs/logs/")
	}

	// parse agent logs for GITHUB_STEP_SUMMARY
	c.generateLogParsing(yaml, engine)

	// parse safe-inputs logs for GITHUB_STEP_SUMMARY (if safe-inputs is enabled)
	if IsSafeInputsEnabled(data.SafeInputs, data) {
		c.generateSafeInputsLogParsing(yaml)
	}

	// parse MCP gateway logs for GITHUB_STEP_SUMMARY
	// Skip if sandbox is disabled (sandbox: false) as gateway won't be running
	if !isSandboxDisabled(data) {
		c.generateMCPGatewayLogParsing(yaml)
	}

	// Add firewall log parsing steps (but not upload - collected for unified upload)
	// For Copilot, Codex, and Claude engines
	if _, ok := engine.(*CopilotEngine); ok {
		if isFirewallEnabled(data) {
			firewallLogParsing := generateFirewallLogParsingStep(data.Name)
			for _, line := range firewallLogParsing {
				yaml.WriteString(line + "\n")
			}
			// Collect firewall logs path for unified upload
			artifactPaths = append(artifactPaths, "/tmp/gh-aw/sandbox/firewall/logs/")
		}
	}
	if _, ok := engine.(*CodexEngine); ok {
		if isFirewallEnabled(data) {
			firewallLogParsing := generateFirewallLogParsingStep(data.Name)
			for _, line := range firewallLogParsing {
				yaml.WriteString(line + "\n")
			}
			// Collect firewall logs path for unified upload
			artifactPaths = append(artifactPaths, "/tmp/gh-aw/sandbox/firewall/logs/")
		}
	}
	if _, ok := engine.(*ClaudeEngine); ok {
		if isFirewallEnabled(data) {
			firewallLogParsing := generateFirewallLogParsingStep(data.Name)
			for _, line := range firewallLogParsing {
				yaml.WriteString(line + "\n")
			}
			// Collect firewall logs path for unified upload
			artifactPaths = append(artifactPaths, "/tmp/gh-aw/sandbox/firewall/logs/")
		}
	}

	// Collect agent stdio logs path for unified upload
	artifactPaths = append(artifactPaths, logFileFull)

	// Collect agent-generated files path for unified upload
	// This directory is used by workflows that instruct the agent to write files
	// (e.g., smoke-claude status summaries)
	artifactPaths = append(artifactPaths, "/tmp/gh-aw/agent/")

	// Add post-execution cleanup step for Copilot engine
	if copilotEngine, ok := engine.(*CopilotEngine); ok {
		cleanupStep := copilotEngine.GetCleanupStep(data)
		for _, line := range cleanupStep {
			yaml.WriteString(line + "\n")
		}
	}

	// Add repo-memory artifact upload to save state for push job
	generateRepoMemoryArtifactUpload(yaml, data)

	// Add cache-memory artifact upload (after agent execution)
	// This ensures artifacts are uploaded after the agent has finished modifying the cache
	generateCacheMemoryArtifactUpload(yaml, data)

	// Add safe-outputs assets artifact upload (after agent execution)
	// This creates a separate artifact for assets that will be downloaded by upload_assets job
	generateSafeOutputsAssetsArtifactUpload(yaml, data)

	// Collect git patch path if safe-outputs with PR operations is configured
	// NOTE: Git patch generation has been moved to the safe-outputs MCP server
	// The patch is now generated when create_pull_request or push_to_pull_request_branch
	// tools are called, providing immediate error feedback if no changes are present.
	if data.SafeOutputs != nil && (data.SafeOutputs.CreatePullRequests != nil || data.SafeOutputs.PushToPullRequestBranch != nil) {
		artifactPaths = append(artifactPaths, "/tmp/gh-aw/aw.patch")
	}

	// Add post-steps (if any) after AI execution
	c.generatePostSteps(yaml, data)

	// Generate single unified artifact upload with all collected paths
	c.generateUnifiedArtifactUpload(yaml, artifactPaths)

	// Add GitHub MCP app token invalidation step if configured (runs always, even on failure)
	c.generateGitHubMCPAppTokenInvalidationStep(yaml, data)

	// Validate step ordering - this is a compiler check to ensure security
	if err := c.stepOrderTracker.ValidateStepOrdering(); err != nil {
		// This is a compiler bug if validation fails
		return fmt.Errorf("step ordering validation failed: %w", err)
	}
	return nil
}

// addCustomStepsAsIs adds custom steps without modification
func (c *Compiler) addCustomStepsAsIs(yaml *strings.Builder, customSteps string) {
	// Remove "steps:" line and adjust indentation
	lines := strings.Split(customSteps, "\n")
	if len(lines) > 1 {
		for _, line := range lines[1:] {
			// Skip empty lines
			if strings.TrimSpace(line) == "" {
				yaml.WriteString("\n")
				continue
			}

			// Simply add 6 spaces for job context indentation
			yaml.WriteString("      " + line + "\n")
		}
	}
}

// addCustomStepsWithRuntimeInsertion adds custom steps and inserts runtime steps after the first checkout
func (c *Compiler) addCustomStepsWithRuntimeInsertion(yaml *strings.Builder, customSteps string, runtimeSetupSteps []GitHubActionStep, tools *ToolsConfig) {
	// Remove "steps:" line and adjust indentation
	lines := strings.Split(customSteps, "\n")
	if len(lines) <= 1 {
		return
	}

	insertedRuntime := false
	i := 1 // Start from index 1 to skip "steps:" line

	for i < len(lines) {
		line := lines[i]

		// Skip empty lines
		if strings.TrimSpace(line) == "" {
			yaml.WriteString("\n")
			i++
			continue
		}

		// Add the line with proper indentation
		yaml.WriteString("      " + line + "\n")

		// Check if this line starts a step with "- name:" or "- uses:"
		trimmed := strings.TrimSpace(line)
		isStepStart := strings.HasPrefix(trimmed, "- name:") || strings.HasPrefix(trimmed, "- uses:")

		if isStepStart && !insertedRuntime {
			// This is the start of a step, check if it's a checkout step
			isCheckoutStep := false

			// Look ahead to find "uses:" line with "checkout"
			for j := i + 1; j < len(lines); j++ {
				nextLine := lines[j]
				nextTrimmed := strings.TrimSpace(nextLine)

				// Stop if we hit the next step
				if strings.HasPrefix(nextTrimmed, "- name:") || strings.HasPrefix(nextTrimmed, "- uses:") {
					break
				}

				// Check if this is a uses line with checkout
				if strings.Contains(nextTrimmed, "uses:") && strings.Contains(nextTrimmed, "checkout") {
					isCheckoutStep = true
					break
				}
			}

			if isCheckoutStep {
				// This is a checkout step, copy all its lines until the next step
				i++
				for i < len(lines) {
					nextLine := lines[i]
					nextTrimmed := strings.TrimSpace(nextLine)

					// Stop if we hit the next step
					if strings.HasPrefix(nextTrimmed, "- name:") || strings.HasPrefix(nextTrimmed, "- uses:") {
						break
					}

					// Add the line
					if nextTrimmed == "" {
						yaml.WriteString("\n")
					} else {
						yaml.WriteString("      " + nextLine + "\n")
					}
					i++
				}

				// Now insert runtime steps after the checkout step
				compilerYamlLog.Printf("Inserting %d runtime setup steps after checkout in custom steps", len(runtimeSetupSteps))
				for _, step := range runtimeSetupSteps {
					for _, stepLine := range step {
						yaml.WriteString(stepLine + "\n")
					}
				}

				// Also insert Serena language service steps if configured
				serenaLanguageSteps := GenerateSerenaLanguageServiceSteps(tools)
				if len(serenaLanguageSteps) > 0 {
					compilerYamlLog.Printf("Inserting %d Serena language service steps after runtime setup", len(serenaLanguageSteps))
					for _, step := range serenaLanguageSteps {
						for _, stepLine := range step {
							yaml.WriteString(stepLine + "\n")
						}
					}
				}

				insertedRuntime = true
				continue // Continue with the next iteration (i is already advanced)
			}
		}

		i++
	}
}

// generateRepositoryImportCheckouts generates checkout steps for repository imports
// Each repository is checked out into a temporary folder at .github/aw/imports/<owner>-<repo>-<sanitized-ref>
// relative to GITHUB_WORKSPACE. This allows the merge script to copy files from pre-checked-out folders instead of doing git operations
func (c *Compiler) generateRepositoryImportCheckouts(yaml *strings.Builder, repositoryImports []string) {
	for _, repoImport := range repositoryImports {
		compilerYamlLog.Printf("Generating checkout step for repository import: %s", repoImport)

		// Parse the import spec to extract owner, repo, and ref
		// Format: owner/repo@ref or owner/repo
		owner, repo, ref := parseRepositoryImportSpec(repoImport)
		if owner == "" || repo == "" {
			compilerYamlLog.Printf("Warning: failed to parse repository import: %s", repoImport)
			continue
		}

		// Generate a sanitized directory name for the checkout
		// Use a consistent format: owner-repo-ref
		// NOTE: Path must be relative to GITHUB_WORKSPACE for actions/checkout@v6
		sanitizedRef := sanitizeRefForPath(ref)
		checkoutPath := fmt.Sprintf(".github/aw/imports/%s-%s-%s", owner, repo, sanitizedRef)

		// Generate the checkout step
		fmt.Fprintf(yaml, "      - name: Checkout repository import %s/%s@%s\n", owner, repo, ref)
		fmt.Fprintf(yaml, "        uses: %s\n", GetActionPin("actions/checkout"))
		yaml.WriteString("        with:\n")
		fmt.Fprintf(yaml, "          repository: %s/%s\n", owner, repo)
		fmt.Fprintf(yaml, "          ref: %s\n", ref)
		fmt.Fprintf(yaml, "          path: %s\n", checkoutPath)
		yaml.WriteString("          sparse-checkout: |\n")
		yaml.WriteString("            .github/\n")
		yaml.WriteString("          persist-credentials: false\n")

		compilerYamlLog.Printf("Added checkout step: %s/%s@%s -> %s", owner, repo, ref, checkoutPath)
	}
}

// parseRepositoryImportSpec parses a repository import specification
// Format: owner/repo@ref or owner/repo (defaults to "main" if no ref)
// Returns: owner, repo, ref
func parseRepositoryImportSpec(importSpec string) (owner, repo, ref string) {
	// Remove section reference if present (file.md#Section)
	cleanSpec := importSpec
	if idx := strings.Index(importSpec, "#"); idx != -1 {
		cleanSpec = importSpec[:idx]
	}

	// Split on @ to get path and ref
	parts := strings.Split(cleanSpec, "@")
	pathPart := parts[0]
	ref = "main" // default ref
	if len(parts) > 1 {
		ref = parts[1]
	}

	// Parse path: owner/repo
	slashParts := strings.Split(pathPart, "/")
	if len(slashParts) != 2 {
		return "", "", ""
	}

	owner = slashParts[0]
	repo = slashParts[1]

	return owner, repo, ref
}

// generateLegacyAgentImportCheckout generates a checkout step for legacy agent imports
// Legacy format: owner/repo/path/to/file.md@ref
// This checks out the entire repository (not just .github folder) since the file could be anywhere
func (c *Compiler) generateLegacyAgentImportCheckout(yaml *strings.Builder, agentImportSpec string) {
	compilerYamlLog.Printf("Generating checkout step for legacy agent import: %s", agentImportSpec)

	// Parse the import spec to extract owner, repo, and ref
	owner, repo, ref := parseRepositoryImportSpec(agentImportSpec)
	if owner == "" || repo == "" {
		compilerYamlLog.Printf("Warning: failed to parse legacy agent import spec: %s", agentImportSpec)
		return
	}

	// Generate a sanitized directory name for the checkout
	sanitizedRef := sanitizeRefForPath(ref)
	checkoutPath := fmt.Sprintf("/tmp/gh-aw/repo-imports/%s-%s-%s", owner, repo, sanitizedRef)

	// Generate the checkout step
	fmt.Fprintf(yaml, "      - name: Checkout agent import %s/%s@%s\n", owner, repo, ref)
	fmt.Fprintf(yaml, "        uses: %s\n", GetActionPin("actions/checkout"))
	yaml.WriteString("        with:\n")
	fmt.Fprintf(yaml, "          repository: %s/%s\n", owner, repo)
	fmt.Fprintf(yaml, "          ref: %s\n", ref)
	fmt.Fprintf(yaml, "          path: %s\n", checkoutPath)
	yaml.WriteString("          sparse-checkout: |\n")
	yaml.WriteString("            .github/\n")
	yaml.WriteString("          persist-credentials: false\n")

	compilerYamlLog.Printf("Added legacy agent checkout step: %s/%s@%s -> %s", owner, repo, ref, checkoutPath)
}

// sanitizeRefForPath sanitizes a git ref for use in a file path
// Replaces characters that are problematic in file paths with safe alternatives
func sanitizeRefForPath(ref string) string {
	// Replace slashes with dashes (for refs like "feature/my-branch")
	sanitized := strings.ReplaceAll(ref, "/", "-")
	// Replace other problematic characters
	sanitized = strings.ReplaceAll(sanitized, ":", "-")
	sanitized = strings.ReplaceAll(sanitized, "\\", "-")
	return sanitized
}

// generateDevModeCLIBuildSteps generates the steps needed to build the gh-aw CLI and Docker image in dev mode
// These steps are injected after checkout in dev mode to create a locally built Docker image that includes
// the gh-aw binary and all dependencies. The agentic-workflows MCP server uses this image instead of alpine:latest.
//
// The build process:
// 1. Setup Go using go.mod version
// 2. Build the gh-aw CLI binary for linux/amd64 (since it runs in a Linux container)
// 3. Setup Docker Buildx for advanced build features
// 4. Build Docker image and tag it as localhost/gh-aw:dev
//
// The built image is used by the agentic-workflows MCP server configuration (see mcp_config_builtin.go)
func (c *Compiler) generateDevModeCLIBuildSteps(yaml *strings.Builder) {
	compilerYamlLog.Print("Generating dev mode CLI build steps")

	// Step 1: Setup Go for building the CLI
	yaml.WriteString("      - name: Setup Go for CLI build\n")
	fmt.Fprintf(yaml, "        uses: %s\n", GetActionPin("actions/setup-go"))
	yaml.WriteString("        with:\n")
	yaml.WriteString("          go-version-file: go.mod\n")
	yaml.WriteString("          cache: true\n")

	// Step 2: Build CLI binary for linux/amd64
	// Use the standard build command from CI/Makefile (not release build)
	// CGO_ENABLED=0 for static linking (required for Alpine containers)
	yaml.WriteString("      - name: Build gh-aw CLI\n")
	yaml.WriteString("        run: |\n")
	yaml.WriteString("          echo \"Building gh-aw CLI for linux/amd64...\"\n")
	yaml.WriteString("          mkdir -p dist\n")
	yaml.WriteString("          VERSION=$(git describe --tags --always --dirty)\n")
	yaml.WriteString("          CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \\\n")
	yaml.WriteString("            -ldflags \"-s -w -X main.version=${VERSION}\" \\\n")
	yaml.WriteString("            -o dist/gh-aw-linux-amd64 \\\n")
	yaml.WriteString("            ./cmd/gh-aw\n")
	yaml.WriteString("          # Copy binary to root for direct execution in user-defined steps\n")
	yaml.WriteString("          cp dist/gh-aw-linux-amd64 ./gh-aw\n")
	yaml.WriteString("          chmod +x ./gh-aw\n")
	yaml.WriteString("          echo \"✓ Built gh-aw CLI successfully\"\n")

	// Step 3: Setup Docker Buildx
	yaml.WriteString("      - name: Setup Docker Buildx\n")
	fmt.Fprintf(yaml, "        uses: %s\n", GetActionPin("docker/setup-buildx-action"))

	// Step 4: Build Docker image
	// Use the Dockerfile at the repository root which expects BINARY build arg
	yaml.WriteString("      - name: Build gh-aw Docker image\n")
	fmt.Fprintf(yaml, "        uses: %s\n", GetActionPin("docker/build-push-action"))
	yaml.WriteString("        with:\n")
	yaml.WriteString("          context: .\n")
	yaml.WriteString("          platforms: linux/amd64\n")
	yaml.WriteString("          push: false\n")
	yaml.WriteString("          load: true\n")
	yaml.WriteString("          tags: localhost/gh-aw:dev\n")
	yaml.WriteString("          build-args: |\n")
	yaml.WriteString("            BINARY=dist/gh-aw-linux-amd64\n")
}
