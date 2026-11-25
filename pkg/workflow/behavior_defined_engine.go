package workflow

import (
	"errors"
	"fmt"
	"slices"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/setutil"
)

const (
	behaviorSecretStrategyUniversalLLMConsumer  = "universal-llm-consumer"
	behaviorSecretStrategyGeminiAPIKey          = "gemini-api-key"
	behaviorProviderEnvModeUniversalLLMConsumer = "universal-llm-consumer"
	behaviorConfigMergeJSON                     = "json-merge"
)

var behaviorDefinedEngineLog = logger.New("workflow:behavior_defined_engine")

// BehaviorDefinedEngine is a declarative CodingAgentEngine built from an engine
// definition's behaviors block.
type BehaviorDefinedEngine struct {
	UniversalLLMConsumerEngine
	definition *EngineDefinition
}

var _ CodingAgentEngine = (*BehaviorDefinedEngine)(nil)

const protectedSessionCaptureID = "${{ github.run_id }}-${{ github.run_attempt }}-${{ github.job }}-${{ strategy.job-index || 0 }}"
const protectedSessionCaptureLogPath = "${{ runner.temp }}/gh-aw-protected-capture-" + protectedSessionCaptureID

func NewBehaviorDefinedEngine(def *EngineDefinition) (*BehaviorDefinedEngine, error) {
	if def == nil {
		return nil, errors.New("engine definition is required")
	}
	if def.Behaviors == nil {
		return nil, fmt.Errorf("engine definition %q is missing behaviors", def.ID)
	}
	if err := validateBehaviorOutputFiles(def.Behaviors.OutputFiles); err != nil {
		return nil, fmt.Errorf("engine definition %q behaviors.output-files: %w", def.ID, err)
	}
	if err := validatePipInstallation(def); err != nil {
		return nil, fmt.Errorf("engine definition %q behaviors.installation: %w", def.ID, err)
	}
	if def.Behaviors.Execution != nil && def.Behaviors.Execution.ProtectedSessionCapture && def.Behaviors.LogParser == "" {
		return nil, fmt.Errorf("engine definition %q enables protected-session-capture without a log-parser", def.ID)
	}
	capabilities := def.Behaviors.Capabilities.ToRuntimeCapabilities()
	capabilities.MCP = true
	// Declaring a plugins behavior block is what enables Agent Plugins for the engine:
	// without it the compiler has no way to make plugins visible to the CLI.
	capabilities.Plugins = def.Behaviors.Plugins != nil
	if plugins := def.Behaviors.Plugins; plugins != nil {
		if plugins.Directory == "" && len(plugins.InstallArgs) == 0 {
			return nil, fmt.Errorf("engine definition %q declares behaviors.plugins without 'directory' or 'install-args'", def.ID)
		}
		if plugins.Directory != "" {
			if _, ok := resolvePluginDirectory(plugins.Directory); !ok {
				return nil, fmt.Errorf("engine definition %q declares an unsupported behaviors.plugins.directory %q; use a workspace-relative or '~/' path without '..' segments", def.ID, plugins.Directory)
			}
		}
	}
	if def.MCP != nil {
		capabilities.MCP = *def.MCP
	}
	if capabilities.WebFetchFallback && (!capabilities.MCP || def.Behaviors.MCP == nil || def.Behaviors.MCP.ConfigAdapter == "") {
		return nil, fmt.Errorf("engine definition %q enables web-fetch-fallback without MCP support and a config-adapter", def.ID)
	}

	engine := &BehaviorDefinedEngine{
		id:               def.ID,
		displayName:      def.DisplayName,
		description:      def.Description,
		experimental:     def.Experimental,
		ghSkillAgentName: def.GHSkillAgentName,
		capabilities:     capabilities,
		definition:       def,
	}
	if engine.protectedSessionCaptureEnabled() && engine.buildWrappedLogParserScript() == "" {
		return nil, fmt.Errorf("engine definition %q enables protected-session-capture without a usable log-parser", def.ID)
	}
	return engine, nil
}

func (e *BehaviorDefinedEngine) behavior() *EngineBehaviorDefinition {
	if e == nil || e.definition == nil {
		return nil
	}
	return e.definition.Behaviors
}

func (e *BehaviorDefinedEngine) protectedSessionCaptureEnabled() bool {
	behavior := e.behavior()
	return behavior != nil && behavior.Execution != nil && behavior.Execution.ProtectedSessionCapture
}

func (e *BehaviorDefinedEngine) GetLogFileForParsing() string {
	if e.protectedSessionCaptureEnabled() {
		return protectedSessionCaptureLogPath
	}
	return e.BaseEngine.GetLogFileForParsing()
}

func (e *BehaviorDefinedEngine) usesUniversalLLMConsumer() bool {
	behavior := e.behavior()
	return behavior != nil && behavior.SecretStrategy == behaviorSecretStrategyUniversalLLMConsumer
}

func (e *BehaviorDefinedEngine) GetModelEnvVarName() string {
	behavior := e.behavior()
	if behavior == nil || behavior.Execution == nil {
		return ""
	}
	return behavior.Execution.ModelEnvVarName
}

func (e *BehaviorDefinedEngine) GetRequiredSecretNames(workflowData *WorkflowData) []string {
	var backend UniversalLLMBackend
	if e.usesUniversalLLMConsumer() {
		backend = e.resolveBackend(workflowData)
	}
	return e.getRequiredSecretNamesForBackend(workflowData, backend)
}

func (e *BehaviorDefinedEngine) getRequiredSecretNamesForBackend(workflowData *WorkflowData, backend UniversalLLMBackend) []string {
	if e.usesUniversalLLMConsumer() {
		return e.getUniversalRequiredSecretNamesForBackend(workflowData, backend)
	}

	seen := make(map[string]struct{})
	var secrets []string
	addSecret := func(secret string) {
		if secret == "" || setutil.Contains(seen, secret) {
			return
		}
		seen[secret] = struct{}{}
		secrets = append(secrets, secret)
	}
	for _, binding := range e.definition.Auth {
		addSecret(binding.Secret)
	}
	for _, secret := range collectCommonMCPSecrets(workflowData) {
		addSecret(secret)
	}
	parsedTools, tools := extractToolsConfig(workflowData)
	if hasGitHubTool(parsedTools) {
		addSecret("GITHUB_MCP_SERVER_TOKEN")
	}
	for varName := range collectHTTPMCPHeaderSecrets(tools) {
		addSecret(varName)
	}
	return secrets
}

func (e *BehaviorDefinedEngine) GetSupportedEnvVarKeys() []string {
	behavior := e.behavior()
	if behavior == nil {
		return nil
	}
	if len(behavior.SupportedEnvVarKeys) > 0 {
		return behavior.SupportedEnvVarKeys
	}
	keys := make([]string, 0, len(e.definition.Auth))
	for _, binding := range e.definition.Auth {
		if binding.Secret != "" {
			keys = append(keys, binding.Secret)
		}
	}
	slices.Sort(keys)
	return slices.Compact(keys)
}

func (e *BehaviorDefinedEngine) GetSecretValidationStep(workflowData *WorkflowData) GitHubActionStep {
	behavior := e.behavior()
	if behavior == nil {
		return GitHubActionStep{}
	}
	if e.usesUniversalLLMConsumer() {
		// Hash-locked Python installations replace pre-agent-steps and keep
		// their prior (absent) universal secret validation.
		if behavior.Installation == nil || e.pipInstallation() != nil {
			return GitHubActionStep{}
		}
		return e.GetUniversalSecretValidationStep(
			workflowData,
			e.definition.DisplayName,
			behavior.Installation.DocumentationURL,
		)
	}
	secrets := make([]string, 0, len(e.definition.Auth))
	seen := make(map[string]struct{}, len(e.definition.Auth))
	for _, binding := range e.definition.Auth {
		if binding.Secret == "" || setutil.Contains(seen, binding.Secret) {
			continue
		}
		seen[binding.Secret] = struct{}{}
		secrets = append(secrets, binding.Secret)
	}
	documentationURL := ""
	if behavior.Installation != nil {
		documentationURL = behavior.Installation.DocumentationURL
	}
	return BuildEngineSecretValidationStep(workflowData, EngineSecretValidationConfig{
		SecretNames: secrets,
		EngineName:  e.definition.DisplayName,
		DocsURL:     documentationURL,
	})
}

//nolint:largefunc // Existing installation steps assembly is kept in generated order.
func (e *BehaviorDefinedEngine) GetInstallationSteps(workflowData *WorkflowData) []GitHubActionStep {
	behavior := e.behavior()
	if behavior == nil {
		return nil
	}
	requiresNode := behavior.HarnessScript != "" || isSkipIfDecisionEnabled(workflowData)
	if workflowData != nil && workflowData.EngineConfig != nil && workflowData.EngineConfig.Command != "" {
		var steps []GitHubActionStep
		if requiresNode {
			steps = append(steps, generateNodeJsSetupStep(workflowData))
		}
		return buildNpmEngineInstallStepsWithAWF(steps, workflowData, false)
	}

	// Behavior-defined engines that execute via a harness script (e.g. Goose) run the
	// harness through Node.js, so Node.js (and, when the firewall is enabled, the AWF
	// binary) must always be installed even when no package-manager based installation
	// is declared for the engine's CLI itself.
	if behavior.Installation == nil {
		if !requiresNode {
			// Engines that install their CLI through `pre-agent-steps`
			// declare no installation block at all, but the agent still runs inside the
			// firewall sandbox, so the AWF binary must be installed.
			return BuildNpmEngineInstallStepsWithAWF(nil, workflowData)
		}
		return BuildNpmEngineInstallStepsWithAWF([]GitHubActionStep{generateNodeJsSetupStep(workflowData)}, workflowData)
	}

	install := behavior.Installation
	if e.pipInstallation() != nil {
		var steps []GitHubActionStep
		if requiresNode {
			steps = append(steps, generateNodeJsSetupStep(workflowData))
		}
		steps = append(steps, e.buildPipInstallationSteps(install, workflowData)...)
		return BuildNpmEngineInstallStepsWithAWF(steps, workflowData)
	}
	if install.PackageManager != "npm" {
		// Non-npm installations are performed by the engine's own steps, but the AWF
		// binary is still required to run the agent inside the firewall sandbox.
		var steps []GitHubActionStep
		if isSkipIfDecisionEnabled(workflowData) {
			steps = append(steps, generateNodeJsSetupStep(workflowData))
		}
		return BuildNpmEngineInstallStepsWithAWF(steps, workflowData)
	}
	version := install.Version
	if workflowData != nil && workflowData.EngineConfig != nil && workflowData.EngineConfig.Version != "" {
		version = workflowData.EngineConfig.Version
	}

	cooldownDays := resolveEngineCooldownDays(workflowData)
	npmSteps := GenerateNpmInstallSteps(
		install.PackageName,
		version,
		install.StepName,
		install.BinaryName,
		NPMInstallOptions{
			IncludeNodeSetup:  install.IncludeNodeSetup,
			RunInstallScripts: install.PostInstallScripts,
			CooldownDays:      &cooldownDays,
		},
	)
	// MicroVMs cannot rely on the global npm installation. Stage the CLI in
	// the runtime tree mounted by Cloud Hypervisor or exported through NVX.
	binaryName := install.BinaryName
	if binaryName == "" && behavior.Execution != nil {
		binaryName = behavior.Execution.CommandName
	}
	if binaryName != "" && (isCloudHypervisorRuntime(workflowData) || isNVXRuntime(workflowData)) {
		npmSteps = append(npmSteps, GenerateMicroVMNpmCLIInstallStep(
			install.PackageName,
			version,
			install.StepName+" in microVM path",
			binaryName,
			install.PostInstallScripts,
			true,
			cooldownDays,
		))
	}
	if install.VerifyCommand != "" {
		npmSteps = append(npmSteps, GitHubActionStep{
			"      - name: " + install.VerifyStepName,
			"        run: " + install.VerifyCommand,
		})
	}
	return BuildNpmEngineInstallStepsWithAWF(npmSteps, workflowData)
}

// GetPluginInstallationSteps checks out pinned Agent Plugins and makes them available to
// the engine, either by staging them in the engine's plugin folder or by running the
// engine CLI's plugin installation command, as declared in the plugins behavior.
func (e *BehaviorDefinedEngine) GetPluginInstallationSteps(workflowData *WorkflowData) []GitHubActionStep {
	behavior := e.behavior()
	if behavior == nil || behavior.Plugins == nil {
		return nil
	}

	commandName := behavior.Plugins.CommandName
	if commandName == "" && behavior.Execution != nil {
		commandName = behavior.Execution.CommandName
	}
	if workflowData != nil && workflowData.EngineConfig != nil && workflowData.EngineConfig.Command != "" {
		commandName = workflowData.EngineConfig.Command
	}

	return generatePluginInstallationSteps(workflowData, pluginInstallSpec{
		Command:     commandName,
		InstallArgs: behavior.Plugins.InstallArgs,
		Directory:   behavior.Plugins.Directory,
	})
}

func (e *BehaviorDefinedEngine) GetAgentManifestFiles() []string {
	behavior := e.behavior()
	if behavior == nil || behavior.Manifest == nil {
		return nil
	}
	return behavior.Manifest.Files
}

func (e *BehaviorDefinedEngine) GetAgentManifestPathPrefixes() []string {
	behavior := e.behavior()
	if behavior == nil || behavior.Manifest == nil {
		return nil
	}
	return behavior.Manifest.PathPrefixes
}
