package workflow

import (
	"fmt"
	"maps"
	"path"
	"regexp"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/sliceutil"
	"github.com/github/gh-aw/pkg/workflow/compilerenv"
)

var codexEngineLog = logger.New("workflow:codex_engine")

// detectionResponseSchema is the JSON Schema for Codex detection runs.
// It constrains the model output to exactly the threat detection result fields.
// The schema is written to detectionSchemaFilePath before Codex runs and passed
// via --output-schema; the structured result is written to detectionResultFilePath
// via --output-last-message for direct parsing without log scraping.
const detectionResponseSchema = `{"type":"object","properties":{"prompt_injection":{"type":"boolean"},"secret_leak":{"type":"boolean"},"malicious_patch":{"type":"boolean"},"reasons":{"type":"array","items":{"type":"string"}}},"required":["prompt_injection","secret_leak","malicious_patch","reasons"],"additionalProperties":false}`

// detectionSchemaFilePath is the path where the detection JSON schema is written
// before Codex runs. It is referenced by --output-schema.
const detectionSchemaFilePath = "/tmp/gh-aw/threat-detection/detection_schema.json"

// detectionResultFilePath is the path where Codex writes the final structured
// verdict via --output-last-message. The parser reads this file directly instead
// of scraping the log stream, eliminating false parse_error warnings from noisy
// SSE/tracing output.
const detectionResultFilePath = "/tmp/gh-aw/threat-detection/detection_result.json"

// Pre-compiled regexes for Codex log parsing (performance optimization)
var (
	codexToolCallOldFormat    = regexp.MustCompile(`\] tool ([^(]+)\(`)
	codexToolCallNewFormat    = regexp.MustCompile(`^tool ([^(]+)\(`)
	codexExecCommandOldFormat = regexp.MustCompile(`\] exec (.+?) in`)
	codexExecCommandNewFormat = regexp.MustCompile(`^exec (.+?) in`)
	codexDurationPattern      = regexp.MustCompile(`in\s+(\d+(?:\.\d+)?)\s*s`)
	codexTokenUsagePattern    = regexp.MustCompile(`(?i)tokens\s+used[:\s]+(\d+)`)
	codexTotalTokensPattern   = regexp.MustCompile(`total_tokens:\s*(\d+)`)
)

// CodexEngine represents the Codex agentic engine
type CodexEngine struct {
	BaseEngine
}

var _ CodingAgentEngine = (*CodexEngine)(nil)

func NewCodexEngine() *CodexEngine {
	return &CodexEngine{
		BaseEngine: BaseEngine{
			id:               "codex",
			displayName:      "Codex",
			description:      "Uses OpenAI Codex CLI with MCP server support",
			experimental:     false,
			ghSkillAgentName: "codex",
			capabilities: EngineCapabilities{
				ToolsAllowlist:   true,
				MCP:              true,
				MaxTurns:         true,  // AWF max-turns is supported for Codex runs
				MaxContinuations: false, // Codex does not support --max-autopilot-continues-style continuation mode
				WebSearch:        true,  // Codex has built-in web-search support
				NativeAgentFile:  false, // Codex does not support agent file natively; the compiler prepends the agent file content to prompt.txt
				BashDisable:      true,  // Codex can fully refuse shell execution via `-c features.shell_tool=false`, though it cannot enforce a per-command allowlist
				Plugins:          true,  // Codex CLI loads Agent Plugins through a generated local marketplace and "codex plugin add"
			},
			dedicatedLLMGatewayPort: constants.CodexLLMGatewayPort,
		},
	}
}

// GetModelEnvVarName returns an empty string because the Codex CLI does not support
// selecting the model via a native environment variable. Model selection for Codex
// is done via the --model flag in the shell command.
func (e *CodexEngine) GetModelEnvVarName() string {
	return ""
}

// ResolveLLMProvider returns the effective provider for Codex inference.
// A copilot/ model prefix selects GitHub-hosted inference. An explicit
// engine.provider (or engine.model-provider) override always takes precedence.
func (e *CodexEngine) ResolveLLMProvider(workflowData *WorkflowData) LLMProvider {
	if workflowData != nil && workflowData.EngineConfig != nil && workflowData.EngineConfig.LLMProvider != "" {
		return resolveEngineLLMProvider(workflowData, LLMProviderOpenAI)
	}
	if workflowData != nil && strings.HasPrefix(strings.ToLower(strings.TrimSpace(workflowData.Model)), "copilot/") {
		return LLMProviderGitHub
	}
	return resolveEngineLLMProvider(workflowData, LLMProviderOpenAI)
}

func codexModelID(model string) string {
	model = strings.TrimSpace(model)
	provider, modelID, found := strings.Cut(model, "/")
	if found && strings.EqualFold(provider, "copilot") {
		return modelID
	}
	return model
}

// GetRequiredSecretNames returns the list of secrets required by the Codex engine
// and any common MCP secrets.
func (e *CodexEngine) GetRequiredSecretNames(workflowData *WorkflowData) []string {
	var secrets []string
	provider := e.ResolveLLMProvider(workflowData)
	if provider != LLMProviderGitHub || !hasCopilotRequestsWritePermission(workflowData) {
		secrets = append(secrets, llmProviderSecretNames(provider)...)
	}
	return append(secrets, collectCommonMCPSecrets(workflowData)...)
}

// GetSupportedEnvVarKeys returns the engine.env variable names that the Codex engine
// supports as defined in the AWF specification.
func (e *CodexEngine) GetSupportedEnvVarKeys() []string {
	return []string{
		constants.CodexAPIKey,
		constants.OpenAIAPIKey,
	}
}

// GetSecretValidationStep returns the secret validation step for the Codex engine.
// Returns an empty step if custom command is specified.
func (e *CodexEngine) GetSecretValidationStep(workflowData *WorkflowData) GitHubActionStep {
	provider := e.ResolveLLMProvider(workflowData)
	return BuildEngineSecretValidationStep(workflowData, EngineSecretValidationConfig{
		SecretNames: llmProviderSecretNames(provider),
		EngineName:  "Codex",
		DocsURL:     llmProviderDocsURL(provider),
		Skip: func(workflowData *WorkflowData) bool {
			return provider == LLMProviderGitHub && hasCopilotRequestsWritePermission(workflowData)
		},
	})
}

func (e *CodexEngine) GetInstallationSteps(workflowData *WorkflowData) []GitHubActionStep {
	codexEngineLog.Printf("Generating installation steps for Codex engine: workflow=%s", workflowData.Name)

	// Skip installation if custom command is specified
	if workflowData.EngineConfig != nil && workflowData.EngineConfig.Command != "" {
		codexEngineLog.Printf("Skipping Codex CLI installation: custom command specified (%s)", workflowData.EngineConfig.Command)
		return buildNpmEngineInstallStepsWithAWF(nil, workflowData, false)
	}

	steps := BuildStandardNpmEngineInstallStepsNoCooldown(
		"@openai/codex",
		string(constants.DefaultCodexVersion),
		"Install Codex CLI",
		"codex",
		workflowData,
	)
	if isDockerSbxRuntime(workflowData) || isCloudHypervisorRuntime(workflowData) {
		steps = append(steps, generateCodexDockerSbxCLIInstallStep(workflowData))
	}

	// Add AWF installation step if firewall is enabled
	if isFirewallEnabled(workflowData) {
		firewallConfig := getFirewallConfig(workflowData)
		agentConfig := getAgentConfig(workflowData)
		var awfVersion string
		if firewallConfig != nil {
			awfVersion = firewallConfig.Version
		}

		// gVisor must be installed and registered BEFORE AWF starts the agent container.
		if isGVisorRuntime(workflowData) && isRuntimeInstallEnabled(workflowData) {
			steps = append(steps, generateGVisorInstallStep())
		}

		// docker-sbx must be installed, authenticated, and smoke-tested BEFORE AWF.
		if isDockerSbxRuntime(workflowData) {
			if isRuntimeInstallEnabled(workflowData) {
				steps = append(steps, generateDockerSbxKVMCheckStep())
				steps = append(steps, generateDockerSbxSecretsCheckStep())
				steps = append(steps, generateDockerSbxInstallStep())
				steps = append(steps, generateDockerSbxAuthAndDaemonStep())
				steps = append(steps, generateDockerSbxPreFlightStep())
			}
		}
		if isCloudHypervisorRuntime(workflowData) {
			steps = append(steps, generateCloudHypervisorKVMAccessStep())
			steps = append(steps, generateCloudHypervisorHostPreflightStep())
			steps = append(steps, generateCloudHypervisorBundleSetupStep(getAWFVersionForSetup(workflowData)))
		}

		// Install AWF binary (or skip if custom command is specified)
		awfInstall := generateAWFInstallationStep(awfVersion, agentConfig)
		if len(awfInstall) > 0 {
			steps = append(steps, awfInstall)
		}
	}

	return steps
}

func generateCodexDockerSbxCLIInstallStep(workflowData *WorkflowData) GitHubActionStep {
	version := string(constants.DefaultCodexVersion)
	if workflowData.EngineConfig != nil && workflowData.EngineConfig.Version != "" {
		version = workflowData.EngineConfig.Version
	}
	return GenerateDockerSbxNpmCLIInstallStep(
		"@openai/codex",
		version,
		"Install Codex CLI in docker-sbx path",
		"codex",
		false,
		false,
	)
}

// codexPluginMarketplaceName returns the deterministic local marketplace name Codex
// registers the index-th checked-out Agent Plugin under. It only uses characters Codex's
// plugin/marketplace name validation accepts ([A-Za-z0-9_-]+).
func codexPluginMarketplaceName(index int) string {
	return fmt.Sprintf("gh-aw-plugin-%d", index)
}

// GetPluginInstallationSteps checks out pinned Agent Plugins and registers each one as a
// single-plugin local Codex marketplace, since the Codex CLI only installs plugins through
// "codex plugin add <name>@<marketplace>" and has no flag to load a bare plugin directory.
// For each checked-out plugin this writes a ".agents/plugins/marketplace.json" manifest
// inside the checkout that points back at the plugin's own directory, reads the plugin's
// declared name from its "plugin.json" manifest, then runs "codex plugin marketplace add"
// followed by "codex plugin add".
func (e *CodexEngine) GetPluginInstallationSteps(workflowData *WorkflowData) []GitHubActionStep {
	commandName := e.codexCommandName(workflowData)
	return generatePluginInstallationSteps(workflowData, pluginInstallSpec{
		CustomInstall: func(parsed parsedSkillRefSpec, checkoutPath, installPath string, index int) []GitHubActionStep {
			marketplaceName := codexPluginMarketplaceName(index)
			pluginSubpath := pluginRepoSubpath(parsed)
			manifestPath := path.Join(checkoutPath, pluginSubpath, "plugin.json")
			marketplaceDir := path.Join(checkoutPath, ".agents/plugins")
			marketplaceManifestPath := path.Join(marketplaceDir, "marketplace.json")
			installCommand := strings.Join([]string{
				fmt.Sprintf("PLUGIN_NAME=$(jq -r %s %q)", `'.name // empty'`, manifestPath),
				fmt.Sprintf(`if [ -z "$PLUGIN_NAME" ]; then echo %s; exit 1; fi`, shellEscapeArg(fmt.Sprintf("::error::Agent plugin %s is missing a \"name\" in plugin.json", parsed.repoPath))),
				fmt.Sprintf("mkdir -p %q", marketplaceDir),
				fmt.Sprintf("jq -n --arg name \"$PLUGIN_NAME\" --arg path %q '{name: %q, plugins: [{name: $name, source: {source: \"local\", path: $path}}]}' > %q", pluginSubpath, marketplaceName, marketplaceManifestPath),
				fmt.Sprintf("%s plugin marketplace add %q", commandName, "./"+checkoutPath),
				fmt.Sprintf(`%s plugin add "$PLUGIN_NAME@%s"`, commandName, marketplaceName),
			}, "\n")
			installStep := []string{"      - name: Install agent plugin " + parsed.repoPath}
			return []GitHubActionStep{FormatStepWithCommandAndEnv(installStep, installCommand, nil)}
		},
	})
}

// GetDeclaredOutputFiles returns the output files that Codex may produce.
// Use /tmp/gh-aw for Codex runtime logs because ${RUNNER_TEMP}/gh-aw is
// mounted read-only inside the AWF chroot sandbox.
func (e *CodexEngine) GetDeclaredOutputFiles() []string {
	// Return the Codex log directory for artifact collection.
	return []string{
		constants.TmpMcpConfigLogsDir,
	}
}

// GetAgentManifestFiles returns Codex-specific instruction files that should be
// treated as security-sensitive manifests.  AGENTS.md is the primary OpenAI
// Codex agent-instruction file; modifying it can redirect agent behaviour.
func (e *CodexEngine) GetAgentManifestFiles() []string {
	return []string{"AGENTS.md"}
}

// GetAgentManifestPathPrefixes returns Codex-specific config directory prefixes.
// The .codex/ directory can contain agent configuration and task-specific settings.
func (e *CodexEngine) GetAgentManifestPathPrefixes() []string {
	return []string{".codex/"}
}

// GetHarnessScriptName returns the filename of the JavaScript harness script that wraps
// Codex CLI execution with retry logic for transient OpenAI API errors.
func (e *CodexEngine) GetHarnessScriptName() string {
	return "codex_harness.cjs"
}

// GetExecutionSteps returns the GitHub Actions steps for executing Codex
func (e *CodexEngine) GetExecutionSteps(workflowData *WorkflowData, logFile string) []GitHubActionStep {
	modelConfigured := workflowData.Model != ""
	firewallEnabled := isFirewallEnabled(workflowData)
	codexEngineLog.Printf("Building Codex execution steps: workflow=%s, modelConfigured=%v, firewall=%v",
		workflowData.Name, modelConfigured, firewallEnabled)

	modelEnvVar := e.codexModelEnvVar(workflowData)
	structuredOutputParam, detectionSchemaWriteCmd := e.codexStructuredOutputConfig(workflowData)
	commandName := e.codexCommandName(workflowData)
	harnessScriptName := e.codexHarnessScriptName(workflowData)
	codexCommand := e.buildCodexCommand(workflowData, commandName, harnessScriptName, firewallEnabled, modelEnvVar, structuredOutputParam)
	command := e.buildCodexExecutionCommand(workflowData, logFile, codexCommand, harnessScriptName, detectionSchemaWriteCmd, firewallEnabled)
	env := e.buildCodexExecutionEnv(workflowData, firewallEnabled, modelConfigured, modelEnvVar)
	step := e.buildCodexExecutionStep(workflowData, command, env)
	return []GitHubActionStep{step}
}

func (e *CodexEngine) codexModelEnvVar(workflowData *WorkflowData) string {
	switch {
	case workflowRunPhase(workflowData) == runPhaseEvals:
		return constants.EnvVarModelEvalsCodex
	case isDetectionRun(workflowData):
		return constants.EnvVarModelDetectionCodex
	default:
		return constants.EnvVarModelAgentCodex
	}
}

func (e *CodexEngine) codexStructuredOutputConfig(workflowData *WorkflowData) (string, string) {
	if !workflowData.IsDetectionRun {
		return "", ""
	}
	codexEngineLog.Printf("Enabling structured outputs for Codex detection run")
	return fmt.Sprintf(` --output-schema %s -o %s`, detectionSchemaFilePath, detectionResultFilePath),
		fmt.Sprintf("mkdir -p /tmp/gh-aw/threat-detection && printf '%%s' '%s' > %s", detectionResponseSchema, detectionSchemaFilePath)
}

func (e *CodexEngine) codexCommandName(workflowData *WorkflowData) string {
	if workflowData.EngineConfig != nil && workflowData.EngineConfig.Command != "" {
		codexEngineLog.Printf("Using custom command: %s", workflowData.EngineConfig.Command)
		return workflowData.EngineConfig.Command
	}
	return "codex"
}

func (e *CodexEngine) codexHarnessScriptName(workflowData *WorkflowData) string {
	harnessScriptName := e.GetHarnessScriptName()
	if workflowData.EngineConfig != nil && workflowData.EngineConfig.HarnessScript != "" {
		harnessScriptName = workflowData.EngineConfig.HarnessScript
		codexEngineLog.Printf("Using custom harness script: %s", harnessScriptName)
	}
	return harnessScriptName
}

func (e *CodexEngine) buildCodexCommand(workflowData *WorkflowData, commandName, harnessScriptName string, firewallEnabled bool, modelEnvVar, structuredOutputParam string) string {
	modelParam := fmt.Sprintf(`${%s:+ --model "$%s"}`, modelEnvVar, modelEnvVar)
	executionPolicyParam := ` --sandbox workspace-write --skip-git-repo-check -c approval_policy="never" `
	if firewallEnabled {
		executionPolicyParam = " --dangerously-bypass-approvals-and-sandbox --skip-git-repo-check "
	}
	webSearchParam := ` -c web_search="disabled"`
	if workflowData.ParsedTools != nil && workflowData.ParsedTools.WebSearch != nil {
		webSearchParam = ""
	}
	webFetchParam := ` -c fetch="disabled"`
	if workflowData.ParsedTools != nil && workflowData.ParsedTools.WebFetch != nil {
		webFetchParam = ""
	}
	shellToolParam := ""
	if workflowData.BashDisabled {
		// tools.bash was fully disabled (bash: false, or bash: []). Codex cannot enforce a
		// per-command allowlist, but it can refuse all shell execution via this feature flag.
		shellToolParam = ` -c features.shell_tool=false`
	}
	customArgsParam := ""
	if workflowData.EngineConfig != nil && len(workflowData.EngineConfig.Args) > 0 {
		var sb strings.Builder
		for _, arg := range workflowData.EngineConfig.Args {
			sb.WriteString(arg + " ")
		}
		customArgsParam = sb.String()
	}
	if harnessScriptName != "" {
		execPrefix := fmt.Sprintf(`%s %s/%s %s`, nodeRuntimeResolutionCommand, SetupActionDestinationShell, harnessScriptName, commandName)
		return fmt.Sprintf("%s exec%s%s%s%s%s%s%s --prompt-file /tmp/gh-aw/aw-prompts/prompt.txt",
			execPrefix, modelParam, webSearchParam, webFetchParam, shellToolParam, executionPolicyParam, structuredOutputParam, customArgsParam)
	}
	return getWorkspaceCommandPrefixFor(workflowData.EngineConfig) + fmt.Sprintf("%s exec%s%s%s%s%s%s%s \"$INSTRUCTION\"",
		commandName, modelParam, webSearchParam, webFetchParam, shellToolParam, executionPolicyParam, structuredOutputParam, customArgsParam)
}

func (e *CodexEngine) buildCodexExecutionCommand(workflowData *WorkflowData, logFile, codexCommand, harnessScriptName, detectionSchemaWriteCmd string, firewallEnabled bool) string {
	if firewallEnabled {
		var codexCommandWithSetup string
		if harnessScriptName != "" {
			codexCommandWithSetup = fmt.Sprintf(`%s && %s`, GetNpmBinPathSetup(), codexCommand)
		} else {
			codexCommandWithSetup = fmt.Sprintf(`%s && INSTRUCTION="$(cat /tmp/gh-aw/aw-prompts/prompt.txt)" && %s`, GetNpmBinPathSetup(), codexCommand)
		}
		if dockerSbxCLIPath := GetDockerSbxNpmCLIPathSetup(workflowData); dockerSbxCLIPath != "" {
			codexCommandWithSetup = fmt.Sprintf("%s && %s", dockerSbxCLIPath, codexCommandWithSetup)
		}
		if mcpCLIPath := GetMCPCLIPathSetup(workflowData); mcpCLIPath != "" {
			codexCommandWithSetup = fmt.Sprintf("%s && %s", mcpCLIPath, codexCommandWithSetup)
		}
		if e.ResolveLLMProvider(workflowData) == LLMProviderGitHub {
			codexCommandWithSetup = codexBYOKAPIKeyExport() + " && " + codexCommandWithSetup
		}
		return BuildAWFCommand(AWFCommandConfig{
			EngineName:         "codex",
			EngineCommand:      codexCommandWithSetup,
			LogFile:            logFile,
			WorkflowData:       workflowData,
			UsesTTY:            false,
			AllowedDomains:     e.codexAllowedDomains(workflowData),
			PathSetup:          e.codexPathSetup(workflowData, detectionSchemaWriteCmd),
			ExcludeEnvVarNames: ComputeAWFExcludeEnvVarNames(workflowData, []string{"CODEX_API_KEY", "OPENAI_API_KEY", "COPILOT_GITHUB_TOKEN"}),
		})
	}
	schemaWritePrefix := ""
	if workflowData.IsDetectionRun {
		schemaWritePrefix = detectionSchemaWriteCmd + " && "
	}
	if harnessScriptName != "" {
		return fmt.Sprintf(`set -o pipefail
printf '%%s' "$(date +%%s%%3N)" > %s
touch %s
(umask 177 && touch %s)
mkdir -p "$CODEX_HOME/logs"
%s%s 2>&1 | tee %s`, AgentCLIStartMsPath, AgentStepSummaryPath, logFile, schemaWritePrefix, codexCommand, logFile)
	}
	return fmt.Sprintf(`set -o pipefail
printf '%%s' "$(date +%%s%%3N)" > %s
touch %s
(umask 177 && touch %s)
INSTRUCTION="$(cat "$GH_AW_PROMPT")"
mkdir -p "$CODEX_HOME/logs"
%s%s 2>&1 | tee %s`, AgentCLIStartMsPath, AgentStepSummaryPath, logFile, schemaWritePrefix, codexCommand, logFile)
}

func (e *CodexEngine) codexAllowedDomains(workflowData *WorkflowData) string {
	allowedDomains := workflowData.CachedAllowedDomainsStr
	if !workflowData.CachedAllowedDomainsComputed {
		allowedDomains = GetAllowedDomainsForEngine(constants.CodexEngine, workflowData.NetworkPermissions, workflowData.Tools, workflowData.Runtimes)
	}
	if workflowData.EngineConfig != nil && workflowData.EngineConfig.APITarget != "" {
		allowedDomains = mergeAPITargetDomains(allowedDomains, workflowData.EngineConfig.APITarget)
	}
	return allowedDomains
}

func (e *CodexEngine) defaultDomains(workflowData *WorkflowData) []string {
	defaults := append([]string{}, CodexDefaultDomains...)
	if e.ResolveLLMProvider(workflowData) == LLMProviderGitHub {
		defaults = append(defaults, CopilotDefaultDomains...)
	}
	return defaults
}

func (e *CodexEngine) codexPathSetup(workflowData *WorkflowData, detectionSchemaWriteCmd string) string {
	base := "mkdir -p \"$CODEX_HOME/logs\" && touch " + AgentStepSummaryPath
	if workflowData.IsDetectionRun {
		return base + " && " + detectionSchemaWriteCmd
	}
	return base
}

func codexBYOKAPIKeyExport() string {
	return "export CODEX_API_KEY=\"$" + constants.CopilotBYOKDummyAPIKeyEnvVar + "\""
}

func (e *CodexEngine) buildCodexExecutionEnv(workflowData *WorkflowData, firewallEnabled, modelConfigured bool, modelEnvVar string) map[string]string {
	effectiveGitHubToken := getEffectiveGitHubToken("")
	provider := e.ResolveLLMProvider(workflowData)
	env := map[string]string{
		"CODEX_HOME":                   constants.TmpMcpConfigDir,
		"GH_AW_GITHUB_TOKEN":           effectiveGitHubToken,
		"GH_AW_LLM_PROVIDER":           string(provider),
		"GH_AW_MCP_CONFIG":             constants.CodexMcpConfigTomlPath,
		"GH_AW_PROMPT":                 constants.AwPromptsFile,
		"GITHUB_AW":                    "true",
		"GITHUB_PERSONAL_ACCESS_TOKEN": effectiveGitHubToken,
		"GITHUB_STEP_SUMMARY":          AgentStepSummaryPath,
		"RUNNER_TEMP":                  "${{ runner.temp }}",
		"RUST_LOG":                     "${{ runner.debug == 1 && 'trace,hyper_util=info,mio=info,reqwest=info,os_info=info,codex_otel=warn,codex_core=debug,codex_exec=debug' || 'warn' }}",
	}
	if provider == LLMProviderGitHub {
		copilotToken := llmProviderSecretExpression(provider, workflowData)
		env["COPILOT_GITHUB_TOKEN"] = copilotToken
		env[constants.CopilotBYOKDummyAPIKeyEnvVar] = constants.CopilotBYOKDummyAPIKey
	} else {
		openAIKey := llmProviderSecretExpression(provider, workflowData)
		env["CODEX_API_KEY"] = openAIKey
		env["OPENAI_API_KEY"] = openAIKey
	}
	injectWorkflowCallNetworkAllowedEnv(env, workflowData)
	env["GH_AW_PHASE"] = workflowRunPhase(workflowData)
	if IsRelease() {
		env["GH_AW_VERSION"] = GetVersion()
	} else {
		env["GH_AW_VERSION"] = "dev"
	}
	applySafeOutputEnvToMap(env, workflowData)
	applyDefaultMaxAICreditsEnvToMap(env, workflowData)
	applyTraceContextEnvToMap(env)
	if firewallEnabled {
		maps.Copy(env, getGitIdentityEnvVars())
		env["AWF_REFLECT_ENABLED"] = "1"
	}
	applyOptionalEngineToolTimeouts(env, workflowData)
	applyEngineMaxTurnsEnv(env, workflowData)
	applyEngineHarnessRetryEnv(env, workflowData)
	if modelConfigured {
		if containsExpression(workflowData.Model) {
			env[constants.EnvVarModelFallback] = compilerenv.BuildModelOverrideExpression(modelEnvVar, compilerenv.DefaultModelCodex, constants.CodexDefaultModel)
		}
		model := codexModelID(workflowData.Model)
		codexEngineLog.Printf("Setting %s env var for model: %s", modelEnvVar, model)
		env[modelEnvVar] = model
	} else {
		env[modelEnvVar] = compilerenv.BuildModelOverrideExpression(modelEnvVar, compilerenv.DefaultModelCodex, constants.CodexDefaultModel)
	}
	applyEngineCwdEnv(env, workflowData)
	applyEngineAndAgentEnv(env, workflowData, codexEngineLog)
	applyMCPScriptsSecretEnv(env, workflowData)
	return env
}

func (e *CodexEngine) buildCodexExecutionStep(workflowData *WorkflowData, command string, env map[string]string) GitHubActionStep {
	stepLines := []string{
		"      - name: Execute Codex CLI",
		"        id: agentic_execution",
		"        timeout-minutes: " + resolveStepTimeoutValue(workflowData),
	}
	filteredEnv := FilterEnvForSecrets(env, e.GetRequiredSecretNames(workflowData))
	addCliProxyGHTokenToEnv(filteredEnv, workflowData)
	return GitHubActionStep(FormatStepWithCommandAndEnv(stepLines, wrapAgentExecutionCommand(command), filteredEnv))
}

// GetSquidLogsSteps returns the steps for uploading and parsing Squid logs (after secret redaction)
func (e *CodexEngine) GetSquidLogsSteps(workflowData *WorkflowData) []GitHubActionStep {
	return defaultGetSquidLogsSteps(workflowData, codexEngineLog)
}

// expandNeutralToolsToCodexTools converts neutral tools to Codex-specific tools format
// This ensures that playwright tools get the same allowlist as the copilot agent
// Updated to use ToolsConfig instead of map[string]any
func (e *CodexEngine) expandNeutralToolsToCodexTools(toolsConfig *ToolsConfig) *ToolsConfig {
	if toolsConfig == nil {
		return &ToolsConfig{
			Custom: make(map[string]MCPServerConfig),
			raw:    make(map[string]any),
		}
	}

	// Create a copy of the tools config
	result := &ToolsConfig{
		GitHub:           toolsConfig.GitHub,
		Bash:             toolsConfig.Bash,
		WebFetch:         toolsConfig.WebFetch,
		WebSearch:        toolsConfig.WebSearch,
		Edit:             toolsConfig.Edit,
		Playwright:       toolsConfig.Playwright,
		AgenticWorkflows: toolsConfig.AgenticWorkflows,
		CacheMemory:      toolsConfig.CacheMemory,
		Timeout:          toolsConfig.Timeout,
		StartupTimeout:   toolsConfig.StartupTimeout,
		Custom:           make(map[string]MCPServerConfig),
		raw:              make(map[string]any),
	}

	// Copy custom tools
	maps.Copy(result.Custom, toolsConfig.Custom)

	// Copy raw map
	maps.Copy(result.raw, toolsConfig.raw)

	// Playwright is a CLI tool and must not be added to the MCP configuration.
	if toolsConfig.Playwright != nil {
		applyCodexPlaywrightTool(result, toolsConfig.Playwright)
	}

	return result
}

func applyCodexPlaywrightTool(result *ToolsConfig, playwright *PlaywrightToolConfig) {
	playwrightConfig := &PlaywrightToolConfig{
		Version: playwright.Version,
		Mode:    playwright.Mode,
	}
	result.Playwright = playwrightConfig
	delete(result.raw, "playwright")
}

// expandNeutralToolsToCodexToolsFromMap is a backward compatibility wrapper
// that accepts map[string]any instead of *ToolsConfig
func (e *CodexEngine) expandNeutralToolsToCodexToolsFromMap(tools map[string]any) map[string]any {
	toolsConfig, _ := ParseToolsConfig(tools)
	result := e.expandNeutralToolsToCodexTools(toolsConfig)
	return result.ToMap()
}

func (e *CodexEngine) getShellEnvironmentPolicyVars(tools map[string]any, mcpTools []string) []string {
	// Collect all environment variables needed by MCP servers
	envVars := make(map[string]struct{})

	// Always include core environment variables
	envVars["PATH"] = struct{}{}
	envVars["HOME"] = struct{}{}

	// Add CODEX_API_KEY for authentication
	envVars["CODEX_API_KEY"] = struct{}{}
	envVars["OPENAI_API_KEY"] = struct{}{} // Fallback for CODEX_API_KEY

	// Check each MCP tool for required environment variables
	for _, toolName := range mcpTools {
		addMCPToolEnvVars(toolName, tools, envVars)
	}

	sortedEnvVars := sliceutil.SortedKeys(envVars)

	// Codex expects regex patterns for shell_environment_policy.include_only, not literal names.
	// Anchor each variable name to avoid accidental substring matches (for example "PATH" matching "PATH_SUFFIX").
	var includeOnlyPatterns []string
	for _, envVar := range sortedEnvVars {
		includeOnlyPatterns = append(includeOnlyPatterns, "^"+regexp.QuoteMeta(envVar)+"$")
	}
	return includeOnlyPatterns
}

// addMCPToolEnvVars adds the environment variables required by the named MCP tool
// to the envVars set. For custom tools, it reads the "env" configuration map.
func addMCPToolEnvVars(toolName string, tools map[string]any, envVars map[string]struct{}) {
	switch toolName {
	case "github":
		// GitHub MCP server needs GITHUB_PERSONAL_ACCESS_TOKEN
		envVars["GITHUB_PERSONAL_ACCESS_TOKEN"] = struct{}{}
	case "agentic-workflows":
		// Agentic workflows MCP server needs GITHUB_TOKEN
		envVars["GITHUB_TOKEN"] = struct{}{}
	case "safe-outputs":
		// Safe outputs MCP server needs several environment variables
		envVars["GH_AW_SAFE_OUTPUTS"] = struct{}{}
		envVars["GH_AW_ASSETS_BRANCH"] = struct{}{}
		envVars["GH_AW_ASSETS_MAX_SIZE_KB"] = struct{}{}
		envVars["GH_AW_ASSETS_ALLOWED_EXTS"] = struct{}{}
		envVars["GITHUB_REPOSITORY"] = struct{}{}
		envVars["GITHUB_SERVER_URL"] = struct{}{}
	default:
		// For custom MCP tools, check if they have env configuration
		if toolValue, ok := tools[toolName]; ok {
			if toolConfig, ok := toolValue.(map[string]any); ok {
				// Extract environment variable names from env configuration
				if env, hasEnv := toolConfig["env"].(map[string]any); hasEnv {
					for envKey := range env {
						envVars[envKey] = struct{}{}
					}
				}
			}
		}
	}
}

// renderShellEnvironmentPolicy generates the [shell_environment_policy] section for config.toml
// This controls which environment variables are passed through to MCP servers for security
func (e *CodexEngine) renderShellEnvironmentPolicy(yaml *strings.Builder, tools map[string]any, mcpTools []string) {
	sortedEnvVars := e.getShellEnvironmentPolicyVars(tools, mcpTools)

	// Render [shell_environment_policy] section
	yaml.WriteString("          \n")
	yaml.WriteString("          [shell_environment_policy]\n")
	yaml.WriteString("          inherit = \"core\"\n")
	yaml.WriteString("          include_only = [")
	for i, envVar := range sortedEnvVars {
		if i > 0 {
			yaml.WriteString(", ")
		}
		yaml.WriteString("\"" + envVar + "\"")
	}
	yaml.WriteString("]\n")
}

func (e *CodexEngine) renderShellEnvironmentPolicyToml(yaml *strings.Builder, tools map[string]any, mcpTools []string, indent string) {
	sortedEnvVars := e.getShellEnvironmentPolicyVars(tools, mcpTools)

	yaml.WriteString(indent + "[shell_environment_policy]\n")
	yaml.WriteString(indent + "inherit = \"core\"\n")
	yaml.WriteString(indent + "include_only = [")
	for i, envVar := range sortedEnvVars {
		if i > 0 {
			yaml.WriteString(", ")
		}
		yaml.WriteString("\"" + envVar + "\"")
	}
	yaml.WriteString("]\n")
}

// RenderMCPConfig is implemented in codex_mcp.go

// renderCodexMCPConfig is implemented in codex_mcp.go

// ParseLogMetrics is implemented in codex_logs.go

// parseCodexToolCallsWithSequence is implemented in codex_logs.go

// updateMostRecentToolWithDuration is implemented in codex_logs.go

// extractCodexTokenUsage is implemented in codex_logs.go

// GetLogParserScriptId is implemented in codex_logs.go
