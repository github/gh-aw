package workflow

import (
	"fmt"
	"maps"
	"path"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
)

func (e *BehaviorDefinedEngine) GetExecutionSteps(workflowData *WorkflowData, logFile string) []GitHubActionStep {
	behavior := e.behavior()
	if behavior == nil || behavior.Execution == nil {
		return nil
	}

	exec := behavior.Execution
	var backend UniversalLLMBackend
	if e.usesUniversalLLMConsumer() || exec.ProviderEnvMode == behaviorProviderEnvModeUniversalLLMConsumer {
		backend = e.resolveBackend(workflowData)
	}
	firewallEnabled := e.behaviorDefinedFirewallEnabled(workflowData)
	engineCommand := e.buildBehaviorDefinedEngineCommand(exec, workflowData)
	if isSkipIfDecisionEnabled(workflowData) {
		engineCommand = buildShellHarnessCommand(e.GetID(), engineCommand)
	}
	command := e.buildBehaviorDefinedExecutionCommand(exec, workflowData, logFile, engineCommand, firewallEnabled, backend)
	env := e.buildBehaviorDefinedExecutionEnv(exec, workflowData, firewallEnabled, backend)
	steps := e.buildBehaviorDefinedSetupSteps()
	steps = append(steps, e.buildBehaviorDefinedExecutionStep(exec, workflowData, command, env, backend))
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
		nodeCommand := nodeRuntimeResolutionCommand
		if exec.ProtectedSessionCapture {
			nodeCommand = strings.TrimSuffix(nodeCommand, `"$GH_AW_NODE_EXEC"`) + `exec "$GH_AW_NODE_EXEC"`
		}
		return fmt.Sprintf("%s %s %s", nodeCommand, harnessPath, strings.Join(harnessArgs, " "))
	}

	if exec.ProtectedSessionCapture {
		commandName = "exec " + commandName
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
	if behavior := e.behavior(); behavior != nil && (behavior.HarnessScript != "" || e.protectedSessionCaptureEnabled()) && !isFirewallDisabledBySandboxAgent(workflowData) {
		firewallEnabled = true
	}
	return firewallEnabled
}

func (e *BehaviorDefinedEngine) buildBehaviorDefinedExecutionCommand(exec *EngineExecutionDefinition, workflowData *WorkflowData, logFile, engineCommand string, firewallEnabled bool, backend UniversalLLMBackend) string {
	if firewallEnabled {
		return e.buildFirewallCommand(exec, workflowData, logFile, engineCommand, backend)
	}
	if exec.WriteTimestamp {
		return fmt.Sprintf("set -o pipefail\nexport no_proxy=\"${NO_PROXY:-}\"\nprintf '%%s' \"$(date +%%s%%3N)\" > %s\n%s 2>&1 | tee -a %s",
			AgentCLIStartMsPath, engineCommand, logFile)
	}
	return fmt.Sprintf("set -o pipefail\nexport no_proxy=\"${NO_PROXY:-}\"\n%s 2>&1 | tee -a %s", engineCommand, logFile)
}

func (e *BehaviorDefinedEngine) buildBehaviorDefinedExecutionEnv(exec *EngineExecutionDefinition, workflowData *WorkflowData, firewallEnabled bool, backend UniversalLLMBackend) map[string]string {
	env := map[string]string{
		"GH_AW_PROMPT":          constants.AwPromptsFile,
		"GITHUB_WORKSPACE":      "${{ github.workspace }}",
		"GH_AW_TIMEOUT_MINUTES": resolveStepTimeoutValue(workflowData),
		"NO_PROXY":              constants.AWFNoProxyHosts,
		"RUNNER_TEMP":           "${{ runner.temp }}",
	}
	injectWorkflowCallNetworkAllowedEnv(env, workflowData)
	maps.Copy(env, exec.Env)
	if e.pipInstallation() != nil {
		env["GH_AW_PYTHON_PACKAGES"] = pythonPackagesSitePath(e.definition.ID)
	}
	if exec.ProviderEnvMode == behaviorProviderEnvModeUniversalLLMConsumer {
		e.applyUniversalProviderEnvForBackend(env, workflowData, firewallEnabled, backend)
		if firewallEnabled && backend == UniversalLLMBackendCopilot {
			env[constants.CopilotBYOKDummyAPIKeyEnvVar] = constants.CopilotBYOKDummyAPIKey
		}
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
	applyAWFRuntimeAgentTimeoutEnvToMap(env, workflowData)
	applyTraceContextEnvToMap(env)
	applyOptionalEngineToolTimeouts(env, workflowData)
	applyEngineMaxTurnsEnv(env, workflowData)
	applyEngineCwdEnv(env, workflowData)
	applyEngineVersionEnv(env, workflowData)
	applyHarnessRetryPolicyEnv(env, workflowData)
	applyEngineAndAgentEnv(env, workflowData, behaviorDefinedEngineLog)
	e.applyBehaviorDefinedModelEnv(exec, workflowData, env)
	if exec.ProtectedSessionCapture && firewallEnabled {
		env["RUNNER_TEMP"] = "${{ runner.temp }}"
		env["GH_AW_PROTECTED_SESSION_CAPTURE"] = "1"
		env["GH_AW_PROTECTED_CAPTURE_ID"] = protectedSessionCaptureID
	}
	applyDurableExecutionEnv(env, workflowData)
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
		if _, model, ok := strings.Cut(modelVal, "/"); ok {
			modelVal = path.Join(exec.ModelEnvProviderPrefix, model)
		}
	}
	env[exec.ModelEnvVarName] = modelVal
}

func (e *BehaviorDefinedEngine) buildBehaviorDefinedExecutionStep(exec *EngineExecutionDefinition, workflowData *WorkflowData, command string, env map[string]string, backend UniversalLLMBackend) GitHubActionStep {
	stepLines := []string{
		"      - name: " + exec.StepName,
		"        id: agentic_execution",
		"        timeout-minutes: " + resolveStepTimeoutValue(workflowData),
	}
	filteredEnv := FilterEnvForSecrets(env, e.getRequiredSecretNamesForBackend(workflowData, backend))
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

func (e *BehaviorDefinedEngine) buildFirewallCommand(exec *EngineExecutionDefinition, workflowData *WorkflowData, logFile, engineCommand string, backend UniversalLLMBackend) string {
	allowedDomains := e.allowedDomains(workflowData)
	// Propagate no_proxy inside the AWF container.  --env-all forwards NO_PROXY
	// from the YAML env block, but Bun (and other runtimes) also check the
	// lowercase variant, so we export it explicitly from the uppercase value.
	engineCommandWithPath := fmt.Sprintf("export no_proxy=\"${NO_PROXY:-}\" && %s && %s", GetNpmBinPathSetup(), engineCommand)
	if exec.ProviderEnvMode == behaviorProviderEnvModeUniversalLLMConsumer && backend == UniversalLLMBackendCopilot {
		// Introduce the client key after AWF credential discovery, never in its startup environment.
		engineCommandWithPath = `export OPENAI_API_KEY="$` + constants.CopilotBYOKDummyAPIKeyEnvVar + `" && ` + engineCommandWithPath
	}
	// microVM runtimes stage the engine CLI under ${RUNNER_TEMP}/gh-aw/engine-cli/bin
	// because the runner tool cache is not mounted inside the sandbox.
	if microVMCLIPath := GetMicroVMNpmCLIPathSetup(workflowData); microVMCLIPath != "" {
		engineCommandWithPath = fmt.Sprintf("%s && %s", microVMCLIPath, engineCommandWithPath)
	}
	if mcpCLIPath := GetMCPCLIPathSetup(workflowData); mcpCLIPath != "" {
		engineCommandWithPath = fmt.Sprintf("%s && %s", mcpCLIPath, engineCommandWithPath)
	}

	excludedSecretNames := e.getRequiredSecretNamesForBackend(workflowData, backend)
	if e.behavior().SecretStrategy != behaviorSecretStrategyGeminiAPIKey {
		for _, binding := range e.definition.Auth {
			excludedSecretNames = slices.DeleteFunc(excludedSecretNames, func(secretName string) bool {
				return secretName == binding.Secret
			})
		}
	}

	return BuildAWFCommand(AWFCommandConfig{
		EngineName:         e.GetID(),
		EngineRuntimeID:    e.definition.RuntimeID,
		EngineCommand:      engineCommandWithPath,
		LogFile:            logFile,
		WorkflowData:       workflowData,
		UsesTTY:            false,
		AllowedDomains:     allowedDomains,
		ExcludeEnvVarNames: ComputeAWFExcludeEnvVarNames(workflowData, excludedSecretNames),
		ProtectLogCapture:  exec.ProtectedSessionCapture,
	})
}

func (e *BehaviorDefinedEngine) allowedDomains(workflowData *WorkflowData) string {
	engineName := constants.EngineName(e.GetID())
	behavior := e.behavior()
	if behavior != nil && behavior.Network != nil {
		var defaults []string
		if e.usesUniversalLLMConsumer() || (behavior.Execution != nil && behavior.Execution.ProviderEnvMode == behaviorProviderEnvModeUniversalLLMConsumer) {
			// AWF-proxied inference uses its gateway target, not direct agent egress.
			defaults = behavior.Network.Defaults
		} else {
			model := ""
			if workflowData != nil && workflowData.EngineConfig != nil {
				model = workflowData.Model
			}
			var err error
			defaults, err = resolveEngineNetworkDomains(behavior.Network, model)
			if err != nil {
				panic(fmt.Sprintf("BUG: invalid model %q reached domain computation (should have been caught by validation): %v", model, err))
			}
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
