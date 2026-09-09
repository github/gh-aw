package workflow

import (
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
)

// generateEngineExecutionSteps generates the GitHub Actions steps for executing the AI engine
func (c *Compiler) generateEngineExecutionSteps(yaml *strings.Builder, data *WorkflowData, engine CodingAgentEngine, logFile string) {
	// --use-samples (hidden) replaces the agent step with a deterministic driver
	// that replays the workflow's safe-outputs `samples` frontmatter entries
	// through the safe-outputs MCP server. The engine is never invoked.
	if data.UseSamples {
		compilerYamlLog.Printf("Replacing engine execution with samples replay driver: engine=%s", engine.GetID())
		c.generateSamplesReplayStep(yaml, data, logFile)
		return
	}

	steps := engine.GetExecutionSteps(data, logFile)
	compilerYamlLog.Printf("Generating engine execution steps: engine=%s, steps=%d", engine.GetID(), len(steps))

	for _, step := range steps {
		for _, line := range step {
			yaml.WriteString(line)
			yaml.WriteByte('\n')
		}
	}
}

// generateLogParsing generates a step that parses the agent's logs and adds them to the step summary
func (c *Compiler) generateLogParsing(yaml *strings.Builder, data *WorkflowData, engine CodingAgentEngine) {
	parserScriptName := engine.GetLogParserScriptId()
	if parserScriptName == "" {
		// Skip log parsing if engine doesn't provide a parser
		compilerYamlLog.Printf("Skipping log parsing: engine %s has no parser script", engine.GetID())
		return
	}

	// Behavior-defined engines write their log-parser script at runtime via
	// GetExecutionSteps. In samples mode GetExecutionSteps is skipped, so the
	// script is never materialized; suppress the parse step to avoid
	// MODULE_NOT_FOUND failures.
	if data.UseSamples {
		if _, ok := engine.(*BehaviorDefinedEngine); ok {
			compilerYamlLog.Printf("Skipping log parsing for behavior-defined engine %s in samples mode (script not materialized)", engine.GetID())
			return
		}
	}

	compilerYamlLog.Printf("Generating log parsing step for engine: %s (parser=%s)", engine.GetID(), parserScriptName)

	logParserScript := GetLogParserScript(parserScriptName)
	if logParserScript == "" {
		// Skip if parser script not found
		compilerYamlLog.Printf("Warning: parser script %s not found, skipping log parsing", parserScriptName)
		return
	}

	// Get the log file path for parsing (may be different from stdout/stderr log)
	logFileForParsing := engine.GetLogFileForParsing()

	yaml.WriteString("      - name: Parse agent logs for step summary\n")
	yaml.WriteString("        if: always()\n")
	fmt.Fprintf(yaml, "        uses: %s\n", getCachedActionPin("actions/github-script", data))
	yaml.WriteString("        env:\n")
	fmt.Fprintf(yaml, "          GH_AW_AGENT_OUTPUT: %s\n", logFileForParsing)
	// GH_AW_SAFE_OUTPUTS lets the log parser detect safe-output entries written by the agent
	// so it can downgrade a "no structured log entries" failure to a warning when the agent
	// demonstrably completed (e.g. emitted a noop). Without this, runs where the container
	// debug log overwrites the JSON stream in agent-stdio.log are permanently red even though
	// the agent ran to completion.
	if data.SafeOutputs != nil {
		yaml.WriteString("          GH_AW_SAFE_OUTPUTS: ${{ steps.set-runtime-paths.outputs.GH_AW_SAFE_OUTPUTS }}\n")
	}
	yaml.WriteString("        with:\n")
	yaml.WriteString("          script: |\n")

	// Use the setup_globals helper to store GitHub Actions objects in global scope
	yaml.WriteString("            const { setupGlobals } = require('" + SetupActionDestination + "/setup_globals.cjs');\n")
	yaml.WriteString("            setupGlobals(core, github, context, exec, io, getOctokit);\n")
	// Load log parser script from external file using require()
	yaml.WriteString("            const { main } = require('${{ runner.temp }}/gh-aw/actions/" + parserScriptName + ".cjs');\n")
	yaml.WriteString("            await main();\n")
}

// generateMCPScriptsLogParsing generates a step that parses mcp-scripts logs and adds them to the step summary
func (c *Compiler) generateMCPScriptsLogParsing(yaml *strings.Builder, data *WorkflowData) {
	compilerYamlLog.Print("Generating mcp-scripts log parsing step")

	yaml.WriteString("      - name: Parse MCP Scripts logs for step summary\n")
	yaml.WriteString("        if: always()\n")
	fmt.Fprintf(yaml, "        uses: %s\n", getCachedActionPin("actions/github-script", data))
	yaml.WriteString("        with:\n")
	yaml.WriteString("          script: |\n")

	// Use the setup_globals helper to store GitHub Actions objects in global scope
	yaml.WriteString("            const { setupGlobals } = require('" + SetupActionDestination + "/setup_globals.cjs');\n")
	yaml.WriteString("            setupGlobals(core, github, context, exec, io, getOctokit);\n")
	// Load mcp-scripts log parser script from external file using require()
	yaml.WriteString("            const { main } = require('${{ runner.temp }}/gh-aw/actions/parse_mcp_scripts_logs.cjs');\n")
	yaml.WriteString("            await main();\n")
}

// generateMCPGatewayLogParsing generates a step that parses MCP gateway logs and adds them to the step summary
func (c *Compiler) generateMCPGatewayLogParsing(yaml *strings.Builder, data *WorkflowData) {
	compilerYamlLog.Print("Generating MCP gateway log parsing step")

	yaml.WriteString("      - name: Parse MCP Gateway logs for step summary\n")
	yaml.WriteString("        if: always()\n")
	fmt.Fprintf(yaml, "        id: %s\n", constants.ParseMCPGatewayStepID)
	fmt.Fprintf(yaml, "        uses: %s\n", getCachedActionPin("actions/github-script", data))
	yaml.WriteString("        with:\n")
	yaml.WriteString("          script: |\n")

	// Use the setup_globals helper to store GitHub Actions objects in global scope
	yaml.WriteString("            const { setupGlobals } = require('" + SetupActionDestination + "/setup_globals.cjs');\n")
	yaml.WriteString("            setupGlobals(core, github, context, exec, io, getOctokit);\n")
	// Load MCP gateway log parser script from external file using require()
	yaml.WriteString("            const { main } = require('${{ runner.temp }}/gh-aw/actions/parse_mcp_gateway_log.cjs');\n")
	yaml.WriteString("            await main();\n")
}

// generateObservabilitySummary generates a step that synthesizes a compact
// observability section for the GitHub Actions step summary from existing runtime files.
// The step is only emitted when OTLP is configured in the workflow.
func (c *Compiler) generateObservabilitySummary(yaml *strings.Builder, data *WorkflowData) {
	if !isOTLPEnabled(data) {
		return
	}

	compilerYamlLog.Print("Generating observability step summary")

	yaml.WriteString("      - name: Generate observability summary\n")
	yaml.WriteString("        if: always()\n")
	fmt.Fprintf(yaml, "        uses: %s\n", getCachedActionPin("actions/github-script", data))
	yaml.WriteString("        with:\n")
	yaml.WriteString("          script: |\n")
	yaml.WriteString("            const { setupGlobals } = require('" + SetupActionDestination + "/setup_globals.cjs');\n")
	yaml.WriteString("            setupGlobals(core, github, context, exec, io, getOctokit);\n")
	yaml.WriteString("            const { main } = require('${{ runner.temp }}/gh-aw/actions/generate_observability_summary.cjs');\n")
	yaml.WriteString("            await main(core);\n")
}

// isOTLPEnabled returns true when OTLP has been configured in the workflow (including
// imported frontmatter). It checks whether injectOTLPConfig has already written the
// OTEL_EXPORTER_OTLP_ENDPOINT env var into workflowData.Env, which is the authoritative
// result of OTLP detection after all frontmatter (main + imports) has been processed.
func isOTLPEnabled(data *WorkflowData) bool {
	if data == nil {
		return false
	}
	return strings.Contains(data.Env, "OTEL_EXPORTER_OTLP_ENDPOINT")
}

// generateStopMCPGateway generates a step that stops the MCP gateway process using its PID from step output
// It passes the gateway port and agent ID to enable graceful shutdown via /close endpoint
func (c *Compiler) generateStopMCPGateway(yaml *strings.Builder, data *WorkflowData) {
	compilerYamlLog.Print("Generating MCP gateway stop step")

	yaml.WriteString("      - name: Stop MCP Gateway\n")
	yaml.WriteString("        if: always()\n")
	yaml.WriteString("        continue-on-error: true\n")

	// Add environment variables for graceful shutdown via /close endpoint
	// These values normally come from Start MCP Gateway step outputs. Enclave workflows
	// inherit the masked agent ID from the compiler-owned GITHUB_ENV handoff instead.
	// Security: Pass all step outputs through environment variables to prevent template injection
	yaml.WriteString("        env:\n")
	yaml.WriteString("          MCP_GATEWAY_PORT: ${{ steps.start-mcp-gateway.outputs.gateway-port }}\n")
	if !enclavesEnabled(data) {
		yaml.WriteString("          MCP_GATEWAY_AGENT_ID: ${{ steps.start-mcp-gateway.outputs.gateway-agent-id }}\n")
	}
	yaml.WriteString("          GATEWAY_PID: ${{ steps.start-mcp-gateway.outputs.gateway-pid }}\n")

	yaml.WriteString("        run: |\n")
	yaml.WriteString("          bash \"${RUNNER_TEMP}/gh-aw/actions/stop_mcp_gateway.sh\" \"$GATEWAY_PID\"\n")
}

// generateAgentOutputPlaceholderStep generates a step that writes a minimal {"items":[]}
// placeholder to agent_output.json when the engine exits before producing any safe outputs.
// This prevents downstream safe_outputs and conclusion jobs from receiving an ENOENT error
// when loading the agent output file, making it easier to surface the real engine failure
// reason (e.g. quota exceeded) instead of an unhelpful file-not-found message.
func (c *Compiler) generateAgentOutputPlaceholderStep(yaml *strings.Builder) {
	compilerYamlLog.Print("Generating agent output placeholder step")

	yaml.WriteString("      - name: Write agent output placeholder if missing\n")
	yaml.WriteString("        if: always()\n")
	yaml.WriteString("        run: |\n")
	yaml.WriteString("          if [ ! -f /tmp/gh-aw/agent_output.json ]; then\n")
	yaml.WriteString("            echo '{\"items\":[]}' > /tmp/gh-aw/agent_output.json\n")
	yaml.WriteString("          fi\n")
}

// generateAgentStepSummaryAppend generates a step that appends the agent's GITHUB_STEP_SUMMARY
// file to the real $GITHUB_STEP_SUMMARY. This runs after secret redaction so the content
// is already sanitised before being published to the workflow step summary.
// The step is a no-op when the file is empty (agent wrote nothing).
func (c *Compiler) generateAgentStepSummaryAppend(yaml *strings.Builder) {
	compilerYamlLog.Print("Generating agent step summary append step")

	yaml.WriteString("      - name: Append agent step summary\n")
	yaml.WriteString("        if: always()\n")
	yaml.WriteString("        run: bash \"${RUNNER_TEMP}/gh-aw/actions/append_agent_step_summary.sh\"\n")
}

// generateTokenUsageSummary generates a step that parses the firewall proxy's
// token-usage.jsonl and appends a markdown table to $GITHUB_STEP_SUMMARY.
// The step also writes aggregated token totals to /tmp/gh-aw/agent_usage.json
// so they are bundled in the agent artifact for third-party tools.
func (c *Compiler) generateTokenUsageSummary(yaml *strings.Builder, data *WorkflowData) {
	compilerYamlLog.Print("Generating token usage summary step")

	yaml.WriteString("      - name: Parse token usage for step summary\n")
	yaml.WriteString("        if: always()\n")
	yaml.WriteString("        continue-on-error: true\n")
	fmt.Fprintf(yaml, "        uses: %s\n", getCachedActionPin("actions/github-script", data))
	yaml.WriteString("        with:\n")
	yaml.WriteString("          script: |\n")
	yaml.WriteString("            const { setupGlobals } = require('" + SetupActionDestination + "/setup_globals.cjs');\n")
	yaml.WriteString("            setupGlobals(core, github, context, exec, io, getOctokit);\n")
	yaml.WriteString("            const { main } = require('" + SetupActionDestination + "/parse_token_usage.cjs');\n")
	yaml.WriteString("            await main();\n")
}

// generateAWFReflectSummary generates a step that reads the AWF /reflect payload
// persisted by copilot_harness.cjs and appends a provider/model table to $GITHUB_STEP_SUMMARY.
//
// The /reflect endpoint (served by the AWF api-proxy sidecar on port 10000) returns the
// list of configured LLM providers together with their available model lists. The harness
// fetches this data from inside the AWF container and writes it to /tmp/gh-aw/awf-reflect.json
// so this step can include it in the summary after the agent has completed.
func (c *Compiler) generateAWFReflectSummary(yaml *strings.Builder, data *WorkflowData) {
	compilerYamlLog.Print("Generating AWF reflect summary step")

	yaml.WriteString("      - name: Print AWF reflect summary\n")
	yaml.WriteString("        if: always()\n")
	yaml.WriteString("        continue-on-error: true\n")
	fmt.Fprintf(yaml, "        uses: %s\n", getCachedActionPin("actions/github-script", data))
	yaml.WriteString("        with:\n")
	yaml.WriteString("          script: |\n")
	yaml.WriteString("            const { setupGlobals } = require('" + SetupActionDestination + "/setup_globals.cjs');\n")
	yaml.WriteString("            setupGlobals(core, github, context, exec, io, getOctokit);\n")
	yaml.WriteString("            const { main } = require('" + SetupActionDestination + "/awf_reflect_summary.cjs');\n")
	yaml.WriteString("            await main();\n")
}

// generateDetectAgentErrorsStep emits a host-runner step that runs the engine's error detection
// script after the AWF container exits. This step must run on the host runner (not inside the
// container) because GITHUB_OUTPUT is not mounted into the AWF sandbox.
// The step is only emitted when the engine provides a detection script via GetErrorDetectionScriptId.
func (c *Compiler) generateDetectAgentErrorsStep(yaml *strings.Builder, data *WorkflowData, engine CodingAgentEngine) {
	scriptId := engine.GetErrorDetectionScriptId()
	if scriptId == "" {
		compilerYamlLog.Printf("Skipping error detection step: engine %s has no detection script", engine.GetID())
		return
	}

	compilerYamlLog.Printf("Generating error detection step for engine: %s (script=%s)", engine.GetID(), scriptId)

	yaml.WriteString("      - name: Detect agent errors\n")
	yaml.WriteString("        if: always()\n")
	fmt.Fprintf(yaml, "        id: %s\n", constants.DetectAgentErrorsStepID)
	yaml.WriteString("        continue-on-error: true\n")
	// The engine step outcome and its timeout-minutes budget allow the detection script to
	// recognize a GitHub Actions step-level timeout ("The action '...' has timed out after N
	// minutes."), which kills the engine without leaving a timeout signature in the agent log.
	yaml.WriteString("        env:\n")
	yaml.WriteString("          GH_AW_AGENTIC_EXECUTION_OUTCOME: ${{ steps.agentic_execution.outcome }}\n")
	fmt.Fprintf(yaml, "          GH_AW_ENGINE_STEP_TIMEOUT_MINUTES: %s\n", resolveStepTimeoutValue(data))
	// Engines that write their own internal tracing/diagnostic logs to files (rather than
	// stdout/stderr) can surface a directory here; the detection script tails the most recently
	// modified log file under it into the step log when the execution step failed, so a bare
	// non-zero exit with no console output still has diagnosable content.
	if internalLogsDir := engine.GetInternalLogsDir(); internalLogsDir != "" {
		fmt.Fprintf(yaml, "          GH_AW_ENGINE_INTERNAL_LOGS_DIR: %s\n", internalLogsDir)
	}
	fmt.Fprintf(yaml, "        uses: %s\n", getCachedActionPin("actions/github-script", data))
	yaml.WriteString("        with:\n")
	yaml.WriteString("          script: |\n")
	yaml.WriteString("            const { setupGlobals } = require('" + SetupActionDestination + "/setup_globals.cjs');\n")
	yaml.WriteString("            setupGlobals(core, github, context, exec, io, getOctokit);\n")
	fmt.Fprintf(yaml, "            const { main } = require('%s/%s.cjs');\n", SetupActionDestination, scriptId)
	yaml.WriteString("            await main();\n")
}

// generateEngineInstallAndPreAgentSteps emits git credential configuration, the PR-ready-for-review
// checkout, engine installation steps, GitHub MCP app token minting, MCP lockdown detection, guard
// variable parsing, DIFC proxy stop, base-.github-folder restore, pre-agent steps, MCP gateway
// setup, and MCP CLI mount.
// The activation artifact download and comment-memory file preparation are emitted earlier (in
// generateActivationArtifactAndCommentMemorySteps) so that user steps: can access prior
// comment-memory state.
// It returns the resolved CodingAgentEngine for use in subsequent phases.
func (c *Compiler) generateEngineInstallAndPreAgentSteps(yaml *strings.Builder, data *WorkflowData, needsGitConfig bool) (CodingAgentEngine, error) { //nolint:largefunc // Existing workflow step orchestration preserves generated step order.
	// Configure git credentials for agentic workflows.
	// Git credential configuration requires a .git directory in the workspace, which is only
	// present when the repository was checked out. Skip these steps when checkout is disabled
	// and no custom steps perform a checkout, since git remote set-url origin would fail
	// with "fatal: not a git repository" otherwise.
	compilerYamlLog.Printf("Git credential configuration needed: %t", needsGitConfig)
	if needsGitConfig {
		gitConfigSteps := c.generateGitConfigurationStepsForData(data)
		for _, line := range gitConfigSteps {
			yaml.WriteString(line)
		}
	}

	// Add step to checkout PR branch if the event is pull_request
	c.generatePRReadyForReviewCheckout(yaml, data)

	// Add Node.js setup if the engine requires it and it's not already set up in custom steps
	engine, err := c.getAgenticEngine(data.AI)
	if err != nil {
		return nil, fmt.Errorf("agentic engine could not be resolved from the 'engine' field, expected a supported engine id such as 'copilot' or 'claude': %w", err)
	}

	// Ensure MCP gateway defaults are set before generating aw_info.json
	// This is needed so that awmg_version is populated correctly
	if HasMCPServers(data) {
		ensureDefaultMCPGatewayConfig(data)
	}

	// Propagate the compiler version so engine installation steps can embed it as
	// GH_AW_COMPILED_VERSION, enabling compat.json-based toolcache resolution at runtime.
	// Non-release builds intentionally normalize this to "dev" to avoid lock-file churn.
	if data.CompiledVersion == "" {
		data.CompiledVersion = GetCompiledVersionForEmission(c.version)
	}

	// Add engine-specific installation steps (includes Node.js setup and secret validation for npm-based engines)
	installSteps := engine.GetInstallationSteps(data)
	compilerYamlLog.Printf("Adding %d engine installation steps for %s", len(installSteps), engine.GetID())
	for _, step := range installSteps {
		for _, line := range step {
			yaml.WriteString(line)
			yaml.WriteByte('\n')
		}
	}

	if pluginInstaller, ok := engine.(PluginInstallationProvider); ok {
		pluginAuthSteps := c.generatePluginAuthTokenSteps(data)
		compilerYamlLog.Printf("Adding %d plugin auth token steps for %s", len(pluginAuthSteps), engine.GetID())
		for _, step := range pluginAuthSteps {
			for _, line := range step {
				yaml.WriteString(line)
				yaml.WriteByte('\n')
			}
		}

		pluginInstallSteps := pluginInstaller.GetPluginInstallationSteps(data)
		compilerYamlLog.Printf("Adding %d plugin installation steps for %s", len(pluginInstallSteps), engine.GetID())
		for _, step := range pluginInstallSteps {
			for _, line := range step {
				yaml.WriteString(line)
				yaml.WriteByte('\n')
			}
		}
	}

	// Add Playwright CLI install steps when playwright is configured in CLI mode.
	// These run after Node.js is available (set up by the engine install steps above).
	for _, step := range generatePlaywrightCLIInstallSteps(data) {
		for _, line := range step {
			yaml.WriteString(line)
			yaml.WriteByte('\n')
		}
	}

	// GH_AW_SAFE_OUTPUTS is now set at job level, no setup step needed

	// Mint the GitHub MCP App token directly in the agent job.
	// The token cannot be passed via job outputs from the activation job because
	// actions/create-github-app-token calls ::add-mask:: on the token, and the
	// GitHub Actions runner silently drops masked values in job outputs (runner v2.308+).
	// By minting the token here, the app-id / private-key secrets are accessed only
	// within this job and the minted token is available as steps.github-mcp-app-token.outputs.token.
	for _, step := range c.generateGitHubMCPAppTokenMintingSteps(data) {
		yaml.WriteString(step)
	}

	// Add GitHub MCP lockdown detection step if needed
	c.generateGitHubMCPLockdownDetectionStep(yaml, data)

	// Add step to parse blocked-users and approval-labels guard variables into JSON arrays
	c.generateParseGuardVarsStep(yaml, data)

	// Stop DIFC proxy before starting the MCP gateway. The proxy must be stopped first
	// to avoid double-filtering: the gateway uses the same guard policy for the agent phase.
	c.generateStopDIFCProxyStep(yaml, data)

	// Stop-time safety checks are now handled by a dedicated job (stop_time_check)
	// No longer generated in the main job steps

	// Restore agent config folders from the base branch snapshot in the activation artifact.
	// The activation job saved these before the PR checkout ran, so this step overwrites any
	// PR-branch-injected files (e.g. forked skill/instruction files) with trusted base content.
	// The .github/mcp.json file is also removed since it may come from the PR branch.
	// The folder and file lists match those used in the save step (derived from engine registry).
	//
	// IMPORTANT: This must run BEFORE pre-agent-steps (below) so that APM-restored skills
	// placed in .github/skills/ by pre-agent-steps are not clobbered by this restore.
	if ShouldGeneratePRCheckoutStep(data) {
		folders, files := resolveAgentManifestPaths(c.engineRegistry, data)
		generateRestoreBaseGitHubFoldersStep(yaml,
			folders,
			files,
		)
		generateRestoreAmbientFoldersStep(yaml, data)
	}

	// Restore inline sub-agents written during the activation job.
	// This step runs AFTER the base-branch restore so the engine-specific agent directory
	// is not clobbered. Inline sub-agents are enabled by default.
	if isFeatureEnabled(constants.FeatureFlag("inline-agents"), data) {
		generateRestoreInlineSubAgentsStep(yaml, data, c.engineRegistry)
	}
	// Restore the engine-specific skills directory when inline skills are enabled or when
	// explicit frontmatter skills were installed during activation.
	if isFeatureEnabled(constants.FeatureFlag("inline-agents"), data) || len(data.Skills) > 0 {
		generateRestoreInlineSkillsStep(yaml, data, c.engineRegistry)
	}

	// Add pre-agent-steps (if any) after base-branch restore but before MCP setup.
	// Running after base restore ensures APM-restored skills (.github/skills/) are not
	// overwritten by the restore step above in PR context.
	// Running before MCP setup ensures pre-agent-steps can install/configure MCP
	// dependencies that the gateway may reference when it starts.
	c.generatePreAgentSteps(yaml, data)

	// Add MCP setup
	if err := c.generateMCPSetup(yaml, data.Tools, engine, data); err != nil {
		return nil, fmt.Errorf("MCP setup could not be generated, expected valid tool configuration in the 'tools' section: %w", err)
	}

	// Mount MCP servers as CLI tools (runs after gateway is started)
	c.generateMCPCLIMountStep(yaml, data)

	return engine, nil
}

// generateAgentRunSteps emits the git credentials cleaner, engine config steps, CLI proxy start,
// AI execution, CLI proxy stop, Copilot error detection, agent-execution-complete marker,
// post-agent git credential regeneration, firewall log collection, engine pre-bundle steps,
// MCP gateway stop, secret redaction, agent step summary append, and output collection.
// It returns the initial set of artifact paths (to be extended by the caller) and the
// agent stdio log path constant.
func (c *Compiler) generateAgentRunSteps(yaml *strings.Builder, data *WorkflowData, engine CodingAgentEngine, needsGitConfig bool) ([]string, string, error) { //nolint:largefunc // Existing workflow step orchestration preserves generated step order.
	// Collect artifact paths for unified upload at the end
	var artifactPaths []string
	artifactPaths = append(artifactPaths, constants.AwPromptsFile)

	logFileFull := constants.AgentStdioLogPath

	// Clean credentials before executing the agentic engine.
	// This removes git credentials from .git/config and, when known credential-leaking
	// actions were detected, also removes cloud-provider / registry credentials.
	credentialsCleanerSteps := c.generateCredentialsCleanerStep(data.KnownActionCredentialEnvVars)
	for _, line := range credentialsCleanerSteps {
		yaml.WriteString(line)
	}

	// Emit an audit step after credentials have been cleaned but before the agent begins
	// execution. This captures a file listing of agent-related directories so the final
	// pre-agent state (including any config written by MCP setup and engine config steps)
	// is visible in the agent artifact without exposing raw credentials.
	c.generatePreAgentAuditStep(yaml)

	// Emit engine config steps (from RenderConfig) before the AI execution step.
	// These steps write runtime config files to disk (e.g. provider/model config files).
	// Most engines return no steps here; only engines that require config files use this.
	if len(data.EngineConfigSteps) > 0 {
		compilerYamlLog.Printf("Adding %d engine config steps for %s", len(data.EngineConfigSteps), engine.GetID())
		for _, step := range data.EngineConfigSteps {
			stepYAML, err := ConvertStepToYAML(step)
			if err != nil {
				return nil, "", fmt.Errorf("engine config step could not be rendered to YAML, expected a step with valid fields: %w", err)
			}
			yaml.WriteString(stepYAML)
		}
	}

	// Start CLI proxy on the host before AWF execution. When features.cli-proxy is enabled,
	// the compiler starts a difc-proxy container on the host that AWF's cli-proxy sidecar
	// connects to via host.docker.internal:18443.
	c.generateStartCliProxyStep(yaml, data)

	// Refresh sbx credentials immediately before AWF execution. Docker Hub OAuth
	// tokens obtained during the daemon-setup step can expire between workflow steps,
	// causing "user is not authenticated to Docker" errors when AWF calls `sbx create`.
	if isDockerSbxRuntime(data) {
		refreshStep := generateDockerSbxCredentialRefreshStep()
		for _, line := range refreshStep {
			yaml.WriteString(line)
			yaml.WriteString("\n")
		}
	}

	// Add AI execution step using the agentic engine
	compilerYamlLog.Printf("Generating engine execution steps for %s", engine.GetID())
	c.generateEngineExecutionSteps(yaml, data, engine, logFileFull)

	// Stop CLI proxy after AWF execution (always runs to ensure cleanup)
	c.generateStopCliProxyStep(yaml, data)

	// Detect agent errors on the host runner immediately after the AWF container exits.
	// GITHUB_OUTPUT is not accessible inside the AWF sandbox, so this step must run here
	// (on the host runner) rather than from within the container. Engines that provide a
	// detection script via GetErrorDetectionScriptId will emit this step.
	c.generateDetectAgentErrorsStep(yaml, data, engine)

	// Mark that we've completed agent execution - step order validation starts from here
	compilerYamlLog.Print("Marking agent execution as complete for step order tracking")
	c.stepOrderTracker.MarkAgentExecutionComplete()

	// Regenerate git credentials after agent execution
	// This allows safe-outputs operations (like create_pull_request) to work properly
	// We regenerate the credentials rather than restoring from backup.
	// Only emit these steps when a checkout was performed (requires a .git directory).
	// When current: true targets a subdirectory, target that path so the step succeeds
	// even if a pre-agent step removed the workspace-root git repository.
	if needsGitConfig {
		gitConfigStepsAfterAgent := c.generateGitConfigurationStepsForData(data)
		for _, line := range gitConfigStepsAfterAgent {
			yaml.WriteString(line)
		}
	}

	// Collect firewall logs BEFORE secret redaction so secrets in logs can be redacted
	for _, step := range engine.GetFirewallLogsCollectionStep(data) {
		for _, line := range step {
			yaml.WriteString(line)
			yaml.WriteByte('\n')
		}
	}

	// Run engine pre-bundle steps to relocate files before secret redaction.
	// This ensures all artifact paths share a common ancestor under /tmp/gh-aw/.
	for _, step := range engine.GetPreBundleSteps(data) {
		for _, line := range step {
			yaml.WriteString(line)
			yaml.WriteByte('\n')
		}
	}

	// Stop MCP gateway after agent execution and before secret redaction
	// This ensures the gateway process is properly cleaned up
	// The MCP gateway is always enabled, even when agent sandbox is disabled
	c.generateStopMCPGateway(yaml, data)

	// Add secret redaction step BEFORE any artifact uploads
	// This ensures all artifacts are scanned for secrets before being uploaded
	c.generateSecretRedactionStep(yaml, yaml.String(), data)

	// Append the agent step summary to the real $GITHUB_STEP_SUMMARY after secrets are redacted.
	// The agent writes its GITHUB_STEP_SUMMARY content to AgentStepSummaryPath (a file inside
	// /tmp/gh-aw/ that is reachable in both AWF sandbox and non-sandbox modes).
	// secret redaction already scanned this file, so it is safe to append.
	c.generateAgentStepSummaryAppend(yaml)

	// Add output collection step only if safe-outputs feature is used (GH_AW_SAFE_OUTPUTS functionality)
	if data.SafeOutputs != nil {
		if err := c.generateOutputCollectionStep(yaml, data); err != nil {
			return nil, "", err
		}
	}

	return artifactPaths, logFileFull, nil
}
