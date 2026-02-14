package workflow

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

var codexEngineLog = logger.New("workflow:codex_engine")

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

func NewCodexEngine() *CodexEngine {
	return &CodexEngine{
		BaseEngine: BaseEngine{
			id:                     "codex",
			displayName:            "Codex",
			description:            "Uses OpenAI Codex CLI with MCP server support",
			experimental:           false,
			supportsToolsAllowlist: true,
			supportsHTTPTransport:  true,  // Codex now supports HTTP transport for remote MCP servers
			supportsMaxTurns:       false, // Codex does not support max-turns feature
			supportsWebFetch:       false, // Codex does not have built-in web-fetch support
			supportsWebSearch:      true,  // Codex has built-in web-search support
			supportsFirewall:       true,  // Codex supports network firewalling via AWF
			supportsLLMGateway:     false, // Codex does not support LLM gateway
		},
	}
}

// SupportsLLMGateway returns the LLM gateway port for Codex engine
func (e *CodexEngine) SupportsLLMGateway() int {
	return 10001 // Codex uses port 10001 for LLM gateway
}

// GetRequiredSecretNames returns the list of secrets required by the Codex engine
// This includes CODEX_API_KEY, OPENAI_API_KEY, and optionally MCP_GATEWAY_API_KEY
func (e *CodexEngine) GetRequiredSecretNames(workflowData *WorkflowData) []string {
	secrets := []string{"CODEX_API_KEY", "OPENAI_API_KEY"}

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

func (e *CodexEngine) GetInstallationSteps(workflowData *WorkflowData) []GitHubActionStep {
	codexEngineLog.Printf("Generating installation steps for Codex engine: workflow=%s", workflowData.Name)

	// Skip installation if custom command is specified
	if workflowData.EngineConfig != nil && workflowData.EngineConfig.Command != "" {
		codexEngineLog.Printf("Skipping installation steps: custom command specified (%s)", workflowData.EngineConfig.Command)
		return []GitHubActionStep{}
	}

	// Use base installation steps (secret validation + npm install)
	steps := GetBaseInstallationSteps(EngineInstallConfig{
		Secrets:    []string{"CODEX_API_KEY", "OPENAI_API_KEY"},
		DocsURL:    "https://github.github.com/gh-aw/reference/engines/#openai-codex",
		NpmPackage: "@openai/codex",
		Version:    string(constants.DefaultCodexVersion),
		Name:       "Codex",
		CliName:    "codex",
	}, workflowData)

	// Add AWF installation step if firewall is enabled
	if isFirewallEnabled(workflowData) {
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

	return steps
}

// GetDeclaredOutputFiles returns the output files that Codex may produce
// Codex (written in Rust) writes logs to ~/.codex/log/codex-tui.log
func (e *CodexEngine) GetDeclaredOutputFiles() []string {
	// Return the Codex log directory for artifact collection
	// Using mcp-config folder structure for consistency with other engines
	return []string{
		"/tmp/gh-aw/mcp-config/logs/",
	}
}

// GetExecutionSteps returns the GitHub Actions steps for executing Codex
func (e *CodexEngine) GetExecutionSteps(workflowData *WorkflowData, logFile string) []GitHubActionStep {
	modelConfigured := workflowData.EngineConfig != nil && workflowData.EngineConfig.Model != ""
	model := ""
	if modelConfigured {
		model = workflowData.EngineConfig.Model
	}
	firewallEnabled := isFirewallEnabled(workflowData)
	codexEngineLog.Printf("Building Codex execution steps: workflow=%s, model=%s, has_agent_file=%v, firewall=%v",
		workflowData.Name, model, workflowData.AgentFile != "", firewallEnabled)

	// Handle custom steps if they exist in engine config
	steps := InjectCustomEngineSteps(workflowData, e.convertStepToYAML)

	// Build model parameter only if specified in engineConfig
	// Otherwise, model can be set via GH_AW_MODEL_AGENT_CODEX or GH_AW_MODEL_DETECTION_CODEX environment variable
	var modelParam string
	if modelConfigured {
		modelParam = fmt.Sprintf("-c model=%s ", workflowData.EngineConfig.Model)
	} else {
		// Check if this is a detection job (has no SafeOutputs config)
		isDetectionJob := workflowData.SafeOutputs == nil
		var modelEnvVar string
		if isDetectionJob {
			modelEnvVar = constants.EnvVarModelDetectionCodex
		} else {
			modelEnvVar = constants.EnvVarModelAgentCodex
		}
		// Model will be conditionally added via shell expansion if environment variable is set
		modelParam = fmt.Sprintf(`${%s:+-c model="$%s" }`, modelEnvVar, modelEnvVar)
	}

	// Build search parameter if web-search tool is present
	webSearchParam := ""
	if workflowData.ParsedTools != nil && workflowData.ParsedTools.WebSearch != nil {
		webSearchParam = "--search "
	}

	// See https://github.com/github/gh-aw/issues/892
	// --dangerously-bypass-approvals-and-sandbox: Skips all confirmation prompts and disables sandboxing
	// This is safe because AWF already provides a container-level sandbox layer
	// --skip-git-repo-check: Allows running in directories without a git repo
	fullAutoParam := " --dangerously-bypass-approvals-and-sandbox --skip-git-repo-check "

	// Build custom args parameter if specified in engineConfig
	var customArgsParam string
	if workflowData.EngineConfig != nil && len(workflowData.EngineConfig.Args) > 0 {
		for _, arg := range workflowData.EngineConfig.Args {
			customArgsParam += arg + " "
		}
	}

	// Build the Codex command
	// Determine which command to use
	var commandName string
	if workflowData.EngineConfig != nil && workflowData.EngineConfig.Command != "" {
		commandName = workflowData.EngineConfig.Command
		codexEngineLog.Printf("Using custom command: %s", commandName)
	} else {
		// Use regular codex command - PATH is inherited via --env-all in AWF mode
		commandName = "codex"
	}

	codexCommand := fmt.Sprintf("%s %sexec%s%s%s\"$INSTRUCTION\"",
		commandName, modelParam, webSearchParam, fullAutoParam, customArgsParam)

	// Build the full command with agent file handling and AWF wrapping if enabled
	var command string
	if firewallEnabled {
		// Build AWF-wrapped command
		firewallConfig := getFirewallConfig(workflowData)
		agentConfig := getAgentConfig(workflowData)
		var awfLogLevel = "info"
		if firewallConfig != nil && firewallConfig.LogLevel != "" {
			awfLogLevel = firewallConfig.LogLevel
		}

		// Get allowed domains (Codex defaults + network permissions + HTTP MCP server URLs + runtime ecosystem domains)
		allowedDomains := GetCodexAllowedDomainsWithToolsAndRuntimes(workflowData.NetworkPermissions, workflowData.Tools, workflowData.Runtimes)

		// Build AWF arguments: standard flags + custom args from config
		// AWF v0.15.0+ uses chroot mode by default, providing transparent access to host binaries
		// and environment while maintaining network isolation
		var awfArgs []string

		// Pass all environment variables to the container
		awfArgs = append(awfArgs, "--env-all")

		// Set container working directory to match GITHUB_WORKSPACE
		awfArgs = append(awfArgs, "--container-workdir", "\"${GITHUB_WORKSPACE}\"")
		codexEngineLog.Print("Set container working directory to GITHUB_WORKSPACE")

		// Add custom mounts from agent config if specified
		if agentConfig != nil && len(agentConfig.Mounts) > 0 {
			sortedMounts := make([]string, len(agentConfig.Mounts))
			copy(sortedMounts, agentConfig.Mounts)
			sort.Strings(sortedMounts)

			for _, mount := range sortedMounts {
				awfArgs = append(awfArgs, "--mount", mount)
			}
			codexEngineLog.Printf("Added %d custom mounts from agent config", len(sortedMounts))
		}

		awfArgs = append(awfArgs, "--allow-domains", allowedDomains)

		// Add blocked domains if specified
		blockedDomains := formatBlockedDomains(workflowData.NetworkPermissions)
		if blockedDomains != "" {
			awfArgs = append(awfArgs, "--block-domains", blockedDomains)
			codexEngineLog.Printf("Added blocked domains: %s", blockedDomains)
		}

		awfArgs = append(awfArgs, "--log-level", awfLogLevel)
		awfArgs = append(awfArgs, "--proxy-logs-dir", "/tmp/gh-aw/sandbox/firewall/logs")

		// Add --enable-host-access when MCP servers are configured (gateway is used)
		// This allows awf to access host.docker.internal for MCP gateway communication
		if HasMCPServers(workflowData) {
			awfArgs = append(awfArgs, "--enable-host-access")
			codexEngineLog.Print("Added --enable-host-access for MCP gateway communication")
		}

		// Pin AWF Docker image version to match the installed binary version
		awfImageTag := getAWFImageTag(firewallConfig)
		awfArgs = append(awfArgs, "--image-tag", awfImageTag)
		codexEngineLog.Printf("Pinned AWF image tag to %s", awfImageTag)

		// Skip pulling images since they are pre-downloaded in the Download container images step
		awfArgs = append(awfArgs, "--skip-pull")
		codexEngineLog.Print("Using --skip-pull since images are pre-downloaded")

		// Enable API proxy sidecar if this engine supports LLM gateway
		// The api-proxy container holds the LLM API keys and proxies requests through the firewall
		llmGatewayPort := e.SupportsLLMGateway()
		if llmGatewayPort > 0 {
			awfArgs = append(awfArgs, "--enable-api-proxy")
			codexEngineLog.Printf("Added --enable-api-proxy for LLM API proxying on port %d", llmGatewayPort)
		}

		// Note: No --tty flag for Codex (it's not a TUI, it outputs to stdout/stderr)

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
			codexEngineLog.Printf("Added %d custom args from agent config", len(agentConfig.Args))
		}

		// Determine the AWF command to use (custom or standard)
		var awfCommand string
		if agentConfig != nil && agentConfig.Command != "" {
			awfCommand = agentConfig.Command
			codexEngineLog.Printf("Using custom AWF command: %s", awfCommand)
		} else {
			awfCommand = "sudo -E awf"
			codexEngineLog.Print("Using standard AWF command")
		}

		// Build the command with agent file handling if specified
		// INSTRUCTION reading is done inside the AWF command to avoid Docker Compose interpolation
		// issues with $ characters in the prompt.
		//
		// AWF v0.15.0+ with --env-all handles most PATH setup natively (chroot mode is default):
		// - GOROOT, JAVA_HOME, etc. are handled via AWF_HOST_PATH and entrypoint.sh
		// However, npm-installed CLIs (like codex) need hostedtoolcache bin directories in PATH.
		npmPathSetup := GetNpmBinPathSetup()

		if workflowData.AgentFile != "" {
			agentPath := ResolveAgentFilePath(workflowData.AgentFile)
			// Read agent file and prompt inside AWF container, with PATH setup for npm binaries
			codexCommandWithSetup := fmt.Sprintf(`%s && AGENT_CONTENT="$(awk 'BEGIN{skip=1} /^---$/{if(skip){skip=0;next}else{skip=1;next}} !skip' %s)" && INSTRUCTION="$(printf "%%s\n\n%%s" "$AGENT_CONTENT" "$(cat /tmp/gh-aw/aw-prompts/prompt.txt)")" && %s`, npmPathSetup, agentPath, codexCommand)
			escapedCodexCommand := strings.ReplaceAll(codexCommandWithSetup, "'", "'\\''")
			shellWrappedCommand := fmt.Sprintf("/bin/bash -c '%s'", escapedCodexCommand)

			command = fmt.Sprintf(`set -o pipefail
mkdir -p "$CODEX_HOME/logs"
%s %s \
  -- %s \
  2>&1 | tee %s`, awfCommand, shellJoinArgs(awfArgs), shellWrappedCommand, shellEscapeArg(logFile))
		} else {
			// Read prompt inside AWF container to avoid Docker Compose interpolation issues, with PATH setup
			codexCommandWithSetup := fmt.Sprintf(`%s && INSTRUCTION="$(cat /tmp/gh-aw/aw-prompts/prompt.txt)" && %s`, npmPathSetup, codexCommand)
			escapedCodexCommand := strings.ReplaceAll(codexCommandWithSetup, "'", "'\\''")
			shellWrappedCommand := fmt.Sprintf("/bin/bash -c '%s'", escapedCodexCommand)

			command = fmt.Sprintf(`set -o pipefail
mkdir -p "$CODEX_HOME/logs"
%s %s \
  -- %s \
  2>&1 | tee %s`, awfCommand, shellJoinArgs(awfArgs), shellWrappedCommand, shellEscapeArg(logFile))
		}
	} else {
		// Build the command without AWF wrapping
		// Reuse commandName already determined above
		if workflowData.AgentFile != "" {
			agentPath := ResolveAgentFilePath(workflowData.AgentFile)
			command = fmt.Sprintf(`set -o pipefail
AGENT_CONTENT="$(awk 'BEGIN{skip=1} /^---$/{if(skip){skip=0;next}else{skip=1;next}} !skip' %s)"
INSTRUCTION="$(printf "%%s\n\n%%s" "$AGENT_CONTENT" "$(cat "$GH_AW_PROMPT")")"
mkdir -p "$CODEX_HOME/logs"
%s %sexec%s%s%s"$INSTRUCTION" 2>&1 | tee %s`, agentPath, commandName, modelParam, webSearchParam, fullAutoParam, customArgsParam, logFile)
		} else {
			command = fmt.Sprintf(`set -o pipefail
INSTRUCTION="$(cat "$GH_AW_PROMPT")"
mkdir -p "$CODEX_HOME/logs"
%s %sexec%s%s%s"$INSTRUCTION" 2>&1 | tee %s`, commandName, modelParam, webSearchParam, fullAutoParam, customArgsParam, logFile)
		}
	}

	// Get effective GitHub token based on precedence: top-level github-token > default
	effectiveGitHubToken := getEffectiveGitHubToken("", workflowData.GitHubToken)

	env := map[string]string{
		"CODEX_API_KEY":                "${{ secrets.CODEX_API_KEY || secrets.OPENAI_API_KEY }}",
		"GITHUB_STEP_SUMMARY":          "${{ env.GITHUB_STEP_SUMMARY }}",
		"GH_AW_PROMPT":                 "/tmp/gh-aw/aw-prompts/prompt.txt",
		"GH_AW_MCP_CONFIG":             "/tmp/gh-aw/mcp-config/config.toml",
		"CODEX_HOME":                   "/tmp/gh-aw/mcp-config",
		"RUST_LOG":                     "trace,hyper_util=info,mio=info,reqwest=info,os_info=info,codex_otel=warn,codex_core=debug,ocodex_exec=debug",
		"GH_AW_GITHUB_TOKEN":           effectiveGitHubToken,
		"GITHUB_PERSONAL_ACCESS_TOKEN": effectiveGitHubToken,                                     // Used by GitHub MCP server via env_vars
		"OPENAI_API_KEY":               "${{ secrets.CODEX_API_KEY || secrets.OPENAI_API_KEY }}", // Fallback for CODEX_API_KEY
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

	// Add model environment variable if model is not explicitly configured
	// This allows users to configure the default model via GitHub Actions variables
	// Use different env vars for agent vs detection jobs
	if !modelConfigured {
		// Check if this is a detection job (has no SafeOutputs config)
		isDetectionJob := workflowData.SafeOutputs == nil
		if isDetectionJob {
			// For detection, use detection-specific env var (no default fallback for Codex)
			env[constants.EnvVarModelDetectionCodex] = fmt.Sprintf("${{ vars.%s || '' }}", constants.EnvVarModelDetectionCodex)
		} else {
			// For agent execution, use agent-specific env var
			env[constants.EnvVarModelAgentCodex] = fmt.Sprintf("${{ vars.%s || '' }}", constants.EnvVarModelAgentCodex)
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
		codexEngineLog.Printf("Added %d custom env vars from agent config", len(agentConfig.Env))
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

	// Generate the step for Codex execution
	stepName := "Run Codex"
	var stepLines []string

	stepLines = append(stepLines, fmt.Sprintf("      - name: %s", stepName))

	// Filter environment variables to only include allowed secrets
	// This is a security measure to prevent exposing unnecessary secrets to the AWF container
	allowedSecrets := e.GetRequiredSecretNames(workflowData)
	filteredEnv := FilterEnvForSecrets(env, allowedSecrets)

	// Format step with command and filtered environment variables using shared helper
	stepLines = FormatStepWithCommandAndEnv(stepLines, command, filteredEnv)

	steps = append(steps, GitHubActionStep(stepLines))

	return steps
}

// GetFirewallLogsCollectionStep returns the step for collecting firewall logs (before secret redaction).
// This method is part of the firewall integration interface. It returns an empty slice because
// firewall logs are written to a known location (/tmp/gh-aw/sandbox/firewall/logs/) and don't need
// a separate collection step. The method is still called from compiler_yaml_main_job.go to maintain
// consistent behavior with other engines that may need log collection steps.
func (e *CodexEngine) GetFirewallLogsCollectionStep(workflowData *WorkflowData) []GitHubActionStep {
	return []GitHubActionStep{}
}

// GetSquidLogsSteps returns the steps for uploading and parsing Squid logs (after secret redaction)
func (e *CodexEngine) GetSquidLogsSteps(workflowData *WorkflowData) []GitHubActionStep {
	var steps []GitHubActionStep

	// Only add upload and parsing steps if firewall is enabled
	if isFirewallEnabled(workflowData) {
		codexEngineLog.Printf("Adding Squid logs upload and parsing steps for workflow: %s", workflowData.Name)

		squidLogsUpload := generateSquidLogsUploadStep(workflowData.Name)
		steps = append(steps, squidLogsUpload)

		// Add firewall log parsing step to create step summary
		firewallLogParsing := generateFirewallLogParsingStep(workflowData.Name)
		steps = append(steps, firewallLogParsing)
	} else {
		codexEngineLog.Print("Firewall disabled, skipping Squid logs upload")
	}

	return steps
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
	for key, value := range toolsConfig.Custom {
		result.Custom[key] = value
	}

	// Copy raw map
	for key, value := range toolsConfig.raw {
		result.raw[key] = value
	}

	// Handle playwright tool by converting it to an MCP tool configuration with copilot agent tools
	if toolsConfig.Playwright != nil {
		// Create an updated Playwright config with the allowed tools
		playwrightConfig := &PlaywrightToolConfig{
			Version:        toolsConfig.Playwright.Version,
			AllowedDomains: toolsConfig.Playwright.AllowedDomains,
			Args:           toolsConfig.Playwright.Args,
		}

		result.Playwright = playwrightConfig

		// Also update the Custom map entry for playwright with allowed tools list
		playwrightMCP := map[string]any{
			"allowed": GetCopilotAgentPlaywrightTools(),
		}
		if playwrightConfig.Version != "" {
			playwrightMCP["version"] = playwrightConfig.Version
		}
		if len(playwrightConfig.AllowedDomains) > 0 {
			playwrightMCP["allowed_domains"] = playwrightConfig.AllowedDomains
		}
		if len(playwrightConfig.Args) > 0 {
			playwrightMCP["args"] = playwrightConfig.Args
		}

		// Update raw map for backward compatibility
		result.raw["playwright"] = playwrightMCP
	}

	return result
}

// expandNeutralToolsToCodexToolsFromMap is a backward compatibility wrapper
// that accepts map[string]any instead of *ToolsConfig
func (e *CodexEngine) expandNeutralToolsToCodexToolsFromMap(tools map[string]any) map[string]any {
	toolsConfig, _ := ParseToolsConfig(tools)
	result := e.expandNeutralToolsToCodexTools(toolsConfig)
	return result.ToMap()
}

// renderShellEnvironmentPolicy generates the [shell_environment_policy] section for config.toml
// This controls which environment variables are passed through to MCP servers for security
func (e *CodexEngine) renderShellEnvironmentPolicy(yaml *strings.Builder, tools map[string]any, mcpTools []string, workflowData *WorkflowData) {
	// Collect all environment variables needed by MCP servers
	envVars := make(map[string]bool)

	// Always include core environment variables
	envVars["PATH"] = true
	envVars["HOME"] = true

	// Add CODEX_API_KEY for authentication
	envVars["CODEX_API_KEY"] = true
	envVars["OPENAI_API_KEY"] = true // Fallback for CODEX_API_KEY

	// Check each MCP tool for required environment variables
	for _, toolName := range mcpTools {
		switch toolName {
		case "github":
			// GitHub MCP server needs GITHUB_PERSONAL_ACCESS_TOKEN
			envVars["GITHUB_PERSONAL_ACCESS_TOKEN"] = true
		case "agentic-workflows":
			// Agentic workflows MCP server needs GITHUB_TOKEN
			envVars["GITHUB_TOKEN"] = true
		case "safe-outputs":
			// Safe outputs MCP server needs several environment variables
			envVars["GH_AW_SAFE_OUTPUTS"] = true
			envVars["GH_AW_ASSETS_BRANCH"] = true
			envVars["GH_AW_ASSETS_MAX_SIZE_KB"] = true
			envVars["GH_AW_ASSETS_ALLOWED_EXTS"] = true
			envVars["GITHUB_REPOSITORY"] = true
			envVars["GITHUB_SERVER_URL"] = true
		default:
			// For custom MCP tools, check if they have env configuration
			if toolValue, ok := tools[toolName]; ok {
				if toolConfig, ok := toolValue.(map[string]any); ok {
					// Extract environment variable names from env configuration
					if env, hasEnv := toolConfig["env"].(map[string]any); hasEnv {
						for envKey := range env {
							envVars[envKey] = true
						}
					}
				}
			}
		}
	}

	// Sort environment variable names for consistent output
	var sortedEnvVars []string
	for envVar := range envVars {
		sortedEnvVars = append(sortedEnvVars, envVar)
	}
	sort.Strings(sortedEnvVars)

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

// RenderMCPConfig is implemented in codex_mcp.go

// renderCodexMCPConfig is implemented in codex_mcp.go

// ParseLogMetrics is implemented in codex_logs.go

// parseCodexToolCallsWithSequence is implemented in codex_logs.go

// updateMostRecentToolWithDuration is implemented in codex_logs.go

// extractCodexTokenUsage is implemented in codex_logs.go

// GetLogParserScriptId is implemented in codex_logs.go
