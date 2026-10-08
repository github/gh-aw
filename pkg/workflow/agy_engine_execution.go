package workflow

import (
	"fmt"
	"path"

	"github.com/github/gh-aw/pkg/constants"
)

func (e *AgyEngine) GetExecutionSteps(workflowData *WorkflowData, logFile string) []GitHubActionStep {
	firewallEnabled := isFirewallEnabled(workflowData)
	commandName := "agy"
	if workflowData.EngineConfig != nil && workflowData.EngineConfig.Command != "" {
		commandName = workflowData.EngineConfig.Command
	}
	engineCommand := fmt.Sprintf("%s %s %s", nodeRuntimeResolutionCommand,
		path.Join(SetupActionDestinationShell, e.GetHarnessScriptName()), shellEscapeArg(commandName))
	command := e.buildExecutionCommand(workflowData, logFile, engineCommand, firewallEnabled)
	env := e.buildExecutionEnv(workflowData, firewallEnabled)
	step := []string{
		"      - name: Execute experimental Agy CLI",
		"        id: agentic_execution",
		"        timeout-minutes: " + resolveStepTimeoutValue(workflowData),
	}
	filteredEnv := FilterEnvForSecrets(env, e.GetRequiredSecretNames(workflowData))
	addCliProxyGHTokenToEnv(filteredEnv, workflowData)
	return []GitHubActionStep{
		GitHubActionStep(FormatStepWithCommandAndEnv(step, wrapAgentExecutionCommand(command), filteredEnv)),
	}
}

func (e *AgyEngine) buildExecutionCommand(workflowData *WorkflowData, logFile, engineCommand string, firewallEnabled bool) string {
	if !firewallEnabled {
		return fmt.Sprintf("set -o pipefail\nexport no_proxy=\"${NO_PROXY:-}\"\nprintf '%%s' \"$(date +%%s%%3N)\" > %s\ntouch %s\n%s 2>&1 | tee -a %s",
			AgentCLIStartMsPath, AgentStepSummaryPath, engineCommand, logFile)
	}
	engineCommand = fmt.Sprintf("export no_proxy=\"${NO_PROXY:-}\" && %s && %s", GetNpmBinPathSetup(), engineCommand)
	if cliPath := GetMicroVMNpmCLIPathSetup(workflowData); cliPath != "" {
		engineCommand = fmt.Sprintf("%s && %s", cliPath, engineCommand)
	}
	if cliPath := GetMCPCLIPathSetup(workflowData); cliPath != "" {
		engineCommand = fmt.Sprintf("%s && %s", cliPath, engineCommand)
	}
	return BuildAWFCommand(AWFCommandConfig{
		EngineName:         e.GetID(),
		EngineCommand:      engineCommand,
		LogFile:            logFile,
		WorkflowData:       workflowData,
		UsesTTY:            false,
		PathSetup:          "touch " + AgentStepSummaryPath,
		AllowedDomains:     mergeDomainsWithNetworkToolsAndRuntimes(AgyDefaultDomains, workflowData.NetworkPermissions, workflowData.Tools, workflowData.Runtimes),
		ExcludeEnvVarNames: ComputeAWFExcludeEnvVarNames(workflowData, e.GetRequiredSecretNames(workflowData)),
	})
}

func (e *AgyEngine) buildExecutionEnv(workflowData *WorkflowData, firewallEnabled bool) map[string]string {
	env := map[string]string{
		"GH_AW_PROMPT":         constants.AwPromptsFile,
		"GITHUB_WORKSPACE":     "${{ github.workspace }}",
		"GITHUB_STEP_SUMMARY":  AgentStepSummaryPath,
		"NO_PROXY":             constants.AWFNoProxyHosts,
		"RUNNER_TEMP":          "${{ runner.temp }}",
		"GH_AW_AGY_MODEL":      constants.AgyDefaultModel,
		"GH_AW_ENGINE_VERSION": string(constants.DefaultAgyVersion),
		"GEMINI_API_KEY":       "${{ secrets.GEMINI_API_KEY }}",
	}
	if firewallEnabled {
		env["AWF_REFLECT_ENABLED"] = "1"
	}
	if HasMCPServers(workflowData) {
		env["GH_AW_MCP_CONFIG"] = "${{ github.workspace }}/.agents/mcp_config.json"
	}
	injectWorkflowCallNetworkAllowedEnv(env, workflowData)
	applySafeOutputEnvToMap(env, workflowData)
	applyDefaultMaxAICreditsEnvToMap(env, workflowData)
	applyTraceContextEnvToMap(env)
	applyOptionalEngineToolTimeouts(env, workflowData)
	applyEngineMaxTurnsEnv(env, workflowData)
	applyEngineVersionEnv(env, workflowData)
	applyEngineAndAgentEnv(env, workflowData, agyLog)
	applyMCPScriptsSecretEnv(env, workflowData)
	if workflowData.Model != "" {
		env[e.GetModelEnvVarName()] = workflowData.Model
	}
	return env
}
