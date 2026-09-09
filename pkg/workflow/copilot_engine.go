// This file implements the GitHub Copilot CLI agentic engine.
//
// The Copilot engine is organized into focused modules:
//   - copilot_engine.go: Core engine interface and constructor
//   - copilot_engine_installation.go: Installation workflow generation
//   - copilot_engine_execution.go: Execution workflow and runtime configuration
//   - copilot_engine_tools.go: Tool permissions, arguments, and error patterns
//   - copilot_logs.go: Log parsing, metrics extraction, and log management
//   - copilot_mcp.go: MCP server configuration rendering
//   - copilot_participant_steps.go: Copilot CLI participant steps
//
// This modular organization improves maintainability and makes it easier
// to locate and modify specific functionality.

package workflow

import (
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

var copilotLog = logger.New("workflow:copilot_engine")

const logsFolder = "/tmp/gh-aw/sandbox/agent/logs/"

// CopilotEngine represents the GitHub Copilot CLI agentic engine.
// It provides integration with GitHub Copilot CLI for agentic workflows,
// including MCP server support, sandboxing (AWF/SRT), and tool permissions.
type CopilotEngine struct {
	BaseEngine
}

var _ CodingAgentEngine = (*CopilotEngine)(nil)

func NewCopilotEngine() *CopilotEngine {
	copilotLog.Print("Creating new Copilot engine instance")
	return &CopilotEngine{
		BaseEngine: BaseEngine{
			id:               "copilot",
			displayName:      "GitHub Copilot CLI",
			description:      "Uses GitHub Copilot CLI with MCP server support",
			experimental:     false,
			ghSkillAgentName: "github-copilot",
			capabilities: EngineCapabilities{
				ToolsAllowlist:       true,
				MCP:                  true,
				MaxTurns:             true,  // AWF max-turns is supported for Copilot runs
				MaxContinuations:     true,  // Copilot CLI supports --autopilot with --max-autopilot-continues
				WebSearch:            false, // Copilot CLI does not have built-in web-search support
				BareMode:             true,  // Copilot CLI supports --no-custom-instructions
				BashCommandAllowlist: true,  // Copilot enforces tools.bash allowlist via --allow-tool shell(cmd)
				Plugins:              true,  // Copilot CLI supports Agent Plugins
			},
			dedicatedLLMGatewayPort: constants.CopilotLLMGatewayPort,
		},
	}
}

// GetAPMTarget returns "copilot" so that apm-action packs Copilot-specific primitives.
func (e *CopilotEngine) GetAPMTarget() string {
	return "copilot"
}

// GetModelEnvVarName returns the native environment variable name that the Copilot CLI uses
// for model selection. Setting COPILOT_MODEL is equivalent to passing --model to the CLI.
func (e *CopilotEngine) GetModelEnvVarName() string {
	return constants.CopilotCLIModelEnvVar
}

// ResolveLLMProvider returns the effective provider for Copilot inference.
// Default is github, overridable via engine.model-provider.
func (e *CopilotEngine) ResolveLLMProvider(workflowData *WorkflowData) LLMProvider {
	return resolveEngineLLMProvider(workflowData, LLMProviderGitHub)
}

// GetRequiredSecretNames returns the list of secrets required by the Copilot engine.
// This includes COPILOT_GITHUB_TOKEN and optionally MCP_GATEWAY_AGENT_ID.
// It also includes COPILOT_PROVIDER_* env var keys that may carry secrets when BYOK mode
// is configured — allowing them to pass through strict-mode validation and the secret filter.
func (e *CopilotEngine) GetRequiredSecretNames(workflowData *WorkflowData) []string {
	copilotLog.Print("Collecting required secrets for Copilot engine")
	provider := e.ResolveLLMProvider(workflowData)
	secrets := append([]string{}, llmProviderSecretNames(provider)...)
	// Always include the BYOK provider keys so that secrets assigned to them via engine.env
	// pass through the strict-mode validator and FilterEnvForSecrets.
	secrets = append(secrets,
		constants.CopilotProviderBaseURL,
		constants.CopilotProviderAPIKey,
		constants.CopilotProviderBearerToken,
	)

	// Add MCP gateway agent ID if MCP servers are present (gateway is always started with MCP servers)
	if HasMCPServers(workflowData) {
		copilotLog.Print("Adding MCP_GATEWAY_AGENT_ID secret")
		secrets = append(secrets, "MCP_GATEWAY_AGENT_ID")
	}

	// Add GitHub token for GitHub MCP server if present
	if hasGitHubTool(workflowData.ParsedTools) {
		copilotLog.Print("Adding GITHUB_MCP_SERVER_TOKEN secret")
		secrets = append(secrets, "GITHUB_MCP_SERVER_TOKEN")
	}

	// Add HTTP MCP header secret names
	headerSecrets := collectHTTPMCPHeaderSecrets(workflowData.Tools)
	for varName := range headerSecrets {
		secrets = append(secrets, varName)
	}
	if len(headerSecrets) > 0 {
		copilotLog.Printf("Added %d HTTP MCP header secrets", len(headerSecrets))
	}

	// Add mcp-scripts secret names
	if IsMCPScriptsEnabled(workflowData.MCPScripts) {
		mcpScriptsSecrets := collectMCPScriptsSecrets(workflowData.MCPScripts)
		for varName := range mcpScriptsSecrets {
			secrets = append(secrets, varName)
		}
		if len(mcpScriptsSecrets) > 0 {
			copilotLog.Printf("Added %d mcp-scripts secrets", len(mcpScriptsSecrets))
		}
	}

	copilotLog.Printf("Total required secrets: %d", len(secrets))
	return secrets
}

// GetSupportedEnvVarKeys returns the engine.env variable names that the Copilot engine
// supports as defined in the AWF specification. These cover the primary auth token and
// all BYOK provider variables that may carry secret values.
func (e *CopilotEngine) GetSupportedEnvVarKeys() []string {
	return []string{
		constants.CopilotGitHubToken,
		constants.CopilotProviderBaseURL,
		constants.CopilotProviderAPIKey,
		constants.CopilotProviderBearerToken,
		constants.CopilotProviderWireAPI,
	}
}

// GetPluginInstallationSteps installs pinned Agent Plugins through the Copilot CLI.
func (e *CopilotEngine) GetPluginInstallationSteps(workflowData *WorkflowData) []GitHubActionStep {
	commandName := "copilot"
	if workflowData != nil && workflowData.EngineConfig != nil && workflowData.EngineConfig.Command != "" {
		commandName = workflowData.EngineConfig.Command
	}
	return generatePluginInstallationSteps(workflowData, pluginInstallSpec{
		Command:     commandName,
		InstallArgs: []string{"plugin", "install"},
	})
}

// GetInstallationSteps is implemented in copilot_engine_installation.go

func (e *CopilotEngine) GetDeclaredOutputFiles() []string {
	// Session state files are copied to logs folder by GetFirewallLogsCollectionStep
	return []string{logsFolder}
}

// GetAgentManifestFiles returns instruction files that should be treated as
// security-sensitive manifests to protect against injection attacks in fork PRs.
// AGENTS.md is the cross-engine convention read by Copilot.
func (e *CopilotEngine) GetAgentManifestFiles() []string {
	return []string{"AGENTS.md"}
}

// GetAgentManifestPathPrefixes returns Copilot-specific config directory prefixes
// that must be protected from fork PR injection.
// The .github/ directory contains copilot-instructions.md, path-specific instruction
// files, and copilot-setup-steps.yml — any of which can alter agent behaviour.
func (e *CopilotEngine) GetAgentManifestPathPrefixes() []string {
	return []string{constants.GithubDir}
}

// GetHarnessScriptName returns the filename of the JavaScript harness script that wraps
// the Copilot CLI with retry logic for transient CAPIError 400 errors.
func (e *CopilotEngine) GetHarnessScriptName() string {
	return "copilot_harness.cjs"
}

// GetExecutionSteps is implemented in copilot_engine_execution.go

// RenderMCPConfig is implemented in copilot_mcp.go

// ParseLogMetrics is implemented in copilot_logs.go

// extractToolCallSizes is implemented in copilot_logs.go

// processToolCalls is implemented in copilot_logs.go

// parseCopilotToolCallsWithSequence is implemented in copilot_logs.go

// GetLogParserScriptId is implemented in copilot_logs.go

// GetLogFileForParsing is implemented in copilot_logs.go

// GetFirewallLogsCollectionStep returns steps for collecting firewall logs and copying session state files
func (e *CopilotEngine) GetFirewallLogsCollectionStep(workflowData *WorkflowData) []GitHubActionStep {
	var steps []GitHubActionStep

	// Add step to copy Copilot session state files to logs folder
	// This ensures session files are in /tmp/gh-aw/ where secret redaction can scan them
	sessionCopyStep := generateCopilotSessionFileCopyStep()
	steps = append(steps, sessionCopyStep)

	return steps
}

// GetSquidLogsSteps returns the steps for uploading and parsing Squid logs (after secret redaction)
func (e *CopilotEngine) GetSquidLogsSteps(workflowData *WorkflowData) []GitHubActionStep {
	return defaultGetSquidLogsSteps(workflowData, copilotLog)
}

// GetCleanupStep returns the post-execution cleanup step (currently empty)
func (e *CopilotEngine) GetCleanupStep(workflowData *WorkflowData) GitHubActionStep {
	// Return empty step - cleanup steps have been removed
	return GitHubActionStep([]string{})
}

// computeCopilotToolArguments is implemented in copilot_engine_tools.go

// generateCopilotToolArgumentsComment is implemented in copilot_engine_tools.go

// GetErrorPatterns is implemented in copilot_engine_tools.go

// generateAWFInstallationStep is implemented in copilot_engine_installation.go

// GenerateCopilotInstallerSteps is implemented in copilot_installer.go.
// See that file for the full signature and priority documentation.
