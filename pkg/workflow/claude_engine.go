package workflow

import (
	"fmt"
	"maps"
	"strconv"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/workflow/compilerenv"
)

var claudeLog = logger.New("workflow:claude_engine")

const claudeDebugLogFile = constants.TmpGhAwDir + "/claude-debug.log"

// ClaudeEngine represents the Claude Code agentic engine
type ClaudeEngine struct {
	BaseEngine
}

var _ CodingAgentEngine = (*ClaudeEngine)(nil)

func NewClaudeEngine() *ClaudeEngine {
	return &ClaudeEngine{
		BaseEngine: BaseEngine{
			id:               "claude",
			displayName:      "Claude Code",
			description:      "Uses Claude Code with full MCP tool support and allow-listing",
			experimental:     false,
			ghSkillAgentName: "claude-code",
			capabilities: EngineCapabilities{
				ToolsAllowlist:       true,
				MCP:                  true,
				MaxTurns:             true,  // Claude supports max-turns feature
				MaxContinuations:     false, // Claude Code does not support --max-autopilot-continues-style continuation
				WebSearch:            true,  // Claude has built-in WebSearch support
				NativeAgentFile:      false, // Claude does not support agent file natively; the compiler prepends the agent file content to prompt.txt
				BareMode:             true,  // Claude CLI supports --bare
				BashCommandAllowlist: true,  // Claude enforces tools.bash allowlist via --allowed-tools Bash(cmd)
				Plugins:              true,  // Claude Code loads Agent Plugins via --plugin-dir
			},
			dedicatedLLMGatewayPort: constants.ClaudeLLMGatewayPort,
		},
	}
}

// GetModelEnvVarName returns the native environment variable name that the Claude Code CLI uses
// for model selection. Setting ANTHROPIC_MODEL is equivalent to passing --model to the CLI.
func (e *ClaudeEngine) GetModelEnvVarName() string {
	return constants.ClaudeCLIModelEnvVar
}

// ResolveLLMProvider returns the effective provider for Claude inference.
// Default is anthropic, overridable via engine.provider (or engine.model-provider).
func (e *ClaudeEngine) ResolveLLMProvider(workflowData *WorkflowData) LLMProvider {
	return resolveEngineLLMProvider(workflowData, LLMProviderAnthropic)
}

// GetAPMTarget returns "claude" so that apm-action packs Claude-specific primitives.
func (e *ClaudeEngine) GetAPMTarget() string {
	return "claude"
}

// GetRequiredSecretNames returns the list of secrets required by the Claude engine.
// When Anthropic WIF (github-oidc + provider=anthropic) is configured, no static API key
// is needed and only common MCP secrets are returned.
func (e *ClaudeEngine) GetRequiredSecretNames(workflowData *WorkflowData) []string {
	provider := e.ResolveLLMProvider(workflowData)
	if provider == LLMProviderAnthropic && isAnthropicWIF(workflowData) {
		return collectCommonMCPSecrets(workflowData)
	}
	return append(llmProviderSecretNames(provider), collectCommonMCPSecrets(workflowData)...)
}

// GetSupportedEnvVarKeys returns the engine.env variable names that the Claude engine
// supports as defined in the AWF specification.
func (e *ClaudeEngine) GetSupportedEnvVarKeys() []string {
	return []string{
		constants.AnthropicAPIKey,
	}
}

// GetSecretValidationStep returns the secret validation step for the Claude engine.
// Returns an empty step if custom command is specified or if Anthropic WIF is configured.
func (e *ClaudeEngine) GetSecretValidationStep(workflowData *WorkflowData) GitHubActionStep {
	provider := e.ResolveLLMProvider(workflowData)
	return BuildEngineSecretValidationStep(workflowData, EngineSecretValidationConfig{
		SecretNames: llmProviderSecretNames(provider),
		EngineName:  "Claude Code",
		DocsURL:     llmProviderDocsURL(provider),
		Skip: func(workflowData *WorkflowData) bool {
			return provider == LLMProviderAnthropic && isAnthropicWIF(workflowData)
		},
	})
}

// isAnthropicWIF returns true when the workflow is configured to use Anthropic
// Workload Identity Federation (github-oidc auth type with provider=anthropic).
func isAnthropicWIF(workflowData *WorkflowData) bool {
	if workflowData == nil || workflowData.EngineConfig == nil || workflowData.EngineConfig.Auth == nil {
		return false
	}
	auth := workflowData.EngineConfig.Auth
	return auth.Type == "github-oidc" && auth.Provider == "anthropic"
}

func (e *ClaudeEngine) GetInstallationSteps(workflowData *WorkflowData) []GitHubActionStep {
	claudeLog.Printf("Generating installation steps for Claude engine: workflow=%s", workflowData.Name)

	// Skip installation if custom command is specified
	if workflowData.EngineConfig != nil && workflowData.EngineConfig.Command != "" {
		claudeLog.Printf("Skipping Claude CLI installation: custom command specified (%s)", workflowData.EngineConfig.Command)
		return buildNpmEngineInstallStepsWithAWF(nil, workflowData, false)
	}

	// Use version from engine config if provided, otherwise default to pinned version
	version := string(constants.DefaultClaudeCodeVersion)
	if workflowData.EngineConfig != nil && workflowData.EngineConfig.Version != "" {
		version = workflowData.EngineConfig.Version
	}

	// Claude Code requires post-install scripts (native binaries) so --ignore-scripts must
	// NOT be passed. This is intentionally different from other engine installs.
	npmSteps := GenerateNpmInstallSteps(
		"@anthropic-ai/claude-code",
		version,
		"Install Claude Code CLI",
		"claude",
		NPMInstallOptions{
			IncludeNodeSetup:  true,
			RunInstallScripts: true,
			CooldownEnabled:   false,
		},
	)
	if isDockerSbxRuntime(workflowData) || isCloudHypervisorRuntime(workflowData) {
		npmSteps = append(npmSteps, GenerateDockerSbxNpmCLIInstallStep(
			"@anthropic-ai/claude-code",
			version,
			"Install Claude Code CLI in docker-sbx path",
			"claude",
			true,
			false,
		))
	}
	return BuildNpmEngineInstallStepsWithAWF(npmSteps, workflowData)
}

// GetPluginInstallationSteps checks out pinned Agent Plugins for the Claude engine.
// Claude Code loads plugin directories through the --plugin-dir flag added by
// appendClaudePluginArgs, so no CLI installation command is required.
func (e *ClaudeEngine) GetPluginInstallationSteps(workflowData *WorkflowData) []GitHubActionStep {
	return generatePluginInstallationSteps(workflowData, pluginInstallSpec{})
}

// GetDeclaredOutputFiles returns the diagnostic files that Claude may produce.
func (e *ClaudeEngine) GetDeclaredOutputFiles() []string {
	return []string{claudeDebugLogFile}
}

// GetAgentManifestFiles returns Claude-specific instruction files that should be
// treated as security-sensitive manifests.  Modifying these files can change the
// agent's instructions, guidelines, or permissions on the next run.
// CLAUDE.md is the primary per-project instruction file; AGENTS.md is the
// cross-engine convention that Claude Code also reads.
func (e *ClaudeEngine) GetAgentManifestFiles() []string {
	return []string{"CLAUDE.md", "AGENTS.md"}
}

// GetAgentManifestPathPrefixes returns Claude-specific config directory prefixes.
// The .claude/ directory contains settings, custom commands, hooks, and other
// engine configuration that could affect agent behaviour.
func (e *ClaudeEngine) GetAgentManifestPathPrefixes() []string {
	return []string{".claude/"}
}

// GetExecutionSteps returns the GitHub Actions steps for executing Claude
func (e *ClaudeEngine) GetExecutionSteps(workflowData *WorkflowData, logFile string) []GitHubActionStep {
	claudeLog.Printf("Generating execution steps for Claude engine: workflow=%s, firewall=%v", workflowData.Name, isFirewallEnabled(workflowData))

	var steps []GitHubActionStep
	toolsWithMountedCLIs := withMountedCLIShellCommandsInRestrictedBash(workflowData)

	// Model is always passed via the native ANTHROPIC_MODEL environment variable when configured.
	// This avoids embedding the value directly in the shell command (which fails template injection
	// validation for GitHub Actions expressions like ${{ inputs.model }}).
	// Fallback for unconfigured model uses GH_AW_MODEL_AGENT_CLAUDE with shell expansion.
	modelConfigured := workflowData.Model != ""

	claudeArgs, mcpConfigArg, allowedTools := e.buildClaudeCliArgs(workflowData, toolsWithMountedCLIs, logFile)

	claudeCommand := e.buildClaudeCommandString(workflowData, claudeArgs, mcpConfigArg, modelConfigured)

	// Build the full command based on whether firewall is enabled
	command := e.buildClaudeFullCommand(workflowData, claudeCommand, logFile)

	// Build environment variables map
	env := e.buildClaudeCommandEnv(workflowData)

	// Generate the step for Claude CLI execution
	var stepLines []string
	stepLines = append(stepLines, "      - name: Execute Claude Code CLI")
	stepLines = append(stepLines, "        id: agentic_execution")

	// Add allowed tools comment before the run section.
	// Reuse the already-computed allowedTools string (computed earlier for --allowed-tools flag)
	// to avoid redundant allocations from calling computeAllowedClaudeToolsString twice.
	if allowedToolsComment := e.generateAllowedToolsComment(allowedTools, "        "); allowedToolsComment != "" {
		commentLines := strings.Split(strings.TrimSuffix(allowedToolsComment, "\n"), "\n")
		stepLines = append(stepLines, commentLines...)
	}

	// Add timeout at step level (GitHub Actions standard)
	stepLines = append(stepLines, "        timeout-minutes: "+resolveStepTimeoutValue(workflowData))

	// Filter environment variables to only include allowed secrets.
	// This is a security measure to prevent exposing unnecessary secrets to the AWF container.
	filteredEnv := FilterEnvForSecrets(env, e.GetRequiredSecretNames(workflowData))

	// Inject GH_TOKEN for CLI proxy (added after filtering since it uses a special
	// fallback expression that is always allowed when cli-proxy is enabled)
	addCliProxyGHTokenToEnv(filteredEnv, workflowData)

	stepLines = FormatStepWithCommandAndEnv(stepLines, wrapAgentExecutionCommand(command), filteredEnv)
	steps = append(steps, GitHubActionStep(stepLines))
	return steps
}

// buildClaudeCliArgs constructs the Claude CLI argument list and returns the args,
// the --mcp-config argument (kept outside shellJoinArgs for runtime ${RUNNER_TEMP} expansion),
// and the allowed-tools string (reused for the comment annotation).
func (e *ClaudeEngine) buildClaudeCliArgs(workflowData *WorkflowData, toolsWithMountedCLIs map[string]any, logFile string) (claudeArgs []string, mcpConfigArg string, allowedTools string) {
	claudeArgs = append(claudeArgs, "--print", "--no-chrome")

	if workflowData.EngineConfig != nil && workflowData.EngineConfig.MaxTurns != "" {
		claudeLog.Printf("Setting max turns: %s", workflowData.EngineConfig.MaxTurns)
		claudeArgs = append(claudeArgs, "--max-turns", workflowData.EngineConfig.MaxTurns)
	}

	// Keep --mcp-config outside shellJoinArgs so ${RUNNER_TEMP} expands at runtime.
	if HasMCPServers(workflowData) {
		claudeLog.Print("Adding MCP configuration")
		mcpConfigArg = ` --mcp-config "${RUNNER_TEMP}/gh-aw/mcp-config/mcp-servers.json"`
	}

	// Note: we use --allowed-tools (not the simpler --tools from v2.0.31+) because it provides
	// fine-grained control: Bash(git:*), MCP tool prefixes, path-specific tools, etc.
	allowedTools = e.computeAllowedClaudeToolsString(toolsWithMountedCLIs, workflowData.SafeOutputs, workflowData.CacheMemoryConfig, workflowData.DriveMemoryConfig, workflowData.MCPScripts, workflowData.SandboxConfig)
	if allowedTools != "" {
		claudeArgs = append(claudeArgs, "--allowed-tools", allowedTools)
	}

	// Keep debug output separate from the stream-json transcript captured by tee.
	claudeArgs = append(claudeArgs, "--debug-file", claudeDebugLogFile, "--verbose")

	permissionMode := resolveClaudePermissionMode(workflowData)
	claudeArgs = append(claudeArgs, "--permission-mode", permissionMode)
	permissionModeValueIndex := len(claudeArgs) - 1

	// stream-json outputs JSONL, compatible with the log parser.
	claudeArgs = append(claudeArgs, "--output-format", "stream-json")

	if workflowData.EngineConfig != nil && workflowData.EngineConfig.Bare {
		claudeLog.Print("Bare mode enabled: adding --bare")
		claudeArgs = append(claudeArgs, "--bare")
	}

	claudeArgs = appendClaudePluginArgs(claudeArgs, workflowData)

	claudeArgs = appendClaudeCustomEngineArgs(claudeArgs, permissionModeValueIndex, workflowData)

	return claudeArgs, mcpConfigArg, allowedTools
}

// appendClaudePluginArgs adds one --plugin-dir flag per checked-out Agent Plugin so the
// Claude CLI loads the plugin directory without an interactive marketplace install.
func appendClaudePluginArgs(claudeArgs []string, workflowData *WorkflowData) []string {
	for _, pluginPath := range pluginLocalPaths(workflowData) {
		claudeLog.Printf("Adding plugin directory: %s", pluginPath)
		claudeArgs = append(claudeArgs, "--plugin-dir", "./"+pluginPath)
	}
	return claudeArgs
}

// resolveClaudePermissionMode returns the --permission-mode value to use, applying any
// engine-level overrides. The default is "acceptEdits" unless tools.edit=false ("auto").
func resolveClaudePermissionMode(workflowData *WorkflowData) string {
	permissionMode := "acceptEdits"
	if isEditToolExplicitlyDisabled(workflowData.Tools) {
		claudeLog.Print("tools.edit=false detected: using auto permission mode")
		permissionMode = "auto"
	}
	if workflowData.EngineConfig != nil && workflowData.EngineConfig.PermissionMode != "" {
		permissionMode = workflowData.EngineConfig.PermissionMode
		claudeLog.Printf("Using engine.permission-mode override: %s", permissionMode)
	}
	return permissionMode
}

// appendClaudeCustomEngineArgs appends engine.args to claudeArgs, applying any
// legacy --permission-mode override found in the custom args.
func appendClaudeCustomEngineArgs(claudeArgs []string, permissionModeValueIndex int, workflowData *WorkflowData) []string {
	if workflowData.EngineConfig == nil || len(workflowData.EngineConfig.Args) == 0 {
		return claudeArgs
	}
	// stripClaudePermissionModeArgs returns an empty string when no override flag is present.
	engineArgs, permissionModeFromArgs := stripClaudePermissionModeArgs(workflowData.EngineConfig.Args)
	if permissionModeFromArgs != "" && workflowData.EngineConfig.PermissionMode == "" {
		claudeLog.Printf("Using legacy engine.args permission mode override: %s", permissionModeFromArgs)
		claudeArgs[permissionModeValueIndex] = permissionModeFromArgs
	}
	return append(claudeArgs, engineArgs...)
}

// buildClaudeCommandString assembles the claude shell command string from the given args.
// When model is not configured, appends the shell-expansion model-fallback suffix.
func (e *ClaudeEngine) buildClaudeCommandString(workflowData *WorkflowData, claudeArgs []string, mcpConfigArg string, modelConfigured bool) string {
	// Determine which command to use.
	commandName := "claude" // PATH is inherited via --env-all in AWF mode
	if workflowData.EngineConfig != nil && workflowData.EngineConfig.Command != "" {
		commandName = workflowData.EngineConfig.Command
		claudeLog.Printf("Using custom command: %s", commandName)
	}

	// Determine harness script to wrap claude execution.
	// The built-in harness provides retry logic for transient Anthropic API errors
	// (overload, rate limit). A custom engine.harness overrides the built-in one.
	harnessScriptName := e.GetHarnessScriptName()
	if workflowData.EngineConfig != nil && workflowData.EngineConfig.HarnessScript != "" {
		harnessScriptName = workflowData.EngineConfig.HarnessScript
		claudeLog.Printf("Using custom harness script: %s", harnessScriptName)
	}

	// The prompt is always read from prompt.txt, assembled by the compiler in the activation job.
	// For engines that do not support native agent-file handling (including Claude), the compiler
	// prepends the agent file content to prompt.txt so no special shell variable juggling is needed.
	var claudeCommand string
	if harnessScriptName != "" {
		// Harness-wrapped execution: the harness reads --prompt-file and passes its content
		// as the last positional arg on the initial run. On --continue retries it omits the
		// prompt so Claude Code resumes from its on-disk session state.
		// The harness sets cwd=GITHUB_WORKSPACE when spawning the claude process, so no
		// shell-level cd prefix is needed.
		execPrefix := fmt.Sprintf(`%s %s/%s %s`, nodeRuntimeResolutionCommand, SetupActionDestinationShell, harnessScriptName, commandName)
		claudeCommand = fmt.Sprintf("%s %s%s --prompt-file /tmp/gh-aw/aw-prompts/prompt.txt", execPrefix, shellJoinArgs(claudeArgs), mcpConfigArg)
	} else {
		// Without harness: use shell expansion for the prompt (no retry logic).
		// Apply workspace prefix here since there is no JS harness to set the cwd.
		// The prompt command is appended raw after shellJoinArgs because it contains
		// shell variable references ("$(cat ...)") that must NOT be escaped —
		// single-quoting them would prevent shell expansion at runtime.
		promptCommand := `"$(cat /tmp/gh-aw/aw-prompts/prompt.txt)"`
		claudeCommand = getWorkspaceCommandPrefixFor(workflowData.EngineConfig) + fmt.Sprintf("%s%s %s", shellJoinArgs(append([]string{commandName}, claudeArgs...)), mcpConfigArg, promptCommand)
	}

	// When model is not configured, use the GH_AW_MODEL_AGENT_CLAUDE fallback env var
	// via shell expansion so users can set a default via GitHub Actions variables.
	// When model IS configured, ANTHROPIC_MODEL is set in the env block and the Claude CLI
	// reads it natively — no --model flag in the shell command needed.
	if !modelConfigured {
		isDetectionJob := isDetectionRun(workflowData)
		var modelEnvVar string
		if workflowRunPhase(workflowData) == runPhaseEvals {
			modelEnvVar = constants.EnvVarModelEvalsClaude
		} else if isDetectionJob {
			modelEnvVar = constants.EnvVarModelDetectionClaude
		} else {
			modelEnvVar = constants.EnvVarModelAgentClaude
		}
		claudeCommand = fmt.Sprintf(`%s${%s:+ --model "$%s"}`, claudeCommand, modelEnvVar, modelEnvVar)
	}

	return claudeCommand
}

// buildClaudeFullCommand wraps the claude command with the AWF firewall wrapper (when enabled)
// or formats it as a plain bash command (when the firewall is disabled).
func (e *ClaudeEngine) buildClaudeFullCommand(workflowData *WorkflowData, claudeCommand string, logFile string) string {
	if isFirewallEnabled(workflowData) {
		// Get allowed domains: prefer the pre-warmed cache on WorkflowData (populated by
		// computeAllowedDomainsForSanitization before GetExecutionSteps is called) to avoid
		// re-running the expensive map+sort operation.
		var allowedDomains string
		if workflowData.CachedAllowedDomainsComputed {
			allowedDomains = workflowData.CachedAllowedDomainsStr
		} else {
			allowedDomains = GetAllowedDomainsForEngine(constants.ClaudeEngine, workflowData.NetworkPermissions, workflowData.Tools, workflowData.Runtimes)
		}
		// Add GHES/custom API target domains to the firewall allow-list when engine.api-target is set
		if workflowData.EngineConfig != nil && workflowData.EngineConfig.APITarget != "" {
			allowedDomains = mergeAPITargetDomains(allowedDomains, workflowData.EngineConfig.APITarget)
		}

		// Build AWF command with all configuration.
		// AWF v0.15.0+ uses chroot mode by default, providing transparent access to host binaries.
		// AWF with --enable-chroot and --env-all handles most PATH setup natively:
		// - GOROOT, JAVA_HOME, etc. are handled via AWF_HOST_PATH and entrypoint.sh
		// However, npm-installed CLIs (like claude) need hostedtoolcache bin directories in PATH.
		// We prepend GetNpmBinPathSetup() to the engine command so it runs inside the AWF container.
		npmPathSetup := GetNpmBinPathSetup()
		claudeCommandWithPath := fmt.Sprintf(`%s && %s`, npmPathSetup, claudeCommand)
		if dockerSbxCLIPath := GetDockerSbxNpmCLIPathSetup(workflowData); dockerSbxCLIPath != "" {
			claudeCommandWithPath = fmt.Sprintf("%s && %s", dockerSbxCLIPath, claudeCommandWithPath)
		}
		// Add MCP CLI bin directory to PATH when cli-proxy is enabled.
		if mcpCLIPath := GetMCPCLIPathSetup(workflowData); mcpCLIPath != "" {
			claudeCommandWithPath = fmt.Sprintf("%s && %s", mcpCLIPath, claudeCommandWithPath)
		}

		return BuildAWFCommand(AWFCommandConfig{
			EngineName:     "claude",
			EngineCommand:  claudeCommandWithPath, // Command with npm PATH setup runs inside AWF
			LogFile:        logFile,
			WorkflowData:   workflowData,
			UsesTTY:        true, // Claude Code CLI requires TTY
			AllowedDomains: allowedDomains,
			PathSetup:      "mkdir -p " + constants.TmpGhAwDir + " && (umask 177 && touch " + claudeDebugLogFile + ") && touch " + AgentStepSummaryPath, // Runs BEFORE AWF on the host
			// Exclude every env var whose step-env value is a secret so the agent
			// cannot read raw token values via bash tools (env / printenv).
			ExcludeEnvVarNames:   ComputeAWFExcludeEnvVarNames(workflowData, llmProviderSecretNames(e.ResolveLLMProvider(workflowData))),
			RetryStartupFailures: true,
		})
	}

	// Run Claude command without AWF wrapper.
	// Note: Claude Code CLI writes debug logs to a separate --debug-file and JSON output to stdout.
	// Use tee to capture stdout (stream-json output) to the log file while also displaying on console.
	// Leave stderr on the workflow log so non-JSON diagnostics cannot corrupt the parser input.
	// PATH is already set correctly by actions/setup-* steps which prepend to PATH.
	return fmt.Sprintf(`set -o pipefail
          printf '%%s' "$(date +%%s%%3N)" > %s
          touch %s
          (umask 177 && touch %s)
          mkdir -p %s
          (umask 177 && touch %s)
          # Execute Claude Code CLI with prompt from file
          %s | tee -a %s`, AgentCLIStartMsPath, AgentStepSummaryPath, logFile, constants.TmpGhAwDir, claudeDebugLogFile, claudeCommand, logFile)
}

// buildClaudeCommandEnv builds the environment variable map for the Claude execution step.
func (e *ClaudeEngine) buildClaudeCommandEnv(workflowData *WorkflowData) map[string]string {
	provider := e.ResolveLLMProvider(workflowData)
	env := buildClaudeBaseEnvMap(provider, workflowData)
	env["GH_AW_LLM_PROVIDER"] = string(provider)
	if isFirewallEnabled(workflowData) && provider != LLMProviderAnthropic {
		env["ANTHROPIC_BASE_URL"] = llmProviderGatewayBaseURL(provider)
	}
	injectWorkflowCallNetworkAllowedEnv(env, workflowData)
	phase := workflowRunPhase(workflowData)
	env["GH_AW_PHASE"] = phase
	if phase != runPhaseDetection {
		// Limit Anthropic SDK internal retries so terminal errors such as
		// 403 ai_credits_limit_exceeded are surfaced quickly to the harness.
		// The outer harness already owns the full retry/backoff loop for 429/529.
		// The external threat-detection path (threat-detect --engine claude) has no
		// harness retry wrapper, so we leave SDK retries at their default there.
		env["ANTHROPIC_MAX_RETRIES"] = "0"
	}
	if IsRelease() {
		env["GH_AW_VERSION"] = GetVersion()
	} else {
		env["GH_AW_VERSION"] = "dev"
	}
	if HasMCPServers(workflowData) {
		env["GH_AW_MCP_CONFIG"] = constants.McpServersJsonPathExpr
	}
	// In sandbox (AWF) mode, set git identity so the first commit succeeds inside the container.
	if isFirewallEnabled(workflowData) {
		maps.Copy(env, getGitIdentityEnvVars())
	}
	applyClaudeTimeoutEnvVars(env, workflowData)
	applyDefaultMaxAICreditsEnvToMap(env, workflowData)
	applySafeOutputEnvToMap(env, workflowData)
	applyTraceContextEnvToMap(env)
	applyOptionalEngineToolTimeouts(env, workflowData)
	applyEngineMaxTurnsEnv(env, workflowData)
	applyEngineHarnessRetryEnv(env, workflowData)
	applyClaudeModelEnvVars(env, workflowData)
	applyEngineCwdEnv(env, workflowData)
	applyEngineAndAgentEnv(env, workflowData, claudeLog)
	applyMCPScriptsSecretEnv(env, workflowData)
	return env
}

// buildClaudeBaseEnvMap returns the initial Claude execution environment with static flags
// and well-known GitHub Actions context values.
func buildClaudeBaseEnvMap(provider LLMProvider, workflowData *WorkflowData) map[string]string {
	return map[string]string{
		"ANTHROPIC_API_KEY": llmProviderSecretExpression(provider, workflowData),
		"DISABLE_TELEMETRY": "1",
		// Prevent telemetry/crash reporting and optional features that don't work in CI.
		"DISABLE_ERROR_REPORTING": "1",
		"DISABLE_BUG_COMMAND":     "1",
		// Disable fast mode: requires server-side flagSettings.fastMode which is unavailable
		// in Agent SDK contexts; without this Claude Code 2.1.120+ crashes mid-session.
		"CLAUDE_CODE_DISABLE_FAST_MODE": "1",
		"GH_AW_PROMPT":                  constants.AwPromptsFile,
		"GITHUB_AW":                     "true",
		// Override GITHUB_STEP_SUMMARY with a sandbox-visible path; the real runner path is
		// unreachable inside AWF. The file is created before the agent starts and appended
		// to the real $GITHUB_STEP_SUMMARY after secret redaction.
		"GITHUB_STEP_SUMMARY": AgentStepSummaryPath,
		"GITHUB_WORKSPACE":    "${{ github.workspace }}",
		"RUNNER_TEMP":         "${{ runner.temp }}",
	}
}

// applyClaudeTimeoutEnvVars sets MCP/Bash timeout env vars derived from workflow config.
func applyClaudeTimeoutEnvVars(env map[string]string, workflowData *WorkflowData) {
	startupTimeoutMs := int(constants.DefaultMCPStartupTimeout / time.Millisecond)
	if n := templatableIntValue(&workflowData.ToolsStartupTimeout); n > 0 {
		startupTimeoutMs = n * 1000
	}
	timeoutMs := int(constants.DefaultToolTimeout / time.Millisecond)
	if n := templatableIntValue(&workflowData.ToolsTimeout); n > 0 {
		timeoutMs = n * 1000
	}
	env["MCP_TIMEOUT"] = strconv.Itoa(startupTimeoutMs)
	env["MCP_TOOL_TIMEOUT"] = strconv.Itoa(timeoutMs)
	env["BASH_DEFAULT_TIMEOUT_MS"] = strconv.Itoa(timeoutMs)
	env["BASH_MAX_TIMEOUT_MS"] = strconv.Itoa(timeoutMs)
}

// applyClaudeModelEnvVars configures ANTHROPIC_MODEL (or fallback env vars) in env.
// When model is configured, the Claude CLI reads ANTHROPIC_MODEL natively, avoiding
// template injection issues from embedding the value in shell commands.
// When model is not configured, fall back to GH_AW_MODEL_AGENT/DETECTION/EVALS_CLAUDE
// with an explicit Claude Sonnet default so Claude CLI does not choose Opus implicitly.
func applyClaudeModelEnvVars(env map[string]string, workflowData *WorkflowData) {
	phase := workflowRunPhase(workflowData)
	if workflowData.Model == "" {
		if phase == runPhaseEvals {
			env[constants.EnvVarModelEvalsClaude] = compilerenv.BuildModelOverrideExpression(constants.EnvVarModelEvalsClaude, compilerenv.DefaultModelClaude, constants.SonnetDefaultModel)
		} else if phase == runPhaseDetection || isDetectionRun(workflowData) {
			env[constants.EnvVarModelDetectionClaude] = compilerenv.BuildModelOverrideExpression(constants.EnvVarModelDetectionClaude, compilerenv.DefaultModelClaude, constants.SonnetDefaultModel)
		} else {
			env[constants.EnvVarModelAgentClaude] = compilerenv.BuildModelOverrideExpression(constants.EnvVarModelAgentClaude, compilerenv.DefaultModelClaude, constants.SonnetDefaultModel)
		}
		return
	}
	var claudeModelVar string
	if phase == runPhaseEvals {
		claudeModelVar = constants.EnvVarModelEvalsClaude
	} else if phase == runPhaseDetection || isDetectionRun(workflowData) {
		claudeModelVar = constants.EnvVarModelDetectionClaude
	} else {
		claudeModelVar = constants.EnvVarModelAgentClaude
	}
	if containsExpression(workflowData.Model) {
		env[constants.EnvVarModelFallback] = compilerenv.BuildModelOverrideExpression(claudeModelVar, compilerenv.DefaultModelClaude, constants.SonnetDefaultModel)
	}
	claudeLog.Printf("Setting %s env var for model: %s", constants.ClaudeCLIModelEnvVar, workflowData.Model)
	env[constants.ClaudeCLIModelEnvVar] = workflowData.Model
}

// GetLogParserScriptId returns the JavaScript script name for parsing Claude logs
func (e *ClaudeEngine) GetLogParserScriptId() string {
	return "parse_claude_log"
}

// GetErrorDetectionScriptId returns the JavaScript script name for detecting
// post-run agent errors from the host runner (including invalid/unsupported model names).
func (e *ClaudeEngine) GetErrorDetectionScriptId() string {
	return "detect_agent_errors"
}

// GetHarnessScriptName returns the filename of the JavaScript harness script that wraps
// the Claude Code CLI with retry logic for transient Anthropic API errors (overload, rate limit).
func (e *ClaudeEngine) GetHarnessScriptName() string {
	return "claude_harness.cjs"
}

// GetSquidLogsSteps returns the steps for uploading and parsing Squid logs (after secret redaction)
func (e *ClaudeEngine) GetSquidLogsSteps(workflowData *WorkflowData) []GitHubActionStep {
	return defaultGetSquidLogsSteps(workflowData, claudeLog)
}

func isEditToolExplicitlyDisabled(tools map[string]any) bool {
	if tools == nil {
		return false
	}

	editConfig, hasEdit := tools["edit"]
	if !hasEdit {
		return false
	}

	enabled, isBool := editConfig.(bool)
	return isBool && !enabled
}

// stripClaudePermissionModeArgs removes all --permission-mode flags from args
// (both "--permission-mode <value>" and "--permission-mode=<value>" forms).
// It returns the filtered argument list and the last permission-mode value found.
// The returned permission-mode value is an empty string when no such flag exists.
func stripClaudePermissionModeArgs(args []string) ([]string, string) {
	filtered := make([]string, 0, len(args))
	permissionMode := ""

	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--permission-mode":
			if i+1 < len(args) {
				permissionMode = args[i+1]
				i++
			}
		case strings.HasPrefix(arg, "--permission-mode="):
			permissionMode = strings.TrimPrefix(arg, "--permission-mode=")
		default:
			filtered = append(filtered, arg)
		}
	}

	return filtered, permissionMode
}
