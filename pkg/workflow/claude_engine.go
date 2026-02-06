package workflow

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

var claudeLog = logger.New("workflow:claude_engine")

// ClaudeEngine represents the Claude Code agentic engine
type ClaudeEngine struct {
	BaseEngine
}

func NewClaudeEngine() *ClaudeEngine {
	return &ClaudeEngine{
		BaseEngine: BaseEngine{
			id:                     "claude",
			displayName:            "Claude Code",
			description:            "Uses Claude Code with full MCP tool support and allow-listing",
			experimental:           false,
			supportsToolsAllowlist: true,
			supportsHTTPTransport:  true, // Claude supports both stdio and HTTP transport
			supportsMaxTurns:       true, // Claude supports max-turns feature
			supportsWebFetch:       true, // Claude has built-in WebFetch support
			supportsWebSearch:      true, // Claude has built-in WebSearch support
			supportsFirewall:       true, // Claude supports network firewalling via AWF
		},
	}
}

// GetRequiredSecretNames returns the list of secrets required by the Claude engine
// This includes ANTHROPIC_API_KEY, CLAUDE_CODE_OAUTH_TOKEN, and optionally MCP_GATEWAY_API_KEY
func (e *ClaudeEngine) GetRequiredSecretNames(workflowData *WorkflowData) []string {
	secrets := []string{"ANTHROPIC_API_KEY", "CLAUDE_CODE_OAUTH_TOKEN"}

	// Add MCP gateway API key if MCP servers are present (gateway is always started with MCP servers)
	if HasMCPServers(workflowData) {
		secrets = append(secrets, "MCP_GATEWAY_API_KEY")
	}

	// Add safe-inputs secret names
	if IsSafeInputsEnabled(workflowData.SafeInputs, workflowData) {
		safeInputsSecrets := collectSafeInputsSecrets(workflowData.SafeInputs)
		for varName := range safeInputsSecrets {
			secrets = append(secrets, varName)
		}
	}

	return secrets
}

func (e *ClaudeEngine) GetInstallationSteps(workflowData *WorkflowData) []GitHubActionStep {
	claudeLog.Printf("Generating installation steps for Claude engine: workflow=%s", workflowData.Name)

	// Skip installation if custom command is specified
	if workflowData.EngineConfig != nil && workflowData.EngineConfig.Command != "" {
		claudeLog.Printf("Skipping installation steps: custom command specified (%s)", workflowData.EngineConfig.Command)
		return []GitHubActionStep{}
	}

	var steps []GitHubActionStep

	// Define engine configuration for shared validation
	config := EngineInstallConfig{
		Secrets:         []string{"CLAUDE_CODE_OAUTH_TOKEN", "ANTHROPIC_API_KEY"},
		DocsURL:         "https://github.github.com/gh-aw/reference/engines/#anthropic-claude-code",
		NpmPackage:      "@anthropic-ai/claude-code",
		Version:         string(constants.DefaultClaudeCodeVersion),
		Name:            "Claude Code",
		CliName:         "claude",
		InstallStepName: "Install Claude Code CLI",
	}

	// Add secret validation step
	secretValidation := GenerateMultiSecretValidationStep(
		config.Secrets,
		config.Name,
		config.DocsURL,
	)
	steps = append(steps, secretValidation)

	// Determine Claude version
	claudeVersion := config.Version
	if workflowData.EngineConfig != nil && workflowData.EngineConfig.Version != "" {
		claudeVersion = workflowData.EngineConfig.Version
	}

	// Add Node.js setup step first (before sandbox installation)
	npmSteps := GenerateNpmInstallSteps(
		config.NpmPackage,
		claudeVersion,
		config.InstallStepName,
		config.CliName,
		true, // Include Node.js setup
	)

	if len(npmSteps) > 0 {
		steps = append(steps, npmSteps[0]) // Setup Node.js step
	}

	// Add AWF installation if firewall is enabled
	if isFirewallEnabled(workflowData) {
		// Install AWF after Node.js setup but before Claude CLI installation
		firewallConfig := getFirewallConfig(workflowData)
		agentConfig := getAgentConfig(workflowData)
		var awfVersion string
		if firewallConfig != nil {
			awfVersion = firewallConfig.Version
		}

		// Install AWF binary (or skip if custom command is specified)
		awfInstall := generateAWFInstallationStep(awfVersion, agentConfig)
		if len(awfInstall) > 0 {
			steps = append(steps, awfInstall)
		}
	}

	// Add Claude CLI installation step after sandbox installation
	if len(npmSteps) > 1 {
		steps = append(steps, npmSteps[1:]...) // Install Claude CLI and subsequent steps
	}

	return steps
}

// GetDeclaredOutputFiles returns the output files that Claude may produce
func (e *ClaudeEngine) GetDeclaredOutputFiles() []string {
	return []string{}
}

// GetExecutionSteps returns the GitHub Actions steps for executing Claude
func (e *ClaudeEngine) GetExecutionSteps(workflowData *WorkflowData, logFile string) []GitHubActionStep {
	claudeLog.Printf("Generating execution steps for Claude engine: workflow=%s, firewall=%v", workflowData.Name, isFirewallEnabled(workflowData))

	// Handle custom steps if they exist in engine config
	steps := InjectCustomEngineSteps(workflowData, e.convertStepToYAML)

	// Build claude CLI arguments based on configuration
	var claudeArgs []string

	// Add print flag for non-interactive mode
	claudeArgs = append(claudeArgs, "--print")

	// Disable slash commands for controlled execution
	claudeArgs = append(claudeArgs, "--disable-slash-commands")

	// Disable Chrome integration for security and deterministic execution
	claudeArgs = append(claudeArgs, "--no-chrome")

	// Add model if specified
	// Model can be configured via:
	// 1. Explicit model in workflow config (highest priority)
	// 2. GH_AW_MODEL_AGENT_CLAUDE environment variable (set via GitHub Actions variables)
	modelConfigured := workflowData.EngineConfig != nil && workflowData.EngineConfig.Model != ""
	if modelConfigured {
		claudeLog.Printf("Using custom model: %s", workflowData.EngineConfig.Model)
		claudeArgs = append(claudeArgs, "--model", workflowData.EngineConfig.Model)
	}

	// Add max_turns if specified (in CLI it's max-turns)
	if workflowData.EngineConfig != nil && workflowData.EngineConfig.MaxTurns != "" {
		claudeLog.Printf("Setting max turns: %s", workflowData.EngineConfig.MaxTurns)
		claudeArgs = append(claudeArgs, "--max-turns", workflowData.EngineConfig.MaxTurns)
	}

	// Add MCP configuration only if there are MCP servers
	if HasMCPServers(workflowData) {
		claudeLog.Print("Adding MCP configuration")
		claudeArgs = append(claudeArgs, "--mcp-config", "/tmp/gh-aw/mcp-config/mcp-servers.json")
	}

	// Add allowed tools configuration
	// Note: Claude Code CLI v2.0.31 introduced a simpler --tools flag, but we continue to use
	// --allowed-tools because it provides fine-grained control needed by gh-aw:
	// - Specific bash commands: Bash(git:*), Bash(ls)
	// - MCP tool prefixes: mcp__github__issue_read
	// - Path-specific tools: Read(/tmp/gh-aw/cache-memory/*)
	// The --tools flag only supports basic tool names (e.g., "Bash,Edit,Read") without patterns.
	allowedTools := e.computeAllowedClaudeToolsString(workflowData.Tools, workflowData.SafeOutputs, workflowData.CacheMemoryConfig)
	if allowedTools != "" {
		claudeArgs = append(claudeArgs, "--allowed-tools", allowedTools)
	}

	// Add debug-file flag to write debug logs directly to file
	// This implicitly enables debug mode and provides cleaner, more reliable log capture
	// than shell redirection with 2>&1 | tee
	claudeArgs = append(claudeArgs, "--debug-file", logFile)

	// Always add verbose flag for enhanced debugging output
	claudeArgs = append(claudeArgs, "--verbose")

	// Add permission mode for non-interactive execution (bypass permissions)
	claudeArgs = append(claudeArgs, "--permission-mode", "bypassPermissions")

	// Add output format for structured output
	// Use "stream-json" to output JSONL format (newline-delimited JSON objects)
	// This format is compatible with the log parser which expects either JSON array or JSONL
	claudeArgs = append(claudeArgs, "--output-format", "stream-json")

	// Add custom args from engine configuration before the prompt
	if workflowData.EngineConfig != nil && len(workflowData.EngineConfig.Args) > 0 {
		claudeArgs = append(claudeArgs, workflowData.EngineConfig.Args...)
	}

	// Build the agent command - prepend custom agent file content if specified (via imports)
	var promptSetup string
	var promptCommand string
	if workflowData.AgentFile != "" {
		agentPath := ResolveAgentFilePath(workflowData.AgentFile)
		claudeLog.Printf("Using custom agent file: %s", workflowData.AgentFile)
		// Extract markdown body from custom agent file and prepend to prompt
		promptSetup = fmt.Sprintf(`# Extract markdown body from custom agent file (skip frontmatter)
          AGENT_CONTENT="$(awk 'BEGIN{skip=1} /^---$/{if(skip){skip=0;next}else{skip=1;next}} !skip' %s)"
          # Combine agent content with prompt
          PROMPT_TEXT="$(printf '%%s\n\n%%s' "$AGENT_CONTENT" "$(cat /tmp/gh-aw/aw-prompts/prompt.txt)")"`, agentPath)
		promptCommand = "\"$PROMPT_TEXT\""
	} else {
		promptCommand = "\"$(cat /tmp/gh-aw/aw-prompts/prompt.txt)\""
	}

	// Build the command string with proper argument formatting
	// Determine which command to use
	var commandName string
	if workflowData.EngineConfig != nil && workflowData.EngineConfig.Command != "" {
		commandName = workflowData.EngineConfig.Command
		claudeLog.Printf("Using custom command: %s", commandName)
	} else {
		// Use regular claude command - PATH is inherited via --env-all in AWF mode
		commandName = "claude"
	}

	commandParts := []string{commandName}
	commandParts = append(commandParts, claudeArgs...)
	commandParts = append(commandParts, promptCommand)

	// Join command parts with proper escaping using shellJoinArgs helper
	// This handles already-quoted arguments correctly and prevents double-escaping
	claudeCommand := shellJoinArgs(commandParts)

	// Add conditional model flag if not explicitly configured
	// Check if this is a detection job (has no SafeOutputs config)
	isDetectionJob := workflowData.SafeOutputs == nil
	var modelEnvVar string
	if isDetectionJob {
		modelEnvVar = constants.EnvVarModelDetectionClaude
	} else {
		modelEnvVar = constants.EnvVarModelAgentClaude
	}
	if !modelConfigured {
		claudeCommand = fmt.Sprintf(`%s${%s:+ --model "$%s"}`, claudeCommand, modelEnvVar, modelEnvVar)
	}

	// Build the full command based on whether firewall is enabled
	var command string
	if isFirewallEnabled(workflowData) {
		// Build the AWF-wrapped command
		firewallConfig := getFirewallConfig(workflowData)
		agentConfig := getAgentConfig(workflowData)
		var awfLogLevel = "info"
		if firewallConfig != nil && firewallConfig.LogLevel != "" {
			awfLogLevel = firewallConfig.LogLevel
		}

		// Get allowed domains (Claude defaults + network permissions + HTTP MCP server URLs + runtime ecosystem domains)
		allowedDomains := GetClaudeAllowedDomainsWithToolsAndRuntimes(workflowData.NetworkPermissions, workflowData.Tools, workflowData.Runtimes)

		// Build AWF arguments: enable-chroot mode + standard flags + custom args from config
		// AWF v0.13.1+ chroot mode provides transparent access to host binaries and environment
		// while maintaining network isolation, eliminating the need for explicit mounts and env flags
		var awfArgs []string
		awfArgs = append(awfArgs, "--enable-chroot")
		claudeLog.Print("Enabled chroot mode for transparent host access")

		// TTY is required for Claude Code CLI
		awfArgs = append(awfArgs, "--tty")

		// Pass all environment variables to the container
		awfArgs = append(awfArgs, "--env-all")

		// Set container working directory to match GITHUB_WORKSPACE
		// This ensures pwd inside the container matches what the prompt tells the AI
		awfArgs = append(awfArgs, "--container-workdir", "\"${GITHUB_WORKSPACE}\"")
		claudeLog.Print("Set container working directory to GITHUB_WORKSPACE")

		// Add custom mounts from agent config if specified
		if agentConfig != nil && len(agentConfig.Mounts) > 0 {
			// Sort mounts for consistent output
			sortedMounts := make([]string, len(agentConfig.Mounts))
			copy(sortedMounts, agentConfig.Mounts)
			sort.Strings(sortedMounts)

			for _, mount := range sortedMounts {
				awfArgs = append(awfArgs, "--mount", mount)
			}
			claudeLog.Printf("Added %d custom mounts from agent config", len(sortedMounts))
		}

		awfArgs = append(awfArgs, "--allow-domains", allowedDomains)

		// Add blocked domains if specified
		blockedDomains := formatBlockedDomains(workflowData.NetworkPermissions)
		if blockedDomains != "" {
			awfArgs = append(awfArgs, "--block-domains", blockedDomains)
			claudeLog.Printf("Added blocked domains: %s", blockedDomains)
		}

		awfArgs = append(awfArgs, "--log-level", awfLogLevel)
		awfArgs = append(awfArgs, "--proxy-logs-dir", "/tmp/gh-aw/sandbox/firewall/logs")

		// Add --enable-host-access when MCP servers are configured (gateway is used)
		// This allows awf to access host.docker.internal for MCP gateway communication
		if HasMCPServers(workflowData) {
			awfArgs = append(awfArgs, "--enable-host-access")
			claudeLog.Print("Added --enable-host-access for MCP gateway communication")
		}

		// Pin AWF Docker image version to match the installed binary version
		awfImageTag := getAWFImageTag(firewallConfig)
		awfArgs = append(awfArgs, "--image-tag", awfImageTag)
		claudeLog.Printf("Pinned AWF image tag to %s", awfImageTag)

		// Skip pulling images since they are pre-downloaded in the Download container images step
		awfArgs = append(awfArgs, "--skip-pull")
		claudeLog.Print("Using --skip-pull since images are pre-downloaded")

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
			claudeLog.Printf("Added %d custom args from agent config", len(agentConfig.Args))
		}

		// Determine the AWF command to use (custom or standard)
		var awfCommand string
		if agentConfig != nil && agentConfig.Command != "" {
			awfCommand = agentConfig.Command
			claudeLog.Printf("Using custom AWF command: %s", awfCommand)
		} else {
			awfCommand = "sudo -E awf"
			claudeLog.Print("Using standard AWF command")
		}

		// Build the command with AWF wrapper
		//
		// AWF with --enable-chroot and --env-all handles most PATH setup natively:
		// - GOROOT, JAVA_HOME, etc. are handled via AWF_HOST_PATH and entrypoint.sh
		// However, npm-installed CLIs (like claude) need hostedtoolcache bin directories in PATH.
		//
		// AWF requires the command to be wrapped in a shell invocation because the claude command
		// contains && chains that need shell interpretation. We use bash -c with properly escaped command.
		// Add PATH setup to find npm-installed binaries in hostedtoolcache
		npmPathSetup := GetNpmBinPathSetup()
		claudeCommandWithPath := fmt.Sprintf(`%s && %s`, npmPathSetup, claudeCommand)
		// Escape single quotes in the command by replacing ' with '\''
		escapedClaudeCommand := strings.ReplaceAll(claudeCommandWithPath, "'", "'\\''")
		shellWrappedCommand := fmt.Sprintf("/bin/bash -c '%s'", escapedClaudeCommand)

		// Note: Claude Code CLI writes debug logs to --debug-file and JSON output to stdout
		// Use tee to capture stdout (stream-json output) to the log file while also displaying on console
		// The combined output (debug logs + JSON) will be in the log file for parsing
		if promptSetup != "" {
			command = fmt.Sprintf(`set -o pipefail
          %s
%s %s \
  -- %s 2>&1 | tee -a %s`, promptSetup, awfCommand, shellJoinArgs(awfArgs), shellWrappedCommand, logFile)
		} else {
			command = fmt.Sprintf(`set -o pipefail
%s %s \
  -- %s 2>&1 | tee -a %s`, awfCommand, shellJoinArgs(awfArgs), shellWrappedCommand, logFile)
		}
	} else {
		// Run Claude command without AWF wrapper
		// Note: Claude Code CLI writes debug logs to --debug-file and JSON output to stdout
		// Use tee to capture stdout (stream-json output) to the log file while also displaying on console
		// The combined output (debug logs + JSON) will be in the log file for parsing
		// PATH is already set correctly by actions/setup-* steps which prepend to PATH
		if promptSetup != "" {
			command = fmt.Sprintf(`set -o pipefail
          %s
          # Execute Claude Code CLI with prompt from file
          %s 2>&1 | tee -a %s`, promptSetup, claudeCommand, logFile)
		} else {
			command = fmt.Sprintf(`set -o pipefail
          # Execute Claude Code CLI with prompt from file
          %s 2>&1 | tee -a %s`, claudeCommand, logFile)
		}
	}

	// Build environment variables map
	env := map[string]string{
		"ANTHROPIC_API_KEY":       "${{ secrets.ANTHROPIC_API_KEY }}",
		"CLAUDE_CODE_OAUTH_TOKEN": "${{ secrets.CLAUDE_CODE_OAUTH_TOKEN }}",
		"DISABLE_TELEMETRY":       "1",
		"DISABLE_ERROR_REPORTING": "1",
		"DISABLE_BUG_COMMAND":     "1",
		"GH_AW_PROMPT":            "/tmp/gh-aw/aw-prompts/prompt.txt",
		"GITHUB_WORKSPACE":        "${{ github.workspace }}",
	}

	// Add GH_AW_MCP_CONFIG for MCP server configuration only if there are MCP servers
	if HasMCPServers(workflowData) {
		env["GH_AW_MCP_CONFIG"] = "/tmp/gh-aw/mcp-config/mcp-servers.json"
	}

	// Set timeout environment variables for Claude Code
	// Use tools.startup-timeout if specified, otherwise default to DefaultMCPStartupTimeout
	startupTimeoutMs := int(constants.DefaultMCPStartupTimeout / time.Millisecond)
	if workflowData.ToolsStartupTimeout > 0 {
		startupTimeoutMs = workflowData.ToolsStartupTimeout * 1000 // convert seconds to milliseconds
	}

	// Use tools.timeout if specified, otherwise default to DefaultToolTimeout
	timeoutMs := int(constants.DefaultToolTimeout / time.Millisecond)
	if workflowData.ToolsTimeout > 0 {
		timeoutMs = workflowData.ToolsTimeout * 1000 // convert seconds to milliseconds
	}

	env["MCP_TIMEOUT"] = fmt.Sprintf("%d", startupTimeoutMs)
	env["MCP_TOOL_TIMEOUT"] = fmt.Sprintf("%d", timeoutMs)
	env["BASH_DEFAULT_TIMEOUT_MS"] = fmt.Sprintf("%d", timeoutMs)
	env["BASH_MAX_TIMEOUT_MS"] = fmt.Sprintf("%d", timeoutMs)

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
	if !modelConfigured {
		if isDetectionJob {
			// For detection, use detection-specific env var (no default fallback for Claude)
			env[constants.EnvVarModelDetectionClaude] = fmt.Sprintf("${{ vars.%s || '' }}", constants.EnvVarModelDetectionClaude)
		} else {
			// For agent execution, use agent-specific env var
			env[constants.EnvVarModelAgentClaude] = fmt.Sprintf("${{ vars.%s || '' }}", constants.EnvVarModelAgentClaude)
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
		claudeLog.Printf("Added %d custom env vars from agent config", len(agentConfig.Env))
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

	// Generate the step for Claude CLI execution
	stepName := "Execute Claude Code CLI"
	var stepLines []string

	stepLines = append(stepLines, fmt.Sprintf("      - name: %s", stepName))
	stepLines = append(stepLines, "        id: agentic_execution")

	// Add allowed tools comment before the run section
	allowedToolsComment := e.generateAllowedToolsComment(e.computeAllowedClaudeToolsString(workflowData.Tools, workflowData.SafeOutputs, workflowData.CacheMemoryConfig), "        ")
	if allowedToolsComment != "" {
		// Split the comment into lines and add each line
		commentLines := strings.Split(strings.TrimSuffix(allowedToolsComment, "\n"), "\n")
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

// GetLogParserScriptId returns the JavaScript script name for parsing Claude logs
func (e *ClaudeEngine) GetLogParserScriptId() string {
	return "parse_claude_log"
}

// GetFirewallLogsCollectionStep returns the step for collecting firewall logs (before secret redaction)
// No longer needed since we know where the logs are in the sandbox folder structure
func (e *ClaudeEngine) GetFirewallLogsCollectionStep(workflowData *WorkflowData) []GitHubActionStep {
	// Collection step removed - firewall logs are now at a known location
	return []GitHubActionStep{}
}

// GetSquidLogsSteps returns the steps for uploading and parsing Squid logs (after secret redaction)
func (e *ClaudeEngine) GetSquidLogsSteps(workflowData *WorkflowData) []GitHubActionStep {
	var steps []GitHubActionStep

	// Only add upload and parsing steps if firewall is enabled
	if isFirewallEnabled(workflowData) {
		claudeLog.Printf("Adding Squid logs upload and parsing steps for workflow: %s", workflowData.Name)

		squidLogsUpload := generateSquidLogsUploadStep(workflowData.Name)
		steps = append(steps, squidLogsUpload)

		// Add firewall log parsing step to create step summary
		firewallLogParsing := generateFirewallLogParsingStep(workflowData.Name)
		steps = append(steps, firewallLogParsing)
	} else {
		claudeLog.Print("Firewall disabled, skipping Squid logs upload")
	}

	return steps
}
