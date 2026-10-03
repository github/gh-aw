package workflow

import (
	"fmt"
	"maps"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/workflow/compilerenv"
)

var piLog = logger.New("workflow:pi_engine")

// PiEngine represents the Pi AI coding agent.
// Pi is a provider-agnostic agentic coding assistant that communicates via stdin/stdout
// and emits a streaming JSONL log for structured event capture.  When engine.model uses
// provider/model format (e.g. "copilot/claude-sonnet-4-20250514"), Pi borrows the
// matching engine's AWF configuration (secrets, gateway port, allowed domains) so the
// firewall can route LLM traffic through the correct sidecar port.  Without a provider
// prefix Pi defaults to the GitHub/Copilot gateway.
//
// Native MCP connects through the compiler-managed MCP gateway. CLI-mounted tools
// remain available when a workflow explicitly selects cli-proxy.
type PiEngine struct {
	BaseEngine
}

var _ CodingAgentEngine = (*PiEngine)(nil)

// NewPiEngine creates and returns a new PiEngine instance.
func NewPiEngine() *PiEngine {
	return &PiEngine{
		BaseEngine: BaseEngine{
			id:               "pi",
			displayName:      "Pi",
			description:      "Pi AI coding agent",
			experimental:     false,
			ghSkillAgentName: "pi",
			capabilities: EngineCapabilities{
				ToolsAllowlist:       true,
				MCP:                  true,
				MaxTurns:             true,
				ContextWindow:        true,
				MaxContinuations:     false,
				WebSearch:            false,
				NativeAgentFile:      false,
				BareMode:             true,
				BashCommandAllowlist: true,
				BashDisable:          true,
			},
		},
	}
}

// GetModelEnvVarName returns the legacy Pi model env-var name exposed by gh-aw.
// gh-aw passes the model to the Pi CLI via --model and separately exports the
// original workflow model for extensions.
func (e *PiEngine) GetModelEnvVarName() string {
	return constants.PiCLIModelEnvVar
}

// ResolveLLMProvider returns the effective provider for Pi inference.
// Default is github, overridable via engine.model-provider.
func (e *PiEngine) ResolveLLMProvider(workflowData *WorkflowData) LLMProvider {
	return resolveEngineLLMProvider(workflowData, LLMProvider(piReflectProviderName(resolvePiBackend(workflowData))))
}

// resolvePiBackend extracts the provider prefix from the engine model (if any) and maps
// it to the matching UniversalLLMBackend.  A model without a slash (e.g. "claude-sonnet-4")
// defaults to the GitHub (Copilot) backend for backward compatibility.
//
// "github-copilot/" is accepted as an alias for "copilot/" since that is the
// provider name used by Pi CLI's built-in model registry.
func resolvePiBackend(workflowData *WorkflowData) UniversalLLMBackend {
	if workflowData == nil || workflowData.EngineConfig == nil || workflowData.Model == "" {
		return UniversalLLMBackendCopilot
	}
	model := workflowData.Model
	if workflowData.EngineConfig.LLMProvider != "" {
		switch normalizeLLMProvider(string(workflowData.EngineConfig.LLMProvider)) {
		case LLMProviderAnthropic:
			return UniversalLLMBackendAnthropic
		case LLMProviderOpenAI:
			return UniversalLLMBackendCodex
		case "google", "gemini":
			return piBackendGoogle
		}
	}
	if !strings.Contains(model, "/") {
		// No provider prefix — default to Copilot (backward compatibility).
		return UniversalLLMBackendCopilot
	}
	// "github-copilot" is Pi CLI's internal name for GitHub Copilot.  Accept it as
	// an alias so workflows can use either "copilot/..." or "github-copilot/...".
	backend, err := resolveBackendWithAliases(model, map[string]UniversalLLMBackend{
		"github-copilot": UniversalLLMBackendCopilot,
		"google":         piBackendGoogle,
		"gemini":         piBackendGoogle,
	})
	if err != nil {
		piLog.Printf("Could not resolve backend for Pi model %q, defaulting to copilot: %v", model, err)
		return UniversalLLMBackendCopilot
	}
	return backend
}

// extractPiModelID returns the model ID portion of a provider/model string.
// For "copilot/claude-sonnet-4" it returns "claude-sonnet-4".
// For a bare model name (no slash) the whole string is returned unchanged.
func extractPiModelID(model string) string {
	if _, after, found := strings.Cut(model, "/"); found {
		return after
	}
	return model
}

// piReflectProviderName maps an AWF UniversalLLMBackend to the normalized
// provider name used by the AWF api-proxy /reflect endpoint and the
// GH_AW_LLM_PROVIDER convention (see llm_provider.go). This is distinct from
// piConfiguredProvider, which returns Pi-CLI-specific provider identifiers.
func piReflectProviderName(backend UniversalLLMBackend) string {
	switch backend {
	case UniversalLLMBackendAnthropic:
		return string(LLMProviderAnthropic)
	case UniversalLLMBackendCodex:
		return string(LLMProviderOpenAI)
	case piBackendGoogle:
		return "google"
	default:
		return string(LLMProviderGitHub)
	}
}

func resolvePiGatewaySecretEnvVar(profile universalLLMBackendProfile, backend UniversalLLMBackend) string {
	if len(profile.coreSecretNames) > 0 {
		return profile.coreSecretNames[0] //nolint:uncheckedsliceindex // len(profile.coreSecretNames) is checked above.
	}

	switch backend {
	case UniversalLLMBackendAnthropic:
		return "ANTHROPIC_API_KEY"
	case UniversalLLMBackendCodex:
		return "CODEX_API_KEY"
	case piBackendGoogle:
		return "GEMINI_API_KEY"
	default:
		// copilot-requests: write intentionally leaves coreSecretNames empty and
		// uses COPILOT_GITHUB_TOKEN=${{ github.token }} instead.
		return "COPILOT_GITHUB_TOKEN"
	}
}

// GetRequiredSecretNames returns the list of secrets required by the Pi engine.
// When the model uses provider/model format the provider-specific secret is required
// (e.g. ANTHROPIC_API_KEY for "anthropic/..."); otherwise Pi routes through the
// Copilot LLM gateway and reuses COPILOT_GITHUB_TOKEN.
func (e *PiEngine) GetRequiredSecretNames(workflowData *WorkflowData) []string {
	piLog.Print("Collecting required secrets for Pi engine")
	profile := piProviderProfile(workflowData)
	secrets := append([]string{}, profile.coreSecretNames...)
	secrets = append(secrets, collectCommonMCPSecrets(workflowData)...)
	return secrets
}

// GetSupportedEnvVarKeys returns the engine.env variable names that the Pi engine
// supports as defined in the AWF specification. Pi is a multi-provider engine so all
// provider API keys are valid engine.env overrides.
func (e *PiEngine) GetSupportedEnvVarKeys() []string {
	keys := []string{
		constants.CopilotGitHubToken,
		constants.AnthropicAPIKey,
		constants.CodexAPIKey,
		constants.OpenAIAPIKey,
		"ANTHROPIC_OAUTH_TOKEN",
		"ANTHROPIC_AUTH_TOKEN",
		"AWS_ACCESS_KEY_ID",
		"AWS_SECRET_ACCESS_KEY",
		"AWS_SESSION_TOKEN",
		"AWS_BEARER_TOKEN_BEDROCK",
		"GOOGLE_CLOUD_API_KEY",
	}
	for _, providerKeys := range piNativeProviderKeys {
		keys = append(keys, providerKeys...)
	}
	slices.Sort(keys)
	return slices.Compact(keys)
}

// GetSecretValidationStep returns the secret validation step for the Pi engine.
// The validated secret depends on the resolved provider backend.
func (e *PiEngine) GetSecretValidationStep(workflowData *WorkflowData) GitHubActionStep {
	profile := piProviderProfile(workflowData)
	return BuildEngineSecretValidationStep(workflowData, EngineSecretValidationConfig{
		SecretNames: profile.coreSecretNames,
		EngineName:  "Pi",
		DocsURL:     "https://github.github.com/gh-aw/engines/pi/",
	})
}

// GetInstallationSteps returns the GitHub Actions steps needed to install the Pi CLI.
// If engine.extensions is configured, additional `pi install <extension>` steps are emitted
// after the main CLI install step.
func (e *PiEngine) GetInstallationSteps(workflowData *WorkflowData) []GitHubActionStep {
	piLog.Printf("Generating installation steps for Pi engine: workflow=%s", workflowData.Name)

	var steps []GitHubActionStep
	if workflowData.EngineConfig != nil && workflowData.EngineConfig.Command != "" {
		piLog.Printf("Skipping Pi CLI installation: custom command specified (%s)", workflowData.EngineConfig.Command)
		steps = buildNpmEngineInstallStepsWithAWF(nil, workflowData, false)
	} else {
		version := string(constants.DefaultPiVersion)
		if workflowData.EngineConfig != nil && workflowData.EngineConfig.Version != "" {
			version = workflowData.EngineConfig.Version
		}

		npmSteps := GenerateNpmInstallSteps(
			"@earendil-works/pi-coding-agent",
			version,
			"Install Pi CLI",
			"pi",
			NPMInstallOptions{
				IncludeNodeSetup:  true,
				RunInstallScripts: false,
				CooldownEnabled:   false,
			},
		)

		// Cloud Hypervisor cannot see the globally installed
		// CLI in the hosted tool cache, so stage a second copy under RUNNER_TEMP.
		if isCloudHypervisorRuntime(workflowData) {
			npmSteps = append(npmSteps, GenerateMicroVMNpmCLIInstallStep(
				"@earendil-works/pi-coding-agent",
				version,
				"Install Pi CLI in microVM path",
				"pi",
				false,
				false,
			))
		}

		steps = BuildNpmEngineInstallStepsWithAWF(npmSteps, workflowData)
	}

	return append(steps, e.extensionInstallationSteps(workflowData)...)
}

func (e *PiEngine) extensionInstallationSteps(data *WorkflowData) []GitHubActionStep {
	if data.EngineConfig == nil || len(data.EngineConfig.Extensions) == 0 {
		return nil
	}
	command := "pi"
	if data.EngineConfig.Command != "" {
		command = data.EngineConfig.Command
	}
	steps := make([]GitHubActionStep, 0, len(data.EngineConfig.Extensions))
	for _, source := range data.EngineConfig.Extensions {
		lines := []string{"      - name: " + strconv.Quote("Install Pi extension "+source)}
		lines = FormatStepWithCommandAndEnv(lines, fmt.Sprintf("%s install %s", command, shellEscapeArg(source)), map[string]string{
			"PI_CODING_AGENT_DIR": constants.TmpPiAgentDir, "PI_OFFLINE": "1",
		})
		steps = append(steps, GitHubActionStep(lines))
	}
	return steps
}

// GetDeclaredOutputFiles returns the output files that Pi may produce.
// The streaming JSONL log is the primary artifact for post-run analysis.
func (e *PiEngine) GetDeclaredOutputFiles() []string {
	return []string{
		PiStreamingLogFile,
		PiSessionDirectory + "/*.jsonl",
		constants.TmpPiAgentDir + "/session.html",
	}
}

// GetLogParserScriptId returns the script ID for parsing Pi logs.
func (e *PiEngine) GetLogParserScriptId() string {
	return "parse_pi_log"
}

// GetLogFileForParsing returns the Pi streaming log file path used by the JS log parser.
func (e *PiEngine) GetLogFileForParsing() string {
	return PiStreamingLogFile
}

// GetAgentManifestFiles returns Pi-specific instruction files treated as
// security-sensitive manifests.
func (e *PiEngine) GetAgentManifestFiles() []string {
	return []string{"PI.md", "AGENTS.md", "AGENTS.override.md", "AGENTS.MD", "CLAUDE.md", "CLAUDE.MD"}
}

// GetAgentManifestPathPrefixes returns Pi-specific config directory prefixes.
func (e *PiEngine) GetAgentManifestPathPrefixes() []string {
	return []string{".pi/"}
}

// GetExecutionSteps returns the GitHub Actions steps for executing the Pi CLI.
// The prompt is piped to Pi via stdin; streaming JSON events are written to
// PiStreamingLogFile for post-run analysis and step summary rendering.
func (e *PiEngine) GetExecutionSteps(workflowData *WorkflowData, logFile string) []GitHubActionStep {
	if workflowData.Model == "" {
		withModel := *workflowData
		withModel.Model = "copilot/gpt-5.4"
		workflowData = &withModel
	}
	piLog.Printf("Generating execution steps for Pi engine: workflow=%s, firewall=%v",
		workflowData.Name, isFirewallEnabled(workflowData))

	commandName := "pi"
	if workflowData.EngineConfig != nil && workflowData.EngineConfig.Command != "" {
		commandName = workflowData.EngineConfig.Command
	}

	// Resolve backend and profile early so we can use them when building piArgs.
	modelConfigured := workflowData.Model != ""
	backend := resolvePiBackend(workflowData)
	profile := piProviderProfile(workflowData)
	firewallEnabled := isFirewallEnabled(workflowData)

	// When engine.driver is set, run the driver script directly instead of the pi CLI.
	driverConfigured := workflowData.EngineConfig != nil && workflowData.EngineConfig.Driver != ""
	piArgs := e.buildPiArgs(workflowData)
	piModelsJSONSetup, piArgs := e.buildPiModelsJSONSetup(workflowData, profile, backend, piArgs, firewallEnabled, modelConfigured, driverConfigured)
	piCommand := e.buildPiCommand(workflowData, commandName, piArgs, piModelsJSONSetup, driverConfigured)
	command := e.buildPiExecutionCommand(workflowData, logFile, piCommand, firewallEnabled, modelConfigured, profile)
	env := e.buildPiExecutionEnv(workflowData, profile, backend, firewallEnabled, modelConfigured, piModelsJSONSetup != "")
	step := e.buildPiExecutionStep(workflowData, command, env)
	return []GitHubActionStep{step}
}

func (e *PiEngine) buildPiArgs(workflowData *WorkflowData) []string {
	piArgs := []string{"--print", "--mode", "json"}
	session := piSessionSettings(workflowData)
	if session.Enabled {
		piArgs = append(piArgs, "--session-dir", PiSessionDirectory)
		switch {
		case session.Resume != "":
			piArgs = append(piArgs, "--session", session.Resume)
		case session.Fork != "":
			piArgs = append(piArgs, "--fork", session.Fork)
		}
		if session.ID != "" {
			piArgs = append(piArgs, "--session-id", session.ID)
		}
	} else {
		piArgs = append(piArgs, "--no-session")
	}
	piArgs = append(piArgs, "--no-approve")
	if hasBashFullyDisabled(workflowData.Tools) {
		piArgs = append(piArgs, "--exclude-tools", "bash")
	}
	if workflowData.Tools["edit"] == false {
		piArgs = append(piArgs, "--exclude-tools", "edit,write")
	}
	if workflowData.EngineConfig != nil && workflowData.EngineConfig.Bare {
		piArgs = append(piArgs, "--no-context-files", "--no-skills", "--no-prompt-templates", "--no-extensions", "--no-themes")
	}
	if workflowData.EngineConfig != nil {
		piArgs = append(piArgs, filterPiArgs(workflowData.EngineConfig.Args)...)
	}
	return piArgs
}

func (e *PiEngine) buildPiModelsJSONSetup(workflowData *WorkflowData, profile universalLLMBackendProfile, backend UniversalLLMBackend, piArgs []string, firewallEnabled, modelConfigured, driverConfigured bool) (string, []string) {
	if !modelConfigured {
		return "", piArgs
	}
	modelID := extractPiModelID(workflowData.Model)
	gatewaySecretEnvVar := resolvePiGatewaySecretEnvVar(profile, backend)
	if firewallEnabled {
		if gatewaySecretEnvVar == "" {
			piLog.Printf("Pi: no gateway apiKey env resolved for backend=%s; defaulting to COPILOT_GITHUB_TOKEN", backend)
			gatewaySecretEnvVar = "COPILOT_GITHUB_TOKEN"
		}
		reflectProvider := piReflectProviderName(backend)
		// The compile-time gatewayPort is only a fallback: pi_models_json.cjs queries AWF's
		// /reflect endpoint at runtime (when AWF_REFLECT_ENABLED=1) to resolve the live
		// api-proxy port for reflectProvider, since AWF's actual port assignment is the
		// source of truth and can drift from the compiled-in value.
		contextWindowAssignment := ""
		if workflowData.EngineConfig != nil && workflowData.EngineConfig.ContextWindow > 0 {
			contextWindowAssignment = " GH_AW_PI_CONTEXT_WINDOW=" + shellEscapeArg(strconv.Itoa(workflowData.EngineConfig.ContextWindow))
		}
		setup := fmt.Sprintf(
			`export GH_AW_PI_MODEL_ID=%s GH_AW_PI_GATEWAY_SECRET_ENV=%s GH_AW_PI_GATEWAY_FALLBACK_PORT=%d GH_AW_LLM_PROVIDER=%s%s && ( %s "%s/pi_models_json.cjs" ) && `,
			shellEscapeArg(modelID), shellEscapeArg(gatewaySecretEnvVar), profile.gatewayPort, shellEscapeArg(reflectProvider),
			contextWindowAssignment,
			nodeRuntimeResolutionCommand, SetupActionDestinationShell,
		)
		if !driverConfigured {
			piArgs = append(piArgs, "--model", "aw-gateway/"+modelID)
		}
		piLog.Printf("Pi: using /reflect-resolved models.json gateway routing for model %q via aw-gateway (fallback port %d)", modelID, profile.gatewayPort)
		return setup, piArgs
	}
	if !driverConfigured {
		nativeProvider := piConfiguredProvider(workflowData)
		piArgs = append(piArgs, "--model", path.Join(nativeProvider, modelID))
		piLog.Printf("Pi: using native provider %q for model %q (no firewall)", nativeProvider, modelID)
	}
	return "", piArgs
}

func (e *PiEngine) buildPiCommand(workflowData *WorkflowData, commandName string, piArgs []string, piModelsJSONSetup string, driverConfigured bool) string {
	var piCommand string
	if driverConfigured {
		piCommand = buildPiDriverCommand(workflowData.EngineConfig.Driver)
		piLog.Printf("Pi: using driver mode with driver=%s", workflowData.EngineConfig.Driver)
	} else {
		piCommand = fmt.Sprintf(
			`cat /tmp/gh-aw/aw-prompts/user.txt | %s %s --append-system-prompt /tmp/gh-aw/aw-prompts/system.txt --extension "${RUNNER_TEMP}/gh-aw/actions/pi_provider.cjs" --extension "${RUNNER_TEMP}/gh-aw/actions/pi_steering_extension.cjs" --extension "${RUNNER_TEMP}/gh-aw/actions/pi_tool_policy.cjs" --extension builtin:mcp --extension builtin:codemode --extension builtin:tool-search 2>&1 | tee %s`,
			commandName, shellJoinArgs(piArgs), PiStreamingLogFile)
	}
	if piModelsJSONSetup != "" {
		piCommand = piModelsJSONSetup + piCommand
	}
	if !driverConfigured && piSessionSettings(workflowData).Export {
		piCommand += fmt.Sprintf(` && %s "%s/pi_session_export.cjs"`, nodeRuntimeResolutionCommand, SetupActionDestinationShell)
	}
	piCommand = fmt.Sprintf(`%s "%s/pi_runtime.cjs" && %s`, nodeRuntimeResolutionCommand, SetupActionDestinationShell, piCommand)
	return getWorkspaceCommandPrefixFor(workflowData.EngineConfig) + piCommand
}

func (e *PiEngine) buildPiExecutionCommand(workflowData *WorkflowData, logFile, piCommand string, firewallEnabled, modelConfigured bool, profile universalLLMBackendProfile) string {
	if firewallEnabled {
		piCommandWithPath := fmt.Sprintf("%s && %s", GetNpmBinPathSetup(), piCommand)
		if microVMCLIPath := GetMicroVMNpmCLIPathSetup(workflowData); microVMCLIPath != "" {
			piCommandWithPath = fmt.Sprintf("%s && %s", microVMCLIPath, piCommandWithPath)
		}
		if mcpCLIPath := GetMCPCLIPathSetup(workflowData); mcpCLIPath != "" {
			piCommandWithPath = fmt.Sprintf("%s && %s", mcpCLIPath, piCommandWithPath)
		}
		pathSetup := "touch " + AgentStepSummaryPath + "\n" +
			"GH_AW_NODE_BIN=$(command -v node 2>/dev/null || true)\n" +
			"export GH_AW_NODE_BIN"
		return BuildAWFCommand(AWFCommandConfig{
			EngineName:         "pi",
			EngineCommand:      buildShellHarnessCommand("pi", piCommandWithPath),
			LogFile:            logFile,
			WorkflowData:       workflowData,
			UsesTTY:            false,
			AllowedDomains:     e.piAllowedDomains(workflowData, modelConfigured),
			PathSetup:          pathSetup,
			ExcludeEnvVarNames: ComputeAWFExcludeEnvVarNames(workflowData, e.GetSupportedEnvVarKeys()),
		})
	}
	// Even without AWF, a Cloud Hypervisor runtime can be configured
	// (e.g. network.firewall: false paired with sandbox.agent.runtime: cloud-hypervisor),
	// so the staged CLI path must still be exported for the microVM to see `pi`.
	if microVMCLIPath := GetMicroVMNpmCLIPathSetup(workflowData); microVMCLIPath != "" {
		piCommand = fmt.Sprintf("%s && %s", microVMCLIPath, piCommand)
	}
	return fmt.Sprintf(`set -o pipefail
printf '%%s' "$(date +%%s%%3N)" > %s
touch %s
(umask 177 && touch %s)
%s 2>&1 | tee -a %s`, AgentCLIStartMsPath, AgentStepSummaryPath, logFile, buildShellHarnessCommand("pi", piCommand), logFile)
}

func (e *PiEngine) piAllowedDomains(workflowData *WorkflowData, modelConfigured bool) string {
	allowedDomains := workflowData.CachedAllowedDomainsStr
	if !workflowData.CachedAllowedDomainsComputed {
		model := ""
		if modelConfigured {
			model = workflowData.Model
		}
		allowedDomains = mustGetAllowedDomainsForEngineWithModel(constants.PiEngine, model, workflowData.NetworkPermissions, workflowData.Tools, workflowData.Runtimes)
	}
	if workflowData.EngineConfig != nil && workflowData.EngineConfig.APITarget != "" {
		allowedDomains = mergeAPITargetDomains(allowedDomains, workflowData.EngineConfig.APITarget)
	}
	return allowedDomains
}

func (e *PiEngine) buildPiExecutionEnv(workflowData *WorkflowData, profile universalLLMBackendProfile, backend UniversalLLMBackend, firewallEnabled, modelConfigured, hasModelsJSONSetup bool) map[string]string {
	env := map[string]string{
		"GH_AW_PROMPT":           constants.AwPromptsFile,
		"GH_AW_PI_USER_PROMPT":   constants.TmpGhAwDir + "/aw-prompts/user.txt",
		"GH_AW_PI_SYSTEM_PROMPT": constants.TmpGhAwDir + "/aw-prompts/system.txt",
		"GITHUB_AW":              "true",
		"GITHUB_STEP_SUMMARY":    AgentStepSummaryPath,
		"GITHUB_WORKSPACE":       "${{ github.workspace }}",
		"GH_AW_TIMEOUT_MINUTES":  resolveStepTimeoutValue(workflowData),
		"PI_OFFLINE":             "1",
		"PI_CODING_AGENT_DIR":    constants.TmpPiAgentDir,
		"RUNNER_TEMP":            "${{ runner.temp }}",
	}
	applyPlaywrightBrowserEnv(env, workflowData)
	injectWorkflowCallNetworkAllowedEnv(env, workflowData)
	if modelConfigured {
		env["GH_AW_PI_MODEL"] = workflowData.Model
		env["GH_AW_PI_NATIVE_PROVIDER"] = piConfiguredProvider(workflowData)
	}
	maps.Copy(env, profile.env)
	if backend == UniversalLLMBackendCopilot {
		delete(env, "OPENAI_API_KEY")
	}
	if hasModelsJSONSetup {
		env["PI_CODING_AGENT_DIR"] = constants.TmpPiAgentDir
		piLog.Printf("Pi: setting PI_CODING_AGENT_DIR for models.json gateway config")
	}
	env["GH_AW_PHASE"] = workflowRunPhase(workflowData)
	if IsRelease() {
		env["GH_AW_VERSION"] = GetVersion()
	} else {
		env["GH_AW_VERSION"] = "dev"
	}
	if firewallEnabled {
		maps.Copy(env, getGitIdentityEnvVars())
		env["AWF_REFLECT_ENABLED"] = "1"
	}
	applySafeOutputEnvToMap(env, workflowData)
	applyDefaultMaxAICreditsEnvToMap(env, workflowData)
	applyTraceContextEnvToMap(env)
	if workflowData.EngineConfig != nil && workflowData.EngineConfig.MaxTurns != "" {
		env["GH_AW_MAX_TURNS"] = workflowData.EngineConfig.MaxTurns
	} else {
		env["GH_AW_MAX_TURNS"] = compilerenv.BuildDefaultMaxTurnsExpression()
	}
	applyEngineCwdEnv(env, workflowData)
	e.applyPiConfigEnv(env, workflowData)
	if workflowData.EngineConfig != nil && len(workflowData.EngineConfig.Env) > 0 {
		maps.Copy(env, workflowData.EngineConfig.Env)
	}
	if agentConfig := getAgentConfig(workflowData); agentConfig != nil && len(agentConfig.Env) > 0 {
		maps.Copy(env, agentConfig.Env)
		piLog.Printf("Added %d custom env vars from agent config", len(agentConfig.Env))
	}
	applyPiToolPolicyEnv(env, workflowData)
	return env
}

func (e *PiEngine) buildPiExecutionStep(workflowData *WorkflowData, command string, env map[string]string) GitHubActionStep {
	stepLines := []string{
		"      - name: Execute Pi CLI",
		"        id: agentic_execution",
		"        timeout-minutes: " + resolveStepTimeoutValue(workflowData),
	}
	filteredEnv := FilterEnvForSecrets(env, e.GetRequiredSecretNames(workflowData))
	addCliProxyGHTokenToEnv(filteredEnv, workflowData)
	return GitHubActionStep(FormatStepWithCommandAndEnv(stepLines, wrapAgentExecutionCommand(command), filteredEnv))
}

// PiStreamingLogFile is the path where Pi CLI writes its streaming JSONL event log.
// All Pi tool calls, messages, and metrics are captured here for post-run analysis
// and step summary rendering.
const PiStreamingLogFile = "/tmp/gh-aw/pi-streaming.jsonl"

const PiSessionDirectory = constants.TmpGhAwAgentDir + "pi-sessions"

// filterPiArgs removes redundant Pi CLI flags that gh-aw should not pass through.
// Pi runs in yolo mode by default, so explicit --yolo flags are ignored while all
// other engine args are preserved in order.
func filterPiArgs(args []string) []string {
	filtered := make([]string, 0, len(args))
	for _, arg := range args {
		if arg == "--yolo" || strings.HasPrefix(arg, "--yolo=") {
			piLog.Printf("Pi: dropping redundant arg %q because Pi runs in yolo mode by default", arg)
			continue
		}
		filtered = append(filtered, arg)
	}

	return filtered
}

// buildPiDriverCommand builds the shell command to run a pi engine driver script.
//
// When driverName contains a path separator ('/'), it is treated as a workspace-relative
// custom driver: "${GITHUB_WORKSPACE}/<driverName>".  When it is a bare filename
// (no '/'), it is resolved from the setup-action directory: "${RUNNER_TEMP}/gh-aw/actions/<driverName>".
//
// The driver reads GH_AW_PROMPT and GH_AW_PI_MODEL from the environment and emits
// JSONL compatible with parse_pi_log.cjs to stdout; stderr and stdout are both
// captured by tee to PiStreamingLogFile.
func buildPiDriverCommand(driverName string) string {
	var driverPath string
	if strings.Contains(driverName, "/") {
		// Workspace-relative custom driver: validation ensures no shell metacharacters or
		// path traversal, so embedding directly in a double-quoted shell argument is safe.
		driverPath = `"${GITHUB_WORKSPACE}/` + driverName + `"`
	} else {
		driverPath = fmt.Sprintf(`"%s/%s"`, SetupActionDestinationShell, driverName)
	}

	return fmt.Sprintf(`%s %s 2>&1 | tee %s`, nodeRuntimeResolutionCommand, driverPath, PiStreamingLogFile)
}
