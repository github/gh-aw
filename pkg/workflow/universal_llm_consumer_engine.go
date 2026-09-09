package workflow

import (
	"errors"
	"fmt"
	"maps"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/workflow/compilerenv"
)

var universalLLMConsumerLog = logger.New("workflow:universal_llm_consumer_engine")

type UniversalLLMBackend string

const (
	UniversalLLMBackendCopilot   UniversalLLMBackend = "copilot"
	UniversalLLMBackendAnthropic UniversalLLMBackend = "anthropic"
	UniversalLLMBackendCodex     UniversalLLMBackend = "codex"
)

type UniversalLLMConsumerEngine struct {
	BaseEngine
}

type universalLLMBackendProfile struct {
	coreSecretNames []string
	env             map[string]string
	baseURLEnvName  string
	extraURLEnvName string // additional URL env var set to the same gateway URL (e.g. OPENAI_BASE_URL for copilot backend)
	gatewayPort     int
}

func resolveUniversalLLMBackendFromModel(model string) (UniversalLLMBackend, error) {
	universalLLMConsumerLog.Printf("Resolving LLM backend from model: %q", model)
	model = strings.TrimSpace(model)
	if model == "" {
		return "", errors.New("for universal consumer engines, engine.model is required and must use provider/model format (supported providers: copilot, anthropic, openai, codex)")
	}

	parts := strings.SplitN(model, "/", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return "", errors.New("for universal consumer engines, engine.model must use provider/model format (for example: copilot/gpt-5, anthropic/claude-sonnet-4, openai/gpt-4.1)")
	}

	switch strings.ToLower(strings.TrimSpace(parts[0])) {
	case "copilot":
		universalLLMConsumerLog.Printf("Resolved backend: copilot (model=%s)", parts[1])
		return UniversalLLMBackendCopilot, nil
	case "anthropic":
		universalLLMConsumerLog.Printf("Resolved backend: anthropic (model=%s)", parts[1])
		return UniversalLLMBackendAnthropic, nil
	case "openai", "codex":
		universalLLMConsumerLog.Printf("Resolved backend: codex/openai (model=%s)", parts[1])
		return UniversalLLMBackendCodex, nil
	default:
		return "", fmt.Errorf("unsupported provider %q in engine.model; supported providers: copilot, anthropic, openai, codex", parts[0])
	}
}

func getUniversalLLMBackendProfile(backend UniversalLLMBackend, useCopilotRequests bool) universalLLMBackendProfile {
	switch backend {
	case UniversalLLMBackendAnthropic:
		return universalLLMBackendProfile{
			coreSecretNames: []string{"ANTHROPIC_API_KEY"},
			env: map[string]string{
				"ANTHROPIC_API_KEY": "${{ secrets.ANTHROPIC_API_KEY }}",
			},
			baseURLEnvName: "ANTHROPIC_BASE_URL",
			gatewayPort:    constants.ClaudeLLMGatewayPort,
		}
	case UniversalLLMBackendCodex:
		return universalLLMBackendProfile{
			coreSecretNames: []string{"CODEX_API_KEY", "OPENAI_API_KEY"},
			env: map[string]string{
				"CODEX_API_KEY":  "${{ secrets.CODEX_API_KEY || secrets.OPENAI_API_KEY }}",
				"OPENAI_API_KEY": "${{ secrets.CODEX_API_KEY || secrets.OPENAI_API_KEY }}",
			},
			baseURLEnvName: "OPENAI_BASE_URL",
			gatewayPort:    constants.CodexLLMGatewayPort,
		}
	default:
		copilotToken := "${{ secrets.COPILOT_GITHUB_TOKEN }}"
		coreSecrets := []string{"COPILOT_GITHUB_TOKEN"}
		if useCopilotRequests {
			copilotToken = "${{ github.token }}"
			coreSecrets = []string{}
		}
		return universalLLMBackendProfile{
			coreSecretNames: coreSecrets,
			env: map[string]string{
				"COPILOT_GITHUB_TOKEN": copilotToken,
				"OPENAI_API_KEY":       copilotToken,
			},
			baseURLEnvName:  "GITHUB_COPILOT_BASE_URL",
			extraURLEnvName: "OPENAI_BASE_URL",
			gatewayPort:     constants.CopilotLLMGatewayPort,
		}
	}
}

func (e *UniversalLLMConsumerEngine) resolveBackend(workflowData *WorkflowData) UniversalLLMBackend {
	model := ""
	if workflowData != nil && workflowData.EngineConfig != nil {
		model = workflowData.Model
	}
	backend, err := resolveUniversalLLMBackendFromModel(model)
	if err != nil {
		universalLLMConsumerLog.Printf("Falling back to copilot backend while resolving model %q: %v", model, err)
		return UniversalLLMBackendCopilot
	}
	return backend
}

func (e *UniversalLLMConsumerEngine) GetUniversalRequiredSecretNames(workflowData *WorkflowData) []string {
	backend := e.resolveBackend(workflowData)
	universalLLMConsumerLog.Printf("Collecting required secret names for backend: %s", backend)
	profile := getUniversalLLMBackendProfile(backend, hasCopilotRequestsWritePermission(workflowData))
	secrets := append([]string{}, profile.coreSecretNames...)

	if workflowData != nil && workflowData.EngineConfig != nil && len(workflowData.EngineConfig.Env) > 0 {
		for key := range workflowData.EngineConfig.Env {
			if strings.HasSuffix(key, "_API_KEY") || strings.HasSuffix(key, "_KEY") {
				secrets = append(secrets, key)
			}
		}
	}

	if workflowData != nil {
		secrets = append(secrets, collectCommonMCPSecrets(workflowData)...)
	}

	parsedTools, tools := extractToolsConfig(workflowData)

	if hasGitHubTool(parsedTools) {
		secrets = append(secrets, "GITHUB_MCP_SERVER_TOKEN")
	}

	headerSecrets := collectHTTPMCPHeaderSecrets(tools)
	for varName := range headerSecrets {
		secrets = append(secrets, varName)
	}

	universalLLMConsumerLog.Printf("Resolved %d required secret names for backend %s", len(secrets), backend)
	return secrets
}

func extractToolsConfig(workflowData *WorkflowData) (*ToolsConfig, map[string]any) {
	if workflowData == nil {
		return nil, map[string]any{}
	}
	if workflowData.Tools == nil {
		return workflowData.ParsedTools, map[string]any{}
	}
	return workflowData.ParsedTools, workflowData.Tools
}

func (e *UniversalLLMConsumerEngine) GetUniversalSecretValidationStep(workflowData *WorkflowData, engineName, docsURL string) GitHubActionStep {
	backend := e.resolveBackend(workflowData)
	profile := getUniversalLLMBackendProfile(backend, hasCopilotRequestsWritePermission(workflowData))
	return BuildEngineSecretValidationStep(workflowData, EngineSecretValidationConfig{
		SecretNames: profile.coreSecretNames,
		EngineName:  engineName,
		DocsURL:     docsURL,
	})
}

func (e *UniversalLLMConsumerEngine) ApplyUniversalProviderEnv(env map[string]string, workflowData *WorkflowData, firewallEnabled bool) {
	backend := e.resolveBackend(workflowData)
	universalLLMConsumerLog.Printf("Applying provider env for backend=%s, firewallEnabled=%t", backend, firewallEnabled)
	profile := getUniversalLLMBackendProfile(backend, hasCopilotRequestsWritePermission(workflowData))
	maps.Copy(env, profile.env)
	switch backend {
	case UniversalLLMBackendAnthropic:
		env["GH_AW_LLM_PROVIDER"] = string(LLMProviderAnthropic)
	case UniversalLLMBackendCodex:
		env["GH_AW_LLM_PROVIDER"] = string(LLMProviderOpenAI)
	default:
		env["GH_AW_LLM_PROVIDER"] = string(LLMProviderGitHub)
	}
	if firewallEnabled {
		universalLLMConsumerLog.Printf("Setting %s to gateway port %d", profile.baseURLEnvName, profile.gatewayPort)
		env[profile.baseURLEnvName] = fmt.Sprintf("http://host.docker.internal:%d", profile.gatewayPort)
		if profile.extraURLEnvName != "" {
			universalLLMConsumerLog.Printf("Setting extra URL env %s to gateway port %d", profile.extraURLEnvName, profile.gatewayPort)
			env[profile.extraURLEnvName] = fmt.Sprintf("http://host.docker.internal:%d", profile.gatewayPort)
		}
	}
}

// resolveBackendWithAliases is like resolveUniversalLLMBackendFromModel but also recognises
// extra provider name prefixes before falling back to the standard lookup. extraAliases maps
// lowercased provider names (the prefix before the first "/") to the corresponding
// UniversalLLMBackend. This lets engines that expose their own provider naming (e.g. Pi's
// "github-copilot") reuse the shared backend resolution logic without special-casing.
func resolveBackendWithAliases(model string, extraAliases map[string]UniversalLLMBackend) (UniversalLLMBackend, error) {
	if len(extraAliases) > 0 {
		model = strings.TrimSpace(model)
		parts := strings.SplitN(model, "/", 2)
		if len(parts) == 2 {
			provider := strings.ToLower(strings.TrimSpace(parts[0]))
			if backend, ok := extraAliases[provider]; ok {
				universalLLMConsumerLog.Printf("Resolved backend via alias %q → %s", provider, backend)
				return backend, nil
			}
		}
	}
	return resolveUniversalLLMBackendFromModel(model)
}

// UniversalCLIEngineExecutionConfig holds the engine-specific parameters for
// BuildCLIEngineExecutionSteps. Engines that share the same execution pattern
// (a "run" subcommand, a JSON permissions config file, standard AWF env vars) can
// populate this struct to reuse the common execution step logic rather than
// duplicating it.
type UniversalCLIEngineExecutionConfig struct {
	// EngineConstant is the engine name used for firewall allowed-domain resolution.
	EngineConstant constants.EngineName
	// DefaultCommandName is the CLI binary name used when engine.command is not set
	// (e.g. a behavior-defined CLI engine).
	DefaultCommandName string
	// ExtraCLIArgs are additional flags passed to the CLI run subcommand before the
	// prompt argument.
	ExtraCLIArgs []string
	// MCPConfigFile is the workspace-relative path of the permissions/MCP config file.
	// It is used to populate GH_AW_MCP_CONFIG when MCP servers are configured.
	MCPConfigFile string
	// StepName is the GitHub Actions step name (e.g. "Execute My CLI").
	StepName string
	// ConfigStep is the pre-built config-writing step that precedes the execution step.
	// Typically writes a JSON file that grants all permissions so the agent never hangs
	// on an interactive prompt in CI.
	ConfigStep GitHubActionStep
	// ModelEnvVarName is the native environment variable used by the CLI for model
	// selection (e.g. "MY_CLI_MODEL"). When empty, model selection
	// via env var is skipped.
	ModelEnvVarName string
	// WriteTimestamp controls whether the non-firewall fallback command writes the
	// agent start timestamp to AgentCLIStartMsPath before running the engine.
	WriteTimestamp bool
}

// BuildCLIEngineExecutionSteps generates the GitHub Actions execution steps for a
// universal CLI engine. It handles firewall-aware command
// construction, common AWF environment variable injection, and step formatting.
// Engines call this from their GetExecutionSteps implementation, supplying engine-
// specific parameters via cfg.
//
//nolint:largefunc // Keeps universal engine setup in generated execution order.
func (e *UniversalLLMConsumerEngine) BuildCLIEngineExecutionSteps(
	workflowData *WorkflowData,
	logFile string,
	cfg UniversalCLIEngineExecutionConfig,
) []GitHubActionStep {
	universalLLMConsumerLog.Printf("Generating execution steps for %s engine: workflow=%s, firewall=%v",
		cfg.DefaultCommandName, workflowData.Name, isFirewallEnabled(workflowData))

	var steps []GitHubActionStep

	// Prepend the config step (writes permissions JSON to workspace).
	if len(cfg.ConfigStep) > 0 {
		steps = append(steps, cfg.ConfigStep)
	}

	modelConfigured := workflowData.Model != ""

	// Build CLI command: <binary> run <extra-args> "<prompt-file>".
	cliArgs := append([]string{}, cfg.ExtraCLIArgs...)
	promptArg := fmt.Sprintf("\"$(cat %s)\"", constants.AwPromptsFile)
	commandName := cfg.DefaultCommandName
	if workflowData.EngineConfig != nil && workflowData.EngineConfig.Command != "" {
		commandName = workflowData.EngineConfig.Command
	}
	engineCommand := fmt.Sprintf("%s run %s %s", commandName, shellJoinArgs(cliArgs), promptArg)
	engineCommand = getWorkspaceCommandPrefixFor(workflowData.EngineConfig) + engineCommand

	firewallEnabled := isFirewallEnabled(workflowData)
	var command string
	if firewallEnabled {
		model := ""
		if modelConfigured {
			model = workflowData.Model
		}
		// Get allowed domains: prefer the pre-warmed cache on WorkflowData to avoid
		// re-running the expensive map+sort operation.
		var allowedDomains string
		if workflowData.CachedAllowedDomainsComputed {
			allowedDomains = workflowData.CachedAllowedDomainsStr
		} else {
			// The model was validated before reaching here, so a malformed model
			// (e.g. leading slash) must never occur. Panic is the correct response
			// to an internal invariant violation.
			allowedDomains = mustGetAllowedDomainsForEngineWithModel(
				cfg.EngineConstant,
				model,
				workflowData.NetworkPermissions,
				workflowData.Tools,
				workflowData.Runtimes,
			)
		}

		npmPathSetup := GetNpmBinPathSetup()
		// Propagate no_proxy inside the AWF container.  --env-all forwards NO_PROXY
		// from the YAML env block, but Bun (and other runtimes) also check the
		// lowercase variant, so we export it explicitly from the uppercase value.
		engineCommandWithPath := fmt.Sprintf("export no_proxy=\"${NO_PROXY:-}\" && %s && %s", npmPathSetup, engineCommand)
		if mcpCLIPath := GetMCPCLIPathSetup(workflowData); mcpCLIPath != "" {
			engineCommandWithPath = fmt.Sprintf("%s && %s", mcpCLIPath, engineCommandWithPath)
		}

		command = BuildAWFCommand(AWFCommandConfig{
			EngineName:     cfg.DefaultCommandName,
			EngineCommand:  engineCommandWithPath,
			LogFile:        logFile,
			WorkflowData:   workflowData,
			UsesTTY:        false,
			AllowedDomains: allowedDomains,
		})
	} else if cfg.WriteTimestamp {
		command = fmt.Sprintf("set -o pipefail\nexport no_proxy=\"${NO_PROXY:-}\"\nprintf '%%s' \"$(date +%%s%%3N)\" > %s\n%s 2>&1 | tee -a %s",
			AgentCLIStartMsPath, engineCommand, logFile)
	} else {
		command = fmt.Sprintf("set -o pipefail\nexport no_proxy=\"${NO_PROXY:-}\"\n%s 2>&1 | tee -a %s", engineCommand, logFile)
	}

	env := map[string]string{
		"GH_AW_PROMPT":     constants.AwPromptsFile,
		"GITHUB_WORKSPACE": "${{ github.workspace }}",
		"RUNNER_TEMP":      "${{ runner.temp }}",
		// Set NO_PROXY so that the AWF agent's HTTP client skips the squid proxy
		// for local endpoints. The lowercase no_proxy variant is exported inside
		// the run script rather than as a YAML env key because GitHub's workflow
		// parser rejects case-insensitive duplicate env keys (NO_PROXY/no_proxy),
		// which causes workflow_dispatch to fail with "failed to parse workflow".
		"NO_PROXY": constants.AWFNoProxyHosts,
	}
	applyPlaywrightBrowserEnv(env, workflowData)
	injectWorkflowCallNetworkAllowedEnv(env, workflowData)
	e.ApplyUniversalProviderEnv(env, workflowData, firewallEnabled)

	if HasMCPServers(workflowData) {
		env["GH_AW_MCP_CONFIG"] = "${{ github.workspace }}/" + cfg.MCPConfigFile
	}

	applySafeOutputEnvToMap(env, workflowData)
	applyDefaultMaxAICreditsEnvToMap(env, workflowData)

	// Propagate W3C trace context so engine spans nest under the gh-aw.agent.setup span.
	applyTraceContextEnvToMap(env)

	if workflowData.EngineConfig != nil && workflowData.EngineConfig.MaxTurns != "" {
		env["GH_AW_MAX_TURNS"] = workflowData.EngineConfig.MaxTurns
	} else {
		env["GH_AW_MAX_TURNS"] = compilerenv.BuildDefaultMaxTurnsExpression()
	}

	// Model env var (only when explicitly configured and the engine supports it).
	if modelConfigured && cfg.ModelEnvVarName != "" {
		universalLLMConsumerLog.Printf("Setting %s env var for model: %s", cfg.ModelEnvVarName, workflowData.Model)
		env[cfg.ModelEnvVarName] = workflowData.Model
	}

	// Custom env from engine config (allows provider key override).
	applyEngineCwdEnv(env, workflowData)
	if workflowData.EngineConfig != nil && len(workflowData.EngineConfig.Env) > 0 {
		maps.Copy(env, workflowData.EngineConfig.Env)
	}

	// Agent config env.
	agentConfig := getAgentConfig(workflowData)
	if agentConfig != nil && len(agentConfig.Env) > 0 {
		maps.Copy(env, agentConfig.Env)
	}

	stepLines := []string{
		"      - name: " + cfg.StepName,
		"        id: agentic_execution",
	}
	allowedSecrets := e.GetUniversalRequiredSecretNames(workflowData)
	filteredEnv := FilterEnvForSecrets(env, allowedSecrets)
	stepLines = FormatStepWithCommandAndEnv(stepLines, wrapAgentExecutionCommand(command), filteredEnv)

	steps = append(steps, GitHubActionStep(stepLines))
	return steps
}
