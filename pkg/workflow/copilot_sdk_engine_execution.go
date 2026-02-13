// This file provides Copilot SDK engine execution logic.
//
// Instead of building shell commands with --allow-tool flags (as the CLI engine does),
// this engine generates a JSON config file and invokes the copilot-runner binary.
// The runner binary uses the Copilot SDK (Go) to programmatically control the session.
//
// The execution strategy still supports all sandbox modes (AWF, SRT, standard) because
// the runner binary is just another executable that can be wrapped by sandboxes.
//
// Generated workflow step:
//  1. Write a JSON config file with all workflow settings
//  2. Invoke copilot-runner --config <config-file>
//  3. The runner handles SDK client lifecycle, session management, and structured output

package workflow

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

var copilotSDKExecLog = logger.New("workflow:copilot_sdk_engine_execution")

// SDKRunnerConfig represents the JSON configuration for the copilot-runner binary.
// This is serialized to a JSON file that the runner reads at startup.
type SDKRunnerConfig struct {
	// CLIPath is the path to the Copilot CLI binary (still required by the SDK).
	// This must be an actual executable path (not containing spaces or arguments).
	CLIPath string `json:"cli_path"`

	// CLIArgs is a list of additional arguments to prepend before the Copilot CLI binary.
	// For example, in SRT mode the CLI is invoked via "node ./node_modules/.bin/copilot",
	// so CLIPath would be "node" and CLIArgs would be ["./node_modules/.bin/copilot"].
	CLIArgs []string `json:"cli_args,omitempty"`

	// Model is the LLM model to use for the session
	Model string `json:"model,omitempty"`

	// PromptFile is the path to the prompt text file
	PromptFile string `json:"prompt_file"`

	// SystemMessage is an optional system message for the session
	SystemMessage string `json:"system_message,omitempty"`

	// AvailableTools is the list of tools to make available to the agent
	AvailableTools []string `json:"available_tools,omitempty"`

	// ExcludedTools is the list of tools to exclude from the agent
	ExcludedTools []string `json:"excluded_tools,omitempty"`

	// MCPConfigPath is the path to the MCP config JSON file (reuses the same format)
	MCPConfigPath string `json:"mcp_config_path,omitempty"`

	// WorkingDirectory is the working directory for the session
	WorkingDirectory string `json:"working_directory,omitempty"`

	// AddDirs is a list of directories to grant file access to
	AddDirs []string `json:"add_dirs,omitempty"`

	// LogDir is the directory for log output
	LogDir string `json:"log_dir"`

	// ShareFile is the path to write the conversation markdown
	ShareFile string `json:"share_file,omitempty"`

	// AutoApprovePermissions auto-approves all tool permission requests
	AutoApprovePermissions bool `json:"auto_approve_permissions"`

	// MaxTurns limits the number of conversation turns (0 = unlimited)
	MaxTurns int `json:"max_turns,omitempty"`

	// TimeoutSeconds is the maximum execution time in seconds
	TimeoutSeconds int `json:"timeout_seconds,omitempty"`

	// DisableBuiltinMCPs disables built-in MCP servers
	DisableBuiltinMCPs bool `json:"disable_builtin_mcps"`

	// AllowAllPaths allows write access to all paths
	AllowAllPaths bool `json:"allow_all_paths,omitempty"`

	// Agent is the agent identifier (if using --agent)
	Agent string `json:"agent,omitempty"`
}

const (
	// sdkConfigPath is where the runner config JSON is written
	sdkConfigPath = "/tmp/gh-aw/sdk-config.json"

	// copilotRunnerBinaryPath is the installed path of the runner binary
	copilotRunnerBinaryPath = "/opt/gh-aw/actions/copilot-runner"
)

// GetExecutionSteps returns the GitHub Actions steps for executing the Copilot SDK runner.
func (e *CopilotSDKEngine) GetExecutionSteps(workflowData *WorkflowData, logFile string) []GitHubActionStep {
	copilotSDKExecLog.Printf("Generating execution steps for Copilot SDK: workflow=%s, firewall=%v", workflowData.Name, isFirewallEnabled(workflowData))

	// Handle custom steps if they exist in engine config
	steps := InjectCustomEngineSteps(workflowData, e.convertStepToYAML)

	sandboxEnabled := isFirewallEnabled(workflowData) || isSRTEnabled(workflowData)

	// Build the runner config
	config := e.buildRunnerConfig(workflowData, sandboxEnabled)

	// Serialize config to JSON
	configJSON, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		copilotSDKExecLog.Printf("Error marshaling SDK config: %v", err)
		configJSON = []byte("{}")
	}

	// Determine the runner command
	var commandName string
	if workflowData.EngineConfig != nil && workflowData.EngineConfig.Command != "" {
		commandName = workflowData.EngineConfig.Command
		copilotSDKExecLog.Printf("Using custom command: %s", commandName)
	} else {
		commandName = copilotRunnerBinaryPath
	}

	// Build model environment variable handling
	modelConfigured := workflowData.EngineConfig != nil && workflowData.EngineConfig.Model != ""
	isDetectionJob := workflowData.SafeOutputs == nil
	var modelEnvVar string
	if isDetectionJob {
		modelEnvVar = constants.EnvVarModelDetectionCopilot
	} else {
		modelEnvVar = constants.EnvVarModelAgentCopilot
	}

	// Build the runner invocation command
	runnerCommand := fmt.Sprintf("%s --config %s", commandName, sdkConfigPath)

	// Add conditional model override via environment variable if model not explicitly set
	if !modelConfigured {
		runnerCommand = fmt.Sprintf(`%s${%s:+ --model-override "$%s"}`, runnerCommand, modelEnvVar, modelEnvVar)
	}

	// Build the write-config + execute command
	var command string
	writeConfigCmd := fmt.Sprintf("mkdir -p /tmp/gh-aw/\ncat > %s << 'SDKCONFIG'\n%s\nSDKCONFIG", sdkConfigPath, string(configJSON))

	if isSRTEnabled(workflowData) {
		copilotSDKExecLog.Print("Using Sandbox Runtime (SRT) for execution")
		agentConfig := getAgentConfig(workflowData)

		if agentConfig != nil && agentConfig.Command != "" {
			escapedCommand := shellEscapeArg(runnerCommand)
			var srtArgs []string
			if len(agentConfig.Args) > 0 {
				srtArgs = append(srtArgs, agentConfig.Args...)
			}
			command = fmt.Sprintf(`set -o pipefail
%s
%s %s -- %s 2>&1 | tee %s`, writeConfigCmd, agentConfig.Command, shellJoinArgs(srtArgs), escapedCommand, shellEscapeArg(logFile))
		} else {
			command = fmt.Sprintf(`set -o pipefail
%s
%s 2>&1 | tee %s`, writeConfigCmd, runnerCommand, logFile)
		}
	} else if isFirewallEnabled(workflowData) {
		copilotSDKExecLog.Print("Using AWF firewall for execution")
		firewallConfig := getFirewallConfig(workflowData)
		agentConfig := getAgentConfig(workflowData)
		var awfLogLevel = "info"
		if firewallConfig != nil && firewallConfig.LogLevel != "" {
			awfLogLevel = firewallConfig.LogLevel
		}

		allowedDomains := GetCopilotAllowedDomainsWithToolsAndRuntimes(workflowData.NetworkPermissions, workflowData.Tools, workflowData.Runtimes)

		var awfArgs []string
		awfArgs = append(awfArgs, "--env-all")
		awfArgs = append(awfArgs, "--container-workdir", "\"${GITHUB_WORKSPACE}\"")

		// Add custom mounts
		if agentConfig != nil && len(agentConfig.Mounts) > 0 {
			sortedMounts := make([]string, len(agentConfig.Mounts))
			copy(sortedMounts, agentConfig.Mounts)
			sort.Strings(sortedMounts)
			for _, mount := range sortedMounts {
				awfArgs = append(awfArgs, "--mount", mount)
			}
		}

		awfArgs = append(awfArgs, "--allow-domains", allowedDomains)

		blockedDomains := formatBlockedDomains(workflowData.NetworkPermissions)
		if blockedDomains != "" {
			awfArgs = append(awfArgs, "--block-domains", blockedDomains)
		}

		awfArgs = append(awfArgs, "--log-level", awfLogLevel)
		awfArgs = append(awfArgs, "--proxy-logs-dir", "/tmp/gh-aw/sandbox/firewall/logs")

		if HasMCPServers(workflowData) {
			awfArgs = append(awfArgs, "--enable-host-access")
		}

		awfImageTag := getAWFImageTag(firewallConfig)
		awfArgs = append(awfArgs, "--image-tag", awfImageTag)
		awfArgs = append(awfArgs, "--skip-pull")

		sslBumpArgs := getSSLBumpArgs(firewallConfig)
		awfArgs = append(awfArgs, sslBumpArgs...)

		if firewallConfig != nil && len(firewallConfig.Args) > 0 {
			awfArgs = append(awfArgs, firewallConfig.Args...)
		}

		if agentConfig != nil && len(agentConfig.Args) > 0 {
			awfArgs = append(awfArgs, agentConfig.Args...)
		}

		var awfCommand string
		if agentConfig != nil && agentConfig.Command != "" {
			awfCommand = agentConfig.Command
		} else {
			awfCommand = "sudo -E awf"
		}

		escapedCommand := shellEscapeArg(runnerCommand)

		command = fmt.Sprintf(`set -o pipefail
%s
%s %s \
  -- %s \
  2>&1 | tee %s`, writeConfigCmd, awfCommand, shellJoinArgs(awfArgs), escapedCommand, shellEscapeArg(logFile))
	} else {
		// Standard mode (no sandbox)
		command = fmt.Sprintf(`set -o pipefail
%s
%s 2>&1 | tee %s`, writeConfigCmd, runnerCommand, logFile)
	}

	// Build environment variables
	var copilotGitHubToken string
	if workflowData.GitHubToken != "" {
		copilotGitHubToken = workflowData.GitHubToken
	} else {
		// #nosec G101 -- This is NOT a hardcoded credential. It's a GitHub Actions expression template.
		copilotGitHubToken = "${{ secrets.COPILOT_GITHUB_TOKEN }}"
	}

	env := map[string]string{
		"XDG_CONFIG_HOME":           "/home/runner",
		"COPILOT_AGENT_RUNNER_TYPE": "STANDALONE",
		"COPILOT_GITHUB_TOKEN":      copilotGitHubToken,
		"GITHUB_STEP_SUMMARY":       "${{ env.GITHUB_STEP_SUMMARY }}",
		"GITHUB_HEAD_REF":           "${{ github.head_ref }}",
		"GITHUB_REF_NAME":           "${{ github.ref_name }}",
		"GITHUB_WORKSPACE":          "${{ github.workspace }}",
	}

	env["GH_AW_PROMPT"] = "/tmp/gh-aw/aw-prompts/prompt.txt"

	if HasMCPServers(workflowData) {
		env["GH_AW_MCP_CONFIG"] = "/home/runner/.copilot/mcp-config.json"
	}

	if hasGitHubTool(workflowData.ParsedTools) {
		customGitHubToken := getGitHubToken(workflowData.Tools["github"])
		effectiveToken := getEffectiveGitHubToken(customGitHubToken, workflowData.GitHubToken)
		env["GITHUB_MCP_SERVER_TOKEN"] = effectiveToken
	}

	applySafeOutputEnvToMap(env, workflowData)

	if workflowData.ToolsStartupTimeout > 0 {
		env["GH_AW_STARTUP_TIMEOUT"] = fmt.Sprintf("%d", workflowData.ToolsStartupTimeout)
	}

	if workflowData.ToolsTimeout > 0 {
		env["GH_AW_TOOL_TIMEOUT"] = fmt.Sprintf("%d", workflowData.ToolsTimeout)
	}

	if workflowData.EngineConfig != nil && workflowData.EngineConfig.MaxTurns != "" {
		env["GH_AW_MAX_TURNS"] = workflowData.EngineConfig.MaxTurns
	}

	// Add model environment variable if model is not explicitly configured
	if workflowData.EngineConfig == nil || workflowData.EngineConfig.Model == "" {
		if isDetectionJob {
			env[constants.EnvVarModelDetectionCopilot] = fmt.Sprintf("${{ vars.%s || '' }}", constants.EnvVarModelDetectionCopilot)
		} else {
			env[constants.EnvVarModelAgentCopilot] = fmt.Sprintf("${{ vars.%s || '' }}", constants.EnvVarModelAgentCopilot)
		}
	}

	// Add custom environment variables
	if workflowData.EngineConfig != nil && len(workflowData.EngineConfig.Env) > 0 {
		for key, value := range workflowData.EngineConfig.Env {
			env[key] = value
		}
	}

	agentConfig := getAgentConfig(workflowData)
	if agentConfig != nil && len(agentConfig.Env) > 0 {
		for key, value := range agentConfig.Env {
			env[key] = value
		}
	}

	// Add HTTP MCP header secrets
	headerSecrets := collectHTTPMCPHeaderSecrets(workflowData.Tools)
	for varName, secretExpr := range headerSecrets {
		if _, exists := env[varName]; !exists {
			env[varName] = secretExpr
		}
	}

	// Add safe-inputs secrets
	if IsSafeInputsEnabled(workflowData.SafeInputs, workflowData) {
		safeInputsSecrets := collectSafeInputsSecrets(workflowData.SafeInputs)
		for varName, secretExpr := range safeInputsSecrets {
			if _, exists := env[varName]; !exists {
				env[varName] = secretExpr
			}
		}
	}

	// Generate the step
	stepName := "Execute GitHub Copilot SDK Agent"
	var stepLines []string

	stepLines = append(stepLines, fmt.Sprintf("      - name: %s", stepName))
	stepLines = append(stepLines, "        id: agentic_execution")

	// Add tool arguments comment
	toolArgsComment := e.generateSDKToolArgumentsComment(workflowData, "        ")
	if toolArgsComment != "" {
		commentLines := strings.Split(strings.TrimSuffix(toolArgsComment, "\n"), "\n")
		stepLines = append(stepLines, commentLines...)
	}

	// Add timeout
	if workflowData.TimeoutMinutes != "" {
		timeoutValue := strings.TrimPrefix(workflowData.TimeoutMinutes, "timeout-minutes: ")
		stepLines = append(stepLines, fmt.Sprintf("        timeout-minutes: %s", timeoutValue))
	} else {
		stepLines = append(stepLines, fmt.Sprintf("        timeout-minutes: %d", int(constants.DefaultAgenticWorkflowTimeout/time.Minute)))
	}

	// Filter environment variables
	allowedSecrets := e.GetRequiredSecretNames(workflowData)
	filteredEnv := FilterEnvForSecrets(env, allowedSecrets)

	// Format step with command and environment
	stepLines = FormatStepWithCommandAndEnv(stepLines, command, filteredEnv)

	steps = append(steps, GitHubActionStep(stepLines))

	return steps
}

// buildRunnerConfig constructs the SDKRunnerConfig from workflow data.
func (e *CopilotSDKEngine) buildRunnerConfig(workflowData *WorkflowData, sandboxEnabled bool) SDKRunnerConfig {
	config := SDKRunnerConfig{
		PromptFile:             "/tmp/gh-aw/aw-prompts/prompt.txt",
		LogDir:                 logsFolder,
		ShareFile:              logsFolder + "conversation.md",
		AutoApprovePermissions: true,
		DisableBuiltinMCPs:     true,
	}

	// Set CLI path based on sandbox mode.
	// CLIPath must be an actual executable path; any prefix arguments go in CLIArgs.
	if sandboxEnabled {
		if isSRTEnabled(workflowData) {
			config.CLIPath = "node"
			config.CLIArgs = []string{"./node_modules/.bin/copilot"}
		} else {
			config.CLIPath = "/usr/local/bin/copilot"
		}
	} else {
		config.CLIPath = "copilot"
	}

	// Set model
	if workflowData.EngineConfig != nil && workflowData.EngineConfig.Model != "" {
		config.Model = workflowData.EngineConfig.Model
	}

	// Set agent
	if workflowData.EngineConfig != nil && workflowData.EngineConfig.Agent != "" {
		config.Agent = workflowData.EngineConfig.Agent
	}

	// Set add-dirs based on sandbox mode
	if sandboxEnabled {
		config.AddDirs = []string{"/tmp/gh-aw/", "${GITHUB_WORKSPACE}"}
	} else {
		config.AddDirs = []string{"/tmp/", "/tmp/gh-aw/", "/tmp/gh-aw/agent/"}
	}

	// Set tools
	config.AvailableTools, config.ExcludedTools = e.computeSDKToolConfig(workflowData)

	// Set MCP config path if MCP servers are present
	if HasMCPServers(workflowData) {
		config.MCPConfigPath = "/home/runner/.copilot/mcp-config.json"
	}

	// Set working directory
	config.WorkingDirectory = "${GITHUB_WORKSPACE}"

	// Set allow-all-paths when edit tool is enabled
	if workflowData.ParsedTools != nil && workflowData.ParsedTools.Edit != nil {
		config.AllowAllPaths = true
	}

	// Add cache memory dirs
	if workflowData.CacheMemoryConfig != nil {
		for _, cache := range workflowData.CacheMemoryConfig.Caches {
			var cacheDir string
			if cache.ID == "default" {
				cacheDir = "/tmp/gh-aw/cache-memory/"
			} else {
				cacheDir = fmt.Sprintf("/tmp/gh-aw/cache-memory-%s/", cache.ID)
			}
			config.AddDirs = append(config.AddDirs, cacheDir)
		}
	}

	return config
}

// generateSDKToolArgumentsComment generates a comment documenting the tool configuration.
func (e *CopilotSDKEngine) generateSDKToolArgumentsComment(workflowData *WorkflowData, indent string) string {
	available, excluded := e.computeSDKToolConfig(workflowData)
	if len(available) == 0 && len(excluded) == 0 {
		return ""
	}

	var comment strings.Builder
	comment.WriteString(indent + "# Copilot SDK tool configuration:\n")

	if len(available) > 0 {
		comment.WriteString(indent + "# Available tools: " + strings.Join(available, ", ") + "\n")
	}
	if len(excluded) > 0 {
		comment.WriteString(indent + "# Excluded tools: " + strings.Join(excluded, ", ") + "\n")
	}

	return comment.String()
}
