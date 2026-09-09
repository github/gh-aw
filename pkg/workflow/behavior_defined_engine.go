package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"path"
	"slices"
	"strings"
	"sync"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/setutil"
	"github.com/goccy/go-yaml"
)

const (
	behaviorSecretStrategyUniversalLLMConsumer  = "universal-llm-consumer"
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

func NewBehaviorDefinedEngine(def *EngineDefinition) (*BehaviorDefinedEngine, error) {
	if def == nil {
		return nil, errors.New("engine definition is required")
	}
	if def.Behaviors == nil {
		return nil, fmt.Errorf("engine definition %q is missing behaviors", def.ID)
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

	engine := &BehaviorDefinedEngine{
		UniversalLLMConsumerEngine: UniversalLLMConsumerEngine{
			BaseEngine: BaseEngine{
				id:               def.ID,
				displayName:      def.DisplayName,
				description:      def.Description,
				experimental:     def.Experimental,
				ghSkillAgentName: def.GHSkillAgentName,
				capabilities:     capabilities,
			},
		},
		definition: def,
	}
	return engine, nil
}

func (e *BehaviorDefinedEngine) behavior() *EngineBehaviorDefinition {
	if e == nil || e.definition == nil {
		return nil
	}
	return e.definition.Behaviors
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
	if e.usesUniversalLLMConsumer() {
		return e.GetUniversalRequiredSecretNames(workflowData)
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
		if behavior.Installation == nil {
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
	if workflowData != nil && workflowData.EngineConfig != nil && workflowData.EngineConfig.Command != "" {
		var steps []GitHubActionStep
		if behavior.HarnessScript != "" {
			steps = append(steps, GenerateNodeJsSetupStep())
		}
		return buildNpmEngineInstallStepsWithAWF(steps, workflowData, false)
	}

	// Behavior-defined engines that execute via a harness script (e.g. Goose) run the
	// harness through Node.js, so Node.js (and, when the firewall is enabled, the AWF
	// binary) must always be installed even when no package-manager based installation
	// is declared for the engine's CLI itself.
	if behavior.Installation == nil {
		if behavior.HarnessScript == "" {
			// Engines that install their CLI through `pre-agent-steps`
			// declare no installation block at all, but the agent still runs inside the
			// firewall sandbox, so the AWF binary must be installed.
			return BuildNpmEngineInstallStepsWithAWF(nil, workflowData)
		}
		return BuildNpmEngineInstallStepsWithAWF([]GitHubActionStep{GenerateNodeJsSetupStep()}, workflowData)
	}

	install := behavior.Installation
	if install.PackageManager != "npm" {
		// Non-npm installations are performed by the engine's own steps, but the AWF
		// binary is still required to run the agent inside the firewall sandbox.
		return BuildNpmEngineInstallStepsWithAWF(nil, workflowData)
	}
	version := install.Version
	if workflowData != nil && workflowData.EngineConfig != nil && workflowData.EngineConfig.Version != "" {
		version = workflowData.EngineConfig.Version
	}

	npmSteps := GenerateNpmInstallSteps(
		install.PackageName,
		version,
		install.StepName,
		install.BinaryName,
		NPMInstallOptions{
			IncludeNodeSetup:  install.IncludeNodeSetup,
			RunInstallScripts: install.PostInstallScripts,
			CooldownEnabled:   install.Cooldown,
		},
	)
	// microVM runtimes (docker-sbx/cloud-hypervisor) do not mount the runner tool cache,
	// so a global npm install is invisible inside the sandbox. Stage a second copy of the
	// CLI under ${RUNNER_TEMP}/gh-aw/engine-cli, which is mounted into the sandbox, exactly
	// as the Claude and Codex engines do.
	binaryName := install.BinaryName
	if binaryName == "" && behavior.Execution != nil {
		binaryName = behavior.Execution.CommandName
	}
	if binaryName != "" && (isDockerSbxRuntime(workflowData) || isCloudHypervisorRuntime(workflowData)) {
		npmSteps = append(npmSteps, GenerateDockerSbxNpmCLIInstallStep(
			install.PackageName,
			version,
			install.StepName+" in docker-sbx path",
			binaryName,
			install.PostInstallScripts,
			install.Cooldown,
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

func (e *BehaviorDefinedEngine) RenderMCPConfig(sb *strings.Builder, tools map[string]any, mcpTools []string, workflowData *WorkflowData) error {
	// The rendered config is piped to start_mcp_gateway.cjs, which is what actually
	// launches the MCP gateway container. It must therefore be emitted even when the
	// engine declares no MCP config path (e.g. engines with `mcp: false` that consume
	// MCP-backed tools through cli-proxy): skipping it leaves the gateway container
	// unstarted while AWF still attempts to attach it to the internal network.
	configPath := constants.ShellMcpServersJsonPath
	if behavior := e.behavior(); behavior != nil && behavior.MCP != nil && behavior.MCP.ConfigPath != "" {
		configPath = behavior.MCP.ConfigPath
	}
	return renderDefaultJSONMCPConfig(sb, tools, mcpTools, workflowData, configPath)
}

// harnessScriptHeredocDelimiter is the shell heredoc delimiter used when writing
// the harness script to disk. It is intentionally long and project-specific so that
// it is extremely unlikely to appear at the start of a line in any JavaScript harness.
const harnessScriptHeredocDelimiter = "GHAW_HARNESS_SCRIPT_3c7b9f1a_EOF"

// mcpConfigAdapterHeredocDelimiter is the shell heredoc delimiter used when writing
// the MCP config-adapter script to disk. It is intentionally long and project-specific
// so that it is extremely unlikely to appear at the start of a line in any JavaScript.
const mcpConfigAdapterHeredocDelimiter = "GHAW_MCP_CONFIG_ADAPTER_SCRIPT_7e1a4d2c_EOF"

// harnessScriptFilename returns the filename (not path) for the engine's harness script.
func (e *BehaviorDefinedEngine) harnessScriptFilename() string {
	return e.GetID() + "_harness.cjs"
}

// mcpConfigAdapterFilename returns the filename (not path) for the engine's MCP
// config-adapter script.
func (e *BehaviorDefinedEngine) mcpConfigAdapterFilename() string {
	return e.GetID() + "_mcp_config_adapter.cjs"
}

// buildScriptWriteStep generates a GitHub Actions step that writes script content to
// ${RUNNER_TEMP}/gh-aw/actions/<filename> via a bash heredoc using delimiter. Returns
// nil and logs a warning if script contains the delimiter, which would break the
// generated shell command.
func (e *BehaviorDefinedEngine) buildScriptWriteStep(stepName, filename, script, delimiter string) GitHubActionStep {
	if script == "" {
		return nil
	}
	// Safety check: if the script contains the heredoc delimiter at the start
	// of any line, the heredoc would be terminated prematurely. Detect this at
	// compile time and log a clear error rather than generating a broken step.
	if strings.Contains(script, "\n"+delimiter) || strings.HasPrefix(script, delimiter) {
		behaviorDefinedEngineLog.Printf(
			"WARNING: engine %q script %q contains heredoc delimiter %q; write step skipped",
			e.GetID(), filename, delimiter,
		)
		return nil
	}
	command := fmt.Sprintf(
		"mkdir -p \"%[1]s\"\ncat <<'%[4]s' > \"%[1]s/%[2]s\"\n%[3]s\n%[4]s\nchmod 755 \"%[1]s/%[2]s\"", //nolint:generatedyamlheredoc // Legacy engine script rendering remains to be migrated.
		SetupActionDestinationShell,
		filename,
		script,
		delimiter,
	)
	stepLines := []string{"      - name: " + stepName}
	stepLines = FormatStepWithCommandAndEnv(stepLines, command, nil)
	return GitHubActionStep(stepLines)
}

// buildHarnessWriteStep generates a GitHub Actions step that writes the behavior-defined
// engine's harness-script content to ${RUNNER_TEMP}/gh-aw/actions/<engine-id>_harness.cjs
// so it can be executed as a Node.js harness during the engine execution step.
// Returns nil and logs a warning if the harness script contains the heredoc delimiter,
// which would break the generated shell command.
func (e *BehaviorDefinedEngine) buildHarnessWriteStep() GitHubActionStep {
	behavior := e.behavior()
	if behavior == nil || behavior.HarnessScript == "" {
		return nil
	}
	return e.buildScriptWriteStep(
		"Write "+e.GetDisplayName()+" harness script",
		e.harnessScriptFilename(),
		behavior.HarnessScript,
		harnessScriptHeredocDelimiter,
	)
}

// GetMCPConfigAdapterWriteStep generates a GitHub Actions step that writes the
// behavior-defined engine's mcp.config-adapter script content to
// ${RUNNER_TEMP}/gh-aw/actions/<engine-id>_mcp_config_adapter.cjs so that
// start_mcp_gateway.cjs can execute it in place of a built-in per-engine converter.
// Returns nil (satisfying the MCPConfigAdapterProvider interface as a no-op) if the
// engine's behaviors do not declare a config-adapter script.
func (e *BehaviorDefinedEngine) GetMCPConfigAdapterWriteStep() GitHubActionStep {
	behavior := e.behavior()
	if behavior == nil || behavior.MCP == nil || behavior.MCP.ConfigAdapter == "" {
		return nil
	}
	return e.buildScriptWriteStep(
		"Write "+e.GetDisplayName()+" MCP config adapter script",
		e.mcpConfigAdapterFilename(),
		behavior.MCP.ConfigAdapter,
		mcpConfigAdapterHeredocDelimiter,
	)
}

// GetMCPConfigAdapterFilename returns the filename (not path) of the engine's MCP
// config-adapter script, or an empty string if no config-adapter is declared.
func (e *BehaviorDefinedEngine) GetMCPConfigAdapterFilename() string {
	behavior := e.behavior()
	if behavior == nil || behavior.MCP == nil || behavior.MCP.ConfigAdapter == "" {
		return ""
	}
	return e.mcpConfigAdapterFilename()
}

// logParserHeredocDelimiter is the shell heredoc delimiter used when writing
// the log-parser script to disk.
const logParserHeredocDelimiter = "GHAW_LOG_PARSER_SCRIPT_5d2e8b3f_EOF"

// logParserScriptFilename returns the filename (not path) for the engine's log-parser script.
func (e *BehaviorDefinedEngine) logParserScriptFilename() string {
	return e.GetID() + "_log_parser"
}

// buildWrappedLogParserScript wraps the user-supplied log-parser source with the
// createEngineLogParser bootstrap and returns the complete script content.
// Returns "" if no log-parser is defined or if the wrapped script contains the
// heredoc delimiter (which would break the generated shell heredoc).
func (e *BehaviorDefinedEngine) buildWrappedLogParserScript() string {
	behavior := e.behavior()
	if behavior == nil || behavior.LogParser == "" {
		return ""
	}
	// Wrap the user-provided parse function with the createEngineLogParser
	// bootstrap so the script conforms to the contract expected by
	// log_parser_bootstrap.cjs (runLogParser). The author only needs to define
	// a parseLog(logContent) function; the wrapper handles file I/O and exports.
	wrapped := fmt.Sprintf(`// @ts-check
// Auto-generated log parser for behavior-defined engine %q.
const { createEngineLogParser } = require("./log_parser_shared.cjs");

// --- begin engine-provided parse function ---
%s
// --- end engine-provided parse function ---

const main = createEngineLogParser({
  parserName: %q,
  parseFunction: parseLog,
  supportsDirectories: false,
});

if (typeof module !== "undefined" && module.exports) {
  module.exports = { main, parseLog };
}`,
		e.GetDisplayName(),
		behavior.LogParser,
		e.GetDisplayName(),
	)
	// Safety check: if the wrapped script contains the heredoc delimiter at the
	// start of any line, the heredoc would be terminated prematurely. Detect
	// this at compile time so GetLogParserScriptId() and buildLogParserWriteStep()
	// are always consistent.
	if strings.Contains(wrapped, "\n"+logParserHeredocDelimiter) || strings.HasPrefix(wrapped, logParserHeredocDelimiter) {
		behaviorDefinedEngineLog.Printf(
			"WARNING: engine %q log-parser script contains heredoc delimiter %q; log-parser step suppressed",
			e.GetID(), logParserHeredocDelimiter,
		)
		return ""
	}
	return wrapped
}

// GetLogParserScriptId returns the log-parser script ID for this engine.
// Returns "" when no log-parser is defined, or when the wrapped script would
// contain the heredoc delimiter (which prevents the write step from running).
// This is always consistent with buildLogParserWriteStep: the ID is non-empty
// only when the write step will actually emit the script file.
func (e *BehaviorDefinedEngine) GetLogParserScriptId() string {
	if e.buildWrappedLogParserScript() == "" {
		return ""
	}
	return e.logParserScriptFilename()
}

// buildLogParserWriteStep generates a GitHub Actions step that writes the
// behavior-defined engine's log-parser script to
// ${RUNNER_TEMP}/gh-aw/actions/<engine-id>_log_parser.cjs so the post-agent
// log-parsing step can require() it.
//
// The raw log-parser JavaScript from the behavior definition is wrapped with a
// createEngineLogParser call from log_parser_shared.cjs so that the author only
// needs to define a parseLog(logContent) function returning
// {markdown, logEntries, mcpFailures, maxTurnsHit}.
//
// The step runs with "if: always()" so the script is written even if an earlier
// step in the job fails, ensuring the log-parsing step (which also runs always)
// can always require() the file.
func (e *BehaviorDefinedEngine) buildLogParserWriteStep() GitHubActionStep {
	script := e.buildWrappedLogParserScript()
	if script == "" {
		return nil
	}
	stepLines := []string{
		"      - name: Write " + e.GetDisplayName() + " log parser script",
		"        if: always()",
	}
	command := fmt.Sprintf(
		"mkdir -p \"%[1]s\"\ncat <<'%[4]s' > \"%[1]s/%[2]s\"\n%[3]s\n%[4]s\nchmod 755 \"%[1]s/%[2]s\"", //nolint:generatedyamlheredoc // Legacy log-parser rendering remains to be migrated.
		SetupActionDestinationShell,
		e.logParserScriptFilename()+".cjs",
		script,
		logParserHeredocDelimiter,
	)
	stepLines = FormatStepWithCommandAndEnv(stepLines, command, nil)
	return GitHubActionStep(stepLines)
}

func (e *BehaviorDefinedEngine) GetExecutionSteps(workflowData *WorkflowData, logFile string) []GitHubActionStep {
	behavior := e.behavior()
	if behavior == nil || behavior.Execution == nil {
		return nil
	}

	exec := behavior.Execution
	firewallEnabled := e.behaviorDefinedFirewallEnabled(workflowData)
	engineCommand := e.buildBehaviorDefinedEngineCommand(exec, workflowData)
	command := e.buildBehaviorDefinedExecutionCommand(exec, workflowData, logFile, engineCommand, firewallEnabled)
	env := e.buildBehaviorDefinedExecutionEnv(exec, workflowData, firewallEnabled)
	steps := e.buildBehaviorDefinedSetupSteps()
	steps = append(steps, e.buildBehaviorDefinedExecutionStep(exec, workflowData, command, env))
	return steps
}

func (e *BehaviorDefinedEngine) buildBehaviorDefinedSetupSteps() []GitHubActionStep {
	var steps []GitHubActionStep
	if configStep := e.buildConfigFileStep(); len(configStep) > 0 {
		steps = append(steps, configStep)
	}
	if harnessStep := e.buildHarnessWriteStep(); len(harnessStep) > 0 {
		steps = append(steps, harnessStep)
	}
	if logParserStep := e.buildLogParserWriteStep(); len(logParserStep) > 0 {
		steps = append(steps, logParserStep)
	}
	return steps
}

func (e *BehaviorDefinedEngine) buildBehaviorDefinedEngineCommand(exec *EngineExecutionDefinition, workflowData *WorkflowData) string {
	commandName := exec.CommandName
	if workflowData != nil && workflowData.EngineConfig != nil && workflowData.EngineConfig.Command != "" {
		commandName = workflowData.EngineConfig.Command
	}
	if behavior := e.behavior(); behavior != nil && behavior.HarnessScript != "" {
		harnessArgs := []string{shellEscapeArg(commandName)}
		if len(exec.Args) > 0 {
			harnessArgs = append(harnessArgs, shellJoinArgs(exec.Args))
		}
		harnessPath := path.Join(SetupActionDestinationShell, e.harnessScriptFilename())
		return fmt.Sprintf("%s %s %s", nodeRuntimeResolutionCommand, harnessPath, strings.Join(harnessArgs, " "))
	}

	commandParts := []string{commandName}
	if len(exec.Args) > 0 {
		commandParts = append(commandParts, shellJoinArgs(exec.Args))
	}
	if modelFragment := e.modelFlagFragment(exec, workflowData); modelFragment != "" {
		commandParts = append(commandParts, modelFragment)
	}
	if mcpFragment := e.mcpFlagFragment(exec, workflowData); mcpFragment != "" {
		commandParts = append(commandParts, mcpFragment)
	}
	commandParts = append(commandParts, fmt.Sprintf(`"$(cat %s)"`, constants.AwPromptsFile))
	return getWorkspaceCommandPrefixFor(workflowData.EngineConfig) + strings.Join(commandParts, " ")
}

func (e *BehaviorDefinedEngine) behaviorDefinedFirewallEnabled(workflowData *WorkflowData) bool {
	firewallEnabled := isFirewallEnabled(workflowData)
	if behavior := e.behavior(); behavior != nil && behavior.HarnessScript != "" && !isFirewallDisabledBySandboxAgent(workflowData) {
		firewallEnabled = true
	}
	return firewallEnabled
}

func (e *BehaviorDefinedEngine) buildBehaviorDefinedExecutionCommand(exec *EngineExecutionDefinition, workflowData *WorkflowData, logFile, engineCommand string, firewallEnabled bool) string {
	if firewallEnabled {
		return e.buildFirewallCommand(exec, workflowData, logFile, engineCommand)
	}
	if exec.WriteTimestamp {
		return fmt.Sprintf("set -o pipefail\nexport no_proxy=\"${NO_PROXY:-}\"\nprintf '%%s' \"$(date +%%s%%3N)\" > %s\n%s 2>&1 | tee -a %s",
			AgentCLIStartMsPath, engineCommand, logFile)
	}
	return fmt.Sprintf("set -o pipefail\nexport no_proxy=\"${NO_PROXY:-}\"\n%s 2>&1 | tee -a %s", engineCommand, logFile)
}

func (e *BehaviorDefinedEngine) buildBehaviorDefinedExecutionEnv(exec *EngineExecutionDefinition, workflowData *WorkflowData, firewallEnabled bool) map[string]string {
	env := map[string]string{
		"GH_AW_PROMPT":     constants.AwPromptsFile,
		"GITHUB_WORKSPACE": "${{ github.workspace }}",
		"NO_PROXY":         constants.AWFNoProxyHosts,
		"RUNNER_TEMP":      "${{ runner.temp }}",
	}
	injectWorkflowCallNetworkAllowedEnv(env, workflowData)
	maps.Copy(env, exec.Env)
	if exec.ProviderEnvMode == behaviorProviderEnvModeUniversalLLMConsumer {
		e.ApplyUniversalProviderEnv(env, workflowData, firewallEnabled)
	}
	e.applyBehaviorDefinedMCPEnv(exec, workflowData, env)
	for _, binding := range e.definition.Auth {
		if binding.Secret != "" {
			env[binding.Secret] = "${{ secrets." + binding.Secret + " }}"
		}
	}
	if behavior := e.behavior(); behavior != nil && behavior.HarnessScript != "" && firewallEnabled {
		env["AWF_REFLECT_ENABLED"] = "1"
	}
	applySafeOutputEnvToMap(env, workflowData)
	applyDefaultMaxAICreditsEnvToMap(env, workflowData)
	applyTraceContextEnvToMap(env)
	applyOptionalEngineToolTimeouts(env, workflowData)
	applyEngineMaxTurnsEnv(env, workflowData)
	applyEngineCwdEnv(env, workflowData)
	applyEngineVersionEnv(env, workflowData)
	applyEngineAndAgentEnv(env, workflowData, behaviorDefinedEngineLog)
	applyMCPScriptsSecretEnv(env, workflowData)
	e.applyBehaviorDefinedModelEnv(exec, workflowData, env)
	return env
}

func (e *BehaviorDefinedEngine) applyBehaviorDefinedMCPEnv(exec *EngineExecutionDefinition, workflowData *WorkflowData, env map[string]string) {
	if exec.MCPConfigEnvVar == "" || !HasMCPServers(workflowData) {
		return
	}
	behavior := e.behavior()
	if behavior != nil && behavior.ConfigFile != nil {
		env[exec.MCPConfigEnvVar] = "${{ github.workspace }}/" + behavior.ConfigFile.Path
		return
	}
	mcpPath := constants.McpServersJsonPathExpr
	if behavior != nil && behavior.MCP != nil && behavior.MCP.ConfigPath != "" {
		mcpPath = behavior.MCP.ConfigPath
	}
	env[exec.MCPConfigEnvVar] = mcpPath
}

func (e *BehaviorDefinedEngine) applyBehaviorDefinedModelEnv(exec *EngineExecutionDefinition, workflowData *WorkflowData, env map[string]string) {
	if exec.ModelEnvVarName == "" || workflowData == nil || workflowData.Model == "" {
		return
	}
	modelVal := workflowData.Model
	if exec.ModelEnvProviderPrefix != "" {
		if parts := strings.SplitN(modelVal, "/", 2); len(parts) == 2 {
			modelVal = path.Join(exec.ModelEnvProviderPrefix, parts[1])
		}
	}
	env[exec.ModelEnvVarName] = modelVal
}

func (e *BehaviorDefinedEngine) buildBehaviorDefinedExecutionStep(exec *EngineExecutionDefinition, workflowData *WorkflowData, command string, env map[string]string) GitHubActionStep {
	stepLines := []string{
		"      - name: " + exec.StepName,
		"        id: agentic_execution",
		"        timeout-minutes: " + resolveStepTimeoutValue(workflowData),
	}
	filteredEnv := FilterEnvForSecrets(env, e.GetRequiredSecretNames(workflowData))
	addCliProxyGHTokenToEnv(filteredEnv, workflowData)
	return GitHubActionStep(FormatStepWithCommandAndEnv(stepLines, wrapAgentExecutionCommand(command), filteredEnv))
}

func (e *BehaviorDefinedEngine) modelFlagFragment(exec *EngineExecutionDefinition, workflowData *WorkflowData) string {
	if exec.ModelEnvVarName == "" || exec.ModelFlag == "" {
		return ""
	}
	if workflowData == nil || workflowData.EngineConfig == nil || workflowData.Model == "" {
		return ""
	}
	return fmt.Sprintf(`%s "$%s"`, exec.ModelFlag, exec.ModelEnvVarName)
}

func (e *BehaviorDefinedEngine) mcpFlagFragment(exec *EngineExecutionDefinition, workflowData *WorkflowData) string {
	if exec.MCPConfigFlag == "" || !HasMCPServers(workflowData) {
		return ""
	}
	path := constants.McpServersJsonPathExpr
	if behavior := e.behavior(); behavior != nil && behavior.MCP != nil {
		if behavior.MCP.ConfigPath != "" {
			path = behavior.MCP.ConfigPath
		}
	}
	return shellJoinArgs([]string{exec.MCPConfigFlag, path})
}

func (e *BehaviorDefinedEngine) buildFirewallCommand(exec *EngineExecutionDefinition, workflowData *WorkflowData, logFile, engineCommand string) string {
	allowedDomains := e.allowedDomains(workflowData)
	// Propagate no_proxy inside the AWF container.  --env-all forwards NO_PROXY
	// from the YAML env block, but Bun (and other runtimes) also check the
	// lowercase variant, so we export it explicitly from the uppercase value.
	engineCommandWithPath := fmt.Sprintf("export no_proxy=\"${NO_PROXY:-}\" && %s && %s", GetNpmBinPathSetup(), engineCommand)
	// microVM runtimes stage the engine CLI under ${RUNNER_TEMP}/gh-aw/engine-cli/bin
	// because the runner tool cache is not mounted inside the sandbox.
	if dockerSbxCLIPath := GetDockerSbxNpmCLIPathSetup(workflowData); dockerSbxCLIPath != "" {
		engineCommandWithPath = fmt.Sprintf("%s && %s", dockerSbxCLIPath, engineCommandWithPath)
	}
	if mcpCLIPath := GetMCPCLIPathSetup(workflowData); mcpCLIPath != "" {
		engineCommandWithPath = fmt.Sprintf("%s && %s", mcpCLIPath, engineCommandWithPath)
	}

	excludedSecretNames := e.GetRequiredSecretNames(workflowData)
	for _, binding := range e.definition.Auth {
		excludedSecretNames = slices.DeleteFunc(excludedSecretNames, func(secretName string) bool {
			return secretName == binding.Secret
		})
	}

	return BuildAWFCommand(AWFCommandConfig{
		EngineName:         e.GetID(),
		EngineCommand:      engineCommandWithPath,
		LogFile:            logFile,
		WorkflowData:       workflowData,
		UsesTTY:            false,
		AllowedDomains:     allowedDomains,
		ExcludeEnvVarNames: ComputeAWFExcludeEnvVarNames(workflowData, excludedSecretNames),
	})
}

func (e *BehaviorDefinedEngine) allowedDomains(workflowData *WorkflowData) string {
	engineName := constants.EngineName(e.GetID())
	behavior := e.behavior()
	if behavior != nil && behavior.Network != nil {
		model := ""
		if workflowData != nil && workflowData.EngineConfig != nil {
			model = workflowData.Model
		}
		defaults, err := resolveEngineNetworkDomains(behavior.Network, model)
		if err != nil {
			panic(fmt.Sprintf("BUG: invalid model %q reached domain computation (should have been caught by validation): %v", model, err))
		}
		return mergeDomainsWithNetworkToolsAndRuntimes(defaults, workflowData.NetworkPermissions, workflowData.Tools, workflowData.Runtimes)
	}
	if e.usesUniversalLLMConsumer() && workflowData != nil && workflowData.EngineConfig != nil {
		return mustGetAllowedDomainsForEngineWithModel(engineName, workflowData.Model, workflowData.NetworkPermissions, workflowData.Tools, workflowData.Runtimes)
	}
	return GetAllowedDomainsForEngine(engineName, workflowData.NetworkPermissions, workflowData.Tools, workflowData.Runtimes)
}

func (e *BehaviorDefinedEngine) buildConfigFileStep() GitHubActionStep {
	behavior := e.behavior()
	if behavior == nil || behavior.ConfigFile == nil || behavior.ConfigFile.Path == "" {
		return nil
	}
	config := behavior.ConfigFile
	shellcheckDirective := ""
	if strings.Contains(config.Content, "$") {
		shellcheckDirective = "# shellcheck disable=SC2016\n"
	}
	command := fmt.Sprintf(`umask 077
mkdir -p "$(dirname "$GITHUB_WORKSPACE/%s")"
CONFIG="$GITHUB_WORKSPACE/%s"
%sBASE_CONFIG='%s'
if [ -f "$CONFIG" ]; then
  MERGED=$(jq -n --argjson base "$BASE_CONFIG" --argjson existing "$(cat "$CONFIG")" '$existing * $base')
  echo "$MERGED" > "$CONFIG"
else
  echo "$BASE_CONFIG" > "$CONFIG"
fi
chmod 600 "$CONFIG"`, config.Path, config.Path, shellcheckDirective, config.Content)
	if config.MergeStrategy != behaviorConfigMergeJSON {
		//nolint:generatedyamlheredoc // Legacy behavior config rendering remains to be migrated to the JavaScript renderer.
		command = fmt.Sprintf(`umask 077
mkdir -p "$(dirname "$GITHUB_WORKSPACE/%s")"
cat <<'EOF' > "$GITHUB_WORKSPACE/%s"
%s
EOF
chmod 600 "$GITHUB_WORKSPACE/%s"`, config.Path, config.Path, config.Content, config.Path)
	}

	stepLines := []string{"      - name: " + config.StepName}
	stepLines = FormatStepWithCommandAndEnv(stepLines, command, nil)
	return GitHubActionStep(stepLines)
}

func isEngineDefinitionForm(def *EngineDefinition) bool {
	if def == nil {
		return false
	}
	// Treat richer metadata-only objects as shared engine definitions. Plain engine
	// config objects ("id", "model", "env", etc.) should continue down the normal
	// EngineConfig path instead of being registered as catalog entries.
	if def.DisplayName != "" || def.RuntimeID != "" || def.Experimental || def.GHSkillAgentName != "" || def.Behaviors != nil || def.DetectionEngine != "" || len(def.Auth) > 0 {
		return true
	}
	if def.Provider.Name != "" || def.Provider.Auth != nil || def.Provider.Request != nil {
		return true
	}
	return def.Models.Default != "" || len(def.Models.Supported) > 0 || len(def.Options) > 0
}

// engineDefinitionBuiltinKeys is the set of JSON strings corresponding to
// built-in engine definitions. It is populated once at startup by
// loadBuiltinEngineDefinitions (via registerBuiltinEngineDefinitionJSON) and
// never modified afterward. Only JSON keys present in this set are eligible for
// caching in engineDefinitionCache, which prevents unbounded growth in long-lived
// compile --watch sessions where each edit to a custom engine definition would
// otherwise create a new cache entry.
var engineDefinitionBuiltinKeys sync.Map // map[string]struct{}

// registerBuiltinEngineDefinitionJSON marks jsonKey as a known built-in engine
// JSON string so that parseEngineDefinitionFromJSON will cache it.
func registerBuiltinEngineDefinitionJSON(jsonKey string) {
	engineDefinitionBuiltinKeys.Store(jsonKey, struct{}{})
}

// engineDefinitionCache caches parsed EngineDefinition values for built-in
// engine JSON strings. Built-in engine files are injected as imports on every
// CompileWorkflow call and their JSON representation is always identical, so
// caching avoids the repeated JSON→any→YAML→struct round-trip that accounted
// for ~24% of BenchmarkCompileMCPWorkflow wall-clock time.
//
// Only keys present in engineDefinitionBuiltinKeys are stored to bound the cache
// to the fixed set of built-in engines. Deep copies are returned on every lookup
// so callers cannot corrupt the cached state through mutations to pointers, slices,
// or maps within the returned definition.
var engineDefinitionCache sync.Map // map[string]EngineDefinition

func parseEngineDefinitionFromJSON(engineJSON string) (*EngineDefinition, error) {
	if engineJSON == "" {
		return nil, nil
	}
	// Fast path: return a deep copy of the cached definition when available.
	if cached, ok := engineDefinitionCache.Load(engineJSON); ok {
		if def, ok := cached.(EngineDefinition); ok {
			defCopy := deepCopyEngineDefinition(def)
			return &defCopy, nil
		}
		// Type assertion failure indicates cache corruption or a concurrent Store with
		// an unexpected type. Log and fall through to re-parse so the caller still works.
		behaviorDefinedEngineLog.Printf("engineDefinitionCache: unexpected value type %T for key (len=%d); re-parsing", cached, len(engineJSON))
		engineDefinitionCache.Delete(engineJSON)
	}
	var engineData any
	if err := json.Unmarshal([]byte(engineJSON), &engineData); err != nil {
		return nil, fmt.Errorf("engine JSON is not recognized, expected a valid JSON object describing the engine: %w", err)
	}
	dataMap, ok := engineData.(map[string]any)
	if !ok {
		return nil, nil
	}
	// EngineDefinition.Auth expects a []AuthBinding sequence. If the auth field is
	// an EngineAuthConfig mapping (e.g. Anthropic/Azure WIF-style auth), strip it before
	// unmarshaling to avoid "mapping was used where sequence is expected". The
	// mapping-style auth is handled separately by extractEngineConfigFromJSON via
	// applyEngineAuthField.
	if isEngineAuthConfigMapping(dataMap["auth"]) {
		delete(dataMap, "auth")
	}
	yamlBytes, err := yaml.Marshal(dataMap)
	if err != nil {
		return nil, fmt.Errorf("engine JSON could not be converted to YAML, expected values that marshal cleanly (e.g. no unsupported types): %w", err)
	}
	var def EngineDefinition
	if err := yaml.Unmarshal(yamlBytes, &def); err != nil {
		return nil, fmt.Errorf("engine definition is not recognized, expected fields matching the EngineDefinition schema: %w", err)
	}
	if def.RuntimeID == "" {
		def.RuntimeID = def.ID
	}
	// Cache only built-in engine definitions (keys pre-seeded by loadBuiltinEngineDefinitions)
	// to prevent unbounded memory growth. Store a deep copy so that any mutations the
	// caller makes to the returned definition cannot corrupt the cached entry.
	if _, isBuiltin := engineDefinitionBuiltinKeys.Load(engineJSON); isBuiltin {
		cacheCopy := deepCopyEngineDefinition(def)
		engineDefinitionCache.Store(engineJSON, cacheCopy)
	}
	return &def, nil
}

func isEngineAuthConfigMapping(auth any) bool {
	authMap, ok := auth.(map[string]any)
	if !ok {
		return false
	}
	authType, ok := authMap["type"].(string)
	return ok && authType == "github-oidc"
}

// deepCopyAny returns a fully independent copy of v for values produced by
// yaml.Unmarshal into interface{}. The possible concrete types are:
// nil, bool, int, float64, string, []any, and map[string]any.
func deepCopyAny(v any) any {
	switch val := v.(type) {
	case map[string]any:
		cp := make(map[string]any, len(val))
		for k, elem := range val {
			cp[k] = deepCopyAny(elem)
		}
		return cp
	case []any:
		cp := make([]any, len(val))
		for i, elem := range val {
			cp[i] = deepCopyAny(elem)
		}
		return cp
	default:
		// Scalars (nil, bool, int, float64, string) are immutable value types.
		return v
	}
}

// deepCopyEngineDefinition returns a fully independent copy of src. All reference
// types (pointers, slices, maps) are recursively copied so that neither the caller
// nor the cache can corrupt the other's state through shared references.
func deepCopyEngineDefinition(src EngineDefinition) EngineDefinition {
	dst := src // value copy covers all scalar fields

	// Models.Supported
	if src.Models.Supported != nil {
		dst.Models.Supported = make([]string, len(src.Models.Supported))
		copy(dst.Models.Supported, src.Models.Supported)
	}

	// Auth ([]AuthBinding; elements contain only string fields so element copy suffices)
	if src.Auth != nil {
		dst.Auth = make([]AuthBinding, len(src.Auth))
		copy(dst.Auth, src.Auth)
	}

	// Options (map[string]any; values may contain nested maps or slices from YAML unmarshal)
	if src.Options != nil {
		dst.Options = make(map[string]any, len(src.Options))
		for k, v := range src.Options {
			dst.Options[k] = deepCopyAny(v)
		}
	}

	// Provider.Auth
	if src.Provider.Auth != nil {
		authCopy := *src.Provider.Auth // AuthDefinition contains only string fields
		dst.Provider.Auth = &authCopy
	}

	// Provider.Request
	if src.Provider.Request != nil {
		reqCopy := *src.Provider.Request
		if src.Provider.Request.Query != nil {
			reqCopy.Query = make(map[string]string, len(src.Provider.Request.Query))
			maps.Copy(reqCopy.Query, src.Provider.Request.Query)
		}
		if src.Provider.Request.BodyInject != nil {
			reqCopy.BodyInject = make(map[string]string, len(src.Provider.Request.BodyInject))
			maps.Copy(reqCopy.BodyInject, src.Provider.Request.BodyInject)
		}
		dst.Provider.Request = &reqCopy
	}

	// Behaviors
	if src.Behaviors != nil {
		behaviorsCopy := deepCopyEngineBehaviorDefinition(*src.Behaviors)
		dst.Behaviors = &behaviorsCopy
	}

	return dst
}

// deepCopyEngineBehaviorDefinition returns a fully independent copy of src.
//
//nolint:largefunc // Deep copy initializes all fields explicitly.
func deepCopyEngineBehaviorDefinition(src EngineBehaviorDefinition) EngineBehaviorDefinition {
	dst := src // value copy covers all scalar fields

	// SupportedEnvVarKeys
	if src.SupportedEnvVarKeys != nil {
		dst.SupportedEnvVarKeys = make([]string, len(src.SupportedEnvVarKeys))
		copy(dst.SupportedEnvVarKeys, src.SupportedEnvVarKeys)
	}

	// Manifest
	if src.Manifest != nil {
		manifestCopy := *src.Manifest
		if src.Manifest.Files != nil {
			manifestCopy.Files = make([]string, len(src.Manifest.Files))
			copy(manifestCopy.Files, src.Manifest.Files)
		}
		if src.Manifest.PathPrefixes != nil {
			manifestCopy.PathPrefixes = make([]string, len(src.Manifest.PathPrefixes))
			copy(manifestCopy.PathPrefixes, src.Manifest.PathPrefixes)
		}
		dst.Manifest = &manifestCopy
	}

	// Network (has Defaults []string and ProviderDomains map[string]string)
	if src.Network != nil {
		networkCopy := *src.Network
		if src.Network.Defaults != nil {
			networkCopy.Defaults = make([]string, len(src.Network.Defaults))
			copy(networkCopy.Defaults, src.Network.Defaults)
		}
		if src.Network.ProviderDomains != nil {
			networkCopy.ProviderDomains = make(map[string]string, len(src.Network.ProviderDomains))
			maps.Copy(networkCopy.ProviderDomains, src.Network.ProviderDomains)
		}
		dst.Network = &networkCopy
	}

	// Installation (only scalar fields; pointer dereference suffices)
	if src.Installation != nil {
		installCopy := *src.Installation
		dst.Installation = &installCopy
	}

	// ConfigFile (only scalar fields)
	if src.ConfigFile != nil {
		cfCopy := *src.ConfigFile
		dst.ConfigFile = &cfCopy
	}

	// Execution (has Args []string and Env map[string]string)
	if src.Execution != nil {
		execCopy := *src.Execution
		if src.Execution.Args != nil {
			execCopy.Args = make([]string, len(src.Execution.Args))
			copy(execCopy.Args, src.Execution.Args)
		}
		if src.Execution.Env != nil {
			execCopy.Env = make(map[string]string, len(src.Execution.Env))
			maps.Copy(execCopy.Env, src.Execution.Env)
		}
		dst.Execution = &execCopy
	}

	// MCP (only scalar fields)
	if src.MCP != nil {
		mcpCopy := *src.MCP
		dst.MCP = &mcpCopy
	}

	return dst
}

func (c *Compiler) registerNamedEngineDefinitionFromJSON(engineJSON string) error {
	def, err := parseEngineDefinitionFromJSON(engineJSON)
	if err != nil || !isEngineDefinitionForm(def) {
		return err
	}
	if def.Behaviors != nil {
		engine, buildErr := NewBehaviorDefinedEngine(def)
		if buildErr != nil {
			return buildErr
		}
		if regErr := c.engineRegistry.Register(engine); regErr != nil {
			return regErr
		}
		def.RuntimeID = engine.GetID()
	}
	c.engineCatalog.Register(def)
	return nil
}
