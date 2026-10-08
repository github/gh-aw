package workflow

import (
	"fmt"
	"maps"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/workflow/compilerenv"
)

var geminiLog = logger.New("workflow:gemini_engine")

// GeminiEngine represents the Google Gemini CLI agentic engine
type GeminiEngine struct {
	BaseEngine
}

var _ CodingAgentEngine = (*GeminiEngine)(nil)

func NewGeminiEngine() *GeminiEngine {
	return &GeminiEngine{
		BaseEngine: BaseEngine{
			id:               "gemini",
			displayName:      "Google Gemini CLI",
			description:      "Google Gemini CLI with headless mode and LLM gateway support",
			experimental:     false,
			ghSkillAgentName: "gemini-cli",
			capabilities: EngineCapabilities{
				ToolsAllowlist:       true,
				MCP:                  true,
				MaxTurns:             true,
				MaxContinuations:     false, // Gemini CLI does not support --max-autopilot-continues-style continuation mode
				WebSearch:            false,
				NativeAgentFile:      false, // Gemini does not support agent file natively; the compiler prepends the agent file content to prompt.txt
				BashCommandAllowlist: true,  // Gemini enforces tools.bash allowlist via tools.core: [run_shell_command(cmd)]
			},
			dedicatedLLMGatewayPort: constants.GeminiLLMGatewayPort,
		},
	}
}

// GetModelEnvVarName returns the native environment variable name that the Gemini CLI uses
// for model selection. Setting GEMINI_MODEL is equivalent to passing --model to the CLI.
func (e *GeminiEngine) GetModelEnvVarName() string {
	return constants.GeminiCLIModelEnvVar
}

// GetRequiredSecretNames returns the list of secrets required by the Gemini engine
// This includes GEMINI_API_KEY and optionally MCP_GATEWAY_AGENT_ID, GITHUB_MCP_SERVER_TOKEN,
// HTTP MCP header secrets, and mcp-scripts secrets.
// When Google/Vertex WIF (github-oidc + provider=google) is configured, no static API key
// is needed and only common MCP secrets are returned.
func (e *GeminiEngine) GetRequiredSecretNames(workflowData *WorkflowData) []string {
	geminiLog.Print("Collecting required secrets for Gemini engine")

	var secrets []string
	if !isGeminiVertexWIF(workflowData) {
		secrets = append(secrets, "GEMINI_API_KEY")
	}

	// Add common MCP secrets (MCP_GATEWAY_AGENT_ID if MCP servers present, mcp-scripts secrets)
	secrets = append(secrets, collectCommonMCPSecrets(workflowData)...)

	// Add GitHub token for GitHub MCP server if present
	if hasGitHubTool(workflowData.ParsedTools) {
		geminiLog.Print("Adding GITHUB_MCP_SERVER_TOKEN secret")
		secrets = append(secrets, "GITHUB_MCP_SERVER_TOKEN")
	}

	// Add HTTP MCP header secret names
	headerSecrets := collectHTTPMCPHeaderSecrets(workflowData.Tools)
	for varName := range headerSecrets {
		secrets = append(secrets, varName)
	}
	if len(headerSecrets) > 0 {
		geminiLog.Printf("Added %d HTTP MCP header secrets", len(headerSecrets))
	}

	return secrets
}

// GetSupportedEnvVarKeys returns the engine.env variable names that the Gemini engine
// supports as defined in the AWF specification.
func (e *GeminiEngine) GetSupportedEnvVarKeys() []string {
	return []string{
		constants.GeminiAPIKey,
	}
}

// GetSecretValidationStep returns the secret validation step for the Gemini engine.
// Returns an empty step if custom command is specified or if Google/Vertex WIF is configured.
func (e *GeminiEngine) GetSecretValidationStep(workflowData *WorkflowData) GitHubActionStep {
	return BuildEngineSecretValidationStep(workflowData, EngineSecretValidationConfig{
		SecretNames: []string{"GEMINI_API_KEY"},
		EngineName:  "Gemini CLI",
		DocsURL:     "https://geminicli.com/docs/get-started/authentication/",
		Skip:        isGeminiVertexWIF,
	})
}

// isGeminiVertexWIF returns true when the workflow is configured to use Google
// Workload Identity Federation (github-oidc auth type with provider=gcp) and
// has the required fields set (workload-identity-provider, service-account, project).
func isGeminiVertexWIF(workflowData *WorkflowData) bool {
	if workflowData == nil || workflowData.EngineConfig == nil || workflowData.EngineConfig.Auth == nil {
		return false
	}
	auth := workflowData.EngineConfig.Auth
	return auth.Type == "github-oidc" && auth.Provider == "gcp" &&
		auth.GoogleWorkloadIdentityProvider != "" &&
		auth.GoogleServiceAccount != "" &&
		auth.GoogleProject != ""
}

func (e *GeminiEngine) GetInstallationSteps(workflowData *WorkflowData) []GitHubActionStep {
	geminiLog.Printf("Generating installation steps for Gemini engine: workflow=%s", workflowData.Name)

	// Skip installation if custom command is specified
	if workflowData.EngineConfig != nil && workflowData.EngineConfig.Command != "" {
		geminiLog.Printf("Skipping Gemini CLI installation: custom command specified (%s)", workflowData.EngineConfig.Command)
		return buildNpmEngineInstallStepsWithAWF(nil, workflowData, false)
	}

	// Normalize engine config version when not explicitly set, so downstream consumers
	// (e.g. execution steps) observe the effective installed version.
	if workflowData.EngineConfig != nil && workflowData.EngineConfig.Version == "" {
		workflowData.EngineConfig.Version = string(constants.DefaultGeminiVersion)
		geminiLog.Printf("No engine.version specified, using default Gemini CLI version: %s", workflowData.EngineConfig.Version)
	}

	npmSteps := BuildStandardNpmEngineInstallStepsNoCooldown(
		"@google/gemini-cli",
		string(constants.DefaultGeminiVersion),
		"Install Gemini CLI",
		"gemini",
		workflowData,
	)
	return BuildNpmEngineInstallStepsWithAWF(npmSteps, workflowData)
}

// GetDeclaredOutputFiles returns the output files that Gemini may produce.
// Gemini CLI writes structured error reports to /tmp/gemini-client-error-*.json
// with a timestamp in the filename (e.g. gemini-client-error-Turn.run-sendMessageStream-2026-02-21T20-45-59-824Z.json).
// These files provide detailed diagnostics when the Gemini API call fails.
// GetPreBundleSteps moves these files into /tmp/gh-aw/ so all artifact paths share a common
// ancestor under /tmp/gh-aw/ and the actions/upload-artifact LCA calculation stays correct.
func (e *GeminiEngine) GetDeclaredOutputFiles() []string {
	return []string{
		constants.TmpGeminiClientErrorGlob,
	}
}

// GetAgentManifestFiles returns Gemini-specific instruction files that should be
// treated as security-sensitive manifests.  A fork PR that modifies these files
// can redirect the agent's behaviour or expand which files it treats as instructions.
// GEMINI.md is the primary per-project context file; AGENTS.md is the cross-engine
// convention that Gemini CLI also reads.
func (e *GeminiEngine) GetAgentManifestFiles() []string {
	return []string{"GEMINI.md", "AGENTS.md"}
}

// GetAgentManifestPathPrefixes returns Gemini-specific config directory prefixes.
// The .gemini/ directory contains settings.json and other configuration that could
// expand which files are treated as instructions or alter agent behaviour.
// Protecting this directory prevents fork PRs from injecting malicious configuration.
func (e *GeminiEngine) GetAgentManifestPathPrefixes() []string {
	return []string{".gemini/"}
}

// GetPreBundleSteps returns a step that moves Gemini CLI error reports from /tmp/ into
// /tmp/gh-aw/ before the unified artifact upload. This keeps all artifact paths under
// /tmp/gh-aw/ so that actions/upload-artifact computes the correct least-common-ancestor
// path and downstream jobs find files at the expected locations.
func (e *GeminiEngine) GetPreBundleSteps(workflowData *WorkflowData) []GitHubActionStep {
	return []GitHubActionStep{
		{
			"      - name: Move Gemini error files to artifact directory",
			"        if: always()",
			"        run: mv /tmp/gemini-client-error-*.json /tmp/gh-aw/ 2>/dev/null || true",
		},
	}
}

// GetExecutionSteps returns the GitHub Actions steps for executing Gemini
func (e *GeminiEngine) GetExecutionSteps(workflowData *WorkflowData, logFile string) []GitHubActionStep { //nolint:largefunc // Existing Gemini step assembly is kept in generated order.
	geminiLog.Printf("Generating execution steps for Gemini engine: workflow=%s, firewall=%v", workflowData.Name, isFirewallEnabled(workflowData))

	var steps []GitHubActionStep

	// Write .gemini/settings.json with context.includeDirectories and tools.core.
	// This step runs after the MCP gateway setup (which may have written mcpServers config)
	// and merges the context/tools settings into any existing settings.json.
	settingsStep := e.generateGeminiSettingsStep(workflowData)
	steps = append(steps, settingsStep)

	// Build gemini CLI arguments based on configuration
	var geminiArgs []string

	// Model is passed via the native GEMINI_MODEL environment variable only when explicitly
	// configured. When not configured, the Gemini CLI uses its built-in default model.
	// This avoids embedding the value directly in the shell command (which fails template injection
	// validation for GitHub Actions expressions like ${{ inputs.model }}).
	modelConfigured := workflowData.Model != ""

	// Gemini CLI reads MCP config from .gemini/settings.json (project-level)
	// The conversion script (convert_gateway_config_gemini.sh) writes settings.json
	// during the MCP setup step, so no --mcp-config flag is needed here.

	// Auto-approve all tool executions (equivalent to Codex's --dangerously-bypass-approvals-and-sandbox)
	// Without this, Gemini CLI's default approval mode rejects tool calls with "Tool execution denied by policy"
	geminiArgs = append(geminiArgs, "--yolo")

	// Skip the workspace trust check so --yolo is not overridden to "default" approval mode.
	// Gemini CLI v1.x checks whether the working directory is trusted and overrides --yolo
	// with "default" approval mode (exit code 55) when the folder is untrusted.
	// GEMINI_CLI_TRUST_WORKSPACE=true (also set in the step env) handles the same case via
	// environment variable, but --skip-trust is more reliable when AWF's sandbox does not
	// forward all host environment variables into the container.
	geminiArgs = append(geminiArgs, "--skip-trust")

	// Add streaming JSON output (JSONL format, compatible with the log parser)
	geminiArgs = append(geminiArgs, "--output-format", "stream-json")

	// Note: the --prompt argument is appended raw after shellJoinArgs below because it contains
	// a shell command substitution ("$(cat ...)") that must NOT go through shellEscapeArg —
	// single-quoting it would prevent shell expansion at runtime.

	// Build the command
	commandName := "gemini"
	if workflowData.EngineConfig != nil && workflowData.EngineConfig.Command != "" {
		commandName = workflowData.EngineConfig.Command
	}

	// Append the prompt arg raw (not through shellJoinArgs) to preserve shell expansion
	geminiCommand := fmt.Sprintf(`%s %s --prompt "$(cat /tmp/gh-aw/aw-prompts/prompt.txt)"`, commandName, shellJoinArgs(geminiArgs))
	geminiCommand = getWorkspaceCommandPrefixFor(workflowData.EngineConfig) + geminiCommand

	// Build the full command with AWF wrapping if enabled
	var command string
	firewallEnabled := isFirewallEnabled(workflowData)
	if firewallEnabled {
		// Get allowed domains: prefer the pre-warmed cache on WorkflowData to avoid
		// re-running the expensive map+sort operation.
		var allowedDomains string
		if workflowData.CachedAllowedDomainsComputed {
			allowedDomains = workflowData.CachedAllowedDomainsStr
		} else {
			allowedDomains = GetAllowedDomainsForEngine(constants.GeminiEngine,
				workflowData.NetworkPermissions,
				workflowData.Tools,
				workflowData.Runtimes,
			)
		}
		// Add GHES/custom API target domains to the firewall allow-list when engine.api-target is set
		if workflowData.EngineConfig != nil && workflowData.EngineConfig.APITarget != "" {
			allowedDomains = mergeAPITargetDomains(allowedDomains, workflowData.EngineConfig.APITarget)
		}

		npmPathSetup := GetNpmBinPathSetup()
		geminiCommandWithPath := fmt.Sprintf("%s && %s", npmPathSetup, geminiCommand)
		// Add MCP CLI bin directory to PATH when cli-proxy is enabled
		if mcpCLIPath := GetMCPCLIPathSetup(workflowData); mcpCLIPath != "" {
			geminiCommandWithPath = fmt.Sprintf("%s && %s", mcpCLIPath, geminiCommandWithPath)
		}

		command = BuildAWFCommand(AWFCommandConfig{
			EngineName:     "gemini",
			EngineCommand:  buildShellHarnessCommand("gemini", geminiCommandWithPath),
			LogFile:        logFile,
			WorkflowData:   workflowData,
			UsesTTY:        false,
			AllowedDomains: allowedDomains,
			// Create the agent step summary file before AWF starts so it is accessible
			// inside the sandbox. The agent writes its step summary content here, and the
			// file is appended to $GITHUB_STEP_SUMMARY after secret redaction.
			PathSetup: "touch " + AgentStepSummaryPath,
			// Exclude every env var whose step-env value is a secret so the agent
			// cannot read raw token values via bash tools (env / printenv).
			ExcludeEnvVarNames: ComputeAWFExcludeEnvVarNames(workflowData, e.GetRequiredSecretNames(workflowData)),
		})
	} else {
		command = fmt.Sprintf(`set -o pipefail
printf '%%s' "$(date +%%s%%3N)" > %s
touch %s
(umask 177 && touch %s)
%s 2>&1 | tee -a %s`, AgentCLIStartMsPath, AgentStepSummaryPath, logFile, buildShellHarnessCommand("gemini", geminiCommand), logFile)
	}

	// Build environment variables
	vertexWIF := isGeminiVertexWIF(workflowData)
	env := map[string]string{
		"GH_AW_PROMPT": constants.AwPromptsFile,
		// Tag the step as a GitHub AW agentic execution for discoverability by agents
		"GITHUB_AW":             "true",
		"GITHUB_WORKSPACE":      "${{ github.workspace }}",
		"RUNNER_TEMP":           "${{ runner.temp }}",
		"GH_AW_TIMEOUT_MINUTES": resolveStepTimeoutValue(workflowData),
		// Override GITHUB_STEP_SUMMARY with a path that exists inside the sandbox.
		// The runner's original path is unreachable within the AWF isolated filesystem;
		// we create this file before the agent starts and append it to the real
		// $GITHUB_STEP_SUMMARY after secret redaction.
		"GITHUB_STEP_SUMMARY": AgentStepSummaryPath,
		// Enable verbose debug logging from Gemini CLI for better diagnostics.
		// Gemini CLI uses the npm 'debug' package, and 'gemini-cli:*' enables all
		// internal Gemini CLI debug channels (see: https://gemini-cli-docs.pages.dev/cli/configuration).
		// Non-JSON debug lines are gracefully skipped by ParseLogMetrics.
		"DEBUG": "gemini-cli:*",
		// Trust the workspace to prevent Gemini CLI v1.x from overriding --yolo to default
		// approval mode when the workspace is untrusted, which causes exit code 55.
		"GEMINI_CLI_TRUST_WORKSPACE": "true",
	}
	applyPlaywrightBrowserEnv(env, workflowData)
	if !vertexWIF {
		// Set static API key when WIF is not configured.
		// When WIF is active, authentication is handled by the AWF api-proxy sidecar
		// via the AWF_AUTH_GCP_* env vars set through engine.auth.
		env["GEMINI_API_KEY"] = "${{ secrets.GEMINI_API_KEY }}"
	}
	injectWorkflowCallNetworkAllowedEnv(env, workflowData)
	// Indicate the phase: "agent" for the main run, "detection" for threat detection,
	// and "evals" for the eval harness execution.
	// Include the compiler version so agents can identify which gh-aw version generated the workflow
	env["GH_AW_PHASE"] = workflowRunPhase(workflowData)
	if IsRelease() {
		env["GH_AW_VERSION"] = GetVersion()
	} else {
		env["GH_AW_VERSION"] = "dev"
	}

	// Add MCP config env var if needed (points to .gemini/settings.json for Gemini)
	if HasMCPServers(workflowData) {
		env["GH_AW_MCP_CONFIG"] = "${{ github.workspace }}/.gemini/settings.json"
	}

	// When the firewall (AWF) is enabled with --enable-api-proxy, point Gemini CLI at the
	// LLM gateway sidecar instead of the real googleapis.com endpoint.
	if firewallEnabled {
		env["GEMINI_API_BASE_URL"] = fmt.Sprintf("http://host.docker.internal:%d", constants.GeminiLLMGatewayPort)

		// Set git identity environment variables so the first git commit succeeds inside the
		// container. AWF's --env-all forwards these to the container, ensuring git does not
		// rely on the host-side ~/.gitconfig which is not visible in the sandbox.
		maps.Copy(env, getGitIdentityEnvVars())
	}

	// Add safe outputs env
	applySafeOutputEnvToMap(env, workflowData)
	applyDefaultMaxAICreditsEnvToMap(env, workflowData)

	// Propagate W3C trace context so engine spans nest under the gh-aw.agent.setup span.
	applyTraceContextEnvToMap(env)

	if workflowData.EngineConfig != nil && workflowData.EngineConfig.MaxTurns != "" {
		env["GH_AW_MAX_TURNS"] = workflowData.EngineConfig.MaxTurns
	} else {
		env["GH_AW_MAX_TURNS"] = compilerenv.BuildDefaultMaxTurnsExpression()
	}

	// Set the model environment variable only when explicitly configured.
	// When model is configured, use the native GEMINI_MODEL env var - the Gemini CLI reads it
	// directly, avoiding the need to embed the value in the shell command (which would fail
	// template injection validation for GitHub Actions expressions like ${{ inputs.model }}).
	// When model is not configured, let the Gemini CLI use its built-in default model.
	if modelConfigured {
		geminiLog.Printf("Setting %s env var for model: %s", constants.GeminiCLIModelEnvVar, workflowData.Model)
		env[constants.GeminiCLIModelEnvVar] = workflowData.Model
	}

	// Add custom environment variables from engine config.
	// This allows users to override the default engine token expression (e.g.
	// GEMINI_API_KEY: ${{ secrets.MY_ORG_GEMINI_KEY }}) via engine.env.
	applyEngineCwdEnv(env, workflowData)
	if workflowData.EngineConfig != nil && len(workflowData.EngineConfig.Env) > 0 {
		maps.Copy(env, workflowData.EngineConfig.Env)
	}

	// Add custom environment variables from agent config
	agentConfig := getAgentConfig(workflowData)
	if agentConfig != nil && len(agentConfig.Env) > 0 {
		maps.Copy(env, agentConfig.Env)
		geminiLog.Printf("Added %d custom env vars from agent config", len(agentConfig.Env))
	}

	// Apply Vertex AI WIF env vars AFTER engine.env and agent.env merges to ensure
	// they cannot be overridden by user-provided engine.env values.
	if vertexWIF {
		auth := workflowData.EngineConfig.Auth
		// Gemini CLI v0.39+ selects Vertex AI backend when this is set to "true".
		env["GOOGLE_GENAI_USE_VERTEXAI"] = "true"
		env["GOOGLE_CLOUD_PROJECT"] = auth.GoogleProject
		location := auth.GoogleLocation
		if location == "" {
			location = "us-central1"
		}
		env["GOOGLE_CLOUD_LOCATION"] = location
	}

	// Generate the execution step
	stepLines := []string{
		"      - name: Execute Gemini CLI",
		"        id: agentic_execution",
	}

	// Add timeout at step level (GitHub Actions standard)
	stepLines = append(stepLines, "        timeout-minutes: "+resolveStepTimeoutValue(workflowData))

	// Filter environment variables for security
	allowedSecrets := e.GetRequiredSecretNames(workflowData)
	filteredEnv := FilterEnvForSecrets(env, allowedSecrets)

	// Inject GH_TOKEN for CLI proxy (added after filtering since it uses a special
	// fallback expression that is always allowed when cli-proxy is enabled)
	addCliProxyGHTokenToEnv(filteredEnv, workflowData)

	// Format step with command and env
	stepLines = FormatStepWithCommandAndEnv(stepLines, wrapAgentExecutionCommand(command), filteredEnv)

	steps = append(steps, GitHubActionStep(stepLines))
	return steps
}
