// This file provides Copilot engine execution logic.
//
// This file contains the GetExecutionSteps function which generates the complete
// GitHub Actions workflow for executing GitHub Copilot CLI. This is the largest
// and most complex function in the Copilot engine, handling:
//
//   - Copilot CLI argument construction based on sandbox mode (AWF, SRT, or standard)
//   - Tool permission configuration (--allow-tool flags)
//   - MCP server configuration and environment setup
//   - Sandbox wrapping (AWF or SRT)
//   - Environment variable handling for model selection and secrets
//   - Log file configuration and output collection
//
// The execution strategy varies significantly based on sandbox mode:
//   - Standard mode: Direct copilot CLI execution
//   - AWF mode: Wrapped with awf binary for network firewalling
//   - SRT mode: Wrapped with Sandbox Runtime for process isolation
//
// This function is intentionally kept in a separate file due to its size (~430 lines)
// and complexity. Future refactoring may split it further if needed.

package workflow

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

var copilotExecLog = logger.New("workflow:copilot_engine_execution")

// GetExecutionSteps returns the GitHub Actions steps for executing GitHub Copilot CLI
func (e *CopilotEngine) GetExecutionSteps(workflowData *WorkflowData, logFile string) []GitHubActionStep {
	copilotExecLog.Printf("Generating execution steps for Copilot: workflow=%s, firewall=%v", workflowData.Name, isFirewallEnabled(workflowData))

	// Handle custom steps if they exist in engine config
	steps := InjectCustomEngineSteps(workflowData, e.convertStepToYAML)

	// Build copilot CLI arguments based on configuration
	var copilotArgs []string
	sandboxEnabled := isFirewallEnabled(workflowData) || isSRTEnabled(workflowData)
	if sandboxEnabled {
		// Simplified args for sandbox mode (AWF or SRT)
		copilotArgs = []string{"--add-dir", "/tmp/gh-aw/", "--log-level", "all", "--log-dir", logsFolder}

		// Always add workspace directory to --add-dir so Copilot CLI can access it
		// This allows Copilot CLI to discover agent files and access the workspace
		// Use double quotes to allow shell variable expansion
		copilotArgs = append(copilotArgs, "--add-dir", "\"${GITHUB_WORKSPACE}\"")
		copilotExecLog.Print("Added workspace directory to --add-dir")

		// Add Copilot config directory when plugins are declared so the CLI can discover installed plugins
		// Plugins are installed to ~/.copilot/plugins/ via copilot plugin install command
		// The CLI also reads plugin-index.json from ~/.copilot/ to discover installed plugins
		if workflowData.PluginInfo != nil && len(workflowData.PluginInfo.Plugins) > 0 {
			copilotArgs = append(copilotArgs, "--add-dir", "/home/runner/.copilot/")
			copilotExecLog.Printf("Added Copilot config directory to --add-dir for plugin discovery (%d plugins)", len(workflowData.PluginInfo.Plugins))
		}

		copilotExecLog.Print("Using firewall mode with simplified arguments")
	} else {
		// Original args for non-sandbox mode
		copilotArgs = []string{"--add-dir", "/tmp/", "--add-dir", "/tmp/gh-aw/", "--add-dir", "/tmp/gh-aw/agent/", "--log-level", "all", "--log-dir", logsFolder}
		copilotExecLog.Print("Using standard mode with full arguments")
	}

	// Add --disable-builtin-mcps to disable built-in MCP servers
	copilotArgs = append(copilotArgs, "--disable-builtin-mcps")

	// Add model if specified
	// Model can be configured via:
	// 1. Explicit model in workflow config (highest priority)
	// 2. GH_AW_MODEL_AGENT_COPILOT environment variable (set via GitHub Actions variables)
	modelConfigured := workflowData.EngineConfig != nil && workflowData.EngineConfig.Model != ""
	if modelConfigured {
		copilotExecLog.Printf("Using custom model: %s", workflowData.EngineConfig.Model)
		copilotArgs = append(copilotArgs, "--model", workflowData.EngineConfig.Model)
	}

	// Add --agent flag if specified via engine.agent
	// Note: Agent imports (.github/agents/*.md) still work for importing markdown content,
	// but they do NOT automatically set the --agent flag. Only engine.agent controls the flag.
	if workflowData.EngineConfig != nil && workflowData.EngineConfig.Agent != "" {
		agentIdentifier := workflowData.EngineConfig.Agent
		copilotExecLog.Printf("Using agent from engine.agent: %s", agentIdentifier)
		copilotArgs = append(copilotArgs, "--agent", agentIdentifier)
	}

	// Add tool permission arguments based on configuration
	toolArgs := e.computeCopilotToolArguments(workflowData.Tools, workflowData.SafeOutputs, workflowData.SafeInputs, workflowData)
	if len(toolArgs) > 0 {
		copilotExecLog.Printf("Adding %d tool permission arguments", len(toolArgs))
	}
	copilotArgs = append(copilotArgs, toolArgs...)

	// if cache-memory tool is used, --add-dir for each cache
	if workflowData.CacheMemoryConfig != nil {
		for _, cache := range workflowData.CacheMemoryConfig.Caches {
			var cacheDir string
			if cache.ID == "default" {
				cacheDir = "/tmp/gh-aw/cache-memory/"
			} else {
				cacheDir = fmt.Sprintf("/tmp/gh-aw/cache-memory-%s/", cache.ID)
			}
			copilotArgs = append(copilotArgs, "--add-dir", cacheDir)
		}
	}

	// Add --allow-all-paths when edit tool is enabled to allow write on all paths
	// See: https://github.com/github/copilot-cli/issues/67#issuecomment-3411256174
	if workflowData.ParsedTools != nil && workflowData.ParsedTools.Edit != nil {
		copilotArgs = append(copilotArgs, "--allow-all-paths")
	}

	// Add custom args from engine configuration before the prompt
	if workflowData.EngineConfig != nil && len(workflowData.EngineConfig.Args) > 0 {
		copilotArgs = append(copilotArgs, workflowData.EngineConfig.Args...)
	}

	// Add --share flag to generate a markdown file of the conversation for step summary
	// The markdown file will be used to create a preview of the agent log
	shareFilePath := logsFolder + "conversation.md"
	copilotArgs = append(copilotArgs, "--share", shareFilePath)
	copilotExecLog.Printf("Added --share flag with path: %s", shareFilePath)

	// Add prompt argument - inline for sandbox modes, variable for non-sandbox
	if sandboxEnabled {
		copilotArgs = append(copilotArgs, "--prompt", "\"$(cat /tmp/gh-aw/aw-prompts/prompt.txt)\"")
	} else {
		copilotArgs = append(copilotArgs, "--prompt", "\"$COPILOT_CLI_INSTRUCTION\"")
	}

	// Extract all --add-dir paths and generate mkdir commands
	addDirPaths := extractAddDirPaths(copilotArgs)

	// Also ensure the log directory exists
	addDirPaths = append(addDirPaths, logsFolder)

	var mkdirCommands strings.Builder
	for _, dir := range addDirPaths {
		fmt.Fprintf(&mkdirCommands, "mkdir -p %s\n", dir)
	}

	// Build the copilot command
	var copilotCommand string

	// Determine if we need to conditionally add --model flag based on environment variable
	needsModelFlag := !modelConfigured
	// Check if this is a detection job (has no SafeOutputs config)
	isDetectionJob := workflowData.SafeOutputs == nil
	var modelEnvVar string
	if isDetectionJob {
		modelEnvVar = constants.EnvVarModelDetectionCopilot
	} else {
		modelEnvVar = constants.EnvVarModelAgentCopilot
	}

	// Determine which command to use (once for both sandbox and non-sandbox modes)
	var commandName string
	if workflowData.EngineConfig != nil && workflowData.EngineConfig.Command != "" {
		commandName = workflowData.EngineConfig.Command
		copilotExecLog.Printf("Using custom command: %s", commandName)
	} else if sandboxEnabled {
		// For SRT: use locally installed package without -y flag to avoid internet fetch
		// For AWF: use the installed binary directly
		if isSRTEnabled(workflowData) {
			// Use node explicitly to invoke copilot CLI to ensure env vars propagate correctly through sandbox
			// The .bin/copilot shell wrapper doesn't properly pass environment variables through bubblewrap
			// Environment variables are explicitly exported in the SRT wrapper to propagate through sandbox
			commandName = "node ./node_modules/.bin/copilot"
		} else {
			// AWF - use the copilot binary installed by the installer script
			// The binary is mounted into the AWF container from /usr/local/bin/copilot
			commandName = "/usr/local/bin/copilot"
		}
	} else {
		// Non-sandbox mode: use standard copilot command
		commandName = "copilot"
	}

	if sandboxEnabled {
		// Build base command
		baseCommand := fmt.Sprintf("%s %s", commandName, shellJoinArgs(copilotArgs))

		// Add conditional model flag if needed
		if needsModelFlag {
			copilotCommand = fmt.Sprintf(`%s${%s:+ --model "$%s"}`, baseCommand, modelEnvVar, modelEnvVar)
		} else {
			copilotCommand = baseCommand
		}
	} else {
		baseCommand := fmt.Sprintf("%s %s", commandName, shellJoinArgs(copilotArgs))

		// Add conditional model flag if needed
		if needsModelFlag {
			copilotCommand = fmt.Sprintf(`%s${%s:+ --model "$%s"}`, baseCommand, modelEnvVar, modelEnvVar)
		} else {
			copilotCommand = baseCommand
		}
	}

	// Conditionally wrap with sandbox (AWF or SRT)
	var command string
	if isSRTEnabled(workflowData) {
		// Build the SRT-wrapped command
		copilotExecLog.Print("Using Sandbox Runtime (SRT) for execution")

		agentConfig := getAgentConfig(workflowData)

		// Generate SRT config JSON
		srtConfigJSON, err := generateSRTConfigJSON(workflowData)
		if err != nil {
			copilotExecLog.Printf("Error generating SRT config: %v", err)
			// Fallback to empty config
			srtConfigJSON = "{}"
		}

		// Check if custom command is specified
		if agentConfig != nil && agentConfig.Command != "" {
			// Use custom command for SRT
			copilotExecLog.Printf("Using custom SRT command: %s", agentConfig.Command)

			// Build args list with custom args appended
			var srtArgs []string
			if len(agentConfig.Args) > 0 {
				srtArgs = append(srtArgs, agentConfig.Args...)
				copilotExecLog.Printf("Added %d custom args from agent config", len(agentConfig.Args))
			}

			// Escape the command so shell operators are passed to SRT, not interpreted by the outer shell
			escapedCommand := shellEscapeArg(copilotCommand)

			// Build the command with custom SRT command
			// The custom command should handle wrapping copilot with SRT
			command = fmt.Sprintf(`set -o pipefail
%s %s -- %s 2>&1 | tee %s`, agentConfig.Command, shellJoinArgs(srtArgs), escapedCommand, shellEscapeArg(logFile))
		} else {
			// Create the Node.js wrapper script for SRT (standard installation)
			srtWrapperScript := generateSRTWrapperScript(copilotCommand, srtConfigJSON, logFile, logsFolder)
			command = srtWrapperScript
		}
	} else if isFirewallEnabled(workflowData) {
		// Build the AWF-wrapped command - no mkdir needed, AWF handles it
		firewallConfig := getFirewallConfig(workflowData)
		agentConfig := getAgentConfig(workflowData)
		var awfLogLevel = "info"
		if firewallConfig != nil && firewallConfig.LogLevel != "" {
			awfLogLevel = firewallConfig.LogLevel
		}

		// Get allowed domains (copilot defaults + network permissions + HTTP MCP server URLs + runtime ecosystem domains)
		allowedDomains := GetCopilotAllowedDomainsWithToolsAndRuntimes(workflowData.NetworkPermissions, workflowData.Tools, workflowData.Runtimes)

		// Build AWF arguments: enable-chroot mode + standard flags + custom args from config
		// AWF v0.13.1+ chroot mode provides transparent access to host binaries and environment
		// while maintaining network isolation, eliminating the need for explicit mounts and env flags
		var awfArgs []string
		awfArgs = append(awfArgs, "--enable-chroot")
		copilotExecLog.Print("Enabled chroot mode for transparent host access")

		// Pass all environment variables to the container
		awfArgs = append(awfArgs, "--env-all")

		// Set container working directory to match GITHUB_WORKSPACE
		// This ensures pwd inside the container matches what the prompt tells the AI
		awfArgs = append(awfArgs, "--container-workdir", "\"${GITHUB_WORKSPACE}\"")
		copilotExecLog.Print("Set container working directory to GITHUB_WORKSPACE")

		// Add custom mounts from agent config if specified
		if agentConfig != nil && len(agentConfig.Mounts) > 0 {
			// Sort mounts for consistent output
			sortedMounts := make([]string, len(agentConfig.Mounts))
			copy(sortedMounts, agentConfig.Mounts)
			sort.Strings(sortedMounts)

			for _, mount := range sortedMounts {
				awfArgs = append(awfArgs, "--mount", mount)
			}
			copilotExecLog.Printf("Added %d custom mounts from agent config", len(sortedMounts))
		}

		awfArgs = append(awfArgs, "--allow-domains", allowedDomains)

		// Add blocked domains if specified
		blockedDomains := formatBlockedDomains(workflowData.NetworkPermissions)
		if blockedDomains != "" {
			awfArgs = append(awfArgs, "--block-domains", blockedDomains)
			copilotExecLog.Printf("Added blocked domains: %s", blockedDomains)
		}

		awfArgs = append(awfArgs, "--log-level", awfLogLevel)
		awfArgs = append(awfArgs, "--proxy-logs-dir", "/tmp/gh-aw/sandbox/firewall/logs")

		// Add --enable-host-access when MCP servers are configured (gateway is used)
		// This allows awf to access host.docker.internal for MCP gateway communication
		if HasMCPServers(workflowData) {
			awfArgs = append(awfArgs, "--enable-host-access")
			copilotExecLog.Print("Added --enable-host-access for MCP gateway communication")
		}

		// Pin AWF Docker image version to match the installed binary version
		awfImageTag := getAWFImageTag(firewallConfig)
		awfArgs = append(awfArgs, "--image-tag", awfImageTag)
		copilotExecLog.Printf("Pinned AWF image tag to %s", awfImageTag)

		// Skip pulling images since they are pre-downloaded in the Download container images step
		awfArgs = append(awfArgs, "--skip-pull")
		copilotExecLog.Print("Using --skip-pull since images are pre-downloaded")

		// Add SSL Bump support for HTTPS content inspection (v0.9.0+)
		sslBumpArgs := getSSLBumpArgs(firewallConfig)
		awfArgs = append(awfArgs, sslBumpArgs...)

		// Add custom args if specified in firewall config
		if firewallConfig != nil && len(firewallConfig.Args) > 0 {
			awfArgs = append(awfArgs, firewallConfig.Args...)
		}

		// Add custom args from agent config if specified
		if agentConfig != nil && len(agentConfig.Args) > 0 {
			awfArgs = append(awfArgs, agentConfig.Args...)
			copilotExecLog.Printf("Added %d custom args from agent config", len(agentConfig.Args))
		}

		// Determine the AWF command to use (custom or standard)
		var awfCommand string
		if agentConfig != nil && agentConfig.Command != "" {
			awfCommand = agentConfig.Command
			copilotExecLog.Printf("Using custom AWF command: %s", awfCommand)
		} else {
			awfCommand = "sudo -E awf"
			copilotExecLog.Print("Using standard AWF command")
		}

		// Build the full AWF command with proper argument separation
		// AWF v0.2.0 uses -- to separate AWF args from the actual command
		// The command arguments should be passed as individual shell arguments, not as a single string
		//
		// AWF with --enable-chroot and --env-all handles PATH natively:
		// 1. Captures host PATH → AWF_HOST_PATH (already has correct ordering from actions/setup-*)
		// 2. Passes ALL host env vars including JAVA_HOME, DOTNET_ROOT, GOROOT
		// 3. entrypoint.sh exports PATH="${AWF_HOST_PATH}" and tool-specific vars
		// 4. Container inherits complete, correctly-ordered environment
		//
		// Version precedence works because actions/setup-* PREPEND to PATH, so
		// /opt/hostedtoolcache/go/1.25.6/x64/bin comes before /usr/bin in AWF_HOST_PATH.

		// Escape the command for shell - copilotCommand may contain shell expansions
		escapedCommand := shellEscapeArg(copilotCommand)

		command = fmt.Sprintf(`set -o pipefail
%s %s \
  -- %s \
  2>&1 | tee %s`, awfCommand, shellJoinArgs(awfArgs), escapedCommand, shellEscapeArg(logFile))
	} else {
		// Run copilot command without AWF wrapper
		command = fmt.Sprintf(`set -o pipefail
COPILOT_CLI_INSTRUCTION="$(cat /tmp/gh-aw/aw-prompts/prompt.txt)"
%s%s 2>&1 | tee %s`, mkdirCommands.String(), copilotCommand, logFile)
	}

	// Use COPILOT_GITHUB_TOKEN
	// If github-token is specified at workflow level, use that instead
	var copilotGitHubToken string
	if workflowData.GitHubToken != "" {
		copilotGitHubToken = workflowData.GitHubToken
	} else {
		// #nosec G101 -- This is NOT a hardcoded credential. It's a GitHub Actions expression template
		// that GitHub Actions runtime replaces with the actual secret value. The string "${{ secrets.COPILOT_GITHUB_TOKEN }}"
		// is a placeholder, not an actual credential.
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

	// Always add GH_AW_PROMPT for agentic workflows
	env["GH_AW_PROMPT"] = "/tmp/gh-aw/aw-prompts/prompt.txt"

	// Add GH_AW_MCP_CONFIG for MCP server configuration only if there are MCP servers
	if HasMCPServers(workflowData) {
		env["GH_AW_MCP_CONFIG"] = "/home/runner/.copilot/mcp-config.json"
	}

	if hasGitHubTool(workflowData.ParsedTools) {
		customGitHubToken := getGitHubToken(workflowData.Tools["github"])
		// Use effective token with precedence: custom > top-level > default
		effectiveToken := getEffectiveGitHubToken(customGitHubToken, workflowData.GitHubToken)
		env["GITHUB_MCP_SERVER_TOKEN"] = effectiveToken
	}

	// Add GH_AW_SAFE_OUTPUTS if output is needed
	applySafeOutputEnvToMap(env, workflowData)

	// Add GH_AW_STARTUP_TIMEOUT environment variable (in seconds) if startup-timeout is specified
	if workflowData.ToolsStartupTimeout > 0 {
		env["GH_AW_STARTUP_TIMEOUT"] = fmt.Sprintf("%d", workflowData.ToolsStartupTimeout)
	}

	// Add GH_AW_TOOL_TIMEOUT environment variable (in seconds) if timeout is specified
	if workflowData.ToolsTimeout > 0 {
		env["GH_AW_TOOL_TIMEOUT"] = fmt.Sprintf("%d", workflowData.ToolsTimeout)
	}

	if workflowData.EngineConfig != nil && workflowData.EngineConfig.MaxTurns != "" {
		env["GH_AW_MAX_TURNS"] = workflowData.EngineConfig.MaxTurns
	}

	// Add model environment variable if model is not explicitly configured
	// This allows users to configure the default model via GitHub Actions variables
	// Use different env vars for agent vs detection jobs
	if workflowData.EngineConfig == nil || workflowData.EngineConfig.Model == "" {
		// Check if this is a detection job (has no SafeOutputs config)
		isDetectionJob := workflowData.SafeOutputs == nil
		if isDetectionJob {
			// For detection, use detection-specific env var (no builtin default, CLI will use its own)
			env[constants.EnvVarModelDetectionCopilot] = fmt.Sprintf("${{ vars.%s || '' }}", constants.EnvVarModelDetectionCopilot)
		} else {
			// For agent execution, use agent-specific env var
			env[constants.EnvVarModelAgentCopilot] = fmt.Sprintf("${{ vars.%s || '' }}", constants.EnvVarModelAgentCopilot)
		}
	}

	// Add custom environment variables from engine config
	if workflowData.EngineConfig != nil && len(workflowData.EngineConfig.Env) > 0 {
		for key, value := range workflowData.EngineConfig.Env {
			env[key] = value
		}
	}

	// Add custom environment variables from agent config
	agentConfig := getAgentConfig(workflowData)
	if agentConfig != nil && len(agentConfig.Env) > 0 {
		for key, value := range agentConfig.Env {
			env[key] = value
		}
		copilotExecLog.Printf("Added %d custom env vars from agent config", len(agentConfig.Env))
	}

	// Add HTTP MCP header secrets to env for passthrough
	headerSecrets := collectHTTPMCPHeaderSecrets(workflowData.Tools)
	for varName, secretExpr := range headerSecrets {
		// Only add if not already in env
		if _, exists := env[varName]; !exists {
			env[varName] = secretExpr
		}
	}

	// Add safe-inputs secrets to env for passthrough to MCP servers
	if IsSafeInputsEnabled(workflowData.SafeInputs, workflowData) {
		safeInputsSecrets := collectSafeInputsSecrets(workflowData.SafeInputs)
		for varName, secretExpr := range safeInputsSecrets {
			// Only add if not already in env
			if _, exists := env[varName]; !exists {
				env[varName] = secretExpr
			}
		}
	}

	// Generate the step for Copilot CLI execution
	stepName := "Execute GitHub Copilot CLI"
	var stepLines []string

	stepLines = append(stepLines, fmt.Sprintf("      - name: %s", stepName))
	stepLines = append(stepLines, "        id: agentic_execution")

	// Add tool arguments comment before the run section
	toolArgsComment := e.generateCopilotToolArgumentsComment(workflowData.Tools, workflowData.SafeOutputs, workflowData.SafeInputs, workflowData, "        ")
	if toolArgsComment != "" {
		// Split the comment into lines and add each line
		commentLines := strings.Split(strings.TrimSuffix(toolArgsComment, "\n"), "\n")
		stepLines = append(stepLines, commentLines...)
	}

	// Add timeout at step level (GitHub Actions standard)
	if workflowData.TimeoutMinutes != "" {
		// Strip timeout-minutes prefix
		timeoutValue := strings.TrimPrefix(workflowData.TimeoutMinutes, "timeout-minutes: ")
		stepLines = append(stepLines, fmt.Sprintf("        timeout-minutes: %s", timeoutValue))
	} else {
		stepLines = append(stepLines, fmt.Sprintf("        timeout-minutes: %d", int(constants.DefaultAgenticWorkflowTimeout/time.Minute))) // Default timeout for agentic workflows
	}

	// Filter environment variables to only include allowed secrets
	// This is a security measure to prevent exposing unnecessary secrets to the AWF container
	allowedSecrets := e.GetRequiredSecretNames(workflowData)
	filteredEnv := FilterEnvForSecrets(env, allowedSecrets)

	// Format step with command and filtered environment variables using shared helper
	stepLines = FormatStepWithCommandAndEnv(stepLines, command, filteredEnv)

	steps = append(steps, GitHubActionStep(stepLines))

	return steps
}

// GetFirewallLogsCollectionStep returns the step for collecting firewall logs (before secret redaction)
